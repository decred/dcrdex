// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mm

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/client/mm/libxc"
	"decred.org/dcrdex/client/orderbook"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/dex/order"
)

// botBalance keeps track of the amount of funds available for a
// bot's use, locked to fund orders, and pending.
type botBalance struct {
	Available uint64 `json:"available"`
	Locked    uint64 `json:"locked"`
	Pending   uint64 `json:"pending"`
}

// botCoreAdaptor is an interface used by bots to access functionality
// implemented by client.Core. There are slight differences with the methods
// of client.Core. One example is AssetBalance is renamed to DEXBalance and
// returns a a botBalance instead of a *core.WalletBalance.
type botCoreAdaptor interface {
	NotificationFeed() *core.NoteFeed
	SyncBook(host string, base, quote uint32) (*orderbook.OrderBook, core.BookFeed, error)
	SingleLotFees(form *core.SingleLotFeesForm) (uint64, uint64, uint64, error)
	Cancel(oidB dex.Bytes) error
	MaxBuy(host string, base, quote uint32, rate uint64) (*core.MaxOrderEstimate, error)
	MaxSell(host string, base, quote uint32) (*core.MaxOrderEstimate, error)
	DEXBalance(assetID uint32) (*botBalance, error)
	MultiTrade(pw []byte, form *core.MultiTradeForm) ([]*core.Order, error)
	MaxFundingFees(fromAsset uint32, host string, numTrades uint32, fromSettings map[string]string) (uint64, error)
	FiatConversionRates() map[uint32]float64
	ExchangeMarket(host string, base, quote uint32) (*core.Market, error)
}

// botCexAdaptor is an interface used by bots to access functionality
// related to a CEX. Some of the functions are implemented by libxc.CEX, but
// have some differences, since this interface is meant to be used by only
// one caller. For example, Trade does not take a subscriptionID, and
// SubscribeTradeUpdates does not return one. Deposit is not available
// on libxc.CEX as it involves sending funds from the DEX wallet, but it is
// exposed here.
type botCexAdaptor interface {
	CEXBalance(assetID uint32) (*botBalance, error)
	CancelTrade(ctx context.Context, baseID, quoteID uint32, tradeID string) error
	SubscribeMarket(ctx context.Context, baseID, quoteID uint32) error
	SubscribeTradeUpdates() (updates <-chan *libxc.Trade, unsubscribe func())
	Trade(ctx context.Context, baseID, quoteID uint32, sell bool, rate, qty uint64) (*libxc.Trade, error)
	VWAP(baseID, quoteID uint32, sell bool, qty uint64) (vwap, extrema uint64, filled bool, err error)
	Deposit(ctx context.Context, assetID uint32, amount uint64, onConfirm func()) error
	Withdraw(ctx context.Context, assetID uint32, amount uint64, onConfirm func()) error
}

// pendingWithdrawal represents a withdrawal from a CEX that has been
// initiated, but the DEX has not yet received.
type pendingWithdrawal struct {
	assetID uint32
	// amtWithdrawn is the amount the CEX balance is decreased by.
	// It will not be the same as the amount received in the dex wallet.
	amtWithdrawn uint64
}

// pendingDeposit represents a deposit to a CEX that has not yet been
// confirmed.
type pendingDeposit struct {
	assetID uint32
	amtSent uint64

	mtx         sync.RWMutex
	fee         uint64
	amtCredited uint64
	// feeConfirmed is relevant assets with dynamic fees where the fee is
	// not known until the tx is mined.
	feeConfirmed bool
	cexConfirmed bool
	// cexConfirmed == true && success == false means the CEX did not credit the
	// deposit for some reason.
	success bool
}

// pendingDEXOrder keeps track of the balance effects of a pending DEX order.
// The actual order is not stored here, only its effects on the balance.
type pendingDEXOrder struct {
	balancesMtx   sync.RWMutex
	availableDiff map[uint32]int64
	locked        map[uint32]uint64
	pending       map[uint32]uint64

	// swaps, redeems, and refunds are caches of transactions. This avoids
	// having to query the wallet for transactions that are already confirmed.
	txsMtx  sync.RWMutex
	swaps   map[string]*asset.WalletTransaction
	redeems map[string]*asset.WalletTransaction
	refunds map[string]*asset.WalletTransaction
}

// unifiedExchangeAdaptor implements both botCoreAdaptor and botCexAdaptor.
type unifiedExchangeAdaptor struct {
	clientCore
	libxc.CEX

	botID string
	log   dex.Logger

	subscriptionIDMtx sync.RWMutex
	subscriptionID    *int

	withdrawalNoncePrefix string
	withdrawalNonce       atomic.Uint64

	balancesMtx sync.RWMutex
	// baseDEXBalance/baseCEXBalance are the balances the bots have before
	// taking into account any pending actions. These are updated whenever
	// a pending action is completed.
	baseDexBalances    map[uint32]uint64
	baseCexBalances    map[uint32]uint64
	pendingDEXOrders   map[order.OrderID]*pendingDEXOrder
	pendingCEXOrders   map[string]*libxc.Trade
	pendingWithdrawals map[string]*pendingWithdrawal
	pendingDeposits    map[string]*pendingDeposit
}

var _ botCoreAdaptor = (*unifiedExchangeAdaptor)(nil)
var _ botCexAdaptor = (*unifiedExchangeAdaptor)(nil)

func (u *unifiedExchangeAdaptor) logBalanceAdjustments(dexDiffs, cexDiffs map[uint32]int64, reason string) {
	var msg strings.Builder
	msg.WriteString("\n" + reason)
	msg.WriteString("\n  Balance adjustments:")
	if len(dexDiffs) > 0 {
		msg.WriteString("\n    DEX: ")
		i := 0
		for assetID, diff := range dexDiffs {
			msg.WriteString(fmt.Sprintf("%s: %d", dex.BipIDSymbol(assetID), diff))
			if i < len(dexDiffs)-1 {
				msg.WriteString(", ")
			}
			i++
		}
	}

	if len(cexDiffs) > 0 {
		msg.WriteString("\n    CEX: ")
		i := 0
		for assetID, diff := range cexDiffs {
			msg.WriteString(fmt.Sprintf("%s: %d", dex.BipIDSymbol(assetID), diff))
			if i < len(cexDiffs)-1 {
				msg.WriteString(", ")
			}
			i++
		}
	}

	msg.WriteString("\n\n  Updated base balances:\n    DEX: ")
	i := 0
	for assetID, bal := range u.baseDexBalances {
		msg.WriteString(fmt.Sprintf("%s: %d", dex.BipIDSymbol(assetID), bal))
		if i < len(u.baseDexBalances)-1 {
			msg.WriteString(", ")
		}
		i++
	}

	if len(u.baseCexBalances) > 0 {
		msg.WriteString("\n    CEX: ")
		i = 0
		for assetID, bal := range u.baseCexBalances {
			msg.WriteString(fmt.Sprintf("%s: %d", dex.BipIDSymbol(assetID), bal))
			if i < len(u.baseCexBalances)-1 {
				msg.WriteString(", ")
			}
			i++
		}
	}

	u.log.Infof(msg.String())
}

func (u *unifiedExchangeAdaptor) maxBuyQty(host string, baseID, quoteID uint32, rate uint64, options map[string]string) (uint64, error) {
	baseBalance, err := u.DEXBalance(baseID)
	if err != nil {
		return 0, err
	}
	quoteBalance, err := u.DEXBalance(quoteID)
	if err != nil {
		return 0, err
	}
	availBaseBal, availQuoteBal := baseBalance.Available, quoteBalance.Available

	mkt, err := u.clientCore.ExchangeMarket(host, baseID, quoteID)
	if err != nil {
		return 0, err
	}

	fundingFees, err := u.clientCore.MaxFundingFees(quoteID, host, 1, options)
	if err != nil {
		return 0, err
	}

	swapFees, redeemFees, refundFees, err := u.clientCore.SingleLotFees(&core.SingleLotFeesForm{
		Host:          host,
		Base:          baseID,
		Quote:         quoteID,
		UseMaxFeeRate: true,
	})
	if err != nil {
		return 0, err
	}

	if availQuoteBal > fundingFees {
		availQuoteBal -= fundingFees
	} else {
		availQuoteBal = 0
	}

	// Account based coins require the refund fees to be reserved as well.
	if !u.isAccountLocker(quoteID) {
		refundFees = 0
	}

	lotSizeQuote := calc.BaseToQuote(rate, mkt.LotSize)
	maxLots := availQuoteBal / (lotSizeQuote + swapFees + refundFees)

	if redeemFees > 0 && u.isAccountLocker(baseID) {
		maxBaseLots := availBaseBal / redeemFees
		if maxLots > maxBaseLots {
			maxLots = maxBaseLots
		}
	}

	return maxLots * mkt.LotSize, nil
}

func (u *unifiedExchangeAdaptor) maxSellQty(host string, baseID, quoteID, numTrades uint32, options map[string]string) (uint64, error) {
	baseBalance, err := u.DEXBalance(baseID)
	if err != nil {
		return 0, err
	}
	quoteBalance, err := u.DEXBalance(quoteID)
	if err != nil {
		return 0, err
	}
	availBaseBal, availQuoteBal := baseBalance.Available, quoteBalance.Available

	mkt, err := u.ExchangeMarket(host, baseID, quoteID)
	if err != nil {
		return 0, err
	}

	fundingFees, err := u.MaxFundingFees(baseID, host, numTrades, options)
	if err != nil {
		return 0, err
	}

	swapFees, redeemFees, refundFees, err := u.SingleLotFees(&core.SingleLotFeesForm{
		Host:          host,
		Base:          baseID,
		Quote:         quoteID,
		Sell:          true,
		UseMaxFeeRate: true,
	})
	if err != nil {
		return 0, err
	}

	if availBaseBal > fundingFees {
		availBaseBal -= fundingFees
	} else {
		availBaseBal = 0
	}

	// Account based coins require the refund fees to be reserved as well.
	if !u.isAccountLocker(baseID) {
		refundFees = 0
	}

	maxLots := availBaseBal / (mkt.LotSize + swapFees + refundFees)
	if u.isAccountLocker(quoteID) && redeemFees > 0 {
		maxQuoteLots := availQuoteBal / redeemFees
		if maxLots > maxQuoteLots {
			maxLots = maxQuoteLots
		}
	}

	return maxLots * mkt.LotSize, nil
}

func (c *unifiedExchangeAdaptor) sufficientBalanceForMultiSell(host string, base, quote uint32, placements []*core.QtyRate, options map[string]string) (bool, error) {
	var totalQty uint64
	for _, placement := range placements {
		totalQty += placement.Qty
	}
	maxQty, err := c.maxSellQty(host, base, quote, uint32(len(placements)), options)
	if err != nil {
		return false, err
	}
	return maxQty >= totalQty, nil
}

func (u *unifiedExchangeAdaptor) sufficientBalanceForMultiBuy(host string, baseID, quoteID uint32, placements []*core.QtyRate, options map[string]string) (bool, error) {
	baseBalance, err := u.DEXBalance(baseID)
	if err != nil {
		return false, err
	}
	quoteBalance, err := u.DEXBalance(quoteID)
	if err != nil {
		return false, err
	}
	availBaseBal, availQuoteBal := baseBalance.Available, quoteBalance.Available

	mkt, err := u.ExchangeMarket(host, baseID, quoteID)
	if err != nil {
		return false, err
	}

	swapFees, redeemFees, refundFees, err := u.SingleLotFees(&core.SingleLotFeesForm{
		Host:          host,
		Base:          baseID,
		Quote:         quoteID,
		UseMaxFeeRate: true,
	})
	if err != nil {
		return false, err
	}

	if !u.isAccountLocker(quoteID) {
		refundFees = 0
	}

	fundingFees, err := u.MaxFundingFees(quoteID, host, uint32(len(placements)), options)
	if err != nil {
		return false, err
	}
	if availQuoteBal < fundingFees {
		return false, nil
	}

	var totalLots uint64
	remainingBalance := availQuoteBal - fundingFees
	for _, placement := range placements {
		quoteQty := calc.BaseToQuote(placement.Rate, placement.Qty)
		numLots := placement.Qty / mkt.LotSize
		totalLots += numLots
		req := quoteQty + (numLots * (swapFees + refundFees))
		if remainingBalance < req {
			return false, nil
		}
		remainingBalance -= req
	}

	if u.isAccountLocker(baseID) && availBaseBal < redeemFees*totalLots {
		return false, nil
	}

	return true, nil
}

func (c *unifiedExchangeAdaptor) sufficientBalanceForMultiTrade(host string, base, quote uint32, sell bool, placements []*core.QtyRate, options map[string]string) (bool, error) {
	if sell {
		return c.sufficientBalanceForMultiSell(host, base, quote, placements, options)
	}
	return c.sufficientBalanceForMultiBuy(host, base, quote, placements, options)
}

func (u *unifiedExchangeAdaptor) addPendingDexOrders(orders []*core.Order) {
	u.balancesMtx.Lock()
	defer u.balancesMtx.Unlock()

	for _, o := range orders {
		var orderID order.OrderID
		copy(orderID[:], o.ID)

		fromAsset, fromFeeAsset, _, toFeeAsset := orderAssets(o)

		availableDiff := map[uint32]int64{}
		availableDiff[fromAsset] -= int64(o.LockedAmt)
		availableDiff[fromFeeAsset] -= int64(o.ParentAssetLockedAmt + o.RefundLockedAmt)
		availableDiff[toFeeAsset] -= int64(o.RedeemLockedAmt)
		if o.FeesPaid != nil {
			availableDiff[fromFeeAsset] -= int64(o.FeesPaid.Funding)
		}

		locked := map[uint32]uint64{}
		locked[fromAsset] += o.LockedAmt
		locked[fromFeeAsset] += o.ParentAssetLockedAmt + o.RefundLockedAmt
		locked[toFeeAsset] += o.RedeemLockedAmt

		u.pendingDEXOrders[orderID] = &pendingDEXOrder{
			swaps:         make(map[string]*asset.WalletTransaction),
			redeems:       make(map[string]*asset.WalletTransaction),
			refunds:       make(map[string]*asset.WalletTransaction),
			availableDiff: availableDiff,
			locked:        locked,
			pending:       map[uint32]uint64{},
		}
	}
}

// MultiTrade is used to place multiple standing limit orders on the same
// side of the same DEX market simultaneously.
func (u *unifiedExchangeAdaptor) MultiTrade(pw []byte, form *core.MultiTradeForm) ([]*core.Order, error) {
	enough, err := u.sufficientBalanceForMultiTrade(form.Host, form.Base, form.Quote, form.Sell, form.Placements, form.Options)
	if err != nil {
		return nil, err
	}
	if !enough {
		return nil, fmt.Errorf("insufficient balance")
	}

	fromAsset := form.Quote
	if form.Sell {
		fromAsset = form.Base
	}
	fromBalance, err := u.DEXBalance(fromAsset)
	if err != nil {
		return nil, err
	}
	form.MaxLock = fromBalance.Available

	orders, err := u.clientCore.MultiTrade(pw, form)
	if err != nil {
		return nil, err
	}

	u.addPendingDexOrders(orders)

	return orders, nil
}

// MayBuy returns the maximum quantity of the base asset that the bot can
// buy for rate using its balance of the quote asset.
func (u *unifiedExchangeAdaptor) MaxBuy(host string, base, quote uint32, rate uint64) (*core.MaxOrderEstimate, error) {
	maxQty, err := u.maxBuyQty(host, base, quote, rate, nil)
	if err != nil {
		return nil, err
	}
	if maxQty == 0 {
		return nil, fmt.Errorf("insufficient balance")
	}

	orderEstimate, err := u.clientCore.PreOrder(&core.TradeForm{
		Host:    host,
		IsLimit: true,
		Base:    base,
		Quote:   quote,
		Qty:     maxQty,
		Rate:    rate,
		// TODO: handle options. need new option for split if remaining balance < certain amount.
	})
	if err != nil {
		return nil, err
	}

	return &core.MaxOrderEstimate{
		Swap:   orderEstimate.Swap.Estimate,
		Redeem: orderEstimate.Redeem.Estimate,
	}, nil
}

// MaxSell returned the maximum quantity of the base asset that the bot can
// sell.
func (u *unifiedExchangeAdaptor) MaxSell(host string, base, quote uint32) (*core.MaxOrderEstimate, error) {
	qty, err := u.maxSellQty(host, base, quote, 1, nil)
	if err != nil {
		return nil, err
	}
	if qty == 0 {
		return nil, fmt.Errorf("insufficient balance")
	}

	orderEstimate, err := u.clientCore.PreOrder(&core.TradeForm{
		Host:    host,
		IsLimit: true,
		Sell:    true,
		Base:    base,
		Quote:   quote,
		Qty:     qty,
	})
	if err != nil {
		return nil, err
	}

	return &core.MaxOrderEstimate{
		Swap:   orderEstimate.Swap.Estimate,
		Redeem: orderEstimate.Redeem.Estimate,
	}, nil
}

// DEXBalance returns the bot's balance for a specific asset on the DEX.
func (u *unifiedExchangeAdaptor) DEXBalance(assetID uint32) (*botBalance, error) {
	u.balancesMtx.RLock()
	defer u.balancesMtx.RUnlock()

	bal, found := u.baseDexBalances[assetID]
	if !found {
		return &botBalance{}, nil
	}

	var totalAvailableDiff int64
	var totalLocked, totalPending uint64

	for _, pendingOrder := range u.pendingDEXOrders {
		pendingOrder.balancesMtx.RLock()
		totalAvailableDiff += pendingOrder.availableDiff[assetID]
		totalLocked += pendingOrder.locked[assetID]
		totalPending += pendingOrder.pending[assetID]
		pendingOrder.balancesMtx.RUnlock()
	}

	for _, pendingDeposit := range u.pendingDeposits {
		pendingDeposit.mtx.RLock()
		if pendingDeposit.assetID == assetID {
			totalAvailableDiff -= int64(pendingDeposit.amtSent)
		}

		token := asset.TokenInfo(pendingDeposit.assetID)
		if token != nil && token.ParentID == assetID {
			totalAvailableDiff -= int64(pendingDeposit.fee)
		} else if token == nil && pendingDeposit.assetID == assetID {
			totalAvailableDiff -= int64(pendingDeposit.fee)
		}
		pendingDeposit.mtx.RUnlock()
	}

	for _, pendingWithdrawal := range u.pendingWithdrawals {
		if pendingWithdrawal.assetID == assetID {
			totalPending += pendingWithdrawal.amtWithdrawn
		}
	}

	availableBalance := bal
	if totalAvailableDiff >= 0 {
		availableBalance += uint64(totalAvailableDiff)
	} else {
		if availableBalance < uint64(-totalAvailableDiff) {
			u.log.Errorf("bot %s totalAvailableDiff %d exceeds available balance %d", u.botID, totalAvailableDiff, availableBalance)
			availableBalance = 0
		} else {
			availableBalance -= uint64(-totalAvailableDiff)
		}
	}

	return &botBalance{
		Available: availableBalance,
		Locked:    totalLocked,
		Pending:   totalPending,
	}, nil
}

// incompleteCexTradeBalanceEffects returns the balance effects of an
// incomplete CEX traade. As soon as a CEX trade is complete, it is removed
// from the pending map, so completed trades do not need to be calculated.
func incompleteCexTradeBalanceEffects(trade *libxc.Trade) (availableDiff map[uint32]int64, locked map[uint32]uint64) {
	availableDiff = make(map[uint32]int64)
	locked = make(map[uint32]uint64)

	if trade.Sell {
		availableDiff[trade.BaseID] = -int64(trade.Qty)
		locked[trade.BaseID] = trade.Qty - trade.BaseFilled
		availableDiff[trade.QuoteID] = int64(trade.QuoteFilled)
	} else {
		availableDiff[trade.QuoteID] = -int64(calc.BaseToQuote(trade.Rate, trade.Qty))
		locked[trade.QuoteID] = calc.BaseToQuote(trade.Rate, trade.Qty) - trade.QuoteFilled
		availableDiff[trade.BaseID] = int64(trade.BaseFilled)
	}

	return
}

// CEXBalance returns the balance of the bot on the CEX.
func (u *unifiedExchangeAdaptor) CEXBalance(assetID uint32) (*botBalance, error) {
	u.balancesMtx.RLock()
	defer u.balancesMtx.RUnlock()

	var available, totalFundingOrder, totalPending uint64
	var totalAvailableDiff int64
	for _, tx := range u.pendingCEXOrders {
		if tx.BaseID == assetID || tx.QuoteID == assetID {
			if tx.Complete {
				u.log.Errorf("pending cex order %s is complete", tx.ID)
				continue
			}
			availableDiff, fundingOrder := incompleteCexTradeBalanceEffects(tx)
			totalAvailableDiff += availableDiff[assetID]
			totalFundingOrder += fundingOrder[assetID]
		}
	}

	for _, withdrawal := range u.pendingWithdrawals {
		if withdrawal.assetID == assetID {
			totalAvailableDiff -= int64(withdrawal.amtWithdrawn)
		}
	}

	// Credited deposits generally should already be part of the base balance,
	// but just in case the amount was credited before the wallet confirmed the
	// fee.
	for _, deposit := range u.pendingDeposits {
		deposit.mtx.RLock()
		if deposit.assetID == assetID {
			totalPending += deposit.amtCredited
		}
		deposit.mtx.RUnlock()
	}

	if totalAvailableDiff < 0 && u.baseCexBalances[assetID] < uint64(-totalAvailableDiff) {
		u.log.Errorf("bot %s asset %d available balance %d is less than total available diff %d", u.botID, assetID, u.baseCexBalances[assetID], totalAvailableDiff)
		available = 0
	} else {
		available = u.baseCexBalances[assetID] + uint64(totalAvailableDiff)
	}

	return &botBalance{
		Available: available,
		Locked:    totalFundingOrder,
		Pending:   totalPending,
	}, nil
}

// updatePendingDeposit applies the balance effects of the deposit to the bot's
// dex and cex base balances after both the fees of the deposit transaction and
// the amount credited by the CEX are confirmed.
func (u *unifiedExchangeAdaptor) updatePendingDeposit(txID string, deposit *pendingDeposit) {
	u.balancesMtx.Lock()
	defer u.balancesMtx.Unlock()

	_, found := u.pendingDeposits[txID]
	if !found {
		u.log.Errorf("%s not found among pending deposits", txID)
		return
	}

	deposit.mtx.RLock()
	defer deposit.mtx.RUnlock()

	if !deposit.cexConfirmed || !deposit.feeConfirmed {
		u.pendingDeposits[txID] = deposit
		return
	}

	if u.baseDexBalances[deposit.assetID] < deposit.amtSent {
		u.log.Errorf("%s balance on dex %d < deposit amt %d",
			dex.BipIDSymbol(deposit.assetID), u.baseDexBalances[deposit.assetID], deposit.amtSent)
		u.baseDexBalances[deposit.assetID] = 0
	} else {
		u.baseDexBalances[deposit.assetID] -= deposit.amtSent
	}

	var feeAsset uint32
	token := asset.TokenInfo(deposit.assetID)
	if token != nil {
		feeAsset = token.ParentID
	} else {
		feeAsset = deposit.assetID
	}
	if u.baseDexBalances[feeAsset] < deposit.fee {
		u.log.Errorf("%s balance on dex %d < deposit fee %d",
			dex.BipIDSymbol(feeAsset), u.baseDexBalances[feeAsset], deposit.fee)
		u.baseDexBalances[feeAsset] = 0
	} else {
		u.baseDexBalances[feeAsset] -= deposit.fee
	}

	if deposit.success {
		u.baseCexBalances[deposit.assetID] += deposit.amtCredited
	}

	delete(u.pendingDeposits, txID)

	dexDiffs := map[uint32]int64{}
	cexDiffs := map[uint32]int64{}
	dexDiffs[deposit.assetID] -= int64(deposit.amtSent)
	dexDiffs[feeAsset] -= int64(deposit.fee)
	msg := fmt.Sprintf("Deposit %s complete.", txID)
	if deposit.success {
		cexDiffs[deposit.assetID] += int64(deposit.amtCredited)
	} else {
		msg = fmt.Sprintf("Deposit %s complete, but not successful.", txID)
	}
	u.logBalanceAdjustments(dexDiffs, cexDiffs, msg)
}

func (u *unifiedExchangeAdaptor) confirmWalletTransaction(ctx context.Context, assetID uint32, coinID dex.Bytes, confirmedTx func(*asset.WalletTransaction)) {
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-timer.C:
			tx, err := u.clientCore.WalletTransaction(assetID, coinID)
			if errors.Is(err, asset.CoinNotFoundError) {
				u.log.Warnf("%s wallet does not know about deposit tx: %s", dex.BipIDSymbol(assetID), coinID)
			} else if err != nil {
				u.log.Errorf("Error getting wallet transaction: %v", err)
			} else if tx.Confirmed {
				confirmedTx(tx)
				return
			}

			timer.Reset(10 * time.Minute)
		case <-ctx.Done():
			return
		}
	}
}

// Deposit deposits funds to the CEX. The deposited funds will be removed from
// the bot's wallet balance and allocated to the bot's CEX balance. After both
// the fees of the deposit transaction are confirmed by the wallet and the
// CEX confirms the amount they received, the onConfirm callback is called.
func (u *unifiedExchangeAdaptor) Deposit(ctx context.Context, assetID uint32, amount uint64, onConfirm func()) error {
	balance, err := u.DEXBalance(assetID)
	if err != nil {
		return err
	}
	// TODO: estimate fee and make sure we have enough to cover it.
	if balance.Available < amount {
		return fmt.Errorf("bot has insufficient balance to deposit %d. required: %v, have: %v", assetID, amount, balance.Available)
	}

	addr, err := u.CEX.GetDepositAddress(ctx, assetID)
	if err != nil {
		return err
	}
	coin, err := u.clientCore.Send([]byte{}, assetID, amount, addr, u.isWithdrawer(assetID))
	if err != nil {
		return err
	}

	getAmtAndFee := func() (uint64, uint64, bool) {
		tx, err := u.clientCore.WalletTransaction(assetID, coin.ID())
		if err != nil {
			u.log.Errorf("Error getting wallet transaction: %v", err)
			return amount, 0, false
		}
		return tx.Amount, tx.Fees, tx.Confirmed
	}

	amt, fee, _ := getAmtAndFee()
	isDynamicSwapper := u.isDynamicSwapper(assetID)
	deposit := &pendingDeposit{
		assetID:      assetID,
		amtSent:      amt,
		fee:          fee,
		feeConfirmed: !isDynamicSwapper,
	}

	u.balancesMtx.Lock()
	u.pendingDeposits[coin.TxID()] = deposit
	u.balancesMtx.Unlock()

	if u.isDynamicSwapper(assetID) {
		confirmedTx := func(tx *asset.WalletTransaction) {
			deposit.mtx.Lock()
			deposit.amtSent = tx.Amount
			deposit.fee = tx.Fees
			deposit.feeConfirmed = true
			deposit.mtx.Unlock()

			u.updatePendingDeposit(coin.TxID(), deposit)

			deposit.mtx.RLock()
			defer deposit.mtx.RUnlock()
			if deposit.cexConfirmed {
				go onConfirm()
			}
		}

		go u.confirmWalletTransaction(ctx, assetID, coin.ID(), confirmedTx)
	}

	cexConfirmedDeposit := func(success bool, creditedAmt uint64) {
		deposit.mtx.Lock()
		deposit.cexConfirmed = true
		deposit.success = success
		deposit.amtCredited = creditedAmt

		if success && deposit.amtCredited != deposit.amtSent {
			u.log.Warnf("CEX credited less to deposit %s than was sent. sent: %d, credited: %d", coin.TxID(), deposit.amtSent, deposit.amtCredited)
		}
		if !success {
			u.log.Warnf("CEX did not credit deposit %s. Check with the CEX about the reason.", coin.TxID())
		}
		deposit.mtx.Unlock()

		u.updatePendingDeposit(coin.TxID(), deposit)

		deposit.mtx.RLock()
		defer deposit.mtx.RUnlock()
		if deposit.feeConfirmed {
			go onConfirm()
		}
	}

	u.CEX.ConfirmDeposit(ctx, coin.TxID(), cexConfirmedDeposit)

	return nil
}

// pendingWithdrawalComplete is called after a withdrawal has been confirmed.
// The amount received is applied to the base balance, and the withdrawal is
// removed from the pending map.
func (u *unifiedExchangeAdaptor) pendingWithdrawalComplete(id string, amtReceived uint64) {
	u.balancesMtx.Lock()
	defer u.balancesMtx.Unlock()

	withdrawal, found := u.pendingWithdrawals[id]
	if !found {
		u.log.Errorf("Completed withdrawal not found among pending withdrawals")
		return
	}

	if u.baseCexBalances[withdrawal.assetID] < withdrawal.amtWithdrawn {
		u.log.Errorf("%s balance on cex %d < withdrawn amt %d",
			dex.BipIDSymbol(withdrawal.assetID), u.baseCexBalances[withdrawal.assetID], withdrawal.amtWithdrawn)
		u.baseCexBalances[withdrawal.assetID] = 0
	} else {
		u.baseCexBalances[withdrawal.assetID] -= withdrawal.amtWithdrawn
	}
	u.baseDexBalances[withdrawal.assetID] += amtReceived
	delete(u.pendingWithdrawals, id)

	dexDiffs := map[uint32]int64{withdrawal.assetID: int64(amtReceived)}
	cexDiffs := map[uint32]int64{withdrawal.assetID: -int64(withdrawal.amtWithdrawn)}
	u.logBalanceAdjustments(dexDiffs, cexDiffs, fmt.Sprintf("Withdrawal %s complete.", id))
}

// Withdraw withdraws funds from the CEX. After withdrawing, the CEX is queried
// for the transaction ID. After the transaction ID is available, the wallet is
// queried for the amount received.
func (u *unifiedExchangeAdaptor) Withdraw(ctx context.Context, assetID uint32, amount uint64, onConfirm func()) error {
	symbol := dex.BipIDSymbol(assetID)

	balance, err := u.CEXBalance(assetID)
	if err != nil {
		return err
	}
	if balance.Available < amount {
		return fmt.Errorf("bot has insufficient balance to withdraw %s. required: %v, have: %v", symbol, amount, balance.Available)
	}

	addr, err := u.clientCore.NewDepositAddress(assetID)
	if err != nil {
		return err
	}

	withdrawalID := fmt.Sprintf("%s%d", u.withdrawalNoncePrefix, u.withdrawalNonce.Add(1))

	confirmWithdrawal := func(amt uint64, txID string) {
		if strings.HasPrefix(txID, "0x") {
			txID, _ = strings.CutPrefix(txID, "0x")
		}

		var err error
		id, err := hex.DecodeString(txID)
		if err != nil {
			u.log.Errorf("Could not decode tx ID %s, unable to process withdrawal.", txID)
			return
		}

		timer := time.NewTimer(0)
		defer timer.Stop()
		for {
			select {
			case <-timer.C:
				tx, err := u.clientCore.WalletTransaction(assetID, id)
				if errors.Is(err, asset.CoinNotFoundError) {
					u.log.Warnf("%s wallet does not know about withdrawal tx: %s", dex.BipIDSymbol(assetID), txID)
					continue
				}
				if err != nil {
					u.log.Errorf("Error getting wallet transaction: %v", err)
				} else if tx.Confirmed {
					u.pendingWithdrawalComplete(withdrawalID, uint64(tx.Amount))
					onConfirm()
					return
				}

				timer = time.NewTimer(time.Minute)
			case <-ctx.Done():
				return
			}
		}
	}

	err = u.CEX.Withdraw(ctx, assetID, amount, addr, confirmWithdrawal)
	if err != nil {
		return err
	}

	u.balancesMtx.Lock()
	u.pendingWithdrawals[withdrawalID] = &pendingWithdrawal{
		assetID:      assetID,
		amtWithdrawn: amount,
	}
	u.balancesMtx.Unlock()

	return nil
}

// handleCEXTradeUpdate handles a trade update from the CEX. If the trade is in
// the pending map, it will be updated. If the trade is complete, the base balances
// will be updated.
func (u *unifiedExchangeAdaptor) handleCEXTradeUpdate(trade *libxc.Trade) {
	u.balancesMtx.Lock()
	defer u.balancesMtx.Unlock()

	if _, found := u.pendingCEXOrders[trade.ID]; !found {
		return
	}

	if !trade.Complete {
		u.pendingCEXOrders[trade.ID] = trade
		return
	}

	delete(u.pendingCEXOrders, trade.ID)

	if trade.BaseFilled == 0 && trade.QuoteFilled == 0 {
		u.log.Infof("CEX trade %s completed with zero filled amount", trade.ID)
		return
	}

	diffs := make(map[uint32]int64)

	if trade.Sell {
		if trade.BaseFilled <= u.baseCexBalances[trade.BaseID] {
			u.baseCexBalances[trade.BaseID] -= trade.BaseFilled
		} else {
			u.log.Errorf("CEX trade %s filled %d more than available balance %d of %s.",
				trade.ID, trade.BaseFilled, u.baseCexBalances[trade.BaseID], dex.BipIDSymbol(trade.BaseID))
			u.baseCexBalances[trade.BaseID] = 0
		}
		u.baseCexBalances[trade.QuoteID] += trade.QuoteFilled

		diffs[trade.BaseID] = -int64(trade.BaseFilled)
		diffs[trade.QuoteID] = int64(trade.QuoteFilled)
	} else {
		if trade.QuoteFilled <= u.baseCexBalances[trade.QuoteID] {
			u.baseCexBalances[trade.QuoteID] -= trade.QuoteFilled
		} else {
			u.log.Errorf("CEX trade %s filled %d more than available balance %d of %s.",
				trade.ID, trade.QuoteFilled, u.baseCexBalances[trade.QuoteID], dex.BipIDSymbol(trade.QuoteID))
			u.baseCexBalances[trade.QuoteID] = 0
		}
		u.baseCexBalances[trade.BaseID] += trade.BaseFilled

		diffs[trade.QuoteID] = -int64(trade.QuoteFilled)
		diffs[trade.BaseID] = int64(trade.BaseFilled)
	}

	u.logBalanceAdjustments(nil, diffs, fmt.Sprintf("CEX trade %s completed.", trade.ID))
}

// SubscribeTradeUpdates subscribes to trade updates for the bot's trades on
// the CEX. This should be called before making any trades, and only once.
func (w *unifiedExchangeAdaptor) SubscribeTradeUpdates() (<-chan *libxc.Trade, func()) {
	w.subscriptionIDMtx.Lock()
	defer w.subscriptionIDMtx.Unlock()
	if w.subscriptionID != nil {
		w.log.Errorf("SubscribeTradeUpdates called more than once by bot %s", w.botID)
		return nil, nil
	}

	updates, unsubscribe, subscriptionID := w.CEX.SubscribeTradeUpdates()
	w.subscriptionID = &subscriptionID

	ctx, cancel := context.WithCancel(context.Background())
	forwardUnsubscribe := func() {
		cancel()
		unsubscribe()
	}
	forwardUpdates := make(chan *libxc.Trade, 256)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case note := <-updates:
				w.handleCEXTradeUpdate(note)
				forwardUpdates <- note
			}
		}
	}()

	return forwardUpdates, forwardUnsubscribe
}

// Trade executes a trade on the CEX. The trade will be executed using the
// bot's CEX balance.
func (u *unifiedExchangeAdaptor) Trade(ctx context.Context, baseID, quoteID uint32, sell bool, rate, qty uint64) (*libxc.Trade, error) {
	var fromAssetID uint32
	var fromAssetQty uint64
	if sell {
		fromAssetID = baseID
		fromAssetQty = qty
	} else {
		fromAssetID = quoteID
		fromAssetQty = calc.BaseToQuote(rate, qty)
	}

	fromAssetBal, err := u.CEXBalance(fromAssetID)
	if err != nil {
		return nil, err
	}
	if fromAssetBal.Available < fromAssetQty {
		return nil, fmt.Errorf("asset bal < required for trade (%d < %d)", fromAssetBal.Available, fromAssetQty)
	}

	u.subscriptionIDMtx.RLock()
	subscriptionID := u.subscriptionID
	u.subscriptionIDMtx.RUnlock()
	if subscriptionID == nil {
		return nil, fmt.Errorf("Trade called before SubscribeTradeUpdates")
	}

	trade, err := u.CEX.Trade(ctx, baseID, quoteID, sell, rate, qty, *subscriptionID)
	if err != nil {
		return nil, err
	}

	u.balancesMtx.Lock()
	defer u.balancesMtx.Unlock()

	if trade.Complete {
		if trade.Sell {
			u.baseCexBalances[trade.BaseID] -= trade.BaseFilled
			u.baseCexBalances[trade.QuoteID] += trade.QuoteFilled
		} else {
			u.baseCexBalances[trade.BaseID] += trade.BaseFilled
			u.baseCexBalances[trade.QuoteID] -= trade.QuoteFilled
		}
	} else {
		u.pendingCEXOrders[trade.ID] = trade
	}

	return trade, nil
}

// isAccountLocker returns if the asset's wallet is an asset.AccountLocker.
func (u *unifiedExchangeAdaptor) isAccountLocker(assetID uint32) bool {
	walletState := u.clientCore.WalletState(assetID)
	if walletState == nil {
		u.log.Errorf("isAccountLocker: wallet state not found for asset %d", assetID)
		return false
	}

	return walletState.Traits.IsAccountLocker()
}

// isDynamicSwapper returns if the asset's wallet is an asset.DynamicSwapper.
func (u *unifiedExchangeAdaptor) isDynamicSwapper(assetID uint32) bool {
	walletState := u.clientCore.WalletState(assetID)
	if walletState == nil {
		u.log.Errorf("isDynamicSwapper: wallet state not found for asset %d", assetID)
		return false
	}

	return walletState.Traits.IsDynamicSwapper()
}

// isWithdrawer returns if the asset's wallet is an asset.Withdrawer.
func (u *unifiedExchangeAdaptor) isWithdrawer(assetID uint32) bool {
	walletState := u.clientCore.WalletState(assetID)
	if walletState == nil {
		u.log.Errorf("isWithdrawer: wallet state not found for asset %d", assetID)
		return false
	}

	return walletState.Traits.IsWithdrawer()
}

func orderAssets(o *core.Order) (fromAsset, fromFeeAsset, toAsset, toFeeAsset uint32) {
	if o.Sell {
		fromAsset = o.BaseID
		toAsset = o.QuoteID
	} else {
		fromAsset = o.QuoteID
		toAsset = o.BaseID
	}
	if token := asset.TokenInfo(fromAsset); token != nil {
		fromFeeAsset = token.ParentID
	} else {
		fromFeeAsset = fromAsset
	}
	if token := asset.TokenInfo(toAsset); token != nil {
		toFeeAsset = token.ParentID
	} else {
		toFeeAsset = toAsset
	}
	return
}

func dexOrderComplete(o *core.Order) bool {
	if o.Status.IsActive() {
		return false
	}

	for _, match := range o.Matches {
		if match.Active {
			return false
		}
	}

	return o.AllFeesConfirmed
}

// orderTransactions returns all of the swap, redeem, and refund transactions
// involving a dex order.
func orderTransactions(o *core.Order) (swaps map[string]bool, redeems map[string]bool, refunds map[string]bool) {
	swaps = make(map[string]bool)
	redeems = make(map[string]bool)
	refunds = make(map[string]bool)

	for _, match := range o.Matches {
		if match.Swap != nil {
			swaps[match.Swap.ID.String()] = true
		}
		if match.Redeem != nil {
			redeems[match.Redeem.ID.String()] = true
		}
		if match.Refund != nil {
			refunds[match.Refund.ID.String()] = true
		}
	}

	return
}

// updatePendingDEXOrder updates a pending DEX order based its current state.
// If the order is complete, its effects are applied to the base balance,
// and it is removed from the pending list.
func (u *unifiedExchangeAdaptor) updatePendingDEXOrder(o *core.Order) {
	var orderID order.OrderID
	copy(orderID[:], o.ID)

	u.balancesMtx.RLock()
	pendingOrder, found := u.pendingDEXOrders[orderID]
	if !found {
		u.balancesMtx.RUnlock()
		return
	}
	u.balancesMtx.RUnlock()

	fromAsset, fromFeeAsset, toAsset, toFeeAsset := orderAssets(o)

	pendingOrder.txsMtx.Lock()

	// Add new txs to tx cache
	swaps, redeems, refunds := orderTransactions(o)
	for id := range swaps {
		if _, found := pendingOrder.swaps[id]; !found {
			pendingOrder.swaps[id] = &asset.WalletTransaction{}
		}
	}
	for id := range redeems {
		if _, found := pendingOrder.redeems[id]; !found {
			pendingOrder.redeems[id] = &asset.WalletTransaction{}
		}
	}
	for id := range refunds {
		if _, found := pendingOrder.refunds[id]; !found {
			pendingOrder.refunds[id] = &asset.WalletTransaction{}
		}
	}

	// Query the wallet regarding all unconfirmed transactions
	for id, oldTx := range pendingOrder.swaps {
		if oldTx.Confirmed {
			continue
		}
		idB, _ := hex.DecodeString(id)
		tx, err := u.clientCore.WalletTransaction(fromAsset, idB)
		if err != nil {
			u.log.Errorf("Error getting swap tx %s: %v", id, err)
			continue
		}
		pendingOrder.swaps[id] = tx
	}
	for id, oldTx := range pendingOrder.redeems {
		if oldTx.Confirmed {
			continue
		}
		idB, _ := hex.DecodeString(id)
		tx, err := u.clientCore.WalletTransaction(toAsset, idB)
		if err != nil {
			u.log.Errorf("Error getting redeem tx %s: %v", id, err)
			continue
		}
		pendingOrder.redeems[id] = tx
	}
	for id, oldTx := range pendingOrder.refunds {
		if oldTx.Confirmed {
			continue
		}
		idB, _ := hex.DecodeString(id)
		tx, err := u.clientCore.WalletTransaction(fromAsset, idB)
		if err != nil {
			u.log.Errorf("Error getting refund tx %s: %v", id, err)
			continue
		}
		pendingOrder.refunds[id] = tx
	}

	// Calculate balance effects based on current state of the order.
	availableDiff := make(map[uint32]int64)
	locked := make(map[uint32]uint64)
	pending := make(map[uint32]uint64)

	availableDiff[fromAsset] -= int64(o.LockedAmt)
	availableDiff[fromFeeAsset] -= int64(o.ParentAssetLockedAmt + o.RefundLockedAmt)
	availableDiff[toFeeAsset] -= int64(o.RedeemLockedAmt)
	if o.FeesPaid != nil {
		availableDiff[fromFeeAsset] -= int64(o.FeesPaid.Funding)
	}

	locked[fromAsset] += o.LockedAmt
	locked[fromFeeAsset] += o.ParentAssetLockedAmt + o.RefundLockedAmt
	locked[toFeeAsset] += o.RedeemLockedAmt

	for _, tx := range pendingOrder.swaps {
		availableDiff[fromAsset] -= int64(tx.Amount)
		availableDiff[fromFeeAsset] -= int64(tx.Fees)
	}
	for _, tx := range pendingOrder.redeems {
		isDynamicSwapper := u.isDynamicSwapper(toAsset)

		// For dynamic fee assets, the fees are paid from the active balance,
		// and are not taken out of the redeem amount.
		if isDynamicSwapper || tx.Confirmed {
			availableDiff[toFeeAsset] -= int64(tx.Fees)
		} else {
			pending[toFeeAsset] -= tx.Fees
		}

		if tx.Confirmed {
			availableDiff[toAsset] += int64(tx.Amount)
		} else {
			pending[toAsset] += tx.Amount
		}
	}
	for _, tx := range pendingOrder.refunds {
		isDynamicSwapper := u.isDynamicSwapper(fromAsset)

		// For dynamic fee assets, the fees are paid from the active balance,
		// and are not taken out of the redeem amount.
		if isDynamicSwapper || tx.Confirmed {
			availableDiff[fromFeeAsset] -= int64(tx.Fees)
		} else {
			pending[fromFeeAsset] -= tx.Fees
		}

		if tx.Confirmed {
			availableDiff[fromAsset] += int64(tx.Amount)
		} else {
			pending[fromAsset] += tx.Amount
		}
	}
	pendingOrder.txsMtx.Unlock()

	// Update the balances
	pendingOrder.balancesMtx.Lock()
	pendingOrder.availableDiff = availableDiff
	pendingOrder.locked = locked
	pendingOrder.pending = pending
	pendingOrder.balancesMtx.Unlock()

	// If complete, remove the order from the pending list, and update the
	// bot's balance.
	if dexOrderComplete(o) {
		u.balancesMtx.Lock()
		defer u.balancesMtx.Unlock()

		delete(u.pendingDEXOrders, orderID)

		adjustedBals := false
		for assetID, diff := range availableDiff {
			adjustedBals = adjustedBals || diff != 0
			if diff > 0 {
				u.baseDexBalances[assetID] += uint64(diff)
			} else {
				if u.baseDexBalances[assetID] < uint64(-diff) {
					u.log.Errorf("bot %s diff %d exceeds available balance %d. Setting balance to 0.", u.botID, diff, u.baseDexBalances[assetID])
					u.baseDexBalances[assetID] = 0
				} else {
					u.baseDexBalances[assetID] -= uint64(-diff)
				}
			}
		}

		if adjustedBals {
			u.logBalanceAdjustments(availableDiff, nil, fmt.Sprintf("DEX order %s complete.", orderID))
		}
	}
}

func (u *unifiedExchangeAdaptor) handleDEXNotification(n core.Notification) {
	switch note := n.(type) {
	case *core.OrderNote:
		u.updatePendingDEXOrder(note.Order)
	case *core.MatchNote:
		o, err := u.clientCore.Order(note.OrderID)
		if err != nil {
			u.log.Errorf("handleDEXNotification: failed to get order %s: %v", note.OrderID, err)
			return
		}
		u.updatePendingDEXOrder(o)
	}
}

func (u *unifiedExchangeAdaptor) run(ctx context.Context) {
	go func() {
		feed := u.clientCore.NotificationFeed()
		defer feed.ReturnFeed()

		for {
			select {
			case <-ctx.Done():
				return
			case n := <-feed.C:
				u.handleDEXNotification(n)
			}
		}
	}()
}

// unifiedExchangeAdaptorForBot returns a unifiedExchangeAdaptor for the specified bot.
func unifiedExchangeAdaptorForBot(botID string, baseDexBalances, baseCexBalances map[uint32]uint64, core clientCore, cex libxc.CEX, log dex.Logger) *unifiedExchangeAdaptor {
	return &unifiedExchangeAdaptor{
		clientCore:         core,
		CEX:                cex,
		botID:              botID,
		log:                log,
		baseDexBalances:    baseDexBalances,
		baseCexBalances:    baseCexBalances,
		pendingDEXOrders:   make(map[order.OrderID]*pendingDEXOrder),
		pendingCEXOrders:   make(map[string]*libxc.Trade),
		pendingDeposits:    make(map[string]*pendingDeposit),
		pendingWithdrawals: make(map[string]*pendingWithdrawal),
	}
}
