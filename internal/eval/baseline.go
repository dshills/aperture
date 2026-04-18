package eval

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/dshills/aperture/internal/manifest"
	"github.com/dshills/aperture/internal/version"
)

// BaselineSchemaVersion is the committed schema version for baseline.json.
const BaselineSchemaVersion = "1.0"

// Baseline is the on-disk representation of a committed selection-quality
// reference (§7.1.3). Fields map exits the JSON keys documented in the spec.
type Baseline struct {
	SchemaVersion         string                      `json:"schema_version"`
	GeneratedAt           string                      `json:"generated_at"`
	ApertureVersion       string                      `json:"aperture_version"`
	SelectionLogicVersion string                      `json:"selection_logic_version"`
	Fixtures              map[string]BaselineFixtureM `json:"fixtures"`
}

// BaselineFixtureM holds the three committed metrics per fixture.
type BaselineFixtureM struct {
	Precision float64 `json:"precision"`
	Recall    float64 `json:"recall"`
	F1        float64 `json:"f1"`
}

// LoadBaseline reads the baseline from path. Returns (nil, nil) when the
// file does not exist so callers can implement the bootstrap rule in §4.2.
func LoadBaseline(path string) (*Baseline, error) {
	b, err := os.ReadFile(path) //nolint:gosec // path from user config
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var bl Baseline
	if err := json.Unmarshal(b, &bl); err != nil {
		return nil, fmt.Errorf("parse baseline: %w", err)
	}
	if bl.SchemaVersion != BaselineSchemaVersion {
		return nil, fmt.Errorf("baseline schema_version %q is not supported (expected %q)", bl.SchemaVersion, BaselineSchemaVersion)
	}
	return &bl, nil
}

// WriteBaseline serializes bl to path with sorted fixture keys and a
// trailing newline. The output is deterministic modulo `generated_at`.
func WriteBaseline(path string, bl *Baseline) error {
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create baseline dir: %w", err)
		}
	}
	buf, err := marshalBaseline(bl)
	if err != nil {
		return err
	}
	return os.WriteFile(path, buf, 0o644) //nolint:gosec // user-selected path
}

// marshalBaseline pretty-prints the baseline with sorted fixture keys.
func marshalBaseline(bl *Baseline) ([]byte, error) {
	// encoding/json sorts map keys lexicographically when marshaling.
	buf, err := json.MarshalIndent(bl, "", "  ")
	if err != nil {
		return nil, err
	}
	buf = append(buf, '\n')
	return buf, nil
}

// BuildBaselineFromRun returns a Baseline populated from run.Fixtures
// (in sorted key order) with the current aperture_version and
// selection_logic_version stamped. generated_at is a UTC RFC3339 string.
func BuildBaselineFromRun(run *RunReport) *Baseline {
	bl := &Baseline{
		SchemaVersion:         BaselineSchemaVersion,
		GeneratedAt:           time.Now().UTC().Format(time.RFC3339),
		ApertureVersion:       version.Version,
		SelectionLogicVersion: manifest.SelectionLogicVersion,
		Fixtures:              make(map[string]BaselineFixtureM, len(run.Fixtures)),
	}
	for _, fr := range run.Fixtures {
		bl.Fixtures[fr.Name] = BaselineFixtureM{
			Precision: fr.Metrics.Precision,
			Recall:    fr.Metrics.Recall,
			F1:        fr.Metrics.F1,
		}
	}
	return bl
}

// RegressionCheck compares each fixture's current F1 against the
// baseline's F1, returning a sorted list of regressed fixture names
// (F1 dropped by more than tolerance). An actual current fixture whose
// name is absent from the baseline is a soft miss (returned in
// `unreferenced`). A baseline fixture absent from the current run is
// returned in `orphaned`; callers decide whether to treat it as exit 2
// per §7.1.3.
type RegressionCheck struct {
	Regressed    []RegressedFixture
	Orphaned     []string
	Unreferenced []string
}

// RegressedFixture names a fixture whose F1 dropped by more than the
// configured tolerance vs. the baseline.
type RegressedFixture struct {
	Name      string
	BaselineF float64
	CurrentF  float64
	Drop      float64
}

// CheckRegressions compares run against bl and returns the check summary.
// bl may be nil (bootstrap path); in that case every fixture is reported
// under Unreferenced and no regressions fire.
func CheckRegressions(run *RunReport, bl *Baseline, tolerance float64) RegressionCheck {
	var rc RegressionCheck
	byName := map[string]BaselineFixtureM{}
	if bl != nil {
		byName = bl.Fixtures
	}
	current := map[string]FixtureResult{}
	for _, fr := range run.Fixtures {
		current[fr.Name] = fr
	}
	for name, base := range byName {
		cur, ok := current[name]
		if !ok {
			rc.Orphaned = append(rc.Orphaned, name)
			continue
		}
		drop := base.F1 - cur.Metrics.F1
		if drop > tolerance {
			rc.Regressed = append(rc.Regressed, RegressedFixture{
				Name: name, BaselineF: base.F1, CurrentF: cur.Metrics.F1, Drop: drop,
			})
		}
	}
	for name := range current {
		if _, ok := byName[name]; !ok {
			rc.Unreferenced = append(rc.Unreferenced, name)
		}
	}
	sort.Strings(rc.Orphaned)
	sort.Strings(rc.Unreferenced)
	sort.Slice(rc.Regressed, func(i, j int) bool { return rc.Regressed[i].Name < rc.Regressed[j].Name })
	return rc
}
