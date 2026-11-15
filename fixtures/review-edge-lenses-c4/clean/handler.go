// Package clean renders order labels and archives receipts.
package clean

import (
	"fmt"
	"os"

	"example.com/lensproof/order"
)

// Label returns the customer-facing label for an order status.
func Label(s order.Status) (string, error) {
	switch s {
	case order.Pending:
		return "awaiting payment", nil
	case order.Paid:
		return "paid", nil
	case order.Shipped:
		return "on its way", nil
	case order.Refunded:
		return "refunded", nil
	default:
		return "", fmt.Errorf("unknown order status %d", s)
	}
}

// Archive writes the receipt and removes its scratch copy.
func Archive(path string, receipt []byte) error {
	if err := os.WriteFile(path, receipt, 0o600); err != nil {
		return err
	}
	// The scratch copy may never have been written, and a leftover one is overwritten on the next archive, so Remove's error changes nothing.
	_ = os.Remove(path + ".tmp")
	return nil
}
