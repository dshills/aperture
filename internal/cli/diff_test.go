package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeManifestFixture marshals m into a temporary file at path (under
// the test's temp dir) and returns the absolute path.
func writeManifestFixture(t *testing.T, dir, name string, body map[string]any) string {
	t.Helper()
	p := filepath.Join(dir, name)
	buf, err := json.MarshalIndent(body, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, append(buf, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// minimalManifest returns the minimum set of fields required for the
// diff tool to successfully load and diff a manifest.
func minimalManifest() map[string]any {
	return map[string]any{
		"schema_version": "1.0",
		"manifest_id":    "apt_0000000000000001",
		"manifest_hash":  "sha256:0000000000000000000000000000000000000000000000000000000000000001",
		"generated_at":   "2026-04-18T00:00:00Z",
		"incomplete":     false,
		"task": map[string]any{
			"task_id":              "tsk_abcdef0123456789",
			"source":               "<inline>",
			"raw_text":             "hello",
			"type":                 "feature",
			"objective":            "hello",
			"anchors":              []string{},
			"expects_tests":        false,
			"expects_config":       false,
			"expects_docs":         false,
			"expects_migration":    false,
			"expects_api_contract": false,
		},
		"repo": map[string]any{
			"root":           "/tmp/x",
			"fingerprint":    "sha256:aaaa",
			"language_hints": []string{"go"},
		},
		"budget": map[string]any{
			"model":                     "claude-sonnet-4-6",
			"token_ceiling":             200000,
			"reserved":                  map[string]any{"instructions": 0, "reasoning": 0, "tool_output": 0, "expansion": 0},
			"effective_context_budget":  152000,
			"estimated_selected_tokens": 0,
			"estimator":                 "heuristic-3.5",
			"estimator_version":         "v1",
		},
		"selections": []any{},
		"reachable":  []any{},
		"exclusions": []any{},
		"gaps":       []any{},
		"feasibility": map[string]any{
			"score":               0.0,
			"assessment":          "ok",
			"positives":           []string{},
			"negatives":           []string{},
			"blocking_conditions": []string{},
			"sub_signals":         map[string]any{},
		},
		"generation_metadata": map[string]any{
			"aperture_version":           "1.1.0",
			"selection_logic_version":    "sel-v2",
			"config_digest":              "sha256:cfg1",
			"side_effect_tables_version": "side-effect-tables-v1",
			"host":                       "h",
			"pid":                        1,
			"wall_clock_started_at":      "2026-04-18T00:00:00Z",
		},
	}
}

// TestDiff_IdenticalManifestsExit0 covers the §4.5 "aperture diff A.json A.json" acceptance.
func TestDiff_IdenticalManifestsExit0(t *testing.T) {
	dir := t.TempDir()
	p := writeManifestFixture(t, dir, "m.json", minimalManifest())

	root := NewRoot()
	var stdout bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stdout)
	root.SetArgs([]string{"diff", p, p, "--format", "json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("aperture diff failed: %v", err)
	}

	var report map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &report); err != nil {
		t.Fatalf("output not JSON: %v\n%s", err, stdout.String())
	}
	if report["semantic_equivalent"] != true {
		t.Errorf("semantic_equivalent should be true for A==A; got %+v", report["semantic_equivalent"])
	}
}

// TestDiff_SchemaVersionTooLowExits1 covers the §7.7 exit-code rule.
func TestDiff_SchemaVersionTooLowExits1(t *testing.T) {
	dir := t.TempDir()
	bad := minimalManifest()
	bad["schema_version"] = "0.9"
	badPath := writeManifestFixture(t, dir, "bad.json", bad)
	okPath := writeManifestFixture(t, dir, "ok.json", minimalManifest())

	root := NewRoot()
	var stdout bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stdout)
	root.SetArgs([]string{"diff", badPath, okPath})
	err := root.Execute()
	var ec *ExitCodeError
	if !errors.As(err, &ec) {
		t.Fatalf("expected ExitCodeError, got %v", err)
	}
	if ec.Code != exitCodeInternal {
		t.Errorf("exit code %d, want %d", ec.Code, exitCodeInternal)
	}
}

// TestDiff_SelectionChangeRendersInMarkdown exercises the Markdown
// emitter end-to-end through the CLI.
func TestDiff_SelectionChangeRendersInMarkdown(t *testing.T) {
	dir := t.TempDir()
	a := minimalManifest()
	a["selections"] = []any{
		map[string]any{
			"path":             "a.go",
			"kind":             "file",
			"load_mode":        "full",
			"relevance_score":  0.8,
			"score_breakdown":  []any{},
			"estimated_tokens": 100,
			"rationale":        []string{},
			"side_effects":     []string{},
		},
	}
	b := minimalManifest()
	b["manifest_hash"] = "sha256:deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	b["selections"] = []any{
		map[string]any{
			"path":             "b.go",
			"kind":             "file",
			"load_mode":        "full",
			"relevance_score":  0.7,
			"score_breakdown":  []any{},
			"estimated_tokens": 80,
			"rationale":        []string{},
			"side_effects":     []string{},
		},
	}
	pa := writeManifestFixture(t, dir, "a.json", a)
	pb := writeManifestFixture(t, dir, "b.json", b)

	root := NewRoot()
	var stdout bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stdout)
	root.SetArgs([]string{"diff", pa, pb, "--format", "markdown"})
	if err := root.Execute(); err != nil {
		t.Fatalf("diff failed: %v", err)
	}
	md := stdout.String()
	if !strings.Contains(md, "semantic_equivalent: false") {
		t.Errorf("markdown missing semantic_equivalent banner: %s", md)
	}
	if !strings.Contains(md, "a.go") || !strings.Contains(md, "b.go") {
		t.Errorf("markdown should mention both paths:\n%s", md)
	}
}

// TestDiff_MalformedJSONExits1: prism-class parse error → exit 1.
func TestDiff_MalformedJSONExits1(t *testing.T) {
	dir := t.TempDir()
	garbage := filepath.Join(dir, "garbage.json")
	if err := os.WriteFile(garbage, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	okPath := writeManifestFixture(t, dir, "ok.json", minimalManifest())

	root := NewRoot()
	var stdout bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stdout)
	root.SetArgs([]string{"diff", garbage, okPath})
	err := root.Execute()
	var ec *ExitCodeError
	if !errors.As(err, &ec) || ec.Code != exitCodeInternal {
		t.Errorf("expected exitCodeInternal, got %v", err)
	}
}
