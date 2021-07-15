// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package btc

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"

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
	methodGetBlockHash       = "getblockhash"
	methodGetBestBlockHash   = "getbestblockhash"
	methodGetRawMempool      = "getrawmempool"
	methodGetRawTransaction  = "getrawtransaction"
)

// RawRequester is for sending context-aware RPC requests, and has methods for
// shutting down the underlying connection.  For testing, it can be satisfied
// by a stub. The returned error should be of type dcrjson.RPCError if non-nil.
type RawRequester interface {
	RawRequest(context.Context, string, []json.RawMessage) (json.RawMessage, error)
}

// rpcClient is a bitcoind wallet RPC client that uses rpcclient.Client's
// RawRequest for wallet-related calls.
type rpcClient struct {
	ctx         context.Context
	requester   RawRequester
	chainParams *chaincfg.Params
	segwit      bool
	decodeAddr  dexbtc.AddressDecoder
	// arglessChangeAddrRPC true will pass no arguments to the
	// getrawchangeaddress RPC.
	arglessChangeAddrRPC bool
}

// newWalletClient is the constructor for a walletClient.
func newWalletClient(requester RawRequester, segwit bool, addrDecoder dexbtc.AddressDecoder, arglessChangeAddrRPC bool, chainParams *chaincfg.Params) *rpcClient {
	if addrDecoder == nil {
		addrDecoder = btcutil.DecodeAddress
	}

	return &rpcClient{
		requester:            requester,
		chainParams:          chainParams,
		segwit:               segwit,
		decodeAddr:           addrDecoder,
		arglessChangeAddrRPC: arglessChangeAddrRPC,
	}
}

// anylist is a list of RPC parameters to be converted to []json.RawMessage and
// sent via RawRequest.
type anylist []interface{}

// EstimateSmartFee requests the server to estimate a fee level.
func (wc *rpcClient) EstimateSmartFee(confTarget int64, mode *btcjson.EstimateSmartFeeMode) (*btcjson.EstimateSmartFeeResult, error) {
	res := new(btcjson.EstimateSmartFeeResult)
	return res, wc.call(methodEstimateSmartFee, anylist{confTarget, mode}, res)
}

// SendRawTransaction submits the encoded transaction to the server which will
// then relay it to the network.
func (wc *rpcClient) SendRawTransaction(tx *wire.MsgTx, allowHighFees bool) (*chainhash.Hash, error) {
	txBytes, err := serializeMsgTx(tx)
	if err != nil {
		return nil, err
	}
	return wc.callHashGetter(methodSendRawTransaction, anylist{
		hex.EncodeToString(txBytes), allowHighFees})
}

// GetTxOut returns the transaction output info if it's unspent and
// nil, otherwise.
func (wc *rpcClient) GetTxOut(txHash *chainhash.Hash, index uint32, mempool bool) (*btcjson.GetTxOutResult, error) {
	// Note that we pass to call pointer to a pointer (&res) so that
	// json.Unmarshal can nil the pointer if the method returns the JSON null.
	var res *btcjson.GetTxOutResult
	return res, wc.call(methodGetTxOut, anylist{txHash.String(), index, mempool},
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

// GetBlockHash returns the hash of the block in the best block chain at the
// given height.
func (wc *rpcClient) GetBlockHash(blockHeight int64) (*chainhash.Hash, error) {
	return wc.callHashGetter(methodGetBlockHash, anylist{blockHeight})
}

// GetBestBlockHash returns the hash of the best block in the longest block
// chain.
func (wc *rpcClient) GetBestBlockHash() (*chainhash.Hash, error) {
	return wc.callHashGetter(methodGetBestBlockHash, nil)
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

// GetRawTransactionVerbose retrieves the verbose tx information.
func (wc *rpcClient) GetRawTransactionVerbose(txHash *chainhash.Hash) (*btcjson.TxRawResult, error) {
	res := new(btcjson.TxRawResult)
	return res, wc.call(methodGetRawTransaction, anylist{txHash.String(),
		true}, res)
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
// is  canceled or fails.
func (wc *rpcClient) LockUnspent(unlock bool, ops []*output) error {
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

// ListLockUnspent returns a slice of outpoints for all unspent outputs marked
// as locked by a wallet.
func (wc *rpcClient) ListLockUnspent() ([]*RPCOutpoint, error) {
	var unspents []*RPCOutpoint
	err := wc.call(methodListLockUnspent, nil, &unspents)
	return unspents, err
}

// ChangeAddress gets a new internal address from the wallet. The address will
// be bech32-encoded (P2WPKH).
func (wc *rpcClient) ChangeAddress() (btcutil.Address, error) {
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
func (wc *rpcClient) AddressPKH() (btcutil.Address, error) {
	return wc.address("legacy")
}

// AddressWPKH gets a new bech32-encoded (P2WPKH) external address from the
// wallet.
func (wc *rpcClient) AddressWPKH() (btcutil.Address, error) {
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

// SignTx attempts to have the wallet sign the transaction inputs.
func (wc *rpcClient) SignTx(inTx *wire.MsgTx) (*wire.MsgTx, error) {
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

// PrivKeyForAddress retrieves the private key associated with the specified
// address.
func (wc *rpcClient) PrivKeyForAddress(addr string) (*btcec.PrivateKey, error) {
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

// GetTransaction retrieves the specified wallet-related transaction.
func (wc *rpcClient) GetTransaction(txid string) (*GetTransactionResult, error) {
	res := new(GetTransactionResult)
	err := wc.call(methodGetTransaction, anylist{txid}, res)
	if err != nil {
		return nil, err
	}
	return res, nil
}

// WalletUnlock unlocks the wallet.
func (wc *rpcClient) WalletUnlock(pass string) error {
	// 100000000 comes from bitcoin-cli help walletpassphrase
	return wc.call(methodUnlock, anylist{pass, 100000000}, nil)
}

// WalletLock locks the wallet.
func (wc *rpcClient) WalletLock() error {
	return wc.call(methodLock, nil, nil)
}

// SendToAddress sends the amount to the address. feeRate is in units of
// atoms/byte.
func (wc *rpcClient) SendToAddress(address string, value, feeRate uint64, subtract bool) (*chainhash.Hash, error) {
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
func (wc *rpcClient) GetAddressInfo(address string) (*GetAddressInfoResult, error) {
	ai := new(GetAddressInfoResult)
	return ai, wc.call(methodGetAddressInfo, anylist{address}, ai)
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
