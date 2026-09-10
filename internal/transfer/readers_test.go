package transfer

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// pairedIdentities returns two identities that have already paired with each
// other, so a transfer between them passes the fingerprint check.
func pairedIdentities(t *testing.T) (sender, recvr *Identity, senderStore, recvStore *PairedStore) {
	t.Helper()
	var err error
	sender, err = LoadOrCreateIdentity(t.TempDir(), "Sender")
	if err != nil {
		t.Fatal(err)
	}
	recvr, err = LoadOrCreateIdentity(t.TempDir(), "Receiver")
	if err != nil {
		t.Fatal(err)
	}
	senderStore = LoadPairedStore(filepath.Join(t.TempDir(), "s.json"))
	recvStore = LoadPairedStore(filepath.Join(t.TempDir(), "r.json"))
	_ = senderStore.Add(PairedDevice{Name: "Receiver", Fingerprint: recvr.FP})
	_ = recvStore.Add(PairedDevice{Name: "Sender", Fingerprint: sender.FP})
	return
}

func bytesSource(b []byte) func() (io.ReadCloser, error) {
	return func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(b)), nil }
}

func TestSendFilesFromReaders(t *testing.T) {
	sender, recvr, senderStore, recvStore := pairedIdentities(t)
	dstDir := t.TempDir()
	port := freePort(t)
	rcv := &Receiver{Dir: dstDir, Name: "Receiver", Port: port, Identity: recvr, Paired: recvStore, Concurrent: true}
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

	got, err := os.ReadFile(filepath.Join(dstDir, "a.bin"))
	if err != nil || !bytes.Equal(got, big) {
		t.Fatalf("a.bin did not arrive intact: %v", err)
	}
	got, err = os.ReadFile(filepath.Join(dstDir, "notes", "b.txt"))
	if err != nil || string(got) != "hello" {
		t.Fatalf("notes/b.txt did not arrive intact: %v", err)
	}
}

func TestSendFilesRejectsEmptyAndUnopenable(t *testing.T) {
	sender, _, senderStore, _ := pairedIdentities(t)
	if err := SendFiles(context.Background(), "127.0.0.1:1", nil, sender, senderStore, nil, time.Second); err == nil {
		t.Fatal("expected an error for an empty file list")
	}
	files := []SendFile{{Name: "x", Size: 1}} // no Open
	if err := SendFiles(context.Background(), "127.0.0.1:1", files, sender, senderStore, nil, time.Second); err == nil {
		t.Fatal("expected an error for a file without a source")
	}
}
