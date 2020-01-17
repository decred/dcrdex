// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package core

import (
	"strings"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	"github.com/decred/dcrd/dcrec/secp256k1/v2"
)

// WalletForm is information necessary to create a new exchange wallet.
type WalletForm struct {
	AssetID uint32
	Account string
	INIPath string
}

// Wallet is a wallet.
type Wallet struct {
	asset.Wallet
	waiter  *dex.StartStopWaiter
	AssetID uint32
}

// Registration is information necessary to register an account on a DEX.
type Registration struct {
	DEX      string
	Password string
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
	MarketBuyBuffer float64 `json:"buybuffer"`
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

type account struct {
	url       string
	encKey    []byte
	privKey   *secp256k1.PrivateKey
	dexPubKey *secp256k1.PublicKey
	feeCoin   []byte
}
