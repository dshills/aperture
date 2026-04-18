// Package budget owns token-count estimation and the Phase-3 selection
// budget bookkeeping. All estimators are deterministic: identical
// (estimator, estimator_version, input) → identical count on any host.
package budget

import (
	"fmt"
	"math"
	"strings"

	"github.com/tiktoken-go/tokenizer"
)

// Estimator is the interface every v1 tokenizer satisfies. Implementations
// are required to produce upward-biased counts (SPEC §7.6.1.1: "All token
// counts must be biased upward").
type Estimator interface {
	// Count returns the estimated token count for the input string.
	Count(s string) int
	// Identity is the string stored as budget.estimator in the manifest.
	Identity() string
	// Version is stored as budget.estimator_version; for tiktoken this is
	// the encoding-table identity, for the heuristic it is "v1".
	Version() string
}

// ResolveOptions controls estimator selection.
type ResolveOptions struct {
	// Model is the resolved --model / defaults.model value. Empty means
	// "unspecified".
	Model string
}

// ResolveError carries a structured reason so callers can map to the
// correct exit code. §16 maps this to exit 10 for the "recognized but
// unsupported" case.
type ResolveError struct {
	Code   int
	Reason string
}

func (e *ResolveError) Error() string { return e.Reason }

// Resolve picks an Estimator per §7.6.1.1. Rules:
//   - claude-*                     → heuristic 3.5 bytes/token
//   - gpt-*, codex-*, o*           → tiktoken (encoding per family)
//   - unspecified / unrecognized   → heuristic 3.5
//   - recognized-but-unsupported   → ResolveError{Code: 10}
func Resolve(opts ResolveOptions) (Estimator, error) {
	m := strings.ToLower(strings.TrimSpace(opts.Model))

	if m == "" {
		return Heuristic35(), nil
	}
	if strings.HasPrefix(m, "claude-") {
		return Heuristic35(), nil
	}

	enc, recognized := encodingForModel(m)
	if !recognized {
		// Unrecognized: heuristic.
		return Heuristic35(), nil
	}
	est, err := newTiktokenEstimator(enc)
	if err != nil {
		return nil, &ResolveError{
			Code:   10,
			Reason: fmt.Sprintf("tokenizer tables for %q (%s) are not embedded in this build: %v", opts.Model, enc, err),
		}
	}
	return est, nil
}

// encodingForModel is a thin case analysis over §7.6.1.1's model→encoding
// table. Returns (encoding, recognized). An unrecognized model yields
// ("", false).
func encodingForModel(lowered string) (tokenizer.Encoding, bool) {
	switch {
	case strings.HasPrefix(lowered, "gpt-4o"):
		return tokenizer.O200kBase, true
	case strings.HasPrefix(lowered, "gpt-4"):
		return tokenizer.Cl100kBase, true
	case strings.HasPrefix(lowered, "gpt-3.5-turbo"):
		return tokenizer.Cl100kBase, true
	case strings.HasPrefix(lowered, "codex-"):
		return tokenizer.P50kBase, true
	case strings.HasPrefix(lowered, "o1") ||
		strings.HasPrefix(lowered, "o3") ||
		strings.HasPrefix(lowered, "o4"):
		return tokenizer.O200kBase, true
	}
	return "", false
}

// ceilDiv returns ceil(n/d) using float division and math.Ceil to avoid
// the integer-divide-rounds-down trap §7.6.1.1 warns about.
func ceilDiv(n int, d float64) int {
	if n <= 0 {
		return 0
	}
	return int(math.Ceil(float64(n) / d))
}
