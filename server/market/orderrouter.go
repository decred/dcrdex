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
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/server/account"
	"decred.org/dcrdex/server/asset"
	"decred.org/dcrdex/server/matcher"
)

const maxClockOffset = 10_000 // milliseconds

// The AuthManager handles client-related actions, including authorization and
// communications.
type AuthManager interface {
	Route(route string, handler func(account.AccountID, *msgjson.Message) *msgjson.Error)
	Auth(user account.AccountID, msg, sig []byte) error
	Sign(...msgjson.Signable)
	Send(account.AccountID, *msgjson.Message)
}

// MarketTunnel is a connection to a market and information about existing
// swaps.
type MarketTunnel interface {
	// SubmitOrder submits the order to the market for insertion into the epoch
	// queue.
	SubmitOrder(*orderRecord) error
	// MidGap returns the mid-gap market rate, which is ths rate halfway between
	// the best buy order and the best sell order in the order book.
	MidGap() uint64
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
	CoinLocked(coinID order.CoinID, assetID uint32) bool
	// Cancelable determines whether an order is cancelable. A cancelable order
	// is a limit order with time-in-force standing either in the epoch queue or
	// in the order book.
	Cancelable(order.OrderID) bool
	// TxMonitored determines whether the transaction for the given user is
	// involved in a DEX-monitored trade. Change outputs from DEX-monitored trades
	// can be used in other orders without waiting for fundConf confirmations.
	TxMonitored(user account.AccountID, txid string) bool // TODO specify asset?
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
	auth     AuthManager
	assets   map[uint32]*asset.BackedAsset
	tunnels  map[string]MarketTunnel
	mbBuffer float64
}

// OrderRouterConfig is the configuration settings for an OrderRouter.
type OrderRouterConfig struct {
	AuthManager     AuthManager
	Assets          map[uint32]*asset.BackedAsset
	Markets         map[string]MarketTunnel
	MarketBuyBuffer float64
}

// NewOrderRouter is a constructor for an OrderRouter.
func NewOrderRouter(cfg *OrderRouterConfig) *OrderRouter {
	router := &OrderRouter{
		auth:     cfg.AuthManager,
		assets:   cfg.Assets,
		tunnels:  cfg.Markets,
		mbBuffer: cfg.MarketBuyBuffer,
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
	limit := new(msgjson.Limit)
	err := json.Unmarshal(msg.Payload, limit)
	if err != nil {
		return msgjson.NewError(msgjson.RPCParseError, "error decoding 'limit' payload")
	}

	rpcErr := r.verifyAccount(user, limit.AccountID, limit)
	if rpcErr != nil {
		return rpcErr
	}

	tunnel, coins, sell, rpcErr := r.extractMarketDetails(&limit.Prefix, &limit.Trade)
	if rpcErr != nil {
		return rpcErr
	}

	// Check that OrderType is set correctly
	if limit.OrderType != msgjson.LimitOrderNum {
		return msgjson.NewError(msgjson.OrderParameterError, "wrong order type set for limit order")
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
		return msgjson.NewError(msgjson.OrderParameterError, "rate not a multiple of ratestep")
	}

	// Calculate the fees and check that the utxo sum is enough.
	swapVal := limit.Quantity
	if !sell {
		swapVal = matcher.BaseToQuote(limit.Rate, limit.Quantity)
	}
	reqVal := calc.RequiredFunds(swapVal, spendSize, &coins.funding.Asset)
	if valSum < reqVal {
		return msgjson.NewError(msgjson.FundingError,
			fmt.Sprintf("not enough funds. need at least %d, got %d", reqVal, valSum))
	}

	// Check time-in-force
	if !(limit.TiF == msgjson.StandingOrderNum || limit.TiF == msgjson.ImmediateOrderNum) {
		return msgjson.NewError(msgjson.OrderParameterError, "unknown time-in-force")
	}

	// Create the limit order
	lo := &order.LimitOrder{
		MarketOrder: order.MarketOrder{
			Prefix: order.Prefix{
				AccountID:  user,
				BaseAsset:  limit.Base,
				QuoteAsset: limit.Quote,
				OrderType:  order.LimitOrderType,
				ClientTime: order.UnixTimeMilli(int64(limit.ClientTime)),
				//ServerTime set in epoch queue processing pipeline.
			},
			Coins:    utxos,
			Sell:     sell,
			Quantity: limit.Quantity,
			Address:  limit.Address,
		},
		Rate:  limit.Rate,
		Force: order.StandingTiF,
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
// msgjson.Market payload, validates the information, constructs an
// order.MarketOrder and submits it to the epoch queue.
func (r *OrderRouter) handleMarket(user account.AccountID, msg *msgjson.Message) *msgjson.Error {
	market := new(msgjson.Market)
	err := json.Unmarshal(msg.Payload, market)
	if err != nil {
		return msgjson.NewError(msgjson.RPCParseError, "error decoding 'market' payload")
	}

	rpcErr := r.verifyAccount(user, market.AccountID, market)
	if rpcErr != nil {
		return rpcErr
	}

	tunnel, assets, sell, rpcErr := r.extractMarketDetails(&market.Prefix, &market.Trade)
	if rpcErr != nil {
		return rpcErr
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
	var reqVal uint64
	if sell {
		reqVal = calc.RequiredFunds(market.Quantity, spendSize, &assets.funding.Asset)
	} else {
		// This is a market buy order, so the quantity gets special handling.
		// 1. The quantity is in units of the quote asset.
		// 2. The quantity has to satisfy the market buy buffer.
		reqVal = matcher.QuoteToBase(tunnel.MidGap(), market.Quantity)
		lotWithBuffer := uint64(float64(assets.base.LotSize) * r.mbBuffer)
		minReq := matcher.QuoteToBase(tunnel.MidGap(), lotWithBuffer)
		if reqVal < minReq {
			return msgjson.NewError(msgjson.FundingError, "order quantity does not satisfy market buy buffer")
		}
	}
	if valSum < reqVal {
		return msgjson.NewError(msgjson.FundingError,
			fmt.Sprintf("not enough funds. need at least %d, got %d", reqVal, valSum))
	}
	// Create the market order
	clientTime := order.UnixTimeMilli(int64(market.ClientTime))
	serverTime := time.Now().Round(time.Millisecond).UTC()
	mo := &order.MarketOrder{
		Prefix: order.Prefix{
			AccountID:  user,
			BaseAsset:  market.Base,
			QuoteAsset: market.Quote,
			OrderType:  order.MarketOrderType,
			ClientTime: clientTime,
			ServerTime: serverTime,
		},
		Coins:    coins,
		Sell:     sell,
		Quantity: market.Quantity,
		Address:  market.Address,
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
	cancel := new(msgjson.Cancel)
	err := json.Unmarshal(msg.Payload, cancel)
	if err != nil {
		return msgjson.NewError(msgjson.RPCParseError, "error decoding 'cancel' payload")
	}

	rpcErr := r.verifyAccount(user, cancel.AccountID, cancel)
	if rpcErr != nil {
		return rpcErr
	}

	tunnel, rpcErr := r.extractMarket(&cancel.Prefix)
	if rpcErr != nil {
		return rpcErr
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

	// Create the cancel order
	clientTime := order.UnixTimeMilli(int64(cancel.ClientTime))
	serverTime := time.Now().Round(time.Millisecond).UTC()
	co := &order.CancelOrder{
		Prefix: order.Prefix{
			AccountID:  user,
			BaseAsset:  cancel.Base,
			QuoteAsset: cancel.Quote,
			OrderType:  order.MarketOrderType,
			ClientTime: clientTime,
			ServerTime: serverTime,
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
	sigMsg, _ := signable.Serialize()
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

// extractMarketDetails finds the MarketTunnel, side, and an assetSet for the
// provided prefix.
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
	offset := order.UnixMilli(time.Now()) - int64(prefix.ClientTime)
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
		// Check that the outpoint isn't locked.
		locked := tunnel.CoinLocked(order.CoinID(coin.ID), coinAssetID)
		if locked {
			return errSet(msgjson.FundingError,
				fmt.Sprintf("coin %x is locked", coin.ID))
		}
		// Get the coin from the backend and validate it.
		dexCoin, err := assets.funding.Backend.Coin(coin.ID, coin.Redeem)
		if err != nil {
			return errSet(msgjson.FundingError,
				fmt.Sprintf("error retreiving coin %x", coin.ID))
		}
		// Make sure the UTXO has the requisite number of confirmations.
		confs, err := dexCoin.Confirmations()
		if err != nil {
			return errSet(msgjson.FundingError,
				fmt.Sprintf("coin confirmations error for %x: %v", coin.ID, err))
		}
		if confs < int64(assets.funding.FundConf) && !tunnel.TxMonitored(user, dexCoin.TxID()) {
			return errSet(msgjson.FundingError,
				fmt.Sprintf("not enough confirmations for %x. require %d, have %d",
					coin.ID, assets.funding.FundConf, confs))
		}
		err = dexCoin.Auth(msgBytesToBytes(coin.PubKeys), msgBytesToBytes(coin.Sigs), coin.ID)
		if err != nil {
			return errSet(msgjson.CoinAuthError,
				fmt.Sprintf("failed to authorize coin %x", coin.ID))
		}
		var id []byte = coin.ID
		coinIDs = append(coinIDs, id)
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
