package eval

import (
	"os"
	"path/filepath"
	"testing"
)

// FuzzFixtureLoad feeds arbitrary bytes into LoadFixture. The invariant:
// malformed YAML MUST produce a structured error (exit 2), never a
// panic. Per PLAN §11.6.
func FuzzFixtureLoad(f *testing.F) {
	f.Add([]byte(`
name: x
task: "hi"
budget: 1
model: m
repo_fingerprint: sha256:0000000000000000000000000000000000000000000000000000000000000000
`))
	f.Add([]byte(`:::: not yaml`))
	f.Add([]byte(``))
	f.Add([]byte("name:\n  -\n"))
	f.Add(make([]byte, 4096)) // all nulls

	f.Fuzz(func(t *testing.T, b []byte) {
		dir := t.TempDir()
		p := filepath.Join(dir, "fuzz.eval.yaml")
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("LoadFixture panicked on fuzz input: %v", r)
			}
		}()
		_, _ = LoadFixture(p)
	})
}
