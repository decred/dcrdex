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
	BaseID  uint32 `json:"base"`
	QuoteID uint32 `json:"quote"`
}

// CEX implements a set of functions that can be used to interact with a
// centralized exchange's spot trading API. All rates and quantities
// when interacting with the CEX interface will adhere to the standard
// rates and quantities of the DEX.
type CEX interface {
	dex.Connector
	// Balance returns the balance of an asset at the CEX.
	Balance(symbol string) (*ExchangeBalance, error)
	// Balances returns a list of all asset balances at the CEX. Only assets that are
	// registered in the DEX client will be returned.
	Balances() (map[uint32]*ExchangeBalance, error)
	// CancelTrade cancels a trade on the CEX.
	CancelTrade(ctx context.Context, baseSymbol, quoteSymbol, tradeID string) error
	// Markets returns the list of markets at the CEX.
	Markets() ([]*Market, error)
	// SubscribeCEXUpdates returns a channel which sends an empty struct when
	// the balance of an asset on the CEX has been updated.
	SubscribeCEXUpdates() (updates <-chan interface{}, unsubscribe func())
	// SubscribeMarket subscribes to order book updates on a market. This must
	// be called before calling VWAP.
	SubscribeMarket(ctx context.Context, baseSymbol, quoteSymbol string) error
	// SubscribeTradeUpdates returns a channel that the caller can use to
	// listen for updates to a trade's status. When the subscription ID
	// returned from this function is passed as the updaterID argument to
	// Trade, then updates to the trade will be sent on the updated channel
	// returned from this function.
	SubscribeTradeUpdates() (updates <-chan *TradeUpdate, unsubscribe func(), subscriptionID int)
	// Trade executes a trade on the CEX. updaterID takes a subscriptionID
	// returned from SubscribeTradeUpdates.
	Trade(ctx context.Context, baseSymbol, quoteSymbol string, sell bool, rate, qty uint64, subscriptionID int) (string, error)
	// UnsubscribeMarket unsubscribes from order book updates on a market.
	UnsubscribeMarket(baseSymbol, quoteSymbol string)
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
