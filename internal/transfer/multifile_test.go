package transfer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// freePort asks the OS for an unused TCP port and returns it.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return port
}

// TestMultiFileBatchAllArrive is a regression test for the deadline bug where a
// stale write deadline (set once at handshake) killed the per-file ack partway
// through a batch, so only the first few files of a 6+ batch arrived. It stands
// up a real paired TLS transfer over loopback and asserts every file lands intact.
func TestMultiFileBatchAllArrive(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	const n = 12
	want := map[string]string{} // filename -> sha256
	var paths []string
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("file-%02d.bin", i)
		p := filepath.Join(srcDir, name)
		// Varied sizes, a few hundred KB each, enough to be real frames.
		data := make([]byte, 200000+i*5000)
		for j := range data {
			data[j] = byte((i * 7) + j)
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		want[name] = hex.EncodeToString(sum[:])
		paths = append(paths, p)
	}

	// Two identities that have pre-paired with each other.
	sender, err := LoadOrCreateIdentity(t.TempDir(), "Sender")
	if err != nil {
		t.Fatal(err)
	}
	recvr, err := LoadOrCreateIdentity(t.TempDir(), "Receiver")
	if err != nil {
		t.Fatal(err)
	}
	senderStore := LoadPairedStore(filepath.Join(t.TempDir(), "s.json"))
	recvStore := LoadPairedStore(filepath.Join(t.TempDir(), "r.json"))
	_ = senderStore.Add(PairedDevice{Name: "Receiver", Fingerprint: recvr.FP})
	_ = recvStore.Add(PairedDevice{Name: "Sender", Fingerprint: sender.FP})

	// Bind an OS-assigned free port. Port 0 on the Receiver means "the default
	// 51888", which would collide with a running Raven, so pick a real free port.
	port := freePort(t)
	rcv := &Receiver{
		Dir: dstDir, Name: "Receiver", Port: port,
		Identity: recvr, Paired: recvStore, Concurrent: true,
	}
	if err := rcv.Listen(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go rcv.Serve(ctx)
	defer rcv.Close()

	// Shrink the control-read/write timeout so the batch's wall-clock easily
	// exceeds it. With the old bug (one stale handshake-time deadline applied to
	// writes), the per-file ack would fail once this elapsed and the batch would
	// break partway. With per-op deadlines + the body unbounded, all files arrive.
	old := idleTimeout
	idleTimeout = 150 * time.Millisecond
	defer func() { idleTimeout = old }()

	target := fmt.Sprintf("127.0.0.1:%d", port)
	if err := SendV3(context.Background(), target, paths, sender, senderStore, NopObserver{}, 10*time.Second); err != nil {
		t.Fatalf("SendV3 failed: %v", err)
	}

	// Every file must be present and byte-identical.
	for name, sum := range want {
		got, err := os.ReadFile(filepath.Join(dstDir, name))
		if err != nil {
			t.Fatalf("missing received file %s: %v", name, err)
		}
		gsum := sha256.Sum256(got)
		if hex.EncodeToString(gsum[:]) != sum {
			t.Fatalf("checksum mismatch for %s", name)
		}
	}
}
