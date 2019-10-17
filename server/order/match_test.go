package order

import (
	"bytes"
	"encoding/hex"
	"testing"
	"time"
)

func TestMatchID(t *testing.T) {
	marketID0, _ := hex.DecodeString("93f565294da3e939f90b1d41288c55dfd21d2f5947001942a9773fd627b5dfbe")
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
		UTXOs: []UTXO{
			newUtxo("a985d8df97571b130ce30a049a76ffedaa79b6e69b173ff81b1bf9fc07f063c7", 1),
		},
		Sell:     true,
		Quantity: qty,
		Address:  "DcqXswjTPnUcd4FRCkX4vRJxmVtfgGVa5ui",
	}

	limitID0, _ := hex.DecodeString("60ba7b92d0905aeaf93df3a69f28df2c7133b52361ec6114c825989b69bcf25b")
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
			UTXOs: []UTXO{
				newUtxo("01516d9c7ffbe260b811dc04462cedd3f8969ce3a3ffe6231ae870775a92e9b0", 1),
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

	expIDStr := "1f30059fbf7d094508dcd0db5ddbddf8862277ae8a131fa57390a9d290b85a14"
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
