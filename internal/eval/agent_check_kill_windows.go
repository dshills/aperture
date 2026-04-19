//go:build windows

package eval

import "os/exec"

// setProcessGroup is a no-op on Windows. Job-object grouping is
// possible but the §7.1.1 env contract is POSIX-centric; the v1.1
// fixture set targets Linux/macOS and we fall through to the
// direct-child kill.
func setProcessGroup(_ *exec.Cmd) {}

// killProcessGroup falls back to killing the direct child process.
func killProcessGroup(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Kill()
}
