package relevance

import (
	"testing"

	"github.com/dshills/aperture/internal/config"
	"github.com/dshills/aperture/internal/index"
	"github.com/dshills/aperture/internal/manifest"
	"github.com/dshills/aperture/internal/task"
)

// TestDampener_FalsePositiveRankingFlip is the §11 / PLAN Phase 2
// behavioral proof: on a synthetic two-file index, the file that the
// task names by path outranks the file with agreeing structural
// signals when the dampener is disabled, and the ranking flips when
// the dampener is enabled — exactly the behavior the false-positive
// fixture is committed to gate against.
func TestDampener_FalsePositiveRankingFlip(t *testing.T) {
	// Synthetic two-file repo:
	//   provider.go — named in the task; NO exported symbols / imports
	//                 that agree with anchors. s_mention dominates.
	//   refresh.go  — exports RefreshToken, Retry, Backoff; anchors
	//                 agree with symbols. s_symbol dominates.
	idx := &index.Index{
		Files: []index.FileEntry{
			{
				Path:      "internal/oauth/provider.go",
				Extension: ".go",
				Language:  "go",
				Package:   "internal/oauth",
				Symbols: []index.Symbol{
					{Name: "Provider", Kind: index.SymbolType},
				},
			},
			{
				Path:      "internal/oauth/refresh.go",
				Extension: ".go",
				Language:  "go",
				Package:   "internal/oauth",
				Symbols: []index.Symbol{
					{Name: "RefreshToken", Kind: index.SymbolFunc},
					{Name: "Retry", Kind: index.SymbolFunc},
					{Name: "Backoff", Kind: index.SymbolFunc},
				},
			},
		},
		Packages: map[string]*index.Package{
			"internal/oauth": {
				Directory: "internal/oauth",
				Files:     []string{"internal/oauth/provider.go", "internal/oauth/refresh.go"},
			},
		},
	}

	// Task: names provider.go directly, but the anchors also include
	// the structural terms "refresh", "retry", "backoff" that refresh.go's
	// symbols match. The v1 scorer treats them independently.
	parsed := task.Task{
		RawText: "Fix the retry regression in provider.go. RefreshToken retry and Backoff helpers aren't firing.",
		Anchors: []string{
			"Fix", "RefreshToken", "Backoff", "retry", "regression",
			"provider.go", "helpers", "firing",
		},
		Type: manifest.ActionTypeBugfix,
	}
	weights := config.DefaultWeights()

	// --- Dampener OFF: v1.0 semantics.
	off := ScoreWithOptions(idx, parsed, weights, Options{
		Dampener: DampenerConfig{Enabled: false, Floor: 0.3, Slope: 0.7},
	})
	byPathOff := indexByPath(off)
	providerOff := byPathOff["internal/oauth/provider.go"].Score
	refreshOff := byPathOff["internal/oauth/refresh.go"].Score
	if providerOff <= refreshOff {
		t.Fatalf("dampener OFF: expected provider.go > refresh.go, got provider=%.4f refresh=%.4f", providerOff, refreshOff)
	}

	// --- Dampener ON: v1.1 default.
	on := ScoreWithOptions(idx, parsed, weights, Options{
		Dampener: DampenerConfig{Enabled: true, Floor: 0.3, Slope: 0.7},
	})
	byPathOn := indexByPath(on)
	providerOn := byPathOn["internal/oauth/provider.go"].Score
	refreshOn := byPathOn["internal/oauth/refresh.go"].Score
	if refreshOn <= providerOn {
		t.Fatalf("dampener ON: expected refresh.go > provider.go, got refresh=%.4f provider=%.4f", refreshOn, providerOn)
	}

	t.Logf("OFF: provider=%.4f refresh=%.4f", providerOff, refreshOff)
	t.Logf(" ON: provider=%.4f refresh=%.4f", providerOn, refreshOn)
}

// TestDampener_CounterExampleRankingPreserved is the counter-example
// gate: a file where s_mention AND structural signals agree must keep
// the same top rank whether the dampener is on or off. Prevents
// dampener over-reach from penalizing correct mentions.
func TestDampener_CounterExampleRankingPreserved(t *testing.T) {
	idx := &index.Index{
		Files: []index.FileEntry{
			{
				Path:      "internal/greet/greet.go",
				Extension: ".go",
				Language:  "go",
				Package:   "internal/greet",
				Symbols: []index.Symbol{
					{Name: "Greet", Kind: index.SymbolFunc},
				},
			},
			{
				Path:      "internal/other/other.go",
				Extension: ".go",
				Language:  "go",
				Package:   "internal/other",
				Symbols: []index.Symbol{
					{Name: "Helper", Kind: index.SymbolFunc},
				},
			},
		},
		Packages: map[string]*index.Package{
			"internal/greet": {Directory: "internal/greet", Files: []string{"internal/greet/greet.go"}},
			"internal/other": {Directory: "internal/other", Files: []string{"internal/other/other.go"}},
		},
	}
	parsed := task.Task{
		RawText: "Update the Greet function in internal/greet/greet.go to accept a prefix.",
		Anchors: []string{"Greet", "update", "internal/greet/greet.go", "greet", "greet.go", "prefix", "function"},
		Type:    manifest.ActionTypeFeature,
	}
	weights := config.DefaultWeights()

	for _, enabled := range []bool{false, true} {
		out := ScoreWithOptions(idx, parsed, weights, Options{
			Dampener: DampenerConfig{Enabled: enabled, Floor: 0.3, Slope: 0.7},
		})
		byPath := indexByPath(out)
		greet := byPath["internal/greet/greet.go"].Score
		other := byPath["internal/other/other.go"].Score
		if greet <= other {
			t.Errorf("dampener enabled=%v: greet.go should be top-ranked; greet=%.4f other=%.4f", enabled, greet, other)
		}
	}
}

func indexByPath(scored []Scored) map[string]Scored {
	out := make(map[string]Scored, len(scored))
	for _, s := range scored {
		out[s.Path] = s
	}
	return out
}
