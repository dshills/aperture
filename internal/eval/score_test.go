package eval

import (
	"testing"

	"github.com/dshills/aperture/internal/manifest"
)

func TestScore_PerfectMatch(t *testing.T) {
	fx := Fixture{
		Expected: Expected{
			Selections: []ExpectedSelection{
				{Path: "a.go"},
				{Path: "b.go"},
			},
		},
	}
	m := &manifest.Manifest{
		Selections: []manifest.Selection{
			{Path: "a.go", LoadMode: manifest.LoadModeFull},
			{Path: "b.go", LoadMode: manifest.LoadModeFull},
		},
	}
	v := Score(fx, m)
	if v.Metrics.F1 != 1.0 {
		t.Errorf("F1=%v, want 1.0", v.Metrics.F1)
	}
	if v.HardFail {
		t.Error("unexpected hard fail")
	}
}

func TestScore_PartialMatchLoadModeTiebreak(t *testing.T) {
	fx := Fixture{
		Expected: Expected{
			Selections: []ExpectedSelection{
				{Path: "a.go", LoadMode: "full"},
				{Path: "b.go", LoadMode: "full"},
			},
		},
	}
	m := &manifest.Manifest{
		Selections: []manifest.Selection{
			{Path: "a.go", LoadMode: manifest.LoadModeFull},              // 1.0
			{Path: "b.go", LoadMode: manifest.LoadModeBehavioralSummary}, // 0.5
		},
	}
	v := Score(fx, m)
	// intersection = 1.5; precision = 1.5/2 = 0.75; recall = 1.5/2 = 0.75; F1 = 0.75
	if v.Metrics.Precision != 0.75 || v.Metrics.Recall != 0.75 {
		t.Errorf("metrics=%+v, want P=0.75 R=0.75", v.Metrics)
	}
	if v.HardFail {
		t.Error("unexpected hard fail")
	}
}

func TestScore_ForbiddenAtScoreTriggersHardFail(t *testing.T) {
	fx := Fixture{
		Expected: Expected{
			Forbidden: []string{"vendor/**"},
		},
	}
	m := &manifest.Manifest{
		Selections: []manifest.Selection{
			{Path: "vendor/foo.go", LoadMode: manifest.LoadModeFull, RelevanceScore: 0.5},
		},
	}
	v := Score(fx, m)
	if !v.HardFail {
		t.Fatal("forbidden selection should hard-fail")
	}
}

func TestScore_ForbiddenBelowThresholdDoesNotFail(t *testing.T) {
	fx := Fixture{
		Expected: Expected{
			Forbidden: []string{"vendor/**"},
		},
	}
	m := &manifest.Manifest{
		Reachable: []manifest.Reachable{
			{Path: "vendor/foo.go", RelevanceScore: 0.1},
		},
	}
	v := Score(fx, m)
	if v.HardFail {
		t.Errorf("reachable at 0.1 should NOT hard-fail (threshold 0.3)")
	}
}

func TestScore_MissingGapHardFails(t *testing.T) {
	fx := Fixture{
		Expected: Expected{
			Gaps: []string{"missing_tests"},
		},
	}
	m := &manifest.Manifest{}
	v := Score(fx, m)
	if !v.HardFail {
		t.Error("expected hard fail when required gap absent")
	}
}

func TestScore_EmptySelectionsZeroRecall(t *testing.T) {
	fx := Fixture{
		Expected: Expected{Selections: []ExpectedSelection{{Path: "a.go"}}},
	}
	m := &manifest.Manifest{}
	v := Score(fx, m)
	if v.Metrics.Recall != 0 {
		t.Errorf("recall=%v, want 0", v.Metrics.Recall)
	}
	if v.Metrics.Precision != 1.0 {
		t.Errorf("empty actual ⇒ precision=1.0; got %v", v.Metrics.Precision)
	}
}
