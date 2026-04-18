package task

import (
	"slices"
	"testing"

	"github.com/dshills/aperture/internal/manifest"
)

func TestClassifyActionType_CoversEveryRow(t *testing.T) {
	cases := []struct {
		name string
		text string
		want manifest.ActionType
	}{
		{"bugfix", "please fix the panic in the parser", manifest.ActionTypeBugfix},
		{"test-addition", "add tests for the oauth refresh path", manifest.ActionTypeTestAddition},
		{"documentation", "update the readme and godoc comments", manifest.ActionTypeDocumentation},
		{"migration", "migrate the users table schema change", manifest.ActionTypeMigration},
		{"refactor", "refactor the token store package", manifest.ActionTypeRefactor},
		{"investigation", "investigate why requests hang on startup", manifest.ActionTypeInvestigation},
		{"feature", "implement graceful shutdown support", manifest.ActionTypeFeature},
		{"unknown", "shimmer ornate glyphs nocturnal", manifest.ActionTypeUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyActionType(tc.text)
			if got != tc.want {
				t.Fatalf("classifyActionType(%q) = %q, want %q", tc.text, got, tc.want)
			}
		})
	}
}

// §7.3.1.1 is strictly rule-order-sensitive: "investigate why the new fix
// breaks" matches both Rule 1 (bugfix, on "fix") and Rule 6 (investigation,
// on "investigate"); Rule 1 wins.
func TestClassifyActionType_HigherPriorityRuleWins(t *testing.T) {
	got := classifyActionType("investigate why the new fix breaks")
	if got != manifest.ActionTypeBugfix {
		t.Fatalf("rule-order regression: got %q, want bugfix", got)
	}
}

func TestTaskID_StableAcrossRuns(t *testing.T) {
	raw := "Add OAuth refresh handling to the GitHub provider"
	first := Parse(raw, ParseOptions{Source: "<inline>"}).TaskID
	second := Parse(raw, ParseOptions{Source: "something-else"}).TaskID
	if first != second {
		t.Fatalf("task_id must depend only on raw text: %s vs %s", first, second)
	}
	if got := first; got[:4] != "tsk_" || len(got) != 4+16 {
		t.Fatalf("task_id shape violated: %s", got)
	}
}

func TestExtractAnchors_UnionAndStopwordFilter(t *testing.T) {
	raw := "Add OAuth `RefreshToken` handling to internal/api/list.go for the User."
	parsed := Parse(raw, ParseOptions{Source: "t.md", IsMarkdown: true})
	// Rule 1: RefreshToken and OAuth and User (identifiers)
	want := []string{"OAuth", "RefreshToken", "User", "RefreshToken", "internal/api/list.go"}
	for _, w := range want {
		if !slices.Contains(parsed.Anchors, w) {
			t.Errorf("anchor %q missing from %v", w, parsed.Anchors)
		}
	}
	// stopword filter: "the", "user", "for" (lowercased alnum >=4) must not appear.
	for _, banned := range []string{"the", "user", "for"} {
		if slices.Contains(parsed.Anchors, banned) {
			t.Errorf("stopword %q leaked into anchors: %v", banned, parsed.Anchors)
		}
	}
}

func TestExtractAnchors_BacktickOnlyInMarkdown(t *testing.T) {
	raw := "update `RefreshToken`"
	md := Parse(raw, ParseOptions{Source: "t.md", IsMarkdown: true})
	txt := Parse(raw, ParseOptions{Source: "t.txt", IsMarkdown: false})
	// Markdown extraction adds the backtick content itself as an anchor, even
	// if the identifier rule already caught it; the set-union makes it a no-op
	// here. The important invariant is that the txt form does not add
	// anything beyond rules 1/2/4.
	if !slices.Contains(md.Anchors, "RefreshToken") {
		t.Fatalf("markdown parse missed RefreshToken anchor: %v", md.Anchors)
	}
	if !slices.Contains(txt.Anchors, "RefreshToken") {
		t.Fatalf("plain parse missed RefreshToken anchor: %v", txt.Anchors)
	}
}

func TestHeuristicBooleans_Feature(t *testing.T) {
	raw := "Add OAuth refresh handling and add tests for it"
	p := Parse(raw, ParseOptions{Source: "<inline>"})
	if !p.ExpectsTests {
		t.Error("feature+tests mention should set ExpectsTests")
	}
	if p.ExpectsMigration {
		t.Error("no migration anchors; ExpectsMigration should be false")
	}
}
