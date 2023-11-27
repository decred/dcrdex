// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mm

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/mm/libxc"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
)

type cexTrade struct {
	qty         uint64
	rate        uint64
	sell        bool
	fromAsset   uint32
	toAsset     uint32
	baseFilled  uint64
	quoteFilled uint64
}

type cex interface {
	Balance(assetID uint32) (*libxc.ExchangeBalance, error)
	CancelTrade(ctx context.Context, baseID, quoteID uint32, tradeID string) error
	SubscribeMarket(ctx context.Context, baseID, quoteID uint32) error
	SubscribeTradeUpdates() (updates <-chan *libxc.TradeUpdate, unsubscribe func())
	Trade(ctx context.Context, baseID, quoteID uint32, sell bool, rate, qty uint64) (string, error)
	VWAP(baseID, quoteID uint32, sell bool, qty uint64) (vwap, extrema uint64, filled bool, err error)
	Deposit(ctx context.Context, assetID uint32, amount uint64, onConfirm func()) error
	Withdraw(ctx context.Context, assetID uint32, amount uint64, onConfirm func()) error
}

// wrappedCEX implements the CEX interface. A separate instance should be
// created for each arbitrage bot, and it will behave as if the entire balance
// on the CEX is the amount that was allocated to the bot.
type wrappedCEX struct {
	libxc.CEX

	mm    *MarketMaker
	botID string
	log   dex.Logger

	subscriptionIDMtx sync.RWMutex
	subscriptionID    *int

	tradesMtx sync.RWMutex
	trades    map[string]*cexTrade
}

var _ cex = (*wrappedCEX)(nil)

// Balance returns the balance of the bot on the CEX.
func (w *wrappedCEX) Balance(assetID uint32) (*libxc.ExchangeBalance, error) {
	return &libxc.ExchangeBalance{
		Available: w.mm.botCEXBalance(w.botID, assetID),
	}, nil
}

// Deposit deposits funds to the CEX. The deposited funds will be removed from
// the bot's wallet balance and allocated to the bot's CEX balance.
func (w *wrappedCEX) Deposit(ctx context.Context, assetID uint32, amount uint64, onConfirm func()) error {
	balance := w.mm.botBalance(w.botID, assetID)
	if balance < amount {
		return fmt.Errorf("bot has insufficient balance to deposit %d. required: %v, have: %v", assetID, amount, balance)
	}

	addr, err := w.CEX.GetDepositAddress(ctx, assetID)
	if err != nil {
		return err
	}

	txID, _, err := w.mm.core.Send([]byte{}, assetID, amount, addr, w.mm.isWithdrawer(assetID))
	if err != nil {
		return err
	}

	// TODO: special handling for wallets that do not support withdrawing.
	w.mm.modifyBotBalance(w.botID, []*balanceMod{{balanceModDecrease, assetID, balTypeAvailable, amount}})

	go func() {
		conf := func(confirmed bool, amt uint64) {
			if confirmed {
				w.mm.modifyBotCEXBalance(w.botID, assetID, amt, balanceModIncrease)
			}
			onConfirm()
		}
		w.CEX.ConfirmDeposit(ctx, txID, conf)
	}()

	return nil
}

// Withdraw withdraws funds from the CEX. The withdrawn funds will be removed
// from the bot's CEX balance and added to the bot's wallet balance.
func (w *wrappedCEX) Withdraw(ctx context.Context, assetID uint32, amount uint64, onConfirm func()) error {
	symbol := dex.BipIDSymbol(assetID)

	balance := w.mm.botCEXBalance(w.botID, assetID)
	if balance < amount {
		return fmt.Errorf("bot has insufficient balance to withdraw %s. required: %v, have: %v", symbol, amount, balance)
	}

	addr, err := w.mm.core.NewDepositAddress(assetID)
	if err != nil {
		return err
	}

	conf := func(withdrawnAmt uint64, txID string) {
		go func() {
			checkTransaction := func() bool {
				confs, err := w.mm.core.TransactionConfirmations(assetID, txID)
				if err != nil {
					if !errors.Is(err, asset.CoinNotFoundError) {
						w.log.Errorf("error checking transaction confirmations: %v", err)
					}
					return false
				}
				if confs > 0 {
					w.mm.modifyBotBalance(w.botID, []*balanceMod{{balanceModIncrease, assetID, balTypeAvailable, withdrawnAmt}})
					onConfirm()
					return true
				}
				return false
			}

			if checkTransaction() {
				return
			}

			ticker := time.NewTicker(time.Minute * 1)
			giveUp := time.NewTimer(2 * time.Hour)
			defer ticker.Stop()
			defer giveUp.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if checkTransaction() {
						return
					}
				case <-giveUp.C:
					w.log.Errorf("timed out waiting for withdrawal confirmation")
					return
				}
			}
		}()
	}

	err = w.CEX.Withdraw(ctx, assetID, amount, addr, conf)
	if err != nil {
		return err
	}

	w.mm.modifyBotCEXBalance(w.botID, assetID, amount, balanceModDecrease)
	return nil
}

func (w *wrappedCEX) handleTradeUpdate(update *libxc.TradeUpdate) {
	w.tradesMtx.Lock()
	defer w.tradesMtx.Unlock()

	trade, found := w.trades[update.TradeID]
	if !found {
		w.log.Errorf("wrappedCEX: trade ID %s not found", update.TradeID)
		return
	}

	if trade.sell && update.QuoteFilled > trade.quoteFilled {
		quoteFilledDelta := update.QuoteFilled - trade.quoteFilled
		w.mm.modifyBotCEXBalance(w.botID, trade.toAsset, quoteFilledDelta, balanceModIncrease)
		trade.quoteFilled = update.QuoteFilled
		trade.baseFilled = update.BaseFilled
	}

	if !trade.sell && update.BaseFilled > trade.baseFilled {
		baseFilledDelta := update.BaseFilled - trade.baseFilled
		w.mm.modifyBotCEXBalance(w.botID, trade.toAsset, baseFilledDelta, balanceModIncrease)
		trade.baseFilled = update.BaseFilled
		trade.quoteFilled = update.QuoteFilled
	}

	if !update.Complete {
		return
	}

	if trade.sell && trade.qty > trade.baseFilled {
		unfilledQty := trade.qty - trade.baseFilled
		w.mm.modifyBotCEXBalance(w.botID, trade.fromAsset, unfilledQty, balanceModIncrease)
	}

	if !trade.sell && calc.BaseToQuote(trade.rate, trade.qty) > trade.quoteFilled {
		unfilledQty := calc.BaseToQuote(trade.rate, trade.qty) - trade.quoteFilled
		w.mm.modifyBotCEXBalance(w.botID, trade.fromAsset, unfilledQty, balanceModIncrease)
	}

	delete(w.trades, update.TradeID)
}

// SubscibeTradeUpdates subscribes to trade updates for the bot's trades on the
// CEX. This should be called before making any trades, and only once.
func (w *wrappedCEX) SubscribeTradeUpdates() (<-chan *libxc.TradeUpdate, func()) {
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
	forwardUpdates := make(chan *libxc.TradeUpdate, 256)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case note := <-updates:
				w.handleTradeUpdate(note)
				forwardUpdates <- note
			}
		}
	}()

	return forwardUpdates, forwardUnsubscribe
}

// Trade executes a trade on the CEX. The trade will be executed using the
// bot's CEX balance.
func (w *wrappedCEX) Trade(ctx context.Context, baseID, quoteID uint32, sell bool, rate, qty uint64) (string, error) {
	var fromAssetID, toAssetID uint32
	var fromAssetQty uint64
	if sell {
		fromAssetID = baseID
		toAssetID = quoteID
		fromAssetQty = qty
	} else {
		fromAssetID = quoteID
		toAssetID = baseID
		fromAssetQty = calc.BaseToQuote(rate, qty)
	}

	fromAssetBal := w.mm.botCEXBalance(w.botID, fromAssetID)
	if fromAssetBal < fromAssetQty {
		return "", fmt.Errorf("asset bal < required for trade (%d < %d)", fromAssetBal, fromAssetQty)
	}

	w.mm.modifyBotCEXBalance(w.botID, fromAssetID, fromAssetQty, balanceModDecrease)
	var success bool
	defer func() {
		if !success {
			w.mm.modifyBotCEXBalance(w.botID, fromAssetID, fromAssetQty, balanceModIncrease)
		}
	}()

	w.subscriptionIDMtx.RLock()
	subscriptionID := w.subscriptionID
	w.subscriptionIDMtx.RUnlock()
	if w.subscriptionID == nil {
		return "", fmt.Errorf("Trade called before SubscribeTradeUpdates")
	}

	w.tradesMtx.Lock()
	defer w.tradesMtx.Unlock()
	tradeID, err := w.CEX.Trade(ctx, baseID, quoteID, sell, rate, qty, *subscriptionID)
	if err != nil {
		return "", err
	}

	success = true
	w.trades[tradeID] = &cexTrade{
		qty:       qty,
		sell:      sell,
		fromAsset: fromAssetID,
		toAsset:   toAssetID,
		rate:      rate,
	}

	return tradeID, nil
}

// wrappedCoreForBot returns a wrappedCore for the specified bot.
func (m *MarketMaker) wrappedCEXForBot(botID string, cex libxc.CEX) *wrappedCEX {
	return &wrappedCEX{
		CEX:    cex,
		botID:  botID,
		log:    m.log,
		mm:     m,
		trades: make(map[string]*cexTrade),
	}
}
