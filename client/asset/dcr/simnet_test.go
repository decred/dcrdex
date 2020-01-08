// +build harness

package dcr

// Simnet tests expect the DCR test harness to be running.
//
// Sim harness info:
// The harness has two wallets, alpha and beta
// Both wallets have confirmed UTXOs.
// The alpha wallet has large coinbase outputs and smaller change outputs.
// The beta wallet has mature vote outputs and regular transaction outputs of
// varying size and confirmation count. Value:Confirmations =
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
	dexdcr "decred.org/dcrdex/dex/dcr"
	"github.com/decred/dcrd/chaincfg/v2"
	"github.com/decred/slog"
)

const (
	walletPassword = "123"
	alphaAddress   = "SsonWQK6xkLSYm7VttCddHWkWWhETFMdR4Y"
	betaAddress    = "Ssb7Peny8omFwicUi8hC6hCALhcEp59p7UK"
)

var (
	tLogger dex.Logger
	tCtx    context.Context
	tDCR    = &dex.Asset{
		ID:       42,
		Symbol:   "ltc",
		SwapSize: dexdcr.InitTxSize,
		FeeRate:  10,
		LotSize:  1e7,
		RateStep: 100,
		SwapConf: 1,
		FundConf: 1,
	}
)

func mineAlpha() error {
	return exec.Command("tmux", "send-keys", "-t", "dcr-harness:4", "./mine-alpha 1", "C-m").Run()
}

func tBackend(t *testing.T, name string, blkFunc func(string, error)) *ExchangeWallet {
	user, err := user.Current()
	if err != nil {
		t.Fatalf("error getting current user: %v", err)
	}
	walletCfg := &WalletConfig{
		INIPath:   filepath.Join(user.HomeDir, "dextest", "dcr", name, "w-"+name+".conf"),
		Account:   "default",
		AssetInfo: tDCR,
		TipChange: func(err error) {
			blkFunc(name, err)
		},
	}
	var backend asset.Wallet
	backend, err = NewWallet(tCtx, walletCfg, tLogger, dex.Simnet)
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

func newTestRig(t *testing.T, blkFunc func(string, error)) *testRig {
	rig := &testRig{
		backends: make(map[string]*ExchangeWallet),
	}
	rig.backends["alpha"] = tBackend(t, "alpha", blkFunc)
	rig.backends["beta"] = tBackend(t, "beta", blkFunc)
	return rig
}

func randBytes(l int) []byte {
	b := make([]byte, l)
	rand.Read(b)
	return b
}

func waitNetwork() {
	time.Sleep(time.Second)
}

func TestMain(m *testing.M) {
	chainParams = chaincfg.SimNetParams()
	tLogger = slog.NewBackend(os.Stdout).Logger("TEST")
	tLogger.SetLevel(slog.LevelTrace)
	var shutdown func()
	tCtx, shutdown = context.WithCancel(context.Background())
	doIt := func() int {
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
	contractValue := toAtoms(2)

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
		available, unconf, err := wallet.Balance()
		tLogger.Debugf("%s %f available, %f unconfirmed", name, float64(available)/1e8, float64(unconf)/1e8)
		if err != nil {
			t.Fatalf("error getting available: %v", err)
		}
	}
	// Grab some coins.
	utxos, err := rig.beta().Fund(contractValue * 3)
	if err != nil {
		t.Fatalf("Funding error: %v", err)
	}
	utxo := utxos[0]

	// Coins should be locked
	utxos, _ = rig.beta().Fund(contractValue * 3)
	if inUTXOs(utxo, utxos) {
		t.Fatalf("received locked output")
	}
	// Now unlock, and see if we get the first one back.
	rig.beta().ReturnCoins([]asset.Coin{utxo})
	rig.beta().ReturnCoins(utxos)
	utxos, _ = rig.beta().Fund(contractValue * 3)
	if !inUTXOs(utxo, utxos) {
		t.Fatalf("unlocked output not returned")
	}
	rig.beta().ReturnCoins(utxos)

	// Get a separate set of UTXOs for each contract.
	utxos1, err := rig.beta().Fund(contractValue)
	if err != nil {
		t.Fatalf("error funding first contract: %v", err)
	}
	// Get a separate set of UTXOs for each contract.
	utxos2, err := rig.beta().Fund(contractValue * 2)
	if err != nil {
		t.Fatalf("error funding second contract: %v", err)
	}

	// Unlock the wallet for use.
	err = rig.beta().Unlock(walletPassword, time.Hour*24)
	if err != nil {
		t.Fatalf("error unlocking beta wallet: %v", err)
	}

	secretKey1 := randBytes(32)
	keyHash1 := sha256.Sum256(secretKey1)
	secretKey2 := randBytes(32)
	keyHash2 := sha256.Sum256(secretKey2)
	lockTime := time.Now().Add(time.Hour * 24).UTC()
	// Have beta send a swap contract to the alpha address.
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
	swap1 := &asset.Swap{
		Inputs:   utxos1,
		Contract: contract1,
	}
	swap2 := &asset.Swap{
		Inputs:   utxos2,
		Contract: contract2,
	}
	swaps := []*asset.Swap{swap1, swap2}

	receipts, err := rig.beta().Swap(swaps)
	if err != nil {
		t.Fatalf("error sending swap transaction: %v", err)
	}

	if len(receipts) != 2 {
		t.Fatalf("expected 1 receipt, got %d", len(receipts))
	}

	// Let alpha get and process that transaction.
	waitNetwork()

	makeRedemption := func(swapVal uint64, receipt asset.Receipt, secret []byte) *asset.Redemption {
		swapOutput := receipt.Coin()
		ci, err := rig.alpha().AuditContract(swapOutput.ID(), swapOutput.Redeem())
		if err != nil {
			t.Fatalf("error auditing contract: %v", err)
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

	err = rig.alpha().Redeem(redemptions)
	if err != nil {
		t.Fatalf("redemption error: %v", err)
	}

	// Find the redemption
	receipt := receipts[0]
	ctx, _ := context.WithDeadline(tCtx, time.Now().Add(time.Second*5))
	waitNetwork()
	checkKey, err := rig.beta().FindRedemption(ctx, receipt.Coin().ID())
	if err != nil {
		t.Fatalf("error finding unconfirmed redemption: %v", err)
	}
	if !bytes.Equal(checkKey, secretKey1) {
		t.Fatalf("findRedemption (unconfirmed) key mismatch. %x != %x", checkKey, secretKey1)
	}

	// Mine a block and find the redemption again.
	mineAlpha()
	waitNetwork()
	if !blockReported {
		t.Fatalf("no block reported")
	}
	_, err = rig.beta().FindRedemption(ctx, receipt.Coin().ID())
	if err != nil {
		t.Fatalf("error finding confirmed redemption: %v", err)
	}

	// Confirmations should now be an error, since the swap output has been spent.
	_, err = receipt.Coin().Confirmations()
	if err == nil {
		t.Fatalf("no error getting confirmations for redeemed swap. has swap output been spent?")
	}

	// Now send another one with lockTime = now and try to refund it.
	secretKey := randBytes(32)
	keyHash := sha256.Sum256(secretKey)
	lockTime = time.Now().Add(-24 * time.Hour)

	// Have beta send a swap contract to the alpha address.
	utxos, _ = rig.beta().Fund(contractValue)
	contract := &asset.Contract{
		Address:    alphaAddress,
		Value:      contractValue,
		SecretHash: keyHash[:],
		LockTime:   uint64(lockTime.Unix()),
	}
	swap := &asset.Swap{
		Inputs:   utxos,
		Contract: contract,
	}
	swaps = []*asset.Swap{swap}

	receipts, err = rig.beta().Swap(swaps)
	if err != nil {
		t.Fatalf("error sending swap transaction: %v", err)
	}

	if len(receipts) != 1 {
		t.Fatalf("expected 1 receipt, got %d", len(receipts))
	}
	receipt = receipts[0]

	waitNetwork()
	err = rig.beta().Refund(receipt)
	if err != nil {
		t.Fatalf("refund error: %v", err)
	}

	// Lock the wallet
	err = rig.beta().Lock()
	if err != nil {
		t.Fatalf("error locking wallet: %v", err)
	}
}
