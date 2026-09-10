package transfer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// memSink is a FileSink that keeps files in memory and records lifecycle calls.
type memSink struct {
	mu        sync.Mutex
	files     map[string]*bytes.Buffer
	committed []string
	discarded []string
}

func newMemSink() *memSink { return &memSink{files: map[string]*bytes.Buffer{}} }

func (s *memSink) Create(name, rel string, size int64) (FileTarget, error) {
	label := name
	if rel != "" {
		label = rel
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	buf := &bytes.Buffer{}
	s.files[label] = buf
	return &memTarget{sink: s, label: label, buf: buf}, nil
}

type memTarget struct {
	sink  *memSink
	label string
	buf   *bytes.Buffer
}

func (t *memTarget) Write(p []byte) (int, error) { return t.buf.Write(p) }
func (t *memTarget) Label() string               { return t.label }
func (t *memTarget) Commit() (string, error) {
	t.sink.mu.Lock()
	defer t.sink.mu.Unlock()
	t.sink.committed = append(t.sink.committed, t.label)
	return "mem://" + t.label, nil
}
func (t *memTarget) Discard() error {
	t.sink.mu.Lock()
	defer t.sink.mu.Unlock()
	t.sink.discarded = append(t.sink.discarded, t.label)
	delete(t.sink.files, t.label)
	return nil
}

// doneRecorder captures FileDone results so a test can check SavedTo.
type doneRecorder struct {
	NopObserver
	mu   sync.Mutex
	done []FileResult
}

func (d *doneRecorder) FileDone(r FileResult) {
	d.mu.Lock()
	d.done = append(d.done, r)
	d.mu.Unlock()
}

func TestReceiveIntoSink(t *testing.T) {
	sender, recvr, senderStore, recvStore := pairedIdentities(t)
	sink := newMemSink()
	rec := &doneRecorder{}
	port := freePort(t)
	rcv := &Receiver{
		Name: "Receiver", Port: port, Identity: recvr, Paired: recvStore, Concurrent: true,
		NewSink: func(string) FileSink { return sink },
		NewObs:  func(string) Observer { return rec },
	}
	if err := rcv.Listen(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go rcv.Serve(ctx)
	defer rcv.Close()

	big := bytes.Repeat([]byte("raven"), 100_000)
	files := []SendFile{
		{Name: "a.bin", Size: int64(len(big)), Open: bytesSource(big)},
		{Name: "b.txt", Rel: "notes/b.txt", Size: 5, Open: bytesSource([]byte("hello"))},
	}
	target := fmt.Sprintf("127.0.0.1:%d", port)
	if err := SendFiles(context.Background(), target, files, sender, senderStore, NopObserver{}, 10*time.Second); err != nil {
		t.Fatalf("SendFiles failed: %v", err)
	}

	sink.mu.Lock()
	defer sink.mu.Unlock()
	if !bytes.Equal(sink.files["a.bin"].Bytes(), big) {
		t.Fatal("a.bin did not arrive intact")
	}
	if sink.files["notes/b.txt"].String() != "hello" {
		t.Fatal("notes/b.txt did not arrive intact")
	}
	if strings.Join(sink.committed, ",") != "a.bin,notes/b.txt" {
		t.Fatalf("committed = %v", sink.committed)
	}
	if len(sink.discarded) != 0 {
		t.Fatalf("unexpected discards: %v", sink.discarded)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.done) != 2 || rec.done[0].SavedTo != "mem://a.bin" || !rec.done[1].Verified {
		t.Fatalf("FileDone results = %+v", rec.done)
	}
}

// bodyWithChecksum serves a body followed by a checksum, then reports closed.
func bodyWithChecksum(body, sum []byte) *shortConn {
	return &shortConn{data: append(append([]byte{}, body...), sum...)}
}

func TestReceiveBodyDiscardsOnChecksumMismatch(t *testing.T) {
	sink := newMemSink()
	r := &Receiver{}
	body := []byte("the quick brown fox")
	wrong := make([]byte, sha256.Size) // all zeros: never the real digest
	conn := bodyWithChecksum(body, wrong)

	err := r.receiveBody(conn, fileMeta{Name: "fox.txt", Size: int64(len(body))}, 1, 1, NopObserver{}, sink)
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("expected a checksum mismatch error, got %v", err)
	}
	if len(sink.discarded) != 1 || sink.discarded[0] != "fox.txt" || len(sink.committed) != 0 {
		t.Fatalf("discarded=%v committed=%v", sink.discarded, sink.committed)
	}
}

func TestReceiveBodyDiscardsOnDrop(t *testing.T) {
	sink := newMemSink()
	r := &Receiver{}
	conn := &shortConn{data: make([]byte, 1024)} // far fewer than declared

	err := r.receiveBody(conn, fileMeta{Name: "partial.bin", Size: 1 << 20}, 1, 1, NopObserver{}, sink)
	if err == nil {
		t.Fatal("expected an error when the connection drops mid-file")
	}
	if len(sink.discarded) != 1 || len(sink.committed) != 0 {
		t.Fatalf("discarded=%v committed=%v", sink.discarded, sink.committed)
	}
}
