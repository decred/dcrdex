// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package electrum

import (
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
)

const (
	methodGetInfo           = "getinfo"
	methodGetFeeRate        = "getfeerate"
	methodCreateNewAddress  = "createnewaddress" // beyond gap limit
	methodGetUnusedAddress  = "getunusedaddress"
	methodGetAddressHistory = "getaddresshistory"
	methodGetAddressUnspent = "getaddressunspent"
	methodGetTransaction    = "gettransaction"
	methodListUnspent       = "listunspent"
	methodGetPrivateKeys    = "getprivatekeys"
	methodPayTo             = "payto"
	methodBroadcast         = "broadcast"
	methodGetTxStatus       = "get_tx_status" // only wallet txns
	methodgetBalance        = "getbalance"
	methodIsMine            = "ismine"
	methodValidateAddress   = "validateaddress"
	methodSignTransaction   = "signtransaction"
	methodFreeze            = "freeze" // freezes all utxos for an address, freeze_utxo not avail in 4.0.9
	methodUnfreeze          = "unfreeze"
)

// GetInfo gets basic Electrum wallet info.
func (ec *Client) GetInfo() (*GetInfoResult, error) {
	var res GetInfoResult
	err := ec.Call(methodGetInfo, nil, &res)
	if err != nil {
		return nil, err
	}
	return &res, nil
}

type feeRateReq struct {
	Method string  `json:"fee_method"`
	Level  float64 `json:"fee_level"`
}

// FeeRate gets a fee rate estimate for a block confirmation target, where 1
// indicates the next block.
func (ec *Client) FeeRate(confTarget int64) (int64, error) {
	if confTarget > 10 {
		confTarget = 10
	} else if confTarget < 1 {
		confTarget = 1
	}
	target := float64(10-confTarget+1) / 20.0 // [0.5, 1.0]
	var satPerKB int64
	err := ec.Call(methodGetFeeRate, feeRateReq{"eta", target}, &satPerKB) // or anylist{"eta", target}
	if err != nil {
		return 0, err
	}
	return satPerKB, nil
}

// CreateNewAddress generates a new address, igoring the gap limit. NOTE: There
// is no method to retrieve a change address.
func (ec *Client) CreateNewAddress() (string, error) {
	var res string
	err := ec.Call(methodCreateNewAddress, nil, &res)
	if err != nil {
		return "", err
	}
	return res, nil
}

// GetUnusedAddress gets the next unused address from the wallet. It may have
// already been requested.
func (ec *Client) GetUnusedAddress() (string, error) {
	var res string
	err := ec.Call(methodGetUnusedAddress, nil, &res)
	if err != nil {
		return "", err
	}
	return res, nil
}

// CheckAddress validates the address and reports if it belongs to the wallet.
func (ec *Client) CheckAddress(addr string) (valid, mine bool, err error) {
	err = ec.Call(methodIsMine, positional{addr}, &mine)
	if err != nil {
		return
	}
	err = ec.Call(methodValidateAddress, positional{addr}, &valid)
	if err != nil {
		return
	}
	return
}

// GetAddressHistory returns the history an address. Confirmed transactions will
// have a nil Fee field, while unconfirmed transactions will have a Fee and a
// value of zero for Height.
func (ec *Client) GetAddressHistory(addr string) ([]*GetAddressHistoryResult, error) {
	var res []*GetAddressHistoryResult
	err := ec.Call(methodGetAddressHistory, positional{addr}, &res)
	if err != nil {
		return nil, err
	}
	return res, nil
}

// GetAddressUnspent returns the unspent outputs for an address. Unconfirmed
// outputs will have a value of zero for Height.
func (ec *Client) GetAddressUnspent(addr string) ([]*GetAddressUnspentResult, error) {
	var res []*GetAddressUnspentResult
	err := ec.Call(methodGetAddressUnspent, positional{addr}, &res)
	if err != nil {
		return nil, err
	}
	return res, nil
}

// FreezeAddress freezes/locks all UTXOs paying to the address.
func (ec *Client) FreezeAddress(addr string) (bool, error) {
	var res bool
	err := ec.Call(methodFreeze, positional{addr}, &res)
	if err != nil {
		return false, err
	}
	return res, nil
}

// UnfreezeAddress unfreezes/unlocks all UTXOs paying to the address.
func (ec *Client) UnfreezeAddress(addr string) (bool, error) {
	var res bool
	err := ec.Call(methodUnfreeze, positional{addr}, &res)
	if err != nil {
		return false, err
	}
	return res, nil
}

// GetRawTransaction retrieves the serialized transaction identified by txid.
func (ec *Client) GetRawTransaction(txid string) ([]byte, error) {
	var res string
	err := ec.Call(methodGetTransaction, positional{txid}, &res)
	if err != nil {
		return nil, err
	}
	tx, err := hex.DecodeString(res)
	if err != nil {
		return nil, err
	}
	return tx, nil
}

// GetWalletTxConfs will get the confirmations on the wallet-related
// transaction. This function will error if it is either not a wallet
// transaction or not known to the wallet.
func (ec *Client) GetWalletTxConfs(txid string) (int, error) {
	var res struct {
		Confs int `json:"confirmations"`
	}
	err := ec.Call(methodGetTxStatus, positional{txid}, &res)
	if err != nil {
		return 0, err
	}
	return res.Confs, nil
}

// ListUnspent returns details on all unspent outputs for the wallet. Note that
// the pkScript is not included, and the user would have to retrieve it with
// GetRawTransaction for PrevOutHash if the output is of interest.
func (ec *Client) ListUnspent() ([]*ListUnspentResult, error) {
	var res []*ListUnspentResult
	err := ec.Call(methodListUnspent, nil, &res)
	if err != nil {
		return nil, err
	}
	return res, nil
}

func (ec *Client) GetBalance() (*Balance, error) {
	var res struct {
		Confirmed   floatString `json:"confirmed"`
		Unconfirmed floatString `json:"unconfirmed"`
		Immature    floatString `json:"unmatured"` // yes, unmatured!
	}
	err := ec.Call(methodgetBalance, nil, &res)
	if err != nil {
		return nil, err
	}
	return &Balance{
		Confirmed:   float64(res.Confirmed),
		Unconfirmed: float64(res.Unconfirmed),
		Immature:    float64(res.Immature),
	}, nil
}

// payto(self, destination, amount, fee=None, feerate=None, from_addr=None, from_coins=None, change_addr=None,
// nocheck=False, unsigned=False, rbf=None, password=None, locktime=None, addtransaction=False, wallet: Abstract_Wallet = None):
type paytoReq struct {
	Addr   string `json:"destination"`
	Amount string `json:"amount"` // BTC, or "!" for max
	// Fee always omitted, only use FeeRate
	FeeRate    *float64 `json:"feerate,omitempty"` // sat/vB, gets multiplied by 1000 for extra precision, omit for high prio
	ChangeAddr string   `json:"change_addr,omitempty"`
	// FromAddr omitted
	FromUTXOs      string `json:"from_coins,omitempty"`
	NoCheck        bool   `json:"nocheck"`
	Unsigned       bool   `json:"unsigned"` // unsigned returns a base64 psbt thing
	RBF            bool   `json:"rbf"`      // default to false
	Password       string `json:"password"`
	LockTime       *int64 `json:"locktime,omitempty"`
	AddTransaction bool   `json:"addtransaction"`
}

func (ec *Client) PayTo(walletPass string, addr string, amtBTC float64, feeRate float64) ([]byte, error) {
	if feeRate < 1 {
		return nil, errors.New("fee rate in sat/vB too low")
	}
	amt := strconv.FormatFloat(amtBTC, 'f', 8, 64)
	var res string
	err := ec.Call(methodPayTo, &paytoReq{
		Addr:     addr,
		Amount:   amt,
		FeeRate:  &feeRate,
		Password: walletPass,
	}, &res)
	if err != nil {
		return nil, err
	}
	txRaw, err := hex.DecodeString(res)
	if err != nil {
		return nil, err
	}
	return txRaw, nil
}

type signTransactionArgs struct {
	Tx   string `json:"tx"`
	Pass string `json:"password"`
	// 4.0.9 has privkey in this request, but 4.2 does not since it has a
	// signtransaction_with_privkey request.
	// Privkey string `json:"privkey,omitempty"` // sign with wallet if empty
}

func (ec *Client) SignTx(walletPass string, tx []byte) ([]byte, error) {
	txStr := hex.EncodeToString(tx)
	var res string
	err := ec.Call(methodSignTransaction, &signTransactionArgs{txStr, walletPass}, &res)
	if err != nil {
		return nil, err
	}
	txRaw, err := hex.DecodeString(res)
	if err != nil {
		return nil, err
	}
	return txRaw, nil
}

func (ec *Client) Broadcast(tx []byte) (string, error) {
	txStr := hex.EncodeToString(tx)
	var res string
	err := ec.Call(methodBroadcast, positional{txStr}, &res)
	if err != nil {
		return "", err
	}
	return res, nil
}

type getPrivKeyArgs struct {
	Addr string `json:"address"`
	Pass string `json:"password"`
}

func (ec *Client) GetPrivateKeys(walletPass, addr string) (string, error) {
	var res string
	err := ec.Call(methodGetPrivateKeys, &getPrivKeyArgs{addr, walletPass}, &res)
	if err != nil {
		return "", err
	}
	privSpit := strings.Split(res, ":")
	if len(privSpit) != 2 {
		return "", errors.New("bad key")
	}
	return privSpit[1], nil
}
