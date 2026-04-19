//go:build !notier2

package tstree

import (
	"sort"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/dshills/aperture/internal/index"
)

// extractTSJS walks the children of a TS/TSX/JS program node and
// emits module-level symbol and import records per §7.3.2. It DOES
// NOT recurse — nested declarations are intentionally skipped.
//
// The order of emission is source order within the program body;
// callers that need deterministic global ordering should sort the
// result. We keep source order here because it maps 1:1 to the
// module-structural view and matches the Go analyzer's output.
func extractTSJS(root *sitter.Node, src []byte, lang Lang) ([]index.Symbol, []string) {
	var symbols []index.Symbol
	importSet := map[string]struct{}{}

	for i := 0; i < int(root.NamedChildCount()); i++ {
		child := root.NamedChild(i)
		if child == nil {
			continue
		}
		addSymbol := func(s index.Symbol) {
			if s.Name != "" {
				symbols = append(symbols, s)
			}
		}
		addImport := func(spec string) {
			if spec != "" {
				importSet[spec] = struct{}{}
			}
		}
		switch child.Type() {
		case "export_statement":
			for _, sym := range symbolsFromExport(child, src) {
				addSymbol(sym)
			}
			// import-from-re-export: `export { x } from "./y"`.
			if s := importFromNode(child, src); s != "" {
				addImport(s)
			}
		case "import_statement":
			addImport(importSpecifierFrom(child, src))
		case "function_declaration":
			if name := identifierChild(child, "name", src); name != "" {
				addSymbol(index.Symbol{Name: name, Kind: index.SymbolFunc, Exported: false})
			}
		case "class_declaration":
			if name := identifierChild(child, "name", src); name != "" {
				addSymbol(index.Symbol{Name: name, Kind: index.SymbolType, Exported: false})
			}
		case "interface_declaration":
			if name := identifierChild(child, "name", src); name != "" {
				addSymbol(index.Symbol{Name: name, Kind: index.SymbolInterface, Exported: false})
			}
		case "type_alias_declaration":
			if name := identifierChild(child, "name", src); name != "" {
				addSymbol(index.Symbol{Name: name, Kind: index.SymbolType, Exported: false})
			}
		case "lexical_declaration", "variable_declaration":
			for _, sym := range symbolsFromVariableDecl(child, src) {
				addSymbol(sym)
			}
			// §7.3.2 enclosure (b): `const foo = require("x")` at
			// module scope. JS-family only — TypeScript's import
			// surface already covers these cases via the typed
			// import AST.
			if lang == LangJavaScript {
				for _, spec := range requireCallsInVariableDecl(child, src) {
					addImport(spec)
				}
			}
		case "expression_statement":
			// CommonJS `require("...")` at module level (JS/JSX/CJS
			// only — TypeScript statements live under typed nodes
			// but the bare-`require` pattern is JS-centric).
			if lang == LangJavaScript {
				if spec := requireCallImportSpec(child, src); spec != "" {
					addImport(spec)
				}
			}
		}
	}

	imports := make([]string, 0, len(importSet))
	for s := range importSet {
		imports = append(imports, s)
	}
	// Sort so the output is deterministic across runs — Go map
	// iteration is randomized, and unsorted imports would vary
	// run-to-run in FileEntry / manifest / cache output.
	sort.Strings(imports)
	return symbols, imports
}

// symbolsFromExport handles `export function ...`, `export class ...`,
// `export interface ...`, `export type ...`, `export const ...`, and
// `export default ...`. Returns one or more symbols.
func symbolsFromExport(node *sitter.Node, src []byte) []index.Symbol {
	var out []index.Symbol
	// Anonymous default: `export default function() {}`,
	// `export default class {}`, `export default <expr>`.
	if isDefaultExport(node, src) {
		sym := defaultExportSymbol(node, src)
		out = append(out, sym)
		return out
	}
	// Named export: recurse into the child declaration, then flip
	// Exported=true on whatever the underlying node produces.
	for i := 0; i < int(node.NamedChildCount()); i++ {
		ch := node.NamedChild(i)
		if ch == nil {
			continue
		}
		switch ch.Type() {
		case "function_declaration":
			if name := identifierChild(ch, "name", src); name != "" {
				out = append(out, index.Symbol{Name: name, Kind: index.SymbolFunc, Exported: true})
			}
		case "class_declaration":
			if name := identifierChild(ch, "name", src); name != "" {
				out = append(out, index.Symbol{Name: name, Kind: index.SymbolType, Exported: true})
			}
		case "interface_declaration":
			if name := identifierChild(ch, "name", src); name != "" {
				out = append(out, index.Symbol{Name: name, Kind: index.SymbolInterface, Exported: true})
			}
		case "type_alias_declaration":
			if name := identifierChild(ch, "name", src); name != "" {
				out = append(out, index.Symbol{Name: name, Kind: index.SymbolType, Exported: true})
			}
		case "lexical_declaration", "variable_declaration":
			for _, sym := range symbolsFromVariableDecl(ch, src) {
				sym.Exported = true
				out = append(out, sym)
			}
		}
	}
	return out
}

func isDefaultExport(node *sitter.Node, src []byte) bool {
	// An `export default` statement has a child literal token "default".
	for i := 0; i < int(node.ChildCount()); i++ {
		c := node.Child(i)
		if c != nil && c.Type() == "default" {
			return true
		}
	}
	// Fallback: textual search for safety on grammars that name the
	// node differently across versions.
	t := nodeText(node, src)
	return strings.HasPrefix(strings.TrimSpace(t), "export default")
}

func defaultExportSymbol(node *sitter.Node, src []byte) index.Symbol {
	kind := index.SymbolVar
	for i := 0; i < int(node.NamedChildCount()); i++ {
		ch := node.NamedChild(i)
		if ch == nil {
			continue
		}
		switch ch.Type() {
		case "function_declaration", "function_expression", "arrow_function":
			kind = index.SymbolFunc
		case "class_declaration", "class":
			kind = index.SymbolType
		}
	}
	return index.Symbol{Name: "default", Kind: kind, Exported: true}
}

// symbolsFromVariableDecl extracts symbols from a
// `lexical_declaration` / `variable_declaration` node. Each declarator
// that names a single identifier produces one symbol; the kind
// depends on the RHS per §7.3.2:
//
//   - RHS is arrow_function or function_expression → SymbolFunc
//   - otherwise → SymbolVar
//
// Destructuring binding patterns are skipped (no single identifier).
func symbolsFromVariableDecl(node *sitter.Node, src []byte) []index.Symbol {
	var out []index.Symbol
	for i := 0; i < int(node.NamedChildCount()); i++ {
		decl := node.NamedChild(i)
		if decl == nil || decl.Type() != "variable_declarator" {
			continue
		}
		nameNode := decl.ChildByFieldName("name")
		if nameNode == nil || nameNode.Type() != "identifier" {
			continue
		}
		kind := index.SymbolVar
		valueNode := decl.ChildByFieldName("value")
		if valueNode != nil {
			switch valueNode.Type() {
			case "arrow_function", "function_expression":
				kind = index.SymbolFunc
			}
		}
		out = append(out, index.Symbol{Name: nodeText(nameNode, src), Kind: kind, Exported: false})
	}
	return out
}

// importSpecifierFrom extracts the "..." out of `import ... from "..."`.
// Returns "" when the node has no explicit source.
func importSpecifierFrom(node *sitter.Node, src []byte) string {
	source := node.ChildByFieldName("source")
	if source == nil {
		return ""
	}
	return trimQuotes(nodeText(source, src))
}

// importFromNode handles `export { ... } from "..."` re-exports: the
// "source" field may exist on `export_statement` when it carries a
// from-clause.
func importFromNode(node *sitter.Node, src []byte) string {
	s := node.ChildByFieldName("source")
	if s == nil {
		return ""
	}
	return trimQuotes(nodeText(s, src))
}

// requireCallsInVariableDecl inspects each variable_declarator in a
// lexical_declaration / variable_declaration and returns the import
// specifier of any module-level `require("...")` initializer. Other
// RHS shapes (literals, non-require calls, function expressions, …)
// are ignored.
func requireCallsInVariableDecl(decl *sitter.Node, src []byte) []string {
	var out []string
	for i := 0; i < int(decl.NamedChildCount()); i++ {
		d := decl.NamedChild(i)
		if d == nil || d.Type() != "variable_declarator" {
			continue
		}
		value := d.ChildByFieldName("value")
		if value == nil || value.Type() != "call_expression" {
			continue
		}
		if spec := requireCallArg(value, src); spec != "" {
			out = append(out, spec)
		}
	}
	return out
}

// requireCallArg pulls the string-literal argument out of a
// `require("...")` call_expression node. Returns "" when the shape
// doesn't match the §7.3.2 rule.
func requireCallArg(call *sitter.Node, src []byte) string {
	callee := call.ChildByFieldName("function")
	if callee == nil || callee.Type() != "identifier" || nodeText(callee, src) != "require" {
		return ""
	}
	args := call.ChildByFieldName("arguments")
	if args == nil || args.NamedChildCount() != 1 {
		return ""
	}
	arg := args.NamedChild(0)
	if arg == nil || arg.Type() != "string" {
		return ""
	}
	return trimQuotes(nodeText(arg, src))
}

// requireCallImportSpec returns the string argument of a top-level
// `require("...")` call wrapped in an `expression_statement`, or ""
// if the node doesn't match that shape. §7.3.2 requires ALL of:
//
//	(a) callee is an identifier with text "require"
//	(b) argument list has exactly one string literal
//	(c) the enclosing statement is a direct child of the program
//	    node (caller guarantees this by only visiting top-level
//	    children).
func requireCallImportSpec(stmt *sitter.Node, src []byte) string {
	if stmt.NamedChildCount() != 1 {
		return ""
	}
	call := stmt.NamedChild(0)
	if call == nil || call.Type() != "call_expression" {
		return ""
	}
	callee := call.ChildByFieldName("function")
	if callee == nil || callee.Type() != "identifier" || nodeText(callee, src) != "require" {
		return ""
	}
	args := call.ChildByFieldName("arguments")
	if args == nil || args.NamedChildCount() != 1 {
		return ""
	}
	arg := args.NamedChild(0)
	if arg == nil || arg.Type() != "string" {
		return ""
	}
	return trimQuotes(nodeText(arg, src))
}

// identifierChild returns the text of a named-field child of `node`
// when that child is an identifier. Returns "" when the field is
// absent or not an identifier.
func identifierChild(node *sitter.Node, field string, src []byte) string {
	ch := node.ChildByFieldName(field)
	if ch == nil {
		return ""
	}
	// Accept `identifier`, `type_identifier`, `property_identifier`.
	return nodeText(ch, src)
}

// trimQuotes strips the single or double quotes that wrap a string
// literal's text. Tree-sitter preserves the quote characters in the
// node's span; we always want the inner value.
func trimQuotes(s string) string {
	if len(s) < 2 {
		return s
	}
	first, last := s[0], s[len(s)-1]
	if (first == '"' || first == '\'' || first == '`') && first == last {
		return s[1 : len(s)-1]
	}
	return s
}
