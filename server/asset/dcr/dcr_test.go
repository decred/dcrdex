// +build !dcrlive
//
// These tests will not be run if the dcrlive build tag is set.

package dcr

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrec"
	"github.com/decred/dcrd/dcrutil/v2"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types"
)

var (
	testP2PKHScript    []byte
	testNotP2PKHScript []byte
)

func TestMain(m *testing.M) {
	var err error
	// The POW reward from block 2.
	testP2PKHScript, err = hex.DecodeString("76a9148ffe7a49ecf0f4858e7a52155302177398d2296988ac")
	if err != nil {
		fmt.Printf("error decoding testP2PKHScript: %v", err)
		return
	}
	// The first stakegen from block 4096.
	testNotP2PKHScript, err = hex.DecodeString("bb76a914d81c2a2b41089cee62c2cbd85a47acf5f5f0353d88ac")
	if err != nil {
		fmt.Printf("error decoding testNotP2PKHScript: %v", err)
		return
	}
	// Call tidyConfig to set the global chainParams.
	err = tidyConfig(&DCRConfig{
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

// A fake blockchain to be used for lookup by the dcrNode interface methods.
type testBlockChain struct {
	txOuts map[string]*chainjson.GetTxOutResult
	txRaws map[chainhash.Hash]*chainjson.TxRawResult
	blocks map[chainhash.Hash]*chainjson.GetBlockVerboseResult
	hashes map[int64]*chainhash.Hash
}

var testChain testBlockChain

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

// Store utxo info as a concatenated string [hash]:[vout].
func txOutID(txHash *chainhash.Hash, index uint32) string {
	return txHash.String() + ":" + strconv.Itoa(int(index))
}

func (testNode) GetTxOut(txHash *chainhash.Hash, index uint32, _ bool) (*chainjson.GetTxOutResult, error) {
	outID := txOutID(txHash, index)
	out := testChain.txOuts[outID]
	// Unfound is not an error for GetTxOut.
	return out, nil
}

func (testNode) GetRawTransactionVerbose(txHash *chainhash.Hash) (*chainjson.TxRawResult, error) {
	tx, found := testChain.txRaws[*txHash]
	if !found {
		return nil, fmt.Errorf("test transaction not found\n")
	}
	return tx, nil
}

func (testNode) GetBlockVerbose(blockHash *chainhash.Hash, verboseTx bool) (*chainjson.GetBlockVerboseResult, error) {
	block, found := testChain.blocks[*blockHash]
	if !found {
		return nil, fmt.Errorf("test block not found")
	}
	return block, nil
}

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
func testRawTransactionVerbose(txid, blockHash *chainhash.Hash, blockHeight, confirmations int64, vout uint32) *chainjson.TxRawResult {
	var hash string
	if blockHash != nil {
		hash = blockHash.String()
	}
	return &chainjson.TxRawResult{
		Txid:          txid.String(),
		BlockHash:     hash,
		BlockHeight:   blockHeight,
		BlockIndex:    vout,
		Confirmations: confirmations,
	}
}

// Add a transaction output and it's getrawtransaction data.
func testAddTxOut(blockHash, txHash *chainhash.Hash, blockHeight, vout uint32, confirmations int64, pkScript []byte) *chainjson.GetTxOutResult {
	txOut := testGetTxOut(confirmations, pkScript)
	testChain.txOuts[txOutID(txHash, vout)] = txOut
	testAddTxVerbose(txHash, blockHash, blockHeight, vout, confirmations)
	return txOut
}

// Add a chainjson.TxRawResult to the blockchain.
func testAddTxVerbose(txHash, blockHash *chainhash.Hash, blockHeight, vout uint32, confirmations int64) *chainjson.TxRawResult {
	tx := testRawTransactionVerbose(txHash, blockHash, int64(blockHeight), confirmations, vout)
	testChain.txRaws[*txHash] = tx
	return tx
}

// Create a *chainjson.GetBlockVerboseResult such as is returned by
// GetBlockVerbose.
func testBlockVerbose(blockHash *chainhash.Hash, confirmations, height int64, stx []string, voteBits uint16) *chainjson.GetBlockVerboseResult {
	if voteBits&1 != 0 {
		testChain.hashes[height] = blockHash
	}
	return &chainjson.GetBlockVerboseResult{
		Hash:          blockHash.String(),
		Confirmations: confirmations,
		Height:        height,
		STx:           stx,
		VoteBits:      voteBits,
	}
}

// Add verbose block data to the blockchain.
func testAddBlockVerbose(blockHash *chainhash.Hash, confirmations int64, height uint32, stx []string, voteBits uint16) *chainhash.Hash {
	if blockHash == nil {
		blockHash = randomHash()
	}
	testChain.blocks[*blockHash] = testBlockVerbose(blockHash, confirmations, int64(height), stx, voteBits)
	return blockHash
}

func testVout(value float64, pkScript []byte) chainjson.Vout {
	return chainjson.Vout{
		Value: value,
		ScriptPubKey: chainjson.ScriptPubKeyResult{
			Hex: hex.EncodeToString(pkScript),
		},
	}
}

func testVin(txHash *chainhash.Hash, vout uint32) chainjson.Vin {
	return chainjson.Vin{
		Txid: txHash.String(),
		Vout: vout,
	}
}

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

	// Create a dcrBackend with the test node.
	ctx, shutdown := context.WithCancel(context.Background())
	defer shutdown()
	dcr := unconnectedDCR(&DCRConfig{
		Context: ctx,
	})
	dcr.node = testNode{}

	// The vout will be randomized during reset.
	var vout uint32
	txHeight := uint32(50)

	// A general reset function that clears the testBlockchain and the blockCache.
	reset := func() {
		cleanTestChain()
		dcr.blockCache = newBlockCache()
		vout = uint32(rand.Int())
	}

	// CASE 1: A valid UTXO in a mempool transaction. This UTXO will have zero
	// confirmations, a valid pkScript and will not be marked as coinbase. Then
	// add a block that includes the transaction, and check that Confirmations
	// updates correctly.
	reset()
	txHash := randomHash()
	blockHash := randomHash()
	testAddTxOut(nil, txHash, 0, vout, 0, testP2PKHScript)
	// There is no block info to add, since this is a mempool transaction
	utxo, err := dcr.utxo(txHash, vout)
	if err != nil {
		t.Fatalf("case 1 - unexpected error: %v", err)
	}
	// While we're here, check the spend script size is correct.
	scriptSize := utxo.ScriptSize()
	if scriptSize != P2PKHSigScriptSize {
		t.Fatalf("case 1 - unexpected spend script size reported. expected %d, got %d", P2PKHSigScriptSize, scriptSize)
	}
	// Now "mine" the transaction.
	testAddBlockVerbose(blockHash, 1, txHeight, nil, 1)
	// Overwrite the test blockchain transaction details.
	testAddTxOut(blockHash, txHash, txHeight, vout, 1, testP2PKHScript)
	confs, err := utxo.Confirmations()
	if err != nil {
		t.Fatalf("case 1 - error retrieving confirmations after transaction \"mined\": %v", err)
	}
	if confs != 1 {
		// The confirmation count is not taken from the txOut.Confirmations, so
		// need to check that it is correct.
		t.Fatalf("case 1 - expected 1 confirmation after mining transaction, found %d", confs)
	}

	// CASE 2: A valid UTXO in a mined block. This UTXO will have non-zero
	// confirmations, a valid pkScipt
	reset()
	blockHash = testAddBlockVerbose(nil, 1, txHeight, nil, 1)
	txHash = randomHash()
	testAddTxOut(blockHash, txHash, txHeight, vout, 1, testP2PKHScript)
	_, err = dcr.utxo(txHash, vout)
	if err != nil {
		t.Fatalf("case 2 - unexpected error: %v", err)
	}

	// CASE 3: A UTXO that is invalid because it is non-existent
	reset()
	_, err = dcr.utxo(randomHash(), vout)
	if err == nil {
		t.Fatalf("case 3 - received no error for a non-existent UTXO")
	}

	// CASE 4: A UTXO that is invalid because it has the wrong script type.
	reset()
	blockHash = testAddBlockVerbose(nil, 1, txHeight, nil, 1)
	txHash = randomHash()
	testAddTxOut(blockHash, txHash, txHeight, vout, 1, testNotP2PKHScript)
	_, err = dcr.utxo(txHash, vout)
	if err == nil {
		t.Fatalf("case 4 - received no error for a UTXO with wrong script type")
	}

	// CASE 5: A UTXO that is invalid because it is a regular tree tx from a
	// stakeholder invalidated block. The transaction is valid when it has 1
	// confirmation, but is invalidated by the next block.
	reset()
	blockHash = testAddBlockVerbose(nil, 1, txHeight, nil, 1)
	txHash = randomHash()
	testAddTxOut(blockHash, txHash, txHeight, vout, 1, testP2PKHScript)
	utxo, err = dcr.utxo(txHash, vout)
	if err != nil {
		t.Fatalf("case 5 - unexpected error: %v", err)
	}
	// Now reject the block.
	rejectingHash := testAddBlockVerbose(nil, 1, txHeight+1, nil, 0)
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
	// invalidated block, because it is a stake tree tx.
	reset()
	txHash = randomHash()
	s := txHash.String()
	blockHash = testAddBlockVerbose(nil, 1, txHeight, []string{s}, 1)
	testAddTxOut(blockHash, txHash, txHeight, vout, 1, testP2PKHScript)
	utxo, err = dcr.utxo(txHash, vout)
	if err != nil {
		t.Fatalf("case 6 - unexpected error: %v", err)
	}
	// Now reject the block.
	rejectingHash = testAddBlockVerbose(nil, 1, txHeight+1, nil, 0)
	rejectingBlock = testChain.blocks[*rejectingHash]
	_, err = dcr.blockCache.add(rejectingBlock)
	if err != nil {
		t.Fatalf("case 5 - error adding to block cache: %v", err)
	}
	_, err = utxo.Confirmations()
	if err != nil {
		t.Fatalf("case 6 - unexpected error during confirmation check: %v", err)
	}

	// CASE 7: A UTXO that becomes invalid in a reorg
	reset()
	txHash = randomHash()
	blockHash = testAddBlockVerbose(nil, 1, txHeight, []string{txHash.String()}, 1)
	testAddTxOut(blockHash, txHash, txHeight, vout, 1, testP2PKHScript)
	utxo, err = dcr.utxo(txHash, vout)
	if err != nil {
		t.Fatalf("case 7 - received error for utxo")
	}
	_, err = utxo.Confirmations()
	if err != nil {
		t.Fatalf("case 7 - received error before reorg")
	}
	betterHash := testAddBlockVerbose(nil, 1, txHeight, nil, 1)
	dcr.anyQ <- betterHash
	time.Sleep(time.Millisecond * 50)
	// Remove the txout from the blockchain, since dcrd would no longer return it.
	delete(testChain.txOuts, txOutID(txHash, vout))
	_, err = utxo.Confirmations()
	if err == nil {
		t.Fatalf("case 7 - received no error for orphaned transaction")
	}

	// CASE 8: A UTXO that is in an orphaned block, but also included in a new
	// mainchain block, so is still valid.
	reset()
	txHash = randomHash()
	orphanHash := testAddBlockVerbose(nil, 1, txHeight, []string{txHash.String()}, 1)
	testAddTxOut(orphanHash, txHash, txHeight, vout, 1, testP2PKHScript)
	utxo, err = dcr.utxo(txHash, vout)
	if err != nil {
		t.Fatalf("case 8 - received error for utxo")
	}
	// Now orphan the block, by doing a reorg.
	betterHash = testAddBlockVerbose(nil, 1, txHeight, nil, 1)
	dcr.anyQ <- betterHash
	time.Sleep(time.Millisecond * 50)
	testAddTxOut(betterHash, txHash, txHeight, vout, 1, testP2PKHScript)
	_, err = utxo.Confirmations()
	if err != nil {
		t.Fatalf("case 8 - unexpected error after reorg")
	}
	if utxo.blockHash != *betterHash {
		t.Fatalf("case 8 - unexpected hash for utxo after reorg")
	}
	// Do it again, but this time, put the utxo into mempool.
	evenBetter := testAddBlockVerbose(nil, 1, txHeight, nil, 1)
	dcr.anyQ <- evenBetter
	time.Sleep(time.Millisecond * 50)
	testAddTxOut(nil, txHash, 0, vout, 0, testP2PKHScript)
	_, err = utxo.Confirmations()
	if err != nil {
		t.Fatalf("case 8 - unexpected error for mempool tx after reorg")
	}
	if utxo.height != 0 {
		t.Fatalf("case 10 - unexpected height %d after dumping into mempool", utxo.height)
	}
}

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
			blockHash := testAddBlockVerbose(nil, int64(tipHeight-h+1), uint32(h), nil, 1)
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
	newHash := testAddBlockVerbose(nil, 1, uint32(tipHeight), nil, 1)
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
	newHash = testAddBlockVerbose(nil, 1, uint32(tipHeight-2), nil, 1)
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

func TestPaysToPubkey(t *testing.T) {
	verify := func(script, pk string) {
		pkScript, _ := hex.DecodeString(script)
		pubkey, _ := hex.DecodeString(pk)
		utxo := &UTXO{
			pkScript: pkScript,
		}
		if !utxo.PaysToPubkey(pubkey, nil) {
			t.Fatalf("pubkey %s not found in script %s", pk, script)
		}
	}
	// 3rd (index 2) coinbase output from block 2
	verify(
		"76a9148ffe7a49ecf0f4858e7a52155302177398d2296988ac",
		"02a11654899f5a7144974b24cc74f7e41726a48e883002e97be1d7dc9bdf23869e",
	)

	// the 0th output of the first transaction in block 257
	verify(
		"76a914b9b3944f5307161bf2f84ed5e5d09e687a437acf88ac",
		"0328280073f257b54b6be3574f467a30b1c25dfbbf08524e24dcd2a3b5000e37f9",
	)
}

// Create a swap (initialization) contract with random pubkeys and return the
// pubkey script and addresses.
func testSwapContract() ([]byte, string, string) {
	contract := make([]byte, 0, SwapContractSize)
	addHex := func(s string) {
		bytes, _ := hex.DecodeString(s)
		contract = append(contract, bytes...)
	}
	addHex("6382012088c020") // This snippet checks the size of the secret and hashes it.
	// hashed secret
	secretKey := randomBytes(32)
	contract = append(contract, secretKey...)
	addHex("8876a914") // check the hash
	receiverPKH := randomBytes(20)
	contract = append(contract, receiverPKH...)
	addHex("6704f690625db17576a914") // check the pubkey hash,  else
	senderPKH := randomBytes(20)
	contract = append(contract, senderPKH...)
	addHex("6888ac") // check the pubkey hash
	receiverAddr, _ := dcrutil.NewAddressPubKeyHash(receiverPKH, chainParams, dcrec.STEcdsaSecp256k1)
	senderAddr, _ := dcrutil.NewAddressPubKeyHash(senderPKH, chainParams, dcrec.STEcdsaSecp256k1)
	return contract, senderAddr.String(), receiverAddr.String()
}

func TestTransactions(t *testing.T) {
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
	vout := int64(0)
	verboseTx := testAddTxVerbose(txHash, blockHash, blockHeight, 2, vout)
	// Mine the transaction and an approving block.
	testAddBlockVerbose(blockHash, 1, blockHeight, nil, 1)
	testAddBlockVerbose(randomHash(), 1, blockHeight+1, nil, 1)
	// Add some random transaction at index 0.
	verboseTx.Vout = append(verboseTx.Vout, testVout(1, randomBytes(20)))
	// Add a swap transaction at index 1.
	pkScript, sender, receiver := testSwapContract()
	verboseTx.Vout = append(verboseTx.Vout, testVout(1, pkScript))
	// Make a spending transaction.
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
	// Check that there is an error when trying to retrieve swap details for a
	// non-swap transaction.
	_, _, _, err = dexTx.SwapDetails(0)
	if err == nil {
		t.Fatalf("no error when retreiving swap details for non-swap output")
	}
	sendAddr, receiveAddr, sentVal, err := dexTx.SwapDetails(1)
	if err != nil {
		t.Fatalf("received an error getting swap details: %v", err)
	}
	if sendAddr != sender {
		t.Fatalf("expected sender address %s, got %s", sender, sendAddr)
	}
	if receiveAddr != receiver {
		t.Fatalf("expected receiver address %s, got %s", receiver, receiveAddr)
	}
	if sentVal != 1e8 {
		t.Fatalf("expected sent value 1e8, got %d", sentVal)
	}
	// Now do an invalidating reorg, and check that Confirmations returns an
	// error.
	newHash := testAddBlockVerbose(nil, 1, blockHeight+1, nil, 0)
	// Passing the hash to anyQ triggers the reorganization.
	dcr.anyQ <- newHash
	time.Sleep(time.Millisecond * 50)
	_, err = dexTx.Confirmations()
	if err == nil {
		t.Fatalf("no error when checking confirmations on an invalidated tx")
	}
	// Make it a stake transaction and make sure the transaction is valid again,
	// since stake transactions are not invalidated by stakeholders.
	testAddBlockVerbose(blockHash, 1, blockHeight, []string{txHash.String()}, 1)
	block := testChain.blocks[*blockHash]
	_, err = dcr.blockCache.add(block)
	if err != nil {
		t.Fatalf("case 5 - error adding to block cache: %v", err)
	}
	_, err = dexTx.Confirmations()
	if err != nil {
		t.Fatalf("error for stake transaction in stakeholder-invalidated block: %v", err)
	}

	// Test 2: Before and after mining.
	cleanTestChain()
	dcr.blockCache = newBlockCache()
	txHash = randomHash()
	// Add a transaction to mempool.
	testAddTxVerbose(txHash, nil, 0, 0, vout)
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
	blockHash = testAddBlockVerbose(nil, 1, blockHeight, nil, 0)
	// Update the verbose tx data
	testAddTxVerbose(txHash, blockHash, blockHeight, 1, vout)
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
