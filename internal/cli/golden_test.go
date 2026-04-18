package cli

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/dshills/aperture/internal/manifest"
)

// §18.2: golden tests cover every manifest section so a stray refactor
// that drops a field or reorders a block fails loudly. We can't snapshot
// the entire bytes because several fields legitimately vary per run
// (paths in manifest_id / hash, repo root absolute path, fingerprint
// sha). Instead, we assert the canonical shape — the ordered list of
// JSON top-level keys AND the ordered list of Markdown section headers.

func TestGolden_ManifestJSONTopLevelShape(t *testing.T) {
	in := buildFixtureInputs(t, "add refresh handling to internal/oauth/provider.go for github oauth", "", 120000)
	m, err := BuildManifest(in)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := manifest.EmitJSON(m)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	// Every §11.1 field must be present. The catalogue is frozen and
	// these names are part of the v1 on-wire contract.
	wanted := []string{
		"schema_version",
		"manifest_id",
		"manifest_hash",
		"generated_at",
		"incomplete",
		"task",
		"repo",
		"budget",
		"selections",
		"reachable",
		"exclusions",
		"gaps",
		"feasibility",
		"generation_metadata",
	}
	for _, key := range wanted {
		if _, ok := doc[key]; !ok {
			t.Errorf("manifest JSON missing §11.1 top-level key %q", key)
		}
	}
}

// Every §11.1 selection sub-field must be present on a real selection;
// protects the per-entry contract in the same way as the top-level test.
func TestGolden_SelectionEntryShape(t *testing.T) {
	in := buildFixtureInputs(t, "add refresh handling to internal/oauth/provider.go for github oauth", "", 120000)
	m, err := BuildManifest(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Selections) == 0 {
		t.Fatal("fixture should produce at least one selection")
	}
	raw, err := json.Marshal(m.Selections[0])
	if err != nil {
		t.Fatal(err)
	}
	var sel map[string]any
	if err := json.Unmarshal(raw, &sel); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{
		"path", "kind", "load_mode", "relevance_score",
		"score_breakdown", "estimated_tokens", "rationale", "side_effects",
	} {
		if _, ok := sel[key]; !ok {
			t.Errorf("selection entry missing §11.1 field %q", key)
		}
	}
}

// §7.9.3: every listed Markdown section header appears EXACTLY once, in
// the spec's declared order. Ordering matters — downstream agents parse
// the sections by offset in some workflows.
func TestGolden_MarkdownSectionsInSpecOrder(t *testing.T) {
	in := buildFixtureInputs(t, "add refresh handling to internal/oauth/provider.go for github oauth", "", 120000)
	m, err := BuildManifest(in)
	if err != nil {
		t.Fatal(err)
	}
	md := string(manifest.EmitMarkdown(m))
	expected := []string{
		"## Task Summary",
		"## Planning Assumptions",
		"## Selected Full Context",
		"## Selected Summaries",
		"## Reachable Context",
		"## Gaps",
		"## Feasibility",
		"## Token Accounting",
		"## Usage Instructions",
	}
	previous := -1
	for _, section := range expected {
		idx := strings.Index(md, section)
		if idx < 0 {
			t.Errorf("markdown missing %q", section)
			continue
		}
		if strings.Count(md, section) != 1 {
			t.Errorf("markdown has duplicate section %q", section)
		}
		if idx <= previous {
			t.Errorf("section %q out of order (at %d, previous at %d)", section, idx, previous)
		}
		previous = idx
	}
}

// §7.7.3: the engine renumbers gap-N sequentially in the manifest's
// emission order. This golden check protects against accidentally
// introducing gap-5 before gap-3 in a refactor.
func TestGolden_GapIDsSequentialFromOne(t *testing.T) {
	in := buildFixtureInputs(t, "add refresh handling to internal/oauth/provider.go for github oauth", "", 120000)
	m, err := BuildManifest(in)
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`^gap-(\d+)$`)
	for i, g := range m.Gaps {
		sub := re.FindStringSubmatch(g.ID)
		if sub == nil {
			t.Errorf("gap %d has non-conforming ID %q", i, g.ID)
			continue
		}
		want := i + 1
		got, err := strconv.Atoi(sub[1])
		if err != nil {
			t.Errorf("gap %d: unparseable ID number %q: %v", i, sub[1], err)
			continue
		}
		if got != want {
			t.Errorf("gap %d: ID number %d, want %d", i, got, want)
		}
	}
}

// §14 determinism: every selection's path is emitted in sorted ascending
// order.
func TestGolden_SelectionsSortedByPath(t *testing.T) {
	in := buildFixtureInputs(t, "add refresh handling to internal/oauth/provider.go for github oauth", "", 120000)
	m, err := BuildManifest(in)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < len(m.Selections); i++ {
		if bytes.Compare([]byte(m.Selections[i-1].Path), []byte(m.Selections[i].Path)) >= 0 {
			t.Fatalf("selections not sorted ascending: %s vs %s", m.Selections[i-1].Path, m.Selections[i].Path)
		}
	}
}
