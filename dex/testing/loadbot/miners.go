// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package main

import (
	"math/rand"
	"time"
)

// blockEvery2 will randomize the delay, but aim for 1 block of each asset
// within a 2 epoch window.
func blockEvery2() {
	i := byte(0)
	for {
		symbol := baseSymbol
		if i == 1 {
			symbol = quoteSymbol
		}
		log.Debugf("mining %s", symbol)
		mine(symbol, alpha)

		getDelay := func() time.Duration {
			return time.Duration(rand.Float64()*float64(epochDuration))*time.Millisecond + 2*time.Second
		}

		select {
		case <-time.After(getDelay()):
		case <-ctx.Done():
			return
		}
		i ^= 1
	}
}

// moreThanOneBlockPer mines a block for every asset within each epoch.
func moreThanOneBlockPer() {
	i := byte(0)
	for {
		symbol := baseSymbol
		if i == 1 {
			symbol = quoteSymbol
		}
		log.Debugf("mining %s", symbol)
		mine(symbol, alpha)
		select {
		case <-time.After(time.Millisecond * time.Duration(epochDuration) * 4 / 9):
		case <-ctx.Done():
			return
		}
		i ^= 1
	}
}
