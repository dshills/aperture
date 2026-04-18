package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
)

// Validate enforces the config correctness rules mandated by SPEC §7.4.2.2,
// §7.4.3, §9.1, and §9.1.2. A non-nil error from Validate maps to exit
// code 5 (§16).
func (c Config) Validate() error {
	if sum := c.Scoring.Weights.Sum(); math.Abs(sum-1.0) > 0.001 {
		return fmt.Errorf("scoring.weights must sum to 1.0 ±0.001 (got %.6f)", sum)
	}
	for name, a := range c.Agents {
		if a.Command == "" {
			return fmt.Errorf("agents.%s.command is required", name)
		}
	}
	switch c.Output.Format {
	case "", "json", "markdown":
	default:
		return fmt.Errorf("output.format must be json or markdown (got %q)", c.Output.Format)
	}
	return nil
}

// canonicalJSON serializes a generic Go value produced by json.Unmarshal as
// compact JSON with lexicographically-sorted keys at every level and with
// numbers written in the canonical form used by the manifest hash.
func canonicalJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	if err := writeCanon(&buf, v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeCanon(buf *bytes.Buffer, v any) error {
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
		b, err := json.Marshal(x)
		if err != nil {
			return err
		}
		buf.Write(b)
	case float64:
		if x == float64(int64(x)) {
			buf.WriteString(strconv.FormatInt(int64(x), 10))
		} else {
			buf.WriteString(strconv.FormatFloat(x, 'f', -1, 64))
		}
	case []any:
		buf.WriteByte('[')
		for i, item := range x {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := writeCanon(buf, item); err != nil {
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
			b, err := json.Marshal(k)
			if err != nil {
				return err
			}
			buf.Write(b)
			buf.WriteByte(':')
			if err := writeCanon(buf, x[k]); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
	default:
		return fmt.Errorf("unsupported type %T in canonical json", v)
	}
	return nil
}
