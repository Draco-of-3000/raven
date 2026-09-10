// Package transfer implements Raven's direct device-to-device file transfer
// protocol over the LAN. It is standard-library only and reports progress and
// lifecycle through an Observer, so the same core drives both the CLI (which
// renders to stdout) and the GUI (which emits events).
//
// Wire protocol (GOXFER03): all connections run inside a mutual-TLS 1.3 session.
// After the handshake the sender writes the 8-byte magic, an intent byte
// (transfer or pair), then for a transfer: the file count, the full manifest
// (all meta frames), a one-byte accept/decline from the receiver, and finally
// the file bodies (each followed by a trailing SHA-256 and a one-byte ack).
package transfer

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"time"
)

// closeOnCancel closes c when ctx is cancelled, which unblocks any in-flight
// read/write on c. It returns a stop func to remove the watcher when the work
// finishes normally (call it via defer). This is how a cancelled transfer aborts
// promptly without plumbing deadlines through every read.
func closeOnCancel(ctx context.Context, c net.Conn) (stop func()) {
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = c.Close()
		case <-done:
		}
	}()
	return func() { close(done) }
}

const (
	// magicV3 is the only protocol: it runs INSIDE a mutual-TLS 1.3 session, read
	// after the handshake completes. v1/v2 (plaintext) are removed.
	magicV3       = "GOXFER03"
	DiscoveryPort = 51999 // UDP port for receiver discovery
	DefaultTCP    = 51888 // default TCP port for transfers
	discoverQuery = "GOXFER_DISCOVER?"
	discoverReply = "GOXFER_HERE"

	// intent byte, sent after magicV3, selects what the session does.
	intentTransfer byte = 0x01
	intentPair     byte = 0x02

	// Wire-safety bounds: a malicious peer supplies these counts/lengths, so we
	// reject absurd values before allocating, to prevent memory-exhaustion DoS.
	maxMetaLen   = 64 * 1024 // a single JSON meta frame
	maxFileCount = 100_000   // files per session
	maxNameWire  = 256       // device name length on the wire (pairing)

	maxSessions = 16 // concurrent receive sessions cap
)

// idleTimeout bounds the small framed control reads/writes (handshake, meta,
// checksum, ack). It is a var, not a const, so tests can shrink it to prove that
// a long batch survives past it. The bulk file body is deliberately NOT bounded
// by this (see handleTransfer); cancellation handles a stalled body.
var idleTimeout = 60 * time.Second

// fileMeta is the JSON header describing a single file. Rel is the file's path
// relative to a dropped folder's parent (forward-slash separated, e.g.
// "photos/trip/img1.jpg"); it is empty for a plain file. Rel is added with
// `omitempty` so the frame stays byte-compatible with v1 receivers that only
// read Name and Size.
type fileMeta struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
	Rel  string `json:"rel,omitempty"`
}

// IncomingFile is one entry in a v2 manifest, shown to the user before accepting.
// Rel carries the folder-relative path (empty for a loose file) so the UI can
// group a folder's files together.
type IncomingFile struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
	Rel  string `json:"rel,omitempty"`
}

func readUint32(r io.Reader) (uint32, error) {
	b := make([]byte, 4)
	if _, err := io.ReadFull(r, b); err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint32(b), nil
}

func writeUint32(w io.Writer, v uint32) error {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, v)
	_, err := w.Write(b)
	return err
}

// equal compares two byte slices without early exit on the first difference.
func equal(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var diff byte
	for i := range a {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}
