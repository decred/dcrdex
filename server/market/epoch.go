// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package market

import (
	"time"

	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/server/account"
)

// EpochQueue represents an epoch order queue. The methods are NOT thread safe
// by design.
type EpochQueue struct {
	// Epoch is the epoch index.
	Epoch    int64
	Duration int64
	// Start and End define the time range of the epoch as [Start,End).
	Start, End time.Time
	// Orders holds the epoch queue orders in a map for quick lookups.
	Orders map[order.OrderID]order.Order
	// UserCancels counts the number of cancel orders per user.
	UserCancels map[account.AccountID]uint32
	// CancelTargets maps known targeted order IDs with the CancelOrder
	CancelTargets map[order.OrderID]*order.CancelOrder
}

// NewEpoch creates an epoch with the given index and duration in milliseconds.
func NewEpoch(idx int64, duration int64) *EpochQueue {
	startTime := time.UnixMilli(idx * duration)
	return &EpochQueue{
		Epoch:         idx,
		Duration:      duration,
		Start:         startTime,
		End:           startTime.Add(time.Duration(duration) * time.Millisecond),
		Orders:        make(map[order.OrderID]order.Order),
		UserCancels:   make(map[account.AccountID]uint32),
		CancelTargets: make(map[order.OrderID]*order.CancelOrder),
	}
}

// OrderSlice extracts the orders in a slice. The slice ordering is random.
func (eq *EpochQueue) OrderSlice() []order.Order {
	orders := make([]order.Order, 0, len(eq.Orders))
	for _, ord := range eq.Orders {
		orders = append(orders, ord)
	}
	return orders
}

// Stores an order in the Order slice, overwriting and pre-existing order.
func (eq *EpochQueue) Insert(ord order.Order) {
	eq.Orders[ord.ID()] = ord
	if co, ok := ord.(*order.CancelOrder); ok {
		eq.CancelTargets[co.TargetOrderID] = co
		eq.UserCancels[co.AccountID]++
	}
}

// IncludesTime checks if the given time falls in the epoch.
func (eq *EpochQueue) IncludesTime(t time.Time) bool {
	// [Start,End): Check the inclusive lower bound.
	if t.Equal(eq.Start) {
		return true
	}
	// Check (Start,End).
	return t.After(eq.Start) && t.Before(eq.End)
}
