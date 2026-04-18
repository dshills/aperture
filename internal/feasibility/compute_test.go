package feasibility

import (
	"testing"

	"github.com/dshills/aperture/internal/index"
	"github.com/dshills/aperture/internal/manifest"
	"github.com/dshills/aperture/internal/selection"
	"github.com/dshills/aperture/internal/task"
)

func baseInputs() Inputs {
	return Inputs{
		Task: task.Task{
			Type:    manifest.ActionTypeFeature,
			Anchors: []string{"Alpha", "Beta", "internal/foo.go"},
		},
		Index: &index.Index{
			Files: []index.FileEntry{
				{Path: "internal/foo.go", Language: "go", Symbols: []index.Symbol{{Name: "Alpha"}}},
			},
		},
		Assignments: []selection.Assignment{
			{Path: "internal/foo.go", LoadMode: manifest.LoadModeFull, Score: 0.90},
		},
		EffectiveContextBudget:  72000,
		EstimatedSelectedTokens: 4000,
	}
}

// Hand-computed expected score for baseInputs:
//
//	coverage=0.25 (1 full / expected=4)
//	anchor_resolution=2/3≈0.667 (Alpha + internal/foo.go resolve, Beta doesn't)
//	task_specificity=1.0 (3 anchors + feature + path mention)
//	budget_headroom=(72000-4000)/72000≈0.944
//	gap_penalty=0
//	score = 0.40*0.25 + 0.25*0.667 + 0.20*1.0 + 0.15*0.944 ≈ 0.609
//
// This lands in §7.8.3's weak band (0.40–0.64).
func TestCompute_WeakOnBaseInputs(t *testing.T) {
	got := Compute(baseInputs())
	if got.Score < 0.40 || got.Score > 0.64 {
		t.Fatalf("expected score in weak band, got %.4f", got.Score)
	}
	if got.Assessment != "weak feasibility" {
		t.Errorf("assessment wrong: %q", got.Assessment)
	}
}

func TestCompute_ClampsBelow040WhenBlockingGap(t *testing.T) {
	in := baseInputs()
	in.Gaps = []manifest.Gap{{Severity: manifest.GapSeverityBlocking}}
	got := Compute(in)
	if got.Score > 0.40 {
		t.Fatalf("blocking gap must clamp score to ≤0.40, got %.4f", got.Score)
	}
}

func TestCompute_GapPenaltyCappedAt050(t *testing.T) {
	gapsMany := make([]manifest.Gap, 20)
	for i := range gapsMany {
		gapsMany[i] = manifest.Gap{Severity: manifest.GapSeverityWarning}
	}
	p := gapPenalty(gapsMany)
	if p != 0.50 {
		t.Fatalf("penalty must cap at 0.50, got %.4f", p)
	}
}

func TestCompute_AnchorResolution(t *testing.T) {
	in := baseInputs()
	got := anchorResolution(in)
	// Alpha resolves via symbol, internal/foo.go via path substring match,
	// Beta does not resolve → 2/3.
	want := 2.0 / 3.0
	if got < want-0.001 || got > want+0.001 {
		t.Fatalf("anchor_resolution wrong: got %.4f want %.4f", got, want)
	}
}

func TestCompute_TaskSpecificity_Tier1(t *testing.T) {
	in := baseInputs()
	// 3 anchors, feature, explicit path mention → 1.0
	if v := taskSpecificity(in); v != 1.0 {
		t.Fatalf("tier 1 should return 1.0, got %.4f", v)
	}
}

func TestCompute_BudgetHeadroom(t *testing.T) {
	in := baseInputs()
	got := budgetHeadroom(in)
	want := float64(72000-4000) / float64(72000)
	if got < want-0.001 || got > want+0.001 {
		t.Fatalf("budget_headroom wrong: got %.4f want %.4f", got, want)
	}
}

func TestRationale_PopulatesNumericValues(t *testing.T) {
	in := baseInputs()
	r := Compute(in)
	pos, neg, block, sub := Rationale(r, in.Gaps)
	if len(pos)+len(neg) < 4 {
		t.Fatalf("expected ≥4 factor lines across positives/negatives, got pos=%v neg=%v", pos, neg)
	}
	if len(block) != 0 {
		t.Fatalf("no blocking gaps expected, got %v", block)
	}
	if _, ok := sub["coverage"]; !ok {
		t.Error("sub_signals must carry coverage")
	}
}
