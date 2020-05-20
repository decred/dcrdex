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

// Test binary marshal/unmarshal of State.LiveAckers map.
func Test_State_ackers_GobEncodeDecode(t *testing.T) {
	// msgjson.Match request with 2 matches for one order.
	user := account.AccountID{0x2, 0x3}
	matchIDs := []order.MatchID{
		{0x4, 0x5},
		{0x6, 0x7},
	}
	order := order.OrderID{0x8, 0x9}

	matchMsgs := []msgjson.Signable{
		&msgjson.Match{
			// signable: signable{
			// 	Sig: nil,
			// },
			OrderID:    order[:],
			MatchID:    matchIDs[0][:],
			Quantity:   7890,
			Rate:       6543,
			Address:    "asdf",
			ServerTime: 1324132412,
			Status:     1,
			Side:       1,
		},
		&msgjson.Match{
			OrderID:    order[:],
			MatchID:    matchIDs[1][:],
			Quantity:   81455,
			Rate:       6666,
			Address:    "qwerty",
			ServerTime: 1324132499,
			Status:     1,
			Side:       1,
		},
	}

	matchMsgs[0].SetSig([]byte{42, 43})
	matchMsgs[1].SetSig([]byte{44, 45})

	matchReqID := uint64(1324321)
	matchReq, err := msgjson.NewRequest(matchReqID, msgjson.MatchRoute, matchMsgs)
	if err != nil {
		t.Fatalf("error creating match notification request: %v", err)
	}

	ackerDatas := make([]*ackerData, len(matchMsgs))
	for i, signable := range matchMsgs {
		ackerDatas[i] = &ackerData{
			User:    user,
			MatchID: matchIDs[i],
			Params:  signable,
			IsMaker: true, // whatever, just not the zero value
		}
	}

	matchReqAckers := &msgAckers{
		Request: matchReq,
		AckData: ackerDatas,
	}

	// msgjson.Audit request
	// auditMsgs := []msgjson.Signable{
	// 	&msgjson.Audit{},
	// }

	st := &State{
		LiveAckers: map[uint64]*msgAckers{
			matchReqID: matchReqAckers,
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

	liveAcker := stOut.LiveAckers[matchReqID]
	if liveAcker == nil {
		t.Fatalf("Failed to find msgAckers for request id %d", matchReqID)
	}
	//spew.Dump(*liveAcker)

	wantSigBytes := matchMsgs[0].(*msgjson.Match).SigBytes()
	gotSigBytes := liveAcker.AckData[0].Params.(*msgjson.Match).SigBytes()
	if !bytes.Equal(wantSigBytes, gotSigBytes) {
		t.Fatalf("incorrect signature bytes")
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
	//spew.Dump(*waiterArgsOut)

	if !reflect.DeepEqual(stOut, st) {
		t.Errorf("different")
	}
}
