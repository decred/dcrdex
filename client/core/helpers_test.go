//go:build !harness
// +build !harness

package core

import (
	"testing"
	"time"
)

func Test_matchReader_TimeString(t *testing.T) {
	stamp := uint64(1607189329)
	mr := &matchReader{
		Match: &Match{
			Stamp: stamp * 1000,
		},
	}

	gotTimeStr := mr.TimeString()

	// Verify the time string can be parsed and matches the expected Time.
	layout := "Jan 2 2006, 15:04:05 MST"
	gotTime, err := time.Parse(layout, gotTimeStr)
	if err != nil {
		t.Fatalf("got bad time string: %v", err)
	}

	wantTime := time.Unix(int64(stamp), 0)
	if !gotTime.Equal(wantTime) {
		t.Errorf("wanted time %q, got %q", wantTime, gotTime)
	}
}
