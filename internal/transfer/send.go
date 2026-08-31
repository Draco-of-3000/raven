package transfer

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// ErrDeclined is returned by SendV3 when the receiver declines the transfer.
var ErrDeclined = errors.New("receiver declined the transfer")

// ErrNotPaired is returned when the target device has not been paired with us.
var ErrNotPaired = errors.New("not paired with this device")

// sendItem is one concrete file to send, paired with the metadata frame that
// precedes it on the wire. For files inside a dropped folder, meta.Rel carries
// the folder-relative path so the receiver can rebuild the tree.
type sendItem struct {
	path string
	meta fileMeta
}

// dialTLS dials target, completes a mutual-TLS 1.3 handshake, and runs verify
// against the server's pinned fingerprint. Returns the TLS conn and the peer's
// fingerprint. The caller closes the conn.
func dialTLS(ctx context.Context, target string, id *Identity, verify VerifyFunc, dialTimeout time.Duration) (*tls.Conn, string, error) {
	raw, err := dial(ctx, normalizeTarget(target), dialTimeout)
	if err != nil {
		return nil, "", err
	}
	cap := &peerCapture{}
	tc := tls.Client(raw, clientTLSConfig(id, verify, cap))
	hctx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()
	if err := tc.HandshakeContext(hctx); err != nil {
		raw.Close()
		return nil, "", err
	}
	return tc, cap.fp, nil
}

// SendV3 streams files to a PAIRED target over mutual TLS, after the receiver
// accepts. The target must already be paired (its fingerprint known in `paired`);
// an unknown or changed identity aborts the handshake.
func SendV3(ctx context.Context, target string, paths []string, id *Identity, paired *PairedStore, obs Observer, dialTimeout time.Duration) error {
	if obs == nil {
		obs = NopObserver{}
	}
	// Derive a cancelable context so the UI can stop this send mid-flight.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if c, ok := obs.(Canceler); ok {
		c.SetCancel(cancel)
		defer c.SetCancel(nil)
	}
	items, err := expand(paths)
	if err != nil {
		return err
	}
	// Verify: the server's fingerprint must be one we have paired with.
	verify := func(fp string) error {
		if paired != nil && paired.IsPaired(fp) {
			return nil
		}
		return ErrNotPaired
	}
	tc, peerFP, err := dialTLS(ctx, target, id, verify, dialTimeout)
	if err != nil {
		return err
	}
	defer tc.Close()

	// Cancellation: closing the conn unblocks any in-flight read/write, so a
	// cancelled ctx aborts the transfer promptly on this (sender) side.
	stop := closeOnCancel(ctx, tc)
	defer stop()

	// Prefer the paired device's friendly name over the raw address for display.
	peerLabel := target
	if paired != nil {
		if dev, ok := paired.Get(peerFP); ok && dev.Name != "" {
			peerLabel = dev.Name
		}
	}

	if _, err := tc.Write([]byte(magicV3)); err != nil {
		return fmt.Errorf("handshake failed: %w", err)
	}
	if _, err := tc.Write([]byte{intentTransfer}); err != nil {
		return err
	}
	if err := writeUint32(tc, uint32(len(items))); err != nil {
		return err
	}
	for _, it := range items {
		if err := writeMetaFrame(tc, it.meta); err != nil {
			return err
		}
	}

	// Blocks here until the receiver accepts/declines; tell the UI we're waiting.
	obs.WaitingForAccept(peerLabel)
	ans := make([]byte, 1)
	if _, err := io.ReadFull(tc, ans); err != nil {
		if ctx.Err() != nil {
			return ctx.Err() // cancelled while waiting for accept
		}
		return fmt.Errorf("read accept: %w", err)
	}
	if ans[0] != 0x01 {
		return ErrDeclined
	}

	var totalBytes int64
	for _, it := range items {
		totalBytes += it.meta.Size
	}
	obs.SessionStart(Sending, peerLabel, len(items), totalBytes)
	for i, it := range items {
		if err := sendBody(tc, it.path, it.meta, i+1, len(items), obs); err != nil {
			obs.SessionEnd(Sending, peerLabel, err)
			return fmt.Errorf("failed sending %q: %w", it.path, err)
		}
	}
	obs.SessionEnd(Sending, peerLabel, nil)
	return nil
}

// Pair performs the dialing side of device pairing with target: TLS handshake
// (any peer cert accepted, this is trust-on-first-use), then the SAS exchange.
// On mutual confirmation it returns the peer to store. localName is our device
// name; confirm shows the 6-digit code and awaits the user.
func Pair(ctx context.Context, target string, id *Identity, localName string, confirm ConfirmSASFunc, dialTimeout time.Duration) (PairedDevice, error) {
	// Pairing accepts any peer identity (we have no prior knowledge); the SAS is
	// what authenticates it.
	tc, peerFP, err := dialTLS(ctx, target, id, func(string) error { return nil }, dialTimeout)
	if err != nil {
		return PairedDevice{}, err
	}
	defer tc.Close()

	if _, err := tc.Write([]byte(magicV3)); err != nil {
		return PairedDevice{}, fmt.Errorf("handshake failed: %w", err)
	}
	if _, err := tc.Write([]byte{intentPair}); err != nil {
		return PairedDevice{}, err
	}
	dev, err := runPairInitiator(tc, localName, id.FP, peerFP, confirm)
	if err != nil {
		return PairedDevice{}, err
	}
	return dev, nil
}

// expand turns a list of dropped paths (files and/or folders) into a flat list
// of files to send. Folders are walked recursively; each contained file gets a
// Rel path rooted at the dropped folder's own name (e.g. "photos/trip/a.jpg"),
// so the receiver can rebuild the structure. Loose files have an empty Rel.
// Empty directories and symlinks are skipped.
func expand(paths []string) ([]sendItem, error) {
	if len(paths) == 0 {
		return nil, errors.New("nothing to send")
	}
	var items []sendItem
	for _, p := range paths {
		fi, err := os.Stat(p)
		if err != nil {
			return nil, fmt.Errorf("cannot read %q: %w", p, err)
		}
		if !fi.IsDir() {
			items = append(items, sendItem{path: p, meta: fileMeta{Name: fi.Name(), Size: fi.Size()}})
			continue
		}
		root := filepath.Clean(p)
		base := filepath.Base(root) // the folder's own name, e.g. "photos"
		err = filepath.WalkDir(root, func(fp string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !d.Type().IsRegular() {
				return nil // skip dirs themselves and any non-regular entries (symlinks, etc.)
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(root, fp)
			if err != nil {
				return err
			}
			// Rel is forward-slash and rooted at the folder name.
			relSlash := path.Join(base, filepath.ToSlash(rel))
			items = append(items, sendItem{path: fp, meta: fileMeta{Name: info.Name(), Size: info.Size(), Rel: relSlash}})
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("reading folder %q: %w", p, err)
		}
	}
	if len(items) == 0 {
		return nil, errors.New("nothing to send (folder is empty)")
	}
	return items, nil
}

func normalizeTarget(target string) string {
	if !strings.Contains(target, ":") {
		return fmt.Sprintf("%s:%d", target, DefaultTCP)
	}
	return target
}

func dial(ctx context.Context, target string, timeout time.Duration) (net.Conn, error) {
	d := net.Dialer{Timeout: timeout}
	conn, err := d.DialContext(ctx, "tcp", target)
	if err != nil {
		return nil, fmt.Errorf("cannot connect to %s: %w", target, err)
	}
	return conn, nil
}

func writeMetaFrame(conn net.Conn, meta fileMeta) error {
	metaBytes, _ := json.Marshal(meta)
	if err := writeUint32(conn, uint32(len(metaBytes))); err != nil {
		return err
	}
	_, err := conn.Write(metaBytes)
	return err
}

func sendBody(conn net.Conn, path string, meta fileMeta, idx, total int, obs Observer) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	label := displayName(meta)
	obs.FileStart(Sending, idx, total, label, meta.Size)
	hasher := sha256.New()
	pw := &progressWriter{obs: obs, dir: Sending, index: idx, total: total, name: label, size: meta.Size}
	src := io.TeeReader(f, io.MultiWriter(hasher, pw))
	if _, err := io.CopyN(conn, src, meta.Size); err != nil {
		obs.FileDone(FileResult{Dir: Sending, Index: idx, Total: total, Name: label, Size: meta.Size, Err: err})
		return fmt.Errorf("send body: %w", err)
	}
	if _, err := conn.Write(hasher.Sum(nil)); err != nil {
		return fmt.Errorf("send checksum: %w", err)
	}
	ack := make([]byte, 1)
	if _, err := io.ReadFull(conn, ack); err != nil {
		return fmt.Errorf("read ack: %w", err)
	}
	if ack[0] != 0x01 {
		err := errors.New("receiver reported checksum mismatch")
		obs.FileDone(FileResult{Dir: Sending, Index: idx, Total: total, Name: label, Size: meta.Size, Err: err})
		return err
	}
	obs.FileDone(FileResult{Dir: Sending, Index: idx, Total: total, Name: label, Size: meta.Size, Verified: true})
	return nil
}

// displayName is what progress/labels show: the relative path for folder files
// (so "photos/trip/a.jpg" reads clearly), otherwise just the file name.
func displayName(meta fileMeta) string {
	if meta.Rel != "" {
		return meta.Rel
	}
	return meta.Name
}
