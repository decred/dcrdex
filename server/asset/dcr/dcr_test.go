// +build !dcrlive
//
// These tests will not be run if the dcrlive build tag is set.

package dcr

import (
	"context"
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"decred.org/dcrdex/dex"
	dexdcr "decred.org/dcrdex/dex/networks/dcr"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrec"
	"github.com/decred/dcrd/dcrec/edwards/v2"
	"github.com/decred/dcrd/dcrec/secp256k1/v3"
	"github.com/decred/dcrd/dcrec/secp256k1/v3/ecdsa"
	"github.com/decred/dcrd/dcrec/secp256k1/v3/schnorr"
	"github.com/decred/dcrd/dcrutil/v3"
	"github.com/decred/dcrd/hdkeychain/v3"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types/v2"
	"github.com/decred/dcrd/txscript/v3"
	"github.com/decred/dcrd/wire"
	flags "github.com/jessevdk/go-flags"
)

var tLogger = dex.StdOutLogger("TEST", dex.LevelTrace)

func TestMain(m *testing.M) {
	// Set the global chainParams.
	chainParams = chaincfg.MainNetParams()
	os.Exit(m.Run())
}

// TestLoadConfig checks that configuration parsing works as expected.
func TestLoadConfig(t *testing.T) {
	cfg := &config{}
	parsedCfg := &config{}

	tempDir, err := ioutil.TempDir("", "btctest")
	if err != nil {
		t.Fatalf("error creating temporary directory: %v", err)
	}
	defer os.RemoveAll(tempDir)
	filePath := filepath.Join(tempDir, "test.conf")
	rootParser := flags.NewParser(cfg, flags.None)
	iniParser := flags.NewIniParser(rootParser)

	runCfg := func(config *config) error {
		*cfg = *config
		err := iniParser.WriteFile(filePath, flags.IniNone)
		if err != nil {
			return err
		}
		parsedCfg, err = loadConfig(filePath, dex.Mainnet)
		return err
	}

	// Try with just the name. Error expected.
	err = runCfg(&config{
		RPCUser: "somename",
	})
	if err == nil {
		t.Fatalf("no error when just name provided")
	}

	// Try with just the password. Error expected.
	err = runCfg(&config{
		RPCPass: "somepass",
	})
	if err == nil {
		t.Fatalf("no error when just password provided")
	}

	// Give both name and password. This should not be an error.
	err = runCfg(&config{
		RPCUser: "somename",
		RPCPass: "somepass",
	})
	if err != nil {
		t.Fatalf("unexpected error when both name and password provided: %v", err)
	}
	if parsedCfg.RPCListen != defaultMainnet {
		t.Fatalf("unexpected default rpc address. wanted %s, got %s", defaultMainnet, cfg.RPCListen)
	}
	// sanity check for name and password match
	if parsedCfg.RPCUser != cfg.RPCUser {
		t.Fatalf("name mismatch")
	}
	if parsedCfg.RPCPass != cfg.RPCPass {
		t.Fatalf("password mismatch")
	}
	if parsedCfg.RPCCert != defaultRPCCert {
		t.Errorf("RPCCert not set implicitly")
	}
	err = runCfg(&config{
		RPCUser:   "abc",
		RPCPass:   "def",
		RPCListen: "123",
		RPCCert:   "456",
	})
	if err != nil {
		t.Errorf("unexpected error when settings RPCListen/RPCCert explicitly: %v", err)
	}
	if cfg.RPCListen != "123" {
		t.Errorf("RPCListen not set to provided value")
	}
	if cfg.RPCCert != "456" {
		t.Errorf("RPCCert not set to provided value")
	}
}

// The remaining tests use the testBlockchain which is a stub for
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
		fmt.Printf("chainhash.Hash.SetBytes error: %v", err)
	}
	return hash
}

// A fake "blockchain" to be used for RPC calls by the dcrNode.
type testBlockChain struct {
	txOuts map[string]*chainjson.GetTxOutResult
	txRaws map[chainhash.Hash]*chainjson.TxRawResult
	blocks map[chainhash.Hash]*chainjson.GetBlockVerboseResult
	hashes map[int64]*chainhash.Hash
}

// The testChain is a "blockchain" to store RPC responses for the Backend
// node stub to request.
var testChainMtx sync.RWMutex
var testChain testBlockChain

// This must be called before using the testNode, and should be called
// in-between independent tests.
func cleanTestChain() {
	testChainMtx.Lock()
	testChain = testBlockChain{
		txOuts: make(map[string]*chainjson.GetTxOutResult),
		txRaws: make(map[chainhash.Hash]*chainjson.TxRawResult),
		blocks: make(map[chainhash.Hash]*chainjson.GetBlockVerboseResult),
		hashes: make(map[int64]*chainhash.Hash),
	}
	testChainMtx.Unlock()
}

// A stub to replace rpcclient.Client for offline testing.
type testNode struct {
	blockchainInfo    *chainjson.GetBlockChainInfoResult
	blockchainInfoErr error
}

// Store utxo info as a concatenated string hash:vout.
func txOutID(txHash *chainhash.Hash, index uint32) string {
	return txHash.String() + ":" + strconv.Itoa(int(index))
}

const optimalFeeRate uint64 = 22

func (*testNode) EstimateSmartFee(_ context.Context, confirmations int64, mode chainjson.EstimateSmartFeeMode) (float64, error) {
	optimalRate := float64(optimalFeeRate) * 1e-5
	// fmt.Println((float64(optimalFeeRate)*1e-5)-0.00022)
	return optimalRate, nil // optimalFeeRate: 22 atoms/byte = 0.00022 DCR/KB * 1e8 atoms/DCR * 1e-3 KB/Byte
}

// Part of the dcrNode interface.
func (*testNode) GetTxOut(_ context.Context, txHash *chainhash.Hash, index uint32, _ bool) (*chainjson.GetTxOutResult, error) {
	outID := txOutID(txHash, index)
	testChainMtx.RLock()
	defer testChainMtx.RUnlock()
	out := testChain.txOuts[outID]
	// Unfound is not an error for GetTxOut.
	return out, nil
}

// Part of the dcrNode interface.
func (*testNode) GetRawTransactionVerbose(_ context.Context, txHash *chainhash.Hash) (*chainjson.TxRawResult, error) {
	testChainMtx.RLock()
	defer testChainMtx.RUnlock()
	tx, found := testChain.txRaws[*txHash]
	if !found {
		return nil, fmt.Errorf("test transaction not found")
	}
	return tx, nil
}

// Part of the dcrNode interface.
func (*testNode) GetBlockVerbose(_ context.Context, blockHash *chainhash.Hash, verboseTx bool) (*chainjson.GetBlockVerboseResult, error) {
	testChainMtx.RLock()
	defer testChainMtx.RUnlock()
	block, found := testChain.blocks[*blockHash]
	if !found {
		return nil, fmt.Errorf("test block not found")
	}
	return block, nil
}

// Part of the dcrNode interface.
func (*testNode) GetBlockHash(_ context.Context, blockHeight int64) (*chainhash.Hash, error) {
	testChainMtx.RLock()
	defer testChainMtx.RUnlock()
	hash, found := testChain.hashes[blockHeight]
	if !found {
		return nil, fmt.Errorf("test hash not found")
	}
	return hash, nil
}

// Part of the dcrNode interface.
func (*testNode) GetBestBlockHash(context.Context) (*chainhash.Hash, error) {
	testChainMtx.RLock()
	defer testChainMtx.RUnlock()
	if len(testChain.hashes) == 0 {
		return nil, fmt.Errorf("no blocks in testChain")
	}
	var bestHeight int64
	for height := range testChain.hashes {
		if height > bestHeight {
			bestHeight = height
		}
	}
	return testChain.hashes[bestHeight], nil
}

func (t *testNode) GetBlockChainInfo(context.Context) (*chainjson.GetBlockChainInfoResult, error) {
	if t.blockchainInfoErr != nil {
		return nil, t.blockchainInfoErr
	}
	return t.blockchainInfo, nil
}

// Create a chainjson.GetTxOutResult such as is returned from GetTxOut.
func testGetTxOut(confirmations int64, pkScript []byte) *chainjson.GetTxOutResult {
	return &chainjson.GetTxOutResult{
		Confirmations: confirmations,
		ScriptPubKey: chainjson.ScriptPubKeyResult{
			Hex: hex.EncodeToString(pkScript),
		},
	}
}

// Create a *chainjson.TxRawResult such as is returned by
// GetRawTransactionVerbose.
func testRawTransactionVerbose(msgTx *wire.MsgTx, txid, blockHash *chainhash.Hash, blockHeight,
	confirmations int64) *chainjson.TxRawResult {

	var hash string
	if blockHash != nil {
		hash = blockHash.String()
	}
	hexTx, err := msgTx.Bytes()
	if err != nil {
		fmt.Printf("error encoding MsgTx")
	}

	return &chainjson.TxRawResult{
		Hex:           hex.EncodeToString(hexTx),
		Txid:          txid.String(),
		BlockHash:     hash,
		BlockHeight:   blockHeight,
		Confirmations: confirmations,
	}
}

// Add a transaction output and it's getrawtransaction data.
func testAddTxOut(msgTx *wire.MsgTx, vout uint32, txHash, blockHash *chainhash.Hash, blockHeight, confirmations int64) *chainjson.GetTxOutResult {
	txOut := testGetTxOut(confirmations, msgTx.TxOut[vout].PkScript)
	testChainMtx.Lock()
	testChain.txOuts[txOutID(txHash, vout)] = txOut
	testChainMtx.Unlock()
	testAddTxVerbose(msgTx, txHash, blockHash, blockHeight, confirmations)
	return txOut
}

// Add a chainjson.TxRawResult to the blockchain.
func testAddTxVerbose(msgTx *wire.MsgTx, txHash, blockHash *chainhash.Hash, blockHeight, confirmations int64) *chainjson.TxRawResult {
	tx := testRawTransactionVerbose(msgTx, txHash, blockHash, blockHeight, confirmations)
	testChainMtx.Lock()
	defer testChainMtx.Unlock()
	testChain.txRaws[*txHash] = tx
	return tx
}

// Add a GetBlockVerboseResult to the blockchain.
func testAddBlockVerbose(blockHash *chainhash.Hash, confirmations int64, height uint32, voteBits uint16) *chainhash.Hash {
	if blockHash == nil {
		blockHash = randomHash()
	}
	testChainMtx.Lock()
	defer testChainMtx.Unlock()
	if voteBits&1 != 0 {
		testChain.hashes[int64(height)] = blockHash
	}
	testChain.blocks[*blockHash] = &chainjson.GetBlockVerboseResult{
		Hash:          blockHash.String(),
		Confirmations: confirmations,
		Height:        int64(height),
		VoteBits:      voteBits,
	}
	return blockHash
}

// An element of the TxRawResult vout array.
func testVout(value float64, pkScript []byte) chainjson.Vout {
	return chainjson.Vout{
		Value: value,
		ScriptPubKey: chainjson.ScriptPubKeyResult{
			Hex: hex.EncodeToString(pkScript),
		},
	}
}

// An element of the TxRawResult vin array.
func testVin(txHash *chainhash.Hash, vout uint32) chainjson.Vin {
	return chainjson.Vin{
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
	tx     *wire.MsgTx
	auth   *testAuth
	vout   uint32
	script []byte
}

// Generate a public key on the secp256k1 curve.
func genPubkey() ([]byte, []byte) {
	priv := secp256k1.PrivKeyFromBytes(randomBytes(32))
	pub := priv.PubKey()
	pubkey := pub.SerializeCompressed()
	pkHash := dcrutil.Hash160(pubkey)
	return pubkey, pkHash
}

func s256Auth(msg []byte) *testAuth {
	priv, err := secp256k1.GeneratePrivateKey()
	if err != nil {
		fmt.Printf("s256Auth error: %v\n", err)
	}
	pubkey := priv.PubKey().SerializeCompressed()
	if msg == nil {
		msg = randomBytes(32)
	}
	sig := ecdsa.Sign(priv, msg)
	return &testAuth{
		pubkey: pubkey,
		pkHash: dcrutil.Hash160(pubkey),
		msg:    msg,
		sig:    sig.Serialize(),
	}
}

func edwardsAuth(msg []byte) *testAuth {
	priv, err := edwards.GeneratePrivateKey()
	if err != nil {
		fmt.Printf("edwardsAuth error: %v\n", err)
	}
	pubkey := priv.PubKey().Serialize()
	if msg == nil {
		msg = randomBytes(32)
	}
	sig, err := priv.Sign(msg)
	if err != nil {
		fmt.Printf("edwardsAuth sign error: %v\n", err)
	}
	return &testAuth{
		pubkey: pubkey,
		pkHash: dcrutil.Hash160(pubkey),
		msg:    msg,
		sig:    sig.Serialize(),
	}
}

func schnorrAuth(msg []byte) *testAuth {
	priv, err := secp256k1.GeneratePrivateKey()
	if err != nil {
		fmt.Printf("schnorrAuth error: %v\n", err)
	}
	pubkey := priv.PubKey().SerializeCompressed()
	if msg == nil {
		msg = randomBytes(32)
	}
	sig, err := schnorr.Sign(priv, msg)
	if err != nil {
		fmt.Printf("schnorrAuth sign error: %v\n", err)
	}
	return &testAuth{
		pubkey: pubkey,
		pkHash: dcrutil.Hash160(pubkey),
		msg:    msg,
		sig:    sig.Serialize(),
	}
}

// A pay-to-script-hash pubkey script.
func newP2PKHScript(sigType dcrec.SignatureType) ([]byte, *testAuth) {
	var auth *testAuth
	switch sigType {
	case dcrec.STEcdsaSecp256k1:
		auth = s256Auth(nil)
	case dcrec.STEd25519:
		auth = edwardsAuth(nil)
	case dcrec.STSchnorrSecp256k1:
		auth = schnorrAuth(nil)
	default:
		fmt.Printf("NewAddressPubKeyHash unknown sigType")
	}
	addr, err := dcrutil.NewAddressPubKeyHash(auth.pkHash, chainParams, sigType)
	if err != nil {
		fmt.Printf("NewAddressPubKeyHash error: %v\n", err)
		return nil, nil
	}
	pkScript, err := txscript.PayToAddrScript(addr)
	if err != nil {
		fmt.Printf("PayToAddrScript error: %v\n", err)
	}
	return pkScript, auth
}

// A pay-to-script-hash pubkey script, with a prepended stake-tree indicator
// byte.
func newStakeP2PKHScript(opcode byte) ([]byte, *testAuth) {
	script, auth := newP2PKHScript(dcrec.STEcdsaSecp256k1)
	stakeScript := make([]byte, 0, len(script)+1)
	stakeScript = append(stakeScript, opcode)
	stakeScript = append(stakeScript, script...)
	return stakeScript, auth
}

// A MsgTx for a regular transaction with a single output. No inputs, so it's
// not really a valid transaction, but that's okay on testBlockchain and
// irrelevant to Backend.
func testMsgTxRegular(sigType dcrec.SignatureType) *testMsgTx {
	pkScript, auth := newP2PKHScript(sigType)
	msgTx := wire.NewMsgTx()
	msgTx.AddTxOut(wire.NewTxOut(1, pkScript))
	return &testMsgTx{
		tx:   msgTx,
		auth: auth,
	}
}

// Information about a swap contract.
type testMsgTxSwap struct {
	tx        *wire.MsgTx
	contract  []byte
	recipient dcrutil.Address
	refund    dcrutil.Address
}

// Create a swap (initialization) contract with random pubkeys and return the
// pubkey script and addresses.
func testSwapContract() ([]byte, dcrutil.Address, dcrutil.Address) {
	lockTime := time.Now().Add(time.Hour * 8).Unix()
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
	receiverAddr, _ := dcrutil.NewAddressPubKeyHash(receiverPKH, chainParams, dcrec.STEcdsaSecp256k1)
	refundAddr, _ := dcrutil.NewAddressPubKeyHash(senderPKH, chainParams, dcrec.STEcdsaSecp256k1)
	return contract, receiverAddr, refundAddr
}

// Create a transaction with a P2SH swap output at vout 0.
func testMsgTxSwapInit(val int64) *testMsgTxSwap {
	msgTx := wire.NewMsgTx()
	contract, recipient, refund := testSwapContract()
	scriptHash := dcrutil.Hash160(contract)
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

// A MsgTx for a vote. Votes have a stricter set of requirements to pass
// txscript.IsSSGen, so some extra inputs and outputs must be constructed.
func testMsgTxVote() *testMsgTx {
	msgTx := wire.NewMsgTx()
	stakeBase := wire.NewTxIn(wire.NewOutPoint(&zeroHash, math.MaxUint32, 0), 0, nil)
	stakeBase.BlockHeight = wire.NullBlockHeight
	stakeBase.BlockIndex = wire.NullBlockIndex
	msgTx.AddTxIn(stakeBase)
	// Second outpoint needs to be stake tree
	msgTx.AddTxIn(wire.NewTxIn(wire.NewOutPoint(&zeroHash, 0, 1), 0, nil))
	// First output must have OP_RETURN script
	script1, err := txscript.NewScriptBuilder().
		AddOp(txscript.OP_RETURN).AddData(randomBytes(36)).Script()
	if err != nil {
		fmt.Printf("script1 building error in testMsgTxVote: %v", err)
	}
	msgTx.AddTxOut(wire.NewTxOut(0, script1))
	// output 2
	script2, err := txscript.NewScriptBuilder().
		AddOp(txscript.OP_RETURN).AddData(randomBytes(2)).Script()
	if err != nil {
		fmt.Printf("script2 building error in testMsgTxVote: %v", err)
	}
	msgTx.AddTxOut(wire.NewTxOut(1, script2))
	// Now just a P2PKH script with a prepended OP_SSGEN
	script3, auth := newStakeP2PKHScript(txscript.OP_SSGEN)
	msgTx.AddTxOut(wire.NewTxOut(2, script3))
	return &testMsgTx{
		tx:   msgTx,
		auth: auth,
		vout: 2,
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

func multiSigScript(pubkeys []*dcrutil.AddressSecpPubKey, numReq int64) ([]byte, error) {
	builder := txscript.NewScriptBuilder().AddInt64(numReq)
	for _, key := range pubkeys {
		builder.AddData(key.ScriptAddress())
	}
	builder.AddInt64(int64(len(pubkeys))).AddOp(txscript.OP_CHECKMULTISIG)
	return builder.Script()
}

// An M-of-N mutli-sig script. This script is pay-to-pubkey.
func testMultiSigScriptMofN(m, n int) ([]byte, *testMultiSigAuth) {
	// serialized compressed pubkey used for multisig
	addrs := make([]*dcrutil.AddressSecpPubKey, 0, n)
	auth := &testMultiSigAuth{
		msg: randomBytes(32),
	}

	for i := 0; i < m; i++ {
		a := s256Auth(auth.msg)
		auth.pubkeys = append(auth.pubkeys, a.pubkey)
		auth.pkHashes = append(auth.pkHashes, a.pkHash)
		auth.sigs = append(auth.sigs, a.sig)
		addr, err := dcrutil.NewAddressSecpPubKey(a.pubkey, chainParams)
		if err != nil {
			fmt.Printf("error creating AddressSecpPubKey: %v", err)
			return nil, nil
		}
		addrs = append(addrs, addr)
	}
	script, err := multiSigScript(addrs, int64(m))
	if err != nil {
		fmt.Printf("error creating MultiSigScript: %v", err)
		return nil, nil
	}
	return script, auth
}

// A pay-to-script-hash M-of-N multi-sig MsgTx.
func testMsgTxP2SHMofN(m, n int) *testMsgTxP2SH {
	script, auth := testMultiSigScriptMofN(m, n)
	pkScript := make([]byte, 0, 23)
	pkScript = append(pkScript, txscript.OP_HASH160)
	pkScript = append(pkScript, txscript.OP_DATA_20)
	scriptHash := dcrutil.Hash160(script)
	pkScript = append(pkScript, scriptHash...)
	pkScript = append(pkScript, txscript.OP_EQUAL)
	msgTx := wire.NewMsgTx()
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

// A pay-to-script hash with a P2SH output. I'm fairly certain this would be an
// uncommon choice in practice, but valid nonetheless.
func testMsgTxP2SHVote() *testMsgTx {
	// Need to pull a little switcharoo, taking the pk script as the redeem script
	// and subbing in a p2sh script.
	msg := testMsgTxVote()
	ogScript := msg.tx.TxOut[msg.vout].PkScript
	pkScript := make([]byte, 0, 24)
	pkScript = append(pkScript, txscript.OP_SSGEN)
	pkScript = append(pkScript, txscript.OP_HASH160)
	pkScript = append(pkScript, txscript.OP_DATA_20)
	scriptHash := dcrutil.Hash160(ogScript)
	pkScript = append(pkScript, scriptHash...)
	pkScript = append(pkScript, txscript.OP_EQUAL)
	msg.tx.TxOut[msg.vout].PkScript = pkScript
	msg.script = ogScript
	return msg
}

// A revocation MsgTx.
func testMsgTxRevocation() *testMsgTx {
	msgTx := wire.NewMsgTx()
	// Need a single input from stake tree
	msgTx.AddTxIn(wire.NewTxIn(wire.NewOutPoint(&zeroHash, 0, 1), 0, nil))
	// All outputs must have OP_SSRTX prefix.
	script, auth := newStakeP2PKHScript(txscript.OP_SSRTX)
	msgTx.AddTxOut(wire.NewTxOut(1, script))
	return &testMsgTx{
		tx:   msgTx,
		auth: auth,
	}
}

// Make a backend that logs to stdout.
func testBackend() (*Backend, func()) {
	dcr := unconnectedDCR(tLogger, nil) // never actually Connect, so no rpc config
	dcr.node = &testNode{}

	ctx, cancel := context.WithCancel(context.Background())
	dcr.ctx = ctx

	var wg sync.WaitGroup
	shutdown := func() {
		cancel()
		wg.Wait()
	}
	wg.Add(1)
	go func() {
		dcr.run(ctx)
		wg.Done()
	}()
	return dcr, shutdown
}

// TestUTXOs tests all UTXO related paths.
func TestUTXOs(t *testing.T) {
	// The various UTXO types to check:
	// 1. A valid UTXO in a mempool transaction
	// 2. A valid UTXO in a mined block. All three signature types
	// 3. A UTXO that is invalid because it is non-existent. This case covers
	//    other important cases, as dcrd will only return a result from
	//    GetTxOut if the utxo is valid and ready to spend.
	// 4. A UTXO that is invalid because it has the wrong script type
	// 5. A UTXO that is invalidated because it is a regular tree tx from a
	//    stakeholder invalidated block
	// 6. A UTXO that is valid even though it is from a stakeholder invalidated
	//    block, because it is a stake tree tx
	// 7. A UTXO that becomes invalid in a reorg
	// 8. A UTXO that is in an orphaned block, but also included in a new
	//     mainchain block, so is still valid.
	// 9. A UTXO with a pay-to-script-hash for a 1-of-2 multisig redeem script
	// 10. A UTXO with a pay-to-script-hash for a 2-of-2 multisig redeem script
	// 11. A UTXO with a pay-to-script-hash for a P2PKH redeem script.
	// 12. A revocation.

	// Create a Backend with the test node.
	dcr, shutdown := testBackend()
	defer shutdown()
	ctx := dcr.ctx // for the functions then take a context, canceled on shutdown()

	// The vout will be randomized during reset.
	txHeight := uint32(32)

	// A general reset function that clears the testBlockchain and the blockCache.
	reset := func() {
		cleanTestChain()
		blockCache := newBlockCache(dcr.log)
		dcr.blockCache.mtx.Lock()
		dcr.blockCache.blocks = blockCache.blocks
		dcr.blockCache.mainchain = blockCache.mainchain
		dcr.blockCache.best = blockCache.best
		dcr.blockCache.mtx.Unlock()
	}

	// CASE 1: A valid UTXO in a mempool transaction. This UTXO will have zero
	// confirmations, a valid pkScript and will not be marked as coinbase. Then
	// add a block that includes the transaction, and check that Confirmations
	// updates correctly.
	reset()
	txHash := randomHash()
	blockHash := randomHash()
	msg := testMsgTxRegular(dcrec.STEcdsaSecp256k1)
	// For a regular test tx, the output is at output index 0. Pass nil for the
	// block hash and 0 for the block height and confirmations for a mempool tx.
	txout := testAddTxOut(msg.tx, msg.vout, txHash, nil, 0, 0)
	// Set the value of this one.
	txout.Value = 2.0
	// There is no block info to add, since this is a mempool transaction
	utxo, err := dcr.utxo(ctx, txHash, msg.vout, nil)
	if err != nil {
		t.Fatalf("case 1 - unexpected error: %v", err)
	}
	// While we're here, check the spend size and value are correct.
	spendSize := utxo.SpendSize()
	wantSpendSize := uint32(dexdcr.TxInOverhead + 1 + dexdcr.P2PKHSigScriptSize)
	if spendSize != wantSpendSize {
		t.Fatalf("case 1 - unexpected spend script size reported. expected %d, got %d", wantSpendSize, spendSize)
	}
	if utxo.Value() != 200_000_000 {
		t.Fatalf("case 1 - unexpected output value. expected 200,000,000, got %d", utxo.Value())
	}
	// Now "mine" the transaction.
	testAddBlockVerbose(blockHash, 1, txHeight, 1)
	// Overwrite the test blockchain transaction details.
	testAddTxOut(msg.tx, 0, txHash, blockHash, int64(txHeight), 1)
	confs, err := utxo.Confirmations(ctx)
	if err != nil {
		t.Fatalf("case 1 - error retrieving confirmations after transaction \"mined\": %v", err)
	}
	if confs != 1 {
		// The confirmation count is not taken from the txOut.Confirmations, so
		// need to check that it is correct.
		t.Fatalf("case 1 - expected 1 confirmation after mining transaction, found %d", confs)
	}
	// Make sure the pubkey spends the output.
	err = utxo.Auth([][]byte{msg.auth.pubkey}, [][]byte{msg.auth.sig}, msg.auth.msg)
	if err != nil {
		t.Fatalf("case 1 - Auth error: %v", err)
	}

	// CASE 2: A valid UTXO in a mined block. This UTXO will have non-zero
	// confirmations, a valid pkScipt. Test all three signature types.
	for _, sigType := range []dcrec.SignatureType{dcrec.STEcdsaSecp256k1, dcrec.STEd25519, dcrec.STSchnorrSecp256k1} {
		reset()
		blockHash = testAddBlockVerbose(nil, 1, txHeight, 1)
		txHash = randomHash()
		msg = testMsgTxRegular(sigType)
		testAddTxOut(msg.tx, msg.vout, txHash, blockHash, int64(txHeight), 1)
		utxo, err = dcr.utxo(ctx, txHash, msg.vout, nil)
		if err != nil {
			t.Fatalf("case 2 - unexpected error for sig type %d: %v", int(sigType), err)
		}
		err = utxo.Auth([][]byte{msg.auth.pubkey}, [][]byte{msg.auth.sig}, msg.auth.msg)
		if err != nil {
			t.Fatalf("case 2 - Auth error with sig type %d: %v", int(sigType), err)
		}
	}

	// CASE 3: A UTXO that is invalid because it is non-existent
	reset()
	_, err = dcr.utxo(ctx, randomHash(), 0, nil)
	if err == nil {
		t.Fatalf("case 3 - received no error for a non-existent UTXO")
	}

	// CASE 4: A UTXO that is invalid because it has the wrong script type.
	reset()
	blockHash = testAddBlockVerbose(nil, 1, txHeight, 1)
	txHash = randomHash()
	msg = testMsgTxRegular(dcrec.STEcdsaSecp256k1)
	// make the script nonsense.
	msg.tx.TxOut[0].PkScript = []byte{0x00, 0x01, 0x02, 0x03}
	testAddTxOut(msg.tx, msg.vout, txHash, blockHash, int64(txHeight), 1)
	_, err = dcr.utxo(ctx, txHash, msg.vout, nil)
	if err == nil {
		t.Fatalf("case 4 - received no error for a UTXO with wrong script type")
	}

	// CASE 5: A UTXO that is invalid because it is a regular tree tx from a
	// stakeholder invalidated block. The transaction is valid when it has 1
	// confirmation, but is invalidated by the next block.
	reset()
	blockHash = testAddBlockVerbose(nil, 1, txHeight, 1)
	txHash = randomHash()
	msg = testMsgTxRegular(dcrec.STEcdsaSecp256k1)
	testAddTxOut(msg.tx, msg.vout, txHash, blockHash, int64(txHeight), 1)
	utxo, err = dcr.utxo(ctx, txHash, msg.vout, nil)
	if err != nil {
		t.Fatalf("case 5 - unexpected error: %v", err)
	}
	// Now reject the block. First update the confirmations.
	testAddBlockVerbose(blockHash, 2, txHeight, 1)
	rejectingHash := testAddBlockVerbose(nil, 1, txHeight+1, 0)
	rejectingBlock := testChain.blocks[*rejectingHash]
	_, err = dcr.blockCache.add(rejectingBlock)
	if err != nil {
		t.Fatalf("case 5 - error adding to block cache: %v", err)
	}
	_, err = utxo.Confirmations(ctx)
	if err == nil {
		t.Fatalf("case 5 - block not detected as invalid after stakeholder invalidation")
	}

	// CASE 6: A UTXO that is valid even though it is from a stakeholder
	// invalidated block, because it is a stake tree tx. First try with an
	// immature vote output, then add a maturing block and try again.
	reset()
	txHash = randomHash()
	immatureHash := testAddBlockVerbose(nil, 2, txHeight, 1)
	msg = testMsgTxVote()
	testAddTxOut(msg.tx, msg.vout, txHash, immatureHash, int64(txHeight), 1)
	_, err = dcr.utxo(ctx, txHash, msg.vout, nil)
	if err == nil {
		t.Fatalf("case 6 - no error for immature transaction")
	}
	// Now reject the block, but mature the transaction. It should still be
	// accepted since it is a stake tree transaction.
	rejectingHash = testAddBlockVerbose(nil, 1, txHeight+1, 0)
	rejectingBlock = testChain.blocks[*rejectingHash]
	_, err = dcr.blockCache.add(rejectingBlock)
	if err != nil {
		t.Fatalf("case 6 - error adding to rejecting block cache: %v", err)
	}
	maturity := int64(chainParams.CoinbaseMaturity)
	testAddBlockVerbose(blockHash, maturity, txHeight, 1)
	testAddBlockVerbose(rejectingHash, maturity-1, txHeight, 1)
	maturingHash := testAddBlockVerbose(nil, 1, txHeight+uint32(maturity)-1, 1)
	maturingBlock := testChain.blocks[*maturingHash]
	_, err = dcr.blockCache.add(maturingBlock)
	if err != nil {
		t.Fatalf("case 6 - error adding to maturing block cache: %v", err)
	}
	testAddTxOut(msg.tx, msg.vout, txHash, immatureHash, int64(txHeight), int64(txHeight)+maturity-1)
	utxo, err = dcr.utxo(ctx, txHash, msg.vout, nil)
	if err != nil {
		t.Fatalf("case 6 - unexpected error after maturing block: %v", err)
	}
	// Since this is our first stake transaction, let's check the pubkey
	err = utxo.Auth([][]byte{msg.auth.pubkey}, [][]byte{msg.auth.sig}, msg.auth.msg)
	if err != nil {
		t.Fatalf("case 6 - Auth error: %v", err)
	}

	// CASE 7: A UTXO that becomes invalid in a reorg
	reset()
	txHash = randomHash()
	blockHash = testAddBlockVerbose(nil, 1, txHeight, 1)
	msg = testMsgTxRegular(dcrec.STEcdsaSecp256k1)
	testAddTxOut(msg.tx, msg.vout, txHash, blockHash, int64(txHeight), 1)
	utxo, err = dcr.utxo(ctx, txHash, msg.vout, nil)
	if err != nil {
		t.Fatalf("case 7 - received error for utxo")
	}
	_, err = utxo.Confirmations(ctx)
	if err != nil {
		t.Fatalf("case 7 - received error before reorg")
	}
	betterHash := testAddBlockVerbose(nil, 1, txHeight, 1)
	dcr.blockCache.add(testChain.blocks[*betterHash])
	dcr.blockCache.reorg(int64(txHeight))
	// Remove the txout from the blockchain, since dcrd would no longer return it.
	testChainMtx.Lock()
	delete(testChain.txOuts, txOutID(txHash, msg.vout))
	testChainMtx.Unlock()
	_, err = utxo.Confirmations(ctx)
	if err == nil {
		t.Fatalf("case 7 - received no error for orphaned transaction")
	}

	// CASE 8: A UTXO that is in an orphaned block, but also included in a new
	// mainchain block, so is still valid.
	reset()
	txHash = randomHash()
	orphanHash := testAddBlockVerbose(nil, 1, txHeight, 1)
	msg = testMsgTxRegular(dcrec.STEcdsaSecp256k1)
	testAddTxOut(msg.tx, msg.vout, txHash, orphanHash, int64(txHeight), 1)
	utxo, err = dcr.utxo(ctx, txHash, msg.vout, nil)
	if err != nil {
		t.Fatalf("case 8 - received error for utxo")
	}
	// Now orphan the block, by doing a reorg.
	betterHash = testAddBlockVerbose(nil, 1, txHeight, 1)
	dcr.blockCache.reorg(int64(txHeight))
	dcr.blockCache.add(testChain.blocks[*betterHash])
	testAddTxOut(msg.tx, msg.vout, txHash, betterHash, int64(txHeight), 1)
	_, err = utxo.Confirmations(ctx)
	if err != nil {
		t.Fatalf("case 8 - unexpected error after reorg: %v", err)
	}
	if utxo.blockHash != *betterHash {
		t.Fatalf("case 8 - unexpected hash for utxo after reorg")
	}
	// Do it again, but this time, put the utxo into mempool.
	evenBetter := testAddBlockVerbose(nil, 1, txHeight, 1)
	dcr.blockCache.reorg(int64(txHeight))
	dcr.blockCache.add(testChain.blocks[*evenBetter])
	testAddTxOut(msg.tx, msg.vout, txHash, evenBetter, 0, 0)
	_, err = utxo.Confirmations(ctx)
	if err != nil {
		t.Fatalf("case 8 - unexpected error for mempool tx after reorg")
	}
	if utxo.height != 0 {
		t.Fatalf("case 8 - unexpected height %d after dumping into mempool", utxo.height)
	}

	// CASE 9: A UTXO with a pay-to-script-hash for a 1-of-2 multisig redeem
	// script
	reset()
	txHash = randomHash()
	blockHash = testAddBlockVerbose(nil, 1, txHeight, 1)
	msgMultiSig := testMsgTxP2SHMofN(1, 2)
	testAddTxOut(msgMultiSig.tx, msgMultiSig.vout, txHash, blockHash, int64(txHeight), 1)
	// First try to get the UTXO without providing the raw script.
	_, err = dcr.utxo(ctx, txHash, msgMultiSig.vout, nil)
	if err == nil {
		t.Fatalf("no error thrown for p2sh utxo when no script was provided")
	}
	// Now provide the script.
	utxo, err = dcr.utxo(ctx, txHash, msgMultiSig.vout, msgMultiSig.script)
	if err != nil {
		t.Fatalf("case 9 - received error for utxo: %v", err)
	}
	confs, err = utxo.Confirmations(ctx)
	if err != nil {
		t.Fatalf("case 9 - error getting confirmations: %v", err)
	}
	if confs != 1 {
		t.Fatalf("case 9 - expected 1 confirmation, got %d", confs)
	}
	err = utxo.Auth(msgMultiSig.auth.pubkeys[:1], msgMultiSig.auth.sigs[:1], msgMultiSig.auth.msg)
	if err != nil {
		t.Fatalf("case 9 - Auth error: %v", err)
	}

	// CASE 10: A UTXO with a pay-to-script-hash for a 2-of-2 multisig redeem
	// script
	reset()
	txHash = randomHash()
	blockHash = testAddBlockVerbose(nil, 1, txHeight, 1)
	msgMultiSig = testMsgTxP2SHMofN(2, 2)
	testAddTxOut(msgMultiSig.tx, msgMultiSig.vout, txHash, blockHash, int64(txHeight), 1)
	utxo, err = dcr.utxo(ctx, txHash, msgMultiSig.vout, msgMultiSig.script)
	if err != nil {
		t.Fatalf("case 10 - received error for utxo: %v", err)
	}
	// Try to get by with just one of the pubkeys.
	err = utxo.Auth(msgMultiSig.auth.pubkeys[:1], msgMultiSig.auth.sigs[:1], msgMultiSig.auth.msg)
	if err == nil {
		t.Fatalf("case 10 - no Auth error when only provided one of two required pubkeys")
	}
	// Now do both.
	err = utxo.Auth(msgMultiSig.auth.pubkeys, msgMultiSig.auth.sigs, msgMultiSig.auth.msg)
	if err != nil {
		t.Fatalf("case 10 - Auth error: %v", err)
	}
	// Try with a duplicate pubkey and signature.
	dupeKeys := [][]byte{msgMultiSig.auth.pubkeys[0], msgMultiSig.auth.pubkeys[0]}
	dupeSigs := [][]byte{msgMultiSig.auth.sigs[0], msgMultiSig.auth.sigs[0]}
	err = utxo.Auth(dupeKeys, dupeSigs, msgMultiSig.auth.msg)
	if err == nil {
		t.Fatalf("case 10 - no Auth error with duplicate keys/sigs")
	}

	// CASE 11: A UTXO with a pay-to-script-hash for a P2PKH redeem script.
	reset()
	txHash = randomHash()
	blockHash = testAddBlockVerbose(nil, maturity, txHeight, 1)
	msg = testMsgTxP2SHVote()
	testAddTxOut(msg.tx, msg.vout, txHash, blockHash, int64(txHeight), maturity)
	// mature the vote
	testAddBlockVerbose(nil, 1, txHeight+uint32(maturity)-1, 1)
	utxo, err = dcr.utxo(ctx, txHash, msg.vout, msg.script)
	if err != nil {
		t.Fatalf("case 11 - received error for utxo: %v", err)
	}
	// Make sure it's marked as stake.
	if !utxo.scriptType.IsStake() {
		t.Fatalf("case 11 - stake p2sh not marked as stake")
	}
	// Give it nonsense.
	err = utxo.Auth([][]byte{randomBytes(33)}, [][]byte{randomBytes(33)}, randomBytes(32))
	if err == nil {
		t.Fatalf("case 11 - no Auth error when providing nonsense pubkey")
	}
	// Now give it the right one.
	err = utxo.Auth([][]byte{msg.auth.pubkey}, [][]byte{msg.auth.sig}, msg.auth.msg)
	if err != nil {
		t.Fatalf("case 11 - Auth error: %v", err)
	}

	// CASE 12: A revocation.
	reset()
	txHash = randomHash()
	blockHash = testAddBlockVerbose(nil, maturity, txHeight, 1)
	msg = testMsgTxRevocation()
	testAddTxOut(msg.tx, msg.vout, txHash, blockHash, int64(txHeight), maturity)
	// mature the revocation
	testAddBlockVerbose(nil, 1, txHeight+uint32(maturity)-1, 1)
	utxo, err = dcr.utxo(ctx, txHash, msg.vout, msg.script)
	if err != nil {
		t.Fatalf("case 12 - received error for utxo: %v", err)
	}
	// Make sure it's marked as stake.
	if !utxo.scriptType.IsStake() {
		t.Fatalf("case 12 - stake p2sh not marked as stake")
	}
	// Check the pubkey.
	err = utxo.Auth([][]byte{msg.auth.pubkey}, [][]byte{msg.auth.sig}, msg.auth.msg)
	if err != nil {
		t.Fatalf("case 12 - Auth error: %v", err)
	}

	// CASE 13: A swap contract
	val := uint64(5)
	cleanTestChain()
	txHash = randomHash()
	blockHash = randomHash()
	swap := testMsgTxSwapInit(int64(val))
	testAddBlockVerbose(blockHash, 1, txHeight, 1)
	testAddTxOut(swap.tx, 0, txHash, blockHash, int64(txHeight), 1).Value = float64(val) / 1e8
	verboseTx := testChain.txRaws[*txHash]
	spentTx := randomHash()
	spentVout := rand.Uint32()
	verboseTx.Vin = append(verboseTx.Vin, testVin(spentTx, spentVout))
	txOut := swap.tx.TxOut[0]
	verboseTx.Vout = append(verboseTx.Vout, testVout(float64(txOut.Value)/1e8, txOut.PkScript))
	utxo, err = dcr.utxo(ctx, txHash, 0, swap.contract)
	if err != nil {
		t.Fatalf("case 13 - received error for utxo: %v", err)
	}

	contract := &Contract{Output: utxo.Output}

	// Now try again with the correct vout.
	err = contract.auditContract() // sets refund and swap addresses
	if err != nil {
		t.Fatalf("case 13 - unexpected error auditing contract: %v", err)
	}
	if contract.SwapAddress() != swap.recipient.String() {
		t.Fatalf("case 13 - wrong recipient. wanted '%s' got '%s'", contract.SwapAddress(), swap.recipient.String())
	}
	if contract.RefundAddress() != swap.refund.String() {
		t.Fatalf("case 13 - wrong recipient. wanted '%s' got '%s'", contract.RefundAddress(), swap.refund.String())
	}
	if contract.Value() != val {
		t.Fatalf("case 13 - unexpected output value. wanted 5, got %d", contract.Value())
	}

}

func TestRedemption(t *testing.T) {
	dcr, shutdown := testBackend()
	defer shutdown()
	ctx := dcr.ctx // for the functions then take a context, canceled on shutdown()

	// The vout will be randomized during reset.
	txHeight := uint32(32)
	cleanTestChain()
	txHash := randomHash()
	redemptionID := toCoinID(txHash, 0)
	// blockHash := randomHash()
	spentHash := randomHash()
	spentVout := uint32(5)
	spentID := toCoinID(spentHash, spentVout)
	msg := testMsgTxRegular(dcrec.STEcdsaSecp256k1)
	vin := chainjson.Vin{
		Txid: spentHash.String(),
		Vout: spentVout,
	}

	// A valid mempool redemption.
	verboseTx := testAddTxVerbose(msg.tx, txHash, nil, 0, 0)
	verboseTx.Vin = append(verboseTx.Vin, vin)
	redemption, err := dcr.Redemption(redemptionID, spentID)
	if err != nil {
		t.Fatalf("Redemption error: %v", err)
	}
	confs, err := redemption.Confirmations(ctx)
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
	_, err = dcr.Redemption(redemptionID, spentID)
	if err == nil {
		t.Fatalf("No error for missing transaction")
	}

	// Doesn't spend transaction.
	verboseTx = testAddTxVerbose(msg.tx, txHash, nil, 0, 0)
	verboseTx.Vin = append(verboseTx.Vin, chainjson.Vin{
		Txid: randomHash().String(),
	})
	_, err = dcr.Redemption(redemptionID, spentID)
	if err == nil {
		t.Fatalf("No error for wrong previous outpoint")
	}

	// Mined transaction.
	blockHash := randomHash()
	blockHeight := txHeight - 1
	verboseTx = testAddTxVerbose(msg.tx, txHash, blockHash, int64(blockHeight), 1)
	verboseTx.Vin = append(verboseTx.Vin, vin)
	testAddBlockVerbose(blockHash, 1, blockHeight, 1)
	redemption, err = dcr.Redemption(redemptionID, spentID)
	if err != nil {
		t.Fatalf("Redemption with confs error: %v", err)
	}
	confs, err = redemption.Confirmations(ctx)
	if err != nil {
		t.Fatalf("redemption with confs Confirmations error: %v", err)
	}
	if confs != 1 {
		t.Fatalf("expected 1 confirmation, got %d", confs)
	}
}

// TestReorg sends a reorganization-causing block through the anyQ channel, and
// checks that the cache is responding as expected.
func TestReorg(t *testing.T) {
	// Create a Backend with the test node.
	dcr, shutdown := testBackend()
	defer shutdown()
	ctx := dcr.ctx // for the functions then take a context, canceled on shutdown()

	// A general reset function that clears the testBlockchain and the blockCache.
	var tipHeight uint32 = 10
	var tipHash *chainhash.Hash
	reset := func() {
		cleanTestChain()
		dcr.blockCache = newBlockCache(dcr.log)
		for h := uint32(0); h <= tipHeight; h++ {
			blockHash := testAddBlockVerbose(nil, int64(tipHeight-h+1), h, 1)
			// force dcr to get and cache the block
			_, err := dcr.getDcrBlock(ctx, blockHash)
			if err != nil {
				t.Fatalf("getDcrBlock: %v", err)
			}
		}
		// Check that the tip is at the expected height and the block is mainchain.
		block, found := dcr.blockCache.atHeight(tipHeight)
		if !found {
			t.Fatalf("tip block not found in cache mainchain")
		}
		if block.orphaned {
			t.Fatalf("block unexpectedly orphaned before reorg")
		}
		_, found = dcr.blockCache.block(&block.hash)
		if !found {
			t.Fatalf("block not found with block method before reorg")
		}
		tipHash = &block.hash
	}

	ensureOrphaned := func(hash *chainhash.Hash, height uint32) {
		// Make sure mainchain is empty at the tip height.
		block, found := dcr.blockCache.block(hash)
		if !found {
			t.Fatalf("orphaned block from height %d not found after reorg", height)
		}
		if !block.orphaned {
			t.Fatalf("reorged block from height %d (%s) not marked as orphaned", height, hash)
		}
	}

	// A one-block reorg.
	reset()
	// Add a replacement blocks
	newHash := testAddBlockVerbose(nil, 1, tipHeight, 1)
	// Passing the hash to anyQ triggers the reorganization.
	dcr.blockCache.reorg(int64(tipHeight))
	dcr.blockCache.add(testChain.blocks[*newHash])
	ensureOrphaned(tipHash, tipHeight)
	newTip, found := dcr.blockCache.atHeight(tipHeight)
	if !found {
		t.Fatalf("3-deep reorg-causing new tip block not found on mainchain")
	}
	if newTip.hash != *newHash {
		t.Fatalf("tip hash mismatch after 1-block reorg")
	}

	// A 3-block reorg
	reset()
	tip, found1 := dcr.blockCache.atHeight(tipHeight)
	oneDeep, found2 := dcr.blockCache.atHeight(tipHeight - 1)
	twoDeep, found3 := dcr.blockCache.atHeight(tipHeight - 2)
	if !found1 || !found2 || !found3 {
		t.Fatalf("not all block found for 3-block reorg (%t, %t, %t)", found1, found2, found3)
	}
	newHash = testAddBlockVerbose(nil, 1, tipHeight-2, 1)
	dcr.blockCache.reorg(int64(tipHeight - 2))
	dcr.blockCache.add(testChain.blocks[*newHash])
	ensureOrphaned(&tip.hash, tip.height)
	ensureOrphaned(&oneDeep.hash, tip.height)
	ensureOrphaned(&twoDeep.hash, tip.height)
	newHeight := int64(dcr.blockCache.tipHeight())
	if newHeight != int64(twoDeep.height) {
		t.Fatalf("from tip height after 3-block reorg. expected %d, saw %d", twoDeep.height-1, newHeight)
	}
	newTip, found = dcr.blockCache.atHeight(uint32(newHeight))
	if !found {
		t.Fatalf("3-deep reorg-causing new tip block not found on mainchain")
	}
	if newTip.hash != *newHash {
		t.Fatalf("tip hash mismatch after 3-block reorg")
	}

	// Create a transaction at the tip, then orphan the block and move the
	// transaction to mempool.
	reset()
	txHash := randomHash()
	tip, _ = dcr.blockCache.atHeight(tipHeight)
	msg := testMsgTxRegular(dcrec.STEcdsaSecp256k1)

	testAddTxOut(msg.tx, 0, txHash, &tip.hash, int64(tipHeight), 1)

	utxo, err := dcr.utxo(ctx, txHash, msg.vout, nil)
	if err != nil {
		t.Fatalf("utxo error: %v", err)
	}
	confs, err := utxo.Confirmations(ctx)
	if err != nil {
		t.Fatalf("Confirmations error: %v", err)
	}
	if confs != 1 {
		t.Fatalf("wrong number of confirmations. expected 1, got %d", confs)
	}

	// Orphan the block and move the transaction to mempool.
	dcr.blockCache.reorg(int64(tipHeight - 2))
	testAddTxOut(msg.tx, 0, txHash, nil, 0, 0)
	confs, err = utxo.Confirmations(ctx)
	if err != nil {
		t.Fatalf("Confirmations error after reorg: %v", err)
	}
	if confs != 0 {
		t.Fatalf("Expected zero confirmations after reorg, found %d", confs)
	}

	// Start over, but put it in a lower block instead.
	reset()
	tip, _ = dcr.blockCache.atHeight(tipHeight)
	testAddBlockVerbose(&tip.hash, 1, tipHeight, 1)
	testAddTxOut(msg.tx, 0, txHash, &tip.hash, int64(tipHeight), 1)
	utxo, err = dcr.utxo(ctx, txHash, msg.vout, nil)
	if err != nil {
		t.Fatalf("utxo error 2: %v", err)
	}

	// Reorg and add a single block with the transaction.
	var reorgHeight uint32 = 5
	dcr.blockCache.reorg(int64(reorgHeight))
	newBlockHash := randomHash()
	testAddTxOut(msg.tx, 0, txHash, newBlockHash, int64(reorgHeight+1), 1)
	testAddBlockVerbose(newBlockHash, 1, reorgHeight+1, 1)
	dcr.getDcrBlock(ctx, newBlockHash) // Force blockCache update
	confs, err = utxo.Confirmations(ctx)
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
	dcr, shutdown := testBackend()
	defer shutdown()
	ctx := dcr.ctx // for the functions then take a context, canceled on shutdown()

	// Add a funding coin and retrieve it. Use a vote, since it has non-zero vout.
	cleanTestChain()
	maturity := int64(chainParams.CoinbaseMaturity)
	msg := testMsgTxVote()
	txid := hex.EncodeToString(randomBytes(32))
	txHash, _ := chainhash.NewHashFromStr(txid)
	txHeight := rand.Uint32()
	blockHash := testAddBlockVerbose(nil, 1, txHeight, 1)
	testAddTxOut(msg.tx, msg.vout, txHash, blockHash, int64(txHeight), maturity)
	coinID := toCoinID(txHash, msg.vout)
	utxo, err := dcr.FundingCoin(ctx, coinID, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if txHash.String() != utxo.TxID() {
		t.Fatalf("utxo tx hash doesn't match")
	}
	if utxo.TxID() != txid {
		t.Fatalf("utxo txid doesn't match")
	}

	// Check that values returned from FeeCoin are as set.
	cleanTestChain()
	msg = testMsgTxRegular(dcrec.STEcdsaSecp256k1)
	msg.tx.TxOut[0].Value = 8 // for consistency with fake TxRawResult added below

	scriptAddrs, nonStandard, err := dexdcr.ExtractScriptAddrs(msg.tx.TxOut[0].PkScript, chainParams)
	if err != nil {
		t.Fatalf("ExtractScriptAddrs error: %v", err)
	}
	if nonStandard {
		t.Errorf("vote output 0 was non-standard")
	}
	addr := scriptAddrs.PkHashes[0].String()

	msgHash := msg.tx.TxHash()
	txHash = &msgHash
	confs := int64(3)
	verboseTx := testAddTxVerbose(msg.tx, txHash, blockHash, int64(txHeight), confs)
	verboseTx.Vout = append(verboseTx.Vout, chainjson.Vout{
		N:       0,
		Value:   8,
		Version: 0,
		ScriptPubKey: chainjson.ScriptPubKeyResult{
			Hex:       hex.EncodeToString(msg.tx.TxOut[0].PkScript),
			ReqSigs:   1,
			Type:      "pubkeyhash",
			Addresses: []string{addr},
		},
	})

	txAddr, v, confs, err := dcr.FeeCoin(toCoinID(txHash, 0))
	if err != nil {
		t.Fatalf("FeeCoin error: %v", err)
	}
	if txAddr != addr {
		t.Fatalf("expected address %s, got %s", addr, txAddr)
	}
	expVal := toAtoms(8)
	if v != expVal {
		t.Fatalf("expected value %d, got %d", expVal, v)
	}
	if confs != 3 {
		t.Fatalf("expected 3 confirmations, got %d", confs)
	}

	var txHashBad chainhash.Hash
	copy(txHashBad[:], randomBytes(32))
	_, _, _, err = dcr.FeeCoin(toCoinID(&txHashBad, 0))
	if err == nil {
		t.Fatal("FeeCoin found for non-existent txid")
	}

	_, _, _, err = dcr.FeeCoin(toCoinID(txHash, 1))
	if err == nil {
		t.Fatal("FeeCoin found for non-existent output")
	}

	// make the output a stake hash
	stakeScript, _ := newStakeP2PKHScript(txscript.OP_SSGEN)
	verboseTx.Vout[0].ScriptPubKey.Hex = hex.EncodeToString(stakeScript)
	_, _, _, err = dcr.FeeCoin(toCoinID(txHash, 0))
	if err == nil {
		t.Fatal("FeeCoin accepted a stake output")
	}

	// make a p2sh
	msgP2SH := testMsgTxP2SHMofN(1, 2)
	scriptAddrs, nonStandard, err = dexdcr.ExtractScriptAddrs(msgP2SH.tx.TxOut[0].PkScript, chainParams)
	if err != nil {
		t.Fatalf("ExtractScriptAddrs error: %v", err)
	}
	if nonStandard {
		t.Errorf("output 0 was non-standard")
	}
	addr = scriptAddrs.PkHashes[0].String()
	msgHash = msgP2SH.tx.TxHash()
	txHash = &msgHash
	confs = int64(3)
	verboseTx = testAddTxVerbose(msgP2SH.tx, txHash, blockHash, int64(txHeight), confs)
	verboseTx.Vout = append(verboseTx.Vout, chainjson.Vout{
		N:       0,
		Value:   8,
		Version: 0,
		ScriptPubKey: chainjson.ScriptPubKeyResult{
			Hex:       hex.EncodeToString(msgP2SH.tx.TxOut[0].PkScript),
			ReqSigs:   1,
			Type:      "scripthash",
			Addresses: []string{addr},
		},
	})
	_, _, _, err = dcr.FeeCoin(toCoinID(txHash, 0))
	if err == nil {
		t.Fatal("FeeCoin accepted a p2sh output")
	}
}

// TestCheckAddress checks that addresses are parsing or not parsing as
// expected.
func TestCheckAddress(t *testing.T) {
	dcr := &Backend{}
	type test struct {
		addr    string
		wantErr bool
	}
	tests := []test{
		{"", true},
		{"DsYXjAK3UiTVN9js8v9G21iRbr2wPty7f12", false},
		{"DeZcGyCtPq7sTvACZupjT3BC1tsSEsKaYL4", false},
		{"DSo9Qw4FZLTwFL6fg2T9XPoJA8sFoZ4idZ7", false},
		{"DkM3W1518RharMSnqSiJCCGQ7RikMKCATeRvRwEW8vy1B2fjTd4Xi", false},
		{"Dce4vLzzENaZT7D2Wq5crRZ4VwfYMDMWkD9", false},
		{"TsYXjAK3UiTVN9js8v9G21iRbr2wPty7f12", true},
		{"Dce4vLzzENaZT7D2Wq5crRZ4VwfYMDMWkD0", true}, // capital letter O not base 58
		{"Dce4vLzzE", true},
	}
	for _, test := range tests {
		if dcr.CheckAddress(test.addr) != !test.wantErr {
			t.Fatalf("wantErr = %t, address = %s", test.wantErr, test.addr)
		}
	}
}

func TestValidateXPub(t *testing.T) {
	seed := randomBytes(hdkeychain.MinSeedBytes)
	master, err := hdkeychain.NewMaster(seed, chainParams)
	if err != nil {
		t.Fatalf("failed to generate master extended key: %v", err)
	}

	be := &Backend{}

	// fail for private key
	xprivStr := master.String()
	if err = be.ValidateXPub(xprivStr); err == nil {
		t.Errorf("no error for extended private key")
	}

	// succeed for public key
	xpub := master.Neuter()
	xpubStr := xpub.String()
	if err = be.ValidateXPub(xpubStr); err != nil {
		t.Error(err)
	}

	// fail for invalid key of wrong length
	if err = be.ValidateXPub(xpubStr[2:]); err == nil {
		t.Errorf("no error for invalid key")
	}

	// fail for wrong network
	masterTestnet, err := hdkeychain.NewMaster(seed, chaincfg.TestNet3Params())
	if err != nil {
		t.Fatalf("failed to generate master extended key: %v", err)
	}
	xpubTestnet := masterTestnet.Neuter()
	if err = be.ValidateXPub(xpubTestnet.String()); err == nil {
		t.Errorf("no error for invalid wrong network")
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

	// Same tests with ValidateCoinID
	be := &Backend{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := be.ValidateCoinID(tt.coinID)
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
	dcr, shutdown := testBackend()
	defer shutdown()
	tNode := dcr.node.(*testNode)
	tNode.blockchainInfo = &chainjson.GetBlockChainInfoResult{
		Headers: 100,
		Blocks:  99,
	}
	synced, err := dcr.Synced()
	if err != nil {
		t.Fatalf("Synced error: %v", err)
	}
	if !synced {
		t.Fatalf("not synced when should be synced")
	}

	tNode.blockchainInfo = &chainjson.GetBlockChainInfoResult{
		Headers: 100,
		Blocks:  50,
	}
	synced, err = dcr.Synced()
	if err != nil {
		t.Fatalf("Synced error: %v", err)
	}
	if synced {
		t.Fatalf("synced when shouldn't be synced")
	}

	tNode.blockchainInfoErr = fmt.Errorf("test error")
	_, err = dcr.Synced()
	if err == nil {
		t.Fatalf("getblockchaininfo error not propagated")
	}
	tNode.blockchainInfoErr = nil
}
