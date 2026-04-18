// Package diff implements `aperture diff` per v1.1 SPEC §4.5, §7.6.
//
// The diff is read-only: it NEVER invokes the planner, NEVER opens the
// repository, NEVER recomputes the manifest hash. It operates purely
// on two manifest JSON payloads already in hand.
package diff

import (
	"sort"

	"github.com/dshills/aperture/internal/manifest"
)

// Diff is the structured output of Compute. It is deterministic: two
// runs on the same pair of manifests produce a byte-identical Diff
// (modulo the per-run metadata emitted alongside by the CLI).
//
// Every section is always present in the JSON and Markdown emitters
// per §7.6.2; empty sections render an "unchanged" marker so a
// consumer can distinguish "checked, no delta" from "not computed".
type Diff struct {
	// SemanticEquivalent reports whether manifest_hash(A) ==
	// manifest_hash(B) per §7.6.3. When true, every structural section
	// below MUST be empty except the per-run exempt fields.
	SemanticEquivalent bool

	// ToolBugDiagnostic, when non-empty, lists structural deltas
	// observed under hash equality. §7.6.3: this is a tool-level bug
	// (the manifest emitter diverged from the hash), never a user
	// error. Normally empty.
	ToolBugDiagnostic []string

	// Hash and ID section (§4.5).
	HashEqual       bool   // manifest_hash
	ManifestIDEqual bool   // manifest_id (informational; always allowed to differ)
	HashA           string // raw "sha256:..." from side A
	HashB           string // raw "sha256:..." from side B
	ConfigDigestA   string // raw config_digest A
	ConfigDigestB   string // raw config_digest B

	// Task delta.
	TaskAnchorsAdded   []string
	TaskAnchorsRemoved []string
	TaskTypeA          string
	TaskTypeB          string
	TaskTextFirstDiff  string // "" when equal; first differing line (truncated) otherwise

	// Repo delta.
	FingerprintEqual   bool
	FingerprintA       string
	FingerprintB       string
	LanguageHintsAdded []string
	LanguageHintsDropd []string

	// Budget delta.
	BudgetModelA      string
	BudgetModelB      string
	TokenCeilingA     int
	TokenCeilingB     int
	EffectiveContextA int
	EffectiveContextB int
	EstimatorA        string
	EstimatorB        string

	// Scope delta (v1.1 additive).
	ScopeA string // "" when absent
	ScopeB string

	// Selection delta.
	SelectionsAdded       []SelectionEntry
	SelectionsRemoved     []SelectionEntry
	SelectionsLoadChanged []SelectionChange

	// Reachable delta (same shape as selections).
	ReachableAdded    []ReachableEntry
	ReachableRemoved  []ReachableEntry
	ReachablePromoted []string // paths moved from reachable → selection

	// Gap delta.
	GapsAdded           []GapEntry
	GapsResolved        []GapEntry
	GapsSeverityChanged []GapSeverityChange

	// Feasibility delta.
	FeasibilityScoreA float64
	FeasibilityScoreB float64
	FeasibilityDeltas []SubSignalDelta

	// Generation metadata.
	ApertureVersionA string
	ApertureVersionB string
	SelectionLogicA  string
	SelectionLogicB  string

	// ResolvedConfigWeightsA / ...B are populated only when
	// ConfigDigestA != ConfigDigestB per §4.5. We extract the scoring
	// weights block from each manifest's GenerationMetadata-adjacent
	// data when available; when neither manifest carries the block
	// inline the emitters render the digest difference alone.
	//
	// Phase 5 note: v1.0 / v1.1 manifests do NOT embed the full
	// resolved config; the digest is the only on-manifest config
	// evidence. The "print resolved weights" requirement from §4.5 is
	// therefore satisfied at the emitter by showing the digest pair
	// and a clear "resolved config weights are not embedded in the
	// manifest; digest difference is the authoritative signal"
	// footnote. A future v1.x that embeds resolved config would fill
	// these fields.
	ResolvedConfigA string
	ResolvedConfigB string
}

// SelectionEntry is a concise view of a selection used in diff output.
type SelectionEntry struct {
	Path           string   `json:"path"`
	LoadMode       string   `json:"load_mode"`
	RelevanceScore float64  `json:"relevance_score"`
	Rationale      []string `json:"rationale,omitempty"`
}

// SelectionChange captures a load_mode transition for a path present
// on both sides.
type SelectionChange struct {
	Path       string  `json:"path"`
	LoadModeA  string  `json:"load_mode_a"`
	LoadModeB  string  `json:"load_mode_b"`
	RelevanceA float64 `json:"relevance_a"`
	RelevanceB float64 `json:"relevance_b"`
}

// ReachableEntry is a concise view of a reachable manifest entry.
type ReachableEntry struct {
	Path           string  `json:"path"`
	RelevanceScore float64 `json:"relevance_score"`
	Reason         string  `json:"reason"`
}

// GapEntry is a concise view of a manifest gap.
type GapEntry struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Severity string `json:"severity"`
}

// GapSeverityChange names a gap whose severity shifted between runs.
type GapSeverityChange struct {
	Type      string `json:"type"`
	SeverityA string `json:"severity_a"`
	SeverityB string `json:"severity_b"`
}

// SubSignalDelta names a feasibility sub-signal whose value changed.
type SubSignalDelta struct {
	Name   string  `json:"name"`
	ValueA float64 `json:"value_a"`
	ValueB float64 `json:"value_b"`
}

// Compute returns the Diff between two manifests. Neither input is
// mutated. The result is stable-ordered (lexicographic on path/name
// inside every section) so repeat calls are byte-identical.
func Compute(a, b *manifest.Manifest) *Diff {
	d := &Diff{
		HashA:             a.ManifestHash,
		HashB:             b.ManifestHash,
		ConfigDigestA:     a.GenerationMetadata.ConfigDigest,
		ConfigDigestB:     b.GenerationMetadata.ConfigDigest,
		TaskTypeA:         string(a.Task.Type),
		TaskTypeB:         string(b.Task.Type),
		FingerprintA:      a.Repo.Fingerprint,
		FingerprintB:      b.Repo.Fingerprint,
		BudgetModelA:      a.Budget.Model,
		BudgetModelB:      b.Budget.Model,
		TokenCeilingA:     a.Budget.TokenCeiling,
		TokenCeilingB:     b.Budget.TokenCeiling,
		EffectiveContextA: a.Budget.EffectiveContextBudget,
		EffectiveContextB: b.Budget.EffectiveContextBudget,
		EstimatorA:        a.Budget.Estimator,
		EstimatorB:        b.Budget.Estimator,
		ApertureVersionA:  a.GenerationMetadata.ApertureVersion,
		ApertureVersionB:  b.GenerationMetadata.ApertureVersion,
		SelectionLogicA:   a.GenerationMetadata.SelectionLogicVersion,
		SelectionLogicB:   b.GenerationMetadata.SelectionLogicVersion,
	}
	d.HashEqual = a.ManifestHash == b.ManifestHash
	d.ManifestIDEqual = a.ManifestID == b.ManifestID
	d.FingerprintEqual = a.Repo.Fingerprint == b.Repo.Fingerprint
	if a.Scope != nil {
		d.ScopeA = a.Scope.Path
	}
	if b.Scope != nil {
		d.ScopeB = b.Scope.Path
	}
	d.SemanticEquivalent = d.HashEqual

	// Task anchors (sets).
	d.TaskAnchorsAdded, d.TaskAnchorsRemoved = diffStringSets(a.Task.Anchors, b.Task.Anchors)
	d.TaskTextFirstDiff = firstDiffLine(a.Task.RawText, b.Task.RawText)

	// Language hints.
	d.LanguageHintsAdded, d.LanguageHintsDropd = diffStringSets(a.Repo.LanguageHints, b.Repo.LanguageHints)

	// Selections.
	d.SelectionsAdded, d.SelectionsRemoved, d.SelectionsLoadChanged = diffSelections(a.Selections, b.Selections)

	// Reachable.
	d.ReachableAdded, d.ReachableRemoved, d.ReachablePromoted = diffReachable(a, b)

	// Gaps.
	d.GapsAdded, d.GapsResolved, d.GapsSeverityChanged = diffGaps(a.Gaps, b.Gaps)

	// Feasibility.
	d.FeasibilityScoreA = a.Feasibility.Score
	d.FeasibilityScoreB = b.Feasibility.Score
	d.FeasibilityDeltas = diffSubSignals(a.Feasibility.SubSignals, b.Feasibility.SubSignals)

	// §7.6.3 fast-path: when hashes agree, every non-exempt delta
	// MUST be empty. Any observed delta surfaces as a tool-bug
	// diagnostic so a silent emitter regression can't hide.
	if d.SemanticEquivalent {
		d.ToolBugDiagnostic = detectToolBugs(d)
	}
	return d
}

// detectToolBugs flags any non-empty structural delta observed under
// hash equality. §7.9.4 exempts manifest_id, generated_at,
// generation_metadata.{host,pid,wall_clock_started_at}, and
// aperture_version from the hash — every OTHER field contributing to
// manifest_hash must therefore be equal when hashes are equal. A
// violation is a manifest-emitter bug, not a user-level delta.
func detectToolBugs(d *Diff) []string {
	var out []string
	// ConfigDigest is part of generation_metadata but participates in
	// the hash input (§7.9.4 excludes aperture_version, host, pid,
	// wall_clock_started_at — not config_digest). So unequal digests
	// under equal hashes is a bug.
	if d.ConfigDigestA != d.ConfigDigestB {
		out = append(out, "config_digest differs under equal manifest_hash")
	}
	if d.FingerprintA != d.FingerprintB {
		out = append(out, "repo.fingerprint differs under equal manifest_hash")
	}
	if d.ScopeA != d.ScopeB {
		out = append(out, "scope.path differs under equal manifest_hash")
	}
	if d.TaskTypeA != d.TaskTypeB || d.TaskTextFirstDiff != "" {
		out = append(out, "task differs under equal manifest_hash")
	}
	if len(d.TaskAnchorsAdded)+len(d.TaskAnchorsRemoved) > 0 {
		out = append(out, "task anchors differ under equal manifest_hash")
	}
	if len(d.SelectionsAdded)+len(d.SelectionsRemoved)+len(d.SelectionsLoadChanged) > 0 {
		out = append(out, "selections differ under equal manifest_hash")
	}
	if len(d.ReachableAdded)+len(d.ReachableRemoved)+len(d.ReachablePromoted) > 0 {
		out = append(out, "reachable differs under equal manifest_hash")
	}
	if len(d.GapsAdded)+len(d.GapsResolved)+len(d.GapsSeverityChanged) > 0 {
		out = append(out, "gaps differ under equal manifest_hash")
	}
	if d.FeasibilityScoreA != d.FeasibilityScoreB || len(d.FeasibilityDeltas) > 0 {
		out = append(out, "feasibility differs under equal manifest_hash")
	}
	if d.SelectionLogicA != d.SelectionLogicB {
		out = append(out, "selection_logic_version differs under equal manifest_hash")
	}
	sort.Strings(out)
	return out
}
