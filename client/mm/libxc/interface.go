package libxc

import (
	"context"
	"fmt"

	"decred.org/dcrdex/dex"
)

// ExchangeBalance holds the available and locked balances of an asset
// on a CEX.
type ExchangeBalance struct {
	Available uint64 `json:"available"`
	Locked    uint64 `json:"locked"`
}

// TradeUpdate is a notification sent when the status of a trade on the CEX
// has been updated.
type TradeUpdate struct {
	TradeID  string
	Complete bool // cancelled or filled
}

// Market is the base and quote assets of a market on a CEX.
type Market struct {
	Base  uint32 `json:"base"`
	Quote uint32 `json:"quote"`
}

// CEX implements a set of functions that can be used to interact with a
// centralized exchange's spot trading API. All rates and quantities
// when interacting with the CEX interface will adhere to the standard
// rates and quantities of the DEX.
type CEX interface {
	Connect(context.Context) error
	// Balance returns the balance of an asset at the CEX.
	Balance(symbol string) (*ExchangeBalance, error)
	// Balances returns a list of all asset balances at the CEX. Only assets that are
	// registered in the DEX client will be returned.
	Balances() (map[uint32]*ExchangeBalance, error)
	// CancelTrade cancels a trade on the CEX.
	CancelTrade(baseSymbol, quoteSymbol, tradeID string) error
	// GenerateTradeID returns a trade ID that must be passed as an argument
	// when calling Trade. This ID will be used to identify updates to the
	// trade. It is necessary to pre-generate this because updates to the
	// trade may arrive before the Trade function returns.
	GenerateTradeID() string
	// Markets returns the list of markets at the CEX.
	Markets() ([]*Market, error)
	// SubscribeCEXUpdates returns a channel which sends an empty struct when
	// the balance of an asset on the CEX has been updated.
	SubscribeCEXUpdates() <-chan interface{}
	// SubscribeMarket subscribes to order book updates on a market. This must
	// be called before calling VWAP.
	SubscribeMarket(baseSymbol, quoteSymbol string) error
	// SubscribeTradeUpdates returns a channel that the caller can use to
	// listen for updates to a trade's status. If the integer returned from
	// this function is passed as the updaterID argument to Trade, then updates
	// to the trade will be sent on the channel.
	SubscribeTradeUpdates() (<-chan *TradeUpdate, int)
	// Trade executes a trade on the CEX. updaterID takes an ID returned from
	// SubscribeTradeUpdates, and tradeID takes an ID returned from
	// GenerateTradeID.
	Trade(baseSymbol, quoteSymbol string, sell bool, rate, qty uint64, updaterID int, tradeID string) error
	// UnsubscribeMarket unsubscribes from order book updates on a market.
	UnsubscribeMarket(baseSymbol, quoteSymbol string) error
	// VWAP returns the volume weighted average price for a certain quantity
	// of the base asset on a market.
	VWAP(baseSymbol, quoteSymbol string, sell bool, qty uint64) (vwap, extrema uint64, filled bool, err error)
}

const (
	Binance   = "Binance"
	BinanceUS = "BinanceUS"
)

// IsValidCEXName returns whether or not a cex name is supported.
func IsValidCexName(cexName string) bool {
	return cexName == Binance || cexName == BinanceUS
}

// NewCEX creates a new CEX.
func NewCEX(cexName string, apiKey, secretKey string, log dex.Logger, net dex.Network) (CEX, error) {
	switch cexName {
	case Binance:
		return newBinance(apiKey, secretKey, log, net, false), nil
	case BinanceUS:
		return newBinance(apiKey, secretKey, log, net, true), nil
	default:
		return nil, fmt.Errorf("unrecognized CEX: %v", cexName)
	}
}
