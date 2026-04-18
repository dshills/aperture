package goanalysis

import (
	"sort"
	"strings"

	"github.com/dshills/aperture/internal/index"
)

// SymbolTable is a lowercase-keyed, repo-wide index of exported Go
// symbols. Phase 4's `unresolved_symbol_dependency` gap rule queries it
// case-insensitively; keeping the lookup explicit avoids repeated
// re-walks of the file list.
type SymbolTable struct {
	byName map[string][]string
}

// BuildSymbolTable assembles a SymbolTable from the Go files in idx.
// Paths are kept sorted and deduped per key.
func BuildSymbolTable(idx *index.Index) *SymbolTable {
	raw := map[string]map[string]struct{}{}

	for _, f := range idx.Files {
		if f.Language != "go" {
			continue
		}
		for _, s := range f.Symbols {
			key := strings.ToLower(s.Name)
			if raw[key] == nil {
				raw[key] = map[string]struct{}{}
			}
			raw[key][f.Path] = struct{}{}
		}
	}

	byName := make(map[string][]string, len(raw))
	for name, set := range raw {
		list := make([]string, 0, len(set))
		for p := range set {
			list = append(list, p)
		}
		sort.Strings(list)
		byName[name] = list
	}
	return &SymbolTable{byName: byName}
}

// Lookup returns the sorted list of file paths exporting a symbol whose
// lowercased name matches lower(name). Returns nil when none match.
func (st *SymbolTable) Lookup(name string) []string {
	if st == nil {
		return nil
	}
	return st.byName[strings.ToLower(name)]
}

// Has reports whether any file exports a symbol matching name
// case-insensitively.
func (st *SymbolTable) Has(name string) bool {
	return len(st.Lookup(name)) > 0
}
