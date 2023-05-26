package mm

import (
	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/client/db"
	"decred.org/dcrdex/client/orderbook"
	"decred.org/dcrdex/dex"
)

type tCore struct {
	assetBalances                map[uint32]*core.WalletBalance
	assetBalanceErr              error
	market                       *core.Market
	orderEstimate                *core.OrderEstimate
	sellSwapFees, sellRedeemFees uint64
	buySwapFees, buyRedeemFees   uint64
	singleLotFeesErr             error
	preOrderParam                *core.TradeForm
	tradeResult                  *core.Order
	multiTradeResult             []*core.Order
	noteFeed                     chan core.Notification
	isAccountLocker              map[uint32]bool
	maxBuyEstimate               *core.MaxOrderEstimate
	maxBuyErr                    error
	maxSellEstimate              *core.MaxOrderEstimate
	maxSellErr                   error
	cancelsPlaced                []dex.Bytes
	buysPlaced                   []*core.TradeForm
	sellsPlaced                  []*core.TradeForm
	maxFundingFees               uint64
}

func (c *tCore) NotificationFeed() *core.NoteFeed {
	return &core.NoteFeed{C: c.noteFeed}
}
func (c *tCore) ExchangeMarket(host string, base, quote uint32) (*core.Market, error) {
	return c.market, nil
}
func (*tCore) SyncBook(host string, base, quote uint32) (*orderbook.OrderBook, core.BookFeed, error) {
	return nil, nil, nil
}
func (*tCore) SupportedAssets() map[uint32]*core.SupportedAsset {
	return nil
}
func (c *tCore) SingleLotFees(form *core.SingleLotFeesForm) (uint64, uint64, error) {
	if c.singleLotFeesErr != nil {
		return 0, 0, c.singleLotFeesErr
	}
	if form.Sell {
		return c.sellSwapFees, c.sellRedeemFees, nil
	}
	return c.buySwapFees, c.buyRedeemFees, nil
}
func (*tCore) Cancel(pw []byte, oidB dex.Bytes) error {
	return nil
}
func (c *tCore) Trade(pw []byte, form *core.TradeForm) (*core.Order, error) {
	return c.tradeResult, nil
}
func (*tCore) MaxBuy(host string, base, quote uint32, rate uint64) (*core.MaxOrderEstimate, error) {
	return nil, nil
}
func (*tCore) MaxSell(host string, base, quote uint32) (*core.MaxOrderEstimate, error) {
	return nil, nil
}
func (c *tCore) AssetBalance(assetID uint32) (*core.WalletBalance, error) {
	return c.assetBalances[assetID], c.assetBalanceErr
}
func (c *tCore) PreOrder(form *core.TradeForm) (*core.OrderEstimate, error) {
	c.preOrderParam = form
	return c.orderEstimate, nil
}
func (c *tCore) MultiTrade(pw []byte, forms *core.MultiTradeForm) ([]*core.Order, error) {
	return c.multiTradeResult, nil
}
func (c *tCore) WalletState(assetID uint32) *core.WalletState {
	isAccountLocker := c.isAccountLocker[assetID]

	var traits asset.WalletTrait
	if isAccountLocker {
		traits |= asset.WalletTraitAccountLocker
	}

	return &core.WalletState{
		Traits: traits,
	}
}
func (c *tCore) MaxFundingFees(fromAsset uint32, numTrades uint32, options map[string]string) (uint64, error) {
	return c.maxFundingFees, nil
}

var _ clientCore = (*tCore)(nil)

func (c *tCore) setAssetBalances(balances map[uint32]uint64) {
	c.assetBalances = make(map[uint32]*core.WalletBalance)
	for assetID, bal := range balances {
		c.assetBalances[assetID] = &core.WalletBalance{
			Balance: &db.Balance{
				Balance: asset.Balance{
					Available: bal,
				},
			},
		}
	}
}

func newTCore() *tCore {
	return &tCore{
		assetBalances:   make(map[uint32]*core.WalletBalance),
		noteFeed:        make(chan core.Notification),
		isAccountLocker: make(map[uint32]bool),
	}
}

type tOrderBook struct {
	midGap    uint64
	midGapErr error
}

func (o *tOrderBook) MidGap() (uint64, error) {
	if o.midGapErr != nil {
		return 0, o.midGapErr
	}
	return o.midGap, nil
}

type tOracle struct {
	marketPrice float64
}

func (o *tOracle) getMarketPrice(base, quote uint32) float64 {
	return o.marketPrice
}
