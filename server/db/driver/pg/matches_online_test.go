//go:build pgonline

package pg

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"testing"
	"time"

	"decred.org/dcrdex/dex/candles"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/server/account"
	"decred.org/dcrdex/server/db"
	"decred.org/dcrdex/server/db/driver/pg/internal"
	"decred.org/dcrdex/server/meshevents"
)

// insertMatchForTest stores or updates a match row via the shared upsertMatch
// helper, the same path the epoch_processed event applier takes.
func insertMatchForTest(match *order.Match) error {
	matchesTableName, err := archie.matchTableName(match)
	if err != nil {
		return err
	}
	N, err := upsertMatch(archie.db, matchesTableName, match)
	if err != nil {
		return err
	}
	if N != 1 {
		return fmt.Errorf("upsertMatch: updated %d rows, expected 1", N)
	}
	return nil
}

// setMatchInactiveForTest marks a fixture match inactive.
func setMatchInactiveForTest(mid db.MarketMatchID) error {
	return archie.updateMatchStmtWithExecutor(archie.db, mid, internal.SetSwapDone, mid.MatchID)
}

// saveContractForTest records a swap contract with recordSwapContract.
func saveContractForTest(mid db.MarketMatchID, maker bool, contract, coinID []byte, timestamp int64) error {
	return archie.recordSwapContract(archie.db, &meshevents.SwapContractRecordedEvent{
		MatchID:  mid.MatchID,
		Base:     mid.Base,
		Quote:    mid.Quote,
		Maker:    maker,
		Contract: contract,
		CoinID:   coinID,
		SwapTime: timestamp,
	})
}

// saveRedeemForTest records a redemption via the shared recordRedeemData
// helper used by the swap_redemption_recorded event applier.
func saveRedeemForTest(mid db.MarketMatchID, maker bool, coinID, secret []byte, timestamp int64) error {
	return archie.recordRedeemData(archie.db, &meshevents.SwapRedemptionRecordedEvent{
		MatchID:    mid.MatchID,
		Base:       mid.Base,
		Quote:      mid.Quote,
		Maker:      maker,
		CoinID:     coinID,
		Secret:     secret,
		RedeemTime: timestamp,
	})
}

func Test_upsertMatch(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	// Make a perfect 1 lot match.
	limitBuyStanding := newLimitOrder(false, 4500000, 1, order.StandingTiF, 0)
	limitSellImmediate := newLimitOrder(true, 4490000, 1, order.ImmediateTiF, 10)

	epochID := order.EpochID{132412341, 1000}
	// Taker is selling.
	matchA := newMatch(limitBuyStanding, limitSellImmediate, limitSellImmediate.Quantity, epochID)

	base, quote := limitBuyStanding.Base(), limitBuyStanding.Quote()

	matchAUpdated := matchA
	matchAUpdated.Status = order.MakerSwapCast
	// matchAUpdated.Sigs.MakerMatch = randomBytes(73)

	cancelLOBuy := newCancelOrder(limitBuyStanding.ID(), base, quote, 0)
	matchCancel := newMatch(limitBuyStanding, cancelLOBuy, 0, epochID)
	matchCancel.Status = order.MatchComplete // will be forced to complete on store too

	tests := []struct {
		name     string
		match    *order.Match
		wantErr  bool
		isCancel bool
	}{
		{
			"store ok",
			matchA,
			false,
			false,
		},
		{
			"update ok",
			matchAUpdated,
			false,
			false,
		},
		{
			"update again ok",
			matchAUpdated,
			false,
			false,
		},
		{
			"cancel",
			matchCancel,
			false,
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := insertMatchForTest(tt.match)
			if (err != nil) != tt.wantErr {
				t.Errorf("insertMatchForTest() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.wantErr {
				return
			}

			matchID := tt.match.ID()
			matchData, err := archie.MatchByID(matchID, base, quote)
			if err != nil {
				t.Fatal(err)
			}
			if matchData.ID != matchID {
				t.Errorf("Retrieved match with ID %v, expected %v", matchData.ID, matchID)
			}
			if matchData.Status != tt.match.Status {
				t.Errorf("Incorrect match status, got %d, expected %d",
					matchData.Status, tt.match.Status)
			}
			if tt.isCancel {
				if matchData.Active {
					t.Errorf("Incorrect match active flag, got %v, expected false",
						matchData.Active)
				}
				trade := tt.match.Taker.Trade()
				if trade != nil {
					if matchData.TakerSell != trade.Sell {
						t.Errorf("expected takerSell = %v, got %v", trade.Sell, matchData.TakerSell)
					}
					if matchData.BaseRate != tt.match.FeeRateBase {
						t.Errorf("expected base fee rate %d, got %d", tt.match.FeeRateBase, matchData.BaseRate)
					}
				} else {
					if matchData.BaseRate != 0 {
						t.Errorf("cancel order should have 0 base fee rate, got %d", matchData.BaseRate)
					}
					if matchData.QuoteRate != 0 {
						t.Errorf("cancel order should have 0 quote fee rate, got %d", matchData.QuoteRate)
					}
					if matchData.TakerSell {
						t.Errorf("cancel order should have false for takerSell")
					}
				}
				if matchData.TakerAddr != "" {
					t.Errorf("Expected empty taker address for cancel match, got %v", matchData.TakerAddr)
				}
				if matchData.MakerAddr != "" {
					t.Errorf("Expected empty maker address for cancel match, got %v", matchData.MakerAddr)
				}
			}
		})
	}
}

func TestSetSwapData(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	// Make a perfect 1 lot match.
	limitBuyStanding := newLimitOrder(false, 4500000, 1, order.StandingTiF, 0)
	limitSellImmediate := newLimitOrder(true, 4490000, 1, order.ImmediateTiF, 10)

	epochID := order.EpochID{132412341, 1000}
	matchA := newMatch(limitBuyStanding, limitSellImmediate, limitSellImmediate.Quantity, epochID)
	matchID := matchA.ID()

	base, quote := limitBuyStanding.Base(), limitBuyStanding.Quote()

	checkMatch := func(wantStatus order.MatchStatus, wantActive bool) error {
		matchData, err := archie.MatchByID(matchID, base, quote)
		if err != nil {
			return err
		}
		if matchData.ID != matchID {
			return fmt.Errorf("Retrieved match with ID %v, expected %v", matchData.ID, matchID)
		}
		if matchData.Status != wantStatus {
			return fmt.Errorf("Incorrect match status, got %d, expected %d",
				matchData.Status, wantStatus)
		}
		if matchData.Active != wantActive {
			return fmt.Errorf("Incorrect match active flag, got %v, expected %v",
				matchData.Active, wantActive)
		}
		return nil
	}

	err := insertMatchForTest(matchA)
	if err != nil {
		t.Errorf("insertMatchForTest() failed: %v", err)
	}

	if err = checkMatch(order.NewlyMatched, true); err != nil {
		t.Fatal(err)
	}

	mid := db.MarketMatchID{
		MatchID: matchA.ID(),
		Base:    base,
		Quote:   quote,
	}

	// Match Ack Sig A (maker's match ack sig)
	sigMakerMatch := randomBytes(73)
	err = archie.saveMatchAck(archie.db, meshevents.MatchAckRecord{
		MatchID: mid.MatchID,
		Base:    mid.Base,
		Quote:   mid.Quote,
		Maker:   true,
		Sig:     sigMakerMatch,
		Address: "maker-swap-addr",
	})
	if err != nil {
		t.Fatal(err)
	}
	status, swapData, err := archie.SwapData(mid)
	if err != nil {
		t.Fatal(err)
	}
	if status != order.NewlyMatched {
		t.Errorf("Got status %v, expected %v", status, order.NewlyMatched)
	}
	if !bytes.Equal(swapData.SigMatchAckMaker, sigMakerMatch) {
		t.Fatalf("SigMatchAckMaker incorrect. got %v, expected %v",
			swapData.SigMatchAckMaker, sigMakerMatch)
	}

	// Match Ack Sig B (taker's match ack sig)
	sigTakerMatch := randomBytes(73)
	err = archie.saveMatchAck(archie.db, meshevents.MatchAckRecord{
		MatchID: mid.MatchID,
		Base:    mid.Base,
		Quote:   mid.Quote,
		Maker:   false,
		Sig:     sigTakerMatch,
		Address: "taker-swap-addr",
	})
	if err != nil {
		t.Fatal(err)
	}
	status, swapData, err = archie.SwapData(mid)
	if err != nil {
		t.Fatal(err)
	}
	if status != order.NewlyMatched {
		t.Errorf("Got status %v, expected %v", status, order.NewlyMatched)
	}
	if !bytes.Equal(swapData.SigMatchAckTaker, sigTakerMatch) {
		t.Fatalf("SigMatchAckTaker incorrect. got %v, expected %v",
			swapData.SigMatchAckTaker, sigTakerMatch)
	}

	// Contract A
	contractA := randomBytes(128)
	coinIDA := randomBytes(36)
	contractATime := int64(1234)
	err = saveContractForTest(mid, true, contractA, coinIDA, contractATime)
	if err != nil {
		t.Fatal(err)
	}

	status, swapData, err = archie.SwapData(mid)
	if err != nil {
		t.Fatal(err)
	}
	if status != order.MakerSwapCast {
		t.Errorf("Got status %v, expected %v", status, order.MakerSwapCast)
	}
	if !bytes.Equal(swapData.ContractA, contractA) {
		t.Fatalf("ContractA incorrect. got %v, expected %v",
			swapData.ContractA, contractA)
	}
	if !bytes.Equal(swapData.ContractACoinID, coinIDA) {
		t.Fatalf("ContractACoinID incorrect. got %v, expected %v",
			swapData.ContractACoinID, coinIDA)
	}
	if swapData.ContractATime != contractATime {
		t.Fatalf("ContractATime incorrect. got %d, expected %d",
			swapData.ContractATime, contractATime)
	}

	// Party B's signature for acknowledgement of contract A
	auditSigB := randomBytes(73)
	if err = archie.updateMatchStmtWithExecutor(archie.db, mid, internal.SetParticipantContractAuditSig, mid.MatchID, auditSigB); err != nil {
		t.Fatal(err)
	}

	status, swapData, err = archie.SwapData(mid)
	if err != nil {
		t.Fatal(err)
	}
	if status != order.MakerSwapCast {
		t.Errorf("Got status %v, expected %v", status, order.MakerSwapCast)
	}
	if !bytes.Equal(swapData.ContractAAckSig, auditSigB) {
		t.Fatalf("ContractAAckSig incorrect. got %v, expected %v",
			swapData.ContractAAckSig, auditSigB)
	}

	// Contract B
	contractB := randomBytes(128)
	coinIDB := randomBytes(36)
	contractBTime := int64(1235)
	err = saveContractForTest(mid, false, contractB, coinIDB, contractBTime)
	if err != nil {
		t.Fatal(err)
	}

	status, swapData, err = archie.SwapData(mid)
	if err != nil {
		t.Fatal(err)
	}
	if status != order.TakerSwapCast {
		t.Errorf("Got status %v, expected %v", status, order.TakerSwapCast)
	}
	if !bytes.Equal(swapData.ContractB, contractB) {
		t.Fatalf("ContractB incorrect. got %v, expected %v",
			swapData.ContractB, contractB)
	}
	if !bytes.Equal(swapData.ContractBCoinID, coinIDB) {
		t.Fatalf("ContractBCoinID incorrect. got %v, expected %v",
			swapData.ContractBCoinID, coinIDB)
	}
	if swapData.ContractBTime != contractBTime {
		t.Fatalf("ContractBTime incorrect. got %d, expected %d",
			swapData.ContractBTime, contractBTime)
	}

	// Party A's signature for acknowledgement of contract B
	auditSigA := randomBytes(73)
	if err = archie.updateMatchStmtWithExecutor(archie.db, mid, internal.SetInitiatorContractAuditSig, mid.MatchID, auditSigA); err != nil {
		t.Fatal(err)
	}

	status, swapData, err = archie.SwapData(mid)
	if err != nil {
		t.Fatal(err)
	}
	if status != order.TakerSwapCast {
		t.Errorf("Got status %v, expected %v", status, order.TakerSwapCast)
	}
	if !bytes.Equal(swapData.ContractBAckSig, auditSigA) {
		t.Fatalf("ContractBAckSig incorrect. got %v, expected %v",
			swapData.ContractBAckSig, auditSigB)
	}

	// Redeem A
	redeemCoinIDA := randomBytes(36)
	secret := randomBytes(72)
	redeemATime := int64(1234)
	err = saveRedeemForTest(mid, true, redeemCoinIDA, secret, redeemATime)
	if err != nil {
		t.Fatal(err)
	}
	status, swapData, err = archie.SwapData(mid)
	if err != nil {
		t.Fatal(err)
	}
	if status != order.MakerRedeemed {
		t.Errorf("Got status %v, expected %v", status, order.MakerRedeemed)
	}
	if !bytes.Equal(swapData.RedeemACoinID, redeemCoinIDA) {
		t.Fatalf("RedeemACoinID incorrect. got %v, expected %v",
			swapData.RedeemACoinID, redeemCoinIDA)
	}
	if !bytes.Equal(swapData.RedeemASecret, secret) {
		t.Fatalf("RedeemASecret incorrect. got %v, expected %v",
			swapData.RedeemASecret, secret)
	}
	if swapData.RedeemATime != redeemATime {
		t.Fatalf("RedeemATime incorrect. got %d, expected %d",
			swapData.RedeemATime, redeemATime)
	}

	// Party B's signature for acknowledgement of A's redemption
	redeemAckSigB := randomBytes(73)
	if err = archie.updateMatchStmtWithExecutor(archie.db, mid, internal.SetParticipantRedeemAckSig, mid.MatchID, redeemAckSigB); err != nil {
		t.Fatal(err)
	}

	status, swapData, err = archie.SwapData(mid)
	if err != nil {
		t.Fatal(err)
	}
	if status != order.MakerRedeemed {
		t.Errorf("Got status %v, expected %v", status, order.MakerRedeemed)
	}
	if !bytes.Equal(swapData.RedeemAAckSig, redeemAckSigB) {
		t.Fatalf("RedeemAAckSig incorrect. got %v, expected %v",
			swapData.RedeemAAckSig, redeemAckSigB)
	}

	// Redeem B
	redeemCoinIDB := randomBytes(36)
	redeemBTime := int64(1234)
	err = saveRedeemForTest(mid, false, redeemCoinIDB, nil, redeemBTime)
	if err != nil {
		t.Fatal(err)
	}

	status, swapData, err = archie.SwapData(mid)
	if err != nil {
		t.Fatal(err)
	}
	if status != order.MatchComplete {
		t.Errorf("Got status %v, expected %v", status, order.MatchComplete)
	}
	if !bytes.Equal(swapData.RedeemBCoinID, redeemCoinIDB) {
		t.Fatalf("RedeemBCoinID incorrect. got %v, expected %v",
			swapData.RedeemBCoinID, redeemCoinIDB)
	}
	if swapData.RedeemBTime != redeemBTime {
		t.Fatalf("RedeemBTime incorrect. got %d, expected %d",
			swapData.RedeemBTime, redeemBTime)
	}

	// Check active flag via MatchByID.
	if err = checkMatch(order.MatchComplete, false); err != nil {
		t.Fatal(err)
	}
}

func TestApplyMatchAcksRecordedEvent(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	ctx := context.Background()
	sharedUser := randomAccountID()
	makerAckPair := generateMatch(t, order.NewlyMatched, true, sharedUser, randomAccountID())
	takerAckPair := generateMatch(t, order.NewlyMatched, true, randomAccountID(), sharedUser)
	makerAckMID := testMarketMatchID(makerAckPair.match)
	takerAckMID := testMarketMatchID(takerAckPair.match)
	event := &meshevents.MatchAcksRecordedEvent{Records: []meshevents.MatchAckRecord{
		{
			MatchID: makerAckMID.MatchID,
			Base:    makerAckMID.Base,
			Quote:   makerAckMID.Quote,
			Maker:   true,
			Sig:     []byte("maker-match-sig"),
			Address: "maker-swap-addr",
		},
		{
			MatchID: takerAckMID.MatchID,
			Base:    takerAckMID.Base,
			Quote:   takerAckMID.Quote,
			Maker:   false,
			Sig:     []byte("taker-match-sig"),
			Address: "taker-swap-addr",
		},
	}}

	// Record one maker acknowledgement and one taker acknowledgement together.
	payload := []byte("match-acks-event")
	tip := testEventApplyTip(t, nil, 1, meshevents.EventKindMatchAcksRecorded, payload, event)
	log, err := archie.ApplyMatchAcksRecordedEvent(ctx, &db.EventLogMeta{Event: payload}, event)
	if err != nil {
		t.Fatalf("ApplyMatchAcksRecordedEvent error: %v", err)
	}
	requireEventApplyLog(t, log, 1, meshevents.EventKindMatchAcksRecorded, payload, tip, event)
	assertEventLogFrontier(t, ctx, 1, tip)

	checkStored := func(wantRecords []meshevents.MatchAckRecord) {
		t.Helper()
		for _, want := range wantRecords {
			mid := db.MarketMatchID{MatchID: want.MatchID, Base: want.Base, Quote: want.Quote}
			_, data, err := archie.SwapData(mid)
			if err != nil {
				t.Fatal(err)
			}
			sig, address := data.SigMatchAckTaker, data.TakerSwapAddr
			otherSig, otherAddress := data.SigMatchAckMaker, data.MakerSwapAddr
			if want.Maker {
				sig, address = data.SigMatchAckMaker, data.MakerSwapAddr
				otherSig, otherAddress = data.SigMatchAckTaker, data.TakerSwapAddr
			}
			if !bytes.Equal(sig, want.Sig) || address != want.Address {
				t.Fatalf("match %v maker=%v: stored ack = %x / %q, want %x / %q",
					want.MatchID, want.Maker, sig, address, want.Sig, want.Address)
			}
			if len(otherSig) != 0 || otherAddress != "" {
				t.Fatalf("match %v: acknowledgement changed the opposite side: %+v", want.MatchID, data)
			}
		}
	}
	checkStored(event.Records)

	// Repeated acknowledgements refresh both signatures but retain the addresses.
	reackEvent := &meshevents.MatchAcksRecordedEvent{Records: slices.Clone(event.Records)}
	for i := range reackEvent.Records {
		reackEvent.Records[i].Sig = []byte(fmt.Sprintf("replacement-sig-%d", i))
		reackEvent.Records[i].Address = fmt.Sprintf("replacement-address-%d", i)
	}
	reackPayload := []byte("match-acks-reack-event")
	tip2 := testEventApplyTip(t, tip, 2, meshevents.EventKindMatchAcksRecorded, reackPayload, reackEvent)
	log2, err := archie.ApplyMatchAcksRecordedEvent(ctx, &db.EventLogMeta{
		Seq:             2,
		Event:           reackPayload,
		ExpectedTipHash: tip2,
	}, reackEvent)
	if err != nil {
		t.Fatalf("ApplyMatchAcksRecordedEvent re-ack error: %v", err)
	}
	requireEventApplyLog(t, log2, 2, meshevents.EventKindMatchAcksRecorded, reackPayload, tip2, reackEvent)
	wantRecords := slices.Clone(event.Records)
	for i := range wantRecords {
		wantRecords[i].Sig = reackEvent.Records[i].Sig
	}
	checkStored(wantRecords)
	assertEventLogFrontier(t, ctx, 2, tip2)
}

func TestApplySwapContractRecordedEvent(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	ctx := context.Background()
	pair := generateMatch(t, order.NewlyMatched, true, randomAccountID(), randomAccountID())
	mid := testMarketMatchID(pair.match)
	makerContract := &meshevents.SwapContractRecordedEvent{
		MatchID:  mid.MatchID,
		Base:     mid.Base,
		Quote:    mid.Quote,
		Maker:    true,
		Status:   order.MakerSwapCast,
		Contract: []byte("maker-contract"),
		CoinID:   []byte("maker-coin"),
		SwapTime: 1670000000000,
	}

	event := []byte("swap-contract-maker-event")
	tip := testEventApplyTip(t, nil, 1, meshevents.EventKindSwapContractRecorded, event, makerContract)
	log, err := archie.ApplySwapContractRecordedEvent(ctx, &db.EventLogMeta{Event: event}, makerContract)
	if err != nil {
		t.Fatalf("ApplySwapContractRecordedEvent maker error: %v", err)
	}
	requireEventApplyLog(t, log, 1, meshevents.EventKindSwapContractRecorded, event, tip, makerContract)
	status, swapData, err := archie.SwapData(mid)
	if err != nil {
		t.Fatalf("SwapData maker error: %v", err)
	}
	if status != order.MakerSwapCast {
		t.Fatalf("match status = %v, want MakerSwapCast", status)
	}
	if !bytes.Equal(swapData.ContractA, makerContract.Contract) ||
		!bytes.Equal(swapData.ContractACoinID, makerContract.CoinID) || swapData.ContractATime != makerContract.SwapTime {
		t.Fatalf("maker swap data = %+v, want contract %+v", swapData, makerContract)
	}

	takerContract := &meshevents.SwapContractRecordedEvent{
		Status:   order.TakerSwapCast,
		MatchID:  mid.MatchID,
		Base:     mid.Base,
		Quote:    mid.Quote,
		Contract: []byte("taker-contract"),
		CoinID:   []byte("taker-coin"),
		SwapTime: 1670000001111,
	}

	takerEvent := []byte("swap-contract-taker-event")
	takerTip := testEventApplyTip(t, tip, 2, meshevents.EventKindSwapContractRecorded, takerEvent, takerContract)
	log, err = archie.ApplySwapContractRecordedEvent(ctx, &db.EventLogMeta{
		Seq:             2,
		Event:           takerEvent,
		ExpectedTipHash: takerTip,
	}, takerContract)
	if err != nil {
		t.Fatalf("ApplySwapContractRecordedEvent taker error: %v", err)
	}
	requireEventApplyLog(t, log, 2, meshevents.EventKindSwapContractRecorded, takerEvent, takerTip, takerContract)
	status, swapData, err = archie.SwapData(mid)
	if err != nil {
		t.Fatalf("SwapData taker error: %v", err)
	}
	if status != order.TakerSwapCast {
		t.Fatalf("match status = %v, want TakerSwapCast", status)
	}
	if !bytes.Equal(swapData.ContractB, takerContract.Contract) ||
		!bytes.Equal(swapData.ContractBCoinID, takerContract.CoinID) || swapData.ContractBTime != takerContract.SwapTime {
		t.Fatalf("taker swap data = %+v, want contract %+v", swapData, takerContract)
	}
	if !bytes.Equal(swapData.ContractA, makerContract.Contract) ||
		!bytes.Equal(swapData.ContractACoinID, makerContract.CoinID) || swapData.ContractATime != makerContract.SwapTime {
		t.Fatalf("recording taker contract changed maker data: %+v", swapData)
	}
}

func TestApplyAuditAckRecordedEvent(t *testing.T) {
	tests := []struct {
		name  string
		maker bool
	}{
		{name: "maker", maker: true},
		{name: "taker"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := cleanTables(archie.db); err != nil {
				t.Fatalf("cleanTables: %v", err)
			}
			pair := generateMatch(t, order.TakerSwapCast, true, randomAccountID(), randomAccountID())
			mid := testMarketMatchID(pair.match)
			event := &meshevents.AuditAckRecordedEvent{
				MatchID: mid.MatchID,
				Base:    mid.Base,
				Quote:   mid.Quote,
				Maker:   tt.maker,
				Sig:     []byte("audit ack"),
			}
			payload, err := event.Encode()
			if err != nil {
				t.Fatal(err)
			}
			tip := testEventApplyTip(t, nil, 1, event.Kind(), payload, event)
			logEntry, err := archie.ApplyAuditAckRecordedEvent(context.Background(), &db.EventLogMeta{Event: payload}, event)
			if err != nil {
				t.Fatalf("ApplyAuditAckRecordedEvent: %v", err)
			}
			requireEventApplyLog(t, logEntry, 1, event.Kind(), payload, tip, event)
			_, swapData, err := archie.SwapData(mid)
			if err != nil {
				t.Fatalf("SwapData: %v", err)
			}
			// The maker acknowledges contract B; the taker acknowledges contract A.
			var wantAckA, wantAckB []byte
			if tt.maker {
				wantAckB = event.Sig
			} else {
				wantAckA = event.Sig
			}
			if !bytes.Equal(swapData.ContractAAckSig, wantAckA) || !bytes.Equal(swapData.ContractBAckSig, wantAckB) {
				t.Fatalf("contract A/B audit signatures = %x/%x, want %x/%x",
					swapData.ContractAAckSig, swapData.ContractBAckSig, wantAckA, wantAckB)
			}
		})
	}
}

func TestApplySwapRedemptionRecordedEvent(t *testing.T) {
	ctx := context.Background()
	policy := &db.ReputationOutcomePolicy{MatchLimit: 10, OrderLimit: 10}
	const redeemTime = int64(1670000003333)

	// Related matches only need the fields used by the unsettled-match query.
	addRelatedMatch := func(t *testing.T, pair *matchPair, maker bool, status order.MatchStatus, inactive bool) {
		t.Helper()
		makerOrder, takerOrder := pair.match.Maker, pair.match.Taker
		if maker {
			takerOrder = newLimitOrder(true, 4490000, 1, order.ImmediateTiF, 10)
		} else {
			makerOrder = newLimitOrder(false, 4500000, 1, order.StandingTiF, 0)
		}
		match := newMatch(makerOrder, takerOrder, pair.match.Quantity, pair.match.Epoch)
		match.Status = status
		if err := insertMatchForTest(match); err != nil {
			t.Fatalf("InsertMatch: %v", err)
		}
		if inactive {
			if err := setMatchInactiveForTest(testMarketMatchID(match)); err != nil {
				t.Fatalf("SetMatchInactive: %v", err)
			}
		}
	}

	tests := []struct {
		name          string
		maker         bool
		status        order.MatchStatus
		booked        bool
		sameUser      bool
		otherStatus   order.MatchStatus // Zero means no related match.
		otherInactive bool
		wantComplete  bool
		wantErr       bool
	}{
		{name: "maker completes order", maker: true, status: order.TakerSwapCast, wantComplete: true},
		{name: "taker completes order", status: order.MakerRedeemed, wantComplete: true},
		{name: "maker requires taker swap", maker: true, status: order.MakerSwapCast, wantErr: true},
		{name: "taker requires maker redemption", status: order.TakerSwapCast, wantErr: true},
		{name: "booked maker order", maker: true, status: order.TakerSwapCast, booked: true},
		{name: "unsettled maker match", maker: true, status: order.TakerSwapCast, otherStatus: order.TakerSwapCast},
		{name: "unsettled taker match", status: order.MakerRedeemed, otherStatus: order.TakerSwapCast},
		{name: "other taker still owes redemption", status: order.MakerRedeemed, otherStatus: order.MakerRedeemed},
		{name: "other maker already redeemed", maker: true, status: order.TakerSwapCast, otherStatus: order.MakerRedeemed, wantComplete: true},
		{name: "other taker already redeemed", status: order.MakerRedeemed, otherStatus: order.MatchComplete, otherInactive: true, wantComplete: true},
		{name: "other match failed", maker: true, status: order.TakerSwapCast, otherStatus: order.TakerSwapCast, otherInactive: true, wantComplete: true},
		{name: "self match has no match credit", maker: true, status: order.TakerSwapCast, sameUser: true, wantComplete: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := cleanTables(archie.db); err != nil {
				t.Fatal(err)
			}
			maker, taker := randomAccountID(), randomAccountID()
			if tt.sameUser {
				taker = maker
			}
			makerStatus := order.OrderStatusExecuted
			if tt.booked {
				makerStatus = order.OrderStatusBooked
			}
			pair := generateMatchWithOrderStatuses(t, tt.status, true, maker, taker, makerStatus, order.OrderStatusExecuted)
			if tt.otherStatus != 0 {
				addRelatedMatch(t, pair, tt.maker, tt.otherStatus, tt.otherInactive)
			}
			mid := testMarketMatchID(pair.match)
			actor, counterparty, actorOrder := taker, maker, pair.match.Taker.ID()
			nextStatus := order.MatchComplete
			if tt.maker {
				actor, counterparty, actorOrder = maker, taker, pair.match.Maker.ID()
				nextStatus = order.MakerRedeemed
			}
			event := &meshevents.SwapRedemptionRecordedEvent{
				MatchID: mid.MatchID, Base: mid.Base, Quote: mid.Quote, Maker: tt.maker,
				Status: nextStatus, CoinID: []byte("redeem coin"), RedeemTime: redeemTime,
			}
			if tt.maker {
				event.Secret = []byte("secret")
			}
			payload, err := event.Encode()
			if err != nil {
				t.Fatal(err)
			}
			beforeStatus, beforeData, err := archie.SwapData(mid)
			if err != nil {
				t.Fatal(err)
			}
			logEntry, err := archie.ApplySwapRedemptionRecordedEvent(ctx, &db.EventLogMeta{Event: payload}, policy, event)
			if (err != nil) != tt.wantErr {
				t.Fatalf("apply error = %v, want error %v", err, tt.wantErr)
			}
			status, data, err := archie.SwapData(mid)
			if err != nil {
				t.Fatal(err)
			}
			if tt.wantErr {
				if status != beforeStatus || !reflect.DeepEqual(data, beforeData) {
					t.Fatalf("rejected event changed swap data: %v, %+v", status, data)
				}
				assertEventLogFrontier(t, ctx, 0, nil)
			} else {
				tip := testEventApplyTip(t, nil, 1, event.Kind(), payload, event)
				requireEventApplyLog(t, logEntry, 1, event.Kind(), payload, tip, event)
				if status != nextStatus {
					t.Fatalf("status = %v, want %v", status, nextStatus)
				}
				if tt.maker {
					if !bytes.Equal(data.RedeemACoinID, event.CoinID) || !bytes.Equal(data.RedeemASecret, event.Secret) || data.RedeemATime != redeemTime {
						t.Fatalf("wrong maker redemption: %+v", data)
					}
				} else if !bytes.Equal(data.RedeemBCoinID, event.CoinID) || data.RedeemBTime != redeemTime {
					t.Fatalf("wrong taker redemption: %+v", data)
				}
			}

			matchData, err := archie.MatchByID(mid.MatchID, mid.Base, mid.Quote)
			if err != nil {
				t.Fatal(err)
			}
			wantActive := tt.wantErr || tt.maker
			if matchData.Active != wantActive {
				t.Fatalf("match active = %v, want %v", matchData.Active, wantActive)
			}

			users := []account.AccountID{actor}
			if actor != counterparty {
				users = append(users, counterparty)
			}
			for _, user := range users {
				wantMatches, wantOrders := 0, 0
				if user == actor && !tt.wantErr {
					if !tt.sameUser {
						wantMatches = 1
					}
					if tt.wantComplete {
						wantOrders = 1
					}
				}
				completed, times, err := archie.CompletedUserOrders(user, 10)
				if err != nil {
					t.Fatal(err)
				}
				if len(completed) != wantOrders || (wantOrders == 1 && (completed[0] != actorOrder || times[0] != redeemTime)) {
					t.Fatalf("user %v completed orders = %v/%v, want %d completion at %d", user, completed, times, wantOrders, redeemTime)
				}
				_, matches, orders, err := archie.GetUserReputationData(ctx, user, 10, 10, 10)
				if err != nil {
					t.Fatal(err)
				}
				if len(matches) != wantMatches || len(orders) != wantOrders {
					t.Fatalf("user %v reputation matches/orders = %d/%d, want %d/%d", user, len(matches), len(orders), wantMatches, wantOrders)
				}
				if wantMatches == 1 && (matches[0].MatchID != mid.MatchID || matches[0].MatchOutcome != db.OutcomeSwapSuccess) {
					t.Fatalf("wrong match outcome: %+v", matches[0])
				}
				if wantOrders == 1 && (orders[0].OrderID != actorOrder || orders[0].Canceled) {
					t.Fatalf("wrong order outcome: %+v", orders[0])
				}
			}
		})
	}
}

func TestApplyRedemptionAckRecordedEvent(t *testing.T) {
	for _, tt := range []struct {
		name  string
		maker bool
	}{
		{name: "taker signature"},
		{name: "maker event log only", maker: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := cleanTables(archie.db); err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			status := order.MakerRedeemed
			if tt.maker {
				status = order.MatchComplete
			}
			pair := generateMatch(t, status, !tt.maker, randomAccountID(), randomAccountID())
			mid := testMarketMatchID(pair.match)
			if err := archie.updateMatchStmtWithExecutor(archie.db, mid, internal.SetParticipantRedeemAckSig, mid.MatchID, []byte("previous taker ack")); err != nil {
				t.Fatal(err)
			}
			_, wantSwap, err := archie.SwapData(mid)
			if err != nil {
				t.Fatal(err)
			}
			event := &meshevents.RedemptionAckRecordedEvent{
				MatchID: mid.MatchID,
				Base:    mid.Base,
				Quote:   mid.Quote,
				Maker:   tt.maker,
				Sig:     []byte("new redemption ack"),
			}
			payload, err := event.Encode()
			if err != nil {
				t.Fatal(err)
			}
			entry, err := archie.ApplyRedemptionAckRecordedEvent(ctx, &db.EventLogMeta{Event: payload}, event)
			if err != nil {
				t.Fatal(err)
			}
			tip := testEventApplyTip(t, nil, 1, event.Kind(), payload, event)
			requireEventApplyLog(t, entry, 1, event.Kind(), payload, tip, event)
			if !tt.maker {
				wantSwap.RedeemAAckSig = event.Sig
			}
			gotStatus, gotSwap, err := archie.SwapData(mid)
			if err != nil {
				t.Fatal(err)
			}
			if gotStatus != status || !reflect.DeepEqual(gotSwap, wantSwap) {
				t.Fatalf("swap data = %v/%+v, want %v/%+v", gotStatus, gotSwap, status, wantSwap)
			}
			match, err := archie.MatchByID(mid.MatchID, mid.Base, mid.Quote)
			if err != nil {
				t.Fatal(err)
			}
			if match.Active != !tt.maker {
				t.Fatalf("match active = %v, want %v", match.Active, !tt.maker)
			}
		})
	}
}

func TestApplyMatchFailedEvent(t *testing.T) {
	ctx := context.Background()
	failTime := time.UnixMilli(1670000000123).UTC()
	policy := &db.ReputationOutcomePolicy{MatchLimit: 10, OrderLimit: 10}
	type orderResult struct {
		status    order.OrderStatus
		completed bool
		penalty   db.Outcome
	}
	tests := []struct {
		name                        string
		status                      order.MatchStatus
		fault                       meshevents.MatchFailureFault
		wrongStatus                 bool
		makerBooked                 bool
		sameUser, forgiven, wantErr bool
		maker, taker                orderResult
	}{
		{
			name: "maker failure revokes booked order", status: order.NewlyMatched,
			fault: meshevents.MatchFailureMakerFault, makerBooked: true,
			maker: orderResult{status: order.OrderStatusRevoked, penalty: db.OutcomeNoSwapAsMaker},
			taker: orderResult{status: order.OrderStatusExecuted, completed: true},
		},
		{
			name: "taker did not provide address", status: order.NewlyMatched,
			fault: meshevents.MatchFailureTakerFault,
			maker: orderResult{status: order.OrderStatusExecuted, completed: true},
			taker: orderResult{status: order.OrderStatusExecuted, penalty: db.OutcomeNoAddrAsTaker},
		},
		{
			name: "taker did not swap", status: order.MakerSwapCast,
			fault: meshevents.MatchFailureTakerFault,
			maker: orderResult{status: order.OrderStatusExecuted, completed: true},
			taker: orderResult{status: order.OrderStatusExecuted, penalty: db.OutcomeNoSwapAsTaker},
		},
		{
			name: "maker did not redeem", status: order.TakerSwapCast,
			fault: meshevents.MatchFailureMakerFault,
			maker: orderResult{status: order.OrderStatusExecuted, penalty: db.OutcomeNoRedeemAsMaker},
			taker: orderResult{status: order.OrderStatusExecuted, completed: true},
		},
		{
			name: "taker did not redeem", status: order.MakerRedeemed,
			fault: meshevents.MatchFailureTakerFault,
			maker: orderResult{status: order.OrderStatusExecuted},
			taker: orderResult{status: order.OrderStatusExecuted, penalty: db.OutcomeNoRedeemAsTaker},
		},
		{
			name: "no fault before swaps", status: order.NewlyMatched,
			fault: meshevents.MatchFailureNoUserFault, forgiven: true,
			maker: orderResult{status: order.OrderStatusExecuted, completed: true},
			taker: orderResult{status: order.OrderStatusExecuted, completed: true},
		},
		{
			name: "no fault after maker swap", status: order.MakerSwapCast,
			fault: meshevents.MatchFailureNoUserFault, forgiven: true,
			maker: orderResult{status: order.OrderStatusExecuted, completed: true},
			taker: orderResult{status: order.OrderStatusExecuted, completed: true},
		},
		{
			name: "no fault after taker swap", status: order.TakerSwapCast,
			fault: meshevents.MatchFailureNoUserFault, forgiven: true,
			maker: orderResult{status: order.OrderStatusExecuted, completed: true},
			taker: orderResult{status: order.OrderStatusExecuted, completed: true},
		},
		{
			name: "no fault after maker redemption", status: order.MakerRedeemed,
			fault: meshevents.MatchFailureNoUserFault, forgiven: true,
			maker: orderResult{status: order.OrderStatusExecuted},
			taker: orderResult{status: order.OrderStatusExecuted, completed: true},
		},
		{
			name: "same account has no match penalty", status: order.MakerSwapCast,
			fault: meshevents.MatchFailureTakerFault, sameUser: true,
			maker: orderResult{status: order.OrderStatusExecuted, completed: true},
			taker: orderResult{status: order.OrderStatusExecuted},
		},
		{
			name: "invalid fault", status: order.NewlyMatched,
			fault: 0, wantErr: true,
			maker: orderResult{status: order.OrderStatusExecuted},
			taker: orderResult{status: order.OrderStatusExecuted},
		},
		{
			name: "status mismatch", status: order.NewlyMatched, wrongStatus: true,
			fault: meshevents.MatchFailureTakerFault, wantErr: true,
			maker: orderResult{status: order.OrderStatusExecuted},
			taker: orderResult{status: order.OrderStatusExecuted},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := cleanTables(archie.db); err != nil {
				t.Fatal(err)
			}
			maker, taker := randomAccountID(), randomAccountID()
			if tt.sameUser {
				taker = maker
			}
			makerStatus, takerStatus := order.OrderStatusExecuted, order.OrderStatusExecuted
			if tt.makerBooked {
				makerStatus = order.OrderStatusBooked
			}
			storedStatus := tt.status
			if tt.wrongStatus {
				storedStatus = order.MakerSwapCast
			}
			pair := generateMatchWithOrderStatuses(t, storedStatus, true, maker, taker, makerStatus, takerStatus)
			mid := db.MatchID(pair.match)
			event := &meshevents.MatchFailedEvent{
				MatchID: mid.MatchID, Base: mid.Base, Quote: mid.Quote,
				FailTime: failTime.UnixMilli(), Status: tt.status, Fault: tt.fault,
			}
			payload, err := event.Encode()
			if err != nil {
				t.Fatal(err)
			}
			entry, err := archie.ApplyMatchFailedEvent(ctx, &db.EventLogMeta{Event: payload}, policy, event)
			if (err != nil) != tt.wantErr {
				t.Fatalf("apply error = %v, want error %v", err, tt.wantErr)
			}
			if tt.wantErr {
				assertEventLogFrontier(t, ctx, 0, nil)
			} else {
				tip := testEventApplyTip(t, nil, 1, event.Kind(), payload, event)
				requireEventApplyLog(t, entry, 1, event.Kind(), payload, tip, event)
			}
			match, err := archie.MatchByID(mid.MatchID, mid.Base, mid.Quote)
			if err != nil {
				t.Fatal(err)
			}
			if match.Active != tt.wantErr || match.Status != storedStatus {
				t.Fatalf("match active/status = %v/%v, want %v/%v", match.Active, match.Status, tt.wantErr, storedStatus)
			}
			requireMatchForgiven(t, ctx, mid, tt.forgiven)

			sides := []struct {
				order order.Order
				want  orderResult
			}{{pair.match.Maker, tt.maker}, {pair.match.Taker, tt.taker}}
			for _, side := range sides {
				ord, want := side.order, side.want
				_, status, err := archie.Order(ord.ID(), mid.Base, mid.Quote)
				if err != nil {
					t.Fatal(err)
				}
				if status != want.status {
					t.Fatalf("order %v status = %v, want %v", ord.ID(), status, want.status)
				}

				// Both sides can belong to the same account. Gather that account's
				// expected outcomes before checking its reputation and completions.
				var wantPenalties []db.Outcome
				var wantOrders, wantCompleted []order.OrderID
				for _, other := range sides {
					if other.order.User() != ord.User() {
						continue
					}
					if other.want.penalty != db.OutcomeInvalid {
						wantPenalties = append(wantPenalties, other.want.penalty)
					}
					if other.want.completed {
						wantCompleted = append(wantCompleted, other.order.ID())
						wantOrders = append(wantOrders, other.order.ID())
					}
					if other.want.status == order.OrderStatusRevoked {
						cancel := makePseudoCancel(other.order.ID(), ord.User(), mid.Base, mid.Quote, failTime)
						wantOrders = append(wantOrders, cancel.ID())
					}
				}
				_, matches, orders, err := archie.GetUserReputationData(ctx, ord.User(), 10, 10, 10)
				if err != nil {
					t.Fatal(err)
				}
				if len(matches) != len(wantPenalties) {
					t.Fatalf("account %v match outcomes = %+v, want %v", ord.User(), matches, wantPenalties)
				}
				for i, outcome := range matches {
					if outcome.MatchOutcome != wantPenalties[i] || outcome.MatchID != mid.MatchID {
						t.Fatalf("unexpected match outcome: %+v", outcome)
					}
				}
				if len(orders) != len(wantOrders) {
					t.Fatalf("account %v order outcomes = %+v, want %v", ord.User(), orders, wantOrders)
				}
				for _, outcome := range orders {
					if !slices.Contains(wantOrders, outcome.OrderID) || outcome.Canceled {
						t.Fatalf("unexpected order outcome: %+v", outcome)
					}
				}
				completed, times, err := archie.CompletedUserOrders(ord.User(), 10)
				if err != nil {
					t.Fatal(err)
				}
				if len(completed) != len(wantCompleted) {
					t.Fatalf("account %v completed orders = %v, want %v", ord.User(), completed, wantCompleted)
				}
				for i, oid := range completed {
					if !slices.Contains(wantCompleted, oid) || times[i] != failTime.UnixMilli() {
						t.Fatalf("unexpected completion %v at %d", oid, times[i])
					}
				}
			}
		})
	}
}

func TestMatchFailureCompletesOrderAfterLastMatch(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	failTime := time.UnixMilli(1670000000123).UTC()
	policy := &db.ReputationOutcomePolicy{MatchLimit: 10, OrderLimit: 10}
	epoch := order.EpochID{Idx: 132412341, Dur: 1000}
	maker := newLimitOrder(false, 4500000, 2, order.StandingTiF, 0)
	takers := []*order.LimitOrder{
		newLimitOrder(true, 4490000, 1, order.ImmediateTiF, 10),
		newLimitOrder(true, 4480000, 1, order.ImmediateTiF, 20),
	}
	for _, ord := range []*order.LimitOrder{maker, takers[0], takers[1]} {
		ord.P.AccountID = randomAccountID()
		if err := storeOrderForTest(archie, ord, int64(epoch.Idx), int64(epoch.Dur), order.OrderStatusExecuted); err != nil {
			t.Fatal(err)
		}
	}
	matches := make([]*order.Match, len(takers))
	for i, taker := range takers {
		matches[i] = newMatch(maker, taker, taker.Quantity, epoch)
		matches[i].Status = order.MakerSwapCast
		if err := insertMatchForTest(matches[i]); err != nil {
			t.Fatal(err)
		}
	}
	for i, match := range matches {
		at := failTime.Add(time.Duration(i) * time.Second)
		event := &meshevents.MatchFailedEvent{
			MatchID: match.ID(), Base: maker.Base(), Quote: maker.Quote(),
			FailTime: at.UnixMilli(), Status: order.MakerSwapCast, Fault: meshevents.MatchFailureTakerFault,
		}
		payload, err := event.Encode()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := archie.ApplyMatchFailedEvent(ctx, &db.EventLogMeta{Event: payload}, policy, event); err != nil {
			t.Fatal(err)
		}
		completed, times, err := archie.CompletedUserOrders(maker.User(), 10)
		if err != nil {
			t.Fatal(err)
		}
		_, _, outcomes, err := archie.GetUserReputationData(ctx, maker.User(), 10, 10, 10)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			if len(completed) != 0 || len(outcomes) != 0 {
				t.Fatal("maker completed before its last match failed")
			}
		} else if len(completed) != 1 || completed[0] != maker.ID() || times[0] != at.UnixMilli() || len(outcomes) != 1 || outcomes[0].OrderID != maker.ID() || outcomes[0].Canceled {
			t.Fatalf("maker completion = %v/%v, outcomes = %+v", completed, times, outcomes)
		}
		completed, _, err = archie.CompletedUserOrders(takers[i].User(), 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(completed) != 0 {
			t.Fatal("faulted taker received order completion")
		}
	}
}

func TestMatchByID(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	// Make a perfect 1 lot match.
	limitBuyStanding := newLimitOrder(false, 4500000, 1, order.StandingTiF, 0)
	limitSellImmediate := newLimitOrder(true, 4490000, 1, order.ImmediateTiF, 10)

	base, quote := limitBuyStanding.Base(), limitBuyStanding.Quote()

	// Store it.
	epochID := order.EpochID{132412341, 1000}
	match := newMatch(limitBuyStanding, limitSellImmediate, limitSellImmediate.Quantity, epochID)
	err := insertMatchForTest(match)
	if err != nil {
		t.Fatalf("insertMatchForTest() failed: %v", err)
	}

	tests := []struct {
		name        string
		matchID     order.MatchID
		base, quote uint32
		wantedErr   error
	}{
		{
			"ok",
			match.ID(),
			base, quote,
			nil,
		},
		{
			"no order",
			order.MatchID{},
			base, quote,
			db.ArchiveError{Code: db.ErrUnknownMatch},
		},
		{
			"bad market",
			match.ID(),
			base, base,
			db.ArchiveError{Code: db.ErrUnsupportedMarket},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matchData, err := archie.MatchByID(tt.matchID, tt.base, tt.quote)
			if !db.SameErrorTypes(err, tt.wantedErr) {
				t.Fatal(err)
			}
			if err == nil && matchData.ID != tt.matchID {
				t.Errorf("Retrieved match with ID %v, expected %v", matchData.ID, tt.matchID)
			}
		})
	}
}

func TestUserMatches(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	// Make a perfect 1 lot match.
	limitBuyStanding := newLimitOrder(false, 4500000, 1, order.StandingTiF, 0)
	limitSellImmediate := newLimitOrder(true, 4490000, 1, order.ImmediateTiF, 10)

	base, quote := limitBuyStanding.Base(), limitBuyStanding.Quote()

	// Store it.
	epochID := order.EpochID{132412341, 1000}
	match := newMatch(limitBuyStanding, limitSellImmediate, limitSellImmediate.Quantity, epochID)
	err := insertMatchForTest(match)
	if err != nil {
		t.Fatalf("insertMatchForTest() failed: %v", err)
	}

	tests := []struct {
		name        string
		acctID      account.AccountID
		numExpected int
		wantedErr   error
	}{
		{
			"ok maker",
			limitBuyStanding.User(),
			1,
			nil,
		},
		{
			"ok taker",
			limitSellImmediate.User(),
			1,
			nil,
		},
		{
			"nope",
			randomAccountID(),
			0,
			nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matchData, err := archie.UserMatches(tt.acctID, base, quote)
			if err != tt.wantedErr {
				t.Fatal(err)
			}
			if len(matchData) != tt.numExpected {
				t.Errorf("Retrieved %d matches for user %v, expected %d.", len(matchData), tt.acctID, tt.numExpected)
			}
		})
	}
}

func TestSwapDataFullByID(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	got, err := archie.SwapDataFullByID(order.MatchID{})
	if !db.IsErrMatchUnknown(err) || got != nil {
		t.Fatalf("missing match = %+v, %v; want nil, ErrUnknownMatch", got, err)
	}

	for _, tt := range []struct {
		name   string
		status order.MatchStatus
		active bool
	}{
		{"active swap", order.MakerSwapCast, true},
		{"completed swap", order.MatchComplete, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			mp := generateMatch(t, tt.status, tt.active, randomAccountID(), randomAccountID())
			mid := mp.match.ID()
			got, err := archie.SwapDataFullByID(mid)
			if err != nil {
				t.Fatalf("SwapDataFullByID: %v", err)
			}
			if got == nil {
				t.Fatal("SwapDataFullByID returned nil")
			}
			if got.ID != mid || got.Base != mp.match.Maker.Base() || got.Quote != mp.match.Maker.Quote() ||
				got.Status != tt.status || got.Active != tt.active {
				t.Fatalf("unexpected match data: %+v", got)
			}
			if got.Maker != mp.match.Maker.ID() || got.MakerAcct != mp.match.Maker.User() ||
				got.Taker != mp.match.Taker.ID() || got.TakerAcct != mp.match.Taker.User() {
				t.Fatalf("unexpected match parties: %+v", got.MatchData)
			}
			wantSwap := &db.SwapData{
				ContractACoinID: mp.status.MakerSwap, ContractA: mp.status.MakerContract,
				ContractBCoinID: mp.status.TakerSwap, ContractB: mp.status.TakerContract,
				RedeemACoinID: mp.status.MakerRedeem, RedeemBCoinID: mp.status.TakerRedeem,
				RedeemASecret: mp.status.Secret,
			}
			if !reflect.DeepEqual(got.SwapData, wantSwap) {
				t.Fatalf("swap data = %+v, want %+v", got.SwapData, wantSwap)
			}
		})
	}
}

func TestMarketMatches(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	// Make a perfect 1 lot match.
	limitBuyStanding := newLimitOrder(false, 4500000, 1, order.StandingTiF, 0)
	limitSellImmediate := newLimitOrder(true, 4490000, 1, order.ImmediateTiF, 10)

	base, quote := limitBuyStanding.Base(), limitBuyStanding.Quote()

	// Store it.
	epochID := order.EpochID{132412341, 1000}
	match := newMatch(limitBuyStanding, limitSellImmediate, limitSellImmediate.Quantity, epochID)
	err := insertMatchForTest(match)
	if err != nil {
		t.Fatalf("insertMatchForTest() failed: %v", err)
	}
	// Make another perfect 1 lot match.
	limitBuyStanding = newLimitOrder(false, 4500000, 1, order.StandingTiF, 0)
	limitSellImmediate = newLimitOrder(true, 4490000, 1, order.ImmediateTiF, 10)

	// Store it.
	match = newMatch(limitBuyStanding, limitSellImmediate, limitSellImmediate.Quantity, epochID)
	err = insertMatchForTest(match)
	if err != nil {
		t.Fatalf("insertMatchForTest() failed: %v", err)
	}
	setMatchInactiveForTest(db.MarketMatchID{
		MatchID: match.ID(),
		Base:    base,
		Quote:   quote,
	})

	// This one has txns.
	mktMatchID := db.MarketMatchID{
		MatchID: match.ID(),
		Base:    limitBuyStanding.Base(),
		Quote:   limitBuyStanding.Quote(),
	}
	midWithCoins := mktMatchID.MatchID
	MakerSwap, MakerContract := encode.RandomBytes(36), encode.RandomBytes(50)
	err = saveContractForTest(mktMatchID, true, MakerContract, MakerSwap, 0)
	if err != nil {
		t.Fatalf("saveContractForTest (maker) error: %v", err)
	}

	TakerSwap, TakerContract := encode.RandomBytes(36), encode.RandomBytes(50)
	err = saveContractForTest(mktMatchID, false, TakerContract, TakerSwap, 0)
	if err != nil {
		t.Fatalf("saveContractForTest/saveRedeemForTest error: %v", err)
	}

	MakerRedeem, Secret := encode.RandomBytes(36), encode.RandomBytes(32)
	err = saveRedeemForTest(mktMatchID, true, MakerRedeem, Secret, 0)
	if err != nil {
		t.Fatalf("saveContractForTest/saveRedeemForTest error: %v", err)
	}
	// TakerRedeem not stored.

	// Make another perfect 1 lot match on another market.
	limitBuyStanding = newLimitOrderWithAssets(false, 4500000, 1, order.StandingTiF, 0, AssetBTC, AssetLTC)
	limitSellImmediate = newLimitOrderWithAssets(true, 4490000, 1, order.ImmediateTiF, 10, AssetBTC, AssetLTC)

	// Store it.
	match = newMatch(limitBuyStanding, limitSellImmediate, limitSellImmediate.Quantity, epochID)
	err = insertMatchForTest(match)
	if err != nil {
		t.Fatalf("insertMatchForTest() failed: %v", err)
	}

	// Only active.
	matchData, err := archie.MarketMatches(base, quote)
	if err != nil {
		t.Fatal(err)
	}
	if len(matchData) != 1 {
		t.Errorf("Retrieved %d matches for market, expected 1.", len(matchData))
	}
	// Include inactive (true), and no limit (-1).
	matchData = []*db.MatchDataWithCoins{}
	N, err := archie.MarketMatchesStreaming(base, quote, true, -1, func(md *db.MatchDataWithCoins) error {
		matchData = append(matchData, md)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if N != len(matchData) {
		t.Errorf("Retrieved %d matches for market, but method claimed %d.", len(matchData), N)
	}
	if len(matchData) != 2 {
		t.Errorf("Retrieved %d matches for market, expected 2.", len(matchData))
	}

	// Find the match with the stored coins and verify them.
	var found bool
	for _, md := range matchData {
		if md.ID == midWithCoins {
			found = true
			if !bytes.Equal(md.MakerSwapCoin, MakerSwap) {
				t.Errorf("Wrong maker swap coin %x, wanted %x", md.MakerSwapCoin, MakerSwap)
			}
			if !bytes.Equal(md.TakerSwapCoin, TakerSwap) {
				t.Errorf("Wrong taker swap coin %x, wanted %x", md.TakerSwapCoin, TakerSwap)
			}
			if !bytes.Equal(md.MakerRedeemCoin, MakerRedeem) {
				t.Errorf("Wrong maker redeem coin %x, wanted %x", md.MakerRedeemCoin, MakerRedeem)
			}
			if len(md.TakerRedeemCoin) > 0 {
				t.Errorf("got taker redeem coin %x, but expected none", md.TakerRedeemCoin)
			}
			break
		}
	}
	if !found {
		t.Errorf("failed to find match with the coins")
	}

	// Bad Market.
	matchData, err = archie.MarketMatches(base, base)
	noMktErr := new(db.ArchiveError)
	if !errors.As(err, noMktErr) || noMktErr.Code != db.ErrUnsupportedMarket {
		t.Fatalf("incorrect error for unsupported market: %v", err)
	}
}

type matchPair struct {
	match  *order.Match
	status *db.MatchStatus
}

func generateMatch(t *testing.T, matchStatus order.MatchStatus, active bool, makerBuyer, takerSeller account.AccountID, epochIdx ...uint64) *matchPair {
	return generateMatchWithOrderStatuses(t, matchStatus, active, makerBuyer, takerSeller,
		order.OrderStatusExecuted, order.OrderStatusExecuted, epochIdx...)
}

func generateMatchWithOrderStatuses(t *testing.T, matchStatus order.MatchStatus, active bool, makerBuyer, takerSeller account.AccountID, makerStatus, takerStatus order.OrderStatus, epochIdx ...uint64) *matchPair {
	t.Helper()
	loBuy := newLimitOrder(false, 4500000, 1, order.StandingTiF, 0)
	loBuy.P.AccountID = makerBuyer
	loSell := newLimitOrder(true, 4490000, 1, order.ImmediateTiF, 10)
	loSell.P.AccountID = takerSeller

	epIdx := uint64(132412341)
	if len(epochIdx) > 0 {
		epIdx = epochIdx[0]
	}
	epochID := order.EpochID{epIdx, 1000}

	err := storeOrderForTest(archie, loBuy, int64(epochID.Idx), int64(epochID.Dur), makerStatus)
	if err != nil {
		t.Fatalf("failed to store order: %v", err)
	}
	err = storeOrderForTest(archie, loSell, int64(epochID.Idx), int64(epochID.Dur), takerStatus)
	if err != nil {
		t.Fatalf("failed to store order: %v", err)
	}

	match := newMatch(loBuy, loSell, loSell.Quantity, epochID)
	match.Status = matchStatus
	err = insertMatchForTest(match)
	if err != nil {
		t.Fatalf("insertMatchForTest() failed: %v", err)
	}
	matchID := match.ID()
	mktMatchID := db.MarketMatchID{
		MatchID: matchID,
		Base:    loBuy.Base(),
		Quote:   loBuy.Quote(),
	}
	// Just alternate the active state.
	status := &db.MatchStatus{
		Status: matchStatus,
		Active: active,
	}
	if !active {
		setMatchInactiveForTest(mktMatchID)
	}
	for iStatus := order.NewlyMatched; iStatus <= matchStatus; iStatus++ {
		switch iStatus {
		case order.MakerSwapCast:
			status.MakerContract = encode.RandomBytes(50)
			status.MakerSwap = encode.RandomBytes(36)
			err := saveContractForTest(mktMatchID, true, status.MakerContract, status.MakerSwap, 0)
			if err != nil {
				t.Fatalf("saveContractForTest (maker) error: %v", err)
			}
		case order.TakerSwapCast:
			status.TakerContract = encode.RandomBytes(50)
			status.TakerSwap = encode.RandomBytes(36)
			err := saveContractForTest(mktMatchID, false, status.TakerContract, status.TakerSwap, 0)
			if err != nil {
				t.Fatalf("saveContractForTest (taker) error: %v", err)
			}
		case order.MakerRedeemed:
			status.MakerRedeem = encode.RandomBytes(36)
			status.Secret = encode.RandomBytes(32)
			err := saveRedeemForTest(mktMatchID, true, status.MakerRedeem, status.Secret, 0)
			if err != nil {
				t.Fatalf("saveRedeemForTest (maker) error: %v", err)
			}
		case order.MatchComplete:
			status.TakerRedeem = encode.RandomBytes(36)
			err := saveRedeemForTest(mktMatchID, false, status.TakerRedeem, nil, 0)
			if err != nil {
				t.Fatalf("saveRedeemForTest (taker) error: %v", err)
			}
		}
	}
	return &matchPair{match: match, status: status}
}

func TestCompletedAndAtFaultMatchStats(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	epIdx := uint64(132412341)
	nextIdx := func() uint64 {
		epIdx++
		return epIdx
	}

	maker, taker := randomAccountID(), randomAccountID()
	matches := []*matchPair{
		generateMatch(t, order.TakerSwapCast, false, maker, taker, nextIdx()), // 0: failed, maker fault
		generateMatch(t, order.MatchComplete, false, maker, taker, nextIdx()), // 1: success
		generateMatch(t, order.MakerRedeemed, true, maker, taker, nextIdx()),  // 2: still active, but maker success
		generateMatch(t, order.MakerRedeemed, false, maker, taker, nextIdx()), // 3: failed, maker success, taker fault
		generateMatch(t, order.MakerRedeemed, false, maker, maker, nextIdx()), // 4: failed, maker fault (no same-user maker success until MatchComplete)
		generateMatch(t, order.MakerSwapCast, false, maker, taker, nextIdx()), // 5: failed, taker fault
		generateMatch(t, order.NewlyMatched, false, maker, taker, nextIdx()),  // 6: failed, maker fault
	}

	// Make a perfect 1 lot match in different market (BTC-LTC).
	limitBuy := newLimitOrder(false, 4500000, 1, order.StandingTiF, 20)
	limitBuy.BaseAsset, limitBuy.QuoteAsset = AssetBTC, AssetLTC
	limitBuy.AccountID = maker
	limitSell := newLimitOrder(true, 4490000, 1, order.ImmediateTiF, 30)
	limitSell.BaseAsset, limitSell.QuoteAsset = AssetBTC, AssetLTC
	taker2 := randomAccountID()
	limitSell.AccountID = taker2
	matchLTC := newMatch(limitBuy, limitSell, limitSell.Quantity, order.EpochID{nextIdx(), 1000})
	matchLTC.Status = order.MatchComplete
	err := insertMatchForTest(matchLTC)
	if err != nil {
		t.Fatalf("insertMatchForTest() failed: %v", err)
	}
	setMatchInactiveForTest(db.MarketMatchID{
		MatchID: matchLTC.ID(),
		Base:    limitBuy.Base(),
		Quote:   limitBuy.Quote(),
	})
	// 7: success
	matches = append(matches, &matchPair{
		match: matchLTC,
		status: &db.MatchStatus{
			Active: false,
			Status: matchLTC.Status,
		},
	})
	// TODO: update with a forgiven one

	epochTime := func(mp *matchPair) int64 {
		return mp.match.Epoch.End().UnixMilli()
	}

	tests := []struct {
		name         string
		acctID       account.AccountID
		wantOutcomes []*db.MatchOutcome
		wantedErr    error
	}{
		{
			"maker",
			maker,
			[]*db.MatchOutcome{ // ascending by time (MatchID field TODO)
				{
					Status: matches[0].match.Status,
					Fail:   true,
					Time:   epochTime(matches[0]),
				}, {
					Status: matches[1].match.Status,
					Fail:   false,
					Time:   epochTime(matches[1]),
				}, {
					Status: matches[2].match.Status,
					Fail:   false,
					Time:   epochTime(matches[2]),
				}, {
					Status: matches[3].match.Status,
					Fail:   false,
					Time:   epochTime(matches[3]),
				}, {
					Status: matches[4].match.Status,
					Fail:   true,
					Time:   epochTime(matches[4]),
				}, {
					Status: matches[6].match.Status,
					Fail:   true,
					Time:   epochTime(matches[6]),
				}, {
					Status: matches[7].match.Status,
					Fail:   false,
					Time:   epochTime(matches[7]),
				},
			},
			nil,
		},
		{
			"taker",
			taker,
			[]*db.MatchOutcome{
				{
					Status: matches[1].match.Status,
					Fail:   false,
					Time:   epochTime(matches[1]),
				}, {
					Status: matches[3].match.Status,
					Fail:   true,
					Time:   epochTime(matches[3]),
				}, {
					Status: matches[5].match.Status,
					Fail:   true,
					Time:   epochTime(matches[5]),
				},
			},
			nil,
		},
		{
			"nope",
			randomAccountID(),
			nil,
			nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outcomes, err := archie.CompletedAndAtFaultMatchStats(tt.acctID, 60)
			if err != tt.wantedErr {
				t.Fatal(err)
			}
			if len(outcomes) != len(tt.wantOutcomes) {
				t.Errorf("Retrieved %d match outcomes for user %v, expected %d.", len(outcomes), tt.acctID, len(tt.wantOutcomes))
			}
			for i, mo := range tt.wantOutcomes {
				if outcomes[i].Time != mo.Time || outcomes[i].Status != mo.Status || outcomes[i].Fail != mo.Fail {
					t.Log(outcomes[i])
					t.Log(mo)
					t.Errorf("wrong %d", i)
				}
			}
		})
	}
}

func TestUserMatchFails(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	epIdx := uint64(132412341)
	nextIdx := func() uint64 {
		epIdx++
		return epIdx
	}

	user, otherUser := randomAccountID(), randomAccountID()
	matches := []*matchPair{
		generateMatch(t, order.TakerSwapCast, false, user, otherUser, nextIdx()), // 0: failed, user fault
		generateMatch(t, order.MatchComplete, false, user, otherUser, nextIdx()), // 1: success
		generateMatch(t, order.MakerRedeemed, true, user, otherUser, nextIdx()),  // 2: still active, but user success
		generateMatch(t, order.MakerRedeemed, false, otherUser, user, nextIdx()), // 3: failed, user success, otherUser fault
		generateMatch(t, order.MakerSwapCast, false, otherUser, user, nextIdx()), // 5: failed, user fault
		generateMatch(t, order.NewlyMatched, false, otherUser, user, nextIdx()),  // 6: failed, otherUser fault
	}
	// Put one of them on another market
	m4 := matches[4]
	m4.match.Maker.Prefix().BaseAsset = AssetBTC
	m4.match.Maker.Prefix().QuoteAsset = AssetLTC
	m4.match.Taker.Prefix().BaseAsset = AssetBTC
	m4.match.Taker.Prefix().QuoteAsset = AssetLTC
	for _, m := range matches {
		err := insertMatchForTest(m.match)
		if err != nil {
			t.Fatalf("insertMatchForTest() failed: %v", err)
		}
	}
	fails, err := archie.UserMatchFails(user, 100)
	if err != nil {
		t.Fatalf("UserMatchFails() failed: %v", err)
	}
check:
	for _, i := range []int{0, 3, 4} {
		matchID := matches[i].match.ID()
		for _, fail := range fails {
			if fail.ID == matchID {
				continue check
			}
		}
		t.Fatalf("expected to find fail for match at index %d, but did not", i)
	}
}

func TestAllActiveUserMatches(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	// Make a perfect 1 lot match.
	limitBuyStanding := newLimitOrder(false, 4500000, 1, order.StandingTiF, 0)
	limitSellImmediate := newLimitOrder(true, 4490000, 1, order.ImmediateTiF, 10)

	// Make it complete and store it.
	epochID := order.EpochID{132412341, 1000}
	// maker buy (quote swap asset), taker sell (base swap asset)
	match := newMatch(limitBuyStanding, limitSellImmediate, limitSellImmediate.Quantity, epochID)
	match.Status = order.TakerSwapCast // failed here
	err := insertMatchForTest(match)   // active by default
	if err != nil {
		t.Fatalf("insertMatchForTest() failed: %v", err)
	}
	err = setMatchInactiveForTest(db.MatchID(match)) // set inactive, not forgiven
	if err != nil {
		t.Fatalf("setMatchInactive() failed: %v", err)
	}

	// Make a perfect 1 lot match, same parties.
	limitBuyStanding2 := newLimitOrder(false, 4500000, 1, order.StandingTiF, 20)
	limitBuyStanding2.AccountID = limitBuyStanding.AccountID
	limitSellImmediate2 := newLimitOrder(true, 4490000, 1, order.ImmediateTiF, 30)
	limitSellImmediate2.AccountID = limitSellImmediate.AccountID

	// Store it.
	epochID2 := order.EpochID{132412342, 1000}
	// maker buy (quote swap asset), taker sell (base swap asset)
	match2 := newMatch(limitBuyStanding2, limitSellImmediate2, limitSellImmediate2.Quantity, epochID2)
	err = insertMatchForTest(match2)
	if err != nil {
		t.Fatalf("insertMatchForTest() failed: %v", err)
	}

	// Make a perfect 1 lot BTC-LTC match.
	limitBuyStanding3 := newLimitOrder(false, 4500000, 1, order.StandingTiF, 20)
	limitBuyStanding3.BaseAsset = AssetBTC
	limitBuyStanding3.QuoteAsset = AssetLTC
	limitBuyStanding3.AccountID = limitBuyStanding.AccountID
	limitSellImmediate3 := newLimitOrder(true, 4490000, 1, order.ImmediateTiF, 30)
	limitSellImmediate3.BaseAsset = AssetBTC
	limitSellImmediate3.QuoteAsset = AssetLTC
	limitSellImmediate3.AccountID = limitSellImmediate.AccountID

	// Store it.
	epochID3 := order.EpochID{132412342, 1000}
	match3 := newMatch(limitBuyStanding3, limitSellImmediate3, limitSellImmediate3.Quantity, epochID3)
	err = insertMatchForTest(match3)
	if err != nil {
		t.Fatalf("insertMatchForTest() failed: %v", err)
	}

	tests := []struct {
		name        string
		acctID      account.AccountID
		numExpected int
		wantMatch   []*order.Match
		wantedErr   error
	}{
		{
			"ok maker",
			limitBuyStanding.User(),
			2,
			[]*order.Match{match2, match3},
			nil,
		},
		{
			"ok taker",
			limitSellImmediate.User(),
			2,
			[]*order.Match{match2, match3},
			nil,
		},
		{
			"nope",
			randomAccountID(),
			0,
			nil,
			nil,
		},
	}

	idInMatchSlice := func(mid order.MatchID, ms []*order.Match) int {
		for i := range ms {
			if ms[i].ID() == mid {
				return i
			}
		}
		return -1
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			userMatch, err := archie.AllActiveUserMatches(tt.acctID)
			if err != tt.wantedErr {
				t.Fatal(err)
			}
			if len(userMatch) != tt.numExpected {
				t.Errorf("Retrieved %d matches for user %v, expected %d.", len(userMatch), tt.acctID, tt.numExpected)
			}
			for _, match := range userMatch {
				loc := idInMatchSlice(match.ID, tt.wantMatch)
				if loc == -1 {
					t.Errorf("Unknown match ID retrieved: %v.", match.ID)
					continue
				}
				if tt.wantMatch[loc].FeeRateBase != match.BaseRate {
					t.Errorf("incorrect base fee rate. got %d, want %d",
						match.BaseRate, tt.wantMatch[loc].FeeRateBase)
				}
				if tt.wantMatch[loc].FeeRateQuote != match.QuoteRate {
					t.Errorf("incorrect quote fee rate. got %d, want %d",
						match.QuoteRate, tt.wantMatch[loc].FeeRateQuote)
				}
				if tt.wantMatch[loc].Epoch.End() != match.Epoch.End() {
					t.Errorf("incorrect match time. got %v, want %v",
						match.Epoch.End(), tt.wantMatch[loc].Epoch.End())
				}
				if tt.wantMatch[loc].Taker.Trade().Address != match.TakerAddr {
					t.Errorf("incorrect counterparty swap address. got %v, want %v",
						match.TakerAddr, tt.wantMatch[loc].Taker.Trade().Address)
				}
				if tt.wantMatch[loc].Maker.Address != match.MakerAddr {
					t.Errorf("incorrect counterparty swap address. got %v, want %v",
						match.MakerAddr, tt.wantMatch[loc].Maker.Address)
				}
			}
		})
	}
}

func TestActiveSwaps(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	swapsDetails, err := archie.ActiveSwaps()
	if err != nil {
		t.Fatal(err)
	}
	if len(swapsDetails) > 0 {
		t.Fatalf("got details for %d swaps, expected 0", len(swapsDetails))
	}

	user1 := randomAccountID()
	user2 := randomAccountID()
	match := generateMatch(t, order.MakerRedeemed, true, user1, user2)

	swapsDetails, err = archie.ActiveSwaps()
	if err != nil {
		t.Fatal(err)
	}
	if len(swapsDetails) != 1 {
		t.Fatalf("got details for %d swaps, expected 1", len(swapsDetails))
	}
	swapDetails := swapsDetails[0]

	taker, _, err := archie.Order(swapDetails.MatchData.Taker, swapDetails.Base, swapDetails.Quote)
	if err != nil {
		t.Fatalf("Failed to load taker order: %v", err)
	}
	if taker.ID() != swapDetails.MatchData.Taker {
		t.Fatalf("Failed to load order %v, computed ID %v instead", swapDetails.MatchData.Taker, taker.ID())
	}
	if match.match.Taker.ID() != swapDetails.MatchData.Taker {
		t.Fatalf("Failed to load order %v, computed ID %v instead", swapDetails.MatchData.Taker, taker.ID())
	}

	maker, _, err := archie.Order(swapDetails.MatchData.Maker, swapDetails.Base, swapDetails.Quote)
	if err != nil {
		t.Fatalf("Failed to load maker order: %v", err)
	}
	if maker.ID() != swapDetails.MatchData.Maker {
		t.Fatalf("Failed to load order %v, computed ID %v instead", swapDetails.MatchData.Maker, maker.ID())
	}
	if match.match.Maker.ID() != swapDetails.MatchData.Maker {
		t.Fatalf("Failed to load order %v, computed ID %v instead", swapDetails.MatchData.Maker, maker.ID())
	}

	if match.match.Rate != swapDetails.Rate {
		t.Fatalf("wrong rate loaded, got %d want %d", swapDetails.Rate, match.match.Rate)
	}
	if match.match.Quantity != swapDetails.Quantity {
		t.Fatalf("wrong quantity loaded, got %d want %d", swapDetails.Quantity, match.match.Quantity)
	}
	makerLO, ok := maker.(*order.LimitOrder)
	if !ok {
		t.Fatalf("Maker order was not a limit order: %T", maker)
	}

	matchBack := &order.Match{
		Taker:        taker,
		Maker:        makerLO,
		Quantity:     swapDetails.Quantity,
		Rate:         swapDetails.Rate,
		FeeRateBase:  swapDetails.BaseRate,
		FeeRateQuote: swapDetails.QuoteRate,
		Epoch:        swapDetails.Epoch,
		Status:       swapDetails.Status,
		Sigs: order.Signatures{ // not really needed
			MakerMatch:  swapDetails.SwapData.SigMatchAckMaker,
			TakerMatch:  swapDetails.SwapData.SigMatchAckTaker,
			MakerAudit:  swapDetails.SwapData.ContractAAckSig,
			TakerAudit:  swapDetails.SwapData.ContractBAckSig,
			TakerRedeem: swapDetails.SwapData.RedeemAAckSig,
		},
	}

	wantMid := match.match.ID()
	if wantMid != swapDetails.MatchData.ID {
		t.Fatalf("incorrect match ID %v, expected %v", swapDetails.MatchData.ID, wantMid)
	}
	// recompute the match ID from the loaded orders (their computed IDs), match rate, qty, etc.
	if wantMid != matchBack.ID() {
		t.Fatalf("Failed to reconstruct Match %v, computed ID %v instead", matchBack.ID(), wantMid)
	}
}

func TestMatchStatuses(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	// Unknown market
	aid := randomAccountID()
	var mid order.MatchID
	copy(mid[:], encode.RandomBytes(32))
	_, err := archie.MatchStatuses(aid, 100, 101, []order.MatchID{mid})
	noMktErr := new(db.ArchiveError)
	if !errors.As(err, noMktErr) || noMktErr.Code != db.ErrUnsupportedMarket {
		t.Fatalf("incorrect error for unsupported market: %v", err)
	}

	user1 := randomAccountID()
	user2 := randomAccountID()

	matches := []*matchPair{
		generateMatch(t, order.NewlyMatched, true, user1, user2),                           // 0
		generateMatch(t, order.MakerSwapCast, false, user1, user2),                         // 1
		generateMatch(t, order.TakerSwapCast, true, user1, user2),                          // 2
		generateMatch(t, order.MakerRedeemed, true, user1, user2),                          // 3
		generateMatch(t, order.MatchComplete, false, user1, user2),                         // 4 -- inactive via SaveRedeemB
		generateMatch(t, order.MakerRedeemed, false, randomAccountID(), randomAccountID()), // 5
	}

	idList := func(idxs ...int) []order.MatchID {
		ids := make([]order.MatchID, 0, len(idxs))
		for _, i := range idxs {
			ids = append(ids, matches[i].match.ID())
		}
		return ids
	}

	tests := []struct {
		name string
		user account.AccountID
		req  []order.MatchID
		exp  []int // matches index
	}{
		// user 1: 1 hit
		{
			name: "find1",
			user: user1,
			req:  idList(0),
			exp:  []int{0},
		},
		// user 1: 1 hit + 1 miss.
		{
			name: "find1-miss1",
			user: user1,
			req:  idList(1, 5),
			exp:  []int{1},
		},
		// user 2 hit 4
		{
			name: "find4",
			user: user2,
			req:  idList(0, 1, 2, 3),
			exp:  []int{0, 1, 2, 3},
		},
	}

	for _, tt := range tests {
		statuses, err := archie.MatchStatuses(tt.user, AssetDCR, AssetBTC, tt.req)
		if err != nil {
			t.Fatalf("%s: error getting order statuses: %v", tt.name, err)
		}
		if len(statuses) != len(tt.exp) {
			t.Fatalf("%s: wrongs number of statuses returned. expected %d, got %d", tt.name, len(tt.exp), len(statuses))
		}
	top:
		for _, expIdx := range tt.exp {
			matchPair := matches[expIdx]
			expStatus := matchPair.status
			matchID := matchPair.match.ID()
			// Find the status
			for _, status := range statuses {
				if status.ID != matchID {
					continue
				}
				if status.Status != expStatus.Status {
					t.Fatalf("%s: expIdx = %d, wrong status. expected %s, got %s", tt.name, expIdx, expStatus.Status, status.Status)
				}
				if !bytes.Equal(status.MakerContract, expStatus.MakerContract) {
					t.Fatalf("%s: wrong MakerContract. expected %x, got %x", tt.name, expStatus.MakerContract, status.MakerContract)
				}
				if !bytes.Equal(status.TakerContract, expStatus.TakerContract) {
					t.Fatalf("%s: wrong TakerContract. expected %x, got %x", tt.name, expStatus.TakerContract, status.TakerContract)
				}
				if !bytes.Equal(status.MakerSwap, expStatus.MakerSwap) {
					t.Fatalf("%s: wrong MakerSwap. expected %x, got %x", tt.name, expStatus.MakerSwap, status.MakerSwap)
				}
				if !bytes.Equal(status.TakerSwap, expStatus.TakerSwap) {
					t.Fatalf("%s: wrong TakerSwap. expected %x, got %x", tt.name, expStatus.TakerSwap, status.TakerSwap)
				}
				if !bytes.Equal(status.MakerRedeem, expStatus.MakerRedeem) {
					t.Fatalf("%s: wrong MakerRedeem. expected %x, got %x", tt.name, expStatus.MakerRedeem, status.MakerRedeem)
				}
				if !bytes.Equal(status.TakerRedeem, expStatus.TakerRedeem) {
					t.Fatalf("%s: wrong TakerRedeem. expected %x, got %x", tt.name, expStatus.TakerRedeem, status.TakerRedeem)
				}
				if !bytes.Equal(status.Secret, expStatus.Secret) {
					t.Fatalf("%s: wrong Secret. expected %x, got %x", tt.name, expStatus.Secret, status.Secret)
				}
				if status.Active != expStatus.Active {
					t.Fatalf("%s: wrong Active. expected %t, got %t", tt.name, expStatus.Active, status.Active)
				}
				continue top
			}
			t.Fatalf("%s: expected match at index %d not found in results", tt.name, expIdx)
		}
	}

}

func TestEpochReport(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	lastRate, err := archie.LastEpochRate(42, 0)
	if err != nil {
		t.Fatalf("error getting last epoch rate from empty table (should be err = nil, rate = 0): %v", err)
	}
	if lastRate != 0 {
		t.Fatalf("wrong initial last rate. expected 0, got %d", lastRate)
	}

	var epochIdx, epochDur int64 = 13245678, 6000
	err = archie.InsertEpoch(&db.EpochResults{
		MktBase:     42,
		MktQuote:    0,
		Idx:         epochIdx,
		Dur:         epochDur,
		MatchVolume: 1,
		HighRate:    2,
		LowRate:     3,
		StartRate:   4,
		EndRate:     5,
		QuoteVolume: 6,
	})

	if err != nil {
		t.Fatalf("error inserting first epoch: %v", err)
	}

	startStamp := uint64(epochIdx * epochDur)
	endStamp := startStamp + uint64(epochDur)
	candle := &candles.Candle{
		StartStamp:  startStamp,
		EndStamp:    endStamp,
		MatchVolume: 1,
		HighRate:    2,
		LowRate:     3,
		StartRate:   4,
		EndRate:     5,
		QuoteVolume: 6,
	}
	addCandles := make([]*candles.Candle, 3)
	addCandles[0] = candle

	lastRate, err = archie.LastEpochRate(42, 0)
	if err != nil {
		t.Fatalf("error getting last epoch rate from after first epoch: %v", err)
	}
	if lastRate != 5 {
		t.Fatalf("wrong first epoch last rate. expected 5, got %d", lastRate)
	}

	// Trying for the same epoch should violate a primary key constraint.
	err = archie.InsertEpoch(&db.EpochResults{
		MktBase:  42,
		MktQuote: 0,
		Idx:      epochIdx,
		Dur:      epochDur,
	})
	if err == nil {
		t.Fatalf("no error for duplicate epoch")
	}

	err = archie.InsertEpoch(&db.EpochResults{
		MktBase:     42,
		MktQuote:    0,
		Idx:         epochIdx + 1,
		Dur:         epochDur,
		MatchVolume: 11,
		HighRate:    12,
		LowRate:     13,
		StartRate:   14,
		EndRate:     15,
		QuoteVolume: 16,
	})
	if err != nil {
		t.Fatalf("error inserting second epoch: %v", err)
	}

	startStamp = uint64((epochIdx + 1) * epochDur)
	endStamp = startStamp + uint64(epochDur)
	candle = &candles.Candle{
		StartStamp:  startStamp,
		EndStamp:    endStamp,
		MatchVolume: 11,
		HighRate:    12,
		LowRate:     13,
		StartRate:   14,
		EndRate:     15,
		QuoteVolume: 16,
	}
	addCandles[1] = candle

	lastRate, err = archie.LastEpochRate(42, 0)
	if err != nil {
		t.Fatalf("error getting last epoch rate from after second-to-last epoch: %v", err)
	}
	if lastRate != 15 {
		t.Fatalf("wrong second-to-last epoch last rate. expected 15, got %d", lastRate)
	}

	archie.InsertEpoch(&db.EpochResults{
		MktBase:     42,
		MktQuote:    0,
		Idx:         epochIdx + 2,
		Dur:         epochDur,
		MatchVolume: 100,
		HighRate:    100,
		LowRate:     100,
		StartRate:   100,
		EndRate:     100,
		QuoteVolume: 100,
	})

	startStamp = uint64((epochIdx + 2) * epochDur)
	endStamp = startStamp + uint64(epochDur)
	candle = &candles.Candle{
		StartStamp:  startStamp,
		EndStamp:    endStamp,
		MatchVolume: 100,
		HighRate:    100,
		LowRate:     100,
		StartRate:   100,
		EndRate:     100,
		QuoteVolume: 100,
	}
	addCandles[2] = candle

	startStamp = uint64((epochIdx + 2) * epochDur)
	endStamp = startStamp + uint64(epochDur)
	dayCandle := &candles.Candle{
		StartStamp:  startStamp,
		EndStamp:    endStamp,
		MatchVolume: 112,
		HighRate:    100,
		LowRate:     3,
		StartRate:   4,
		EndRate:     100,
		QuoteVolume: 122,
	}

	if err = archie.InsertCandles(42, 0, uint64(epochDur), addCandles); err != nil {
		t.Fatalf("error inserting candles: %v", err)
	}

	if err = archie.InsertCandles(42, 0, uint64(time.Hour*24/time.Millisecond), []*candles.Candle{dayCandle}); err != nil {
		t.Fatalf("error inserting candle: %v", err)
	}

	epochCache := candles.NewCache(3, uint64(epochDur))
	dayCache := candles.NewCache(2, uint64(time.Hour*24/time.Millisecond))

	err = archie.LoadEpochStats(42, 0, []*candles.Cache{epochCache, dayCache})
	if err != nil {
		t.Fatalf("error loading epoch stats: %v", err)
	}

	epochCandles := epochCache.WireCandles(3).Candles()
	if len(epochCandles) != 3 {
		t.Fatalf("epoch cache has wrong number of entries. expected 3, got %d", len(epochCandles))
	}
	lastCandle := epochCandles[len(epochCandles)-1]
	if lastCandle.MatchVolume != 100 {
		t.Fatalf("wrong last epoch candle match volume. expected 100, got %d", lastCandle.MatchVolume)
	}

	dayCandles := dayCache.WireCandles(2).Candles()
	if len(dayCandles) != 1 {
		t.Fatalf("day cache has wrong number of entries. expected 1, got %d", len(dayCandles))
	}
	lastCandle = dayCandles[len(dayCandles)-1]
	if lastCandle.MatchVolume != 112 { // 1 + 11
		t.Fatalf("wrong last day candle MatchVolume. expected 112, got %d", lastCandle.MatchVolume)
	}
	if lastCandle.QuoteVolume != 122 { // 6 + 16
		t.Fatalf("wrong last day candle QuoteVolume. expected 122, got %d", lastCandle.MatchVolume)
	}
	if lastCandle.HighRate != 100 {
		t.Fatalf("wrong last day candle HighRate. expected 100, got %d", lastCandle.HighRate)
	}
	if lastCandle.LowRate != 3 {
		t.Fatalf("wrong last day candle LowRate. expected 3, got %d", lastCandle.LowRate)
	}
	if lastCandle.StartRate != 4 {
		t.Fatalf("wrong last day candle StartRate. expected 4, got %d", lastCandle.StartRate)
	}
	if lastCandle.EndRate != 100 {
		t.Fatalf("wrong last day candle EndRate. expected 100, got %d", lastCandle.EndRate)
	}

}
