package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/dshills/aperture/internal/manifest"
)

// newFixtureManifest builds a small manifest by running the Phase-3 pipeline
// against the small_go fixture. Explain tests then render it to a buffer.
func newFixtureManifest(t *testing.T) *manifest.Manifest {
	t.Helper()
	in := buildFixtureInputs(t, "add refresh handling to internal/oauth/provider.go for github oauth", "", 120000)
	m, err := BuildManifest(in)
	if err != nil {
		t.Fatalf("BuildManifest: %v", err)
	}
	return m
}

func TestExplain_RendersAllSections(t *testing.T) {
	m := newFixtureManifest(t)
	var buf bytes.Buffer
	if err := renderExplain(&buf, m); err != nil {
		t.Fatalf("renderExplain: %v", err)
	}
	out := buf.String()
	for _, section := range []string{
		"Task:",
		"Budget:",
		"Selections:",
		"Reachable:",
		"Gaps:",
		"Feasibility:",
	} {
		if !strings.Contains(out, section) {
			t.Errorf("explain output missing %q section:\n%s", section, out)
		}
	}
}

func TestExplain_ShowsScoreBreakdown(t *testing.T) {
	m := newFixtureManifest(t)
	var buf bytes.Buffer
	if err := renderExplain(&buf, m); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "signal=") || !strings.Contains(buf.String(), "contribution=") {
		t.Fatalf("explain should render score_breakdown:\n%s", buf.String())
	}
}

// Explain on a manifest with a blocking gap must surface it.
func TestExplain_ShowsBlockingGap(t *testing.T) {
	m := &manifest.Manifest{
		Task:   manifest.Task{TaskID: "tsk_test", Type: manifest.ActionTypeFeature, Anchors: []string{}},
		Budget: manifest.Budget{Estimator: "heuristic-3.5", EstimatorVersion: "v1"},
		Gaps: []manifest.Gap{{
			ID:          "gap-1",
			Type:        manifest.GapOversizedPrimaryContext,
			Severity:    manifest.GapSeverityBlocking,
			Description: "budget too small",
		}},
		Feasibility: manifest.Feasibility{
			Score:              0.25,
			Assessment:         "poor",
			BlockingConditions: []string{"oversized_primary_context: budget too small"},
			SubSignals:         map[string]float64{},
		},
	}
	var buf bytes.Buffer
	if err := renderExplain(&buf, m); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "blocking") {
		t.Errorf("explain should mark severity as blocking:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "BLOCKING") {
		t.Errorf("explain should list blocking_conditions:\n%s", buf.String())
	}
}
