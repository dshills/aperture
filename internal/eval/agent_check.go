package eval

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// AgentCheckOutcome enumerates the four outcomes v1.1 §7.1.1 /
// §7.5.1 recognize for an agent_check invocation.
type AgentCheckOutcome string

const (
	AgentCheckPass     AgentCheckOutcome = "pass"
	AgentCheckFail     AgentCheckOutcome = "fail"
	AgentCheckTimeout  AgentCheckOutcome = "timeout"   // fixture fail; eval continues
	AgentCheckNotFound AgentCheckOutcome = "not_found" // eval aborts (exit 1)
	// AgentCheckCanceled reports user/parent-context cancellation
	// (e.g. Ctrl+C). Distinct from AgentCheckFail so reports don't
	// misclassify a stopped run as a fixture failure. The caller
	// checks ctx.Err() to propagate the abort.
	AgentCheckCanceled AgentCheckOutcome = "canceled"
)

// AgentCheckResult is the per-invocation verdict. Outcome is the
// primary signal; DurationMS / Stdout / Stderr are informational.
// When Outcome is NotFound, the CLI aborts the whole eval run
// (§7.7 error table); every other outcome lets the run continue.
type AgentCheckResult struct {
	Outcome    AgentCheckOutcome
	ExitCode   int
	DurationMS int64
	Stdout     []byte
	Stderr     []byte
	Err        error
}

// AgentCheckEnv is the §7.1.1 env contract passed to every
// agent_check subprocess. The loadmode harness populates this per
// invocation from the in-flight plan artifacts.
type AgentCheckEnv struct {
	ManifestPath         string
	ManifestMarkdownPath string
	PromptPath           string
	TaskPath             string
	RepoRoot             string
	ManifestHash         string
	ApertureVersion      string
}

// agentCheckAllowlistedEnv returns the env-var names propagated
// from the parent process into the agent_check child. Everything
// else is stripped — §7.1.1 env contract + PATH so the child can
// find its own dependencies.
var agentCheckAllowlistedEnv = []string{"PATH"}

// RunAgentCheck invokes `command` with the §7.1.1 env-var set plus
// the agentCheckAllowlistedEnv passthroughs, waits up to `timeout`,
// and returns the structured outcome. The subprocess environment
// is an explicit allowlist — credential-bearing parent variables
// are NOT inherited.
func RunAgentCheck(ctx context.Context, command string, timeout time.Duration, env AgentCheckEnv) AgentCheckResult {
	start := time.Now()

	// Timeout context wraps the caller's ctx so SIGKILL fires at
	// `timeout` even when the eval harness's context has no deadline.
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.Command(command) //nolint:gosec // command is a user-declared fixture path; documented in §7.1.1 as untrusted
	cmd.Env = buildAgentCheckEnv(env)
	// Run from the fixture repo root so relative paths inside the
	// script resolve consistently with the env-var contract's
	// APERTURE_REPO_ROOT.
	if env.RepoRoot != "" {
		cmd.Dir = env.RepoRoot
	}
	// Put the child in its own process group. On timeout we kill
	// the entire group rather than just the direct child —
	// otherwise a grandchild like `sleep` spawned by the
	// fixture's /bin/sh wrapper inherits stdout/stderr, holds
	// the pipes open after the shell dies, and blocks cmd.Wait()
	// until it exits on its own.
	setProcessGroup(cmd)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return AgentCheckResult{
			Outcome:    AgentCheckNotFound,
			DurationMS: time.Since(start).Milliseconds(),
			Err:        err,
		}
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	var err error
	timedOut := false
	canceled := false
	var duration time.Duration
	select {
	case <-execCtx.Done():
		// Duration measured at the moment the context fires —
		// excludes the grace-period wait below so the reported
		// number reflects actual execution time, not cleanup.
		duration = time.Since(start)
		// Distinguish a deadline-exceeded (real fixture timeout)
		// from a parent-context cancellation (user Ctrl+C).
		// Canceled propagates upward as an error; timeout just
		// classifies the fixture as failed and the eval loop
		// continues. §7.1.1 pass/fail semantics.
		switch {
		case errors.Is(ctx.Err(), context.Canceled):
			canceled = true
		case errors.Is(execCtx.Err(), context.DeadlineExceeded):
			timedOut = true
		default:
			timedOut = true
		}
		killProcessGroup(cmd)
		// Bounded grace period so a D-state / zombie child can't
		// hang the eval harness indefinitely. SIGKILL on a live
		// process group normally releases within microseconds;
		// a 5s ceiling is generous and still small enough that
		// CI doesn't stall on a single misbehaving fixture.
		const killGrace = 5 * time.Second
		select {
		case <-done:
			// Wait() returned; pipes are closed.
		case <-time.After(killGrace):
			// Give up waiting. The Wait() goroutine uses a
			// buffered channel (cap=1) so it can still send
			// and exit cleanly once cmd.Wait eventually
			// returns; only an unkillable D-state child
			// would leak the goroutine here.
		}
	case err = <-done:
		duration = time.Since(start)
	}
	res := AgentCheckResult{
		DurationMS: duration.Milliseconds(),
		Stdout:     stdout.Bytes(),
		Stderr:     stderr.Bytes(),
		Err:        err,
	}

	if canceled {
		// Propagate the cancellation so the eval harness's
		// outer ctx.Err() check can short-circuit the run
		// instead of treating Ctrl+C as a fixture fail.
		res.Outcome = AgentCheckCanceled
		res.Err = ctx.Err()
		return res
	}
	if timedOut {
		res.Outcome = AgentCheckTimeout
		return res
	}

	if err == nil {
		res.Outcome = AgentCheckPass
		return res
	}

	// exec.ExitError signals a real subprocess that returned non-
	// zero. Any other error (executable not found, permission
	// denied, fork failed) is a configuration failure, which
	// §7.7 maps to an eval-run abort (exit 1).
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		res.ExitCode = ee.ExitCode()
		res.Outcome = AgentCheckFail
		return res
	}
	// PathError (from exec.LookPath) / fork/exec failure:
	// §7.1.1 "command-not-found at invocation time".
	res.Outcome = AgentCheckNotFound
	return res
}

// buildAgentCheckEnv assembles the restricted env slice. The
// APERTURE_* variables come from env; the allowlist passthroughs
// come from the parent process. Anything else is dropped.
func buildAgentCheckEnv(env AgentCheckEnv) []string {
	out := make([]string, 0, len(agentCheckAllowlistedEnv)+7)
	for _, k := range agentCheckAllowlistedEnv {
		if v, ok := os.LookupEnv(k); ok {
			out = append(out, k+"="+v)
		}
	}
	if env.ManifestPath != "" {
		out = append(out, "APERTURE_MANIFEST_PATH="+env.ManifestPath)
	}
	if env.ManifestMarkdownPath != "" {
		out = append(out, "APERTURE_MANIFEST_MARKDOWN_PATH="+env.ManifestMarkdownPath)
	}
	if env.PromptPath != "" {
		out = append(out, "APERTURE_PROMPT_PATH="+env.PromptPath)
	}
	if env.TaskPath != "" {
		out = append(out, "APERTURE_TASK_PATH="+env.TaskPath)
	}
	if env.RepoRoot != "" {
		out = append(out, "APERTURE_REPO_ROOT="+env.RepoRoot)
	}
	if env.ManifestHash != "" {
		out = append(out, "APERTURE_MANIFEST_HASH="+env.ManifestHash)
	}
	if env.ApertureVersion != "" {
		out = append(out, "APERTURE_VERSION="+env.ApertureVersion)
	}
	return out
}

// ResolveAgentCheckCommand returns the absolute path to the
// fixture's agent_check command. Relative paths resolve under the
// fixture's repo/ directory AND MUST stay within that tree — a
// `..`-bearing specifier that tries to escape the fixture is
// rejected so a malformed fixture YAML cannot execute arbitrary
// binaries elsewhere on the host. Absolute paths are taken as-is
// (the PLAN treats absolute `command` strings as a deliberate
// operator choice; fixture authors using absolute paths accept
// responsibility).
func ResolveAgentCheckCommand(repoDir, command string) (string, error) {
	if command == "" {
		return "", fmt.Errorf("agent_check command is empty")
	}
	if filepath.IsAbs(command) {
		return command, nil
	}
	joined := filepath.Join(repoDir, filepath.FromSlash(command))
	absRepo, err := filepath.Abs(repoDir)
	if err != nil {
		return "", fmt.Errorf("resolve repo: %w", err)
	}
	absJoined, err := filepath.Abs(joined)
	if err != nil {
		return "", fmt.Errorf("resolve command: %w", err)
	}
	// Stay-within-repo check. filepath.Rel returns a path
	// beginning with ".." when the target escapes the base;
	// explicit prefix / separator test avoids false positives
	// against sibling directories whose names share a prefix.
	rel, err := filepath.Rel(absRepo, absJoined)
	if err != nil {
		return "", fmt.Errorf("agent_check command outside fixture repo: %s", command)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("agent_check command must stay within fixture repo (got %q)", command)
	}
	return joined, nil
}
