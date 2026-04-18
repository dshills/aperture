// Package gaps implements the rule-based gap detector from SPEC §7.7.
// No rule depends on an LLM; all decisions are derived from task, index,
// and selection state.
package gaps

import (
	"sort"
	"strconv"
	"strings"

	"github.com/dshills/aperture/internal/index"
	"github.com/dshills/aperture/internal/manifest"
	"github.com/dshills/aperture/internal/selection"
	"github.com/dshills/aperture/internal/task"
)

// Inputs is the read-only snapshot every rule function operates on. All
// derivation happens once per plan; rules are pure.
type Inputs struct {
	Task           task.Task
	Index          *index.Index
	Assignments    []selection.Assignment
	Underflow      bool
	Demotions      map[string]string // selection path → demotion_reason
	BlockingConfig map[string]struct{}

	// Scores is the per-file relevance score across every scored file in
	// the index (not just selected files). Populated by the CLI pipeline
	// so ambiguousOwnership can inspect package peers that did not win a
	// selection slot — the §7.7.3 rule is scored against the package's
	// files, not just the final assignment list.
	Scores map[string]float64

	// exportedSymbols is a lazy-built, lowercased Go-symbol index used by
	// the unresolved_symbol_dependency rule. Populated by Engine once per
	// plan; rule functions read-only.
	exportedSymbols map[string]struct{}
}

// Engine runs every rule function in the §7.7.3 order and returns the
// final Gap slice. IDs are assigned `gap-1`, `gap-2`, … in emission
// order. §7.7.3 stipulates that severity may be upgraded to blocking
// when a rule's type is in gaps.blocking config; this engine applies
// that upgrade after rule evaluation but before ordering.
func Engine(in Inputs) []manifest.Gap {
	// Build the Go-symbol set once per plan so rule functions can do O(1)
	// lookups instead of rewalking Index.Files.
	if in.exportedSymbols == nil {
		in.exportedSymbols = buildExportedSymbolSet(in.Index)
	}

	var all []manifest.Gap

	all = append(all, missingSpec(in)...)
	all = append(all, missingTests(in)...)
	all = append(all, missingConfigContext(in)...)
	all = append(all, unresolvedSymbolDependency(in)...)
	all = append(all, ambiguousOwnership(in)...)
	all = append(all, missingRuntimePath(in)...)
	all = append(all, missingExternalContract(in)...)
	all = append(all, oversizedPrimaryContext(in)...)
	all = append(all, taskUnderspecified(in)...)

	// Apply gaps.blocking config upgrades.
	for i := range all {
		if _, ok := in.BlockingConfig[string(all[i].Type)]; ok {
			all[i].Severity = manifest.GapSeverityBlocking
		}
	}

	// Stable emission order: by type (§7.7.3 table order), then by
	// description for deterministic tie-break among multi-emission rules
	// like unresolved_symbol_dependency.
	sort.SliceStable(all, func(i, j int) bool {
		pi, pj := typePriority(all[i].Type), typePriority(all[j].Type)
		if pi != pj {
			return pi < pj
		}
		return all[i].Description < all[j].Description
	})
	for i := range all {
		all[i].ID = gapID(i + 1)
	}
	return all
}

// typePriority encodes the §7.7.3 table row order so gaps emit
// deterministically regardless of append order above.
func typePriority(t manifest.GapType) int {
	switch t {
	case manifest.GapMissingSpec:
		return 1
	case manifest.GapMissingTests:
		return 2
	case manifest.GapMissingConfigContext:
		return 3
	case manifest.GapUnresolvedSymbolDependency:
		return 4
	case manifest.GapAmbiguousOwnership:
		return 5
	case manifest.GapMissingRuntimePath:
		return 6
	case manifest.GapMissingExternalContract:
		return 7
	case manifest.GapOversizedPrimaryContext:
		return 8
	case manifest.GapTaskUnderspecified:
		return 9
	}
	return 99
}

// gapID returns the stable "gap-N" identifier without zero-padding, per
// §6.5's illustrative example ("gap-1", "gap-2").
func gapID(n int) string {
	return "gap-" + strconv.Itoa(n)
}

// hasGoFiles is a cheap helper used by the Go-dependent rules per the
// §7.7.3 round-7 suppression ("skip when the index has zero Go files").
func hasGoFiles(idx *index.Index) bool {
	for _, f := range idx.Files {
		if f.Language == "go" {
			return true
		}
	}
	return false
}

// buildExportedSymbolSet produces a lowercased set of every exported Go
// symbol name across the index. Run once per Engine invocation so the
// unresolved_symbol_dependency rule is O(anchors) after setup.
func buildExportedSymbolSet(idx *index.Index) map[string]struct{} {
	out := map[string]struct{}{}
	if idx == nil {
		return out
	}
	for _, f := range idx.Files {
		if f.Language != "go" {
			continue
		}
		for _, s := range f.Symbols {
			out[strings.ToLower(s.Name)] = struct{}{}
		}
	}
	return out
}
