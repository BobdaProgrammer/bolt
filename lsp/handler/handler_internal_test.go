package handler

import (
	"path/filepath"
	"testing"
)

// isWithinDir decides whether a document is part of the stdlib cache, which in
// turn switches on the GOSTDCALL identifier exemption. A false positive would
// silently suppress real "Identifier not found" errors.
func TestIsWithinDir(t *testing.T) {
	sep := string(filepath.Separator)
	root := sep + filepath.Join("cache", "bolt", "stdlib")

	cases := []struct {
		name      string
		dir, path string
		want      bool
	}{
		{"file directly inside", root, filepath.Join(root, "a.bolt"), true},
		{"file in nested dir", root, filepath.Join(root, "sub", "deeper", "a.bolt"), true},
		{"the directory itself", root, root, true},
		{"path with redundant elements", root, root + sep + "sub" + sep + ".." + sep + "a.bolt", true},
		{"sibling directory", root, sep + filepath.Join("cache", "bolt", "other", "a.bolt"), false},
		{"parent directory", root, sep + filepath.Join("cache", "bolt"), false},
		{"grandparent", root, sep + "cache", false},
		{"filesystem root", root, sep, false},
		{"shared string prefix but different dir", root, root + "-extra" + sep + "a.bolt", false},
		{"shared string prefix, same level", root, root + "2", false},
		{"escapes via ..", root, filepath.Join(root, "..", "evil.bolt"), false},
		{"child whose name starts with two dots", root, filepath.Join(root, "..hidden", "a.bolt"), true},
		{"child named like a dotfile", root, filepath.Join(root, ".git"), true},
		{"relative path vs absolute dir", root, filepath.Join("cache", "bolt", "stdlib", "a.bolt"), false},
		{"absolute path vs relative dir", filepath.Join("cache", "bolt", "stdlib"), filepath.Join(root, "a.bolt"), false},
		{"both relative, inside", filepath.Join("a", "b"), filepath.Join("a", "b", "c"), true},
		{"both relative, outside", filepath.Join("a", "b"), filepath.Join("a", "c"), false},
		{"empty path", root, "", false},
		{"empty dir and relative path", "", "a.bolt", true},
		{"both empty", "", "", true},
		{"trailing separator on dir", root + sep, filepath.Join(root, "a.bolt"), true},
		{"unicode directory", sep + filepath.Join("キャッシュ", "bolt"), sep + filepath.Join("キャッシュ", "bolt", "a.bolt"), true},
		{"spaces in directory", sep + filepath.Join("my cache", "bolt"), sep + filepath.Join("my cache", "bolt", "a b.bolt"), true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IsWithinDir(c.dir, c.path); got != c.want {
				t.Fatalf("IsWithinDir(%q, %q) = %v, want %v", c.dir, c.path, got, c.want)
			}
		})
	}
}
