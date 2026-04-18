package eval

import (
	"path"
	"sort"

	"github.com/dshills/aperture/internal/manifest"
)

// Metrics are the per-fixture quality numbers emitted for each run.
type Metrics struct {
	Precision float64
	Recall    float64
	F1        float64
}

// Verdict carries the fixture outcome: a hard-fail flag and, when failed,
// a list of reasons. Hard failures (forbidden path at score ≥ 0.30, or
// a missing required gap) override F1 per §7.1.2.
type Verdict struct {
	HardFail       bool
	HardFailReason []string
	Metrics        Metrics
}

// forbiddenRelevanceThreshold is the §7.1.2 cutoff. A forbidden path
// appearing at this score or above (in either selections[] or reachable[])
// triggers a hard failure.
const forbiddenRelevanceThreshold = 0.30

// Score computes precision/recall/F1 against fx.Expected using the
// §11.1 manifest fields. `manifestSel` is the emitted selections slice.
// Hard-failure detection also reads the manifest's reachable and gaps
// fields so the forbidden-at-≥0.30 and missing-gap rules can fire.
func Score(fx Fixture, m *manifest.Manifest) Verdict {
	v := Verdict{}

	actualSet := make(map[string]manifest.LoadMode, len(m.Selections))
	for _, s := range m.Selections {
		actualSet[s.Path] = s.LoadMode
	}

	expectedSet := make(map[string]string, len(fx.Expected.Selections))
	for _, e := range fx.Expected.Selections {
		expectedSet[e.Path] = e.LoadMode
	}

	// Intersection weight per §7.1.2: path match = 1.0, path match but
	// load_mode disagreement = 0.5.
	intersection := 0.0
	for ep, eMode := range expectedSet {
		aMode, ok := actualSet[ep]
		if !ok {
			continue
		}
		switch {
		case eMode == "":
			intersection += 1.0
		case string(aMode) == eMode:
			intersection += 1.0
		default:
			intersection += 0.5
		}
	}

	switch {
	case len(actualSet) == 0:
		v.Metrics.Precision = 1.0
	default:
		v.Metrics.Precision = intersection / float64(len(actualSet))
	}
	switch {
	case len(expectedSet) == 0:
		v.Metrics.Recall = 1.0
	default:
		v.Metrics.Recall = intersection / float64(len(expectedSet))
	}
	if v.Metrics.Precision+v.Metrics.Recall > 0 {
		v.Metrics.F1 = 2 * v.Metrics.Precision * v.Metrics.Recall / (v.Metrics.Precision + v.Metrics.Recall)
	}

	// §7.1.2 hard failure: forbidden path appearing at relevance_score
	// ≥ 0.30 in either selections[] or reachable[]. Forbidden entries
	// support the doublestar-style glob patterns the walker accepts, but
	// for v1.1 Phase 1 we restrict matching to simple glob (path.Match)
	// plus a literal-prefix form for `x/**` patterns.
	for _, pat := range fx.Expected.Forbidden {
		for _, s := range m.Selections {
			if matchForbidden(pat, s.Path) && s.RelevanceScore >= forbiddenRelevanceThreshold {
				v.HardFail = true
				v.HardFailReason = append(v.HardFailReason,
					"forbidden path "+s.Path+" appears in selections at score >= 0.30")
			}
		}
		for _, r := range m.Reachable {
			if matchForbidden(pat, r.Path) && r.RelevanceScore >= forbiddenRelevanceThreshold {
				v.HardFail = true
				v.HardFailReason = append(v.HardFailReason,
					"forbidden path "+r.Path+" appears in reachable at score >= 0.30")
			}
		}
	}

	// §7.1.2 hard failure: expected.gaps entry not present in manifest.
	if len(fx.Expected.Gaps) > 0 {
		have := make(map[string]struct{}, len(m.Gaps))
		for _, g := range m.Gaps {
			have[string(g.Type)] = struct{}{}
		}
		for _, want := range fx.Expected.Gaps {
			if _, ok := have[want]; !ok {
				v.HardFail = true
				v.HardFailReason = append(v.HardFailReason, "expected gap "+want+" not present")
			}
		}
	}
	sort.Strings(v.HardFailReason)
	return v
}

// matchForbidden implements a minimal glob for forbidden entries.
// Supports:
//   - `a/b/c.go`        — literal path
//   - `dir/**`          — any file whose path begins with "dir/"
//   - `dir/**/x.go`     — any file under `dir/` whose basename is `x.go`
//
// Any other pattern falls back to path.Match (forward-slash globbing).
func matchForbidden(pattern, candidate string) bool {
	switch {
	case pattern == candidate:
		return true
	case hasSuffix(pattern, "/**"):
		prefix := pattern[:len(pattern)-len("/**")]
		return candidate == prefix || hasPrefix(candidate, prefix+"/")
	}
	if idx := indexOf(pattern, "/**/"); idx >= 0 {
		prefix := pattern[:idx]
		suffix := pattern[idx+len("/**/"):]
		if candidate != prefix && !hasPrefix(candidate, prefix+"/") {
			return false
		}
		base := path.Base(candidate)
		return base == suffix
	}
	ok, err := path.Match(pattern, candidate)
	return err == nil && ok
}

// small string helpers — kept inline to avoid an import-cycle with strings
// when this file is deep in test matchers. Compiler inlines these.
func hasPrefix(s, p string) bool { return len(s) >= len(p) && s[:len(p)] == p }
func hasSuffix(s, p string) bool { return len(s) >= len(p) && s[len(s)-len(p):] == p }
func indexOf(s, sub string) int {
	if len(sub) == 0 {
		return 0
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
