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
	Cancel(pw []byte, oidB dex.Bytes) error
	Trade(pw []byte, form *core.TradeForm) (*core.Order, error)
	MaxBuy(host string, base, quote uint32, rate uint64) (*core.MaxOrderEstimate, error)
	MaxSell(host string, base, quote uint32) (*core.MaxOrderEstimate, error)
}

// MarketMaker handles the market making process. It supports running different
// strategies on different markets.
type MarketMaker struct {
	ctx     context.Context
	die     context.CancelFunc
	running atomic.Bool
	log     dex.Logger
	core    *core.Core
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
		mkt := fmt.Sprintf("%s-%d-%d", cfg.Host, cfg.BaseAsset, cfg.QuoteAsset)
		if _, found := mkts[mkt]; found {
			return fmt.Errorf("duplicate bot config for market %s", mkt)
		}
		mkts[mkt] = struct{}{}
	}

	return nil
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

	var oracle *priceOracle
	marketsRequiringOracle := marketsRequiringPriceOracle(cfgs)
	if len(marketsRequiringOracle) > 0 {
		oracle, err = newPriceOracle(m.ctx, marketsRequiringOracle, m.log.SubLogger("PriceOracle"))
		if err != nil {
			return fmt.Errorf("failed to create PriceOracle: %v", err)
		}
	}

	startedMarketMaking = true

	wg := new(sync.WaitGroup)
	for _, cfg := range cfgs {
		switch {
		case cfg.MMCfg != nil:
			wg.Add(1)
			go func(cfg *BotConfig) {
				logger := m.log.SubLogger(fmt.Sprintf("MarketMaker-%s-%d-%d", cfg.Host, cfg.BaseAsset, cfg.QuoteAsset))
				RunBasicMarketMaker(m.ctx, cfg, m.core, oracle, pw, logger)
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
