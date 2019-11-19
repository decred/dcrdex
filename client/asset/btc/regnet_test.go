// +build harness

package btc

// Regnet tests expect the BTC test harness to be running.
//
// Sim harness info:
// The harness has three wallets, alpha, beta, and gamma.
// All three wallets have confirmed UTXOs.
// The beta wallet has only coinbase outputs.
// The alpha wallet has coinbase outputs too, but has sent some to the gamma
//   wallet, so also has some change outputs.
// The gamma wallet has regular transaction outputs of varying size and
// confirmation count. Value:Confirmations =
// 10:8, 18:7, 5:6, 7:5, 1:4, 15:3, 3:2, 25:1

import (
	"bytes"
	"context"
	"crypto/sha256"
	"math/rand"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"testing"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	dexbtc "decred.org/dcrdex/dex/btc"
	"github.com/decred/slog"
)

const (
	gammaAddress  = "n2GFTvKNjyTXNMh1UjL5rZnc6kbJX2RPi4"
	gammaPassword = "abc"
	alphaAddress  = "mjqAiNeRe8jWzTUnEYL9CYa2YKjFWQDjJY"
	betaAddress   = "ms1dwotcBovBWCfffVjBMnfFmPeP9xBsph"
)

var (
	tLogger dex.Logger
	tCtx    context.Context
	tBTC    = &dex.Asset{
		ID:       0,
		Symbol:   "btc",
		SwapSize: dexbtc.InitTxSize,
		FeeRate:  2,
		LotSize:  1e6,
		RateStep: 10,
		SwapConf: 1,
		FundConf: 1,
	}
	tBlockTick = time.Second
	tBlockWait = tBlockTick + time.Millisecond*50
)

func mineAlpha() error {
	return exec.Command("tmux", "send-keys", "-t", "btc-harness:2", "./mine-alpha 1", "C-m").Run()
}

func tBackend(t *testing.T, conf, name string, blkFunc func(string, error)) *ExchangeWallet {
	user, err := user.Current()
	if err != nil {
		t.Fatalf("error getting current user: %v", err)
	}
	walletCfg := &WalletConfig{
		INIPath:    filepath.Join(user.HomeDir, "btc-harness", "harness-ctl", conf+".conf"),
		WalletName: name,
		AssetInfo:  tBTC,
		TipChange: func(err error) {
			blkFunc(conf, err)
		},
	}
	var backend asset.Wallet
	backend, err = NewWallet(tCtx, walletCfg, tLogger, dex.Regtest)
	if err != nil {
		t.Fatalf("error creating backend: %v", err)
	}
	return backend.(*ExchangeWallet)
}

type testRig struct {
	backends map[string]*ExchangeWallet
}

func (rig *testRig) alpha() *ExchangeWallet {
	return rig.backends["alpha"]
}
func (rig *testRig) beta() *ExchangeWallet {
	return rig.backends["beta"]
}
func (rig *testRig) gamma() *ExchangeWallet {
	return rig.backends["gamma"]
}

func newTestRig(t *testing.T, blkFunc func(string, error)) *testRig {
	rig := &testRig{
		backends: make(map[string]*ExchangeWallet),
	}
	rig.backends["alpha"] = tBackend(t, "alpha", "", blkFunc)
	rig.backends["beta"] = tBackend(t, "beta", "", blkFunc)
	rig.backends["gamma"] = tBackend(t, "alpha", "gamma", blkFunc)
	return rig
}

func randBytes(l int) []byte {
	b := make([]byte, l)
	rand.Read(b)
	return b
}

func TestMain(m *testing.M) {
	blockTicker = tBlockTick
	tLogger = slog.NewBackend(os.Stdout).Logger("TEST")
	tLogger.SetLevel(slog.LevelTrace)
	var shutdown func()
	tCtx, shutdown = context.WithCancel(context.Background())
	doIt := func() int {
		// Not counted as coverage, must test Archiver constructor explicitly.
		defer shutdown()
		return m.Run()
	}
	os.Exit(doIt())
}

func TestWallet(t *testing.T) {
	blockReported := false
	rig := newTestRig(t, func(name string, err error) {
		blockReported = true
		tLogger.Infof("%s has reported a new block, error = %v", name, err)
	})
	contractValue := toSatoshi(2)

	inUTXOs := func(utxo asset.Coin, utxos []asset.Coin) bool {
		for _, u := range utxos {
			if bytes.Equal(u.ID(), utxo.ID()) {
				return true
			}
		}
		return false
	}

	// Check available amount.
	for name, wallet := range rig.backends {
		available, unconf, err := wallet.Available()
		tLogger.Debugf("%s %f available, %f unconfirmed", name, float64(available)/1e8, float64(unconf)/1e8)
		if err != nil {
			t.Fatalf("error getting available: %v", err)
		}
	}
	// Gamma should only have 10 BTC utxos, so calling fund for less should only
	// return 1 utxo.
	utxos, err := rig.gamma().Fund(contractValue)
	if err != nil {
		t.Fatalf("Funding error: %v", err)
	}
	utxo := utxos[0]

	// UTXOs should be locked
	utxos, _ = rig.gamma().Fund(contractValue)
	if inUTXOs(utxo, utxos) {
		t.Fatalf("received locked output")
	}
	// Now unlock, and see if we get the first one back.
	rig.gamma().ReturnCoins([]asset.Coin{utxo})
	rig.gamma().ReturnCoins(utxos)
	utxos, _ = rig.gamma().Fund(contractValue)
	if !inUTXOs(utxo, utxos) {
		t.Fatalf("unlocked output not returned")
	}

	// Unlock the wallet for use.
	err = rig.gamma().Unlock(gammaPassword, time.Hour*24)
	if err != nil {
		t.Fatalf("error unlocking gamma wallet: %v", err)
	}

	secretKey := randBytes(32)
	keyHash := sha256.Sum256(secretKey)
	lockTime := time.Now().Add(time.Hour * 24).UTC()
	// Have gamma send a swap contract to the alpha address.
	contract := &asset.Contract{
		Address:    alphaAddress,
		Value:      contractValue,
		SecretHash: keyHash[:],
		LockTime:   uint64(lockTime.Unix()),
	}
	swapTx := &asset.SwapTx{
		Inputs:    utxos,
		Contracts: []*asset.Contract{contract},
	}

	receipts, err := rig.gamma().Swap(swapTx)
	if err != nil {
		t.Fatalf("error sending swap transaction: %v", err)
	}

	if len(receipts) != 1 {
		t.Fatalf("expected 1 receipt, got %d", len(receipts))
	}
	receipt := receipts[0]

	// Alpha should be able to redeem.
	swapOutput := receipt.Coin()
	ci, err := rig.alpha().AuditContract(swapOutput.ID(), swapOutput.Redeem())
	if err != nil {
		t.Fatalf("error auditing contract")
	}
	swapOutput = ci.Coin()
	if ci.Recipient() != alphaAddress {
		t.Fatalf("wrong address. %s != %s", ci.Recipient(), alphaAddress)
	}
	if swapOutput.Value() != contractValue {
		t.Fatalf("wrong contract value. wanted %d, got %d", contractValue, swapOutput.Value())
	}
	confs, err := swapOutput.Confirmations()
	if err != nil {
		t.Fatalf("error getting confirmations: %v", err)
	}
	if confs != 0 {
		t.Fatalf("unexpected number of confirmations. wanted 0, got %d", confs)
	}
	if ci.Expiration().Equal(lockTime) {
		t.Fatalf("wrong lock time. wanted %s, got %s", lockTime, ci.Expiration())
	}

	redemption := &asset.Redemption{
		Spends: ci,
		Secret: secretKey,
	}
	err = rig.alpha().Redeem(redemption)
	if err != nil {
		t.Fatalf("redemption error: %v", err)
	}

	// Find the redemption
	ctx, _ := context.WithDeadline(tCtx, time.Now().Add(time.Second*5))
	checkKey, err := rig.gamma().FindRedemption(ctx, swapOutput.ID())
	if err != nil {
		t.Fatalf("error finding unconfirmed redemption: %v", err)
	}
	if !bytes.Equal(checkKey, secretKey) {
		t.Fatalf("findRedemption (unconfirmed) key mismatch. %x != %x", checkKey, secretKey)
	}

	// Mine a block and find the redemption again.
	mineAlpha()
	time.Sleep(tBlockWait)
	if !blockReported {
		t.Fatalf("no block reported")
	}
	_, err = rig.gamma().FindRedemption(ctx, swapOutput.ID())
	if err != nil {
		t.Fatalf("error finding confirmed redemption: %v", err)
	}

	// Confirmations should now be an error, since the swap output has been spent.
	confs, err = swapOutput.Confirmations()
	if err == nil {
		t.Fatalf("error getting confirmations. has swap output been spent?")
	}

	// Now send another one with lockTime = now and try to refund it.
	secretKey = randBytes(32)
	keyHash = sha256.Sum256(secretKey)
	lockTime = time.Now().Add(-24 * time.Hour)

	// Have gamma send a swap contract to the alpha address.
	utxos, _ = rig.gamma().Fund(contractValue)
	contract = &asset.Contract{
		Address:    alphaAddress,
		Value:      contractValue,
		SecretHash: keyHash[:],
		LockTime:   uint64(lockTime.Unix()),
	}
	swapTx = &asset.SwapTx{
		Inputs:    utxos,
		Contracts: []*asset.Contract{contract},
	}

	receipts, err = rig.gamma().Swap(swapTx)
	if err != nil {
		t.Fatalf("error sending swap transaction: %v", err)
	}

	if len(receipts) != 1 {
		t.Fatalf("expected 1 receipt, got %d", len(receipts))
	}
	receipt = receipts[0]

	err = rig.gamma().Refund(receipt)
	if err != nil {
		t.Fatalf("refund error: %v", err)
	}

	// Lock the wallet
	err = rig.gamma().Lock()
	if err != nil {
		t.Fatalf("error locking wallet: %v", err)
	}
}
