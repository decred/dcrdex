package libxc

import (
	"context"
	"errors"
	"fmt"

	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/dex"
)

// ExchangeBalance holds the available and locked balances of an asset
// on a CEX.
type ExchangeBalance struct {
	Available uint64 `json:"available"`
	Locked    uint64 `json:"locked"`
}

// Trade represents a trade made on a CEX.
type Trade struct {
	ID          string
	Sell        bool
	Qty         uint64
	Rate        uint64
	BaseID      uint32
	QuoteID     uint32
	BaseFilled  uint64
	QuoteFilled uint64
	Complete    bool // cancelled or filled
}

type MarketDay struct {
	Vol            float64 `json:"vol"`
	QuoteVol       float64 `json:"quoteVol"`
	PriceChange    float64 `json:"priceChange"`
	PriceChangePct float64 `json:"priceChangePct"`
	AvgPrice       float64 `json:"avgPrice"`
	LastPrice      float64 `json:"lastPrice"`
	OpenPrice      float64 `json:"openPrice"`
	HighPrice      float64 `json:"highPrice"`
	LowPrice       float64 `json:"lowPrice"`
}

// Market is the base and quote assets of a market on a CEX.
type Market struct {
	BaseID           uint32     `json:"baseID"`
	QuoteID          uint32     `json:"quoteID"`
	Day              *MarketDay `json:"day"`
	BaseMinWithdraw  uint64     `json:"baseMinWithdraw"`
	QuoteMinWithdraw uint64     `json:"quoteMinWithdraw"`
}

type Status struct {
	Markets  map[string]*Market          `json:"markets"`
	Balances map[uint32]*ExchangeBalance `json:"balances"`
}

type DepositData struct {
	AssetID            uint32
	AmountConventional float64
	TxID               string
}

// MarketMatch is a market for which both assets are supported by Bison Wallet.
type MarketMatch struct {
	BaseID  uint32 `json:"baseID"`
	QuoteID uint32 `json:"quoteID"`
	// MarketID is the id used by DCRDEX.
	MarketID string `json:"marketID"`
	// Slug is a market identifier used by the cex.
	Slug string `json:"slug"`
}

type BalanceUpdate struct {
	AssetID uint32           `json:"assetID"`
	Balance *ExchangeBalance `json:"balance"`
}

var (
	ErrWithdrawalPending = errors.New("withdrawal pending")
	ErrUnsyncedOrderbook = errors.New("orderbook not synced")
)

// CEX implements a set of functions that can be used to interact with a
// centralized exchange's spot trading API. All rates and quantities
// when interacting with the CEX interface will adhere to the standard
// rates and quantities of the DEX.
type CEX interface {
	dex.Connector
	// Balance returns the balance of an asset at the CEX.
	Balance(assetID uint32) (*ExchangeBalance, error)
	// Balances returns the balances of known assets on the CEX.
	Balances(ctx context.Context) (map[uint32]*ExchangeBalance, error)
	// CancelTrade cancels a trade on the CEX.
	CancelTrade(ctx context.Context, baseID, quoteID uint32, tradeID string) error
	// Markets returns the list of markets at the CEX.
	Markets(ctx context.Context) (map[string]*Market, error)
	// SubscribeMarket subscribes to order book updates on a market. This must
	// be called before calling VWAP.
	SubscribeMarket(ctx context.Context, baseID, quoteID uint32) error
	// SubscribeTradeUpdates returns a channel that the caller can use to
	// listen for updates to a trade's status. When the subscription ID
	// returned from this function is passed as the updaterID argument to
	// Trade, then updates to the trade will be sent on the updated channel
	// returned from this function.
	SubscribeTradeUpdates() (updates <-chan *Trade, unsubscribe func(), subscriptionID int)
	// Trade executes a trade on the CEX. updaterID takes a subscriptionID
	// returned from SubscribeTradeUpdates.
	Trade(ctx context.Context, baseID, quoteID uint32, sell bool, rate, qty uint64, subscriptionID int) (*Trade, error)
	// UnsubscribeMarket unsubscribes from order book updates on a market.
	UnsubscribeMarket(baseID, quoteID uint32) error
	// VWAP returns the volume weighted average price for a certain quantity
	// of the base asset on a market. The sell parameter specifies the side of
	// the market on which to get the average price. SubscribeMarket must be
	// called, and the market must be synced before results can be expected.
	VWAP(baseID, quoteID uint32, sell bool, qty uint64) (vwap, extrema uint64, filled bool, err error)
	// MidGap returns the mid-gap price for an order book.
	MidGap(baseID, quoteID uint32) uint64
	// GetDepositAddress returns a deposit address for an asset.
	GetDepositAddress(ctx context.Context, assetID uint32) (string, error)
	// ConfirmDeposit is an async function that calls onConfirm when the status
	// of a deposit has been confirmed.
	ConfirmDeposit(ctx context.Context, deposit *DepositData) (bool, uint64)
	// Withdraw withdraws funds from the CEX to a certain address. The txID
	// and the amount the cex balance was reduced by are returned. This may
	// be equal to or higher than the amount requested due to fees, depending
	// on the CEX.
	Withdraw(ctx context.Context, assetID uint32, amt uint64, address string) (string, uint64, error)
	// ConfirmWithdrawal checks whether a withdrawal has been completed. If the
	// withdrawal has not yet been sent, ErrWithdrawalPending is returned.
	ConfirmWithdrawal(ctx context.Context, withdrawalID string, assetID uint32) (uint64, string, error)
	// TradeStatus returns the current status of a trade.
	TradeStatus(ctx context.Context, id string, baseID, quoteID uint32) (*Trade, error)
	// Book generates the CEX's current view of a market's orderbook.
	Book(baseID, quoteID uint32) (buys, sells []*core.MiniOrder, _ error)
}

const (
	Binance   = "Binance"
	BinanceUS = "BinanceUS"
	Coinbase  = "Coinbase"
)

// IsValidCEXName returns whether or not a cex name is supported.
func IsValidCexName(cexName string) bool {
	return cexName == Binance || cexName == BinanceUS
}

type CEXConfig struct {
	Net       dex.Network
	APIKey    string
	SecretKey string
	Logger    dex.Logger
	Notify    func(interface{})
}

// NewCEX creates a new CEX.
func NewCEX(cexName string, cfg *CEXConfig) (CEX, error) {
	switch cexName {
	case Binance:
		return newBinance(cfg, false), nil
	case BinanceUS:
		return newBinance(cfg, true), nil
	case Coinbase:
		return newCoinbase(cfg)
	default:
		return nil, fmt.Errorf("unrecognized CEX: %v", cexName)
	}
}
