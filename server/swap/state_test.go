package swap

import (
	"bytes"
	"encoding/gob"
	"reflect"
	"testing"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/server/account"
)

func Test_matchTrackerData_GobEncodeDecode(t *testing.T) {
	makerSell := true
	set := tPerfectLimitLimit(uint64(2e8), uint64(1e8), makerSell)
	orderMatch := set.matchInfos[0].match // tMatch.match is an *order.Match
	orderMatch.Epoch.Dur = 1234
	orderMatch.Epoch.Idx = 13241234
	orderMatch.Sigs.MakerMatch = []byte{2, 3}
	orderMatch.Sigs.TakerMatch = []byte{4, 5}

	mt := &matchTracker{
		Match: orderMatch,
		time:  time.Unix(132412349, 0).Truncate(time.Millisecond).UTC(),
		// matchTime is ignored by Data() since it should equal Match.Epoch.End()
		makerStatus: &swapStatus{
			swapAsset: 42,
		},
		takerStatus: &swapStatus{
			swapAsset: 1,
		},
	}

	mtd := mt.Data()

	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	if err := enc.Encode(mtd); err != nil {
		t.Fatalf("Encode (GobEncode) of matchTrackerData failed: %v", err)
	}

	mtdOut := new(matchTrackerData)
	rd := bytes.NewReader(buf.Bytes())
	dec := gob.NewDecoder(rd)
	if err := dec.Decode(mtdOut); err != nil {
		t.Fatalf("Decode (GobDecode) of matchTrackerData failed: %v", err)
	}

	// recompute the cached match IDs
	mtd.Match.ID()
	mtdOut.Match.ID()

	if !reflect.DeepEqual(mtdOut, mtd) {
		t.Errorf("different")
	}
}

func Test_state_GobEncodeDecode(t *testing.T) {
	makerSell := true
	set := tPerfectLimitLimit(uint64(2e8), uint64(1e8), makerSell)
	orderMatch := set.matchInfos[0].match // tMatch.match is an *order.Match
	orderMatch.Epoch.Dur = 1234
	orderMatch.Epoch.Idx = 13241234
	orderMatch.Sigs.MakerMatch = []byte{2, 3}
	orderMatch.Sigs.TakerMatch = []byte{4, 5}

	mt := &matchTracker{
		Match: orderMatch,
		time:  time.Unix(132412349, 0).Truncate(time.Millisecond).UTC(),
		// matchTime is ignored by Data() since it should equal Match.Epoch.End()
		makerStatus: &swapStatus{
			swapAsset: 42,
		},
		takerStatus: &swapStatus{
			swapAsset: 1,
		},
	}

	st := &State{
		MatchTrackers: map[order.MatchID]*matchTrackerData{
			orderMatch.ID(): mt.Data(),
		},
		OrderMatches: map[order.OrderID]*orderSwapStat{
			set.matchSet.Taker.ID(): {
				HasFailed: true,
				OffBook:   true,
				SwapCount: 6,
			},
		},
	}

	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	if err := enc.Encode(st); err != nil {
		t.Fatalf("Encode (GobEncode) of State failed: %v", err)
	}

	stOut := new(State)
	rd := bytes.NewReader(buf.Bytes())
	dec := gob.NewDecoder(rd)
	if err := dec.Decode(stOut); err != nil {
		t.Fatalf("Decode (GobDecode) of State failed: %v", err)
	}

	if stOut.MatchTrackers[orderMatch.ID()] == nil {
		t.Fatalf("match tracker for %v not found in output", orderMatch.ID())
	}
}

func Test_State_waiters_GobEncodeDecode(t *testing.T) {
	user := account.AccountID{0x2, 0x3}
	orderID := order.OrderID{0x8, 0x9}
	matchID := order.MatchID{0x4, 0x5}
	coinID := dex.Bytes{5, 6, 7}
	contract := dex.Bytes{0xa, 0xb, 0xc}

	reqID := uint64(1324321)
	init := msgjson.Init{
		OrderID:  orderID[:],
		MatchID:  matchID[:],
		CoinID:   coinID,
		Contract: contract,
	}
	msg, err := msgjson.NewRequest(reqID, msgjson.InitRoute, init)
	if err != nil {
		t.Fatalf("error creating init request: %v", err)
	}

	waiterArgs := &handlerArgs{
		User: user,
		Msg:  msg,
	}
	key := waiterKey{reqID, user}

	st := &State{
		LiveWaiters: map[waiterKey]*handlerArgs{
			key: waiterArgs,
		},
	}

	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	if err := enc.Encode(st); err != nil {
		t.Fatalf("Encode (GobEncode) of state failed: %v", err)
	}

	stOut := new(State)
	rd := bytes.NewReader(buf.Bytes())
	dec := gob.NewDecoder(rd)
	if err := dec.Decode(stOut); err != nil {
		t.Fatalf("Decode (GobDecode) of state failed: %v", err)
	}

	waiterArgsOut := stOut.LiveWaiters[key]
	if waiterArgsOut == nil {
		t.Fatalf("Failed to find msgAckers for user request %v+%d", user, reqID)
	}

	if !reflect.DeepEqual(stOut, st) {
		t.Errorf("different")
	}
}
