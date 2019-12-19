// Copyright (c) 2019, The Decred developers
// See LICENSE for details.

// Package order defines the Order and Match types used throughout the DEX.
package order

import (
	"bytes"
	"fmt"

	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/server/account"
)

var uint32B = encode.Uint32Bytes
var uint64B = encode.Uint64Bytes
var intCoder = encode.IntCoder
var bEqual = bytes.Equal

// EncodePrefix encodes the order Prefix to a versioned blob.
func EncodePrefix(p *Prefix) []byte {
	return encode.BuildyBytes{0}.
		AddData(p.AccountID[:]).
		AddData(uint32B(p.BaseAsset)).
		AddData(uint32B(p.QuoteAsset)).
		AddData([]byte{byte(p.OrderType)}).
		AddData(uint64B(unixMilliU(p.ClientTime))).
		AddData(uint64B(unixMilliU(p.ServerTime)))
}

// DecodePrefix decodes the versioned blob to a *Prefix.
func DecodePrefix(b []byte) (prefix *Prefix, err error) {
	ver, pushes, err := encode.DecodeBlob(b)
	if err != nil {
		return nil, err
	}
	switch ver {
	case 0:
		return decodePrefix_v0(pushes)
	}
	return nil, fmt.Errorf("unknown Prefix version %d", ver)
}

// decodePrefix_v0 decodes the v0 payload into a *Prefix.
func decodePrefix_v0(pushes [][]byte) (prefix *Prefix, err error) {
	if len(pushes) != 6 {
		return nil, fmt.Errorf("expected 6 prefix parts, got %d", len(pushes))
	}
	acctB, baseB, quoteB := pushes[0], pushes[1], pushes[2]
	oTypeB, cTimeB, sTimeB := pushes[3], pushes[4], pushes[5]
	if len(acctB) != account.HashSize {
		return nil, fmt.Errorf("expected account ID length %d, got %d", account.HashSize, len(acctB))
	}
	var acctID account.AccountID
	copy(acctID[:], acctB)
	return &Prefix{
		AccountID:  acctID,
		BaseAsset:  intCoder.Uint32(baseB),
		QuoteAsset: intCoder.Uint32(quoteB),
		OrderType:  OrderType(oTypeB[0]),
		ClientTime: encode.DecodeUTime(cTimeB),
		ServerTime: encode.DecodeUTime(sTimeB),
	}, nil
}

// EncodeTrade encodes the MarketOrder information, but not the Prefix
// structure.
func EncodeTrade(ord *Trade) []byte {
	sell := encode.ByteFalse
	if ord.Sell {
		sell = encode.ByteTrue
	}
	coins := encode.BuildyBytes{}
	for _, coin := range ord.Coins {
		coins = coins.AddData(coin)
	}
	return encode.BuildyBytes{0}.
		AddData(coins).
		AddData(sell).
		AddData(uint64B(ord.Quantity)).
		AddData([]byte(ord.Address)).
		AddData(uint64B(ord.Filled))
}

// DecodeTrade decodes the versioned-blob market order, but does not populate
// the embedded Prefix.
func DecodeTrade(b []byte) (trade *Trade, err error) {
	ver, pushes, err := encode.DecodeBlob(b)
	if err != nil {
		return nil, err
	}
	switch ver {
	case 0:
		return decodeTrade_v0(pushes)
	}
	return nil, fmt.Errorf("unknown MarketOrder version %d", ver)
}

// decodeTrade_v0 decodes the version 0 payload into a MarketOrder, but
// does not populate the embedded Prefix.
func decodeTrade_v0(pushes [][]byte) (mrkt *Trade, err error) {
	if len(pushes) != 5 {
		return nil, fmt.Errorf("expected 5 pushes, got %d", len(pushes))
	}
	coinsB, sellB, qtyB, addrB, filledB := pushes[0], pushes[1], pushes[2], pushes[3], pushes[4]
	rawCoins, err := encode.ExtractPushes(coinsB)
	if err != nil {
		return nil, fmt.Errorf("error extracting coins: %v", err)
	}
	coins := make([]CoinID, 0, len(rawCoins))
	for _, coin := range rawCoins {
		coins = append(coins, coin)
	}
	sell := true
	if bEqual(sellB, encode.ByteFalse) {
		sell = false
	}
	return &Trade{
		Coins:    coins,
		Sell:     sell,
		Quantity: intCoder.Uint64(qtyB),
		Address:  string(addrB),
		Filled:   intCoder.Uint64(filledB),
	}, nil
}

// EncodeMatch encodes the UserMatch to bytes suitable for binary storage or
// communications.
func EncodeMatch(match *UserMatch) []byte {
	return encode.BuildyBytes{0}.
		AddData(match.OrderID[:]).
		AddData(match.MatchID[:]).
		AddData(uint64B(match.Quantity)).
		AddData(uint64B(match.Rate)).
		AddData([]byte(match.Address)).
		AddData(uint64B(match.Time)).
		AddData([]byte{byte(match.Status)}).
		AddData([]byte{byte(match.Side)})
}

// DecodeMatch decodes the versioned blob into a UserMatch.
func DecodeMatch(b []byte) (match *UserMatch, err error) {
	ver, pushes, err := encode.DecodeBlob(b)
	if err != nil {
		return nil, err
	}
	switch ver {
	case 0:
		return matchDecoder_v0(pushes)
	}
	return nil, fmt.Errorf("unknown UserMatch version %d", ver)
}

// matchDecoder_v0 decodes the version 0 payload into a *UserMatch.
func matchDecoder_v0(pushes [][]byte) (*UserMatch, error) {
	if len(pushes) != 8 {
		return nil, fmt.Errorf("matchDecoder_v0: expected 8 pushes, got %d", len(pushes))
	}
	oidB, midB := pushes[0], pushes[1]
	if len(oidB) != OrderIDSize {
		return nil, fmt.Errorf("matchDecoder_v0: expected length %d order ID, got %d", OrderIDSize, len(oidB))
	}
	if len(midB) != MatchIDSize {
		return nil, fmt.Errorf("matchDecoder_v0: expected length %d match ID, got %d", MatchIDSize, len(midB))
	}
	var oid OrderID
	copy(oid[:], oidB)
	var mid MatchID
	copy(mid[:], midB)

	statusB, sideB := pushes[6], pushes[7]
	if len(statusB) != 1 || len(sideB) != 1 {
		return nil, fmt.Errorf("matchDecoder_v0: status/side incorrect length %d/%d", len(statusB), len(sideB))
	}
	return &UserMatch{
		OrderID:  oid,
		MatchID:  mid,
		Quantity: intCoder.Uint64(pushes[2]),
		Rate:     intCoder.Uint64(pushes[3]),
		Address:  string(pushes[4]),
		Time:     intCoder.Uint64(pushes[5]),
		Status:   MatchStatus(statusB[0]),
		Side:     MatchSide(sideB[0]),
	}, nil
}

// Length-1 byte slices used as flags to indicate common order constants.
var (
	orderTypeLimit    = []byte{'l'}
	orderTypeMarket   = []byte{'m'}
	orderTypeCancel   = []byte{'c'}
	orderTifImmediate = []byte{'i'}
	orderTifStanding  = []byte{'s'}
)

// EncodeOrder encodes the order to bytes suitable for wire communications or
// database storage. EncodeOrder accepts any type of Order.
func EncodeOrder(ord Order) []byte {
	switch o := ord.(type) {
	case *LimitOrder:
		tif := orderTifStanding
		if o.Force == ImmediateTiF {
			tif = orderTifImmediate
		}
		return encode.BuildyBytes{0}.
			AddData(orderTypeLimit).
			AddData(EncodePrefix(&o.P)).
			AddData(EncodeTrade(&o.T)).
			AddData(encode.BuildyBytes{}.
				AddData(uint64B(o.Rate)).
				AddData(tif),
			)
	case *MarketOrder:
		return encode.BuildyBytes{0}.
			AddData(orderTypeMarket).
			AddData(EncodePrefix(&o.P)).
			AddData(EncodeTrade(&o.T))
	case *CancelOrder:
		return encode.BuildyBytes{0}.
			AddData(orderTypeCancel).
			AddData(EncodePrefix(&o.P)).
			AddData(o.TargetOrderID[:])
	default:
		panic("encodeOrder: unknown order type")
	}
}

// DecodeOrder decodes the byte-encoded order. DecodeOrder accepts any type of
// order.
func DecodeOrder(b []byte) (ord Order, err error) {
	ver, pushes, err := encode.DecodeBlob(b)
	if err != nil {
		return nil, err
	}
	switch ver {
	case 0:
		return decodeOrder_v0(pushes)
	}
	return nil, fmt.Errorf("unknown Order version %d", ver)
}

// decodeOrder_v0 decodes the version 0 payload into an Order.
func decodeOrder_v0(pushes [][]byte) (Order, error) {
	if len(pushes) == 0 {
		return nil, fmt.Errorf("decodeOrder_v0: zero pushes for order")
	}
	// The first push should be the order type.
	oType := pushes[0]
	pushes = pushes[1:]
	switch {
	case bEqual(oType, orderTypeLimit):
		if len(pushes) != 3 {
			return nil, fmt.Errorf("decodeOrder_v0: expected 3 pushes for limit order, got %d", len(pushes))
		}
		prefixB, tradeB, limitFlagsB := pushes[0], pushes[1], pushes[2]
		prefix, err := DecodePrefix(prefixB)
		if err != nil {
			return nil, err
		}
		trade, err := DecodeTrade(tradeB)
		if err != nil {
			return nil, err
		}
		flags, err := encode.ExtractPushes(limitFlagsB)
		if err != nil {
			return nil, fmt.Errorf("decodeOrder_v0: error extracting limit flags: %d", err)
		}
		if len(flags) != 2 {
			return nil, fmt.Errorf("decodeOrder_v0: expected 2 error flags, got %d", len(flags))
		}
		rateB, tifB := flags[0], flags[1]
		tif := ImmediateTiF
		if bEqual(tifB, orderTifStanding) {
			tif = StandingTiF
		}
		return &LimitOrder{
			P:     *prefix,
			T:     *trade,
			Rate:  intCoder.Uint64(rateB),
			Force: tif,
		}, nil

	case bEqual(oType, orderTypeMarket):
		if len(pushes) != 2 {
			return nil, fmt.Errorf("decodeOrder_v0: expected 2 pushes for market order, got %d", len(pushes))
		}
		prefixB, tradeB := pushes[0], pushes[1]
		prefix, err := DecodePrefix(prefixB)
		if err != nil {
			return nil, err
		}
		trade, err := DecodeTrade(tradeB)
		if err != nil {
			return nil, err
		}
		return &MarketOrder{
			P: *prefix,
			T: *trade,
		}, nil

	case bEqual(oType, orderTypeCancel):
		if len(pushes) != 2 {
			return nil, fmt.Errorf("decodeOrder_v0: expected 2 pushes for cancel order, got %d", len(pushes))
		}
		prefixB, targetB := pushes[0], pushes[1]
		prefix, err := DecodePrefix(prefixB)
		if err != nil {
			return nil, err
		}
		if len(targetB) != OrderIDSize {
			return nil, fmt.Errorf("decodeOrder_v0: expected order ID of size %d, got %d", OrderIDSize, len(targetB))
		}
		var oID OrderID
		copy(oID[:], targetB)
		return &CancelOrder{
			P:             *prefix,
			TargetOrderID: oID,
		}, nil

	default:
		return nil, fmt.Errorf("decodeOrder_v0: unknown order type %x", oType)
	}
}
