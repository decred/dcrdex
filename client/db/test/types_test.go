package dbtest

import (
	"bytes"
	"os"
	"testing"
	"time"

	"decred.org/dcrdex/client/db"
)

func TestAccountInfo(t *testing.T) {
	spins := 10000
	if testing.Short() {
		spins = 1000
	}
	tStart := time.Now()
	nTimes(spins, func(i int) {
		ai := RandomAccountInfo()
		aiB := ai.Encode()
		reAI, err := db.DecodeAccountInfo(aiB)
		if err != nil {
			t.Fatalf("error decoding AccountInfo: %v", err)
		}
		MustCompareAccountInfo(t, ai, reAI)
	})
	t.Logf("encoded, decoded, and compared %d AccountInfo in %d ms", spins, time.Since(tStart)/time.Millisecond)
}

func TestMatchProof(t *testing.T) {
	spins := 10000
	if testing.Short() {
		spins = 1000
	}
	proofs := make([]*db.MatchProof, 0, spins)
	// Generate proofs with an average of 20% sparsity. Empty fields should not
	// affect accurate encoding/decoding.
	nTimes(spins, func(int) { proofs = append(proofs, RandomMatchProof(0.4)) })
	tStart := time.Now()
	nTimes(spins, func(i int) {
		proof := proofs[i]
		proofB := proof.Encode()
		reProof, err := db.DecodeMatchProof(proofB)
		if err != nil {
			t.Fatalf("match decode error: %v", err)
		}
		MustCompareMatchProof(t, proof, reProof)
	})
	t.Logf("encoded, decoded, and compared %d MatchProof in %d ms", spins, time.Since(tStart)/time.Millisecond)
}

func TestOrderProof(t *testing.T) {
	spins := 10000
	if testing.Short() {
		spins = 1000
	}
	proofs := make([]*db.OrderProof, 0, spins)
	nTimes(spins, func(int) {
		proofs = append(proofs, &db.OrderProof{
			DEXSig:   randBytes(73),
			Preimage: randBytes(32),
		})
	})
	tStart := time.Now()
	nTimes(spins, func(i int) {
		proof := proofs[i]
		proofB := proof.Encode()
		reProof, err := db.DecodeOrderProof(proofB)
		if err != nil {
			t.Fatalf("decode error: %v", err)
		}
		MustCompareOrderProof(t, proof, reProof)
	})
	t.Logf("encoded, decoded, and compared %d OrderProof in %d ms", spins, time.Since(tStart)/time.Millisecond)
}

func nTimes(n int, f func(int)) {
	for i := 0; i < n; i++ {
		f(i)
	}
}

func TestAccountsBackupAndRestore(t *testing.T) {
	keyParams := randBytes(128)
	count := 5
	accts := make(map[string]*db.AccountInfo, count)
	for i := 0; i < count; i++ {
		acct := RandomAccountInfo()
		accts[acct.Host] = acct
	}

	bkp := db.AccountBackup{
		KeyParams: keyParams,
	}
	for _, acct := range accts {
		bkp.Accounts = append(bkp.Accounts, acct)
	}

	// Save the account backup to file.
	backupPath := "acct.backup"
	err := bkp.Save(backupPath)
	if err != nil {
		t.Fatalf("[Save] unexpected error: %v", err)
	}

	defer os.Remove(backupPath)

	// Restore accounts from backup.
	restored, err := db.RestoreAccountBackup(backupPath)
	if err != nil {
		t.Fatalf("[RestoreAccountBackup] unexpected error: %v", err)
	}

	// Ensure restored account details are identical to the original.
	if !bytes.Equal(restored.KeyParams, bkp.KeyParams) {
		t.Fatalf("expected key params value of %x, got %x",
			bkp.KeyParams, restored.KeyParams)
	}

	if len(restored.Accounts) != len(bkp.Accounts) {
		t.Fatalf("expected %d restored accounts, got %x",
			count, len(restored.Accounts))
	}

	for _, dexAcct := range restored.Accounts {
		acct, ok := accts[dexAcct.Host]
		if !ok {
			t.Fatalf("no account found with url %s", dexAcct.Host)
		}

		if !bytes.Equal(dexAcct.EncKey, acct.EncKey) {
			t.Fatalf("expected restored account %s with encryption key %x, "+
				"got %x", dexAcct.Host, dexAcct.EncKey, acct.EncKey)
		}

		if !dexAcct.DEXPubKey.IsEqual(acct.DEXPubKey) {
			t.Fatalf("expected restored account %s with dex public key %x, "+
				"got %x", dexAcct.Host, dexAcct.DEXPubKey.SerializeCompressed(),
				acct.DEXPubKey.SerializeCompressed())
		}
	}
}
