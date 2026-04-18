package selection

import (
	"github.com/dshills/aperture/internal/loadmode"
	"github.com/dshills/aperture/internal/manifest"
)

// Underflow detects the §7.6.5 condition: the effective context budget
// (after reservations) is smaller than the smallest viable cost of the
// highest-scoring candidate. When true, the caller must:
//
//   - emit an `oversized_primary_context` gap with severity "blocking"
//   - set manifest.incomplete = true
//   - exit non-zero (code 9) regardless of other threshold flags
//   - still emit the manifest for auditability
//
// Detection is deterministic even under tied top scores: we look at
// every candidate sharing the maximum score and take the minimum viable
// cost across ALL of them. Underflow fires iff no viable cost fits the
// budget. This is a strict reading: §7.6.5 talks about "the highest-
// scoring candidate" (singular), but ties would otherwise make the
// predicate depend on input-slice order — so we honor the spec's intent
// (can any top-tier candidate fit?) rather than its ambiguous wording.
func Underflow(candidates []loadmode.Candidate, effectiveBudget int) bool {
	if len(candidates) == 0 {
		return false
	}
	var topScore float64
	for i, c := range candidates {
		if i == 0 || c.Score > topScore {
			topScore = c.Score
		}
	}
	if topScore < 0.30 {
		// The top candidate is below plausibly-relevant; underflow
		// applies only when we DO have a viable selection target.
		return false
	}
	minCost := -1
	for _, c := range candidates {
		if c.Score < topScore {
			continue
		}
		for _, m := range loadmode.Eligibility(c) {
			var cost int
			switch m {
			case manifest.LoadModeFull:
				cost = c.CostFull
			case manifest.LoadModeStructuralSummary:
				cost = c.CostStructural
			case manifest.LoadModeBehavioralSummary:
				cost = c.CostBehavioral
			default:
				continue
			}
			if minCost < 0 || cost < minCost {
				minCost = cost
			}
		}
	}
	if minCost < 0 {
		return false
	}
	return effectiveBudget < minCost
}
