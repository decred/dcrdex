//go:build !spvlive
// +build !spvlive

// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package btc

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"decred.org/dcrdex/dex"
	dexbtc "decred.org/dcrdex/dex/networks/btc"
	"github.com/btcsuite/btcd/btcec"
	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/peer"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
	"github.com/btcsuite/btcutil"
	"github.com/btcsuite/btcutil/gcs"
	"github.com/btcsuite/btcutil/gcs/builder"
	"github.com/btcsuite/btcutil/psbt"
	"github.com/btcsuite/btcwallet/chain"
	"github.com/btcsuite/btcwallet/waddrmgr"
	"github.com/btcsuite/btcwallet/wallet"
	"github.com/btcsuite/btcwallet/walletdb"
	"github.com/btcsuite/btcwallet/wtxmgr"
	"github.com/lightninglabs/neutrino"
	"github.com/lightninglabs/neutrino/headerfs"
)

type tBtcWallet struct {
	*testData
}

func (c *tBtcWallet) PublishTransaction(tx *wire.MsgTx, label string) error {
	c.sentRawTx = tx
	if c.sendErr != nil {
		return c.sendErr
	}
	if c.sendToAddressErr != nil {
		return c.sendToAddressErr
	}
	if c.badSendHash != nil {
		// testData would return the badSendHash. We'll do something similar
		// by adding a random output.
		tx.AddTxOut(wire.NewTxOut(1, []byte{0x01}))
	}
	return c.sendErr
}

func (c *tBtcWallet) CalculateAccountBalances(account uint32, confirms int32) (wallet.Balances, error) {
	if c.getBalancesErr != nil {
		return wallet.Balances{}, c.getBalancesErr
	}
	bal := &c.getBalances.Mine
	mustAmount := func(v float64) btcutil.Amount {
		amt, _ := btcutil.NewAmount(v)
		return amt
	}
	return wallet.Balances{
		Total:          mustAmount(bal.Trusted + bal.Untrusted),
		Spendable:      mustAmount(bal.Trusted + bal.Untrusted),
		ImmatureReward: mustAmount(bal.Immature),
	}, nil
}

func (c *tBtcWallet) ListUnspent(minconf, maxconf int32, acctName string) ([]*btcjson.ListUnspentResult, error) {
	if c.listUnspentErr != nil {
		return nil, c.listUnspentErr
	}
	unspents := make([]*btcjson.ListUnspentResult, 0, len(c.listUnspent))
	for _, u := range c.listUnspent {
		unspents = append(unspents, &btcjson.ListUnspentResult{
			TxID:    u.TxID,
			Vout:    u.Vout,
			Address: u.Address,
			// Account: ,
			ScriptPubKey:  u.ScriptPubKey.String(),
			RedeemScript:  u.RedeemScript.String(),
			Amount:        u.Amount,
			Confirmations: int64(u.Confirmations),
			Spendable:     u.Spendable,
		})
	}

	return unspents, nil
}

func (c *tBtcWallet) FetchInputInfo(prevOut *wire.OutPoint) (*wire.MsgTx, *wire.TxOut, *psbt.Bip32Derivation, int64, error) {
	return c.fetchInputInfoTx, nil, nil, 0, nil
}

func (c *tBtcWallet) ResetLockedOutpoints() {}

func (c *tBtcWallet) LockOutpoint(op wire.OutPoint) {
	c.lockedCoins = []*RPCOutpoint{{
		TxID: op.Hash.String(),
		Vout: op.Index,
	}}
}

func (c *tBtcWallet) UnlockOutpoint(op wire.OutPoint) {}

func (c *tBtcWallet) LockedOutpoints() []btcjson.TransactionInput {
	unspents := make([]btcjson.TransactionInput, 0)
	for _, u := range c.listLockUnspent {
		unspents = append(unspents, btcjson.TransactionInput{
			Txid: u.TxID,
			Vout: u.Vout,
		})
	}

	return unspents
}

func (c *tBtcWallet) NewChangeAddress(account uint32, scope waddrmgr.KeyScope) (btcutil.Address, error) {
	if c.changeAddrErr != nil {
		return nil, c.changeAddrErr
	}
	return btcutil.DecodeAddress(c.changeAddr, &chaincfg.MainNetParams)
}

func (c *tBtcWallet) NewAddress(account uint32, scope waddrmgr.KeyScope) (btcutil.Address, error) {
	if c.newAddressErr != nil {
		return nil, c.newAddressErr
	}
	return btcutil.DecodeAddress(c.newAddress, &chaincfg.MainNetParams)
}

func (c *tBtcWallet) SignTransaction(tx *wire.MsgTx, hashType txscript.SigHashType, additionalPrevScriptsadditionalPrevScripts map[wire.OutPoint][]byte,
	additionalKeysByAddress map[string]*btcutil.WIF, p2shRedeemScriptsByAddress map[string][]byte) ([]wallet.SignatureError, error) {

	if c.signTxErr != nil {
		return nil, c.signTxErr
	}
	c.signFunc(tx)
	if c.sigIncomplete {
		return []wallet.SignatureError{{Error: errors.New("tBtcWallet SignTransaction error")}}, nil
	}
	return nil, nil
}

func (c *tBtcWallet) PrivKeyForAddress(a btcutil.Address) (*btcec.PrivateKey, error) {
	if c.privKeyForAddrErr != nil {
		return nil, c.privKeyForAddrErr
	}
	return c.privKeyForAddr.PrivKey, nil
}

func (c *tBtcWallet) Database() walletdb.DB {
	return nil
}

func (c *tBtcWallet) Unlock(passphrase []byte, lock <-chan time.Time) error {
	return c.unlockErr
}

func (c *tBtcWallet) Lock() {}

func (c *tBtcWallet) Locked() bool {
	return false
}

func (c *tBtcWallet) SendOutputs(outputs []*wire.TxOut, keyScope *waddrmgr.KeyScope, account uint32, minconf int32, satPerKb btcutil.Amount, label string) (*wire.MsgTx, error) {
	if c.sendToAddressErr != nil {
		return nil, c.sendToAddressErr
	}
	tx := wire.NewMsgTx(wire.TxVersion)
	for _, txOut := range outputs {
		tx.AddTxOut(txOut)
	}
	tx.AddTxIn(dummyInput())

	return tx, nil
}

func (c *tBtcWallet) HaveAddress(a btcutil.Address) (bool, error) {
	return false, nil
}

func (c *tBtcWallet) Stop() {}

func (c *tBtcWallet) WaitForShutdown() {}

func (c *tBtcWallet) ChainSynced() bool {
	if c.getBlockchainInfo == nil {
		return false
	}
	return c.getBlockchainInfo.Blocks >= c.getBlockchainInfo.Headers-1
}

func (c *tBtcWallet) SynchronizeRPC(chainClient chain.Interface) {}

func (c *tBtcWallet) walletTransaction(txHash *chainhash.Hash) (*wtxmgr.TxDetails, error) {
	if c.getTransactionErr != nil {
		return nil, c.getTransactionErr
	}
	if c.testData.getTransaction == nil {
		return nil, WalletTransactionNotFound
	}

	txData := c.testData.getTransaction
	tx, _ := msgTxFromBytes(txData.Hex)

	blockHash, _ := chainhash.NewHashFromStr(txData.BlockHash)

	blk := c.getBlock(txData.BlockHash)

	credits := make([]wtxmgr.CreditRecord, 0, len(tx.TxIn))
	for i := range tx.TxIn {
		credits = append(credits, wtxmgr.CreditRecord{
			// Amount:,
			Index: uint32(i),
			Spent: c.walletTxSpent,
			// Change: ,
		})
	}

	return &wtxmgr.TxDetails{
		TxRecord: wtxmgr.TxRecord{
			MsgTx: *tx,
		},
		Block: wtxmgr.BlockMeta{
			Block: wtxmgr.Block{
				Hash:   *blockHash,
				Height: int32(blk.height),
			},
		},
		Credits: credits,
	}, nil
}

func (c *tBtcWallet) getTransaction(txHash *chainhash.Hash) (*GetTransactionResult, error) {
	if c.getTransactionErr != nil {
		return nil, c.getTransactionErr
	}
	return c.testData.getTransaction, nil
}

func (c *tBtcWallet) syncedTo() waddrmgr.BlockStamp {
	bestHash, bestHeight := c.bestBlock()
	blk := c.getBlock(bestHash.String())
	return waddrmgr.BlockStamp{
		Height:    int32(bestHeight),
		Hash:      *bestHash,
		Timestamp: blk.msgBlock.Header.Timestamp,
	}
}

func (c *tBtcWallet) signTransaction(tx *wire.MsgTx) error {
	if c.signTxErr != nil {
		return c.signTxErr
	}
	c.signFunc(tx)
	if c.sigIncomplete {
		return errors.New("tBtcWallet SignTransaction error")
	}
	return nil
}

type tNeutrinoClient struct {
	*testData
}

func (c *tNeutrinoClient) Stop() error { return nil }

func (c *tNeutrinoClient) GetBlockHash(blockHeight int64) (*chainhash.Hash, error) {
	c.blockchainMtx.RLock()
	defer c.blockchainMtx.RUnlock()
	for height, blockHash := range c.mainchain {
		if height == blockHeight {
			return blockHash, nil
		}
	}
	return nil, fmt.Errorf("no (test) block at height %d", blockHeight)
}

func (c *tNeutrinoClient) BestBlock() (*headerfs.BlockStamp, error) {
	if c.getBestBlockHashErr != nil {
		return nil, c.getBestBlockHashErr
	}
	bestHash, bestHeight := c.bestBlock()
	return &headerfs.BlockStamp{
		Height: int32(bestHeight),
		Hash:   *bestHash,
	}, nil
}

func (c *tNeutrinoClient) Peers() []*neutrino.ServerPeer {
	peer := &neutrino.ServerPeer{Peer: &peer.Peer{}}
	if c.getBlockchainInfo != nil {
		peer.UpdateLastBlockHeight(int32(c.getBlockchainInfo.Headers))
	}
	return []*neutrino.ServerPeer{peer}
}

func (c *tNeutrinoClient) GetBlockHeight(hash *chainhash.Hash) (int32, error) {
	block := c.getBlock(hash.String())
	if block == nil {
		return 0, fmt.Errorf("(test) block not found for block hash %s", hash)
	}
	return int32(block.height), nil
}

func (c *tNeutrinoClient) GetBlockHeader(blkHash *chainhash.Hash) (*wire.BlockHeader, error) {
	block := c.getBlock(blkHash.String())
	if block == nil {
		return nil, fmt.Errorf("no block verbose found")
	}
	return &block.msgBlock.Header, nil
}

func (c *tNeutrinoClient) GetCFilter(blockHash chainhash.Hash, filterType wire.FilterType, options ...neutrino.QueryOption) (*gcs.Filter, error) {
	var key [gcs.KeySize]byte
	copy(key[:], blockHash.CloneBytes()[:])
	return gcs.BuildGCSFilter(builder.DefaultP, builder.DefaultM, key, c.getCFilterScripts[blockHash])
}

func (c *tNeutrinoClient) GetBlock(blockHash chainhash.Hash, options ...neutrino.QueryOption) (*btcutil.Block, error) {
	blk := c.getBlock(blockHash.String())
	if blk == nil {
		return nil, fmt.Errorf("no (test) block %s", blockHash)
	}
	return btcutil.NewBlock(blk.msgBlock), nil
}

type tSPVPeer struct {
	startHeight, lastHeight int32
}

func (p *tSPVPeer) StartingHeight() int32 {
	return p.startHeight
}

func (p *tSPVPeer) LastBlock() int32 {
	return p.lastHeight
}

func TestSwapConfirmations(t *testing.T) {
	wallet, node, shutdown, _ := tNewWallet(true, WalletTypeSPV)
	defer shutdown()

	spv := wallet.node.(*spvWallet)
	node.txOutRes = nil

	const tipHeight = 10
	const swapHeight = 1
	const spendHeight = 3
	const swapConfs = tipHeight - swapHeight + 1

	for i := int64(0); i <= tipHeight; i++ {
		node.addRawTx(i, dummyTx())
	}

	_, _, pkScript, _, _, _, _ := makeSwapContract(true, time.Hour*12)

	swapTx := makeRawTx([]dex.Bytes{pkScript}, []*wire.TxIn{dummyInput()})
	swapTxHash := swapTx.TxHash()
	const vout = 0
	swapOutPt := newOutPoint(&swapTxHash, vout)
	swapBlockHash, _ := node.addRawTx(swapHeight, swapTx)

	spendTx := dummyTx()
	spendTx.TxIn[0].PreviousOutPoint.Hash = swapTxHash
	spendBlockHash, _ := node.addRawTx(spendHeight, spendTx)
	spendTxHash := spendTx.TxHash()

	matchTime := time.Unix(1, 0)

	checkSuccess := func(tag string, expConfs uint32, expSpent bool) {
		t.Helper()
		confs, spent, err := spv.swapConfirmations(&swapTxHash, vout, pkScript, matchTime)
		if err != nil {
			t.Fatalf("%s error: %v", tag, err)
		}
		if confs != expConfs {
			t.Fatalf("wrong number of %s confs. wanted %d, got %d", tag, expConfs, confs)
		}
		if spent != expSpent {
			t.Fatalf("%s path not expected spent status. wanted %t, got %t", tag, expSpent, spent)
		}
	}

	checkFailure := func(tag string) {
		t.Helper()
		_, _, err := spv.swapConfirmations(&swapTxHash, vout, pkScript, matchTime)
		if err == nil {
			t.Fatalf("no error for %q test", tag)
		}
	}

	// confirmations() path
	node.confsErr = tErr
	checkFailure("confirmations")

	node.confsErr = nil
	node.confs = 10
	node.confsSpent = true
	txB, _ := serializeMsgTx(swapTx)
	node.getTransaction = &GetTransactionResult{
		BlockHash:  swapBlockHash.String(),
		BlockIndex: swapHeight,
		Hex:        txB,
	}
	node.walletTxSpent = true
	checkSuccess("confirmations", swapConfs, true)
	node.getTransaction = nil
	node.walletTxSpent = false
	node.confsErr = WalletTransactionNotFound

	// DB path.
	node.dbBlockForTx[swapTxHash] = &hashEntry{hash: *swapBlockHash}
	node.dbBlockForTx[spendTxHash] = &hashEntry{hash: *spendBlockHash}
	node.checkpoints[swapOutPt] = &scanCheckpoint{res: &filterScanResult{
		blockHash:   swapBlockHash,
		blockHeight: swapHeight,
		spend:       &spendingInput{},
		checkpoint:  *spendBlockHash,
	}}
	checkSuccess("GetSpend", swapConfs, true)
	delete(node.checkpoints, swapOutPt)
	delete(node.dbBlockForTx, swapTxHash)

	// Neutrino scan

	// Fail cuz no match time provided and block not known.
	matchTime = time.Time{}
	checkFailure("no match time")
	matchTime = time.Unix(1, 0)

	// spent
	node.getCFilterScripts[*spendBlockHash] = [][]byte{pkScript}
	delete(node.checkpoints, swapOutPt)
	checkSuccess("spend", 0, true)
	node.getCFilterScripts[*spendBlockHash] = nil

	// Not found
	delete(node.checkpoints, swapOutPt)
	checkFailure("no utxo")

	// Found.
	node.getCFilterScripts[*swapBlockHash] = [][]byte{pkScript}
	delete(node.checkpoints, swapOutPt)
	checkSuccess("scan find", swapConfs, false)
}

func TestFindBlockForTime(t *testing.T) {
	wallet, node, shutdown, _ := tNewWallet(true, WalletTypeSPV)
	defer shutdown()
	spv := wallet.node.(*spvWallet)

	const tipHeight = 40

	for i := 0; i <= tipHeight; i++ {
		node.addRawTx(int64(i), dummyTx())
	}

	const searchBlock = 35
	matchTime := generateTestBlockTime(searchBlock)
	const offsetBlock = searchBlock - testBlocksPerBlockTimeOffset
	const startBlock = offsetBlock - medianTimeBlocks
	_, height, err := spv.findBlockForTime(matchTime)
	if err != nil {
		t.Fatalf("findBlockForTime error: %v", err)
	}
	if height != startBlock {
		t.Fatalf("wrong height. wanted %d, got %d", startBlock, height)
	}

	// But if we shift the startBlock time to > offsetBlock time, the window
	// will continue down 11 more.
	_, blk := node.getBlockAtHeight(startBlock)
	blk.msgBlock.Header.Timestamp = generateTestBlockTime(offsetBlock)
	_, height, err = spv.findBlockForTime(matchTime)
	if err != nil {
		t.Fatalf("findBlockForTime error for shifted start block: %v", err)
	}
	if height != startBlock-medianTimeBlocks {
		t.Fatalf("wrong height. wanted %d, got %d", startBlock-11, height)
	}

	// And doing an early enough block just returns genesis
	blockHash, height, err := spv.findBlockForTime(generateTestBlockTime(10))
	if err != nil {
		t.Fatalf("findBlockForTime error for genesis test: %v", err)
	}
	if *blockHash != *chaincfg.MainNetParams.GenesisHash {
		t.Fatalf("not genesis: height = %d", height)
	}

	// A time way in the future still returns at least the last 11 blocks.
	_, height, err = spv.findBlockForTime(generateTestBlockTime(100))
	if err != nil {
		t.Fatalf("findBlockForTime error for future test: %v", err)
	}
	// +1 because tip block is included here, as opposed to the shifted start
	// block, where the shifted block wasn't included.
	if height != tipHeight-medianTimeBlocks+1 {
		t.Fatalf("didn't get tip - 11. wanted %d, got %d", tipHeight-medianTimeBlocks, height)
	}
}

func TestGetTxOut(t *testing.T) {
	wallet, node, shutdown, _ := tNewWallet(true, WalletTypeSPV)
	defer shutdown()
	spv := wallet.node.(*spvWallet)

	_, _, pkScript, _, _, _, _ := makeSwapContract(true, time.Hour*12)
	const vout = 0
	const blockHeight = 10
	const tipHeight = 20
	tx := makeRawTx([]dex.Bytes{pkScript}, []*wire.TxIn{dummyInput()})
	txHash := tx.TxHash()
	outPt := newOutPoint(&txHash, vout)
	blockHash, _ := node.addRawTx(blockHeight, tx)
	txB, _ := serializeMsgTx(tx)
	node.addRawTx(tipHeight, dummyTx())
	spendingTx := dummyTx()
	spendingTx.TxIn[0].PreviousOutPoint.Hash = txHash
	spendBlockHash, _ := node.addRawTx(tipHeight-1, spendingTx)

	// Prime the db
	for h := int64(1); h <= tipHeight; h++ {
		node.addRawTx(h, dummyTx())
	}

	// Abnormal error
	node.getTransactionErr = tErr
	_, _, err := spv.getTxOut(&txHash, vout, pkScript, generateTestBlockTime(blockHeight))
	if err == nil {
		t.Fatalf("no error for getWalletTransaction error")
	}

	// Wallet transaction found
	node.getTransactionErr = nil
	node.getTransaction = &GetTransactionResult{
		BlockHash: blockHash.String(),
		Hex:       txB,
	}

	_, confs, err := spv.getTxOut(&txHash, vout, pkScript, generateTestBlockTime(blockHeight))
	if err != nil {
		t.Fatalf("error for wallet transaction found: %v", err)
	}
	if confs != tipHeight-blockHeight+1 {
		t.Fatalf("wrong confs for wallet transaction. wanted %d, got %d", tipHeight-blockHeight+1, confs)
	}

	// No wallet transaction, but we have a spend recorded.
	node.getTransactionErr = WalletTransactionNotFound
	node.getTransaction = nil
	node.checkpoints[outPt] = &scanCheckpoint{res: &filterScanResult{
		blockHash:  blockHash,
		spend:      &spendingInput{},
		checkpoint: *spendBlockHash,
	}}
	op, confs, err := spv.getTxOut(&txHash, vout, pkScript, generateTestBlockTime(blockHeight))
	if op != nil || confs != 0 || err != nil {
		t.Fatal("wrong result for spent txout", op != nil, confs, err)
	}
	delete(node.checkpoints, outPt)

	// no spend record. gotta scan

	// case 1: we have a block hash in the database
	node.dbBlockForTx[txHash] = &hashEntry{hash: *blockHash}
	node.getCFilterScripts[*blockHash] = [][]byte{pkScript}
	_, _, err = spv.getTxOut(&txHash, vout, pkScript, generateTestBlockTime(blockHeight))
	if err != nil {
		t.Fatalf("error for GetUtxo with cached hash: %v", err)
	}

	// case 2: no block hash in db. Will do scan and store them.
	delete(node.dbBlockForTx, txHash)
	delete(node.checkpoints, outPt)
	_, _, err = spv.getTxOut(&txHash, vout, pkScript, generateTestBlockTime(blockHeight))
	if err != nil {
		t.Fatalf("error for GetUtxo with no cached hash: %v", err)
	}
	if _, inserted := node.dbBlockForTx[txHash]; !inserted {
		t.Fatalf("db not updated after GetUtxo scan success")
	}

	// case 3: spending tx found first
	delete(node.checkpoints, outPt)
	node.getCFilterScripts[*spendBlockHash] = [][]byte{pkScript}
	txOut, _, err := spv.getTxOut(&txHash, vout, pkScript, generateTestBlockTime(blockHeight))
	if err != nil {
		t.Fatalf("error for spent tx: %v", err)
	}
	if txOut != nil {
		t.Fatalf("spend output returned from getTxOut")
	}

	// Make sure we can find it with the checkpoint.
	node.checkpoints[outPt].res.spend = nil
	node.getCFilterScripts[*spendBlockHash] = nil
	// We won't actually scan for the output itself, so nil'ing these should
	// have no effect.
	node.getCFilterScripts[*blockHash] = nil
	_, _, err = spv.getTxOut(&txHash, vout, pkScript, generateTestBlockTime(blockHeight))
	if err != nil {
		t.Fatalf("error for checkpointed output: %v", err)
	}
}

func TestSendWithSubtract(t *testing.T) {
	wallet, node, shutdown, _ := tNewWallet(true, WalletTypeSPV)
	defer shutdown()
	spv := wallet.node.(*spvWallet)

	const availableFunds = 5e8
	const feeRate = 100
	const inputSize = dexbtc.RedeemP2WPKHInputSize + ((dexbtc.RedeemP2WPKHInputWitnessWeight + 2 + 3) / 4)
	const feesWithChange = (dexbtc.MinimumTxOverhead + 2*dexbtc.P2WPKHOutputSize + inputSize) * feeRate
	const feesWithoutChange = (dexbtc.MinimumTxOverhead + dexbtc.P2WPKHOutputSize + inputSize) * feeRate

	addr, _ := btcutil.DecodeAddress(tP2WPKHAddr, &chaincfg.MainNetParams)
	pkScript, _ := txscript.PayToAddrScript(addr)

	node.changeAddr = tP2WPKHAddr
	node.signFunc = func(tx *wire.MsgTx) {
		signFunc(tx, 0, true)
	}
	node.listUnspent = []*ListUnspentResult{{
		TxID:          tTxID,
		Address:       tP2WPKHAddr,
		Confirmations: 5,
		ScriptPubKey:  pkScript,
		Spendable:     true,
		Solvable:      true,
		Safe:          true,
		Amount:        float64(availableFunds) / 1e8,
	}}

	test := func(req, expVal int64, expChange bool) {
		t.Helper()
		_, err := spv.sendWithSubtract(pkScript, uint64(req), feeRate)
		if err != nil {
			t.Fatalf("half withdraw error: %v", err)
		}
		opCount := len(node.sentRawTx.TxOut)
		if (opCount == 1 && expChange) || (opCount == 2 && !expChange) {
			t.Fatalf("%d outputs when expChange = %t", opCount, expChange)
		}
		received := node.sentRawTx.TxOut[opCount-1].Value
		if received != expVal {
			t.Fatalf("wrong value received. expected %d, got %d", expVal, received)
		}
	}

	// No change
	var req int64 = availableFunds / 2
	test(req, req-feesWithChange, true)

	// Drain it
	test(availableFunds, availableFunds-feesWithoutChange, false)

	// Requesting just a little less shouldn't result in a reduction of the
	// amount received, since the change would be dust.
	test(availableFunds-10, availableFunds-feesWithoutChange, false)

	// Requesting too

	// listUnspent error
	node.listUnspentErr = tErr
	_, err := spv.sendWithSubtract(pkScript, availableFunds/2, feeRate)
	if err == nil {
		t.Fatalf("test passed with listUnspent error")
	}
	node.listUnspentErr = nil

	node.changeAddrErr = tErr
	_, err = spv.sendWithSubtract(pkScript, availableFunds/2, feeRate)
	if err == nil {
		t.Fatalf("test passed with NewChangeAddress error")
	}
	node.changeAddrErr = nil

	node.signTxErr = tErr
	_, err = spv.sendWithSubtract(pkScript, availableFunds/2, feeRate)
	if err == nil {
		t.Fatalf("test passed with SignTransaction error")
	}
	node.signTxErr = nil

	// outrageous fees
	_, err = spv.sendWithSubtract(pkScript, availableFunds/2, 1e8)
	if err == nil {
		t.Fatalf("test passed with fees > available error")
	}
}
