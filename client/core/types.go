// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package core

import "strings"

// Registration is used to register a new DEX account.
type Registration struct {
	DEX        string `json:"dex"`
	Wallet     string `json:"wallet"`
	WalletPass string `json:"walletpass"`
	RPCAddr    string `json:"rpcaddr"`
	RPCUser    string `json:"rpcuser"`
	RPCPass    string `json:"rpcpass"`
}

// MarketInfo contains information about the markets for a DEX server.
type MarketInfo struct {
	DEX     string   `json:"dex"`
	Markets []Market `json:"markets"`
}

// Market is market info.
type Market struct {
	BaseID          uint32  `json:"baseid"`
	BaseSymbol      string  `json:"basesymbol"`
	QuoteID         uint32  `json:"quoteid"`
	QuoteSymbol     string  `json:"quotesymbol"`
	EpochLen        uint64  `json:"epochlen"`
	StartEpoch      uint64  `json:"startepoch"`
	MarketBuyBuffer float32 `json:"buybuffer"`
}

// Display returns an ID string suitable for displaying in a UI.
func (m *Market) Display() string {
	return strings.ToUpper(m.BaseSymbol) + "-" + strings.ToUpper(m.QuoteSymbol)
}

// MiniOrder is minimal information about an order in a market's order book.
type MiniOrder struct {
	Qty   float64 `json:"qty"`
	Rate  float64 `json:"rate"`
	Epoch bool    `json:"epoch"`
}

// OrderBook represents an order book, which is just two sorted lists of orders.
type OrderBook struct {
	Sells []*MiniOrder `json:"sells"`
	Buys  []*MiniOrder `json:"buys"`
}

// BookUpdate is an order book update.
type BookUpdate struct {
	Market string
}
