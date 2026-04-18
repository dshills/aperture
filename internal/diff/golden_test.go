package diff

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestGolden_CommittedPairs runs Compute on each committed manifest
// pair under testdata/fixtures/manifests/ and asserts the expected
// structural outcome for each pair. The pairs are committed as JSON
// (per PLAN §Phase 5 "commit only the JSON goldens") and the
// structural expectations live here.
func TestGolden_CommittedPairs(t *testing.T) {
	cases := []struct {
		pair               string
		wantSemanticEquiv  bool
		wantHashEqual      bool
		wantSelectionDelta bool
		wantScopeDelta     bool
		wantDigestDelta    bool
	}{
		{"identical", true, true, false, false, false},
		{"config-digest-diff", false, false, false, false, true},
		{"selection-diff", false, false, true, false, false},
		{"scope-delta", false, false, false, true, false},
	}
	base, err := filepath.Abs("../../testdata/fixtures/manifests")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		t.Run(c.pair, func(t *testing.T) {
			a, err := LoadManifestFile(filepath.Join(base, c.pair, "a.json"))
			if err != nil {
				t.Fatalf("load a.json: %v", err)
			}
			b, err := LoadManifestFile(filepath.Join(base, c.pair, "b.json"))
			if err != nil {
				t.Fatalf("load b.json: %v", err)
			}
			d := Compute(a, b)
			if d.SemanticEquivalent != c.wantSemanticEquiv {
				t.Errorf("semantic_equivalent=%v, want %v", d.SemanticEquivalent, c.wantSemanticEquiv)
			}
			if d.HashEqual != c.wantHashEqual {
				t.Errorf("hash_equal=%v, want %v", d.HashEqual, c.wantHashEqual)
			}
			gotSelectionDelta := len(d.SelectionsAdded)+len(d.SelectionsRemoved)+len(d.SelectionsLoadChanged) > 0
			if gotSelectionDelta != c.wantSelectionDelta {
				t.Errorf("selection delta=%v, want %v (added=%v removed=%v changed=%v)",
					gotSelectionDelta, c.wantSelectionDelta, d.SelectionsAdded, d.SelectionsRemoved, d.SelectionsLoadChanged)
			}
			gotScopeDelta := d.ScopeA != d.ScopeB
			if gotScopeDelta != c.wantScopeDelta {
				t.Errorf("scope delta=%v, want %v (A=%q B=%q)", gotScopeDelta, c.wantScopeDelta, d.ScopeA, d.ScopeB)
			}
			gotDigestDelta := d.ConfigDigestA != d.ConfigDigestB
			if gotDigestDelta != c.wantDigestDelta {
				t.Errorf("config_digest delta=%v, want %v", gotDigestDelta, c.wantDigestDelta)
			}

			// Markdown output must contain the semantic_equivalent
			// banner and every required section title — render at
			// test time so we don't commit Markdown goldens (per
			// PLAN).
			md := string(EmitMarkdown(d))
			if c.wantSemanticEquiv && !strings.Contains(md, "semantic_equivalent: true") {
				t.Errorf("markdown missing semantic_equivalent: true banner")
			}
			if !c.wantSemanticEquiv && !strings.Contains(md, "semantic_equivalent: false") {
				t.Errorf("markdown missing semantic_equivalent: false banner")
			}
			for _, sec := range []string{"Hash and ID", "Task", "Repo", "Budget", "Scope", "Selections", "Reachable", "Gaps", "Feasibility", "Generation metadata"} {
				if !strings.Contains(md, "## "+sec) {
					t.Errorf("markdown missing `## %s` section", sec)
				}
			}
		})
	}
}
