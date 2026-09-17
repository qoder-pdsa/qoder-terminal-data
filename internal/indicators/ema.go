package indicators

import "github.com/qoder-pdsa/qoder-terminal-data/internal/money"

// EMA computes the exponential moving average. The result has the same length as the input, with nil
// for the first window-1 positions. When window <= 0 every value is nil.
//
// The first valid value, at index window-1, is the SMA of the first window closes; every later value
// recurses with k = 2/(window+1):
//
//	ema = k*close + (1-k)*ema_prev
//
// That recursion is evaluated in the algebraically identical integer form
//
//	ema = (2*close + (window-1)*ema_prev) / (window+1)
//
// so no float64 is involved and each step rounds once, half away from zero via money.DivInt, exactly
// like SMA.
func EMA(closes []money.Decimal, window int) []*money.Decimal {
	out := make([]*money.Decimal, len(closes))
	if window <= 0 {
		return out
	}
	var sum money.Decimal
	for i, c := range closes {
		switch {
		case i < window-1:
			sum = sum.Add(c)
		case i == window-1:
			seed := sum.Add(c).DivInt(int64(window))
			out[i] = &seed
		default:
			prev := *out[i-1]
			next := c.MulInt(2).Add(prev.MulInt(int64(window - 1))).DivInt(int64(window + 1))
			out[i] = &next
		}
	}
	return out
}
