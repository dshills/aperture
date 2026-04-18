// Package task parses raw task input into a structured Task per SPEC §7.3.
package task

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/dshills/aperture/internal/manifest"
)

// Task is the structured form of the user-supplied task input.
type Task struct {
	TaskID             string
	Source             string
	RawText            string
	Type               manifest.ActionType
	Objective          string
	Anchors            []string
	ExpectsTests       bool
	ExpectsConfig      bool
	ExpectsDocs        bool
	ExpectsMigration   bool
	ExpectsAPIContract bool
}

// ParseOptions controls task parsing.
type ParseOptions struct {
	// Source is the user-facing path or "<inline>" for -p input.
	Source string
	// IsMarkdown determines whether §7.3.2 rule 3 (backtick-quoted code
	// spans) is applied. Only `.md`, `.markdown`, `.mdx` files trigger it.
	IsMarkdown bool
}

// Parse runs the deterministic task parser from SPEC §7.3 on rawText and
// returns the populated Task. All inference is rule-based; no LLM is used.
func Parse(rawText string, opts ParseOptions) Task {
	t := Task{
		TaskID:    taskID(rawText),
		Source:    opts.Source,
		RawText:   rawText,
		Objective: firstNonEmptyLine(rawText),
	}

	lowered := strings.ToLower(rawText)
	t.Type = classifyActionType(lowered)
	t.Anchors = extractAnchors(rawText, lowered, opts.IsMarkdown)
	t.ExpectsTests, t.ExpectsConfig, t.ExpectsDocs, t.ExpectsMigration, t.ExpectsAPIContract =
		heuristicBooleans(t.Type, lowered, t.Anchors)
	return t
}

// IsMarkdownPath reports whether filename triggers rule 3 extraction.
func IsMarkdownPath(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, ".md") ||
		strings.HasSuffix(lower, ".markdown") ||
		strings.HasSuffix(lower, ".mdx")
}

func taskID(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return "tsk_" + hex.EncodeToString(sum[:])[:16]
}

func firstNonEmptyLine(raw string) string {
	for _, ln := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(ln)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// actionRule is one row of the §7.3.1.1 table.
type actionRule struct {
	Type    manifest.ActionType
	Pattern *regexp.Regexp
}

// actionRules is evaluated in Order; the first match wins. Patterns are
// compiled with `\b` word boundaries and applied to lowercased task text.
// The priority here MUST match §7.3.1.1 verbatim.
var actionRules = []actionRule{
	{manifest.ActionTypeBugfix, regexp.MustCompile(`\b(fix|bug|broken|regression|crash|panic|error is|fails to|should not|incorrect)\b`)},
	{manifest.ActionTypeTestAddition, regexp.MustCompile(`\b(add tests?|write tests?|test coverage|unit tests?|integration tests?|missing tests?)\b`)},
	{manifest.ActionTypeDocumentation, regexp.MustCompile(`\b(document|docs?|readme|comments?|godoc|javadoc)\b`)},
	{manifest.ActionTypeMigration, regexp.MustCompile(`\b(migrate|migration|upgrade|downgrade|backfill|rename column|drop column|schema change)\b`)},
	{manifest.ActionTypeRefactor, regexp.MustCompile(`\b(refactor|rewrite|restructure|clean up|cleanup|extract|split|deduplicate)\b`)},
	{manifest.ActionTypeInvestigation, regexp.MustCompile(`\b(investigate|explore|understand|research|look into|diagnose|why does|how does)\b`)},
	{manifest.ActionTypeFeature, regexp.MustCompile(`\b(add|implement|support|introduce|new|create|enable)\b`)},
}

func classifyActionType(lowered string) manifest.ActionType {
	for _, r := range actionRules {
		if r.Pattern.MatchString(lowered) {
			return r.Type
		}
	}
	return manifest.ActionTypeUnknown
}

var (
	// Rule 1: [A-Z][A-Za-z0-9_]{2,}
	identifierRe = regexp.MustCompile(`[A-Z][A-Za-z0-9_]{2,}`)
	// Rule 2: bare filenames/paths ending in a known extension (case-insensitive).
	filenameRe = regexp.MustCompile(`(?i)[A-Za-z0-9_./-]+\.(go|md|yaml|yml|json|toml|proto|sql|ts|tsx|js|py|sh)`)
	// Rule 4 tokenizer: word of length ≥4 made of letters/digits.
	wordRe = regexp.MustCompile(`[A-Za-z0-9]{4,}`)
)

func extractAnchors(raw, lowered string, isMarkdown bool) []string {
	set := map[string]struct{}{}

	for _, m := range identifierRe.FindAllString(raw, -1) {
		set[m] = struct{}{}
	}

	for _, m := range filenameRe.FindAllString(raw, -1) {
		set[m] = struct{}{}
	}

	if isMarkdown {
		for _, span := range extractBacktickSpans(raw) {
			if span != "" {
				set[span] = struct{}{}
			}
		}
	}

	for _, w := range wordRe.FindAllString(lowered, -1) {
		cleaned := alnumLower(w)
		if len(cleaned) < 4 {
			continue
		}
		if _, stop := stopwords[cleaned]; stop {
			continue
		}
		set[cleaned] = struct{}{}
	}

	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// extractBacktickSpans implements §7.3.2 rule 3: pair the first and next
// unescaped backtick on each line in a single deterministic pass. No
// external Markdown library.
func extractBacktickSpans(raw string) []string {
	var out []string
	for _, line := range strings.Split(raw, "\n") {
		runes := []rune(line)
		i := 0
		for i < len(runes) {
			if runes[i] == '`' && !isEscaped(runes, i) {
				// find closing backtick on the same line
				j := i + 1
				for j < len(runes) {
					if runes[j] == '`' && !isEscaped(runes, j) {
						break
					}
					j++
				}
				if j < len(runes) {
					out = append(out, string(runes[i+1:j]))
					i = j + 1
					continue
				}
				// unmatched opener — abandon the rest of the line
				break
			}
			i++
		}
	}
	return out
}

func isEscaped(runes []rune, i int) bool {
	// A backtick is escaped iff preceded by an odd number of backslashes.
	n := 0
	for k := i - 1; k >= 0 && runes[k] == '\\'; k-- {
		n++
	}
	return n%2 == 1
}

func alnumLower(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

func heuristicBooleans(a manifest.ActionType, lowered string, anchors []string) (tests, cfg, docs, mig, api bool) {
	tests = actionIn(a, manifest.ActionTypeFeature, manifest.ActionTypeBugfix,
		manifest.ActionTypeRefactor, manifest.ActionTypeMigration, manifest.ActionTypeTestAddition) ||
		containsWord(lowered, "test", "spec", "verify", "assert")

	cfg = anchorMatches(anchors, []string{
		"config", "env", "environment", "settings", "flag", "flags", "secret",
		"database", "db", "port", "host", "url", "token",
		"yaml", "yml", "toml", "dotenv",
	})

	docs = a == manifest.ActionTypeDocumentation ||
		containsWord(lowered, "document", "readme", "doc", "docs", "adr", "design doc")

	mig = a == manifest.ActionTypeMigration ||
		containsWord(lowered, "migration", "schema", "column", "table", "index", "backfill")

	api = anchorMatches(anchors, []string{
		"api", "rpc", "grpc", "openapi", "swagger", "graphql",
		"proto", "protobuf", "contract", "schema", "interface",
	})
	return
}

func actionIn(a manifest.ActionType, set ...manifest.ActionType) bool {
	for _, s := range set {
		if a == s {
			return true
		}
	}
	return false
}

func containsWord(lowered string, words ...string) bool {
	for _, w := range words {
		// whole-word via surrounding non-letter boundary.
		pat := regexp.MustCompile(`\b` + regexp.QuoteMeta(w) + `\b`)
		if pat.MatchString(lowered) {
			return true
		}
	}
	return false
}

func anchorMatches(anchors []string, keywords []string) bool {
	want := map[string]struct{}{}
	for _, k := range keywords {
		want[k] = struct{}{}
	}
	for _, a := range anchors {
		if _, ok := want[strings.ToLower(a)]; ok {
			return true
		}
	}
	return false
}
