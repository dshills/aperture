// Package pipeline is the Go side of the polyglot-python fixture.
// A v1.0 plan would pick this file because Go is the only
// tier-1 language; v1.1 tier-2 tree-sitter surfaces
// scripts/runner.py as the real answer.
package pipeline

// Orchestrator glues together external batch jobs.
type Orchestrator struct {
	name string
}

// Run kicks off the batch job pipeline.
func (o *Orchestrator) Run() error { return nil }
