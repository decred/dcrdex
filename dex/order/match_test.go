package order

import (
	"bytes"
	"encoding/hex"
	"testing"
	"time"
)

func TestMatchID(t *testing.T) {
	// taker
	marketID0, _ := hex.DecodeString("a347a8b9b9204e7626ed9f03e6a3a49f16a527451fb42c4d6c9494b136e85d50")
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
	limitID0, _ := hex.DecodeString("1bd2250771efa587e2b076bc59b93ee56bcbfd8b8d534a362127d23ce4766f51")
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

	expIDStr := "29b431b631b34405d3298418bf084d525cbd66eba9f4bc7f00a1dbe4808642e6"
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
