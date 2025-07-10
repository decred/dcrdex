//go:build harness

package btc

// Simnet tests expect the BTC test harness to be running.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
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
	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

const (
	gammaSeed = "1285a47d6a59f9c548b2a72c2c34a2de97967bede3844090102bbba76707fe9d"
)

var (
	tLogger   dex.Logger
	tCtx      context.Context
	tLotSize  uint64 = 1e7
	tRateStep uint64 = 100
	tBTC             = &dex.Asset{
		ID:         0,
		Symbol:     "btc",
		Version:    version,
		MaxFeeRate: 10,
		SwapConf:   1,
	}
	walletPassword = []byte("abc")
)

func mineAlpha() error {
	return exec.Command("tmux", "send-keys", "-t", "btc-harness:0", "./mine-alpha 1", "C-m").Run()
}

func mineBeta() error {
	return exec.Command("tmux", "send-keys", "-t", "btc-harness:0", "./mine-beta 1", "C-m").Run()
}

func tBackend(t *testing.T, name string, isInternal bool, blkFunc func(string, error)) (*ExchangeWalletFullNode, *dex.ConnectionMaster) {
	t.Helper()
	user, err := user.Current()
	if err != nil {
		t.Fatalf("error getting current user: %v", err)
	}
	settings := make(map[string]string)
	if !isInternal {
		cfgPath := filepath.Join(user.HomeDir, "dextest", "btc", name, name+".conf")
		settings, err = config.Parse(cfgPath)
		if err != nil {
			t.Fatalf("error reading config options: %v", err)
		}
	}

	noteChan := make(chan asset.WalletNotification, 128)
	go func() {
		for {
			select {
			case <-noteChan:
			case <-tCtx.Done():
				return
			}
		}
	}()

	walletCfg := &asset.WalletConfig{
		Settings: settings,
		Emit:     asset.NewWalletEmitter(make(chan asset.WalletNotification, 128), 0, tLogger),
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
		regtestDir := filepath.Join(dataDir, chaincfg.RegressionNetParams.Name)
		err = createSPVWallet(walletPassword, seed, DefaultWalletBirthday, regtestDir, tLogger, 0, 0, &chaincfg.RegressionNetParams)
		if err != nil {
			t.Fatal(err)
		}
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
			syncStatus, err := backend.SyncStatus()
			if err != nil {
				t.Fatal(err)
			}
			if syncStatus.Synced {
				break
			}
			if i == 5 {
				t.Fatal("spv wallet not synced after 5 seconds")
			}
			i++
			time.Sleep(time.Second)
		}

		spv := backend.(*ExchangeWalletSPV)
		fullNode := &ExchangeWalletFullNode{
			intermediaryWallet: spv.intermediaryWallet,
			authAddOn:          spv.authAddOn,
		}

		return fullNode, cm
	}

	accelerator := backend.(*ExchangeWalletAccelerator)
	return accelerator.ExchangeWalletFullNode, cm
}

type testRig struct {
	backends          map[string]*ExchangeWalletFullNode
	connectionMasters map[string]*dex.ConnectionMaster
}

func newTestRig(t *testing.T, blkFunc func(string, error)) *testRig {
	t.Helper()
	rig := &testRig{
		backends:          make(map[string]*ExchangeWalletFullNode),
		connectionMasters: make(map[string]*dex.ConnectionMaster, 3),
	}
	rig.backends["alpha"], rig.connectionMasters["alpha"] = tBackend(t, "alpha", false, blkFunc)
	rig.backends["beta"], rig.connectionMasters["beta"] = tBackend(t, "beta", false, blkFunc)
	rig.backends["gamma"], rig.connectionMasters["gamma"] = tBackend(t, "gamma", true, blkFunc)

	gammaAddr, err := rig.backends["gamma"].DepositAddress()
	if err != nil {
		t.Fatalf("error getting gamma deposit address: %v", err)
	}

	_, err = rig.alpha().Send(gammaAddr, toSatoshi(100), 10)
	if err != nil {
		t.Fatalf("error sending to gamma: %v", err)
	}

	mineAlpha()

	return rig
}

func (rig *testRig) alpha() *ExchangeWalletFullNode {
	return rig.backends["alpha"]
}
func (rig *testRig) beta() *ExchangeWalletFullNode {
	return rig.backends["beta"]
}
func (rig *testRig) gamma() *ExchangeWalletFullNode {
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
		case <-time.NewTimer(60 * time.Second).C:
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

func TestExternalFeeRate(t *testing.T) {
	fetchRateWithTimeout(t, dex.Mainnet)
	fetchRateWithTimeout(t, dex.Testnet)
}

func fetchRateWithTimeout(t *testing.T, net dex.Network) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()
	feeRate, err := externalFeeRate(ctx, net)
	if err != nil {
		t.Fatalf("error fetching %s fees: %v", net, err)
	}
	fmt.Printf("##### Fee rate fetched for %s! %d Sats/vB \n", net, feeRate)
}

func TestWalletTxBalanceSync(t *testing.T) {
	rig := newTestRig(t, func(name string, _ error) {
		tLogger.Infof("%s has reported a new block", name)
	})
	defer rig.close(t)

	beta := rig.beta()
	gamma := rig.gamma()

	err := beta.Unlock(walletPassword)
	if err != nil {
		t.Fatalf("error unlocking beta wallet: %v", err)
	}
	err = gamma.Unlock(walletPassword)
	if err != nil {
		t.Fatalf("error unlocking gamma wallet: %v", err)
	}

	t.Run("rpc", func(t *testing.T) {
		testWalletTxBalanceSync(t, gamma, beta)
	})

	t.Run("spv", func(t *testing.T) {
		testWalletTxBalanceSync(t, beta, gamma)
	})
}

// This tests that redemptions becoming available in the balance and the
// asset.WalletTransaction returned from WalletTransaction becomes confirmed
// at the same time.
func testWalletTxBalanceSync(t *testing.T, fromWallet, toWallet *ExchangeWalletFullNode) {
	receivingAddr, err := toWallet.DepositAddress()
	if err != nil {
		t.Fatalf("error getting deposit address: %v", err)
	}

	order := &asset.Order{
		AssetVersion:  version,
		FeeSuggestion: 10,
		MaxSwapCount:  1,
		MaxFeeRate:    20,
	}
	coins, _, _, err := fromWallet.FundOrder(order)
	if err != nil {
		t.Fatalf("error funding order: %v", err)
	}

	secret := randBytes(32)
	secretHash := sha256.Sum256(secret)
	contract := &asset.Contract{
		Address:    receivingAddr,
		Value:      order.Value,
		SecretHash: secretHash[:],
		LockTime:   uint64(time.Now().Add(-1 * time.Hour).Unix()),
	}
	swaps := &asset.Swaps{
		Inputs:  coins,
		FeeRate: 10,
		Contracts: []*asset.Contract{
			contract,
		},
	}
	receipts, _, _, err := fromWallet.Swap(swaps)
	if err != nil {
		t.Fatalf("error swapping: %v", err)
	}
	receipt := receipts[0]

	var auditInfo *asset.AuditInfo
	for i := 0; i < 10; i++ {
		auditInfo, err = toWallet.AuditContract(receipt.Coin().ID(), receipt.Contract(), []byte{}, false)
		if err == nil {
			break
		}

		time.Sleep(5 * time.Second)
	}
	if err != nil {
		t.Fatalf("error auditing contract: %v", err)
	}

	balance, err := toWallet.Balance()
	if err != nil {
		t.Fatalf("error getting balance: %v", err)
	}
	_, out, _, err := toWallet.Redeem(&asset.RedeemForm{
		Redemptions: []*asset.Redemption{
			{
				Spends: auditInfo,
				Secret: secret,
			},
		},
		FeeSuggestion: 10,
	})
	if err != nil {
		t.Fatalf("error redeeming: %v", err)
	}

	confirmSync := func(originalBalance uint64, coinID []byte) {
		t.Helper()

		for i := 0; i < 10; i++ {
			balance, err := toWallet.Balance()
			if err != nil {
				t.Fatalf("error getting balance: %v", err)
			}
			balDiff := balance.Available - originalBalance

			var confirmed bool
			var txDiff uint64
			if wt, err := toWallet.WalletTransaction(context.Background(), hex.EncodeToString(coinID)); err == nil {
				confirmed = wt.Confirmed
				txDiff = wt.Amount - wt.Fees
			} else if !errors.Is(err, asset.CoinNotFoundError) {
				t.Fatal(err)
			}

			balanceChanged := balance.Available != originalBalance
			if confirmed != balanceChanged {
				if balanceChanged && !confirmed {
					for j := 0; j < 20; j++ {
						if wt, err := toWallet.WalletTransaction(context.Background(), hex.EncodeToString(coinID)); err == nil && wt.Confirmed {
							t.Fatalf("took %d seconds after balance changed before tx was confirmed", j/2)
						} else if !errors.Is(err, asset.CoinNotFoundError) {
							t.Fatal(err)
						}
						time.Sleep(500 * time.Millisecond)
					}
				}
				t.Fatalf("confirmed status does not match balance change. confirmed = %v, balance changed = %d", confirmed, balDiff)
			}

			if confirmed {
				if balDiff != txDiff {
					t.Fatalf("balance and transaction diffs do not match. balance diff = %d, tx diff = %d", balDiff, txDiff)
				}
				return
			}

			time.Sleep(5 * time.Second)
		}

		t.Fatal("timed out waiting for balance and transaction to sync")
	}

	confirmSync(balance.Available, out.ID())

	balance, err = toWallet.Balance()
	if err != nil {
		t.Fatalf("error getting balance: %v", err)
	}

	receivingAddr, err = toWallet.DepositAddress()
	if err != nil {
		t.Fatalf("error getting deposit address: %v", err)
	}

	coin, err := fromWallet.Send(receivingAddr, toSatoshi(1), 10)
	if err != nil {
		t.Fatalf("error sending: %v", err)
	}

	confirmSync(balance.Available, coin.ID())
}

type privateSwapper struct {
	isMaker bool

	order *asset.Order

	ourSwapTx       []byte
	ourSwapTxCoinID []byte
	cpSwapTx        []byte
	cpSwapTxCoinID  []byte

	ourUnsignedRedeem []byte
	cpUnsignedRedeem  []byte

	ourRedeemPubKey []byte
	ourRefundPubKey []byte

	cpRedeemPubKey []byte
	cpRefundPubKey []byte

	cpRedeemAdaptorSig []byte
	cpRefundAdaptorSig []byte

	ourRedeemAdaptorSig []byte
	ourRefundAdaptorSig []byte

	adaptorSecret *btcec.ModNScalar
	adaptorPub    *btcec.JacobianPoint

	lockTime time.Time

	wallet *ExchangeWalletFullNode
}

type takerPreSwapMsg struct {
	redeemPubKey []byte
	refundPubKey []byte
}

func (s *privateSwapper) generateTakerPreSwapMsg() (*takerPreSwapMsg, error) {
	if s.isMaker {
		return nil, fmt.Errorf("not taker")
	}

	var err error
	s.ourRedeemPubKey, err = s.wallet.PrivateSwapPubKey()
	if err != nil {
		return nil, err
	}
	s.ourRefundPubKey, err = s.wallet.PrivateSwapPubKey()
	if err != nil {
		return nil, err
	}

	return &takerPreSwapMsg{
		redeemPubKey: s.ourRedeemPubKey,
		refundPubKey: s.ourRefundPubKey,
	}, nil
}

type mkrInitSwapMsg struct {
	swapTx       []byte
	swapTxCoinID []byte
	redeemPubKey []byte
	refundPubKey []byte
}

func (s *privateSwapper) handleTakerPreSwapMsg(msg *takerPreSwapMsg) (*mkrInitSwapMsg, error) {
	if !s.isMaker {
		return nil, fmt.Errorf("not maker")
	}

	s.cpRedeemPubKey = msg.redeemPubKey
	s.cpRefundPubKey = msg.refundPubKey

	var err error
	s.ourRefundPubKey, err = s.wallet.PrivateSwapPubKey()
	if err != nil {
		return nil, err
	}
	s.ourRedeemPubKey, err = s.wallet.PrivateSwapPubKey()
	if err != nil {
		return nil, err
	}

	coins, _, _, err := s.wallet.FundOrder(s.order)
	if err != nil {
		return nil, err
	}
	contract := &asset.PrivateContract{
		LockTime:        uint64(s.lockTime.Unix()),
		Value:           s.order.Value,
		RedeemPublicKey: s.cpRedeemPubKey,
		RefundPublicKey: s.ourRefundPubKey,
	}
	swaps := &asset.PrivateSwaps{
		Version:    0,
		Contracts:  []*asset.PrivateContract{contract},
		FeeRate:    s.order.MaxFeeRate,
		Inputs:     coins,
		LockChange: false,
	}
	receipts, _, swapTx, _, err := s.wallet.SwapPrivate(swaps)
	if err != nil {
		return nil, err
	}
	s.ourSwapTx = swapTx
	ourSwapTxCoinID := receipts[0].Coin().ID()

	return &mkrInitSwapMsg{
		swapTx:       swapTx,
		swapTxCoinID: ourSwapTxCoinID,
		redeemPubKey: s.ourRedeemPubKey,
		refundPubKey: s.ourRefundPubKey,
	}, nil
}

type takerInitSwapMsg struct {
	swapTx         []byte
	swapCoinID     []byte
	unsignedRedeem []byte
}

func (s *privateSwapper) handleMkrInitSwapMsg(msg *mkrInitSwapMsg) (*takerInitSwapMsg, error) {
	if s.isMaker {
		return nil, fmt.Errorf("not taker")
	}

	s.cpSwapTx = msg.swapTx
	s.cpSwapTxCoinID = msg.swapTxCoinID
	s.cpRedeemPubKey = msg.redeemPubKey
	s.cpRefundPubKey = msg.refundPubKey

	cpContract := &asset.PrivateContract{
		LockTime:        uint64(s.lockTime.Unix()),
		Value:           s.order.Value,
		RedeemPublicKey: s.ourRedeemPubKey,
		RefundPublicKey: s.cpRefundPubKey,
	}
	err := s.wallet.AuditPrivateContract(s.cpSwapTxCoinID, s.cpSwapTx, cpContract, false)
	if err != nil {
		return nil, fmt.Errorf("error auditing contract: %w", err)
	}

	unsignedRedeem, err := s.wallet.GenerateUnsignedRedeemTx(s.cpSwapTxCoinID, cpContract, s.order.MaxFeeRate)
	if err != nil {
		return nil, fmt.Errorf("error generating unsigned redeem: %w", err)
	}
	s.ourUnsignedRedeem = unsignedRedeem

	coins, _, _, err := s.wallet.FundOrder(s.order)
	if err != nil {
		return nil, fmt.Errorf("error funding order: %w", err)
	}
	contract := &asset.PrivateContract{
		LockTime:        uint64(s.lockTime.Unix()),
		Value:           s.order.Value,
		RedeemPublicKey: s.cpRedeemPubKey,
		RefundPublicKey: s.ourRefundPubKey,
	}
	swaps := &asset.PrivateSwaps{
		Version:    0,
		Contracts:  []*asset.PrivateContract{contract},
		FeeRate:    s.order.MaxFeeRate,
		Inputs:     coins,
		LockChange: false,
	}
	receipts, _, swapTx, _, err := s.wallet.SwapPrivate(swaps)
	if err != nil {
		return nil, fmt.Errorf("error swapping: %w", err)
	}
	s.ourSwapTxCoinID = receipts[0].Coin().ID()
	s.ourSwapTx = swapTx

	return &takerInitSwapMsg{
		swapTx:         swapTx,
		swapCoinID:     s.ourSwapTxCoinID,
		unsignedRedeem: s.ourUnsignedRedeem,
	}, nil
}

type makerAdaptorsMsg struct {
	adaptorSigRefund []byte
	adaptorSigRedeem []byte
	adaptorPub       *btcec.JacobianPoint
	unsignedRedeem   []byte
}

func (s *privateSwapper) handleTakerInitSwapMsg(msg *takerInitSwapMsg) (*makerAdaptorsMsg, error) {
	if !s.isMaker {
		return nil, fmt.Errorf("not maker")
	}

	s.cpSwapTx = msg.swapTx
	s.cpSwapTxCoinID = msg.swapCoinID
	s.cpUnsignedRedeem = msg.unsignedRedeem

	cpContract := &asset.PrivateContract{
		LockTime:        uint64(s.lockTime.Unix()),
		Value:           s.order.Value,
		RedeemPublicKey: s.ourRedeemPubKey,
		RefundPublicKey: s.cpRefundPubKey,
	}

	err := s.wallet.AuditPrivateContract(s.cpSwapTxCoinID, s.cpSwapTx, cpContract, false)
	if err != nil {
		return nil, fmt.Errorf("error auditing contract: %w", err)
	}
	unsignedRedeem, err := s.wallet.GenerateUnsignedRedeemTx(s.cpSwapTxCoinID, cpContract, s.order.MaxFeeRate)
	if err != nil {
		return nil, fmt.Errorf("error generating unsigned redeem: %w", err)
	}
	s.ourUnsignedRedeem = unsignedRedeem

	ourSwapContract := &asset.PrivateContract{
		LockTime:        uint64(s.lockTime.Unix()),
		Value:           s.order.Value,
		RedeemPublicKey: s.cpRedeemPubKey,
		RefundPublicKey: s.ourRefundPubKey,
	}

	var adaptorSecret btcec.ModNScalar
	for i := 0; i < 100; i++ {
		var secretB [32]byte
		_, err := rand.Read(secretB[:])
		if err != nil {
			return nil, fmt.Errorf("error generating random secret: %w", err)
		}

		adaptorSecret.SetBytes(&secretB)

		if ok, err := s.wallet.ValidateAdaptorSecret(&adaptorSecret, unsignedRedeem, cpContract); err != nil {
			return nil, fmt.Errorf("error validating adaptor secret: %w", err)
		} else if !ok {
			continue
		}

		if ok, err := s.wallet.ValidateAdaptorSecret(&adaptorSecret, s.cpUnsignedRedeem, ourSwapContract); err != nil {
			return nil, fmt.Errorf("error validating adaptor secret: %w", err)
		} else if !ok {
			continue
		}

		fmt.Println("Found valid adaptor secret after", i, "attempts")
		break
	}

	redeemAdaptorSig, err := s.wallet.GeneratePrivateKeyTweakedAdaptor(unsignedRedeem, cpContract, &adaptorSecret, true)
	if err != nil {
		return nil, fmt.Errorf("error generating redeem adaptor: %w", err)
	}

	refundAdaptorSig, err := s.wallet.GeneratePrivateKeyTweakedAdaptor(s.cpUnsignedRedeem, ourSwapContract, &adaptorSecret, false)
	if err != nil {
		return nil, fmt.Errorf("error generating refund adaptor: %w", err)
	}

	s.adaptorSecret = &adaptorSecret
	var adaptorPub btcec.JacobianPoint
	btcec.ScalarBaseMultNonConst(&adaptorSecret, &adaptorPub)

	return &makerAdaptorsMsg{
		adaptorPub:       &adaptorPub,
		adaptorSigRedeem: redeemAdaptorSig,
		adaptorSigRefund: refundAdaptorSig,
		unsignedRedeem:   unsignedRedeem,
	}, nil
}

type takerAdaptorMsg struct {
	adaptorSig []byte
}

func (s *privateSwapper) handleMakerAdaptorsMsg(msg *makerAdaptorsMsg) (*takerAdaptorMsg, error) {
	if s.isMaker {
		return nil, fmt.Errorf("not taker")
	}

	s.cpUnsignedRedeem = msg.unsignedRedeem
	s.adaptorPub = msg.adaptorPub
	s.cpRefundAdaptorSig = msg.adaptorSigRefund
	s.cpRedeemAdaptorSig = msg.adaptorSigRedeem

	ourSwapContract := &asset.PrivateContract{
		LockTime:        uint64(s.lockTime.Unix()),
		Value:           s.order.Value,
		RedeemPublicKey: s.cpRedeemPubKey,
		RefundPublicKey: s.ourRefundPubKey,
	}

	ourRedeemContract := &asset.PrivateContract{
		LockTime:        uint64(s.lockTime.Unix()),
		Value:           s.order.Value,
		RedeemPublicKey: s.ourRedeemPubKey,
		RefundPublicKey: s.cpRefundPubKey,
	}

	valid, err := s.wallet.ValidateAdaptorSig(s.ourUnsignedRedeem, msg.adaptorSigRefund, msg.adaptorPub, ourRedeemContract, false)
	if err != nil {
		return nil, fmt.Errorf("error validating cp refund adaptor: %v", err)
	}
	if !valid {
		return nil, fmt.Errorf("cp refund adaptor invalid")
	}

	valid, err = s.wallet.ValidateAdaptorSig(s.cpUnsignedRedeem, msg.adaptorSigRedeem, msg.adaptorPub, ourSwapContract, true)
	if err != nil {
		return nil, fmt.Errorf("error validating cp redeem adaptor: %v", err)
	}
	if !valid {
		return nil, fmt.Errorf("cp redeem adaptor invalid")
	}

	adaptorSig, err := s.wallet.GeneratePublicKeyTweakedAdaptor(s.cpUnsignedRedeem, ourSwapContract, s.adaptorPub)
	if err != nil {
		return nil, fmt.Errorf("error generating adaptor sig: %w", err)
	}

	return &takerAdaptorMsg{
		adaptorSig: adaptorSig,
	}, nil
}

type makerRedeemMsg struct {
	redeemTx []byte
}

func (s *privateSwapper) handleTakerAdaptorMsg(msg *takerAdaptorMsg) (*makerRedeemMsg, error) {
	if !s.isMaker {
		return nil, fmt.Errorf("not maker")
	}

	cpSwapContract := &asset.PrivateContract{
		LockTime:        uint64(s.lockTime.Unix()),
		Value:           s.order.Value,
		RedeemPublicKey: s.ourRedeemPubKey,
		RefundPublicKey: s.cpRefundPubKey,
	}

	_, _, txData, err := s.wallet.RedeemPrivate(cpSwapContract, s.ourUnsignedRedeem, msg.adaptorSig, s.adaptorSecret)
	if err != nil {
		return nil, fmt.Errorf("error redeeming: %w", err)
	}

	return &makerRedeemMsg{
		redeemTx: txData,
	}, nil
}

func (s *privateSwapper) handleMakerRedeemMsg(msg *makerRedeemMsg) error {
	if s.isMaker {
		return fmt.Errorf("not taker")
	}

	ourSwapContract := &asset.PrivateContract{
		LockTime:        uint64(s.lockTime.Unix()),
		Value:           s.order.Value,
		RedeemPublicKey: s.cpRedeemPubKey,
		RefundPublicKey: s.ourRefundPubKey,
	}
	adaptorSecret, err := s.wallet.RecoverAdaptorSecret(msg.redeemTx, s.ourRefundAdaptorSig, s.cpRedeemAdaptorSig, s.adaptorPub, ourSwapContract)
	if err != nil {
		return fmt.Errorf("error recovering adaptor secret: %w", err)
	}

	cpSwapContract := &asset.PrivateContract{
		LockTime:        uint64(s.lockTime.Unix()),
		Value:           s.order.Value,
		RedeemPublicKey: s.ourRedeemPubKey,
		RefundPublicKey: s.cpRefundPubKey,
	}
	_, _, _, err = s.wallet.RedeemPrivate(cpSwapContract, s.ourUnsignedRedeem, s.cpRefundAdaptorSig, adaptorSecret)
	if err != nil {
		return fmt.Errorf("error redeeming: %w", err)
	}

	return nil
}

func TestPrivateSwap(t *testing.T) {
	rig := newTestRig(t, func(name string, _ error) {
		tLogger.Infof("%s has reported a new block", name)
	})
	defer rig.close(t)

	beta := rig.beta()
	gamma := rig.gamma()
	err := beta.Unlock(walletPassword)
	if err != nil {
		t.Fatalf("error unlocking beta wallet: %v", err)
	}
	err = gamma.Unlock(walletPassword)
	if err != nil {
		t.Fatalf("error unlocking gamma wallet: %v", err)
	}
	makerStr := "beta"
	takerStr := "gamma"
	testData := map[string]*privateSwapper{
		makerStr: {
			isMaker: true,
			wallet:  beta,
			order: &asset.Order{
				AssetVersion:  version,
				FeeSuggestion: 10,
				MaxSwapCount:  1,
				MaxFeeRate:    20,
				Value:         toSatoshi(1),
			},
			lockTime: time.Now().Add(time.Hour * -48),
		},
		takerStr: {
			isMaker: false,
			wallet:  gamma,
			order: &asset.Order{
				AssetVersion:  version,
				FeeSuggestion: 10,
				MaxSwapCount:  1,
				MaxFeeRate:    20,
				Value:         toSatoshi(1),
			},
			lockTime: time.Now().Add(time.Hour * -48),
		},
	}
	maker := testData[makerStr]
	taker := testData[takerStr]

	preSwapMsg, err := taker.generateTakerPreSwapMsg()
	if err != nil {
		t.Fatalf("error generating taker pre-swap msg: %v", err)
	}

	makerInitSwapMsg, err := maker.handleTakerPreSwapMsg(preSwapMsg)
	if err != nil {
		t.Fatalf("error handling taker pre-swap msg: %v", err)
	}

	takerInitSwapMsg, err := taker.handleMkrInitSwapMsg(makerInitSwapMsg)
	if err != nil {
		t.Fatalf("error handling maker init swap msg: %v", err)
	}

	makerAdaptorsMsg, err := maker.handleTakerInitSwapMsg(takerInitSwapMsg)
	if err != nil {
		t.Fatalf("error handling taker init swap msg: %v", err)
	}

	time.Sleep(time.Second * 16)

	takerAdaptorMsg, err := taker.handleMakerAdaptorsMsg(makerAdaptorsMsg)
	if err != nil {
		t.Fatalf("error handling maker adaptors msg: %v", err)
	}

	makerRedeemMsg, err := maker.handleTakerAdaptorMsg(takerAdaptorMsg)
	if err != nil {
		t.Fatalf("error handling taker adaptor msg: %v", err)
	}

	time.Sleep(time.Second * 16)

	err = taker.handleMakerRedeemMsg(makerRedeemMsg)
	if err != nil {
		t.Fatalf("error handling maker redeem msg: %v", err)
	}
}

func TestRefundPrivateSwap(t *testing.T) {
	rig := newTestRig(t, func(name string, _ error) {
		tLogger.Infof("%s has reported a new block", name)
	})
	defer rig.close(t)

	beta := rig.beta()
	gamma := rig.gamma()
	err := beta.Unlock(walletPassword)
	if err != nil {
		t.Fatalf("error unlocking beta wallet: %v", err)
	}
	err = gamma.Unlock(walletPassword)
	if err != nil {
		t.Fatalf("error unlocking gamma wallet: %v", err)
	}

	// Get initial balance
	initialBalance, err := beta.Balance()
	if err != nil {
		t.Fatalf("error getting initial balance: %v", err)
	}

	// Create a private swap
	order := &asset.Order{
		AssetVersion:  version,
		FeeSuggestion: 10,
		MaxSwapCount:  1,
		MaxFeeRate:    20,
		Value:         toSatoshi(1),
	}

	// Fund the order
	coins, _, _, err := beta.FundOrder(order)
	if err != nil {
		t.Fatalf("error funding order: %v", err)
	}

	redeemPubKey, err := gamma.PrivateSwapPubKey()
	if err != nil {
		t.Fatalf("error getting private swap pubkey: %v", err)
	}

	refundPubKey, err := beta.PrivateSwapPubKey()
	if err != nil {
		t.Fatalf("error getting private swap pubkey: %v", err)
	}

	// Create a private contract with a past lock time (so it can be refunded immediately)
	lockTime := time.Now().Add(time.Hour * -48) // 48 hours in the past
	contract := &asset.PrivateContract{
		LockTime:        uint64(lockTime.Unix()),
		Value:           order.Value,
		RedeemPublicKey: redeemPubKey,
		RefundPublicKey: refundPubKey,
	}

	// Create private swaps
	swaps := &asset.PrivateSwaps{
		Version:    0,
		Contracts:  []*asset.PrivateContract{contract},
		FeeRate:    order.MaxFeeRate,
		Inputs:     coins,
		LockChange: false,
	}

	// Perform the private swap
	receipts, _, _, _, err := beta.SwapPrivate(swaps)
	if err != nil {
		t.Fatalf("error performing private swap: %v", err)
	}

	if len(receipts) == 0 {
		t.Fatalf("no receipts returned from private swap")
	}

	receipt := receipts[0]
	coinID := receipt.Coin().ID()

	// Wait a bit for the transaction to be processed
	time.Sleep(time.Second * 2)

	// Get balance after swap
	afterSwapBalance, err := beta.Balance()
	if err != nil {
		t.Fatalf("error getting balance after swap: %v", err)
	}

	// Check that balance decreased (due to the swap)
	if afterSwapBalance.Available >= initialBalance.Available {
		t.Fatalf("balance should have decreased after swap. Initial: %d, After swap: %d",
			initialBalance.Available, afterSwapBalance.Available)
	}

	// Perform the refund
	refundCoinID, err := beta.RefundPrivate(coinID, contract, order.MaxFeeRate)
	if err != nil {
		t.Fatalf("error performing refund: %v", err)
	}

	// Wait a bit for the refund transaction to be processed
	time.Sleep(time.Second * 15)

	// Get balance after refund
	afterRefundBalance, err := beta.Balance()
	if err != nil {
		t.Fatalf("error getting balance after refund: %v", err)
	}

	// Check that balance increased from before the refund
	if afterRefundBalance.Available <= afterSwapBalance.Available {
		t.Fatalf("balance should have increased after refund. After swap: %d, After refund: %d",
			afterSwapBalance.Available, afterRefundBalance.Available)
	}

	// Wait 15 seconds (block time)
	time.Sleep(time.Second * 15)

	// Call RefundPrivate again and ensure the same coinID is returned
	refundCoinID2, err := beta.RefundPrivate(coinID, contract, order.MaxFeeRate)
	if err != nil {
		t.Fatalf("error calling RefundPrivate after already refunded.: %v", err)
	}

	// Check that the same coinID is returned
	if !bytes.Equal(refundCoinID, refundCoinID2) {
		t.Fatalf("second refund should return the same coinID. First: %x, Second: %x",
			refundCoinID, refundCoinID2)
	}

	t.Logf("Test completed successfully. Initial balance: %d, After swap: %d, After refund: %d",
		initialBalance.Available, afterSwapBalance.Available, afterRefundBalance.Available)
}
