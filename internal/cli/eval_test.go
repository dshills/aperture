//go:build !notier2

// The committed eval fixtures include `polyglot-resolver` whose
// expected F1 assumes tier-2 is enabled. Under `-tags notier2` the
// tier-2 analyzer is stubbed and that fixture regresses; guard the
// CLI integration tests accordingly. Unit-level eval tests (scoring
// math, baseline I/O, determinism) are in internal/eval and are
// language-agnostic.

package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dshills/aperture/internal/eval"
)

// TestEvalRun_TrivialFixtures runs the committed trivial-pass and
// trivial-fail fixtures against the committed baseline and asserts
// exit 0 with no regressions.
func TestEvalRun_TrivialFixtures(t *testing.T) {
	fixturesDir, err := filepath.Abs("../../testdata/eval")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(fixturesDir, "baseline.json")); err != nil {
		t.Skip("baseline.json not committed; rebuild via `aperture eval baseline --force`")
	}
	root := NewRoot()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"eval", "run", "--fixtures", fixturesDir, "--format", "markdown"})
	err = root.Execute()
	if err != nil {
		var ec *ExitCodeError
		if errors.As(err, &ec) {
			t.Fatalf("eval run exited %d: %s\nstdout:\n%s\nstderr:\n%s",
				ec.Code, ec.Err, stdout.String(), stderr.String())
		}
		t.Fatal(err)
	}
	out := stdout.String()
	if !strings.Contains(out, "trivial-pass") || !strings.Contains(out, "trivial-fail") {
		t.Errorf("report missing fixture rows:\n%s", out)
	}
	if !strings.Contains(out, PerRunMetadataMDHeadingFromEval()) {
		t.Errorf("report missing per-run metadata section:\n%s", out)
	}
}

// PerRunMetadataMDHeadingFromEval returns the canonical heading so the
// cli test doesn't need to import the eval package just for the
// constant identifier.
func PerRunMetadataMDHeadingFromEval() string { return eval.PerRunMetadataMDHeading }

// TestEvalRun_OrphanedBaselineEntryFails covers the §7.1.3 "orphaned
// baseline entry in an unfiltered run → exit 2" contract. Constructs
// an isolated fixtures dir so we don't mutate the committed baseline.
func TestEvalRun_OrphanedBaselineEntryFails(t *testing.T) {
	// Copy trivial-pass to a fresh temp dir and plant a baseline that
	// references a second fixture we do NOT copy over.
	src, err := filepath.Abs("../../testdata/eval/trivial-pass")
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	dst := filepath.Join(tmp, "trivial-pass")
	if err := copyTree(src, dst); err != nil {
		t.Fatal(err)
	}

	// Build a baseline with two entries; only one fixture exists.
	bl := &eval.Baseline{
		SchemaVersion:         eval.BaselineSchemaVersion,
		GeneratedAt:           "2026-04-18T00:00:00Z",
		ApertureVersion:       "test",
		SelectionLogicVersion: "sel-v2",
		Fixtures: map[string]eval.BaselineFixtureM{
			"trivial-pass":   {Precision: 1, Recall: 1, F1: 1},
			"does-not-exist": {Precision: 1, Recall: 1, F1: 1},
		},
	}
	blPath := filepath.Join(tmp, "baseline.json")
	if err := eval.WriteBaseline(blPath, bl); err != nil {
		t.Fatal(err)
	}

	root := NewRoot()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"eval", "run", "--fixtures", tmp, "--baseline", blPath, "--format", "markdown"})
	err = root.Execute()
	if err == nil {
		t.Fatalf("expected exit 2; stdout:\n%s", stdout.String())
	}
	var ec *ExitCodeError
	if !errors.As(err, &ec) {
		t.Fatalf("error is not ExitCodeError: %v", err)
	}
	if ec.Code != exitCodeBadArgs {
		t.Errorf("exit code = %d, want %d", ec.Code, exitCodeBadArgs)
	}
	if !strings.Contains(err.Error(), "does-not-exist") {
		t.Errorf("error should mention orphan fixture: %v", err)
	}
}

// TestEvalRun_DeterministicReportModuloPerRun runs the harness twice
// against the same committed fixtures and asserts the stripped reports
// are byte-identical. This verifies the §8.1 per-run contract: exactly
// the per_run_metadata section varies run-to-run.
func TestEvalRun_DeterministicReportModuloPerRun(t *testing.T) {
	fixturesDir, err := filepath.Abs("../../testdata/eval")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(fixturesDir, "baseline.json")); err != nil {
		t.Skip("committed baseline absent")
	}

	runOnce := func() []byte {
		t.Helper()
		root := NewRoot()
		var stdout bytes.Buffer
		root.SetOut(&stdout)
		root.SetArgs([]string{"eval", "run", "--fixtures", fixturesDir, "--format", "json"})
		if err := root.Execute(); err != nil {
			var ec *ExitCodeError
			if errors.As(err, &ec) && ec.Code == exitCodeBadArgs {
				// Regression vs. committed baseline — still return the body.
			} else if err != nil {
				t.Fatalf("eval run failed: %v", err)
			}
		}
		stripped, err := eval.StripPerRunJSON(stdout.Bytes())
		if err != nil {
			t.Fatalf("strip: %v", err)
		}
		return stripped
	}
	a := runOnce()
	b := runOnce()
	if !bytes.Equal(a, b) {
		t.Errorf("stripped eval reports differ across runs\nA:\n%s\nB:\n%s", a, b)
	}
}

// copyTree is a minimal recursive copy used by the orphan-baseline test.
func copyTree(src, dst string) error {
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := os.ReadFile(p) //nolint:gosec // test-local path
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	})
}
