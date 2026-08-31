package transfer

import "time"

// Direction distinguishes a send from a receive in Observer callbacks.
type Direction int

const (
	Sending Direction = iota
	Receiving
)

func (d Direction) String() string {
	if d == Sending {
		return "send"
	}
	return "receive"
}

// FileProgress is reported repeatedly while a single file's bytes move.
type FileProgress struct {
	Dir         Direction
	Index       int
	Total       int
	Name        string
	Bytes       int64
	Size        int64
	BytesPerSec int64
}

// FileResult is reported once a single file finishes (success or failure).
type FileResult struct {
	Dir      Direction
	Index    int
	Total    int
	Name     string
	Size     int64
	SavedTo  string // receive side only; empty for sends
	Verified bool
	Err      error
}

// Observer receives lifecycle and progress callbacks from the core. Methods may
// be called from the goroutine running a transfer, so implementations must not
// block (the GUI implementation emits asynchronous events).
type Observer interface {
	// WaitingForAccept fires on the sender after the manifest is sent, while it
	// blocks waiting for the receiver to accept or decline.
	WaitingForAccept(peer string)
	SessionStart(dir Direction, peer string, fileCount int, totalBytes int64)
	FileStart(dir Direction, index, total int, name string, size int64)
	Progress(p FileProgress)
	FileDone(r FileResult)
	SessionEnd(dir Direction, peer string, err error)
}

// Canceler is an optional Observer extension. If an Observer implements it, the
// core hands it a cancel func for the live transfer so the UI can stop it. The
// core calls SetCancel(nil) when the transfer ends.
type Canceler interface {
	SetCancel(cancel func())
}

// NopObserver is a no-op Observer, usable as a safe default.
type NopObserver struct{}

func (NopObserver) WaitingForAccept(string)                      {}
func (NopObserver) SessionStart(Direction, string, int, int64)   {}
func (NopObserver) FileStart(Direction, int, int, string, int64) {}
func (NopObserver) Progress(FileProgress)                        {}
func (NopObserver) FileDone(FileResult)                          {}
func (NopObserver) SessionEnd(Direction, string, error)          {}

// progressWriter counts bytes and forwards throttled FileProgress to obs. It
// replaces the original stdout progress bar; rendering now lives in the caller.
type progressWriter struct {
	obs          Observer
	dir          Direction
	index, total int
	name         string
	size         int64
	written      int64
	start, last  time.Time
}

func (p *progressWriter) Write(b []byte) (int, error) {
	n := len(b)
	if p.start.IsZero() {
		p.start = time.Now()
	}
	p.written += int64(n)
	now := time.Now()
	if now.Sub(p.last) < 100*time.Millisecond && p.written < p.size {
		return n, nil
	}
	p.last = now
	var rate int64
	if el := time.Since(p.start).Seconds(); el > 0 {
		rate = int64(float64(p.written) / el)
	}
	p.obs.Progress(FileProgress{
		Dir: p.dir, Index: p.index, Total: p.total, Name: p.name,
		Bytes: p.written, Size: p.size, BytesPerSec: rate,
	})
	return n, nil
}
