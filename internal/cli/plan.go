package cli

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/spf13/cobra"

	"github.com/dshills/aperture/internal/config"
	"github.com/dshills/aperture/internal/manifest"
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

	m, err := buildStubManifest(buildInputs{
		Config:     cfg,
		Task:       parsed,
		RepoRoot:   repoRoot,
		ModelFlag:  f.model,
		BudgetFlag: f.budget,
	})
	if err != nil {
		return exitErr(1, err)
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
	return nil
}

type buildInputs struct {
	Config     config.Config
	Task       task.Task
	RepoRoot   string
	ModelFlag  string
	BudgetFlag int
}

// buildStubManifest produces a Phase-1 manifest: populated task + deterministic
// structural fields, empty arrays everywhere else. Later phases replace these
// stubs with real scan/score/select output.
func buildStubManifest(in buildInputs) (*manifest.Manifest, error) {
	now := time.Now().UTC().Format(time.RFC3339)

	estimator, estimatorVersion := resolveEstimator(in.ModelFlag, in.Config.Defaults.Model)
	tokenCeiling := in.BudgetFlag
	if tokenCeiling == 0 {
		tokenCeiling = in.Config.Defaults.Budget
	}
	effective := tokenCeiling
	r := in.Config.Defaults.Reserve
	reserveTotal := r.Instructions + r.Reasoning + r.ToolOutput + r.Expansion
	if effective > 0 {
		effective -= reserveTotal
		if effective < 0 {
			effective = 0
		}
	}

	resolvedModel := in.ModelFlag
	if resolvedModel == "" {
		resolvedModel = in.Config.Defaults.Model
	}

	anchors := append([]string{}, in.Task.Anchors...)
	sort.Strings(anchors)

	digest, err := in.Config.Digest()
	if err != nil {
		return nil, fmt.Errorf("config digest: %w", err)
	}

	host, hostErr := os.Hostname()
	if hostErr != nil || host == "" {
		host = "unknown"
	}
	m := &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		GeneratedAt:   now,
		Incomplete:    false,
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
			Fingerprint:   "",
			LanguageHints: []string{},
		},
		Budget: manifest.Budget{
			Model:        resolvedModel,
			TokenCeiling: tokenCeiling,
			Reserved: manifest.Reserved{
				Instructions: r.Instructions,
				Reasoning:    r.Reasoning,
				ToolOutput:   r.ToolOutput,
				Expansion:    r.Expansion,
			},
			EffectiveContextBudget:  effective,
			EstimatedSelectedTokens: 0,
			Estimator:               estimator,
			EstimatorVersion:        estimatorVersion,
		},
		Selections: []manifest.Selection{},
		Reachable:  []manifest.Reachable{},
		Exclusions: []manifest.Exclusion{},
		Gaps:       []manifest.Gap{},
		Feasibility: manifest.Feasibility{
			Score:              0.0,
			Assessment:         "pending: selection pipeline stubbed for Phase 1",
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
	if err := manifest.ApplyHash(m); err != nil {
		return nil, err
	}
	return m, nil
}

// resolveEstimator returns the v1 estimator identity for the given model. In
// Phase 1 only the heuristic branch is wired; tiktoken integration lands in
// Phase 3. "Recognized but unsupported" models therefore still map to the
// heuristic here and will begin returning exit 10 once the real tokenizer is
// introduced.
func resolveEstimator(cliModel, cfgModel string) (string, string) {
	_ = cliModel
	_ = cfgModel
	return "heuristic-3.5", "v1"
}

func resolveRepoRoot(flag string) (string, error) {
	if flag == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("getwd: %w", err)
		}
		return cwd, nil
	}
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
