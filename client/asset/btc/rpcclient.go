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
	methodGetBalances        = "getbalances"
	methodGetBalance         = "getbalance"
	methodListUnspent        = "listunspent"
	methodLockUnspent        = "lockunspent"
	methodListLockUnspent    = "listlockunspent"
	methodChangeAddress      = "getrawchangeaddress"
	methodNewAddress         = "getnewaddress"
	methodSignTx             = "signrawtransactionwithwallet"
	methodSignTxLegacy       = "signrawtransaction"
	methodUnlock             = "walletpassphrase"
	methodLock               = "walletlock"
	methodPrivKeyForAddress  = "dumpprivkey"
	methodGetTransaction     = "gettransaction"
	methodSendToAddress      = "sendtoaddress"
	methodSetTxFee           = "settxfee"
	methodGetWalletInfo      = "getwalletinfo"
	methodGetAddressInfo     = "getaddressinfo"
	methodListDescriptors    = "listdescriptors"
	methodValidateAddress    = "validateaddress"
	methodEstimateSmartFee   = "estimatesmartfee"
	methodSendRawTransaction = "sendrawtransaction"
	methodGetTxOut           = "gettxout"
	methodGetBlock           = "getblock"
	methodGetBlockHash       = "getblockhash"
	methodGetBestBlockHash   = "getbestblockhash"
	methodGetRawMempool      = "getrawmempool"
	methodGetRawTransaction  = "getrawtransaction"
	methodGetBlockHeader     = "getblockheader"
	methodGetNetworkInfo     = "getnetworkinfo"
	methodGetBlockchainInfo  = "getblockchaininfo"
)

// isTxNotFoundErr will return true if the error indicates that the requested
// transaction is not known. The error must be dcrjson.RPCError with a numeric
// code equal to btcjson.ErrRPCNoTxInfo. WARNING: This is specific to errors
// from an RPC to a bitcoind (or clone) using dcrd's rpcclient!
func isTxNotFoundErr(err error) bool {
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
	RawRequest(string, []json.RawMessage) (json.RawMessage, error)
}

// RawRequesterWithContext defines decred's rpcclient RawRequest func where all
// RPC requests sent through. For testing, it can be satisfied by a stub.
type RawRequesterWithContext interface {
	RawRequest(context.Context, string, []json.RawMessage) (json.RawMessage, error)
}

// anylist is a list of RPC parameters to be converted to []json.RawMessage and
// sent via RawRequest.
type anylist []interface{}

type rpcCore struct {
	rpcConfig                *RPCConfig
	cloneParams              *BTCCloneCFG
	requesterV               atomic.Value // RawRequesterWithContext
	segwit                   bool
	decodeAddr               dexbtc.AddressDecoder
	stringAddr               dexbtc.AddressStringer
	legacyRawSends           bool
	minNetworkVersion        uint64
	log                      dex.Logger
	chainParams              *chaincfg.Params
	omitAddressType          bool
	legacySignTx             bool
	booleanGetBlock          bool
	unlockSpends             bool
	deserializeTx            func([]byte) (*wire.MsgTx, error)
	serializeTx              func(*wire.MsgTx) ([]byte, error)
	deserializeBlock         func([]byte) (*wire.MsgBlock, error)
	hashTx                   func(*wire.MsgTx) *chainhash.Hash
	numericGetRawTxRPC       bool
	legacyValidateAddressRPC bool
	manualMedianTime         bool
}

func (c *rpcCore) requester() RawRequesterWithContext {
	return c.requesterV.Load().(RawRequesterWithContext)
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

func (wc *rpcClient) connect(ctx context.Context, _ *sync.WaitGroup) error {
	wc.ctx = ctx
	// Check the version. Do it here, so we can also diagnose a bad connection.
	netVer, codeVer, err := wc.getVersion()
	if err != nil {
		return fmt.Errorf("error getting version: %w", err)
	}
	if netVer < wc.minNetworkVersion {
		return fmt.Errorf("reported node version %d is less than minimum %d", netVer, wc.minNetworkVersion)
	}
	// TODO: codeVer is actually asset-dependent. ZCash, for example, is at
	// 170100. So we're just lucking out here, really.
	if codeVer < minProtocolVersion {
		return fmt.Errorf("node software out of date. version %d is less than minimum %d", codeVer, minProtocolVersion)
	}
	wiRes, err := wc.GetWalletInfo()
	if err != nil {
		return fmt.Errorf("getwalletinfo failure: %w", err)
	}
	wc.descriptors = wiRes.Descriptors
	if wc.descriptors {
		if netVer < minDescriptorVersion {
			return fmt.Errorf("reported node version %d is less than minimum %d"+
				" for descriptor wallets", netVer, minDescriptorVersion)
		}
		wc.log.Debug("Using a descriptor wallet.")
	}
	return nil
}

// reconfigure attempts to reconfigure the rpcClient for the new settings. Live
// reconfiguration is only attempted if the new wallet type is walletTypeRPC. If
// the special_activelyUsed flag is set, reconfigure will fail if we can't
// validate ownership of the current deposit address.
func (wc *rpcClient) reconfigure(cfg *asset.WalletConfig, currentAddress string) (restartRequired bool, err error) {
	// rpcClient only handles walletTypeRPC.
	if cfg.Type != walletTypeRPC {
		restartRequired = true
		return
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

		// If the wallet is in active use, check the supplied address.
		if parsedCfg.ActivelyUsed {
			// We can't use wc.ownsAddress because the rpcClient still has the
			// old requester stored, so we'll call directly.
			method := methodGetAddressInfo
			if wc.legacyValidateAddressRPC {
				method = methodValidateAddress
			}

			ai := new(GetAddressInfoResult)
			if err := call(wc.ctx, cl, method, anylist{currentAddress}, ai); err != nil {
				return false, fmt.Errorf("error getting address info with new RPC credentials: %w", err)
			} else if !ai.IsMine {
				return false, errors.New("cannot reconfigure to a new RPC wallet during active use")
			}
		}

		wc.requesterV.Store(cl)
		wc.rpcConfig = newCfg

		// No restart required
	}
	return
}

// RawRequest passes the request to the wallet's RawRequester.
func (wc *rpcClient) RawRequest(method string, params []json.RawMessage) (json.RawMessage, error) {
	return wc.requester().RawRequest(wc.ctx, method, params)
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

	// TODO: lockUnspent should really just take a []*outPoint, since it doesn't
	// need the value.
	ops := make([]*output, 0, len(tx.TxIn))
	for _, txIn := range tx.TxIn {
		prevOut := &txIn.PreviousOutPoint
		ops = append(ops, &output{pt: newOutPoint(&prevOut.Hash, prevOut.Index)})
	}
	if err := wc.lockUnspent(true, ops); err != nil {
		wc.log.Warnf("error unlocking spent outputs: %v", err)
	}
	return txHash, nil
}

// getTxOut returns the transaction output info if it's unspent and
// nil, otherwise.
func (wc *rpcClient) getTxOut(txHash *chainhash.Hash, index uint32, _ []byte, _ time.Time) (*wire.TxOut, uint32, error) {
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
func (wc *rpcClient) getBestBlockHeader() (*blockHeader, error) {
	tipHash, err := wc.getBestBlockHash()
	if err != nil {
		return nil, err
	}
	return wc.getBlockHeader(tipHash)
}

// getBestBlockHeight returns the height of the top mainchain block.
func (wc *rpcClient) getBestBlockHeight() (int32, error) {
	header, err := wc.getBestBlockHeader()
	if err != nil {
		return -1, err
	}
	return int32(header.Height), nil
}

// getChainStamp satisfies chainStamper for manual median time calculations.
func (wc *rpcClient) getChainStamp(blockHash *chainhash.Hash) (stamp time.Time, prevHash *chainhash.Hash, err error) {
	hdr, err := wc.getBlockHeader(blockHash)
	if err != nil {
		return
	}
	prevHash, err = chainhash.NewHashFromStr(hdr.PreviousBlockHash)
	if err != nil {
		return
	}
	return time.Unix(hdr.Time, 0).UTC(), prevHash, nil
}

// medianTime is the median time for the current best block.
func (wc *rpcClient) medianTime() (stamp time.Time, err error) {
	tipHash, err := wc.getBestBlockHash()
	if err != nil {
		return
	}
	if wc.manualMedianTime {
		return calcMedianTime(wc, tipHash)
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

func (wc *rpcClient) externalAddress() (btcutil.Address, error) {
	if wc.segwit {
		return wc.address("bech32")
	}
	return wc.address("legacy")
}

func (wc *rpcClient) refundAddress() (btcutil.Address, error) {
	return wc.externalAddress()
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

// signTx attempts to have the wallet sign the transaction inputs.
func (wc *rpcClient) signTx(inTx *wire.MsgTx) (*wire.MsgTx, error) {
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

// privKeyForAddress retrieves the private key associated with the specified
// address.
func (wc *rpcClient) privKeyForAddress(addr string) (*btcec.PrivateKey, error) {
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

// getWalletTransaction retrieves the JSON-RPC gettransaction result.
func (wc *rpcClient) getWalletTransaction(txHash *chainhash.Hash) (*GetTransactionResult, error) {
	tx := new(GetTransactionResult)
	err := wc.call(methodGetTransaction, anylist{txHash.String()}, tx)
	if err != nil {
		if isTxNotFoundErr(err) {
			return nil, asset.CoinNotFoundError
		}
		return nil, err
	}
	return tx, nil
}

// walletUnlock unlocks the wallet.
func (wc *rpcClient) walletUnlock(pw []byte) error {
	// 100000000 comes from bitcoin-cli help walletpassphrase
	return wc.call(methodUnlock, anylist{string(pw), 100000000}, nil)
}

// walletLock locks the wallet.
func (wc *rpcClient) walletLock() error {
	return wc.call(methodLock, nil, nil)
}

// sendToAddress sends the amount to the address. feeRate is in units of
// sats/byte.
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
func (wc *rpcClient) getAddressInfo(addr btcutil.Address, method string) (*GetAddressInfoResult, error) {
	ai := new(GetAddressInfoResult)
	addrStr, err := wc.stringAddr(addr, wc.chainParams)
	if err != nil {
		return nil, err
	}
	return ai, wc.call(method, anylist{addrStr}, ai)
}

// ownsAddress indicates if an address belongs to the wallet.
func (wc *rpcClient) ownsAddress(addr btcutil.Address) (bool, error) {
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
		Syncing: chainInfo.syncing(),
	}, nil
}

// swapConfirmations gets the number of confirmations for the specified coin ID
// by first checking for a unspent output, and if not found, searching indexed
// wallet transactions.
func (wc *rpcClient) swapConfirmations(txHash *chainhash.Hash, vout uint32, _ []byte, _ time.Time) (confs uint32, spent bool, err error) {
	// Check for an unspent output.
	txOut, err := wc.getTxOutput(txHash, vout)
	if err == nil && txOut != nil {
		return uint32(txOut.Confirmations), false, nil
	}
	// Check wallet transactions.
	tx, err := wc.getWalletTransaction(txHash)
	if err != nil {
		if isTxNotFoundErr(err) {
			return 0, false, asset.CoinNotFoundError
		}
		return 0, false, err
	}
	return uint32(tx.Confirmations), true, nil
}

// rpcBlockHeader adds a MedianTime field to blockHeader.
type rpcBlockHeader struct {
	blockHeader
	MedianTime int64 `json:"mediantime"`
}

// getBlockHeader gets the *rpcBlockHeader for the specified block hash.
func (wc *rpcClient) getRPCBlockHeader(blockHash *chainhash.Hash) (*rpcBlockHeader, error) {
	blkHeader := new(rpcBlockHeader)
	err := wc.call(methodGetBlockHeader,
		anylist{blockHash.String(), true}, blkHeader)
	if err != nil {
		return nil, err
	}

	return blkHeader, nil
}

// getBlockHeader gets the *blockHeader for the specified block hash.
func (wc *rpcClient) getBlockHeader(blockHash *chainhash.Hash) (*blockHeader, error) {
	hdr, err := wc.getRPCBlockHeader(blockHash)
	if err != nil {
		return nil, err
	}
	return &hdr.blockHeader, nil
}

// getBlockHeight gets the mainchain height for the specified block.
func (wc *rpcClient) getBlockHeight(blockHash *chainhash.Hash) (int32, error) {
	hdr, err := wc.getBlockHeader(blockHash)
	if err != nil {
		return -1, err
	}
	if hdr.Height < 0 {
		return -1, fmt.Errorf("block is not a mainchain block")
	}
	return int32(hdr.Height), nil
}

func (wc *rpcClient) peerCount() (uint32, error) {
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
		newlyDiscovered := findRedemptionsInTxWithHasher(ctx, wc.segwit, reqs, tx, wc.chainParams, wc.hashTx)
		for outPt, res := range newlyDiscovered {
			discovered[outPt] = res
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
		newlyDiscovered := findRedemptionsInTxWithHasher(ctx, wc.segwit, reqs, msgTx, wc.chainParams, wc.hashTx)
		for outPt, res := range newlyDiscovered {
			discovered[outPt] = res
		}
	}
	return
}

// call is used internally to marshal parameters and send requests to the RPC
// server via (*rpcclient.Client).RawRequest. If thing is non-nil, the result
// will be marshaled into thing.
func (wc *rpcClient) call(method string, args anylist, thing interface{}) error {
	return call(wc.ctx, wc.requester(), method, args, thing)
}

func call(ctx context.Context, r RawRequesterWithContext, method string, args anylist, thing interface{}) error {
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
