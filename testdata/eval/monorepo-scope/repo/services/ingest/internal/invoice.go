// Package invoice is the ingest service's own invoice model. It is
// deliberately NAMED identically to billing's Invoice so that in a
// full-repo plan, s_symbol on the billing task's "Invoice" anchor hits
// BOTH files. --scope services/billing must limit ownership resolution
// to billing only (§7.4.1), suppressing this file from the candidate
// pool entirely.
package invoice

// Invoice is ingest's representation of a received invoice event.
type Invoice struct {
	EventID      string
	PayloadBytes []byte
}

// Parse deserializes an ingest-side Invoice payload.
func Parse(b []byte) (*Invoice, error) {
	return &Invoice{EventID: "evt-" + string(b[:min(4, len(b))]), PayloadBytes: b}, nil
}
