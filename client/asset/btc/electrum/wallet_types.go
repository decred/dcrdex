// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package electrum

// GetInfoResult is the result of the getinfo RPC.
type GetInfoResult struct {
	AutoConnect   bool   `json:"auto_connect"`
	SyncHeight    int64  `json:"blockchain_height"`
	Connected     bool   `json:"connected"`
	DefaultWallet string `json:"default_wallet"`
	FeePerKB      int64  `json:"fee_per_kb"`
	Path          string `json:"path"`
	Server        string `json:"server"`
	ServerHeight  int64  `json:"server_height"`
	Connections   int64  `json:"spv_nodes"`
	Version       string `json:"version"`
}

// GetServersResult is an entry in the map returned by the getservers RPC, with
// the Host name included.
type GetServersResult struct {
	Host    string
	Pruning string
	SSL     uint16
	TCP     uint16
	Version string
}

// GetAddressHistoryResult is an element of the array returned by the
// getaddresshistory RPC.
type GetAddressHistoryResult struct {
	Fee    *int64 `json:"fee,omitempty"` // set when unconfirmed
	Height int64  `json:"height"`        // 0 when unconfirmed
	TxHash string `json:"tx_hash"`
}

// GetAddressUnspentResult is an element of the array returned by the
// getaddressunspent RPC.
type GetAddressUnspentResult struct { // todo: check unconfirmed fields
	Height int64  `json:"height"`
	TxHash string `json:"tx_hash"`
	TxPos  int32  `json:"tx_pos"`
	Value  int64  `json:"value"`
}

// ListUnspentResult is an element of the array returned by the listunspent RPC.
type ListUnspentResult struct {
	Address       string `json:"address"`
	Value         string `json:"value"` // BTC in a string :/
	Coinbase      bool   `json:"coinbase"`
	Height        int64  `json:"height"`
	Sequence      uint32 `json:"nsequence"`
	PrevOutHash   string `json:"prevout_hash"`
	PrevOutIdx    uint32 `json:"prevout_n"`
	RedeemScript  string `json:"redeem_script"`
	WitnessScript string `json:"witness_script"`
	// PartSigs ? "part_sigs": {},
	// BIP32Paths string    `json:"bip32_paths"`
	// Sighash "sighash": null,
	// "unknown_psbt_fields": {},
	// "utxo": null,
	// "witness_utxo": null
}

// Balance is the result of the balance RPC.
type Balance struct {
	Confirmed   float64 // not reduced by spends until the txn is confirmed
	Unconfirmed float64 // will be negative for sends
	Immature    float64
}
