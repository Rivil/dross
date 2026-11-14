// Package seeded renders order labels and archives receipts.
package seeded

import (
	"os"

	"example.com/lensproof/order"
)

// Label returns the customer-facing label for an order status.
func Label(s order.Status) string {
	switch s {
	case order.Pending:
		return "awaiting payment"
	case order.Paid:
		return "paid"
	case order.Shipped:
		return "on its way"
	default:
		return ""
	}
}

// Archive writes the receipt and removes its scratch copy.
func Archive(path string, receipt []byte) error {
	if err := os.WriteFile(path, receipt, 0o600); err != nil {
		return err
	}
	_ = os.Remove(path + ".tmp")
	return nil
}
