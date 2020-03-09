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

func tBackend(t *testing.T, conf, name string, blkFunc func(string, error)) (*ExchangeWallet, *dex.ConnectionMaster) {
	user, err := user.Current()
	if err != nil {
		t.Fatalf("error getting current user: %v", err)
	}
	walletCfg := &asset.WalletConfig{
		INIPath: filepath.Join(user.HomeDir, "dextest", "btc", "harness-ctl", conf+".conf"),
		Account: name,
		TipChange: func(err error) {
			blkFunc(conf, err)
		},
	}
	var backend asset.Wallet
	backend, err = NewWallet(walletCfg, tLogger, dex.Regtest)
	if err != nil {
		t.Fatalf("error creating backend: %v", err)
	}
	cm := dex.NewConnectionMaster(backend)
	err = cm.Connect(tCtx)
	if err != nil {
		t.Fatalf("error connecting backend: %v", err)
	}
	return backend.(*ExchangeWallet), cm
}

type testRig struct {
	backends          map[string]*ExchangeWallet
	connectionMasters map[string]*dex.ConnectionMaster
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
func (rig *testRig) close(t *testing.T) {
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

func newTestRig(t *testing.T, blkFunc func(string, error)) *testRig {
	rig := &testRig{
		backends:          make(map[string]*ExchangeWallet),
		connectionMasters: make(map[string]*dex.ConnectionMaster, 3),
	}
	rig.backends["alpha"], rig.connectionMasters["alpha"] = tBackend(t, "alpha", "", blkFunc)
	rig.backends["beta"], rig.connectionMasters["beta"] = tBackend(t, "beta", "", blkFunc)
	rig.backends["gamma"], rig.connectionMasters["gamma"] = tBackend(t, "alpha", "gamma", blkFunc)
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
	defer rig.close(t)
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
		available, unconf, err := wallet.Balance(tBTC.FundConf)
		tLogger.Debugf("%s %f available, %f unconfirmed", name, float64(available)/1e8, float64(unconf)/1e8)
		if err != nil {
			t.Fatalf("error getting available: %v", err)
		}
	}
	// Gamma should only have 10 BTC utxos, so calling fund for less should only
	// return 1 utxo.
	utxos, err := rig.gamma().Fund(contractValue*3, tBTC)
	if err != nil {
		t.Fatalf("Funding error: %v", err)
	}
	utxo := utxos[0]

	// UTXOs should be locked
	utxos, _ = rig.gamma().Fund(contractValue*3, tBTC)
	if inUTXOs(utxo, utxos) {
		t.Fatalf("received locked output")
	}
	// Unlock
	rig.gamma().ReturnCoins([]asset.Coin{utxo})
	rig.gamma().ReturnCoins(utxos)
	// Make sure we get the first utxo back with Fund.
	utxos, _ = rig.gamma().Fund(contractValue*3, tBTC)
	if !inUTXOs(utxo, utxos) {
		t.Fatalf("unlocked output not returned")
	}
	rig.gamma().ReturnCoins(utxos)

	// Get a separate set of UTXOs for each contract.
	utxos1, err := rig.gamma().Fund(contractValue, tBTC)
	if err != nil {
		t.Fatalf("error funding first contract: %v", err)
	}
	// Get a separate set of UTXOs for each contract.
	utxos2, err := rig.gamma().Fund(contractValue*2, tBTC)
	if err != nil {
		t.Fatalf("error funding second contract: %v", err)
	}

	// Unlock the wallet for use.
	err = rig.gamma().Unlock(gammaPassword, time.Hour*24)
	if err != nil {
		t.Fatalf("error unlocking gamma wallet: %v", err)
	}

	secretKey1 := randBytes(32)
	keyHash1 := sha256.Sum256(secretKey1)
	secretKey2 := randBytes(32)
	keyHash2 := sha256.Sum256(secretKey2)
	lockTime := time.Now().Add(time.Hour * 24).UTC()
	// Have gamma send a swap contract to the alpha address.
	contract1 := &asset.Contract{
		Address:    alphaAddress,
		Value:      contractValue,
		SecretHash: keyHash1[:],
		LockTime:   uint64(lockTime.Unix()),
	}
	contract2 := &asset.Contract{
		Address:    alphaAddress,
		Value:      contractValue * 2,
		SecretHash: keyHash2[:],
		LockTime:   uint64(lockTime.Unix()),
	}
	swaps := &asset.Swaps{
		Inputs:    append(utxos1, utxos2...),
		Contracts: []*asset.Contract{contract1, contract2},
	}

	receipts, _, err := rig.gamma().Swap(swaps, tBTC)
	if err != nil {
		t.Fatalf("error sending swap transaction: %v", err)
	}

	if len(receipts) != 2 {
		t.Fatalf("expected 1 receipt, got %d", len(receipts))
	}

	confCoin := receipts[0].Coin()
	checkConfs := func(n uint32) {
		confs, err := rig.gamma().Confirmations(confCoin.ID())
		if err != nil {
			t.Fatalf("error getting %d confs: %v", n, err)
		}
		if confs != n {
			t.Fatalf("expected %d confs, got %d", n, confs)
		}
	}
	// Check that there are 0 confirmations.
	checkConfs(0)

	makeRedemption := func(swapVal uint64, receipt asset.Receipt, secret []byte) *asset.Redemption {
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
		if swapOutput.Value() != swapVal {
			t.Fatalf("wrong contract value. wanted %d, got %d", swapVal, swapOutput.Value())
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
		return &asset.Redemption{
			Spends: ci,
			Secret: secret,
		}
	}

	redemptions := []*asset.Redemption{
		makeRedemption(contractValue, receipts[0], secretKey1),
		makeRedemption(contractValue*2, receipts[1], secretKey2),
	}

	_, err = rig.alpha().Redeem(redemptions, tBTC)
	if err != nil {
		t.Fatalf("redemption error: %v", err)
	}

	// Find the redemption
	receipt := receipts[0]
	ctx, _ := context.WithDeadline(tCtx, time.Now().Add(time.Second*5))
	checkKey, err := rig.gamma().FindRedemption(ctx, receipt.Coin().ID())
	if err != nil {
		t.Fatalf("error finding unconfirmed redemption: %v", err)
	}
	if !bytes.Equal(checkKey, secretKey1) {
		t.Fatalf("findRedemption (unconfirmed) key mismatch. %x != %x", checkKey, secretKey1)
	}

	// Mine a block and find the redemption again.
	mineAlpha()
	time.Sleep(tBlockWait)
	if !blockReported {
		t.Fatalf("no block reported")
	}
	// Check that there is 1 confirmation on the swap
	checkConfs(1)
	_, err = rig.gamma().FindRedemption(ctx, receipt.Coin().ID())
	if err != nil {
		t.Fatalf("error finding confirmed redemption: %v", err)
	}

	// Confirmations should now be an error, since the swap output has been spent.
	_, err = receipt.Coin().Confirmations()
	if err == nil {
		t.Fatalf("error getting confirmations. has swap output been spent?")
	}

	// Now send another one with lockTime = now and try to refund it.
	secretKey := randBytes(32)
	keyHash := sha256.Sum256(secretKey)
	lockTime = time.Now().Add(-24 * time.Hour)

	// Have gamma send a swap contract to the alpha address.
	utxos, _ = rig.gamma().Fund(contractValue, tBTC)
	contract := &asset.Contract{
		Address:    alphaAddress,
		Value:      contractValue,
		SecretHash: keyHash[:],
		LockTime:   uint64(lockTime.Unix()),
	}
	swaps = &asset.Swaps{
		Inputs:    utxos,
		Contracts: []*asset.Contract{contract},
	}

	time.Sleep(time.Second)

	receipts, _, err = rig.gamma().Swap(swaps, tBTC)
	if err != nil {
		t.Fatalf("error sending swap transaction: %v", err)
	}

	if len(receipts) != 1 {
		t.Fatalf("expected 1 receipt, got %d", len(receipts))
	}
	receipt = receipts[0]

	err = rig.gamma().Refund(receipt, tBTC)
	if err != nil {
		t.Fatalf("refund error: %v", err)
	}

	// Test PayFee
	coin, err := rig.gamma().PayFee(alphaAddress, 1e8, tBTC)
	if err != nil {
		t.Fatalf("error paying fees: %v", err)
	}
	tLogger.Infof("fee paid with tx %s", coin.String())

	// Test Withdraw
	coin, err = rig.gamma().Withdraw(alphaAddress, 5e7, 10)
	if err != nil {
		t.Fatalf("error withdrawing: %v", err)
	}
	tLogger.Infof("withdrew with tx %s", coin.String())

	// Lock the wallet
	err = rig.gamma().Lock()
	if err != nil {
		t.Fatalf("error locking wallet: %v", err)
	}
}
