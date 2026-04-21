package cli

import (
	"context"
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

type evalLoadmodeFlags struct {
	fixtures string
	format   string
	out      string
}

// newEvalLoadmodeCommand wires `aperture eval loadmode` per v1.1
// SPEC §4.3 / §7.5.1. For each fixture it runs the planner twice
// — normal (Plan_A) and with SuppressDemotion=true (Plan_B) —
// computes the symbolic diff, invokes agent_check when declared,
// and emits a combined report.
func newEvalLoadmodeCommand() *cobra.Command {
	var f evalLoadmodeFlags
	cmd := &cobra.Command{
		Use:   "loadmode",
		Short: "Calibrate the behavioral_summary vs. full load-mode boundary",
		Long: `aperture eval loadmode runs every fixture twice — once with the
normal §7.5.0 demotion pass and once with SuppressDemotion=true
("Plan_B: forced-full") — and emits the §7.5.1 symbolic diff plus,
when the fixture declares agent_check, the IMPROVEMENT /
REGRESSION / NO_CHANGE_{PASS,FAIL} classification. The §7.5.2
advisory is aggregated across all declared agent_check records.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runEvalLoadmode(cmd.Context(), cmd.OutOrStdout(), f)
		},
	}
	cmd.Flags().StringVar(&f.fixtures, "fixtures", "testdata/eval", "Path to fixtures directory")
	cmd.Flags().StringVar(&f.format, "format", "markdown", "Output format: json or markdown")
	cmd.Flags().StringVar(&f.out, "out", "", "Write report to this path (default: stdout)")
	return cmd
}

func runEvalLoadmode(ctx context.Context, stdout io.Writer, f evalLoadmodeFlags) error {
	if ctx == nil {
		ctx = context.Background()
	}
	absFixtures, err := filepath.Abs(f.fixtures)
	if err != nil {
		return exitErr(exitCodeBadArgs, fmt.Errorf("resolve --fixtures: %w", err))
	}
	fixtures, err := eval.LoadFixtures(absFixtures)
	if err != nil {
		return exitErr(exitCodeBadArgs, err)
	}

	start := time.Now()
	rows := make([]eval.LoadmodeFixtureResult, 0, len(fixtures))
	for _, fx := range fixtures {
		// Honor Ctrl+C / upstream cancellation — without this
		// check an in-flight agent_check subprocess would run to
		// its timeout even after the user interrupted the CLI.
		if err := ctx.Err(); err != nil {
			return exitErr(exitCodeInternal, err)
		}
		row, abort := runOneLoadmodeFixture(ctx, fx)
		if abort != nil {
			return abort
		}
		rows = append(rows, row)
	}
	elapsed := time.Since(start)

	report := eval.NewLoadmodeReport(absFixtures, rows, elapsed)

	var buf []byte
	switch f.format {
	case "json":
		buf, err = eval.EmitLoadmodeJSON(report)
		if err != nil {
			return exitErr(exitCodeBadManifest, err)
		}
	case "markdown":
		buf = eval.EmitLoadmodeMarkdown(report)
	default:
		return usageErr(fmt.Sprintf("unknown --format %q", f.format))
	}
	return writeLoadmodeOutput(stdout, f.out, buf)
}

// runOneLoadmodeFixture runs a single fixture under both planning
// variants and classifies the result. The second return is non-nil
// only for §7.7 "abort the whole eval run" conditions (currently:
// agent_check command_not_found).
func runOneLoadmodeFixture(ctx context.Context, fx eval.Fixture) (eval.LoadmodeFixtureResult, error) {
	row := eval.LoadmodeFixtureResult{Name: fx.Name}
	repoDir := filepath.Join(fx.Dir, "repo")

	if err := eval.VerifyRepoFingerprint(repoDir, fx.RepoFingerprint); err != nil {
		row.Error = err.Error()
		return row, nil
	}
	rawTask, source, isMarkdown, err := eval.ResolveTaskText(fx)
	if err != nil {
		row.Error = err.Error()
		return row, nil
	}
	parsed := task.Parse(rawTask, task.ParseOptions{Source: source, IsMarkdown: isMarkdown})

	cfg := config.Default()
	if fx.Budget > 0 {
		cfg.Defaults.Budget = fx.Budget
	}
	if fx.Model != "" {
		cfg.Defaults.Model = fx.Model
	}

	pipeRes, err := pipeline.Build(ctx, pipeline.BuildOptions{
		Root:              repoDir,
		DefaultExcludes:   config.DefaultExclusions(),
		UserExcludes:      userOnly(cfg),
		TypeScriptEnabled: cfg.Languages.TypeScript.Enabled,
		JavaScriptEnabled: cfg.Languages.JavaScript.Enabled,
		PythonEnabled:     cfg.Languages.Python.Enabled,
	})
	if err != nil {
		row.Error = fmt.Sprintf("pipeline: %s", err.Error())
		return row, nil
	}
	fp, err := repo.Fingerprint(walkerFiles(pipeRes.Index), version.Version)
	if err != nil {
		row.Error = fmt.Sprintf("fingerprint: %s", err.Error())
		return row, nil
	}

	buildCommon := buildInputs{
		Config:      cfg,
		Task:        parsed,
		RepoRoot:    repoDir,
		ModelFlag:   fx.Model,
		BudgetFlag:  fx.Budget,
		Fingerprint: fp,
		Languages:   pipeRes.Index.LanguageHints(),
		Exclusions:  pipeRes.Exclusions,
		Index:       pipeRes.Index,
	}

	// Plan A — normal selection.
	mA, planA, err := runPlanVariant(buildCommon, false)
	if err != nil {
		row.Error = fmt.Sprintf("plan_a: %s", err.Error())
		return row, nil
	}
	// Plan B — forced-full via the §7.5.1 SuppressDemotion flag.
	// The forbidigo lint rule allows this call (`eval_loadmode.go`
	// is under internal/cli but is the eval-harness entry point).
	mB, planB, err := runPlanVariant(buildCommon, true) //nolint:forbidigo // eval loadmode is the §7.5.1 authorized caller
	if err != nil {
		row.Error = fmt.Sprintf("plan_b: %s", err.Error())
		return row, nil
	}

	row.PlanAManifestHash = mA.ManifestHash
	row.PlanBManifestHash = mB.ManifestHash
	row.Symbolic = eval.ComputeSymbolicDiff(mA, mB, planB.BudgetOverflow)
	_ = planA // retained for possible future diagnostics; no field of planA used here

	if fx.AgentCheckCommand == "" {
		return row, nil
	}
	row.DeclaresAgentCheck = true

	// Materialize manifests and a merged prompt path on disk so
	// the agent_check script receives real files through the
	// §7.1.1 env contract.
	artifactsDir, err := os.MkdirTemp("", "aperture-loadmode-*")
	if err != nil {
		row.Error = fmt.Sprintf("artifacts: %s", err.Error())
		return row, nil
	}
	defer func() { _ = os.RemoveAll(artifactsDir) }()

	envA, err := writeLoadmodeArtifacts(artifactsDir, "planA", mA, repoDir, source)
	if err != nil {
		row.Error = fmt.Sprintf("artifacts A: %s", err.Error())
		return row, nil
	}
	envB, err := writeLoadmodeArtifacts(artifactsDir, "planB", mB, repoDir, source)
	if err != nil {
		row.Error = fmt.Sprintf("artifacts B: %s", err.Error())
		return row, nil
	}

	cmdPath, err := eval.ResolveAgentCheckCommand(repoDir, fx.AgentCheckCommand)
	if err != nil {
		row.Error = err.Error()
		return row, nil
	}

	resA := eval.RunAgentCheck(ctx, cmdPath, fx.AgentCheckTimeout, envA)
	if resA.Outcome == eval.AgentCheckNotFound {
		return row, exitErr(exitCodeInternal,
			fmt.Errorf("agent_check command not found: %s (fixture %s)", cmdPath, fx.Name))
	}
	if resA.Outcome == eval.AgentCheckCanceled {
		return row, exitErr(exitCodeInternal, resA.Err)
	}
	resB := eval.RunAgentCheck(ctx, cmdPath, fx.AgentCheckTimeout, envB)
	if resB.Outcome == eval.AgentCheckNotFound {
		return row, exitErr(exitCodeInternal,
			fmt.Errorf("agent_check command not found: %s (fixture %s)", cmdPath, fx.Name))
	}
	if resB.Outcome == eval.AgentCheckCanceled {
		return row, exitErr(exitCodeInternal, resB.Err)
	}

	row.AgentCheckPlanA = eval.AgentCheckSummary{
		Outcome:    resA.Outcome,
		ExitCode:   resA.ExitCode,
		DurationMS: resA.DurationMS,
	}
	row.AgentCheckPlanB = eval.AgentCheckSummary{
		Outcome:    resB.Outcome,
		ExitCode:   resB.ExitCode,
		DurationMS: resB.DurationMS,
	}
	row.Delta = eval.ClassifyAgentCheckDelta(resA.Outcome, resB.Outcome)
	return row, nil
}

// runPlanVariant runs BuildManifest once with the given
// SuppressDemotion flag. Returns the manifest, the selection
// result (for BudgetOverflow), and any fatal error. Underflow is
// treated as a legitimate fixture outcome (not an error) per the
// eval harness contract.
func runPlanVariant(in buildInputs, suppress bool) (*manifest.Manifest, *planExtras, error) {
	local := in
	local.SuppressDemotion = suppress //nolint:forbidigo // eval-loadmode-only; gate is the static package-boundary check
	m, err := BuildManifest(local)
	var ec *ExitCodeError
	switch {
	case err != nil && errors.As(err, &ec) && ec.Code == exitCodeBudgetUnderflow && m != nil:
		// Underflow is OK — the manifest is still populated and
		// the caller wants the (partial) plan for diff-ing.
	case err != nil:
		return nil, nil, err
	}
	// BuildManifest currently does NOT surface selection.Result
	// to its callers; we rebuild the overflow count by inspecting
	// the manifest's selections vs. effective budget.
	extras := &planExtras{
		BudgetOverflow: computeOverflowFromManifest(m),
	}
	return m, extras, nil
}

// planExtras carries information the eval harness needs from
// BuildManifest that isn't currently surfaced on the manifest.
// For Phase 6 the only field is budget overflow — computed as
// max(0, estimated_selected_tokens - effective_context_budget).
type planExtras struct {
	BudgetOverflow int
}

// computeOverflowFromManifest is the overflow approximation derived
// from the public manifest shape. In practice it equals
// selection.Result.BudgetOverflow because estimated_selected_tokens
// IS selResult.SpentTokens and effective_context_budget IS the
// selector's input budget (see assembleManifest).
func computeOverflowFromManifest(m *manifest.Manifest) int {
	if m == nil {
		return 0
	}
	over := m.Budget.EstimatedSelectedTokens - m.Budget.EffectiveContextBudget
	if over < 0 {
		return 0
	}
	return over
}

// writeLoadmodeArtifacts materializes Plan_A / Plan_B manifests
// plus a merged-prompt placeholder under artifactsDir. The
// returned AgentCheckEnv is ready to pass into eval.RunAgentCheck.
func writeLoadmodeArtifacts(artifactsDir, prefix string, m *manifest.Manifest, repoDir, taskSource string) (eval.AgentCheckEnv, error) {
	mJSON, err := manifest.EmitJSON(m)
	if err != nil {
		return eval.AgentCheckEnv{}, fmt.Errorf("emit json: %w", err)
	}
	mMD := manifest.EmitMarkdown(m)

	jsonPath := filepath.Join(artifactsDir, prefix+".json")
	mdPath := filepath.Join(artifactsDir, prefix+".md")
	promptPath := filepath.Join(artifactsDir, prefix+".prompt")
	taskPath := filepath.Join(artifactsDir, prefix+".task")
	if err := os.WriteFile(jsonPath, mJSON, 0o600); err != nil {
		return eval.AgentCheckEnv{}, err
	}
	if err := os.WriteFile(mdPath, mMD, 0o600); err != nil {
		return eval.AgentCheckEnv{}, err
	}
	// Prompt placeholder: in the real `aperture run` adapter flow
	// this is the merged task+manifest prompt. For loadmode we
	// reuse the manifest markdown (the adapter protocol is still
	// fluid and we only need the env contract to be valid).
	if err := os.WriteFile(promptPath, mMD, 0o600); err != nil {
		return eval.AgentCheckEnv{}, err
	}
	if err := os.WriteFile(taskPath, []byte(m.Task.RawText), 0o600); err != nil {
		return eval.AgentCheckEnv{}, err
	}
	_ = taskSource
	return eval.AgentCheckEnv{
		ManifestPath:         jsonPath,
		ManifestMarkdownPath: mdPath,
		PromptPath:           promptPath,
		TaskPath:             taskPath,
		RepoRoot:             repoDir,
		ManifestHash:         m.ManifestHash,
		ApertureVersion:      version.Version,
	}, nil
}

func writeLoadmodeOutput(stdout io.Writer, out string, data []byte) error {
	if out == "" || out == "-" {
		_, err := stdout.Write(data)
		if err != nil {
			return exitErr(exitCodeInternal, err)
		}
		return nil
	}
	if dir := filepath.Dir(out); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return exitErr(exitCodeInternal, err)
		}
	}
	if err := os.WriteFile(out, data, 0o644); err != nil { //nolint:gosec // user path
		return exitErr(exitCodeInternal, err)
	}
	return nil
}

// ensure imports aren't stripped when the file lacks a direct
// reference (future expansions).
var _ = strings.TrimSpace
