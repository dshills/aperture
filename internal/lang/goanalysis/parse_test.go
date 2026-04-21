package goanalysis

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/dshills/aperture/internal/index"
)

const sampleGo = `
// Package sample exercises every exported-symbol kind plus a few
// unexported declarations that must not leak into the symbol set.
package sample

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// ExportedType is an exported struct.
type ExportedType struct{ field int }

// Exposed is an exported interface.
type Exposed interface {
	Name() string
}

// ExportedConst is an exported const.
const ExportedConst = 42

// unexportedConst must not appear in the symbol set.
const unexportedConst = 1

// ExportedVar is an exported var.
var ExportedVar = "hello"

// ExportedFunc is a top-level exported function.
func ExportedFunc() string { return "x" }

// String is a method on ExportedType.
func (e *ExportedType) String() string { return fmt.Sprint(e.field) }

// internalFunc must not appear in the symbol set.
func internalFunc() {}

// keep imports live so go vet in the test harness doesn't complain
var _ = http.StatusOK
var _ = time.Now
`

func TestAnalyze_ExtractsEveryExportedSymbolKind(t *testing.T) {
	root := t.TempDir()
	p := "sample.go"
	writeTempFile(t, filepath.Join(root, p), sampleGo)

	res, err := Analyze(context.Background(), AnalyzeOptions{Root: root, Paths: []string{p}})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if len(res) != 1 {
		t.Fatalf("expected 1 result, got %d", len(res))
	}
	got := res[0]

	if got.PackageName != "sample" {
		t.Errorf("package name: got %q want sample", got.PackageName)
	}
	if got.ParseError {
		t.Errorf("parse should have succeeded")
	}

	want := map[string]index.SymbolKind{
		"ExportedType":  index.SymbolType,
		"Exposed":       index.SymbolInterface,
		"ExportedConst": index.SymbolConst,
		"ExportedVar":   index.SymbolVar,
		"ExportedFunc":  index.SymbolFunc,
		"String":        index.SymbolMethod,
	}

	seen := map[string]index.SymbolKind{}
	for _, s := range got.Symbols {
		seen[s.Name] = s.Kind
	}
	for name, kind := range want {
		if seen[name] != kind {
			t.Errorf("missing or wrong-kind symbol %s: got %q want %q", name, seen[name], kind)
		}
	}
	// Unexported names must not leak.
	for _, bad := range []string{"internalFunc", "unexportedConst"} {
		if _, ok := seen[bad]; ok {
			t.Errorf("unexported symbol %q leaked into symbol set", bad)
		}
	}

	if !slices.Contains(got.Imports, "net/http") || !slices.Contains(got.Imports, "time") {
		t.Errorf("expected imports missing: %v", got.Imports)
	}
}

func TestAnalyze_ParseErrorFallbackRecordsImports(t *testing.T) {
	root := t.TempDir()
	p := "broken.go"
	// Truncated function declaration — will NOT parse.
	writeTempFile(t, filepath.Join(root, p), `package broken
import (
	"context"
	"os"
	"time"
)
func Broken( { `)
	res, err := Analyze(context.Background(), AnalyzeOptions{Root: root, Paths: []string{p}})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	got := res[0]
	if !got.ParseError {
		t.Fatal("expected ParseError=true on malformed file")
	}
	if !slices.Contains(got.Imports, "os") || !slices.Contains(got.Imports, "time") {
		t.Fatalf("fallback import scan missed imports: %v", got.Imports)
	}
	if len(got.Symbols) != 0 {
		t.Fatalf("no symbols should be emitted on parse error: %v", got.Symbols)
	}
}

func TestAnalyze_DeterministicOrdering(t *testing.T) {
	root := t.TempDir()
	writeTempFile(t, filepath.Join(root, "b.go"), "package x\n")
	writeTempFile(t, filepath.Join(root, "a.go"), "package x\n")
	writeTempFile(t, filepath.Join(root, "c.go"), "package x\n")

	// Call twice with shuffled input orders; outputs must still be sorted
	// ascending by path.
	for _, order := range [][]string{{"b.go", "a.go", "c.go"}, {"c.go", "b.go", "a.go"}} {
		res, err := Analyze(context.Background(), AnalyzeOptions{Root: root, Paths: order})
		if err != nil {
			t.Fatalf("Analyze: %v", err)
		}
		paths := []string{res[0].Path, res[1].Path, res[2].Path}
		want := []string{"a.go", "b.go", "c.go"}
		if !slices.Equal(paths, want) {
			t.Fatalf("non-deterministic output for order %v: got %v", order, paths)
		}
	}
}

func writeTempFile(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
