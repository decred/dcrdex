// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package btc

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"decred.org/dcrdex/dex"
	"github.com/btcsuite/btcd/btcec"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
	"github.com/btcsuite/btcutil"
)

const (
	methodListUnspent       = "listunspent"
	methodLockUnspent       = "lockunspent"
	methodChangeAddress     = "getrawchangeaddress"
	methodNewAddress        = "getnewaddress"
	methodSignTx            = "signrawtransactionwithwallet"
	methodUnlock            = "walletpassphrase"
	methodLock              = "walletlock"
	methodPrivKeyForAddress = "dumpprivkey"
	methodSignMessage       = "signmessagewithprivkey"
	methodGetTransaction    = "gettransaction"
	methodSendToAddress     = "sendtoaddress"
	methodSetTxFee          = "settxfee"
)

// walletClient is a bitcoind wallet RPC client that uses rpcclient.Client's
// RawRequest for wallet-related calls.
type walletClient struct {
	node        rpcClient
	chainParams *chaincfg.Params
}

// newWalletClient is the constructor for a walletClient.
func newWalletClient(node rpcClient, chainParams *chaincfg.Params) *walletClient {
	return &walletClient{
		node:        node,
		chainParams: chainParams,
	}
}

// anylist is a list of RPC parameters to be converted to []json.RawMessage and
// sent via RawRequest.
type anylist []interface{}

// ListUnspent retrieves a list of the wallet's UTXOs.
func (wc *walletClient) ListUnspent() ([]*ListUnspentResult, error) {
	unspents := make([]*ListUnspentResult, 0)
	return unspents, wc.call(methodListUnspent, anylist{uint8(0)}, &unspents)
}

// LockUnspent locks and unlocks outputs for spending. An output that is part of
// an order, but not yet spent, should be locked until spent or until the order
// is  canceled or fails.
func (wc *walletClient) LockUnspent(unlock bool, ops []*output) error {
	rpcops := make([]*RPCOutpoint, 0, len(ops))
	for _, op := range ops {
		rpcops = append(rpcops, &RPCOutpoint{
			TxID: op.txHash.String(),
			Vout: op.vout,
		})
	}
	var success bool
	err := wc.call(methodLockUnspent, anylist{unlock, rpcops}, &success)
	if err == nil && !success {
		return fmt.Errorf("lockunspent unsuccessful")
	}
	return err
}

// ChangeAddress gets a new internal address from the wallet. The address will
// be bech32-encoded (P2WPKH).
func (wc *walletClient) ChangeAddress() (btcutil.Address, error) {
	var addrStr string
	err := wc.call(methodChangeAddress, anylist{"bech32"}, &addrStr)
	if err != nil {
		return nil, err
	}
	return btcutil.DecodeAddress(addrStr, wc.chainParams)
}

// AddressPKH gets a new base58-encoded (P2PKH) external address from the
// wallet.
func (wc *walletClient) AddressPKH() (btcutil.Address, error) {
	return wc.address("legacy")
}

// AddressWPKH gets a new bech32-encoded (P2WPKH) external address from the
// wallet.
func (wc *walletClient) AddressWPKH() (btcutil.Address, error) {
	return wc.address("bech32")
}

// address is used internally for fetching addresses of various types from the
// wallet.
func (wc *walletClient) address(aType string) (btcutil.Address, error) {
	var addrStr string
	err := wc.call(methodNewAddress, anylist{"", aType}, &addrStr)
	if err != nil {
		return nil, err
	}
	return btcutil.DecodeAddress(addrStr, wc.chainParams)
}

// SignTx attempts to have the wallet sign the transaction inputs.
func (wc *walletClient) SignTx(inTx *wire.MsgTx) (*wire.MsgTx, error) {
	txBytes, err := serializeMsgTx(inTx)
	if err != nil {
		return nil, fmt.Errorf("tx serialization error: %v", err)
	}
	res := new(SignTxResult)
	err = wc.call(methodSignTx, anylist{hex.EncodeToString(txBytes)}, res)
	if err != nil {
		return nil, fmt.Errorf("tx signing error: %v", err)
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
	outTx, err := msgTxFromHex(res.Hex)
	if err != nil {
		return nil, fmt.Errorf("error deserializing transaction response: %v", err)
	}
	return outTx, nil
}

// PrivKeyForAddress retrieves the private key associated with the specified
// address.
func (wc *walletClient) PrivKeyForAddress(addr string) (*btcec.PrivateKey, error) {
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
func (wc *walletClient) GetTransaction(txid string) (*GetTransactionResult, error) {
	res := new(GetTransactionResult)
	err := wc.call(methodGetTransaction, anylist{txid}, res)
	if err != nil {
		return nil, err
	}
	return res, nil
}

// Unlock unlocks the wallet.
func (wc *walletClient) Unlock(pass string, dur time.Duration) error {
	return wc.call(methodUnlock, anylist{pass, dur / time.Second}, nil)
}

// Lock locks the wallet.
func (wc *walletClient) Lock() error {
	return wc.call(methodLock, nil, nil)
}

// SendToAddress sends the amount to the address. feeRate is in units of
// atoms/byte.
func (wc *walletClient) SendToAddress(address string, value, feeRate uint64, subtract bool) (*chainhash.Hash, error) {
	var success bool
	// 1e-5 = 1e-8 for satoshis * 1000 for kB.
	err := wc.call(methodSetTxFee, anylist{float64(feeRate) / 1e5}, &success)
	if err != nil {
		return nil, fmt.Errorf("error setting transaction fee: %v", err)
	}
	if !success {
		return nil, fmt.Errorf("failed to set transaction fee")
	}
	var txid string
	// Last boolean argument is to subtract the fee from the amount.
	err = wc.call(methodSendToAddress, anylist{address, float64(value) / 1e8, "dcrdex", "", subtract}, &txid)
	if err != nil {
		return nil, err
	}
	return chainhash.NewHashFromStr(txid)
}

// call is used internally to  marshal parmeters and send requests to  the RPC
// server via (*rpcclient.Client).RawRequest. If `thing` is non-nil, the result
// will be marshaled into `thing`.
func (wc *walletClient) call(method string, args anylist, thing interface{}) error {
	params := make([]json.RawMessage, 0, len(args))
	for i := range args {
		p, err := json.Marshal(args[i])
		if err != nil {
			return err
		}
		params = append(params, p)
	}
	b, err := wc.node.RawRequest(method, params)
	if err != nil {
		return fmt.Errorf("rawrequest error: %v", err)
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

// MsgTxFromHex creates a wire.MsgTx by deserializing the hex transaction.
func msgTxFromHex(txHex dex.Bytes) (*wire.MsgTx, error) {
	msgTx := wire.NewMsgTx(wire.TxVersion)
	if err := msgTx.Deserialize(bytes.NewReader(txHex)); err != nil {
		return nil, err
	}
	return msgTx, nil
}
