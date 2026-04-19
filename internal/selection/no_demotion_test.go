package selection

import (
	"testing"

	"github.com/dshills/aperture/internal/index"
	"github.com/dshills/aperture/internal/loadmode"
	"github.com/dshills/aperture/internal/manifest"
)

// TestSelectWithOptions_DefaultMatchesSelect: zero-value Options
// produces byte-identical output to Select() — the §7.5.1 flag
// must not perturb the v1.0 path.
func TestSelectWithOptions_DefaultMatchesSelect(t *testing.T) {
	cs := []loadmode.Candidate{
		makeCand("a.go", 0.85, 500, 200, 300),
		makeCand("b.go", 0.70, 700, 250, 350),
	}
	a := Select(cs, 10000)
	b := SelectWithOptions(cs, 10000, Options{})
	if len(a.Assignments) != len(b.Assignments) {
		t.Fatalf("len mismatch: %d vs %d", len(a.Assignments), len(b.Assignments))
	}
	for i := range a.Assignments {
		if a.Assignments[i] != b.Assignments[i] {
			t.Errorf("assignment %d differs: %+v vs %+v", i, a.Assignments[i], b.Assignments[i])
		}
	}
	if a.BudgetOverflow != 0 || b.BudgetOverflow != 0 {
		t.Errorf("normal path should not overflow")
	}
}

// TestSelectWithOptions_SuppressDemotion_KeepsFull: full-eligible
// candidates land at LoadModeFull regardless of budget.
func TestSelectWithOptions_SuppressDemotion_KeepsFull(t *testing.T) {
	// Two full-eligible candidates, total cost 1200, budget only 500.
	cs := []loadmode.Candidate{
		makeCand("a.go", 0.85, 500, 200, 300),
		makeCand("b.go", 0.90, 700, 250, 350),
	}
	out := SelectWithOptions(cs, 500, Options{SuppressDemotion: true})
	fullCount := 0
	for _, a := range out.Assignments {
		if a.LoadMode == manifest.LoadModeFull {
			fullCount++
		}
	}
	if fullCount != 2 {
		t.Errorf("expected both candidates at full; got assignments=%+v", out.Assignments)
	}
	if out.BudgetOverflow == 0 {
		t.Errorf("expected overflow > 0; spent=%d budget=500", out.SpentTokens)
	}
	wantOverflow := 1200 - 500
	if out.BudgetOverflow != wantOverflow {
		t.Errorf("overflow=%d, want %d", out.BudgetOverflow, wantOverflow)
	}
}

// TestSelectWithOptions_SuppressDemotion_Deterministic: same inputs
// produce identical outputs across repeated calls.
func TestSelectWithOptions_SuppressDemotion_Deterministic(t *testing.T) {
	cs := []loadmode.Candidate{
		makeCand("a.go", 0.9, 500, 200, 300),
		makeCand("b.go", 0.8, 700, 250, 350),
		makeCand("c.go", 0.85, 600, 240, 330),
	}
	prev := SelectWithOptions(cs, 1000, Options{SuppressDemotion: true})
	for i := 0; i < 5; i++ {
		cur := SelectWithOptions(cs, 1000, Options{SuppressDemotion: true})
		if len(cur.Assignments) != len(prev.Assignments) {
			t.Fatalf("run %d: len differs", i)
		}
		for j := range cur.Assignments {
			if cur.Assignments[j] != prev.Assignments[j] {
				t.Errorf("run %d assignment %d differs", i, j)
			}
		}
		if cur.BudgetOverflow != prev.BudgetOverflow {
			t.Errorf("overflow differs across runs: %d vs %d", cur.BudgetOverflow, prev.BudgetOverflow)
		}
	}
}

func makeCand(path string, score float64, costFull, costStruct, costBehav int) loadmode.Candidate {
	f := &index.FileEntry{
		Path:     path,
		Language: "go",
		Symbols:  []index.Symbol{{Name: "Sym" + path, Kind: index.SymbolFunc, Exported: true}},
	}
	return loadmode.Candidate{
		File:           f,
		Score:          score,
		Band:           loadmode.ClassifyScore(score),
		Size:           loadmode.SizeSmall,
		Mentioned:      true,
		CostFull:       costFull,
		CostStructural: costStruct,
		CostBehavioral: costBehav,
	}
}
