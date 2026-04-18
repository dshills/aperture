package cli

import (
	"fmt"
	"path/filepath"

	"github.com/dshills/aperture/internal/cache"
	"github.com/dshills/aperture/internal/config"
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
}

// preparePlan resolves the repo root, reads the task, loads config, walks
// the repo, and computes the fingerprint — the shared preamble for both
// `aperture plan` and `aperture explain`. taskPath is the positional
// TASK_FILE (may be nil for inline-only). inlineText is the -p value.
func preparePlan(repoFlag string, taskArgs []string, inlineText, configFlag string) (commonInputs, error) {
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

	// Attach the persistent AST cache. InvalidateAll-on-drift keeps
	// version-bumped binaries from reading stale entries.
	cacheDir := filepath.Join(repoRoot, ".aperture", "cache")
	astCache := cache.New(cacheDir, version.Version)
	if astCache.DetectSchemaDrift() {
		astCache.InvalidateAll("cache_schema_version mismatch")
	}

	res, err := pipeline.Build(pipeline.BuildOptions{
		Root:            repoRoot,
		DefaultExcludes: config.DefaultExclusions(),
		UserExcludes:    userOnly(cfg),
		Cache:           astCache,
	})
	if err != nil {
		return commonInputs{}, exitErr(exitCodeInternal, fmt.Errorf("index build: %w", err))
	}
	fp, err := repo.Fingerprint(walkerFiles(res.Index), version.Version)
	if err != nil {
		return commonInputs{}, exitErr(exitCodeInternal, fmt.Errorf("fingerprint: %w", err))
	}

	return commonInputs{
		RepoRoot:    repoRoot,
		Config:      cfg,
		Task:        parsed,
		Fingerprint: fp,
		Languages:   res.Index.LanguageHints(),
		Exclusions:  res.Exclusions,
		PipelineRes: res,
	}, nil
}
