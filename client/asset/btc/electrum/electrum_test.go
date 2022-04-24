// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build electrumlive

package electrum

import (
	"bytes"
	"fmt"
	"testing"

	dexltc "decred.org/dcrdex/dex/networks/ltc"

	"github.com/btcsuite/btcd/wire"
	"github.com/btcsuite/btcutil"
	"github.com/davecgh/go-spew/spew"
)

func TestGetStuff(t *testing.T) {
	// .electrum-ltc/testnet/config: rpcuser, rpcpass, rpcport
	const walletPass = "walletPass" // set me
	ec := NewClient("user", "pass", "http://127.0.0.1:5678")

	res, err := ec.GetInfo()
	if err != nil {
		t.Fatal(err)
	}
	spew.Dump(res)

	feeRate, err := ec.FeeRate(1)
	if err != nil {
		t.Fatal(err)
	}
	t.Log(feeRate)

	addrHist, err := ec.GetAddressHistory("Qe6bKuE2ZKxg6sCvLWb47kfgBdpjbbymtT")
	if err != nil {
		t.Fatal(err)
	}
	spew.Dump(addrHist)

	addrUnspent, err := ec.GetAddressUnspent("Qe6bKuE2ZKxg6sCvLWb47kfgBdpjbbymtT")
	if err != nil {
		t.Fatal(err)
	}
	spew.Dump(addrUnspent)

	tx, err := ec.GetRawTransaction("c100f9df92c90a7412e4c55ec632a2d9eb3051ac8a920d293f6e277d3b3819ca")
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("%x\n", tx)
	msgTx := &wire.MsgTx{}
	err = msgTx.Deserialize(bytes.NewReader(tx))
	if err != nil {
		t.Fatal(err)
	}
	spew.Dump(msgTx)

	unspent, err := ec.ListUnspent()
	if err != nil {
		t.Fatal(err)
	}
	spew.Dump(unspent)

	myAddr := "tltc1q74fcwvn5ydl0ssf7pvdscx2gu6aaxzavf2u7lr"
	valid, mine, err := ec.CheckAddress(myAddr)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("valid: %v, mine: %v", valid, mine)

	notMine := "tltc1qtmjdzc3xmju803e529jv9l2860w0h47c95h37f"
	valid, mine, err = ec.CheckAddress(notMine)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("valid: %v, mine: %v", valid, mine)

	invalid := "tltc1qtmjdzc3xmju803e529jv9l2860w0h47c95xxxx"
	valid, mine, err = ec.CheckAddress(invalid)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("valid: %v, mine: %v", valid, mine)

	wifStr, err := ec.GetPrivateKeys(walletPass, myAddr)
	if err != nil {
		t.Fatal(err)
	}
	wif, err := btcutil.DecodeWIF(wifStr)
	if err != nil {
		t.Fatal(err)
	}
	pkh := btcutil.Hash160(wif.SerializePubKey())
	p2wpkh, err := btcutil.NewAddressWitnessPubKeyHash(pkh, dexltc.TestNet4Params)
	if err != nil {
		t.Fatal(err)
	}
	if addr := p2wpkh.EncodeAddress(); addr != myAddr {
		t.Fatalf("%v != %v", addr, myAddr)
	}
	// fmt.Println(wif.PrivKey.Serialize())

	txRaw, err := ec.PayTo(walletPass, "tltc1qmk8uqqn37a2r5lu9jc86jnat6j0y28ydmfuadd", 1.21, 17.3454)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%x", txRaw)

	txRawSigned, err := ec.SignTx(walletPass, txRaw)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%x", txRawSigned)

	// txid, err := ec.Broadcast(txRaw)
	// if err != nil {
	// 	t.Fatal(err)
	// }
	// t.Log(txid)

	confs, err := ec.GetWalletTxConfs("f4b5fca9e2fa3abfe22ea82eece8e28cdcf3e03bb11c63c327b5c719bfa2df6f")
	if err != nil {
		t.Fatal(err)
	}
	t.Log("Confs:", confs)

	_, err = ec.GetWalletTxConfs("ffca8ec6bcd44937d92c25d1c3e85cc2895419ccb66a46c986f3817cd60fa693")
	if err == nil {
		t.Fatal("worked for non-wallet txn???")
	}

	bal, err := ec.GetBalance()
	if err != nil {
		t.Fatal(err)
	}
	t.Log(bal.Confirmed, bal.Unconfirmed, bal.Immature)

	ok, err := ec.FreezeAddress(myAddr)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("failed to freeze")
	}
	ok, err = ec.UnfreezeAddress(myAddr)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("failed to unfreeze")
	}

	addr, err := ec.GetUnusedAddress()
	if err != nil {
		t.Fatal(err)
	}
	t.Log(addr)
}
