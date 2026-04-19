package cli

import (
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/dshills/aperture/internal/budget"
	"github.com/dshills/aperture/internal/feasibility"
	"github.com/dshills/aperture/internal/gaps"
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
		return nil, exitErr(exitCodeInternal, err)
	}

	// §7.4.1 / §7.4.2: when scope is set, project the index
	// (non-destructively) into (in-scope candidates, admissible
	// out-of-scope supplementals). The walker already walked the
	// whole repo so fingerprint and supplemental detection use the
	// full tree; scope is applied downstream, at the candidate pool,
	// not at the walker. The projection returns a NEW *index.Index;
	// the caller's in.Index is left untouched so it can be shared
	// across invocations without tripping a data race.
	preScopeCount := len(in.Index.Files)
	if in.Scope.IsSet() {
		in.Index = applyScopeToIndex(in.Index, in.Scope)
	}
	postScopeCount := len(in.Index.Files)
	if in.Verbose {
		slog.Info("scope resolved",
			"path", in.Scope.Path,
			"pre_filter_candidates", preScopeCount,
			"post_filter_candidates", postScopeCount,
		)
	}
	// §7.4.6: scope leaves zero in-scope candidates AND no supplemental
	// file survives as an admissible candidate → exit 9 (reuses the
	// v1 §7.6.5 underflow code per §7.7 clarification).
	if in.Scope.IsSet() && postScopeCount == 0 {
		return nil, exitErr(exitCodeBudgetUnderflow,
			fmt.Errorf("scope %q leaves zero planable candidates", in.Scope.Path))
	}

	// Phase-3 pipeline is I/O-aware: score using index metadata first,
	// then read file content ONLY for viable candidates (and doc Jaccard
	// tokens). This avoids reading every file in large repos when most
	// will be dropped as low-relevance.
	//
	// Step 1 — score with sDoc=0 (index-only). The §7.2.2 dampener
	// config flows into every scoring call so the manifest hash folds
	// the dampener state in via the emitted selections and their
	// breakdown fields.
	dampenerOpts := relevance.DampenerFromConfig(in.Config.Scoring.MentionDampener)
	scored := relevance.ScoreWithOptions(in.Index, in.Task, in.Config.Scoring.Weights, relevance.Options{Dampener: dampenerOpts})
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
		return nil, exitErr(exitCodeInternal, err)
	}

	// Step 4 — when any doc files carry task-relevant content, re-score
	// so sDoc contributes to the final score. Cheap: only the sDoc factor
	// changes, but ScoreWithOptions recomputes the full pipeline for
	// clarity. Either way, ALWAYS populate candidates' Score/Band from
	// the resolved scores so the selector has inputs to compare.
	if len(docTokens) > 0 {
		rescored := relevance.ScoreWithOptions(in.Index, in.Task, in.Config.Scoring.Weights, relevance.Options{
			DocTokens: docTokens,
			Dampener:  dampenerOpts,
		})
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

	// Phase-4 gaps + feasibility. Gaps runs over the assignment list so
	// it can introspect selected scores; underflow, if present, owns the
	// oversized_primary_context blocking gap and the engine defers.
	blockingCfg := blockingFromConfig(in.Config.Gaps.Blocking)
	demotions := collectDemotions(selResult.Assignments)
	// Flatten the scored map into path→score for the gaps engine so
	// ambiguousOwnership can inspect package peers that didn't win a
	// selection slot (per §7.7.3).
	scoreMap := make(map[string]float64, len(scoresByPath))
	for p, s := range scoresByPath {
		scoreMap[p] = s.Score
	}
	ruleGaps := gaps.Engine(gaps.Inputs{
		Task:           in.Task,
		Index:          in.Index,
		Assignments:    selResult.Assignments,
		Underflow:      underflow,
		Demotions:      demotions,
		BlockingConfig: blockingCfg,
		Scores:         scoreMap,
	})
	if underflow {
		// Prepend the §7.6.5 blocking gap so it keeps gap-1 and the rest
		// reindex behind it.
		allGaps := make([]manifest.Gap, 0, len(ruleGaps)+1)
		allGaps = append(allGaps, underflowGap(effective))
		allGaps = append(allGaps, ruleGaps...)
		ruleGaps = renumberGaps(allGaps)
	}

	feas := feasibility.Compute(feasibility.Inputs{
		Task:                    in.Task,
		Index:                   in.Index,
		Assignments:             selResult.Assignments,
		EffectiveContextBudget:  effective,
		EstimatedSelectedTokens: selResult.SpentTokens,
		Gaps:                    ruleGaps,
	})
	pos, neg, block, sub := feasibility.Rationale(feas, ruleGaps)

	m := assembleManifest(in, estimator, tokenCeiling, effective, selections, reachable, selResult.SpentTokens, underflow)
	m.Gaps = ruleGaps
	m.Feasibility = manifest.Feasibility{
		Score:              round4(feas.Score),
		Assessment:         feas.Assessment,
		Positives:          pos,
		Negatives:          neg,
		BlockingConditions: block,
		SubSignals:         sub,
	}
	if err := manifest.ApplyHash(m); err != nil {
		return nil, exitErr(exitCodeBadManifest, err)
	}
	if underflow {
		return m, exitErr(exitCodeBudgetUnderflow, fmt.Errorf("budget underflow: no viable selection fits within %d tokens", effective))
	}
	// Threshold gates per §16. Evaluated in CLI caller — we just attach
	// context here so runPlan can decide.
	return m, nil
}

// blockingFromConfig converts the config-resolved gaps.blocking slice into
// the set form the engine consumes.
func blockingFromConfig(list []string) map[string]struct{} {
	out := make(map[string]struct{}, len(list))
	for _, s := range list {
		out[s] = struct{}{}
	}
	return out
}

// collectDemotions extracts the demotion_reason map the gaps engine uses
// to detect the non-underflow `oversized_primary_context` warning path.
func collectDemotions(in []selection.Assignment) map[string]string {
	out := map[string]string{}
	for _, a := range in {
		if a.DemotedReason != "" {
			out[a.Path] = a.DemotedReason
		}
	}
	return out
}

// renumberGaps resets every gap's ID to gap-1, gap-2, … in the slice's
// current order. Used when the engine's output is combined with the
// underflow gap so IDs remain stable across runs.
//
// Note: this function MODIFIES the input slice in place (changing each
// entry's ID field) and returns the same slice. Callers should pass in
// a slice they intend to mutate — typically one freshly assembled from
// engine output.
func renumberGaps(in []manifest.Gap) []manifest.Gap {
	for i := range in {
		in[i].ID = fmt.Sprintf("gap-%d", i+1)
	}
	return in
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

	dampCfg := relevance.DampenerFromConfig(in.Config.Scoring.MentionDampener)
	for _, a := range assignments {
		s := scores[a.Path]
		f := in.Index.File(a.Path)
		breakdown := relevance.BreakdownWithDampener(s.Signals, in.Config.Scoring.Weights, dampCfg, s.Dampener)
		// §8.4 verbose logging: surface the per-candidate dampener when
		// it's below 1.0 and the candidate made the selection / reachable
		// set. "dampener < 1.0" is the threshold for interesting events —
		// a value of 1.0 means the dampener was inactive or the candidate
		// had a peer signal fully agreeing.
		if in.Verbose && dampCfg.Enabled && s.Dampener < 1.0 {
			slog.Info("mention dampener applied",
				"path", a.Path,
				"dampener", s.Dampener,
				"other_max", relevance.OtherMaxForDampener(s.Signals),
				"mention_signal", s.Signals["mention"],
				"load_mode", string(a.LoadMode),
			)
		}
		if a.LoadMode == manifest.LoadModeReachable {
			reachables = append(reachables, manifest.Reachable{
				Path:           a.Path,
				RelevanceScore: round4(s.Score),
				Reason:         reachableReason(a),
				ScoreBreakdown: breakdown,
			})
			continue
		}
		rationale := rationaleFor(s, f)
		if f != nil && f.OutOfScope {
			// §7.4.2 mandates this exact rationale token on
			// out-of-scope supplementals so the manifest stays
			// auditable about why a non-scoped file was admitted.
			rationale = append(rationale, "outside_scope_supplemental")
		}
		sel := manifest.Selection{
			Path:            a.Path,
			Kind:            "file",
			LoadMode:        a.LoadMode,
			RelevanceScore:  round4(s.Score),
			ScoreBreakdown:  breakdown,
			EstimatedTokens: a.Cost,
			Rationale:       rationale,
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
			LanguageTiers:           languageTiersFromIndex(in),
		},
	}
	// v1.1 §7.4.4: emit the scope projection here (not after) so every
	// downstream step — including manifest.ApplyHash — sees the
	// scoped and unscoped manifests as structurally distinct.
	if in.Scope.IsSet() {
		m.Scope = &manifest.Scope{Path: in.Scope.Path}
	}
	return m
}

// languageTiersFromIndex builds the §10.1 language_tiers map by
// reducing FileEntry.LanguageTier values to a language → tier
// summary. The result is a stable, sorted-key map (encoding/json
// sorts map keys lexicographically on emit, so deterministic input
// is all we need). Empty when the index has no files.
func languageTiersFromIndex(in buildInputs) map[string]string {
	if in.Index == nil || len(in.Index.Files) == 0 {
		return nil
	}
	out := make(map[string]string)
	for _, f := range in.Index.Files {
		if f.Language == "" || f.LanguageTier == "" {
			continue
		}
		out[f.Language] = f.LanguageTier
	}
	if len(out) == 0 {
		return nil
	}
	return out
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

// round4 trims a float64 to 4 decimal places. Used by BuildManifest for
// the feasibility.score field and relevance_score entries so the JSON
// emission stays compact while remaining unambiguous.
//
// Kept package-local; referenced by BuildManifest above (see "round4(feas.Score)")
// and by translateAssignments for relevance_score + reachable-entry scores.
// The canonical manifest hash emits floats via strconv.FormatFloat('f', -1, …),
// so this rounding is the only place precision is intentionally dropped.
func round4(f float64) float64 {
	const scale = 10000.0
	return math.Round(f*scale) / scale
}
