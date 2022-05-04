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

func Test_IntDivUp(t *testing.T) {
	tests := []struct {
		val, div int64
		want     int64
	}{
		{-1, 2, 0},
		{-1, 1, -1},
		{0, 1, 0},
		{1, 1, 1},
		{1, 2, 1},
		{3, 2, 2},
		{3, 500, 1},
		{1500, 500, 3},
		{1501, 500, 4},
		{23000, 1000, 23},
		{23105, 1000, 24},
		{24000, 1000, 24},
	}
	for _, tt := range tests {
		if got := IntDivUp(tt.val, tt.div); got != tt.want {
			t.Errorf("intDivUp() = %v, want %v", got, tt.want)
		}
	}
}
