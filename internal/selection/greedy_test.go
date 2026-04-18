package selection

import (
	"fmt"
	"math/rand/v2"
	"testing"

	"github.com/dshills/aperture/internal/index"
	"github.com/dshills/aperture/internal/loadmode"
	"github.com/dshills/aperture/internal/manifest"
)

// mkCandidate builds a simple candidate with deterministic per-mode costs.
func mkCandidate(path string, score float64, size loadmode.SizeBand, costFull, costStructural, costBehavioral int) loadmode.Candidate {
	f := &index.FileEntry{
		Path:     path,
		Language: "go",
		Symbols:  []index.Symbol{{Name: "Sym", Kind: index.SymbolFunc}},
	}
	return loadmode.Candidate{
		File:           f,
		Score:          score,
		Band:           loadmode.ClassifyScore(score),
		Size:           size,
		CostFull:       costFull,
		CostStructural: costStructural,
		CostBehavioral: costBehavioral,
	}
}

// Shuffling the input 100 times must not change the final assignment —
// this exercises the single-comparator tie-break chain from §7.6.2.1.
func TestSelect_DeterministicUnderShuffle(t *testing.T) {
	base := []loadmode.Candidate{
		mkCandidate("a/alpha.go", 0.90, loadmode.SizeSmall, 200, 80, 50),
		mkCandidate("a/bravo.go", 0.90, loadmode.SizeSmall, 200, 80, 50),
		mkCandidate("a/charlie.go", 0.85, loadmode.SizeSmall, 300, 100, 70),
		mkCandidate("b/delta.go", 0.60, loadmode.SizeSmall, 400, 150, 90),
		mkCandidate("b/echo.go", 0.40, loadmode.SizeSmall, 500, 200, 100),
	}
	const budget = 900

	canonical := Select(base, budget)
	canonicalKey := fingerprint(canonical.Assignments)

	rng := rand.New(rand.NewPCG(42, 43))
	for i := 0; i < 100; i++ {
		shuffled := append([]loadmode.Candidate{}, base...)
		rng.Shuffle(len(shuffled), func(a, b int) { shuffled[a], shuffled[b] = shuffled[b], shuffled[a] })
		got := Select(shuffled, budget)
		if k := fingerprint(got.Assignments); k != canonicalKey {
			t.Fatalf("shuffle %d changed output\nwant: %s\n got: %s", i, canonicalKey, k)
		}
	}
}

// Two files tied on efficiency, cost, and path must tie-break by mode
// priority (full > structural > behavioral) per §7.6.2.1.
func TestSelect_TieBreakByModePriority(t *testing.T) {
	// One file with multiple eligible modes at the same score. The
	// comparator must prefer full over structural over behavioral.
	c := mkCandidate("a/only.go", 0.95, loadmode.SizeSmall, 100, 100, 100)
	got := Select([]loadmode.Candidate{c}, 200)
	if len(got.Assignments) != 1 {
		t.Fatalf("expected one assignment, got %d", len(got.Assignments))
	}
	if got.Assignments[0].LoadMode != manifest.LoadModeFull {
		t.Fatalf("tie-break must pick full: got %s", got.Assignments[0].LoadMode)
	}
}

// A plausibly-relevant file that didn't win a Pass-1 mode must land in
// reachable (§7.5.4).
func TestSelect_PlausibleFallsThroughToReachable(t *testing.T) {
	c := mkCandidate("x/p.go", 0.45, loadmode.SizeSmall, 100, 100, 100)
	got := Select([]loadmode.Candidate{c}, 1000)
	if len(got.Assignments) != 1 {
		t.Fatalf("expected one assignment, got %d", len(got.Assignments))
	}
	if got.Assignments[0].LoadMode != manifest.LoadModeReachable {
		t.Fatalf("plausible should go reachable (not eligible for full/summary), got %s", got.Assignments[0].LoadMode)
	}
	if got.SpentTokens != 0 {
		t.Errorf("reachable must not consume budget, got %d", got.SpentTokens)
	}
}

// Budget exhaustion: when only one file's full mode fits, that file gets
// picked and the other must fall back to a cheaper mode or get dropped.
// Cost ratio is tuned so full beats structural on efficiency
// (cost_structural/cost_full = 0.70 > 0.60 mode-weight ratio).
func TestSelect_BudgetExhaustionOrder(t *testing.T) {
	cs := []loadmode.Candidate{
		mkCandidate("lo.go", 0.61, loadmode.SizeSmall, 400, 280, 100),
		mkCandidate("hi.go", 0.95, loadmode.SizeSmall, 400, 280, 100),
	}
	got := Select(cs, 450) // fits one full (400), leaving 50 for any summary.
	if len(got.Assignments) == 0 {
		t.Fatalf("expected some assignment")
	}
	var hiMode, loMode manifest.LoadMode
	for _, a := range got.Assignments {
		switch a.Path {
		case "hi.go":
			hiMode = a.LoadMode
		case "lo.go":
			loMode = a.LoadMode
		}
	}
	if hiMode != manifest.LoadModeFull {
		t.Fatalf("high-score file must get full, got %s", hiMode)
	}
	if loMode == manifest.LoadModeFull {
		t.Fatalf("low-score file must not also get full under tight budget; got %s", loMode)
	}
}

func fingerprint(assignments []Assignment) string {
	s := ""
	for _, a := range assignments {
		s += fmt.Sprintf("%s:%s|", a.Path, a.LoadMode)
	}
	return s
}
