// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package webserver

var (
	// updateWalletRoute is a notification route that updates the state of a
	// wallet.
	updateWalletRoute = "update_wallet"
	// errMsgRoute is used to send a simple error message .
	errorMsgRoute = "error_message"
	// successMsgRoute is used to send a simple success message.
	successMsgRoute = "success_message"
)

type walletStatus struct {
	Symbol  string `json:"symbol"`
	AssetID uint32 `json:"asset"`
	Open    bool   `json:"open"`
	Running bool   `json:"running"`
}
