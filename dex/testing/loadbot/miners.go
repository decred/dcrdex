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
	i := 0
	for {
		symbol := "btc"
		if i%2 == 0 {
			symbol = "dcr"
		}
		log.Debugf("mining %s", symbol)
		mineAlpha(symbol)

		getDelay := func() time.Duration {
			return time.Duration(rand.Float64()*float64(epochDuration))*time.Millisecond + 2*time.Second
		}

		select {
		case <-time.After(getDelay()):
		case <-ctx.Done():
			return
		}
		i++
	}
}

// moreThanOneBlockPer mines a block for every asset within each epoch.
func moreThanOneBlockPer() {
	i := 0
	for {
		symbol := "btc"
		if i%2 == 0 {
			symbol = "dcr"
		}
		log.Debugf("mining %s", symbol)
		mineAlpha(symbol)
		select {
		case <-time.After(time.Millisecond * time.Duration(epochDuration) * 4 / 9):
		case <-ctx.Done():
			return
		}
		i++
	}
}
