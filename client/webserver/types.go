// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package webserver

import (
	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/encode"
)

// standardResponse is a basic API response when no data needs to be returned.
type standardResponse struct {
	OK   bool   `json:"ok"`
	Msg  string `json:"msg,omitempty"`
	Code *int   `json:"code,omitempty"`
}

// simpleAck is a plain standardResponse with "ok" = true.
func simpleAck() *standardResponse {
	return &standardResponse{
		OK: true,
	}
}

// The initForm is sent by the client to initialize the DEX.
type initForm struct {
	Pass         encode.PassBytes `json:"pass"`
	Seed         dex.Bytes        `json:"seed,omitempty"`
	RememberPass bool             `json:"rememberPass"`
}

// The loginForm is sent by the client to log in to a DEX.
type loginForm struct {
	Pass         encode.PassBytes `json:"pass"`
	RememberPass bool             `json:"rememberPass"`
}

// registrationForm is used to register a new DEX account.
type registrationForm struct {
	Addr     string           `json:"addr"`
	Cert     string           `json:"cert"`
	Password encode.PassBytes `json:"pass"`
	AssetID  *uint32          `json:"asset,omitempty"` // prevent omission using BTC
}

// postBondForm is used to post a new bond for an existing DEX account.
type postBondForm struct {
	Addr     string           `json:"addr"`
	Cert     string           `json:"cert"` // may be empty for adding bond to existing account
	Password encode.PassBytes `json:"pass"`
	Bond     uint64           `json:"bond"`
	AssetID  *uint32          `json:"asset,omitempty"` // prevent omission using BTC
	LockTime uint64           `json:"lockTime"`
}

type registrationTxFeeForm struct {
	Addr    string  `json:"addr"`
	Cert    string  `json:"cert"`
	AssetID *uint32 `json:"asset,omitempty"`
}

type walletConfig struct {
	AssetID    uint32 `json:"assetID"`
	WalletType string `json:"walletType"`
	// These are only used if the Decred wallet does not already exist. In that
	// case, these parameters will be used to create the wallet.
	Config map[string]string `json:"config"`
}

// newWalletForm is information necessary to create a new wallet.
type newWalletForm struct {
	walletConfig
	Pass       encode.PassBytes `json:"pass"`
	AppPW      encode.PassBytes `json:"appPass"`
	ParentForm *walletConfig    `json:"parentForm"`
}

// openWalletForm is information necessary to open a wallet.
type openWalletForm struct {
	AssetID uint32           `json:"assetID"`
	Pass    encode.PassBytes `json:"pass"` // Application password.
}

type tradeForm struct {
	Pass  encode.PassBytes `json:"pw"`
	Order *core.TradeForm  `json:"order"`
}

type cancelForm struct {
	Pass    encode.PassBytes `json:"pw"`
	OrderID dex.Bytes        `json:"orderID"`
}

// sendOrWithdrawForm is sent to initiate either send or withdraw tx.
type sendOrWithdrawForm struct {
	AssetID  uint32           `json:"assetID"`
	Value    uint64           `json:"value"`
	Address  string           `json:"address"`
	Subtract bool             `json:"subtract"`
	Pass     encode.PassBytes `json:"pw"`
}

type accountExportForm struct {
	Pass encode.PassBytes `json:"pw"`
	Host string           `json:"host"`
}

type accountImportForm struct {
	Pass    encode.PassBytes `json:"pw"`
	Account *core.Account    `json:"account"`
}

type accountDisableForm struct {
	Pass encode.PassBytes `json:"pw"`
	Host string           `json:"host"`
}
