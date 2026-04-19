package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/dshills/aperture/internal/manifest"
	"github.com/dshills/aperture/internal/version"
)

// LoadmodeFixtureResult is one row of the `aperture eval loadmode`
// report: symbolic diff always, agent_check deltas when declared.
type LoadmodeFixtureResult struct {
	Name               string            `json:"name"`
	DeclaresAgentCheck bool              `json:"declares_agent_check"`
	Error              string            `json:"error,omitempty"`
	PlanAManifestHash  string            `json:"plan_a_manifest_hash"`
	PlanBManifestHash  string            `json:"plan_b_manifest_hash"`
	Symbolic           SymbolicDiff      `json:"symbolic_diff"`
	AgentCheckPlanA    AgentCheckSummary `json:"agent_check_plan_a,omitempty"`
	AgentCheckPlanB    AgentCheckSummary `json:"agent_check_plan_b,omitempty"`
	Delta              LoadmodeDelta     `json:"delta,omitempty"`
}

// AgentCheckSummary is the JSON-friendly view of an
// AgentCheckResult — bytes fields are dropped to keep the report
// small. Stdout / stderr go into a separate log file when
// --log-dir is set (Phase 6 is happy to omit this for now).
type AgentCheckSummary struct {
	Outcome    AgentCheckOutcome `json:"outcome"`
	ExitCode   int               `json:"exit_code"`
	DurationMS int64             `json:"duration_ms"`
}

// LoadmodeReport is the top-level JSON shape emitted by
// `aperture eval loadmode`. Fixtures is sorted by name.
type LoadmodeReport struct {
	SchemaVersion         string                  `json:"schema_version"`
	ApertureVersion       string                  `json:"aperture_version"`
	SelectionLogicVersion string                  `json:"selection_logic_version"`
	FixturesDir           string                  `json:"fixtures_dir"`
	Fixtures              []LoadmodeFixtureResult `json:"fixtures"`
	Advisory              Advisory                `json:"advisory"`
	PerRunMetadata        PerRunMetadata          `json:"per_run_metadata"`
}

// LoadmodeSchemaVersion is the committed version for the loadmode
// report JSON.
const LoadmodeSchemaVersion = "1.0"

// NewLoadmodeReport packages fixture rows + the aggregate advisory
// into the emit-ready structure. Callers pass `elapsed` so the
// per-run metadata block carries a real wall-clock duration.
func NewLoadmodeReport(fixturesDir string, rows []LoadmodeFixtureResult, elapsed time.Duration) *LoadmodeReport {
	sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "unknown"
	}
	inputs := make([]AdvisoryInput, 0, len(rows))
	for _, r := range rows {
		inputs = append(inputs, AdvisoryInput{
			HasAgentCheck: r.DeclaresAgentCheck && r.Error == "",
			PlanAPass:     r.AgentCheckPlanA.Outcome == AgentCheckPass,
			PlanBPass:     r.AgentCheckPlanB.Outcome == AgentCheckPass,
		})
	}
	return &LoadmodeReport{
		SchemaVersion:         LoadmodeSchemaVersion,
		ApertureVersion:       version.Version,
		SelectionLogicVersion: manifest.SelectionLogicVersion,
		FixturesDir:           fixturesDir,
		Fixtures:              rows,
		Advisory:              ComputeAdvisory(inputs),
		PerRunMetadata: PerRunMetadata{
			GeneratedAt:         time.Now().UTC().Format(time.RFC3339),
			WallClockDurationMS: elapsed.Milliseconds(),
			Host:                host,
			PID:                 os.Getpid(),
			ApertureVersion:     version.Version,
		},
	}
}

// EmitLoadmodeJSON serializes r with stable field order and a
// trailing newline.
func EmitLoadmodeJSON(r *LoadmodeReport) ([]byte, error) {
	buf, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(buf, '\n'), nil
}

// EmitLoadmodeMarkdown renders the report as a human-readable
// document. Section layout:
//
//	# Aperture Load-Mode Calibration
//	## Summary (fixture count, advisory if any)
//	## Per-fixture reports (symbolic diff + agent_check deltas)
//	## Per-Run Metadata (exempt from determinism tests)
func EmitLoadmodeMarkdown(r *LoadmodeReport) []byte {
	var sb strings.Builder
	fmt.Fprintln(&sb, "# Aperture Load-Mode Calibration")
	fmt.Fprintln(&sb)
	fmt.Fprintf(&sb, "- Fixtures dir: `%s`\n", r.FixturesDir)
	fmt.Fprintf(&sb, "- Selection logic: `%s`\n", r.SelectionLogicVersion)
	fmt.Fprintln(&sb)

	if r.Advisory.Emit {
		fmt.Fprintln(&sb, "## Advisory")
		fmt.Fprintln(&sb)
		fmt.Fprintf(&sb, "%s\n\n", r.Advisory.Message)
	} else {
		fmt.Fprintln(&sb, "## Advisory")
		fmt.Fprintln(&sb)
		fmt.Fprintln(&sb, "_no recommendation — Plan_A pass-rate is not materially below Plan_B._")
		fmt.Fprintln(&sb)
	}

	fmt.Fprintln(&sb, "## Fixtures")
	fmt.Fprintln(&sb)
	for _, f := range r.Fixtures {
		fmt.Fprintf(&sb, "### %s\n\n", f.Name)
		if f.Error != "" {
			fmt.Fprintf(&sb, "- Error: %s\n\n", f.Error)
			continue
		}
		fmt.Fprintf(&sb, "- Plan_A hash: `%s`\n", f.PlanAManifestHash)
		fmt.Fprintf(&sb, "- Plan_B hash: `%s`\n", f.PlanBManifestHash)
		fmt.Fprintf(&sb, "- Tokens gained by forcing: %d\n", f.Symbolic.TokensGainedByForcing)
		fmt.Fprintf(&sb, "- Budget overflow: %d tokens\n", f.Symbolic.BudgetOverflowTokens)
		fmt.Fprintf(&sb, "- Forced-full would underflow: %v\n", f.Symbolic.ForcedFullWouldUnderflow)
		fmt.Fprintf(&sb, "- Feasibility delta: %+.4f\n", f.Symbolic.FeasibilityDelta)
		if len(f.Symbolic.DemotedInA) > 0 {
			fmt.Fprintln(&sb, "- Demoted in A (held at full in B):")
			for _, d := range f.Symbolic.DemotedInA {
				fmt.Fprintf(&sb, "  - `%s` (score=%.4f, tokens=%d)\n", d.Path, d.ScoreA, d.TokenCount)
			}
		}
		if len(f.Symbolic.GapsFiredInAOnly) > 0 {
			fmt.Fprintln(&sb, "- Gaps fired in A only:", strings.Join(f.Symbolic.GapsFiredInAOnly, ", "))
		}
		if len(f.Symbolic.GapsFiredInBOnly) > 0 {
			fmt.Fprintln(&sb, "- Gaps fired in B only:", strings.Join(f.Symbolic.GapsFiredInBOnly, ", "))
		}
		if f.DeclaresAgentCheck {
			fmt.Fprintf(&sb, "- agent_check(A): %s (exit=%d, %dms)\n",
				f.AgentCheckPlanA.Outcome, f.AgentCheckPlanA.ExitCode, f.AgentCheckPlanA.DurationMS)
			fmt.Fprintf(&sb, "- agent_check(B): %s (exit=%d, %dms)\n",
				f.AgentCheckPlanB.Outcome, f.AgentCheckPlanB.ExitCode, f.AgentCheckPlanB.DurationMS)
			fmt.Fprintf(&sb, "- Delta: **%s**\n", f.Delta)
		}
		fmt.Fprintln(&sb)
	}

	// Per-run metadata section (exempt from determinism tests, per
	// v1.1 §8.1 contract shared across all eval reports).
	fmt.Fprintln(&sb, PerRunMetadataMDHeading)
	fmt.Fprintln(&sb)
	fmt.Fprintf(&sb, "- generated_at: %s\n", r.PerRunMetadata.GeneratedAt)
	fmt.Fprintf(&sb, "- wall_clock_duration_ms: %d\n", r.PerRunMetadata.WallClockDurationMS)
	fmt.Fprintf(&sb, "- host: %s\n", r.PerRunMetadata.Host)
	fmt.Fprintf(&sb, "- pid: %d\n", r.PerRunMetadata.PID)
	fmt.Fprintf(&sb, "- aperture_version: %s\n", r.PerRunMetadata.ApertureVersion)
	return []byte(sb.String())
}
