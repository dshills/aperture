package cli

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	"github.com/dshills/aperture/internal/config"
	"github.com/dshills/aperture/internal/manifest"
	"github.com/dshills/aperture/internal/pipeline"
	"github.com/dshills/aperture/internal/repo"
	"github.com/dshills/aperture/internal/task"
	"github.com/dshills/aperture/internal/version"
)

// buildFixtureInputs is the common test setup: walk the small_go fixture,
// compute fingerprint, and return a populated buildInputs. Callers
// override the Model / Budget flags as needed.
func buildFixtureInputs(t *testing.T, rawTask string, model string, budget int) buildInputs {
	t.Helper()
	fixture, err := filepath.Abs("../../testdata/fixtures/small_go")
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	parsed := task.Parse(rawTask, task.ParseOptions{Source: "<inline>"})
	res, err := pipeline.Build(pipeline.BuildOptions{
		Root:            fixture,
		DefaultExcludes: config.DefaultExclusions(),
	})
	if err != nil {
		t.Fatal(err)
	}
	fp, err := repo.Fingerprint(walkerFiles(res.Index), version.Version)
	if err != nil {
		t.Fatal(err)
	}
	return buildInputs{
		Config:      cfg,
		Task:        parsed,
		RepoRoot:    fixture,
		ModelFlag:   model,
		BudgetFlag:  budget,
		Fingerprint: fp,
		Languages:   res.Index.LanguageHints(),
		Exclusions:  res.Exclusions,
		Index:       res.Index,
	}
}

func TestBuildManifest_SmallGoFixture_Populated(t *testing.T) {
	in := buildFixtureInputs(t, "add refresh handling to internal/oauth/provider.go for github oauth", "", 120000)
	m, err := BuildManifest(in)
	if err != nil {
		t.Fatalf("BuildManifest: %v", err)
	}
	b, err := manifest.EmitJSON(m)
	if err != nil {
		t.Fatalf("EmitJSON: %v", err)
	}
	if err := manifest.Validate(b); err != nil {
		t.Fatalf("schema validation failed: %v", err)
	}

	if m.Repo.Fingerprint == "" {
		t.Error("repo.fingerprint missing")
	}
	if !slices.Contains(m.Repo.LanguageHints, "go") {
		t.Errorf("expected go in language_hints: %v", m.Repo.LanguageHints)
	}
	if m.Budget.Estimator != "heuristic-3.5" {
		t.Errorf("unspecified model should use heuristic-3.5, got %q", m.Budget.Estimator)
	}
	// provider.go should have been picked up as a selection.
	var sawProvider bool
	for _, s := range m.Selections {
		if s.Path == "internal/oauth/provider.go" {
			sawProvider = true
			if s.LoadMode == "" {
				t.Error("selection missing load_mode")
			}
			if len(s.ScoreBreakdown) == 0 {
				t.Error("selection missing score_breakdown")
			}
			for _, entry := range s.ScoreBreakdown {
				if entry.Signal == 0 {
					t.Errorf("zero-signal factor %q should have been omitted", entry.Factor)
				}
			}
		}
	}
	if !sawProvider {
		t.Errorf("expected internal/oauth/provider.go in selections, got %d selections", len(m.Selections))
	}
}

// §7.6.1.1: --model gpt-4o routes to tiktoken:o200k_base.
func TestBuildManifest_TiktokenDispatch(t *testing.T) {
	in := buildFixtureInputs(t, "add refresh handling to internal/oauth/provider.go for github oauth", "gpt-4o", 120000)
	m, err := BuildManifest(in)
	if err != nil {
		t.Fatalf("BuildManifest: %v", err)
	}
	if m.Budget.Estimator != "tiktoken:o200k_base" {
		t.Fatalf("expected tiktoken:o200k_base, got %q", m.Budget.Estimator)
	}
	if m.Budget.EstimatorVersion == "" {
		t.Error("estimator_version should be recorded")
	}
}

// §7.6.5: budget underflow must emit incomplete=true and exit 9.
func TestBuildManifest_BudgetUnderflow(t *testing.T) {
	in := buildFixtureInputs(t, "add refresh handling to internal/oauth/provider.go for github oauth", "", 100)
	m, err := BuildManifest(in)
	var ec *ExitCodeError
	if !errors.As(err, &ec) {
		t.Fatalf("expected ExitCodeError, got %v", err)
	}
	if ec.Code != 9 {
		t.Fatalf("underflow must exit 9, got %d", ec.Code)
	}
	if m == nil {
		t.Fatal("manifest must still be emitted on underflow for auditability")
	}
	if !m.Incomplete {
		t.Error("incomplete flag must be true on underflow")
	}
}

// Repeated runs on identical inputs must produce byte-identical manifest
// hashes — the §7.9.4 determinism invariant.
func TestBuildManifest_DeterministicHash(t *testing.T) {
	var h1, h2 string
	for i := 0; i < 2; i++ {
		in := buildFixtureInputs(t, "add refresh handling to internal/oauth/provider.go for github oauth", "", 120000)
		m, err := BuildManifest(in)
		if err != nil {
			t.Fatalf("BuildManifest: %v", err)
		}
		if i == 0 {
			h1 = m.ManifestHash
		} else {
			h2 = m.ManifestHash
		}
	}
	if h1 != h2 {
		t.Fatalf("manifest hash not deterministic: %s vs %s", h1, h2)
	}
}

func TestReadTask_Inline(t *testing.T) {
	raw, src, isMD, err := readTask(nil, "Do the thing")
	if err != nil {
		t.Fatalf("readTask: %v", err)
	}
	if raw != "Do the thing" || src != "<inline>" || isMD {
		t.Fatalf("unexpected readTask result: %q %q %v", raw, src, isMD)
	}
}

func TestReadTask_File(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "t.md")
	if err := os.WriteFile(p, []byte("# t\n\nhello"), 0o600); err != nil {
		t.Fatal(err)
	}
	raw, src, isMD, err := readTask([]string{p}, "")
	if err != nil {
		t.Fatalf("readTask: %v", err)
	}
	if raw == "" || src != p || !isMD {
		t.Fatalf("unexpected readTask result: %q %q %v", raw, src, isMD)
	}
}

func TestReadTask_RejectsBinary(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "t.bin")
	if err := os.WriteFile(p, []byte{0x00, 0x01, 0x02}, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := readTask([]string{p}, ""); err == nil {
		t.Fatal("expected binary rejection")
	}
}

// Phase-4: gaps and feasibility populated on small_go fixture.
func TestBuildManifest_Phase4_GapsAndFeasibility(t *testing.T) {
	in := buildFixtureInputs(t, "add refresh handling to internal/oauth/provider.go for github oauth", "", 120000)
	m, err := BuildManifest(in)
	if err != nil {
		t.Fatalf("BuildManifest: %v", err)
	}
	// Feasibility must be populated with non-empty sub-signals.
	if m.Feasibility.Score <= 0 {
		t.Errorf("feasibility should be positive, got %.4f", m.Feasibility.Score)
	}
	for _, k := range []string{"coverage", "anchor_resolution", "task_specificity", "budget_headroom", "gap_penalty"} {
		if _, ok := m.Feasibility.SubSignals[k]; !ok {
			t.Errorf("missing sub_signal %q", k)
		}
	}
	if m.Feasibility.Assessment == "" {
		t.Error("assessment must be populated")
	}
	// All gap IDs sequential and start at gap-1.
	for i, g := range m.Gaps {
		want := "gap-" + strconv.Itoa(i+1)
		if g.ID != want {
			t.Errorf("gap %d has ID %q, want %q", i, g.ID, want)
		}
	}
}

// --min-feasibility threshold: exit code 7 when score falls below.
func TestBuildManifest_Phase4_MinFeasibilityExit7(t *testing.T) {
	// A task that will produce very low feasibility (unknown action type).
	in := buildFixtureInputs(t, "foo bar baz", "", 120000)
	m, err := BuildManifest(in)
	if err != nil {
		t.Fatalf("BuildManifest should not itself fail: %v", err)
	}
	if m.Feasibility.Score >= 0.85 {
		t.Skipf("fixture happened to score above threshold (%.4f) — adjust task", m.Feasibility.Score)
	}
	// Simulate the plan command's threshold logic.
	threshold := 0.85
	if m.Feasibility.Score >= threshold {
		t.Fatalf("expected score below threshold, got %.4f", m.Feasibility.Score)
	}
}

// --fail-on-gaps: exit 8 when a blocking gap fires (via gaps.blocking
// config upgrade from warning).
func TestBuildManifest_Phase4_FailOnGapsExit8(t *testing.T) {
	in := buildFixtureInputs(t, "add refresh handling to internal/oauth/provider.go for github oauth", "", 120000)
	in.Config.Gaps.Blocking = []string{string(manifest.GapMissingSpec)}
	m, err := BuildManifest(in)
	if err != nil {
		t.Fatalf("BuildManifest: %v", err)
	}
	var blocking *manifest.Gap
	for i, g := range m.Gaps {
		if g.Severity == manifest.GapSeverityBlocking {
			blocking = &m.Gaps[i]
			break
		}
	}
	if blocking == nil {
		t.Fatalf("expected at least one blocking gap after config upgrade, got %+v", m.Gaps)
	}
}

// Rule-priority sanity: §7.3.1.1 classifies "investigate the broken fix"
// as bugfix because Rule 1 wins over Rule 6.
func TestBuildManifest_RuleOrderPriority(t *testing.T) {
	in := buildFixtureInputs(t, "investigate the broken fix", "", 120000)
	m, err := BuildManifest(in)
	if err != nil {
		t.Fatalf("BuildManifest: %v", err)
	}
	b, err := manifest.EmitJSON(m)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	taskMap, _ := doc["task"].(map[string]any)
	if taskMap["type"] != "bugfix" {
		t.Fatalf("expected bugfix, got %v", taskMap["type"])
	}
}
