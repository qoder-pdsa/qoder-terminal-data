package indicators

// Characterization tests locking the PRE-CHANGE behaviour of SMA, the existing indicator that
// BL-01 places EMA and RSI beside. SMA itself is not modified by BL-01; these tests exist so that
// any accidental change to it (or to the shared nil/warm-up shape that EMA and RSI must match)
// is caught by the characterization regression in dev_characterization.
//
// Group selection: go test ./... -run TestCharacterization
// Baseline: actually run on the unchanged tree at commit 35f33b3 (evidence/baseline-characterization.log).
//
// Reuses the existing table-driven helpers decimals() and render() from sma_test.go rather than
// introducing a second test style.

import "testing"

func TestCharacterizationSMA(t *testing.T) {
	tests := []struct {
		name   string
		closes []string
		window int
		want   []string
	}{
		{"basic window 3", []string{"1", "2", "3", "4"}, 3, []string{"nil", "nil", "2.0000", "3.0000"}},
		{"no float error", []string{"0.1", "0.2"}, 2, []string{"nil", "0.1500"}},
		{"rounds half away from zero", []string{"0.0001", "0.0002"}, 2, []string{"nil", "0.0002"}},
		{"exact division keeps scale", []string{"10.0001", "10.0002", "10.0003"}, 3, []string{"nil", "nil", "10.0002"}},
		{"window larger than input", []string{"1", "2"}, 5, []string{"nil", "nil"}},
		{"window equal to input", []string{"1", "2", "3"}, 3, []string{"nil", "nil", "2.0000"}},
		{"window one is identity", []string{"1.5", "2.5"}, 1, []string{"1.5000", "2.5000"}},
		{"zero window all nil", []string{"1"}, 0, []string{"nil"}},
		{"negative window all nil", []string{"1", "2"}, -3, []string{"nil", "nil"}},
		{"flat series", []string{"7", "7", "7"}, 2, []string{"nil", "7.0000", "7.0000"}},
		{"empty input", nil, 3, []string{}},
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

// TestCharacterizationSMAShapeInvariants locks the two shape rules the HTTP layer relies on and
// that EMA and RSI must honour identically: the result is always the same length as the input,
// and every position before the warm-up is nil.
func TestCharacterizationSMAShapeInvariants(t *testing.T) {
	closes := make([]string, 0, 30)
	for range 30 {
		closes = append(closes, "100.0000")
	}
	for _, window := range []int{1, 5, 20, 30, 31, 250} {
		values := SMA(decimals(t, closes...), window)
		if len(values) != len(closes) {
			t.Fatalf("window %d: len = %d, want %d", window, len(values), len(closes))
		}
		firstValid := window - 1
		if firstValid < 0 {
			firstValid = 0
		}
		for i := range values {
			switch {
			case window > len(closes):
				if values[i] != nil {
					t.Errorf("window %d: [%d] = %s, want nil (window never fills)", window, i, values[i])
				}
			case i < firstValid:
				if values[i] != nil {
					t.Errorf("window %d: [%d] = %s, want nil (warm-up)", window, i, values[i])
				}
			default:
				if values[i] == nil {
					t.Errorf("window %d: [%d] = nil, want a value", window, i)
				}
			}
		}
	}
}

// TestCharacterizationSMAMaxWindowNoOverflow locks that the largest window the HTTP layer accepts
// (maxIndicatorWindow = 250) over realistic HK price magnitudes neither overflows nor loses scale.
func TestCharacterizationSMAMaxWindowNoOverflow(t *testing.T) {
	closes := make([]string, 250)
	for i := range closes {
		closes[i] = "570.0000"
	}
	values := SMA(decimals(t, closes...), 250)
	if len(values) != 250 {
		t.Fatalf("len = %d, want 250", len(values))
	}
	if values[248] != nil {
		t.Errorf("[248] = %s, want nil", values[248])
	}
	if values[249] == nil || values[249].String() != "570.0000" {
		t.Errorf("[249] = %v, want 570.0000", values[249])
	}
}
