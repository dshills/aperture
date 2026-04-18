package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
)

// interactivePromptSoftCap is a defensive size ceiling for the merged
// prompt passed as a CLI argument in `mode: interactive`. Unix ARG_MAX
// varies by platform (macOS is 256 KiB-ish for the combined argv+envp),
// so we refuse to pass a prompt larger than this through argv — users
// with big prompts should stick with the default non-interactive mode
// which pipes the prompt on stdin and has no such ceiling.
const interactivePromptSoftCap = 64 * 1024

// claudeAdapter is the v1 built-in §7.10.2 adapter. It invokes
// `claude --print --permission-mode bypassPermissions` with the merged
// prompt piped on stdin in the default (non-interactive) mode. The
// "interactive" mode passes the prompt as the initial message argument
// instead, matching how `claude <prompt>` behaves on a terminal.
type claudeAdapter struct{}

// Ensure claudeAdapter satisfies the Adapter interface at compile time.
// The compiler rejects this build if any method goes missing.
var _ Adapter = (*claudeAdapter)(nil)

func (*claudeAdapter) Invoke(ctx context.Context, req RunRequest) (int, error) {
	cmd := req.AgentConfig.Command
	if cmd == "" {
		cmd = "claude"
	}
	args := append([]string{}, req.AgentConfig.Args...)

	interactive := req.AgentConfig.Mode == "interactive"
	if interactive {
		// Interactive: claude <args> <prompt-contents-as-arg>. We read
		// the prompt file and pass the body through argv. Refuse when
		// the prompt exceeds the platform ARG_MAX budget to avoid
		// cryptic exec failures; users who routinely exceed the cap
		// should stay on the default non-interactive (stdin) mode.
		body, err := os.ReadFile(req.PromptPath) //nolint:gosec // adapter-verified path
		if err != nil {
			return 0, fmt.Errorf("read merged prompt: %w", err)
		}
		if len(body) > interactivePromptSoftCap {
			slog.Warn("merged prompt exceeds interactive-mode ARG_MAX safety cap — falling back to stdin pipe",
				"size", len(body), "cap", interactivePromptSoftCap)
			interactive = false
		} else {
			args = append(args, string(body))
		}
	}
	if !interactive {
		// Non-interactive (default): claude --print --permission-mode
		// bypassPermissions <args...>, with the merged prompt piped on
		// stdin. Callers that want to override this shape should declare
		// a custom adapter; these defaults are the §7.10.2 baseline.
		args = append([]string{"--print", "--permission-mode", "bypassPermissions"}, args...)
	}

	return invokeExternal(ctx, cmd, args, req, !interactive /* pipeStdin */)
}

// invokeExternal spawns cmd with args, exporting the APERTURE_* env,
// wiring stdout/stderr, optionally piping the merged prompt file on
// stdin, and returning the adapter's exit code. Pre-exec errors (exec
// not found, permission denied) are returned as a non-nil error so the
// CLI can map them to exit code 12.
func invokeExternal(ctx context.Context, cmd string, args []string, req RunRequest, pipeStdin bool) (int, error) {
	ec := exec.CommandContext(ctx, cmd, args...) //nolint:gosec // command resolved via user config
	ec.Env = append(os.Environ(), apertureEnv(req)...)
	if req.Stdout != nil {
		ec.Stdout = req.Stdout
	} else {
		ec.Stdout = os.Stdout
	}
	if req.Stderr != nil {
		ec.Stderr = req.Stderr
	} else {
		ec.Stderr = os.Stderr
	}
	switch {
	case req.Stdin != nil:
		ec.Stdin = req.Stdin
	case pipeStdin && req.PromptPath != "":
		f, err := os.Open(req.PromptPath) //nolint:gosec // adapter-verified path
		if err != nil {
			return 0, fmt.Errorf("open merged prompt: %w", err)
		}
		defer func() { _ = f.Close() }()
		ec.Stdin = f
	}

	if err := ec.Start(); err != nil {
		// exec.ErrNotFound is the normalized shape for "command not
		// found"; other start errors (permission denied, E2BIG, etc.)
		// also land here. In every case the adapter never ran, so we
		// surface a non-nil error and the CLI returns exit code 12.
		return 0, fmt.Errorf("start %q: %w", cmd, err)
	}
	if waitErr := ec.Wait(); waitErr != nil {
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			// Adapter ran and exited non-zero. Propagate its code.
			return exitErr.ExitCode(), nil
		}
		return 0, fmt.Errorf("wait %q: %w", cmd, waitErr)
	}
	return 0, nil
}
