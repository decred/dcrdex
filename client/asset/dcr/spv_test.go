//go:build !harness

package dcr

import (
	"context"
	"errors"
	"testing"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/encode"
	walleterrors "decred.org/dcrwallet/v2/errors"
	"decred.org/dcrwallet/v2/p2p"
	walletjson "decred.org/dcrwallet/v2/rpc/jsonrpc/types"
	"decred.org/dcrwallet/v2/wallet"
	"decred.org/dcrwallet/v2/wallet/udb"
	"github.com/decred/dcrd/blockchain/stake/v4"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrutil/v4"
	"github.com/decred/dcrd/gcs/v3"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types/v3"
	"github.com/decred/dcrd/txscript/v4"
	"github.com/decred/dcrd/txscript/v4/stdaddr"
	"github.com/decred/dcrd/wire"
)

type tDcrWallet struct {
	wallet.NetworkBackend
	knownAddr      wallet.KnownAddress
	knownAddrErr   error
	txsByHash      []*wire.MsgTx
	txsByHashErr   error
	acctBal        wallet.Balances
	acctBalErr     error
	lockedPts      []chainjson.TransactionInput
	lockedPtsErr   error
	unspents       []*walletjson.ListUnspentResult
	listUnspentErr error
	listTxs        []walletjson.ListTransactionsResult
	listTxsErr     error
	tip            struct {
		hash   chainhash.Hash
		height int32
	}
	extAddr              stdaddr.Address
	extAddrErr           error
	intAddr              stdaddr.Address
	intAddrErr           error
	sigErrs              []wallet.SignatureError
	signTxErr            error
	publishTxErr         error
	blockHeader          map[chainhash.Hash]*wire.BlockHeader
	blockHeaderErr       map[chainhash.Hash]error
	mainchainDontHave    bool
	mainchainInvalidated bool
	mainchainErr         error
	filterKey            [gcs.KeySize]byte
	filter               *gcs.FilterV2
	filterErr            error
	blockInfo            *wallet.BlockInfo
	blockInfoErr         error
	acctLocked           bool
	acctUnlockedErr      error
	lockAcctErr          error
	unlockAcctErr        error
	priv                 *secp256k1.PrivateKey
	privKeyErr           error
	txDetails            *udb.TxDetails
	txDetailsErr         error
	remotePeers          map[string]*p2p.RemotePeer
	spvBlocks            []*wire.MsgBlock
	spvBlocksErr         error
	unlockedOutpoint     *wire.OutPoint
	lockedOutpoint       *wire.OutPoint
}

func (w *tDcrWallet) KnownAddress(ctx context.Context, a stdaddr.Address) (wallet.KnownAddress, error) {
	return w.knownAddr, w.knownAddrErr
}

func (w *tDcrWallet) AccountBalance(ctx context.Context, account uint32, confirms int32) (wallet.Balances, error) {
	return w.acctBal, w.acctBalErr
}

func (w *tDcrWallet) LockedOutpoints(ctx context.Context, accountName string) ([]chainjson.TransactionInput, error) {
	return w.lockedPts, w.lockedPtsErr
}

func (w *tDcrWallet) ListUnspent(ctx context.Context, minconf, maxconf int32, addresses map[string]struct{}, accountName string) ([]*walletjson.ListUnspentResult, error) {
	return w.unspents, w.listUnspentErr
}

func (w *tDcrWallet) UnlockOutpoint(txHash *chainhash.Hash, index uint32) {
	w.unlockedOutpoint = &wire.OutPoint{
		Hash:  *txHash,
		Index: index,
	}
}

func (w *tDcrWallet) LockOutpoint(txHash *chainhash.Hash, index uint32) {
	w.lockedOutpoint = &wire.OutPoint{
		Hash:  *txHash,
		Index: index,
	}
}

func (w *tDcrWallet) ListTransactionDetails(ctx context.Context, txHash *chainhash.Hash) ([]walletjson.ListTransactionsResult, error) {
	return w.listTxs, w.listTxsErr
}

func (w *tDcrWallet) MainChainTip(ctx context.Context) (hash chainhash.Hash, height int32) {
	return w.tip.hash, w.tip.height
}

func (w *tDcrWallet) NewExternalAddress(ctx context.Context, account uint32, callOpts ...wallet.NextAddressCallOption) (stdaddr.Address, error) {
	return w.extAddr, w.extAddrErr
}

func (w *tDcrWallet) NewInternalAddress(ctx context.Context, account uint32, callOpts ...wallet.NextAddressCallOption) (stdaddr.Address, error) {
	return w.intAddr, w.intAddrErr
}

func (w *tDcrWallet) SignTransaction(ctx context.Context, tx *wire.MsgTx, hashType txscript.SigHashType,
	additionalPrevScripts map[wire.OutPoint][]byte, additionalKeysByAddress map[string]*dcrutil.WIF,
	p2shRedeemScriptsByAddress map[string][]byte) ([]wallet.SignatureError, error) {

	return w.sigErrs, w.signTxErr
}

func (w *tDcrWallet) PublishTransaction(ctx context.Context, tx *wire.MsgTx, n wallet.NetworkBackend) (*chainhash.Hash, error) {
	if w.publishTxErr != nil {
		return nil, w.publishTxErr
	}
	h := tx.TxHash()
	return &h, nil
}

func (w *tDcrWallet) BlockHeader(ctx context.Context, blockHash *chainhash.Hash) (*wire.BlockHeader, error) {
	return w.blockHeader[*blockHash], w.blockHeaderErr[*blockHash]
}

func (w *tDcrWallet) BlockInMainChain(ctx context.Context, hash *chainhash.Hash) (haveBlock, invalidated bool, err error) {
	return !w.mainchainDontHave, w.mainchainInvalidated, w.mainchainErr
}

func (w *tDcrWallet) CFilterV2(ctx context.Context, blockHash *chainhash.Hash) ([gcs.KeySize]byte, *gcs.FilterV2, error) {
	return w.filterKey, w.filter, w.filterErr
}

func (w *tDcrWallet) BlockInfo(ctx context.Context, blockID *wallet.BlockIdentifier) (*wallet.BlockInfo, error) {
	return w.blockInfo, w.blockInfoErr
}

func (w *tDcrWallet) AccountUnlocked(ctx context.Context, account uint32) (bool, error) {
	return !w.acctLocked, w.acctUnlockedErr
}

func (w *tDcrWallet) LockAccount(ctx context.Context, account uint32) error {
	return w.lockAcctErr
}

func (w *tDcrWallet) UnlockAccount(ctx context.Context, account uint32, passphrase []byte) error {
	return w.unlockAcctErr
}

func (w *tDcrWallet) LoadPrivateKey(ctx context.Context, addr stdaddr.Address) (key *secp256k1.PrivateKey, zero func(), err error) {
	return w.priv, func() {}, w.privKeyErr
}

func (w *tDcrWallet) TxDetails(ctx context.Context, txHash *chainhash.Hash) (*udb.TxDetails, error) {
	return w.txDetails, w.txDetailsErr
}

func (w *tDcrWallet) GetRemotePeers() map[string]*p2p.RemotePeer {
	return w.remotePeers
}

func (w *tDcrWallet) Blocks(ctx context.Context, blockHashes []*chainhash.Hash) ([]*wire.MsgBlock, error) {
	return w.spvBlocks, w.spvBlocksErr
}

func (w *tDcrWallet) GetTransactionsByHashes(ctx context.Context, txHashes []*chainhash.Hash) (
	txs []*wire.MsgTx, notFound []*wire.InvVect, err error) {

	return w.txsByHash, nil, w.txsByHashErr
}

func tNewSpvWallet() (*spvWallet, *tDcrWallet) {
	dcrw := &tDcrWallet{
		blockHeader:    make(map[chainhash.Hash]*wire.BlockHeader),
		blockHeaderErr: make(map[chainhash.Hash]error),
	}
	return &spvWallet{
		dcrWallet: dcrw,
		spv:       dcrw,
		log:       dex.StdOutLogger("T", dex.LevelTrace),
		blockCache: blockCache{
			blocks: make(map[chainhash.Hash]*cachedBlock),
		},
		acctName: tAcctName,
	}, dcrw
}

type tKnownAddress struct {
	stdaddr.Address
	acctName string
	acctType wallet.AccountKind // 0-value is AccountKindBIP0044
}

func (a *tKnownAddress) AccountName() string {
	return a.acctName
}

func (a *tKnownAddress) AccountKind() wallet.AccountKind {
	return a.acctType
}

func (a *tKnownAddress) ScriptLen() int { return 1 }

var _ wallet.KnownAddress = (*tKnownAddress)(nil)

func TestAccountOwnsAddress(t *testing.T) {
	w, dcrw := tNewSpvWallet()

	kaddr := &tKnownAddress{
		Address:  tPKHAddr,
		acctName: tAcctName,
	}
	dcrw.knownAddr = kaddr

	// Initial success
	if have, err := w.AccountOwnsAddress(tCtx, tPKHAddr, ""); err != nil {
		t.Fatalf("initial success trial failed: %v", err)
	} else if !have {
		t.Fatal("failed initial success. have = false")
	}

	// Foreign address
	dcrw.knownAddrErr = walleterrors.NotExist
	if have, err := w.AccountOwnsAddress(tCtx, tPKHAddr, ""); err != nil {
		t.Fatalf("unexpected error when should just be have = false for foreign address: %v", err)
	} else if have {
		t.Fatalf("shouldn't have, but have for foreign address")
	}

	// Other KnownAddress error
	dcrw.knownAddrErr = tErr
	if _, err := w.AccountOwnsAddress(tCtx, tPKHAddr, ""); err == nil {
		t.Fatal("no error for KnownAddress error")
	}
	dcrw.knownAddrErr = nil

	// Wrong account
	kaddr.acctName = "not the right name"
	if have, err := w.AccountOwnsAddress(tCtx, tPKHAddr, ""); err != nil {
		t.Fatalf("unexpected error when should just be have = false for wrong account: %v", err)
	} else if have {
		t.Fatalf("shouldn't have, but have for wrong account")
	}
	kaddr.acctName = tAcctName

	// Wrong type
	kaddr.acctType = wallet.AccountKindImportedXpub
	if have, err := w.AccountOwnsAddress(tCtx, tPKHAddr, ""); err != nil {
		t.Fatalf("don't have trial failed: %v", err)
	} else if have {
		t.Fatal("have, but shouldn't")
	}
	kaddr.acctType = wallet.AccountKindBIP0044
}

func TestAccountBalance(t *testing.T) {
	w, dcrw := tNewSpvWallet()
	const amt = 1e8

	dcrw.acctBal = wallet.Balances{
		Spendable: amt,
	}

	// Initial success
	if bal, err := w.AccountBalance(tCtx, 1, ""); err != nil {
		t.Fatalf("AccountBalance during initial success test: %v", err)
	} else if bal.Spendable != amt/1e8 {
		t.Fatalf("wrong amount. wanted %.0f, got %.0f", amt, bal.Spendable)
	}

	// AccountBalance error
	dcrw.acctBalErr = tErr
	if _, err := w.AccountBalance(tCtx, 1, ""); err == nil {
		t.Fatal("no error for AccountBalance error")
	}
}

func TestSimpleErrorPropagation(t *testing.T) {
	w, dcrw := tNewSpvWallet()
	var err error
	tests := map[string]func(){
		"LockedOutputs": func() {
			dcrw.lockedPtsErr = tErr
			_, err = w.LockedOutputs(tCtx, "")
		},
		"Unspents": func() {
			dcrw.listUnspentErr = tErr
			_, err = w.Unspents(tCtx, "")
		},
		"SignRawTransaction.err": func() {
			dcrw.signTxErr = tErr
			_, err = w.SignRawTransaction(tCtx, new(wire.MsgTx))
			dcrw.signTxErr = nil
		},
		"SignRawTransaction.sigErrs": func() {
			dcrw.sigErrs = []wallet.SignatureError{{}}
			_, err = w.SignRawTransaction(tCtx, new(wire.MsgTx))
		},
		"SendRawTransaction": func() {
			dcrw.publishTxErr = tErr
			_, err = w.SendRawTransaction(tCtx, nil, false)
		},
		"ExternalAddress": func() {
			dcrw.extAddrErr = tErr
			_, err = w.ExternalAddress(tCtx, "")
		},
		"InternalAddress": func() {
			dcrw.intAddrErr = tErr
			_, err = w.InternalAddress(tCtx, "")
		},
		"AccountUnlocked": func() {
			dcrw.acctUnlockedErr = tErr
			_, err = w.AccountUnlocked(tCtx, "")
		},
		"LockAccount": func() {
			dcrw.lockAcctErr = tErr
			err = w.LockAccount(tCtx, "")
		},
		"UnlockAccount": func() {
			dcrw.unlockAcctErr = tErr
			err = w.UnlockAccount(tCtx, []byte("abc"), "")
		},
		"AddressPrivKey": func() {
			dcrw.privKeyErr = tErr
			_, err = w.AddressPrivKey(tCtx, tPKHAddr)
		},
	}

	for name, f := range tests {
		if f(); err == nil {
			t.Fatalf("%q error did not propagate", name)
		}
	}
}

func TestLockUnlockOutpoints(t *testing.T) {
	w, dcrw := tNewSpvWallet()
	lock := &wire.OutPoint{
		Hash:  chainhash.Hash{0x1},
		Index: 55,
	}

	w.LockUnspent(nil, false, []*wire.OutPoint{lock})
	if *dcrw.lockedOutpoint != *lock {
		t.Fatalf("outpoint not locked")
	}

	unlock := &wire.OutPoint{
		Hash:  chainhash.Hash{0x2},
		Index: 555,
	}
	w.LockUnspent(nil, true, []*wire.OutPoint{unlock})
	if *dcrw.unlockedOutpoint != *unlock {
		t.Fatalf("outpoint not unlocked")
	}
}

func TestUnspentOutput(t *testing.T) {
	w, dcrw := tNewSpvWallet()

	const txOutIdx = 1

	dcrw.txDetails = &udb.TxDetails{
		TxRecord: udb.TxRecord{
			MsgTx: wire.MsgTx{
				TxOut: []*wire.TxOut{
					{},
					{},
				},
			},
			TxType: stake.TxTypeRegular,
		},
		Credits: []udb.CreditRecord{
			{
				Index: txOutIdx,
			},
		},
	}

	dcrw.listTxs = []walletjson.ListTransactionsResult{
		{
			Vout:    txOutIdx,
			Address: tPKHAddr.String(),
		},
	}

	// Initial success
	if _, err := w.UnspentOutput(tCtx, nil, 1, 0); err != nil {
		t.Fatalf("failed initial success test: %v", err)
	}

	// TxDetails NotExist error
	dcrw.txDetailsErr = walleterrors.NotExist
	if _, err := w.UnspentOutput(tCtx, nil, 1, 0); !errors.Is(err, asset.CoinNotFoundError) {
		t.Fatalf("expected asset.CoinNotFoundError, got %v", err)
	}

	// TxDetails generic error
	dcrw.txDetailsErr = tErr
	if _, err := w.UnspentOutput(tCtx, nil, 1, 0); err == nil {
		t.Fatalf("expected TxDetail generic error to propagate")
	}
	dcrw.txDetailsErr = nil

	// ListTransactionDetails error
	dcrw.listTxsErr = tErr
	if _, err := w.UnspentOutput(tCtx, nil, 1, 0); err == nil {
		t.Fatalf("expected ListTransactionDetails error to propagate")
	}
	dcrw.listTxsErr = nil

	// output not found
	dcrw.listTxs[0].Vout = txOutIdx + 1
	if _, err := w.UnspentOutput(tCtx, nil, 1, 0); err == nil {
		t.Fatalf("expected ListTransactionDetails output not found")
	}
	dcrw.listTxs[0].Vout = txOutIdx

	// Not enough outputs
	dcrw.txDetails.MsgTx.TxOut = dcrw.txDetails.MsgTx.TxOut[:1]
	if _, err := w.UnspentOutput(tCtx, nil, 1, 0); err == nil {
		t.Fatalf("expected error for too few TxDetails outputs")
	}
	dcrw.txDetails.MsgTx.AddTxOut(new(wire.TxOut))

	// Credit spent
	dcrw.txDetails.Credits[0].Spent = true
	if _, err := w.UnspentOutput(tCtx, nil, 1, 0); err == nil {
		t.Fatalf("expected error TxDetail output spent")
	}
	dcrw.txDetails.Credits[0].Spent = false

	// Output not found
	dcrw.txDetails.Credits[0].Index = txOutIdx + 1
	if _, err := w.UnspentOutput(tCtx, nil, 1, 0); !errors.Is(err, asset.CoinNotFoundError) {
		t.Fatalf("expected asset.CoinNotFoundError for not our output, got %v", err)
	}
	dcrw.txDetails.Credits[0].Index = txOutIdx

	// ensure we can recover success
	if _, err := w.UnspentOutput(tCtx, nil, 1, 0); err != nil {
		t.Fatalf("failed final success test: %v", err)
	}
}

func TestGetBlockHeader(t *testing.T) {
	w, dcrw := tNewSpvWallet()

	const tipHeight = 12
	const blockHeight = 11 // 2 confirmations
	var blockHash, prevHash, blockHash5 chainhash.Hash
	for height := 0; height <= tipHeight; height++ {
		hdr := &wire.BlockHeader{
			Height:    uint32(height),
			PrevBlock: prevHash,
			Timestamp: time.Unix(int64(height), 0),
		}
		h := hdr.BlockHash()
		switch height {
		case blockHeight:
			blockHash = h
		case 5:
			blockHash5 = h
		}
		prevHash = h
		dcrw.blockHeader[h] = hdr
	}
	tipHash := prevHash

	dcrw.tip.hash = tipHash
	dcrw.tip.height = tipHeight

	dcrw.blockInfo = &wallet.BlockInfo{
		Hash: tipHash,
	}

	hdr, err := w.GetBlockHeader(tCtx, &blockHash)
	if err != nil {
		t.Fatalf("initial success error: %v", err)
	}

	if hdr.BlockHash() != blockHash {
		t.Fatal("wrong header?")
	}

	if hdr.MedianTime != tipHeight/2 {
		t.Fatalf("wrong median time. wanted %d, got %d", tipHeight/2, hdr.MedianTime)
	}

	if hdr.Confirmations != 2 {
		t.Fatalf("expected 2 confs, got %d", hdr.Confirmations)
	}

	if *hdr.NextHash != tipHash {
		t.Fatal("wrong next hash")
	}

	// BlockHeader error
	dcrw.blockHeaderErr[blockHash] = tErr
	if _, err := w.GetBlockHeader(tCtx, &blockHash); err == nil {
		t.Fatalf("BlockHeader error not propagated")
	}
	dcrw.blockHeaderErr[blockHash] = nil

	// medianTime error
	dcrw.blockHeaderErr[blockHash5] = tErr
	if _, err := w.GetBlockHeader(tCtx, &blockHash); err == nil {
		t.Fatalf("medianTime error not propagated")
	}
	dcrw.blockHeaderErr[blockHash5] = nil

	// MainChainTip error
	dcrw.tip.height = 3
	if _, err := w.GetBlockHeader(tCtx, &blockHash); err == nil {
		t.Fatalf("invalid tip height not noticed")
	}
	dcrw.tip.height = tipHeight

	// GetBlockHash error
	dcrw.blockInfoErr = tErr
	if _, err := w.GetBlockHeader(tCtx, &blockHash); err == nil {
		t.Fatalf("BlockInfo error not propagated")
	}
	dcrw.blockInfoErr = nil

	_, err = w.GetBlockHeader(tCtx, &blockHash)
	if err != nil {
		t.Fatalf("final success error: %v", err)
	}
}

func TestGetBlock(t *testing.T) {
	// 2 success routes, cached and uncached.
	w, dcrw := tNewSpvWallet()

	hdr := wire.BlockHeader{Height: 5}
	blockHash := hdr.BlockHash()
	msgBlock := &wire.MsgBlock{Header: hdr}
	dcrw.spvBlocks = []*wire.MsgBlock{msgBlock}

	// uncached
	blk, err := w.GetBlock(tCtx, &blockHash)
	if err != nil {
		t.Fatalf("initial success (uncached) error: %v", err)
	}
	delete(w.blockCache.blocks, blockHash)

	if blk.BlockHash() != blockHash {
		t.Fatalf("wrong hash")
	}

	// Blocks error
	dcrw.spvBlocksErr = tErr
	if _, err := w.GetBlock(tCtx, &blockHash); err == nil {
		t.Fatalf("Blocks error not propagated")
	}
	dcrw.spvBlocksErr = nil

	// No blocks
	dcrw.spvBlocks = []*wire.MsgBlock{}
	if _, err := w.GetBlock(tCtx, &blockHash); err == nil {
		t.Fatalf("empty Blocks didn't generate an error")
	}
	dcrw.spvBlocks = []*wire.MsgBlock{msgBlock}

	if _, err = w.GetBlock(tCtx, &blockHash); err != nil {
		t.Fatalf("final success (uncached) error: %v", err)
	}

	// The block should be cached
	if w.blockCache.blocks[blockHash] == nil {
		t.Fatalf("block not cached")
	}

	// Zero the time. Then check to make sure it was updated.
	// We can also add back our Blocks error, because with a cached block,
	// we'll never get there.
	dcrw.spvBlocksErr = tErr
	w.blockCache.blocks[blockHash].lastAccess = time.Time{}
	if _, err = w.GetBlock(tCtx, &blockHash); err != nil {
		t.Fatalf("final success (cached) error: %v", err)
	}
	if w.blockCache.blocks[blockHash].lastAccess.IsZero() {
		t.Fatalf("lastAccess stamp not updated")
	}
}

func TestGetTransaction(t *testing.T) {
	w, dcrw := tNewSpvWallet()

	const txOutIdx = 1

	dcrw.txDetails = &udb.TxDetails{
		TxRecord: udb.TxRecord{
			MsgTx: wire.MsgTx{
				TxOut: []*wire.TxOut{
					{},
					{},
				},
			},
			TxType: stake.TxTypeRegular,
		},
		Credits: []udb.CreditRecord{
			{
				Index: txOutIdx,
			},
		},
		Block: udb.BlockMeta{
			Block: udb.Block{
				Height: 2,
			},
		},
	}

	dcrw.tip.height = 3 // 2 confirmations

	dcrw.listTxs = []walletjson.ListTransactionsResult{
		{
			Vout:    txOutIdx,
			Address: tPKHAddr.String(),
		},
	}

	txHash := &chainhash.Hash{0x12}
	tx, err := w.GetTransaction(tCtx, txHash)
	if err != nil {
		t.Fatalf("initial success error: %v", err)
	}

	if tx.Confirmations != 2 {
		t.Fatalf("expected 2 confirmations, got %d", tx.Confirmations)
	}

	// TxDetails NotExist error
	dcrw.txDetailsErr = walleterrors.NotExist
	if _, err := w.GetTransaction(tCtx, txHash); !errors.Is(err, asset.CoinNotFoundError) {
		t.Fatalf("expected asset.CoinNotFoundError, got %v", err)
	}

	// TxDetails generic error
	dcrw.txDetailsErr = tErr
	if _, err := w.GetTransaction(tCtx, txHash); err == nil {
		t.Fatalf("expected TxDetail generic error to propagate")
	}
	dcrw.txDetailsErr = nil

	// ListTransactionDetails error
	dcrw.listTxsErr = tErr
	if _, err := w.UnspentOutput(tCtx, nil, 1, 0); err == nil {
		t.Fatalf("expected ListTransactionDetails error to propagate")
	}
	dcrw.listTxsErr = nil

	if _, err := w.GetTransaction(tCtx, txHash); err != nil {
		t.Fatalf("final success error: %v", err)
	}
}

func TestMatchAnyScript(t *testing.T) {
	w, dcrw := tNewSpvWallet()

	const (
		// dcrd/gcs/blockcf2/blockcf.go
		B = 19
		M = 784931
	)

	// w.filterKey, w.filter, w.filterErr

	key := [gcs.KeySize]byte{0xb2}
	var blockHash chainhash.Hash
	copy(blockHash[:], key[:])

	pkScript := encode.RandomBytes(25)
	filter, err := gcs.NewFilterV2(B, M, key, [][]byte{pkScript})
	if err != nil {
		t.Fatalf("NewFilterV2 error: %v", err)
	}
	dcrw.filterKey = key
	dcrw.filter = filter

	match, err := w.MatchAnyScript(tCtx, &blockHash, [][]byte{pkScript})
	if err != nil {
		t.Fatalf("MatchAnyScript error: %v", err)
	}

	if !match {
		t.Fatalf("no match reported")
	}

	dcrw.filterErr = tErr
	if _, err := w.MatchAnyScript(tCtx, &blockHash, [][]byte{pkScript}); err == nil {
		t.Fatalf("CFilterV2 error did not propagate")
	}
}

func TestGetRawTransaction(t *testing.T) {
	w, dcrw := tNewSpvWallet()

	dcrw.txsByHash = []*wire.MsgTx{{}}

	if _, err := w.GetRawTransaction(tCtx, nil); err != nil {
		t.Fatalf("intitial GetRawTransaction error: %v", err)
	}

	// TxDetails NotExist error
	dcrw.txsByHash = []*wire.MsgTx{}
	if _, err := w.GetRawTransaction(tCtx, nil); !errors.Is(err, asset.CoinNotFoundError) {
		t.Fatalf("expected asset.CoinNotFoundError, got %v", err)
	}
	dcrw.txsByHash = []*wire.MsgTx{{}}

	// TxDetails generic error
	dcrw.txsByHashErr = tErr
	if _, err := w.GetRawTransaction(tCtx, nil); err == nil {
		t.Fatalf("expected TxDetail generic error to propagate")
	}
	dcrw.txsByHashErr = nil
}
