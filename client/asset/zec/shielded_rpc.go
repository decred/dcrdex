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
	methodZGetNotesCount        = "z_getnotescount"
	methodZListUnspent          = "z_listunspent"
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
	return unified, c.CallRPC(methodZGetAddressForAccount, []any{acctNumber, addrTypes}, &unified)
}

type unifiedReceivers struct {
	Transparent string `json:"p2pkh"`
	Orchard     string `json:"orchard"`
	Sapling     string `json:"sapling,omitempty"`
}

// z_listunifiedreceivers unified_address
func zGetUnifiedReceivers(c rpcCaller, unifiedAddr string) (receivers *unifiedReceivers, err error) {
	return receivers, c.CallRPC(methodZListUnifiedReceivers, []any{unifiedAddr}, &receivers)
}

type valZat struct { // Ignoring similar fields for Sapling, Transparent...
	ValueZat uint64 `json:"valueZat"`
}

type poolBalances struct {
	Orchard     uint64 `json:"orchard"`
	Transparent uint64 `json:"transparent"`
	Sapling     uint64 `json:"sapling"`
}

type zBalancePools struct {
	Orchard     valZat `json:"orchard"`
	Transparent valZat `json:"transparent"`
	Sapling     valZat `json:"sapling"`
}

type zAccountBalance struct {
	Pools zBalancePools `json:"pools"`
}

func zGetBalanceForAccount(c rpcCaller, acct uint32, confs int) (*poolBalances, error) {
	var res zAccountBalance
	return &poolBalances{
		Orchard:     res.Pools.Orchard.ValueZat,
		Transparent: res.Pools.Transparent.ValueZat,
		Sapling:     res.Pools.Sapling.ValueZat,
	}, c.CallRPC(methodZGetBalanceForAccount, []any{acct, confs}, &res)
}

type zNotesCount struct {
	Sprout  uint32 `json:"sprout"`
	Sapling uint32 `json:"sapling"`
	Orchard uint32 `json:"orchard"`
}

func zGetNotesCount(c rpcCaller) (counts *zNotesCount, _ error) {
	return counts, c.CallRPC(methodZGetNotesCount, []any{minOrchardConfs}, &counts)
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
	return res, c.CallRPC(methodZValidateAddress, []any{addr}, &res)
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
	FullPrivacy                  privacyPolicy = "FullPrivacy"
	AllowRevealedRecipients      privacyPolicy = "AllowRevealedRecipients"
	AllowRevealedAmounts         privacyPolicy = "AllowRevealedAmounts"
	AllowRevealedSenders         privacyPolicy = "AllowRevealedSenders"
	AllowLinkingAccountAddresses privacyPolicy = "AllowLinkingAccountAddresses"
	NoPrivacy                    privacyPolicy = "NoPrivacy"
)

// z_sendmany "fromaddress" [{"address":... ,"amount":...},...] ( minconf ) ( fee ) ( privacyPolicy )
func zSendMany(c rpcCaller, fromAddress string, recips []*zSendManyRecipient, priv privacyPolicy) (operationID string, err error) {
	const minConf, fee = 1, 0.00001
	return operationID, c.CallRPC(methodZSendMany, []any{fromAddress, recips, minConf, fee, priv}, &operationID)
}

type opResult struct {
	TxID string `json:"txid"`
}

type operationStatus struct {
	ID           string    `json:"id"`
	Status       string    `json:"status"` // "success", "failed", others?
	CreationTime int64     `json:"creation_time"`
	Error        *struct { // nil for success
		Code    int32  `json:"code"`
		Message string `json:"message"`
	} `json:"error" `
	Result           *opResult `json:"result"`
	ExecutionSeconds float64   `json:"execution_secs"`
	Method           string    `json:"method"`
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
	if err := c.CallRPC(methodZGetOperationResult, []any{[]string{operationID}}, &res); err != nil {
		return nil, err
	}
	if len(res) == 0 {
		return nil, ErrEmptyOpResults
	}
	return res[0], nil
}

type zListUnspentResult struct {
	TxID          string  `json:"txid"`
	Pool          string  `json:"pool"`
	JSIndex       uint32  `json:"jsindex"`
	JSOutindex    uint32  `json:"jsoutindex"`
	Outindex      uint32  `json:"outindex"`
	Confirmations uint64  `json:"confirmations"`
	Spendable     bool    `json:"spendable"`
	Account       uint32  `json:"account"`
	Address       string  `json:"address"`
	Amount        float64 `json:"amount"`
	Memo          string  `json:"memo"`
	MemoStr       string  `json:"memoStr"`
	Change        bool    `json:"change"`
}

// z_listunspent ( minconf maxconf includeWatchonly ["zaddr",...] asOfHeight )
func zListUnspent(c rpcCaller) ([]*zListUnspentResult, error) {
	var res []*zListUnspentResult
	return res, c.CallRPC(methodZListUnspent, []any{minOrchardConfs}, &res)
}
