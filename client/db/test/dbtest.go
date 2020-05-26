// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dbtest

import (
	"bytes"
	"math/rand"
	"strings"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/db"
	ordertest "decred.org/dcrdex/dex/order/test"
	"github.com/decred/dcrd/dcrec/secp256k1/v2"
)

// Generate a public key on the secp256k1 curve.
func randomPubKey() *secp256k1.PublicKey {
	_, pub := secp256k1.PrivKeyFromBytes(randBytes(32))
	return pub
}

func randString(maxLen int) string {
	s := string(randBytes(int(randBytes(1)[0])))
	if len(s) > maxLen {
		s = s[:maxLen]
	}
	return s
}

// RandomAccountInfo creates an AccountInfo with random values.
func RandomAccountInfo() *db.AccountInfo {
	return &db.AccountInfo{
		URL:       ordertest.RandomAddress(),
		EncKey:    randBytes(32),
		DEXPubKey: randomPubKey(),
		FeeCoin:   randBytes(32),
		Cert:      randBytes(100),
	}
}

// RandomWallet creates a random wallet.
func RandomWallet() *db.Wallet {
	return &db.Wallet{
		AssetID: rand.Uint32(),
		Account: ordertest.RandomAddress(),
		Settings: map[string]string{
			ordertest.RandomAddress(): ordertest.RandomAddress(),
			ordertest.RandomAddress(): ordertest.RandomAddress(),
			ordertest.RandomAddress(): ordertest.RandomAddress(),
			ordertest.RandomAddress(): ordertest.RandomAddress(),
		},
		Balance: &asset.Balance{
			Available: rand.Uint64(),
			Immature:  rand.Uint64(),
			Locked:    rand.Uint64(),
		},
		BalUpdate: time.Now().Truncate(time.Millisecond).UTC(),
		Address:   ordertest.RandomAddress(),
	}
}

func randBytes(l int) []byte {
	b := make([]byte, l)
	rand.Read(b)
	return b
}

// RandomMatchProof creates a match proof with random values. Set the sparsity
// to change how many fields are populated, with some variation around the ratio
// supplied. 0 < sparsity < 1.
func RandomMatchProof(sparsity float64) *db.MatchProof {
	proof := new(db.MatchProof)
	doZero := func() bool { return rand.Intn(1000) < int(sparsity*1000) }
	if !doZero() {
		proof.CounterScript = randBytes(75)
	}
	if !doZero() {
		proof.SecretHash = randBytes(32)
	}
	if !doZero() {
		proof.Secret = randBytes(32)
	}
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
		proof.Auth.MatchSig = randBytes(73)
	}
	if !doZero() {
		proof.Auth.MatchStamp = rand.Uint64()
	}
	if !doZero() {
		proof.Auth.InitSig = randBytes(73)
	}
	if !doZero() {
		proof.Auth.InitStamp = rand.Uint64()
	}
	if !doZero() {
		proof.Auth.AuditSig = randBytes(73)
	}
	if !doZero() {
		proof.Auth.AuditStamp = rand.Uint64()
	}
	if !doZero() {
		proof.Auth.RedeemSig = randBytes(73)
	}
	if !doZero() {
		proof.Auth.RedeemStamp = rand.Uint64()
	}
	if !doZero() {
		proof.Auth.RedemptionSig = randBytes(73)
	}
	if !doZero() {
		proof.Auth.RedemptionStamp = rand.Uint64()
	}
	return proof
}

func RandomNotification(maxTime uint64) *db.Notification {
	return &db.Notification{
		NoteType:    randString(25),
		SubjectText: randString(255),
		DetailText:  randString(255),
		// Since this is for DB tests, only use severity level >= Success.
		Severeness: db.Severity(rand.Intn(3)) + db.Success,
		TimeStamp:  uint64(rand.Int63n(int64(maxTime))),
	}
}

type testKiller interface {
	Fatalf(string, ...interface{})
}

// MustCompareMatchAuth ensures the two MatchAuth are identical, calling the
// Fatalf method of the testKiller if not.
func MustCompareMatchAuth(t testKiller, a1, a2 *db.MatchAuth) {
	if !bytes.Equal(a1.MatchSig, a2.MatchSig) {
		t.Fatalf("MatchSig mismatch. %x != %x", a1.MatchSig, a2.MatchSig)
	}
	if a1.MatchStamp != a2.MatchStamp {
		t.Fatalf("MatchStamp mismatch. %d != %d", a1.MatchStamp, a2.MatchStamp)
	}
	if !bytes.Equal(a1.InitSig, a2.InitSig) {
		t.Fatalf("InitSig mismatch. %x != %x", a1.InitSig, a2.InitSig)
	}
	if a1.InitStamp != a2.InitStamp {
		t.Fatalf("InitStamp mismatch. %d != %d", a1.InitStamp, a2.InitStamp)
	}
	if !bytes.Equal(a1.AuditSig, a2.AuditSig) {
		t.Fatalf("AuditSig mismatch. %x != %x", a1.AuditSig, a2.AuditSig)
	}
	if a1.AuditStamp != a2.AuditStamp {
		t.Fatalf("AuditStamp mismatch. %d != %d", a1.AuditStamp, a2.AuditStamp)
	}
	if !bytes.Equal(a1.RedeemSig, a2.RedeemSig) {
		t.Fatalf("RedeemSig mismatch. %x != %x", a1.RedeemSig, a2.RedeemSig)
	}
	if a1.RedeemStamp != a2.RedeemStamp {
		t.Fatalf("RedeemStamp mismatch. %d != %d", a1.RedeemStamp, a2.RedeemStamp)
	}
	if !bytes.Equal(a1.RedemptionSig, a2.RedemptionSig) {
		t.Fatalf("RedemptionSig mismatch. %x != %x", a1.RedemptionSig, a2.RedemptionSig)
	}
	if a1.RedemptionStamp != a2.RedemptionStamp {
		t.Fatalf("RedemptionStamp mismatch. %d != %d", a1.RedemptionStamp, a2.RedemptionStamp)
	}
}

// MustCompareMatchProof ensures the two MatchProof are identical, calling the
// Fatalf method of the testKiller if not.
func MustCompareMatchProof(t testKiller, m1, m2 *db.MatchProof) {
	if !bytes.Equal(m1.CounterScript, m2.CounterScript) {
		t.Fatalf("CounterScript mismatch. %x != %x", m1.CounterScript, m2.CounterScript)
	}
	if !bytes.Equal(m1.SecretHash, m2.SecretHash) {
		t.Fatalf("SecretHash mismatch. %x != %x", m1.SecretHash, m2.SecretHash)
	}
	if !bytes.Equal(m1.Secret, m2.Secret) {
		t.Fatalf("SecretKey mismatch. %x != %x", m1.Secret, m2.Secret)
	}
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
	MustCompareMatchAuth(t, &m1.Auth, &m2.Auth)
}

// MustCompareAccountInfo ensures the two AccountInfo are identical, calling the
// Fatalf method of the testKiller if not.
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

// MustCompareOrderProof ensures the two OrderProof are identical, calling the
// Fatalf method of the testKiller if not.
func MustCompareOrderProof(t testKiller, p1, p2 *db.OrderProof) {
	if !bytes.Equal(p1.DEXSig, p2.DEXSig) {
		t.Fatalf("DEXSig mismatch. %x != %x", p1.DEXSig, p2.DEXSig)
	}
	if !bytes.Equal(p1.Preimage, p2.Preimage) {
		t.Fatalf("Preimage mismatch. %x != %x", p1.Preimage, p2.Preimage)
	}
}

// MustCompareWallets ensures the two Wallet are identical, calling the Fatalf
// method of the testKiller if not.
func MustCompareWallets(t testKiller, w1, w2 *db.Wallet) {
	if w1.AssetID != w2.AssetID {
		t.Fatalf("AssetID mismatch. %d != %d", w1.AssetID, w2.AssetID)
	}
	if w1.Account != w2.Account {
		t.Fatalf("Account mismatch. %s != %s", w1.Account, w2.Account)
	}
	if len(w1.Settings) != len(w2.Settings) {
		t.Fatalf("Settings mismatch. %d != %s", len(w1.Settings), len(w2.Settings))
	}
	w2Settings := make(map[string]string, len(w2.Settings))
	for k, v := range w2.Settings {
		w2Settings[strings.ToLower(k)] = v
	}
	for k, v1 := range w1.Settings {
		if v2, ok := w2Settings[k]; !ok {
			t.Fatalf("Settings mismatch: key '%s' not found in one wallet", k)
		} else if v1 != v2 {
			t.Fatalf("Settings mismatch: different values for key '%s'", k)
		}
	}
	MustCompareBalances(t, w1.Balance, w2.Balance)
	if !w1.BalUpdate.Equal(w2.BalUpdate) {
		t.Fatalf("BalUpdate mismatch. %s != %s", w1.BalUpdate, w2.BalUpdate)
	}
	if w1.Address != w2.Address {
		t.Fatalf("Address mismatch. %s != %s", w1.Address, w2.Address)
	}
	if !bytes.Equal(w1.EncryptedPW, w2.EncryptedPW) {
		t.Fatalf("EncryptedPW mismatch. %x != %x", w1.EncryptedPW, w2.EncryptedPW)
	}
}

func MustCompareBalances(t testKiller, b1, b2 *asset.Balance) {
	if b1.Available != b2.Available {
		t.Fatalf("available balance mismatch. %d != %d", b1.Available, b2.Available)
	}
	if b1.Immature != b2.Immature {
		t.Fatalf("immature balance mismatch. %d != %d", b1.Immature, b2.Immature)
	}
	if b1.Locked != b2.Locked {
		t.Fatalf("locked balance mismatch. %d != %d", b1.Locked, b2.Locked)
	}
}

func MustCompareNotifications(t testKiller, n1, n2 *db.Notification) {
	if n1.NoteType != n2.NoteType {
		t.Fatalf("NoteType mismatch. %s != %s", n1.NoteType, n2.NoteType)
	}
	if n1.SubjectText != n2.SubjectText {
		t.Fatalf("SubjectText mismatch. %s != %s", n1.SubjectText, n2.SubjectText)
	}
	if n1.DetailText != n2.DetailText {
		t.Fatalf("DetailText mismatch. %s != %s", n1.DetailText, n2.DetailText)
	}
	if n1.Severeness != n2.Severeness {
		t.Fatalf("Severeness mismatch. %d != %d", n1.Severeness, n2.Severeness)
	}
	if n1.TimeStamp != n2.TimeStamp {
		t.Fatalf("TimeStamp mismatch. %d != %d", n1.TimeStamp, n2.TimeStamp)
	}
	if n1.ID().String() != n2.ID().String() {
		t.Fatalf("ID mismatch. %s != %s", n1.ID(), n2.ID())
	}
}
