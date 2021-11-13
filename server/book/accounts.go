// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package book

import (
	"decred.org/dcrdex/dex/order"
)

// AccountTracking is a bitfield representing the assets which need account
// tracking.
type AccountTracking uint8

const (
	// AccountTrackingBase should be included if the base asset is
	// account-based.
	AccountTrackingBase AccountTracking = 1 << iota
	// AccountTrackingQuote should be included if the quote asset is
	// account-based.
	AccountTrackingQuote
)

func (a AccountTracking) base() bool {
	return (a & AccountTrackingBase) > 0
}

func (a AccountTracking) quote() bool {
	return (a & AccountTrackingQuote) > 0
}

// accountTracker tracks orders for account-based assets. Account tracker only
// tracks assets that are account-based, as specified in the constructor.
// If neither base or quote is account-based, then all of accountTracker's
// methods do nothing, so there's no harm in using a newAccountTracker(0) rather
// than checking whether assets are actually account-based everywhere.
// The accountTracker is not thread-safe. In use, synchronization is provided by
// the *OrderPQ's mutex.
type accountTracker struct {
	tracking    AccountTracking
	base, quote map[string]map[order.OrderID]*order.LimitOrder
}

func newAccountTracker(tracking AccountTracking) *accountTracker {
	// nilness is used to signal that an asset is not account-based and does
	// not need tracking.
	var base, quote map[string]map[order.OrderID]*order.LimitOrder
	if tracking.base() {
		base = make(map[string]map[order.OrderID]*order.LimitOrder)
	}
	if tracking.quote() {
		quote = make(map[string]map[order.OrderID]*order.LimitOrder)
	}
	return &accountTracker{
		tracking: tracking,
		base:     base,
		quote:    quote,
	}
}

// add an order to tracking.
func (a *accountTracker) add(lo *order.LimitOrder) {
	if a.base != nil {
		addAccountOrder(lo.BaseAccount(), a.base, lo)
	}
	if a.quote != nil {
		addAccountOrder(lo.QuoteAccount(), a.quote, lo)
	}
}

// remove an order from tracking.
func (a *accountTracker) remove(lo *order.LimitOrder) {
	if a.base != nil {
		removeAccountOrder(lo.BaseAccount(), a.base, lo.ID())
	}
	if a.quote != nil {
		removeAccountOrder(lo.QuoteAccount(), a.quote, lo.ID())
	}
}

// addAccountOrder adds the order to the account address -> orders map, creating
// an entry if necessary.
func addAccountOrder(addr string, acctOrds map[string]map[order.OrderID]*order.LimitOrder, lo *order.LimitOrder) {
	ords, found := acctOrds[addr]
	if !found {
		ords = make(map[order.OrderID]*order.LimitOrder)
		acctOrds[addr] = ords
	}
	ords[lo.ID()] = lo
}

// removeAccountOrder removes the order from the account address -> orders map,
// deleting the map if empty.
func removeAccountOrder(addr string, acctOrds map[string]map[order.OrderID]*order.LimitOrder, oid order.OrderID) {
	ords, found := acctOrds[addr]
	if !found {
		return
	}
	delete(ords, oid)
	if len(ords) == 0 {
		delete(acctOrds, addr)
	}
}

// iterateBaseAccount calls the provided function for every tracked order with a
// base asset corresponding to the specified account address.
func (a *accountTracker) iterateBaseAccount(acctAddr string, f func(*order.LimitOrder)) {
	if a.base == nil {
		return
	}
	for _, lo := range a.base[acctAddr] {
		f(lo)
	}
}

// iterateQuoteAccount calls the provided function for every tracked order with
// a quote asset corresponding to the specified account address.
func (a *accountTracker) iterateQuoteAccount(acctAddr string, f func(*order.LimitOrder)) {
	if a.quote == nil {
		return
	}
	for _, lo := range a.quote[acctAddr] {
		f(lo)
	}
}
