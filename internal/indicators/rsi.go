package indicators

import "github.com/qoder-pdsa/qoder-terminal-data/internal/money"

// RSI computes the Relative Strength Index with Wilder smoothing. The result has the same length as the
// input, with nil for the first window positions: the first value, at index window, needs window
// close-to-close changes. When window <= 0 every value is nil.
//
// avgGain and avgLoss are seeded with the mean of the first window changes and then smoothed with
//
//	avg = (avg*(window-1) + value) / window
//
// The index is reported as 100*avgGain/(avgGain+avgLoss), algebraically identical to the usual
// 100 - 100/(1+avgGain/avgLoss) but needing no division by a ratio, so it stays inside money.Decimal
// and can never divide by zero. Both averages are held at money.Scale, so a result may differ from an
// arbitrary-precision reference in the last digits (see the window 3 case in rsi_test.go).
//
// Convention for a flat window: when both smoothed averages are zero — every close in the window is
// equal, so there are neither gains nor losses — the ratio is 0/0 and RSI is undefined. This
// implementation reports the neutral 50.0000, because a series that never moved is neither overbought
// nor oversold; reporting 0 or 100 would claim a direction the data does not have.
func RSI(closes []money.Decimal, window int) []*money.Decimal {
	out := make([]*money.Decimal, len(closes))
	if window <= 0 {
		return out
	}
	flat := money.FromInt(50)
	w := int64(window)
	var gainSum, lossSum, avgGain, avgLoss money.Decimal
	for i := 1; i < len(closes); i++ {
		change := closes[i].Sub(closes[i-1])
		var gain, loss money.Decimal
		switch {
		case change.Units() > 0:
			gain = change
		case change.Units() < 0:
			loss = money.FromUnits(-change.Units())
		}
		switch {
		case i < window:
			gainSum, lossSum = gainSum.Add(gain), lossSum.Add(loss)
			continue
		case i == window:
			avgGain, avgLoss = gainSum.Add(gain).DivInt(w), lossSum.Add(loss).DivInt(w)
		default:
			avgGain = avgGain.MulInt(w - 1).Add(gain).DivInt(w)
			avgLoss = avgLoss.MulInt(w - 1).Add(loss).DivInt(w)
		}
		value := flat
		if total := avgGain.Add(avgLoss); total.Units() != 0 {
			value = avgGain.PercentOf(total)
		}
		out[i] = &value
	}
	return out
}
