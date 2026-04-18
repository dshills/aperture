package relevance

import "github.com/dshills/aperture/internal/config"

// DampenerConfig parameterizes the §7.2.2 mention dampener. Zero-value
// DampenerConfig{} is NOT a valid configuration — callers must resolve
// their config defaults (floor=0.30, slope=0.70) before invoking Dampen.
type DampenerConfig struct {
	Enabled bool
	Floor   float64
	Slope   float64
}

// DampenerFromConfig builds a DampenerConfig from the v1.1 config block.
// Centralized here so callers (the manifest builder invokes this twice —
// once for scoring, once for breakdown emission) can't drift out of sync.
func DampenerFromConfig(m config.MentionDampener) DampenerConfig {
	return DampenerConfig{
		Enabled: m.Enabled,
		Floor:   m.Floor,
		Slope:   m.Slope,
	}
}

// Dampen returns the v1.1 §7.2.2 dampener factor applied to s_mention:
//
//	dampener = min(1.0, floor + slope * otherMax)
//
// Where otherMax = max(s_symbol, s_filename, s_import, s_package). The
// caller is responsible for supplying an otherMax computed over exactly
// those four factors (§7.2.2); Dampen itself performs no factor
// selection so the rule stays in one place.
//
// Invariants (verified in unit tests):
//   - result ∈ [floor, 1.0] when enabled
//   - strictly monotone non-decreasing in otherMax
//   - returns 1.0 exactly when !enabled (pass-through)
//   - returns floor exactly when otherMax == 0
//
// Out-of-range floor/slope are clamped defensively: negative values
// clamp to 0, values above 1 clamp to 1. The config validator rejects
// out-of-range values up front (§7.2.3), so this clamp is a safety net
// for unit tests that pass synthetic inputs.
func Dampen(otherMax float64, cfg DampenerConfig) float64 {
	if !cfg.Enabled {
		return 1.0
	}
	floor := cfg.Floor
	if floor < 0 {
		floor = 0
	}
	if floor > 1 {
		floor = 1
	}
	slope := cfg.Slope
	if slope < 0 {
		slope = 0
	}
	if slope > 1 {
		slope = 1
	}
	om := otherMax
	if om < 0 {
		om = 0
	}
	if om > 1 {
		om = 1
	}
	v := floor + slope*om
	if v > 1 {
		return 1
	}
	return v
}

// OtherMaxForDampener returns max(s_symbol, s_filename, s_import,
// s_package) per §7.2.2. Stated as a named function so it appears
// exactly once in the codebase; the four-factor set is normative and
// bumping it requires a selection_logic_version bump (§7.2.2).
func OtherMaxForDampener(signals map[string]float64) float64 {
	m := signals["symbol"]
	if v := signals["filename"]; v > m {
		m = v
	}
	if v := signals["import"]; v > m {
		m = v
	}
	if v := signals["package"]; v > m {
		m = v
	}
	return m
}
