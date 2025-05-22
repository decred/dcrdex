// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package tanka

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
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
	// select their own minimum lot size. The user's UI should ignore
	// orderbook orders that don't have the requisite lot size. The UI should
	// show lot size selection in terms of a sliding scale of fee exposure.
	// Lot sizes can only be powers of 2.
	LotSize uint64    `json:"lotSize"`
	Nonce   uint64    `json:"nonce"`
	Stamp   time.Time `json:"stamp"`
}

func (ord *Order) ID() ID40 {
	var b ID40
	copy(b[:32], ord.From[:])
	binary.BigEndian.PutUint64(b[32:], ord.Nonce)
	return b
}

func (ord *Order) Valid() error {
	// Check whether the lot size is a power of 2, using binary jiu-jitsu.
	if ord.LotSize&(ord.LotSize-1) != 0 {
		return fmt.Errorf("lot size %d is not a power of 2", ord.LotSize)
	}
	if ord.Qty%ord.LotSize != 0 {
		return fmt.Errorf("order quantity %d is not an integer-multiple of the order lot size %d", ord.Qty, ord.LotSize)
	}
	if ord.BaseID == ord.QuoteID {
		return fmt.Errorf("base and quote assets are identical. %d = %d", ord.BaseID, ord.QuoteID)
	}
	if ord.Qty == 0 {
		return errors.New("order quantity is zero")
	}
	if ord.Rate == 0 {
		return errors.New("order rate is zero")
	}
	return nil
}

type ID32 [32]byte

func (i ID32) String() string {
	return hex.EncodeToString(i[:])
}

type ID40 [40]byte

func (i ID40) String() string {
	return hex.EncodeToString(i[:])
}

type Match struct {
	From    PeerID    `json:"from"`
	OrderID ID40      `json:"orderID"`
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
	OrderID ID40 `json:"orderID"`
	MatchID ID32 `json:"matchID"`
}

type MarketParameters struct {
	BaseID  uint32 `json:"baseID"`
	QuoteID uint32 `json:"quoteID"`
}

type OrderUpdate struct {
	From  PeerID    `json:"from"`
	Nonce uint64    `json:"nonce"`
	Qty   uint64    `json:"qty"`
	Stamp time.Time `json:"stamp"`
}

func (ou *OrderUpdate) ID() ID40 {
	var b ID40
	copy(b[:32], ou.From[:])
	binary.BigEndian.PutUint64(b[32:], ou.Nonce)
	return b
}
