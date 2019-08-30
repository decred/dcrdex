// +build !btclive
//
// These tests will not be run if the btclive build tag is set. In that case,
// the live_test.go tests will run.

package btc

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcec"
	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
	"github.com/btcsuite/btcutil"
	"github.com/decred/dcrdex/server/asset"
	"github.com/decred/slog"
	flags "github.com/jessevdk/go-flags"
)

var (
	testParams     = &chaincfg.MainNetParams
	mainnetPort    = 8332
	blockPollDelay time.Duration
)

func TestMain(m *testing.M) {
	// Set any created BTCBackends to poll for blocks every 50 ms to
	// accommodate reorg testing.
	blockPollInterval = time.Millisecond * 50
	blockPollDelay = blockPollInterval + time.Millisecond*5
	os.Exit(m.Run())
}

// TestConfig tests the LoadConfig function.
func TestConfig(t *testing.T) {
	cfg := &BTCConfig{}
	parsedCfg := &BTCConfig{}

	tempDir, err := ioutil.TempDir("", "btctest")
	if err != nil {
		t.Fatalf("error creating temporary directory: %v", err)
	}
	defer os.RemoveAll(tempDir)
	filePath := filepath.Join(tempDir, "test.conf")
	rootParser := flags.NewParser(cfg, flags.None)
	iniParser := flags.NewIniParser(rootParser)

	runCfg := func(config *BTCConfig) error {
		*cfg = *config
		err := iniParser.WriteFile(filePath, flags.IniNone)
		if err != nil {
			return err
		}
		parsedCfg, err = LoadConfig(filePath, asset.Mainnet, btcPorts)
		return err
	}

	// Check that there is an error from an unpopulated configuration.
	err = runCfg(cfg)
	if err == nil {
		t.Fatalf("no error for empty config")
	}

	// Try with just the name. Error expected.
	err = runCfg(&BTCConfig{
		RPCUser: "somename",
	})
	if err == nil {
		t.Fatalf("no error when just name provided")
	}

	// Try with just the password. Error expected.
	err = runCfg(&BTCConfig{
		RPCPass: "somepass",
	})
	if err == nil {
		t.Fatalf("no error when just password provided")
	}

	// Give both name and password. This should not be an error.
	err = runCfg(&BTCConfig{
		RPCUser: "somename",
		RPCPass: "somepass",
	})
	if err != nil {
		t.Fatalf("unexpected error when both name and password provided: %v", err)
	}
	h, p, err := net.SplitHostPort(parsedCfg.RPCBind)
	if err != nil {
		t.Fatalf("error splitting host and port: %v", err)
	}
	if h != defaultHost {
		t.Fatalf("unexpected default host. wanted %s, got %s", defaultHost, h)
	}
	if p != strconv.Itoa(mainnetPort) {
		t.Fatalf("unexpected default port. wanted %d, got %s", mainnetPort, p)
	}
	// sanity check for name and password match
	if parsedCfg.RPCUser != cfg.RPCUser {
		t.Fatalf("name mismatch")
	}
	if parsedCfg.RPCPass != cfg.RPCPass {
		t.Fatalf("password mismatch")
	}

	// Check with a designated port, but no host specified.
	err = runCfg(&BTCConfig{
		RPCUser: "somename",
		RPCPass: "somepass",
		RPCPort: 1234,
	})
	if err != nil {
		t.Fatalf("unexpected error when setting port: %v", err)
	}
	// See that RPCBind returns localhost.
	h, p, err = net.SplitHostPort(parsedCfg.RPCBind)
	if err != nil {
		t.Fatalf("error splitting host and port when setting port: %v", err)
	}
	if h != defaultHost {
		t.Fatalf("unexpected host when setting port. wanted %s, got %s", defaultHost, h)
	}
	if p != "1234" {
		t.Fatalf("unexpected custom port. wanted 1234, got %s", p)
	}

	// Check with rpcbind set (without designated port) and custom rpcport.
	err = runCfg(&BTCConfig{
		RPCUser: "somename",
		RPCPass: "somepass",
		RPCBind: "127.0.0.2",
		RPCPort: 1234,
	})
	if err != nil {
		t.Fatalf("unexpected error when setting portless rpcbind: %v", err)
	}
	h, p, err = net.SplitHostPort(parsedCfg.RPCBind)
	if err != nil {
		t.Fatalf("error splitting host and port when setting portless rpcbind: %v", err)
	}
	if h != "127.0.0.2" {
		t.Fatalf("unexpected host when setting portless rpcbind. wanted 127.0.0.2, got %s", h)
	}
	if p != "1234" {
		t.Fatalf("unexpected custom port when setting portless rpcbind. wanted 1234, got %s", p)
	}

	// Check with a port set with both rpcbind and rpcport. The rpcbind port
	// should take precedence.
	err = runCfg(&BTCConfig{
		RPCUser: "somename",
		RPCPass: "somepass",
		RPCBind: "127.0.0.2:1234",
		RPCPort: 1235,
	})
	if err != nil {
		t.Fatalf("unexpected error when setting port twice: %v", err)
	}
	h, p, err = net.SplitHostPort(parsedCfg.RPCBind)
	if err != nil {
		t.Fatalf("error splitting host and port when setting port twice: %v", err)
	}
	if h != "127.0.0.2" {
		t.Fatalf("unexpected host when setting port twice. wanted 127.0.0.2, got %s", h)
	}
	if p != "1234" {
		t.Fatalf("unexpected custom port when setting port twice. wanted 1234, got %s", p)
	}

	// Check with just a port for rpcbind and make sure it gets parsed.
	err = runCfg(&BTCConfig{
		RPCUser: "somename",
		RPCPass: "somepass",
		RPCBind: ":1234",
	})
	if err != nil {
		t.Fatalf("unexpected error when setting port-only rpcbind: %v", err)
	}
	h, p, err = net.SplitHostPort(parsedCfg.RPCBind)
	if err != nil {
		t.Fatalf("error splitting host and port when setting port-only rpcbind: %v", err)
	}
	if h != defaultHost {
		t.Fatalf("unexpected host when setting port-only rpcbind. wanted %s, got %s", defaultHost, h)
	}
	if p != "1234" {
		t.Fatalf("unexpected custom port when setting port-only rpcbind. wanted 1234, got %s", p)
	}

	// IPv6
	err = runCfg(&BTCConfig{
		RPCUser: "somename",
		RPCPass: "somepass",
		RPCBind: "[24c2:2865:4c7e:fd9b:76ea:4aa0:263d:6377]:1234",
	})
	if err != nil {
		t.Fatalf("unexpected error when trying IPv6: %v", err)
	}
	h, p, err = net.SplitHostPort(parsedCfg.RPCBind)
	if err != nil {
		t.Fatalf("error splitting host and port when trying IPv6: %v", err)
	}
	if h != "24c2:2865:4c7e:fd9b:76ea:4aa0:263d:6377" {
		t.Fatalf("unexpected host when trying IPv6. wanted %s, got %s", "24c2:2865:4c7e:fd9b:76ea:4aa0:263d:6377", h)
	}
	if p != "1234" {
		t.Fatalf("unexpected custom port when trying IPv6. wanted 1234, got %s", p)
	}
}

// The remaining tests use the testBlockchain which feeds a testNode stub for
// rpcclient.Client. UTXOs, transactions and blocks are added to the blockchain
// as jsonrpc types to be requested by the BTCBackend.
//
// General formula for testing
// 1. Create a BTCBackend with the node field set to a testNode
// 2. Create a fake UTXO and all of the associated jsonrpc-type blocks and
//    transactions and add the to the test blockchain.
// 3. Verify the BTCBackend and UTXO methods are returning whatever is expected.
// 4. Optionally add more blocks and/or transactions to the blockchain and check
//    return values again, as things near the top of the chain can change.

func randomBytes(len int) []byte {
	bytes := make([]byte, len)
	rand.Read(bytes)
	return bytes
}

func randomHash() *chainhash.Hash {
	hash := new(chainhash.Hash)
	err := hash.SetBytes(randomBytes(32))
	if err != nil {
		fmt.Printf("chainhash.Hash.SetBytes error: %v\n", err)
	}
	return hash
}

// A fake "blockchain" to be used for RPC calls by the btcNode.
type testBlockChain struct {
	txOuts map[string]*btcjson.GetTxOutResult
	txRaws map[chainhash.Hash]*btcjson.TxRawResult
	blocks map[chainhash.Hash]*btcjson.GetBlockVerboseResult
	hashes map[int64]*chainhash.Hash
}

// The testChain is a "blockchain" to store RPC responses for the BTCBackend
// node stub to request.
var testChain testBlockChain
var testChainMtx sync.RWMutex

type testBlock struct {
	hash   chainhash.Hash
	height uint32
}

var testBestBlock testBlock

// This must be called before using the testNode.
func cleanTestChain() {
	testBestBlock = testBlock{
		hash:   zeroHash,
		height: 0,
	}
	testChain = testBlockChain{
		txOuts: make(map[string]*btcjson.GetTxOutResult),
		txRaws: make(map[chainhash.Hash]*btcjson.TxRawResult),
		blocks: make(map[chainhash.Hash]*btcjson.GetBlockVerboseResult),
		hashes: make(map[int64]*chainhash.Hash),
	}
}

// A stub to replace rpcclient.Client for offline testing.
type testNode struct{}

// Encode utxo info as a concatenated string hash:vout.
func txOutID(txHash *chainhash.Hash, index uint32) string {
	return txHash.String() + ":" + strconv.Itoa(int(index))
}

// Part of the btcNode interface.
func (t testNode) GetTxOut(txHash *chainhash.Hash, index uint32, _ bool) (*btcjson.GetTxOutResult, error) {
	testChainMtx.RLock()
	defer testChainMtx.RUnlock()
	outID := txOutID(txHash, index)
	out := testChain.txOuts[outID]
	// Unfound is not an error for GetTxOut.
	return out, nil
}

// Part of the btcNode interface.
func (t testNode) GetRawTransactionVerbose(txHash *chainhash.Hash) (*btcjson.TxRawResult, error) {
	testChainMtx.RLock()
	defer testChainMtx.RUnlock()
	tx, found := testChain.txRaws[*txHash]
	if !found {
		return nil, fmt.Errorf("test transaction not found\n")
	}
	return tx, nil
}

// Part of the btcNode interface.
func (t testNode) GetBlockVerbose(blockHash *chainhash.Hash) (*btcjson.GetBlockVerboseResult, error) {
	testChainMtx.RLock()
	defer testChainMtx.RUnlock()
	block, found := testChain.blocks[*blockHash]
	if !found {
		return nil, fmt.Errorf("test block not found")
	}
	return block, nil
}

// Part of the btcNode interface.
func (t testNode) GetBlockHash(blockHeight int64) (*chainhash.Hash, error) {
	testChainMtx.RLock()
	defer testChainMtx.RUnlock()
	hash, found := testChain.hashes[blockHeight]
	if !found {
		return nil, fmt.Errorf("test hash not found")
	}
	return hash, nil
}

// Part of the btcNode interface.
func (t testNode) GetBestBlockHash() (*chainhash.Hash, error) {
	testChainMtx.RLock()
	defer testChainMtx.RUnlock()
	return &testBestBlock.hash, nil
}

// Create a btcjson.GetTxOutResult such as is returned from GetTxOut.
func testGetTxOut(confirmations int64, pkScript []byte) *btcjson.GetTxOutResult {
	return &btcjson.GetTxOutResult{
		Confirmations: confirmations,
		ScriptPubKey: btcjson.ScriptPubKeyResult{
			Hex: hex.EncodeToString(pkScript),
		},
	}
}

// Create a *btcjson.TxRawResult such as is returned by
// GetRawTransactionVerbose.
func testRawTransactionVerbose(msgTx *wire.MsgTx, txid, blockHash *chainhash.Hash, blockHeight,
	confirmations int64) *btcjson.TxRawResult {

	var hash string
	if blockHash != nil {
		hash = blockHash.String()
	}
	w := bytes.NewBuffer(make([]byte, 0))
	err := msgTx.Serialize(w)
	if err != nil {
		fmt.Printf("error encoding MsgTx\n")
	}
	hexTx := w.Bytes()
	return &btcjson.TxRawResult{
		Hex:           hex.EncodeToString(hexTx),
		Txid:          txid.String(),
		BlockHash:     hash,
		Confirmations: uint64(confirmations),
	}
}

// Add a transaction output and it's getrawtransaction data.
func testAddTxOut(msgTx *wire.MsgTx, vout uint32, txHash, blockHash *chainhash.Hash, blockHeight, confirmations int64) *btcjson.GetTxOutResult {
	testChainMtx.Lock()
	defer testChainMtx.Unlock()
	txOut := testGetTxOut(confirmations, msgTx.TxOut[vout].PkScript)
	testChain.txOuts[txOutID(txHash, vout)] = txOut
	testAddTxVerbose(msgTx, txHash, blockHash, blockHeight, confirmations)
	return txOut
}

// Add a btcjson.TxRawResult to the blockchain.
func testAddTxVerbose(msgTx *wire.MsgTx, txHash, blockHash *chainhash.Hash, blockHeight, confirmations int64) *btcjson.TxRawResult {
	tx := testRawTransactionVerbose(msgTx, txHash, blockHash, blockHeight, confirmations)
	testChain.txRaws[*txHash] = tx
	return tx
}

func testDeleteTxOut(txHash *chainhash.Hash, vout uint32) {
	testChainMtx.Lock()
	defer testChainMtx.Unlock()
	txOutID(txHash, vout)
	delete(testChain.txOuts, txOutID(txHash, vout))
}

// Create a *btcjson.GetBlockVerboseResult such as is returned by
// GetBlockVerbose.
func testBlockVerbose(blockHash, prevHash *chainhash.Hash, confirmations, height int64) *btcjson.GetBlockVerboseResult {
	return &btcjson.GetBlockVerboseResult{
		Hash:          blockHash.String(),
		Confirmations: confirmations,
		Height:        height,
		PreviousHash:  prevHash.String(),
	}
}

// Add a GetBlockVerboseResult to the blockchain.
func testAddBlockVerbose(blockHash, prevHash *chainhash.Hash, confirmations int64, height uint32) *chainhash.Hash {
	testChainMtx.Lock()
	defer testChainMtx.Unlock()
	if blockHash == nil {
		blockHash = randomHash()
	}
	if height >= testBestBlock.height {
		testBestBlock = testBlock{
			hash:   *blockHash,
			height: height,
		}
	}
	if prevHash == nil {
		if height == testBestBlock.height+1 {
			prevHash = &testBestBlock.hash
		} else {
			prevHash = &zeroHash
		}
	}
	testChain.blocks[*blockHash] = testBlockVerbose(blockHash, prevHash, confirmations, int64(height))
	return blockHash
}

func testClearBestBlock() {
	testChainMtx.Lock()
	defer testChainMtx.Unlock()
	testBestBlock = testBlock{}
}

// An element of the TxRawResult vout array.
func testVout(value float64, pkScript []byte) btcjson.Vout {
	return btcjson.Vout{
		Value: value,
		ScriptPubKey: btcjson.ScriptPubKeyResult{
			Hex: hex.EncodeToString(pkScript),
		},
	}
}

// An element of the TxRawResult vin array.
func testVin(txHash *chainhash.Hash, vout uint32) btcjson.Vin {
	return btcjson.Vin{
		Txid: txHash.String(),
		Vout: vout,
	}
}

type testMsgTx struct {
	tx     *wire.MsgTx
	pubkey []byte
	pkHash []byte
	vout   uint32
}

// Generate a public key on the secp256k1 curve.
func genPubkey() ([]byte, []byte) {
	_, pub := btcec.PrivKeyFromBytes(btcec.S256(), randomBytes(32))
	pubkey := pub.SerializeCompressed()
	pkHash := btcutil.Hash160(pubkey)
	return pubkey, pkHash
}

// A pay-to-script-hash pubkey script.
func newP2PKHScript(segwit bool) ([]byte, []byte, []byte) {
	pubkey, pkHash := genPubkey()
	var pkScript []byte
	var err error
	if segwit {
		pkScript, err = txscript.NewScriptBuilder().
			AddOp(txscript.OP_0).
			AddData(pkHash).
			Script()
		if err != nil {
			fmt.Printf("newP2PKHScript error: %v\n", err)
		}
	} else {
		pkScript, err = txscript.NewScriptBuilder().
			AddOps([]byte{
				txscript.OP_DUP,
				txscript.OP_HASH160,
			}).
			AddData(pkHash).
			AddOps([]byte{
				txscript.OP_EQUALVERIFY,
				txscript.OP_CHECKSIG,
			}).Script()
		if err != nil {
			fmt.Printf("newP2PKHScript error: %v\n", err)
		}
	}

	return pkScript, pubkey, pkHash
}

// A MsgTx for a regular transaction with a single output. No inputs, so it's
// not really a valid transaction, but that's okay on testBlockchain.
func testMakeMsgTx(segwit bool) *testMsgTx {
	pkScript, pubkey, pkHash := newP2PKHScript(segwit)
	msgTx := wire.NewMsgTx(wire.TxVersion)
	msgTx.AddTxOut(wire.NewTxOut(1, pkScript))
	return &testMsgTx{
		tx:     msgTx,
		pubkey: pubkey,
		pkHash: pkHash,
	}
}

type testMsgTxSwap struct {
	tx        *wire.MsgTx
	vout      uint32
	contract  []byte
	recipient btcutil.Address
}

// Create a swap (initialization) contract with random pubkeys and return the
// pubkey script and addresses.
func testSwapContract() ([]byte, btcutil.Address) {
	lockTime := time.Now().Add(time.Hour * 24).Unix()
	secretKey := randomBytes(32)
	_, receiverPKH := genPubkey()
	_, senderPKH := genPubkey()
	contract, err := txscript.NewScriptBuilder().
		AddOps([]byte{
			txscript.OP_IF,
			txscript.OP_SIZE,
		}).AddInt64(32).
		AddOps([]byte{
			txscript.OP_EQUALVERIFY,
			txscript.OP_SHA256,
		}).AddData(secretKey).
		AddOps([]byte{
			txscript.OP_EQUALVERIFY,
			txscript.OP_DUP,
			txscript.OP_HASH160,
		}).AddData(receiverPKH).
		AddOp(txscript.OP_ELSE).
		AddInt64(lockTime).AddOps([]byte{
		txscript.OP_CHECKLOCKTIMEVERIFY,
		txscript.OP_DROP,
		txscript.OP_DUP,
		txscript.OP_HASH160,
	}).AddData(senderPKH).
		AddOps([]byte{
			txscript.OP_ENDIF,
			txscript.OP_EQUALVERIFY,
			txscript.OP_CHECKSIG,
		}).Script()
	if err != nil {
		fmt.Printf("testSwapContract error: %v\n", err)
	}
	receiverAddr, _ := btcutil.NewAddressPubKeyHash(receiverPKH, testParams)
	return contract, receiverAddr
}

func testMsgTxSwapInit() *testMsgTxSwap {
	msgTx := wire.NewMsgTx(wire.TxVersion)
	contract, recipient := testSwapContract()
	scriptHash := btcutil.Hash160(contract)
	pkScript, err := txscript.NewScriptBuilder().
		AddOp(txscript.OP_HASH160).
		AddData(scriptHash).
		AddOp(txscript.OP_EQUAL).
		Script()
	if err != nil {
		fmt.Printf("script building error in testMsgTxSwapInit: %v", err)
	}
	msgTx.AddTxOut(wire.NewTxOut(1, pkScript))
	return &testMsgTxSwap{
		tx:        msgTx,
		contract:  contract,
		recipient: recipient,
	}
}

type testMsgTxP2SH struct {
	tx       *wire.MsgTx
	pubkeys  [][]byte
	pkHashes [][]byte
	vout     uint32
	script   []byte
	n        int
	m        int
}

// An M-of-N mutli-sig script.
func testMultiSigScriptMofN(m, n int) ([]byte, [][]byte, [][]byte) {
	// serialized compressed pubkey used for multisig
	addrs := make([]*btcutil.AddressPubKey, 0, n)
	pubkeys := make([][]byte, 0, n)
	pkHashes := make([][]byte, 0, n)

	for i := 0; i < m; i++ {
		pubkey, pkHash := genPubkey()
		pubkeys = append(pubkeys, pubkey)
		pkHashes = append(pkHashes, pkHash)
		addr, err := btcutil.NewAddressPubKey(pubkey, testParams)
		if err != nil {
			fmt.Printf("error creating AddressSecpPubKey: %v\n", err)
			return nil, nil, nil
		}
		addrs = append(addrs, addr)
	}
	script, err := txscript.MultiSigScript(addrs, m)
	if err != nil {
		fmt.Printf("error creating MultiSigScript: %v\n", err)
		return nil, nil, nil
	}
	return script, pubkeys, pkHashes
}

// A pay-to-script-hash M-of-N multi-sig output and vout 0 of a MsgTx.
func testMsgTxP2SHMofN(m, n int, segwit bool) *testMsgTxP2SH {
	script, pubkeys, pkHashes := testMultiSigScriptMofN(m, n)
	var pkScript []byte
	var err error
	if segwit {
		scriptHash := sha256.Sum256(script)
		pkScript, err = txscript.NewScriptBuilder().
			AddOp(txscript.OP_0).
			AddData(scriptHash[:]).
			Script()
		if err != nil {
			fmt.Printf("error building script in testMsgTxP2SHMofN: %v", err)
		}
	} else {
		scriptHash := btcutil.Hash160(script)
		pkScript, err = txscript.NewScriptBuilder().
			AddOp(txscript.OP_HASH160).
			AddData(scriptHash).
			AddOp(txscript.OP_EQUAL).
			Script()
		if err != nil {
			fmt.Printf("error building script in testMsgTxP2SHMofN: %v", err)
		}
	}
	msgTx := wire.NewMsgTx(wire.TxVersion)
	msgTx.AddTxOut(wire.NewTxOut(1, pkScript))
	return &testMsgTxP2SH{
		tx:       msgTx,
		pubkeys:  pubkeys,
		pkHashes: pkHashes,
		script:   script,
		vout:     0,
		n:        n,
		m:        m,
	}
}

// Make a backend that logs to stdout.
func testBackend() (*BTCBackend, func()) {
	logger := slog.NewBackend(os.Stdout).Logger("TEST")
	ctx, shutdown := context.WithCancel(context.Background())
	btc := newBTC(ctx, testParams, logger, testNode{})
	return btc, shutdown
}

// TestUTXOs tests UTXO-related paths.
func TestUTXOs(t *testing.T) {
	// The various UTXO types to check:
	// 1. A valid UTXO in a mempool transaction
	// 2. A valid UTXO in a mined
	// 3. A UTXO that is invalid because it is non-existent
	// 4. A UTXO that is invalid because it has the wrong script type
	// 5. A UTXO that becomes invalid in a reorg
	// 6. A UTXO with a pay-to-script-hash for a 1-of-2 multisig redeem script
	// 7. A UTXO with a pay-to-script-hash for a 2-of-2 multisig redeem script
	// 8. A UTXO spending a pay-to-witness-pubkey-hash (P2WPKH) script.
	// 9. A UTXO spending a pay-to-witness-script-hash (P2WSH) 2-of-2 multisig
	//    redeem script
	// 10. A UTXO from a coinbase transaction, before and after maturing.

	// Create a BTCBackend with the test node.
	btc, shutdown := testBackend()
	defer shutdown()

	// The vout will be randomized during reset.
	const txHeight = uint32(50)

	// A general reset function that clears the testBlockchain and the blockCache.
	reset := func() {
		cleanTestChain()
		btc.blockCache = newBlockCache()
	}

	// CASE 1: A valid UTXO in a mempool transaction
	reset()
	txHash := randomHash()
	blockHash := randomHash()
	msg := testMakeMsgTx(false)
	// For a regular test tx, the output is at output index 0. Pass nil for the
	// block hash and 0 for the block height and confirmations for a mempool tx.
	testAddTxOut(msg.tx, msg.vout, txHash, nil, 0, 0)
	// There is no block info to add, since this is a mempool transaction
	utxo, err := btc.utxo(txHash, msg.vout, nil)
	if err != nil {
		t.Fatalf("case 1 - unexpected error: %v", err)
	}
	// While we're here, check the spend script size is correct.
	scriptSize := utxo.ScriptSize()
	if scriptSize != P2PKHSigScriptSize+txInOverhead {
		t.Fatalf("case 1 - unexpected spend script size reported. expected %d, got %d", P2PKHSigScriptSize, scriptSize)
	}
	// Now "mine" the transaction.
	testAddBlockVerbose(blockHash, nil, 1, txHeight)
	// Overwrite the test blockchain transaction details.
	testAddTxOut(msg.tx, 0, txHash, blockHash, int64(txHeight), 1)
	// "mining" the block should cause a reorg.
	confs, err := utxo.Confirmations()
	if err != nil {
		t.Fatalf("case 1 - error retrieving confirmations after transaction \"mined\": %v", err)
	}
	if confs != 1 {
		// The confirmation count is not taken from the wire.TxOut.Confirmations,
		// so check that it is correctly calculated based on height.
		t.Fatalf("case 1 - expected 1 confirmation after mining transaction, found %d", confs)
	}
	// Make sure the pubkey spends the output.
	spends, err := utxo.PaysToPubkeys([][]byte{msg.pubkey})
	if err != nil {
		t.Fatalf("case 1 - PaysToPubkeys error: %v", err)
	}
	if !spends {
		//
		t.Fatalf("case 1 - false returned from PaysToPubkeys")
	}

	// CASE 2: A valid UTXO in a mined block. This UTXO will have non-zero
	// confirmations, a valid pkScipt
	reset()
	blockHash = testAddBlockVerbose(nil, nil, 1, txHeight)
	txHash = randomHash()
	msg = testMakeMsgTx(false)
	testAddTxOut(msg.tx, msg.vout, txHash, blockHash, int64(txHeight), 1)
	utxo, err = btc.utxo(txHash, msg.vout, nil)
	if err != nil {
		t.Fatalf("case 2 - unexpected error: %v", err)
	}
	spends, err = utxo.PaysToPubkeys([][]byte{msg.pubkey})
	if err != nil {
		t.Fatalf("case 2 - PaysToPubkeys error: %v", err)
	}
	if !spends {
		t.Fatalf("case 2 - false returned from PaysToPubkeys")
	}

	// CASE 3: A UTXO that is invalid because it is non-existent
	reset()
	_, err = btc.utxo(randomHash(), 0, nil)
	if err == nil {
		t.Fatalf("case 3 - received no error for a non-existent UTXO")
	}

	// CASE 4: A UTXO that is invalid because it has the wrong script type.
	reset()
	blockHash = testAddBlockVerbose(nil, nil, 1, txHeight)
	txHash = randomHash()
	msg = testMakeMsgTx(false)
	// make the script nonsense.
	msg.tx.TxOut[0].PkScript = []byte{0x00, 0x01, 0x02, 0x03}
	testAddTxOut(msg.tx, msg.vout, txHash, blockHash, int64(txHeight), 1)
	_, err = btc.utxo(txHash, msg.vout, nil)
	if err == nil {
		t.Fatalf("case 4 - received no error for a UTXO with wrong script type")
	}

	// CASE 5: A UTXO that becomes invalid in a reorg
	reset()
	txHash = randomHash()
	blockHash = testAddBlockVerbose(nil, nil, 1, txHeight)
	msg = testMakeMsgTx(false)
	testAddTxOut(msg.tx, msg.vout, txHash, blockHash, int64(txHeight), 1)
	utxo, err = btc.utxo(txHash, msg.vout, nil)
	if err != nil {
		t.Fatalf("case 5 - received error for utxo")
	}
	_, err = utxo.Confirmations()
	if err != nil {
		t.Fatalf("case 5 - received error before reorg")
	}
	testAddBlockVerbose(nil, nil, 1, txHeight)
	// Remove the txout from the blockchain, since bitcoind would no longer
	// return it.
	testDeleteTxOut(txHash, msg.vout)
	time.Sleep(blockPollDelay)
	_, err = utxo.Confirmations()
	if err == nil {
		t.Fatalf("case 5 - received no error for orphaned transaction")
	}
	// Now put it back in mempool and check again.
	testAddTxOut(msg.tx, msg.vout, txHash, nil, 0, 0)
	confs, err = utxo.Confirmations()
	if err != nil {
		t.Fatalf("case 5 - error checking confirmations on orphaned transaction back in mempool: %v", err)
	}
	if confs != 0 {
		t.Fatalf("case 5 - expected 0 confirmations, got %d", confs)
	}

	// CASE 6: A UTXO with a pay-to-script-hash for a 1-of-2 multisig redeem
	// script
	reset()
	txHash = randomHash()
	blockHash = testAddBlockVerbose(nil, nil, 1, txHeight)
	msgMultiSig := testMsgTxP2SHMofN(1, 2, false)
	testAddTxOut(msgMultiSig.tx, msgMultiSig.vout, txHash, blockHash, int64(txHeight), 1)
	// First try to get the UTXO without providing the raw script.
	_, err = btc.utxo(txHash, msgMultiSig.vout, nil)
	if err == nil {
		t.Fatalf("no error thrown for p2sh utxo when no script was provided")
	}
	// Now provide the script.
	utxo, err = btc.utxo(txHash, msgMultiSig.vout, msgMultiSig.script)
	if err != nil {
		t.Fatalf("case 6 - received error for utxo: %v", err)
	}
	confs, err = utxo.Confirmations()
	if err != nil {
		t.Fatalf("case 6 - error getting confirmations: %v", err)
	}
	if confs != 1 {
		t.Fatalf("case 6 - expected 1 confirmation, got %d", confs)
	}
	spends, err = utxo.PaysToPubkeys(msgMultiSig.pubkeys[:1])
	if err != nil {
		t.Fatalf("case 6 - PaysToPubkeys error: %v", err)
	}
	if !spends {
		t.Fatalf("case 6 - false returned from PaysToPubkeys")
	}

	// CASE 7: A UTXO with a pay-to-script-hash for a 2-of-2 multisig redeem
	// script
	reset()
	txHash = randomHash()
	blockHash = testAddBlockVerbose(nil, nil, 1, txHeight)
	msgMultiSig = testMsgTxP2SHMofN(2, 2, false)
	testAddTxOut(msgMultiSig.tx, msgMultiSig.vout, txHash, blockHash, int64(txHeight), 1)
	utxo, err = btc.utxo(txHash, msgMultiSig.vout, msgMultiSig.script)
	if err != nil {
		t.Fatalf("case 7 - received error for utxo: %v", err)
	}
	// Try to get by with just one of the pubkeys.
	_, err = utxo.PaysToPubkeys(msgMultiSig.pubkeys[:1])
	if err == nil {
		t.Fatalf("case 7 - no error when only provided one of two required pubkeys")
	}
	// Now do both.
	spends, err = utxo.PaysToPubkeys(msgMultiSig.pubkeys)
	if err != nil {
		t.Fatalf("case 7 - PaysToPubkeys error: %v", err)
	}
	if !spends {
		t.Fatalf("case 7 - false returned from PaysToPubkeys")
	}

	// CASE 8: A UTXO spending a pay-to-witness-pubkey-hash (P2WPKH) script.
	reset()
	blockHash = testAddBlockVerbose(nil, nil, 1, txHeight)
	txHash = randomHash()
	msg = testMakeMsgTx(true) // true - P2WPKH at vout 0
	testAddTxOut(msg.tx, msg.vout, txHash, blockHash, int64(txHeight), 1)
	utxo, err = btc.utxo(txHash, msg.vout, nil)
	if err != nil {
		t.Fatalf("case 8 - unexpected error: %v", err)
	}
	// Check that the segwit flag is set.
	if !utxo.scriptType.isSegwit() {
		t.Fatalf("case 8 - script type parsed as non-segwit")
	}
	spends, err = utxo.PaysToPubkeys([][]byte{msg.pubkey})
	if err != nil {
		t.Fatalf("case 8 - PaysToPubkeys error: %v", err)
	}
	if !spends {
		t.Fatalf("case 8 - false returned from PaysToPubkeys")
	}

	// CASE 9: A UTXO spending a pay-to-witness-script-hash (P2WSH) 2-of-2
	// multisig redeem script
	reset()
	txHash = randomHash()
	blockHash = testAddBlockVerbose(nil, nil, 1, txHeight)
	msgMultiSig = testMsgTxP2SHMofN(2, 2, true)
	testAddTxOut(msgMultiSig.tx, msgMultiSig.vout, txHash, blockHash, int64(txHeight), 1)
	utxo, err = btc.utxo(txHash, msgMultiSig.vout, msgMultiSig.script)
	if err != nil {
		t.Fatalf("case 9 - received error for utxo: %v", err)
	}
	// Check that the script is flagged segwit
	if !utxo.scriptType.isSegwit() {
		t.Fatalf("case 9 - script type parsed as non-segwit")
	}
	// Try to get by with just one of the pubkeys.
	_, err = utxo.PaysToPubkeys(msgMultiSig.pubkeys[:1])
	if err == nil {
		t.Fatalf("case 9 - no error when only provided one of two required pubkeys")
	}
	// Now do both.
	spends, err = utxo.PaysToPubkeys(msgMultiSig.pubkeys)
	if err != nil {
		t.Fatalf("case 9 - PaysToPubkeys error: %v", err)
	}
	if !spends {
		t.Fatalf("case 9 - false returned from PaysToPubkeys")
	}

	// CASE 10: A UTXO from a coinbase transaction, before and after maturing.
	reset()
	blockHash = testAddBlockVerbose(nil, nil, 1, txHeight)
	txHash = randomHash()
	msg = testMakeMsgTx(false)
	txOut := testAddTxOut(msg.tx, msg.vout, txHash, blockHash, int64(txHeight), 1)
	txOut.Coinbase = true
	_, err = btc.utxo(txHash, msg.vout, nil)
	if err == nil {
		t.Fatalf("case 10 - no error for immature transaction")
	}
	if err != immatureTransactionError {
		t.Fatalf("case 10 - expected immatureTransactionError, got: %v", err)
	}
	// Mature the transaction
	maturity := uint32(testParams.CoinbaseMaturity)
	testAddBlockVerbose(nil, nil, 1, txHeight+maturity-1)
	txOut.Confirmations = int64(maturity)
	time.Sleep(blockPollDelay)
	_, err = btc.utxo(txHash, msg.vout, nil)
	if err != nil {
		t.Fatalf("case 10 - unexpected error after maturing: %v", err)
	}
}

// TestReorg tests various reorg paths. Because bitcoind doesn't support
// websocket notifications, and ZeroMQ is not desirable, BTCBackend polls for
// new block data every 5 seconds or so. The poll interval means it's possible
// for various flavors of reorg to happen before a new block is detected. These
// tests check that reorgs of various depths are correctly handled by the
// block monitor loop and the block cache.
func TestReorg(t *testing.T) {
	// Create a BTCBackend with the test node.
	btc, shutdown := testBackend()
	defer shutdown()

	// Clear the blockchain and set the provided chain to build on the ancestor
	// block.
	reset := func() {
		cleanTestChain()
		btc.blockCache = newBlockCache()
	}
	reset()

	ancestorHeight := uint32(50)
	ancestorHash := testAddBlockVerbose(nil, nil, 0, ancestorHeight)

	makeChain := func() []*chainhash.Hash {
		chain := make([]*chainhash.Hash, 0, 3)
		for i := 0; i < 3; i++ {
			h := testAddBlockVerbose(nil, nil, 0, ancestorHeight+1+uint32(i))
			chain = append(chain, h)
		}
		return chain
	}
	chainA := makeChain()
	chainB := makeChain()

	setChain := func(hashes []*chainhash.Hash) {
		testClearBestBlock()
		rootConfs := int64(len(hashes)) + 1
		// Add the ancestor block
		testAddBlockVerbose(ancestorHash, nil, rootConfs, ancestorHeight)
		prevHash := ancestorHash
		for i, hash := range hashes {
			prevHash = testAddBlockVerbose(hash, prevHash, rootConfs-int64(i), ancestorHeight+uint32(i))

		}
		time.Sleep(blockPollDelay)
	}

	setSidechainConfs := func(hashes []*chainhash.Hash) {
		for _, hash := range hashes {
			blk, err := btc.node.GetBlockVerbose(hash)
			if err != nil {
				t.Fatalf("error retrieving sidechain block to set confirmations: %v", err)
			}
			testChainMtx.Lock()
			blk.Confirmations = -1
			testChainMtx.Unlock()
		}
	}

	checkOrphanState := func(hashes []*chainhash.Hash, orphanState bool) bool {
		for _, hash := range hashes {
			blk, err := btc.getBtcBlock(hash)
			if err != nil {
				t.Fatalf("error retrieving block after reorg: %v\n", err)
			}
			if blk.orphaned != orphanState {
				return false
			}
		}
		return true
	}

	// The test will start with chain A at length lenA above the ancestor. A reorg
	// of length lenB will fully replace chain A. Chain A should be fully
	// orphaned and chain B should be fully mainchain afterwards.
	test := func(lenA, lenB int) {
		reset()
		setChain(chainA[:lenA])
		// Check that chain A is all mainchain.
		if !checkOrphanState(chainA[:lenA], false) {
			t.Fatalf("chain A block not mainchain for test %d:%d before reorg", lenA, lenB)
		}
		// Reorg the chain.
		setSidechainConfs(chainA[:lenA])
		setChain(chainB[:lenB])
		// Chain A should all be orphaned.
		if !checkOrphanState(chainA[:lenA], true) {
			t.Fatalf("chain A block still mainchain for test %d:%d after reorg", lenA, lenB)
		}
		// Chain B should all be mainchain.
		if !checkOrphanState(chainB[:lenB], false) {
			t.Fatalf("chain B block not mainchain for test %d:%d after reorg", lenA, lenB)
		}
	}

	// Now run 9 tests.
	for a := 1; a <= 3; a++ {
		for b := 1; b <= 3; b++ {
			test(a, b)
		}
	}
}

// TestSignatures checks that signature verification is working.
func TestSignatures(t *testing.T) {
	btc := BTCBackend{}
	verify := func(pk, msg, sig string) {
		pubkey, _ := hex.DecodeString(pk)
		message, _ := hex.DecodeString(msg)
		signature, _ := hex.DecodeString(sig)
		if !btc.VerifySignature(message, pubkey, signature) {
			t.Fatalf("failed signature with pubkey %s", pk)
		}
	}
	// Some randomly generated messages, pubkeys, signatures.
	verify(
		"0286a0a6e0b95a540671c9237f84ceb9a98970729b5fd76e02cb5e449138d5f873",
		"2258cbd6271157d1ba6676c9a8520a2da4194e8f511a5885156470d2de667171913fbb35019b31b6da8c914408ca25fb7b6a",
		"30440220436d2c54a5e22ca99ec23d8e0f3ae6cac0fbda9224809a2302dd865502033f6702206132532571cdccec3f48fe7db954680f7aac72b308a71ec06299ab8108bb8810e",
	)
	verify(
		"024cd2f9e57b674e90ffed62acbc554d3bf9d76ebbc82236f3b4d2243b648f8906",
		"c42acf2b8e8693d2e29864758a313c3737b993bef3dd85d4eaa052f02c7c844caa3b9ae431a948f62b92293eebed9645387c",
		"3045022100e7411d6da3c92020083e74834f7e0935aba0d44b1a47b1a2ae0372c69f564fdb02204eb254124ee42d637dabb4137cce08124a603963a339f08f65a2157e013fd8d7",
	)
	verify(
		"030e5386d7744aa05c7f7e8751ee3fec109991e355f5d194cc390d4a621851761e",
		"d62b3c3c56ea1fa5152a5bb84bcce9fe880ffc2c646caa029e92203e6eee2f0c22a7958c891dbccfc3b0e578fad8d2566db4",
		"304402204ad2ac5695d0f4a28fc1f01466d03b2bd0300ce41f393b8ac398dd57aad8086f02203f4789d13c060a2c976aca2ca39184125c3fe2d9ba2350b1c27059acb29d6757",
	)
}

// TestTx checks the transaction-related methods and functions.
func TestTx(t *testing.T) {
	// Create a BTCBackend with the test node.
	btc, shutdown := testBackend()
	defer shutdown()

	// Test 1: Test different states of validity.
	cleanTestChain()
	blockHeight := uint32(500)
	txHash := randomHash()
	blockHash := randomHash()
	msg := testMakeMsgTx(false)
	verboseTx := testAddTxVerbose(msg.tx, txHash, blockHash, int64(blockHeight), 2)
	// Mine the transaction and an approving block.
	testAddBlockVerbose(blockHash, nil, 1, blockHeight)
	testAddBlockVerbose(randomHash(), nil, 1, blockHeight+1)
	time.Sleep(blockPollDelay)
	// Add some random output at index 0.
	verboseTx.Vout = append(verboseTx.Vout, testVout(1, randomBytes(20)))
	// Add a swap output at index 1.
	swap := testMsgTxSwapInit()
	swapTxOut := swap.tx.TxOut[swap.vout]
	verboseTx.Vout = append(verboseTx.Vout, testVout(float64(swapTxOut.Value)/btcToSatoshi, swapTxOut.PkScript))
	// Make an input spending some random utxo.
	spentTx := randomHash()
	spentVout := rand.Uint32()
	verboseTx.Vin = append(verboseTx.Vin, testVin(spentTx, spentVout))
	// Get the transaction from the backend.
	dexTx, err := btc.transaction(txHash)
	if err != nil {
		t.Fatalf("error getting dex tx: %v", err)
	}
	// Check that there are 2 confirmations.
	confs, err := dexTx.Confirmations()
	if err != nil {
		t.Fatalf("unexpected error getting confirmation count: %v", err)
	}
	if confs != 2 {
		t.Fatalf("expected 2 confirmations, but %d were reported", confs)
	}
	// Check that the spent tx is in dexTx.
	spent := dexTx.SpendsUTXO(spentTx.String(), spentVout)
	if !spent {
		t.Fatalf("transaction not confirming spent utxo")
	}

	// Check that the swap contract doesn't match the first input.
	_, _, err = dexTx.AuditContract(0, swap.contract)
	if err == nil {
		t.Fatalf("no error for contract audit on non-swap output")
	}
	// Now try again with the correct vout.
	recipient, swapVal, err := dexTx.AuditContract(1, swap.contract)
	if err != nil {
		t.Fatalf("unexpected error auditing contract: %v", err)
	}
	if recipient != swap.recipient.String() {
		t.Fatalf("wrong recipient. wanted '%s' got '%s'", recipient, swap.recipient.String())
	}
	if swapVal != uint64(swapTxOut.Value) {
		t.Fatalf("unexpected output value. wanted %d, got %d", swapVal, swapTxOut.Value)
	}

	// Now do an invalidating reorg, and check that Confirmations returns an
	// error.
	testClearBestBlock()
	testAddBlockVerbose(nil, nil, 1, blockHeight)
	// Wait for the reorg to be seen by the BTCBackend.loop.
	time.Sleep(blockPollDelay)
	_, err = dexTx.Confirmations()
	if err == nil {
		t.Fatalf("no error when checking confirmations on an invalidated tx")
	}

	// Test 2: Before and after mining.
	cleanTestChain()
	btc.blockCache = newBlockCache()
	txHash = randomHash()
	// Add a transaction to mempool.
	msg = testMakeMsgTx(false)
	testAddTxVerbose(msg.tx, txHash, nil, 0, 0)
	// Get the transaction data through the backend and make sure it has zero
	// confirmations.
	dexTx, err = btc.transaction(txHash)
	if err != nil {
		t.Fatalf("unexpected error retrieving transaction through backend: %v", err)
	}
	confs, err = dexTx.Confirmations()
	if err != nil {
		t.Fatalf("unexpected error getting confirmations on mempool transaction: %v", err)
	}
	if confs != 0 {
		t.Fatalf("expected 0 confirmations for mempool transaction, got %d", confs)
	}
	// Now mine the transaction
	blockHash = testAddBlockVerbose(nil, nil, 1, blockHeight)
	time.Sleep(blockPollDelay)
	// Update the verbose tx data
	testAddTxVerbose(msg.tx, txHash, blockHash, int64(blockHeight), 1)
	// Check Confirmations
	confs, err = dexTx.Confirmations()
	if err != nil {
		t.Fatalf("unexpected error getting confirmations on mined transaction: %v", err)
	}
	if confs != 1 {
		t.Fatalf("expected 1 confirmation for mined transaction, got %d", confs)
	}
	if dexTx.blockHash != *blockHash {
		t.Fatalf("unexpected block hash on a mined transaction. expected %s, got %s", blockHash, dexTx.blockHash)
	}
	if dexTx.height != int64(blockHeight) {
		t.Fatalf("unexpected block height on a mined transaction. expected %d, got %d", blockHeight, dexTx.height)
	}
}

// TestAuxiliary checks the UTXO convenience functions like TxHash, Vout, and
// TxID.
func TestAuxiliary(t *testing.T) {
	// Create a BTCBackend with the test node.
	btc, shutdown := testBackend()
	defer shutdown()

	// Add a transaction and retrieve it.
	cleanTestChain()
	maturity := int64(testParams.CoinbaseMaturity)
	msg := testMakeMsgTx(false)
	txid := hex.EncodeToString(randomBytes(32))
	txHash, _ := chainhash.NewHashFromStr(txid)
	txHeight := rand.Uint32()
	blockHash := testAddBlockVerbose(nil, nil, 1, txHeight)
	testAddTxOut(msg.tx, msg.vout, txHash, blockHash, int64(txHeight), maturity)
	utxo, err := btc.UTXO(txid, msg.vout, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(txHash.CloneBytes(), utxo.TxHash()) {
		t.Fatalf("utxo tx hash doesn't match")
	}
	if utxo.Vout() != msg.vout {
		t.Fatalf("utxo vout doesn't match")
	}
	if utxo.TxID() != txid {
		t.Fatalf("utxo txid doesn't match")
	}
}
