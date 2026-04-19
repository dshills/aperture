//go:build notier2

// Package tstree: stub for `-tags notier2` builds. Tier-2 analysis
// is skipped; every Parse call reports ParseError=true and every
// file falls through to tier-3 lexical scoring. This tag is the
// PLAN's CGO_ENABLED=0 escape hatch.
package tstree

import (
	"context"

	"github.com/dshills/aperture/internal/index"
)

// Lang mirrors the type in parse.go; we redeclare it here so
// callers don't need build tags.
type Lang int

const (
	LangTypeScript Lang = iota + 1
	LangTSX
	LangJavaScript
	LangPython
)

// Result mirrors the CGo-enabled form.
type Result struct {
	Path       string
	Symbols    []index.Symbol
	Imports    []string
	ParseError bool
}

// LanguageForExtension always returns zero under notier2, which
// routes every extension to tier-3.
func LanguageForExtension(_ string) Lang { return 0 }

// Parse is a no-op under notier2.
func Parse(_ context.Context, path string, _ Lang, _ []byte) *Result {
	return &Result{Path: path, ParseError: true}
}
