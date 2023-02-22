//go:build !harness

package dcr

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"os"
	"sync"
	"testing"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/dex/config"
	dexdcr "decred.org/dcrdex/dex/networks/dcr"
	"decred.org/dcrwallet/v2/rpc/client/dcrwallet"
	walletjson "decred.org/dcrwallet/v2/rpc/jsonrpc/types"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrec"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
	"github.com/decred/dcrd/dcrjson/v4"
	"github.com/decred/dcrd/dcrutil/v4"
	"github.com/decred/dcrd/gcs/v3"
	"github.com/decred/dcrd/gcs/v3/blockcf2"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types/v3"
	"github.com/decred/dcrd/txscript/v4"
	"github.com/decred/dcrd/txscript/v4/stdaddr"
	"github.com/decred/dcrd/wire"
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
		MaxFeeRate:   24, // FundOrder and swap/redeem fallback when estimation fails
		SwapConf:     1,
	}
	optimalFeeRate uint64 = 22
	tErr                  = fmt.Errorf("test error")
	tTxID                 = "308e9a3675fc3ea3862b7863eeead08c621dcc37ff59de597dd3cdab41450ad9"
	tTxHash        *chainhash.Hash
	tPKHAddr       stdaddr.Address
	tP2PKHScript   []byte
	tChainParams          = chaincfg.MainNetParams()
	feeSuggestion  uint64 = 10
	tAcctName             = "tAcctName"
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

func makeRawTx(inputs []*wire.TxIn, outputScripts []dex.Bytes) *wire.MsgTx {
	tx := wire.NewMsgTx()
	for _, pkScript := range outputScripts {
		tx.TxOut = append(tx.TxOut, &wire.TxOut{
			PkScript: pkScript,
		})
	}
	tx.TxIn = inputs
	return tx
}

func makeTxHex(inputs []*wire.TxIn, pkScripts []dex.Bytes) (string, error) {
	msgTx := wire.NewMsgTx()
	msgTx.TxIn = inputs
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

func makeRPCVin(txHash *chainhash.Hash, vout uint32, sigScript []byte) *wire.TxIn {
	return &wire.TxIn{
		PreviousOutPoint: *wire.NewOutPoint(txHash, vout, 0),
		SignatureScript:  sigScript,
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

func dummyInput() *wire.TxIn {
	return wire.NewTxIn(wire.NewOutPoint(&chainhash.Hash{0x01}, 0, 0), 0, nil)
}

func dummyTx() *wire.MsgTx {
	return makeRawTx([]*wire.TxIn{dummyInput()}, []dex.Bytes{})
}

// sometimes we want to not start monitor blocks to avoid a race condition in
// case we replace the wallet.
func tNewWalletMonitorBlocks(monitorBlocks bool) (*ExchangeWallet, *tRPCClient, func(), error) {
	client := newTRPCClient()
	walletCfg := &asset.WalletConfig{
		TipChange:   func(error) {},
		PeersChange: func(uint32, error) {},
	}
	walletCtx, shutdown := context.WithCancel(tCtx)

	wallet, err := unconnectedWallet(walletCfg, &walletConfig{PrimaryAccount: tAcctName}, tChainParams, tLogger, dex.Simnet)
	if err != nil {
		shutdown()
		return nil, nil, nil, err
	}
	wallet.wallet = &rpcWallet{
		rpcClient: client,
		log:       tLogger.SubLogger("trpc"),
	}
	wallet.ctx = walletCtx

	// Initialize the best block.
	wallet.tipMtx.Lock()
	wallet.currentTip, _ = wallet.getBestBlock(walletCtx)
	wallet.tipMtx.Unlock()

	if monitorBlocks {
		go wallet.monitorBlocks(walletCtx)
	}

	return wallet, client, shutdown, nil

}

func tNewWallet() (*ExchangeWallet, *tRPCClient, func(), error) {
	return tNewWalletMonitorBlocks(true)
}

func signFunc(msgTx *wire.MsgTx, scriptSize int) (*wire.MsgTx, bool, error) {
	for i := range msgTx.TxIn {
		msgTx.TxIn[i].SignatureScript = randBytes(scriptSize)
	}
	return msgTx, true, nil
}

type tRPCClient struct {
	sendRawHash    *chainhash.Hash
	sendRawErr     error
	sentRawTx      *wire.MsgTx
	txOutRes       map[outPoint]*chainjson.GetTxOutResult
	txOutErr       error
	bestBlockErr   error
	mempoolErr     error
	rawTxErr       error
	unspent        []walletjson.ListUnspentResult
	unspentErr     error
	balanceResult  *walletjson.GetBalanceResult
	balanceErr     error
	lockUnspentErr error
	changeAddr     stdaddr.Address
	changeAddrErr  error
	newAddr        stdaddr.Address
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
	blockchain     *tBlockchain
	lluCoins       []walletjson.ListUnspentResult // Returned from ListLockUnspent
	lockedCoins    []*wire.OutPoint               // Last submitted to LockUnspent
	listLockedErr  error
	estFeeErr      error
}

type wireTxWithHeight struct {
	tx     *wire.MsgTx
	height int64
}

type tBlockchain struct {
	mtx               sync.RWMutex
	rawTxs            map[chainhash.Hash]*wireTxWithHeight
	mainchain         map[int64]*chainhash.Hash
	verboseBlocks     map[chainhash.Hash]*wire.MsgBlock
	blockHeaders      map[chainhash.Hash]*wire.BlockHeader
	v2CFilterBuilders map[chainhash.Hash]*tV2CFilterBuilder
}

func (blockchain *tBlockchain) addRawTx(blockHeight int64, tx *wire.MsgTx) (*chainhash.Hash, *wire.MsgBlock) {
	blockchain.mtx.Lock()
	defer blockchain.mtx.Unlock()

	blockchain.rawTxs[tx.TxHash()] = &wireTxWithHeight{tx, blockHeight}

	if blockHeight < 0 {
		return nil, nil
	}

	prevBlockHash := blockchain.mainchain[blockHeight-1]

	// Mined tx. Add to block.
	block := blockchain.blockAt(blockHeight)
	if block == nil {
		block = &wire.MsgBlock{
			Header: wire.BlockHeader{
				PrevBlock: *prevBlockHash,
				Height:    uint32(blockHeight),
				VoteBits:  1,
			},
		}
	}
	block.Transactions = append(block.Transactions, tx)
	blockHash := block.BlockHash()
	blockFilterBuilder := blockchain.v2CFilterBuilders[blockHash]
	blockchain.mainchain[blockHeight] = &blockHash
	blockchain.verboseBlocks[blockHash] = block

	blockchain.blockHeaders[blockHash] = &block.Header

	// Save prevout and output scripts in block cfilters.
	if blockFilterBuilder == nil {
		blockFilterBuilder = &tV2CFilterBuilder{}
		copy(blockFilterBuilder.key[:], randBytes(16))

	}
	blockchain.v2CFilterBuilders[blockHash] = blockFilterBuilder
	for _, txIn := range tx.TxIn {
		prevOut := &txIn.PreviousOutPoint
		prevTx, found := blockchain.rawTxs[prevOut.Hash]
		if !found || len(prevTx.tx.TxOut) <= int(prevOut.Index) {
			continue
		}
		blockFilterBuilder.data.AddRegularPkScript(prevTx.tx.TxOut[prevOut.Index].PkScript)
	}
	for _, txOut := range tx.TxOut {
		blockFilterBuilder.data.AddRegularPkScript(txOut.PkScript)
	}

	return &blockHash, block
}

// blockchain.mtx lock should be held for writes.
func (blockchain *tBlockchain) blockAt(height int64) *wire.MsgBlock {
	blkHash, found := blockchain.mainchain[height]
	if found {
		return blockchain.verboseBlocks[*blkHash]
	}

	return nil
}

type tV2CFilterBuilder struct {
	data blockcf2.Entries
	key  [gcs.KeySize]byte
}

func (filterBuilder *tV2CFilterBuilder) build() (*gcs.FilterV2, error) {
	return gcs.NewFilterV2(blockcf2.B, blockcf2.M, filterBuilder.key, filterBuilder.data)
}

func defaultSignFunc(tx *wire.MsgTx) (*wire.MsgTx, bool, error) { return tx, true, nil }

func newTRPCClient() *tRPCClient {
	// setup genesis block, required by bestblock polling goroutine
	var newHash chainhash.Hash
	copy(newHash[:], randBytes(32))
	return &tRPCClient{
		txOutRes: make(map[outPoint]*chainjson.GetTxOutResult),
		blockchain: &tBlockchain{
			rawTxs: map[chainhash.Hash]*wireTxWithHeight{},
			mainchain: map[int64]*chainhash.Hash{
				0: &newHash,
			},
			verboseBlocks: map[chainhash.Hash]*wire.MsgBlock{
				newHash: {},
			},
			blockHeaders: map[chainhash.Hash]*wire.BlockHeader{
				newHash: {},
			},
			v2CFilterBuilders: map[chainhash.Hash]*tV2CFilterBuilder{},
		},
		signFunc: defaultSignFunc,
		rawRes:   make(map[string]json.RawMessage),
		rawErr:   make(map[string]error),
	}
}

func (c *tRPCClient) GetCurrentNet(context.Context) (wire.CurrencyNet, error) {
	return tChainParams.Net, nil
}

func (c *tRPCClient) EstimateSmartFee(_ context.Context, confirmations int64, mode chainjson.EstimateSmartFeeMode) (*chainjson.EstimateSmartFeeResult, error) {
	if c.estFeeErr != nil {
		return nil, c.estFeeErr
	}
	optimalRate := float64(optimalFeeRate) * 1e-5 // optimalFeeRate: 22 atoms/byte = 0.00022 DCR/KB * 1e8 atoms/DCR * 1e-3 KB/Byte
	// fmt.Println((float64(optimalFeeRate)*1e-5)-0.00022)
	return &chainjson.EstimateSmartFeeResult{FeeRate: optimalRate}, nil
}

func (c *tRPCClient) SendRawTransaction(_ context.Context, tx *wire.MsgTx, allowHighFees bool) (*chainhash.Hash, error) {
	c.sentRawTx = tx
	if c.sendRawErr == nil && c.sendRawHash == nil {
		h := tx.TxHash()
		return &h, nil
	}
	return c.sendRawHash, c.sendRawErr
}

func (c *tRPCClient) GetTxOut(_ context.Context, txHash *chainhash.Hash, vout uint32, tree int8, mempool bool) (*chainjson.GetTxOutResult, error) {
	return c.txOutRes[newOutPoint(txHash, vout)], c.txOutErr
}

func (c *tRPCClient) GetBestBlock(_ context.Context) (*chainhash.Hash, int64, error) {
	if c.bestBlockErr != nil {
		return nil, -1, c.bestBlockErr
	}
	bestHash, bestBlkHeight := c.getBestBlock()
	return bestHash, bestBlkHeight, nil
}

func (c *tRPCClient) getBestBlock() (*chainhash.Hash, int64) {
	c.blockchain.mtx.RLock()
	defer c.blockchain.mtx.RUnlock()
	var bestHash *chainhash.Hash
	var bestBlkHeight int64
	for height, hash := range c.blockchain.mainchain {
		if height >= bestBlkHeight {
			bestBlkHeight = height
			bestHash = hash
		}
	}
	return bestHash, bestBlkHeight
}

func (c *tRPCClient) GetBlockHash(_ context.Context, blockHeight int64) (*chainhash.Hash, error) {
	c.blockchain.mtx.RLock()
	defer c.blockchain.mtx.RUnlock()
	h, found := c.blockchain.mainchain[blockHeight]
	if !found {
		return nil, fmt.Errorf("no test block at height %d", blockHeight)
	}
	return h, nil
}

func (c *tRPCClient) GetBlock(_ context.Context, blockHash *chainhash.Hash) (*wire.MsgBlock, error) {
	c.blockchain.mtx.RLock()
	defer c.blockchain.mtx.RUnlock()
	blk, found := c.blockchain.verboseBlocks[*blockHash]
	if !found {
		return nil, fmt.Errorf("no test block found for %s", blockHash)
	}
	return blk, nil
}

func (c *tRPCClient) GetBlockHeaderVerbose(_ context.Context, blockHash *chainhash.Hash) (*chainjson.GetBlockHeaderVerboseResult, error) {
	c.blockchain.mtx.RLock()
	defer c.blockchain.mtx.RUnlock()
	hdr, found := c.blockchain.blockHeaders[*blockHash]
	if !found {
		return nil, fmt.Errorf("no test block header found for %s", blockHash)
	}
	return &chainjson.GetBlockHeaderVerboseResult{
		Height:        hdr.Height,
		Hash:          blockHash.String(),
		PreviousHash:  hdr.PrevBlock.String(),
		Confirmations: 1,  // just not -1, which indicates side chain
		NextHash:      "", // empty string signals that it is tip
	}, nil
}

func (c *tRPCClient) GetBlockHeader(_ context.Context, blockHash *chainhash.Hash) (*wire.BlockHeader, error) {
	c.blockchain.mtx.RLock()
	defer c.blockchain.mtx.RUnlock()
	hdr, found := c.blockchain.blockHeaders[*blockHash]
	if !found {
		return nil, fmt.Errorf("no test block header found for %s", blockHash)
	}
	return hdr, nil
}

func (c *tRPCClient) GetRawMempool(_ context.Context, txType chainjson.GetRawMempoolTxTypeCmd) ([]*chainhash.Hash, error) {
	if c.mempoolErr != nil {
		return nil, c.mempoolErr
	}
	c.blockchain.mtx.RLock()
	defer c.blockchain.mtx.RUnlock()
	txHashes := make([]*chainhash.Hash, 0)
	for _, tx := range c.blockchain.rawTxs {
		if tx.height < 0 {
			txHash := tx.tx.TxHash()
			txHashes = append(txHashes, &txHash)
		}
	}
	return txHashes, nil
}

func (c *tRPCClient) GetRawTransaction(_ context.Context, txHash *chainhash.Hash) (*dcrutil.Tx, error) {
	if c.rawTxErr != nil {
		return nil, c.rawTxErr
	}
	tx, found := c.blockchain.rawTxs[*txHash]
	if !found {
		return nil, dcrjson.NewRPCError(dcrjson.ErrRPCNoTxInfo, "no test raw tx "+txHash.String())
	}
	return dcrutil.NewTx(tx.tx), nil
}

func (c *tRPCClient) GetBalanceMinConf(_ context.Context, account string, minConfirms int) (*walletjson.GetBalanceResult, error) {
	return c.balanceResult, c.balanceErr
}

func (c *tRPCClient) LockUnspent(_ context.Context, unlock bool, ops []*wire.OutPoint) error {
	if unlock == false {
		c.lockedCoins = ops
	}
	return c.lockUnspentErr
}

func (c *tRPCClient) GetRawChangeAddress(_ context.Context, account string, net stdaddr.AddressParams) (stdaddr.Address, error) {
	return c.changeAddr, c.changeAddrErr
}

func (c *tRPCClient) GetNewAddressGapPolicy(_ context.Context, account string, gapPolicy dcrwallet.GapPolicy) (stdaddr.Address, error) {
	return c.newAddr, c.newAddrErr
}

func (c *tRPCClient) DumpPrivKey(_ context.Context, address stdaddr.Address) (*dcrutil.WIF, error) {
	return c.privWIF, c.privWIFErr
}

func (c *tRPCClient) GetTransaction(_ context.Context, txHash *chainhash.Hash) (*walletjson.GetTransactionResult, error) {
	if c.walletTx != nil || c.walletTxErr != nil {
		return c.walletTx, c.walletTxErr
	}
	return nil, dcrjson.NewRPCError(dcrjson.ErrRPCNoTxInfo, "no test transaction")
}

func (c *tRPCClient) AccountUnlocked(_ context.Context, acct string) (*walletjson.AccountUnlockedResult, error) {
	return &walletjson.AccountUnlockedResult{}, nil // go the walletlock/walletpassphrase route
}

func (c *tRPCClient) LockAccount(_ context.Context, acct string) error       { return nil }
func (c *tRPCClient) UnlockAccount(_ context.Context, acct, pw string) error { return nil }

func (c *tRPCClient) WalletLock(_ context.Context) error {
	return c.lockErr
}

func (c *tRPCClient) WalletPassphrase(_ context.Context, passphrase string, timeoutSecs int64) error {
	return c.passErr
}

func (c *tRPCClient) WalletInfo(_ context.Context) (*walletjson.WalletInfoResult, error) {
	return &walletjson.WalletInfoResult{
		Unlocked: true,
	}, nil
}

func (c *tRPCClient) ValidateAddress(_ context.Context, address stdaddr.Address) (*walletjson.ValidateAddressWalletResult, error) {
	return &walletjson.ValidateAddressWalletResult{
		IsMine: true,
	}, nil
}

func (c *tRPCClient) Disconnected() bool {
	return c.disconnected
}

func (c *tRPCClient) RawRequest(_ context.Context, method string, params []json.RawMessage) (json.RawMessage, error) {
	if rr, found := c.rawRes[method]; found {
		return rr, c.rawErr[method] // err probably should be nil, but respect the config
	}
	if re, found := c.rawErr[method]; found {
		return nil, re
	}

	switch method {
	case methodGetPeerInfo:
		return json.Marshal([]*walletjson.GetPeerInfoResult{
			{
				Addr: "127.0.0.1",
			},
		})
	case methodGetCFilterV2:
		if len(params) != 1 {
			return nil, fmt.Errorf("getcfilterv2 requires 1 param, got %d", len(params))
		}

		var hashStr string
		json.Unmarshal(params[0], &hashStr)
		blkHash, _ := chainhash.NewHashFromStr(hashStr)

		c.blockchain.mtx.RLock()
		defer c.blockchain.mtx.RUnlock()
		blockFilterBuilder := c.blockchain.v2CFilterBuilders[*blkHash]
		if blockFilterBuilder == nil {
			return nil, fmt.Errorf("cfilters builder not found for block %s", blkHash)
		}
		v2CFilter, err := blockFilterBuilder.build()
		if err != nil {
			return nil, err
		}
		res := &walletjson.GetCFilterV2Result{
			BlockHash: blkHash.String(),
			Filter:    hex.EncodeToString(v2CFilter.Bytes()),
			Key:       hex.EncodeToString(blockFilterBuilder.key[:]),
		}
		return json.Marshal(res)

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

	return nil, fmt.Errorf("method %v not implemented by (*tRPCClient).RawRequest", method)
}

func TestMain(m *testing.M) {
	tChainParams = chaincfg.MainNetParams()
	tPKHAddr, _ = stdaddr.DecodeAddress("DsTya4cCFBgtofDLiRhkyPYEQjgs3HnarVP", tChainParams)
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
	wallet, node, shutdown, err := tNewWallet()
	defer shutdown()
	if err != nil {
		t.Fatal(err)
	}

	// With an empty list returned, there should be no error, but the value zero
	// should be returned.
	unspents := make([]walletjson.ListUnspentResult, 0)
	node.unspent = unspents
	balanceResult := &walletjson.GetBalanceResult{
		Balances: []walletjson.GetAccountBalanceResult{
			{
				AccountName: tAcctName,
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
			Account:       tAcctName,
			Amount:        float64(atomAmt) / 1e8,
			Confirmations: confs,
			ScriptPubKey:  hex.EncodeToString(tP2PKHScript),
			Spendable:     true,
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
		Value:         0,
		MaxSwapCount:  1,
		DEXConfig:     tDCR,
		FeeSuggestion: feeSuggestion,
	}

	setOrderValue := func(v uint64) {
		ord.Value = v
		ord.MaxSwapCount = v / tLotSize
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
	wallet.config().useSplitTx = true
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
	node.unspent[0].Account = tAcctName

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
func (c *tCoin) String() string                                    { return hex.EncodeToString(c.id) }
func (c *tCoin) Value() uint64                                     { return 100 }
func (c *tCoin) Confirmations(ctx context.Context) (uint32, error) { return 2, nil }

func TestReturnCoins(t *testing.T) {
	wallet, node, shutdown, err := tNewWallet()
	defer shutdown()
	if err != nil {
		t.Fatal(err)
	}

	// Test it with the local output type.
	coins := asset.Coins{
		newOutput(tTxHash, 0, 1, wire.TxTreeRegular),
	}
	err = wallet.ReturnCoins(coins)
	if err != nil {
		t.Fatalf("error with output type coins: %v", err)
	}

	// Should error for no coins.
	err = wallet.ReturnCoins(asset.Coins{})
	if err == nil {
		t.Fatalf("no error for zero coins")
	}

	// nil unlocks all
	wallet.fundingCoins[outPoint{*tTxHash, 0}] = &fundingCoin{}
	err = wallet.ReturnCoins(nil)
	if err != nil {
		t.Fatalf("error for nil coins: %v", err)
	}
	if len(wallet.fundingCoins) != 0 {
		t.Errorf("all funding coins not unlocked")
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
	if err != nil {
		t.Fatalf("error with custom coin type: %v", err)
	}
}

func TestFundingCoins(t *testing.T) {
	wallet, node, shutdown, err := tNewWallet()
	defer shutdown()
	if err != nil {
		t.Fatal(err)
	}

	vout := uint32(123)
	coinID := toCoinID(tTxHash, vout)
	p2pkhUnspent := walletjson.ListUnspentResult{
		TxID:      tTxID,
		Vout:      vout,
		Address:   tPKHAddr.String(),
		Spendable: true,
		Account:   tAcctName,
	}

	node.unspent = []walletjson.ListUnspentResult{p2pkhUnspent}
	coinIDs := []dex.Bytes{coinID}

	ensureGood := func() {
		t.Helper()
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
		op: newOutput(tTxHash, vout, 0, 0),
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

func checkMaxOrder(t *testing.T, wallet *ExchangeWallet, lots, swapVal, maxFees, estWorstCase, estBestCase uint64) {
	t.Helper()
	_, maxOrder, err := wallet.maxOrder(tLotSize, feeSuggestion, tDCR)
	if err != nil {
		t.Fatalf("MaxOrder error: %v", err)
	}
	checkSwapEstimate(t, maxOrder, lots, swapVal, maxFees, estWorstCase, estBestCase)
}

func checkSwapEstimate(t *testing.T, est *asset.SwapEstimate, lots, swapVal, maxFees, estWorstCase, estBestCase uint64) {
	t.Helper()
	if est.Lots != lots {
		t.Fatalf("MaxOrder has wrong Lots. wanted %d, got %d", lots, est.Lots)
	}
	if est.Value != swapVal {
		t.Fatalf("est has wrong Value. wanted %d, got %d", swapVal, est.Value)
	}
	if est.MaxFees != maxFees {
		t.Fatalf("est has wrong MaxFees. wanted %d, got %d", maxFees, est.MaxFees)
	}
	if est.RealisticWorstCase != estWorstCase {
		t.Fatalf("MaxOrder has wrong RealisticWorstCase. wanted %d, got %d", estWorstCase, est.RealisticWorstCase)
	}
	if est.RealisticBestCase != estBestCase {
		t.Fatalf("MaxOrder has wrong RealisticBestCase. wanted %d, got %d", estBestCase, est.RealisticBestCase)
	}
}

func TestFundEdges(t *testing.T) {
	wallet, node, shutdown, err := tNewWallet()
	defer shutdown()
	if err != nil {
		t.Fatal(err)
	}
	swapVal := uint64(1e8)
	lots := swapVal / tLotSize

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
	// base_best_case_bytes = swap_size_base + (lots - 1) * swap_output_size (P2SHOutputSize) + backing_bytes
	//                      = 85 + 9*34 + 166 = 557
	const swapSize = 251
	const totalBytes = 2510
	const bestCaseBytes = 557
	const swapOutputSize = 34
	fees := uint64(totalBytes) * tDCR.MaxFeeRate
	p2pkhUnspent := walletjson.ListUnspentResult{
		TxID:          tTxID,
		Address:       tPKHAddr.String(),
		Account:       tAcctName,
		Amount:        float64(swapVal+fees-1) / 1e8, // one atom less than needed
		Confirmations: 5,
		ScriptPubKey:  hex.EncodeToString(tP2PKHScript),
		Spendable:     true,
	}

	node.unspent = []walletjson.ListUnspentResult{p2pkhUnspent}
	ord := &asset.Order{
		Value:         swapVal,
		MaxSwapCount:  lots,
		DEXConfig:     tDCR,
		FeeSuggestion: feeSuggestion,
	}

	var feeReduction uint64 = swapSize * tDCR.MaxFeeRate
	estFeeReduction := swapSize * feeSuggestion
	checkMaxOrder(t, wallet, lots-1, swapVal-tLotSize,
		fees-feeReduction,                            // max fees
		totalBytes*feeSuggestion-estFeeReduction,     // worst case
		(bestCaseBytes-swapOutputSize)*feeSuggestion) // best case

	_, _, err = wallet.FundOrder(ord)
	if err == nil {
		t.Fatalf("no error when not enough funds in single p2pkh utxo")
	}
	// Now add the needed atoms and try again.
	p2pkhUnspent.Amount = float64(swapVal+fees) / 1e8
	node.unspent = []walletjson.ListUnspentResult{p2pkhUnspent}

	checkMaxOrder(t, wallet, lots, swapVal, fees, totalBytes*feeSuggestion,
		bestCaseBytes*feeSuggestion)

	_, _, err = wallet.FundOrder(ord)
	if err != nil {
		t.Fatalf("should be enough to fund with a single p2pkh utxo: %v", err)
	}

	// For a split transaction, we would need to cover the splitTxBaggage as
	// well.
	wallet.config().useSplitTx = true
	node.changeAddr = tPKHAddr
	node.signFunc = func(msgTx *wire.MsgTx) (*wire.MsgTx, bool, error) {
		return signFunc(msgTx, dexdcr.P2PKHSigScriptSize)
	}

	fees = uint64(totalBytes+splitTxBaggage) * tDCR.MaxFeeRate
	v := swapVal + fees - 1
	node.unspent[0].Amount = float64(v) / 1e8
	coins, _, err := wallet.FundOrder(ord)
	if err != nil {
		t.Fatalf("error when skipping split tx because not enough to cover baggage: %v", err)
	}
	if coins[0].Value() != v {
		t.Fatalf("split performed when baggage wasn't covered")
	}
	// Now get the split.
	v = swapVal + fees
	node.unspent[0].Amount = float64(v) / 1e8

	checkMaxOrder(t, wallet, lots, swapVal, fees, (totalBytes+splitTxBaggage)*feeSuggestion,
		(bestCaseBytes+splitTxBaggage)*feeSuggestion)

	coins, _, err = wallet.FundOrder(ord)
	if err != nil {
		t.Fatalf("error funding split tx: %v", err)
	}
	if coins[0].Value() == v {
		t.Fatalf("split performed when baggage wasn't covered")
	}

	// Split transactions require a fee suggestion.
	// TODO:
	// 1.0: Error when no suggestion.
	// ord.FeeSuggestion = 0
	// _, _, err = wallet.FundOrder(ord)
	// if err == nil {
	// 	t.Fatalf("no error for no fee suggestions on split tx")
	// }
	ord.FeeSuggestion = tDCR.MaxFeeRate + 1
	_, _, err = wallet.FundOrder(ord)
	if err == nil {
		t.Fatalf("no error for high fee suggestions on split tx")
	}
	// Check success again.
	ord.FeeSuggestion = tDCR.MaxFeeRate
	_, _, err = wallet.FundOrder(ord)
	if err != nil {
		t.Fatalf("error fixing split tx: %v", err)
	}
	wallet.config().useSplitTx = false

	// TODO: test version mismatch
}

func TestSwap(t *testing.T) {
	wallet, node, shutdown, err := tNewWallet()
	defer shutdown()
	if err != nil {
		t.Fatal(err)
	}
	swapVal := toAtoms(5)
	coins := asset.Coins{
		newOutput(tTxHash, 0, toAtoms(3), wire.TxTreeRegular),
		newOutput(tTxHash, 0, toAtoms(3), wire.TxTreeRegular),
	}

	privBytes, _ := hex.DecodeString("b07209eec1a8fb6cfe5cb6ace36567406971a75c330db7101fb21bc679bc5330")

	node.changeAddr = tPKHAddr
	node.privWIF, err = dcrutil.NewWIF(privBytes, tChainParams.PrivateKeyID, dcrec.STEcdsaSecp256k1)
	if err != nil {
		t.Fatalf("NewWIF error: %v", err)
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
		return signFunc(msgTx, scriptSize)
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
	wallet, node, shutdown, err := tNewWallet()
	defer shutdown()
	if err != nil {
		t.Fatal(err)
	}
	swapVal := toAtoms(5)
	secret := randBytes(32)
	secretHash := sha256.Sum256(secret)
	lockTime := time.Now().Add(time.Hour * 12)
	addr := tPKHAddr.String()

	contract, err := dexdcr.MakeContract(addr, addr, secretHash[:], lockTime.Unix(), tChainParams)
	if err != nil {
		t.Fatalf("error making swap contract: %v", err)
	}

	coin := newOutput(tTxHash, 0, swapVal, wire.TxTreeRegular)

	ci := &asset.AuditInfo{
		Coin:       coin,
		Contract:   contract,
		Recipient:  tPKHAddr.String(),
		Expiration: lockTime,
	}

	redemption := &asset.Redemption{
		Spends: ci,
		Secret: secret,
	}

	privBytes, _ := hex.DecodeString("b07209eec1a8fb6cfe5cb6ace36567406971a75c330db7101fb21bc679bc5330")

	node.changeAddr = tPKHAddr
	node.privWIF, err = dcrutil.NewWIF(privBytes, tChainParams.PrivateKeyID, dcrec.STEcdsaSecp256k1)
	if err != nil {
		t.Fatalf("NewWIF error: %v", err)
	}

	redemptions := &asset.RedeemForm{
		Redemptions: []*asset.Redemption{redemption},
	}

	_, _, feesPaid, err := wallet.Redeem(redemptions)
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
	_, _, _, err = wallet.Redeem(redemptions)
	if err == nil {
		t.Fatalf("no error for nil AuditInfo")
	}
	redemption.Spends = ci

	// Spoofing AuditInfo is not allowed.
	redemption.Spends = &asset.AuditInfo{}
	_, _, _, err = wallet.Redeem(redemptions)
	if err == nil {
		t.Fatalf("no error for spoofed AuditInfo")
	}
	redemption.Spends = ci

	// Wrong secret hash
	redemption.Secret = randBytes(32)
	_, _, _, err = wallet.Redeem(redemptions)
	if err == nil {
		t.Fatalf("no error for wrong secret")
	}
	redemption.Secret = secret

	// too low of value
	coin.value = 200
	_, _, _, err = wallet.Redeem(redemptions)
	if err == nil {
		t.Fatalf("no error for redemption not worth the fees")
	}
	coin.value = swapVal

	// Change address error
	node.changeAddrErr = tErr
	_, _, _, err = wallet.Redeem(redemptions)
	if err == nil {
		t.Fatalf("no error for change address error")
	}
	node.changeAddrErr = nil

	// Missing priv key error
	node.privWIFErr = tErr
	_, _, _, err = wallet.Redeem(redemptions)
	if err == nil {
		t.Fatalf("no error for missing private key")
	}
	node.privWIFErr = nil

	// Send error
	node.sendRawErr = tErr
	_, _, _, err = wallet.Redeem(redemptions)
	if err == nil {
		t.Fatalf("no error for send error")
	}
	node.sendRawErr = nil

	// Wrong hash
	var h chainhash.Hash
	h[0] = 0x01
	node.sendRawHash = &h
	_, _, _, err = wallet.Redeem(redemptions)
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
	wallet, node, shutdown, err := tNewWallet()
	defer shutdown()
	if err != nil {
		t.Fatal(err)
	}

	vout := uint32(5)
	privBytes, _ := hex.DecodeString("b07209eec1a8fb6cfe5cb6ace36567406971a75c330db7101fb21bc679bc5330")
	privKey := secp256k1.PrivKeyFromBytes(privBytes)
	pubKey := privKey.PubKey()

	msg := randBytes(36)
	pk := pubKey.SerializeCompressed()
	msgHash := chainhash.HashB(msg)
	signature := ecdsa.Sign(privKey, msgHash)
	sig := signature.Serialize()

	node.privWIF, err = dcrutil.NewWIF(privBytes, tChainParams.PrivateKeyID, dcrec.STEcdsaSecp256k1)
	if err != nil {
		t.Fatalf("NewWIF error: %v", err)
	}

	op := newOutput(tTxHash, vout, 5e7, wire.TxTreeRegular)

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
	txOut := makeGetTxOutRes(0, 5, nil)
	txOut.ScriptPubKey.Addresses = []string{tPKHAddr.String()}
	node.txOutRes[newOutPoint(tTxHash, vout)] = txOut
	check()

	// gettxout error
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
	wallet, _, shutdown, err := tNewWallet()
	defer shutdown()
	if err != nil {
		t.Fatal(err)
	}
	secretHash, _ := hex.DecodeString("5124208c80d33507befa517c08ed01aa8d33adbf37ecd70fb5f9352f7a51a88d")
	lockTime := time.Now().Add(time.Hour * 12)
	addrStr := tPKHAddr.String()
	contract, err := dexdcr.MakeContract(addrStr, addrStr, secretHash, lockTime.Unix(), tChainParams)
	if err != nil {
		t.Fatalf("error making swap contract: %v", err)
	}
	addr, _ := stdaddr.NewAddressScriptHashV0(contract, tChainParams)
	_, pkScript := addr.PaymentScript()

	// Prepare the contract tx data.
	contractTx := wire.NewMsgTx()
	contractTx.AddTxIn(&wire.TxIn{})
	contractTx.AddTxOut(&wire.TxOut{
		Value:    5 * int64(tLotSize),
		PkScript: pkScript,
	})
	contractTxData, err := contractTx.Bytes()
	if err != nil {
		t.Fatalf("error preparing contract txdata: %v", err)
	}

	contractHash := contractTx.TxHash()
	contractVout := uint32(0)
	contractCoinID := toCoinID(&contractHash, contractVout)

	audit, err := wallet.AuditContract(contractCoinID, contract, contractTxData, true)
	if err != nil {
		t.Fatalf("audit error: %v", err)
	}
	if audit.Recipient != addrStr {
		t.Fatalf("wrong recipient. wanted '%s', got '%s'", addrStr, audit.Recipient)
	}
	if !bytes.Equal(audit.Contract, contract) {
		t.Fatalf("contract not set to coin redeem script")
	}
	if audit.Expiration.Equal(lockTime) {
		t.Fatalf("wrong lock time. wanted %d, got %d", lockTime.Unix(), audit.Expiration.Unix())
	}

	// Invalid txid
	_, err = wallet.AuditContract(make([]byte, 15), contract, contractTxData, false)
	if err == nil {
		t.Fatalf("no error for bad txid")
	}

	// Wrong contract
	pkh, _ := hex.DecodeString("c6a704f11af6cbee8738ff19fc28cdc70aba0b82")
	wrongAddr, _ := stdaddr.NewAddressPubKeyHashEcdsaSecp256k1V0(pkh, tChainParams)
	wrongAddrStr := wrongAddr.String()
	wrongContract, err := dexdcr.MakeContract(wrongAddrStr, wrongAddrStr, secretHash, lockTime.Unix(), tChainParams)
	if err != nil {
		t.Fatalf("error making wrong swap contract: %v", err)
	}
	_, err = wallet.AuditContract(contractCoinID, wrongContract, contractTxData, false)
	if err == nil {
		t.Fatalf("no error for wrong contract")
	}

	// Invalid contract
	_, wrongPkScript := wrongAddr.PaymentScript()
	_, err = wallet.AuditContract(contractCoinID, wrongPkScript, contractTxData, false) // addrPkScript not a valid contract
	if err == nil {
		t.Fatalf("no error for invalid contract")
	}

	// No txdata
	_, err = wallet.AuditContract(contractCoinID, contract, nil, false)
	if err == nil {
		t.Fatalf("no error for no txdata")
	}

	// Invalid txdata, zero inputs
	contractTx.TxIn = nil
	invalidContractTxData, err := contractTx.Bytes()
	if err != nil {
		t.Fatalf("error preparing invalid contract txdata: %v", err)
	}
	_, err = wallet.AuditContract(contractCoinID, contract, invalidContractTxData, false)
	if err == nil {
		t.Fatalf("no error for unknown txout")
	}

	// Wrong txdata, wrong output script
	wrongContractTx := wire.NewMsgTx()
	wrongContractTx.AddTxIn(&wire.TxIn{})
	wrongContractTx.AddTxOut(&wire.TxOut{
		Value:    5 * int64(tLotSize),
		PkScript: wrongPkScript,
	})
	wrongContractTxData, err := wrongContractTx.Bytes()
	if err != nil {
		t.Fatalf("error preparing wrong contract txdata: %v", err)
	}
	_, err = wallet.AuditContract(contractCoinID, contract, wrongContractTxData, false)
	if err == nil {
		t.Fatalf("no error for unknown txout")
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
	wallet, node, shutdown, err := tNewWallet()
	defer shutdown()
	if err != nil {
		t.Fatal(err)
	}

	_, bestBlockHeight, err := node.GetBestBlock(context.Background())
	if err != nil {
		t.Fatalf("unexpected GetBestBlock error: %v", err)
	}

	contractHeight := bestBlockHeight + 1
	contractVout := uint32(1)

	secret := randBytes(32)
	secretHash := sha256.Sum256(secret)
	lockTime := time.Now().Add(time.Hour * 12)
	addrStr := tPKHAddr.String()
	contract, err := dexdcr.MakeContract(addrStr, addrStr, secretHash[:], lockTime.Unix(), tChainParams)
	if err != nil {
		t.Fatalf("error making swap contract: %v", err)
	}
	contractAddr, _ := stdaddr.NewAddressScriptHashV0(contract, tChainParams)
	_, contractP2SHScript := contractAddr.PaymentScript()

	tPKHAddrV3, _ := stdaddr.DecodeAddress(tPKHAddr.String(), tChainParams)
	_, otherScript := tPKHAddrV3.PaymentScript()

	redemptionScript, _ := dexdcr.RedeemP2SHContract(contract, randBytes(73), randBytes(33), secret)
	otherSpendScript, _ := txscript.NewScriptBuilder().
		AddData(randBytes(73)).
		AddData(randBytes(33)).
		Script()

	// Prepare and add the contract transaction to the blockchain. Put the pay-to-contract script at index 1.
	inputs := []*wire.TxIn{makeRPCVin(&chainhash.Hash{}, 0, otherSpendScript)}
	outputScripts := []dex.Bytes{otherScript, contractP2SHScript}
	contractTx := makeRawTx(inputs, outputScripts)
	contractTxHash := contractTx.TxHash()
	coinID := toCoinID(&contractTxHash, contractVout)
	blockHash, _ := node.blockchain.addRawTx(contractHeight, contractTx)
	txHex, err := makeTxHex(inputs, outputScripts)
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
	node.blockchain.addRawTx(contractHeight+1, dummyTx())

	// Prepare the redemption tx inputs including an input that spends the contract output.
	inputs = append(inputs, makeRPCVin(&contractTxHash, contractVout, redemptionScript))

	// Add the redemption to mempool and check if wallet.FindRedemption finds it.
	redeemTx := makeRawTx(inputs, []dex.Bytes{otherScript})
	node.blockchain.addRawTx(-1, redeemTx)
	_, checkSecret, err := wallet.FindRedemption(tCtx, coinID, nil)
	if err != nil {
		t.Fatalf("error finding redemption: %v", err)
	}
	if !bytes.Equal(checkSecret, secret) {
		t.Fatalf("wrong secret. expected %x, got %x", secret, checkSecret)
	}

	// Move the redemption to a new block and check if wallet.FindRedemption finds it.
	_, redeemBlock := node.blockchain.addRawTx(contractHeight+2, makeRawTx(inputs, []dex.Bytes{otherScript}))
	_, checkSecret, err = wallet.FindRedemption(tCtx, coinID, nil)
	if err != nil {
		t.Fatalf("error finding redemption: %v", err)
	}
	if !bytes.Equal(checkSecret, secret) {
		t.Fatalf("wrong secret. expected %x, got %x", secret, checkSecret)
	}

	// gettransaction error
	node.walletTxErr = tErr
	_, _, err = wallet.FindRedemption(tCtx, coinID, nil)
	if err == nil {
		t.Fatalf("no error for gettransaction rpc error")
	}
	node.walletTxErr = nil

	// getcfilterv2 error
	node.rawErr[methodGetCFilterV2] = tErr
	_, _, err = wallet.FindRedemption(tCtx, coinID, nil)
	if err == nil {
		t.Fatalf("no error for getcfilterv2 rpc error")
	}
	delete(node.rawErr, methodGetCFilterV2)

	// missing redemption
	redeemBlock.Transactions[0].TxIn[1].PreviousOutPoint.Hash = chainhash.Hash{}
	ctx, cancel := context.WithTimeout(tCtx, 2*time.Second)
	defer cancel() // ctx should auto-cancel after 2 seconds, but this is apparently good practice to prevent leak
	_, k, err := wallet.FindRedemption(ctx, coinID, nil)
	if ctx.Err() == nil || k != nil {
		// Expected ctx to cancel after timeout and no secret should be found.
		t.Fatalf("unexpected result for missing redemption: secret: %v, err: %v", k, err)
	}
	redeemBlock.Transactions[0].TxIn[1].PreviousOutPoint.Hash = contractTxHash

	// Canceled context
	deadCtx, cancelCtx := context.WithCancel(tCtx)
	cancelCtx()
	_, _, err = wallet.FindRedemption(deadCtx, coinID, nil)
	if err == nil {
		t.Fatalf("no error for canceled context")
	}

	// Expect FindRedemption to error because of bad input sig.
	redeemBlock.Transactions[0].TxIn[1].SignatureScript = randBytes(100)
	_, _, err = wallet.FindRedemption(tCtx, coinID, nil)
	if err == nil {
		t.Fatalf("no error for wrong redemption")
	}
	redeemBlock.Transactions[0].TxIn[1].SignatureScript = redemptionScript

	// Wrong script type for output
	node.walletTx.Hex, _ = makeTxHex(inputs, []dex.Bytes{otherScript, otherScript})
	_, _, err = wallet.FindRedemption(tCtx, coinID, nil)
	if err == nil {
		t.Fatalf("no error for wrong script type")
	}
	node.walletTx.Hex = txHex

	// Sanity check to make sure it passes again.
	_, _, err = wallet.FindRedemption(tCtx, coinID, nil)
	if err != nil {
		t.Fatalf("error after clearing errors: %v", err)
	}
}

func TestRefund(t *testing.T) {
	wallet, node, shutdown, err := tNewWallet()
	defer shutdown()
	if err != nil {
		t.Fatal(err)
	}

	secret := randBytes(32)
	secretHash := sha256.Sum256(secret)
	lockTime := time.Now().Add(time.Hour * 12)
	addrStr := tPKHAddr.String()
	contract, err := dexdcr.MakeContract(addrStr, addrStr, secretHash[:], lockTime.Unix(), tChainParams)
	if err != nil {
		t.Fatalf("error making swap contract: %v", err)
	}
	const feeSuggestion = 100

	tipHash, tipHeight := node.getBestBlock()
	var confs int64 = 1
	if tipHeight > 1 {
		confs = 2
	}

	bigTxOut := makeGetTxOutRes(confs, 5, nil)
	bigOutID := newOutPoint(tTxHash, 0)
	node.txOutRes[bigOutID] = bigTxOut
	node.txOutRes[bigOutID].BestBlock = tipHash.String() // required to calculate the block for the output
	node.changeAddr = tPKHAddr
	node.newAddr = tPKHAddr

	privBytes, _ := hex.DecodeString("b07209eec1a8fb6cfe5cb6ace36567406971a75c330db7101fb21bc679bc5330")
	node.privWIF, err = dcrutil.NewWIF(privBytes, tChainParams.PrivateKeyID, dcrec.STEcdsaSecp256k1)
	if err != nil {
		t.Fatalf("NewWIF error: %v", err)
	}

	contractOutput := newOutput(tTxHash, 0, 1e8, wire.TxTreeRegular)
	_, err = wallet.Refund(contractOutput.ID(), contract, feeSuggestion)
	if err != nil {
		t.Fatalf("refund error: %v", err)
	}

	// Invalid coin
	badReceipt := &tReceipt{
		coin: &tCoin{id: make([]byte, 15)},
	}
	_, err = wallet.Refund(badReceipt.coin.id, badReceipt.contract, feeSuggestion)
	if err == nil {
		t.Fatalf("no error for bad receipt")
	}

	// gettxout error
	node.txOutErr = tErr
	_, err = wallet.Refund(contractOutput.ID(), contract, feeSuggestion)
	if err == nil {
		t.Fatalf("no error for missing utxo")
	}
	node.txOutErr = nil

	// bad contract
	badContractOutput := newOutput(tTxHash, 0, 1e8, wire.TxTreeRegular)
	_, err = wallet.Refund(badContractOutput.ID(), randBytes(50), feeSuggestion)
	if err == nil {
		t.Fatalf("no error for bad contract")
	}

	// Too small.
	node.txOutRes[bigOutID] = newTxOutResult(nil, 100, 2)
	_, err = wallet.Refund(contractOutput.ID(), contract, feeSuggestion)
	if err == nil {
		t.Fatalf("no error for value < fees")
	}
	node.txOutRes[bigOutID] = bigTxOut

	// signature error
	node.privWIFErr = tErr
	_, err = wallet.Refund(contractOutput.ID(), contract, feeSuggestion)
	if err == nil {
		t.Fatalf("no error for dumpprivkey rpc error")
	}
	node.privWIFErr = nil

	// send error
	node.sendRawErr = tErr
	_, err = wallet.Refund(contractOutput.ID(), contract, feeSuggestion)
	if err == nil {
		t.Fatalf("no error for sendrawtransaction rpc error")
	}
	node.sendRawErr = nil

	// bad checkhash
	var badHash chainhash.Hash
	badHash[0] = 0x05
	node.sendRawHash = &badHash
	_, err = wallet.Refund(contractOutput.ID(), contract, feeSuggestion)
	if err == nil {
		t.Fatalf("no error for tx hash")
	}
	node.sendRawHash = nil

	// Sanity check that we can succeed again.
	_, err = wallet.Refund(contractOutput.ID(), contract, feeSuggestion)
	if err != nil {
		t.Fatalf("re-refund error: %v", err)
	}
}

type tSenderType byte

const (
	tSendSender tSenderType = iota
	tWithdrawSender
)

func testSender(t *testing.T, senderType tSenderType) {
	wallet, node, shutdown, err := tNewWallet()
	defer shutdown()
	if err != nil {
		t.Fatal(err)
	}
	var sendVal uint64 = 1e8
	var unspentVal uint64 = 100e8
	const feeSuggestion = 100
	funName := "Send"
	sender := func(addr string, val uint64) (asset.Coin, error) {
		return wallet.Send(addr, val, feeSuggestion)
	}
	if senderType == tWithdrawSender {
		funName = "Withdraw"
		// For withdraw, test with unspent total = withdraw value
		unspentVal = sendVal
		sender = func(addr string, val uint64) (asset.Coin, error) {
			return wallet.Withdraw(addr, val, feeSuggestion)
		}
	}

	addr := tPKHAddr.String()
	node.changeAddr = tPKHAddr

	node.unspent = []walletjson.ListUnspentResult{{
		TxID:          tTxID,
		Address:       tPKHAddr.String(),
		Account:       tAcctName,
		Amount:        float64(unspentVal) / 1e8,
		Confirmations: 5,
		ScriptPubKey:  hex.EncodeToString(tP2PKHScript),
		Spendable:     true,
	}}
	//node.unspent = append(node.unspent, node.unspent[0])

	_, err = sender(addr, sendVal)
	if err != nil {
		t.Fatalf(funName+" error: %v", err)
	}

	// invalid address
	_, err = sender("badaddr", sendVal)
	if err == nil {
		t.Fatalf("no error for bad address: %v", err)
	}

	// GetRawChangeAddress error
	if senderType == tSendSender { // withdraw test does not get a change address
		node.changeAddrErr = tErr
		_, err = sender(addr, sendVal)
		if err == nil {
			t.Fatalf("no error for rawchangeaddress: %v", err)
		}
		node.changeAddrErr = nil
	}

	// good again
	_, err = sender(addr, sendVal)
	if err != nil {
		t.Fatalf(funName+" error afterwards: %v", err)
	}
}

func TestWithdraw(t *testing.T) {
	testSender(t, tWithdrawSender)
}

func TestSend(t *testing.T) {
	testSender(t, tSendSender)
}

func Test_withdraw(t *testing.T) {
	wallet, node, shutdown, err := tNewWallet()
	defer shutdown()
	if err != nil {
		t.Fatal(err)
	}
	address := tPKHAddr.String()
	node.changeAddr = tPKHAddr

	var unspentVal uint64 = 100e8
	node.unspent = []walletjson.ListUnspentResult{{
		TxID:          tTxID,
		Address:       tPKHAddr.String(),
		Account:       tAcctName,
		Amount:        float64(unspentVal) / 1e8,
		Confirmations: 5,
		ScriptPubKey:  hex.EncodeToString(tP2PKHScript),
		Spendable:     true,
	}}

	addr, err := stdaddr.DecodeAddress(address, tChainParams)
	if err != nil {
		t.Fatal(err)
	}

	// This should make a msgTx with one input and one output.
	msgTx, val, err := wallet.withdraw(addr, unspentVal, optimalFeeRate)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgTx.TxOut) != 1 {
		t.Fatalf("expected 1 output, got %d", len(msgTx.TxOut))
	}
	if val != uint64(msgTx.TxOut[0].Value) {
		t.Errorf("expected non-change output to be %d, got %d", val, msgTx.TxOut[0].Value)
	}
	if val >= unspentVal {
		t.Errorf("expected output to be have fees deducted")
	}

	// Then with unspentVal just slightly larger than send. This should still
	// make a msgTx with one output, but larger than before. The sent value is
	// SMALLER than requested because it was required for fees.
	avail := unspentVal + 77
	node.unspent[0].Amount = float64(avail) / 1e8
	msgTx, val, err = wallet.withdraw(addr, unspentVal, optimalFeeRate)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgTx.TxOut) != 1 {
		t.Fatalf("expected 1 output, got %d", len(msgTx.TxOut))
	}
	if val != uint64(msgTx.TxOut[0].Value) {
		t.Errorf("expected non-change output to be %d, got %d", val, msgTx.TxOut[0].Value)
	}
	if val >= unspentVal {
		t.Errorf("expected output to be have fees deducted")
	}

	// Still no change, but this time the sent value is LARGER than requested
	// because change would be dust, and we don't over pay fees.
	avail = unspentVal + 3000
	node.unspent[0].Amount = float64(avail) / 1e8
	msgTx, val, err = wallet.withdraw(addr, unspentVal, optimalFeeRate)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgTx.TxOut) != 1 {
		t.Fatalf("expected 1 output, got %d", len(msgTx.TxOut))
	}
	if val <= unspentVal {
		t.Errorf("expected output to be more thrifty")
	}

	// Then with unspentVal considerably larger than (double) send. This should
	// make a msgTx with two outputs as the change is no longer dust. The change
	// should be exactly unspentVal and the sent amount should be
	// unspentVal-fees.
	node.unspent[0].Amount = float64(unspentVal*2) / 1e8
	msgTx, val, err = wallet.withdraw(addr, unspentVal, optimalFeeRate)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgTx.TxOut) != 2 {
		t.Fatalf("expected 2 outputs, got %d", len(msgTx.TxOut))
	}
	if val != uint64(msgTx.TxOut[0].Value) {
		t.Errorf("expected non-change output to be %d, got %d", val, msgTx.TxOut[0].Value)
	}
	if unspentVal != uint64(msgTx.TxOut[1].Value) {
		t.Errorf("expected change output to be %d, got %d", unspentVal, msgTx.TxOut[1].Value)
	}
}

func Test_sendToAddress(t *testing.T) {
	wallet, node, shutdown, err := tNewWallet()
	defer shutdown()
	if err != nil {
		t.Fatal(err)
	}
	address := tPKHAddr.String()
	node.changeAddr = tPKHAddr

	var unspentVal uint64 = 100e8
	node.unspent = []walletjson.ListUnspentResult{{
		TxID:          tTxID,
		Address:       tPKHAddr.String(),
		Account:       tAcctName,
		Amount:        float64(unspentVal) / 1e8,
		Confirmations: 5,
		ScriptPubKey:  hex.EncodeToString(tP2PKHScript),
		Spendable:     true,
	}}

	addr, err := stdaddr.DecodeAddress(address, tChainParams)
	if err != nil {
		t.Fatal(err)
	}

	// This should return an error, not enough funds to send.
	_, _, err = wallet.sendToAddress(addr, unspentVal, optimalFeeRate)
	if err == nil {
		t.Fatal("Expected error, not enough funds to send.")
	}

	// With a lower send value, send should be successful.
	var sendVal uint64 = 10e8
	node.unspent[0].Amount = float64(unspentVal)
	msgTx, val, err := wallet.sendToAddress(addr, sendVal, optimalFeeRate)
	if err != nil {
		t.Fatal(err)
	}
	if val != uint64(msgTx.TxOut[0].Value) {
		t.Errorf("expected non-change output to be %d, got %d", val, msgTx.TxOut[0].Value)
	}
	if val != sendVal {
		t.Errorf("expected non-change output to be %d, got %d", sendVal, val)
	}
}

func TestLookupTxOutput(t *testing.T) {
	wallet, node, shutdown, err := tNewWallet()
	defer shutdown()
	if err != nil {
		t.Fatal(err)
	}

	coinID := make([]byte, 36)
	copy(coinID[:32], tTxHash[:])
	op := newOutPoint(tTxHash, 0)

	// Bad output coin
	op.vout = 10
	_, _, spent, err := wallet.lookupTxOutput(context.Background(), &op.txHash, op.vout)
	if err == nil {
		t.Fatalf("no error for bad output coin")
	}
	if spent {
		t.Fatalf("spent is true for bad output coin")
	}
	op.vout = 0

	// Add the txOutRes with 2 confs and BestBlock correctly set.
	node.txOutRes[op] = makeGetTxOutRes(2, 1, tP2PKHScript)
	_, confs, spent, err := wallet.lookupTxOutput(context.Background(), &op.txHash, op.vout)
	if err != nil {
		t.Fatalf("unexpected error for gettxout path: %v", err)
	}
	if confs != 2 {
		t.Fatalf("confs not retrieved from gettxout path. expected 2, got %d", confs)
	}
	if spent {
		t.Fatalf("expected spent = false for gettxout path, got true")
	}

	// gettransaction error
	delete(node.txOutRes, op)
	node.walletTxErr = tErr
	_, _, spent, err = wallet.lookupTxOutput(context.Background(), &op.txHash, op.vout)
	if err == nil {
		t.Fatalf("no error for gettransaction error")
	}
	if spent {
		t.Fatalf("spent is true with gettransaction error")
	}
	node.walletTxErr = nil

	// wallet.lookupTxOutput will check if the tx is confirmed, its hex
	// is valid and contains an output at index 0, for the output to be
	// considered spent.
	tx := wire.NewMsgTx()
	tx.AddTxIn(&wire.TxIn{})
	tx.AddTxOut(&wire.TxOut{
		PkScript: tP2PKHScript,
	})
	txHex, err := msgTxToHex(tx)
	if err != nil {
		t.Fatalf("error preparing tx hex with 1 output: %v", err)
	}
	node.walletTx = &walletjson.GetTransactionResult{
		Hex:           txHex,
		Confirmations: 0, // unconfirmed = unspent
	}
	_, _, spent, err = wallet.lookupTxOutput(context.Background(), &op.txHash, op.vout)
	if err != nil {
		t.Fatalf("unexpected error for gettransaction path (unconfirmed): %v", err)
	}
	if spent {
		t.Fatalf("expected spent = false for gettransaction path (unconfirmed), got true")
	}

	// Confirmed wallet tx without gettxout response is spent.
	node.walletTx.Confirmations = 2
	_, _, spent, err = wallet.lookupTxOutput(context.Background(), &op.txHash, op.vout)
	if err != nil {
		t.Fatalf("unexpected error for gettransaction path (confirmed): %v", err)
	}
	if !spent {
		t.Fatalf("expected spent = true for gettransaction path (confirmed), got false")
	}

	// In spv mode, output is assumed unspent if it doesn't pay to the wallet.
	(wallet.wallet.(*rpcWallet)).spvMode = true
	_, _, spent, err = wallet.lookupTxOutput(context.Background(), &op.txHash, op.vout)
	if err != nil {
		t.Fatalf("unexpected error for spv gettransaction path (non-wallet output): %v", err)
	}
	if spent {
		t.Fatalf("expected spent = false for spv gettransaction path (non-wallet output), got true")
	}

	// In spv mode, output is spent if it pays to the wallet.
	node.walletTx.Details = []walletjson.GetTransactionDetailsResult{{
		Vout:     0,
		Category: "receive", // output at index 0 pays to the wallet
	}}
	_, _, spent, err = wallet.lookupTxOutput(context.Background(), &op.txHash, op.vout)
	if err != nil {
		t.Fatalf("unexpected error for spv gettransaction path (wallet output): %v", err)
	}
	if !spent {
		t.Fatalf("expected spent = true for spv gettransaction path (wallet output), got false")
	}
}

func TestSendEdges(t *testing.T) {
	wallet, node, shutdown, err := tNewWallet()
	defer shutdown()
	if err != nil {
		t.Fatal(err)
	}

	const feeRate uint64 = 3

	const swapVal = 2e8 // leaving untyped. NewTxOut wants int64

	contractAddr, _ := stdaddr.NewAddressScriptHashV0(randBytes(20), tChainParams)
	// See dexdcr.IsDust for the source of this dustCoverage voodoo.
	dustCoverage := (dexdcr.P2PKHOutputSize + 165) * feeRate * 3
	dexReqFees := dexdcr.InitTxSize * feeRate

	_, pkScript := contractAddr.PaymentScript()

	newBaseTx := func(funding uint64) *wire.MsgTx {
		baseTx := wire.NewMsgTx()
		baseTx.AddTxIn(wire.NewTxIn(new(wire.OutPoint), int64(funding), nil))
		baseTx.AddTxOut(wire.NewTxOut(swapVal, pkScript))
		return baseTx
	}

	node.signFunc = func(tx *wire.MsgTx) (*wire.MsgTx, bool, error) {
		return signFunc(tx, dexdcr.P2PKHSigScriptSize)
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

	// tPKHAddrV3, _ := stdaddr.DecodeAddress(tPKHAddr.String(), tChainParams)
	node.changeAddr = tPKHAddr

	for _, tt := range tests {
		tx, err := wallet.sendWithReturn(newBaseTx(tt.funding), feeRate, -1)
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
	wallet, node, shutdown, err := tNewWallet()
	defer shutdown()
	if err != nil {
		t.Fatal(err)
	}

	node.rawRes[methodSyncStatus], node.rawErr[methodSyncStatus] = json.Marshal(&walletjson.SyncStatusResult{
		Synced:               true,
		InitialBlockDownload: false,
		HeadersFetchProgress: 1,
	})
	synced, progress, err := wallet.SyncStatus()
	if err != nil {
		t.Fatalf("SyncStatus error (synced expected): %v", err)
	}
	if !synced {
		t.Fatalf("synced = false for progress=1")
	}
	if progress < 1 {
		t.Fatalf("progress not complete with sync true")
	}

	node.rawErr[methodSyncStatus] = tErr
	_, _, err = wallet.SyncStatus()
	if err == nil {
		t.Fatalf("SyncStatus error not propagated")
	}
	node.rawErr[methodSyncStatus] = nil

	nodeSyncStatusResult := &walletjson.SyncStatusResult{
		Synced:               false,
		InitialBlockDownload: false,
		HeadersFetchProgress: 0.5, // Headers: 200, WalletTip: 100
	}
	node.rawRes[methodSyncStatus], node.rawErr[methodSyncStatus] = json.Marshal(nodeSyncStatusResult)
	synced, progress, err = wallet.SyncStatus()
	if err != nil {
		t.Fatalf("SyncStatus error (half-synced): %v", err)
	}
	if synced {
		t.Fatalf("synced = true for progress=0.5")
	}
	if progress != nodeSyncStatusResult.HeadersFetchProgress {
		t.Fatalf("progress out of range. Expected %.2f, got %.2f", nodeSyncStatusResult.HeadersFetchProgress, progress)
	}
}

func TestPreSwap(t *testing.T) {
	wallet, node, shutdown, _ := tNewWallet()
	defer shutdown()

	// See math from TestFundEdges. 10 lots with max fee rate of 34 sats/vbyte.

	swapVal := uint64(1e8)
	lots := swapVal / tLotSize // 10 lots

	const swapSize = 251
	const totalBytes = 2510
	const bestCaseBytes = 557

	backingFees := uint64(totalBytes) * tDCR.MaxFeeRate // total_bytes * fee_rate

	minReq := swapVal + backingFees

	fees := uint64(totalBytes) * tDCR.MaxFeeRate
	p2pkhUnspent := walletjson.ListUnspentResult{
		TxID:          tTxID,
		Address:       tPKHAddr.String(),
		Account:       tAcctName,
		Amount:        float64(swapVal+fees-1) / 1e8, // one atom less than needed
		Confirmations: 5,
		ScriptPubKey:  hex.EncodeToString(tP2PKHScript),
		Spendable:     true,
	}

	node.unspent = []walletjson.ListUnspentResult{p2pkhUnspent}

	form := &asset.PreSwapForm{
		LotSize:       tLotSize,
		Lots:          lots,
		AssetConfig:   tDCR,
		Immediate:     false,
		FeeSuggestion: feeSuggestion,
	}

	node.unspent[0].Amount = float64(minReq) / 1e8

	// Initial success.
	preSwap, err := wallet.PreSwap(form)
	if err != nil {
		t.Fatalf("PreSwap error: %v", err)
	}

	maxFees := totalBytes * tDCR.MaxFeeRate
	estHighFees := totalBytes * feeSuggestion
	estLowFees := bestCaseBytes * feeSuggestion
	checkSwapEstimate(t, preSwap.Estimate, lots, swapVal, maxFees, estHighFees, estLowFees)

	// Too little funding is an error.
	node.unspent[0].Amount = float64(minReq-1) / 1e8
	_, err = wallet.PreSwap(form)
	if err == nil {
		t.Fatalf("no PreSwap error for not enough funds")
	}
	node.unspent[0].Amount = float64(minReq) / 1e8

	// Success again.
	_, err = wallet.PreSwap(form)
	if err != nil {
		t.Fatalf("PreSwap error: %v", err)
	}
}

func TestPreRedeem(t *testing.T) {
	wallet, _, shutdown, _ := tNewWallet()
	defer shutdown()

	preRedeem, err := wallet.PreRedeem(&asset.PreRedeemForm{
		Lots:        5,
		AssetConfig: tDCR,
	})
	// Shouldn't actually be any path to error.
	if err != nil {
		t.Fatalf("PreRedeem non-segwit error: %v", err)
	}

	// Just a sanity check.
	if preRedeem.Estimate.RealisticBestCase >= preRedeem.Estimate.RealisticWorstCase {
		t.Fatalf("best case > worst case")
	}
}

func TestEstimateRegistrationTxFee(t *testing.T) {
	wallet, _, shutdown, _ := tNewWallet()
	defer shutdown()

	const inputCount = 5
	const txSize = dexdcr.MsgTxOverhead + dexdcr.P2PKHOutputSize*2 + inputCount*dexdcr.P2PKHInputSize
	wallet.config().feeRateLimit = 100
	wallet.config().fallbackFeeRate = 30

	estimate := wallet.EstimateRegistrationTxFee(50)
	if estimate != 50*txSize {
		t.Fatalf("expected tx fee to be %d but got %d", 50*txSize, estimate)
	}

	estimate = wallet.EstimateRegistrationTxFee(0)
	if estimate != wallet.config().fallbackFeeRate*txSize {
		t.Fatalf("expected tx fee to be %d but got %d", wallet.config().fallbackFeeRate*txSize, estimate)
	}

	estimate = wallet.EstimateRegistrationTxFee(wallet.config().feeRateLimit + 1)
	if estimate != wallet.config().fallbackFeeRate*txSize {
		t.Fatalf("expected tx fee to be %d but got %d", wallet.config().fallbackFeeRate*txSize, estimate)
	}
}

func Test_dcrPerKBToAtomsPerByte(t *testing.T) {
	tests := []struct {
		name             string
		estimatedFeeRate float64
		want             uint64
		wantErr          bool
	}{
		{
			"catch negative", // but caller should check
			-0.0002,
			0,
			true,
		},
		{
			"ok 0", // but caller should check
			0.0,
			0,
			false,
		},
		{
			"ok 10",
			0.0001,
			10,
			false,
		},
		{
			"ok 11",
			0.00011,
			11,
			false,
		},
		{
			"ok 1",
			0.00001,
			1,
			false,
		},
		{
			"ok 1 rounded up",
			0.000002,
			1,
			false,
		},
		{
			"catch NaN err",
			math.NaN(),
			0,
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := dcrPerKBToAtomsPerByte(tt.estimatedFeeRate)
			if (err != nil) != tt.wantErr {
				t.Errorf("dcrPerKBToAtomsPerByte() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("dcrPerKBToAtomsPerByte() = %v, want %v", got, tt.want)
			}
		})
	}
}

type tReconfigurer struct {
	*rpcWallet
	restart bool
	err     error
}

func (r *tReconfigurer) Reconfigure(ctx context.Context, cfg *asset.WalletConfig, net dex.Network, currentAddress, depositAccount string) (restartRequired bool, err error) {
	return r.restart, r.err
}

func TestReconfigure(t *testing.T) {
	wallet, _, shutdown, _ := tNewWalletMonitorBlocks(false)
	defer shutdown()

	reconfigurer := tReconfigurer{
		rpcWallet: wallet.wallet.(*rpcWallet),
	}

	wallet.wallet = &reconfigurer

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg1 := &walletConfig{
		PrimaryAccount:   "primary",
		UnmixedAccount:   "unmixed",
		TradingAccount:   "trading",
		UseSplitTx:       true,
		FallbackFeeRate:  55,
		FeeRateLimit:     98,
		RedeemConfTarget: 7,
		ApiFeeFallback:   true,
	}

	cfg2 := &walletConfig{
		PrimaryAccount:   "primary2",
		UnmixedAccount:   "unmixed2",
		TradingAccount:   "trading2",
		UseSplitTx:       false,
		FallbackFeeRate:  66,
		FeeRateLimit:     97,
		RedeemConfTarget: 5,
		ApiFeeFallback:   false,
	}

	checkConfig := func(cfg *walletConfig) {
		if cfg.PrimaryAccount != wallet.config().primaryAcct ||
			cfg.UnmixedAccount != wallet.config().unmixedAccount ||
			cfg.TradingAccount != wallet.config().tradingAccount ||
			cfg.UseSplitTx != wallet.config().useSplitTx ||
			toAtoms(cfg.FallbackFeeRate/1000) != wallet.config().fallbackFeeRate ||
			toAtoms(cfg.FeeRateLimit/1000) != wallet.config().feeRateLimit ||
			cfg.RedeemConfTarget != wallet.config().redeemConfTarget ||
			cfg.ApiFeeFallback != wallet.config().apiFeeFallback {
			t.Fatalf("wallet not configured with the correct values")
		}
	}

	settings1, err := config.Mapify(cfg1)
	if err != nil {
		t.Fatalf("failed to mapify: %v", err)
	}

	settings2, err := config.Mapify(cfg2)
	if err != nil {
		t.Fatalf("failed to mapify: %v", err)
	}

	walletCfg := &asset.WalletConfig{
		Type:     walletTypeDcrwRPC,
		Settings: settings1,
		DataDir:  "abcd",
	}

	// restart = false
	restart, err := wallet.Reconfigure(ctx, walletCfg, "123456")
	if err != nil {
		t.Fatalf("did not expect an error")
	}
	if restart {
		t.Fatalf("expected false restart but got true")
	}
	checkConfig(cfg1)

	// restart = 2
	reconfigurer.restart = true
	restart, err = wallet.Reconfigure(ctx, walletCfg, "123456")
	if err != nil {
		t.Fatalf("did not expect an error")
	}
	if !restart {
		t.Fatalf("expected true restart but got false")
	}
	checkConfig(cfg1)

	// try to set new configs, but get error. config should not change.
	reconfigurer.err = errors.New("reconfigure error")
	walletCfg.Settings = settings2
	_, err = wallet.Reconfigure(ctx, walletCfg, "123456")
	if err == nil {
		t.Fatalf("expected an error")
	}
	checkConfig(cfg1)
}
