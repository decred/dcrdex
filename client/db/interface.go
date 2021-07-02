// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package db

import (
	"context"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/encrypt"
	"decred.org/dcrdex/dex/order"
)

// DB is an interface that must be satisfied by a DEX client persistent storage
// manager.
type DB interface {
	dex.Runner
	// SetPrimaryCredentials sets the initial *PrimaryCredentials.
	SetPrimaryCredentials(creds *PrimaryCredentials) error
	// PrimaryCredentials fetches the *PrimaryCredentials.
	PrimaryCredentials() (*PrimaryCredentials, error)
	// Recrypt re-encrypts the wallet passwords and account private keys, and
	// stores the new *PrimaryCredentials.
	Recrypt(creds *PrimaryCredentials, oldCrypter, newCrypter encrypt.Crypter) (
		walletUpdates map[uint32][]byte, acctUpdates map[string][]byte, err error)
	// ListAccounts returns a list of DEX URLs. The DB is designed to have a
	// single account per DEX, so the account is uniquely identified by the DEX
	// host.
	ListAccounts() ([]string, error)
	// Accounts retrieves all accounts.
	Accounts() ([]*AccountInfo, error)
	// Account gets the AccountInfo associated with the specified DEX node.
	Account(host string) (*AccountInfo, error)
	// CreateAccount saves the AccountInfo.
	CreateAccount(ai *AccountInfo) error
	// UpdateAccountInfo updates the account info for an existing account with
	// the same Host as the parameter. If no account exists with this host,
	// an error is returned.
	UpdateAccountInfo(ai *AccountInfo) error
	// AddBond saves a new Bond for a DEX.
	AddBond(host string, bond *Bond) error
	ConfirmBond(host string, assetID uint32, bondCoinID []byte) error
	BondRefunded(host string, assetID uint32, bondCoinID []byte) error
	// DisableAccount sets the AccountInfo disabled status to true.
	DisableAccount(host string) error
	// AccountProof retrieves the AccountPoof value specified by url. DEPRECATED
	AccountProof(host string) (*AccountProof, error)
	// StoreAccountProof stores an AccountProof, marking the account as paid
	// with the legacy registration fee. DEPRECATED
	StoreAccountProof(proof *AccountProof) error
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
	// Orders fetches a slice of orders, sorted by descending time, and filtered
	// with the provided OrderFilter.
	Orders(*OrderFilter) ([]*MetaOrder, error)
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
	// UpdateOrderMetaData updates the order metadata, not including the Host.
	UpdateOrderMetaData(order.OrderID, *OrderMetaData) error
	// UpdateOrderStatus sets the order status for an order.
	UpdateOrderStatus(oid order.OrderID, status order.OrderStatus) error
	// LinkOrder sets the LinkedOrder field of the specified order's
	// OrderMetaData.
	LinkOrder(oid, linkedID order.OrderID) error
	// UpdateMatch updates the match information in the database. Any existing
	// entry for the match will be overwritten without indication.
	UpdateMatch(m *MetaMatch) error
	// ActiveMatches retrieves the matches that are in an active state, which is
	// any state except order.MatchComplete.
	ActiveMatches() ([]*MetaMatch, error)
	// DEXOrdersWithActiveMatches retrieves order IDs for any order that has
	// active matches, regardless of whether the order itself is in an active
	// state.
	DEXOrdersWithActiveMatches(dex string) ([]order.OrderID, error)
	// MatchesForOrder gets the matches for the order ID.
	MatchesForOrder(oid order.OrderID, excludeCancels bool) ([]*MetaMatch, error)
	// Update wallets adds a wallet to the database, or updates the wallet
	// credentials if the wallet already exists. A wallet is specified by the
	// pair (asset ID, account name).
	UpdateWallet(wallet *Wallet) error
	// SetWalletPassword sets the encrypted password for the wallet.
	SetWalletPassword(wid []byte, newPW []byte) error
	// UpdateBalance updates a wallet's balance.
	UpdateBalance(wid []byte, balance *Balance) error
	// Wallets lists all saved wallets.
	Wallets() ([]*Wallet, error)
	// Wallet fetches the wallet for the specified asset by wallet ID.
	Wallet(wid []byte) (*Wallet, error)
	// Backup makes a copy of the database to the default "backups" folder.
	Backup() error
	// BackupTo makes a backup of the database at the specified location,
	// optionally overwriting any existing file and compacting the database.
	BackupTo(dst string, overwrite, compact bool) error
	// SaveNotification saves the notification.
	SaveNotification(*Notification) error
	// NotificationsN reads out the N most recent notifications.
	NotificationsN(int) ([]*Notification, error)
	// AckNotification sets the acknowledgement for a notification.
	AckNotification(id []byte) error
	// DeleteInactiveOrders deletes inactive orders from the database that
	// have been updated after the supplied time. If no time is supplied
	// the current time is used. Accepts an optional function to perform on
	// deleted orders.
	DeleteInactiveOrders(ctx context.Context, olderThan *time.Time, perOrderFn func(ord *MetaOrder) error) error
	// DeleteInactiveMatches deletes inactive matches from the database
	// that have been created after the supplied time. If no time is
	// supplied the current time is used. Accepts an optional function to
	// perform on deleted matches that includes if it was a sell order.
	DeleteInactiveMatches(ctx context.Context, olderThan *time.Time, perMatchFn func(mtch *MetaMatch, isSell bool) error) error
	// SetSeedGenerationTime stores the time when the app seed was generated.
	SetSeedGenerationTime(time uint64) error
	// SeedGenerationTime fetches the time when the app seed was generated.
	SeedGenerationTime() (uint64, error)
	// DisabledRateSources retrieves disabled fiat rate sources from the
	// database.
	DisabledRateSources() ([]string, error)
	// SaveDisabledRateSources saves disabled fiat rate sources in the database.
	// A source name must not contain a comma.
	SaveDisabledRateSources(disabledSources []string) error
}
