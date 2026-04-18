package index

import (
	"path"
	"sort"
	"strings"
)

// PackageGrouping re-keys the Go file set by the directory they share.
// v1 treats that directory as the package's local "import path" (the
// external module prefix is recovered separately when needed); Phase 3's
// scorer uses both the directory and the package name for s_package
// matching.
func BuildPackages(idx *Index) {
	pkgs := map[string]*Package{}
	for _, f := range idx.Files {
		if f.Language != "go" {
			continue
		}
		dir := path.Dir(f.Path)
		if dir == "" || dir == "." {
			dir = "."
		}
		pkg, ok := pkgs[dir]
		if !ok {
			pkg = &Package{Directory: dir, ImportPath: dir, Files: []string{}}
			pkgs[dir] = pkg
		}
		pkg.Files = append(pkg.Files, f.Path)
	}
	for _, pkg := range pkgs {
		sort.Strings(pkg.Files)
	}
	idx.Packages = pkgs

	// Attach each file to its package for quick lookup. Package name in
	// this context is the directory key; callers who need the declared
	// package identifier can still read it from FileEntry.PackageName.
	for i := range idx.Files {
		if idx.Files[i].Language != "go" {
			continue
		}
		dir := path.Dir(idx.Files[i].Path)
		if dir == "" {
			dir = "."
		}
		idx.Files[i].Package = dir
	}
}

// LinkTests populates the TestLinks bidirectional slices: every foo.go
// that has a sibling foo_test.go in the same package — or vice versa —
// gets a reciprocal link. Links are sorted and deduped.
func LinkTests(idx *Index) {
	// O(1) path → *FileEntry lookups to avoid the O(N²) linear scans the
	// earlier implementation incurred on large repos.
	byPath := make(map[string]*FileEntry, len(idx.Files))
	for i := range idx.Files {
		byPath[idx.Files[i].Path] = &idx.Files[i]
	}

	byKey := map[string][]string{}
	for _, f := range idx.Files {
		if f.Language != "go" {
			continue
		}
		dir := path.Dir(f.Path)
		base := path.Base(f.Path)
		stem := strings.TrimSuffix(base, ".go")
		stem = strings.TrimSuffix(stem, "_test")
		key := dir + "/" + stem
		byKey[key] = append(byKey[key], f.Path)
	}

	// Accumulate reciprocal links into per-file sets, then flush to sorted
	// slices exactly once per file. Prior implementations sorted inside
	// the inner loop (O(K²·log K)) which this avoids.
	links := map[string]map[string]struct{}{}
	for _, paths := range byKey {
		if len(paths) < 2 {
			continue
		}
		for _, p := range paths {
			if _, ok := byPath[p]; !ok {
				continue
			}
			set, ok := links[p]
			if !ok {
				set = map[string]struct{}{}
				links[p] = set
			}
			for _, q := range paths {
				if q != p {
					set[q] = struct{}{}
				}
			}
		}
	}
	for p, set := range links {
		e := byPath[p]
		out := make([]string, 0, len(set))
		for q := range set {
			out = append(out, q)
		}
		sort.Strings(out)
		e.TestLinks = out
	}
}
