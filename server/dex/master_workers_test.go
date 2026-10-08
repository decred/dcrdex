// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dex

import (
	"context"
	"slices"
	"testing"
	"time"

	"decred.org/dcrdex/server/market"
)

func TestNewMasterWorkers(t *testing.T) {
	var readied bool
	swapperRun := func(context.Context, func(error)) {}
	workers := newMasterWorkers(swapperRun, func() { readied = true },
		map[string]*market.Market{"abc_xyz": nil, "def_xyz": nil})

	wantNames := []string{"Swapper", "Market[abc_xyz]", "Market[def_xyz]", "Swap inactivity checks"}
	var names []string
	for _, worker := range workers {
		names = append(names, worker.Name)
	}
	if !slices.Equal(names, wantNames) {
		t.Fatalf("worker order = %v, want %v", names, wantNames)
	}
	last := workers[len(workers)-1]

	ready := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		last.Run(ctx, func(err error) { ready <- err })
	}()
	select {
	case err := <-ready:
		if err != nil {
			t.Fatalf("sentinel readiness error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("sentinel never reported ready")
	}
	if !readied {
		t.Fatal("sentinel reported ready without calling enableInactionChecks")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("sentinel did not stop on context cancel")
	}
}
