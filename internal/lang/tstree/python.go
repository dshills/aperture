//go:build !notier2

package tstree

import (
	"sort"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/dshills/aperture/internal/index"
)

// extractPython walks the top-level children of a Python module node
// and emits module-level symbol and import records per §7.3.2.
//
// Rules (§7.3.2):
//   - `def` / `async def` → SymbolFunc. Exported = !strings.HasPrefix(name,"_").
//   - `class` → SymbolType. Same Exported rule.
//   - Module-level assignment `NAME = RHS` where the target is a
//     single identifier → SymbolFunc when RHS is a direct `lambda`,
//     else SymbolVar. Same Exported rule.
//   - `import X` records the dotted module name X.
//   - `from X import a, b, c` records module name X once; `a, b, c`
//     are NOT separate imports in v1.1.
//   - Relative imports preserve the literal dot-prefixed string.
//   - Nested imports / imports inside conditionals are NOT recorded.
func extractPython(root *sitter.Node, src []byte) ([]index.Symbol, []string) {
	var symbols []index.Symbol
	importSet := map[string]struct{}{}

	for i := 0; i < int(root.NamedChildCount()); i++ {
		child := root.NamedChild(i)
		if child == nil {
			continue
		}
		switch child.Type() {
		case "function_definition":
			symbols = append(symbols, pyFuncSymbol(child, src))
		case "decorated_definition":
			// @decorator\ndef foo(): ...
			def := child.ChildByFieldName("definition")
			if def != nil {
				switch def.Type() {
				case "function_definition":
					symbols = append(symbols, pyFuncSymbol(def, src))
				case "class_definition":
					symbols = append(symbols, pyClassSymbol(def, src))
				}
			}
		case "class_definition":
			symbols = append(symbols, pyClassSymbol(child, src))
		case "expression_statement":
			// Module-level assignment: a single-target identifier
			// with a possibly-lambda RHS.
			if sym, ok := pyAssignSymbol(child, src); ok {
				symbols = append(symbols, sym)
			}
		case "import_statement":
			for _, name := range pyImportNames(child, src) {
				importSet[name] = struct{}{}
			}
		case "import_from_statement":
			if module := pyFromImportModule(child, src); module != "" {
				importSet[module] = struct{}{}
			}
		}
	}

	// Filter out zero-name entries defensively.
	filtered := make([]index.Symbol, 0, len(symbols))
	for _, s := range symbols {
		if s.Name != "" {
			filtered = append(filtered, s)
		}
	}

	imports := make([]string, 0, len(importSet))
	for s := range importSet {
		imports = append(imports, s)
	}
	// Sort so the output is deterministic across runs (Go map
	// iteration is randomized).
	sort.Strings(imports)
	return filtered, imports
}

func pyFuncSymbol(node *sitter.Node, src []byte) index.Symbol {
	name := nodeText(node.ChildByFieldName("name"), src)
	return index.Symbol{Name: name, Kind: index.SymbolFunc, Exported: !strings.HasPrefix(name, "_")}
}

func pyClassSymbol(node *sitter.Node, src []byte) index.Symbol {
	name := nodeText(node.ChildByFieldName("name"), src)
	return index.Symbol{Name: name, Kind: index.SymbolType, Exported: !strings.HasPrefix(name, "_")}
}

// pyAssignSymbol recognizes the simplest module-level assignment:
// `NAME = RHS`. Returns (Symbol, true) only when the left side is a
// single identifier. RHS kind classification:
//
//   - direct `lambda` → SymbolFunc
//   - anything else (call, literal, identifier, …) → SymbolVar
func pyAssignSymbol(stmt *sitter.Node, src []byte) (index.Symbol, bool) {
	if stmt.NamedChildCount() != 1 {
		return index.Symbol{}, false
	}
	child := stmt.NamedChild(0)
	if child == nil || child.Type() != "assignment" {
		return index.Symbol{}, false
	}
	target := child.ChildByFieldName("left")
	if target == nil || target.Type() != "identifier" {
		return index.Symbol{}, false
	}
	name := nodeText(target, src)
	kind := index.SymbolVar
	rhs := child.ChildByFieldName("right")
	if rhs != nil && rhs.Type() == "lambda" {
		kind = index.SymbolFunc
	}
	return index.Symbol{Name: name, Kind: kind, Exported: !strings.HasPrefix(name, "_")}, true
}

// pyImportNames handles `import A`, `import A.B`, `import A as B`,
// `import A, B`. Returns the dotted module names exactly as written.
func pyImportNames(node *sitter.Node, src []byte) []string {
	var out []string
	for i := 0; i < int(node.NamedChildCount()); i++ {
		n := node.NamedChild(i)
		if n == nil {
			continue
		}
		switch n.Type() {
		case "dotted_name":
			out = append(out, nodeText(n, src))
		case "aliased_import":
			if name := n.ChildByFieldName("name"); name != nil {
				out = append(out, nodeText(name, src))
			}
		}
	}
	return out
}

// pyFromImportModule extracts X from `from X import ...`. Relative
// imports (`from . import y`, `from ..util import z`) keep the
// literal dot-prefixed string.
func pyFromImportModule(node *sitter.Node, src []byte) string {
	mod := node.ChildByFieldName("module_name")
	if mod != nil {
		return nodeText(mod, src)
	}
	// Relative import: the grammar represents leading dots as
	// unnamed "." / ".." children. We reconstruct the textual
	// prefix by scanning children up to the "import" keyword.
	var dots strings.Builder
	for i := 0; i < int(node.ChildCount()); i++ {
		c := node.Child(i)
		if c == nil {
			continue
		}
		t := c.Type()
		if t == "." {
			dots.WriteByte('.')
			continue
		}
		if t == "import" {
			break
		}
		if t == "dotted_name" || t == "relative_import" {
			// Name suffix on a relative import (e.g. `from ..util import`)
			return dots.String() + nodeText(c, src)
		}
	}
	if dots.Len() > 0 {
		return dots.String()
	}
	return ""
}
