// Package order defines an order's lifecycle states.
package order

// Status is an order's lifecycle state.
type Status int

const (
	Pending Status = iota
	Paid
	Shipped
	Refunded
)
