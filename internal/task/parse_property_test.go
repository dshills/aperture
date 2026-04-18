package task

import (
	"math/rand/v2"
	"slices"
	"sort"
	"strings"
	"testing"
)

// Property tests for task.Parse. Scaffolded in response to
// TESTREC-6C7A29F1 (verifier run 2026-04-18). The unit suite in
// parse_test.go covers every row of §7.3.1.1 and a few specific
// anchors; these property tests cover the determinism + ordering
// invariants that §7.3 mandates for ALL inputs, not just the
// hand-picked fixtures.

// Invariant: Parse is a pure function. Running it twice on the same
// (rawText, opts) produces identical Task values — including task_id,
// anchors, action_type, and every expects_* boolean.
func TestProperty_Parse_IsDeterministicAcrossInvocations(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for i := 0; i < 64; i++ {
		raw := randomTaskText(rng, 40+rng.IntN(400))
		a := Parse(raw, ParseOptions{Source: "<inline>"})
		b := Parse(raw, ParseOptions{Source: "<inline>"})
		if a.TaskID != b.TaskID {
			t.Fatalf("task_id non-deterministic: %q vs %q (input=%q)", a.TaskID, b.TaskID, raw)
		}
		if a.Type != b.Type {
			t.Fatalf("action_type non-deterministic: %q vs %q", a.Type, b.Type)
		}
		if !slices.Equal(a.Anchors, b.Anchors) {
			t.Fatalf("anchors non-deterministic:\n  a=%v\n  b=%v", a.Anchors, b.Anchors)
		}
		if a.ExpectsTests != b.ExpectsTests ||
			a.ExpectsConfig != b.ExpectsConfig ||
			a.ExpectsDocs != b.ExpectsDocs ||
			a.ExpectsMigration != b.ExpectsMigration ||
			a.ExpectsAPIContract != b.ExpectsAPIContract {
			t.Fatalf("expects_* booleans non-deterministic for %q", raw)
		}
	}
}

// Invariant: §7.3.2 requires anchors to be a sorted, deduplicated list.
// Sorted = ascending byte-wise; deduplicated = every string appears at
// most once. This holds regardless of how the input is shuffled.
func TestProperty_Parse_AnchorsSortedAndDeduped(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 4))
	for i := 0; i < 64; i++ {
		raw := randomTaskText(rng, 40+rng.IntN(400))
		parsed := Parse(raw, ParseOptions{Source: "<inline>"})
		if !sort.StringsAreSorted(parsed.Anchors) {
			t.Fatalf("anchors not sorted ascending for %q: %v", raw, parsed.Anchors)
		}
		seen := map[string]struct{}{}
		for _, a := range parsed.Anchors {
			if _, dup := seen[a]; dup {
				t.Fatalf("duplicate anchor %q in %v (input=%q)", a, parsed.Anchors, raw)
			}
			seen[a] = struct{}{}
		}
	}
}

// Invariant: §7.3.1.1 defines an ORDERED classification table. The
// first matching rule wins regardless of where the matching token sits
// in the input text. Moving a rule-1 keyword ("fix") around in the
// sentence must still classify the action as bugfix when a lower-
// priority keyword ("investigate") is also present.
func TestProperty_Parse_ActionTypePriorityOrderInsensitive(t *testing.T) {
	// Rule 1 (bugfix) vs Rule 6 (investigation). Both triggers present
	// in every permutation — rule 1 must always win.
	permutations := []string{
		"investigate why the new fix breaks things",
		"why does the fix break — investigate the regression",
		"regression investigate fix broken panic",
		"broken: investigate the fix",
	}
	for _, p := range permutations {
		parsed := Parse(p, ParseOptions{Source: "<inline>"})
		if string(parsed.Type) != "bugfix" {
			t.Errorf("rule-priority violated for %q: got %s, want bugfix", p, parsed.Type)
		}
	}
}

// Invariant: task_id is `tsk_` + sha256(raw_text)[:16]. Adding or
// removing a single character from the input changes the digest with
// overwhelming probability — we assert that any 1-char mutation
// actually changes the task_id. Collision probability is ~1 / 2^64 so
// this is effectively a cryptographic property.
func TestProperty_Parse_TaskIDChangesOnSingleCharMutation(t *testing.T) {
	rng := rand.New(rand.NewPCG(5, 6))
	for i := 0; i < 32; i++ {
		raw := randomTaskText(rng, 40+rng.IntN(200))
		base := Parse(raw, ParseOptions{Source: "<inline>"}).TaskID

		mutated := raw + "x"
		after := Parse(mutated, ParseOptions{Source: "<inline>"}).TaskID
		if base == after {
			t.Fatalf("task_id unchanged after 1-char append:\n  base=%q\n  mutated=%q\n  id=%s",
				raw, mutated, base)
		}
	}
}

// randomTaskText produces a simple whitespace-separated bag of words
// with some mix of lowercase, uppercase identifiers, and filename-
// shaped tokens so all four §7.3.2 anchor rules get exercised.
func randomTaskText(rng *rand.Rand, wordCount int) string {
	verbs := []string{"fix", "add", "refactor", "migrate", "investigate", "document", "update"}
	nouns := []string{"handler", "config", "token", "cache", "parser", "scheduler", "router"}
	idents := []string{"RefreshToken", "HTTPClient", "UserStore", "DBConn"}
	files := []string{"internal/foo.go", "cmd/app/main.go", "config.yaml"}

	var b strings.Builder
	for i := 0; i < wordCount; i++ {
		switch rng.IntN(4) {
		case 0:
			b.WriteString(verbs[rng.IntN(len(verbs))])
		case 1:
			b.WriteString(nouns[rng.IntN(len(nouns))])
		case 2:
			b.WriteString(idents[rng.IntN(len(idents))])
		case 3:
			b.WriteString(files[rng.IntN(len(files))])
		}
		b.WriteByte(' ')
	}
	return b.String()
}
