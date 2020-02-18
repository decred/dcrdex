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
	matchA := newMatch(limitBuyStanding, limitSellImmediate, limitSellImmediate.Quantity, epochID)

	base, quote := limitBuyStanding.Base(), limitBuyStanding.Quote()

	matchAUpdated := matchA
	matchAUpdated.Status = order.MakerSwapCast
	// matchAUpdated.Sigs.MakerMatch = randomBytes(73)

	tests := []struct {
		name    string
		match   *order.Match
		wantErr bool
	}{
		{
			"store ok",
			matchA,
			false,
		},
		{
			"update ok",
			matchAUpdated,
			false,
		},
		{
			"update again ok",
			matchAUpdated,
			false,
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

	checkMatch := func(wantStatus order.MatchStatus) error {
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
		return nil
	}

	err := archie.InsertMatch(matchA)
	if err != nil {
		t.Errorf("InsertMatch() failed: %v", err)
	}

	if err = checkMatch(order.NewlyMatched); err != nil {
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
	redeemATime := int64(1234)
	err = archie.SaveRedeemA(mid, redeemCoinIDA, redeemATime)
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

func TestActiveMatches(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	// Make a perfect 1 lot match.
	limitBuyStanding := newLimitOrder(false, 4500000, 1, order.StandingTiF, 0)
	limitSellImmediate := newLimitOrder(true, 4490000, 1, order.ImmediateTiF, 10)

	// Make it complete and store it.
	epochID := order.EpochID{132412341, 10}
	match := newMatch(limitBuyStanding, limitSellImmediate, limitSellImmediate.Quantity, epochID)
	match.Status = order.MatchComplete // not an active order now
	err := archie.InsertMatch(match)
	if err != nil {
		t.Fatalf("InsertMatch() failed: %v", err)
	}

	// Make a perfect 1 lot match, same parties.
	limitBuyStanding2 := newLimitOrder(false, 4500000, 1, order.StandingTiF, 20)
	limitBuyStanding2.AccountID = limitBuyStanding.AccountID
	limitSellImmediate2 := newLimitOrder(true, 4490000, 1, order.ImmediateTiF, 30)
	limitSellImmediate2.AccountID = limitSellImmediate.AccountID

	// Store it.
	epochID2 := order.EpochID{132412342, 10}
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
		name         string
		acctID       account.AccountID
		numExpected  int
		wantMatchIDs []order.MatchID
		wantedErr    error
	}{
		{
			"ok maker",
			limitBuyStanding.User(),
			2,
			[]order.MatchID{match2.ID(), match3.ID()},
			nil,
		},
		{
			"ok taker",
			limitSellImmediate.User(),
			2,
			[]order.MatchID{match2.ID(), match3.ID()},
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

	idInSlice := func(mid order.MatchID, mids []order.MatchID) bool {
		for i := range mids {
			if mids[i] == mid {
				return true
			}
		}
		return false
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matchData, err := archie.ActiveMatches(tt.acctID)
			if err != tt.wantedErr {
				t.Fatal(err)
			}
			if len(matchData) != tt.numExpected {
				t.Errorf("Retrieved %d matches for user %v, expected %d.", len(matchData), tt.acctID, tt.numExpected)
			}
			for i := range matchData {
				if !idInSlice(matchData[i].MatchID, tt.wantMatchIDs) {
					t.Errorf("Incorrect match ID retrieved. Got %v, expected %v.", matchData[i].MatchID, tt.wantMatchIDs[i])
				}
			}
		})
	}
}
