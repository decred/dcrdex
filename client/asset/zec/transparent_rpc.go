package zec

import (
	"encoding/hex"
	"errors"
	"fmt"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/asset/btc"
	"decred.org/dcrdex/dex"
	dexzec "decred.org/dcrdex/dex/networks/zec"
	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/rpcclient/v8"
)

func listUnspent(c rpcCaller) (res []*btc.ListUnspentResult, err error) {
	const minConf = 0
	return res, c.CallRPC("listunspent", []any{minConf}, &res)
}

func lockUnspent(c rpcCaller, unlock bool, ops []*btc.Output) error {
	var rpcops []*btc.RPCOutpoint // To clear all, this must be nil->null, not empty slice.
	for _, op := range ops {
		rpcops = append(rpcops, &btc.RPCOutpoint{
			TxID: op.Pt.TxHash.String(),
			Vout: op.Pt.Vout,
		})
	}
	var success bool
	err := c.CallRPC("lockunspent", []any{unlock, rpcops}, &success)
	if err == nil && !success {
		return fmt.Errorf("lockunspent unsuccessful")
	}
	return err
}

type zTx struct {
	*dexzec.Tx
	blockHash *chainhash.Hash
}

func getTransaction(c rpcCaller, txHash *chainhash.Hash) (*zTx, error) {
	var tx btc.GetTransactionResult
	if err := c.CallRPC("gettransaction", []any{txHash.String()}, &tx); err != nil {
		return nil, err
	}
	dexzecTx, err := dexzec.DeserializeTx(tx.Bytes)
	if err != nil {
		return nil, err
	}
	blockHash, err := chainhash.NewHashFromStr(tx.BlockHash)
	if err != nil {
		return nil, fmt.Errorf("invalid block hash for transaction: %v", err)
	}
	zt := &zTx{
		Tx:        dexzecTx,
		blockHash: blockHash,
	}
	return zt, nil
}

func getRawTransaction(c rpcCaller, txHash *chainhash.Hash) ([]byte, error) {
	var txB dex.Bytes
	return txB, c.CallRPC("getrawtransaction", []any{txHash.String()}, &txB)
}

func signTxByRPC(c rpcCaller, inTx *dexzec.Tx) (*dexzec.Tx, error) {
	txBytes, err := inTx.Bytes()
	if err != nil {
		return nil, fmt.Errorf("tx serialization error: %w", err)
	}
	res := new(btc.SignTxResult)

	err = c.CallRPC("signrawtransaction", []any{hex.EncodeToString(txBytes)}, res)
	if err != nil {
		return nil, fmt.Errorf("tx signing error: %w", err)
	}
	if !res.Complete {
		sep := ""
		errMsg := ""
		for _, e := range res.Errors {
			errMsg += e.Error + sep
			sep = ";"
		}
		return nil, fmt.Errorf("signing incomplete. %d signing errors encountered: %s", len(res.Errors), errMsg)
	}
	outTx, err := dexzec.DeserializeTx(res.Hex)
	if err != nil {
		return nil, fmt.Errorf("error deserializing transaction response: %w", err)
	}
	return outTx, nil
}

func callHashGetter(c rpcCaller, method string, args []any) (*chainhash.Hash, error) {
	var txid string
	err := c.CallRPC(method, args, &txid)
	if err != nil {
		return nil, err
	}
	return chainhash.NewHashFromStr(txid)
}

func sendRawTransaction(c rpcCaller, tx *dexzec.Tx) (*chainhash.Hash, error) {
	txB, err := tx.Bytes()
	if err != nil {
		return nil, err
	}
	return callHashGetter(c, "sendrawtransaction", []any{hex.EncodeToString(txB), false})
}

func dumpPrivKey(c rpcCaller, addr string) (*secp256k1.PrivateKey, error) {
	var keyHex string
	err := c.CallRPC("dumpprivkey", []any{addr}, &keyHex)
	if err != nil {
		return nil, err
	}
	wif, err := btcutil.DecodeWIF(keyHex)
	if err != nil {
		return nil, err
	}
	return wif.PrivKey, nil
}

func listLockUnspent(c rpcCaller, log dex.Logger) ([]*btc.RPCOutpoint, error) {
	var unspents []*btc.RPCOutpoint
	err := c.CallRPC("listlockunspent", nil, &unspents)
	if err != nil {
		return nil, err
	}
	// This is quirky wallet software that does not unlock spent outputs, so
	// we'll verify that each output is actually unspent.
	var i int // for in-place filter
	for _, utxo := range unspents {
		var gtxo *btcjson.GetTxOutResult
		err = c.CallRPC("gettxout", []any{utxo.TxID, utxo.Vout, true}, &gtxo)
		if err != nil {
			log.Warnf("gettxout(%v:%d): %v", utxo.TxID, utxo.Vout, err)
			continue
		}
		if gtxo != nil {
			unspents[i] = utxo // unspent, keep it
			i++
			continue
		}
		// actually spent, unlock
		var success bool
		op := []*btc.RPCOutpoint{{
			TxID: utxo.TxID,
			Vout: utxo.Vout,
		}}

		err = c.CallRPC("lockunspent", []any{true, op}, &success)
		if err != nil || !success {
			log.Warnf("lockunspent(unlocking %v:%d): success = %v, err = %v",
				utxo.TxID, utxo.Vout, success, err)
			continue
		}
		log.Debugf("Unlocked spent outpoint %v:%d", utxo.TxID, utxo.Vout)
	}
	unspents = unspents[:i]
	return unspents, nil
}

func getTxOut(c rpcCaller, txHash *chainhash.Hash, index uint32) (*wire.TxOut, uint32, error) {
	// Note that we pass to call pointer to a pointer (&res) so that
	// json.Unmarshal can nil the pointer if the method returns the JSON null.
	var res *btcjson.GetTxOutResult
	if err := c.CallRPC("gettxout", []any{txHash.String(), index, true}, &res); err != nil {
		return nil, 0, err
	}
	if res == nil {
		return nil, 0, nil
	}
	outputScript, err := hex.DecodeString(res.ScriptPubKey.Hex)
	if err != nil {
		return nil, 0, err
	}
	return wire.NewTxOut(int64(toZats(res.Value)), outputScript), uint32(res.Confirmations), nil
}

func getVersion(c rpcCaller) (uint64, uint64, error) {
	r := &struct {
		Version         uint64 `json:"version"`
		SubVersion      string `json:"subversion"`
		ProtocolVersion uint64 `json:"protocolversion"`
	}{}
	err := c.CallRPC("getnetworkinfo", nil, r)
	if err != nil {
		return 0, 0, err
	}
	return r.Version, r.ProtocolVersion, nil
}

func getBlockchainInfo(c rpcCaller) (*btc.GetBlockchainInfoResult, error) {
	chainInfo := new(btc.GetBlockchainInfoResult)
	err := c.CallRPC("getblockchaininfo", nil, chainInfo)
	if err != nil {
		return nil, err
	}
	return chainInfo, nil
}

func getBestBlockHeader(c rpcCaller) (*btc.BlockHeader, error) {
	tipHash, err := getBestBlockHash(c)
	if err != nil {
		return nil, err
	}
	hdr, _, err := getVerboseBlockHeader(c, tipHash)
	return hdr, err
}

func getBestBlockHash(c rpcCaller) (*chainhash.Hash, error) {
	return callHashGetter(c, "getbestblockhash", nil)
}

func getVerboseBlockHeader(c rpcCaller, blockHash *chainhash.Hash) (header *btc.BlockHeader, mainchain bool, err error) {
	hdr, err := getRPCBlockHeader(c, blockHash)
	if err != nil {
		return nil, false, err
	}
	// RPC wallet must return negative confirmations number for orphaned blocks.
	mainchain = hdr.Confirmations >= 0
	return hdr, mainchain, nil
}

func getBlockHeader(c rpcCaller, blockHash *chainhash.Hash) (*wire.BlockHeader, error) {
	var b dex.Bytes
	err := c.CallRPC("getblockheader", []any{blockHash.String(), false}, &b)
	if err != nil {
		return nil, err
	}
	return dexzec.DeserializeBlockHeader(b)
}

func getRPCBlockHeader(c rpcCaller, blockHash *chainhash.Hash) (*btc.BlockHeader, error) {
	blkHeader := new(btc.BlockHeader)
	err := c.CallRPC("getblockheader", []any{blockHash.String(), true}, blkHeader)
	if err != nil {
		return nil, err
	}
	return blkHeader, nil
}

func getWalletTransaction(c rpcCaller, txHash *chainhash.Hash) (*btc.GetTransactionResult, error) {
	var tx btc.GetTransactionResult
	err := c.CallRPC("gettransaction", []any{txHash.String()}, &tx)
	if err != nil {
		if btc.IsTxNotFoundErr(err) {
			return nil, asset.CoinNotFoundError
		}
		return nil, err
	}
	return &tx, nil
}

func getBalance(c rpcCaller) (bal uint64, err error) {
	return bal, c.CallRPC("getbalance", []any{"", 0 /* minConf */, false /* includeWatchOnly */, true /* inZats */}, &bal)
}

type networkInfo struct {
	Connections uint32 `json:"connections"`
}

func peerCount(c rpcCaller) (uint32, error) {
	var r networkInfo
	err := c.CallRPC("getnetworkinfo", nil, &r)
	if err != nil {
		return 0, codedError(errGetNetInfo, err)
	}
	return r.Connections, nil
}

func getBlockHeight(c rpcCaller, blockHash *chainhash.Hash) (int32, error) {
	hdr, _, err := getVerboseBlockHeader(c, blockHash)
	if err != nil {
		return -1, err
	}
	if hdr.Height < 0 {
		return -1, fmt.Errorf("block is not a mainchain block")
	}
	return int32(hdr.Height), nil
}

func getBlock(c rpcCaller, h chainhash.Hash) (*dexzec.Block, error) {
	var blkB dex.Bytes
	err := c.CallRPC("getblock", []any{h.String(), int64(0)}, &blkB)
	if err != nil {
		return nil, err
	}

	return dexzec.DeserializeBlock(blkB)
}

// getBestBlockHeight returns the height of the top mainchain block.
func getBestBlockHeight(c rpcCaller) (int32, error) {
	header, err := getBestBlockHeader(c)
	if err != nil {
		return -1, err
	}
	return int32(header.Height), nil
}

func getBlockHash(c rpcCaller, blockHeight int64) (*chainhash.Hash, error) {
	return callHashGetter(c, "getblockhash", []any{blockHeight})
}

func getRawMempool(c rpcCaller) ([]*chainhash.Hash, error) {
	var mempool []string
	err := c.CallRPC("getrawmempool", nil, &mempool)
	if err != nil {
		return nil, translateRPCCancelErr(err)
	}

	// Convert received hex hashes to chainhash.Hash
	hashes := make([]*chainhash.Hash, 0, len(mempool))
	for _, h := range mempool {
		hash, err := chainhash.NewHashFromStr(h)
		if err != nil {
			return nil, err
		}
		hashes = append(hashes, hash)
	}
	return hashes, nil
}

func getZecTransaction(c rpcCaller, txHash *chainhash.Hash) (*dexzec.Tx, error) {
	txB, err := getRawTransaction(c, txHash)
	if err != nil {
		return nil, err
	}

	return dexzec.DeserializeTx(txB)
}

func translateRPCCancelErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, rpcclient.ErrRequestCanceled) {
		err = asset.ErrRequestTimeout
	}
	return err
}

func getTxOutput(c rpcCaller, txHash *chainhash.Hash, index uint32) (*btcjson.GetTxOutResult, error) {
	// Note that we pass to call pointer to a pointer (&res) so that
	// json.Unmarshal can nil the pointer if the method returns the JSON null.
	var res *btcjson.GetTxOutResult
	return res, c.CallRPC("gettxout", []any{txHash.String(), index, true}, &res)
}

func syncStatus(c rpcCaller) (*btc.SyncStatus, error) {
	chainInfo, err := getBlockchainInfo(c)
	if err != nil {
		return nil, newError(errGetChainInfo, "getblockchaininfo error: %w", err)
	}
	return &btc.SyncStatus{
		Target:  int32(chainInfo.Headers),
		Height:  int32(chainInfo.Blocks),
		Syncing: chainInfo.Syncing(),
	}, nil
}

type listSinceBlockRes struct {
	Transactions []btcjson.ListTransactionsResult `json:"transactions"`
}

func listSinceBlock(c rpcCaller, txHash *chainhash.Hash) ([]btcjson.ListTransactionsResult, error) {
	var res listSinceBlockRes
	if err := c.CallRPC("listsinceblock", []any{txHash.String()}, &res); err != nil {
		return nil, err
	}
	return res.Transactions, nil
}

type walletInfoRes struct {
	WalletVersion              int     `json:"walletversion"`
	Balance                    float64 `json:"balance"`
	UnconfirmedBalance         float64 `json:"unconfirmed_balance"`
	ImmatureBalance            float64 `json:"immature_balance"`
	ShieldedBalance            string  `json:"shielded_balance"`
	ShieldedUnconfirmedBalance string  `json:"shielded_unconfirmed_balance"`
	TxCount                    int     `json:"txcount"`
	KeypoolOldest              int     `json:"keypoololdest"`
	KeypoolSize                int     `json:"keypoolsize"`
	PayTxFee                   float64 `json:"paytxfee"`
	MnemonicSeedfp             string  `json:"mnemonic_seedfp"`
	LegacySeedfp               string  `json:"legacy_seedfp,omitempty"`
}

func walletInfo(c rpcCaller) (*walletInfoRes, error) {
	var res walletInfoRes
	if err := c.CallRPC("getwalletinfo", nil, &res); err != nil {
		return nil, err
	}
	return &res, nil
}
