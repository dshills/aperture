package agent

import "context"

// codexAdapter is the v1 built-in §7.10.3 adapter. Codex CLI invocation
// conventions mirror claude's: the merged prompt file carries the
// manifest and task together, and the adapter pipes it on stdin. If a
// user needs different flag shape they can override command / args in
// their .aperture.yaml agents block — this default stays minimal.
type codexAdapter struct{}

// Compile-time interface assertion. See claude.go for the same pattern
// on the claudeAdapter; this proves codexAdapter satisfies Adapter
// without waiting for a CLI call site to exercise the interface.
var _ Adapter = (*codexAdapter)(nil)

func (*codexAdapter) Invoke(ctx context.Context, req RunRequest) (int, error) {
	cmd := req.AgentConfig.Command
	if cmd == "" {
		cmd = "codex"
	}
	args := append([]string{}, req.AgentConfig.Args...)
	return invokeExternal(ctx, cmd, args, req, true /* pipeStdin */)
}
