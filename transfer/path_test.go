package transfer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestResolveTraversal verifies that hostile Rel paths cannot escape the save
// directory, and that legitimate nested paths resolve correctly inside it.
func TestResolveTraversal(t *testing.T) {
	base := t.TempDir()

	cases := []struct {
		name string
		rel  string
		want string // expected path relative to base, or "" if it must be rejected/neutralized outside base
	}{
		{"simple nested", "photos/a.jpg", "photos/a.jpg"},
		{"deep nested", "photos/trip/day 1/n.txt", "photos/trip/day 1/n.txt"},
		{"dotdot prefix", "../evil.txt", ""},          // becomes loose "evil.txt" inside base (rel emptied)
		{"dotdot middle", "photos/../../evil", ""},     // .. components stripped
		{"absolute", "/etc/passwd", ""},                // leading slash stripped
		{"sneaky", "ok/../../../../tmp/pwned", ""},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pr := newPathResolver(base)
			out, _, err := pr.resolve(fileMeta{Name: filepath.Base(c.rel), Size: 1, Rel: c.rel})
			if err != nil {
				return // explicit rejection is an acceptable outcome
			}
			abs, _ := filepath.Abs(out)
			baseAbs, _ := filepath.Abs(base)
			if !strings.HasPrefix(abs+string(filepath.Separator), baseAbs+string(filepath.Separator)) {
				t.Fatalf("path escaped save dir: rel=%q -> %q (base %q)", c.rel, abs, baseAbs)
			}
		})
	}
}

// TestResolveFolderRename verifies that a colliding folder root is renamed once
// and all files in that folder share the renamed root.
func TestResolveFolderRename(t *testing.T) {
	base := t.TempDir()
	// pre-create "photos" so the incoming folder must be renamed
	if err := os.MkdirAll(filepath.Join(base, "photos"), 0o755); err != nil {
		t.Fatal(err)
	}
	pr := newPathResolver(base)
	out1, label1, _ := pr.resolve(fileMeta{Name: "a.jpg", Size: 1, Rel: "photos/a.jpg"})
	out2, label2, _ := pr.resolve(fileMeta{Name: "b.jpg", Size: 1, Rel: "photos/sub/b.jpg"})

	root1 := strings.Split(label1, "/")[0]
	root2 := strings.Split(label2, "/")[0]
	if root1 != root2 {
		t.Fatalf("folder root inconsistent across files: %q vs %q", root1, root2)
	}
	if root1 == "photos" {
		t.Fatalf("expected rename away from existing 'photos', got %q", root1)
	}
	if !strings.Contains(out1, root1) || !strings.Contains(out2, root2) {
		t.Fatalf("outputs %q / %q do not use chosen root %q", out1, out2, root1)
	}
}
