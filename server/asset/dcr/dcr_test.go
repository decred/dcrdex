// +build !dcrlive
//
// These tests will not be run if the dcrlive build tag is set.

package dcr

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"math"
	"math/rand"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/decred/dcrd/blockchain/stake/v2"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrec"
	"github.com/decred/dcrd/dcrec/secp256k1"
	"github.com/decred/dcrd/dcrutil/v2"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types"
	"github.com/decred/dcrd/txscript/v2"
	"github.com/decred/dcrd/wire"
)

func TestMain(m *testing.M) {
	// Call tidyConfig to set the global chainParams.
	err := tidyConfig(&DCRConfig{
		Net:      mainnetName,
		DcrdUser: "testuser",
		DcrdPass: "testpass",
	})
	if err != nil {
		fmt.Printf("tidyConfig error: %v\n", err)
		return
	}
	os.Exit(m.Run())
}

// TestTidyConfig checks that configuration parsing works as expected.
func TestTidyConfig(t *testing.T) {
	cfg := &DCRConfig{Net: mainnetName}
	err := tidyConfig(cfg)
	if err == nil {
		t.Errorf("expected error for config with no dcrd username/password, but saw none")
	}
	cfg = &DCRConfig{
		Net:      mainnetName,
		DcrdUser: "abc",
	}
	err = tidyConfig(cfg)
	if err == nil {
		t.Errorf("expected error for config with no dcrd username, but saw none")
	}
	cfg = &DCRConfig{
		Net:      mainnetName,
		DcrdPass: "def",
	}
	err = tidyConfig(cfg)
	if err == nil {
		t.Errorf("expected error for config with no dcrd password, but saw none")
	}
	cfg = &DCRConfig{
		Net:      mainnetName,
		DcrdUser: "abc",
		DcrdPass: "def",
	}
	err = tidyConfig(cfg)
	if err != nil {
		t.Errorf("unexpected error for config with username and password set: %v", err)
	}
	if cfg.DcrdServ != defaultMainnet {
		t.Errorf("DcrdServ not set implicitly")
	}
	if cfg.DcrdCert != defaultDaemonRPCCertFile {
		t.Errorf("DcrdCert not set implicitly")
	}
	cfg = &DCRConfig{
		Net:      mainnetName,
		DcrdUser: "abc",
		DcrdPass: "def",
		DcrdServ: "123",
		DcrdCert: "456",
	}
	err = tidyConfig(cfg)
	if err != nil {
		t.Errorf("unexpected error when settings DcrdServ/DcrdCert explicitly")
	}
	if cfg.DcrdServ != "123" {
		t.Errorf("DcrdServ not set to provided value")
	}
	if cfg.DcrdCert != "456" {
		t.Errorf("DcrdCert not set to provided value")
	}
}

// The remaining tests use the testBlockchain which, is a stub for
// rpcclient.Client. UTXOs, transactions and blocks are added to the blockchain
// as jsonrpc types to be requested by the dcrBackend.
//
// General formula for testing
// 1. Create a dcrBackend with the node field set to a dcrNode-implementing stub
// 2. Create a fake UTXO and all of the necessary jsonrpc-type blocks and
//    transactions and add the to the test blockchain.
// 3. Verify the dcrBackend and UTXO methods are returning whatever is expected.
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

// A fake blockchain to be used for RPC calls by the dcrNode.
type testBlockChain struct {
	txOuts map[string]*chainjson.GetTxOutResult
	txRaws map[chainhash.Hash]*chainjson.TxRawResult
	blocks map[chainhash.Hash]*chainjson.GetBlockVerboseResult
	hashes map[int64]*chainhash.Hash
}

// The testChain is a "blockchain" to store RPC responses for the dcrBackend
// node stub to request.
var testChain testBlockChain

// This must be called before using the testNode.
func cleanTestChain() {
	testChain = testBlockChain{
		txOuts: make(map[string]*chainjson.GetTxOutResult),
		txRaws: make(map[chainhash.Hash]*chainjson.TxRawResult),
		blocks: make(map[chainhash.Hash]*chainjson.GetBlockVerboseResult),
		hashes: make(map[int64]*chainhash.Hash),
	}
}

// A stub to replace rpcclient.Client for offline testing.
type testNode struct{}

// Store utxo info as a concatenated string hash:vout.
func txOutID(txHash *chainhash.Hash, index uint32) string {
	return txHash.String() + ":" + strconv.Itoa(int(index))
}

// Part of the dcrNode interface.
func (testNode) GetTxOut(txHash *chainhash.Hash, index uint32, _ bool) (*chainjson.GetTxOutResult, error) {
	outID := txOutID(txHash, index)
	out := testChain.txOuts[outID]
	// Unfound is not an error for GetTxOut.
	return out, nil
}

// Part of the dcrNode interface.
func (testNode) GetRawTransactionVerbose(txHash *chainhash.Hash) (*chainjson.TxRawResult, error) {
	tx, found := testChain.txRaws[*txHash]
	if !found {
		return nil, fmt.Errorf("test transaction not found\n")
	}
	return tx, nil
}

// Part of the dcrNode interface.
func (testNode) GetBlockVerbose(blockHash *chainhash.Hash, verboseTx bool) (*chainjson.GetBlockVerboseResult, error) {
	block, found := testChain.blocks[*blockHash]
	if !found {
		return nil, fmt.Errorf("test block not found")
	}
	return block, nil
}

// Part of the dcrNode interface.
func (testNode) GetBlockHash(blockHeight int64) (*chainhash.Hash, error) {
	hash, found := testChain.hashes[blockHeight]
	if !found {
		return nil, fmt.Errorf("test hash not found")
	}
	return hash, nil
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
	testChain.txOuts[txOutID(txHash, vout)] = txOut
	testAddTxVerbose(msgTx, txHash, blockHash, blockHeight, confirmations)
	return txOut
}

// Add a chainjson.TxRawResult to the blockchain.
func testAddTxVerbose(msgTx *wire.MsgTx, txHash, blockHash *chainhash.Hash, blockHeight, confirmations int64) *chainjson.TxRawResult {
	tx := testRawTransactionVerbose(msgTx, txHash, blockHash, blockHeight, confirmations)
	testChain.txRaws[*txHash] = tx
	return tx
}

// Create a *chainjson.GetBlockVerboseResult such as is returned by
// GetBlockVerbose.
func testBlockVerbose(blockHash *chainhash.Hash, confirmations, height int64, voteBits uint16) *chainjson.GetBlockVerboseResult {
	if voteBits&1 != 0 {
		testChain.hashes[height] = blockHash
	}
	return &chainjson.GetBlockVerboseResult{
		Hash:          blockHash.String(),
		Confirmations: confirmations,
		Height:        height,
		VoteBits:      voteBits,
	}
}

// Add a GetBlockVerboseResult to the blockchain.
func testAddBlockVerbose(blockHash *chainhash.Hash, confirmations int64, height uint32, voteBits uint16) *chainhash.Hash {
	if blockHash == nil {
		blockHash = randomHash()
	}
	testChain.blocks[*blockHash] = testBlockVerbose(blockHash, confirmations, int64(height), voteBits)
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

// Decode the hex string and append it to the script, returning the script.
func addHex(script []byte, encoded string) []byte {
	bytes, _ := hex.DecodeString(encoded)
	script = append(script, bytes...)
	return script
}

type testMsgTx struct {
	tx     *wire.MsgTx
	pubkey []byte
	pkHash []byte
	vout   uint32
	script []byte
}

// Generate a public key on the secp256k1 curve.
func genPubkey() ([]byte, []byte) {
	_, pub := secp256k1.PrivKeyFromBytes(randomBytes(32))
	pubkey := pub.Serialize()
	pkHash := dcrutil.Hash160(pubkey)
	return pubkey, pkHash
}

// A pay-to-script-hash pubkey script.
func newP2PKHScript() ([]byte, []byte, []byte) {
	script := make([]byte, 0, SwapContractSize)
	script = addHex(script, "76a914")
	pubkey, pkHash := genPubkey()
	script = append(script, pkHash...)
	script = addHex(script, "88ac")
	return script, pubkey, pkHash
}

// A pay-to-script-hash pubkey script, with a prepended stake-tree indicator
// byte.
func newStakeP2PKHScript(opcode byte) ([]byte, []byte, []byte) {
	script, pubkey, pkHash := newP2PKHScript()
	stakeScript := make([]byte, 0, len(script)+1)
	stakeScript = append(stakeScript, opcode)
	stakeScript = append(stakeScript, script...)
	return stakeScript, pubkey, pkHash
}

// A MsgTx for a regular transaction with a single output. No inputs, so it's
// not really a valid transaction, but that's okay on testBlockchain and
// irrelevant to dcrBackend.
func testMsgTxRegular() *testMsgTx {
	pkScript, pubkey, pkHash := newP2PKHScript()
	msgTx := wire.NewMsgTx()
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
	recipient dcrutil.Address
}

// Create a swap (initialization) contract with random pubkeys and return the
// pubkey script and addresses.
func testSwapContract() ([]byte, dcrutil.Address) {
	contract := make([]byte, 0, SwapContractSize)
	contract = addHex(contract, "6382012088c020") // This snippet checks the size of the secret and hashes it.
	// hashed secret
	secretKey := randomBytes(32)
	contract = append(contract, secretKey...)
	contract = addHex(contract, "8876a914") // check the hash
	_, receiverPKH := genPubkey()
	contract = append(contract, receiverPKH...)
	contract = addHex(contract, "6704f690625db17576a914") // check the pubkey hash,  else
	_, senderPKH := genPubkey()
	contract = append(contract, senderPKH...)
	contract = addHex(contract, "6888ac") // check the pubkey hash
	receiverAddr, _ := dcrutil.NewAddressPubKeyHash(receiverPKH, chainParams, dcrec.STEcdsaSecp256k1)
	// senderAddr, _ := dcrutil.NewAddressPubKeyHash(senderPKH, chainParams, dcrec.STEcdsaSecp256k1)
	return contract, receiverAddr
}

func testMsgTxSwapInit() *testMsgTxSwap {
	msgTx := wire.NewMsgTx()
	contract, recipient := testSwapContract()
	pkScript := make([]byte, 0, 23)
	pkScript = append(pkScript, txscript.OP_HASH160)
	pkScript = append(pkScript, txscript.OP_DATA_20)
	scriptHash := dcrutil.Hash160(contract)
	pkScript = append(pkScript, scriptHash...)
	pkScript = append(pkScript, txscript.OP_EQUAL)
	msgTx.AddTxOut(wire.NewTxOut(1, pkScript))
	return &testMsgTxSwap{
		tx:        msgTx,
		contract:  contract,
		recipient: recipient,
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
	script1 := make([]byte, stake.SSGenBlockReferenceOutSize)
	script1[0] = txscript.OP_RETURN
	script1[1] = txscript.OP_DATA_36
	msgTx.AddTxOut(wire.NewTxOut(0, script1))
	// output 2
	script2 := make([]byte, stake.SSGenVoteBitsOutputMinSize)
	script2[0] = txscript.OP_RETURN
	script2[1] = txscript.OP_DATA_2
	msgTx.AddTxOut(wire.NewTxOut(1, script2))
	// Now just a P2PKH script with a prepended OP_SSGEN
	script3, pubkey, pkHash := newStakeP2PKHScript(txscript.OP_SSGEN)
	msgTx.AddTxOut(wire.NewTxOut(2, script3))
	return &testMsgTx{
		tx:     msgTx,
		pubkey: pubkey,
		pkHash: pkHash,
		vout:   2,
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

// An M-of-N mutli-sig script. This script is pay-to-pubkey.
func testMultiSigScriptMofN(m, n int) ([]byte, [][]byte, [][]byte) {
	// serialized compressed pubkey used for multisig
	addrs := make([]*dcrutil.AddressSecpPubKey, 0, n)
	pubkeys := make([][]byte, 0, n)
	pkHashes := make([][]byte, 0, n)

	for i := 0; i < m; i++ {
		pubkey, pkHash := genPubkey()
		pubkeys = append(pubkeys, pubkey)
		pkHashes = append(pkHashes, pkHash)
		addr, err := dcrutil.NewAddressSecpPubKey(pubkey, chainParams)
		if err != nil {
			fmt.Printf("error creating AddressSecpPubKey: %v", err)
			return nil, nil, nil
		}
		addrs = append(addrs, addr)
	}
	script, err := txscript.MultiSigScript(addrs, m)
	if err != nil {
		fmt.Printf("error creating MultiSigScript: %v", err)
		return nil, nil, nil
	}
	return script, pubkeys, pkHashes
}

// A pay-to-script-hash M-of-N multi-sig MsgTx.
func testMsgTxP2SHMofN(m, n int) *testMsgTxP2SH {
	script, pubkeys, pkHashes := testMultiSigScriptMofN(m, n)
	pkScript := make([]byte, 0, 23)
	pkScript = append(pkScript, txscript.OP_HASH160)
	pkScript = append(pkScript, txscript.OP_DATA_20)
	scriptHash := dcrutil.Hash160(script)
	pkScript = append(pkScript, scriptHash...)
	pkScript = append(pkScript, txscript.OP_EQUAL)
	msgTx := wire.NewMsgTx()
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
	script, pubkey, pkHash := newStakeP2PKHScript(txscript.OP_SSRTX)
	msgTx.AddTxOut(wire.NewTxOut(1, script))
	return &testMsgTx{
		tx:     msgTx,
		pubkey: pubkey,
		pkHash: pkHash,
	}
}

// TestUTXOs tests all UTXO related paths.
func TestUTXOs(t *testing.T) {
	// The various UTXO types to check:
	// 1. A valid UTXO in a mempool transaction
	// 2. A valid UTXO in a mined block
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

	// Create a dcrBackend with the test node.
	ctx, shutdown := context.WithCancel(context.Background())
	defer shutdown()
	dcr := unconnectedDCR(&DCRConfig{
		Context: ctx,
	})
	dcr.node = testNode{}

	// The vout will be randomized during reset.
	txHeight := uint32(50)

	// A general reset function that clears the testBlockchain and the blockCache.
	reset := func() {
		cleanTestChain()
		dcr.blockCache = newBlockCache()
	}

	// CASE 1: A valid UTXO in a mempool transaction. This UTXO will have zero
	// confirmations, a valid pkScript and will not be marked as coinbase. Then
	// add a block that includes the transaction, and check that Confirmations
	// updates correctly.
	reset()
	txHash := randomHash()
	blockHash := randomHash()
	msg := testMsgTxRegular()
	// For a regular test tx, the output is at output index 0. Pass nil for the
	// block hash and 0 for the block height and confirmations for a mempool tx.
	testAddTxOut(msg.tx, msg.vout, txHash, nil, 0, 0)
	// There is no block info to add, since this is a mempool transaction
	utxo, err := dcr.utxo(txHash, msg.vout, nil)
	if err != nil {
		t.Fatalf("case 1 - unexpected error: %v", err)
	}
	// While we're here, check the spend script size is correct.
	scriptSize := utxo.ScriptSize()
	if scriptSize != P2PKHSigScriptSize {
		t.Fatalf("case 1 - unexpected spend script size reported. expected %d, got %d", P2PKHSigScriptSize, scriptSize)
	}
	// Now "mine" the transaction.
	testAddBlockVerbose(blockHash, 1, txHeight, 1)
	// Overwrite the test blockchain transaction details.
	testAddTxOut(msg.tx, 0, txHash, blockHash, int64(txHeight), 1)
	confs, err := utxo.Confirmations()
	if err != nil {
		t.Fatalf("case 1 - error retrieving confirmations after transaction \"mined\": %v", err)
	}
	if confs != 1 {
		// The confirmation count is not taken from the txOut.Confirmations, so
		// need to check that it is correct.
		t.Fatalf("case 1 - expected 1 confirmation after mining transaction, found %d", confs)
	}
	// Make sure the pubkey spends the output.
	spends, err := utxo.PaysToPubkeys([][]byte{msg.pubkey})
	if err != nil {
		t.Fatalf("case 1 - PaysToPubkeys error: %v", err)
	}
	if !spends {
		t.Fatalf("case 1 - false returned from PaysToPubkeys")
	}

	// CASE 2: A valid UTXO in a mined block. This UTXO will have non-zero
	// confirmations, a valid pkScipt
	reset()
	blockHash = testAddBlockVerbose(nil, 1, txHeight, 1)
	txHash = randomHash()
	msg = testMsgTxRegular()
	testAddTxOut(msg.tx, msg.vout, txHash, blockHash, int64(txHeight), 1)
	utxo, err = dcr.utxo(txHash, msg.vout, nil)
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
	_, err = dcr.utxo(randomHash(), 0, nil)
	if err == nil {
		t.Fatalf("case 3 - received no error for a non-existent UTXO")
	}

	// CASE 4: A UTXO that is invalid because it has the wrong script type.
	reset()
	blockHash = testAddBlockVerbose(nil, 1, txHeight, 1)
	txHash = randomHash()
	msg = testMsgTxRegular()
	// make the script nonsense.
	msg.tx.TxOut[0].PkScript = []byte{0x00, 0x01, 0x02, 0x03}
	testAddTxOut(msg.tx, msg.vout, txHash, blockHash, int64(txHeight), 1)
	_, err = dcr.utxo(txHash, msg.vout, nil)
	if err == nil {
		t.Fatalf("case 4 - received no error for a UTXO with wrong script type")
	}

	// CASE 5: A UTXO that is invalid because it is a regular tree tx from a
	// stakeholder invalidated block. The transaction is valid when it has 1
	// confirmation, but is invalidated by the next block.
	reset()
	blockHash = testAddBlockVerbose(nil, 1, txHeight, 1)
	txHash = randomHash()
	msg = testMsgTxRegular()
	testAddTxOut(msg.tx, msg.vout, txHash, blockHash, int64(txHeight), 1)
	utxo, err = dcr.utxo(txHash, msg.vout, nil)
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
	_, err = utxo.Confirmations()
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
	_, err = dcr.utxo(txHash, msg.vout, nil)
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
	utxo, err = dcr.utxo(txHash, msg.vout, nil)
	if err != nil {
		t.Fatalf("case 6 - unexpected error after maturing block: %v", err)
	}
	// Since this is our first stake transaction, let's check the pubkey
	spends, err = utxo.PaysToPubkeys([][]byte{msg.pubkey})
	if err != nil {
		t.Fatalf("case 6 - PaysToPubkeys error: %v", err)
	}
	if !spends {
		t.Fatalf("case 6 - false returned from PaysToPubkeys")
	}

	// CASE 7: A UTXO that becomes invalid in a reorg
	reset()
	txHash = randomHash()
	blockHash = testAddBlockVerbose(nil, 1, txHeight, 1)
	msg = testMsgTxRegular()
	testAddTxOut(msg.tx, msg.vout, txHash, blockHash, int64(txHeight), 1)
	utxo, err = dcr.utxo(txHash, msg.vout, nil)
	if err != nil {
		t.Fatalf("case 7 - received error for utxo")
	}
	_, err = utxo.Confirmations()
	if err != nil {
		t.Fatalf("case 7 - received error before reorg")
	}
	betterHash := testAddBlockVerbose(nil, 1, txHeight, 1)
	dcr.anyQ <- betterHash
	time.Sleep(time.Millisecond * 50)
	// Remove the txout from the blockchain, since dcrd would no longer return it.
	delete(testChain.txOuts, txOutID(txHash, msg.vout))
	_, err = utxo.Confirmations()
	if err == nil {
		t.Fatalf("case 7 - received no error for orphaned transaction")
	}

	// CASE 8: A UTXO that is in an orphaned block, but also included in a new
	// mainchain block, so is still valid.
	reset()
	txHash = randomHash()
	orphanHash := testAddBlockVerbose(nil, 1, txHeight, 1)
	msg = testMsgTxRegular()
	testAddTxOut(msg.tx, msg.vout, txHash, orphanHash, int64(txHeight), 1)
	utxo, err = dcr.utxo(txHash, msg.vout, nil)
	if err != nil {
		t.Fatalf("case 8 - received error for utxo")
	}
	// Now orphan the block, by doing a reorg.
	betterHash = testAddBlockVerbose(nil, 1, txHeight, 1)
	dcr.anyQ <- betterHash
	time.Sleep(time.Millisecond * 50)
	testAddTxOut(msg.tx, msg.vout, txHash, betterHash, int64(txHeight), 1)
	_, err = utxo.Confirmations()
	if err != nil {
		t.Fatalf("case 8 - unexpected error after reorg")
	}
	if utxo.blockHash != *betterHash {
		t.Fatalf("case 8 - unexpected hash for utxo after reorg")
	}
	// Do it again, but this time, put the utxo into mempool.
	evenBetter := testAddBlockVerbose(nil, 1, txHeight, 1)
	dcr.anyQ <- evenBetter
	time.Sleep(time.Millisecond * 50)
	testAddTxOut(msg.tx, msg.vout, txHash, evenBetter, 0, 0)
	_, err = utxo.Confirmations()
	if err != nil {
		t.Fatalf("case 8 - unexpected error for mempool tx after reorg")
	}
	if utxo.height != 0 {
		t.Fatalf("case 10 - unexpected height %d after dumping into mempool", utxo.height)
	}

	// CASE 9: A UTXO with a pay-to-script-hash for a 1-of-2 multisig redeem
	// script
	reset()
	txHash = randomHash()
	blockHash = testAddBlockVerbose(nil, 1, txHeight, 1)
	msgMultiSig := testMsgTxP2SHMofN(1, 2)
	testAddTxOut(msgMultiSig.tx, msgMultiSig.vout, txHash, blockHash, int64(txHeight), 1)
	// First try to get the UTXO without providing the raw script.
	_, err = dcr.utxo(txHash, msgMultiSig.vout, nil)
	if err == nil {
		t.Fatalf("no error thrown for p2sh utxo when no script was provided")
	}
	// Now provide the script.
	utxo, err = dcr.utxo(txHash, msgMultiSig.vout, msgMultiSig.script)
	if err != nil {
		t.Fatalf("case 9 - received error for utxo: %v", err)
	}
	confs, err = utxo.Confirmations()
	if err != nil {
		t.Fatalf("case 9 - error getting confirmations: %v", err)
	}
	if confs != 1 {
		t.Fatalf("case 9 - expected 1 confirmation, got %d", confs)
	}
	spends, err = utxo.PaysToPubkeys(msgMultiSig.pubkeys[:1])
	if err != nil {
		t.Fatalf("case 9 - PaysToPubkeys error: %v", err)
	}
	if !spends {
		t.Fatalf("case 9 - false returned from PaysToPubkeys")
	}

	// CASE 10: A UTXO with a pay-to-script-hash for a 2-of-2 multisig redeem
	// script
	reset()
	txHash = randomHash()
	blockHash = testAddBlockVerbose(nil, 1, txHeight, 1)
	msgMultiSig = testMsgTxP2SHMofN(2, 2)
	testAddTxOut(msgMultiSig.tx, msgMultiSig.vout, txHash, blockHash, int64(txHeight), 1)
	utxo, err = dcr.utxo(txHash, msgMultiSig.vout, msgMultiSig.script)
	if err != nil {
		t.Fatalf("case 10 - received error for utxo: %v", err)
	}
	// Try to get by with just one of the pubkeys.
	_, err = utxo.PaysToPubkeys(msgMultiSig.pubkeys[:1])
	if err == nil {
		t.Fatalf("case 10 - no error when only provided one of two required pubkeys")
	}
	// Now do both.
	spends, err = utxo.PaysToPubkeys(msgMultiSig.pubkeys)
	if err != nil {
		t.Fatalf("case 10 - PaysToPubkeys error: %v", err)
	}
	if !spends {
		t.Fatalf("case 10 - false returned from PaysToPubkeys")
	}

	// CASE 11: A UTXO with a pay-to-script-hash for a P2PKH redeem script.
	reset()
	txHash = randomHash()
	blockHash = testAddBlockVerbose(nil, maturity, txHeight, 1)
	msg = testMsgTxP2SHVote()
	testAddTxOut(msg.tx, msg.vout, txHash, blockHash, int64(txHeight), maturity)
	// mature the vote
	testAddBlockVerbose(nil, 1, txHeight+uint32(maturity)-1, 1)
	utxo, err = dcr.utxo(txHash, msg.vout, msg.script)
	if err != nil {
		t.Fatalf("case 11 - received error for utxo: %v", err)
	}
	// Make sure it's marked as stake.
	if !utxo.scriptType.isStake() {
		t.Fatalf("case 11 - stake p2sh not marked as stake")
	}
	// Give it nonsense.
	_, err = utxo.PaysToPubkeys([][]byte{randomBytes(33)})
	if err == nil {
		t.Fatalf("case 11 - no error when providing nonsense pubkey")
	}
	// Now give it the right one.
	spends, err = utxo.PaysToPubkeys([][]byte{msg.pubkey})
	if err != nil {
		t.Fatalf("case 11 - PaysToPubkeys error: %v", err)
	}
	if !spends {
		t.Fatalf("case 11 - false returned from PaysToPubkeys")
	}

	// CASE 12: A revocation.
	reset()
	txHash = randomHash()
	blockHash = testAddBlockVerbose(nil, maturity, txHeight, 1)
	msg = testMsgTxRevocation()
	testAddTxOut(msg.tx, msg.vout, txHash, blockHash, int64(txHeight), maturity)
	// mature the revocation
	testAddBlockVerbose(nil, 1, txHeight+uint32(maturity)-1, 1)
	utxo, err = dcr.utxo(txHash, msg.vout, msg.script)
	if err != nil {
		t.Fatalf("case 12 - received error for utxo: %v", err)
	}
	// Make sure it's marked as stake.
	if !utxo.scriptType.isStake() {
		t.Fatalf("case 12 - stake p2sh not marked as stake")
	}
	// Check the pubkey.
	spends, err = utxo.PaysToPubkeys([][]byte{msg.pubkey})
	if err != nil {
		t.Fatalf("case 12 - PaysToPubkeys error: %v", err)
	}
	if !spends {
		t.Fatalf("case 12 - false returned from PaysToPubkeys")
	}
}

// TestReorg sends a reorganization-causing block through the anyQ channel, and
// checks that the cache is responding as expected.
func TestReorg(t *testing.T) {
	// Create a dcrBackend with the test node.
	ctx, shutdown := context.WithCancel(context.Background())
	defer shutdown()
	dcr := unconnectedDCR(&DCRConfig{
		Context: ctx,
	})
	dcr.node = testNode{}

	// A general reset function that clears the testBlockchain and the blockCache.
	tipHeight := 10
	var tipHash *chainhash.Hash
	reset := func() {
		cleanTestChain()
		dcr.blockCache = newBlockCache()
		for h := 0; h <= tipHeight; h++ {
			blockHash := testAddBlockVerbose(nil, int64(tipHeight-h+1), uint32(h), 1)
			// force dcr to get and cache the block
			_, err := dcr.getDcrBlock(blockHash)
			if err != nil {
				t.Fatalf("getDcrBlock: %v", err)
			}
		}
		// Check that the tip is at the expected height and the block is mainchain.
		block, found := dcr.blockCache.mainchain[uint32(tipHeight)]
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

	ensureOrphaned := func(hash *chainhash.Hash, height int) {
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
	newHash := testAddBlockVerbose(nil, 1, uint32(tipHeight), 1)
	// Passing the hash to anyQ triggers the reorganization.
	dcr.anyQ <- newHash
	time.Sleep(time.Millisecond * 50)
	ensureOrphaned(tipHash, tipHeight)
	newTip, found := dcr.blockCache.mainchain[uint32(tipHeight)]
	if !found {
		t.Fatalf("3-deep reorg-causing new tip block not found on mainchain")
	}
	if newTip.hash != *newHash {
		t.Fatalf("tip hash mismatch after 1-block reorg")
	}

	// A 3-block reorg
	reset()
	tip, found1 := dcr.blockCache.mainchain[uint32(tipHeight)]
	oneDeep, found2 := dcr.blockCache.mainchain[uint32(tipHeight-1)]
	twoDeep, found3 := dcr.blockCache.mainchain[uint32(tipHeight-2)]
	if !found1 || !found2 || !found3 {
		t.Fatalf("not all block found for 3-block reorg (%t, %t, %t)", found1, found2, found3)
	}
	newHash = testAddBlockVerbose(nil, 1, uint32(tipHeight-2), 1)
	dcr.anyQ <- newHash
	time.Sleep(time.Millisecond * 50)
	ensureOrphaned(&tip.hash, int(tip.height))
	ensureOrphaned(&oneDeep.hash, int(tip.height))
	ensureOrphaned(&twoDeep.hash, int(tip.height))
	newHeight := int64(dcr.blockCache.tipHeight())
	if newHeight != twoDeep.height {
		t.Fatalf("from tip height after 3-block reorg. expected %d, saw %d", twoDeep.height-1, newHeight)
	}
	newTip, found = dcr.blockCache.mainchain[uint32(newHeight)]
	if !found {
		t.Fatalf("3-deep reorg-causing new tip block not found on mainchain")
	}
	if newTip.hash != *newHash {
		t.Fatalf("tip hash mismatch after 3-block reorg")
	}
}

// TestSignatures checks that signature verification is working.
func TestSignatures(t *testing.T) {
	dcr := dcrBackend{}
	verify := func(pk, msg, sig string) {
		pubkey, _ := hex.DecodeString(pk)
		message, _ := hex.DecodeString(msg)
		signature, _ := hex.DecodeString(sig)
		if !dcr.VerifySignature(message, pubkey, signature) {
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
	// Create a dcrBackend with the test node.
	ctx, shutdown := context.WithCancel(context.Background())
	defer shutdown()
	dcr := unconnectedDCR(&DCRConfig{
		Context: ctx,
	})
	dcr.node = testNode{}

	// Test 1: Test different states of validity.
	cleanTestChain()
	blockHeight := uint32(500)
	txHash := randomHash()
	blockHash := randomHash()
	msg := testMsgTxRegular()
	verboseTx := testAddTxVerbose(msg.tx, txHash, blockHash, int64(blockHeight), 2)
	// Mine the transaction and an approving block.
	testAddBlockVerbose(blockHash, 1, blockHeight, 1)
	testAddBlockVerbose(randomHash(), 1, blockHeight+1, 1)
	// Add some random transaction at index 0.
	verboseTx.Vout = append(verboseTx.Vout, testVout(1, randomBytes(20)))
	// Add a swap output at index 1.
	swap := testMsgTxSwapInit()
	swapTxOut := swap.tx.TxOut[swap.vout]
	verboseTx.Vout = append(verboseTx.Vout, testVout(float64(swapTxOut.Value)/dcrToAtoms, swapTxOut.PkScript))
	// Make a transaction input spending some random utxo.
	spentTx := randomHash()
	spentVout := rand.Uint32()
	verboseTx.Vin = append(verboseTx.Vin, testVin(spentTx, spentVout))
	// Get the transaction through the backend.
	dexTx, err := dcr.transaction(txHash)
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
	newHash := testAddBlockVerbose(nil, 1, blockHeight+1, 0)
	// Passing the hash to anyQ triggers the reorganization.
	dcr.anyQ <- newHash
	time.Sleep(time.Millisecond * 50)
	_, err = dexTx.Confirmations()
	if err == nil {
		t.Fatalf("no error when checking confirmations on an invalidated tx")
	}

	// Test 2: Before and after mining.
	cleanTestChain()
	dcr.blockCache = newBlockCache()
	txHash = randomHash()
	// Add a transaction to mempool.
	msg = testMsgTxRegular()
	testAddTxVerbose(msg.tx, txHash, nil, 0, 0)
	// Get the transaction data through the backend and make sure it has zero
	// confirmations.
	dexTx, err = dcr.transaction(txHash)
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
	blockHash = testAddBlockVerbose(nil, 1, blockHeight, 0)
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
	// Create a dcrBackend with the test node.
	ctx, shutdown := context.WithCancel(context.Background())
	defer shutdown()
	dcr := unconnectedDCR(&DCRConfig{
		Context: ctx,
	})
	dcr.node = testNode{}

	// Add a transaction and retrieve it. Use a vote, since it has non-zero vout.
	cleanTestChain()
	maturity := int64(chainParams.CoinbaseMaturity)
	msg := testMsgTxVote()
	txid := hex.EncodeToString(randomBytes(32))
	txHash, _ := chainhash.NewHashFromStr(txid)
	txHeight := rand.Uint32()
	blockHash := testAddBlockVerbose(nil, 1, txHeight, 1)
	testAddTxOut(msg.tx, msg.vout, txHash, blockHash, int64(txHeight), maturity)
	utxo, err := dcr.UTXO(txid, msg.vout, nil)
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
