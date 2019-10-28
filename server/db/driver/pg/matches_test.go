package pg

import "testing"

func Test_splitEpochID(t *testing.T) {
	tests := []struct {
		name         string
		epochID      string
		wantEpochIdx uint64
		wantEpochDur uint64
		wantErr      bool
	}{
		{
			"ok",
			"12341234:6",
			12341234,
			6,
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotEpochIdx, gotEpochDur, err := splitEpochID(tt.epochID)
			if (err != nil) != tt.wantErr {
				t.Errorf("splitEpochID() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if gotEpochIdx != tt.wantEpochIdx {
				t.Errorf("splitEpochID() gotEpochIdx = %v, want %v", gotEpochIdx, tt.wantEpochIdx)
			}
			if gotEpochDur != tt.wantEpochDur {
				t.Errorf("splitEpochID() gotEpochDur = %v, want %v", gotEpochDur, tt.wantEpochDur)
			}
		})
	}
}
