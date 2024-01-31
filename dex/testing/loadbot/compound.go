// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package main

import (
	"sync"
)

// runCompound runs the 'compound' program, consisting of 2 (5/3) unmetered
// sideStackers, a 1-order sniper, and a pingPonger.
func runCompound(numStanding, ordsPerEpoch int) {
	var oscillator uint64
	var wg sync.WaitGroup
	wg.Add(4)
	if numStanding == 0 {
		numStanding = 20
	}
	if ordsPerEpoch == 0 {
		ordsPerEpoch = 3
	}
	go func() {
		defer wg.Done()
		seller, metered, oscillatorWrite := true, false, true
		runTrader(newSideStacker(20, 3, seller, metered, oscillatorWrite,
			&oscillator, log.SubLogger("STACKER:0")), "CMPD:STACKER:0")
	}()
	go func() {
		defer wg.Done()
		seller, metered, oscillatorWrite := false, false, false
		runTrader(newSideStacker(20, 3, seller, metered, oscillatorWrite,
			&oscillator, log.SubLogger("STACKER:1")), "CMPD:STACKER:1")
	}()
	go func() {
		defer wg.Done()
		runTrader(newSniper(1), "CMPD:SNIPER:0")
	}()
	go func() {
		defer wg.Done()
		runTrader(&pingPonger{}, "CMPD:PINGPONG:0")
	}()
	wg.Wait()
}
