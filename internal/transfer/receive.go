package transfer

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// AcceptFunc decides whether to accept an incoming transfer session, given the
// peer (already authenticated as paired) and the file manifest. nil = auto-accept.
type AcceptFunc func(peerFP, peerName string, files []IncomingFile) bool

// Receiver listens for incoming transfers and reports them through an Observer.
// All connections are mutual-TLS; a peer must be paired before it can transfer.
type Receiver struct {
	Dir        string                     // directory to save received files into
	Name       string                     // friendly name advertised during discovery
	Port       int                        // TCP port to listen on (0 means DefaultTCP)
	Obs        Observer                   // shared observer (nil means NopObserver); used if NewObs is nil
	NewObs     func(peer string) Observer // per-session observer factory (preferred for the GUI)
	Concurrent bool                       // handle sessions concurrently (GUI) vs sequentially (CLI)
	Accept     AcceptFunc                 // transfer gate (nil = auto-accept)

	Identity     *Identity                       // this device's TLS identity (required)
	Paired       *PairedStore                    // known/trusted peers (required)
	OnPair       ConfirmSASFunc                  // shows the SAS during an incoming pairing (nil = refuse pairing)
	OnPairResult func(dev PairedDevice, ok bool) // authoritative outcome after the mutual exchange

	serveCtx  context.Context // base context for derived per-session cancellation
	ln        net.Listener
	responder *Responder
	wg        sync.WaitGroup
	sem       chan struct{} // concurrency cap for receive sessions
}

func nowUnix() int64 { return time.Now().Unix() }

// Listen binds the TCP port. Call it before Serve so bind errors surface early.
func (r *Receiver) Listen() error {
	if r.Identity == nil {
		return errors.New("receiver requires an Identity")
	}
	if r.Port == 0 {
		r.Port = DefaultTCP
	}
	if r.Dir == "" {
		r.Dir = "."
	}
	if err := os.MkdirAll(r.Dir, 0o755); err != nil {
		return fmt.Errorf("cannot create save directory: %w", err)
	}
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", r.Port))
	if err != nil {
		return fmt.Errorf("cannot listen on port %d: %w", r.Port, err)
	}
	r.ln = ln
	return nil
}

// LocalAddr reports the chosen LAN IP and port once Listen has succeeded.
func (r *Receiver) LocalAddr() (string, int) {
	return LocalIP(), r.Port
}

// Serve accepts connections until ctx is cancelled or Close is called. It blocks,
// so run it in a goroutine.
func (r *Receiver) Serve(ctx context.Context) error {
	if r.ln == nil {
		return errors.New("Serve called before Listen")
	}
	r.serveCtx = ctx
	if resp, err := StartResponder(ctx, r.Name, r.Port); err == nil {
		r.responder = resp
	}
	go func() {
		<-ctx.Done()
		r.ln.Close()
	}()

	if r.sem == nil {
		r.sem = make(chan struct{}, maxSessions)
	}
	for {
		conn, err := r.ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) || ctx.Err() != nil {
				break
			}
			continue
		}
		if r.Concurrent {
			select {
			case r.sem <- struct{}{}: // acquire a session slot
			default:
				conn.Close() // at capacity: drop rather than spawn unbounded goroutines
				continue
			}
			r.wg.Add(1)
			go func() {
				defer r.wg.Done()
				defer func() { <-r.sem }()
				r.handleIncoming(conn)
			}()
		} else {
			r.handleIncoming(conn)
		}
	}
	r.wg.Wait()
	return nil
}

// Close stops the discovery responder and the accept loop.
func (r *Receiver) Close() error {
	if r.responder != nil {
		r.responder.Close()
	}
	if r.ln != nil {
		return r.ln.Close()
	}
	return nil
}

func (r *Receiver) obsFor(peer string) Observer {
	if r.NewObs != nil {
		if o := r.NewObs(peer); o != nil {
			return o
		}
	}
	if r.Obs != nil {
		return r.Obs
	}
	return NopObserver{}
}

func (r *Receiver) handleIncoming(rawConn net.Conn) {
	defer rawConn.Close()
	peerAddr := rawConn.RemoteAddr().String()

	// Wrap in TLS and complete the mutual handshake BEFORE reading any framing.
	// VerifyPeerCertificate records the peer's fingerprint via cap.
	cap := &peerCapture{}
	tc := tls.Server(rawConn, serverTLSConfig(r.Identity, func(string) error { return nil }, cap))
	_ = tc.SetDeadline(time.Now().Add(idleTimeout))
	if err := tc.Handshake(); err != nil {
		return // not a TLS speaker, or handshake failed
	}
	// Clear the one-time handshake deadline. Otherwise this absolute deadline
	// would later kill writes (e.g. the per-file ack) once it elapses, breaking
	// long multi-file transfers. From here on, deadlines are set per-operation.
	_ = tc.SetDeadline(time.Time{})
	peerFP := cap.fp

	hdr := make([]byte, len(magicV3))
	if _, err := io.ReadFull(tc, hdr); err != nil || string(hdr) != magicV3 {
		return // not a Raven sender
	}
	intent := make([]byte, 1)
	if _, err := io.ReadFull(tc, intent); err != nil {
		return
	}

	switch intent[0] {
	case intentPair:
		r.handlePair(tc, peerFP)
	case intentTransfer:
		r.handleTransfer(tc, peerAddr, peerFP)
	default:
		// unknown intent
	}
}

// handlePair runs the responder side of pairing. Allowed for any (even unknown)
// peer, since pairing is precisely how an unknown peer becomes trusted. Refused if no
// OnPair hook is set (e.g. CLI receiver without a confirm callback).
func (r *Receiver) handlePair(tc *tls.Conn, peerFP string) {
	if r.OnPair == nil {
		return
	}
	_ = tc.SetDeadline(time.Now().Add(2 * time.Minute)) // pairing waits on a human
	dev, err := runPairResponder(tc, r.Name, r.Identity.FP, peerFP, r.OnPair)
	if err != nil {
		// Mutual confirmation failed (one side said "codes differ", or a timeout).
		if r.OnPairResult != nil {
			r.OnPairResult(PairedDevice{Fingerprint: peerFP}, false)
		}
		return
	}
	if r.Paired != nil {
		dev.PairedAt = nowUnix()
		dev.LastSeen = dev.PairedAt
		_ = r.Paired.Add(dev)
	}
	if r.OnPairResult != nil {
		r.OnPairResult(dev, true) // both sides confirmed
	}
}

// handleTransfer runs the responder side of a file transfer. The peer MUST be
// paired; an unpaired peer is declined before any file is written.
func (r *Receiver) handleTransfer(tc *tls.Conn, peerAddr, peerFP string) {
	obs := r.obsFor(peerAddr)

	// Per-session cancellation: the receiver can stop an in-flight transfer.
	// Closing the conn on cancel unblocks the body read.
	base := r.serveCtx
	if base == nil {
		base = context.Background()
	}
	sctx, cancel := context.WithCancel(base)
	defer cancel()
	stop := closeOnCancel(sctx, tc)
	defer stop()
	if c, ok := obs.(Canceler); ok {
		c.SetCancel(cancel)
		defer c.SetCancel(nil)
	}

	paired, isPaired := PairedDevice{}, false
	if r.Paired != nil {
		paired, isPaired = r.Paired.Get(peerFP)
	}
	peerLabel := peerAddr
	if isPaired && paired.Name != "" {
		peerLabel = paired.Name
	}

	count, err := readUint32(tc)
	if err != nil {
		obs.SessionEnd(Receiving, peerLabel, fmt.Errorf("read file count: %w", err))
		return
	}
	if count > maxFileCount {
		obs.SessionEnd(Receiving, peerLabel, fmt.Errorf("file count %d exceeds limit", count))
		return
	}
	metas := make([]fileMeta, 0, count)
	for i := uint32(0); i < count; i++ {
		_ = tc.SetReadDeadline(time.Now().Add(idleTimeout))
		m, err := readMetaFrame(tc)
		if err != nil {
			obs.SessionEnd(Receiving, peerLabel, err)
			return
		}
		metas = append(metas, m)
	}

	// Authorization: unpaired peers cannot transfer. Then the accept gate.
	accepted := isPaired
	if accepted && r.Accept != nil {
		incoming := make([]IncomingFile, len(metas))
		for i, m := range metas {
			incoming[i] = IncomingFile{Name: sanitizeName(m.Name), Size: m.Size, Rel: safeRel(m.Rel)}
		}
		accepted = r.Accept(peerFP, peerLabel, incoming)
	}
	ack := byte(0x00)
	if accepted {
		ack = 0x01
	}
	if _, err := tc.Write([]byte{ack}); err != nil {
		return
	}
	if !accepted {
		return
	}
	if isPaired {
		paired.LastSeen = nowUnix()
		_ = r.Paired.Add(paired)
	}

	var totalBytes int64
	for _, m := range metas {
		totalBytes += m.Size
	}
	obs.SessionStart(Receiving, peerLabel, len(metas), totalBytes)
	pr := newPathResolver(r.Dir)
	for i, m := range metas {
		// No deadline on the bulk body transfer: a big file on slow WiFi can take
		// minutes, and a fixed deadline would wrongly kill it. Long-stall safety
		// comes from the user's Cancel (closeOnCancel) and TCP itself; the small
		// framed reads/writes (meta, checksum, ack) keep their per-op deadlines.
		if err := r.receiveBody(tc, m, i+1, len(metas), obs, pr); err != nil {
			obs.SessionEnd(Receiving, peerLabel, err)
			return
		}
	}
	obs.SessionEnd(Receiving, peerLabel, nil)
}

func readMetaFrame(conn net.Conn) (fileMeta, error) {
	metaLen, err := readUint32(conn)
	if err != nil {
		return fileMeta{}, fmt.Errorf("read meta length: %w", err)
	}
	if metaLen == 0 || metaLen > maxMetaLen {
		return fileMeta{}, fmt.Errorf("meta frame length %d out of bounds", metaLen)
	}
	buf := make([]byte, metaLen)
	if _, err := io.ReadFull(conn, buf); err != nil {
		return fileMeta{}, fmt.Errorf("read meta: %w", err)
	}
	var m fileMeta
	if err := json.Unmarshal(buf, &m); err != nil {
		return fileMeta{}, fmt.Errorf("bad meta: %w", err)
	}
	if m.Size < 0 {
		return fileMeta{}, fmt.Errorf("negative file size")
	}
	return m, nil
}

func (r *Receiver) receiveBody(conn net.Conn, meta fileMeta, idx, total int, obs Observer, pr *pathResolver) error {
	outPath, label, err := pr.resolve(meta)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return fmt.Errorf("create folder for %s: %w", label, err)
	}
	f, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("create %s: %w", outPath, err)
	}
	defer f.Close()

	obs.FileStart(Receiving, idx, total, label, meta.Size)
	hasher := sha256.New()
	pw := &progressWriter{obs: obs, dir: Receiving, index: idx, total: total, name: label, size: meta.Size}
	dst := io.MultiWriter(f, hasher, pw)
	if _, err := io.CopyN(dst, conn, meta.Size); err != nil {
		// Cancelled or dropped mid-file: don't leave a half-written partial behind.
		f.Close()
		_ = os.Remove(outPath)
		return fmt.Errorf("copy body: %w", err)
	}

	// The trailing checksum should arrive promptly after the body; bound this
	// small read so a stalled peer can't hang us, then clear it.
	_ = conn.SetReadDeadline(time.Now().Add(idleTimeout))
	want := make([]byte, sha256.Size)
	if _, err := io.ReadFull(conn, want); err != nil {
		return fmt.Errorf("read checksum: %w", err)
	}
	_ = conn.SetReadDeadline(time.Time{})
	ok := equal(want, hasher.Sum(nil))

	ackb := byte(0x00)
	if ok {
		ackb = 0x01
	}
	// Give the ack its own short write deadline (not a stale one), then clear it.
	_ = conn.SetWriteDeadline(time.Now().Add(idleTimeout))
	_, _ = conn.Write([]byte{ackb})
	_ = conn.SetWriteDeadline(time.Time{})

	if !ok {
		_ = os.Remove(outPath)
		err := errors.New("checksum mismatch, file discarded")
		obs.FileDone(FileResult{Dir: Receiving, Index: idx, Total: total, Name: label, Size: meta.Size, Err: err})
		return err
	}
	obs.FileDone(FileResult{Dir: Receiving, Index: idx, Total: total, Name: label, Size: meta.Size, SavedTo: outPath, Verified: true})
	return nil
}

// pathResolver maps incoming file metadata to a safe on-disk path within the
// save directory, rebuilding folder structure from Rel while preventing path
// traversal. A dropped folder's root name is renamed once on collision (e.g.
// "photos" -> "photos (1)") and that mapping is reused for every file in it, so
// the whole folder stays together. Loose files keep the original per-file
// uniquePath behavior.
type pathResolver struct {
	dir   string
	roots map[string]string // original folder root -> chosen (possibly renamed) root
}

func newPathResolver(dir string) *pathResolver {
	return &pathResolver{dir: dir, roots: map[string]string{}}
}

// resolve returns the absolute output path and a human-friendly label.
func (pr *pathResolver) resolve(meta fileMeta) (string, string, error) {
	rel := safeRel(meta.Rel)
	if rel == "" {
		// Loose file: sanitize and de-dupe per file, as before.
		safe := sanitizeName(meta.Name)
		out := uniquePath(filepath.Join(pr.dir, safe))
		return out, safe, nil
	}

	parts := strings.Split(rel, "/")
	origRoot := parts[0]
	chosenRoot, ok := pr.roots[origRoot]
	if !ok {
		// First time we see this folder root: pick a unique directory name.
		chosenRoot = filepath.Base(uniqueDir(filepath.Join(pr.dir, origRoot)))
		pr.roots[origRoot] = chosenRoot
	}
	parts[0] = chosenRoot

	// Build and confirm the path stays within pr.dir (defense in depth).
	out := filepath.Join(append([]string{pr.dir}, parts...)...)
	base := filepath.Clean(pr.dir) + string(os.PathSeparator)
	if !strings.HasPrefix(filepath.Clean(out)+string(os.PathSeparator), base) {
		return "", "", fmt.Errorf("unsafe path %q rejected", meta.Rel)
	}
	label := chosenRoot + "/" + strings.Join(parts[1:], "/")
	return out, label, nil
}

// safeRel sanitizes a folder-relative path: forward-slash only, each component
// cleaned with sanitizeName, with empty/./.. components dropped. Returns "" if
// nothing safe remains (caller then treats it as a loose file).
func safeRel(rel string) string {
	if rel == "" {
		return ""
	}
	rel = filepath.ToSlash(rel)
	var clean []string
	for _, comp := range strings.Split(rel, "/") {
		if comp == "" || comp == "." || comp == ".." {
			continue
		}
		clean = append(clean, sanitizeName(comp))
	}
	return strings.Join(clean, "/")
}

// uniqueDir is uniquePath for directories: foo -> "foo (1)" if foo exists.
func uniqueDir(p string) string {
	if _, err := os.Stat(p); errors.Is(err, os.ErrNotExist) {
		return p
	}
	for i := 1; ; i++ {
		cand := fmt.Sprintf("%s (%d)", p, i)
		if _, err := os.Stat(cand); errors.Is(err, os.ErrNotExist) {
			return cand
		}
	}
}
