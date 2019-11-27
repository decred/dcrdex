package test

import (
	"encoding/binary"
	"time"

	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/server/account"
)

const (
	baseClientTime = 1566497653
	baseServerTime = 1566497656
)

var (
	acctTemplate = account.AccountID{
		0x22, 0x4c, 0xba, 0xaa, 0xfa, 0x80, 0xbf, 0x3b, 0xd1, 0xff, 0x73, 0x15,
		0x90, 0xbc, 0xbd, 0xda, 0x5a, 0x76, 0xf9, 0x1e, 0x60, 0xa1, 0x56, 0x99,
		0x46, 0x34, 0xe9, 0x1c, 0xaa, 0xaa, 0xaa, 0xaa,
	}
	acctCounter uint32
)

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

// Writer represents a client that places orders on one side of a market.
type Writer struct {
	Addr   string
	Acct   account.AccountID
	Sell   bool
	Market *Market
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
		MarketOrder: order.MarketOrder{
			Prefix: order.Prefix{
				AccountID:  writer.Acct,
				BaseAsset:  writer.Market.Base,
				QuoteAsset: writer.Market.Quote,
				OrderType:  order.LimitOrderType,
				ClientTime: time.Unix(baseClientTime+timeOffset, 0),
				ServerTime: time.Unix(baseServerTime+timeOffset, 0),
			},
			Coins:    []order.CoinID{},
			Sell:     writer.Sell,
			Quantity: lots * writer.Market.LotSize,
			Address:  writer.Addr,
		},
		Rate:  rate,
		Force: force,
	}
}

// WriteMarketOrder creates a market order with the specified writer and
// quantity.
func WriteMarketOrder(writer *Writer, lots uint64, timeOffset int64) *order.MarketOrder {
	if writer.Sell {
		lots *= writer.Market.LotSize
	}
	return &order.MarketOrder{
		Prefix: order.Prefix{
			AccountID:  writer.Acct,
			BaseAsset:  writer.Market.Base,
			QuoteAsset: writer.Market.Quote,
			OrderType:  order.MarketOrderType,
			ClientTime: time.Unix(baseClientTime+timeOffset, 0).UTC(),
			ServerTime: time.Unix(baseServerTime+timeOffset, 0).UTC(),
		},
		Coins:    []order.CoinID{},
		Sell:     writer.Sell,
		Quantity: lots,
		Address:  writer.Addr,
	}
}

// WriteCancelOrder creates a cancel order with the specified order ID.
func WriteCancelOrder(writer *Writer, targetOrderID order.OrderID, timeOffset int64) *order.CancelOrder {
	return &order.CancelOrder{
		Prefix: order.Prefix{
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
