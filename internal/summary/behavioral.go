package summary

import (
	"fmt"
	"sort"
	"strings"

	"github.com/dshills/aperture/internal/index"
	"github.com/dshills/aperture/internal/loadmode"
)

// Behavioral renders the §12.2 deterministic behavioral summary. No
// natural-language "responsibilities" — just mechanical facts: imports,
// side-effect tags, exported API surface (for Go), associated test file,
// size band, and estimated token cost. For non-Go files the summary
// still includes path + size band + any available tags (empty set in v1).
func Behavioral(f *index.FileEntry, estimatedTokensFull int) string {
	if f == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# behavioral summary: %s\n", f.Path)
	fmt.Fprintf(&b, "size_band: %s\n", loadmode.ClassifySize(f.Size, estimatedTokensFull))
	fmt.Fprintf(&b, "size_bytes: %d\n", f.Size)
	fmt.Fprintf(&b, "estimated_tokens_full: %d\n", estimatedTokensFull)

	if f.Language != "" {
		fmt.Fprintf(&b, "language: %s\n", f.Language)
	}
	if f.ParseError {
		b.WriteString("parse_error: true\n")
	}

	if len(f.Imports) > 0 {
		b.WriteString("imports:\n")
		imports := append([]string{}, f.Imports...)
		sort.Strings(imports)
		for _, imp := range imports {
			fmt.Fprintf(&b, "  - %s\n", imp)
		}
	}
	if len(f.SideEffects) > 0 {
		b.WriteString("side_effects:\n")
		tags := append([]string{}, f.SideEffects...)
		sort.Strings(tags)
		for _, t := range tags {
			fmt.Fprintf(&b, "  - %s\n", t)
		}
	}
	if f.Language == "go" && len(f.Symbols) > 0 {
		b.WriteString("exported_api:\n")
		syms := append([]index.Symbol{}, f.Symbols...)
		sort.Slice(syms, func(i, j int) bool {
			if syms[i].Kind != syms[j].Kind {
				return syms[i].Kind < syms[j].Kind
			}
			return syms[i].Name < syms[j].Name
		})
		for _, s := range syms {
			if s.Receiver != "" {
				fmt.Fprintf(&b, "  - %s %s.%s\n", s.Kind, s.Receiver, s.Name)
			} else {
				fmt.Fprintf(&b, "  - %s %s\n", s.Kind, s.Name)
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
