// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package encode

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math/rand"
	"time"

	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/server/account"
)

var (
	// IntCoder is the DEX-wide integer byte-encoding order.
	IntCoder = binary.BigEndian
	// A byte-slice representation of boolean true.
	ByteTrue = []byte{0}
	// A byte-slice representation of boolean false.
	ByteFalse = []byte{1}
	maxU16    = int(^uint16(0))
	bEqual    = bytes.Equal
)

// Uint64Bytes converts the uint16 to a length-2, big-endian encoded byte slice.
func Uint16Bytes(i uint16) []byte {
	b := make([]byte, 2)
	IntCoder.PutUint16(b, i)
	return b
}

// Uint64Bytes converts the uint32 to a length-4, big-endian encoded byte slice.
func Uint32Bytes(i uint32) []byte {
	b := make([]byte, 4)
	IntCoder.PutUint32(b, i)
	return b
}

// Uint64Bytes converts the uint64 to a length-8, big-endian encoded byte slice.
func Uint64Bytes(i uint64) []byte {
	b := make([]byte, 8)
	IntCoder.PutUint64(b, i)
	return b
}

// CopySlice makes a copy of the slice.
func CopySlice(b []byte) []byte {
	newB := make([]byte, len(b))
	copy(newB[:], b)
	return newB
}

// RandomBytes returns a byte slice with the specified length of random bytes.
func RandomBytes(len int) []byte {
	bytes := make([]byte, len)
	rand.Read(bytes)
	return bytes
}

// ExtractPushes parses the linearly-encoded 2D byte slice into a slice of
// slices.
func ExtractPushes(b []byte) ([][]byte, error) {
	pushes := make([][]byte, 0)
	for {
		if len(b) == 0 {
			break
		}
		l := int(b[0])
		b = b[1:]
		if l == 255 {
			if len(b) < 2 {
				return nil, fmt.Errorf("2 bytes not available for uint16 data length")
			}
			l = int(IntCoder.Uint16(b[:2]))
			b = b[2:]
		}
		if len(b) < l {
			return nil, fmt.Errorf("data too short for pop of %d bytes", l)
		}
		pushes = append(pushes, b[:l])
		b = b[l:]
	}
	return pushes, nil
}

// EncodePrefix encodes the order Prefix to a versioned blob.
func EncodePrefix(p *order.Prefix) []byte {
	return BuildyBytes{0}.
		AddData(p.AccountID[:]).
		AddData(Uint32Bytes(p.BaseAsset)).
		AddData(Uint32Bytes(p.QuoteAsset)).
		AddData([]byte{byte(p.OrderType)}).
		AddData(Uint64Bytes(uint64(p.ClientTime.Unix()))).
		AddData(Uint64Bytes(uint64(p.ServerTime.Unix())))
}

// DecodePrefix decodes the versioned blob to a *Prefix.
func DecodePrefix(b []byte) (prefix *order.Prefix, err error) {
	ver, pushes, err := DecodeBlob(b)
	if err != nil {
		return nil, err
	}
	switch ver {
	case 0:
		return decodePrefix_v0(pushes)
	}
	return nil, fmt.Errorf("unkown Prefix version %d", ver)
}

// decodePrefix_v0 decodes the v0 payload into a *Prefix.
func decodePrefix_v0(pushes [][]byte) (prefix *order.Prefix, err error) {
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
	return &order.Prefix{
		AccountID:  acctID,
		BaseAsset:  IntCoder.Uint32(baseB),
		QuoteAsset: IntCoder.Uint32(quoteB),
		OrderType:  order.OrderType(oTypeB[0]),
		ClientTime: time.Unix(int64(IntCoder.Uint64(cTimeB)), 0),
		ServerTime: time.Unix(int64(IntCoder.Uint64(sTimeB)), 0),
	}, nil
}

// EncodeTrade encodes the MarketOrder information, but not the Prefix
// structure.
func EncodeTrade(ord *order.MarketOrder) []byte {
	sell := ByteFalse
	if ord.Sell {
		sell = ByteTrue
	}
	coins := BuildyBytes{}
	for _, coin := range ord.Coins {
		coins = coins.AddData(coin)
	}
	return BuildyBytes{0}.
		AddData(coins).
		AddData(sell).
		AddData(Uint64Bytes(ord.Quantity)).
		AddData([]byte(ord.Address)).
		AddData(Uint64Bytes(ord.Filled))
}

// DecodeTrade decodes the versioned-blob market order, but does not populate
// the embedded Prefix.
func DecodeTrade(b []byte) (mrkt *order.MarketOrder, err error) {
	ver, pushes, err := DecodeBlob(b)
	if err != nil {
		return nil, err
	}
	switch ver {
	case 0:
		return decodeTrade_v0(pushes)
	}
	return nil, fmt.Errorf("unkown MarketOrder version %d", ver)
}

// decodeTrade_v0 decodes the version 0 payload into a MarketOrder, but
// does not populate the embedded Prefix.
func decodeTrade_v0(pushes [][]byte) (mrkt *order.MarketOrder, err error) {
	if len(pushes) != 5 {
		return nil, fmt.Errorf("expected 5 pushes, got %d", len(pushes))
	}
	coinsB, sellB, qtyB, addrB, filledB := pushes[0], pushes[1], pushes[2], pushes[3], pushes[4]
	rawCoins, err := ExtractPushes(coinsB)
	if err != nil {
		return nil, fmt.Errorf("error extracting coins: %v", err)
	}
	coins := make([]order.CoinID, 0, len(rawCoins))
	for _, coin := range rawCoins {
		coins = append(coins, coin)
	}
	sell := true
	if bEqual(sellB, ByteFalse) {
		sell = false
	}
	return &order.MarketOrder{
		Coins:    coins,
		Sell:     sell,
		Quantity: IntCoder.Uint64(qtyB),
		Address:  string(addrB),
		Filled:   IntCoder.Uint64(filledB),
	}, nil
}

// EncodeMatch encodes the UserMatch to bytes suitable for binary storage or
// communications.
func EncodeMatch(match *order.UserMatch) []byte {
	return BuildyBytes{0}.
		AddData(match.OrderID[:]).
		AddData(match.MatchID[:]).
		AddData(Uint64Bytes(match.Quantity)).
		AddData(Uint64Bytes(match.Rate)).
		AddData([]byte(match.Address)).
		AddData(Uint64Bytes(match.Time)).
		AddData([]byte{byte(match.Status)}).
		AddData([]byte{byte(match.Side)})
}

// DecodeMatch decodes the versioned blob into a UserMatch.
func DecodeMatch(b []byte) (match *order.UserMatch, err error) {
	ver, pushes, err := DecodeBlob(b)
	if err != nil {
		return nil, err
	}
	switch ver {
	case 0:
		return matchDecoder_v0(pushes)
	}
	return nil, fmt.Errorf("unkown UserMatch version %d", ver)
}

// matchDecoder_v0 decodes the version 0 payload into a *UserMatch.
func matchDecoder_v0(pushes [][]byte) (*order.UserMatch, error) {
	if len(pushes) != 8 {
		return nil, fmt.Errorf("decodeMatch: expected 8 pushes, got %d", len(pushes))
	}
	oidB, midB := pushes[0], pushes[1]
	if len(oidB) != order.OrderIDSize {
		return nil, fmt.Errorf("decodeMatch: expected length %d order ID, got %d", order.OrderIDSize, len(oidB))
	}
	if len(midB) != order.MatchIDSize {
		return nil, fmt.Errorf("decodeMatch: expected length %d match ID, got %d", order.MatchIDSize, len(midB))
	}
	var oid order.OrderID
	copy(oid[:], oidB)
	var mid order.MatchID
	copy(mid[:], midB)

	statusB, sideB := pushes[6], pushes[7]
	if len(statusB) != 1 || len(sideB) != 1 {
		return nil, fmt.Errorf("decodeMatch: status/side incorrect length %d/%d", len(statusB), len(sideB))
	}
	return &order.UserMatch{
		OrderID:  oid,
		MatchID:  mid,
		Quantity: IntCoder.Uint64(pushes[2]),
		Rate:     IntCoder.Uint64(pushes[3]),
		Address:  string(pushes[4]),
		Time:     IntCoder.Uint64(pushes[5]),
		Status:   order.MatchStatus(statusB[0]),
		Side:     order.MatchSide(sideB[0]),
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

// EncodeOrder encodes the order to bytes sutiable for wire communications or
// database storage. EncodeOrder accepts any type of Order.
func EncodeOrder(ord order.Order) []byte {
	var oType []byte
	var parts [][]byte
	switch o := ord.(type) {
	case *order.LimitOrder:
		oType = orderTypeLimit
		parts = append(parts, EncodePrefix(&o.Prefix))
		parts = append(parts, EncodeTrade(&o.MarketOrder))
		tif := orderTifStanding
		if o.Force == order.ImmediateTiF {
			tif = orderTifImmediate
		}
		parts = append(parts, BuildyBytes{}.AddData(Uint64Bytes(o.Rate)).AddData(tif))
	case *order.MarketOrder:
		oType = orderTypeMarket
		parts = append(parts, EncodePrefix(&o.Prefix))
		parts = append(parts, EncodeTrade(o))
	case *order.CancelOrder:
		oType = orderTypeCancel
		parts = append(parts, EncodePrefix(&o.Prefix))
		parts = append(parts, o.TargetOrderID[:])
	default:
		panic("encodeOrder: unknown order type")
	}
	encOrder := BuildyBytes{}.AddData(oType)
	for _, part := range parts {
		encOrder = encOrder.AddData(part)
	}
	return append([]byte{0}, encOrder...)
}

// DecodeOrder decodes the byte-encoded order. DecodeOrder accepts any type of
// order.
func DecodeOrder(b []byte) (ord order.Order, err error) {
	ver, pushes, err := DecodeBlob(b)
	if err != nil {
		return nil, err
	}
	switch ver {
	case 0:
		return decodeOrder_v0(pushes)
	}
	return nil, fmt.Errorf("unkown Order version %d", ver)
}

// decodeOrder_v0 decodes the version 0 payload into an Order.
func decodeOrder_v0(pushes [][]byte) (order.Order, error) {
	if len(pushes) == 0 {
		return nil, fmt.Errorf("decodeOrder: zero pushes for order")
	}
	// The first push should be the order type.
	oType := pushes[0]
	pushes = pushes[1:]
	switch {
	case bEqual(oType, orderTypeLimit):
		if len(pushes) != 3 {
			return nil, fmt.Errorf("decodeOrder: expected 3 pushes for limit order, got %d", len(pushes))
		}
		prefixB, tradeB, limitFlagsB := pushes[0], pushes[1], pushes[2]
		prefix, err := DecodePrefix(prefixB)
		if err != nil {
			return nil, err
		}
		mrkt, err := DecodeTrade(tradeB)
		if err != nil {
			return nil, err
		}
		flags, err := ExtractPushes(limitFlagsB)
		if err != nil {
			return nil, fmt.Errorf("decodeOrder: error extracting limit flags: %d", err)
		}
		if len(flags) != 2 {
			return nil, fmt.Errorf("decodeOrder: expected 2 error flags, got %d", len(flags))
		}
		rateB, tifB := flags[0], flags[1]
		tif := order.ImmediateTiF
		if bEqual(tifB, orderTifStanding) {
			tif = order.StandingTiF
		}
		mrkt.Prefix = *prefix
		return &order.LimitOrder{
			MarketOrder: *mrkt,
			Rate:        IntCoder.Uint64(rateB),
			Force:       tif,
		}, nil

	case bEqual(oType, orderTypeMarket):
		if len(pushes) != 2 {
			return nil, fmt.Errorf("decodeOrder: expected 2 pushes for market order, got %d", len(pushes))
		}
		prefixB, tradeB := pushes[0], pushes[1]
		prefix, err := DecodePrefix(prefixB)
		if err != nil {
			return nil, err
		}
		mrkt, err := DecodeTrade(tradeB)
		if err != nil {
			return nil, err
		}
		mrkt.Prefix = *prefix
		return mrkt, nil

	case bEqual(oType, orderTypeCancel):
		if len(pushes) != 2 {
			return nil, fmt.Errorf("decodeOrder: expected 2 pushes for cancel order, got %d", len(pushes))
		}
		prefixB, targetB := pushes[0], pushes[1]
		prefix, err := DecodePrefix(prefixB)
		if err != nil {
			return nil, err
		}
		if len(targetB) != order.OrderIDSize {
			return nil, fmt.Errorf("decodeOrder: expected order ID of size %d, got %d", order.OrderIDSize, len(targetB))
		}
		var oID order.OrderID
		copy(oID[:], targetB)
		return &order.CancelOrder{
			Prefix:        *prefix,
			TargetOrderID: oID,
		}, nil

	default:
		return nil, fmt.Errorf("decodeOrder: unknown order type %x", oType)
	}
}

// DecodeBlob decodes a versioned blob into its version and the pushes extracted
// from its data.
func DecodeBlob(b []byte) (byte, [][]byte, error) {
	if len(b) == 0 {
		return 0, nil, fmt.Errorf("zero length blob not allowed")
	}
	ver := b[0]
	b = b[1:]
	pushes, err := ExtractPushes(b)
	return ver, pushes, err
}

// BuildyBytes is a byte-slice with an AddData method for building linearly
// encoded 2D byte slices. The AddData method supports chaining. The canonical
// use case is to create "versioned blobs", where the BuildyBytes is instantated
// with a single version byte, and then data pushes are added using the AddData
// method. Example use:
//
//   version := 0
//   b := BuildyBytes{version}.AddData(data1).AddData(data2)
//
// The versioned blob can be decoded with DecodeBlob to separate the version
// byte and the "payload". BuildyBytes has some similarities to dcrd's
// txscript.ScriptBuilder, though simpler and less efficient.
type BuildyBytes []byte

// AddData adds the data to the BuildyBytes, and returns the new BuildyBytes.
// The data has hard-coded length limit of uint16_max = 65535 bytes.
func (b BuildyBytes) AddData(d []byte) BuildyBytes {
	l := len(d)
	var lBytes []byte
	if l > 0xff-1 {
		if l > maxU16 {
			panic("cannot use addData for pushes > 65535 bytes")
		}
		i := make([]byte, 2)
		IntCoder.PutUint16(i, uint16(l))
		lBytes = append([]byte{0xff}, i...)
	} else {
		lBytes = []byte{byte(l)}
	}
	return append(b, append(lBytes, d...)...)
}
