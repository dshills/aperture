package budget

// heuristic35 implements the conservative 3.5-bytes-per-token estimator
// used for Claude models and for unspecified / unrecognized models. It
// counts bytes (not runes) because the spec uses "len(utf8_bytes)".
type heuristic35 struct{}

// Compile-time assertion that heuristic35 implements Estimator.
var _ Estimator = heuristic35{}

// Heuristic35 returns a singleton heuristic estimator; its Identity /
// Version are stable across the process.
func Heuristic35() Estimator { return heuristic35{} }

func (heuristic35) Count(s string) int { return ceilDiv(len(s), 3.5) }
func (heuristic35) Identity() string   { return "heuristic-3.5" }
func (heuristic35) Version() string    { return "v1" }
