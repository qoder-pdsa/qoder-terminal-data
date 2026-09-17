// Package indicators implements technical indicators as pure functions on money.Decimal.
package indicators

import "github.com/qoder-pdsa/qoder-terminal-data/internal/money"

// SMA computes the simple moving average. The result has the same length as the input, with nil for the first window-1 positions.
// When window <= 0 every value is nil. Results are rounded to money.Scale digits.
func SMA(closes []money.Decimal, window int) []*money.Decimal {
	out := make([]*money.Decimal, len(closes))
	if window <= 0 {
		return out
	}
	var sum money.Decimal
	for i, c := range closes {
		sum = sum.Add(c)
		if i >= window {
			sum = sum.Sub(closes[i-window])
		}
		if i+1 >= window {
			avg := sum.DivInt(int64(window))
			out[i] = &avg
		}
	}
	return out
}
