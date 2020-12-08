package order

import (
	"bytes"
	"encoding/hex"
	"math/rand"
	"testing"
	"time"
)

// randomCommitment creates a random order commitment. If you require a matching
// commitment, generate a Preimage, then Preimage.Commit().
func randomCommitment() (com Commitment) {
	rand.Read(com[:])
	return
}

func TestMatchID(t *testing.T) {
	// taker
	marketID0, _ := hex.DecodeString("f1f4fb29235f5eeef55b27d909aac860828f6cf45f0b0fab92c6265844c50e54")
	var marketID OrderID
	copy(marketID[:], marketID0)

	rate := uint64(13241324)
	qty := uint64(132413241324)

	mo := &MarketOrder{
		P: Prefix{
			AccountID:  acct0,
			BaseAsset:  AssetDCR,
			QuoteAsset: AssetBTC,
			OrderType:  MarketOrderType,
			ClientTime: time.Unix(1566497653, 0),
			ServerTime: time.Unix(1566497656, 0),
			Commit:     randomCommitment(),
		},
		T: Trade{
			Coins: []CoinID{
				utxoCoinID("a985d8df97571b130ce30a049a76ffedaa79b6e69b173ff81b1bf9fc07f063c7", 1),
			},
			Sell:     true,
			Quantity: qty,
			Address:  "DcqXswjTPnUcd4FRCkX4vRJxmVtfgGVa5ui",
		},
	}

	// maker
	limitID0, _ := hex.DecodeString("f215571e8eec872b0bc6e14c989f217b4fe6fd68a0db07f0ffe75f18d28c28e3")
	var limitID OrderID
	copy(limitID[:], limitID0)

	lo := &LimitOrder{
		P: Prefix{
			AccountID:  acct0,
			BaseAsset:  AssetDCR,
			QuoteAsset: AssetBTC,
			OrderType:  LimitOrderType,
			ClientTime: time.Unix(1566497653, 0),
			ServerTime: time.Unix(1566497656, 0),
			Commit:     randomCommitment(),
		},
		T: Trade{
			Coins: []CoinID{
				utxoCoinID("01516d9c7ffbe260b811dc04462cedd3f8969ce3a3ffe6231ae870775a92e9b0", 1),
			},
			Sell:     false,
			Quantity: qty,
			Address:  "DcqXswjTPnUcd4FRCkX4vRJxmVtfgGVa5ui",
		},
		Rate:  rate,
		Force: StandingTiF,
	}

	set := &MatchSet{
		Taker:   mo,
		Makers:  []*LimitOrder{lo, lo},
		Amounts: []uint64{qty, qty},
		Rates:   []uint64{rate, rate},
		Total:   qty * 2,
	}

	matches := set.Matches()
	if len(matches) != 2 {
		t.Fatalf("wrong number of matches. expected 2, got %d", len(matches))
	}
	match, match2 := matches[0], matches[1]
	if match.ID() != match2.ID() {
		t.Fatalf("identical matches has different IDs. %s != %s", match.ID(), match2.ID())
	}

	expIDStr := "e80341f82c7b1187c8480f7ebbb179925e62358773429d396e624d30e15a7727"
	expID, _ := hex.DecodeString(expIDStr)
	matchID := match.ID()
	if !bytes.Equal(expID, matchID[:]) {
		t.Fatalf("expected match ID %x, got %x", expID, matchID[:])
	}
	if matchID.String() != expIDStr {
		t.Fatalf("wrong hex ID. wanted %s, got %s", expIDStr, matchID.String())
	}
	if match.Maker.ID() != limitID {
		t.Fatalf("wrong maker ID. expected %s, got %s", limitID, match.Maker.ID())
	}
	if match.Taker.ID() != marketID {
		t.Fatalf("wrong taker ID. expected %s, got %s", marketID, match.Taker.ID())
	}
}

func TestMatchSet(t *testing.T) {
	matchSet := &MatchSet{
		Rates:   []uint64{1e8, 2e8},
		Amounts: []uint64{5, 10},
	}
	h, l := matchSet.HighLowRates()
	if h != 2e8 {
		t.Fatalf("wrong high rate. wanted 2e8, got %d", h)
	}
	if l != 1e8 {
		t.Fatalf("wrong low rate. wanted 1e8, got %d", l)
	}
	qv := matchSet.QuoteVolume()
	if qv != 25 {
		t.Fatalf("wrong quote volume. wanted 25, got %d", qv)
	}
}
