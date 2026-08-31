package transfer

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
)

// ReceiverInfo describes a discovered receiver.
type ReceiverInfo struct {
	Name string `json:"name"`
	Addr string `json:"addr"` // host:port
}

// LocalIP returns the outbound interface IP without sending packets.
func LocalIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "127.0.0.1"
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).IP.String()
}

// LocalIPs returns the set of IP addresses bound to local interfaces. Used to
// recognise (and skip) our own machine in discovery results.
func LocalIPs() map[string]bool {
	out := map[string]bool{}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return out
	}
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok {
			out[ipnet.IP.String()] = true
		}
	}
	return out
}

// maxNameLen caps a single path component length (defense against absurd names).
const maxNameLen = 200

// sanitizeName strips any path components so a sender cannot write outside dir,
// removes characters that could mislead the user (control chars and Unicode
// bidirectional overrides that can disguise an extension, e.g. "photo<U+202E>gpj.exe"),
// and caps the length.
func sanitizeName(name string) string {
	name = CleanText(name)
	name = filepath.Base(filepath.FromSlash(name))
	name = strings.ReplaceAll(name, "..", "_")
	if name == "" || name == "." || name == string(filepath.Separator) {
		return "received.bin"
	}
	return capLen(name, maxNameLen)
}

// CleanText removes control characters and Unicode bidi-override/isolate code
// points from an untrusted string so it cannot spoof or corrupt a display. It is
// exported so the wire-facing layers (and tests) can sanitize device names too.
func CleanText(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\t' || r == '\n' || r == '\r':
			b.WriteRune(' ') // fold whitespace controls to a space
		case r < 0x20 || r == 0x7f:
			// drop other C0 controls and DEL
		case r >= 0x80 && r <= 0x9f:
			// drop C1 controls
		case r >= 0x202a && r <= 0x202e:
			// drop LRE/RLE/PDF/LRO/RLO bidi overrides
		case r >= 0x2066 && r <= 0x2069:
			// drop LRI/RLI/FSI/PDI bidi isolates
		case r == 0x200e || r == 0x200f || r == 0x061c:
			// drop LRM/RLM/ALM marks
		default:
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

// capLen truncates s to at most n bytes, preserving the file extension when it
// can, so a very long name stays recognizable.
func capLen(s string, n int) string {
	if len(s) <= n {
		return s
	}
	ext := filepath.Ext(s)
	if len(ext) > 0 && len(ext) < 16 {
		keep := n - len(ext)
		if keep > 0 {
			return s[:keep] + ext
		}
	}
	return s[:n]
}

// uniquePath avoids clobbering an existing file: foo.txt becomes foo (1).txt.
func uniquePath(p string) string {
	if _, err := os.Stat(p); errors.Is(err, os.ErrNotExist) {
		return p
	}
	ext := filepath.Ext(p)
	base := strings.TrimSuffix(p, ext)
	for i := 1; ; i++ {
		cand := fmt.Sprintf("%s (%d)%s", base, i, ext)
		if _, err := os.Stat(cand); errors.Is(err, os.ErrNotExist) {
			return cand
		}
	}
}

// HumanBytes formats a byte count like "4.2 MiB".
func HumanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
