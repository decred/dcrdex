// Package order defines the Order and Match types used throughout the DEX.
package order

import (
	"encoding/hex"
	"reflect"
	"testing"
	"time"

	"github.com/decred/dcrdex/server/account"
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

const (
	AssetDCR uint32 = iota
	AssetBTC
)

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
			[]byte{0x22, 0x4c, 0xba, 0xaa, 0xfa, 0x80, 0xbf, 0x3b, 0xd1, 0xff, 0x73,
				0x15, 0x90, 0xbc, 0xbd, 0xda, 0x5a, 0x76, 0xf9, 0x1e, 0x60, 0xa1,
				0x56, 0x99, 0x46, 0x34, 0xe9, 0x1c, 0xec, 0x25, 0xd5, 0x40, 0x0,
				0x0, 0x0, 0x0, 0x1, 0x0, 0x0, 0x0, 0x0, 0x75, 0xdb, 0x5e,
				0x5d, 0x0, 0x0, 0x0, 0x0, 0x78, 0xdb, 0x5e, 0x5d, 0x0, 0x0,
				0x0, 0x0},
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

func TestMarketOrder_Serialize(t *testing.T) {
	type fields struct {
		Prefix   Prefix
		UTXOs    []UTXO
		Side     OrderSide
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
				UTXOs:    []UTXO{},
				Side:     BuySide,
				Quantity: 132413241324,
				Address:  "DcqXswjTPnUcd4FRCkX4vRJxmVtfgGVa5ui",
			},
			[]byte{0x22, 0x4c, 0xba, 0xaa, 0xfa, 0x80, 0xbf, 0x3b, 0xd1, 0xff,
				0x73, 0x15, 0x90, 0xbc, 0xbd, 0xda, 0x5a, 0x76, 0xf9, 0x1e,
				0x60, 0xa1, 0x56, 0x99, 0x46, 0x34, 0xe9, 0x1c, 0xec, 0x25,
				0xd5, 0x40, 0x0, 0x0, 0x0, 0x0, 0x1, 0x0, 0x0, 0x0,
				0x1, 0x75, 0xdb, 0x5e, 0x5d, 0x0, 0x0, 0x0, 0x0, 0x78,
				0xdb, 0x5e, 0x5d, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0xec,
				0xb7, 0x71, 0xd4, 0x1e, 0x0, 0x0, 0x0, 0x44, 0x63, 0x71,
				0x58, 0x73, 0x77, 0x6a, 0x54, 0x50, 0x6e, 0x55, 0x63, 0x64,
				0x34, 0x46, 0x52, 0x43, 0x6b, 0x58, 0x34, 0x76, 0x52, 0x4a,
				0x78, 0x6d, 0x56, 0x74, 0x66, 0x67, 0x47, 0x56, 0x61, 0x35,
				0x75, 0x69},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := &MarketOrder{
				Prefix:   tt.fields.Prefix,
				UTXOs:    tt.fields.UTXOs,
				Side:     tt.fields.Side,
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

func TestLimitOrder_Serialize(t *testing.T) {
	type fields struct {
		MarketOrder MarketOrder
		Rate        float64
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
					UTXOs:    []UTXO{},
					Side:     BuySide,
					Quantity: 132413241324,
					Address:  "DcqXswjTPnUcd4FRCkX4vRJxmVtfgGVa5ui",
				},
				Rate:  0.13241324,
				Force: StandingTiF,
			},
			[]byte{0x22, 0x4c, 0xba, 0xaa, 0xfa, 0x80, 0xbf, 0x3b, 0xd1, 0xff,
				0x73, 0x15, 0x90, 0xbc, 0xbd, 0xda, 0x5a, 0x76, 0xf9, 0x1e,
				0x60, 0xa1, 0x56, 0x99, 0x46, 0x34, 0xe9, 0x1c, 0xec, 0x25,
				0xd5, 0x40, 0x0, 0x0, 0x0, 0x0, 0x1, 0x0, 0x0, 0x0,
				0x0, 0x75, 0xdb, 0x5e, 0x5d, 0x0, 0x0, 0x0, 0x0, 0x78,
				0xdb, 0x5e, 0x5d, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0xec,
				0xb7, 0x71, 0xd4, 0x1e, 0x0, 0x0, 0x0, 0x44, 0x63, 0x71,
				0x58, 0x73, 0x77, 0x6a, 0x54, 0x50, 0x6e, 0x55, 0x63, 0x64,
				0x34, 0x46, 0x52, 0x43, 0x6b, 0x58, 0x34, 0x76, 0x52, 0x4a,
				0x78, 0x6d, 0x56, 0x74, 0x66, 0x67, 0x47, 0x56, 0x61, 0x35,
				0x75, 0x69, 0x40, 0xbf, 0xad, 0xc3, 0xea, 0xf2, 0xc0, 0x3f,
				0x0},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := &LimitOrder{
				MarketOrder: tt.fields.MarketOrder,
				Rate:        tt.fields.Rate,
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
		Prefix  Prefix
		OrderID OrderID
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
					BaseAsset:  AssetDCR, // !
					QuoteAsset: AssetBTC, // !
					OrderType:  CancelOrderType,
					ClientTime: time.Unix(1566497693, 0),
					ServerTime: time.Unix(1566497696, 0),
				},
				OrderID: OrderID{0xce, 0x8c, 0xc8, 0xd, 0xda, 0x9a, 0x40, 0xbb, 0x43,
					0xba, 0x58, 0x9, 0x75, 0xfd, 0x23, 0x85, 0x4c, 0x4, 0x4d, 0x8, 0x12,
					0x54, 0x1f, 0x88, 0x25, 0x48, 0xaa, 0x8, 0x78, 0xe5, 0xa2, 0x67},
			},
			[]byte{0x22, 0x4c, 0xba, 0xaa, 0xfa, 0x80, 0xbf, 0x3b, 0xd1, 0xff, 0x73,
				0x15, 0x90, 0xbc, 0xbd, 0xda, 0x5a, 0x76, 0xf9, 0x1e, 0x60, 0xa1,
				0x56, 0x99, 0x46, 0x34, 0xe9, 0x1c, 0xec, 0x25, 0xd5, 0x40, 0x0,
				0x0, 0x0, 0x0, 0x1, 0x0, 0x0, 0x0, 0x2, 0x9d, 0xdb, 0x5e,
				0x5d, 0x0, 0x0, 0x0, 0x0, 0xa0, 0xdb, 0x5e, 0x5d, 0x0, 0x0,
				0x0, 0x0,
				0xce, 0x8c, 0xc8, 0xd, 0xda, 0x9a, 0x40, 0xbb, 0x43, 0xba, 0x58,
				0x9, 0x75, 0xfd, 0x23, 0x85, 0x4c, 0x4, 0x4d, 0x8, 0x12, 0x54,
				0x1f, 0x88, 0x25, 0x48, 0xaa, 0x8, 0x78, 0xe5, 0xa2, 0x67},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := &CancelOrder{
				Prefix:  tt.fields.Prefix,
				OrderID: tt.fields.OrderID,
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
	orderID0, _ := hex.DecodeString("5e68ca2f5c9d31e1e4d61d92e2c427969ac6bd6a99830774efbd3e35fc1405d0")
	var orderID OrderID
	copy(orderID[:], orderID0)

	type fields struct {
		Prefix   Prefix
		UTXOs    []UTXO
		Side     OrderSide
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
				UTXOs:    []UTXO{},
				Side:     BuySide,
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
				UTXOs:    tt.fields.UTXOs,
				Side:     tt.fields.Side,
				Quantity: tt.fields.Quantity,
				Address:  tt.fields.Address,
			}
			if got := o.ID(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("MarketOrder.ID() = %x, want %x", got, tt.want)
			}
		})
	}
}

func TestLimitOrder_ID(t *testing.T) {
	orderID0, _ := hex.DecodeString("0e41e1122fb63c84335547f1eb829844ab8e41a804f97201b2b5ddbdf4174850")
	var orderID OrderID
	copy(orderID[:], orderID0)

	type fields struct {
		MarketOrder MarketOrder
		Rate        float64
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
					UTXOs:    []UTXO{},
					Side:     BuySide,
					Quantity: 132413241324,
					Address:  "DcqXswjTPnUcd4FRCkX4vRJxmVtfgGVa5ui",
				},
				Rate:  0.132413241324,
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
			}
			if got := o.ID(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("LimitOrder.ID() = %x, want %x", got, tt.want)
			}
		})
	}
}

func TestCancelOrder_ID(t *testing.T) {
	limitOrderID0, _ := hex.DecodeString("0e41e1122fb63c84335547f1eb829844ab8e41a804f97201b2b5ddbdf4174850")
	var limitOrderID OrderID
	copy(limitOrderID[:], limitOrderID0)

	orderID0, _ := hex.DecodeString("61632e28f841581b811ec995e8f7e3738d7e45a87c354719760bf29c4861449c")
	var orderID OrderID
	copy(orderID[:], orderID0)

	type fields struct {
		Prefix  Prefix
		OrderID OrderID
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
				OrderID: limitOrderID,
			},
			orderID,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := &CancelOrder{
				Prefix:  tt.fields.Prefix,
				OrderID: tt.fields.OrderID,
			}
			if got := o.ID(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("CancelOrder.ID() = %x, want %x", got, tt.want)
			}
		})
	}
}
