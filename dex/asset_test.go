package dex

import "testing"

func TestConventionalString(t *testing.T) {
	type test struct {
		v        uint64
		unitInfo UnitInfo
		exp      string
	}

	ui := func(r uint64) UnitInfo {
		return UnitInfo{
			Conventional: Denomination{
				ConversionFactor: r,
			},
		}
	}

	tests := []test{
		{ // integer with no decimal part still displays zeros.
			v:        100000000,
			unitInfo: ui(1e8),
			exp:      "1.00000000",
		},
		{ // trailing zeroes are displayed.
			v:        10,
			unitInfo: ui(1e3),
			exp:      "0.010",
		},
		{ // gwei
			v:        123,
			unitInfo: ui(1e9),
			exp:      "0.000000123",
		},
		{ // no thousands delimiters
			v:        1000000,
			unitInfo: ui(1e3),
			exp:      "1000.000",
		},
	}

	for _, tt := range tests {
		s := tt.unitInfo.ConventionalString(tt.v)
		if s != tt.exp {
			t.Fatalf("unexpected output for value %d, expected %q, got %q", tt.v, tt.exp, s)
		}
	}
}
