package indicators

import (
	"testing"

	"github.com/qoder-pdsa/qoder-terminal-data/internal/money"
)

func TestEMA(t *testing.T) {
	tests := []struct {
		name   string
		closes []string
		window int
		want   []string
	}{
		// Reference values below are hand-computed from the integer form of the recursion,
		// ema = (2*close + (window-1)*ema_prev) / (window+1), which is k = 2/(window+1).
		{"window 3 gives k=1/2", []string{"1", "2", "3", "4", "5"}, 3, []string{"nil", "nil", "2.0000", "3.0000", "4.0000"}},
		{"window 2 gives k=2/3", []string{"1", "2", "3", "4"}, 2, []string{"nil", "1.5000", "2.5000", "3.5000"}},
		{"falling series", []string{"10", "11", "12", "9", "8"}, 3, []string{"nil", "nil", "11.0000", "10.0000", "9.0000"}},
		{"flat series", []string{"5", "5", "5", "5"}, 3, []string{"nil", "nil", "5.0000", "5.0000"}},
		{"window one tracks the close", []string{"1.5", "2.5"}, 1, []string{"1.5000", "2.5000"}},
		{"window equals input", []string{"1", "2", "3"}, 3, []string{"nil", "nil", "2.0000"}},
		{"no float error", []string{"0.1", "0.2"}, 2, []string{"nil", "0.1500"}},
		{"rounds half away from zero", []string{"0.0001", "0.0002", "0.0003"}, 2, []string{"nil", "0.0002", "0.0003"}},
		{"window larger than input", []string{"1", "2"}, 5, []string{"nil", "nil"}},
		{"zero window", []string{"1"}, 0, []string{"nil"}},
		{"negative window", []string{"1", "2"}, -3, []string{"nil", "nil"}},
		{"empty", nil, 3, []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := render(EMA(decimals(t, tt.closes...), tt.window))
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

// TestEMASeedIsSMA checks the acceptance requirement that the first valid EMA value equals the SMA of
// the first window closes, so the two indicators agree at the warm-up boundary.
func TestEMASeedIsSMA(t *testing.T) {
	closes := decimals(t, "388.2", "380", "391.75", "377.5", "385.25", "390", "372.8", "381.6")
	for _, window := range []int{1, 2, 3, 5, 8} {
		ema := EMA(closes, window)
		sma := SMA(closes, window)
		if ema[window-1] == nil || sma[window-1] == nil {
			t.Fatalf("window %d: missing seed value", window)
		}
		if ema[window-1].String() != sma[window-1].String() {
			t.Errorf("window %d: first EMA = %s, want the SMA seed %s", window, ema[window-1], sma[window-1])
		}
	}
}

// TestEMAShapeInvariants checks the length and warm-up contract for every window, including the
// maximum window the API allows.
func TestEMAShapeInvariants(t *testing.T) {
	closes := make([]money.Decimal, 40)
	for i := range closes {
		closes[i] = money.FromInt(int64(100 + i))
	}
	for _, window := range []int{1, 2, 5, 20, 39, 40, 41, 250} {
		got := EMA(closes, window)
		if len(got) != len(closes) {
			t.Fatalf("window %d: len = %d, want %d", window, len(got), len(closes))
		}
		for i, v := range got {
			if i < window-1 {
				if v != nil {
					t.Errorf("window %d: [%d] = %s, want nil during warm-up", window, i, v)
				}
				continue
			}
			if window <= len(closes) && v == nil {
				t.Errorf("window %d: [%d] = nil, want a value", window, i)
			}
		}
	}
}

// TestEMAMaxWindowDoesNotOverflow exercises the largest window the API accepts over a series of
// high prices, where the (window-1)*ema term is at its biggest.
func TestEMAMaxWindowDoesNotOverflow(t *testing.T) {
	closes := make([]money.Decimal, 250)
	for i := range closes {
		closes[i] = money.FromInt(570)
	}
	got := EMA(closes, 250)
	if got[248] != nil {
		t.Errorf("[248] = %s, want nil", got[248])
	}
	if got[249] == nil || got[249].String() != "570.0000" {
		t.Errorf("[249] = %v, want 570.0000", got[249])
	}
}
