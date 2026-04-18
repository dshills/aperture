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

// PerRunMetadataKey is the canonical JSON key (and Markdown section
// title) for the v1.1 per-run metadata contract documented in the PLAN.
// Determinism tests strip exactly this section to compare reports byte-
// for-byte across runs.
const (
	PerRunMetadataJSONKey   = "per_run_metadata"
	PerRunMetadataMDHeading = "## Per-Run Metadata"
)

// RunReport is the structured form of `aperture eval run` output.
// Fixtures is ordered lexicographically by Name so the report is
// deterministic.
type RunReport struct {
	SchemaVersion         string            `json:"schema_version"`
	ApertureVersion       string            `json:"aperture_version"`
	SelectionLogicVersion string            `json:"selection_logic_version"`
	FixturesDir           string            `json:"fixtures_dir"`
	BaselinePath          string            `json:"baseline_path"`
	Tolerance             float64           `json:"tolerance"`
	Fixtures              []FixtureResult   `json:"fixtures"`
	Regressions           []RegressedFixt   `json:"regressions"`
	Orphaned              []string          `json:"orphaned_baseline_entries"`
	Unreferenced          []string          `json:"baseline_unreferenced_fixtures"`
	PerRunMetadata        PerRunMetadata    `json:"per_run_metadata"`
	BaselineSnapshot      *BaselineSnapshot `json:"baseline_snapshot,omitempty"`
}

// FixtureResult captures one fixture's outcome in a run.
type FixtureResult struct {
	Name           string   `json:"name"`
	Metrics        Metrics  `json:"metrics"`
	HardFail       bool     `json:"hard_fail"`
	HardFailReason []string `json:"hard_fail_reason"`
	ManifestHash   string   `json:"manifest_hash"`
	Error          string   `json:"error,omitempty"` // planner error for this fixture, if any
}

// RegressedFixt is the report-level shape of a regression, mirroring
// RegressedFixture from baseline.go but carrying JSON tags.
type RegressedFixt struct {
	Name      string  `json:"name"`
	BaselineF float64 `json:"baseline_f1"`
	CurrentF  float64 `json:"current_f1"`
	Drop      float64 `json:"drop"`
}

// PerRunMetadata holds the exempt-from-determinism section (§8.1).
type PerRunMetadata struct {
	GeneratedAt         string `json:"generated_at"`
	WallClockDurationMS int64  `json:"wall_clock_duration_ms"`
	Host                string `json:"host"`
	PID                 int    `json:"pid"`
	ApertureVersion     string `json:"aperture_version"`
}

// BaselineSnapshot is a small subset of the committed baseline included
// in the report for debugging when a regression fires. Only present in
// the JSON output (Markdown shows a one-liner summary).
type BaselineSnapshot struct {
	GeneratedAt     string                      `json:"generated_at"`
	ApertureVersion string                      `json:"aperture_version"`
	Fixtures        map[string]BaselineFixtureM `json:"fixtures"`
}

// NewReport assembles a RunReport from per-fixture results plus the
// optional regression comparison. `elapsed` is the total wall time of
// the run; it is placed into per-run metadata (not compared for
// byte-identity).
func NewReport(fixturesDir, baselinePath string, tolerance float64, results []FixtureResult, rc RegressionCheck, bl *Baseline, elapsed time.Duration) *RunReport {
	sort.Slice(results, func(i, j int) bool { return results[i].Name < results[j].Name })
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "unknown"
	}
	r := &RunReport{
		SchemaVersion:         BaselineSchemaVersion,
		ApertureVersion:       version.Version,
		SelectionLogicVersion: manifest.SelectionLogicVersion,
		FixturesDir:           fixturesDir,
		BaselinePath:          baselinePath,
		Tolerance:             tolerance,
		Fixtures:              results,
		Orphaned:              append([]string{}, rc.Orphaned...),
		Unreferenced:          append([]string{}, rc.Unreferenced...),
		PerRunMetadata: PerRunMetadata{
			GeneratedAt:         time.Now().UTC().Format(time.RFC3339),
			WallClockDurationMS: elapsed.Milliseconds(),
			Host:                host,
			PID:                 os.Getpid(),
			ApertureVersion:     version.Version,
		},
	}
	for _, rf := range rc.Regressed {
		r.Regressions = append(r.Regressions, RegressedFixt(rf))
	}
	if r.Regressions == nil {
		r.Regressions = []RegressedFixt{}
	}
	if bl != nil {
		r.BaselineSnapshot = &BaselineSnapshot{
			GeneratedAt:     bl.GeneratedAt,
			ApertureVersion: bl.ApertureVersion,
			Fixtures:        bl.Fixtures,
		}
	}
	return r
}

// EmitJSON returns the JSON-encoded report with a trailing newline.
func EmitJSON(r *RunReport) ([]byte, error) {
	buf, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(buf, '\n'), nil
}

// EmitMarkdown returns a Markdown-rendered report. The trailing section
// is always `## Per-Run Metadata`; determinism tests strip everything
// from that heading onward.
func EmitMarkdown(r *RunReport) []byte {
	var sb strings.Builder
	fmt.Fprintln(&sb, "# Aperture Eval Report")
	fmt.Fprintln(&sb)
	fmt.Fprintf(&sb, "- Fixtures dir: `%s`\n", r.FixturesDir)
	fmt.Fprintf(&sb, "- Baseline path: `%s`\n", r.BaselinePath)
	fmt.Fprintf(&sb, "- Tolerance: `%.3f`\n", r.Tolerance)
	fmt.Fprintf(&sb, "- Selection logic: `%s`\n", r.SelectionLogicVersion)
	fmt.Fprintln(&sb)

	fmt.Fprintln(&sb, "## Summary")
	fmt.Fprintln(&sb)
	fmt.Fprintln(&sb, "| Fixture | Precision | Recall | F1 | Hard fail |")
	fmt.Fprintln(&sb, "|---------|-----------|--------|----|-----------|")
	for _, f := range r.Fixtures {
		hf := ""
		if f.HardFail {
			hf = "yes"
		}
		fmt.Fprintf(&sb, "| %s | %.4f | %.4f | %.4f | %s |\n",
			f.Name, f.Metrics.Precision, f.Metrics.Recall, f.Metrics.F1, hf)
	}
	fmt.Fprintln(&sb)

	if len(r.Regressions) > 0 {
		fmt.Fprintln(&sb, "## Regressions")
		fmt.Fprintln(&sb)
		for _, rg := range r.Regressions {
			fmt.Fprintf(&sb, "- **%s**: F1 %.4f → %.4f (drop %.4f, tolerance %.4f)\n",
				rg.Name, rg.BaselineF, rg.CurrentF, rg.Drop, r.Tolerance)
		}
		fmt.Fprintln(&sb)
	} else {
		fmt.Fprintln(&sb, "## Regressions")
		fmt.Fprintln(&sb)
		fmt.Fprintln(&sb, "_none_")
		fmt.Fprintln(&sb)
	}

	if len(r.Orphaned) > 0 {
		fmt.Fprintln(&sb, "## Orphaned baseline entries")
		fmt.Fprintln(&sb)
		for _, n := range r.Orphaned {
			fmt.Fprintf(&sb, "- %s\n", n)
		}
		fmt.Fprintln(&sb)
	}
	if len(r.Unreferenced) > 0 {
		fmt.Fprintln(&sb, "## Current-run fixtures missing from baseline")
		fmt.Fprintln(&sb)
		for _, n := range r.Unreferenced {
			fmt.Fprintf(&sb, "- %s\n", n)
		}
		fmt.Fprintln(&sb)
	}

	// Per-fixture hard-fail details.
	anyHF := false
	for _, f := range r.Fixtures {
		if f.HardFail || f.Error != "" {
			anyHF = true
			break
		}
	}
	if anyHF {
		fmt.Fprintln(&sb, "## Fixture details")
		fmt.Fprintln(&sb)
		for _, f := range r.Fixtures {
			if !f.HardFail && f.Error == "" {
				continue
			}
			fmt.Fprintf(&sb, "### %s\n\n", f.Name)
			if f.Error != "" {
				fmt.Fprintf(&sb, "- Error: %s\n", f.Error)
			}
			for _, reason := range f.HardFailReason {
				fmt.Fprintf(&sb, "- Hard fail: %s\n", reason)
			}
			fmt.Fprintln(&sb)
		}
	}

	// Per-run metadata section (exempt from determinism tests).
	fmt.Fprintln(&sb, PerRunMetadataMDHeading)
	fmt.Fprintln(&sb)
	fmt.Fprintf(&sb, "- generated_at: %s\n", r.PerRunMetadata.GeneratedAt)
	fmt.Fprintf(&sb, "- wall_clock_duration_ms: %d\n", r.PerRunMetadata.WallClockDurationMS)
	fmt.Fprintf(&sb, "- host: %s\n", r.PerRunMetadata.Host)
	fmt.Fprintf(&sb, "- pid: %d\n", r.PerRunMetadata.PID)
	fmt.Fprintf(&sb, "- aperture_version: %s\n", r.PerRunMetadata.ApertureVersion)

	return []byte(sb.String())
}

// StripPerRunJSON returns a copy of the JSON-encoded report with the
// `per_run_metadata` key removed. Used by determinism tests to diff two
// consecutive runs without the per-run-timestamp noise.
func StripPerRunJSON(buf []byte) ([]byte, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(buf, &m); err != nil {
		return nil, err
	}
	delete(m, PerRunMetadataJSONKey)
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

// StripPerRunMarkdown returns the Markdown report truncated at the
// `## Per-Run Metadata` heading.
func StripPerRunMarkdown(buf []byte) []byte {
	i := strings.Index(string(buf), PerRunMetadataMDHeading)
	if i < 0 {
		return buf
	}
	return buf[:i]
}
