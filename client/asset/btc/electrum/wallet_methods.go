// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package electrum

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const (
	// Wallet-agnostic commands
	methodCommands          = "commands" // list of supported methods
	methodGetInfo           = "getinfo"
	methodGetServers        = "getservers"
	methodGetFeeRate        = "getfeerate"
	methodGetAddressHistory = "getaddresshistory"
	methodGetAddressUnspent = "getaddressunspent"
	methodBroadcast         = "broadcast"
	methodValidateAddress   = "validateaddress"

	// Wallet-specific commands
	methodCreateNewAddress = "createnewaddress" // beyond gap limit, makes recovery difficult
	methodGetUnusedAddress = "getunusedaddress"
	methodGetTransaction   = "gettransaction"
	methodListUnspent      = "listunspent"
	methodGetPrivateKeys   = "getprivatekeys" // requires password for protected wallets
	methodPayTo            = "payto"          // requires password for protected wallets
	methodAddLocalTx       = "addtransaction"
	methodRemoveLocalTx    = "removelocaltx"
	methodGetTxStatus      = "get_tx_status" // only wallet txns
	methodGetBalance       = "getbalance"
	methodIsMine           = "ismine"
	methodSignTransaction  = "signtransaction" // requires password for protected wallets
	methodFreezeUTXO       = "freeze_utxo"
	methodUnfreezeUTXO     = "unfreeze_utxo"
	methodOnchainHistory   = "onchain_history"
	methodVersion          = "version"
)

// Commands gets a list of the supported wallet RPCs.
func (wc *WalletClient) Commands(ctx context.Context) ([]string, error) {
	var res string
	err := wc.Call(ctx, methodCommands, nil, &res)
	if err != nil {
		return nil, err
	}
	return strings.Split(res, " "), nil
}

// GetInfo gets basic Electrum wallet info.
func (wc *WalletClient) GetInfo(ctx context.Context) (*GetInfoResult, error) {
	var res GetInfoResult
	err := wc.Call(ctx, methodGetInfo, nil, &res)
	if err != nil {
		return nil, err
	}
	return &res, nil
}

// GetServers gets the electrum servers known to the wallet. These are the
// possible servers to which Electrum may connect. This includes the currently
// connected server named in the GetInfo result.
func (wc *WalletClient) GetServers(ctx context.Context) ([]*GetServersResult, error) {
	type getServersResult struct {
		Pruning string `json:"pruning"` // oldest block or "-" for no pruning
		SSL     string `json:"s"`       // port, as a string for some reason
		TCP     string `json:"t"`
		Version string `json:"version"` // e.g. "1.4.2"
	}
	var res map[string]*getServersResult
	err := wc.Call(ctx, methodGetServers, nil, &res)
	if err != nil {
		return nil, err
	}

	servers := make([]*GetServersResult, 0, len(res))
	for host, info := range res {
		var ssl, tcp uint16
		if info.SSL != "" {
			sslP, err := strconv.ParseUint(info.SSL, 10, 16)
			if err == nil {
				ssl = uint16(sslP)
			} else {
				fmt.Println(err)
			}
		}
		if info.TCP != "" {
			tcpP, err := strconv.ParseUint(info.TCP, 10, 16)
			if err == nil {
				tcp = uint16(tcpP)
			} else {
				fmt.Println(err)
			}
		}
		servers = append(servers, &GetServersResult{
			Host:    host,
			Pruning: info.Pruning,
			SSL:     ssl,
			TCP:     tcp,
			Version: info.Version,
		})
	}

	return servers, nil
}

// FeeRate gets a fee rate estimate for a block confirmation target, where 1
// indicates the next block.
func (wc *WalletClient) FeeRate(ctx context.Context, _ int64) (int64, error) {
	var res struct {
		Method   string `json:"method"`
		SatPerKB int64  `json:"sat/kvB"`
		Tooltip  string `json:"tooltip"`
		Value    int64  `json:"value"`
	}
	err := wc.Call(ctx, methodGetFeeRate, nil, &res)
	if err != nil {
		return 0, err
	}
	return res.SatPerKB, nil
}

type walletReq struct {
	Wallet string `json:"wallet,omitempty"`
}

// CreateNewAddress generates a new address, ignoring the gap limit. NOTE: There
// is no method to retrieve a change address (makes recovery difficult).
func (wc *WalletClient) CreateNewAddress(ctx context.Context) (string, error) {
	var res string
	err := wc.Call(ctx, methodCreateNewAddress, &walletReq{wc.walletFile}, &res)
	if err != nil {
		return "", err
	}
	return res, nil
}

// GetUnusedAddress gets the next unused address from the wallet. It may have
// already been requested.
func (wc *WalletClient) GetUnusedAddress(ctx context.Context) (string, error) {
	var res string
	err := wc.Call(ctx, methodGetUnusedAddress, &walletReq{wc.walletFile}, &res)
	if err != nil {
		return "", err
	}
	return res, nil
}

type addrReq struct {
	Addr   string `json:"address"`
	Wallet string `json:"wallet,omitempty"`
}

// CheckAddress validates the address and reports if it belongs to the wallet.
func (wc *WalletClient) CheckAddress(ctx context.Context, addr string) (valid, mine bool, err error) {
	err = wc.Call(ctx, methodIsMine, addrReq{Addr: addr, Wallet: wc.walletFile}, &mine)
	if err != nil {
		return
	}
	err = wc.Call(ctx, methodValidateAddress, positional{addr}, &valid) // no wallet arg for validateaddress
	if err != nil {
		return
	}
	return
}

// GetAddressHistory returns the history an address. Confirmed transactions will
// have a nil Fee field, while unconfirmed transactions will have a Fee and a
// value of zero for Height.
func (wc *WalletClient) GetAddressHistory(ctx context.Context, addr string) ([]*GetAddressHistoryResult, error) {
	var res []*GetAddressHistoryResult
	err := wc.Call(ctx, methodGetAddressHistory, positional{addr}, &res) // no wallet arg for getaddresshistory
	if err != nil {
		return nil, err
	}
	return res, nil
}

// GetAddressUnspent returns the unspent outputs for an address. Unconfirmed
// outputs will have a value of zero for Height.
func (wc *WalletClient) GetAddressUnspent(ctx context.Context, addr string) ([]*GetAddressUnspentResult, error) {
	var res []*GetAddressUnspentResult
	err := wc.Call(ctx, methodGetAddressUnspent, positional{addr}, &res) // no wallet arg for getaddressunspent
	if err != nil {
		return nil, err
	}
	return res, nil
}

type utxoReq struct {
	UTXO   string `json:"coin"`
	Wallet string `json:"wallet,omitempty"`
}

// FreezeUTXO freezes/locks a single UTXO. It will still be reported by
// listunspent while locked.
func (wc *WalletClient) FreezeUTXO(ctx context.Context, txid string, out uint32) error {
	utxo := txid + ":" + strconv.FormatUint(uint64(out), 10)
	var res bool
	err := wc.Call(ctx, methodFreezeUTXO, &utxoReq{UTXO: utxo, Wallet: wc.walletFile}, &res)
	if err != nil {
		return err
	}
	if !res { // always returns true in all forks I've checked
		return fmt.Errorf("wallet could not freeze utxo %v", utxo)
	}
	return nil
}

// UnfreezeUTXO unfreezes/unlocks a single UTXO.
func (wc *WalletClient) UnfreezeUTXO(ctx context.Context, txid string, out uint32) error {
	utxo := txid + ":" + strconv.FormatUint(uint64(out), 10)
	var res bool
	err := wc.Call(ctx, methodUnfreezeUTXO, &utxoReq{UTXO: utxo, Wallet: wc.walletFile}, &res)
	if err != nil {
		return err
	}
	if !res { // always returns true in all forks I've checked
		return fmt.Errorf("wallet could not unfreeze utxo %v", utxo)
	}
	return nil
}

type txidReq struct {
	TxID   string `json:"txid"`
	Wallet string `json:"wallet,omitempty"`
}

// GetRawTransaction retrieves the serialized transaction identified by txid.
func (wc *WalletClient) GetRawTransaction(ctx context.Context, txid string) ([]byte, error) {
	var res string
	err := wc.Call(ctx, methodGetTransaction, &txidReq{TxID: txid, Wallet: wc.walletFile}, &res)
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
func (wc *WalletClient) GetWalletTxConfs(ctx context.Context, txid string) (int, error) {
	var res struct {
		Confs int `json:"confirmations"`
	}
	err := wc.Call(ctx, methodGetTxStatus, &txidReq{TxID: txid, Wallet: wc.walletFile}, &res)
	if err != nil {
		return 0, err
	}
	return res.Confs, nil
}

// ListUnspent returns details on all unspent outputs for the wallet. Note that
// the pkScript is not included, and the user would have to retrieve it with
// GetRawTransaction for PrevOutHash if the output is of interest.
func (wc *WalletClient) ListUnspent(ctx context.Context) ([]*ListUnspentResult, error) {
	var res []*ListUnspentResult
	err := wc.Call(ctx, methodListUnspent, &walletReq{wc.walletFile}, &res)
	if err != nil {
		return nil, err
	}
	return res, nil
}

// GetBalance returns the result of the getbalance wallet RPC.
func (wc *WalletClient) GetBalance(ctx context.Context) (*Balance, error) {
	var res struct {
		Confirmed   floatString `json:"confirmed"`
		Unconfirmed floatString `json:"unconfirmed"`
		Immature    floatString `json:"unmatured"` // yes, unmatured!
	}
	err := wc.Call(ctx, methodGetBalance, &walletReq{wc.walletFile}, &res)
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
	Addr       string   `json:"destination"`
	Amount     string   `json:"amount"` // BTC, or "!" for max
	Fee        *float64 `json:"fee,omitempty"`
	FeeRate    *float64 `json:"feerate,omitempty"` // sat/vB, gets multiplied by 1000 for extra precision, omit for high prio
	ChangeAddr string   `json:"change_addr,omitempty"`
	// FromAddr omitted
	FromUTXOs      string `json:"from_coins,omitempty"`
	NoCheck        bool   `json:"nocheck"`
	Unsigned       bool   `json:"unsigned"`      // unsigned returns a base64 psbt thing
	RBF            bool   `json:"rbf,omitempty"` // default to null
	Password       string `json:"password,omitempty"`
	LockTime       *int64 `json:"locktime,omitempty"`
	AddTransaction bool   `json:"addtransaction"`
	Wallet         string `json:"wallet,omitempty"`
}

// PayTo sends the specified amount in BTC (or the conventional unit for the
// assets e.g. LTC) to an address using a certain fee rate. The transaction is
// not broadcasted; the raw bytes of the signed transaction are returned. After
// the caller verifies the transaction, it may be sent with Broadcast.
func (wc *WalletClient) PayTo(ctx context.Context, walletPass string, addr string, amtBTC float64, feeRate float64) ([]byte, error) {
	if feeRate < 1 {
		return nil, errors.New("fee rate in sat/vB too low")
	}
	amt := strconv.FormatFloat(amtBTC, 'f', 8, 64)
	var res string
	err := wc.Call(ctx, methodPayTo, &paytoReq{
		Addr:     addr,
		Amount:   amt,
		FeeRate:  &feeRate,
		Password: walletPass,
		// AddTransaction adds the transaction to Electrum as a "local" txn
		// before broadcasting. If we don't, rapid back-to-back sends can result
		// in a mempool conflict from spending the same prevouts.
		AddTransaction: true,
		Wallet:         wc.walletFile,
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

// PayToFromCoinsAbsFee allows specifying prevouts (in txid:vout format) and an
// absolute fee in BTC instead of a fee rate. This combination allows specifying
// precisely how much will be withdrawn from the wallet (subtracting fees),
// unless the change is dust and omitted. The transaction is not broadcasted;
// the raw bytes of the signed transaction are returned. After the caller
// verifies the transaction, it may be sent with Broadcast.
func (wc *WalletClient) PayToFromCoinsAbsFee(ctx context.Context, walletPass string, fromCoins []string, addr string, amtBTC float64, absFee float64) ([]byte, error) {
	if absFee > 1 {
		return nil, errors.New("abs fee too high")
	}
	amt := strconv.FormatFloat(amtBTC, 'f', 8, 64)
	var res string
	err := wc.Call(ctx, methodPayTo, &paytoReq{
		Addr:           addr,
		Amount:         amt,
		Fee:            &absFee,
		Password:       walletPass,
		FromUTXOs:      strings.Join(fromCoins, ","),
		AddTransaction: true,
		Wallet:         wc.walletFile,
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

// Sweep sends all available funds to an address with a specified fee rate. No
// change output is created. The transaction is not broadcasted; the raw bytes
// of the signed transaction are returned. After the caller verifies the
// transaction, it may be sent with Broadcast.
func (wc *WalletClient) Sweep(ctx context.Context, walletPass string, addr string, feeRate float64) ([]byte, error) {
	if feeRate < 1 {
		return nil, errors.New("fee rate in sat/vB too low")
	}
	var res string
	err := wc.Call(ctx, methodPayTo, &paytoReq{
		Addr:           addr,
		Amount:         "!", // special "max" indicator, creating no change output
		FeeRate:        &feeRate,
		Password:       walletPass,
		AddTransaction: true,
		Wallet:         wc.walletFile,
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
	Pass string `json:"password,omitempty"`
	// 4.0.9 has privkey in this request, but 4.2 does not since it has a
	// signtransaction_with_privkey request. (this RPC should not use positional
	// arguments)
	// Privkey string `json:"privkey,omitempty"` // sign with wallet if empty
	Wallet         string `json:"wallet,omitempty"`
	IgnoreWarnings bool   `json:"iknowwhatimdoing,omitempty"`
}

// SetIncludeIgnoreWarnings sets the includeIgnoreWarnings bool. Needed for btc
// at 4.5.5 but causes ltc at 4.2.2 to fail.
func (wc *WalletClient) SetIncludeIgnoreWarnings(include bool) {
	wc.includeIgnoreWarnings.Store(include)
}

// SignTx signs the base-64 encoded PSBT with the wallet's keys, returning the
// signed transaction.
func (wc *WalletClient) SignTx(ctx context.Context, walletPass string, psbtB64 string) ([]byte, error) {
	var res string
	req := &signTransactionArgs{
		Tx:     psbtB64,
		Pass:   walletPass,
		Wallet: wc.walletFile}
	if wc.includeIgnoreWarnings.Load() {
		req.IgnoreWarnings = true
	}
	err := wc.Call(ctx, methodSignTransaction, req, &res)
	if err != nil {
		return nil, err
	}
	txRaw, err := hex.DecodeString(res)
	if err != nil {
		return nil, err
	}
	return txRaw, nil
}

// Broadcast submits the transaction to the network.
func (wc *WalletClient) Broadcast(ctx context.Context, tx []byte) (string, error) {
	txStr := hex.EncodeToString(tx)
	var res string
	err := wc.Call(ctx, methodBroadcast, positional{txStr}, &res) // no wallet arg
	if err != nil {
		return "", err
	}
	return res, nil
}

type rawTxReq struct {
	RawTx  string `json:"tx"`
	Wallet string `json:"wallet,omitempty"`
}

// AddLocalTx is used to add a "local" transaction to the Electrum wallet DB.
// This does not broadcast it.
func (wc *WalletClient) AddLocalTx(ctx context.Context, tx []byte) (string, error) {
	txStr := hex.EncodeToString(tx)
	var txid string
	err := wc.Call(ctx, methodAddLocalTx, &rawTxReq{RawTx: txStr, Wallet: wc.walletFile}, &txid)
	if err != nil {
		return "", err
	}
	return txid, nil
}

// RemoveLocalTx is used to remove a "local" transaction from the Electrum
// wallet DB. This can only be done if the tx was not broadcasted. This is
// required if using AddLocalTx or a payTo method that added the local
// transaction but either it failed to broadcast or the user no longer wants to
// send it after inspecting the raw transaction. Calling RemoveLocalTx with an
// already broadcast or non-existent txid will not generate an error.
func (wc *WalletClient) RemoveLocalTx(ctx context.Context, txid string) error {
	return wc.Call(ctx, methodRemoveLocalTx, &txidReq{TxID: txid, Wallet: wc.walletFile}, nil)
}

type getPrivKeyArgs struct {
	Addr   string `json:"address"`
	Pass   string `json:"password,omitempty"`
	Wallet string `json:"wallet,omitempty"`
}

// GetPrivateKeys uses the getprivatekeys RPC to retrieve the keys for a given
// address. The returned string is WIF-encoded.
func (wc *WalletClient) GetPrivateKeys(ctx context.Context, walletPass, addr string) (string, error) {
	var res string
	err := wc.Call(ctx, methodGetPrivateKeys, &getPrivKeyArgs{
		Addr:   addr,
		Pass:   walletPass,
		Wallet: wc.walletFile},
		&res)
	if err != nil {
		return "", err
	}
	privSplit := strings.Split(res, ":")
	if len(privSplit) != 2 {
		return "", errors.New("bad key")
	}
	return privSplit[1], nil
}

type onchainHistoryReq struct {
	Wallet string `json:"wallet,omitempty"`
	From   int64  `json:"from_height,omitempty"`
	To     int64  `json:"to_height,omitempty"`
}

func (wc *WalletClient) OnchainHistory(ctx context.Context, from, to int64) ([]TransactionResult, error) {
	// A balance summary is included but left out here.
	var res struct {
		Transactions []TransactionResult `json:"transactions"`
	}
	err := wc.Call(ctx, methodOnchainHistory, &onchainHistoryReq{Wallet: wc.walletFile, From: from, To: to}, &res)
	if err != nil {
		return nil, err
	}
	return res.Transactions, nil
}

func (wc *WalletClient) Version(ctx context.Context) (string, error) {
	var res string
	err := wc.Call(ctx, methodVersion, &walletReq{wc.walletFile}, &res)
	if err != nil {
		return "", err
	}
	return res, nil
}
