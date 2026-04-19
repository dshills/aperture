package eval

import (
	"sort"

	"github.com/dshills/aperture/internal/manifest"
)

// LoadmodeDelta names the four §7.5.1 agent_check delta outcomes.
// Every fixture with a declared agent_check lands in exactly one
// of these four buckets.
type LoadmodeDelta string

const (
	LoadmodeImprovement  LoadmodeDelta = "IMPROVEMENT"    // Plan_A failed, Plan_B passed
	LoadmodeRegression   LoadmodeDelta = "REGRESSION"     // Plan_A passed, Plan_B failed
	LoadmodeNoChangePass LoadmodeDelta = "NO_CHANGE_PASS" // both passed
	LoadmodeNoChangeFail LoadmodeDelta = "NO_CHANGE_FAIL" // both failed
)

// ClassifyAgentCheckDelta maps (A-pass, B-pass) to the §7.5.1
// four-valued enum. Outcomes other than pass/fail collapse to
// "fail" per §7.1.1's pass/fail semantics (timeout → fail;
// not-found is handled at the caller by aborting the run).
func ClassifyAgentCheckDelta(a, b AgentCheckOutcome) LoadmodeDelta {
	aPass := a == AgentCheckPass
	bPass := b == AgentCheckPass
	switch {
	case !aPass && bPass:
		return LoadmodeImprovement
	case aPass && !bPass:
		return LoadmodeRegression
	case aPass && bPass:
		return LoadmodeNoChangePass
	default:
		return LoadmodeNoChangeFail
	}
}

// SymbolicDiff is the §7.5.1 "always-reported" view: the structural
// manifest delta between Plan_A (normal) and Plan_B (forced-full)
// that doesn't require a downstream agent to compute. Every field
// is populated regardless of whether agent_check is declared on
// the fixture.
type SymbolicDiff struct {
	// DemotedInA enumerates files that were demoted from full in
	// Plan_A but held at full in Plan_B. Keyed by path; entries
	// carry the Plan_A score and the file size bytes for context.
	DemotedInA []DemotedEntry

	// TokensGainedByForcing is EstimatedSelectedTokens(B) -
	// EstimatedSelectedTokens(A). Always >= 0 (forcing full can
	// only increase spent tokens).
	TokensGainedByForcing int

	// FeasibilityDelta is feasibility.score(B) - feasibility.score(A).
	// Positive means forcing-full helped; negative means it hurt.
	FeasibilityDelta float64

	// GapsFiredInAOnly / GapsFiredInBOnly list gap types that
	// appeared in one plan but not the other as a consequence of
	// forcing full.
	GapsFiredInAOnly []string
	GapsFiredInBOnly []string

	// ForcedFullWouldUnderflow records whether Plan_B's
	// budget was exceeded (§7.5.1: "planner-level definition:
	// no candidate at the highest priority fits within the
	// effective context budget"). Recorded as a boolean data
	// point; never raised as an error by loadmode.
	ForcedFullWouldUnderflow bool

	// BudgetOverflowTokens is the token count by which Plan_B's
	// forced-full spend exceeds its effective context budget.
	// 0 when everything fits.
	BudgetOverflowTokens int
}

// DemotedEntry is one row of SymbolicDiff.DemotedInA. TokenCount is
// the §6.6 "full"-mode cost for this file as carried on the manifest
// selection — it is a token count, NOT a byte count. The manifest
// does not surface raw file sizes per selection.
type DemotedEntry struct {
	Path       string
	ScoreA     float64
	TokenCount int64
}

// ComputeSymbolicDiff compares two manifests and returns the
// §7.5.1 symbolic view. Caller supplies both manifests plus the
// Plan_B budget-overflow count from selection.Result.
func ComputeSymbolicDiff(planA, planB *manifest.Manifest, overflowB int) SymbolicDiff {
	d := SymbolicDiff{
		BudgetOverflowTokens: overflowB,
	}

	// One-pass indexing of planA.Selections so per-path score /
	// token lookups stay O(1). Without this the demotion loop is
	// O(demoted × selections) because selectionScore /
	// selectionSize each linear-scan m.Selections.
	selByPathA := make(map[string]manifest.Selection, len(planA.Selections))
	for _, s := range planA.Selections {
		selByPathA[s.Path] = s
	}
	loadA := selectionLoadModes(planA)
	loadB := selectionLoadModes(planB)
	demotions := []DemotedEntry{}
	for path, modeA := range loadA {
		if modeA == manifest.LoadModeFull {
			continue
		}
		if loadB[path] != manifest.LoadModeFull {
			continue
		}
		// Score/size come from Plan_A's selections — both plans
		// operate on the same index snapshot, so the size is
		// identical on either side.
		sA := selByPathA[path]
		demotions = append(demotions, DemotedEntry{
			Path:      path,
			ScoreA:    sA.RelevanceScore,
			TokenCount: int64(sA.EstimatedTokens),
		})
	}
	sort.Slice(demotions, func(i, j int) bool { return demotions[i].Path < demotions[j].Path })
	d.DemotedInA = demotions

	d.TokensGainedByForcing = planB.Budget.EstimatedSelectedTokens - planA.Budget.EstimatedSelectedTokens
	if d.TokensGainedByForcing < 0 {
		d.TokensGainedByForcing = 0
	}
	d.FeasibilityDelta = planB.Feasibility.Score - planA.Feasibility.Score

	typesA := gapTypeSet(planA)
	typesB := gapTypeSet(planB)
	for t := range typesA {
		if _, ok := typesB[t]; !ok {
			d.GapsFiredInAOnly = append(d.GapsFiredInAOnly, t)
		}
	}
	for t := range typesB {
		if _, ok := typesA[t]; !ok {
			d.GapsFiredInBOnly = append(d.GapsFiredInBOnly, t)
		}
	}
	sort.Strings(d.GapsFiredInAOnly)
	sort.Strings(d.GapsFiredInBOnly)

	d.ForcedFullWouldUnderflow = overflowB > 0
	return d
}

func selectionLoadModes(m *manifest.Manifest) map[string]manifest.LoadMode {
	out := make(map[string]manifest.LoadMode, len(m.Selections))
	for _, s := range m.Selections {
		out[s.Path] = s.LoadMode
	}
	return out
}

func gapTypeSet(m *manifest.Manifest) map[string]struct{} {
	out := make(map[string]struct{}, len(m.Gaps))
	for _, g := range m.Gaps {
		out[string(g.Type)] = struct{}{}
	}
	return out
}
