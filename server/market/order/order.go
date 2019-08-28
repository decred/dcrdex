// Copyright (c) 2019, The Decred developers
// See LICENSE for details.

// Package order defines the Order and Match types used throughout the DEX.
package order

import (
	"encoding/binary"
	"encoding/hex"
	"math"
	"time"

	"github.com/decred/dcrd/crypto/blake256"
	"github.com/decred/dcrdex/server/account"
)

// OrderIDSize defines the length in bytes of an OrderID.
const OrderIDSize = blake256.Size

// OrderID is the unique identifier for each order.
type OrderID [OrderIDSize]byte

// String returns a hexadecimal representation of the OrderID. String implements
// fmt.Stringer.
func (oid OrderID) String() string {
	return hex.EncodeToString(oid[:])
}

// OrderType distinguishes the different kinds of orders (e.g. limit, market,
// cancel).
type OrderType uint8

// The different OrderType values.
const (
	LimitOrderType OrderType = iota
	MarketOrderType
	CancelOrderType
)

// TimeInForce indicates how limit order execution is to be handled. That is,
// when the order is not immediately matched during processing of the order's
// epoch, the order may become a standing order or be revoked without a fill.
type TimeInForce uint8

// The TimeInForce is either ImmediateTiF, which prevents the order from
// becoming a standing order if there is no match during epoch processing, or
// StandingTiF, which allows limit orders to enter the order book if not
// immediately matched during epoch processing.
const (
	ImmediateTiF TimeInForce = iota
	StandingTiF
)

// Order specifies the methods required for a type to function as a DEX order.
// See the concrete implementations of MarketOrder, LimitOrder, and CancelOrder.
type Order interface {
	// ID computes the Order's ID from its serialization as per the spec.
	ID() OrderID

	// UID gives the string representation of the order ID. It is named to
	// reflect the intent of providing a unique identifier.
	UID() string

	// Serialize marshals the order as per the spec.
	Serialize() []byte

	// SerializeSize gives the length of the serialized order in bytes.
	SerializeSize() int

	// Type indicates the Order's type (e.g. LimitOrder, MarketOrder, etc.).
	Type() OrderType

	// Time returns the Order's server time, when it was received by the server.
	Time() int64

	// Remaining computes the unfilled amount of the order.
	Remaining() uint64
}

// An order's ID is computed as the Blake-256 hash of the serialized order.
func calcOrderID(order Order) OrderID {
	return blake256.Sum256(order.Serialize())
}

// UTXO is the interface required to be satisfied by any asset's implementation
// of a UTXO type.
type UTXO interface {
	TxHash() string
	Vout() uint32
	Serialize() []byte
	SerializeSize() int
}

// Prefix is the order prefix containing data fields common to all orders.
type Prefix struct {
	AccountID  account.AccountID
	BaseAsset  uint32 // unused for CancelOrder?
	QuoteAsset uint32 // unused for CancelOrder
	OrderType  OrderType
	ClientTime time.Time
	ServerTime time.Time
}

// PrefixLen is the length in bytes of the serialized order Prefix.
const PrefixLen = account.HashSize + 4 + 4 + 1 + 8 + 8

// SerializeSize returns the length of the serialized order Prefix.
func (p *Prefix) SerializeSize() int {
	return PrefixLen
}

// Serialize marshals the Prefix into a []byte.
// TODO: Deserialize.
func (p *Prefix) Serialize() []byte {
	b := make([]byte, PrefixLen)

	// account ID
	offset := len(p.AccountID)
	copy(b[:offset], p.AccountID[:])

	// base asset
	binary.LittleEndian.PutUint32(b[offset:offset+4], p.BaseAsset)
	offset += 4

	// quote asset
	binary.LittleEndian.PutUint32(b[offset:offset+4], p.QuoteAsset)
	offset += 4

	// order type (e.g. market, limit, cancel)
	b[offset] = uint8(p.OrderType)
	offset++

	// client time
	binary.LittleEndian.PutUint64(b[offset:offset+8], uint64(p.ClientTime.Unix()))
	offset += 8

	// server time
	binary.LittleEndian.PutUint64(b[offset:offset+8], uint64(p.ServerTime.Unix()))
	return b
}

// MarketOrder defines a market order in terms of a Prefix and the order
// details, including the backing UTXOs, the order direction/side, order
// quantity, and the address where the matched client will send funds. The order
// quantity is in atoms of the base asset, and must be an integral multiple of
// the asset's lot size, except for Market buy orders when it is in units of the
// quote asset and is not bound by integral lot size multiple constraints.
type MarketOrder struct {
	Prefix
	UTXOs    []UTXO
	Sell     bool
	Quantity uint64
	Address  string

	// Filled is not part of the order's serialization.
	Filled uint64
}

// ID computes the order ID.
func (o *MarketOrder) ID() OrderID {
	return calcOrderID(o)
}

// UID computes the order ID, returning the string representation.
func (o *MarketOrder) UID() string {
	return o.ID().String()
}

// SerializeSize returns the length of the serialized MarketOrder.
func (o *MarketOrder) SerializeSize() int {
	// Compute the size of the serialized UTXOs.
	var utxosSize int
	for _, u := range o.UTXOs {
		utxosSize += u.SerializeSize()
		// TODO: ensure all UTXOs have the same size, indicating the same asset?
	}
	// The serialized order includes a byte for UTXO count, but this is implicit
	// in UTXO slice length.
	return o.Prefix.SerializeSize() + 1 + utxosSize + 1 + 8 + len(o.Address)
}

// Serialize marshals the MarketOrder into a []byte.
func (o *MarketOrder) Serialize() []byte {
	b := make([]byte, o.SerializeSize())

	// Prefix
	copy(b[:PrefixLen], o.Prefix.Serialize())
	offset := PrefixLen

	// UTXO count
	b[offset] = uint8(len(o.UTXOs))
	offset++

	// UTXO data
	for _, u := range o.UTXOs {
		utxoSize := u.SerializeSize()
		copy(b[offset:offset+utxoSize], u.Serialize())
		offset += utxoSize
	}

	// order side
	var side uint8
	if o.Sell {
		side = 1
	}
	b[offset] = side
	offset++

	// order quantity
	binary.LittleEndian.PutUint64(b[offset:offset+8], o.Quantity)
	offset += 8

	// client address for received funds
	copy(b[offset:offset+len(o.Address)], []byte(o.Address))
	return b
}

// Type returns MarketOrderType for a MarketOrder.
func (o *MarketOrder) Type() OrderType {
	return MarketOrderType
}

// Remaining returns the remaining order amount.
func (o *MarketOrder) Remaining() uint64 {
	return o.Quantity - o.Filled
}

// Ensure MarketOrder is an Order.
var _ Order = (*MarketOrder)(nil)

// LimitOrder defines a limit order in terms of a MarketOrder and limit-specific
// data including rate (price) and time in force.
type LimitOrder struct {
	MarketOrder         // order type in the prefix is the only difference
	Rate        float64 // price as quote asset per base asset
	Force       TimeInForce
}

// ID computes the order ID.
func (o *LimitOrder) ID() OrderID {
	return calcOrderID(o)
}

// UID computes the order ID, returning the string representation.
func (o *LimitOrder) UID() string {
	return o.ID().String()
}

// SerializeSize returns the length of the serialized LimitOrder.
func (o *LimitOrder) SerializeSize() int {
	return o.MarketOrder.SerializeSize() + 8 + 1
}

// Serialize marshals the LimitOrder into a []byte.
func (o *LimitOrder) Serialize() []byte {
	b := make([]byte, o.SerializeSize())
	// Prefix and data common with MarketOrder
	offset := o.MarketOrder.SerializeSize()
	copy(b[:offset], o.MarketOrder.Serialize())

	// Price rate
	// var fb bytes.Buffer
	// _ = binary.Write(&fb, binary.LittleEndian, o.Rate)
	// copy(b[mSz:], fb.Bytes())
	binary.LittleEndian.PutUint64(b[offset:offset+8], math.Float64bits(o.Rate))
	// Price rate in atoms of quote asset
	// binary.LittleEndian.PutUint64(b[offset:offset+8], o.Rate)
	offset += 8

	// Time in force
	b[offset] = uint8(o.Force)
	return b
}

// Type returns LimitOrderType for a LimitOrder.
func (o *LimitOrder) Type() OrderType {
	return LimitOrderType
}

// Ensure LimitOrder is an Order.
var _ Order = (*LimitOrder)(nil)

// CancelOrder defines a cancel order in terms of an order Prefix and the ID of
// the order to be canceled.
type CancelOrder struct {
	Prefix
	TargetOrderID OrderID
}

// ID computes the order ID.
func (o *CancelOrder) ID() OrderID {
	return calcOrderID(o)
}

// UID computes the order ID, returning the string representation.
func (o *CancelOrder) UID() string {
	return o.ID().String()
}

// SerializeSize returns the length of the serialized CancelOrder.
func (o *CancelOrder) SerializeSize() int {
	return o.Prefix.SerializeSize() + OrderIDSize
}

// Serialize marshals the CancelOrder into a []byte.
func (o *CancelOrder) Serialize() []byte {
	return append(o.Prefix.Serialize(), o.TargetOrderID[:]...)
}

// Type returns CancelOrderType for a CancelOrder.
func (o *CancelOrder) Type() OrderType {
	return CancelOrderType
}

// Remaining always returns 0 for a CancelOrder.
func (o *CancelOrder) Remaining() uint64 {
	return 0
}

// Ensure LimitOrder is an Order.
var _ Order = (*CancelOrder)(nil)
