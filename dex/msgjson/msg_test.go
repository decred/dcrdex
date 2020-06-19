package msgjson

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"math/rand"
	"testing"
)

func TestMatch(t *testing.T) {
	// serialization: orderid (32) + matchid (8) + qty (8) + rate (8)
	// + address (varies)
	oid, _ := hex.DecodeString("2219c5f3a03407c87211748c884404e2f466cba19616faca1cda0010ca5db0d3")
	mid, _ := hex.DecodeString("4969784b00a59dd0340952c9b8f52840fbb32e9b51d4f6e18cbec7f50c8a3ed7")
	match := &Match{
		OrderID:      oid,
		MatchID:      mid,
		Quantity:     5e8,
		Rate:         uint64(2e8),
		ServerTime:   1585043380123,
		Address:      "DcqXswjTPnUcd4FRCkX4vRJxmVtfgGVa5ui",
		FeeRateBase:  65536 + 255,
		FeeRateQuote: (1 << 32) + 256 + 1,
	}
	exp := []byte{
		// Order ID 32 bytes
		0x22, 0x19, 0xc5, 0xf3, 0xa0, 0x34, 0x07, 0xc8, 0x72, 0x11, 0x74, 0x8c, 0x88,
		0x44, 0x04, 0xe2, 0xf4, 0x66, 0xcb, 0xa1, 0x96, 0x16, 0xfa, 0xca, 0x1c, 0xda,
		0x00, 0x10, 0xca, 0x5d, 0xb0, 0xd3,
		// Match ID 32 bytes
		0x49, 0x69, 0x78, 0x4b, 0x00, 0xa5, 0x9d, 0xd0, 0x34, 0x09, 0x52, 0xc9, 0xb8,
		0xf5, 0x28, 0x40, 0xfb, 0xb3, 0x2e, 0x9b, 0x51, 0xd4, 0xf6, 0xe1, 0x8c, 0xbe,
		0xc7, 0xf5, 0x0c, 0x8a, 0x3e, 0xd7,
		// quantity 8 bytes
		0x00, 0x00, 0x00, 0x00, 0x1d, 0xcd, 0x65, 0x00,
		// rate 8 bytes
		0x00, 0x00, 0x00, 0x00, 0x0b, 0xeb, 0xc2, 0x00,
		// timestamp 8 bytes
		0x00, 0x00, 0x01, 0x71, 0x0b, 0xf2, 0x97, 0x9b,
		// address - utf-8 encoding
		0x44, 0x63, 0x71, 0x58, 0x73, 0x77, 0x6a, 0x54, 0x50, 0x6e, 0x55, 0x63, 0x64,
		0x34, 0x46, 0x52, 0x43, 0x6b, 0x58, 0x34, 0x76, 0x52, 0x4a, 0x78, 0x6d, 0x56,
		0x74, 0x66, 0x67, 0x47, 0x56, 0x61, 0x35, 0x75, 0x69,
		// base fee rate 8 bytes
		0x0, 0x0, 0x0, 0x0, 0x0, 0x1, 0x0, 0xff,
		// quote fee rate 8 bytes
		0x0, 0x0, 0x0, 0x1, 0x0, 0x0, 0x1, 0x1,
	}

	b := match.Serialize()
	if !bytes.Equal(b, exp) {
		t.Fatalf("unexpected serialization. Wanted %x, got %x", exp, b)
	}

	matchB, err := json.Marshal(match)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var matchBack Match
	err = json.Unmarshal(matchB, &matchBack)
	if err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if !bytes.Equal(matchBack.MatchID, match.MatchID) {
		t.Fatal(matchBack.MatchID, match.MatchID)
	}
	if !bytes.Equal(matchBack.OrderID, match.OrderID) {
		t.Fatal(matchBack.OrderID, match.OrderID)
	}
	if matchBack.Quantity != match.Quantity {
		t.Fatal(matchBack.Quantity, match.Quantity)
	}
	if matchBack.Rate != match.Rate {
		t.Fatal(matchBack.Rate, match.Rate)
	}
	if matchBack.ServerTime != match.ServerTime {
		t.Fatal(matchBack.ServerTime, match.ServerTime)
	}
	if matchBack.Address != match.Address {
		t.Fatal(matchBack.Address, match.Address)
	}
}

func TestInit(t *testing.T) {
	// serialization: orderid (32) + matchid (32) + txid (probably 64) + vout (4)
	// + contract (97 ish)
	oid, _ := hex.DecodeString("ceb09afa675cee31c0f858b94c81bd1a4c2af8c5947d13e544eef772381f2c8d")
	mid, _ := hex.DecodeString("7c6b44735e303585d644c713fe0e95897e7e8ba2b9bba98d6d61b70006d3d58c")
	coinid, _ := hex.DecodeString("c3161033de096fd74d9051ff0bd99e359de35080a3511081ed035f541b850d4300000005")
	contract, _ := hex.DecodeString("caf8d277f80f71e4")
	init := &Init{
		OrderID:  oid,
		MatchID:  mid,
		CoinID:   coinid,
		Contract: contract,
	}

	exp := []byte{
		// Order ID 32 bytes
		0xce, 0xb0, 0x9a, 0xfa, 0x67, 0x5c, 0xee, 0x31, 0xc0, 0xf8, 0x58, 0xb9,
		0x4c, 0x81, 0xbd, 0x1a, 0x4c, 0x2a, 0xf8, 0xc5, 0x94, 0x7d, 0x13, 0xe5,
		0x44, 0xee, 0xf7, 0x72, 0x38, 0x1f, 0x2c, 0x8d,
		// Match ID 32 bytes
		0x7c, 0x6b, 0x44, 0x73, 0x5e, 0x30, 0x35, 0x85, 0xd6, 0x44, 0xc7, 0x13,
		0xfe, 0x0e, 0x95, 0x89, 0x7e, 0x7e, 0x8b, 0xa2, 0xb9, 0xbb, 0xa9, 0x8d,
		0x6d, 0x61, 0xb7, 0x00, 0x06, 0xd3, 0xd5, 0x8c,
		// Coin ID 36 bytes
		0xc3, 0x16, 0x10, 0x33, 0xde, 0x09, 0x6f, 0xd7, 0x4d, 0x90, 0x51, 0xff,
		0x0b, 0xd9, 0x9e, 0x35, 0x9d, 0xe3, 0x50, 0x80, 0xa3, 0x51, 0x10, 0x81,
		0xed, 0x03, 0x5f, 0x54, 0x1b, 0x85, 0x0d, 0x43, 0x00, 0x00, 0x00, 0x05,
		// Contract 8 bytes (shortened for testing)
		0xca, 0xf8, 0xd2, 0x77, 0xf8, 0x0f, 0x71, 0xe4,
	}
	b := init.Serialize()
	if !bytes.Equal(b, exp) {
		t.Fatalf("unexpected serialization. Wanted %x, got %x", exp, b)
	}

	initB, err := json.Marshal(init)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var initBack Init
	err = json.Unmarshal(initB, &initBack)
	if err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if !bytes.Equal(initBack.MatchID, init.MatchID) {
		t.Fatal(initBack.MatchID, init.MatchID)
	}
	if !bytes.Equal(initBack.OrderID, init.OrderID) {
		t.Fatal(initBack.OrderID, init.OrderID)
	}
	if !bytes.Equal(initBack.CoinID, init.CoinID) {
		t.Fatal(initBack.CoinID, init.CoinID)
	}
	if !bytes.Equal(initBack.Contract, init.Contract) {
		t.Fatal(initBack.Contract, init.Contract)
	}
}

func TestAudit(t *testing.T) {
	// serialization: orderid (32) + matchid (32) + time (8) + coin ID (36)
	// + contract (97 ish)
	oid, _ := hex.DecodeString("d6c752bb34d833b6e0eb4d114d690d044f8ab3f6de9defa08e9d7d237f670fe4")
	mid, _ := hex.DecodeString("79f84ef6c60e72edd305047c015d7b7ade64525a301fdac136976f05edb6172b")
	coinID, _ := hex.DecodeString("3cdabd9bd62dfbd7d8b020d5de4e643b439886f4b0dc86cb8a56dff8e61c5ec333487a97")
	contract, _ := hex.DecodeString("fc99f576f8e0e5dc")
	audit := &Audit{
		OrderID:  oid,
		MatchID:  mid,
		Time:     1570705920,
		CoinID:   coinID,
		Contract: contract,
	}

	exp := []byte{
		// Order ID 32 bytes
		0xd6, 0xc7, 0x52, 0xbb, 0x34, 0xd8, 0x33, 0xb6, 0xe0, 0xeb, 0x4d, 0x11,
		0x4d, 0x69, 0x0d, 0x04, 0x4f, 0x8a, 0xb3, 0xf6, 0xde, 0x9d, 0xef, 0xa0,
		0x8e, 0x9d, 0x7d, 0x23, 0x7f, 0x67, 0x0f, 0xe4,
		// Match ID 32 bytes
		0x79, 0xf8, 0x4e, 0xf6, 0xc6, 0x0e, 0x72, 0xed, 0xd3, 0x05, 0x04, 0x7c,
		0x01, 0x5d, 0x7b, 0x7a, 0xde, 0x64, 0x52, 0x5a, 0x30, 0x1f, 0xda, 0xc1,
		0x36, 0x97, 0x6f, 0x05, 0xed, 0xb6, 0x17, 0x2b,
		// Timestamp 8 bytes
		0x00, 0x00, 0x00, 0x00, 0x5d, 0x9f, 0x12, 0x00,
		// Coin ID
		0x3c, 0xda, 0xbd, 0x9b, 0xd6, 0x2d, 0xfb, 0xd7, 0xd8, 0xb0, 0x20, 0xd5,
		0xde, 0x4e, 0x64, 0x3b, 0x43, 0x98, 0x86, 0xf4, 0xb0, 0xdc, 0x86, 0xcb,
		0x8a, 0x56, 0xdf, 0xf8, 0xe6, 0x1c, 0x5e, 0xc3, 0x33, 0x48, 0x7a, 0x97,
		// Contract 8 bytes (shortened for testing)
		0xfc, 0x99, 0xf5, 0x76, 0xf8, 0xe0, 0xe5, 0xdc,
	}

	b := audit.Serialize()
	if !bytes.Equal(b, exp) {
		t.Fatalf("unexpected serialization. Wanted %x, got %x", exp, b)
	}

	auditB, err := json.Marshal(audit)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var auditBack Audit
	err = json.Unmarshal(auditB, &auditBack)
	if err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if !bytes.Equal(auditBack.MatchID, audit.MatchID) {
		t.Fatal(auditBack.MatchID, audit.MatchID)
	}
	if !bytes.Equal(auditBack.OrderID, audit.OrderID) {
		t.Fatal(auditBack.OrderID, audit.OrderID)
	}
	if auditBack.Time != audit.Time {
		t.Fatal(auditBack.Time, audit.Time)
	}
	if !bytes.Equal(auditBack.Contract, audit.Contract) {
		t.Fatal(auditBack.Contract, audit.Contract)
	}
}

func TestRevokeMatch(t *testing.T) {
	// serialization: order id (32) + match id (32)
	oid, _ := hex.DecodeString("47b903b6e71a1fff3ec1be25b23228bf2e8682b1502dc451f7a9aa32556123f2")
	mid, _ := hex.DecodeString("be218305e71b07a11c59c1b6c3ad3cf6ad4ed7582da8c639b87188aa95795c16")
	revoke := &RevokeMatch{
		OrderID: oid,
		MatchID: mid,
	}

	exp := []byte{
		// Order ID 32 bytes
		0x47, 0xb9, 0x03, 0xb6, 0xe7, 0x1a, 0x1f, 0xff, 0x3e, 0xc1, 0xbe, 0x25,
		0xb2, 0x32, 0x28, 0xbf, 0x2e, 0x86, 0x82, 0xb1, 0x50, 0x2d, 0xc4, 0x51,
		0xf7, 0xa9, 0xaa, 0x32, 0x55, 0x61, 0x23, 0xf2,
		// Match ID 32 bytes
		0xbe, 0x21, 0x83, 0x05, 0xe7, 0x1b, 0x07, 0xa1, 0x1c, 0x59, 0xc1, 0xb6,
		0xc3, 0xad, 0x3c, 0xf6, 0xad, 0x4e, 0xd7, 0x58, 0x2d, 0xa8, 0xc6, 0x39,
		0xb8, 0x71, 0x88, 0xaa, 0x95, 0x79, 0x5c, 0x16,
	}

	b := revoke.Serialize()
	if !bytes.Equal(b, exp) {
		t.Fatalf("unexpected serialization. Wanted %x, got %x", exp, b)
	}

	revB, err := json.Marshal(revoke)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var revokeBack RevokeMatch
	err = json.Unmarshal(revB, &revokeBack)
	if err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if !bytes.Equal(revokeBack.MatchID, revoke.MatchID) {
		t.Fatal(revokeBack.MatchID, revoke.MatchID)
	}
	if !bytes.Equal(revokeBack.OrderID, revoke.OrderID) {
		t.Fatal(revokeBack.OrderID, revoke.OrderID)
	}
}

func TestRedeem(t *testing.T) {
	// Redeem serialization is orderid (32) + matchid (32) + coin ID (36) +
	// secret (32) = 132
	oid, _ := hex.DecodeString("ee17139af2d86bd6052829389c0531f71042ed0b0539e617213a9a7151215a1b")
	mid, _ := hex.DecodeString("6ea1227b03d7bf05ce1e23f3edf57368f69ba9ee0cc069f09ab0952a36d964c5")
	coinid, _ := hex.DecodeString("28cb86e678f647cc88da734eed11286dab18b8483feb04580e3cbc90555a004700000005")
	secret, _ := hex.DecodeString("f1df467afb1e0803cefa25e76cf00e07a99416087f6c9d10921bbbb55be5ded9")
	redeem := &Redeem{
		OrderID: oid,
		MatchID: mid,
		CoinID:  coinid,
		Secret:  secret,
	}

	exp := []byte{
		// Order ID 32 bytes
		0xee, 0x17, 0x13, 0x9a, 0xf2, 0xd8, 0x6b, 0xd6, 0x05, 0x28, 0x29, 0x38,
		0x9c, 0x05, 0x31, 0xf7, 0x10, 0x42, 0xed, 0x0b, 0x05, 0x39, 0xe6, 0x17,
		0x21, 0x3a, 0x9a, 0x71, 0x51, 0x21, 0x5a, 0x1b,
		// Match ID 32 bytes
		0x6e, 0xa1, 0x22, 0x7b, 0x03, 0xd7, 0xbf, 0x05, 0xce, 0x1e, 0x23, 0xf3,
		0xed, 0xf5, 0x73, 0x68, 0xf6, 0x9b, 0xa9, 0xee, 0x0c, 0xc0, 0x69, 0xf0,
		0x9a, 0xb0, 0x95, 0x2a, 0x36, 0xd9, 0x64, 0xc5,
		// Coin ID, 36 Bytes
		0x28, 0xcb, 0x86, 0xe6, 0x78, 0xf6, 0x47, 0xcc, 0x88, 0xda, 0x73, 0x4e,
		0xed, 0x11, 0x28, 0x6d, 0xab, 0x18, 0xb8, 0x48, 0x3f, 0xeb, 0x04, 0x58,
		0x0e, 0x3c, 0xbc, 0x90, 0x55, 0x5a, 0x00, 0x47, 0x00, 0x00, 0x00, 0x05,
		// Secret 32 bytes
		0xf1, 0xdf, 0x46, 0x7a, 0xfb, 0x1e, 0x08, 0x03, 0xce, 0xfa, 0x25, 0xe7,
		0x6c, 0xf0, 0x0e, 0x07, 0xa9, 0x94, 0x16, 0x08, 0x7f, 0x6c, 0x9d, 0x10,
		0x92, 0x1b, 0xbb, 0xb5, 0x5b, 0xe5, 0xde, 0xd9,
	}

	b := redeem.Serialize()
	if !bytes.Equal(b, exp) {
		t.Fatalf("unexpected serialization. Wanted %x, got %x", exp, b)
	}

	redeemB, err := json.Marshal(redeem)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var redeemBack Redeem
	err = json.Unmarshal(redeemB, &redeemBack)
	if err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if !bytes.Equal(redeemBack.MatchID, redeem.MatchID) {
		t.Fatal(redeemBack.MatchID, redeem.MatchID)
	}
	if !bytes.Equal(redeemBack.OrderID, redeem.OrderID) {
		t.Fatal(redeemBack.OrderID, redeem.OrderID)
	}
	if !bytes.Equal(redeemBack.CoinID, redeem.CoinID) {
		t.Fatal(redeemBack.CoinID, redeem.CoinID)
	}
}

func TestRedemption(t *testing.T) {
	// serialization: orderid (32) + matchid (32) + txid (probably 64) + vout (4)
	// + timestamp (8)
	oid, _ := hex.DecodeString("ee17139af2d86bd6052829389c0531f71042ed0b0539e617213a9a7151215a1b")
	mid, _ := hex.DecodeString("6ea1227b03d7bf05ce1e23f3edf57368f69ba9ee0cc069f09ab0952a36d964c5")
	coinid, _ := hex.DecodeString("28cb86e678f647cc88da734eed11286dab18b8483feb04580e3cbc90555a004700000005")
	redeem := &Redemption{
		Redeem: Redeem{
			OrderID: oid,
			MatchID: mid,
			CoinID:  coinid,
		},
		Time: 1570706834,
	}

	exp := []byte{
		// Order ID 32 bytes
		0xee, 0x17, 0x13, 0x9a, 0xf2, 0xd8, 0x6b, 0xd6, 0x05, 0x28, 0x29, 0x38,
		0x9c, 0x05, 0x31, 0xf7, 0x10, 0x42, 0xed, 0x0b, 0x05, 0x39, 0xe6, 0x17,
		0x21, 0x3a, 0x9a, 0x71, 0x51, 0x21, 0x5a, 0x1b,
		// Match ID 32 bytes
		0x6e, 0xa1, 0x22, 0x7b, 0x03, 0xd7, 0xbf, 0x05, 0xce, 0x1e, 0x23, 0xf3,
		0xed, 0xf5, 0x73, 0x68, 0xf6, 0x9b, 0xa9, 0xee, 0x0c, 0xc0, 0x69, 0xf0,
		0x9a, 0xb0, 0x95, 0x2a, 0x36, 0xd9, 0x64, 0xc5,
		// Coin ID, 36 Bytes
		0x28, 0xcb, 0x86, 0xe6, 0x78, 0xf6, 0x47, 0xcc, 0x88, 0xda, 0x73, 0x4e,
		0xed, 0x11, 0x28, 0x6d, 0xab, 0x18, 0xb8, 0x48, 0x3f, 0xeb, 0x04, 0x58,
		0x0e, 0x3c, 0xbc, 0x90, 0x55, 0x5a, 0x00, 0x47, 0x00, 0x00, 0x00, 0x05,
		// Timestamp 8 bytes
		0x00, 0x00, 0x00, 0x00, 0x5d, 0x9f, 0x15, 0x92,
	}

	b := redeem.Serialize()
	if !bytes.Equal(b, exp) {
		t.Fatalf("unexpected serialization. Wanted %x, got %x", exp, b)
	}

	redeemB, err := json.Marshal(redeem)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var redeemBack Redemption
	err = json.Unmarshal(redeemB, &redeemBack)
	if err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if !bytes.Equal(redeemBack.MatchID, redeem.MatchID) {
		t.Fatal(redeemBack.MatchID, redeem.MatchID)
	}
	if !bytes.Equal(redeemBack.OrderID, redeem.OrderID) {
		t.Fatal(redeemBack.OrderID, redeem.OrderID)
	}
	if !bytes.Equal(redeemBack.CoinID, redeem.CoinID) {
		t.Fatal(redeemBack.CoinID, redeem.CoinID)
	}
	if redeemBack.Time != redeem.Time {
		t.Fatal(redeemBack.Time, redeem.Time)
	}
}

func TestPrefix(t *testing.T) {
	// serialization: account ID (32) + base asset (4) + quote asset (4) +
	// order type (1), client time (8), server time (8) = 57 bytes
	acctID, _ := hex.DecodeString("05bf0f2b97fa551375b9c92687f7a948a8f4a4237653a04e6b00c6f14c72fd1e")
	commit := []byte{
		0xd9, 0x83, 0xec, 0xdf, 0x34, 0x0f, 0xd9, 0xaf, 0xda, 0xb8, 0x81,
		0x8d, 0x5a, 0x29, 0x36, 0xe0, 0x71, 0xaf, 0x3c, 0xbb, 0x3d, 0xa8,
		0xac, 0xf4, 0x38, 0xb6, 0xc2, 0x91, 0x65, 0xf2, 0x0d, 0x8d,
	}
	prefix := &Prefix{
		AccountID:  acctID,
		Base:       256,
		Quote:      65536,
		OrderType:  1,
		ClientTime: 1571871297,
		ServerTime: 1571871841,
		Commit:     commit,
	}

	exp := []byte{
		// Account ID 32 bytes
		0x05, 0xbf, 0x0f, 0x2b, 0x97, 0xfa, 0x55, 0x13, 0x75, 0xb9, 0xc9, 0x26,
		0x87, 0xf7, 0xa9, 0x48, 0xa8, 0xf4, 0xa4, 0x23, 0x76, 0x53, 0xa0, 0x4e,
		0x6b, 0x00, 0xc6, 0xf1, 0x4c, 0x72, 0xfd, 0x1e,
		// Base Asset 4 bytes
		0x00, 0x00, 0x01, 0x00,
		// Quote Asset 4 bytes
		0x00, 0x01, 0x00, 0x00,
		// Order Type 1 bytes
		0x01,
		// Client Time 8 bytes
		0x00, 0x00, 0x00, 0x00, 0x5d, 0xb0, 0xda, 0x41,
		// Server Time 8 bytes (zeros for client signature)
		0x00, 0x00, 0x00, 0x00, 0x5d, 0xb0, 0xdc, 0x61,
		// Commitment
		0xd9, 0x83, 0xec, 0xdf, 0x34, 0x0f, 0xd9, 0xaf, 0xda, 0xb8, 0x81,
		0x8d, 0x5a, 0x29, 0x36, 0xe0, 0x71, 0xaf, 0x3c, 0xbb, 0x3d, 0xa8,
		0xac, 0xf4, 0x38, 0xb6, 0xc2, 0x91, 0x65, 0xf2, 0x0d, 0x8d,
	}

	b := prefix.Serialize()
	if !bytes.Equal(b, exp) {
		t.Fatalf("unexpected serialization. Wanted %x, got %x", exp, b)
	}

	prefixB, err := json.Marshal(prefix)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var prefixBack Prefix
	err = json.Unmarshal(prefixB, &prefixBack)
	if err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if !bytes.Equal(prefixBack.AccountID, prefix.AccountID) {
		t.Fatalf("wrong account id. wanted %d, got %d", prefix.AccountID, prefixBack.AccountID)
	}
	if prefixBack.Base != prefix.Base {
		t.Fatalf("wrong base asset. wanted %d, got %d", prefix.Base, prefixBack.Base)
	}
	if prefixBack.Quote != prefix.Quote {
		t.Fatalf("wrong quote asset. wanted %d, got %d", prefix.Quote, prefixBack.Quote)
	}
	if prefixBack.OrderType != prefix.OrderType {
		t.Fatalf("wrong order type. wanted %d, got %d", prefix.OrderType, prefixBack.OrderType)
	}
	if prefixBack.ClientTime != prefix.ClientTime {
		t.Fatalf("wrong client time. wanted %d, got %d", prefix.ClientTime, prefixBack.ClientTime)
	}
	if prefixBack.ServerTime != prefix.ServerTime {
		t.Fatalf("wrong server time. wanted %d, got %d", prefix.ServerTime, prefixBack.ServerTime)
	}
	if !bytes.Equal(prefixBack.Commit, prefix.Commit) {
		t.Fatalf("wrong commitment. wanted %d, got %d", prefix.Commit, prefixBack.Commit)
	}
}

func TestTrade(t *testing.T) {
	// serialization: coin count (1), coin IDs (36*count), side (1), qty (8)
	// = 10 + 36*count

	addr := "13DePXLAKNsFCSmgfrEsYm8G1aCVZdYvP9"
	coin1 := randomCoin()
	coin2 := randomCoin()

	trade := &Trade{
		Side:     1,
		Quantity: 600_000_000,
		Coins:    []*Coin{coin1, coin2},
		Address:  addr,
	}

	// Coin count
	b := trade.Serialize()
	if b[0] != 0x02 {
		t.Fatalf("coin count byte incorrect: %d", b[0])
	}
	b = b[1:]

	// first coin
	cLen := len(coin1.ID)
	if !bytes.Equal(b[:cLen], coin1.ID) {
		t.Fatal(b[:cLen], coin1.ID)
	}
	b = b[cLen:]

	// second coin
	cLen = len(coin2.ID)
	if !bytes.Equal(b[:cLen], coin2.ID) {
		t.Fatal(b[:cLen], coin2.ID)
	}
	b = b[cLen:]

	// side
	if b[0] != 0x01 {
		t.Fatalf("wrong side. wanted 1, got %d", b[0])
	}
	b = b[1:]

	qty := []byte{0x00, 0x00, 0x00, 0x00, 0x23, 0xc3, 0x46, 0x00}
	if !bytes.Equal(b, qty) {
		t.Fatal(b, qty)
	}
}

func TestLimit(t *testing.T) {
	// serialization: prefix (105) + trade (variable) + address (~35)
	// = 140 + len(trade)
	prefix := &Prefix{
		AccountID:  randomBytes(32),
		Base:       256,
		Quote:      65536,
		OrderType:  1,
		ClientTime: 1571874397,
		ServerTime: 1571874405,
		Commit:     randomBytes(32),
	}
	addr := "DsDePXLAKNsFCSmgfrEsYm8G1aCVZdYvP9"
	coin1 := randomCoin()
	coin2 := randomCoin()
	trade := &Trade{
		Side:     1,
		Quantity: 600_000_000,
		Coins:    []*Coin{coin1, coin2},
		Address:  addr,
	}
	limit := &LimitOrder{
		Prefix: *prefix,
		Trade:  *trade,
		Rate:   350_000_000,
		TiF:    1,
	}

	b := limit.Serialize()

	// Compare the prefix byte-for-byte and pop it from the front.
	x := prefix.Serialize()
	xLen := len(x)
	if !bytes.Equal(b[:xLen], x) {
		t.Fatal(x, b[:xLen])
	}
	b = b[xLen:]

	// Compare the trade byte-for-byte and pop it from the front.
	x = trade.Serialize()
	xLen = len(x)
	if !bytes.Equal(b[:xLen], x) {
		t.Fatal(x, b[:xLen])
	}
	b = b[xLen:]

	exp := []byte{
		// Rate 8 bytes
		0x00, 0x00, 0x00, 0x00, 0x14, 0xdc, 0x93, 0x80,
		// Time-in-force 1 byte
		0x01,
		// Address 35 bytes
		0x44, 0x73, 0x44, 0x65, 0x50, 0x58, 0x4c, 0x41, 0x4b, 0x4e, 0x73, 0x46,
		0x43, 0x53, 0x6d, 0x67, 0x66, 0x72, 0x45, 0x73, 0x59, 0x6d, 0x38, 0x47,
		0x31, 0x61, 0x43, 0x56, 0x5a, 0x64, 0x59, 0x76, 0x50, 0x39,
	}
	if !bytes.Equal(exp, b) {
		t.Fatal(exp, b)
	}

	limitB, err := json.Marshal(limit)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var limitBack LimitOrder
	err = json.Unmarshal(limitB, &limitBack)
	if err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	comparePrefix(t, &limitBack.Prefix, &limit.Prefix)
	compareTrade(t, &limitBack.Trade, &limit.Trade)
	if limitBack.Rate != limit.Rate {
		t.Fatal(limitBack.Rate, limit.Rate)
	}
	if limitBack.TiF != limit.TiF {
		t.Fatal(limitBack.TiF, limit.TiF)
	}
}

func TestMarket(t *testing.T) {
	// serialization: prefix (105) + trade (variable) + rate (8)
	// + time-in-force (1) + address (~35) = 149 + len(trade)
	prefix := &Prefix{
		AccountID:  randomBytes(32),
		Base:       256,
		Quote:      65536,
		OrderType:  1,
		ClientTime: 1571874397,
		ServerTime: 1571874405,
		Commit:     randomBytes(32),
	}
	addr := "16brznLu4ieZ6tToKfUgibD94UcqshGUE3"
	coin1 := randomCoin()
	coin2 := randomCoin()
	trade := &Trade{
		Side:     1,
		Quantity: 600_000_000,
		Coins:    []*Coin{coin1, coin2},
		Address:  addr,
	}
	market := &MarketOrder{
		Prefix: *prefix,
		Trade:  *trade,
	}

	b := market.Serialize()

	// Compare the prefix byte-for-byte and pop it from the front.
	x := prefix.Serialize()
	xLen := len(x)
	if !bytes.Equal(b[:xLen], x) {
		t.Fatal(x, b[:xLen])
	}
	b = b[xLen:]

	// Compare the trade data byte-for-byte and pop it from the front.
	x = trade.Serialize()
	xLen = len(x)
	if !bytes.Equal(b[:xLen], x) {
		t.Fatal(x, b[:xLen])
	}
	b = b[xLen:]

	// The only thing left should be the utf-8 encoded address.
	addrBytes := []byte{
		0x31, 0x36, 0x62, 0x72, 0x7a, 0x6e, 0x4c, 0x75, 0x34, 0x69, 0x65, 0x5a,
		0x36, 0x74, 0x54, 0x6f, 0x4b, 0x66, 0x55, 0x67, 0x69, 0x62, 0x44, 0x39,
		0x34, 0x55, 0x63, 0x71, 0x73, 0x68, 0x47, 0x55, 0x45, 0x33,
	}
	if !bytes.Equal(b, addrBytes) {
		t.Fatal(b, addrBytes)
	}

	marketB, err := json.Marshal(market)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var marketBack MarketOrder
	err = json.Unmarshal(marketB, &marketBack)
	if err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	comparePrefix(t, &marketBack.Prefix, &market.Prefix)
	compareTrade(t, &marketBack.Trade, &market.Trade)
}

func TestCancel(t *testing.T) {
	// serialization: prefix (105) + target id (32) = 137
	prefix := &Prefix{
		AccountID:  randomBytes(32),
		Base:       256,
		Quote:      65536,
		OrderType:  1,
		ClientTime: 1571874397,
		ServerTime: 1571874405,
		Commit:     randomBytes(32),
	}
	targetID, _ := hex.DecodeString("a1f1b66916353b58dbb65562eb19731953b2f1215987a9d9137f0df3458637b7")
	cancel := &CancelOrder{
		Prefix:   *prefix,
		TargetID: targetID,
	}

	b := cancel.Serialize()

	// Compare the prefix byte-for-byte and pop it from the front.
	x := prefix.Serialize()
	xLen := len(x)
	if !bytes.Equal(x, b[:xLen]) {
		t.Fatal(x, b[:xLen])
	}
	b = b[xLen:]

	target := []byte{
		0xa1, 0xf1, 0xb6, 0x69, 0x16, 0x35, 0x3b, 0x58, 0xdb, 0xb6, 0x55, 0x62,
		0xeb, 0x19, 0x73, 0x19, 0x53, 0xb2, 0xf1, 0x21, 0x59, 0x87, 0xa9, 0xd9,
		0x13, 0x7f, 0x0d, 0xf3, 0x45, 0x86, 0x37, 0xb7,
	}
	if !bytes.Equal(b, target) {
		t.Fatal(b, target)
	}

	cancelB, err := json.Marshal(cancel)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var cancelBack CancelOrder
	err = json.Unmarshal(cancelB, &cancelBack)
	if err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	comparePrefix(t, &cancelBack.Prefix, &cancel.Prefix)
	if !bytes.Equal(cancelBack.TargetID, cancel.TargetID) {
		t.Fatal(cancelBack.TargetID, cancel.TargetID)
	}
}

func TestConnect(t *testing.T) {
	// serialization: account ID (32) + api version (2) + timestamp (8) = 42 bytes
	acctID, _ := hex.DecodeString("14ae3cbc703587122d68ac6fa9194dfdc8466fb5dec9f47d2805374adff3e016")
	connect := &Connect{
		AccountID:  acctID,
		APIVersion: uint16(1),
		Time:       uint64(1571575096),
	}

	exp := []byte{
		// Account ID 32 bytes
		0x14, 0xae, 0x3c, 0xbc, 0x70, 0x35, 0x87, 0x12, 0x2d, 0x68, 0xac, 0x6f,
		0xa9, 0x19, 0x4d, 0xfd, 0xc8, 0x46, 0x6f, 0xb5, 0xde, 0xc9, 0xf4, 0x7d,
		0x28, 0x05, 0x37, 0x4a, 0xdf, 0xf3, 0xe0, 0x16,
		// API Version 2 bytes
		0x00, 0x01,
		// Time 8 bytes
		0x00, 0x00, 0x00, 0x00, 0x5d, 0xac, 0x55, 0x38,
	}

	b := connect.Serialize()
	if !bytes.Equal(b, exp) {
		t.Fatalf("unexpected serialization. Wanted %x, got %x", exp, b)
	}

	connectB, err := json.Marshal(connect)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var connectBack Connect
	err = json.Unmarshal(connectB, &connectBack)
	if err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if !bytes.Equal(connectBack.AccountID, connect.AccountID) {
		t.Fatal(connectBack.AccountID, connect.AccountID)
	}
	if connectBack.APIVersion != connect.APIVersion {
		t.Fatal(connectBack.APIVersion, connect.APIVersion)
	}
	if connectBack.Time != connect.Time {
		t.Fatal(connectBack.Time, connect.Time)
	}
}

func TestRegister(t *testing.T) {
	// serialization: pubkey (33) + time (8) = 41
	pk, _ := hex.DecodeString("f06e5cf13fc6debb8b90776da6624991ba50a11e784efed53d0a81c3be98397982")
	register := &Register{
		PubKey: pk,
		Time:   uint64(1571700077),
	}

	exp := []byte{
		// PubKey 33 bytes
		0xf0, 0x6e, 0x5c, 0xf1, 0x3f, 0xc6, 0xde, 0xbb, 0x8b, 0x90, 0x77, 0x6d,
		0xa6, 0x62, 0x49, 0x91, 0xba, 0x50, 0xa1, 0x1e, 0x78, 0x4e, 0xfe, 0xd5,
		0x3d, 0x0a, 0x81, 0xc3, 0xbe, 0x98, 0x39, 0x79, 0x82,
		// Time 8 bytes
		0x00, 0x00, 0x00, 0x00, 0x5d, 0xae, 0x3d, 0x6d,
	}

	b := register.Serialize()
	if !bytes.Equal(b, exp) {
		t.Fatalf("unexpected serialization. Wanted %x, got %x", exp, b)
	}

	registerB, err := json.Marshal(register)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var registerBack Register
	err = json.Unmarshal(registerB, &registerBack)
	if err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if !bytes.Equal(registerBack.PubKey, register.PubKey) {
		t.Fatal(registerBack.PubKey, register.PubKey)
	}
	if registerBack.Time != register.Time {
		t.Fatal(registerBack.Time, register.Time)
	}
}

func TestRegisterResult(t *testing.T) {
	// serialization: pubkey (33) + client pubkey (33) + time (8) + fee (8) +
	// address (35-ish) = 117
	dexPK, _ := hex.DecodeString("511a26bd3db115fd63e4093471227532b7264b125b8cad596bf4f15ed57ef1564d")
	clientPK, _ := hex.DecodeString("405441ebff6608bdc59f2fbb5020d9b30ca1cb6e8b11ca597997b1e37cadb550b9")
	address := "Dcur2mcGjmENx4DhNqDctW5wJCVyT3Qeqkx"
	regRes := &RegisterResult{
		DEXPubKey:    dexPK,
		ClientPubKey: clientPK,
		Address:      address,
		Time:         1571701946,
		Fee:          100_000_000,
	}

	exp := []byte{
		// DEX PubKey 33 bytes
		0x51, 0x1a, 0x26, 0xbd, 0x3d, 0xb1, 0x15, 0xfd, 0x63, 0xe4, 0x09, 0x34,
		0x71, 0x22, 0x75, 0x32, 0xb7, 0x26, 0x4b, 0x12, 0x5b, 0x8c, 0xad, 0x59,
		0x6b, 0xf4, 0xf1, 0x5e, 0xd5, 0x7e, 0xf1, 0x56, 0x4d,
		// Client PubKey 33 bytes
		0x40, 0x54, 0x41, 0xeb, 0xff, 0x66, 0x08, 0xbd, 0xc5, 0x9f, 0x2f, 0xbb,
		0x50, 0x20, 0xd9, 0xb3, 0x0c, 0xa1, 0xcb, 0x6e, 0x8b, 0x11, 0xca, 0x59,
		0x79, 0x97, 0xb1, 0xe3, 0x7c, 0xad, 0xb5, 0x50, 0xb9,
		// Time 8 Bytes
		0x00, 0x00, 0x00, 0x00, 0x5d, 0xae, 0x44, 0xba,
		// Fee 8 bytes
		0x00, 0x00, 0x00, 0x00, 0x05, 0xf5, 0xe1, 0x00,
		// Address 35 bytes
		0x44, 0x63, 0x75, 0x72, 0x32, 0x6d, 0x63, 0x47, 0x6a, 0x6d, 0x45, 0x4e,
		0x78, 0x34, 0x44, 0x68, 0x4e, 0x71, 0x44, 0x63, 0x74, 0x57, 0x35, 0x77,
		0x4a, 0x43, 0x56, 0x79, 0x54, 0x33, 0x51, 0x65, 0x71, 0x6b, 0x78,
	}

	b := regRes.Serialize()
	if !bytes.Equal(b, exp) {
		t.Fatalf("unexpected serialization. Wanted %x, got %x", exp, b)
	}

	regResB, err := json.Marshal(regRes)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var regResBack RegisterResult
	err = json.Unmarshal(regResB, &regResBack)
	if err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if !bytes.Equal(regResBack.DEXPubKey, regRes.DEXPubKey) {
		t.Fatal(regResBack.DEXPubKey, regRes.DEXPubKey)
	}
	// We don't check the ClientPubKey, since it is not encoded in the JSON
	// message. The client must add it themselves before serialization.
	if regResBack.Address != regRes.Address {
		t.Fatal(regResBack.Address, regRes.Address)
	}
	if regResBack.Time != regRes.Time {
		t.Fatal(regResBack.Time, regRes.Time)
	}
	if regResBack.Fee != regRes.Fee {
		t.Fatal(regResBack.Fee, regRes.Fee)
	}
}

func TestNotifyFee(t *testing.T) {
	// serialization: account id (32) + txid (32) + vout (4) = 68
	acctID, _ := hex.DecodeString("bd3faf7353b8fc40618527687b3ef99d00da480e354f2c4986479e2da626acf5")
	coinid, _ := hex.DecodeString("51891f751b0dd987c0b8ff1703cd0dd3e2712847f4bdbc268c9656dc80d233c7")
	notify := &NotifyFee{
		AccountID: acctID,
		CoinID:    coinid,
		Time:      1571704611,
	}

	exp := []byte{
		// Account ID 32 bytes
		0xbd, 0x3f, 0xaf, 0x73, 0x53, 0xb8, 0xfc, 0x40, 0x61, 0x85, 0x27, 0x68,
		0x7b, 0x3e, 0xf9, 0x9d, 0x00, 0xda, 0x48, 0x0e, 0x35, 0x4f, 0x2c, 0x49,
		0x86, 0x47, 0x9e, 0x2d, 0xa6, 0x26, 0xac, 0xf5,
		// Tx ID 32 bytes
		0x51, 0x89, 0x1f, 0x75, 0x1b, 0x0d, 0xd9, 0x87, 0xc0, 0xb8, 0xff, 0x17,
		0x03, 0xcd, 0x0d, 0xd3, 0xe2, 0x71, 0x28, 0x47, 0xf4, 0xbd, 0xbc, 0x26,
		0x8c, 0x96, 0x56, 0xdc, 0x80, 0xd2, 0x33, 0xc7,
		// Time 8 bytes
		0x00, 0x00, 0x00, 0x00, 0x5d, 0xae, 0x4f, 0x23,
	}

	b := notify.Serialize()
	if !bytes.Equal(b, exp) {
		t.Fatalf("unexpected serialization. Wanted %x, got %x", exp, b)
	}

	notifyB, err := json.Marshal(notify)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var notifyBack NotifyFee
	err = json.Unmarshal(notifyB, &notifyBack)
	if err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if !bytes.Equal(notifyBack.AccountID, notify.AccountID) {
		t.Fatal(notifyBack.AccountID, notify.AccountID)
	}
	if !bytes.Equal(notifyBack.CoinID, notify.CoinID) {
		t.Fatal(notifyBack.CoinID, notify.CoinID)
	}
	if notifyBack.Time != notify.Time {
		t.Fatal(notifyBack.Time, notify.Time)
	}
}

func TestSignable(t *testing.T) {
	sig := []byte{
		0x07, 0xad, 0x7f, 0x33, 0xc5, 0xb0, 0x13, 0xa1, 0xbb, 0xd6, 0xad, 0xc0,
		0xd2, 0x16, 0xd8, 0x93, 0x8c, 0x73, 0x64, 0xe5, 0x6a, 0x17, 0x8c, 0x7a,
		0x17, 0xa9, 0xe7, 0x47, 0xad, 0x55, 0xaf, 0xe6, 0x55, 0x2b, 0xb2, 0x76,
		0xf8, 0x8e, 0x34, 0x2e, 0x56, 0xac, 0xaa, 0x8a, 0x52, 0x41, 0x2e, 0x51,
		0x8b, 0x0f, 0xe6, 0xb2, 0x2a, 0x21, 0x77, 0x9a, 0x76, 0x99, 0xa5, 0xe5,
		0x39, 0xa8, 0xa1, 0xdd, 0x1d, 0x49, 0x8b, 0xb0, 0x16, 0xf7, 0x18, 0x70,
	}
	s := Signature{}
	s.SetSig(sig)
	if !bytes.Equal(sig, s.SigBytes()) {
		t.Fatalf("signatures not equal")
	}
}

func TestBytes(t *testing.T) {
	rawB := []byte{0xfc, 0xf6, 0xd9, 0xb9, 0xdb, 0x10, 0x4c, 0xc0, 0x13, 0x3a}
	hexB := "fcf6d9b9db104cc0133a"

	type byter struct {
		B Bytes `json:"b"`
	}

	b := byter{}
	js := `{"b":"` + hexB + `"}`
	err := json.Unmarshal([]byte(js), &b)
	if err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if !bytes.Equal(rawB, b.B) {
		t.Fatalf("unmarshalled Bytes not correct. wanted %x, got %x.", rawB, b.B)
	}

	marshalled, err := json.Marshal(b)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	if string(marshalled) != js {
		t.Fatalf("marshalled Bytes not correct. wanted %s, got %s", js, string(marshalled))
	}

	fromHex, _ := hex.DecodeString(hexB)
	if !bytes.Equal(rawB, fromHex) {
		t.Fatalf("hex-constructed Bytes not correct. wanted %x, got %x.", rawB, fromHex)
	}
}

func TestDecodeMessage(t *testing.T) {
	msg, err := DecodeMessage([]byte(`{"type":1,"route":"testroute","id":5,"payload":10}`))
	if err != nil {
		t.Fatalf("error decoding json message: %v", err)
	}
	if msg.Type != 1 {
		t.Fatalf("wrong message type. wanted 1, got %d", msg.Type)
	}
	if msg.Route != "testroute" {
		t.Fatalf("wrong message type. wanted 'testroute', got '%s'", msg.Route)
	}
	if msg.ID != 5 {
		t.Fatalf("wrong message type. wanted 5, got %d", msg.ID)
	}
	if string(msg.Payload) != "10" {
		t.Fatalf("wrong payload. wanted '10, got '%s'", string(msg.Payload))
	}
	// Test invalid json
	_, err = DecodeMessage([]byte(`{"type":?}`))
	if err == nil {
		t.Fatalf("no json decode error for invalid json")
	}
}

func TestRespReq(t *testing.T) {
	// Test invalid json result.
	_, err := NewResponse(5, make(chan int), nil)
	if err == nil {
		t.Fatalf("no error for invalid json")
	}
	// Zero ID not valid.
	_, err = NewResponse(0, 10, nil)
	if err == nil {
		t.Fatalf("no error for id = 0")
	}
	msg, err := NewResponse(5, 10, nil)
	if err != nil {
		t.Fatalf("NewResponse error: %v", err)
	}
	if msg.ID != 5 {
		t.Fatalf("wrong message ID. wanted 5, got ")
	}
	resp, err := msg.Response()
	if err != nil {
		t.Fatalf("error getting response payload: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error making success response")
	}
	if string(resp.Result) != "10" {
		t.Fatalf("unexpected result. wanted '10', got '%s'", string(resp.Result))
	}

	// Check error.
	msg, err = NewResponse(5, nil, NewError(15, "testmsg"))
	if err != nil {
		t.Fatalf("unexpected error making error response")
	}
	_, err = msg.Response()
	if err != nil {
		t.Fatalf("unexpected error getting error response payload: %v", err)
	}

	// Test Requests
	_, err = NewRequest(5, "testroute", make(chan int))
	if err == nil {
		t.Fatalf("no error for invalid json type request payload")
	}
	_, err = NewRequest(0, "testroute", 10)
	if err == nil {
		t.Fatalf("no error id = 0 request")
	}
	_, err = NewRequest(5, "", 10)
	if err == nil {
		t.Fatalf("no error for empty string route request")
	}
	msg, err = NewRequest(5, "testroute", 10)
	if err != nil {
		t.Fatalf("error for valid request payload: %v", err)
	}
	// A Request-type Message should error if trying to retrieve the
	// ResponsePayload
	_, err = msg.Response()
	if err == nil {
		t.Fatalf("no error when retrieving response payload from request-type message")
	}

	// Test Notifications
	_, err = NewNotification("testroute", make(chan int))
	if err == nil {
		t.Fatalf("no error for invalid json type notification payload")
	}
	_, err = NewNotification("", 10)
	if err == nil {
		t.Fatalf("no error for empty string route request")
	}
	_, err = NewNotification("testroute", 10)
	if err != nil {
		t.Fatalf("error for valid request payload: %v", err)
	}
}

func comparePrefix(t *testing.T, p1, p2 *Prefix) {
	if !bytes.Equal(p1.AccountID, p2.AccountID) {
		t.Fatal(p1.AccountID, p2.AccountID)
	}
	if p1.Base != p2.Base {
		t.Fatal(p1.Base, p2.Base)
	}
	if p1.Quote != p2.Quote {
		t.Fatal(p1.Quote, p2.Quote)
	}
	if p1.OrderType != p2.OrderType {
		t.Fatal(p1.OrderType, p2.OrderType)
	}
	if p1.ClientTime != p2.ClientTime {
		t.Fatal(p1.ClientTime, p2.ClientTime)
	}
	if p1.ServerTime != p2.ServerTime {
		t.Fatal(p1.ServerTime, p2.ServerTime)
	}
}

func compareTrade(t *testing.T, t1, t2 *Trade) {
	if t1.Side != t2.Side {
		t.Fatal(t1.Side, t2.Side)
	}
	if t1.Quantity != t2.Quantity {
		t.Fatal(t1.Quantity, t2.Quantity)
	}
	if len(t1.Coins) != 2 {
		t.Fatalf("wrong number of coins. expected 2 got %d", len(t1.Coins))
	}
	if t1.Address != t2.Address {
		t.Fatal(t1.Address, t2.Address)
	}
}

func randomBytes(len int) []byte {
	bytes := make([]byte, len)
	rand.Read(bytes)
	return bytes
}

func randomCoin() *Coin {
	return &Coin{
		ID: randomBytes(36),
		// the rest are not part of the serialized coins.
		PubKeys: []Bytes{randomBytes(33)},
		Sigs:    []Bytes{randomBytes(77)},
		Redeem:  randomBytes(25),
	}
}
