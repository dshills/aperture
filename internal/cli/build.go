package cli

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/dshills/aperture/internal/budget"
	"github.com/dshills/aperture/internal/index"
	"github.com/dshills/aperture/internal/loadmode"
	"github.com/dshills/aperture/internal/manifest"
	"github.com/dshills/aperture/internal/relevance"
	"github.com/dshills/aperture/internal/selection"
	"github.com/dshills/aperture/internal/summary"
	"github.com/dshills/aperture/internal/version"
)

// BuildManifest runs the full Phase-3 pipeline: tokenizer resolution,
// scoring, load-mode eligibility, greedy selection, underflow detection,
// and manifest assembly. The returned manifest already carries its hash.
// On §7.6.5 underflow, the function sets Incomplete=true and returns an
// ExitCodeError(9) alongside the manifest so callers can still emit it.
func BuildManifest(in buildInputs) (*manifest.Manifest, error) {
	estimator, err := budget.Resolve(budget.ResolveOptions{Model: resolvedModel(in)})
	if err != nil {
		var rerr *budget.ResolveError
		if errors.As(err, &rerr) {
			return nil, exitErr(rerr.Code, err)
		}
		return nil, exitErr(1, err)
	}

	// Phase-3 pipeline is I/O-aware: score using index metadata first,
	// then read file content ONLY for viable candidates (and doc Jaccard
	// tokens). This avoids reading every file in large repos when most
	// will be dropped as low-relevance.
	//
	// Step 1 — score with sDoc=0 (index-only).
	scored := relevance.Score(in.Index, in.Task, in.Config.Scoring.Weights)
	scoresByPath := map[string]relevance.Scored{}
	for _, s := range scored {
		scoresByPath[s.Path] = s
	}

	// Step 2 — drop low-relevance files before touching the filesystem.
	viablePaths := map[string]struct{}{}
	for _, s := range scored {
		if loadmode.ClassifyScore(s.Score) != loadmode.LowRelevance {
			viablePaths[s.Path] = struct{}{}
		}
	}

	// Step 3 — read content for viable files only; compute per-mode token
	// costs AND, for doc files, the §7.4.2.1 first-2-KiB token bag from
	// the same read (no double I/O).
	candidates, docTokens, err := buildViableCandidates(in, estimator, viablePaths)
	if err != nil {
		return nil, exitErr(1, err)
	}

	// Step 4 — when any doc files carry task-relevant content, re-score
	// so sDoc contributes to the final score. Cheap: only the sDoc factor
	// changes, but ScoreWithOptions recomputes the full pipeline for
	// clarity. Either way, ALWAYS populate candidates' Score/Band from
	// the resolved scores so the selector has inputs to compare.
	if len(docTokens) > 0 {
		rescored := relevance.ScoreWithOptions(in.Index, in.Task, in.Config.Scoring.Weights, relevance.Options{DocTokens: docTokens})
		for _, s := range rescored {
			scoresByPath[s.Path] = s
		}
	}
	for i := range candidates {
		s := scoresByPath[candidates[i].File.Path]
		candidates[i].Score = s.Score
		candidates[i].Band = loadmode.ClassifyScore(s.Score)
	}

	// Effective context budget = token_ceiling − reservations.
	tokenCeiling := in.BudgetFlag
	if tokenCeiling == 0 {
		tokenCeiling = in.Config.Defaults.Budget
	}
	r := in.Config.Defaults.Reserve
	effective := tokenCeiling - (r.Instructions + r.Reasoning + r.ToolOutput + r.Expansion)
	if effective < 0 {
		effective = 0
	}

	// §7.6.5 underflow check. Must fire before the selector runs.
	underflow := selection.Underflow(candidates, effective)

	selResult := selection.Select(candidates, effective)

	selections, reachable := translateAssignments(selResult.Assignments, scoresByPath, in)

	m := assembleManifest(in, estimator, tokenCeiling, effective, selections, reachable, selResult.SpentTokens, underflow)
	if underflow {
		m.Gaps = []manifest.Gap{underflowGap(effective)}
	}
	if err := manifest.ApplyHash(m); err != nil {
		return nil, exitErr(6, err)
	}
	if underflow {
		return m, exitErr(9, fmt.Errorf("budget underflow: no viable selection fits within %d tokens", effective))
	}
	return m, nil
}

// underflowGap produces the §7.6.5 mandatory blocking gap that accompanies
// an `incomplete: true` manifest. The same record is also surfaced to the
// user via the exit-9 error message.
func underflowGap(effective int) manifest.Gap {
	return manifest.Gap{
		ID:          "gap-1",
		Type:        manifest.GapOversizedPrimaryContext,
		Severity:    manifest.GapSeverityBlocking,
		Description: fmt.Sprintf("effective budget %d is smaller than the smallest viable cost of the highest-scoring candidate", effective),
		Evidence: []string{
			"§7.6.5 budget underflow",
			fmt.Sprintf("effective_context_budget=%d", effective),
		},
		SuggestedRemediation: []string{
			"increase --budget",
			"reduce reserved token headroom in .aperture.yaml",
			"narrow the task so fewer high-score candidates are generated",
		},
	}
}

// buildViableCandidates reads content for the viable-path set only, computes
// per-mode token costs, and — for doc files — also extracts the §7.4.2.1
// first-2-KiB token bag from the same read. Returns the candidate slice
// and the doc-token map so callers can re-score sDoc without double I/O.
//
// Memory footprint: `content` is local to the loop iteration and becomes
// GC-eligible at iteration end. No file's bytes survive past its own
// iteration; the returned candidates carry only integer token counts.
// Peak heap is bounded by the largest admitted file (walker caps at
// 10 MiB per §7.4.3).
func buildViableCandidates(in buildInputs, est budget.Estimator, viablePaths map[string]struct{}) ([]loadmode.Candidate, map[string]map[string]struct{}, error) {
	taskLower := strings.ToLower(in.Task.RawText)
	out := make([]loadmode.Candidate, 0, len(viablePaths))
	docTokens := make(map[string]map[string]struct{})
	for i := range in.Index.Files {
		f := &in.Index.Files[i]
		if _, ok := viablePaths[f.Path]; !ok {
			continue
		}
		contentPath := filepath.Join(in.RepoRoot, filepath.FromSlash(f.Path))
		content, err := os.ReadFile(contentPath) //nolint:gosec // walker-verified path
		if err != nil {
			slog.Warn("skipping unreadable file during candidate build",
				"path", f.Path, "error", err.Error())
			continue
		}
		costFull := budget.EstimateFullBytes(est, content)
		structural := summary.Structural(f)
		behavioral := summary.Behavioral(f, costFull)
		costStruct := budget.EstimateSummary(est, structural)
		costBehav := budget.EstimateSummary(est, behavioral)

		// Reuse the same read for doc-token extraction when applicable.
		switch f.Extension {
		case ".md", ".markdown", ".rst", ".adoc":
			probe := content
			if len(probe) > 2048 {
				probe = probe[:2048]
			}
			docTokens[f.Path] = alnumTokenSet(strings.ToLower(string(probe)))
		}

		c := loadmode.Candidate{
			File:           f,
			CostFull:       costFull,
			CostStructural: costStruct,
			CostBehavioral: costBehav,
			Mentioned:      mentioned(taskLower, f.Path),
		}
		c.Size = loadmode.ClassifySize(f.Size, costFull)
		out = append(out, c)
	}
	return out, docTokens, nil
}

// alnumTokenSet splits s on any non-[a-z0-9] boundary and returns the
// resulting token set. Duplicates are collapsed. Used for the §7.4.2.1
// doc token bag.
func alnumTokenSet(s string) map[string]struct{} {
	tokens := map[string]struct{}{}
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			tokens[cur.String()] = struct{}{}
			cur.Reset()
		}
	}
	for _, r := range s {
		if ('a' <= r && r <= 'z') || ('0' <= r && r <= '9') {
			cur.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	return tokens
}

func translateAssignments(
	assignments []selection.Assignment,
	scores map[string]relevance.Scored,
	in buildInputs,
) ([]manifest.Selection, []manifest.Reachable) {
	selections := make([]manifest.Selection, 0, len(assignments))
	reachables := make([]manifest.Reachable, 0)

	for _, a := range assignments {
		s := scores[a.Path]
		f := in.Index.File(a.Path)
		breakdown := relevance.Breakdown(s.Signals, in.Config.Scoring.Weights)
		if a.LoadMode == manifest.LoadModeReachable {
			reachables = append(reachables, manifest.Reachable{
				Path:           a.Path,
				RelevanceScore: round4(s.Score),
				Reason:         reachableReason(a),
				ScoreBreakdown: breakdown,
			})
			continue
		}
		sel := manifest.Selection{
			Path:            a.Path,
			Kind:            "file",
			LoadMode:        a.LoadMode,
			RelevanceScore:  round4(s.Score),
			ScoreBreakdown:  breakdown,
			EstimatedTokens: a.Cost,
			Rationale:       rationaleFor(s, f),
			SideEffects:     sideEffectsOrNil(f),
		}
		if a.DemotedReason != "" {
			reason := a.DemotedReason
			sel.DemotionReason = &reason
		}
		selections = append(selections, sel)
	}
	sort.Slice(selections, func(i, j int) bool { return selections[i].Path < selections[j].Path })
	sort.Slice(reachables, func(i, j int) bool { return reachables[i].Path < reachables[j].Path })
	return selections, reachables
}

func reachableReason(a selection.Assignment) string {
	if a.Demotion != "" {
		return "budget_exhausted"
	}
	return "plausibly_relevant"
}

// rationaleFor produces short, deterministic human-readable rationale
// strings for each selection, ordered by descending signal contribution.
func rationaleFor(s relevance.Scored, f *index.FileEntry) []string {
	if len(s.Signals) == 0 {
		return []string{}
	}
	type kv struct {
		name  string
		value float64
	}
	entries := make([]kv, 0, len(s.Signals))
	for name, val := range s.Signals {
		if val > 0 {
			entries = append(entries, kv{name, val})
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].value != entries[j].value {
			return entries[i].value > entries[j].value
		}
		return entries[i].name < entries[j].name
	})
	out := make([]string, 0, len(entries))
	phrases := map[string]string{
		"mention":  "direct task mention",
		"filename": "filename token match",
		"symbol":   "exported symbol match",
		"import":   "import adjacency",
		"package":  "package match",
		"test":     "associated tests",
		"doc":      "documentation overlap",
		"config":   "configuration file",
	}
	for _, e := range entries {
		phrase, ok := phrases[e.name]
		if !ok {
			phrase = e.name
		}
		out = append(out, phrase)
	}
	if f != nil && f.ParseError {
		out = append(out, "parse_error fallback")
	}
	return out
}

func sideEffectsOrNil(f *index.FileEntry) []string {
	if f == nil || f.Language != "go" {
		return nil
	}
	if len(f.SideEffects) == 0 {
		return []string{}
	}
	out := append([]string{}, f.SideEffects...)
	sort.Strings(out)
	return out
}

func assembleManifest(
	in buildInputs,
	est budget.Estimator,
	tokenCeiling, effective int,
	selections []manifest.Selection,
	reachables []manifest.Reachable,
	spent int,
	underflow bool,
) *manifest.Manifest {
	now := time.Now().UTC().Format(time.RFC3339)
	digest, _ := in.Config.Digest()
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "unknown"
	}

	anchors := append([]string{}, in.Task.Anchors...)
	sort.Strings(anchors)

	r := in.Config.Defaults.Reserve

	m := &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		GeneratedAt:   now,
		Incomplete:    underflow,
		Task: manifest.Task{
			TaskID:             in.Task.TaskID,
			Source:             in.Task.Source,
			RawText:            in.Task.RawText,
			Type:               in.Task.Type,
			Objective:          in.Task.Objective,
			Anchors:            anchors,
			ExpectsTests:       in.Task.ExpectsTests,
			ExpectsConfig:      in.Task.ExpectsConfig,
			ExpectsDocs:        in.Task.ExpectsDocs,
			ExpectsMigration:   in.Task.ExpectsMigration,
			ExpectsAPIContract: in.Task.ExpectsAPIContract,
		},
		Repo: manifest.Repo{
			Root:          in.RepoRoot,
			Fingerprint:   in.Fingerprint,
			LanguageHints: langHintsOrEmpty(in.Languages),
		},
		Budget: manifest.Budget{
			Model:        resolvedModel(in),
			TokenCeiling: tokenCeiling,
			Reserved: manifest.Reserved{
				Instructions: r.Instructions,
				Reasoning:    r.Reasoning,
				ToolOutput:   r.ToolOutput,
				Expansion:    r.Expansion,
			},
			EffectiveContextBudget:  effective,
			EstimatedSelectedTokens: spent,
			Estimator:               est.Identity(),
			EstimatorVersion:        est.Version(),
		},
		Selections: selections,
		Reachable:  reachables,
		Exclusions: manifestExclusions(in.Exclusions),
		Gaps:       []manifest.Gap{},
		Feasibility: manifest.Feasibility{
			Score:              0.0,
			Assessment:         "pending: gap/feasibility lands in Phase 4",
			Positives:          []string{},
			Negatives:          []string{},
			BlockingConditions: []string{},
			SubSignals:         map[string]float64{},
		},
		GenerationMetadata: manifest.GenerationMetadata{
			ApertureVersion:         version.Version,
			SelectionLogicVersion:   manifest.SelectionLogicVersion,
			ConfigDigest:            digest,
			SideEffectTablesVersion: manifest.SideEffectTablesVer,
			Host:                    host,
			PID:                     os.Getpid(),
			WallClockStartedAt:      now,
		},
	}
	return m
}

func resolvedModel(in buildInputs) string {
	if in.ModelFlag != "" {
		return in.ModelFlag
	}
	return in.Config.Defaults.Model
}

// mentioned reports whether the task text explicitly names a file. BOTH
// the full relative-path form and the basename form are checked with a
// word-boundary guard so that — for example — a task saying "please run
// tests" does NOT match a file called `run.go`, and a task mentioning
// "data.go" does NOT match `a.go`. A mention must be bordered on both
// sides by non-[A-Za-z0-9_] characters (or string boundaries).
func mentioned(taskLower, p string) bool {
	lp := strings.ToLower(p)
	if lp == "" {
		return false
	}
	if hasBoundaryMention(taskLower, lp) {
		return true
	}
	return hasBoundaryMention(taskLower, strings.ToLower(filepath.Base(p)))
}

// hasBoundaryMention returns true iff needle appears in hay flanked by
// non-word characters (or string ends).
func hasBoundaryMention(hay, needle string) bool {
	if needle == "" {
		return false
	}
	idx := 0
	for {
		hit := strings.Index(hay[idx:], needle)
		if hit < 0 {
			return false
		}
		start := idx + hit
		end := start + len(needle)
		if isBoundary(hay, start-1) && isBoundary(hay, end) {
			return true
		}
		idx = end
	}
}

// isBoundary reports whether taskLower[i] is outside the string or is a
// non-word character (anything outside [A-Za-z0-9_]).
func isBoundary(s string, i int) bool {
	if i < 0 || i >= len(s) {
		return true
	}
	c := s[i]
	switch {
	case 'a' <= c && c <= 'z':
		return false
	case '0' <= c && c <= '9':
		return false
	case c == '_':
		return false
	}
	return true
}

// round4 trims to 4 decimal places so relevance_score JSON stays compact
// while remaining unambiguous. The value is still a float64, and the
// canonical hash emits floats via strconv.FormatFloat('f', -1, …), so
// this rounding is the only place precision is intentionally dropped.
func round4(f float64) float64 {
	scale := 10000.0
	return float64(int64(f*scale+0.5)) / scale
}
