package cli

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/dshills/aperture/internal/cache"
	"github.com/dshills/aperture/internal/config"
	"github.com/dshills/aperture/internal/manifest"
	"github.com/dshills/aperture/internal/pipeline"
	"github.com/dshills/aperture/internal/repo"
	"github.com/dshills/aperture/internal/task"
	"github.com/dshills/aperture/internal/version"
)

// commonInputs carries the fields that both `plan` and `explain` need to
// assemble before calling BuildManifest. Keeping the preparation in one
// place avoids drift between the two commands.
type commonInputs struct {
	RepoRoot    string
	Config      config.Config
	Task        task.Task
	Fingerprint string
	Languages   []string
	Exclusions  []repo.Exclusion
	PipelineRes pipeline.Result
	Scope       repo.Scope
}

// resolveScope applies the §7.4.5 CLI-vs-config precedence to the
// user-supplied --scope flag and the config's defaults.scope field:
//
//   - When the CLI flag was explicitly set, it wins. The sentinels ""
//     and "." unset the config scope for this invocation.
//   - Otherwise the config's defaults.scope applies (also subject to
//     the same §7.4.4 validation).
//
// The resolved scope is returned as repo.Scope; an unset scope is the
// zero value. Validation failures produce exit 4.
func resolveScope(repoRoot string, cfg config.Config, flagValue string, flagSet bool) (repo.Scope, error) {
	if flagSet {
		s, err := repo.ResolveScope(repoRoot, flagValue)
		if err != nil {
			return repo.Scope{}, exitErr(exitCodeBadRepo, err)
		}
		return s, nil
	}
	if cfg.Defaults.Scope == "" {
		return repo.Scope{}, nil
	}
	s, err := repo.ResolveScope(repoRoot, cfg.Defaults.Scope)
	if err != nil {
		return repo.Scope{}, exitErr(exitCodeBadRepo, err)
	}
	return s, nil
}

// scopeFlagInputs carries the post-parse `--scope` state from each
// CLI command. Passing a struct keeps preparePlan's signature stable
// as more flags accumulate.
type scopeFlagInputs struct {
	Value string
	Set   bool
}

// preparePlan resolves the repo root, reads the task, loads config, walks
// the repo, and computes the fingerprint — the shared preamble for both
// `aperture plan` and `aperture explain`. taskPath is the positional
// TASK_FILE (may be nil for inline-only). inlineText is the -p value.
// scopeFlag carries the CLI --scope state (value + whether-set), so the
// function can apply the §7.4.5 CLI-vs-config override rules.
func preparePlan(ctx context.Context, repoFlag string, taskArgs []string, inlineText, configFlag string, scopeFlag scopeFlagInputs) (commonInputs, error) {
	repoRoot, err := resolveRepoRoot(repoFlag)
	if err != nil {
		return commonInputs{}, exitErr(exitCodeBadRepo, err)
	}
	rawText, source, isMarkdown, err := readTask(taskArgs, inlineText)
	if err != nil {
		return commonInputs{}, exitErr(exitCodeBadTask, err)
	}
	cfgPath := configFlag
	if cfgPath == "" {
		cfgPath = filepath.Join(repoRoot, ".aperture.yaml")
	}
	cfg, err := config.Load(config.LoadOptions{Path: cfgPath})
	if err != nil {
		return commonInputs{}, exitErr(exitCodeBadConfig, err)
	}
	if err := cfg.Validate(); err != nil {
		return commonInputs{}, exitErr(exitCodeBadConfig, err)
	}
	parsed := task.Parse(rawText, task.ParseOptions{Source: source, IsMarkdown: isMarkdown})

	// Attach the persistent AST cache. Bind it to
	// SelectionLogicVersion (currently "sel-v2"), not the aperture
	// build version — docs-only patches shouldn't invalidate the
	// entire cached AST parse set. InvalidateAll-on-drift still
	// handles the on-disk schema bump (cache-v1 → cache-v2) that
	// accompanied this keying change. The v1.1 §8.3 bump to sel-v2
	// flowed naturally through this reference.
	cacheDir := filepath.Join(repoRoot, ".aperture", "cache")
	astCache := cache.New(cacheDir, manifest.SelectionLogicVersion)
	if astCache.DetectSchemaDrift() {
		astCache.InvalidateAll("cache_schema_version mismatch")
	}

	res, err := pipeline.Build(ctx, pipeline.BuildOptions{
		Root:              repoRoot,
		DefaultExcludes:   config.DefaultExclusions(),
		UserExcludes:      userOnly(cfg),
		Cache:             astCache,
		TypeScriptEnabled: cfg.Languages.TypeScript.Enabled,
		JavaScriptEnabled: cfg.Languages.JavaScript.Enabled,
		PythonEnabled:     cfg.Languages.Python.Enabled,
	})
	if err != nil {
		return commonInputs{}, exitErr(exitCodeInternal, fmt.Errorf("index build: %w", err))
	}
	fp, err := repo.Fingerprint(walkerFiles(res.Index), version.Version)
	if err != nil {
		return commonInputs{}, exitErr(exitCodeInternal, fmt.Errorf("fingerprint: %w", err))
	}

	scope, err := resolveScope(repoRoot, cfg, scopeFlag.Value, scopeFlag.Set)
	if err != nil {
		return commonInputs{}, err
	}

	return commonInputs{
		RepoRoot:    repoRoot,
		Config:      cfg,
		Task:        parsed,
		Fingerprint: fp,
		Languages:   res.Index.LanguageHints(),
		Exclusions:  res.Exclusions,
		PipelineRes: res,
		Scope:       scope,
	}, nil
}
