// Package index holds the canonical repository index assembled from a
// walked file set and per-language analyzers. Every slice and map inside
// is expected to be deterministically ordered before downstream consumers
// (scorer, selector, manifest emitter) read it.
package index

import (
	"sort"

	"github.com/dshills/aperture/internal/repo"
)

// Index is the repo-wide structure assembled by Phase 2. Later phases
// attach scoring, summaries, and selection state; this type stays a plain
// data bag.
type Index struct {
	Files             []FileEntry
	Packages          map[string]*Package
	SupplementalFiles map[SupplementalCategory][]string
}

// FileEntry carries per-file metadata plus Go-specific AST output when
// applicable. Non-Go entries leave the AST fields zero-valued.
type FileEntry struct {
	Path      string
	Size      int64
	SHA256    string
	MTime     string
	Extension string
	Language  string

	// Package is the resolved import path for Go files, keyed into the
	// Index.Packages map. Empty for non-Go files.
	Package string
	// PackageName is the declared package identifier (e.g., "goanalysis").
	PackageName string
	// Imports is the deduplicated, sorted import path list.
	Imports []string
	// Symbols is the file's exported-symbol set in source order.
	Symbols []Symbol
	// SideEffects is the deduped sorted side-effect tag list derived from
	// Imports via the §12.2 tables. Non-Go files carry no tags.
	SideEffects []string
	// TestLinks are paths of test files associated with this file (for
	// production files) or production files associated with this test
	// file. Bidirectional.
	TestLinks []string
	// IsTest is true for Go files named *_test.go.
	IsTest bool
	// ParseError is true when the Go AST parse failed and the file fell
	// back to the minimal import scan (SPEC §7.2.3).
	ParseError bool
}

// SymbolKind enumerates the exported-symbol classes v1 extracts.
type SymbolKind string

const (
	SymbolType      SymbolKind = "type"
	SymbolInterface SymbolKind = "interface"
	SymbolFunc      SymbolKind = "func"
	SymbolMethod    SymbolKind = "method"
	SymbolConst     SymbolKind = "const"
	SymbolVar       SymbolKind = "var"
)

// Symbol is one exported declaration from a Go file.
type Symbol struct {
	Name     string
	Kind     SymbolKind
	Receiver string // only set when Kind == SymbolMethod
}

// Package groups the files that share an import path. Files within a
// package are sorted ascending by path; the import-path "directory" is the
// normalized repo-relative directory containing the files.
type Package struct {
	ImportPath string
	Directory  string
	Files      []string
}

// FromWalk seeds an Index from a repo.Walk result. AST-derived fields are
// filled in by the goanalysis package in a second pass.
func FromWalk(w repo.WalkResult) *Index {
	files := make([]FileEntry, 0, len(w.Files))
	for _, f := range w.Files {
		files = append(files, FileEntry{
			Path:      f.Path,
			Size:      f.Size,
			SHA256:    f.SHA256,
			MTime:     f.MTime,
			Extension: f.Extension,
			Language:  f.Language,
			IsTest:    f.Extension == ".go" && hasSuffix(f.Path, "_test.go"),
		})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return &Index{
		Files:             files,
		Packages:          map[string]*Package{},
		SupplementalFiles: map[SupplementalCategory][]string{},
	}
}

// LanguageHints returns the sorted, deduped list of distinct language tags
// carried by the indexed files. Manifest §11.1 records this as
// `repo.language_hints`.
func (idx *Index) LanguageHints() []string {
	set := map[string]struct{}{}
	for _, f := range idx.Files {
		if f.Language != "" {
			set[f.Language] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// File looks up an entry by normalized path. Returns nil when absent.
func (idx *Index) File(path string) *FileEntry {
	for i := range idx.Files {
		if idx.Files[i].Path == path {
			return &idx.Files[i]
		}
	}
	return nil
}

func hasSuffix(s, suf string) bool {
	return len(s) >= len(suf) && s[len(s)-len(suf):] == suf
}
