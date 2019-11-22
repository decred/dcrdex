// +build !harness

package dcr

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/rand"
	"os"
	"testing"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	dexdcr "decred.org/dcrdex/dex/dcr"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v2"
	"github.com/decred/dcrd/dcrec"
	"github.com/decred/dcrd/dcrec/secp256k1/v2"
	"github.com/decred/dcrd/dcrutil/v2"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types/v2"
	"github.com/decred/dcrd/txscript/v2"
	"github.com/decred/dcrd/wire"
	walletjson "github.com/decred/dcrwallet/rpc/jsonrpc/types"
	"github.com/decred/slog"
)

var (
	tLogger  dex.Logger
	tCtx     context.Context
	tLotSize uint64 = 1e7
	tDCR            = &dex.Asset{
		ID:       42,
		Symbol:   "dcr",
		SwapSize: dexdcr.InitTxSize,
		FeeRate:  10,
		LotSize:  tLotSize,
		RateStep: 100,
		SwapConf: 1,
		FundConf: 1,
	}
	tErr         = fmt.Errorf("test error")
	tTxID        = "308e9a3675fc3ea3862b7863eeead08c621dcc37ff59de597dd3cdab41450ad9"
	tTxHash      *chainhash.Hash
	tPKHAddr     dcrutil.Address
	tP2PKHScript []byte
)

func randBytes(l int) []byte {
	b := make([]byte, l)
	rand.Read(b)
	return b
}

func makeGetTxOutRes(confs, lots int64, pkScript []byte) *chainjson.GetTxOutResult {
	return &chainjson.GetTxOutResult{
		Confirmations: confs,
		Value:         float64(lots*int64(tLotSize)) / 1e8,
		ScriptPubKey: chainjson.ScriptPubKeyResult{
			Hex: hex.EncodeToString(pkScript),
		},
	}
}

func makeRawTx(txid string, pkScripts []dex.Bytes, inputs []chainjson.Vin) *chainjson.TxRawResult {
	tx := &chainjson.TxRawResult{
		Txid: txid,
		Vin:  inputs,
	}
	for _, pkScript := range pkScripts {
		tx.Vout = append(tx.Vout, chainjson.Vout{
			ScriptPubKey: chainjson.ScriptPubKeyResult{
				Hex: hex.EncodeToString(pkScript),
			},
		})
	}
	return tx
}

func makeRPCVin(txid string, vout uint32, sigScript []byte) chainjson.Vin {
	return chainjson.Vin{
		Txid: txid,
		Vout: vout,
		ScriptSig: &chainjson.ScriptSig{
			Hex: hex.EncodeToString(sigScript),
		},
	}
}

func newTxOutResult(script []byte, value uint64, confs int64) *chainjson.GetTxOutResult {
	return &chainjson.GetTxOutResult{
		Confirmations: confs,
		Value:         float64(value) / 1e8,
		ScriptPubKey: chainjson.ScriptPubKeyResult{
			Hex: hex.EncodeToString(script),
		},
	}
}

func tNewWallet() (*ExchangeWallet, *tRPCClient, func()) {
	client := newTRPCClient()
	walletCfg := &WalletConfig{
		Account:   "default",
		AssetInfo: tDCR,
		TipChange: func(error) {},
	}
	walletCtx, shutdown := context.WithCancel(tCtx)
	wallet := unconnectedWallet(walletCtx, walletCfg, tLogger)
	wallet.node = client

	return wallet, client, shutdown
}

type tRPCClient struct {
	sendRawHash     *chainhash.Hash
	sendRawErr      error
	txOutRes        map[string]*chainjson.GetTxOutResult
	txOutErr        error
	bestBlockHash   *chainhash.Hash
	bestBlockHeight int64
	bestBlockErr    error
	mempool         []*chainhash.Hash
	mempoolErr      error
	rawTx           *chainjson.TxRawResult
	rawTxErr        error
	unspent         []walletjson.ListUnspentResult
	unspentErr      error
	lockUnspentErr  error
	changeAddr      dcrutil.Address
	changeAddrErr   error
	newAddr         dcrutil.Address
	newAddrErr      error
	signFunc        func(tx *wire.MsgTx) (*wire.MsgTx, bool, error)
	privWIF         *dcrutil.WIF
	privWIFErr      error
	walletTx        *walletjson.GetTransactionResult
	walletTxErr     error
	lockErr         error
	passErr         error
	disconnected    bool
	verboseBlocks   map[string]*chainjson.GetBlockVerboseResult
	mainchain       map[int64]*chainhash.Hash
	bestHash        chainhash.Hash
}

func newTRPCClient() *tRPCClient {
	return &tRPCClient{
		txOutRes:      make(map[string]*chainjson.GetTxOutResult),
		verboseBlocks: make(map[string]*chainjson.GetBlockVerboseResult),
		mainchain:     make(map[int64]*chainhash.Hash),
		bestHash:      chainhash.Hash{},
	}
}

func (c *tRPCClient) SendRawTransaction(tx *wire.MsgTx, allowHighFees bool) (*chainhash.Hash, error) {
	if c.sendRawErr == nil && c.sendRawHash == nil {
		h := tx.TxHash()
		return &h, nil
	}
	return c.sendRawHash, c.sendRawErr
}

func (c *tRPCClient) GetTxOut(txHash *chainhash.Hash, vout uint32, mempool bool) (*chainjson.GetTxOutResult, error) {
	return c.txOutRes[outpointID(txHash, vout)], c.txOutErr
}

func (c *tRPCClient) GetBestBlock() (*chainhash.Hash, int64, error) {
	return c.bestBlockHash, c.bestBlockHeight, c.bestBlockErr
}

func (c *tRPCClient) GetBlockHash(blockHeight int64) (*chainhash.Hash, error) {
	h, found := c.mainchain[blockHeight]
	if !found {
		return nil, fmt.Errorf("no test block at height %d", blockHeight)
	}
	return h, nil
}

func (c *tRPCClient) GetBlockVerbose(blockHash *chainhash.Hash, verboseTx bool) (*chainjson.GetBlockVerboseResult, error) {
	blk, found := c.verboseBlocks[blockHash.String()]
	if !found {
		return nil, fmt.Errorf("no test block found for %s", blockHash)
	}
	return blk, nil
}

func (c *tRPCClient) GetRawMempool(txType chainjson.GetRawMempoolTxTypeCmd) ([]*chainhash.Hash, error) {
	return c.mempool, c.mempoolErr
}

func (c *tRPCClient) GetRawTransactionVerbose(txHash *chainhash.Hash) (*chainjson.TxRawResult, error) {
	return c.rawTx, c.rawTxErr
}

func (c *tRPCClient) addRawTx(blockHeight int64, tx *chainjson.TxRawResult) (*chainhash.Hash, *chainjson.GetBlockVerboseResult) {
	blkHash, found := c.mainchain[blockHeight]
	if !found {
		var newHash chainhash.Hash
		copy(newHash[:], randBytes(32))
		c.verboseBlocks[newHash.String()] = &chainjson.GetBlockVerboseResult{}
		blkHash = &newHash
		c.mainchain[blockHeight] = blkHash
	}
	block := c.verboseBlocks[blkHash.String()]
	block.RawTx = append(block.RawTx, *tx)
	return blkHash, block
}

func (c *tRPCClient) ListUnspentMin(int) ([]walletjson.ListUnspentResult, error) {
	return c.unspent, c.unspentErr
}

func (c *tRPCClient) LockUnspent(unlock bool, ops []*wire.OutPoint) error {
	return c.lockUnspentErr
}

func (c *tRPCClient) GetRawChangeAddress(account string, net dcrutil.AddressParams) (dcrutil.Address, error) {
	return c.changeAddr, c.changeAddrErr
}

func (c *tRPCClient) GetNewAddress(account string, net dcrutil.AddressParams) (dcrutil.Address, error) {
	return c.newAddr, c.newAddrErr
}

func (c *tRPCClient) SignRawTransaction(tx *wire.MsgTx) (*wire.MsgTx, bool, error) {
	return c.signFunc(tx)
}

func (c *tRPCClient) DumpPrivKey(address dcrutil.Address, net [2]byte) (*dcrutil.WIF, error) {
	return c.privWIF, c.privWIFErr
}

func (c *tRPCClient) GetTransaction(txHash *chainhash.Hash) (*walletjson.GetTransactionResult, error) {
	return c.walletTx, c.walletTxErr
}

func (c *tRPCClient) WalletLock() error {
	return c.lockErr
}

func (c *tRPCClient) WalletPassphrase(passphrase string, timeoutSecs int64) error {
	return c.passErr
}

func (c *tRPCClient) Disconnected() bool {
	return c.disconnected
}

func TestMain(m *testing.M) {
	chainParams = chaincfg.MainNetParams()
	tPKHAddr, _ = dcrutil.DecodeAddress("DsTya4cCFBgtofDLiRhkyPYEQjgs3HnarVP", chainParams)
	tLogger = slog.NewBackend(os.Stdout).Logger("TEST")
	tLogger.SetLevel(slog.LevelTrace)
	var shutdown func()
	tCtx, shutdown = context.WithCancel(context.Background())
	tTxHash, _ = chainhash.NewHashFromStr(tTxID)
	tP2PKHScript, _ = hex.DecodeString("76a9148fc02268f208a61767504fe0b48d228641ba81e388ac")
	// tP2SH, _ = hex.DecodeString("76a91412a9abf5c32392f38bd8a1f57d81b1aeecc5699588ac")
	doIt := func() int {
		// Not counted as coverage, must test Archiver constructor explicitly.
		defer shutdown()
		return m.Run()
	}
	os.Exit(doIt())
}

func TestAvailableFund(t *testing.T) {
	wallet, node, shutdown := tNewWallet()
	defer shutdown()

	// With an empty list returned, there should be no error, but the value zero
	// should be returned.
	unspents := make([]walletjson.ListUnspentResult, 0)
	node.unspent = unspents
	available, unconf, err := wallet.Balance()
	if err != nil {
		t.Fatalf("error for zero utxos: %v", err)
	}
	if available != 0 {
		t.Fatalf("expected available = 0, got %d", available)
	}
	if unconf != 0 {
		t.Fatalf("unconf unconf = 0, got %d", unconf)
	}

	littleBit := uint64(1e7)
	littleUnspent := walletjson.ListUnspentResult{
		TxID:          tTxID,
		Address:       tPKHAddr.String(),
		Amount:        float64(littleBit) / 1e8,
		Confirmations: 0,
		ScriptPubKey:  hex.EncodeToString(tP2PKHScript),
	}

	node.unspent = []walletjson.ListUnspentResult{littleUnspent}
	available, unconf, err = wallet.Balance()
	if err != nil {
		t.Fatalf("error for 1 utxo: %v", err)
	}
	if available != 0 {
		t.Fatalf("expected available = 0 for unconf-only, got %d", available)
	}
	if unconf != littleBit {
		t.Fatalf("expected unconf = %d, got %d", littleBit, unconf)
	}

	lottaBit := uint64(1e9)
	littleUnspent.Confirmations = 1
	unspents = []walletjson.ListUnspentResult{littleUnspent, {
		TxID:          tTxID,
		Address:       tPKHAddr.String(),
		Amount:        float64(lottaBit) / 1e8,
		Confirmations: 1,
		Vout:          1,
		ScriptPubKey:  hex.EncodeToString(tP2PKHScript),
	}}
	node.unspent = unspents
	available, unconf, err = wallet.Balance()
	if err != nil {
		t.Fatalf("error for 2 utxos: %v", err)
	}
	if available != littleBit+lottaBit {
		t.Fatalf("expected available = %d for 2 outputs, got %d", littleBit+lottaBit, available)
	}
	if unconf != 0 {
		t.Fatalf("expected unconf = 0 for 2 outputs, got %d", unconf)
	}

	// Zero value
	_, err = wallet.Fund(0)
	if err == nil {
		t.Fatalf("no funding error for zero value")
	}

	// RPC error
	node.unspentErr = tErr
	_, err = wallet.Fund(littleBit - 5000)
	if err == nil {
		t.Fatalf("no funding error for rpc error")
	}
	node.unspentErr = nil

	// Negative response when locking outputs.
	node.lockUnspentErr = tErr
	_, err = wallet.Fund(littleBit - 5000)
	if err == nil {
		t.Fatalf("no error for lockunspent result = false: %v", err)
	}
	node.lockUnspentErr = nil

	// Fund a little bit.
	spendables, err := wallet.Fund(littleBit - 5000)
	if err != nil {
		t.Fatalf("error funding small amount: %v", err)
	}
	if len(spendables) != 1 {
		t.Fatalf("expected 1 spendable, got %d", len(spendables))
	}
	v := spendables[0].Value()
	if v != littleBit {
		t.Fatalf("expected spendable of value %d, got %d", littleBit, v)
	}

	// Fund a lotta bit.
	spendables, err = wallet.Fund(lottaBit - 3e6)
	if err != nil {
		t.Fatalf("error funding large amount: %v", err)
	}
	if len(spendables) != 1 {
		t.Fatalf("expected 1 spendable, got %d", len(spendables))
	}
	v = spendables[0].Value()
	if v != lottaBit {
		t.Fatalf("expected spendable of value %d, got %d", lottaBit, v)
	}
	// Not enough to cover transaction fees.
	_, err = wallet.Fund(lottaBit + littleBit - 1)
	if err == nil {
		t.Fatalf("no error when not enough to cover tx fees")
	}
}

// Since ReturnCoins takes the wallet.Coin interface, make sure any interface
// is acceptable.
type tCoin struct{ id []byte }

func (c *tCoin) ID() dex.Bytes {
	if len(c.id) > 0 {
		return c.id
	}
	return make([]byte, 36)
}
func (c *tCoin) Value() uint64                  { return 100 }
func (c *tCoin) Confirmations() (uint32, error) { return 2, nil }
func (c *tCoin) Redeem() dex.Bytes              { return nil }

func TestReturnCoins(t *testing.T) {
	wallet, node, shutdown := tNewWallet()
	defer shutdown()

	// Test it with the local output type.
	coins := asset.Coins{
		newOutput(node, tTxHash, 0, 1, wire.TxTreeRegular, nil),
	}
	err := wallet.ReturnCoins(coins)
	if err != nil {
		t.Fatalf("error with output type coins: %v", err)
	}

	// Should error for no coins.
	err = wallet.ReturnCoins(asset.Coins{})
	if err == nil {
		t.Fatalf("no error for zero coins")
	}

	// Have the RPC return negative response.
	node.lockUnspentErr = tErr
	err = wallet.ReturnCoins(coins)
	if err == nil {
		t.Fatalf("no error for RPC failure")
	}
	node.lockUnspentErr = nil

	// ReturnCoins should accept any type that implements wallet.Coin.

	// Test the convertCoin method while we're here.
	badID := []byte{0x01, 0x02}
	badCoins := asset.Coins{&tCoin{id: badID}, &tCoin{id: badID}}

	err = wallet.ReturnCoins(badCoins)
	if err == nil {
		t.Fatalf("no error for bad coins")
	}

	coinID := toCoinID(tTxHash, 0)
	coins = asset.Coins{&tCoin{id: coinID}, &tCoin{id: coinID}}
	err = wallet.ReturnCoins(coins)
	if err == nil {
		t.Fatalf("no error for missing txout")
	}

	node.txOutRes[outpointID(tTxHash, 0)] = makeGetTxOutRes(1, 1, tP2PKHScript)
	err = wallet.ReturnCoins(coins)
	if err != nil {
		t.Fatalf("error with custom coin type: %v", err)
	}
}

func TestFundEdges(t *testing.T) {
	wallet, node, shutdown := tNewWallet()
	defer shutdown()
	swapVal := uint64(1e8)

	// Base Fees
	// fee_rate: 10 atoms / byte
	// swap_size: 253 bytes
	// lot_size: 1e7
	// swap_value: 1e8
	// base fees = swap_size * fee_rate * swap_value / lot_size = 25300 satoshi

	// P2PKH input size: dexdcr.RedeemP2PKHInputSize = 166 bytes
	// backing fees: 166 * fee_rate(10 atoms/byte) = 1660 satoshi
	// total: 25300 + 1660 = 26960 satoshi
	fees := uint64(26960)
	p2pkhUnspent := walletjson.ListUnspentResult{
		TxID:          tTxID,
		Address:       tPKHAddr.String(),
		Amount:        float64(swapVal+fees-1) / 1e8,
		Confirmations: 5,
		ScriptPubKey:  hex.EncodeToString(tP2PKHScript),
	}

	node.unspent = []walletjson.ListUnspentResult{p2pkhUnspent}
	_, err := wallet.Fund(swapVal)
	if err == nil {
		t.Fatalf("no error when not enough funds in single p2pkh utxo")
	}
	// Now add the needed satoshi and try again.
	p2pkhUnspent.Amount = float64(swapVal+fees) / 1e8
	node.unspent = []walletjson.ListUnspentResult{p2pkhUnspent}
	_, err = wallet.Fund(swapVal)
	if err != nil {
		t.Fatalf("error when should be enough funding in single p2pkh utxo: %v", err)
	}

	// P2SH(P2PKH) script size = 25 bytes
	// P2SH redeem input size = overhead(58) + 1 + 73 + 1 + 33 + 1  +
	//   script length(25) = 192 bytes
	// backing fees: 192 bytes * fee_rate(10) = 1920 satoshi
	// Use 1 P2SH and 1 P2PKH
	// total: 1660 + 1920 + 25300 = 28880 satoshi
	p2shRedeem, _ := hex.DecodeString("76a914db1755408acd315baa75c18ebbe0e8eaddf64a9788ac")
	scriptAddr := "DcsJEKNF3dQwcSozroei5FRPsbPEmMuWRaj"
	p2shScriptPubKey, _ := hex.DecodeString("a9143ff6a24a50135f69be9ffed744443da08408fc1a87")
	fees = 28880
	halfSwap := swapVal / 2
	p2shUnspent := walletjson.ListUnspentResult{
		TxID:          tTxID,
		Address:       scriptAddr,
		Amount:        float64(halfSwap) / 1e8,
		Confirmations: 10,
		ScriptPubKey:  hex.EncodeToString(p2shScriptPubKey),
		RedeemScript:  hex.EncodeToString(p2shRedeem),
	}
	p2pkhUnspent.Amount = float64(halfSwap+fees-1) / 1e8
	node.unspent = []walletjson.ListUnspentResult{p2pkhUnspent, p2shUnspent}
	_, err = wallet.Fund(swapVal)
	if err == nil {
		t.Fatalf("no error when not enough funds in two utxos")
	}
	p2pkhUnspent.Amount = float64(halfSwap+fees) / 1e8
	node.unspent = []walletjson.ListUnspentResult{p2pkhUnspent, p2shUnspent}
	_, err = wallet.Fund(swapVal)
	if err != nil {
		t.Fatalf("error when should be enough funding in two utxos: %v", err)
	}
}

func TestSwap(t *testing.T) {
	wallet, node, shutdown := tNewWallet()
	defer shutdown()
	swapVal := toAtoms(5)
	coins := asset.Coins{
		newOutput(node, tTxHash, 0, toAtoms(3), wire.TxTreeRegular, nil),
		newOutput(node, tTxHash, 0, toAtoms(3), wire.TxTreeRegular, nil),
	}

	node.newAddr = tPKHAddr
	node.changeAddr = tPKHAddr

	secretHash, _ := hex.DecodeString("5124208c80d33507befa517c08ed01aa8d33adbf37ecd70fb5f9352f7a51a88d")
	contract := &asset.Contract{
		Address:    tPKHAddr.String(),
		Value:      swapVal,
		SecretHash: secretHash,
		LockTime:   uint64(time.Now().Unix()),
	}

	swap := &asset.Swap{
		Inputs:   coins,
		Contract: contract,
	}
	swaps := []*asset.Swap{swap}

	// Aim for 3 signature cycles.
	sigSizer := 0
	signFunc := func(msgTx *wire.MsgTx) (*wire.MsgTx, bool, error) {
		// Set the sigScripts to random bytes of the correct length for spending a
		// p2pkh output.
		scriptSize := dexdcr.P2PKHSigScriptSize
		// Oscillate the signature size to work the fee optimization loop.
		if sigSizer%2 == 0 {
			scriptSize -= 2
		}
		sigSizer++
		for i := range msgTx.TxIn {
			msgTx.TxIn[i].SignatureScript = randBytes(scriptSize)
		}
		return msgTx, true, nil
	}

	node.signFunc = signFunc

	// This time should succeed.
	_, err := wallet.Swap(swaps)
	if err != nil {
		t.Fatalf("swap error: %v", err)
	}

	// Not enough funds
	swap.Inputs = coins[:1]
	_, err = wallet.Swap(swaps)
	if err == nil {
		t.Fatalf("no error for listunspent not enough funds")
	}
	swap.Inputs = coins

	// AddressPKH error
	node.newAddrErr = tErr
	_, err = wallet.Swap(swaps)
	if err == nil {
		t.Fatalf("no error for getnewaddress rpc error")
	}
	node.newAddrErr = nil

	// ChangeAddress error
	node.changeAddrErr = tErr
	_, err = wallet.Swap(swaps)
	if err == nil {
		t.Fatalf("no error for getrawchangeaddress rpc error")
	}
	node.changeAddrErr = nil

	// SignTx error
	node.signFunc = func(msgTx *wire.MsgTx) (*wire.MsgTx, bool, error) {
		return nil, false, tErr
	}
	_, err = wallet.Swap(swaps)
	if err == nil {
		t.Fatalf("no error for signrawtransactionwithwallet rpc error")
	}

	// incomplete signatures
	node.signFunc = func(msgTx *wire.MsgTx) (*wire.MsgTx, bool, error) {
		return msgTx, false, nil
	}
	_, err = wallet.Swap(swaps)
	if err == nil {
		t.Fatalf("no error for incomplete signature rpc error")
	}
	node.signFunc = signFunc

	// Make sure we can succeed again.
	_, err = wallet.Swap(swaps)
	if err != nil {
		t.Fatalf("re-swap error: %v", err)
	}
}

type TAuditInfo struct{}

func (ai *TAuditInfo) Recipient() string     { return tPKHAddr.String() }
func (ai *TAuditInfo) Expiration() time.Time { return time.Time{} }
func (ai *TAuditInfo) Coin() asset.Coin      { return &tCoin{} }

func TestRedeem(t *testing.T) {
	wallet, node, shutdown := tNewWallet()
	defer shutdown()
	swapVal := toAtoms(5)
	secret := randBytes(32)
	secretHash := sha256.Sum256(secret)
	lockTime := time.Now().Add(time.Hour * 12)
	addr := tPKHAddr.String()

	contract, err := dexdcr.MakeContract(addr, addr, secretHash[:], lockTime.Unix(), chainParams)
	if err != nil {
		t.Fatalf("error making swap contract: %v", err)
	}

	ci := &auditInfo{
		output:     newOutput(node, tTxHash, 0, swapVal, wire.TxTreeRegular, contract),
		recipient:  tPKHAddr,
		expiration: lockTime,
	}

	redemption := &asset.Redemption{
		Spends: ci,
		Secret: secret,
	}

	privBytes, _ := hex.DecodeString("b07209eec1a8fb6cfe5cb6ace36567406971a75c330db7101fb21bc679bc5330")
	privKey, _ := secp256k1.PrivKeyFromBytes(privBytes)

	node.changeAddr = tPKHAddr
	node.privWIF = dcrutil.NewWIF(privKey, chainParams.PrivateKeyID, dcrec.STEcdsaSecp256k1)

	err = wallet.Redeem([]*asset.Redemption{redemption})
	if err != nil {
		t.Fatalf("redeem error: %v", err)
	}

	// No audit info
	redemption.Spends = nil
	err = wallet.Redeem([]*asset.Redemption{redemption})
	if err == nil {
		t.Fatalf("no error for nil AuditInfo")
	}
	redemption.Spends = ci

	// Spoofing AuditInfo is not allowed.
	redemption.Spends = &TAuditInfo{}
	err = wallet.Redeem([]*asset.Redemption{redemption})
	if err == nil {
		t.Fatalf("no error for spoofed AuditInfo")
	}
	redemption.Spends = ci

	// Wrong secret hash
	redemption.Secret = randBytes(32)
	err = wallet.Redeem([]*asset.Redemption{redemption})
	if err == nil {
		t.Fatalf("no error for wrong secret")
	}
	redemption.Secret = secret

	// too low of value
	ci.output.value = 200
	err = wallet.Redeem([]*asset.Redemption{redemption})
	if err == nil {
		t.Fatalf("no error for redemption not worth the fees")
	}
	ci.output.value = swapVal

	// Change address error
	node.changeAddrErr = tErr
	err = wallet.Redeem([]*asset.Redemption{redemption})
	if err == nil {
		t.Fatalf("no error for change address error")
	}
	node.changeAddrErr = nil

	// Missing priv key error
	node.privWIFErr = tErr
	err = wallet.Redeem([]*asset.Redemption{redemption})
	if err == nil {
		t.Fatalf("no error for missing private key")
	}
	node.privWIFErr = nil

	// Send error
	node.sendRawErr = tErr
	err = wallet.Redeem([]*asset.Redemption{redemption})
	if err == nil {
		t.Fatalf("no error for send error")
	}
	node.sendRawErr = nil

	// Wrong hash
	var h chainhash.Hash
	h[0] = 0x01
	node.sendRawHash = &h
	err = wallet.Redeem([]*asset.Redemption{redemption})
	if err == nil {
		t.Fatalf("no error for wrong return hash")
	}
	node.sendRawHash = nil
}

func TestSignMessage(t *testing.T) {
	wallet, node, shutdown := tNewWallet()
	defer shutdown()

	vout := uint32(5)
	privBytes, _ := hex.DecodeString("b07209eec1a8fb6cfe5cb6ace36567406971a75c330db7101fb21bc679bc5330")
	privKey, pubKey := secp256k1.PrivKeyFromBytes(privBytes)

	msg := randBytes(36)
	pk := pubKey.SerializeCompressed()
	signature, err := privKey.Sign(msg)
	if err != nil {
		t.Fatalf("signature error: %v", err)
	}
	sig := signature.Serialize()

	node.walletTx = &walletjson.GetTransactionResult{
		Details: []walletjson.GetTransactionDetailsResult{
			{
				Address:  tPKHAddr.String(),
				Category: txCatReceive,
				Vout:     vout,
			},
		},
	}

	node.privWIF = dcrutil.NewWIF(privKey, chainParams.PrivateKeyID, dcrec.STEcdsaSecp256k1)

	var coin asset.Coin = newOutput(node, tTxHash, vout, 5e7, wire.TxTreeRegular, nil)
	pubkeys, sigs, err := wallet.SignMessage(coin, msg)
	if err != nil {
		t.Fatalf("SignMessage error: %v", err)
	}
	if len(pubkeys) != 1 {
		t.Fatalf("expected 1 pubkey, received %d", len(pubkeys))
	}
	if len(sigs) != 1 {
		t.Fatalf("expected 1 sig, received %d", len(sigs))
	}
	if !bytes.Equal(pk, pubkeys[0]) {
		t.Fatalf("wrong pubkey. expected %x, got %x", pubkeys[0], pk)
	}
	if !bytes.Equal(sig, sigs[0]) {
		t.Fatalf("wrong signature. exptected %x, got %x", sigs[0], sig)
	}

	// gettransaction error
	node.walletTxErr = tErr
	_, _, err = wallet.SignMessage(coin, msg)
	if err == nil {
		t.Fatalf("no error for gettransaction rpc error")
	}
	node.walletTxErr = nil

	// dumpprivkey error
	node.privWIFErr = tErr
	_, _, err = wallet.SignMessage(coin, msg)
	if err == nil {
		t.Fatalf("no error for dumpprivkey rpc error")
	}
	node.privWIFErr = nil

	// bad coin
	badCoin := &tCoin{id: make([]byte, 15)}
	_, _, err = wallet.SignMessage(badCoin, msg)
	if err == nil {
		t.Fatalf("no error for bad coin")
	}

	// Not a wallet address. This would probably always be an error though.
	node.walletTx = &walletjson.GetTransactionResult{}
	_, _, err = wallet.SignMessage(coin, msg)
	if err == nil {
		t.Fatalf("no error for unrecognized address")
	}
}

func TestAuditContract(t *testing.T) {
	wallet, node, shutdown := tNewWallet()
	defer shutdown()
	vout := uint32(5)
	secretHash, _ := hex.DecodeString("5124208c80d33507befa517c08ed01aa8d33adbf37ecd70fb5f9352f7a51a88d")
	lockTime := time.Now().Add(time.Hour * 12)
	addrStr := tPKHAddr.String()
	contract, err := dexdcr.MakeContract(addrStr, addrStr, secretHash[:], lockTime.Unix(), chainParams)
	if err != nil {
		t.Fatalf("error making swap contract: %v", err)
	}
	addr, _ := dcrutil.NewAddressScriptHash(contract, chainParams)
	pkScript, _ := txscript.PayToAddrScript(addr)

	node.txOutRes[outpointID(tTxHash, vout)] = makeGetTxOutRes(1, 5, pkScript)

	audit, err := wallet.AuditContract(toCoinID(tTxHash, vout), contract)
	if err != nil {
		t.Fatalf("audit error: %v", err)
	}
	if audit.Recipient() != addrStr {
		t.Fatalf("wrong recipient. wanted '%s', got '%s'", addrStr, audit.Recipient())
	}
	if !bytes.Equal(audit.Coin().Redeem(), contract) {
		t.Fatalf("contract not set to coin redeem script")
	}
	if audit.Expiration().Equal(lockTime) {
		t.Fatalf("wrong lock time. wanted %d, got %d", lockTime.Unix(), audit.Expiration().Unix())
	}

	// Invalid txid
	_, err = wallet.AuditContract(make([]byte, 15), contract)
	if err == nil {
		t.Fatalf("no error for bad txid")
	}

	// GetTxOut error
	node.txOutErr = tErr
	_, err = wallet.AuditContract(toCoinID(tTxHash, vout), contract)
	if err == nil {
		t.Fatalf("no error for unknown txout")
	}
	node.txOutErr = nil

	// Wrong contract
	pkh, _ := hex.DecodeString("c6a704f11af6cbee8738ff19fc28cdc70aba0b82")
	wrongAddr, _ := dcrutil.NewAddressPubKeyHash(pkh, chainParams, dcrec.STEcdsaSecp256k1)
	badContract, _ := txscript.PayToAddrScript(wrongAddr)
	_, err = wallet.AuditContract(toCoinID(tTxHash, vout), badContract)
	if err == nil {
		t.Fatalf("no error for wrong contract")
	}
}

type tReceipt struct {
	coin       *tCoin
	expiration uint64
}

func (r *tReceipt) Expiration() time.Time { return time.Unix(int64(r.expiration), 0).UTC() }
func (r *tReceipt) Coin() asset.Coin      { return r.coin }

func TestFindRedemption(t *testing.T) {
	wallet, node, shutdown := tNewWallet()
	defer shutdown()

	contractHeight := int64(5000)
	contractTxid := "e1b7c47df70d7d8f4c9c26f8ba9a59102c10885bd49024d32fdef08242f0c26c"
	contractTxHash, _ := chainhash.NewHashFromStr(contractTxid)
	otherTxid := "7a7b3b5c3638516bc8e7f19b4a3dec00f052a599fed5036c2b89829de2367bb6"
	contractVout := uint32(1)
	coinID := toCoinID(contractTxHash, contractVout)

	secret := randBytes(32)
	secretHash := sha256.Sum256(secret)
	lockTime := time.Now().Add(time.Hour * 12)
	addrStr := tPKHAddr.String()
	contract, err := dexdcr.MakeContract(addrStr, addrStr, secretHash[:], lockTime.Unix(), chainParams)
	if err != nil {
		t.Fatalf("error making swap contract: %v", err)
	}
	contractAddr, _ := dcrutil.NewAddressScriptHash(contract, chainParams)
	pkScript, _ := txscript.PayToAddrScript(contractAddr)

	otherScript, _ := txscript.PayToAddrScript(tPKHAddr)

	redemptionScript, _ := dexdcr.RedeemP2SHContract(contract, randBytes(73), randBytes(33), secret)
	otherSpendScript, _ := txscript.NewScriptBuilder().
		AddData(randBytes(73)).
		AddData(randBytes(33)).
		Script()

	// Prepare the "blockchain"
	inputs := []chainjson.Vin{makeRPCVin(otherTxid, 0, otherSpendScript)}
	// Add the contract transaction. Put the pay-to-contract script at index 1.
	blockHash, contractBlock := node.addRawTx(contractHeight, makeRawTx(contractTxid, []dex.Bytes{otherScript, pkScript}, inputs))
	getTxRes := &walletjson.GetTransactionResult{
		BlockHash:     blockHash.String(),
		Confirmations: 1,
		Details: []walletjson.GetTransactionDetailsResult{
			{
				Address:  contractAddr.String(),
				Category: "send",
				Vout:     contractVout,
			},
		},
	}
	node.walletTx = getTxRes
	// Add an intermediate block for good measure.
	h, middleBlock := node.addRawTx(contractHeight+1, makeRawTx(otherTxid, []dex.Bytes{otherScript}, inputs))
	contractBlock.NextHash = h.String()
	// Now add the redemption.
	inputs = append(inputs, makeRPCVin(contractTxid, contractVout, redemptionScript))
	rawRedeem := makeRawTx(otherTxid, []dex.Bytes{otherScript}, inputs)
	h, redeemBlock := node.addRawTx(contractHeight+2, rawRedeem)
	middleBlock.NextHash = h.String()
	// Add the redeem transaction to the verbose block.

	checkSecret, err := wallet.FindRedemption(tCtx, coinID)
	if err != nil {
		t.Fatalf("error finding redemption: %v", err)
	}
	if !bytes.Equal(checkSecret, secret) {
		t.Fatalf("wrong secret. expected %x, got %x", secret, checkSecret)
	}

	// gettransaction error
	node.walletTxErr = tErr
	_, err = wallet.FindRedemption(tCtx, coinID)
	if err == nil {
		t.Fatalf("no error for gettransaction rpc error")
	}
	node.walletTxErr = nil

	// missing redemption
	redeemBlock.RawTx[0].Vin[1].Txid = otherTxid
	k, err := wallet.FindRedemption(tCtx, coinID)
	if err == nil {
		t.Fatalf("no error for missing redemption rpc error: %s", k.String())
	}
	redeemBlock.RawTx[0].Vin[1].Txid = contractTxid

	// Canceled context
	deadCtx, shutdown := context.WithCancel(context.Background())
	shutdown()
	_, err = wallet.FindRedemption(deadCtx, coinID)
	if err == nil {
		t.Fatalf("no error for canceled context")
	}

	// Wrong redemption
	redeemBlock.RawTx[0].Vin[1].ScriptSig.Hex = hex.EncodeToString(randBytes(100))
	_, err = wallet.FindRedemption(tCtx, coinID)
	if err == nil {
		t.Fatalf("no error for wrong redemption")
	}
	redeemBlock.RawTx[0].Vin[1].ScriptSig.Hex = hex.EncodeToString(redemptionScript)

	// Wrong script type for output
	contractBlock.RawTx[0].Vout[1].ScriptPubKey.Hex = hex.EncodeToString(otherScript)
	_, err = wallet.FindRedemption(tCtx, coinID)
	if err == nil {
		t.Fatalf("no error for wrong script type")
	}
	contractBlock.RawTx[0].Vout[1].ScriptPubKey.Hex = hex.EncodeToString(pkScript)

	// Sanity check to make sure it passes again.
	_, err = wallet.FindRedemption(tCtx, coinID)
	if err != nil {
		t.Fatalf("error after clearing errors: %v", err)
	}
}

func TestRefund(t *testing.T) {
	wallet, node, shutdown := tNewWallet()
	defer shutdown()

	secret := randBytes(32)
	secretHash := sha256.Sum256(secret)
	lockTime := time.Now().Add(time.Hour * 12)
	addrStr := tPKHAddr.String()
	contract, err := dexdcr.MakeContract(addrStr, addrStr, secretHash[:], lockTime.Unix(), chainParams)
	if err != nil {
		t.Fatalf("error making swap contract: %v", err)
	}

	bigTxOut := makeGetTxOutRes(2, 5, nil)
	bigOutID := outpointID(tTxHash, 0)
	node.txOutRes[bigOutID] = bigTxOut
	node.changeAddr = tPKHAddr

	privBytes, _ := hex.DecodeString("b07209eec1a8fb6cfe5cb6ace36567406971a75c330db7101fb21bc679bc5330")
	privKey, _ := secp256k1.PrivKeyFromBytes(privBytes)
	node.privWIF = dcrutil.NewWIF(privKey, chainParams.PrivateKeyID, dcrec.STEcdsaSecp256k1)

	contractOutput := newOutput(node, tTxHash, 0, 1e8, wire.TxTreeRegular, contract)
	receipt := &swapReceipt{
		output:     contractOutput,
		expiration: time.Now().Add(time.Hour * 24).UTC(),
	}
	err = wallet.Refund(receipt)
	if err != nil {
		t.Fatalf("refund error: %v", err)
	}

	// Invalid coin
	badReceipt := &tReceipt{
		coin: &tCoin{id: make([]byte, 15)},
	}
	err = wallet.Refund(badReceipt)
	if err == nil {
		t.Fatalf("no error for bad receipt")
	}

	// gettxout error
	node.txOutErr = tErr
	err = wallet.Refund(receipt)
	if err == nil {
		t.Fatalf("no error for missing utxo")
	}
	node.txOutErr = nil

	// bad contract
	receipt.output = newOutput(node, tTxHash, 0, 1e8, wire.TxTreeRegular, randBytes(50))
	err = wallet.Refund(receipt)
	if err == nil {
		t.Fatalf("no error for bad contract")
	}
	receipt.output = contractOutput

	// Too small.
	node.txOutRes[bigOutID] = newTxOutResult(nil, 100, 2)
	err = wallet.Refund(receipt)
	if err == nil {
		t.Fatalf("no error for value < fees")
	}
	node.txOutRes[bigOutID] = bigTxOut

	// getrawchangeaddress error
	node.changeAddrErr = tErr
	err = wallet.Refund(receipt)
	if err == nil {
		t.Fatalf("no error for getrawchangeaddress rpc error")
	}
	node.changeAddrErr = nil

	// signature error
	node.privWIFErr = tErr
	err = wallet.Refund(receipt)
	if err == nil {
		t.Fatalf("no error for dumpprivkey rpc error")
	}
	node.privWIFErr = nil

	// send error
	node.sendRawErr = tErr
	err = wallet.Refund(receipt)
	if err == nil {
		t.Fatalf("no error for sendrawtransaction rpc error")
	}
	node.sendRawErr = nil

	// bad checkhash
	var badHash chainhash.Hash
	badHash[0] = 0x05
	node.sendRawHash = &badHash
	err = wallet.Refund(receipt)
	if err == nil {
		t.Fatalf("no error for tx hash")
	}
	node.sendRawHash = nil

	// Sanity check that we can succeed again.
	err = wallet.Refund(receipt)
	if err != nil {
		t.Fatalf("re-refund error: %v", err)
	}
}
