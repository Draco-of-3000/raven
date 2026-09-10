package transfer

import (
	"strings"
	"testing"
)

// TestCleanText verifies control characters and Unicode bidi overrides are
// stripped so a crafted filename cannot disguise its extension or break display.
func TestCleanText(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "report.pdf", "report.pdf"},
		{"newline folded", "a\nb", "a b"},
		{"nul dropped", "a\x00b", "ab"},
		{"del dropped", "a\x7fb", "ab"},
		{"rlo dropped", "photo‮gpj.exe", "photogpj.exe"},
		{"isolate dropped", "x⁦y⁩z", "xyz"},
		{"rlm dropped", "a‏b", "ab"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := CleanText(c.in); got != c.want {
				t.Fatalf("CleanText(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// TestSanitizeNameStripsBidi confirms the on-disk name sanitizer also removes the
// bidi override (the classic "photo<RLO>gpj.exe" disguise).
func TestSanitizeNameStripsBidi(t *testing.T) {
	got := sanitizeName("photo‮gpj.exe")
	if strings.ContainsRune(got, 0x202e) {
		t.Fatalf("sanitizeName left a bidi override in %q", got)
	}
}

// TestSASDeterministic verifies both pairing roles, given the same fingerprints
// and session salt, derive the identical 6-digit code (and a different salt or
// fingerprint changes it). It exercises the hashing directly via a fake state by
// checking the pure derivation is order-canonical: initiator/responder order is
// fixed, so both sides feed identical input.
func TestSASDeterministic(t *testing.T) {
	// ComputeSAS needs a tls.ConnectionState for the exporter; here we validate
	// the surrounding determinism by hashing identical inputs two ways. The full
	// end-to-end SAS match is covered by the CLI pairing round-trip.
	fpA := "aaaa1111"
	fpB := "bbbb2222"
	salt := []byte("fixed-salt-32-bytes-long-padding")

	code1 := sasFromParts(fpA, fpB, salt)
	code2 := sasFromParts(fpA, fpB, salt)
	if code1 != code2 {
		t.Fatalf("SAS not deterministic: %q vs %q", code1, code2)
	}
	if len(code1) != 6 {
		t.Fatalf("SAS must be 6 digits, got %q", code1)
	}
	// Different salt (different TLS session) must change the code.
	if sasFromParts(fpA, fpB, []byte("a-totally-different-salt-32-byte!")) == code1 {
		t.Fatalf("SAS did not change with a different salt")
	}
	// Swapped fingerprint order is a different code (initiator vs responder is fixed).
	if sasFromParts(fpB, fpA, salt) == code1 {
		t.Fatalf("SAS did not depend on fingerprint order")
	}
}
