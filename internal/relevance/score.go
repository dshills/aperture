// Package relevance computes per-file relevance scores per SPEC §7.4.2.1.
// Scoring is deterministic: the same (repo snapshot, task, resolved config)
// produces the same score on every host.
package relevance

import (
	"path"
	"sort"
	"strings"

	"github.com/dshills/aperture/internal/config"
	"github.com/dshills/aperture/internal/index"
	"github.com/dshills/aperture/internal/manifest"
	"github.com/dshills/aperture/internal/task"
)

// Factor names as emitted in score_breakdown entries. The ORDER of this
// slice is the manifest emission order per §11.1 catalogue (§7.4.2.2
// declaration order).
var factorOrder = []string{
	"mention",
	"filename",
	"symbol",
	"import",
	"package",
	"test",
	"doc",
	"config",
}

// Scored represents one file's scoring output — the final score plus the
// per-factor signal values. The manifest's score_breakdown is derived from
// Signals (non-zero factors only, in factorOrder).
type Scored struct {
	Path    string
	Score   float64
	Signals map[string]float64
	// Dampener is the resolved §7.2.2 factor applied to s_mention for
	// this file. 1.0 when the dampener is disabled; in [floor, 1.0]
	// when enabled. Breakdown() plumbs this into the per-factor
	// manifest entry for the "mention" factor so downstream consumers
	// can reproduce the contribution.
	Dampener float64
}

// Options configures optional inputs to Score.
type Options struct {
	// DocTokens, when non-nil, maps normalized repo-relative paths to
	// their lowercased alphanumeric token sets (first 2 KiB of the file
	// per §7.4.2.1). Callers that have already read doc content supply
	// this so sDoc can compute Jaccard similarity without re-reading the
	// filesystem from inside the scoring layer. Missing paths yield 0.
	DocTokens map[string]map[string]struct{}

	// Dampener controls the v1.1 §7.2.2 mention dampener. The zero
	// value (Enabled=false) preserves v1.0 scoring byte-identically.
	// Callers resolved from .aperture.yaml supply the §7.2.3 defaults.
	Dampener DampenerConfig
}

// Score runs the full two-pass pipeline from §7.4.2.1 against an index
// and a parsed task and returns deterministic per-file results. The pass-1
// cache is internal to this call so Score itself is stateless.
func Score(idx *index.Index, t task.Task, w config.Weights) []Scored {
	return ScoreWithOptions(idx, t, w, Options{})
}

// ScoreWithOptions is the Score variant that accepts pre-computed caches
// (currently: doc-token bags for §7.4.2.1 sDoc). Use this when you have
// file content in hand (e.g., the CLI's candidate-building pass).
func ScoreWithOptions(idx *index.Index, t task.Task, w config.Weights, opts Options) []Scored {
	ctx := buildContext(idx, t)
	ctx.docTokens = opts.DocTokens
	ctx.dampener = opts.Dampener
	// Precompute per-repo lookup indexes once so per-file signal functions
	// are O(1) instead of O(packages) or O(packages·log packages). These
	// structures are read-only for the remainder of Score.
	sortedPkgKeys := sortedPackageKeys(idx)
	siblingIdx := buildSiblingIndex(idx)

	// Pass 1: every factor except s_import.
	pass1 := make([]Scored, 0, len(idx.Files))
	pass1ByPath := make(map[string]float64, len(idx.Files))
	for _, f := range idx.Files {
		signals := map[string]float64{
			"mention":  sMention(f, ctx),
			"filename": sFilename(f, ctx),
			"symbol":   sSymbol(f, ctx),
			"package":  sPackage(f, siblingIdx, ctx),
			"test":     sTestPass1(f, idx, ctx, pass1ByPath),
			"doc":      sDoc(f, ctx),
			"config":   sConfig(f, t, ctx),
		}
		// §7.4.2: supplemental files admitted under `--scope` but
		// outside the scope subtree contribute only direction-
		// independent signals. The scoring math is otherwise
		// unchanged — the four zeroed factors drop out of combine()
		// and other_max naturally.
		if f.OutOfScope {
			signals["symbol"] = 0
			signals["package"] = 0
			signals["test"] = 0
			// s_import is computed in pass 2; zeroed there too.
		}
		damp := Dampen(OtherMaxForDampener(signals), ctx.dampener)
		score := clamp01(combine(signals, w, damp))
		pass1 = append(pass1, Scored{Path: f.Path, Score: score, Signals: signals, Dampener: damp})
		pass1ByPath[f.Path] = score
	}

	// Pass 1.5: reconcile s_test because §7.4.2.1 defines "test file for
	// a candidate scoring ≥ 0.5 under the other factors" — that's a
	// pass-1 score. We already have pass1ByPath; recompute s_test once.
	for i := range pass1 {
		pass1[i].Signals["test"] = sTestFinal(idx.File(pass1[i].Path), idx, ctx, pass1ByPath)
	}

	// Package-level relevance derived from pass 1 (§7.4.2.1 s_import).
	pkgScores := packageMaxScores(idx, pass1ByPath)
	// Cache each package's union-of-imports once so the pass-2 transitive
	// walk is O(packages) total instead of O(files × avg-files-per-package).
	pkgImports := buildPackageImportsCache(idx)
	// Cache import-path → package-dir resolution so each unique import is
	// resolved at most once, eliminating the O(N·P) scan that would
	// otherwise happen as every file hits resolveImportDir per import.
	importCache := buildImportResolutionCache(idx, sortedPkgKeys, pkgImports)

	// Pass 2: compute s_import and roll into final score. The dampener
	// is recomputed because s_import is part of other_max (§7.2.2).
	// §7.4.2: out-of-scope supplementals skip s_import (forced to 0).
	out := make([]Scored, 0, len(pass1))
	for _, entry := range pass1 {
		f := idx.File(entry.Path)
		if f == nil {
			continue
		}
		if f.OutOfScope {
			entry.Signals["import"] = 0
		} else {
			entry.Signals["import"] = sImport(f, importCache, pkgScores, pkgImports)
		}
		entry.Dampener = Dampen(OtherMaxForDampener(entry.Signals), ctx.dampener)
		entry.Score = clamp01(combine(entry.Signals, w, entry.Dampener))
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// buildImportResolutionCache precomputes the import-path → package-dir
// mapping for every unique import path referenced in the repo (both direct
// from file imports and transitive from the pre-built pkgImports cache).
// This reduces pass-2's per-file work from O(imports × packages) to
// O(imports) with O(unique-imports × packages) one-time precompute.
func buildImportResolutionCache(idx *index.Index, sortedPkgKeys []string, pkgImports map[string][]string) map[string]string {
	unique := make(map[string]struct{})
	for _, f := range idx.Files {
		for _, imp := range f.Imports {
			unique[imp] = struct{}{}
		}
	}
	for _, imps := range pkgImports {
		for _, imp := range imps {
			unique[imp] = struct{}{}
		}
	}
	cache := make(map[string]string, len(unique))
	for imp := range unique {
		cache[imp] = resolveImportDir(imp, sortedPkgKeys)
	}
	return cache
}

// buildPackageImportsCache precomputes every package's sorted, deduped
// import list. Callers that walk the transitive import graph then look up
// a package's fan-out in O(1) instead of re-walking the package's files.
func buildPackageImportsCache(idx *index.Index) map[string][]string {
	out := make(map[string][]string, len(idx.Packages))
	for dir, pkg := range idx.Packages {
		set := map[string]struct{}{}
		for _, p := range pkg.Files {
			f := idx.File(p)
			if f == nil {
				continue
			}
			for _, imp := range f.Imports {
				set[imp] = struct{}{}
			}
		}
		list := make([]string, 0, len(set))
		for k := range set {
			list = append(list, k)
		}
		sort.Strings(list)
		out[dir] = list
	}
	return out
}

// combine multiplies each signal by its weight and sums them. Factors
// absent from the signals map contribute zero. The per-file dampener
// (§7.2.2) is applied to s_mention before its weight is multiplied in;
// callers pass 1.0 when the dampener is disabled.
//
// This function is invoked twice per file (pass 1 + pass 2), so every
// per-file allocation counts on large repos. The weights lookup is
// inlined against config.Weights' struct fields rather than a map to
// avoid a fresh map allocation each call.
func combine(signals map[string]float64, w config.Weights, mentionDampener float64) float64 {
	return signals["mention"]*mentionDampener*w.Mention +
		signals["filename"]*w.Filename +
		signals["symbol"]*w.Symbol +
		signals["import"]*w.Import +
		signals["package"]*w.Package +
		signals["test"]*w.Test +
		signals["doc"]*w.Doc +
		signals["config"]*w.Config
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

// scoringContext caches the few task-derived values every signal function
// needs: lowercased task text, anchor set, lowercased anchor set, anchor
// token set for Jaccard, etc. Keeps the signal functions short and
// allocation-free in their hot paths.
type scoringContext struct {
	taskLower      string
	anchors        []string
	anchorsLower   []string
	anchorLowerSet map[string]struct{}
	actionType     manifest.ActionType
	task           task.Task
	docTokens      map[string]map[string]struct{}
	dampener       DampenerConfig
}

func buildContext(_ *index.Index, t task.Task) *scoringContext {
	anchorsLower := make([]string, len(t.Anchors))
	lowerSet := make(map[string]struct{}, len(t.Anchors))
	for i, a := range t.Anchors {
		al := strings.ToLower(a)
		anchorsLower[i] = al
		lowerSet[al] = struct{}{}
	}
	return &scoringContext{
		taskLower:      strings.ToLower(t.RawText),
		anchors:        t.Anchors,
		anchorsLower:   anchorsLower,
		anchorLowerSet: lowerSet,
		actionType:     t.Type,
		task:           t,
	}
}

// sMention — SPEC §7.4.2.1. 1.0 if the lowercased task text contains the
// lowercased normalized repo-relative path OR the lowercased basename.
//
// Reviewers: BOTH sides of every comparison below are explicitly
// lowercased. f.Path is passed through strings.ToLower on the line that
// defines pLower, and ctx.taskLower was lowercased once in buildContext.
// The basename `base` variable is similarly lowercased.
//
// The basename branch uses a word-boundary check (mentionHasBoundary)
// so a file like `run.go` does not spuriously match "please run tests"
// — only phrases like "in run.go" or "edit run.go" count. Full
// repo-relative paths (e.g. "internal/oauth/provider.go") are accepted
// without the boundary check because they cannot occur accidentally.
func sMention(f index.FileEntry, ctx *scoringContext) float64 {
	pLower := strings.ToLower(f.Path)
	if mentionHasBoundary(ctx.taskLower, pLower) {
		return 1.0
	}
	base := strings.ToLower(path.Base(f.Path))
	if mentionHasBoundary(ctx.taskLower, base) {
		return 1.0
	}
	return 0.0
}

// mentionHasBoundary reports whether `needle` appears in `hay` flanked
// on both sides by non-[A-Za-z0-9_] characters or string boundaries.
// Matches §7.3.1.1's word-boundary semantics for anchor extraction.
func mentionHasBoundary(hay, needle string) bool {
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
		if mentionBoundaryChar(hay, start-1) && mentionBoundaryChar(hay, end) {
			return true
		}
		idx = end
	}
}

func mentionBoundaryChar(s string, i int) bool {
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

// sFilename — Jaccard similarity between the lowercase alphanumeric tokens
// in the file basename and the task anchor set.
func sFilename(f index.FileEntry, ctx *scoringContext) float64 {
	tokens := alnumTokens(strings.ToLower(path.Base(f.Path)))
	if len(tokens) == 0 || len(ctx.anchorsLower) == 0 {
		return 0.0
	}
	return jaccard(tokens, ctx.anchorLowerSet)
}

// sSymbol — for Go files, the fraction of task anchors that case-
// insensitively match any exported identifier in the file's symbol
// table, capped at 1.0. "Match" here is substring containment: an anchor
// like "refresh" matches a symbol like "RefreshToken". Strict equality
// would give near-zero recall against real codebases where task text
// talks in verbs ("refresh") and code uses nouns ("RefreshToken").
//
// Performance: concatenate all lowercased symbol names into a single
// separator-joined blob and scan each anchor against the blob once.
// That keeps us at O(anchors · total-symbol-bytes) per file, rather than
// O(anchors · symbols · avg-name-length) with loop overhead for each
// symbol. Anchors are already ≥3 chars by construction in §7.3.2, so no
// additional length guard is needed.
func sSymbol(f index.FileEntry, ctx *scoringContext) float64 {
	// v1.1 §5.4: both tier-1 (Go) and tier-2 (TS/JS/Python) files
	// contribute to s_symbol. Tier-3 fallback files have no Symbols
	// populated so the len-check naturally excludes them.
	if len(f.Symbols) == 0 || len(ctx.anchors) == 0 {
		return 0.0
	}
	var blob strings.Builder
	for i, s := range f.Symbols {
		if i > 0 {
			blob.WriteByte('\x1f') // non-textual separator so anchors can't span symbols.
		}
		blob.WriteString(strings.ToLower(s.Name))
	}
	names := blob.String()
	hit := 0
	for _, a := range ctx.anchorsLower {
		if strings.Contains(names, a) {
			hit++
		}
	}
	v := float64(hit) / float64(len(ctx.anchors))
	if v > 1 {
		v = 1
	}
	return v
}

// sPackage — 1.0 if the file's package path/segment matches any anchor;
// 0.7 if the file's package directory is a sibling of such a package;
// 0.0 otherwise. siblingIdx is a precomputed parent-directory → package
// list map so the sibling check is O(siblings-in-parent) per call instead
// of O(all packages).
func sPackage(f index.FileEntry, siblingIdx map[string][]string, ctx *scoringContext) float64 {
	if f.Package == "" {
		return 0.0
	}
	pkgLower := strings.ToLower(f.Package)
	pkgBase := strings.ToLower(path.Base(f.Package))
	if _, ok := ctx.anchorLowerSet[pkgLower]; ok {
		return 1.0
	}
	if _, ok := ctx.anchorLowerSet[pkgBase]; ok {
		return 1.0
	}
	parent := path.Dir(f.Package)
	for _, dir := range siblingIdx[parent] {
		if dir == f.Package {
			continue
		}
		if _, ok := ctx.anchorLowerSet[strings.ToLower(path.Base(dir))]; ok {
			return 0.7
		}
	}
	return 0.0
}

// sortedPackageKeys returns the repo-local package directory list sorted
// ascending once, for reuse by resolveImportDir's tie-break logic.
func sortedPackageKeys(idx *index.Index) []string {
	keys := make([]string, 0, len(idx.Packages))
	for k := range idx.Packages {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// buildSiblingIndex groups package directories by their parent directory
// so sibling lookup is O(average packages per parent) instead of O(all
// packages).
func buildSiblingIndex(idx *index.Index) map[string][]string {
	out := make(map[string][]string, len(idx.Packages))
	for dir := range idx.Packages {
		parent := path.Dir(dir)
		out[parent] = append(out[parent], dir)
	}
	return out
}

// sTestPass1 produces a conservative pre-final s_test used to seed the
// pass-1 scores. It only returns the "test for any package referenced by
// the task" half of the rule because the full 1.0 tier requires knowing
// the tested file's pass-1 score.
func sTestPass1(f index.FileEntry, idx *index.Index, ctx *scoringContext, pass1 map[string]float64) float64 {
	_ = idx
	_ = pass1
	if !f.IsTest {
		return 0.0
	}
	if len(ctx.anchorLowerSet) == 0 {
		return 0.0
	}
	pkgBase := strings.ToLower(path.Base(f.Package))
	if _, ok := ctx.anchorLowerSet[pkgBase]; ok {
		return 0.5
	}
	return 0.0
}

// sTestFinal is the real §7.4.2.1 rule: 1.0 if the file is a test for a
// production file scoring ≥ 0.5 (pass-1), 0.5 if it is a test for any
// package the task references, 0.0 otherwise.
func sTestFinal(f *index.FileEntry, idx *index.Index, ctx *scoringContext, pass1 map[string]float64) float64 {
	if f == nil || !f.IsTest {
		return 0.0
	}
	for _, link := range f.TestLinks {
		if pass1[link] >= 0.5 {
			return 1.0
		}
	}
	if _, ok := ctx.anchorLowerSet[strings.ToLower(path.Base(f.Package))]; ok {
		return 0.5
	}
	_ = idx
	return 0.0
}

// sDoc — SPEC §7.4.2.1: for .md/.rst/.adoc files, Jaccard similarity
// between the document's lowercased alphanumeric token bag (first 2 KiB)
// and the task anchor set. The token bag is supplied by the caller
// through scoringContext.docTokens; if absent (legacy Score entry point),
// sDoc returns 0.0 to preserve backward compatibility.
func sDoc(f index.FileEntry, ctx *scoringContext) float64 {
	switch f.Extension {
	case ".md", ".markdown", ".rst", ".adoc":
	default:
		return 0.0
	}
	if ctx.docTokens == nil || len(ctx.anchorLowerSet) == 0 {
		return 0.0
	}
	tokens, ok := ctx.docTokens[f.Path]
	if !ok || len(tokens) == 0 {
		return 0.0
	}
	return jaccard(tokens, ctx.anchorLowerSet)
}

// BuildDocTokens scans the first 2 KiB of each markdown/rst/adoc file in
// idx, splits on non-alphanumerics, lowercases, and returns the path →
// token-set map that ScoreWithOptions consumes. Reads are best-effort;
// unreadable files simply produce empty sets.
func BuildDocTokens(idx *index.Index, repoRoot string, readFile func(path string) ([]byte, error)) map[string]map[string]struct{} {
	out := map[string]map[string]struct{}{}
	for _, f := range idx.Files {
		switch f.Extension {
		case ".md", ".markdown", ".rst", ".adoc":
		default:
			continue
		}
		body, err := readFile(f.Path)
		if err != nil {
			continue
		}
		if len(body) > 2048 {
			body = body[:2048]
		}
		out[f.Path] = alnumTokens(strings.ToLower(string(body)))
	}
	_ = repoRoot
	return out
}

// sConfig — 1.0 if the filename matches the spec's config filename set
// under the repo root or a config/configs directory AND the action type
// is feature/migration/refactor; 0.5 under the same filename condition
// for other action types; 0.0 otherwise.
func sConfig(f index.FileEntry, t task.Task, ctx *scoringContext) float64 {
	_ = ctx
	if !looksLikeConfigPath(f.Path) {
		return 0.0
	}
	switch t.Type {
	case manifest.ActionTypeFeature, manifest.ActionTypeMigration, manifest.ActionTypeRefactor:
		return 1.0
	}
	return 0.5
}

// sImport — pass 2. Highest matching tier wins (§7.4.2.1):
//
//	1.0 if any directly-imported package has pass1 ≥ 0.80
//	0.7 if any directly-imported package has pass1 ≥ 0.60
//	0.4 if any package within 2 hops has pass1 ≥ 0.60
//	0.0 otherwise
func sImport(f *index.FileEntry, importCache map[string]string, pkgScores map[string]float64, pkgImports map[string][]string) float64 {
	// v1.1 §5.4: both tier-1 (Go) and tier-2 (TS/JS/Python) files
	// contribute to s_import. Tier-2 import specifiers are stored as
	// the raw string ("./util", "node:fs", "os.path") and matched
	// against repo-local package directories by the same
	// resolveImportDir suffix rule that Go uses.
	if f == nil || len(f.Imports) == 0 {
		return 0.0
	}
	var best float64
	seen := map[string]struct{}{}
	for _, imp := range f.Imports {
		dir := importCache[imp]
		if dir == "" {
			continue
		}
		seen[dir] = struct{}{}
		if pkgScores[dir] >= 0.80 && best < 1.0 {
			best = 1.0
		} else if pkgScores[dir] >= 0.60 && best < 0.7 {
			best = 0.7
		}
	}
	if best >= 1.0 {
		return best
	}
	// 2-hop transitive walk with a visited set so an accidental import
	// cycle (e.g. a malformed vendored tree) terminates.
	frontier := make([]string, 0, len(seen))
	for d := range seen {
		frontier = append(frontier, d)
	}
	visited := make(map[string]struct{}, len(seen))
	for _, d := range frontier {
		visited[d] = struct{}{}
	}
	for hop := 0; hop < 1 && len(frontier) > 0; hop++ {
		next := make([]string, 0, len(frontier))
		for _, dir := range frontier {
			for _, subImp := range pkgImports[dir] {
				sub := importCache[subImp]
				if sub == "" {
					continue
				}
				if _, ok := visited[sub]; ok {
					continue
				}
				visited[sub] = struct{}{}
				next = append(next, sub)
				if pkgScores[sub] >= 0.60 && best < 0.4 {
					best = 0.4
				}
			}
		}
		frontier = next
	}
	return best
}

// packageMaxScores returns package-directory → max pass1 score among its
// files, per §7.4.2.1 ("score_pass1(pkg) = max(score_pass1(f) for f in
// files(pkg))").
func packageMaxScores(idx *index.Index, pass1 map[string]float64) map[string]float64 {
	out := map[string]float64{}
	for dir, pkg := range idx.Packages {
		var m float64
		for _, p := range pkg.Files {
			if s := pass1[p]; s > m {
				m = s
			}
		}
		out[dir] = m
	}
	return out
}

// resolveImportDir maps a Go import path to a repo-local package
// directory. Matching requires a full path-segment boundary: import
// "example.com/foo/internal/auth" matches the repo-local dir
// "internal/auth", but "example.com/foo/xinternal/auth" does NOT match
// "internal/auth" because the segment before would have to be the exact
// boundary "/internal/". The caller-supplied sortedKeys slice is sorted
// once per Score invocation (not per call) to preserve deterministic
// tie-break when multiple dirs could match.
func resolveImportDir(importPath string, sortedKeys []string) string {
	// Prefer the LONGEST matching dir so "internal/auth" wins over "auth"
	// for an import like "example.com/foo/internal/auth". Still stable:
	// among equal-length matches, the lexicographically smaller one wins
	// because sortedKeys is pre-sorted.
	var best string
	for _, dir := range sortedKeys {
		if importPath == dir || strings.HasSuffix(importPath, "/"+dir) {
			if len(dir) > len(best) {
				best = dir
			}
		}
	}
	if best != "" {
		return best
	}
	return ""
}

func alnumTokens(s string) map[string]struct{} {
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

func jaccard(a, b map[string]struct{}) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 0
	}
	inter := 0
	for k := range a {
		if _, ok := b[k]; ok {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

// looksLikeConfigPath implements §7.4.2.1's config-file pattern: the
// filename matches Makefile, *.mk, go.mod, go.sum, *.yaml, *.yml, *.toml,
// *.json AND lives at the repo root OR under a directory named
// config/configs.
func looksLikeConfigPath(rel string) bool {
	base := strings.ToLower(path.Base(rel))
	ext := strings.ToLower(path.Ext(rel))
	namedMatch := base == "makefile" || base == "go.mod" || base == "go.sum"
	extMatch := ext == ".mk" || ext == ".yaml" || ext == ".yml" || ext == ".toml" || ext == ".json"
	if !namedMatch && !extMatch {
		return false
	}
	dir := path.Dir(rel)
	if dir == "." || dir == "" {
		return true
	}
	for _, seg := range strings.Split(dir, "/") {
		if seg == "config" || seg == "configs" {
			return true
		}
	}
	return false
}
