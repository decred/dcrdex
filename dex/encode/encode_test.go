package encode

import (
	"testing"
	"time"

	"decred.org/dcrdex/dex/order"
	ordertest "decred.org/dcrdex/dex/order/test"
)

var (
	tEmpty = []byte{}
	tA     = []byte{0xaa}
	tB     = []byte{0xbb, 0xbb}
	tC     = []byte{0xcc, 0xcc, 0xcc}
)

func TestBuildyBytes(t *testing.T) {
	type test struct {
		pushes [][]byte
		exp    []byte
	}
	tests := []test{
		{
			pushes: [][]byte{tA},
			exp:    []byte{0x01, 0xaa},
		},
		{
			pushes: [][]byte{tA, tB},
			exp:    []byte{1, 0xaa, 2, 0xbb, 0xbb},
		},
		{
			pushes: [][]byte{tA, nil},
			exp:    []byte{1, 0xaa, 0},
		},
		{
			pushes: [][]byte{tEmpty, tEmpty},
			exp:    []byte{0, 0},
		},
	}
	for i, tt := range tests {
		var b BuildyBytes
		for _, p := range tt.pushes {
			b = b.AddData(p)
		}
		if !bEqual(b, tt.exp) {
			t.Fatalf("test %d failed", i)
		}
	}
}

func TestDecodeBlob(t *testing.T) {
	longBlob := RandomBytes(255)
	type test struct {
		v       byte
		b       []byte
		exp     [][]byte
		wantErr bool
	}
	tests := []test{
		{
			v:   1,
			b:   BuildyBytes{1}.AddData(nil).AddData(tEmpty).AddData(tA),
			exp: [][]byte{tEmpty, tEmpty, tA},
		},
		{
			v:   5,
			b:   BuildyBytes{5}.AddData(tB).AddData(tC),
			exp: [][]byte{tB, tC},
		},
		{
			v:   255,
			b:   BuildyBytes{255}.AddData(tA).AddData(longBlob),
			exp: [][]byte{tA, longBlob},
		},
		{
			b:       []byte{0x01, 0x02}, // missing two bytes
			wantErr: true,
		},
	}
	for i, tt := range tests {
		ver, pushes, err := DecodeBlob(tt.b)
		if (err != nil) != tt.wantErr {
			t.Fatalf("test %d: %v", i, err)
		}
		if tt.wantErr {
			continue
		}
		if ver != tt.v {
			t.Fatalf("test %d: wanted version %d, got %d", i, tt.v, ver)
		}
		if len(pushes) != len(tt.exp) {
			t.Fatalf("wrongs number of pushes. wanted %d, got %d", len(tt.exp), len(pushes))
		}
		for j, push := range pushes {
			check := tt.exp[j]
			if !bEqual(check, push) {
				t.Fatalf("push %d:%d incorrect. wanted %x, got %x", i, j, check, push)
			}
		}
	}
}

func TestOrders(t *testing.T) {
	spins := 10000
	tStart := time.Now()
	for i := 0; i < spins; i++ {
		testOrders(t)
	}
	t.Logf("created, encoded, and decoded %d orders (%d each of 3 types) in %d ms", 3*spins, spins, time.Since(tStart)/time.Millisecond)
}

func testOrders(t *testing.T) {
	lo := ordertest.RandomLimitOrder()
	lo.Coins = []order.CoinID{RandomBytes(36), RandomBytes(36)}
	lo.SetTime(time.Now().Unix())

	loB := EncodeOrder(lo)
	reOrder, err := DecodeOrder(loB)
	if err != nil {
		t.Fatalf("error decoding limit order: %v", err)
	}
	reLO, ok := reOrder.(*order.LimitOrder)
	if !ok {
		t.Fatalf("wrong order type returned for limit order")
	}

	ordertest.MustCompareLimitOrders(t, lo, reLO)

	mo := ordertest.RandomMarketOrder()
	mo.Coins = []order.CoinID{RandomBytes(36), RandomBytes(36), RandomBytes(38)}
	// Not setting the server time on this one.

	moB := EncodeOrder(mo)
	reOrder, err = DecodeOrder(moB)
	if err != nil {
		t.Fatalf("error decoding market order: %v", err)
	}
	reMO, ok := reOrder.(*order.MarketOrder)
	if !ok {
		t.Fatalf("wrong order type returned for market order")
	}
	ordertest.MustCompareMarketOrders(t, mo, reMO)

	co := ordertest.RandomCancelOrder()
	co.SetTime(time.Now().Unix())

	coB := EncodeOrder(co)
	reOrder, err = DecodeOrder(coB)
	if err != nil {
		t.Fatalf("error decoding cancel order: %v", err)
	}
	reCO, ok := reOrder.(*order.CancelOrder)
	if !ok {
		t.Fatalf("wrong order type returned for cancel order")
	}
	ordertest.MustCompareCancelOrders(t, co, reCO)
}

func TestUserMatch(t *testing.T) {
	spins := 30000
	tStart := time.Now()
	for i := 0; i < spins; i++ {
		testUserMatch(t)
	}
	t.Logf("created, encoded, and decoded %d matches in %d ms", spins, time.Since(tStart)/time.Millisecond)
}

func testUserMatch(t *testing.T) {
	match := ordertest.RandomUserMatch()
	matchB := EncodeMatch(match)

	reMatch, err := DecodeMatch(matchB)
	if err != nil {
		t.Fatalf("errror decoding UserMatch: %v", err)
	}
	ordertest.MustCompareUserMatch(t, match, reMatch)
}
