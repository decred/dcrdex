// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mm

import (
	"fmt"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/client/db"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/dex/order"
)

// wrappedCore implements the clientCore interface. A separate
// instance should be created for each bot, and the core functions will behave
// as if the entire balance of the wallet is the amount that has been reserved
// for the bot.
type wrappedCore struct {
	clientCore

	mm    *MarketMaker
	botID string
	log   dex.Logger
}

var _ clientCore = (*wrappedCore)(nil)

func (c *wrappedCore) maxBuyQty(host string, base, quote uint32, rate uint64, options map[string]string) (uint64, error) {
	baseBalance := c.mm.botBalance(c.botID, base)
	quoteBalance := c.mm.botBalance(c.botID, quote)

	mkt, err := c.ExchangeMarket(host, base, quote)
	if err != nil {
		return 0, err
	}

	fundingFees, err := c.MaxFundingFees(quote, host, 1, options)
	if err != nil {
		return 0, err
	}

	swapFees, redeemFees, refundFees, err := c.SingleLotFees(&core.SingleLotFeesForm{
		Host:          host,
		Base:          base,
		Quote:         quote,
		UseMaxFeeRate: true,
	})
	if err != nil {
		return 0, err
	}

	if quoteBalance > fundingFees {
		quoteBalance -= fundingFees
	} else {
		quoteBalance = 0
	}

	// Account based coins require the refund fees to be reserved as well.
	if !c.mm.isAccountLocker(quote) {
		refundFees = 0
	}

	lotSizeQuote := calc.BaseToQuote(rate, mkt.LotSize)
	maxLots := quoteBalance / (lotSizeQuote + swapFees + refundFees)

	if redeemFees > 0 && c.mm.isAccountLocker(base) {
		maxBaseLots := baseBalance / redeemFees
		if maxLots > maxBaseLots {
			maxLots = maxBaseLots
		}
	}

	return maxLots * mkt.LotSize, nil
}

func (c *wrappedCore) maxSellQty(host string, base, quote, numTrades uint32, options map[string]string) (uint64, error) {
	baseBalance := c.mm.botBalance(c.botID, base)
	quoteBalance := c.mm.botBalance(c.botID, quote)

	mkt, err := c.ExchangeMarket(host, base, quote)
	if err != nil {
		return 0, err
	}

	fundingFees, err := c.MaxFundingFees(base, host, numTrades, options)
	if err != nil {
		return 0, err
	}

	swapFees, redeemFees, refundFees, err := c.SingleLotFees(&core.SingleLotFeesForm{
		Host:          host,
		Base:          base,
		Quote:         quote,
		Sell:          true,
		UseMaxFeeRate: true,
	})
	if err != nil {
		return 0, err
	}

	if baseBalance > fundingFees {
		baseBalance -= fundingFees
	} else {
		baseBalance = 0
	}

	// Account based coins require the refund fees to be reserved as well.
	if !c.mm.isAccountLocker(base) {
		refundFees = 0
	}

	maxLots := baseBalance / (mkt.LotSize + swapFees + refundFees)
	if c.mm.isAccountLocker(quote) && redeemFees > 0 {
		maxQuoteLots := quoteBalance / redeemFees
		if maxLots > maxQuoteLots {
			maxLots = maxQuoteLots
		}
	}

	return maxLots * mkt.LotSize, nil
}

func (c *wrappedCore) sufficientBalanceForTrade(host string, base, quote uint32, sell bool, rate, qty uint64, options map[string]string) (bool, error) {
	var maxQty uint64
	if sell {
		var err error
		maxQty, err = c.maxSellQty(host, base, quote, 1, options)
		if err != nil {
			return false, err
		}
	} else {
		var err error
		maxQty, err = c.maxBuyQty(host, base, quote, rate, options)
		if err != nil {
			return false, err
		}
	}

	return maxQty >= qty, nil
}

func (c *wrappedCore) sufficientBalanceForMultiSell(host string, base, quote uint32, placements []*core.QtyRate, options map[string]string) (bool, error) {
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

func (c *wrappedCore) sufficientBalanceForMultiBuy(host string, base, quote uint32, placements []*core.QtyRate, options map[string]string) (bool, error) {
	baseBalance := c.mm.botBalance(c.botID, base)
	quoteBalance := c.mm.botBalance(c.botID, quote)

	mkt, err := c.ExchangeMarket(host, base, quote)
	if err != nil {
		return false, err
	}

	swapFees, redeemFees, refundFees, err := c.SingleLotFees(&core.SingleLotFeesForm{
		Host:          host,
		Base:          base,
		Quote:         quote,
		UseMaxFeeRate: true,
	})
	if err != nil {
		return false, err
	}

	if !c.mm.isAccountLocker(quote) {
		refundFees = 0
	}

	fundingFees, err := c.MaxFundingFees(quote, host, uint32(len(placements)), options)
	if err != nil {
		return false, err
	}
	if quoteBalance < fundingFees {
		return false, nil
	}

	var totalLots uint64
	remainingBalance := quoteBalance - fundingFees
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

	if c.mm.isAccountLocker(base) && baseBalance < redeemFees*totalLots {
		return false, nil
	}

	return true, nil
}

func (c *wrappedCore) sufficientBalanceForMultiTrade(host string, base, quote uint32, sell bool, placements []*core.QtyRate, options map[string]string) (bool, error) {
	if sell {
		return c.sufficientBalanceForMultiSell(host, base, quote, placements, options)
	}
	return c.sufficientBalanceForMultiBuy(host, base, quote, placements, options)
}

// Trade checks that the bot has enough balance for the trade, and if not,
// immediately returns an error. Otherwise, it forwards the call to the
// underlying core. Then, the bot's balance in the balance handler is
// updated to reflect the trade, and the balanceHandler will start tracking
// updates to the order to ensure that the bot's balance is updated.
func (c *wrappedCore) Trade(pw []byte, form *core.TradeForm) (*core.Order, error) {
	if !form.IsLimit {
		return nil, fmt.Errorf("only limit orders are supported")
	}

	enough, err := c.sufficientBalanceForTrade(form.Host, form.Base, form.Quote, form.Sell, form.Rate, form.Qty, form.Options)
	if err != nil {
		return nil, err
	}
	if !enough {
		return nil, fmt.Errorf("insufficient balance")
	}

	singleLotSwapFees, singleLotRedeemFees, singleLotRefundFees, err := c.SingleLotFees(&core.SingleLotFeesForm{
		Host:          form.Host,
		Base:          form.Base,
		Quote:         form.Quote,
		Sell:          form.Sell,
		UseMaxFeeRate: true,
	})
	if err != nil {
		return nil, err
	}

	mkt, err := c.ExchangeMarket(form.Host, form.Base, form.Quote)
	if err != nil {
		return nil, err
	}

	o, err := c.clientCore.Trade(pw, form)
	if err != nil {
		return nil, err
	}

	var orderID order.OrderID
	copy(orderID[:], o.ID)

	c.mm.ordersMtx.Lock()
	c.mm.orders[orderID] = &orderInfo{
		bot:                     c.botID,
		order:                   o,
		initialFundsLocked:      o.LockedAmt,
		initialRedeemFeesLocked: o.RedeemLockedAmt,
		initialRefundFeesLocked: o.RefundLockedAmt,
		singleLotSwapFees:       singleLotSwapFees,
		singleLotRedeemFees:     singleLotRedeemFees,
		singleLotRefundFees:     singleLotRefundFees,
		lotSize:                 mkt.LotSize,
		matchesSettled:          make(map[order.MatchID]struct{}),
		matchesSeen:             make(map[order.MatchID]struct{}),
		revokedMatchesSeen:      make(map[order.MatchID]struct{}),
	}
	c.mm.ordersMtx.Unlock()

	fromAsset, toAsset := form.Quote, form.Base
	if form.Sell {
		fromAsset, toAsset = toAsset, fromAsset
	}

	var fundingFees uint64
	if o.FeesPaid != nil {
		fundingFees = o.FeesPaid.Funding
	}

	balMods := []*balanceMod{
		{balanceModDecrease, fromAsset, balTypeAvailable, o.LockedAmt + o.RefundLockedAmt + fundingFees},
		{balanceModIncrease, fromAsset, balTypeFundingOrder, o.LockedAmt + o.RefundLockedAmt},
	}
	if o.RedeemLockedAmt > 0 {
		balMods = append(balMods, &balanceMod{balanceModDecrease, toAsset, balTypeAvailable, o.RedeemLockedAmt})
		balMods = append(balMods, &balanceMod{balanceModIncrease, toAsset, balTypeFundingOrder, o.RedeemLockedAmt})
	}

	c.mm.modifyBotBalance(c.botID, balMods)

	return o, nil
}

func (c *wrappedCore) MultiTrade(pw []byte, form *core.MultiTradeForm) ([]*core.Order, error) {
	enough, err := c.sufficientBalanceForMultiTrade(form.Host, form.Base, form.Quote, form.Sell, form.Placements, form.Options)
	if err != nil {
		return nil, err
	}
	if !enough {
		return nil, fmt.Errorf("insufficient balance")
	}

	singleLotSwapFees, singleLotRedeemFees, singleLotRefundFees, err := c.SingleLotFees(&core.SingleLotFeesForm{
		Host:          form.Host,
		Base:          form.Base,
		Quote:         form.Quote,
		Sell:          form.Sell,
		UseMaxFeeRate: true,
	})
	if err != nil {
		return nil, err
	}

	mkt, err := c.ExchangeMarket(form.Host, form.Base, form.Quote)
	if err != nil {
		return nil, err
	}

	fromAsset := form.Quote
	toAsset := form.Base
	if form.Sell {
		fromAsset = form.Base
		toAsset = form.Quote
	}
	form.MaxLock = c.mm.botBalance(c.botID, fromAsset)

	orders, err := c.clientCore.MultiTrade(pw, form)
	if err != nil {
		return nil, err
	}

	var totalFromLocked, totalToLocked, fundingFeesPaid uint64
	for _, o := range orders {
		var orderID order.OrderID
		copy(orderID[:], o.ID)

		c.mm.ordersMtx.Lock()
		c.mm.orders[orderID] = &orderInfo{
			bot:                     c.botID,
			order:                   o,
			initialFundsLocked:      o.LockedAmt,
			initialRedeemFeesLocked: o.RedeemLockedAmt,
			initialRefundFeesLocked: o.RefundLockedAmt,
			singleLotSwapFees:       singleLotSwapFees,
			singleLotRedeemFees:     singleLotRedeemFees,
			singleLotRefundFees:     singleLotRefundFees,
			lotSize:                 mkt.LotSize,
			matchesSettled:          make(map[order.MatchID]struct{}),
			matchesSeen:             make(map[order.MatchID]struct{}),
			revokedMatchesSeen:      make(map[order.MatchID]struct{}),
		}
		c.mm.ordersMtx.Unlock()

		totalFromLocked += o.LockedAmt
		totalFromLocked += o.RefundLockedAmt
		totalToLocked += o.RedeemLockedAmt
		if o.FeesPaid != nil {
			fundingFeesPaid += o.FeesPaid.Funding
		}
	}

	balMods := []*balanceMod{
		{balanceModDecrease, fromAsset, balTypeAvailable, totalFromLocked + fundingFeesPaid},
		{balanceModIncrease, fromAsset, balTypeFundingOrder, totalFromLocked},
	}
	if totalToLocked > 0 {
		balMods = append(balMods, &balanceMod{balanceModDecrease, toAsset, balTypeAvailable, totalToLocked})
		balMods = append(balMods, &balanceMod{balanceModIncrease, toAsset, balTypeFundingOrder, totalToLocked})
	}
	c.mm.modifyBotBalance(c.botID, balMods)

	return orders, nil
}

// MayBuy returns the maximum quantity of the base asset that the bot can
// buy for rate using its balance of the quote asset.
func (c *wrappedCore) MaxBuy(host string, base, quote uint32, rate uint64) (*core.MaxOrderEstimate, error) {
	maxQty, err := c.maxBuyQty(host, base, quote, rate, nil)
	if err != nil {
		return nil, err
	}
	if maxQty == 0 {
		return nil, fmt.Errorf("insufficient balance")
	}

	orderEstimate, err := c.clientCore.PreOrder(&core.TradeForm{
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
func (c *wrappedCore) MaxSell(host string, base, quote uint32) (*core.MaxOrderEstimate, error) {
	qty, err := c.maxSellQty(host, base, quote, 1, nil)
	if err != nil {
		return nil, err
	}
	if qty == 0 {
		return nil, fmt.Errorf("insufficient balance")
	}

	orderEstimate, err := c.clientCore.PreOrder(&core.TradeForm{
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

// AssetBalance returns the bot's balance for a specific asset.
func (c *wrappedCore) AssetBalance(assetID uint32) (*core.WalletBalance, error) {
	bal := c.mm.botBalance(c.botID, assetID)

	return &core.WalletBalance{
		Balance: &db.Balance{
			Balance: asset.Balance{
				Available: bal,
			},
		},
	}, nil
}

// PreOrder checks if the bot's balance is sufficient for the trade, and if it
// is, forwards the request to the underlying core.
func (c *wrappedCore) PreOrder(form *core.TradeForm) (*core.OrderEstimate, error) {
	enough, err := c.sufficientBalanceForTrade(form.Host, form.Base, form.Quote, form.Sell, form.Rate, form.Qty, form.Options)
	if err != nil {
		return nil, err
	}

	if !enough {
		return nil, fmt.Errorf("insufficient balance")
	}

	return c.clientCore.PreOrder(form)
}

// wrappedCoreForBot returns a wrappedCore for the specified bot.
func (m *MarketMaker) wrappedCoreForBot(botID string) *wrappedCore {
	return &wrappedCore{
		clientCore: m.core,
		botID:      botID,
		log:        m.log,
		mm:         m,
	}
}
