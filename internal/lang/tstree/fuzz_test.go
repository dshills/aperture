//go:build !notier2

package tstree

import (
	"context"
	"testing"
)

// FuzzParseTypeScript feeds arbitrary bytes into the TypeScript
// parser. The invariant per PLAN §11.6: Parse MUST NOT panic, MUST
// return either a valid *Result with ParseError=false/true, and MUST
// bound its memory usage to the grammar's internal limits (which
// tree-sitter already enforces). A fuzz-found panic is a bug.
func FuzzParseTypeScript(f *testing.F) {
	f.Add([]byte(`export function foo() {}`))
	f.Add([]byte(`export const x = () => {}`))
	f.Add([]byte(``))
	f.Add([]byte(`\x00\x00\x00\x00`))
	f.Add([]byte(`import { a } from "./b"; const c = require("d");`))
	f.Add([]byte(`function<T>(): T { yield 1 }`)) // borderline syntax

	f.Fuzz(func(t *testing.T, src []byte) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("Parse panicked on input: %v", r)
			}
		}()
		_ = Parse(context.Background(), "fuzz.ts", LangTypeScript, src)
	})
}
