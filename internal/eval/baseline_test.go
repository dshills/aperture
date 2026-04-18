package eval

import (
	"path/filepath"
	"testing"
)

func TestLoadBaseline_Missing(t *testing.T) {
	bl, err := LoadBaseline(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatalf("missing baseline should return nil, nil — got err: %v", err)
	}
	if bl != nil {
		t.Fatalf("missing baseline should return nil Baseline, got %+v", bl)
	}
}

func TestWriteAndLoadBaseline_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "baseline.json")
	bl := &Baseline{
		SchemaVersion:         BaselineSchemaVersion,
		GeneratedAt:           "2026-04-18T00:00:00Z",
		ApertureVersion:       "dev",
		SelectionLogicVersion: "sel-v1",
		Fixtures: map[string]BaselineFixtureM{
			"foo": {Precision: 0.9, Recall: 0.9, F1: 0.9},
			"bar": {Precision: 1.0, Recall: 1.0, F1: 1.0},
		},
	}
	if err := WriteBaseline(p, bl); err != nil {
		t.Fatal(err)
	}
	back, err := LoadBaseline(p)
	if err != nil {
		t.Fatal(err)
	}
	if back == nil || len(back.Fixtures) != 2 {
		t.Fatalf("round trip failed: %+v", back)
	}
}

func TestCheckRegressions_DetectsF1Drop(t *testing.T) {
	bl := &Baseline{
		Fixtures: map[string]BaselineFixtureM{
			"foo": {F1: 0.90},
		},
	}
	rr := &RunReport{Fixtures: []FixtureResult{{Name: "foo", Metrics: Metrics{F1: 0.85}}}}
	rc := CheckRegressions(rr, bl, 0.02)
	if len(rc.Regressed) != 1 || rc.Regressed[0].Name != "foo" {
		t.Errorf("expected foo regression, got %+v", rc)
	}
}

func TestCheckRegressions_ToleratesSmallDrop(t *testing.T) {
	bl := &Baseline{Fixtures: map[string]BaselineFixtureM{"foo": {F1: 0.90}}}
	rr := &RunReport{Fixtures: []FixtureResult{{Name: "foo", Metrics: Metrics{F1: 0.89}}}}
	rc := CheckRegressions(rr, bl, 0.02)
	if len(rc.Regressed) != 0 {
		t.Errorf("drop within tolerance should not regress: %+v", rc)
	}
}

func TestCheckRegressions_OrphanAndUnreferenced(t *testing.T) {
	bl := &Baseline{Fixtures: map[string]BaselineFixtureM{"orphan": {F1: 0.5}}}
	rr := &RunReport{Fixtures: []FixtureResult{{Name: "new", Metrics: Metrics{F1: 0.5}}}}
	rc := CheckRegressions(rr, bl, 0.02)
	if len(rc.Orphaned) != 1 || rc.Orphaned[0] != "orphan" {
		t.Errorf("orphan list: %+v", rc.Orphaned)
	}
	if len(rc.Unreferenced) != 1 || rc.Unreferenced[0] != "new" {
		t.Errorf("unreferenced list: %+v", rc.Unreferenced)
	}
}
