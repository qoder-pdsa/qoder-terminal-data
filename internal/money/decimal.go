// Package money 提供定点十进制数，避免浮点误差。
package money

import (
	"fmt"
	"strconv"
	"strings"
)

// Scale 是小数位数（4 位，覆盖港股价格与百分比）。
const Scale = 4

const unit = 10000

// Decimal 以最小单位（1e-4）存储的定点数。
type Decimal struct{ units int64 }

// FromUnits 由最小单位构造。
func FromUnits(units int64) Decimal { return Decimal{units: units} }

// Parse 解析十进制字符串（如 "388.200"）。超过 Scale 的小数位四舍五入（远离零）。
func Parse(s string) (Decimal, error) {
	raw := strings.TrimSpace(s)
	neg := strings.HasPrefix(raw, "-")
	body := strings.TrimPrefix(raw, "-")
	intPart, fracPart, _ := strings.Cut(body, ".")
	if intPart == "" || strings.ContainsAny(intPart+fracPart, "+-") {
		return Decimal{}, fmt.Errorf("money: invalid decimal %q", s)
	}
	whole, err := strconv.ParseInt(intPart, 10, 64)
	if err != nil {
		return Decimal{}, fmt.Errorf("money: invalid decimal %q: %w", s, err)
	}
	roundUp := len(fracPart) > Scale && fracPart[Scale] >= '5'
	fracPart = (fracPart + strings.Repeat("0", Scale))[:Scale]
	frac, err := strconv.ParseInt(fracPart, 10, 64)
	if err != nil {
		return Decimal{}, fmt.Errorf("money: invalid decimal %q: %w", s, err)
	}
	units := whole*unit + frac
	if roundUp {
		units++
	}
	if neg {
		units = -units
	}
	return Decimal{units: units}, nil
}

// Units 返回最小单位值。
func (d Decimal) Units() int64 { return d.units }

// Add 返回 d + o。
func (d Decimal) Add(o Decimal) Decimal { return Decimal{units: d.units + o.units} }

// Sub 返回 d - o。
func (d Decimal) Sub(o Decimal) Decimal { return Decimal{units: d.units - o.units} }

// DivInt 返回 d / n，四舍五入（远离零）；n 必须大于 0。
func (d Decimal) DivInt(n int64) Decimal {
	half := n / 2
	if d.units < 0 {
		return Decimal{units: (d.units - half) / n}
	}
	return Decimal{units: (d.units + half) / n}
}

// PercentOf 返回 d 相对 base 的百分比；base 为 0 时返回 0。
func (d Decimal) PercentOf(base Decimal) Decimal {
	if base.units == 0 {
		return Decimal{}
	}
	return Decimal{units: d.units * 100 * unit / base.units}
}

// String 输出固定 Scale 位小数的字符串，如 "123.4500"。
func (d Decimal) String() string {
	sign := ""
	u := d.units
	if u < 0 {
		sign, u = "-", -u
	}
	return fmt.Sprintf("%s%d.%0*d", sign, u/unit, Scale, u%unit)
}
