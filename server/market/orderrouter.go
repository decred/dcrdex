// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package market

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/server/account"
	"decred.org/dcrdex/server/asset"
	"decred.org/dcrdex/server/comms"
	"decred.org/dcrdex/server/matcher"
)

const maxClockOffset = 600_000 // milliseconds => 600 sec => 10 minutes

// The AuthManager handles client-related actions, including authorization and
// communications.
type AuthManager interface {
	Route(route string, handler func(account.AccountID, *msgjson.Message) *msgjson.Error)
	Auth(user account.AccountID, msg, sig []byte) error
	Suspended(user account.AccountID) (found, suspended bool)
	Sign(...msgjson.Signable) error
	Send(account.AccountID, *msgjson.Message) error
	SendWhenConnected(account.AccountID, *msgjson.Message, time.Duration, func())
	Request(account.AccountID, *msgjson.Message, func(comms.Link, *msgjson.Message)) error
	RequestWithTimeout(account.AccountID, *msgjson.Message, func(comms.Link, *msgjson.Message), time.Duration, func()) error
	Penalize(user account.AccountID, rule account.Rule) error
	RecordCancel(user account.AccountID, oid, target order.OrderID, t time.Time)
}

const DefaultConnectTimeout = 10 * time.Minute

// MarketTunnel is a connection to a market and information about existing
// swaps.
type MarketTunnel interface {
	// SubmitOrder submits the order to the market for insertion into the epoch
	// queue.
	SubmitOrder(*orderRecord) error
	// MidGap returns the mid-gap market rate, which is ths rate halfway between
	// the best buy order and the best sell order in the order book.
	MidGap() uint64
	// MarketBuyBuffer is a coefficient that when multiplied by the market's lot
	// size specifies the minimum required amount for a market buy order.
	MarketBuyBuffer() float64
	// CoinLocked should return true if the CoinID is currently a funding Coin
	// for an active DEX order. This is required for Coin validation to prevent
	// a user from submitting multiple orders spending the same Coin. This
	// method will likely need to check all orders currently in the epoch queue,
	// the order book, and the swap monitor, since UTXOs will still be unspent
	// according to the asset backends until the client broadcasts their
	// initialization transaction.
	//
	// DRAFT NOTE: This function could also potentially be handled by persistent
	// storage, since active orders and active matches are tracked there.
	CoinLocked(assetID uint32, coinID order.CoinID) bool
	// Cancelable determines whether an order is cancelable. A cancelable order
	// is a limit order with time-in-force standing either in the epoch queue or
	// in the order book.
	Cancelable(order.OrderID) bool

	// Suspend suspends the market as soon as a given time, returning the final
	// epoch index and and time at which that epoch closes.
	Suspend(asSoonAs time.Time, persistBook bool) (finalEpochIdx int64, finalEpochEnd time.Time)

	// Running indicates is the market is accepting new orders. This will return
	// false when suspended, but false does not necessarily mean Run has stopped
	// since a start epoch may be set.
	Running() bool
}

// orderRecord contains the information necessary to respond to an order
// request.
type orderRecord struct {
	order order.Order
	req   msgjson.Stampable
	msgID uint64
}

// assetSet is pointers to two different assets, but with 4 ways of addressing
// them.
type assetSet struct {
	funding   *asset.BackedAsset
	receiving *asset.BackedAsset
	base      *asset.BackedAsset
	quote     *asset.BackedAsset
}

// newAssetSet is a constructor for an assetSet.
func newAssetSet(base, quote *asset.BackedAsset, sell bool) *assetSet {
	coins := &assetSet{
		quote:     quote,
		base:      base,
		funding:   quote,
		receiving: base,
	}
	if sell {
		coins.funding, coins.receiving = base, quote
	}
	return coins
}

// outpoint satisfies order.Outpoint.
type outpoint struct {
	hash []byte
	vout uint32
}

// newOutpoint is a constructor for an outpoint.
func newOutpoint(h []byte, v uint32) *outpoint {
	return &outpoint{
		hash: h,
		vout: v,
	}
}

// Txhash is a getter for the outpoint's hash.
func (o *outpoint) TxHash() []byte { return o.hash }

// Vout is a getter for the outpoint's vout.
func (o *outpoint) Vout() uint32 { return o.vout }

// OrderRouter handles the 'limit', 'market', and 'cancel' DEX routes. These
// are authenticated routes used for placing and canceling orders.
type OrderRouter struct {
	auth    AuthManager
	assets  map[uint32]*asset.BackedAsset
	tunnels map[string]MarketTunnel
}

// OrderRouterConfig is the configuration settings for an OrderRouter.
type OrderRouterConfig struct {
	AuthManager AuthManager
	Assets      map[uint32]*asset.BackedAsset
	Markets     map[string]MarketTunnel
}

// NewOrderRouter is a constructor for an OrderRouter.
func NewOrderRouter(cfg *OrderRouterConfig) *OrderRouter {
	router := &OrderRouter{
		auth:    cfg.AuthManager,
		assets:  cfg.Assets,
		tunnels: cfg.Markets,
	}
	cfg.AuthManager.Route(msgjson.LimitRoute, router.handleLimit)
	cfg.AuthManager.Route(msgjson.MarketRoute, router.handleMarket)
	cfg.AuthManager.Route(msgjson.CancelRoute, router.handleCancel)
	return router
}

// handleLimit is the handler for the 'limit' route. This route accepts a
// msgjson.Limit payload, validates the information, constructs an
// order.LimitOrder and submits it to the epoch queue.
func (r *OrderRouter) handleLimit(user account.AccountID, msg *msgjson.Message) *msgjson.Error {
	limit := new(msgjson.LimitOrder)
	err := json.Unmarshal(msg.Payload, limit)
	if err != nil {
		return msgjson.NewError(msgjson.RPCParseError, "error decoding 'limit' payload")
	}

	rpcErr := r.verifyAccount(user, limit.AccountID, limit)
	if rpcErr != nil {
		return rpcErr
	}

	if _, suspended := r.auth.Suspended(user); suspended {
		return msgjson.NewError(msgjson.MarketNotRunningError, "suspended account may not submit trade orders")
	}

	tunnel, coins, sell, rpcErr := r.extractMarketDetails(&limit.Prefix, &limit.Trade)
	if rpcErr != nil {
		return rpcErr
	}

	// Spare some resources if the market is closed now. Any orders that make it
	// through to a closed market will receive a similar error from SubmitOrder.
	if !tunnel.Running() {
		return msgjson.NewError(msgjson.MarketNotRunningError, "market closed to new orders")
	}

	// Check that OrderType is set correctly
	if limit.OrderType != msgjson.LimitOrderNum {
		return msgjson.NewError(msgjson.OrderParameterError, "wrong order type set for limit order. wanted %d, got %d", msgjson.LimitOrderNum, limit.OrderType)
	}

	valSum, spendSize, utxos, rpcErr := r.checkPrefixTrade(user, tunnel, coins, &limit.Prefix, &limit.Trade, true)
	if rpcErr != nil {
		return rpcErr
	}

	// Check that the rate is non-zero and obeys the rate step interval.
	if limit.Rate == 0 {
		return msgjson.NewError(msgjson.OrderParameterError, "rate = 0 not allowed")
	}
	if limit.Rate%coins.quote.RateStep != 0 {
		return msgjson.NewError(msgjson.OrderParameterError, "rate (%d) not a multiple of ratestep (%d)",
			limit.Rate, coins.quote.RateStep)
	}

	// Calculate the fees and check that the utxo sum is enough.
	swapVal := limit.Quantity
	lots := swapVal / coins.base.LotSize
	if !sell {
		swapVal = matcher.BaseToQuote(limit.Rate, limit.Quantity)
	}
	fundAsset := &coins.funding.Asset
	reqVal := calc.RequiredOrderFunds(swapVal, uint64(spendSize), lots, fundAsset)
	if valSum < reqVal {
		return msgjson.NewError(msgjson.FundingError,
			fmt.Sprintf("not enough funds. need at least %d, got %d", reqVal, valSum))
	}

	// Check time-in-force
	var force order.TimeInForce
	switch limit.TiF {
	case msgjson.StandingOrderNum:
		force = order.StandingTiF
	case msgjson.ImmediateOrderNum:
		force = order.ImmediateTiF
	default:
		return msgjson.NewError(msgjson.OrderParameterError, "unknown time-in-force")
	}

	// Commitment.
	if len(limit.Commit) != order.CommitmentSize {
		return msgjson.NewError(msgjson.OrderParameterError, "invalid commitment")
	}
	var commit order.Commitment
	copy(commit[:], limit.Commit)

	// Create the limit order.
	lo := &order.LimitOrder{
		P: order.Prefix{
			AccountID:  user,
			BaseAsset:  limit.Base,
			QuoteAsset: limit.Quote,
			OrderType:  order.LimitOrderType,
			ClientTime: encode.UnixTimeMilli(int64(limit.ClientTime)),
			//ServerTime set in epoch queue processing pipeline.
			Commit: commit,
		},
		T: order.Trade{
			Coins:    utxos,
			Sell:     sell,
			Quantity: limit.Quantity,
			Address:  limit.Address,
		},
		Rate:  limit.Rate,
		Force: force,
	}

	// NOTE: ServerTime is not yet set, so the order's ID, which is computed
	// from the serialized order, is not yet valid. The Market will stamp the
	// order on receipt, and the order ID will be valid.

	// Send the order to the epoch queue where it will be time stamped.
	oRecord := &orderRecord{
		order: lo,
		req:   limit,
		msgID: msg.ID,
	}
	if err := tunnel.SubmitOrder(oRecord); err != nil {
		log.Warnf("Market failed to SubmitOrder: %v", err)
		return msgjson.NewError(msgjson.UnknownMarketError, "failed to submit order")
	}
	return nil
}

// handleMarket is the handler for the 'market' route. This route accepts a
// msgjson.MarketOrder payload, validates the information, constructs an
// order.MarketOrder and submits it to the epoch queue.
func (r *OrderRouter) handleMarket(user account.AccountID, msg *msgjson.Message) *msgjson.Error {
	market := new(msgjson.MarketOrder)
	err := json.Unmarshal(msg.Payload, market)
	if err != nil {
		return msgjson.NewError(msgjson.RPCParseError, "error decoding 'market' payload")
	}

	rpcErr := r.verifyAccount(user, market.AccountID, market)
	if rpcErr != nil {
		return rpcErr
	}

	if _, suspended := r.auth.Suspended(user); suspended {
		return msgjson.NewError(msgjson.MarketNotRunningError, "suspended account may not submit trade orders")
	}

	tunnel, assets, sell, rpcErr := r.extractMarketDetails(&market.Prefix, &market.Trade)
	if rpcErr != nil {
		return rpcErr
	}

	if !tunnel.Running() {
		mktName, _ := dex.MarketName(market.Base, market.Quote)
		return msgjson.NewError(msgjson.MarketNotRunningError, "market %s closed to new orders", mktName)
	}

	// Check that OrderType is set correctly
	if market.OrderType != msgjson.MarketOrderNum {
		return msgjson.NewError(msgjson.OrderParameterError, "wrong order type set for market order")
	}

	// Passing sell as the checkLot parameter causes the lot size check to be
	// ignored for market buy orders.
	valSum, spendSize, coins, rpcErr := r.checkPrefixTrade(user, tunnel, assets, &market.Prefix, &market.Trade, sell)
	if rpcErr != nil {
		return rpcErr
	}

	// Calculate the fees and check that the utxo sum is enough.
	fundAsset := &assets.funding.Asset
	var reqVal uint64
	if sell {
		lots := market.Quantity / assets.base.LotSize
		reqVal = calc.RequiredOrderFunds(market.Quantity, uint64(spendSize), lots, fundAsset)
	} else {
		// This is a market buy order, so the quantity gets special handling.
		// 1. The quantity is in units of the quote asset.
		// 2. The quantity has to satisfy the market buy buffer.
		midGap := tunnel.MidGap()
		buyBuffer := tunnel.MarketBuyBuffer()
		lotWithBuffer := uint64(float64(assets.base.LotSize) * buyBuffer)
		minReq := matcher.BaseToQuote(midGap, lotWithBuffer)
		reqVal = calc.RequiredOrderFunds(minReq, uint64(spendSize), 1, &assets.base.Asset)

		// TODO: I'm pretty sure that if there are no orders on the book, the
		// midGap will be zero, and so will minReq, meaning any Quantity would
		// be accepted. Is this a security concern?

		if market.Quantity < minReq {
			errStr := fmt.Sprintf("order quantity does not satisfy market buy buffer. %d < %d. midGap = %d", market.Quantity, minReq, midGap)
			return msgjson.NewError(msgjson.FundingError, errStr)
		}
	}
	if valSum < reqVal {
		return msgjson.NewError(msgjson.FundingError,
			fmt.Sprintf("not enough funds. need at least %d, got %d", reqVal, valSum))
	}

	// Commitment.
	if len(market.Commit) != order.CommitmentSize {
		return msgjson.NewError(msgjson.OrderParameterError, "invalid commitment")
	}
	var commit order.Commitment
	copy(commit[:], market.Commit)

	// Create the market order
	mo := &order.MarketOrder{
		P: order.Prefix{
			AccountID:  user,
			BaseAsset:  market.Base,
			QuoteAsset: market.Quote,
			OrderType:  order.MarketOrderType,
			ClientTime: encode.UnixTimeMilli(int64(market.ClientTime)),
			//ServerTime set in epoch queue processing pipeline.
			Commit: commit,
		},
		T: order.Trade{
			Coins:    coins,
			Sell:     sell,
			Quantity: market.Quantity,
			Address:  market.Address,
		},
	}

	// Send the order to the epoch queue.
	oRecord := &orderRecord{
		order: mo,
		req:   market,
		msgID: msg.ID,
	}
	if err := tunnel.SubmitOrder(oRecord); err != nil {
		log.Warnf("Market failed to SubmitOrder: %v", err)
		return msgjson.NewError(msgjson.UnknownMarketError, "failed to submit order")
	}
	return nil
}

// handleCancel is the handler for the 'cancel' route. This route accepts a
// msgjson.Cancel payload, validates the information, constructs an
// order.CancelOrder and submits it to the epoch queue.
func (r *OrderRouter) handleCancel(user account.AccountID, msg *msgjson.Message) *msgjson.Error {
	cancel := new(msgjson.CancelOrder)
	err := json.Unmarshal(msg.Payload, cancel)
	if err != nil {
		return msgjson.NewError(msgjson.RPCParseError, "error decoding 'cancel' payload")
	}

	rpcErr := r.verifyAccount(user, cancel.AccountID, cancel)
	if rpcErr != nil {
		return rpcErr
	}

	// Consideration: allow suspended accounts to submit cancel orders? Depends
	// if their orders get canceled on suspension or if they simply cannot make
	// new orders.
	// if _, suspended := r.auth.Suspended(user); suspended {
	// 	return msgjson.NewError(msgjson.MarketNotRunningError, "suspended account may not submit cancel orders")
	// }

	tunnel, rpcErr := r.extractMarket(&cancel.Prefix)
	if rpcErr != nil {
		return rpcErr
	}

	if !tunnel.Running() {
		mktName, _ := dex.MarketName(cancel.Base, cancel.Quote)
		return msgjson.NewError(msgjson.MarketNotRunningError, "market %s closed to new orders", mktName)
	}

	if len(cancel.TargetID) != order.OrderIDSize {
		return msgjson.NewError(msgjson.OrderParameterError, "invalid target ID format")
	}
	var targetID order.OrderID
	copy(targetID[:], cancel.TargetID)

	if !tunnel.Cancelable(targetID) {
		return msgjson.NewError(msgjson.OrderParameterError, "target order not known")
	}

	// Check that OrderType is set correctly
	if cancel.OrderType != msgjson.CancelOrderNum {
		return msgjson.NewError(msgjson.OrderParameterError, "wrong order type set for cancel order")
	}

	rpcErr = checkTimes(&cancel.Prefix)
	if rpcErr != nil {
		return rpcErr
	}

	// Commitment.
	if len(cancel.Commit) != order.CommitmentSize {
		return msgjson.NewError(msgjson.OrderParameterError, "invalid commitment")
	}
	var commit order.Commitment
	copy(commit[:], cancel.Commit)

	// Create the cancel order
	co := &order.CancelOrder{
		P: order.Prefix{
			AccountID:  user,
			BaseAsset:  cancel.Base,
			QuoteAsset: cancel.Quote,
			OrderType:  order.CancelOrderType,
			ClientTime: encode.UnixTimeMilli(int64(cancel.ClientTime)),
			//ServerTime set in epoch queue processing pipeline.
			Commit: commit,
		},
		TargetOrderID: targetID,
	}

	// Send the order to the epoch queue.
	oRecord := &orderRecord{
		order: co,
		req:   cancel,
		msgID: msg.ID,
	}
	if err := tunnel.SubmitOrder(oRecord); err != nil {
		log.Warnf("Market failed to SubmitOrder: %v", err)
		return msgjson.NewError(msgjson.UnknownMarketError, "failed to submit order")
	}
	return nil
}

// verifyAccount checks that the submitted order squares with the submitting user.
func (r *OrderRouter) verifyAccount(user account.AccountID, msgAcct msgjson.Bytes, signable msgjson.Signable) *msgjson.Error {
	// Verify account ID matches.
	if !bytes.Equal(user[:], msgAcct) {
		return msgjson.NewError(msgjson.OrderParameterError, "account ID mismatch")
	}
	// Check the clients signature of the order.
	// DRAFT NOTE: These Serialize methods actually never return errors. We should
	// just drop the error return value.
	sigMsg := signable.Serialize()
	err := r.auth.Auth(user, sigMsg, signable.SigBytes())
	if err != nil {
		return msgjson.NewError(msgjson.SignatureError, "signature error: "+err.Error())
	}
	return nil
}

// extractMarket finds the MarketTunnel for the provided prefix.
func (r *OrderRouter) extractMarket(prefix *msgjson.Prefix) (MarketTunnel, *msgjson.Error) {
	mktName, err := dex.MarketName(prefix.Base, prefix.Quote)
	if err != nil {
		return nil, msgjson.NewError(msgjson.UnknownMarketError, "asset lookup error: "+err.Error())
	}
	tunnel, found := r.tunnels[mktName]
	if !found {
		return nil, msgjson.NewError(msgjson.UnknownMarketError, "unknown market "+mktName)
	}
	return tunnel, nil
}

// SuspendEpoch holds the index and end time of final epoch marking the
// suspension of a market.
type SuspendEpoch struct {
	Idx int64
	End time.Time
}

// SuspendMarket schedules a suspension of a given market, with the option to
// persist the orders on the book (or purge the book automatically on market
// shutdown). The scheduled final epoch and suspend time are returned. Note that
// OrderRouter is a proxy for this request to the ultimate Market. This is done
// because OrderRouter is the entry point for new orders into the market. TODO:
// track running, suspended, and scheduled-suspended markets, appropriately
// blocking order submission according to the schedule rather than just checking
// Market.Running prior to submitting incoming orders to the Market.
func (r *OrderRouter) SuspendMarket(mktName string, asSoonAs time.Time, persistBooks bool) *SuspendEpoch {
	mkt, found := r.tunnels[mktName]
	if !found {
		return nil
	}

	idx, t := mkt.Suspend(asSoonAs, persistBooks)
	return &SuspendEpoch{
		Idx: idx,
		End: t,
	}
}

// Suspend is like SuspendMarket, but for all known markets. TODO: use this in a
// "suspend all as soon as" DEX function with rather than shutting down in the
// middle of an active epoch as SIGINT shutdown presently does.
func (r *OrderRouter) Suspend(asSoonAs time.Time, persistBooks bool) map[string]*SuspendEpoch {

	suspendTimes := make(map[string]*SuspendEpoch, len(r.tunnels))
	for name, mkt := range r.tunnels {
		idx, ts := mkt.Suspend(asSoonAs, persistBooks)
		suspendTimes[name] = &SuspendEpoch{Idx: idx, End: ts}
	}

	// MarketTunnel.Running will return false when the market closes, and true
	// when and if it opens again. Locking/blocking of the incoming order
	// handlers is not necessary since any orders that sneak in to a Market will
	// be rejected if there is no active epoch.

	return suspendTimes
}

// extractMarketDetails finds the MarketTunnel, an assetSet, and market side for
// the provided prefix.
func (r *OrderRouter) extractMarketDetails(prefix *msgjson.Prefix, trade *msgjson.Trade) (MarketTunnel, *assetSet, bool, *msgjson.Error) {
	// Check that assets are for a valid market.
	tunnel, rpcErr := r.extractMarket(prefix)
	if rpcErr != nil {
		return nil, nil, false, rpcErr
	}
	// Side must be one of buy or sell
	var sell bool
	switch trade.Side {
	case msgjson.BuyOrderNum:
	case msgjson.SellOrderNum:
		sell = true
	default:
		return nil, nil, false, msgjson.NewError(msgjson.OrderParameterError,
			fmt.Sprintf("invalid side value %d", trade.Side))
	}
	quote, found := r.assets[prefix.Quote]
	if !found {
		panic("missing quote asset for known market should be impossible")
	}
	base, found := r.assets[prefix.Base]
	if !found {
		panic("missing base asset for known market should be impossible")
	}
	return tunnel, newAssetSet(base, quote, sell), sell, nil
}

// checkTimes validates the timestamps in an order prefix.
func checkTimes(prefix *msgjson.Prefix) *msgjson.Error {
	offset := encode.UnixMilli(time.Now()) - int64(prefix.ClientTime)
	if offset < 0 {
		offset *= -1
	}
	if offset >= maxClockOffset {
		return msgjson.NewError(msgjson.ClockRangeError, fmt.Sprintf(
			"clock offset of %d ms is larger than maximum allowed, %d ms",
			offset, maxClockOffset,
		))
	}
	// Server time should be unset.
	if prefix.ServerTime != 0 {
		return msgjson.NewError(msgjson.OrderParameterError, "non-zero server time not allowed")
	}
	return nil
}

// checkPrefixTrade validates the information in the prefix and trade portions
// of an order.
func (r *OrderRouter) checkPrefixTrade(user account.AccountID, tunnel MarketTunnel, assets *assetSet, prefix *msgjson.Prefix,
	trade *msgjson.Trade, checkLot bool) (uint64, uint32, []order.CoinID, *msgjson.Error) {
	// Check that the client's timestamp is still valid.
	rpcErr := checkTimes(prefix)
	if rpcErr != nil {
		return 0, 0, nil, rpcErr
	}
	errSet := func(code int, message string) (uint64, uint32, []order.CoinID, *msgjson.Error) {
		return 0, 0, nil, msgjson.NewError(code, message)
	}
	// Check that the address is valid.
	if !assets.receiving.Backend.CheckAddress(trade.Address) {
		return errSet(msgjson.OrderParameterError, "address doesn't check")
	}
	// Quantity cannot be zero, and must be an integral multiple of the lot size.
	if trade.Quantity == 0 {
		return errSet(msgjson.OrderParameterError, "zero quantity not allowed")
	}
	if checkLot && trade.Quantity%assets.base.LotSize != 0 {
		return errSet(msgjson.OrderParameterError, "order quantity not a multiple of lot size")
	}
	// Validate UTXOs
	// Check that all required arrays are of equal length.
	if len(trade.Coins) == 0 {
		return errSet(msgjson.FundingError, "order must specify utxos")
	}
	var valSum uint64
	var spendSize uint32
	var coinIDs []order.CoinID
	coinAssetID := assets.funding.ID
	for i, coin := range trade.Coins {
		sigCount := len(coin.Sigs)
		if sigCount == 0 {
			return errSet(msgjson.SignatureError, fmt.Sprintf("no signature for coin %d", i))
		}
		if len(coin.PubKeys) != sigCount {
			return errSet(msgjson.OrderParameterError, fmt.Sprintf(
				"pubkey count %d not equal to signature count %d for coin %d",
				len(coin.PubKeys), sigCount, i,
			))
		}
		// Get the coin from the backend and validate it.
		dexCoin, err := assets.funding.Backend.FundingCoin(coin.ID, coin.Redeem)
		if err != nil {
			log.Debugf("FundingCoin error for %s coin %v: %v", assets.funding.Symbol, coin.ID, err)
			return errSet(msgjson.FundingError,
				fmt.Sprintf("error retrieving coin ID %v", coin.ID))
		}
		// FundingCoin must ensure that the coin requires at least one signature
		// to spend, and that the redeem script is not non-standard.

		// Check that the outpoint isn't locked.
		locked := tunnel.CoinLocked(coinAssetID, order.CoinID(coin.ID))
		if locked {
			return errSet(msgjson.FundingError,
				fmt.Sprintf("coin %v is locked", dexCoin))
		}

		// Verify that the user controls the funding coins.
		err = dexCoin.Auth(msgBytesToBytes(coin.PubKeys), msgBytesToBytes(coin.Sigs), coin.ID)
		if err != nil {
			log.Debugf("Auth error for %s coin %s: %v", assets.funding.Symbol, dexCoin, err)
			return errSet(msgjson.CoinAuthError,
				fmt.Sprintf("failed to authorize coin %v", dexCoin))
		}
		coinIDs = append(coinIDs, []byte(coin.ID))
		valSum += dexCoin.Value()
		spendSize += dexCoin.SpendSize()
	}
	return valSum, spendSize, coinIDs, nil
}

// msgBytesToBytes converts a []msgjson.Byte to a [][]byte.
func msgBytesToBytes(msgBs []msgjson.Bytes) [][]byte {
	b := make([][]byte, 0, len(msgBs))
	for _, msgB := range msgBs {
		b = append(b, msgB)
	}
	return b
}
