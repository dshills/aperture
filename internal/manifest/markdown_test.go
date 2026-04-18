package manifest

import (
	"strings"
	"testing"
)

// §7.9.3 requires the Markdown renderer to carry every listed section.
// This test is the authoritative contract-level check: if a section
// header goes missing after a refactor, downstream agents that parse
// the sections break silently without it.
func TestEmitMarkdown_CarriesAll7_9_3Sections(t *testing.T) {
	m := newStubManifest()
	if err := ApplyHash(m); err != nil {
		t.Fatal(err)
	}
	body := string(EmitMarkdown(m))
	for _, section := range []string{
		"## Task Summary",
		"## Planning Assumptions",
		"## Selected Full Context",
		"## Selected Summaries",
		"## Reachable Context",
		"## Gaps",
		"## Feasibility",
		"## Token Accounting",
		"## Usage Instructions",
	} {
		if !strings.Contains(body, section) {
			t.Errorf("markdown missing §7.9.3 section %q:\n%s", section, body)
		}
	}
}

// Running the renderer twice on the same manifest must produce identical
// bytes — the determinism contract (§8.3) extends to Markdown output
// even though the hash is computed from compact JSON.
func TestEmitMarkdown_Deterministic(t *testing.T) {
	m := newStubManifest()
	if err := ApplyHash(m); err != nil {
		t.Fatal(err)
	}
	a := string(EmitMarkdown(m))
	b := string(EmitMarkdown(m))
	if a != b {
		t.Fatalf("markdown not deterministic across invocations")
	}
}

// When the manifest is incomplete (i.e. underflow fired), the renderer
// must emit a visible warning so downstream agents / human readers
// don't act on a partial selection.
func TestEmitMarkdown_IncompleteEmitsWarning(t *testing.T) {
	m := newStubManifest()
	m.Incomplete = true
	if err := ApplyHash(m); err != nil {
		t.Fatal(err)
	}
	body := string(EmitMarkdown(m))
	if !strings.Contains(body, "incomplete") {
		t.Errorf("expected incomplete warning in output:\n%s", body)
	}
	if !strings.Contains(body, "Do NOT proceed") {
		t.Errorf("expected explicit warning language:\n%s", body)
	}
}

// Selections with demotion_reason must surface it under the relevant
// section so auditors can see why a highly-relevant file got a summary.
func TestEmitMarkdown_DemotionReasonRendered(t *testing.T) {
	m := newStubManifest()
	reason := "size_band=large"
	m.Selections = []Selection{{
		Path:            "internal/foo.go",
		Kind:            "file",
		LoadMode:        LoadModeStructuralSummary,
		RelevanceScore:  0.91,
		ScoreBreakdown:  []BreakdownEntry{{Factor: "mention", Signal: 1, Weight: 0.25, Contribution: 0.25}},
		EstimatedTokens: 200,
		Rationale:       []string{"direct task mention"},
		DemotionReason:  &reason,
		SideEffects:     []string{},
	}}
	if err := ApplyHash(m); err != nil {
		t.Fatal(err)
	}
	body := string(EmitMarkdown(m))
	if !strings.Contains(body, "size_band=large") {
		t.Errorf("demotion reason not rendered:\n%s", body)
	}
	if !strings.Contains(body, "internal/foo.go") {
		t.Errorf("selection path not rendered:\n%s", body)
	}
}
