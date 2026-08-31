package transfer

import (
	"bytes"
	"crypto/sha256"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestAckSurvivesStaleDeadline reproduces the multi-file regression at its root:
// a write deadline left over from earlier in the session (the old code set one
// absolute deadline at handshake and never cleared it) had already elapsed by the
// time later files were acked, so the ack write failed and the batch broke.
//
// receiveBody must set its OWN fresh deadline for the ack write. We simulate the
// stale state by pre-setting a deadline in the past on the connection, then assert
// receiveBody still completes the file (ack write succeeds).
func TestAckSurvivesStaleDeadline(t *testing.T) {
	dir := t.TempDir()
	r := &Receiver{Dir: dir}
	pr := newPathResolver(dir)

	body := []byte("hello raven, this is one file in a long batch")
	sum := sha256Sum(body)

	// A conn that yields the body then the trailing checksum, captures the ack,
	// and starts with an ALREADY-EXPIRED deadline (the bug's stale state).
	c := &deadlineConn{
		readbuf:  bytes.NewBuffer(append(append([]byte{}, body...), sum...)),
		deadline: time.Now().Add(-time.Hour), // expired an hour ago
	}

	meta := fileMeta{Name: "batchfile.bin", Size: int64(len(body))}
	if err := r.receiveBody(c, meta, 3, 12, NopObserver{}, pr); err != nil {
		t.Fatalf("receiveBody failed with a stale deadline present (the bug): %v", err)
	}
	if len(c.written) != 1 || c.written[0] != 0x01 {
		t.Fatalf("expected a success ack (0x01) to be written, got %v", c.written)
	}
	got, err := os.ReadFile(filepath.Join(dir, "batchfile.bin"))
	if err != nil || !bytes.Equal(got, body) {
		t.Fatalf("file not written correctly: err=%v", err)
	}
}

func sha256Sum(b []byte) []byte {
	s := sha256.Sum256(b)
	return s[:]
}

// deadlineConn enforces a write deadline like a real socket: if a write happens
// while the deadline is in the past, it fails, UNLESS SetWriteDeadline was called
// to move it into the future first (which the fix does).
type deadlineConn struct {
	readbuf  *bytes.Buffer
	written  []byte
	deadline time.Time
}

func (c *deadlineConn) Read(p []byte) (int, error) {
	if c.readbuf.Len() == 0 {
		return 0, io.EOF
	}
	return c.readbuf.Read(p)
}
func (c *deadlineConn) Write(p []byte) (int, error) {
	if !c.deadline.IsZero() && time.Now().After(c.deadline) {
		return 0, os.ErrDeadlineExceeded // mimic a socket write past its deadline
	}
	c.written = append(c.written, p...)
	return len(p), nil
}
func (c *deadlineConn) Close() error                  { return nil }
func (c *deadlineConn) LocalAddr() net.Addr           { return testAddr{} }
func (c *deadlineConn) RemoteAddr() net.Addr          { return testAddr{} }
func (c *deadlineConn) SetDeadline(t time.Time) error { c.deadline = t; return nil }
func (c *deadlineConn) SetReadDeadline(time.Time) error { return nil }
func (c *deadlineConn) SetWriteDeadline(t time.Time) error { c.deadline = t; return nil }
