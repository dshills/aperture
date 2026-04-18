package diff

import (
	"encoding/json"
	"sort"
)

// jsonShape mirrors Diff but enforces the exact §4.5 field order and
// emission contract. Slices are always allocated (never nil) so the
// JSON carries `"selections_added": []` rather than `null` — this is
// what determinism tests diff against.
type jsonShape struct {
	SchemaVersion      string   `json:"schema_version"`
	SemanticEquivalent bool     `json:"semantic_equivalent"`
	ToolBugDiagnostic  []string `json:"tool_bug_diagnostic"`

	Hash      section `json:"hash"`
	Task      section `json:"task"`
	Repo      section `json:"repo"`
	Budget    section `json:"budget"`
	Scope     section `json:"scope"`
	Selection section `json:"selections"`
	Reachable section `json:"reachable"`
	Gaps      section `json:"gaps"`
	Feas      section `json:"feasibility"`
	Gen       section `json:"generation_metadata"`
}

// section carries a generic "unchanged" marker plus an arbitrary
// payload map. Keeping one shape across sections makes the contract
// obvious and renders "unchanged" uniformly.
type section struct {
	Unchanged bool                   `json:"unchanged"`
	Details   map[string]any `json:"details,omitempty"`
}

// EmitJSON returns the deterministic JSON rendering of the Diff. The
// output is pretty-printed (2-space indent) and terminates with a
// single newline so `git diff` and other tools handle it cleanly.
func EmitJSON(d *Diff) ([]byte, error) {
	s := toJSONShape(d)
	buf, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(buf, '\n'), nil
}

// diffSchemaVersion is the committed schema version for the `aperture
// diff` JSON output. Bumped only when the output structure changes
// incompatibly.
const diffSchemaVersion = "1.0"

func toJSONShape(d *Diff) *jsonShape {
	out := &jsonShape{
		SchemaVersion:      diffSchemaVersion,
		SemanticEquivalent: d.SemanticEquivalent,
		ToolBugDiagnostic:  emptyStringsIfNil(d.ToolBugDiagnostic),
	}

	out.Hash = hashSection(d)
	out.Task = taskSection(d)
	out.Repo = repoSection(d)
	out.Budget = budgetSection(d)
	out.Scope = scopeSection(d)
	out.Selection = selectionSection(d)
	out.Reachable = reachableSection(d)
	out.Gaps = gapsSection(d)
	out.Feas = feasibilitySection(d)
	out.Gen = generationSection(d)

	return out
}

func hashSection(d *Diff) section {
	if d.HashEqual && d.ManifestIDEqual && d.ConfigDigestA == d.ConfigDigestB {
		return section{Unchanged: true}
	}
	return section{Details: map[string]any{
		"manifest_hash_a":  d.HashA,
		"manifest_hash_b":  d.HashB,
		"manifest_hash_eq": d.HashEqual,
		"manifest_id_eq":   d.ManifestIDEqual,
		"config_digest_a":  d.ConfigDigestA,
		"config_digest_b":  d.ConfigDigestB,
		"config_digest_eq": d.ConfigDigestA == d.ConfigDigestB,
	}}
}

func taskSection(d *Diff) section {
	if len(d.TaskAnchorsAdded) == 0 && len(d.TaskAnchorsRemoved) == 0 &&
		d.TaskTypeA == d.TaskTypeB && d.TaskTextFirstDiff == "" {
		return section{Unchanged: true}
	}
	details := map[string]any{
		"anchors_added":   emptyStringsIfNil(d.TaskAnchorsAdded),
		"anchors_removed": emptyStringsIfNil(d.TaskAnchorsRemoved),
		"type_a":          d.TaskTypeA,
		"type_b":          d.TaskTypeB,
	}
	if d.TaskTextFirstDiff != "" {
		details["text_first_diff"] = d.TaskTextFirstDiff
	}
	return section{Details: details}
}

func repoSection(d *Diff) section {
	if d.FingerprintEqual && len(d.LanguageHintsAdded) == 0 && len(d.LanguageHintsDropd) == 0 {
		return section{Unchanged: true}
	}
	return section{Details: map[string]any{
		"fingerprint_a":          d.FingerprintA,
		"fingerprint_b":          d.FingerprintB,
		"fingerprint_eq":         d.FingerprintEqual,
		"language_hints_added":   emptyStringsIfNil(d.LanguageHintsAdded),
		"language_hints_removed": emptyStringsIfNil(d.LanguageHintsDropd),
	}}
}

func budgetSection(d *Diff) section {
	if d.BudgetModelA == d.BudgetModelB &&
		d.TokenCeilingA == d.TokenCeilingB &&
		d.EffectiveContextA == d.EffectiveContextB &&
		d.EstimatorA == d.EstimatorB {
		return section{Unchanged: true}
	}
	return section{Details: map[string]any{
		"model_a":             d.BudgetModelA,
		"model_b":             d.BudgetModelB,
		"token_ceiling_a":     d.TokenCeilingA,
		"token_ceiling_b":     d.TokenCeilingB,
		"effective_context_a": d.EffectiveContextA,
		"effective_context_b": d.EffectiveContextB,
		"estimator_a":         d.EstimatorA,
		"estimator_b":         d.EstimatorB,
	}}
}

func scopeSection(d *Diff) section {
	if d.ScopeA == d.ScopeB {
		return section{Unchanged: true}
	}
	return section{Details: map[string]any{
		"scope_a": d.ScopeA,
		"scope_b": d.ScopeB,
	}}
}

func selectionSection(d *Diff) section {
	if len(d.SelectionsAdded) == 0 && len(d.SelectionsRemoved) == 0 && len(d.SelectionsLoadChanged) == 0 {
		return section{Unchanged: true}
	}
	return section{Details: map[string]any{
		"added":             selectionEntriesOrEmpty(d.SelectionsAdded),
		"removed":           selectionEntriesOrEmpty(d.SelectionsRemoved),
		"load_mode_changed": selectionChangesOrEmpty(d.SelectionsLoadChanged),
	}}
}

func reachableSection(d *Diff) section {
	if len(d.ReachableAdded) == 0 && len(d.ReachableRemoved) == 0 && len(d.ReachablePromoted) == 0 {
		return section{Unchanged: true}
	}
	return section{Details: map[string]any{
		"added":    reachableEntriesOrEmpty(d.ReachableAdded),
		"removed":  reachableEntriesOrEmpty(d.ReachableRemoved),
		"promoted": emptyStringsIfNil(d.ReachablePromoted),
	}}
}

func gapsSection(d *Diff) section {
	if len(d.GapsAdded) == 0 && len(d.GapsResolved) == 0 && len(d.GapsSeverityChanged) == 0 {
		return section{Unchanged: true}
	}
	return section{Details: map[string]any{
		"added":            gapEntriesOrEmpty(d.GapsAdded),
		"resolved":         gapEntriesOrEmpty(d.GapsResolved),
		"severity_changed": gapSeverityChangesOrEmpty(d.GapsSeverityChanged),
	}}
}

func feasibilitySection(d *Diff) section {
	if d.FeasibilityScoreA == d.FeasibilityScoreB && len(d.FeasibilityDeltas) == 0 {
		return section{Unchanged: true}
	}
	return section{Details: map[string]any{
		"score_a":     d.FeasibilityScoreA,
		"score_b":     d.FeasibilityScoreB,
		"sub_signals": subSignalsOrEmpty(d.FeasibilityDeltas),
	}}
}

func generationSection(d *Diff) section {
	if d.ApertureVersionA == d.ApertureVersionB && d.SelectionLogicA == d.SelectionLogicB {
		return section{Unchanged: true}
	}
	return section{Details: map[string]any{
		"aperture_version_a":        d.ApertureVersionA,
		"aperture_version_b":        d.ApertureVersionB,
		"selection_logic_version_a": d.SelectionLogicA,
		"selection_logic_version_b": d.SelectionLogicB,
	}}
}

// ----- small helpers that keep JSON output deterministic -----

func emptyStringsIfNil(in []string) []string {
	if in == nil {
		return []string{}
	}
	out := append([]string{}, in...)
	sort.Strings(out)
	return out
}

func selectionEntriesOrEmpty(in []SelectionEntry) []SelectionEntry {
	if in == nil {
		return []SelectionEntry{}
	}
	return in
}

func selectionChangesOrEmpty(in []SelectionChange) []SelectionChange {
	if in == nil {
		return []SelectionChange{}
	}
	return in
}

func reachableEntriesOrEmpty(in []ReachableEntry) []ReachableEntry {
	if in == nil {
		return []ReachableEntry{}
	}
	return in
}

func gapEntriesOrEmpty(in []GapEntry) []GapEntry {
	if in == nil {
		return []GapEntry{}
	}
	return in
}

func gapSeverityChangesOrEmpty(in []GapSeverityChange) []GapSeverityChange {
	if in == nil {
		return []GapSeverityChange{}
	}
	return in
}

func subSignalsOrEmpty(in []SubSignalDelta) []SubSignalDelta {
	if in == nil {
		return []SubSignalDelta{}
	}
	return in
}
