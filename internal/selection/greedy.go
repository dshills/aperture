// Package selection implements the mandatory two-pass deterministic
// greedy selector from SPEC §7.6.2.1. No other algorithm is permitted in
// v1; the contract is that bit-identical inputs produce bit-identical
// selections regardless of input ordering.
package selection

import (
	"math"
	"sort"

	"github.com/dshills/aperture/internal/loadmode"
	"github.com/dshills/aperture/internal/manifest"
)

// ModeWeight is the fixed Pass-1 mode weight from §7.6.2.1. `reachable`
// is never used in Pass 1; its nominal 0.05 is recorded here only so a
// caller that wants it for display purposes can read it via a constant.
var ModeWeight = map[manifest.LoadMode]float64{
	manifest.LoadModeFull:              1.00,
	manifest.LoadModeStructuralSummary: 0.60,
	manifest.LoadModeBehavioralSummary: 0.40,
	manifest.LoadModeReachable:         0.05,
}

// Assignment is one output row of the selector: the chosen mode for a
// candidate plus the cost that mode consumes. For reachable, Cost is 0.
type Assignment struct {
	Path          string
	LoadMode      manifest.LoadMode
	Score         float64
	Cost          int
	Demotion      string // populated when a candidate was demoted off of `full`
	DemotedReason string // raw reason enum per §7.6.4
}

// Result carries both the assignments and the tokens actually spent.
type Result struct {
	Assignments      []Assignment
	SpentTokens      int
	EligibleButEmpty bool // true when there were candidates but none survived Pass 1 or 2
	// BudgetOverflow is the amount by which SpentTokens exceeds
	// the input budget. Always 0 under the normal v1 selector
	// because it refuses to overspend. Non-zero only under
	// SelectWithOptions(SuppressDemotion=true) per v1.1 §7.5.1:
	// the forced-full variant tolerates overflow and records the
	// excess so `aperture eval loadmode` can report it.
	BudgetOverflow int
}

// Select runs the §7.6.2.1 algorithm. Candidates must already carry their
// per-mode token costs (callers: see internal/budget.EstimateFull /
// EstimateSummary). Budget is the effective context budget (§6.3).
func Select(candidates []loadmode.Candidate, budget int) Result {
	// Snapshot input by path for deterministic retrieval.
	byPath := map[string]loadmode.Candidate{}
	for _, c := range candidates {
		byPath[c.File.Path] = c
	}

	// Pass 1 pairs exclude reachable (§7.6.2.1): only full, structural,
	// behavioral consume budget.
	type pair struct {
		path       string
		mode       manifest.LoadMode
		score      float64
		cost       int
		efficiency float64
	}
	pairs := make([]pair, 0, len(candidates)*3)
	for _, c := range candidates {
		for _, mode := range loadmode.Eligibility(c) {
			if mode == manifest.LoadModeReachable {
				continue
			}
			cost := costFor(c, mode)
			pairs = append(pairs, pair{
				path:       c.File.Path,
				mode:       mode,
				score:      c.Score,
				cost:       cost,
				efficiency: quantizeEfficiency(c.Score * ModeWeight[mode] / float64(maxInt1(cost))),
			})
		}
	}

	// The deterministic comparator encodes all four §7.6.2.1 tie-break
	// levels in ONE Less function. No stacked sorts — a stable sort over
	// multiple passes can reorder equal-key siblings depending on their
	// original input position.
	sort.Slice(pairs, func(i, j int) bool {
		a, b := pairs[i], pairs[j]
		if a.efficiency != b.efficiency {
			return a.efficiency > b.efficiency
		}
		if a.cost != b.cost {
			return a.cost < b.cost
		}
		if a.path != b.path {
			return a.path < b.path
		}
		return modePriority(a.mode) > modePriority(b.mode)
	})

	chosen := map[string]Assignment{}
	remaining := budget
	for _, p := range pairs {
		if _, already := chosen[p.path]; already {
			continue
		}
		if p.cost > remaining {
			continue
		}
		c := byPath[p.path]
		a := Assignment{
			Path:     p.path,
			LoadMode: p.mode,
			Score:    c.Score,
			Cost:     p.cost,
		}
		// Demotion bookkeeping — §7.6.4: a highly_relevant file that ends
		// up in a summary rather than full must carry a demotion_reason.
		// SizeLarge files are disqualified from full by the eligibility
		// layer (so candidateWantsFull returns false), meaning we must
		// evaluate size independently; budget-driven demotions are the
		// remaining case.
		if p.mode != manifest.LoadModeFull && c.Band == loadmode.HighlyRelevant {
			a.Demotion = string(p.mode)
			switch {
			case c.Size == loadmode.SizeLarge:
				a.DemotedReason = "size_band=large"
			case candidateWantsFull(c):
				a.DemotedReason = "budget_insufficient"
			default:
				a.DemotedReason = "size_band=large"
			}
		}
		chosen[p.path] = a
		remaining -= p.cost
	}

	// Pass 2: reachable for every candidate that (a) didn't get a Pass-1
	// assignment and (b) is eligible per §7.5.4 or was demoted out of a
	// higher mode by budget exhaustion.
	reachablePaths := make([]string, 0, len(candidates))
	for _, c := range candidates {
		if _, ok := chosen[c.File.Path]; ok {
			continue
		}
		if !reachableEligible(c) {
			continue
		}
		reachablePaths = append(reachablePaths, c.File.Path)
	}
	sort.Strings(reachablePaths)
	for _, p := range reachablePaths {
		c := byPath[p]
		chosen[p] = Assignment{
			Path:     p,
			LoadMode: manifest.LoadModeReachable,
			Score:    c.Score,
			Cost:     0,
		}
	}

	// Emit in canonical path order (§14).
	assignments := make([]Assignment, 0, len(chosen))
	for _, a := range chosen {
		assignments = append(assignments, a)
	}
	sort.Slice(assignments, func(i, j int) bool {
		return assignments[i].Path < assignments[j].Path
	})

	return Result{
		Assignments:      assignments,
		SpentTokens:      budget - remaining,
		EligibleButEmpty: len(candidates) > 0 && len(assignments) == 0,
	}
}

func costFor(c loadmode.Candidate, mode manifest.LoadMode) int {
	switch mode {
	case manifest.LoadModeFull:
		return c.CostFull
	case manifest.LoadModeStructuralSummary:
		return c.CostStructural
	case manifest.LoadModeBehavioralSummary:
		return c.CostBehavioral
	}
	return 0
}

func modePriority(m manifest.LoadMode) int {
	switch m {
	case manifest.LoadModeFull:
		return 3
	case manifest.LoadModeStructuralSummary:
		return 2
	case manifest.LoadModeBehavioralSummary:
		return 1
	}
	return 0
}

func maxInt1(n int) int {
	if n < 1 {
		return 1
	}
	return n
}

// quantizeEfficiency rounds the efficiency metric to a fixed 9-decimal
// representation. Two `float64` values produced by the same arithmetic
// sequence on the same CPU always compare equal; this rounding exists so
// they also compare equal across Go toolchain versions and fused-multiply-
// add vs. non-FMA hardware paths. The scale is well above the precision
// ever needed (score ∈ [0,1], cost ≥ 1 → efficiency ≤ 1.0), so distinct
// inputs still produce distinct quantized outputs.
//
// Uses math.Round (banker's-round-half-away-from-zero) rather than the
// manual +0.5 / int64 conversion so values near int64's precision limit
// are handled safely. Efficiency is bounded by [0, 1], so the x·scale
// product stays well within float64 precision.
func quantizeEfficiency(x float64) float64 {
	const scale = 1e9
	return math.Round(x*scale) / scale
}

// candidateWantsFull reports whether the candidate was full-eligible —
// used for demotion-reason bookkeeping.
func candidateWantsFull(c loadmode.Candidate) bool {
	for _, m := range loadmode.Eligibility(c) {
		if m == manifest.LoadModeFull {
			return true
		}
	}
	return false
}

// reachableEligible is the Pass-2 admission rule: plausibly-relevant or
// higher, AND not already assigned. Files demoted from a higher mode by
// budget exhaustion are still eligible (they will show up here because
// the Pass-1 loop skipped them).
func reachableEligible(c loadmode.Candidate) bool {
	return c.Band == loadmode.PlausiblyRelevant ||
		c.Band == loadmode.ModeratelyRelevant ||
		c.Band == loadmode.HighlyRelevant
}
