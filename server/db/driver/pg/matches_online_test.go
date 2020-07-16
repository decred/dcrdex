// +build pgonline

package pg

import (
	"bytes"
	"fmt"
	"testing"

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

	epochID := order.EpochID{132412341, 10}
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

	epochID := order.EpochID{132412341, 10}
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

	// Party A's signature for acknowledgement of B's redemption
	redeemAckSigA := randomBytes(73)
	if err = archie.SaveRedeemAckSigA(mid, redeemAckSigA); err != nil {
		t.Fatal(err)
	}

	status, swapData, err = archie.SwapData(mid)
	if err != nil {
		t.Fatal(err)
	}
	if status != order.MatchComplete {
		t.Errorf("Got status %v, expected %v", status, order.MatchComplete)
	}
	if !bytes.Equal(swapData.RedeemBAckSig, redeemAckSigA) {
		t.Fatalf("RedeemBAckSig incorrect. got %v, expected %v",
			swapData.RedeemBAckSig, redeemAckSigA)
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
	epochID := order.EpochID{132412341, 10}
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
	epochID := order.EpochID{132412341, 10}
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

func TestAllActiveUserMatches(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	// Make a perfect 1 lot match.
	limitBuyStanding := newLimitOrder(false, 4500000, 1, order.StandingTiF, 0)
	limitSellImmediate := newLimitOrder(true, 4490000, 1, order.ImmediateTiF, 10)

	// Make it complete and store it.
	epochID := order.EpochID{132412341, 10}
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
	epochID2 := order.EpochID{132412342, 10}
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
	epochID3 := order.EpochID{132412342, 10}
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
