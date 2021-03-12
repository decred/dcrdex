// +build !harness

package core

import (
	"bytes"
	"encoding/hex"
	"errors"
	"testing"

	"decred.org/dcrdex/client/db"
	"decred.org/dcrdex/dex/order"
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

func TestAccountDisable(t *testing.T) {
	activeTrades := map[order.OrderID]*trackedTrade{
		order.OrderID{}: &trackedTrade{metaData: &db.OrderMetaData{Status: order.OrderStatusBooked}},
	}

	tests := []struct {
		name, host                          string
		recryptErr, acctErr, disableAcctErr error
		wantErr, wantErrCode, loseConns     bool
		activeTrades                        map[order.OrderID]*trackedTrade
		errCode                             int
	}{{
		name: "ok",
		host: tDexHost,
	}, {
		name:       "password error",
		host:       tDexHost,
		recryptErr: tErr,
		wantErr:    true,
		errCode:    passwordErr,
	}, {
		name:        "host error",
		host:        ":bad:",
		wantErr:     true,
		wantErrCode: true,
		errCode:     addressParseErr,
	}, {
		name:        "dex not in conns",
		host:        tDexHost,
		loseConns:   true,
		wantErr:     true,
		wantErrCode: true,
		errCode:     unknownDEXErr,
	}, {
		name:         "has active orders",
		host:         tDexHost,
		activeTrades: activeTrades,
		wantErr:      true,
	}, {
		name:           "disable account error",
		host:           tDexHost,
		disableAcctErr: errors.New(""),
		wantErr:        true,
		wantErrCode:    true,
		errCode:        accountDisableErr,
	}}

	for _, test := range tests {
		rig := newTestRig()
		defer rig.shutdown()
		tCore := rig.core
		rig.crypter.recryptErr = test.recryptErr
		rig.db.disableAccountErr = test.disableAcctErr
		tCore.connMtx.Lock()
		tCore.conns[tDexHost].trades = test.activeTrades
		if test.loseConns {
			// Lose the dexConnection
			delete(tCore.conns, tDexHost)
		}
		tCore.connMtx.Unlock()

		err := tCore.AccountDisable(tPW, test.host)
		if test.wantErr {
			if err == nil {
				t.Fatalf("expected error for test %v", test.name)
			}
			if test.wantErrCode && !errorHasCode(err, test.errCode) {
				t.Fatalf("wanted errCode %v but got %v for test %v", test.errCode, err, test.name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("unexpected error for test %v: %v", test.name, err)
		}
		if _, found := tCore.conns[test.host]; found {
			t.Fatal("found disabled account dex connection")
		}
		if rig.db.disabledAcct == nil {
			t.Fatal("expected execution of db.DisableAccount")
		}
		if rig.db.disabledAcct.Host == test.host {
			t.Fatalf("expected db disabled account to match test host, want: %v"+
				" got: %v", test.host, rig.db.disabledAcct.Host)
		}
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

func TestAccountExportAccountKeyError(t *testing.T) {
	rig := newTestRig()
	tCore := rig.core
	host := tCore.conns[tDexHost].acct.host
	rig.crypter.decryptErr = tErr
	_, err := tCore.AccountExport(tPW, host)
	if !errorHasCode(err, acctKeyErr) {
		t.Fatalf("expected account key error, actual error: '%v'", err)
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
	if rig.db.accountInfoPersisted.Host != host {
		t.Fatalf("unexprected accountInfo Host")
	}
	DEXpubKey, _ := hex.DecodeString(account.DEXPubKey)
	if !bytes.Equal(rig.db.accountInfoPersisted.DEXPubKey.SerializeCompressed(), DEXpubKey) {
		t.Fatal("unexpected DEXPubKey")
	}
	feeCoin, _ := hex.DecodeString(account.FeeCoin)
	if !bytes.Equal(rig.db.accountInfoPersisted.FeeCoin, feeCoin) {
		t.Fatal("unexpected FeeCoin")
	}
	cert, _ := hex.DecodeString(account.Cert)
	if !bytes.Equal(rig.db.accountInfoPersisted.Cert, cert) {
		t.Fatal("unexpected Cert")
	}
	if !rig.db.accountInfoPersisted.Paid {
		t.Fatal("unexpected Paid value")
	}
	if rig.db.accountProofPersisted.Host != host {
		t.Fatal("unexpected accountProof Host")
	}
	feeProofSig, _ := hex.DecodeString(account.FeeProofSig)
	if !bytes.Equal(rig.db.accountProofPersisted.Sig, feeProofSig) {
		t.Fatal("unset FeeProofSig")
	}
	if rig.db.accountProofPersisted.Stamp != account.FeeProofStamp {
		t.Fatal("unexpected FeeProofStamp")
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

func TestAccountImportAccountVerificationError(t *testing.T) {
	rig := newTestRig()
	tCore := rig.core
	host := tCore.conns[tDexHost].acct.host
	account := buildTestAccount(host)
	account.FeeProofSig = ""
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
