package agent

import (
	"context"
	"fmt"
)

// customAdapter handles user-declared agents from .aperture.yaml. Unlike
// claude/codex which own their CLI convention, custom adapters treat
// the configured command as authoritative: the declared command and
// args run as-is, with the task path appended when pass_task_as_arg is
// true, and the merged prompt is NOT piped on stdin by default — users
// who want that shape should say so via their wrapper script.
type customAdapter struct{}

var _ Adapter = (*customAdapter)(nil)

func (*customAdapter) Invoke(ctx context.Context, req RunRequest) (int, error) {
	cmd := req.AgentConfig.Command
	if cmd == "" {
		return 0, fmt.Errorf("agent command is empty; declare agents.<name>.command in .aperture.yaml")
	}
	args := append([]string{}, req.AgentConfig.Args...)

	// §9.1.2: custom adapters default pass_task_as_arg=true — the
	// resolved task path is appended as the final positional argument.
	// The built-in claude/codex defaults flip this to false because
	// they carry the task via the merged prompt file; custom adapters
	// opt in by leaving the config default in place.
	appendTask := true
	if req.AgentConfig.PassTaskAsArg != nil {
		appendTask = *req.AgentConfig.PassTaskAsArg
	}
	if appendTask && req.TaskPath != "" {
		args = append(args, req.TaskPath)
	}

	return invokeExternal(ctx, cmd, args, req, false /* pipeStdin */)
}
