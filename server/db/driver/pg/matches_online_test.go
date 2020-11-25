// +build pgonline

package pg

import (
	"bytes"
	"errors"
	"fmt"
	"testing"
	"time"

	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/server/account"
	"decred.org/dcrdex/server/db"
)

func TestInsertMatch(t *testing.T) {
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
			err := archie.InsertMatch(tt.match)
			if (err != nil) != tt.wantErr {
				t.Errorf("InsertMatch() error = %v, wantErr %v", err, tt.wantErr)
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

	err := archie.InsertMatch(matchA)
	if err != nil {
		t.Errorf("InsertMatch() failed: %v", err)
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
	err = archie.SaveMatchAckSigA(mid, sigMakerMatch)
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
	err = archie.SaveMatchAckSigB(mid, sigTakerMatch)
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
	err = archie.SaveContractA(mid, contractA, coinIDA, contractATime)
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
	if err = archie.SaveAuditAckSigB(mid, auditSigB); err != nil {
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
	err = archie.SaveContractB(mid, contractB, coinIDB, contractBTime)
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
	if err = archie.SaveAuditAckSigA(mid, auditSigA); err != nil {
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
	err = archie.SaveRedeemA(mid, redeemCoinIDA, secret, redeemATime)
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
	if err = archie.SaveRedeemAckSigB(mid, redeemAckSigB); err != nil {
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
	err = archie.SaveRedeemB(mid, redeemCoinIDB, redeemBTime)
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
	err := archie.InsertMatch(match)
	if err != nil {
		t.Fatalf("InsertMatch() failed: %v", err)
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
	err := archie.InsertMatch(match)
	if err != nil {
		t.Fatalf("InsertMatch() failed: %v", err)
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
	err := archie.InsertMatch(match)
	if err != nil {
		t.Fatalf("InsertMatch() failed: %v", err)
	}
	// Make another perfect 1 lot match.
	limitBuyStanding = newLimitOrder(false, 4500000, 1, order.StandingTiF, 0)
	limitSellImmediate = newLimitOrder(true, 4490000, 1, order.ImmediateTiF, 10)

	// Store it.
	match = newMatch(limitBuyStanding, limitSellImmediate, limitSellImmediate.Quantity, epochID)
	err = archie.InsertMatch(match)
	if err != nil {
		t.Fatalf("InsertMatch() failed: %v", err)
	}
	archie.SetMatchInactive(db.MarketMatchID{
		MatchID: match.ID(),
		Base:    base,
		Quote:   quote,
	})
	// Make another perfect 1 lot match on another market.
	limitBuyStanding = newLimitOrderWithAssets(false, 4500000, 1, order.StandingTiF, 0, AssetBTC, AssetLTC)
	limitSellImmediate = newLimitOrderWithAssets(true, 4490000, 1, order.ImmediateTiF, 10, AssetBTC, AssetLTC)

	// Store it.
	match = newMatch(limitBuyStanding, limitSellImmediate, limitSellImmediate.Quantity, epochID)
	err = archie.InsertMatch(match)
	if err != nil {
		t.Fatalf("InsertMatch() failed: %v", err)
	}
	// Don't include inactive.
	matchData, err := archie.MarketMatches(base, quote, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(matchData) != 1 {
		t.Errorf("Retrieved %d matches for market, expected 1.", len(matchData))
	}
	// Include inactive.
	matchData, err = archie.MarketMatches(base, quote, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(matchData) != 2 {
		t.Errorf("Retrieved %d matches for market, expected 2.", len(matchData))
	}
	// Bad Market.
	matchData, err = archie.MarketMatches(base, base, true)
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

	err := archie.StoreOrder(loBuy, int64(epochID.Idx), int64(epochID.Dur), order.OrderStatusExecuted)
	if err != nil {
		t.Fatalf("failed to store order: %v", err)
	}
	err = archie.StoreOrder(loSell, int64(epochID.Idx), int64(epochID.Dur), order.OrderStatusExecuted)
	if err != nil {
		t.Fatalf("failed to store order: %v", err)
	}

	match := newMatch(loBuy, loSell, loSell.Quantity, epochID)
	match.Status = matchStatus
	err = archie.InsertMatch(match)
	if err != nil {
		t.Fatalf("InsertMatch() failed: %v", err)
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
		archie.SetMatchInactive(mktMatchID)
	}
	for iStatus := order.NewlyMatched; iStatus <= matchStatus; iStatus++ {
		switch iStatus {
		case order.MakerSwapCast:
			status.MakerContract = encode.RandomBytes(50)
			status.MakerSwap = encode.RandomBytes(36)
			err := archie.SaveContractA(mktMatchID, status.MakerContract, status.MakerSwap, 0)
			if err != nil {
				t.Fatalf("SaveContractA error: %v", err)
			}
		case order.TakerSwapCast:
			status.TakerContract = encode.RandomBytes(50)
			status.TakerSwap = encode.RandomBytes(36)
			err := archie.SaveContractB(mktMatchID, status.TakerContract, status.TakerSwap, 0)
			if err != nil {
				t.Fatalf("SaveContractB error: %v", err)
			}
		case order.MakerRedeemed:
			status.MakerRedeem = encode.RandomBytes(36)
			status.Secret = encode.RandomBytes(32)
			err := archie.SaveRedeemA(mktMatchID, status.MakerRedeem, status.Secret, 0)
			if err != nil {
				t.Fatalf("SaveContractB error: %v", err)
			}
		case order.MatchComplete:
			status.TakerRedeem = encode.RandomBytes(36)
			err := archie.SaveRedeemB(mktMatchID, status.TakerRedeem, 0)
			if err != nil {
				t.Fatalf("SaveContractB error: %v", err)
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
	err := archie.InsertMatch(matchLTC)
	if err != nil {
		t.Fatalf("InsertMatch() failed: %v", err)
	}
	archie.SetMatchInactive(db.MarketMatchID{
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

	epochTime := func(mp *matchPair) int64 {
		return encode.UnixMilli(mp.match.Epoch.End())
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
			[]*db.MatchOutcome{ // descending by time (MatchID field TODO)
				{
					Status: matches[7].match.Status,
					Fail:   false,
					Time:   epochTime(matches[7]),
				}, {
					Status: matches[6].match.Status,
					Fail:   true,
					Time:   epochTime(matches[6]),
				}, {
					Status: matches[4].match.Status,
					Fail:   true,
					Time:   epochTime(matches[4]),
				}, {
					Status: matches[3].match.Status,
					Fail:   false,
					Time:   epochTime(matches[3]),
				}, {
					Status: matches[2].match.Status,
					Fail:   false,
					Time:   epochTime(matches[2]),
				}, {
					Status: matches[1].match.Status,
					Fail:   false,
					Time:   epochTime(matches[1]),
				}, {
					Status: matches[0].match.Status,
					Fail:   true,
					Time:   epochTime(matches[0]),
				},
			},
			nil,
		},
		{
			"taker",
			taker,
			[]*db.MatchOutcome{
				{
					Status: matches[5].match.Status,
					Fail:   true,
					Time:   epochTime(matches[5]),
				}, {
					Status: matches[3].match.Status,
					Fail:   true,
					Time:   epochTime(matches[3]),
				}, {
					Status: matches[1].match.Status,
					Fail:   false,
					Time:   epochTime(matches[1]),
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
	err := archie.InsertMatch(match)   // active by default
	if err != nil {
		t.Fatalf("InsertMatch() failed: %v", err)
	}
	err = archie.SetMatchInactive(db.MatchID(match)) // set inactive
	if err != nil {
		t.Fatalf("SetMatchInactive() failed: %v", err)
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
	err = archie.InsertMatch(match2)
	if err != nil {
		t.Fatalf("InsertMatch() failed: %v", err)
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
	err = archie.InsertMatch(match3)
	if err != nil {
		t.Fatalf("InsertMatch() failed: %v", err)
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

	var epochIdx, epochDur int64 = 13245678, 6000
	err := archie.InsertEpoch(&db.EpochResults{
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

	epochCache := db.NewCandleCache(3, uint64(epochDur))
	dayCache := db.NewCandleCache(2, uint64(time.Hour*24/time.Millisecond))

	err = archie.LoadEpochStats(42, 0, []*db.CandleCache{epochCache, dayCache})
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
