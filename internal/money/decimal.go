// Package money provides fixed-point decimals to avoid floating-point errors.
package money

import (
	"fmt"
	"strconv"
	"strings"
)

// Scale is the number of fractional digits (4, enough for HK prices and percentages).
const Scale = 4

const unit = 10000

// Decimal is a fixed-point number stored in minimal units (1e-4).
type Decimal struct{ units int64 }

// FromUnits builds a Decimal from minimal units.
func FromUnits(units int64) Decimal { return Decimal{units: units} }

// Parse parses a decimal string (e.g. "388.200"). Digits beyond Scale are rounded half away from zero.
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

// Units returns the value in minimal units.
func (d Decimal) Units() int64 { return d.units }

// Add returns d + o.
func (d Decimal) Add(o Decimal) Decimal { return Decimal{units: d.units + o.units} }

// Sub returns d - o.
func (d Decimal) Sub(o Decimal) Decimal { return Decimal{units: d.units - o.units} }

// DivInt returns d / n rounded half away from zero; n must be greater than 0.
func (d Decimal) DivInt(n int64) Decimal {
	half := n / 2
	if d.units < 0 {
		return Decimal{units: (d.units - half) / n}
	}
	return Decimal{units: (d.units + half) / n}
}

// PercentOf returns d as a percentage of base; returns 0 when base is 0.
func (d Decimal) PercentOf(base Decimal) Decimal {
	if base.units == 0 {
		return Decimal{}
	}
	return Decimal{units: d.units * 100 * unit / base.units}
}

// String formats the value with exactly Scale fractional digits, e.g. "123.4500".
func (d Decimal) String() string {
	sign := ""
	u := d.units
	if u < 0 {
		sign, u = "-", -u
	}
	return fmt.Sprintf("%s%d.%0*d", sign, u/unit, Scale, u%unit)
}
