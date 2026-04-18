package diff

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/dshills/aperture/internal/manifest"
)

func baseManifest() *manifest.Manifest {
	return &manifest.Manifest{
		SchemaVersion: "1.0",
		ManifestID:    "apt_0000000000000001",
		ManifestHash:  "sha256:0000000000000000000000000000000000000000000000000000000000000001",
		GeneratedAt:   "2026-04-18T00:00:00Z",
		Task: manifest.Task{
			TaskID:  "tsk_abcdef0123456789",
			RawText: "Update foo",
			Type:    manifest.ActionTypeFeature,
			Anchors: []string{"foo", "update"},
		},
		Repo: manifest.Repo{
			Fingerprint:   "sha256:aaaa",
			LanguageHints: []string{"go"},
		},
		Budget: manifest.Budget{
			Model:        "claude-sonnet-4-6",
			TokenCeiling: 200000,
		},
		Selections: []manifest.Selection{
			{Path: "a.go", LoadMode: manifest.LoadModeFull, RelevanceScore: 0.8},
			{Path: "b.go", LoadMode: manifest.LoadModeStructuralSummary, RelevanceScore: 0.5},
		},
		Reachable: []manifest.Reachable{
			{Path: "c.go", RelevanceScore: 0.35, Reason: "plausibly_relevant"},
		},
		Gaps: []manifest.Gap{
			{ID: "gap-1", Type: manifest.GapMissingTests, Severity: manifest.GapSeverityWarning},
		},
		Feasibility: manifest.Feasibility{
			Score:      0.7,
			SubSignals: map[string]float64{"anchors": 0.9, "symbols": 0.5},
		},
		GenerationMetadata: manifest.GenerationMetadata{
			ApertureVersion:       "1.1.0",
			SelectionLogicVersion: "sel-v2",
			ConfigDigest:          "sha256:cfg1",
		},
	}
}

func TestCompute_SameManifestIsSemanticEquivalent(t *testing.T) {
	a := baseManifest()
	b := baseManifest()
	d := Compute(a, b)
	if !d.SemanticEquivalent {
		t.Error("hash equal should be semantically equivalent")
	}
	if len(d.ToolBugDiagnostic) != 0 {
		t.Errorf("expected no tool-bug diagnostic, got %v", d.ToolBugDiagnostic)
	}
}

func TestCompute_HashDifferSemanticNotEquivalent(t *testing.T) {
	a := baseManifest()
	b := baseManifest()
	b.ManifestHash = "sha256:deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	b.Selections[0].LoadMode = manifest.LoadModeBehavioralSummary
	d := Compute(a, b)
	if d.SemanticEquivalent {
		t.Error("hashes differ; should not be semantically equivalent")
	}
	if len(d.SelectionsLoadChanged) != 1 || d.SelectionsLoadChanged[0].Path != "a.go" {
		t.Errorf("expected a.go load-mode change, got %+v", d.SelectionsLoadChanged)
	}
}

func TestCompute_ToolBugDiagnosticFiresUnderHashAgreement(t *testing.T) {
	a := baseManifest()
	b := baseManifest()
	// Construct an impossible state: hashes agree but selections differ.
	// In a well-behaved emitter this shouldn't happen; the diagnostic
	// exists precisely to catch that regression.
	b.Selections = append(b.Selections, manifest.Selection{
		Path: "phantom.go", LoadMode: manifest.LoadModeFull,
	})
	d := Compute(a, b)
	if !d.SemanticEquivalent {
		t.Fatal("hashes are equal; should be semantically equivalent")
	}
	if len(d.ToolBugDiagnostic) == 0 {
		t.Fatal("tool-bug diagnostic should fire under hash-equal + content-differ")
	}
	found := false
	for _, msg := range d.ToolBugDiagnostic {
		if strings.Contains(msg, "selections") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("tool-bug diagnostic should mention selections: %v", d.ToolBugDiagnostic)
	}
}

func TestCompute_SelectionsAddedRemoved(t *testing.T) {
	a := baseManifest()
	b := baseManifest()
	b.ManifestHash = "sha256:different"
	b.Selections = []manifest.Selection{
		{Path: "a.go", LoadMode: manifest.LoadModeFull, RelevanceScore: 0.8},
		{Path: "new.go", LoadMode: manifest.LoadModeFull, RelevanceScore: 0.6},
	}
	d := Compute(a, b)
	if len(d.SelectionsAdded) != 1 || d.SelectionsAdded[0].Path != "new.go" {
		t.Errorf("added=%+v", d.SelectionsAdded)
	}
	if len(d.SelectionsRemoved) != 1 || d.SelectionsRemoved[0].Path != "b.go" {
		t.Errorf("removed=%+v", d.SelectionsRemoved)
	}
}

func TestCompute_ReachablePromotedToSelection(t *testing.T) {
	a := baseManifest()
	b := baseManifest()
	b.ManifestHash = "sha256:promoted"
	// c.go moves from reachable to selection in B.
	b.Reachable = nil
	b.Selections = append(b.Selections, manifest.Selection{
		Path: "c.go", LoadMode: manifest.LoadModeFull, RelevanceScore: 0.7,
	})
	d := Compute(a, b)
	if len(d.ReachablePromoted) != 1 || d.ReachablePromoted[0] != "c.go" {
		t.Errorf("promoted=%+v", d.ReachablePromoted)
	}
}

func TestCompute_GapsAddedResolvedSeverityChanged(t *testing.T) {
	a := baseManifest()
	b := baseManifest()
	b.ManifestHash = "sha256:gapchanges"
	b.Gaps = []manifest.Gap{
		// missing_tests: severity bumped to blocking.
		{ID: "gap-1", Type: manifest.GapMissingTests, Severity: manifest.GapSeverityBlocking},
		// new gap added.
		{ID: "gap-2", Type: manifest.GapMissingSpec, Severity: manifest.GapSeverityInfo},
	}
	d := Compute(a, b)
	if len(d.GapsAdded) != 1 || d.GapsAdded[0].Type != string(manifest.GapMissingSpec) {
		t.Errorf("added=%+v", d.GapsAdded)
	}
	if len(d.GapsSeverityChanged) != 1 {
		t.Errorf("severity changed=%+v", d.GapsSeverityChanged)
	}
	if len(d.GapsResolved) != 0 {
		t.Errorf("resolved should be empty, got %+v", d.GapsResolved)
	}
}

func TestCompute_ScopeDelta(t *testing.T) {
	a := baseManifest()
	b := baseManifest()
	b.ManifestHash = "sha256:scoped"
	b.Scope = &manifest.Scope{Path: "services/billing"}
	d := Compute(a, b)
	if d.ScopeA != "" || d.ScopeB != "services/billing" {
		t.Errorf("scope=%q → %q", d.ScopeA, d.ScopeB)
	}
}

func TestCompute_Deterministic20Runs(t *testing.T) {
	a := baseManifest()
	b := baseManifest()
	b.ManifestHash = "sha256:different"
	b.Selections = []manifest.Selection{
		{Path: "z.go", LoadMode: manifest.LoadModeFull},
		{Path: "a.go", LoadMode: manifest.LoadModeFull},
		{Path: "m.go", LoadMode: manifest.LoadModeBehavioralSummary},
	}
	var prev []byte
	for i := 0; i < 20; i++ {
		d := Compute(a, b)
		buf, err := EmitJSON(d)
		if err != nil {
			t.Fatal(err)
		}
		if i > 0 && !bytes.Equal(prev, buf) {
			t.Fatalf("run %d differs from run 0", i)
		}
		prev = buf
	}
}

func TestEmitJSON_SchemaShape(t *testing.T) {
	d := Compute(baseManifest(), baseManifest())
	buf, err := EmitJSON(d)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(buf, &m); err != nil {
		t.Fatal(err)
	}
	required := []string{
		"schema_version", "semantic_equivalent", "tool_bug_diagnostic",
		"hash", "task", "repo", "budget", "scope",
		"selections", "reachable", "gaps", "feasibility", "generation_metadata",
	}
	for _, k := range required {
		if _, ok := m[k]; !ok {
			t.Errorf("JSON output missing required key %q", k)
		}
	}
}

func TestEmitMarkdown_AlwaysHasAllSections(t *testing.T) {
	d := Compute(baseManifest(), baseManifest())
	md := string(EmitMarkdown(d))
	sections := []string{"Hash and ID", "Task", "Repo", "Budget", "Scope", "Selections", "Reachable", "Gaps", "Feasibility", "Generation metadata"}
	for _, s := range sections {
		if !strings.Contains(md, "## "+s) {
			t.Errorf("markdown missing `## %s` section", s)
		}
	}
	// Unchanged sections render an `_unchanged_` marker.
	if !strings.Contains(md, "_unchanged_") {
		t.Errorf("unchanged sections should render `_unchanged_` marker")
	}
}

func TestSchemaVersion_MajorMinorOrdering(t *testing.T) {
	cases := []struct {
		v, min string
		ok     bool
	}{
		{"1.0", "1.0", true},
		{"1.10", "1.0", true}, // lexicographic would say "1.10" < "1.2"
		{"2.0", "1.0", true},
		{"0.9", "1.0", false},
		{"1.0.3", "1.0", true}, // 3-component still parses; trailing ignored
	}
	for _, c := range cases {
		got, err := schemaVersionAtLeast(c.v, c.min)
		if err != nil {
			t.Fatalf("schemaVersionAtLeast(%q,%q): %v", c.v, c.min, err)
		}
		if got != c.ok {
			t.Errorf("schemaVersionAtLeast(%q,%q) = %v, want %v", c.v, c.min, got, c.ok)
		}
	}

	// Malformed input surfaces an error.
	if _, err := schemaVersionAtLeast("not-a-version", "1.0"); err == nil {
		t.Error("malformed version should error")
	}
}
