package eval

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// ErrRipgrepMissing is returned when the `rg` binary cannot be resolved
// on PATH. Callers translate this to exit 1 per §7.7.
var ErrRipgrepMissing = errors.New("rg binary not found on PATH")

// runRipgrep invokes `rg` with a restricted environment (§PLAN: only
// PATH and LANG are propagated, no inherited secrets). Returns the
// combined stdout bytes. stderr is captured and included in the error
// string on non-zero exit.
func runRipgrep(ctx context.Context, repoRoot, pattern string, excludes []string) ([]byte, error) {
	rgPath, err := exec.LookPath("rg")
	if err != nil {
		return nil, ErrRipgrepMissing
	}

	args := []string{
		"--ignore-case",
		"--count-matches",
		"--no-heading",
		"--color=never",
	}
	for _, e := range excludes {
		args = append(args, "--glob", "!"+e)
	}
	// `--` separator prevents pattern or repoRoot values starting with
	// "-" from being interpreted as ripgrep flags (e.g., an anchor
	// "--version" in a task would otherwise be consumed as a flag).
	args = append(args, "--", pattern, repoRoot)

	cmd := exec.CommandContext(ctx, rgPath, args...) //nolint:gosec // rgPath from exec.LookPath
	cmd.Env = restrictedEnv()

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	// ripgrep exit codes: 0 = matches, 1 = no matches, 2+ = error.
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && ee.ExitCode() == 1 {
			return nil, nil // no matches is not an error
		}
		return nil, fmt.Errorf("rg: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// restrictedEnv returns the explicit allowlist environment used for the
// ripgrep subprocess. Only PATH and LANG are propagated (§PLAN).
func restrictedEnv() []string {
	out := make([]string, 0, 2)
	for _, key := range []string{"PATH", "LANG"} {
		if v, ok := lookupEnv(key); ok {
			out = append(out, key+"="+v)
		}
	}
	return out
}
