// +build !harness

package core

import (
	"encoding/hex"
	"testing"
)

func TestAccountExport(t *testing.T) {
	rig := newTestRig()
	tCore := rig.core
	host := tCore.conns[tDexHost].acct.host
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

func buildTestAccount(host string) Account {
	return Account{
		Host:      host,
		AccountID: tDexAccountID.String(),
		DEXPubKey: hex.EncodeToString(tDexKey.SerializeCompressed()),
		PrivKey:   hex.EncodeToString(tDexPriv.Serialize()),
		Cert:      hex.EncodeToString([]byte{}),
		FeeCoin:   hex.EncodeToString([]byte("somecoin")),
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

func TestAccountImportAccountVerificationError(t *testing.T) {
	rig := newTestRig()
	tCore := rig.core
	host := tCore.conns[tDexHost].acct.host
	account := buildTestAccount(host)
	account.IsPaid = false
	account.FeeCoin = ""
	rig.queueConfig()
	err := tCore.AccountImport(tPW, account)
	if !errorHasCode(err, accountVerificationErr) {
		t.Fatalf("expected account verification error, actual error: '%v'", err)
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
