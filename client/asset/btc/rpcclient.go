// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package btc

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	dexbtc "decred.org/dcrdex/dex/networks/btc"
	"github.com/btcsuite/btcd/btcec"
	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
	"github.com/btcsuite/btcutil"
)

const (
	methodGetBalances        = "getbalances"
	methodListUnspent        = "listunspent"
	methodLockUnspent        = "lockunspent"
	methodListLockUnspent    = "listlockunspent"
	methodChangeAddress      = "getrawchangeaddress"
	methodNewAddress         = "getnewaddress"
	methodSignTx             = "signrawtransactionwithwallet"
	methodUnlock             = "walletpassphrase"
	methodLock               = "walletlock"
	methodPrivKeyForAddress  = "dumpprivkey"
	methodSignMessage        = "signmessagewithprivkey"
	methodGetTransaction     = "gettransaction"
	methodSendToAddress      = "sendtoaddress"
	methodSetTxFee           = "settxfee"
	methodGetWalletInfo      = "getwalletinfo"
	methodGetAddressInfo     = "getaddressinfo"
	methodEstimateSmartFee   = "estimatesmartfee"
	methodSendRawTransaction = "sendrawtransaction"
	methodGetTxOut           = "gettxout"
	methodGetBlock           = "getblock"
	methodGetBlockHash       = "getblockhash"
	methodGetBestBlockHash   = "getbestblockhash"
	methodGetRawMempool      = "getrawmempool"
	methodGetRawTransaction  = "getrawtransaction"
)

// RawRequester defines dcred's rpcclient RawRequest func where all RPC
// requests sent through. For testing, it can be satisfied by a stub.
type RawRequester interface {
	RawRequest(string, []json.RawMessage) (json.RawMessage, error)
}

// RawRequesterWithContext defines dcred's rpcclient RawRequest func where all
// RPC requests sent through. For testing, it can be satisfied by a stub.
type RawRequesterWithContext interface {
	RawRequest(context.Context, string, []json.RawMessage) (json.RawMessage, error)
}

// anylist is a list of RPC parameters to be converted to []json.RawMessage and
// sent via RawRequest.
type anylist []interface{}

// rpcClient is a bitcoind JSON RPC client that uses rpcclient.Client's
// RawRequest for wallet-related calls.
type rpcClient struct {
	ctx                  context.Context
	log                  dex.Logger
	requester            RawRequesterWithContext
	chainParams          *chaincfg.Params
	segwit               bool
	decodeAddr           dexbtc.AddressDecoder
	minNetworkVersion    uint64
	arglessChangeAddrRPC bool
	legacyRawSends       bool
}

// newRPCClient is the constructor for a rpcClient.
func newRPCClient(requester RawRequesterWithContext, segwit bool, addrDecoder dexbtc.AddressDecoder, arglessChangeAddrRPC bool,
	legacyRawSends bool, minNetworkVersion uint64, logger dex.Logger, chainParams *chaincfg.Params) *rpcClient {

	return &rpcClient{
		requester:            requester,
		chainParams:          chainParams,
		segwit:               segwit,
		log:                  logger,
		decodeAddr:           addrDecoder,
		minNetworkVersion:    minNetworkVersion,
		arglessChangeAddrRPC: arglessChangeAddrRPC,
		legacyRawSends:       legacyRawSends,
	}
}

func (wc *rpcClient) connect(ctx context.Context) error {
	wc.ctx = ctx
	// Check the version. Do it here, so we can also diagnose a bad connection.
	netVer, codeVer, err := wc.getVersion()
	if err != nil {
		return fmt.Errorf("error getting version: %w", err)
	}
	if netVer < wc.minNetworkVersion {
		return fmt.Errorf("reported node version %d is less than minimum %d", netVer, wc.minNetworkVersion)
	}
	if codeVer < minProtocolVersion {
		return fmt.Errorf("node software out of date. version %d is less than minimum %d", codeVer, minProtocolVersion)
	}
	return nil
}

// RawRequest passes the reqeuest to the wallet's RawRequester.
func (wc *rpcClient) RawRequest(method string, params []json.RawMessage) (json.RawMessage, error) {
	return wc.requester.RawRequest(wc.ctx, method, params)
}

// estimateSmartFee requests the server to estimate a fee level based on the
// given parameters.
func (wc *rpcClient) estimateSmartFee(confTarget int64, mode *btcjson.EstimateSmartFeeMode) (*btcjson.EstimateSmartFeeResult, error) {
	res := new(btcjson.EstimateSmartFeeResult)
	return res, wc.call(methodEstimateSmartFee, anylist{confTarget, mode}, res)
}

// SendRawTransactionLegacy broadcasts the transaction with an additional legacy
// boolean `allowhighfees` argument set to false.
func (wc *rpcClient) SendRawTransactionLegacy(tx *wire.MsgTx) (*chainhash.Hash, error) {
	txBytes, err := serializeMsgTx(tx)
	if err != nil {
		return nil, err
	}
	return wc.callHashGetter(methodSendRawTransaction, anylist{
		hex.EncodeToString(txBytes), false})
}

// sendRawTransaction sends the MsgTx.
func (wc *rpcClient) sendRawTransaction(tx *wire.MsgTx) (*chainhash.Hash, error) {
	if wc.legacyRawSends {
		return wc.SendRawTransactionLegacy(tx)
	}
	return wc.SendRawTransaction(tx)
}

// getTxOut returns the transaction output info if it's unspent and
// nil, otherwise.
func (wc *rpcClient) getTxOut(txHash *chainhash.Hash, index uint32, _ []byte, _ time.Time) (*wire.TxOut, uint32, error) {
	txOut, err := wc.getTxOutput(txHash, index)
	if err != nil {
		return nil, 0, fmt.Errorf("getTxOut error: %v", err)
	}
	if txOut == nil {
		return nil, 0, nil
	}
	outputScript, _ := hex.DecodeString(txOut.ScriptPubKey.Hex)
	// Check equivalence of pkScript and outputScript?
	return wire.NewTxOut(int64(toSatoshi(txOut.Value)), outputScript), uint32(txOut.Confirmations), nil
}

// getTxOut returns the transaction output info if it's unspent and
// nil, otherwise.
func (wc *rpcClient) getTxOutput(txHash *chainhash.Hash, index uint32) (*btcjson.GetTxOutResult, error) {
	// Note that we pass to call pointer to a pointer (&res) so that
	// json.Unmarshal can nil the pointer if the method returns the JSON null.
	var res *btcjson.GetTxOutResult
	return res, wc.call(methodGetTxOut, anylist{txHash.String(), index, true},
		&res)
}

// locked returns the wallet's lock state.
func (wc *rpcClient) locked() bool {
	walletInfo, err := wc.GetWalletInfo()
	if err != nil {
		wc.log.Errorf("GetWalletInfo error: %w", err)
		return false
	}
	if walletInfo.UnlockedUntil == nil {
		// This wallet is not encrypted.
		return false
	}

	return time.Unix(*walletInfo.UnlockedUntil, 0).Before(time.Now())
}

func (wc *rpcClient) callHashGetter(method string, args anylist) (*chainhash.Hash, error) {
	var txid string
	err := wc.call(method, args, &txid)
	if err != nil {
		return nil, err
	}
	return chainhash.NewHashFromStr(txid)
}

// getBlock fetches the MsgBlock.
func (wc *rpcClient) getBlock(h chainhash.Hash) (*wire.MsgBlock, error) {
	var txB dex.Bytes
	err := wc.call(methodGetBlock, anylist{h.String(), 0}, &txB)
	if err != nil {
		return nil, err
	}
	msgBlock := &wire.MsgBlock{}
	return msgBlock, msgBlock.Deserialize(bytes.NewReader(txB))
}

// getBlockHash returns the hash of the block in the best block chain at the
// given height.
func (wc *rpcClient) getBlockHash(blockHeight int64) (*chainhash.Hash, error) {
	return wc.callHashGetter(methodGetBlockHash, anylist{blockHeight})
}

// getBestBlockHash returns the hash of the best block in the longest block
// chain.
func (wc *rpcClient) getBestBlockHash() (*chainhash.Hash, error) {
	return wc.callHashGetter(methodGetBestBlockHash, nil)
}

// getBestBlockHeight returns the height of the top mainchain block.
func (wc *rpcClient) getBestBlockHeight() (int32, error) {
	tipHash, err := wc.getBestBlockHash()
	if err != nil {
		return -1, err
	}
	header, err := wc.getBlockHeader(tipHash.String())
	if err != nil {
		return -1, err
	}
	return int32(header.Height), nil
}

// GetRawMempool returns the hashes of all transactions in the memory pool.
func (wc *rpcClient) GetRawMempool() ([]*chainhash.Hash, error) {
	var mempool []string
	err := wc.call(methodGetRawMempool, nil, &mempool)
	if err != nil {
		return nil, err
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

// GetRawTransaction retrieves the MsgTx.
func (wc *rpcClient) GetRawTransaction(txHash *chainhash.Hash) (*wire.MsgTx, error) {
	var txB dex.Bytes
	err := wc.call(methodGetRawTransaction, anylist{txHash.String(), false}, &txB)
	if err != nil {
		return nil, err
	}
	tx := wire.NewMsgTx(wire.TxVersion)
	return tx, tx.Deserialize(bytes.NewReader(txB))
}

// balances retrieves a wallet's balance details.
func (wc *rpcClient) balances() (*GetBalancesResult, error) {
	var balances GetBalancesResult
	return &balances, wc.call(methodGetBalances, nil, &balances)
}

// listUnspent retrieves a list of the wallet's UTXOs.
func (wc *rpcClient) listUnspent() ([]*ListUnspentResult, error) {
	unspents := make([]*ListUnspentResult, 0)
	// TODO: listunspent 0 9999999 []string{}, include_unsafe=false
	return unspents, wc.call(methodListUnspent, anylist{uint8(0)}, &unspents)
}

// lockUnspent locks and unlocks outputs for spending. An output that is part of
// an order, but not yet spent, should be locked until spent or until the order
// is  canceled or fails.
func (wc *rpcClient) lockUnspent(unlock bool, ops []*output) error {
	var rpcops []*RPCOutpoint // To clear all, this must be nil, not empty slice.
	for _, op := range ops {
		rpcops = append(rpcops, &RPCOutpoint{
			TxID: op.txHash().String(),
			Vout: op.vout(),
		})
	}
	var success bool
	err := wc.call(methodLockUnspent, anylist{unlock, rpcops}, &success)
	if err == nil && !success {
		return fmt.Errorf("lockunspent unsuccessful")
	}
	return err
}

// listLockUnspent returns a slice of outpoints for all unspent outputs marked
// as locked by a wallet.
func (wc *rpcClient) listLockUnspent() ([]*RPCOutpoint, error) {
	var unspents []*RPCOutpoint
	err := wc.call(methodListLockUnspent, nil, &unspents)
	return unspents, err
}

// changeAddress gets a new internal address from the wallet. The address will
// be bech32-encoded (P2WPKH).
func (wc *rpcClient) changeAddress() (btcutil.Address, error) {
	var addrStr string
	var err error
	switch {
	case wc.arglessChangeAddrRPC:
		err = wc.call(methodChangeAddress, nil, &addrStr)
	case wc.segwit:
		err = wc.call(methodChangeAddress, anylist{"bech32"}, &addrStr)
	default:
		err = wc.call(methodChangeAddress, anylist{"legacy"}, &addrStr)
	}
	if err != nil {
		return nil, err
	}
	return wc.decodeAddr(addrStr, wc.chainParams)
}

// AddressPKH gets a new base58-encoded (P2PKH) external address from the
// wallet.
func (wc *rpcClient) addressPKH() (btcutil.Address, error) {
	return wc.address("legacy")
}

// addressWPKH gets a new bech32-encoded (P2WPKH) external address from the
// wallet.
func (wc *rpcClient) addressWPKH() (btcutil.Address, error) {
	return wc.address("bech32")
}

// address is used internally for fetching addresses of various types from the
// wallet.
func (wc *rpcClient) address(aType string) (btcutil.Address, error) {
	var addrStr string
	err := wc.call(methodNewAddress, anylist{"", aType}, &addrStr)
	if err != nil {
		return nil, err
	}
	return wc.decodeAddr(addrStr, wc.chainParams)
}

// signTx attempts to have the wallet sign the transaction inputs.
func (wc *rpcClient) signTx(inTx *wire.MsgTx) (*wire.MsgTx, error) {
	txBytes, err := serializeMsgTx(inTx)
	if err != nil {
		return nil, fmt.Errorf("tx serialization error: %w", err)
	}
	res := new(SignTxResult)
	err = wc.call(methodSignTx, anylist{hex.EncodeToString(txBytes)}, res)
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
	outTx, err := msgTxFromBytes(res.Hex)
	if err != nil {
		return nil, fmt.Errorf("error deserializing transaction response: %w", err)
	}
	return outTx, nil
}

// privKeyForAddress retrieves the private key associated with the specified
// address.
func (wc *rpcClient) privKeyForAddress(addr string) (*btcec.PrivateKey, error) {
	var keyHex string
	err := wc.call(methodPrivKeyForAddress, anylist{addr}, &keyHex)
	if err != nil {
		return nil, err
	}
	wif, err := btcutil.DecodeWIF(keyHex)
	if err != nil {
		return nil, err
	}
	return wif.PrivKey, nil
}

// getTransaction retrieves the JSON-RPC gettransaction result.
func (wc *rpcClient) getTransaction(txHash *chainhash.Hash) (*GetTransactionResult, error) {
	tx := new(GetTransactionResult)
	err := wc.call(methodGetTransaction, anylist{txHash.String()}, tx)
	if err != nil {
		return nil, err
	}
	return tx, nil
}

// walletUnlock unlocks the wallet.
func (wc *rpcClient) walletUnlock(pass string) error {
	// 100000000 comes from bitcoin-cli help walletpassphrase
	return wc.call(methodUnlock, anylist{pass, 100000000}, nil)
}

// walletLock locks the wallet.
func (wc *rpcClient) walletLock() error {
	return wc.call(methodLock, nil, nil)
}

// sendToAddress sends the amount to the address. feeRate is in units of
// atoms/byte.
func (wc *rpcClient) sendToAddress(address string, value, feeRate uint64, subtract bool) (*chainhash.Hash, error) {
	var success bool
	// 1e-5 = 1e-8 for satoshis * 1000 for kB.
	err := wc.call(methodSetTxFee, anylist{float64(feeRate) / 1e5}, &success)
	if err != nil {
		return nil, fmt.Errorf("error setting transaction fee: %w", err)
	}
	if !success {
		return nil, fmt.Errorf("failed to set transaction fee")
	}
	var txid string
	// Last boolean argument is to subtract the fee from the amount.
	coinValue := btcutil.Amount(value).ToBTC()
	err = wc.call(methodSendToAddress, anylist{address, coinValue, "dcrdex", "", subtract}, &txid)
	if err != nil {
		return nil, err
	}
	return chainhash.NewHashFromStr(txid)
}

// GetWalletInfo gets the getwalletinfo RPC result.
func (wc *rpcClient) GetWalletInfo() (*GetWalletInfoResult, error) {
	wi := new(GetWalletInfoResult)
	return wi, wc.call(methodGetWalletInfo, nil, wi)
}

// GetAddressInfo gets information about the given address by calling
// getaddressinfo RPC command.
func (wc *rpcClient) getAddressInfo(addr btcutil.Address) (*GetAddressInfoResult, error) {
	ai := new(GetAddressInfoResult)
	return ai, wc.call(methodGetAddressInfo, anylist{addr.String()}, ai)
}

// ownsAddress indicates if an address belongs to the wallet.
func (wc *rpcClient) ownsAddress(addr btcutil.Address) (bool, error) {
	ai, err := wc.getAddressInfo(addr)
	if err != nil {
		return false, err
	}
	return ai.IsMine, nil
}

// syncStatus is the current synchronization state of the node.
type syncStatus struct {
	Target  int32 `json:"target"`
	Height  int32 `json:"height"`
	Syncing bool  `json:"syncing"`
}

// syncStatus is information about the blockchain sync status.
func (wc *rpcClient) syncStatus() (*syncStatus, error) {
	chainInfo, err := wc.getBlockchainInfo()
	if err != nil {
		return nil, fmt.Errorf("getblockchaininfo error: %w", err)
	}
	return &syncStatus{
		Target:  int32(chainInfo.Headers),
		Height:  int32(chainInfo.Blocks),
		Syncing: chainInfo.InitialBlockDownload || chainInfo.Headers-chainInfo.Blocks > 1,
	}, nil
}

// SendRawTransaction broadcasts the transaction.
func (wc *rpcClient) SendRawTransaction(tx *wire.MsgTx) (*chainhash.Hash, error) {
	b, err := serializeMsgTx(tx)
	if err != nil {
		return nil, err
	}
	var txid string
	err = wc.call(methodSendRawTx, anylist{hex.EncodeToString(b)}, &txid)
	if err != nil {
		return nil, err
	}
	return chainhash.NewHashFromStr(txid)
}

// swapConfirmations gets the number of confirmations for the specified coin ID
// by first checking for a unspent output, and if not found, searching indexed
// wallet transactions.
func (wc *rpcClient) swapConfirmations(txHash *chainhash.Hash, vout uint32, _ []byte, _ time.Time) (confs uint32, err error) {
	// Check for an unspent output.
	txOut, err := wc.getTxOutput(txHash, vout)
	if err == nil && txOut != nil {
		return uint32(txOut.Confirmations), nil
	}
	// Check wallet transactions.
	tx, err := wc.getTransaction(txHash)
	if err != nil {
		if isTxNotFoundErr(err) {
			return 0, asset.CoinNotFoundError
		}
		return 0, err
	}
	return uint32(tx.Confirmations), asset.ErrSpentSwap
}

// getBlockHeader gets the block header for the specified block hash.
func (wc *rpcClient) getBlockHeader(blockHash string) (*blockHeader, error) {
	blkHeader := new(blockHeader)
	err := wc.call(methodGetBlockHeader,
		anylist{blockHash, true}, blkHeader)
	if err != nil {
		return nil, err
	}
	return blkHeader, nil
}

// getBlockHeight gets the mainchain height for the specified block.
func (wc *rpcClient) getBlockHeight(h *chainhash.Hash) (int32, error) {
	hdr, err := wc.getBlockHeader(h.String())
	if err != nil {
		return -1, err
	}
	if hdr.Height < 0 {
		return -1, fmt.Errorf("block is not a mainchain block")
	}
	return int32(hdr.Height), nil
}

// getBlockchainInfo sends the getblockchaininfo request and returns the result.
func (wc *rpcClient) getBlockchainInfo() (*getBlockchainInfoResult, error) {
	chainInfo := new(getBlockchainInfoResult)
	err := wc.call(methodGetBlockchainInfo, nil, chainInfo)
	if err != nil {
		return nil, err
	}
	return chainInfo, nil
}

// getVersion gets the current BTC network and protocol versions.
func (wc *rpcClient) getVersion() (uint64, uint64, error) {
	r := &struct {
		Version         uint64 `json:"version"`
		ProtocolVersion uint64 `json:"protocolversion"`
	}{}
	err := wc.call(methodGetNetworkInfo, nil, r)
	if err != nil {
		return 0, 0, err
	}
	return r.Version, r.ProtocolVersion, nil
}

// findRedemptionsInMempool attempts to find spending info for the specified
// contracts by searching every input of all txs in the mempool.
func (wc *rpcClient) findRedemptionsInMempool(ctx context.Context, reqs map[outPoint]*findRedemptionReq) (discovered map[outPoint]*findRedemptionResult) {
	contractsCount := len(reqs)
	wc.log.Debugf("finding redemptions for %d contracts in mempool", contractsCount)

	discovered = make(map[outPoint]*findRedemptionResult, len(reqs))

	var totalFound, totalCanceled int
	logAbandon := func(reason string) {
		// Do not remove the contracts from the findRedemptionQueue
		// as they could be subsequently redeemed in some mined tx(s),
		// which would be captured when a new tip is reported.
		if totalFound+totalCanceled > 0 {
			wc.log.Debugf("%d redemptions found, %d canceled out of %d contracts in mempool",
				totalFound, totalCanceled, contractsCount)
		}
		wc.log.Errorf("abandoning mempool redemption search for %d contracts because of %s",
			contractsCount-totalFound-totalCanceled, reason)
	}

	mempoolTxs, err := wc.GetRawMempool()
	if err != nil {
		logAbandon(fmt.Sprintf("error retrieving transactions: %v", err))
		return
	}

	for _, txHash := range mempoolTxs {
		if ctx.Err() != nil {
			return nil
		}
		tx, err := wc.GetRawTransaction(txHash)
		if err != nil {
			logAbandon(fmt.Sprintf("getrawtransaction error for tx hash %v: %v", txHash, err))
			return
		}
		newlyDiscovered := findRedemptionsInTx(ctx, wc.segwit, reqs, tx, wc.chainParams)
		for outPt, res := range newlyDiscovered {
			discovered[outPt] = res
		}

	}
	return
}

// findRedemptionsInTx searches the MsgTx for the redemptions for the specified
// swaps.
func findRedemptionsInTx(ctx context.Context, segwit bool, reqs map[outPoint]*findRedemptionReq, msgTx *wire.MsgTx,
	chainParams *chaincfg.Params) (discovered map[outPoint]*findRedemptionResult) {

	discovered = make(map[outPoint]*findRedemptionResult, len(reqs))

	for vin, txIn := range msgTx.TxIn {
		if ctx.Err() != nil {
			return discovered
		}
		poHash, poVout := txIn.PreviousOutPoint.Hash, txIn.PreviousOutPoint.Index
		for outPt, req := range reqs {
			if discovered[outPt] != nil {
				continue
			}
			if outPt.txHash == poHash && outPt.vout == poVout {
				// Match!
				txHash := msgTx.TxHash()
				secret, err := dexbtc.FindKeyPush(txIn.Witness, txIn.SignatureScript, req.contractHash[:], segwit, chainParams)
				if err != nil {
					req.fail("no secret extracted from redemption input %s:%d for swap output %s: %v",
						msgTx.TxHash(), vin, outPt, err)
					continue
				}
				discovered[outPt] = &findRedemptionResult{
					redemptionCoinID: toCoinID(&txHash, uint32(vin)),
					secret:           secret,
				}
			}
		}
	}
	return
}

// searchBlockForRedemptions attempts to find spending info for the specified
// contracts by searching every input of all txs in the provided block range.
func (wc *rpcClient) searchBlockForRedemptions(ctx context.Context, reqs map[outPoint]*findRedemptionReq, blockHash chainhash.Hash) (discovered map[outPoint]*findRedemptionResult) {
	msgBlock, err := wc.getBlock(blockHash)
	if err != nil {
		wc.log.Errorf("RPC GetBlock error: %v", err)
		return
	}

	discovered = make(map[outPoint]*findRedemptionResult, len(reqs))

	for _, msgTx := range msgBlock.Transactions {
		newlyDiscovered := findRedemptionsInTx(ctx, wc.segwit, reqs, msgTx, wc.chainParams)
		for outPt, res := range newlyDiscovered {
			discovered[outPt] = res
		}
	}
	return
}

// call is used internally to marshal parmeters and send requests to the RPC
// server via (*rpcclient.Client).RawRequest. If `thing` is non-nil, the result
// will be marshaled into `thing`.
func (wc *rpcClient) call(method string, args anylist, thing interface{}) error {
	params := make([]json.RawMessage, 0, len(args))
	for i := range args {
		p, err := json.Marshal(args[i])
		if err != nil {
			return err
		}
		params = append(params, p)
	}

	b, err := wc.requester.RawRequest(wc.ctx, method, params)
	if err != nil {
		return fmt.Errorf("rawrequest error: %w", err)
	}
	if thing != nil {
		return json.Unmarshal(b, thing)
	}
	return nil
}

// serializeMsgTx serializes the wire.MsgTx.
func serializeMsgTx(msgTx *wire.MsgTx) ([]byte, error) {
	buf := bytes.NewBuffer(make([]byte, 0, msgTx.SerializeSize()))
	err := msgTx.Serialize(buf)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// msgTxFromHex creates a wire.MsgTx by deserializing the hex-encoded
// transaction.
func msgTxFromHex(txHex string) (*wire.MsgTx, error) {
	b, err := hex.DecodeString(txHex)
	if err != nil {
		return nil, err
	}
	return msgTxFromBytes(b)
}

// msgTxFromBytes creates a wire.MsgTx by deserializing the transaction.
func msgTxFromBytes(txB []byte) (*wire.MsgTx, error) {
	msgTx := wire.NewMsgTx(wire.TxVersion)
	if err := msgTx.Deserialize(bytes.NewReader(txB)); err != nil {
		return nil, err
	}
	return msgTx, nil
}
