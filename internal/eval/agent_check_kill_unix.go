//go:build !windows

package eval

import (
	"os/exec"
	"syscall"
)

// setProcessGroup configures cmd so the child gets its own process
// group. Paired with killProcessGroup on timeout, this guarantees
// that grandchildren (e.g. a `sleep` spawned by the fixture's
// /bin/sh script) die alongside the script — otherwise
// exec.CommandContext's SIGKILL reaches only the direct child and
// the grandchild keeps stdout/stderr open, blocking cmd.Run()
// until the grandchild exits on its own.
func setProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// killProcessGroup sends SIGKILL to the entire process group led
// by cmd's direct child. Negative PID semantics: `kill -SIGKILL -pgid`.
//
// Defensive pgid bounds check: `kill -0 ...` targets the caller's
// own process group (killing the aperture harness), and `kill -1
// ...` targets every process on the system except init.
// setProcessGroup forces Setpgid=true so the child is the leader
// of a fresh group whose pgid == child.PID (always >= 2 on any
// realistic host), but the belt-and-suspenders check below
// protects against container edge cases where those guarantees
// might not hold.
func killProcessGroup(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil || pgid <= 1 {
		// Fallback: kill the direct child only — avoids the
		// `kill -0` / `kill -1` footgun when pgid is degenerate.
		_ = cmd.Process.Kill()
		return
	}
	_ = syscall.Kill(-pgid, syscall.SIGKILL)
}
