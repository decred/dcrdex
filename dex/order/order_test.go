// Package order defines the Order and Match types used throughout the DEX.
package order

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"reflect"
	"testing"
	"time"

	"decred.org/dcrdex/server/account"
)

// func randomAccount() (acct account.AccountID) {
// 	if _, err := rand.Read(acct[:]); err != nil {
// 		panic("boom")
// 	}
// 	return
// }

var acct0 = account.AccountID{
	0x22, 0x4c, 0xba, 0xaa, 0xfa, 0x80, 0xbf, 0x3b, 0xd1, 0xff, 0x73, 0x15,
	0x90, 0xbc, 0xbd, 0xda, 0x5a, 0x76, 0xf9, 0x1e, 0x60, 0xa1, 0x56, 0x99,
	0x46, 0x34, 0xe9, 0x1c, 0xec, 0x25, 0xd5, 0x40,
}

// var acctX = account.AccountID{
// 	0x12, 0x4c, 0xba, 0xaa, 0xfa, 0x80, 0xbf, 0x3b, 0xd1, 0xff, 0x73, 0x15,
// 	0x90, 0xbc, 0xbd, 0xda, 0x5a, 0x76, 0xf9, 0x1e, 0x60, 0xa1, 0x56, 0x99,
// 	0x46, 0x34, 0xe9, 0x1c, 0xec, 0x25, 0xd5, 0x41,
// }

const (
	AssetDCR uint32 = iota
	AssetBTC
)

func utxoCoinID(txid string, vout uint32) CoinID {
	hash, err := hex.DecodeString(txid)
	if err != nil {
		panic(err)
	}
	hashLen := len(hash)
	b := make([]byte, hashLen+4)
	copy(b[:hashLen], hash)
	binary.BigEndian.PutUint32(b[hashLen:], vout)
	return b
}

func Test_calcOrderID(t *testing.T) {
	mo := &MarketOrder{}
	defer func() {
		if recover() == nil {
			t.Error("MarketOrder.ID should have paniced with ServerTime unset.")
		}
	}()
	_ = calcOrderID(mo)
}

func TestPrefix_Serialize(t *testing.T) {
	type fields struct {
		AccountID  account.AccountID
		BaseAsset  uint32
		QuoteAsset uint32
		OrderType  OrderType
		ClientTime time.Time
		ServerTime time.Time
	}
	tests := []struct {
		name   string
		fields fields
		want   []byte
	}{
		{
			"ok acct0",
			fields{
				AccountID:  acct0,
				BaseAsset:  AssetDCR,
				QuoteAsset: AssetBTC,
				OrderType:  LimitOrderType,
				ClientTime: time.Unix(1566497653, 0),
				ServerTime: time.Unix(1566497656, 0),
			},
			[]byte{
				// AccountID 32 bytes
				0x22, 0x4c, 0xba, 0xaa, 0xfa, 0x80, 0xbf, 0x3b, 0xd1, 0xff, 0x73,
				0x15, 0x90, 0xbc, 0xbd, 0xda, 0x5a, 0x76, 0xf9, 0x1e, 0x60, 0xa1,
				0x56, 0x99, 0x46, 0x34, 0xe9, 0x1c, 0xec, 0x25, 0xd5, 0x40,
				// BaseAsset 4 bytes
				0x0, 0x0, 0x0, 0x0,
				// QuoteAsset 4 bytes
				0x0, 0x0, 0x0, 0x1,
				// OrderType 1 byte
				0x1,
				// ClientTime 8 bytes
				0x0, 0x0, 0x0, 0x0, 0x5d, 0x5e, 0xdb, 0x75,
				// ServerTime 8 bytes
				0x0, 0x0, 0x0, 0x0, 0x5d, 0x5e, 0xdb, 0x78,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Prefix{
				AccountID:  tt.fields.AccountID,
				BaseAsset:  tt.fields.BaseAsset,
				QuoteAsset: tt.fields.QuoteAsset,
				OrderType:  tt.fields.OrderType,
				ClientTime: tt.fields.ClientTime,
				ServerTime: tt.fields.ServerTime,
			}
			got := p.Serialize()
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Prefix.Serialize() = %#v, want %#v", got, tt.want)
			}
			sz := p.SerializeSize()
			wantSz := len(got)
			if sz != wantSz {
				t.Errorf("Prefix.SerializeSize() = %d,\n want %d", sz, wantSz)
			}
		})
	}
}

func TestMarketOrder_Serialize_SerializeSize(t *testing.T) {
	type fields struct {
		Prefix   Prefix
		Coins    []CoinID
		Sell     bool
		Quantity uint64
		Address  string
	}
	tests := []struct {
		name   string
		fields fields
		want   []byte
	}{
		{
			"ok acct0",
			fields{
				Prefix: Prefix{
					AccountID:  acct0,
					BaseAsset:  AssetDCR,
					QuoteAsset: AssetBTC,
					OrderType:  MarketOrderType,
					ClientTime: time.Unix(1566497653, 0),
					ServerTime: time.Unix(1566497656, 0),
				},
				Coins: []CoinID{
					utxoCoinID("aed8e9b2b889bf0a78e559684796800144cd76dc8faac2aeac44fbd1c310124b", 1),
					utxoCoinID("45b82138ca90e665a1c8793aa901aa232dd82be41b8e630dd621f24e717fc13a", 2),
				},
				Sell:     false,
				Quantity: 132413241324,
				Address:  "DcqXswjTPnUcd4FRCkX4vRJxmVtfgGVa5ui",
			},
			[]byte{
				// Prefix - AccountID 32 bytes
				0x22, 0x4c, 0xba, 0xaa, 0xfa, 0x80, 0xbf, 0x3b, 0xd1, 0xff, 0x73, 0x15,
				0x90, 0xbc, 0xbd, 0xda, 0x5a, 0x76, 0xf9, 0x1e, 0x60, 0xa1, 0x56, 0x99,
				0x46, 0x34, 0xe9, 0x1c, 0xec, 0x25, 0xd5, 0x40,
				// Prefix - BaseAsset 4 bytes
				0x0, 0x0, 0x0, 0x0,
				// Prefix - QuoteAsset 4 bytes
				0x0, 0x0, 0x0, 0x1,
				// Prefix - OrderType 1 byte
				0x2,
				// Prefix - ClientTime 8 bytes
				0x0, 0x0, 0x0, 0x0, 0x5d, 0x5e, 0xdb, 0x75,
				// Prefix - ServerTime 8 bytes
				0x0, 0x0, 0x0, 0x0, 0x5d, 0x5e, 0xdb, 0x78,
				// UTXO count 1 byte
				0x2,
				// UTXO 1 hash 32 bytes
				0xae, 0xd8, 0xe9, 0xb2, 0xb8, 0x89, 0xbf, 0xa, 0x78, 0xe5, 0x59, 0x68,
				0x47, 0x96, 0x80, 0x1, 0x44, 0xcd, 0x76, 0xdc, 0x8f, 0xaa, 0xc2, 0xae,
				0xac, 0x44, 0xfb, 0xd1, 0xc3, 0x10, 0x12, 0x4b,
				// UTXO 1 vout 4 bytes
				0x0, 0x0, 0x0, 0x1,
				// UTXO 2 hash 32 bytes
				0x45, 0xb8, 0x21, 0x38, 0xca, 0x90, 0xe6, 0x65, 0xa1, 0xc8, 0x79, 0x3a,
				0xa9, 0x1, 0xaa, 0x23, 0x2d, 0xd8, 0x2b, 0xe4, 0x1b, 0x8e, 0x63, 0xd,
				0xd6, 0x21, 0xf2, 0x4e, 0x71, 0x7f, 0xc1, 0x3a,
				// UTXO 2 vout 4 bytes
				0x0, 0x0, 0x0, 0x2,
				// Sell 1 byte
				0x0,
				// Quantity 8 bytes
				0x0, 0x0, 0x0, 0x1e, 0xd4, 0x71, 0xb7, 0xec,
				// Address (variable size)
				0x44, 0x63, 0x71, 0x58, 0x73, 0x77, 0x6a, 0x54, 0x50, 0x6e, 0x55, 0x63,
				0x64, 0x34, 0x46, 0x52, 0x43, 0x6b, 0x58, 0x34, 0x76, 0x52, 0x4a, 0x78,
				0x6d, 0x56, 0x74, 0x66, 0x67, 0x47, 0x56, 0x61, 0x35, 0x75, 0x69},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := &MarketOrder{
				Prefix:   tt.fields.Prefix,
				Coins:    tt.fields.Coins,
				Sell:     tt.fields.Sell,
				Quantity: tt.fields.Quantity,
				Address:  tt.fields.Address,
			}
			got := o.Serialize()
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("MarketOrder.Serialize() = %#v,\n want %#v", got, tt.want)
			}
			sz := o.SerializeSize()
			wantSz := len(got)
			if sz != wantSz {
				t.Errorf("MarketOrder.SerializeSize() = %d,\n want %d", sz, wantSz)
			}
		})
	}
}

func TestLimitOrder_Serialize_SerializeSize(t *testing.T) {
	type fields struct {
		MarketOrder MarketOrder
		Rate        uint64
		Force       TimeInForce
	}
	tests := []struct {
		name   string
		fields fields
		want   []byte
	}{
		{
			"ok acct0",
			fields{
				MarketOrder: MarketOrder{
					Prefix: Prefix{
						AccountID:  acct0,
						BaseAsset:  AssetDCR,
						QuoteAsset: AssetBTC,
						OrderType:  LimitOrderType,
						ClientTime: time.Unix(1566497653, 0),
						ServerTime: time.Unix(1566497656, 0),
					},
					Coins: []CoinID{
						utxoCoinID("d186e4b6625c9c94797cc494f535fc150177e0619e2303887e0a677f29ef1bab", 0),
						utxoCoinID("11d9580e19ad65a875a5bc558d600e96b2916062db9e8b65cbc2bb905207c1ad", 16),
					},
					Sell:     false,
					Quantity: 132413241324,
					Address:  "DcqXswjTPnUcd4FRCkX4vRJxmVtfgGVa5ui",
				},
				Rate:  13241324,
				Force: StandingTiF,
			},
			[]byte{
				// Prefix - AccountID 32 bytes
				0x22, 0x4c, 0xba, 0xaa, 0xfa, 0x80, 0xbf, 0x3b, 0xd1, 0xff, 0x73, 0x15,
				0x90, 0xbc, 0xbd, 0xda, 0x5a, 0x76, 0xf9, 0x1e, 0x60, 0xa1, 0x56, 0x99,
				0x46, 0x34, 0xe9, 0x1c, 0xec, 0x25, 0xd5, 0x40,
				// Prefix - BaseAsset 4 bytes
				0x0, 0x0, 0x0, 0x0,
				// Prefix - QuoteAsset 4 bytes
				0x0, 0x0, 0x0, 0x1,
				// Prefix - OrderType 1 byte
				0x1,
				// Prefix - ClientTime 8 bytes
				0x0, 0x0, 0x0, 0x0, 0x5d, 0x5e, 0xdb, 0x75,
				// Prefix - ServerTime 8 bytes
				0x0, 0x0, 0x0, 0x0, 0x5d, 0x5e, 0xdb, 0x78,
				// UTXO count 1 byte
				0x2,
				// UTXO 1 hash 32 bytes
				0xd1, 0x86, 0xe4, 0xb6, 0x62, 0x5c, 0x9c, 0x94, 0x79, 0x7c, 0xc4, 0x94,
				0xf5, 0x35, 0xfc, 0x15, 0x1, 0x77, 0xe0, 0x61, 0x9e, 0x23, 0x3, 0x88,
				0x7e, 0xa, 0x67, 0x7f, 0x29, 0xef, 0x1b, 0xab,
				// UTXO 1 vout 4 bytes
				0x0, 0x0, 0x0, 0x0,
				// UTXO 2 hash 32 bytes
				0x11, 0xd9, 0x58, 0xe, 0x19, 0xad, 0x65, 0xa8, 0x75, 0xa5, 0xbc, 0x55,
				0x8d, 0x60, 0xe, 0x96, 0xb2, 0x91, 0x60, 0x62, 0xdb, 0x9e, 0x8b, 0x65,
				0xcb, 0xc2, 0xbb, 0x90, 0x52, 0x7, 0xc1, 0xad,
				// UTXO 2 vout 4 bytes
				0x0, 0x0, 0x0, 0x10,
				// Sell 1 byte
				0x0,
				// Quantity 8 bytes
				0x0, 0x0, 0x0, 0x1e, 0xd4, 0x71, 0xb7, 0xec,
				// Address (variable size)
				0x44, 0x63, 0x71,
				0x58, 0x73, 0x77, 0x6a, 0x54, 0x50, 0x6e, 0x55, 0x63, 0x64, 0x34, 0x46,
				0x52, 0x43, 0x6b, 0x58, 0x34, 0x76, 0x52, 0x4a, 0x78, 0x6d, 0x56, 0x74,
				0x66, 0x67, 0x47, 0x56, 0x61, 0x35, 0x75, 0x69,
				// Rate 8 bytes
				0x0, 0x0, 0x0, 0x0, 0x0, 0xca, 0xb, 0xec,
				// Force 1 byte
				0x1,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := &LimitOrder{
				MarketOrder: tt.fields.MarketOrder,
				Rate:        tt.fields.Rate,
				Force:       tt.fields.Force,
			}
			got := o.Serialize()
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("LimitOrder.Serialize() = %#v, want %#v", got, tt.want)
			}
			sz := o.SerializeSize()
			wantSz := len(got)
			if sz != wantSz {
				t.Errorf("LimitOrder.SerializeSize() = %d,\n want %d", sz, wantSz)
			}
		})
	}
}

func TestCancelOrder_Serialize(t *testing.T) {
	type fields struct {
		Prefix        Prefix
		TargetOrderID OrderID
	}
	tests := []struct {
		name   string
		fields fields
		want   []byte
	}{
		{
			"ok",
			fields{
				Prefix: Prefix{
					AccountID:  acct0,
					BaseAsset:  AssetDCR,
					QuoteAsset: AssetBTC,
					OrderType:  CancelOrderType,
					ClientTime: time.Unix(1566497693, 0),
					ServerTime: time.Unix(1566497696, 0),
				},
				TargetOrderID: OrderID{0xce, 0x8c, 0xc8, 0xd, 0xda, 0x9a, 0x40, 0xbb, 0x43,
					0xba, 0x58, 0x9, 0x75, 0xfd, 0x23, 0x85, 0x4c, 0x4, 0x4d, 0x8, 0x12,
					0x54, 0x1f, 0x88, 0x25, 0x48, 0xaa, 0x8, 0x78, 0xe5, 0xa2, 0x67},
			},
			[]byte{
				// Prefix - AccountID 32 bytes
				0x22, 0x4c, 0xba, 0xaa, 0xfa, 0x80, 0xbf, 0x3b, 0xd1, 0xff, 0x73,
				0x15, 0x90, 0xbc, 0xbd, 0xda, 0x5a, 0x76, 0xf9, 0x1e, 0x60, 0xa1,
				0x56, 0x99, 0x46, 0x34, 0xe9, 0x1c, 0xec, 0x25, 0xd5, 0x40,
				// Prefix - BaseAsset 4 bytes
				0x0, 0x0, 0x0, 0x0,
				// Prefix - QuoteAsset 4 bytes
				0x0, 0x0, 0x0, 0x1,
				// Prefix - OrderType 1 byte
				0x3,
				// Prefix - ClientTime 8 bytes
				0x0, 0x0, 0x0, 0x0, 0x5d, 0x5e, 0xdb, 0x9d,
				// Prefix - ServerTime 8 bytes
				0x0, 0x0, 0x0, 0x0, 0x5d, 0x5e, 0xdb, 0xa0,
				// Order ID - 32 bytes
				0xce, 0x8c, 0xc8, 0xd, 0xda, 0x9a, 0x40, 0xbb, 0x43, 0xba, 0x58,
				0x9, 0x75, 0xfd, 0x23, 0x85, 0x4c, 0x4, 0x4d, 0x8, 0x12, 0x54,
				0x1f, 0x88, 0x25, 0x48, 0xaa, 0x8, 0x78, 0xe5, 0xa2, 0x67,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := &CancelOrder{
				Prefix:        tt.fields.Prefix,
				TargetOrderID: tt.fields.TargetOrderID,
			}
			got := o.Serialize()
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("CancelOrder.Serialize() = %#v, want %v", got, tt.want)
			}
			sz := o.SerializeSize()
			wantSz := len(got)
			if sz != wantSz {
				t.Errorf("CancelOrder.SerializeSize() = %d,\n want %d", sz, wantSz)
			}
		})
	}
}

func TestMarketOrder_ID(t *testing.T) {
	orderID0, _ := hex.DecodeString("a347a8b9b9204e7626ed9f03e6a3a49f16a527451fb42c4d6c9494b136e85d50")
	var orderID OrderID
	copy(orderID[:], orderID0)

	type fields struct {
		Prefix   Prefix
		Coins    []CoinID
		Sell     bool
		Quantity uint64
		Address  string
	}
	tests := []struct {
		name   string
		fields fields
		want   OrderID
	}{
		{
			"ok",
			fields{
				Prefix: Prefix{
					AccountID:  acct0,
					BaseAsset:  AssetDCR,
					QuoteAsset: AssetBTC,
					OrderType:  MarketOrderType,
					ClientTime: time.Unix(1566497653, 0),
					ServerTime: time.Unix(1566497656, 0),
				},
				Coins: []CoinID{
					utxoCoinID("a985d8df97571b130ce30a049a76ffedaa79b6e69b173ff81b1bf9fc07f063c7", 1),
				},
				Sell:     true,
				Quantity: 132413241324,
				Address:  "DcqXswjTPnUcd4FRCkX4vRJxmVtfgGVa5ui",
			},
			orderID,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := &MarketOrder{
				Prefix:   tt.fields.Prefix,
				Coins:    tt.fields.Coins,
				Sell:     tt.fields.Sell,
				Quantity: tt.fields.Quantity,
				Address:  tt.fields.Address,
			}
			remaining := o.Remaining()
			if remaining != o.Quantity-o.Filled {
				t.Errorf("MarketOrder.Remaining incorrect, got %d, expected %d",
					remaining, o.Quantity-o.Filled)
			}
			if got := o.ID(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("MarketOrder.ID() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLimitOrder_ID(t *testing.T) {
	orderID0, _ := hex.DecodeString("1bd2250771efa587e2b076bc59b93ee56bcbfd8b8d534a362127d23ce4766f51")
	var orderID OrderID
	copy(orderID[:], orderID0)

	type fields struct {
		MarketOrder MarketOrder
		Rate        uint64
		Force       TimeInForce
	}
	tests := []struct {
		name   string
		fields fields
		want   OrderID
	}{
		{
			"ok",
			fields{
				MarketOrder: MarketOrder{
					Prefix: Prefix{
						AccountID:  acct0,
						BaseAsset:  AssetDCR,
						QuoteAsset: AssetBTC,
						OrderType:  LimitOrderType,
						ClientTime: time.Unix(1566497653, 0),
						ServerTime: time.Unix(1566497656, 0),
					},
					Coins: []CoinID{
						utxoCoinID("01516d9c7ffbe260b811dc04462cedd3f8969ce3a3ffe6231ae870775a92e9b0", 1),
					},
					Sell:     false,
					Quantity: 132413241324,
					Address:  "DcqXswjTPnUcd4FRCkX4vRJxmVtfgGVa5ui",
				},
				Rate:  13241324,
				Force: StandingTiF,
			},
			orderID,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := &LimitOrder{
				MarketOrder: tt.fields.MarketOrder,
				Rate:        tt.fields.Rate,
				Force:       tt.fields.Force,
			}
			remaining := o.Remaining()
			if remaining != o.Quantity-o.Filled {
				t.Errorf("LimitOrder.Remaining incorrect, got %d, expected %d",
					remaining, o.Quantity-o.Filled)
			}
			if got := o.ID(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("LimitOrder.ID() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCancelOrder_ID(t *testing.T) {
	limitOrderID0, _ := hex.DecodeString("60ba7b92d0905aeaf93df3a69f28df2c7133b52361ec6114c825989b69bcf25b")
	var limitOrderID OrderID
	copy(limitOrderID[:], limitOrderID0)

	orderID0, _ := hex.DecodeString("c051a262af8bd781e1464e72b3c24c2774989eca44d08ef088d82122ce669750")
	var cancelOrderID OrderID
	copy(cancelOrderID[:], orderID0)

	type fields struct {
		Prefix        Prefix
		TargetOrderID OrderID
	}
	tests := []struct {
		name   string
		fields fields
		want   OrderID
	}{
		{
			"ok",
			fields{
				Prefix: Prefix{
					AccountID:  acct0,
					BaseAsset:  AssetDCR,
					QuoteAsset: AssetBTC,
					OrderType:  CancelOrderType,
					ClientTime: time.Unix(1566497693, 0),
					ServerTime: time.Unix(1566497696, 0),
				},
				TargetOrderID: limitOrderID,
			},
			cancelOrderID,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := &CancelOrder{
				Prefix:        tt.fields.Prefix,
				TargetOrderID: tt.fields.TargetOrderID,
			}
			remaining := o.Remaining()
			if remaining != 0 {
				t.Errorf("CancelOrder.Remaining should be 0, got %d", remaining)
			}
			if got := o.ID(); !reflect.DeepEqual(got, tt.want) {
				fmt.Printf("--cancel order serialization: %x\n", o.Serialize())
				t.Errorf("CancelOrder.ID() = %v, want %v", got, tt.want)
			}
		})
	}
}

// func randomHash() (h [32]byte) {
// 	rand.Read(h[:])
// 	return
// }

func Test_UTXOtoCoinID(t *testing.T) {
	type args struct {
		id CoinID
	}
	tests := []struct {
		name string
		args args
		want []byte
	}{
		{
			"ok",
			args{
				utxoCoinID("bc4b0ffe3a70cf159657b1f8f12c2d895c5d7e849de6ac1c3358be86842f4549", 4),
			},
			[]byte{
				// Tx hash 32 bytes
				0xbc, 0x4b, 0xf, 0xfe, 0x3a, 0x70, 0xcf, 0x15, 0x96, 0x57, 0xb1, 0xf8,
				0xf1, 0x2c, 0x2d, 0x89, 0x5c, 0x5d, 0x7e, 0x84, 0x9d, 0xe6, 0xac, 0x1c,
				0x33, 0x58, 0xbe, 0x86, 0x84, 0x2f, 0x45, 0x49,
				// Vout 4 bytes
				0x0, 0x0, 0x0, 0x4,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !bytes.Equal(tt.args.id, tt.want) {
				t.Errorf("serializeOutpoint() = %#v, want %v", tt.args.id, tt.want)
			}
		})
	}
}
