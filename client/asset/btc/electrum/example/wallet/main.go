// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"decred.org/dcrdex/client/asset/btc/electrum"
	dexltc "decred.org/dcrdex/dex/networks/ltc"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/wire"
	"github.com/davecgh/go-spew/spew"
)

func main() {
	err := run()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	// .electrum-ltc/testnet/config: rpcuser, rpcpass, rpcport
	const walletPass = "walletpass" // set me
	ec := electrum.NewWalletClient("user", "pass", "http://127.0.0.1:5678")

	commands, err := ec.Commands(ctx)
	if err != nil {
		return err
	}
	fmt.Println(commands)

	res, err := ec.GetInfo(ctx)
	if err != nil {
		return err
	}
	spew.Dump(res)

	feeRate, err := ec.FeeRate(ctx, 1)
	if err != nil {
		return err
	}
	fmt.Println(feeRate)

	addrHist, err := ec.GetAddressHistory(ctx, "Qe6bKuE2ZKxg6sCvLWb47kfgBdpjbbymtT")
	if err != nil {
		return err
	}
	spew.Dump(addrHist)

	addrUnspent, err := ec.GetAddressUnspent(ctx, "Qe6bKuE2ZKxg6sCvLWb47kfgBdpjbbymtT")
	if err != nil {
		return err
	}
	spew.Dump(addrUnspent)

	tx, err := ec.GetRawTransaction(ctx, "b8f5a301aee2d7bede7cfaeac8d6e13d095b3939e011ea2d259f7ccda3083ae7")
	if err != nil {
		return err
	}
	fmt.Printf("%x\n", tx)
	msgTx := &wire.MsgTx{}
	err = msgTx.Deserialize(bytes.NewReader(tx))
	if err != nil {
		return err
	}
	spew.Dump(msgTx)

	unspent, err := ec.ListUnspent(ctx)
	if err != nil {
		return err
	}
	spew.Dump(unspent)

	// These addresses should be from your wallet.
	myAddr := "tltc1q74fcwvn5ydl0ssf7pvdscx2gu6aaxzavf2u7lr"
	valid, mine, err := ec.CheckAddress(ctx, myAddr)
	if err != nil {
		return err
	}
	fmt.Printf("valid: %v, mine: %v\n", valid, mine)

	notMine := "tltc1qtmjdzc3xmju803e529jv9l2860w0h47c95h37f"
	valid, mine, err = ec.CheckAddress(ctx, notMine)
	if err != nil {
		return err
	}
	fmt.Printf("valid: %v, mine: %v\n", valid, mine)

	invalid := "tltc1qtmjdzc3xmju803e529jv9l2860w0h47c95xxxx"
	valid, mine, err = ec.CheckAddress(ctx, invalid)
	if err != nil {
		return err
	}
	fmt.Printf("valid: %v, mine: %v\n", valid, mine)

	wifStr, err := ec.GetPrivateKeys(ctx, walletPass, myAddr)
	if err != nil {
		return err
	}
	wif, err := btcutil.DecodeWIF(wifStr)
	if err != nil {
		return err
	}
	pkh := btcutil.Hash160(wif.SerializePubKey())
	p2wpkh, err := btcutil.NewAddressWitnessPubKeyHash(pkh, dexltc.TestNet4Params)
	if err != nil {
		return err
	}
	if addr := p2wpkh.String(); addr != myAddr {
		return fmt.Errorf("%v != %v", addr, myAddr)
	}
	// fmt.Println(wif.PrivKey.Serialize())

	txRaw, err := ec.PayTo(ctx, walletPass, "tltc1qmk8uqqn37a2r5lu9jc86jnat6j0y28ydmfuadd", 1.21, 17.3454)
	if err != nil {
		return err
	}
	fmt.Printf("%x\n", txRaw) // this is signed

	txid, err := ec.Broadcast(ctx, txRaw)
	if err != nil {
		return err
	}
	fmt.Println(txid)

	confs, err := ec.GetWalletTxConfs(ctx, "6e1b5b3e9555507aab1ce18a6d2b327d0d6a624359543dd430eccbfefa3585dc")
	if err != nil {
		return err // if not a wallet txn: 'code 1: "Transaction not in wallet."'
	}
	fmt.Println("Confs:", confs)

	_, err = ec.GetWalletTxConfs(ctx, "958e86e68a6d8c84e0fc9d1f0d7ca39cc411cf31388ef37420d048ffb99bb518")
	if err == nil {
		return errors.New("worked for non-wallet txn???")
	}

	bal, err := ec.GetBalance(ctx)
	if err != nil {
		return err
	}
	fmt.Println(bal.Confirmed, bal.Unconfirmed, bal.Immature)

	addr, err := ec.GetUnusedAddress(ctx)
	if err != nil {
		return err
	}
	fmt.Println(addr)
	return nil
}
