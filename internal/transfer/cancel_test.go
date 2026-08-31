package transfer

import (
	"net"
	"os"
	"path/filepath"
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

	err := r.receiveBody(cw, meta, 1, 1, NopObserver{}, pr)
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
