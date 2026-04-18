package repo

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/dshills/aperture/internal/config"
)

func TestWalk_SortsFilesAndExclusions(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "z.md"), "z")
	writeFile(t, filepath.Join(dir, "a.md"), "a")
	writeFile(t, filepath.Join(dir, "m.md"), "m")

	res, err := Walk(WalkOptions{Root: dir, DefaultPatterns: config.DefaultExclusions()})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	paths := pathsOf(res.Files)
	want := []string{"a.md", "m.md", "z.md"}
	if !slices.Equal(paths, want) {
		t.Fatalf("files not sorted deterministically: got %v want %v", paths, want)
	}
}

func TestWalk_DefaultExclusionsHit(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "README.md"), "hi")
	if err := os.MkdirAll(filepath.Join(dir, "node_modules", "foo"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "node_modules", "foo", "index.js"), "x")

	res, err := Walk(WalkOptions{Root: dir, DefaultPatterns: config.DefaultExclusions()})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	for _, f := range res.Files {
		if f.Path == "node_modules/foo/index.js" {
			t.Fatalf("node_modules should have been excluded")
		}
	}
	var hasExclusion bool
	for _, e := range res.Exclusions {
		if e.Path == "node_modules/" && e.Reason == ExcludeDefaultPattern {
			hasExclusion = true
		}
	}
	if !hasExclusion {
		t.Fatalf("missing default-pattern exclusion for node_modules/: %v", res.Exclusions)
	}
}

func TestWalk_HiddenDirAllowList(t *testing.T) {
	dir := t.TempDir()
	// Not in allow list — should be excluded.
	if err := os.MkdirAll(filepath.Join(dir, ".secrets"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, ".secrets", "token"), "s")

	// In allow list — should be traversed.
	if err := os.MkdirAll(filepath.Join(dir, ".github", "workflows"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, ".github", "workflows", "ci.yml"), "ci")

	res, err := Walk(WalkOptions{Root: dir, DefaultPatterns: config.DefaultExclusions()})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	var sawCI, sawSecret bool
	for _, f := range res.Files {
		if f.Path == ".github/workflows/ci.yml" {
			sawCI = true
		}
		if f.Path == ".secrets/token" {
			sawSecret = true
		}
	}
	if !sawCI {
		t.Error(".github/workflows/ci.yml should be indexed (allow list)")
	}
	if sawSecret {
		t.Error(".secrets/token must not be indexed (hidden_dir)")
	}
}

func TestWalk_BinaryDetection(t *testing.T) {
	dir := t.TempDir()
	// NUL byte in first 8 KiB → binary.
	binPath := filepath.Join(dir, "image.bin")
	payload := append([]byte("header"), 0x00, 0x01, 0x02)
	if err := os.WriteFile(binPath, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	// Plain text survives.
	writeFile(t, filepath.Join(dir, "ok.txt"), "hello")

	res, err := Walk(WalkOptions{Root: dir, DefaultPatterns: config.DefaultExclusions()})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	for _, f := range res.Files {
		if f.Path == "image.bin" {
			t.Fatalf("binary file should not be indexed")
		}
	}
	var flagged bool
	for _, e := range res.Exclusions {
		if e.Path == "image.bin" && e.Reason == ExcludeBinary {
			flagged = true
		}
	}
	if !flagged {
		t.Fatalf("expected binary exclusion reason: %v", res.Exclusions)
	}
}

// SHA-256 and size are recorded during the walk; subsequent invocations
// must produce identical file-level data on an unchanged tree.
func TestWalk_StableHashesAndSizes(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.txt"), "hello world")
	first, err := Walk(WalkOptions{Root: dir})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Walk(WalkOptions{Root: dir})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Files) != len(second.Files) || first.Files[0].SHA256 != second.Files[0].SHA256 {
		t.Fatalf("walk not stable: %+v vs %+v", first.Files, second.Files)
	}
}

func writeFile(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func pathsOf(files []FileEntry) []string {
	out := make([]string, 0, len(files))
	for _, f := range files {
		out = append(out, f.Path)
	}
	return out
}
