// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package db

import (
	"bytes"
	"testing"
	"time"

	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/server/account"
	"decred.org/dcrdex/server/meshevents"
)

func TestMarketStartedUpdateEventTxData(t *testing.T) {
	var acct account.AccountID
	acct[0] = 7
	ord := &order.LimitOrder{
		P: order.Prefix{
			AccountID:  acct,
			BaseAsset:  42,
			QuoteAsset: 0,
			OrderType:  order.LimitOrderType,
			ClientTime: time.UnixMilli(1000),
			ServerTime: time.UnixMilli(1001),
		},
		T: order.Trade{
			Sell:     true,
			Quantity: 10,
		},
		Rate:  5,
		Force: order.StandingTiF,
	}
	update := &MarketStartedUpdate{
		Market:          "dcr_btc",
		Base:            42,
		Quote:           0,
		CurrentEpochIdx: 123,
		EpochDur:        500,
		RevocationTime:  time.UnixMilli(123456789).UTC(),
	}

	for _, populated := range []bool{false, true} {
		name := "empty epoch revokes"
		if populated {
			name = "populated epoch revokes"
			update.EpochRevokes = []*StartupOrderRevoke{{Order: ord, Reason: meshevents.StartupOrderRevokeReasonEpochAbandoned}}
		}
		t.Run(name, func(t *testing.T) {
			txData, err := update.EventTxData()
			if err != nil {
				t.Fatal(err)
			}
			ver, fields, err := encode.DecodeBlob(txData)
			if err != nil || ver != 0 || len(fields) != 8 {
				t.Fatalf("transaction data: version %d, fields %d, error %v", ver, len(fields), err)
			}
			wantFields := [][]byte{
				[]byte("dcr_btc"), encode.Uint32Bytes(42), encode.Uint32Bytes(0),
				encode.Uint64Bytes(123), encode.Uint64Bytes(500), encode.Uint64Bytes(123456789),
				{0}, // Empty booked revocations.
			}
			for i, want := range wantFields {
				if !bytes.Equal(fields[i], want) {
					t.Fatalf("transaction field %d = %x, want %x", i, fields[i], want)
				}
			}
			ver, records, err := encode.DecodeBlob(fields[7])
			if err != nil || ver != 0 || len(records) != len(update.EpochRevokes) {
				t.Fatalf("epoch revocations: version %d, records %d, error %v", ver, len(records), err)
			}
			if !populated {
				return
			}
			ver, fields, err = encode.DecodeBlob(records[0])
			if err != nil || ver != 0 || len(fields) != 7 {
				t.Fatalf("revocation record: version %d, fields %d, error %v", ver, len(fields), err)
			}
			oid := ord.ID()
			wantFields = [][]byte{
				oid[:], acct[:], encode.Uint32Bytes(42), encode.Uint32Bytes(0),
				{byte(order.LimitOrderType)}, ord.Serialize(), {byte(meshevents.StartupOrderRevokeReasonEpochAbandoned)},
			}
			for i, want := range wantFields {
				if !bytes.Equal(fields[i], want) {
					t.Fatalf("revocation field %d = %x, want %x", i, fields[i], want)
				}
			}
		})
	}
}

func TestSuspendedCancelUpdateEventTxData(t *testing.T) {
	var acct account.AccountID
	acct[0] = 1
	const base, quote uint32 = 42, 0

	target := &order.LimitOrder{
		P: order.Prefix{
			AccountID:  acct,
			BaseAsset:  base,
			QuoteAsset: quote,
			OrderType:  order.LimitOrderType,
			ClientTime: time.UnixMilli(1000),
			ServerTime: time.UnixMilli(1001),
		},
		T: order.Trade{
			Sell:     true,
			Quantity: 10,
		},
		Rate:  5,
		Force: order.StandingTiF,
	}
	cancel := &order.CancelOrder{
		P: order.Prefix{
			AccountID:  acct,
			BaseAsset:  base,
			QuoteAsset: quote,
			OrderType:  order.CancelOrderType,
			ClientTime: time.UnixMilli(2000),
			ServerTime: time.UnixMilli(2001),
		},
		TargetOrderID: target.ID(),
	}
	match := &order.Match{
		Taker:    cancel,
		Maker:    target,
		Quantity: target.Remaining(),
		Rate:     target.Rate,
		Epoch:    order.EpochID{Idx: 123, Dur: 500},
		Status:   order.MatchComplete,
	}
	update := &SuspendedCancelUpdate{
		Market:          "dcr_btc",
		Base:            base,
		Quote:           quote,
		Cancel:          cancel,
		TargetOrderID:   target.ID(),
		TargetAccount:   acct,
		TargetSell:      target.Sell,
		EpochIdx:        123,
		EpochDur:        500,
		MatchServerTime: time.UnixMilli(3000),
		Match:           match,
	}

	incompleteMatch := *match
	incompleteMatch.Status = order.NewlyMatched
	missingMaker := *match
	missingMaker.Maker = nil
	missingTaker := *match
	missingTaker.Taker = nil
	for _, tc := range []struct {
		name   string
		update *SuspendedCancelUpdate
	}{
		{name: "nil update"},
		{name: "missing cancel", update: &SuspendedCancelUpdate{Match: match}},
		{name: "missing match", update: &SuspendedCancelUpdate{Cancel: cancel}},
		{name: "incomplete match", update: &SuspendedCancelUpdate{Cancel: cancel, Match: &incompleteMatch}},
		{name: "missing maker", update: &SuspendedCancelUpdate{Cancel: cancel, Match: &missingMaker}},
		{name: "missing taker", update: &SuspendedCancelUpdate{Cancel: cancel, Match: &missingTaker}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tc.update.EventTxData(); err == nil {
				t.Fatal("expected encoding error")
			}
		})
	}

	txData, err := update.EventTxData()
	if err != nil {
		t.Fatalf("EventTxData error: %v", err)
	}
	_, pushes, err := encode.DecodeBlob(txData)
	if err != nil {
		t.Fatalf("DecodeBlob error: %v", err)
	}
	if len(pushes) == 0 {
		t.Fatalf("no tx data pushes")
	}
	if !bytes.Equal(pushes[len(pushes)-1], []byte{byte(order.MatchComplete)}) {
		t.Fatalf("status push = %x, want %x", pushes[len(pushes)-1], []byte{byte(order.MatchComplete)})
	}
}
