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
	"encoding/json"
	"errors"
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

	"decred.org/dcrdex/dex"
	dexbtc "decred.org/dcrdex/dex/networks/btc"
	"github.com/btcsuite/btcd/btcec"
	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
	"github.com/btcsuite/btcutil"
	"gopkg.in/ini.v1"
)

var (
	testParams     = &chaincfg.MainNetParams
	mainnetPort    = 8332
	blockPollDelay time.Duration
	defaultHost    = "localhost"
)

func TestMain(m *testing.M) {
	// Set any created Backends to poll for blocks every 50 ms to
	// accommodate reorg testing.
	blockPollInterval = time.Millisecond * 50
	blockPollDelay = blockPollInterval + time.Millisecond*5
	os.Exit(m.Run())
}

// TestConfig tests the LoadConfig function.
func TestConfig(t *testing.T) {
	cfg := &dexbtc.Config{}
	parsedCfg := &dexbtc.Config{}

	tempDir, err := ioutil.TempDir("", "btctest")
	if err != nil {
		t.Fatalf("error creating temporary directory: %v", err)
	}
	defer os.RemoveAll(tempDir)
	filePath := filepath.Join(tempDir, "test.conf")

	runCfg := func(config *dexbtc.Config) error {
		*cfg = *config
		cfgFile := ini.Empty()
		err := cfgFile.ReflectFrom(cfg)
		if err != nil {
			return err
		}
		err = cfgFile.SaveTo(filePath)
		if err != nil {
			return err
		}
		parsedCfg, err = dexbtc.LoadConfigFromPath(filePath, assetName, dex.Mainnet, dexbtc.RPCPorts)
		return err
	}

	// Check that there is an error from an unpopulated configuration.
	err = runCfg(cfg)
	if err == nil {
		t.Fatalf("no error for empty config")
	}

	// Try with just the name. Error expected.
	err = runCfg(&dexbtc.Config{
		RPCUser: "somename",
	})
	if err == nil {
		t.Fatalf("no error when just name provided")
	}

	// Try with just the password. Error expected.
	err = runCfg(&dexbtc.Config{
		RPCPass: "somepass",
	})
	if err == nil {
		t.Fatalf("no error when just password provided")
	}

	// Give both name and password. This should not be an error.
	err = runCfg(&dexbtc.Config{
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
	err = runCfg(&dexbtc.Config{
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
	err = runCfg(&dexbtc.Config{
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
	err = runCfg(&dexbtc.Config{
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
	err = runCfg(&dexbtc.Config{
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
	err = runCfg(&dexbtc.Config{
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
// as jsonrpc types to be requested by the Backend.
//
// General formula for testing
// 1. Create a Backend with the node field set to a testNode
// 2. Create a fake UTXO and all of the associated jsonrpc-type blocks and
//    transactions and add the to the test blockchain.
// 3. Verify the Backend and UTXO methods are returning whatever is expected.
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

// The testChain is a "blockchain" to store RPC responses for the Backend
// node stub to request.
var testChainMtx sync.RWMutex
var testChain testBlockChain
var testBestBlock testBlock

type testBlock struct {
	hash   chainhash.Hash
	height uint32
}

// This must be called before using the testNode.
func cleanTestChain() {
	testChainMtx.Lock()
	defer testChainMtx.Unlock()
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
type testNode struct {
	rawResult []byte
	rawErr    error
}

// Encode utxo info as a concatenated string hash:vout.
func txOutID(txHash *chainhash.Hash, index uint32) string {
	return txHash.String() + ":" + strconv.Itoa(int(index))
}

const optimalFeeRate uint64 = 24

func (*testNode) EstimateSmartFee(confTarget int64, mode *btcjson.EstimateSmartFeeMode) (*btcjson.EstimateSmartFeeResult, error) {
	optimalRate := float64(optimalFeeRate) * 1e-5
	// fmt.Println((float64(optimalFeeRate)*1e-5)-0.00024)
	return &btcjson.EstimateSmartFeeResult{
		Blocks:  2,
		FeeRate: &optimalRate,
	}, nil
}

// Part of the btcNode interface.
func (t *testNode) GetTxOut(txHash *chainhash.Hash, index uint32, _ bool) (*btcjson.GetTxOutResult, error) {
	testChainMtx.RLock()
	defer testChainMtx.RUnlock()
	outID := txOutID(txHash, index)
	out := testChain.txOuts[outID]
	// Unfound is not an error for GetTxOut.
	return out, nil
}

// Part of the btcNode interface.
func (t *testNode) GetRawTransactionVerbose(txHash *chainhash.Hash) (*btcjson.TxRawResult, error) {
	testChainMtx.RLock()
	defer testChainMtx.RUnlock()
	tx, found := testChain.txRaws[*txHash]
	if !found {
		return nil, fmt.Errorf("test transaction not found\n")
	}
	return tx, nil
}

// Part of the btcNode interface.
func (t *testNode) GetBlockVerbose(blockHash *chainhash.Hash) (*btcjson.GetBlockVerboseResult, error) {
	testChainMtx.RLock()
	defer testChainMtx.RUnlock()
	block, found := testChain.blocks[*blockHash]
	if !found {
		return nil, fmt.Errorf("test block not found")
	}
	return block, nil
}

// Part of the btcNode interface.
func (t *testNode) GetBlockHash(blockHeight int64) (*chainhash.Hash, error) {
	testChainMtx.RLock()
	defer testChainMtx.RUnlock()
	hash, found := testChain.hashes[blockHeight]
	if !found {
		return nil, fmt.Errorf("test hash not found")
	}
	return hash, nil
}

// Part of the btcNode interface.
func (t *testNode) GetBestBlockHash() (*chainhash.Hash, error) {
	testChainMtx.RLock()
	defer testChainMtx.RUnlock()
	bbHash := testBestBlock.hash
	return &bbHash, nil
}

func (t *testNode) RawRequest(string, []json.RawMessage) (json.RawMessage, error) {
	if t.rawErr != nil {
		return nil, t.rawErr
	}
	return t.rawResult, nil
}

// Create a btcjson.GetTxOutResult such as is returned from GetTxOut.
func testGetTxOut(confirmations, value int64, pkScript []byte) *btcjson.GetTxOutResult {
	return &btcjson.GetTxOutResult{
		Value:         float64(value) / 1e8,
		Confirmations: confirmations,
		ScriptPubKey: btcjson.ScriptPubKeyResult{
			Hex: hex.EncodeToString(pkScript),
		},
	}
}

// Create a *btcjson.TxRawResult such as is returned by
// GetRawTransactionVerbose.
func testRawTransactionVerbose(msgTx *wire.MsgTx, txid, blockHash *chainhash.Hash,
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
	txOut := testGetTxOut(confirmations, msgTx.TxOut[vout].Value, msgTx.TxOut[vout].PkScript)
	testChain.txOuts[txOutID(txHash, vout)] = txOut
	testAddTxVerbose(msgTx, txHash, blockHash, confirmations)
	return txOut
}

// Add a btcjson.TxRawResult to the blockchain.
func testAddTxVerbose(msgTx *wire.MsgTx, txHash, blockHash *chainhash.Hash, confirmations int64) *btcjson.TxRawResult {
	tx := testRawTransactionVerbose(msgTx, txHash, blockHash, confirmations)
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

type testAuth struct {
	pubkey []byte
	pkHash []byte
	msg    []byte
	sig    []byte
}

type testMsgTx struct {
	tx   *wire.MsgTx
	auth *testAuth
	vout uint32
}

func s256Auth(msg []byte) *testAuth {
	priv, err := btcec.NewPrivateKey(btcec.S256())
	if err != nil {
		fmt.Printf("s256Auth error: %v\n", err)
	}
	pubkey := priv.PubKey().SerializeCompressed()
	if msg == nil {
		msg = randomBytes(32)
	}
	sig, err := priv.Sign(msg)
	if err != nil {
		fmt.Printf("s256Auth sign error: %v\n", err)
	}
	return &testAuth{
		pubkey: pubkey,
		pkHash: btcutil.Hash160(pubkey),
		msg:    msg,
		sig:    sig.Serialize(),
	}
}

// Generate a public key on the secp256k1 curve.
func genPubkey() ([]byte, []byte) {
	_, pub := btcec.PrivKeyFromBytes(btcec.S256(), randomBytes(32))
	pubkey := pub.SerializeCompressed()
	pkHash := btcutil.Hash160(pubkey)
	return pubkey, pkHash
}

// A pay-to-script-hash pubkey script.
func newP2PKHScript(segwit bool) ([]byte, *testAuth) {
	auth := s256Auth(nil)
	var pkScript []byte
	var err error
	if segwit {
		pkScript, err = txscript.NewScriptBuilder().
			AddOp(txscript.OP_0).
			AddData(auth.pkHash).
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
			AddData(auth.pkHash).
			AddOps([]byte{
				txscript.OP_EQUALVERIFY,
				txscript.OP_CHECKSIG,
			}).Script()
		if err != nil {
			fmt.Printf("newP2PKHScript error: %v\n", err)
		}
	}

	return pkScript, auth
}

// A MsgTx for a regular transaction with a single output. No inputs, so it's
// not really a valid transaction, but that's okay on testBlockchain.
func testMakeMsgTx(segwit bool) *testMsgTx {
	pkScript, auth := newP2PKHScript(segwit)
	msgTx := wire.NewMsgTx(wire.TxVersion)
	msgTx.AddTxOut(wire.NewTxOut(1, pkScript))
	return &testMsgTx{
		tx:   msgTx,
		auth: auth,
	}
}

type testMsgTxSwap struct {
	tx        *wire.MsgTx
	contract  []byte
	recipient btcutil.Address
	refund    btcutil.Address
}

// Create a swap (initialization) contract with random pubkeys and return the
// pubkey script and addresses.
func testSwapContract(segwit bool) ([]byte, btcutil.Address, btcutil.Address) {
	lockTime := time.Now().Add(time.Hour * 8).Unix()
	secretHash := randomBytes(32)
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
		}).AddData(secretHash).
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
	var receiverAddr, refundAddr btcutil.Address
	if segwit {
		receiverAddr, _ = btcutil.NewAddressWitnessPubKeyHash(receiverPKH, testParams)
		refundAddr, _ = btcutil.NewAddressWitnessPubKeyHash(senderPKH, testParams)
	} else {
		receiverAddr, _ = btcutil.NewAddressPubKeyHash(receiverPKH, testParams)
		refundAddr, _ = btcutil.NewAddressPubKeyHash(senderPKH, testParams)
	}

	return contract, receiverAddr, refundAddr
}

func testMsgTxSwapInit(val int64, segwit bool) *testMsgTxSwap {
	msgTx := wire.NewMsgTx(wire.TxVersion)
	contract, recipient, refund := testSwapContract(segwit)
	scriptHash := btcutil.Hash160(contract)
	pkScript, err := txscript.NewScriptBuilder().
		AddOp(txscript.OP_HASH160).
		AddData(scriptHash).
		AddOp(txscript.OP_EQUAL).
		Script()
	if err != nil {
		fmt.Printf("script building error in testMsgTxSwapInit: %v", err)
	}
	msgTx.AddTxOut(wire.NewTxOut(val, pkScript))
	return &testMsgTxSwap{
		tx:        msgTx,
		contract:  contract,
		recipient: recipient,
		refund:    refund,
	}
}

type testMultiSigAuth struct {
	pubkeys  [][]byte
	pkHashes [][]byte
	msg      []byte
	sigs     [][]byte
}

// Information about a transaction with a P2SH output.
type testMsgTxP2SH struct {
	tx     *wire.MsgTx
	auth   *testMultiSigAuth
	vout   uint32
	script []byte
	n      int
	m      int
}

// An M-of-N multi-sig script.
func testMultiSigScriptMofN(m, n int) ([]byte, *testMultiSigAuth) {
	// serialized compressed pubkey used for multisig
	addrs := make([]*btcutil.AddressPubKey, 0, n)
	auth := &testMultiSigAuth{
		msg: randomBytes(32),
	}

	for i := 0; i < n; i++ {
		a := s256Auth(auth.msg)
		if i < m {
			auth.pubkeys = append(auth.pubkeys, a.pubkey)
			auth.pkHashes = append(auth.pkHashes, a.pkHash)
			auth.sigs = append(auth.sigs, a.sig)
		}
		addr, err := btcutil.NewAddressPubKey(a.pubkey, testParams)
		if err != nil {
			fmt.Printf("error creating AddressSecpPubKey: %v\n", err)
			return nil, nil
		}
		addrs = append(addrs, addr)
	}
	script, err := txscript.MultiSigScript(addrs, m)
	if err != nil {
		fmt.Printf("error creating MultiSigScript: %v\n", err)
		return nil, nil
	}
	return script, auth
}

// A pay-to-script-hash M-of-N multi-sig output and vout 0 of a MsgTx.
func testMsgTxP2SHMofN(m, n int, segwit bool) *testMsgTxP2SH {
	script, auth := testMultiSigScriptMofN(m, n)
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
		tx:     msgTx,
		auth:   auth,
		script: script,
		vout:   0,
		n:      n,
		m:      m,
	}
}

// Make a backend that logs to stdout.
func testBackend(segwit bool) (*Backend, func()) {
	logger := dex.StdOutLogger("TEST", dex.LevelTrace)
	// skip both loading config file and rpcclient.New in Connect. Manually
	// create the Backend and set the node to our node stub.
	btc := newBTC("btc", segwit, testParams, logger, nil)
	btc.node = &testNode{}

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		btc.run(ctx)
		wg.Done()
	}()
	shutdown := func() {
		cancel()
		wg.Wait()
	}
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

	// Create a Backend with the test node.
	btc, shutdown := testBackend(false)
	defer shutdown()

	// The vout will be randomized during reset.
	const txHeight = uint32(50)

	// A general reset function that clears the testBlockchain and the blockCache.
	reset := func() {
		btc.blockCache.mtx.Lock()
		defer btc.blockCache.mtx.Unlock()
		cleanTestChain()
		newBC := newBlockCache()
		btc.blockCache.blocks = newBC.blocks
		btc.blockCache.mainchain = newBC.mainchain
		btc.blockCache.best = newBC.best
	}

	// CASE 1: A valid UTXO in a mempool transaction
	reset()
	txHash := randomHash()
	blockHash := randomHash()
	msg := testMakeMsgTx(false)
	// For a regular test tx, the output is at output index 0. Pass nil for the
	// block hash and 0 for the block height and confirmations for a mempool tx.
	txout := testAddTxOut(msg.tx, msg.vout, txHash, nil, 0, 0)
	// Set the value for this one.
	txout.Value = 5.0
	// There is no block info to add, since this is a mempool transaction
	utxo, err := btc.utxo(txHash, msg.vout, nil)
	if err != nil {
		t.Fatalf("case 1 - unexpected error: %v", err)
	}
	// While we're here, check the spend script size and value are correct.
	scriptSize := utxo.SpendSize()
	wantScriptSize := uint32(dexbtc.TxInOverhead + 1 + dexbtc.RedeemP2PKHSigScriptSize)
	if scriptSize != wantScriptSize {
		t.Fatalf("case 1 - unexpected spend script size reported. expected %d, got %d", wantScriptSize, scriptSize)
	}
	if utxo.Value() != 500_000_000 {
		t.Fatalf("case 1 - unexpected output value. expected 500,000,000, got %d", utxo.Value())
	}
	// Now "mine" the transaction.
	testAddBlockVerbose(blockHash, nil, 1, txHeight)
	// Overwrite the test blockchain transaction details.
	testAddTxOut(msg.tx, 0, txHash, blockHash, int64(txHeight), 1)
	// "mining" the block should cause a reorg.
	confs, err := utxo.Confirmations(context.Background())
	if err != nil {
		t.Fatalf("case 1 - error retrieving confirmations after transaction \"mined\": %v", err)
	}
	if confs != 1 {
		// The confirmation count is not taken from the wire.TxOut.Confirmations,
		// so check that it is correctly calculated based on height.
		t.Fatalf("case 1 - expected 1 confirmation after mining transaction, found %d", confs)
	}
	// Make sure the pubkey spends the output.
	err = utxo.Auth([][]byte{msg.auth.pubkey}, [][]byte{msg.auth.sig}, msg.auth.msg)
	if err != nil {
		t.Fatalf("case 1 - Auth error: %v", err)
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
	err = utxo.Auth([][]byte{msg.auth.pubkey}, [][]byte{msg.auth.sig}, msg.auth.msg)
	if err != nil {
		t.Fatalf("case 2 - Auth error: %v", err)
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
	_, err = utxo.Confirmations(context.Background())
	if err != nil {
		t.Fatalf("case 5 - received error before reorg")
	}
	testAddBlockVerbose(nil, nil, 1, txHeight)
	// Remove the txout from the blockchain, since bitcoind would no longer
	// return it.
	testDeleteTxOut(txHash, msg.vout)
	time.Sleep(blockPollDelay)
	_, err = utxo.Confirmations(context.Background())
	if err == nil {
		t.Fatalf("case 5 - received no error for orphaned transaction")
	}
	// Now put it back in mempool and check again.
	testAddTxOut(msg.tx, msg.vout, txHash, nil, 0, 0)
	confs, err = utxo.Confirmations(context.Background())
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
	confs, err = utxo.Confirmations(context.Background())
	if err != nil {
		t.Fatalf("case 6 - error getting confirmations: %v", err)
	}
	if confs != 1 {
		t.Fatalf("case 6 - expected 1 confirmation, got %d", confs)
	}
	err = utxo.Auth(msgMultiSig.auth.pubkeys[:1], msgMultiSig.auth.sigs[:1], msgMultiSig.auth.msg)
	if err != nil {
		t.Fatalf("case 6 - Auth error: %v", err)
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
	err = utxo.Auth(msgMultiSig.auth.pubkeys[:1], msgMultiSig.auth.sigs[:1], msgMultiSig.auth.msg)
	if err == nil {
		t.Fatalf("case 7 - no error when only provided one of two required pubkeys")
	}
	// Now do both.
	err = utxo.Auth(msgMultiSig.auth.pubkeys, msgMultiSig.auth.sigs, msgMultiSig.auth.msg)
	if err != nil {
		t.Fatalf("case 7 - Auth error: %v", err)
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
	if !utxo.scriptType.IsSegwit() {
		t.Fatalf("case 8 - script type parsed as non-segwit")
	}
	err = utxo.Auth([][]byte{msg.auth.pubkey}, [][]byte{msg.auth.sig}, msg.auth.msg)
	if err != nil {
		t.Fatalf("case 8 - Auth error: %v", err)
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
	if !utxo.scriptType.IsSegwit() {
		t.Fatalf("case 9 - script type parsed as non-segwit")
	}
	// Try to get by with just one of the pubkeys.
	err = utxo.Auth(msgMultiSig.auth.pubkeys[:1], msgMultiSig.auth.sigs[:1], msgMultiSig.auth.msg)
	if err == nil {
		t.Fatalf("case 9 - no error when only provided one of two required pubkeys")
	}
	// Now do both.
	err = utxo.Auth(msgMultiSig.auth.pubkeys, msgMultiSig.auth.sigs, msgMultiSig.auth.msg)
	if err != nil {
		t.Fatalf("case 9 - Auth error: %v", err)
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
	if !errors.Is(err, immatureTransactionError) {
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

	// CASE 11: A swap contract
	val := int64(5)
	cleanTestChain()
	txHash = randomHash()
	blockHash = randomHash()
	swap := testMsgTxSwapInit(val, btc.segwit)
	testAddBlockVerbose(blockHash, nil, 1, txHeight)
	btcVal := btcutil.Amount(val).ToBTC()
	testAddTxOut(swap.tx, 0, txHash, blockHash, int64(txHeight), 1).Value = btcVal
	verboseTx := testChain.txRaws[*txHash]

	spentTxHash := randomHash()
	verboseTx.Vin = append(verboseTx.Vin, testVin(spentTxHash, 0))
	// We need to add that transaction to the blockchain too, because it will
	// be requested for the previous outpoint value.
	spentMsg := testMakeMsgTx(false)
	spentTx := testAddTxVerbose(spentMsg.tx, spentTxHash, blockHash, 2)
	spentTx.Vout = []btcjson.Vout{testVout(1, nil)}
	swapOut := swap.tx.TxOut[0]
	btcVal = btcutil.Amount(swapOut.Value).ToBTC()
	verboseTx.Vout = append(verboseTx.Vout, testVout(btcVal, swapOut.PkScript))
	utxo, err = btc.utxo(txHash, 0, swap.contract)
	if err != nil {
		t.Fatalf("case 11 - received error for utxo: %v", err)
	}

	contract := &Contract{Output: utxo.Output}

	// Now try again with the correct vout.
	err = btc.auditContract(contract) // sets refund and swap addresses
	if err != nil {
		t.Fatalf("case 11 - unexpected error auditing contract: %v", err)
	}
	if contract.SwapAddress() != swap.recipient.String() {
		t.Fatalf("case 11 - wrong recipient. wanted '%s' got '%s'", contract.SwapAddress(), swap.recipient.String())
	}
	if contract.RefundAddress() != swap.refund.String() {
		t.Fatalf("case 11 - wrong recipient. wanted '%s' got '%s'", contract.RefundAddress(), swap.refund.String())
	}
	if contract.Value() != 5 {
		t.Fatalf("case 11 - unexpected output value. wanted 5, got %d", contract.Value())
	}
}

func TestRedemption(t *testing.T) {
	t.Run("segwit", func(t *testing.T) {
		testRedemption(t, true)
	})
	t.Run("non-segwit", func(t *testing.T) {
		testRedemption(t, false)
	})
}

func testRedemption(t *testing.T, segwit bool) {
	btc, shutdown := testBackend(false)
	defer shutdown()

	// The vout will be randomized during reset.
	txHeight := uint32(32)
	cleanTestChain()
	txHash := randomHash()
	redemptionID := toCoinID(txHash, 0)
	// blockHash := randomHash()
	spentHash := randomHash()
	spentVout := uint32(0)
	spentID := toCoinID(spentHash, spentVout)
	verboseTx := testAddTxVerbose(testMakeMsgTx(segwit).tx, spentHash, nil, 0)
	verboseTx.Vout = append(verboseTx.Vout, btcjson.Vout{
		Value: 5,
	})
	msg := testMakeMsgTx(segwit)
	vin := btcjson.Vin{
		Txid: spentHash.String(),
		Vout: spentVout,
	}

	// A valid mempool redemption.
	verboseTx = testAddTxVerbose(msg.tx, txHash, nil, 0)
	verboseTx.Vin = append(verboseTx.Vin, vin)
	redemption, err := btc.Redemption(redemptionID, spentID)
	if err != nil {
		t.Fatalf("Redemption error: %v", err)
	}
	confs, err := redemption.Confirmations(context.Background())
	if err != nil {
		t.Fatalf("redemption Confirmations error: %v", err)
	}
	if confs != 0 {
		t.Fatalf("expected 0 confirmations, got %d", confs)
	}

	// Missing transaction
	testChainMtx.Lock()
	delete(testChain.txRaws, *txHash)
	testChainMtx.Unlock()
	_, err = btc.Redemption(redemptionID, spentID)
	if err == nil {
		t.Fatalf("No error for missing transaction")
	}

	// Doesn't spend transaction.
	verboseTx = testAddTxVerbose(msg.tx, txHash, nil, 0)
	verboseTx.Vin = append(verboseTx.Vin, btcjson.Vin{
		Txid: randomHash().String(),
	})
	_, err = btc.Redemption(redemptionID, spentID)
	if err == nil {
		t.Fatalf("No error for wrong previous outpoint")
	}

	// Mined transaction.
	blockHash := randomHash()
	blockHeight := txHeight - 1
	verboseTx = testAddTxVerbose(msg.tx, txHash, blockHash, int64(blockHeight))
	verboseTx.Vin = append(verboseTx.Vin, vin)
	testAddBlockVerbose(blockHash, nil, 1, blockHeight)
	redemption, err = btc.Redemption(redemptionID, spentID)
	if err != nil {
		t.Fatalf("Redemption with confs error: %v", err)
	}
	confs, err = redemption.Confirmations(context.Background())
	if err != nil {
		t.Fatalf("redemption with confs Confirmations error: %v", err)
	}
	if confs != 1 {
		t.Fatalf("expected 1 confirmation, got %d", confs)
	}
}

// TestReorg tests various reorg paths. Because bitcoind doesn't support
// websocket notifications, and ZeroMQ is not desirable, Backend polls for
// new block data every 5 seconds or so. The poll interval means it's possible
// for various flavors of reorg to happen before a new block is detected. These
// tests check that reorgs of various depths are correctly handled by the
// block monitor loop and the block cache.
func TestReorg(t *testing.T) {
	// Create a Backend with the test node.
	btc, shutdown := testBackend(false)
	defer shutdown()

	// Clear the blockchain and set the provided chain to build on the ancestor
	// block.
	reset := func() {
		btc.blockCache.mtx.Lock()
		defer btc.blockCache.mtx.Unlock()
		cleanTestChain()
		newBC := newBlockCache()
		btc.blockCache.blocks = newBC.blocks
		btc.blockCache.mainchain = newBC.mainchain
		btc.blockCache.best = newBC.best
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
			// Set Confirmations
			btc.blockCache.mtx.Lock() // read from (*blockCache).add in cache.go
			testChainMtx.Lock()       // field of testChain
			blk.Confirmations = -1
			testChainMtx.Unlock()
			btc.blockCache.mtx.Unlock()
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

	// Create a transaction at the tip, then orphan the block and move the
	// transaction to mempool.
	reset()
	setChain(chainA)
	tipHeight := btc.blockCache.tipHeight()
	txHash := randomHash()
	tip, _ := btc.blockCache.atHeight(tipHeight)
	msg := testMakeMsgTx(false)
	testAddBlockVerbose(&tip.hash, nil, 1, tipHeight)
	testAddTxOut(msg.tx, 0, txHash, &tip.hash, int64(tipHeight), 1)
	utxo, err := btc.utxo(txHash, msg.vout, nil)
	if err != nil {
		t.Fatalf("utxo error 1: %v", err)
	}
	confs, err := utxo.Confirmations(context.Background())
	if err != nil {
		t.Fatalf("Confirmations error: %v", err)
	}
	if confs != 1 {
		t.Fatalf("wrong number of confirmations. expected 1, got %d", confs)
	}

	// Orphan the block and move the transaction to mempool.
	btc.blockCache.reorg(int64(ancestorHeight))
	testAddTxOut(msg.tx, 0, txHash, nil, 0, 0)
	confs, err = utxo.Confirmations(context.Background())
	if err != nil {
		t.Fatalf("Confirmations error after reorg: %v", err)
	}
	if confs != 0 {
		t.Fatalf("Expected zero confirmations after reorg, found %d", confs)
	}

	// Start over, but put it in a lower block instead.
	reset()
	setChain(chainA)
	tip, _ = btc.blockCache.atHeight(tipHeight)
	testAddBlockVerbose(&tip.hash, nil, 1, tipHeight)
	testAddTxOut(msg.tx, 0, txHash, &tip.hash, int64(tipHeight), 1)
	utxo, err = btc.utxo(txHash, msg.vout, nil)
	if err != nil {
		t.Fatalf("utxo error 2: %v", err)
	}

	// Reorg and add a single block with the transaction.
	btc.blockCache.reorg(int64(ancestorHeight))
	newBlockHash := randomHash()
	testAddTxOut(msg.tx, 0, txHash, newBlockHash, int64(ancestorHeight+1), 1)
	testAddBlockVerbose(newBlockHash, ancestorHash, 1, ancestorHeight+1)
	time.Sleep(blockPollDelay)
	confs, err = utxo.Confirmations(context.Background())
	if err != nil {
		t.Fatalf("Confirmations error after reorg to lower block: %v", err)
	}
	if confs != 1 {
		t.Fatalf("Expected zero confirmations after reorg to lower block, found %d", confs)
	}
}

// TestAuxiliary checks the UTXO convenience functions like TxHash, Vout, and
// TxID.
func TestAuxiliary(t *testing.T) {
	// Create a Backend with the test node.
	btc, shutdown := testBackend(false)
	defer shutdown()

	// Add a transaction and retrieve it.
	cleanTestChain()
	maturity := int64(testParams.CoinbaseMaturity)
	msg := testMakeMsgTx(false)
	valueSats := int64(29e6)
	msg.tx.TxOut[0].Value = valueSats
	txid := hex.EncodeToString(randomBytes(32))
	txHash, _ := chainhash.NewHashFromStr(txid)
	txHeight := rand.Uint32()
	blockHash := testAddBlockVerbose(nil, nil, 1, txHeight)
	testAddTxOut(msg.tx, msg.vout, txHash, blockHash, int64(txHeight), maturity)
	coinID := toCoinID(txHash, msg.vout)

	verboseTx := testChain.txRaws[*txHash]
	vout := btcjson.Vout{
		Value: 0.29,
		N:     0,
		//ScriptPubKey
	}
	verboseTx.Vout = append(verboseTx.Vout, vout)

	utxo, err := btc.FundingCoin(context.Background(), coinID, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if utxo.TxID() != txid {
		t.Fatalf("utxo txid doesn't match")
	}

	if utxo.(*UTXO).value != uint64(msg.tx.TxOut[0].Value) {
		t.Errorf("incorrect value. got %d, wanted %d", utxo.(*UTXO).value, msg.tx.TxOut[0].Value)
	}

	voutVal, err := btc.prevOutputValue(txid, 0)
	if err != nil {
		t.Fatalf("prevOutputValue: %v", err)
	}
	if voutVal != uint64(msg.tx.TxOut[0].Value) {
		t.Errorf("incorrect value. got %d, wanted %d", utxo.(*UTXO).value, msg.tx.TxOut[0].Value)
	}
}

// TestCheckAddress checks that addresses are parsing or not parsing as
// expected.
func TestCheckAddress(t *testing.T) {
	btc := &Backend{
		chainParams: &chaincfg.MainNetParams,
	}
	type test struct {
		addr    string
		wantErr bool
	}
	tests := []test{
		{"", true},
		{"18Zpft83eov56iESWuPpV8XFLJ1b8gMZy7", false},                                 // p2pk
		{"3GD2fSQxhkXDAW66i6JBwCqhLFSvhMNrtJ", false},                                 // p2pkh
		{"03aab68960ac1cc2a4151e40c530fcf32284afaed0cebbaec98500c8f3c491d50b", false}, // p2sh
		{"bc1qq3wc0u7x0nezw3hfjkh45ffk09gm4ghl0k7dwe", false},                         // p2wpkh
		{"bc1qdn28r3yr790mjzadkd79sgdkm92jdfq6j5zxsz6w0j9hvwsmr4ys7yn244", false},     // p2wskh
		{"28Zpft83eov56iESWuPpV8XFLJ1b8gMZy7", true},                                  // wrong network
		{"3GD2fSQxhkXDAW66i6JBwCqhLFSvhMNrtO", true},                                  // capital letter O not base 58
		{"3GD2fSQx", true},
	}
	for _, test := range tests {
		if btc.CheckAddress(test.addr) != !test.wantErr {
			t.Fatalf("wantErr = %t, address = %s", test.wantErr, test.addr)
		}
	}
}

func TestDriver_DecodeCoinID(t *testing.T) {
	tests := []struct {
		name    string
		coinID  []byte
		want    string
		wantErr bool
	}{
		{
			"ok",
			[]byte{
				0x16, 0x8f, 0x34, 0x3a, 0xdf, 0x17, 0xe0, 0xc3,
				0xa2, 0xe8, 0x88, 0x79, 0x8, 0x87, 0x17, 0xb8,
				0xac, 0x93, 0x47, 0xb9, 0x66, 0xd, 0xa7, 0x4b,
				0xde, 0x3e, 0x1d, 0x1f, 0x47, 0x94, 0x9f, 0xdf, // 32 byte hash
				0x0, 0x0, 0x0, 0x1, // 4 byte vout
			},
			"df9f94471f1d3ede4ba70d66b94793acb81787087988e8a2c3e017df3a348f16:1",
			false,
		},
		{
			"bad",
			[]byte{
				0x16, 0x8f, 0x34, 0x3a, 0xdf, 0x17, 0xe0, 0xc3,
				0xa2, 0xe8, 0x88, 0x79, 0x8, 0x87, 0x17, 0xb8,
				0xac, 0x93, 0x47, 0xb9, 0x66, 0xd, 0xa7, 0x4b,
				0xde, 0x3e, 0x1d, 0x1f, 0x47, 0x94, 0x9f, // 31 bytes
				0x0, 0x0, 0x0, 0x1,
			},
			"",
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &Driver{}
			got, err := d.DecodeCoinID(tt.coinID)
			if (err != nil) != tt.wantErr {
				t.Errorf("Driver.DecodeCoinID() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("Driver.DecodeCoinID() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSynced(t *testing.T) {
	btc, shutdown := testBackend(true)
	defer shutdown()
	tNode := btc.node.(*testNode)
	tNode.rawResult, _ = json.Marshal(&btcjson.GetBlockChainInfoResult{
		Headers: 100,
		Blocks:  99,
	})
	synced, err := btc.Synced()
	if err != nil {
		t.Fatalf("Synced error: %v", err)
	}
	if !synced {
		t.Fatalf("not synced when should be synced")
	}

	tNode.rawResult, _ = json.Marshal(&btcjson.GetBlockChainInfoResult{
		Headers: 100,
		Blocks:  50,
	})
	synced, err = btc.Synced()
	if err != nil {
		t.Fatalf("Synced error: %v", err)
	}
	if synced {
		t.Fatalf("synced when shouldn't be synced")
	}

	tNode.rawErr = fmt.Errorf("test error")
	_, err = btc.Synced()
	if err == nil {
		t.Fatalf("getblockchaininfo error not propagated")
	}
	tNode.rawErr = nil
}
