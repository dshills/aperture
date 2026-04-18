package relevance

import (
	"github.com/dshills/aperture/internal/config"
	"github.com/dshills/aperture/internal/manifest"
)

// Breakdown renders a score_breakdown slice per §11.1 catalogue: one
// entry per non-zero-signal factor, sorted in the §7.4.2.2 declaration
// order (see factorOrder at the top of this package). Zero-signal
// factors must be OMITTED per the catalogue rather than emitted as zero.
func Breakdown(signals map[string]float64, w config.Weights) []manifest.BreakdownEntry {
	weights := map[string]float64{
		"mention":  w.Mention,
		"filename": w.Filename,
		"symbol":   w.Symbol,
		"import":   w.Import,
		"package":  w.Package,
		"test":     w.Test,
		"doc":      w.Doc,
		"config":   w.Config,
	}
	out := make([]manifest.BreakdownEntry, 0, len(factorOrder))
	for _, name := range factorOrder {
		signal := signals[name]
		if signal == 0 {
			continue
		}
		weight := weights[name]
		out = append(out, manifest.BreakdownEntry{
			Factor:       name,
			Signal:       signal,
			Weight:       weight,
			Contribution: signal * weight,
		})
	}
	return out
}
