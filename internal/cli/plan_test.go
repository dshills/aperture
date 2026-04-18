package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/dshills/aperture/internal/config"
	"github.com/dshills/aperture/internal/manifest"
	"github.com/dshills/aperture/internal/pipeline"
	"github.com/dshills/aperture/internal/repo"
	"github.com/dshills/aperture/internal/task"
	"github.com/dshills/aperture/internal/version"
)

func TestBuildStubManifest_IsSchemaValid(t *testing.T) {
	tmp := t.TempDir()
	parsed := task.Parse("Add OAuth refresh handling", task.ParseOptions{Source: "<inline>"})
	m, err := buildStubManifest(buildInputs{
		Config:     config.Default(),
		Task:       parsed,
		RepoRoot:   tmp,
		ModelFlag:  "",
		BudgetFlag: 120000,
	})
	if err != nil {
		t.Fatalf("buildStubManifest: %v", err)
	}
	b, err := manifest.EmitJSON(m)
	if err != nil {
		t.Fatalf("EmitJSON: %v", err)
	}
	if err := manifest.Validate(b); err != nil {
		t.Fatalf("manifest failed schema validation: %v", err)
	}
	// Critical Phase-1 invariants from the acceptance criteria.
	if len(m.Selections) != 0 {
		t.Error("selections must start empty in Phase 1")
	}
	if len(m.Gaps) != 0 {
		t.Error("gaps must start empty in Phase 1")
	}
	if m.Task.TaskID == "" {
		t.Error("task_id must be populated")
	}
	if m.GenerationMetadata.SelectionLogicVersion != "sel-v1" {
		t.Errorf("selection_logic_version wrong: %s", m.GenerationMetadata.SelectionLogicVersion)
	}
}

// Equivalent runs on identical inputs produce byte-identical hashes, even
// when generated_at / host / pid / aperture_version differ. Because the
// stub manifest is deterministic except for those fields, we verify hash
// stability across two builds where we then reset those excluded fields.
func TestBuildStubManifest_HashStableAcrossRuns(t *testing.T) {
	tmp := t.TempDir()
	parsed := task.Parse("Add OAuth refresh handling", task.ParseOptions{Source: "<inline>"})
	cfg := config.Default()
	m1, err := buildStubManifest(buildInputs{Config: cfg, Task: parsed, RepoRoot: tmp, BudgetFlag: 120000})
	if err != nil {
		t.Fatalf("buildStubManifest: %v", err)
	}
	m2, err := buildStubManifest(buildInputs{Config: cfg, Task: parsed, RepoRoot: tmp, BudgetFlag: 120000})
	if err != nil {
		t.Fatalf("buildStubManifest: %v", err)
	}
	if m1.ManifestHash != m2.ManifestHash {
		t.Fatalf("hash changed between runs on identical inputs: %s vs %s", m1.ManifestHash, m2.ManifestHash)
	}
	if m1.ManifestID != m2.ManifestID {
		t.Fatalf("id changed between runs on identical inputs: %s vs %s", m1.ManifestID, m2.ManifestID)
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

// Phase-2 end-to-end: running the pipeline against the small_go fixture
// should populate fingerprint, language_hints, and exclusions on a
// schema-valid manifest.
func TestPlan_SmallGoFixture_PopulatesRepoMetadata(t *testing.T) {
	fixture, err := filepath.Abs("../../testdata/fixtures/small_go")
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	parsed := task.Parse("add OAuth refresh handling", task.ParseOptions{Source: "<inline>"})

	res, err := pipeline.Build(pipeline.BuildOptions{
		Root:            fixture,
		DefaultExcludes: config.DefaultExclusions(),
	})
	if err != nil {
		t.Fatalf("pipeline.Build: %v", err)
	}
	fp, err := repo.Fingerprint(walkerFiles(res.Index), version.Version)
	if err != nil {
		t.Fatalf("Fingerprint: %v", err)
	}
	if len(fp) != len("sha256:")+64 {
		t.Fatalf("bad fingerprint shape: %s", fp)
	}

	m, err := buildStubManifest(buildInputs{
		Config:      cfg,
		Task:        parsed,
		RepoRoot:    fixture,
		BudgetFlag:  120000,
		Fingerprint: fp,
		Languages:   res.Index.LanguageHints(),
		Exclusions:  res.Exclusions,
	})
	if err != nil {
		t.Fatalf("buildStubManifest: %v", err)
	}

	jsonBytes, err := manifest.EmitJSON(m)
	if err != nil {
		t.Fatalf("EmitJSON: %v", err)
	}
	if err := manifest.Validate(jsonBytes); err != nil {
		t.Fatalf("schema validation failed: %v", err)
	}

	if m.Repo.Fingerprint == "" {
		t.Error("repo.fingerprint must be populated")
	}
	if !slices.Contains(m.Repo.LanguageHints, "go") {
		t.Errorf("language_hints must include go: %v", m.Repo.LanguageHints)
	}
	if m.GenerationMetadata.SideEffectTablesVersion != "side-effect-tables-v1" {
		t.Errorf("unexpected side_effect_tables_version: %s", m.GenerationMetadata.SideEffectTablesVersion)
	}
}

// Running the same plan twice on the same fixture produces byte-identical
// fingerprints and manifest hashes.
func TestPlan_SmallGoFixture_DeterministicFingerprint(t *testing.T) {
	fixture, err := filepath.Abs("../../testdata/fixtures/small_go")
	if err != nil {
		t.Fatal(err)
	}
	var hashes [2]string
	for i := 0; i < 2; i++ {
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
		hashes[i] = fp
	}
	if hashes[0] != hashes[1] {
		t.Fatalf("fingerprint not stable across runs: %s vs %s", hashes[0], hashes[1])
	}
}

// Sanity-check that the manifest JSON emitted through the full pipeline
// parses back into a map and carries the expected task anchors.
func TestPlan_EndToEndStubManifest(t *testing.T) {
	tmp := t.TempDir()
	parsed := task.Parse("investigate the broken fix", task.ParseOptions{Source: "<inline>"})
	m, err := buildStubManifest(buildInputs{
		Config:     config.Default(),
		Task:       parsed,
		RepoRoot:   tmp,
		ModelFlag:  "",
		BudgetFlag: 120000,
	})
	if err != nil {
		t.Fatal(err)
	}
	b, err := manifest.EmitJSON(m)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	// §7.3.1.1 priority: "fix" takes precedence over "investigate".
	task, _ := doc["task"].(map[string]any)
	if task["type"] != "bugfix" {
		t.Fatalf("expected bugfix, got %v", task["type"])
	}
}
