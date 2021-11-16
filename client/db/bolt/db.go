// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package bolt

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"decred.org/dcrdex/client/db"
	dexdb "decred.org/dcrdex/client/db"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/encrypt"
	"decred.org/dcrdex/dex/order"
	"go.etcd.io/bbolt"
)

// getCopy returns a copy of the value for the given key and provided bucket. If
// the key is not found or if an empty slice was loaded, nil is returned. Thus,
// use bkt.Get(key) == nil directly to test for existence of the key. This
// function should be used instead of bbolt.(*Bucket).Get when the read value
// needs to be kept after the transaction, at which time the buffer from Get is
// no longer safe to use.
func getCopy(bkt *bbolt.Bucket, key []byte) []byte {
	b := bkt.Get(key)
	if len(b) == 0 {
		return nil
	}
	val := make([]byte, len(b))
	copy(val, b)
	return val
}

// Short names for some commonly used imported functions.
var (
	intCoder    = encode.IntCoder
	bEqual      = bytes.Equal
	uint16Bytes = encode.Uint16Bytes
	uint32Bytes = encode.Uint32Bytes
	uint64Bytes = encode.Uint64Bytes
)

// Bolt works on []byte keys and values. These are some commonly used key and
// value encodings.
var (
	appBucket              = []byte("appBucket")
	accountsBucket         = []byte("accounts")
	disabledAccountsBucket = []byte("disabledAccounts")
	activeOrdersBucket     = []byte("activeOrders")
	archivedOrdersBucket   = []byte("orders")
	activeMatchesBucket    = []byte("activeMatches")
	archivedMatchesBucket  = []byte("matches")
	walletsBucket          = []byte("wallets")
	notesBucket            = []byte("notes")
	versionKey             = []byte("version")
	linkedKey              = []byte("linked")
	feeProofKey            = []byte("feecoin")
	statusKey              = []byte("status")
	baseKey                = []byte("base")
	quoteKey               = []byte("quote")
	orderKey               = []byte("order")
	matchKey               = []byte("match")
	orderIDKey             = []byte("orderID")
	matchIDKey             = []byte("matchID")
	proofKey               = []byte("proof")
	activeKey              = []byte("active")
	dexKey                 = []byte("dex")
	updateTimeKey          = []byte("utime")
	accountKey             = []byte("account")
	balanceKey             = []byte("balance")
	walletKey              = []byte("wallet")
	changeKey              = []byte("change")
	noteKey                = []byte("note")
	stampKey               = []byte("stamp")
	severityKey            = []byte("severity")
	ackKey                 = []byte("ack")
	swapFeesKey            = []byte("swapFees")
	maxFeeRateKey          = []byte("maxFeeRate")
	redemptionFeesKey      = []byte("redeemFees")
	typeKey                = []byte("type")
	credentialsBucket      = []byte("credentials")
	encSeedKey             = []byte("encSeed")
	encInnerKeyKey         = []byte("encInnerKey")
	innerKeyParamsKey      = []byte("innerKeyParams")
	outerKeyParamsKey      = []byte("outerKeyParams")
	legacyKeyParamsKey     = []byte("keyParams")
	fromVersionKey         = []byte("fromVersion")
	toVersionKey           = []byte("toVersion")
	byteTrue               = encode.ByteTrue
	backupDir              = "backup"
)

// BoltDB is a bbolt-based database backend for a DEX client. BoltDB satisfies
// the db.DB interface defined at decred.org/dcrdex/client/db.
type BoltDB struct {
	*bbolt.DB
	log dex.Logger
}

// Check that BoltDB satisfies the db.DB interface.
var _ dexdb.DB = (*BoltDB)(nil)

// NewDB is a constructor for a *BoltDB.
func NewDB(dbPath string, logger dex.Logger) (dexdb.DB, error) {
	_, err := os.Stat(dbPath)
	isNew := os.IsNotExist(err)

	db, err := bbolt.Open(dbPath, 0600, &bbolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		return nil, err
	}

	// Release the file lock on exit.
	bdb := &BoltDB{
		DB:  db,
		log: logger,
	}

	if err = bdb.makeTopLevelBuckets([][]byte{
		appBucket, accountsBucket, disabledAccountsBucket,
		activeOrdersBucket, archivedOrdersBucket,
		activeMatchesBucket, archivedMatchesBucket,
		walletsBucket, notesBucket, credentialsBucket,
	}); err != nil {
		return nil, err
	}

	// If the db is a new one, initialize it with the current DB version.
	if isNew {
		err := bdb.DB.Update(func(dbTx *bbolt.Tx) error {
			bkt := dbTx.Bucket(appBucket)
			if bkt == nil {
				return fmt.Errorf("app bucket not found")
			}

			versionB := encode.Uint32Bytes(DBVersion)
			err := bkt.Put(versionKey, versionB)
			if err != nil {
				return err
			}

			bdb.log.Infof("creating new version %d database", DBVersion)

			return nil
		})
		if err != nil {
			return nil, err
		}

		return bdb, nil
	}

	return bdb, bdb.upgradeDB()
}

// Run waits for context cancellation and closes the database.
func (db *BoltDB) Run(ctx context.Context) {
	<-ctx.Done()
	err := db.Backup()
	if err != nil {
		db.log.Errorf("unable to backup database: %v", err)
	}
	db.Close()
}

// Recrypt re-encrypts the wallet passwords and account private keys. As a
// convenience, the provided *PrimaryCredentials are stored under the same
// transaction.
func (db *BoltDB) Recrypt(creds *dexdb.PrimaryCredentials, oldCrypter, newCrypter encrypt.Crypter) (walletUpdates map[uint32][]byte, acctUpdates map[string][]byte, err error) {
	if err := validateCreds(creds); err != nil {
		return nil, nil, err
	}

	walletUpdates = make(map[uint32][]byte)
	acctUpdates = make(map[string][]byte)

	return walletUpdates, acctUpdates, db.Update(func(tx *bbolt.Tx) error {
		// Updates accounts and wallets.
		wallets := tx.Bucket(walletsBucket)
		if wallets == nil {
			return fmt.Errorf("no wallets bucket")
		}

		accounts := tx.Bucket(accountsBucket)
		if accounts == nil {
			return fmt.Errorf("no accounts bucket")
		}

		if err := wallets.ForEach(func(wid, _ []byte) error {
			wBkt := wallets.Bucket(wid)
			w, err := makeWallet(wBkt)
			if err != nil {
				return err
			}
			if len(w.EncryptedPW) == 0 {
				return nil
			}

			pw, err := oldCrypter.Decrypt(w.EncryptedPW)
			if err != nil {
				return fmt.Errorf("Decrypt error: %w", err)
			}
			w.EncryptedPW, err = newCrypter.Encrypt(pw)
			if err != nil {
				return fmt.Errorf("Encrypt error: %w", err)
			}
			err = wBkt.Put(walletKey, w.Encode())
			if err != nil {
				return err
			}
			walletUpdates[w.AssetID] = w.EncryptedPW
			return nil

		}); err != nil {
			return fmt.Errorf("wallets update error: %w", err)
		}

		err = accounts.ForEach(func(hostB, _ []byte) error {
			acct := accounts.Bucket(hostB)
			if acct == nil {
				return fmt.Errorf("account bucket %s value not a nested bucket", string(hostB))
			}
			acctB := getCopy(acct, accountKey)
			if acctB == nil {
				return fmt.Errorf("empty account found for %s", string(hostB))
			}
			acctInfo, err := dexdb.DecodeAccountInfo(acctB)
			if err != nil {
				return err
			}
			if len(acctInfo.LegacyEncKey) == 0 {
				db.log.Warnf("no LegacyEncKey for %s during Recrypt?", string(hostB))
				return nil
			}
			privB, err := oldCrypter.Decrypt(acctInfo.LegacyEncKey)
			if err != nil {
				return err
			}

			acctInfo.LegacyEncKey, err = newCrypter.Encrypt(privB)
			if err != nil {
				return err
			}

			acctUpdates[acctInfo.Host] = acctInfo.LegacyEncKey
			return acct.Put(accountKey, acctInfo.Encode())
		})
		if err != nil {
			return fmt.Errorf("accounts update error: %w", err)
		}

		// Store the new credentials.
		return db.setCreds(tx, creds)
	})
}

// SetPrimaryCredentials validates and stores the PrimaryCredentials.
func (db *BoltDB) SetPrimaryCredentials(creds *dexdb.PrimaryCredentials) error {
	if err := validateCreds(creds); err != nil {
		return err
	}

	return db.Update(func(tx *bbolt.Tx) error {
		return db.setCreds(tx, creds)
	})
}

// validateCreds checks that the PrimaryCredentials fields are properly
// populated.
func validateCreds(creds *dexdb.PrimaryCredentials) error {
	if len(creds.EncSeed) == 0 {
		return errors.New("EncSeed not set")
	}
	if len(creds.EncInnerKey) == 0 {
		return errors.New("EncInnerKey not set")
	}
	if len(creds.InnerKeyParams) == 0 {
		return errors.New("InnerKeyParams not set")
	}
	if len(creds.OuterKeyParams) == 0 {
		return errors.New("OuterKeyParams not set")
	}
	return nil
}

// primaryCreds reconstructs the *PrimaryCredentials.
func (db *BoltDB) primaryCreds() (creds *dexdb.PrimaryCredentials, err error) {
	return creds, db.View(func(tx *bbolt.Tx) error {
		bkt := tx.Bucket(credentialsBucket)
		if bkt == nil {
			return fmt.Errorf("no credentials bucket")
		}
		if bkt.Stats().KeyN == 0 {
			return dexdb.ErrNoCredentials
		}
		creds = &dexdb.PrimaryCredentials{
			EncSeed:        getCopy(bkt, encSeedKey),
			EncInnerKey:    getCopy(bkt, encInnerKeyKey),
			InnerKeyParams: getCopy(bkt, innerKeyParamsKey),
			OuterKeyParams: getCopy(bkt, outerKeyParamsKey),
		}
		return nil
	})
}

// setCreds stores the *PrimaryCredentials.
func (db *BoltDB) setCreds(tx *bbolt.Tx, creds *dexdb.PrimaryCredentials) error {
	bkt := tx.Bucket(credentialsBucket)
	if bkt == nil {
		return fmt.Errorf("no credentials bucket")
	}
	return newBucketPutter(bkt).
		put(encSeedKey, creds.EncSeed).
		put(encInnerKeyKey, creds.EncInnerKey).
		put(innerKeyParamsKey, creds.InnerKeyParams).
		put(outerKeyParamsKey, creds.OuterKeyParams).
		err()
}

// PrimaryCredentials retrieves the *PrimaryCredentials, if they are stored. It
// is an error if none have been stored.
func (db *BoltDB) PrimaryCredentials() (creds *dexdb.PrimaryCredentials, err error) {
	return db.primaryCreds()
}

// ListAccounts returns a list of DEX URLs. The DB is designed to have a single
// account per DEX, so the account itself is identified by the DEX URL.
func (db *BoltDB) ListAccounts() ([]string, error) {
	var urls []string
	return urls, db.acctsView(func(accts *bbolt.Bucket) error {
		c := accts.Cursor()
		for acct, _ := c.First(); acct != nil; acct, _ = c.Next() {
			acctBkt := accts.Bucket(acct)
			if acctBkt == nil {
				return fmt.Errorf("account bucket %s value not a nested bucket", string(acct))
			}
			if bEqual(acctBkt.Get(activeKey), byteTrue) {
				urls = append(urls, string(acct))
			}
		}
		return nil
	})
}

// Accounts returns a list of DEX Accounts. The DB is designed to have a single
// account per DEX, so the account itself is identified by the DEX URL.
func (db *BoltDB) Accounts() ([]*dexdb.AccountInfo, error) {
	var accounts []*dexdb.AccountInfo
	return accounts, db.acctsView(func(accts *bbolt.Bucket) error {
		c := accts.Cursor()
		// key, _ := c.First()
		for acctKey, _ := c.First(); acctKey != nil; acctKey, _ = c.Next() {
			acct := accts.Bucket(acctKey)
			if acct == nil {
				return fmt.Errorf("account bucket %s value not a nested bucket", string(acctKey))
			}
			acctB := getCopy(acct, accountKey)
			if acctB == nil {
				return fmt.Errorf("empty account found for %s", string(acctKey))
			}
			acctInfo, err := dexdb.DecodeAccountInfo(acctB)
			if err != nil {
				return err
			}
			acctInfo.Paid = len(acct.Get(feeProofKey)) > 0
			accounts = append(accounts, acctInfo)
		}
		return nil
	})
}

// Account gets the AccountInfo associated with the specified DEX address.
func (db *BoltDB) Account(url string) (*dexdb.AccountInfo, error) {
	var acctInfo *dexdb.AccountInfo
	acctKey := []byte(url)
	return acctInfo, db.acctsView(func(accts *bbolt.Bucket) error {
		acct := accts.Bucket(acctKey)
		if acct == nil {
			return fmt.Errorf("account not found for %s", url)
		}
		acctB := getCopy(acct, accountKey)
		if acctB == nil {
			return fmt.Errorf("empty account found for %s", url)
		}
		var err error
		acctInfo, err = dexdb.DecodeAccountInfo(acctB)
		if err != nil {
			return err
		}
		acctInfo.Paid = len(acct.Get(feeProofKey)) > 0

		return nil
	})
}

// CreateAccount saves the AccountInfo. If an account already exists for this
// DEX, it will return an error.
func (db *BoltDB) CreateAccount(ai *dexdb.AccountInfo) error {
	if ai.Host == "" {
		return fmt.Errorf("empty host not allowed")
	}
	if ai.DEXPubKey == nil {
		return fmt.Errorf("nil DEXPubKey not allowed")
	}
	if len(ai.EncKey()) == 0 {
		return fmt.Errorf("zero-length EncKey not allowed")
	}
	return db.acctsUpdate(func(accts *bbolt.Bucket) error {
		acct, err := accts.CreateBucket([]byte(ai.Host))
		if err != nil {
			return fmt.Errorf("failed to create account bucket")
		}

		err = acct.Put(accountKey, ai.Encode())
		if err != nil {
			return fmt.Errorf("accountKey put error: %w", err)
		}
		err = acct.Put(activeKey, byteTrue)
		if err != nil {
			return fmt.Errorf("activeKey put error: %w", err)
		}
		return nil
	})
}

// deleteAccount removes the account by host.
func (db *BoltDB) deleteAccount(host string) error {
	acctKey := []byte(host)
	return db.acctsUpdate(func(accts *bbolt.Bucket) error {
		return accts.DeleteBucket(acctKey)
	})
}

// DisableAccount disables the account associated with the given host
// and archives it. The Accounts and Account methods will no longer find
// the disabled account.
//
// TODO: Add disabledAccounts method for retrieval of a disabled account and
// possible recovery of the account data.
func (db *BoltDB) DisableAccount(url string) error {
	// Get account's info.
	ai, err := db.Account(url)
	if err != nil {
		return err
	}
	// Copy AccountInfo to disabledAccounts.
	err = db.disabledAcctsUpdate(func(disabledAccounts *bbolt.Bucket) error {
		return disabledAccounts.Put(ai.EncKey(), ai.Encode())
	})
	if err != nil {
		return err
	}
	// WARNING/TODO: account proof (fee paid info) not saved!
	err = db.deleteAccount(ai.Host)
	if err != nil {
		if errors.Is(err, bbolt.ErrBucketNotFound) {
			db.log.Warnf("Cannot delete account from active accounts"+
				" table. Host: not found. %s err: %v", ai.Host, err)
		} else {
			return err
		}
	}
	return nil
}

// disabledAccount gets the AccountInfo from disabledAccount associated with
// the specified EncKey.
func (db *BoltDB) disabledAccount(encKey []byte) (*dexdb.AccountInfo, error) {
	var acctInfo *dexdb.AccountInfo
	return acctInfo, db.disabledAcctsView(func(accts *bbolt.Bucket) error {
		acct := accts.Get(encKey)
		if acct == nil {
			return fmt.Errorf("account not found for key")
		}
		var err error
		acctInfo, err = dexdb.DecodeAccountInfo(acct)
		if err != nil {
			return err
		}
		return nil
	})
}

func (db *BoltDB) AccountProof(url string) (*dexdb.AccountProof, error) {
	var acctProof *dexdb.AccountProof
	acctKey := []byte(url)
	return acctProof, db.acctsView(func(accts *bbolt.Bucket) error {
		acct := accts.Bucket(acctKey)
		if acct == nil {
			return fmt.Errorf("account not found for %s", url)
		}
		var err error
		acctProof, err = dexdb.DecodeAccountProof(getCopy(acct, feeProofKey))
		if err != nil {
			return err
		}
		return nil
	})
}

// AccountPaid marks the account as paid by setting the "fee proof".
func (db *BoltDB) AccountPaid(proof *dexdb.AccountProof) error {
	acctKey := []byte(proof.Host)
	return db.acctsUpdate(func(accts *bbolt.Bucket) error {
		acct := accts.Bucket(acctKey)
		if acct == nil {
			return fmt.Errorf("account not found for %s", proof.Host)
		}
		return acct.Put(feeProofKey, proof.Encode())
	})
}

// acctsView is a convenience function for reading from the account bucket.
func (db *BoltDB) acctsView(f bucketFunc) error {
	return db.withBucket(accountsBucket, db.View, f)
}

// acctsUpdate is a convenience function for updating the account bucket.
func (db *BoltDB) acctsUpdate(f bucketFunc) error {
	return db.withBucket(accountsBucket, db.Update, f)
}

// disabledAcctsView is a convenience function for reading from the disabledAccounts bucket.
func (db *BoltDB) disabledAcctsView(f bucketFunc) error {
	return db.withBucket(disabledAccountsBucket, db.View, f)
}

// disabledAcctsUpdate is a convenience function for inserting into the disabledAccounts bucket.
func (db *BoltDB) disabledAcctsUpdate(f bucketFunc) error {
	return db.withBucket(disabledAccountsBucket, db.Update, f)
}

// UpdateOrder saves the order information in the database. Any existing order
// info for the same order ID will be overwritten without indication.
func (db *BoltDB) UpdateOrder(m *dexdb.MetaOrder) error {
	ord, md := m.Order, m.MetaData
	if md.Status == order.OrderStatusUnknown {
		return fmt.Errorf("cannot set order %s status to unknown", ord.ID())
	}
	if md.Host == "" {
		return fmt.Errorf("empty DEX not allowed")
	}
	if len(md.Proof.DEXSig) == 0 {
		return fmt.Errorf("cannot save order without DEX signature")
	}
	return db.ordersUpdate(func(ob, archivedOB *bbolt.Bucket) error {
		oid := ord.ID()

		// Create or move an order bucket based on order status. Active
		// orders go in the activeOrdersBucket. Inactive orders go in the
		// archivedOrdersBucket.
		bkt := ob
		whichBkt := "active"
		inactive := !md.Status.IsActive()
		if inactive {
			// No order means that it was never added to the active
			// orders bucket, or UpdateOrder was already called on
			// this order after it became inactive.
			if ob.Bucket(oid[:]) != nil {
				// This order now belongs in the archived orders
				// bucket. Delete it from active orders.
				if err := ob.DeleteBucket(oid[:]); err != nil {
					return fmt.Errorf("archived order bucket delete error: %w", err)
				}
			}
			bkt = archivedOB
			whichBkt = "archived"
		}
		oBkt, err := bkt.CreateBucketIfNotExists(oid[:])
		if err != nil {
			return fmt.Errorf("%s bucket error: %w", whichBkt, err)
		}

		var linkedB []byte
		if !md.LinkedOrder.IsZero() {
			linkedB = md.LinkedOrder[:]
		}

		return newBucketPutter(oBkt).
			put(baseKey, uint32Bytes(ord.Base())).
			put(quoteKey, uint32Bytes(ord.Quote())).
			put(statusKey, uint16Bytes(uint16(md.Status))).
			put(dexKey, []byte(md.Host)).
			put(updateTimeKey, uint64Bytes(timeNow())).
			put(proofKey, md.Proof.Encode()).
			put(changeKey, md.ChangeCoin).
			put(linkedKey, linkedB).
			put(typeKey, []byte{byte(ord.Type())}).
			put(orderKey, order.EncodeOrder(ord)).
			put(swapFeesKey, uint64Bytes(md.SwapFeesPaid)).
			put(maxFeeRateKey, uint64Bytes(md.MaxFeeRate)).
			put(redemptionFeesKey, uint64Bytes(md.RedemptionFeesPaid)).
			err()
	})
}

// ActiveOrders retrieves all orders which appear to be in an active state,
// which is either in the epoch queue or in the order book.
func (db *BoltDB) ActiveOrders() ([]*dexdb.MetaOrder, error) {
	return db.filteredOrders(func(_ *bbolt.Bucket) bool {
		return true
	}, false)
}

// AccountOrders retrieves all orders associated with the specified DEX. n = 0
// applies no limit on number of orders returned. since = 0 is equivalent to
// disabling the time filter, since no orders were created before before 1970.
func (db *BoltDB) AccountOrders(dex string, n int, since uint64) ([]*dexdb.MetaOrder, error) {
	dexB := []byte(dex)
	if n == 0 && since == 0 {
		return db.filteredOrders(func(oBkt *bbolt.Bucket) bool {
			return bEqual(dexB, oBkt.Get(dexKey))
		}, true)
	}
	sinceB := uint64Bytes(since)
	return db.newestOrders(n, func(_ []byte, oBkt *bbolt.Bucket) bool {
		timeB := oBkt.Get(updateTimeKey)
		return bEqual(dexB, oBkt.Get(dexKey)) && bytes.Compare(timeB, sinceB) >= 0
	}, true)
}

// MarketOrders retrieves all orders for the specified DEX and market. n = 0
// applies no limit on number of orders returned. since = 0 is equivalent to
// disabling the time filter, since no orders were created before before 1970.
func (db *BoltDB) MarketOrders(dex string, base, quote uint32, n int, since uint64) ([]*dexdb.MetaOrder, error) {
	dexB := []byte(dex)
	baseB := uint32Bytes(base)
	quoteB := uint32Bytes(quote)
	if n == 0 && since == 0 {
		return db.marketOrdersAll(dexB, baseB, quoteB)
	}
	return db.marketOrdersSince(dexB, baseB, quoteB, n, since)
}

// ActiveDEXOrders retrieves all orders for the specified DEX.
func (db *BoltDB) ActiveDEXOrders(dex string) ([]*dexdb.MetaOrder, error) {
	dexB := []byte(dex)
	return db.filteredOrders(func(oBkt *bbolt.Bucket) bool {
		return bEqual(dexB, oBkt.Get(dexKey))
	}, false)
}

// marketOrdersAll retrieves all orders for the specified DEX and market.
func (db *BoltDB) marketOrdersAll(dexB, baseB, quoteB []byte) ([]*dexdb.MetaOrder, error) {
	return db.filteredOrders(func(oBkt *bbolt.Bucket) bool {
		return bEqual(dexB, oBkt.Get(dexKey)) && bEqual(baseB, oBkt.Get(baseKey)) &&
			bEqual(quoteB, oBkt.Get(quoteKey))
	}, true)
}

// marketOrdersSince grabs market orders with optional filters for maximum count
// and age. The sort order is always newest first. If n is 0, there is no limit
// to the number of orders returned. If using with both n = 0, and since = 0,
// use marketOrdersAll instead.
func (db *BoltDB) marketOrdersSince(dexB, baseB, quoteB []byte, n int, since uint64) ([]*dexdb.MetaOrder, error) {
	sinceB := uint64Bytes(since)
	return db.newestOrders(n, func(_ []byte, oBkt *bbolt.Bucket) bool {
		timeB := oBkt.Get(updateTimeKey)
		return bEqual(dexB, oBkt.Get(dexKey)) && bEqual(baseB, oBkt.Get(baseKey)) &&
			bEqual(quoteB, oBkt.Get(quoteKey)) && bytes.Compare(timeB, sinceB) >= 0
	}, true)
}

// newestOrders returns the n newest orders, filtered with a supplied filter
// function. Each order's bucket is provided to the filter, and a boolean true
// return value indicates the order should is eligible to be decoded and
// returned.
func (db *BoltDB) newestOrders(n int, filter func([]byte, *bbolt.Bucket) bool, includeArchived bool) ([]*dexdb.MetaOrder, error) {
	orders := make([]*dexdb.MetaOrder, 0, n)
	return orders, db.ordersView(func(ob, archivedOB *bbolt.Bucket) error {
		buckets := []*bbolt.Bucket{ob}
		if includeArchived {
			buckets = append(buckets, archivedOB)
		}
		trios := newestBuckets(buckets, n, updateTimeKey, filter)
		for _, trio := range trios {
			o, err := decodeOrderBucket(trio.k, trio.b)
			if err != nil {
				return err
			}
			orders = append(orders, o)
		}
		return nil
	})
}

// filteredOrders gets all orders that pass the provided filter function. Each
// order's bucket is provided to the filter, and a boolean true return value
// indicates the order should be decoded and returned.
func (db *BoltDB) filteredOrders(filter func(*bbolt.Bucket) bool, includeArchived bool) ([]*dexdb.MetaOrder, error) {
	var orders []*dexdb.MetaOrder
	return orders, db.ordersView(func(ob, archivedOB *bbolt.Bucket) error {
		buckets := []*bbolt.Bucket{ob}
		if includeArchived {
			buckets = append(buckets, archivedOB)
		}
		for _, master := range buckets {
			err := master.ForEach(func(oid, _ []byte) error {
				oBkt := master.Bucket(oid)
				if oBkt == nil {
					return fmt.Errorf("order %x bucket is not a bucket", oid)
				}
				if filter(oBkt) {
					o, err := decodeOrderBucket(oid, oBkt)
					if err != nil {
						return err
					}
					orders = append(orders, o)
				}
				return nil
			})
			if err != nil {
				return err
			}
		}
		return nil
	})
}

// Order fetches a MetaOrder by order ID.
func (db *BoltDB) Order(oid order.OrderID) (mord *dexdb.MetaOrder, err error) {
	oidB := oid[:]
	err = db.ordersView(func(ob, archivedOB *bbolt.Bucket) error {
		oBkt := ob.Bucket(oidB)
		// If the order is not in the active bucket, check the archived
		// orders bucket.
		if oBkt == nil {
			oBkt = archivedOB.Bucket(oidB)
		}
		if oBkt == nil {
			return fmt.Errorf("order %s not found", oid)
		}
		var err error
		mord, err = decodeOrderBucket(oidB, oBkt)
		return err
	})
	return mord, err
}

// filterSet is a set of bucket filtering functions. Each function takes a
// bucket key and the associated bucket, and should return true if the bucket
// passes the filter.
type filterSet []func(oidB []byte, oBkt *bbolt.Bucket) bool

// check runs the bucket through all filters, and will return false if the
// bucket fails to pass any one filter.
func (fs filterSet) check(oidB []byte, oBkt *bbolt.Bucket) bool {
	for _, f := range fs {
		if !f(oidB, oBkt) {
			return false
		}
	}
	return true
}

// Orders fetches a slice of orders, sorted by descending time, and filtered
// with the provided OrderFilter. Orders does not return cancel orders.
func (db *BoltDB) Orders(orderFilter *db.OrderFilter) (ords []*dexdb.MetaOrder, err error) {
	// Default filter is just to exclude cancel orders.
	filters := filterSet{
		func(oidB []byte, oBkt *bbolt.Bucket) bool {
			oTypeB := oBkt.Get(typeKey)
			if len(oTypeB) != 1 {
				db.log.Error("encountered order type encoded with wrong number of bytes = %d for order %x", len(oTypeB), oidB)
				return false
			}
			oType := order.OrderType(oTypeB[0])
			return oType != order.CancelOrderType
		},
	}

	if len(orderFilter.Hosts) > 0 {
		hosts := make(map[string]bool, len(orderFilter.Hosts))
		for _, host := range orderFilter.Hosts {
			hosts[host] = true
		}
		filters = append(filters, func(_ []byte, oBkt *bbolt.Bucket) bool {
			return hosts[string(oBkt.Get(dexKey))]
		})
	}

	if len(orderFilter.Assets) > 0 {
		assetIDs := make(map[uint32]bool, len(orderFilter.Assets))
		for _, assetID := range orderFilter.Assets {
			assetIDs[assetID] = true
		}
		filters = append(filters, func(_ []byte, oBkt *bbolt.Bucket) bool {
			return assetIDs[intCoder.Uint32(oBkt.Get(baseKey))] || assetIDs[intCoder.Uint32(oBkt.Get(quoteKey))]
		})
	}

	includeArchived := true
	if len(orderFilter.Statuses) > 0 {
		filters = append(filters, func(_ []byte, oBkt *bbolt.Bucket) bool {
			status := order.OrderStatus(intCoder.Uint16(oBkt.Get(statusKey)))
			for _, acceptable := range orderFilter.Statuses {
				if status == acceptable {
					return true
				}
			}
			return false
		})
		includeArchived = false
		for _, status := range orderFilter.Statuses {
			if !status.IsActive() {
				includeArchived = true
				break
			}
		}
	}

	if !orderFilter.Offset.IsZero() {
		offsetOID := orderFilter.Offset
		var stampB []byte
		err := db.ordersView(func(ob, archivedOB *bbolt.Bucket) error {
			offsetBucket := ob.Bucket(offsetOID[:])
			// If the order is not in the active bucket, check the
			// archived orders bucket.
			if offsetBucket == nil {
				offsetBucket = archivedOB.Bucket(offsetOID[:])
			}
			if offsetBucket == nil {
				return fmt.Errorf("order %s not found", offsetOID)
			}
			stampB = getCopy(offsetBucket, updateTimeKey)
			return nil
		})
		if err != nil {
			return nil, err
		}

		filters = append(filters, func(oidB []byte, oBkt *bbolt.Bucket) bool {
			comp := bytes.Compare(oBkt.Get(updateTimeKey), stampB)
			return comp < 0 || (comp == 0 && bytes.Compare(offsetOID[:], oidB) < 0)
		})
	}

	return db.newestOrders(orderFilter.N, filters.check, includeArchived)
}

// decodeOrderBucket decodes the order's *bbolt.Bucket into a *MetaOrder.
func decodeOrderBucket(oid []byte, oBkt *bbolt.Bucket) (*dexdb.MetaOrder, error) {
	orderB := getCopy(oBkt, orderKey)
	if orderB == nil {
		return nil, fmt.Errorf("nil order bytes for order %x", oid)
	}
	ord, err := order.DecodeOrder(orderB)
	if err != nil {
		return nil, fmt.Errorf("error decoding order %x: %w", oid, err)
	}
	proofB := getCopy(oBkt, proofKey)
	if proofB == nil {
		return nil, fmt.Errorf("nil proof for order %x", oid)
	}
	proof, err := dexdb.DecodeOrderProof(proofB)
	if err != nil {
		return nil, fmt.Errorf("error decoding order proof for %x: %w", oid, err)
	}

	var linkedID order.OrderID
	copy(linkedID[:], oBkt.Get(linkedKey))

	// Old cancel orders may not have a maxFeeRate set since the v2 upgrade
	// doesn't set it for cancel orders.
	var maxFeeRate uint64
	if maxFeeRateB := oBkt.Get(maxFeeRateKey); len(maxFeeRateB) == 8 {
		maxFeeRate = intCoder.Uint64(maxFeeRateB)
	} else if ord.Type() != order.CancelOrderType {
		// Cancel orders should use zero, but trades need a non-zero value.
		maxFeeRate = ^uint64(0) // should not happen for trade orders after v2 upgrade
	}

	var fromVersion, toVersion uint32
	fromVersionB, toVersionB := oBkt.Get(fromVersionKey), oBkt.Get(toVersionKey)
	if len(fromVersionB) == 4 {
		fromVersion = intCoder.Uint32(fromVersionB)
	}
	if len(toVersionB) == 4 {
		toVersion = intCoder.Uint32(toVersionB)
	}

	return &dexdb.MetaOrder{
		MetaData: &dexdb.OrderMetaData{
			Proof:              *proof,
			Status:             order.OrderStatus(intCoder.Uint16(oBkt.Get(statusKey))),
			Host:               string(getCopy(oBkt, dexKey)),
			ChangeCoin:         getCopy(oBkt, changeKey),
			LinkedOrder:        linkedID,
			SwapFeesPaid:       intCoder.Uint64(oBkt.Get(swapFeesKey)),
			MaxFeeRate:         maxFeeRate,
			RedemptionFeesPaid: intCoder.Uint64(oBkt.Get(redemptionFeesKey)),
			FromVersion:        fromVersion,
			ToVersion:          toVersion,
		},
		Order: ord,
	}, nil
}

// updateOrderBucket expects an oid to exist in either the orders or archived
// order buckets. If status is not active, it first checks active orders. If
// found it moves the order to the archived bucket and returns that order
// bucket. If status is less than or equal to booked, it expects the order
// to already be in active orders and returns that bucket.
func updateOrderBucket(ob, archivedOB *bbolt.Bucket, oid order.OrderID, status order.OrderStatus) (*bbolt.Bucket, error) {
	if status == order.OrderStatusUnknown {
		return nil, fmt.Errorf("cannot set order %s status to unknown", oid)
	}
	inactive := !status.IsActive()
	if inactive {
		if bkt := ob.Bucket(oid[:]); bkt != nil {
			// It's in the active bucket. Move it to the archived bucket.
			oBkt, err := archivedOB.CreateBucket(oid[:])
			if err != nil {
				return nil, fmt.Errorf("unable to create archived order bucket: %v", err)
			}
			// Assume the order bucket contains only values, no
			// sub-buckets
			if err := bkt.ForEach(func(k, v []byte) error {
				return oBkt.Put(k, v)
			}); err != nil {
				return nil, fmt.Errorf("unable to copy active order bucket: %v", err)
			}
			if err := ob.DeleteBucket(oid[:]); err != nil {
				return nil, fmt.Errorf("unable to delete active order bucket: %v", err)
			}
			return oBkt, nil
		}
		// It's not in the active bucket, check archived.
		oBkt := archivedOB.Bucket(oid[:])
		if oBkt == nil {
			return nil, fmt.Errorf("archived order %s not found", oid)
		}
		return oBkt, nil
	}
	// Active status should be in the active bucket.
	oBkt := ob.Bucket(oid[:])
	if oBkt == nil {
		return nil, fmt.Errorf("active order %s not found", oid)
	}
	return oBkt, nil
}

// UpdateOrderMetaData updates the order metadata, not including the Host.
func (db *BoltDB) UpdateOrderMetaData(oid order.OrderID, md *db.OrderMetaData) error {
	return db.ordersUpdate(func(ob, archivedOB *bbolt.Bucket) error {
		oBkt, err := updateOrderBucket(ob, archivedOB, oid, md.Status)
		if err != nil {
			return fmt.Errorf("UpdateOrderMetaData: %w", err)
		}

		var linkedB []byte
		if !md.LinkedOrder.IsZero() {
			linkedB = md.LinkedOrder[:]
		}

		return newBucketPutter(oBkt).
			put(statusKey, uint16Bytes(uint16(md.Status))).
			put(updateTimeKey, uint64Bytes(timeNow())).
			put(proofKey, md.Proof.Encode()).
			put(changeKey, md.ChangeCoin).
			put(linkedKey, linkedB).
			put(swapFeesKey, uint64Bytes(md.SwapFeesPaid)).
			put(maxFeeRateKey, uint64Bytes(md.MaxFeeRate)).
			put(redemptionFeesKey, uint64Bytes(md.RedemptionFeesPaid)).
			put(fromVersionKey, uint32Bytes(md.FromVersion)).
			put(toVersionKey, uint32Bytes(md.ToVersion)).
			err()
	})
}

// UpdateOrderStatus sets the order status for an order.
func (db *BoltDB) UpdateOrderStatus(oid order.OrderID, status order.OrderStatus) error {
	return db.ordersUpdate(func(ob, archivedOB *bbolt.Bucket) error {
		oBkt, err := updateOrderBucket(ob, archivedOB, oid, status)
		if err != nil {
			return fmt.Errorf("UpdateOrderStatus: %w", err)
		}
		return oBkt.Put(statusKey, uint16Bytes(uint16(status)))
	})
}

// LinkOrder sets the linked order.
func (db *BoltDB) LinkOrder(oid, linkedID order.OrderID) error {
	return db.ordersUpdate(func(ob, archivedOB *bbolt.Bucket) error {
		oBkt := ob.Bucket(oid[:])
		// If the order is not in the active bucket, check the archived
		// orders bucket.
		if oBkt == nil {
			oBkt = archivedOB.Bucket(oid[:])
		}
		if oBkt == nil {
			return fmt.Errorf("LinkOrder - order %s not found", oid)
		}
		linkedB := linkedID[:]
		if linkedID.IsZero() {
			linkedB = nil
		}
		return oBkt.Put(linkedKey, linkedB)
	})
}

// ordersView is a convenience function for reading from the order buckets.
// Orders are spread over two buckets to make searching active orders faster.
// Any reads of the order buckets should be done in the same transaction, as
// orders may move from active to archived at any time.
func (db *BoltDB) ordersView(f func(ob, archivedOB *bbolt.Bucket) error) error {
	return db.View(func(tx *bbolt.Tx) error {
		ob := tx.Bucket(activeOrdersBucket)
		if ob == nil {
			return fmt.Errorf("failed to open %s bucket", string(activeOrdersBucket))
		}
		archivedOB := tx.Bucket(archivedOrdersBucket)
		if archivedOB == nil {
			return fmt.Errorf("failed to open %s bucket", string(archivedOrdersBucket))
		}
		return f(ob, archivedOB)
	})
}

// ordersUpdate is a convenience function for updating the order buckets.
// Orders are spread over two buckets to make searching active orders faster.
// Any writes of the order buckets should be done in the same transaction to
// ensure that reads can be kept concurrent.
func (db *BoltDB) ordersUpdate(f func(ob, archivedOB *bbolt.Bucket) error) error {
	return db.Update(func(tx *bbolt.Tx) error {
		ob := tx.Bucket(activeOrdersBucket)
		if ob == nil {
			return fmt.Errorf("failed to open %s bucket", string(activeOrdersBucket))
		}
		archivedOB := tx.Bucket(archivedOrdersBucket)
		if archivedOB == nil {
			return fmt.Errorf("failed to open %s bucket", string(archivedOrdersBucket))
		}
		return f(ob, archivedOB)
	})
}

// matchBucket gets a match's bucket in either the active or archived matches
// bucket. It will be created if it is not in either matches bucket. If an
// inactive match is found in the active bucket, it is moved to the archived
// matches bucket.
func matchBucket(mb, archivedMB *bbolt.Bucket, metaID []byte, active bool) (*bbolt.Bucket, error) {
	if active {
		// Active match would be in active bucket if it was previously inserted.
		return mb.CreateBucketIfNotExists(metaID) // might exist already
	}

	// Archived match may currently be in either active or archived bucket.
	mBkt := mb.Bucket(metaID) // try the active bucket first
	if mBkt == nil {
		// It's not in the active bucket, check/create in archived.
		return archivedMB.CreateBucketIfNotExists(metaID) // might exist already
	}

	// It's in the active bucket, but no longer active. Move it to the archived
	// bucket.
	newBkt, err := archivedMB.CreateBucketIfNotExists(metaID) // should not exist in both buckets though!
	if err != nil {
		return nil, fmt.Errorf("unable to create archived match bucket: %w", err)
	}
	// Assume the order bucket contains only values, no sub-buckets.
	if err := mBkt.ForEach(func(k, v []byte) error {
		return newBkt.Put(k, v)
	}); err != nil {
		return nil, fmt.Errorf("unable to copy active matches bucket: %w", err)
	}
	if err := mb.DeleteBucket(metaID); err != nil {
		return nil, fmt.Errorf("unable to delete active match bucket: %w", err)
	}
	return newBkt, nil
}

// UpdateMatch updates the match information in the database. Any existing
// entry for the same match ID will be overwritten without indication.
func (db *BoltDB) UpdateMatch(m *dexdb.MetaMatch) error {
	match, md := m.UserMatch, m.MetaData
	if md.Quote == md.Base {
		return fmt.Errorf("quote and base asset cannot be the same")
	}
	if md.DEX == "" {
		return fmt.Errorf("empty DEX not allowed")
	}
	return db.matchesUpdate(func(mb, archivedMB *bbolt.Bucket) error {
		metaID := m.MatchOrderUniqueID()
		active := dexdb.MatchIsActive(m.UserMatch, &m.MetaData.Proof)
		mBkt, err := matchBucket(mb, archivedMB, metaID, active)
		if err != nil {
			return err
		}

		return newBucketPutter(mBkt).
			put(baseKey, uint32Bytes(md.Base)).
			put(quoteKey, uint32Bytes(md.Quote)).
			put(statusKey, []byte{byte(match.Status)}).
			put(dexKey, []byte(md.DEX)).
			put(updateTimeKey, uint64Bytes(timeNow())).
			put(proofKey, md.Proof.Encode()).
			put(orderIDKey, match.OrderID[:]).
			put(matchIDKey, match.MatchID[:]).
			put(matchKey, order.EncodeMatch(match)).
			put(stampKey, uint64Bytes(md.Stamp)).
			err()
	})
}

// ActiveMatches retrieves the matches that are in an active state, which is
// any match that is still active.
func (db *BoltDB) ActiveMatches() ([]*dexdb.MetaMatch, error) {
	return db.filteredMatches(
		func(mBkt *bbolt.Bucket) bool {
			return true // all matches in the bucket
		},
		true,  // don't bother with cancel matches that are never active
		false, // exclude archived matches
	)
}

// DEXOrdersWithActiveMatches retrieves order IDs for any order that has active
// matches, regardless of whether the order itself is in an active state.
func (db *BoltDB) DEXOrdersWithActiveMatches(dex string) ([]order.OrderID, error) {
	dexB := []byte(dex)
	// For each match for this DEX, pick the active ones.
	idMap := make(map[order.OrderID]bool)
	err := db.matchesView(func(ob, _ *bbolt.Bucket) error { // only the active matches bucket is used
		return ob.ForEach(func(k, _ []byte) error {
			mBkt := ob.Bucket(k)
			if mBkt == nil {
				return fmt.Errorf("match %x bucket is not a bucket", k)
			}
			if !bytes.Equal(dexB, mBkt.Get(dexKey)) {
				return nil
			}

			oidB := mBkt.Get(orderIDKey)
			var oid order.OrderID
			copy(oid[:], oidB)
			idMap[oid] = true
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	ids := make([]order.OrderID, 0, len(idMap))
	for id := range idMap {
		ids = append(ids, id)
	}
	return ids, nil

}

// MatchesForOrder retrieves the matches for the specified order ID.
func (db *BoltDB) MatchesForOrder(oid order.OrderID, excludeCancels bool) ([]*dexdb.MetaMatch, error) {
	oidB := oid[:]
	return db.filteredMatches(func(mBkt *bbolt.Bucket) bool {
		oid := mBkt.Get(orderIDKey)
		return bytes.Equal(oid, oidB)
	}, excludeCancels, true) // include archived matches
}

// filteredMatches gets all matches that pass the provided filter function. Each
// match's bucket is provided to the filter, and a boolean true return value
// indicates the match should be decoded and returned. Matches with cancel
// orders may be excluded, a separate option so the filter function does not
// need to load and decode the matchKey value.
func (db *BoltDB) filteredMatches(filter func(*bbolt.Bucket) bool, excludeCancels, includeArchived bool) ([]*dexdb.MetaMatch, error) {
	var matches []*dexdb.MetaMatch
	return matches, db.matchesView(func(mb, archivedMB *bbolt.Bucket) error {
		buckets := []*bbolt.Bucket{mb}
		if includeArchived {
			buckets = append(buckets, archivedMB)
		}
		for _, master := range buckets {
			err := master.ForEach(func(k, _ []byte) error {
				mBkt := master.Bucket(k)
				if mBkt == nil {
					return fmt.Errorf("match %x bucket is not a bucket", k)
				}
				if !filter(mBkt) {
					return nil
				}
				match, err := loadMatchBucket(mBkt, excludeCancels)
				if err != nil {
					return fmt.Errorf("loading match %x bucket: %w", k, err)
				}
				if match != nil {
					matches = append(matches, match)
				}
				return nil
			})
			if err != nil {
				return err
			}
		}
		return nil
	})
}

func loadMatchBucket(mBkt *bbolt.Bucket, excludeCancels bool) (*dexdb.MetaMatch, error) {
	var proof *dexdb.MatchProof
	matchB := getCopy(mBkt, matchKey)
	if matchB == nil {
		return nil, fmt.Errorf("nil match bytes")
	}
	match, err := order.DecodeMatch(matchB)
	if err != nil {
		return nil, fmt.Errorf("error decoding match: %w", err)
	}
	// A cancel match for a maker (trade) order has an empty address.
	if excludeCancels && match.Address == "" {
		return nil, nil
	}
	proofB := getCopy(mBkt, proofKey)
	if len(proofB) == 0 {
		return nil, fmt.Errorf("empty proof")
	}
	proof, _, err = dexdb.DecodeMatchProof(proofB)
	if err != nil {
		return nil, fmt.Errorf("error decoding proof: %w", err)
	}
	// A cancel match for a taker (the cancel) order is complete with no
	// InitSig. Unfortunately, the trade Address was historically set.
	if excludeCancels && (len(proof.Auth.InitSig) == 0 && match.Status == order.MatchComplete) {
		return nil, nil
	}
	return &dexdb.MetaMatch{
		MetaData: &dexdb.MatchMetaData{
			Proof: *proof,
			DEX:   string(getCopy(mBkt, dexKey)),
			Base:  intCoder.Uint32(mBkt.Get(baseKey)),
			Quote: intCoder.Uint32(mBkt.Get(quoteKey)),
			Stamp: intCoder.Uint64(mBkt.Get(stampKey)),
		},
		UserMatch: match,
	}, nil
}

// matchesView is a convenience function for reading from the match bucket.
func (db *BoltDB) matchesView(f func(mb, archivedMB *bbolt.Bucket) error) error {
	return db.View(func(tx *bbolt.Tx) error {
		mb := tx.Bucket(activeMatchesBucket)
		if mb == nil {
			return fmt.Errorf("failed to open %s bucket", string(activeMatchesBucket))
		}
		archivedMB := tx.Bucket(archivedMatchesBucket)
		if archivedMB == nil {
			return fmt.Errorf("failed to open %s bucket", string(archivedMatchesBucket))
		}
		return f(mb, archivedMB)
	})
}

// matchesUpdate is a convenience function for updating the match bucket.
func (db *BoltDB) matchesUpdate(f func(mb, archivedMB *bbolt.Bucket) error) error {
	return db.Update(func(tx *bbolt.Tx) error {
		mb := tx.Bucket(activeMatchesBucket)
		if mb == nil {
			return fmt.Errorf("failed to open %s bucket", string(activeMatchesBucket))
		}
		archivedMB := tx.Bucket(archivedMatchesBucket)
		if archivedMB == nil {
			return fmt.Errorf("failed to open %s bucket", string(archivedMatchesBucket))
		}
		return f(mb, archivedMB)
	})
}

// UpdateWallet adds a wallet to the database.
func (db *BoltDB) UpdateWallet(wallet *dexdb.Wallet) error {
	if wallet.Balance == nil {
		return fmt.Errorf("cannot UpdateWallet with nil Balance field")
	}
	return db.walletsUpdate(func(master *bbolt.Bucket) error {
		wBkt, err := master.CreateBucketIfNotExists(wallet.ID())
		if err != nil {
			return err
		}
		err = wBkt.Put(walletKey, wallet.Encode())
		if err != nil {
			return err
		}
		return wBkt.Put(balanceKey, wallet.Balance.Encode())
	})
}

// SetWalletPassword set the encrypted password field for the wallet.
func (db *BoltDB) SetWalletPassword(wid []byte, newEncPW []byte) error {
	return db.walletsUpdate(func(master *bbolt.Bucket) error {
		wBkt := master.Bucket(wid)
		if wBkt == nil {
			return fmt.Errorf("wallet with ID is %x not known", wid)
		}
		b := getCopy(wBkt, walletKey)
		if b == nil {
			return fmt.Errorf("no wallet found in bucket")
		}
		wallet, err := dexdb.DecodeWallet(b)
		if err != nil {
			return err
		}
		wallet.EncryptedPW = make([]byte, len(newEncPW))
		copy(wallet.EncryptedPW, newEncPW)
		// No need to populate wallet.Balance since it's not part of the
		// serialization stored in the walletKey sub-bucket.

		return wBkt.Put(walletKey, wallet.Encode())
	})
}

// UpdateBalance updates balance in the wallet bucket.
func (db *BoltDB) UpdateBalance(wid []byte, bal *db.Balance) error {
	return db.walletsUpdate(func(master *bbolt.Bucket) error {
		wBkt := master.Bucket(wid)
		if wBkt == nil {
			return fmt.Errorf("wallet %x bucket is not a bucket", wid)
		}
		return wBkt.Put(balanceKey, bal.Encode())
	})
}

// Wallets loads all wallets from the database.
func (db *BoltDB) Wallets() ([]*dexdb.Wallet, error) {
	var wallets []*dexdb.Wallet
	return wallets, db.walletsView(func(master *bbolt.Bucket) error {
		c := master.Cursor()
		// key, _ := c.First()
		for wid, _ := c.First(); wid != nil; wid, _ = c.Next() {
			w, err := makeWallet(master.Bucket(wid))
			if err != nil {
				return err
			}
			wallets = append(wallets, w)
		}
		return nil
	})
}

// Wallet loads all wallet from the database.
func (db *BoltDB) Wallet(wid []byte) (wallet *dexdb.Wallet, err error) {
	return wallet, db.walletsView(func(master *bbolt.Bucket) error {
		wallet, err = makeWallet(master.Bucket(wid))
		return err
	})
}

func makeWallet(wBkt *bbolt.Bucket) (*dexdb.Wallet, error) {
	if wBkt == nil {
		return nil, fmt.Errorf("wallets bucket value not a nested bucket")
	}
	b := getCopy(wBkt, walletKey)
	if b == nil {
		return nil, fmt.Errorf("no wallet found in bucket")
	}

	w, err := dexdb.DecodeWallet(b)
	if err != nil {
		return nil, fmt.Errorf("DecodeWallet error: %w", err)
	}

	balB := getCopy(wBkt, balanceKey)
	if balB != nil {
		bal, err := db.DecodeBalance(balB)
		if err != nil {
			return nil, fmt.Errorf("DecodeBalance error: %w", err)
		}
		w.Balance = bal
	}
	return w, nil
}

// walletsView is a convenience function for reading from the wallets bucket.
func (db *BoltDB) walletsView(f bucketFunc) error {
	return db.withBucket(walletsBucket, db.View, f)
}

// walletUpdate is a convenience function for updating the wallets bucket.
func (db *BoltDB) walletsUpdate(f bucketFunc) error {
	return db.withBucket(walletsBucket, db.Update, f)
}

// SaveNotification saves the notification.
func (db *BoltDB) SaveNotification(note *dexdb.Notification) error {
	if note.Severeness < dexdb.Success {
		return fmt.Errorf("storage of notification with severity %s is forbidden", note.Severeness)
	}
	return db.notesUpdate(func(master *bbolt.Bucket) error {
		noteB := note.Encode()
		k := note.ID()
		noteBkt, err := master.CreateBucketIfNotExists(k)
		if err != nil {
			return err
		}
		err = noteBkt.Put(stampKey, uint64Bytes(note.TimeStamp))
		if err != nil {
			return err
		}
		err = noteBkt.Put(severityKey, []byte{byte(note.Severeness)})
		if err != nil {
			return err
		}
		return noteBkt.Put(noteKey, noteB)
	})
}

// AckNotification sets the acknowledgement for a notification.
func (db *BoltDB) AckNotification(id []byte) error {
	return db.notesUpdate(func(master *bbolt.Bucket) error {
		noteBkt := master.Bucket(id)
		if noteBkt == nil {
			return fmt.Errorf("notification not found")
		}
		return noteBkt.Put(ackKey, byteTrue)
	})
}

// NotificationsN reads out the N most recent notifications.
func (db *BoltDB) NotificationsN(n int) ([]*dexdb.Notification, error) {
	notes := make([]*dexdb.Notification, 0, n)
	return notes, db.notesView(func(master *bbolt.Bucket) error {
		trios := newestBuckets([]*bbolt.Bucket{master}, n, stampKey, nil)
		for _, trio := range trios {
			note, err := dexdb.DecodeNotification(getCopy(trio.b, noteKey))
			if err != nil {
				return err
			}
			note.Ack = bEqual(trio.b.Get(ackKey), byteTrue)
			note.Id = note.ID()
			notes = append(notes, note)
		}
		return nil
	})
}

// notesView is a convenience function to read from the notifications bucket.
func (db *BoltDB) notesView(f bucketFunc) error {
	return db.withBucket(notesBucket, db.View, f)
}

// notesUpdate is a convenience function for updating the notifications bucket.
func (db *BoltDB) notesUpdate(f bucketFunc) error {
	return db.withBucket(notesBucket, db.Update, f)
}

// newest buckets gets the nested buckets with the hightest timestamp from the
// specified master buckets. The nested bucket should have an encoded uint64 at
// the timeKey. An optional filter function can be used to reject buckets.
func newestBuckets(buckets []*bbolt.Bucket, n int, timeKey []byte, filter func([]byte, *bbolt.Bucket) bool) []*keyTimeTrio {
	idx := newTimeIndexNewest(n)
	for _, master := range buckets {
		master.ForEach(func(k, _ []byte) error {
			bkt := master.Bucket(k)
			stamp := intCoder.Uint64(bkt.Get(timeKey))
			if filter == nil || filter(k, bkt) {
				idx.add(stamp, k, bkt)
			}
			return nil
		})
	}
	return idx.trios
}

// makeTopLevelBuckets creates a top-level bucket for each of the provided keys,
// if the bucket doesn't already exist.
func (db *BoltDB) makeTopLevelBuckets(buckets [][]byte) error {
	return db.Update(func(tx *bbolt.Tx) error {
		for _, bucket := range buckets {
			_, err := tx.CreateBucketIfNotExists(bucket)
			if err != nil {
				return err
			}
		}

		return nil
	})
}

// withBucket is a creates a view into a (probably nested) bucket. The viewer
// can be read-only (db.View), or read-write (db.Update). The provided
// bucketFunc will be called with the requested bucket as its only argument.
func (db *BoltDB) withBucket(bkt []byte, viewer txFunc, f bucketFunc) error {
	return viewer(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(bkt)
		if bucket == nil {
			return fmt.Errorf("failed to open %s bucket", string(bkt))
		}
		return f(bucket)
	})
}

// Backup makes a copy of the database.
func (db *BoltDB) Backup() error {
	return db.backup("")
}

// backup makes a copy of the database to the specified file name in the backup
// subfolder of the current DB file's folder. If fileName is empty, the current
// DB's file name is used.
func (db *BoltDB) backup(fileName string) error {
	dir := filepath.Join(filepath.Dir(db.Path()), backupDir)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		err := os.Mkdir(dir, 0700)
		if err != nil {
			return fmt.Errorf("unable to create backup directory: %w", err)
		}
	}

	if fileName == "" {
		fileName = filepath.Base(db.Path())
	}
	path := filepath.Join(dir, fileName)
	err := db.View(func(tx *bbolt.Tx) error {
		return tx.CopyFile(path, 0600)
	})
	return err
}

// bucketPutter enables chained calls to (*bbolt.Bucket).Put with error
// deferment.
type bucketPutter struct {
	bucket *bbolt.Bucket
	putErr error
}

// newBucketPutter is a constructor for a bucketPutter.
func newBucketPutter(bkt *bbolt.Bucket) *bucketPutter {
	return &bucketPutter{bucket: bkt}
}

// put calls Put on the underlying bucket. If an error has been encountered in a
// previous call to push, nothing is done. put enables the *bucketPutter to
// enable chaining.
func (bp *bucketPutter) put(k, v []byte) *bucketPutter {
	if bp.putErr != nil {
		return bp
	}
	bp.putErr = bp.bucket.Put(k, v)
	return bp
}

// Return any push error encountered.
func (bp *bucketPutter) err() error {
	return bp.putErr
}

// keyTimeTrio is used to build an on-the-fly time-sorted index.
type keyTimeTrio struct {
	k []byte
	t uint64
	b *bbolt.Bucket
}

// timeIndexNewest is a struct used to build an index of sorted keyTimeTrios.
// The index can have a maximum capacity. If the capacity is set to zero, the
// index size is unlimited.
type timeIndexNewest struct {
	trios []*keyTimeTrio
	cap   int
}

// Create a new *timeIndexNewest, with the specified capacity.
func newTimeIndexNewest(n int) *timeIndexNewest {
	return &timeIndexNewest{
		trios: make([]*keyTimeTrio, 0, n),
		cap:   n,
	}
}

// Conditionally add a time-key trio to the index. The trio will only be added
// if the timeIndexNewest is under capacity and the time t is larger than the
// oldest trio's time.
func (idx *timeIndexNewest) add(t uint64, k []byte, b *bbolt.Bucket) {
	count := len(idx.trios)
	if idx.cap == 0 || count < idx.cap {
		idx.trios = append(idx.trios, &keyTimeTrio{
			// Need to make a copy, and []byte(k) upsets the linter.
			k: append([]byte(nil), k...),
			t: t,
			b: b,
		})
	} else {
		// non-zero length, at capacity.
		if t <= idx.trios[count-1].t {
			// Too old. Discard.
			return
		}
		idx.trios[count-1] = &keyTimeTrio{
			k: append([]byte(nil), k...),
			t: t,
			b: b,
		}
	}
	sort.Slice(idx.trios, func(i, j int) bool {
		// newest (highest t) first, or by lexicographically by key if the times
		// are equal.
		t1, t2 := idx.trios[i].t, idx.trios[j].t
		return t1 > t2 || (t1 == t2 && bytes.Compare(idx.trios[i].k, idx.trios[j].k) == 1)
	})
}

// timeNow is the current unix timestamp in milliseconds.
func timeNow() uint64 {
	return encode.UnixMilliU(time.Now())
}

// A couple of common bbolt functions.
type bucketFunc func(*bbolt.Bucket) error
type txFunc func(func(*bbolt.Tx) error) error
