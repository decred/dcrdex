// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package main

import (
	"fmt"
	"sync"

	"decred.org/dcrdex/dex/calc"
)

// runCompound runs the 'compound' program, consisting of 2 (5/3) unmetered
// sideStackers, a 1-order sniper, and a pingPonger.
func runCompound() {
	var oscillator uint64
	var wg sync.WaitGroup
	wg.Add(4)
	go func() {
		defer wg.Done()
		seller, metered, oscillatorWrite := true, false, true
		runTrader(newSideStacker(20, 3, alpha, seller, metered, oscillatorWrite,
			&oscillator, log.SubLogger("STACKER:0")), "CMPD:STACKER:0")
	}()
	go func() {
		defer wg.Done()
		seller, metered, oscillatorWrite := false, false, false
		runTrader(newSideStacker(20, 3, alpha, seller, metered, oscillatorWrite,
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

// runHeavy runs the 'heavy' program, consisting of 2 (12/6) metered
// sideStackers, 2 (8/4) unmetered sideStackers, and a 5-order sniper.
func runHeavy() {
	// Gotta get some major funding to the beta wallets.
	var base uint64 = 100 * lotSize
	var quote uint64 = calc.BaseToQuote(uint64(defaultMidGap*rateEncFactor), base)
	log.Infof("loading the beta node wallets")
	for i := 0; i < 10; i++ {
		for j := 0; j < 5; j++ {
			if err := send(baseSymbol, alpha, betaAddrBase, quote); err != nil {
				panic(fmt.Errorf("unable to send funds to quote asset: %v", err))
			}
			if err := send(quoteSymbol, alpha, betaAddrQuote, base); err != nil {
				panic(fmt.Errorf("unable to send funds to base asset: %v", err))
			}
		}
		<-mine(quoteSymbol, alpha)
		<-mine(baseSymbol, alpha)
		if ctx.Err() != nil {
			return
		}
	}

	var oscillator uint64
	var wg sync.WaitGroup
	wg.Add(5)
	go func() {
		defer wg.Done()
		seller, metered, oscillatorWrite := true, false, true
		runTrader(newSideStacker(24, 6, alpha, seller, metered, oscillatorWrite,
			&oscillator, log.SubLogger("STACKER:0")), "HEAVY:STACKER:0")
	}()
	go func() {
		defer wg.Done()
		seller, metered, oscillatorWrite := false, false, false
		runTrader(newSideStacker(24, 6, alpha, seller, metered, oscillatorWrite,
			&oscillator, log.SubLogger("STACKER:1")), "HEAVY:STACKER:1")
	}()
	go func() {
		defer wg.Done()
		seller, metered, oscillatorWrite := true, false, false
		runTrader(newSideStacker(16, 4, beta, seller, metered, oscillatorWrite,
			&oscillator, log.SubLogger("STACKER:2")), "HEAVY:STACKER:2")
	}()
	go func() {
		defer wg.Done()
		seller, metered, oscillatorWrite := false, false, false
		runTrader(newSideStacker(16, 4, beta, seller, metered, oscillatorWrite,
			&oscillator, log.SubLogger("STACKER:3")), "HEAVY:STACKER:3")
	}()
	go func() {
		defer wg.Done()
		runTrader(newSniper(5), "HEAVY:SNIPER:0")
	}()
	wg.Wait()
}
