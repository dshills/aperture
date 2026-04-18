package eval

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFingerprintRepo_Deterministic(t *testing.T) {
	dir := t.TempDir()
	mustWriteTree(t, dir, map[string]string{
		"a.go":          "package a\n",
		"sub/b.go":      "package sub\n",
		"sub/c/deep.go": "package c\n",
	})
	a, err := FingerprintRepo(dir)
	if err != nil {
		t.Fatal(err)
	}
	b, err := FingerprintRepo(dir)
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Errorf("repeated fingerprint differs:\n  %s\n  %s", a, b)
	}
	if !strings.HasPrefix(a, "sha256:") || len(a) != len("sha256:")+64 {
		t.Errorf("fingerprint malformed: %q", a)
	}
}

func TestFingerprintRepo_SensitiveToContent(t *testing.T) {
	dir := t.TempDir()
	mustWriteTree(t, dir, map[string]string{"a.go": "package a\n"})
	before, err := FingerprintRepo(dir)
	if err != nil {
		t.Fatal(err)
	}
	mustWriteTree(t, dir, map[string]string{"a.go": "package a // changed\n"})
	after, err := FingerprintRepo(dir)
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Error("fingerprint did not change on content edit")
	}
}

func TestFingerprintRepo_RejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	mustWriteTree(t, dir, map[string]string{"a.go": "package a\n"})
	link := filepath.Join(dir, "link_to_a.go")
	if err := os.Symlink("a.go", link); err != nil {
		t.Skipf("symlink creation unsupported: %v", err)
	}
	_, err := FingerprintRepo(dir)
	if err == nil {
		t.Fatal("expected symlink rejection")
	}
}

func mustWriteTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, body := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
