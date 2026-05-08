// Package currency provides helpers for formatting DD$ amounts.
package currency

import "fmt"

// Format converts a cent amount to a human-readable string.
// 10050 → "DD$ 100.50", -500 → "-DD$ 5.00"
func Format(cents int64, symbol string) string {
	sign := ""
	if cents < 0 {
		sign = "-"
		cents = -cents
	}
	return fmt.Sprintf("%s%s %d.%02d", sign, symbol, cents/100, cents%100)
}
