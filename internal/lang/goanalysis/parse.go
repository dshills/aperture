package goanalysis

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/dshills/aperture/internal/index"
)

// FileResult bundles the AST-derived fields for a single Go file. Populated
// when Analyze completes; consumed by the caller to fill into the Index.
type FileResult struct {
	Path        string
	PackageName string
	Imports     []string
	Symbols     []index.Symbol
	SideEffects []string
	ParseError  bool
}

// AnalyzeOptions configures Analyze.
type AnalyzeOptions struct {
	// Root is the absolute repo root. Each file in Paths is read from
	// filepath.Join(Root, file).
	Root string
	// Paths are the repo-relative Go file paths to parse.
	Paths []string
}

// Analyze parses every listed Go file concurrently and returns a slice of
// FileResult sorted ascending by path so downstream stages assemble the
// Index deterministically regardless of goroutine scheduling.
func Analyze(opts AnalyzeOptions) ([]FileResult, error) {
	if len(opts.Paths) == 0 {
		return nil, nil
	}
	workers := runtime.NumCPU()
	if workers < 1 {
		workers = 1
	}
	if workers > len(opts.Paths) {
		workers = len(opts.Paths)
	}

	jobs := make(chan string, len(opts.Paths))
	for _, p := range opts.Paths {
		jobs <- p
	}
	close(jobs)

	results := make([]FileResult, len(opts.Paths))
	indexByPath := map[string]int{}
	for i, p := range opts.Paths {
		indexByPath[p] = i
	}

	var (
		wg       sync.WaitGroup
		once     sync.Once
		firstErr error
	)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for rel := range jobs {
				res, err := analyzeOne(opts.Root, rel)
				if err != nil {
					once.Do(func() { firstErr = err })
					continue
				}
				results[indexByPath[rel]] = res
			}
		}()
	}
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}

	sort.Slice(results, func(i, j int) bool { return results[i].Path < results[j].Path })
	return results, nil
}

// analyzeOne parses a single file. On success it returns the extracted
// facts; on parse failure it returns the §7.2.3 fallback result with
// ParseError=true and a best-effort import list harvested by the minimal
// non-AST scan.
func analyzeOne(root, rel string) (FileResult, error) {
	absPath := filepath.Join(root, rel)
	fset := token.NewFileSet()
	src, err := os.ReadFile(absPath) //nolint:gosec // walker-verified path
	if err != nil {
		return FileResult{Path: rel, ParseError: true}, nil
	}

	// ParseComments is deliberately omitted — we do not need them for v1
	// and excluding them keeps the AST smaller.
	file, parseErr := parser.ParseFile(fset, absPath, src, parser.SkipObjectResolution)
	if parseErr != nil {
		imports := fallbackImportScan(src)
		return FileResult{
			Path:        rel,
			Imports:     dedupeSort(imports),
			SideEffects: SideEffectsFor(imports),
			ParseError:  true,
		}, nil
	}

	pkgName := ""
	if file.Name != nil {
		pkgName = file.Name.Name
	}

	imports := collectImports(file)
	symbols := collectSymbols(file)

	return FileResult{
		Path:        rel,
		PackageName: pkgName,
		Imports:     dedupeSort(imports),
		Symbols:     symbols,
		SideEffects: SideEffectsFor(imports),
	}, nil
}

func collectImports(file *ast.File) []string {
	out := make([]string, 0, len(file.Imports))
	for _, imp := range file.Imports {
		if imp.Path == nil {
			continue
		}
		p, err := unquoteGoString(imp.Path.Value)
		if err != nil {
			continue
		}
		out = append(out, p)
	}
	return out
}

func collectSymbols(file *ast.File) []index.Symbol {
	var out []index.Symbol
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			out = append(out, symbolsFromGenDecl(d)...)
		case *ast.FuncDecl:
			if sym, ok := symbolFromFuncDecl(d); ok {
				out = append(out, sym)
			}
		}
	}
	return out
}

// symbolsFromGenDecl processes `type`, `const`, and `var` declarations,
// expanding one-token-per-spec as needed. Interfaces are distinguished
// from structs/aliases via the declared type expression.
func symbolsFromGenDecl(d *ast.GenDecl) []index.Symbol {
	var out []index.Symbol
	switch d.Tok {
	case token.TYPE:
		for _, spec := range d.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			if ts.Name == nil || !ts.Name.IsExported() {
				continue
			}
			kind := index.SymbolType
			if _, ok := ts.Type.(*ast.InterfaceType); ok {
				kind = index.SymbolInterface
			}
			out = append(out, index.Symbol{Name: ts.Name.Name, Kind: kind})
		}
	case token.CONST, token.VAR:
		kind := index.SymbolConst
		if d.Tok == token.VAR {
			kind = index.SymbolVar
		}
		for _, spec := range d.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, n := range vs.Names {
				if n == nil || !n.IsExported() {
					continue
				}
				out = append(out, index.Symbol{Name: n.Name, Kind: kind})
			}
		}
	}
	return out
}

func symbolFromFuncDecl(d *ast.FuncDecl) (index.Symbol, bool) {
	if d.Name == nil || !d.Name.IsExported() {
		return index.Symbol{}, false
	}
	if d.Recv == nil || len(d.Recv.List) == 0 {
		return index.Symbol{Name: d.Name.Name, Kind: index.SymbolFunc}, true
	}
	recv := receiverTypeName(d.Recv.List[0].Type)
	return index.Symbol{Name: d.Name.Name, Kind: index.SymbolMethod, Receiver: recv}, true
}

// receiverTypeName recovers the receiver type's identifier (stripping `*`
// and generic type-parameter syntax). Best-effort; used only for
// metadata.
func receiverTypeName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.StarExpr:
		return receiverTypeName(t.X)
	case *ast.Ident:
		return t.Name
	case *ast.IndexExpr:
		return receiverTypeName(t.X)
	case *ast.IndexListExpr:
		return receiverTypeName(t.X)
	}
	return ""
}

// unquoteGoString decodes a Go string literal. Go import paths can in
// principle contain escape sequences (\u, \x, \n) — stripping quotes
// manually would mis-handle those cases, so we defer to strconv.Unquote.
func unquoteGoString(lit string) (string, error) {
	s, err := strconv.Unquote(lit)
	if err != nil {
		return "", fmt.Errorf("malformed import string %q: %w", lit, err)
	}
	return s, nil
}

func dedupeSort(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(in))
	for _, s := range in {
		set[s] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// fallbackImportScan walks src line by line and collects the paths inside
// a single-line or block `import` declaration. This is used ONLY when the
// AST parse fails — SPEC §7.2.3 narrowly permits it as a fallback.
func fallbackImportScan(src []byte) []string {
	var (
		out     []string
		inBlock bool
	)
	scanner := bufio.NewScanner(strings.NewReader(string(src)))
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case !inBlock && strings.HasPrefix(line, "import ("):
			inBlock = true
		case inBlock && line == ")":
			inBlock = false
		case inBlock:
			if p := extractQuoted(line); p != "" {
				out = append(out, p)
			}
		case strings.HasPrefix(line, "import \""):
			if p := extractQuoted(line); p != "" {
				out = append(out, p)
			}
		case strings.HasPrefix(line, "import ") && strings.Contains(line, "\""):
			// `import alias "path"`
			if p := extractQuoted(line); p != "" {
				out = append(out, p)
			}
		}
	}
	return out
}

func extractQuoted(line string) string {
	start := strings.IndexByte(line, '"')
	if start < 0 {
		return ""
	}
	end := strings.IndexByte(line[start+1:], '"')
	if end < 0 {
		return ""
	}
	return line[start+1 : start+1+end]
}
