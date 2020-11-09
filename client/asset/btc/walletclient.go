// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package btc

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/btcsuite/btcd/btcec"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
	"github.com/btcsuite/btcutil"
)

const (
	methodGetBalances       = "getbalances"
	methodListUnspent       = "listunspent"
	methodLockUnspent       = "lockunspent"
	methodListLockUnspent   = "listlockunspent"
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
	methodGetWalletInfo     = "getwalletinfo"
)

// walletClient is a bitcoind wallet RPC client that uses rpcclient.Client's
// RawRequest for wallet-related calls.
type walletClient struct {
	node        rpcClient
	chainParams *chaincfg.Params
	segwit      bool
}

// newWalletClient is the constructor for a walletClient.
func newWalletClient(node rpcClient, segwit bool, chainParams *chaincfg.Params) *walletClient {
	return &walletClient{
		node:        node,
		chainParams: chainParams,
		segwit:      segwit,
	}
}

// anylist is a list of RPC parameters to be converted to []json.RawMessage and
// sent via RawRequest.
type anylist []interface{}

// Balances retrieves a wallet's balance details.
func (wc *walletClient) Balances() (*GetBalancesResult, error) {
	var balances GetBalancesResult
	return &balances, wc.call(methodGetBalances, nil, &balances)
}

// ListUnspent retrieves a list of the wallet's UTXOs.
func (wc *walletClient) ListUnspent() ([]*ListUnspentResult, error) {
	unspents := make([]*ListUnspentResult, 0)
	// TODO: listunspent 0 9999999 []string{}, include_unsafe=false
	return unspents, wc.call(methodListUnspent, anylist{uint8(0)}, &unspents)
}

// LockUnspent locks and unlocks outputs for spending. An output that is part of
// an order, but not yet spent, should be locked until spent or until the order
// is  canceled or fails.
func (wc *walletClient) LockUnspent(unlock bool, ops []*output) error {
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
func (wc *walletClient) ListLockUnspent() ([]*RPCOutpoint, error) {
	var unspents []*RPCOutpoint
	err := wc.call(methodListLockUnspent, nil, &unspents)
	return unspents, err
}

// ChangeAddress gets a new internal address from the wallet. The address will
// be bech32-encoded (P2WPKH).
func (wc *walletClient) ChangeAddress() (btcutil.Address, error) {
	var addrStr string
	var err error
	if wc.segwit {
		err = wc.call(methodChangeAddress, anylist{"bech32"}, &addrStr)
	} else {
		err = wc.call(methodChangeAddress, anylist{"legacy"}, &addrStr)
	}
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
func (wc *walletClient) Unlock(pass string) error {
	// 100000000 comes from bitcoin-cli help walletpassphrase
	return wc.call(methodUnlock, anylist{pass, 100000000}, nil)
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
func (wc *walletClient) GetWalletInfo() (*GetWalletInfoResult, error) {
	wi := new(GetWalletInfoResult)
	return wi, wc.call(methodGetWalletInfo, nil, wi)
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
