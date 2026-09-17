package indicators

import "testing"

func TestRSI(t *testing.T) {
	tests := []struct {
		name   string
		closes []string
		window int
		want   []string
	}{
		// Reference values are hand-computed from Wilder smoothing,
		// avg = (avg*(window-1) + value) / window, reported as 100*avgGain/(avgGain+avgLoss).
		{"window 2 reference", []string{"44", "44.25", "44.5", "43.75", "44.5"}, 2,
			[]string{"nil", "nil", "100.0000", "25.0000", "70.0000"}},
		// Same series with window 3: the smoothed averages are held at money.Scale, so the results
		// differ from an arbitrary-precision reference (40.00 / 68.42) in the last digits.
		{"window 3 reference", []string{"44", "44.25", "44.5", "43.75", "44.5"}, 3,
			[]string{"nil", "nil", "nil", "40.0047", "68.4160"}},
		{"window one all gains", []string{"1", "2"}, 1, []string{"nil", "100.0000"}},
		{"window one all losses", []string{"2", "1"}, 1, []string{"nil", "0.0000"}},
		{"window one follows each close", []string{"1", "2", "1"}, 1, []string{"nil", "100.0000", "0.0000"}},
		{"all gains is 100", []string{"1", "2", "3", "4"}, 2, []string{"nil", "nil", "100.0000", "100.0000"}},
		{"all losses is 0", []string{"4", "3", "2", "1"}, 2, []string{"nil", "nil", "0.0000", "0.0000"}},
		// Documented convention: a flat window has neither gains nor losses, so RSI is 0/0 and the
		// neutral 50 is reported. See the RSI doc comment.
		{"flat window is neutral", []string{"5", "5", "5", "5"}, 2, []string{"nil", "nil", "50.0000", "50.0000"}},
		{"flat window larger than input", []string{"5", "5"}, 3, []string{"nil", "nil"}},
		{"no float error on equal moves", []string{"0.1", "0.2", "0.1"}, 2, []string{"nil", "nil", "50.0000"}},
		{"mixed moves", []string{"0.1", "0.3", "0.2"}, 2, []string{"nil", "nil", "66.6666"}},
		{"window larger than input", []string{"1", "2"}, 5, []string{"nil", "nil"}},
		{"zero window", []string{"1"}, 0, []string{"nil"}},
		{"negative window", []string{"1", "2"}, -3, []string{"nil", "nil"}},
		{"empty", nil, 3, []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := render(RSI(decimals(t, tt.closes...), tt.window))
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

// TestRSIShapeAndBounds checks the two acceptance invariants over every window size: the first window
// positions are nil, and every reported value lies within 0-100.
func TestRSIShapeAndBounds(t *testing.T) {
	closes := decimals(t, "44", "44.25", "43.75", "44.5", "44.25", "43.5", "44.75", "44", "43.25", "44.5")
	for window := 1; window <= len(closes)+1; window++ {
		got := RSI(closes, window)
		if len(got) != len(closes) {
			t.Fatalf("window %d: len = %d, want %d", window, len(got), len(closes))
		}
		for i, v := range got {
			if i < window {
				if v != nil {
					t.Errorf("window %d: [%d] = %s, want nil during warm-up", window, i, v)
				}
				continue
			}
			if v == nil {
				t.Errorf("window %d: [%d] = nil, want a value", window, i)
				continue
			}
			if v.Units() < 0 || v.Units() > 1_000_000 {
				t.Errorf("window %d: [%d] = %s, want a value within 0-100", window, i, v)
			}
		}
	}
}

// TestRSIFlatWindowIsNeutral locks the documented flat-series convention on its own, so a future
// change to 0 or 100 fails loudly instead of silently redefining the contract.
func TestRSIFlatWindowIsNeutral(t *testing.T) {
	for _, window := range []int{1, 2, 5} {
		closes := make([]string, window+3)
		for i := range closes {
			closes[i] = "123.4500"
		}
		got := render(RSI(decimals(t, closes...), window))
		for i := window; i < len(got); i++ {
			if got[i] != "50.0000" {
				t.Errorf("window %d: [%d] = %s, want the neutral 50.0000", window, i, got[i])
			}
		}
	}
}
