package money

// Characterization tests locking the PRE-CHANGE behaviour of Decimal.
//
// Purpose: BL-01 needs decimal-only arithmetic for EMA (k = 2/(window+1)) and RSI (Wilder
// smoothing, 100 - 100/(1+RS)), so new helpers are added to this shared type. Every existing
// method must keep its exact behaviour and rounding, because SMA, the quote maths and the
// Longbridge mapping all depend on it. These tests pin that, including the rounding direction
// of DivInt and the truncation (not rounding) of PercentOf.
//
// Group selection: go test ./... -run TestCharacterization
// Baseline: actually run on the unchanged tree at commit 35f33b3 (evidence/baseline-characterization.log).

import "testing"

func TestCharacterizationDecimalAddSub(t *testing.T) {
	tests := []struct {
		name    string
		a, b    int64
		wantAdd int64
		wantSub int64
	}{
		{"positive", 12345, 6789, 19134, 5556},
		{"zero", 0, 500, 500, -500},
		{"negative operand", 10000, -2500, 7500, 12500},
		{"both negative", -100, -250, -350, 150},
		{"sub unit precision", 1, 1, 2, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, b := FromUnits(tt.a), FromUnits(tt.b)
			if got := a.Add(b).Units(); got != tt.wantAdd {
				t.Errorf("Add = %d, want %d", got, tt.wantAdd)
			}
			if got := a.Sub(b).Units(); got != tt.wantSub {
				t.Errorf("Sub = %d, want %d", got, tt.wantSub)
			}
		})
	}
}

// TestCharacterizationDecimalDivIntRounding locks half-away-from-zero rounding in both directions.
// EMA and RSI will divide by an integer count, so this direction must not drift.
func TestCharacterizationDecimalDivIntRounding(t *testing.T) {
	tests := []struct {
		name    string
		units   int64
		n       int64
		want    int64
		wantStr string
	}{
		{"exact", 10000, 4, 2500, "0.2500"},
		{"half rounds up", 10, 4, 3, "0.0003"},
		{"negative half rounds away from zero", -10, 4, -3, "-0.0003"},
		{"below half rounds down", 7, 4, 2, "0.0002"},
		{"negative below half", -7, 4, -2, "-0.0002"},
		{"odd half positive", 7, 2, 4, "0.0004"},
		{"odd half negative", -7, 2, -4, "-0.0004"},
		{"divide by one is identity", 12345, 1, 12345, "1.2345"},
		{"zero numerator", 0, 250, 0, "0.0000"},
		{"large window divisor", 5700000, 250, 22800, "2.2800"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FromUnits(tt.units).DivInt(tt.n)
			if got.Units() != tt.want {
				t.Errorf("units = %d, want %d", got.Units(), tt.want)
			}
			if got.String() != tt.wantStr {
				t.Errorf("String = %q, want %q", got.String(), tt.wantStr)
			}
		})
	}
}

// TestCharacterizationDecimalPercentOf locks that PercentOf truncates toward zero (integer
// division) and returns zero for a zero base instead of panicking. RSI must not silently inherit
// different rounding from a changed helper.
func TestCharacterizationDecimalPercentOf(t *testing.T) {
	tests := []struct {
		name       string
		units      int64
		baseUnits  int64
		wantString string
	}{
		{"five percent", 50000, 1000000, "5.0000"},
		{"zero base returns zero", 50000, 0, "0.0000"},
		{"truncates toward zero", 1, 3, "33.3333"},
		{"negative numerator", -50000, 1000000, "-5.0000"},
		{"hundred percent", 1000000, 1000000, "100.0000"},
		{"zero numerator", 0, 1000000, "0.0000"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FromUnits(tt.units).PercentOf(FromUnits(tt.baseUnits)).String(); got != tt.wantString {
				t.Errorf("PercentOf = %q, want %q", got, tt.wantString)
			}
		})
	}
}

func TestCharacterizationDecimalString(t *testing.T) {
	tests := []struct {
		units int64
		want  string
	}{
		{0, "0.0000"},
		{1, "0.0001"},
		{7, "0.0007"},
		{-1, "-0.0001"},
		{-50, "-0.0050"},
		{9999, "0.9999"},
		{10000, "1.0000"},
		{1234500, "123.4500"},
		{-1234500, "-123.4500"},
		{5700000, "570.0000"},
		{100000000, "10000.0000"},
	}
	for _, tt := range tests {
		if got := FromUnits(tt.units).String(); got != tt.want {
			t.Errorf("FromUnits(%d).String() = %q, want %q", tt.units, got, tt.want)
		}
		if got := FromUnits(tt.units).Units(); got != tt.units {
			t.Errorf("FromUnits(%d).Units() = %d, want round trip", tt.units, got)
		}
	}
}

func TestCharacterizationDecimalParse(t *testing.T) {
	tests := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"388.2", "388.2000", false},
		{"388.200", "388.2000", false},
		{"0", "0.0000", false},
		{"12", "12.0000", false},
		{"-0.35", "-0.3500", false},
		{"0.0001", "0.0001", false},
		{" 42.5 ", "42.5000", false},
		{"1.23455", "1.2346", false},
		{"1.23454", "1.2345", false},
		{"-1.23455", "-1.2346", false},
		{"1.23456789", "1.2346", false},
		{"", "", true},
		{"abc", "", true},
		{"1.2.3", "", true},
		{"--1", "", true},
		{"+1", "", true},
		{"1e5", "", true},
		{".5", "", true},
	}
	for _, tt := range tests {
		got, err := Parse(tt.in)
		if (err != nil) != tt.wantErr {
			t.Errorf("Parse(%q) err = %v, wantErr %v", tt.in, err, tt.wantErr)
			continue
		}
		if !tt.wantErr && got.String() != tt.want {
			t.Errorf("Parse(%q) = %s, want %s", tt.in, got, tt.want)
		}
	}
}

// TestCharacterizationScaleIsFour locks money.Scale, which the HTTP layer's decimal string format
// and the OpenAPI Decimal pattern both depend on.
func TestCharacterizationScaleIsFour(t *testing.T) {
	if Scale != 4 {
		t.Errorf("Scale = %d, want 4", Scale)
	}
}
