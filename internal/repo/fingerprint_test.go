package repo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFingerprint_StableAcrossRunsUnderIdenticalTree(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.txt"), "hello")
	writeFile(t, filepath.Join(dir, "b.md"), "# world")

	w1, err := Walk(WalkOptions{Root: dir})
	if err != nil {
		t.Fatal(err)
	}
	f1, err := Fingerprint(w1.Files, "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	w2, err := Walk(WalkOptions{Root: dir})
	if err != nil {
		t.Fatal(err)
	}
	f2, err := Fingerprint(w2.Files, "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if f1 != f2 {
		t.Fatalf("fingerprint unstable: %s vs %s", f1, f2)
	}
	if len(f1) != len("sha256:")+64 {
		t.Fatalf("unexpected fingerprint shape: %s", f1)
	}
}

func TestFingerprint_ChangesOnFileContentMutation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	writeFile(t, path, "v1")

	w1, err := Walk(WalkOptions{Root: dir})
	if err != nil {
		t.Fatal(err)
	}
	f1, err := Fingerprint(w1.Files, "1.0.0")
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte("v2-different"), 0o600); err != nil {
		t.Fatal(err)
	}
	w2, err := Walk(WalkOptions{Root: dir})
	if err != nil {
		t.Fatal(err)
	}
	f2, err := Fingerprint(w2.Files, "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if f1 == f2 {
		t.Fatalf("fingerprint failed to change after content mutation: %s", f1)
	}

	// Revert the file and assert the original hash comes back — this
	// protects against mtime-only fingerprints.
	if err := os.WriteFile(path, []byte("v1"), 0o600); err != nil {
		t.Fatal(err)
	}
	w3, err := Walk(WalkOptions{Root: dir})
	if err != nil {
		t.Fatal(err)
	}
	// Zero out mtime differences by forcing the FileEntry mtime field to
	// match the original; fingerprint must still track content only.
	for i := range w3.Files {
		w3.Files[i].MTime = w1.Files[i].MTime
	}
	f3, err := Fingerprint(w3.Files, "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if f3 != f1 {
		t.Fatalf("reverted fingerprint diverged: f1=%s f3=%s", f1, f3)
	}
}

func TestFingerprint_VersionIsInputToHash(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.txt"), "hello")
	w, err := Walk(WalkOptions{Root: dir})
	if err != nil {
		t.Fatal(err)
	}
	a, _ := Fingerprint(w.Files, "1.0.0")
	b, _ := Fingerprint(w.Files, "2.0.0")
	if a == b {
		t.Fatalf("aperture_version must contribute to fingerprint")
	}
}
