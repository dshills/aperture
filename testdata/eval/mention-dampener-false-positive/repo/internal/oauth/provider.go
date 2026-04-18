// Package oauth hosts a thin logging wrapper around the refresh
// pipeline. The real refresh behavior lives in refresh.go; provider.go
// is only a glue layer and does NOT implement the refresh or retry
// logic. Present in the fixture to lure a mention-driven planner.
package oauth

import "log"

// Provider is a thin logging wrapper around the refresh pipeline. It
// forwards every call down without modification — no retry, no token
// handling, no error translation. The fixture's task deliberately
// names this file; the v1.1 dampener should keep it out of the top
// rank because no other scoring factor agrees.
type Provider struct {
	name string
}

// New returns a Provider. It does nothing interesting.
func New(name string) *Provider {
	return &Provider{name: name}
}

// Log emits a log line for every refresh attempt. Behavior unrelated
// to the bug in the task prompt.
func (p *Provider) Log(msg string) {
	log.Printf("[%s] %s", p.name, msg)
}
