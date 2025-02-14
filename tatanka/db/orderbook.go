// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package db

import (
	"errors"
	"fmt"
	"time"

	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/lexi"
	"decred.org/dcrdex/tatanka/tanka"
)

type OrderUpdate struct {
	Sig []byte `json:"sig"`
	*tanka.Order
}

func (o *OrderUpdate) MarshalBinary() ([]byte, error) {
	const orderVer = 0
	var b encode.BuildyBytes = make([]byte, 1, 32+4+4+1+8+8+8+8+8+8+4+8+len(o.Sig))
	b[0] = orderVer
	sell := encode.ByteFalse
	if o.Sell {
		sell = encode.ByteTrue
	}
	b = b.AddData(o.From[:]).
		AddData(encode.Uint32Bytes(o.BaseID)).
		AddData(encode.Uint32Bytes(o.QuoteID)).
		AddData(sell).
		AddData(encode.Uint64Bytes(o.Qty)).
		AddData(encode.Uint64Bytes(o.Rate)).
		AddData(encode.Uint64Bytes(o.LotSize)).
		AddData(encode.Uint64Bytes(o.MinFeeRate)).
		AddData(encode.Uint64Bytes(uint64(o.Stamp.UnixMilli()))).
		AddData(encode.Uint32Bytes(o.Nonce)).
		AddData(encode.Uint64Bytes(uint64(o.Expiration.UnixMilli()))).
		AddData(encode.Uint64Bytes(o.Settled)).
		AddData(o.Sig)

	return b, nil
}

func (o *OrderUpdate) UnmarshalBinary(b []byte) error {
	if o == nil {
		return errors.New("nil order update")
	}
	const orderVer = 0
	o.Order = new(tanka.Order)
	ver, pushes, err := encode.DecodeBlob(b, 13)
	if err != nil {
		return fmt.Errorf("error decoding order update blob: %w", err)
	}
	if ver != orderVer {
		return fmt.Errorf("unknown order update version %d", ver)
	}
	if len(pushes) != 13 {
		return fmt.Errorf("unknown number of order update blob pushes %d", len(pushes))
	}
	copy(o.From[:], pushes[0])
	o.BaseID = encode.BytesToUint32(pushes[1])
	o.QuoteID = encode.BytesToUint32(pushes[2])
	o.Sell = pushes[3][0] == encode.ByteTrue[0]
	o.Qty = encode.BytesToUint64(pushes[4])
	o.Rate = encode.BytesToUint64(pushes[5])
	o.LotSize = encode.BytesToUint64(pushes[6])
	o.MinFeeRate = encode.BytesToUint64(pushes[7])
	o.Stamp = time.UnixMilli(int64(encode.BytesToUint64(pushes[8])))
	o.Nonce = encode.BytesToUint32(pushes[9])
	o.Expiration = time.UnixMilli(int64(encode.BytesToUint64(pushes[10])))
	o.Settled = encode.BytesToUint64(pushes[11])
	o.Sig = pushes[12]

	return nil
}

type OrderBooker interface {
	OrderIDs(fromIdx, nOrders uint64) ([]tanka.ID32, bool, error)
	Order(id tanka.ID32) (*OrderUpdate, error)
	Orders(id []tanka.ID32) ([]*OrderUpdate, error)
	FindOrders(filter *OrderFilter) ([]*OrderUpdate, error)
	Add(*OrderUpdate) error
	Update(*OrderUpdate) error
	Delete(id tanka.ID32) error
}

type OrderFilter struct {
	IsSell *bool
	Check  func(*OrderUpdate) (ok, done bool)
}

var _ OrderBooker = (*OrderBook)(nil)

type OrderBook struct {
	orderBook            *lexi.Table
	orderBookOrderIDIdx  *lexi.Index
	orderBookStampIdx    *lexi.Index
	orderBookSellRateIdx *lexi.Index
}

func (ob *OrderBook) OrderIDs(fromIdx, nOrders uint64) (oids []tanka.ID32, all bool, err error) {
	var (
		i         uint64
		enoughErr = errors.New("enough")
	)
	all = true
	err = ob.orderBookOrderIDIdx.Iterate([]byte{}, func(it *lexi.Iter) error {
		if i < fromIdx {
			i++
			return nil
		}
		if i-fromIdx >= nOrders {
			all = false
			return enoughErr
		}
		k, err := it.K()
		if err != nil {
			return fmt.Errorf("error getting order key: %w", err)
		}
		var oid tanka.ID32
		copy(oid[:], k)
		oids = append(oids, oid)
		i++
		return nil
	})
	if err != nil && !errors.Is(err, enoughErr) {
		return nil, false, err
	}
	return oids, all, nil
}

func (ob *OrderBook) Order(id tanka.ID32) (ou *OrderUpdate, err error) {
	return ou, ob.orderBook.Get(id, ou)
}

func (ob *OrderBook) Orders(ids []tanka.ID32) ([]*OrderUpdate, error) {
	ous := make([]*OrderUpdate, 0, len(ids))
	for _, id := range ids {
		ou := new(OrderUpdate)
		if err := ob.orderBook.Get(id, ou); err != nil {
			return nil, err
		}
		ous = append(ous, ou)
	}
	return ous, nil
}

func (ob *OrderBook) FindOrders(filter *OrderFilter) (ous []*OrderUpdate, err error) {
	if filter == nil {
		return nil, errors.New("filter is nil")
	}
	var enoughErr = errors.New("enough")
	find := func(isSell bool) error {
		var i uint64
		prefix := [1]byte{}
		// Find best orders first.
		opts := lexi.WithReverse()
		if isSell {
			prefix[0] = 1
			opts = lexi.WithForward()
		}
		err = ob.orderBookSellRateIdx.Iterate(prefix[:], func(it *lexi.Iter) error {
			ou := new(OrderUpdate)
			if err := it.V(func(vB []byte) error {
				return ou.UnmarshalBinary(vB)
			}); err != nil {
				return err
			}
			if filter.Check != nil {
				ok, done := filter.Check(ou)
				if !ok {
					return nil
				}
				if done {
					return enoughErr
				}
			}
			ous = append(ous, ou)
			i++
			return nil
		}, opts)
		if err != nil && !errors.Is(err, enoughErr) {
			return err
		}
		return nil
	}
	if filter.IsSell != nil {
		if err := find(*filter.IsSell); err != nil {
			return nil, err
		}
	} else {
		if err := find(false); err != nil {
			return nil, err
		}
		if err := find(true); err != nil {
			return nil, err
		}
	}
	return ous, nil
}

func (ob *OrderBook) Add(ou *OrderUpdate) error {
	id := ou.ID()
	return ob.orderBook.Set(id, ou, lexi.WithReplace())
}

func (ob *OrderBook) Update(ou *OrderUpdate) error {
	id := ou.ID()
	return ob.orderBook.Set(id, ou, lexi.WithReplace())
}

func (ob *OrderBook) Delete(id tanka.ID32) error {
	return ob.orderBook.Delete(id[:])
}
