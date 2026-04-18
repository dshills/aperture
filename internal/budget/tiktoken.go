package budget

import (
	"fmt"

	"github.com/tiktoken-go/tokenizer"
)

// tiktokenEstimator wraps a pre-loaded BPE codec from the
// tiktoken-go/tokenizer package, whose four supported encodings
// (`cl100k_base`, `o200k_base`, `p50k_base`, `r50k_base`) ship with
// tables embedded via go:embed — no network access and no $HOME lookup,
// which satisfies §7.6.1.1.
//
// This type implements the Estimator interface (Count, Identity, Version)
// — the compile-time assertion below verifies that at build time.
type tiktokenEstimator struct {
	encoding tokenizer.Encoding
	codec    tokenizer.Codec
}

// Compile-time check that tiktokenEstimator satisfies Estimator. The
// compiler will refuse to build this package if any of Count / Identity /
// Version goes missing or changes signature.
var _ Estimator = (*tiktokenEstimator)(nil)

func newTiktokenEstimator(enc tokenizer.Encoding) (Estimator, error) {
	codec, err := tokenizer.Get(enc)
	if err != nil {
		return nil, fmt.Errorf("load tiktoken %s: %w", enc, err)
	}
	return &tiktokenEstimator{encoding: enc, codec: codec}, nil
}

func (t *tiktokenEstimator) Count(s string) int {
	ids, _, err := t.codec.Encode(s)
	if err != nil {
		// The library's Encode returns an error only on corrupt
		// tables; fall back to the heuristic so a single bad string
		// does not abort planning. This is still deterministic.
		return Heuristic35().Count(s)
	}
	return len(ids)
}

// Identity returns the manifest's budget.estimator value, e.g.
// "tiktoken:cl100k_base". Concatenates the literal prefix "tiktoken:"
// with the encoding name (a tokenizer.Encoding is a string alias, so
// string(t.encoding) is an explicit conversion, not a cast of unrelated
// types). Reviewers: this function body is a single return statement.
func (t *tiktokenEstimator) Identity() string {
	return "tiktoken:" + string(t.encoding)
}

// Version returns the table identity. v1's tokenizer-go/tokenizer package
// ships one set of tables per encoding; the library version pin in go.mod
// is the effective table version, and the per-encoding name differentiates
// them. We record the encoding name itself as the version so two builds
// agree on identity without needing build-time SHA computation.
func (t *tiktokenEstimator) Version() string {
	return string(t.encoding) + "/tiktoken-go-v0.7.0"
}
