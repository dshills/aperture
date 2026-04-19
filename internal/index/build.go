package index

import (
	"path"
	"sort"
	"strings"
)

// isTestFile reports whether rel (repo-relative, forward-slash) looks
// like a v1.1 §7.3.3 test file. Extension-family scoped:
//
//   - Go (.go):  foo_test.go
//   - JS/TS:     foo.test.{ts,tsx,js,jsx,mjs,cjs} or foo.spec.{...}
//   - Python:    test_foo.py  OR  foo_test.py
func isTestFile(rel, ext string) bool {
	base := path.Base(rel)
	lext := strings.ToLower(ext)
	switch lext {
	case ".go":
		return strings.HasSuffix(base, "_test.go")
	case ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs":
		stem := strings.TrimSuffix(base, lext)
		return strings.HasSuffix(stem, ".test") || strings.HasSuffix(stem, ".spec")
	case ".py":
		return strings.HasPrefix(base, "test_") || strings.HasSuffix(base, "_test.py")
	}
	return false
}

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

// LinkTests populates the TestLinks bidirectional slices, scoped to
// each file's language family per §7.3.3:
//
//   - Go: foo.go ↔ foo_test.go
//   - JS/TS: foo.ts ↔ foo.test.ts / foo.spec.ts, with intra-family
//     extension priority when multiple production candidates exist
//     (.tsx > .ts > .mjs > .cjs > .jsx > .js). Cross-family links
//     are forbidden — a .ts test cannot link to a .py production
//     file even when the basename stems agree.
//   - Python: foo.py ↔ test_foo.py / foo_test.py
//
// Links are sorted and deduped on each file.
func LinkTests(idx *Index) {
	byPath := make(map[string]*FileEntry, len(idx.Files))
	for i := range idx.Files {
		byPath[idx.Files[i].Path] = &idx.Files[i]
	}

	links := map[string]map[string]struct{}{}
	addLink := func(a, b string) {
		if a == b {
			return
		}
		set, ok := links[a]
		if !ok {
			set = map[string]struct{}{}
			links[a] = set
		}
		set[b] = struct{}{}
	}

	linkGoTests(idx, byPath, addLink)
	linkJSTSTests(idx, byPath, addLink)
	linkPythonTests(idx, byPath, addLink)

	for p, set := range links {
		e, ok := byPath[p]
		if !ok {
			continue
		}
		out := make([]string, 0, len(set))
		for q := range set {
			out = append(out, q)
		}
		sort.Strings(out)
		e.TestLinks = out
	}
}

// linkGoTests — v1 behavior preserved verbatim: group foo.go and
// foo_test.go by (dir, stem) and reciprocally link every pair.
func linkGoTests(idx *Index, byPath map[string]*FileEntry, addLink func(string, string)) {
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
	for _, paths := range byKey {
		if len(paths) < 2 {
			continue
		}
		for _, p := range paths {
			if _, ok := byPath[p]; !ok {
				continue
			}
			for _, q := range paths {
				if q != p {
					addLink(p, q)
				}
			}
		}
	}
}

// jstsPriority defines the intra-family extension priority per
// §7.3.3. Lower index = higher priority.
var jstsPriority = []string{".tsx", ".ts", ".mjs", ".cjs", ".jsx", ".js"}

// linkJSTSTests handles the JS/TS family test-to-production linking.
// Tests are the `.test.<ext>` / `.spec.<ext>` files; production is
// everything else in the same directory whose basename stem matches.
// When multiple production candidates exist, the highest-priority
// extension wins (no ties — one extension per candidate).
func linkJSTSTests(idx *Index, byPath map[string]*FileEntry, addLink func(string, string)) {
	type fileInfo struct {
		Path string
		Ext  string
	}
	// dir+stem → [production candidates]
	productionByKey := map[string][]fileInfo{}
	tests := []fileInfo{}
	for _, f := range idx.Files {
		if !isJSTSExt(f.Extension) {
			continue
		}
		dir := path.Dir(f.Path)
		base := path.Base(f.Path)
		stem := strings.TrimSuffix(base, f.Extension)
		isTest := strings.HasSuffix(stem, ".test") || strings.HasSuffix(stem, ".spec")
		if isTest {
			// The stem without .test/.spec is what tests on the
			// production side.
			stem = strings.TrimSuffix(stem, ".test")
			stem = strings.TrimSuffix(stem, ".spec")
			tests = append(tests, fileInfo{Path: f.Path, Ext: f.Extension})
			_ = dir
			_ = stem
			continue
		}
		key := dir + "/" + stem
		productionByKey[key] = append(productionByKey[key], fileInfo{Path: f.Path, Ext: f.Extension})
	}

	for _, t := range tests {
		dir := path.Dir(t.Path)
		base := path.Base(t.Path)
		stem := strings.TrimSuffix(base, t.Ext)
		stem = strings.TrimSuffix(stem, ".test")
		stem = strings.TrimSuffix(stem, ".spec")
		key := dir + "/" + stem
		candidates := productionByKey[key]
		if len(candidates) == 0 {
			continue
		}
		// Pick the highest-priority extension.
		best := candidates[0]
		bestPri := jstsPriorityIndex(best.Ext)
		for _, c := range candidates[1:] {
			pri := jstsPriorityIndex(c.Ext)
			if pri < bestPri {
				best = c
				bestPri = pri
			}
		}
		if _, ok := byPath[t.Path]; !ok {
			continue
		}
		if _, ok := byPath[best.Path]; !ok {
			continue
		}
		addLink(t.Path, best.Path)
		addLink(best.Path, t.Path)
	}
}

func isJSTSExt(ext string) bool {
	switch ext {
	case ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs":
		return true
	}
	return false
}

func jstsPriorityIndex(ext string) int {
	for i, e := range jstsPriority {
		if e == ext {
			return i
		}
	}
	return len(jstsPriority) // unknown extension sorts last
}

// linkPythonTests handles the Python test-to-production pairing.
// A Python test is `test_<name>.py` or `<name>_test.py` (file-level),
// and it links to sibling `<name>.py` in the same directory.
func linkPythonTests(idx *Index, byPath map[string]*FileEntry, addLink func(string, string)) {
	for _, f := range idx.Files {
		if f.Extension != ".py" {
			continue
		}
		base := path.Base(f.Path)
		var stem string
		switch {
		case strings.HasPrefix(base, "test_"):
			stem = strings.TrimSuffix(strings.TrimPrefix(base, "test_"), ".py")
		case strings.HasSuffix(base, "_test.py"):
			stem = strings.TrimSuffix(base, "_test.py")
		default:
			continue
		}
		prod := path.Join(path.Dir(f.Path), stem+".py")
		if _, ok := byPath[prod]; !ok {
			continue
		}
		addLink(f.Path, prod)
		addLink(prod, f.Path)
	}
}
