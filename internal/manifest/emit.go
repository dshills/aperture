package manifest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// EmitJSON returns the pretty-printed JSON form of m, suitable for writing
// to disk. Key order within objects matches the struct field order for the
// human-readable form; the hash is always computed from the lexicographically-
// sorted compact form in hash.go and is unaffected by pretty formatting.
func EmitJSON(m *Manifest) ([]byte, error) {
	return json.MarshalIndent(m, "", "  ")
}

// marshalCanonical marshals m to JSON and re-parses it into generic Go
// maps/slices so downstream normalization can sort keys and strip fields
// without needing per-struct knowledge.
func marshalCanonical(m *Manifest) (any, error) {
	buf, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	var v any
	if err := json.Unmarshal(buf, &v); err != nil {
		return nil, err
	}
	return v, nil
}

// stripHashExcluded removes the dotted paths in hashExcludedPaths from v.
// v is expected to be the result of json.Unmarshal into any.
func stripHashExcluded(v any) (any, error) {
	for _, p := range hashExcludedPaths {
		parts := strings.Split(p, ".")
		removePath(v, parts)
	}
	return v, nil
}

func removePath(v any, parts []string) {
	if len(parts) == 0 {
		return
	}
	m, ok := v.(map[string]any)
	if !ok {
		return
	}
	if len(parts) == 1 {
		delete(m, parts[0])
		return
	}
	next, ok := m[parts[0]]
	if !ok {
		return
	}
	removePath(next, parts[1:])
}

// compactSortedJSON writes v as compact JSON with object keys sorted
// lexicographically (byte-wise) at every level. Numbers are emitted in a
// canonical form (integers without decimals; floats via strconv with
// shortest-precision round-trip).
func compactSortedJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	if err := writeCanonical(&buf, v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeCanonical(buf *bytes.Buffer, v any) error {
	switch x := v.(type) {
	case nil:
		buf.WriteString("null")
	case bool:
		if x {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
	case string:
		return writeJSONString(buf, x)
	case json.Number:
		return writeJSONNumber(buf, string(x))
	case float64:
		return writeJSONFloat(buf, x)
	case int:
		buf.WriteString(strconv.FormatInt(int64(x), 10))
	case int64:
		buf.WriteString(strconv.FormatInt(x, 10))
	case []any:
		buf.WriteByte('[')
		for i, item := range x {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := writeCanonical(buf, item); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		buf.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := writeJSONString(buf, k); err != nil {
				return err
			}
			buf.WriteByte(':')
			if err := writeCanonical(buf, x[k]); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
	default:
		return fmt.Errorf("unsupported type %T in canonical JSON", v)
	}
	return nil
}

func writeJSONFloat(buf *bytes.Buffer, f float64) error {
	if f == float64(int64(f)) && !isNegativeZero(f) {
		buf.WriteString(strconv.FormatInt(int64(f), 10))
		return nil
	}
	buf.WriteString(strconv.FormatFloat(f, 'f', -1, 64))
	return nil
}

func isNegativeZero(f float64) bool {
	return f == 0 && 1/f < 0
}

func writeJSONNumber(buf *bytes.Buffer, s string) error {
	// json.Number carries the raw literal; re-parse as float64 to normalize.
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return err
	}
	return writeJSONFloat(buf, f)
}

func writeJSONString(buf *bytes.Buffer, s string) error {
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	buf.Write(b)
	return nil
}

// EmitMarkdown renders the §7.9.3 Markdown form of m. The output is
// deterministic modulo the hash-excluded fields baked into the manifest
// (generated_at, host, pid); callers that want a byte-stable doc for
// golden tests should strip those before diffing.
func EmitMarkdown(m *Manifest) []byte {
	if m == nil {
		return nil
	}
	var b bytes.Buffer

	fmt.Fprintf(&b, "# Aperture Manifest %s\n\n", m.ManifestID)
	fmt.Fprintf(&b, "- schema_version: %s\n", m.SchemaVersion)
	fmt.Fprintf(&b, "- manifest_hash: %s\n", m.ManifestHash)
	if m.Incomplete {
		b.WriteString("- incomplete: true\n")
	}
	b.WriteString("\n")

	mdTaskSummary(&b, m)
	mdPlanningAssumptions(&b, m)
	mdSelectedFullContext(&b, m)
	mdSelectedSummaries(&b, m)
	mdReachableContext(&b, m)
	mdGaps(&b, m)
	mdFeasibility(&b, m)
	mdTokenAccounting(&b, m)
	mdUsageInstructions(&b, m)

	return b.Bytes()
}

func mdTaskSummary(b *bytes.Buffer, m *Manifest) {
	b.WriteString("## Task Summary\n\n")
	fmt.Fprintf(b, "- task_id: %s\n", m.Task.TaskID)
	fmt.Fprintf(b, "- type: %s\n", m.Task.Type)
	fmt.Fprintf(b, "- source: %s\n", m.Task.Source)
	fmt.Fprintf(b, "- objective: %s\n", m.Task.Objective)
	if len(m.Task.Anchors) > 0 {
		fmt.Fprintf(b, "- anchors: %s\n", strings.Join(m.Task.Anchors, ", "))
	}
	b.WriteString("- expects:")
	if m.Task.ExpectsTests {
		b.WriteString(" tests")
	}
	if m.Task.ExpectsConfig {
		b.WriteString(" config")
	}
	if m.Task.ExpectsDocs {
		b.WriteString(" docs")
	}
	if m.Task.ExpectsMigration {
		b.WriteString(" migration")
	}
	if m.Task.ExpectsAPIContract {
		b.WriteString(" api_contract")
	}
	b.WriteString("\n\n")
}

func mdPlanningAssumptions(b *bytes.Buffer, m *Manifest) {
	b.WriteString("## Planning Assumptions\n\n")
	fmt.Fprintf(b, "- repo_root: %s\n", m.Repo.Root)
	fmt.Fprintf(b, "- repo_fingerprint: %s\n", m.Repo.Fingerprint)
	if len(m.Repo.LanguageHints) > 0 {
		fmt.Fprintf(b, "- language_hints: %s\n", strings.Join(m.Repo.LanguageHints, ", "))
	}
	fmt.Fprintf(b, "- model: %s\n", m.Budget.Model)
	fmt.Fprintf(b, "- token_ceiling: %d\n", m.Budget.TokenCeiling)
	fmt.Fprintf(b, "- effective_context_budget: %d\n\n", m.Budget.EffectiveContextBudget)
}

func mdSelectedFullContext(b *bytes.Buffer, m *Manifest) {
	b.WriteString("## Selected Full Context\n\n")
	any := false
	for _, s := range m.Selections {
		if s.LoadMode != LoadModeFull {
			continue
		}
		any = true
		fmt.Fprintf(b, "- `%s` — score %s, tokens %d\n", s.Path, formatScore(s.RelevanceScore), s.EstimatedTokens)
		if s.DemotionReason != nil && *s.DemotionReason != "" {
			fmt.Fprintf(b, "  - demotion_reason: %s\n", *s.DemotionReason)
		}
		if len(s.Rationale) > 0 {
			fmt.Fprintf(b, "  - rationale: %s\n", strings.Join(s.Rationale, "; "))
		}
	}
	if !any {
		b.WriteString("_(none selected as full)_\n")
	}
	b.WriteString("\n")
}

func mdSelectedSummaries(b *bytes.Buffer, m *Manifest) {
	b.WriteString("## Selected Summaries\n\n")
	any := false
	for _, s := range m.Selections {
		if s.LoadMode != LoadModeStructuralSummary && s.LoadMode != LoadModeBehavioralSummary {
			continue
		}
		any = true
		fmt.Fprintf(b, "- `%s` — %s, score %s, tokens %d\n", s.Path, s.LoadMode, formatScore(s.RelevanceScore), s.EstimatedTokens)
		if s.DemotionReason != nil && *s.DemotionReason != "" {
			fmt.Fprintf(b, "  - demotion_reason: %s\n", *s.DemotionReason)
		}
		if len(s.Rationale) > 0 {
			fmt.Fprintf(b, "  - rationale: %s\n", strings.Join(s.Rationale, "; "))
		}
	}
	if !any {
		b.WriteString("_(none selected as summaries)_\n")
	}
	b.WriteString("\n")
}

func mdReachableContext(b *bytes.Buffer, m *Manifest) {
	b.WriteString("## Reachable Context\n\n")
	if len(m.Reachable) == 0 {
		b.WriteString("_(none)_\n\n")
		return
	}
	for _, r := range m.Reachable {
		fmt.Fprintf(b, "- `%s` — score %s (%s)\n", r.Path, formatScore(r.RelevanceScore), r.Reason)
	}
	b.WriteString("\n")
}

func mdGaps(b *bytes.Buffer, m *Manifest) {
	b.WriteString("## Gaps\n\n")
	if len(m.Gaps) == 0 {
		b.WriteString("_(none)_\n\n")
		return
	}
	for _, g := range m.Gaps {
		fmt.Fprintf(b, "- **%s** [%s/%s] — %s\n", g.ID, g.Type, g.Severity, g.Description)
		for _, e := range g.Evidence {
			fmt.Fprintf(b, "  - evidence: %s\n", e)
		}
		for _, r := range g.SuggestedRemediation {
			fmt.Fprintf(b, "  - remediation: %s\n", r)
		}
	}
	b.WriteString("\n")
}

func mdFeasibility(b *bytes.Buffer, m *Manifest) {
	b.WriteString("## Feasibility\n\n")
	fmt.Fprintf(b, "- score: %s (%s)\n", formatScore(m.Feasibility.Score), m.Feasibility.Assessment)
	for _, key := range []string{"coverage", "anchor_resolution", "task_specificity", "budget_headroom", "gap_penalty"} {
		if v, ok := m.Feasibility.SubSignals[key]; ok {
			fmt.Fprintf(b, "  - %s: %s\n", key, formatScore(v))
		}
	}
	if len(m.Feasibility.Positives) > 0 {
		fmt.Fprintf(b, "- positives: %s\n", strings.Join(m.Feasibility.Positives, "; "))
	}
	if len(m.Feasibility.Negatives) > 0 {
		fmt.Fprintf(b, "- negatives: %s\n", strings.Join(m.Feasibility.Negatives, "; "))
	}
	for _, bc := range m.Feasibility.BlockingConditions {
		fmt.Fprintf(b, "- **blocking**: %s\n", bc)
	}
	b.WriteString("\n")
}

func mdTokenAccounting(b *bytes.Buffer, m *Manifest) {
	b.WriteString("## Token Accounting\n\n")
	fmt.Fprintf(b, "- estimator: %s (%s)\n", m.Budget.Estimator, m.Budget.EstimatorVersion)
	fmt.Fprintf(b, "- token_ceiling: %d\n", m.Budget.TokenCeiling)
	fmt.Fprintf(b, "- reserved: instructions=%d reasoning=%d tool_output=%d expansion=%d\n",
		m.Budget.Reserved.Instructions, m.Budget.Reserved.Reasoning,
		m.Budget.Reserved.ToolOutput, m.Budget.Reserved.Expansion)
	fmt.Fprintf(b, "- effective_context_budget: %d\n", m.Budget.EffectiveContextBudget)
	fmt.Fprintf(b, "- estimated_selected_tokens: %d\n\n", m.Budget.EstimatedSelectedTokens)
}

func mdUsageInstructions(b *bytes.Buffer, m *Manifest) {
	b.WriteString("## Usage Instructions\n\n")
	b.WriteString("Downstream agents should:\n\n")
	b.WriteString("1. Load every file listed under **Selected Full Context** verbatim.\n")
	b.WriteString("2. Treat entries under **Selected Summaries** as pre-condensed views; consult the underlying file only when the summary leaves a concrete question unanswered.\n")
	b.WriteString("3. Use **Reachable Context** as a discovery list — do not load it by default; fetch on demand when a selection's summary is insufficient.\n")
	b.WriteString("4. Address every **blocking** gap before taking irreversible action; surface warnings to the user.\n")
	if m.Incomplete {
		b.WriteString("\n> **Warning:** this manifest is marked `incomplete`. The planner failed to fit any viable selection within the effective budget. Do NOT proceed without re-planning at a larger budget.\n")
	}
}

func formatScore(f float64) string {
	return strconv.FormatFloat(f, 'f', 4, 64)
}
