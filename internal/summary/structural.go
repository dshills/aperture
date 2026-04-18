// Package summary renders the deterministic §12.1 structural and §12.2
// behavioral summary bodies. No LLM, no natural-language synthesis — both
// forms are mechanical projections of the indexed symbol table and import
// list. v1 forbids LLM-driven summaries per §12.3.
package summary

import (
	"fmt"
	"sort"
	"strings"

	"github.com/dshills/aperture/internal/index"
)

// Structural renders a §12.1 structural summary for a Go file. Non-Go
// files are not eligible for structural summaries in v1 — callers should
// route them to Behavioral instead.
func Structural(f *index.FileEntry) string {
	if f == nil || f.Language != "go" {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# structural summary: %s\n", f.Path)
	if f.PackageName != "" {
		fmt.Fprintf(&b, "package %s\n", f.PackageName)
	}
	if len(f.Imports) > 0 {
		b.WriteString("imports:\n")
		for _, imp := range f.Imports {
			fmt.Fprintf(&b, "  - %s\n", imp)
		}
	}
	// Group exported symbols by kind in a stable order.
	kinds := []index.SymbolKind{
		index.SymbolType,
		index.SymbolInterface,
		index.SymbolFunc,
		index.SymbolMethod,
		index.SymbolConst,
		index.SymbolVar,
	}
	bucket := map[index.SymbolKind][]index.Symbol{}
	for _, s := range f.Symbols {
		bucket[s.Kind] = append(bucket[s.Kind], s)
	}
	for _, k := range kinds {
		entries := bucket[k]
		if len(entries) == 0 {
			continue
		}
		sort.Slice(entries, func(i, j int) bool {
			if entries[i].Name != entries[j].Name {
				return entries[i].Name < entries[j].Name
			}
			return entries[i].Receiver < entries[j].Receiver
		})
		fmt.Fprintf(&b, "%s:\n", k)
		for _, s := range entries {
			if s.Receiver != "" {
				fmt.Fprintf(&b, "  - %s.%s\n", s.Receiver, s.Name)
			} else {
				fmt.Fprintf(&b, "  - %s\n", s.Name)
			}
		}
	}
	if len(f.TestLinks) > 0 {
		b.WriteString("tests:\n")
		for _, tl := range f.TestLinks {
			fmt.Fprintf(&b, "  - %s\n", tl)
		}
	}
	return b.String()
}
