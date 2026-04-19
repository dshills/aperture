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
	"github.com/dshills/aperture/internal/repo"
	"github.com/dshills/aperture/internal/task"
)

type planFlags struct {
	repo              string
	inline            string
	model             string
	budget            int
	format            string
	out               string
	failOnGaps        bool
	minFeasibility    float64
	minFeasibilitySet bool // captured from cmd.Flags().Changed to allow --min-feasibility 0 to disable a config threshold
	configPath        string
	verbose           bool
	scope             string
	scopeSet          bool // captured via cmd.Flags().Changed so the sentinel "" can unset a config scope
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
			// Distinguish "flag explicitly set" from "flag left at zero default"
			// so --min-feasibility 0 can disable a config-level threshold
			// (rather than silently falling back to it).
			f.minFeasibilitySet = cmd.Flags().Changed("min-feasibility")
			f.scopeSet = cmd.Flags().Changed("scope")
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
	cmd.Flags().BoolVar(&f.verbose, "verbose", false, "Emit extra diagnostics to stderr (dampener factors, etc.)")
	cmd.Flags().StringVar(&f.scope, "scope", "", "Restrict candidate generation to this repo-relative subtree (\"\" or \".\" unsets any config scope)")
	return cmd
}

func runPlan(_ *cobra.Command, args []string, f planFlags) error {
	prep, err := preparePlan(f.repo, args, f.inline, f.configPath, scopeFlagInputs{Value: f.scope, Set: f.scopeSet})
	if err != nil {
		return err
	}

	m, err := BuildManifest(buildInputs{
		Config:      prep.Config,
		Task:        prep.Task,
		RepoRoot:    prep.RepoRoot,
		ModelFlag:   f.model,
		BudgetFlag:  f.budget,
		Fingerprint: prep.Fingerprint,
		Languages:   prep.Languages,
		Exclusions:  prep.Exclusions,
		Index:       prep.PipelineRes.Index,
		Verbose:     f.verbose,
		Scope:       prep.Scope,
	})
	// BuildManifest returns (manifest, ExitCodeError) on underflow — we
	// still emit the manifest body, then propagate the exit code.
	var ec *ExitCodeError
	underflow := false
	if err != nil && errors.As(err, &ec) && ec.Code == exitCodeBudgetUnderflow && m != nil {
		underflow = true
	} else if err != nil {
		return err
	}

	jsonBytes, err := manifest.EmitJSON(m)
	if err != nil {
		return exitErr(exitCodeBadManifest, fmt.Errorf("serialize manifest: %w", err))
	}
	if err := manifest.Validate(jsonBytes); err != nil {
		return exitErr(exitCodeBadManifest, err)
	}

	switch f.format {
	case "json":
		if err := writeOutput(f.out, jsonBytes); err != nil {
			return exitErr(exitCodeBadManifest, err)
		}
	case "markdown":
		md := manifest.EmitMarkdown(m)
		if err := writeOutput(f.out, md); err != nil {
			return exitErr(exitCodeBadManifest, err)
		}
	default:
		return usageErr(fmt.Sprintf("unknown --format %q", f.format))
	}
	if underflow {
		return exitErr(exitCodeBudgetUnderflow, fmt.Errorf("budget underflow: manifest emitted with incomplete=true"))
	}

	// §16: --fail-on-gaps → exit 8 when any blocking gap is present; or
	// when a gap whose type is in gaps.blocking config fired. The engine
	// has already applied the config upgrade, so we can check severity
	// directly.
	if f.failOnGaps || prep.Config.Thresholds.FailOnBlockingGaps {
		for _, g := range m.Gaps {
			if g.Severity == manifest.GapSeverityBlocking {
				return exitErr(exitCodeFailOnGaps, fmt.Errorf("blocking gap %s: %s", g.ID, g.Description))
			}
		}
	}

	// §16: --min-feasibility (or thresholds.min_feasibility) → exit 7
	// when the resolved score is below the threshold. A CLI flag wins
	// over config even when its value is 0 — that way a user can pass
	// --min-feasibility 0 to disable the gate for one invocation.
	var minFeas float64
	switch {
	case f.minFeasibilitySet:
		minFeas = f.minFeasibility
	case prep.Config.Thresholds.MinFeasibility > 0:
		minFeas = prep.Config.Thresholds.MinFeasibility
	}
	if minFeas > 0 && m.Feasibility.Score < minFeas {
		return exitErr(exitCodeFeasibilityBelow, fmt.Errorf("feasibility %.2f below threshold %.2f", m.Feasibility.Score, minFeas))
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
	// Verbose enables stderr diagnostic logging (SPEC §8.4). Default
	// false keeps BuildManifest byte-identical and silent.
	Verbose bool
	// Scope, when IsSet, is the v1.1 §7.4.4 projection: candidates
	// outside the scope path (except admissible supplementals) are
	// filtered out before scoring, ambiguous_ownership peer-count
	// is restricted to in-scope files, and s_import pass-2 only
	// considers in-scope target packages. Zero value means no scope.
	Scope repo.Scope
	// SuppressDemotion forces the selector into v1.1 §7.5.1
	// "Plan_B" mode: every full-eligible candidate lands at
	// LoadModeFull regardless of budget. Budget overflow is
	// tolerated and recorded on the resulting manifest via
	// selection.Result.BudgetOverflow for `aperture eval loadmode`.
	// MUST NOT be set by production code paths (forbidigo-gated).
	SuppressDemotion bool //nolint:forbidigo // eval-only, gate enforced by .golangci.yml
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
