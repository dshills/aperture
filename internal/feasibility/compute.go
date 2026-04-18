// Package feasibility implements the deterministic feasibility score
// from SPEC §7.8.2.1. The function is pure — no filesystem, no LLM, no
// randomness — and returns every numeric sub-signal so callers can
// populate the manifest's positives/negatives/blocking_conditions
// enumerations without a second pass.
package feasibility

import (
	"strings"

	"github.com/dshills/aperture/internal/index"
	"github.com/dshills/aperture/internal/manifest"
	"github.com/dshills/aperture/internal/selection"
	"github.com/dshills/aperture/internal/task"
)

// Inputs snapshots everything the §7.8.2.1 algorithm needs.
type Inputs struct {
	Task                    task.Task
	Index                   *index.Index
	Assignments             []selection.Assignment
	EffectiveContextBudget  int
	EstimatedSelectedTokens int
	Gaps                    []manifest.Gap
}

// SubSignals holds the per-factor numeric values exactly as §7.8.2.1
// defines them. Every value is in [0, 1] except GapPenalty which is in
// [0, 0.50].
type SubSignals struct {
	Coverage         float64 `json:"coverage"`
	AnchorResolution float64 `json:"anchor_resolution"`
	TaskSpecificity  float64 `json:"task_specificity"`
	BudgetHeadroom   float64 `json:"budget_headroom"`
	GapPenalty       float64 `json:"gap_penalty"`
}

// Result is the full feasibility output. The score is already clamped
// (including the blocking-gap ≤0.40 rule).
type Result struct {
	Score      float64
	SubSignals SubSignals
	Assessment string
}

// expectedPrimaryFiles maps §7.8.2.1's per-action-type baseline.
var expectedPrimaryFiles = map[manifest.ActionType]int{
	manifest.ActionTypeBugfix:        3,
	manifest.ActionTypeFeature:       4,
	manifest.ActionTypeRefactor:      5,
	manifest.ActionTypeTestAddition:  2,
	manifest.ActionTypeDocumentation: 2,
	manifest.ActionTypeInvestigation: 3,
	manifest.ActionTypeMigration:     4,
	manifest.ActionTypeUnknown:       3,
}

// Compute returns the resolved feasibility per §7.8.2.1.
func Compute(in Inputs) Result {
	sub := SubSignals{
		Coverage:         coverage(in),
		AnchorResolution: anchorResolution(in),
		TaskSpecificity:  taskSpecificity(in),
		BudgetHeadroom:   budgetHeadroom(in),
		GapPenalty:       gapPenalty(in.Gaps),
	}
	score := clamp01(0.40*sub.Coverage+
		0.25*sub.AnchorResolution+
		0.20*sub.TaskSpecificity+
		0.15*sub.BudgetHeadroom) - sub.GapPenalty

	// Blocking-gap clamp (§7.8.2.1 final paragraph): if any blocking gap
	// fired, feasibility must not exceed 0.40.
	if hasBlocking(in.Gaps) && score > 0.40 {
		score = 0.40
	}
	if score < 0 {
		score = 0
	}
	return Result{Score: score, SubSignals: sub, Assessment: assessment(score)}
}

// coverage = min(1, (count_full + 0.5·count_structural + 0.3·count_behavioral)
//
//	/ max(3, expected_primary_files))
func coverage(in Inputs) float64 {
	var full, structural, behavioral float64
	for _, a := range in.Assignments {
		switch a.LoadMode {
		case manifest.LoadModeFull:
			full++
		case manifest.LoadModeStructuralSummary:
			structural++
		case manifest.LoadModeBehavioralSummary:
			behavioral++
		}
	}
	expected := expectedPrimaryFiles[in.Task.Type]
	if expected < 3 {
		expected = 3
	}
	v := (full + 0.5*structural + 0.3*behavioral) / float64(expected)
	if v > 1 {
		v = 1
	}
	return v
}

// anchorResolution = fraction of task anchors that resolve against at
// least one non-reachable selection. An anchor resolves when ONE of:
//
//   - its lowercase form is an exact path segment of a selection's path
//     (e.g. "oauth" in "internal/oauth/provider.go")
//   - its lowercase form matches the basename with or without extension
//     (e.g. "provider" or "provider.go")
//   - it is a case-insensitive substring of any exported Go symbol name
//     exported by a selected file (preserves "refresh" → "RefreshToken")
//
// Path matching is exact-segment rather than substring to avoid noise
// like "io" lighting up against "priority"; symbol matching remains
// substring because task text uses verbs ("refresh") and code uses
// nouns ("RefreshToken").
func anchorResolution(in Inputs) float64 {
	if len(in.Task.Anchors) == 0 {
		return 0
	}
	pathSegments, fullPaths, symbolBlob := buildSelectionSearchIndex(in)
	resolved := 0
	for _, anchor := range in.Task.Anchors {
		lower := strings.ToLower(anchor)
		if _, ok := pathSegments[lower]; ok {
			resolved++
			continue
		}
		// Anchors that carry a slash are explicit path mentions (e.g.
		// "internal/oauth/provider.go" or "oauth/provider.go"); match
		// them against the full selection paths, either exactly or as a
		// trailing suffix preceded by a path boundary so "auth/x.go"
		// doesn't light up against "cauth/x.go".
		if strings.ContainsRune(lower, '/') {
			if _, ok := fullPaths[lower]; ok {
				resolved++
				continue
			}
			// Anchor-with-slash matches any selection where the anchor
			// appears as a contiguous path-segment run: either at the
			// tail ("/internal/oauth"), at the head ("internal/oauth/…"),
			// or embedded in the middle ("pkg/internal/oauth/x.go"). The
			// exact-match case is already handled by the lookup above.
			matched := false
			for fp := range fullPaths {
				if strings.HasSuffix(fp, "/"+lower) ||
					strings.HasPrefix(fp, lower+"/") ||
					strings.Contains(fp, "/"+lower+"/") {
					matched = true
					break
				}
			}
			if matched {
				resolved++
				continue
			}
		}
		if strings.Contains(symbolBlob, lower) {
			resolved++
		}
	}
	return float64(resolved) / float64(len(in.Task.Anchors))
}

// buildSelectionSearchIndex returns:
//
//   - pathSegments: a set of lowercased path-component tokens (directory
//     names, basenames with and without extension) from every
//     non-reachable selection. Used for exact-segment anchor matching.
//   - symbolBlob: one lowercased concatenation of every exported symbol
//     name across the selected files, separated by a non-textual byte so
//     anchors can't match across symbol boundaries. Used for substring
//     symbol matching.
//
// Computed once per anchorResolution call so the anchor loop is O(anchors)
// rather than O(anchors × (paths + symbols)).
func buildSelectionSearchIndex(in Inputs) (segments map[string]struct{}, fullPaths map[string]struct{}, symbolBlob string) {
	segments = map[string]struct{}{}
	fullPaths = map[string]struct{}{}
	// Pre-size the symbol blob so strings.Builder doesn't grow-and-copy
	// its backing buffer for symbol-heavy packages. Average symbol name
	// is ~12 chars; +1 for the separator byte.
	blobSize := 0
	for _, a := range in.Assignments {
		if a.LoadMode == manifest.LoadModeReachable {
			continue
		}
		if f := in.Index.File(a.Path); f != nil {
			for _, s := range f.Symbols {
				blobSize += len(s.Name) + 1
			}
		}
	}
	var blob strings.Builder
	blob.Grow(blobSize)
	for _, a := range in.Assignments {
		if a.LoadMode == manifest.LoadModeReachable {
			continue
		}
		lower := strings.ToLower(a.Path)
		fullPaths[lower] = struct{}{}
		parts := strings.Split(lower, "/")
		for _, seg := range parts {
			if seg != "" {
				segments[seg] = struct{}{}
			}
		}
		// The basename is the last non-empty split segment, so the loop
		// above already added it; we only need to synthesize the
		// extension-stripped form separately so anchors like "provider"
		// match "provider.go" without re-calling path.Base.
		if len(parts) > 0 {
			base := parts[len(parts)-1]
			if dot := strings.LastIndexByte(base, '.'); dot > 0 {
				segments[base[:dot]] = struct{}{}
			}
		}
		f := in.Index.File(a.Path)
		if f == nil {
			continue
		}
		for _, s := range f.Symbols {
			blob.WriteString(strings.ToLower(s.Name))
			blob.WriteByte('\x1f')
		}
	}
	return segments, fullPaths, blob.String()
}

// taskSpecificity mirrors §7.8.2.1's ladder:
//
//	1.0 if ≥3 anchors + resolved action in {bugfix, feature, refactor,
//	     test-addition, migration} + ≥1 explicit file/path mention
//	0.7 if ≥2 anchors + resolved action type
//	0.4 if ≥1 anchor
//	0.1 otherwise (action_type defaults to unknown)
func taskSpecificity(in Inputs) float64 {
	anchors := len(in.Task.Anchors)
	actionResolved := in.Task.Type != manifest.ActionTypeUnknown
	specificAction := false
	switch in.Task.Type {
	case manifest.ActionTypeBugfix, manifest.ActionTypeFeature,
		manifest.ActionTypeRefactor, manifest.ActionTypeTestAddition,
		manifest.ActionTypeMigration:
		specificAction = true
	}
	explicitPath := taskHasExplicitPathMention(in.Task)

	switch {
	case anchors >= 3 && specificAction && explicitPath:
		return 1.0
	case anchors >= 2 && actionResolved:
		return 0.7
	case anchors >= 1:
		return 0.4
	}
	return 0.1
}

// budgetHeadroom = clamp01((effective − estimated_selected) / effective)
func budgetHeadroom(in Inputs) float64 {
	if in.EffectiveContextBudget <= 0 {
		return 0
	}
	v := float64(in.EffectiveContextBudget-in.EstimatedSelectedTokens) / float64(in.EffectiveContextBudget)
	return clamp01(v)
}

// gapPenalty = 0.05·warning + 0.20·blocking, capped at 0.50.
func gapPenalty(gaps []manifest.Gap) float64 {
	var warn, block int
	for _, g := range gaps {
		switch g.Severity {
		case manifest.GapSeverityWarning:
			warn++
		case manifest.GapSeverityBlocking:
			block++
		}
	}
	p := 0.05*float64(warn) + 0.20*float64(block)
	if p > 0.50 {
		p = 0.50
	}
	return p
}

func hasBlocking(gaps []manifest.Gap) bool {
	for _, g := range gaps {
		if g.Severity == manifest.GapSeverityBlocking {
			return true
		}
	}
	return false
}

func clamp01(f float64) float64 {
	switch {
	case f < 0:
		return 0
	case f > 1:
		return 1
	}
	return f
}

// taskHasExplicitPathMention returns true if the task text contains any
// token resembling a filename / path. §7.3.2 Rule 2 already extracts
// such tokens into the anchor set — we reuse that rather than
// re-scanning the raw text.
//
// A token qualifies if EITHER:
//   - it contains a forward slash (e.g. "internal/foo.go", "cmd/app"), or
//   - it carries a recognized file extension (via the §7.3.2 Rule 2
//     whitelist: go, md, yaml, yml, json, toml, proto, sql, ts, tsx,
//     js, py, sh).
//
// The bare-dot check the earlier version used fired on version strings
// like "v1.0" and hostname-style anchors; requiring a known extension
// avoids those false positives without losing real file mentions.
func taskHasExplicitPathMention(t task.Task) bool {
	for _, a := range t.Anchors {
		if strings.ContainsRune(a, '/') {
			return true
		}
		if hasKnownFileExtension(a) {
			return true
		}
	}
	return false
}

var knownTaskFileExts = []string{
	".go", ".md", ".yaml", ".yml", ".json", ".toml",
	".proto", ".sql", ".ts", ".tsx", ".js", ".py", ".sh",
}

func hasKnownFileExtension(s string) bool {
	lower := strings.ToLower(s)
	for _, ext := range knownTaskFileExts {
		if strings.HasSuffix(lower, ext) && len(lower) > len(ext) {
			return true
		}
	}
	return false
}

// assessment returns the §7.8.3 band label for display. Bands:
//
//	≥0.85 high; 0.65–0.84 moderate; 0.40–0.64 weak; <0.40 poor.
func assessment(score float64) string {
	switch {
	case score >= 0.85:
		return "high feasibility"
	case score >= 0.65:
		return "moderate feasibility"
	case score >= 0.40:
		return "weak feasibility"
	}
	return "poor feasibility"
}
