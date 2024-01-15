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

// Market is the base and quote assets of a market on a CEX.
type Market struct {
	BaseID  uint32 `json:"baseID"`
	QuoteID uint32 `json:"quoteID"`
}

// CEX implements a set of functions that can be used to interact with a
// centralized exchange's spot trading API. All rates and quantities
// when interacting with the CEX interface will adhere to the standard
// rates and quantities of the DEX.
type CEX interface {
	dex.Connector
	// Balance returns the balance of an asset at the CEX.
	Balance(assetID uint32) (*ExchangeBalance, error)
	// CancelTrade cancels a trade on the CEX.
	CancelTrade(ctx context.Context, baseID, quoteID uint32, tradeID string) error
	// Markets returns the list of markets at the CEX.
	Markets(ctx context.Context) ([]*Market, error)
	// SubscribeCEXUpdates returns a channel which sends an empty struct when
	// the balance of an asset on the CEX has been updated.
	SubscribeCEXUpdates() (updates <-chan interface{}, unsubscribe func())
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
	// VWAP returns the volume weighted average price for a certainWithdraw(address string, value, feeRate uint64)  quantity
	// of the base asset on a market.
	VWAP(baseID, quoteID uint32, sell bool, qty uint64) (vwap, extrema uint64, filled bool, err error)
	// GetDepositAddress returns a deposit address for an asset.
	GetDepositAddress(ctx context.Context, assetID uint32) (string, error)
	// ConfirmDeposit is an async function that calls onConfirm when the status
	// of a deposit has been confirmed.
	ConfirmDeposit(ctx context.Context, txID string, onConfirm func(success bool, amount uint64))
	// Withdraw withdraws funds from the CEX to a certain address. onComplete
	// is called with the actual amount withdrawn (amt - fees) and the
	// transaction ID of the withdrawal.
	Withdraw(ctx context.Context, assetID uint32, amt uint64, address string, onComplete func(amt uint64, txID string)) error
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
