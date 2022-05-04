// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build electrumlive

package btc

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	"decred.org/dcrdex/client/asset"
	"github.com/btcsuite/btcd/btcutil/psbt"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
)

const walletPass = "walletpass"

func TestPSBT(t *testing.T) {
	// an unsigned swap txn funded by a single p2wpkh
	txRaw, _ := hex.DecodeString("010000000118f36e05994f8e69dace16f4a3d4c9ff8db6f58bf48d498b1c0d6daf63c6f8690100000000ffffffff02408e2c0000000000220020e0133024bb27f510f6959467df91ed5daa791f598ba1c09ef1f1ac4540bfdda16e7710000000000016001460a8dbedd538501e2ad9e9f5a4d8a11e35a6c4e700000000")
	msgTx := wire.NewMsgTx(wire.TxVersion)
	err := msgTx.Deserialize(bytes.NewReader(txRaw))
	if err != nil {
		t.Fatal(err)
	}

	packet, err := psbt.NewFromUnsignedTx(msgTx)
	if err != nil {
		t.Fatal(err)
	}
	enc, err := packet.B64Encode()
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println(enc)
}

// This test uses a testnet BTC Electrum wallet listening on localhost 6789.

func TestElectrumExchangeWallet(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tipChanged := make(chan struct{}, 1)
	walletCfg := &asset.WalletConfig{
		Type: walletTypeElectrum,
		Settings: map[string]string{
			"rpcuser":     "user",
			"rpcpassword": "pass",
			"rpcbind":     "127.0.0.1:6789",
		},
		TipChange: func(error) {
			select {
			case tipChanged <- struct{}{}:
				t.Log("tip change, that's enough testing")
				time.Sleep(300 * time.Millisecond)
				cancel()
			default:
			}
		},
		PeersChange: func(num uint32, err error) {
			t.Logf("peer count = %d, err = %v", num, err)
		},
	}
	cfg := &BTCCloneCFG{
		WalletCFG:           walletCfg,
		Symbol:              "btc",
		Logger:              tLogger,
		ChainParams:         &chaincfg.TestNet3Params,
		WalletInfo:          WalletInfo,
		DefaultFallbackFee:  defaultFee,
		DefaultFeeRateLimit: defaultFeeRateLimit,
		Segwit:              true,
	}
	eew, err := ElectrumWallet(cfg)
	if err != nil {
		t.Fatal(err)
	}

	wg, err := eew.Connect(ctx)
	if err != nil {
		t.Fatal(err)
	}

	addr, err := eew.DepositAddress()
	if err != nil {
		t.Fatal(err)
	}
	t.Log(addr)

	feeRate := eew.FeeRate()
	if feeRate == 0 {
		t.Fatal("zero fee rate")
	}
	t.Log("feeRate:", feeRate)

	// findRedemption
	swapTxHash, _ := chainhash.NewHashFromStr("1e99930f76638e3eddd79de94bf0ff574c7a400d1e6986cd61b3b5fd8212b1a3")
	swapVout := uint32(0)
	redeemTxHash, _ := chainhash.NewHashFromStr("4283c1fefa4898eb1bd2041547cd6361f8167dec004890b58ba1817914dc8541")
	redeemVin := uint32(0)
	// P2WSH: tb1q9vqw464klshj80vklss0ms4h82082y8q86x8cen3934r7zrvt6vs4e3m8u / 00202b00eaeab6fc2f23bd96fc20fdc2b73a9e7510e03e8c7c66712c6a3f086c5e99
	// contract: 6382012088a820135a45665765d68dc255ecd5f1870a1b29b8f1fd95c1e02ca5151e354d2a2cf68876a9144fdb2cad8e98983b25675d3ebe2133e650c56dbf6704ecb0d262b17576a9144fdb2cad8e98983b25675d3ebe2133e650c56dbf6888ac
	contract, _ := hex.DecodeString("6382012088a820135a45665765d68dc255ecd5f1870a1b29b8f1fd95c1e02ca5151e354d2a2cf68876a9144fdb2cad8e98983b25675d3ebe2133e650c56dbf6704ecb0d262b17576a9144fdb2cad8e98983b25675d3ebe2133e650c56dbf6888ac")
	// contractHash, _ := hex.DecodeString("2b00eaeab6fc2f23bd96fc20fdc2b73a9e7510e03e8c7c66712c6a3f086c5e99")
	contractHash := sha256.Sum256(contract)
	wantSecret, _ := hex.DecodeString("aa8e04bb335da65d362b89ec0630dc76fd02ffaca783ae58cb712a2820f504ce")
	foundTxHash, foundVin, secret, err := eew.findRedemption(ctx, newOutPoint(swapTxHash, swapVout), contractHash[:])
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(secret, wantSecret) {
		t.Errorf("incorrect secret %x, wanted %x", secret, wantSecret)
	}
	if !foundTxHash.IsEqual(redeemTxHash) {
		t.Errorf("incorrect redeem tx hash %v, wanted %v", foundTxHash, redeemTxHash)
	}
	if redeemVin != foundVin {
		t.Errorf("incorrect redeem tx input %d, wanted %d", foundVin, redeemVin)
	}

	// FindRedemption
	redeemCoin, secretBytes, err := eew.FindRedemption(ctx, toCoinID(swapTxHash, swapVout), contract)
	if err != nil {
		t.Fatal(err)
	}
	foundTxHash, foundVin, err = decodeCoinID(redeemCoin)
	if err != nil {
		t.Fatal(err)
	}
	if !foundTxHash.IsEqual(redeemTxHash) {
		t.Errorf("incorrect redeem tx hash %v, wanted %v", foundTxHash, redeemTxHash)
	}
	if redeemVin != foundVin {
		t.Errorf("incorrect redeem tx input %d, wanted %d", foundVin, redeemVin)
	}
	if !secretBytes.Equal(wantSecret) {
		t.Errorf("incorrect secret %v, wanted %x", secretBytes, wantSecret)
	}

	t.Logf("Found redemption of contract %v:%d at %v:%d!", swapTxHash, swapVout, foundTxHash, foundVin)

	// err = eew.Unlock([]byte(walletPass))
	// if err != nil {
	// 	t.Fatal(err)
	// }

	// wdCoin, err := eew.Withdraw(addr, toSatoshi(1.2435), feeRate)
	// if err != nil {
	// 	t.Fatal(err)
	// }
	// t.Log(wdCoin.String())

	select {
	case <-time.After(10 * time.Second): // a bit of best block polling
		cancel()
	case <-ctx.Done(): // or until TipChange cancels the context
	}

	wg.Wait()
}
