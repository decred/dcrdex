// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package webserver

import (
	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/dex/encode"
)

// standardResponse is a basic API response when no data needs to be returned.
type standardResponse struct {
	OK  bool   `json:"ok"`
	Msg string `json:"msg,omitempty"`
}

// simpleAck is a plain standardResponse with "ok" = true.
func simpleAck() *standardResponse {
	return &standardResponse{
		OK: true,
	}
}

// The loginForm is sent by the client to log in to a DEX.
type loginForm struct {
	Pass encode.PassBytes `json:"pass"`
}

// registration is used to register a new DEX account.
type registration struct {
	Addr     string           `json:"addr"`
	Cert     string           `json:"cert"`
	Password encode.PassBytes `json:"pass"`
	Fee      uint64           `json:"fee"`
}

// newWalletForm is information necessary to create a new wallet.
type newWalletForm struct {
	AssetID uint32 `json:"assetID"`
	// These are only used if the Decred wallet does not already exist. In that
	// case, these parameters will be used to create the wallet.
	Account string           `json:"account"`
	Config  string           `json:"config"`
	Pass    encode.PassBytes `json:"pass"`
	AppPW   encode.PassBytes `json:"appPass"`
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
	OrderID string           `json:"orderID"`
}

// withdrawForm is sent to initiate a withdraw.
type withdrawForm struct {
	AssetID uint32           `json:"assetID"`
	Value   uint64           `json:"value"`
	Address string           `json:"address"`
	Pass    encode.PassBytes `json:"pw"`
}
