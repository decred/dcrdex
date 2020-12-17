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
	"sync"
	"testing"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
	dexbtc "decred.org/dcrdex/dex/networks/btc"
	"github.com/btcsuite/btcd/btcec"
	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
	"github.com/btcsuite/btcutil"
)

var (
	tLogger  dex.Logger
	tCtx     context.Context
	tLotSize uint64 = 1e6 // 0.01 BTC
	tBTC            = &dex.Asset{
		ID:           0,
		Symbol:       "btc",
		SwapSize:     dexbtc.InitTxSize,
		SwapSizeBase: dexbtc.InitTxSizeBase,
		MaxFeeRate:   34,
		LotSize:      tLotSize,
		RateStep:     10,
		SwapConf:     1,
	}
	optimalFeeRate uint64 = 24
	tErr                  = fmt.Errorf("test error")
	tTxID                 = "308e9a3675fc3ea3862b7863eeead08c621dcc37ff59de597dd3cdab41450ad9"
	tTxHash        *chainhash.Hash
	tP2PKHAddr     = "1Bggq7Vu5oaoLFV1NNp5KhAzcku83qQhgi"
	tP2PKH         []byte
	tP2WPKH        []byte
	tP2WPKHAddr    = "bc1qq49ypf420s0kh52l9pk7ha8n8nhsugdpculjas"
)

func randBytes(l int) []byte {
	b := make([]byte, l)
	rand.Read(b)
	return b
}

func signFunc(t *testing.T, params []json.RawMessage, sizeTweak int, sigComplete, segwit bool) (json.RawMessage, error) {
	signTxRes := SignTxResult{
		Complete: sigComplete,
	}
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
	if segwit {
		sigSize := 73 + sizeTweak
		for i := range msgTx.TxIn {
			msgTx.TxIn[i].Witness = wire.TxWitness{
				randBytes(sigSize),
				randBytes(33),
			}
		}
	} else {
		scriptSize := dexbtc.RedeemP2PKHSigScriptSize + sizeTweak
		for i := range msgTx.TxIn {
			msgTx.TxIn[i].SignatureScript = randBytes(scriptSize)
		}
	}

	buf := new(bytes.Buffer)
	err = msgTx.Serialize(buf)
	if err != nil {
		t.Fatalf("error serializing contract: %v", err)
	}
	signTxRes.Hex = buf.Bytes()
	return mustMarshal(t, signTxRes), nil
}

type tRPCClient struct {
	sendHash      *chainhash.Hash
	sendErr       error
	sentRawTx     *wire.MsgTx
	txOutRes      *btcjson.GetTxOutResult
	txOutErr      error
	rawRes        map[string]json.RawMessage
	rawErr        map[string]error
	signFunc      func([]json.RawMessage) (json.RawMessage, error)
	signMsgFunc   func([]json.RawMessage) (json.RawMessage, error)
	blockchainMtx sync.RWMutex
	verboseBlocks map[string]*btcjson.GetBlockVerboseResult
	mainchain     map[int64]*chainhash.Hash
	mempoolTxs    []*chainhash.Hash
	mpErr         error
	mpVerboseTxs  map[string]*btcjson.TxRawResult
	rawVerboseErr error
	lockedCoins   []*RPCOutpoint
}

func newTRPCClient() *tRPCClient {
	// setup genesis block, required by bestblock polling goroutine
	var newHash chainhash.Hash
	copy(newHash[:], randBytes(32))
	return &tRPCClient{
		txOutRes: newTxOutResult([]byte{}, 1, 0),
		rawRes:   make(map[string]json.RawMessage),
		rawErr:   make(map[string]error),
		verboseBlocks: map[string]*btcjson.GetBlockVerboseResult{
			newHash.String(): {},
		},
		mainchain: map[int64]*chainhash.Hash{
			0: &newHash,
		},
		mpVerboseTxs: make(map[string]*btcjson.TxRawResult),
	}
}

func (c *tRPCClient) EstimateSmartFee(confTarget int64, mode *btcjson.EstimateSmartFeeMode) (*btcjson.EstimateSmartFeeResult, error) {
	optimalRate := float64(optimalFeeRate) * 1e-5 // ~0.00024
	//fmt.Println((float64(optimalFeeRate) * 1e-5) - optimalRate)
	//fmt.Println(uint64(math.Round(feefloat * 1e5)))
	return &btcjson.EstimateSmartFeeResult{
		Blocks:  2,
		FeeRate: &optimalRate,
	}, nil
}

func (c *tRPCClient) SendRawTransaction(tx *wire.MsgTx, _ bool) (*chainhash.Hash, error) {
	c.sentRawTx = tx
	if c.sendErr == nil && c.sendHash == nil {
		h := tx.TxHash()
		return &h, nil
	}
	return c.sendHash, c.sendErr
}

func (c *tRPCClient) GetTxOut(txHash *chainhash.Hash, index uint32, mempool bool) (*btcjson.GetTxOutResult, error) {
	return c.txOutRes, c.txOutErr
}

func (c *tRPCClient) getBlock(blockHash string) *btcjson.GetBlockVerboseResult {
	c.blockchainMtx.Lock()
	defer c.blockchainMtx.Unlock()
	blk, found := c.verboseBlocks[blockHash]
	if !found {
		return nil
	}
	if nextHash, exists := c.mainchain[blk.Height+1]; exists {
		blk.NextHash = nextHash.String()
	}
	return blk
}

func (c *tRPCClient) GetBlockVerboseTx(blockHash *chainhash.Hash) (*btcjson.GetBlockVerboseResult, error) {
	blk := c.getBlock(blockHash.String())
	if blk == nil {
		return nil, fmt.Errorf("no test block found for %s", blockHash)
	}
	return blk, nil
}

func (c *tRPCClient) GetBlockHash(blockHeight int64) (*chainhash.Hash, error) {
	c.blockchainMtx.RLock()
	defer c.blockchainMtx.RUnlock()
	h, found := c.mainchain[blockHeight]
	if !found {
		return nil, fmt.Errorf("no test block at height %d", blockHeight)
	}
	return h, nil
}

func (c *tRPCClient) GetBestBlockHeight() int64 {
	c.blockchainMtx.RLock()
	defer c.blockchainMtx.RUnlock()
	var bestBlkHeight int64
	for height := range c.mainchain {
		if height >= bestBlkHeight {
			bestBlkHeight = height
		}
	}
	return bestBlkHeight
}

func (c *tRPCClient) GetBestBlockHash() (*chainhash.Hash, error) {
	c.blockchainMtx.RLock()
	defer c.blockchainMtx.RUnlock()
	var bestHash *chainhash.Hash
	var bestBlkHeight int64
	for height, hash := range c.mainchain {
		if height >= bestBlkHeight {
			bestBlkHeight = height
			bestHash = hash
		}
	}
	return bestHash, nil
}

func (c *tRPCClient) GetRawMempool() ([]*chainhash.Hash, error) {
	return c.mempoolTxs, c.mpErr
}

func (c *tRPCClient) GetRawTransactionVerbose(txHash *chainhash.Hash) (*btcjson.TxRawResult, error) {
	return c.mpVerboseTxs[txHash.String()], c.rawVerboseErr
}

func (c *tRPCClient) Disconnected() bool { return false }

func (c *tRPCClient) RawRequest(method string, params []json.RawMessage) (json.RawMessage, error) {
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
		var blkHash string
		_ = json.Unmarshal(params[0], &blkHash)
		block := c.getBlock(blkHash)
		if block == nil {
			return nil, fmt.Errorf("no block verbose found")
		}
		// block may get modified concurrently, lock mtx before reading fields.
		c.blockchainMtx.RLock()
		defer c.blockchainMtx.RUnlock()
		return json.Marshal(&verboseBlockTxs{
			Hash:     block.Hash,
			Height:   uint64(block.Height),
			NextHash: block.NextHash,
			Tx:       block.RawTx,
		})
	case methodGetBlockHeader:
		var blkHash string
		_ = json.Unmarshal(params[0], &blkHash)
		block := c.getBlock(blkHash)
		if block == nil {
			return nil, fmt.Errorf("no block verbose found")
		}
		// block may get modified concurrently, lock mtx before reading fields.
		c.blockchainMtx.RLock()
		defer c.blockchainMtx.RUnlock()
		return json.Marshal(&blockHeader{
			Hash:          block.Hash,
			Height:        block.Height,
			Confirmations: block.Confirmations,
			Time:          block.Time,
		})
	case methodLockUnspent:
		coins := make([]*RPCOutpoint, 0)
		_ = json.Unmarshal(params[1], &coins)
		if string(params[0]) == "false" {
			c.lockedCoins = coins
		}
	}
	return c.rawRes[method], c.rawErr[method]
}

func (c *tRPCClient) addRawTx(blockHeight int64, tx *btcjson.TxRawResult) (*chainhash.Hash, *btcjson.GetBlockVerboseResult) {
	c.blockchainMtx.Lock()
	defer c.blockchainMtx.Unlock()
	blkHash, found := c.mainchain[blockHeight]
	if !found {
		var newHash chainhash.Hash
		copy(newHash[:], randBytes(32))
		blkHash = &newHash
		c.verboseBlocks[newHash.String()] = &btcjson.GetBlockVerboseResult{
			Height: blockHeight,
			Hash:   blkHash.String(),
		}
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

func makeTxHex(pkScripts []dex.Bytes, inputs []btcjson.Vin) ([]byte, error) {
	msgTx := wire.NewMsgTx(wire.TxVersion)
	for _, input := range inputs {
		prevOutHash, err := chainhash.NewHashFromStr(input.Txid)
		if err != nil {
			return nil, err
		}
		var sigScript []byte
		if input.ScriptSig != nil {
			sigScript, err = hex.DecodeString(input.ScriptSig.Hex)
			if err != nil {
				return nil, err
			}
		}

		witness := make([][]byte, len(input.Witness))
		for i, witnessHex := range input.Witness {
			witness[i], err = hex.DecodeString(witnessHex)
			if err != nil {
				return nil, err
			}
		}
		txIn := wire.NewTxIn(wire.NewOutPoint(prevOutHash, input.Vout), sigScript, witness)
		msgTx.AddTxIn(txIn)
	}
	for _, pkScript := range pkScripts {
		txOut := wire.NewTxOut(100000000, pkScript)
		msgTx.AddTxOut(txOut)
	}
	txBuf := bytes.NewBuffer(make([]byte, 0, dexbtc.MsgTxVBytes(msgTx)))
	err := msgTx.Serialize(txBuf)
	if err != nil {
		return nil, err
	}
	return txBuf.Bytes(), nil
}

func makeRPCVin(txid string, vout uint32, sigScript []byte, witness [][]byte) btcjson.Vin {
	var rpcWitness []string
	for _, b := range witness {
		rpcWitness = append(rpcWitness, hex.EncodeToString(b))
	}

	return btcjson.Vin{
		Txid: txid,
		Vout: vout,
		ScriptSig: &btcjson.ScriptSig{
			Hex: hex.EncodeToString(sigScript),
		},
		Witness: rpcWitness,
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

func tNewWallet(segwit bool) (*ExchangeWallet, *tRPCClient, func()) {
	if segwit {
		tBTC.SwapSize = dexbtc.InitTxSizeSegwit
		tBTC.SwapSizeBase = dexbtc.InitTxSizeBaseSegwit
	} else {
		tBTC.SwapSize = dexbtc.InitTxSize
		tBTC.SwapSizeBase = dexbtc.InitTxSizeBase
	}

	client := newTRPCClient()
	walletCfg := &asset.WalletConfig{
		TipChange: func(error) {},
	}
	walletCtx, shutdown := context.WithCancel(tCtx)
	cfg := &BTCCloneCFG{
		WalletCFG:          walletCfg,
		Symbol:             "btc",
		Logger:             tLogger,
		ChainParams:        &chaincfg.MainNetParams,
		WalletInfo:         WalletInfo,
		DefaultFallbackFee: defaultFee,
		Segwit:             segwit,
	}
	wallet := newWallet(cfg, &dexbtc.Config{}, client)
	// Initialize the best block.
	bestHash, _ := client.GetBestBlockHash() // does not return error
	wallet.tipMtx.Lock()
	wallet.currentTip = &block{height: client.GetBestBlockHeight(), hash: bestHash.String()}
	wallet.tipMtx.Unlock()
	go wallet.run(walletCtx)

	return wallet, client, shutdown
}

func mustMarshal(t *testing.T, thing interface{}) []byte {
	t.Helper()
	b, err := json.Marshal(thing)
	if err != nil {
		t.Fatalf("mustMarshal error: %v", err)
	}
	return b
}

func TestMain(m *testing.M) {
	tLogger = dex.StdOutLogger("TEST", dex.LevelTrace)
	var shutdown func()
	tCtx, shutdown = context.WithCancel(context.Background())
	tTxHash, _ = chainhash.NewHashFromStr(tTxID)
	tP2PKH, _ = hex.DecodeString("76a9148fc02268f208a61767504fe0b48d228641ba81e388ac")
	tP2WPKH, _ = hex.DecodeString("0014148fc02268f208a61767504fe0b48d228641ba81")
	// tP2SH, _ = hex.DecodeString("76a91412a9abf5c32392f38bd8a1f57d81b1aeecc5699588ac")
	doIt := func() int {
		// Not counted as coverage, must test Archiver constructor explicitly.
		defer shutdown()
		return m.Run()
	}
	os.Exit(doIt())
}

func TestAvailableFund(t *testing.T) {
	wallet, node, shutdown := tNewWallet(true)
	defer shutdown()

	// With an empty list returned, there should be no error, but the value zero
	// should be returned.
	unspents := make([]*ListUnspentResult, 0)
	node.rawRes[methodListUnspent] = mustMarshal(t, unspents) // only needed for Fund, not Balance
	node.rawRes[methodListLockUnspent] = mustMarshal(t, make([]*RPCOutpoint, 0))
	var bals GetBalancesResult
	node.rawRes[methodGetBalances] = mustMarshal(t, &bals)
	bal, err := wallet.Balance()
	if err != nil {
		t.Fatalf("error for zero utxos: %v", err)
	}
	if bal.Available != 0 {
		t.Fatalf("expected available = 0, got %d", bal.Available)
	}
	if bal.Immature != 0 {
		t.Fatalf("expected unconf = 0, got %d", bal.Immature)
	}

	node.rawErr[methodGetBalances] = tErr
	_, err = wallet.Balance()
	if err == nil {
		t.Fatalf("no wallet error for rpc error")
	}
	node.rawErr[methodGetBalances] = nil
	var littleLots uint64 = 12
	littleOrder := tLotSize * littleLots
	littleFunds := calc.RequiredOrderFunds(littleOrder, dexbtc.RedeemP2PKHInputSize, littleLots, tBTC)
	littleUTXO := &ListUnspentResult{
		TxID:          tTxID,
		Address:       "1Bggq7Vu5oaoLFV1NNp5KhAzcku83qQhgi",
		Amount:        float64(littleFunds) / 1e8,
		Confirmations: 0,
		ScriptPubKey:  tP2PKH,
		Spendable:     true,
		Solvable:      true,
		Safe:          true,
	}
	unspents = append(unspents, littleUTXO)
	node.rawRes[methodListUnspent] = mustMarshal(t, unspents)
	bals.Mine.Trusted = float64(littleFunds) / 1e8
	node.rawRes[methodGetBalances] = mustMarshal(t, &bals)
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

	bal, err = wallet.Balance()
	if err != nil {
		t.Fatalf("error for 1 utxo: %v", err)
	}
	if bal.Available != littleFunds-lockedVal {
		t.Fatalf("expected available = %d for confirmed utxos, got %d", littleOrder-lockedVal, bal.Available)
	}
	if bal.Immature != 0 {
		t.Fatalf("expected immature = 0, got %d", bal.Immature)
	}
	if bal.Locked != lockedVal {
		t.Fatalf("expected locked = %d, got %d", lockedVal, bal.Locked)
	}

	var lottaLots uint64 = 100
	lottaOrder := tLotSize * lottaLots
	// Add funding for an extra input to accommodate the later combined tests.
	lottaFunds := calc.RequiredOrderFunds(lottaOrder, 2*dexbtc.RedeemP2PKHInputSize, lottaLots, tBTC)
	lottaUTXO := &ListUnspentResult{
		TxID:          tTxID,
		Address:       "1Bggq7Vu5oaoLFV1NNp5KhAzcku83qQhgi",
		Amount:        float64(lottaFunds) / 1e8,
		Confirmations: 1,
		Vout:          1,
		ScriptPubKey:  tP2PKH,
		Spendable:     true,
		Solvable:      true,
		Safe:          true,
	}
	unspents = append(unspents, lottaUTXO)
	littleUTXO.Confirmations = 1
	node.rawRes[methodListUnspent] = mustMarshal(t, unspents)
	bals.Mine.Trusted += float64(lottaFunds) / 1e8
	node.rawRes[methodGetBalances] = mustMarshal(t, &bals)
	bal, err = wallet.Balance()
	if err != nil {
		t.Fatalf("error for 2 utxos: %v", err)
	}
	if bal.Available != littleFunds+lottaFunds-lockedVal {
		t.Fatalf("expected available = %d for 2 outputs, got %d", littleFunds+lottaFunds-lockedVal, bal.Available)
	}
	if bal.Immature != 0 {
		t.Fatalf("expected immature = 0 for 2 outputs, got %d", bal.Immature)
	}

	ord := &asset.Order{
		Value:        0,
		MaxSwapCount: 1,
		DEXConfig:    tBTC,
	}

	setOrderValue := func(v uint64) {
		ord.Value = v
		ord.MaxSwapCount = v / tBTC.LotSize
	}

	// Zero value
	_, _, err = wallet.FundOrder(ord)
	if err == nil {
		t.Fatalf("no funding error for zero value")
	}

	// Nothing to spend
	node.rawRes[methodListUnspent] = mustMarshal(t, []struct{}{})
	setOrderValue(littleOrder)
	_, _, err = wallet.FundOrder(ord)
	if err == nil {
		t.Fatalf("no error for zero utxos")
	}
	node.rawRes[methodListUnspent] = mustMarshal(t, unspents)

	// RPC error
	node.rawErr[methodListUnspent] = tErr
	_, _, err = wallet.FundOrder(ord)
	if err == nil {
		t.Fatalf("no funding error for rpc error")
	}
	node.rawErr[methodListUnspent] = nil

	// Negative response when locking outputs.
	node.rawRes[methodLockUnspent] = []byte(`false`)
	_, _, err = wallet.FundOrder(ord)
	if err == nil {
		t.Fatalf("no error for lockunspent result = false: %v", err)
	}
	node.rawRes[methodLockUnspent] = []byte(`true`)

	// Fund a little bit, with unsafe littleOrder.
	littleUTXO.Safe = false
	littleUTXO.Confirmations = 0
	node.rawRes[methodListUnspent] = mustMarshal(t, unspents)
	spendables, _, err := wallet.FundOrder(ord)
	if err != nil {
		t.Fatalf("error funding small amount: %v", err)
	}
	if len(spendables) != 1 {
		t.Fatalf("expected 1 spendable, got %d", len(spendables))
	}
	v := spendables[0].Value()
	if v != lottaFunds { // has to pick the larger output
		t.Fatalf("expected spendable of value %d, got %d", lottaFunds, v)
	}

	// Now with safe unconfirmed littleOrder.
	littleUTXO.Safe = true
	node.rawRes[methodListUnspent] = mustMarshal(t, unspents)
	spendables, _, err = wallet.FundOrder(ord)
	if err != nil {
		t.Fatalf("error funding small amount: %v", err)
	}
	if len(spendables) != 1 {
		t.Fatalf("expected 1 spendable, got %d", len(spendables))
	}
	v = spendables[0].Value()
	if v != littleFunds { // now picks the smaller output
		t.Fatalf("expected spendable of value %d, got %d", littleFunds, v)
	}

	// Fund a lotta bit, covered by just the lottaBit UTXO.
	setOrderValue(lottaOrder)
	spendables, _, err = wallet.FundOrder(ord)
	if err != nil {
		t.Fatalf("error funding large amount: %v", err)
	}
	if len(spendables) != 1 {
		t.Fatalf("expected 1 spendable, got %d", len(spendables))
	}
	v = spendables[0].Value()
	if v != lottaFunds {
		t.Fatalf("expected spendable of value %d, got %d", lottaFunds, v)
	}

	// require both spendables
	extraLottaOrder := littleOrder + lottaOrder
	extraLottaLots := littleLots + lottaLots
	setOrderValue(extraLottaOrder)
	spendables, _, err = wallet.FundOrder(ord)
	if err != nil {
		t.Fatalf("error funding large amount: %v", err)
	}
	if len(spendables) != 2 {
		t.Fatalf("expected 2 spendable, got %d", len(spendables))
	}
	v = spendables[0].Value()
	if v != lottaFunds {
		t.Fatalf("expected spendable of value %d, got %d", lottaFunds, v)
	}

	// Not enough to cover transaction fees.
	tweak := float64(littleFunds+lottaFunds-calc.RequiredOrderFunds(extraLottaOrder, 2*dexbtc.RedeemP2PKHInputSize, extraLottaLots, tBTC)+1) / 1e8
	lottaUTXO.Amount -= tweak
	node.rawRes[methodListUnspent] = mustMarshal(t, unspents)
	_, _, err = wallet.FundOrder(ord)
	if err == nil {
		t.Fatalf("no error when not enough to cover tx fees")
	}
	lottaUTXO.Amount += tweak
	node.rawRes[methodListUnspent] = mustMarshal(t, unspents)

	// Prepare for a split transaction.
	baggageFees := tBTC.MaxFeeRate * splitTxBaggage
	node.rawRes[methodChangeAddress] = mustMarshal(t, tP2WPKHAddr)
	wallet.useSplitTx = true
	// No error when no split performed cuz math.
	coins, _, err := wallet.FundOrder(ord)
	if err != nil {
		t.Fatalf("error for no-split split: %v", err)
	}

	// Should be both coins.
	if len(coins) != 2 {
		t.Fatalf("no-split split didn't return both coins")
	}

	// No split because not standing order.
	ord.Immediate = true
	coins, _, err = wallet.FundOrder(ord)
	if err != nil {
		t.Fatalf("error for no-split split: %v", err)
	}
	ord.Immediate = false
	if len(coins) != 2 {
		t.Fatalf("no-split split didn't return both coins")
	}

	// With a little more locked, the split should be performed.
	node.signFunc = func(params []json.RawMessage) (json.RawMessage, error) {
		return signFunc(t, params, 0, true, wallet.segwit)
	}
	lottaUTXO.Amount += float64(baggageFees) / 1e8
	node.rawRes[methodListUnspent] = mustMarshal(t, unspents)
	coins, _, err = wallet.FundOrder(ord)
	if err != nil {
		t.Fatalf("error for split tx: %v", err)
	}

	// Should be just one coin.
	if len(coins) != 1 {
		t.Fatalf("split failed - coin count != 1")
	}
	if node.sentRawTx == nil {
		t.Fatalf("split failed - no tx sent")
	}

	// // Hit some error paths.

	// GetRawChangeAddress error
	node.rawErr[methodChangeAddress] = tErr
	_, _, err = wallet.FundOrder(ord)
	if err == nil {
		t.Fatalf("no error for split tx change addr error")
	}
	node.rawErr[methodChangeAddress] = nil

	// SendRawTx error
	node.sendErr = tErr
	_, _, err = wallet.FundOrder(ord)
	if err == nil {
		t.Fatalf("no error for split tx send error")
	}
	node.sendErr = nil

	// Success again.
	_, _, err = wallet.FundOrder(ord)
	if err != nil {
		t.Fatalf("error for split tx recovery run")
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
func (c *tCoin) String() string                                { return hex.EncodeToString(c.id) }
func (c *tCoin) Value() uint64                                 { return 100 }
func (c *tCoin) Confirmations(context.Context) (uint32, error) { return 2, nil }

func TestReturnCoins(t *testing.T) {
	wallet, node, shutdown := tNewWallet(true)
	defer shutdown()

	// Test it with the local output type.
	coins := asset.Coins{
		newOutput(tTxHash, 0, 1),
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
	wallet, node, shutdown := tNewWallet(true)
	defer shutdown()

	vout := uint32(123)
	coinID := toCoinID(tTxHash, vout)

	p2pkhUnspent := &ListUnspentResult{
		TxID:         tTxID,
		Vout:         vout,
		ScriptPubKey: tP2PKH,
		Spendable:    true,
		Solvable:     true,
		Safe:         true,
	}
	unspents := []*ListUnspentResult{p2pkhUnspent}
	node.rawRes[methodListLockUnspent] = mustMarshal(t, []*RPCOutpoint{})
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
		wallet.fundingCoins = make(map[outPoint]*utxo)
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

	// Coins locked but not in wallet.fundingCoins.
	node.rawRes[methodListLockUnspent] = mustMarshal(t, []*RPCOutpoint{
		{TxID: p2pkhUnspent.TxID, Vout: p2pkhUnspent.Vout},
	})
	node.rawRes[methodListUnspent] = mustMarshal(t, []*ListUnspentResult{})
	node.txOutRes = &btcjson.GetTxOutResult{
		Value: p2pkhUnspent.Amount,
		ScriptPubKey: btcjson.ScriptPubKeyResult{
			Hex:       hex.EncodeToString(p2pkhUnspent.ScriptPubKey),
			Addresses: []string{p2pkhUnspent.Address},
		},
	}
	ensureGood()
}

func checkMaxOrder(t *testing.T, wallet *ExchangeWallet, lots, swapVal, maxFees, estFees, locked uint64) {
	t.Helper()
	maxOrder, err := wallet.MaxOrder(tBTC.LotSize, tBTC)
	if err != nil {
		t.Fatalf("MaxOrder error: %v", err)
	}
	if maxOrder.Lots != lots {
		t.Fatalf("MaxOrder has wrong Lots. wanted %d, got %d", lots, maxOrder.Lots)
	}
	if maxOrder.Value != swapVal {
		t.Fatalf("MaxOrder has wrong Value. wanted %d, got %d", swapVal, maxOrder.Value)
	}
	if maxOrder.MaxFees != maxFees {
		t.Fatalf("MaxOrder has wrong MaxFees. wanted %d, got %d", maxFees, maxOrder.MaxFees)
	}
	if maxOrder.EstimatedFees != estFees {
		t.Fatalf("MaxOrder has wrong EstimatedFees. wanted %d, got %d", estFees, maxOrder.EstimatedFees)
	}
	if maxOrder.Locked != locked {
		t.Fatalf("MaxOrder has wrong Locked. wanted %d, got %d", locked, maxOrder.Locked)
	}
}

func TestFundEdges(t *testing.T) {
	wallet, node, shutdown := tNewWallet(false)
	defer shutdown()
	swapVal := uint64(1e7)
	lots := swapVal / tBTC.LotSize
	node.rawRes[methodLockUnspent] = mustMarshal(t, true)
	var estFeeRate = optimalFeeRate + 1 // +1 added in feeRate

	checkMax := func(lots, swapVal, maxFees, estFees, locked uint64) {
		checkMaxOrder(t, wallet, lots, swapVal, maxFees, estFees, locked)
	}

	// Base Fees
	// fee_rate: 34 satoshi / vbyte (MaxFeeRate)
	// swap_size: 225 bytes (InitTxSize)
	// p2pkh input: 149 bytes (RedeemP2PKHInputSize)

	// NOTE: Shouldn't swap_size_base be 73 bytes?

	// swap_size_base: 76 bytes (225 - 149 p2pkh input) (InitTxSizeBase)
	// lot_size: 1e6
	// swap_value: 1e7
	// lots = swap_size / lot_size = 10
	//   total_bytes = first_swap_size + chained_swap_sizes
	//   chained_swap_sizes = (lots - 1) * swap_size
	//   first_swap_size = swap_size_base + backing_bytes
	//   total_bytes  = swap_size_base + backing_bytes + (lots - 1) * swap_size
	//   base_tx_bytes = total_bytes - backing_bytes
	// base_tx_bytes = (lots - 1) * swap_size + swap_size_base = 9 * 225 + 76 = 2101
	// base_fees = base_tx_bytes * fee_rate = 2101 * 34 = 71434
	// backing_bytes: 1x P2PKH inputs = dexbtc.P2PKHInputSize = 149 bytes
	// backing_fees: 149 * fee_rate(34 atoms/byte) = 5066 atoms
	// total_bytes  = base_tx_bytes + backing_bytes = 2101 + 149 = 2250
	// total_fees: base_fees + backing_fees = 71434 + 5066 = 76500 atoms
	//          OR total_bytes * fee_rate = 2510 * 34 = 76500
	const swapSize = 225
	const totalBytes = 2250
	backingFees := uint64(totalBytes) * tBTC.MaxFeeRate // total_bytes * fee_rate
	p2pkhUnspent := &ListUnspentResult{
		TxID:          tTxID,
		Address:       tP2PKHAddr,
		Amount:        float64(swapVal+backingFees-1) / 1e8,
		Confirmations: 5,
		ScriptPubKey:  tP2PKH,
		Spendable:     true,
		Solvable:      true,
		Safe:          true,
	}
	unspents := []*ListUnspentResult{p2pkhUnspent}
	node.rawRes[methodListUnspent] = mustMarshal(t, unspents)
	ord := &asset.Order{
		Value:        swapVal,
		MaxSwapCount: lots,
		DEXConfig:    tBTC,
	}

	var feeReduction uint64 = swapSize * tBTC.MaxFeeRate
	estFeeReduction := swapSize * estFeeRate
	checkMax(lots-1, swapVal-tBTC.LotSize, backingFees-feeReduction, totalBytes*estFeeRate-estFeeReduction, swapVal+backingFees-1)

	_, _, err := wallet.FundOrder(ord)
	if err == nil {
		t.Fatalf("no error when not enough funds in single p2pkh utxo")
	}
	// Now add the needed satoshi and try again.
	p2pkhUnspent.Amount = float64(swapVal+backingFees) / 1e8
	node.rawRes[methodListUnspent] = mustMarshal(t, unspents)

	checkMax(lots, swapVal, backingFees, totalBytes*estFeeRate, swapVal+backingFees)

	_, _, err = wallet.FundOrder(ord)
	if err != nil {
		t.Fatalf("error when should be enough funding in single p2pkh utxo: %v", err)
	}

	// For a split transaction, we would need to cover the splitTxBaggage as
	// well.
	wallet.useSplitTx = true
	node.rawRes[methodChangeAddress] = mustMarshal(t, tP2WPKHAddr)
	node.signFunc = func(params []json.RawMessage) (json.RawMessage, error) {
		return signFunc(t, params, 0, true, wallet.segwit)
	}
	backingFees = uint64(totalBytes+splitTxBaggage) * tBTC.MaxFeeRate
	// 1 too few atoms
	v := swapVal + backingFees - 1
	p2pkhUnspent.Amount = float64(v) / 1e8
	node.rawRes[methodListUnspent] = mustMarshal(t, unspents)

	coins, _, err := wallet.FundOrder(ord)
	if err != nil {
		t.Fatalf("error when skipping split tx due to baggage: %v", err)
	}
	if coins[0].Value() != v {
		t.Fatalf("split performed when baggage wasn't covered")
	}
	// Just enough.
	v = swapVal + backingFees
	p2pkhUnspent.Amount = float64(v) / 1e8
	node.rawRes[methodListUnspent] = mustMarshal(t, unspents)

	checkMax(lots, swapVal, backingFees, (totalBytes+splitTxBaggage)*estFeeRate, v)

	coins, _, err = wallet.FundOrder(ord)
	if err != nil {
		t.Fatalf("error funding split tx: %v", err)
	}
	if coins[0].Value() == v {
		t.Fatalf("split performed when baggage wasn't covered")
	}
	wallet.useSplitTx = false

	// P2SH(P2PKH) p2sh pkScript = 23 bytes, p2pkh pkScript (redeemscript) = 25 bytes
	// sigScript = signature(1 + 73) + pubkey(1 + 33) + redeemscript(1 + 25) = 134
	// P2SH input size = overhead(40) + sigScriptData(1 + 134) = 40 + 135 = 175 bytes
	// backing fees: 175 bytes * fee_rate(34) = 5950 satoshi
	// Use 1 P2SH AND 1 P2PKH from the previous test.
	// total: 71434 + 5950 + 5066 = 82450 satoshi
	p2shRedeem, _ := hex.DecodeString("76a914db1755408acd315baa75c18ebbe0e8eaddf64a9788ac") // 25, p2pkh redeem script
	scriptAddr := "37XDx4CwPVEg5mC3awSPGCKA5Fe5FdsAS2"
	p2shScriptPubKey, _ := hex.DecodeString("a9143ff6a24a50135f69be9ffed744443da08408fc1a87") // 23, p2sh pkScript
	backingFees = 82450
	halfSwap := swapVal / 2
	p2shUnspent := &ListUnspentResult{
		TxID:          tTxID,
		Address:       scriptAddr,
		Amount:        float64(halfSwap) / 1e8,
		Confirmations: 10,
		ScriptPubKey:  p2shScriptPubKey,
		RedeemScript:  p2shRedeem,
		Spendable:     true,
		Solvable:      true,
		Safe:          true,
	}
	p2pkhUnspent.Amount = float64(halfSwap+backingFees-1) / 1e8
	unspents = []*ListUnspentResult{p2pkhUnspent, p2shUnspent}
	node.rawRes[methodListUnspent] = mustMarshal(t, unspents)
	_, _, err = wallet.FundOrder(ord)
	if err == nil {
		t.Fatalf("no error when not enough funds in two utxos")
	}
	p2pkhUnspent.Amount = float64(halfSwap+backingFees) / 1e8
	node.rawRes[methodListUnspent] = mustMarshal(t, unspents)
	_, _, err = wallet.FundOrder(ord)
	if err != nil {
		t.Fatalf("error when should be enough funding in two utxos: %v", err)
	}

	// P2WPKH witness: RedeemP2WPKHInputWitnessWeight = 109
	// P2WPKH input size = overhead(40) + no sigScript(1+0) + witness(ceil(109/4)) = 69 vbytes
	// backing fees: 69 * fee_rate(34) = 2346 satoshi
	// total: base_fees(71434) + 2346 = 73780 satoshi
	backingFees = 73780
	p2wpkhAddr := tP2WPKHAddr
	p2wpkhPkScript, _ := hex.DecodeString("0014054a40a6aa7c1f6bd15f286debf4f33cef0e21a1")
	p2wpkhUnspent := &ListUnspentResult{
		TxID:          tTxID,
		Address:       p2wpkhAddr,
		Amount:        float64(swapVal+backingFees-1) / 1e8,
		Confirmations: 3,
		ScriptPubKey:  p2wpkhPkScript,
		Spendable:     true,
		Solvable:      true,
		Safe:          true,
	}
	unspents = []*ListUnspentResult{p2wpkhUnspent}
	node.rawRes[methodListUnspent] = mustMarshal(t, unspents)
	_, _, err = wallet.FundOrder(ord)
	if err == nil {
		t.Fatalf("no error when not enough funds in single p2wpkh utxo")
	}
	p2wpkhUnspent.Amount = float64(swapVal+backingFees) / 1e8
	node.rawRes[methodListUnspent] = mustMarshal(t, unspents)
	_, _, err = wallet.FundOrder(ord)
	if err != nil {
		t.Fatalf("error when should be enough funding in single p2wpkh utxo: %v", err)
	}

	// P2WSH(P2WPKH)
	//  p2wpkh redeem script length, btc.P2WPKHPkScriptSize: 22
	// witness: version(1) + signature(1 + 73) + pubkey(1 + 33) + redeemscript(1 + 22) = 132
	// input size: overhead(40) + no sigScript(1+0) + witness(132)/4 = 74 vbyte
	// backing fees: 74 * 34 = 2516 satoshi
	// total: base_fees(71434) + 2516 = 73950 satoshi
	backingFees = 73950
	p2wpkhRedeemScript, _ := hex.DecodeString("0014b71554f9a66ef4fa4dbeddb9fa491f5a1d938ebc") //22
	p2wshAddr := "bc1q9heng7q275grmy483cueqrr00dvyxpd8w6kes3nzptm7087d6lvsvffpqf"
	p2wshPkScript, _ := hex.DecodeString("00202df334780af5103d92a78e39900c6f7b584305a776ad9846620af7e79fcdd7d9") //34
	p2wpshUnspent := &ListUnspentResult{
		TxID:          tTxID,
		Address:       p2wshAddr,
		Amount:        float64(swapVal+backingFees-1) / 1e8,
		Confirmations: 7,
		ScriptPubKey:  p2wshPkScript,
		RedeemScript:  p2wpkhRedeemScript,
		Spendable:     true,
		Solvable:      true,
		Safe:          true,
	}
	unspents = []*ListUnspentResult{p2wpshUnspent}
	node.rawRes[methodListUnspent] = mustMarshal(t, unspents)
	_, _, err = wallet.FundOrder(ord)
	if err == nil {
		t.Fatalf("no error when not enough funds in single p2wsh utxo")
	}
	p2wpshUnspent.Amount = float64(swapVal+backingFees) / 1e8
	node.rawRes[methodListUnspent] = mustMarshal(t, unspents)
	_, _, err = wallet.FundOrder(ord)
	if err != nil {
		t.Fatalf("error when should be enough funding in single p2wsh utxo: %v", err)
	}
}

func TestFundEdgesSegwit(t *testing.T) {
	wallet, node, shutdown := tNewWallet(true)
	defer shutdown()
	swapVal := uint64(1e7)
	lots := swapVal / tBTC.LotSize
	node.rawRes[methodLockUnspent] = mustMarshal(t, true)
	var estFeeRate = optimalFeeRate + 1 // +1 added in feeRateWithFallback

	checkMax := func(lots, swapVal, maxFees, estFees, locked uint64) {
		checkMaxOrder(t, wallet, lots, swapVal, maxFees, estFees, locked)
	}

	// Base Fees
	// fee_rate: 34 satoshi / vbyte (MaxFeeRate)

	// swap_size: 153 bytes (InitTxSizeSegwit)
	// p2wpkh input, incl. marker and flag: 69 bytes (RedeemP2WPKHInputSize + ((RedeemP2WPKHInputWitnessWeight + 2 + 3) / 4))
	// swap_size_base: 84 bytes (153 - 69 p2pkh input) (InitTxSizeBaseSegwit)

	// lot_size: 1e6
	// swap_value: 1e7
	// lots = swap_value / lot_size = 10
	//   total_bytes = first_swap_size + chained_swap_sizes
	//   chained_swap_sizes = (lots - 1) * swap_size
	//   first_swap_size = swap_size_base + backing_bytes
	//   total_bytes  = swap_size_base + backing_bytes + (lots - 1) * swap_size
	//   base_tx_bytes = total_bytes - backing_bytes
	// base_tx_bytes = (lots - 1) * swap_size + swap_size_base = 9 * 153 + 84 = 1461
	// base_fees = base_tx_bytes * fee_rate = 1461 * 34 = 49674
	// backing_bytes: 1x P2WPKH-spending input = p2wpkh input = 69 bytes
	// backing_fees: 69 * fee_rate(34 atoms/byte) = 2346 atoms
	// total_bytes  = base_tx_bytes + backing_bytes = 1461 + 69 = 1530
	// total_fees: base_fees + backing_fees = 49674 + 2346 = 52020 atoms
	//          OR total_bytes * fee_rate = 1530 * 34 = 52020
	const swapSize = 153
	const totalBytes = 1530
	backingFees := uint64(totalBytes) * tBTC.MaxFeeRate // total_bytes * fee_rate
	p2wpkhUnspent := &ListUnspentResult{
		TxID:          tTxID,
		Address:       tP2WPKHAddr,
		Amount:        float64(swapVal+backingFees-1) / 1e8,
		Confirmations: 5,
		ScriptPubKey:  tP2WPKH,
		Spendable:     true,
		Solvable:      true,
		Safe:          true,
	}
	unspents := []*ListUnspentResult{p2wpkhUnspent}
	node.rawRes[methodListUnspent] = mustMarshal(t, unspents)
	ord := &asset.Order{
		Value:        swapVal,
		MaxSwapCount: lots,
		DEXConfig:    tBTC,
	}

	var feeReduction uint64 = swapSize * tBTC.MaxFeeRate
	estFeeReduction := swapSize * estFeeRate
	checkMax(lots-1, swapVal-tBTC.LotSize, backingFees-feeReduction, totalBytes*estFeeRate-estFeeReduction, swapVal+backingFees-1)

	_, _, err := wallet.FundOrder(ord)
	if err == nil {
		t.Fatalf("no error when not enough funds in single p2wpkh utxo")
	}
	// Now add the needed satoshi and try again.
	p2wpkhUnspent.Amount = float64(swapVal+backingFees) / 1e8
	node.rawRes[methodListUnspent] = mustMarshal(t, unspents)

	checkMax(lots, swapVal, backingFees, totalBytes*estFeeRate, swapVal+backingFees)

	_, _, err = wallet.FundOrder(ord)
	if err != nil {
		t.Fatalf("error when should be enough funding in single p2wpkh utxo: %v", err)
	}

	// For a split transaction, we would need to cover the splitTxBaggage as
	// well.
	wallet.useSplitTx = true
	node.rawRes[methodChangeAddress] = mustMarshal(t, tP2WPKHAddr)
	node.signFunc = func(params []json.RawMessage) (json.RawMessage, error) {
		return signFunc(t, params, 0, true, wallet.segwit)
	}
	backingFees = uint64(totalBytes+splitTxBaggageSegwit) * tBTC.MaxFeeRate
	v := swapVal + backingFees - 1
	p2wpkhUnspent.Amount = float64(v) / 1e8
	node.rawRes[methodListUnspent] = mustMarshal(t, unspents)
	coins, _, err := wallet.FundOrder(ord)
	if err != nil {
		t.Fatalf("error when skipping split tx because not enough to cover baggage: %v", err)
	}
	if coins[0].Value() != v {
		t.Fatalf("split performed when baggage wasn't covered")
	}
	// Now get the split.
	v = swapVal + backingFees
	p2wpkhUnspent.Amount = float64(v) / 1e8
	node.rawRes[methodListUnspent] = mustMarshal(t, unspents)

	checkMax(lots, swapVal, backingFees, (totalBytes+splitTxBaggageSegwit)*estFeeRate, v)

	coins, _, err = wallet.FundOrder(ord)
	if err != nil {
		t.Fatalf("error funding split tx: %v", err)
	}
	if coins[0].Value() == v {
		t.Fatalf("split performed when baggage wasn't covered")
	}
	wallet.useSplitTx = false
}

func TestSwap(t *testing.T) {
	t.Run("segwit", func(t *testing.T) {
		testSwap(t, true)
	})
	t.Run("non-segwit", func(t *testing.T) {
		testSwap(t, false)
	})
}

func testSwap(t *testing.T, segwit bool) {
	wallet, node, shutdown := tNewWallet(segwit)
	defer shutdown()
	swapVal := toSatoshi(5)
	coins := asset.Coins{
		newOutput(tTxHash, 0, toSatoshi(3)),
		newOutput(tTxHash, 0, toSatoshi(3)),
	}
	addrStr := tP2PKHAddr
	if segwit {
		addrStr = tP2WPKHAddr
	}

	node.rawRes[methodNewAddress] = mustMarshal(t, addrStr)
	node.rawRes[methodChangeAddress] = mustMarshal(t, addrStr)

	secretHash, _ := hex.DecodeString("5124208c80d33507befa517c08ed01aa8d33adbf37ecd70fb5f9352f7a51a88d")
	contract := &asset.Contract{
		Address:    addrStr,
		Value:      swapVal,
		SecretHash: secretHash,
		LockTime:   uint64(time.Now().Unix()),
	}

	swaps := &asset.Swaps{
		Inputs:     coins,
		Contracts:  []*asset.Contract{contract},
		LockChange: true,
		FeeRate:    tBTC.MaxFeeRate,
	}

	signatureComplete := true

	// Aim for 3 signature cycles.
	sigSizer := 0
	node.signFunc = func(params []json.RawMessage) (json.RawMessage, error) {
		var sizeTweak int
		if sigSizer%2 == 0 {
			sizeTweak = -2
		}
		sigSizer++
		return signFunc(t, params, sizeTweak, signatureComplete, wallet.segwit)
	}

	// This time should succeed.
	_, changeCoin, feesPaid, err := wallet.Swap(swaps)
	if err != nil {
		t.Fatalf("swap error: %v", err)
	}

	// Make sure the change coin is locked.
	if len(node.lockedCoins) != 1 {
		t.Fatalf("did not lock change coin")
	}
	txHash, _, _ := decodeCoinID(changeCoin.ID())
	if node.lockedCoins[0].TxID != txHash.String() {
		t.Fatalf("wrong coin locked during swap")
	}

	// Fees should be returned.
	minFees := tBTC.MaxFeeRate * dexbtc.MsgTxVBytes(node.sentRawTx)
	if feesPaid < minFees {
		t.Fatalf("sent fees, %d, less than required fees, %d", feesPaid, minFees)
	}

	// Not enough funds
	swaps.Inputs = coins[:1]
	_, _, _, err = wallet.Swap(swaps)
	if err == nil {
		t.Fatalf("no error for listunspent not enough funds")
	}
	swaps.Inputs = coins

	// AddressPKH error
	node.rawErr[methodNewAddress] = tErr
	_, _, _, err = wallet.Swap(swaps)
	if err == nil {
		t.Fatalf("no error for getnewaddress rpc error")
	}
	node.rawErr[methodNewAddress] = nil

	// ChangeAddress error
	node.rawErr[methodChangeAddress] = tErr
	_, _, _, err = wallet.Swap(swaps)
	if err == nil {
		t.Fatalf("no error for getrawchangeaddress rpc error")
	}
	node.rawErr[methodChangeAddress] = nil

	// SignTx error
	node.rawErr[methodSignTx] = tErr
	_, _, _, err = wallet.Swap(swaps)
	if err == nil {
		t.Fatalf("no error for signrawtransactionwithwallet rpc error")
	}
	node.rawErr[methodSignTx] = nil

	// incomplete signatures
	signatureComplete = false
	_, _, _, err = wallet.Swap(swaps)
	if err == nil {
		t.Fatalf("no error for incomplete signature rpc error")
	}
	signatureComplete = true

	// Make sure we can succeed again.
	_, _, _, err = wallet.Swap(swaps)
	if err != nil {
		t.Fatalf("re-swap error: %v", err)
	}
}

type TAuditInfo struct{}

func (ai *TAuditInfo) Recipient() string     { return tP2PKHAddr }
func (ai *TAuditInfo) Expiration() time.Time { return time.Time{} }
func (ai *TAuditInfo) Coin() asset.Coin      { return &tCoin{} }
func (ai *TAuditInfo) Contract() dex.Bytes   { return nil }
func (ai *TAuditInfo) SecretHash() dex.Bytes { return nil }

func TestRedeem(t *testing.T) {
	t.Run("segwit", func(t *testing.T) {
		testRedeem(t, true)
	})
	t.Run("non-segwit", func(t *testing.T) {
		testRedeem(t, false)
	})
}

func testRedeem(t *testing.T, segwit bool) {
	wallet, node, shutdown := tNewWallet(segwit)
	defer shutdown()
	swapVal := toSatoshi(5)
	secret := randBytes(32)
	secretHash := sha256.Sum256(secret)
	lockTime := time.Now().Add(time.Hour * 12)
	addrStr := tP2PKHAddr
	if segwit {
		addrStr = tP2WPKHAddr
	}

	contract, err := dexbtc.MakeContract(addrStr, addrStr, secretHash[:], lockTime.Unix(), segwit, &chaincfg.MainNetParams)
	if err != nil {
		t.Fatalf("error making swap contract: %v", err)
	}

	addr, _ := btcutil.DecodeAddress(tP2PKHAddr, &chaincfg.MainNetParams)
	ci := &auditInfo{
		output:     newOutput(tTxHash, 0, swapVal),
		contract:   contract,
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

	node.rawRes[methodChangeAddress] = mustMarshal(t, addrStr)
	node.rawRes[methodPrivKeyForAddress] = mustMarshal(t, wif.String())

	_, _, feesPaid, err := wallet.Redeem([]*asset.Redemption{redemption})
	if err != nil {
		t.Fatalf("redeem error: %v", err)
	}

	// Check that fees are returned.
	minFees := optimalFeeRate * dexbtc.MsgTxVBytes(node.sentRawTx)
	if feesPaid < minFees {
		t.Fatalf("sent fees, %d, less than expected minimum fees, %d", feesPaid, minFees)
	}

	// No audit info
	redemption.Spends = nil
	_, _, _, err = wallet.Redeem([]*asset.Redemption{redemption})
	if err == nil {
		t.Fatalf("no error for nil AuditInfo")
	}
	redemption.Spends = ci

	// Spoofing AuditInfo is not allowed.
	redemption.Spends = &TAuditInfo{}
	_, _, _, err = wallet.Redeem([]*asset.Redemption{redemption})
	if err == nil {
		t.Fatalf("no error for spoofed AuditInfo")
	}
	redemption.Spends = ci

	// Wrong secret hash
	redemption.Secret = randBytes(32)
	_, _, _, err = wallet.Redeem([]*asset.Redemption{redemption})
	if err == nil {
		t.Fatalf("no error for wrong secret")
	}
	redemption.Secret = secret

	// too low of value
	ci.output.value = 200
	_, _, _, err = wallet.Redeem([]*asset.Redemption{redemption})
	if err == nil {
		t.Fatalf("no error for redemption not worth the fees")
	}
	ci.output.value = swapVal

	// Change address error
	node.rawErr[methodChangeAddress] = tErr
	_, _, _, err = wallet.Redeem([]*asset.Redemption{redemption})
	if err == nil {
		t.Fatalf("no error for change address error")
	}
	node.rawErr[methodChangeAddress] = nil

	// Missing priv key error
	node.rawErr[methodPrivKeyForAddress] = tErr
	_, _, _, err = wallet.Redeem([]*asset.Redemption{redemption})
	if err == nil {
		t.Fatalf("no error for missing private key")
	}
	node.rawErr[methodPrivKeyForAddress] = nil

	// Send error
	node.sendErr = tErr
	_, _, _, err = wallet.Redeem([]*asset.Redemption{redemption})
	if err == nil {
		t.Fatalf("no error for send error")
	}
	node.sendErr = nil

	// Wrong hash
	var h chainhash.Hash
	h[0] = 0x01
	node.sendHash = &h
	_, _, _, err = wallet.Redeem([]*asset.Redemption{redemption})
	if err == nil {
		t.Fatalf("no error for wrong return hash")
	}
	node.sendHash = nil
}

func TestSignMessage(t *testing.T) {
	wallet, node, shutdown := tNewWallet(true)
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

	pt := newOutPoint(tTxHash, vout)
	utxo := &utxo{address: tP2PKHAddr}
	wallet.fundingCoins[pt] = utxo
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

	var coin asset.Coin = newOutput(tTxHash, vout, 5e7)
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
	delete(wallet.fundingCoins, pt)
	_, _, err = wallet.SignMessage(coin, msg)
	if err == nil {
		t.Fatalf("no error for unknown utxo")
	}
	wallet.fundingCoins[pt] = utxo

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
	t.Run("segwit", func(t *testing.T) {
		testAuditContract(t, true)
	})
	t.Run("non-segwit", func(t *testing.T) {
		testAuditContract(t, false)
	})
}

func testAuditContract(t *testing.T, segwit bool) {
	wallet, node, shutdown := tNewWallet(segwit)
	defer shutdown()
	vout := uint32(5)
	swapVal := toSatoshi(5)
	secretHash, _ := hex.DecodeString("5124208c80d33507befa517c08ed01aa8d33adbf37ecd70fb5f9352f7a51a88d")
	lockTime := time.Now().Add(time.Hour * 12)
	addrStr := tP2PKHAddr
	if segwit {
		addrStr = tP2WPKHAddr
	}

	contract, err := dexbtc.MakeContract(addrStr, addrStr, secretHash, lockTime.Unix(), segwit, &chaincfg.MainNetParams)
	if err != nil {
		t.Fatalf("error making swap contract: %v", err)
	}

	var contractAddr btcutil.Address
	if segwit {
		h := sha256.Sum256(contract)
		contractAddr, _ = btcutil.NewAddressWitnessScriptHash(h[:], &chaincfg.MainNetParams)
	} else {
		contractAddr, _ = btcutil.NewAddressScriptHash(contract, &chaincfg.MainNetParams)
	}
	pkScript, _ := txscript.PayToAddrScript(contractAddr)

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
	if audit.Recipient() != addrStr {
		t.Fatalf("wrong recipient. wanted '%s', got '%s'", addrStr, audit.Recipient())
	}
	if !bytes.Equal(audit.Contract(), contract) {
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
	contract   []byte
	expiration uint64
}

func (r *tReceipt) Expiration() time.Time { return time.Unix(int64(r.expiration), 0).UTC() }
func (r *tReceipt) Coin() asset.Coin      { return r.coin }
func (r *tReceipt) Contract() dex.Bytes   { return r.contract }

func TestFindRedemption(t *testing.T) {
	t.Run("segwit", func(t *testing.T) {
		testFindRedemption(t, true)
	})
	t.Run("non-segwit", func(t *testing.T) {
		testFindRedemption(t, false)
	})
}

func testFindRedemption(t *testing.T, segwit bool) {
	node := newTRPCClient()
	cfg := &BTCCloneCFG{
		WalletCFG: &asset.WalletConfig{
			TipChange: func(error) {},
		},
		Symbol:             "btc",
		Logger:             tLogger,
		ChainParams:        &chaincfg.MainNetParams,
		WalletInfo:         WalletInfo,
		DefaultFallbackFee: defaultFee,
		Segwit:             segwit,
	}
	wallet := newWallet(cfg, &dexbtc.Config{}, node)
	wallet.currentTip = &block{} // since we're not using Connect, run checkForNewBlocks after adding blocks

	contractHeight := node.GetBestBlockHeight() + 1
	contractTxid := "e1b7c47df70d7d8f4c9c26f8ba9a59102c10885bd49024d32fdef08242f0c26c"
	contractTxHash, _ := chainhash.NewHashFromStr(contractTxid)
	otherTxid := "7a7b3b5c3638516bc8e7f19b4a3dec00f052a599fed5036c2b89829de2367bb6"
	contractVout := uint32(1)
	coinID := toCoinID(contractTxHash, contractVout)

	secret := randBytes(32)
	secretHash := sha256.Sum256(secret)

	addrStr := tP2PKHAddr
	if segwit {
		addrStr = tP2WPKHAddr
	}

	lockTime := time.Now().Add(time.Hour * 12)
	contract, err := dexbtc.MakeContract(addrStr, addrStr, secretHash[:], lockTime.Unix(), segwit, &chaincfg.MainNetParams)
	if err != nil {
		t.Fatalf("error making swap contract: %v", err)
	}
	contractAddr, _ := wallet.scriptHashAddress(contract)
	pkScript, _ := txscript.PayToAddrScript(contractAddr)

	otherAddr, _ := btcutil.DecodeAddress(addrStr, &chaincfg.MainNetParams)
	otherScript, _ := txscript.PayToAddrScript(otherAddr)

	var redemptionWitness, otherWitness [][]byte
	var redemptionSigScript, otherSigScript []byte
	if segwit {
		redemptionWitness = dexbtc.RedeemP2WSHContract(contract, randBytes(73), randBytes(33), secret)
		otherWitness = [][]byte{randBytes(73), randBytes(33)}
	} else {
		redemptionSigScript, _ = dexbtc.RedeemP2SHContract(contract, randBytes(73), randBytes(33), secret)
		otherSigScript, _ = txscript.NewScriptBuilder().
			AddData(randBytes(73)).
			AddData(randBytes(33)).
			Script()
	}

	// Prepare the "blockchain"
	inputs := []btcjson.Vin{makeRPCVin(otherTxid, 0, otherSigScript, otherWitness)}
	// Add the contract transaction. Put the pay-to-contract script at index 1.
	blockHash, _ := node.addRawTx(contractHeight, makeRawTx(contractTxid, []dex.Bytes{otherScript, pkScript}, inputs))
	txHex, err := makeTxHex([]dex.Bytes{otherScript, pkScript}, inputs)
	if err != nil {
		t.Fatalf("error generating hex for contract tx: %v", err)
	}
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
		Hex: txHex,
	}
	node.rawRes[methodGetTransaction] = mustMarshal(t, getTxRes)

	// Add an intermediate block for good measure.
	node.addRawTx(contractHeight+1, makeRawTx(otherTxid, []dex.Bytes{otherScript}, inputs))

	// Now add the redemption.
	rpcVin := makeRPCVin(contractTxid, contractVout, redemptionSigScript, redemptionWitness)
	inputs = append(inputs, rpcVin)
	_, redeemBlock := node.addRawTx(contractHeight+2, makeRawTx(otherTxid, []dex.Bytes{otherScript}, inputs))
	redeemVin := &redeemBlock.RawTx[0].Vin[1]

	// Update currentTip from "RPC". Normally run() would do this.
	wallet.checkForNewBlocks()

	// Check find redemption result.
	_, checkSecret, err := wallet.FindRedemption(tCtx, coinID)
	if err != nil {
		t.Fatalf("error finding redemption: %v", err)
	}
	if !bytes.Equal(checkSecret, secret) {
		t.Fatalf("wrong secret. expected %x, got %x", secret, checkSecret)
	}

	// gettransaction error
	node.rawErr[methodGetTransaction] = tErr
	_, _, err = wallet.FindRedemption(tCtx, coinID)
	if err == nil {
		t.Fatalf("no error for gettransaction rpc error")
	}
	node.rawErr[methodGetTransaction] = nil

	// timeout finding missing redemption
	redeemVin.Txid = otherTxid
	ctx, cancel := context.WithTimeout(tCtx, 500*time.Millisecond) // 0.5 seconds is long enough
	defer cancel()
	_, k, err := wallet.FindRedemption(ctx, coinID)
	if ctx.Err() == nil || k != nil {
		// Expected ctx to cancel after timeout and no secret should be found.
		t.Fatalf("unexpected result for missing redemption: secret: %v, err: %v", k, err)
	}
	redeemVin.Txid = contractTxid

	// Canceled context
	deadCtx, cancelCtx := context.WithCancel(tCtx)
	cancelCtx()
	_, _, err = wallet.FindRedemption(deadCtx, coinID)
	if err == nil {
		t.Fatalf("no error for canceled context")
	}

	// Expect FindRedemption to error because of bad input sig.
	node.blockchainMtx.Lock()
	redeemVin.Witness = []string{string(randBytes(100))}
	redeemVin.ScriptSig.Hex = hex.EncodeToString(randBytes(100))

	node.blockchainMtx.Unlock()
	_, _, err = wallet.FindRedemption(tCtx, coinID)
	if err == nil {
		t.Fatalf("no error for wrong redemption")
	}
	node.blockchainMtx.Lock()
	redeemVin.Witness = rpcVin.Witness
	redeemVin.ScriptSig.Hex = hex.EncodeToString(redemptionSigScript)
	node.blockchainMtx.Unlock()

	// Wrong script type for contract output
	getTxRes.Hex, err = makeTxHex([]dex.Bytes{otherScript, otherScript}, inputs)
	if err != nil {
		t.Fatalf("makeTxHex: %v", err)
	}
	node.rawRes[methodGetTransaction] = mustMarshal(t, getTxRes)
	_, _, err = wallet.FindRedemption(tCtx, coinID)
	if err == nil {
		t.Fatalf("no error for wrong script type")
	}
	getTxRes.Hex = txHex
	node.rawRes[methodGetTransaction] = mustMarshal(t, getTxRes)

	// Sanity check to make sure it passes again.
	_, _, err = wallet.FindRedemption(tCtx, coinID)
	if err != nil {
		t.Fatalf("error after clearing errors: %v", err)
	}
}

func TestRefund(t *testing.T) {
	t.Run("segwit", func(t *testing.T) {
		testRefund(t, true)
	})
	t.Run("non-segwit", func(t *testing.T) {
		testRefund(t, false)
	})
}

func testRefund(t *testing.T, segwit bool) {
	wallet, node, shutdown := tNewWallet(segwit)
	defer shutdown()

	secret := randBytes(32)
	secretHash := sha256.Sum256(secret)
	lockTime := time.Now().Add(time.Hour * 12)

	addrStr := tP2PKHAddr
	if segwit {
		addrStr = tP2WPKHAddr
	}

	contract, err := dexbtc.MakeContract(addrStr, addrStr, secretHash[:], lockTime.Unix(), segwit, &chaincfg.MainNetParams)
	if err != nil {
		t.Fatalf("error making swap contract: %v", err)
	}

	bigTxOut := newTxOutResult(nil, 1e8, 2)
	node.txOutRes = bigTxOut
	node.rawRes[methodChangeAddress] = mustMarshal(t, addrStr)

	privBytes, _ := hex.DecodeString("b07209eec1a8fb6cfe5cb6ace36567406971a75c330db7101fb21bc679bc5330")
	privKey, _ := btcec.PrivKeyFromBytes(btcec.S256(), privBytes)
	wif, err := btcutil.NewWIF(privKey, &chaincfg.MainNetParams, true)
	if err != nil {
		t.Fatalf("error encoding wif: %v", err)
	}
	node.rawRes[methodPrivKeyForAddress] = mustMarshal(t, wif.String())

	contractOutput := newOutput(tTxHash, 0, 1e8)
	_, err = wallet.Refund(contractOutput.ID(), contract)
	if err != nil {
		t.Fatalf("refund error: %v", err)
	}

	// Invalid coin
	badReceipt := &tReceipt{
		coin: &tCoin{id: make([]byte, 15)},
	}
	_, err = wallet.Refund(badReceipt.coin.id, badReceipt.Contract())
	if err == nil {
		t.Fatalf("no error for bad receipt")
	}

	// gettxout error
	node.txOutErr = tErr
	_, err = wallet.Refund(contractOutput.ID(), contract)
	if err == nil {
		t.Fatalf("no error for missing utxo")
	}
	node.txOutErr = nil

	// bad contract
	badContractOutput := newOutput(tTxHash, 0, 1e8)
	badContract := randBytes(50)
	_, err = wallet.Refund(badContractOutput.ID(), badContract)
	if err == nil {
		t.Fatalf("no error for bad contract")
	}

	// Too small.
	node.txOutRes = newTxOutResult(nil, 100, 2)
	_, err = wallet.Refund(contractOutput.ID(), contract)
	if err == nil {
		t.Fatalf("no error for value < fees")
	}
	node.txOutRes = bigTxOut

	// getrawchangeaddress error
	node.rawErr[methodChangeAddress] = tErr
	_, err = wallet.Refund(contractOutput.ID(), contract)
	if err == nil {
		t.Fatalf("no error for getrawchangeaddress rpc error")
	}
	node.rawErr[methodChangeAddress] = nil

	// signature error
	node.rawErr[methodPrivKeyForAddress] = tErr
	_, err = wallet.Refund(contractOutput.ID(), contract)
	if err == nil {
		t.Fatalf("no error for dumpprivkey rpc error")
	}
	node.rawErr[methodPrivKeyForAddress] = nil

	// send error
	node.sendErr = tErr
	_, err = wallet.Refund(contractOutput.ID(), contract)
	if err == nil {
		t.Fatalf("no error for sendrawtransaction rpc error")
	}
	node.sendErr = nil

	// bad checkhash
	var badHash chainhash.Hash
	badHash[0] = 0x05
	node.sendHash = &badHash
	_, err = wallet.Refund(contractOutput.ID(), contract)
	if err == nil {
		t.Fatalf("no error for tx hash")
	}
	node.sendHash = nil

	// Sanity check that we can succeed again.
	_, err = wallet.Refund(contractOutput.ID(), contract)
	if err != nil {
		t.Fatalf("re-refund error: %v", err)
	}
}

func TestLockUnlock(t *testing.T) {
	wallet, node, shutdown := tNewWallet(true)
	defer shutdown()

	// just checking that the errors come through.
	node.rawRes[methodUnlock] = mustMarshal(t, true)
	node.rawRes[methodLockUnspent] = []byte(`true`)
	err := wallet.Unlock("pass")
	if err != nil {
		t.Fatalf("unlock error: %v", err)
	}
	node.rawErr[methodUnlock] = tErr
	err = wallet.Unlock("pass")
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
	wallet, node, shutdown := tNewWallet(true)
	sender := func(addr string, val uint64) (asset.Coin, error) {
		return wallet.PayFee(addr, val)
	}
	if senderType == tWithdrawSender {
		sender = func(addr string, val uint64) (asset.Coin, error) {
			return wallet.Withdraw(addr, val)
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
		Safe:          true,
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
	wallet, node, shutdown := tNewWallet(true)
	defer shutdown()

	coinID := make([]byte, 36)
	copy(coinID[:32], tTxHash[:])

	// Bad coin id
	_, spent, err := wallet.Confirmations(context.Background(), randBytes(35))
	if err == nil {
		t.Fatalf("no error for bad coin ID")
	}
	if spent {
		t.Fatalf("spent is non-zero for non-nil error")
	}

	// Short path.
	node.txOutRes = &btcjson.GetTxOutResult{
		Confirmations: 2,
	}
	confs, spent, err := wallet.Confirmations(context.Background(), coinID)
	if err != nil {
		t.Fatalf("error for gettransaction path: %v", err)
	}
	if confs != 2 {
		t.Fatalf("confs not retrieved from gettxout path. expected 2, got %d", confs)
	}
	if spent {
		t.Fatalf("expected spent = false for gettxout path, got true")
	}

	// gettransaction error
	node.txOutRes = nil
	_, spent, err = wallet.Confirmations(context.Background(), coinID)
	if err == nil {
		t.Fatalf("no error for gettransaction error")
	}
	if spent {
		t.Fatalf("spent is non-zero with gettransaction error")
	}
	node.rawErr[methodGetTransaction] = nil

	node.rawRes[methodGetTransaction] = mustMarshal(t, &GetTransactionResult{})
	_, spent, err = wallet.Confirmations(context.Background(), coinID)
	if err != nil {
		t.Fatalf("coin error: %v", err)
	}
	if !spent {
		t.Fatalf("expected spent = true for gettransaction path, got false")
	}
}

func TestSendEdges(t *testing.T) {
	t.Run("segwit", func(t *testing.T) {
		testSendEdges(t, true)
	})
	t.Run("non-segwit", func(t *testing.T) {
		testSendEdges(t, false)
	})
}

func testSendEdges(t *testing.T, segwit bool) {
	wallet, node, shutdown := tNewWallet(segwit)
	defer shutdown()

	const feeRate uint64 = 3

	const swapVal = 2e8 // leaving untyped. NewTxOut wants int64

	var addr, contractAddr btcutil.Address
	var dexReqFees, dustCoverage uint64

	if segwit {
		addr, _ = btcutil.DecodeAddress(tP2WPKHAddr, &chaincfg.MainNetParams)
		contractAddr, _ = btcutil.NewAddressWitnessScriptHash(randBytes(32), &chaincfg.MainNetParams)
		// See dexbtc.IsDust for the source of this dustCoverage voodoo.
		dustCoverage = (dexbtc.P2WPKHOutputSize + 41 + (107 / 4)) * feeRate * 3
		dexReqFees = dexbtc.InitTxSizeSegwit * feeRate
	} else {
		addr, _ = btcutil.DecodeAddress(tP2PKHAddr, &chaincfg.MainNetParams)
		contractAddr, _ = btcutil.NewAddressScriptHash(randBytes(20), &chaincfg.MainNetParams)
		dustCoverage = (dexbtc.P2PKHOutputSize + 41 + 107) * feeRate * 3
		dexReqFees = dexbtc.InitTxSize * feeRate
	}

	pkScript, _ := txscript.PayToAddrScript(contractAddr)

	newBaseTx := func() *wire.MsgTx {
		baseTx := wire.NewMsgTx(wire.TxVersion)
		baseTx.AddTxIn(wire.NewTxIn(new(wire.OutPoint), nil, nil))
		baseTx.AddTxOut(wire.NewTxOut(swapVal, pkScript))
		return baseTx
	}

	node.signFunc = func(params []json.RawMessage) (json.RawMessage, error) {
		return signFunc(t, params, 0, true, wallet.segwit)
	}

	tests := []struct {
		name      string
		funding   uint64
		expChange bool
	}{
		{
			name:    "not enough for change output",
			funding: swapVal + dexReqFees - 1,
		},
		{
			// Still dust here, but a different path.
			name:    "exactly enough for change output",
			funding: swapVal + dexReqFees,
		},
		{
			name:    "more than enough for change output but still dust",
			funding: swapVal + dexReqFees + 1,
		},
		{
			name:    "1 atom short to not be dust",
			funding: swapVal + dexReqFees + dustCoverage - 1,
		},
		{
			name:      "exactly enough to not be dust",
			funding:   swapVal + dexReqFees + dustCoverage,
			expChange: true,
		},
	}

	for _, tt := range tests {
		tx, _, _, err := wallet.sendWithReturn(newBaseTx(), addr, tt.funding, swapVal, feeRate)
		if err != nil {
			t.Fatalf("sendWithReturn error: %v", err)
		}

		if len(tx.TxOut) == 1 && tt.expChange {
			t.Fatalf("%s: no change added", tt.name)
		} else if len(tx.TxOut) == 2 && !tt.expChange {
			t.Fatalf("%s: change output added for dust. Output value = %d", tt.name, tx.TxOut[1].Value)
		}
	}
}

func TestSyncStatus(t *testing.T) {
	wallet, node, shutdown := tNewWallet(false)
	defer shutdown()
	node.rawRes[methodGetBlockchainInfo] = mustMarshal(t, &getBlockchainInfoResult{
		Headers: 100,
		Blocks:  99,
	})

	synced, progress, err := wallet.SyncStatus()
	if err != nil {
		t.Fatalf("SyncStatus error (synced expected): %v", err)
	}
	if !synced {
		t.Fatalf("synced = false for 1 block to go")
	}
	if progress < 1 {
		t.Fatalf("progress not complete when loading last block")
	}

	node.rawErr[methodGetBlockchainInfo] = tErr
	_, _, err = wallet.SyncStatus()
	if err == nil {
		t.Fatalf("SyncStatus error not propagated")
	}
	node.rawErr[methodGetBlockchainInfo] = nil

	wallet.tipAtConnect = 100
	node.rawRes[methodGetBlockchainInfo] = mustMarshal(t, &getBlockchainInfoResult{
		Headers: 200,
		Blocks:  150,
	})
	synced, progress, err = wallet.SyncStatus()
	if err != nil {
		t.Fatalf("SyncStatus error (half-synced): %v", err)
	}
	if synced {
		t.Fatalf("synced = true for 50 blocks to go")
	}
	if progress > 0.500001 || progress < 0.4999999 {
		t.Fatalf("progress out of range. Expected 0.5, got %.2f", progress)
	}
}
