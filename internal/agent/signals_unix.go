//go:build !windows

package agent

import (
	"os"
	"os/signal"
	"sync"
	"syscall"
)

// RegisterCleanupOnSignal arranges for fn to run exactly once when the
// process receives SIGINT, SIGTERM, or SIGHUP. The returned cancel
// function detaches the handler and releases the os/signal channel so
// tests can swap handlers in and out without leaking goroutines.
//
// The handler does NOT itself exit — it runs cleanup and re-raises the
// original signal so the calling shell sees the conventional exit
// status (128+signum). Callers that also register deferred cleanup
// will see their defer run during unwind.
func RegisterCleanupOnSignal(fn func()) (cancel func()) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	var once sync.Once
	done := make(chan struct{})
	go func() {
		select {
		case sig := <-ch:
			once.Do(fn)
			// Stop listening for further signals so the re-raise below
			// actually terminates the process instead of looping back
			// through this handler.
			signal.Stop(ch)
			// Re-raise the signal so the caller's parent shell observes
			// the canonical exit status. If the process is still alive
			// after this (e.g. SIGINT being ignored), the caller's own
			// return path will exit normally.
			_ = syscall.Kill(os.Getpid(), sig.(syscall.Signal))
		case <-done:
			signal.Stop(ch)
		}
	}()

	return func() {
		once.Do(func() {}) // prevent fn from firing via the channel
		close(done)
	}
}
