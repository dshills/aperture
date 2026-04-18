package cli

import (
	"github.com/dshills/aperture/internal/index"
	"github.com/dshills/aperture/internal/repo"
)

// applyScopeToIndex returns a NEW *index.Index that projects the input
// index under the v1.1 §7.4.1 / §7.4.2 scope. The input is not
// mutated — neither its slice headers nor its map entries — so the
// caller can safely reuse or share the original index after this call.
//
// Semantics:
//   - Files whose repo-relative path is NOT under `scope.Path` are
//     excluded UNLESS they are supplemental files.
//   - Out-of-scope supplemental files are retained with
//     `OutOfScope = true` so the scorer applies the restricted signal
//     set (§7.4.2 table: s_symbol, s_import, s_package, s_test forced
//     to 0; s_mention, s_filename, s_doc, s_config unchanged).
//   - idx.Packages is rebuilt so only in-scope package directories
//     remain. `ambiguous_ownership` (§7.7.3) inspects this map for
//     peer count — restricting it here is exactly the §7.4.1
//     "ownership considers only in-scope files" requirement.
//
// Every other field (SupplementalFiles, etc.) is forwarded verbatim
// from the input; they already span the whole repo per §7.4.3 /
// §7.4.5 and remain available to the gap engine's whole-tree rules
// (missing_spec, missing_external_contract, …).
//
// Callers gate on scope.IsSet() before invoking.
func applyScopeToIndex(idx *index.Index, scope repo.Scope) *index.Index {
	supplemental := collectSupplementalPaths(idx)

	kept := make([]index.FileEntry, 0, len(idx.Files))
	for i := range idx.Files {
		f := idx.Files[i]
		f.OutOfScope = false
		switch {
		case scope.Contains(f.Path):
			kept = append(kept, f)
		case supplemental[f.Path]:
			// §7.4.2: admit as restricted-signal supplemental.
			f.OutOfScope = true
			kept = append(kept, f)
		default:
			// Excluded from the candidate pool entirely.
		}
	}

	newPackages := make(map[string]*index.Package, len(idx.Packages))
	for dir, pkg := range idx.Packages {
		survivors := make([]string, 0, len(pkg.Files))
		for _, p := range pkg.Files {
			if scope.Contains(p) {
				survivors = append(survivors, p)
			}
		}
		if len(survivors) == 0 {
			continue
		}
		scopedPkg := *pkg // shallow copy; callers keep their original
		scopedPkg.Files = survivors
		newPackages[dir] = &scopedPkg
	}

	return &index.Index{
		Files:             kept,
		Packages:          newPackages,
		SupplementalFiles: idx.SupplementalFiles, // whole-repo per §7.4.3 / §7.4.5
	}
}

// collectSupplementalPaths flattens idx.SupplementalFiles into a set of
// repo-relative paths. Used by applyScopeToIndex to admit out-of-scope
// supplementals under restricted scoring.
func collectSupplementalPaths(idx *index.Index) map[string]bool {
	out := make(map[string]bool)
	for _, paths := range idx.SupplementalFiles {
		for _, p := range paths {
			out[p] = true
		}
	}
	return out
}
