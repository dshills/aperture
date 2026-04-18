// Package invoice implements the billing service's Invoice type.
package invoice

// Invoice is the billing domain aggregate. The task anchors target
// this type.
type Invoice struct {
	ID     string
	Amount int64
}

// Finalize closes out an Invoice for dispatch.
func Finalize(inv *Invoice) error {
	inv.ID = "final-" + inv.ID
	return nil
}
