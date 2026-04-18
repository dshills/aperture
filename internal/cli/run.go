package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dshills/aperture/internal/agent"
	"github.com/dshills/aperture/internal/manifest"
	"github.com/dshills/aperture/internal/task"
	"github.com/dshills/aperture/internal/version"
)

type runFlags struct {
	repo              string
	inline            string
	model             string
	budget            int
	configPath        string
	failOnGaps        bool
	minFeasibility    float64
	minFeasibilitySet bool
	outDir            string
}

func newRunCommand() *cobra.Command {
	var f runFlags
	cmd := &cobra.Command{
		Use:   "run <agent> [TASK_FILE]",
		Short: "Plan and invoke a downstream coding-agent adapter",
		Long: `aperture run <agent> [TASK_FILE] plans the task, validates thresholds,
persists the manifest and merged prompt under .aperture/manifests/
(overridable via output.directory or --out-dir), and invokes the named
agent. Exit codes follow SPEC §16:

  7  feasibility below --min-feasibility / thresholds.min_feasibility
  8  blocking gap present under --fail-on-gaps / gaps.blocking
  9  budget underflow
  11 unknown <agent>
  12 adapter failed to start (exec not found, permission denied)
  *  any other value: adapter's own exit code, propagated verbatim`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			f.minFeasibilitySet = cmd.Flags().Changed("min-feasibility")
			return runRun(cmd.Context(), args, f)
		},
	}
	cmd.Flags().StringVar(&f.repo, "repo", "", "Repository root (defaults to cwd)")
	cmd.Flags().StringVarP(&f.inline, "prompt", "p", "", "Inline task text")
	cmd.Flags().StringVar(&f.model, "model", "", "Target model id")
	cmd.Flags().IntVar(&f.budget, "budget", 0, "Total token budget override")
	cmd.Flags().StringVar(&f.configPath, "config", "", "Path to .aperture.yaml (default: <repo>/.aperture.yaml)")
	cmd.Flags().BoolVar(&f.failOnGaps, "fail-on-gaps", false, "Exit 8 if any blocking gap is present")
	cmd.Flags().Float64Var(&f.minFeasibility, "min-feasibility", 0.0, "Exit 7 if feasibility < threshold")
	cmd.Flags().StringVar(&f.outDir, "out-dir", "", "Directory for persisted manifests (default: <repo>/.aperture/manifests)")
	return cmd
}

func runRun(ctx context.Context, args []string, f runFlags) error {
	// Orphan sweep first — bounded-best-effort cleanup of leftover
	// inline-task tempfiles from prior crashes (§7.10.4.1). Never fails.
	agent.SweepOrphanTempfiles()

	if ctx == nil {
		ctx = context.Background()
	}
	agentName := args[0]
	taskArgs := args[1:]
	if len(taskArgs) == 0 && f.inline == "" {
		return usageErr("either TASK_FILE or -p/--prompt is required")
	}
	if len(taskArgs) > 0 && f.inline != "" {
		return usageErr("TASK_FILE and -p/--prompt are mutually exclusive")
	}

	prep, err := preparePlan(f.repo, taskArgs, f.inline, f.configPath)
	if err != nil {
		return err
	}

	// Resolve the adapter BEFORE we do any planning work so a typo in
	// the agent name fails fast (§16 exit 11).
	adapter, agentCfg, ok := agent.Resolve(agentName, prep.Config.Agents)
	if !ok {
		return exitErr(exitCodeUnknownAgent, fmt.Errorf("unknown agent %q; check .aperture.yaml agents.%s", agentName, agentName))
	}

	m, buildErr := BuildManifest(buildInputs{
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
	underflow := false
	switch {
	case buildErr == nil:
	case errors.As(buildErr, &ec) && ec.Code == exitCodeBudgetUnderflow && m != nil:
		underflow = true
	default:
		return buildErr
	}

	// Persist the manifest (JSON + Markdown) + merged prompt BEFORE any
	// threshold check, so the run is auditable even when the threshold
	// fires before the adapter is invoked.
	paths, err := writeRunArtifacts(prep.RepoRoot, prep.Config.Output.Directory, f.outDir, m, prep.Task.RawText)
	if err != nil {
		return exitErr(exitCodeBadManifest, err)
	}

	// §7.6.5 underflow is a terminal condition: never invoke the adapter.
	if underflow {
		return exitErr(exitCodeBudgetUnderflow, fmt.Errorf("budget underflow: manifest emitted at %s with incomplete=true", paths.JSON))
	}

	// Threshold gates run AFTER persistence and BEFORE adapter dispatch.
	if f.failOnGaps || prep.Config.Thresholds.FailOnBlockingGaps {
		for _, g := range m.Gaps {
			if g.Severity == manifest.GapSeverityBlocking {
				return exitErr(exitCodeFailOnGaps, fmt.Errorf("blocking gap %s: %s", g.ID, g.Description))
			}
		}
	}
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

	// Resolve the task path that the adapter should see. Inline tasks
	// land in a tempfile the CLI is responsible for deleting; file
	// tasks keep their original path untouched.
	taskPath, taskCleanup, err := resolveAdapterTaskPath(m.ManifestID, prep.Task)
	if err != nil {
		return exitErr(exitCodeBadTask, err)
	}
	if taskCleanup != nil {
		// Register cleanup on SIGINT/SIGTERM/SIGHUP in addition to the
		// explicit defer below so hard interrupts still leave $TMPDIR
		// clean. On Windows the register is a no-op — the 24 h orphan
		// sweep runs next startup.
		detach := agent.RegisterCleanupOnSignal(taskCleanup)
		defer detach()
		defer taskCleanup()
	}

	req := agent.RunRequest{
		ManifestJSONPath:     paths.JSON,
		ManifestMarkdownPath: paths.Markdown,
		TaskPath:             taskPath,
		PromptPath:           paths.Prompt,
		RepoRoot:             prep.RepoRoot,
		ManifestHash:         stripSha256Prefix(m.ManifestHash),
		ApertureVersion:      version.Version,
		AgentConfig:          agentCfg,
		Stdout:               os.Stdout,
		Stderr:               os.Stderr,
	}
	exit, invokeErr := adapter.Invoke(ctx, req)
	if invokeErr != nil {
		// Pre-exec failure per §16 row 12: exec not found, permission
		// denied, or any other error before the adapter's own process
		// body ran.
		return exitErr(exitCodeAdapterPreExecFail, invokeErr)
	}
	if exit != 0 {
		// Adapter ran and exited non-zero — propagate verbatim per §7.10.4.1.
		return exitErr(exit, fmt.Errorf("agent %q exited with code %d", agentName, exit))
	}
	return nil
}

type runArtifactPaths struct {
	JSON     string
	Markdown string
	Prompt   string
}

// writeRunArtifacts persists the JSON manifest, the Markdown manifest,
// and the merged prompt (markdown + "---" + task body) under the
// resolved output directory. Flag --out-dir overrides config; config
// overrides the `.aperture/manifests/` default.
func writeRunArtifacts(repoRoot, configDir, flagDir string, m *manifest.Manifest, rawTask string) (runArtifactPaths, error) {
	dir := flagDir
	if dir == "" {
		dir = configDir
	}
	if dir == "" {
		dir = ".aperture/manifests"
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(repoRoot, dir)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return runArtifactPaths{}, fmt.Errorf("create output dir: %w", err)
	}
	hashSuffix := stripSha256Prefix(m.ManifestHash)
	jsonPath := filepath.Join(dir, "manifest-"+hashSuffix+".json")
	mdPath := filepath.Join(dir, "manifest-"+hashSuffix+".md")
	promptPath := filepath.Join(dir, "run-"+m.ManifestID+".md")

	jsonBytes, err := manifest.EmitJSON(m)
	if err != nil {
		return runArtifactPaths{}, fmt.Errorf("serialize manifest json: %w", err)
	}
	if err := manifest.Validate(jsonBytes); err != nil {
		return runArtifactPaths{}, fmt.Errorf("manifest schema validation: %w", err)
	}
	if err := os.WriteFile(jsonPath, jsonBytes, 0o644); err != nil { //nolint:gosec // user-selected path
		return runArtifactPaths{}, fmt.Errorf("write json manifest: %w", err)
	}
	mdBytes := manifest.EmitMarkdown(m)
	if err := os.WriteFile(mdPath, mdBytes, 0o644); err != nil { //nolint:gosec // user-selected path
		return runArtifactPaths{}, fmt.Errorf("write markdown manifest: %w", err)
	}
	// Merged prompt = markdown body + "---" separator + task text. This
	// is the file built-in adapters (claude/codex) pipe on stdin or pass
	// as the initial message in interactive mode.
	var merged []byte
	merged = append(merged, mdBytes...)
	if len(mdBytes) > 0 && mdBytes[len(mdBytes)-1] != '\n' {
		merged = append(merged, '\n')
	}
	merged = append(merged, []byte("\n---\n\n")...)
	merged = append(merged, []byte(rawTask)...)
	if err := os.WriteFile(promptPath, merged, 0o644); err != nil { //nolint:gosec // user-selected path
		return runArtifactPaths{}, fmt.Errorf("write merged prompt: %w", err)
	}
	return runArtifactPaths{JSON: jsonPath, Markdown: mdPath, Prompt: promptPath}, nil
}

// resolveAdapterTaskPath returns the absolute path the adapter should
// see as APERTURE_TASK_PATH + the file positional. Inline tasks land
// in a fresh tempfile under $TMPDIR with a cleanup callback; file tasks
// resolve to their absolute form with nil cleanup (the user owns that
// file).
func resolveAdapterTaskPath(manifestID string, t task.Task) (path string, cleanup func(), err error) {
	if t.Source != "<inline>" {
		abs, absErr := filepath.Abs(t.Source)
		if absErr != nil {
			return "", nil, fmt.Errorf("resolve task path: %w", absErr)
		}
		return abs, nil, nil
	}
	return agent.WriteInlineTaskFile(manifestID, t.RawText)
}

// stripSha256Prefix turns "sha256:…" into "…" for filename usage.
func stripSha256Prefix(h string) string {
	return strings.TrimPrefix(h, "sha256:")
}
