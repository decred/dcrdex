// +build !harness

package core

import (
	"encoding/hex"

	"testing"

	"decred.org/dcrdex/client/db"
)

func TestAccountExport(t *testing.T) {
	rig := newTestRig()
	tCore := rig.core
	host := tCore.conns[tDexHost].acct.host
	tCore.conns[tDexHost].acct.isPaid = true

	setupRigAccountProof(host, rig)

	accountResponse, err := tCore.AccountExport(tPW, host)
	if err != nil {
		t.Fatalf("account keys error: %v", err)
	}
	if accountResponse == nil {
		t.Fatalf("accountResponse is nil")
	}
	if host != accountResponse.Host {
		t.Fatalf("host key not equal to account host")
	}
	if accountResponse.AccountID != rig.acct.id.String() {
		t.Fatal("unexpected AccountID")
	}
	if accountResponse.DEXPubKey != hex.EncodeToString(rig.acct.dexPubKey.SerializeCompressed()) {
		t.Fatal("unexpected DEXPubKey")
	}
	if accountResponse.PrivKey != hex.EncodeToString(rig.acct.privKey.Serialize()) {
		t.Fatal("unexpected PrivKey")
	}
	if accountResponse.FeeCoin != hex.EncodeToString(rig.acct.feeCoin) {
		t.Fatal("unexpected FeeCoin")
	}
	if accountResponse.Cert != hex.EncodeToString(rig.acct.cert) {
		t.Fatal("unexpected Cert")
	}
	if accountResponse.FeeProofSig != hex.EncodeToString(rig.db.accountProof.Sig) {
		t.Fatal("unexpected FeeProofSig")
	}
	if accountResponse.FeeProofStamp != rig.db.accountProof.Stamp {
		t.Fatal("unexpected FeeProofStamp")
	}
}

// If account is not paid then AccountProof should contain unset values
func TestAccountExportNoAccountProof(t *testing.T) {
	rig := newTestRig()
	tCore := rig.core
	host := tCore.conns[tDexHost].acct.host
	tCore.conns[tDexHost].acct.isPaid = false

	setupRigAccountProof(host, rig)

	accountResponse, err := tCore.AccountExport(tPW, host)
	if err != nil {
		t.Fatalf("account keys error: %v", err)
	}
	if accountResponse == nil {
		t.Fatalf("accountResponse is nil")
	}

	if accountResponse.FeeProofSig != "" {
		t.Fatal("unexpected FeeProofSig")
	}
	if accountResponse.FeeProofStamp != 0 {
		t.Fatal("unexpected FeeProofStamp")
	}
}

var tFeeProofStamp uint64 = 123456789
var tFeeProofSig = []byte("some signature here")

func setupRigAccountProof(host string, rig *testRig) {
	accountProof := &db.AccountProof{
		Host:  host,
		Stamp: tFeeProofStamp,
		Sig:   tFeeProofSig,
	}
	rig.db.accountProof = accountProof
}

func TestAccountExportPasswordError(t *testing.T) {
	rig := newTestRig()
	tCore := rig.core
	host := tCore.conns[tDexHost].acct.host
	rig.crypter.recryptErr = tErr
	_, err := tCore.AccountExport(tPW, host)
	if !errorHasCode(err, passwordErr) {
		t.Fatalf("expected password error, actual error: '%v'", err)
	}
}

func TestAccountExportAddressError(t *testing.T) {
	rig := newTestRig()
	tCore := rig.core
	host := ":bad:"
	_, err := tCore.AccountExport(tPW, host)
	if !errorHasCode(err, addressParseErr) {
		t.Fatalf("expected address parse error, actual error: '%v'", err)
	}
}

func TestAccountExportUnknownDEX(t *testing.T) {
	rig := newTestRig()
	tCore := rig.core
	host := tCore.conns[tDexHost].acct.host
	// Lose the dexConnection
	tCore.connMtx.Lock()
	delete(tCore.conns, tDexHost)
	tCore.connMtx.Unlock()
	_, err := tCore.AccountExport(tPW, host)
	if !errorHasCode(err, unknownDEXErr) {
		t.Fatalf("expected unknown DEX error, actual error: '%v'", err)
	}
}

func TestAccountExportAccountProofError(t *testing.T) {
	rig := newTestRig()
	tCore := rig.core
	host := tCore.conns[tDexHost].acct.host
	tCore.conns[tDexHost].acct.isPaid = true
	rig.db.accountProofErr = tErr
	_, err := tCore.AccountExport(tPW, host)
	if !errorHasCode(err, accountProofErr) {
		t.Fatalf("expected account proof error, actual error: '%v'", err)
	}
}

func buildTestAccount(host string) Account {
	return Account{
		Host:          host,
		AccountID:     tDexAccountID.String(),
		DEXPubKey:     hex.EncodeToString(tDexKey.SerializeCompressed()),
		PrivKey:       hex.EncodeToString(tDexPriv.Serialize()),
		Cert:          hex.EncodeToString([]byte{}),
		FeeCoin:       hex.EncodeToString([]byte("somecoin")),
		FeeProofSig:   hex.EncodeToString(tFeeProofSig),
		FeeProofStamp: tFeeProofStamp,
	}
}

func TestAccountImport(t *testing.T) {
	rig := newTestRig()
	tCore := rig.core
	host := tCore.conns[tDexHost].acct.host
	account := buildTestAccount(host)
	rig.queueConfig()
	err := tCore.AccountImport(tPW, account)
	if err != nil {
		t.Fatalf("account import error: %v", err)
	}
	if !rig.db.verifyAccountPaid {
		t.Fatalf("expected execution of db.AccountPaid")
	}
	if !rig.db.verifyCreateAccount {
		t.Fatalf("expected execution of db.CreateAccount")
	}
}

func TestAccountImportEmptyFeeProofSig(t *testing.T) {
	rig := newTestRig()
	tCore := rig.core
	host := tCore.conns[tDexHost].acct.host
	account := buildTestAccount(host)
	account.FeeProofSig = ""
	rig.queueConfig()
	err := tCore.AccountImport(tPW, account)
	if err != nil {
		t.Fatalf("account import error: %v", err)
	}
	if rig.db.verifyAccountPaid {
		t.Fatalf("not expecting execution of db.AccountPaid")
	}
	if !rig.db.verifyCreateAccount {
		t.Fatalf("expected execution of db.CreateAccount")
	}
}

func TestAccountImportEmptyFeeProofStamp(t *testing.T) {
	rig := newTestRig()
	tCore := rig.core
	host := tCore.conns[tDexHost].acct.host
	account := buildTestAccount(host)
	account.FeeProofStamp = 0
	rig.queueConfig()
	err := tCore.AccountImport(tPW, account)
	if err != nil {
		t.Fatalf("account import error: %v", err)
	}
	if rig.db.verifyAccountPaid {
		t.Fatalf("not expecting execution of db.AccountPaid")
	}
	if !rig.db.verifyCreateAccount {
		t.Fatalf("expected execution of db.CreateAccount")
	}
}

func TestAccountImportPasswordError(t *testing.T) {
	rig := newTestRig()
	tCore := rig.core
	host := tCore.conns[tDexHost].acct.host
	account := buildTestAccount(host)
	rig.queueConfig()
	rig.crypter.recryptErr = tErr
	err := tCore.AccountImport(tPW, account)
	if !errorHasCode(err, passwordErr) {
		t.Fatalf("expected password error, actual error: '%v'", err)
	}
}

func TestAccountImportAddressError(t *testing.T) {
	rig := newTestRig()
	tCore := rig.core
	host := ":bad:"
	account := buildTestAccount(host)
	rig.queueConfig()
	err := tCore.AccountImport(tPW, account)
	if !errorHasCode(err, addressParseErr) {
		t.Fatalf("expected address parse error, actual error: '%v'", err)
	}
}

func TestAccountImportDecodePubKeyError(t *testing.T) {
	rig := newTestRig()
	tCore := rig.core
	host := tCore.conns[tDexHost].acct.host
	account := buildTestAccount(host)
	account.DEXPubKey = "bad"
	rig.queueConfig()
	err := tCore.AccountImport(tPW, account)
	if !errorHasCode(err, decodeErr) {
		t.Fatalf("expected decode error, actual error: '%v'", err)
	}
}

func TestAccountImportParsePubKeyError(t *testing.T) {
	rig := newTestRig()
	tCore := rig.core
	host := tCore.conns[tDexHost].acct.host
	account := buildTestAccount(host)
	account.DEXPubKey = hex.EncodeToString([]byte("bad"))
	rig.queueConfig()
	err := tCore.AccountImport(tPW, account)
	if !errorHasCode(err, parseKeyErr) {
		t.Fatalf("expected parse key error, actual error: '%v'", err)
	}
}

func TestAccountImportDecodeCertError(t *testing.T) {
	rig := newTestRig()
	tCore := rig.core
	host := tCore.conns[tDexHost].acct.host
	account := buildTestAccount(host)
	account.Cert = "bad"
	rig.queueConfig()
	err := tCore.AccountImport(tPW, account)
	if !errorHasCode(err, decodeErr) {
		t.Fatalf("expected decode error, actual error: '%v'", err)
	}
}

func TestAccountImportDecodeFeeCoinError(t *testing.T) {
	rig := newTestRig()
	tCore := rig.core
	host := tCore.conns[tDexHost].acct.host
	account := buildTestAccount(host)
	account.FeeCoin = "bad"
	rig.queueConfig()
	err := tCore.AccountImport(tPW, account)
	if !errorHasCode(err, decodeErr) {
		t.Fatalf("expected decode error, actual error: '%v'", err)
	}
}

func TestAccountImportDecodePrivKeyError(t *testing.T) {
	rig := newTestRig()
	tCore := rig.core
	host := tCore.conns[tDexHost].acct.host
	account := buildTestAccount(host)
	account.PrivKey = "bad"
	rig.queueConfig()
	err := tCore.AccountImport(tPW, account)
	if !errorHasCode(err, decodeErr) {
		t.Fatalf("expected decode error, actual error: '%v'", err)
	}
}

func TestAccountImportEncryptPrivKeyError(t *testing.T) {
	rig := newTestRig()
	tCore := rig.core
	host := tCore.conns[tDexHost].acct.host
	account := buildTestAccount(host)
	rig.crypter.encryptErr = tErr
	rig.queueConfig()
	err := tCore.AccountImport(tPW, account)
	if !errorHasCode(err, encryptionErr) {
		t.Fatalf("expected encryption error, actual error: '%v'", err)
	}
}

func TestAccountImportDecodeFeeProofSigError(t *testing.T) {
	rig := newTestRig()
	tCore := rig.core
	host := tCore.conns[tDexHost].acct.host
	account := buildTestAccount(host)
	account.FeeProofSig = "bad"
	rig.queueConfig()
	err := tCore.AccountImport(tPW, account)
	if !errorHasCode(err, decodeErr) {
		t.Fatalf("expected decode error, actual error: '%v'", err)
	}
}

func TestAccountImportAccountPaidError(t *testing.T) {
	rig := newTestRig()
	tCore := rig.core
	host := tCore.conns[tDexHost].acct.host
	account := buildTestAccount(host)
	rig.queueConfig()
	rig.db.accountPaidErr = tErr
	err := tCore.AccountImport(tPW, account)
	if !errorHasCode(err, dbErr) {
		t.Fatalf("expected db error, actual error: '%v'", err)
	}
}

func TestAccountImportAccountCreateAccountError(t *testing.T) {
	rig := newTestRig()
	tCore := rig.core
	host := tCore.conns[tDexHost].acct.host
	account := buildTestAccount(host)
	rig.queueConfig()
	rig.db.createAccountErr = tErr
	err := tCore.AccountImport(tPW, account)
	if !errorHasCode(err, dbErr) {
		t.Fatalf("expected db error, actual error: '%v'", err)
	}
}
