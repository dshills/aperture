//go:build !notier2

package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/dshills/aperture/internal/config"
	"github.com/dshills/aperture/internal/eval"
	"github.com/dshills/aperture/internal/pipeline"
	"github.com/dshills/aperture/internal/repo"
	"github.com/dshills/aperture/internal/task"
	"github.com/dshills/aperture/internal/version"
)

// PLAN §11.3 mandates 20-run byte-identity for three artifacts:
// aperture eval run, aperture diff, scope-restricted plans. Each
// test below runs the respective CLI surface 20 times against
// identical inputs and asserts the stripped output is byte-equal
// across every run.
//
// Per-run noise (generated_at, pid, host, wall_clock_duration_ms)
// is stripped via the §8.1 per_run_metadata contract before the
// comparison.

// TestDeterminism_EvalRun_20Runs: PLAN §11.3 / §12-11.
func TestDeterminism_EvalRun_20Runs(t *testing.T) {
	fixturesDir, err := filepath.Abs("../../testdata/eval")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(fixturesDir, "baseline.json")); err != nil {
		t.Skip("committed baseline absent; skip determinism gate")
	}
	runOnce := func() []byte {
		t.Helper()
		root := NewRoot()
		var stdout bytes.Buffer
		root.SetOut(&stdout)
		root.SetErr(&stdout)
		root.SetArgs([]string{"eval", "run", "--fixtures", fixturesDir, "--format", "json"})
		if err := root.Execute(); err != nil {
			var ec *ExitCodeError
			if !errors.As(err, &ec) {
				t.Fatalf("eval run: %v", err)
			}
			// Exit 2 on regression would indicate a baseline
			// inconsistency; exit 0 is the happy path. In
			// either case the report body is still emitted.
		}
		stripped, err := eval.StripPerRunJSON(stdout.Bytes())
		if err != nil {
			t.Fatalf("strip: %v", err)
		}
		return stripped
	}
	ref := runOnce()
	for i := 1; i < 20; i++ {
		got := runOnce()
		if !bytes.Equal(ref, got) {
			t.Fatalf("eval run differs at iteration %d\nexpected %d bytes, got %d",
				i, len(ref), len(got))
		}
	}
}

// TestDeterminism_Diff_20Runs: PLAN §11.3 / §12-11.
func TestDeterminism_Diff_20Runs(t *testing.T) {
	base, err := filepath.Abs("../../testdata/fixtures/manifests")
	if err != nil {
		t.Fatal(err)
	}
	runOnce := func() []byte {
		t.Helper()
		root := NewRoot()
		var stdout bytes.Buffer
		root.SetOut(&stdout)
		root.SetErr(&stdout)
		root.SetArgs([]string{
			"diff",
			filepath.Join(base, "selection-diff", "a.json"),
			filepath.Join(base, "selection-diff", "b.json"),
			"--format", "json",
		})
		if err := root.Execute(); err != nil {
			t.Fatalf("aperture diff failed: %v", err)
		}
		return stdout.Bytes()
	}
	ref := runOnce()
	for i := 1; i < 20; i++ {
		got := runOnce()
		if !bytes.Equal(ref, got) {
			t.Fatalf("aperture diff differs at iteration %d", i)
		}
	}
}

// TestDeterminism_ScopeRestrictedPlan_20Runs: PLAN §11.3 scope
// projection stability. The scoped monorepo fixture is
// exercised in-process 20 times and the stripped manifest JSON
// is asserted byte-identical across every run.
func TestDeterminism_ScopeRestrictedPlan_20Runs(t *testing.T) {
	fixture, err := filepath.Abs("../../testdata/eval/monorepo-scope/repo")
	if err != nil {
		t.Fatal(err)
	}
	taskText := "Update Invoice.Finalize in services/billing/internal/invoice.go."
	cfg := config.Default()

	runOnce := func() []byte {
		t.Helper()
		parsed := task.Parse(taskText, task.ParseOptions{Source: "<inline>"})
		res, err := pipeline.Build(pipeline.BuildOptions{
			Root:              fixture,
			DefaultExcludes:   config.DefaultExclusions(),
			TypeScriptEnabled: true,
			JavaScriptEnabled: true,
			PythonEnabled:     true,
		})
		if err != nil {
			t.Fatal(err)
		}
		fp, err := repo.Fingerprint(walkerFiles(res.Index), version.Version)
		if err != nil {
			t.Fatal(err)
		}
		scope, err := repo.ResolveScope(fixture, "services/billing")
		if err != nil {
			t.Fatal(err)
		}
		m, err := BuildManifest(buildInputs{
			Config:      cfg,
			Task:        parsed,
			RepoRoot:    fixture,
			BudgetFlag:  200000,
			Fingerprint: fp,
			Languages:   res.Index.LanguageHints(),
			Exclusions:  res.Exclusions,
			Index:       res.Index,
			Scope:       scope,
		})
		if err != nil {
			var ec *ExitCodeError
			if errors.As(err, &ec) && ec.Code == exitCodeBudgetUnderflow {
				// Manifest still emitted under underflow;
				// deterministic comparison still meaningful.
			} else {
				t.Fatalf("BuildManifest: %v", err)
			}
		}
		// Strip the six per-run fields from §7.9.4 so the
		// byte-identity comparison only fires on a real
		// determinism violation.
		buf, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		stripped, err := stripPerRunManifestFields(buf)
		if err != nil {
			t.Fatal(err)
		}
		return stripped
	}
	ref := runOnce()
	for i := 1; i < 20; i++ {
		got := runOnce()
		if !bytes.Equal(ref, got) {
			t.Fatalf("scoped plan differs at iteration %d", i)
		}
	}
}

// stripPerRunManifestFields removes the six v1 §7.9.4 per-run
// fields (manifest_id, generated_at, and four generation_metadata
// entries including aperture_version) from a marshaled manifest
// so deterministic-suite tests can compare runs without false
// positives on timestamps / host names / PIDs.
//
// manifest_hash is deliberately KEPT in the stripped output: the
// hash input is the manifest-minus-exempt-fields (the same shape
// we're producing here), so a stable stripped body MUST produce a
// stable hash. Keeping the hash in the comparison turns
// "deterministic scoring" and "deterministic hashing" into a
// single assertion — any hash drift under identical stripped
// content is a bug we want the test to fail on.
func stripPerRunManifestFields(buf []byte) ([]byte, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(buf, &m); err != nil {
		return nil, err
	}
	delete(m, "manifest_id")
	delete(m, "generated_at")
	if gm, ok := m["generation_metadata"]; ok {
		var gmap map[string]json.RawMessage
		// Surface an unmarshal failure explicitly — a malformed
		// generation_metadata object would otherwise leave the
		// per-run fields in place and produce spurious
		// determinism-test failures.
		if err := json.Unmarshal(gm, &gmap); err != nil {
			return nil, err
		}
		delete(gmap, "host")
		delete(gmap, "pid")
		delete(gmap, "wall_clock_started_at")
		delete(gmap, "aperture_version")
		back, err := json.Marshal(gmap)
		if err != nil {
			return nil, err
		}
		m["generation_metadata"] = back
	}
	return json.Marshal(m)
}
