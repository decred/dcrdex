package order

import (
	"bytes"
	"encoding/hex"
	"testing"
	"time"
)

func TestMatchID(t *testing.T) {
	// taker
	marketID0, _ := hex.DecodeString("6258988bdb7cc6def56635b3089b8a3f9f7cb4ef4517573b3fb2d086fd62b6f7")
	var marketID OrderID
	copy(marketID[:], marketID0)

	rate := uint64(13241324)
	qty := uint64(132413241324)

	mo := &MarketOrder{
		Prefix: Prefix{
			AccountID:  acct0,
			BaseAsset:  AssetDCR,
			QuoteAsset: AssetBTC,
			OrderType:  MarketOrderType,
			ClientTime: time.Unix(1566497653, 0),
			ServerTime: time.Unix(1566497656, 0),
		},
		Coins: []CoinID{
			utxoCoinID("a985d8df97571b130ce30a049a76ffedaa79b6e69b173ff81b1bf9fc07f063c7", 1),
		},
		Sell:     true,
		Quantity: qty,
		Address:  "DcqXswjTPnUcd4FRCkX4vRJxmVtfgGVa5ui",
	}

	// maker
	limitID0, _ := hex.DecodeString("b89cb353ebe077e21ad35e024a6154d0a37e9e091a380357f82b277c694f3d64")
	var limitID OrderID
	copy(limitID[:], limitID0)

	lo := &LimitOrder{
		MarketOrder: MarketOrder{
			Prefix: Prefix{
				AccountID:  acct0,
				BaseAsset:  AssetDCR,
				QuoteAsset: AssetBTC,
				OrderType:  LimitOrderType,
				ClientTime: time.Unix(1566497653, 0),
				ServerTime: time.Unix(1566497656, 0),
			},
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

	expIDStr := "a8998eb09346c49c7ad1d15c5cbc2c65a733ac0d630262564ec09756f1818e6f"
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
