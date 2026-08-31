package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"raven/internal/transfer"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// Config is the user's persisted settings.
type Config struct {
	DeviceName       string `json:"deviceName"`
	SaveDir          string `json:"saveDir"`
	Theme            string `json:"theme"` // "parchment" | "graphite" | "system"
	Port             int    `json:"port"`
	AutoAcceptPaired bool   `json:"autoAcceptPaired"` // skip the prompt for paired devices
}

// Status is the snapshot the frontend renders the title/rail from.
type Status struct {
	Listening        bool   `json:"listening"`
	IP               string `json:"ip"`
	Port             int    `json:"port"`
	SaveDir          string `json:"saveDir"`
	DeviceName       string `json:"deviceName"`
	Theme            string `json:"theme"`
	Fingerprint      string `json:"fingerprint"`      // this device's identity (hex SPKI hash)
	ShortFingerprint string `json:"shortFingerprint"` // compact form for display
	AutoAcceptPaired bool   `json:"autoAcceptPaired"`
}

// App is the Wails backend, bound to the frontend.
type App struct {
	ctx        context.Context
	cancel     context.CancelFunc
	recvCancel context.CancelFunc
	recv       *transfer.Receiver

	identity *transfer.Identity
	paired   *transfer.PairedStore

	mu        sync.Mutex
	cfg       Config
	listenIP  string
	listening bool

	pending sync.Map // incoming-request / pairing id -> chan bool
	cancels sync.Map // transfer id -> context cancel func (for CancelTransfer)
	counter int64
}

// NewApp creates the app.
func NewApp() *App { return &App{} }

func (a *App) startup(ctx context.Context) {
	c, cancel := context.WithCancel(ctx)
	a.ctx = c
	a.cancel = cancel

	a.cfg = loadConfig()

	// Load (or create) this device's cryptographic identity and the paired store.
	dir := filepath.Dir(configPath())
	id, err := transfer.LoadOrCreateIdentity(dir, a.cfg.DeviceName)
	if err != nil {
		runtime.EventsEmit(a.ctx, "receiver:error", map[string]string{"stage": "identity", "message": err.Error()})
		return
	}
	a.identity = id
	a.paired = transfer.LoadPairedStore(filepath.Join(dir, "paired.json"))

	a.startReceiver()
	// File drops are handled on the frontend via the Wails runtime OnFileDrop
	// (see frontend/src/App.tsx). We don't register the Go-side handler too, to
	// avoid two handlers competing for the same drop.
}

func (a *App) shutdown(ctx context.Context) {
	if a.cancel != nil {
		a.cancel()
	}
	if a.recv != nil {
		a.recv.Close()
	}
}

func (a *App) startReceiver() {
	recvCtx, recvCancel := context.WithCancel(a.ctx)
	a.recvCancel = recvCancel

	a.mu.Lock()
	cfg := a.cfg
	a.mu.Unlock()

	a.recv = &transfer.Receiver{
		Dir:        cfg.SaveDir,
		Name:       cfg.DeviceName,
		Port:       cfg.Port,
		Concurrent: true,
		Accept:     a.confirmAccept,
		Identity:     a.identity,
		Paired:       a.paired,
		OnPair:       a.confirmPairing,
		OnPairResult: a.onPairResult,
		NewObs:       func(peer string) transfer.Observer { return a.newObserver("rx", peer) },
	}
	if err := a.recv.Listen(); err != nil {
		a.mu.Lock()
		a.listening = false
		a.mu.Unlock()
		runtime.EventsEmit(a.ctx, "receiver:error", map[string]string{"stage": "listen", "message": err.Error()})
		return
	}
	ip, port := a.recv.LocalAddr()
	a.mu.Lock()
	a.listenIP = ip
	a.cfg.Port = port
	a.listening = true
	a.mu.Unlock()

	go a.recv.Serve(recvCtx)
	a.emitStatus()
}

func (a *App) restartReceiver() {
	if a.recvCancel != nil {
		a.recvCancel()
	}
	if a.recv != nil {
		a.recv.Close()
	}
	a.startReceiver()
}

func (a *App) emitStatus() {
	runtime.EventsEmit(a.ctx, "receiver:status", a.status())
}

func (a *App) status() Status {
	a.mu.Lock()
	defer a.mu.Unlock()
	fp := ""
	if a.identity != nil {
		fp = a.identity.FP
	}
	return Status{
		Listening:        a.listening,
		IP:               a.listenIP,
		Port:             a.cfg.Port,
		SaveDir:          a.cfg.SaveDir,
		DeviceName:       a.cfg.DeviceName,
		Theme:            a.cfg.Theme,
		Fingerprint:      fp,
		ShortFingerprint: transfer.ShortFingerprint(fp),
		AutoAcceptPaired: a.cfg.AutoAcceptPaired,
	}
}

// ----- Bound methods (callable from the frontend) -----

// GetStatus returns the current listen status for the initial render.
func (a *App) GetStatus() Status { return a.status() }

// GetConfig returns the persisted settings.
func (a *App) GetConfig() Config {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cfg
}

// ListReceivers discovers receivers on the LAN, excluding this app's own
// instance (the GUI runs a discovery responder, so it would otherwise answer
// its own query and list itself).
func (a *App) ListReceivers() ([]transfer.ReceiverInfo, error) {
	list, err := transfer.Discover(a.ctx, 3*time.Second)
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	ownPort := a.cfg.Port
	a.mu.Unlock()
	locals := transfer.LocalIPs()

	out := make([]transfer.ReceiverInfo, 0, len(list))
	for _, r := range list {
		host, portStr, err := net.SplitHostPort(r.Addr)
		if err == nil && locals[host] {
			if p, _ := strconv.Atoi(portStr); p == ownPort {
				continue // this is us
			}
		}
		out = append(out, r)
	}
	return out, nil
}

// SendFiles sends paths to a PAIRED target over TLS (prompt-to-accept). Returns a
// typed "pair first" error if the target is not yet paired.
func (a *App) SendFiles(target string, paths []string) error {
	obs := a.newObserver("tx", target)
	err := transfer.SendV3(a.ctx, target, paths, a.identity, a.paired, obs, 15*time.Second)
	switch {
	case err == transfer.ErrDeclined:
		return fmt.Errorf("the other device declined the transfer")
	case err == transfer.ErrNotPaired:
		return fmt.Errorf("not paired with this device, pair first")
	}
	return err
}

// CheckPaths returns, for each given path, whether it still exists on disk. The
// frontend uses this to strike through history entries whose files were moved or
// deleted. An empty path maps to false.
func (a *App) CheckPaths(paths []string) map[string]bool {
	out := make(map[string]bool, len(paths))
	for _, p := range paths {
		if p == "" {
			continue
		}
		_, err := os.Stat(p)
		out[p] = err == nil
	}
	return out
}

// PickFilesToSend opens a multi-select file picker and returns the chosen paths.
func (a *App) PickFilesToSend() ([]string, error) {
	return runtime.OpenMultipleFilesDialog(a.ctx, runtime.OpenDialogOptions{
		Title:                "Choose files to send",
		CanCreateDirectories: false,
	})
}

// PickFolderToSend opens a directory picker and returns the chosen folder path
// (as a single-element slice, so the caller can treat it like PickFilesToSend).
func (a *App) PickFolderToSend() ([]string, error) {
	dir, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: "Choose a folder to send"})
	if err != nil || dir == "" {
		return nil, err
	}
	return []string{dir}, nil
}

// RevealInFileManager opens the OS file manager with the given path selected
// (macOS Finder, Windows File Explorer, Linux file manager). If the path no
// longer exists, it falls back to opening the save directory.
func (a *App) RevealInFileManager(path string) error {
	if path == "" {
		return fmt.Errorf("no path given")
	}
	if _, err := os.Stat(path); err != nil {
		// File may have been moved/deleted since the transfer; open the save dir.
		path = a.GetConfig().SaveDir
	}
	switch goruntime.GOOS {
	case "darwin":
		return exec.Command("open", "-R", path).Start()
	case "windows":
		// explorer returns a non-zero exit code even on success, so ignore it.
		_ = exec.Command("explorer", "/select,", filepath.Clean(path)).Start()
		return nil
	default: // linux and others: open the containing folder
		dir := path
		if fi, err := os.Stat(path); err == nil && !fi.IsDir() {
			dir = filepath.Dir(path)
		}
		return exec.Command("xdg-open", dir).Start()
	}
}

// fileManagerName returns the platform's file manager name for UI labels.
func (a *App) FileManagerName() string {
	switch goruntime.GOOS {
	case "darwin":
		return "Finder"
	case "windows":
		return "File Explorer"
	default:
		return "Files"
	}
}

// ChooseSaveDir opens a directory picker and persists the choice.
func (a *App) ChooseSaveDir() (string, error) {
	dir, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: "Choose where to save received files"})
	if err != nil || dir == "" {
		return a.GetConfig().SaveDir, err
	}
	a.mu.Lock()
	a.cfg.SaveDir = dir
	a.mu.Unlock()
	if a.recv != nil {
		a.recv.Dir = dir
	}
	a.saveConfig()
	a.emitStatus()
	return dir, nil
}

// SetDeviceName updates the friendly device name.
func (a *App) SetDeviceName(name string) error {
	a.mu.Lock()
	a.cfg.DeviceName = name
	a.mu.Unlock()
	if a.recv != nil {
		a.recv.Name = name
	}
	a.saveConfig()
	a.emitStatus()
	return nil
}

// SetTheme persists the theme preference.
func (a *App) SetTheme(theme string) error {
	a.mu.Lock()
	a.cfg.Theme = theme
	a.mu.Unlock()
	a.saveConfig()
	a.emitStatus()
	return nil
}

// SetPort changes the listen port and restarts the receiver on it. The port must
// be in the unprivileged range so we never try to bind a privileged port.
func (a *App) SetPort(port int) error {
	if port < 1024 || port > 65535 {
		return fmt.Errorf("port must be between 1024 and 65535")
	}
	a.mu.Lock()
	same := port == a.cfg.Port
	a.cfg.Port = port
	a.mu.Unlock()
	if same {
		return nil
	}
	a.saveConfig()
	a.restartReceiver()
	return nil
}

// RespondToIncoming resolves a pending accept prompt (also used for pairing).
func (a *App) RespondToIncoming(id string, accept bool) error {
	if v, ok := a.pending.Load(id); ok {
		select {
		case v.(chan bool) <- accept:
		default:
		}
		return nil
	}
	return fmt.Errorf("no pending request %s", id)
}

// confirmAccept is the receiver's AcceptFunc: it asks the UI and blocks. A paired
// peer is auto-accepted when the user has enabled AutoAcceptPaired.
func (a *App) confirmAccept(peerFP, peerName string, files []transfer.IncomingFile) bool {
	a.mu.Lock()
	auto := a.cfg.AutoAcceptPaired
	a.mu.Unlock()
	if auto && a.paired != nil && a.paired.IsPaired(peerFP) {
		runtime.EventsEmit(a.ctx, "incoming:auto", map[string]interface{}{"peer": peerName, "count": len(files)})
		return true
	}

	id := fmt.Sprintf("in-%d", atomic.AddInt64(&a.counter, 1))
	ch := make(chan bool, 1)
	a.pending.Store(id, ch)
	defer a.pending.Delete(id)

	var total int64
	for _, f := range files {
		total += f.Size
	}
	runtime.EventsEmit(a.ctx, "incoming:request", map[string]interface{}{
		"id":        id,
		"peer":      peerName,
		"files":     files,
		"totalSize": total,
	})

	select {
	case ok := <-ch:
		return ok
	case <-time.After(60 * time.Second):
		runtime.EventsEmit(a.ctx, "incoming:timeout", map[string]string{"id": id})
		return false
	case <-a.ctx.Done():
		return false
	}
}

// confirmPairing is the receiver's pairing hook: it shows the SAS code and blocks
// until the user confirms or cancels (or it times out). It does NOT decide the
// final outcome: pairing only completes when BOTH devices confirm, so the
// authoritative pairing:done is emitted by onPairResult after the mutual
// exchange. Here we only signal the dialog to stop waiting if the local user
// cancels or it times out.
func (a *App) confirmPairing(peerName, code string) bool {
	id := fmt.Sprintf("pair-%d", atomic.AddInt64(&a.counter, 1))
	ch := make(chan bool, 1)
	a.pending.Store(id, ch)
	defer a.pending.Delete(id)

	runtime.EventsEmit(a.ctx, "pairing:code", map[string]interface{}{
		"id": id, "peer": peerName, "code": code, "incoming": true,
	})
	select {
	case ok := <-ch:
		if !ok {
			// Local user said the codes differ: close the dialog now.
			runtime.EventsEmit(a.ctx, "pairing:done", map[string]interface{}{"id": id, "ok": false, "peer": peerName})
		}
		// If ok, leave the dialog up until onPairResult reports the mutual result.
		return ok
	case <-time.After(2 * time.Minute):
		runtime.EventsEmit(a.ctx, "pairing:done", map[string]interface{}{"id": id, "ok": false, "peer": peerName})
		return false
	case <-a.ctx.Done():
		return false
	}
}

// onPairResult is the authoritative outcome of an incoming pairing, emitted after
// BOTH devices have confirmed (or one declined). This is what tells the receiver
// UI whether pairing truly succeeded.
func (a *App) onPairResult(dev transfer.PairedDevice, ok bool) {
	runtime.EventsEmit(a.ctx, "pairing:done", map[string]interface{}{"ok": ok, "peer": dev.Name})
}

// StartPairing dials target and runs the pairing exchange. The SAS code is shown
// to this user via a pairing:code event; ConfirmPairing resolves it.
func (a *App) StartPairing(target string) error {
	a.mu.Lock()
	name := a.cfg.DeviceName
	a.mu.Unlock()

	confirm := func(peerName, code string) bool {
		id := fmt.Sprintf("pair-%d", atomic.AddInt64(&a.counter, 1))
		ch := make(chan bool, 1)
		a.pending.Store(id, ch)
		defer a.pending.Delete(id)
		runtime.EventsEmit(a.ctx, "pairing:code", map[string]interface{}{
			"id": id, "peer": peerName, "code": code, "incoming": false,
		})
		select {
		case ok := <-ch:
			return ok
		case <-time.After(2 * time.Minute):
			return false
		case <-a.ctx.Done():
			return false
		}
	}

	dev, err := transfer.Pair(a.ctx, target, a.identity, name, confirm, 15*time.Second)
	if err != nil {
		runtime.EventsEmit(a.ctx, "pairing:error", map[string]string{"message": err.Error()})
		return err
	}
	dev.PairedAt = time.Now().Unix()
	dev.LastSeen = dev.PairedAt
	_ = a.paired.Add(dev)
	runtime.EventsEmit(a.ctx, "pairing:done", map[string]interface{}{"ok": true, "peer": dev.Name})
	return nil
}

// ConfirmPairing resolves a pending SAS confirmation (same mechanism as
// RespondToIncoming).
func (a *App) ConfirmPairing(id string, accept bool) error {
	return a.RespondToIncoming(id, accept)
}

// ListPairedDevices returns the trusted devices.
func (a *App) ListPairedDevices() []transfer.PairedDevice {
	if a.paired == nil {
		return nil
	}
	return a.paired.List()
}

// ForgetDevice removes a paired device by fingerprint.
func (a *App) ForgetDevice(fingerprint string) error {
	if a.paired == nil {
		return nil
	}
	return a.paired.Remove(fingerprint)
}

// SetAutoAcceptPaired toggles auto-accepting transfers from paired devices.
func (a *App) SetAutoAcceptPaired(on bool) error {
	a.mu.Lock()
	a.cfg.AutoAcceptPaired = on
	a.mu.Unlock()
	a.saveConfig()
	a.emitStatus()
	return nil
}

func (a *App) newObserver(prefix, peer string) transfer.Observer {
	return &wailsObserver{
		app:  a,
		ctx:  a.ctx,
		id:   fmt.Sprintf("%s-%d", prefix, atomic.AddInt64(&a.counter, 1)),
		peer: peer,
	}
}

// CancelTransfer stops an in-flight transfer (send or receive) by its id.
func (a *App) CancelTransfer(id string) error {
	if v, ok := a.cancels.Load(id); ok {
		if c, ok := v.(func()); ok {
			c()
		}
	}
	return nil
}

// wailsObserver bridges the transfer core to frontend events. One instance per
// transfer carries a stable id so the UI can correlate rows.
type wailsObserver struct {
	app  *App
	ctx  context.Context
	id   string
	peer string
}

func (o *wailsObserver) emit(name string, data map[string]interface{}) {
	data["id"] = o.id
	runtime.EventsEmit(o.ctx, name, data)
}

// SetCancel registers (or clears) the cancel func for this transfer so
// CancelTransfer(id) can stop it.
func (o *wailsObserver) SetCancel(cancel func()) {
	if cancel == nil {
		o.app.cancels.Delete(o.id)
		return
	}
	o.app.cancels.Store(o.id, cancel)
}

// WaitingForAccept tells the sender's UI it's blocked awaiting the receiver.
func (o *wailsObserver) WaitingForAccept(peer string) {
	if peer != "" {
		o.peer = peer
	}
	o.emit("transfer:waiting", map[string]interface{}{"peer": o.peer})
}

func (o *wailsObserver) SessionStart(dir transfer.Direction, peer string, count int, totalBytes int64) {
	// Prefer the friendly device name the core resolved over the dialed address.
	if peer != "" {
		o.peer = peer
	}
	o.emit("transfer:start", map[string]interface{}{"dir": dir.String(), "peer": o.peer, "fileCount": count, "totalBytes": totalBytes})
}

func (o *wailsObserver) FileStart(dir transfer.Direction, index, total int, name string, size int64) {
	o.emit("file:start", map[string]interface{}{"dir": dir.String(), "index": index, "total": total, "name": name, "size": size})
}

func (o *wailsObserver) Progress(p transfer.FileProgress) {
	o.emit("file:progress", map[string]interface{}{
		"dir": p.Dir.String(), "index": p.Index, "total": p.Total, "name": p.Name,
		"bytes": p.Bytes, "size": p.Size, "bytesPerSec": p.BytesPerSec,
	})
}

func (o *wailsObserver) FileDone(r transfer.FileResult) {
	errStr := ""
	if r.Err != nil {
		errStr = r.Err.Error()
	}
	o.emit("file:done", map[string]interface{}{
		"dir": r.Dir.String(), "index": r.Index, "total": r.Total, "name": r.Name,
		"size": r.Size, "savedTo": r.SavedTo, "verified": r.Verified, "error": errStr,
	})
}

func (o *wailsObserver) SessionEnd(dir transfer.Direction, peer string, err error) {
	errStr := ""
	if err != nil {
		errStr = err.Error()
	}
	o.emit("transfer:end", map[string]interface{}{"dir": dir.String(), "peer": o.peer, "error": errStr})
}

// ----- config persistence -----

func configPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = "."
	}
	return filepath.Join(dir, "Raven", "config.json")
}

func defaultSaveDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "Raven"
	}
	return filepath.Join(home, "Downloads", "Raven")
}

func loadConfig() Config {
	var cfg Config
	if b, err := os.ReadFile(configPath()); err == nil {
		_ = json.Unmarshal(b, &cfg)
	}
	if cfg.DeviceName == "" {
		if h, err := os.Hostname(); err == nil {
			cfg.DeviceName = h
		} else {
			cfg.DeviceName = "Raven"
		}
	}
	if cfg.SaveDir == "" {
		cfg.SaveDir = defaultSaveDir()
	}
	if cfg.Theme == "" {
		cfg.Theme = "system"
	}
	if cfg.Port == 0 {
		cfg.Port = transfer.DefaultTCP
	}
	return cfg
}

func (a *App) saveConfig() {
	a.mu.Lock()
	cfg := a.cfg
	a.mu.Unlock()
	p := configPath()
	_ = os.MkdirAll(filepath.Dir(p), 0o700)
	if b, err := json.MarshalIndent(cfg, "", "  "); err == nil {
		_ = os.WriteFile(p, b, 0o600) // owner-only; the config dir also holds the identity key
	}
}
