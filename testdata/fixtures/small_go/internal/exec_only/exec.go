// Package exec_only imports only os/exec so the side-effect table's
// `!excludes: os/exec` carve-out can be verified: this file must carry
// io:process but NOT io:filesystem.
package exec_only

import "os/exec"

// Run executes a no-op command to keep the import live.
func Run() error {
	return exec.Command("true").Run()
}
