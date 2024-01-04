// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package tanka

import (
	"encoding/binary"
	"encoding/hex"
	"time"

	"github.com/decred/dcrd/crypto/blake256"
)

type Order struct {
	From    PeerID `json:"from"`
	BaseID  uint32 `json:"baseID"`
	QuoteID uint32 `json:"quoteID"`
	Sell    bool   `json:"sell"`
	Qty     uint64 `json:"qty"`
	Rate    uint64 `json:"rate"`
	// LotSize: Tatankanet does not prescribe a lot size. Instead, users must
	// select their own minimum minimum lot size. The user's UI should ignore
	// orderbook orders that don't have the requisite lot size. The UI should
	// show lot size selection in terms of a sliding scale of fee exposure.
	// Lot sizes can only be powers of 2.
	LotSize uint64 `json:"lotSize"`
	// MinFeeRate: Tatankanet does not prescribe a fee rate on an order, but it
	// does supply a suggested fee rate that is updated periodically. The user's
	// UI should ignore an order from the order book if its MinFeeRate falls
	// below the Tatnkanet suggested rate.
	MinFeeRate uint64    `json:"minFeeRate"`
	Stamp      time.Time `json:"stamp"`
	Expiration time.Time `json:"expiration"`
}

func (ord *Order) ID() [32]byte {
	const msgLen = 32 + 4 + 4 + 1 + 8 + 8 + 8 + 8 + 8 + 8
	b := make([]byte, msgLen)
	copy(b[:32], ord.From[:])
	binary.BigEndian.PutUint32(b[32:36], ord.BaseID)
	binary.BigEndian.PutUint32(b[36:40], ord.QuoteID)
	if ord.Sell {
		b[41] = 1
	}
	binary.BigEndian.PutUint64(b[41:49], ord.Qty)
	binary.BigEndian.PutUint64(b[49:57], ord.Rate)
	binary.BigEndian.PutUint64(b[57:65], ord.LotSize)
	binary.BigEndian.PutUint64(b[65:73], ord.MinFeeRate)
	binary.BigEndian.PutUint64(b[73:81], uint64(ord.Stamp.UnixMilli()))
	binary.BigEndian.PutUint64(b[81:89], uint64(ord.Expiration.UnixMilli()))
	return blake256.Sum256(b)

}

type ID32 [32]byte

func (i ID32) String() string {
	return hex.EncodeToString(i[:])
}

type Match struct {
	From    PeerID    `json:"from"`
	OrderID ID32      `json:"orderID"`
	Qty     uint64    `json:"qty"`
	Stamp   time.Time `json:"stamp"`
}

func (m *Match) ID() ID32 {
	const msgLen = 32 + 32 + 8 + 8
	b := make([]byte, msgLen)
	copy(b[:32], m.From[:])
	copy(b[32:64], m.OrderID[:])
	binary.BigEndian.PutUint64(b[64:72], m.Qty)
	binary.BigEndian.PutUint64(b[72:80], uint64(m.Stamp.UnixMilli()))
	return blake256.Sum256(b)
}

type MatchAcceptance struct {
	OrderID ID32 `json:"orderID"`
	MatchID ID32 `json:"matchID"`
}

type MarketParameters struct {
	BaseID  uint32 `json:"baseID"`
	QuoteID uint32 `json:"quoteID"`
}
