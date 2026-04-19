// Package charge is the Go side of the combined polyglot+scope
// fixture. The fixture's task mentions RefundCharge plus the
// Python sibling's refund_charge helper.
package charge

// RefundCharge rolls back a previously-captured charge.
type RefundCharge struct {
	ID     string
	Amount int64
}

// Apply commits the refund.
func (r *RefundCharge) Apply() error { return nil }
