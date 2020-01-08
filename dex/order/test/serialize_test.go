// Copyright (c) 2019, The Decred developers
// See LICENSE for details.

// Package order defines the Order and Match types used throughout the DEX.
package test

import (
	"testing"
	"time"

	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/order"
)

var randB = encode.RandomBytes

func TestOrders(t *testing.T) {
	spins := 10000
	tStart := time.Now()
	for i := 0; i < spins; i++ {
		testOrders(t)
	}
	t.Logf("created, encoded, and decoded %d orders (%d each of 3 types) in %d ms", 3*spins, spins, time.Since(tStart)/time.Millisecond)
}

func testOrders(t *testing.T) {
	lo := RandomLimitOrder()
	lo.Coins = []order.CoinID{randB(36), randB(36)}
	// Truncate time to pass time.Equal testing after decoding.
	lo.SetTime(time.Now().Truncate(time.Millisecond))

	loB := order.EncodeOrder(lo)
	reOrder, err := order.DecodeOrder(loB)
	if err != nil {
		t.Fatalf("error decoding limit order: %v", err)
	}
	reLO, ok := reOrder.(*order.LimitOrder)
	if !ok {
		t.Fatalf("wrong order type returned for limit order")
	}

	MustCompareLimitOrders(t, lo, reLO)

	mo := RandomMarketOrder()
	mo.Coins = []order.CoinID{randB(36), randB(36), randB(38)}
	// Not setting the server time on this one.

	moB := order.EncodeOrder(mo)
	reOrder, err = order.DecodeOrder(moB)
	if err != nil {
		t.Fatalf("error decoding market order: %v", err)
	}
	reMO, ok := reOrder.(*order.MarketOrder)
	if !ok {
		t.Fatalf("wrong order type returned for market order")
	}
	MustCompareMarketOrders(t, mo, reMO)

	co := RandomCancelOrder()
	co.SetTime(time.Now().Truncate(time.Millisecond))

	coB := order.EncodeOrder(co)
	reOrder, err = order.DecodeOrder(coB)
	if err != nil {
		t.Fatalf("error decoding cancel order: %v", err)
	}
	reCO, ok := reOrder.(*order.CancelOrder)
	if !ok {
		t.Fatalf("wrong order type returned for cancel order")
	}
	MustCompareCancelOrders(t, co, reCO)
}

func TestUserMatch(t *testing.T) {
	spins := 30000
	tStart := time.Now()
	for i := 0; i < spins; i++ {
		testUserMatch(t)
	}
	t.Logf("created, encoded, and decoded %d matches in %d ms", spins, time.Since(tStart)/time.Millisecond)
}

func testUserMatch(t *testing.T) {
	match := RandomUserMatch()
	matchB := order.EncodeMatch(match)

	reMatch, err := order.DecodeMatch(matchB)
	if err != nil {
		t.Fatalf("errror decoding UserMatch: %v", err)
	}
	MustCompareUserMatch(t, match, reMatch)
}
