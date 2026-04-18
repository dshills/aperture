package budget

import (
	"errors"
	"testing"
)

func TestResolve_Heuristic_WhenUnspecified(t *testing.T) {
	est, err := Resolve(ResolveOptions{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if est.Identity() != "heuristic-3.5" {
		t.Fatalf("expected heuristic-3.5, got %q", est.Identity())
	}
}

func TestResolve_Heuristic_ForClaude(t *testing.T) {
	est, err := Resolve(ResolveOptions{Model: "claude-sonnet-4-6"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if est.Identity() != "heuristic-3.5" {
		t.Fatalf("claude must use heuristic, got %q", est.Identity())
	}
}

func TestResolve_Tiktoken_ForGPT4o(t *testing.T) {
	est, err := Resolve(ResolveOptions{Model: "gpt-4o"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if est.Identity() != "tiktoken:o200k_base" {
		t.Fatalf("gpt-4o should map to o200k_base, got %q", est.Identity())
	}
}

func TestResolve_Tiktoken_ForGPT4(t *testing.T) {
	est, err := Resolve(ResolveOptions{Model: "gpt-4"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if est.Identity() != "tiktoken:cl100k_base" {
		t.Fatalf("gpt-4 should map to cl100k_base, got %q", est.Identity())
	}
}

func TestResolve_Heuristic_ForUnrecognizedModel(t *testing.T) {
	est, err := Resolve(ResolveOptions{Model: "some-mystery-model"})
	if err != nil {
		t.Fatalf("Resolve should not error on unrecognized models: %v", err)
	}
	if est.Identity() != "heuristic-3.5" {
		t.Fatalf("unrecognized should fall back to heuristic, got %q", est.Identity())
	}
}

// The heuristic must round up, never down (§7.6.1.1 "biased upward").
func TestHeuristic_RoundsUp(t *testing.T) {
	h := Heuristic35()
	// 10 bytes / 3.5 = 2.857... → ceil = 3
	if got := h.Count("0123456789"); got != 3 {
		t.Fatalf("expected 3 tokens for 10 bytes, got %d", got)
	}
	// 7 bytes / 3.5 = 2.0 → exact, no rounding concern
	if got := h.Count("0123456"); got != 2 {
		t.Fatalf("expected 2 tokens for 7 bytes, got %d", got)
	}
	// Empty string → 0
	if got := h.Count(""); got != 0 {
		t.Fatalf("expected 0 tokens for empty string, got %d", got)
	}
}

func TestTiktoken_CountIsPositive(t *testing.T) {
	est, err := newTiktokenEstimator("cl100k_base")
	if err != nil {
		t.Fatalf("newTiktokenEstimator: %v", err)
	}
	n := est.Count("hello world")
	if n <= 0 {
		t.Fatalf("expected positive token count, got %d", n)
	}
}

// Resolve returning ResolveError should map to exit code 10 per §16.
// We can't reproduce the error from pkoukk's loaded tables, but we can
// assert the error type is usable with errors.As for downstream callers.
func TestResolveError_Type(t *testing.T) {
	err := &ResolveError{Code: 10, Reason: "test"}
	var re *ResolveError
	if !errors.As(err, &re) {
		t.Fatal("ResolveError must be matchable via errors.As")
	}
	if re.Code != 10 {
		t.Fatalf("code lost in round trip: %d", re.Code)
	}
}
