//go:build !spvlive && !harness

// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package btc

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/encode"
	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/btcutil/gcs"
	"github.com/btcsuite/btcd/btcutil/gcs/builder"
	"github.com/btcsuite/btcd/btcutil/psbt"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/peer"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
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

func (c *tBtcWallet) ListSinceBlock(start, end, syncHeight int32) ([]btcjson.ListTransactionsResult, error) {
	return nil, nil
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
		return errors.New("bad hash")
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
	if c.lockedCoins != nil {
		// check if already locked
		for _, l := range c.lockedCoins {
			if l.TxID == op.Hash.String() && l.Vout == op.Index {
				return
			}
		}

		c.lockedCoins = append(c.lockedCoins, &RPCOutpoint{
			TxID: op.Hash.String(),
			Vout: op.Index,
		})
		return
	}

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
	return c.locked
}

func (c *tBtcWallet) SendOutputs(outputs []*wire.TxOut, keyScope *waddrmgr.KeyScope,
	account uint32, minconf int32, satPerKb btcutil.Amount,
	coinSelectionStrategy wallet.CoinSelectionStrategy, label string) (*wire.MsgTx, error) {
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
	if c.ownedAddresses != nil && c.ownedAddresses[a.String()] {
		return true, nil
	}
	return c.ownsAddress, nil
}

func (c *tBtcWallet) Stop() {}

func (c *tBtcWallet) Start() (SPVService, error) {
	return nil, nil
}

func (c *tBtcWallet) WaitForShutdown() {}

func (c *tBtcWallet) ChainSynced() bool {
	c.blockchainMtx.RLock()
	defer c.blockchainMtx.RUnlock()
	if c.getBlockchainInfo == nil {
		return false
	}
	return c.getBlockchainInfo.Blocks >= c.getBlockchainInfo.Headers // -1 ok for chain sync ?
}

func (c *tBtcWallet) SynchronizeRPC(chainClient chain.Interface) {}

func (c *tBtcWallet) WalletTransaction(txHash *chainhash.Hash) (*wtxmgr.TxDetails, error) {
	if c.getTransactionErr != nil {
		return nil, c.getTransactionErr
	}
	var txData *GetTransactionResult
	if c.getTransactionMap != nil {
		if txData = c.getTransactionMap["any"]; txData == nil {
			txData = c.getTransactionMap[txHash.String()]
		}
	}
	if txData == nil {
		return nil, WalletTransactionNotFound
	}

	tx, _ := msgTxFromBytes(txData.Bytes)
	blockHash, _ := chainhash.NewHashFromStr(txData.BlockHash)

	blk := c.getBlock(*blockHash)
	var blockHeight int32
	if blk != nil {
		blockHeight = int32(blk.height)
	} else {
		blockHeight = -1
	}

	credits := make([]wtxmgr.CreditRecord, 0, len(tx.TxIn))
	debits := make([]wtxmgr.DebitRecord, 0, len(tx.TxIn))
	for i, in := range tx.TxIn {
		credits = append(credits, wtxmgr.CreditRecord{
			// Amount:,
			Index: uint32(i),
			Spent: c.walletTxSpent,
			// Change: ,
		})

		var debitAmount int64
		// The sources of transaction inputs all need to be added to getTransactionMap
		// in order to get accurate Fees and Amounts when calling GetWalletTransaction
		// when using the SPV wallet.
		if gtr := c.getTransactionMap[in.PreviousOutPoint.Hash.String()]; gtr != nil {
			tx, _ := msgTxFromBytes(gtr.Bytes)
			debitAmount = tx.TxOut[in.PreviousOutPoint.Index].Value
		}

		debits = append(debits, wtxmgr.DebitRecord{
			Amount: btcutil.Amount(debitAmount),
		})

	}
	return &wtxmgr.TxDetails{
		TxRecord: wtxmgr.TxRecord{
			MsgTx:    *tx,
			Received: time.Unix(int64(txData.Time), 0),
		},
		Block: wtxmgr.BlockMeta{
			Block: wtxmgr.Block{
				Hash:   *blockHash,
				Height: blockHeight,
			},
		},
		Credits: credits,
		Debits:  debits,
	}, nil
}

func (c *tBtcWallet) SyncedTo() waddrmgr.BlockStamp {
	bestHash, bestHeight := c.bestBlock() // NOTE: in reality this may be lower than the chain service's best block
	blk := c.getBlock(*bestHash)
	return waddrmgr.BlockStamp{
		Height:    int32(bestHeight),
		Hash:      *bestHash,
		Timestamp: blk.msgBlock.Header.Timestamp,
	}
}

func (c *tBtcWallet) SignTx(tx *wire.MsgTx) error {
	if c.signTxErr != nil {
		return c.signTxErr
	}
	c.signFunc(tx)
	if c.sigIncomplete {
		return errors.New("tBtcWallet SignTransaction error")
	}
	return nil
}

func (c *tBtcWallet) AccountProperties(scope waddrmgr.KeyScope, acct uint32) (*waddrmgr.AccountProperties, error) {
	return nil, nil
}

func (c *tBtcWallet) BlockNotifications(ctx context.Context) <-chan *BlockNotification {
	return nil
}

func (c *tBtcWallet) ForceRescan() {}

func (c *tBtcWallet) RescanAsync() error { return nil }

func (c *tBtcWallet) Birthday() time.Time {
	return time.Time{}
}

func (c *tBtcWallet) Reconfigure(*asset.WalletConfig, string) (bool, error) {
	return false, nil
}

func (c *tBtcWallet) Peers() ([]*asset.WalletPeer, error) {
	return nil, nil
}
func (c *tBtcWallet) AddPeer(string) error {
	return nil
}
func (c *tBtcWallet) RemovePeer(string) error {
	return nil
}

func (c *tBtcWallet) TotalReceivedForAddr(addr btcutil.Address, minConf int32) (btcutil.Amount, error) {
	return 0, nil
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
	c.blockchainMtx.RLock()
	if c.getBestBlockHashErr != nil {
		defer c.blockchainMtx.RUnlock() // reading c.getBestBlockHashErr below
		return nil, c.getBestBlockHashErr
	}
	c.blockchainMtx.RUnlock()
	bestHash, bestHeight := c.bestBlock()
	blk := c.getBlock(*bestHash)
	return &headerfs.BlockStamp{
		Height:    int32(bestHeight),
		Hash:      *bestHash,
		Timestamp: blk.msgBlock.Header.Timestamp, // neutrino sets this as of 88c025e
	}, nil
}

func (c *tNeutrinoClient) Peers() []SPVPeer {
	c.blockchainMtx.RLock()
	defer c.blockchainMtx.RUnlock()
	peer := &neutrino.ServerPeer{Peer: &peer.Peer{}}
	if c.getBlockchainInfo != nil {
		peer.UpdateLastBlockHeight(int32(c.getBlockchainInfo.Headers))
	}
	return []SPVPeer{peer}
}

func (c *tNeutrinoClient) AddPeer(string) error {
	return nil
}

func (c *tNeutrinoClient) GetBlockHeight(hash *chainhash.Hash) (int32, error) {
	block := c.getBlock(*hash)
	if block == nil {
		return 0, fmt.Errorf("(test) block not found for block hash %s", hash)
	}
	return int32(block.height), nil
}

func (c *tNeutrinoClient) GetBlockHeader(blkHash *chainhash.Hash) (*wire.BlockHeader, error) {
	block := c.getBlock(*blkHash)
	if block == nil {
		return nil, errors.New("no block verbose found")
	}
	return &block.msgBlock.Header, nil
}

func (c *tNeutrinoClient) GetCFilter(blockHash chainhash.Hash, filterType wire.FilterType, options ...neutrino.QueryOption) (*gcs.Filter, error) {
	var key [gcs.KeySize]byte
	copy(key[:], blockHash.CloneBytes()[:])
	scripts := c.getCFilterScripts[blockHash]
	scripts = append(scripts, encode.RandomBytes(10))
	return gcs.BuildGCSFilter(builder.DefaultP, builder.DefaultM, key, scripts)
}

func (c *tNeutrinoClient) GetBlock(blockHash chainhash.Hash, options ...neutrino.QueryOption) (*btcutil.Block, error) {
	blk := c.getBlock(blockHash)
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
	wallet, node, shutdown := tNewWallet(true, walletTypeSPV)
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
	swapOutPt := NewOutPoint(&swapTxHash, vout)
	swapBlockHash, _ := node.addRawTx(swapHeight, swapTx)

	spendTx := dummyTx()
	spendTx.TxIn[0].PreviousOutPoint.Hash = swapTxHash
	spendBlockHash, _ := node.addRawTx(spendHeight, spendTx)
	spendTxHash := spendTx.TxHash()

	matchTime := time.Unix(1, 0)

	checkSuccess := func(tag string, expConfs uint32, expSpent bool) {
		t.Helper()
		confs, spent, err := spv.SwapConfirmations(&swapTxHash, vout, pkScript, matchTime)
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
		_, _, err := spv.SwapConfirmations(&swapTxHash, vout, pkScript, matchTime)
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
	node.getTransactionMap = map[string]*GetTransactionResult{
		"any": {
			BlockHash:  swapBlockHash.String(),
			BlockIndex: swapHeight,
			Bytes:      txB,
		}}
	node.walletTxSpent = true
	checkSuccess("confirmations", swapConfs, true)
	node.getTransactionMap = nil
	node.walletTxSpent = false
	node.confsErr = WalletTransactionNotFound

	// DB path.
	node.dbBlockForTx[swapTxHash] = &hashEntry{hash: *swapBlockHash}
	node.dbBlockForTx[spendTxHash] = &hashEntry{hash: *spendBlockHash}
	node.checkpoints[swapOutPt] = &ScanCheckpoint{
		Res: &FilterScanResult{
			BlockHash:   swapBlockHash,
			BlockHeight: swapHeight,
			Spend:       &SpendingInput{},
			Checkpoint:  *spendBlockHash,
		},
	}
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
	wallet, node, shutdown := tNewWallet(true, walletTypeSPV)
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
	height, err := spv.FindBlockForTime(matchTime)
	if err != nil {
		t.Fatalf("FindBlockForTime error: %v", err)
	}
	if height != startBlock {
		t.Fatalf("wrong height. wanted %d, got %d", startBlock, height)
	}

	// But if we shift the startBlock time to > offsetBlock time, the window
	// will continue down 11 more.
	_, blk := node.getBlockAtHeight(startBlock)
	blk.msgBlock.Header.Timestamp = generateTestBlockTime(offsetBlock)
	height, err = spv.FindBlockForTime(matchTime)
	if err != nil {
		t.Fatalf("FindBlockForTime error for shifted start block: %v", err)
	}
	if height != startBlock-medianTimeBlocks {
		t.Fatalf("wrong height. wanted %d, got %d", startBlock-11, height)
	}

	// And doing an early enough block just returns genesis
	height, err = spv.FindBlockForTime(generateTestBlockTime(10))
	if err != nil {
		t.Fatalf("FindBlockForTime error for genesis test: %v", err)
	}
	if height != 0 {
		t.Fatalf("not genesis: height = %d", height)
	}

	// A time way in the future still returns at least the last 11 blocks.
	height, err = spv.FindBlockForTime(generateTestBlockTime(100))
	if err != nil {
		t.Fatalf("FindBlockForTime error for future test: %v", err)
	}
	// +1 because tip block is included here, as opposed to the shifted start
	// block, where the shifted block wasn't included.
	if height != tipHeight-medianTimeBlocks+1 {
		t.Fatalf("didn't get tip - 11. wanted %d, got %d", tipHeight-medianTimeBlocks, height)
	}
}

func TestGetTxOut(t *testing.T) {
	wallet, node, shutdown := tNewWallet(true, walletTypeSPV)
	defer shutdown()
	spv := wallet.node.(*spvWallet)

	_, _, pkScript, _, _, _, _ := makeSwapContract(true, time.Hour*12)
	const vout = 0
	const blockHeight = 10
	const tipHeight = 20
	tx := makeRawTx([]dex.Bytes{pkScript}, []*wire.TxIn{dummyInput()})
	txHash := tx.TxHash()
	outPt := NewOutPoint(&txHash, vout)
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
	_, _, err := spv.GetTxOut(&txHash, vout, pkScript, generateTestBlockTime(blockHeight))
	if err == nil {
		t.Fatalf("no error for getWalletTransaction error")
	}

	// Wallet transaction found
	node.getTransactionErr = nil
	node.getTransactionMap = map[string]*GetTransactionResult{"any": {
		BlockHash: blockHash.String(),
		Bytes:     txB,
	}}

	_, confs, err := spv.GetTxOut(&txHash, vout, pkScript, generateTestBlockTime(blockHeight))
	if err != nil {
		t.Fatalf("error for wallet transaction found: %v", err)
	}
	if confs != tipHeight-blockHeight+1 {
		t.Fatalf("wrong confs for wallet transaction. wanted %d, got %d", tipHeight-blockHeight+1, confs)
	}

	// No wallet transaction, but we have a spend recorded.
	node.getTransactionErr = WalletTransactionNotFound
	node.getTransactionMap = nil
	node.checkpoints[outPt] = &ScanCheckpoint{
		Res: &FilterScanResult{
			BlockHash:  blockHash,
			Spend:      &SpendingInput{},
			Checkpoint: *spendBlockHash,
		}}
	op, confs, err := spv.GetTxOut(&txHash, vout, pkScript, generateTestBlockTime(blockHeight))
	if op != nil || confs != 0 || err != nil {
		t.Fatal("wrong result for spent txout", op != nil, confs, err)
	}
	delete(node.checkpoints, outPt)

	// no spend record. gotta scan

	// case 1: we have a block hash in the database
	node.dbBlockForTx[txHash] = &hashEntry{hash: *blockHash}
	node.getCFilterScripts[*blockHash] = [][]byte{pkScript}
	_, _, err = spv.GetTxOut(&txHash, vout, pkScript, generateTestBlockTime(blockHeight))
	if err != nil {
		t.Fatalf("error for GetUtxo with cached hash: %v", err)
	}

	// case 2: no block hash in db. Will do scan and store them.
	delete(node.dbBlockForTx, txHash)
	delete(node.checkpoints, outPt)
	_, _, err = spv.GetTxOut(&txHash, vout, pkScript, generateTestBlockTime(blockHeight))
	if err != nil {
		t.Fatalf("error for GetUtxo with no cached hash: %v", err)
	}
	if _, inserted := node.dbBlockForTx[txHash]; !inserted {
		t.Fatalf("db not updated after GetUtxo scan success")
	}

	// case 3: spending tx found first
	delete(node.checkpoints, outPt)
	node.getCFilterScripts[*spendBlockHash] = [][]byte{pkScript}
	txOut, _, err := spv.GetTxOut(&txHash, vout, pkScript, generateTestBlockTime(blockHeight))
	if err != nil {
		t.Fatalf("error for spent tx: %v", err)
	}
	if txOut != nil {
		t.Fatalf("spend output returned from getTxOut")
	}

	// Make sure we can find it with the checkpoint.
	node.checkpoints[outPt].Res.Spend = nil
	node.getCFilterScripts[*spendBlockHash] = nil
	// We won't actually scan for the output itself, so nil'ing these should
	// have no effect.
	node.getCFilterScripts[*blockHash] = nil
	_, _, err = spv.GetTxOut(&txHash, vout, pkScript, generateTestBlockTime(blockHeight))
	if err != nil {
		t.Fatalf("error for checkpointed output: %v", err)
	}
}

func TestTryBlocksWithNotifier(t *testing.T) {
	defaultWalletBlockAllowance := walletBlockAllowance
	defaultBlockTicker := blockTicker

	walletBlockAllowance = 50 * time.Millisecond
	blockTicker = 20 * time.Millisecond

	defer func() {
		walletBlockAllowance = defaultWalletBlockAllowance
		blockTicker = defaultBlockTicker
	}()

	wallet, node, shutdown := tNewWallet(true, walletTypeSPV)
	defer shutdown()

	spv := wallet.node.(*spvWallet)

	getNote := func(timeout time.Duration) bool {
		select {
		case <-node.tipChanged:
			return true
		case <-time.After(timeout):
			return false
		}
	}

	if getNote(walletBlockAllowance * 2) {
		t.Fatalf("got a first block")
	}

	// Feed (*baseWallet).watchBlocks:
	// (*spvWallet).getBestBlockHeader -> (*spvWallet).getBestBlockHash -> (*tBtcWallet).syncedTo -> (*testData).bestBlock [mainchain]
	// (*spvWallet).getBestBlockHeader -> (*spvWallet).getBlockHeader -> (*testData).getBlock [verboseBlocks]

	var tipHeight int64
	addBlock := func() *BlockVector { // update the mainchain and verboseBlocks testData fields
		tipHeight++
		h, _ := node.addRawTx(tipHeight, dummyTx())
		return &BlockVector{tipHeight, *h}
	}

	// Start with no blocks so that we're not synced.
	node.blockchainMtx.Lock()
	node.getBlockchainInfo = &GetBlockchainInfoResult{
		Headers: 3,
		Blocks:  0,
	}
	node.blockchainMtx.Unlock()

	addBlock()

	if !getNote(blockTicker * 2) {
		t.Fatalf("0th block didn't send tip change update")
	}

	addBlock()

	// It should not come through on the block tick, since it will be queued.
	if getNote(blockTicker * 2) {
		t.Fatalf("got non-0th block that should've been queued")
	}

	// And it won't come through after a single block allowance, because we're
	// not synced.
	if getNote(walletBlockAllowance * 2) {
		t.Fatal("block didn't wait for the syncing mode allowance")
	}

	// But it will come through after the sync timeout = 10 * normal timeout.
	if !getNote(walletBlockAllowance * 9) {
		t.Fatal("block didn't time out in syncing mode")
	}

	// But if we're synced, it should come through after the normal block
	// allowance.
	addBlock()
	node.blockchainMtx.Lock()
	node.getBlockchainInfo = &GetBlockchainInfoResult{
		Headers: tipHeight,
		Blocks:  tipHeight,
	}
	node.blockchainMtx.Unlock()
	if !getNote(walletBlockAllowance * 2) {
		t.Fatal("block didn't time out in normal mode")
	}

	// On the other hand, a wallet block should come through immediately. Not
	// even waiting on the block tick.
	spv.tipChan <- addBlock()
	if !getNote(blockTicker / 2) {
		t.Fatal("wallet block wasn't sent through")
	}

	// If we do the same thing but make sure that a polled block is queued
	// first, we should still see the block right away, and the queued block
	// should be canceled.
	blk := addBlock()
	time.Sleep(blockTicker * 2)
	spv.tipChan <- blk
	if !getNote(blockTicker / 2) {
		t.Fatal("wallet block wasn't sent through with polled block queued")
	}

	if getNote(walletBlockAllowance * 2) {
		t.Fatal("queued polled block that should have been canceled came through")
	}
}

func TestGetBlockHeader(t *testing.T) {
	wallet, node, shutdown := tNewWallet(true, walletTypeSPV)
	defer shutdown()

	const tipHeight = 12
	const blockHeight = 11 // 2 confirmations
	var blockHash, prevHash, h chainhash.Hash
	var blockHdr msgBlockWithHeight
	for height := 0; height <= tipHeight; height++ {
		hdr := &msgBlockWithHeight{
			msgBlock: &wire.MsgBlock{
				Header: wire.BlockHeader{
					PrevBlock: h,
					Timestamp: time.Unix(int64(height), 0),
				},
				Transactions: nil,
			},
			height: int64(height),
		}

		h = hdr.msgBlock.BlockHash()

		switch height {
		case blockHeight - 1:
			prevHash = h
			blockHdr = *hdr
		case blockHeight:
			blockHash = h
		}

		node.verboseBlocks[h] = hdr
		hh := h // just because we are storing pointers in mainchain
		node.mainchain[int64(height)] = &hh
	}

	hdr, mainchain, err := wallet.tipRedeemer.GetBlockHeader(&blockHash)
	if err != nil {
		t.Fatalf("initial success error: %v", err)
	}
	if !mainchain {
		t.Fatalf("expected block %s to be in mainchain", blockHash)
	}
	if hdr.Hash != blockHash.String() {
		t.Fatal("wrong header?")
	}
	if hdr.Height != blockHeight {
		t.Fatal("wrong height?")
	}
	if hdr.Time != blockHeight {
		t.Fatalf("wrong block time stamp, wanted %d, got %d", blockHeight, hdr.Time)
	}
	if hdr.Confirmations != 2 {
		t.Fatalf("expected 2 confs, got %d", hdr.Confirmations)
	}
	if hdr.PreviousBlockHash != prevHash.String() {
		t.Fatalf("wrong previous hash, want: %s got: %s", prevHash.String(), hdr.PreviousBlockHash)
	}

	node.mainchain[int64(blockHeight)] = &chainhash.Hash{0x01} // mainchain ended up with different block
	hdr, mainchain, err = wallet.tipRedeemer.GetBlockHeader(&blockHash)
	if err != nil {
		t.Fatalf("initial success error: %v", err)
	}
	if mainchain {
		t.Fatalf("expected block %s to be classified as oprhan", blockHash)
	}
	if hdr.Confirmations != -1 {
		t.Fatalf("expected -1 confs for side chain block, got %d", hdr.Confirmations)
	}
	node.mainchain[int64(blockHeight)] = &blockHash // clean up

	// Can't fetch header error.
	delete(node.verboseBlocks, blockHash) // can't find block by hash
	if _, _, err := wallet.tipRedeemer.GetBlockHeader(&blockHash); err == nil {
		t.Fatalf("Can't fetch header error not propagated")
	}
	node.verboseBlocks[blockHash] = &blockHdr // clean up

	// Main chain is shorter than requested block.
	prevMainchain := node.mainchain
	node.mainchain = map[int64]*chainhash.Hash{
		0: node.mainchain[0],
		1: node.mainchain[1],
	}
	hdr, mainchain, err = wallet.tipRedeemer.GetBlockHeader(&blockHash)
	if err != nil {
		t.Fatalf("invalid tip height not noticed")
	}
	if mainchain {
		t.Fatalf("expected block %s to be classified as orphan", blockHash)
	}
	if hdr.Confirmations != -1 {
		t.Fatalf("confirmations not zero for lower mainchain tip height, got: %d", hdr.Confirmations)
	}
	node.mainchain = prevMainchain // clean up
}
