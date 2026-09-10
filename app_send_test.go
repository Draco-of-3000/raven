package main

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Draco-of-3000/raven/transfer"
)

// recorder keeps what a sender's UI would have been told.
type recorder struct {
	transfer.NopObserver
	mu   sync.Mutex
	ends []error
}

func (r *recorder) SessionEnd(_ transfer.Direction, _ string, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ends = append(r.ends, err)
}

func (r *recorder) collected() []error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]error(nil), r.ends...)
}

// TestDeclinedSendTellsTheSender is a regression test for a window that sat on "waiting for them to accept"
// forever. A decline arrives before the transfer starts, so the core never reports the end of a session it
// never began, and nothing cleared the waiting card.
func TestDeclinedSendTellsTheSender(t *testing.T) {
	sender, senderStore, recvr, recvStore := pairedPair(t)
	port := freeTCPPort(t)
	rcv := &transfer.Receiver{
		Dir: t.TempDir(), Name: "Receiver", Port: port, Identity: recvr, Paired: recvStore, Concurrent: true,
		Accept: func(string, string, []transfer.IncomingFile) bool { return false }, // the person says no
	}
	if err := rcv.Listen(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = rcv.Serve(ctx); close(done) }()
	t.Cleanup(func() { cancel(); _ = rcv.Close(); <-done })

	file := filepath.Join(t.TempDir(), "note.txt")
	if err := os.WriteFile(file, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	obs := &recorder{}
	target := net.JoinHostPort("127.0.0.1", itoa(port))
	err := sendAndReport(context.Background(), obs, target, []string{file}, sender, senderStore, 10*time.Second)
	if err != transfer.ErrDeclined {
		t.Fatalf("send returned %v, want a decline", err)
	}
	if got := obs.collected(); len(got) != 1 || got[0] != transfer.ErrDeclined {
		t.Fatalf("the sender was told %v, want exactly one decline", got)
	}
}

// TestSessionEndIsReportedOnce covers the guard: the core reports the end of a session it started, and the
// send path reports the failure that ended it, and the window must not hear about it twice.
func TestSessionEndIsReportedOnce(t *testing.T) {
	inner := &recorder{}
	obs := &onceObserver{Observer: inner}
	obs.SessionEnd(transfer.Sending, "peer", transfer.ErrDeclined)
	obs.SessionEnd(transfer.Sending, "peer", transfer.ErrNotPaired)
	if got := inner.collected(); len(got) != 1 || got[0] != transfer.ErrDeclined {
		t.Fatalf("the window heard %v, want only the first", got)
	}
}

// TestCancelStillReachesTheCore guards the wrapper: the core asks the observer it was handed for a cancel
// hook, and wrapping one must not swallow that.
func TestCancelStillReachesTheCore(t *testing.T) {
	inner := &cancelable{}
	obs := &onceObserver{Observer: inner}
	transfer.Observer(obs).(interface{ SetCancel(func()) }).SetCancel(func() {})
	if !inner.got {
		t.Fatal("the cancel hook never reached the observer underneath")
	}
}

type cancelable struct {
	transfer.NopObserver
	got bool
}

func (c *cancelable) SetCancel(func()) { c.got = true }

func pairedPair(t *testing.T) (*transfer.Identity, *transfer.PairedStore, *transfer.Identity, *transfer.PairedStore) {
	t.Helper()
	sdir, rdir := t.TempDir(), t.TempDir()
	sender, err := transfer.LoadOrCreateIdentity(sdir, "Sender")
	if err != nil {
		t.Fatal(err)
	}
	recvr, err := transfer.LoadOrCreateIdentity(rdir, "Receiver")
	if err != nil {
		t.Fatal(err)
	}
	senderStore := transfer.LoadPairedStore(filepath.Join(sdir, "paired.json"))
	recvStore := transfer.LoadPairedStore(filepath.Join(rdir, "paired.json"))
	_ = senderStore.Add(transfer.PairedDevice{Name: "Receiver", Fingerprint: recvr.FP})
	_ = recvStore.Add(transfer.PairedDevice{Name: "Sender", Fingerprint: sender.FP})
	return sender, senderStore, recvr, recvStore
}

func freeTCPPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
