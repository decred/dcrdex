// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package btc

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/config"
	dexbtc "decred.org/dcrdex/dex/networks/btc"
	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
	"github.com/decred/dcrd/dcrjson/v4" // for dcrjson.RPCError returns from rpcclient
)

const (
	methodGetBalances          = "getbalances"
	methodGetBalance           = "getbalance"
	methodListUnspent          = "listunspent"
	methodLockUnspent          = "lockunspent"
	methodListLockUnspent      = "listlockunspent"
	methodChangeAddress        = "getrawchangeaddress"
	methodNewAddress           = "getnewaddress"
	methodSignTx               = "signrawtransactionwithwallet"
	methodSignTxLegacy         = "signrawtransaction"
	methodUnlock               = "walletpassphrase"
	methodLock                 = "walletlock"
	methodPrivKeyForAddress    = "dumpprivkey"
	methodGetTransaction       = "gettransaction"
	methodSendToAddress        = "sendtoaddress"
	methodSetTxFee             = "settxfee"
	methodGetWalletInfo        = "getwalletinfo"
	methodGetAddressInfo       = "getaddressinfo"
	methodListDescriptors      = "listdescriptors"
	methodValidateAddress      = "validateaddress"
	methodEstimateSmartFee     = "estimatesmartfee"
	methodSendRawTransaction   = "sendrawtransaction"
	methodGetTxOut             = "gettxout"
	methodGetBlock             = "getblock"
	methodGetBlockHash         = "getblockhash"
	methodGetBestBlockHash     = "getbestblockhash"
	methodGetRawMempool        = "getrawmempool"
	methodGetRawTransaction    = "getrawtransaction"
	methodGetBlockHeader       = "getblockheader"
	methodGetNetworkInfo       = "getnetworkinfo"
	methodGetBlockchainInfo    = "getblockchaininfo"
	methodFundRawTransaction   = "fundrawtransaction"
	methodListSinceBlock       = "listsinceblock"
	methodGetReceivedByAddress = "getreceivedbyaddress"
)

// IsTxNotFoundErr will return true if the error indicates that the requested
// transaction is not known. The error must be dcrjson.RPCError with a numeric
// code equal to btcjson.ErrRPCNoTxInfo. WARNING: This is specific to errors
// from an RPC to a bitcoind (or clone) using dcrd's rpcclient!
func IsTxNotFoundErr(err error) bool {
	// We are using dcrd's client with Bitcoin Core, so errors will be of type
	// dcrjson.RPCError, but numeric codes should come from btcjson.
	const errRPCNoTxInfo = int(btcjson.ErrRPCNoTxInfo)
	var rpcErr *dcrjson.RPCError
	return errors.As(err, &rpcErr) && int(rpcErr.Code) == errRPCNoTxInfo
}

// isMethodNotFoundErr will return true if the error indicates that the RPC
// method was not found by the RPC server. The error must be dcrjson.RPCError
// with a numeric code equal to btcjson.ErrRPCMethodNotFound.Code or a message
// containing "method not found".
func isMethodNotFoundErr(err error) bool {
	var errRPCMethodNotFound = int(btcjson.ErrRPCMethodNotFound.Code)
	var rpcErr *dcrjson.RPCError
	return errors.As(err, &rpcErr) &&
		(int(rpcErr.Code) == errRPCMethodNotFound ||
			strings.Contains(strings.ToLower(rpcErr.Message), "method not found"))
}

// RawRequester defines decred's rpcclient RawRequest func where all RPC
// requests sent through. For testing, it can be satisfied by a stub.
type RawRequester interface {
	RawRequest(context.Context, string, []json.RawMessage) (json.RawMessage, error)
}

// anylist is a list of RPC parameters to be converted to []json.RawMessage and
// sent via RawRequest.
type anylist []any

type rpcCore struct {
	rpcConfig         *RPCConfig
	cloneParams       *BTCCloneCFG
	requesterV        atomic.Value // RawRequester
	segwit            bool
	decodeAddr        dexbtc.AddressDecoder
	stringAddr        dexbtc.AddressStringer
	legacyRawSends    bool
	minNetworkVersion uint64
	minDescriptorVersion uint64
	
	log               dex.Logger
	chainParams       *chaincfg.Params
	omitAddressType   bool
	legacySignTx      bool
	booleanGetBlock   bool
	unlockSpends      bool

	deserializeTx      func([]byte) (*wire.MsgTx, error)
	serializeTx        func(*wire.MsgTx) ([]byte, error)
	hashTx             func(*wire.MsgTx) *chainhash.Hash
	numericGetRawTxRPC bool
	manualMedianTime   bool
	addrFunc           func() (btcutil.Address, error)

	deserializeBlock         func([]byte) (*wire.MsgBlock, error)
	legacyValidateAddressRPC bool
	omitRPCOptionsArg        bool
	privKeyFunc              func(addr string) (*btcec.PrivateKey, error)
}

func (c *rpcCore) requester() RawRequester {
	return c.requesterV.Load().(RawRequester)
}

// rpcClient is a bitcoind JSON RPC client that uses rpcclient.Client's
// RawRequest for wallet-related calls.
type rpcClient struct {
	*rpcCore
	ctx         context.Context
	descriptors bool // set on connect like ctx
}

var _ Wallet = (*rpcClient)(nil)

// newRPCClient is the constructor for a rpcClient.
func newRPCClient(cfg *rpcCore) *rpcClient {
	return &rpcClient{rpcCore: cfg}
}

// ChainOK is for screening the chain field of the getblockchaininfo result.
func ChainOK(net dex.Network, str string) bool {
	var chainStr string
	switch net {
	case dex.Mainnet:
		chainStr = "main"
	case dex.Testnet:
		chainStr = "test"
	case dex.Regtest:
		chainStr = "reg"
	}
	return strings.Contains(str, chainStr)
}

func (wc *rpcClient) Connect(ctx context.Context, _ *sync.WaitGroup) error {
	wc.ctx = ctx
	// Check the version. Do it here, so we can also diagnose a bad connection.
	netVer, codeVer, err := wc.getVersion()
	if err != nil {
		return fmt.Errorf("error getting version: %w", err)
	}
	if netVer < wc.minNetworkVersion {
		return fmt.Errorf("reported node version %d is less than minimum %d", netVer, wc.minNetworkVersion)
	}
	// TODO: codeVer is actually asset-dependent. Zcash, for example, is at
	// 170100. So we're just lucking out here, really.
	if codeVer < minProtocolVersion {
		return fmt.Errorf("node software out of date. version %d is less than minimum %d", codeVer, minProtocolVersion)
	}
	chainInfo, err := wc.getBlockchainInfo()
	if err != nil {
		return fmt.Errorf("getblockchaininfo error: %w", err)
	}
	if !ChainOK(wc.cloneParams.Network, chainInfo.Chain) {
		return errors.New("wrong net")
	}
	wiRes, err := wc.GetWalletInfo()
	if err != nil {
		return fmt.Errorf("getwalletinfo failure: %w", err)
	}
	wc.descriptors = wiRes.Descriptors
	if wc.descriptors {
		if netVer < wc.minDescriptorVersion {
			return fmt.Errorf("reported node version %d is less than minimum %d"+
				" for descriptor wallets", netVer, wc.minDescriptorVersion)
		}
		wc.log.Debug("Using a descriptor wallet.")
	}
	return nil
}

// Reconfigure attempts to reconfigure the rpcClient for the new settings. Live
// reconfiguration is only attempted if the new wallet type is walletTypeRPC. If
// the special_activelyUsed flag is set, reconfigure will fail if we can't
// validate ownership of the current deposit address.
func (wc *rpcClient) Reconfigure(cfg *asset.WalletConfig, currentAddress string) (restartRequired bool, err error) {
	if cfg.Type != wc.cloneParams.WalletCFG.Type {
		restartRequired = true
		return
	}
	if wc.ctx == nil || wc.ctx.Err() != nil {
		return true, nil // not connected, ok to reconfigure, but restart required
	}

	parsedCfg := new(RPCWalletConfig)
	if err = config.Unmapify(cfg.Settings, parsedCfg); err != nil {
		return
	}

	// Check the RPC configuration.
	newCfg := &parsedCfg.RPCConfig
	if err = dexbtc.CheckRPCConfig(&newCfg.RPCConfig, wc.cloneParams.WalletInfo.Name,
		wc.cloneParams.Network, wc.cloneParams.Ports); err != nil {
		return
	}

	// If the RPC configuration has changed, try to update the client.
	oldCfg := wc.rpcConfig
	if *newCfg != *oldCfg {
		cl, err := newRPCConnection(parsedCfg, wc.cloneParams.SingularWallet)
		if err != nil {
			return false, fmt.Errorf("error creating RPC client with new credentials: %v", err)
		}

		// Require restart if the wallet does not own or understand our current
		// address. We can't use wc.ownsAddress because the rpcClient still has
		// the old requester stored, so we'll call directly.
		method := methodGetAddressInfo
		if wc.legacyValidateAddressRPC {
			method = methodValidateAddress
		}
		ai := new(GetAddressInfoResult)
		if err := Call(wc.ctx, cl, method, anylist{currentAddress}, ai); err != nil {
			return false, fmt.Errorf("error getting address info with new RPC client: %w", err)
		} else if !ai.IsMine {
			// If the wallet is in active use, check the supplied address.
			if parsedCfg.ActivelyUsed { // deny reconfigure
				return false, errors.New("cannot reconfigure to a new RPC wallet during active use")
			}
			// Allow reconfigure, but restart to trigger dep address refresh and
			// full connect checks, which include the getblockchaininfo check.
			return true, nil
		} // else same wallet, skip full reconnect

		chainInfo := new(GetBlockchainInfoResult)
		if err := Call(wc.ctx, cl, methodGetBlockchainInfo, nil, chainInfo); err != nil {
			return false, fmt.Errorf("%s: %w", methodGetBlockchainInfo, err)
		}
		if !ChainOK(wc.cloneParams.Network, chainInfo.Chain) {
			return false, errors.New("wrong net")
		}

		wc.requesterV.Store(cl)
		wc.rpcConfig = newCfg

		// No restart required
	}
	return
}

// RawRequest passes the request to the wallet's RawRequester.
func (wc *rpcClient) RawRequest(ctx context.Context, method string, params []json.RawMessage) (json.RawMessage, error) {
	return wc.requester().RawRequest(ctx, method, params)
}

// estimateSmartFee requests the server to estimate a fee level based on the
// given parameters.
func estimateSmartFee(ctx context.Context, rr RawRequester, confTarget uint64, mode *btcjson.EstimateSmartFeeMode) (*btcjson.EstimateSmartFeeResult, error) {
	res := new(btcjson.EstimateSmartFeeResult)
	return res, Call(ctx, rr, methodEstimateSmartFee, anylist{confTarget, mode}, res)
}

// SendRawTransactionLegacy broadcasts the transaction with an additional legacy
// boolean `allowhighfees` argument set to false.
func (wc *rpcClient) SendRawTransactionLegacy(tx *wire.MsgTx) (*chainhash.Hash, error) {
	txBytes, err := wc.serializeTx(tx)
	if err != nil {
		return nil, err
	}
	return wc.callHashGetter(methodSendRawTransaction, anylist{
		hex.EncodeToString(txBytes), false})
}

// SendRawTransaction broadcasts the transaction.
func (wc *rpcClient) SendRawTransaction(tx *wire.MsgTx) (*chainhash.Hash, error) {
	b, err := wc.serializeTx(tx)
	if err != nil {
		return nil, err
	}
	var txid string
	err = wc.call(methodSendRawTransaction, anylist{hex.EncodeToString(b)}, &txid)
	if err != nil {
		return nil, err
	}
	return chainhash.NewHashFromStr(txid)
}

// sendRawTransaction sends the MsgTx.
func (wc *rpcClient) sendRawTransaction(tx *wire.MsgTx) (txHash *chainhash.Hash, err error) {
	if wc.legacyRawSends {
		txHash, err = wc.SendRawTransactionLegacy(tx)
	} else {
		txHash, err = wc.SendRawTransaction(tx)
	}
	if err != nil {
		return nil, err
	}
	if !wc.unlockSpends {
		return txHash, nil
	}

	// TODO: lockUnspent should really just take a []*OutPoint, since it doesn't
	// need the value.
	ops := make([]*Output, 0, len(tx.TxIn))
	for _, txIn := range tx.TxIn {
		prevOut := &txIn.PreviousOutPoint
		ops = append(ops, &Output{Pt: NewOutPoint(&prevOut.Hash, prevOut.Index)})
	}
	if err := wc.LockUnspent(true, ops); err != nil {
		wc.log.Warnf("error unlocking spent outputs: %v", err)
	}
	return txHash, nil
}

// GetTxOut returns the transaction output info if it's unspent and
// nil, otherwise.
func (wc *rpcClient) GetTxOut(txHash *chainhash.Hash, index uint32, _ []byte, _ time.Time) (*wire.TxOut, uint32, error) {
	txOut, err := wc.getTxOutput(txHash, index)
	if err != nil {
		return nil, 0, fmt.Errorf("getTxOut error: %w", err)
	}
	if txOut == nil {
		return nil, 0, nil
	}
	outputScript, _ := hex.DecodeString(txOut.ScriptPubKey.Hex)
	// Check equivalence of pkScript and outputScript?
	return wire.NewTxOut(int64(toSatoshi(txOut.Value)), outputScript), uint32(txOut.Confirmations), nil
}

// getTxOutput returns the transaction output info if it's unspent and
// nil, otherwise.
func (wc *rpcClient) getTxOutput(txHash *chainhash.Hash, index uint32) (*btcjson.GetTxOutResult, error) {
	// Note that we pass to call pointer to a pointer (&res) so that
	// json.Unmarshal can nil the pointer if the method returns the JSON null.
	var res *btcjson.GetTxOutResult
	return res, wc.call(methodGetTxOut, anylist{txHash.String(), index, true},
		&res)
}

func (wc *rpcClient) callHashGetter(method string, args anylist) (*chainhash.Hash, error) {
	var txid string
	err := wc.call(method, args, &txid)
	if err != nil {
		return nil, err
	}
	return chainhash.NewHashFromStr(txid)
}

// GetBlock fetches the MsgBlock.
func (wc *rpcClient) GetBlock(h chainhash.Hash) (*wire.MsgBlock, error) {
	var blkB dex.Bytes
	args := anylist{h.String()}
	if wc.booleanGetBlock {
		args = append(args, false)
	} else {
		args = append(args, 0)
	}
	err := wc.call(methodGetBlock, args, &blkB)
	if err != nil {
		return nil, err
	}

	return wc.deserializeBlock(blkB)
}

// GetBlockHash returns the hash of the block in the best block chain at the
// given height.
func (wc *rpcClient) GetBlockHash(blockHeight int64) (*chainhash.Hash, error) {
	return wc.callHashGetter(methodGetBlockHash, anylist{blockHeight})
}

// GetBestBlockHash returns the hash of the best block in the longest block
// chain (aka mainchain).
func (wc *rpcClient) GetBestBlockHash() (*chainhash.Hash, error) {
	return wc.callHashGetter(methodGetBestBlockHash, nil)
}

// GetBestBlockHeader returns the height of the top mainchain block.
func (wc *rpcClient) GetBestBlockHeader() (*BlockHeader, error) {
	tipHash, err := wc.GetBestBlockHash()
	if err != nil {
		return nil, err
	}
	hdr, _, err := wc.GetBlockHeader(tipHash)
	return hdr, err
}

// GetBestBlockHeight returns the height of the top mainchain block.
func (wc *rpcClient) GetBestBlockHeight() (int32, error) {
	header, err := wc.GetBestBlockHeader()
	if err != nil {
		return -1, err
	}
	return int32(header.Height), nil
}

// getChainStamp satisfies chainStamper for manual median time calculations.
func (wc *rpcClient) getChainStamp(blockHash *chainhash.Hash) (stamp time.Time, prevHash *chainhash.Hash, err error) {
	hdr, _, err := wc.GetBlockHeader(blockHash)
	if err != nil {
		return
	}
	prevHash, err = chainhash.NewHashFromStr(hdr.PreviousBlockHash)
	if err != nil {
		return
	}
	return time.Unix(hdr.Time, 0).UTC(), prevHash, nil
}

// MedianTime is the median time for the current best block.
func (wc *rpcClient) MedianTime() (stamp time.Time, err error) {
	tipHash, err := wc.GetBestBlockHash()
	if err != nil {
		return
	}
	if wc.manualMedianTime {
		return CalcMedianTime(func(blockHash *chainhash.Hash) (stamp time.Time, prevHash *chainhash.Hash, err error) {
			hdr, _, err := wc.GetBlockHeader(blockHash)
			if err != nil {
				return
			}
			prevHash, err = chainhash.NewHashFromStr(hdr.PreviousBlockHash)
			if err != nil {
				return
			}
			return time.Unix(hdr.Time, 0), prevHash, nil
		}, tipHash)
	}
	hdr, err := wc.getRPCBlockHeader(tipHash)
	if err != nil {
		return
	}
	return time.Unix(hdr.MedianTime, 0).UTC(), nil
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
	args := anylist{txHash.String(), false}
	if wc.numericGetRawTxRPC {
		args[1] = 0
	}
	err := wc.call(methodGetRawTransaction, args, &txB)
	if err != nil {
		return nil, err
	}

	return wc.deserializeTx(txB)
}

// Balances retrieves a wallet's balance details.
func (wc *rpcClient) Balances() (*GetBalancesResult, error) {
	var balances GetBalancesResult
	return &balances, wc.call(methodGetBalances, nil, &balances)
}

// ListUnspent retrieves a list of the wallet's UTXOs.
func (wc *rpcClient) ListUnspent() ([]*ListUnspentResult, error) {
	unspents := make([]*ListUnspentResult, 0)
	// TODO: listunspent 0 9999999 []string{}, include_unsafe=false
	return unspents, wc.call(methodListUnspent, anylist{uint8(0)}, &unspents)
}

// LockUnspent locks and unlocks outputs for spending. An output that is part of
// an order, but not yet spent, should be locked until spent or until the order
// is canceled or fails.
func (wc *rpcClient) LockUnspent(unlock bool, ops []*Output) error {
	var rpcops []*RPCOutpoint // To clear all, this must be nil->null, not empty slice.
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

// ListLockUnspent returns a slice of outpoints for all unspent outputs marked
// as locked by a wallet.
func (wc *rpcClient) ListLockUnspent() ([]*RPCOutpoint, error) {
	var unspents []*RPCOutpoint
	err := wc.call(methodListLockUnspent, nil, &unspents)
	if err != nil {
		return nil, err
	}
	if !wc.unlockSpends {
		return unspents, nil
	}
	// This is quirky wallet software that does not unlock spent outputs, so
	// we'll verify that each output is actually unspent.
	var i int // for in-place filter
	for _, utxo := range unspents {
		var gtxo *btcjson.GetTxOutResult
		err = wc.call(methodGetTxOut, anylist{utxo.TxID, utxo.Vout, true}, &gtxo)
		if err != nil {
			wc.log.Warnf("gettxout(%v:%d): %v", utxo.TxID, utxo.Vout, err)
			continue
		}
		if gtxo != nil {
			unspents[i] = utxo // unspent, keep it
			i++
			continue
		}
		// actually spent, unlock
		var success bool
		op := []*RPCOutpoint{{
			TxID: utxo.TxID,
			Vout: utxo.Vout,
		}}
		err = wc.call(methodLockUnspent, anylist{true, op}, &success)
		if err != nil || !success {
			wc.log.Warnf("lockunspent(unlocking %v:%d): success = %v, err = %v",
				utxo.TxID, utxo.Vout, success, err)
			continue
		}
		wc.log.Debugf("Unlocked spent outpoint %v:%d", utxo.TxID, utxo.Vout)
	}
	unspents = unspents[:i]
	return unspents, nil
}

// ChangeAddress gets a new internal address from the wallet. The address will
// be bech32-encoded (P2WPKH).
func (wc *rpcClient) ChangeAddress() (btcutil.Address, error) {
	var addrStr string
	var err error
	switch {
	case wc.omitAddressType:
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

func (wc *rpcClient) ExternalAddress() (btcutil.Address, error) {
	if wc.segwit {
		return wc.address("bech32")
	}
	return wc.address("legacy")
}

// address is used internally for fetching addresses of various types from the
// wallet.
func (wc *rpcClient) address(aType string) (btcutil.Address, error) {
	var addrStr string
	args := anylist{""}
	if !wc.omitAddressType {
		args = append(args, aType)
	}
	err := wc.call(methodNewAddress, args, &addrStr)
	if err != nil {
		return nil, err
	}
	return wc.decodeAddr(addrStr, wc.chainParams) // we should consider returning a string
}

// SignTx attempts to have the wallet sign the transaction inputs.
func (wc *rpcClient) SignTx(inTx *wire.MsgTx) (*wire.MsgTx, error) {
	txBytes, err := wc.serializeTx(inTx)
	if err != nil {
		return nil, fmt.Errorf("tx serialization error: %w", err)
	}
	res := new(SignTxResult)
	method := methodSignTx
	if wc.legacySignTx {
		method = methodSignTxLegacy
	}

	err = wc.call(method, anylist{hex.EncodeToString(txBytes)}, res)
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
	outTx, err := wc.deserializeTx(res.Hex)
	if err != nil {
		return nil, fmt.Errorf("error deserializing transaction response: %w", err)
	}
	return outTx, nil
}

func (wc *rpcClient) listDescriptors(private bool) (*listDescriptorsResult, error) {
	descriptors := new(listDescriptorsResult)
	return descriptors, wc.call(methodListDescriptors, anylist{private}, descriptors)
}

func (wc *rpcClient) ListTransactionsSinceBlock(blockHeight int32) ([]*ListTransactionsResult, error) {
	blockHash, err := wc.GetBlockHash(int64(blockHeight))
	if err != nil {
		return nil, fmt.Errorf("getBlockHash error: %w", err)
	}
	result := new(struct {
		Transactions []btcjson.ListTransactionsResult `json:"transactions"`
	})
	err = wc.call(methodListSinceBlock, anylist{blockHash.String()}, result)
	if err != nil {
		return nil, fmt.Errorf("listtransactions error: %w", err)
	}

	txs := make([]*ListTransactionsResult, 0, len(result.Transactions))
	for _, tx := range result.Transactions {
		var blockHeight uint32
		if tx.BlockHeight != nil {
			blockHeight = uint32(*tx.BlockHeight)
		}
		txs = append(txs, &ListTransactionsResult{
			TxID:        tx.TxID,
			BlockHeight: blockHeight,
			BlockTime:   uint64(tx.BlockTime),
			Fee:         tx.Fee,
			Send:        tx.Category == "send",
		})
	}

	return txs, nil
}

// PrivKeyForAddress retrieves the private key associated with the specified
// address.
func (wc *rpcClient) PrivKeyForAddress(addr string) (*btcec.PrivateKey, error) {
	// Use a specialized client's privKey function
	if wc.privKeyFunc != nil {
		return wc.privKeyFunc(addr)
	}
	// Descriptor wallets do not have dumpprivkey.
	if !wc.descriptors {
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

	// With descriptor wallets, we have to get the address' descriptor from
	// getaddressinfo, parse out its key origin (fingerprint of the master
	// private key followed by derivation path to the address) and the pubkey of
	// the address itself. Then we get the private key using listdescriptors
	// private=true, which returns a set of master private keys and derivation
	// paths, one of which corresponds to the fingerprint and path from
	// getaddressinfo. When the parent master private key is identified, we
	// derive the private key for the address.
	ai := new(GetAddressInfoResult)
	if err := wc.call(methodGetAddressInfo, anylist{addr}, ai); err != nil {
		return nil, fmt.Errorf("getaddressinfo RPC failure: %w", err)
	}
	wc.log.Tracef("Address %v descriptor: %v", addr, ai.Descriptor)
	desc, err := dexbtc.ParseDescriptor(ai.Descriptor)
	if err != nil {
		return nil, fmt.Errorf("failed to parse descriptor %q: %w", ai.Descriptor, err)
	}
	if desc.KeyOrigin == nil {
		return nil, errors.New("address descriptor has no key origin")
	}
	// For addresses from imported private keys that have no derivation path in
	// the key origin, we inspect private keys of type KeyWIFPriv. For addresses
	// with a derivation path, we match KeyExtended private keys based on the
	// master key fingerprint and derivation path.
	fp, addrPath := desc.KeyOrigin.Fingerprint, desc.KeyOrigin.Steps
	// Should match:
	//   fp, path = ai.HDMasterFingerprint, ai.HDKeyPath
	//   addrPath, _, err = dexbtc.ParsePath(path)
	bareKey := len(addrPath) == 0

	if desc.KeyFmt != dexbtc.KeyHexPub {
		return nil, fmt.Errorf("not a hexadecimal pubkey: %v", desc.Key)
	}
	// The key was validated by ParseDescriptor, but check again.
	addrPubKeyB, err := hex.DecodeString(desc.Key)
	if err != nil {
		return nil, fmt.Errorf("address pubkey not hexadecimal: %w", err)
	}
	addrPubKey, err := btcec.ParsePubKey(addrPubKeyB)
	if err != nil {
		return nil, fmt.Errorf("invalid pubkey for address: %w", err)
	}
	addrPubKeyC := addrPubKey.SerializeCompressed() // may or may not equal addrPubKeyB

	// Get the private key descriptors.
	masterDescs, err := wc.listDescriptors(true)
	if err != nil {
		return nil, fmt.Errorf("listdescriptors RPC failure: %w", err)
	}

	// We're going to decode a number of private keys that we need to zero.
	var toClear []interface{ Zero() }
	defer func() {
		for _, k := range toClear {
			k.Zero()
		}
	}() // surprisingly, much cleaner than making the loop body below into a function
	deferZero := func(z interface{ Zero() }) { toClear = append(toClear, z) }

masters:
	for _, d := range masterDescs.Descriptors {
		masterDesc, err := dexbtc.ParseDescriptor(d.Descriptor)
		if err != nil {
			wc.log.Errorf("Failed to parse descriptor %q: %v", d.Descriptor, err)
			continue // unexpected, but check the others
		}
		if bareKey { // match KeyHexPub -> KeyWIFPriv
			if masterDesc.KeyFmt != dexbtc.KeyWIFPriv {
				continue
			}
			wif, err := btcutil.DecodeWIF(masterDesc.Key)
			if err != nil {
				wc.log.Errorf("Invalid WIF private key: %v", err)
				continue // ParseDescriptor already validated it, so shouldn't happen
			}
			if !bytes.Equal(addrPubKeyC, wif.PrivKey.PubKey().SerializeCompressed()) {
				continue // not the one
			}
			return wif.PrivKey, nil
		}

		// match KeyHexPub -> [fingerprint/path]KeyExtended
		if masterDesc.KeyFmt != dexbtc.KeyExtended {
			continue
		}
		// Break the key into its parts and compute the fingerprint of the
		// master private key.
		xPriv, fingerprint, pathStr, isRange, err := dexbtc.ParseKeyExtended(masterDesc.Key)
		if err != nil {
			wc.log.Debugf("Failed to parse descriptor extended key: %v", err)
			continue
		}
		deferZero(xPriv)
		if fingerprint != fp {
			continue
		}
		if !xPriv.IsPrivate() { // imported xpub with no private key?
			wc.log.Debugf("Not an extended private key. Fingerprint: %v", fingerprint)
			continue
		}
		// NOTE: After finding the xprv with the matching fingerprint, we could
		// skip to checking the private key for a match instead of first
		// matching the path. Let's just check the path too since fingerprint
		// collision are possible, and the different address types are allowed
		// to use descriptors with different fingerprints.
		if !isRange {
			continue // imported?
		}
		path, _, err := dexbtc.ParsePath(pathStr)
		if err != nil {
			wc.log.Debugf("Failed to parse descriptor extended key path %q: %v", pathStr, err)
			continue
		}
		if len(addrPath) != len(path)+1 { // addrPath includes index of self
			continue
		}
		for i := range path {
			if addrPath[i] != path[i] {
				continue masters // different path
			}
		}

		// NOTE: We could conceivably cache the extended private key for this
		// address range/branch, but it could be a security risk:
		// childIdx := addrPath[len(addrPath)-1]
		// branch, err := dexbtc.DeepChild(xPriv, path)
		// child, err := branch.Derive(childIdx)
		child, err := dexbtc.DeepChild(xPriv, addrPath)
		if err != nil {
			return nil, fmt.Errorf("address key derivation failed: %v", err) // any point in checking the rest?
		}
		deferZero(child)
		privkey, err := child.ECPrivKey()
		if err != nil { // only errors if the extended key is not private
			return nil, err // hdkeychain.ErrNotPrivExtKey
		}
		// That's the private key, but do a final check that the pubkey matches
		// the "pubkey" field of the getaddressinfo response.
		pubkey := privkey.PubKey().SerializeCompressed()
		if !bytes.Equal(pubkey, addrPubKeyC) {
			wc.log.Warnf("Derived wrong pubkey for address %v from matching descriptor %v: %x != %x",
				addr, d.Descriptor, pubkey, addrPubKey)
			continue // theoretically could be a fingerprint collision (see KeyOrigin docs)
		}
		return privkey, nil
	}

	return nil, errors.New("no private key found for address")
}

// GetWalletTransaction retrieves the JSON-RPC gettransaction result.
func (wc *rpcClient) GetWalletTransaction(txHash *chainhash.Hash) (*GetTransactionResult, error) {
	tx := new(GetTransactionResult)
	err := wc.call(methodGetTransaction, anylist{txHash.String()}, tx)
	if err != nil {
		if IsTxNotFoundErr(err) {
			return nil, asset.CoinNotFoundError
		}
		return nil, err
	}
	return tx, nil
}

// WalletUnlock unlocks the wallet.
func (wc *rpcClient) WalletUnlock(pw []byte) error {
	// 100000000 comes from bitcoin-cli help walletpassphrase
	return wc.call(methodUnlock, anylist{string(pw), 100000000}, nil)
}

// WalletLock locks the wallet.
func (wc *rpcClient) WalletLock() error {
	return wc.call(methodLock, nil, nil)
}

// Locked returns the wallet's lock state.
func (wc *rpcClient) Locked() bool {
	walletInfo, err := wc.GetWalletInfo()
	if err != nil {
		wc.log.Errorf("GetWalletInfo error: %v", err)
		return false
	}
	if walletInfo.UnlockedUntil == nil {
		// This wallet is not encrypted.
		return false
	}

	return time.Unix(*walletInfo.UnlockedUntil, 0).Before(time.Now())
}

// EstimateSendTxFee returns the fee required to send tx using the provided
// feeRate.
func (wc *rpcClient) EstimateSendTxFee(tx *wire.MsgTx, feeRate uint64, subtract bool) (txfee uint64, err error) {
	txBytes, err := wc.serializeTx(tx)
	if err != nil {
		return 0, fmt.Errorf("tx serialization error: %w", err)
	}
	args := anylist{hex.EncodeToString(txBytes)}

	// 1e-5 = 1e-8 for satoshis * 1000 for kB.
	feeRateOption := float64(feeRate) / 1e5
	options := &btcjson.FundRawTransactionOpts{
		FeeRate: &feeRateOption,
	}
	if !wc.omitAddressType {
		if wc.segwit {
			options.ChangeType = &btcjson.ChangeTypeBech32
		} else {
			options.ChangeType = &btcjson.ChangeTypeLegacy
		}
	}
	if subtract {
		options.SubtractFeeFromOutputs = []int{0}
	}
	args = append(args, options)

	var res struct {
		TxBytes dex.Bytes `json:"hex"`
		Fees    float64   `json:"fee"`
	}
	err = wc.call(methodFundRawTransaction, args, &res)
	if err != nil {
		wc.log.Debugf("%s fundrawtranasaction error for args %+v: %v \n", wc.cloneParams.WalletInfo.Name, args, err)
		// This is a work around for ZEC wallet, which does not support options
		// argument for fundrawtransaction.
		if wc.omitRPCOptionsArg {
			var sendAmount uint64
			for _, txOut := range tx.TxOut {
				sendAmount += uint64(txOut.Value)
			}
			var bal float64
			// args: "(dummy)" minconf includeWatchonly inZat
			// Using default inZat = false for compatibility with ZCL.
			if err := wc.call(methodGetBalance, anylist{"", 0, false}, &bal); err != nil {
				return 0, err
			}
			if subtract && sendAmount <= toSatoshi(bal) {
				return 0, errors.New("wallet does not support options")
			}
		}
		return 0, fmt.Errorf("error calculating transaction fee: %w", err)
	}
	return toSatoshi(res.Fees), nil
}

// GetWalletInfo gets the getwalletinfo RPC result.
func (wc *rpcClient) GetWalletInfo() (*GetWalletInfoResult, error) {
	wi := new(GetWalletInfoResult)
	return wi, wc.call(methodGetWalletInfo, nil, wi)
}

// Fingerprint returns an identifier for this wallet. Only HD wallets will have
// an identifier. Descriptor wallets will not.
func (wc *rpcClient) Fingerprint() (string, error) {
	walletInfo, err := wc.GetWalletInfo()
	if err != nil {
		return "", err
	}

	if walletInfo.HdSeedID == "" {
		return "", fmt.Errorf("fingerprint not availble")
	}

	return walletInfo.HdSeedID, nil
}

// GetAddressInfo gets information about the given address by calling
// getaddressinfo RPC command.
func (wc *rpcClient) getAddressInfo(addr btcutil.Address, method string) (*GetAddressInfoResult, error) {
	ai := new(GetAddressInfoResult)
	addrStr, err := wc.stringAddr(addr, wc.chainParams)
	if err != nil {
		return nil, err
	}
	return ai, wc.call(method, anylist{addrStr}, ai)
}

// OwnsAddress indicates if an address belongs to the wallet.
func (wc *rpcClient) OwnsAddress(addr btcutil.Address) (bool, error) {
	method := methodGetAddressInfo
	if wc.legacyValidateAddressRPC {
		method = methodValidateAddress
	}
	ai, err := wc.getAddressInfo(addr, method)
	if err != nil {
		return false, err
	}
	return ai.IsMine, nil
}

// SyncStatus is information about the blockchain sync status.
func (wc *rpcClient) SyncStatus() (*asset.SyncStatus, error) {
	chainInfo, err := wc.getBlockchainInfo()
	if err != nil {
		return nil, fmt.Errorf("getblockchaininfo error: %w", err)
	}
	synced := !chainInfo.Syncing()
	return &asset.SyncStatus{
		Synced:       synced,
		TargetHeight: uint64(chainInfo.Headers),
		Blocks:       uint64(chainInfo.Blocks),
	}, nil
}

// SwapConfirmations gets the number of confirmations for the specified coin ID
// by first checking for a unspent output, and if not found, searching indexed
// wallet transactions.
func (wc *rpcClient) SwapConfirmations(txHash *chainhash.Hash, vout uint32, _ []byte, _ time.Time) (confs uint32, spent bool, err error) {
	// Check for an unspent output.
	txOut, err := wc.getTxOutput(txHash, vout)
	if err == nil && txOut != nil {
		return uint32(txOut.Confirmations), false, nil
	}
	// Check wallet transactions.
	tx, err := wc.GetWalletTransaction(txHash)
	if err != nil {
		if IsTxNotFoundErr(err) {
			return 0, false, asset.CoinNotFoundError
		}
		return 0, false, err
	}
	return uint32(tx.Confirmations), true, nil
}

// getRPCBlockHeader gets the *rpcBlockHeader for the specified block hash.
func (wc *rpcClient) getRPCBlockHeader(blockHash *chainhash.Hash) (*BlockHeader, error) {
	blkHeader := new(BlockHeader)
	err := wc.call(methodGetBlockHeader,
		anylist{blockHash.String(), true}, blkHeader)
	if err != nil {
		return nil, err
	}

	return blkHeader, nil
}

// GetBlockHeader gets the *BlockHeader for the specified block hash. It also
// returns a bool value to indicate whether this block is a part of main chain.
// For orphaned blocks header.Confirmations is negative (typically -1).
func (wc *rpcClient) GetBlockHeader(blockHash *chainhash.Hash) (header *BlockHeader, mainchain bool, err error) {
	hdr, err := wc.getRPCBlockHeader(blockHash)
	if err != nil {
		return nil, false, err
	}
	// RPC wallet must return negative confirmations number for orphaned blocks.
	mainchain = hdr.Confirmations >= 0
	return hdr, mainchain, nil
}

// GetBlockHeight gets the mainchain height for the specified block. Returns
// error for orphaned blocks.
func (wc *rpcClient) GetBlockHeight(blockHash *chainhash.Hash) (int32, error) {
	hdr, _, err := wc.GetBlockHeader(blockHash)
	if err != nil {
		return -1, err
	}
	if hdr.Height < 0 {
		return -1, fmt.Errorf("block is not a mainchain block")
	}
	return int32(hdr.Height), nil
}

func (wc *rpcClient) PeerCount() (uint32, error) {
	var r struct {
		Connections uint32 `json:"connections"`
	}
	err := wc.call(methodGetNetworkInfo, nil, &r)
	if err != nil {
		return 0, err
	}
	return r.Connections, nil
}

// getBlockchainInfo sends the getblockchaininfo request and returns the result.
func (wc *rpcClient) getBlockchainInfo() (*GetBlockchainInfoResult, error) {
	chainInfo := new(GetBlockchainInfoResult)
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
		SubVersion      string `json:"subversion"`
		ProtocolVersion uint64 `json:"protocolversion"`
	}{}
	err := wc.call(methodGetNetworkInfo, nil, r)
	if err != nil {
		return 0, 0, err
	}
	// TODO: We might consider checking getnetworkinfo's "subversion" field,
	// which is something like "/Satoshi:24.0.1/".
	wc.log.Debugf("Node at %v reports subversion \"%v\"", wc.rpcConfig.RPCBind, r.SubVersion)
	return r.Version, r.ProtocolVersion, nil
}

// FindRedemptionsInMempool attempts to find spending info for the specified
// contracts by searching every input of all txs in the mempool.
func (wc *rpcClient) FindRedemptionsInMempool(ctx context.Context, reqs map[OutPoint]*FindRedemptionReq) (discovered map[OutPoint]*FindRedemptionResult) {
	return FindRedemptionsInMempool(ctx, wc.log, reqs, wc.GetRawMempool, wc.GetRawTransaction, wc.segwit, wc.hashTx, wc.chainParams)
}

func FindRedemptionsInMempool(
	ctx context.Context,
	log dex.Logger,
	reqs map[OutPoint]*FindRedemptionReq,
	getMempool func() ([]*chainhash.Hash, error),
	getTx func(txHash *chainhash.Hash) (*wire.MsgTx, error),
	segwit bool,
	hashTx func(*wire.MsgTx) *chainhash.Hash,
	chainParams *chaincfg.Params,

) (discovered map[OutPoint]*FindRedemptionResult) {
	contractsCount := len(reqs)
	log.Debugf("finding redemptions for %d contracts in mempool", contractsCount)

	discovered = make(map[OutPoint]*FindRedemptionResult, len(reqs))

	var totalFound, totalCanceled int
	logAbandon := func(reason string) {
		// Do not remove the contracts from the findRedemptionQueue
		// as they could be subsequently redeemed in some mined tx(s),
		// which would be captured when a new tip is reported.
		if totalFound+totalCanceled > 0 {
			log.Debugf("%d redemptions found, %d canceled out of %d contracts in mempool",
				totalFound, totalCanceled, contractsCount)
		}
		log.Errorf("abandoning mempool redemption search for %d contracts because of %s",
			contractsCount-totalFound-totalCanceled, reason)
	}

	mempoolTxs, err := getMempool()
	if err != nil {
		logAbandon(fmt.Sprintf("error retrieving transactions: %v", err))
		return
	}

	for _, txHash := range mempoolTxs {
		if ctx.Err() != nil {
			return nil
		}
		tx, err := getTx(txHash)
		if err != nil {
			logAbandon(fmt.Sprintf("getrawtransaction error for tx hash %v: %v", txHash, err))
			return
		}
		newlyDiscovered := FindRedemptionsInTxWithHasher(ctx, segwit, reqs, tx, chainParams, hashTx)
		for outPt, res := range newlyDiscovered {
			discovered[outPt] = res
		}

	}
	return
}

// SearchBlockForRedemptions attempts to find spending info for the specified
// contracts by searching every input of all txs in the provided block range.
func (wc *rpcClient) SearchBlockForRedemptions(ctx context.Context, reqs map[OutPoint]*FindRedemptionReq, blockHash chainhash.Hash) (discovered map[OutPoint]*FindRedemptionResult) {
	msgBlock, err := wc.GetBlock(blockHash)
	if err != nil {
		wc.log.Errorf("RPC GetBlock error: %v", err)
		return
	}
	return SearchBlockForRedemptions(ctx, reqs, msgBlock, wc.segwit, wc.hashTx, wc.chainParams)
}

func SearchBlockForRedemptions(
	ctx context.Context,
	reqs map[OutPoint]*FindRedemptionReq,
	msgBlock *wire.MsgBlock,
	segwit bool,
	hashTx func(*wire.MsgTx) *chainhash.Hash,
	chainParams *chaincfg.Params,
) (discovered map[OutPoint]*FindRedemptionResult) {

	discovered = make(map[OutPoint]*FindRedemptionResult, len(reqs))

	for _, msgTx := range msgBlock.Transactions {
		newlyDiscovered := FindRedemptionsInTxWithHasher(ctx, segwit, reqs, msgTx, chainParams, hashTx)
		for outPt, res := range newlyDiscovered {
			discovered[outPt] = res
		}
	}
	return
}

func (wc *rpcClient) AddressUsed(addr string) (bool, error) {
	var recv float64
	const minConf = 0
	if err := wc.call(methodGetReceivedByAddress, []any{addr, minConf}, &recv); err != nil {
		return false, err
	}
	return recv != 0, nil
}

// call is used internally to marshal parameters and send requests to the RPC
// server via (*rpcclient.Client).RawRequest. If thing is non-nil, the result
// will be marshaled into thing.
func (wc *rpcClient) call(method string, args anylist, thing any) error {
	return Call(wc.ctx, wc.requester(), method, args, thing)
}

func Call(ctx context.Context, r RawRequester, method string, args anylist, thing any) error {
	params := make([]json.RawMessage, 0, len(args))
	for i := range args {
		p, err := json.Marshal(args[i])
		if err != nil {
			return err
		}
		params = append(params, p)
	}

	b, err := r.RawRequest(ctx, method, params)
	if err != nil {
		return fmt.Errorf("rawrequest (%v) error: %w", method, err)
	}
	if thing != nil {
		return json.Unmarshal(b, thing)
	}
	return nil
}
