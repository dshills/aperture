//go:build !notier2

package cli

import (
	"path/filepath"
	"testing"

	"github.com/dshills/aperture/internal/config"
	"github.com/dshills/aperture/internal/manifest"
	"github.com/dshills/aperture/internal/pipeline"
	"github.com/dshills/aperture/internal/repo"
	"github.com/dshills/aperture/internal/task"
	"github.com/dshills/aperture/internal/version"
)

// buildPolyglotInputs mirrors buildFixtureInputs but for the
// polyglot fixture and with tier-2 languages enabled.
func buildPolyglotInputs(t *testing.T, rawTask string) buildInputs {
	t.Helper()
	fixture, err := filepath.Abs("../../testdata/eval/polyglot-resolver/repo")
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	parsed := task.Parse(rawTask, task.ParseOptions{Source: "<inline>"})
	res, err := pipeline.Build(pipeline.BuildOptions{
		Root:              fixture,
		DefaultExcludes:   config.DefaultExclusions(),
		TypeScriptEnabled: true,
		JavaScriptEnabled: true,
		PythonEnabled:     true,
	})
	if err != nil {
		t.Fatal(err)
	}
	fp, err := repo.Fingerprint(walkerFiles(res.Index), version.Version)
	if err != nil {
		t.Fatal(err)
	}
	return buildInputs{
		Config:      cfg,
		Task:        parsed,
		RepoRoot:    fixture,
		ModelFlag:   "",
		BudgetFlag:  200000,
		Fingerprint: fp,
		Languages:   res.Index.LanguageHints(),
		Exclusions:  res.Exclusions,
		Index:       res.Index,
	}
}

// TestTier2_PolyglotResolverSelectsTypeScript is PLAN §Phase 4's
// polyglot-resolver acceptance: v1.1 picks resolver.ts as a
// selection (not just reachable) when the task names the resolver
// and its symbols.
func TestTier2_PolyglotResolverSelectsTypeScript(t *testing.T) {
	taskText := "Add a rate-limit header to resolver.ts. Update Resolver.resolveRateLimit and applyRateLimitHeader. The RateLimitResolver class emits RATE_LIMIT_HEADER."
	in := buildPolyglotInputs(t, taskText)
	m, err := BuildManifest(in)
	if err != nil {
		t.Fatalf("BuildManifest: %v", err)
	}
	var saw bool
	for _, s := range m.Selections {
		if s.Path == "src/graphql/resolver.ts" {
			saw = true
			if s.RelevanceScore < 0.30 {
				t.Errorf("resolver.ts relevance=%v, want ≥ 0.30", s.RelevanceScore)
			}
		}
	}
	if !saw {
		t.Errorf("resolver.ts missing from selections: %+v", m.Selections)
	}
}

// TestTier2_LanguageTiersEmitted verifies §10.1:
// generation_metadata.language_tiers is populated with the observed
// language→tier mapping.
func TestTier2_LanguageTiersEmitted(t *testing.T) {
	in := buildPolyglotInputs(t, "Touch resolver.ts")
	m, err := BuildManifest(in)
	if err != nil {
		t.Fatal(err)
	}
	tiers := m.GenerationMetadata.LanguageTiers
	if tiers["go"] != "tier1_deep" {
		t.Errorf("go tier=%q, want tier1_deep", tiers["go"])
	}
	if tiers["typescript"] != "tier2_structural" {
		t.Errorf("typescript tier=%q, want tier2_structural", tiers["typescript"])
	}
}

// TestTier2_DisabledFallsBackToTier3 covers the §9
// `languages.typescript.enabled: false` case: the config opt-out
// lands .ts files at tier3_lexical and they no longer contribute to
// s_symbol. resolver.ts should fall out of the selection set even
// with a TypeScript-heavy task.
func TestTier2_DisabledFallsBackToTier3(t *testing.T) {
	fixture, err := filepath.Abs("../../testdata/eval/polyglot-resolver/repo")
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Languages.TypeScript.Enabled = false
	parsed := task.Parse(
		"Add a rate-limit header to resolver.ts. Update Resolver.resolveRateLimit and applyRateLimitHeader.",
		task.ParseOptions{Source: "<inline>"},
	)
	res, err := pipeline.Build(pipeline.BuildOptions{
		Root:              fixture,
		DefaultExcludes:   config.DefaultExclusions(),
		TypeScriptEnabled: false, // the flag-off branch
		JavaScriptEnabled: true,
		PythonEnabled:     true,
	})
	if err != nil {
		t.Fatal(err)
	}
	fp, err := repo.Fingerprint(walkerFiles(res.Index), version.Version)
	if err != nil {
		t.Fatal(err)
	}
	m, err := BuildManifest(buildInputs{
		Config:      cfg,
		Task:        parsed,
		RepoRoot:    fixture,
		BudgetFlag:  200000,
		Fingerprint: fp,
		Languages:   res.Index.LanguageHints(),
		Exclusions:  res.Exclusions,
		Index:       res.Index,
	})
	if err != nil {
		t.Fatal(err)
	}
	tiers := m.GenerationMetadata.LanguageTiers
	if tiers["typescript"] != "tier3_lexical" {
		t.Errorf("tier toggle: typescript=%q, want tier3_lexical", tiers["typescript"])
	}
}

// TestTier2_TestLinkingPairsJSTS verifies the §7.3.3 JS/TS
// priority linking: resolver.ts ↔ resolver.test.ts.
func TestTier2_TestLinkingPairsJSTS(t *testing.T) {
	fixture, err := filepath.Abs("../../testdata/eval/polyglot-resolver/repo")
	if err != nil {
		t.Fatal(err)
	}
	res, err := pipeline.Build(pipeline.BuildOptions{
		Root:              fixture,
		DefaultExcludes:   config.DefaultExclusions(),
		TypeScriptEnabled: true,
		JavaScriptEnabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	var resolver, testFile manifest.Selection
	_ = resolver
	_ = testFile
	var resolverLinks, testLinks []string
	for _, f := range res.Index.Files {
		switch f.Path {
		case "src/graphql/resolver.ts":
			resolverLinks = f.TestLinks
		case "src/graphql/resolver.test.ts":
			testLinks = f.TestLinks
		}
	}
	found := false
	for _, l := range resolverLinks {
		if l == "src/graphql/resolver.test.ts" {
			found = true
		}
	}
	if !found {
		t.Errorf("resolver.ts missing link to resolver.test.ts: %v", resolverLinks)
	}
	found = false
	for _, l := range testLinks {
		if l == "src/graphql/resolver.ts" {
			found = true
		}
	}
	if !found {
		t.Errorf("resolver.test.ts missing link back: %v", testLinks)
	}
}
