// Package loadmode encodes the §7.5.0 relevance / size bands and the
// §7.5.1–§7.5.4 eligibility rules used by the selector. All predicates
// here are pure; they read only the FileEntry, the score, and the
// precomputed token estimates.
package loadmode

import (
	"path"
	"strings"

	"github.com/dshills/aperture/internal/index"
	"github.com/dshills/aperture/internal/manifest"
)

// RelevanceBand names the score bands from §7.5.0.
type RelevanceBand string

const (
	HighlyRelevant     RelevanceBand = "highly"
	ModeratelyRelevant RelevanceBand = "moderately"
	PlausiblyRelevant  RelevanceBand = "plausibly"
	LowRelevance       RelevanceBand = "low"
)

// ClassifyScore returns the §7.5.0 band for a [0,1] score.
func ClassifyScore(score float64) RelevanceBand {
	switch {
	case score >= 0.80:
		return HighlyRelevant
	case score >= 0.60:
		return ModeratelyRelevant
	case score >= 0.30:
		return PlausiblyRelevant
	}
	return LowRelevance
}

// SizeBand names the §7.5.0 size buckets.
type SizeBand string

const (
	SizeSmall  SizeBand = "small"
	SizeMedium SizeBand = "medium"
	SizeLarge  SizeBand = "large"
)

// ClassifySize returns the §7.5.0 size band for a file.
func ClassifySize(bytes int64, estimatedTokensFull int) SizeBand {
	const (
		smallBytes  int64 = 8 * 1024
		mediumBytes int64 = 32 * 1024
	)
	if bytes <= smallBytes && estimatedTokensFull <= 2000 {
		return SizeSmall
	}
	if bytes <= mediumBytes && estimatedTokensFull <= 8000 {
		return SizeMedium
	}
	return SizeLarge
}

// Candidate is a scored file prepared for selection. Callers fill in the
// per-mode token costs; the eligibility predicates consume them.
type Candidate struct {
	File      *index.FileEntry
	Score     float64
	Band      RelevanceBand
	Size      SizeBand
	Mentioned bool // true if the task explicitly named the path/filename

	CostFull       int
	CostStructural int
	CostBehavioral int
}

// Eligibility returns the ordered list of load modes the candidate is
// eligible for, excluding budget considerations. The caller (selector)
// enforces the budget. `reachable` is appended only when the candidate
// is at least plausibly relevant.
func Eligibility(c Candidate) []manifest.LoadMode {
	out := make([]manifest.LoadMode, 0, 4)

	if eligibleFull(c) {
		out = append(out, manifest.LoadModeFull)
	}
	if eligibleStructural(c) {
		out = append(out, manifest.LoadModeStructuralSummary)
	}
	if eligibleBehavioral(c) {
		out = append(out, manifest.LoadModeBehavioralSummary)
	}
	// §7.5.4 reachable eligibility is expanded by the selector Pass 2,
	// which also promotes moderate/highly-relevant files that missed out
	// due to budget exhaustion. We expose the baseline here:
	if c.Band == PlausiblyRelevant {
		out = append(out, manifest.LoadModeReachable)
	}
	return out
}

// eligibleFull — §7.5.1: highly relevant OR explicitly mentioned, small/medium,
// and cost fits (the fit test lives in the selector, not here).
func eligibleFull(c Candidate) bool {
	if c.Band != HighlyRelevant && !c.Mentioned {
		return false
	}
	return c.Size == SizeSmall || c.Size == SizeMedium
}

// eligibleStructural — §7.5.2: highly or moderately relevant, Go file with a
// non-empty symbol table. The selector itself enforces the "not eligible for
// full" clause by preferring full when it fits the budget; structural acts
// as a secondary fallback for that file when budget or size rejects full.
func eligibleStructural(c Candidate) bool {
	if c.Band != HighlyRelevant && c.Band != ModeratelyRelevant {
		return false
	}
	if c.File == nil || c.File.Language != "go" || len(c.File.Symbols) == 0 {
		return false
	}
	return true
}

// eligibleBehavioral — §7.5.3: moderately relevant or higher, AND (not
// eligible for structural OR ≥2 side-effect tags OR deterministic role-
// pattern filename). The "not eligible for full" clause is handled by the
// selector: behavioral is only picked when full and structural are not.
func eligibleBehavioral(c Candidate) bool {
	if c.Band != HighlyRelevant && c.Band != ModeratelyRelevant {
		return false
	}
	if c.File == nil {
		return false
	}
	// Condition (1): not eligible for structural (non-Go OR empty
	// symbols OR parse error).
	if !eligibleStructural(c) {
		return true
	}
	// Condition (2): ≥ 2 tags from the io:* set.
	if countIOTags(c.File.SideEffects) >= 2 {
		return true
	}
	// Condition (3): role-pattern filename.
	return matchesRolePattern(c.File.Path)
}

// PreferStructural encodes the §7.5.3 tie-break: when both structural
// and behavioral eligibility fire, prefer structural for Go files with
// a non-empty symbol table, behavioral otherwise.
func PreferStructural(c Candidate) bool {
	return c.File != nil && c.File.Language == "go" && len(c.File.Symbols) > 0 && !c.File.ParseError
}

func countIOTags(tags []string) int {
	n := 0
	for _, t := range tags {
		if strings.HasPrefix(t, "io:") {
			n++
		}
	}
	return n
}

func matchesRolePattern(p string) bool {
	base := path.Base(p)
	lower := strings.ToLower(base)
	if lower == "main.go" || lower == "config.go" || lower == "server.go" || lower == "router.go" {
		return true
	}
	if strings.HasSuffix(lower, "_main.go") || strings.HasSuffix(lower, "_config.go") {
		return true
	}
	if strings.HasPrefix(lower, "config_") || strings.HasPrefix(lower, "handler") {
		return true
	}
	ext := strings.ToLower(path.Ext(p))
	if ext == ".md" || ext == ".rst" || ext == ".adoc" {
		return true
	}
	// cmd/*/main.go — already covered by main.go basename test.
	return false
}
