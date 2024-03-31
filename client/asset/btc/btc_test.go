//go:build !spvlive && !harness

package btc

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/dex/encode"
	dexbtc "decred.org/dcrdex/dex/networks/btc"
	dexltc "decred.org/dcrdex/dex/networks/ltc"
	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/ecdsa"
	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
)

var (
	tLogger  dex.Logger
	tCtx     context.Context
	tLotSize uint64 = 1e6 // 0.01 BTC
	tBTC            = &dex.Asset{
		ID:           0,
		Symbol:       "btc",
		Version:      version,
		SwapSize:     dexbtc.InitTxSizeSegwit, // patched by tNewWallet, but default to segwit
		SwapSizeBase: dexbtc.InitTxSizeBaseSegwit,
		MaxFeeRate:   34,
		SwapConf:     1,
	}
	optimalFeeRate uint64 = 24
	tErr                  = fmt.Errorf("test error")
	tTxID                 = "308e9a3675fc3ea3862b7863eeead08c621dcc37ff59de597dd3cdab41450ad9"
	tTxHash        *chainhash.Hash
	tP2PKHAddr     = "1Bggq7Vu5oaoLFV1NNp5KhAzcku83qQhgi"
	tP2PKH         []byte
	tP2WPKH        []byte
	tP2WPKHAddr           = "bc1qq49ypf420s0kh52l9pk7ha8n8nhsugdpculjas"
	feeSuggestion  uint64 = 10
)

func boolPtr(v bool) *bool {
	return &v
}

func btcAddr(segwit bool) btcutil.Address {
	var addr btcutil.Address
	if segwit {
		addr, _ = btcutil.DecodeAddress(tP2WPKHAddr, &chaincfg.MainNetParams)
	} else {
		addr, _ = btcutil.DecodeAddress(tP2PKHAddr, &chaincfg.MainNetParams)
	}
	return addr
}

func randBytes(l int) []byte {
	b := make([]byte, l)
	rand.Read(b)
	return b
}

func signFuncRaw(t *testing.T, params []json.RawMessage, sizeTweak int, sigComplete, segwit bool) (json.RawMessage, error) {
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

	signFunc(msgTx, sizeTweak, segwit)

	buf := new(bytes.Buffer)
	err = msgTx.Serialize(buf)
	if err != nil {
		t.Fatalf("error serializing contract: %v", err)
	}
	signTxRes.Hex = buf.Bytes()
	return mustMarshal(signTxRes), nil
}

func signFunc(tx *wire.MsgTx, sizeTweak int, segwit bool) {
	// Set the sigScripts to random bytes of the correct length for spending a
	// p2pkh output.
	if segwit {
		sigSize := 73 + sizeTweak
		for i := range tx.TxIn {
			tx.TxIn[i].Witness = wire.TxWitness{
				randBytes(sigSize),
				randBytes(33),
			}
		}
	} else {
		scriptSize := dexbtc.RedeemP2PKHSigScriptSize + sizeTweak
		for i := range tx.TxIn {
			tx.TxIn[i].SignatureScript = randBytes(scriptSize)
		}
	}
}

type msgBlockWithHeight struct {
	msgBlock *wire.MsgBlock
	height   int64
}

type testData struct {
	walletCfg     *baseWalletConfig
	badSendHash   *chainhash.Hash
	sendErr       error
	sentRawTx     *wire.MsgTx
	txOutRes      *btcjson.GetTxOutResult
	txOutErr      error
	sigIncomplete bool
	signFunc      func(*wire.MsgTx)
	signMsgFunc   func([]json.RawMessage) (json.RawMessage, error)

	blockchainMtx       sync.RWMutex
	verboseBlocks       map[chainhash.Hash]*msgBlockWithHeight
	dbBlockForTx        map[chainhash.Hash]*hashEntry
	mainchain           map[int64]*chainhash.Hash
	getBlockchainInfo   *GetBlockchainInfoResult
	getBestBlockHashErr error

	mempoolTxs        map[chainhash.Hash]*wire.MsgTx
	rawVerboseErr     error
	lockedCoins       []*RPCOutpoint
	estFeeErr         error
	listLockUnspent   []*RPCOutpoint
	getBalances       *GetBalancesResult
	getBalancesErr    error
	lockUnspentErr    error
	changeAddr        string
	changeAddrErr     error
	newAddress        string
	newAddressErr     error
	privKeyForAddr    *btcutil.WIF
	privKeyForAddrErr error
	birthdayTime      time.Time

	// If there is an "any" key in the getTransactionMap, that value will be
	// returned for all requests. Otherwise the tx id is looked up.
	getTransactionMap map[string]*GetTransactionResult
	getTransactionErr error

	getBlockchainInfoErr error
	unlockErr            error
	lockErr              error
	sendToAddress        string
	sendToAddressErr     error
	setTxFee             bool
	signTxErr            error
	listUnspent          []*ListUnspentResult
	listUnspentErr       error
	tipChanged           chan asset.WalletNotification

	// spv
	fetchInputInfoTx  *wire.MsgTx
	getCFilterScripts map[chainhash.Hash][][]byte
	checkpoints       map[OutPoint]*scanCheckpoint
	confs             uint32
	confsSpent        bool
	confsErr          error
	walletTxSpent     bool
	txFee             uint64
	ownedAddresses    map[string]bool
	ownsAddress       bool
	locked            bool
}

func newTestData() *testData {
	// setup genesis block, required by bestblock polling goroutine
	genesisHash := chaincfg.MainNetParams.GenesisHash
	return &testData{
		txOutRes: newTxOutResult([]byte{}, 1, 0),
		verboseBlocks: map[chainhash.Hash]*msgBlockWithHeight{
			*genesisHash: {msgBlock: &wire.MsgBlock{}},
		},
		dbBlockForTx: make(map[chainhash.Hash]*hashEntry),
		mainchain: map[int64]*chainhash.Hash{
			0: genesisHash,
		},
		mempoolTxs:        make(map[chainhash.Hash]*wire.MsgTx),
		fetchInputInfoTx:  dummyTx(),
		getCFilterScripts: make(map[chainhash.Hash][][]byte),
		confsErr:          WalletTransactionNotFound,
		checkpoints:       make(map[OutPoint]*scanCheckpoint),
		tipChanged:        make(chan asset.WalletNotification, 1),
		getTransactionMap: make(map[string]*GetTransactionResult),
	}
}

func (c *testData) getBlock(blockHash chainhash.Hash) *msgBlockWithHeight {
	c.blockchainMtx.Lock()
	defer c.blockchainMtx.Unlock()
	return c.verboseBlocks[blockHash]
}

func (c *testData) GetBestBlockHeight() int64 {
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

func (c *testData) bestBlock() (*chainhash.Hash, int64) {
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

func encodeOrError(thing any, err error) (json.RawMessage, error) {
	if err != nil {
		return nil, err
	}
	return json.Marshal(thing)
}

type tRawRequester struct {
	*testData
}

func (c *tRawRequester) RawRequest(_ context.Context, method string, params []json.RawMessage) (json.RawMessage, error) {
	switch method {
	// TODO: handle methodGetBlockHash and add actual tests to cover it.
	case methodGetNetworkInfo:
		return json.Marshal(&btcjson.GetNetworkInfoResult{
			Connections: 1,
		})
	case methodEstimateSmartFee:
		if c.testData.estFeeErr != nil {
			return nil, c.testData.estFeeErr
		}
		optimalRate := float64(optimalFeeRate) * 1e-5 // ~0.00024
		return json.Marshal(&btcjson.EstimateSmartFeeResult{
			Blocks:  2,
			FeeRate: &optimalRate,
		})
	case methodSendRawTransaction:
		var txHex string
		err := json.Unmarshal(params[0], &txHex)
		if err != nil {
			return nil, err
		}
		tx, err := msgTxFromHex(txHex)
		if err != nil {
			return nil, err
		}
		c.sentRawTx = tx
		if c.sendErr == nil && c.badSendHash == nil {
			h := tx.TxHash().String()
			return json.Marshal(&h)
		}
		if c.sendErr != nil {
			return nil, c.sendErr
		}
		return json.Marshal(c.badSendHash.String())
	case methodGetTxOut:
		return encodeOrError(c.txOutRes, c.txOutErr)
	case methodGetBestBlockHash:
		c.blockchainMtx.RLock()
		if c.getBestBlockHashErr != nil {
			c.blockchainMtx.RUnlock()
			return nil, c.getBestBlockHashErr
		}
		c.blockchainMtx.RUnlock()
		bestHash, _ := c.bestBlock()
		return json.Marshal(bestHash.String())
	case methodGetBlockHash:
		var blockHeight int64
		if err := json.Unmarshal(params[0], &blockHeight); err != nil {
			return nil, err
		}
		c.blockchainMtx.RLock()
		defer c.blockchainMtx.RUnlock()
		for height, blockHash := range c.mainchain {
			if height == blockHeight {
				return json.Marshal(blockHash.String())
			}
		}
		return nil, fmt.Errorf("block not found")

	case methodGetRawMempool:
		hashes := make([]string, 0, len(c.mempoolTxs))
		for txHash := range c.mempoolTxs {
			hashes = append(hashes, txHash.String())
		}
		return json.Marshal(hashes)
	case methodGetRawTransaction:
		if c.testData.rawVerboseErr != nil {
			return nil, c.testData.rawVerboseErr
		}
		var hashStr string
		err := json.Unmarshal(params[0], &hashStr)
		if err != nil {
			return nil, err
		}
		txHash, err := chainhash.NewHashFromStr(hashStr)
		if err != nil {
			return nil, err
		}
		msgTx := c.mempoolTxs[*txHash]
		if msgTx == nil {
			return nil, fmt.Errorf("transaction not found")
		}
		txB, _ := serializeMsgTx(msgTx)
		return json.Marshal(hex.EncodeToString(txB))
	case methodSignTx:
		if c.signTxErr != nil {
			return nil, c.signTxErr
		}
		signTxRes := SignTxResult{
			Complete: !c.sigIncomplete,
		}
		var msgHex string
		err := json.Unmarshal(params[0], &msgHex)
		if err != nil {
			return nil, fmt.Errorf("json.Unmarshal error for tRawRequester -> RawRequest -> methodSignTx: %v", err)
		}
		msgBytes, _ := hex.DecodeString(msgHex)
		txReader := bytes.NewReader(msgBytes)
		msgTx := wire.NewMsgTx(wire.TxVersion)
		err = msgTx.Deserialize(txReader)
		if err != nil {
			return nil, fmt.Errorf("MsgTx.Deserialize error for tRawRequester -> RawRequest -> methodSignTx: %v", err)
		}

		c.signFunc(msgTx)

		buf := new(bytes.Buffer)
		err = msgTx.Serialize(buf)
		if err != nil {
			return nil, fmt.Errorf("MsgTx.Serialize error for tRawRequester -> RawRequest -> methodSignTx: %v", err)
		}
		signTxRes.Hex = buf.Bytes()
		return mustMarshal(signTxRes), nil
	case methodGetBlock:
		c.blockchainMtx.Lock()
		defer c.blockchainMtx.Unlock()
		var blockHashStr string
		err := json.Unmarshal(params[0], &blockHashStr)
		if err != nil {
			return nil, err
		}
		blkHash, err := chainhash.NewHashFromStr(blockHashStr)
		if err != nil {
			return nil, err
		}
		blk, found := c.verboseBlocks[*blkHash]
		if !found {
			return nil, fmt.Errorf("block not found")
		}
		var buf bytes.Buffer
		err = blk.msgBlock.Serialize(&buf)
		if err != nil {
			return nil, err
		}
		return json.Marshal(hex.EncodeToString(buf.Bytes()))

	case methodGetBlockHeader:
		var blkHashStr string
		_ = json.Unmarshal(params[0], &blkHashStr)
		blkHash, err := chainhash.NewHashFromStr(blkHashStr)
		if err != nil {
			return nil, err
		}
		block := c.getBlock(*blkHash)
		if block == nil {
			return nil, fmt.Errorf("no block verbose found")
		}
		// block may get modified concurrently, lock mtx before reading fields.
		c.blockchainMtx.RLock()
		defer c.blockchainMtx.RUnlock()
		return json.Marshal(&BlockHeader{
			Hash:   blkHash.String(),
			Height: block.height,
			// Confirmations: block.Confirmations,
			// Time:          block.Time,
		})
	case methodLockUnspent:
		if c.lockUnspentErr != nil {
			return json.Marshal(false)
		}
		coins := make([]*RPCOutpoint, 0)
		_ = json.Unmarshal(params[1], &coins)
		if string(params[0]) == "false" {
			if c.lockedCoins != nil {
				c.lockedCoins = append(c.lockedCoins, coins...)
			} else {
				c.lockedCoins = coins
			}
		}
		return json.Marshal(true)
	case methodListLockUnspent:
		return mustMarshal(c.listLockUnspent), nil
	case methodGetBalances:
		return encodeOrError(c.getBalances, c.getBalancesErr)
	case methodChangeAddress:
		return encodeOrError(c.changeAddr, c.changeAddrErr)
	case methodNewAddress:
		return encodeOrError(c.newAddress, c.newAddressErr)
	case methodPrivKeyForAddress:
		if c.privKeyForAddrErr != nil {
			return nil, c.privKeyForAddrErr
		}
		return json.Marshal(c.privKeyForAddr.String())
	case methodGetTransaction:
		if c.getTransactionErr != nil {
			return nil, c.getTransactionErr
		}

		c.blockchainMtx.Lock()
		defer c.blockchainMtx.Unlock()
		var txID string
		err := json.Unmarshal(params[0], &txID)
		if err != nil {
			return nil, err
		}

		var txData *GetTransactionResult
		if c.getTransactionMap != nil {
			if txData = c.getTransactionMap["any"]; txData == nil {
				txData = c.getTransactionMap[txID]
			}
		}
		if txData == nil {
			return nil, WalletTransactionNotFound
		}
		return json.Marshal(txData)
	case methodGetBlockchainInfo:
		c.blockchainMtx.RLock()
		defer c.blockchainMtx.RUnlock()
		return encodeOrError(c.getBlockchainInfo, c.getBlockchainInfoErr)
	case methodLock:
		return nil, c.lockErr
	case methodUnlock:
		return nil, c.unlockErr
	case methodSendToAddress:
		return encodeOrError(c.sendToAddress, c.sendToAddressErr)
	case methodSetTxFee:
		return json.Marshal(c.setTxFee)
	case methodListUnspent:
		return encodeOrError(c.listUnspent, c.listUnspentErr)
	case methodFundRawTransaction:
		b, _ := serializeMsgTx(wire.NewMsgTx(wire.TxVersion))
		resp := &struct {
			Transaction string  `json:"hex"`
			Fee         float64 `json:"fee"`
		}{
			Transaction: hex.EncodeToString(b),
			Fee:         toBTC(c.txFee),
		}
		return json.Marshal(resp)
	case methodGetWalletInfo:
		return json.Marshal(&GetWalletInfoResult{UnlockedUntil: nil /* unencrypted -> unlocked */})
	case methodGetAddressInfo:
		var addr string
		err := json.Unmarshal(params[0], &addr)
		if err != nil {
			panic(err)
		}
		owns := c.ownedAddresses != nil && c.ownedAddresses[addr]
		if !owns {
			owns = c.ownsAddress
		}
		return json.Marshal(&btcjson.GetAddressInfoResult{
			IsMine: owns,
		})
	}
	panic("method not registered: " + method)
}

const testBlocksPerBlockTimeOffset = 4

func generateTestBlockTime(blockHeight int64) time.Time {
	return time.Unix(1e6, 0).Add(time.Duration(blockHeight) * maxFutureBlockTime / testBlocksPerBlockTimeOffset)
}

func (c *testData) addRawTx(blockHeight int64, tx *wire.MsgTx) (*chainhash.Hash, *wire.MsgBlock) {
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
		msgBlock := wire.NewMsgBlock(header) // only now do we know the block hash
		hash := msgBlock.BlockHash()
		blockHash = &hash
		c.verboseBlocks[*blockHash] = &msgBlockWithHeight{
			msgBlock: msgBlock,
			height:   blockHeight,
		}
		c.mainchain[blockHeight] = blockHash
	}
	block := c.verboseBlocks[*blockHash]
	// NOTE: Adding a transaction changes the msgBlock.BlockHash() so the
	// map key is technically always wrong.
	block.msgBlock.AddTransaction(tx)
	return blockHash, block.msgBlock
}

func (c *testData) addDBBlockForTx(txHash, blockHash *chainhash.Hash) {
	c.blockchainMtx.Lock()
	defer c.blockchainMtx.Unlock()
	c.dbBlockForTx[*txHash] = &hashEntry{hash: *blockHash}
}

func (c *testData) getBlockAtHeight(blockHeight int64) (*chainhash.Hash, *msgBlockWithHeight) {
	c.blockchainMtx.RLock()
	defer c.blockchainMtx.RUnlock()
	blockHash, found := c.mainchain[blockHeight]
	if !found {
		return nil, nil
	}
	blk := c.verboseBlocks[*blockHash]
	return blockHash, blk
}

func (c *testData) truncateChains() {
	c.blockchainMtx.RLock()
	defer c.blockchainMtx.RUnlock()
	c.mainchain = make(map[int64]*chainhash.Hash)
	c.verboseBlocks = make(map[chainhash.Hash]*msgBlockWithHeight)
	c.mempoolTxs = make(map[chainhash.Hash]*wire.MsgTx)
}

func makeRawTx(pkScripts []dex.Bytes, inputs []*wire.TxIn) *wire.MsgTx {
	tx := &wire.MsgTx{
		TxIn: inputs,
	}
	for _, pkScript := range pkScripts {
		tx.TxOut = append(tx.TxOut, wire.NewTxOut(1, pkScript))
	}
	return tx
}

func makeTxHex(pkScripts []dex.Bytes, inputs []*wire.TxIn) ([]byte, error) {
	msgTx := wire.NewMsgTx(wire.TxVersion)
	for _, txIn := range inputs {
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

// msgTxFromHex creates a wire.MsgTx by deserializing the hex-encoded
// transaction.
func msgTxFromHex(txHex string) (*wire.MsgTx, error) {
	return deserializeMsgTx(hex.NewDecoder(strings.NewReader(txHex)))
}

func makeRPCVin(txHash *chainhash.Hash, vout uint32, sigScript []byte, witness [][]byte) *wire.TxIn {
	return wire.NewTxIn(wire.NewOutPoint(txHash, vout), sigScript, witness)
}

func dummyInput() *wire.TxIn {
	return wire.NewTxIn(wire.NewOutPoint(&chainhash.Hash{0x01}, 0), nil, nil)
}

func dummyTx() *wire.MsgTx {
	return makeRawTx([]dex.Bytes{randBytes(32)}, []*wire.TxIn{dummyInput()})
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

func makeSwapContract(segwit bool, lockTimeOffset time.Duration) (secret []byte, secretHash [32]byte, pkScript, contract []byte, addr, contractAddr btcutil.Address, lockTime time.Time) {
	secret = randBytes(32)
	secretHash = sha256.Sum256(secret)

	addr = btcAddr(segwit)

	lockTime = time.Now().Add(lockTimeOffset)
	contract, err := dexbtc.MakeContract(addr, addr, secretHash[:], lockTime.Unix(), segwit, &chaincfg.MainNetParams)
	if err != nil {
		panic("error making swap contract:" + err.Error())
	}
	contractAddr, _ = scriptHashAddress(segwit, contract, &chaincfg.MainNetParams)
	pkScript, _ = txscript.PayToAddrScript(contractAddr)
	return
}

func tNewWallet(segwit bool, walletType string) (*intermediaryWallet, *testData, func()) {
	if segwit {
		tBTC.SwapSize = dexbtc.InitTxSizeSegwit
		tBTC.SwapSizeBase = dexbtc.InitTxSizeBaseSegwit
	} else {
		tBTC.SwapSize = dexbtc.InitTxSize
		tBTC.SwapSizeBase = dexbtc.InitTxSizeBase
	}

	dataDir, err := os.MkdirTemp("", "")
	if err != nil {
		panic("couldn't create data dir:" + err.Error())
	}

	data := newTestData()
	walletCfg := &asset.WalletConfig{
		Emit: asset.NewWalletEmitter(data.tipChanged, BipID, tLogger),
		PeersChange: func(num uint32, err error) {
			fmt.Printf("peer count = %d, err = %v", num, err)
		},
		DataDir: dataDir,
	}
	walletCtx, shutdown := context.WithCancel(tCtx)
	cfg := &BTCCloneCFG{
		WalletCFG:           walletCfg,
		Symbol:              "btc",
		Logger:              tLogger,
		ChainParams:         &chaincfg.MainNetParams,
		WalletInfo:          WalletInfo,
		DefaultFallbackFee:  defaultFee,
		DefaultFeeRateLimit: defaultFeeRateLimit,
		Segwit:              segwit,
		FeeEstimator:        rpcFeeRate,
		AddressDecoder:      btcutil.DecodeAddress,
	}

	var wallet *intermediaryWallet
	switch walletType {
	case walletTypeRPC:
		wallet, err = newRPCWallet(&tRawRequester{data}, cfg, &RPCWalletConfig{})
	case walletTypeSPV:
		w, err := newUnconnectedWallet(cfg, &WalletConfig{})
		if err == nil {
			neutrinoClient := &tNeutrinoClient{data}
			spvw := &spvWallet{
				chainParams: &chaincfg.MainNetParams,
				cfg:         &WalletConfig{},
				wallet:      &tBtcWallet{data},
				cl:          neutrinoClient,
				tipChan:     make(chan *BlockVector, 1),
				acctNum:     0,
				log:         cfg.Logger.SubLogger("SPV"),
				decodeAddr:  btcutil.DecodeAddress,
			}
			spvw.BlockFiltersScanner = NewBlockFiltersScanner(spvw, spvw.log)
			spvw.txBlocks = data.dbBlockForTx
			spvw.checkpoints = data.checkpoints
			w.setNode(spvw)
			wallet = &intermediaryWallet{
				baseWallet:     w,
				txFeeEstimator: spvw,
				tipRedeemer:    spvw,
			}
			wallet.prepareRedemptionFinder()
		}
	}

	data.walletCfg = wallet.cfgV.Load().(*baseWalletConfig)

	if err != nil {
		shutdown()
		os.RemoveAll(dataDir)
		panic(err.Error())
	}
	// Initialize the best block.
	bestHash, err := wallet.node.getBestBlockHash()
	if err != nil {
		shutdown()
		os.RemoveAll(dataDir)
		panic(err.Error())
	}
	wallet.tipMtx.Lock()
	wallet.currentTip = &BlockVector{
		Height: data.GetBestBlockHeight(),
		Hash:   *bestHash,
	}
	wallet.tipMtx.Unlock()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		wallet.watchBlocks(walletCtx)
	}()
	shutdownAndWait := func() {
		shutdown()
		os.RemoveAll(dataDir)
		wg.Wait()
	}
	return wallet, data, shutdownAndWait
}

func mustMarshal(thing any) []byte {
	b, err := json.Marshal(thing)
	if err != nil {
		panic("mustMarshal error: " + err.Error())
	}
	return b
}

func TestMain(m *testing.M) {
	tLogger = dex.StdOutLogger("TEST", dex.LevelCritical)
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

type testFunc func(t *testing.T, segwit bool, walletType string)

func runRubric(t *testing.T, f testFunc) {
	t.Run("rpc|segwit", func(t *testing.T) {
		f(t, true, walletTypeRPC)
	})
	t.Run("rpc|non-segwit", func(t *testing.T) {
		f(t, false, walletTypeRPC)
	})
	t.Run("spv|segwit", func(t *testing.T) {
		f(t, true, walletTypeSPV)
	})
}

func TestFundMultiOrder(t *testing.T) {
	runRubric(t, testFundMultiOrder)
}

func decodeString(s string) []byte {
	b, _ := hex.DecodeString(s)
	return b
}

func testFundMultiOrder(t *testing.T, segwit bool, walletType string) {
	wallet, node, shutdown := tNewWallet(segwit, walletType)
	defer shutdown()

	maxFeeRate := uint64(200)
	feeSuggestion := uint64(100)

	txIDs := make([]string, 0, 5)
	txHashes := make([]*chainhash.Hash, 0, 5)

	addresses_legacy := []string{
		"n235HrCqx9EcS7teHcJEAthoBF5gvtrAoy",
		"mfjtHyu163DW5ZJXRHkY6kMLtHyWHTH6Qx",
		"mi5QmGr9KwVLM2WVNB9hwSS7KhUx8uNQqU",
		"mhMda8yTy52Avowe34ufbq3D59VoG7jUsy",
		"mhgZ41MUC6EBevkwnhvetkLbBmb61F7Lyr",
	}
	scriptPubKeys_legacy := []dex.Bytes{
		decodeString("76a914e114d5bb20cdbd75f3726f27c10423eb1332576288ac"),
		decodeString("76a91402721143370117f4b37146f6688862892f272a7b88ac"),
		decodeString("76a9141c13a8666a373c29a8a8a270b56816bb43a395e888ac"),
		decodeString("76a914142ce1063712235182613794d6759f62dea8205a88ac"),
		decodeString("76a91417c10273e4236592fd91f3aec1571810bbb0db6888ac"),
	}

	addresses_segwit := []string{
		"bcrt1qy7agjj62epx0ydnqskgwlcfwu52xjtpj36hr0d",
		"bcrt1qhyflz52jwha67dvg92q92k887emxu6jj0ytrrd",
		"bcrt1qus3827tpu7uhreq3f3x5wmfmpgmkd0y7zhjdrs",
		"bcrt1q4fjzhnum2krkurhg55xtadzyf76f8waqj26d0e",
		"bcrt1qqnl9fhnms6gpmlmrwnsq36h4rqxwd3m4plkcw4",
	}
	scriptPubKeys_segwit := []dex.Bytes{
		decodeString("001427ba894b4ac84cf236608590efe12ee514692c32"),
		decodeString("0014b913f1515275fbaf35882a805558e7f6766e6a52"),
		decodeString("0014e422757961e7b971e4114c4d476d3b0a3766bc9e"),
		decodeString("0014aa642bcf9b55876e0ee8a50cbeb4444fb493bba0"),
		decodeString("001404fe54de7b86901dff6374e008eaf5180ce6c775"),
	}
	for i := 0; i < 5; i++ {
		txIDs = append(txIDs, hex.EncodeToString(encode.RandomBytes(32)))
		h, _ := chainhash.NewHashFromStr(txIDs[i])
		txHashes = append(txHashes, h)
	}

	addresses := func(i int) string {
		if segwit {
			return addresses_segwit[i]
		}
		return addresses_legacy[i]
	}

	scriptPubKeys := func(i int) dex.Bytes {
		if segwit {
			return scriptPubKeys_segwit[i]
		}
		return scriptPubKeys_legacy[i]
	}

	addrStr := tP2PKHAddr
	if segwit {
		addrStr = tP2WPKHAddr
	}
	node.changeAddr = addrStr
	node.signFunc = func(tx *wire.MsgTx) {
		signFunc(tx, 0, wallet.segwit)
	}

	expectedSplitFee := func(numInputs, numOutputs uint64) uint64 {
		var inputSize, outputSize uint64
		if segwit {
			inputSize = dexbtc.RedeemP2WPKHInputTotalSize
			outputSize = dexbtc.P2WPKHOutputSize
		} else {
			inputSize = dexbtc.RedeemP2PKHInputSize
			outputSize = dexbtc.P2PKHOutputSize
		}

		return (dexbtc.MinimumTxOverhead + numInputs*inputSize + numOutputs*outputSize) * feeSuggestion
	}

	requiredForOrder := func(value, maxSwapCount uint64) int64 {
		var inputSize uint64
		if segwit {
			inputSize = dexbtc.RedeemP2WPKHInputTotalSize
		} else {
			inputSize = dexbtc.RedeemP2PKHInputSize
		}
		return int64(calc.RequiredOrderFundsAlt(value, inputSize, maxSwapCount,
			wallet.initTxSizeBase, wallet.initTxSize, maxFeeRate))
	}

	type test struct {
		name         string
		multiOrder   *asset.MultiOrder
		allOrNothing bool
		maxLock      uint64
		utxos        []*ListUnspentResult
		bondReserves uint64
		balance      uint64

		// if expectedCoins is nil, all the coins are from
		// the split output. If any of the coins are nil,
		// than that output is from the split output.
		expectedCoins         []asset.Coins
		expectedRedeemScripts [][]dex.Bytes
		expectSendRawTx       bool
		expectedSplitFee      uint64
		expectedInputs        []*wire.TxIn
		expectedOutputs       []*wire.TxOut
		expectedChange        uint64
		expectedLockedCoins   []*RPCOutpoint
		expectErr             bool
	}

	tests := []*test{
		{ // "split not allowed, utxos like split previously done"
			name: "split not allowed, utxos like split previously done",
			multiOrder: &asset.MultiOrder{
				Values: []*asset.MultiOrderValue{
					{
						Value:        1e6,
						MaxSwapCount: 1,
					},
					{
						Value:        2e6,
						MaxSwapCount: 2,
					},
				},
				MaxFeeRate:    maxFeeRate,
				FeeSuggestion: feeSuggestion,
				Options: map[string]string{
					"swapsplit": "false",
				},
			},
			utxos: []*ListUnspentResult{
				{
					Confirmations: 1,
					Spendable:     true,
					TxID:          txIDs[0],
					RedeemScript:  nil,
					ScriptPubKey:  scriptPubKeys(0),
					Address:       addresses(0),
					Amount:        19e5 / 1e8,
					Vout:          0,
				},
				{
					Confirmations: 1,
					Spendable:     true,
					TxID:          txIDs[1],
					RedeemScript:  nil,
					ScriptPubKey:  scriptPubKeys(1),
					Address:       addresses(1),
					Amount:        35e5 / 1e8,
					Vout:          0,
				},
			},
			balance: 35e5,
			expectedCoins: []asset.Coins{
				{NewOutput(txHashes[0], 0, 19e5)},
				{NewOutput(txHashes[1], 0, 35e5)},
			},
			expectedRedeemScripts: [][]dex.Bytes{
				{nil},
				{nil},
			},
		},
		{ // "split not allowed, require multiple utxos per order"
			name: "split not allowed, require multiple utxos per order",
			multiOrder: &asset.MultiOrder{
				Values: []*asset.MultiOrderValue{
					{
						Value:        1e6,
						MaxSwapCount: 1,
					},
					{
						Value:        2e6,
						MaxSwapCount: 2,
					},
				},
				MaxFeeRate:    maxFeeRate,
				FeeSuggestion: feeSuggestion,
				Options: map[string]string{
					"swapsplit": "false",
				},
			},
			allOrNothing: true,
			utxos: []*ListUnspentResult{
				{
					Confirmations: 1,
					Spendable:     true,
					TxID:          txIDs[0],
					RedeemScript:  nil,
					ScriptPubKey:  scriptPubKeys(0),
					Address:       addresses(0),
					Amount:        6e5 / 1e8,
					Vout:          0,
				},
				{
					Confirmations: 1,
					Spendable:     true,
					TxID:          txIDs[1],
					RedeemScript:  nil,
					ScriptPubKey:  scriptPubKeys(1),
					Address:       addresses(1),
					Amount:        5e5 / 1e8,
					Vout:          0,
				},
				{
					Confirmations: 1,
					Spendable:     true,
					TxID:          txIDs[2],
					RedeemScript:  nil,
					ScriptPubKey:  scriptPubKeys(2),
					Address:       addresses(2),
					Amount:        22e5 / 1e8,
					Vout:          0,
				},
			},
			balance: 33e5,
			expectedCoins: []asset.Coins{
				{NewOutput(txHashes[0], 0, 6e5), NewOutput(txHashes[1], 0, 5e5)},
				{NewOutput(txHashes[2], 0, 22e5)},
			},
			expectedRedeemScripts: [][]dex.Bytes{
				{nil, nil},
				{nil},
			},
			expectedLockedCoins: []*RPCOutpoint{
				{txHashes[0].String(), 0},
				{txHashes[1].String(), 0},
				{txHashes[2].String(), 0},
			},
		},
		{ // "split not allowed, can only fund first order and respect maxLock"
			name: "split not allowed, can only fund first order and respect maxLock",
			multiOrder: &asset.MultiOrder{
				Values: []*asset.MultiOrderValue{
					{
						Value:        1e6,
						MaxSwapCount: 1,
					},
					{
						Value:        2e6,
						MaxSwapCount: 2,
					},
				},
				MaxFeeRate:    maxFeeRate,
				FeeSuggestion: feeSuggestion,
				Options: map[string]string{
					"swapsplit": "false",
				},
			},
			maxLock: 32e5,
			utxos: []*ListUnspentResult{
				{
					Confirmations: 1,
					Spendable:     true,
					TxID:          txIDs[2],
					RedeemScript:  nil,
					ScriptPubKey:  scriptPubKeys(2),
					Address:       addresses(2),
					Amount:        1e6 / 1e8,
					Vout:          0,
				},
				{
					Confirmations: 1,
					Spendable:     true,
					TxID:          txIDs[0],
					RedeemScript:  nil,
					ScriptPubKey:  scriptPubKeys(0),
					Address:       addresses(0),
					Amount:        11e5 / 1e8,
					Vout:          0,
				},
				{
					Confirmations: 1,
					Spendable:     true,
					TxID:          txIDs[1],
					RedeemScript:  nil,
					ScriptPubKey:  scriptPubKeys(1),
					Address:       addresses(1),
					Amount:        25e5 / 1e8,
					Vout:          0,
				},
			},
			balance: 46e5,
			expectedCoins: []asset.Coins{
				{NewOutput(txHashes[0], 0, 11e5)},
			},
			expectedRedeemScripts: [][]dex.Bytes{
				{nil},
			},
			expectedLockedCoins: []*RPCOutpoint{
				{txHashes[0].String(), 0},
			},
		},
		{ // "split not allowed, can only fund first order and respect bond reserves"
			name: "no split allowed, can only fund first order and respect bond reserves",
			multiOrder: &asset.MultiOrder{
				Values: []*asset.MultiOrderValue{
					{
						Value:        1e6,
						MaxSwapCount: 1,
					},
					{
						Value:        2e6,
						MaxSwapCount: 2,
					},
				},
				MaxFeeRate:    maxFeeRate,
				FeeSuggestion: feeSuggestion,
				Options: map[string]string{
					multiSplitKey: "false",
				},
			},
			maxLock:      46e5,
			bondReserves: 12e5,
			utxos: []*ListUnspentResult{
				{
					Confirmations: 1,
					Spendable:     true,
					TxID:          txIDs[2],
					RedeemScript:  nil,
					ScriptPubKey:  scriptPubKeys(2),
					Address:       addresses(2),
					Amount:        1e6 / 1e8,
					Vout:          0,
				},
				{
					Confirmations: 1,
					Spendable:     true,
					TxID:          txIDs[0],
					RedeemScript:  nil,
					ScriptPubKey:  scriptPubKeys(0),
					Address:       addresses(0),
					Amount:        11e5 / 1e8,
					Vout:          0,
				},
				{
					Confirmations: 1,
					Spendable:     true,
					TxID:          txIDs[1],
					RedeemScript:  nil,
					ScriptPubKey:  scriptPubKeys(1),
					Address:       addresses(1),
					Amount:        25e5 / 1e8,
					Vout:          0,
				},
			},
			balance: 46e5,
			expectedCoins: []asset.Coins{
				{NewOutput(txHashes[0], 0, 11e5)},
			},
			expectedRedeemScripts: [][]dex.Bytes{
				{nil},
			},
			expectedLockedCoins: []*RPCOutpoint{
				{txHashes[0].String(), 0},
			},
		},
		{ // "split not allowed, need to fund in increasing order"
			name: "no split, need to fund in increasing order",
			multiOrder: &asset.MultiOrder{
				Values: []*asset.MultiOrderValue{
					{
						Value:        2e6,
						MaxSwapCount: 2,
					},
					{
						Value:        11e5,
						MaxSwapCount: 1,
					},
					{
						Value:        9e5,
						MaxSwapCount: 1,
					},
				},
				MaxFeeRate:    maxFeeRate,
				FeeSuggestion: feeSuggestion,
				Options: map[string]string{
					multiSplitKey: "false",
				},
			},
			maxLock: 50e5,
			utxos: []*ListUnspentResult{
				{
					Confirmations: 1,
					Spendable:     true,
					TxID:          txIDs[0],
					RedeemScript:  nil,
					ScriptPubKey:  scriptPubKeys(0),
					Address:       addresses(0),
					Amount:        11e5 / 1e8,
					Vout:          0,
				},
				{
					Confirmations: 1,
					Spendable:     true,
					TxID:          txIDs[1],
					RedeemScript:  nil,
					ScriptPubKey:  scriptPubKeys(1),
					Address:       addresses(1),
					Amount:        13e5 / 1e8,
					Vout:          0,
				},
				{
					Confirmations: 1,
					Spendable:     true,
					TxID:          txIDs[2],
					RedeemScript:  nil,
					ScriptPubKey:  scriptPubKeys(2),
					Address:       addresses(2),
					Amount:        26e5 / 1e8,
					Vout:          0,
				},
			},
			balance: 50e5,
			expectedCoins: []asset.Coins{
				{NewOutput(txHashes[2], 0, 26e5)},
				{NewOutput(txHashes[1], 0, 13e5)},
				{NewOutput(txHashes[0], 0, 11e5)},
			},
			expectedRedeemScripts: [][]dex.Bytes{
				{nil},
				{nil},
				{nil},
			},
			expectedLockedCoins: []*RPCOutpoint{
				{txHashes[0].String(), 0},
				{txHashes[1].String(), 0},
				{txHashes[2].String(), 0},
			},
		},
		{ // "split allowed, no split required"
			name: "split allowed, no split required",
			multiOrder: &asset.MultiOrder{
				Values: []*asset.MultiOrderValue{
					{
						Value:        1e6,
						MaxSwapCount: 1,
					},
					{
						Value:        2e6,
						MaxSwapCount: 2,
					},
				},
				MaxFeeRate:    maxFeeRate,
				FeeSuggestion: feeSuggestion,
				Options: map[string]string{
					multiSplitKey: "true",
				},
			},
			allOrNothing: false,
			maxLock:      43e5,
			utxos: []*ListUnspentResult{
				{
					Confirmations: 1,
					Spendable:     true,
					TxID:          txIDs[2],
					RedeemScript:  nil,
					ScriptPubKey:  scriptPubKeys(2),
					Address:       addresses(2),
					Amount:        1e6 / 1e8,
					Vout:          0,
				},
				{
					Confirmations: 1,
					Spendable:     true,
					TxID:          txIDs[0],
					RedeemScript:  nil,
					ScriptPubKey:  scriptPubKeys(0),
					Address:       addresses(0),
					Amount:        11e5 / 1e8,
					Vout:          0,
				},
				{
					Confirmations: 1,
					Spendable:     true,
					TxID:          txIDs[1],
					RedeemScript:  nil,
					ScriptPubKey:  scriptPubKeys(1),
					Address:       addresses(1),
					Amount:        22e5 / 1e8,
					Vout:          0,
				},
			},
			balance: 43e5,
			expectedCoins: []asset.Coins{
				{NewOutput(txHashes[0], 0, 11e5)},
				{NewOutput(txHashes[1], 0, 22e5)},
			},
			expectedRedeemScripts: [][]dex.Bytes{
				{nil},
				{nil},
			},
			expectedLockedCoins: []*RPCOutpoint{
				{txHashes[0].String(), 0},
				{txHashes[1].String(), 0},
			},
		},
		{ // "split allowed, can fund both with split"
			name: "split allowed, can fund both with split",
			multiOrder: &asset.MultiOrder{
				Values: []*asset.MultiOrderValue{
					{
						Value:        15e5,
						MaxSwapCount: 2,
					},
					{
						Value:        15e5,
						MaxSwapCount: 2,
					},
				},
				MaxFeeRate:    maxFeeRate,
				FeeSuggestion: feeSuggestion,
				Options: map[string]string{
					multiSplitKey: "true",
				},
			},
			utxos: []*ListUnspentResult{
				{
					Confirmations: 1,
					Spendable:     true,
					TxID:          txIDs[0],
					RedeemScript:  nil,
					ScriptPubKey:  scriptPubKeys(0),
					Address:       addresses(0),
					Amount:        1e6 / 1e8,
					Vout:          0,
				},
				{
					Confirmations: 1,
					Spendable:     true,
					TxID:          txIDs[1],
					RedeemScript:  nil,
					ScriptPubKey:  scriptPubKeys(1),
					Address:       addresses(1),
					Amount:        (2*float64(requiredForOrder(15e5, 2)) + float64(expectedSplitFee(2, 2)) - 1e6) / 1e8,
					Vout:          0,
				},
			},
			maxLock:         2*uint64(requiredForOrder(15e5, 2)) + expectedSplitFee(2, 2),
			balance:         2*uint64(requiredForOrder(15e5, 2)) + expectedSplitFee(2, 2),
			expectSendRawTx: true,
			expectedInputs: []*wire.TxIn{
				{
					PreviousOutPoint: wire.OutPoint{
						Hash:  *txHashes[1],
						Index: 0,
					},
				},
				{
					PreviousOutPoint: wire.OutPoint{
						Hash:  *txHashes[0],
						Index: 0,
					},
				},
			},
			expectedOutputs: []*wire.TxOut{
				wire.NewTxOut(requiredForOrder(15e5, 2), []byte{}),
				wire.NewTxOut(requiredForOrder(15e5, 2), []byte{}),
			},
			expectedSplitFee: expectedSplitFee(2, 2),
			expectedRedeemScripts: [][]dex.Bytes{
				{nil},
				{nil},
			},
		},
		{ // "split allowed, cannot fund both with split"
			name: "split allowed, cannot fund both with split",
			multiOrder: &asset.MultiOrder{
				Values: []*asset.MultiOrderValue{
					{
						Value:        15e5,
						MaxSwapCount: 2,
					},
					{
						Value:        15e5,
						MaxSwapCount: 2,
					},
				},
				MaxFeeRate:    maxFeeRate,
				FeeSuggestion: feeSuggestion,
				Options: map[string]string{
					multiSplitKey: "true",
				},
			},
			utxos: []*ListUnspentResult{
				{
					Confirmations: 1,
					Spendable:     true,
					TxID:          txIDs[0],
					RedeemScript:  nil,
					ScriptPubKey:  scriptPubKeys(0),
					Address:       addresses(0),
					Amount:        1e6 / 1e8,
					Vout:          0,
				},
				{
					Confirmations: 1,
					Spendable:     true,
					TxID:          txIDs[1],
					RedeemScript:  nil,
					ScriptPubKey:  scriptPubKeys(1),
					Address:       addresses(1),
					Amount:        (2*float64(requiredForOrder(15e5, 2)) + float64(expectedSplitFee(2, 2)) - 1e6) / 1e8,
					Vout:          0,
				},
			},
			maxLock:   2*uint64(requiredForOrder(15e5, 2)) + expectedSplitFee(2, 2) - 1,
			balance:   2*uint64(requiredForOrder(15e5, 2)) + expectedSplitFee(2, 2) - 1,
			expectErr: true,
		},
		{ // "can fund both with split and respect maxLock"
			name: "can fund both with split and respect maxLock",
			multiOrder: &asset.MultiOrder{
				Values: []*asset.MultiOrderValue{
					{
						Value:        15e5,
						MaxSwapCount: 2,
					},
					{
						Value:        15e5,
						MaxSwapCount: 2,
					},
				},
				MaxFeeRate:    maxFeeRate,
				FeeSuggestion: feeSuggestion,
				Options: map[string]string{
					multiSplitKey: "true",
				},
			},
			utxos: []*ListUnspentResult{
				{
					Confirmations: 1,
					Spendable:     true,
					TxID:          txIDs[0],
					RedeemScript:  nil,
					ScriptPubKey:  scriptPubKeys(0),
					Address:       addresses(0),
					Amount:        float64(50e5) / 1e8,
					Vout:          0,
				},
			},
			balance:         50e5,
			maxLock:         2*uint64(requiredForOrder(15e5, 2)) + expectedSplitFee(1, 2),
			expectSendRawTx: true,
			expectedInputs: []*wire.TxIn{
				{
					PreviousOutPoint: wire.OutPoint{
						Hash:  *txHashes[0],
						Index: 0,
					},
				},
			},
			expectedOutputs: []*wire.TxOut{
				wire.NewTxOut(requiredForOrder(15e5, 2), []byte{}),
				wire.NewTxOut(requiredForOrder(15e5, 2), []byte{}),
			},
			expectedChange:   50e5 - (2*uint64(requiredForOrder(15e5, 2)) + expectedSplitFee(1, 3)),
			expectedSplitFee: expectedSplitFee(1, 3),
			expectedRedeemScripts: [][]dex.Bytes{
				{nil},
				{nil},
			},
		},
		{ // "cannot fund both with split and respect maxLock"
			name: "cannot fund both with split and respect maxLock",
			multiOrder: &asset.MultiOrder{
				Values: []*asset.MultiOrderValue{
					{
						Value:        15e5,
						MaxSwapCount: 2,
					},
					{
						Value:        15e5,
						MaxSwapCount: 2,
					},
				},
				MaxFeeRate:    maxFeeRate,
				FeeSuggestion: feeSuggestion,
				Options: map[string]string{
					multiSplitKey: "true",
				},
			},
			utxos: []*ListUnspentResult{
				{
					Confirmations: 1,
					Spendable:     true,
					TxID:          txIDs[0],
					RedeemScript:  nil,
					ScriptPubKey:  scriptPubKeys(0),
					Address:       addresses(0),
					Amount:        float64(50e5) / 1e8,
					Vout:          0,
				},
			},
			balance:   50e5,
			maxLock:   2*uint64(requiredForOrder(15e5, 2)) + expectedSplitFee(1, 2) - 1,
			expectErr: true,
		},
		{ // "split allowed, can fund both with split with bond reserves"
			name: "split allowed, can fund both with split with bond reserves",
			multiOrder: &asset.MultiOrder{
				Values: []*asset.MultiOrderValue{
					{
						Value:        15e5,
						MaxSwapCount: 2,
					},
					{
						Value:        15e5,
						MaxSwapCount: 2,
					},
				},
				MaxFeeRate:    maxFeeRate,
				FeeSuggestion: feeSuggestion,
				Options: map[string]string{
					multiSplitKey: "true",
				},
			},
			bondReserves: 2e6,
			utxos: []*ListUnspentResult{
				{
					Confirmations: 1,
					Spendable:     true,
					TxID:          txIDs[0],
					RedeemScript:  nil,
					ScriptPubKey:  scriptPubKeys(0),
					Address:       addresses(0),
					Amount:        (2*float64(requiredForOrder(15e5, 2)) + 2e6 + float64(expectedSplitFee(1, 3))) / 1e8,
					Vout:          0,
				},
			},
			balance:         2e6 + 2*uint64(requiredForOrder(15e5, 2)) + expectedSplitFee(1, 3),
			maxLock:         2e6 + 2*uint64(requiredForOrder(15e5, 2)) + expectedSplitFee(1, 3),
			expectSendRawTx: true,
			expectedInputs: []*wire.TxIn{
				{
					PreviousOutPoint: wire.OutPoint{
						Hash:  *txHashes[0],
						Index: 0,
					},
				},
			},
			expectedOutputs: []*wire.TxOut{
				wire.NewTxOut(requiredForOrder(15e5, 2), []byte{}),
				wire.NewTxOut(requiredForOrder(15e5, 2), []byte{}),
			},
			expectedChange:   2e6,
			expectedSplitFee: expectedSplitFee(1, 3),
			expectedRedeemScripts: [][]dex.Bytes{
				{nil},
				{nil},
			},
		},
		{ // "split allowed, cannot fund both with split and keep and bond reserves"
			name: "split allowed, cannot fund both with split and keep and bond reserves",
			multiOrder: &asset.MultiOrder{
				Values: []*asset.MultiOrderValue{
					{
						Value:        15e5,
						MaxSwapCount: 2,
					},
					{
						Value:        15e5,
						MaxSwapCount: 2,
					},
				},
				MaxFeeRate:    maxFeeRate,
				FeeSuggestion: feeSuggestion,
				Options: map[string]string{
					multiSplitKey: "true",
				},
			},
			bondReserves: 2e6,
			utxos: []*ListUnspentResult{
				{
					Confirmations: 1,
					Spendable:     true,
					TxID:          txIDs[0],
					RedeemScript:  nil,
					ScriptPubKey:  scriptPubKeys(0),
					Address:       addresses(0),
					Amount:        ((2*float64(requiredForOrder(15e5, 2)) + 2e6 + float64(expectedSplitFee(1, 3))) / 1e8) - 1/1e8,
					Vout:          0,
				},
			},
			balance:   2e6 + 2*uint64(requiredForOrder(15e5, 2)) + expectedSplitFee(1, 3) - 1,
			maxLock:   2e6 + 2*uint64(requiredForOrder(15e5, 2)) + expectedSplitFee(1, 3) - 1,
			expectErr: true,
		},
		{ // "split with buffer"
			name: "split with buffer",
			multiOrder: &asset.MultiOrder{
				Values: []*asset.MultiOrderValue{
					{
						Value:        15e5,
						MaxSwapCount: 2,
					},
					{
						Value:        15e5,
						MaxSwapCount: 2,
					},
				},
				MaxFeeRate:    maxFeeRate,
				FeeSuggestion: feeSuggestion,
				Options: map[string]string{
					multiSplitKey:       "true",
					multiSplitBufferKey: "10",
				},
			},
			utxos: []*ListUnspentResult{
				{
					Confirmations: 1,
					Spendable:     true,
					TxID:          txIDs[0],
					RedeemScript:  nil,
					ScriptPubKey:  scriptPubKeys(0),
					Address:       addresses(0),
					Amount:        (2*float64(requiredForOrder(15e5, 2)*110/100) + float64(expectedSplitFee(1, 2))) / 1e8,
					Vout:          0,
				},
			},
			balance:         2*uint64(requiredForOrder(15e5, 2)*110/100) + expectedSplitFee(1, 2),
			maxLock:         2*uint64(requiredForOrder(15e5, 2)*110/100) + expectedSplitFee(1, 2),
			expectSendRawTx: true,
			expectedInputs: []*wire.TxIn{
				{
					PreviousOutPoint: wire.OutPoint{
						Hash:  *txHashes[0],
						Index: 0,
					},
				},
			},
			expectedOutputs: []*wire.TxOut{
				wire.NewTxOut(requiredForOrder(15e5, 2)*110/100, []byte{}),
				wire.NewTxOut(requiredForOrder(15e5, 2)*110/100, []byte{}),
			},
			expectedSplitFee: expectedSplitFee(1, 2),
			expectedRedeemScripts: [][]dex.Bytes{
				{nil},
				{nil},
			},
		},
		{ // "split, maxLock too low to fund buffer"
			name: "split, maxLock too low to fund buffer",
			multiOrder: &asset.MultiOrder{
				Values: []*asset.MultiOrderValue{
					{
						Value:        15e5,
						MaxSwapCount: 2,
					},
					{
						Value:        15e5,
						MaxSwapCount: 2,
					},
				},
				MaxFeeRate:    maxFeeRate,
				FeeSuggestion: feeSuggestion,
				Options: map[string]string{
					multiSplitKey:       "true",
					multiSplitBufferKey: "10",
				},
			},
			utxos: []*ListUnspentResult{
				{
					Confirmations: 1,
					Spendable:     true,
					TxID:          txIDs[0],
					RedeemScript:  nil,
					ScriptPubKey:  scriptPubKeys(0),
					Address:       addresses(0),
					Amount:        (2*float64(requiredForOrder(15e5, 2)*110/100) + float64(expectedSplitFee(1, 2))) / 1e8,
					Vout:          0,
				},
			},
			balance:   2*uint64(requiredForOrder(15e5, 2)*110/100) + expectedSplitFee(1, 2),
			maxLock:   2*uint64(requiredForOrder(15e5, 2)*110/100) + expectedSplitFee(1, 2) - 1,
			expectErr: true,
		},
		{ // "only one order needs a split, rest can be funded without"
			name: "only one order needs a split, rest can be funded without",
			multiOrder: &asset.MultiOrder{
				Values: []*asset.MultiOrderValue{
					{
						Value:        1e6,
						MaxSwapCount: 2,
					},
					{
						Value:        1e6,
						MaxSwapCount: 2,
					},
					{
						Value:        1e6,
						MaxSwapCount: 2,
					},
				},
				MaxFeeRate:    maxFeeRate,
				FeeSuggestion: feeSuggestion,
				Options: map[string]string{
					multiSplitKey: "true",
				},
			},
			utxos: []*ListUnspentResult{
				{
					Confirmations: 1,
					Spendable:     true,
					TxID:          txIDs[0],
					RedeemScript:  nil,
					ScriptPubKey:  scriptPubKeys(0),
					Address:       addresses(0),
					Amount:        12e5 / 1e8,
					Vout:          0,
				},
				{
					Confirmations: 1,
					Spendable:     true,
					TxID:          txIDs[1],
					RedeemScript:  nil,
					ScriptPubKey:  scriptPubKeys(1),
					Address:       addresses(1),
					Amount:        12e5 / 1e8,
					Vout:          0,
				},
				{
					Confirmations: 1,
					Spendable:     true,
					TxID:          txIDs[2],
					RedeemScript:  nil,
					ScriptPubKey:  scriptPubKeys(2),
					Address:       addresses(2),
					Amount:        120e5 / 1e8,
					Vout:          0,
				},
			},
			maxLock:         50e5,
			balance:         144e5,
			expectSendRawTx: true,
			expectedInputs: []*wire.TxIn{
				{
					PreviousOutPoint: wire.OutPoint{
						Hash:  *txHashes[2],
						Index: 0,
					},
				},
			},
			expectedOutputs: []*wire.TxOut{
				wire.NewTxOut(requiredForOrder(1e6, 2), []byte{}),
				wire.NewTxOut(120e5-requiredForOrder(1e6, 2)-int64(expectedSplitFee(1, 2)), []byte{}),
			},
			expectedSplitFee: expectedSplitFee(1, 2),
			expectedRedeemScripts: [][]dex.Bytes{
				{nil},
				{nil},
				{nil},
			},
			expectedCoins: []asset.Coins{
				{NewOutput(txHashes[0], 0, 12e5)},
				{NewOutput(txHashes[1], 0, 12e5)},
				nil,
			},
		},
		{ // "only one order needs a split due to bond reserves, rest funded without"
			name: "only one order needs a split, rest can be funded without",
			multiOrder: &asset.MultiOrder{
				Values: []*asset.MultiOrderValue{
					{
						Value:        1e6,
						MaxSwapCount: 2,
					},
					{
						Value:        1e6,
						MaxSwapCount: 2,
					},
					{
						Value:        1e6,
						MaxSwapCount: 2,
					},
				},
				MaxFeeRate:    maxFeeRate,
				FeeSuggestion: feeSuggestion,
				Options: map[string]string{
					multiSplitKey: "true",
				},
			},
			utxos: []*ListUnspentResult{
				{
					Confirmations: 1,
					Spendable:     true,
					TxID:          txIDs[0],
					RedeemScript:  nil,
					ScriptPubKey:  scriptPubKeys(0),
					Address:       addresses(0),
					Amount:        12e5 / 1e8,
					Vout:          0,
				},
				{
					Confirmations: 1,
					Spendable:     true,
					TxID:          txIDs[1],
					RedeemScript:  nil,
					ScriptPubKey:  scriptPubKeys(1),
					Address:       addresses(1),
					Amount:        12e5 / 1e8,
					Vout:          0,
				},
				{
					Confirmations: 1,
					Spendable:     true,
					TxID:          txIDs[2],
					RedeemScript:  nil,
					ScriptPubKey:  scriptPubKeys(2),
					Address:       addresses(2),
					Amount:        120e5 / 1e8,
					Vout:          0,
				},
			},
			maxLock:         0,
			bondReserves:    1e6,
			balance:         144e5,
			expectSendRawTx: true,
			expectedInputs: []*wire.TxIn{
				{
					PreviousOutPoint: wire.OutPoint{
						Hash:  *txHashes[2],
						Index: 0,
					},
				},
			},
			expectedOutputs: []*wire.TxOut{
				wire.NewTxOut(requiredForOrder(1e6, 2), []byte{}),
				wire.NewTxOut(120e5-requiredForOrder(1e6, 2)-int64(expectedSplitFee(1, 2)), []byte{}),
			},
			expectedSplitFee: expectedSplitFee(1, 2),
			expectedRedeemScripts: [][]dex.Bytes{
				{nil},
				{nil},
				{nil},
			},
			expectedCoins: []asset.Coins{
				{NewOutput(txHashes[0], 0, 12e5)},
				{NewOutput(txHashes[1], 0, 12e5)},
				nil,
			},
		},
	}

	for _, test := range tests {
		node.listUnspent = test.utxos
		node.sentRawTx = nil
		node.lockedCoins = nil
		node.getBalances = &GetBalancesResult{
			Mine: Balances{
				Trusted: toBTC(test.balance),
			},
		}
		wallet.cm.lockedOutputs = make(map[OutPoint]*UTxO)
		wallet.bondReserves.Store(test.bondReserves)

		allCoins, _, splitFee, err := wallet.FundMultiOrder(test.multiOrder, test.maxLock)
		if test.expectErr {
			if err == nil {
				t.Fatalf("%s: no error returned", test.name)
			}
			if strings.Contains(err.Error(), "insufficient funds") {
				t.Fatalf("%s: unexpected insufficient funds error", test.name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", test.name, err)
		}

		if !test.expectSendRawTx { // no split
			if node.sentRawTx != nil {
				t.Fatalf("%s: unexpected transaction sent", test.name)
			}
			if len(allCoins) != len(test.expectedCoins) {
				t.Fatalf("%s: expected %d coins, got %d", test.name, len(test.expectedCoins), len(allCoins))
			}
			for i := range allCoins {
				if len(allCoins[i]) != len(test.expectedCoins[i]) {
					t.Fatalf("%s: expected %d coins in set %d, got %d", test.name, len(test.expectedCoins[i]), i, len(allCoins[i]))
				}
				actual := allCoins[i]
				expected := test.expectedCoins[i]
				sort.Slice(actual, func(i, j int) bool {
					return bytes.Compare(actual[i].ID(), actual[j].ID()) < 0
				})
				sort.Slice(expected, func(i, j int) bool {
					return bytes.Compare(expected[i].ID(), expected[j].ID()) < 0
				})
				for j := range actual {
					if !bytes.Equal(actual[j].ID(), expected[j].ID()) {
						t.Fatalf("%s: unexpected coin in set %d. expected %s, got %s", test.name, i, expected[j].ID(), actual[j].ID())
					}
					if actual[j].Value() != expected[j].Value() {
						t.Fatalf("%s: unexpected coin value in set %d. expected %d, got %d", test.name, i, expected[j].Value(), actual[j].Value())
					}
				}
			}
		} else { // expectSplit
			if node.sentRawTx == nil {
				t.Fatalf("%s: SendRawTransaction not called", test.name)
			}
			if len(node.sentRawTx.TxIn) != len(test.expectedInputs) {
				t.Fatalf("%s: expected %d inputs, got %d", test.name, len(test.expectedInputs), len(node.sentRawTx.TxIn))
			}
			for i, actualIn := range node.sentRawTx.TxIn {
				expectedIn := test.expectedInputs[i]
				if !bytes.Equal(actualIn.PreviousOutPoint.Hash[:], expectedIn.PreviousOutPoint.Hash[:]) {
					t.Fatalf("%s: unexpected input %d hash. expected %s, got %s", test.name, i, expectedIn.PreviousOutPoint.Hash, actualIn.PreviousOutPoint.Hash)
				}
				if actualIn.PreviousOutPoint.Index != expectedIn.PreviousOutPoint.Index {
					t.Fatalf("%s: unexpected input %d index. expected %d, got %d", test.name, i, expectedIn.PreviousOutPoint.Index, actualIn.PreviousOutPoint.Index)
				}
			}
			expectedNumOutputs := len(test.expectedOutputs)
			if test.expectedChange > 0 {
				expectedNumOutputs++
			}
			if len(node.sentRawTx.TxOut) != expectedNumOutputs {
				t.Fatalf("%s: expected %d outputs, got %d", test.name, expectedNumOutputs, len(node.sentRawTx.TxOut))
			}

			for i, expectedOut := range test.expectedOutputs {
				actualOut := node.sentRawTx.TxOut[i]
				if actualOut.Value != expectedOut.Value {
					t.Fatalf("%s: unexpected output %d value. expected %d, got %d", test.name, i, expectedOut.Value, actualOut.Value)
				}
			}
			if test.expectedChange > 0 {
				actualOut := node.sentRawTx.TxOut[len(node.sentRawTx.TxOut)-1]
				if uint64(actualOut.Value) != test.expectedChange {
					t.Fatalf("%s: unexpected change value. expected %d, got %d", test.name, test.expectedChange, actualOut.Value)
				}
			}

			if len(test.multiOrder.Values) != len(allCoins) {
				t.Fatalf("%s: expected %d coins, got %d", test.name, len(test.multiOrder.Values), len(allCoins))
			}
			splitTxID := node.sentRawTx.TxHash()

			// This means all coins are split outputs
			if test.expectedCoins == nil {
				for i, actualCoin := range allCoins {
					actualOut := actualCoin[0].(*Output)
					expectedOut := node.sentRawTx.TxOut[i]
					if uint64(expectedOut.Value) != actualOut.Val {
						t.Fatalf("%s: unexpected output %d value. expected %d, got %d", test.name, i, expectedOut.Value, actualOut.Val)
					}
					if !bytes.Equal(actualOut.Pt.TxHash[:], splitTxID[:]) {
						t.Fatalf("%s: unexpected output %d txid. expected %s, got %s", test.name, i, splitTxID, actualOut.Pt.TxHash)
					}
				}
			} else {
				var splitTxOutputIndex int
				for i := range allCoins {
					actual := allCoins[i]
					expected := test.expectedCoins[i]

					// This means the coins are the split outputs
					if expected == nil {
						actualOut := actual[0].(*Output)
						expectedOut := node.sentRawTx.TxOut[splitTxOutputIndex]
						if uint64(expectedOut.Value) != actualOut.Val {
							t.Fatalf("%s: unexpected output %d value. expected %d, got %d", test.name, i, expectedOut.Value, actualOut.Val)
						}
						if !bytes.Equal(actualOut.Pt.TxHash[:], splitTxID[:]) {
							t.Fatalf("%s: unexpected output %d txid. expected %s, got %s", test.name, i, splitTxID, actualOut.Pt.TxHash)
						}
						splitTxOutputIndex++
						continue
					}

					if len(actual) != len(expected) {
						t.Fatalf("%s: expected %d coins in set %d, got %d", test.name, len(test.expectedCoins[i]), i, len(allCoins[i]))
					}
					sort.Slice(actual, func(i, j int) bool {
						return bytes.Compare(actual[i].ID(), actual[j].ID()) < 0
					})
					sort.Slice(expected, func(i, j int) bool {
						return bytes.Compare(expected[i].ID(), expected[j].ID()) < 0
					})
					for j := range actual {
						if !bytes.Equal(actual[j].ID(), expected[j].ID()) {
							t.Fatalf("%s: unexpected coin in set %d. expected %s, got %s", test.name, i, expected[j].ID(), actual[j].ID())
						}
						if actual[j].Value() != expected[j].Value() {
							t.Fatalf("%s: unexpected coin value in set %d. expected %d, got %d", test.name, i, expected[j].Value(), actual[j].Value())
						}
					}
				}
			}

			// Each split output should be locked
			if len(node.lockedCoins) != len(allCoins)+len(test.expectedInputs) {
				t.Fatalf("%s: expected %d locked coins, got %d", test.name, len(allCoins)+len(test.expectedInputs), len(node.lockedCoins))
			}

		}

		// Check that the right coins are locked and in the fundingCoins map
		var totalNumCoins int
		for _, coins := range allCoins {
			totalNumCoins += len(coins)
		}
		if totalNumCoins != len(wallet.cm.lockedOutputs) {
			t.Fatalf("%s: expected %d funding coins in wallet, got %d", test.name, totalNumCoins, len(wallet.cm.lockedOutputs))
		}
		totalNumCoins += len(test.expectedInputs)
		if totalNumCoins != len(node.lockedCoins) {
			t.Fatalf("%s: expected %d locked coins, got %d", test.name, totalNumCoins, len(node.lockedCoins))
		}
		lockedCoins := make(map[RPCOutpoint]any)
		for _, coin := range node.lockedCoins {
			lockedCoins[*coin] = true
		}
		checkLockedCoin := func(txHash chainhash.Hash, vout uint32) {
			if _, ok := lockedCoins[RPCOutpoint{TxID: txHash.String(), Vout: vout}]; !ok {
				t.Fatalf("%s: expected locked coin %s:%d not found", test.name, txHash, vout)
			}
		}
		checkFundingCoin := func(txHash chainhash.Hash, vout uint32) {
			if _, ok := wallet.cm.lockedOutputs[OutPoint{TxHash: txHash, Vout: vout}]; !ok {
				t.Fatalf("%s: expected locked coin %s:%d not found in wallet", test.name, txHash, vout)
			}
		}
		for _, coins := range allCoins {
			for _, coin := range coins {
				// decode coin to output
				out := coin.(*Output)
				checkLockedCoin(out.Pt.TxHash, out.Pt.Vout)
				checkFundingCoin(out.Pt.TxHash, out.Pt.Vout)
			}
		}
		for _, expectedIn := range test.expectedInputs {
			checkLockedCoin(expectedIn.PreviousOutPoint.Hash, expectedIn.PreviousOutPoint.Index)
		}

		if test.expectedSplitFee != splitFee {
			t.Fatalf("%s: unexpected split fee. expected %d, got %d", test.name, test.expectedSplitFee, splitFee)
		}
	}
}

func TestMaxFundingFees(t *testing.T) {
	runRubric(t, testMaxFundingFees)
}

func testMaxFundingFees(t *testing.T, segwit bool, walletType string) {
	wallet, _, shutdown := tNewWallet(segwit, walletType)
	defer shutdown()

	maxFeeRate := uint64(100)

	useSplitOptions := map[string]string{
		multiSplitKey: "true",
	}
	noSplitOptions := map[string]string{
		multiSplitKey: "false",
	}

	var inputSize, outputSize uint64
	if segwit {
		inputSize = dexbtc.RedeemP2WPKHInputTotalSize
		outputSize = dexbtc.P2WPKHOutputSize
	} else {
		inputSize = dexbtc.RedeemP2PKHInputSize
		outputSize = dexbtc.P2PKHOutputSize
	}

	const maxSwaps = 3
	const numInputs = 12
	maxFundingFees := wallet.MaxFundingFees(maxSwaps, maxFeeRate, useSplitOptions)
	expectedFees := maxFeeRate * (inputSize*numInputs + outputSize*(maxSwaps+1) + dexbtc.MinimumTxOverhead)
	if maxFundingFees != expectedFees {
		t.Fatalf("unexpected max funding fees. expected %d, got %d", expectedFees, maxFundingFees)
	}

	maxFundingFees = wallet.MaxFundingFees(maxSwaps, maxFeeRate, noSplitOptions)
	if maxFundingFees != 0 {
		t.Fatalf("unexpected max funding fees. expected 0, got %d", maxFundingFees)
	}
}

func TestAvailableFund(t *testing.T) {
	runRubric(t, testAvailableFund)
}

func testAvailableFund(t *testing.T, segwit bool, walletType string) {
	wallet, node, shutdown := tNewWallet(segwit, walletType)
	defer shutdown()

	// With an empty list returned, there should be no error, but the value zero
	// should be returned.
	unspents := make([]*ListUnspentResult, 0)
	node.listUnspent = unspents // only needed for Fund, not Balance
	node.listLockUnspent = []*RPCOutpoint{}
	var bals GetBalancesResult
	node.getBalances = &bals
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

	node.getBalancesErr = tErr
	_, err = wallet.Balance()
	if err == nil {
		t.Fatalf("no wallet error for rpc error")
	}
	node.getBalancesErr = nil
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
		SafePtr:       boolPtr(true),
	}
	unspents = append(unspents, littleUTXO)
	node.listUnspent = unspents
	bals.Mine.Trusted = float64(littleFunds) / 1e8
	node.getBalances = &bals
	lockedVal := uint64(1e6)
	node.listLockUnspent = []*RPCOutpoint{
		{
			TxID: tTxID,
			Vout: 1,
		},
	}

	msgTx := makeRawTx([]dex.Bytes{{0x01}, {0x02}}, []*wire.TxIn{dummyInput()})
	msgTx.TxOut[1].Value = int64(lockedVal)
	txBuf := bytes.NewBuffer(make([]byte, 0, dexbtc.MsgTxVBytes(msgTx)))
	msgTx.Serialize(txBuf)
	const blockHeight = 5
	blockHash, _ := node.addRawTx(blockHeight, msgTx)

	node.getTransactionMap = map[string]*GetTransactionResult{
		"any": {
			BlockHash:  blockHash.String(),
			BlockIndex: blockHeight,
			Bytes:      txBuf.Bytes(),
		}}

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
		SafePtr:       boolPtr(true),
	}
	unspents = append(unspents, lottaUTXO)
	littleUTXO.Confirmations = 1
	node.listUnspent = unspents
	bals.Mine.Trusted += float64(lottaFunds) / 1e8
	node.getBalances = &bals
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
		Version:       version,
		Value:         0,
		MaxSwapCount:  1,
		MaxFeeRate:    tBTC.MaxFeeRate,
		FeeSuggestion: feeSuggestion,
	}

	setOrderValue := func(v uint64) {
		ord.Value = v
		ord.MaxSwapCount = v / tLotSize
	}

	// Zero value
	_, _, _, err = wallet.FundOrder(ord)
	if err == nil {
		t.Fatalf("no funding error for zero value")
	}

	// Nothing to spend
	node.listUnspent = []*ListUnspentResult{}
	setOrderValue(littleOrder)
	_, _, _, err = wallet.FundOrder(ord)
	if err == nil {
		t.Fatalf("no error for zero utxos")
	}
	node.listUnspent = unspents

	// RPC error
	node.listUnspentErr = tErr
	_, _, _, err = wallet.FundOrder(ord)
	if err == nil {
		t.Fatalf("no funding error for rpc error")
	}
	node.listUnspentErr = nil

	// Negative response when locking outputs.
	// There is no way to error locking outpoints in spv
	if walletType != walletTypeSPV {
		node.lockUnspentErr = tErr
		_, _, _, err = wallet.FundOrder(ord)
		if err == nil {
			t.Fatalf("no error for lockunspent result = false: %v", err)
		}
		node.lockUnspentErr = nil
	}

	// Fund a little bit, with unsafe littleUTXO.
	littleUTXO.SafePtr = boolPtr(false)
	littleUTXO.Confirmations = 0
	node.listUnspent = unspents
	spendables, _, _, err := wallet.FundOrder(ord)
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

	// Return/unlock the reserved coins to avoid warning in subsequent tests
	// about fundingCoins map containing the coins already. i.e.
	// "Known order-funding coin %v returned by listunspent"

	_ = wallet.ReturnCoins(spendables)

	// Now with safe confirmed littleUTXO.
	littleUTXO.SafePtr = boolPtr(true)
	littleUTXO.Confirmations = 2
	node.listUnspent = unspents
	spendables, _, _, err = wallet.FundOrder(ord)
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
	_ = wallet.ReturnCoins(spendables)

	// Adding a fee bump should now require the larger UTXO.
	ord.Options = map[string]string{swapFeeBumpKey: "1.5"}
	spendables, _, _, err = wallet.FundOrder(ord)
	if err != nil {
		t.Fatalf("error funding bumped fees: %v", err)
	}
	if len(spendables) != 1 {
		t.Fatalf("expected 1 spendable, got %d", len(spendables))
	}
	v = spendables[0].Value()
	if v != lottaFunds { // picks the bigger output because it is confirmed
		t.Fatalf("expected bumped fee utxo of value %d, got %d", littleFunds, v)
	}
	ord.Options = nil
	littleUTXO.Confirmations = 0
	_ = wallet.ReturnCoins(spendables)

	// Make lottaOrder unconfirmed like littleOrder, favoring little now.
	lottaUTXO.Confirmations = 0
	node.listUnspent = unspents
	spendables, _, _, err = wallet.FundOrder(ord)
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
	_ = wallet.ReturnCoins(spendables)

	// Fund a lotta bit, covered by just the lottaBit UTXO.
	setOrderValue(lottaOrder)
	spendables, _, fees, err := wallet.FundOrder(ord)
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
	_ = wallet.ReturnCoins(spendables)

	// require both spendables
	extraLottaOrder := littleOrder + lottaOrder
	extraLottaLots := littleLots + lottaLots
	setOrderValue(extraLottaOrder)
	spendables, _, fees, err = wallet.FundOrder(ord)
	if err != nil {
		t.Fatalf("error funding large amount: %v", err)
	}
	if len(spendables) != 2 {
		t.Fatalf("expected 2 spendable, got %d", len(spendables))
	}
	if fees != 0 {
		t.Fatalf("expected no fees, got %d", fees)
	}
	v = spendables[0].Value()
	if v != lottaFunds {
		t.Fatalf("expected spendable of value %d, got %d", lottaFunds, v)
	}
	_ = wallet.ReturnCoins(spendables)

	// Not enough to cover transaction fees.
	tweak := float64(littleFunds+lottaFunds-calc.RequiredOrderFunds(extraLottaOrder, 2*dexbtc.RedeemP2PKHInputSize, extraLottaLots, tBTC)+1) / 1e8
	lottaUTXO.Amount -= tweak
	node.listUnspent = unspents
	_, _, _, err = wallet.FundOrder(ord)
	if err == nil {
		t.Fatalf("no error when not enough to cover tx fees")
	}
	lottaUTXO.Amount += tweak
	node.listUnspent = unspents

	// Prepare for a split transaction.
	baggageFees := tBTC.MaxFeeRate * splitTxBaggage
	if segwit {
		node.changeAddr = tP2WPKHAddr
	} else {
		node.changeAddr = tP2PKHAddr
	}
	node.walletCfg.useSplitTx = true
	// No error when no split performed cuz math.
	coins, _, fees, err := wallet.FundOrder(ord)
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
	_ = wallet.ReturnCoins(coins)

	// No split because not standing order.
	ord.Immediate = true
	coins, _, fees, err = wallet.FundOrder(ord)
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
	_ = wallet.ReturnCoins(coins)

	var inputSize, outputSize uint64
	if wallet.segwit {
		inputSize = dexbtc.RedeemP2WPKHInputTotalSize
		outputSize = dexbtc.P2WPKHOutputSize
	} else {
		inputSize = dexbtc.RedeemP2PKHInputSize
		outputSize = dexbtc.P2PKHOutputSize
	}
	expectedTxSize := dexbtc.MinimumTxOverhead + 2*inputSize + 2*outputSize
	if wallet.segwit {
		expectedTxSize -= 1 // double counted wittness flag
	}
	expectedFees := expectedTxSize * feeSuggestion

	// With a little more locked, the split should be performed.
	node.signFunc = func(tx *wire.MsgTx) {
		signFunc(tx, 0, wallet.segwit)
	}
	lottaUTXO.Amount += float64(baggageFees) / 1e8
	node.listUnspent = unspents
	coins, _, fees, err = wallet.FundOrder(ord)
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
	if fees != expectedFees {
		t.Fatalf("split returned unexpected fees. wanted %d, got %d", expectedFees, fees)
	}
	_ = wallet.ReturnCoins(coins)

	// The split should also be added if we set the option at order time.
	node.walletCfg.useSplitTx = false
	ord.Options = map[string]string{splitKey: "true"}
	coins, _, fees, err = wallet.FundOrder(ord)
	if err != nil {
		t.Fatalf("error for forced split tx: %v", err)
	}
	// Should be just one coin still.
	if len(coins) != 1 {
		t.Fatalf("forced split failed - coin count != 1")
	}
	if fees != expectedFees {
		t.Fatalf("split returned unexpected fees. wanted %d, got %d", expectedFees, fees)
	}
	_ = wallet.ReturnCoins(coins)

	// // Hit some error paths.

	// Split transaction requires valid fee suggestion.
	// TODO:
	// 1.0: Error when no suggestion.
	// ord.FeeSuggestion = 0
	// _, _, err = wallet.FundOrder(ord)
	// if err == nil {
	// 	t.Fatalf("no error for no fee suggestions on split tx")
	// }
	ord.FeeSuggestion = tBTC.MaxFeeRate + 1
	_, _, _, err = wallet.FundOrder(ord)
	if err == nil {
		t.Fatalf("no error for no fee suggestions on split tx")
	}
	// Check success again.
	ord.FeeSuggestion = tBTC.MaxFeeRate
	coins, _, _, err = wallet.FundOrder(ord)
	if err != nil {
		t.Fatalf("error fixing split tx: %v", err)
	}
	if fees != expectedFees {
		t.Fatalf("expected fees of %d, got %d", expectedFees, fees)
	}
	_ = wallet.ReturnCoins(coins)

	// GetRawChangeAddress error
	node.changeAddrErr = tErr
	_, _, _, err = wallet.FundOrder(ord)
	if err == nil {
		t.Fatalf("no error for split tx change addr error")
	}
	node.changeAddrErr = nil

	// SendRawTx error
	node.sendErr = tErr
	_, _, _, err = wallet.FundOrder(ord)
	if err == nil {
		t.Fatalf("no error for split tx send error")
	}
	node.sendErr = nil

	// Success again.
	spendables, _, _, err = wallet.FundOrder(ord)
	if err != nil {
		t.Fatalf("error for split tx recovery run")
	}
	_ = wallet.ReturnCoins(spendables)
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
func (c *tCoin) String() string { return hex.EncodeToString(c.id) }
func (c *tCoin) TxID() string   { return hex.EncodeToString(c.id) }
func (c *tCoin) Value() uint64  { return 100 }

func TestReturnCoins(t *testing.T) {
	wallet, node, shutdown := tNewWallet(true, walletTypeRPC)
	defer shutdown()

	// Test it with the local output type.
	coins := asset.Coins{
		NewOutput(tTxHash, 0, 1),
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

	// nil unlocks all
	wallet.cm.lockedOutputs[OutPoint{*tTxHash, 0}] = &UTxO{}
	err = wallet.ReturnCoins(nil)
	if err != nil {
		t.Fatalf("error for nil coins: %v", err)
	}
	if len(wallet.cm.lockedOutputs) != 0 {
		t.Errorf("all funding coins not unlocked")
	}

	// Have the RPC return negative response.
	node.lockUnspentErr = tErr
	err = wallet.ReturnCoins(coins)
	if err == nil {
		t.Fatalf("no error for RPC failure")
	}
	node.lockUnspentErr = nil

	// ReturnCoins should accept any type that implements asset.Coin.
	err = wallet.ReturnCoins(asset.Coins{&tCoin{}, &tCoin{}})
	if err != nil {
		t.Fatalf("error with custom coin type: %v", err)
	}
}

func TestFundingCoins(t *testing.T) {
	// runRubric(t, testFundingCoins)
	testFundingCoins(t, false, walletTypeRPC)
}

func testFundingCoins(t *testing.T, segwit bool, walletType string) {
	wallet, node, shutdown := tNewWallet(segwit, walletType)
	defer shutdown()

	const vout0 = 1
	const txBlockHeight = 3
	tx0 := makeRawTx([]dex.Bytes{{0x01}, tP2PKH}, []*wire.TxIn{dummyInput()})
	txHash0 := tx0.TxHash()
	_, _ = node.addRawTx(txBlockHeight, tx0)
	coinID0 := ToCoinID(&txHash0, vout0)
	// Make spendable (confs > 0)
	node.addRawTx(txBlockHeight+1, dummyTx())

	p2pkhUnspent0 := &ListUnspentResult{
		TxID:         txHash0.String(),
		Vout:         vout0,
		ScriptPubKey: tP2PKH,
		Spendable:    true,
		Solvable:     true,
		SafePtr:      boolPtr(true),
		Amount:       1,
	}
	unspents := []*ListUnspentResult{p2pkhUnspent0}

	// Add a second funding coin to make sure more than one iteration of the
	// utxo loops is required.
	const vout1 = 0
	tx1 := makeRawTx([]dex.Bytes{tP2PKH, {0x02}}, []*wire.TxIn{dummyInput()})
	txHash1 := tx1.TxHash()
	_, _ = node.addRawTx(txBlockHeight, tx1)
	coinID1 := ToCoinID(&txHash1, vout1)
	// Make spendable (confs > 0)
	node.addRawTx(txBlockHeight+1, dummyTx())

	p2pkhUnspent1 := &ListUnspentResult{
		TxID:         txHash1.String(),
		Vout:         vout1,
		ScriptPubKey: tP2PKH,
		Spendable:    true,
		Solvable:     true,
		SafePtr:      boolPtr(true),
		Amount:       1,
	}
	unspents = append(unspents, p2pkhUnspent1)

	node.listLockUnspent = []*RPCOutpoint{}
	node.listUnspent = unspents
	coinIDs := []dex.Bytes{coinID0, coinID1}

	ensureGood := func() {
		t.Helper()
		coins, err := wallet.FundingCoins(coinIDs)
		if err != nil {
			t.Fatalf("FundingCoins error: %v", err)
		}
		if len(coins) != 2 {
			t.Fatalf("expected 2 coins, got %d", len(coins))
		}
	}
	ensureGood()

	ensureErr := func(tag string) {
		t.Helper()
		// Clear the cache.
		wallet.cm.lockedOutputs = make(map[OutPoint]*UTxO)
		_, err := wallet.FundingCoins(coinIDs)
		if err == nil {
			t.Fatalf("%s: no error", tag)
		}
	}

	// No coins
	node.listUnspent = []*ListUnspentResult{}
	ensureErr("no coins")
	node.listUnspent = unspents

	// RPC error
	node.listUnspentErr = tErr
	ensureErr("rpc coins")
	node.listUnspentErr = nil

	// Bad coin ID.
	ogIDs := coinIDs
	coinIDs = []dex.Bytes{randBytes(35)}
	ensureErr("bad coin ID")
	coinIDs = ogIDs

	// Coins locked but not in wallet.fundingCoins.
	irrelevantTx := dummyTx()
	node.addRawTx(txBlockHeight+1, irrelevantTx)
	node.listLockUnspent = []*RPCOutpoint{
		{TxID: p2pkhUnspent0.TxID, Vout: p2pkhUnspent0.Vout},
		{TxID: p2pkhUnspent1.TxID, Vout: p2pkhUnspent1.Vout},
	}
	node.listUnspent = []*ListUnspentResult{}

	txRaw0, _ := serializeMsgTx(tx0)
	getTxRes0 := &GetTransactionResult{
		Bytes: txRaw0,
	}
	txRaw1, _ := serializeMsgTx(tx1)
	getTxRes1 := &GetTransactionResult{
		Bytes: txRaw1,
	}

	node.getTransactionMap = map[string]*GetTransactionResult{
		txHash0.String(): getTxRes0,
		txHash1.String(): getTxRes1,
	}

	ensureGood()
}

func checkMaxOrder(t *testing.T, wallet asset.Wallet, lots, swapVal, maxFees, estWorstCase, estBestCase uint64) {
	t.Helper()
	maxOrder, err := wallet.MaxOrder(&asset.MaxOrderForm{
		LotSize:       tLotSize,
		FeeSuggestion: feeSuggestion,
		AssetVersion:  version,
		MaxFeeRate:    tBTC.MaxFeeRate,
	})
	if err != nil {
		t.Fatalf("MaxOrder error: %v", err)
	}
	checkSwapEstimate(t, maxOrder, lots, swapVal, maxFees, estWorstCase, estBestCase)
}

func checkSwapEstimate(t *testing.T, est *asset.SwapEstimate, lots, swapVal, maxFees, estWorstCase, estBestCase uint64) {
	t.Helper()
	if est.Lots != lots {
		t.Fatalf("Estimate has wrong Lots. wanted %d, got %d", lots, est.Lots)
	}
	if est.Value != swapVal {
		t.Fatalf("Estimate has wrong Value. wanted %d, got %d", swapVal, est.Value)
	}
	if est.MaxFees != maxFees {
		t.Fatalf("Estimate has wrong MaxFees. wanted %d, got %d", maxFees, est.MaxFees)
	}
	if est.RealisticWorstCase != estWorstCase {
		t.Fatalf("Estimate has wrong RealisticWorstCase. wanted %d, got %d", estWorstCase, est.RealisticWorstCase)
	}
	if est.RealisticBestCase != estBestCase {
		t.Fatalf("Estimate has wrong RealisticBestCase. wanted %d, got %d", estBestCase, est.RealisticBestCase)
	}
}

func TestFundEdges(t *testing.T) {
	wallet, node, shutdown := tNewWallet(false, walletTypeRPC)
	defer shutdown()
	swapVal := uint64(1e7)
	lots := swapVal / tLotSize

	// Base Fees
	// fee_rate: 34 satoshi / vbyte (MaxFeeRate)
	// swap_size: 225 bytes (InitTxSize)
	// p2pkh input: 149 bytes (RedeemP2PKHInputSize)

	// NOTE: Shouldn't swap_size_base be 73 bytes?

	// swap_size_base: 76 bytes (225 - 149 p2pkh input) (InitTxSizeBase)
	// lot_size: 1e6
	// swap_value: 1e7
	// lots = swap_value / lot_size = 10
	//   total_bytes = first_swap_size + chained_swap_sizes
	//   chained_swap_sizes = (lots - 1) * swap_size
	//   first_swap_size = swap_size_base + backing_bytes
	//   total_bytes = swap_size_base + backing_bytes + (lots - 1) * swap_size
	//   base_tx_bytes = total_bytes - backing_bytes
	// base_tx_bytes = (lots - 1) * swap_size + swap_size_base = 9 * 225 + 76 = 2101
	// base_fees = base_tx_bytes * fee_rate = 2101 * 34 = 71434
	// backing_bytes: 1x P2PKH inputs = dexbtc.P2PKHInputSize = 149 bytes
	// backing_fees: 149 * fee_rate(34 atoms/byte) = 5066 atoms
	// total_bytes  = base_tx_bytes + backing_bytes = 2101 + 149 = 2250
	// total_fees: base_fees + backing_fees = 71434 + 5066 = 76500 atoms
	//          OR total_bytes * fee_rate = 2250 * 34 = 76500
	// base_best_case_bytes = swap_size_base + backing_bytes
	//                      = 76 + 149 = 225
	const swapSize = 225
	const totalBytes = 2250
	const bestCaseBytes = 225
	backingFees := uint64(totalBytes) * tBTC.MaxFeeRate // total_bytes * fee_rate
	p2pkhUnspent := &ListUnspentResult{
		TxID:          tTxID,
		Address:       tP2PKHAddr,
		Amount:        float64(swapVal+backingFees-1) / 1e8,
		Confirmations: 5,
		ScriptPubKey:  tP2PKH,
		Spendable:     true,
		Solvable:      true,
		SafePtr:       boolPtr(true),
	}
	unspents := []*ListUnspentResult{p2pkhUnspent}
	node.listUnspent = unspents
	ord := &asset.Order{
		Version:       version,
		Value:         swapVal,
		MaxSwapCount:  lots,
		MaxFeeRate:    tBTC.MaxFeeRate,
		FeeSuggestion: feeSuggestion,
	}

	var feeReduction uint64 = swapSize * tBTC.MaxFeeRate
	estFeeReduction := swapSize * feeSuggestion
	splitFees := splitTxBaggage * tBTC.MaxFeeRate
	checkMaxOrder(t, wallet, lots-1, swapVal-tLotSize, backingFees+splitFees-feeReduction,
		(totalBytes+splitTxBaggage)*feeSuggestion-estFeeReduction,
		(bestCaseBytes+splitTxBaggage)*feeSuggestion)

	_, _, _, err := wallet.FundOrder(ord)
	if err == nil {
		t.Fatalf("no error when not enough funds in single p2pkh utxo")
	}
	// Now add the needed satoshi and try again.
	p2pkhUnspent.Amount = float64(swapVal+backingFees) / 1e8
	node.listUnspent = unspents

	checkMaxOrder(t, wallet, lots, swapVal, backingFees, totalBytes*feeSuggestion,
		bestCaseBytes*feeSuggestion)

	spendables, _, _, err := wallet.FundOrder(ord)
	if err != nil {
		t.Fatalf("error when should be enough funding in single p2pkh utxo: %v", err)
	}
	_ = wallet.ReturnCoins(spendables)

	// For a split transaction, we would need to cover the splitTxBaggage as
	// well.
	node.walletCfg.useSplitTx = true
	node.changeAddr = tP2WPKHAddr
	node.signFunc = func(tx *wire.MsgTx) {
		signFunc(tx, 0, wallet.segwit)
	}
	backingFees = uint64(totalBytes+splitTxBaggage) * tBTC.MaxFeeRate
	// 1 too few atoms
	v := swapVal + backingFees - 1
	p2pkhUnspent.Amount = float64(v) / 1e8
	node.listUnspent = unspents

	coins, _, _, err := wallet.FundOrder(ord)
	if err != nil {
		t.Fatalf("error when skipping split tx due to baggage: %v", err)
	}
	if coins[0].Value() != v {
		t.Fatalf("split performed when baggage wasn't covered")
	}
	_ = wallet.ReturnCoins(spendables)

	// Just enough.
	v = swapVal + backingFees
	p2pkhUnspent.Amount = float64(v) / 1e8
	node.listUnspent = unspents

	checkMaxOrder(t, wallet, lots, swapVal, backingFees,
		(totalBytes+splitTxBaggage)*feeSuggestion,
		(bestCaseBytes+splitTxBaggage)*feeSuggestion)

	coins, _, _, err = wallet.FundOrder(ord)
	if err != nil {
		t.Fatalf("error funding split tx: %v", err)
	}
	if coins[0].Value() == v {
		t.Fatalf("split performed when baggage wasn't covered")
	}
	_ = wallet.ReturnCoins(spendables)
	node.walletCfg.useSplitTx = false

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
		SafePtr:       boolPtr(true),
	}
	p2pkhUnspent.Amount = float64(halfSwap+backingFees-1) / 1e8
	unspents = []*ListUnspentResult{p2pkhUnspent, p2shUnspent}
	node.listUnspent = unspents
	_, _, _, err = wallet.FundOrder(ord)
	if err == nil {
		t.Fatalf("no error when not enough funds in two utxos")
	}
	p2pkhUnspent.Amount = float64(halfSwap+backingFees) / 1e8
	node.listUnspent = unspents
	_, _, _, err = wallet.FundOrder(ord)
	if err != nil {
		t.Fatalf("error when should be enough funding in two utxos: %v", err)
	}
	_ = wallet.ReturnCoins(spendables)

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
		SafePtr:       boolPtr(true),
	}
	unspents = []*ListUnspentResult{p2wpkhUnspent}
	node.listUnspent = unspents
	_, _, _, err = wallet.FundOrder(ord)
	if err == nil {
		t.Fatalf("no error when not enough funds in single p2wpkh utxo")
	}
	p2wpkhUnspent.Amount = float64(swapVal+backingFees) / 1e8
	node.listUnspent = unspents
	_, _, _, err = wallet.FundOrder(ord)
	if err != nil {
		t.Fatalf("error when should be enough funding in single p2wpkh utxo: %v", err)
	}
	_ = wallet.ReturnCoins(spendables)

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
		SafePtr:       boolPtr(true),
	}
	unspents = []*ListUnspentResult{p2wpshUnspent}
	node.listUnspent = unspents
	_, _, _, err = wallet.FundOrder(ord)
	if err == nil {
		t.Fatalf("no error when not enough funds in single p2wsh utxo")
	}
	p2wpshUnspent.Amount = float64(swapVal+backingFees) / 1e8
	node.listUnspent = unspents
	_, _, _, err = wallet.FundOrder(ord)
	if err != nil {
		t.Fatalf("error when should be enough funding in single p2wsh utxo: %v", err)
	}
	_ = wallet.ReturnCoins(spendables)
}

func TestFundEdgesSegwit(t *testing.T) {
	wallet, node, shutdown := tNewWallet(true, walletTypeRPC)
	defer shutdown()
	swapVal := uint64(1e7)
	lots := swapVal / tLotSize

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
	// base_best_case_bytes = swap_size_base + backing_bytes
	//                      = 84 + 69 = 153
	const swapSize = 153
	const totalBytes = 1530
	const bestCaseBytes = 153
	backingFees := uint64(totalBytes) * tBTC.MaxFeeRate // total_bytes * fee_rate
	p2wpkhUnspent := &ListUnspentResult{
		TxID:          tTxID,
		Address:       tP2WPKHAddr,
		Amount:        float64(swapVal+backingFees-1) / 1e8,
		Confirmations: 5,
		ScriptPubKey:  tP2WPKH,
		Spendable:     true,
		Solvable:      true,
		SafePtr:       boolPtr(true),
	}
	unspents := []*ListUnspentResult{p2wpkhUnspent}
	node.listUnspent = unspents
	ord := &asset.Order{
		Version:       version,
		Value:         swapVal,
		MaxSwapCount:  lots,
		MaxFeeRate:    tBTC.MaxFeeRate,
		FeeSuggestion: feeSuggestion,
	}

	var feeReduction uint64 = swapSize * tBTC.MaxFeeRate
	estFeeReduction := swapSize * feeSuggestion
	splitFees := splitTxBaggageSegwit * tBTC.MaxFeeRate
	checkMaxOrder(t, wallet, lots-1, swapVal-tLotSize, backingFees+splitFees-feeReduction,
		(totalBytes+splitTxBaggageSegwit)*feeSuggestion-estFeeReduction,
		(bestCaseBytes+splitTxBaggageSegwit)*feeSuggestion)

	_, _, _, err := wallet.FundOrder(ord)
	if err == nil {
		t.Fatalf("no error when not enough funds in single p2wpkh utxo")
	}
	// Now add the needed satoshi and try again.
	p2wpkhUnspent.Amount = float64(swapVal+backingFees) / 1e8
	node.listUnspent = unspents

	checkMaxOrder(t, wallet, lots, swapVal, backingFees, totalBytes*feeSuggestion,
		bestCaseBytes*feeSuggestion)

	spendables, _, _, err := wallet.FundOrder(ord)
	if err != nil {
		t.Fatalf("error when should be enough funding in single p2wpkh utxo: %v", err)
	}
	_ = wallet.ReturnCoins(spendables)

	// For a split transaction, we would need to cover the splitTxBaggage as
	// well.
	node.walletCfg.useSplitTx = true
	node.changeAddr = tP2WPKHAddr
	node.signFunc = func(tx *wire.MsgTx) {
		signFunc(tx, 0, wallet.segwit)
	}
	backingFees = uint64(totalBytes+splitTxBaggageSegwit) * tBTC.MaxFeeRate
	v := swapVal + backingFees - 1
	p2wpkhUnspent.Amount = float64(v) / 1e8
	node.listUnspent = unspents
	coins, _, _, err := wallet.FundOrder(ord)
	if err != nil {
		t.Fatalf("error when skipping split tx because not enough to cover baggage: %v", err)
	}
	if coins[0].Value() != v {
		t.Fatalf("split performed when baggage wasn't covered")
	}
	_ = wallet.ReturnCoins(spendables)
	// Now get the split.
	v = swapVal + backingFees
	p2wpkhUnspent.Amount = float64(v) / 1e8
	node.listUnspent = unspents

	checkMaxOrder(t, wallet, lots, swapVal, backingFees,
		(totalBytes+splitTxBaggageSegwit)*feeSuggestion,
		(bestCaseBytes+splitTxBaggageSegwit)*feeSuggestion)

	coins, _, _, err = wallet.FundOrder(ord)
	if err != nil {
		t.Fatalf("error funding split tx: %v", err)
	}
	_ = wallet.ReturnCoins(spendables)
	if coins[0].Value() == v {
		t.Fatalf("split performed when baggage wasn't covered")
	}
	node.walletCfg.useSplitTx = false
}

func TestSwap(t *testing.T) {
	runRubric(t, testSwap)
}

func testSwap(t *testing.T, segwit bool, walletType string) {
	wallet, node, shutdown := tNewWallet(segwit, walletType)
	defer shutdown()

	swapVal := toSatoshi(5)
	coins := asset.Coins{
		NewOutput(tTxHash, 0, toSatoshi(3)),
		NewOutput(tTxHash, 0, toSatoshi(3)),
	}
	addrStr := tP2PKHAddr
	if segwit {
		addrStr = tP2WPKHAddr
	}

	node.newAddress = addrStr
	node.changeAddr = addrStr

	privBytes, _ := hex.DecodeString("b07209eec1a8fb6cfe5cb6ace36567406971a75c330db7101fb21bc679bc5330")
	privKey, _ := btcec.PrivKeyFromBytes(privBytes)
	wif, err := btcutil.NewWIF(privKey, &chaincfg.MainNetParams, true)
	if err != nil {
		t.Fatalf("error encoding wif: %v", err)
	}
	node.privKeyForAddr = wif

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

	// Aim for 3 signature cycles.
	sigSizer := 0
	node.signFunc = func(tx *wire.MsgTx) {
		var sizeTweak int
		if sigSizer%2 == 0 {
			sizeTweak = -2
		}
		sigSizer++
		signFunc(tx, sizeTweak, wallet.segwit)
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
	node.newAddressErr = tErr
	_, _, _, err = wallet.Swap(swaps)
	if err == nil {
		t.Fatalf("no error for getnewaddress rpc error")
	}
	node.newAddressErr = nil

	// ChangeAddress error
	node.changeAddrErr = tErr
	_, _, _, err = wallet.Swap(swaps)
	if err == nil {
		t.Fatalf("no error for getrawchangeaddress rpc error")
	}
	node.changeAddrErr = nil

	// SignTx error
	node.signTxErr = tErr
	_, _, _, err = wallet.Swap(swaps)
	if err == nil {
		t.Fatalf("no error for signrawtransactionwithwallet rpc error")
	}
	node.signTxErr = nil

	// incomplete signatures
	node.sigIncomplete = true
	_, _, _, err = wallet.Swap(swaps)
	if err == nil {
		t.Fatalf("no error for incomplete signature rpc error")
	}
	node.sigIncomplete = false

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
	runRubric(t, testRedeem)
}

func testRedeem(t *testing.T, segwit bool, walletType string) {
	wallet, node, shutdown := tNewWallet(segwit, walletType)
	defer shutdown()
	swapVal := toSatoshi(5)

	secret, _, _, contract, addr, _, lockTime := makeSwapContract(segwit, time.Hour*12)

	coin := NewOutput(tTxHash, 0, swapVal)
	ci := &asset.AuditInfo{
		Coin:       coin,
		Contract:   contract,
		Recipient:  addr.String(),
		Expiration: lockTime,
	}

	redemption := &asset.Redemption{
		Spends: ci,
		Secret: secret,
	}

	privBytes, _ := hex.DecodeString("b07209eec1a8fb6cfe5cb6ace36567406971a75c330db7101fb21bc679bc5330")
	privKey, _ := btcec.PrivKeyFromBytes(privBytes)
	wif, err := btcutil.NewWIF(privKey, &chaincfg.MainNetParams, true)
	if err != nil {
		t.Fatalf("error encoding wif: %v", err)
	}

	addrStr := tP2PKHAddr
	if segwit {
		addrStr = tP2WPKHAddr
	}

	node.changeAddr = addrStr
	node.privKeyForAddr = wif

	redemptions := &asset.RedeemForm{
		Redemptions: []*asset.Redemption{redemption},
	}

	_, _, feesPaid, err := wallet.Redeem(redemptions)
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
	coin.Val = 200
	_, _, _, err = wallet.Redeem(redemptions)
	if err == nil {
		t.Fatalf("no error for redemption not worth the fees")
	}
	coin.Val = swapVal

	// Change address error
	node.changeAddrErr = tErr
	_, _, _, err = wallet.Redeem(redemptions)
	if err == nil {
		t.Fatalf("no error for change address error")
	}
	node.changeAddrErr = nil

	// Missing priv key error
	node.privKeyForAddrErr = tErr
	_, _, _, err = wallet.Redeem(redemptions)
	if err == nil {
		t.Fatalf("no error for missing private key")
	}
	node.privKeyForAddrErr = nil

	// Send error
	node.sendErr = tErr
	_, _, _, err = wallet.Redeem(redemptions)
	if err == nil {
		t.Fatalf("no error for send error")
	}
	node.sendErr = nil

	// Wrong hash
	var h chainhash.Hash
	h[0] = 0x01
	node.badSendHash = &h
	_, _, _, err = wallet.Redeem(redemptions)
	if err == nil {
		t.Fatalf("no error for wrong return hash")
	}
	node.badSendHash = nil
}

func TestSignMessage(t *testing.T) {
	runRubric(t, testSignMessage)
}

func testSignMessage(t *testing.T, segwit bool, walletType string) {
	wallet, node, shutdown := tNewWallet(segwit, walletType)
	defer shutdown()

	vout := uint32(5)
	privBytes, _ := hex.DecodeString("b07209eec1a8fb6cfe5cb6ace36567406971a75c330db7101fb21bc679bc5330")
	privKey, pubKey := btcec.PrivKeyFromBytes(privBytes)
	wif, err := btcutil.NewWIF(privKey, &chaincfg.MainNetParams, true)
	if err != nil {
		t.Fatalf("error encoding wif: %v", err)
	}

	msg := randBytes(36)
	msgHash := chainhash.HashB(msg)
	pk := pubKey.SerializeCompressed()
	signature := ecdsa.Sign(privKey, msgHash)
	sig := signature.Serialize()

	pt := NewOutPoint(tTxHash, vout)
	utxo := &UTxO{Address: tP2PKHAddr}
	wallet.cm.lockedOutputs[pt] = utxo
	node.privKeyForAddr = wif
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
		msgHash := chainhash.HashB(sentMsg)
		sig := ecdsa.Sign(wif.PrivKey, msgHash)
		r, _ := json.Marshal(base64.StdEncoding.EncodeToString(sig.Serialize()))
		return r, nil
	}

	var coin asset.Coin = NewOutput(tTxHash, vout, 5e7)
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
	delete(wallet.cm.lockedOutputs, pt)
	_, _, err = wallet.SignMessage(coin, msg)
	if err == nil {
		t.Fatalf("no error for unknown utxo")
	}
	wallet.cm.lockedOutputs[pt] = utxo

	// dumpprivkey error
	node.privKeyForAddrErr = tErr
	_, _, err = wallet.SignMessage(coin, msg)
	if err == nil {
		t.Fatalf("no error for dumpprivkey rpc error")
	}
	node.privKeyForAddrErr = nil

	// bad coin
	badCoin := &tCoin{id: make([]byte, 15)}
	_, _, err = wallet.SignMessage(badCoin, msg)
	if err == nil {
		t.Fatalf("no error for bad coin")
	}
}

func TestAuditContract(t *testing.T) {
	runRubric(t, testAuditContract)
}

func testAuditContract(t *testing.T, segwit bool, walletType string) {
	wallet, _, shutdown := tNewWallet(segwit, walletType)
	defer shutdown()
	secretHash, _ := hex.DecodeString("5124208c80d33507befa517c08ed01aa8d33adbf37ecd70fb5f9352f7a51a88d")
	lockTime := time.Now().Add(time.Hour * 12)
	addr, _ := btcutil.DecodeAddress(tP2PKHAddr, &chaincfg.MainNetParams)
	if segwit {
		addr, _ = btcutil.DecodeAddress(tP2WPKHAddr, &chaincfg.MainNetParams)
	}

	contract, err := dexbtc.MakeContract(addr, addr, secretHash, lockTime.Unix(), segwit, &chaincfg.MainNetParams)
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
	tx := makeRawTx([]dex.Bytes{pkScript}, []*wire.TxIn{dummyInput()})
	txData, err := serializeMsgTx(tx)
	if err != nil {
		t.Fatalf("error making contract tx data: %v", err)
	}

	txHash := tx.TxHash()
	const vout = 0

	audit, err := wallet.AuditContract(ToCoinID(&txHash, vout), contract, txData, false)
	if err != nil {
		t.Fatalf("audit error: %v", err)
	}
	if audit.Recipient != addr.String() {
		t.Fatalf("wrong recipient. wanted '%s', got '%s'", addr, audit.Recipient)
	}
	if !bytes.Equal(audit.Contract, contract) {
		t.Fatalf("contract not set to coin redeem script")
	}
	if audit.Expiration.Equal(lockTime) {
		t.Fatalf("wrong lock time. wanted %d, got %d", lockTime.Unix(), audit.Expiration.Unix())
	}

	// Invalid txid
	_, err = wallet.AuditContract(make([]byte, 15), contract, txData, false)
	if err == nil {
		t.Fatalf("no error for bad txid")
	}

	// Wrong contract
	pkh, _ := hex.DecodeString("c6a704f11af6cbee8738ff19fc28cdc70aba0b82")
	wrongAddr, _ := btcutil.NewAddressPubKeyHash(pkh, &chaincfg.MainNetParams)
	badContract, _ := txscript.PayToAddrScript(wrongAddr)
	_, err = wallet.AuditContract(ToCoinID(&txHash, vout), badContract, nil, false)
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
	runRubric(t, testFindRedemption)
}

func testFindRedemption(t *testing.T, segwit bool, walletType string) {
	wallet, node, shutdown := tNewWallet(segwit, walletType)
	defer shutdown()

	contractHeight := node.GetBestBlockHeight() + 1
	otherTxid := "7a7b3b5c3638516bc8e7f19b4a3dec00f052a599fed5036c2b89829de2367bb6"
	otherTxHash, _ := chainhash.NewHashFromStr(otherTxid)
	contractVout := uint32(1)

	secret, _, pkScript, contract, addr, _, _ := makeSwapContract(segwit, time.Hour*12)
	otherScript, _ := txscript.PayToAddrScript(addr)

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
	inputs := []*wire.TxIn{makeRPCVin(otherTxHash, 0, otherSigScript, otherWitness)}
	// Add the contract transaction. Put the pay-to-contract script at index 1.
	contractTx := makeRawTx([]dex.Bytes{otherScript, pkScript}, inputs)
	contractTxHash := contractTx.TxHash()
	coinID := ToCoinID(&contractTxHash, contractVout)
	blockHash, _ := node.addRawTx(contractHeight, contractTx)
	txHex, err := makeTxHex([]dex.Bytes{otherScript, pkScript}, inputs)
	if err != nil {
		t.Fatalf("error generating hex for contract tx: %v", err)
	}
	getTxRes := &GetTransactionResult{
		BlockHash:  blockHash.String(),
		BlockIndex: contractHeight,
		Bytes:      txHex,
	}
	node.getTransactionMap = map[string]*GetTransactionResult{
		"any": getTxRes}

	// Add an intermediate block for good measure.
	node.addRawTx(contractHeight+1, makeRawTx([]dex.Bytes{otherScript}, inputs))

	// Now add the redemption.
	redeemVin := makeRPCVin(&contractTxHash, contractVout, redemptionSigScript, redemptionWitness)
	inputs = append(inputs, redeemVin)
	redeemBlockHash, _ := node.addRawTx(contractHeight+2, makeRawTx([]dex.Bytes{otherScript}, inputs))
	node.getCFilterScripts[*redeemBlockHash] = [][]byte{pkScript}

	// Update currentTip from "RPC". Normally run() would do this.
	wallet.reportNewTip(tCtx, &BlockVector{
		Hash:   *redeemBlockHash,
		Height: contractHeight + 2,
	})

	// Check find redemption result.
	_, checkSecret, err := wallet.FindRedemption(tCtx, coinID, nil)
	if err != nil {
		t.Fatalf("error finding redemption: %v", err)
	}
	if !bytes.Equal(checkSecret, secret) {
		t.Fatalf("wrong secret. expected %x, got %x", secret, checkSecret)
	}

	// gettransaction error
	node.getTransactionErr = tErr
	_, _, err = wallet.FindRedemption(tCtx, coinID, nil)
	if err == nil {
		t.Fatalf("no error for gettransaction rpc error")
	}
	node.getTransactionErr = nil

	// timeout finding missing redemption
	redeemVin.PreviousOutPoint.Hash = *otherTxHash
	delete(node.getCFilterScripts, *redeemBlockHash)
	timedCtx, cancel := context.WithTimeout(tCtx, 500*time.Millisecond) // 0.5 seconds is long enough
	defer cancel()
	_, k, err := wallet.FindRedemption(timedCtx, coinID, nil)
	if timedCtx.Err() == nil || k != nil {
		// Expected ctx to cancel after timeout and no secret should be found.
		t.Fatalf("unexpected result for missing redemption: secret: %v, err: %v", k, err)
	}

	node.blockchainMtx.Lock()
	redeemVin.PreviousOutPoint.Hash = contractTxHash
	node.getCFilterScripts[*redeemBlockHash] = [][]byte{pkScript}
	node.blockchainMtx.Unlock()

	// Canceled context
	deadCtx, cancelCtx := context.WithCancel(tCtx)
	cancelCtx()
	_, _, err = wallet.FindRedemption(deadCtx, coinID, nil)
	if err == nil {
		t.Fatalf("no error for canceled context")
	}

	// Expect FindRedemption to error because of bad input sig.
	node.blockchainMtx.Lock()
	redeemVin.Witness = [][]byte{randBytes(100)}
	redeemVin.SignatureScript = randBytes(100)

	node.blockchainMtx.Unlock()
	_, _, err = wallet.FindRedemption(tCtx, coinID, nil)
	if err == nil {
		t.Fatalf("no error for wrong redemption")
	}
	node.blockchainMtx.Lock()
	redeemVin.Witness = redemptionWitness
	redeemVin.SignatureScript = redemptionSigScript
	node.blockchainMtx.Unlock()

	// Sanity check to make sure it passes again.
	_, _, err = wallet.FindRedemption(tCtx, coinID, nil)
	if err != nil {
		t.Fatalf("error after clearing errors: %v", err)
	}
}

func TestRefund(t *testing.T) {
	runRubric(t, testRefund)
}

func testRefund(t *testing.T, segwit bool, walletType string) {
	wallet, node, shutdown := tNewWallet(segwit, walletType)
	defer shutdown()

	_, _, pkScript, contract, addr, _, _ := makeSwapContract(segwit, time.Hour*12)

	bigTxOut := newTxOutResult(nil, 1e8, 2)
	node.txOutRes = bigTxOut // rpc
	node.changeAddr = addr.String()
	const feeSuggestion = 100

	privBytes, _ := hex.DecodeString("b07209eec1a8fb6cfe5cb6ace36567406971a75c330db7101fb21bc679bc5330")
	privKey, _ := btcec.PrivKeyFromBytes(privBytes)
	wif, err := btcutil.NewWIF(privKey, &chaincfg.MainNetParams, true)
	if err != nil {
		t.Fatalf("error encoding wif: %v", err)
	}
	node.privKeyForAddr = wif

	tx := makeRawTx([]dex.Bytes{pkScript}, []*wire.TxIn{dummyInput()})
	const vout = 0
	tx.TxOut[vout].Value = 1e8
	txHash := tx.TxHash()
	outPt := NewOutPoint(&txHash, vout)
	blockHash, _ := node.addRawTx(1, tx)
	node.getCFilterScripts[*blockHash] = [][]byte{pkScript}
	node.getTransactionErr = WalletTransactionNotFound

	contractOutput := NewOutput(&txHash, 0, 1e8)
	_, err = wallet.Refund(contractOutput.ID(), contract, feeSuggestion)
	if err != nil {
		t.Fatalf("refund error: %v", err)
	}

	// Invalid coin
	badReceipt := &tReceipt{
		coin: &tCoin{id: make([]byte, 15)},
	}
	_, err = wallet.Refund(badReceipt.coin.id, badReceipt.Contract(), feeSuggestion)
	if err == nil {
		t.Fatalf("no error for bad receipt")
	}

	ensureErr := func(tag string) {
		delete(node.checkpoints, outPt)
		_, err = wallet.Refund(contractOutput.ID(), contract, feeSuggestion)
		if err == nil {
			t.Fatalf("no error for %q", tag)
		}
	}

	// gettxout error
	node.txOutErr = tErr
	node.getCFilterScripts[*blockHash] = nil
	ensureErr("no utxo")
	node.getCFilterScripts[*blockHash] = [][]byte{pkScript}
	node.txOutErr = nil

	// bad contract
	badContractOutput := NewOutput(tTxHash, 0, 1e8)
	badContract := randBytes(50)
	_, err = wallet.Refund(badContractOutput.ID(), badContract, feeSuggestion)
	if err == nil {
		t.Fatalf("no error for bad contract")
	}

	// Too small.
	node.txOutRes = newTxOutResult(nil, 100, 2)
	tx.TxOut[0].Value = 2
	ensureErr("value < fees")
	node.txOutRes = bigTxOut
	tx.TxOut[0].Value = 1e8

	// getrawchangeaddress error
	node.changeAddrErr = tErr
	ensureErr("getchangeaddress error")
	node.changeAddrErr = nil

	// signature error
	node.privKeyForAddrErr = tErr
	ensureErr("dumpprivkey error")
	node.privKeyForAddrErr = nil

	// send error
	node.sendErr = tErr
	ensureErr("send error")
	node.sendErr = nil

	// bad checkhash
	var badHash chainhash.Hash
	badHash[0] = 0x05
	node.badSendHash = &badHash
	ensureErr("checkhash error")
	node.badSendHash = nil

	// Sanity check that we can succeed again.
	_, err = wallet.Refund(contractOutput.ID(), contract, feeSuggestion)
	if err != nil {
		t.Fatalf("re-refund error: %v", err)
	}

	// TODO test spv spent
}

func TestLockUnlock(t *testing.T) {
	runRubric(t, testLockUnlock)
}

func testLockUnlock(t *testing.T, segwit bool, walletType string) {
	w, node, shutdown := tNewWallet(segwit, walletType)
	defer shutdown()

	pw := []byte("pass")
	wallet := &ExchangeWalletFullNode{w, &authAddOn{w.node}}

	// just checking that the errors come through.
	err := wallet.Unlock(pw)
	if err != nil {
		t.Fatalf("unlock error: %v", err)
	}
	node.unlockErr = tErr
	err = wallet.Unlock(pw)
	if err == nil {
		t.Fatalf("no error for walletpassphrase error")
	}

	// Locking can't error on SPV.
	if walletType == walletTypeRPC {
		err = wallet.Lock()
		if err != nil {
			t.Fatalf("lock error: %v", err)
		}
		node.lockErr = tErr
		err = wallet.Lock()
		if err == nil {
			t.Fatalf("no error for walletlock rpc error")
		}
	}

}

type tSenderType byte

const (
	tSendSender tSenderType = iota
	tWithdrawSender
)

func testSender(t *testing.T, senderType tSenderType, segwit bool, walletType string) {
	wallet, node, shutdown := tNewWallet(segwit, walletType)
	defer shutdown()
	const feeSuggestion = 100
	sender := func(addr string, val uint64) (asset.Coin, error) {
		return wallet.Send(addr, val, defaultFee)
	}
	if senderType == tWithdrawSender {
		sender = func(addr string, val uint64) (asset.Coin, error) {
			return wallet.Withdraw(addr, val, feeSuggestion)
		}
	}

	node.signFunc = func(tx *wire.MsgTx) {
		signFunc(tx, 0, wallet.segwit)
	}

	addr := btcAddr(segwit)
	node.setTxFee = true
	node.changeAddr = btcAddr(segwit).String()

	pkScript, _ := txscript.PayToAddrScript(addr)
	tx := makeRawTx([]dex.Bytes{randBytes(5), pkScript}, []*wire.TxIn{dummyInput()})
	txHash := tx.TxHash()

	changeAddr := btcAddr(segwit)
	node.changeAddr = changeAddr.String()

	expectedFees := func(numInputs int) uint64 {
		txSize := dexbtc.MinimumTxOverhead
		if segwit {
			txSize += (dexbtc.P2WPKHOutputSize * 2) + (numInputs * dexbtc.RedeemP2WPKHInputTotalSize)
		} else {
			txSize += (dexbtc.P2PKHOutputSize * 2) + (numInputs * dexbtc.RedeemP2PKHInputSize)
		}
		return uint64(txSize * feeSuggestion)
	}

	expectedSentVal := func(sendVal, fees uint64) uint64 {
		if senderType == tSendSender {
			return sendVal
		}
		return sendVal - fees
	}

	expectedChangeVal := func(totalInput, sendVal, fees uint64) uint64 {
		if senderType == tSendSender {
			return totalInput - sendVal - fees
		}
		return totalInput - sendVal
	}

	requiredVal := func(sendVal, fees uint64) uint64 {
		if senderType == tSendSender {
			return sendVal + fees
		}
		return sendVal
	}

	type test struct {
		name         string
		val          uint64
		unspents     []*ListUnspentResult
		bondReserves uint64

		expectedInputs []*OutPoint
		expectSentVal  uint64
		expectChange   uint64
		expectErr      bool
	}
	tests := []test{
		{
			name: "plenty of funds",
			val:  toSatoshi(5),
			unspents: []*ListUnspentResult{{
				TxID:          txHash.String(),
				Address:       addr.String(),
				Amount:        100,
				Confirmations: 1,
				Vout:          0,
				ScriptPubKey:  pkScript,
				SafePtr:       boolPtr(true),
				Spendable:     true,
			}},
			expectedInputs: []*OutPoint{
				{TxHash: txHash, Vout: 0},
			},
			expectSentVal: expectedSentVal(toSatoshi(5), expectedFees(1)),
			expectChange:  expectedChangeVal(toSatoshi(100), toSatoshi(5), expectedFees(1)),
		},
		{
			name: "just enough change for bond reserves",
			val:  toSatoshi(5),
			unspents: []*ListUnspentResult{{
				TxID:          txHash.String(),
				Address:       addr.String(),
				Amount:        5.2,
				Confirmations: 1,
				Vout:          0,
				ScriptPubKey:  pkScript,
				SafePtr:       boolPtr(true),
				Spendable:     true,
			}},
			expectedInputs: []*OutPoint{
				{TxHash: txHash,
					Vout: 0},
			},
			expectSentVal: expectedSentVal(toSatoshi(5), expectedFees(1)),
			expectChange:  expectedChangeVal(toSatoshi(5.2), toSatoshi(5), expectedFees(1)),
			bondReserves:  expectedChangeVal(toSatoshi(5.2), toSatoshi(5), expectedFees(1)),
		},
		{
			name: "not enough change for bond reserves",
			val:  toSatoshi(5),
			unspents: []*ListUnspentResult{{
				TxID:          txHash.String(),
				Address:       addr.String(),
				Amount:        5.2,
				Confirmations: 1,
				Vout:          0,
				ScriptPubKey:  pkScript,
				SafePtr:       boolPtr(true),
				Spendable:     true,
			}},
			expectedInputs: []*OutPoint{
				{TxHash: txHash, Vout: 0},
			},
			bondReserves: expectedChangeVal(toSatoshi(5.2), toSatoshi(5), expectedFees(1)) + 1,
			expectErr:    true,
		},
		{
			name: "1 satoshi less than needed",
			val:  toSatoshi(5),
			unspents: []*ListUnspentResult{{
				TxID:          txHash.String(),
				Address:       addr.String(),
				Amount:        toBTC(requiredVal(toSatoshi(5), expectedFees(1)) - 1),
				Confirmations: 1,
				Vout:          0,
				ScriptPubKey:  pkScript,
				SafePtr:       boolPtr(true),
				Spendable:     true,
			}},
			expectErr: true,
		},
		{
			name: "exact amount needed",
			val:  toSatoshi(5),
			unspents: []*ListUnspentResult{{
				TxID:          txHash.String(),
				Address:       addr.String(),
				Amount:        toBTC(requiredVal(toSatoshi(5), expectedFees(1))),
				Confirmations: 1,
				Vout:          0,
				ScriptPubKey:  pkScript,
				SafePtr:       boolPtr(true),
				Spendable:     true,
			}},
			expectedInputs: []*OutPoint{
				{TxHash: txHash, Vout: 0},
			},
			expectSentVal: expectedSentVal(toSatoshi(5), expectedFees(1)),
			expectChange:  0,
		},
	}

	for _, test := range tests {
		node.listUnspent = test.unspents
		wallet.bondReserves.Store(test.bondReserves)

		_, err := sender(addr.String(), test.val)
		if test.expectErr {
			if err == nil {
				t.Fatalf("%s: no error for expected error", test.name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", test.name, err)
		}

		tx := node.sentRawTx
		if len(test.expectedInputs) != len(tx.TxIn) {
			t.Fatalf("expected %d inputs, got %d", len(test.expectedInputs), len(tx.TxIn))
		}

		for i, input := range tx.TxIn {
			if input.PreviousOutPoint.Hash != test.expectedInputs[i].TxHash ||
				input.PreviousOutPoint.Index != test.expectedInputs[i].Vout {
				t.Fatalf("expected input %d to be %v, got %v", i, test.expectedInputs[i], input.PreviousOutPoint)
			}
		}

		if test.expectChange > 0 && len(tx.TxOut) != 2 {
			t.Fatalf("expected 2 outputs, got %d", len(tx.TxOut))
		}
		if test.expectChange == 0 && len(tx.TxOut) != 1 {
			t.Fatalf("expected 2 outputs, got %d", len(tx.TxOut))
		}

		if tx.TxOut[0].Value != int64(test.expectSentVal) {
			t.Fatalf("expected sent value to be %d, got %d", test.expectSentVal, tx.TxOut[0].Value)
		}

		if test.expectChange > 0 && tx.TxOut[1].Value != int64(test.expectChange) {
			t.Fatalf("expected change value to be %d, got %d", test.expectChange, tx.TxOut[1].Value)
		}
	}
}

func TestEstimateRegistrationTxFee(t *testing.T) {
	runRubric(t, testEstimateRegistrationTxFee)
}

func testEstimateRegistrationTxFee(t *testing.T, segwit bool, walletType string) {
	wallet, node, shutdown := tNewWallet(segwit, walletType)
	defer shutdown()

	const inputCount = 5
	const txSize = dexbtc.MinimumTxOverhead + 2*dexbtc.P2PKHOutputSize + inputCount*dexbtc.RedeemP2PKHInputSize
	node.walletCfg.feeRateLimit = 100
	node.walletCfg.fallbackFeeRate = 30

	estimate := wallet.EstimateRegistrationTxFee(50)
	if estimate != 50*txSize {
		t.Fatalf("expected tx fee to be %d but got %d", 50*txSize, estimate)
	}

	estimate = wallet.EstimateRegistrationTxFee(0)
	if estimate != wallet.fallbackFeeRate()*txSize {
		t.Fatalf("expected tx fee to be %d but got %d", wallet.fallbackFeeRate()*txSize, estimate)
	}

	estimate = wallet.EstimateRegistrationTxFee(wallet.feeRateLimit() + 1)
	if estimate != wallet.fallbackFeeRate()*txSize {
		t.Fatalf("expected tx fee to be %d but got %d", wallet.fallbackFeeRate()*txSize, estimate)
	}
}

func TestEstimateSendTxFee(t *testing.T) {
	runRubric(t, testEstimateSendTxFee)
}

func testEstimateSendTxFee(t *testing.T, segwit bool, walletType string) {
	wallet, node, shutdown := tNewWallet(segwit, walletType)
	defer shutdown()

	addr := btcAddr(segwit)
	sendAddr := addr.String()
	node.changeAddr = btcAddr(segwit).String()
	pkScript, _ := txscript.PayToAddrScript(addr)

	unspentVal := 100 // BTC
	unspents := []*ListUnspentResult{{
		Address:       addr.String(),
		Amount:        float64(unspentVal),
		Confirmations: 1,
		Vout:          1,
		ScriptPubKey:  pkScript,
		SafePtr:       boolPtr(true),
		Spendable:     true,
	}}
	node.listUnspent = unspents
	var bals GetBalancesResult
	node.getBalances = &bals
	bals.Mine.Trusted = float64(unspentVal)
	unspentSats := toSatoshi(unspents[0].Amount)

	tx := wire.NewMsgTx(wire.TxVersion)
	tx.AddTxOut(wire.NewTxOut(int64(unspentSats), pkScript))

	// single ouptput size.
	opSize := dexbtc.P2PKHOutputSize
	if segwit {
		opSize = dexbtc.P2WPKHOutputSize
	}

	// bSize is the size for a single input.
	witnessWeight := 4
	bSize := dexbtc.TxInOverhead +
		wire.VarIntSerializeSize(uint64(dexbtc.RedeemP2PKHSigScriptSize)) +
		dexbtc.RedeemP2PKHSigScriptSize + (witnessWeight-1)/witnessWeight
	if segwit {
		bSize = dexbtc.TxInOverhead + 1 + (dexbtc.RedeemP2WPKHInputWitnessWeight+
			(witnessWeight-1))/witnessWeight
	}

	txSize := bSize + tx.SerializeSize()
	minEstFee := optimalFeeRate * uint64(txSize)

	// This should return fee estimate for one output.
	node.txFee = minEstFee

	estimate, _, err := wallet.EstimateSendTxFee(sendAddr, unspentSats, optimalFeeRate, true)
	if err != nil {
		t.Fatal(err)
	}
	if estimate != minEstFee {
		t.Fatalf("expected estimate to be %v, got %v)", minEstFee, estimate)
	}

	// This should return fee estimate for two output.
	minEstFeeWithEstChangeFee := uint64(txSize+opSize) * optimalFeeRate
	node.txFee = minEstFeeWithEstChangeFee
	estimate, _, err = wallet.EstimateSendTxFee(sendAddr, unspentSats/2, optimalFeeRate, false)
	if err != nil {
		t.Fatal(err)
	}
	if estimate != minEstFeeWithEstChangeFee {
		t.Fatalf("expected estimate to be %v, got %v)", minEstFeeWithEstChangeFee, estimate)
	}

	dust := 0.00000016
	node.listUnspent[0].Amount += dust
	// This should return fee estimate for one output with dust added to fee.
	minFeeWithDust := minEstFee + toSatoshi(dust)
	node.txFee = minFeeWithDust
	estimate, _, err = wallet.EstimateSendTxFee(sendAddr, unspentSats, optimalFeeRate, true)
	if err != nil {
		t.Fatal(err)
	}
	if estimate != minFeeWithDust {
		t.Fatalf("expected estimate to be %v, got %v)", minFeeWithDust, estimate)
	}

	// Invalid address
	_, valid, _ := wallet.EstimateSendTxFee("invalidsendAddr", unspentSats, optimalFeeRate, true)
	if valid {
		t.Fatal("Expected false for invalid send address")
	}

	// Successful estimation without an address
	_, _, err = wallet.EstimateSendTxFee("", unspentSats, optimalFeeRate, true)
	if err != nil {
		t.Fatalf("Error for estimation without an address: %v", err)
	}

	// Zero send amount
	_, _, err = wallet.EstimateSendTxFee(sendAddr, 0, optimalFeeRate, true)
	if err == nil {
		t.Fatal("Expected an error for zero send amount")
	}

	// Output value is dust.
	_, _, err = wallet.EstimateSendTxFee(sendAddr, 500, optimalFeeRate, true)
	if err == nil {
		t.Fatal("Expected an error for dust output")
	}
}

func TestWithdraw(t *testing.T) {
	runRubric(t, func(t *testing.T, segwit bool, walletType string) {
		testSender(t, tWithdrawSender, segwit, walletType)
	})
}

func TestSend(t *testing.T) {
	runRubric(t, func(t *testing.T, segwit bool, walletType string) {
		testSender(t, tSendSender, segwit, walletType)
	})
}

func TestConfirmations(t *testing.T) {
	runRubric(t, testConfirmations)
}

func testConfirmations(t *testing.T, segwit bool, walletType string) {
	wallet, node, shutdown := tNewWallet(segwit, walletType)
	defer shutdown()

	// coinID := make([]byte, 36)
	// copy(coinID[:32], tTxHash[:])

	_, _, pkScript, contract, _, _, _ := makeSwapContract(segwit, time.Hour*12)
	const tipHeight = 10
	const swapHeight = 2
	const spendHeight = 4
	const expConfs = tipHeight - swapHeight + 1

	tx := makeRawTx([]dex.Bytes{pkScript}, []*wire.TxIn{dummyInput()})
	blockHash, swapBlock := node.addRawTx(swapHeight, tx)
	txHash := tx.TxHash()
	coinID := ToCoinID(&txHash, 0)
	// Simulate a spending transaction, and advance the tip so that the swap
	// has two confirmations.
	spendingTx := dummyTx()
	spendingTx.TxIn[0].PreviousOutPoint.Hash = txHash
	spendingBlockHash, _ := node.addRawTx(spendHeight, spendingTx)

	// Prime the blockchain
	for i := int64(1); i <= tipHeight; i++ {
		node.addRawTx(i, dummyTx())
	}

	matchTime := swapBlock.Header.Timestamp

	// Bad coin id
	_, _, err := wallet.SwapConfirmations(context.Background(), randBytes(35), contract, matchTime)
	if err == nil {
		t.Fatalf("no error for bad coin ID")
	}

	// Short path.
	txOutRes := &btcjson.GetTxOutResult{
		Confirmations: expConfs,
		BestBlock:     blockHash.String(),
	}
	node.txOutRes = txOutRes
	node.getCFilterScripts[*blockHash] = [][]byte{pkScript}
	confs, _, err := wallet.SwapConfirmations(context.Background(), coinID, contract, matchTime)
	if err != nil {
		t.Fatalf("error for gettransaction path: %v", err)
	}
	if confs != expConfs {
		t.Fatalf("confs not retrieved from gettxout path. expected %d, got %d", expConfs, confs)
	}

	// no tx output found
	node.txOutRes = nil
	node.getCFilterScripts[*blockHash] = nil
	node.getTransactionErr = tErr
	_, _, err = wallet.SwapConfirmations(context.Background(), coinID, contract, matchTime)
	if err == nil {
		t.Fatalf("no error for gettransaction error")
	}
	node.getCFilterScripts[*blockHash] = [][]byte{pkScript}
	node.getTransactionErr = nil
	txB, _ := serializeMsgTx(tx)

	node.getTransactionMap = map[string]*GetTransactionResult{
		"any": {
			BlockHash: blockHash.String(),
			Bytes:     txB,
		}}

	node.getCFilterScripts[*spendingBlockHash] = [][]byte{pkScript}
	node.walletTxSpent = true
	_, spent, err := wallet.SwapConfirmations(context.Background(), coinID, contract, matchTime)
	if err != nil {
		t.Fatalf("error for spent swap: %v", err)
	}
	if !spent {
		t.Fatalf("swap not spent")
	}
}

func TestSendEdges(t *testing.T) {
	runRubric(t, testSendEdges)
}

func testSendEdges(t *testing.T, segwit bool, walletType string) {
	wallet, node, shutdown := tNewWallet(segwit, walletType)
	defer shutdown()

	const feeRate uint64 = 3

	const swapVal = 2e8 // leaving untyped. NewTxOut wants int64

	var addr, contractAddr btcutil.Address
	var dexReqFees, dustCoverage uint64

	addr = btcAddr(segwit)

	if segwit {
		contractAddr, _ = btcutil.NewAddressWitnessScriptHash(randBytes(32), &chaincfg.MainNetParams)
		// See dexbtc.IsDust for the source of this dustCoverage voodoo.
		dustCoverage = (dexbtc.P2WPKHOutputSize + 41 + (107 / 4)) * feeRate * 3
		dexReqFees = dexbtc.InitTxSizeSegwit * feeRate
	} else {
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

	node.signFunc = func(tx *wire.MsgTx) {
		signFunc(tx, 0, wallet.segwit)
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
		tx, err := wallet.sendWithReturn(newBaseTx(), addr, tt.funding, swapVal, feeRate)
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
	runRubric(t, testSyncStatus)
}

func testSyncStatus(t *testing.T, segwit bool, walletType string) {
	wallet, node, shutdown := tNewWallet(segwit, walletType)
	defer shutdown()

	// full node
	node.getBlockchainInfo = &GetBlockchainInfoResult{
		Headers: 100,
		Blocks:  99, // full node allowed to be synced when 1 block behind
	}

	// spv
	blkHash, msgBlock := node.addRawTx(100, dummyTx())
	node.birthdayTime = msgBlock.Header.Timestamp.Add(-time.Minute) // SPV, wallet birthday is passed
	node.mainchain[100] = blkHash                                   // SPV, actually has to reach target

	synced, progress, err := wallet.SyncStatus()
	if err != nil {
		t.Fatalf("SyncStatus error (synced expected): %v", err)
	}
	if !synced {
		t.Fatalf("synced = false")
	}
	if progress < 1 {
		t.Fatalf("progress not complete when loading last block")
	}

	node.getBlockchainInfoErr = tErr // rpc
	node.blockchainMtx.Lock()
	node.getBestBlockHashErr = tErr // spv BestBlock()
	node.blockchainMtx.Unlock()
	delete(node.mainchain, 100) // force spv to BestBlock() with no wallet block
	_, _, err = wallet.SyncStatus()
	if err == nil {
		t.Fatalf("SyncStatus error not propagated")
	}
	node.getBlockchainInfoErr = nil
	node.blockchainMtx.Lock()
	node.getBestBlockHashErr = nil
	node.blockchainMtx.Unlock()

	wallet.tipAtConnect = 100
	node.getBlockchainInfo = &GetBlockchainInfoResult{
		Headers: 200,
		Blocks:  150,
	}
	node.addRawTx(150, makeRawTx([]dex.Bytes{randBytes(1)}, []*wire.TxIn{dummyInput()})) // spv needs this for BestBlock
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

func TestPreSwap(t *testing.T) {
	runRubric(t, testPreSwap)
}

func testPreSwap(t *testing.T, segwit bool, walletType string) {
	wallet, node, shutdown := tNewWallet(segwit, walletType)
	defer shutdown()

	// See math from TestFundEdges. 10 lots with max fee rate of 34 sats/vbyte.

	swapVal := uint64(1e7)
	lots := swapVal / tLotSize // 10 lots

	// var swapSize = 225
	var totalBytes uint64 = 2250
	var bestCaseBytes uint64 = 225
	pkScript := tP2PKH
	if segwit {
		// swapSize = 153
		totalBytes = 1530
		bestCaseBytes = 153
		pkScript = tP2WPKH
	}

	backingFees := totalBytes * tBTC.MaxFeeRate // total_bytes * fee_rate

	minReq := swapVal + backingFees

	unspent := &ListUnspentResult{
		TxID:          tTxID,
		Address:       tP2PKHAddr,
		Confirmations: 5,
		ScriptPubKey:  pkScript,
		Spendable:     true,
		Solvable:      true,
		SafePtr:       boolPtr(true),
	}
	unspents := []*ListUnspentResult{unspent}

	setFunds := func(v uint64) {
		unspent.Amount = float64(v) / 1e8
		node.listUnspent = unspents
	}

	form := &asset.PreSwapForm{
		Version:       version,
		LotSize:       tLotSize,
		Lots:          lots,
		MaxFeeRate:    tBTC.MaxFeeRate,
		Immediate:     false,
		FeeSuggestion: feeSuggestion,
		// Redeem fields unneeded
	}

	setFunds(minReq)

	// Initial success.
	preSwap, err := wallet.PreSwap(form)
	if err != nil {
		t.Fatalf("PreSwap error: %v", err)
	}

	maxFees := totalBytes * tBTC.MaxFeeRate
	estHighFees := totalBytes * feeSuggestion
	estLowFees := bestCaseBytes * feeSuggestion
	checkSwapEstimate(t, preSwap.Estimate, lots, swapVal, maxFees, estHighFees, estLowFees)

	// Too little funding is an error.
	setFunds(minReq - 1)
	_, err = wallet.PreSwap(form)
	if err == nil {
		t.Fatalf("no PreSwap error for not enough funds")
	}
	setFunds(minReq)

	// Success again.
	_, err = wallet.PreSwap(form)
	if err != nil {
		t.Fatalf("PreSwap error: %v", err)
	}
}

func TestPreRedeem(t *testing.T) {
	runRubric(t, testPreRedeem)
}

func testPreRedeem(t *testing.T, segwit bool, walletType string) {
	wallet, _, shutdown := tNewWallet(segwit, walletType)
	defer shutdown()

	preRedeem, err := wallet.PreRedeem(&asset.PreRedeemForm{
		Version: version,
		Lots:    5,
	})
	// Shouldn't actually be any path to error.
	if err != nil {
		t.Fatalf("PreRedeem non-segwit error: %v", err)
	}

	// Just a couple of sanity checks.
	if preRedeem.Estimate.RealisticBestCase >= preRedeem.Estimate.RealisticWorstCase {
		t.Fatalf("best case > worst case")
	}
}

func TestTryRedemptionRequests(t *testing.T) {
	// runRubric(t, testTryRedemptionRequests)
	testTryRedemptionRequests(t, true, walletTypeSPV)
}

func testTryRedemptionRequests(t *testing.T, segwit bool, walletType string) {
	wallet, node, shutdown := tNewWallet(segwit, walletType)
	defer shutdown()

	const swapVout = 1

	randHash := func() *chainhash.Hash {
		var h chainhash.Hash
		copy(h[:], randBytes(32))
		return &h
	}

	otherScript, _ := txscript.PayToAddrScript(btcAddr(segwit))
	otherInput := []*wire.TxIn{makeRPCVin(randHash(), 0, randBytes(5), nil)}
	otherTx := func() *wire.MsgTx {
		return makeRawTx([]dex.Bytes{otherScript}, otherInput)
	}

	addBlocks := func(n int) {
		var h int64
		// Make dummy transactions.
		for i := 0; i < n; i++ {
			node.addRawTx(h, otherTx())
			h++
		}
	}

	getTx := func(blockHeight int64, txIdx int) (*wire.MsgTx, *chainhash.Hash) {
		if blockHeight == -1 {
			// mempool
			txHash := randHash()
			tx := otherTx()
			node.mempoolTxs[*txHash] = tx
			return tx, nil
		}
		blockHash, blk := node.getBlockAtHeight(blockHeight)
		for len(blk.msgBlock.Transactions) <= txIdx {
			blk.msgBlock.Transactions = append(blk.msgBlock.Transactions, otherTx())
		}
		return blk.msgBlock.Transactions[txIdx], blockHash
	}

	type tRedeem struct {
		redeemTxIdx, redeemVin        int
		swapHeight, redeemBlockHeight int64
		notRedeemed                   bool
	}

	redeemReq := func(r *tRedeem) *FindRedemptionReq {
		var swapBlockHash *chainhash.Hash
		var swapHeight int64
		if r.swapHeight >= 0 {
			swapHeight = r.swapHeight
			swapBlockHash, _ = node.getBlockAtHeight(swapHeight)
		}

		swapTxHash := randHash()
		secret, _, pkScript, contract, _, _, _ := makeSwapContract(segwit, time.Hour*12)

		if !r.notRedeemed {
			redeemTx, redeemBlockHash := getTx(r.redeemBlockHeight, r.redeemTxIdx)

			// redemptionSigScript, _ := dexbtc.RedeemP2SHContract(contract, randBytes(73), randBytes(33), secret)
			for len(redeemTx.TxIn) < r.redeemVin {
				redeemTx.TxIn = append(redeemTx.TxIn, makeRPCVin(randHash(), 0, nil, nil))
			}

			var redemptionSigScript []byte
			var redemptionWitness [][]byte
			if segwit {
				redemptionWitness = dexbtc.RedeemP2WSHContract(contract, randBytes(73), randBytes(33), secret)
			} else {
				redemptionSigScript, _ = dexbtc.RedeemP2SHContract(contract, randBytes(73), randBytes(33), secret)
			}

			redeemTx.TxIn = append(redeemTx.TxIn, makeRPCVin(swapTxHash, swapVout, redemptionSigScript, redemptionWitness))
			if redeemBlockHash != nil {
				node.getCFilterScripts[*redeemBlockHash] = [][]byte{pkScript}
			}
		}

		req := &FindRedemptionReq{
			outPt:        NewOutPoint(swapTxHash, swapVout),
			blockHash:    swapBlockHash,
			blockHeight:  int32(swapHeight),
			resultChan:   make(chan *FindRedemptionResult, 1),
			pkScript:     pkScript,
			contractHash: hashContract(segwit, contract),
		}
		wallet.rf.redemptions[req.outPt] = req
		return req
	}

	type test struct {
		numBlocks        int
		startBlockHeight int64
		redeems          []*tRedeem
		forcedErr        bool
		canceledCtx      bool
	}

	isMempoolTest := func(tt *test) bool {
		for _, r := range tt.redeems {
			if r.redeemBlockHeight == -1 {
				return true
			}
		}
		return false
	}

	tests := []*test{
		{ // Normal redemption
			numBlocks: 2,
			redeems: []*tRedeem{{
				redeemBlockHeight: 1,
				redeemTxIdx:       1,
				redeemVin:         1,
			}},
		},
		{ // Mempool redemption
			numBlocks: 2,
			redeems: []*tRedeem{{
				redeemBlockHeight: -1,
				redeemTxIdx:       2,
				redeemVin:         2,
			}},
		},
		{ // A couple of redemptions, both in tip.
			numBlocks:        3,
			startBlockHeight: 1,
			redeems: []*tRedeem{{
				redeemBlockHeight: 2,
			}, {
				redeemBlockHeight: 2,
			}},
		},
		{ // A couple of redemptions, spread apart.
			numBlocks: 6,
			redeems: []*tRedeem{{
				redeemBlockHeight: 2,
			}, {
				redeemBlockHeight: 4,
			}},
		},
		{ // nil start block
			numBlocks:        5,
			startBlockHeight: -1,
			redeems: []*tRedeem{{
				swapHeight:        4,
				redeemBlockHeight: 4,
			}},
		},
		{ // A mix of mined and mempool redeems
			numBlocks:        3,
			startBlockHeight: 1,
			redeems: []*tRedeem{{
				redeemBlockHeight: -1,
			}, {
				redeemBlockHeight: 2,
			}},
		},
		{ // One found, one not found.
			numBlocks:        4,
			startBlockHeight: 1,
			redeems: []*tRedeem{{
				redeemBlockHeight: 2,
			}, {
				notRedeemed: true,
			}},
		},
		{ // One found in mempool, one not found.
			numBlocks:        4,
			startBlockHeight: 1,
			redeems: []*tRedeem{{
				redeemBlockHeight: -1,
			}, {
				notRedeemed: true,
			}},
		},
		{ // Swap not mined.
			numBlocks:        3,
			startBlockHeight: 1,
			redeems: []*tRedeem{{
				swapHeight:        -1,
				redeemBlockHeight: -1,
			}},
		},
		{ // Fatal error
			numBlocks: 2,
			forcedErr: true,
			redeems: []*tRedeem{{
				redeemBlockHeight: 1,
			}},
		},
		{ // Canceled context.
			numBlocks:   2,
			canceledCtx: true,
			redeems: []*tRedeem{{
				redeemBlockHeight: 1,
			}},
		},
	}

	for _, tt := range tests {
		// Skip tests where we're expected to see mempool in SPV.
		if walletType == walletTypeSPV && isMempoolTest(tt) {
			continue
		}

		node.truncateChains()
		wallet.rf.redemptions = make(map[OutPoint]*FindRedemptionReq)
		node.blockchainMtx.Lock()
		node.getBestBlockHashErr = nil
		if tt.forcedErr {
			node.getBestBlockHashErr = tErr
		}
		node.blockchainMtx.Unlock()
		addBlocks(tt.numBlocks)
		var startBlock *chainhash.Hash
		if tt.startBlockHeight >= 0 {
			startBlock, _ = node.getBlockAtHeight(tt.startBlockHeight)
		}

		ctx := tCtx
		if tt.canceledCtx {
			timedCtx, cancel := context.WithTimeout(tCtx, time.Second)
			ctx = timedCtx
			cancel()
		}

		reqs := make([]*FindRedemptionReq, 0, len(tt.redeems))
		for _, redeem := range tt.redeems {
			reqs = append(reqs, redeemReq(redeem))
		}

		wallet.rf.tryRedemptionRequests(ctx, startBlock, reqs)

		for i, req := range reqs {
			select {
			case res := <-req.resultChan:
				if res.err != nil {
					if !tt.forcedErr {
						t.Fatalf("result error: %v", res.err)
					}
				} else if tt.canceledCtx {
					t.Fatalf("got success with canceled context")
				}
			default:
				redeem := tt.redeems[i]
				if !redeem.notRedeemed && !tt.canceledCtx {
					t.Fatalf("redemption not found")
				}
			}
		}
	}
}

func TestPrettyBTC(t *testing.T) {
	type test struct {
		v   uint64
		exp string
	}

	tests := []*test{{
		v:   1,
		exp: "0.00000001",
	}, {
		v:   1e8,
		exp: "1",
	}, {
		v:   100000001,
		exp: "1.00000001",
	}, {
		v:   0,
		exp: "0",
	}, {
		v:   123000,
		exp: "0.00123",
	}, {
		v:   100123000,
		exp: "1.00123",
	}}

	for _, tt := range tests {
		if prettyBTC(tt.v) != tt.exp {
			t.Fatalf("prettyBTC(%d) = %s != %q", tt.v, prettyBTC(tt.v), tt.exp)
		}
	}
}

// TestAccelerateOrder tests the entire acceleration workflow, including
// AccelerateOrder, PreAccelerate, and AccelerationEstimate
func TestAccelerateOrder(t *testing.T) {
	runRubric(t, testAccelerateOrder)
}

func testAccelerateOrder(t *testing.T, segwit bool, walletType string) {
	w, node, shutdown := tNewWallet(segwit, walletType)
	defer shutdown()

	wallet := &ExchangeWalletAccelerator{&ExchangeWalletFullNode{w, &authAddOn{w.node}}}

	var blockHash100 chainhash.Hash
	copy(blockHash100[:], encode.RandomBytes(32))
	node.verboseBlocks[blockHash100] = &msgBlockWithHeight{height: 100, msgBlock: &wire.MsgBlock{
		Header: wire.BlockHeader{Timestamp: time.Now()},
	}}
	node.mainchain[100] = &blockHash100

	if segwit {
		node.changeAddr = tP2WPKHAddr
	} else {
		node.changeAddr = tP2PKHAddr
	}

	node.signFunc = func(tx *wire.MsgTx) {
		signFunc(tx, 0, wallet.segwit)
	}

	sumFees := func(fees []float64, confs []uint64) uint64 {
		var totalFees uint64
		for i, fee := range fees {
			if confs[i] == 0 {
				totalFees += toSatoshi(fee)
			}
		}
		return totalFees
	}

	sumTxSizes := func(txs []*wire.MsgTx, confirmations []uint64) uint64 {
		var totalSize uint64
		for i, tx := range txs {
			if confirmations[i] == 0 {
				totalSize += dexbtc.MsgTxVBytes(tx)
			}
		}

		return totalSize
	}

	loadTxsIntoNode := func(txs []*wire.MsgTx, fees []float64, confs []uint64, node *testData, withinTimeLimit bool, t *testing.T) {
		t.Helper()
		if len(txs) != len(fees) || len(txs) != len(confs) {
			t.Fatalf("len(txs) = %d, len(fees) = %d, len(confs) = %d", len(txs), len(fees), len(confs))
		}

		serializedTxs := make([][]byte, 0, len(txs))
		for _, tx := range txs {
			serializedTx, err := serializeMsgTx(tx)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			serializedTxs = append(serializedTxs, serializedTx)
		}

		currentTime := time.Now().Unix()
		for i := range txs {
			var blockHash string
			if confs[i] == 1 {
				blockHash = blockHash100.String()
			}
			node.getTransactionMap[txs[i].TxHash().String()] = &GetTransactionResult{
				TxID:          txs[i].TxHash().String(),
				Bytes:         serializedTxs[i],
				BlockHash:     blockHash,
				Confirmations: confs[i]}

			if withinTimeLimit {
				node.getTransactionMap[txs[i].TxHash().String()].Time = uint64(currentTime) - minTimeBeforeAcceleration + 3
			} else {
				node.getTransactionMap[txs[i].TxHash().String()].Time = uint64(currentTime) - minTimeBeforeAcceleration - 1000
			}
		}
	}

	// getAccelerationParams returns a chain of 4 swap transactions where the change
	// output of the last transaction has a certain value. If addAcceleration true,
	// the third transaction will be an acceleration transaction instead of one
	// that initiates a swap.
	getAccelerationParams := func(changeVal int64, addChangeToUnspent, addAcceleration bool, fees []float64, node *testData) ([]dex.Bytes, []dex.Bytes, dex.Bytes, []*wire.MsgTx) {
		txs := make([]*wire.MsgTx, 4)

		// In order to be able to test using the SPV wallet, we need to properly
		// set the size of each of the outputs. The SPV wallet will parse these
		// transactions and calculate the fee on its own instead of just returning
		// what was set in getTransactionMap.
		changeOutputAmounts := make([]int64, 4)
		changeOutputAmounts[3] = changeVal
		swapAmount := int64(2e6)
		for i := 2; i >= 0; i-- {
			changeAmount := int64(toSatoshi(fees[i+1])) + changeOutputAmounts[i+1]
			if !(i == 1 && addAcceleration) {
				changeAmount += swapAmount
			}
			changeOutputAmounts[i] = changeAmount
		}

		// The initial transaction in the chain has multiple inputs.
		fundingCoinsTotalOutput := changeOutputAmounts[0] + swapAmount + int64(toSatoshi(fees[0]))
		fundingTx := wire.MsgTx{
			TxIn: []*wire.TxIn{{
				PreviousOutPoint: wire.OutPoint{
					Hash:  *tTxHash,
					Index: 0,
				}},
				{PreviousOutPoint: wire.OutPoint{
					Hash:  *tTxHash,
					Index: 1,
				}},
			},
			TxOut: []*wire.TxOut{{
				Value: fundingCoinsTotalOutput / 2,
			}, {
				Value: fundingCoinsTotalOutput - fundingCoinsTotalOutput/2,
			}},
		}
		fudingTxHex, _ := serializeMsgTx(&fundingTx)
		node.getTransactionMap[fundingTx.TxHash().String()] = &GetTransactionResult{Bytes: fudingTxHex, BlockHash: blockHash100.String()}

		txs[0] = &wire.MsgTx{
			TxIn: []*wire.TxIn{{
				PreviousOutPoint: wire.OutPoint{
					Hash:  fundingTx.TxHash(),
					Index: 0,
				}},
				{PreviousOutPoint: wire.OutPoint{
					Hash:  fundingTx.TxHash(),
					Index: 1,
				}},
			},
			TxOut: []*wire.TxOut{{
				Value: changeOutputAmounts[0],
			}, {
				Value: swapAmount,
			}},
		}
		for i := 1; i < 4; i++ {
			txs[i] = &wire.MsgTx{
				TxIn: []*wire.TxIn{{
					PreviousOutPoint: wire.OutPoint{
						Hash:  txs[i-1].TxHash(),
						Index: 0,
					}},
				},
				TxOut: []*wire.TxOut{{
					Value: changeOutputAmounts[i],
				}},
			}
			if !(i == 2 && addAcceleration) {
				txs[i].TxOut = append(txs[i].TxOut, &wire.TxOut{Value: swapAmount})
			}
		}

		swapCoins := make([]dex.Bytes, 0, len(txs))
		accelerationCoins := make([]dex.Bytes, 0, 1)
		var changeCoin dex.Bytes
		for i, tx := range txs {
			hash := tx.TxHash()

			if i == 2 && addAcceleration {
				accelerationCoins = append(accelerationCoins, ToCoinID(&hash, 0))
			} else {
				ToCoinID(&hash, 0)
				swapCoins = append(swapCoins, ToCoinID(&hash, 0))
			}

			if i == len(txs)-1 {
				changeCoin = ToCoinID(&hash, 0)
				if addChangeToUnspent {
					node.listUnspent = append(node.listUnspent, &ListUnspentResult{
						TxID: hash.String(),
						Vout: 0,
					})
				}
			}
		}

		return swapCoins, accelerationCoins, changeCoin, txs
	}

	addUTXOToNode := func(confs uint32, segwit bool, amount uint64, node *testData) {
		var scriptPubKey []byte
		if segwit {
			scriptPubKey = tP2WPKH
		} else {
			scriptPubKey = tP2PKH
		}

		node.listUnspent = append(node.listUnspent, &ListUnspentResult{
			TxID:          hex.EncodeToString(encode.RandomBytes(32)),
			Address:       "1Bggq7Vu5oaoLFV1NNp5KhAzcku83qQhgi",
			Amount:        toBTC(amount),
			Confirmations: confs,
			ScriptPubKey:  scriptPubKey,
			Spendable:     true,
			Solvable:      true,
		})

		var prevChainHash chainhash.Hash
		copy(prevChainHash[:], encode.RandomBytes(32))

		tx := &wire.MsgTx{
			TxIn: []*wire.TxIn{{
				PreviousOutPoint: wire.OutPoint{
					Hash:  prevChainHash,
					Index: 0,
				}},
			},
			TxOut: []*wire.TxOut{
				{
					Value: int64(amount),
				},
			}}
		unspentTxHex, err := serializeMsgTx(tx)
		if err != nil {
			panic(fmt.Sprintf("could not serialize: %v", err))
		}
		var blockHash string
		if confs == 0 {
			blockHash = blockHash100.String()
		}

		node.getTransactionMap[node.listUnspent[len(node.listUnspent)-1].TxID] = &GetTransactionResult{
			TxID:          tx.TxHash().String(),
			Bytes:         unspentTxHex,
			BlockHash:     blockHash,
			Confirmations: uint64(confs)}
	}

	totalInputOutput := func(tx *wire.MsgTx) (uint64, uint64) {
		var in, out uint64
		for _, input := range tx.TxIn {
			inputGtr, found := node.getTransactionMap[input.PreviousOutPoint.Hash.String()]
			if !found {
				t.Fatalf("tx id not found: %v", input.PreviousOutPoint.Hash.String())
			}
			inputTx, err := msgTxFromHex(inputGtr.Bytes.String())
			if err != nil {
				t.Fatalf("failed to deserialize tx: %v", err)
			}
			in += uint64(inputTx.TxOut[input.PreviousOutPoint.Index].Value)
		}

		for _, output := range node.sentRawTx.TxOut {
			out += uint64(output.Value)
		}

		return in, out
	}

	calculateChangeTxSize := func(hasChange, segwit bool, numInputs int) uint64 {
		baseSize := dexbtc.MinimumTxOverhead

		var inputSize, witnessSize, outputSize int
		if segwit {
			witnessSize = dexbtc.RedeemP2WPKHInputWitnessWeight*numInputs + 2
			inputSize = dexbtc.RedeemP2WPKHInputSize * numInputs
			outputSize = dexbtc.P2WPKHOutputSize
		} else {
			inputSize = numInputs * dexbtc.RedeemP2PKHInputSize
			outputSize = dexbtc.P2PKHOutputSize
		}

		baseSize += inputSize
		if hasChange {
			baseSize += outputSize
		}

		txWeight := baseSize*4 + witnessSize
		txSize := (txWeight + 3) / 4
		return uint64(txSize)
	}

	var changeAmount int64 = 21350
	var newFeeRate uint64 = 50
	fees := []float64{0.00002, 0.000005, 0.00001, 0.00001}
	confs := []uint64{0, 0, 0, 0}
	_, _, _, txs := getAccelerationParams(changeAmount, false, false, fees, node)
	expectedFees := (sumTxSizes(txs, confs)+calculateChangeTxSize(false, segwit, 1))*newFeeRate - sumFees(fees, confs)
	_, _, _, txs = getAccelerationParams(changeAmount, true, false, fees, node)
	expectedFeesWithChange := (sumTxSizes(txs, confs)+calculateChangeTxSize(true, segwit, 1))*newFeeRate - sumFees(fees, confs)

	// See dexbtc.IsDust for the source of this dustCoverage voodoo.
	var dustCoverage uint64
	if segwit {
		dustCoverage = (dexbtc.P2WPKHOutputSize + 41 + (107 / 4)) * 3 * newFeeRate
	} else {
		dustCoverage = (dexbtc.P2PKHOutputSize + 41 + 107) * 3 * newFeeRate
	}

	type utxo struct {
		amount uint64
		confs  uint32
	}

	tests := []struct {
		name                      string
		changeNotInUnspent        bool
		addPreviousAcceleration   bool
		numUtxosUsed              int
		changeAmount              int64
		lockChange                bool
		scrambleSwapCoins         bool
		expectChange              bool
		utxos                     []utxo
		fees                      []float64
		confs                     []uint64
		requiredForRemainingSwaps uint64
		expectChangeLocked        bool
		txTimeWithinLimit         bool

		// needed to test AccelerateOrder and AccelerationEstimate
		expectAccelerateOrderErr      bool
		expectAccelerationEstimateErr bool
		newFeeRate                    uint64

		// needed to test PreAccelerate
		suggestedFeeRate       uint64
		expectPreAccelerateErr bool
		expectTooEarly         bool
	}{
		{
			name:                          "change not in utxo set",
			changeAmount:                  int64(expectedFees),
			fees:                          []float64{0.00002, 0.000005, 0.00001, 0.00001},
			confs:                         []uint64{0, 0, 0, 0},
			newFeeRate:                    50,
			expectAccelerationEstimateErr: true,
			expectAccelerateOrderErr:      true,
			expectPreAccelerateErr:        true,
			changeNotInUnspent:            true,
		},
		{
			name:             "just enough without change",
			changeAmount:     int64(expectedFees),
			fees:             []float64{0.00002, 0.000005, 0.00001, 0.00001},
			confs:            confs,
			newFeeRate:       newFeeRate,
			suggestedFeeRate: 30,
		},
		{
			name:                    "works with acceleration",
			changeAmount:            int64(expectedFees),
			fees:                    []float64{0.00002, 0.000005, 0.00001, 0.00001},
			confs:                   confs,
			newFeeRate:              newFeeRate,
			addPreviousAcceleration: true,
		},
		{
			name:              "scramble swap coins",
			changeAmount:      int64(expectedFees),
			fees:              []float64{0.00002, 0.000005, 0.00001, 0.00001},
			confs:             []uint64{0, 0, 0, 0},
			newFeeRate:        newFeeRate,
			scrambleSwapCoins: true,
		},
		{
			name:                          "not enough with just change",
			changeAmount:                  int64(expectedFees - 1),
			fees:                          []float64{0.00002, 0.000005, 0.00001, 0.00001},
			confs:                         []uint64{0, 0, 0, 0},
			newFeeRate:                    newFeeRate,
			expectAccelerationEstimateErr: true,
			expectAccelerateOrderErr:      true,
		},
		{
			name:         "add greater than dust amount to change",
			changeAmount: int64(expectedFeesWithChange + dustCoverage),
			fees:         []float64{0.00002, 0.000005, 0.00001, 0.00001},
			confs:        []uint64{0, 0, 0, 0},
			newFeeRate:   newFeeRate,
			expectChange: true,
		},
		{
			name:         "add dust amount to change",
			changeAmount: int64(expectedFeesWithChange + dustCoverage - 1),
			fees:         []float64{0.00002, 0.000005, 0.00001, 0.00001},
			confs:        []uint64{0, 0, 0, 0},
			newFeeRate:   newFeeRate,
			expectChange: false,
		},
		{
			name:         "don't accelerate confirmed transactions",
			changeAmount: int64(expectedFeesWithChange + dustCoverage),
			fees:         []float64{0.00002, 0.000005, 0.00001, 0.00001},
			confs:        []uint64{1, 1, 0, 0},
			newFeeRate:   newFeeRate,
			expectChange: true,
		},
		{
			name:                          "not enough",
			changeAmount:                  int64(expectedFees - 1),
			fees:                          []float64{0.00002, 0.000005, 0.00001, 0.00001},
			confs:                         []uint64{0, 0, 0, 0},
			newFeeRate:                    newFeeRate,
			expectAccelerationEstimateErr: true,
			expectAccelerateOrderErr:      true,
		},
		{
			name:                          "not enough with 0-conf utxo in wallet",
			changeAmount:                  int64(expectedFees - 1),
			fees:                          []float64{0.00002, 0.000005, 0.00001, 0.00001},
			confs:                         []uint64{0, 0, 0, 0},
			newFeeRate:                    newFeeRate,
			expectAccelerationEstimateErr: true,
			expectAccelerateOrderErr:      true,
			utxos: []utxo{{
				confs:  0,
				amount: 5e9,
			}},
		},
		{
			name:         "enough with 1-conf utxo in wallet",
			changeAmount: int64(expectedFees - 1),
			fees:         []float64{0.00002, 0.000005, 0.00001, 0.00001},
			confs:        []uint64{0, 0, 0, 0},
			newFeeRate:   newFeeRate,
			numUtxosUsed: 1,
			expectChange: true,
			utxos: []utxo{{
				confs:  1,
				amount: 2e6,
			}},
		},
		{
			name:                          "not enough for remaining swaps",
			changeAmount:                  int64(expectedFees - 1),
			fees:                          fees,
			confs:                         []uint64{0, 0, 0, 0},
			newFeeRate:                    newFeeRate,
			numUtxosUsed:                  1,
			expectAccelerationEstimateErr: true,
			expectAccelerateOrderErr:      true,
			expectChange:                  true,
			requiredForRemainingSwaps:     2e6,
			utxos: []utxo{{
				confs:  1,
				amount: 2e6,
			}},
		},
		{
			name:                      "enough for remaining swaps",
			changeAmount:              int64(expectedFees - 1),
			fees:                      fees,
			confs:                     []uint64{0, 0, 0, 0},
			newFeeRate:                newFeeRate,
			numUtxosUsed:              2,
			expectChange:              true,
			expectChangeLocked:        true,
			requiredForRemainingSwaps: 2e6,
			utxos: []utxo{
				{
					confs:  1,
					amount: 2e6,
				},
				{
					confs:  1,
					amount: 1e6,
				},
			},
		},
		{
			name:                      "locked change, required for remaining >0",
			changeAmount:              int64(expectedFees - 1),
			fees:                      fees,
			confs:                     []uint64{0, 0, 0, 0},
			newFeeRate:                newFeeRate,
			numUtxosUsed:              2,
			expectChange:              true,
			expectChangeLocked:        true,
			requiredForRemainingSwaps: 2e6,
			lockChange:                true,
			utxos: []utxo{
				{
					confs:  1,
					amount: 2e6,
				},
				{
					confs:  1,
					amount: 1e6,
				},
			},
		},
		{
			name:                          "locked change, required for remaining == 0",
			changeAmount:                  int64(expectedFees - 1),
			fees:                          []float64{0.00002, 0.000005, 0.00001, 0.00001},
			confs:                         []uint64{0, 0, 0, 0},
			newFeeRate:                    newFeeRate,
			numUtxosUsed:                  2,
			expectChange:                  true,
			expectAccelerationEstimateErr: true,
			expectAccelerateOrderErr:      true,
			expectPreAccelerateErr:        true,
			lockChange:                    true,
			utxos: []utxo{
				{
					confs:  1,
					amount: 2e6,
				},
				{
					confs:  1,
					amount: 1e6,
				},
			},
		},
		{
			name:              "tx time within limit",
			txTimeWithinLimit: true,
			expectTooEarly:    true,
			changeAmount:      int64(expectedFees),
			fees:              []float64{0.00002, 0.000005, 0.00001, 0.00001},
			confs:             confs,
			newFeeRate:        newFeeRate,
			suggestedFeeRate:  30,
		},
	}

	for _, test := range tests {
		node.listUnspent = []*ListUnspentResult{}
		swapCoins, accelerations, changeCoin, txs := getAccelerationParams(test.changeAmount, !test.changeNotInUnspent, test.addPreviousAcceleration, test.fees, node)
		expectedFees := (sumTxSizes(txs, test.confs)+calculateChangeTxSize(test.expectChange, segwit, 1+test.numUtxosUsed))*test.newFeeRate - sumFees(test.fees, test.confs)
		if !test.expectChange {
			expectedFees = uint64(test.changeAmount)
		}
		if test.scrambleSwapCoins {
			temp := swapCoins[0]
			swapCoins[0] = swapCoins[2]
			swapCoins[2] = swapCoins[1]
			swapCoins[1] = temp
		}
		if test.lockChange {
			changeTxID, changeVout, _ := decodeCoinID(changeCoin)
			node.listLockUnspent = []*RPCOutpoint{
				{
					TxID: changeTxID.String(),
					Vout: changeVout,
				},
			}
		}
		for _, utxo := range test.utxos {
			addUTXOToNode(utxo.confs, segwit, utxo.amount, node)
		}

		loadTxsIntoNode(txs, test.fees, test.confs, node, test.txTimeWithinLimit, t)

		testAccelerateOrder := func() {
			change, txID, err := wallet.AccelerateOrder(swapCoins, accelerations, changeCoin, test.requiredForRemainingSwaps, test.newFeeRate)
			if test.expectAccelerateOrderErr {
				if err == nil {
					t.Fatalf("%s: expected AccelerateOrder error but did not get", test.name)
				}
				return
			}
			if err != nil {
				t.Fatalf("%s: unexpected error: %v", test.name, err)
			}

			in, out := totalInputOutput(node.sentRawTx)
			lastTxFee := in - out
			if expectedFees != lastTxFee {
				t.Fatalf("%s: expected fee to be %d but got %d", test.name, expectedFees, lastTxFee)
			}

			if node.sentRawTx.TxHash().String() != txID {
				t.Fatalf("%s: expected tx id %s, but got %s", test.name, node.sentRawTx.TxHash().String(), txID)
			}

			if test.expectChange {
				if out == 0 {
					t.Fatalf("%s: expected change but did not get", test.name)
				}
				if change == nil {
					t.Fatalf("%s: expected change, but got nil", test.name)
				}

				changeScript := node.sentRawTx.TxOut[0].PkScript

				changeAddr, err := btcutil.DecodeAddress(node.changeAddr, &chaincfg.MainNetParams)
				if err != nil {
					t.Fatalf("%s: unexpected error: %v", test.name, err)
				}
				expectedChangeScript, err := txscript.PayToAddrScript(changeAddr)
				if err != nil {
					t.Fatalf("%s: unexpected error: %v", test.name, err)
				}

				if !bytes.Equal(changeScript, expectedChangeScript) {
					t.Fatalf("%s: expected change script != actual", test.name)
				}

				changeTxHash, changeVout, err := decodeCoinID(change.ID())
				if err != nil {
					t.Fatalf("%s: unexpected error: %v", test.name, err)
				}
				if *changeTxHash != node.sentRawTx.TxHash() {
					t.Fatalf("%s: change tx hash %s != expected: %s", test.name, changeTxHash, node.sentRawTx.TxHash())
				}
				if changeVout != 0 {
					t.Fatalf("%s: change vout %v != expected: 0", test.name, changeVout)
				}

				var changeLocked bool
				for _, coin := range node.lockedCoins {
					if changeVout == coin.Vout && changeTxHash.String() == coin.TxID {
						changeLocked = true
					}
				}
				if changeLocked != test.expectChangeLocked {
					t.Fatalf("%s: expected change locked = %v, but was %v", test.name, test.expectChangeLocked, changeLocked)
				}
			} else {
				if out > 0 {
					t.Fatalf("%s: not expecting change but got %v", test.name, out)
				}
				if change != nil {
					t.Fatalf("%s: not expecting change but accelerate returned: %+v", test.name, change)
				}
			}

			if test.requiredForRemainingSwaps > out {
				t.Fatalf("%s: %d needed of remaining swaps, but output was only %d", test.name, test.requiredForRemainingSwaps, out)
			}
		}
		testAccelerateOrder()

		testAccelerationEstimate := func() {
			estimate, err := wallet.AccelerationEstimate(swapCoins, accelerations, changeCoin, test.requiredForRemainingSwaps, test.newFeeRate)
			if test.expectAccelerationEstimateErr {
				if err == nil {
					t.Fatalf("%s: expected AccelerationEstimate error but did not get", test.name)
				}
				return
			}
			if err != nil {
				t.Fatalf("%s: unexpected error: %v", test.name, err)
			}
			if estimate != expectedFees {
				t.Fatalf("%s: estimate %v != expected fees %v", test.name, estimate, expectedFees)
			}
		}
		testAccelerationEstimate()

		testPreAccelerate := func() {
			currentRate, suggestedRange, earlyAcceleration, err := wallet.PreAccelerate(swapCoins, accelerations, changeCoin, test.requiredForRemainingSwaps, test.suggestedFeeRate)
			if test.expectPreAccelerateErr {
				if err == nil {
					t.Fatalf("%s: expected PreAccelerate error but did not get", test.name)
				}
				return
			}
			if err != nil {
				t.Fatalf("%s: unexpected error: %v", test.name, err)
			}

			if test.expectTooEarly != (earlyAcceleration != nil) {
				t.Fatalf("%s: expected early acceleration %v, but got %v", test.name, test.expectTooEarly, earlyAcceleration)
			}

			var totalSize, totalFee uint64
			for i, tx := range txs {
				if test.confs[i] == 0 {
					totalSize += dexbtc.MsgTxVBytes(tx)
					totalFee += toSatoshi(test.fees[i])
				}
			}

			expectedRate := totalFee / totalSize
			if expectedRate != currentRate {
				t.Fatalf("%s: expected current rate %v != actual %v", test.name, expectedRate, currentRate)
			}

			totalFeePossible := totalFee + uint64(test.changeAmount) - test.requiredForRemainingSwaps
			var numConfirmedUTXO int
			for _, utxo := range test.utxos {
				if utxo.confs > 0 {
					totalFeePossible += utxo.amount
					numConfirmedUTXO++
				}
			}
			totalSize += calculateChangeTxSize(test.expectChange, segwit, 1+numConfirmedUTXO)
			maxRatePossible := totalFeePossible / totalSize

			expectedRangeHigh := expectedRate * 5
			if test.suggestedFeeRate > expectedRate {
				expectedRangeHigh = test.suggestedFeeRate * 5
			}
			if maxRatePossible < expectedRangeHigh {
				expectedRangeHigh = maxRatePossible
			}

			expectedRangeLowX := float64(expectedRate+1) / float64(expectedRate)
			if suggestedRange.Start.X != expectedRangeLowX {
				t.Fatalf("%s: start of range should be %v on X, got: %v", test.name, expectedRangeLowX, suggestedRange.Start.X)
			}

			if suggestedRange.Start.Y != float64(expectedRate+1) {
				t.Fatalf("%s: expected start of range on Y to be: %v, but got %v", test.name, float64(expectedRate), suggestedRange.Start.Y)
			}

			if suggestedRange.End.Y != float64(expectedRangeHigh) {
				t.Fatalf("%s: expected end of range on Y to be: %v, but got %v", test.name, float64(expectedRangeHigh), suggestedRange.End.Y)
			}

			expectedRangeHighX := float64(expectedRangeHigh) / float64(expectedRate)
			if suggestedRange.End.X != expectedRangeHighX {
				t.Fatalf("%s: expected end of range on X to be: %v, but got %v", test.name, float64(expectedRate), suggestedRange.End.X)
			}
		}
		testPreAccelerate()
	}
}

func TestGetTxFee(t *testing.T) {
	runRubric(t, testGetTxFee)
}

func testGetTxFee(t *testing.T, segwit bool, walletType string) {
	w, node, shutdown := tNewWallet(segwit, walletType)
	defer shutdown()

	inputTx := &wire.MsgTx{
		TxIn: []*wire.TxIn{{}},
		TxOut: []*wire.TxOut{
			{
				Value: 2e6,
			},
			{
				Value: 3e6,
			},
		},
	}
	txBytes, err := serializeMsgTx(inputTx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	node.getTransactionMap = map[string]*GetTransactionResult{
		"any": {
			Bytes: txBytes,
		},
	}

	tests := []struct {
		name        string
		tx          *wire.MsgTx
		expectErr   bool
		expectedFee uint64
		getTxErr    bool
	}{
		{
			name: "ok",
			tx: &wire.MsgTx{
				TxIn: []*wire.TxIn{{
					PreviousOutPoint: wire.OutPoint{
						Hash:  inputTx.TxHash(),
						Index: 0,
					}},
					{PreviousOutPoint: wire.OutPoint{
						Hash:  inputTx.TxHash(),
						Index: 1,
					}},
				},
				TxOut: []*wire.TxOut{{
					Value: 4e6,
				}},
			},
			expectedFee: 1e6,
		},
		{
			name: "get transaction error",
			tx: &wire.MsgTx{
				TxIn: []*wire.TxIn{{
					PreviousOutPoint: wire.OutPoint{
						Hash:  inputTx.TxHash(),
						Index: 0,
					}},
					{PreviousOutPoint: wire.OutPoint{
						Hash:  inputTx.TxHash(),
						Index: 1,
					}},
				},
				TxOut: []*wire.TxOut{{
					Value: 4e6,
				}},
			},
			getTxErr:  true,
			expectErr: true,
		},
		{
			name: "invalid prev output index error",
			tx: &wire.MsgTx{
				TxIn: []*wire.TxIn{{
					PreviousOutPoint: wire.OutPoint{
						Hash:  inputTx.TxHash(),
						Index: 0,
					}},
					{PreviousOutPoint: wire.OutPoint{
						Hash:  inputTx.TxHash(),
						Index: 2,
					}},
				},
				TxOut: []*wire.TxOut{{
					Value: 4e6,
				}},
			},
			expectErr: true,
		},
		{
			name: "tx out > in error",
			tx: &wire.MsgTx{
				TxIn: []*wire.TxIn{{
					PreviousOutPoint: wire.OutPoint{
						Hash:  inputTx.TxHash(),
						Index: 0,
					}},
					{PreviousOutPoint: wire.OutPoint{
						Hash:  inputTx.TxHash(),
						Index: 2,
					}},
				},
				TxOut: []*wire.TxOut{{
					Value: 8e6,
				}},
			},
			expectErr: true,
		},
	}

	for _, test := range tests {
		node.getTransactionErr = nil
		if test.getTxErr {
			node.getTransactionErr = errors.New("")
		}

		fee, err := w.getTxFee(test.tx)
		if test.expectErr {
			if err == nil {
				t.Fatalf("%s: expected error but did not get", test.name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", test.name, err)
		}
		if fee != test.expectedFee {
			t.Fatalf("%s: expected fee %d, but got %d", test.name, test.expectedFee, fee)
		}
	}
}

func TestTooEarlyToAccelerate(t *testing.T) {
	type tx struct {
		secondsBeforeNow uint64
		confirmations    uint64
	}
	tests := []struct {
		name           string
		accelerations  []tx
		swaps          []tx
		expectedReturn *asset.EarlyAcceleration
		expectError    bool
	}{
		{
			name: "all confirmed",
			accelerations: []tx{
				{secondsBeforeNow: minTimeBeforeAcceleration + 800,
					confirmations: 2},
				{secondsBeforeNow: minTimeBeforeAcceleration + 300,
					confirmations: 1}},
			swaps: []tx{
				{secondsBeforeNow: minTimeBeforeAcceleration + 1000,
					confirmations: 2},
				{secondsBeforeNow: minTimeBeforeAcceleration + 500,
					confirmations: 1}},
			expectError: true,
		},
		{
			name:          "no accelerations, not too early",
			accelerations: []tx{},
			swaps: []tx{
				{secondsBeforeNow: minTimeBeforeAcceleration + 1000,
					confirmations: 2},
				{secondsBeforeNow: minTimeBeforeAcceleration + 500,
					confirmations: 0},
				{secondsBeforeNow: minTimeBeforeAcceleration + 300,
					confirmations: 0},
				{secondsBeforeNow: minTimeBeforeAcceleration + 800,
					confirmations: 2}},
		},
		{
			name: "acceleration after unconfirmed swap, not too early",
			accelerations: []tx{
				{secondsBeforeNow: minTimeBeforeAcceleration + 300,
					confirmations: 0}},
			swaps: []tx{
				{secondsBeforeNow: minTimeBeforeAcceleration + 1000,
					confirmations: 2},
				{secondsBeforeNow: minTimeBeforeAcceleration + 800,
					confirmations: 2},
				{secondsBeforeNow: minTimeBeforeAcceleration + 500,
					confirmations: 0}},
		},
		{
			name: "acceleration after unconfirmed swap, too early",
			accelerations: []tx{
				{secondsBeforeNow: minTimeBeforeAcceleration - 300,
					confirmations: 0}},
			swaps: []tx{
				{secondsBeforeNow: minTimeBeforeAcceleration + 1000,
					confirmations: 2},
				{secondsBeforeNow: minTimeBeforeAcceleration + 800,
					confirmations: 2},
				{secondsBeforeNow: minTimeBeforeAcceleration + 500,
					confirmations: 0}},
			expectedReturn: &asset.EarlyAcceleration{
				TimePast:       minTimeBeforeAcceleration - 300,
				WasAccelerated: true,
			},
		},
		{
			name:          "no accelerations, too early",
			accelerations: []tx{},
			swaps: []tx{
				{secondsBeforeNow: minTimeBeforeAcceleration + 1000,
					confirmations: 2},
				{secondsBeforeNow: minTimeBeforeAcceleration + 800,
					confirmations: 2},
				{secondsBeforeNow: minTimeBeforeAcceleration - 300,
					confirmations: 0},
				{secondsBeforeNow: minTimeBeforeAcceleration - 500,
					confirmations: 0}},
			expectedReturn: &asset.EarlyAcceleration{
				TimePast:       minTimeBeforeAcceleration - 300,
				WasAccelerated: false,
			},
		},
		{
			name: "only accelerations are unconfirmed, too early",
			accelerations: []tx{
				{secondsBeforeNow: minTimeBeforeAcceleration - 300,
					confirmations: 0}},
			swaps: []tx{
				{secondsBeforeNow: minTimeBeforeAcceleration + 1000,
					confirmations: 2},
				{secondsBeforeNow: minTimeBeforeAcceleration + 800,
					confirmations: 2},
			},
			expectedReturn: &asset.EarlyAcceleration{
				TimePast:       minTimeBeforeAcceleration - 300,
				WasAccelerated: true,
			},
		},
	}

	for _, test := range tests {
		swapTxs := make([]*GetTransactionResult, 0, len(test.swaps))
		accelerationTxs := make([]*GetTransactionResult, 0, len(test.accelerations))
		now := time.Now().Unix()

		for _, swap := range test.swaps {
			var txHash chainhash.Hash
			copy(txHash[:], encode.RandomBytes(32))
			swapTxs = append(swapTxs, &GetTransactionResult{
				TxID:          txHash.String(),
				Confirmations: swap.confirmations,
				Time:          uint64(now) - swap.secondsBeforeNow,
			})
		}

		for _, acceleration := range test.accelerations {
			var txHash chainhash.Hash
			copy(txHash[:], encode.RandomBytes(32))
			accelerationTxs = append(accelerationTxs, &GetTransactionResult{
				TxID:          txHash.String(),
				Confirmations: acceleration.confirmations,
				Time:          uint64(now) - acceleration.secondsBeforeNow,
			})
		}

		earlyAcceleration, err := tooEarlyToAccelerate(swapTxs, accelerationTxs)
		if test.expectError {
			if err == nil {
				t.Fatalf("%s: expected error but did not get", test.name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", test.name, err)
		}

		if test.expectedReturn == nil && earlyAcceleration == nil {
			continue
		}
		if test.expectedReturn == nil && earlyAcceleration != nil {
			t.Fatalf("%s: expected return to be nil, but got %+v", test.name, earlyAcceleration)
		}
		if test.expectedReturn != nil && earlyAcceleration == nil {
			t.Fatalf("%s: expected return to not be nil, but got nil", test.name)
		}
		if test.expectedReturn.TimePast != earlyAcceleration.TimePast ||
			test.expectedReturn.WasAccelerated != earlyAcceleration.WasAccelerated {
			t.Fatalf("%s: expected %+v, got %+v", test.name, test.expectedReturn, earlyAcceleration)
		}
	}
}

type tReconfigurer struct {
	*rpcClient
	restart bool
	err     error
}

func (r *tReconfigurer) reconfigure(walletCfg *asset.WalletConfig, currentAddress string) (restartRequired bool, err error) {
	return r.restart, r.err
}

func TestReconfigure(t *testing.T) {
	wallet, _, shutdown := tNewWallet(false, walletTypeRPC)
	// We don't need the wallet running, and we need to write to the node field,
	// which is used in the block loop.
	shutdown()

	reconfigurer := &tReconfigurer{rpcClient: wallet.node.(*rpcClient)}
	wallet.baseWallet.setNode(reconfigurer)

	cfg := &asset.WalletConfig{
		Settings: map[string]string{
			"redeemconftarget": "3",
		},
	}

	restart, err := wallet.Reconfigure(tCtx, cfg, "")
	if err != nil {
		t.Fatalf("initial Reconfigure error: %v", err)
	}
	if restart {
		t.Fatal("restart = false not propagated")
	}
	if wallet.redeemConfTarget() != 3 {
		t.Fatal("redeemconftarget not updated", wallet.redeemConfTarget())
	}

	cfg.Settings["redeemconftarget"] = "2"

	reconfigurer.err = tErr
	if _, err = wallet.Reconfigure(tCtx, cfg, ""); err == nil {
		t.Fatal("node reconfigure error not propagated")
	}
	reconfigurer.err = nil
	// Redeem target should be unchanged
	if wallet.redeemConfTarget() != 3 {
		t.Fatal("redeemconftarget updated for node reconfigure error", wallet.redeemConfTarget())
	}

	reconfigurer.restart = true
	if restart, err := wallet.Reconfigure(tCtx, cfg, ""); err != nil {
		t.Fatal("Reconfigure error for restart = true")
	} else if !restart {
		t.Fatal("restart = true not propagated")
	}
	reconfigurer.restart = false

	// One last success, and make sure baseWalletConfig is updated.
	if _, err := wallet.Reconfigure(tCtx, cfg, ""); err != nil {
		t.Fatalf("Reconfigure error for final success: %v", err)
	}
	// Redeem target should be changed now.
	if wallet.redeemConfTarget() != 2 {
		t.Fatal("redeemconftarget not updated in final success", wallet.redeemConfTarget())
	}
}

func TestConfirmRedemption(t *testing.T) {
	segwit := true
	wallet, node, shutdown := tNewWallet(segwit, walletTypeRPC)
	defer shutdown()

	swapVal := toSatoshi(5)

	secret, _, _, contract, addr, _, lockTime := makeSwapContract(segwit, time.Hour*12)

	coin := NewOutput(tTxHash, 0, swapVal)
	ci := &asset.AuditInfo{
		Coin:       coin,
		Contract:   contract,
		Recipient:  addr.String(),
		Expiration: lockTime,
	}

	redemption := &asset.Redemption{
		Spends: ci,
		Secret: secret,
	}

	coinID := coin.ID()

	privBytes, _ := hex.DecodeString("b07209eec1a8fb6cfe5cb6ace36567406971a75c330db7101fb21bc679bc5330")
	privKey, _ := btcec.PrivKeyFromBytes(privBytes)
	wif, err := btcutil.NewWIF(privKey, &chaincfg.MainNetParams, true)
	if err != nil {
		t.Fatalf("error encoding wif: %v", err)
	}

	node.changeAddr = tP2WPKHAddr
	node.privKeyForAddr = wif

	tests := []struct {
		name                 string
		redemption           *asset.Redemption
		coinID               []byte
		wantErr              bool
		wantConfs            uint64
		txOutRes             *btcjson.GetTxOutResult
		getTransactionResult *GetTransactionResult
		txOutErr             error
		getTransactionErr    error
	}{{
		name:                 "ok and found",
		coinID:               coinID,
		redemption:           redemption,
		getTransactionResult: new(GetTransactionResult),
	}, {
		name:       "ok spent by someone but not sure who",
		coinID:     coinID,
		redemption: redemption,
		wantConfs:  requiredRedeemConfirms,
	}, {
		name:       "ok but sending new tx",
		coinID:     coinID,
		redemption: redemption,
		txOutRes:   new(btcjson.GetTxOutResult),
	}, {
		name:       "decode coin error",
		redemption: redemption,
		wantErr:    true,
	}, {
		name:       "error finding contract output",
		coinID:     coinID,
		redemption: redemption,
		txOutErr:   errors.New(""),
		wantErr:    true,
	}, {
		name:              "error finding redeem tx",
		coinID:            coinID,
		redemption:        redemption,
		getTransactionErr: errors.New(""),
		wantErr:           true,
	}, {
		name:   "redemption error",
		coinID: coinID,
		redemption: func() *asset.Redemption {
			ci := &asset.AuditInfo{
				Coin: coin,
				// Contract:   contract,
				Recipient:  addr.String(),
				Expiration: lockTime,
			}

			return &asset.Redemption{
				Spends: ci,
				Secret: secret,
			}
		}(),
		txOutRes: new(btcjson.GetTxOutResult),
		wantErr:  true,
	}}
	for _, test := range tests {
		node.txOutRes = test.txOutRes
		node.txOutErr = test.txOutErr
		node.getTransactionErr = test.getTransactionErr
		node.getTransactionMap[tTxID] = test.getTransactionResult

		status, err := wallet.ConfirmRedemption(test.coinID, test.redemption, 0)
		if test.wantErr {
			if err == nil {
				t.Fatalf("%q: expected error", test.name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%q: unexpected error: %v", test.name, err)
		}
		if status.Confs != test.wantConfs {
			t.Fatalf("%q: wanted %d confs but got %d", test.name, test.wantConfs, status.Confs)
		}
	}
}

func TestAddressRecycling(t *testing.T) {
	w, td, shutdown := tNewWallet(false, walletTypeSPV)
	defer shutdown()

	compareAddrLists := func(tag string, exp, actual []string) {
		if len(exp) != len(actual) {
			t.Fatalf("%s: Wrong number of recycled addrs. Expected %d, got %d", tag, len(exp), len(actual))
		}
		unfound := make(map[string]bool, len(exp))
		for _, addr := range exp {
			unfound[addr] = true
		}
		for _, addr := range actual {
			delete(unfound, addr)
		}
		if len(unfound) > 0 {
			t.Fatalf("%s: Wrong addresses stored. Expected %+v, got %+v", tag, exp, actual)
		}
	}

	checkAddrs := func(tag string, expAddrs ...string) {
		memList := make([]string, 0, len(w.ar.addrs))
		for addr := range w.ar.addrs {
			memList = append(memList, addr)
		}
		compareAddrLists(tag, expAddrs, memList)
	}

	addr1, _ := btcutil.NewAddressPubKeyHash(encode.RandomBytes(20), &chaincfg.MainNetParams)
	addr2, _ := btcutil.NewAddressPubKeyHash(encode.RandomBytes(20), &chaincfg.MainNetParams)

	w.ReturnRedemptionAddress(addr1.String())

	checkAddrs("first single addr", addr1.String())

	td.ownsAddress = true
	redemptionAddr, _ := w.RedemptionAddress()
	if redemptionAddr != addr1.String() {
		t.Fatalf("recycled address not returned for redemption address")
	}

	contracts := make([][]byte, 2)
	contracts[0], _ = dexbtc.MakeContract(addr1, addr2, encode.RandomBytes(32), time.Now().Unix(), false, &chaincfg.MainNetParams)
	contracts[1], _ = dexbtc.MakeContract(addr2, addr1, encode.RandomBytes(32), time.Now().Unix(), false, &chaincfg.MainNetParams)

	w.ReturnRefundContracts(contracts)

	checkAddrs("two addrs", addr1.String(), addr2.String())

	// Check that unowned addresses are not returned
	td.ownsAddress = false
	td.newAddress = "1JoiKZz2QRd47ARtcYgvgxC9jhnre9aphv"
	priv, _ := btcec.NewPrivateKey()
	td.privKeyForAddr, _ = btcutil.NewWIF(priv, &chaincfg.MainNetParams, true)
	redemptionAddr, err := w.RedemptionAddress()
	if err != nil {
		t.Fatalf("RedemptionAddress error: %v", err)
	}
	if redemptionAddr == addr1.String() || redemptionAddr == addr2.String() {
		t.Fatalf("unowned address returned (1)")
	}
	redemptionAddr, _ = w.RedemptionAddress()
	if redemptionAddr == addr1.String() || redemptionAddr == addr2.String() {
		t.Fatalf("unowned address returned (2)")
	}

	checkAddrs("after unowned")

	// Check address loading.
	w.ReturnRefundContracts(contracts)

	w.ar.WriteRecycledAddrsToFile()
	b, _ := os.ReadFile(w.ar.recyclePath)
	var fileAddrs []string
	for _, addr := range strings.Split(string(b), "\n") {
		if addr == "" {
			continue
		}
		fileAddrs = append(fileAddrs, addr)
	}
	compareAddrLists("filecheck", []string{addr1.String(), addr2.String()}, fileAddrs)

	otherW, _ := newUnconnectedWallet(w.cloneParams, &WalletConfig{})
	if len(otherW.ar.addrs) != 2 {
		t.Fatalf("newly opened wallet didn't load recycled addrs")
	}

}

func TestFindBond(t *testing.T) {
	wallet, node, shutdown := tNewWallet(false, walletTypeRPC)
	defer shutdown()

	node.signFunc = func(tx *wire.MsgTx) {
		signFunc(tx, 0, wallet.segwit)
	}

	privBytes, _ := hex.DecodeString("b07209eec1a8fb6cfe5cb6ace36567406971a75c330db7101fb21bc679bc5330")
	bondKey, _ := btcec.PrivKeyFromBytes(privBytes)

	amt := uint64(500_000)
	acctID := [32]byte{}
	lockTime := time.Now().Add(time.Hour * 12)
	utxo := &ListUnspentResult{
		TxID:          tTxID,
		Address:       tP2PKHAddr,
		Amount:        1.0,
		Confirmations: 1,
		Spendable:     true,
		ScriptPubKey:  decodeString("76a914e114d5bb20cdbd75f3726f27c10423eb1332576288ac"),
	}
	node.listUnspent = []*ListUnspentResult{utxo}
	node.changeAddr = tP2PKHAddr

	bond, _, err := wallet.MakeBondTx(0, amt, 200, lockTime, bondKey, acctID[:])
	if err != nil {
		t.Fatal(err)
	}

	txRes := func(tx []byte) *GetTransactionResult {
		return &GetTransactionResult{
			BlockHash: hex.EncodeToString(randBytes(32)),
			Bytes:     tx,
		}
	}

	newBondTx := func() *wire.MsgTx {
		msgTx, err := msgTxFromBytes(bond.SignedTx)
		if err != nil {
			t.Fatal(err)
		}
		return msgTx
	}
	tooFewOutputs := newBondTx()
	tooFewOutputs.TxOut = tooFewOutputs.TxOut[2:]
	tooFewOutputsBytes, err := serializeMsgTx(tooFewOutputs)
	if err != nil {
		t.Fatal(err)
	}

	badBondScript := newBondTx()
	badBondScript.TxOut[1].PkScript = badBondScript.TxOut[1].PkScript[1:]
	badBondScriptBytes, err := serializeMsgTx(badBondScript)
	if err != nil {
		t.Fatal(err)
	}

	noBondMatch := newBondTx()
	noBondMatch.TxOut[0].PkScript = noBondMatch.TxOut[0].PkScript[1:]
	noBondMatchBytes, err := serializeMsgTx(noBondMatch)
	if err != nil {
		t.Fatal(err)
	}

	node.addRawTx(1, newBondTx())
	verboseBlocks := node.verboseBlocks
	for _, blk := range verboseBlocks {
		blk.msgBlock.Header.Timestamp = time.Now()
	}

	tests := []struct {
		name              string
		coinID            []byte
		txRes             *GetTransactionResult
		bestBlockErr      error
		getTransactionErr error
		verboseBlocks     map[chainhash.Hash]*msgBlockWithHeight
		searchUntil       time.Time
		wantErr           bool
	}{{
		name:   "ok",
		coinID: bond.CoinID,
		txRes:  txRes(bond.SignedTx),
	}, {
		name:              "ok with find blocks",
		coinID:            bond.CoinID,
		getTransactionErr: asset.CoinNotFoundError,
	}, {
		name:    "bad coin id",
		coinID:  make([]byte, 0),
		txRes:   txRes(bond.SignedTx),
		wantErr: true,
	}, {
		name:    "missing an output",
		coinID:  bond.CoinID,
		txRes:   txRes(tooFewOutputsBytes),
		wantErr: true,
	}, {
		name:    "bad bond commitment script",
		coinID:  bond.CoinID,
		txRes:   txRes(badBondScriptBytes),
		wantErr: true,
	}, {
		name:    "bond script does not match commitment",
		coinID:  bond.CoinID,
		txRes:   txRes(noBondMatchBytes),
		wantErr: true,
	}, {
		name:    "bad msgtx",
		coinID:  bond.CoinID,
		txRes:   txRes(bond.SignedTx[1:]),
		wantErr: true,
	}, {
		name:              "get best block error",
		coinID:            bond.CoinID,
		getTransactionErr: asset.CoinNotFoundError,
		bestBlockErr:      errors.New("some error"),
		wantErr:           true,
	}, {
		name:              "block not found",
		coinID:            bond.CoinID,
		getTransactionErr: asset.CoinNotFoundError,
		verboseBlocks:     map[chainhash.Hash]*msgBlockWithHeight{},
		wantErr:           true,
	}, {
		name:              "did not find by search until time",
		coinID:            bond.CoinID,
		getTransactionErr: asset.CoinNotFoundError,
		searchUntil:       time.Now().Add(time.Hour),
		wantErr:           true,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			node.getTransactionMap["any"] = test.txRes
			node.getBestBlockHashErr = test.bestBlockErr
			node.verboseBlocks = verboseBlocks
			if test.verboseBlocks != nil {
				node.verboseBlocks = test.verboseBlocks
			}
			bd, err := wallet.FindBond(tCtx, test.coinID, test.searchUntil)
			if test.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !bd.CheckPrivKey(bondKey) {
				t.Fatal("pkh not equal")
			}
		})
	}
}

func TestIDUnknownTx(t *testing.T) {
	type idResult struct {
		txType    asset.TransactionType
		recipient *string
		bondInfo  *asset.BondTxInfo
		amt       uint64
		fee       uint64
	}

	type test struct {
		name           string
		ltr            *ListTransactionsResult
		txB            dex.Bytes
		segwit         bool
		chainParams    *chaincfg.Params
		ownedAddresses map[string]bool
		ownsAddress    bool
		exp            *idResult
	}

	h2b := func(h string) []byte {
		b, _ := hex.DecodeString(h)
		return b
	}

	floatPtr := func(f float64) *float64 {
		return &f
	}
	stringPtr := func(s string) *string {
		return &s
	}

	tests := []*test{
		{
			name: "btc swap",
			ltr: &ListTransactionsResult{
				Send: true,
			},
			txB: h2b("01000000000101d291fc06a2f14c984bd2be00d4b319ba3ffe8131dbdaf8e8089df76e18d7f6400100000000ffffffff0200e1f505000000002200205c1989ae196beb93b57d26f714382d8c8b149d548d64804b9b6629da01133da4bcb62542020000001600148bb93295660c3530110a4eb1f8bd4a6c54fcbe050247304402205aa4c40e6bb5f79f5d0520f4e2928a989b48721785cb128805a8dafa88b8a6e5022018a9dd5478144c95aad7b2210b1d768ed81fda750d59af03435fe167aba53f070121037c226414b8613aa39e611958397ee727a622b4619a3837f28abc14eee7e4162c00000000"),
			exp: &idResult{
				txType: asset.SwapOrSend,
				amt:    toSatoshi(1),
			},
			segwit:      true,
			chainParams: &chaincfg.RegressionNetParams,
		},
		{
			name: "btc redeem",
			ltr: &ListTransactionsResult{
				Send: false,
				Fee:  floatPtr(0.0000143),
			},
			txB: h2b("010000000001016c5dfe4624d80beb2d6da0e7c58fd01782ecc602bc8ad233207b5febf69336f90000000000ffffffff016adbf50500000000160014b0cd6818ef5f504b1344176d6c835ea3828106630547304402207576c0ac7d9e1a12e641820b6009c092d216193f184c9acf4382b383543e2bf802204d2852725db5e88ad7db5a87f57f2466452f543ea520dcdd6f1d8868c3cdd8cb0121021c88273c992be50158b8fc6ac64c5530777de952a973a863452fb9006f10d7a620d5f148dcc90348c609846e62f36825cc7d8473f8fcdb769aa1938599d90978140101616382012088a8201736b4f1e1ede09dfb3629ade052736502eee684fe52318a9d3458d57753d79a8876a914ffb2c4df684587890f8c5ef961b5d939b8f5cce267048de80566b17576a91462ba66322a59ee8727e7220a8095f0f4904323816888ac00000000"),
			exp: &idResult{
				txType: asset.Redeem,
				amt:    toSatoshi(1),
				fee:    toSatoshi(0.0000143),
			},
			segwit:      true,
			chainParams: &chaincfg.RegressionNetParams,
		},
		{
			name: "btc create bond",
			ltr: &ListTransactionsResult{
				Send: false,
				Fee:  floatPtr(0.0000222),
			},
			txB: h2b("010000000001010d43ae30b1e90406cf0f5a9397ca76ace439027770bfc83ad7e01896320267fc0000000000ffffffff03a0860100000000002200201e3f29940add3ed71f384a5d3a026d8b2dcca86f2ecbff9e0a5d6072abe0445200000000000000003c6a3a0000a0836b39b5ceb84f422b8a8cd5940117087a8522457c6d81d200557652fbe6ea660583a20e39bbb09592fd00b7d770cc832ddf4d625ae3a0b4540a5402000000160014f0a2c09f74852593add56e3efb0bcbb2c1b0d6e402473044022003ac9c93e13e8af518d4623d4d675680adc4cb72b22ddab03d3233e036d391040220227ae36d036da035a5703d3a16726e20af878f8c05977b0e01b26a8bb39ac100012102767e264e6c356e17398dd35096df1782492443bbbb4186875617a937ad7c8ed900000000"),
			exp: &idResult{
				txType: asset.CreateBond,
				amt:    toSatoshi(0.001),
				fee:    toSatoshi(0.0000222),
				bondInfo: &asset.BondTxInfo{
					AccountID: h2b("a0836b39b5ceb84f422b8a8cd5940117087a8522457c6d81d200557652fbe6ea"),
					BondID:    h2b("0e39bbb09592fd00b7d770cc832ddf4d625ae3a0"),
					LockTime:  1711637410,
				},
			},
			segwit:      true,
			chainParams: &chaincfg.RegressionNetParams,
		},
		{
			name: "btc redeem bond",
			ltr: &ListTransactionsResult{
				Send: false,
			},
			txB: h2b("01000000000101429fc9ff1f7aea896226b8111bfb418d81e0dc19bd844c1de8d71f1a303c47300000000000feffffff0188580100000000001600142e49fdaa8079b5f4903877dd221ee333f46531520347304402204af5004a2aa0865c3c8d77516f57a670d93b63fc518730c8eba318905be88bf90220614007e3fa0811617aaaf11bf5de35be977fcc25f4561840e3282c184d5fb8c8012102e0f6490ea74c70e71ea6f033489b1dc6c44d2755edd0a035294d4a44b1c47b092004a2830566b17576a9140e39bbb09592fd00b7d770cc832ddf4d625ae3a088aca2830566"),
			exp: &idResult{
				txType: asset.RedeemBond,
				amt:    toSatoshi(0.000882),
				bondInfo: &asset.BondTxInfo{
					AccountID: []byte{},
					BondID:    h2b("0e39bbb09592fd00b7d770cc832ddf4d625ae3a0"),
					LockTime:  1711637410,
				},
			},
			segwit:      true,
			chainParams: &chaincfg.RegressionNetParams,
		},
		{
			name: "btc acceleration",
			ltr: &ListTransactionsResult{
				Send: true,
			},
			txB: h2b("01000000000101036633cbdd682bb3aa7b2e77f2012cdd0df3817c687e66a7115ea6507c97799e0100000000ffffffff0100ed074e020000001600141ed9add001c74e3cb0ecb5cb1ef07065773ab40902483045022100ea1f1137dd47bed6632546b8555c67b1942e899045ca14723c1c9e2badde1470022022579110c9ad68b357aff10ff9d852caec16e3f60f680f3be1532bd56c06113b012103597df71b639033fab233ac40b896357a83ad244b8768a14180023c1192fb2fde00000000"),
			exp: &idResult{
				txType: asset.Acceleration,
			},
			ownedAddresses: map[string]bool{
				"bcrt1qrmv6m5qpca8rev8vkh93aursv4mn4dqfj8xhlr": true,
			},
			segwit:      true,
			chainParams: &chaincfg.RegressionNetParams,
		},
		{
			name: "btc receive",
			ltr: &ListTransactionsResult{
				Send: false,
			},
			txB: h2b("0200000000010843b998de1250c8e6a7ff646a5b36eb76fe79c79f972097fa482b8d4c7f06be930000000000feffffffe40884187079afdfb721ed4b58d9c629e948fff4bdef255d10da81c1630962350000000000feffffff19806278fd9c3752a5f4aed4173a4d5df26df7015d0d702d46a444f471fd52540000000000feffffff2e94494d4d43de61d924ba37d95ac3b8b818c6a5f7a14e2f04741b667c5cb63c0000000000feffffff6d78e080d529e2a8989d21860b78a6c790b0b233a1fff30d8a032f0f9518521d0000000000feffffff95c9c073f28a4353b0b9f8810c96fd83589e5da6bf45bf6a2343fdc5dffe08a20300000000feffffff4a1a43da251e6a9ffef5bf7b364bfd2a4a360e99e4b8eae5913a4613a562c9f50000000000feffffff6cf2c255cfbce2b93d0e6d5851a40405e7e459d88ed704ce15ba8cfd40cdf6e90000000000feffffff0200e40b54020000001600149cf5e30367e2ad3e0408013e41013166e42af97c4f7a75e800000000160014b1c6c90e26f833cceb9c4ee5d29a1a96d5fa5cfb02473044022074c1ffd5a61b396586752ee301a8e9e413587cd0fa4418c97d62dc4c1c177ef402207eae4f750febb7b9399e8b6adb13d18400eeca5a0eb2d1302b198d8713f8c4690121030958f6fb97080b0e3fed9320316e49d3bdb113345e82caf8e5525d095c70cbb1024730440220231f320b17c1b82a72e053b8e248f6efa10559a238f017f67dcae3a34e0896460220154177afea441ae2189f7a6ce0b545240f7b042502856bf68b1a05d1b2a263680121030958f6fb97080b0e3fed9320316e49d3bdb113345e82caf8e5525d095c70cbb102473044022006aa13d3ada09fc630a49e3324d6b27aa7ff942643e677755589788935d5ffb302204badb059d623672dba5a79b022f9b4c134030fd69cc2c19e6725c0850d3c1c950121030958f6fb97080b0e3fed9320316e49d3bdb113345e82caf8e5525d095c70cbb102473044022063338039df79756c6d439cd17629bb0e385b0e55d76fc66f412d9240691d77bc0220691198f9888048ae62d993ec7676e38b31c11a9e27b9f7f5824249fcc28c98720121030958f6fb97080b0e3fed9320316e49d3bdb113345e82caf8e5525d095c70cbb1024730440220456d5b7a9d0195e0e6540e6fc989c6091ec84cb87c0b1b9fdfdd02d86a3f7b7b02201156bc90675c6f244b48b1f291aab24475b76f250f21350cb06a51b76beb67a20121030958f6fb97080b0e3fed9320316e49d3bdb113345e82caf8e5525d095c70cbb10247304402204095b1e62f69e14d3fb6dcebfba30f42c9a82b50e174051653f5291b3c742a08022010e219784fecfc9f43558442052b51beb7bcdf1c5ca19e4db19e487601bba0d90121034fbe054ccf4fc05c9a750d361c10b20ec19fc4e45c31a008f9686f2d0943fbc30247304402204c27e00912623630ea56979523454f608966da367a5eacee4911a7fa79f8d16902206248c8532a430e0ed1de7a85faab68060e88d8127611517fb08bb7b0362129af0121030958f6fb97080b0e3fed9320316e49d3bdb113345e82caf8e5525d095c70cbb10247304402203f9539bc251546e6d50719f1b0fbc20f24834c3ba7791eab317e3d2e96b0b0510220479104cab8fe9bee68090685d7fa431374cf8572902745052df4a8411e86fa940121030958f6fb97080b0e3fed9320316e49d3bdb113345e82caf8e5525d095c70cbb1ca030000"),
			exp: &idResult{
				txType:    asset.Receive,
				amt:       toSatoshi(100),
				recipient: stringPtr("bcrt1qnn67xqm8u2knupqgqylyzqf3vmjz47tumdumvy"),
			},
			ownedAddresses: map[string]bool{
				"bcrt1qnn67xqm8u2knupqgqylyzqf3vmjz47tumdumvy": true,
			},
			segwit:      true,
			chainParams: &chaincfg.RegressionNetParams,
		},
		{
			name: "ltc swap",
			ltr: &ListTransactionsResult{
				Send: true,
			},
			txB: h2b("010000000001012f554d2dad719503eb7372e924f90ed1bb563745b0902d84f9898eb92ed7d1fe0100000000ffffffff02404b4c00000000002200209d1cd2b4f19f552208f0e09211f3ef693c9e2ffdb49e3245c0f235ebc2434c6a0a5b7d4d02000000160014cb7c5e4e69f18ab9353cd1b46010ff445786a8a20247304402203d8a546d37dece699dfe88dff011eaac54a85c8a46493a391c84022b7278c7fa02206fa9589415d354d4fab4342bf96dc766aaec03e14f3c0c4b28a099c87eb7175801210399d9d2b2c6765ae5ae47d2deeaca334ac93ee0588dc6190d9d9a5532ea66f10b00000000"),
			exp: &idResult{
				txType: asset.SwapOrSend,
				amt:    toSatoshi(0.05),
			},
			segwit:      true,
			chainParams: dexltc.RegressionNetParams,
		},
		{
			name: "ltc redeem",
			ltr: &ListTransactionsResult{
				Send: false,
				Fee:  floatPtr(0.0000143),
			},
			txB: h2b("01000000000101e56911e8f37ee44a8f5ab8bfd56d4738567b8d476e095212cfa4b99b53df93aa0000000000ffffffff01aa454c0000000000160014aa7c6c357ecef1da748cc8f49689e8c3a49b5ebb05483045022100d2d219c539f9ad88641fcab3d43103f90f1d83a702933a264c53fa111822f17502205d0428a917df83a6a9f7e53d5bc8f24a19955fd50f5e172713ffe84308bd252b01210355aee7e8af0613d27dfac540b0c86ec2c2ad01c1143ed209d0dc283ed91507da20bcb62416ef59372a5cd77e78698e57b794874dff9da3cc14ebd39874b643a6ad0101616382012088a820fe4f2da55f79f0acb8dca2d60ad1b37851c0daaefa8c30034c56f665b8704f4e8876a914fe8f599067f8e9ec878eb0fff667659601bc85c7670431ea0566b17576a914be1459bcc4fc021ebee36e2217a5a455b66415866888ac00000000"),
			exp: &idResult{
				txType: asset.Redeem,
				amt:    toSatoshi(0.05),
				fee:    toSatoshi(0.0000143),
			},
			segwit:      true,
			chainParams: dexltc.RegressionNetParams,
		},
		{
			name: "ltc receive",
			ltr: &ListTransactionsResult{
				Send: false,
			},
			txB: h2b("020000000001035bcb611338cc9b1534641eca1f6f176694e7f65798e915ebf89127f765f7dfbb000000001716001427ba894b4ac84cf236608590efe12ee514692c32feffffffb437473f057ce680b99a5e7644a29db92be4d53132d5433191ed58a554a1c251000000001716001427ba894b4ac84cf236608590efe12ee514692c32feffffff442c3085152ca4e695cbf4d142d02b8f5477264c02ae778a543e7006c12e8dcd0300000000feffffff0200e40b5402000000160014df9053f66260950c3f086ed7cbf636710120937128e5f90200000000160014f538a4ce11498ae288108e8aa8476da4d556e1c90247304402203767218411d8007721ee5937207376f9896239cb0509f3e90a33937ac4f9bc7f02202240562b068cc0c8453abe7a0e32317f912b8436d2a872e1686411ce8cf60e7c0121030958f6fb97080b0e3fed9320316e49d3bdb113345e82caf8e5525d095c70cbb10247304402207f70b52b41455b82ab9d7aaefab6e29bb7dc9e76b3469c779ebe24ac5472f7c302207d3634ac4d340a03f8a476fba772ca9e98308f568eff17b958ce0ab8343de1670121030958f6fb97080b0e3fed9320316e49d3bdb113345e82caf8e5525d095c70cbb1024730440220644c3c39db84eece837f1e559ce656e2aed9243cd837799b3588bb7d95fc7712022026411d73e130e1c9c58fa15f912b9edb8dedb73424b74043fdf31080a793baba012102e5e3124d3475365355315118ad7a093803fce7d30c10d120c84c144008125fbace030000"),
			exp: &idResult{
				txType:    asset.Receive,
				amt:       toSatoshi(100),
				recipient: stringPtr("rltc1qm7g98anzvz2sc0cgdmtuha3kwyqjpym3nmlx26"),
			},
			ownedAddresses: map[string]bool{
				"rltc1qm7g98anzvz2sc0cgdmtuha3kwyqjpym3nmlx26": true,
			},
			segwit:      true,
			chainParams: dexltc.RegressionNetParams,
		},
		{
			name: "ltc receive 2", // This was a tx populating the wallet. It sends to p2sh.
			ltr: &ListTransactionsResult{
				Send: false,
			},
			txB: h2b("020000000001092e9c9b17bab0ef3e48a7036ad392d4d502ba7b3c4e892c40387720249e21c5190000000000feffffffde74fe76d8c70e0e7c7adf77247204067a67109b383e1477bec2c1a9faf9188d0300000000feffffffa62288711adce247f5dd65a03eda5ab7fe3d1c83a66a28672da4ee18bbe940e40000000000feffffffa6b454ff4e6256bfe362b53af26a084c449e3c37a4f1e8180ebe19d0e8269f300100000000feffffffb7920d7c20d2aec8967e6c7efe79d3373d23332f479acda1c914155a1a4ace7e0000000000feffffffc59832cae1ba20b0c3367baba2a98f330b6db8f93c37d9a510e068cd19b4026c000000001716001427ba894b4ac84cf236608590efe12ee514692c32feffffff080a596a4a4441e6a5a5aaa01c6875ef15d3653d88c101c2ff181d595d5e40670000000000feffffff78133092e9026f2df4b9f11d047335c38badc5455c5d5e39060329ba137533470200000000feffffff0cec7b521195a1adca741b1e60c9452dd924bf9d8a237de7a7c66eb7f7e13b1f0200000000feffffff04325ff5050000000016001443ad3a62f0d2b0bd04fc71e98ca3cbec94843de6008c86470000000017a914824af6b755a13eafb06c956bc6edc86f80ebe6b887008c86470000000017a914b096d49c0da63108c20b4ea78c6873466f018ad387008c86470000000017a914ce06fa7565bb215a0ba835e65418bd89dffe1ae4870247304402201a18559c1ac39621813709a5196a559b4639a5c0d81dc5903d763b36fec5123902205c3ca583401def87b3064d0282099a43b89c8e1366f1d1f4f7cf5e24279e14390121023f68d5d4fa933e84618b2919dee0dd30da447efe4f19d9e1bdc83a81df4c20950247304402205739478409f9c8d42816186ab3815bad6e7f7c49b6adee7ff3b550ac414a416e022054376b2457b522a3e61e712569f835c6572f0dc3cc33221674253b749f58f4ad0121034fbe054ccf4fc05c9a750d361c10b20ec19fc4e45c31a008f9686f2d0943fbc30247304402207b34b8466bdd69be9d03a2c2424f65b01cf68aba271525c0dd8620b9556d55f702200c393cb382ce09c900e590744fde34dae992adb892b02539c6b560440d5bfa160121031acec82644e5c18087a88d889560cc405f0f03f6f4beefa930ca4b653479764702473044022071049a5d5048c4ea6687fe7f433c3ab716c87363bab95fd1ef876c9e69a1d6cd022050495c796935dc4a544616d0c02120b2cb433356dfc031cb9f0298d0e0eb6ecf0121021d863c102e737d4db886416b5cec11bdd7784a873d6343388eaea9bf3f96243a024730440220793449fb3554fc42179bd7ad546d76ca981fbc432cdf7095ec73d73368ce1209022010c6a4689feb7874ac0c6217dd03588ab744b000bf2661305db06df3d2163691012103cd5297318bfe3120b94f0b5a6825ae39c65d8d4cf09728a8295c51b4e621c22d0247304402201f6ebf68c10b26afe953ed92dfc17286a52a4c5c4beeec0779b5ce9b6e51fdee02201a4e32cde8f345ffcef2ea946f8dfdfb7fadeadb7732e883e92416740b9f96ba0121030958f6fb97080b0e3fed9320316e49d3bdb113345e82caf8e5525d095c70cbb102473044022007d6b0dfe8144f5a01766bdb174e6f45c66ae0b6a9402457a1020274a94d9e6c0220023b6f9d31d044c9cf7599c913ce54781f28e20c211a8c774e7d8e44478857b6012103c69cfb1b8207574db8ac68db88b48ab73a86d96c9fedbc387859065e1726b5060247304402200ec806c73d609d41bea96bbca7f868edee54c3cf5b7f986eef1ad0d70e95f72b02202a5659c3334c3a3c17f36edda8de957ec67f3709c9f3f9d047a3436a482c317401210353394a72a4e7cee4846309843a35a535c1cea0efd2cd6d5cb4f199df7c4eadde0247304402200cf39c7dd3af1e7babc886eace4884524a3b43b82dba3e408cf0a0e4c609d4f402205baa81e83b3d30d6e37d5d20a30562fe5f70bb4239d6430596ad8f8b3785cd2c012103432cd635aac3419ec3b0fe830b96cf715744da770a5bd1e7e73feb8e0f2bfb9ec8010000"),
			exp: &idResult{
				txType:    asset.Receive,
				amt:       toSatoshi(12),
				recipient: stringPtr("QfPMfgRPHd6mvQKDkJN8uEnR2kmBYAUE1x"),
			},
			ownedAddresses: map[string]bool{
				"QfPMfgRPHd6mvQKDkJN8uEnR2kmBYAUE1x": true,
			},
			segwit:      true,
			chainParams: dexltc.RegressionNetParams,
		},
		{
			name: "ltc send",
			ltr: &ListTransactionsResult{
				Send: true,
				Fee:  floatPtr(0.0000141),
			},
			txB: h2b("0100000000010156ba41217c0114309279f0ab5b2b7992dbc9d99aad930978d4cbf82b07112d940100000000ffffffff0200e1f505000000001600148a94b43c8ab88812884b1f9aa5b9b8fdc0839fb3c8a0c94d02000000160014431ba070198d15ab57acdc890cf5bb163268e9b6024730440220206036192f90c5d52a6c924d614b8d2bda24a82b9e68161f85367846c0d8f33002200650623b7de5d5343f0f8630ab5e8d5b928d3f013c8300564a609c7cdb3b76b20121025c0a9fd0aaeb795b4efe6da04606564f233b850911c47a3b8d37e2986e95f2ed00000000"),
			exp: &idResult{
				txType:    asset.Send,
				amt:       toSatoshi(1),
				recipient: stringPtr("rltc1q322tg0y2hzyp9zztr7d2twdclhqg88anj0u8ea"),
				fee:       toSatoshi(0.0000141),
			},
			ownedAddresses: map[string]bool{
				"rltc1qgvd6quqe3526k4avmjyseadmzcex36dkywtqat": true, // change addr
			},
			segwit:      true,
			chainParams: dexltc.RegressionNetParams,
		},
		{
			name: "ltc split",
			ltr: &ListTransactionsResult{
				Send: true,
			},
			txB: h2b("01000000000101e79382797b0b59c227d4005588378c7a6b4d936892d03814d57c497592d226a30000000000ffffffff0234574c000000000016001483eb3eb25365fda46e66a54e00b1a8183eea23ff4a87bf5302000000160014a69e891e243b00fd8dde000afe9bb36ad39767e3024730440220419540531f49a7767cee3be0c31748937ec762d281987b227bc17b3263d4208b02206e0bd4058458a83900e7494d931ca71ffbf13923d292b62fc0ae19bbf65b6e970121033d77445341a7a972edfd9d902a036b9f7143013869453d41b04117882a7d261800000000"),
			exp: &idResult{
				txType: asset.Split,
			},
			ownsAddress: true,
			segwit:      true,
			chainParams: dexltc.RegressionNetParams,
		},
	}

	runTest := func(tt *test) {
		t.Run(tt.name, func(t *testing.T) {
			wallet, node, shutdown := tNewWallet(true, walletTypeRPC)
			wallet.chainParams = tt.chainParams
			defer shutdown()
			msgTx, err := msgTxFromBytes(tt.txB)
			if err != nil {
				panic(err)
			}
			txID := msgTx.TxHash().String()
			tt.ltr.TxID = txID
			node.getTransactionMap[txID] = &GetTransactionResult{Bytes: tt.txB}
			node.ownedAddresses = tt.ownedAddresses
			node.ownsAddress = tt.ownsAddress
			txType, recipient, bondInfo, amt, fee, err := wallet.idUnknownTx(tt.ltr)
			if err != nil {
				t.Fatalf("%s: unexpected error: %v", tt.name, err)
			}
			if txType != tt.exp.txType {
				t.Fatalf("%s: wrong tx type. wanted %v, got %v", tt.name, tt.exp.txType, txType)
			}
			if tt.exp.recipient == nil != (recipient == nil) {
				t.Fatalf("%s: wrong recipient. wanted %v, got %v", tt.name, tt.exp.recipient, recipient)
			}
			if tt.exp.recipient != nil && *tt.exp.recipient != *recipient {
				t.Fatalf("%s: wrong recipient. wanted %v, got %v", tt.name, *tt.exp.recipient, *recipient)
			}
			if tt.exp.bondInfo == nil != (bondInfo == nil) {
				t.Fatalf("%s: wrong bond info. wanted %v, got %v", tt.name, tt.exp.bondInfo, bondInfo)
			}
			if !reflect.DeepEqual(tt.exp.bondInfo, bondInfo) {
				t.Fatalf("%s: wrong bond info. wanted %v, got %v", tt.name, *tt.exp.bondInfo, *bondInfo)
			}
			if amt != tt.exp.amt {
				t.Fatalf("%s: wrong amount. wanted %d, got %d", tt.name, tt.exp.amt, amt)
			}
			if fee != tt.exp.fee {
				t.Fatalf("%s: wrong fee. wanted %d, got %d", tt.name, tt.exp.fee, fee)
			}
		})
	}

	for _, tt := range tests {
		runTest(tt)
	}
}
