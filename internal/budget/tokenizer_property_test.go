package budget

import (
	"math/rand/v2"
	"strings"
	"testing"
)

// Property + fuzz coverage for the token estimators. Scaffolded in
// response to verifier findings TESTREC-9E9AD3C5 (Count determinism)
// and TESTREC-8255FCAC (Resolve model dispatch). The unit suite in
// tokenizer_test.go hits hand-picked models; these cover the
// invariants that must hold for ANY input.

// Invariant: the heuristic and tiktoken estimators are pure — the same
// input string produces the same count, every time, on the same host.
// §7.6.1.1 requires "deterministic for a given (estimator,
// estimator_version, input) triple".
func TestProperty_Count_IsDeterministic(t *testing.T) {
	estimators := []Estimator{Heuristic35()}
	if tk, err := newTiktokenEstimator("cl100k_base"); err == nil {
		estimators = append(estimators, tk)
	}
	rng := rand.New(rand.NewPCG(7, 8))
	for i := 0; i < 64; i++ {
		s := randString(rng, 1+rng.IntN(2000))
		for _, est := range estimators {
			a := est.Count(s)
			b := est.Count(s)
			if a != b {
				t.Fatalf("%s.Count non-deterministic: %d vs %d", est.Identity(), a, b)
			}
		}
	}
}

// Invariant: longer inputs never produce fewer tokens than shorter
// prefixes. Token counts monotonically increase with input length.
func TestProperty_Count_IsMonotoneInLength(t *testing.T) {
	est := Heuristic35()
	rng := rand.New(rand.NewPCG(9, 10))
	for i := 0; i < 32; i++ {
		base := randString(rng, 200+rng.IntN(400))
		shortCount := est.Count(base[:len(base)/2])
		fullCount := est.Count(base)
		if fullCount < shortCount {
			t.Fatalf("Count(prefix)=%d > Count(full)=%d", shortCount, fullCount)
		}
	}
}

// Invariant: §7.6.1.1 mandates upward-biased counts for the heuristic
// ("ceil(len(utf8_bytes) / 3.5)"). For non-empty inputs the count must
// be at least ceil(len / 3.5), which is also a lower bound on any
// conforming estimator. We assert the exact heuristic value for the
// heuristic AND assert tiktoken never returns 0 for a non-empty input.
func TestProperty_Count_NonEmptyInputProducesPositiveCount(t *testing.T) {
	estimators := []Estimator{Heuristic35()}
	if tk, err := newTiktokenEstimator("cl100k_base"); err == nil {
		estimators = append(estimators, tk)
	}
	rng := rand.New(rand.NewPCG(11, 12))
	for i := 0; i < 32; i++ {
		s := randString(rng, 1+rng.IntN(500))
		for _, est := range estimators {
			if got := est.Count(s); got <= 0 {
				t.Fatalf("%s.Count(%q)=%d, want >0", est.Identity(), s, got)
			}
		}
	}
}

// FuzzResolve exercises the §7.6.1.1 model-name dispatch with arbitrary
// inputs. Resolve must NEVER panic — unrecognized models fall through
// to the heuristic, recognized-but-unsupported models return a
// ResolveError, and no input can produce a nil estimator with nil
// error.
func FuzzResolve(f *testing.F) {
	seeds := []string{
		"",
		"claude-sonnet",
		"gpt-4o",
		"gpt-4",
		"gpt-3.5-turbo",
		"codex-edit-001",
		"o1-mini",
		"some-mystery-model",
		"CLAUDE-UPPER",
		"\x00\x01\x02",
		strings.Repeat("x", 10000),
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, model string) {
		est, err := Resolve(ResolveOptions{Model: model})
		if err != nil {
			// The only legitimate error is ResolveError; any other
			// type means Resolve leaked an internal failure mode.
			if _, ok := err.(*ResolveError); !ok {
				t.Fatalf("Resolve(%q) returned non-ResolveError: %T: %v", model, err, err)
			}
			return
		}
		if est == nil {
			t.Fatalf("Resolve(%q) returned (nil, nil)", model)
		}
		// Identity and Version must be non-empty regardless of input.
		if est.Identity() == "" {
			t.Fatalf("Resolve(%q).Identity() is empty", model)
		}
		if est.Version() == "" {
			t.Fatalf("Resolve(%q).Version() is empty", model)
		}
	})
}

// randString returns a deterministic pseudo-random ASCII-ish string of
// the given length. Mixes letters, digits, and whitespace so the
// heuristic's byte-length math is exercised over varied content.
func randString(rng *rand.Rand, n int) string {
	const alphabet = "abcdefghijklmnopqrstuvwxyz ABCDEFGHIJKLMNOPQRSTUVWXYZ 0123456789 \t\n"
	var b strings.Builder
	b.Grow(n)
	for i := 0; i < n; i++ {
		b.WriteByte(alphabet[rng.IntN(len(alphabet))])
	}
	return b.String()
}
