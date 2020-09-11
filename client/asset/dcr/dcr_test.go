// +build !harness

package dcr

import (
	"bytes"
	"context"
	"crypto/sha256"
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
	dexdcr "decred.org/dcrdex/dex/networks/dcr"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v2"
	"github.com/decred/dcrd/dcrec"
	"github.com/decred/dcrd/dcrec/secp256k1/v2"
	"github.com/decred/dcrd/dcrutil/v2"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types/v2"
	"github.com/decred/dcrd/rpcclient/v5"
	"github.com/decred/dcrd/txscript/v2"
	"github.com/decred/dcrd/wire"
	walletjson "github.com/decred/dcrwallet/rpc/jsonrpc/types"
)

var (
	tLogger  dex.Logger
	tCtx     context.Context
	tLotSize uint64 = 1e7
	tDCR            = &dex.Asset{
		ID:           42,
		Symbol:       "dcr",
		SwapSize:     dexdcr.InitTxSize,
		SwapSizeBase: dexdcr.InitTxSizeBase,
		MaxFeeRate:   24, // FundOrder and swap/redeem fallback when estimation fails
		LotSize:      tLotSize,
		RateStep:     100,
		SwapConf:     1,
	}
	optimalFeeRate uint64 = 22
	tErr                  = fmt.Errorf("test error")
	tTxID                 = "308e9a3675fc3ea3862b7863eeead08c621dcc37ff59de597dd3cdab41450ad9"
	tTxHash        *chainhash.Hash
	tPKHAddr       dcrutil.Address
	tP2PKHScript   []byte
)

func randBytes(l int) []byte {
	b := make([]byte, l)
	rand.Read(b)
	return b
}

func makeGetTxOutRes(confs, lots int64, pkScript []byte) *chainjson.GetTxOutResult {
	val := dcrutil.Amount(lots * int64(tLotSize)).ToCoin()
	return &chainjson.GetTxOutResult{
		Confirmations: confs,
		Value:         val,
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

func makeTxHex(txid string, pkScripts []dex.Bytes, inputs []chainjson.Vin) (string, error) {
	msgTx := wire.NewMsgTx()
	for _, input := range inputs {
		prevOutHash, err := chainhash.NewHashFromStr(input.Txid)
		if err != nil {
			return "", err
		}
		prevOut := wire.NewOutPoint(prevOutHash, input.Vout, input.Tree)
		amountIn, err := dcrutil.NewAmount(input.AmountIn)
		if err != nil {
			return "", err
		}
		sigScript, err := hex.DecodeString(input.ScriptSig.Hex)
		if err != nil {
			return "", err
		}
		txIn := wire.NewTxIn(prevOut, int64(amountIn), sigScript)
		msgTx.AddTxIn(txIn)
	}
	for _, pkScript := range pkScripts {
		txOut := wire.NewTxOut(100000000, pkScript)
		msgTx.AddTxOut(txOut)
	}
	txBuf := bytes.NewBuffer(make([]byte, 0, msgTx.SerializeSize()))
	err := msgTx.Serialize(txBuf)
	if err != nil {
		return "", err
	}
	txHex, err := txBuf.Bytes(), nil
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(txHex), nil
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
	walletCfg := &asset.WalletConfig{
		Settings: map[string]string{
			"account": "default",
		},
		TipChange: func(error) {},
	}
	walletCtx, shutdown := context.WithCancel(tCtx)
	wallet := unconnectedWallet(walletCfg, &Config{}, tLogger)
	wallet.node = client

	// Initialize the best block.
	wallet.tipMtx.Lock()
	wallet.currentTip, _ = wallet.getBestBlock()
	wallet.tipMtx.Unlock()

	go wallet.monitorBlocks(walletCtx)

	return wallet, client, shutdown
}

type tRPCClient struct {
	sendRawHash    *chainhash.Hash
	sendRawErr     error
	sentRawTx      *wire.MsgTx
	txOutRes       map[outPoint]*chainjson.GetTxOutResult
	txOutErr       error
	bestBlockErr   error
	mempool        []*chainhash.Hash
	mempoolErr     error
	rawTx          *chainjson.TxRawResult
	rawTxErr       error
	unspent        []walletjson.ListUnspentResult
	unspentErr     error
	balanceResult  *walletjson.GetBalanceResult
	balanceErr     error
	lockUnspentErr error
	changeAddr     dcrutil.Address
	changeAddrErr  error
	newAddr        dcrutil.Address
	newAddrErr     error
	signFunc       func(tx *wire.MsgTx) (*wire.MsgTx, bool, error)
	privWIF        *dcrutil.WIF
	privWIFErr     error
	walletTx       *walletjson.GetTransactionResult
	walletTxErr    error
	lockErr        error
	passErr        error
	disconnected   bool
	rawRes         map[string]json.RawMessage
	rawErr         map[string]error
	blockchainMtx  sync.RWMutex
	verboseBlocks  map[string]*chainjson.GetBlockVerboseResult
	mainchain      map[int64]*chainhash.Hash
	lluCoins       []walletjson.ListUnspentResult // Returned from ListLockUnspent
	lockedCoins    []*wire.OutPoint               // Last submitted to LockUnspent
	listLockedErr  error
}

func defaultSignFunc(tx *wire.MsgTx) (*wire.MsgTx, bool, error) { return tx, true, nil }

func newTRPCClient() *tRPCClient {
	// setup genesis block, required by bestblock polling goroutine
	var newHash chainhash.Hash
	copy(newHash[:], randBytes(32))
	return &tRPCClient{
		txOutRes: make(map[outPoint]*chainjson.GetTxOutResult),
		verboseBlocks: map[string]*chainjson.GetBlockVerboseResult{
			newHash.String(): {},
		},
		mainchain: map[int64]*chainhash.Hash{
			0: &newHash,
		},
		signFunc: defaultSignFunc,
		rawRes:   make(map[string]json.RawMessage),
		rawErr:   make(map[string]error),
	}
}

func (c *tRPCClient) EstimateSmartFee(confirmations int64, mode chainjson.EstimateSmartFeeMode) (float64, error) {
	optimalRate := float64(optimalFeeRate) * 1e-5
	// fmt.Println((float64(optimalFeeRate)*1e-5)-0.00022)
	return optimalRate, nil // optimalFeeRate: 22 atoms/byte = 0.00022 DCR/KB * 1e8 atoms/DCR * 1e-3 KB/Byte
}

func (c *tRPCClient) SendRawTransaction(tx *wire.MsgTx, allowHighFees bool) (*chainhash.Hash, error) {
	c.sentRawTx = tx
	if c.sendRawErr == nil && c.sendRawHash == nil {
		h := tx.TxHash()
		return &h, nil
	}
	return c.sendRawHash, c.sendRawErr
}

func (c *tRPCClient) GetTxOut(txHash *chainhash.Hash, vout uint32, mempool bool) (*chainjson.GetTxOutResult, error) {
	return c.txOutRes[newOutPoint(txHash, vout)], c.txOutErr
}

func (c *tRPCClient) GetBestBlock() (*chainhash.Hash, int64, error) {
	if c.bestBlockErr != nil {
		return nil, -1, c.bestBlockErr
	}
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
	return bestHash, bestBlkHeight, nil
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

func (c *tRPCClient) GetBlockVerbose(blockHash *chainhash.Hash, verboseTx bool) (*chainjson.GetBlockVerboseResult, error) {
	c.blockchainMtx.RLock()
	defer c.blockchainMtx.RUnlock()
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
	c.blockchainMtx.Lock()
	defer c.blockchainMtx.Unlock()
	blkHash, found := c.mainchain[blockHeight]
	if !found {
		var newHash chainhash.Hash
		copy(newHash[:], randBytes(32))
		blkHash = &newHash
		prevBlockHash := c.mainchain[blockHeight-1]
		// Modifying c.verboseBlocks[prevBlockHash.String()].NextHash causes
		// race errors.
		prevBlock := *c.verboseBlocks[prevBlockHash.String()]
		prevBlock.NextHash = blkHash.String()
		c.verboseBlocks[prevBlockHash.String()] = &prevBlock
		c.verboseBlocks[newHash.String()] = &chainjson.GetBlockVerboseResult{
			Height:       blockHeight,
			Hash:         blkHash.String(),
			PreviousHash: prevBlockHash.String(),
		}
		c.mainchain[blockHeight] = blkHash
	}
	block := c.verboseBlocks[blkHash.String()]
	block.RawTx = append(block.RawTx, *tx)
	return blkHash, block
}

func (c *tRPCClient) GetBalanceMinConf(account string, minConfirms int) (*walletjson.GetBalanceResult, error) {
	return c.balanceResult, c.balanceErr
}

func (c *tRPCClient) LockUnspent(unlock bool, ops []*wire.OutPoint) error {
	if unlock == false {
		c.lockedCoins = ops
	}
	return c.lockUnspentErr
}

func (c *tRPCClient) GetRawChangeAddress(account string, net dcrutil.AddressParams) (dcrutil.Address, error) {
	return c.changeAddr, c.changeAddrErr
}

func (c *tRPCClient) GetNewAddressGapPolicy(account string, gapPolicy rpcclient.GapPolicy, net dcrutil.AddressParams) (dcrutil.Address, error) {
	return c.newAddr, c.newAddrErr
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

func (c *tRPCClient) RawRequest(method string, params []json.RawMessage) (json.RawMessage, error) {
	switch method {
	case methodListUnspent:
		if c.unspentErr != nil {
			return nil, c.unspentErr
		}

		var acct string
		if len(params) > 3 {
			// filter with provided acct param
			_ = json.Unmarshal(params[3], &acct)
		}
		allAccts := acct == "" || acct == "*"

		var unspents []walletjson.ListUnspentResult
		for _, unspent := range c.unspent {
			if allAccts || unspent.Account == acct {
				unspents = append(unspents, unspent)
			}
		}
		response, _ := json.Marshal(unspents)
		return response, nil

	case methodListLockUnspent:
		if c.listLockedErr != nil {
			return nil, c.listLockedErr
		}

		var acct string
		if len(params) > 0 {
			_ = json.Unmarshal(params[0], &acct)
		}
		allAccts := acct == "" || acct == "*"

		var locked []chainjson.TransactionInput
		for _, utxo := range c.lluCoins {
			if allAccts || utxo.Account == acct {
				locked = append(locked, chainjson.TransactionInput{
					Txid:   utxo.TxID,
					Amount: utxo.Amount,
					Vout:   utxo.Vout,
					Tree:   utxo.Tree,
				})
			}
		}
		response, _ := json.Marshal(locked)
		return response, nil

	case methodSignRawTransaction:
		if len(params) != 1 {
			return nil, fmt.Errorf("needed 1 param")
		}

		var msgTxHex string
		err := json.Unmarshal(params[0], &msgTxHex)
		if err != nil {
			return nil, err
		}

		msgTx, err := msgTxFromHex(msgTxHex)
		if err != nil {
			res := walletjson.SignRawTransactionResult{
				Hex: msgTxHex,
				Errors: []walletjson.SignRawTransactionError{
					{
						TxID:  msgTx.CachedTxHash().String(),
						Error: err.Error(),
					},
				},
				// Complete stays false.
			}
			return json.Marshal(&res)
		}

		if c.signFunc == nil {
			return nil, fmt.Errorf("no signFunc configured")
		}

		signedTx, complete, err := c.signFunc(msgTx)
		if err != nil {
			res := walletjson.SignRawTransactionResult{
				Hex: msgTxHex,
				Errors: []walletjson.SignRawTransactionError{
					{
						TxID:  msgTx.CachedTxHash().String(),
						Error: err.Error(),
					},
				},
				// Complete stays false.
			}
			return json.Marshal(&res)
		}

		txHex, err := msgTxToHex(signedTx)
		if err != nil {
			return nil, fmt.Errorf("failed to encode MsgTx: %w", err)
		}

		res := walletjson.SignRawTransactionResult{
			Hex:      txHex,
			Complete: complete,
		}
		return json.Marshal(&res)
	}

	if rr, found := c.rawRes[method]; found {
		return rr, c.rawErr[method] // err probably should be nil, but respect the config
	}
	if re, found := c.rawErr[method]; found {
		return nil, re
	}

	return nil, fmt.Errorf("method %v not implemented by (*tRPCClient).RawRequest", method)
}

func TestMain(m *testing.M) {
	chainParams = chaincfg.MainNetParams()
	tPKHAddr, _ = dcrutil.DecodeAddress("DsTya4cCFBgtofDLiRhkyPYEQjgs3HnarVP", chainParams)
	tLogger = dex.StdOutLogger("TEST", dex.LevelTrace)
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
	balanceResult := &walletjson.GetBalanceResult{
		Balances: []walletjson.GetAccountBalanceResult{
			{
				AccountName: wallet.acct,
			},
		},
	}
	node.balanceResult = balanceResult
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

	var vout uint32
	addUtxo := func(atomAmt uint64, confs int64, lock bool) {
		utxo := walletjson.ListUnspentResult{
			TxID:          tTxID,
			Vout:          vout,
			Address:       tPKHAddr.String(),
			Account:       wallet.acct,
			Amount:        float64(atomAmt) / 1e8,
			Confirmations: confs,
			ScriptPubKey:  hex.EncodeToString(tP2PKHScript),
		}
		if lock {
			node.lluCoins = append(node.lluCoins, utxo)
		} else {
			unspents = append(unspents, utxo)
			node.unspent = unspents
		}
		// update balance
		balanceResult.Balances[0].Spendable += utxo.Amount
		vout++
	}

	// Add 1 unspent output and check balance
	var littleLots uint64 = 6
	littleOrder := tLotSize * littleLots
	littleFunds := calc.RequiredOrderFunds(littleOrder, dexdcr.P2PKHInputSize, littleLots, tDCR)
	addUtxo(littleFunds, 0, false)
	bal, err = wallet.Balance()
	if err != nil {
		t.Fatalf("error for 1 utxo: %v", err)
	}
	if bal.Available != littleFunds {
		t.Fatalf("expected available = %d for confirmed utxos, got %d", littleFunds, bal.Available)
	}
	if bal.Immature != 0 {
		t.Fatalf("expected immature = %d, got %d", 0, bal.Immature)
	}
	if bal.Locked != 0 {
		t.Fatalf("expected locked = %d, got %d", 0, bal.Locked)
	}

	// Add a second utxo, lock it and check balance.
	lockedBit := tLotSize * 2
	addUtxo(lockedBit, 1, true)
	bal, err = wallet.Balance()
	if err != nil {
		t.Fatalf("error for 2 utxos: %v", err)
	}
	// Available balance should exclude locked utxo amount.
	if bal.Available != littleFunds {
		t.Fatalf("expected available = %d for confirmed utxos, got %d", littleFunds, bal.Available)
	}
	if bal.Immature != 0 {
		t.Fatalf("expected immature = %d, got %d", 0, bal.Immature)
	}
	if bal.Locked != lockedBit {
		t.Fatalf("expected locked = %d, got %d", lockedBit, bal.Locked)
	}

	// Add a third utxo.
	var lottaLots uint64 = 100
	lottaOrder := tLotSize * 100
	// Add funding for an extra input to accommodate the later combined tests.
	lottaFunds := calc.RequiredOrderFunds(lottaOrder, 2*dexdcr.P2PKHInputSize, lottaLots, tDCR)
	addUtxo(lottaFunds, 1, false)
	bal, err = wallet.Balance()
	if err != nil {
		t.Fatalf("error for 3 utxos: %v", err)
	}
	if bal.Available != littleFunds+lottaFunds {
		t.Fatalf("expected available = %d for 2 outputs, got %d", littleFunds+lottaFunds, bal.Available)
	}
	if bal.Immature != 0 {
		t.Fatalf("expected unconf = 0 for 2 outputs, got %d", bal.Immature)
	}
	// locked balance should remain same as utxo2 amount.
	if bal.Locked != lockedBit {
		t.Fatalf("expected locked = %d, got %d", lockedBit, bal.Locked)
	}

	ord := &asset.Order{
		Value:        0,
		MaxSwapCount: 1,
		DEXConfig:    tDCR,
	}

	setOrderValue := func(v uint64) {
		ord.Value = v
		ord.MaxSwapCount = v / tDCR.LotSize
	}

	// Zero value
	_, _, err = wallet.FundOrder(ord)
	if err == nil {
		t.Fatalf("no funding error for zero value")
	}

	// Nothing to spend
	node.unspent = nil
	setOrderValue(littleOrder)
	_, _, err = wallet.FundOrder(ord)
	if err == nil {
		t.Fatalf("no error for zero utxos")
	}
	node.unspent = unspents

	// RPC error
	node.unspentErr = tErr
	_, _, err = wallet.FundOrder(ord)
	if err == nil {
		t.Fatalf("no funding error for rpc error")
	}
	node.unspentErr = nil

	// Negative response when locking outputs.
	node.lockUnspentErr = tErr
	_, _, err = wallet.FundOrder(ord)
	if err == nil {
		t.Fatalf("no error for lockunspent result = false: %v", err)
	}
	node.lockUnspentErr = nil

	// Fund a little bit, but small output is unconfirmed.
	spendables, _, err := wallet.FundOrder(ord)
	if err != nil {
		t.Fatalf("error funding small amount: %v", err)
	}
	if len(spendables) != 1 {
		t.Fatalf("expected 1 spendable, got %d", len(spendables))
	}
	v := spendables[0].Value()
	if v != lottaFunds {
		t.Fatalf("expected spendable of value %d, got %d", lottaFunds, v)
	}

	// Now confirm the little bit and have it selected.
	unspents[0].Confirmations++
	spendables, _, err = wallet.FundOrder(ord)
	if err != nil {
		t.Fatalf("error funding small amount: %v", err)
	}
	if len(spendables) != 1 {
		t.Fatalf("expected 1 spendable, got %d", len(spendables))
	}
	v = spendables[0].Value()
	if v != littleFunds {
		t.Fatalf("expected spendable of value %d, got %d", littleFunds, v)
	}

	// Fund a lotta bit.
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

	extraLottaOrder := littleOrder + lottaOrder
	extraLottaLots := littleLots + lottaLots
	// Prepare for a split transaction.
	baggageFees := tDCR.MaxFeeRate * splitTxBaggage
	node.changeAddr = tPKHAddr
	wallet.useSplitTx = true
	// No split performed due to economics is not an error.
	setOrderValue(extraLottaOrder)
	coins, _, err := wallet.FundOrder(ord)
	if err != nil {
		t.Fatalf("error for no-split split: %v", err)
	}
	// Should be both coins.
	if len(coins) != 2 {
		t.Fatalf("no-split split didn't return both coins")
	}

	// Not enough to cover transaction fees.
	tweak := float64(littleFunds+lottaFunds-calc.RequiredOrderFunds(extraLottaOrder, 2*dexdcr.P2PKHInputSize, extraLottaLots, tDCR)+1) / 1e8
	node.unspent[0].Amount -= tweak
	setOrderValue(extraLottaOrder)
	_, _, err = wallet.FundOrder(ord)
	if err == nil {
		t.Fatalf("no error when not enough to cover tx fees")
	}
	node.unspent[0].Amount += tweak

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
	node.unspent[1].Amount += float64(baggageFees) / 1e8
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

	// Hit some error paths.

	// GetRawChangeAddress error
	node.changeAddrErr = tErr
	_, _, err = wallet.FundOrder(ord)
	if err == nil {
		t.Fatalf("no error for split tx change addr error")
	}
	node.changeAddrErr = nil

	// SendRawTx error
	node.sendRawErr = tErr
	_, _, err = wallet.FundOrder(ord)
	if err == nil {
		t.Fatalf("no error for split tx send error")
	}
	node.sendRawErr = nil

	// Success again.
	_, _, err = wallet.FundOrder(ord)
	if err != nil {
		t.Fatalf("error for split tx recovery run")
	}

	// Not enough funds, because littleUnspent is a different account.
	unspents[0].Account = "wrong account"
	setOrderValue(extraLottaOrder)
	_, _, err = wallet.FundOrder(ord)
	if err == nil {
		t.Fatalf("no error for wrong account")
	}
	node.unspent[0].Account = wallet.acct

	// Place locked utxo in different account, check locked balance.
	node.lluCoins[0].Account = "wrong account"
	bal, err = wallet.Balance()
	if err != nil {
		t.Fatalf("error for 3 utxos, with locked utxo in wrong acct: %v", err)
	}
	if bal.Locked != 0 {
		t.Fatalf("expected locked = %d, got %d", 0, bal.Locked)
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
func (c *tCoin) String() string                 { return hex.EncodeToString(c.id) }
func (c *tCoin) Value() uint64                  { return 100 }
func (c *tCoin) Confirmations() (uint32, error) { return 2, nil }

func TestReturnCoins(t *testing.T) {
	wallet, node, shutdown := tNewWallet()
	defer shutdown()

	// Test it with the local output type.
	coins := asset.Coins{
		newOutput(node, tTxHash, 0, 1, wire.TxTreeRegular),
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

	node.txOutRes[newOutPoint(tTxHash, 0)] = makeGetTxOutRes(1, 1, tP2PKHScript)
	err = wallet.ReturnCoins(coins)
	if err != nil {
		t.Fatalf("error with custom coin type: %v", err)
	}
}

func TestFundingCoins(t *testing.T) {
	wallet, node, shutdown := tNewWallet()
	defer shutdown()

	vout := uint32(123)
	coinID := toCoinID(tTxHash, vout)
	p2pkhUnspent := walletjson.ListUnspentResult{
		TxID:    tTxID,
		Vout:    vout,
		Address: tPKHAddr.String(),
		Account: wallet.acct,
	}

	node.unspent = []walletjson.ListUnspentResult{p2pkhUnspent}
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

	// Check initial success.
	ensureGood()

	// Clear the RPC coins, but add a coin to the cache.
	node.unspent = nil
	opID := newOutPoint(tTxHash, vout)
	wallet.fundingCoins[opID] = &fundingCoin{
		op: newOutput(node, tTxHash, vout, 0, 0),
	}
	ensureGood()

	ensureErr := func(tag string) {
		_, err := wallet.FundingCoins(coinIDs)
		if err == nil {
			t.Fatalf("%s: no error", tag)
		}
	}

	// No coins
	delete(wallet.fundingCoins, opID)
	ensureErr("no coins")
	node.unspent = []walletjson.ListUnspentResult{p2pkhUnspent}

	// Bad coin ID
	ogIDs := coinIDs
	coinIDs = []dex.Bytes{randBytes(35)}
	ensureErr("bad coin ID")
	coinIDs = ogIDs

	// listunspent error
	node.unspentErr = tErr
	ensureErr("listunpent")
	node.unspentErr = nil

	ensureGood()
}

func TestFundEdges(t *testing.T) {
	wallet, node, shutdown := tNewWallet()
	defer shutdown()
	swapVal := uint64(1e8)
	lots := swapVal / tDCR.LotSize

	// Swap fees
	//
	// fee_rate: 24 atoms / byte (dex MaxFeeRate)
	// swap_size: 251 bytes
	// swap_size_base: 85 bytes (251 - 166 p2pkh input)
	// lot_size: 1e7
	// swap_value: 1e8
	//  lots = swap_size / lot_size = 10
	//  base_tx_bytes = (lots - 1) * swap_size + swap_size_base = 9 * 251 + 85 = 2344
	//  base_fees = 56256
	//  backing_bytes: 1x P2PKH inputs = dexdcr.P2PKHInputSize = 166 bytes
	//  backing_fees: 166 * fee_rate(24 atoms/byte) = 3984 atoms
	//  total_bytes  = base_tx_bytes + backing_bytes = 2344 + 166 = 2510
	// total_fees: base_fees + backing_fees = 56256 + 3984 = 60240 atoms
	//          OR total_bytes * fee_rate = 2510 * 24 = 60240
	fees := uint64(2510) * tDCR.MaxFeeRate
	p2pkhUnspent := walletjson.ListUnspentResult{
		TxID:          tTxID,
		Address:       tPKHAddr.String(),
		Account:       wallet.acct,
		Amount:        float64(swapVal+fees-1) / 1e8, // one atom less than needed
		Confirmations: 5,
		ScriptPubKey:  hex.EncodeToString(tP2PKHScript),
	}

	node.unspent = []walletjson.ListUnspentResult{p2pkhUnspent}
	ord := &asset.Order{
		Value:        swapVal,
		MaxSwapCount: lots,
		DEXConfig:    tDCR,
	}
	_, _, err := wallet.FundOrder(ord)
	if err == nil {
		t.Fatalf("no error when not enough funds in single p2pkh utxo")
	}
	// Now add the needed atoms and try again.
	p2pkhUnspent.Amount = float64(swapVal+fees) / 1e8
	node.unspent = []walletjson.ListUnspentResult{p2pkhUnspent}
	_, _, err = wallet.FundOrder(ord)
	if err != nil {
		t.Fatalf("should be enough to fund with a single p2pkh utxo: %v", err)
	}

	// TODO: fix the p2sh test so that the redeem script is a p2pk pkScript or a
	// multisig pkScript, not a p2pkh pkScript.

	// P2SH pkScript size = 23 bytes
	// P2PK pkScript (the redeem script) = 35 bytes
	// P2SH redeem input size = overhead(58) + sigScript((1 + 73 + 1) + (1 + 33 + 1)) +
	//   p2sh pkScript length(23) = 191 bytes vs 166 for P2PKH
	// backing_fees: 191 bytes * fee_rate(24) = 4584 atoms
	// total bytes: 2344 + 191 = 2535
	// total: 56256 + 4584 = 60840 atoms
	//     OR (2344 + 191) * 24 = 60840
	// p2shRedeem, _ := hex.DecodeString("76a914" + "db1755408acd315baa75c18ebbe0e8eaddf64a97" + "88ac") // (p2pkh! pkScript) 1+1+1+20+1+1 =25 bytes
	// scriptAddr := "DcsJEKNF3dQwcSozroei5FRPsbPEmMuWRaj"
	// p2shScriptPubKey, _ := hex.DecodeString("a914" + "3ff6a24a50135f69be9ffed744443da08408fc1a" + "87") // 1 + 1 + 20 + 1 = 23 bytes
	// fees = 2535 * tDCR.MaxFeeRate
	// halfSwap := swapVal / 2
	// p2shUnspent := walletjson.ListUnspentResult{
	// 	TxID:          tTxID,
	// 	Address:       scriptAddr,
	// 	Amount:        float64(halfSwap) / 1e8,
	// 	Confirmations: 10,
	// 	ScriptPubKey:  hex.EncodeToString(p2shScriptPubKey),
	// 	RedeemScript:  hex.EncodeToString(p2shRedeem),
	// }
	// p2pkhUnspent.Amount = float64(halfSwap+fees-1) / 1e8
	// node.unspent = []walletjson.ListUnspentResult{p2pkhUnspent, p2shUnspent}
	// _, err = wallet.FundOrder(swapVal, false, tDCR)
	// if err == nil {
	// 	t.Fatalf("no error when not enough funds in two utxos")
	// }
	// p2pkhUnspent.Amount = float64(halfSwap+fees) / 1e8
	// node.unspent = []walletjson.ListUnspentResult{p2pkhUnspent, p2shUnspent}
	// _, err = wallet.FundOrder(swapVal, false, tDCR)
	// if err != nil {
	// 	t.Fatalf("error when should be enough funding in two utxos: %v", err)
	// }
}

func TestSwap(t *testing.T) {
	wallet, node, shutdown := tNewWallet()
	defer shutdown()
	swapVal := toAtoms(5)
	coins := asset.Coins{
		newOutput(node, tTxHash, 0, toAtoms(3), wire.TxTreeRegular),
		newOutput(node, tTxHash, 0, toAtoms(3), wire.TxTreeRegular),
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

	swaps := &asset.Swaps{
		Inputs:     coins,
		Contracts:  []*asset.Contract{contract},
		LockChange: true,
		FeeRate:    tDCR.MaxFeeRate,
	}

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
	// reset the signFunc after this test so captured variables are free
	defer func() { node.signFunc = defaultSignFunc }()

	// This time should succeed.
	_, changeCoin, feesPaid, err := wallet.Swap(swaps)
	if err != nil {
		t.Fatalf("swap error: %v", err)
	}

	// Make sure the change coin is locked.
	if len(node.lockedCoins) != 1 {
		t.Fatalf("change coin not locked")
	}
	txHash, _, _ := decodeCoinID(changeCoin.ID())
	if txHash.String() != node.lockedCoins[0].Hash.String() {
		t.Fatalf("wrong coin locked during swap")
	}

	// Fees should be returned.
	minFees := tDCR.MaxFeeRate * uint64(node.sentRawTx.SerializeSize())
	if feesPaid <= minFees {
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
	node.newAddrErr = tErr
	_, _, _, err = wallet.Swap(swaps)
	if err == nil {
		t.Fatalf("no error for getnewaddress rpc error")
	}
	node.newAddrErr = nil

	// ChangeAddress error
	node.changeAddrErr = tErr
	_, _, _, err = wallet.Swap(swaps)
	if err == nil {
		t.Fatalf("no error for getrawchangeaddress rpc error")
	}
	node.changeAddrErr = nil

	// SignTx error
	node.signFunc = func(msgTx *wire.MsgTx) (*wire.MsgTx, bool, error) {
		return nil, false, tErr
	}
	_, _, _, err = wallet.Swap(swaps)
	if err == nil {
		t.Fatalf("no error for signrawtransactionwithwallet rpc error")
	}

	// incomplete signatures
	node.signFunc = func(msgTx *wire.MsgTx) (*wire.MsgTx, bool, error) {
		return msgTx, false, nil
	}
	_, _, _, err = wallet.Swap(swaps)
	if err == nil {
		t.Fatalf("no error for incomplete signature rpc error")
	}
	node.signFunc = signFunc

	// Make sure we can succeed again.
	_, _, _, err = wallet.Swap(swaps)
	if err != nil {
		t.Fatalf("re-swap error: %v", err)
	}
}

type TAuditInfo struct{}

func (ai *TAuditInfo) Recipient() string     { return tPKHAddr.String() }
func (ai *TAuditInfo) Expiration() time.Time { return time.Time{} }
func (ai *TAuditInfo) Coin() asset.Coin      { return &tCoin{} }
func (ai *TAuditInfo) Contract() dex.Bytes   { return nil }
func (ai *TAuditInfo) SecretHash() dex.Bytes { return nil }

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
		output:     newOutput(node, tTxHash, 0, swapVal, wire.TxTreeRegular),
		contract:   contract,
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

	_, _, feesPaid, err := wallet.Redeem([]*asset.Redemption{redemption})
	if err != nil {
		t.Fatalf("redeem error: %v", err)
	}

	// Check that fees are returned.
	minFees := optimalFeeRate * uint64(node.sentRawTx.SerializeSize())
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
	node.changeAddrErr = tErr
	_, _, _, err = wallet.Redeem([]*asset.Redemption{redemption})
	if err == nil {
		t.Fatalf("no error for change address error")
	}
	node.changeAddrErr = nil

	// Missing priv key error
	node.privWIFErr = tErr
	_, _, _, err = wallet.Redeem([]*asset.Redemption{redemption})
	if err == nil {
		t.Fatalf("no error for missing private key")
	}
	node.privWIFErr = nil

	// Send error
	node.sendRawErr = tErr
	_, _, _, err = wallet.Redeem([]*asset.Redemption{redemption})
	if err == nil {
		t.Fatalf("no error for send error")
	}
	node.sendRawErr = nil

	// Wrong hash
	var h chainhash.Hash
	h[0] = 0x01
	node.sendRawHash = &h
	_, _, _, err = wallet.Redeem([]*asset.Redemption{redemption})
	if err == nil {
		t.Fatalf("no error for wrong return hash")
	}
	node.sendRawHash = nil
}

const (
	txCatReceive = "recv"
	txCatSend    = "send"
	//txCatGenerate = "generate"
)

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

	op := newOutput(node, tTxHash, vout, 5e7, wire.TxTreeRegular)

	wallet.fundingCoins[op.pt] = &fundingCoin{
		addr: tPKHAddr.String(),
	}

	check := func() {
		pubkeys, sigs, err := wallet.SignMessage(op, msg)
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
	}

	check()
	delete(wallet.fundingCoins, op.pt)
	txOut := makeGetTxOutRes(1, 5, nil)
	txOut.ScriptPubKey.Addresses = []string{tPKHAddr.String()}
	node.txOutRes[newOutPoint(tTxHash, vout)] = txOut
	check()

	// gettransaction error
	node.txOutErr = tErr
	_, _, err = wallet.SignMessage(op, msg)
	if err == nil {
		t.Fatalf("no error for gettxout rpc error")
	}
	node.txOutErr = nil

	// dumpprivkey error
	node.privWIFErr = tErr
	_, _, err = wallet.SignMessage(op, msg)
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
}

func TestAuditContract(t *testing.T) {
	wallet, node, shutdown := tNewWallet()
	defer shutdown()
	vout := uint32(5)
	secretHash, _ := hex.DecodeString("5124208c80d33507befa517c08ed01aa8d33adbf37ecd70fb5f9352f7a51a88d")
	lockTime := time.Now().Add(time.Hour * 12)
	addrStr := tPKHAddr.String()
	contract, err := dexdcr.MakeContract(addrStr, addrStr, secretHash, lockTime.Unix(), chainParams)
	if err != nil {
		t.Fatalf("error making swap contract: %v", err)
	}
	addr, _ := dcrutil.NewAddressScriptHash(contract, chainParams)
	pkScript, _ := txscript.PayToAddrScript(addr)

	node.txOutRes[newOutPoint(tTxHash, vout)] = makeGetTxOutRes(1, 5, pkScript)

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
	wrongAddr, _ := dcrutil.NewAddressPubKeyHash(pkh, chainParams, dcrec.STEcdsaSecp256k1)
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
	wallet, node, shutdown := tNewWallet()
	defer shutdown()

	_, bestBlockHeight, err := node.GetBestBlock()
	if err != nil {
		t.Fatalf("unexpected GetBestBlock error: %v", err)
	}
	contractHeight := bestBlockHeight + 1
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
	blockHash, _ := node.addRawTx(contractHeight, makeRawTx(contractTxid, []dex.Bytes{otherScript, pkScript}, inputs))
	txHex, err := makeTxHex(contractTxid, []dex.Bytes{otherScript, pkScript}, inputs)
	if err != nil {
		t.Fatalf("error generating hex for contract tx: %v", err)
	}
	node.walletTx = &walletjson.GetTransactionResult{
		BlockHash:     blockHash.String(),
		Confirmations: 1,
		Details: []walletjson.GetTransactionDetailsResult{
			{
				Address:  contractAddr.String(),
				Category: txCatSend,
				Vout:     contractVout,
			},
		},
		Hex: txHex,
	}

	// Add an intermediate block for good measure.
	node.addRawTx(contractHeight+1, makeRawTx(otherTxid, []dex.Bytes{otherScript}, inputs))

	// Now add the redemption.
	inputs = append(inputs, makeRPCVin(contractTxid, contractVout, redemptionScript))
	rawRedeem := makeRawTx(otherTxid, []dex.Bytes{otherScript}, inputs)
	_, redeemBlock := node.addRawTx(contractHeight+2, rawRedeem)

	// Check find redemption result.
	_, checkSecret, err := wallet.FindRedemption(tCtx, coinID)
	if err != nil {
		t.Fatalf("error finding redemption: %v", err)
	}
	if !bytes.Equal(checkSecret, secret) {
		t.Fatalf("wrong secret. expected %x, got %x", secret, checkSecret)
	}

	// gettransaction error
	node.walletTxErr = tErr
	_, _, err = wallet.FindRedemption(tCtx, coinID)
	if err == nil {
		t.Fatalf("no error for gettransaction rpc error")
	}
	node.walletTxErr = nil

	// missing redemption
	redeemBlock.RawTx[0].Vin[1].Txid = otherTxid
	ctx, cancel := context.WithTimeout(tCtx, 2*time.Second)
	defer cancel() // ctx should auto-cancel after 2 seconds, but this is apparently good practice to prevent leak
	_, k, err := wallet.FindRedemption(ctx, coinID)
	if ctx.Err() == nil || k != nil {
		// Expected ctx to cancel after timeout and no secret should be found.
		t.Fatalf("unexpected result for missing redemption: secret: %v, err: %v", k, err)
	}
	redeemBlock.RawTx[0].Vin[1].Txid = contractTxid

	// Canceled context
	deadCtx, cancelCtx := context.WithCancel(tCtx)
	cancelCtx()
	_, _, err = wallet.FindRedemption(deadCtx, coinID)
	if err == nil {
		t.Fatalf("no error for canceled context")
	}

	// Expect FindRedemption to error because of bad input sig.
	redeemBlock.RawTx[0].Vin[1].ScriptSig.Hex = hex.EncodeToString(randBytes(100))
	_, _, err = wallet.FindRedemption(tCtx, coinID)
	if err == nil {
		t.Fatalf("no error for wrong redemption")
	}
	redeemBlock.RawTx[0].Vin[1].ScriptSig.Hex = hex.EncodeToString(redemptionScript)

	// Wrong script type for output
	node.walletTx.Hex, _ = makeTxHex(contractTxid, []dex.Bytes{otherScript, otherScript}, inputs)
	_, _, err = wallet.FindRedemption(tCtx, coinID)
	if err == nil {
		t.Fatalf("no error for wrong script type")
	}
	node.walletTx.Hex = txHex

	// Sanity check to make sure it passes again.
	_, _, err = wallet.FindRedemption(tCtx, coinID)
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
	bigOutID := newOutPoint(tTxHash, 0)
	node.txOutRes[bigOutID] = bigTxOut
	node.changeAddr = tPKHAddr
	node.newAddr = tPKHAddr

	privBytes, _ := hex.DecodeString("b07209eec1a8fb6cfe5cb6ace36567406971a75c330db7101fb21bc679bc5330")
	privKey, _ := secp256k1.PrivKeyFromBytes(privBytes)
	node.privWIF = dcrutil.NewWIF(privKey, chainParams.PrivateKeyID, dcrec.STEcdsaSecp256k1)

	contractOutput := newOutput(node, tTxHash, 0, 1e8, wire.TxTreeRegular)
	_, err = wallet.Refund(contractOutput.ID(), contract)
	if err != nil {
		t.Fatalf("refund error: %v", err)
	}

	// Invalid coin
	badReceipt := &tReceipt{
		coin: &tCoin{id: make([]byte, 15)},
	}
	_, err = wallet.Refund(badReceipt.coin.id, badReceipt.contract)
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
	badContractOutput := newOutput(node, tTxHash, 0, 1e8, wire.TxTreeRegular)
	_, err = wallet.Refund(badContractOutput.ID(), randBytes(50))
	if err == nil {
		t.Fatalf("no error for bad contract")
	}

	// Too small.
	node.txOutRes[bigOutID] = newTxOutResult(nil, 100, 2)
	_, err = wallet.Refund(contractOutput.ID(), contract)
	if err == nil {
		t.Fatalf("no error for value < fees")
	}
	node.txOutRes[bigOutID] = bigTxOut

	// signature error
	node.privWIFErr = tErr
	_, err = wallet.Refund(contractOutput.ID(), contract)
	if err == nil {
		t.Fatalf("no error for dumpprivkey rpc error")
	}
	node.privWIFErr = nil

	// send error
	node.sendRawErr = tErr
	_, err = wallet.Refund(contractOutput.ID(), contract)
	if err == nil {
		t.Fatalf("no error for sendrawtransaction rpc error")
	}
	node.sendRawErr = nil

	// bad checkhash
	var badHash chainhash.Hash
	badHash[0] = 0x05
	node.sendRawHash = &badHash
	_, err = wallet.Refund(contractOutput.ID(), contract)
	if err == nil {
		t.Fatalf("no error for tx hash")
	}
	node.sendRawHash = nil

	// Sanity check that we can succeed again.
	_, err = wallet.Refund(contractOutput.ID(), contract)
	if err != nil {
		t.Fatalf("re-refund error: %v", err)
	}
}

type tSenderType byte

const (
	tPayFeeSender tSenderType = iota
	tWithdrawSender
)

func testSender(t *testing.T, senderType tSenderType) {
	wallet, node, shutdown := tNewWallet()
	var sendVal uint64 = 1e8
	var unspentVal uint64 = 100e8
	sender := func(addr string, val uint64) (asset.Coin, error) {
		return wallet.PayFee(addr, val)
	}
	if senderType == tWithdrawSender {
		// For withdraw, test with unspent total = withdraw value
		unspentVal = sendVal
		sender = func(addr string, val uint64) (asset.Coin, error) {
			return wallet.Withdraw(addr, val)
		}
	}
	defer shutdown()
	addr := tPKHAddr.String()
	node.changeAddr = tPKHAddr

	node.unspent = []walletjson.ListUnspentResult{{
		TxID:          tTxID,
		Address:       tPKHAddr.String(),
		Account:       wallet.acct,
		Amount:        float64(unspentVal) / 1e8,
		Confirmations: 5,
		ScriptPubKey:  hex.EncodeToString(tP2PKHScript),
	}}

	_, err := sender(addr, sendVal)
	if err != nil {
		t.Fatalf("PayFee error: %v", err)
	}

	// invalid address
	_, err = sender("badaddr", sendVal)
	if err == nil {
		t.Fatalf("no error for bad address: %v", err)
	}

	// GetRawChangeAddress error
	node.changeAddrErr = tErr
	_, err = sender(addr, sendVal)
	if err == nil {
		t.Fatalf("no error for rawchangeaddress: %v", err)
	}
	node.changeAddrErr = nil

	// good again
	_, err = sender(addr, sendVal)
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

	// Bad coin idea
	_, err := wallet.Confirmations(randBytes(35))
	if err == nil {
		t.Fatalf("no error for bad coin ID")
	}

	// listunspent error
	node.walletTxErr = tErr
	_, err = wallet.Confirmations(coinID)
	if err == nil {
		t.Fatalf("no error for listunspent error")
	}
	node.walletTxErr = nil

	node.walletTx = &walletjson.GetTransactionResult{}
	_, err = wallet.Confirmations(coinID)
	if err != nil {
		t.Fatalf("coin error: %v", err)
	}
}
