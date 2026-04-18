package eval

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var osWriteFile = os.WriteFile

func TestLoadFixture_TrivialPass(t *testing.T) {
	abs, err := filepath.Abs("../../testdata/eval/trivial-pass/trivial-pass.eval.yaml")
	if err != nil {
		t.Fatal(err)
	}
	fx, err := LoadFixture(abs)
	if err != nil {
		t.Fatalf("LoadFixture: %v", err)
	}
	if fx.Name != "trivial-pass" {
		t.Errorf("Name=%q, want trivial-pass", fx.Name)
	}
	if fx.Budget == 0 {
		t.Error("Budget not loaded")
	}
	if fx.Task == "" {
		t.Error("Task not loaded")
	}
	if !strings.HasPrefix(fx.RepoFingerprint, "sha256:") {
		t.Errorf("RepoFingerprint missing sha256: prefix: %q", fx.RepoFingerprint)
	}
}

func TestLoadFixtures_Deterministic(t *testing.T) {
	dir, err := filepath.Abs("../../testdata/eval")
	if err != nil {
		t.Fatal(err)
	}
	a, err := LoadFixtures(dir)
	if err != nil {
		t.Fatal(err)
	}
	b, err := LoadFixtures(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != len(b) {
		t.Fatalf("fixture count differs: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i].Name != b[i].Name {
			t.Errorf("order differs at %d: %q vs %q", i, a[i].Name, b[i].Name)
		}
	}
}

func TestLoadFixture_RejectsBothTaskAndTaskFile(t *testing.T) {
	bad := writeTempYAML(t, `
name: x
task: "inline"
task_file: "foo.md"
budget: 1
model: m
repo_fingerprint: sha256:0000000000000000000000000000000000000000000000000000000000000000
`)
	_, err := LoadFixture(bad)
	if err == nil {
		t.Fatal("expected error for both task and task_file set")
	}
	if !strings.Contains(err.Error(), "exactly one") {
		t.Errorf("error message should mention 'exactly one', got: %v", err)
	}
}

func TestLoadFixture_RejectsNeitherTaskNorTaskFile(t *testing.T) {
	bad := writeTempYAML(t, `
name: x
budget: 1
model: m
repo_fingerprint: sha256:0000000000000000000000000000000000000000000000000000000000000000
`)
	_, err := LoadFixture(bad)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "exactly one") {
		t.Errorf("error should mention 'exactly one', got: %v", err)
	}
}

func TestLoadFixture_RejectsBareIntegerTimeout(t *testing.T) {
	bad := writeTempYAML(t, `
name: x
task: "hi"
budget: 1
model: m
repo_fingerprint: sha256:0000000000000000000000000000000000000000000000000000000000000000
agent_check:
  command: /bin/true
  timeout: 30
`)
	_, err := LoadFixture(bad)
	if err == nil {
		t.Fatal("expected error for bare integer timeout")
	}
}

func TestLoadFixture_RejectsUnknownKey(t *testing.T) {
	bad := writeTempYAML(t, `
name: x
task: "hi"
budget: 1
model: m
repo_fingerprint: sha256:0000000000000000000000000000000000000000000000000000000000000000
bogus_extra: true
`)
	_, err := LoadFixture(bad)
	if err == nil {
		t.Fatal("expected error for unknown key")
	}
}

func writeTempYAML(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "fixture.eval.yaml")
	if err := writeFile(p, []byte(body)); err != nil {
		t.Fatal(err)
	}
	return p
}

// writeFile is a tiny wrapper to keep test files lint-clean.
func writeFile(path string, b []byte) error {
	return osWriteFile(path, b, 0o644)
}
