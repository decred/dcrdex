// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package zec

const (
	methodZGetAddressForAccount = "z_getaddressforaccount"
	methodZListUnifiedReceivers = "z_listunifiedreceivers"
	methodZListAccounts         = "z_listaccounts"
	methodZGetNewAccount        = "z_getnewaccount"
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
