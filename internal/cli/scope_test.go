package cli

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/dshills/aperture/internal/config"
	"github.com/dshills/aperture/internal/manifest"
	"github.com/dshills/aperture/internal/pipeline"
	"github.com/dshills/aperture/internal/repo"
	"github.com/dshills/aperture/internal/task"
	"github.com/dshills/aperture/internal/version"
)

// buildMonorepoInputs wires the monorepo fixture to buildInputs for
// direct BuildManifest invocation — matches the pattern used by
// buildFixtureInputs in plan_test.go.
func buildMonorepoInputs(t *testing.T, rawTask, scopeIn string) buildInputs {
	t.Helper()
	fixture, err := filepath.Abs("../../testdata/eval/monorepo-scope/repo")
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
	var scope repo.Scope
	if scopeIn != "" {
		scope, err = repo.ResolveScope(fixture, scopeIn)
		if err != nil {
			t.Fatalf("ResolveScope: %v", err)
		}
	}
	return buildInputs{
		Config:      cfg,
		Task:        parsed,
		RepoRoot:    fixture,
		ModelFlag:   "",
		BudgetFlag:  200000,
		Fingerprint: fp,
		Languages:   res.Index.LanguageHints(),
		Exclusions:  res.Exclusions,
		Index:       res.Index,
		Scope:       scope,
	}
}

func TestScope_ManifestFieldPresentAndHashFolds(t *testing.T) {
	taskText := "Update Invoice.Finalize in services/billing/internal/invoice.go."
	scopedInputs := buildMonorepoInputs(t, taskText, "services/billing")
	unscopedInputs := buildMonorepoInputs(t, taskText, "")

	scoped, err := BuildManifest(scopedInputs)
	if err != nil {
		t.Fatalf("BuildManifest(scoped): %v", err)
	}
	unscoped, err := BuildManifest(unscopedInputs)
	if err != nil {
		t.Fatalf("BuildManifest(unscoped): %v", err)
	}

	if scoped.Scope == nil || scoped.Scope.Path != "services/billing" {
		t.Errorf("scoped manifest Scope=%+v, want path=services/billing", scoped.Scope)
	}
	if unscoped.Scope != nil {
		t.Errorf("unscoped manifest should have no Scope, got %+v", unscoped.Scope)
	}
	if scoped.ManifestHash == unscoped.ManifestHash {
		t.Errorf("scoped and unscoped manifests share hash %s — scope must fold in", scoped.ManifestHash)
	}
}

func TestScope_SelectionsStayInsideSubtree(t *testing.T) {
	taskText := "Update Invoice.Finalize in services/billing/internal/invoice.go."
	in := buildMonorepoInputs(t, taskText, "services/billing")
	m, err := BuildManifest(in)
	if err != nil {
		t.Fatalf("BuildManifest: %v", err)
	}
	for _, s := range m.Selections {
		insideScope := strings.HasPrefix(s.Path, "services/billing/") || s.Path == "services/billing"
		// A selection may also be an admitted supplemental; those
		// carry the "outside_scope_supplemental" rationale token.
		isSupplemental := slices.Contains(s.Rationale, "outside_scope_supplemental")
		if !insideScope && !isSupplemental {
			t.Errorf("selection %q is outside scope and not a supplemental: rationale=%v",
				s.Path, s.Rationale)
		}
	}
}

// TestScope_MonorepoSiblingExcluded: with scope set to
// `services/billing`, the rival `services/ingest/internal/invoice.go`
// must not appear in selections or reachable even though it shares the
// Invoice symbol name.
func TestScope_MonorepoSiblingExcluded(t *testing.T) {
	taskText := "Update Invoice in services/billing. Fix Finalize."
	in := buildMonorepoInputs(t, taskText, "services/billing")
	m, err := BuildManifest(in)
	if err != nil {
		t.Fatalf("BuildManifest: %v", err)
	}
	forbidden := "services/ingest/internal/invoice.go"
	for _, s := range m.Selections {
		if s.Path == forbidden {
			t.Errorf("scoped plan must not select %s: %+v", forbidden, s)
		}
	}
	for _, r := range m.Reachable {
		if r.Path == forbidden {
			t.Errorf("scoped plan must not surface %s in reachable", forbidden)
		}
	}
}

// TestScope_InvalidPathReturnsExit4: resolveScope's failure propagates
// as exit code 4 to the caller.
func TestScope_InvalidPathReturnsExit4(t *testing.T) {
	fixture, err := filepath.Abs("../../testdata/eval/monorepo-scope/repo")
	if err != nil {
		t.Fatal(err)
	}
	_, err = resolveScope(fixture, config.Default(), "../escape", true)
	var ec *ExitCodeError
	if !errors.As(err, &ec) {
		t.Fatalf("expected ExitCodeError, got %v", err)
	}
	if ec.Code != exitCodeBadRepo {
		t.Errorf("exit code = %d, want %d", ec.Code, exitCodeBadRepo)
	}
}

// TestScope_SentinelUnsetsConfigScope: a CLI sentinel ("" or ".") MUST
// override a config-declared defaults.scope.
func TestScope_SentinelUnsetsConfigScope(t *testing.T) {
	fixture, err := filepath.Abs("../../testdata/eval/monorepo-scope/repo")
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Defaults.Scope = "services/billing"
	// Simulate `--scope ""` (flagSet=true, value="").
	s, err := resolveScope(fixture, cfg, "", true)
	if err != nil {
		t.Fatalf("sentinel should unset, got err %v", err)
	}
	if s.IsSet() {
		t.Errorf("expected unset scope, got %+v", s)
	}
}

// TestScope_ZeroCandidatesAndNoSupplementalsExits9 exercises the
// §7.4.6 / §7.7 "scope leaves zero planable candidates AND no
// supplemental admits as a candidate" branch — reuses v1 exit 9.
func TestScope_ZeroCandidatesAndNoSupplementalsExits9(t *testing.T) {
	// Build a throwaway repo with ONLY a scope subtree and NO
	// supplementals. Scoping into a sibling that doesn't exist would
	// be rejected at ResolveScope; to exercise the "zero in-scope
	// files" exit-9 branch we scope into a real empty subtree.
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	// One file outside the scope, which is NOT a supplemental (not
	// named SPEC.md/README.md/etc.), so it cannot admit under §7.4.2.
	if err := os.WriteFile(filepath.Join(root, "other.go"), []byte("package other\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	scope, err := repo.ResolveScope(root, "empty")
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	parsed := task.Parse("placeholder", task.ParseOptions{Source: "<inline>"})
	res, err := pipeline.Build(pipeline.BuildOptions{
		Root:            root,
		DefaultExcludes: config.DefaultExclusions(),
	})
	if err != nil {
		t.Fatal(err)
	}
	fp, _ := repo.Fingerprint(walkerFiles(res.Index), version.Version)
	_, err = BuildManifest(buildInputs{
		Config:      cfg,
		Task:        parsed,
		RepoRoot:    root,
		BudgetFlag:  200000,
		Fingerprint: fp,
		Languages:   res.Index.LanguageHints(),
		Exclusions:  res.Exclusions,
		Index:       res.Index,
		Scope:       scope,
	})
	var ec *ExitCodeError
	if !errors.As(err, &ec) {
		t.Fatalf("expected ExitCodeError, got %v", err)
	}
	if ec.Code != exitCodeBudgetUnderflow {
		t.Errorf("exit code = %d, want %d (budget underflow / zero in-scope)", ec.Code, exitCodeBudgetUnderflow)
	}
}

// TestScope_SchemaValidatesScopedManifest: the v1.1 additive `scope`
// field passes the manifest JSON Schema.
func TestScope_SchemaValidatesScopedManifest(t *testing.T) {
	in := buildMonorepoInputs(t, "Touch Invoice.Finalize", "services/billing")
	m, err := BuildManifest(in)
	if err != nil {
		t.Fatalf("BuildManifest: %v", err)
	}
	buf, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	// Round-trip through the schema validator.
	if err := manifest.Validate(buf); err != nil {
		t.Errorf("scoped manifest failed schema validation: %v", err)
	}
}
