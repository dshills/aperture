//go:build !notier2

package tstree

import (
	"context"
	"slices"
	"sort"
	"testing"

	"github.com/dshills/aperture/internal/index"
)

func parseOK(t *testing.T, lang Lang, src string) *Result {
	t.Helper()
	r := Parse(context.Background(), "test.src", lang, []byte(src))
	if r.ParseError {
		t.Fatalf("ParseError=true for source:\n%s", src)
	}
	return r
}

func symbolNames(ss []index.Symbol) []string {
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		out = append(out, s.Name)
	}
	sort.Strings(out)
	return out
}

func TestParse_TypeScript_ExportedDeclarations(t *testing.T) {
	src := `
import { helper } from "./util";
import * as fs from "node:fs";

export function handleRequest() {}
export class Handler {}
export interface Options {}
export type Payload = { id: string };
export const maker = () => 42;
export const notFunc = 1;
export default function() {}

function helperInternal() {}
const x = 1;
`
	r := parseOK(t, LangTypeScript, src)
	got := symbolNames(r.Symbols)
	want := []string{"Handler", "Options", "Payload", "default", "handleRequest", "helperInternal", "maker", "notFunc", "x"}
	if !slices.Equal(got, want) {
		t.Errorf("symbols=%v\nwant   =%v", got, want)
	}
	// Verify Kind on notFunc: it's an exported const that is NOT a
	// function-valued arrow; must land as SymbolVar.
	for _, s := range r.Symbols {
		if s.Name == "notFunc" && s.Kind != index.SymbolVar {
			t.Errorf("notFunc kind=%v, want variable", s.Kind)
		}
		if s.Name == "maker" && s.Kind != index.SymbolFunc {
			t.Errorf("maker kind=%v, want function", s.Kind)
		}
		if s.Name == "helperInternal" && s.Exported {
			t.Errorf("helperInternal should be Exported=false")
		}
		if s.Name == "Handler" && !s.Exported {
			t.Errorf("Handler should be Exported=true")
		}
	}
	// Imports: both specifiers recorded verbatim, deduped.
	wantImports := map[string]bool{"./util": true, "node:fs": true}
	for _, imp := range r.Imports {
		if !wantImports[imp] {
			t.Errorf("unexpected import %q (got %v)", imp, r.Imports)
		}
	}
	if len(r.Imports) != 2 {
		t.Errorf("imports=%v, want 2 entries", r.Imports)
	}
}

func TestParse_TypeScript_NestedDeclarationsSkipped(t *testing.T) {
	src := `
export function outer() {
  function nestedFn() {}
  class NestedClass {}
}
`
	r := parseOK(t, LangTypeScript, src)
	got := symbolNames(r.Symbols)
	if !slices.Equal(got, []string{"outer"}) {
		t.Errorf("nested declarations should be skipped; got %v", got)
	}
}

func TestParse_TSX_HandlesJSX(t *testing.T) {
	src := `
export const Button = () => <button>go</button>;
`
	r := parseOK(t, LangTSX, src)
	got := symbolNames(r.Symbols)
	if !slices.Equal(got, []string{"Button"}) {
		t.Errorf("tsx parse: got %v", got)
	}
}

func TestParse_JavaScript_RequireAtModuleLevel(t *testing.T) {
	src := `
const foo = require("./local");
require("side-effect");

function inner() {
  const nested = require("nested-import");
}
`
	r := parseOK(t, LangJavaScript, src)
	got := map[string]bool{}
	for _, s := range r.Imports {
		got[s] = true
	}
	if !got["./local"] {
		t.Errorf("missing ./local import: %v", r.Imports)
	}
	if !got["side-effect"] {
		t.Errorf("missing side-effect import: %v", r.Imports)
	}
	if got["nested-import"] {
		t.Errorf("nested require should NOT be recorded: %v", r.Imports)
	}
}

func TestParse_Python_ModuleLevelSymbols(t *testing.T) {
	src := `
import os
import typing as t
from .util import helper
from ..shared import OrderModel

def top():
    pass

async def async_top():
    pass

class Handler:
    pass

_private_const = 1
PublicVar = "hello"
lam = lambda x: x + 1

def _underscore():
    pass
`
	r := parseOK(t, LangPython, src)
	got := symbolNames(r.Symbols)
	wantSubset := []string{"Handler", "PublicVar", "_private_const", "_underscore", "async_top", "lam", "top"}
	for _, w := range wantSubset {
		if !slices.Contains(got, w) {
			t.Errorf("expected symbol %q; got %v", w, got)
		}
	}
	// Visibility: `_`-prefixed names are Exported=false.
	for _, s := range r.Symbols {
		if s.Name == "_underscore" && s.Exported {
			t.Errorf("_underscore should be Exported=false")
		}
		if s.Name == "top" && !s.Exported {
			t.Errorf("top should be Exported=true")
		}
		if s.Name == "lam" && s.Kind != index.SymbolFunc {
			t.Errorf("lam (lambda) should be SymbolFunc; got %v", s.Kind)
		}
	}
	// Imports: module names recorded once; submodule names from
	// `from X import a, b` are NOT recorded (§7.3.2).
	wantImports := map[string]bool{"os": true, "typing": true, ".util": true, "..shared": true}
	for _, imp := range r.Imports {
		if !wantImports[imp] {
			t.Errorf("unexpected python import %q (all: %v)", imp, r.Imports)
		}
	}
}

func TestParse_ParseErrorOnGarbage(t *testing.T) {
	garbage := []byte("!!!! this is not valid ts !!!!")
	r := Parse(context.Background(), "bad.ts", LangTypeScript, garbage)
	if !r.ParseError {
		t.Errorf("ParseError should be true for garbage input; got Symbols=%v", r.Symbols)
	}
}
