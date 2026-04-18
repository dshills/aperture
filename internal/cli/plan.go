package cli

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/dshills/aperture/internal/config"
	"github.com/dshills/aperture/internal/index"
	"github.com/dshills/aperture/internal/manifest"
	"github.com/dshills/aperture/internal/pipeline"
	"github.com/dshills/aperture/internal/repo"
	"github.com/dshills/aperture/internal/task"
	"github.com/dshills/aperture/internal/version"
)

type planFlags struct {
	repo           string
	inline         string
	model          string
	budget         int
	format         string
	out            string
	failOnGaps     bool
	minFeasibility float64
	configPath     string
}

func newPlanCommand() *cobra.Command {
	var f planFlags
	cmd := &cobra.Command{
		Use:   "plan [TASK_FILE]",
		Short: "Generate a context manifest for a task",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && f.inline == "" {
				return usageErr("either TASK_FILE or -p/--prompt is required")
			}
			if len(args) > 0 && f.inline != "" {
				return usageErr("TASK_FILE and -p/--prompt are mutually exclusive")
			}
			return runPlan(cmd, args, f)
		},
	}
	cmd.Flags().StringVar(&f.repo, "repo", "", "Repository root (defaults to cwd)")
	cmd.Flags().StringVarP(&f.inline, "prompt", "p", "", "Inline task text")
	cmd.Flags().StringVar(&f.model, "model", "", "Target model id (e.g., claude-sonnet, gpt-4o)")
	cmd.Flags().IntVar(&f.budget, "budget", 0, "Total token budget override")
	cmd.Flags().StringVar(&f.format, "format", "json", "Output format: json or markdown")
	cmd.Flags().StringVar(&f.out, "out", "", "Write manifest to this path (default: stdout)")
	cmd.Flags().BoolVar(&f.failOnGaps, "fail-on-gaps", false, "Exit 8 if any blocking gap is present")
	cmd.Flags().Float64Var(&f.minFeasibility, "min-feasibility", 0.0, "Exit 7 if feasibility < threshold")
	cmd.Flags().StringVar(&f.configPath, "config", "", "Path to .aperture.yaml (default: <repo>/.aperture.yaml)")
	return cmd
}

func runPlan(_ *cobra.Command, args []string, f planFlags) error {
	repoRoot, err := resolveRepoRoot(f.repo)
	if err != nil {
		return exitErr(4, err)
	}

	rawText, source, isMarkdown, err := readTask(args, f.inline)
	if err != nil {
		return exitErr(3, err)
	}

	cfgPath := f.configPath
	if cfgPath == "" {
		cfgPath = filepath.Join(repoRoot, ".aperture.yaml")
	}
	cfg, err := config.Load(config.LoadOptions{Path: cfgPath})
	if err != nil {
		return exitErr(5, err)
	}
	if err := cfg.Validate(); err != nil {
		return exitErr(5, err)
	}

	parsed := task.Parse(rawText, task.ParseOptions{Source: source, IsMarkdown: isMarkdown})

	res, err := pipeline.Build(pipeline.BuildOptions{
		Root:            repoRoot,
		DefaultExcludes: config.DefaultExclusions(),
		UserExcludes:    userOnly(cfg),
	})
	if err != nil {
		return exitErr(1, fmt.Errorf("index build: %w", err))
	}

	fingerprint, err := repo.Fingerprint(walkerFiles(res.Index), version.Version)
	if err != nil {
		return exitErr(1, fmt.Errorf("fingerprint: %w", err))
	}

	m, err := BuildManifest(buildInputs{
		Config:      cfg,
		Task:        parsed,
		RepoRoot:    repoRoot,
		ModelFlag:   f.model,
		BudgetFlag:  f.budget,
		Fingerprint: fingerprint,
		Languages:   res.Index.LanguageHints(),
		Exclusions:  res.Exclusions,
		Index:       res.Index,
	})
	// BuildManifest returns (manifest, ExitCodeError) on underflow — we
	// still emit the manifest body, then propagate the exit code.
	var ec *ExitCodeError
	underflow := false
	if err != nil && errors.As(err, &ec) && ec.Code == 9 && m != nil {
		underflow = true
	} else if err != nil {
		return err
	}

	jsonBytes, err := manifest.EmitJSON(m)
	if err != nil {
		return exitErr(6, fmt.Errorf("serialize manifest: %w", err))
	}
	if err := manifest.Validate(jsonBytes); err != nil {
		return exitErr(6, err)
	}

	switch f.format {
	case "json":
		if err := writeOutput(f.out, jsonBytes); err != nil {
			return exitErr(6, err)
		}
	case "markdown":
		md := manifest.EmitMarkdown(m)
		if err := writeOutput(f.out, md); err != nil {
			return exitErr(6, err)
		}
	default:
		return usageErr(fmt.Sprintf("unknown --format %q", f.format))
	}
	if underflow {
		return exitErr(9, fmt.Errorf("budget underflow: manifest emitted with incomplete=true"))
	}
	return nil
}

type buildInputs struct {
	Config      config.Config
	Task        task.Task
	RepoRoot    string
	ModelFlag   string
	BudgetFlag  int
	Fingerprint string
	Languages   []string
	Exclusions  []repo.Exclusion
	Index       *index.Index
}

// userOnly returns the config's user-added exclude patterns — i.e. cfg.Exclude
// minus the built-in defaults — so the pipeline can report them under the
// "user_pattern" reason without double-matching a default pattern against the
// same file.
func userOnly(cfg config.Config) []string {
	defaults := config.DefaultExclusions()
	skip := make(map[string]struct{}, len(defaults))
	for _, d := range defaults {
		skip[d] = struct{}{}
	}
	out := make([]string, 0, len(cfg.Exclude))
	for _, p := range cfg.Exclude {
		if _, isDefault := skip[p]; isDefault {
			continue
		}
		out = append(out, p)
	}
	return out
}

// manifestExclusions translates the walker's exclusion log into the
// manifest-native Exclusion shape; ordering is preserved (the walker
// already sorted by path then reason).
func manifestExclusions(in []repo.Exclusion) []manifest.Exclusion {
	out := make([]manifest.Exclusion, 0, len(in))
	for _, e := range in {
		out = append(out, manifest.Exclusion{Path: e.Path, Reason: string(e.Reason)})
	}
	return out
}

// langHintsOrEmpty makes sure the manifest carries [] rather than nil when
// no language tags were detected, keeping the JSON schema happy.
func langHintsOrEmpty(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

// walkerFiles extracts the subset of fields the fingerprinter consumes
// from the assembled Index. Kept in CLI so the pipeline package doesn't
// need to leak repo.FileEntry to its callers.
func walkerFiles(idx *index.Index) []repo.FileEntry {
	out := make([]repo.FileEntry, 0, len(idx.Files))
	for _, f := range idx.Files {
		out = append(out, repo.FileEntry{
			Path:   f.Path,
			Size:   f.Size,
			SHA256: f.SHA256,
			MTime:  f.MTime,
		})
	}
	return out
}

func resolveRepoRoot(flag string) (string, error) {
	if flag != "" {
		// Explicit --repo is honored verbatim per SPEC §7.1.2 — the user
		// takes responsibility for pointing at the right directory.
		abs, err := filepath.Abs(flag)
		if err != nil {
			return "", fmt.Errorf("resolve --repo: %w", err)
		}
		fi, err := os.Stat(abs)
		if err != nil {
			return "", fmt.Errorf("stat --repo: %w", err)
		}
		if !fi.IsDir() {
			return "", fmt.Errorf("--repo %q is not a directory", abs)
		}
		return abs, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("getwd: %w", err)
	}
	return repo.DiscoverRoot(cwd)
}

func readTask(args []string, inline string) (rawText, source string, isMarkdown bool, err error) {
	if inline != "" {
		return inline, "<inline>", false, nil
	}
	path := args[0]
	b, err := os.ReadFile(path) //nolint:gosec // path supplied by user
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", "", false, fmt.Errorf("task file not found: %s", path)
		}
		return "", "", false, fmt.Errorf("read task: %w", err)
	}
	if bytes.IndexByte(b[:min(len(b), 8192)], 0) >= 0 {
		return "", "", false, fmt.Errorf("task file appears to be binary: %s", path)
	}
	return string(b), path, task.IsMarkdownPath(path), nil
}

func writeOutput(out string, data []byte) error {
	if out == "" || out == "-" {
		_, err := os.Stdout.Write(append(data, '\n'))
		return err
	}
	if dir := filepath.Dir(out); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create output dir: %w", err)
		}
	}
	return os.WriteFile(out, data, 0o644) //nolint:gosec // user-selected path
}
