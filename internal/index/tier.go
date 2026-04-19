package index

// LanguageTier is the v1.1 §5.4 analysis-capability tier for a file's
// language. Stored as the descriptive-name string inside
// FileEntry.LanguageTier so the manifest's language_tiers map (§10.1)
// renders the tier directly.
type LanguageTier string

const (
	// Tier1Deep covers symbols, imports, side-effect tags, test
	// linking — the v1 Go analyzer's full output.
	Tier1Deep LanguageTier = "tier1_deep"

	// Tier2Structural covers symbols, imports, test linking —
	// tree-sitter-powered analysis for JS/TS/Python in v1.1. No
	// side-effect tags.
	Tier2Structural LanguageTier = "tier2_structural"

	// Tier3Lexical is the fallback: only filename + doc tokens.
	// Languages neither tier-1 nor tier-2 land here, as do tier-2
	// languages whose config block is `enabled: false`.
	Tier3Lexical LanguageTier = "tier3_lexical"
)

// ResolveTierForLanguage returns the §5.4 descriptive-name tier for
// the given language string (as produced by repo.Walk). enabledTS /
// enabledJS / enabledPy mirror the `languages.<name>.enabled` config
// block — when the relevant flag is false, the language falls back
// from tier-2 to tier-3 (§9 config block).
func ResolveTierForLanguage(lang string, enabledTS, enabledJS, enabledPy bool) LanguageTier {
	switch lang {
	case "go":
		return Tier1Deep
	case "typescript":
		if enabledTS {
			return Tier2Structural
		}
		return Tier3Lexical
	case "javascript":
		if enabledJS {
			return Tier2Structural
		}
		return Tier3Lexical
	case "python":
		if enabledPy {
			return Tier2Structural
		}
		return Tier3Lexical
	}
	return Tier3Lexical
}
