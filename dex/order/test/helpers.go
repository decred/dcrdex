package test

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math/rand"
	"time"

	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/server/account"
)

const (
	baseClientTime = 1566497653
	baseServerTime = 1566497656
	b58Set         = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"
	addressLength  = 34
)

var (
	acctTemplate = account.AccountID{
		0x22, 0x4c, 0xba, 0xaa, 0xfa, 0x80, 0xbf, 0x3b, 0xd1, 0xff, 0x73, 0x15,
		0x90, 0xbc, 0xbd, 0xda, 0x5a, 0x76, 0xf9, 0x1e, 0x60, 0xa1, 0x56, 0x99,
		0x46, 0x34, 0xe9, 0x1c, 0xaa, 0xaa, 0xaa, 0xaa,
	}
	acctCounter uint32
)

func randBytes(l int) []byte {
	b := make([]byte, l)
	rand.Read(b)
	return b
}

func randUint32() uint32 { return uint32(rand.Int31()) }
func randUint64() uint64 { return uint64(rand.Int63()) }

func randBool() bool {
	return rand.Intn(2) == 1
}

// A random base-58 string.
func RandomAddress() string {
	b := make([]byte, addressLength)
	for i := range b {
		b[i] = b58Set[rand.Intn(58)]
	}
	return string(b)
}

// NextAccount gets a unique account ID.
func NextAccount() account.AccountID {
	acctCounter++
	intBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(intBytes, acctCounter)
	acctID := account.AccountID{}
	copy(acctID[:], acctTemplate[:])
	copy(acctID[account.HashSize-4:], intBytes)
	return acctID
}

// RandomOrderID creates a random order ID.
func RandomOrderID() order.OrderID {
	var oid order.OrderID
	copy(oid[:], randBytes(order.OrderIDSize))
	return oid
}

// RandomMatchID creates a random match ID.
func RandomMatchID() order.MatchID {
	var mid order.MatchID
	copy(mid[:], randBytes(order.MatchIDSize))
	return mid
}

// Writer represents a client that places orders on one side of a market.
type Writer struct {
	Addr   string
	Acct   account.AccountID
	Sell   bool
	Market *Market
}

// RandomWriter creates a random Writer.
func RandomWriter() *Writer {
	return &Writer{
		Addr: RandomAddress(),
		Acct: NextAccount(),
		Sell: randBool(),
		Market: &Market{
			Base:    randUint32(),
			Quote:   randUint32(),
			LotSize: randUint64(),
		},
	}
}

// Market is a exchange market.
type Market struct {
	Base    uint32
	Quote   uint32
	LotSize uint64
}

// WriteLimitOrder creates a limit order with the specified writer and order
// values.
func WriteLimitOrder(writer *Writer, rate, lots uint64, force order.TimeInForce, timeOffset int64) *order.LimitOrder {
	return &order.LimitOrder{
		P: order.Prefix{
			AccountID:  writer.Acct,
			BaseAsset:  writer.Market.Base,
			QuoteAsset: writer.Market.Quote,
			OrderType:  order.LimitOrderType,
			ClientTime: time.Unix(baseClientTime+timeOffset, 0),
			ServerTime: time.Unix(baseServerTime+timeOffset, 0),
		},
		T: order.Trade{
			Coins:    []order.CoinID{},
			Sell:     writer.Sell,
			Quantity: lots * writer.Market.LotSize,
			Address:  writer.Addr,
		},
		Rate:  rate,
		Force: force,
	}
}

// RandomLimitOrder creates a random limit order with a random writer.
func RandomLimitOrder() *order.LimitOrder {
	return WriteLimitOrder(RandomWriter(), randUint64(), randUint64(), order.TimeInForce(rand.Intn(2)), 0)
}

// WriteMarketOrder creates a market order with the specified writer and
// quantity.
func WriteMarketOrder(writer *Writer, lots uint64, timeOffset int64) *order.MarketOrder {
	if writer.Sell {
		lots *= writer.Market.LotSize
	}
	return &order.MarketOrder{
		P: order.Prefix{
			AccountID:  writer.Acct,
			BaseAsset:  writer.Market.Base,
			QuoteAsset: writer.Market.Quote,
			OrderType:  order.MarketOrderType,
			ClientTime: time.Unix(baseClientTime+timeOffset, 0).UTC(),
			ServerTime: time.Unix(baseServerTime+timeOffset, 0).UTC(),
		},
		T: order.Trade{
			Coins:    []order.CoinID{},
			Sell:     writer.Sell,
			Quantity: lots,
			Address:  writer.Addr,
		},
	}
}

// RandomMarketOrder creates a random market order with a random writer.
func RandomMarketOrder() *order.MarketOrder {
	return WriteMarketOrder(RandomWriter(), randUint64(), 0)
}

// WriteCancelOrder creates a cancel order with the specified order ID.
func WriteCancelOrder(writer *Writer, targetOrderID order.OrderID, timeOffset int64) *order.CancelOrder {
	return &order.CancelOrder{
		P: order.Prefix{
			AccountID:  writer.Acct,
			BaseAsset:  writer.Market.Base,
			QuoteAsset: writer.Market.Quote,
			OrderType:  order.CancelOrderType,
			ClientTime: time.Unix(baseClientTime+timeOffset, 0).UTC(),
			ServerTime: time.Unix(baseServerTime+timeOffset, 0).UTC(),
		},
		TargetOrderID: targetOrderID,
	}
}

// RandomCancelOrder creates a random cancel order with a random writer.
func RandomCancelOrder() *order.CancelOrder {
	return WriteCancelOrder(RandomWriter(), RandomOrderID(), 0)
}

// ComparePrefix compares the prefixes field-by-field and returns an error if a
// mismatch is found.
func ComparePrefix(p1, p2 *order.Prefix) error {
	if !bytes.Equal(p1.AccountID[:], p2.AccountID[:]) {
		return fmt.Errorf("account ID mismatch. %x != %x", p1.AccountID[:], p2.AccountID[:])
	}
	if p1.BaseAsset != p2.BaseAsset {
		return fmt.Errorf("base asset mismatch. %d != %d", p1.BaseAsset, p2.BaseAsset)
	}
	if p1.QuoteAsset != p2.QuoteAsset {
		return fmt.Errorf("quote asset mismatch. %d != %d", p1.QuoteAsset, p2.QuoteAsset)
	}
	if p1.OrderType != p2.OrderType {
		return fmt.Errorf("order type mismatch. %d != %d", p1.OrderType, p2.OrderType)
	}
	if !p1.ClientTime.Equal(p2.ClientTime) {
		return fmt.Errorf("client time mismatch. %s != %s", p1.ClientTime, p2.ClientTime)
	}
	if !p1.ServerTime.Equal(p2.ServerTime) {
		return fmt.Errorf("server time mismatch. %s != %s", p1.ServerTime, p2.ServerTime)
	}
	return nil
}

// CompareTrade compares the MarketOrders field-by-field and returns an error if
// a mismatch is found.
func CompareTrade(t1, t2 *order.MarketOrder) error {
	if len(t1.Coins) != len(t2.Coins) {
		return fmt.Errorf("coin length mismatch. %d != %d", len(t1.Coins), len(t2.Coins))
	}
	for i, coin := range t2.Coins {
		reCoin := t1.Coins[i]
		if !bytes.Equal(reCoin, coin) {
			return fmt.Errorf("coins %d not equal. %x != %x", i, coin, t1)
		}
	}
	if t1.Sell != t2.Sell {
		return fmt.Errorf("sell mismatch. %t != %t", t1.Sell, t2.Sell)
	}
	if t1.Quantity != t2.Quantity {
		return fmt.Errorf("quantity mismatch. %d != %d", t1.Quantity, t2.Quantity)
	}
	if t1.Address != t2.Address {
		return fmt.Errorf("address mismatch. %s != %s", t1.Address, t2.Address)
	}
	return nil
}

// RandomUserMatch creates a random UserMatch.
func RandomUserMatch() *order.UserMatch {
	return &order.UserMatch{
		OrderID:  RandomOrderID(),
		MatchID:  RandomMatchID(),
		Quantity: randUint64(),
		Rate:     randUint64(),
		Address:  RandomAddress(),
		Time:     randUint64(),
		Status:   order.MatchStatus(rand.Intn(5)),
		Side:     order.MatchSide(rand.Intn(2)),
	}
}

// CompareUserMatch compares the UserMatches field-by-field and returns an error
// if a mismatch is found.
func CompareUserMatch(m1, m2 *order.UserMatch) error {
	if !bytes.Equal(m1.OrderID[:], m2.OrderID[:]) {
		return fmt.Errorf("OrderID mismatch. %s != %s", m1.OrderID, m2.OrderID)
	}
	if !bytes.Equal(m1.MatchID[:], m2.MatchID[:]) {
		return fmt.Errorf("MatchID mismatch. %s != %s", m1.MatchID, m2.MatchID)
	}
	if m1.Quantity != m2.Quantity {
		return fmt.Errorf("Quantity mismatch. %d != %d", m1.Quantity, m2.Quantity)
	}
	if m1.Rate != m2.Rate {
		return fmt.Errorf("Rate mismatch. %d != %d", m1.Rate, m2.Rate)
	}
	if m1.Address != m2.Address {
		return fmt.Errorf("Address mismatch. %s != %s", m1.Address, m2.Address)
	}
	if m1.Time != m2.Time {
		return fmt.Errorf("Time mismatch. %d != %d", m1.Time, m2.Time)
	}
	if m1.Status != m2.Status {
		return fmt.Errorf("Status mismatch. %d != %d", m1.Status, m2.Status)
	}
	if m1.Side != m2.Side {
		return fmt.Errorf("Side mismatch. %d != %d", m1.Side, m2.Side)
	}
	return nil
}

type testKiller interface {
	Fatalf(string, ...interface{})
}

// MustComparePrefix compares the Prefix field-by-field and calls the Fatalf
// method on the supplied testKiller if a mismatch is encountered.
func MustComparePrefix(t testKiller, p1, p2 *order.Prefix) {
	err := ComparePrefix(p1, p2)
	if err != nil {
		t.Fatalf(err.Error())
	}
}

// MustCompareTrade compares the MarketOrders field-by-field and calls the
// Fatalf method on the supplied testKiller if a mismatch is encountered.
func MustCompareTrade(t testKiller, t1, t2 *order.MarketOrder) {
	err := CompareTrade(t1, t2)
	if err != nil {
		t.Fatalf(err.Error())
	}
}

// MustCompareUserMatch compares the UserMatches field-by-field and calls the
// Fatalf method on the supplied testKiller if a mismatch is encountered.
func MustCompareUserMatch(t testKiller, m1, m2 *order.UserMatch) {
	err := CompareUserMatch(m1, m2)
	if err != nil {
		t.Fatalf(err.Error())
	}
}

// MustCompareUserMatch compares the LimitOrders field-by-field and calls the
// Fatalf method on the supplied testKiller if a mismatch is encountered.
func MustCompareLimitOrders(t testKiller, l1, l2 *order.LimitOrder) {
	MustComparePrefix(t, &l1.Prefix, &l2.Prefix)
	MustCompareTrade(t, &l1.MarketOrder, &l2.MarketOrder)
	if l1.Rate != l2.Rate {
		t.Fatalf("rate mismatch. %d != %d", l1.Rate, l2.Rate)
	}
	if l1.Force != l2.Force {
		t.Fatalf("time-in-force mismatch. %d != %d", l1.Force, l2.Force)
	}
}

// MustCompareMarketOrders compares the MarketOrders field-by-field and calls
// the Fatalf method on the supplied testKiller if a mismatch is encountered.
func MustCompareMarketOrders(t testKiller, m1, m2 *order.MarketOrder) {
	MustComparePrefix(t, &m1.Prefix, &m2.Prefix)
	MustCompareTrade(t, m1, m2)
}

// MustCompareCancelOrders compares the CancelOrders field-by-field and calls
// the Fatalf method on the supplied testKiller if a mismatch is encountered.
func MustCompareCancelOrders(t testKiller, c1, c2 *order.CancelOrder) {
	MustComparePrefix(t, &c1.Prefix, &c2.Prefix)
	if !bytes.Equal(c1.TargetOrderID[:], c2.TargetOrderID[:]) {
		t.Fatalf("wrong target order ID. wanted %s, got %s", c1.TargetOrderID, c2.TargetOrderID)
	}
}

// MustCompareOrders compares the Orders field-by-field and calls
// the Fatalf method on the supplied testKiller if a mismatch is encountered.
func MustCompareOrders(t testKiller, o1, o2 order.Order) {
	switch ord1 := o1.(type) {
	case *order.LimitOrder:
		ord2, ok := o2.(*order.LimitOrder)
		if !ok {
			t.Fatalf("first order was a limit order, but second order was not")
		}
		MustCompareLimitOrders(t, ord1, ord2)
	case *order.MarketOrder:
		ord2, ok := o2.(*order.MarketOrder)
		if !ok {
			t.Fatalf("first order was a market order, but second order was not")
		}
		MustCompareMarketOrders(t, ord1, ord2)
	case *order.CancelOrder:
		ord2, ok := o2.(*order.CancelOrder)
		if !ok {
			t.Fatalf("first order was a cancel order, but second order was not")
		}
		MustCompareCancelOrders(t, ord1, ord2)
	default:
		t.Fatalf("Unknown order type")
	}

}
