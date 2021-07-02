//go:build !harness

package core

import (
	"bytes"
	"encoding/hex"
	"errors"
	"testing"

	"decred.org/dcrdex/client/db"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/server/account"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

/* TODO: rework TestAccountExport
func TestAccountExport(t *testing.T) {
	rig := newTestRig()
	tCore := rig.core
	host := tCore.conns[tDexHost].acct.host
	tCore.conns[tDexHost].acct.isPaid = true

	setupRigAccountProof(host, rig)

	accountResponse, _ , err := tCore.AccountExport(tPW, host)
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
*/

// If account is not paid then AccountProof should contain unset values
func TestAccountExportNoAccountProof(t *testing.T) {
	rig := newTestRig()
	defer rig.shutdown()
	tCore := rig.core
	host := tCore.conns[tDexHost].acct.host
	tCore.conns[tDexHost].acct.isPaid = false

	setupRigAccountProof(host, rig)

	accountResponse, _ /*bonds*/, err := tCore.AccountExport(tPW, host)
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
		{}: {metaData: &db.OrderMetaData{Status: order.OrderStatusBooked}},
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
		errCode:     unknownDEXErr,
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
		rig.crypter.(*tCrypter).recryptErr = test.recryptErr
		rig.db.disabledHost = nil
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
		if rig.db.disabledHost == nil {
			t.Fatal("expected execution of db.DisableAccount")
		}
		if *rig.db.disabledHost != test.host {
			t.Fatalf("expected db disabled account to match test host, want: %v"+
				" got: %v", test.host, *rig.db.disabledHost)
		}
	}
}

func TestUpdateCert(t *testing.T) {
	rig := newTestRig()
	tCore := rig.core
	rig.db.acct.LegacyFeePaid = true
	rig.db.acct.LegacyFeeCoin = encode.RandomBytes(32)

	tests := []struct {
		name                 string
		host                 string
		acctErr              bool
		updateAccountInfoErr bool
		queueConfig          bool
		expectError          bool
	}{
		{
			name:        "ok",
			host:        rig.db.acct.Host,
			queueConfig: true,
		},
		{
			name:        "connect error",
			host:        rig.db.acct.Host,
			queueConfig: false,
			expectError: true,
		},
		{
			name:        "db get account error",
			host:        rig.db.acct.Host,
			queueConfig: true,
			acctErr:     true,
			expectError: true,
		},
		{
			name:                 "db update account err",
			host:                 rig.db.acct.Host,
			queueConfig:          true,
			updateAccountInfoErr: true,
			expectError:          true,
		},
	}

	for _, test := range tests {
		rig.db.verifyUpdateAccountInfo = false
		if test.updateAccountInfoErr {
			rig.db.updateAccountInfoErr = errors.New("")
		} else {
			rig.db.updateAccountInfoErr = nil
		}
		if test.acctErr {
			rig.db.acctErr = errors.New("")
		} else {
			rig.db.acctErr = nil
		}
		randomCert := encode.RandomBytes(32)
		if test.queueConfig {
			rig.queueConfig()
		}
		err := tCore.UpdateCert(test.host, randomCert)
		if test.expectError {
			if err == nil {
				t.Fatalf("%s: expected error but did not get", test.name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", test.name, err)
		}
		if !rig.db.verifyUpdateAccountInfo {
			t.Fatalf("%s: expected update account to be called but it was not", test.name)
		}
		if !bytes.Equal(randomCert, rig.db.acct.Cert) {
			t.Fatalf("%s: expected account to be updated with cert but it was not", test.name)
		}
	}
}

func TestUpdateDEXHost(t *testing.T) {
	newPrivKey, _ := secp256k1.GeneratePrivateKey()
	newPubKey := newPrivKey.PubKey()
	newHost := "newhost.com:123"

	tests := []struct {
		name        string
		oldHost     string
		feePending  bool
		expectError bool
		newPubKey   *secp256k1.PublicKey
	}{
		{
			name:      "ok",
			oldHost:   tDexHost,
			newPubKey: tDexKey,
		},
		{
			name:        "new host has different pub key",
			oldHost:     tDexHost,
			newPubKey:   newPubKey,
			expectError: true,
		},
		{
			name:        "trying to update host that doesn't exist",
			oldHost:     "hostdoesntexist.com:123",
			newPubKey:   tDexKey,
			expectError: true,
		},
		{
			name:        "old dc still fee pending",
			oldHost:     tDexHost,
			newPubKey:   tDexKey,
			feePending:  true,
			expectError: true,
		},
	}

	for _, test := range tests {
		rig := newTestRig()
		tCore := rig.core
		rig.db.acct.LegacyFeePaid = true
		rig.db.acct.LegacyFeeCoin = encode.RandomBytes(32)
		rig.db.acct.Host = tDexHost

		tCore.connMtx.Lock()
		tCore.conns[rig.acct.host] = rig.dc
		tCore.connMtx.Unlock()

		rig.dc.pendingFee = nil
		if test.feePending {
			rig.dc.setPendingFee(42, 1)
		}

		rig.queueConfig()
		rig.queueConnect(nil, []*msgjson.Match{}, []*msgjson.OrderStatus{}, false)
		rig.dc.cfg.DEXPubKey = test.newPubKey.SerializeCompressed()

		_, err := tCore.UpdateDEXHost(test.oldHost, newHost, tPW, []byte{11, 11})
		if test.expectError {
			if err == nil {
				t.Fatalf("%s: expected error but did not get", err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: unepected error: %v", test.name, err)
		}

		if len(tCore.conns) != 1 {
			t.Fatalf("%s: expected conns map to have 1 entry but got %d", test.name, len(tCore.conns))
		}

		if _, ok := tCore.conns[newHost]; !ok {
			t.Fatalf("%s: new host was not added to connections map", test.name)
		}

		if rig.db.disabledHost == nil {
			t.Fatalf("%s: expected execution of db.DisableAccount", test.name)
		}
		if *rig.db.disabledHost != rig.acct.host {
			t.Fatalf("%s: expected db disabled account to match test host, want: %v"+
				" got: %v", test.name, rig.acct.host, rig.db.disabledHost)
		}

		if rig.db.acct.Host != newHost {
			t.Fatalf("%s: expected newly create host %v to match test host %v",
				test.name, rig.db.acct.Host, newHost)
		}
	}
}

func TestAccountExportPasswordError(t *testing.T) {
	rig := newTestRig()
	defer rig.shutdown()
	tCore := rig.core
	host := tCore.conns[tDexHost].acct.host
	rig.crypter.(*tCrypter).recryptErr = tErr
	_, _, err := tCore.AccountExport(tPW, host)
	if !errorHasCode(err, passwordErr) {
		t.Fatalf("expected password error, actual error: '%v'", err)
	}
}

func TestAccountExportAddressError(t *testing.T) {
	rig := newTestRig()
	defer rig.shutdown()
	tCore := rig.core
	host := ":bad:"
	_, _, err := tCore.AccountExport(tPW, host)
	if !errorHasCode(err, addressParseErr) {
		t.Fatalf("expected address parse error, actual error: '%v'", err)
	}
}

func TestAccountExportUnknownDEX(t *testing.T) {
	rig := newTestRig()
	defer rig.shutdown()
	rig.db.acct.Host = "different"
	// Test the db Account look up failing.
	rig.db.acctErr = errors.New("acct retrieve error")
	defer func() { rig.db.acctErr = nil }()
	tCore := rig.core
	_, _, err := tCore.AccountExport(tPW, rig.db.acct.Host) // any valid host is fine
	if !errorHasCode(err, unknownDEXErr) {
		t.Fatalf("expected unknown DEX error, actual error: '%v'", err)
	}
}

func TestAccountExportAccountKeyError(t *testing.T) {
	rig := newTestRig()
	defer rig.shutdown()
	tCore := rig.core
	host := tCore.conns[tDexHost].acct.host
	rig.crypter.(*tCrypter).decryptErr = tErr
	_, _, err := tCore.AccountExport(tPW, host)
	if !errorHasCode(err, passwordErr) {
		t.Fatalf("expected password error, actual error: '%v'", err)
	}
}

func TestAccountExportAccountProofError(t *testing.T) {
	rig := newTestRig()
	defer rig.shutdown()
	tCore := rig.core
	host := tCore.conns[tDexHost].acct.host
	rig.db.acct.LegacyFeePaid = true
	rig.db.accountProofErr = tErr
	_, _, err := tCore.AccountExport(tPW, host)
	if !errorHasCode(err, accountProofErr) {
		t.Fatalf("expected account proof error, actual error: '%v'", err)
	}
}

func buildTestAccount(host string) *Account {
	privKey, _ := secp256k1.GeneratePrivateKey()
	return &Account{
		Host:          host,
		AccountID:     account.NewID(privKey.PubKey().SerializeCompressed()).String(), // can be anything though
		PrivKey:       hex.EncodeToString(privKey.Serialize()),
		DEXPubKey:     hex.EncodeToString(tDexKey.SerializeCompressed()),
		Cert:          hex.EncodeToString([]byte{0x1}),
		FeeCoin:       hex.EncodeToString([]byte("somecoin")),
		FeeProofSig:   hex.EncodeToString(tFeeProofSig),
		FeeProofStamp: tFeeProofStamp,
	}
}

/* TODO: rework AccountImport
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
*/

func TestAccountImportEmptyFeeProofSig(t *testing.T) {
	rig := newTestRig()
	defer rig.shutdown()
	tCore := rig.core
	delete(tCore.conns, tDexHost)
	account := buildTestAccount(tDexHost)
	account.FeeProofSig = ""
	rig.queueConfig()
	err := tCore.AccountImport(tPW, account, nil /* bonds */)
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

// func TestAccountImportEmptyFeeProofStamp(t *testing.T) {
// 	rig := newTestRig()
// 	tCore := rig.core
// 	host := tCore.conns[tDexHost].acct.host
// 	account := buildTestAccount(host)
// 	account.FeeProofStamp = 0
// 	rig.queueConfig()
// 	err := tCore.AccountImport(tPW, account)
// 	if err != nil {
// 		t.Fatalf("account import error: %v", err)
// 	}
// 	if rig.db.verifyAccountPaid {
// 		t.Fatalf("not expecting execution of db.AccountPaid")
// 	}
// 	if !rig.db.verifyCreateAccount {
// 		t.Fatalf("expected execution of db.CreateAccount")
// 	}
// }

func TestAccountImportPasswordError(t *testing.T) {
	rig := newTestRig()
	defer rig.shutdown()
	tCore := rig.core
	delete(tCore.conns, tDexHost)
	account := buildTestAccount(tDexHost)
	rig.queueConfig()
	rig.crypter.(*tCrypter).recryptErr = tErr
	err := tCore.AccountImport(tPW, account, nil)
	if !errorHasCode(err, passwordErr) {
		t.Fatalf("expected password error, actual error: '%v'", err)
	}
}

func TestAccountImportAddressError(t *testing.T) {
	rig := newTestRig()
	defer rig.shutdown()
	tCore := rig.core
	delete(tCore.conns, tDexHost)
	account := buildTestAccount(":bad:")
	rig.queueConfig()
	err := tCore.AccountImport(tPW, account, nil)
	if !errorHasCode(err, addressParseErr) {
		t.Fatalf("expected address parse error, actual error: '%v'", err)
	}
}

func TestAccountImportDecodePubKeyError(t *testing.T) {
	rig := newTestRig()
	defer rig.shutdown()
	tCore := rig.core
	delete(tCore.conns, tDexHost)
	account := buildTestAccount(tDexHost)
	account.DEXPubKey = "bad"
	rig.queueConfig()
	err := tCore.AccountImport(tPW, account, nil)
	if !errorHasCode(err, decodeErr) {
		t.Fatalf("expected decode error, actual error: '%v'", err)
	}
}

func TestAccountImportParsePubKeyError(t *testing.T) {
	rig := newTestRig()
	defer rig.shutdown()
	tCore := rig.core
	delete(tCore.conns, tDexHost)
	account := buildTestAccount(tDexHost)
	account.DEXPubKey = hex.EncodeToString([]byte("bad"))
	rig.queueConfig()
	err := tCore.AccountImport(tPW, account, nil)
	if !errorHasCode(err, parseKeyErr) {
		t.Fatalf("expected parse key error, actual error: '%v'", err)
	}
}

func TestAccountImportDecodeCertError(t *testing.T) {
	rig := newTestRig()
	defer rig.shutdown()
	tCore := rig.core
	delete(tCore.conns, tDexHost)
	account := buildTestAccount(tDexHost)
	account.Cert = "bad"
	rig.queueConfig()
	err := tCore.AccountImport(tPW, account, nil)
	if !errorHasCode(err, decodeErr) {
		t.Fatalf("expected decode error, actual error: '%v'", err)
	}
}

func TestAccountImportDecodeFeeCoinError(t *testing.T) {
	rig := newTestRig()
	defer rig.shutdown()
	tCore := rig.core
	delete(tCore.conns, tDexHost)
	account := buildTestAccount(tDexHost)
	account.FeeCoin = "bad"
	rig.queueConfig()
	err := tCore.AccountImport(tPW, account, nil)
	if !errorHasCode(err, decodeErr) {
		t.Fatalf("expected decode error, actual error: '%v'", err)
	}
}

func TestAccountImportDecodePrivKeyError(t *testing.T) {
	rig := newTestRig()
	defer rig.shutdown()
	tCore := rig.core
	delete(tCore.conns, tDexHost)
	account := buildTestAccount(tDexHost)
	account.PrivKey = "bad"
	rig.queueConfig()
	err := tCore.AccountImport(tPW, account, nil)
	if !errorHasCode(err, decodeErr) {
		t.Fatalf("expected decode error, actual error: '%v'", err)
	}
}

func TestAccountImportEncryptPrivKeyError(t *testing.T) {
	rig := newTestRig()
	defer rig.shutdown()
	tCore := rig.core
	delete(tCore.conns, tDexHost)
	account := buildTestAccount(tDexHost)
	rig.crypter.(*tCrypter).encryptErr = tErr
	rig.queueConfig()
	err := tCore.AccountImport(tPW, account, nil)
	if !errorHasCode(err, encryptionErr) {
		t.Fatalf("expected encryption error, actual error: '%v'", err)
	}
}

func TestAccountImportDecodeFeeProofSigError(t *testing.T) {
	rig := newTestRig()
	defer rig.shutdown()
	tCore := rig.core
	delete(tCore.conns, tDexHost)
	account := buildTestAccount(tDexHost)
	account.FeeProofSig = "bad"
	account.FeeProofStamp = 1232325
	rig.queueConfig()
	err := tCore.AccountImport(tPW, account, nil)
	if !errorHasCode(err, decodeErr) {
		t.Fatalf("expected decode error, actual error: '%v'", err)
	}
}

func TestAccountImportAccountPaidError(t *testing.T) {
	rig := newTestRig()
	defer rig.shutdown()
	tCore := rig.core
	delete(tCore.conns, tDexHost)
	account := buildTestAccount(tDexHost)
	rig.queueConfig()
	rig.db.storeAccountProofErr = tErr
	err := tCore.AccountImport(tPW, account, nil)
	if !errorHasCode(err, dbErr) {
		t.Fatalf("expected db error, actual error: '%v'", err)
	}
}

func TestAccountImportAccountCreateAccountError(t *testing.T) {
	rig := newTestRig()
	defer rig.shutdown()
	tCore := rig.core
	delete(tCore.conns, tDexHost)
	account := buildTestAccount(tDexHost)
	rig.queueConfig()
	rig.db.createAccountErr = tErr
	err := tCore.AccountImport(tPW, account, nil)
	if !errorHasCode(err, dbErr) {
		t.Fatalf("expected db error, actual error: '%v'", err)
	}
}
