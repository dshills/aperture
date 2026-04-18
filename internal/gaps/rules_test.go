package gaps

import (
	"strconv"
	"strings"
	"testing"

	"github.com/dshills/aperture/internal/index"
	"github.com/dshills/aperture/internal/manifest"
	"github.com/dshills/aperture/internal/selection"
	"github.com/dshills/aperture/internal/task"
)

// mkInputs builds an Inputs value with the Index/Task preconfigured so
// individual rule tests stay short. It is NOT shared test fixture data —
// every caller customizes the fields that matter for its rule.
func mkInputs() Inputs {
	return Inputs{
		Task: task.Task{
			Type:    manifest.ActionTypeFeature,
			Anchors: []string{"alpha", "beta"},
		},
		Index: &index.Index{
			Files:             []index.FileEntry{},
			Packages:          map[string]*index.Package{},
			SupplementalFiles: map[index.SupplementalCategory][]string{},
		},
	}
}

func goFile(p string, symbols ...string) index.FileEntry {
	syms := make([]index.Symbol, 0, len(symbols))
	for _, s := range symbols {
		syms = append(syms, index.Symbol{Name: s, Kind: index.SymbolFunc})
	}
	return index.FileEntry{Path: p, Language: "go", Package: "pkg", Symbols: syms}
}

func TestRule_MissingSpec_Fires(t *testing.T) {
	in := mkInputs()
	in.Task.Type = manifest.ActionTypeFeature
	gaps := missingSpec(in)
	if len(gaps) != 1 || gaps[0].Type != manifest.GapMissingSpec {
		t.Fatalf("missingSpec should fire on feature with no SPEC.md; got %+v", gaps)
	}
}

func TestRule_MissingSpec_SuppressedWhenSpecPresent(t *testing.T) {
	in := mkInputs()
	in.Index.SupplementalFiles[index.SupSpec] = []string{"SPEC.md"}
	if got := missingSpec(in); len(got) != 0 {
		t.Fatalf("missingSpec should suppress when SPEC.md present: %+v", got)
	}
}

func TestRule_MissingTests_FiresWhenNoTestSelected(t *testing.T) {
	in := mkInputs()
	in.Task.Type = manifest.ActionTypeFeature
	in.Assignments = []selection.Assignment{{Path: "pkg/foo.go", Score: 0.80, LoadMode: manifest.LoadModeFull}}
	gaps := missingTests(in)
	if len(gaps) != 1 {
		t.Fatalf("missingTests should fire: %+v", gaps)
	}
}

func TestRule_MissingTests_SuppressedWhenTestSelected(t *testing.T) {
	in := mkInputs()
	in.Task.Type = manifest.ActionTypeFeature
	in.Assignments = []selection.Assignment{
		{Path: "pkg/foo.go", Score: 0.80, LoadMode: manifest.LoadModeFull},
		{Path: "pkg/foo_test.go", Score: 0.60, LoadMode: manifest.LoadModeStructuralSummary},
	}
	if got := missingTests(in); len(got) != 0 {
		t.Fatalf("missingTests should suppress: %+v", got)
	}
}

func TestRule_MissingConfigContext_FiresOnConfigAnchor(t *testing.T) {
	in := mkInputs()
	in.Task.Anchors = []string{"database", "token"}
	if got := missingConfigContext(in); len(got) != 1 {
		t.Fatalf("missingConfigContext should fire: %+v", got)
	}
}

func TestRule_MissingConfigContext_SuppressedWithConfigSelection(t *testing.T) {
	in := mkInputs()
	in.Task.Anchors = []string{"database"}
	in.Assignments = []selection.Assignment{{Path: "config/settings.yaml", Score: 0.60}}
	if got := missingConfigContext(in); len(got) != 0 {
		t.Fatalf("should suppress when config file selected: %+v", got)
	}
}

func TestRule_UnresolvedSymbolDependency_FiresForUnknownIdentifier(t *testing.T) {
	in := mkInputs()
	in.Index.Files = []index.FileEntry{goFile("pkg/existing.go", "Known")}
	in.Index.Packages["pkg"] = &index.Package{Directory: "pkg", Files: []string{"pkg/existing.go"}}
	in.Task.Anchors = []string{"NotDefined"}
	gaps := unresolvedSymbolDependency(in)
	if len(gaps) != 1 {
		t.Fatalf("should emit one gap: %+v", gaps)
	}
	if !strings.Contains(gaps[0].Description, "NotDefined") {
		t.Errorf("description should name the symbol: %s", gaps[0].Description)
	}
}

func TestRule_UnresolvedSymbolDependency_SuppressedOnNonGoRepo(t *testing.T) {
	in := mkInputs()
	in.Task.Anchors = []string{"Mystery"}
	// no Go files in index
	if got := unresolvedSymbolDependency(in); len(got) != 0 {
		t.Fatalf("should suppress on non-Go repo: %+v", got)
	}
}

func TestRule_UnresolvedSymbolDependency_CapsAt5(t *testing.T) {
	in := mkInputs()
	in.Index.Files = []index.FileEntry{goFile("pkg/x.go", "Known")}
	in.Task.Anchors = []string{"Aaa", "Bbb", "Ccc", "Ddd", "Eee", "Fff", "Ggg"}
	got := unresolvedSymbolDependency(in)
	if len(got) != 5 {
		t.Fatalf("rule must cap at 5 emissions, got %d", len(got))
	}
}

func TestRule_AmbiguousOwnership_FiresOnCrowdedPackage(t *testing.T) {
	in := mkInputs()
	in.Task.Type = manifest.ActionTypeBugfix
	f1 := goFile("pkg/a.go", "A")
	f2 := goFile("pkg/b.go", "B")
	f3 := goFile("pkg/c.go", "C")
	in.Index.Files = []index.FileEntry{f1, f2, f3}
	in.Index.Packages["pkg"] = &index.Package{Directory: "pkg", Files: []string{"pkg/a.go", "pkg/b.go", "pkg/c.go"}}
	in.Assignments = []selection.Assignment{
		{Path: "pkg/a.go", Score: 0.70, LoadMode: manifest.LoadModeFull},
		{Path: "pkg/b.go", Score: 0.65, LoadMode: manifest.LoadModeStructuralSummary},
		{Path: "pkg/c.go", Score: 0.62, LoadMode: manifest.LoadModeBehavioralSummary},
	}
	got := ambiguousOwnership(in)
	if len(got) != 1 || got[0].Severity != manifest.GapSeverityInfo {
		t.Fatalf("should emit single info gap: %+v", got)
	}
}

func TestRule_AmbiguousOwnership_SuppressedOnClearOwner(t *testing.T) {
	in := mkInputs()
	in.Task.Type = manifest.ActionTypeBugfix
	in.Index.Files = []index.FileEntry{goFile("pkg/a.go", "A"), goFile("pkg/b.go", "B")}
	in.Index.Packages["pkg"] = &index.Package{Directory: "pkg", Files: []string{"pkg/a.go", "pkg/b.go"}}
	in.Assignments = []selection.Assignment{
		{Path: "pkg/a.go", Score: 0.90, LoadMode: manifest.LoadModeFull},
		{Path: "pkg/b.go", Score: 0.65, LoadMode: manifest.LoadModeStructuralSummary},
	}
	if got := ambiguousOwnership(in); len(got) != 0 {
		t.Fatalf("should suppress on clear owner: %+v", got)
	}
}

func TestRule_MissingRuntimePath_FiresOnRequestAnchor(t *testing.T) {
	in := mkInputs()
	in.Task.Type = manifest.ActionTypeFeature
	in.Task.Anchors = []string{"request", "handler"}
	f := goFile("pkg/util.go", "Util") // no io:* tags
	in.Index.Files = []index.FileEntry{f}
	in.Assignments = []selection.Assignment{{Path: "pkg/util.go", Score: 0.80, LoadMode: manifest.LoadModeFull}}
	if got := missingRuntimePath(in); len(got) != 1 {
		t.Fatalf("should fire: %+v", got)
	}
}

func TestRule_MissingRuntimePath_SuppressedWithIOTag(t *testing.T) {
	in := mkInputs()
	in.Task.Type = manifest.ActionTypeFeature
	in.Task.Anchors = []string{"request"}
	f := goFile("pkg/net.go", "Handle")
	f.SideEffects = []string{"io:network"}
	in.Index.Files = []index.FileEntry{f}
	in.Assignments = []selection.Assignment{{Path: "pkg/net.go", Score: 0.80, LoadMode: manifest.LoadModeFull}}
	if got := missingRuntimePath(in); len(got) != 0 {
		t.Fatalf("should suppress when io:network present: %+v", got)
	}
}

func TestRule_MissingExternalContract_FiresOnAPIAnchor(t *testing.T) {
	in := mkInputs()
	in.Task.Anchors = []string{"openapi", "contract"}
	in.Assignments = []selection.Assignment{{Path: "pkg/util.go", Score: 0.80}}
	if got := missingExternalContract(in); len(got) != 1 {
		t.Fatalf("should fire: %+v", got)
	}
}

func TestRule_MissingExternalContract_SuppressedWithOpenAPISelection(t *testing.T) {
	in := mkInputs()
	in.Task.Anchors = []string{"openapi"}
	in.Assignments = []selection.Assignment{{Path: "api/openapi.yaml", Score: 0.80}}
	if got := missingExternalContract(in); len(got) != 0 {
		t.Fatalf("should suppress: %+v", got)
	}
}

func TestRule_OversizedPrimary_SilentWhenUnderflowOwnsIt(t *testing.T) {
	in := mkInputs()
	in.Underflow = true
	if got := oversizedPrimaryContext(in); len(got) != 0 {
		t.Fatalf("engine defers to selector-side underflow gap: %+v", got)
	}
}

func TestRule_OversizedPrimary_FiresOnDemotion(t *testing.T) {
	in := mkInputs()
	in.Demotions = map[string]string{"pkg/huge.go": "size_band=large"}
	got := oversizedPrimaryContext(in)
	if len(got) != 1 || got[0].Severity != manifest.GapSeverityWarning {
		t.Fatalf("demotion should emit warning: %+v", got)
	}
}

func TestRule_TaskUnderspecified_FiresWhenMaxScoreBelowThreshold(t *testing.T) {
	in := mkInputs()
	in.Assignments = []selection.Assignment{{Path: "pkg/foo.go", Score: 0.45, LoadMode: manifest.LoadModeFull}}
	if got := taskUnderspecified(in); len(got) != 1 {
		t.Fatalf("should fire when max score <0.60: %+v", got)
	}
}

func TestRule_TaskUnderspecified_DoesNotFireWhenMaxScoreAbove(t *testing.T) {
	in := mkInputs()
	in.Assignments = []selection.Assignment{{Path: "pkg/foo.go", Score: 0.75, LoadMode: manifest.LoadModeFull}}
	if got := taskUnderspecified(in); len(got) != 0 {
		t.Fatalf("should not fire when max score ≥0.60: %+v", got)
	}
}

// Anchors<2 and action=unknown are already covered by feasibility's
// task_specificity sub-signal; the rule must NOT double-count them here.
func TestRule_TaskUnderspecified_AnchorsAndActionAloneDoNotFire(t *testing.T) {
	in := mkInputs()
	in.Task.Type = manifest.ActionTypeUnknown
	in.Task.Anchors = []string{"only"}
	in.Assignments = []selection.Assignment{{Path: "pkg/foo.go", Score: 0.80, LoadMode: manifest.LoadModeFull}}
	if got := taskUnderspecified(in); len(got) != 0 {
		t.Fatalf("anchors/action triggers alone must not fire this rule: %+v", got)
	}
}

func TestEngine_AssignsStableGapIDs(t *testing.T) {
	in := mkInputs()
	in.Task.Type = manifest.ActionTypeFeature
	in.Task.Anchors = []string{"database"}
	// Feature + no spec + no tests + config anchor → at least 3 gaps.
	got := Engine(in)
	if len(got) < 2 {
		t.Fatalf("expected multiple gaps, got %+v", got)
	}
	for i, g := range got {
		want := "gap-" + strconv.Itoa(i+1)
		if g.ID != want {
			t.Errorf("gap %d has ID %q, want %q", i, g.ID, want)
		}
	}
}

func TestEngine_BlockingConfigUpgradesSeverity(t *testing.T) {
	in := mkInputs()
	in.Task.Type = manifest.ActionTypeFeature
	in.BlockingConfig = map[string]struct{}{string(manifest.GapMissingSpec): {}}
	got := Engine(in)
	if len(got) == 0 {
		t.Fatal("expected at least missing_spec gap")
	}
	var found bool
	for _, g := range got {
		if g.Type == manifest.GapMissingSpec {
			found = true
			if g.Severity != manifest.GapSeverityBlocking {
				t.Errorf("expected blocking upgrade, got %s", g.Severity)
			}
		}
	}
	if !found {
		t.Error("missing_spec gap not present")
	}
}
