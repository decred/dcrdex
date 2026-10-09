// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package db

import (
	"fmt"

	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/order"
)

func int64Bytes(v int64) []byte {
	return encode.Uint64Bytes(uint64(v))
}

func int32Bytes(v int32) []byte {
	return encode.Uint32Bytes(uint32(v))
}

func boolBytes(v bool) []byte {
	if v {
		return encode.ByteTrue
	}
	return encode.ByteFalse
}

func orderTypeBytes(t order.OrderType) []byte {
	return []byte{byte(t)}
}

func orderIDListTxData(ids []order.OrderID) []byte {
	b := encode.BuildyBytes{0}
	for _, id := range ids {
		b = b.AddData(id[:])
	}
	return b
}

// orderIDsTxData encodes the IDs of the supplied orders.
func orderIDsTxData[T interface {
	comparable
	order.Order
}](ords []T) ([]byte, error) {
	var zero T
	b := encode.BuildyBytes{0}
	for i, ord := range ords {
		if ord == zero {
			return nil, fmt.Errorf("nil order at index %d", i)
		}
		oid := ord.ID()
		b = b.AddData(oid[:])
	}
	return b, nil
}

// orderFillsTxData encodes each order's ID and total filled amount.
func orderFillsTxData(ords []*order.LimitOrder) ([]byte, error) {
	b := encode.BuildyBytes{0}
	for _, ord := range ords {
		if ord == nil {
			return nil, fmt.Errorf("nil partial fill order")
		}
		oid := ord.ID()
		b = b.AddData(encode.BuildyBytes{0}.
			AddData(oid[:]).
			AddData(encode.Uint64Bytes(ord.Trade().Filled())))
	}
	return b, nil
}

func startupOrderRevokesTxData(ords []*StartupOrderRevoke) ([]byte, error) {
	b := encode.BuildyBytes{0}
	for _, revoke := range ords {
		if revoke == nil {
			return nil, fmt.Errorf("nil startup order revoke")
		}
		ord := revoke.Order
		if ord == nil {
			return nil, fmt.Errorf("nil startup order revoke value")
		}
		oid := ord.ID()
		user := ord.User()
		b = b.AddData(encode.BuildyBytes{0}.
			AddData(oid[:]).
			AddData(user[:]).
			AddData(encode.Uint32Bytes(ord.Base())).
			AddData(encode.Uint32Bytes(ord.Quote())).
			AddData(orderTypeBytes(ord.Type())).
			AddData(ord.Serialize()).
			AddData([]byte{byte(revoke.Reason)}))
	}
	// TODO: Support revoking more orders.
	if len(b) > encode.MaxDataLen {
		return nil, fmt.Errorf("startup revocation data size %d exceeds maximum %d",
			len(b), encode.MaxDataLen)
	}
	return b, nil
}

// matchIDsTxData encodes the IDs of the supplied matches.
func matchIDsTxData(matches []*order.Match) ([]byte, error) {
	b := encode.BuildyBytes{0}
	for _, match := range matches {
		if match == nil {
			return nil, fmt.Errorf("nil match")
		}
		id := match.ID()
		b = b.AddData(id[:])
	}
	return b, nil
}

// EventTxData returns the versioned transaction data recorded in the event log
// for an order_accepted event.
func (u *OrderAcceptedUpdate) EventTxData() ([]byte, error) {
	if u == nil {
		return nil, fmt.Errorf("nil order accepted update")
	}
	if u.Order == nil {
		return nil, fmt.Errorf("nil accepted order")
	}
	return encode.BuildyBytes{0}.
		AddData(u.Order.Serialize()).
		AddData(int64Bytes(u.EpochIdx)).
		AddData(int64Bytes(u.EpochDur)).
		AddData(int32Bytes(u.EpochGap)), nil
}

// EventTxData returns the versioned transaction data recorded in the event log
// for a market_started event.
func (u *MarketStartedUpdate) EventTxData() ([]byte, error) {
	if u == nil {
		return nil, fmt.Errorf("nil market started update")
	}
	bookedRevokes, err := startupOrderRevokesTxData(u.BookedRevokes)
	if err != nil {
		return nil, err
	}
	epochRevokes, err := startupOrderRevokesTxData(u.EpochRevokes)
	if err != nil {
		return nil, err
	}
	return encode.BuildyBytes{0}.
		AddData([]byte(u.Market)).
		AddData(encode.Uint32Bytes(u.Base)).
		AddData(encode.Uint32Bytes(u.Quote)).
		AddData(int64Bytes(u.CurrentEpochIdx)).
		AddData(int64Bytes(u.EpochDur)).
		AddData(int64Bytes(u.RevocationTime.UnixMilli())).
		AddData(bookedRevokes).
		AddData(epochRevokes), nil
}

// EventTxData returns the versioned transaction data for market_suspend_scheduled.
func (u *MarketSuspendScheduledUpdate) EventTxData() ([]byte, error) {
	if u == nil {
		return nil, fmt.Errorf("nil market_suspend_scheduled update")
	}
	return encodeEventTxData(
		[]byte(u.Market),
		encode.Uint32Bytes(u.Base),
		encode.Uint32Bytes(u.Quote),
		int64Bytes(u.FinalEpochIdx),
		int64Bytes(u.EpochDur),
		boolBytes(u.PersistBook),
	)
}

// EventTxData returns the versioned transaction data for market_suspended.
func (u *MarketSuspendedUpdate) EventTxData() ([]byte, error) {
	if u == nil {
		return nil, fmt.Errorf("nil market_suspended update")
	}
	return encodeEventTxData(
		[]byte(u.Market),
		encode.Uint32Bytes(u.Base),
		encode.Uint32Bytes(u.Quote),
		int64Bytes(u.FinalEpochIdx),
		int64Bytes(u.EpochDur),
		int64Bytes(u.Timestamp.UnixMilli()),
	)
}

// EventTxData returns the versioned transaction data for market_resume_scheduled.
func (u *MarketResumeScheduledUpdate) EventTxData() ([]byte, error) {
	if u == nil {
		return nil, fmt.Errorf("nil market_resume_scheduled update")
	}
	return encodeEventTxData(
		[]byte(u.Market),
		encode.Uint32Bytes(u.Base),
		encode.Uint32Bytes(u.Quote),
		int64Bytes(u.StartEpochIdx),
		int64Bytes(u.EpochDur),
	)
}

// EventTxData returns the versioned transaction data for market_resumed.
func (u *MarketResumedUpdate) EventTxData() ([]byte, error) {
	if u == nil {
		return nil, fmt.Errorf("nil market_resumed update")
	}
	resumeRevokes, err := startupOrderRevokesTxData(u.ResumeRevokes)
	if err != nil {
		return nil, err
	}
	return encodeEventTxData(
		[]byte(u.Market),
		encode.Uint32Bytes(u.Base),
		encode.Uint32Bytes(u.Quote),
		int64Bytes(u.StartEpochIdx),
		int64Bytes(u.EpochDur),
		int64Bytes(u.Timestamp.UnixMilli()),
		resumeRevokes,
	)
}

// encodeEventTxData encodes versioned fields, rejecting oversized fields
// instead of letting BuildyBytes.AddData panic.
func encodeEventTxData(fields ...[]byte) ([]byte, error) {
	b := encode.BuildyBytes{0}
	for i, field := range fields {
		if len(field) > encode.MaxDataLen {
			return nil, fmt.Errorf("transaction data field %d size %d exceeds maximum %d", i, len(field), encode.MaxDataLen)
		}
		b = b.AddData(field)
	}
	return b, nil
}

func preimageMissesTxData(misses []*PreimageMissUpdate) ([]byte, error) {
	b := encode.BuildyBytes{0}
	for _, miss := range misses {
		if miss == nil {
			return nil, fmt.Errorf("nil preimage miss update")
		}
		if miss.Order == nil {
			return nil, fmt.Errorf("nil preimage miss order")
		}
		data, err := encodeEventTxData(
			miss.Order.Serialize(),
			int64Bytes(miss.RevokeTime.UnixMilli()),
		)
		if err != nil {
			return nil, err
		}
		if len(data) > encode.MaxDataLen {
			return nil, fmt.Errorf("preimage miss data size %d exceeds maximum %d", len(data), encode.MaxDataLen)
		}
		b = b.AddData(data)
	}
	return b, nil
}

func preimageRevealsTxData(reveals []*PreimageRevealUpdate) ([]byte, error) {
	b := encode.BuildyBytes{0}
	for _, reveal := range reveals {
		if reveal == nil {
			return nil, fmt.Errorf("nil preimage reveal update")
		}
		if reveal.Order == nil {
			return nil, fmt.Errorf("nil preimage reveal order")
		}
		data, err := encodeEventTxData(
			reveal.Order.Serialize(),
			reveal.Preimage[:],
		)
		if err != nil {
			return nil, err
		}
		if len(data) > encode.MaxDataLen {
			return nil, fmt.Errorf("preimage reveal data size %d exceeds maximum %d", len(data), encode.MaxDataLen)
		}
		b = b.AddData(data)
	}
	return b, nil
}

func epochResultsTxData(epoch *EpochResults) ([]byte, error) {
	return encodeEventTxData(
		encode.Uint32Bytes(epoch.MktBase),
		encode.Uint32Bytes(epoch.MktQuote),
		int64Bytes(epoch.Idx),
		int64Bytes(epoch.Dur),
		int64Bytes(epoch.MatchTime),
		epoch.CSum,
		epoch.Seed,
		orderIDListTxData(epoch.OrdersRevealed),
		orderIDListTxData(epoch.OrdersMissed),
		encode.Uint64Bytes(epoch.MatchVolume),
		encode.Uint64Bytes(epoch.QuoteVolume),
		encode.Uint64Bytes(epoch.BookBuys),
		encode.Uint64Bytes(epoch.BookBuys5),
		encode.Uint64Bytes(epoch.BookBuys25),
		encode.Uint64Bytes(epoch.BookSells),
		encode.Uint64Bytes(epoch.BookSells5),
		encode.Uint64Bytes(epoch.BookSells25),
		encode.Uint64Bytes(epoch.HighRate),
		encode.Uint64Bytes(epoch.LowRate),
		encode.Uint64Bytes(epoch.StartRate),
		encode.Uint64Bytes(epoch.EndRate),
	)
}

// EventTxData returns the versioned transaction data recorded in the event log
// for an epoch_processed event.
func (u *EpochProcessedUpdate) EventTxData() ([]byte, error) {
	if u == nil {
		return nil, fmt.Errorf("nil epoch processed update")
	}
	if u.Epoch == nil {
		return nil, fmt.Errorf("nil epoch results")
	}
	epoch, err := epochResultsTxData(u.Epoch)
	if err != nil {
		return nil, err
	}
	misses, err := preimageMissesTxData(u.Misses)
	if err != nil {
		return nil, err
	}
	reveals, err := preimageRevealsTxData(u.Reveals)
	if err != nil {
		return nil, err
	}
	tradesBooked, err := orderIDsTxData(u.TradesBooked)
	if err != nil {
		return nil, err
	}
	tradesPartial, err := orderFillsTxData(u.TradesPartial)
	if err != nil {
		return nil, err
	}
	tradesCompleted, err := orderIDsTxData(u.TradesCompleted)
	if err != nil {
		return nil, err
	}
	tradesCanceled, err := orderIDsTxData(u.TradesCanceled)
	if err != nil {
		return nil, err
	}
	tradesFailed, err := orderIDsTxData(u.TradesFailed)
	if err != nil {
		return nil, err
	}
	cancelsFailed, err := orderIDsTxData(u.CancelsFailed)
	if err != nil {
		return nil, err
	}
	cancelsExecuted, err := orderIDsTxData(u.CancelsExecuted)
	if err != nil {
		return nil, err
	}
	matches, err := matchIDsTxData(u.Matches)
	if err != nil {
		return nil, err
	}
	return encodeEventTxData(
		epoch,
		misses,
		reveals,
		tradesBooked,
		tradesPartial,
		tradesCompleted,
		tradesCanceled,
		tradesFailed,
		cancelsFailed,
		cancelsExecuted,
		matches,
	)
}

// EventTxData returns the versioned transaction data recorded in the event log
// for a suspended_cancel event.
func (u *SuspendedCancelUpdate) EventTxData() ([]byte, error) {
	if u == nil {
		return nil, fmt.Errorf("nil suspended cancel update")
	}
	if u.Cancel == nil {
		return nil, fmt.Errorf("nil suspended cancel order")
	}
	if u.Match == nil {
		return nil, fmt.Errorf("nil suspended cancel match")
	}
	if u.Match.Maker == nil || u.Match.Taker == nil {
		return nil, fmt.Errorf("nil suspended cancel match order")
	}
	if u.Match.Status != order.MatchComplete {
		return nil, fmt.Errorf("suspended cancel match status %d, want %d", u.Match.Status, order.MatchComplete)
	}
	mid := u.Match.ID()
	makerID := u.Match.Maker.ID()
	takerID := u.Match.Taker.ID()
	return encode.BuildyBytes{0}.
		AddData([]byte(u.Market)).
		AddData(encode.Uint32Bytes(u.Base)).
		AddData(encode.Uint32Bytes(u.Quote)).
		AddData(u.Cancel.Serialize()).
		AddData(u.TargetOrderID[:]).
		AddData(u.TargetAccount[:]).
		AddData(boolBytes(u.TargetSell)).
		AddData(int64Bytes(u.EpochIdx)).
		AddData(int64Bytes(u.EpochDur)).
		AddData(encode.Uint64Bytes(u.FeeRateBase)).
		AddData(encode.Uint64Bytes(u.FeeRateQuote)).
		AddData(int64Bytes(u.MatchServerTime.UnixMilli())).
		AddData(mid[:]).
		AddData(makerID[:]).
		AddData(takerID[:]).
		AddData(encode.Uint64Bytes(u.Match.Quantity)).
		AddData(encode.Uint64Bytes(u.Match.Rate)).
		AddData([]byte{byte(u.Match.Status)}), nil
}

// EventTxData returns the versioned transaction data recorded in the event log
// for an orders_revoked event.
func (u *OrdersRevokedUpdate) EventTxData() ([]byte, error) {
	if u == nil {
		return nil, fmt.Errorf("nil orders revoked update")
	}
	orders, err := orderIDsTxData(u.Orders)
	if err != nil {
		return nil, err
	}
	if len(orders) > encode.MaxDataLen {
		return nil, fmt.Errorf("revoked order data size %d exceeds maximum %d", len(orders), encode.MaxDataLen)
	}
	return encode.BuildyBytes{0}.
		AddData([]byte{byte(u.Reason)}).
		AddData(int64Bytes(u.RevokeTime.UnixMilli())).
		AddData(orders), nil
}
