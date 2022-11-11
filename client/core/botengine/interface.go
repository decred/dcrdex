package botengine

import (
	"context"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex/order"
)

type EngineType string

const (
	GapEngineType EngineType = "GapEngine"
)

// EpochNote is a notification that is sent to a BotEngine to notify
// that a new epoch has begun.
type EpochNote uint64

// OrderNote is sent on the trade feed to signal a buy/sell order.
type OrderNote struct {
	Lots uint64
	Rate uint64
	Sell bool
}

// CancelNote is sent on the trade feed to signal a cancel.
type CancelNote struct {
	OID order.OrderID
}

// Order represents an order on the DEX order book.
type Order struct {
	Sell   bool
	ID     order.OrderID
	Status order.OrderStatus
	Rate   uint64
	Lots   uint64
	Epoch  uint64
}

// MaxOrderEstimate is an estimate of the largest order a wallet can make.
type MaxOrderEstimate struct {
	Swap   *asset.SwapEstimate   `json:"swap"`
	Redeem *asset.RedeemEstimate `json:"redeem"`
}

// BotEngine defines the function required to implement an engine for a market
// maker bot.
type BotEngine interface {
	Run(context.Context)
	Notify(interface{})
	TradeFeed() <-chan interface{}
	Update([]byte) error
	InitialLotsRequired() uint64
}
