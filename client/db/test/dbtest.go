// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dbtest

import (
	"bytes"
	"math/rand"

	"decred.org/dcrdex/client/db"
	ordertest "decred.org/dcrdex/dex/order/test"
	"github.com/decred/dcrd/dcrec/secp256k1/v2"
)

// Generate a public key on the secp256k1 curve.
func randomPubKey() *secp256k1.PublicKey {
	_, pub := secp256k1.PrivKeyFromBytes(randBytes(32))
	return pub
}

func RandomAccountInfo() *db.AccountInfo {
	return &db.AccountInfo{
		URL:       ordertest.RandomAddress(),
		EncKey:    randBytes(32),
		DEXPubKey: randomPubKey(),
		FeeCoin:   randBytes(32),
	}
}

func randBytes(l int) []byte {
	b := make([]byte, l)
	rand.Read(b)
	return b
}

func RandomMatchProof(sparsity float64) *db.MatchProof {
	proof := new(db.MatchProof)
	doZero := func() bool { return rand.Intn(1000) < int(sparsity*1000) }
	if !doZero() {
		proof.MakerSwap = randBytes(36)
	}
	if !doZero() {
		proof.MakerRedeem = randBytes(36)
	}
	if !doZero() {
		proof.TakerSwap = randBytes(36)
	}
	if !doZero() {
		proof.TakerRedeem = randBytes(36)
	}
	if !doZero() {
		proof.Sigs.Match = randBytes(73)
	}
	if !doZero() {
		proof.Sigs.Init = randBytes(73)
	}
	if !doZero() {
		proof.Sigs.Audit = randBytes(73)
	}
	if !doZero() {
		proof.Sigs.Redeem = randBytes(73)
	}
	if !doZero() {
		proof.Sigs.Redemption = randBytes(73)
	}
	return proof
}

type testKiller interface {
	Fatalf(string, ...interface{})
}

func MustCompareMatchSigs(t testKiller, s1, s2 *db.MatchSignatures) {
	if !bytes.Equal(s1.Match, s2.Match) {
		t.Fatalf("Match mismatch. %x != %x", s1.Match, s2.Match)
	}
	if !bytes.Equal(s1.Init, s2.Init) {
		t.Fatalf("Init mismatch. %x != %x", s1.Init, s2.Init)
	}
	if !bytes.Equal(s1.Audit, s2.Audit) {
		t.Fatalf("Audit mismatch. %x != %x", s1.Audit, s2.Audit)
	}
	if !bytes.Equal(s1.Redeem, s2.Redeem) {
		t.Fatalf("Redeem mismatch. %x != %x", s1.Redeem, s2.Redeem)
	}
	if !bytes.Equal(s1.Redemption, s2.Redemption) {
		t.Fatalf("Redemption mismatch. %x != %x", s1.Redemption, s2.Redemption)
	}
}

func MustCompareMatchProof(t testKiller, m1, m2 *db.MatchProof) {
	if !bytes.Equal(m1.MakerSwap, m2.MakerSwap) {
		t.Fatalf("MakerSwap mismatch. %x != %x", m1.MakerSwap, m2.MakerSwap)
	}
	if !bytes.Equal(m1.MakerRedeem, m2.MakerRedeem) {
		t.Fatalf("MakerRedeem mismatch. %x != %x", m1.MakerRedeem, m2.MakerRedeem)
	}
	if !bytes.Equal(m1.TakerSwap, m2.TakerSwap) {
		t.Fatalf("TakerSwap mismatch. %x != %x", m1.TakerSwap, m2.TakerSwap)
	}
	if !bytes.Equal(m1.TakerRedeem, m2.TakerRedeem) {
		t.Fatalf("TakerRedeem mismatch. %x != %x", m1.TakerRedeem, m2.TakerRedeem)
	}
	MustCompareMatchSigs(t, &m1.Sigs, &m2.Sigs)
}

func MustCompareAccountInfo(t testKiller, a1, a2 *db.AccountInfo) {
	if a1.URL != a2.URL {
		t.Fatalf("URL mismatch. %s != %s", a1.URL, a2.URL)
	}
	if !bytes.Equal(a1.EncKey, a2.EncKey) {
		t.Fatalf("EncKey mismatch. %x != %x", a1.EncKey, a2.EncKey)
	}
	if !bytes.Equal(a1.DEXPubKey.SerializeCompressed(), a2.DEXPubKey.SerializeCompressed()) {
		t.Fatalf("EncKey mismatch. %x != %x",
			a1.DEXPubKey.SerializeCompressed(), a2.DEXPubKey.SerializeCompressed())
	}
	if !bytes.Equal(a1.FeeCoin, a2.FeeCoin) {
		t.Fatalf("EncKey mismatch. %x != %x", a1.FeeCoin, a2.FeeCoin)
	}
}

func MustCompareOrderProof(t testKiller, p1, p2 *db.OrderProof) {
	if !bytes.Equal(p1.DEXSig, p2.DEXSig) {
		t.Fatalf("DEXSig mismatch. %x != %x", p1.DEXSig, p2.DEXSig)
	}
}
