// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package db

// DB is an interface that must be satisfied by a DEX client persistent storage
// manager.
type DB interface {
	// ListAccounts returns a list of DEX URLs. The DB is designed to have a
	// single account per DEX, so the account is uniquely identified by the DEX
	// URL.
	ListAccounts() []string
	// Account gets the AccountInfo associated with the specified DEX node.
	Account(url string) (*AccountInfo, error)
	// CreateAccount saves the AccountInfo.
	CreateAccount(ai *AccountInfo) error
	// UpdateOrder saves the order information in the database. Any existing
	// order info will be overwritten without indication.
	UpdateOrder(m *MetaOrder) error
	// ActiveOrders retrieves all orders which appear to be in an active state,
	// which is either in the epoch queue or in the order book.
	ActiveOrders() ([]*MetaOrder, error)
	// AccountOrders retrieves all orders associated with the specified DEX.
	AccountOrders(dex string) ([]*MetaOrder, error)
	// MarketOrders retrieves all orders for the specified DEX and market.
	MarketOrders(dex string, base, quote uint32) ([]*MetaOrder, error)
	// UpdateMatch updates the match information in the database. Any existing
	// entry for the match will be overwritten without indication.
	UpdateMatch(m *MetaMatch) error
	// ActiveMatches retrieves the matches that are in an active state, which is
	// any state except order.MatchComplete.
	ActiveMatches() ([]*MetaMatch, error)
}
