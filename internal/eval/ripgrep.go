package eval

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// RipgrepBaseline is the ground-truth-equivalent set a naive ripgrep
// query would pick for a fixture's task anchors. `Invoked` is false when
// the anchors array was empty after dedup (§4.4 step 5) and `rg` was
// therefore NOT executed.
type RipgrepBaseline struct {
	Pattern  string
	Invoked  bool
	Files    []string
	Metrics  Metrics
	TopN     int
	ExitCode int // 0 on success; non-zero mirrors rg failure
}

// RipgrepOptions controls RipgrepFixture.
type RipgrepOptions struct {
	RepoRoot string
	TopN     int      // default 20 when ≤ 0
	Excludes []string // walker-exclusion globs to pass as --glob !<pat>
}

// RipgrepFixture runs the §4.4 normative ripgrep baseline for a single
// fixture. It renders the fixture's task anchors into a pattern,
// invokes `rg --count-matches`, ranks by count then by path, truncates
// to top-N, and then — note — scoring against fx.Expected is left to
// the caller so they can invoke the same Aperture budget fitter
// (§4.4 "budget fitting" rule). The returned Files is the ranked,
// truncated candidate set ready for budget fitting.
func RipgrepFixture(ctx context.Context, fx Fixture, anchors []string, opts RipgrepOptions) (*RipgrepBaseline, error) {
	topN := opts.TopN
	if topN <= 0 {
		topN = 20
	}

	pat, dedupedAnchors := renderAnchorPattern(anchors)
	base := &RipgrepBaseline{Pattern: pat, TopN: topN}
	if len(dedupedAnchors) == 0 {
		// §4.4 step 5: empty anchors → empty candidate set, no rg call.
		base.Invoked = false
		return base, nil
	}
	base.Invoked = true

	raw, err := runRipgrep(ctx, opts.RepoRoot, pat, opts.Excludes)
	if err != nil {
		base.ExitCode = 1
		return base, err
	}
	ranked, err := parseCountMatches(raw, opts.RepoRoot)
	if err != nil {
		return base, err
	}
	if len(ranked) > topN {
		ranked = ranked[:topN]
	}
	base.Files = ranked
	return base, nil
}

// renderAnchorPattern implements the §4.4 steps 1-5 rendering rule.
// Returns (pattern, dedupedAnchorsInFirstSeenOrder). When the deduped
// anchor list is empty, the returned pattern is also empty.
func renderAnchorPattern(anchors []string) (string, []string) {
	seen := make(map[string]struct{}, len(anchors))
	dedup := make([]string, 0, len(anchors))
	for _, a := range anchors {
		key := strings.ToLower(a)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		dedup = append(dedup, a)
	}
	if len(dedup) == 0 {
		return "", dedup
	}
	escaped := make([]string, 0, len(dedup))
	for _, a := range dedup {
		escaped = append(escaped, regexp.QuoteMeta(a))
	}
	return strings.Join(escaped, "|"), dedup
}

// parseCountMatches parses the `path:count` lines emitted by
// `rg --count-matches`. Lines with count == 0 are filtered. Output is
// ranked by count desc, then by normalized repo-relative path asc.
// Paths returned are repo-relative, forward-slash.
func parseCountMatches(raw []byte, repoRoot string) ([]string, error) {
	type row struct {
		path  string
		count int
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	rows := make([]row, 0, len(lines))
	for _, l := range lines {
		if l == "" {
			continue
		}
		idx := strings.LastIndex(l, ":")
		if idx < 0 {
			continue
		}
		p := l[:idx]
		cs := l[idx+1:]
		c, err := strconv.Atoi(strings.TrimSpace(cs))
		if err != nil {
			return nil, fmt.Errorf("parse rg count-matches line %q: %w", l, err)
		}
		if c <= 0 {
			continue
		}
		rel, rerr := filepath.Rel(repoRoot, p)
		if rerr != nil {
			rel = p
		}
		rel = filepath.ToSlash(rel)
		rows = append(rows, row{path: rel, count: c})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].count != rows[j].count {
			return rows[i].count > rows[j].count
		}
		return rows[i].path < rows[j].path
	})
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.path)
	}
	return out, nil
}

// ScoreRipgrepBaseline computes precision/recall/F1 of actualPaths vs.
// fx.Expected.Selections using the same §7.1.2 rules as the Aperture
// scoring path — except that the ripgrep baseline carries no load-mode
// information, so every actual match is treated as load-mode-unspecified.
// Forbidden and gaps hard-failure rules do NOT apply to the ripgrep
// comparator; its purpose is measurement only.
func ScoreRipgrepBaseline(fx Fixture, actualPaths []string) Metrics {
	var m Metrics
	actual := make(map[string]struct{}, len(actualPaths))
	for _, p := range actualPaths {
		actual[p] = struct{}{}
	}
	expected := make(map[string]struct{}, len(fx.Expected.Selections))
	for _, e := range fx.Expected.Selections {
		expected[e.Path] = struct{}{}
	}
	intersection := 0
	for ep := range expected {
		if _, ok := actual[ep]; ok {
			intersection++
		}
	}
	switch {
	case len(actual) == 0:
		m.Precision = 1.0
	default:
		m.Precision = float64(intersection) / float64(len(actual))
	}
	switch {
	case len(expected) == 0:
		m.Recall = 1.0
	default:
		m.Recall = float64(intersection) / float64(len(expected))
	}
	if m.Precision+m.Recall > 0 {
		m.F1 = 2 * m.Precision * m.Recall / (m.Precision + m.Recall)
	}
	return m
}
