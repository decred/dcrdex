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
	// select their own minimum minimum lot size. The user's UI should ignore
	// orderbook orders that don't have the requisite lot size. The UI should
	// show lot size selection in terms of a sliding scale of fee exposure.
	// Lot sizes can only be powers of 2.
	LotSize uint64    `json:"lotSize"`
	Stamp   time.Time `json:"stamp"`
	// Nonce can be used to force unique ids while other values are the same.
	Nonce uint32 `json:"nonce"`
}

func (ord *Order) ID() [32]byte {
	const msgLen = 32 + 4 + 4 + 1 + 8 + 8 + 8 + 8 + 4
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
	binary.BigEndian.PutUint64(b[65:73], uint64(ord.Stamp.UnixMilli()))
	binary.BigEndian.PutUint32(b[73:77], ord.Nonce)
	return blake256.Sum256(b)

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

const (
	OrderExpiration    = time.Hour * 12
	OrderUpdateVersion = 0
)

type OrderUpdate struct {
	*Order
	Version    uint8     `json:"version"`
	Expiration time.Time `json:"expiration"`
	Settled    uint64    `json:"settled"`
	// The signature of all other serialized fields with private key
	// belonging to PeerID.
	Sig []byte `json:"sig"`
}

func NewOrderUpdate(o *Order, settled uint64) *OrderUpdate {
	return &OrderUpdate{
		Version:    OrderUpdateVersion,
		Expiration: o.Stamp.Add(OrderExpiration),
		Settled:    settled,
		Order:      o,
	}
}

// Serialize returns the bytes needed to sign the update.
func (ou *OrderUpdate) Serialize() ([]byte, error) {
	msgLen := 1 + 32 + 4 + 4 + 1 + 8 + 8 + 8 + 8 + 4 + 8 + 8
	b := make([]byte, msgLen)
	b[0] = ou.Version
	copy(b[1:33], ou.From[:])
	binary.BigEndian.PutUint32(b[33:37], ou.BaseID)
	binary.BigEndian.PutUint32(b[37:41], ou.QuoteID)
	if ou.Sell {
		b[42] = 1
	}
	binary.BigEndian.PutUint64(b[42:50], ou.Qty)
	binary.BigEndian.PutUint64(b[50:58], ou.Rate)
	binary.BigEndian.PutUint64(b[58:66], ou.LotSize)
	binary.BigEndian.PutUint64(b[66:74], uint64(ou.Stamp.UnixMilli()))
	binary.BigEndian.PutUint32(b[74:78], ou.Nonce)
	binary.BigEndian.PutUint64(b[78:84], uint64(ou.Expiration.UnixMilli()))
	binary.BigEndian.PutUint64(b[84:92], ou.Settled)

	return b, nil
}
