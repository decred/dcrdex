// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package zec

import (
	"decred.org/dcrdex/dex"
	"github.com/btcsuite/btcd/btcutil"
)

const (
	methodZGetAddressForAccount = "z_getaddressforaccount"
	methodZListUnifiedReceivers = "z_listunifiedreceivers"
	methodZListAccounts         = "z_listaccounts"
	methodZGetNewAccount        = "z_getnewaccount"
	methodZGetBalanceForAccount = "z_getbalanceforaccount"
	methodZSendMany             = "z_sendmany"
	methodZValidateAddress      = "z_validateaddress"
	methodZGetOperationResult   = "z_getoperationresult"
)

type zListAccountsResult struct {
	Number    uint32 `json:"account"`
	Addresses []struct {
		Diversifier uint32 `json:"diversifier"`
		UnifiedAddr string `json:"ua"`
	} `json:"addresses"`
}

// z_listaccounts
func zListAccounts(c rpcCaller) (accts []zListAccountsResult, err error) {
	return accts, c.CallRPC(methodZListAccounts, nil, &accts)
}

// z_getnewaccount
func zGetNewAccount(c rpcCaller) (uint32, error) {
	var res struct {
		Number uint32 `json:"account"`
	}
	if err := c.CallRPC(methodZGetNewAccount, nil, &res); err != nil {
		return 0, err
	}
	return res.Number, nil
}

type zGetAddressForAccountResult struct {
	Account          uint32   `json:"account"`
	DiversifierIndex uint32   `json:"diversifier_index"`
	ReceiverTypes    []string `json:"receiver_types"`
	Address          string   `json:"address"`
}

// z_getaddressforaccount account ( ["receiver_type", ...] diversifier_index )
func zGetAddressForAccount(c rpcCaller, acctNumber uint32, addrTypes []string) (unified *zGetAddressForAccountResult, err error) {
	return unified, c.CallRPC(methodZGetAddressForAccount, []interface{}{acctNumber, addrTypes}, &unified)
}

type unifiedReceivers struct {
	Transparent string `json:"p2pkh"`
	Orchard     string `json:"orchard"`
	Sapling     string `json:"sapling"`
}

// z_listunifiedreceivers unified_address
func zGetUnifiedReceivers(c rpcCaller, unifiedAddr string) (receivers *unifiedReceivers, err error) {
	return receivers, c.CallRPC(methodZListUnifiedReceivers, []interface{}{unifiedAddr}, &receivers)
}

func zGetBalanceForAccount(c rpcCaller, acct uint32) (uint64, error) {
	const minConf = 1

	var res struct {
		Pools struct {
			Orchard struct { // Ignoring similar fields for Sapling, Transparent...
				ValueZat uint64 `json:"valueZat"`
			} `json:"orchard"`
		} `json:"pools"`
	}
	return res.Pools.Orchard.ValueZat, c.CallRPC(methodZGetBalanceForAccount, []interface{}{acct, minConf}, &res)
}

type zValidateAddressResult struct {
	IsValid     bool   `json:"isvalid"`
	Address     string `json:"address"`
	AddressType string `json:"address_type"`
	IsMine      bool   `json:"ismine"`
	// PayingKey string `json:"payingkey"`
	// TransmissionKey string `json:"transmissionkey"`
	Diversifier                string `json:"diversifier"`                // sapling
	DiversifiedTransmissionkey string `json:"diversifiedtransmissionkey"` // sapling
}

// z_validateaddress "address"
func zValidateAddress(c rpcCaller, addr string) (res *zValidateAddressResult, err error) {
	return res, c.CallRPC(methodZValidateAddress, []interface{}{addr}, &res)
}

type zSendManyRecipient struct {
	Address string  `json:"address"`
	Amount  float64 `json:"amount"`
	Memo    string  `json:"memo,omitempty"`
}

func singleSendManyRecipient(addr string, amt uint64) []*zSendManyRecipient {
	return []*zSendManyRecipient{
		{
			Address: addr,
			Amount:  btcutil.Amount(amt).ToBTC(),
		},
	}
}

// When sending using z_sendmany and you're not sending a shielded tx to the
// same pool, you must specify a lower privacy policy.
type privacyPolicy string

const (
	FullPrivacy             privacyPolicy = "FullPrivacy"
	AllowRevealedRecipients privacyPolicy = "AllowRevealedRecipients"
	AllowRevealedAmounts    privacyPolicy = "AllowRevealedAmounts"
	AllowRevealedSenders    privacyPolicy = "AllowRevealedSenders"
)

// z_sendmany "fromaddress" [{"address":... ,"amount":...},...] ( minconf ) ( fee ) ( privacyPolicy )
func zSendMany(c rpcCaller, fromAddress string, recips []*zSendManyRecipient, priv privacyPolicy) (operationID string, err error) {
	const minConf = 1
	var fee *uint64 // Only makes sense with >= 5.5.0
	return operationID, c.CallRPC(methodZSendMany, []interface{}{fromAddress, recips, minConf, fee, priv}, &operationID)
}

type operationStatus struct {
	ID           string    `json:"id"`
	Status       string    `json:"status"` // "success", "failed", others?
	CreationTime int64     `json:"creation_time"`
	Error        *struct { // nil for success
		Code    int32  `json:"code"`
		Message string `json:"message"`
	} `json:"error" `
	Result *struct {
		TxID string `json:"txid"`
	} `json:"result"`
	ExecutionSeconds float64 `json:"execution_secs"`
	Method           string  `json:"method"`
	Params           struct {
		FromAddress string `json:"fromaddress"`
		Amounts     []struct {
			Address string  `json:"address"`
			Amount  float64 `json:"amount"`
		} `json:"amounts"`
		MinConf uint32  `json:"minconf"`
		Fee     float64 `json:"fee"`
	} `json:"params"`
}

// ErrEmptyOpResults is returned when z_getoperationresult returns an empty
// array for the specified operation ID. This appears to be normal in some or
// all cases when the result is not yet ready.
const ErrEmptyOpResults = dex.ErrorKind("no z_getoperationstatus results")

// z_getoperationresult (["operationid", ... ])
func zGetOperationResult(c rpcCaller, operationID string) (s *operationStatus, err error) {
	var res []*operationStatus
	if err := c.CallRPC(methodZGetOperationResult, []interface{}{[]string{operationID}}, &res); err != nil {
		return nil, err
	}
	if len(res) == 0 {
		return nil, ErrEmptyOpResults
	}
	return res[0], nil
}
