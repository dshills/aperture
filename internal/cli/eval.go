package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/dshills/aperture/internal/config"
	"github.com/dshills/aperture/internal/eval"
	"github.com/dshills/aperture/internal/manifest"
	"github.com/dshills/aperture/internal/pipeline"
	"github.com/dshills/aperture/internal/repo"
	"github.com/dshills/aperture/internal/task"
	"github.com/dshills/aperture/internal/version"
)

// jsonMarshal pretty-prints v with 2-space indent. Isolated here so unit
// tests can cover ripgrep-report JSON serialization without importing
// encoding/json at every call site.
func jsonMarshal(v any) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}

// newEvalCommand wires `aperture eval {run,baseline,ripgrep,loadmode}`
// per v1.1 SPEC §4.1-§4.4 and §7.5.1.
func newEvalCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "eval",
		Short: "Selection-quality harness",
		Long:  "Regression-test the planner against committed fixtures (v1.1 SPEC §4.1-§4.4).",
	}
	cmd.AddCommand(
		newEvalRunCommand(),
		newEvalBaselineCommand(),
		newEvalRipgrepCommand(),
		newEvalLoadmodeCommand(),
	)
	return cmd
}

// -----------------------------------------------------------------------
// `aperture eval run`

type evalRunFlags struct {
	fixtures  string
	baseline  string
	tolerance float64
	format    string
	out       string
}

func newEvalRunCommand() *cobra.Command {
	var f evalRunFlags
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run the fixture harness and exit 2 on regression",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runEvalRun(cmd.Context(), cmd.OutOrStdout(), f)
		},
	}
	cmd.Flags().StringVar(&f.fixtures, "fixtures", "testdata/eval", "Path to fixtures directory")
	cmd.Flags().StringVar(&f.baseline, "baseline", "", "Path to baseline.json (default: <fixtures>/baseline.json)")
	cmd.Flags().Float64Var(&f.tolerance, "tolerance", 0.02, "F1 tolerance per fixture")
	cmd.Flags().StringVar(&f.format, "format", "markdown", "Output format: json or markdown")
	cmd.Flags().StringVar(&f.out, "out", "", "Write report to this path (default: stdout)")
	return cmd
}

func runEvalRun(ctx context.Context, w io.Writer, f evalRunFlags) error {
	absFixtures, err := filepath.Abs(f.fixtures)
	if err != nil {
		return exitErr(exitCodeBadArgs, fmt.Errorf("resolve --fixtures: %w", err))
	}
	baselinePath := f.baseline
	if baselinePath == "" {
		baselinePath = filepath.Join(absFixtures, "baseline.json")
	}

	fixtures, err := eval.LoadFixtures(absFixtures)
	if err != nil {
		return exitErr(exitCodeBadArgs, err)
	}

	start := time.Now()
	results := make([]eval.FixtureResult, 0, len(fixtures))
	for _, fx := range fixtures {
		if err := ctx.Err(); err != nil {
			return err
		}
		res := runOneFixture(ctx, fx)
		results = append(results, res)
	}
	elapsed := time.Since(start)

	bl, err := eval.LoadBaseline(baselinePath)
	if err != nil {
		return exitErr(exitCodeBadArgs, err)
	}
	rc := eval.CheckRegressions(&eval.RunReport{Fixtures: results}, bl, f.tolerance)

	report := eval.NewReport(absFixtures, baselinePath, f.tolerance, results, rc, bl, elapsed)
	if err := writeReport(f.out, f.format, report, w); err != nil {
		return exitErr(exitCodeBadManifest, err)
	}

	// §7.7 exit-code rules:
	//   • Orphaned baseline entry (unfiltered run) → exit 2.
	//   • Regression beyond tolerance → exit 2.
	//   • Hard-failed fixture (forbidden path / missing gap) → exit 2.
	for _, r := range results {
		if r.HardFail {
			return exitErr(exitCodeBadArgs,
				fmt.Errorf("fixture %s hard-failed: %s", r.Name, strings.Join(r.HardFailReason, "; ")))
		}
	}
	if len(rc.Orphaned) > 0 {
		return exitErr(exitCodeBadArgs,
			fmt.Errorf("baseline references %d fixture(s) absent from current run: %s",
				len(rc.Orphaned), strings.Join(rc.Orphaned, ", ")))
	}
	if len(rc.Regressed) > 0 {
		names := make([]string, 0, len(rc.Regressed))
		for _, r := range rc.Regressed {
			names = append(names, r.Name)
		}
		return exitErr(exitCodeBadArgs,
			fmt.Errorf("regressions detected in %d fixture(s): %s",
				len(rc.Regressed), strings.Join(names, ", ")))
	}
	return nil
}

// -----------------------------------------------------------------------
// `aperture eval baseline`

type evalBaselineFlags struct {
	fixtures  string
	out       string
	tolerance float64
	force     bool
}

func newEvalBaselineCommand() *cobra.Command {
	var f evalBaselineFlags
	cmd := &cobra.Command{
		Use:   "baseline",
		Short: "Regenerate baseline.json (reviewer-only)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runEvalBaseline(cmd.Context(), cmd.OutOrStdout(), f)
		},
	}
	cmd.Flags().StringVar(&f.fixtures, "fixtures", "testdata/eval", "Path to fixtures directory")
	cmd.Flags().StringVar(&f.out, "out", "", "Baseline output path (default: <fixtures>/baseline.json)")
	cmd.Flags().Float64Var(&f.tolerance, "tolerance", 0.02, "F1 tolerance when checking against an existing baseline")
	cmd.Flags().BoolVar(&f.force, "force", false, "Overwrite even when the current run regresses")
	return cmd
}

func runEvalBaseline(ctx context.Context, w io.Writer, f evalBaselineFlags) error {
	absFixtures, err := filepath.Abs(f.fixtures)
	if err != nil {
		return exitErr(exitCodeBadArgs, fmt.Errorf("resolve --fixtures: %w", err))
	}
	out := f.out
	if out == "" {
		out = filepath.Join(absFixtures, "baseline.json")
	}

	fixtures, err := eval.LoadFixtures(absFixtures)
	if err != nil {
		return exitErr(exitCodeBadArgs, err)
	}

	start := time.Now()
	results := make([]eval.FixtureResult, 0, len(fixtures))
	for _, fx := range fixtures {
		if err := ctx.Err(); err != nil {
			return err
		}
		results = append(results, runOneFixture(ctx, fx))
	}
	elapsed := time.Since(start)

	existing, err := eval.LoadBaseline(out)
	if err != nil {
		return exitErr(exitCodeBadArgs, err)
	}
	rc := eval.CheckRegressions(&eval.RunReport{Fixtures: results}, existing, f.tolerance)
	_ = elapsed // (informational; not written into the baseline file)

	// §4.2 rules:
	//   1. No baseline exists → bootstrap (exit 0).
	//   2. Baseline exists, no regressions → overwrite (exit 0).
	//   3. Baseline exists, regressions → refuse (exit 1) unless --force.
	if existing != nil && len(rc.Regressed) > 0 && !f.force {
		return exitErr(exitCodeInternal,
			fmt.Errorf("refusing to overwrite baseline: %d fixture(s) regressed; pass --force to override", len(rc.Regressed)))
	}
	bl := eval.BuildBaselineFromRun(&eval.RunReport{Fixtures: results})
	if err := eval.WriteBaseline(out, bl); err != nil {
		return exitErr(exitCodeBadManifest, err)
	}
	var msg string
	switch {
	case existing == nil:
		msg = fmt.Sprintf("baseline bootstrapped: %s (%d fixtures)\n", out, len(bl.Fixtures))
	case f.force && len(rc.Regressed) > 0:
		msg = fmt.Sprintf("baseline overwritten with --force despite %d regression(s): %s\n", len(rc.Regressed), out)
	default:
		msg = fmt.Sprintf("baseline overwritten: %s (%d fixtures)\n", out, len(bl.Fixtures))
	}
	if _, err := io.WriteString(w, msg); err != nil {
		return exitErr(exitCodeInternal, err)
	}
	return nil
}

// -----------------------------------------------------------------------
// `aperture eval ripgrep`

type evalRipgrepFlags struct {
	fixtures string
	topN     int
	format   string
	out      string
}

func newEvalRipgrepCommand() *cobra.Command {
	var f evalRipgrepFlags
	cmd := &cobra.Command{
		Use:   "ripgrep",
		Short: "Compare Aperture to the ripgrep-top-N baseline (§4.4)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runEvalRipgrep(cmd.OutOrStdout(), f)
		},
	}
	cmd.Flags().StringVar(&f.fixtures, "fixtures", "testdata/eval", "Path to fixtures directory")
	cmd.Flags().IntVar(&f.topN, "top-n", 20, "Top-N candidate limit for the ripgrep baseline")
	cmd.Flags().StringVar(&f.format, "format", "markdown", "Output format: json or markdown")
	cmd.Flags().StringVar(&f.out, "out", "", "Write report to this path (default: stdout)")
	return cmd
}

func runEvalRipgrep(w io.Writer, f evalRipgrepFlags) error {
	absFixtures, err := filepath.Abs(f.fixtures)
	if err != nil {
		return exitErr(exitCodeBadArgs, fmt.Errorf("resolve --fixtures: %w", err))
	}
	fixtures, err := eval.LoadFixtures(absFixtures)
	if err != nil {
		return exitErr(exitCodeBadArgs, err)
	}

	type row struct {
		Name      string  `json:"name"`
		Invoked   bool    `json:"invoked"`
		Precision float64 `json:"precision"`
		Recall    float64 `json:"recall"`
		F1        float64 `json:"f1"`
	}
	rows := make([]row, 0, len(fixtures))

	ctx := context.Background()
	for _, fx := range fixtures {
		if err := eval.VerifyRepoFingerprint(filepath.Join(fx.Dir, "repo"), fx.RepoFingerprint); err != nil {
			return exitErr(exitCodeBadArgs, err)
		}
		rawTask, source, isMarkdown, err := eval.ResolveTaskText(fx)
		if err != nil {
			return exitErr(exitCodeBadTask, err)
		}
		parsed := task.Parse(rawTask, task.ParseOptions{Source: source, IsMarkdown: isMarkdown})

		b, err := eval.RipgrepFixture(ctx, fx, parsed.Anchors, eval.RipgrepOptions{
			RepoRoot: filepath.Join(fx.Dir, "repo"),
			TopN:     f.topN,
			Excludes: config.DefaultExclusions(),
		})
		if err != nil {
			if errors.Is(err, eval.ErrRipgrepMissing) {
				return exitErr(exitCodeInternal, err)
			}
			return exitErr(exitCodeInternal, fmt.Errorf("rg (%s): %w", fx.Name, err))
		}
		m := eval.ScoreRipgrepBaseline(fx, b.Files)
		rows = append(rows, row{
			Name: fx.Name, Invoked: b.Invoked,
			Precision: m.Precision, Recall: m.Recall, F1: m.F1,
		})
	}

	// Emit a tiny report. We keep this separate from RunReport because
	// the comparator has different semantics (no forbidden/gap rules).
	switch f.format {
	case "json":
		return writeOutputFromCLI(w, f.out, mustMarshal(map[string]any{
			"schema_version":          "1.0",
			"aperture_version":        version.Version,
			"selection_logic_version": manifest.SelectionLogicVersion,
			"top_n":                   f.topN,
			"fixtures":                rows,
		}))
	case "markdown":
		var sb strings.Builder
		fmt.Fprintln(&sb, "# Aperture ripgrep-baseline Report")
		fmt.Fprintln(&sb)
		fmt.Fprintf(&sb, "- Top-N: %d\n", f.topN)
		fmt.Fprintln(&sb)
		fmt.Fprintln(&sb, "| Fixture | Invoked | Precision | Recall | F1 |")
		fmt.Fprintln(&sb, "|---------|---------|-----------|--------|----|")
		for _, r := range rows {
			inv := "no"
			if r.Invoked {
				inv = "yes"
			}
			fmt.Fprintf(&sb, "| %s | %s | %.4f | %.4f | %.4f |\n",
				r.Name, inv, r.Precision, r.Recall, r.F1)
		}
		return writeOutputFromCLI(w, f.out, []byte(sb.String()))
	default:
		return usageErr(fmt.Sprintf("unknown --format %q", f.format))
	}
}

// -----------------------------------------------------------------------
// Shared helpers.

// runOneFixture executes the full planner pipeline for a single fixture
// in-process and returns its scoring result. On any non-planner error,
// result.Error is set and Metrics are zeroed.
func runOneFixture(ctx context.Context, fx eval.Fixture) eval.FixtureResult {
	res := eval.FixtureResult{Name: fx.Name}
	repoDir := filepath.Join(fx.Dir, "repo")

	if err := eval.VerifyRepoFingerprint(repoDir, fx.RepoFingerprint); err != nil {
		res.Error = err.Error()
		return res
	}

	rawTask, source, isMarkdown, err := eval.ResolveTaskText(fx)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	parsed := task.Parse(rawTask, task.ParseOptions{Source: source, IsMarkdown: isMarkdown})

	cfg := config.Default()
	if fx.Budget > 0 {
		cfg.Defaults.Budget = fx.Budget
	}
	if fx.Model != "" {
		cfg.Defaults.Model = fx.Model
	}

	// Intentionally no cache: writing under repoDir would pollute the
	// fixture tree and break the fixture fingerprint. Fixture runs are
	// deterministic one-shots — the cache miss cost is acceptable.
	pipeRes, err := pipeline.Build(ctx, pipeline.BuildOptions{
		Root:              repoDir,
		DefaultExcludes:   config.DefaultExclusions(),
		UserExcludes:      userOnly(cfg),
		TypeScriptEnabled: cfg.Languages.TypeScript.Enabled,
		JavaScriptEnabled: cfg.Languages.JavaScript.Enabled,
		PythonEnabled:     cfg.Languages.Python.Enabled,
	})
	if err != nil {
		res.Error = fmt.Sprintf("pipeline: %s", err.Error())
		return res
	}
	fp, err := repo.Fingerprint(walkerFiles(pipeRes.Index), version.Version)
	if err != nil {
		res.Error = fmt.Sprintf("fingerprint: %s", err.Error())
		return res
	}

	m, err := BuildManifest(buildInputs{
		Config:      cfg,
		Task:        parsed,
		RepoRoot:    repoDir,
		ModelFlag:   fx.Model,
		BudgetFlag:  fx.Budget,
		Fingerprint: fp,
		Languages:   pipeRes.Index.LanguageHints(),
		Exclusions:  pipeRes.Exclusions,
		Index:       pipeRes.Index,
	})
	// Underflow is a legitimate fixture outcome — the harness records
	// metrics against whatever manifest was produced.
	var ec *ExitCodeError
	if err != nil && errors.As(err, &ec) && ec.Code == exitCodeBudgetUnderflow && m != nil {
		// continue with m
	} else if err != nil {
		res.Error = err.Error()
		return res
	}

	verdict := eval.Score(fx, m)
	res.Metrics = verdict.Metrics
	res.HardFail = verdict.HardFail
	res.HardFailReason = verdict.HardFailReason
	res.ManifestHash = m.ManifestHash
	return res
}

func writeReport(outPath, format string, report *eval.RunReport, stdout io.Writer) error {
	var buf []byte
	var err error
	switch format {
	case "json":
		buf, err = eval.EmitJSON(report)
	case "markdown":
		buf = eval.EmitMarkdown(report)
	default:
		return fmt.Errorf("unknown --format %q", format)
	}
	if err != nil {
		return err
	}
	return writeOutputFromCLI(stdout, outPath, buf)
}

// writeOutputFromCLI mirrors writeOutput in plan.go but takes an explicit
// io.Writer for stdout, which keeps the eval tests happy when they
// capture output via cobra.SetOut.
func writeOutputFromCLI(stdout io.Writer, out string, data []byte) error {
	if out == "" || out == "-" {
		_, err := stdout.Write(append(data, '\n'))
		return err
	}
	if dir := filepath.Dir(out); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create output dir: %w", err)
		}
	}
	return os.WriteFile(out, data, 0o644) //nolint:gosec // user-selected path
}

// mustMarshal serializes v to pretty-printed JSON with a trailing newline.
// Panics on marshal error (only possible with a programmer bug in the
// type definitions above).
func mustMarshal(v any) []byte {
	b, err := jsonMarshal(v)
	if err != nil {
		panic(err)
	}
	return append(b, '\n')
}
