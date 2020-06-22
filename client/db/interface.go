// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package db

import (
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/order"
)

// DB is an interface that must be satisfied by a DEX client persistent storage
// manager.
type DB interface {
	dex.Runner
	// Store allows the storage of arbitrary data.
	Store(string, []byte) error
	// Get retrieves values stored with Store.
	Get(string) ([]byte, error)
	// ValueExists checks if a value was previously stored.
	ValueExists(k string) (bool, error)
	// ListAccounts returns a list of DEX URLs. The DB is designed to have a
	// single account per DEX, so the account is uniquely identified by the DEX
	// URL.
	ListAccounts() ([]string, error)
	// Accounts retrieves all accounts.
	Accounts() ([]*AccountInfo, error)
	// Account gets the AccountInfo associated with the specified DEX node.
	Account(url string) (*AccountInfo, error)
	// CreateAccount saves the AccountInfo.
	CreateAccount(ai *AccountInfo) error
	// AccountPaid marks the account as paid.
	AccountPaid(proof *msgjson.AccountProof) error
	// UpdateOrder saves the order information in the database. Any existing
	// order info will be overwritten without indication.
	UpdateOrder(m *MetaOrder) error
	// ActiveOrders retrieves all orders which appear to be in an active state,
	// which is either in the epoch queue or in the order book.
	ActiveOrders() ([]*MetaOrder, error)
	// AccountOrders retrieves all orders associated with the specified DEX. The
	// order count can be limited by supplying a non-zero n value. In that case
	// the newest n orders will be returned. The orders can be additionally
	// filtered by supplying a non-zero since value, corresponding to a UNIX
	// timestamp, in milliseconds. n = 0 applies no limit on number of orders
	// returned. since = 0 is equivalent to disabling the time filter, since
	// no orders were created before before 1970.
	AccountOrders(dex string, n int, since uint64) ([]*MetaOrder, error)
	// Order fetches a MetaOrder by order ID.
	Order(order.OrderID) (*MetaOrder, error)
	// ActiveDEXOrders retrieves orders for a particular dex, specified by its
	// URL.
	ActiveDEXOrders(dex string) ([]*MetaOrder, error)
	// MarketOrders retrieves all orders for the specified DEX and market. The
	// order count can be limited by supplying a non-zero n value. In that case
	// the newest n orders will be returned. The orders can be additionally
	// filtered by supplying a non-zero since value, corresponding to a UNIX
	// timestamp, in milliseconds. n = 0 applies no limit on number of orders
	// returned. since = 0 is equivalent to disabling the time filter, since
	// no orders were created before before 1970.
	MarketOrders(dex string, base, quote uint32, n int, since uint64) ([]*MetaOrder, error)
	// SetChangeCoin stores the change coin for the order.
	SetChangeCoin(order.OrderID, order.CoinID) error
	// UpdateOrderStatus sets the order status for an order.
	UpdateOrderStatus(oid order.OrderID, status order.OrderStatus) error
	// UpdateMatch updates the match information in the database. Any existing
	// entry for the match will be overwritten without indication.
	UpdateMatch(m *MetaMatch) error
	// ActiveMatches retrieves the matches that are in an active state, which is
	// any state except order.MatchComplete.
	ActiveMatches() ([]*MetaMatch, error)
	// ActiveDEXMatches retrieves the matches that are in an active state for a
	// specified DEX, which is any state except order.MatchComplete.
	ActiveDEXMatches(dex string) ([]*MetaMatch, error)
	// MatchesForOrder gets the matches for the order ID.
	MatchesForOrder(oid order.OrderID) ([]*MetaMatch, error)
	// Update wallets adds a wallet to the database, or updates the wallet
	// credentials if the wallet already exists. A wallet is specified by the
	// pair (asset ID, account name).
	UpdateWallet(wallet *Wallet) error
	// UpdateBalance updates a wallet's balance.
	UpdateBalance(wid []byte, balance *Balance) error
	// Wallets lists all saved wallets.
	Wallets() ([]*Wallet, error)
	// Backup makes a copy of the database.
	Backup() error
	// SaveNotification saves the notification.
	SaveNotification(*Notification) error
	// NotificationsN reads out the N most recent notifications.
	NotificationsN(int) ([]*Notification, error)
	// AckNotification sets the acknowledgement for a notification.
	AckNotification(id []byte) error
}
