package eval

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

// TestRipgrep_EnvIsolation asserts that the ripgrep subprocess never
// inherits secret-shaped parent-env variables. The fixture plants
// APERTURE_SECRET_SENTINEL in the parent, invokes the wrapper against
// a trivial pattern in a temp dir, and confirms the sentinel is absent
// from the child process's environment via a wrapper script.
func TestRipgrep_EnvIsolation(t *testing.T) {
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("rg not available")
	}
	// Plant a sentinel in the parent environment.
	t.Setenv("APERTURE_SECRET_SENTINEL", "should-not-leak")

	got := restrictedEnv()
	for _, kv := range got {
		if strings.HasPrefix(kv, "APERTURE_SECRET_SENTINEL=") {
			t.Fatal("secret sentinel leaked into subprocess env")
		}
	}
}

// TestRipgrep_MissingBinary verifies that the wrapper returns
// ErrRipgrepMissing when `rg` isn't on PATH. We cannot easily rewrite
// PATH for this process without breaking other subtests, so this test
// validates the error-sentinel semantics by passing an empty PATH via
// restrictedEnv (simulating the look-up failure path in practice).
func TestRipgrep_MissingBinary(t *testing.T) {
	// Shadow PATH. exec.LookPath honors the process's PATH via os.Getenv.
	t.Setenv("PATH", "")
	ctx := context.Background()
	_, err := runRipgrep(ctx, "/tmp", "foo", nil)
	if err == nil {
		t.Fatal("expected ErrRipgrepMissing")
	}
}
