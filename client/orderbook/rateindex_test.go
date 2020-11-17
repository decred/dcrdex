package orderbook

import (
	"testing"
)

// makeRateIndex creates a new rate index.
func makeRateIndex(index []uint64) *rateIndex {
	if index != nil {
		return &rateIndex{
			Rates: index,
		}
	}

	return newRateIndex()
}

func TestRateIndexAdd(t *testing.T) {
	tests := []struct {
		index    *rateIndex
		entry    uint64
		expected *rateIndex
	}{
		{
			index:    makeRateIndex([]uint64{0, 1, 3, 5}),
			entry:    4,
			expected: makeRateIndex([]uint64{0, 1, 3, 4, 5}),
		},
		{
			index:    makeRateIndex([]uint64{0, 1, 3, 4}),
			entry:    0,
			expected: makeRateIndex([]uint64{0, 1, 3, 4}),
		},
		{
			index:    makeRateIndex([]uint64{0}),
			entry:    1,
			expected: makeRateIndex([]uint64{0, 1}),
		},
		{
			index:    makeRateIndex(nil),
			entry:    0,
			expected: makeRateIndex([]uint64{0}),
		},
	}

	for idx, tc := range tests {
		tc.index.Add(tc.entry)

		if len(tc.index.Rates) != len(tc.expected.Rates) {
			t.Fatalf("[RateIndex.Add] #%d: expected index size of %d, got %d",
				idx+1, len(tc.expected.Rates), len(tc.index.Rates))
		}

		for i := 0; i < len(tc.index.Rates); i++ {
			if tc.index.Rates[i] != tc.expected.Rates[i] {
				t.Fatalf("[RateIndex.Add] #%d: expected %d at index "+
					"%d, got %v", idx+1, tc.expected.Rates[i], i,
					tc.index.Rates[i])
			}
		}
	}
}

func TestRateIndexRemove(t *testing.T) {
	tests := []struct {
		index    *rateIndex
		entry    uint64
		expected *rateIndex
		wantErr  bool
	}{
		{
			index:    makeRateIndex([]uint64{0, 1, 3, 4, 5}),
			entry:    4,
			expected: makeRateIndex([]uint64{0, 1, 3, 5}),
			wantErr:  false,
		},
		{
			index:    makeRateIndex([]uint64{0, 1, 3, 4}),
			entry:    5,
			expected: makeRateIndex([]uint64{0, 1, 3, 4}),
			wantErr:  true,
		},
		{
			index:    makeRateIndex([]uint64{0, 1}),
			entry:    1,
			expected: makeRateIndex([]uint64{0}),
			wantErr:  false,
		},
		{
			index:    makeRateIndex([]uint64{0}),
			entry:    0,
			expected: makeRateIndex(nil),
			wantErr:  false,
		},
	}

	for idx, tc := range tests {
		err := tc.index.Remove(tc.entry)

		if (err != nil) != tc.wantErr {
			t.Fatalf("[RateIndex.Remove] #%d: error: %v, wantErr: %v",
				idx+1, err, tc.wantErr)
		}

		if len(tc.index.Rates) != len(tc.expected.Rates) {
			t.Fatalf("[RateIndex.Remove] #%d: expected index size "+
				"of %d, got %d", idx+1, len(tc.expected.Rates),
				len(tc.index.Rates))
		}

		for i := 0; i < len(tc.index.Rates); i++ {
			if tc.index.Rates[i] != tc.expected.Rates[i] {
				t.Fatalf("[RateIndex.Remove] #%d: expected %d at index "+
					"%d, got %v", idx+1, tc.expected.Rates[i], i,
					tc.index.Rates[i])
			}
		}
	}
}
