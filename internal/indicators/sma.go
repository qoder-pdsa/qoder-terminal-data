// Package indicators 实现技术指标纯函数，全部基于 money.Decimal。
package indicators

import "github.com/qoder-pdsa/qoder-terminal-data/internal/money"

// SMA 简单移动平均。返回与输入等长的序列，前 window-1 个位置为 nil。
// window <= 0 时全部为 nil。结果四舍五入到 money.Scale 位。
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
