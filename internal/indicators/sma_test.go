package indicators

import (
	"testing"

	"github.com/qoder-pdsa/qoder-terminal-data/internal/money"
)

func decimals(t *testing.T, values ...string) []money.Decimal {
	t.Helper()
	out := make([]money.Decimal, len(values))
	for i, v := range values {
		d, err := money.Parse(v)
		if err != nil {
			t.Fatal(err)
		}
		out[i] = d
	}
	return out
}

func render(values []*money.Decimal) []string {
	out := make([]string, len(values))
	for i, v := range values {
		if v == nil {
			out[i] = "nil"
			continue
		}
		out[i] = v.String()
	}
	return out
}

func TestSMA(t *testing.T) {
	tests := []struct {
		name   string
		closes []string
		window int
		want   []string
	}{
		{"basic", []string{"1", "2", "3", "4"}, 3, []string{"nil", "nil", "2.0000", "3.0000"}},
		{"no float error", []string{"0.1", "0.2"}, 2, []string{"nil", "0.1500"}},
		{"window larger than input", []string{"1", "2"}, 5, []string{"nil", "nil"}},
		{"window one", []string{"1.5", "2.5"}, 1, []string{"1.5000", "2.5000"}},
		{"zero window", []string{"1"}, 0, []string{"nil"}},
		{"empty", nil, 3, []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := render(SMA(decimals(t, tt.closes...), tt.window))
			if len(got) != len(tt.want) {
				t.Fatalf("len = %d, want %d", len(got), len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("[%d] = %s, want %s", i, got[i], tt.want[i])
				}
			}
		})
	}
}
