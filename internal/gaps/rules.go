package gaps

import (
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/dshills/aperture/internal/index"
	"github.com/dshills/aperture/internal/manifest"
	"github.com/dshills/aperture/internal/selection"
)

// identifierPattern is §7.3.2 Rule 1 — a likely type/identifier token. Used
// by the unresolved_symbol_dependency rule. Compiled once at package init
// rather than on every invocation.
//
// The regex is SPEC-MANDATED verbatim: §7.7.3's unresolved_symbol_dependency
// trigger condition specifies `^[A-Z][A-Za-z0-9_]{2,}$`. That deliberately
// excludes 2-character identifiers like "DB", "ID", "UI" because those are
// ambiguous in free-form task text (a user saying "use the DB" is unlikely
// to be naming a Go symbol). Altering the regex here — even to be more
// permissive — is a spec deviation and would require a spec revision.
var identifierPattern = regexp.MustCompile(`^[A-Z][A-Za-z0-9_]{2,}$`)

// externalContractNeedles is the §7.7.3 filename-substring filter for the
// missing_external_contract rule. Lifted to package scope so looksLike-
// ExternalContract doesn't reallocate it on every call.
var externalContractNeedles = []string{"openapi", "swagger", "schema", "api"}

// missingSpec — §7.7.3: action type feature/refactor/migration/investigation
// AND no file matching SPEC.md / specs/**/SPEC.md / docs/spec*.md / AGENTS.md.
func missingSpec(in Inputs) []manifest.Gap {
	switch in.Task.Type {
	case manifest.ActionTypeFeature, manifest.ActionTypeRefactor,
		manifest.ActionTypeMigration, manifest.ActionTypeInvestigation:
	default:
		return nil
	}
	if indexHasSupplemental(in.Index, index.SupSpec) ||
		indexHasSupplemental(in.Index, index.SupAgents) {
		return nil
	}
	return []manifest.Gap{{
		Type:        manifest.GapMissingSpec,
		Severity:    manifest.GapSeverityWarning,
		Description: "no SPEC.md / AGENTS.md / docs/spec*.md found for a task that typically needs one",
		Evidence: []string{
			fmt.Sprintf("action_type=%s", in.Task.Type),
			"no spec/agents supplemental file detected",
		},
		SuggestedRemediation: remediationMissingSpec(),
	}}
}

// missingTests — §7.7.3: action type feature/bugfix/refactor/migration AND no
// _test.go (or language-appropriate test filename) assigned any load mode
// with score(f) ≥ 0.50.
func missingTests(in Inputs) []manifest.Gap {
	switch in.Task.Type {
	case manifest.ActionTypeFeature, manifest.ActionTypeBugfix,
		manifest.ActionTypeRefactor, manifest.ActionTypeMigration:
	default:
		return nil
	}
	for _, a := range in.Assignments {
		if a.Score < 0.50 {
			continue
		}
		if isTestFilename(a.Path) {
			return nil
		}
	}
	return []manifest.Gap{{
		Type:        manifest.GapMissingTests,
		Severity:    manifest.GapSeverityWarning,
		Description: "no test file scored ≥0.50 and was selected for this task",
		Evidence: []string{
			fmt.Sprintf("action_type=%s", in.Task.Type),
			"no _test.go / *.test.ts / *_test.py in selections at threshold",
		},
		SuggestedRemediation: remediationMissingTests(),
	}}
}

// missingConfigContext — §7.7.3: task anchors overlap with config-related
// keywords AND no file with s_config > 0 was selected.
func missingConfigContext(in Inputs) []manifest.Gap {
	cfgKeywords := map[string]struct{}{
		"config": {}, "env": {}, "environment": {}, "settings": {},
		"flag": {}, "flags": {}, "secret": {}, "database": {}, "db": {},
		"port": {}, "host": {}, "url": {}, "token": {},
	}
	if !anchorsOverlap(in.Task.Anchors, cfgKeywords) {
		return nil
	}
	for _, a := range in.Assignments {
		if looksLikeConfigPath(a.Path) {
			return nil
		}
	}
	return []manifest.Gap{{
		Type:        manifest.GapMissingConfigContext,
		Severity:    manifest.GapSeverityWarning,
		Description: "task mentions config/env/settings but no config file was selected",
		Evidence: []string{
			"anchors overlap with config/env/settings/flag/secret",
			"zero selections satisfy s_config",
		},
		SuggestedRemediation: remediationMissingConfigContext(),
	}}
}

// unresolvedSymbolDependency — §7.7.3: index has Go files AND task text
// contains a token matching `^[A-Z][A-Za-z0-9_]{2,}$` AND no Go file in
// the index exports that identifier (case-insensitive). Emits at most
// five gaps, ordered by byte-wise ascending symbol name. Suppressed on
// non-Go repos per the round-7 fix.
func unresolvedSymbolDependency(in Inputs) []manifest.Gap {
	if !hasGoFiles(in.Index) {
		return nil
	}
	// Engine() always pre-populates in.exportedSymbols before calling any
	// rule, so no nil guard is needed here.
	exported := in.exportedSymbols

	// Collect unresolved identifiers from task anchors. Rule 1 of §7.3.2
	// already extracts them; we walk the parsed set once and dedupe by
	// the lookup key (lowercased) so "RefreshToken" and "refreshtoken"
	// don't both emit — the `exported` map is lowercased too, so using
	// the same normalized form for both sides avoids case drift between
	// the membership test and the output set.
	unresolved := map[string]string{}
	for _, a := range in.Task.Anchors {
		if !identifierPattern.MatchString(a) {
			continue
		}
		key := strings.ToLower(a)
		if _, ok := exported[key]; ok {
			continue
		}
		if _, already := unresolved[key]; !already {
			unresolved[key] = a
		}
	}
	if len(unresolved) == 0 {
		return nil
	}
	// Sort by the normalized key so ordering is stable across runs, but
	// render the display name in the user's original casing.
	keys := make([]string, 0, len(unresolved))
	for k := range unresolved {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) > 5 {
		keys = keys[:5]
	}
	out := make([]manifest.Gap, 0, len(keys))
	for _, k := range keys {
		name := unresolved[k]
		out = append(out, manifest.Gap{
			Type:        manifest.GapUnresolvedSymbolDependency,
			Severity:    manifest.GapSeverityWarning,
			Description: fmt.Sprintf("symbol %q is referenced by the task but not exported by any indexed Go file", name),
			Evidence: []string{
				fmt.Sprintf("task_anchor=%s", name),
				"no case-insensitive match in repo-wide Go symbol table",
			},
			SuggestedRemediation: remediationUnresolvedSymbol(name),
		})
	}
	return out
}

// ambiguousOwnership — §7.7.3: highest-scoring selected file's Go package
// contains ≥2 other files with score ≥0.60 AND no single file in the
// package has score ≥0.80 AND action type is bugfix/feature/refactor.
// Default severity info (not warning).
func ambiguousOwnership(in Inputs) []manifest.Gap {
	switch in.Task.Type {
	case manifest.ActionTypeBugfix, manifest.ActionTypeFeature, manifest.ActionTypeRefactor:
	default:
		return nil
	}
	if len(in.Assignments) == 0 {
		return nil
	}
	// Highest-scoring selected file.
	var top selection.Assignment
	for i, a := range in.Assignments {
		if i == 0 || a.Score > top.Score {
			top = a
		}
	}
	topFile := in.Index.File(top.Path)
	if topFile == nil || topFile.Package == "" {
		return nil
	}
	pkg := in.Index.Packages[topFile.Package]
	if pkg == nil {
		return nil
	}

	// §7.7.3 reads the rule against the package's full file set, so we
	// consult in.Scores (which covers every scored file) and fall back
	// to the assignment-only view when the caller didn't populate it.
	scoresByPath := in.Scores
	if scoresByPath == nil {
		scoresByPath = map[string]float64{}
		for _, a := range in.Assignments {
			scoresByPath[a.Path] = a.Score
		}
	}

	var (
		overSixty     int
		anyOverEighty bool
	)
	for _, p := range pkg.Files {
		if p == top.Path {
			continue
		}
		s := scoresByPath[p]
		if s >= 0.60 {
			overSixty++
		}
		if s >= 0.80 {
			anyOverEighty = true
		}
	}
	if top.Score >= 0.80 {
		anyOverEighty = true
	}

	if overSixty < 2 || anyOverEighty {
		return nil
	}
	return []manifest.Gap{{
		Type:        manifest.GapAmbiguousOwnership,
		Severity:    manifest.GapSeverityInfo,
		Description: fmt.Sprintf("package %q has ≥2 other files at score ≥0.60 with no clear owner", topFile.Package),
		Evidence: []string{
			fmt.Sprintf("package=%s", topFile.Package),
			fmt.Sprintf("peer_candidates_over_0.60=%d", overSixty),
			"no single file reaches score ≥0.80",
		},
		SuggestedRemediation: remediationAmbiguousOwnership(topFile.Package),
	}}
}

// missingRuntimePath — §7.7.3: index has Go files AND action type is
// feature/bugfix/migration AND no selected Go file has any io:* side-
// effect tag AND task anchors overlap with runtime-adjacent keywords.
// Suppressed on non-Go repos.
func missingRuntimePath(in Inputs) []manifest.Gap {
	if !hasGoFiles(in.Index) {
		return nil
	}
	switch in.Task.Type {
	case manifest.ActionTypeFeature, manifest.ActionTypeBugfix, manifest.ActionTypeMigration:
	default:
		return nil
	}
	runtime := map[string]struct{}{
		"request": {}, "handler": {}, "route": {}, "endpoint": {},
		"server": {}, "db": {}, "query": {}, "write": {}, "read": {},
		"send": {}, "publish": {}, "consume": {},
	}
	if !anchorsOverlap(in.Task.Anchors, runtime) {
		return nil
	}
	for _, a := range in.Assignments {
		f := in.Index.File(a.Path)
		if f == nil || f.Language != "go" {
			continue
		}
		for _, t := range f.SideEffects {
			if strings.HasPrefix(t, "io:") {
				return nil
			}
		}
	}
	return []manifest.Gap{{
		Type:        manifest.GapMissingRuntimePath,
		Severity:    manifest.GapSeverityWarning,
		Description: "task talks about request/route/db-style runtime behavior but no selected Go file carries an io:* side-effect tag",
		Evidence: []string{
			fmt.Sprintf("action_type=%s", in.Task.Type),
			"runtime-keyword anchors matched",
			"zero selections carry io:* tags",
		},
		SuggestedRemediation: remediationMissingRuntimePath(),
	}}
}

// missingExternalContract — §7.7.3: task anchors overlap with contract
// keywords AND no file with extension in {.proto, .graphql, .yaml, .yml,
// .json} matching *openapi*, *swagger*, *schema*, or *api* was selected.
func missingExternalContract(in Inputs) []manifest.Gap {
	contract := map[string]struct{}{
		"api": {}, "rpc": {}, "grpc": {}, "openapi": {}, "graphql": {},
		"protobuf": {}, "proto": {}, "interface": {}, "contract": {}, "schema": {},
	}
	if !anchorsOverlap(in.Task.Anchors, contract) {
		return nil
	}
	for _, a := range in.Assignments {
		if looksLikeExternalContract(a.Path) {
			return nil
		}
	}
	return []manifest.Gap{{
		Type:        manifest.GapMissingExternalContract,
		Severity:    manifest.GapSeverityWarning,
		Description: "task references an external contract (api/rpc/schema) but no *openapi*/*swagger*/*schema*/*api* file was selected",
		Evidence: []string{
			"anchors overlap with api/rpc/grpc/openapi/graphql/protobuf/contract/schema",
			"zero selections match the contract filename patterns",
		},
		SuggestedRemediation: remediationMissingExternalContract(),
	}}
}

// oversizedPrimaryContext — §7.7.3: emitted when §7.6.5 underflow fires
// (blocking), OR when a highly_relevant file was demoted from full due to
// size_band=large or budget_insufficient (warning). At most one gap per
// run.
func oversizedPrimaryContext(in Inputs) []manifest.Gap {
	if in.Underflow {
		// Already emitted by the selector-side underflow hook; engine
		// does not duplicate it here.
		return nil
	}
	// Sort demoted paths so the chosen gap target is deterministic —
	// map iteration order would otherwise vary per run, violating the
	// manifest determinism contract (§8.3).
	paths := make([]string, 0, len(in.Demotions))
	for p, reason := range in.Demotions {
		if reason != "" {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		return nil
	}
	p := paths[0]
	reason := in.Demotions[p]
	return []manifest.Gap{{
		Type:        manifest.GapOversizedPrimaryContext,
		Severity:    manifest.GapSeverityWarning,
		Description: fmt.Sprintf("%q was demoted from full to a summary (%s)", p, reason),
		Evidence: []string{
			fmt.Sprintf("demotion_reason=%s", reason),
			fmt.Sprintf("path=%s", p),
		},
		SuggestedRemediation: remediationOversized(),
	}}
}

// taskUnderspecified — §7.7.3: task has <2 anchors OR action type unknown
// OR no candidate reaches score ≥0.60.
func taskUnderspecified(in Inputs) []manifest.Gap {
	var triggers []string
	if len(in.Task.Anchors) < 2 {
		triggers = append(triggers, fmt.Sprintf("anchors=%d (<2)", len(in.Task.Anchors)))
	}
	if in.Task.Type == manifest.ActionTypeUnknown {
		triggers = append(triggers, "action_type=unknown")
	}
	maxScore := 0.0
	for _, a := range in.Assignments {
		if a.Score > maxScore {
			maxScore = a.Score
		}
	}
	if maxScore < 0.60 {
		triggers = append(triggers, fmt.Sprintf("max_candidate_score=%.2f (<0.60)", maxScore))
	}
	if len(triggers) == 0 {
		return nil
	}
	return []manifest.Gap{{
		Type:                 manifest.GapTaskUnderspecified,
		Severity:             manifest.GapSeverityWarning,
		Description:          "task is too thin to produce a confident selection",
		Evidence:             triggers,
		SuggestedRemediation: remediationTaskUnderspecified(),
	}}
}

// ---- helpers ----

func indexHasSupplemental(idx *index.Index, cat index.SupplementalCategory) bool {
	return len(idx.SupplementalFiles[cat]) > 0
}

func anchorsOverlap(anchors []string, keywords map[string]struct{}) bool {
	for _, a := range anchors {
		if _, ok := keywords[strings.ToLower(a)]; ok {
			return true
		}
	}
	return false
}

func isTestFilename(p string) bool {
	lower := strings.ToLower(p)
	return strings.HasSuffix(lower, "_test.go") ||
		strings.HasSuffix(lower, ".test.ts") ||
		strings.HasSuffix(lower, ".test.tsx") ||
		strings.HasSuffix(lower, "_test.py")
}

func looksLikeConfigPath(rel string) bool {
	base := strings.ToLower(path.Base(rel))
	ext := strings.ToLower(path.Ext(rel))
	if base == "makefile" || base == "go.mod" || base == "go.sum" {
		return true
	}
	switch ext {
	case ".mk", ".yaml", ".yml", ".toml", ".json":
	default:
		return false
	}
	if !strings.ContainsRune(rel, '/') {
		return true
	}
	for _, seg := range strings.Split(rel, "/") {
		if seg == "config" || seg == "configs" {
			return true
		}
	}
	return false
}

func looksLikeExternalContract(rel string) bool {
	lower := strings.ToLower(rel)
	ext := strings.ToLower(path.Ext(rel))
	switch ext {
	case ".proto", ".graphql":
		return true
	case ".yaml", ".yml", ".json":
	default:
		return false
	}
	// Name filter: *openapi*, *swagger*, *schema*, *api*. The full path
	// contains the basename, so a single Contains against `lower` covers
	// both "api/openapi.yaml" and plain "openapi.yaml" style hits.
	for _, needle := range externalContractNeedles {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}
