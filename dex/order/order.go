// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

// Package order defines the Order and Match types used throughout the DEX.
package order

import (
	"database/sql"
	"database/sql/driver"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"time"

	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/server/account"
	"github.com/decred/dcrd/crypto/blake256"
)

// Several types including OrderID and Commitment are defined as a Blake256
// hash. Define an internal hash type and hashSize for convenience.
const hashSize = blake256.Size // 32
type hash = [hashSize]byte

// OrderIDSize defines the length in bytes of an OrderID.
const OrderIDSize = hashSize

// OrderID is the unique identifier for each order. It is defined as the
// Blake256 hash of the serialized order.
type OrderID hash

// String returns a hexadecimal representation of the OrderID. String implements
// fmt.Stringer.
func (oid OrderID) String() string {
	return hex.EncodeToString(oid[:])
}

// Value implements the sql/driver.Valuer interface.
func (oid OrderID) Value() (driver.Value, error) {
	return oid[:], nil // []byte
}

// Scan implements the sql.Scanner interface.
func (oid *OrderID) Scan(src interface{}) error {
	switch src := src.(type) {
	case []byte:
		copy(oid[:], src)
		return nil
		//case string:
		// case nil:
		// 	*oid = nil
		// 	return nil
	}

	return fmt.Errorf("cannot convert %T to OrderID", src)
}

// OrderType distinguishes the different kinds of orders (e.g. limit, market,
// cancel).
type OrderType uint8

// The different OrderType values.
const (
	UnknownOrderType OrderType = iota
	LimitOrderType
	MarketOrderType
	CancelOrderType
)

// Value implements the sql/driver.Valuer interface.
func (ot OrderType) Value() (driver.Value, error) {
	return int64(ot), nil
}

// Scan implements the sql.Scanner interface.
func (ot *OrderType) Scan(src interface{}) error {
	// Use sql.(NullInt32).Scan because it uses the unexported
	// sql.convertAssignRows to coerce compatible types.
	v := new(sql.NullInt32)
	if err := v.Scan(src); err != nil {
		return err
	}
	*ot = OrderType(v.Int32)
	return nil
}

// String returns a string representation of the OrderType.
func (ot OrderType) String() string {
	switch ot {
	case LimitOrderType:
		return "limit"
	case MarketOrderType:
		return "market"
	case CancelOrderType:
		return "cancel"
	default:
		return "unknown"
	}
}

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
	// Prefix returns the order *Prefix.
	Prefix() *Prefix

	// Trade returns the order *Trade if a limit or market order, else nil.
	Trade() *Trade

	// ID computes the Order's ID from its serialization. Serialization is
	// detailed in the 'Client Order Management' section of the DEX
	// specification.
	ID() OrderID

	// UID gives the string representation of the order ID. It is named to
	// reflect the intent of providing a unique identifier.
	UID() string

	// User gives the user's account ID.
	User() account.AccountID

	// Serialize marshals the order. Serialization is detailed in the 'Client
	// Order Management' section of the DEX specification.
	Serialize() []byte

	// Type indicates the Order's type (e.g. LimitOrder, MarketOrder, etc.).
	Type() OrderType

	// Time returns the Order's server time in milliseconds, when it was received by the server.
	Time() int64

	// SetTime sets the ServerTime field of the prefix.
	SetTime(time.Time)

	// Base returns the unique integer identifier of the base asset as defined
	// in the asset package.
	Base() uint32

	// Quote returns the unique integer identifier of the quote asset as defined
	// in the asset package.
	Quote() uint32

	// Commitment returns the order's preimage commitment.
	Commitment() Commitment
}

// zeroTime is the Unix time for a Time where IsZero() == true.
var zeroTime = unixMilli(time.Time{})

// An order's ID is computed as the Blake-256 hash of the serialized order.
func calcOrderID(order Order) OrderID {
	sTime := order.Time()
	if sTime == zeroTime {
		panic("Order's ServerTime is unset")
	}
	return blake256.Sum256(order.Serialize())
}

// CoinID should be used to wrap a []byte so that it may be used as a map key.
type CoinID []byte

func (c CoinID) String() string {
	return hex.EncodeToString(c)
}

// CommitmentSize is the length of the Commitment, a 32-byte Blake-256 hash
// according to the DEX specification.
const CommitmentSize = hashSize

// Commitment is the Blake-256 hash of the Preimage.
type Commitment hash

// Value implements the sql/driver.Valuer interface.
func (c Commitment) Value() (driver.Value, error) {
	return c[:], nil // []byte
}

// Scan implements the sql.Scanner interface.
func (c *Commitment) Scan(src interface{}) error {
	switch src := src.(type) {
	case []byte:
		copy(c[:], src)
		return nil
		//case string:
		// case nil:
		// 	*oid = nil
		// 	return nil
	}

	return fmt.Errorf("cannot convert %T to Commitment", src)
}

// String returns a hexadecimal representation of the Commitment. String
// implements fmt.Stringer.
func (c Commitment) String() string {
	return hex.EncodeToString(c[:])
}

// PreimageSize defines the length of the preimage, which is a 32-byte value
// according to the DEX specification.
const PreimageSize = 32

// Preimage represents the 32-byte preimage as a byte slice.
type Preimage [PreimageSize]byte

// Commit computes the preimage commitment as the Blake-256 hash of the
// Preimage.
func (pi *Preimage) Commit() Commitment {
	return blake256.Sum256(pi[:])
}

// Prefix is the order prefix containing data fields common to all orders.
type Prefix struct {
	AccountID  account.AccountID
	BaseAsset  uint32
	QuoteAsset uint32
	OrderType  OrderType
	ClientTime time.Time
	ServerTime time.Time
	Commit     Commitment

	id  *OrderID // cache of the order's OrderID
	uid string   // cache of the order's UID
}

// P is an alias for Prefix. Embedding with the alias allows us to define a
// method on the interface called Prefix that returns the *Prefix.
type P = Prefix

func (p *Prefix) Prefix() *Prefix {
	return p
}

// PrefixLen is the length in bytes of the serialized order Prefix.
const PrefixLen = account.HashSize + 4 + 4 + 1 + 8 + 8 + CommitmentSize

// serializeSize returns the length of the serialized order Prefix.
func (p *Prefix) serializeSize() int {
	return PrefixLen
}

// Time returns the order prefix's server time as a UNIX epoch time in
// milliseconds.
func (p *Prefix) Time() int64 {
	return unixMilli(p.ServerTime)
}

// SetTime sets the order prefix's server time.
func (p *Prefix) SetTime(t time.Time) {
	p.ServerTime = t.UTC()
	// SetTime should only ever be called once in practice, but in case it is
	// necessary to restamp the ServerTime, clear any computed OrderID.
	p.id, p.uid = nil, ""
}

// User gives the user's account ID.
func (p *Prefix) User() account.AccountID {
	return p.AccountID
}

// Serialize marshals the Prefix into a []byte.
func (p *Prefix) Serialize() []byte {
	b := make([]byte, PrefixLen)

	// account ID
	offset := len(p.AccountID)
	copy(b[:offset], p.AccountID[:])

	// base asset
	binary.BigEndian.PutUint32(b[offset:offset+4], p.BaseAsset)
	offset += 4

	// quote asset
	binary.BigEndian.PutUint32(b[offset:offset+4], p.QuoteAsset)
	offset += 4

	// order type (e.g. market, limit, cancel)
	b[offset] = uint8(p.OrderType)
	offset++

	// client time
	binary.BigEndian.PutUint64(b[offset:offset+8], unixMilliU(p.ClientTime))
	offset += 8

	// server time
	binary.BigEndian.PutUint64(b[offset:offset+8], unixMilliU(p.ServerTime))
	offset += 8

	// commitment
	copy(b[offset:offset+CommitmentSize], p.Commit[:])

	return b
}

// Base returns the base asset integer ID.
func (p *Prefix) Base() uint32 {
	return p.BaseAsset
}

// Quote returns the quote asset integer ID.
func (p *Prefix) Quote() uint32 {
	return p.QuoteAsset
}

// Type returns the order type.
func (p *Prefix) Type() OrderType {
	return p.OrderType
}

// Commitment returns the order Commitment.
func (p *Prefix) Commitment() Commitment {
	return p.Commit
}

// Trade is information about a trade-type order. Both limit and market orders
// are trade-type orders.
type Trade struct {
	Coins    []CoinID
	Sell     bool
	Quantity uint64
	Address  string

	// Filled is not part of the order's serialization.
	Filled uint64
}

// T is an alias for Trade. Embedding with the alias allows us to define a
// method on the interface called Trade that returns the *Trade.
type T = Trade

// Trade returns a pointer to the orders embedded Trade.
func (t *Trade) Trade() *Trade {
	return t
}

// Remaining returns the remaining order amount.
func (t *Trade) Remaining() uint64 {
	return t.Quantity - t.Filled
}

// SwapAddress returns the order's payment address.
func (t *Trade) SwapAddress() string {
	return t.Address
}

// serializeSize returns the length of the serialized Trade.
func (t *Trade) serializeSize() int {
	// Compute the size of the serialized Coin IDs.
	var coinSz int
	for _, coinID := range t.Coins {
		coinSz += len(coinID)
		// TODO: ensure all Coin IDs have the same size, indicating the same asset?
	}
	// The serialized order includes a byte for coin count, but this is implicit
	// in coin slice length.
	return 1 + coinSz + 1 + 8 + len(t.Address)
}

// Serialize marshals the Trade into a []byte.
func (t *Trade) Serialize() []byte {
	b := make([]byte, t.serializeSize())
	offset := 0

	// Coin count
	b[offset] = uint8(len(t.Coins))
	offset++

	// Coins
	for _, coinID := range t.Coins {
		coinSz := len(coinID)
		copy(b[offset:offset+coinSz], coinID)
		offset += coinSz
	}

	// order side
	var side uint8
	if t.Sell {
		side = 1
	}
	b[offset] = side
	offset++

	// order quantity
	binary.BigEndian.PutUint64(b[offset:offset+8], t.Quantity)
	offset += 8

	// client address for received funds
	copy(b[offset:offset+len(t.Address)], []byte(t.Address))
	return b
}

// MarketOrder defines a market order in terms of a Prefix and the order
// details, including the backing Coins, the order direction/side, order
// quantity, and the address where the matched client will send funds. The order
// quantity is in atoms of the base asset, and must be an integral multiple of
// the asset's lot size, except for Market buy orders when it is in units of the
// quote asset and is not bound by integral lot size multiple constraints.
type MarketOrder struct {
	P
	T
}

// ID computes the order ID.
func (o *MarketOrder) ID() OrderID {
	if o.id != nil {
		return *o.id
	}
	id := calcOrderID(o)
	o.id = &id
	return id
}

// UID computes the order ID, returning the string representation.
func (o *MarketOrder) UID() string {
	if o.uid != "" {
		return o.uid
	}
	uid := o.ID().String()
	o.uid = uid
	return uid
}

// String is the same as UID. It is defined to satisfy Stringer.
func (o *MarketOrder) String() string {
	return o.UID()
}

// serializeSize returns the length of the serialized MarketOrder.
func (o *MarketOrder) serializeSize() int {
	// Compute the size of the serialized Coin IDs.
	var coinSz int
	for _, coinID := range o.Coins {
		coinSz += len(coinID)
		// TODO: ensure all Coin IDs have the same size, indicating the same asset?
	}
	// The serialized order includes a byte for coin count, but this is implicit
	// in coin slice length.
	return o.P.serializeSize() + o.T.serializeSize()
}

// Serialize marshals the LimitOrder into a []byte.
func (o *MarketOrder) Serialize() []byte {
	b := make([]byte, o.serializeSize())
	// Prefix and data common with MarketOrder
	offset := o.P.serializeSize()
	copy(b[:offset], o.P.Serialize())
	tradeLen := o.T.serializeSize()
	copy(b[offset:offset+tradeLen], o.T.Serialize())
	return b
}

// Ensure MarketOrder is an Order.
var _ Order = (*MarketOrder)(nil)

// LimitOrder defines a limit order in terms of a MarketOrder and limit-specific
// data including rate (price) and time in force.
type LimitOrder struct {
	P
	T
	Rate  uint64 // price as atoms of quote asset, applied per 1e8 units of the base asset
	Force TimeInForce
}

// ID computes the order ID.
func (o *LimitOrder) ID() OrderID {
	if o.id != nil {
		return *o.id
	}
	id := calcOrderID(o)
	o.id = &id
	return id
}

// UID computes the order ID, returning the string representation.
func (o *LimitOrder) UID() string {
	if o.uid != "" {
		return o.uid
	}
	uid := o.ID().String()
	o.uid = uid
	return uid
}

// String is the same as UID. It is defined to satisfy Stringer.
func (o *LimitOrder) String() string {
	return o.UID()
}

// serializeSize returns the length of the serialized LimitOrder.
func (o *LimitOrder) serializeSize() int {
	return o.P.serializeSize() + o.T.serializeSize() + 8 + 1
}

// Serialize marshals the LimitOrder into a []byte.
func (o *LimitOrder) Serialize() []byte {
	b := make([]byte, o.serializeSize())
	// Prefix and data common with MarketOrder
	offset := o.P.serializeSize()
	copy(b[:offset], o.P.Serialize())
	tradeLen := o.T.serializeSize()
	copy(b[offset:offset+tradeLen], o.T.Serialize())
	offset += tradeLen

	// Price rate in atoms of quote asset
	binary.BigEndian.PutUint64(b[offset:offset+8], o.Rate)
	offset += 8

	// Time in force
	b[offset] = uint8(o.Force)
	return b
}

// Ensure LimitOrder is an Order.
var _ Order = (*LimitOrder)(nil)

// Price returns the limit order's price rate.
func (o *LimitOrder) Price() uint64 {
	return o.Rate
}

// CancelOrder defines a cancel order in terms of an order Prefix and the ID of
// the order to be canceled.
type CancelOrder struct {
	P
	TargetOrderID OrderID
}

// ID computes the order ID.
func (o *CancelOrder) ID() OrderID {
	if o.id != nil {
		return *o.id
	}
	id := calcOrderID(o)
	o.id = &id
	return id
}

// UID computes the order ID, returning the string representation.
func (o *CancelOrder) UID() string {
	if o.uid != "" {
		return o.uid
	}
	uid := o.ID().String()
	o.uid = uid
	return uid
}

// Trade returns a pointer to the orders embedded Trade.
func (o *CancelOrder) Trade() *Trade {
	return nil
}

// String is the same as UID. It is defined to satisfy Stringer.
func (o *CancelOrder) String() string {
	return o.UID()
}

// serializeSize returns the length of the serialized CancelOrder.
func (o *CancelOrder) serializeSize() int {
	return o.P.serializeSize() + OrderIDSize
}

// Serialize marshals the CancelOrder into a []byte.
func (o *CancelOrder) Serialize() []byte {
	return append(o.P.Serialize(), o.TargetOrderID[:]...)
}

// Ensure CancelOrder is an Order.
var _ Order = (*CancelOrder)(nil)

// Some commonly used time transformations.
var unixMilli = encode.UnixMilli
var unixMilliU = encode.UnixMilliU
var unixTimeMilli = encode.UnixTimeMilli
