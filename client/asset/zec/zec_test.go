// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package zec

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"sync"
	"testing"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/asset/btc"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/encode"
	dexbtc "decred.org/dcrdex/dex/networks/btc"
	dexzec "decred.org/dcrdex/dex/networks/zec"
	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
	"github.com/decred/dcrd/dcrjson/v4"
)

const (
	maxFutureBlockTime = 2 * time.Hour // see MaxTimeOffsetSeconds in btcd/blockchain/validate.go
	tAddr              = "tmH2m5fi5yY3Qg2GpGwcCrnnoD4wp944RMJ"
	tUnifiedAddr       = "uregtest1w2khftfjd8w7vgw32g24nwqdt0f3zfrwe9a4uy6trsq3swlpgsfssf2uqsfluu3490jdgers4vwz8l9yg39c9x70phllu8cy57f8mdc6ym5e7xtra0f99wtxvnm0wg4uz7an0wvl5s7jt2y7fqla2j976ej0e4hsspq73m0zrw5lzly797fhku0q74xeshuwvwmrku5u8f7gyd2r0sx"
)

var (
	tCtx     context.Context
	tLogger  = dex.StdOutLogger("T", dex.LevelError)
	tTxID    = "308e9a3675fc3ea3862b7863eeead08c621dcc37ff59de597dd3cdab41450ad9"
	tTxHash  *chainhash.Hash
	tP2PKH   []byte
	tErr            = errors.New("test error")
	tLotSize uint64 = 1e6
)

type msgBlockWithHeight struct {
	msgBlock *dexzec.Block
	height   int64
}

const testBlocksPerBlockTimeOffset = 4

func generateTestBlockTime(blockHeight int64) time.Time {
	return time.Unix(1e6, 0).Add(time.Duration(blockHeight) * maxFutureBlockTime / testBlocksPerBlockTimeOffset)
}

func makeRawTx(pkScripts []dex.Bytes, inputs []*wire.TxIn) *dexzec.Tx {
	tx := &wire.MsgTx{
		TxIn: inputs,
	}
	for _, pkScript := range pkScripts {
		tx.TxOut = append(tx.TxOut, wire.NewTxOut(1, pkScript))
	}
	return dexzec.NewTxFromMsgTx(tx, dexzec.MaxExpiryHeight)
}

func dummyInput() *wire.TxIn {
	return wire.NewTxIn(wire.NewOutPoint(&chainhash.Hash{0x01}, 0), nil, nil)
}

func dummyTx() *dexzec.Tx {
	return makeRawTx([]dex.Bytes{encode.RandomBytes(32)}, []*wire.TxIn{dummyInput()})
}

func signRawJSONTx(raw json.RawMessage) (*dexzec.Tx, []byte) {
	var txB dex.Bytes
	if err := json.Unmarshal(raw, &txB); err != nil {
		panic(fmt.Sprintf("error unmarshaling tx: %v", err))
	}
	tx, err := dexzec.DeserializeTx(txB)
	if err != nil {
		panic(fmt.Sprintf("error deserializing tx: %v", err))
	}
	for i := range tx.TxIn {
		tx.TxIn[i].SignatureScript = encode.RandomBytes(dexbtc.RedeemP2PKHSigScriptSize)
	}
	txB, err = tx.Bytes()
	if err != nil {
		panic(fmt.Sprintf("error serializing tx: %v", err))
	}
	return tx, txB
}

func scriptHashAddress(contract []byte, chainParams *chaincfg.Params) (btcutil.Address, error) {
	return btcutil.NewAddressScriptHash(contract, chainParams)
}

func makeSwapContract(lockTimeOffset time.Duration) (secret []byte, secretHash [32]byte, pkScript, contract []byte, addr, contractAddr btcutil.Address, lockTime time.Time) {
	secret = encode.RandomBytes(32)
	secretHash = sha256.Sum256(secret)

	addr, _ = dexzec.DecodeAddress(tAddr, dexzec.RegressionNetAddressParams, dexzec.RegressionNetParams)

	lockTime = time.Now().Add(lockTimeOffset)
	contract, err := dexbtc.MakeContract(addr, addr, secretHash[:], lockTime.Unix(), false, &chaincfg.MainNetParams)
	if err != nil {
		panic("error making swap contract:" + err.Error())
	}
	contractAddr, _ = scriptHashAddress(contract, &chaincfg.RegressionNetParams)
	pkScript, _ = txscript.PayToAddrScript(contractAddr)
	return
}

type tRPCClient struct {
	tipChanged chan asset.WalletNotification
	responses  map[string][]any

	// If there is an "any" key in the getTransactionMap, that value will be
	// returned for all requests. Otherwise the tx id is looked up.
	getTransactionMap map[string]*btc.GetTransactionResult
	getTransactionErr error

	blockchainMtx       sync.RWMutex
	verboseBlocks       map[string]*msgBlockWithHeight
	mainchain           map[int64]*chainhash.Hash
	getBestBlockHashErr error
}

func newRPCClient() *tRPCClient {
	genesisHash := chaincfg.MainNetParams.GenesisHash
	return &tRPCClient{
		responses:  make(map[string][]any),
		tipChanged: make(chan asset.WalletNotification),
		verboseBlocks: map[string]*msgBlockWithHeight{
			genesisHash.String(): {msgBlock: &dexzec.Block{}},
		},
		mainchain: map[int64]*chainhash.Hash{
			0: genesisHash,
		},
	}
}

func (c *tRPCClient) queueResponse(method string, resp any) {
	c.responses[method] = append(c.responses[method], resp)
}

func (c *tRPCClient) checkEmptiness(t *testing.T) {
	t.Helper()
	var stillQueued = map[string]int{}
	for method, resps := range c.responses {
		if len(resps) > 0 {
			stillQueued[method] = len(resps)
			fmt.Printf("Method %s still has %d responses queued \n", method, len(resps))
		}
	}
	if len(stillQueued) > 0 {
		t.Fatalf("response queue not empty: %+v", stillQueued)
	}
}

func (c *tRPCClient) getBlock(blockHash string) *msgBlockWithHeight {
	c.blockchainMtx.Lock()
	defer c.blockchainMtx.Unlock()
	return c.verboseBlocks[blockHash]
}

func (c *tRPCClient) bestBlock() (*chainhash.Hash, int64) {
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
	return bestHash, bestBlkHeight
}

func (c *tRPCClient) getBestBlockHeight() int64 {
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

func (c *tRPCClient) addRawTx(blockHeight int64, tx *dexzec.Tx) (*chainhash.Hash, *dexzec.Block) {
	c.blockchainMtx.Lock()
	defer c.blockchainMtx.Unlock()
	blockHash, found := c.mainchain[blockHeight]
	if !found {
		prevBlock := &chainhash.Hash{}
		if blockHeight > 0 {
			var exists bool
			prevBlock, exists = c.mainchain[blockHeight-1]
			if !exists {
				prevBlock = &chainhash.Hash{}
			}
		}
		nonce, bits := rand.Uint32(), rand.Uint32()
		header := wire.NewBlockHeader(0, prevBlock, &chainhash.Hash{} /* lie, maybe fix this */, bits, nonce)
		header.Timestamp = generateTestBlockTime(blockHeight)
		blk := &dexzec.Block{MsgBlock: *wire.NewMsgBlock(header)} // only now do we know the block hash
		hash := blk.BlockHash()
		blockHash = &hash
		c.verboseBlocks[blockHash.String()] = &msgBlockWithHeight{
			msgBlock: blk,
			height:   blockHeight,
		}
		c.mainchain[blockHeight] = blockHash
	}
	block := c.verboseBlocks[blockHash.String()]
	block.msgBlock.AddTransaction(tx.MsgTx)
	return blockHash, block.msgBlock
}

func (c *tRPCClient) RawRequest(ctx context.Context, method string, args []json.RawMessage) (json.RawMessage, error) {
	respond := func(resp any) (json.RawMessage, error) {
		b, err := json.Marshal(resp)
		if err != nil {
			panic(fmt.Sprintf("error marshaling test response: %v", err))
		}
		return b, nil
	}

	responses := c.responses[method]
	if len(responses) > 0 {
		resp := c.responses[method][0]
		c.responses[method] = c.responses[method][1:]
		if err, is := resp.(error); is {
			return nil, err
		}
		if f, is := resp.(func([]json.RawMessage) (json.RawMessage, error)); is {
			return f(args)
		}
		return respond(resp)
	}

	// Some methods are handled by the testing infrastructure
	switch method {
	case "getbestblockhash":
		c.blockchainMtx.RLock()
		if c.getBestBlockHashErr != nil {
			c.blockchainMtx.RUnlock()
			return nil, c.getBestBlockHashErr
		}
		c.blockchainMtx.RUnlock()
		bestHash, _ := c.bestBlock()
		return respond(bestHash.String())
	case "getblockhash":
		var blockHeight int64
		if err := json.Unmarshal(args[0], &blockHeight); err != nil {
			panic(fmt.Sprintf("error unmarshaling block height: %v", err))
		}
		c.blockchainMtx.RLock()
		defer c.blockchainMtx.RUnlock()
		for height, blockHash := range c.mainchain {
			if height == blockHeight {
				return respond(blockHash.String())
			}
		}
		return nil, fmt.Errorf("block not found")
	case "getblock":
		c.blockchainMtx.Lock()
		defer c.blockchainMtx.Unlock()
		var blockHashStr string
		if err := json.Unmarshal(args[0], &blockHashStr); err != nil {
			panic(fmt.Sprintf("error unmarshaling block hash: %v", err))
		}
		blk, found := c.verboseBlocks[blockHashStr]
		if !found {
			return nil, fmt.Errorf("block not found")
		}
		var buf bytes.Buffer
		err := blk.msgBlock.Serialize(&buf)
		if err != nil {
			return nil, fmt.Errorf("block serialization error: %v", err)
		}
		return respond(hex.EncodeToString(buf.Bytes()))
	case "getblockheader":
		var blkHash string
		if err := json.Unmarshal(args[0], &blkHash); err != nil {
			panic(fmt.Sprintf("error unmarshaling block hash: %v", err))
		}
		block := c.getBlock(blkHash)
		if block == nil {
			return nil, fmt.Errorf("no block verbose found")
		}
		// block may get modified concurrently, lock mtx before reading fields.
		c.blockchainMtx.RLock()
		defer c.blockchainMtx.RUnlock()
		return respond(&btc.BlockHeader{
			Hash:   block.msgBlock.BlockHash().String(),
			Height: block.height,
			// Confirmations: block.Confirmations,
			// Time:          block.Time,
		})
	case "gettransaction":
		if c.getTransactionErr != nil {
			return nil, c.getTransactionErr
		}

		c.blockchainMtx.Lock()
		defer c.blockchainMtx.Unlock()
		var txID string
		if err := json.Unmarshal(args[0], &txID); err != nil {
			panic(fmt.Sprintf("error unmarshaling block hash: %v", err))
		}
		var txData *btc.GetTransactionResult
		if c.getTransactionMap != nil {
			if txData = c.getTransactionMap["any"]; txData == nil {
				txData = c.getTransactionMap[txID]
			}
		}
		if txData == nil {
			return nil, btc.WalletTransactionNotFound
		}
		return respond(txData)
	case "signrawtransaction":
		_, txB := signRawJSONTx(args[0])
		return respond(&btc.SignTxResult{
			Hex:      txB,
			Complete: true,
		})
	}
	return nil, fmt.Errorf("no test response queued for  %q", method)
}

func boolPtr(v bool) *bool {
	return &v
}

func tNewWallet() (*zecWallet, *tRPCClient, func()) {
	dataDir, err := os.MkdirTemp("", "")
	if err != nil {
		panic("couldn't create data dir:" + err.Error())
	}

	cl := newRPCClient()
	walletCfg := &asset.WalletConfig{
		Emit: asset.NewWalletEmitter(cl.tipChanged, BipID, tLogger),
		PeersChange: func(num uint32, err error) {
			fmt.Printf("peer count = %d, err = %v", num, err)
		},
		DataDir: dataDir,
		Settings: map[string]string{
			"rpcuser":     "a",
			"rpcpassword": "b",
		},
	}
	walletCtx, shutdown := context.WithCancel(tCtx)

	wi, err := NewWallet(walletCfg, tLogger, dex.Simnet)
	if err != nil {
		panic(fmt.Sprintf("NewWallet error: %v", err))
	}

	w := wi.(*zecWallet)
	w.node = cl
	bestHash, _ := cl.bestBlock()
	w.currentTip = &btc.BlockVector{
		Height: cl.getBestBlockHeight(),
		Hash:   *bestHash,
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		w.watchBlocks(walletCtx)
	}()
	shutdownAndWait := func() {
		shutdown()
		os.RemoveAll(dataDir)
		wg.Wait()
	}
	return w, cl, shutdownAndWait
}

func TestMain(m *testing.M) {
	tLogger = dex.StdOutLogger("TEST", dex.LevelCritical)
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

func TestFundOrder(t *testing.T) {
	w, cl, shutdown := tNewWallet()
	defer shutdown()
	defer cl.checkEmptiness(t)

	acctBal := &zAccountBalance{}
	queueAccountBals := func() {
		cl.queueResponse(methodZGetBalanceForAccount, acctBal) // 0-conf
		cl.queueResponse(methodZGetBalanceForAccount, acctBal) // minOrchardConfs
		cl.queueResponse(methodZGetNotesCount, &zNotesCount{Orchard: 1})
	}

	// With an empty list returned, there should be no error, but the value zero
	// should be returned.
	unspents := make([]*btc.ListUnspentResult, 0)
	queueAccountBals()
	cl.queueResponse("listlockunspent", []*btc.RPCOutpoint{})

	bal, err := w.Balance()
	if err != nil {
		t.Fatalf("error for zero utxos: %v", err)
	}
	if bal.Available != 0 {
		t.Fatalf("expected available = 0, got %d", bal.Available)
	}
	if bal.Immature != 0 {
		t.Fatalf("expected unconf = 0, got %d", bal.Immature)
	}

	cl.queueResponse(methodZGetBalanceForAccount, tErr)
	_, err = w.Balance()
	if !errorHasCode(err, errBalanceRetrieval) {
		t.Fatalf("wrong error for rpc error: %v", err)
	}

	var littleLots uint64 = 12
	littleOrder := tLotSize * littleLots
	littleFunds := dexzec.RequiredOrderFunds(littleOrder, 1, dexbtc.RedeemP2PKHInputSize, littleLots)
	littleUTXO := &btc.ListUnspentResult{
		TxID:          tTxID,
		Address:       "tmH2m5fi5yY3Qg2GpGwcCrnnoD4wp944RMJ",
		Amount:        float64(littleFunds) / 1e8,
		Confirmations: 1,
		ScriptPubKey:  tP2PKH,
		Spendable:     true,
		Solvable:      true,
		SafePtr:       boolPtr(true),
	}
	unspents = append(unspents, littleUTXO)

	lockedVal := uint64(1e6)
	acctBal = &zAccountBalance{
		Pools: zBalancePools{
			Transparent: valZat{
				ValueZat: littleFunds - lockedVal,
			},
		},
	}

	lockedOutpoints := []*btc.RPCOutpoint{
		{
			TxID: tTxID,
			Vout: 1,
		},
	}

	queueAccountBals()
	cl.queueResponse("gettxout", &btcjson.GetTxOutResult{})
	cl.queueResponse("listlockunspent", lockedOutpoints)

	tx := makeRawTx([]dex.Bytes{{0x01}, {0x02}}, []*wire.TxIn{dummyInput()})
	tx.TxOut[1].Value = int64(lockedVal)
	txB, _ := tx.Bytes()
	const blockHeight = 5
	blockHash, _ := cl.addRawTx(blockHeight, tx)

	cl.getTransactionMap = map[string]*btc.GetTransactionResult{
		"any": {
			BlockHash:  blockHash.String(),
			BlockIndex: blockHeight,
			Bytes:      txB,
		},
	}

	bal, err = w.Balance()
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
	lottaFunds := dexzec.RequiredOrderFunds(lottaOrder, 1, dexbtc.RedeemP2PKHInputSize, lottaLots)
	lottaUTXO := &btc.ListUnspentResult{
		TxID:          tTxID,
		Address:       "tmH2m5fi5yY3Qg2GpGwcCrnnoD4wp944RMJ",
		Amount:        float64(lottaFunds) / 1e8,
		Confirmations: 1,
		Vout:          1,
		ScriptPubKey:  tP2PKH,
		Spendable:     true,
		Solvable:      true,
		SafePtr:       boolPtr(true),
	}
	unspents = append(unspents, lottaUTXO)
	// littleUTXO.Confirmations = 1
	// node.listUnspent = unspents
	// bals.Mine.Trusted += float64(lottaFunds) / 1e8
	// node.getBalances = &bals
	acctBal.Pools.Transparent.ValueZat = littleFunds + lottaFunds - lockedVal
	queueAccountBals()
	cl.queueResponse("gettxout", &btcjson.GetTxOutResult{})
	cl.queueResponse("listlockunspent", lockedOutpoints)
	bal, err = w.Balance()
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
		AssetVersion: version,
		Value:        0,
		MaxSwapCount: 1,
		Options:      make(map[string]string),
	}

	setOrderValue := func(v uint64) {
		ord.Value = v
		ord.MaxSwapCount = v / tLotSize
	}

	queueBalances := func() {
		cl.queueResponse(methodZGetBalanceForAccount, acctBal) // 0-conf
		cl.queueResponse(methodZGetBalanceForAccount, acctBal) // minOrchardConfs
		cl.queueResponse(methodZGetNotesCount, &zNotesCount{Orchard: nActionsOrchardEstimate})
	}

	// Zero value
	_, _, _, err = w.FundOrder(ord)
	if !errorHasCode(err, errNoFundsRequested) {
		t.Fatalf("wrong error for zero value: %v", err)
	}

	// Nothing to spend
	acctBal.Pools.Transparent.ValueZat = 0
	queueBalances()
	cl.queueResponse("listunspent", []*btc.ListUnspentResult{})
	setOrderValue(littleOrder)
	_, _, _, err = w.FundOrder(ord)
	if !errorHasCode(err, errFunding) {
		t.Fatalf("wrong error for zero utxos: %v", err)
	}

	// RPC error
	acctBal.Pools.Transparent.ValueZat = littleFunds + lottaFunds
	queueBalances()
	cl.queueResponse("listunspent", tErr)
	_, _, _, err = w.FundOrder(ord)
	if !errorHasCode(err, errFunding) {
		t.Fatalf("wrong funding error for rpc error: %v", err)
	}

	// Negative response when locking outputs.
	queueBalances()
	cl.queueResponse("listunspent", unspents)
	cl.queueResponse("lockunspent", false)
	_, _, _, err = w.FundOrder(ord)
	if !errorHasCode(err, errLockUnspent) {
		t.Fatalf("wrong error for lockunspent result = false: %v", err)
	}
	// New coin manager with no locked coins. Could also use ReturnCoins
	w.prepareCoinManager()

	queueSuccess := func() {
		queueBalances()
		cl.queueResponse("listunspent", unspents)
		cl.queueResponse("lockunspent", true)
	}

	// Fund a little bit, with zero-conf littleUTXO.
	littleUTXO.Confirmations = 0
	lottaUTXO.Confirmations = 1
	queueSuccess()
	spendables, _, _, err := w.FundOrder(ord)
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
	w.prepareCoinManager()

	// Now with confirmed littleUTXO.
	littleUTXO.Confirmations = 1
	queueSuccess()
	spendables, _, _, err = w.FundOrder(ord)
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
	w.prepareCoinManager()

	// // Adding a fee bump should now require the larger UTXO.
	// ord.Options = map[string]string{swapFeeBumpKey: "1.5"}
	// spendables, _, _, err = wallet.FundOrder(ord)
	// if err != nil {
	// 	t.Fatalf("error funding bumped fees: %v", err)
	// }
	// if len(spendables) != 1 {
	// 	t.Fatalf("expected 1 spendable, got %d", len(spendables))
	// }
	// v = spendables[0].Value()
	// if v != lottaFunds { // picks the bigger output because it is confirmed
	// 	t.Fatalf("expected bumped fee utxo of value %d, got %d", littleFunds, v)
	// }
	// ord.Options = nil
	// littleUTXO.Confirmations = 0
	// _ = wallet.ReturnCoins(spendables)

	// Make lottaOrder unconfirmed like littleOrder, favoring little now.
	littleUTXO.Confirmations = 0
	lottaUTXO.Confirmations = 0
	queueSuccess()
	spendables, _, _, err = w.FundOrder(ord)
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
	w.prepareCoinManager()

	// Fund a lotta bit, covered by just the lottaBit UTXO.
	setOrderValue(lottaOrder)
	queueSuccess()
	spendables, _, fees, err := w.FundOrder(ord)
	if err != nil {
		t.Fatalf("error funding large amount: %v", err)
	}
	if len(spendables) != 1 {
		t.Fatalf("expected 1 spendable, got %d", len(spendables))
	}
	if fees != 0 {
		t.Fatalf("expected no fees, got %d", fees)
	}
	v = spendables[0].Value()
	if v != lottaFunds {
		t.Fatalf("expected spendable of value %d, got %d", lottaFunds, v)
	}
	w.prepareCoinManager()

	// require both spendables
	extraLottaOrder := littleOrder + lottaOrder
	setOrderValue(extraLottaOrder)
	queueSuccess()
	spendables, _, fees, err = w.FundOrder(ord)
	if err != nil {
		t.Fatalf("error funding large amount: %v", err)
	}
	if len(spendables) != 2 {
		t.Fatalf("expected 2 spendable, got %d", len(spendables))
	}
	if fees != 0 {
		t.Fatalf("expected no split tx fees, got %d", fees)
	}
	v = spendables[0].Value()
	if v != lottaFunds {
		t.Fatalf("expected spendable of value %d, got %d", lottaFunds, v)
	}
	w.prepareCoinManager()

	// Not enough to cover transaction fees.
	extraLottaLots := littleLots + lottaLots
	tweak := float64(littleFunds+lottaFunds-dexzec.RequiredOrderFunds(extraLottaOrder, 2, 2*dexbtc.RedeemP2PKHInputSize, extraLottaLots)+1) / 1e8
	lottaUTXO.Amount -= tweak
	// cl.queueResponse("listunspent", unspents)
	_, _, _, err = w.FundOrder(ord)
	if err == nil {
		t.Fatalf("no error when not enough to cover tx fees")
	}
	lottaUTXO.Amount += tweak
	w.prepareCoinManager()

	// Prepare for a split transaction.
	w.walletCfg.Load().(*WalletConfig).UseSplitTx = true
	queueSuccess()
	// No error when no split performed cuz math.
	coins, _, fees, err := w.FundOrder(ord)
	if err != nil {
		t.Fatalf("error for no-split split: %v", err)
	}
	if fees != 0 {
		t.Fatalf("no-split split returned non-zero fees: %d", fees)
	}
	// Should be both coins.
	if len(coins) != 2 {
		t.Fatalf("no-split split didn't return both coins")
	}
	w.prepareCoinManager()

	// No split because not standing order.
	ord.Immediate = true
	queueSuccess()
	coins, _, fees, err = w.FundOrder(ord)
	if err != nil {
		t.Fatalf("error for no-split split: %v", err)
	}
	ord.Immediate = false
	if len(coins) != 2 {
		t.Fatalf("no-split split didn't return both coins")
	}
	if fees != 0 {
		t.Fatalf("no-split split returned non-zero fees: %d", fees)
	}
	w.prepareCoinManager()

	var rawSent bool
	queueSplit := func() {
		cl.queueResponse(methodZGetAddressForAccount, &zGetAddressForAccountResult{Address: tAddr}) // split output
		cl.queueResponse(methodZListUnifiedReceivers, &unifiedReceivers{Transparent: tAddr})
		cl.queueResponse(methodZGetAddressForAccount, &zGetAddressForAccountResult{Address: tAddr}) // change
		cl.queueResponse(methodZListUnifiedReceivers, &unifiedReceivers{Transparent: tAddr})
		cl.queueResponse("sendrawtransaction", func(args []json.RawMessage) (json.RawMessage, error) {
			rawSent = true
			tx, _ := signRawJSONTx(args[0])
			return json.Marshal(tx.TxHash().String())
		})
	}

	// With a little more available, the split should be performed.
	baggageFees := dexzec.TxFeesZIP317(2*dexbtc.RedeemP2PKHInputSize+1, 2*dexbtc.P2PKHOutputSize+1, 0, 0, 0, 0)
	lottaUTXO.Amount += (float64(baggageFees) + 1) / 1e8
	queueSuccess()
	queueSplit()
	coins, _, fees, err = w.FundOrder(ord)
	if err != nil {
		t.Fatalf("error for split tx: %v", err)
	}
	// Should be just one coin.
	if len(coins) != 1 {
		t.Fatalf("split failed - coin count != 1. got %d", len(coins))
	}
	if !rawSent {
		t.Fatalf("split failed - no tx sent")
	}
	if fees != baggageFees {
		t.Fatalf("split returned unexpected fees. wanted %d, got %d", baggageFees, fees)
	}
	w.prepareCoinManager()

	// The split should also be added if we set the option at order time.
	w.walletCfg.Load().(*WalletConfig).UseSplitTx = true
	ord.Options = map[string]string{"swapsplit": "true"}
	queueSuccess()
	queueSplit()
	coins, _, fees, err = w.FundOrder(ord)
	if err != nil {
		t.Fatalf("error for forced split tx: %v", err)
	}
	// Should be just one coin still.
	if len(coins) != 1 {
		t.Fatalf("forced split failed - coin count != 1")
	}
	if fees != baggageFees {
		t.Fatalf("split returned unexpected fees. wanted %d, got %d", baggageFees, fees)
	}
	w.prepareCoinManager()

	// Go back to just enough, but add reserves and get an error.
	lottaUTXO.Amount = float64(lottaFunds) / 1e8
	w.reserves.Store(1)
	cl.queueResponse("listunspent", unspents)
	queueBalances()
	_, _, _, err = w.FundOrder(ord)
	if !errors.Is(err, asset.ErrInsufficientBalance) {
		t.Fatalf("wrong error for reserves rejection: %v", err)
	}

	// Double-check that we're still good
	w.reserves.Store(0)
	queueSuccess()
	_, _, _, err = w.FundOrder(ord)
	if err != nil {
		t.Fatalf("got out of whack somehow")
	}
	w.prepareCoinManager()

	// No reserves, no little UTXO, but shielded funds available for
	// shielded split
	unspents = []*btc.ListUnspentResult{lottaUTXO}
	splitOutputAmt := dexzec.RequiredOrderFunds(ord.Value, 1, dexbtc.RedeemP2PKHInputSize, ord.MaxSwapCount)
	shieldedSplitFees := dexzec.TxFeesZIP317(dexbtc.RedeemP2PKHInputSize+1, dexbtc.P2PKHOutputSize+1, 0, 0, 0, nActionsOrchardEstimate)
	w.lastAddress.Store(tUnifiedAddr)
	acctBal.Pools.Orchard.ValueZat = splitOutputAmt + shieldedSplitFees - lottaFunds
	queueSuccess()
	cl.queueResponse(methodZGetAddressForAccount, &zGetAddressForAccountResult{Address: tAddr}) // split output
	cl.queueResponse(methodZListUnifiedReceivers, &unifiedReceivers{Transparent: tAddr})
	cl.queueResponse(methodZSendMany, "opid-123456")
	txid := dex.Bytes(encode.RandomBytes(32)).String()
	cl.queueResponse(methodZGetOperationResult, []*operationStatus{{
		Status: "success",
		Result: &opResult{
			TxID: txid,
		},
	}})
	btcAddr, _ := dexzec.DecodeAddress(tAddr, w.addrParams, w.btcParams)
	pkScript, _ := txscript.PayToAddrScript(btcAddr)
	msgTx := wire.NewMsgTx(dexzec.VersionNU5)
	msgTx.TxOut = append(msgTx.TxOut, wire.NewTxOut(int64(splitOutputAmt), pkScript))
	txB, _ = dexzec.NewTxFromMsgTx(msgTx, dexzec.MaxExpiryHeight).Bytes()
	cl.getTransactionMap = map[string]*btc.GetTransactionResult{
		txid: {
			Bytes: txB,
		},
	}
	coins, _, fees, err = w.FundOrder(ord)
	if err != nil {
		t.Fatalf("error for shielded split: %v", err)
	}
	if len(coins) != 1 {
		t.Fatalf("shielded split failed - coin count %d != 1", len(coins))
	}
	if fees != shieldedSplitFees {
		t.Fatalf("shielded split returned unexpected fees. wanted %d, got %d", shieldedSplitFees, fees)
	}
}

func TestFundingCoins(t *testing.T) {
	w, cl, shutdown := tNewWallet()
	defer shutdown()
	defer cl.checkEmptiness(t)

	const vout0 = 1
	const txBlockHeight = 3
	tx0 := makeRawTx([]dex.Bytes{{0x01}, tP2PKH}, []*wire.TxIn{dummyInput()})
	txHash0 := tx0.TxHash()
	_, _ = cl.addRawTx(txBlockHeight, tx0)
	coinID0 := btc.ToCoinID(&txHash0, vout0)
	// Make spendable (confs > 0)
	cl.addRawTx(txBlockHeight+1, dummyTx())

	p2pkhUnspent0 := &btc.ListUnspentResult{
		TxID:         txHash0.String(),
		Vout:         vout0,
		ScriptPubKey: tP2PKH,
		Spendable:    true,
		Solvable:     true,
		SafePtr:      boolPtr(true),
		Amount:       1,
	}
	unspents := []*btc.ListUnspentResult{p2pkhUnspent0}

	// Add a second funding coin to make sure more than one iteration of the
	// utxo loops is required.
	const vout1 = 0
	tx1 := makeRawTx([]dex.Bytes{tP2PKH, {0x02}}, []*wire.TxIn{dummyInput()})
	txHash1 := tx1.TxHash()
	_, _ = cl.addRawTx(txBlockHeight, tx1)
	coinID1 := btc.ToCoinID(&txHash1, vout1)
	// Make spendable (confs > 0)
	cl.addRawTx(txBlockHeight+1, dummyTx())

	p2pkhUnspent1 := &btc.ListUnspentResult{
		TxID:         txHash1.String(),
		Vout:         vout1,
		ScriptPubKey: tP2PKH,
		Spendable:    true,
		Solvable:     true,
		SafePtr:      boolPtr(true),
		Amount:       1,
	}
	unspents = append(unspents, p2pkhUnspent1)
	coinIDs := []dex.Bytes{coinID0, coinID1}
	locked := []*btc.RPCOutpoint{}

	queueSuccess := func() {
		cl.queueResponse("listlockunspent", locked)
		cl.queueResponse("listunspent", unspents)
		cl.queueResponse("lockunspent", true)
	}

	ensureGood := func() {
		t.Helper()
		coins, err := w.FundingCoins(coinIDs)
		if err != nil {
			t.Fatalf("FundingCoins error: %v", err)
		}
		if len(coins) != 2 {
			t.Fatalf("expected 2 coins, got %d", len(coins))
		}
	}
	queueSuccess()
	ensureGood()

	ensureErr := func(tag string) {
		t.Helper()
		// Clear the cache.
		w.prepareCoinManager()
		_, err := w.FundingCoins(coinIDs)
		if err == nil {
			t.Fatalf("%s: no error", tag)
		}
	}

	// No coins
	cl.queueResponse("listlockunspent", locked)
	cl.queueResponse("listunspent", []*btc.ListUnspentResult{})
	ensureErr("no coins")

	// RPC error
	cl.queueResponse("listlockunspent", tErr)
	ensureErr("rpc coins")

	// Bad coin ID.
	goodCoinIDs := coinIDs
	coinIDs = []dex.Bytes{encode.RandomBytes(35)}
	ensureErr("bad coin ID")
	coinIDs = goodCoinIDs

	queueSuccess()
	ensureGood()
}

func TestSwap(t *testing.T) {
	w, cl, shutdown := tNewWallet()
	defer shutdown()
	defer cl.checkEmptiness(t)

	swapVal := toZats(5)
	coins := asset.Coins{
		btc.NewOutput(tTxHash, 0, toZats(3)),
		btc.NewOutput(tTxHash, 0, toZats(3)),
	}
	addrStr := tAddr

	privBytes, _ := hex.DecodeString("b07209eec1a8fb6cfe5cb6ace36567406971a75c330db7101fb21bc679bc5330")
	privKey, _ := btcec.PrivKeyFromBytes(privBytes)
	wif, _ := btcutil.NewWIF(privKey, &chaincfg.MainNetParams, true)

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
	}

	var rawSent bool
	queueSuccess := func() {
		cl.queueResponse(methodZGetAddressForAccount, &zGetAddressForAccountResult{Address: tAddr}) // revocation output
		cl.queueResponse(methodZListUnifiedReceivers, &unifiedReceivers{Transparent: tAddr})
		cl.queueResponse(methodZGetAddressForAccount, &zGetAddressForAccountResult{Address: tAddr}) // change output
		cl.queueResponse(methodZListUnifiedReceivers, &unifiedReceivers{Transparent: tAddr})
		cl.queueResponse("dumpprivkey", wif.String())
		cl.queueResponse("sendrawtransaction", func(args []json.RawMessage) (json.RawMessage, error) {
			rawSent = true
			tx, _ := signRawJSONTx(args[0])
			return json.Marshal(tx.TxHash().String())
		})
	}

	// This time should succeed.
	queueSuccess()
	_, _, feesPaid, err := w.Swap(swaps)
	if err != nil {
		t.Fatalf("swap error: %v", err)
	}
	if !rawSent {
		t.Fatalf("tx not sent?")
	}

	// Fees should be returned.
	btcAddr, _ := dexzec.DecodeAddress(tAddr, w.addrParams, w.btcParams)
	contractScript, err := dexbtc.MakeContract(btcAddr, btcAddr, encode.RandomBytes(32), 0, false, w.btcParams)
	if err != nil {
		t.Fatalf("unable to create pubkey script for address %s: %v", contract.Address, err)
	}
	// contracts = append(contracts, contractScript)

	// Make the P2SH address and pubkey script.
	scriptAddr, err := w.scriptHashAddress(contractScript)
	if err != nil {
		t.Fatalf("error encoding script address: %v", err)
	}

	pkScript, err := txscript.PayToAddrScript(scriptAddr)
	if err != nil {
		t.Fatalf("error creating pubkey script: %v", err)
	}
	txOutsSize := uint64(wire.NewTxOut(int64(contract.Value), pkScript).SerializeSize())
	txInsSize := uint64(len(coins))*dexbtc.RedeemP2PKHInputSize + 1
	fees := dexzec.TxFeesZIP317(txInsSize, txOutsSize, 0, 0, 0, 0)
	if feesPaid != fees {
		t.Fatalf("sent fees, %d, less than required fees, %d", feesPaid, fees)
	}

	// Not enough funds
	swaps.Inputs = coins[:1]
	cl.queueResponse(methodZGetAddressForAccount, &zGetAddressForAccountResult{Address: tAddr}) // change address
	cl.queueResponse(methodZListUnifiedReceivers, &unifiedReceivers{Transparent: tAddr})
	_, _, _, err = w.Swap(swaps)
	if !errorHasCode(err, errInsufficientBalance) {
		t.Fatalf("wrong error for listunspent not enough funds: %v", err)
	}
	swaps.Inputs = coins

	// Make sure we can succeed again.
	queueSuccess()
	_, _, _, err = w.Swap(swaps)
	if err != nil {
		t.Fatalf("re-swap error: %v", err)
	}
}

func TestRedeem(t *testing.T) {
	w, cl, shutdown := tNewWallet()
	defer shutdown()
	defer cl.checkEmptiness(t)
	swapVal := toZats(5)

	secret, _, _, contract, _, _, lockTime := makeSwapContract(time.Hour * 12)

	coin := btc.NewOutput(tTxHash, 0, swapVal)
	ci := &asset.AuditInfo{
		Coin:       coin,
		Contract:   contract,
		Recipient:  tAddr,
		Expiration: lockTime,
	}

	redemption := &asset.Redemption{
		Spends: ci,
		Secret: secret,
	}

	privBytes, _ := hex.DecodeString("b07209eec1a8fb6cfe5cb6ace36567406971a75c330db7101fb21bc679bc5330")
	privKey, _ := btcec.PrivKeyFromBytes(privBytes)
	wif, err := btcutil.NewWIF(privKey, &chaincfg.RegressionNetParams, true)
	if err != nil {
		t.Fatalf("error encoding wif: %v", err)
	}

	redemptions := &asset.RedeemForm{
		Redemptions: []*asset.Redemption{redemption},
	}

	cl.queueResponse(methodZGetAddressForAccount, &zGetAddressForAccountResult{Address: tAddr}) // revocation output
	cl.queueResponse(methodZListUnifiedReceivers, &unifiedReceivers{Transparent: tAddr})
	cl.queueResponse("dumpprivkey", wif.String())
	var rawSent bool
	cl.queueResponse("sendrawtransaction", func(args []json.RawMessage) (json.RawMessage, error) {
		rawSent = true
		tx, _ := signRawJSONTx(args[0])
		return json.Marshal(tx.TxHash().String())
	})

	_, _, _, err = w.Redeem(redemptions)
	if err != nil {
		t.Fatalf("redeem error: %v", err)
	}
	if !rawSent {
		t.Fatalf("split failed - no tx sent")
	}
}
func TestSend(t *testing.T) {
	w, cl, shutdown := tNewWallet()
	defer shutdown()
	defer cl.checkEmptiness(t)

	const sendAmt uint64 = 1e8
	w.lastAddress.Store(tUnifiedAddr)

	successOp := []*operationStatus{{
		Status: "success",
		Result: &opResult{TxID: tTxID},
	}}

	dTx := dummyTx()
	txB, _ := dTx.Bytes()
	tx := &btc.GetTransactionResult{
		Bytes: txB,
	}

	// Unified address.
	cl.queueResponse(methodZValidateAddress, &zValidateAddressResult{IsValid: true, AddressType: unifiedAddressType})
	cl.queueResponse(methodZListUnifiedReceivers, &unifiedReceivers{Transparent: tAddr})
	cl.queueResponse(methodZSendMany, "operationid123")
	cl.queueResponse(methodZGetOperationResult, successOp)
	cl.queueResponse("gettransaction", tx)
	if _, err := w.Send(tUnifiedAddr, sendAmt, 0); err != nil {
		t.Fatalf("Send error: %v", err)
	}

	// Transparent address.
	cl.queueResponse(methodZValidateAddress, &zValidateAddressResult{IsValid: true, AddressType: transparentAddressType})
	cl.queueResponse(methodZSendMany, "operationid123")
	cl.queueResponse(methodZGetOperationResult, successOp)
	cl.queueResponse("gettransaction", tx)
	if _, err := w.Send(tAddr, sendAmt, 0); err != nil {
		t.Fatalf("Send error: %v", err)
	}
}

func TestSwapConfirmations(t *testing.T) {
	w, cl, shutdown := tNewWallet()
	defer shutdown()
	defer cl.checkEmptiness(t)

	_, _, pkScript, contract, _, _, _ := makeSwapContract(time.Hour * 12)
	const tipHeight = 10
	const swapHeight = 2
	const expConfs = tipHeight - swapHeight + 1

	tx := makeRawTx([]dex.Bytes{pkScript}, []*wire.TxIn{dummyInput()})
	txB, _ := tx.Bytes()
	blockHash, swapBlock := cl.addRawTx(swapHeight, tx)
	txHash := tx.TxHash()
	cl.getTransactionMap = map[string]*btc.GetTransactionResult{
		txHash.String(): {
			Bytes: txB,
		},
	}
	coinID := btc.ToCoinID(&txHash, 0)
	// Simulate a spending transaction, and advance the tip so that the swap
	// has two confirmations.
	spendingTx := dummyTx()
	spendingTx.TxIn[0].PreviousOutPoint.Hash = txHash

	// Prime the blockchain
	for i := int64(1); i <= tipHeight; i++ {
		cl.addRawTx(i, dummyTx())
	}

	matchTime := swapBlock.Header.Timestamp

	// Bad coin id
	_, _, err := w.SwapConfirmations(tCtx, encode.RandomBytes(35), contract, matchTime)
	if err == nil {
		t.Fatalf("no error for bad coin ID")
	}

	txOutRes := &btcjson.GetTxOutResult{
		Confirmations: expConfs,
		BestBlock:     blockHash.String(),
	}

	cl.queueResponse("gettxout", txOutRes)
	confs, _, err := w.SwapConfirmations(tCtx, coinID, contract, matchTime)
	if err != nil {
		t.Fatalf("error for gettransaction path: %v", err)
	}
	if confs != expConfs {
		t.Fatalf("confs not retrieved from gettxout path. expected %d, got %d", expConfs, confs)
	}

	// no tx output found
	cl.queueResponse("gettxout", nil)
	cl.queueResponse("gettransaction", tErr)
	_, _, err = w.SwapConfirmations(tCtx, coinID, contract, matchTime)
	if !errorHasCode(err, errNoTx) {
		t.Fatalf("wrong error for gettransaction error: %v", err)
	}

	cl.queueResponse("gettxout", nil)
	cl.queueResponse("gettransaction", &dcrjson.RPCError{
		Code: dcrjson.RPCErrorCode(btcjson.ErrRPCNoTxInfo),
	})
	_, _, err = w.SwapConfirmations(tCtx, coinID, contract, matchTime)
	if !errors.Is(err, asset.CoinNotFoundError) {
		t.Fatalf("wrong error for CoinNotFoundError: %v", err)
	}

	cl.queueResponse("gettxout", nil)
	// Will pull transaction from getTransactionMap
	_, _, err = w.SwapConfirmations(tCtx, coinID, contract, matchTime)
	if err != nil {
		t.Fatalf("error for gettransaction path: %v", err)
	}
}

func TestSyncStatus(t *testing.T) {
	w, cl, shutdown := tNewWallet()
	defer shutdown()
	defer cl.checkEmptiness(t)

	bci := &btc.GetBlockchainInfoResult{
		Headers: 100,
		Blocks:  99, // full node allowed to be synced when 1 block behind
	}

	const numConns = 2
	ni := &networkInfo{
		Connections: numConns,
	}

	// ok
	cl.queueResponse("getblockchaininfo", bci)
	cl.queueResponse("getnetworkinfo", ni)
	ss, err := w.SyncStatus()
	if err != nil {
		t.Fatalf("SyncStatus error (synced expected): %v", err)
	}
	if !ss.Synced {
		t.Fatalf("synced = false")
	}
	if ss.BlockProgress() < 1 {
		t.Fatalf("progress not complete when loading last block")
	}

	// getblockchaininfo error
	cl.queueResponse("getblockchaininfo", tErr)
	_, err = w.SyncStatus()
	if !errorHasCode(err, errGetChainInfo) {
		t.Fatalf("SyncStatus wrong error: %v", err)
	}

	// getnetworkinfo error
	cl.queueResponse("getblockchaininfo", bci)
	cl.queueResponse("getnetworkinfo", tErr)
	_, err = w.SyncStatus()
	if !errorHasCode(err, errGetNetInfo) {
		t.Fatalf("SyncStatus wrong error: %v", err)
	}

	// no peers is not synced = false
	ni.Connections = 0
	cl.queueResponse("getblockchaininfo", bci)
	cl.queueResponse("getnetworkinfo", ni)
	ss, err = w.SyncStatus()
	if err != nil {
		t.Fatalf("SyncStatus error (!synced expected): %v", err)
	}
	if ss.Synced {
		t.Fatalf("synced = true")
	}
	ni.Connections = 2

	// No headers is progress = 0
	bci.Headers = 0
	cl.queueResponse("getblockchaininfo", bci)
	ss, err = w.SyncStatus()
	if err != nil {
		t.Fatalf("SyncStatus error (no headers): %v", err)
	}
	if ss.Synced || ss.BlockProgress() != 0 {
		t.Fatal("wrong sync status for no headers", ss.Synced, ss.BlockProgress())
	}
	bci.Headers = 100

	// 50% synced
	w.tipAtConnect.Store(100)
	bci.Headers = 200
	bci.Blocks = 150
	cl.queueResponse("getblockchaininfo", bci)
	ss, err = w.SyncStatus()
	if err != nil {
		t.Fatalf("SyncStatus error (half-synced): %v", err)
	}
	if ss.Synced {
		t.Fatalf("synced = true for 50 blocks to go")
	}
	if ss.BlockProgress() > 0.500001 || ss.BlockProgress() < 0.4999999 {
		t.Fatalf("progress out of range. Expected 0.5, got %.2f", ss.BlockProgress())
	}
}

func TestPreSwap(t *testing.T) {
	w, cl, shutdown := tNewWallet()
	defer shutdown()
	defer cl.checkEmptiness(t)

	// See math from TestFundEdges. 10 lots with max fee rate of 34 sats/vbyte.

	const lots = 10
	swapVal := lots * tLotSize

	singleSwapFees := dexzec.TxFeesZIP317(dexbtc.RedeemP2PKHInputSize+1, dexbtc.P2SHOutputSize+1, 0, 0, 0, 0)
	bestCaseFees := singleSwapFees
	worstCaseFees := lots * bestCaseFees

	minReq := swapVal + worstCaseFees

	unspent := &btc.ListUnspentResult{
		TxID:          tTxID,
		Address:       tAddr,
		Confirmations: 5,
		ScriptPubKey:  tP2PKH,
		Spendable:     true,
		Solvable:      true,
	}
	unspents := []*btc.ListUnspentResult{unspent}

	setFunds := func(v uint64) {
		unspent.Amount = float64(v) / 1e8
	}

	form := &asset.PreSwapForm{
		AssetVersion: version,
		LotSize:      tLotSize,
		Lots:         lots,
		Immediate:    false,
	}

	setFunds(minReq)

	acctBal := &zAccountBalance{}
	queueFunding := func() {
		cl.queueResponse("listunspent", unspents)              // maxOrder
		cl.queueResponse(methodZGetBalanceForAccount, acctBal) // 0-conf
		cl.queueResponse(methodZGetBalanceForAccount, acctBal) // minOrchardConfs
		cl.queueResponse(methodZGetNotesCount, &zNotesCount{Orchard: 1})
	}

	// Initial success.
	queueFunding()
	preSwap, err := w.PreSwap(form)
	if err != nil {
		t.Fatalf("PreSwap error: %v", err)
	}

	if preSwap.Estimate.Lots != lots {
		t.Fatalf("wrong lots. expected %d got %d", lots, preSwap.Estimate.Lots)
	}
	if preSwap.Estimate.MaxFees != worstCaseFees {
		t.Fatalf("wrong worst-case fees. expected %d got %d", worstCaseFees, preSwap.Estimate.MaxFees)
	}
	if preSwap.Estimate.RealisticBestCase != bestCaseFees {
		t.Fatalf("wrong best-case fees. expected %d got %d", bestCaseFees, preSwap.Estimate.RealisticBestCase)
	}
	w.prepareCoinManager()

	// Too little funding is an error.
	setFunds(minReq - 1)
	cl.queueResponse("listunspent", unspents) // maxOrder
	cl.queueResponse(methodZGetBalanceForAccount, &zAccountBalance{
		Pools: zBalancePools{
			Transparent: valZat{
				ValueZat: toZats(unspent.Amount),
			},
		},
	})
	_, err = w.PreSwap(form)
	if err == nil {
		t.Fatalf("no PreSwap error for not enough funds")
	}
	setFunds(minReq)

	// Success again.
	queueFunding()
	_, err = w.PreSwap(form)
	if err != nil {
		t.Fatalf("final PreSwap error: %v", err)
	}
}

func TestConfirmRedemption(t *testing.T) {
	w, cl, shutdown := tNewWallet()
	defer shutdown()
	defer cl.checkEmptiness(t)

	swapVal := toZats(5)

	secret, _, _, contract, addr, _, lockTime := makeSwapContract(time.Hour * 12)

	coin := btc.NewOutput(tTxHash, 0, swapVal)
	coinID := coin.ID()
	ci := &asset.AuditInfo{
		Coin:       coin,
		Contract:   contract,
		Recipient:  addr.String(),
		Expiration: lockTime,
	}

	confirmTx := asset.NewRedeemConfTx(ci, secret)

	walletTx := &btc.GetTransactionResult{
		Confirmations: 1,
	}

	cl.queueResponse("gettransaction", walletTx)
	st, err := w.ConfirmTransaction(coinID, confirmTx, 0)
	if err != nil {
		t.Fatalf("Initial ConfirmTransaction error: %v", err)
	}
	if st.Confs != walletTx.Confirmations {
		t.Fatalf("wrongs confs, %d != %d", st.Confs, walletTx.Confirmations)
	}

	cl.queueResponse("gettransaction", tErr)
	cl.queueResponse("gettxout", tErr)
	_, err = w.ConfirmTransaction(coinID, confirmTx, 0)
	if !errorHasCode(err, errNoTx) {
		t.Fatalf("wrong error for gettxout error: %v", err)
	}

	cl.queueResponse("gettransaction", tErr)
	cl.queueResponse("gettxout", nil)
	st, err = w.ConfirmTransaction(coinID, confirmTx, 0)
	if err != nil {
		t.Fatalf("ConfirmTransaction error for spent redemption: %v", err)
	}
	if st.Confs != requiredConfTxConfirms {
		t.Fatalf("wrong confs for spent redemption: %d != %d", st.Confs, requiredConfTxConfirms)
	}

	// Re-submission path is tested by TestRedemption
}

func TestFundMultiOrder(t *testing.T) {
	w, cl, shutdown := tNewWallet()
	defer shutdown()
	defer cl.checkEmptiness(t)

	const maxLock = 1e16

	// Invalid input
	mo := &asset.MultiOrder{
		Values: []*asset.MultiOrderValue{{}}, // 1 zero-value order
	}
	if _, _, _, err := w.FundMultiOrder(mo, maxLock); !errorHasCode(err, errBadInput) {
		t.Fatalf("wrong error for zero value: %v", err)
	}

	mo = &asset.MultiOrder{
		Values: []*asset.MultiOrderValue{{
			Value:        tLotSize,
			MaxSwapCount: 1,
		}},
	}
	req := dexzec.RequiredOrderFunds(tLotSize, 1, dexbtc.RedeemP2PKHInputSize, 1)

	// maxLock too low
	if _, _, _, err := w.FundMultiOrder(mo, 1); !errorHasCode(err, errMaxLock) {
		t.Fatalf("wrong error for exceeding maxLock: %v", err)
	}

	// Balance error
	cl.queueResponse(methodZGetBalanceForAccount, tErr)
	if _, _, _, err := w.FundMultiOrder(mo, maxLock); !errorHasCode(err, errFunding) {
		t.Fatalf("wrong error for expected balance error: %v", err)
	}

	acctBal := &zAccountBalance{
		Pools: zBalancePools{
			Transparent: valZat{
				ValueZat: req - 1, // too little
			},
		},
	}

	queueBalance := func() {
		cl.queueResponse(methodZGetBalanceForAccount, acctBal) // 0-conf
		cl.queueResponse(methodZGetBalanceForAccount, acctBal) // minOrchardConfs
		cl.queueResponse(methodZGetNotesCount, &zNotesCount{Orchard: 1})
		cl.queueResponse("listlockunspent", nil)
	}

	// Not enough balance
	queueBalance()
	if _, _, _, err := w.FundMultiOrder(mo, maxLock); !errorHasCode(err, errInsufficientBalance) {
		t.Fatalf("wrong low balance error: %v", err)
	}

	// listunspent error
	acctBal.Pools.Transparent.ValueZat = req
	queueBalance()
	cl.queueResponse("listunspent", tErr)
	if _, _, _, err := w.FundMultiOrder(mo, maxLock); !errorHasCode(err, errFunding) {
		t.Fatalf("wrong error for listunspent error: %v", err)
	}

	// Enough without split.
	unspent := &btc.ListUnspentResult{
		ScriptPubKey:  tP2PKH,
		Amount:        float64(req) / 1e8,
		TxID:          tTxID,
		Confirmations: 1,
		Spendable:     true,
	}
	unspents := []*btc.ListUnspentResult{unspent}

	queueBalance()
	cl.queueResponse("listunspent", unspents)
	if _, _, _, err := w.FundMultiOrder(mo, maxLock); err != nil {
		t.Fatalf("error for simple path: %v", err)
	}
}
