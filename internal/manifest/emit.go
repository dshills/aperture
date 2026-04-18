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

// EmitMarkdown renders a minimal Phase-1 Markdown manifest carrying every
// §7.9.3 section header. Bodies beyond the task summary are stubbed.
func EmitMarkdown(m *Manifest) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "# Aperture Manifest %s\n\n", m.ManifestID)
	fmt.Fprintf(&b, "schema_version: %s\n\n", m.SchemaVersion)

	b.WriteString("## Task Summary\n\n")
	fmt.Fprintf(&b, "- task_id: %s\n", m.Task.TaskID)
	fmt.Fprintf(&b, "- type: %s\n", m.Task.Type)
	fmt.Fprintf(&b, "- objective: %s\n\n", m.Task.Objective)

	b.WriteString("## Planning Assumptions\n\n")
	fmt.Fprintf(&b, "- model: %s\n", m.Budget.Model)
	fmt.Fprintf(&b, "- token_ceiling: %d\n", m.Budget.TokenCeiling)
	fmt.Fprintf(&b, "- effective_context_budget: %d\n\n", m.Budget.EffectiveContextBudget)

	b.WriteString("## Selected Full Context\n\n")
	b.WriteString("_(none)_\n\n")

	b.WriteString("## Selected Summaries\n\n")
	b.WriteString("_(none)_\n\n")

	b.WriteString("## Reachable Context\n\n")
	b.WriteString("_(none)_\n\n")

	b.WriteString("## Gaps\n\n")
	b.WriteString("_(none)_\n\n")

	b.WriteString("## Feasibility\n\n")
	fmt.Fprintf(&b, "- score: %s\n\n", strconv.FormatFloat(m.Feasibility.Score, 'f', -1, 64))

	b.WriteString("## Token Accounting\n\n")
	fmt.Fprintf(&b, "- estimator: %s\n", m.Budget.Estimator)
	fmt.Fprintf(&b, "- estimator_version: %s\n", m.Budget.EstimatorVersion)
	fmt.Fprintf(&b, "- estimated_selected_tokens: %d\n\n", m.Budget.EstimatedSelectedTokens)

	b.WriteString("## Usage Instructions\n\n")
	b.WriteString("Load the files listed under selections; follow up on reachable entries as needed.\n")

	return b.Bytes()
}
