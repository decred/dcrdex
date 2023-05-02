//go:build harness

package btc

// Simnet tests expect the BTC test harness to be running.

import (
	"bytes"
	"context"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"testing"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/config"
	dexbtc "decred.org/dcrdex/dex/networks/btc"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

var (
	tLogger   dex.Logger
	tCtx      context.Context
	tLotSize  uint64 = 1e7
	tRateStep uint64 = 100
	tBTC             = &dex.Asset{
		ID:           0,
		Symbol:       "btc",
		Version:      version,
		SwapSize:     dexbtc.InitTxSize,
		SwapSizeBase: dexbtc.InitTxSizeBase,
		MaxFeeRate:   10,
		SwapConf:     1,
	}
)

func mineAlpha() error {
	return exec.Command("tmux", "send-keys", "-t", "btc-harness:0", "./mine-alpha 1", "C-m").Run()
}

func mineBeta() error {
	return exec.Command("tmux", "send-keys", "-t", "btc-harness:0", "./mine-beta 1", "C-m").Run()
}

func tBackend(t *testing.T, name string, blkFunc func(string, error)) (*ExchangeWalletAccelerator, *dex.ConnectionMaster) {
	t.Helper()
	user, err := user.Current()
	if err != nil {
		t.Fatalf("error getting current user: %v", err)
	}
	cfgPath := filepath.Join(user.HomeDir, "dextest", "btc", name, name+".conf")
	settings, err := config.Parse(cfgPath)
	if err != nil {
		t.Fatalf("error reading config options: %v", err)
	}
	// settings["account"] = "default"
	walletCfg := &asset.WalletConfig{
		Type:     walletTypeRPC,
		Settings: settings,
		TipChange: func(err error) {
			blkFunc(name, err)
		},
		PeersChange: func(num uint32, err error) {
			t.Logf("peer count = %d, err = %v", num, err)
		},
	}
	var backend asset.Wallet
	backend, err = NewWallet(walletCfg, tLogger, dex.Simnet)
	if err != nil {
		t.Fatalf("error creating backend: %v", err)
	}
	cm := dex.NewConnectionMaster(backend)
	err = cm.Connect(tCtx)
	if err != nil {
		t.Fatalf("error connecting backend: %v", err)
	}
	return backend.(*ExchangeWalletAccelerator), cm
}

type testRig struct {
	backends          map[string]*ExchangeWalletAccelerator
	connectionMasters map[string]*dex.ConnectionMaster
}

func newTestRig(t *testing.T, blkFunc func(string, error)) *testRig {
	t.Helper()
	rig := &testRig{
		backends:          make(map[string]*ExchangeWalletAccelerator),
		connectionMasters: make(map[string]*dex.ConnectionMaster, 3),
	}
	rig.backends["alpha"], rig.connectionMasters["alpha"] = tBackend(t, "alpha", blkFunc)
	rig.backends["beta"], rig.connectionMasters["beta"] = tBackend(t, "beta", blkFunc)
	return rig
}

func (rig *testRig) alpha() *ExchangeWalletAccelerator {
	return rig.backends["alpha"]
}
func (rig *testRig) beta() *ExchangeWalletAccelerator {
	return rig.backends["beta"]
}
func (rig *testRig) close(t *testing.T) {
	t.Helper()
	for name, cm := range rig.connectionMasters {
		closed := make(chan struct{})
		go func() {
			cm.Disconnect()
			close(closed)
		}()
		select {
		case <-closed:
		case <-time.NewTimer(time.Second).C:
			t.Fatalf("failed to disconnect from %s", name)
		}
	}
}

func randBytes(l int) []byte {
	b := make([]byte, l)
	rand.Read(b)
	return b
}

func waitNetwork() {
	time.Sleep(time.Second * 3 / 2)
}

func TestMain(m *testing.M) {
	tLogger = dex.StdOutLogger("TEST", dex.LevelTrace)
	var shutdown func()
	tCtx, shutdown = context.WithCancel(context.Background())
	doIt := func() int {
		defer shutdown()
		return m.Run()
	}
	os.Exit(doIt())
}

func TestMakeBondTx(t *testing.T) {
	rig := newTestRig(t, func(name string, err error) {
		tLogger.Infof("%s has reported a new block, error = %v", name, err)
	})
	defer rig.close(t)

	// Get a private key for the bond script. This would come from the client's
	// HD key chain.
	priv, err := secp256k1.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	pubkey := priv.PubKey()

	acctID := randBytes(32)
	fee := uint64(10_2030_4050) //  ~10.2 DCR
	const bondVer = 0

	wallet := rig.alpha()

	// Unlock the wallet to sign the tx and get keys.
	err = wallet.Unlock([]byte("abc"))
	if err != nil {
		t.Fatalf("error unlocking beta wallet: %v", err)
	}

	lockTime := time.Now().Add(10 * time.Second)
	bond, _, err := wallet.MakeBondTx(bondVer, fee, 10, lockTime, priv, acctID)
	if err != nil {
		t.Fatal(err)
	}
	coinhash, _, err := decodeCoinID(bond.CoinID)
	if err != nil {
		t.Fatalf("decodeCoinID: %v", err)
	}
	t.Logf("bond txid %v\n", coinhash)
	t.Logf("signed tx: %x\n", bond.SignedTx)
	t.Logf("unsigned tx: %x\n", bond.UnsignedTx)
	t.Logf("bond script: %x\n", bond.Data)
	t.Logf("redeem tx: %x\n", bond.RedeemTx)
	_, err = msgTxFromBytes(bond.SignedTx)
	if err != nil {
		t.Fatalf("invalid bond tx: %v", err)
	}

	pkh := btcutil.Hash160(pubkey.SerializeCompressed())

	lockTimeUint, pkhPush, err := dexbtc.ExtractBondDetailsV0(0, bond.Data)
	if err != nil {
		t.Fatalf("ExtractBondDetailsV0: %v", err)
	}
	if !bytes.Equal(pkh, pkhPush) {
		t.Fatalf("mismatching pubkeyhash in bond script and signature (%x != %x)", pkh, pkhPush)
	}

	if lockTime.Unix() != int64(lockTimeUint) {
		t.Fatalf("mismatching locktimes (%d != %d)", lockTime.Unix(), lockTimeUint)
	}
	lockTimePush := time.Unix(int64(lockTimeUint), 0)
	t.Logf("lock time in bond script: %v", lockTimePush)

	sendBondTx, err := wallet.SendTransaction(bond.SignedTx)
	if err != nil {
		t.Fatalf("RefundBond: %v", err)
	}
	sendBondTxid, _, err := decodeCoinID(sendBondTx)
	if err != nil {
		t.Fatalf("decodeCoinID: %v", err)
	}
	t.Logf("sendBondTxid: %v\n", sendBondTxid)

	waitNetwork() // wait for alpha to see the txn
	mineAlpha()
	waitNetwork() // wait for beta to see the new block (bond must be mined for RefundBond)

	var expired bool
	for !expired {
		expired, err = wallet.LockTimeExpired(tCtx, lockTime)
		if err != nil {
			t.Fatalf("LocktimeExpired: %v", err)
		}
		if expired {
			break
		}
		fmt.Println("bond still not expired")
		time.Sleep(15 * time.Second)
	}

	refundCoin, err := wallet.RefundBond(context.Background(), bondVer, bond.CoinID,
		bond.Data, bond.Amount, priv)
	if err != nil {
		t.Fatalf("RefundBond: %v", err)
	}
	t.Logf("refundCoin: %v\n", refundCoin)
}

func TestSendEstimation(t *testing.T) {
	rig := newTestRig(t, func(name string, err error) {
		tLogger.Infof("%s has reported a new block, error = %v", name, err)
	})
	defer rig.close(t)

	addr, _ := btcutil.DecodeAddress("bcrt1qs6d2lpkcfccus6q7c0dvjnlpf5g45gf7yak6mm", &chaincfg.RegressionNetParams)
	pkScript, _ := txscript.PayToAddrScript(addr)
	tx := wire.NewMsgTx(wire.TxVersion)
	tx.AddTxOut(wire.NewTxOut(10e8, pkScript))

	// Use alpha, since there are many utxos.
	w := rig.alpha()
	const numCycles = 100
	tStart := time.Now()
	for i := 0; i < numCycles; i++ {
		_, err := w.estimateSendTxFee(tx, 20, false)
		if err != nil {
			t.Fatalf("Error estimating with utxos: %v", err)
		}
	}
	fmt.Println("Time to pick utxos ourselves:", time.Since(tStart))
	node := w.node.(*rpcClient)
	tStart = time.Now()
	for i := 0; i < numCycles; i++ {
		_, err := node.estimateSendTxFee(tx, 20, false)
		if err != nil {
			t.Fatalf("Error estimating with utxos: %v", err)
		}
	}
	fmt.Println("Time to use fundrawtransaction:", time.Since(tStart))
}
