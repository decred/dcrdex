// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package main

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/client/db"
	"decred.org/dcrdex/dex"
)

// A Trader is a client routine to interact with the server. Each Trader passed
// to runTrader will get its own *Mantle, which embed *core.Core and provides
// some additional utilities.
type Trader interface {
	// SetupWallets should create the Trader's wallets. It is an error if a
	// Decred wallet is not created by SetupWallets.
	SetupWallets(*Mantle)
	// HandleNotification is a receiver for core.Notifications. The Trader will
	// use the provided *Mantle to perform any requisite actions in response to
	// the Notification. Trading can begin as soon as the *FeePaymentNote with
	// subject AccountRegisteredSubject is received.
	HandleNotification(*Mantle, core.Notification)
	// HandleBookNote(*Mantle, *core.BookUpdate)
}

// runTrader is the LoadBot workhorse. Creates a new mantle and runs the Trader.
// runTrader will block until the ctx is canceled.
func runTrader(t Trader, name string) {
	m, err := NewMantle(name)
	if err != nil {
		log.Errorf("failed to create new Mantle: %v", err)
		return
	}

	t.SetupWallets(m)

	if ctx.Err() != nil {
		return
	}

	_, err = m.Register(&core.RegisterForm{
		Addr:    hostAddr,
		AppPass: pass,
		Fee:     defaultRegFee,
		Cert:    filepath.Join(dextestDir, "dcrdex", "rpc.cert"),
	})
	if err != nil {
		m.fatalError("registration error: %v", err)
		return
	}

out:
	for {
		select {
		case note := <-m.notes:
			if note.Severity() >= db.ErrorLevel {
				m.fatalError("Error note received: %s", mustJSON(note))
				return
			}
			switch n := note.(type) {
			case *core.FeePaymentNote:
				// Once registration is complete, register for a book feed.
				if n.Topic() == core.TopicAccountRegistered {
					// Even if we're not going to use it, we need to subscribe
					// to a book feed and keep the channel empty, so that we
					// can keep receiving book feed notifications.
					bookFeed, err := m.SyncBook(hostAddr, dcrID, btcID)
					if err != nil {
						m.fatalError("SyncBook error: %v", err)
						return
					}
					go func() {
						for {
							select {
							case <-bookFeed.C:
								// If we ever enable the  thebook feed, we
								// would pass the update to the Trader here.
								// For now, just keep the channel empty.
								m.log.Tracef("book note received")
							case <-ctx.Done():
								return
							}
						}
					}()
				}
			case *core.EpochNotification:
				m.log.Debugf("Epoch note received: %s", mustJSON(note))
				if n.MarketID == dcrBtcMarket {
					m.replenishBalances()
				}
			case *core.MatchNote:
				if n.Topic() == core.TopicNewMatch {
					atomic.AddUint32(&matchCounter, 1)
				}
			}

			t.HandleNotification(m, note)
		case <-ctx.Done():
			break out
		}
	}

	// Let Core shutdown and lock up.
	m.waiter.WaitForShutdown()
}

// A Mantle is a wrapper for *core.Core that adds some useful LoadBot methods
// and fields.
type Mantle struct {
	*core.Core
	waiter  *dex.StartStopWaiter
	name    string
	log     dex.Logger
	notes   <-chan core.Notification
	wallets map[uint32]*botWallet
}

// NewMantle is a constructor for a *Mantle. Each Mantle has its own core. The
// embedded Core is initialized, but not registered.
func NewMantle(name string) (*Mantle, error) {
	dbPath := filepath.Join(botDir, "mantle_"+name+".db")
	c, err := core.New(&core.Config{
		DBPath: dbPath,
		Net:    dex.Simnet,
		Logger: loggerMaker.Logger("CORE:" + name),
	})
	if err != nil {
		return nil, fmt.Errorf("error initializing core: %w", err)
	}

	waiter := dex.NewStartStopWaiter(c)
	waiter.Start(ctx)

	err = c.InitializeClient(pass, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize client")
	}

	m := &Mantle{
		Core:    c,
		waiter:  waiter,
		name:    name,
		log:     loggerMaker.Logger("MANTLE:" + name),
		wallets: make(map[uint32]*botWallet),
		notes:   c.NotificationFeed(),
	}

	return m, nil
}

// fatalError kills the LoadBot by cancelling the global Context.
func (m *Mantle) fatalError(s string, a ...interface{}) {
	m.log.Criticalf(s, a...)
	quit()
}

// order places an order on the dcr_btc market.
func (m *Mantle) order(sell bool, qty, rate uint64) error {
	_, err := m.Trade(pass, coreLimitOrder(sell, qty, rate))
	if err != nil {
		if isOverLimitError(err) {
			m.log.Infof("Over-limit error. Order not placed.")
		} else {
			m.fatalError("Trade error (limit order, sell = %t, qty = %d, rate = %d): %v", sell, qty, rate, err)
		}
		return err
	}
	atomic.AddUint32(&orderCounter, 1)
	return nil
}

type orderReq struct {
	sell bool
	qty  uint64
	rate uint64
}

// orderMetered places a series of orders spaced out over a specific redemption.
// The orders are spaced so that the period for n orders is dur / n, not
// dur / (n - 1), meaning there is no order placed at time now + dur, the last
// order is placed at now + dur - (dur / n). This is a convenience so that the
// caller can target n orders per epoch with a dur = mkt.EpochDuration, and
// avoid the simultaneous order at the beginning of the next epoch.
// order places an order on the dcr_btc market.
func (m *Mantle) orderMetered(ords []*orderReq, dur time.Duration) {
	if len(ords) == 0 {
		return
	}
	placeOrder := func() error {
		ord := ords[0]
		ords = ords[1:]
		return m.order(ord.sell, ord.qty, ord.rate)
	}
	err := placeOrder()
	if isOverLimitError(err) {
		return
	}
	if len(ords) == 0 {
		// There was only one to place. No need to set a ticker.
		return
	}
	go func() {
		ticker := time.NewTicker(dur / time.Duration(len(ords)))
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				err := placeOrder()
				if isOverLimitError(err) {
					return
				}
				if len(ords) == 0 {
					// There was only one to place. No need to set a ticker.
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
}

// order places an order on the dcr_btc market.
func (m *Mantle) marketOrder(sell bool, qty uint64) {
	mo := coreLimitOrder(sell, qty, 0)
	mo.IsLimit = false
	_, err := m.Trade(pass, mo)
	if err != nil {
		if isOverLimitError(err) {
			m.log.Infof("Over-limit error. Order not placed.")
		} else {
			m.fatalError("Trade error (market order, sell = %t, qty = %d: %v", sell, qty, err)
		}
		return
	}
	atomic.AddUint32(&orderCounter, 1)
}

// book gets the dcr_btc book, or kills LoadBot on error.
func (m *Mantle) book() *core.OrderBook {
	book, err := m.Book(hostAddr, dcrID, btcID)
	if err != nil {
		m.fatalError("sideStacker error getting order book: %v", err)
		return &core.OrderBook{}
	}
	return book
}

// truncatedMidGap is the dcr_btc mid-gap value truncated to the next lowest
// multiple of BTC rate-step.
func (m *Mantle) truncatedMidGap() uint64 {
	midGap := midGap(m.book(), rateStep)
	return truncate(int64(midGap), int64(rateStep))
}

// createWallet creates a new wallet/account for the asset and node. If an error
// is encountered, LoadBot will be killed.
func (m *Mantle) createWallet(symbol, node string, minFunds, maxFunds uint64, numCoins int) {
	// Generate a name for this wallet.
	name := randomToken()
	cmdOut := <-harnessCtl(symbol, "./new-wallet", node, name)
	if cmdOut.err != nil {
		m.fatalError("%s new-wallet error: %v", symbol, cmdOut.err)
		return
	}
	m.log.Infof("created wallet %s:%s on node %s", symbol, name, node)
	// Even though the harnessCtl is synchronous, I've still observed some
	// issues with trying to create the wallet immediately.
	<-time.After(time.Second)

	var walletPass []byte
	if symbol == dcr {
		walletPass = pass
	}
	w := newBotWallet(symbol, node, name, walletPass, minFunds, maxFunds, numCoins)
	m.wallets[w.assetID] = w
	err := m.CreateWallet(pass, walletPass, w.form)
	if err != nil {
		m.fatalError("Mantle %s failed to create wallet: %v", m.name, err)
		return
	}
	coreWallet := m.WalletState(w.assetID)
	if coreWallet == nil {
		m.fatalError("Failed to retrieve WalletState for newly created %s wallet, node %s", symbol, node)
		return
	}
	w.address = coreWallet.Address
	if numCoins < 1 {
		return
	}

	chunk := maxFunds/uint64(numCoins) + 1
	for i := 0; i < numCoins; i++ {
		<-harnessCtl(symbol, fmt.Sprintf("./%s", node), "sendtoaddress", coreWallet.Address, valString(chunk))
	}
	<-harnessCtl(symbol, fmt.Sprintf("./mine-%s", node), "1")
	time.Sleep(time.Second)
}

// replenishBalances will run replenishBalance for all wallets.
func (m *Mantle) replenishBalances() {
	for _, w := range m.wallets {
		m.replenishBalance(w)
	}
}

// replenishBalance will bring the balance with allowable limits by requesting
// funds from or sending funds to the wallet's node.
func (m *Mantle) replenishBalance(w *botWallet) {
	// Get the Balance from the user in case it changed while while this note
	// was in the notification pipeline.
	bal, err := m.AssetBalance(w.assetID)
	if err != nil {
		m.fatalError("error updating %s balance: %v", w.symbol, err)
		return
	}

	m.log.Debugf("Balance note received for %s (minFunds = %s, maxFunds = %s): %s",
		w.symbol, valString(w.minFunds), valString(w.maxFunds), mustJSON(bal))

	effectiveMax := w.maxFunds + (w.maxFunds - w.minFunds)

	if bal.Available < w.minFunds {
		chunk := (w.maxFunds - bal.Available) / uint64(w.numCoins)
		for i := 0; i < w.numCoins; i++ {
			m.log.Debugf("Requesting %s from %s alpha node", valString(chunk), w.symbol)
			cmdOut := <-harnessCtl(w.symbol, "./alpha", "sendtoaddress", w.address, valString(chunk))
			if cmdOut.err != nil {
				m.fatalError("error refreshing balance for %s: %v", w.symbol, cmdOut.err)
			}
		}
	} else if bal.Available > effectiveMax {
		// Send some back to the alpha address.
		amt := bal.Available - w.maxFunds
		m.log.Debugf("Sending %s back to %s alpha node", valString(amt), w.symbol)
		_, err := m.Withdraw(pass, w.assetID, amt, returnAddress(w.symbol, alpha))
		if err != nil {
			m.fatalError("failed to withdraw funds to alpha: %v", err)
		}
	}
}

// mustJSON JSON-encodes the thing. If an error is encountered, the error text
// is returned instead.
func mustJSON(thing interface{}) string {
	s, err := json.Marshal(thing)
	if err != nil {
		return "invalid json: " + err.Error()
	}
	return string(s)
}

// Convert the conventional units value to atoms/sats.
func toAtoms(v float64) uint64 {
	return uint64(math.Round(v * 1e8))
}

// valString returns a string representation of the value in conventional
// units.
func valString(v uint64) string {
	return fmt.Sprintf("%.8f", float64(v)/1e8)
}

// coreOrder creates a *core.Order for dcr_btc.
func coreLimitOrder(sell bool, qty, rate uint64) *core.TradeForm {
	return &core.TradeForm{
		Host:    hostAddr,
		IsLimit: true,
		Sell:    sell,
		Base:    dcrID,
		Quote:   btcID,
		Qty:     qty,
		Rate:    rate,
		TifNow:  false,
	}
}

// midGap parses the provided order book for the mid-gap price. If the book
// is empty, a default value of rateStep * midGapMultiplier is returned instead.
func midGap(book *core.OrderBook, rateStep uint64) uint64 {
	if len(book.Sells) > 0 {
		if len(book.Buys) > 0 {
			return toAtoms((book.Buys[0].Rate + book.Sells[0].Rate) / 2)
		}
		return toAtoms(book.Sells[0].Rate)
	} else {
		if len(book.Buys) > 0 {
			return toAtoms(book.Buys[0].Rate)
		}
	}
	return truncate(defaultBtcPerDcr*1e8, int64(rateStep))
}

// truncate rounds the provided v down to an integer-multiple of mod.
func truncate(v, mod int64) uint64 {
	return uint64(v - (v % mod))
}

// clamp returns the closest value to v within the bounds of [min, max].
func clamp(v, min, max int) int {
	if v > max {
		return max
	}
	if v < min {
		return min
	}
	return v
}

const walletNameLength = 4

var chars = []byte("123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz")

func randomToken() string {
	b := make([]byte, walletNameLength)
	for i := range b {
		b[i] = chars[rand.Intn(len(chars))]
	}
	return string(b)
}

// botWallet is the local wallet representation. Mantle uses the botWallet to
// keep the Core wallet's balance within allowable range.
type botWallet struct {
	form     *core.WalletForm
	name     string
	node     string
	symbol   string
	pass     []byte
	assetID  uint32
	minFunds uint64
	maxFunds uint64
	address  string
	numCoins int
}

// newBotWallet is the constructor for a botWallet. For a botWallet created
// with Mantle.createWallet, the botWallet's balance will be replenished up to
// once per epoch, if it falls outside of the range [minFunds, maxFunds].
// Set numCoins to at least twice the the maximum number of (booked + epoch)
// orders the wallet is expected to support.
func newBotWallet(symbol, node, name string, pass []byte, minFunds, maxFunds uint64, numCoins int) *botWallet {
	var form *core.WalletForm
	switch symbol {
	case dcr:
		form = &core.WalletForm{
			AssetID: dcrID,
			Config: map[string]string{
				"account":   name,
				"username":  "user",
				"password":  "pass",
				"rpccert":   filepath.Join(dextestDir, "dcr/"+node+"/rpc.cert"),
				"rpclisten": rpcAddr(symbol, node),
			},
		}
	case btc:
		form = &core.WalletForm{
			AssetID: btcID,
			Config: map[string]string{
				"walletname":  name,
				"rpcuser":     "user",
				"rpcpassword": "pass",
				"rpcport":     rpcAddr(symbol, node),
			},
		}
	}
	return &botWallet{
		form:     form,
		name:     name,
		node:     node,
		symbol:   symbol,
		pass:     pass,
		assetID:  form.AssetID,
		minFunds: minFunds,
		maxFunds: maxFunds,
		numCoins: numCoins,
	}
}

// isOverLimitError will be true if the error is a ErrQuantityTooHigh,
// indicating the client has reached its order limit. Ideally, Core would
// know the limit and we could query it to use in our algorithm, but the order
// limit change is new and Core doesn't know what to do with it yet.
func isOverLimitError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "order quantity exceeds user limit")
}
