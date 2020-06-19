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
	"math"
	"math/rand"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/asset/btc"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/config"
	"github.com/decred/slog"
)

type testKiller interface {
	Fatalf(string, ...interface{})
}

type WalletConstructor func(cfg *asset.WalletConfig, logger dex.Logger, network dex.Network) (asset.Wallet, error)

// Convert the BTC value to satoshi.
func toSatoshi(v float64) uint64 {
	return uint64(math.Round(v * 1e8))
}

func tBackend(t testKiller, ctx context.Context, newWallet WalletConstructor, symbol, conf, name string,
	logger dex.Logger, blkFunc func(string, error)) (*btc.ExchangeWallet, *dex.ConnectionMaster) {
	user, err := user.Current()
	if err != nil {
		t.Fatalf("error getting current user: %v", err)
	}
	cfgPath := filepath.Join(user.HomeDir, "dextest", symbol, "harness-ctl", conf+".conf")
	settings, err := config.Parse(cfgPath)
	if err != nil {
		t.Fatalf("error reading config options: %v", err)
	}
	walletCfg := &asset.WalletConfig{
		Settings: settings,
		Account:  name,
		TipChange: func(err error) {
			blkFunc(conf, err)
		},
	}
	var backend asset.Wallet
	backend, err = newWallet(walletCfg, logger, dex.Regtest)
	if err != nil {
		t.Fatalf("error creating backend: %v", err)
	}
	cm := dex.NewConnectionMaster(backend)
	err = cm.Connect(ctx)
	if err != nil {
		t.Fatalf("error connecting backend: %v", err)
	}
	return backend.(*btc.ExchangeWallet), cm
}

type testRig struct {
	t                 testKiller
	symbol            string
	backends          map[string]*btc.ExchangeWallet
	connectionMasters map[string]*dex.ConnectionMaster
}

func (rig *testRig) alpha() *btc.ExchangeWallet {
	return rig.backends["alpha"]
}
func (rig *testRig) beta() *btc.ExchangeWallet {
	return rig.backends["beta"]
}
func (rig *testRig) gamma() *btc.ExchangeWallet {
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
		case <-time.NewTimer(time.Second).C:
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

func Run(t testKiller, newWallet WalletConstructor, address string, dexAsset *dex.Asset) {
	tLogger := slog.NewBackend(os.Stdout).Logger("TEST")
	tLogger.SetLevel(slog.LevelTrace)
	tCtx, shutdown := context.WithCancel(context.Background())
	defer shutdown()

	tBlockTick := time.Second
	tBlockWait := tBlockTick + time.Millisecond*50
	walletPassword := "abc"

	blockReported := false
	blkFunc := func(name string, err error) {
		blockReported = true
		tLogger.Infof("%s has reported a new block, error = %v", name, err)
	}

	rig := &testRig{
		t:                 t,
		symbol:            dexAsset.Symbol,
		backends:          make(map[string]*btc.ExchangeWallet),
		connectionMasters: make(map[string]*dex.ConnectionMaster, 3),
	}
	rig.backends["alpha"], rig.connectionMasters["alpha"] = tBackend(t, tCtx, newWallet, dexAsset.Symbol, "alpha", "", tLogger, blkFunc)
	rig.backends["beta"], rig.connectionMasters["beta"] = tBackend(t, tCtx, newWallet, dexAsset.Symbol, "beta", "", tLogger, blkFunc)
	rig.backends["gamma"], rig.connectionMasters["gamma"] = tBackend(t, tCtx, newWallet, dexAsset.Symbol, "alpha", "gamma", tLogger, blkFunc)
	defer rig.close()
	contractValue := 2 * dexAsset.LotSize

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
		tLogger.Debugf("%s %f available, %f immature, %f locked",
			name, float64(bal.Available)/1e8, float64(bal.Immature)/1e8, float64(bal.Locked)/1e8)
	}
	// Gamma should only have 10 BTC utxos, so calling fund for less should only
	// return 1 utxo.
	utxos, err := rig.gamma().Fund(contractValue*3, dexAsset)
	if err != nil {
		t.Fatalf("Funding error: %v", err)
	}
	utxo := utxos[0]

	// UTXOs should be locked
	utxos, _ = rig.gamma().Fund(contractValue*3, dexAsset)
	if inUTXOs(utxo, utxos) {
		t.Fatalf("received locked output")
	}
	// Unlock
	rig.gamma().ReturnCoins([]asset.Coin{utxo})
	rig.gamma().ReturnCoins(utxos)
	// Make sure we get the first utxo back with Fund.
	utxos, _ = rig.gamma().Fund(contractValue*3, dexAsset)
	if !inUTXOs(utxo, utxos) {
		t.Fatalf("unlocked output not returned")
	}
	rig.gamma().ReturnCoins(utxos)

	// Get a separate set of UTXOs for each contract.
	utxos1, err := rig.gamma().Fund(contractValue, dexAsset)
	if err != nil {
		t.Fatalf("error funding first contract: %v", err)
	}
	// Get a separate set of UTXOs for each contract.
	utxos2, err := rig.gamma().Fund(contractValue*2, dexAsset)
	if err != nil {
		t.Fatalf("error funding second contract: %v", err)
	}

	// Unlock the wallet for use.
	err = rig.gamma().Unlock(walletPassword, time.Hour*24)
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
	}

	receipts, _, err := rig.gamma().Swap(swaps, dexAsset)
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
		if ci.Recipient() != address {
			t.Fatalf("wrong address. %s != %s", ci.Recipient(), address)
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

	_, _, err = rig.alpha().Redeem(redemptions, dexAsset)
	if err != nil {
		t.Fatalf("redemption error: %v", err)
	}

	// Find the redemption
	swapCoin := receipts[0].Coin()
	ctx, cancel := context.WithDeadline(tCtx, time.Now().Add(time.Second*5))
	defer cancel()
	checkKey, err := rig.gamma().FindRedemption(ctx, swapCoin.ID())
	if err != nil {
		t.Fatalf("error finding unconfirmed redemption: %v", err)
	}
	if !bytes.Equal(checkKey, secretKey1) {
		t.Fatalf("findRedemption (unconfirmed) key mismatch. %x != %x", checkKey, secretKey1)
	}

	// Mine a block and find the redemption again.
	rig.mineAlpha()
	time.Sleep(tBlockWait)
	if !blockReported {
		t.Fatalf("no block reported")
	}
	// Check that there is 1 confirmation on the swap
	checkConfs(1)
	_, err = rig.gamma().FindRedemption(ctx, swapCoin.ID())
	if err != nil {
		t.Fatalf("error finding confirmed redemption: %v", err)
	}

	// Confirmations should now be an error, since the swap output has been spent.
	_, err = swapCoin.Confirmations()
	if err == nil {
		t.Fatalf("error getting confirmations. has swap output been spent?")
	}

	// Now send another one with lockTime = now and try to refund it.
	secretKey := randBytes(32)
	keyHash := sha256.Sum256(secretKey)
	lockTime = time.Now().Add(-24 * time.Hour)

	// Have gamma send a swap contract to the alpha address.
	utxos, _ = rig.gamma().Fund(contractValue, dexAsset)
	contract := &asset.Contract{
		Address:    address,
		Value:      contractValue,
		SecretHash: keyHash[:],
		LockTime:   uint64(lockTime.Unix()),
	}
	swaps = &asset.Swaps{
		Inputs:    utxos,
		Contracts: []*asset.Contract{contract},
	}

	time.Sleep(time.Second)

	receipts, _, err = rig.gamma().Swap(swaps, dexAsset)
	if err != nil {
		t.Fatalf("error sending swap transaction: %v", err)
	}

	if len(receipts) != 1 {
		t.Fatalf("expected 1 receipt, got %d", len(receipts))
	}
	swapCoin = receipts[0].Coin()

	_, err = rig.gamma().Refund(swapCoin.ID(), swapCoin.Redeem(), dexAsset)
	if err != nil {
		t.Fatalf("refund error: %v", err)
	}

	// Test PayFee
	coin, err := rig.gamma().PayFee(address, 1e8, dexAsset)
	if err != nil {
		t.Fatalf("error paying fees: %v", err)
	}
	tLogger.Infof("fee paid with tx %s", coin.String())

	// Test Withdraw
	coin, err = rig.gamma().Withdraw(address, 5e7, 10)
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
