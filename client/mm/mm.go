// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mm

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/client/orderbook"
	"decred.org/dcrdex/dex"
)

// clientCore is satisfied by core.Core.
type clientCore interface {
	NotificationFeed() *core.NoteFeed
	ExchangeMarket(host string, base, quote uint32) (*core.Market, error)
	SyncBook(host string, base, quote uint32) (*orderbook.OrderBook, core.BookFeed, error)
	SupportedAssets() map[uint32]*core.SupportedAsset
	SingleLotFees(form *core.SingleLotFeesForm) (uint64, uint64, error)
	Cancel(oidB dex.Bytes) error
	Trade(pw []byte, form *core.TradeForm) (*core.Order, error)
	MaxBuy(host string, base, quote uint32, rate uint64) (*core.MaxOrderEstimate, error)
	MaxSell(host string, base, quote uint32) (*core.MaxOrderEstimate, error)
	AssetBalance(assetID uint32) (*core.WalletBalance, error)
	PreOrder(form *core.TradeForm) (*core.OrderEstimate, error)
	WalletState(assetID uint32) *core.WalletState
	MultiTrade(pw []byte, form *core.MultiTradeForm) ([]*core.Order, error)
	MaxFundingFees(fromAsset uint32, numTrades uint32, options map[string]string) (uint64, error)
}

var _ clientCore = (*core.Core)(nil)

// MarketMaker handles the market making process. It supports running different
// strategies on different markets.
type MarketMaker struct {
	ctx     context.Context
	die     context.CancelFunc
	running atomic.Bool
	log     dex.Logger
	core    *core.Core
	bh      *balanceHandler
}

// Running returns true if the MarketMaker is running.
func (m *MarketMaker) Running() bool {
	return m.running.Load()
}

func marketsRequiringPriceOracle(cfgs []*BotConfig) []*mkt {
	mkts := make([]*mkt, 0, len(cfgs))

	for _, cfg := range cfgs {
		if cfg.requiresPriceOracle() {
			mkts = append(mkts, &mkt{base: cfg.BaseAsset, quote: cfg.QuoteAsset})
		}
	}

	return mkts
}

// duplicateBotConfig returns an error if there is more than one bot config for
// the same market on the same dex host.
func duplicateBotConfig(cfgs []*BotConfig) error {
	mkts := make(map[string]struct{})

	for _, cfg := range cfgs {
		mkt := dexMarketID(cfg.Host, cfg.BaseAsset, cfg.QuoteAsset)
		if _, found := mkts[mkt]; found {
			return fmt.Errorf("duplicate bot config for market %s", mkt)
		}
		mkts[mkt] = struct{}{}
	}

	return nil
}

func priceOracleFromConfigs(ctx context.Context, cfgs []*BotConfig, log dex.Logger) (*priceOracle, error) {
	var oracle *priceOracle
	var err error
	marketsRequiringOracle := marketsRequiringPriceOracle(cfgs)
	if len(marketsRequiringOracle) > 0 {
		oracle, err = newPriceOracle(ctx, marketsRequiringOracle, log)
		if err != nil {
			return nil, fmt.Errorf("failed to create PriceOracle: %v", err)
		}
	}

	return oracle, nil
}

// Run starts the MarketMaker. There can only be one BotConfig per dex market.
func (m *MarketMaker) Run(ctx context.Context, cfgs []*BotConfig, pw []byte) error {
	if !m.running.CompareAndSwap(false, true) {
		return errors.New("market making is already running")
	}

	var startedMarketMaking bool
	defer func() {
		if !startedMarketMaking {
			m.running.Store(false)
		}
	}()

	m.ctx, m.die = context.WithCancel(ctx)

	if err := duplicateBotConfig(cfgs); err != nil {
		return err
	}

	err := m.core.Login(pw)
	if err != nil {
		return fmt.Errorf("failed to login: %w", err)
	}

	// TODO: unlock all wallets

	oracle, err := priceOracleFromConfigs(m.ctx, cfgs, m.log.SubLogger("PriceOracle"))
	if err != nil {
		return err
	}

	m.bh, err = newBalanceHandler(cfgs, m.core, m.log.SubLogger("BalanceHandler"))
	if err != nil {
		return err
	}

	startedMarketMaking = true

	wg := new(sync.WaitGroup)
	wg.Add(1)
	go func() {
		defer wg.Done()
		m.bh.run(m.ctx)
	}()

	for _, cfg := range cfgs {
		switch {
		case cfg.MMCfg != nil:
			wg.Add(1)
			go func(cfg *BotConfig) {
				logger := m.log.SubLogger(fmt.Sprintf("MarketMaker-%s-%d-%d", cfg.Host, cfg.BaseAsset, cfg.QuoteAsset))
				mktID := dexMarketID(cfg.Host, cfg.BaseAsset, cfg.QuoteAsset)
				RunBasicMarketMaker(m.ctx, cfg, m.bh.wrappedCoreForBot(mktID), oracle, pw, logger)
				wg.Done()
			}(cfg)
		default:
			m.log.Errorf("Only basic market making is supported at this time. Skipping %s-%d-%d", cfg.Host, cfg.BaseAsset, cfg.QuoteAsset)
		}
	}

	go func() {
		wg.Wait()
		m.log.Infof("All bots have stopped running.")
		m.running.Store(false)
	}()

	return nil
}

// BotBalances returns the amount of each asset currently allocated to each
// bot. This can only be called when the MarketMaker is running.
func (m *MarketMaker) BotBalances() map[string]map[uint32]uint64 {
	if !m.running.Load() {
		return nil
	}

	return m.bh.allBotBalances()
}

// Stop stops the MarketMaker.
func (m *MarketMaker) Stop() {
	if m.die != nil {
		m.die()
	}
}

// NewMarketMaker creates a new MarketMaker.
func NewMarketMaker(core *core.Core, log dex.Logger) (*MarketMaker, error) {
	return &MarketMaker{
		core:    core,
		log:     log,
		running: atomic.Bool{},
	}, nil
}
