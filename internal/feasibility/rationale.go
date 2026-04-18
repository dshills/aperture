package feasibility

import (
	"fmt"
	"sort"

	"github.com/dshills/aperture/internal/manifest"
)

// SubSignalKeys enumerates the manifest sub_signals keys §7.8.2.1 emits,
// in the canonical order explain / debug output should render them.
// Exported so renderers (e.g. aperture explain) stay in sync with this
// package's ground truth rather than duplicating a hardcoded list.
var SubSignalKeys = []string{
	"coverage",
	"anchor_resolution",
	"task_specificity",
	"budget_headroom",
	"gap_penalty",
}

// Rationale builds the manifest's positives/negatives/blocking_conditions
// arrays and the sub_signals map. The inputs — r and gaps — are the same
// Result and gap slice the caller already has.
//
// Per §7.8.2.1 final paragraph, "positives, negatives, and
// blocking_conditions must enumerate the concrete sub-signal
// contributions (with numeric values) that drove the score — not prose
// guesses."
func Rationale(r Result, gaps []manifest.Gap) (positives, negatives, blocking []string, subSignals map[string]float64) {
	positives = make([]string, 0, 4)
	negatives = make([]string, 0, 4)
	blocking = make([]string, 0)

	// Treat the four contributing factors as positives when they meet a
	// reasonable bar (≥0.50) and as negatives when they're weak (<0.50).
	type factor struct {
		name  string
		value float64
	}
	factors := []factor{
		{"coverage", r.SubSignals.Coverage},
		{"anchor_resolution", r.SubSignals.AnchorResolution},
		{"task_specificity", r.SubSignals.TaskSpecificity},
		{"budget_headroom", r.SubSignals.BudgetHeadroom},
	}
	for _, f := range factors {
		line := fmt.Sprintf("%s=%.2f", f.name, f.value)
		if f.value >= 0.50 {
			positives = append(positives, line)
		} else {
			negatives = append(negatives, line)
		}
	}
	if r.SubSignals.GapPenalty > 0 {
		negatives = append(negatives, fmt.Sprintf("gap_penalty=%.2f", r.SubSignals.GapPenalty))
	}

	// Blocking conditions enumerate every blocking gap with a short
	// reference to its rule type. Ordering is alphabetical for
	// determinism.
	for _, g := range gaps {
		if g.Severity == manifest.GapSeverityBlocking {
			blocking = append(blocking, fmt.Sprintf("%s: %s", g.Type, g.Description))
		}
	}
	sort.Strings(blocking)

	subSignals = map[string]float64{
		"coverage":          r.SubSignals.Coverage,
		"anchor_resolution": r.SubSignals.AnchorResolution,
		"task_specificity":  r.SubSignals.TaskSpecificity,
		"budget_headroom":   r.SubSignals.BudgetHeadroom,
		"gap_penalty":       r.SubSignals.GapPenalty,
	}
	return
}
