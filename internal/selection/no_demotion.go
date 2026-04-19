package selection

import (
	"sort"

	"github.com/dshills/aperture/internal/loadmode"
	"github.com/dshills/aperture/internal/manifest"
)

// Options controls Select behavior. The zero value reproduces the v1
// deterministic two-pass greedy selector (identical to Select()).
// v1.1 §7.5.1 adds SuppressDemotion for the eval loadmode harness.
type Options struct {
	// SuppressDemotion keeps every full-eligible candidate at
	// LoadModeFull regardless of budget. §7.5.1 "no-demotion shim"
	// for `aperture eval loadmode`. Budget overflow is tolerated
	// and the overflow amount is recorded on Result.BudgetOverflow.
	// This flag MUST NOT be set by production (non-eval) code paths
	// (guarded by a forbidigo rule in .golangci.yml).
	SuppressDemotion bool
}

// SelectWithOptions is the v1.1 variant of Select() that honors
// Options.SuppressDemotion. Under SuppressDemotion=false the
// behavior is byte-identical to Select(); under
// SuppressDemotion=true every candidate eligible for
// LoadModeFull is assigned LoadModeFull regardless of cost.
//
// The deterministic Pass-1 comparator (§7.6.2.1) still applies,
// but the budget check is removed for full-eligible candidates.
// Pass-2 reachable promotion is unchanged.
func SelectWithOptions(candidates []loadmode.Candidate, budget int, opts Options) Result {
	if !opts.SuppressDemotion {
		return Select(candidates, budget)
	}
	return selectForcedFull(candidates, budget)
}

// selectForcedFull implements §7.5.1's "Plan_B": every candidate
// that would have been eligible for LoadModeFull gets assigned
// LoadModeFull; budget overflow is tolerated and recorded.
// Candidates that are NOT full-eligible still go through the
// normal Eligibility() flow (structural / behavioral / reachable).
func selectForcedFull(candidates []loadmode.Candidate, budget int) Result {
	byPath := make(map[string]loadmode.Candidate, len(candidates))
	for _, c := range candidates {
		byPath[c.File.Path] = c
	}

	chosen := make(map[string]Assignment, len(candidates))
	remaining := budget

	// Pass 1a: every full-eligible candidate lands at full, no
	// budget check. Sorted by path so emission is deterministic.
	// Dedup paths — if candidates somehow carries the same path
	// more than once (a defensive guard mirroring the v1 selector's
	// chosen-map check), the budget must only be deducted once per
	// unique path. byPath already holds one entry per path.
	seen := make(map[string]struct{}, len(candidates))
	sortedPaths := make([]string, 0, len(candidates))
	for _, c := range candidates {
		if _, dup := seen[c.File.Path]; dup {
			continue
		}
		seen[c.File.Path] = struct{}{}
		sortedPaths = append(sortedPaths, c.File.Path)
	}
	sort.Strings(sortedPaths)
	for _, p := range sortedPaths {
		c := byPath[p]
		if !candidateWantsFull(c) {
			continue
		}
		chosen[p] = Assignment{
			Path:     p,
			LoadMode: manifest.LoadModeFull,
			Score:    c.Score,
			Cost:     c.CostFull,
		}
		remaining -= c.CostFull
	}

	// Pass 1b: for candidates NOT full-eligible, run the normal
	// v1 selection against whatever budget remains (which may be
	// negative — in that case no further candidates land and we
	// report the overflow).
	type pair struct {
		path       string
		mode       manifest.LoadMode
		score      float64
		cost       int
		efficiency float64
	}
	pairs := make([]pair, 0, len(candidates)*3)
	for _, c := range candidates {
		if _, already := chosen[c.File.Path]; already {
			continue
		}
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
	for _, p := range pairs {
		if _, already := chosen[p.path]; already {
			continue
		}
		if p.cost > remaining {
			continue
		}
		c := byPath[p.path]
		chosen[p.path] = Assignment{
			Path:     p.path,
			LoadMode: p.mode,
			Score:    c.Score,
			Cost:     p.cost,
		}
		remaining -= p.cost
	}

	// Pass 2: reachable for survivors that didn't get a Pass-1 slot.
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

	assignments := make([]Assignment, 0, len(chosen))
	for _, a := range chosen {
		assignments = append(assignments, a)
	}
	sort.Slice(assignments, func(i, j int) bool { return assignments[i].Path < assignments[j].Path })

	spent := budget - remaining
	overflow := 0
	if remaining < 0 {
		overflow = -remaining
	}
	return Result{
		Assignments:      assignments,
		SpentTokens:      spent,
		EligibleButEmpty: len(candidates) > 0 && len(assignments) == 0,
		BudgetOverflow:   overflow,
	}
}
