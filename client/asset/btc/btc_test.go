// +build !harness

package btc

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"testing"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	dexbtc "decred.org/dcrdex/dex/btc"
	"github.com/btcsuite/btcd/btcec"
	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
	"github.com/btcsuite/btcutil"
	"github.com/decred/slog"
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
	tErr        = fmt.Errorf("test error")
	tTxID       = "308e9a3675fc3ea3862b7863eeead08c621dcc37ff59de597dd3cdab41450ad9"
	tTxHash     *chainhash.Hash
	tP2PKHAddr  = "1Bggq7Vu5oaoLFV1NNp5KhAzcku83qQhgi"
	tP2PKH      []byte
	tP2WPKHAddr = "bc1qq49ypf420s0kh52l9pk7ha8n8nhsugdpculjas"
)

func randBytes(l int) []byte {
	b := make([]byte, l)
	rand.Read(b)
	return b
}

type tRPCClient struct {
	sendHash      *chainhash.Hash
	sendErr       error
	txOutRes      *btcjson.GetTxOutResult
	txOutErr      error
	rawRes        map[string]json.RawMessage
	rawErr        map[string]error
	lastMethod    string
	lastParams    []json.RawMessage
	signFunc      func([]json.RawMessage) (json.RawMessage, error)
	signMsgFunc   func([]json.RawMessage) (json.RawMessage, error)
	verboseBlocks map[string]*btcjson.GetBlockVerboseResult
	mainchain     map[int64]*chainhash.Hash
	bestHash      chainhash.Hash
	mempoolTxs    []*chainhash.Hash
	mpErr         error
	mpVerboseTxs  map[string]*btcjson.TxRawResult
	rawVerboseErr error
}

func newTRPCClient() *tRPCClient {
	return &tRPCClient{
		txOutRes:      newTxOutResult([]byte{}, 1, 0),
		rawRes:        make(map[string]json.RawMessage),
		rawErr:        make(map[string]error),
		verboseBlocks: make(map[string]*btcjson.GetBlockVerboseResult),
		mainchain:     make(map[int64]*chainhash.Hash),
		bestHash:      chainhash.Hash{},
		mpVerboseTxs:  make(map[string]*btcjson.TxRawResult),
	}
}

func (c *tRPCClient) SendRawTransaction(tx *wire.MsgTx, _ bool) (*chainhash.Hash, error) {
	if c.sendErr == nil && c.sendHash == nil {
		h := tx.TxHash()
		return &h, nil
	}
	return c.sendHash, c.sendErr
}

func (c *tRPCClient) GetTxOut(txHash *chainhash.Hash, index uint32, mempool bool) (*btcjson.GetTxOutResult, error) {
	return c.txOutRes, c.txOutErr
}

func (c *tRPCClient) GetBlockVerboseTx(blockHash *chainhash.Hash) (*btcjson.GetBlockVerboseResult, error) {
	blk, found := c.verboseBlocks[blockHash.String()]
	if !found {
		return nil, fmt.Errorf("no test block found for %s", blockHash)
	}
	return blk, nil
}

func (c *tRPCClient) GetBlockHash(blockHeight int64) (*chainhash.Hash, error) {
	h, found := c.mainchain[blockHeight]
	if !found {
		return nil, fmt.Errorf("no test block at height %d", blockHeight)
	}
	return h, nil
}

func (c *tRPCClient) GetBestBlockHash() (*chainhash.Hash, error) {
	return &c.bestHash, nil
}

func (c *tRPCClient) GetRawMempool() ([]*chainhash.Hash, error) {
	return c.mempoolTxs, c.mpErr
}

func (c *tRPCClient) GetRawTransactionVerbose(txHash *chainhash.Hash) (*btcjson.TxRawResult, error) {
	return c.mpVerboseTxs[txHash.String()], c.rawVerboseErr
}

func (c *tRPCClient) RawRequest(method string, params []json.RawMessage) (json.RawMessage, error) {
	c.lastMethod = method
	c.lastParams = params
	switch method {
	case methodSignTx:
		if c.rawErr[method] == nil {
			return c.signFunc(params)
		}
	case methodSignMessage:
		if c.rawErr[method] == nil {
			return c.signMsgFunc(params)
		}
	case methodGetBlockVerboseTx:
		var txid string
		_ = json.Unmarshal(params[0], &txid)
		block := c.verboseBlocks[txid]
		if block == nil {
			return nil, fmt.Errorf("no block verbose found")
		}
		b, _ := json.Marshal(&verboseBlockTxs{
			Hash:   block.Hash,
			Height: uint64(block.Height),
			Tx:     block.RawTx,
		})
		return b, nil
	}
	return c.rawRes[method], c.rawErr[method]
}

func (c *tRPCClient) addRawTx(blockHeight int64, tx *btcjson.TxRawResult) (*chainhash.Hash, *btcjson.GetBlockVerboseResult) {
	blkHash, found := c.mainchain[blockHeight]
	if !found {
		var newHash chainhash.Hash
		copy(newHash[:], randBytes(32))
		c.verboseBlocks[newHash.String()] = &btcjson.GetBlockVerboseResult{}
		blkHash = &newHash
		c.mainchain[blockHeight] = blkHash
	}
	block := c.verboseBlocks[blkHash.String()]
	block.RawTx = append(block.RawTx, *tx)
	return blkHash, block
}

func makeRawTx(txid string, pkScripts []dex.Bytes, inputs []btcjson.Vin) *btcjson.TxRawResult {
	tx := &btcjson.TxRawResult{
		Txid: txid,
		Vin:  inputs,
	}
	for _, pkScript := range pkScripts {
		tx.Vout = append(tx.Vout, btcjson.Vout{
			ScriptPubKey: btcjson.ScriptPubKeyResult{
				Hex: hex.EncodeToString(pkScript),
			},
		})
	}
	return tx
}

func makeRPCVin(txid string, vout uint32, sigScript []byte) btcjson.Vin {
	return btcjson.Vin{
		Txid: txid,
		Vout: vout,
		ScriptSig: &btcjson.ScriptSig{
			Hex: hex.EncodeToString(sigScript),
		},
	}
}

func newTxOutResult(script []byte, value uint64, confs int64) *btcjson.GetTxOutResult {
	return &btcjson.GetTxOutResult{
		Confirmations: confs,
		Value:         float64(value) / 1e8,
		ScriptPubKey: btcjson.ScriptPubKeyResult{
			Hex: hex.EncodeToString(script),
		},
	}
}

func tNewWallet() (*ExchangeWallet, *tRPCClient, func()) {
	client := newTRPCClient()
	walletCfg := &asset.WalletConfig{
		Account:   "testwallet",
		TipChange: func(error) {},
	}
	walletCtx, shutdown := context.WithCancel(tCtx)
	wallet := newWallet(walletCfg, "btc", tLogger, &chaincfg.MainNetParams, client)
	go wallet.run(walletCtx)

	return wallet, client, shutdown
}

func mustMarshal(t *testing.T, thing interface{}) []byte {
	b, err := json.Marshal(thing)
	if err != nil {
		t.Fatalf("mustMarshal error: %v", err)
	}
	return b
}

func TestMain(m *testing.M) {
	tLogger = slog.NewBackend(os.Stdout).Logger("TEST")
	tLogger.SetLevel(slog.LevelTrace)
	var shutdown func()
	tCtx, shutdown = context.WithCancel(context.Background())
	tTxHash, _ = chainhash.NewHashFromStr(tTxID)
	tP2PKH, _ = hex.DecodeString("76a9148fc02268f208a61767504fe0b48d228641ba81e388ac")
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
	unspents := make([]*ListUnspentResult, 0)
	node.rawRes[methodListUnspent] = mustMarshal(t, unspents)
	node.rawRes[methodListLockUnspent] = mustMarshal(t, make([]*RPCOutpoint, 0))
	balances, err := wallet.Balance([]uint32{tBTC.FundConf})
	if err != nil {
		t.Fatalf("error for zero utxos: %v", err)
	}
	bal := balances[0]
	if bal.Available != 0 {
		t.Fatalf("expected available = 0, got %d", bal.Available)
	}
	if bal.Immature != 0 {
		t.Fatalf("unconf unconf = 0, got %d", bal.Immature)
	}

	node.rawErr[methodListUnspent] = tErr
	_, err = wallet.Balance([]uint32{tBTC.FundConf})
	if err == nil {
		t.Fatalf("no wallet error for rpc error")
	}
	node.rawErr[methodListUnspent] = nil

	littleBit := uint64(1e4)
	littleUnspent := &ListUnspentResult{
		TxID:          tTxID,
		Address:       "1Bggq7Vu5oaoLFV1NNp5KhAzcku83qQhgi",
		Amount:        float64(littleBit) / 1e8,
		Confirmations: 0,
		ScriptPubKey:  tP2PKH,
	}
	unspents = append(unspents, littleUnspent)
	node.rawRes[methodListUnspent] = mustMarshal(t, unspents)
	lockedVal := uint64(1e6)
	node.rawRes[methodListLockUnspent] = mustMarshal(t, []*RPCOutpoint{
		{
			TxID: tTxID,
			Vout: 5,
		},
	})
	node.txOutRes = &btcjson.GetTxOutResult{
		Confirmations: 2,
		Value:         float64(lockedVal) / 1e8,
	}

	balances, err = wallet.Balance([]uint32{tBTC.FundConf})
	if err != nil {
		t.Fatalf("error for 1 utxo: %v", err)
	}
	bal = balances[0]
	if bal.Available != 0 {
		t.Fatalf("expected available = 0 for unconf-only, got %d", bal.Available)
	}
	if bal.Immature != littleBit {
		t.Fatalf("expected unconf = %d, got %d", littleBit, bal.Immature)
	}
	if bal.Locked != lockedVal {
		t.Fatalf("expected locked = %d, got %d", lockedVal, bal.Locked)
	}

	lottaBit := uint64(1e7)
	unspents = append(unspents, &ListUnspentResult{
		TxID:          tTxID,
		Address:       "1Bggq7Vu5oaoLFV1NNp5KhAzcku83qQhgi",
		Amount:        float64(lottaBit) / 1e8,
		Confirmations: 1,
		Vout:          1,
		ScriptPubKey:  tP2PKH,
	})
	littleUnspent.Confirmations = 1
	node.rawRes[methodListUnspent] = mustMarshal(t, unspents)
	balances, err = wallet.Balance([]uint32{tBTC.FundConf})
	if err != nil {
		t.Fatalf("error for 2 utxos: %v", err)
	}
	bal = balances[0]
	if bal.Available != littleBit+lottaBit {
		t.Fatalf("expected available = %d for 2 outputs, got %d", littleBit+lottaBit, bal.Available)
	}
	if bal.Immature != 0 {
		t.Fatalf("expected unconf = 0 for 2 outputs, got %d", bal.Immature)
	}

	// Zero value
	_, err = wallet.Fund(0, tBTC)
	if err == nil {
		t.Fatalf("no funding error for zero value")
	}

	// Nothing to spend
	node.rawRes[methodListUnspent] = mustMarshal(t, []struct{}{})
	_, err = wallet.Fund(littleBit-300, tBTC)
	if err == nil {
		t.Fatalf("no error for zero utxos")
	}
	node.rawRes[methodListUnspent] = mustMarshal(t, unspents)

	// RPC error
	node.rawErr[methodListUnspent] = tErr
	_, err = wallet.Fund(littleBit-300, tBTC)
	if err == nil {
		t.Fatalf("no funding error for rpc error")
	}
	node.rawErr[methodListUnspent] = nil

	// Negative response when locking outputs.
	node.rawRes[methodLockUnspent] = []byte(`false`)
	_, err = wallet.Fund(littleBit-300, tBTC)
	if err == nil {
		t.Fatalf("no error for lockunspent result = false: %v", err)
	}
	node.rawRes[methodLockUnspent] = []byte(`true`)

	// Fund a little bit.
	spendables, err := wallet.Fund(littleBit-300, tBTC)
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
	spendables, err = wallet.Fund(lottaBit-3e4, tBTC)
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
	_, err = wallet.Fund(lottaBit+littleBit-1, tBTC)
	if err == nil {
		t.Fatalf("no error when not enough to cover tx fees")
	}
}

// Since ReturnCoins takes the asset.Coin interface, make sure any interface
// is acceptable.
type tCoin struct{ id []byte }

func (c *tCoin) ID() dex.Bytes {
	if len(c.id) > 0 {
		return c.id
	}
	return make([]byte, 36)
}
func (c *tCoin) String() string                 { return hex.EncodeToString(c.id) }
func (c *tCoin) Value() uint64                  { return 100 }
func (c *tCoin) Confirmations() (uint32, error) { return 2, nil }
func (c *tCoin) Redeem() dex.Bytes              { return nil }

func TestReturnCoins(t *testing.T) {
	wallet, node, shutdown := tNewWallet()
	defer shutdown()

	// Test it with the local output type.
	coins := asset.Coins{
		newOutput(node, tTxHash, 0, 1, nil),
	}
	node.rawRes[methodLockUnspent] = []byte(`true`)
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
	node.rawRes[methodLockUnspent] = []byte(`false`)
	err = wallet.ReturnCoins(coins)
	if err == nil {
		t.Fatalf("no error for RPC failure")
	}
	node.rawRes[methodLockUnspent] = []byte(`true`)

	// ReturnCoins should accept any type that implements asset.Coin.
	err = wallet.ReturnCoins(asset.Coins{&tCoin{}, &tCoin{}})
	if err != nil {
		t.Fatalf("error with custom coin type: %v", err)
	}
}

func TestFundingCoins(t *testing.T) {
	wallet, node, shutdown := tNewWallet()
	defer shutdown()

	vout := uint32(123)
	coinID := toCoinID(tTxHash, vout)

	p2pkhUnspent := &ListUnspentResult{
		TxID:         tTxID,
		Vout:         vout,
		ScriptPubKey: tP2PKH,
	}
	unspents := []*ListUnspentResult{p2pkhUnspent}
	node.rawRes[methodListUnspent] = mustMarshal(t, unspents)
	node.rawRes[methodLockUnspent] = mustMarshal(t, true)
	coinIDs := []dex.Bytes{coinID}

	ensureGood := func() {
		coins, err := wallet.FundingCoins(coinIDs)
		if err != nil {
			t.Fatalf("FundingCoins error: %v", err)
		}
		if len(coins) != 1 {
			t.Fatalf("expected 1 coin, got %d", len(coins))
		}
	}
	ensureGood()

	ensureErr := func(tag string) {
		// Clear the cache.
		wallet.fundingCoins = make(map[string]*compositeUTXO)
		_, err := wallet.FundingCoins(coinIDs)
		if err == nil {
			t.Fatalf("%s: no error", tag)
		}
	}

	// No coins
	node.rawRes[methodListUnspent] = mustMarshal(t, []*ListUnspentResult{})
	ensureErr("no coins")
	node.rawRes[methodListUnspent] = mustMarshal(t, unspents)

	// RPC error
	node.rawErr[methodListUnspent] = tErr
	ensureErr("rpc coins")
	node.rawErr[methodListUnspent] = nil

	// Bad coin ID.
	ogIDs := coinIDs
	coinIDs = []dex.Bytes{randBytes(35)}
	ensureErr("bad coin ID")
	coinIDs = ogIDs

	ensureGood()
}

func TestFundEdges(t *testing.T) {
	wallet, node, shutdown := tNewWallet()
	defer shutdown()
	swapVal := uint64(1e7)

	// Base Fees
	// fee_rate: 2 satoshi / vbyte
	// swap_size: 226 bytes
	// lot_size: 1e6
	// swap_value: 1e7
	// base fees = swap_size * fee_rate * swap_value / lot_size = 4480 satoshi

	// P2PKH input size: dexbtc.RedeemP2PKHInputSize = 148 bytes
	// backing fees: 148 * fee_rate(2 satoshi/byte) = 296 satoshi
	// total: 4480 + 296 = 4776 satoshi
	backingFees := uint64(4776)
	p2pkhUnspent := &ListUnspentResult{
		TxID:          tTxID,
		Address:       tP2PKHAddr,
		Amount:        float64(swapVal+backingFees-1) / 1e8,
		Confirmations: 5,
		ScriptPubKey:  tP2PKH,
	}
	unspents := []*ListUnspentResult{p2pkhUnspent}
	node.rawRes[methodListUnspent] = mustMarshal(t, unspents)
	_, err := wallet.Fund(swapVal, tBTC)
	if err == nil {
		t.Fatalf("no error when not enough funds in single p2pkh utxo")
	}
	// Now add the needed satoshi and try again.
	p2pkhUnspent.Amount = float64(swapVal+backingFees) / 1e8
	node.rawRes[methodListUnspent] = mustMarshal(t, unspents)
	node.rawRes[methodLockUnspent] = mustMarshal(t, true)
	_, err = wallet.Fund(swapVal, tBTC)
	if err != nil {
		t.Fatalf("error when should be enough funding in single p2pkh utxo: %v", err)
	}

	// P2SH(P2PKH) script size = 25 bytes
	// P2SH input size = overhead(40) + 1 + 73 + 1 + 33 + 1 + 1 +
	//   script length(25) = 175 bytes
	// backing fees: 175 bytes * fee_rate(2) = 350 satoshi
	// Use 1 P2SH and 1 P2PKH
	// total: 296 + 350 + 4480 = 5126 satoshi
	p2shRedeem, _ := hex.DecodeString("76a914db1755408acd315baa75c18ebbe0e8eaddf64a9788ac")
	scriptAddr := "37XDx4CwPVEg5mC3awSPGCKA5Fe5FdsAS2"
	p2shScriptPubKey, _ := hex.DecodeString("a9143ff6a24a50135f69be9ffed744443da08408fc1a87")
	backingFees = 5126
	halfSwap := swapVal / 2
	p2shUnspent := &ListUnspentResult{
		TxID:          tTxID,
		Address:       scriptAddr,
		Amount:        float64(halfSwap) / 1e8,
		Confirmations: 10,
		ScriptPubKey:  p2shScriptPubKey,
		RedeemScript:  p2shRedeem,
	}
	p2pkhUnspent.Amount = float64(halfSwap+backingFees-1) / 1e8
	unspents = []*ListUnspentResult{p2pkhUnspent, p2shUnspent}
	node.rawRes[methodListUnspent] = mustMarshal(t, unspents)
	_, err = wallet.Fund(swapVal, tBTC)
	if err == nil {
		t.Fatalf("no error when not enough funds in two utxos")
	}
	p2pkhUnspent.Amount = float64(halfSwap+backingFees) / 1e8
	node.rawRes[methodListUnspent] = mustMarshal(t, unspents)
	_, err = wallet.Fund(swapVal, tBTC)
	if err != nil {
		t.Fatalf("error when should be enough funding in two utxos: %v", err)
	}

	// P2WPKH input size = overhead(40) + ceiling(108/4) = 67 vbytes
	// backing fees: 67 * fee_rate(2) = 134 satoshi
	// total: 4480 + 114 = 4654 satoshi
	backingFees = 4614
	p2wpkhAddr := tP2WPKHAddr
	p2wpkhPkScript, _ := hex.DecodeString("0014054a40a6aa7c1f6bd15f286debf4f33cef0e21a1")
	p2wpkhUnspent := &ListUnspentResult{
		TxID:          tTxID,
		Address:       p2wpkhAddr,
		Amount:        float64(swapVal+backingFees-1) / 1e8,
		Confirmations: 3,
		ScriptPubKey:  p2wpkhPkScript,
	}
	unspents = []*ListUnspentResult{p2wpkhUnspent}
	node.rawRes[methodListUnspent] = mustMarshal(t, unspents)
	_, err = wallet.Fund(swapVal, tBTC)
	if err == nil {
		t.Fatalf("no error when not enough funds in single p2wpkh utxo")
	}
	p2wpkhUnspent.Amount = float64(swapVal+backingFees) / 1e8
	node.rawRes[methodListUnspent] = mustMarshal(t, unspents)
	_, err = wallet.Fund(swapVal, tBTC)
	if err != nil {
		t.Fatalf("error when should be enough funding in single p2wpkh utxo: %v", err)
	}

	// P2WSH(P2WPKH)
	//  p2wpkh redeem script length: 22
	// input size: overhead(40) + (1 + 73 + 1 + 33 + 1 + 22)/4 = 73 vbyte
	// backing fees: 73 * 2 = 146 satoshi
	// total: 4480 + 146 = 4666 satoshi
	backingFees = 4626
	p2wpkhRedeemScript, _ := hex.DecodeString("0014b71554f9a66ef4fa4dbeddb9fa491f5a1d938ebc")
	p2wshAddr := "bc1q9heng7q275grmy483cueqrr00dvyxpd8w6kes3nzptm7087d6lvsvffpqf"
	p2wshPkScript, _ := hex.DecodeString("00202df334780af5103d92a78e39900c6f7b584305a776ad9846620af7e79fcdd7d9")
	p2wpshUnspent := &ListUnspentResult{
		TxID:          tTxID,
		Address:       p2wshAddr,
		Amount:        float64(swapVal+backingFees-1) / 1e8,
		Confirmations: 7,
		ScriptPubKey:  p2wshPkScript,
		RedeemScript:  p2wpkhRedeemScript,
	}
	unspents = []*ListUnspentResult{p2wpshUnspent}
	node.rawRes[methodListUnspent] = mustMarshal(t, unspents)
	_, err = wallet.Fund(swapVal, tBTC)
	if err == nil {
		t.Fatalf("no error when not enough funds in single p2wsh utxo")
	}
	p2wpshUnspent.Amount = float64(swapVal+backingFees) / 1e8
	node.rawRes[methodListUnspent] = mustMarshal(t, unspents)
	_, err = wallet.Fund(swapVal, tBTC)
	if err != nil {
		t.Fatalf("error when should be enough funding in single p2wsh utxo: %v", err)
	}
}

func TestSwap(t *testing.T) {
	wallet, node, shutdown := tNewWallet()
	defer shutdown()
	swapVal := toSatoshi(5)
	coins := asset.Coins{
		newOutput(node, tTxHash, 0, toSatoshi(3), nil),
		newOutput(node, tTxHash, 0, toSatoshi(3), nil),
	}

	node.rawRes[methodNewAddress] = mustMarshal(t, tP2PKHAddr)
	node.rawRes[methodChangeAddress] = mustMarshal(t, tP2WPKHAddr)

	secretHash, _ := hex.DecodeString("5124208c80d33507befa517c08ed01aa8d33adbf37ecd70fb5f9352f7a51a88d")
	contract := &asset.Contract{
		Address:    tP2PKHAddr,
		Value:      swapVal,
		SecretHash: secretHash,
		LockTime:   uint64(time.Now().Unix()),
	}

	swaps := &asset.Swaps{
		Inputs:    coins,
		Contracts: []*asset.Contract{contract},
	}

	signTxRes := SignTxResult{
		Complete: true,
	}

	// Aim for 3 signature cycles.
	sigSizer := 0
	node.signFunc = func(params []json.RawMessage) (json.RawMessage, error) {
		var msgHex string
		err := json.Unmarshal(params[0], &msgHex)
		if err != nil {
			t.Fatalf("error unmarshaling transaction hex: %v", err)
		}
		msgBytes, _ := hex.DecodeString(msgHex)
		txReader := bytes.NewReader(msgBytes)
		msgTx := wire.NewMsgTx(wire.TxVersion)
		err = msgTx.Deserialize(txReader)
		if err != nil {
			t.Fatalf("error deserializing contract: %v", err)
		}
		// Set the sigScripts to random bytes of the correct length for spending a
		// p2pkh output.
		scriptSize := dexbtc.RedeemP2PKHSigScriptSize
		if sigSizer%2 == 0 {
			scriptSize -= 2
		}
		sigSizer++
		for i := range msgTx.TxIn {
			msgTx.TxIn[i].SignatureScript = randBytes(scriptSize)
		}
		buf := new(bytes.Buffer)
		err = msgTx.Serialize(buf)
		if err != nil {
			t.Fatalf("error serializing contract: %v", err)
		}
		signTxRes.Hex = buf.Bytes()
		return mustMarshal(t, signTxRes), nil
	}

	// This time should succeed.
	_, _, err := wallet.Swap(swaps, tBTC)
	if err != nil {
		t.Fatalf("swap error: %v", err)
	}

	// Not enough funds
	swaps.Inputs = coins[:1]
	_, _, err = wallet.Swap(swaps, tBTC)
	if err == nil {
		t.Fatalf("no error for listunspent not enough funds")
	}
	swaps.Inputs = coins

	// AddressPKH error
	node.rawErr[methodNewAddress] = tErr
	_, _, err = wallet.Swap(swaps, tBTC)
	if err == nil {
		t.Fatalf("no error for getnewaddress rpc error")
	}
	node.rawErr[methodNewAddress] = nil

	// ChangeAddress error
	node.rawErr[methodChangeAddress] = tErr
	_, _, err = wallet.Swap(swaps, tBTC)
	if err == nil {
		t.Fatalf("no error for getrawchangeaddress rpc error")
	}
	node.rawErr[methodChangeAddress] = nil

	// SignTx error
	node.rawErr[methodSignTx] = tErr
	_, _, err = wallet.Swap(swaps, tBTC)
	if err == nil {
		t.Fatalf("no error for signrawtransactionwithwallet rpc error")
	}
	node.rawErr[methodSignTx] = nil

	// incomplete signatures
	signTxRes.Complete = false
	_, _, err = wallet.Swap(swaps, tBTC)
	if err == nil {
		t.Fatalf("no error for incomplete signature rpc error")
	}
	signTxRes.Complete = true

	// Make sure we can succeed again.
	_, _, err = wallet.Swap(swaps, tBTC)
	if err != nil {
		t.Fatalf("re-swap error: %v", err)
	}
}

type TAuditInfo struct{}

func (ai *TAuditInfo) Recipient() string     { return tP2PKHAddr }
func (ai *TAuditInfo) Expiration() time.Time { return time.Time{} }
func (ai *TAuditInfo) Coin() asset.Coin      { return &tCoin{} }
func (ai *TAuditInfo) SecretHash() dex.Bytes { return nil }

func TestRedeem(t *testing.T) {
	wallet, node, shutdown := tNewWallet()
	defer shutdown()
	swapVal := toSatoshi(5)
	secret := randBytes(32)
	secretHash := sha256.Sum256(secret)
	lockTime := time.Now().Add(time.Hour * 12)

	contract, err := dexbtc.MakeContract(tP2PKHAddr, tP2PKHAddr, secretHash[:], lockTime.Unix(), &chaincfg.MainNetParams)
	if err != nil {
		t.Fatalf("error making swap contract: %v", err)
	}

	addr, _ := btcutil.DecodeAddress(tP2PKHAddr, &chaincfg.MainNetParams)
	ci := &auditInfo{
		output:     newOutput(node, tTxHash, 0, swapVal, contract),
		recipient:  addr,
		expiration: lockTime,
	}

	redemption := &asset.Redemption{
		Spends: ci,
		Secret: secret,
	}

	privBytes, _ := hex.DecodeString("b07209eec1a8fb6cfe5cb6ace36567406971a75c330db7101fb21bc679bc5330")
	privKey, _ := btcec.PrivKeyFromBytes(btcec.S256(), privBytes)
	wif, err := btcutil.NewWIF(privKey, &chaincfg.MainNetParams, true)
	if err != nil {
		t.Fatalf("error encoding wif: %v", err)
	}

	node.rawRes[methodChangeAddress] = mustMarshal(t, tP2WPKHAddr)
	node.rawRes[methodPrivKeyForAddress] = mustMarshal(t, wif.String())

	_, _, err = wallet.Redeem([]*asset.Redemption{redemption}, tBTC)
	if err != nil {
		t.Fatalf("redeem error: %v", err)
	}

	// No audit info
	redemption.Spends = nil
	_, _, err = wallet.Redeem([]*asset.Redemption{redemption}, tBTC)
	if err == nil {
		t.Fatalf("no error for nil AuditInfo")
	}
	redemption.Spends = ci

	// Spoofing AuditInfo is not allowed.
	redemption.Spends = &TAuditInfo{}
	_, _, err = wallet.Redeem([]*asset.Redemption{redemption}, tBTC)
	if err == nil {
		t.Fatalf("no error for spoofed AuditInfo")
	}
	redemption.Spends = ci

	// Wrong secret hash
	redemption.Secret = randBytes(32)
	_, _, err = wallet.Redeem([]*asset.Redemption{redemption}, tBTC)
	if err == nil {
		t.Fatalf("no error for wrong secret")
	}
	redemption.Secret = secret

	// too low of value
	ci.output.value = 200
	_, _, err = wallet.Redeem([]*asset.Redemption{redemption}, tBTC)
	if err == nil {
		t.Fatalf("no error for redemption not worth the fees")
	}
	ci.output.value = swapVal

	// Change address error
	node.rawErr[methodChangeAddress] = tErr
	_, _, err = wallet.Redeem([]*asset.Redemption{redemption}, tBTC)
	if err == nil {
		t.Fatalf("no error for change address error")
	}
	node.rawErr[methodChangeAddress] = nil

	// Missing priv key error
	node.rawErr[methodPrivKeyForAddress] = tErr
	_, _, err = wallet.Redeem([]*asset.Redemption{redemption}, tBTC)
	if err == nil {
		t.Fatalf("no error for missing private key")
	}
	node.rawErr[methodPrivKeyForAddress] = nil

	// Send error
	node.sendErr = tErr
	_, _, err = wallet.Redeem([]*asset.Redemption{redemption}, tBTC)
	if err == nil {
		t.Fatalf("no error for send error")
	}
	node.sendErr = nil

	// Wrong hash
	var h chainhash.Hash
	h[0] = 0x01
	node.sendHash = &h
	_, _, err = wallet.Redeem([]*asset.Redemption{redemption}, tBTC)
	if err == nil {
		t.Fatalf("no error for wrong return hash")
	}
	node.sendHash = nil
}

func TestSignMessage(t *testing.T) {
	wallet, node, shutdown := tNewWallet()
	defer shutdown()

	vout := uint32(5)
	privBytes, _ := hex.DecodeString("b07209eec1a8fb6cfe5cb6ace36567406971a75c330db7101fb21bc679bc5330")
	privKey, pubKey := btcec.PrivKeyFromBytes(btcec.S256(), privBytes)
	wif, err := btcutil.NewWIF(privKey, &chaincfg.MainNetParams, true)
	if err != nil {
		t.Fatalf("error encoding wif: %v", err)
	}

	msg := randBytes(36)
	pk := pubKey.SerializeCompressed()
	signature, err := privKey.Sign(msg)
	if err != nil {
		t.Fatalf("signature error: %v", err)
	}
	sig := signature.Serialize()

	opID := outpointID(tTxID, vout)
	utxo := &compositeUTXO{address: tP2PKHAddr}
	wallet.fundingCoins[opID] = utxo
	node.rawRes[methodPrivKeyForAddress] = mustMarshal(t, wif.String())
	node.signMsgFunc = func(params []json.RawMessage) (json.RawMessage, error) {
		if len(params) != 2 {
			t.Fatalf("expected 2 params, found %d", len(params))
		}
		var sentKey string
		var sentMsg dex.Bytes
		err := json.Unmarshal(params[0], &sentKey)
		if err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}
		_ = sentMsg.UnmarshalJSON(params[1])
		if sentKey != wif.String() {
			t.Fatalf("received wrong key. expected '%s', got '%s'", wif.String(), sentKey)
		}
		var checkMsg dex.Bytes = msg
		if sentMsg.String() != checkMsg.String() {
			t.Fatalf("received wrong message. expected '%s', got '%s'", checkMsg.String(), sentMsg.String())
		}
		sig, _ := wif.PrivKey.Sign(sentMsg)
		r, _ := json.Marshal(base64.StdEncoding.EncodeToString(sig.Serialize()))
		return r, nil
	}

	var coin asset.Coin = newOutput(node, tTxHash, vout, 5e7, nil)
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

	// Unknown UTXO
	delete(wallet.fundingCoins, opID)
	_, _, err = wallet.SignMessage(coin, msg)
	if err == nil {
		t.Fatalf("no error for unknown utxo")
	}
	wallet.fundingCoins[opID] = utxo

	// dumpprivkey error
	node.rawErr[methodPrivKeyForAddress] = tErr
	_, _, err = wallet.SignMessage(coin, msg)
	if err == nil {
		t.Fatalf("no error for dumpprivkey rpc error")
	}
	node.rawErr[methodPrivKeyForAddress] = nil

	// bad coin
	badCoin := &tCoin{id: make([]byte, 15)}
	_, _, err = wallet.SignMessage(badCoin, msg)
	if err == nil {
		t.Fatalf("no error for bad coin")
	}
}

func TestAuditContract(t *testing.T) {
	wallet, node, shutdown := tNewWallet()
	defer shutdown()
	vout := uint32(5)
	swapVal := toSatoshi(5)
	secretHash, _ := hex.DecodeString("5124208c80d33507befa517c08ed01aa8d33adbf37ecd70fb5f9352f7a51a88d")
	lockTime := time.Now().Add(time.Hour * 12)
	contract, err := dexbtc.MakeContract(tP2PKHAddr, tP2PKHAddr, secretHash, lockTime.Unix(), &chaincfg.MainNetParams)
	if err != nil {
		t.Fatalf("error making swap contract: %v", err)
	}
	addr, _ := btcutil.NewAddressScriptHash(contract, &chaincfg.MainNetParams)
	pkScript, _ := txscript.PayToAddrScript(addr)

	node.txOutRes = &btcjson.GetTxOutResult{
		Confirmations: 2,
		Value:         float64(swapVal) / 1e8,
		ScriptPubKey: btcjson.ScriptPubKeyResult{
			Hex: hex.EncodeToString(pkScript),
		},
	}

	audit, err := wallet.AuditContract(toCoinID(tTxHash, vout), contract)
	if err != nil {
		t.Fatalf("audit error: %v", err)
	}
	if audit.Recipient() != tP2PKHAddr {
		t.Fatalf("wrong recipient. wanted '%s', got '%s'", tP2PKHAddr, audit.Recipient())
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
	wrongAddr, _ := btcutil.NewAddressPubKeyHash(pkh, &chaincfg.MainNetParams)
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
	contract, err := dexbtc.MakeContract(tP2PKHAddr, tP2PKHAddr, secretHash[:], lockTime.Unix(), &chaincfg.MainNetParams)
	if err != nil {
		t.Fatalf("error making swap contract: %v", err)
	}
	contractAddr, _ := btcutil.NewAddressScriptHash(contract, &chaincfg.MainNetParams)
	pkScript, _ := txscript.PayToAddrScript(contractAddr)

	otherAddr, _ := btcutil.DecodeAddress(tP2PKHAddr, &chaincfg.MainNetParams)
	otherScript, _ := txscript.PayToAddrScript(otherAddr)

	redemptionScript, _ := dexbtc.RedeemP2SHContract(contract, randBytes(73), randBytes(33), secret)
	otherSpendScript, _ := txscript.NewScriptBuilder().
		AddData(randBytes(73)).
		AddData(randBytes(33)).
		Script()

	// Prepare the "blockchain"
	inputs := []btcjson.Vin{makeRPCVin(otherTxid, 0, otherSpendScript)}
	// Add the contract transaction. Put the pay-to-contract script at index 1.
	blockHash, contractBlock := node.addRawTx(contractHeight, makeRawTx(contractTxid, []dex.Bytes{otherScript, pkScript}, inputs))
	getTxRes := &GetTransactionResult{
		BlockHash:  blockHash.String(),
		BlockIndex: contractHeight,
		Details: []*WalletTxDetails{
			{
				Address:  contractAddr.String(),
				Category: TxCatSend,
				Vout:     contractVout,
			},
		},
	}
	node.rawRes[methodGetTransaction] = mustMarshal(t, getTxRes)
	// Add an intermediate block for good measure.
	node.addRawTx(contractHeight+1, makeRawTx(otherTxid, []dex.Bytes{otherScript}, inputs))
	// Now add the redemption.
	inputs = append(inputs, makeRPCVin(contractTxid, contractVout, redemptionScript))
	rawRedeem := makeRawTx(otherTxid, []dex.Bytes{otherScript}, inputs)
	_, redeemBlock := node.addRawTx(contractHeight+2, rawRedeem)
	// Add the redeem transaction to the verbose block.

	checkSecret, err := wallet.FindRedemption(tCtx, coinID)
	if err != nil {
		t.Fatalf("error finding redemption: %v", err)
	}
	if !bytes.Equal(checkSecret, secret) {
		t.Fatalf("wrong secret. expected %x, got %x", secret, checkSecret)
	}

	// gettransaction error
	node.rawErr[methodGetTransaction] = tErr
	_, err = wallet.FindRedemption(tCtx, coinID)
	if err == nil {
		t.Fatalf("no error for gettransaction rpc error")
	}
	node.rawErr[methodGetTransaction] = nil

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
	contract, err := dexbtc.MakeContract(tP2PKHAddr, tP2PKHAddr, secretHash[:], lockTime.Unix(), &chaincfg.MainNetParams)
	if err != nil {
		t.Fatalf("error making swap contract: %v", err)
	}

	bigTxOut := newTxOutResult(nil, 1e8, 2)
	node.txOutRes = bigTxOut
	node.rawRes[methodChangeAddress] = mustMarshal(t, tP2WPKHAddr)

	privBytes, _ := hex.DecodeString("b07209eec1a8fb6cfe5cb6ace36567406971a75c330db7101fb21bc679bc5330")
	privKey, _ := btcec.PrivKeyFromBytes(btcec.S256(), privBytes)
	wif, err := btcutil.NewWIF(privKey, &chaincfg.MainNetParams, true)
	if err != nil {
		t.Fatalf("error encoding wif: %v", err)
	}
	node.rawRes[methodPrivKeyForAddress] = mustMarshal(t, wif.String())

	contractOutput := newOutput(node, tTxHash, 0, 1e8, contract)
	receipt := &swapReceipt{
		output:     contractOutput,
		expiration: time.Now().Add(time.Hour * 24).UTC(),
	}
	err = wallet.Refund(receipt, tBTC)
	if err != nil {
		t.Fatalf("refund error: %v", err)
	}

	// Invalid coin
	badReceipt := &tReceipt{
		coin: &tCoin{id: make([]byte, 15)},
	}
	err = wallet.Refund(badReceipt, tBTC)
	if err == nil {
		t.Fatalf("no error for bad receipt")
	}

	// gettxout error
	node.txOutErr = tErr
	err = wallet.Refund(receipt, tBTC)
	if err == nil {
		t.Fatalf("no error for missing utxo")
	}
	node.txOutErr = nil

	// bad contract
	receipt.output = newOutput(node, tTxHash, 0, 1e8, randBytes(50))
	err = wallet.Refund(receipt, tBTC)
	if err == nil {
		t.Fatalf("no error for bad contract")
	}
	receipt.output = contractOutput

	// Too small.
	node.txOutRes = newTxOutResult(nil, 100, 2)
	err = wallet.Refund(receipt, tBTC)
	if err == nil {
		t.Fatalf("no error for value < fees")
	}
	node.txOutRes = bigTxOut

	// getrawchangeaddress error
	node.rawErr[methodChangeAddress] = tErr
	err = wallet.Refund(receipt, tBTC)
	if err == nil {
		t.Fatalf("no error for getrawchangeaddress rpc error")
	}
	node.rawErr[methodChangeAddress] = nil

	// signature error
	node.rawErr[methodPrivKeyForAddress] = tErr
	err = wallet.Refund(receipt, tBTC)
	if err == nil {
		t.Fatalf("no error for dumpprivkey rpc error")
	}
	node.rawErr[methodPrivKeyForAddress] = nil

	// send error
	node.sendErr = tErr
	err = wallet.Refund(receipt, tBTC)
	if err == nil {
		t.Fatalf("no error for sendrawtransaction rpc error")
	}
	node.sendErr = nil

	// bad checkhash
	var badHash chainhash.Hash
	badHash[0] = 0x05
	node.sendHash = &badHash
	err = wallet.Refund(receipt, tBTC)
	if err == nil {
		t.Fatalf("no error for tx hash")
	}
	node.sendHash = nil

	// Sanity check that we can succeed again.
	err = wallet.Refund(receipt, tBTC)
	if err != nil {
		t.Fatalf("re-refund error: %v", err)
	}
}

func TestLockUnlock(t *testing.T) {
	wallet, node, shutdown := tNewWallet()
	defer shutdown()

	// just checking that the errors come through.
	node.rawRes[methodUnlock] = mustMarshal(t, true)
	node.rawRes[methodLockUnspent] = []byte(`true`)
	err := wallet.Unlock("pass", time.Hour*24*365)
	if err != nil {
		t.Fatalf("unlock error: %v", err)
	}
	node.rawErr[methodUnlock] = tErr
	err = wallet.Unlock("pass", time.Hour*24*365)
	if err == nil {
		t.Fatalf("no error for walletpassphrase error")
	}

	// same for locking
	node.rawRes[methodLock] = mustMarshal(t, true)
	err = wallet.Lock()
	if err != nil {
		t.Fatalf("lock error: %v", err)
	}
	node.rawErr[methodLock] = tErr
	err = wallet.Lock()
	if err == nil {
		t.Fatalf("no error for walletlock rpc error")
	}
}

type tSenderType byte

const (
	tPayFeeSender tSenderType = iota
	tWithdrawSender
)

func testSender(t *testing.T, senderType tSenderType) {
	wallet, node, shutdown := tNewWallet()
	sender := func(addr string, val uint64) (asset.Coin, error) {
		return wallet.PayFee(addr, val, tBTC)
	}
	if senderType == tWithdrawSender {
		sender = func(addr string, val uint64) (asset.Coin, error) {
			return wallet.Withdraw(addr, val, walletInfo.DefaultFeeRate)
		}
	}
	defer shutdown()
	addr := tP2PKHAddr
	fee := float64(1) // BTC
	node.rawRes[methodSetTxFee] = mustMarshal(t, true)
	node.rawRes[methodChangeAddress] = mustMarshal(t, tP2PKHAddr)
	node.rawRes[methodSendToAddress] = mustMarshal(t, tTxID)
	node.rawRes[methodGetTransaction] = mustMarshal(t, &GetTransactionResult{
		Details: []*WalletTxDetails{
			{
				Address:  tP2PKHAddr,
				Category: TxCatReceive,
				Vout:     1,
				Amount:   -fee,
			},
		},
	})

	unspents := []*ListUnspentResult{{
		TxID:          tTxID,
		Address:       "1Bggq7Vu5oaoLFV1NNp5KhAzcku83qQhgi",
		Amount:        100,
		Confirmations: 1,
		Vout:          1,
		ScriptPubKey:  tP2PKH,
	}}
	node.rawRes[methodListUnspent] = mustMarshal(t, unspents)

	_, err := sender(addr, toSatoshi(fee))
	if err != nil {
		t.Fatalf("PayFee error: %v", err)
	}

	// SendToAddress error
	node.rawErr[methodSendToAddress] = tErr
	_, err = sender(addr, 1e8)
	if err == nil {
		t.Fatalf("no error for SendToAddress error: %v", err)
	}
	node.rawErr[methodSendToAddress] = nil

	// GetTransaction error
	node.rawErr[methodGetTransaction] = tErr
	_, err = sender(addr, 1e8)
	if err == nil {
		t.Fatalf("no error for gettransaction error: %v", err)
	}
	node.rawErr[methodGetTransaction] = nil

	// good again
	_, err = sender(addr, toSatoshi(fee))
	if err != nil {
		t.Fatalf("PayFee error afterwards: %v", err)
	}
}

func TestPayFee(t *testing.T) {
	testSender(t, tPayFeeSender)
}

func TestWithdraw(t *testing.T) {
	testSender(t, tWithdrawSender)
}

func TestConfirmations(t *testing.T) {
	wallet, node, shutdown := tNewWallet()
	defer shutdown()

	coinID := make([]byte, 36)
	copy(coinID[:32], tTxHash[:])

	// Bad coin id
	_, err := wallet.Confirmations(randBytes(35))
	if err == nil {
		t.Fatalf("no error for bad coin ID")
	}

	// listunspent error
	node.rawErr[methodGetTransaction] = tErr
	_, err = wallet.Confirmations(coinID)
	if err == nil {
		t.Fatalf("no error for listunspent error")
	}
	node.rawErr[methodGetTransaction] = nil

	node.rawRes[methodGetTransaction] = mustMarshal(t, &GetTransactionResult{})
	_, err = wallet.Confirmations(coinID)
	if err != nil {
		t.Fatalf("coin error: %v", err)
	}
}
