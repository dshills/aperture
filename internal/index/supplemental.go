package index

import (
	"path"
	"sort"
	"strings"
)

// SupplementalCategory identifies a §7.1.3 supplemental-file bucket.
type SupplementalCategory string

const (
	SupSpec         SupplementalCategory = "spec"
	SupPlan         SupplementalCategory = "plan"
	SupAgents       SupplementalCategory = "agents"
	SupReadme       SupplementalCategory = "readme"
	SupArchitecture SupplementalCategory = "architecture"
	SupLintConfig   SupplementalCategory = "lint_config"
	SupTestConfig   SupplementalCategory = "test_config"
	SupBuildFiles   SupplementalCategory = "build_files"
	SupCIConfig     SupplementalCategory = "ci_config"
)

// supplementalRule is a compiled form of one row of the §7.1.3 table. All
// patterns are matched case-insensitively on the basename portion of the
// path; `**` is supported for recursive matching.
type supplementalRule struct {
	Category SupplementalCategory
	Glob     string
}

// supplementalRules enumerates every §7.1.3 pattern exactly as specified.
// Ordering is stable so two runs bucket files in the same order.
var supplementalRules = []supplementalRule{
	{SupSpec, "SPEC.md"},
	{SupSpec, "specs/**/SPEC.md"},
	{SupSpec, "docs/spec*.md"},

	{SupPlan, "PLAN.md"},
	{SupPlan, "specs/**/PLAN.md"},
	{SupPlan, "docs/plan*.md"},

	{SupAgents, "AGENTS.md"},
	{SupAgents, "CLAUDE.md"},
	{SupAgents, ".cursor/rules/*.md"},
	{SupAgents, ".cursorrules"},
	{SupAgents, ".github/copilot-instructions.md"},

	{SupReadme, "README.md"},
	{SupReadme, "README"},
	{SupReadme, "README.rst"},
	{SupReadme, "README.adoc"},

	{SupArchitecture, "docs/architecture*.md"},
	{SupArchitecture, "docs/design*.md"},
	{SupArchitecture, "docs/adr/**/*.md"},
	{SupArchitecture, "docs/decisions/**/*.md"},
	{SupArchitecture, "ARCHITECTURE.md"},
	{SupArchitecture, "DESIGN.md"},

	{SupLintConfig, ".golangci.yml"},
	{SupLintConfig, ".golangci.yaml"},
	{SupLintConfig, ".eslintrc*"},
	{SupLintConfig, ".prettierrc*"},
	{SupLintConfig, ".editorconfig"},
	{SupLintConfig, "ruff.toml"},
	{SupLintConfig, "pyproject.toml"},
	{SupLintConfig, "tsconfig.json"},
	{SupLintConfig, "biome.json"},

	{SupTestConfig, ".verifier.yaml"},
	{SupTestConfig, "jest.config.*"},
	{SupTestConfig, "vitest.config.*"},
	{SupTestConfig, "playwright.config.*"},
	{SupTestConfig, "pytest.ini"},
	{SupTestConfig, "tox.ini"},
	{SupTestConfig, "go.test.yaml"},

	{SupBuildFiles, "Makefile"},
	{SupBuildFiles, "makefile"},
	{SupBuildFiles, "*.mk"},
	{SupBuildFiles, "go.mod"},
	{SupBuildFiles, "go.sum"},
	{SupBuildFiles, "Dockerfile"},
	{SupBuildFiles, "docker-compose.y*ml"},
	{SupBuildFiles, "package.json"},
	{SupBuildFiles, "yarn.lock"},
	{SupBuildFiles, "pnpm-lock.yaml"},
	{SupBuildFiles, "Cargo.toml"},
	{SupBuildFiles, "pyproject.toml"},
	{SupBuildFiles, "Taskfile.y*ml"},

	{SupCIConfig, ".github/workflows/*.y*ml"},
	{SupCIConfig, ".gitlab-ci.yml"},
	{SupCIConfig, ".circleci/config.yml"},
}

// DetectSupplemental populates idx.SupplementalFiles by matching every
// indexed file against the §7.1.3 rule table. Each matched path is
// recorded under every category whose rule matches; a file may appear in
// multiple categories (e.g. pyproject.toml is both lint_config and
// build_files per the spec).
func DetectSupplemental(idx *Index) {
	// Precompute lowercased pattern + basename forms so every file-pattern
	// comparison skips a redundant ToLower on both sides.
	type compiledRule struct {
		cat        SupplementalCategory
		pattern    string
		isBareBase bool
	}
	compiled := make([]compiledRule, len(supplementalRules))
	for i, r := range supplementalRules {
		p := strings.ToLower(r.Glob)
		compiled[i] = compiledRule{cat: r.Category, pattern: p, isBareBase: !strings.ContainsRune(p, '/')}
	}

	buckets := map[SupplementalCategory]map[string]struct{}{}
	for _, f := range idx.Files {
		lowRel := strings.ToLower(f.Path)
		lowBase := path.Base(lowRel)
		for _, r := range compiled {
			if !matchLowered(r.pattern, lowRel, lowBase, r.isBareBase) {
				continue
			}
			if buckets[r.cat] == nil {
				buckets[r.cat] = map[string]struct{}{}
			}
			buckets[r.cat][f.Path] = struct{}{}
		}
	}
	for cat, set := range buckets {
		list := make([]string, 0, len(set))
		for p := range set {
			list = append(list, p)
		}
		sort.Strings(list)
		idx.SupplementalFiles[cat] = list
	}
}

// matchLowered takes already-lowercased inputs and evaluates the §7.1.3
// rule. Pre-lowered inputs avoid the tens-of-thousands of ToLower calls
// the naive per-pair form would incur on medium-sized repos.
func matchLowered(pattern, lowRel, lowBase string, bareBase bool) bool {
	if globMatchInsensitive(pattern, lowRel) {
		return true
	}
	if bareBase {
		if ok, _ := path.Match(pattern, lowBase); ok {
			return true
		}
	}
	return false
}

// globMatchInsensitive mirrors repo.globMatch but over lowercased input.
// `**` is handled by splitting on the sentinel; remaining fragments use
// path.Match.
func globMatchInsensitive(pattern, rel string) bool {
	if !strings.Contains(pattern, "**") {
		if strings.ContainsRune(pattern, '/') {
			ok, _ := path.Match(pattern, rel)
			return ok
		}
		ok, _ := path.Match(pattern, path.Base(rel))
		return ok
	}
	parts := strings.Split(pattern, "**")
	if len(parts) == 2 {
		prefix, suffix := parts[0], parts[1]
		if prefix != "" {
			if !strings.HasPrefix(rel, strings.TrimSuffix(prefix, "/")) {
				return false
			}
			rel = strings.TrimPrefix(rel, strings.TrimSuffix(prefix, "/"))
			rel = strings.TrimPrefix(rel, "/")
		}
		if suffix == "" {
			return true
		}
		suffix = strings.TrimPrefix(suffix, "/")
		// Any trailing path-component boundary is acceptable.
		for {
			if ok, _ := path.Match(suffix, rel); ok {
				return true
			}
			idx := strings.Index(rel, "/")
			if idx < 0 {
				return false
			}
			rel = rel[idx+1:]
		}
	}
	return false
}
