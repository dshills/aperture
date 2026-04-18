// Package order models the orders service. Named after a non-overlapping
// domain so it won't compete with a billing-scoped task.
package order

// Order is the orders-service aggregate.
type Order struct {
	ID string
}

// Ship marks an order shipped.
func Ship(o *Order) error { o.ID = "shipped-" + o.ID; return nil }
