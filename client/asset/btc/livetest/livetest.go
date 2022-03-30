package livetest

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
	"errors"
	"fmt"
	"math/rand"
	"os/exec"
	"os/user"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/config"
)

type WalletConstructor func(cfg *asset.WalletConfig, logger dex.Logger, network dex.Network) (asset.Wallet, error)

func tBackend(ctx context.Context, t *testing.T, cfg *Config, node, name string,
	logger dex.Logger, blkFunc func(string, error)) (asset.Wallet, *dex.ConnectionMaster) {

	t.Helper()
	user, err := user.Current()
	if err != nil {
		t.Fatalf("error getting current user: %v", err)
	}
	cfgPath := filepath.Join(user.HomeDir, "dextest", cfg.Asset.Symbol, node, node+".conf")
	settings, err := config.Parse(cfgPath)
	if err != nil {
		t.Fatalf("error reading config options: %v", err)
	}
	settings["walletname"] = name
	if cfg.SplitTx {
		settings["txsplit"] = "1"
	}

	reportName := fmt.Sprintf("%s:%s-%s", cfg.Asset.Symbol, node, name)

	walletCfg := &asset.WalletConfig{
		Settings: settings,
		TipChange: func(err error) {
			blkFunc(reportName, err)
		},
		PeersChange: func(num uint32) {
			fmt.Println("peer count: ", num)
		},
	}

	w, err := cfg.NewWallet(walletCfg, logger, dex.Regtest)
	if err != nil {
		t.Fatalf("error creating backend: %v", err)
	}

	cm := dex.NewConnectionMaster(w)
	err = cm.Connect(ctx)
	if err != nil {
		t.Fatalf("error connecting backend: %v", err)
	}

	return w, cm
}

type testRig struct {
	t                 *testing.T
	symbol            string
	backends          map[string]asset.Wallet
	connectionMasters map[string]*dex.ConnectionMaster
}

func (rig *testRig) alpha() asset.Wallet {
	return rig.backends["alpha"]
}

// TODO: Test with beta since it's a different node.
// func (rig *testRig) beta() asset.Wallet {
// 	return rig.backends["beta"]
// }
func (rig *testRig) gamma() asset.Wallet {
	return rig.backends["gamma"]
}
func (rig *testRig) close() {
	for name, cm := range rig.connectionMasters {
		closed := make(chan struct{})
		go func() {
			cm.Disconnect()
			close(closed)
		}()
		select {
		case <-closed:
		case <-time.NewTimer(time.Second * 30).C:
			rig.t.Fatalf("failed to disconnect from %s", name)
		}
	}
}

func (rig *testRig) mineAlpha() error {
	return exec.Command("tmux", "send-keys", "-t", rig.symbol+"-harness:2", "./mine-alpha 1", "C-m").Run()
}

func randBytes(l int) []byte {
	b := make([]byte, l)
	rand.Read(b)
	return b
}

type Config struct {
	NewWallet WalletConstructor
	LotSize   uint64
	Asset     *dex.Asset
	SplitTx   bool
	SPV       bool
}

func Run(t *testing.T, cfg *Config) {
	tLogger := dex.StdOutLogger("TEST", dex.LevelDebug)
	tCtx, shutdown := context.WithCancel(context.Background())
	defer shutdown()

	tStart := time.Now()

	walletPassword := []byte("abc")

	var blockReported uint32
	blkFunc := func(name string, err error) {
		atomic.StoreUint32(&blockReported, 1)
		tLogger.Infof("%s has reported a new block, error = %v", name, err)
	}

	rig := &testRig{
		t:                 t,
		symbol:            cfg.Asset.Symbol,
		backends:          make(map[string]asset.Wallet),
		connectionMasters: make(map[string]*dex.ConnectionMaster, 3),
	}

	var expConfs uint32
	blockWait := time.Second
	if cfg.SPV {
		blockWait = time.Second * 6
	}
	mine := func() {
		if cfg.SPV { // broadcast with spv client takes a bit longer
			time.Sleep(blockWait)
		}
		rig.mineAlpha()
		expConfs++
		time.Sleep(blockWait)
	}

	t.Log("Setting up alpha/beta/gamma wallet backends...")
	rig.backends["alpha"], rig.connectionMasters["alpha"] = tBackend(tCtx, t, cfg, "alpha", "", tLogger.SubLogger("alpha"), blkFunc)
	// rig.backends["beta"], rig.connectionMasters["beta"] = tBackend(tCtx, t, cfg, "beta", "", tLogger.SubLogger("beta"), blkFunc)
	rig.backends["gamma"], rig.connectionMasters["gamma"] = tBackend(tCtx, t, cfg, "alpha", "gamma", tLogger.SubLogger("gamma"), blkFunc)
	defer rig.close()

	// Unlock the wallet for use.
	err := rig.alpha().Unlock(walletPassword)
	if err != nil {
		t.Fatalf("error unlocking gamma wallet: %v", err)
	}

	if cfg.SPV {
		// // The test expects beta and gamma to be unlocked.
		// if err := rig.beta().Unlock(walletPassword); err != nil {
		// 	t.Fatalf("beta Unlock error: %v", err)
		// }
		if err := rig.gamma().Unlock(walletPassword); err != nil {
			t.Fatalf("gamma Unlock error: %v", err)
		}
	}

	var lots uint64 = 2
	contractValue := lots * cfg.LotSize

	tLogger.Info("Wallets configured")

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
			t.Fatalf("error getting available balance: %v", err)
		}
		tLogger.Infof("%s %f available, %f immature, %f locked",
			name, float64(bal.Available)/1e8, float64(bal.Immature)/1e8, float64(bal.Locked)/1e8)
	}

	ord := &asset.Order{
		Value:        contractValue * 3,
		MaxSwapCount: lots * 3,
		DEXConfig:    cfg.Asset,
	}
	setOrderValue := func(v uint64) {
		ord.Value = v
		ord.MaxSwapCount = v / cfg.LotSize
	}

	tLogger.Info("Testing FundOrder")

	// Gamma should only have 10 BTC utxos, so calling fund for less should only
	// return 1 utxo.
	utxos, _, err := rig.gamma().FundOrder(ord)
	if err != nil {
		t.Fatalf("Funding error: %v", err)
	}
	utxo := utxos[0]

	// UTXOs should be locked
	utxos, _, _ = rig.gamma().FundOrder(ord)
	if inUTXOs(utxo, utxos) {
		t.Fatalf("received locked output")
	}
	rig.gamma().ReturnCoins([]asset.Coin{utxo})
	rig.gamma().ReturnCoins(utxos)
	// Make sure we get the first utxo back with Fund.
	utxos, _, _ = rig.gamma().FundOrder(ord)
	if !cfg.SplitTx && !inUTXOs(utxo, utxos) {
		t.Fatalf("unlocked output not returned")
	}
	rig.gamma().ReturnCoins(utxos)

	// Get a separate set of UTXOs for each contract.
	setOrderValue(contractValue)
	utxos1, _, err := rig.gamma().FundOrder(ord)
	if err != nil {
		t.Fatalf("error funding first contract: %v", err)
	}
	// Get a separate set of UTXOs for each contract.
	setOrderValue(contractValue * 2)
	utxos2, _, err := rig.gamma().FundOrder(ord)
	if err != nil {
		t.Fatalf("error funding second contract: %v", err)
	}

	// For some reason, SwapConfirmations fails with a split tx in SPV mode in
	// this test. It works fine in practice, so figuring this out is a TODO.
	if cfg.SplitTx && cfg.SPV {
		time.Sleep(blockWait)
		rig.mineAlpha()
		time.Sleep(blockWait)
	}

	address, err := rig.alpha().Address()
	if err != nil {
		t.Fatalf("error getting alpha address: %v", err)
	}

	secretKey1 := randBytes(32)
	keyHash1 := sha256.Sum256(secretKey1)
	secretKey2 := randBytes(32)
	keyHash2 := sha256.Sum256(secretKey2)
	lockTime := time.Now().Add(time.Hour * 8).UTC()
	// Have gamma send a swap contract to the alpha address.
	contract1 := &asset.Contract{
		Address:    address,
		Value:      contractValue,
		SecretHash: keyHash1[:],
		LockTime:   uint64(lockTime.Unix()),
	}
	contract2 := &asset.Contract{
		Address:    address,
		Value:      contractValue * 2,
		SecretHash: keyHash2[:],
		LockTime:   uint64(lockTime.Unix()),
	}
	swaps := &asset.Swaps{
		Inputs:    append(utxos1, utxos2...),
		Contracts: []*asset.Contract{contract1, contract2},
		FeeRate:   cfg.Asset.MaxFeeRate,
	}

	tLogger.Info("Testing Swap")

	receipts, _, _, err := rig.gamma().Swap(swaps)
	if err != nil {
		t.Fatalf("error sending swap transaction: %v", err)
	}
	if len(receipts) != 2 {
		t.Fatalf("expected 1 receipt, got %d", len(receipts))
	}

	tLogger.Infof("Sent %d swaps", len(receipts))
	for i, r := range receipts {
		tLogger.Infof("      Swap # %d: %s", i+1, r.Coin())
	}

	// Don't check zero confs for SPV. Core deals with the failures until the
	// tx is mined.

	if cfg.SPV {
		mine()
	}

	confCoin := receipts[0].Coin()
	confContract := receipts[0].Contract()
	checkConfs := func(n uint32, expSpent bool) {
		t.Helper()
		confs, spent, err := rig.gamma().SwapConfirmations(context.Background(), confCoin.ID(), confContract, tStart)
		if err != nil {
			if n > 0 || !errors.Is(err, asset.CoinNotFoundError) {
				t.Fatalf("error getting %d confs: %v", n, err)
			}
		}
		if confs != n {
			t.Fatalf("expected %d confs, got %d", n, confs)
		}
		if spent != expSpent {
			t.Fatalf("checkConfs: expected spent = %t, got %t", expSpent, spent)
		}
	}

	checkConfs(expConfs, false)

	makeRedemption := func(swapVal uint64, receipt asset.Receipt, secret []byte) *asset.Redemption {
		t.Helper()

		// Alpha should be able to redeem.
		ci, err := rig.alpha().AuditContract(receipt.Coin().ID(), receipt.Contract(), nil, false) // no TxData because server gets that for us in practice!
		if err != nil {
			t.Fatalf("error auditing contract: %v", err)
		}
		auditCoin := ci.Coin
		if ci.Recipient != address {
			t.Fatalf("wrong address. %s != %s", ci.Recipient, address)
		}
		if auditCoin.Value() != swapVal {
			t.Fatalf("wrong contract value. wanted %d, got %d", swapVal, auditCoin.Value())
		}
		confs, spent, err := rig.alpha().SwapConfirmations(context.TODO(), receipt.Coin().ID(), receipt.Contract(), tStart)
		if err != nil {
			t.Fatalf("error getting confirmations: %v", err)
		}
		if confs != expConfs {
			t.Fatalf("unexpected number of confirmations. wanted %d, got %d", expConfs, confs)
		}
		if spent {
			t.Fatalf("makeRedemption: expected unspent, got spent")
		}
		if ci.Expiration.Equal(lockTime) {
			t.Fatalf("wrong lock time. wanted %s, got %s", lockTime, ci.Expiration)
		}
		return &asset.Redemption{
			Spends: ci,
			Secret: secret,
		}
	}

	tLogger.Info("Testing AuditContract")

	redemptions := []*asset.Redemption{
		makeRedemption(contractValue, receipts[0], secretKey1),
		makeRedemption(contractValue*2, receipts[1], secretKey2),
	}

	tLogger.Info("Testing Redeem")

	_, _, _, err = rig.alpha().Redeem(&asset.RedeemForm{
		Redemptions: redemptions,
	})
	if err != nil {
		t.Fatalf("redemption error: %v", err)
	}

	// Find the redemption

	// Only do the mempool zero-conf redemption check when not spv.
	if cfg.SPV {
		mine()
	}

	swapReceipt := receipts[0]
	ctx, cancelFind := context.WithDeadline(tCtx, time.Now().Add(time.Second*30))
	defer cancelFind()

	tLogger.Info("Testing FindRedemption")

	_, _, err = rig.gamma().FindRedemption(ctx, swapReceipt.Coin().ID(), nil)
	if err != nil {
		t.Fatalf("error finding unconfirmed redemption: %v", err)
	}

	// Mine a block and find the redemption again.
	mine()
	if atomic.LoadUint32(&blockReported) == 0 {
		t.Fatalf("no block reported")
	}
	// Check that there is 1 confirmation on the swap
	checkConfs(expConfs, true)
	_, checkKey, err := rig.gamma().FindRedemption(ctx, swapReceipt.Coin().ID(), nil)
	if err != nil {
		t.Fatalf("error finding confirmed redemption: %v", err)
	}
	if !bytes.Equal(checkKey, secretKey1) {
		t.Fatalf("findRedemption (unconfirmed) key mismatch. %x != %x", checkKey, secretKey1)
	}

	// Now send another one with lockTime = now and try to refund it.
	secretKey := randBytes(32)
	keyHash := sha256.Sum256(secretKey)
	lockTime = time.Now().Add(-8 * time.Hour)

	// Have gamma send a swap contract to the alpha address.
	setOrderValue(contractValue)
	utxos, _, _ = rig.gamma().FundOrder(ord)
	contract := &asset.Contract{
		Address:    address,
		Value:      contractValue,
		SecretHash: keyHash[:],
		LockTime:   uint64(lockTime.Unix()),
	}
	swaps = &asset.Swaps{
		Inputs:    utxos,
		Contracts: []*asset.Contract{contract},
		FeeRate:   cfg.Asset.MaxFeeRate,
	}

	tLogger.Info("Testing Refund")

	receipts, _, _, err = rig.gamma().Swap(swaps)
	if err != nil {
		t.Fatalf("error sending swap transaction: %v", err)
	}

	if len(receipts) != 1 {
		t.Fatalf("expected 1 receipt, got %d", len(receipts))
	}
	swapReceipt = receipts[0]

	// SPV doesn't recognize ownership of the swap output, so we need to mine
	// the transaction in order to establish spent status. In theory, we could
	// just yolo and refund regardless of spent status.
	if cfg.SPV {
		mine()
	}

	coinID, err := rig.gamma().Refund(swapReceipt.Coin().ID(), swapReceipt.Contract(), 100)
	if err != nil {
		t.Fatalf("refund error: %v", err)
	}
	c, _ := asset.DecodeCoinID(cfg.Asset.ID, coinID)
	tLogger.Infof("Refunded with %s", c)

	// Test PayFee
	const defaultFee = 100
	coin, err := rig.gamma().PayFee(address, 1e8, defaultFee)
	if err != nil {
		t.Fatalf("error paying fees: %v", err)
	}
	tLogger.Infof("Fee paid with %s", coin.String())

	tLogger.Info("Testing Withdraw")

	// Test Withdraw
	coin, err = rig.gamma().Withdraw(address, 5e7, 100)
	if err != nil {
		t.Fatalf("error withdrawing: %v", err)
	}
	tLogger.Infof("Withdrew with %s", coin.String())

	if cfg.SPV {
		mine()
	}
}
