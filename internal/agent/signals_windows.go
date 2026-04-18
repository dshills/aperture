//go:build windows

package agent

// RegisterCleanupOnSignal is a no-op on Windows. The §7.10.4.1 design
// explicitly accepts that signal-based cleanup is unavailable there and
// relies on (a) defer-based cleanup in the CLI and (b) the 24 h orphan
// sweep run at startup via SweepOrphanTempfiles to bound leak duration.
func RegisterCleanupOnSignal(fn func()) (cancel func()) {
	_ = fn
	return func() {}
}
