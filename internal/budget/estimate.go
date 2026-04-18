package budget

// Phase-3 overheads applied on top of the raw text estimate. These cover
// the JSON scaffold plus a small header each selection adds to the merged
// prompt. Upward bias per §7.6.1.1.
const (
	fullHeaderOverheadTokens    = 32
	summaryHeaderOverheadTokens = 24
)

// EstimateBytes returns the estimator's count for a raw byte slice.
//
// Note on memory: `string(b)` copies the slice once, so peak usage is
// about 2× the file size during the call. The copy is unavoidable given
// the Estimator interface accepts strings; a bytes-flavored Count method
// would eliminate it at the cost of complicating every estimator
// implementation (and v1 tiktoken-go wants strings anyway). The walker
// caps individual files at 10 MiB (§7.4.3), bounding the transient
// allocation; callers process files sequentially so this doesn't
// accumulate. A bytes-native Count is a documented future optimization.
func EstimateBytes(e Estimator, b []byte) int {
	if len(b) == 0 {
		return 0
	}
	return e.Count(string(b))
}

// EstimateFullBytes returns the cost of including the full content of a
// file, including the per-selection header overhead.
func EstimateFullBytes(e Estimator, content []byte) int {
	return EstimateBytes(e, content) + fullHeaderOverheadTokens
}

// EstimateFull is the string-flavored form of EstimateFullBytes, kept for
// call-sites that already have summaries rendered as strings.
func EstimateFull(e Estimator, content string) int {
	return e.Count(content) + fullHeaderOverheadTokens
}

// EstimateSummary returns the cost of a pre-rendered summary (structural
// or behavioral) plus the summary header overhead.
func EstimateSummary(e Estimator, summary string) int {
	return e.Count(summary) + summaryHeaderOverheadTokens
}
