package transfer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestReceiveBodyCleansPartialOnDrop verifies that when the connection drops
// mid-file (as a cancel does), the receiver removes the half-written file instead
// of leaving a corrupt partial behind.
func TestReceiveBodyCleansPartialOnDrop(t *testing.T) {
	dir := t.TempDir()
	r := &Receiver{Dir: dir}
	pr := newPathResolver(dir)

	// A conn that delivers a few bytes then closes early, simulating a dropped
	// connection partway through a 1 MiB file.
	cw := &shortConn{data: make([]byte, 1024)} // far fewer than the declared size
	meta := fileMeta{Name: "partial.bin", Size: 1 << 20}

	err := r.receiveBody(cw, meta, 1, 1, NopObserver{}, &dirSink{pr: pr})
	if err == nil {
		t.Fatal("expected an error when the connection drops mid-file")
	}
	if _, statErr := os.Stat(filepath.Join(dir, "partial.bin")); !os.IsNotExist(statErr) {
		t.Fatalf("partial file was left behind after a dropped transfer")
	}
}

// shortConn is a net.Conn whose Read returns a little data then a closed error,
// enough to drive receiveBody's copy to fail partway through.
type shortConn struct {
	data []byte
	off  int
}

func (c *shortConn) Read(p []byte) (int, error) {
	if c.off >= len(c.data) {
		return 0, os.ErrClosed // simulate the conn closing mid-stream
	}
	n := copy(p, c.data[c.off:])
	c.off += n
	return n, nil
}
func (c *shortConn) Write(p []byte) (int, error)      { return len(p), nil }
func (c *shortConn) Close() error                     { return nil }
func (c *shortConn) LocalAddr() net.Addr              { return testAddr{} }
func (c *shortConn) RemoteAddr() net.Addr             { return testAddr{} }
func (c *shortConn) SetDeadline(time.Time) error      { return nil }
func (c *shortConn) SetReadDeadline(time.Time) error  { return nil }
func (c *shortConn) SetWriteDeadline(time.Time) error { return nil }

type testAddr struct{}

func (testAddr) Network() string { return "test" }
func (testAddr) String() string  { return "test" }

// TestCancelMidSendReportsCancellation is a regression test for a bug found on a phone. Cancelling
// closes the connection to unblock the body write, and the send reported that closed connection's
// write error, so an app told the person their own cancel "didn't complete". A cancel while waiting
// for accept already reported the cancellation; a cancel mid-body must too.
func TestCancelMidSendReportsCancellation(t *testing.T) {
	sender, recvr, senderStore, recvStore := pairedIdentities(t)
	port := serveForCancel(t, &Receiver{Dir: t.TempDir(), Name: "Receiver", Identity: recvr, Paired: recvStore})

	obs := &cancelOnStart{ended: make(chan error, 1)}
	files := []SendFile{{Name: "big.bin", Size: 64 << 20, Open: slowSource(64 << 20)}}
	err := SendFiles(context.Background(), fmt.Sprintf("127.0.0.1:%d", port), files, sender, senderStore, obs, 10*time.Second)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("SendFiles returned %v, want a cancellation", err)
	}
	if got := obs.wait(t); !errors.Is(got, context.Canceled) {
		t.Fatalf("SessionEnd reported %v, want a cancellation", got)
	}
}

// TestCancelMidReceiveReportsCancellation covers the same bug on the receiving side, where the
// cancel closes the connection under the body read.
func TestCancelMidReceiveReportsCancellation(t *testing.T) {
	sender, recvr, senderStore, recvStore := pairedIdentities(t)
	obs := &cancelOnStart{ended: make(chan error, 1)}
	port := serveForCancel(t, &Receiver{
		Dir: t.TempDir(), Name: "Receiver", Identity: recvr, Paired: recvStore,
		NewObs: func(string) Observer { return obs },
	})

	files := []SendFile{{Name: "big.bin", Size: 64 << 20, Open: slowSource(64 << 20)}}
	if err := SendFiles(context.Background(), fmt.Sprintf("127.0.0.1:%d", port), files, sender, senderStore, NopObserver{}, 10*time.Second); err == nil {
		t.Fatal("the sender should see the transfer stop")
	}
	if got := obs.wait(t); !errors.Is(got, context.Canceled) {
		t.Fatalf("SessionEnd reported %v, want a cancellation", got)
	}
}

// serveForCancel starts r on a free port, accepting every transfer, and stops it when the test ends.
func serveForCancel(t *testing.T, r *Receiver) int {
	t.Helper()
	r.Port = freePort(t)
	r.Concurrent = true
	r.Accept = func(string, string, []IncomingFile) bool { return true }
	if err := r.Listen(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = r.Serve(ctx); close(done) }()
	t.Cleanup(func() { cancel(); _ = r.Close(); <-done })
	return r.Port
}

// cancelOnStart cancels its transfer the moment the session starts, then records how it ended.
type cancelOnStart struct {
	NopObserver
	mu     sync.Mutex
	cancel func()
	ended  chan error
}

func (o *cancelOnStart) SetCancel(c func()) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.cancel = c
}

func (o *cancelOnStart) SessionStart(Direction, string, int, int64) {
	o.mu.Lock()
	c := o.cancel
	o.mu.Unlock()
	if c != nil {
		c()
	}
}

func (o *cancelOnStart) SessionEnd(_ Direction, _ string, err error) { o.ended <- err }

func (o *cancelOnStart) wait(t *testing.T) error {
	t.Helper()
	select {
	case err := <-o.ended:
		return err
	case <-time.After(10 * time.Second):
		t.Fatal("the session never ended")
		return nil
	}
}

// slowSource hands out zeros a chunk per millisecond, so a transfer is still mid-body when it is cancelled.
func slowSource(size int64) func() (io.ReadCloser, error) {
	return func() (io.ReadCloser, error) { return io.NopCloser(io.LimitReader(slowZeros{}, size)), nil }
}

type slowZeros struct{}

func (slowZeros) Read(p []byte) (int, error) {
	time.Sleep(time.Millisecond)
	if len(p) > 32<<10 {
		p = p[:32<<10]
	}
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}
