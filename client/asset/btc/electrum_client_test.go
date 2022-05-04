// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build electrumlive

package btc

import (
	"bytes"
	"context"
	"encoding/hex"
	"sync"
	"testing"
	"time"

	"decred.org/dcrdex/client/asset/btc/electrum"
	"decred.org/dcrdex/dex"

	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/txscript"
)

func Test_electrumWallet(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	// This test uses a *testnet* BTC wallet listening on localhost port 6789.
	// Testnet is used because it tests interaction with actual remote servers
	// (and the bitcoin network) and there are historical transactions that can
	// be inspected in public block explorers.
	const walletPass = "walletpass" // set me
	ewc := electrum.NewWalletClient("user", "pass", "http://127.0.0.1:6789")
	ew := newElectrumWallet(ewc, &electrumWalletConfig{
		params: &chaincfg.TestNet3Params,
		log:    dex.StdOutLogger("ELECTRUM-TEST", dex.LevelTrace),
		segwit: true,
	})
	var wg sync.WaitGroup
	err := ew.connect(ctx, &wg)
	if err != nil {
		t.Fatal(err)
	}

	medianTime, err := ew.calcMedianTime(ctx, 2286089) // ltc testnet
	if err != nil {
		t.Fatal(err)
	}
	t.Log(medianTime.UTC()) // 2022-07-15 13:09:39 +0000 UTC

	// Output:
	// https://tbtc.bitaps.com/b9890847952be7ca8918368cbfa88fea0efb71ffe9549de0c4ae6e23c2b02e4c/output/0
	// Spending input:
	// https://tbtc.bitaps.com/9dbed377dbbee2ac98dcf5edfc504e04f16afa73b07afc81bc0711b7625f66d1/input/0

	// findOutputSpender
	fundHash, _ := chainhash.NewHashFromStr("b9890847952be7ca8918368cbfa88fea0efb71ffe9549de0c4ae6e23c2b02e4c")
	fundVout := uint32(0)
	fundVal := int64(1922946974)
	fundPkScript, _ := hex.DecodeString("00145bb911c50e0f79e0212aae1e0503ee8192038b57")
	fundAddr := "tb1qtwu3r3gwpau7qgf24c0q2qlwsxfq8z6hl6efm4"
	spendMsgTx, spendVin, err := ew.findOutputSpender(ctx, fundHash, fundVout)
	if err != nil {
		t.Fatal(err)
	}
	spendHash := spendMsgTx.TxHash()
	wantSpendTxID := "9dbed377dbbee2ac98dcf5edfc504e04f16afa73b07afc81bc0711b7625f66d1"
	if spendHash.String() != wantSpendTxID {
		t.Errorf("Incorrect spending tx hash %v, want %v", spendHash, wantSpendTxID)
	}
	wantSpendVin := uint32(0)
	if spendVin != wantSpendVin {
		t.Errorf("Incorrect spending tx input index %d, want %d", spendVin, wantSpendVin)
	}

	// gettxout - first the spent output
	fundTxOut, fundConfs, err := ew.getTxOut(fundHash, fundVout, nil, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if fundTxOut != nil {
		t.Errorf("expected a nil TxOut for a spent output, but got one")
	}

	// gettxout - next an old unspent output that's likely to stay unspent
	fundHash, _ = chainhash.NewHashFromStr("34fd12be115549ec482fdb20bc5196e0107cfcda32c252ffea9e8116ee6fcf8e")
	fundVout = 0
	fundVal = 20000
	fundPkScript, _ = hex.DecodeString("76a914b3e0f80ce29ac48793de504ae9aa0b6579dda29a88ac")
	fundAddr = "mwv4kWRXkc2w42823FU9SJ16cvZH8Aobke"
	fundTxOut, fundConfs, err = ew.getTxOut(fundHash, fundVout, nil, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%v confs = %d", fundHash, fundConfs)
	if fundTxOut.Value != fundVal {
		t.Errorf("wrong output value %d, wanted %d", fundTxOut.Value, fundVal)
	}
	if !bytes.Equal(fundTxOut.PkScript, fundPkScript) {
		t.Errorf("wanted pkScript %x, got %x", fundPkScript, fundTxOut.PkScript)
	}
	scriptClass, addrs, reqSigs, err := txscript.ExtractPkScriptAddrs(fundTxOut.PkScript, ew.chainParams)
	if err != nil {
		t.Fatal(err)
	}
	if scriptClass != txscript.PubKeyHashTy {
		t.Errorf("got script class %v, wanted %v", scriptClass, txscript.PubKeyHashTy)
	}
	if len(addrs) != 1 {
		t.Fatalf("got %d addresses, wanted 1", len(addrs))
	}
	if addrs[0].String() != fundAddr {
		t.Errorf("got address %v, wanted %v", addrs[0], fundAddr)
	}
	if reqSigs != 1 {
		t.Fatalf("requires %d sigs, expected 1", reqSigs)
	}

	addr, err := ew.wallet.GetUnusedAddress(ctx) // or ew.externalAddress(), but let's not blow up the gap limit testing
	if err != nil {
		t.Fatal(err)
	}
	t.Log(addr)

	err = ew.walletUnlock([]byte(walletPass))
	if err != nil {
		t.Fatal(err)
	}

	_, err = ew.privKeyForAddress(addr)
	if err != nil {
		t.Fatal(err)
	}

	// Try the following with your own addresses!
	// sent1, err := ew.sendToAddress("tltc1qn7dsn7glncdgf078pauerlqum6ma8peyvfa7gz", toSatoshi(0.2345), 4, false)
	// if err != nil {
	// 	t.Fatal(err)
	// }
	// t.Log(sent1)
	// sent2, err := ew.sendToAddress("tltc1qpth226vjw2vp3mk8fvjfnrc4ygy8vtnx8p2q78", toSatoshi(0.2345), 5, true)
	// if err != nil {
	// 	t.Fatal(err)
	// }
	// t.Log(sent2)

	// addr, err := ew.externalAddress()
	// if err != nil {
	// 	t.Fatal(err)
	// }
	// sweepTx, err := ew.sweep(addr.EncodeAddress(), 6)
	// if err != nil {
	// 	t.Fatal(err)
	// }
	// t.Log(sweepTx)

	// Hang out until the context times out. Maybe see a new block. Think about
	// other tests...
	wg.Wait()
}
