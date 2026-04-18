package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dshills/aperture/internal/feasibility"
	"github.com/dshills/aperture/internal/manifest"
)

type explainFlags struct {
	manifest   string
	repo       string
	inline     string
	model      string
	budget     int
	configPath string
}

func newExplainCommand() *cobra.Command {
	var f explainFlags
	cmd := &cobra.Command{
		Use:   "explain [TASK_FILE]",
		Short: "Render selection reasoning for a prior manifest or a fresh planning run",
		Long: `aperture explain has two modes:

  * --manifest <path>: load a previously-written manifest JSON and render
    its selections, gaps, feasibility sub-signals, and budget spent.

  * Without --manifest: accept the same inputs as 'aperture plan' (repo,
    TASK_FILE, --model, --budget) and run the pipeline, printing the
    rationale instead of emitting a machine-readable manifest.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runExplain(cmd.OutOrStdout(), args, f)
		},
	}
	cmd.Flags().StringVar(&f.manifest, "manifest", "", "Path to a prior manifest JSON")
	cmd.Flags().StringVar(&f.repo, "repo", "", "Repository root (defaults to cwd)")
	cmd.Flags().StringVarP(&f.inline, "prompt", "p", "", "Inline task text")
	cmd.Flags().StringVar(&f.model, "model", "", "Target model id")
	cmd.Flags().IntVar(&f.budget, "budget", 0, "Total token budget override")
	cmd.Flags().StringVar(&f.configPath, "config", "", "Path to .aperture.yaml (default: <repo>/.aperture.yaml)")
	return cmd
}

// runExplain is intentionally a read-only lens: it NEVER propagates the
// underflow/threshold exit codes that `aperture plan` enforces (7/8/9).
// The user has asked to SEE the reasoning, including for an incomplete
// manifest — aborting with exit 9 just because a prior plan underflowed
// would defeat the purpose. Both code paths (loaded via --manifest and
// rebuilt via explainViaPipeline) therefore unwrap the manifest and
// render it; explainViaPipeline already strips exit 9 from BuildManifest.
func runExplain(w io.Writer, args []string, f explainFlags) error {
	var m *manifest.Manifest
	if f.manifest != "" {
		loaded, err := loadManifest(f.manifest)
		if err != nil {
			return exitErr(exitCodeBadManifest, err)
		}
		m = loaded
	} else {
		built, err := explainViaPipeline(args, f)
		if err != nil {
			return err
		}
		m = built
	}
	return renderExplain(w, m)
}

func loadManifest(path string) (*manifest.Manifest, error) {
	b, err := os.ReadFile(path) //nolint:gosec // user-supplied path
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	var m manifest.Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	return &m, nil
}

// explainViaPipeline runs the same planning pipeline as 'aperture plan'
// but returns the manifest in memory without writing to disk. Threshold
// gates are deliberately skipped — explain is a read-only lens. The
// pipeline preparation itself is shared with runPlan via preparePlan.
func explainViaPipeline(args []string, f explainFlags) (*manifest.Manifest, error) {
	prep, err := preparePlan(f.repo, args, f.inline, f.configPath)
	if err != nil {
		return nil, err
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
	})
	var ec *ExitCodeError
	if err != nil && errors.As(err, &ec) && ec.Code == exitCodeBudgetUnderflow && m != nil {
		// Underflow — still render the manifest, don't propagate exit 9.
		return m, nil
	}
	return m, err
}

// errWriter wraps an io.Writer so every Fprintf/Fprintln can be called
// without checking the error return individually. The first error sticks
// and is returned at the end of the rendering pass.
type errWriter struct {
	w   io.Writer
	err error
}

func (e *errWriter) printf(f string, a ...any) {
	if e.err != nil {
		return
	}
	_, e.err = fmt.Fprintf(e.w, f, a...)
}

func (e *errWriter) println(s string) {
	if e.err != nil {
		return
	}
	_, e.err = fmt.Fprintln(e.w, s)
}

// renderExplain writes a plain-text (no ANSI color, §8.4.1) explanation.
// Golden-test friendly: every field that varies across runs (generated_at,
// host, pid, manifest_hash digits) is elided from the output header.
func renderExplain(w io.Writer, m *manifest.Manifest) error {
	if m == nil {
		return fmt.Errorf("explain: nil manifest")
	}
	ew := &errWriter{w: w}

	ew.printf("Task:            %s\n", m.Task.TaskID)
	ew.printf("  type:          %s\n", m.Task.Type)
	ew.printf("  objective:     %s\n", m.Task.Objective)
	ew.printf("  anchors (%d):  %s\n", len(m.Task.Anchors), strings.Join(m.Task.Anchors, ", "))
	ew.println("")

	ew.println("Budget:")
	ew.printf("  model:                    %s\n", m.Budget.Model)
	ew.printf("  estimator:                %s (%s)\n", m.Budget.Estimator, m.Budget.EstimatorVersion)
	ew.printf("  token_ceiling:            %d\n", m.Budget.TokenCeiling)
	ew.printf("  effective_context_budget: %d\n", m.Budget.EffectiveContextBudget)
	ew.printf("  estimated_selected:       %d\n", m.Budget.EstimatedSelectedTokens)
	ew.println("")

	ew.println("Selections:")
	if len(m.Selections) == 0 {
		ew.println("  (none)")
	}
	selections := append([]manifest.Selection{}, m.Selections...)
	sort.Slice(selections, func(i, j int) bool { return selections[i].Path < selections[j].Path })
	for _, s := range selections {
		ew.printf("  %-40s  score=%.4f  load=%-20s  tokens=%d\n",
			s.Path, s.RelevanceScore, s.LoadMode, s.EstimatedTokens)
		if s.DemotionReason != nil {
			ew.printf("    demoted: %s\n", *s.DemotionReason)
		}
		for _, b := range s.ScoreBreakdown {
			ew.printf("    %-10s signal=%.2f weight=%.2f contribution=%.4f\n",
				b.Factor, b.Signal, b.Weight, b.Contribution)
		}
		if len(s.Rationale) > 0 {
			ew.printf("    rationale: %s\n", strings.Join(s.Rationale, "; "))
		}
	}
	ew.println("")

	ew.println("Reachable:")
	if len(m.Reachable) == 0 {
		ew.println("  (none)")
	}
	// Sort explicitly so the rendered output is deterministic even when
	// the loaded manifest happens to carry reachable entries in some
	// other order (e.g., older manifest JSON pre-dating the §14 path-
	// order contract).
	reachable := append([]manifest.Reachable{}, m.Reachable...)
	sort.Slice(reachable, func(i, j int) bool { return reachable[i].Path < reachable[j].Path })
	for _, r := range reachable {
		ew.printf("  %-40s  score=%.4f  reason=%s\n", r.Path, r.RelevanceScore, r.Reason)
	}
	ew.println("")

	ew.println("Gaps:")
	if len(m.Gaps) == 0 {
		ew.println("  (none)")
	}
	for _, g := range m.Gaps {
		ew.printf("  [%s] %s (%s): %s\n", g.ID, g.Type, g.Severity, g.Description)
		for _, e := range g.Evidence {
			ew.printf("    evidence: %s\n", e)
		}
		for _, r := range g.SuggestedRemediation {
			ew.printf("    remediation: %s\n", r)
		}
	}
	ew.println("")

	ew.println("Feasibility:")
	ew.printf("  score:       %.4f  (%s)\n", m.Feasibility.Score, m.Feasibility.Assessment)
	// SubSignalKeys is the authoritative render order owned by the
	// feasibility package, so adding a new sub-signal there will surface
	// here automatically.
	for _, k := range feasibility.SubSignalKeys {
		if v, ok := m.Feasibility.SubSignals[k]; ok {
			ew.printf("  %-18s %.4f\n", k+":", v)
		}
	}
	if len(m.Feasibility.Positives) > 0 {
		ew.printf("  positives:   %s\n", strings.Join(m.Feasibility.Positives, "; "))
	}
	if len(m.Feasibility.Negatives) > 0 {
		ew.printf("  negatives:   %s\n", strings.Join(m.Feasibility.Negatives, "; "))
	}
	for _, b := range m.Feasibility.BlockingConditions {
		ew.printf("  BLOCKING: %s\n", b)
	}
	return ew.err
}
