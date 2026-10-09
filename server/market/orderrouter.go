// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package market

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/dex/wait"
	"decred.org/dcrdex/server/account"
	"decred.org/dcrdex/server/asset"
	"decred.org/dcrdex/server/comms"
	"decred.org/dcrdex/server/db"
	"decred.org/dcrdex/server/matcher"
	"decred.org/dcrdex/server/mesh"
)

// The AuthManager handles client-related actions, including authorization and
// communications.
type AuthManager interface {
	Route(route string, handler func(account.AccountID, *msgjson.Message) *msgjson.Error)
	VerifyUserSig(user account.AccountID, msg, sig []byte) error
	AcctStatus(user account.AccountID) (connected bool, tier int64)
	Sign(...msgjson.Signable)
	Send(account.AccountID, *msgjson.Message) error
	SendIfLocal(account.AccountID, *msgjson.Message) error
	Request(account.AccountID, *msgjson.Message, func(comms.Link, *msgjson.Message)) error
	RequestIfLocal(account.AccountID, *msgjson.Message, func(comms.Link, *msgjson.Message)) error
	RequestWithTimeout(account.AccountID, *msgjson.Message, func(comms.Link, *msgjson.Message), time.Duration, func()) error
	ReputationOutcomePolicy() *db.ReputationOutcomePolicy
	UserReputationAt(user account.AccountID, asOf time.Time) (tier int64, score, maxScore int32, err error)
}

const (
	maxClockOffset = 600_000 // milliseconds => 600 sec => 10 minutes
	fundingTxWait  = time.Minute
	// ZeroConfFeeRateThreshold is multiplied by the last known fee rate for an
	// asset to attain a minimum fee rate acceptable for zero-conf funding
	// coins.
	ZeroConfFeeRateThreshold = 0.9
)

// MarketTunnel is a connection to a market.
type MarketTunnel interface {
	// AcceptOrderCommand validates and accepts an order.
	AcceptOrderCommand(context.Context, *orderRecord, *mesh.CommandCompletion) *msgjson.Error
	HandleOrderResubmission(ctx context.Context, rec *orderRecord, completion *mesh.CommandCompletion) (handled bool, rpcErr *msgjson.Error)
	// MidGap returns the mid-gap market rate, which is ths rate halfway between
	// the best buy order and the best sell order in the order book.
	MidGap() uint64
	// MarketBuyBuffer is a coefficient that when multiplied by the market's lot
	// size specifies the minimum required amount for a market buy order.
	MarketBuyBuffer() float64
	// LotSize is the market's lot size in units of the base asset.
	LotSize() uint64
	// RateStep is the market's rate step in units of the quote asset.
	RateStep() uint64
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

	// Running indicates is the market is accepting new orders. This will return
	// false when suspended, but false does not necessarily mean Run has stopped
	// since a start epoch may be set.
	Running() bool

	// CheckUnfilled revokes a user's unfilled booked orders whose funding
	// coins are spent and returns the revoked orders.
	CheckUnfilled(assetID uint32, user account.AccountID) []*order.LimitOrder

	// Parcels calculates the number of active parcels for the market.
	Parcels(user account.AccountID, settlingQty uint64) float64
}

type MarketParcelCalculator func(settlingQty uint64) (parcels float64)

// orderRecord contains an order and its client request.
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

// FeeSource is a source of the last reported tx fee rate estimate for an asset.
type FeeSource interface {
	LastRate(assetID uint32) (feeRate uint64)
}

// MatchSwapper is a source for information about settling matches.
type MatchSwapper interface {
	UnsettledQuantity(user account.AccountID) map[[2]uint32]uint64
}

// OrderRouter validates client orders and routes requests to their markets.
type OrderRouter struct {
	auth        AuthManager
	assets      map[uint32]*asset.BackedAsset
	tunnels     map[string]MarketTunnel
	latencyQ    *wait.TickerQueue
	feeSource   FeeSource
	dexBalancer *DEXBalancer
	swapper     MatchSwapper
	mesh        MeshService
}

// OrderRouterConfig is the configuration settings for an OrderRouter.
type OrderRouterConfig struct {
	AuthManager  AuthManager
	Assets       map[uint32]*asset.BackedAsset
	Markets      map[string]MarketTunnel
	FeeSource    FeeSource
	DEXBalancer  *DEXBalancer
	MatchSwapper MatchSwapper
}

// NewOrderRouter is a constructor for an OrderRouter.
func NewOrderRouter(cfg *OrderRouterConfig) *OrderRouter {
	router := &OrderRouter{
		auth:        cfg.AuthManager,
		assets:      cfg.Assets,
		tunnels:     cfg.Markets,
		latencyQ:    wait.NewTickerQueue(2 * time.Second),
		feeSource:   cfg.FeeSource,
		dexBalancer: cfg.DEXBalancer,
		swapper:     cfg.MatchSwapper,
	}
	cfg.AuthManager.Route(msgjson.LimitRoute, router.handleLimit)
	cfg.AuthManager.Route(msgjson.MarketRoute, router.handleMarket)
	cfg.AuthManager.Route(msgjson.CancelRoute, router.handleCancel)
	return router
}

// SetMeshService configures the mesh service. It must be set before the comms
// routes serve traffic.
func (r *OrderRouter) SetMeshService(mesh MeshService) {
	r.mesh = mesh
}

func (r *OrderRouter) Run(ctx context.Context) {
	r.latencyQ.Run(ctx)
}

func fundingCoin(backend asset.Backend, coinID []byte, redeemScript []byte) (asset.FundingCoin, error) {
	outputTracker, is := backend.(asset.OutputTracker)
	if !is {
		return nil, fmt.Errorf("fundingCoin requested for incapable asset")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	return outputTracker.FundingCoin(ctx, coinID, redeemScript)
}

func coinConfirmations(coin asset.Coin) (int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	return coin.Confirmations(ctx)
}

// handleLimit submits a limit order command.
func (r *OrderRouter) handleLimit(user account.AccountID, msg *msgjson.Message) *msgjson.Error {
	return r.mesh.ExecuteCommand(context.Background(), mesh.CommandRequest{
		Kind: commandKindLimit,
		User: user,
		Msg:  msg,
		Respond: func(resp *msgjson.Message) error {
			return r.auth.Send(user, resp)
		},
	})
}

// executeLimit validates a limit order request and submits the order for acceptance.
func (r *OrderRouter) executeLimit(cmdCtx *mesh.CommandContext) *msgjson.Error {
	user := cmdCtx.Request.User
	msg := cmdCtx.Request.Msg

	limit := new(msgjson.LimitOrder)
	err := msg.Unmarshal(&limit)
	if err != nil || limit == nil {
		return msgjson.NewError(msgjson.RPCParseError, "error decoding 'limit' payload")
	}

	rpcErr := r.verifyAccount(user, limit.AccountID, limit)
	if rpcErr != nil {
		return rpcErr
	}

	tunnel, assets, sell, rpcErr := r.extractMarketDetails(&limit.Prefix, &limit.Trade)
	if rpcErr != nil {
		return rpcErr
	}

	// Check that OrderType is set correctly
	if limit.OrderType != msgjson.LimitOrderNum {
		return msgjson.NewError(msgjson.OrderParameterError, "wrong order type set for limit order. wanted %d, got %d",
			msgjson.LimitOrderNum, limit.OrderType)
	}

	if limit.Rate == 0 {
		return msgjson.NewError(msgjson.OrderParameterError, "rate = 0 not allowed")
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

	// Commitment
	if len(limit.Commit) != order.CommitmentSize {
		return msgjson.NewError(msgjson.OrderParameterError, "invalid commitment")
	}
	var commit order.Commitment
	copy(commit[:], limit.Commit)

	coinIDs := make([]order.CoinID, 0, len(limit.Trade.Coins))
	for _, coin := range limit.Trade.Coins {
		coinID := order.CoinID(coin.ID)
		coinIDs = append(coinIDs, coinID)
	}

	// Create the limit order.
	lo := &order.LimitOrder{
		P: order.Prefix{
			AccountID:  user,
			BaseAsset:  limit.Base,
			QuoteAsset: limit.Quote,
			OrderType:  order.LimitOrderType,
			ClientTime: time.UnixMilli(int64(limit.ClientTime)),
			// ServerTime is set by command acceptance.
			Commit: commit,
		},
		T: order.Trade{
			Coins:    coinIDs,
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

	oRecord := &orderRecord{
		order: lo,
		req:   limit,
		msgID: msg.ID,
	}

	if handled, rpcErr := tunnel.HandleOrderResubmission(cmdCtx, oRecord, cmdCtx.Completion); handled {
		return rpcErr
	}

	if rateStep := tunnel.RateStep(); limit.Rate%rateStep != 0 {
		return msgjson.NewError(msgjson.OrderParameterError, "rate (%d) not a multiple of ratestep (%d)",
			limit.Rate, rateStep)
	}

	rpcErr = r.checkPrefixTrade(assets, tunnel.LotSize(), &limit.Prefix, &limit.Trade, true)
	if rpcErr != nil {
		return rpcErr
	}

	if _, tier := r.auth.AcctStatus(user); tier < 1 {
		return msgjson.NewError(msgjson.AccountClosedError, "account %v with tier %d may not submit trade orders", user, tier)
	}

	// Spare some resources if the market is closed now. Any orders that make it
	// through to a closed market will receive a similar error from the market
	// command handler.
	if !tunnel.Running() {
		return msgjson.NewError(msgjson.MarketNotRunningError, "market closed to new orders")
	}

	return r.processTrade(oRecord, tunnel, cmdCtx.Completion, assets, limit.Coins, sell, limit.Rate, limit.RedeemSig, limit.Serialize())
}

// handleMarket submits a market order command.
func (r *OrderRouter) handleMarket(user account.AccountID, msg *msgjson.Message) *msgjson.Error {
	return r.mesh.ExecuteCommand(context.Background(), mesh.CommandRequest{
		Kind: commandKindMarket,
		User: user,
		Msg:  msg,
		Respond: func(resp *msgjson.Message) error {
			return r.auth.Send(user, resp)
		},
	})
}

// executeMarket validates a market order request and submits the order for acceptance.
func (r *OrderRouter) executeMarket(cmdCtx *mesh.CommandContext) *msgjson.Error {
	user := cmdCtx.Request.User
	msg := cmdCtx.Request.Msg

	market := new(msgjson.MarketOrder)
	err := msg.Unmarshal(&market)
	if err != nil || market == nil {
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

	// Commitment.
	if len(market.Commit) != order.CommitmentSize {
		return msgjson.NewError(msgjson.OrderParameterError, "invalid commitment")
	}
	var commit order.Commitment
	copy(commit[:], market.Commit)

	coinIDs := make([]order.CoinID, 0, len(market.Trade.Coins))
	for _, coin := range market.Trade.Coins {
		coinID := order.CoinID(coin.ID)
		coinIDs = append(coinIDs, coinID)
	}

	// Create the market order
	mo := &order.MarketOrder{
		P: order.Prefix{
			AccountID:  user,
			BaseAsset:  market.Base,
			QuoteAsset: market.Quote,
			OrderType:  order.MarketOrderType,
			ClientTime: time.UnixMilli(int64(market.ClientTime)),
			// ServerTime is set by command acceptance.
			Commit: commit,
		},
		T: order.Trade{
			Coins:    coinIDs,
			Sell:     sell,
			Quantity: market.Quantity,
			Address:  market.Address,
		},
	}

	// Submit the order for acceptance.
	oRecord := &orderRecord{
		order: mo,
		req:   market,
		msgID: msg.ID,
	}

	if handled, rpcErr := tunnel.HandleOrderResubmission(cmdCtx, oRecord, cmdCtx.Completion); handled {
		return rpcErr
	}

	// Passing sell as the checkLot parameter causes the lot size check to be
	// ignored for market buy orders.
	rpcErr = r.checkPrefixTrade(assets, tunnel.LotSize(), &market.Prefix, &market.Trade, sell)
	if rpcErr != nil {
		return rpcErr
	}

	if _, tier := r.auth.AcctStatus(user); tier < 1 {
		return msgjson.NewError(msgjson.AccountClosedError, "account %v with tier %d may not submit trade orders", user, tier)
	}

	if !tunnel.Running() {
		mktName, _ := dex.MarketName(market.Base, market.Quote)
		return msgjson.NewError(msgjson.MarketNotRunningError, "market %s closed to new orders", mktName)
	}

	return r.processTrade(oRecord, tunnel, cmdCtx.Completion, assets, market.Coins, sell, 0, market.RedeemSig, market.Serialize())
}

// processTrade checks that the trade is valid and submits it to the market.
func (r *OrderRouter) processTrade(oRecord *orderRecord, tunnel MarketTunnel, completion *mesh.CommandCompletion, assets *assetSet,
	coins []*msgjson.Coin, sell bool, rate uint64, redeemSig *msgjson.RedeemSig, sigMsg []byte) *msgjson.Error {

	fundingAsset := assets.funding
	user := oRecord.order.User()
	trade := oRecord.order.Trade()

	// If the receiving asset is account-based, we need to check that they can
	// cover fees for the redemption, since they can't be subtracted from the
	// received amount.
	receivingBalancer, isToAccount := assets.receiving.Backend.(asset.AccountBalancer)
	if isToAccount {
		if redeemSig == nil {
			log.Infof("user %s did not include a RedeemSig for received asset %s", user, assets.receiving.Symbol)
			return msgjson.NewError(msgjson.OrderParameterError, "no redeem address verification included for asset %s", assets.receiving.Symbol)
		}

		acctAddr := trade.ToAccount()
		if err := receivingBalancer.ValidateSignature(acctAddr, redeemSig.PubKey, sigMsg, redeemSig.Sig); err != nil {
			log.Infof("user %s failed redeem signature validation for order: %v",
				user, err)
			return msgjson.NewError(msgjson.SignatureError, "redeem signature validation failed")
		}

		if !r.sufficientAccountBalance(acctAddr, oRecord.order, assets.receiving.Asset.ID, assets.receiving.ID, tunnel) {
			return msgjson.NewError(msgjson.FundingError, "insufficient balance")
		}
	}

	// If the funding asset is account-based, we'll check balance and submit the
	// order immediately, since we don't need to find coins.
	fundingBalancer, isAccountFunded := assets.funding.Backend.(asset.AccountBalancer)
	if isAccountFunded {
		// Validate that the coins are correct for an account-based-asset-funded
		// order. There should be 1 coin, 1 sig, 1 pubkey, and no redeem script.
		if len(coins) != 1 {
			log.Infof("user %s submitted an %s-funded order with %d coin IDs", user, assets.funding.Symbol, len(coins))
			return msgjson.NewError(msgjson.OrderParameterError, "account-type asset funding requires exactly one coin ID")
		}
		acctProof := coins[0]
		if len(acctProof.PubKeys) != 1 || len(acctProof.Sigs) != 1 || len(acctProof.Redeem) > 0 {
			log.Infof("user %s submitted an %s-funded order with %d pubkeys, %d sigs, redeem script length %d",
				user, assets.funding.Symbol, len(acctProof.PubKeys), len(acctProof.Sigs), len(acctProof.Redeem))
			return msgjson.NewError(msgjson.OrderParameterError, "account-type asset funding requires exactly one coin ID")
		}

		acctAddr := trade.FromAccount()
		pubKey := acctProof.PubKeys[0]
		sig := acctProof.Sigs[0]
		if err := fundingBalancer.ValidateSignature(acctAddr, pubKey, sigMsg, sig); err != nil {
			log.Infof("user %s failed signature validation for order: %v",
				user, err)
			return msgjson.NewError(msgjson.SignatureError, "signature validation failed")
		}

		if !r.sufficientAccountBalance(acctAddr, oRecord.order, assets.funding.Asset.ID, assets.receiving.ID, tunnel) {
			return msgjson.NewError(msgjson.FundingError, "insufficient balance")
		}
		return tunnel.AcceptOrderCommand(context.Background(), oRecord, completion)
	}

	// Funding coins are from a utxo-based asset. Need to find them.

	funder, is := assets.funding.Backend.(asset.OutputTracker)
	if !is {
		return msgjson.NewError(msgjson.RPCInternal, "internal error")
	}

	// Validate coin IDs and prepare some strings for debug logging.
	coinStrs := make([]string, 0, len(coins))
	for _, coinID := range trade.Coins {
		coinStr, err := fundingAsset.Backend.ValidateCoinID(coinID)
		if err != nil {
			return msgjson.NewError(msgjson.FundingError, "invalid coin ID %v: %v", coinID, err)
		}
		// TODO: Check all markets here?
		if tunnel.CoinLocked(assets.funding.ID, coinID) {
			// An identical request may have been accepted since the initial lookup.
			if handled, rpcErr := tunnel.HandleOrderResubmission(context.Background(), oRecord, completion); handled {
				return rpcErr
			}
			return msgjson.NewError(msgjson.FundingError, "coin %s is locked", fmtCoinID(assets.funding.ID, coinID))
		}
		coinStrs = append(coinStrs, coinStr)
	}

	// Use this as a chance to check user's existing market orders.
	// TODO: check all markets?
	for mktName, tunnel := range r.tunnels {
		unbookedUnfunded := tunnel.CheckUnfilled(assets.funding.ID, oRecord.order.User())
		for _, badLo := range unbookedUnfunded {
			log.Infof("Unbooked unfunded order %v from market %s for user %v", badLo, mktName, oRecord.order.User())
		}
	}

	lotSize := tunnel.LotSize()

	midGap := tunnel.MidGap()
	if midGap == 0 {
		midGap = tunnel.RateStep()
	}

	lots := trade.Quantity / lotSize
	if !sell && rate == 0 {
		lots = matcher.QuoteToBase(midGap, trade.Quantity) / lotSize
	}

	var valSum uint64
	var spendSize uint32
	neededCoins := make(map[int]*msgjson.Coin, len(trade.Coins))
	for i, coin := range coins {
		neededCoins[i] = coin
	}

	checkCoins := func() (tryAgain bool, msgErr *msgjson.Error) {
		for key, coin := range neededCoins {
			// Get the coin from the backend and validate it.
			dexCoin, err := fundingCoin(fundingAsset.Backend, coin.ID, coin.Redeem)
			if err != nil {
				if errors.Is(err, asset.CoinNotFoundError) {
					return true, nil
				}
				if errors.Is(err, asset.ErrRequestTimeout) {
					log.Errorf("Deadline exceeded attempting to verify funding coin %v (%s). Will try again.",
						coin.ID, fundingAsset.Symbol)
					return true, nil
				}
				log.Errorf("Error retrieving limit order funding coin ID %s. user = %s: %v", coin.ID, user, err)
				return false, msgjson.NewError(msgjson.FundingError, "error retrieving coin ID %v", coin.ID)
			}

			// Verify that the user controls the funding coins.
			err = dexCoin.Auth(msgBytesToBytes(coin.PubKeys), msgBytesToBytes(coin.Sigs), coin.ID)
			if err != nil {
				log.Debugf("Auth error for %s coin %s: %v", fundingAsset.Symbol, dexCoin, err)
				return false, msgjson.NewError(msgjson.CoinAuthError, "failed to authorize coin %v", dexCoin)
			}

			msgErr := r.checkZeroConfs(dexCoin, fundingAsset)
			if msgErr != nil {
				return false, msgErr
			}

			delete(neededCoins, key) // don't check this coin again
			valSum += dexCoin.Coin().Value()
			// NOTE: Summing like this is actually not quite sufficient to
			// estimate the size associated with the input, because if it's a
			// BTC segwit output, we would also have to account for the marker
			// and flag weight, but only once per tx. The weight would add
			// either 0 or 1 byte to the tx virtual size, so we have a chance of
			// under-estimating by 1 byte to the advantage of the client. It
			// won't ever cause issues though, because we also require funding
			// for a change output in the final swap, which is actually not
			// needed, so there's some buffer.
			spendSize += dexCoin.SpendSize()
		}

		if valSum == 0 {
			return false, msgjson.NewError(msgjson.FundingError, "zero value funding coins not permitted")
		}

		// Calculate the fees and check that the utxo sum is enough.
		var swapVal uint64
		if sell {
			swapVal = trade.Quantity
		} else {
			if rate > 0 { // limit buy
				swapVal = calc.BaseToQuote(rate, trade.Quantity)
			} else {
				// This is a market buy order, so the quantity gets special handling.
				// 1. The quantity is in units of the quote asset.
				// 2. The quantity has to satisfy the market buy buffer.
				midGap := tunnel.MidGap()
				if midGap == 0 {
					midGap = tunnel.RateStep()
				}
				buyBuffer := tunnel.MarketBuyBuffer()
				lotWithBuffer := uint64(float64(lotSize) * buyBuffer)
				bufferQty := matcher.BaseToQuote(midGap, lotWithBuffer)
				if trade.Quantity < bufferQty {
					return false, msgjson.NewError(msgjson.FundingError, "order quantity does not satisfy market buy buffer. %d < %d. midGap = %d",
						trade.Quantity, bufferQty, midGap)
				}
				swapVal = trade.Quantity
			}
		}

		if !funder.ValidateOrderFunding(swapVal, valSum, uint64(len(trade.Coins)), uint64(spendSize), lots, &assets.funding.Asset) {
			return false, msgjson.NewError(msgjson.FundingError, "failed funding validation")
		}

		return false, nil
	}

	respondError := func(msgErr *msgjson.Error) {
		log.Debugf("Error going to user %v: %s", user, msgErr)
		if err := completion.Fail(context.Background(), msgErr); err != nil {
			log.Infof("Failed to send order error response (msg = %s) to disconnected user %v: %q",
				msgErr, user, err)
		}
	}

	log.Tracef("Searching for %s coins %v for new order", fundingAsset.Symbol, coinStrs)
	// Funding checks may continue after this function returns. The completion
	// delivers the response when the order is accepted or the checks fail.
	r.latencyQ.Wait(&wait.Waiter{
		Expiration: time.Now().Add(fundingTxWait),
		TryFunc: func() wait.TryDirective {
			tryAgain, msgErr := checkCoins()
			if tryAgain {
				return wait.TryAgain
			}
			if msgErr != nil {
				respondError(msgErr)
				return wait.DontTryAgain
			}

			// Submit the order for acceptance, where it will be time stamped.
			log.Tracef("Found and validated %s coins %v for new order", fundingAsset.Symbol, coinStrs)
			if msgErr := tunnel.AcceptOrderCommand(context.Background(), oRecord, completion); msgErr != nil {
				respondError(msgErr)
			}
			return wait.DontTryAgain
		},
		ExpireFunc: func() {
			// Tell them to broadcast again or check their node before broadcast
			// timeout is reached and the match is revoked.
			respondError(msgjson.NewError(msgjson.TransactionUndiscovered,
				"failed to find funding coins %v", coinStrs))
		},
	})

	return nil
}

// sufficientAccountBalance checks that the user's account-based asset balance
// is sufficient to support the order, considering the user's other orders and
// active matches across all DEX markets.
func (r *OrderRouter) sufficientAccountBalance(accountAddr string, ord order.Order,
	assetID, redeemAssetID uint32, tunnel MarketTunnel) bool {
	trade := ord.Trade()

	// This asset is funding an order when it is either:
	//  - base asset in a sell order e.g. selling ETH in a ETH-LTC market
	//  - quote asset in a buy order e.g. buying BTC in a BTC-ETH market
	// This asset will be redeemed when it is either:
	//  - base asset in a buy order e.g. buying ETH in a ETH-LTC market
	//  - quote asset in a sell order e.g. selling in a BTC-ETH market

	var fundingQty, fundingLots uint64 // when the asset is base in sell order, or quote in buy order
	var redeems int                    // when the asset is base in buy order, or quote in sell order
	if ord.Base() == assetID {
		if trade.Sell {
			fundingQty = trade.Quantity
			fundingLots = trade.Quantity / tunnel.LotSize()
		} else { // buying base asset
			baseQty := trade.Quantity
			if _, ok := ord.(*order.MarketOrder); ok {
				// Market buy Quantity is in units of quote asset, so estimate
				// how much of base asset that might be based on mid-gap rate.
				baseQty = calc.QuoteToBase(safeMidGap(tunnel), trade.Quantity)
			}
			redeems = int(baseQty / tunnel.LotSize())
		}
	} else {
		if trade.Sell {
			redeems = int(trade.Quantity / tunnel.LotSize())
		} else {
			if lo, ok := ord.(*order.LimitOrder); ok {
				fundingQty = calc.BaseToQuote(lo.Rate, trade.Quantity)
				fundingLots = trade.Quantity / tunnel.LotSize()
			} else { // market buy
				fundingQty = trade.Quantity
				fundingLots = fundingQty / tunnel.LotSize()
			}
		}
	}

	return r.dexBalancer.CheckBalance(accountAddr, assetID, redeemAssetID, fundingQty, fundingLots, redeems)
}

// calcParcelLimit computes the users score-scaled user parcel limit.
func calcParcelLimit(tier int64, score, maxScore int32) uint32 {
	// Users limit starts at 2 parcels per tier.
	lowerLimit := tier * dex.PerTierBaseParcelLimit
	// Limit can scale up to 3x with score.
	upperLimit := lowerLimit * dex.ParcelLimitScoreMultiplier
	limitRange := upperLimit - lowerLimit
	var scaleFactor float64
	if score > 0 {
		scaleFactor = float64(score) / float64(maxScore)
	}
	return uint32(lowerLimit) + uint32(math.Round(scaleFactor*float64(limitRange)))
}

// CheckParcelLimit checks that the user does not exceed their parcel limit.
// The calcParcels function must be provided by the order's targeted Market, and
// calculate the number of parcels from that market when quantity from settling
// matches is taken into consideration. CheckParcelLimit checks the global
// parcel limit, based on the users tier and score and active orders for ALL
// markets. The tier is evaluated at asOf (the order's server time). A returned
// error is a reputation-load failure, not a limit verdict.
func (r *OrderRouter) CheckParcelLimit(user account.AccountID, targetMarketName string, asOf time.Time, calcParcels MarketParcelCalculator) (bool, error) {
	tier, score, maxScore, err := r.auth.UserReputationAt(user, asOf)
	if err != nil {
		return false, fmt.Errorf("loading reputation for parcel limit check: %w", err)
	}
	if tier <= 0 {
		return false, nil
	}

	roundParcels := func(parcels float64) uint32 {
		// Rounding to 8 decimal places first should resolve any floating point
		// error, then we take the floor. 1e8 is not completetly arbitrary. We
		// need to choose a number of decimals of an order > the expected parcel
		// size of a low-lot-size market, which I expect wouldn't be greater
		// than 1e5.
		return uint32(math.Round(parcels*1e8) / 1e8)
	}

	parcelLimit := calcParcelLimit(tier, score, maxScore)

	settlingQuantities := make(map[string]uint64)
	for bq, qty := range r.swapper.UnsettledQuantity(user) {
		mktName, _ := dex.MarketName(bq[0], bq[1])
		settlingQuantities[mktName] += qty
	}

	// Accumulate in sorted market order: float addition is not associative,
	// and this verdict re-runs on every node, so map-iteration order must
	// not be able to flip a boundary case between nodes.
	mktNames := make([]string, 0, len(r.tunnels))
	for mktName := range r.tunnels {
		mktNames = append(mktNames, mktName)
	}
	sort.Strings(mktNames)

	var otherMarketParcels float64
	var settlingQty uint64
	for _, mktName := range mktNames {
		if mktName == targetMarketName {
			settlingQty = settlingQuantities[mktName]
			continue
		}

		otherMarketParcels += r.tunnels[mktName].Parcels(user, settlingQuantities[mktName])
		if roundParcels(otherMarketParcels) > parcelLimit {
			return false, nil
		}
	}
	targetMarketParcels := calcParcels(settlingQty)

	return roundParcels(otherMarketParcels+targetMarketParcels) <= parcelLimit, nil
}

// Check the FundingCoin confirmations, and if zero, ensure the tx fee rate
// is sufficient, > 90% of our last recorded estimate for the asset.
func (r *OrderRouter) checkZeroConfs(dexCoin asset.FundingCoin, fundingAsset *asset.BackedAsset) *msgjson.Error {
	// Verify that zero-conf coins are within 10% of the last known fee
	// rate.
	confs, err := coinConfirmations(dexCoin.Coin())
	if err != nil {
		log.Debugf("Confirmations error for %s coin %s: %v", fundingAsset.Symbol, dexCoin, err)
		return msgjson.NewError(msgjson.FundingError, "failed to verify coin %v", dexCoin)
	}
	if confs > 0 {
		return nil
	}
	lastKnownFeeRate := r.feeSource.LastRate(fundingAsset.ID) // MaxFeeRate applied inside feeSource
	feeMinimum := uint64(math.Round(float64(lastKnownFeeRate) * ZeroConfFeeRateThreshold))

	if !fundingAsset.Backend.ValidateFeeRate(dexCoin.Coin(), feeMinimum) {
		log.Debugf("Fees too low %s coin %s: fee mim %d", fundingAsset.Symbol, dexCoin, feeMinimum)
		return msgjson.NewError(msgjson.FundingError,
			"fee rate for %s is too low. fee min %d", dexCoin, feeMinimum)
	}
	return nil
}

// handleCancel submits a cancel order command.
func (r *OrderRouter) handleCancel(user account.AccountID, msg *msgjson.Message) *msgjson.Error {
	return r.mesh.ExecuteCommand(context.Background(), mesh.CommandRequest{
		Kind: commandKindCancel,
		User: user,
		Msg:  msg,
		Respond: func(resp *msgjson.Message) error {
			return r.auth.Send(user, resp)
		},
	})
}

// executeCancel validates a cancel order request and submits the order for acceptance.
func (r *OrderRouter) executeCancel(cmdCtx *mesh.CommandContext) *msgjson.Error {
	user := cmdCtx.Request.User
	msg := cmdCtx.Request.Msg

	cancel := new(msgjson.CancelOrder)
	err := msg.Unmarshal(&cancel)
	if err != nil || cancel == nil {
		return msgjson.NewError(msgjson.RPCParseError, "error decoding 'cancel' payload")
	}

	rpcErr := r.verifyAccount(user, cancel.AccountID, cancel)
	if rpcErr != nil {
		return rpcErr
	}

	// NOTE: Allow suspended accounts to submit cancel orders.

	tunnel, rpcErr := r.extractMarket(&cancel.Prefix)
	if rpcErr != nil {
		return rpcErr
	}

	if len(cancel.TargetID) != order.OrderIDSize {
		return msgjson.NewError(msgjson.OrderParameterError, "invalid target ID format")
	}
	var targetID order.OrderID
	copy(targetID[:], cancel.TargetID)

	// Check that OrderType is set correctly
	if cancel.OrderType != msgjson.CancelOrderNum {
		return msgjson.NewError(msgjson.OrderParameterError, "wrong order type set for cancel order")
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
			ClientTime: time.UnixMilli(int64(cancel.ClientTime)),
			// ServerTime is set by command acceptance.
			Commit: commit,
		},
		TargetOrderID: targetID,
	}

	// Submit the order for acceptance.
	oRecord := &orderRecord{
		order: co,
		req:   cancel,
		msgID: msg.ID,
	}

	if handled, rpcErr := tunnel.HandleOrderResubmission(cmdCtx, oRecord, cmdCtx.Completion); handled {
		return rpcErr
	}

	rpcErr = checkTimes(&cancel.Prefix)
	if rpcErr != nil {
		return rpcErr
	}

	if !tunnel.Cancelable(targetID) {
		return msgjson.NewError(msgjson.UnknownOrderError, "target order not known: %v", targetID)
	}

	return tunnel.AcceptOrderCommand(cmdCtx, oRecord, cmdCtx.Completion)
}

// verifyAccount checks that the submitted order squares with the submitting user.
func (r *OrderRouter) verifyAccount(user account.AccountID, msgAcct msgjson.Bytes, signable msgjson.Signable) *msgjson.Error {
	// Verify account ID matches.
	if !bytes.Equal(user[:], msgAcct) {
		return msgjson.NewError(msgjson.OrderParameterError, "account ID mismatch")
	}
	// Check the clients signature of the order.
	sigMsg := signable.Serialize()
	err := r.auth.VerifyUserSig(user, sigMsg, signable.SigBytes())
	if err != nil {
		return msgjson.NewError(msgjson.SignatureError, "signature error: %v", err.Error())
	}
	return nil
}

// extractMarket finds the MarketTunnel for the provided prefix.
func (r *OrderRouter) extractMarket(prefix *msgjson.Prefix) (MarketTunnel, *msgjson.Error) {
	mktName, err := dex.MarketName(prefix.Base, prefix.Quote)
	if err != nil {
		return nil, msgjson.NewError(msgjson.UnknownMarketError, "asset lookup error: %v", err.Error())
	}
	tunnel, found := r.tunnels[mktName]
	if !found {
		return nil, msgjson.NewError(msgjson.UnknownMarketError, "unknown market %s", mktName)
	}
	return tunnel, nil
}

// SuspendEpoch holds the index and end time of final epoch marking the
// suspension of a market.
type SuspendEpoch struct {
	Idx int64
	End time.Time
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
			"invalid side value %d", trade.Side)
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
	offset := time.Now().UnixMilli() - int64(prefix.ClientTime)
	if offset < 0 {
		offset *= -1
	}
	if offset >= maxClockOffset {
		return msgjson.NewError(msgjson.ClockRangeError,
			"clock offset of %d ms is larger than maximum allowed, %d ms",
			offset, maxClockOffset,
		)
	}
	// Server time should be unset.
	if prefix.ServerTime != 0 {
		return msgjson.NewError(msgjson.OrderParameterError, "non-zero server time not allowed")
	}
	return nil
}

// checkPrefixTrade validates the information in the prefix and trade portions
// of an order.
func (r *OrderRouter) checkPrefixTrade(assets *assetSet, lotSize uint64, prefix *msgjson.Prefix,
	trade *msgjson.Trade, checkLot bool) *msgjson.Error {
	// Check that the client's timestamp is still valid.
	rpcErr := checkTimes(prefix)
	if rpcErr != nil {
		return rpcErr
	}
	// Check that the address is valid.
	if !assets.receiving.Backend.CheckSwapAddress(trade.Address) {
		return msgjson.NewError(msgjson.OrderParameterError, "address doesn't check")
	}
	// Quantity cannot be zero, and must be an integral multiple of the lot size.
	if trade.Quantity == 0 {
		return msgjson.NewError(msgjson.OrderParameterError, "zero quantity not allowed")
	}
	if checkLot && trade.Quantity%lotSize != 0 {
		return msgjson.NewError(msgjson.OrderParameterError, "order quantity not a multiple of lot size")
	}
	// Validate UTXOs
	// Check that all required arrays are of equal length.
	if len(trade.Coins) == 0 {
		return msgjson.NewError(msgjson.FundingError, "order must specify utxos")
	}

	for i, coin := range trade.Coins {
		sigCount := len(coin.Sigs)
		if sigCount == 0 {
			return msgjson.NewError(msgjson.SignatureError, "no signature for coin %d", i)
		}
		if len(coin.PubKeys) != sigCount {
			return msgjson.NewError(msgjson.OrderParameterError,
				"pubkey count %d not equal to signature count %d for coin %d",
				len(coin.PubKeys), sigCount, i,
			)
		}
	}

	return nil
}

// msgBytesToBytes converts a []msgjson.Byte to a [][]byte.
func msgBytesToBytes(msgBs []msgjson.Bytes) [][]byte {
	b := make([][]byte, 0, len(msgBs))
	for _, msgB := range msgBs {
		b = append(b, msgB)
	}
	return b
}

// fmtCoinID formats the coin ID by asset. If an error is encountered, the
// coinID string returned hex-encoded and prepended with "unparsed:".
func fmtCoinID(assetID uint32, coinID []byte) string {
	strID, err := asset.DecodeCoinID(assetID, coinID)
	if err != nil {
		return "unparsed:" + hex.EncodeToString(coinID)
	}
	return strID
}

// fmtCoinIDs is like fmtCoinID but for a slice of CoinIDs, printing with
// default Go slice formatting like "[coin1 coin2 ...]".
func fmtCoinIDs(assetID uint32, coinIDs []order.CoinID) string {
	out := make([]string, len(coinIDs))
	for i := range coinIDs {
		out[i] = fmtCoinID(assetID, coinIDs[i])
	}
	return fmt.Sprint(out)
}

func safeMidGap(tunnel MarketTunnel) uint64 {
	midGap := tunnel.MidGap()
	if midGap == 0 {
		return tunnel.RateStep()
	}
	return midGap
}
