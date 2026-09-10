package transfer

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestSlowAcceptDoesNotTimeOutTheBody is a regression test for a bug found on a phone. The receiver
// set a per-frame read deadline while reading the manifest and never cleared it, so when the person
// took longer than idleTimeout to accept, the body read failed with "i/o timeout" and the sender saw
// "connection reset by peer" after sending every byte.
func TestSlowAcceptDoesNotTimeOutTheBody(t *testing.T) {
	old := idleTimeout
	idleTimeout = 200 * time.Millisecond
	defer func() { idleTimeout = old }()

	sender, recvr, senderStore, recvStore := pairedIdentities(t)
	dstDir := t.TempDir()
	port := freePort(t)
	rcv := &Receiver{
		Dir: dstDir, Name: "Receiver", Port: port, Identity: recvr, Paired: recvStore, Concurrent: true,
		// A person who takes longer than the idle timeout to tap Accept.
		Accept: func(string, string, []IncomingFile) bool { time.Sleep(3 * idleTimeout); return true },
	}
	if err := rcv.Listen(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = rcv.Serve(ctx); close(done) }()
	// Stop the receiver and wait for it before the idleTimeout restore above runs, so nothing races.
	defer func() { cancel(); _ = rcv.Close(); <-done }()

	body := bytes.Repeat([]byte("raven"), 40_000)
	files := []SendFile{{Name: "slow.bin", Size: int64(len(body)), Open: bytesSource(body)}}
	target := fmt.Sprintf("127.0.0.1:%d", port)
	if err := SendFiles(context.Background(), target, files, sender, senderStore, NopObserver{}, 10*time.Second); err != nil {
		t.Fatalf("a slow accept broke the transfer: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dstDir, "slow.bin"))
	if err != nil || !bytes.Equal(got, body) {
		t.Fatalf("slow.bin did not arrive intact: %v", err)
	}
}
