// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package core

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/csv"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/comms"
	"decred.org/dcrdex/client/db"
	"decred.org/dcrdex/client/db/bolt"
	"decred.org/dcrdex/client/orderbook"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/dex/config"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/encrypt"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/dex/wait"
	"decred.org/dcrdex/server/account"
	serverdex "decred.org/dcrdex/server/dex"
	"github.com/decred/dcrd/crypto/blake256"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
	"github.com/decred/go-socks/socks"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

const (
	keyParamsKey = "keyParams"

	// tickCheckDivisions is how many times to tick trades per broadcast timeout
	// interval. e.g. 12 min btimeout / 8 divisions = 90 sec between checks.
	tickCheckDivisions = 8
	// defaultTickInterval is the tick interval used before the broadcast
	// timeout is known (e.g. startup with down server).
	defaultTickInterval = 30 * time.Second

	marketBuyRedemptionSlippageBuffer = 2
)

var (
	unbip = dex.BipIDSymbol
	// The coin waiters will query for transaction data every recheckInterval.
	recheckInterval = time.Second * 5
	// When waiting for a wallet to sync, a SyncStatus check will be performed
	// every syncTickerPeriod. var instead of const for testing purposes.
	syncTickerPeriod = 3 * time.Second
	// serverAPIVers are the DEX server API versions this client is capable
	// of communicating with.
	//
	// NOTE: API version may change at any time. Keep this in mind when
	// updating the API. Long-running operations may start and end with
	// differing versions.
	serverAPIVers = []int{serverdex.PreAPIVersion}
	// ActiveOrdersLogoutErr is returned from logout when there are active
	// orders.
	ActiveOrdersLogoutErr = errors.New("cannot log out with active orders")

	errTimeout = errors.New("timeout")
)

type dexTicker struct {
	dur int64 // atomic
	*time.Ticker
}

func newDexTicker(dur time.Duration) *dexTicker {
	return &dexTicker{
		dur:    int64(dur),
		Ticker: time.NewTicker(dur),
	}
}

func (dt *dexTicker) Reset(dur time.Duration) {
	atomic.StoreInt64(&dt.dur, int64(dur))
	dt.Ticker.Reset(dur)
}

func (dt *dexTicker) Dur() time.Duration {
	return time.Duration(atomic.LoadInt64(&dt.dur))
}

type pendingFeeState struct {
	confs uint32
	asset uint32
}

// dexConnection is the websocket connection and the DEX configuration.
type dexConnection struct {
	comms.WsConn
	connMaster *dex.ConnectionMaster
	log        dex.Logger
	acct       *dexAccount
	notify     func(Notification)
	ticker     *dexTicker
	// apiVer is an atomic. An uninitiated connection should be set to -1.
	apiVer int32

	assetsMtx sync.RWMutex
	assets    map[uint32]*dex.Asset

	cfgMtx sync.RWMutex
	cfg    *msgjson.ConfigResult

	booksMtx sync.RWMutex
	books    map[string]*bookie

	// tradeMtx is used to synchronize access to the trades map.
	tradeMtx sync.RWMutex
	// trades tracks outstanding orders issued by this client.
	trades map[order.OrderID]*trackedTrade

	epochMtx sync.RWMutex
	epoch    map[string]uint64

	// connectionStatus is a best guess on the ws connection status.
	connectionStatus uint32

	pendingFeeMtx sync.RWMutex
	pendingFee    *pendingFeeState

	reportingConnects uint32

	spotsMtx sync.RWMutex
	spots    map[string]*msgjson.Spot
}

// DefaultResponseTimeout is the default timeout for responses after a request is
// successfully sent.
const (
	DefaultResponseTimeout = comms.DefaultResponseTimeout
	fundingTxWait          = time.Minute // TODO: share var with server/market or put in config
)

// running returns the status of the provided market.
func (dc *dexConnection) running(mkt string) bool {
	dc.cfgMtx.RLock()
	defer dc.cfgMtx.RUnlock()
	mktCfg := dc.findMarketConfig(mkt)
	if mktCfg == nil {
		return false // not found means not running
	}
	return mktCfg.Running()
}

// status returns the status of the connection to the dex.
func (dc *dexConnection) status() comms.ConnectionStatus {
	return comms.ConnectionStatus(atomic.LoadUint32(&dc.connectionStatus))
}

func (dc *dexConnection) feeAsset(assetID uint32) *msgjson.FeeAsset {
	dc.cfgMtx.RLock()
	defer dc.cfgMtx.RUnlock()
	if dc.cfg == nil {
		return nil
	}
	symb := unbip(assetID)
	return dc.cfg.RegFees[symb]
}

// marketConfig is the market's configuration, as returned by the server in the
// 'config' response.
func (dc *dexConnection) marketConfig(mktID string) *msgjson.Market {
	dc.cfgMtx.RLock()
	defer dc.cfgMtx.RUnlock()
	return dc.findMarketConfig(mktID)
}

// marketMap creates a map of this DEX's *Market keyed by name/ID,
// [base]_[quote].
func (dc *dexConnection) marketMap() map[string]*Market {
	dc.cfgMtx.RLock()
	cfg := dc.cfg
	dc.cfgMtx.RUnlock()
	if cfg == nil {
		return nil
	}
	mktConfigs := cfg.Markets

	marketMap := make(map[string]*Market, len(mktConfigs))
	for _, mkt := range mktConfigs {
		// The presence of the asset for every market was already verified when the
		// dexConnection was created in connectDEX.
		dc.assetsMtx.RLock()
		base, quote := dc.assets[mkt.Base], dc.assets[mkt.Quote]
		dc.assetsMtx.RUnlock()
		mkt := &Market{
			Name:            mkt.Name,
			BaseID:          base.ID,
			BaseSymbol:      base.Symbol,
			QuoteID:         quote.ID,
			QuoteSymbol:     quote.Symbol,
			LotSize:         mkt.LotSize,
			RateStep:        mkt.RateStep,
			EpochLen:        mkt.EpochLen,
			StartEpoch:      mkt.StartEpoch,
			MarketBuyBuffer: mkt.MarketBuyBuffer,
		}
		mktID := mkt.marketName()

		for _, trade := range dc.marketTrades(mktID) {
			mkt.Orders = append(mkt.Orders, trade.coreOrder())
		}

		marketMap[mktID] = mkt
	}

	// Populate spots.
	dc.spotsMtx.RLock()
	for mktID, mkt := range marketMap {
		mkt.SpotPrice = dc.spots[mktID]
	}
	dc.spotsMtx.RUnlock()

	return marketMap
}

func (dc *dexConnection) trackedTrades() []*trackedTrade {
	dc.tradeMtx.RLock()
	defer dc.tradeMtx.RUnlock()
	allTrades := make([]*trackedTrade, 0, len(dc.trades))
	for _, trade := range dc.trades {
		allTrades = append(allTrades, trade)
	}
	return allTrades
}

// marketTrades is a slice of active trades in the trades map.
func (dc *dexConnection) marketTrades(mktID string) []*trackedTrade {
	// Copy trades to avoid locking both tradeMtx and trackedTrade.mtx.
	allTrades := dc.trackedTrades()
	trades := make([]*trackedTrade, 0, len(allTrades)) // may over-allocate
	for _, trade := range allTrades {
		if trade.mktID == mktID && trade.isActive() {
			trades = append(trades, trade)
		}
		// Retiring inactive orders is presently the responsibility of ticker.
	}
	return trades
}

func (dc *dexConnection) setPendingFee(asset, confs uint32) {
	dc.pendingFeeMtx.Lock()
	dc.pendingFee = &pendingFeeState{asset: asset, confs: confs}
	dc.pendingFeeMtx.Unlock()
}

func (dc *dexConnection) clearPendingFee() {
	dc.pendingFeeMtx.Lock()
	dc.pendingFee = nil
	dc.pendingFeeMtx.Unlock()
}

// getPendingFee returns the PendingFeeState for the dex registration or nil if
// no registration fee is pending.
func (dc *dexConnection) getPendingFee() *PendingFeeState {
	dc.pendingFeeMtx.RLock()
	defer dc.pendingFeeMtx.RUnlock()
	pf := dc.pendingFee
	if pf == nil {
		return nil
	}
	return &PendingFeeState{
		AssetID: pf.asset,
		Symbol:  unbip(pf.asset),
		Confs:   pf.confs,
	}
}

func (dc *dexConnection) exchangeInfo() *Exchange {
	// Set AcctID to empty string if not registered.
	acctID := dc.acct.ID().String()
	var emptyAcctID account.AccountID
	if dc.acct.ID() == emptyAcctID {
		acctID = ""
	}

	dc.cfgMtx.RLock()
	cfg := dc.cfg
	dc.cfgMtx.RUnlock()
	if cfg == nil { // no config, assets, or markets data
		return &Exchange{
			Host:             dc.acct.host,
			AcctID:           acctID,
			ConnectionStatus: dc.status(),
			PendingFee:       dc.getPendingFee(),
		}
	}

	dc.assetsMtx.RLock()
	assets := make(map[uint32]*dex.Asset, len(dc.assets))
	for assetID, dexAsset := range dc.assets {
		assets[assetID] = dexAsset
	}
	dc.assetsMtx.RUnlock()

	feeAssets := make(map[string]*FeeAsset, len(cfg.RegFees))
	for symb, asset := range cfg.RegFees {
		feeAssets[symb] = &FeeAsset{
			ID:    asset.ID,
			Confs: asset.Confs,
			Amt:   asset.Amt,
		}
	}
	dcrAsset := feeAssets["dcr"]
	if dcrAsset == nil { // should have happened in refreshServerConfig
		dcrAsset = &FeeAsset{
			ID:    42,
			Amt:   cfg.Fee,
			Confs: uint32(cfg.RegFeeConfirms),
		}
		feeAssets["dcr"] = dcrAsset
	}

	return &Exchange{
		Host:             dc.acct.host,
		AcctID:           acctID,
		Markets:          dc.marketMap(),
		Assets:           assets,
		ConnectionStatus: dc.status(),
		Fee:              dcrAsset,
		RegFees:          feeAssets,
		PendingFee:       dc.getPendingFee(),
		CandleDurs:       cfg.BinSizes,
	}
}

// assetFamily prepares a map of asset IDs for asset that share a parent asset
// with the specified assetID. The assetID and the parent asset's ID both have
// entries, as well as any tokens.
func assetFamily(assetID uint32) map[uint32]bool {
	assetFamily := make(map[uint32]bool, 1)
	var parentAsset *asset.RegisteredAsset
	if parentAsset = asset.Asset(assetID); parentAsset == nil {
		if tkn := asset.TokenInfo(assetID); tkn != nil {
			parentAsset = asset.Asset(tkn.ParentID)
		}
	}
	if parentAsset != nil {
		assetFamily[parentAsset.ID] = true
		for tokenID := range parentAsset.Tokens {
			assetFamily[tokenID] = true
		}
	}
	return assetFamily
}

// hasActiveAssetOrders checks whether there are any active orders or negotiating
// matches for the specified asset.
func (dc *dexConnection) hasActiveAssetOrders(assetID uint32) bool {
	dc.tradeMtx.RLock()
	defer dc.tradeMtx.RUnlock()
	familial := assetFamily(assetID)
	for _, trade := range dc.trades {
		if (familial[trade.Base()] || familial[trade.Quote()]) &&
			trade.isActive() {
			return true
		}

	}
	return false
}

// hasActiveOrders checks whether there are any active orders for the dexConnection.
func (dc *dexConnection) hasActiveOrders() bool {
	dc.tradeMtx.RLock()
	defer dc.tradeMtx.RUnlock()

	for _, trade := range dc.trades {
		if trade.isActive() {
			return true
		}
	}
	return false
}

// findOrder returns the tracker and preimage for an order ID, and a boolean
// indicating whether this is a cancel order.
func (dc *dexConnection) findOrder(oid order.OrderID) (tracker *trackedTrade, preImg order.Preimage, isCancel bool) {
	dc.tradeMtx.RLock()
	defer dc.tradeMtx.RUnlock()
	// Try to find the order as a trade.
	if tracker, found := dc.trades[oid]; found {
		return tracker, tracker.preImg, false
	}
	// Search the cancel order IDs.
	for _, tracker := range dc.trades {
		if tracker.cancel != nil && tracker.cancel.ID() == oid {
			return tracker, tracker.cancel.preImg, true
		}
	}
	return
}

// tryCancel will look for an order with the specified order ID, and attempt to
// cancel the order. It is not an error if the order is not found.
func (c *Core) tryCancel(dc *dexConnection, oid order.OrderID) (found bool, err error) {
	tracker, _, _ := dc.findOrder(oid)
	if tracker == nil {
		return // false, nil
	}
	return true, c.tryCancelTrade(dc, tracker)
}

// tryCancelTrade attempts to cancel the order.
func (c *Core) tryCancelTrade(dc *dexConnection, tracker *trackedTrade) error {
	oid := tracker.ID()
	if lo, ok := tracker.Order.(*order.LimitOrder); !ok || lo.Force != order.StandingTiF {
		return fmt.Errorf("cannot cancel %s order %s that is not a standing limit order", tracker.Type(), oid)
	}

	tracker.mtx.Lock()
	defer tracker.mtx.Unlock()

	if status := tracker.metaData.Status; status != order.OrderStatusEpoch && status != order.OrderStatusBooked {
		return fmt.Errorf("order %v not cancellable in status %v", oid, status)
	}

	if tracker.cancel != nil {
		// Existing cancel might be stale. Deleting it now allows this
		// cancel attempt to proceed.
		tracker.deleteStaleCancelOrder()

		if tracker.cancel != nil {
			return fmt.Errorf("order %s - only one cancel order can be submitted per order per epoch. "+
				"still waiting on cancel order %s to match", oid, tracker.cancel.ID())
		}
	}

	// Construct the order.
	prefix := tracker.Prefix()
	preImg := newPreimage()
	co := &order.CancelOrder{
		P: order.Prefix{
			AccountID:  prefix.AccountID,
			BaseAsset:  prefix.BaseAsset,
			QuoteAsset: prefix.QuoteAsset,
			OrderType:  order.CancelOrderType,
			ClientTime: time.Now(),
			Commit:     preImg.Commit(),
		},
		TargetOrderID: oid,
	}
	err := order.ValidateOrder(co, order.OrderStatusEpoch, 0)
	if err != nil {
		return err
	}

	commitSig := make(chan struct{})
	defer close(commitSig) // signals on both success and failure, unlike syncOrderPlaced/piSyncers
	c.sentCommitsMtx.Lock()
	c.sentCommits[co.Commit] = commitSig
	c.sentCommitsMtx.Unlock()

	// Create and send the order message. Check the response before using it.
	route, msgOrder, _ := messageOrder(co, nil)
	var result = new(msgjson.OrderResult)
	err = dc.signAndRequest(msgOrder, route, result, DefaultResponseTimeout)
	if err != nil {
		// At this point there is a possibility that the server got the request
		// and created the cancel order, but we lost the connection before
		// receiving the response with the cancel's order ID. Any preimage
		// request will be unrecognized. This order is ABANDONED.
		return fmt.Errorf("failed to submit cancel order targeting trade %v: %w", oid, err)
	}
	err = validateOrderResponse(dc, result, co, msgOrder)
	if err != nil {
		return fmt.Errorf("Abandoning order. preimage: %x, server time: %d: %w",
			preImg[:], result.ServerTime, err)
	}

	// Store the cancel order with the tracker.
	err = tracker.cancelTrade(co, preImg)
	if err != nil {
		return fmt.Errorf("error storing cancel order info %s: %w", co.ID(), err)
	}

	// Now that the trackedTrade is updated, sync with the preimage request.
	c.syncOrderPlaced(co.ID())

	// Store the cancel order.
	err = c.db.UpdateOrder(&db.MetaOrder{
		MetaData: &db.OrderMetaData{
			Status: order.OrderStatusEpoch,
			Host:   dc.acct.host,
			Proof: db.OrderProof{
				DEXSig:   result.Sig,
				Preimage: preImg[:],
			},
			LinkedOrder: oid,
		},
		Order: co,
	})
	if err != nil {
		return fmt.Errorf("failed to store order in database: %w", err)
	}

	c.log.Infof("Cancel order %s targeting order %s at %s has been placed",
		co.ID(), oid, dc.acct.host)

	subject, details := c.formatDetails(TopicCancellingOrder, tracker.token())
	c.notify(newOrderNote(TopicCancellingOrder, subject, details, db.Poke, tracker.coreOrderInternal()))

	return nil
}

// Synchronize with the preimage request, in case that request came before
// we had an order ID and added this order to the trades map or cancel field.
func (c *Core) syncOrderPlaced(oid order.OrderID) {
	c.piSyncMtx.Lock()
	syncChan, found := c.piSyncers[oid]
	if found {
		// If we found the channel, the preimage request is already waiting.
		// Close the channel to allow that request to proceed.
		close(syncChan)
		delete(c.piSyncers, oid)
	} else {
		// If there is no channel, the preimage request hasn't come yet. Add a
		// channel to signal readiness. Could insert nil unless syncOrderPlaced
		// is erroneously called twice for the same order ID.
		c.piSyncers[oid] = make(chan struct{})
	}
	c.piSyncMtx.Unlock()
}

// signAndRequest signs and sends the request, unmarshaling the response into
// the provided interface.
func (dc *dexConnection) signAndRequest(signable msgjson.Signable, route string, result interface{}, timeout time.Duration) error {
	if dc.acct.locked() {
		return fmt.Errorf("cannot sign: %s account locked", dc.acct.host)
	}
	sign(dc.acct.privKey, signable)
	return sendRequest(dc.WsConn, route, signable, result, timeout)
}

// ack sends an Acknowledgement for a match-related request.
func (dc *dexConnection) ack(msgID uint64, matchID order.MatchID, signable msgjson.Signable) (err error) {
	ack := &msgjson.Acknowledgement{
		MatchID: matchID[:],
	}
	sigMsg := signable.Serialize()
	ack.Sig, err = dc.acct.sign(sigMsg)
	if err != nil {
		return fmt.Errorf("sign error - %w", err)
	}
	msg, err := msgjson.NewResponse(msgID, ack, nil)
	if err != nil {
		return fmt.Errorf("NewResponse error - %w", err)
	}
	err = dc.Send(msg)
	if err != nil {
		return fmt.Errorf("Send error - %w", err)
	}
	return nil
}

// serverMatches are an intermediate structure used by the dexConnection to
// sort incoming match notifications.
type serverMatches struct {
	tracker    *trackedTrade
	msgMatches []*msgjson.Match
	cancel     *msgjson.Match
}

// parseMatches sorts the list of matches and associates them with a trade. This
// may be called from handleMatchRoute on receipt of a new 'match' request, or
// by authDEX with the list of active matches returned by the 'connect' request.
func (dc *dexConnection) parseMatches(msgMatches []*msgjson.Match, checkSigs bool) (map[order.OrderID]*serverMatches, []msgjson.Acknowledgement, error) {
	var acks []msgjson.Acknowledgement
	matches := make(map[order.OrderID]*serverMatches)
	var errs []string
	for _, msgMatch := range msgMatches {
		var oid order.OrderID
		copy(oid[:], msgMatch.OrderID)
		tracker, _, isCancel := dc.findOrder(oid)
		if tracker == nil {
			errs = append(errs, "order "+oid.String()+" not found")
			continue
		}

		// Check the fee rate against the maxfeerate recorded at order time.
		swapRate := msgMatch.FeeRateQuote
		if tracker.Trade().Sell {
			swapRate = msgMatch.FeeRateBase
		}
		if !isCancel && swapRate > tracker.metaData.MaxFeeRate {
			errs = append(errs, fmt.Sprintf("rejecting match %s for order %s because assigned rate (%d) is > MaxFeeRate (%d)",
				msgMatch.MatchID, msgMatch.OrderID, swapRate, tracker.metaData.MaxFeeRate))
			continue
		}

		sigMsg := msgMatch.Serialize()
		if checkSigs {
			err := dc.acct.checkSig(sigMsg, msgMatch.Sig)
			if err != nil {
				// If the caller (e.g. handleMatchRoute) requests signature
				// verification, this is fatal.
				return nil, nil, fmt.Errorf("parseMatches: match signature verification failed: %w", err)
			}
		}
		sig, err := dc.acct.sign(sigMsg)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}

		// Success. Add the serverMatch and the Acknowledgement.
		acks = append(acks, msgjson.Acknowledgement{
			MatchID: msgMatch.MatchID,
			Sig:     sig,
		})

		trackerID := tracker.ID()
		match := matches[trackerID]
		if match == nil {
			match = &serverMatches{
				tracker: tracker,
			}
			matches[trackerID] = match
		}
		if isCancel {
			match.cancel = msgMatch // taker match
		} else {
			match.msgMatches = append(match.msgMatches, msgMatch)
		}

		status := order.MatchStatus(msgMatch.Status)
		dc.log.Debugf("Registering match %v for order %v (%v) in status %v",
			msgMatch.MatchID, oid, order.MatchSide(msgMatch.Side), status)
	}

	var err error
	if len(errs) > 0 {
		err = fmt.Errorf("parseMatches errors: %s", strings.Join(errs, ", "))
	}
	// A non-nil error only means that at least one match failed to parse, so we
	// must return the successful matches and acks for further processing.
	return matches, acks, err
}

// matchDiscreps specifies a trackedTrades's missing and extra matches compared
// to the server's list of active matches as returned in the connect response.
type matchDiscreps struct {
	trade   *trackedTrade
	missing []*matchTracker
	extra   []*msgjson.Match
}

// matchStatusConflict is a conflict between our status, and the status returned
// by the server in the connect response.
type matchStatusConflict struct {
	trade   *trackedTrade
	matches []*matchTracker
}

// compareServerMatches resolves the matches reported by the server in the
// 'connect' response against those marked incomplete in the matchTracker map
// for each serverMatch.
// Reported matches with missing trackers are already checked by parseMatches,
// but we also must check for incomplete matches that the server is not
// reporting.
func (dc *dexConnection) compareServerMatches(srvMatches map[order.OrderID]*serverMatches) (
	exceptions map[order.OrderID]*matchDiscreps, statusConflicts map[order.OrderID]*matchStatusConflict) {

	exceptions = make(map[order.OrderID]*matchDiscreps)
	statusConflicts = make(map[order.OrderID]*matchStatusConflict)

	// Identify extra matches named by the server response that we do not
	// recognize.
	for oid, match := range srvMatches {
		var extra []*msgjson.Match
		match.tracker.mtx.RLock()
		for _, msgMatch := range match.msgMatches {
			var matchID order.MatchID
			copy(matchID[:], msgMatch.MatchID)
			mt := match.tracker.matches[matchID]
			if mt == nil {
				extra = append(extra, msgMatch)
				continue
			}
			mt.exceptionMtx.Lock()
			mt.checkServerRevoke = false
			mt.exceptionMtx.Unlock()
			if mt.Status != order.MatchStatus(msgMatch.Status) {
				conflict := statusConflicts[oid]
				if conflict == nil {
					conflict = &matchStatusConflict{trade: match.tracker}
					statusConflicts[oid] = conflict
				}
				conflict.matches = append(conflict.matches, mt)
			}
		}
		match.tracker.mtx.RUnlock()
		if len(extra) > 0 {
			exceptions[match.tracker.ID()] = &matchDiscreps{
				trade: match.tracker,
				extra: extra,
			}
		}
	}

	in := func(matches []*msgjson.Match, mid []byte) bool {
		for _, m := range matches {
			if bytes.Equal(m.MatchID, mid) {
				return true
			}
		}
		return false
	}

	setMissing := func(trade *trackedTrade, missing []*matchTracker) {
		if tt, found := exceptions[trade.ID()]; found {
			tt.missing = missing
		} else {
			exceptions[trade.ID()] = &matchDiscreps{
				trade:   trade,
				missing: missing,
			}
		}
	}

	// Identify active matches that are missing from server's response.
	dc.tradeMtx.RLock()
	defer dc.tradeMtx.RUnlock()
	for oid, trade := range dc.trades {
		activeMatches := trade.activeMatches()
		if len(activeMatches) == 0 {
			continue
		}
		tradeMatches, found := srvMatches[oid]
		if !found {
			// ALL of this trade's active matches are missing.
			setMissing(trade, activeMatches)
			continue // check next local trade
		}
		// Check this local trade's active matches against server's reported
		// matches for this trade.
		var missing []*matchTracker
		for _, match := range activeMatches { // each local match
			if !in(tradeMatches.msgMatches, match.MatchID[:]) { // against reported matches
				missing = append(missing, match)
			}
		}
		if len(missing) > 0 {
			setMissing(trade, missing)
		}
	}

	return
}

// reconcileTrades compares the statuses of orders in the dc.trades map to the
// statuses returned by the server on `connect`, updating the statuses of the
// tracked trades where applicable e.g.
//   - Booked orders that were tracked as Epoch are updated to status Booked.
//   - Orders thought to be active in the dc.trades map but not returned by the
//     server are updated to Executed, Canceled or Revoked.
//
// Setting the order status appropriately now, especially for inactive orders,
// ensures that...
//   - the affected trades can be retired once the trade ticker (in core.listen)
//     observes that there are no active matches for the trades.
//   - coins are unlocked either as the affected trades' matches are swapped or
//     revoked (for trades with active matches), or when the trades are retired.
//
// Also purges "stale" cancel orders if the targeted order is returned in the
// server's `connect` response. See *trackedTrade.deleteStaleCancelOrder for
// the definition of a stale cancel order.
func (dc *dexConnection) reconcileTrades(srvOrderStatuses []*msgjson.OrderStatus) (unknownOrdersCount, reconciledOrdersCount int) {
	dc.tradeMtx.RLock()
	// Check for unknown orders reported as active by the server. If such
	// exists, could be that they were known to the client but were thought
	// to be inactive and thus were not loaded from db or were retired.
	srvActiveOrderStatuses := make(map[order.OrderID]*msgjson.OrderStatus, len(srvOrderStatuses))
	for _, srvOrderStatus := range srvOrderStatuses {
		var oid order.OrderID
		copy(oid[:], srvOrderStatus.ID)
		if _, tracked := dc.trades[oid]; tracked {
			srvActiveOrderStatuses[oid] = srvOrderStatus
		} else {
			dc.log.Warnf("Unknown order %v reported by DEX %s as active", oid, dc.acct.host)
			unknownOrdersCount++
		}
	}
	knownActiveTrades := make(map[order.OrderID]*trackedTrade)
	for oid, trade := range dc.trades {
		trade.mtx.RLock()
		if trade.metaData.Status == order.OrderStatusEpoch || trade.metaData.Status == order.OrderStatusBooked {
			knownActiveTrades[oid] = trade
		} else if srvOrderStatus := srvActiveOrderStatuses[oid]; srvOrderStatus != nil {
			// Lock redemption funds?
			dc.log.Warnf("Inactive order %v, status %q reported by DEX %s as active, status %q",
				oid, trade.metaData.Status, dc.acct.host, order.OrderStatus(srvOrderStatus.Status))
		}
		trade.mtx.RUnlock()
	}
	dc.tradeMtx.RUnlock()

	updateOrder := func(trade *trackedTrade, srvOrderStatus *msgjson.OrderStatus) {
		reconciledOrdersCount++
		oid := trade.ID()
		previousStatus := trade.metaData.Status
		newStatus := order.OrderStatus(srvOrderStatus.Status)
		trade.metaData.Status = newStatus
		// If there is an associated cancel order, and we are revising the
		// status of the targeted order to anything other than canceled, we can
		// infer the cancel order is done. Update the status of the cancel order
		// and unlink it from the trade. If the targeted order is reported as
		// canceled, that indicates we submitted a cancel order preimage but
		// missed the match notification, so the cancel order is executed.
		if newStatus != order.OrderStatusCanceled {
			trade.deleteCancelOrder()
		} else if trade.cancel != nil {
			cid := trade.cancel.ID()
			err := trade.db.UpdateOrderStatus(cid, order.OrderStatusExecuted)
			if err != nil {
				dc.log.Errorf("Failed to update status of executed cancel order %v: %v", cid, err)
			}
		}
		// If we're updating an order from an active state to executed,
		// canceled, or revoked, return the remaining quantity.
		if newStatus >= order.OrderStatusExecuted && trade.Trade().Remaining() > 0 &&
			(!trade.isMarketBuy() || len(trade.matches) == 0) {
			if trade.isMarketBuy() {
				trade.unlockRedemptionFraction(1, 1)
				trade.unlockRefundFraction(1, 1)
			} else {
				trade.unlockRedemptionFraction(trade.Trade().Remaining(), trade.Trade().Quantity)
				trade.unlockRefundFraction(trade.Trade().Remaining(), trade.Trade().Quantity)
			}
		}
		// Now update the trade.
		if err := trade.db.UpdateOrder(trade.metaOrder()); err != nil {
			dc.log.Errorf("Error updating status in db for order %v from %v to %v", oid, previousStatus, newStatus)
		} else {
			dc.log.Warnf("Order %v updated from recorded status %q to new status %q reported by DEX %s",
				oid, previousStatus, newStatus, dc.acct.host)
		}

		subject, details := trade.formatDetails(TopicOrderStatusUpdate, trade.token(), previousStatus, newStatus)
		dc.notify(newOrderNote(TopicOrderStatusUpdate, subject, details, db.WarningLevel, trade.coreOrderInternal()))
	}

	// Compare the status reported by the server for each known active trade. Orders
	// for which the server did not return a status are no longer active (now Executed,
	// Canceled or Revoked). Use the order_status route to determine the correct status
	// for such orders and update accordingly.
	var orderStatusRequests []*msgjson.OrderStatusRequest
	for oid, trade := range knownActiveTrades {
		srvOrderStatus := srvActiveOrderStatuses[oid]
		if srvOrderStatus == nil {
			// Order status not returned by server. Must be inactive now.
			// Request current status from the DEX.
			orderStatusRequests = append(orderStatusRequests, &msgjson.OrderStatusRequest{
				Base:    trade.Base(),
				Quote:   trade.Quote(),
				OrderID: trade.ID().Bytes(),
			})
			continue
		}

		trade.mtx.Lock()

		// Server reports this order as active. Delete any associated cancel
		// order if the cancel order's epoch has passed.
		trade.deleteStaleCancelOrder()

		serverStatus := order.OrderStatus(srvOrderStatus.Status)
		if trade.metaData.Status == serverStatus {
			dc.log.Tracef("Status reconciliation not required for order %v, status %q, server-reported status %q",
				oid, trade.metaData.Status, serverStatus)
		} else if trade.metaData.Status == order.OrderStatusEpoch && serverStatus == order.OrderStatusBooked {
			// Only standing orders can move from Epoch to Booked. This must have
			// happened in the client's absence (maybe a missed nomatch message).
			if lo, ok := trade.Order.(*order.LimitOrder); ok && lo.Force == order.StandingTiF {
				updateOrder(trade, srvOrderStatus)
			} else {
				dc.log.Warnf("Incorrect status %q reported for non-standing order %v by DEX %s, client status = %q",
					serverStatus, oid, dc.acct.host, trade.metaData.Status)
			}
		} else {
			dc.log.Warnf("Inconsistent status %q reported for order %v by DEX %s, client status = %q",
				serverStatus, oid, dc.acct.host, trade.metaData.Status)
		}

		trade.mtx.Unlock()
	}

	if len(orderStatusRequests) > 0 {
		dc.log.Debugf("Requesting statuses for %d orders from DEX %s", len(orderStatusRequests), dc.acct.host)

		// Send the 'order_status' request.
		var orderStatusResults []*msgjson.OrderStatus
		err := sendRequest(dc.WsConn, msgjson.OrderStatusRoute, orderStatusRequests, &orderStatusResults, DefaultResponseTimeout)
		if err != nil {
			dc.log.Errorf("Error retrieving order statuses from DEX %s: %v", dc.acct.host, err)
			return
		}

		if len(orderStatusResults) != len(orderStatusRequests) {
			dc.log.Errorf("Retrieved statuses for %d out of %d orders from order_status route",
				len(orderStatusResults), len(orderStatusRequests))
		}

		// Update the orders with the statuses received.
		for _, srvOrderStatus := range orderStatusResults {
			var oid order.OrderID
			copy(oid[:], srvOrderStatus.ID)
			trade := knownActiveTrades[oid] // no need to lock dc.tradeMtx
			trade.mtx.Lock()
			updateOrder(trade, srvOrderStatus)
			trade.mtx.Unlock()
		}
	}

	return
}

// tickAsset checks open matches related to a specific asset for needed action.
func (c *Core) tickAsset(dc *dexConnection, assetID uint32) assetMap {
	dc.tradeMtx.RLock()
	assetTrades := make([]*trackedTrade, 0, len(dc.trades))
	for _, trade := range dc.trades {
		if trade.Base() == assetID || trade.Quote() == assetID {
			assetTrades = append(assetTrades, trade)
		}
	}
	dc.tradeMtx.RUnlock()

	updated := make(assetMap)
	updateChan := make(chan assetMap)
	for _, trade := range assetTrades {
		if c.ctx.Err() != nil { // don't fail each one in sequence if shutting down
			return updated
		}
		trade := trade // bad go, bad
		go func() {
			newUpdates, err := c.tick(trade)
			if err != nil {
				c.log.Errorf("%s tick error: %v", dc.acct.host, err)
			}
			updateChan <- newUpdates
		}()
	}

	for range assetTrades {
		updated.merge(<-updateChan)
	}
	return updated
}

// Get the *dexConnection and connection status for the the host.
func (c *Core) dex(addr string) (*dexConnection, bool, error) {
	host, err := addrHost(addr)
	if err != nil {
		return nil, false, newError(addressParseErr, "error parsing address: %w", err)
	}

	// Get the dexConnection and the dex.Asset for each asset.
	c.connMtx.RLock()
	dc, found := c.conns[host]
	c.connMtx.RUnlock()
	if !found {
		return nil, false, fmt.Errorf("unknown DEX %s", addr)
	}
	return dc, dc.status() == comms.Connected, nil
}

// Get the *dexConnection for the the host. Return an error if the DEX is not
// connected.
func (c *Core) connectedDEX(addr string) (*dexConnection, error) {
	dc, connected, err := c.dex(addr)
	if err != nil {
		return nil, err
	}

	if dc.acct.locked() {
		return nil, fmt.Errorf("cannot place order on a locked %s account. Are you logged in?", dc.acct.host)
	}

	if !connected {
		return nil, fmt.Errorf("currently disconnected from %s. Cannot place order", dc.acct.host)
	}
	return dc, nil
}

// setEpoch sets the epoch. If the passed epoch is greater than the highest
// previously passed epoch, an epoch notification is sent to all subscribers and
// true is returned.
func (dc *dexConnection) setEpoch(mktID string, epochIdx uint64) bool {
	dc.epochMtx.Lock()
	defer dc.epochMtx.Unlock()
	if epochIdx > dc.epoch[mktID] {
		dc.epoch[mktID] = epochIdx
		dc.notify(newEpochNotification(dc.acct.host, mktID, epochIdx))
		return true
	}
	return false
}

// marketEpochDuration gets the market's epoch duration. If the market is not
// known, an error is logged and 0 is returned.
func (dc *dexConnection) marketEpochDuration(mktID string) uint64 {
	mkt := dc.marketConfig(mktID)
	if mkt == nil {
		dc.log.Errorf("marketEpoch called for unknown market %s", mktID)
		return 0
	}
	return mkt.EpochLen
}

// marketEpoch gets the epoch index for the specified market and time stamp. If
// the market is not known, 0 is returned.
func (dc *dexConnection) marketEpoch(mktID string, stamp time.Time) uint64 {
	epochLen := dc.marketEpochDuration(mktID)
	if epochLen == 0 {
		return 0
	}
	return uint64(stamp.UnixMilli()) / epochLen
}

// fetchFeeRate gets an asset's fee rate estimate from the server.
func (dc *dexConnection) fetchFeeRate(assetID uint32) (rate uint64) {
	msg, err := msgjson.NewRequest(dc.NextID(), msgjson.FeeRateRoute, assetID)
	if err != nil {
		dc.log.Errorf("Error fetching fee rate for %s: %v", unbip(assetID), err)
		return
	}
	errChan := make(chan error, 1)
	err = dc.RequestWithTimeout(msg, func(msg *msgjson.Message) {
		errChan <- msg.UnmarshalResult(&rate)
	}, DefaultResponseTimeout, func() {
		errChan <- fmt.Errorf("timed out waiting for fee_rate response")
	})
	if err == nil {
		err = <-errChan
	}
	if err != nil {
		dc.log.Errorf("Error fetching fee rate for %s: %v", unbip(assetID), err)
		return
	}
	return
}

// bestBookFeeSuggestion attempts to find a fee rate for the specified asset in
// any synced book.
func (dc *dexConnection) bestBookFeeSuggestion(assetID uint32) uint64 {
	dc.booksMtx.RLock()
	defer dc.booksMtx.RUnlock()
	for _, book := range dc.books {
		var feeRate uint64
		switch assetID {
		case book.base:
			feeRate = book.BaseFeeRate()
		case book.quote:
			feeRate = book.QuoteFeeRate()
		}
		if feeRate > 0 {
			return feeRate
		}
	}
	return 0
}

// blockWaiter is a message waiting to be stamped, signed, and sent once a
// specified coin has the requisite confirmations. The blockWaiter is similar to
// dcrdex/server/blockWaiter.Waiter, but is different enough to warrant a
// separate type.
type blockWaiter struct {
	assetID uint32
	trigger func() (bool, error)
	action  func(error)
}

// Config is the configuration for the Core.
type Config struct {
	// DBPath is a filepath to use for the client database. If the database does
	// not already exist, it will be created.
	DBPath string
	// Net is the current network.
	Net dex.Network
	// Logger is the Core's logger and is also used to create the sub-loggers
	// for the asset backends.
	Logger dex.Logger
	// Onion is the address (host:port) of a Tor proxy for use with DEX hosts
	// with a .onion address. To use Tor with regular DEX addresses as well, set
	// TorProxy.
	Onion string
	// TorProxy specifies the address of a Tor proxy server.
	TorProxy string
	// TorIsolation specifies whether to enable Tor circuit isolation.
	TorIsolation bool
	// Language. A BCP 47 language tag. Default is en-US.
	Language string
}

// Core is the core client application. Core manages DEX connections, wallets,
// database access, match negotiation and more.
type Core struct {
	ctx           context.Context
	wg            sync.WaitGroup
	ready         chan struct{}
	loginSlot     chan struct{}
	cfg           *Config
	log           dex.Logger
	db            db.DB
	net           dex.Network
	lockTimeTaker time.Duration
	lockTimeMaker time.Duration

	locale        map[Topic]*translation
	localePrinter *message.Printer

	credMtx     sync.RWMutex
	credentials *db.PrimaryCredentials

	seedGenerationTime uint64

	wsConstructor func(*comms.WsCfg) (comms.WsConn, error)
	newCrypter    func([]byte) encrypt.Crypter
	reCrypter     func([]byte, []byte) (encrypt.Crypter, error)
	latencyQ      *wait.TickerQueue

	connMtx sync.RWMutex
	conns   map[string]*dexConnection

	walletMtx sync.RWMutex
	wallets   map[uint32]*xcWallet

	waiterMtx    sync.RWMutex
	blockWaiters map[string]*blockWaiter

	tickSchedMtx sync.Mutex
	tickSched    map[order.OrderID]*time.Timer

	noteMtx   sync.RWMutex
	noteChans []chan Notification

	piSyncMtx sync.Mutex
	piSyncers map[order.OrderID]chan struct{}

	sentCommitsMtx sync.Mutex
	sentCommits    map[order.Commitment]chan struct{}

	ratesMtx        sync.RWMutex
	fiatRateSources map[string]*commonRateSource
	// stopFiatRateFetching will be used to shutdown fetchFiatExchangeRates
	// goroutine when all rate sources have been disabled.
	stopFiatRateFetching context.CancelFunc

	pendingWalletsMtx sync.RWMutex
	pendingWallets    map[uint32]bool
}

// New is the constructor for a new Core.
func New(cfg *Config) (*Core, error) {
	if cfg.Logger == nil {
		return nil, fmt.Errorf("Core.Config must specify a Logger")
	}
	boltDB, err := bolt.NewDB(cfg.DBPath, cfg.Logger.SubLogger("DB"))
	if err != nil {
		return nil, fmt.Errorf("database initialization error: %w", err)
	}
	if cfg.TorProxy != "" {
		if _, _, err = net.SplitHostPort(cfg.TorProxy); err != nil {
			return nil, err
		}
	}
	if cfg.Onion != "" {
		if _, _, err = net.SplitHostPort(cfg.Onion); err != nil {
			return nil, err
		}
	} else { // default to torproxy if onion not set explicitly
		cfg.Onion = cfg.TorProxy
	}
	lang := language.AmericanEnglish
	if cfg.Language != "" {
		acceptLang, err := language.Parse(cfg.Language)
		if err != nil {
			return nil, fmt.Errorf("unable to parse requested language: %w", err)
		}
		var langs []language.Tag
		for locale := range locales {
			tag, err := language.Parse(locale)
			if err != nil {
				return nil, fmt.Errorf("bad %v: %w", locale, err)
			}
			langs = append(langs, tag)
		}
		matcher := language.NewMatcher(langs)
		_, idx, conf := matcher.Match(acceptLang) // use index because tag may end up as something hyper specific like zh-Hans-u-rg-cnzzzz
		tag := langs[idx]
		switch conf {
		case language.Exact:
		case language.High, language.Low:
			cfg.Logger.Infof("Using language %v", tag)
		case language.No:
			return nil, fmt.Errorf("no match for %q in recognized languages %v", cfg.Language, langs)
		}
		lang = tag
	}
	cfg.Logger.Debugf("Using locale printer for %q", lang)

	locale, found := locales[lang.String()]
	if !found {
		return nil, fmt.Errorf("No translations for language %s", lang)
	}

	// Try to get the primary credentials, but ignore no-credentials error here
	// because the client may not be initialized.
	creds, err := boltDB.PrimaryCredentials()
	if err != nil && !errors.Is(err, db.ErrNoCredentials) {
		return nil, err
	}

	seedGenerationTime, err := boltDB.SeedGenerationTime()
	if err != nil && !errors.Is(err, db.ErrNoSeedGenTime) {
		return nil, err
	}

	core := &Core{
		cfg:           cfg,
		credentials:   creds,
		ready:         make(chan struct{}),
		log:           cfg.Logger,
		loginSlot:     make(chan struct{}, 1),
		db:            boltDB,
		conns:         make(map[string]*dexConnection),
		wallets:       make(map[uint32]*xcWallet),
		net:           cfg.Net,
		lockTimeTaker: dex.LockTimeTaker(cfg.Net),
		lockTimeMaker: dex.LockTimeMaker(cfg.Net),
		blockWaiters:  make(map[string]*blockWaiter),
		piSyncers:     make(map[order.OrderID]chan struct{}),
		sentCommits:   make(map[order.Commitment]chan struct{}),
		tickSched:     make(map[order.OrderID]*time.Timer),
		// Allowing to change the constructor makes testing a lot easier.
		wsConstructor: comms.NewWsConn,
		newCrypter:    encrypt.NewCrypter,
		reCrypter:     encrypt.Deserialize,
		latencyQ:      wait.NewTickerQueue(recheckInterval),

		locale:             locale,
		localePrinter:      message.NewPrinter(lang),
		seedGenerationTime: seedGenerationTime,

		fiatRateSources: make(map[string]*commonRateSource),
		pendingWallets:  make(map[uint32]bool),
	}

	// Populate the initial user data. User won't include any DEX info yet, as
	// those are retrieved when Run is called and the core connects to the DEXes.
	core.log.Debugf("new client core created")
	return core, nil
}

// Run runs the core. Satisfies the runner.Runner interface.
func (c *Core) Run(ctx context.Context) {
	c.log.Infof("Starting DEX client core")
	// Store the context as a field, since we will need to spawn new DEX threads
	// when new accounts are registered.
	c.ctx = ctx
	c.initialize() // connectDEX gets ctx for the wsConn
	close(c.ready)

	// The DB starts first and stops last.
	ctxDB, stopDB := context.WithCancel(context.Background())
	var dbWG sync.WaitGroup
	dbWG.Add(1)
	go func() {
		defer dbWG.Done()
		c.db.Run(ctxDB)
	}()

	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		c.latencyQ.Run(ctx)
	}()

	// Skip rate fetch setup if on simnet. Rate fetching maybe enabled if
	// desired.
	if c.cfg.Net != dex.Simnet {
		c.ratesMtx.Lock()
		// Retrieve disabled fiat rate sources from database.
		disabledSources, err := c.db.DisabledRateSources()
		if err != nil {
			c.log.Errorf("Unable to retrieve disabled fiat rate source: %v", err)
		}

		// Construct enabled fiat rate sources.
	fetchers:
		for token, rateFetcher := range fiatRateFetchers {
			for _, v := range disabledSources {
				if token == v {
					continue fetchers
				}
			}
			c.fiatRateSources[token] = newCommonRateSource(rateFetcher)
		}

		// Start goroutine for fiat rate fetcher's if we have at least one source.
		if len(c.fiatRateSources) != 0 {
			c.fetchFiatExchangeRates()
		} else {
			c.log.Debug("no fiat rate source initialized")
		}
		c.ratesMtx.Unlock()
	}

	c.wg.Wait() // block here until all goroutines except DB complete

	// Stop the DB after dexConnections and other goroutines are done.
	stopDB()
	dbWG.Wait()

	// At this point, it should be safe to access the data structures without
	// mutex protection. Goroutines have returned, and consumers should not call
	// Core methods after shutdown. We'll play it safe anyway.

	// Clear account private keys and wait for the DEX ws connections that began
	// shutting down on context cancellation (the listen goroutines have already
	// returned however). Warn about specific active orders, and unlock any
	// locked coins for inactive orders that are not yet retired.
	for _, dc := range c.dexConnections() {
		// context is already canceled, allowing just a Wait(), but just in case
		// use Disconnect otherwise it could hang forever.
		dc.connMaster.Disconnect()
		dc.acct.lock()

		// Note active orders, and unlock any coins locked by inactive orders.
		dc.tradeMtx.Lock()
		for _, trade := range dc.trades {
			oid := trade.ID()
			if trade.isActive() {
				c.log.Warnf("Shutting down with active order %v in status %v.", oid, trade.metaData.Status)
				continue
			}
			c.log.Debugf("Retiring inactive order %v. Unlocking coins = %v",
				oid, trade.coinsLocked || trade.changeLocked)
			delete(dc.trades, oid) // for inspection/debugging
			trade.returnCoins()
			// Do not bother with OrderNote/SubjectOrderRetired and BalanceNote
			// notes since any web/rpc servers should be down by now. Go
			// consumers can check orders on restart.
		}
		dc.tradeMtx.Unlock()
	}

	// Lock and disconnect the wallets.
	c.walletMtx.Lock()
	defer c.walletMtx.Unlock()
	for assetID, wallet := range c.wallets {
		delete(c.wallets, assetID)
		if !wallet.connected() {
			continue
		}
		symb := strings.ToUpper(unbip(assetID))
		c.log.Infof("Locking %s wallet", symb)
		if err := wallet.Lock(5 * time.Second); err != nil {
			c.log.Errorf("Failed to lock %v wallet: %v", symb, err)
		}
		wallet.Disconnect()
	}

	c.log.Infof("DEX client core off")
}

// Ready returns a channel that is closed when Run completes its initialization
// tasks and Core becomes ready for use.
func (c *Core) Ready() <-chan struct{} {
	return c.ready
}

// BackupDB makes a backup of the database at the specified location, optionally
// overwriting any existing file and compacting the database.
func (c *Core) BackupDB(dst string, overwrite, compact bool) error {
	return c.db.BackupTo(dst, overwrite, compact)
}

const defaultDEXPort = "7232"

// addrHost returns the host or url:port pair for an address.
func addrHost(addr string) (string, error) {
	const defaultHost = "localhost"
	const missingPort = "missing port in address"
	// Empty addresses are localhost.
	if addr == "" {
		return defaultHost + ":" + defaultDEXPort, nil
	}
	host, port, splitErr := net.SplitHostPort(addr)
	_, portErr := strconv.ParseUint(port, 10, 16)

	// net.SplitHostPort will error on anything not in the format
	// string:string or :string or if a colon is in an unexpected position,
	// such as in the scheme.
	// If the port isn't a port, it must also be parsed.
	if splitErr != nil || portErr != nil {
		// Any address with no colons is appended with the default port.
		var addrErr *net.AddrError
		if errors.As(splitErr, &addrErr) && addrErr.Err == missingPort {
			host = strings.Trim(addrErr.Addr, "[]") // JoinHostPort expects no brackets for ipv6 hosts
			return net.JoinHostPort(host, defaultDEXPort), nil
		}
		// These are addresses with at least one colon in an unexpected
		// position.
		a, err := url.Parse(addr)
		// This address is of an unknown format.
		if err != nil {
			return "", fmt.Errorf("addrHost: unable to parse address '%s'", addr)
		}
		host, port = a.Hostname(), a.Port()
		// If the address parses but there is no port, append the default port.
		if port == "" {
			return net.JoinHostPort(host, defaultDEXPort), nil
		}
	}
	// We have a port but no host. Replace with localhost.
	if host == "" {
		host = defaultHost
	}
	return net.JoinHostPort(host, port), nil
}

// creds returns the *PrimaryCredentials.
func (c *Core) creds() *db.PrimaryCredentials {
	c.credMtx.RLock()
	defer c.credMtx.RUnlock()
	if c.credentials == nil {
		return nil
	}
	if len(c.credentials.EncInnerKey) == 0 {
		// database upgraded, but Core hasn't updated the PrimaryCredentials.
		return nil
	}
	return c.credentials
}

// setCredentials stores the *PrimaryCredentials.
func (c *Core) setCredentials(creds *db.PrimaryCredentials) {
	c.credMtx.Lock()
	c.credentials = creds
	c.credMtx.Unlock()
}

// Network returns the current DEX network.
func (c *Core) Network() dex.Network {
	return c.net
}

// Exchanges creates a map of *Exchange keyed by host, including markets and
// orders.
func (c *Core) Exchanges() map[string]*Exchange {
	dcs := c.dexConnections()
	infos := make(map[string]*Exchange, len(dcs))
	for _, dc := range dcs {
		infos[dc.acct.host] = dc.exchangeInfo()
	}
	return infos
}

// Exchange returns an exchange with a certain host. It returns an error if
// no exchange exists at that host.
func (c *Core) Exchange(host string) (*Exchange, error) {
	dc, _, err := c.dex(host)
	if err != nil {
		return nil, err
	}
	return dc.exchangeInfo(), nil
}

// dexConnections creates a slice of the *dexConnection in c.conns.
func (c *Core) dexConnections() []*dexConnection {
	c.connMtx.RLock()
	defer c.connMtx.RUnlock()
	conns := make([]*dexConnection, 0, len(c.conns))
	for _, conn := range c.conns {
		conns = append(conns, conn)
	}
	return conns
}

// wallet gets the wallet for the specified asset ID in a thread-safe way.
func (c *Core) wallet(assetID uint32) (*xcWallet, bool) {
	c.walletMtx.RLock()
	defer c.walletMtx.RUnlock()
	w, found := c.wallets[assetID]
	return w, found
}

// encryptionKey retrieves the application encryption key. The password is used
// to recreate the outer key/crypter, which is then used to decode and recreate
// the inner key/crypter.
func (c *Core) encryptionKey(pw []byte) (encrypt.Crypter, error) {
	creds := c.creds()
	if creds == nil {
		return nil, fmt.Errorf("primary credentials not retrieved. Is the client initialized?")
	}
	outerCrypter, err := c.reCrypter(pw, creds.OuterKeyParams)
	if err != nil {
		return nil, fmt.Errorf("outer key deserialization error: %w", err)
	}
	defer outerCrypter.Close()
	innerKey, err := outerCrypter.Decrypt(creds.EncInnerKey)
	if err != nil {
		return nil, fmt.Errorf("inner key decryption error: %w", err)
	}
	innerCrypter, err := c.reCrypter(innerKey, creds.InnerKeyParams)
	if err != nil {
		return nil, fmt.Errorf("inner key deserialization error: %w", err)
	}
	return innerCrypter, nil
}

func (c *Core) storeDepositAddress(wdbID []byte, addr string) error {
	// Store the new address in the DB.
	dbWallet, err := c.db.Wallet(wdbID)
	if err != nil {
		return fmt.Errorf("error retrieving DB wallet: %w", err)
	}
	dbWallet.Address = addr
	return c.db.UpdateWallet(dbWallet)
}

func (c *Core) connectAndUpdateWallet(w *xcWallet) error {
	assetID := w.AssetID

	token := asset.TokenInfo(assetID)
	if token != nil {
		parentWallet, found := c.wallet(token.ParentID)
		if !found {
			return fmt.Errorf("token %s wallet has no %s parent?", unbip(assetID), unbip(token.ParentID))
		}
		if !parentWallet.connected() {
			if err := c.connectAndUpdateWallet(parentWallet); err != nil {
				return fmt.Errorf("failed to connect %s parent wallet for %s token",
					unbip(token.ParentID), unbip(assetID))
			}
		}
	}

	c.log.Infof("Connecting wallet for %s", unbip(assetID))
	addr := w.currentDepositAddress()
	newAddr, err := c.connectWallet(w)
	if err != nil {
		return fmt.Errorf("connectWallet: %w", err) // core.Error with code connectWalletErr
	}
	if newAddr != addr {
		c.log.Infof("New deposit address for %v wallet: %v", unbip(assetID), newAddr)
		if err = c.storeDepositAddress(w.dbID, newAddr); err != nil {
			return fmt.Errorf("storeDepositAddress: %w", err)
		}
	}
	// First update balances since it is included in WalletState. Ignore errors
	// because some wallets may not reveal balance until unlocked.
	_, err = c.updateWalletBalance(w)
	if err != nil {
		// Warn because the balances will be stale.
		c.log.Warnf("Could not retrieve balances from %s wallet: %v", unbip(assetID), err)
	}

	c.notify(newWalletStateNote(w.state()))
	return nil
}

// connectedWallet fetches a wallet and will connect the wallet if it is not
// already connected. If the wallet gets connected, this also emits WalletState
// and WalletBalance notification.
func (c *Core) connectedWallet(assetID uint32) (*xcWallet, error) {
	wallet, exists := c.wallet(assetID)
	if !exists {
		return nil, newError(missingWalletErr, "no configured wallet found for %s (%d)",
			strings.ToUpper(unbip(assetID)), assetID)
	}
	if !wallet.connected() {
		err := c.connectAndUpdateWallet(wallet)
		if err != nil {
			return nil, err
		}
	}
	return wallet, nil
}

// connectWallet connects to the wallet and returns the deposit address
// validated by the xcWallet after connecting. If the wallet backend is still
// syncing, this also starts a goroutine to monitor sync status, emitting
// WalletStateNotes on each progress update.
func (c *Core) connectWallet(w *xcWallet) (depositAddr string, err error) {
	err = w.Connect() // ensures valid deposit address
	if err != nil {
		return "", codedError(connectWalletErr, err)
	}

	w.mtx.RLock()
	defer w.mtx.RUnlock()
	depositAddr = w.address

	// If the wallet is not synced, start a loop to check the sync status until
	// it is.
	if !w.synced {
		c.startWalletSyncMonitor(w)
	}

	return
}

// Connect to the wallet if not already connected. Unlock the wallet if not
// already unlocked.
func (c *Core) connectAndUnlock(crypter encrypt.Crypter, wallet *xcWallet) error {
	if !wallet.connected() {
		c.log.Infof("Connecting wallet for %s", unbip(wallet.AssetID))
		err := c.connectAndUpdateWallet(wallet)
		if err != nil {
			return err
		}
	}
	// Unlock if either the backend itself is locked or if we lack a cached
	// unencrypted password for encrypted wallets.
	if !wallet.unlocked() {
		// Note that in cases where we already had the cached decrypted password
		// but it was just the backend reporting as locked, only unlocking the
		// backend is needed but this redecrypts the password using the provided
		// crypter. This case could instead be handled with a refreshUnlock.
		err := wallet.Unlock(crypter)
		if err != nil {
			return newError(walletAuthErr, "failed to unlock %s wallet: %w",
				unbip(wallet.AssetID), err)
		}
		// Notify new wallet state.
		c.notify(newWalletStateNote(wallet.state()))
	}
	return nil
}

// walletBalance gets the xcWallet's current WalletBalance, which includes the
// db.Balance plus order/contract locked amounts. The data is not stored. Use
// updateWalletBalance instead to also update xcWallet.balance and the DB.
func (c *Core) walletBalance(wallet *xcWallet) (*WalletBalance, error) {
	bal, err := wallet.Balance()
	if err != nil {
		return nil, err
	}
	contractLockedAmt, orderLockedAmt := c.lockedAmounts(wallet.AssetID)
	return &WalletBalance{
		Balance: &db.Balance{
			Balance: *bal,
			Stamp:   time.Now(),
		},
		OrderLocked:    orderLockedAmt,
		ContractLocked: contractLockedAmt,
	}, nil
}

// updateWalletBalance retrieves balances for the wallet, updates
// xcWallet.balance and the balance in the DB, and emits a BalanceNote.
func (c *Core) updateWalletBalance(wallet *xcWallet) (*WalletBalance, error) {
	walletBal, err := c.walletBalance(wallet)
	if err != nil {
		return nil, err
	}
	wallet.setBalance(walletBal)

	// Store the db.Balance.
	err = c.db.UpdateBalance(wallet.dbID, walletBal.Balance)
	if err != nil {
		return nil, fmt.Errorf("error updating %s balance in database: %w", unbip(wallet.AssetID), err)
	}

	c.notify(newBalanceNote(wallet.AssetID, walletBal))
	return walletBal, nil
}

// lockedAmounts returns the total amount locked in unredeemed and unrefunded
// swaps (contractLocked) and the total amount locked by orders for future
// swaps (orderLocked). Only applies to trades where the specified assetID is
// the fromAssetID.
func (c *Core) lockedAmounts(assetID uint32) (contractLocked, orderLocked uint64) {
	for _, dc := range c.dexConnections() {
		for _, tracker := range dc.trackedTrades() {
			if tracker.fromAssetID == assetID {
				tracker.mtx.RLock()
				contractLocked += tracker.unspentContractAmounts()
				orderLocked += tracker.lockedAmount()
				tracker.mtx.RUnlock()
			}
		}
	}
	return
}

// updateBalances updates the balance for every key in the counter map.
// Notifications are sent.
func (c *Core) updateBalances(assets assetMap) {
	if len(assets) == 0 {
		return
	}
	for assetID := range assets {
		w, exists := c.wallet(assetID)
		if !exists {
			// This should never be the case, but log an error in case I'm
			// wrong or something changes.
			c.log.Errorf("non-existent %d wallet should exist", assetID)
			continue
		}
		_, err := c.updateWalletBalance(w)
		if err != nil {
			c.log.Errorf("error updating %q balance: %v", unbip(assetID), err)
			continue
		}
	}
}

// updateAssetBalance updates the balance for the specified asset. A
// notification is sent.
func (c *Core) updateAssetBalance(assetID uint32) {
	c.updateBalances(assetMap{assetID: struct{}{}})
}

// xcWallets creates a slice of the c.wallets xcWallets.
func (c *Core) xcWallets() []*xcWallet {
	c.walletMtx.RLock()
	defer c.walletMtx.RUnlock()
	wallets := make([]*xcWallet, 0, len(c.wallets))
	for _, wallet := range c.wallets {
		wallets = append(wallets, wallet)
	}
	return wallets
}

// Wallets creates a slice of WalletState for all known wallets.
func (c *Core) Wallets() []*WalletState {
	wallets := c.xcWallets()
	state := make([]*WalletState, 0, len(wallets))
	for _, wallet := range wallets {
		state = append(state, wallet.state())
	}
	return state
}

// SupportedAssets returns a map of asset information for supported assets.
func (c *Core) SupportedAssets() map[uint32]*SupportedAsset {
	return c.assetMap()
}

func (c *Core) walletCreationPending(tokenID uint32) bool {
	c.pendingWalletsMtx.RLock()
	defer c.pendingWalletsMtx.RUnlock()
	return c.pendingWallets[tokenID]
}

func (c *Core) setWalletCreationPending(tokenID uint32) error {
	c.pendingWalletsMtx.Lock()
	defer c.pendingWalletsMtx.Unlock()
	if c.pendingWallets[tokenID] {
		return fmt.Errorf("creation already pending for %s", unbip(tokenID))
	}
	c.pendingWallets[tokenID] = true
	return nil
}

func (c *Core) setWalletCreationComplete(tokenID uint32) {
	c.pendingWalletsMtx.Lock()
	delete(c.pendingWallets, tokenID)
	c.pendingWalletsMtx.Unlock()
}

// assetMap returns a map of asset information for supported assets.
func (c *Core) assetMap() map[uint32]*SupportedAsset {
	supported := asset.Assets()
	assets := make(map[uint32]*SupportedAsset, len(supported))
	c.walletMtx.RLock()
	defer c.walletMtx.RUnlock()
	for assetID, asset := range supported {
		var wallet *WalletState
		w, found := c.wallets[assetID]
		if found {
			wallet = w.state()
		}
		assets[assetID] = &SupportedAsset{
			ID:       assetID,
			Symbol:   asset.Symbol,
			Wallet:   wallet,
			Info:     asset.Info,
			Name:     asset.Info.Name,
			UnitInfo: asset.Info.UnitInfo,
		}
		for tokenID, token := range asset.Tokens {
			wallet = nil
			w, found := c.wallets[tokenID]
			if found {
				wallet = w.state()
			}
			assets[tokenID] = &SupportedAsset{
				ID:                    tokenID,
				Symbol:                dex.BipIDSymbol(tokenID),
				Wallet:                wallet,
				Token:                 token,
				Name:                  token.Name,
				UnitInfo:              token.UnitInfo,
				WalletCreationPending: c.walletCreationPending(tokenID),
			}
		}
	}
	return assets
}

// User is a thread-safe getter for the User.
func (c *Core) User() *User {
	return &User{
		Assets:             c.assetMap(),
		Exchanges:          c.Exchanges(),
		Initialized:        c.IsInitialized(),
		SeedGenerationTime: c.seedGenerationTime,
		FiatRates:          c.fiatConversions(),
	}
}

// CreateWallet creates a new exchange wallet.
func (c *Core) CreateWallet(appPW, walletPW []byte, form *WalletForm) error {
	assetID := form.AssetID
	symbol := unbip(assetID)
	_, exists := c.wallet(assetID)
	if exists {
		return fmt.Errorf("%s wallet already exists", symbol)
	}

	crypter, err := c.encryptionKey(appPW)
	if err != nil {
		return err
	}

	var creationQueued bool
	defer func() {
		if !creationQueued {
			crypter.Close()
		}
	}()

	// If this isn't a token, easy route.
	token := asset.TokenInfo(assetID)
	if token == nil {
		_, err = c.createWalletOrToken(crypter, walletPW, form)
		return err
	}

	// Prevent two different tokens from trying to create the parent simulataneously.
	if err = c.setWalletCreationPending(token.ParentID); err != nil {
		return err
	}
	defer c.setWalletCreationComplete(token.ParentID)

	// If the parent already exists, easy route.
	_, found := c.wallet(token.ParentID)
	if found {
		_, err = c.createWalletOrToken(crypter, walletPW, form)
		return err
	}

	// Double-registration mode. The parent wallet will be created
	// synchronously, then a goroutine is launched to wait for the parent to
	// sync before creating the token wallet. The caller can get information
	// about the asynchronous creation from WalletCreationNote notifications.

	// First check that they configured the parent asset.
	if form.ParentForm == nil {
		return fmt.Errorf("no parent wallet %d for token %d (%s), and no parent asset configuration provided",
			token.ParentID, assetID, unbip(assetID))
	}
	if form.ParentForm.AssetID != token.ParentID {
		return fmt.Errorf("parent form asset ID %d is not expected value %d",
			form.ParentForm.AssetID, token.ParentID)
	}

	// Create the parent synchronously.
	parentWallet, err := c.createWalletOrToken(crypter, walletPW, form.ParentForm)
	if err != nil {
		return fmt.Errorf("error creating parent wallet: %v", err)
	}

	if err = c.setWalletCreationPending(assetID); err != nil {
		return err
	}

	// Start a goroutine to wait until the parent wallet is synced, and then
	// begin creation of the token wallet.
	c.wg.Add(1)

	c.notify(newWalletCreationNote(TopicCreationQueued, "", "", db.Data, assetID))

	go func() {
		defer c.wg.Done()
		defer c.setWalletCreationComplete(assetID)
		defer crypter.Close()

		for {
			parentWallet.mtx.RLock()
			synced := parentWallet.synced
			parentWallet.mtx.RUnlock()
			if synced {
				break
			}
			select {
			case <-c.ctx.Done():
				return
			case <-time.After(time.Second):
			}
		}
		// If there was a walletPW provided, it was for the parent wallet, so
		// use nil here.
		if _, err := c.createWalletOrToken(crypter, nil, form); err != nil {
			c.log.Errorf("failed to create token wallet: %v", err)
			subject, details := c.formatDetails(TopicQueuedCreationFailed, unbip(token.ParentID), symbol)
			c.notify(newWalletCreationNote(TopicQueuedCreationFailed, subject, details, db.ErrorLevel, assetID))
		} else {
			c.notify(newWalletCreationNote(TopicQueuedCreationSuccess, "", "", db.Data, assetID))
		}
	}()
	creationQueued = true
	return nil
}

func (c *Core) createWalletOrToken(crypter encrypt.Crypter, walletPW []byte, form *WalletForm) (wallet *xcWallet, err error) {
	assetID := form.AssetID
	symbol := unbip(assetID)
	token := asset.TokenInfo(assetID)
	var dbWallet *db.Wallet
	if token != nil {
		dbWallet, err = c.createTokenWallet(assetID, token, form)
	} else {
		dbWallet, err = c.createWallet(crypter, walletPW, assetID, form)
	}
	if err != nil {
		return nil, err
	}

	wallet, err = c.loadWallet(dbWallet)
	if err != nil {
		return nil, fmt.Errorf("error loading wallet for %d -> %s: %w", assetID, symbol, err)
	}

	dbWallet.Address, err = c.connectWallet(wallet)
	if err != nil {
		return nil, err
	}

	initErr := func(s string, a ...interface{}) (*xcWallet, error) {
		_ = wallet.Lock(2 * time.Second) // just try, but don't confuse the user with an error
		wallet.Disconnect()
		return nil, fmt.Errorf(s, a...)
	}

	err = wallet.Unlock(crypter)
	if err != nil {
		wallet.Disconnect()
		return nil, fmt.Errorf("%s wallet authentication error: %w", symbol, err)
	}

	balances, err := c.walletBalance(wallet)
	if err != nil {
		return initErr("error getting wallet balance for %s: %w", symbol, err)
	}
	wallet.setBalance(balances)         // update xcWallet's WalletBalance
	dbWallet.Balance = balances.Balance // store the db.Balance

	// Store the wallet in the database.
	err = c.db.UpdateWallet(dbWallet)
	if err != nil {
		return initErr("error storing wallet credentials: %w", err)
	}

	c.log.Infof("Created %s wallet. Balance available = %d / "+
		"locked = %d / locked in contracts = %d, Deposit address = %s",
		symbol, balances.Available, balances.Locked, balances.ContractLocked,
		dbWallet.Address)

	// The wallet has been successfully created. Store it.
	c.walletMtx.Lock()
	c.wallets[assetID] = wallet
	c.walletMtx.Unlock()

	c.notify(newWalletStateNote(wallet.state()))

	return wallet, nil
}

func (c *Core) createWallet(crypter encrypt.Crypter, walletPW []byte, assetID uint32, form *WalletForm) (*db.Wallet, error) {
	walletDef, err := walletDefinition(assetID, form.Type)
	if err != nil {
		return nil, err
	}

	// Sometimes core will insert data into the Settings map to communicate
	// information back to the wallet, so it cannot be nil.
	if form.Config == nil {
		form.Config = make(map[string]string)
	}

	// Remove unused key-values from parsed settings before saving to db.
	// Especially necessary if settings was parsed from a config file, b/c
	// config files usually define more key-values than we need.
	// Expected keys should be lowercase because config.Parse returns lowercase
	// keys.
	expectedKeys := make(map[string]bool, len(walletDef.ConfigOpts))
	for _, option := range walletDef.ConfigOpts {
		expectedKeys[strings.ToLower(option.Key)] = true
	}
	for key := range form.Config {
		if !expectedKeys[key] {
			delete(form.Config, key)
		}
	}

	if walletDef.Seeded {
		if len(walletPW) > 0 {
			return nil, errors.New("external password incompatible with seeded wallet")
		}
		walletPW, err = c.createSeededWallet(assetID, crypter, form)
		if err != nil {
			return nil, err
		}
	}

	var encPW []byte
	if len(walletPW) > 0 {
		encPW, err = crypter.Encrypt(walletPW)
		if err != nil {
			return nil, fmt.Errorf("wallet password encryption error: %w", err)
		}
	}

	return &db.Wallet{
		Type:        walletDef.Type,
		AssetID:     assetID,
		Settings:    form.Config,
		EncryptedPW: encPW,
		// Balance and Address are set after connect.
	}, nil
}

func (c *Core) createTokenWallet(tokenID uint32, token *asset.Token, form *WalletForm) (*db.Wallet, error) {
	wallet, found := c.wallet(token.ParentID)
	if !found {
		return nil, fmt.Errorf("no parent wallet %d for token %d (%s)", token.ParentID, tokenID, unbip(tokenID))
	}

	tokenMaster, is := wallet.Wallet.(asset.TokenMaster)
	if !is {
		return nil, fmt.Errorf("parent wallet %s is not a TokenMaster", unbip(token.ParentID))
	}

	// Sometimes core will insert data into the Settings map to communicate
	// information back to the wallet, so it cannot be nil.
	if form.Config == nil {
		form.Config = make(map[string]string)
	}

	if err := tokenMaster.CreateTokenWallet(tokenID, form.Config); err != nil {
		return nil, fmt.Errorf("CreateTokenWallet error: %w", err)
	}

	return &db.Wallet{
		Type:     "token",
		AssetID:  tokenID,
		Settings: form.Config,
		// EncryptedPW ignored because we assume throughout that token wallet
		// authorization is handled by the parent.
		// Balance and Address are set after connect.
	}, nil
}

// createSeededWallet initializes a seeded wallet with an asset-specific seed
// and password derived deterministically from the app seed. The password is
// returned for encrypting and storing.
func (c *Core) createSeededWallet(assetID uint32, crypter encrypt.Crypter, form *WalletForm) ([]byte, error) {
	seed, pw, err := c.assetSeedAndPass(assetID, crypter)
	if err != nil {
		return nil, err
	}
	defer encode.ClearBytes(seed)

	c.log.Infof("Initializing a built-in %s wallet", unbip(assetID))
	if err = asset.CreateWallet(assetID, &asset.CreateWalletParams{
		Type:     form.Type,
		Seed:     seed,
		Pass:     pw,
		Settings: form.Config,
		DataDir:  c.assetDataDirectory(assetID),
		Net:      c.net,
		Logger:   c.log.SubLogger("CREATE"),
	}); err != nil {
		return nil, fmt.Errorf("Error creating wallet: %w", err)
	}

	return pw, nil
}

func (c *Core) assetSeedAndPass(assetID uint32, crypter encrypt.Crypter) (seed, pass []byte, err error) {
	creds := c.creds()
	if creds == nil {
		return nil, nil, errors.New("no v2 credentials stored")
	}

	appSeed, err := crypter.Decrypt(creds.EncSeed)
	if err != nil {
		return nil, nil, fmt.Errorf("app seed decryption error: %w", err)
	}

	seed, pass = AssetSeedAndPass(assetID, appSeed)
	return seed, pass, nil
}

// AssetSeedAndPass derives the wallet seed and password that would be used to
// create a native wallet for a particular asset and application seed. Depending
// on external wallet software and their key derivation paths, this seed may be
// usable for accessing funds outside of DEX applications, e.g. btcwallet.
func AssetSeedAndPass(assetID uint32, appSeed []byte) ([]byte, []byte) {
	b := make([]byte, len(appSeed)+4)
	copy(b, appSeed)
	binary.BigEndian.PutUint32(b[len(appSeed):], assetID)

	s := blake256.Sum256(b)
	p := blake256.Sum256(s[:])
	return s[:], p[:]
}

// assetDataDirectory is a directory for a wallet to use for local storage.
func (c *Core) assetDataDirectory(assetID uint32) string {
	return filepath.Join(filepath.Dir(c.cfg.DBPath), "assetdb", unbip(assetID))
}

// assetDataBackupDirectory is a directory for a wallet to use for backups of
// data. Wallet data is copied here instead of being deleted when recovering a
// wallet.
func (c *Core) assetDataBackupDirectory(assetID uint32) string {
	return filepath.Join(filepath.Dir(c.cfg.DBPath), "assetdb-backup", unbip(assetID))
}

// loadWallet uses the data from the database to construct a new exchange
// wallet. The returned wallet is running but not connected.
func (c *Core) loadWallet(dbWallet *db.Wallet) (*xcWallet, error) {
	var parent *xcWallet
	assetID := dbWallet.AssetID

	// Construct the unconnected xcWallet.
	contractLockedAmt, orderLockedAmt := c.lockedAmounts(assetID)
	wallet := &xcWallet{ // captured by the PeersChange closure
		AssetID: assetID,
		balance: &WalletBalance{
			Balance:        dbWallet.Balance,
			OrderLocked:    orderLockedAmt,
			ContractLocked: contractLockedAmt,
		},
		encPass:      dbWallet.EncryptedPW,
		address:      dbWallet.Address,
		peerCount:    -1, // no count yet
		dbID:         dbWallet.ID(),
		walletType:   dbWallet.Type,
		broadcasting: new(uint32),
	}

	token := asset.TokenInfo(assetID)

	peersChange := func(numPeers uint32, err error) {
		go c.peerChange(wallet, numPeers, err)
	}

	tipChange := func(err error) {
		// asset.Wallet implementations should not need wait for the
		// callback, as they don't know what it is, and will likely launch
		// TipChange as a goroutine. However, guard against the possibility
		// of deadlocking a Core method that calls Wallet.Disconnect.
		go c.tipChange(assetID, err)
	}

	var w asset.Wallet
	var err error
	if token == nil {
		walletCfg := &asset.WalletConfig{
			Type:        dbWallet.Type,
			Settings:    dbWallet.Settings,
			TipChange:   tipChange,
			PeersChange: peersChange,
			DataDir:     c.assetDataDirectory(assetID),
		}

		walletCfg.Settings[asset.SpecialSettingActivelyUsed] =
			strconv.FormatBool(c.assetHasActiveOrders(dbWallet.AssetID))
		defer delete(walletCfg.Settings, asset.SpecialSettingActivelyUsed)

		logger := c.log.SubLogger(unbip(assetID))
		w, err = asset.OpenWallet(assetID, walletCfg, logger, c.net)
	} else {
		var found bool
		parent, found = c.wallet(token.ParentID)
		if !found {
			return nil, fmt.Errorf("cannot load %s wallet before %s wallet", unbip(assetID), unbip(token.ParentID))
		}

		tokenMaster, is := parent.Wallet.(asset.TokenMaster)
		if !is {
			return nil, fmt.Errorf("%s token's %s parent wallet is not a TokenMaster", unbip(assetID), unbip(token.ParentID))
		}

		w, err = tokenMaster.OpenTokenWallet(&asset.TokenConfig{
			AssetID:     assetID,
			Settings:    dbWallet.Settings,
			TipChange:   tipChange,
			PeersChange: peersChange,
		})
	}
	if err != nil {
		return nil, fmt.Errorf("error opening wallet: %w", err)
	}

	wallet.Wallet = w
	wallet.parent = parent
	wallet.connector = dex.NewConnectionMaster(w)
	wallet.traits = asset.DetermineWalletTraits(w)
	atomic.StoreUint32(wallet.broadcasting, 1)
	return wallet, nil
}

// WalletState returns the *WalletState for the asset ID.
func (c *Core) WalletState(assetID uint32) *WalletState {
	c.walletMtx.Lock()
	defer c.walletMtx.Unlock()
	wallet, has := c.wallets[assetID]
	if !has {
		c.log.Tracef("wallet status requested for unknown asset %d -> %s", assetID, unbip(assetID))
		return nil
	}
	return wallet.state()
}

// assetHasActiveOrders checks whether there are any active orders or
// negotiating matches for the specified asset.
func (c *Core) assetHasActiveOrders(assetID uint32) bool {
	for _, dc := range c.dexConnections() {
		if dc.hasActiveAssetOrders(assetID) {
			return true
		}
	}
	return false
}

// walletIsActive combines assetHasActiveOrders with a check for pending
// registration fee payments.
func (c *Core) walletIsActive(assetID uint32) bool {
	if c.assetHasActiveOrders(assetID) {
		return true
	}
	for _, dc := range c.dexConnections() {
		if pf := dc.getPendingFee(); pf != nil && pf.AssetID == assetID {
			return true
		}
	}
	return false
}

// walletCheckAndNotify sets the xcWallet's synced and syncProgress fields from
// the wallet's SyncStatus result, emits a WalletStateNote, and returns the
// synced value. When synced is true, this also updates the wallet's balance,
// stores the balance in the DB, and emits a BalanceNote.
func (c *Core) walletCheckAndNotify(w *xcWallet) bool {
	synced, progress, err := w.SyncStatus()
	if err != nil {
		c.log.Errorf("Unable to get wallet/node sync status for %s: %v",
			unbip(w.AssetID), err)
		return false
	}

	w.mtx.Lock()
	wasSynced := w.synced
	w.synced = synced
	w.syncProgress = progress
	w.mtx.Unlock()

	if atomic.LoadUint32(w.broadcasting) == 1 {
		c.notify(newWalletStateNote(w.state()))
	}
	if synced && !wasSynced {
		c.updateWalletBalance(w)
		c.log.Infof("Wallet synced for asset %s", unbip(w.AssetID))
	}
	return synced
}

// startWalletSyncMonitor repeatedly calls walletCheckAndNotify on a ticker
// until it is synced. This launches the monitor goroutine, if not already
// running, and immediately returns.
func (c *Core) startWalletSyncMonitor(wallet *xcWallet) {
	// Prevent multiple sync monitors for this wallet.
	if !atomic.CompareAndSwapUint32(&wallet.monitored, 0, 1) {
		return // already monitoring
	}

	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		defer atomic.StoreUint32(&wallet.monitored, 0)
		ticker := time.NewTicker(syncTickerPeriod)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if c.walletCheckAndNotify(wallet) {
					return
				}
			case <-wallet.connector.Done():
				c.log.Warnf("%v wallet shut down before sync completed.", wallet.Info().Name)
				return
			case <-c.ctx.Done():
				return
			}
		}
	}()
}

// RescanWallet will issue a Rescan command to the wallet if supported by the
// wallet implementation. It is up to the underlying wallet backend if and how
// to implement this functionality. It may be asynchronous. Core will emit
// wallet state notifications until the rescan is complete. If force is false,
// this will check for active orders involving this asset before initiating a
// rescan. WARNING: It is ill-advised to initiate a wallet rescan with active
// orders unless as a last ditch effort to get the wallet to recognize a
// transaction needed to complete a swap.
func (c *Core) RescanWallet(assetID uint32, force bool) error {
	if !force && c.walletIsActive(assetID) {
		return newError(activeOrdersErr, "active orders or registration fee payments for %v", unbip(assetID))
	}

	wallet, err := c.connectedWallet(assetID)
	if err != nil {
		return fmt.Errorf("OpenWallet: wallet not found for %d -> %s: %w",
			assetID, unbip(assetID), err)
	}

	// Begin potentially asynchronous wallet rescan operation.
	if err = wallet.rescan(c.ctx); err != nil {
		return err
	}

	if c.walletCheckAndNotify(wallet) {
		return nil // sync done, Rescan may have by synchronous or a no-op
	}

	// Synchronization still running. Launch a status update goroutine.
	c.startWalletSyncMonitor(wallet)

	return nil
}

func (c *Core) removeWallet(assetID uint32) {
	c.walletMtx.Lock()
	defer c.walletMtx.Unlock()
	delete(c.wallets, assetID)
}

// RecoverWallet will retrieve some recovery information from the wallet,
// which may not be possible if the wallet is too corrupted, disconnect and
// destroy the old wallet, create a new one, and if the recovery information
// was retrieved from the old wallet, send this information to the new one.
// If force is false, this will check for active orders involving this
// asset before initiating a rescan. WARNING: It is ill-advised to initiate
// a wallet recovery with active orders unless the wallet db is definitely
// corrupted and even a rescan will not save it.
//
// DO NOT MAKE CONCURRENT CALLS TO THIS FUNCTION WITH THE SAME ASSET.
func (c *Core) RecoverWallet(assetID uint32, appPW []byte, force bool) error {
	crypter, err := c.encryptionKey(appPW)
	if err != nil {
		return newError(authErr, "RecoverWallet password error: %w", err)
	}
	defer crypter.Close()

	if !force {
		for _, dc := range c.dexConnections() {
			if dc.hasActiveAssetOrders(assetID) {
				return newError(activeOrdersErr, "active orders for %v", unbip(assetID))
			}
		}
	}

	oldWallet, found := c.wallet(assetID)
	if !found {
		return fmt.Errorf("RecoverWallet: wallet not found for %d -> %s: %w",
			assetID, unbip(assetID), err)
	}

	recoverer, isRecoverer := oldWallet.Wallet.(asset.Recoverer)
	if !isRecoverer {
		return errors.New("wallet is not a recoverer")
	}
	walletDef, err := walletDefinition(assetID, oldWallet.walletType)
	if err != nil {
		return err
	}
	// Unseeded wallets shouldn't implement the Recoverer interface. This
	// is just an additional check for safety.
	if !walletDef.Seeded {
		return fmt.Errorf("can only recover a seeded wallet")
	}

	dbWallet, err := c.db.Wallet(oldWallet.dbID)
	if err != nil {
		return fmt.Errorf("error retrieving DB wallet: %w", err)
	}

	seed, pw, err := c.assetSeedAndPass(assetID, crypter)
	if err != nil {
		return err
	}
	defer encode.ClearBytes(seed)
	defer encode.ClearBytes(pw)

	if recoveryCfg, err := recoverer.GetRecoveryCfg(); err != nil {
		c.log.Errorf("RecoverWallet: unable to get recovery config: %v", err)
	} else {
		// merge recoveryCfg with dbWallet.Settings
		for key, val := range recoveryCfg {
			dbWallet.Settings[key] = val
		}
	}

	// Before we pull the plug, remove the wallet from wallets map. Otherwise,
	// connectedWallet would try to connect it.
	c.removeWallet(assetID)
	oldWallet.Disconnect() // wallet now shut down and w.hookedUp == false -> connected() returns false

	if err = recoverer.Move(c.assetDataBackupDirectory(assetID)); err != nil {
		return fmt.Errorf("failed to move wallet data to backup folder: %w", err)
	}

	if err = asset.CreateWallet(assetID, &asset.CreateWalletParams{
		Type:     dbWallet.Type,
		Seed:     seed,
		Pass:     pw,
		Settings: dbWallet.Settings,
		DataDir:  c.assetDataDirectory(assetID),
		Net:      c.net,
		Logger:   c.log.SubLogger("CREATE"),
	}); err != nil {
		return fmt.Errorf("error creating wallet: %w", err)
	}

	newWallet, err := c.loadWallet(dbWallet)
	if err != nil {
		return newError(walletErr, "error loading wallet for %d -> %s: %w",
			assetID, unbip(assetID), err)
	}

	_, err = c.connectWallet(newWallet)
	if err != nil {
		return err
	}

	c.updateAssetWalletRefs(newWallet)

	err = newWallet.Unlock(crypter)
	if err != nil {
		return err
	}

	state := newWallet.state()
	c.notify(newWalletStateNote(state))

	return nil
}

// OpenWallet opens (unlocks) the wallet for use.
func (c *Core) OpenWallet(assetID uint32, appPW []byte) error {
	crypter, err := c.encryptionKey(appPW)
	if err != nil {
		return err
	}
	defer crypter.Close()
	wallet, err := c.connectedWallet(assetID)
	if err != nil {
		return fmt.Errorf("OpenWallet: wallet not found for %d -> %s: %w", assetID, unbip(assetID), err)
	}
	err = wallet.Unlock(crypter)
	if err != nil {
		return newError(walletAuthErr, "failed to unlock %s wallet: %w", unbip(assetID), err)
	}

	state := wallet.state()
	balances, err := c.updateWalletBalance(wallet)
	if err != nil {
		return err
	}
	c.log.Infof("Connected to and unlocked %s wallet. Balance available "+
		"= %d / locked = %d / locked in contracts = %d, Deposit address = %s",
		state.Symbol, balances.Available, balances.Locked, balances.ContractLocked,
		state.Address)

	go c.checkUnpaidFees(wallet)

	c.notify(newWalletStateNote(state))

	return nil
}

// CloseWallet closes the wallet for the specified asset. The wallet cannot be
// closed if there are active negotiations for the asset.
func (c *Core) CloseWallet(assetID uint32) error {
	if c.assetHasActiveOrders(assetID) {
		return fmt.Errorf("cannot lock %s wallet with active swap negotiations", unbip(assetID))
	}
	wallet, err := c.connectedWallet(assetID)
	if err != nil {
		return fmt.Errorf("wallet not found for %d -> %s: %w", assetID, unbip(assetID), err)
	}
	err = wallet.Lock(5 * time.Second)
	if err != nil {
		return err
	}

	c.notify(newWalletStateNote(wallet.state()))

	return nil
}

// ConnectWallet connects to the wallet without unlocking.
func (c *Core) ConnectWallet(assetID uint32) error {
	wallet, err := c.connectedWallet(assetID)
	if err != nil {
		return err
	}
	c.notify(newWalletStateNote(wallet.state()))
	return nil
}

// WalletSettings fetches the current wallet configuration details from the
// database.
func (c *Core) WalletSettings(assetID uint32) (map[string]string, error) {
	wallet, found := c.wallet(assetID)
	if !found {
		return nil, newError(missingWalletErr, "%d -> %s wallet not found", assetID, unbip(assetID))
	}
	// Get the settings from the database.
	dbWallet, err := c.db.Wallet(wallet.dbID)
	if err != nil {
		return nil, codedError(dbErr, err)
	}
	return dbWallet.Settings, nil
}

// ChangeAppPass updates the application password to the provided new password
// after validating the current password.
func (c *Core) ChangeAppPass(appPW, newAppPW []byte) error {
	// Validate current password.
	if len(newAppPW) == 0 {
		return fmt.Errorf("application password cannot be empty")
	}
	creds := c.creds()
	if creds == nil {
		return fmt.Errorf("no primary credentials. Is the client initialized?")
	}

	outerCrypter, err := c.reCrypter(appPW, creds.OuterKeyParams)
	if err != nil {
		return newError(authErr, "old password error: %w", err)
	}
	innerKey, err := outerCrypter.Decrypt(creds.EncInnerKey)
	if err != nil {
		return fmt.Errorf("inner key decryption error: %w", err)
	}
	newOuterCrypter := c.newCrypter(newAppPW)
	newEncInnerKey, err := newOuterCrypter.Encrypt(innerKey)
	if err != nil {
		return fmt.Errorf("encryption error: %v", err)
	}
	newCreds := &db.PrimaryCredentials{
		EncSeed:        creds.EncSeed,
		EncInnerKey:    newEncInnerKey,
		InnerKeyParams: creds.InnerKeyParams,
		OuterKeyParams: newOuterCrypter.Serialize(),
	}

	err = c.db.SetPrimaryCredentials(newCreds)
	if err != nil {
		return fmt.Errorf("SetPrimaryCredentials error: %w", err)
	}

	c.setCredentials(newCreds)

	return nil
}

// ReconfigureWallet updates the wallet configuration settings, it also updates
// the password if newWalletPW is non-nil. Do not make concurrent calls to
// ReconfigureWallet for the same asset.
func (c *Core) ReconfigureWallet(appPW, newWalletPW []byte, form *WalletForm) error {
	crypter, err := c.encryptionKey(appPW)
	if err != nil {
		return newError(authErr, "ReconfigureWallet password error: %w", err)
	}
	defer crypter.Close()

	assetID := form.AssetID

	walletDef, err := walletDefinition(assetID, form.Type)
	if err != nil {
		return err
	}
	if walletDef.Seeded && newWalletPW != nil {
		return newError(passwordErr, "cannot set a password on a built-in(seeded) wallet")
	}

	oldWallet, found := c.wallet(assetID)
	if !found {
		return newError(missingWalletErr, "%d -> %s wallet not found",
			assetID, unbip(assetID))
	}

	oldDef, err := walletDefinition(assetID, oldWallet.walletType)
	if err != nil {
		return fmt.Errorf("failed to locate old wallet definition: %v", err)
	}
	oldDepositAddr := oldWallet.currentDepositAddress()

	dbWallet := &db.Wallet{
		Type:        form.Type,
		AssetID:     oldWallet.AssetID,
		Settings:    form.Config,
		Balance:     &db.Balance{}, // in case retrieving new balance after connect fails
		EncryptedPW: oldWallet.encPW(),
		Address:     oldDepositAddr,
	}

	storeWithBalance := func(w *xcWallet) error {
		balances, err := c.walletBalance(w)
		if err != nil {
			c.log.Warnf("Error getting balance for wallet %s: %v", unbip(assetID), err)
			// Do not fail in case this requires an unlocked wallet.
		} else {
			w.setBalance(balances)              // update xcWallet's WalletBalance
			dbWallet.Balance = balances.Balance // store the db.Balance
		}

		err = c.db.UpdateWallet(dbWallet)
		if err != nil {
			return newError(dbErr, "error saving wallet configuration: %w", err)
		}

		c.notify(newBalanceNote(assetID, balances)) // redundant with wallet config note?
		subject, details := c.formatDetails(TopicWalletConfigurationUpdated, unbip(assetID), w.address)
		c.notify(newWalletConfigNote(TopicWalletConfigurationUpdated, subject, details, db.Success, w.state()))

		return nil
	}

	clearTickGovernors := func() {
		for _, dc := range c.dexConnections() {
			for _, t := range dc.trackedTrades() {
				if t.Base() != assetID && t.Quote() != assetID {
					continue
				}
				isFromAsset := t.wallets.fromAsset.ID == assetID
				t.mtx.RLock()
				for _, m := range t.matches { // maybe range t.activeMatches()
					m.exceptionMtx.Lock()
					if m.tickGovernor != nil &&
						((m.suspectSwap && isFromAsset) || (m.suspectRedeem && !isFromAsset)) {

						m.tickGovernor.Stop()
						m.tickGovernor = nil
					}
					m.exceptionMtx.Unlock()
				}
				t.mtx.RUnlock()
			}
		}
	}

	// See if the wallet offers a quick path.
	if configurer, is := oldWallet.Wallet.(asset.LiveReconfigurer); is {
		form.Config[asset.SpecialSettingActivelyUsed] = strconv.FormatBool(c.assetHasActiveOrders(dbWallet.AssetID))
		defer delete(form.Config, asset.SpecialSettingActivelyUsed)

		if restart, err := configurer.Reconfigure(c.ctx, &asset.WalletConfig{
			Type:     form.Type,
			Settings: form.Config,
			DataDir:  c.assetDataDirectory(assetID),
		}, oldWallet.currentDepositAddress()); err != nil {
			return err
		} else if !restart {
			// Config was updated without a need to restart.
			if owns, err := oldWallet.OwnsDepositAddress(oldWallet.currentDepositAddress()); err != nil {
				return newError(walletErr, "error checking deposit address after live config update: %w", err)
			} else if !owns {
				if dbWallet.Address, err = oldWallet.refreshDepositAddress(); err != nil {
					return newError(newAddrErr, "error refreshing deposit address after live config update: %w", err)
				}
			}
			if !walletDef.Seeded && newWalletPW != nil {
				if err = c.setWalletPassword(oldWallet, newWalletPW, crypter); err != nil {
					return newError(walletAuthErr, "failed to update password: %v", err)
				}
			}
			if err = storeWithBalance(oldWallet); err != nil {
				return err
			}
			clearTickGovernors()
			c.log.Infof("%s wallet configuration updated without a restart 👍", unbip(assetID))
			return nil
		}
	}

	c.log.Infof("%s wallet configuration update will require a restart", unbip(assetID))

	var restartOnFail bool

	defer func() {
		if restartOnFail {
			if _, err := c.connectWallet(oldWallet); err != nil {
				c.log.Errorf("Failed to reconnect wallet after a failed reconfiguration attempt: %v", err)
			}
		}
	}()

	if walletDef.Seeded {
		exists, err := asset.WalletExists(assetID, form.Type, c.assetDataDirectory(assetID), form.Config, c.net)
		if err != nil {
			return newError(existenceCheckErr, "error checking wallet pre-existence: %w", err)
		}

		// The password on a seeded wallet is deterministic, based on the seed
		// itself, so if the seeded wallet of this Type for this asset already
		// exists, recompute the password from the app seed.
		var pw []byte
		if exists {
			_, pw, err = c.assetSeedAndPass(assetID, crypter)
			if err != nil {
				return newError(authErr, "error retrieving wallet password: %w", err)
			}
		} else {
			pw, err = c.createSeededWallet(assetID, crypter, form)
			if err != nil {
				return newError(createWalletErr, "error creating new %q-type %s wallet: %w", form.Type, unbip(assetID), err)
			}
		}
		dbWallet.EncryptedPW, err = crypter.Encrypt(pw)
		if err != nil {
			return fmt.Errorf("wallet password encryption error: %w", err)
		}

		if oldDef.Seeded && oldWallet.connected() {
			oldWallet.Disconnect()
			restartOnFail = true
		}
	} else if newWalletPW == nil && oldDef.Seeded {
		// If we're switching from a seeded wallet to a non-seeded wallet and no
		// password was provided, use empty string = wallet not encrypted.
		newWalletPW = []byte{}
	}

	// Reload the wallet with the new settings.
	wallet, err := c.loadWallet(dbWallet)
	if err != nil {
		return newError(walletErr, "error loading wallet for %d -> %s: %w",
			assetID, unbip(assetID), err)
	}

	// Block PeersChange until we know this wallet is ready.
	atomic.StoreUint32(wallet.broadcasting, 0)
	var success bool
	defer func() {
		if success {
			atomic.StoreUint32(wallet.broadcasting, 1)
			c.walletCheckAndNotify(wallet)
		}
	}()

	// Must connect to ensure settings are good. This comes before
	// setWalletPassword since it would use connectAndUpdateWallet, which
	// performs additional deposit address validation and balance updates that
	// are redundant with the rest of this function.
	dbWallet.Address, err = c.connectWallet(wallet)
	if err != nil {
		return err
	}

	// If there are active trades, make sure they can be settled by the
	// keys held within the new wallet.
	sameWallet := func() error {
		if c.walletIsActive(assetID) {
			owns, err := wallet.OwnsDepositAddress(oldDepositAddr)
			if err != nil {
				return err
			}
			if !owns {
				return errors.New("new wallet in active use does not own the old deposit address. abandoning configuration update")
			}
		}
		return nil
	}
	if err := sameWallet(); err != nil {
		wallet.Disconnect()
		return newError(walletErr, "new wallet cannot be used with current active trades: %w", err)
	}

	// If newWalletPW is non-nil, update the wallet's password.
	if newWalletPW != nil { // includes empty non-nil slice
		err = c.setWalletPassword(wallet, newWalletPW, crypter)
		if err != nil {
			wallet.Disconnect()
			return err
		}
		// Update dbWallet so db.UpdateWallet below reflects the new password.
		dbWallet.EncryptedPW = make([]byte, len(wallet.encPass))
		copy(dbWallet.EncryptedPW, wallet.encPass)
	} else if oldWallet.locallyUnlocked() {
		// If the password was not changed, carry over any cached password
		// regardless of backend lock state. loadWallet already copied encPW, so
		// this will decrypt pw rather than actually copying it, and it will
		// ensure the backend is also unlocked.
		err := wallet.Unlock(crypter) // decrypt encPW if set and unlock the backend
		if err != nil {
			wallet.Disconnect()
			return newError(walletAuthErr, "wallet successfully connected, but failed to unlock. "+
				"reconfiguration not saved: %w", err)
		}
	}

	if err = storeWithBalance(wallet); err != nil {
		wallet.Disconnect()
		return err
	}

	c.updateAssetWalletRefs(wallet)

	restartOnFail = false
	success = true

	if oldWallet.connected() {
		// NOTE: Cannot lock the wallet backend because it may be the same as
		// the one just connected.
		go oldWallet.Disconnect()
	}

	// reReserveFunding is likely a no-op because of the walletIsActive check
	// above, and because of the way current LiveReconfigurers are implemented.
	// For forward compatibility though, if a LiveReconfigurer with active
	// orders indicates restart and the new wallet still owns the keys, we can
	// end up here and we need to re-reserve.
	go c.reReserveFunding(wallet)

	clearTickGovernors()

	return nil
}

// updateAssetWalletRefs sets all references of an asset's wallet to newWallet.
func (c *Core) updateAssetWalletRefs(newWallet *xcWallet) {
	assetID := newWallet.AssetID
	for _, dc := range c.dexConnections() {
		dc.tradeMtx.RLock()
		for _, tracker := range dc.trades {
			tracker.mtx.Lock()
			if tracker.wallets.fromWallet.AssetID == assetID {
				tracker.wallets.fromWallet = newWallet
			} else if tracker.wallets.toWallet.AssetID == assetID {
				tracker.wallets.toWallet = newWallet
			}
			tracker.mtx.Unlock()
		}
		dc.tradeMtx.RUnlock()
	}

	c.walletMtx.Lock()
	c.wallets[assetID] = newWallet
	c.walletMtx.Unlock()
}

// SetWalletPassword updates the (encrypted) password for the wallet. Returns
// passwordErr if provided newPW is nil. The wallet will be connected if it is
// not already.
func (c *Core) SetWalletPassword(appPW []byte, assetID uint32, newPW []byte) error {
	// Ensure newPW isn't nil.
	if newPW == nil {
		return newError(passwordErr, "SetWalletPassword password can't be nil")
	}

	// Check the app password and get the crypter.
	crypter, err := c.encryptionKey(appPW)
	if err != nil {
		return newError(authErr, "SetWalletPassword password error: %w", err)
	}
	defer crypter.Close()

	// Check that the specified wallet exists.
	c.walletMtx.Lock()
	defer c.walletMtx.Unlock()
	wallet, found := c.wallets[assetID]
	if !found {
		return newError(missingWalletErr, "wallet for %s (%d) is not known", unbip(assetID), assetID)
	}

	// Set new password, connecting to it if necessary to verify. It is left
	// connected since it is in the wallets map.
	return c.setWalletPassword(wallet, newPW, crypter)
}

// setWalletPassword updates the (encrypted) password for the wallet.
func (c *Core) setWalletPassword(wallet *xcWallet, newPW []byte, crypter encrypt.Crypter) error {
	walletDef, err := walletDefinition(wallet.AssetID, wallet.walletType)
	if err != nil {
		return err
	}
	if walletDef.Seeded {
		return newError(passwordErr, "cannot set a password on a seeded wallet")
	}

	// Connect if necessary.
	wasConnected := wallet.connected()
	if !wasConnected {
		if err := c.connectAndUpdateWallet(wallet); err != nil {
			return newError(connectionErr, "SetWalletPassword connection error: %w", err)
		}
	}

	wasUnlocked := wallet.unlocked()
	newPasswordSet := len(newPW) > 0 // excludes empty but non-nil

	// Check that the new password works. If the new password is empty, skip
	// this step, since an empty password signifies an unencrypted wallet.
	// TODO: find a way to verify that the wallet actually is unencrypted or
	// otherwise does not require a password. Perhaps an
	// asset.Wallet.RequiresPassword wallet method?
	if newPasswordSet {
		// Encrypt password if it's not an empty string.
		encNewPW, err := crypter.Encrypt(newPW)
		if err != nil {
			return newError(encryptionErr, "encryption error: %w", err)
		}
		err = wallet.Wallet.Unlock(newPW)
		if err != nil {
			return newError(authErr,
				"setWalletPassword unlocking wallet error, is the new password correct?: %w", err)
		}
		wallet.setEncPW(encNewPW)
	} else {
		wallet.setEncPW(nil)
	}

	err = c.db.SetWalletPassword(wallet.dbID, wallet.encPW())
	if err != nil {
		return codedError(dbErr, err)
	}

	// Re-lock the wallet if it was previously locked.
	if !wasUnlocked {
		if err = wallet.Lock(2 * time.Second); err != nil {
			c.log.Warnf("Unable to relock %s wallet: %v", unbip(wallet.AssetID), err)
		}
	}

	// Do not disconnect because the Wallet may not allow reconnection.

	subject, details := c.formatDetails(TopicWalletPasswordUpdated, unbip(wallet.AssetID))
	c.notify(newWalletConfigNote(TopicWalletPasswordUpdated, subject, details, db.Success, wallet.state()))

	return nil
}

// NewDepositAddress retrieves a new deposit address from the specified asset's
// wallet, saves it to the database, and emits a notification.
func (c *Core) NewDepositAddress(assetID uint32) (string, error) {
	w, exists := c.wallet(assetID)
	if !exists {
		return "", newError(missingWalletErr, "no wallet found for %s", unbip(assetID))
	}

	// Retrieve a fresh deposit address.
	addr, err := w.refreshDepositAddress()
	if err != nil {
		return "", err
	}

	if err = c.storeDepositAddress(w.dbID, addr); err != nil {
		return "", err
	}

	// Update wallet state in the User data struct and emit a WalletStateNote.
	c.notify(newWalletStateNote(w.state()))

	return addr, nil
}

// AutoWalletConfig attempts to load setting from a wallet package's
// asset.WalletInfo.DefaultConfigPath. If settings are not found, an empty map
// is returned.
func (c *Core) AutoWalletConfig(assetID uint32, walletType string) (map[string]string, error) {
	walletDef, err := walletDefinition(assetID, walletType)
	if err != nil {
		return nil, err
	}

	if walletDef.DefaultConfigPath == "" {
		return nil, fmt.Errorf("no config path found for %s wallet, type %q", unbip(assetID), walletType)
	}

	settings, err := config.Parse(walletDef.DefaultConfigPath)
	c.log.Infof("%d %s configuration settings loaded from file at default location %s", len(settings), unbip(assetID), walletDef.DefaultConfigPath)
	if err != nil {
		c.log.Debugf("config.Parse could not load settings from default path: %v", err)
		return make(map[string]string), nil
	}
	return settings, nil
}

func (c *Core) isRegistered(host string) bool {
	c.connMtx.RLock()
	dc, found := c.conns[host]
	c.connMtx.RUnlock()
	if !found {
		return false
	}

	dc.acct.keyMtx.RLock()
	defer dc.acct.keyMtx.RUnlock()
	return len(dc.acct.encKey) > 0 // should be for all in conns map, but check
}

// tempDexConnection creates an unauthenticated dexConnection. The caller must
// dc.connMaster.Disconnect when done with the connection.
func (c *Core) tempDexConnection(dexAddr string, certI interface{}) (*dexConnection, error) {
	host, err := addrHost(dexAddr)
	if err != nil {
		return nil, newError(addressParseErr, "error parsing address: %w", err)
	}
	cert, err := parseCert(host, certI, c.net)
	if err != nil {
		return nil, newError(fileReadErr, "failed to parse certificate: %w", err)
	}
	if c.isRegistered(host) {
		return nil, newError(dupeDEXErr, "already registered at %s", dexAddr)
	}

	return c.connectDEX(&db.AccountInfo{
		Host: host,
		Cert: cert,
	}, true)
}

// GetDEXConfig creates a temporary connection to the specified DEX Server and
// fetches the full exchange config. The connection is closed after the config
// is retrieved. An error is returned if user is already registered to the DEX
// since a DEX connection is already established and the config is accessible
// via the User or Exchanges methods. A TLS certificate, certI, can be provided
// as either a string filename, or []byte file contents.
func (c *Core) GetDEXConfig(dexAddr string, certI interface{}) (*Exchange, error) {
	dc, err := c.tempDexConnection(dexAddr, certI)
	if dc != nil {
		// Stop (re)connect loop, which may be running even if err != nil.
		defer dc.connMaster.Disconnect()
	}
	if err != nil {
		return nil, err
	}

	// Since connectDEX succeeded, we have the server config. exchangeInfo is
	// guaranteed to return an *Exchange with full asset and market info.
	return dc.exchangeInfo(), nil
}

// discoverAccount attempts to identify existing accounts at the connected DEX.
// The dexConnection.acct struct will have its encKey, privKey, and id fields
// set. If the bool is true, the account will have been recorded in the DB, and
// the isPaid and feeCoin fields of the account set. If the bool is false, the
// account is not paid and the user should register.
func (c *Core) discoverAccount(dc *dexConnection, crypter encrypt.Crypter) (bool, error) {
	if dc.acct.dexPubKey == nil {
		return false, fmt.Errorf("dex server does not support HD key accounts")
	}

	// Setup our account keys and attempt to authorize with the DEX.
	creds := c.creds()

	// Start at key index 0 and attempt to authorize accounts until either (1)
	// the server indicates the account is not found, and we return paid=false
	// to signal a new account should be registered, or (2) an account is found
	// that is not suspended, and we return with paid=true after storing the
	// discovered account and promoting it to a persistent connection. In this
	// process, we will increment the key index and try again whenever the
	// connect response indicates a suspended account is found. This instance of
	// Core lacks any order or match history for this dex to complete any active
	// swaps that might exist for a suspended account, so the user had better
	// have another instance with this data if they hope to recover those swaps.
	var keyIndex uint32
	for {
		err := dc.acct.setupCryptoV2(creds, crypter, keyIndex)
		if err != nil {
			return false, newError(acctKeyErr, "setupCryptoV2 error: %w", err)
		}

		// Discover the account by attempting a 'connect' (authorize) request.
		err = c.authDEX(dc)
		if err != nil {
			var mErr *msgjson.Error
			if errors.As(err, &mErr) && (mErr.Code == msgjson.AccountNotFoundError ||
				mErr.Code == msgjson.UnpaidAccountError) {
				if mErr.Code == msgjson.UnpaidAccountError {
					c.log.Warnf("Detected existing but unpaid account! Register " +
						"with the same credentials to complete registration with " +
						"the previously-assigned fee address and asset ID.")
				}
				return false, nil // all good, just go register now
			}
			return false, newError(authErr, "unexpected authDEX error: %w", err)
		}
		if dc.acct.isSuspended {
			c.log.Infof("HD account key for %s was reported as suspended. Deriving another account key.", dc.acct.host)
			keyIndex++
			time.Sleep(200 * time.Millisecond) // don't hammer
			continue
		}

		break // great, the account at this key index is paid and ready
	}

	// Actual fee asset ID and coin are unknown, but paid.
	dc.acct.isPaid = true
	dc.acct.feeCoin = []byte("DUMMY COIN")
	dc.acct.feeAssetID = 42

	err := c.db.CreateAccount(&db.AccountInfo{
		Host:         dc.acct.host,
		Cert:         dc.acct.cert,
		DEXPubKey:    dc.acct.dexPubKey,
		EncKeyV2:     dc.acct.encKey,
		LegacyEncKey: nil,
		FeeAssetID:   dc.acct.feeAssetID,
		FeeCoin:      dc.acct.feeCoin,
		// Paid set with AccountPaid below.
	})
	if err != nil {
		return false, fmt.Errorf("error saving restored account: %w", err)
	}

	err = c.db.AccountPaid(&db.AccountProof{
		Host:  dc.acct.host,
		Stamp: 54321,
		Sig:   []byte("RECOVERY SIGNATURE"),
	})
	if err != nil {
		return false, fmt.Errorf("error marking recovered account as paid: %w", err)
	}

	return true, nil // great, just stay connected
}

// dexWithPubKeyExists checks whether or not there is a non-disabled account
// for a dex that has pubKey.
func (c *Core) dexWithPubKeyExists(pubKey *secp256k1.PublicKey) (bool, string) {
	for _, dc := range c.dexConnections() {
		if dc.acct.dexPubKey == nil {
			continue
		}

		if dc.acct.dexPubKey.IsEqual(pubKey) {
			return true, dc.acct.host
		}
	}

	return false, ""
}

// upgradeConnection promotes a temporary dex connection and starts listening
// to the messages it receives.
func (c *Core) upgradeConnection(dc *dexConnection) {
	if atomic.CompareAndSwapUint32(&dc.reportingConnects, 0, 1) {
		c.wg.Add(1)
		go c.listen(dc)
		go dc.subPriceFeed()
	}
	c.connMtx.Lock()
	c.conns[dc.acct.host] = dc
	c.connMtx.Unlock()
}

// DiscoverAccount fetches the DEX server's config, and if the server supports
// the new deterministic account derivation scheme by providing its public key
// in the config response, DiscoverAccount also checks if the account is already
// paid. If the returned paid value is true, the account is ready for immediate
// use. If paid is false, Register should be used to complete the registration.
// For an older server that does not provide its pubkey in the config response,
// paid will always be false and the user should proceed to use Register.
//
// The purpose of DiscoverAccount is existing account discovery when the client
// has been restored from seed. As such, DiscoverAccount is not strictly necessary
// to register on a DEX, and Register may be called directly, although it requires
// the expected fee amount as an additional input and it will pay the fee if the
// account is not discovered and paid.
func (c *Core) DiscoverAccount(dexAddr string, appPW []byte, certI interface{}) (*Exchange, bool, error) {
	if !c.IsInitialized() {
		return nil, false, fmt.Errorf("cannot register DEX because app has not been initialized")
	}

	host, err := addrHost(dexAddr)
	if err != nil {
		return nil, false, newError(addressParseErr, "error parsing address: %w", err)
	}

	crypter, err := c.encryptionKey(appPW)
	if err != nil {
		return nil, false, codedError(passwordErr, err)
	}
	defer crypter.Close()

	var ready bool
	dc, err := c.tempDexConnection(host, certI)
	if dc != nil { // (re)connect loop may be running even if err != nil
		defer func() {
			// Either disconnect or promote this connection.
			if !ready {
				dc.connMaster.Disconnect()
				return
			}

			c.upgradeConnection(dc)
		}()
	}
	if err != nil {
		return nil, false, err
	}

	// Older DEX server. We won't allow registering without an HD account key,
	// but discovery can conclude we do not have an HD account with this DEX.
	if dc.acct.dexPubKey == nil {
		return dc.exchangeInfo(), false, nil
	}

	// Don't allow registering for another dex with the same pubKey. There can only
	// be one dex connection per pubKey. UpdateDEXHost must be called to connect to
	// the same dex using a different host name.
	exists, host := c.dexWithPubKeyExists(dc.acct.dexPubKey)
	if exists {
		return nil, false,
			fmt.Errorf("the dex at %v is the same dex as %v. Use Update Host to switch host names", host, dexAddr)
	}

	// Setup our account keys and attempt to authorize with the DEX.
	paid, err := c.discoverAccount(dc, crypter)
	if err != nil {
		return nil, false, err
	}
	if !paid {
		return dc.exchangeInfo(), false, nil // all good, just go register now
	}

	ready = true // do not disconnect

	return dc.exchangeInfo(), true, nil
}

// EstimateRegistrationTxFee provides an estimate for the tx fee needed to
// pay the registration fee for a certain asset. The dex host is required
// because the dex server is used as a fallback to determine the current
// fee rate in case the client wallet is unable to do it.
func (c *Core) EstimateRegistrationTxFee(host string, certI interface{}, assetID uint32) (uint64, error) {
	wallet, err := c.connectedWallet(assetID)
	if err != nil {
		return 0, err
	}

	var rate uint64
	if rater, is := wallet.Wallet.(asset.FeeRater); is {
		rate = rater.FeeRate()
	}

	if rate == 0 {
		dc, err := c.tempDexConnection(host, certI)
		if dc != nil {
			// Stop (re)connect loop, which may be running even if err != nil.
			defer dc.connMaster.Disconnect()
		}
		if err == nil {
			rate = dc.fetchFeeRate(assetID)
		} else {
			c.log.Warnf("failed to connect to dex: %v", err)
		}
	}

	txFee := wallet.EstimateRegistrationTxFee(rate)
	return txFee, nil
}

// Register registers an account with a new DEX. If an error occurs while
// fetching the DEX configuration or creating the fee transaction, it will be
// returned immediately.
// A thread will be started to wait for the requisite confirmations and send
// the fee notification to the server. Any error returned from that thread is
// sent as a notification.
func (c *Core) Register(form *RegisterForm) (*RegisterResult, error) {
	// Make sure the app has been initialized. This condition would error when
	// attempting to retrieve the encryption key below as well, but the
	// messaging may be confusing.
	if !c.IsInitialized() {
		return nil, fmt.Errorf("cannot register DEX because app has not been initialized")
	}

	// Check the app password.
	crypter, err := c.encryptionKey(form.AppPass)
	if err != nil {
		return nil, codedError(passwordErr, err)
	}
	defer crypter.Close()
	if form.Addr == "" {
		return nil, newError(emptyHostErr, "no dex address specified")
	}
	host, err := addrHost(form.Addr)
	if err != nil {
		return nil, newError(addressParseErr, "error parsing address: %w", err)
	}
	if c.isRegistered(host) {
		return nil, newError(dupeDEXErr, "already registered at %s", form.Addr)
	}

	// Default to using DCR unless specified.
	regFeeAssetID := uint32(42)
	if form.Asset != nil {
		regFeeAssetID = *form.Asset
	}
	regFeeAssetSymbol := dex.BipIDSymbol(regFeeAssetID)

	wallet, err := c.connectedWallet(regFeeAssetID)
	if err != nil {
		// Wrap the error from connectedWallet, a core.Error coded as
		// missingWalletErr or connectWalletErr.
		return nil, fmt.Errorf("cannot connect to %s wallet to pay fee: %w", regFeeAssetSymbol, err)
	}

	if !wallet.unlocked() {
		err = wallet.Unlock(crypter)
		if err != nil {
			return nil, newError(walletAuthErr, "failed to unlock %s wallet: %w", unbip(wallet.AssetID), err)
		}
	}

	cert, err := parseCert(host, form.Cert, c.net)
	if err != nil {
		return nil, newError(fileReadErr, "failed to read certificate file from %s: %w", cert, err)
	}

	dc, err := c.connectDEX(&db.AccountInfo{
		Host: host,
		Cert: cert,
	})
	if err != nil {
		if dc != nil {
			// Stop (re)connect loop, which may be running even if err != nil.
			dc.connMaster.Disconnect()
		}
		return nil, codedError(connectionErr, err)
	}

	// close the connection to the dex server if the registration fails.
	var registrationComplete bool
	defer func() {
		if !registrationComplete {
			dc.connMaster.Disconnect()
		}
	}()

	// Ensure this DEX supports this asset for registration fees, and get the
	// required confirmations and fee amount.
	dc.cfgMtx.RLock()
	feeAsset, supported := dc.cfg.RegFees[regFeeAssetSymbol]
	dc.cfgMtx.RUnlock()
	if !supported || feeAsset == nil {
		return nil, newError(assetSupportErr, "dex server does not accept registration fees in asset %q", regFeeAssetSymbol)
	}
	if feeAsset.ID != regFeeAssetID {
		return nil, newError(assetSupportErr, "reported asset ID %d does not match requested %d (%s)",
			feeAsset.ID, regFeeAssetID, regFeeAssetSymbol)
	}
	reqConfs := feeAsset.Confs
	// TODO: basic sanity check on required confirms, e.g. > 1000, but asset-specific

	paid, err := c.discoverAccount(dc, crypter)
	if err != nil {
		return nil, err
	}
	if paid {
		registrationComplete = true
		// The listen goroutine is already running, now track the conn.
		c.connMtx.Lock()
		c.conns[dc.acct.host] = dc
		c.connMtx.Unlock()

		return &RegisterResult{FeeID: hex.EncodeToString(dc.acct.feeCoin), ReqConfirms: 0}, nil
	}
	// dc.acct is now configured with encKey, privKey, and id for a new
	// (unregistered) account.

	// Before we do the 'register' request, make sure we have sufficient funds.
	balance, err := wallet.Balance()
	if err != nil {
		return nil, newError(walletErr, "unable to retrieve wallet balance for %v: %w",
			regFeeAssetSymbol, err)
	}
	// Just avail==required is not sufficient because of net fees, although that
	// is actually not true for degen tokens so this will need adjusting.
	if balance.Available <= feeAsset.Amt {
		// TODO: Use an asset-specific network fee source e.g.
		// (*ExchangeWallet).EstimateRegistrationTxFee.
		return nil, newError(walletBalanceErr, "insufficient balance for fee %v, have %v, "+
			"but need fee amount plus network fees", feeAsset.Amt, balance.Available)
	}

	// Make the register request to the server for fee payment details.
	regRes, paid, suspended, err := c.register(dc, feeAsset.ID)
	if err != nil {
		return nil, err
	}
	if paid { // would have gotten this from discoverAccount
		c.connMtx.Lock()
		c.conns[dc.acct.host] = dc
		c.connMtx.Unlock()

		registrationComplete = true
		// register already promoted the connection
		return &RegisterResult{FeeID: hex.EncodeToString(dc.acct.feeCoin), ReqConfirms: 0}, nil
	}
	if suspended { // would have gotten this from discoverAccount
		return nil, fmt.Errorf("unexpectedly tried to register a suspended account - try again")
	}

	if err := dc.acct.unlock(crypter); err != nil { // should already be unlocked
		return nil, newError(authErr, "failed to unlock account: %w", err)
	}

	// Check that the fee is non-zero.
	fee := feeAsset.Amt // expected amount according to DEX config
	if regRes.Fee == 0 {
		return nil, newError(zeroFeeErr, "zero registration fees not allowed")
	}
	if regRes.Fee != fee {
		return nil, newError(feeMismatchErr, "DEX 'register' result fee doesn't match the 'config' value. %d != %d", regRes.Fee, fee)
	}
	if regRes.Fee != form.Fee {
		return nil, newError(feeMismatchErr, "registration fee provided to Register does not match the DEX registration fee. %d != %d", form.Fee, regRes.Fee)
	}

	// Pay the registration fee.
	c.log.Infof("Attempting registration fee payment to %s, account ID %v, of %d units of %s. "+
		"Do NOT manually send funds to this address even if this fails.",
		regRes.Address, dc.acct.id, regRes.Fee, regFeeAssetSymbol)
	feeRate := c.feeSuggestionAny(feeAsset.ID, dc)
	coin, err := wallet.Send(regRes.Address, regRes.Fee, feeRate)
	if err != nil {
		return nil, newError(feeSendErr, "error paying registration fee: %w", err)
	}

	// Set the dexConnection account fields and save account info to db.
	dc.acct.feeCoin = coin.ID()
	dc.acct.feeAssetID = feeAsset.ID

	// Registration complete.
	registrationComplete = true
	c.connMtx.Lock()
	c.conns[host] = dc
	c.connMtx.Unlock()

	err = c.db.CreateAccount(&db.AccountInfo{
		Host:       dc.acct.host,
		Cert:       dc.acct.cert,
		DEXPubKey:  dc.acct.dexPubKey,
		EncKeyV2:   dc.acct.encKey,
		FeeAssetID: dc.acct.feeAssetID,
		FeeCoin:    dc.acct.feeCoin,
		// Paid set with AccountPaid after notifyFee.
	})
	if err != nil {
		c.log.Errorf("error saving account: %v\n", err)
		// Don't abandon registration. The fee is already paid.
	}

	c.updateAssetBalance(regFeeAssetID)

	subject, details := c.formatDetails(TopicFeePaymentInProgress, reqConfs, dc.acct.host)
	c.notify(newFeePaymentNote(TopicFeePaymentInProgress, subject, details, db.Success, dc.acct.host))

	// Set up the coin waiter, which waits for the required number of
	// confirmations to notify the DEX and establish an authenticated
	// connection.
	c.verifyRegistrationFee(wallet.AssetID, dc, coin.ID(), 0, reqConfs)
	res := &RegisterResult{FeeID: coin.String(), ReqConfirms: uint16(reqConfs)}
	return res, nil
}

// register submits a new 'register' request to the server.
// The result of this registration attempt will be returned to enable follow up
// action if this is a fresh registration with the server or if the account
// already exists but is suspended.
// The registration result is nil if the account exists and the fee is paid. If
// the account exists and fee is paid, the account is restored, the dc is auth'ed
// and added to the conns map and a goroutine is started to listen for server
// messages.
func (c *Core) register(dc *dexConnection, assetID uint32) (regRes *msgjson.RegisterResult, paid, suspended bool, err error) {
	if dc.acct.privKey == nil {
		return nil, false, false, fmt.Errorf("account identity not configured and unlocked")
	}
	acctPubKey := dc.acct.privKey.PubKey().SerializeCompressed()
	dexReg := &msgjson.Register{
		PubKey: acctPubKey,
		Time:   uint64(time.Now().UnixMilli()),
		Asset:  &assetID,
	}
	regRes = new(msgjson.RegisterResult)
	err = dc.signAndRequest(dexReg, msgjson.RegisterRoute, regRes, DefaultResponseTimeout)
	if err == nil {
		// If we already had a DEX pubkey from the 'config' response, check for
		// discrepancies.
		if !bytes.Equal(dc.acct.dexPubKey.SerializeCompressed(), regRes.DEXPubKey) {
			return nil, false, false, fmt.Errorf("different pubkeys reported by dex in 'config' and 'register' responses")
		}
		// Insert our pubkey back into the register result since it is excluded
		// from the JSON serialization.
		regRes.ClientPubKey = acctPubKey
		// Check the DEX server's signature.
		msg := regRes.Serialize()
		err = checkSigS256(msg, regRes.DEXPubKey, regRes.Sig)
		if err != nil {
			return nil, false, false, newError(signatureErr, "%s pubkey error: %w", dc.acct.host, err)
		}
		// Compatibility with older servers that do not include asset ID.
		if regRes.AssetID != nil && assetID != *regRes.AssetID {
			return nil, false, false, fmt.Errorf("requested fee payment with asset %d, got details for %d", assetID, regRes.AssetID)
		}
		if regRes.AssetID == nil && assetID != 42 {
			return nil, false, false, fmt.Errorf("server only supports registration with DCR")
		}
		// Fresh account registration success.
		return regRes, false, false, nil
	}

	// Registration error could be AccountExistsError or AccountSuspendedError.
	// These cases are mostly dead code if discoverAccount is used first.
	var msgErr *msgjson.Error
	if errors.As(err, &msgErr) {
		switch msgErr.Code {
		case msgjson.AccountSuspendedError:
			return regRes, false, true, nil

		case msgjson.AccountExistsError:
			// This is now account recovery, which is great news since we don't
			// have to pay the fee. Server provides fee coin as the error message.
			c.log.Infof("%s is reporting that this account already exists. Skipping fee payment.", dc.acct.host)
			dc.acct.feeAssetID = 42 // actual asset ID unknown, but paid
			dc.acct.feeCoin, err = hex.DecodeString(msgErr.Message)
			if err != nil || len(dc.acct.feeCoin) == 0 { // err may be nil but feeCoin is empty, e.g. if msgErr.Message == ""
				c.log.Errorf("Failed to decode fee coin from pre-paid account info message = %q, err = %v", msgErr.Message, err)
				dc.acct.feeCoin = []byte("DUMMY COIN")
			} else {
				cid, err := asset.DecodeCoinID(dc.acct.feeAssetID, dc.acct.feeCoin)
				if err != nil {
					c.log.Errorf("Failed to decode coin ID for pre-paid account from feeCoin = %x, err = %v", dc.acct.feeCoin, err)
				} else {
					c.log.Infof("Recovered paid account for %s. Fee previously paid with %s", dc.acct.host, cid)
				}
			}

			err = c.db.CreateAccount(&db.AccountInfo{
				Host:       dc.acct.host,
				Cert:       dc.acct.cert,
				DEXPubKey:  dc.acct.dexPubKey,
				EncKeyV2:   dc.acct.encKey,
				FeeAssetID: dc.acct.feeAssetID,
				FeeCoin:    dc.acct.feeCoin,
				// Paid set with AccountPaid below.
			})
			if err != nil {
				// Shouldn't let the client trade with this server if we can't store
				// the acct details to DB. Since this is not a fresh registration with
				// a randomly generated acct key, the client can restore this account
				// after fixing any db issues before proceeding to trade.
				return nil, true, false, fmt.Errorf("error saving restored account: %w", err)
			}

			err = c.db.AccountPaid(&db.AccountProof{
				Host:  dc.acct.host,
				Stamp: 54321,
				Sig:   []byte("RECOVERY SIGNATURE"),
			})
			if err != nil {
				return nil, true, false, fmt.Errorf("error marking recovered account as paid: %w", err)
			}

			dc.acct.isPaid = true

			err = c.authDEX(dc)
			if err != nil {
				return nil, true, false, fmt.Errorf("error authorizing pre-paid account: %w", err)
			}

			return nil, true, false, nil
		}
	}

	return nil, false, false, codedError(registerErr, err)
}

// verifyRegistrationFee waits the required amount of confirmations for the
// registration fee payment. Once the requirement is met the server is notified.
// If the server acknowledgment is successful, the account is set as 'paid' in
// the database. Notifications about confirmations increase, errors and success
// events are broadcasted to all subscribers.
func (c *Core) verifyRegistrationFee(assetID uint32, dc *dexConnection, coinID []byte, confs, reqConfs uint32) {
	dc.setPendingFee(assetID, confs)

	trigger := func() (bool, error) {
		// We already know the wallet is there by now.
		wallet, _ := c.wallet(assetID)
		confs, err := wallet.RegFeeConfirmations(c.ctx, coinID)
		if err != nil && !errors.Is(err, asset.CoinNotFoundError) {
			return false, fmt.Errorf("Error getting confirmations for %s: %w", coinIDString(wallet.AssetID, coinID), err)
		}

		if confs < reqConfs {
			dc.setPendingFee(assetID, confs)
			subject, details := c.formatDetails(TopicRegUpdate, confs, reqConfs)
			c.notify(newFeePaymentNoteWithConfirmations(TopicRegUpdate, subject, details, db.Data, assetID, confs, dc.acct.host))
		}

		return confs >= reqConfs, nil
	}

	c.wait(coinID, assetID, trigger, func(err error) {
		wallet, _ := c.wallet(assetID)
		c.log.Debugf("Registration fee txn %s now has %d confirmations.", coinIDString(wallet.AssetID, coinID), reqConfs)
		defer func() {
			if err != nil {
				subject, details := c.formatDetails(TopicFeePaymentError, dc.acct.host, err)
				c.notify(newFeePaymentNote(TopicFeePaymentError, subject, details, db.ErrorLevel, dc.acct.host))
			} else {
				dc.clearPendingFee()
				subject, details := c.formatDetails(TopicAccountRegistered, dc.acct.host)
				c.notify(newFeePaymentNote(TopicAccountRegistered, subject, details, db.Success, dc.acct.host))
			}
		}()
		if err != nil {
			return
		}
		c.log.Infof("Notifying dex %s of fee payment.", dc.acct.host)
		err = c.notifyFee(dc, coinID)
		if err != nil {
			return
		}
		dc.acct.markFeePaid()
		err = c.authDEX(dc)
		if err != nil {
			c.log.Errorf("fee paid, but failed to authenticate connection to %s: %v", dc.acct.host, err)
		}
	})

}

// IsInitialized checks if the app is already initialized.
func (c *Core) IsInitialized() bool {
	c.credMtx.RLock()
	defer c.credMtx.RUnlock()
	return c.credentials != nil
}

// InitializeClient sets the initial app-wide password and app seed for the
// client. The seed argument should be left nil unless restoring from seed.
func (c *Core) InitializeClient(pw, restorationSeed []byte) error {
	if c.IsInitialized() {
		return fmt.Errorf("already initialized, login instead")
	}

	_, creds, err := c.generateCredentials(pw, restorationSeed)
	if err != nil {
		return err
	}

	err = c.db.SetPrimaryCredentials(creds)
	if err != nil {
		return fmt.Errorf("SetPrimaryCredentials error: %w", err)
	}

	freshSeed := len(restorationSeed) == 0
	if freshSeed {
		now := uint64(time.Now().Unix())
		err = c.db.SetSeedGenerationTime(now)
		if err != nil {
			return fmt.Errorf("SetSeedGenerationTime error: %w", err)
		}
		c.seedGenerationTime = now
	}

	c.setCredentials(creds)

	if len(restorationSeed) == 0 {
		subject, details := c.formatDetails(TopicSeedNeedsSaving)
		c.notify(newSecurityNote(TopicSeedNeedsSaving, subject, details, db.Success))
	}

	return nil
}

// ExportSeed exports the application seed.
func (c *Core) ExportSeed(pw []byte) ([]byte, error) {
	crypter, err := c.encryptionKey(pw)
	if err != nil {
		return nil, fmt.Errorf("ExportSeed password error: %w", err)
	}
	defer crypter.Close()

	creds := c.creds()
	if creds == nil {
		return nil, fmt.Errorf("no v2 credentials stored")
	}

	seed, err := crypter.Decrypt(creds.EncSeed)
	if err != nil {
		return nil, fmt.Errorf("app seed decryption error: %w", err)
	}

	return seed, nil
}

// generateCredentials generates a new set of *PrimaryCredentials. The
// credentials are not stored to the database. A restoration seed can be
// provided, otherwise should be nil.
func (c *Core) generateCredentials(pw, seed []byte) (encrypt.Crypter, *db.PrimaryCredentials, error) {
	if len(pw) == 0 {
		return nil, nil, fmt.Errorf("empty password not allowed")
	}

	// Generate an inner key and it's Crypter.
	innerKey := encode.RandomBytes(32)
	innerCrypter := c.newCrypter(innerKey)

	// Generate the outer key.
	outerCrypter := c.newCrypter(pw)
	encInnerKey, err := outerCrypter.Encrypt(innerKey)
	if err != nil {
		return nil, nil, fmt.Errorf("inner key encryption error: %w", err)
	}

	// Generate a seed to use as the root for all future key generation.
	const seedLen = 64
	if len(seed) == 0 {
		seed = encode.RandomBytes(seedLen)
	} else if len(seed) != seedLen {
		return nil, nil, fmt.Errorf("invalid seed length %d. expected %d", len(seed), seedLen)
	}
	defer encode.ClearBytes(seed)

	encSeed, err := innerCrypter.Encrypt(seed)
	if err != nil {
		return nil, nil, fmt.Errorf("client seed encryption error: %w", err)
	}

	creds := &db.PrimaryCredentials{
		EncSeed:        encSeed,
		EncInnerKey:    encInnerKey,
		InnerKeyParams: innerCrypter.Serialize(),
		OuterKeyParams: outerCrypter.Serialize(),
	}

	return innerCrypter, creds, nil
}

// Login logs the user in, decrypting the account keys for all known DEXes.
func (c *Core) Login(pw []byte) (*LoginResult, error) {
	// Make sure the app has been initialized. This condition would error when
	// attempting to retrieve the encryption key below as well, but the
	// messaging may be confusing.
	c.credMtx.RLock()
	creds := c.credentials
	c.credMtx.RUnlock()

	if creds == nil {
		return nil, fmt.Errorf("cannot log in because app has not been initialized")
	}

	if len(creds.EncInnerKey) == 0 {
		err := c.initializePrimaryCredentials(pw, creds.OuterKeyParams)
		if err != nil {
			// It's tempting to panic here, since Core and the db are probably
			// out of sync and the client shouldn't be doing anything else.
			c.log.Criticalf("v1 upgrade failed: %v", err)
			return nil, err
		}
	}

	crypter, err := c.encryptionKey(pw)
	if err != nil {
		return nil, err
	}
	defer crypter.Close()

	loginResult := func() *LoginResult {
		// NOTE: initializeDEXConnections will do authDEX and a 'connect'
		// request only if NOT already dc.acct.authed().
		dexStats := c.initializeDEXConnections(crypter)
		notes, err := c.db.NotificationsN(100)
		if err != nil {
			c.log.Errorf("Login -> NotificationsN error: %v", err)
		}

		return &LoginResult{
			Notifications: notes,
			DEXes:         dexStats,
		}
	}

	// Connecting and loading wallets might take time, but we don't want
	// concurrent wallet connect attempts running, especially for native wallets
	// that use the same file system resources. NOTE: The following channel
	// mechanism similar to the following, and it does suggest some design
	// issues, described below.
	//
	// !c.loginMtx.TryLock() {
	//  c.loginMtx.Lock()
	//  defer c.loginMtx.Lock()
	//  return loginResult(), err
	// }
	defer func() { <-c.loginSlot }() // remove the peg from the slot when done
	select {
	case c.loginSlot <- struct{}{}: // full login
	default:
		c.log.Warnf("Login already running! Waiting for it to complete.")
		// Just wait and take the short path.
		c.loginSlot <- struct{}{}
		return loginResult(), err
	}
	// The design issue that requires this is that we require multiple Login
	// calls to succeed without a Logout between them. To properly solve this,
	// Login would actually error if already logged in. However:
	//  1. Login gives "restart" capabilities for loading and resuming trades.
	//  2. We lack a mechanism for repeated callers (e.g. impatient HTTP
	//     requests or sessions from other browsers) to get the result from the
	//     already-running Login.
	//
	// Fundamentally, we need to separate the following notions:
	//  1. login to Core (credentials check, get cookies)
	//  2. Core initialization (prep internal state, e.g. loading data from DB)
	//  3. login to the server (initializeDEXConnections > authDEX).

	// Attempt to connect to and retrieve balance from all known wallets. It is
	// not an error if we can't connect, unless we need the wallet for active
	// trades, but that condition is checked later in resolveActiveTrades.
	// Ignore updateWalletBalance errors here too, to accommodate wallets that
	// must be unlocked to get the balance. We won't try to unlock here, but if
	// the wallet is needed for active trades, it will be unlocked in
	// resolveActiveTrades and the balance updated there.
	var wg sync.WaitGroup
	var connectCount uint32
	connectWallet := func(wallet *xcWallet) {
		defer wg.Done()
		if !wallet.connected() {
			err := c.connectAndUpdateWallet(wallet)
			if err != nil {
				c.log.Errorf("Unable to connect to %s wallet (start and sync wallets BEFORE starting dex!): %v",
					unbip(wallet.AssetID), err)
				// NOTE: Details for this topic is in the context of fee
				// payment, but the subject pertains to a failure to connect
				// to the wallet.
				subject, _ := c.formatDetails(TopicWalletConnectionWarning)
				c.notify(newWalletConfigNote(TopicWalletConnectionWarning, subject, err.Error(),
					db.ErrorLevel, wallet.state()))
				return
			}
		}
		atomic.AddUint32(&connectCount, 1)
	}
	wallets := c.xcWallets()
	walletCount := len(wallets)
	var tokenWallets []*xcWallet

	for _, wallet := range wallets {
		if asset.TokenInfo(wallet.AssetID) != nil {
			tokenWallets = append(tokenWallets, wallet)
			continue
		}
		wg.Add(1)
		go connectWallet(wallet)
	}
	wg.Wait()

	for _, wallet := range tokenWallets {
		wg.Add(1)
		go connectWallet(wallet)
	}
	wg.Wait()

	if walletCount > 0 {
		c.log.Infof("Connected to %d of %d wallets.", connectCount, walletCount)
	}

	loaded := c.resolveActiveTrades(crypter)
	if loaded > 0 {
		c.log.Infof("loaded %d incomplete orders", loaded)
	}

	return loginResult(), nil
}

// initializePrimaryCredentials sets the PrimaryCredential fields after the DB
// upgrade.
func (c *Core) initializePrimaryCredentials(pw []byte, oldKeyParams []byte) error {
	oldCrypter, err := c.reCrypter(pw, oldKeyParams)
	if err != nil {
		return fmt.Errorf("legacy encryption key deserialization error: %w", err)
	}

	newCrypter, creds, err := c.generateCredentials(pw, nil)
	if err != nil {
		return err
	}

	walletUpdates, acctUpdates, err := c.db.Recrypt(creds, oldCrypter, newCrypter)
	if err != nil {
		return err
	}

	c.setCredentials(creds)

	subject, details := c.formatDetails(TopicUpgradedToSeed)
	c.notify(newSecurityNote(TopicUpgradedToSeed, subject, details, db.WarningLevel))

	for assetID, newEncPW := range walletUpdates {
		w, found := c.wallet(assetID)
		if !found {
			c.log.Errorf("no wallet found for v1 upgrade asset ID %d", assetID)
			continue
		}
		w.setEncPW(newEncPW)
	}

	for host, newEncKey := range acctUpdates {
		dc, _, err := c.dex(host)
		if err != nil {
			c.log.Warnf("no %s dexConnection to update", host)
			continue
		}
		acct := dc.acct
		acct.keyMtx.Lock()
		acct.encKey = newEncKey
		acct.keyMtx.Unlock()
	}

	return nil
}

// Logout logs the user out
func (c *Core) Logout() error {

	// Check active orders
	conns := c.dexConnections()
	for _, dc := range conns {
		if dc.hasActiveOrders() {
			return ActiveOrdersLogoutErr
		}
	}

	// Lock wallets
	for _, w := range c.xcWallets() {
		if w.connected() {
			if err := w.Lock(5 * time.Second); err != nil {
				// A failure to lock the wallet need not block the ability to
				// lock the DEX accounts or shutdown Core gracefully.
				c.log.Warnf("Unable to lock %v wallet: %v", unbip(w.AssetID), err)
			}
		}
	}

	// With no open orders for any of the dex connections, and all wallets locked,
	// lock each dex account.
	for _, dc := range conns {
		dc.acct.lock()
	}

	return nil
}

// Orders fetches a batch of user orders, filtered with the provided
// OrderFilter.
func (c *Core) Orders(filter *OrderFilter) ([]*Order, error) {
	var oid order.OrderID
	if len(filter.Offset) > 0 {
		if len(filter.Offset) != order.OrderIDSize {
			return nil, fmt.Errorf("invalid offset order ID length. wanted %d, got %d", order.OrderIDSize, len(filter.Offset))
		}
		copy(oid[:], filter.Offset)
	}

	ords, err := c.db.Orders(&db.OrderFilter{
		N:        filter.N,
		Offset:   oid,
		Hosts:    filter.Hosts,
		Assets:   filter.Assets,
		Statuses: filter.Statuses,
	})
	if err != nil {
		return nil, fmt.Errorf("UserOrders error: %w", err)
	}

	cords := make([]*Order, 0, len(ords))
	for _, mOrd := range ords {
		corder, err := c.coreOrderFromMetaOrder(mOrd)
		if err != nil {
			return nil, err
		}
		cords = append(cords, corder)
	}

	return cords, nil
}

// coreOrderFromMetaOrder creates an *Order from a *db.MetaOrder, including
// loading matches from the database. The order is presumed to be inactive, so
// swap coin confirmations will not be set. For active orders, get the
// *trackedTrade and use the coreOrder method.
func (c *Core) coreOrderFromMetaOrder(mOrd *db.MetaOrder) (*Order, error) {
	corder := coreOrderFromTrade(mOrd.Order, mOrd.MetaData)
	oid := mOrd.Order.ID()
	excludeCancels := false // maybe don't include cancel order matches?
	matches, err := c.db.MatchesForOrder(oid, excludeCancels)
	if err != nil {
		return nil, fmt.Errorf("MatchesForOrder error loading matches for %s: %w", oid, err)
	}
	corder.Matches = make([]*Match, 0, len(matches))
	for _, match := range matches {
		corder.Matches = append(corder.Matches, matchFromMetaMatch(mOrd.Order, match))
	}
	return corder, nil
}

// Order fetches a single user order.
func (c *Core) Order(oidB dex.Bytes) (*Order, error) {
	oid, err := order.IDFromBytes(oidB)
	if err != nil {
		return nil, err
	}
	// See if its an active order first.
	var tracker *trackedTrade
	for _, dc := range c.dexConnections() {
		tracker, _, _ = dc.findOrder(oid)
		if tracker != nil {
			break
		}
	}
	if tracker != nil {
		return tracker.coreOrder(), nil
	}
	// Must not be an active order. Get it from the database.
	mOrd, err := c.db.Order(oid)
	if err != nil {
		return nil, fmt.Errorf("error retrieving order %s: %w", oid, err)
	}

	return c.coreOrderFromMetaOrder(mOrd)
}

// marketWallets gets the 2 *dex.Assets and 2 *xcWallet associated with a
// market. The wallets will be connected, but not necessarily unlocked.
func (c *Core) marketWallets(host string, base, quote uint32) (ba, qa *dex.Asset, bw, qw *xcWallet, err error) {
	c.connMtx.RLock()
	dc, found := c.conns[host]
	c.connMtx.RUnlock()
	if !found {
		return nil, nil, nil, nil, fmt.Errorf("Unknown host: %s", host)
	}

	ba, found = dc.assets[base]
	if !found {
		return nil, nil, nil, nil, fmt.Errorf("%s not supported by %s", unbip(base), host)
	}
	qa, found = dc.assets[quote]
	if !found {
		return nil, nil, nil, nil, fmt.Errorf("%s not supported by %s", unbip(quote), host)
	}

	bw, err = c.connectedWallet(base)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("%s wallet error: %v", unbip(base), err)
	}
	qw, err = c.connectedWallet(quote)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("%s wallet error: %v", unbip(quote), err)
	}
	return
}

// MaxBuy is the maximum-sized *OrderEstimate for a buy order on the specified
// market. An order rate must be provided, since the number of lots available
// for trading will vary based on the rate for a buy order (unlike a sell
// order).
func (c *Core) MaxBuy(host string, base, quote uint32, rate uint64) (*MaxOrderEstimate, error) {
	baseAsset, quoteAsset, baseWallet, quoteWallet, err := c.marketWallets(host, base, quote)
	if err != nil {
		return nil, err
	}

	dc, _, err := c.dex(host)
	if err != nil {
		return nil, err
	}

	mktID := marketName(base, quote)
	mktConf := dc.marketConfig(mktID)
	if mktConf == nil {
		return nil, newError(marketErr, "unknown market %q", mktID)
	}

	lotSize := mktConf.LotSize
	quoteLotEst := calc.BaseToQuote(rate, lotSize)
	if quoteLotEst == 0 {
		return nil, errors.New("cannot divide by lot size zero")
	}

	swapFeeSuggestion := c.feeSuggestion(dc, quote)
	if swapFeeSuggestion == 0 {
		return nil, fmt.Errorf("failed to get swap fee suggestion for %s at %s", unbip(quote), host)
	}

	redeemFeeSuggestion := c.feeSuggestionAny(base)
	if redeemFeeSuggestion == 0 {
		return nil, fmt.Errorf("failed to get redeem fee suggestion for %s at %s", unbip(base), host)
	}

	maxBuy, err := quoteWallet.MaxOrder(&asset.MaxOrderForm{
		LotSize:       quoteLotEst,
		FeeSuggestion: swapFeeSuggestion,
		AssetConfig:   quoteAsset,
		RedeemConfig:  baseAsset,
	})
	if err != nil {
		return nil, fmt.Errorf("%s wallet MaxOrder error: %v", unbip(quote), err)
	}

	preRedeem, err := baseWallet.PreRedeem(&asset.PreRedeemForm{
		Lots:          maxBuy.Lots,
		FeeSuggestion: redeemFeeSuggestion,
		AssetConfig:   baseAsset,
	})
	if err != nil {
		return nil, fmt.Errorf("%s PreRedeem error: %v", unbip(base), err)
	}

	return &MaxOrderEstimate{
		Swap:   maxBuy,
		Redeem: preRedeem.Estimate,
	}, nil
}

// MaxSell is the maximum-sized *OrderEstimate for a sell order on the specified
// market.
func (c *Core) MaxSell(host string, base, quote uint32) (*MaxOrderEstimate, error) {
	baseAsset, quoteAsset, baseWallet, quoteWallet, err := c.marketWallets(host, base, quote)
	if err != nil {
		return nil, err
	}

	dc, _, err := c.dex(host)
	if err != nil {
		return nil, err
	}
	mktID := marketName(base, quote)
	mktConf := dc.marketConfig(mktID)
	if mktConf == nil {
		return nil, newError(marketErr, "unknown market %q", mktID)
	}
	lotSize := mktConf.LotSize
	if lotSize == 0 {
		return nil, errors.New("cannot divide by lot size zero")
	}

	// Estimate a quote-converted lot size.
	book := dc.bookie(mktID)
	if book == nil {
		return nil, fmt.Errorf("no book synced for %s at %s", mktID, host)
	}

	swapFeeSuggestion := c.feeSuggestion(dc, base)
	if swapFeeSuggestion == 0 {
		return nil, fmt.Errorf("failed to get swap fee suggestion for %s at %s", unbip(base), host)
	}

	redeemFeeSuggestion := c.feeSuggestionAny(quote)
	if redeemFeeSuggestion == 0 {
		return nil, fmt.Errorf("failed to get redeem fee suggestion for %s at %s", unbip(quote), host)
	}

	maxSell, err := baseWallet.MaxOrder(&asset.MaxOrderForm{
		LotSize:       lotSize,
		FeeSuggestion: swapFeeSuggestion,
		AssetConfig:   baseAsset,
		RedeemConfig:  quoteAsset,
	})
	if err != nil {
		return nil, fmt.Errorf("%s wallet MaxOrder error: %v", unbip(base), err)
	}

	preRedeem, err := quoteWallet.PreRedeem(&asset.PreRedeemForm{
		Lots:          maxSell.Lots,
		FeeSuggestion: redeemFeeSuggestion,
		AssetConfig:   quoteAsset,
	})
	if err != nil {
		return nil, fmt.Errorf("%s PreRedeem error: %v", unbip(quote), err)
	}

	return &MaxOrderEstimate{
		Swap:   maxSell,
		Redeem: preRedeem.Estimate,
	}, nil
}

// initializeDEXConnections connects to the DEX servers in the conns map and
// authenticates the connection. If registration is incomplete, reFee is run and
// the connection will be authenticated once the `notifyfee` request is sent.
// If an account is not found on the dex server upon dex authentication the
// account is disabled and the corresponding entry in c.conns is removed
// which will result in the user being prompted to register again.
func (c *Core) initializeDEXConnections(crypter encrypt.Crypter) []*DEXBrief {
	var wg sync.WaitGroup
	conns := c.dexConnections()
	results := make([]*DEXBrief, 0, len(conns))
	for _, dc := range conns {
		dc.tradeMtx.RLock()
		tradeIDs := make([]string, 0, len(dc.trades))
		for tradeID := range dc.trades {
			tradeIDs = append(tradeIDs, tradeID.String())
		}
		dc.tradeMtx.RUnlock()
		result := &DEXBrief{
			Host:     dc.acct.host,
			TradeIDs: tradeIDs,
			// AcctID might be set to a zeroed string here, but will be reset
			// below if we have to auth.
			AcctID: dc.acct.ID().String(),
		}
		results = append(results, result)
		// Unlock before checking auth and continuing, because if the user
		// logged out and didn't shut down, the account is still authed, but
		// locked, and needs unlocked.
		err := dc.acct.unlock(crypter)
		if err != nil {
			subject, details := c.formatDetails(TopicAccountUnlockError, dc.acct.host, err)
			c.notify(newFeePaymentNote(TopicAccountUnlockError, subject, details, db.ErrorLevel, dc.acct.host))
			result.AuthErr = details
			continue
		}
		if dc.acct.authed() {
			result.Authed = true
			result.AcctID = dc.acct.ID().String()
			continue // authDEX already done
		}
		result.AcctID = dc.acct.ID().String()

		if !dc.acct.feePaid() {
			if len(dc.acct.feeCoin) == 0 {
				subject, details := c.formatDetails(TopicFeeCoinError, dc.acct.host)
				c.notify(newFeePaymentNote(TopicFeeCoinError, subject, details, db.ErrorLevel, dc.acct.host))
				result.AuthErr = details
				continue
			}
			// Try to unlock the fee wallet, which should run the reFee cycle, and
			// in turn will run authDEX.
			feeWallet, err := c.connectedWallet(dc.acct.feeAssetID)
			if err != nil {
				c.log.Debugf("Failed to connect for reFee at %s with error: %v", dc.acct.host, err)
				subject, details := c.formatDetails(TopicWalletConnectionWarning, dc.acct.host)
				c.notify(newFeePaymentNote(TopicWalletConnectionWarning, subject, details, db.WarningLevel, dc.acct.host))
				result.AuthErr = details
				continue
			}
			if !feeWallet.unlocked() {
				err = feeWallet.Unlock(crypter)
				if err != nil {
					subject, details := c.formatDetails(TopicWalletUnlockError, dc.acct.host, err)
					c.notify(newFeePaymentNote(TopicWalletUnlockError, subject, details, db.ErrorLevel, dc.acct.host))
					result.AuthErr = details
					continue
				}
			}
			c.reFee(feeWallet, dc)
			continue
		}

		wg.Add(1)
		go func(dc *dexConnection) {
			defer wg.Done()
			err := c.authDEX(dc)
			if err != nil {
				subject, details := c.formatDetails(TopicDexAuthError, dc.acct.host, err)
				c.notify(newDEXAuthNote(TopicDexAuthError, subject, dc.acct.host, false, details, db.ErrorLevel))
				result.AuthErr = details
				return
			}
			result.Authed = true
		}(dc)
	}
	wg.Wait()
	return results
}

// resolveActiveTrades loads order and match data from the database. Only active
// orders and orders with active matches are loaded. Also, only active matches
// are loaded, even if there are inactive matches for the same order, but it may
// be desirable to load all matches, so this behavior may change.
func (c *Core) resolveActiveTrades(crypter encrypt.Crypter) (loaded int) {
	failed := make(map[uint32]struct{})
	relocks := make(assetMap)
	for _, dc := range c.dexConnections() {
		// loadDBTrades can add to the failed map.
		ready, err := c.loadDBTrades(dc, crypter, failed)
		if err != nil {
			subject, details := c.formatDetails(TopicOrderLoadFailure, err)
			c.notify(newOrderNote(TopicOrderLoadFailure, subject, details, db.ErrorLevel, nil))
			// Keep going since some trades may still have loaded.
		}
		if len(ready) > 0 {
			locks := c.resumeTrades(dc, ready) // fills out dc.trades
			relocks.merge(locks)
		}
		loaded += len(ready)
	}
	c.updateBalances(relocks)
	return loaded
}

func (c *Core) wait(coinID []byte, assetID uint32, trigger func() (bool, error), action func(error)) {
	c.waiterMtx.Lock()
	defer c.waiterMtx.Unlock()
	c.blockWaiters[coinIDString(assetID, coinID)] = &blockWaiter{
		assetID: assetID,
		trigger: trigger,
		action:  action,
	}
}

func (c *Core) notifyFee(dc *dexConnection, coinID []byte) error {
	if dc.acct.locked() {
		return fmt.Errorf("%s account locked. cannot notify fee. log in first", dc.acct.host)
	}
	// Notify the server of the fee coin once there are enough confirmations.
	req := &msgjson.NotifyFee{
		AccountID: dc.acct.id[:],
		CoinID:    coinID,
	}
	// Timestamp and sign the request.
	stampAndSign(dc.acct.privKey, req)
	msg, err := msgjson.NewRequest(dc.NextID(), msgjson.NotifyFeeRoute, req)
	if err != nil {
		return fmt.Errorf("failed to create notifyfee request: %w", err)
	}

	// Make the 'notifyfee' request and wait for the response. The server waits
	// an unspecified amount of time to discover the transaction, so time out
	// after this DEX's configured broadcast timeout plus a healthy buffer for
	// communication and server processing latencies. There is no reason to give
	// up too soon.
	timeout := time.Millisecond*time.Duration(dc.cfg.BroadcastTimeout) + 10*time.Second
	errChan := make(chan error, 1)
	err = dc.RequestWithTimeout(msg, func(resp *msgjson.Message) {
		ack := new(msgjson.Acknowledgement)
		// Do NOT capture err in this closure.
		if err := resp.UnmarshalResult(ack); err != nil {
			errChan <- fmt.Errorf("notify fee result error: %w", err)
			return
		}
		err := dc.acct.checkSig(req.Serialize(), ack.Sig)
		if err != nil {
			c.log.Warnf("Account was registered, but DEX signature could not be verified: %v", err)
		}
		errChan <- c.db.AccountPaid(&db.AccountProof{
			Host:  dc.acct.host,
			Stamp: req.Time,
			Sig:   ack.Sig,
		})
	}, timeout, func() {
		errChan <- fmt.Errorf("timed out waiting for '%s' response", msgjson.NotifyFeeRoute)
	})
	if err != nil {
		return fmt.Errorf("Sending the 'notifyfee' request failed: %w", err)
	}

	// The request was sent. Wait for a response or timeout.
	return <-errChan
}

// feeSuggestionAny gets a fee suggestion for the given asset from any source
// with it available. It first checks for a capable wallet, then relevant books
// for a cached fee rate obtained with an epoch_report message, and falls back
// to directly requesting a rate from servers with a fee_rate request.
func (c *Core) feeSuggestionAny(assetID uint32, preferredConns ...*dexConnection) uint64 {
	conns := append(preferredConns, c.dexConnections()...)
	// See if the wallet supports fee rates.
	w, found := c.wallet(assetID)
	if found && w.connected() {
		if rater, is := w.feeRater(); is {
			if r := rater.FeeRate(); r != 0 {
				return r
			}
		}
	}
	// Look for cached rates from epoch_report messages.
	for _, dc := range conns {
		feeSuggestion := dc.bestBookFeeSuggestion(assetID)
		if feeSuggestion > 0 {
			return feeSuggestion
		}
	}

	// Helper function to determine if a server has an active market that pairs
	// the requested asset.
	hasActiveMarket := func(dc *dexConnection) bool {
		dc.cfgMtx.RLock()
		cfg := dc.cfg
		dc.cfgMtx.RUnlock()
		for _, mkt := range cfg.Markets {
			if mkt.Base == assetID || mkt.Quote == assetID && mkt.Running() {
				return true
			}
		}
		return false
	}

	// Request a rate with fee_rate.
	for _, dc := range conns {
		// The server should have at least one active market with the asset,
		// otherwise we might get an outdated rate for an asset whose backend
		// might be supported but not in active use, e.g. down for maintenance.
		// The fee_rate endpoint will happily return a very old rate without
		// indication.
		if !hasActiveMarket(dc) {
			continue
		}

		feeSuggestion := dc.fetchFeeRate(assetID)
		if feeSuggestion > 0 {
			return feeSuggestion
		}
	}
	return 0
}

// feeSuggestion gets the best fee suggestion, first from a synced order book,
// and if not synced, directly from the server.
func (c *Core) feeSuggestion(dc *dexConnection, assetID uint32) (feeSuggestion uint64) {
	// Prepare a fee suggestion based on the last reported fee rate in the
	// order book feed.
	feeSuggestion = dc.bestBookFeeSuggestion(assetID)
	if feeSuggestion > 0 {
		return
	}
	return dc.fetchFeeRate(assetID)
}

// Withdraw initiates a withdraw from an exchange wallet. The client password
// must be provided as an additional verification. This method is DEPRECATED. Use
// Send with the subtract option instead.
func (c *Core) Withdraw(pw []byte, assetID uint32, value uint64, address string) (asset.Coin, error) {
	return c.Send(pw, assetID, value, address, true)
}

// Send initiates either send or withdraw from an exchange wallet. if subtract
// is true, fees are subtracted from the value else fees are taken from the
// exchange wallet. The client password must be provided as an additional
// verification.
func (c *Core) Send(pw []byte, assetID uint32, value uint64, address string, subtract bool) (asset.Coin, error) {
	crypter, err := c.encryptionKey(pw)
	if err != nil {
		return nil, fmt.Errorf("password error: %w", err)
	}
	defer crypter.Close()
	if value == 0 {
		return nil, fmt.Errorf("cannot send/withdraw zero %s", unbip(assetID))
	}
	wallet, found := c.wallet(assetID)
	if !found {
		return nil, newError(missingWalletErr, "no wallet found for %s", unbip(assetID))
	}
	err = c.connectAndUnlock(crypter, wallet)
	if err != nil {
		return nil, err
	}

	var coin asset.Coin
	feeSuggestion := c.feeSuggestionAny(assetID)
	if !subtract {
		coin, err = wallet.Wallet.Send(address, value, feeSuggestion)
	} else {
		if withdrawer, isWithdrawer := wallet.Wallet.(asset.Withdrawer); isWithdrawer {
			coin, err = withdrawer.Withdraw(address, value, feeSuggestion)
		} else {
			return nil, fmt.Errorf("wallet does not support subtracting network fee from withdraw amount")
		}
	}
	if err != nil {
		subject, details := c.formatDetails(TopicSendError, unbip(assetID), err)
		c.notify(newSendNote(TopicSendError, subject, details, db.ErrorLevel))
		return nil, err
	}

	subject, details := c.formatDetails(TopicSendSuccess, unbip(assetID), coin)
	c.notify(newSendNote(TopicSendSuccess, subject, details, db.Success))

	c.updateAssetBalance(assetID)
	return coin, nil
}

func (c *Core) PreOrder(form *TradeForm) (*OrderEstimate, error) {
	dc, err := c.connectedDEX(form.Host)
	if err != nil {
		return nil, err
	}

	mktID := marketName(form.Base, form.Quote)
	mktConf := dc.marketConfig(mktID)
	if mktConf == nil {
		return nil, newError(marketErr, "unknown market %q", mktID)
	}

	wallets, err := c.walletSet(dc, form.Base, form.Quote, form.Sell)
	if err != nil {
		return nil, err
	}

	// So here's the thing. Our assets thus far don't require the wallet to be
	// unlocked to get order estimation (listunspent works on locked wallet),
	// but if we run into an asset that breaks that assumption, we may need
	// to require a password here before estimation.

	// We need the wallets to be connected.
	if !wallets.fromWallet.connected() {
		err := c.connectAndUpdateWallet(wallets.fromWallet)
		if err != nil {
			c.log.Errorf("Error connecting to %s wallet: %v", wallets.fromAsset.Symbol, err)
			return nil, fmt.Errorf("Error connecting to %s wallet", wallets.fromAsset.Symbol)
		}
	}

	if !wallets.toWallet.connected() {
		err := c.connectAndUpdateWallet(wallets.toWallet)
		if err != nil {
			c.log.Errorf("Error connecting to %s wallet: %v", wallets.toAsset.Symbol, err)
			return nil, fmt.Errorf("Error connecting to %s wallet", wallets.toAsset.Symbol)
		}
	}

	// Fund the order and prepare the coins.
	lotSize := mktConf.LotSize
	lots := form.Qty / lotSize
	rate := form.Rate

	if !form.IsLimit {
		// If this is a market order, we'll predict the fill price.
		book := dc.bookie(marketName(form.Base, form.Quote))
		if book == nil {
			return nil, fmt.Errorf("Cannot estimate market order without a synced book")
		}

		midGap, err := book.MidGap()
		if err != nil {
			return nil, fmt.Errorf("Cannot estimate market order with an empty order book")
		}

		if !form.Sell && calc.BaseToQuote(lotSize, midGap) > form.Qty {
			return nil, fmt.Errorf("Market order quantity buys less than a single lot")
		}

		var fills []*orderbook.Fill
		var filled bool
		if form.Sell {
			fills, filled = book.BestFill(form.Sell, form.Qty)
		} else {
			fills, filled = book.BestFillMarketBuy(form.Qty, lotSize)
		}

		if !filled {
			return nil, fmt.Errorf("Market is too thin to estimate market order")
		}

		// Get an average rate.
		var qtySum, product uint64
		for _, fill := range fills {
			product += fill.Quantity * fill.Rate
			qtySum += fill.Quantity
		}
		rate = product / qtySum
		if !form.Sell {
			lots = qtySum / lotSize
		}
	}

	swapFeeSuggestion := c.feeSuggestion(dc, wallets.fromAsset.ID) // server rates only for the swap init
	if swapFeeSuggestion == 0 {
		return nil, fmt.Errorf("failed to get swap fee suggestion for %s at %s", unbip(wallets.fromAsset.ID), form.Host)
	}

	redeemFeeSuggestion := c.feeSuggestionAny(wallets.toAsset.ID) // wallet rate or server rate
	if redeemFeeSuggestion == 0 {
		return nil, fmt.Errorf("failed to get redeem fee suggestion for %s at %s", unbip(wallets.toAsset.ID), form.Host)
	}

	swapLotSize := lotSize
	if !form.Sell {
		swapLotSize = calc.BaseToQuote(rate, lotSize)
	}

	swapEstimate, err := wallets.fromWallet.PreSwap(&asset.PreSwapForm{
		LotSize:         swapLotSize,
		Lots:            lots,
		AssetConfig:     wallets.fromAsset,
		RedeemConfig:    wallets.toAsset,
		Immediate:       form.IsLimit && form.TifNow,
		FeeSuggestion:   swapFeeSuggestion,
		SelectedOptions: form.Options,
	})
	if err != nil {
		return nil, fmt.Errorf("error getting swap estimate: %w", err)
	}

	redeemEstimate, err := wallets.toWallet.PreRedeem(&asset.PreRedeemForm{
		Lots:            lots,
		FeeSuggestion:   redeemFeeSuggestion,
		SelectedOptions: form.Options,
		AssetConfig:     wallets.toAsset,
	})
	if err != nil {
		return nil, fmt.Errorf("error getting redemption estimate: %v", err)
	}

	return &OrderEstimate{
		Swap:   swapEstimate,
		Redeem: redeemEstimate,
	}, nil
}

// Trade is used to place a market or limit order.
func (c *Core) Trade(pw []byte, form *TradeForm) (*Order, error) {
	// Check the user password.
	crypter, err := c.encryptionKey(pw)
	if err != nil {
		return nil, fmt.Errorf("Trade password error: %w", err)
	}
	defer crypter.Close()
	dc, err := c.connectedDEX(form.Host)
	if err != nil {
		return nil, err
	}
	if dc.acct.suspended() {
		return nil, newError(suspendedAcctErr, "may not trade while account is suspended")
	}

	corder, updatedAssets, err := c.prepareTrackedTrade(dc, form, crypter)
	if err != nil {
		return nil, err
	}

	for assetID := range updatedAssets {
		c.updateAssetBalance(assetID)
	}

	return corder, nil
}

// Send an order, process result, prepare and store the trackedTrade.
func (c *Core) prepareTrackedTrade(dc *dexConnection, form *TradeForm, crypter encrypt.Crypter) (*Order, assetMap, error) {
	mktID := marketName(form.Base, form.Quote)
	mktConf := dc.marketConfig(mktID)
	if mktConf == nil {
		return nil, nil, newError(marketErr, "order placed for unknown market %q", mktID)
	}

	// Proceed with the order if there is no trade suspension
	// scheduled for the market.
	if !dc.running(mktID) {
		return nil, nil, newError(marketErr, "%s market trading is suspended", mktID)
	}

	rate, qty := form.Rate, form.Qty
	if form.IsLimit && rate == 0 {
		return nil, nil, newError(orderParamsErr, "zero-rate order not allowed")
	}

	wallets, err := c.walletSet(dc, form.Base, form.Quote, form.Sell)
	if err != nil {
		return nil, nil, err
	}
	fromWallet, toWallet := wallets.fromWallet, wallets.toWallet

	accountRedeemer, isAccountRedemption := toWallet.Wallet.(asset.AccountLocker)
	accountRefunder, isAccountRefund := fromWallet.Wallet.(asset.AccountLocker)

	prepareWallet := func(w *xcWallet) error {
		// NOTE: If the wallet is already internally unlocked (the decrypted
		// password cached in xcWallet.pw), this could be done without the
		// crypter via refreshUnlock.
		err := c.connectAndUnlock(crypter, w)
		if err != nil {
			return fmt.Errorf("%s connectAndUnlock error: %w",
				wallets.fromAsset.Symbol, err)
		}
		w.mtx.RLock()
		defer w.mtx.RUnlock()
		if w.peerCount < 1 {
			return fmt.Errorf("%s wallet has no network peers (check your network or firewall)",
				unbip(w.AssetID))
		}
		if !w.synced {
			return fmt.Errorf("%s still syncing. progress = %.2f%%", unbip(w.AssetID),
				w.syncProgress*100)
		}
		return nil
	}

	err = prepareWallet(fromWallet)
	if err != nil {
		return nil, nil, err
	}

	err = prepareWallet(toWallet)
	if err != nil {
		return nil, nil, err
	}

	// Get an address for the swap contract.
	redeemAddr, err := toWallet.RedemptionAddress()
	if err != nil {
		return nil, nil, codedError(walletErr, fmt.Errorf("%s RedemptionAddress error: %w", wallets.toAsset.Symbol, err))
	}

	// Fund the order and prepare the coins.
	lotSize := mktConf.LotSize
	fundQty := qty
	lots := qty / lotSize
	if form.IsLimit && !form.Sell {
		fundQty = calc.BaseToQuote(rate, fundQty)
	}
	redemptionRefundLots := lots

	isImmediate := (!form.IsLimit || form.TifNow)

	// Market buy order
	if !form.IsLimit && !form.Sell {
		// There is some ambiguity here about whether the specified quantity for
		// a market buy order should include projected fees, or whether fees
		// should be in addition to the quantity. If the fees should be
		// additional to the order quantity (the approach taken here), we should
		// try to estimate the number of lots based on the current market. If
		// the market is not synced, fall back to a single-lot estimate, with
		// the knowledge that such an estimate means that the specified amount
		// might not all be available for matching once fees are considered.
		lots = 1
		book := dc.bookie(mktID)
		if book != nil {
			midGap, err := book.MidGap()
			// An error is only returned when there are no orders on the book.
			// In that case, fall back to the 1 lot estimate for now.
			if err == nil {
				baseQty := calc.QuoteToBase(midGap, fundQty)
				lots = baseQty / lotSize
				redemptionRefundLots = lots * marketBuyRedemptionSlippageBuffer
				if lots == 0 {
					err = newError(orderParamsErr,
						"order quantity is too low for current market rates. "+
							"qty = %d %s, mid-gap = %d, base-qty = %d %s, lot size = %d",
						qty, wallets.quoteAsset.Symbol, midGap, baseQty,
						wallets.baseAsset.Symbol, lotSize)
					return nil, nil, err
				}
			} else if isAccountRedemption {
				return nil, nil, newError(orderParamsErr, "cannot estimate redemption count")
			}
		}
	}

	if lots == 0 {
		return nil, nil, newError(orderParamsErr, "order quantity < 1 lot. qty = %d %s, rate = %d, lot size = %d",
			qty, wallets.baseAsset.Symbol, rate, lotSize)
	}

	coins, redeemScripts, err := fromWallet.FundOrder(&asset.Order{
		Value:         fundQty,
		MaxSwapCount:  lots,
		DEXConfig:     wallets.fromAsset,
		RedeemConfig:  wallets.toAsset,
		Immediate:     isImmediate,
		FeeSuggestion: c.feeSuggestion(dc, wallets.fromAsset.ID),
		Options:       form.Options,
	})
	if err != nil {
		return nil, nil, codedError(walletErr, fmt.Errorf("FundOrder error for %s, funding quantity %d (%d lots): %w",
			wallets.fromAsset.Symbol, fundQty, lots, err))
	}

	coinIDs := make([]order.CoinID, 0, len(coins))
	for i := range coins {
		coinIDs = append(coinIDs, []byte(coins[i].ID()))
	}

	// In the special case that there is a single coin that implements
	// RecoveryCoin, set that as the change coin.
	var recoveryCoin asset.Coin
	var changeID []byte
	if len(coins) == 1 {
		c := coins[0]
		if rc, is := c.(asset.RecoveryCoin); is {
			recoveryCoin = c
			changeID = rc.RecoveryID()
		}
	}

	// The coins selected for this order will need to be unlocked
	// if the order does not get to the server successfully.
	var success bool
	defer func() {
		if success {
			return
		}
		err := fromWallet.ReturnCoins(coins)
		if err != nil {
			c.log.Warnf("Unable to return %s funding coins: %v", unbip(fromWallet.AssetID), err)
		}
	}()

	// Construct the order.
	preImg := newPreimage()
	prefix := &order.Prefix{
		AccountID:  dc.acct.ID(),
		BaseAsset:  form.Base,
		QuoteAsset: form.Quote,
		OrderType:  order.MarketOrderType,
		ClientTime: time.Now(),
		Commit:     preImg.Commit(),
	}
	var ord order.Order
	if form.IsLimit {
		prefix.OrderType = order.LimitOrderType
		tif := order.StandingTiF
		if form.TifNow {
			tif = order.ImmediateTiF
		}
		ord = &order.LimitOrder{
			P: *prefix,
			T: order.Trade{
				Coins:    coinIDs,
				Sell:     form.Sell,
				Quantity: form.Qty,
				Address:  redeemAddr,
			},
			Rate:  form.Rate,
			Force: tif,
		}
	} else {
		ord = &order.MarketOrder{
			P: *prefix,
			T: order.Trade{
				Coins:    coinIDs,
				Sell:     form.Sell,
				Quantity: form.Qty,
				Address:  redeemAddr,
			},
		}
	}
	err = order.ValidateOrder(ord, order.OrderStatusEpoch, lotSize)
	if err != nil {
		return nil, nil, fmt.Errorf("ValidateOrder error: %w", err)
	}

	msgCoins, err := messageCoins(wallets.fromWallet, coins, redeemScripts)
	if err != nil {
		return nil, nil, fmt.Errorf("wallet %v failed to sign coins: %w", wallets.fromAsset.ID, err)
	}

	// Everything is ready. Send the order.
	route, msgOrder, msgTrade := messageOrder(ord, msgCoins)

	// If the to asset is an AccountLocker, we need to lock up redemption
	// funds.
	var redemptionReserves uint64
	if isAccountRedemption {
		pubKeys, sigs, err := toWallet.SignMessage(nil, msgOrder.Serialize())
		if err != nil {
			return nil, nil, codedError(signatureErr, fmt.Errorf("SignMessage error: %w", err))
		}
		if len(pubKeys) == 0 || len(sigs) == 0 {
			return nil, nil, newError(signatureErr, "wrong number of pubkeys or signatures, %d & %d", len(pubKeys), len(sigs))
		}
		redemptionReserves, err = accountRedeemer.ReserveNRedemptions(redemptionRefundLots, wallets.toAsset)
		if err != nil {
			return nil, nil, codedError(walletErr, fmt.Errorf("ReserveNRedemptions error: %w", err))
		}
		msgTrade.RedeemSig = &msgjson.RedeemSig{
			PubKey: pubKeys[0],
			Sig:    sigs[0],
		}
		defer func() {
			if !success {
				accountRedeemer.UnlockRedemptionReserves(redemptionReserves)
			}
		}()
	}

	// If the from asset is an AccountLocker, we need to lock up refund funds.
	var refundReserves uint64
	if isAccountRefund {
		refundReserves, err = accountRefunder.ReserveNRefunds(redemptionRefundLots, wallets.fromAsset)
		if err != nil {
			return nil, nil, codedError(walletErr, fmt.Errorf("ReserveNRefunds error: %w", err))
		}
		defer func() {
			if !success {
				accountRefunder.UnlockRefundReserves(refundReserves)
			}
		}()
	}

	// A non-nil changeID indicates that this is an account based coin. The
	// first coin is an address and the entire serialized message needs to
	// be signed with that address's private key.
	if changeID != nil {
		if _, msgTrade.Coins[0].Sigs, err = fromWallet.SignMessage(nil, msgOrder.Serialize()); err != nil {
			return nil, nil, fmt.Errorf("wallet %v failed to sign for redeem: %w", wallets.fromAsset.ID, err)
		}
	}

	commitSig := make(chan struct{})
	defer close(commitSig) // signals on both success and failure, unlike syncOrderPlaced/piSyncers
	c.sentCommitsMtx.Lock()
	c.sentCommits[prefix.Commit] = commitSig
	c.sentCommitsMtx.Unlock()

	// Send and get the result.
	result := new(msgjson.OrderResult)
	err = dc.signAndRequest(msgOrder, route, result, fundingTxWait+DefaultResponseTimeout)
	if err != nil {
		// At this point there is a possibility that the server got the request
		// and created the trade order, but we lost the connection before
		// receiving the response with the trade's order ID. Any preimage
		// request will be unrecognized. This order is ABANDONED.
		return nil, nil, fmt.Errorf("new order request with DEX server %v market %v failed: %w", dc.acct.host, mktID, err)
	}

	// If we encounter an error, perform some basic logging.
	logAbandon := func(err interface{}) {
		c.log.Errorf("Abandoning order. preimage: %x, server time: %d: %v",
			preImg[:], result.ServerTime, err)
	}

	err = validateOrderResponse(dc, result, ord, msgOrder) // stamps the order, giving it a valid ID
	if err != nil {
		logAbandon(fmt.Sprintf("order response validation failure: %v", err))
		return nil, nil, fmt.Errorf("validateOrderResponse error: %w", err)
	}

	// Store the order.
	dbOrder := &db.MetaOrder{
		MetaData: &db.OrderMetaData{
			Status:           order.OrderStatusEpoch,
			Host:             dc.acct.host,
			MaxFeeRate:       wallets.fromAsset.MaxFeeRate,
			RedeemMaxFeeRate: wallets.toAsset.MaxFeeRate,
			Proof: db.OrderProof{
				DEXSig:   result.Sig,
				Preimage: preImg[:],
			},
			FromVersion:        wallets.fromAsset.Version,
			ToVersion:          wallets.toAsset.Version,
			Options:            form.Options,
			RedemptionReserves: redemptionReserves,
			ChangeCoin:         changeID,
		},
		Order: ord,
	}
	err = c.db.UpdateOrder(dbOrder)
	if err != nil {
		logAbandon(fmt.Sprintf("failed to store order in database: %v", err))
		return nil, nil, fmt.Errorf("Order abandoned due to database error: %w", err)
	}

	// Prepare and store the tracker and get the core.Order to return.
	tracker := newTrackedTrade(dbOrder, preImg, dc, dc.marketEpochDuration(mktID), c.lockTimeTaker, c.lockTimeMaker,
		c.db, c.latencyQ, wallets, coins, c.notify, c.formatDetails, form.Options, redemptionReserves, refundReserves)

	tracker.redemptionLocked = tracker.redemptionReserves
	tracker.refundLocked = tracker.refundReserves

	if recoveryCoin != nil {
		tracker.change = recoveryCoin
		tracker.coinsLocked = false
		tracker.changeLocked = true
	}

	dc.tradeMtx.Lock()
	dc.trades[tracker.ID()] = tracker
	dc.tradeMtx.Unlock()

	// Now that the trades map is updated, sync with the preimage request.
	c.syncOrderPlaced(ord.ID())

	// Send a low-priority notification.
	corder := tracker.coreOrder()
	if !form.IsLimit && !form.Sell {
		ui := wallets.quoteWallet.Info().UnitInfo
		subject, details := c.formatDetails(TopicYoloPlaced,
			ui.ConventionalString(corder.Qty), ui.Conventional.Unit, tracker.token())
		c.notify(newOrderNote(TopicYoloPlaced, subject, details, db.Poke, corder))
	} else {
		rateString := "market"
		if form.IsLimit {
			rateString = wallets.trimmedConventionalRateString(corder.Rate)
		}
		ui := wallets.baseWallet.Info().UnitInfo
		subject, details := c.formatDetails(TopicOrderPlaced,
			sellString(corder.Sell), ui.ConventionalString(corder.Qty), ui.Conventional.Unit, rateString, tracker.token())
		c.notify(newOrderNote(TopicOrderPlaced, subject, details, db.Poke, corder))
	}

	updated := assetMap{fromWallet.AssetID: struct{}{}}
	if isAccountRefund && fromWallet.parent != nil {
		updated[fromWallet.parent.AssetID] = struct{}{}
	}
	if isAccountRedemption && toWallet.parent != nil {
		updated[toWallet.parent.AssetID] = struct{}{}
	}

	success = true

	return corder, updated, nil
}

// walletSet is a pair of wallets with asset configurations identified in useful
// ways.
type walletSet struct {
	baseAsset   *dex.Asset
	quoteAsset  *dex.Asset
	fromWallet  *xcWallet
	fromAsset   *dex.Asset
	toWallet    *xcWallet
	toAsset     *dex.Asset
	baseWallet  *xcWallet
	quoteWallet *xcWallet
}

// conventionalRate converts the message-rate encoded rate to a rate in
// conventional units.
func (w *walletSet) conventionalRate(msgRate uint64) float64 {
	return calc.ConventionalRate(msgRate, w.baseWallet.Info().UnitInfo, w.quoteWallet.Info().UnitInfo)
}

func (w *walletSet) trimmedConventionalRateString(r uint64) string {
	s := strconv.FormatFloat(w.conventionalRate(r), 'f', 8, 64)
	return strings.TrimRight(strings.TrimRight(s, "0"), ".")
}

// walletSet constructs a walletSet.
func (c *Core) walletSet(dc *dexConnection, baseID, quoteID uint32, sell bool) (*walletSet, error) {
	dc.assetsMtx.RLock()
	baseAsset, baseFound := dc.assets[baseID]
	quoteAsset, quoteFound := dc.assets[quoteID]
	dc.assetsMtx.RUnlock()
	if !baseFound {
		return nil, fmt.Errorf("unknown base asset %d -> %s for %s", baseID, unbip(baseID), dc.acct.host)
	}
	if !quoteFound {
		return nil, fmt.Errorf("unknown quote asset %d -> %s for %s", quoteID, unbip(quoteID), dc.acct.host)
	}

	// Connect and open the wallets if needed.
	baseWallet, found := c.wallet(baseID)
	if !found {
		return nil, newError(missingWalletErr, "no wallet found for %s", unbip(baseID))
	}
	quoteWallet, found := c.wallet(quoteID)
	if !found {
		return nil, newError(missingWalletErr, "no wallet found for %s", unbip(quoteID))
	}

	if ver := baseWallet.Info().Version; baseAsset.Version != ver {
		return nil, newError(walletErr, "wallet asset %d version %d does not match server asset version %d",
			baseID, ver, baseAsset.Version)
	}

	if ver := quoteWallet.Info().Version; quoteAsset.Version != ver {
		return nil, newError(walletErr, "wallet asset %d version %d does not match server asset version %d",
			quoteID, ver, quoteAsset.Version)
	}

	// We actually care less about base/quote, and more about from/to, which
	// depends on whether this is a buy or sell order.
	fromAsset, toAsset := baseAsset, quoteAsset
	fromWallet, toWallet := baseWallet, quoteWallet
	if !sell {
		fromAsset, toAsset = quoteAsset, baseAsset
		fromWallet, toWallet = quoteWallet, baseWallet
	}

	return &walletSet{
		baseAsset:   baseAsset,
		quoteAsset:  quoteAsset,
		fromWallet:  fromWallet,
		fromAsset:   fromAsset,
		toWallet:    toWallet,
		toAsset:     toAsset,
		baseWallet:  baseWallet,
		quoteWallet: quoteWallet,
	}, nil
}

// Cancel is used to send a cancel order which cancels a limit order.
func (c *Core) Cancel(pw []byte, oidB dex.Bytes) error {
	// Check the user password.
	_, err := c.encryptionKey(pw)
	if err != nil {
		return fmt.Errorf("Cancel password error: %w", err)
	}

	oid, err := order.IDFromBytes(oidB)
	if err != nil {
		return err
	}

	for _, dc := range c.dexConnections() {
		found, err := c.tryCancel(dc, oid)
		if err != nil {
			return err
		}
		if found {
			return nil
		}
	}

	return fmt.Errorf("Cancel: failed to find order %s", oidB)
}

// authDEX authenticates the connection for a DEX.
func (c *Core) authDEX(dc *dexConnection) error {
	// Prepare and sign the message for the 'connect' route.
	acctID := dc.acct.ID()
	payload := &msgjson.Connect{
		AccountID:  acctID[:],
		APIVersion: 0,
		Time:       uint64(time.Now().UnixMilli()),
	}
	sigMsg := payload.Serialize()
	sig, err := dc.acct.sign(sigMsg)
	if err != nil {
		return fmt.Errorf("signing error: %w", err)
	}
	payload.SetSig(sig)

	// Send the 'connect' request.
	req, err := msgjson.NewRequest(dc.NextID(), msgjson.ConnectRoute, payload)
	if err != nil {
		return fmt.Errorf("error encoding 'connect' request: %w", err)
	}
	errChan := make(chan error, 1)
	result := new(msgjson.ConnectResult)
	err = dc.RequestWithTimeout(req, func(msg *msgjson.Message) {
		errChan <- msg.UnmarshalResult(result)
	}, DefaultResponseTimeout, func() {
		errChan <- fmt.Errorf("timed out waiting for '%s' response", msgjson.ConnectRoute)
	})

	// Check the request error.
	if err != nil {
		return err
	}
	// Check the response error.
	err = <-errChan
	if err != nil {
		return fmt.Errorf("'connect' error: %w", err)
	}

	// Check the servers response signature.
	err = dc.acct.checkSig(sigMsg, result.Sig)
	if err != nil {
		return newError(signatureErr, "DEX signature validation error: %w", err)
	}

	var suspended bool
	if result.Suspended != nil {
		suspended = *result.Suspended
	}

	// Set the account as authenticated.
	c.log.Debugf("Authenticated connection to %s, acct %v, %d active orders, %d active matches, score %d (suspended = %v)",
		dc.acct.host, acctID, len(result.ActiveOrderStatuses), len(result.ActiveMatches), result.Score, suspended)
	dc.acct.auth(suspended)

	// Associate the matches with known trades.
	matches, _, err := dc.parseMatches(result.ActiveMatches, false)
	if err != nil {
		c.log.Error(err)
	}

	exceptions, matchConflicts := dc.compareServerMatches(matches)
	updatedAssets := make(assetMap)
	for oid, matchAnomalies := range exceptions {
		trade := matchAnomalies.trade
		missing, extras := matchAnomalies.missing, matchAnomalies.extra

		trade.mtx.Lock()

		// Flag each of the missing matches as revoked.
		for _, match := range missing {
			c.log.Warnf("DEX %s did not report active match %s on order %s - assuming revoked, status %v.",
				dc.acct.host, match, oid, match.Status)
			// Must have been revoked while we were gone. Flag to allow recovery
			// and subsequent retirement of the match and parent trade.
			match.MetaData.Proof.SelfRevoked = true
			if err := c.db.UpdateMatch(&match.MetaMatch); err != nil {
				c.log.Errorf("Failed to update missing/revoked match: %v", err)
			}
		}

		// Send a "Missing matches" order note if there are missing match message.
		// Also, check if the now-Revoked matches were the last set of matches that
		// required sending swaps, and unlock coins if so.
		if len(missing) > 0 {
			if trade.maybeReturnCoins() {
				updatedAssets.count(trade.wallets.fromAsset.ID)
			}

			subject, details := c.formatDetails(TopicMissingMatches,
				len(missing), trade.token(), dc.acct.host)
			c.notify(newOrderNote(TopicMissingMatches, subject, details, db.ErrorLevel, trade.coreOrderInternal()))
		}

		// Start negotiation for extra matches for this trade.
		if len(extras) > 0 {
			err := trade.negotiate(extras)
			if err != nil {
				c.log.Errorf("Error negotiating one or more previously unknown matches for order %s reported by %s on connect: %v",
					oid, dc.acct.host, err)
				subject, details := c.formatDetails(TopicMatchResolutionError, len(extras), dc.acct.host, trade.token())
				c.notify(newOrderNote(TopicMatchResolutionError, subject, details, db.ErrorLevel, trade.coreOrderInternal()))
			} else {
				// For taker matches in MakerSwapCast, queue up match status
				// resolution to retrieve the maker's contract and coin.
				for _, extra := range extras {
					if order.MatchSide(extra.Side) == order.Taker && order.MatchStatus(extra.Status) == order.MakerSwapCast {
						var matchID order.MatchID
						copy(matchID[:], extra.MatchID)
						match, found := trade.matches[matchID]
						if !found {
							c.log.Errorf("Extra match %v was not registered by negotiate (db error?)", matchID)
							continue
						}
						c.log.Infof("Queueing match status resolution for newly discovered match %v (%s) "+
							"as taker to MakerSwapCast status.", matchID, match.Status) // had better be NewlyMatched!

						oid := trade.ID()
						conflicts := matchConflicts[oid]
						if conflicts == nil {
							conflicts = &matchStatusConflict{trade: trade}
							matchConflicts[oid] = conflicts
						}
						conflicts.matches = append(conflicts.matches, trade.matches[matchID])
					}
				}
			}
		}

		trade.mtx.Unlock()
	}

	// Compare the server-returned active orders with tracked trades, updating
	// the trade statuses where necessary. This is done after processing the
	// connect resp matches so that where possible, available match data can be
	// used to properly set order statuses and filled amount.
	unknownOrdersCount, reconciledOrdersCount := dc.reconcileTrades(result.ActiveOrderStatuses)
	if unknownOrdersCount > 0 {
		subject, details := c.formatDetails(TopicUnknownOrders, unknownOrdersCount, dc.acct.host)
		c.notify(newDEXAuthNote(TopicUnknownOrders, subject, dc.acct.host, false, details, db.Poke))
	}
	if reconciledOrdersCount > 0 {
		subject, details := c.formatDetails(TopicOrdersReconciled, reconciledOrdersCount)
		c.notify(newDEXAuthNote(TopicOrdersReconciled, subject, dc.acct.host, false, details, db.Poke))
	}

	if len(matchConflicts) > 0 {
		var n int
		for _, c := range matchConflicts {
			n += len(c.matches)
		}
		c.log.Warnf("Beginning match status resolution for %d matches...", n)
		c.resolveMatchConflicts(dc, matchConflicts)
	}

	// List and cancel standing limit orders that are in epoch or booked status,
	// but without funding coins for new matches. This should be done after the
	// order status resolution done above.
	var brokenTrades []*trackedTrade
	dc.tradeMtx.RLock()
	for _, trade := range dc.trades {
		if lo, ok := trade.Order.(*order.LimitOrder); !ok || lo.Force != order.StandingTiF {
			continue // only standing limit orders need to be canceled
		}
		trade.mtx.RLock()
		status := trade.metaData.Status
		if (status == order.OrderStatusEpoch || status == order.OrderStatusBooked) &&
			!trade.hasFundingCoins() {
			brokenTrades = append(brokenTrades, trade)
		}
		trade.mtx.RUnlock()
	}
	dc.tradeMtx.RUnlock()
	for _, trade := range brokenTrades {
		c.log.Warnf("Canceling unfunded standing limit order %v", trade.ID())
		if err = c.tryCancelTrade(dc, trade); err != nil {
			c.log.Warnf("Unable to cancel unfunded trade %v: %v", trade.ID(), err)
		}
	}

	if len(updatedAssets) > 0 {
		c.updateBalances(updatedAssets)
	}

	return nil
}

// AssetBalance retrieves and updates the current wallet balance.
func (c *Core) AssetBalance(assetID uint32) (*WalletBalance, error) {
	wallet, err := c.connectedWallet(assetID)
	if err != nil {
		return nil, fmt.Errorf("%d -> %s wallet error: %w", assetID, unbip(assetID), err)
	}
	return c.updateWalletBalance(wallet)
}

// initialize pulls the known DEXes from the database and attempts to connect
// and retrieve the DEX configuration.
func (c *Core) initialize() {
	accts, err := c.db.Accounts()
	if err != nil {
		c.log.Errorf("Error retrieving accounts from database: %v", err) // panic?
	}

	// Start connecting to DEX servers.
	var liveConns uint32
	var wg sync.WaitGroup
	for _, acct := range accts {
		wg.Add(1)
		go func(acct *db.AccountInfo) {
			defer wg.Done()
			if _, connected := c.connectAccount(acct); connected {
				atomic.AddUint32(&liveConns, 1)
			}
		}(acct)
	}

	dbWallets, err := c.db.Wallets()
	if err != nil {
		c.log.Errorf("error loading wallets from database: %v", err)
	}

	for _, dbWallet := range dbWallets {
		assetID := dbWallet.AssetID
		wallet, err := c.loadWallet(dbWallet)
		if err != nil {
			c.log.Errorf("error loading %d -> %s wallet: %v", assetID, unbip(assetID), err)
			continue
		}
		// Wallet is loaded from the DB, but not yet connected.
		c.log.Infof("Loaded %s wallet configuration.", unbip(assetID))
		c.walletMtx.Lock()
		c.wallets[assetID] = wallet
		c.walletMtx.Unlock()
	}
	c.walletMtx.RLock()
	numWallets := len(c.wallets)
	c.walletMtx.RUnlock()

	if len(dbWallets) > 0 {
		c.log.Infof("successfully loaded %d of %d wallets", numWallets, len(dbWallets))
	}

	// Wait for DEXes to be connected to ensure DEXes are ready for
	// authentication when Login is triggered. NOTE/TODO: Login could just as
	// easily make the connection, but arguably configured DEXs should be
	// available for unauthenticated operations such as watching market feeds.
	wg.Wait()
	c.log.Infof("Successfully connected to %d out of %d DEX servers", liveConns, len(accts))
	pluralize := func(n int) string {
		if n == 1 {
			return ""
		}
		return "s"
	}
	for _, dc := range c.dexConnections() {
		activeOrders, _ := c.dbOrders(dc) // non-nil error will load 0 orders, and any subsequent db error will cause a shutdown on dex auth or sooner
		if n := len(activeOrders); n > 0 {
			c.log.Warnf("\n\n\t ****  IMPORTANT: You have %d active order%s on %s. LOGIN immediately!  **** \n", n, pluralize(n), dc.acct.host)
		}
	}
}

// connectAccount makes a connection to the DEX for the given account. If a
// non-nil dexConnection is returned, it was inserted into the conns map even if
// the initial connection attempt failed (connected == false), and the connect
// retry / keepalive loop is active. If there was already a dexConnection, it is
// first stopped.
func (c *Core) connectAccount(acct *db.AccountInfo) (dc *dexConnection, connected bool) {
	if !acct.Paid && len(acct.FeeCoin) == 0 {
		// Register should have set this when creating the account that was
		// obtained via db.Accounts.
		c.log.Warnf("Incomplete registration without fee payment detected for DEX %s. "+
			"Discarding account.", acct.Host)
		return
	}

	host, err := addrHost(acct.Host)
	if err != nil {
		c.log.Errorf("skipping loading of %s due to address parse error: %v", host, err)
		return
	}

	c.connMtx.RLock()
	if dc := c.conns[host]; dc != nil {
		dc.connMaster.Disconnect()
		dc.acct.lock()
		dc.booksMtx.Lock()
		for m, b := range dc.books {
			b.closeFeeds()
			if b.closeTimer != nil {
				b.closeTimer.Stop()
			}
			delete(dc.books, m)
		}
		dc.booksMtx.Unlock()
	} // leave it in the map so it remains listed if connectDEX fails
	c.connMtx.RUnlock()

	dc, err = c.connectDEX(acct)
	if dc == nil {
		c.log.Errorf("Cannot connect to DEX %s: %v", host, err)
		return
	}
	if err != nil {
		c.log.Errorf("Trouble establishing connection to %s (will retry). Error: %v", host, err)
	} else {
		connected = true
	}

	c.connMtx.Lock()
	c.conns[host] = dc
	c.connMtx.Unlock()

	return
}

// feeLock is used to ensure that no more than one reFee check is running at a
// time.
var feeLock uint32

// checkUnpaidFees checks whether the registration fee info has an acceptable
// state, and tries to rectify any inconsistencies.
func (c *Core) checkUnpaidFees(wallet *xcWallet) {
	if !atomic.CompareAndSwapUint32(&feeLock, 0, 1) {
		return
	}
	var wg sync.WaitGroup
	for _, dc := range c.dexConnections() {
		if dc.acct.feePaid() {
			continue
		}
		if dc.acct.feeAssetID != wallet.AssetID {
			continue // different wallet
		}
		if len(dc.acct.feeCoin) == 0 {
			c.log.Errorf("empty fee coin found for unpaid account")
			continue
		}
		wg.Add(1)
		go func(dc *dexConnection) {
			c.reFee(wallet, dc)
			wg.Done()
		}(dc)
	}
	wg.Wait()
	atomic.StoreUint32(&feeLock, 0)
}

// reFee attempts to finish the fee payment process for a DEX. reFee might be
// called if the client was shutdown after a fee was paid, but before it had the
// requisite confirmations for the 'notifyfee' message to be sent to the server.
func (c *Core) reFee(wallet *xcWallet, dc *dexConnection) {
	feeAsset := dc.feeAsset(wallet.AssetID)
	if feeAsset == nil {
		c.log.Errorf("DEX not connected, or does not accept registration fees in asset %q", unbip(wallet.AssetID))
		return
	}
	reqConfs := feeAsset.Confs

	// Return if the coin is already in blockWaiters.
	if c.existsWaiter(coinIDString(dc.acct.feeAssetID, dc.acct.feeCoin)) {
		return
	}

	// Get the database account info.
	acctInfo, err := c.db.Account(dc.acct.host)
	if err != nil {
		c.log.Errorf("reFee %s - error retrieving account info: %v", dc.acct.host, err)
		return
	}
	// A few sanity checks.
	if !bytes.Equal(acctInfo.FeeCoin, dc.acct.feeCoin) {
		c.log.Errorf("reFee %s - fee coin mismatch. %x != %x", dc.acct.host, acctInfo.FeeCoin, dc.acct.feeCoin)
		return
	}
	if acctInfo.FeeAssetID != dc.acct.feeAssetID {
		c.log.Errorf("reFee %s - fee asset mismatch. %d != %d", dc.acct.host, acctInfo.FeeAssetID, dc.acct.feeAssetID)
		return
	}
	if acctInfo.Paid {
		c.log.Errorf("reFee %s - account for %x already marked paid", dc.acct.host, dc.acct.feeCoin)
		return
	}
	// Get the coin for the fee.
	confs, err := wallet.RegFeeConfirmations(c.ctx, acctInfo.FeeCoin)
	if err != nil {
		c.log.Errorf("reFee %s - error getting coin confirmations: %v", dc.acct.host, err)
		return
	}

	if confs >= reqConfs {
		err := c.notifyFee(dc, acctInfo.FeeCoin)
		if err != nil {
			c.log.Errorf("reFee %s - notifyfee error: %v", dc.acct.host, err)
			subject, details := c.formatDetails(TopicFeePaymentError, dc.acct.host, err)
			c.notify(newFeePaymentNote(TopicFeePaymentError, subject, details, db.ErrorLevel, dc.acct.host))
		} else {
			c.log.Infof("Fee paid at %s", dc.acct.host)
			subject, details := c.formatDetails(TopicAccountRegistered, dc.acct.host)
			c.notify(newFeePaymentNote(TopicAccountRegistered, subject, details, db.Success, dc.acct.host))
			// dc.acct.pay() and c.authDEX????
			dc.acct.markFeePaid()
			err = c.authDEX(dc)
			if err != nil {
				c.log.Errorf("fee paid, but failed to authenticate connection to %s: %v", dc.acct.host, err)
			}
		}
		return
	}
	c.verifyRegistrationFee(wallet.AssetID, dc, acctInfo.FeeCoin, confs, reqConfs)
}

func (c *Core) dbOrders(dc *dexConnection) ([]*db.MetaOrder, error) {
	// Prepare active orders, according to the DB.
	dbOrders, err := c.db.ActiveDEXOrders(dc.acct.host)
	if err != nil {
		return nil, fmt.Errorf("database error when fetching orders for %s: %w", dc.acct.host, err)
	}
	c.log.Infof("Loaded %d active orders.", len(dbOrders))

	// It's possible for an order to not be active, but still have active matches.
	// Grab the orders for those too.
	haveOrder := func(oid order.OrderID) bool {
		for _, dbo := range dbOrders {
			if dbo.Order.ID() == oid {
				return true
			}
		}
		return false
	}

	activeMatchOrders, err := c.db.DEXOrdersWithActiveMatches(dc.acct.host)
	if err != nil {
		return nil, fmt.Errorf("database error fetching active match orders for %s: %w", dc.acct.host, err)
	}
	c.log.Infof("Loaded %d active match orders", len(activeMatchOrders))
	for _, oid := range activeMatchOrders {
		if haveOrder(oid) {
			continue
		}
		dbOrder, err := c.db.Order(oid)
		if err != nil {
			return nil, fmt.Errorf("database error fetching order %s for %s: %w", oid, dc.acct.host, err)
		}
		dbOrders = append(dbOrders, dbOrder)
	}

	return dbOrders, nil
}

// dbTrackers prepares trackedTrades based on active orders and matches in the
// database. Since dbTrackers may run before sign in when wallets are not
// connected or unlocked, wallets and coins are not added to the returned
// trackers. Use resumeTrades with the app Crypter to prepare wallets and coins.
func (c *Core) dbTrackers(dc *dexConnection) (map[order.OrderID]*trackedTrade, error) {
	// Prepare active orders, according to the DB.
	dbOrders, err := c.dbOrders(dc)
	if err != nil {
		return nil, err
	}

	// Index all of the cancel orders so we can account for them when loading
	// the trade orders. Whatever remains is orphaned.
	unknownCancels := make(map[order.OrderID]struct{})
	for _, dbOrder := range dbOrders {
		if dbOrder.Order.Type() == order.CancelOrderType {
			unknownCancels[dbOrder.Order.ID()] = struct{}{}
		}
	}

	trackers := make(map[order.OrderID]*trackedTrade, len(dbOrders))
	excludeCancelMatches := true
	for _, dbOrder := range dbOrders {
		ord := dbOrder.Order
		oid := ord.ID()
		// Ignore cancel orders here. They'll be retrieved from LinkedOrder for
		// trade orders below.
		if ord.Type() == order.CancelOrderType {
			continue
		}

		mktID := marketName(ord.Base(), ord.Quote())
		if dc.marketConfig(mktID) == nil {
			c.log.Errorf("Active %s order retrieved for unknown market %s", oid, mktID)
			continue
		}

		var preImg order.Preimage
		copy(preImg[:], dbOrder.MetaData.Proof.Preimage)
		tracker := newTrackedTrade(dbOrder, preImg, dc, dc.marketEpochDuration(mktID), c.lockTimeTaker,
			c.lockTimeMaker, c.db, c.latencyQ, nil, nil, c.notify, c.formatDetails,
			dbOrder.MetaData.Options, dbOrder.MetaData.RedemptionReserves, dbOrder.MetaData.RefundReserves)
		trackers[dbOrder.Order.ID()] = tracker

		// Get matches.
		dbMatches, err := c.db.MatchesForOrder(oid, excludeCancelMatches)
		if err != nil {
			return nil, fmt.Errorf("error loading matches for order %s: %w", oid, err)
		}
		var makerCancel *msgjson.Match
		for _, dbMatch := range dbMatches {
			// Only trade matches are added to the matches map. Detect and skip
			// cancel order matches, which have an empty Address field.
			if dbMatch.Address == "" { // only correct for maker's cancel match
				// tracker.cancel is set from LinkedOrder with cancelTrade.
				makerCancel = &msgjson.Match{
					OrderID:  oid[:],
					MatchID:  dbMatch.MatchID[:],
					Quantity: dbMatch.Quantity,
				}
				continue
			}
			// Make sure that a taker will not prematurely send an
			// initialization until it is confirmed with the server
			// that the match is not revoked.
			checkServerRevoke := dbMatch.Side == order.Taker && dbMatch.Status == order.MakerSwapCast
			tracker.matches[dbMatch.MatchID] = &matchTracker{
				prefix:    tracker.Prefix(),
				trade:     tracker.Trade(),
				MetaMatch: *dbMatch,
				// Ensure logging on the first check of counterparty contract
				// confirms and own contract expiry.
				counterConfirms:   -1,
				lastExpireDur:     365 * 24 * time.Hour,
				checkServerRevoke: checkServerRevoke,
			}
		}

		// Load any linked cancel order.
		cancelID := tracker.metaData.LinkedOrder
		if cancelID.IsZero() {
			continue
		}
		metaCancel, err := c.db.Order(cancelID)
		if err != nil {
			c.log.Errorf("cancel order %s not found for trade %s", cancelID, oid)
			continue
		}
		co, ok := metaCancel.Order.(*order.CancelOrder)
		if !ok {
			c.log.Errorf("linked order %s is not a cancel order", cancelID)
			continue
		}
		var pimg order.Preimage
		copy(pimg[:], metaCancel.MetaData.Proof.Preimage)
		err = tracker.cancelTrade(co, pimg) // set tracker.cancel and link
		tracker.cancel.matches.maker = makerCancel
		if err != nil {
			c.log.Errorf("Error setting cancel order info %s: %v", co.ID(), err)
		}
		delete(unknownCancels, cancelID) // this one is known
		c.log.Debugf("Loaded cancel order %v for trade %v", cancelID, oid)
		// TODO: The trackedTrade.cancel.matches is not being repopulated on
		// startup. The consequences are that the Filled value will not include
		// the canceled portion, and the *CoreOrder generated by
		// coreOrderInternal will be Cancelling, but not Canceled. Instead of
		// using the matchTracker.matches msgjson.Match fields, we should be
		// storing the match data in the OrderMetaData so that it can be
		// tracked across sessions.
	}

	// Retire any remaining cancel orders that don't have active target orders.
	// This means we somehow already retired the trade, but not the cancel.
	for cid := range unknownCancels {
		c.log.Warnf("Retiring orphaned cancel order %v", cid)
		err = c.db.UpdateOrderStatus(cid, order.OrderStatusRevoked)
		if err != nil {
			c.log.Errorf("Failed to update status of orphaned cancel order %v: %v", cid, err)
		}
	}

	return trackers, nil
}

// loadDBTrades loads orders and matches from the database for the specified
// dexConnection. If there are active trades, the necessary wallets will be
// unlocked. To prevent spamming wallet connections, the 'failed' map will be
// populated with asset IDs for which the attempt to connect or unlock has
// failed. The failed map should be passed on subsequent calls for other dexes.
func (c *Core) loadDBTrades(dc *dexConnection, crypter encrypt.Crypter, failed map[uint32]struct{}) ([]*trackedTrade, error) {
	// Parse the active trades and see if any wallets need unlocking.
	trades, err := c.dbTrackers(dc)
	if err != nil {
		return nil, fmt.Errorf("error retrieving active matches: %w", err)
	}

	errs := newErrorSet(dc.acct.host + ": ")
	ready := make([]*trackedTrade, 0, len(trades))
	for _, trade := range trades {
		if !trade.isActive() {
			// In this event, there is a discrepancy between the active criteria
			// between dbTrackers and isActive that should be resolved.
			c.log.Warnf("Loaded inactive trade %v from the DB.", trade.ID())
			continue
		}
		base, quote := trade.Base(), trade.Quote()
		_, baseFailed := failed[base]
		_, quoteFailed := failed[quote]
		if !baseFailed {
			baseWallet, err := c.connectedWallet(base)
			if err != nil {
				baseFailed = true
				failed[base] = struct{}{}
				c.log.Errorf("Connecting to wallet %s failed: %v", unbip(base), err)
			} else if !baseWallet.unlocked() {
				err = baseWallet.Unlock(crypter)
				if err != nil {
					baseFailed = true
					failed[base] = struct{}{}
					c.log.Errorf("Unlock wallet %s failed: %v", unbip(base), err)
				}
			}
		}
		if !baseFailed && !quoteFailed {
			quoteWallet, err := c.connectedWallet(quote)
			if err != nil {
				quoteFailed = true
				failed[quote] = struct{}{}
				c.log.Errorf("Connecting to wallet %s failed: %v", unbip(quote), err)
			} else if !quoteWallet.unlocked() {
				err = quoteWallet.Unlock(crypter)
				if err != nil {
					quoteFailed = true
					failed[quote] = struct{}{}
					c.log.Errorf("Unlock wallet %s failed: %v", unbip(quote), err)
				}
			}
		}
		if baseFailed {
			errs.add("could not complete order %s because the wallet for %s cannot be used", trade.token(), unbip(base))
			continue
		}
		if quoteFailed {
			errs.add("could not complete order %s because the wallet for %s cannot be used", trade.token(), unbip(quote))
			continue
		}
		ready = append(ready, trade)
	}
	return ready, errs.ifAny()
}

// resumeTrades recovers the states of active trades and matches, including
// loading audit info needed to finish swaps and funding coins needed to create
// new matches on an order.
func (c *Core) resumeTrades(dc *dexConnection, trackers []*trackedTrade) assetMap {
	var tracker *trackedTrade
	notifyErr := func(topic Topic, args ...interface{}) {
		subject, detail := c.formatDetails(topic, args...)
		c.notify(newOrderNote(topic, subject, detail, db.ErrorLevel, tracker.coreOrder()))
	}

	// markUnfunded is used to allow an unfunded order to enter the trades map
	// so that status resolution and match negotiation for unaffected matches
	// may continue. By not self-revoking, the user may have the opportunity to
	// resolve any wallet issues that may have lead to a failure to find the
	// funding coins. Otherwise the server will (or already did) revoke some or
	// all of the matches and the order itself.
	markUnfunded := func(trade *trackedTrade, matches []*matchTracker) {
		// Block negotiating new matches.
		trade.changeLocked = false
		trade.coinsLocked = false
		// Block swap txn attempts on matches needing funds.
		for _, match := range matches {
			match.swapErr = errors.New("no funding coins for swap")
		}
		// Will not be retired until revoke or cancel of the order and all
		// matches, which may happen on status resolution after authenticating
		// with the DEX server, or from a revoke_match/revoke_order notification
		// after timeout. However, the order should be unconditionally canceled.
	}

	relocks := make(assetMap)
	dc.tradeMtx.Lock()
	defer dc.tradeMtx.Unlock()
	for _, tracker = range trackers {
		// Never overwrite existing trackedTrades.
		if _, found := dc.trades[tracker.ID()]; found {
			c.log.Tracef("Trade %v already running.", tracker.ID())
			continue
		}

		// See if the order is 100% filled.
		trade := tracker.Trade()
		// Make sure we have the necessary wallets.
		wallets, err := c.walletSet(dc, tracker.Base(), tracker.Quote(), trade.Sell)
		if err != nil {
			notifyErr(TopicWalletMissing, tracker.token(), err)
			continue
		}

		tracker.wallets = wallets

		// Find the least common multiplier to use as the denom for adding
		// reserve fractions.
		denom, marketMult, limitMult := lcm(uint64(len(tracker.matches)), tracker.Trade().Quantity)
		var refundNum, redeemNum uint64

		addMatchRedemption := func(match *matchTracker) {
			if tracker.isMarketBuy() {
				redeemNum += marketMult // * 1
			} else {
				redeemNum += match.Quantity * limitMult
			}
		}

		addMatchRefund := func(match *matchTracker) {
			if tracker.isMarketBuy() {
				refundNum += marketMult // * 1
			} else {
				refundNum += match.Quantity * limitMult
			}
		}

		// If matches haven't redeemed, but the counter-swap has been received,
		// reload the audit info.
		isActive := tracker.metaData.Status == order.OrderStatusBooked || tracker.metaData.Status == order.OrderStatusEpoch
		var matchesNeedingCoins []*matchTracker
		for _, match := range tracker.matches {
			var needsAuditInfo bool
			var counterSwap []byte
			if match.Side == order.Maker {
				if match.Status < order.MakerSwapCast {
					matchesNeedingCoins = append(matchesNeedingCoins, match)
				}
				if match.Status == order.TakerSwapCast {
					needsAuditInfo = true // maker needs AuditInfo for takers contract
					counterSwap = match.MetaData.Proof.TakerSwap
				}
				if match.Status < order.MakerRedeemed {
					addMatchRedemption(match)
					addMatchRefund(match)
				}
			} else { // Taker
				if match.Status < order.TakerSwapCast {
					matchesNeedingCoins = append(matchesNeedingCoins, match)
				}
				if match.Status < order.MatchComplete && match.Status >= order.MakerSwapCast {
					needsAuditInfo = true // taker needs AuditInfo for maker's contract
					counterSwap = match.MetaData.Proof.MakerSwap
				}
				if match.Status < order.MakerRedeemed {
					addMatchRefund(match)
				}
				if match.Status < order.MatchComplete {
					addMatchRedemption(match)
				}
			}
			c.log.Tracef("Trade %v match %v needs coins = %v, needs audit info = %v",
				tracker.ID(), match.MatchID, len(matchesNeedingCoins) > 0, needsAuditInfo)
			if needsAuditInfo {
				// Check for unresolvable states.
				if len(counterSwap) == 0 {
					match.swapErr = fmt.Errorf("missing counter-swap, order %s, match %s", tracker.ID(), match)
					notifyErr(TopicMatchErrorCoin, match.Side, tracker.token(), match.Status)
					continue
				}
				counterContract := match.MetaData.Proof.CounterContract
				if len(counterContract) == 0 {
					match.swapErr = fmt.Errorf("missing counter-contract, order %s, match %s", tracker.ID(), match)
					notifyErr(TopicMatchErrorContract, match.Side, tracker.token(), match.Status)
					continue
				}
				counterTxData := match.MetaData.Proof.CounterTxData

				// Note that this does not actually audit the contract's value,
				// recipient, expiration, or secret hash (if maker), as that was
				// already done when it was initially stored as CounterScript.
				auditInfo, err := wallets.toWallet.AuditContract(counterSwap, counterContract, counterTxData, true)
				if err != nil {
					// This case is unlikely to happen since the original audit
					// message handling would have passed the audit based on the
					// tx data, but it depends on the asset backend.
					contractStr := coinIDString(wallets.toAsset.ID, counterSwap)
					c.log.Warnf("Auditing for counterparty contract %v (%s): %v",
						contractStr, unbip(wallets.toAsset.ID), err)
					// Start the audit retry waiter. Set swapErr to block tick
					// actions like counterSwap.Confirmations checks while it is
					// searching since matchTracker.counterSwap is not yet set.
					// We may consider removing this if AuditContract is an
					// offline action for all wallet implementations.
					match.swapErr = fmt.Errorf("audit in progress, please wait") // don't frighten the users
					go func(tracker *trackedTrade, match *matchTracker) {
						auditInfo, err := tracker.searchAuditInfo(match, counterSwap, counterContract, counterTxData)
						tracker.mtx.Lock()
						defer tracker.mtx.Unlock()
						if err != nil { // contract data could be bad, or just already spent (refunded)
							match.swapErr = fmt.Errorf("audit error: %w", err)
							// NOTE: This behaviour differs from the audit request handler behaviour for failed audits.
							// handleAuditRoute does NOT set a swapErr in case a revised audit request is received from
							// the server. Audit requests are currently NOT resent, so this difference is trivial. IF
							// a revised audit request did come through though, no further actions will be taken for this
							// match even if the revised audit passes validation.
							c.log.Debugf("AuditContract error for match %v status %v, refunded = %v, revoked = %v: %v",
								match, match.Status, len(match.MetaData.Proof.RefundCoin) > 0,
								match.MetaData.Proof.IsRevoked(), err)
							subject, detail := c.formatDetails(TopicMatchRecoveryError,
								unbip(wallets.toAsset.ID), contractStr, tracker.token(), err)
							c.notify(newOrderNote(TopicMatchRecoveryError, subject, detail,
								db.ErrorLevel, tracker.coreOrderInternal())) // tracker.mtx already locked
							// The match may be revoked by server. Only refund possible now.
							return
						}
						match.counterSwap = auditInfo
						match.swapErr = nil // unblock tick actions
						c.log.Infof("Successfully re-validated counterparty contract %v (%s)",
							contractStr, unbip(wallets.toAsset.ID))
					}(tracker, match)

					continue // leave auditInfo nil
				}
				match.counterSwap = auditInfo
				continue
			}
		}

		if refundNum != 0 {
			tracker.lockRefundFraction(refundNum, denom)
		}
		if redeemNum != 0 {
			tracker.lockRedemptionFraction(redeemNum, denom)
		}

		// Active orders and orders with matches with unsent swaps need funding
		// coin(s). If they are not found, block new matches and swap attempts.
		needsCoins := len(matchesNeedingCoins) > 0
		if isActive || needsCoins {
			coinIDs := trade.Coins
			if len(tracker.metaData.ChangeCoin) != 0 {
				coinIDs = []order.CoinID{tracker.metaData.ChangeCoin}
			}
			tracker.coins = map[string]asset.Coin{} // should already be
			if len(coinIDs) == 0 {
				notifyErr(TopicOrderCoinError, tracker.token())
				markUnfunded(tracker, matchesNeedingCoins) // bug - no user resolution
			} else {
				byteIDs := make([]dex.Bytes, 0, len(coinIDs))
				for _, cid := range coinIDs {
					byteIDs = append(byteIDs, []byte(cid))
				}
				coins, err := wallets.fromWallet.FundingCoins(byteIDs)
				if err != nil || len(coins) == 0 {
					notifyErr(TopicOrderCoinFetchError, tracker.token(), unbip(wallets.fromAsset.ID), err)
					// Block matches needing funding coins.
					markUnfunded(tracker, matchesNeedingCoins)
					// Note: tracker is still added to trades map for (1) status
					// resolution, (2) continued settlement of matches that no
					// longer require funding coins, and (3) cancellation in
					// authDEX if the order is booked.
					c.log.Warnf("Check the status of your %s wallet and the coins logged above! "+
						"Resolve the wallet issue if possible and restart the DEX client.",
						strings.ToUpper(unbip(wallets.fromAsset.ID)))
					c.log.Warnf("Unfunded order %v will be canceled on connect, but %d active matches need funding coins!",
						tracker.ID(), len(matchesNeedingCoins))
					// If the funding coins are spent or inaccessible, the user
					// can only wait for match revocation.
				} else {
					// NOTE: change and changeLocked are not set even if the
					// funding coins were loaded from the DB's ChangeCoin.
					tracker.coinsLocked = true
					tracker.coins = mapifyCoins(coins)
				}
			}
		}

		tracker.recalcFilled()

		if isActive {
			tracker.lockRedemptionFraction(trade.Remaining(), trade.Quantity)
			tracker.lockRefundFraction(trade.Remaining(), trade.Quantity)
		}

		// Balances should be updated for any orders with locked wallet coins,
		// or orders with funds locked in contracts.
		if isActive || needsCoins || tracker.unspentContractAmounts() > 0 {
			relocks.count(wallets.fromAsset.ID)
		}

		dc.trades[tracker.ID()] = tracker
		c.notify(newOrderNote(TopicOrderLoaded, "", "", db.Data, tracker.coreOrder()))
	}
	return relocks
}

// reReserveFunding reserves funding coins for a newly instantiated wallet.
// reReserveFunding is closely modeled on resumeTrades, so see resumeTrades for
// docs.
func (c *Core) reReserveFunding(w *xcWallet) {

	markUnfunded := func(trade *trackedTrade, matches []*matchTracker) {
		trade.changeLocked = false
		trade.coinsLocked = false
		for _, match := range matches {
			match.swapErr = errors.New("no funding coins for swap")
		}
	}

	for _, dc := range c.dexConnections() {
		for _, tracker := range dc.trackedTrades() {
			// TODO: Consider tokens
			if tracker.Base() != w.AssetID && tracker.Quote() != w.AssetID {
				continue
			}

			notifyErr := func(topic Topic, args ...interface{}) {
				subject, detail := c.formatDetails(topic, args...)
				c.notify(newOrderNote(topic, subject, detail, db.ErrorLevel, tracker.coreOrderInternal()))
			}

			trade := tracker.Trade()

			fromID := tracker.Quote()
			if trade.Sell {
				fromID = tracker.Base()
			}

			denom, marketMult, limitMult := lcm(uint64(len(tracker.matches)), trade.Quantity)
			var refundNum, redeemNum uint64

			addMatchRedemption := func(match *matchTracker) {
				if tracker.isMarketBuy() {
					redeemNum += marketMult // * 1
				} else {
					redeemNum += match.Quantity * limitMult
				}
			}

			addMatchRefund := func(match *matchTracker) {
				if tracker.isMarketBuy() {
					refundNum += marketMult // * 1
				} else {
					refundNum += match.Quantity * limitMult
				}
			}

			isActive := tracker.metaData.Status == order.OrderStatusBooked || tracker.metaData.Status == order.OrderStatusEpoch
			var matchesNeedingCoins []*matchTracker
			for _, match := range tracker.matches {
				if match.Side == order.Maker {
					if match.Status < order.MakerSwapCast {
						matchesNeedingCoins = append(matchesNeedingCoins, match)
					}
					if match.Status < order.MakerRedeemed {
						addMatchRedemption(match)
						addMatchRefund(match)
					}
				} else { // Taker
					if match.Status < order.TakerSwapCast {
						matchesNeedingCoins = append(matchesNeedingCoins, match)
					}
					if match.Status < order.MakerRedeemed {
						addMatchRefund(match)
					}
					if match.Status < order.MatchComplete {
						addMatchRedemption(match)
					}
				}
			}

			if c.ctx.Err() != nil {
				return
			}

			// Prepare funding coins, but don't update tracker until the mutex
			// is locked.
			needsCoins := len(matchesNeedingCoins) > 0
			// nil coins = no locking required, empty coins = something went
			// wrong, non-empty means locking required.
			var coins asset.Coins
			if fromID == w.AssetID && (isActive || needsCoins) {
				coins = []asset.Coin{} // should already be
				coinIDs := trade.Coins
				if len(tracker.metaData.ChangeCoin) != 0 {
					coinIDs = []order.CoinID{tracker.metaData.ChangeCoin}
				}
				if len(coinIDs) == 0 {
					notifyErr(TopicOrderCoinError, tracker.token())
					markUnfunded(tracker, matchesNeedingCoins) // bug - no user resolution
				} else {
					byteIDs := make([]dex.Bytes, 0, len(coinIDs))
					for _, cid := range coinIDs {
						byteIDs = append(byteIDs, []byte(cid))
					}
					var err error
					coins, err = w.FundingCoins(byteIDs)
					if err != nil || len(coins) == 0 {
						notifyErr(TopicOrderCoinFetchError, tracker.token(), unbip(fromID), err)
						c.log.Warnf("(re-reserve) Check the status of your %s wallet and the coins logged above! "+
							"Resolve the wallet issue if possible and restart the DEX client.",
							strings.ToUpper(unbip(fromID)))
						c.log.Warnf("(re-reserve) Unfunded order %v will be revoked if %d active matches don't get funding coins!",
							tracker.ID(), len(matchesNeedingCoins))
					}
				}
			}

			tracker.mtx.Lock()

			// Refund and redemption reserves for active matches. Doing this
			// under mutex lock, but noting that the underlying calls to
			// ReReserveRedemption and ReReserveRefund could potentially involve
			// long-running RPC calls.
			if fromID == w.AssetID {
				tracker.refundLocked = 0
				if refundNum != 0 {
					tracker.lockRefundFraction(refundNum, denom)
				}
			} else {
				tracker.redemptionLocked = 0
				if redeemNum != 0 {
					tracker.lockRedemptionFraction(redeemNum, denom)
				}
			}

			// Funding coins
			if coins != nil {
				tracker.coinsLocked = len(coins) > 0
				tracker.coins = mapifyCoins(coins)
			}

			// Refund and redemption reserves for booked orders.

			tracker.recalcFilled() // Make sure Remaining is accurate.

			if isActive {
				if fromID == w.AssetID {
					tracker.lockRefundFraction(trade.Remaining(), trade.Quantity)
				} else {
					tracker.lockRedemptionFraction(trade.Remaining(), trade.Quantity)
				}
			}

			tracker.mtx.Unlock()
		}
	}
}

// generateDEXMaps creates the associated assets, market and epoch maps of the
// DEXs from the provided configuration.
func generateDEXMaps(host string, cfg *msgjson.ConfigResult) (map[uint32]*dex.Asset, map[string]uint64, error) {
	assets := make(map[uint32]*dex.Asset, len(cfg.Assets))
	for _, asset := range cfg.Assets {
		assets[asset.ID] = convertAssetInfo(asset)
	}
	// Validate the markets so we don't have to check every time later.
	for _, mkt := range cfg.Markets {
		_, ok := assets[mkt.Base]
		if !ok {
			return nil, nil, fmt.Errorf("%s reported a market with base "+
				"asset %d, but did not provide the asset info.", host, mkt.Base)
		}
		_, ok = assets[mkt.Quote]
		if !ok {
			return nil, nil, fmt.Errorf("%s reported a market with quote "+
				"asset %d, but did not provide the asset info.", host, mkt.Quote)
		}
	}

	epochMap := make(map[string]uint64)
	for _, mkt := range cfg.Markets {
		epochMap[mkt.Name] = 0
	}

	return assets, epochMap, nil
}

// runMatches runs the sorted matches returned from parseMatches.
func (c *Core) runMatches(tradeMatches map[order.OrderID]*serverMatches) (assetMap, error) {
	runMatch := func(sm *serverMatches) (assetMap, error) {
		updatedAssets := make(assetMap)
		tracker := sm.tracker
		oid := tracker.ID()

		// Verify and record any cancel Match targeting this trade.
		if sm.cancel != nil {
			err := tracker.processCancelMatch(sm.cancel)
			if err != nil {
				return updatedAssets, fmt.Errorf("processCancelMatch for cancel order %v targeting order %v failed: %w",
					sm.cancel.OrderID, oid, err)
			}
		}

		// Begin negotiation for any trade Matches.
		if len(sm.msgMatches) > 0 {
			tracker.mtx.Lock()
			err := tracker.negotiate(sm.msgMatches)
			tracker.mtx.Unlock()
			if err != nil {
				return updatedAssets, fmt.Errorf("negotiate order %v matches failed: %w", oid, err)
			}

			// Coins may be returned for canceled orders.
			tracker.mtx.RLock()
			if tracker.metaData.Status == order.OrderStatusCanceled {
				updatedAssets.count(tracker.fromAssetID)
			}
			tracker.mtx.RUnlock()

			// Try to tick the trade now, but do not interrupt on error. The
			// trade will tick again automatically.
			tickUpdatedAssets, err := c.tick(tracker)
			updatedAssets.merge(tickUpdatedAssets)
			if err != nil {
				return updatedAssets, fmt.Errorf("tick of order %v failed: %w", oid, err)
			}
		}

		return updatedAssets, nil
	}

	// Process the trades concurrently.
	type runMatchResult struct {
		updatedAssets assetMap
		err           error
	}
	resultChan := make(chan *runMatchResult)
	for _, trade := range tradeMatches {
		go func(trade *serverMatches) {
			assetsUpdated, err := runMatch(trade)
			resultChan <- &runMatchResult{assetsUpdated, err}
		}(trade)
	}

	errs := newErrorSet("runMatches - ")
	assetsUpdated := make(assetMap)
	for range tradeMatches {
		result := <-resultChan
		assetsUpdated.merge(result.updatedAssets) // assets might be updated even if an error occurs
		if result.err != nil {
			errs.addErr(result.err)
		}
	}

	return assetsUpdated, errs.ifAny()
}

// sendOutdatedClientNotification will send a notification to the UI that
// indicates the client should be updated to be used with this DEX server.
func sendOutdatedClientNotification(c *Core, dc *dexConnection) {
	subject, details := c.formatDetails(TopicUpgradeNeeded, dc.acct.host)
	c.notify(newUpgradeNote(TopicUpgradeNeeded, subject, details, db.WarningLevel))
}

func isOnionHost(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	return strings.HasSuffix(host, ".onion")
}

// connectDEX establishes a ws connection to a DEX server using the provided
// account info, but does not authenticate the connection through the 'connect'
// route. If temporary is provided and true, the c.listen(dc) goroutine is not
// started so that associated trades are not processed and no incoming requests
// and notifications are handled. A temporary dexConnection may be used to
// inspect the config response or check if a (paid) HD account exists with a DEX.
func (c *Core) connectDEX(acctInfo *db.AccountInfo, temporary ...bool) (*dexConnection, error) {
	// Get the host from the DEX URL.
	host, err := addrHost(acctInfo.Host)
	if err != nil {
		return nil, newError(addressParseErr, "error parsing address: %v", err)
	}
	// The scheme switches gorilla/websocket to use the tls.Config or not.
	scheme := "wss"
	if len(acctInfo.Cert) == 0 {
		scheme = "ws" // only supported for .onion hosts, but could allow private IP too
	}
	wsAddr := scheme + "://" + host + "/ws"
	wsURL, err := url.Parse(wsAddr)
	if err != nil {
		return nil, newError(addressParseErr, "error parsing ws address %s: %w", wsAddr, err)
	}

	listen := len(temporary) == 0 || !temporary[0]

	var reporting uint32
	if listen {
		reporting = 1
	}

	dc := &dexConnection{
		log:               c.log,
		acct:              newDEXAccount(acctInfo),
		notify:            c.notify,
		ticker:            newDexTicker(defaultTickInterval), // updated when server config obtained
		books:             make(map[string]*bookie),
		trades:            make(map[order.OrderID]*trackedTrade),
		apiVer:            -1,
		reportingConnects: reporting,
		spots:             make(map[string]*msgjson.Spot),
		connectionStatus:  uint32(comms.Disconnected),
		// On connect, must set: cfg, epoch, and assets.
	}

	wsCfg := comms.WsCfg{
		URL:      wsURL.String(),
		PingWait: 20 * time.Second, // larger than server's pingPeriod (server/comms/server.go)
		Cert:     acctInfo.Cert,
		Logger:   c.log.SubLogger(wsURL.String()),
	}

	isOnionHost := isOnionHost(wsURL.Host)
	if isOnionHost || c.cfg.TorProxy != "" {
		proxyAddr := c.cfg.TorProxy
		if isOnionHost {
			if c.cfg.Onion == "" {
				return nil, errors.New("tor must be configured for .onion addresses")
			}
			proxyAddr = c.cfg.Onion
		}
		proxy := &socks.Proxy{
			Addr:         proxyAddr,
			TorIsolation: c.cfg.TorIsolation, // need socks.NewPool with isolation???
		}
		wsCfg.NetDialContext = proxy.DialContext
	}
	if scheme == "ws" && !isOnionHost {
		return nil, errors.New("a TLS connection is required when not using a hidden service")
	}

	wsCfg.ConnectEventFunc = func(status comms.ConnectionStatus) {
		c.handleConnectEvent(dc, status)
	}
	wsCfg.ReconnectSync = func() {
		go c.handleReconnect(host)
	}

	// Create a websocket connection to the server.
	conn, err := c.wsConstructor(&wsCfg)
	if err != nil {
		return nil, err
	}

	dc.WsConn = conn
	dc.connMaster = dex.NewConnectionMaster(conn)

	// At this point, we have a valid dexConnection object whether or not we can
	// actually connect. In any return below, we return the dexConnection so it
	// may be tracked in the c.conns map (and listed as a known DEX). TODO:
	// split the above code into a dexConnection constructor, and the below into
	// a startDexConnection function so we don't have the anti-pattern of
	// returning a non-nil object with a non-nil error and requiring the caller
	// to check both!

	// Start listening for messages. The listener stops when core shuts down or
	// the dexConnection's ConnectionMaster is shut down. This goroutine should
	// be started as long as the reconnect loop is running. It only returns when
	// the wsConn is stopped.
	if listen {
		c.wg.Add(1)
		go c.listen(dc)
	}

	err = dc.connMaster.Connect(c.ctx)
	if err != nil {
		// Not connected, but reconnect cycle is running. Caller should track
		// this dexConnection, and a listen goroutine must be running to handle
		// messages received when the connection is eventually established.
		return dc, err
	}

	// Request the market configuration.
	err = dc.refreshServerConfig() // handleReconnect must too
	if err != nil {
		if errors.Is(err, outdatedClientErr) {
			sendOutdatedClientNotification(c, dc)
		}
		return dc, err
	}
	// handleConnectEvent sets dc.connected, even on first connect

	if listen {
		c.log.Infof("Connected to DEX server at %s and listening for messages.", host)
		go dc.subPriceFeed()
	} else {
		c.log.Infof("Connected to DEX server at %s but NOT listening for messages.", host)
	}

	return dc, nil
}

// handleReconnect is called when a WsConn indicates that a lost connection has
// been re-established.
func (c *Core) handleReconnect(host string) {
	c.connMtx.RLock()
	dc, found := c.conns[host]
	c.connMtx.RUnlock()
	if !found {
		c.log.Errorf("handleReconnect: Unable to find previous connection to DEX at %s", host)
		return
	}

	// The server's configuration may have changed, so retrieve the current
	// server configuration.
	err := dc.refreshServerConfig()
	if err != nil {
		if errors.Is(err, outdatedClientErr) {
			sendOutdatedClientNotification(c, dc)
		}
		c.log.Errorf("handleReconnect: Unable to apply new configuration for DEX at %s: %v", host, err)
		return
	}

	go dc.subPriceFeed()

	if !dc.acct.locked() && dc.acct.feePaid() {
		err = c.authDEX(dc)
		if err != nil {
			c.log.Errorf("handleReconnect: Unable to authorize DEX at %s: %v", host, err)
			return
		}
	} else {
		c.log.Infof("Connection to %v established, but you still need to login.", host)
		// Continue to resubscribe to market fees.
	}

	type market struct {
		name  string
		base  uint32
		quote uint32
	}

	resubMkt := func(mkt *market) {
		// Locate any bookie for this market.
		booky := dc.bookie(mkt.name)
		if booky == nil {
			// Was not previously subscribed with the server for this market.
			return
		}

		// Resubscribe since our old subscription was probably lost by the
		// server when the connection dropped.
		snap, err := dc.subscribe(mkt.base, mkt.quote)
		if err != nil {
			c.log.Errorf("handleReconnect: Failed to Subscribe to market %q 'orderbook': %v", mkt.name, err)
			return
		}

		// Create a fresh OrderBook for the bookie.
		err = booky.Reset(snap)
		if err != nil {
			c.log.Errorf("handleReconnect: Failed to Sync market %q order book snapshot: %v", mkt.name, err)
		}

		// Send a FreshBookAction to the subscribers.
		booky.send(&BookUpdate{
			Action:   FreshBookAction,
			Host:     dc.acct.host,
			MarketID: mkt.name,
			Payload: &MarketOrderBook{
				Base:  mkt.base,
				Quote: mkt.quote,
				Book:  booky.book(),
			},
		})
	}

	// Create a list of books to check.
	dc.cfgMtx.RLock()
	mkts := make([]*market, 0, len(dc.cfg.Markets))
	for _, m := range dc.cfg.Markets {
		mkts = append(mkts, &market{
			name:  m.Name,
			base:  m.Base,
			quote: m.Quote,
		})
	}
	dc.cfgMtx.RUnlock()

	// For each market, resubscribe to any market books.
	for _, mkt := range mkts {
		resubMkt(mkt)
	}
}

func (dc *dexConnection) broadcastingConnect() bool {
	return atomic.LoadUint32(&dc.reportingConnects) == 1
}

// handleConnectEvent is called when a WsConn indicates that a connection was
// lost or established.
//
// NOTE: Disconnect event notifications may lag behind actual disconnections.
func (c *Core) handleConnectEvent(dc *dexConnection, status comms.ConnectionStatus) {
	topic := TopicDEXDisconnected
	if status == comms.Connected {
		topic = TopicDEXConnected
	} else {
		for _, tracker := range dc.trackedTrades() {
			tracker.mtx.RLock()
			for _, match := range tracker.matches {
				// Make sure that a taker will not prematurely send an
				// initialization until it is confirmed with the server
				// that the match is not revoked.
				if match.Side == order.Taker && match.Status == order.MakerSwapCast {
					match.exceptionMtx.Lock()
					match.checkServerRevoke = true
					match.exceptionMtx.Unlock()
				}
			}
			tracker.mtx.RUnlock()
		}
	}
	atomic.StoreUint32(&dc.connectionStatus, uint32(status))
	if dc.broadcastingConnect() {
		subject, details := c.formatDetails(topic, dc.acct.host)
		dc.notify(newConnEventNote(topic, subject, dc.acct.host, status, details, db.Poke))
	}
}

// handleMatchProofMsg is called when a match_proof notification is received.
func handleMatchProofMsg(c *Core, dc *dexConnection, msg *msgjson.Message) error {
	var note msgjson.MatchProofNote
	err := msg.Unmarshal(&note)
	if err != nil {
		return fmt.Errorf("match proof note unmarshal error: %w", err)
	}

	// Expire the epoch
	dc.setEpoch(note.MarketID, note.Epoch+1)

	book := dc.bookie(note.MarketID)
	if book == nil {
		return fmt.Errorf("no order book found with market id %q",
			note.MarketID)
	}

	err = book.ValidateMatchProof(note)
	if err != nil {
		return fmt.Errorf("match proof validation failed: %w", err)
	}

	// Validate match_proof commitment checksum for client orders in this epoch.
	for _, trade := range dc.trackedTrades() {
		if note.MarketID != trade.mktID {
			continue
		}

		// Validation can fail either due to server trying to cheat (by
		// requesting a preimage before closing the epoch to more orders), or
		// client losing trades' epoch csums (e.g. due to restarting, since we
		// don't persistently store these at the moment).
		//
		// Just warning the user for now, later on we might wanna revoke the
		// order if this happens.
		if err = trade.verifyCSum(note.CSum, note.Epoch); err != nil {
			c.log.Warnf("Failed to validate commitment checksum for %s epoch %d at %s: %v"+
				note.MarketID, note.Epoch, dc.acct.host, err)
		}
	}

	return nil
}

// handleRevokeOrderMsg is called when a revoke_order message is received.
func handleRevokeOrderMsg(c *Core, dc *dexConnection, msg *msgjson.Message) error {
	var revocation msgjson.RevokeOrder
	err := msg.Unmarshal(&revocation)
	if err != nil {
		return fmt.Errorf("revoke order unmarshal error: %w", err)
	}

	var oid order.OrderID
	copy(oid[:], revocation.OrderID)

	tracker, _, _ := dc.findOrder(oid)
	if tracker == nil {
		return fmt.Errorf("no order found with id %s", oid.String())
	}

	tracker.revoke()

	subject, details := c.formatDetails(TopicOrderRevoked, tracker.token(), tracker.mktID, dc.acct.host)
	c.notify(newOrderNote(TopicOrderRevoked, subject, details, db.ErrorLevel, tracker.coreOrder()))

	// Update market orders, and the balance to account for unlocked coins.
	c.updateAssetBalance(tracker.fromAssetID)
	return nil
}

// handleRevokeMatchMsg is called when a revoke_match message is received.
func handleRevokeMatchMsg(c *Core, dc *dexConnection, msg *msgjson.Message) error {
	var revocation msgjson.RevokeMatch
	err := msg.Unmarshal(&revocation)
	if err != nil {
		return fmt.Errorf("revoke match unmarshal error: %w", err)
	}

	var oid order.OrderID
	copy(oid[:], revocation.OrderID)

	tracker, _, _ := dc.findOrder(oid)
	if tracker == nil {
		return fmt.Errorf("no order found with id %s (not an error if you've completed your side of the swap)", oid.String())
	}

	if len(revocation.MatchID) != order.MatchIDSize {
		return fmt.Errorf("invalid match ID %v", revocation.MatchID)
	}

	var matchID order.MatchID
	copy(matchID[:], revocation.MatchID)

	tracker.mtx.Lock()
	err = tracker.revokeMatch(matchID, true)
	tracker.mtx.Unlock()
	if err != nil {
		return fmt.Errorf("unable to revoke match %s for order %s: %w", matchID, tracker.ID(), err)
	}

	// Update market orders, and the balance to account for unlocked coins.
	c.updateAssetBalance(tracker.fromAssetID)
	return nil
}

// handleNotifyMsg is called when a notify notification is received.
func handleNotifyMsg(c *Core, dc *dexConnection, msg *msgjson.Message) error {
	var txt string
	err := msg.Unmarshal(&txt)
	if err != nil {
		return fmt.Errorf("notify unmarshal error: %w", err)
	}
	subject, details := c.formatDetails(TopicDEXNotification, dc.acct.host, txt)
	c.notify(newServerNotifyNote(TopicDEXNotification, subject, details, db.WarningLevel))
	return nil
}

// handlePenaltyMsg is called when a Penalty notification is received.
//
// TODO: Consider other steps needed to take immediately after being banned.
func handlePenaltyMsg(c *Core, dc *dexConnection, msg *msgjson.Message) error {
	var note msgjson.PenaltyNote
	err := msg.Unmarshal(&note)
	if err != nil {
		return fmt.Errorf("penalty note unmarshal error: %w", err)
	}
	// Check the signature.
	err = dc.acct.checkSig(note.Serialize(), note.Sig)
	if err != nil {
		return newError(signatureErr, "handlePenaltyMsg: DEX signature validation error: %w", err)
	}
	t := time.UnixMilli(int64(note.Penalty.Time))
	// d := time.Duration(note.Penalty.Duration) * time.Millisecond

	subject, details := c.formatDetails(TopicPenalized, dc.acct.host, note.Penalty.Rule, t, note.Penalty.Details)
	c.notify(newServerNotifyNote(TopicPenalized, subject, details, db.WarningLevel))
	return nil
}

// routeHandler is a handler for a message from the DEX.
type routeHandler func(*Core, *dexConnection, *msgjson.Message) error

var reqHandlers = map[string]routeHandler{
	msgjson.PreimageRoute:   handlePreimageRequest,
	msgjson.MatchRoute:      handleMatchRoute,
	msgjson.AuditRoute:      handleAuditRoute,
	msgjson.RedemptionRoute: handleRedemptionRoute, // TODO: to ntfn
}

var noteHandlers = map[string]routeHandler{
	msgjson.MatchProofRoute:      handleMatchProofMsg,
	msgjson.BookOrderRoute:       handleBookOrderMsg,
	msgjson.EpochOrderRoute:      handleEpochOrderMsg,
	msgjson.UnbookOrderRoute:     handleUnbookOrderMsg,
	msgjson.PriceUpdateRoute:     handlePriceUpdateNote,
	msgjson.UpdateRemainingRoute: handleUpdateRemainingMsg,
	msgjson.EpochReportRoute:     handleEpochReportMsg,
	msgjson.SuspensionRoute:      handleTradeSuspensionMsg,
	msgjson.ResumptionRoute:      handleTradeResumptionMsg,
	msgjson.NotifyRoute:          handleNotifyMsg,
	msgjson.PenaltyRoute:         handlePenaltyMsg,
	msgjson.NoMatchRoute:         handleNoMatchRoute,
	msgjson.RevokeOrderRoute:     handleRevokeOrderMsg,
	msgjson.RevokeMatchRoute:     handleRevokeMatchMsg,
}

// listen monitors the DEX websocket connection for server requests and
// notifications. This should be run as a goroutine. listen will return when
// either c.ctx is canceled or the Message channel from the dexConnection's
// MessageSource method is closed. The latter would be the case when the
// dexConnection's WsConn is shut down / ConnectionMaster stopped.
func (c *Core) listen(dc *dexConnection) {
	defer c.wg.Done()
	msgs := dc.MessageSource() // dc.connMaster.Disconnect closes it e.g. cancel of client/comms.(*wsConn).Connect

	defer dc.ticker.Stop()
	lastTick := time.Now()

	// Messages must be run in the order in which they are received, but they
	// should not be blocking or run concurrently. TODO: figure out which if any
	// can run asynchronously, maybe all.
	type msgJob struct {
		hander routeHandler
		msg    *msgjson.Message
	}
	runJob := func(job *msgJob) {
		tStart := time.Now()
		defer func() {
			if pv := recover(); pv != nil {
				c.log.Criticalf("Uh-oh! Panic while handling message from %v.\n\n"+
					"Message:\n\n%#v\n\nPanic:\n\n%v\n\nStack:\n\n%v\n\n",
					dc.acct.host, job.msg, pv, string(debug.Stack()))
			}
			if eTime := time.Since(tStart); eTime > 250*time.Millisecond {
				c.log.Infof("runJob(%v) completed in %v", job.msg.Route, eTime)
			}
		}()
		if err := job.hander(c, dc, job.msg); err != nil {
			c.log.Errorf("Route '%v' %v handler error (DEX %s): %v", job.msg.Route,
				job.msg.Type, dc.acct.host, err)
		}
	}
	// Start a single runner goroutine to run jobs one at a time in the order
	// that they were received. Include the handler goroutine in the WaitGroup
	// to allow it to complete if the connection master desires.
	nextJob := make(chan *msgJob, 1024) // start blocking at this cap
	defer close(nextJob)
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		for job := range nextJob {
			runJob(job)
		}
	}()

	checkTrades := func() {
		// checkTrades should be snappy. If it takes too long we are creating
		// lock contention.
		tStart := time.Now()
		defer func() {
			if eTime := time.Since(tStart); eTime > 250*time.Millisecond {
				c.log.Warnf("checkTrades completed in %v (slow)", eTime)
			}
		}()

		var doneTrades, activeTrades []*trackedTrade
		// NOTE: Don't lock tradeMtx while also locking a trackedTrade's mtx
		// since we risk blocking access to the trades map if there is lock
		// contention for even one trade.
		for _, trade := range dc.trackedTrades() {
			if trade.isActive() {
				activeTrades = append(activeTrades, trade)
				continue
			}
			doneTrades = append(doneTrades, trade)
		}

		if len(doneTrades) > 0 {
			dc.tradeMtx.Lock()

			for _, trade := range doneTrades {
				// Log an error if redemption funds are still reserved.
				trade.mtx.RLock()
				redeemLocked := trade.redemptionLocked
				refundLocked := trade.refundLocked
				trade.mtx.RUnlock()
				if redeemLocked > 0 {
					dc.log.Errorf("retiring order %s with %d > 0 redemption funds locked", trade.ID(), redeemLocked)
				}
				if refundLocked > 0 {
					dc.log.Errorf("retiring order %s with %d > 0 refund funds locked", trade.ID(), refundLocked)
				}

				c.notify(newOrderNote(TopicOrderRetired, "", "", db.Data, trade.coreOrder()))
				delete(dc.trades, trade.ID())
			}
			dc.tradeMtx.Unlock()
		}

		// Unlock funding coins for retired orders for good measure, in case
		// there were not unlocked at an earlier time.
		updatedAssets := make(assetMap)
		for _, trade := range doneTrades {
			trade.mtx.Lock()
			c.log.Infof("Retiring inactive order %v in status %v", trade.ID(), trade.metaData.Status)
			trade.returnCoins()
			trade.mtx.Unlock()
			updatedAssets.count(trade.wallets.fromAsset.ID)
		}

		for _, trade := range activeTrades {
			if c.ctx.Err() != nil { // don't fail each one in sequence if shutting down
				return
			}
			newUpdates, err := c.tick(trade)
			if err != nil {
				c.log.Error(err)
			}
			updatedAssets.merge(newUpdates)
		}

		if len(updatedAssets) > 0 {
			c.updateBalances(updatedAssets)
		}
	}

	stopTicks := make(chan struct{})
	defer close(stopTicks)
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		for {
			select {
			case <-dc.ticker.C:
				sinceLast := time.Since(lastTick)
				lastTick = time.Now()
				if sinceLast >= 2*dc.ticker.Dur() {
					// The app likely just woke up from being suspended. Skip this
					// tick to let DEX connections reconnect and resync matches.
					c.log.Warnf("Long delay since previous trade check (just resumed?): %v. "+
						"Skipping this check to allow reconnect.", sinceLast)
					continue
				}

				checkTrades()
			case <-stopTicks:
				return
			case <-c.ctx.Done():
				return
			}
		}
	}()

out:
	for {
		select {
		case msg, ok := <-msgs:
			if !ok {
				c.log.Debugf("listen(dc): Connection terminated for %s.", dc.acct.host)
				// TODO: This just means that wsConn, which created the
				// MessageSource channel, was shut down before this loop
				// returned via ctx.Done. It may be necessary to investigate the
				// most appropriate normal shutdown sequence (i.e. close all
				// connections before stopping Core).
				return
			}

			var handler routeHandler
			var found bool
			switch msg.Type {
			case msgjson.Request:
				handler, found = reqHandlers[msg.Route]
			case msgjson.Notification:
				handler, found = noteHandlers[msg.Route]
			case msgjson.Response:
				// client/comms.wsConn handles responses to requests we sent.
				c.log.Errorf("A response was received in the message queue: %s", msg)
				continue
			default:
				c.log.Errorf("Invalid message type %d from MessageSource", msg.Type)
				continue
			}
			// Until all the routes have handlers, check for nil too.
			if !found || handler == nil {
				c.log.Errorf("No handler found for route '%s'", msg.Route)
				continue
			}

			// Queue the handling of this message.
			nextJob <- &msgJob{handler, msg}

		case <-c.ctx.Done():
			break out
		}
	}
}

// handlePreimageRequest handles a DEX-originating request for an order
// preimage. If the order id in the request is not known, it may launch a
// goroutine to wait for a market/limit/cancel request to finish processing.
func handlePreimageRequest(c *Core, dc *dexConnection, msg *msgjson.Message) error {
	req := new(msgjson.PreimageRequest)
	err := msg.Unmarshal(req)
	if err != nil {
		return fmt.Errorf("preimage request parsing error: %w", err)
	}

	oid, err := order.IDFromBytes(req.OrderID)
	if err != nil {
		return err
	}

	// NEW protocol with commitment specified.
	if len(req.Commitment) == order.CommitmentSize {
		// See if we recognize that commitment, and if we do, just wait for the
		// order ID, and process the request.
		var commit order.Commitment
		copy(commit[:], req.Commitment)

		c.sentCommitsMtx.Lock()
		defer c.sentCommitsMtx.Unlock()
		commitSig, found := c.sentCommits[commit]
		if !found { // this is the main benefit of a commitment index
			return fmt.Errorf("received preimage request for unknown commitment %v, order %v",
				req.Commitment, oid)
		}
		delete(c.sentCommits, commit)

		dc.log.Debugf("Received preimage request for order %v with known commitment %v", oid, commit)

		// Go async while waiting.
		go func() {
			// Order request success OR fail closes the channel.
			<-commitSig
			if err := processPreimageRequest(c, dc, msg.ID, oid, req.CommitChecksum); err != nil {
				c.log.Errorf("async processPreimageRequest for %v failed: %v", oid, err)
			} else {
				c.log.Debugf("async processPreimageRequest for %v succeeded", oid)
			}

			// There or not, delete this oid entry from the deprecated map.
			c.piSyncMtx.Lock()
			delete(c.piSyncers, oid)
			c.piSyncMtx.Unlock()
		}()

		return nil
	} // else no or invalid commitment, eventually error (v0 DEPRECATION)

	// OLD protocol below without the commitment is DEPRECATED. Remove when
	// protocol version reaches 1.

	// Sync with order placement, the response from which provides the order ID.
	c.piSyncMtx.Lock()
	if _, found := c.piSyncers[oid]; found {
		// If we found a map entry, the tracker is already in the trades map,
		// and we can go ahead.
		delete(c.piSyncers, oid)
		c.piSyncMtx.Unlock()
		dc.log.Debugf("Received preimage request for known order %v", oid)
		return processPreimageRequest(c, dc, msg.ID, oid, req.CommitChecksum)
	}

	// If we didn't find an entry, Trade or Cancel is still running. Add a chan
	// and wait for Trade to close it.
	syncChan := make(chan struct{})
	c.piSyncers[oid] = syncChan
	c.piSyncMtx.Unlock()

	// The order submission could be timing out waiting for a response, or this
	// could be a bogus preimage request, so we do not want to block the caller,
	// (*Core).listen if this hangs. Preimage requests are ok to handle
	// asynchronously since there can be no matches until we respond to this.
	c.log.Warnf("Received preimage request for %v with no corresponding order submission response! Waiting...", oid)
	go func() {
		select {
		case <-syncChan:
			if err := processPreimageRequest(c, dc, msg.ID, oid, req.CommitChecksum); err != nil {
				c.log.Errorf("async processPreimageRequest for %v failed: %v", oid, err)
			} else {
				c.log.Debugf("async processPreimageRequest for %v succeeded", oid)
			}
			// The channel is deleted from the piSyncers map by syncOrderPlaced.
			return
		case <-time.After(DefaultResponseTimeout):
			c.log.Errorf("Timed out syncing preimage request from %s, order %s", dc.acct.host, oid)
		case <-c.ctx.Done():
		}
		c.piSyncMtx.Lock()
		delete(c.piSyncers, oid)
		c.piSyncMtx.Unlock()
	}()

	return nil
}

func processPreimageRequest(c *Core, dc *dexConnection, reqID uint64, oid order.OrderID, commitChecksum dex.Bytes) error {
	tracker, preImg, isCancel := dc.findOrder(oid)
	if tracker == nil {
		return fmt.Errorf("no active order found for preimage request for %s", oid)
	}

	// Record the csum if this preimage request is novel, and deny it if this is
	// a duplicate request with an altered csum.
	if !acceptCsum(tracker, isCancel, commitChecksum) {
		csumErr := errors.New("invalid csum in duplicate preimage request")
		resp, err := msgjson.NewResponse(reqID, nil,
			msgjson.NewError(msgjson.InvalidRequestError, csumErr.Error()))
		if err != nil {
			c.log.Errorf("Failed to encode response to denied preimage request: %v", err)
			return csumErr
		}
		err = dc.Send(resp)
		if err != nil {
			c.log.Errorf("Failed to send response to denied preimage request: %v", err)
		}
		return csumErr
	}

	// Clean up the sentCommits now that we loaded the commitment. This can be
	// removed when the old piSyncers method is removed.
	defer func() {
		// Note the commitment is not tracker.Commitment() for cancel orders.
		c.sentCommitsMtx.Lock()
		delete(c.sentCommits, preImg.Commit()) // redundant if the commitment was in request
		c.sentCommitsMtx.Unlock()
	}()
	resp, err := msgjson.NewResponse(reqID, &msgjson.PreimageResponse{
		Preimage: preImg[:],
	}, nil)
	if err != nil {
		return fmt.Errorf("preimage response encoding error: %w", err)
	}
	err = dc.Send(resp)
	if err != nil {
		return fmt.Errorf("preimage send error: %w", err)
	}
	topic := TopicPreimageSent
	if isCancel {
		topic = TopicCancelPreimageSent
	}
	c.notify(newOrderNote(topic, "", "", db.Data, tracker.coreOrder()))
	return nil
}

// acceptCsum will record the commitment checksum so we can verify that the
// subsequent match_proof with this order has the same checksum. If it does not,
// the server may have used the knowledge of this preimage we are sending them
// now to alter the epoch shuffle. The return value is false if a previous
// checksum has been recorded that differs from the provided one.
func acceptCsum(tracker *trackedTrade, isCancel bool, commitChecksum dex.Bytes) bool {
	// Do not allow csum to be changed once it has been committed to
	// (initialized to something other than `nil`) because it is probably a
	// malicious behavior by the server.
	tracker.mtx.Lock()
	defer tracker.mtx.Unlock()

	if isCancel {
		if tracker.cancel.csum == nil {
			tracker.cancel.csum = commitChecksum
			return true
		}
		return bytes.Equal(commitChecksum, tracker.cancel.csum)
	}
	if tracker.csum == nil {
		tracker.csum = commitChecksum
		return true
	}

	return bytes.Equal(commitChecksum, tracker.csum)
}

// handleMatchRoute processes the DEX-originating match route request,
// indicating that a match has been made and needs to be negotiated.
func handleMatchRoute(c *Core, dc *dexConnection, msg *msgjson.Message) error {
	msgMatches := make([]*msgjson.Match, 0)
	err := msg.Unmarshal(&msgMatches)
	if err != nil {
		return fmt.Errorf("match request parsing error: %w", err)
	}

	// TODO: If the dexConnection.acct is locked, prompt the user to login.
	// Maybe even spin here before failing with no hope of retrying the match
	// request handling.

	// Acknowledgements MUST be in the same orders as the msgjson.Matches.
	matches, acks, err := dc.parseMatches(msgMatches, true)
	if err != nil {
		// Even one failed match fails them all since the server requires acks
		// for them all, and in the same order. TODO: consider lifting this
		// requirement, which requires changes to the server's handling.
		return err
	}

	// Warn about new matches for unfunded orders. We still must ack all the
	// matches in the 'match' request for the server to accept it, although the
	// server doesn't require match acks. See (*Swapper).processMatchAcks.
	for oid, srvMatch := range matches {
		if !srvMatch.tracker.hasFundingCoins() {
			c.log.Warnf("Received new match for unfunded order %v!", oid)
			// In runMatches>tracker.negotiate we generate the matchTracker and
			// set swapErr after updating order status and filled amount, and
			// storing the match to the DB. It may still be possible for the
			// user to recover if the issue is just that the wrong wallet is
			// connected by fixing wallet config and restarting. p.s. Hopefully
			// we are maker.
		}
	}

	resp, err := msgjson.NewResponse(msg.ID, acks, nil)
	if err != nil {
		return err
	}

	// Send the match acknowledgments.
	err = dc.Send(resp)
	if err != nil {
		// Do not bail on the matches on error, just log it.
		c.log.Errorf("Send match response: %v", err)
	}

	// Begin match negotiation.
	updatedAssets, err := c.runMatches(matches)
	if len(updatedAssets) > 0 {
		c.updateBalances(updatedAssets)
	}

	return err
}

// handleNoMatchRoute handles the DEX-originating nomatch request, which is sent
// when an order does not match during the epoch match cycle.
func handleNoMatchRoute(c *Core, dc *dexConnection, msg *msgjson.Message) error {
	nomatchMsg := new(msgjson.NoMatch)
	err := msg.Unmarshal(nomatchMsg)
	if err != nil {
		return fmt.Errorf("nomatch request parsing error: %w", err)
	}
	var oid order.OrderID
	copy(oid[:], nomatchMsg.OrderID)

	tracker, _, _ := dc.findOrder(oid)
	if tracker == nil {
		return newError(unknownOrderErr, "nomatch request received for unknown order %v from %s", oid, dc.acct.host)
	}
	updatedAssets, err := tracker.nomatch(oid)
	if len(updatedAssets) > 0 {
		c.updateBalances(updatedAssets)
	}
	return err
}

func (c *Core) schedTradeTick(tracker *trackedTrade) {
	oid := tracker.ID()
	c.tickSchedMtx.Lock()
	defer c.tickSchedMtx.Unlock()
	if _, found := c.tickSched[oid]; found {
		return // already going to tick this trade
	}

	tick := func() {
		assets, err := c.tick(tracker)
		if len(assets) > 0 {
			c.updateBalances(assets)
		}
		if err != nil {
			c.log.Errorf("tick error for order %v: %v", oid, err)
		}
	}

	numMatches := len(tracker.activeMatches())
	if numMatches == 1 {
		go tick()
		return
	}

	// Schedule a tick for this trade.
	delay := 2*time.Second + time.Duration(numMatches)*time.Second/10 // 1 sec extra delay for every 10 active matches
	if delay > 5*time.Second {
		delay = 5 * time.Second
	}
	c.log.Debugf("Waiting %v to tick trade %v with %d active matches", delay, oid, numMatches)
	c.tickSched[oid] = time.AfterFunc(delay, func() {
		c.tickSchedMtx.Lock()
		defer c.tickSchedMtx.Unlock()
		defer delete(c.tickSched, oid)
		tick()
	})
}

// handleAuditRoute handles the DEX-originating audit request, which is sent
// when a match counter-party reports their initiation transaction.
func handleAuditRoute(c *Core, dc *dexConnection, msg *msgjson.Message) error {
	audit := new(msgjson.Audit)
	err := msg.Unmarshal(audit)
	if err != nil {
		return fmt.Errorf("audit request parsing error: %w", err)
	}
	var oid order.OrderID
	copy(oid[:], audit.OrderID)

	tracker, _, _ := dc.findOrder(oid)
	if tracker == nil {
		return fmt.Errorf("audit request received for unknown order: %s", string(msg.Payload))
	}
	return tracker.processAuditMsg(msg.ID, audit)
}

// handleRedemptionRoute handles the DEX-originating redemption request, which
// is sent when a match counter-party reports their redemption transaction.
func handleRedemptionRoute(c *Core, dc *dexConnection, msg *msgjson.Message) error {
	redemption := new(msgjson.Redemption)
	err := msg.Unmarshal(redemption)
	if err != nil {
		return fmt.Errorf("redemption request parsing error: %w", err)
	}
	var oid order.OrderID
	copy(oid[:], redemption.OrderID)

	tracker, _, _ := dc.findOrder(oid)
	if tracker == nil {
		return fmt.Errorf("redemption request received for unknown order: %s", string(msg.Payload))
	}
	err = tracker.processRedemption(msg.ID, redemption)
	if err != nil {
		return err
	}
	c.schedTradeTick(tracker)
	return nil
}

// existsWaiter returns true if the waiter already exists in the map.
func (c *Core) existsWaiter(id string) bool {
	c.waiterMtx.RLock()
	_, exists := c.blockWaiters[id]
	c.waiterMtx.RUnlock()
	return exists
}

// removeWaiter removes a blockWaiter from the map.
func (c *Core) removeWaiter(id string) {
	c.waiterMtx.Lock()
	delete(c.blockWaiters, id)
	c.waiterMtx.Unlock()
}

// peerChange is called by a wallet backend when the peer count changes or
// cannot be determined. A wallet state note is always emitted. In addition to
// recording the number of peers, if the number of peers is 0, the wallet is
// flagged as not synced. If the number of peers has just dropped to zero, a
// notification that includes wallet state is emitted with the topic
// TopicWalletPeersWarning. If the number of peers is >0 and was previously
// zero, a resync monitor goroutine is launched to poll SyncStatus until the
// wallet has caught up with its network. The monitor goroutine will regularly
// emit wallet state notes, and once sync has been restored, a wallet balance
// note will be emitted. If err is non-nil, numPeers should be zero.
func (c *Core) peerChange(w *xcWallet, numPeers uint32, err error) {
	if err != nil {
		c.log.Warnf("%s wallet communication issue: %q", unbip(w.AssetID), err.Error())
	} else if numPeers == 0 {
		c.log.Warnf("Wallet for asset %s has zero network peers!", unbip(w.AssetID))
	} else {
		c.log.Tracef("New peer count for asset %s: %v", unbip(w.AssetID), numPeers)
	}

	w.mtx.Lock()
	wasDisconnected := w.peerCount == 0 // excludes no count (-1)
	w.peerCount = int32(numPeers)
	if numPeers == 0 {
		w.synced = false
	}
	w.mtx.Unlock()

	// When we get peers after having none, start waiting for re-sync, otherwise
	// leave synced alone. This excludes the unknown state (-1) prior to the
	// initial peer count report.
	if wasDisconnected && numPeers > 0 {
		subject, details := c.formatDetails(TopicWalletPeersRestored, w.Info().Name)
		c.notify(newWalletConfigNote(TopicWalletPeersRestored, subject, details,
			db.Success, w.state()))
		c.startWalletSyncMonitor(w)
	}

	// Send a WalletStateNote in case Synced or anything else has changed.
	if atomic.LoadUint32(w.broadcasting) == 1 {
		if (numPeers == 0 || err != nil) && !wasDisconnected { // was connected or initial report
			if err != nil {
				subject, details := c.formatDetails(TopicWalletCommsWarning,
					w.Info().Name, err.Error())
				c.notify(newWalletConfigNote(TopicWalletCommsWarning, subject, details,
					db.ErrorLevel, w.state()))
			} else {
				subject, details := c.formatDetails(TopicWalletPeersWarning, w.Info().Name)
				c.notify(newWalletConfigNote(TopicWalletPeersWarning, subject, details,
					db.WarningLevel, w.state()))
			}
		}

		c.notify(newWalletStateNote(w.state()))
	}
}

// tipChange is called by a wallet backend when the tip block changes, or when
// a connection error is encountered such that tip change reporting may be
// adversely affected.
func (c *Core) tipChange(assetID uint32, nodeErr error) {
	if nodeErr != nil {
		c.log.Errorf("%s wallet is reporting a failed state: %v", unbip(assetID), nodeErr)
		return
	}
	c.log.Tracef("processing tip change for %s", unbip(assetID))
	c.waiterMtx.Lock()
	for id, waiter := range c.blockWaiters {
		if waiter.assetID != assetID {
			continue
		}
		go func(id string, waiter *blockWaiter) {
			ok, err := waiter.trigger()
			if err != nil {
				waiter.action(err)
				c.removeWaiter(id)
			}
			if ok {
				waiter.action(nil)
				c.removeWaiter(id)
			}
		}(id, waiter)
	}
	c.waiterMtx.Unlock()

	assets := make(assetMap)
	for _, dc := range c.dexConnections() {
		newUpdates := c.tickAsset(dc, assetID)
		if len(newUpdates) > 0 {
			assets.merge(newUpdates)
		}
	}

	// Ensure we always at least update this asset's balance regardless of trade
	// status changes.
	assets.count(assetID)
	c.updateBalances(assets)
}

// convertAssetInfo converts from a *msgjson.Asset to the nearly identical
// *dex.Asset.
func convertAssetInfo(ai *msgjson.Asset) *dex.Asset {
	return &dex.Asset{
		ID:           ai.ID,
		Symbol:       ai.Symbol,
		Version:      ai.Version,
		MaxFeeRate:   ai.MaxFeeRate,
		SwapSize:     ai.SwapSize,
		SwapSizeBase: ai.SwapSizeBase,
		RedeemSize:   ai.RedeemSize,
		SwapConf:     uint32(ai.SwapConf),
		UnitInfo:     ai.UnitInfo,
	}
}

// checkSigS256 checks that the message's signature was created with the private
// key for the provided secp256k1 public key on the sha256 hash of the message.
func checkSigS256(msg, pkBytes, sigBytes []byte) error {
	pubKey, err := secp256k1.ParsePubKey(pkBytes)
	if err != nil {
		return fmt.Errorf("error decoding secp256k1 PublicKey from bytes: %w", err)
	}
	signature, err := ecdsa.ParseDERSignature(sigBytes)
	if err != nil {
		return fmt.Errorf("error decoding secp256k1 Signature from bytes: %w", err)
	}
	hash := sha256.Sum256(msg)
	if !signature.Verify(hash[:], pubKey) {
		// Might be an older buggy server. (V0PURGE)
		if !signature.Verify(msg, pubKey) {
			return fmt.Errorf("secp256k1 signature verification failed")
		}
	}
	return nil
}

// signMsg hashes and signs the message with the sha256 hash function and the
// provided private key.
func signMsg(privKey *secp256k1.PrivateKey, msg []byte) []byte {
	// NOTE: legacy servers will not accept this signature.
	hash := sha256.Sum256(msg)
	return ecdsa.Sign(privKey, hash[:]).Serialize()
}

// sign signs the msgjson.Signable with the provided private key.
func sign(privKey *secp256k1.PrivateKey, payload msgjson.Signable) {
	sigMsg := payload.Serialize()
	payload.SetSig(signMsg(privKey, sigMsg))
}

// stampAndSign time stamps the msgjson.Stampable, and signs it with the given
// private key.
func stampAndSign(privKey *secp256k1.PrivateKey, payload msgjson.Stampable) {
	payload.Stamp(uint64(time.Now().UnixMilli()))
	sign(privKey, payload)
}

// sendRequest sends a request via the specified ws connection and unmarshals
// the response into the provided interface.
// TODO: Modify to accept a context.Context argument so callers can pass core's
// context to break out of the reply wait when Core starts shutting down.
func sendRequest(conn comms.WsConn, route string, request, response interface{}, timeout time.Duration) error {
	reqMsg, err := msgjson.NewRequest(conn.NextID(), route, request)
	if err != nil {
		return fmt.Errorf("error encoding %q request: %w", route, err)
	}

	errChan := make(chan error, 1)
	err = conn.RequestWithTimeout(reqMsg, func(msg *msgjson.Message) {
		errChan <- msg.UnmarshalResult(response)
	}, timeout, func() {
		errChan <- fmt.Errorf("timed out waiting for %q response", route)
	})
	// Check the request error.
	if err != nil {
		return err
	}

	// Check the response error.
	return <-errChan
}

// newPreimage creates a random order commitment. If you require a matching
// commitment, generate a Preimage, then Preimage.Commit().
func newPreimage() (p order.Preimage) {
	copy(p[:], encode.RandomBytes(order.PreimageSize))
	return
}

// messagePrefix converts the order.Prefix to a msgjson.Prefix.
func messagePrefix(prefix *order.Prefix) *msgjson.Prefix {
	oType := uint8(msgjson.LimitOrderNum)
	switch prefix.OrderType {
	case order.MarketOrderType:
		oType = msgjson.MarketOrderNum
	case order.CancelOrderType:
		oType = msgjson.CancelOrderNum
	}
	return &msgjson.Prefix{
		AccountID:  prefix.AccountID[:],
		Base:       prefix.BaseAsset,
		Quote:      prefix.QuoteAsset,
		OrderType:  oType,
		ClientTime: uint64(prefix.ClientTime.UnixMilli()),
		Commit:     prefix.Commit[:],
	}
}

// messageTrade converts the order.Trade to a msgjson.Trade, adding the coins.
func messageTrade(trade *order.Trade, coins []*msgjson.Coin) *msgjson.Trade {
	side := uint8(msgjson.BuyOrderNum)
	if trade.Sell {
		side = msgjson.SellOrderNum
	}
	return &msgjson.Trade{
		Side:     side,
		Quantity: trade.Quantity,
		Coins:    coins,
		Address:  trade.Address,
	}
}

// messageCoin converts the []asset.Coin to a []*msgjson.Coin, signing the coin
// IDs and retrieving the pubkeys too.
func messageCoins(wallet *xcWallet, coins asset.Coins, redeemScripts []dex.Bytes) ([]*msgjson.Coin, error) {
	msgCoins := make([]*msgjson.Coin, 0, len(coins))
	for i, coin := range coins {
		coinID := coin.ID()
		pubKeys, sigs, err := wallet.SignMessage(coin, coinID)
		if err != nil {
			return nil, fmt.Errorf("%s SignMessage error: %w", unbip(wallet.AssetID), err)
		}
		msgCoins = append(msgCoins, &msgjson.Coin{
			ID:      coinID,
			PubKeys: pubKeys,
			Sigs:    sigs,
			Redeem:  redeemScripts[i],
		})
	}
	return msgCoins, nil
}

// messageOrder converts an order.Order of any underlying type to an appropriate
// msgjson type used for submitting the order.
func messageOrder(ord order.Order, coins []*msgjson.Coin) (string, msgjson.Stampable, *msgjson.Trade) {
	prefix, trade := ord.Prefix(), ord.Trade()
	switch o := ord.(type) {
	case *order.LimitOrder:
		tifFlag := uint8(msgjson.StandingOrderNum)
		if o.Force == order.ImmediateTiF {
			tifFlag = msgjson.ImmediateOrderNum
		}
		msgOrd := &msgjson.LimitOrder{
			Prefix: *messagePrefix(prefix),
			Trade:  *messageTrade(trade, coins),
			Rate:   o.Rate,
			TiF:    tifFlag,
		}
		return msgjson.LimitRoute, msgOrd, &msgOrd.Trade
	case *order.MarketOrder:
		msgOrd := &msgjson.MarketOrder{
			Prefix: *messagePrefix(prefix),
			Trade:  *messageTrade(trade, coins),
		}
		return msgjson.MarketRoute, msgOrd, &msgOrd.Trade
	case *order.CancelOrder:
		return msgjson.CancelRoute, &msgjson.CancelOrder{
			Prefix:   *messagePrefix(prefix),
			TargetID: o.TargetOrderID[:],
		}, nil
	default:
		panic("unknown order type")
	}
}

// validateOrderResponse validates the response against the order and the order
// message, and stamps the order with the ServerTime, giving it a valid OrderID.
func validateOrderResponse(dc *dexConnection, result *msgjson.OrderResult, ord order.Order, msgOrder msgjson.Stampable) error {
	if result.ServerTime == 0 {
		return fmt.Errorf("OrderResult cannot have servertime = 0")
	}
	msgOrder.Stamp(result.ServerTime)
	msg := msgOrder.Serialize()
	err := dc.acct.checkSig(msg, result.Sig)
	if err != nil {
		return fmt.Errorf("signature error. order abandoned")
	}
	ord.SetTime(time.UnixMilli(int64(result.ServerTime)))
	checkID, err := order.IDFromBytes(result.OrderID)
	if err != nil {
		return err
	}
	oid := ord.ID()
	if oid != checkID {
		return fmt.Errorf("failed ID match. order abandoned")
	}
	return nil
}

// parseCert returns the (presumed to be) TLS certificate. If the certI is a
// string, it will be treated as a filepath and the raw file contents returned.
// if certI is already a []byte, it is presumed to be the raw file contents, and
// is returned unmodified.
func parseCert(host string, certI interface{}, net dex.Network) ([]byte, error) {
	switch c := certI.(type) {
	case string:
		if len(c) == 0 {
			return CertStore[net][host], nil // not found is ok (try without TLS)
		}
		cert, err := os.ReadFile(c)
		if err != nil {
			return nil, newError(fileReadErr, "failed to read certificate file from %s: %w", c, err)
		}
		return cert, nil
	case []byte:
		if len(c) == 0 {
			return CertStore[net][host], nil // not found is ok (try without TLS)
		}
		return c, nil
	case nil:
		return CertStore[net][host], nil // not found is ok (try without TLS)
	}
	return nil, fmt.Errorf("not a valid certificate type %T", certI)
}

// walletDefinition gets the registered WalletDefinition for the asset and
// wallet type.
func walletDefinition(assetID uint32, walletType string) (*asset.WalletDefinition, error) {
	token := asset.TokenInfo(assetID)
	if token != nil {
		return token.Definition, nil
	}
	winfo, err := asset.Info(assetID)
	if err != nil {
		return nil, newError(assetSupportErr, "asset.Info error: %w", err)
	}
	if walletType == "" {
		if len(winfo.AvailableWallets) <= winfo.LegacyWalletIndex {
			return nil, fmt.Errorf("legacy wallet index out of range")
		}
		return winfo.AvailableWallets[winfo.LegacyWalletIndex], nil
	}
	var walletDef *asset.WalletDefinition
	for _, def := range winfo.AvailableWallets {
		if def.Type == walletType {
			walletDef = def
		}
	}
	if walletDef == nil {
		return nil, fmt.Errorf("could not find wallet definition for asset %s, type %q", unbip(assetID), walletType)
	}
	return walletDef, nil
}

// WalletLogFilePath returns the path to the wallet's log file.
func (c *Core) WalletLogFilePath(assetID uint32) (string, error) {
	wallet, exists := c.wallet(assetID)
	if !exists {
		return "", newError(missingWalletErr, "no configured wallet found for %s (%d)",
			strings.ToUpper(unbip(assetID)), assetID)
	}

	return wallet.logFilePath()
}

// WalletRestorationInfo returns information about how to restore the currently
// loaded wallet for assetID in various external wallet software. This function
// will return an error if the currently loaded wallet for assetID does not
// implement the WalletRestorer interface.
func (c *Core) WalletRestorationInfo(pw []byte, assetID uint32) ([]*asset.WalletRestoration, error) {
	crypter, err := c.encryptionKey(pw)
	if err != nil {
		return nil, fmt.Errorf("WalletRestorationInfo password error: %w", err)
	}
	defer crypter.Close()

	seed, _, err := c.assetSeedAndPass(assetID, crypter)
	if err != nil {
		return nil, fmt.Errorf("assetSeedAndPass error: %w", err)
	}
	defer encode.ClearBytes(seed)

	wallet, found := c.wallet(assetID)
	if !found {
		return nil, fmt.Errorf("no wallet configured for asset %d", assetID)
	}

	restorer, ok := wallet.Wallet.(asset.WalletRestorer)
	if !ok {
		return nil, fmt.Errorf("wallet for asset %d doesn't support exporting functionality", assetID)
	}

	restorationInfo, err := restorer.RestorationInfo(seed)
	if err != nil {
		return nil, fmt.Errorf("failed to get restoration info for wallet %w", err)
	}

	return restorationInfo, nil
}

func createFile(fileName string) (*os.File, error) {
	if fileName == "" {
		return nil, errors.New("no file path specified for creating")
	}
	fileName = dex.CleanAndExpandPath(fileName)
	// Errors if file exists.
	f, err := os.OpenFile(fileName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return nil, err
	}
	return f, nil
}

func (c *Core) deleteOrderFn(ordersFileStr string) (perOrderFn func(*db.MetaOrder) error, cleanUpFn func() error, err error) {
	ordersFile, err := createFile(ordersFileStr)
	if err != nil {
		return nil, nil, fmt.Errorf("problem opening orders file: %v", err)
	}
	csvWriter := csv.NewWriter(ordersFile)
	csvWriter.UseCRLF = runtime.GOOS == "windows"
	err = csvWriter.Write([]string{
		"Host",
		"Order ID",
		"Base",
		"Quote",
		"Base Quantity",
		"Order Rate",
		"Actual Rate",
		"Base Fees",
		"Quote Fees",
		"Type",
		"Side",
		"Time in Force",
		"Status",
		"TargetOrderID",
		"Filled (%)",
		"Settled (%)",
		"Time",
	})
	if err != nil {
		ordersFile.Close()
		return nil, nil, fmt.Errorf("error writing CSV: %v", err)
	}
	csvWriter.Flush()
	err = csvWriter.Error()
	if err != nil {
		ordersFile.Close()
		return nil, nil, fmt.Errorf("error writing CSV: %v", err)
	}
	return func(ord *db.MetaOrder) error {
		cord := coreOrderFromTrade(ord.Order, ord.MetaData)

		baseUnitInfo, err := asset.UnitInfo(cord.BaseID)
		if err != nil {
			return fmt.Errorf("unable to get base unit info for %v: %v", cord.BaseSymbol, err)
		}
		quoteUnitInfo, err := asset.UnitInfo(cord.QuoteID)
		if err != nil {
			return fmt.Errorf("unable to get quote unit info for %v: %v", cord.QuoteSymbol, err)
		}
		ordReader := &OrderReader{
			Order:         cord,
			BaseUnitInfo:  baseUnitInfo,
			QuoteUnitInfo: quoteUnitInfo,
		}

		timestamp := time.UnixMilli(int64(cord.Stamp)).Local().Format(time.RFC3339Nano)
		err = csvWriter.Write([]string{
			cord.Host,                     // Host
			ord.Order.ID().String(),       // Order ID
			cord.BaseSymbol,               // Base
			cord.QuoteSymbol,              // Quote
			ordReader.BaseQtyString(),     // Base Quantity
			ordReader.SimpleRateString(),  // Order Rate
			ordReader.AverageRateString(), // Actual Rate
			ordReader.BaseAssetFees(),     // Base Fees
			ordReader.QuoteAssetFees(),    // Quote Fees
			ordReader.Type.String(),       // Type
			ordReader.SideString(),        // Side
			cord.TimeInForce.String(),     // Time in Force
			ordReader.StatusString(),      // Status
			cord.TargetOrderID.String(),   // Target Order ID
			ordReader.FilledPercent(),     // Filled
			ordReader.SettledPercent(),    // Settled
			timestamp,                     // Time
		})
		if err != nil {
			return fmt.Errorf("error writing orders CSV: %v", err)
		}
		csvWriter.Flush()
		err = csvWriter.Error()
		if err != nil {
			return fmt.Errorf("error writing orders CSV: %v", err)
		}
		return nil
	}, ordersFile.Close, nil
}

func deleteMatchFn(matchesFileStr string) (perMatchFn func(*db.MetaMatch, bool) error, cleanUpFn func() error, err error) {
	matchesFile, err := createFile(matchesFileStr)
	if err != nil {
		return nil, nil, fmt.Errorf("problem opening orders file: %v", err)
	}
	csvWriter := csv.NewWriter(matchesFile)
	csvWriter.UseCRLF = runtime.GOOS == "windows"

	err = csvWriter.Write([]string{
		"Host",
		"Base",
		"Quote",
		"Match ID",
		"Order ID",
		"Quantity",
		"Rate",
		"Swap Fee Rate",
		"Swap Address",
		"Status",
		"Side",
		"Secret Hash",
		"Secret",
		"Maker Swap Coin ID",
		"Maker Redeem Coin ID",
		"Taker Swap Coin ID",
		"Taker Redeem Coin ID",
		"Refund Coin ID",
		"Time",
	})
	if err != nil {
		matchesFile.Close()
		return nil, nil, fmt.Errorf("error writing matches CSV: %v", err)
	}
	csvWriter.Flush()
	err = csvWriter.Error()
	if err != nil {
		matchesFile.Close()
		return nil, nil, fmt.Errorf("error writing matches CSV: %v", err)
	}
	return func(mtch *db.MetaMatch, isSell bool) error {
		numToStr := func(n interface{}) string {
			return fmt.Sprintf("%d", n)
		}
		base, quote := mtch.MetaData.Base, mtch.MetaData.Quote

		makerAsset, takerAsset := base, quote
		// If we are either not maker or not buying, invert it. Double
		// inverse would be no change.
		if (mtch.Side == order.Taker) != isSell {
			makerAsset, takerAsset = quote, base
		}

		var (
			makerSwapID, makerRedeemID, takerSwapID, redeemSwapID, refundCoinID string
			err                                                                 error
		)

		decode := func(assetID uint32, coin []byte) (string, error) {
			if coin == nil {
				return "", nil
			}
			return asset.DecodeCoinID(assetID, coin)
		}

		makerSwapID, err = decode(takerAsset, mtch.MetaData.Proof.MakerSwap)
		if err != nil {
			return fmt.Errorf("unable to format maker's swap: %v", err)
		}
		makerRedeemID, err = decode(makerAsset, mtch.MetaData.Proof.MakerRedeem)
		if err != nil {
			return fmt.Errorf("unable to format maker's redeem: %v", err)
		}
		takerSwapID, err = decode(makerAsset, mtch.MetaData.Proof.TakerSwap)
		if err != nil {
			return fmt.Errorf("unable to format taker's swap: %v", err)
		}
		redeemSwapID, err = decode(takerAsset, mtch.MetaData.Proof.TakerRedeem)
		if err != nil {
			return fmt.Errorf("unable to format taker's redeem: %v", err)
		}
		refundCoinID, err = decode(makerAsset, mtch.MetaData.Proof.RefundCoin)
		if err != nil {
			return fmt.Errorf("unable to format maker's refund: %v", err)
		}

		timestamp := time.UnixMilli(int64(mtch.MetaData.Stamp)).Local().Format(time.RFC3339Nano)
		err = csvWriter.Write([]string{
			mtch.MetaData.DEX,                                 // Host
			dex.BipIDSymbol(base),                             // Base
			dex.BipIDSymbol(quote),                            // Quote
			mtch.MatchID.String(),                             // Match ID
			mtch.OrderID.String(),                             // Order ID
			numToStr(mtch.Quantity),                           // Quantity
			numToStr(mtch.Rate),                               // Rate
			numToStr(mtch.FeeRateSwap),                        // Swap Fee Rate
			mtch.Address,                                      // Swap Address
			mtch.Status.String(),                              // Status
			mtch.Side.String(),                                // Side
			fmt.Sprintf("%x", mtch.MetaData.Proof.SecretHash), // Secret Hash
			fmt.Sprintf("%x", mtch.MetaData.Proof.Secret),     // Secret
			makerSwapID,                                       // Maker Swap Coin ID
			makerRedeemID,                                     // Maker Redeem Coin ID
			takerSwapID,                                       // Taker Swap Coin ID
			redeemSwapID,                                      // Taker Redeem Coin ID
			refundCoinID,                                      // Refund Coin ID
			timestamp,                                         // Time
		})
		if err != nil {
			return fmt.Errorf("error writing matches CSV: %v", err)
		}
		csvWriter.Flush()
		err = csvWriter.Error()
		if err != nil {
			return fmt.Errorf("error writing matches CSV: %v", err)
		}
		return nil
	}, matchesFile.Close, nil
}

// DeleteArchivedRecords deletes archived matches from the database. Optionally
// set a time to delete records after and file paths to save deleted records as
// comma separated values. If a nil *time.Time is provided, current time is used.
func (c *Core) DeleteArchivedRecords(olderThan *time.Time, matchesFile, ordersFile string) error {
	var (
		err       error
		perMtchFn func(*db.MetaMatch, bool) error
	)
	// If provided a file to write the orders csv to, write the header and
	// defer closing the file.
	if matchesFile != "" {
		var cleanup func() error
		perMtchFn, cleanup, err = deleteMatchFn(matchesFile)
		if err != nil {
			return fmt.Errorf("unable to set up orders csv: %v", err)
		}
		defer cleanup()
	}

	// Delete matches while saving to csv if available until the database
	// says that's all or context is canceled.
	err = c.db.DeleteInactiveMatches(c.ctx, olderThan, perMtchFn)
	if err != nil {
		return fmt.Errorf("unable to delete matches: %v", err)
	}

	var perOrdFn func(*db.MetaOrder) error
	// If provided a file to write the orders csv to, write the header and
	// defer closing the file.
	if ordersFile != "" {
		var cleanup func() error
		perOrdFn, cleanup, err = c.deleteOrderFn(ordersFile)
		if err != nil {
			return fmt.Errorf("unable to set up orders csv: %v", err)
		}
		defer cleanup()
	}

	// Delete orders while saving to csv if available until the database
	// says that's all or context is canceled.
	err = c.db.DeleteInactiveOrders(c.ctx, olderThan, perOrdFn)
	if err != nil {
		return fmt.Errorf("unable to delete orders: %v", err)
	}
	return nil
}

// AccelerateOrder will use the Child-Pays-For-Parent technique to accelerate
// the swap transactions in an order.
func (c *Core) AccelerateOrder(pw []byte, oidB dex.Bytes, newFeeRate uint64) (string, error) {
	_, err := c.encryptionKey(pw)
	if err != nil {
		return "", fmt.Errorf("AccelerateOrder password error: %w", err)
	}

	oid, err := order.IDFromBytes(oidB)
	if err != nil {
		return "", err
	}
	tracker, err := c.findActiveOrder(oid)
	if err != nil {
		return "", err
	}

	if !tracker.wallets.fromWallet.traits.IsAccelerator() {
		return "", fmt.Errorf("the %s wallet is not an accelerator",
			tracker.wallets.fromAsset.Symbol)
	}

	tracker.mtx.Lock()
	defer tracker.mtx.Unlock()

	swapCoinIDs, accelerationCoins, changeCoinID, requiredForRemainingSwaps, err := tracker.orderAccelerationParameters()
	if err != nil {
		return "", err
	}

	newChangeCoin, txID, err :=
		tracker.wallets.fromWallet.accelerateOrder(swapCoinIDs, accelerationCoins, changeCoinID, requiredForRemainingSwaps, newFeeRate)
	if err != nil {
		return "", err
	}
	if newChangeCoin != nil {
		tracker.metaData.ChangeCoin = order.CoinID(newChangeCoin.ID())
		tracker.coins[newChangeCoin.ID().String()] = newChangeCoin
	} else {
		tracker.metaData.ChangeCoin = nil
	}
	tracker.metaData.AccelerationCoins = append(tracker.metaData.AccelerationCoins, tracker.metaData.ChangeCoin)
	return txID, tracker.db.UpdateOrderMetaData(oid, tracker.metaData)
}

// AccelerationEstimate returns the amount of funds that would be needed to
// accelerate the swap transactions in an order to a desired fee rate.
func (c *Core) AccelerationEstimate(oidB dex.Bytes, newFeeRate uint64) (uint64, error) {
	oid, err := order.IDFromBytes(oidB)
	if err != nil {
		return 0, err
	}

	tracker, err := c.findActiveOrder(oid)
	if err != nil {
		return 0, err
	}
	tracker.mtx.RLock()
	defer tracker.mtx.RUnlock()

	swapCoins, accelerationCoins, changeCoin, requiredForRemainingSwaps, err := tracker.orderAccelerationParameters()
	if err != nil {
		return 0, err
	}

	accelerationFee, err := tracker.wallets.fromWallet.accelerationEstimate(swapCoins, accelerationCoins, changeCoin, requiredForRemainingSwaps, newFeeRate)
	if err != nil {
		return 0, err
	}

	return accelerationFee, nil
}

// PreAccelerateOrder returns information the user can use to decide how much
// to accelerate stuck swap transactions in an order.
func (c *Core) PreAccelerateOrder(oidB dex.Bytes) (*PreAccelerate, error) {
	oid, err := order.IDFromBytes(oidB)
	if err != nil {
		return nil, err
	}

	tracker, err := c.findActiveOrder(oid)
	if err != nil {
		return nil, err
	}

	feeSuggestion := c.feeSuggestionAny(tracker.fromAssetID)

	tracker.mtx.RLock()
	defer tracker.mtx.RUnlock()
	swapCoinIDs, accelerationCoins, changeCoinID, requiredForRemainingSwaps, err := tracker.orderAccelerationParameters()
	if err != nil {
		return nil, err
	}

	currentRate, suggestedRange, earlyAcceleration, err :=
		tracker.wallets.fromWallet.preAccelerate(swapCoinIDs, accelerationCoins, changeCoinID, requiredForRemainingSwaps, feeSuggestion)
	if err != nil {
		return nil, err
	}

	if suggestedRange == nil {
		// this should never happen
		return nil, fmt.Errorf("suggested range is nil")
	}

	return &PreAccelerate{
		SwapRate:          currentRate,
		SuggestedRate:     feeSuggestion,
		SuggestedRange:    *suggestedRange,
		EarlyAcceleration: earlyAcceleration,
	}, nil
}

// findActiveOrder will search the dex connections for an active order by order
// id. An error is returned if it cannot be found.
func (c *Core) findActiveOrder(oid order.OrderID) (*trackedTrade, error) {
	for _, dc := range c.dexConnections() {
		tracker, _, _ := dc.findOrder(oid)
		if tracker != nil {
			return tracker, nil
		}
	}
	return nil, fmt.Errorf("could not find active order with order id: %s", oid)
}

// fetchFiatExchangeRates starts the fiat rate fetcher goroutine and schedules
// refresh cycles. Use under ratesMtx lock.
func (c *Core) fetchFiatExchangeRates() {
	if c.stopFiatRateFetching != nil {
		c.log.Debug("Fiat exchange rate fetching is already enabled")
		return
	}
	ctx, cancel := context.WithCancel(c.ctx)
	c.stopFiatRateFetching = cancel

	c.log.Debug("starting fiat rate fetching")

	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		tick := time.NewTicker(fiatRateRequestInterval)
		defer tick.Stop()
		for {
			c.refreshFiatRates(ctx)

			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			}
		}
	}()
}

// refreshFiatRates refreshes the fiat rates for rate sources whose values have
// not been updated since fiatRateRequestInterval. It also checks if fiat rates
// are expired and does some clean-up.
func (c *Core) refreshFiatRates(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	supportedAssets := c.SupportedAssets()
	c.ratesMtx.RLock()
	for _, source := range c.fiatRateSources {
		wg.Add(1)
		go func(source *commonRateSource) {
			defer wg.Done()
			source.refreshRates(ctx, c.log, supportedAssets)
		}(source)
	}
	c.ratesMtx.RUnlock()
	wg.Wait()

	// Remove expired rate source if any.
	c.removeExpiredRateSources()

	fiatRatesMap := c.fiatConversions()
	if len(fiatRatesMap) != 0 {
		c.notify(newFiatRatesUpdate(fiatRatesMap))
	}
}

// FiatRateSources returns a list of fiat rate sources and their individual
// status.
func (c *Core) FiatRateSources() map[string]bool {
	c.ratesMtx.RLock()
	defer c.ratesMtx.RUnlock()
	rateSources := make(map[string]bool, len(fiatRateFetchers))
	for token := range fiatRateFetchers {
		rateSources[token] = c.fiatRateSources[token] != nil
	}
	return rateSources
}

// fiatConversions returns fiat rate for all supported assets that have a
// wallet.
func (c *Core) fiatConversions() map[uint32]float64 {
	supportedAssets := asset.Assets()

	c.ratesMtx.RLock()
	defer c.ratesMtx.RUnlock()
	fiatRatesMap := make(map[uint32]float64, len(supportedAssets))
	for assetID := range supportedAssets {
		var rateSum float64
		var sources int
		for _, source := range c.fiatRateSources {
			rateInfo := source.assetRate(assetID)
			if rateInfo != nil && time.Since(rateInfo.lastUpdate) < fiatRateDataExpiry {
				sources++
				rateSum += rateInfo.rate
			}
		}
		if rateSum != 0 {
			fiatRatesMap[assetID] = rateSum / float64(sources) // get average rate.
		}
	}
	return fiatRatesMap
}

// ToggleRateSourceStatus toggles a fiat rate source status. If disable is true,
// the fiat rate source is disabled, otherwise the rate source is enabled.
func (c *Core) ToggleRateSourceStatus(source string, disable bool) error {
	if disable {
		return c.disableRateSource(source)
	}
	return c.enableRateSource(source)
}

// enableRateSource enables a fiat rate source.
func (c *Core) enableRateSource(source string) error {
	// Check if it's an invalid rate source or it is already enabled.
	rateFetcher, found := fiatRateFetchers[source]
	if !found {
		return errors.New("cannot enable unknown fiat rate source")
	}

	c.ratesMtx.Lock()
	defer c.ratesMtx.Unlock()
	if c.fiatRateSources[source] != nil {
		return nil // already enabled.
	}

	// Build fiat rate source.
	rateSource := newCommonRateSource(rateFetcher)
	c.fiatRateSources[source] = rateSource

	// If this is our first fiat rate source, start fiat rate fetcher goroutine,
	// else fetch rates.
	if len(c.fiatRateSources) == 1 {
		c.fetchFiatExchangeRates()
	} else {
		go func() {
			supportedAssets := c.SupportedAssets() // not with ratesMtx locked!
			ctx, cancel := context.WithTimeout(c.ctx, 4*time.Second)
			defer cancel()
			rateSource.refreshRates(ctx, c.log, supportedAssets)
		}()
	}

	// Update disabled fiat rate source.
	c.saveDisabledRateSources()

	c.log.Infof("Enabled %s to fetch fiat rates.", source)
	return nil
}

// disableRateSource disables a fiat rate source.
func (c *Core) disableRateSource(source string) error {
	// Check if it's an invalid fiat rate source or it is already
	// disabled.
	_, found := fiatRateFetchers[source]
	if !found {
		return errors.New("cannot disable unknown fiat rate source")
	}

	c.ratesMtx.Lock()
	defer c.ratesMtx.Unlock()

	if c.fiatRateSources[source] == nil {
		return nil // already disabled.
	}

	// Remove fiat rate source.
	delete(c.fiatRateSources, source)

	// Save disabled fiat rate sources to database.
	c.saveDisabledRateSources()

	c.log.Infof("Disabled %s from fetching fiat rates.", source)
	return nil
}

// removeExpiredRateSources disables expired fiat rate source.
func (c *Core) removeExpiredRateSources() {
	c.ratesMtx.Lock()
	defer c.ratesMtx.Unlock()

	// Remove fiat rate source with expired exchange rate data.
	var disabledSources []string
	for token, source := range c.fiatRateSources {
		if source.isExpired(fiatRateDataExpiry) {
			delete(c.fiatRateSources, token)
			disabledSources = append(disabledSources, token)
		}
	}

	// Ensure disabled fiat rate fetchers are saved to database.
	if len(disabledSources) > 0 {
		c.saveDisabledRateSources()
		c.log.Warnf("Expired rate source(s) has been disabled: %v", strings.Join(disabledSources, ", "))
	}
}

// saveDisabledRateSources saves disabled fiat rate sources to database and
// shuts down rate fetching if there are no exchange rate source. Use under
// ratesMtx lock.
func (c *Core) saveDisabledRateSources() {
	var disabled []string
	for token := range fiatRateFetchers {
		if c.fiatRateSources[token] == nil {
			disabled = append(disabled, token)
		}
	}

	// Shutdown rate fetching if there are no exchange rate source.
	if len(c.fiatRateSources) == 0 && c.stopFiatRateFetching != nil {
		c.stopFiatRateFetching()
		c.stopFiatRateFetching = nil
		c.log.Debug("shutting down rate fetching")
	}

	err := c.db.SaveDisabledRateSources(disabled)
	if err != nil {
		c.log.Errorf("Unable to save disabled fiat rate source to database: %v", err)
	}
}
