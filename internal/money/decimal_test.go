package money

import "testing"

func TestDecimalString(t *testing.T) {
	tests := []struct {
		units int64
		want  string
	}{
		{0, "0.0000"},
		{1234500, "123.4500"},
		{-50, "-0.0050"},
		{7, "0.0007"},
	}
	for _, tt := range tests {
		if got := FromUnits(tt.units).String(); got != tt.want {
			t.Errorf("FromUnits(%d).String() = %q, want %q", tt.units, got, tt.want)
		}
	}
}

func TestParse(t *testing.T) {
	tests := []struct {
		in, want string
		wantErr  bool
	}{
		{"388.2", "388.2000", false},
		{"388.200", "388.2000", false},
		{"-0.35", "-0.3500", false},
		{"12", "12.0000", false},
		{"1.23455", "1.2346", false},
		{"1.23454", "1.2345", false},
		{"", "", true},
		{"abc", "", true},
		{"1.2.3", "", true},
		{"--1", "", true},
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

func TestDivIntRounds(t *testing.T) {
	if got := FromUnits(10).DivInt(4).Units(); got != 3 { // 2.5 → 3
		t.Errorf("DivInt = %d, want 3", got)
	}
	if got := FromUnits(-10).DivInt(4).Units(); got != -3 {
		t.Errorf("DivInt negative = %d, want -3", got)
	}
}

func TestPercentOf(t *testing.T) {
	change := FromUnits(50000) // 5.0000
	base := FromUnits(1000000) // 100.0000
	if got := change.PercentOf(base).String(); got != "5.0000" {
		t.Errorf("PercentOf = %q, want 5.0000", got)
	}
	if got := change.PercentOf(FromUnits(0)).String(); got != "0.0000" {
		t.Errorf("PercentOf zero base = %q, want 0.0000", got)
	}
}

func TestFromInt(t *testing.T) {
	tests := []struct {
		n    int64
		want string
	}{
		{0, "0.0000"},
		{1, "1.0000"},
		{50, "50.0000"},
		{-3, "-3.0000"},
	}
	for _, tt := range tests {
		if got := FromInt(tt.n).String(); got != tt.want {
			t.Errorf("FromInt(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

func TestMulInt(t *testing.T) {
	tests := []struct {
		units int64
		n     int64
		want  int64
	}{
		{10000, 2, 20000},
		{10000, 0, 0},
		{10000, 1, 10000},
		{-2500, 3, -7500},
		{2500, -1, -2500},
		{1, 249, 249},
	}
	for _, tt := range tests {
		if got := FromUnits(tt.units).MulInt(tt.n).Units(); got != tt.want {
			t.Errorf("FromUnits(%d).MulInt(%d) = %d, want %d", tt.units, tt.n, got, tt.want)
		}
	}
}
