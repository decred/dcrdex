//go:build harness

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
	"encoding/hex"
	"math"
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
	"decred.org/dcrdex/dex/encode"
	dexdcr "decred.org/dcrdex/dex/networks/dcr"
	"github.com/davecgh/go-spew/spew"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrutil/v4"
	"github.com/decred/dcrd/wire"
)

const (
	alphaAddress = "SsWKp7wtdTZYabYFYSc9cnxhwFEjA5g4pFc"
	betaAddress  = "Ssge52jCzbixgFC736RSTrwAnvH3a4hcPRX"
	gammaSeed    = "1285a47d6a59f9c548b2a72c2c34a2de97967bede3844090102bbba76707fe9d"
	vspAddr      = "http://127.0.0.1:19591"
)

var (
	tLogger   dex.Logger
	tCtx      context.Context
	tLotSize  uint64 = 1e7
	tRateStep uint64 = 100
	tDCR             = &dex.Asset{
		ID:           42,
		Symbol:       "dcr",
		Version:      version,
		SwapSize:     dexdcr.InitTxSize,
		SwapSizeBase: dexdcr.InitTxSizeBase,
		MaxFeeRate:   10,
		SwapConf:     1,
	}
	walletPassword = []byte("abc")
)

func mineAlpha() error {
	return exec.Command("tmux", "send-keys", "-t", "dcr-harness:0", "./mine-alpha 1", "C-m").Run()
}

func tBackend(t *testing.T, name string, isInternal bool, blkFunc func(string, error)) (*ExchangeWallet, *dex.ConnectionMaster) {
	t.Helper()
	user, err := user.Current()
	if err != nil {
		t.Fatalf("error getting current user: %v", err)
	}
	settings := make(map[string]string)
	if !isInternal {
		cfgPath := filepath.Join(user.HomeDir, "dextest", "dcr", name, name+".conf")
		var err error
		settings, err = config.Parse(cfgPath)
		if err != nil {
			t.Fatalf("error reading config options: %v", err)
		}
	}
	walletCfg := &asset.WalletConfig{
		Settings: settings,
		TipChange: func(err error) {
			blkFunc(name, err)
		},
		PeersChange: func(num uint32, err error) {
			t.Logf("peer count = %d, err = %v", num, err)
		},
	}
	if isInternal {
		seed, err := hex.DecodeString(gammaSeed)
		if err != nil {
			t.Fatal(err)
		}
		dataDir := t.TempDir()
		createSPVWallet(walletPassword, seed, dataDir, 0, 0, chaincfg.SimNetParams())
		walletCfg.Type = walletTypeSPV
		walletCfg.DataDir = dataDir
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
	if isInternal {
		i := 0
		for {
			synced, _, err := backend.SyncStatus()
			if err != nil {
				t.Fatal(err)
			}
			if synced {
				break
			}
			if i == 5 {
				t.Fatal("spv wallet not synced after 5 seconds")
			}
			i++
			time.Sleep(time.Second)
		}
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
	rig.backends["alpha"], rig.connectionMasters["alpha"] = tBackend(t, "alpha", false, blkFunc)
	rig.backends["beta"], rig.connectionMasters["beta"] = tBackend(t, "beta", false, blkFunc)
	rig.backends["gamma"], rig.connectionMasters["gamma"] = tBackend(t, "gamma", true, blkFunc)
	return rig
}

// alpha is an external wallet connected to dcrd.
func (rig *testRig) alpha() *ExchangeWallet {
	return rig.backends["alpha"]
}

// beta is an external spv wallet.
func (rig *testRig) beta() *ExchangeWallet {
	return rig.backends["beta"]
}

// gamma is an internal spv wallet.
func (rig *testRig) gamma() *ExchangeWallet {
	return rig.backends["gamma"]
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

	wallet := rig.beta()

	// Unlock the wallet to sign the tx and get keys.
	err = wallet.Unlock(walletPassword)
	if err != nil {
		t.Fatalf("error unlocking beta wallet: %v", err)
	}

	lockTime := time.Now().Add(5 * time.Minute)
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
	bondMsgTx, err := msgTxFromBytes(bond.SignedTx)
	if err != nil {
		t.Fatalf("invalid bond tx: %v", err)
	}
	bondOutVersion := bondMsgTx.TxOut[0].Version

	pkh := dcrutil.Hash160(pubkey.SerializeCompressed())

	lockTimeUint, pkhPush, err := dexdcr.ExtractBondDetailsV0(bondOutVersion, bond.Data)
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

	refundCoin, err := wallet.RefundBond(context.Background(), bondVer, bond.CoinID,
		bond.Data, bond.Amount, priv)
	if err != nil {
		t.Fatalf("RefundBond: %v", err)
	}
	t.Logf("refundCoin: %v\n", refundCoin)
}

func TestWallet(t *testing.T) {
	tLogger.Infof("////////// WITHOUT SPLIT FUNDING TRANSACTIONS //////////")
	runTest(t, false)
	tLogger.Infof("////////// WITH SPLIT FUNDING TRANSACTIONS //////////")
	runTest(t, true)
}

func runTest(t *testing.T, splitTx bool) {
	tStart := time.Now()
	blockReported := false
	rig := newTestRig(t, func(name string, err error) {
		blockReported = true
		tLogger.Infof("%s has reported a new block, error = %v", name, err)
	})
	defer rig.close(t)
	contractValue := toAtoms(2)
	lots := contractValue / tLotSize

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
		wallet.config().useSplitTx = splitTx
	}

	// Unlock the wallet for use.
	err := rig.beta().Unlock(walletPassword)
	if err != nil {
		t.Fatalf("error unlocking beta wallet: %v", err)
	}

	ord := &asset.Order{
		Version:      tDCR.Version,
		Value:        contractValue * 3,
		MaxSwapCount: lots * 3,
		MaxFeeRate:   tDCR.MaxFeeRate,
	}
	setOrderValue := func(v uint64) {
		ord.Value = v
		ord.MaxSwapCount = v / tLotSize
	}

	// Grab some coins.
	utxos, _, _, err := rig.beta().FundOrder(ord)
	if err != nil {
		t.Fatalf("Funding error: %v", err)
	}
	utxo := utxos[0]

	// Coins should be locked
	utxos, _, _, _ = rig.beta().FundOrder(ord)
	if !splitTx && inUTXOs(utxo, utxos) {
		t.Fatalf("received locked output")
	}
	rig.beta().ReturnCoins([]asset.Coin{utxo})
	rig.beta().ReturnCoins(utxos)
	// Make sure we get the first utxo back with Fund.
	utxos, _, _, _ = rig.beta().FundOrder(ord)
	if !splitTx && !inUTXOs(utxo, utxos) {
		t.Fatalf("unlocked output not returned")
	}
	rig.beta().ReturnCoins(utxos)

	// Get a separate set of UTXOs for each contract.
	setOrderValue(contractValue)
	utxos1, _, _, err := rig.beta().FundOrder(ord)
	if err != nil {
		t.Fatalf("error funding first contract: %v", err)
	}
	// Get a separate set of UTXOs for each contract.
	setOrderValue(contractValue * 2)
	utxos2, _, _, err := rig.beta().FundOrder(ord)
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
	confContract := receipts[0].Contract()
	checkConfs := func(n uint32, expSpent bool) {
		t.Helper()
		confs, spent, err := rig.beta().SwapConfirmations(tCtx, confCoin.ID(), confContract, tStart)
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
		op := swapOutput.(*output)
		tx, err := rig.beta().wallet.GetTransaction(tCtx, op.txHash())
		if err != nil || tx == nil {
			t.Fatalf("GetTransaction: %v", err)
		}
		msgTx, err := msgTxFromHex(tx.Hex)
		if err != nil {
			t.Fatalf("msgTxFromHex: %v", err)
		}
		txData, err := msgTx.Bytes()
		if err != nil {
			t.Fatalf("msgTx.Bytes: %v", err)
		}
		ci, err := rig.alpha().AuditContract(swapOutput.ID(), receipt.Contract(), txData, false)
		if err != nil {
			t.Fatalf("error auditing contract: %v", err)
		}
		swapOutput = ci.Coin
		if ci.Recipient != alphaAddress {
			t.Fatalf("wrong address. %s != %s", ci.Recipient, alphaAddress)
		}
		if swapOutput.Value() != swapVal {
			t.Fatalf("wrong contract value. wanted %d, got %d", swapVal, swapOutput.Value())
		}
		confs, spent, err := rig.alpha().SwapConfirmations(context.TODO(), swapOutput.ID(), receipt.Contract(), tStart)
		if err != nil {
			t.Fatalf("error getting confirmations: %v", err)
		}
		if spent {
			t.Fatalf("swap spent")
		}
		if confs != 0 {
			t.Fatalf("unexpected number of confirmations. wanted 0, got %d", confs)
		}
		if ci.Expiration.Equal(lockTime) {
			t.Fatalf("wrong lock time. wanted %s, got %s", lockTime, ci.Expiration)
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

	_, _, _, err = rig.alpha().Redeem(&asset.RedeemForm{
		Redemptions: redemptions,
	})
	if err != nil {
		t.Fatalf("redemption error: %v", err)
	}

	betaSPV := rig.beta().wallet.SpvMode()

	// Find the redemption
	swapReceipt := receipts[0]
	// The mempool find redemption request does not work in SPV mode.
	if !betaSPV {
		waitNetwork()
		ctx, cancel := context.WithDeadline(tCtx, time.Now().Add(time.Second*5))
		defer cancel()
		_, checkKey, err := rig.beta().FindRedemption(ctx, swapReceipt.Coin().ID(), nil)
		if err != nil {
			t.Fatalf("error finding unconfirmed redemption: %v", err)
		}
		if !bytes.Equal(checkKey, secretKey1) {
			t.Fatalf("findRedemption (unconfirmed) key mismatch. %x != %x", checkKey, secretKey1)
		}
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
	_, _, err = rig.beta().FindRedemption(ctx, swapReceipt.Coin().ID(), nil)
	if err != nil {
		t.Fatalf("error finding confirmed redemption: %v", err)
	}

	// Now send another one with lockTime = now and try to refund it.
	secretKey := randBytes(32)
	keyHash := sha256.Sum256(secretKey)
	lockTime = time.Now().Add(-8 * time.Hour)

	// Have beta send a swap contract to the alpha address.
	setOrderValue(contractValue)
	utxos, _, _, _ = rig.beta().FundOrder(ord)
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
	_, err = rig.beta().Refund(swapReceipt.Coin().ID(), swapReceipt.Contract(), tDCR.MaxFeeRate/4)
	if err != nil {
		t.Fatalf("refund error: %v", err)
	}

	// Test Send
	coin, err := rig.beta().Send(alphaAddress, 1e8, defaultFee)
	if err != nil {
		t.Fatalf("error sending fees: %v", err)
	}
	tLogger.Infof("fee paid with tx %s", coin.String())

	// Test Withdraw
	coin, err = rig.beta().Withdraw(alphaAddress, 5e7, tDCR.MaxFeeRate/4)
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

func TestTickets(t *testing.T) {
	rig := newTestRig(t, func(name string, err error) {
		tLogger.Infof("%s has reported a new block, error = %v", name, err)
	})
	defer rig.close(t)
	tLogger.Info("Testing ticket methods with the alpha rig.")
	testTickets(t, false, rig.alpha())
	// TODO: beta wallet is spv but has no vsp in its config. Add it and
	// test here.
	tLogger.Info("Testing ticket methods with the gamma rig.")
	testTickets(t, true, rig.gamma())
}

func testTickets(t *testing.T, isInternal bool, ew *ExchangeWallet) {

	// Test StakeStatus.
	ss, err := ew.StakeStatus()
	if err != nil {
		t.Fatalf("unable to get stake status: %v", err)
	}
	tLogger.Info("The following are stake status before setting vsp or purchasing tickets.")
	spew.Dump(ss)

	// Test SetVSP.
	err = ew.SetVSP(vspAddr)
	if isInternal {
		if err != nil {
			t.Fatalf("unexpected error setting vsp for internal wallet: %v", err)
		}
	} else {
		if err == nil {
			t.Fatal("expected error setting vsp for external wallet")
		}
	}

	// Test PurchaseTickets.
	if err := ew.Unlock(walletPassword); err != nil {
		t.Fatalf("unable to unlock wallet: %v", err)
	}
	tickets, err := ew.PurchaseTickets(3, 20)
	if err != nil {
		t.Fatalf("error purchasing tickets: %v", err)
	}
	tLogger.Infof("Purchased the following tickets: %v", tickets)

	var currentDeployments []chaincfg.ConsensusDeployment
	var bestVer uint32
	for ver, deps := range ew.chainParams.Deployments {
		if bestVer == 0 || ver > bestVer {
			currentDeployments = deps
			bestVer = ver
		}
	}

	choices := make(map[string]string)
	for _, d := range currentDeployments {
		choices[d.Vote.Id] = d.Vote.Choices[rand.Int()%len(d.Vote.Choices)].Id
	}

	aPubKey := "034a43df1b95bf1b0dd77b53b8d880d4a5f47376fb036a5be5c1f3ba8d12ef65d7"
	bPubKey := "02028dd2bbabbcd262c3e2326ae27a61fcc935beb2bbc2f0f76f3f5025ba4a3c5d"

	treasuryPolicy := map[string]string{
		aPubKey: "yes",
		bPubKey: "no",
	}

	var tspendPolicy map[string]string
	if spvw, is := ew.wallet.(*spvWallet); is {
		if dcrw, is := spvw.dcrWallet.(*extendedWallet); is {
			txIn := &wire.TxIn{SignatureScript: encode.RandomBytes(66 + secp256k1.PubKeyBytesLenCompressed)}
			tspendA := &wire.MsgTx{Expiry: math.MaxUint32 - 1, TxIn: []*wire.TxIn{txIn}}
			tspendB := &wire.MsgTx{Expiry: math.MaxUint32 - 2, TxIn: []*wire.TxIn{txIn}}
			txHashA, txHashB := tspendA.TxHash(), tspendB.TxHash()
			tspendPolicy = map[string]string{
				txHashA.String(): "yes",
				txHashB.String(): "no",
			}
			for _, tx := range []*wire.MsgTx{tspendA, tspendB} {
				if err := dcrw.AddTSpend(*tx); err != nil {
					t.Fatalf("Error adding tspend: %v", err)
				}
			}
		}
	}

	// Test SetVotingPreferences.
	if err := ew.SetVotingPreferences(choices, tspendPolicy, treasuryPolicy); err != nil {
		t.Fatalf("Error setting voting preferences: %v", err)
	}

	// Test StakeStatus again.
	ss, err = ew.StakeStatus()
	if err != nil {
		t.Fatalf("Unable to get stake status: %v", err)
	}
	tLogger.Info("The following are stake status after setting vsp and purchasing tickets.")
	spew.Dump(ss)

	if len(ss.Stances.Agendas) != len(choices) {
		t.Fatalf("wrong number of vote choices. expected %d, got %d", len(choices), len(ss.Stances.Agendas))
	}

	for _, agenda := range ss.Stances.Agendas {
		choiceID, found := choices[agenda.ID]
		if !found {
			t.Fatalf("unknown agenda %s", agenda.ID)
		}
		if agenda.CurrentChoice != choiceID {
			t.Fatalf("wrong choice reported. expected %s, got %s", choiceID, agenda.CurrentChoice)
		}
	}

	if len(ss.Stances.TreasuryKeys) != len(treasuryPolicy) {
		t.Fatalf("wrong number of treasury keys. expected %d, got %d", len(treasuryPolicy), len(ss.Stances.TreasuryKeys))
	}

	for _, tp := range ss.Stances.TreasuryKeys {
		policy, found := treasuryPolicy[tp.Key]
		if !found {
			t.Fatalf("unknown treasury key %s", tp.Key)
		}
		if tp.Policy != policy {
			t.Fatalf("wrong policy reported. expected %s, got %s", policy, tp.Policy)
		}
	}

	if len(ss.Stances.TreasurySpends) != len(tspendPolicy) {
		t.Fatalf("wrong number of tspends. expected %d, got %d", len(tspendPolicy), len(ss.Stances.TreasurySpends))
	}

	for _, tspend := range ss.Stances.TreasurySpends {
		policy, found := tspendPolicy[tspend.Hash]
		if !found {
			t.Fatalf("unknown tspend tx %s", tspend.Hash)
		}
		if tspend.CurrentPolicy != policy {
			t.Fatalf("wrong policy reported. expected %s, got %s", policy, tspend.CurrentPolicy)
		}
	}
}
