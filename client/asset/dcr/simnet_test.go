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
	"decred.org/dcrdex/dex/config"
	dexdcr "decred.org/dcrdex/dex/networks/dcr"
	"github.com/decred/dcrd/chaincfg/v3"
)

const (
	walletPassword = "abc"
	alphaAddress   = "SsWKp7wtdTZYabYFYSc9cnxhwFEjA5g4pFc"
	betaAddress    = "Ssge52jCzbixgFC736RSTrwAnvH3a4hcPRX"
)

var (
	tLogger dex.Logger
	tCtx    context.Context
	tDCR    = &dex.Asset{
		ID:           42,
		Symbol:       "dcr",
		SwapSize:     dexdcr.InitTxSize,
		SwapSizeBase: dexdcr.InitTxSizeBase,
		MaxFeeRate:   10,
		LotSize:      1e7,
		RateStep:     100,
		SwapConf:     1,
	}
)

func mineAlpha() error {
	return exec.Command("tmux", "send-keys", "-t", "dcr-harness:0", "./mine-alpha 1", "C-m").Run()
}

func tBackend(t *testing.T, name string, blkFunc func(string, error)) (*ExchangeWallet, *dex.ConnectionMaster) {
	t.Helper()
	user, err := user.Current()
	if err != nil {
		t.Fatalf("error getting current user: %v", err)
	}
	cfgPath := filepath.Join(user.HomeDir, "dextest", "dcr", name, name+".conf")
	settings, err := config.Parse(cfgPath)
	if err != nil {
		t.Fatalf("error reading config options: %v", err)
	}
	settings["account"] = "default"
	walletCfg := &asset.WalletConfig{
		Settings: settings,
		TipChange: func(err error) {
			blkFunc(name, err)
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
	return backend.(*ExchangeWallet), cm
}

type testRig struct {
	backends          map[string]*ExchangeWallet
	connectionMasters map[string]*dex.ConnectionMaster
}

func newTestRig(t *testing.T, blkFunc func(string, error)) *testRig {
	t.Helper()
	rig := &testRig{
		backends:          make(map[string]*ExchangeWallet),
		connectionMasters: make(map[string]*dex.ConnectionMaster, 3),
	}
	rig.backends["alpha"], rig.connectionMasters["alpha"] = tBackend(t, "alpha", blkFunc)
	rig.backends["beta"], rig.connectionMasters["beta"] = tBackend(t, "beta", blkFunc)
	return rig
}

func (rig *testRig) alpha() *ExchangeWallet {
	return rig.backends["alpha"]
}
func (rig *testRig) beta() *ExchangeWallet {
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
	chainParams = chaincfg.SimNetParams()
	tLogger = dex.StdOutLogger("TEST", dex.LevelTrace)
	var shutdown func()
	tCtx, shutdown = context.WithCancel(context.Background())
	doIt := func() int {
		defer shutdown()
		return m.Run()
	}
	os.Exit(doIt())
}

func TestWallet(t *testing.T) {
	tLogger.Infof("////////// WITHOUT SPLIT FUNDING TRANSACTIONS //////////")
	runTest(t, false)
	tLogger.Infof("////////// WITH SPLIT FUNDING TRANSACTIONS //////////")
	runTest(t, true)
}

func runTest(t *testing.T, splitTx bool) {
	blockReported := false
	rig := newTestRig(t, func(name string, err error) {
		blockReported = true
		tLogger.Infof("%s has reported a new block, error = %v", name, err)
	})
	defer rig.close(t)
	contractValue := toAtoms(2)
	lots := contractValue / tDCR.LotSize

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
		bal, err := wallet.Balance()
		if err != nil {
			t.Fatalf("error getting available: %v", err)
		}
		tLogger.Debugf("%s %f available, %f unconfirmed, %f locked",
			name, float64(bal.Available)/1e8, float64(bal.Immature)/1e8, float64(bal.Locked)/1e8)
		wallet.useSplitTx = splitTx
	}

	// Unlock the wallet for use.
	err := rig.beta().Unlock(walletPassword)
	if err != nil {
		t.Fatalf("error unlocking beta wallet: %v", err)
	}

	ord := &asset.Order{
		Value:        contractValue * 3,
		MaxSwapCount: lots * 3,
		DEXConfig:    tDCR,
	}
	setOrderValue := func(v uint64) {
		ord.Value = v
		ord.MaxSwapCount = v / tDCR.LotSize
	}

	// Grab some coins.
	utxos, _, err := rig.beta().FundOrder(ord)
	if err != nil {
		t.Fatalf("Funding error: %v", err)
	}
	utxo := utxos[0]

	// Coins should be locked
	utxos, _, _ = rig.beta().FundOrder(ord)
	if !splitTx && inUTXOs(utxo, utxos) {
		t.Fatalf("received locked output")
	}
	rig.beta().ReturnCoins([]asset.Coin{utxo})
	rig.beta().ReturnCoins(utxos)
	// Make sure we get the first utxo back with Fund.
	utxos, _, _ = rig.beta().FundOrder(ord)
	if !splitTx && !inUTXOs(utxo, utxos) {
		t.Fatalf("unlocked output not returned")
	}
	rig.beta().ReturnCoins(utxos)

	// Get a separate set of UTXOs for each contract.
	setOrderValue(contractValue)
	utxos1, _, err := rig.beta().FundOrder(ord)
	if err != nil {
		t.Fatalf("error funding first contract: %v", err)
	}
	// Get a separate set of UTXOs for each contract.
	setOrderValue(contractValue * 2)
	utxos2, _, err := rig.beta().FundOrder(ord)
	if err != nil {
		t.Fatalf("error funding second contract: %v", err)
	}

	secretKey1 := randBytes(32)
	keyHash1 := sha256.Sum256(secretKey1)
	secretKey2 := randBytes(32)
	keyHash2 := sha256.Sum256(secretKey2)
	lockTime := time.Now().Add(time.Hour * 8).UTC()
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
	swaps := &asset.Swaps{
		Inputs:    append(utxos1, utxos2...),
		Contracts: []*asset.Contract{contract1, contract2},
		FeeRate:   tDCR.MaxFeeRate,
	}

	receipts, _, _, err := rig.beta().Swap(swaps)
	if err != nil {
		t.Fatalf("error sending swap transaction: %v", err)
	}

	if len(receipts) != 2 {
		t.Fatalf("expected 1 receipt, got %d", len(receipts))
	}

	confCoin := receipts[0].Coin()
	checkConfs := func(n uint32, expSpent bool) {
		t.Helper()
		confs, spent, err := rig.beta().Confirmations(tCtx, confCoin.ID())
		if err != nil {
			t.Fatalf("error getting %d confs: %v", n, err)
		}
		if confs != n {
			t.Fatalf("expected %d confs, got %d", n, confs)
		}
		// Not using checkConfs until after redemption, so expect spent.
		if spent != expSpent {
			t.Fatalf("checkConfs: expected spent = %t, got %t", expSpent, spent)
		}
	}
	// Let alpha get and process that transaction.
	waitNetwork()
	// Check that there are 0 confirmations.
	checkConfs(0, false)

	// Unlock the wallet for use.
	err = rig.alpha().Unlock(walletPassword)
	if err != nil {
		t.Fatalf("error unlocking alpha wallet: %v", err)
	}

	makeRedemption := func(swapVal uint64, receipt asset.Receipt, secret []byte) *asset.Redemption {
		t.Helper()
		swapOutput := receipt.Coin()
		ci, err := rig.alpha().AuditContract(swapOutput.ID(), receipt.Contract())
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
		confs, spent, err := rig.alpha().Confirmations(context.TODO(), swapOutput.ID())
		if err != nil {
			t.Fatalf("error getting confirmations: %v", err)
		}
		if confs != 0 {
			t.Fatalf("unexpected number of confirmations. wanted 0, got %d", confs)
		}
		if ci.Expiration().Equal(lockTime) {
			t.Fatalf("wrong lock time. wanted %s, got %s", lockTime, ci.Expiration())
		}
		if spent {
			t.Fatalf("makeRedemption: expected unspent, got spent")
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

	_, _, _, err = rig.alpha().Redeem(redemptions)
	if err != nil {
		t.Fatalf("redemption error: %v", err)
	}

	// Find the redemption
	swapReceipt := receipts[0]
	waitNetwork()
	ctx, cancel := context.WithDeadline(tCtx, time.Now().Add(time.Second*5))
	defer cancel()
	_, checkKey, err := rig.beta().FindRedemption(ctx, swapReceipt.Coin().ID())
	if err != nil {
		t.Fatalf("error finding unconfirmed redemption: %v", err)
	}
	if !bytes.Equal(checkKey, secretKey1) {
		t.Fatalf("findRedemption (unconfirmed) key mismatch. %x != %x", checkKey, secretKey1)
	}

	// Mine a block and find the redemption again.
	mineAlpha()
	waitNetwork()
	// Check that the swap has one confirmation.
	checkConfs(1, true)
	if !blockReported {
		t.Fatalf("no block reported")
	}
	ctx, cancel2 := context.WithDeadline(tCtx, time.Now().Add(time.Second*5))
	defer cancel2()
	_, _, err = rig.beta().FindRedemption(ctx, swapReceipt.Coin().ID())
	if err != nil {
		t.Fatalf("error finding confirmed redemption: %v", err)
	}

	// Now send another one with lockTime = now and try to refund it.
	secretKey := randBytes(32)
	keyHash := sha256.Sum256(secretKey)
	lockTime = time.Now().Add(-8 * time.Hour)

	// Have beta send a swap contract to the alpha address.
	setOrderValue(contractValue)
	utxos, _, _ = rig.beta().FundOrder(ord)
	contract := &asset.Contract{
		Address:    alphaAddress,
		Value:      contractValue,
		SecretHash: keyHash[:],
		LockTime:   uint64(lockTime.Unix()),
	}
	swaps = &asset.Swaps{
		Inputs:    utxos,
		Contracts: []*asset.Contract{contract},
		FeeRate:   tDCR.MaxFeeRate,
	}

	receipts, _, _, err = rig.beta().Swap(swaps)
	if err != nil {
		t.Fatalf("error sending swap transaction: %v", err)
	}

	if len(receipts) != 1 {
		t.Fatalf("expected 1 receipt, got %d", len(receipts))
	}
	swapReceipt = receipts[0]

	waitNetwork()
	_, err = rig.beta().Refund(swapReceipt.Coin().ID(), swapReceipt.Contract())
	if err != nil {
		t.Fatalf("refund error: %v", err)
	}

	// Test PayFee
	coin, err := rig.beta().PayFee(alphaAddress, 1e8)
	if err != nil {
		t.Fatalf("error paying fees: %v", err)
	}
	tLogger.Infof("fee paid with tx %s", coin.String())

	// Test Withdraw
	coin, err = rig.beta().Withdraw(alphaAddress, 5e7)
	if err != nil {
		t.Fatalf("error withdrawing: %v", err)
	}
	tLogger.Infof("withdrew with tx %s", coin.String())

	// Lock the wallet
	err = rig.beta().Lock()
	if err != nil {
		t.Fatalf("error locking wallet: %v", err)
	}
}
