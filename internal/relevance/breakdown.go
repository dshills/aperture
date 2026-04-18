package relevance

import (
	"github.com/dshills/aperture/internal/config"
	"github.com/dshills/aperture/internal/manifest"
)

// Breakdown renders a score_breakdown slice per §11.1 catalogue: one
// entry per non-zero-signal factor, sorted in the §7.4.2.2 declaration
// order (see factorOrder at the top of this package). Zero-signal
// factors must be OMITTED per the catalogue rather than emitted as zero.
//
// BreakdownWithDampener is the v1.1-aware variant that additionally
// emits the per-factor dampener field (§7.2.2 / §10.1). Breakdown
// preserves the v1.0 call signature so non-CLI callers aren't forced
// to pass the dampener.
func Breakdown(signals map[string]float64, w config.Weights) []manifest.BreakdownEntry {
	return BreakdownWithDampener(signals, w, DampenerConfig{}, 1.0)
}

// dampenerOne is a package-level 1.0 that every non-"mention" factor
// can point at when the dampener is enabled. Using a shared variable
// avoids a per-factor per-selection heap allocation in
// BreakdownWithDampener.
var dampenerOne = 1.0

// BreakdownWithDampener renders the score_breakdown including the
// §10.1 dampener field.
//
//   - When cfg.Enabled is false, the dampener field is OMITTED entirely
//     (pointer nil) so v1.0 manifests round-trip byte-identical.
//   - When cfg.Enabled is true, every emitted entry carries dampener —
//     value `mentionDampener` on the "mention" factor, 1.0 on every
//     other factor.
//
// The caller supplies the per-file mentionDampener that was actually
// applied in combine(), so a consumer can reconstruct the contribution
// exactly: contribution = signal * dampener * weight.
func BreakdownWithDampener(
	signals map[string]float64,
	w config.Weights,
	cfg DampenerConfig,
	mentionDampener float64,
) []manifest.BreakdownEntry {
	// Allocate mentionDamp once per call rather than per factor. Its
	// address is reused for every "mention" entry emitted by this
	// invocation. (Breakdown entries outlive this call through the
	// manifest, so the pointer must refer to a heap-lived local, not
	// the parameter's stack slot.)
	mentionDamp := mentionDampener
	out := make([]manifest.BreakdownEntry, 0, len(factorOrder))
	for _, name := range factorOrder {
		signal := signals[name]
		if signal == 0 {
			continue
		}
		weight := weightForFactor(name, w)
		entry := manifest.BreakdownEntry{
			Factor: name,
			Signal: signal,
			Weight: weight,
		}
		if cfg.Enabled {
			if name == "mention" {
				entry.Dampener = &mentionDamp
				entry.Contribution = signal * mentionDamp * weight
			} else {
				entry.Dampener = &dampenerOne
				entry.Contribution = signal * weight // dampener is 1.0 here
			}
		} else {
			entry.Contribution = signal * weight
		}
		out = append(out, entry)
	}
	return out
}

// weightForFactor returns the configured weight for a factor name
// without allocating an intermediate map. The factorOrder slice at the
// top of score.go is the source of truth for legal factor names.
func weightForFactor(name string, w config.Weights) float64 {
	switch name {
	case "mention":
		return w.Mention
	case "filename":
		return w.Filename
	case "symbol":
		return w.Symbol
	case "import":
		return w.Import
	case "package":
		return w.Package
	case "test":
		return w.Test
	case "doc":
		return w.Doc
	case "config":
		return w.Config
	}
	return 0
}
