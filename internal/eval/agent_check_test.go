package eval

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// writeScript writes a POSIX shell script at path with the given
// body and returns the absolute path. Skips the test on Windows
// where the #!/bin/sh execution model doesn't apply out of the box.
func writeScript(t *testing.T, name, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("agent_check tests use POSIX /bin/sh scripts; Windows path not exercised here")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestAgentCheck_PassExitZero(t *testing.T) {
	script := writeScript(t, "ok.sh", "exit 0")
	r := RunAgentCheck(context.Background(), script, 10*time.Second, AgentCheckEnv{})
	if r.Outcome != AgentCheckPass {
		t.Fatalf("outcome=%q, want %q (err=%v stderr=%s)", r.Outcome, AgentCheckPass, r.Err, string(r.Stderr))
	}
}

func TestAgentCheck_FailNonZero(t *testing.T) {
	script := writeScript(t, "fail.sh", "exit 7")
	r := RunAgentCheck(context.Background(), script, 10*time.Second, AgentCheckEnv{})
	if r.Outcome != AgentCheckFail {
		t.Fatalf("outcome=%q, want %q", r.Outcome, AgentCheckFail)
	}
	if r.ExitCode != 7 {
		t.Errorf("exit code = %d, want 7", r.ExitCode)
	}
}

func TestAgentCheck_TimeoutSIGKILLsAndReports(t *testing.T) {
	script := writeScript(t, "hang.sh", "sleep 10")
	r := RunAgentCheck(context.Background(), script, 100*time.Millisecond, AgentCheckEnv{})
	if r.Outcome != AgentCheckTimeout {
		t.Fatalf("outcome=%q, want %q", r.Outcome, AgentCheckTimeout)
	}
	// Deadline-bounded: must complete in well under the script's
	// nominal 10s sleep (the context-cancel killed it).
	if r.DurationMS > 5000 {
		t.Errorf("duration=%dms — timeout didn't fire fast enough", r.DurationMS)
	}
}

func TestAgentCheck_NotFoundReportsCleanly(t *testing.T) {
	r := RunAgentCheck(context.Background(), "/definitely/does/not/exist", 1*time.Second, AgentCheckEnv{})
	if r.Outcome != AgentCheckNotFound {
		t.Fatalf("outcome=%q, want %q", r.Outcome, AgentCheckNotFound)
	}
}

// TestAgentCheck_EnvIsolation_ParentSecretsStripped: PLAN §PLAN
// Phase 6 requires the subprocess environment to be an explicit
// allowlist. Plants a sentinel secret in the parent env and
// asserts it does NOT appear in the child's environment.
func TestAgentCheck_EnvIsolation_ParentSecretsStripped(t *testing.T) {
	t.Setenv("APERTURE_SECRET_SENTINEL", "should-not-leak")
	t.Setenv("FAKE_PROVIDER_TOKEN_SENTINEL", "should-not-leak")

	dir := t.TempDir()
	out := filepath.Join(dir, "env.out")
	script := writeScript(t, "dump_env.sh", "env > "+out+"; exit 0")

	r := RunAgentCheck(context.Background(), script, 10*time.Second, AgentCheckEnv{
		ManifestPath: "/tmp/foo.json",
		RepoRoot:     dir,
	})
	if r.Outcome != AgentCheckPass {
		t.Fatalf("script should pass; outcome=%q stderr=%s", r.Outcome, string(r.Stderr))
	}
	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	dump := string(body)
	if strings.Contains(dump, "APERTURE_SECRET_SENTINEL") {
		t.Errorf("sentinel leaked into child env:\n%s", dump)
	}
	if strings.Contains(dump, "FAKE_PROVIDER_TOKEN_SENTINEL") {
		t.Errorf("token sentinel leaked:\n%s", dump)
	}
	// Positive: the APERTURE_* allowlist variables MUST be present.
	if !strings.Contains(dump, "APERTURE_MANIFEST_PATH=/tmp/foo.json") {
		t.Errorf("APERTURE_MANIFEST_PATH missing:\n%s", dump)
	}
	if !strings.Contains(dump, "APERTURE_REPO_ROOT="+dir) {
		t.Errorf("APERTURE_REPO_ROOT missing:\n%s", dump)
	}
}
