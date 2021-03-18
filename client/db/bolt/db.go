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
	ordersBucket           = []byte("orders")
	matchesBucket          = []byte("matches")
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
	byteTrue               = encode.ByteTrue
	byteFalse              = encode.ByteFalse
	byteEpoch              = uint16Bytes(uint16(order.OrderStatusEpoch))
	byteBooked             = uint16Bytes(uint16(order.OrderStatusBooked))
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

	err = bdb.makeTopLevelBuckets([][]byte{appBucket, accountsBucket, disabledAccountsBucket,
		ordersBucket, matchesBucket, walletsBucket, notesBucket})
	if err != nil {
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

// Store stores a value at the specified key in the general-use bucket.
func (db *BoltDB) Store(k string, v []byte) error {
	if len(k) == 0 {
		return fmt.Errorf("cannot store with empty key")
	}
	keyB := []byte(k)
	return db.Update(func(tx *bbolt.Tx) error {
		bucket, err := tx.CreateBucketIfNotExists(appBucket)
		if err != nil {
			return fmt.Errorf("failed to create key bucket")
		}
		return bucket.Put(keyB, v)
	})
}

// ValueExists checks if a value was previously stored in the general-use
// bucket at the specified key.
func (db *BoltDB) ValueExists(k string) (bool, error) {
	var exists bool
	return exists, db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(appBucket)
		if bucket == nil {
			return fmt.Errorf("app bucket not found")
		}
		exists = bucket.Get([]byte(k)) != nil
		return nil
	})
}

// Get retrieves value previously stored with Store.
func (db *BoltDB) Get(k string) ([]byte, error) {
	var v []byte
	keyB := []byte(k)
	return v, db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(appBucket)
		if bucket == nil {
			return fmt.Errorf("app bucket not found")
		}
		vx := bucket.Get(keyB)
		if vx == nil {
			return fmt.Errorf("no value found for %s", k)
		}
		// An empty non-nil slice is returned nil without error.
		if len(vx) > 0 {
			v = encode.CopySlice(vx)
		}
		return nil
	})
}

// ListAccounts returns a list of DEX URLs. The DB is designed to have a single
// account per DEX, so the account itself is identified by the DEX URL.
func (db *BoltDB) ListAccounts() ([]string, error) {
	var urls []string
	return urls, db.acctsView(func(accts *bbolt.Bucket) error {
		c := accts.Cursor()
		// key, _ := c.First()
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
	if len(ai.EncKey) == 0 {
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

// DisableAccount account disables the given account and archives it. The
// Accounts and Account methods will no longer find the disabled account.
//
// TODO: Add disabledAccounts method for retrieval of a disabled account and
// possible recovery of the account data.
func (db *BoltDB) DisableAccount(ai *dexdb.AccountInfo) error {
	// Copy AccountInfo to disabledAccounts
	err := db.disabledAcctsUpdate(func(disabledAccounts *bbolt.Bucket) error {
		return disabledAccounts.Put(ai.EncKey, ai.Encode())
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
	if md.Host == "" {
		return fmt.Errorf("empty DEX not allowed")
	}
	if len(md.Proof.DEXSig) == 0 {
		return fmt.Errorf("cannot save order without DEX signature")
	}
	return db.ordersUpdate(func(master *bbolt.Bucket) error {
		oid := ord.ID()
		oBkt, err := master.CreateBucketIfNotExists(oid[:])
		if err != nil {
			return fmt.Errorf("order bucket error: %w", err)
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
	return db.filteredOrders(func(oBkt *bbolt.Bucket) bool {
		status := oBkt.Get(statusKey)
		return bEqual(status, byteEpoch) || bEqual(status, byteBooked)
	})
}

// AccountOrders retrieves all orders associated with the specified DEX. n = 0
// applies no limit on number of orders returned. since = 0 is equivalent to
// disabling the time filter, since no orders were created before before 1970.
func (db *BoltDB) AccountOrders(dex string, n int, since uint64) ([]*dexdb.MetaOrder, error) {
	dexB := []byte(dex)
	if n == 0 && since == 0 {
		return db.filteredOrders(func(oBkt *bbolt.Bucket) bool {
			return bEqual(dexB, oBkt.Get(dexKey))
		})
	}
	sinceB := uint64Bytes(since)
	return db.newestOrders(n, func(_ []byte, oBkt *bbolt.Bucket) bool {
		timeB := oBkt.Get(updateTimeKey)
		return bEqual(dexB, oBkt.Get(dexKey)) && bytes.Compare(timeB, sinceB) >= 0
	})
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
		status := oBkt.Get(statusKey)
		return bEqual(dexB, oBkt.Get(dexKey)) && (bEqual(status, byteEpoch) || bEqual(status, byteBooked))
	})
}

// marketOrdersAll retrieves all orders for the specified DEX and market.
func (db *BoltDB) marketOrdersAll(dexB, baseB, quoteB []byte) ([]*dexdb.MetaOrder, error) {
	return db.filteredOrders(func(oBkt *bbolt.Bucket) bool {
		return bEqual(dexB, oBkt.Get(dexKey)) && bEqual(baseB, oBkt.Get(baseKey)) &&
			bEqual(quoteB, oBkt.Get(quoteKey))
	})
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
	})
}

// newestOrders returns the n newest orders, filtered with a supplied filter
// function. Each order's bucket is provided to the filter, and a boolean true
// return value indicates the order should is eligible to be decoded and
// returned.
func (db *BoltDB) newestOrders(n int, filter func([]byte, *bbolt.Bucket) bool) ([]*dexdb.MetaOrder, error) {
	var orders []*dexdb.MetaOrder
	return orders, db.ordersView(func(master *bbolt.Bucket) error {
		pairs := newestBuckets(master, n, updateTimeKey, filter)
		for _, pair := range pairs {
			oBkt := master.Bucket(pair.k)
			o, err := decodeOrderBucket(pair.k, oBkt)
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
func (db *BoltDB) filteredOrders(filter func(*bbolt.Bucket) bool) ([]*dexdb.MetaOrder, error) {
	var orders []*dexdb.MetaOrder
	return orders, db.ordersView(func(master *bbolt.Bucket) error {
		return master.ForEach(func(oid, _ []byte) error {
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
	})
}

// Order fetches a MetaOrder by order ID.
func (db *BoltDB) Order(oid order.OrderID) (mord *dexdb.MetaOrder, err error) {
	oidB := oid[:]
	err = db.ordersView(func(master *bbolt.Bucket) error {
		oBkt := master.Bucket(oidB)
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
	}

	if !orderFilter.Offset.IsZero() {
		offsetOID := orderFilter.Offset[:]
		var offsetBucket *bbolt.Bucket
		var stampB []byte
		err := db.ordersView(func(master *bbolt.Bucket) error {
			offsetBucket = master.Bucket(offsetOID)
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
			return comp < 0 || (comp == 0 && bytes.Compare(offsetOID, oidB) < 0)
		})
	}

	return db.newestOrders(orderFilter.N, filters.check)
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

	return &dexdb.MetaOrder{
		MetaData: &dexdb.OrderMetaData{
			Proof:              *proof,
			Status:             order.OrderStatus(intCoder.Uint16(oBkt.Get(statusKey))),
			Host:               string(getCopy(oBkt, dexKey)),
			ChangeCoin:         getCopy(oBkt, changeKey),
			LinkedOrder:        linkedID,
			SwapFeesPaid:       intCoder.Uint64(oBkt.Get(swapFeesKey)),
			MaxFeeRate:         intCoder.Uint64(oBkt.Get(maxFeeRateKey)),
			RedemptionFeesPaid: intCoder.Uint64(oBkt.Get(redemptionFeesKey)),
		},
		Order: ord,
	}, nil
}

// UpdateOrderMetaData updates the order metadata, not including the Host.
func (db *BoltDB) UpdateOrderMetaData(oid order.OrderID, md *db.OrderMetaData) error {
	return db.ordersUpdate(func(master *bbolt.Bucket) error {
		oBkt := master.Bucket(oid[:])
		if oBkt == nil {
			return fmt.Errorf("UpdateOrderMetaData - order %s not found", oid)
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
			err()
	})
}

// UpdateOrderStatus sets the order status for an order.
func (db *BoltDB) UpdateOrderStatus(oid order.OrderID, status order.OrderStatus) error {
	return db.ordersUpdate(func(master *bbolt.Bucket) error {
		oBkt := master.Bucket(oid[:])
		if oBkt == nil {
			return fmt.Errorf("UpdateOrderStatus - order %s not found", oid)
		}
		return oBkt.Put(statusKey, uint16Bytes(uint16(status)))
	})
}

// LinkOrder sets the linked order.
func (db *BoltDB) LinkOrder(oid, linkedID order.OrderID) error {
	return db.ordersUpdate(func(master *bbolt.Bucket) error {
		oBkt := master.Bucket(oid[:])
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

// ordersView is a convenience function for reading from the order bucket.
func (db *BoltDB) ordersView(f bucketFunc) error {
	return db.withBucket(ordersBucket, db.View, f)
}

// ordersUpdate is a convenience function for updating the order bucket.
func (db *BoltDB) ordersUpdate(f bucketFunc) error {
	return db.withBucket(ordersBucket, db.Update, f)
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
	return db.matchesUpdate(func(master *bbolt.Bucket) error {
		metaID := m.MatchOrderUniqueID()
		mBkt, err := master.CreateBucketIfNotExists(metaID)
		if err != nil {
			return fmt.Errorf("order bucket error: %w", err)
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
// any state except order.MatchComplete.
func (db *BoltDB) ActiveMatches() ([]*dexdb.MetaMatch, error) {
	return db.filteredMatches(func(mBkt *bbolt.Bucket) bool {
		status := mBkt.Get(statusKey)
		return len(status) != 1 || status[0] != uint8(order.MatchComplete)
	})
}

// DEXOrdersWithActiveMatches retrieves order IDs for any order that has active
// matches, regardless of whether the order itself is in an active state.
func (db *BoltDB) DEXOrdersWithActiveMatches(dex string) ([]order.OrderID, error) {
	dexB := []byte(dex)
	// For each match for this DEX, pick the active ones.
	idMap := make(map[order.OrderID]bool)
	err := db.matchesView(func(master *bbolt.Bucket) error {
		return master.ForEach(func(k, _ []byte) error {
			mBkt := master.Bucket(k)
			if mBkt == nil {
				return fmt.Errorf("match %x bucket is not a bucket", k)
			}
			if !bytes.Equal(dexB, mBkt.Get(dexKey)) {
				return nil
			}
			// Inactive with MatchComplete status.
			statusB := mBkt.Get(statusKey)
			if len(statusB) != 1 {
				db.log.Errorf("match %x has no status set", k)
				return nil
			}
			status := order.MatchStatus(statusB[0])
			if status == order.MatchComplete {
				return nil
			}

			// Inactive if refunded.
			proofB := getCopy(mBkt, proofKey)
			if len(proofB) == 0 {
				db.log.Errorf("empty match proof")
				return nil
			}
			proof, errM := dexdb.DecodeMatchProof(proofB)
			if errM != nil {
				db.log.Errorf("error decoding proof: %v", errM)
				return nil
			}
			if len(proof.RefundCoin) > 0 {
				return nil
			}

			// Some revoked matches are inactive depending on match status and
			// party side. They may need to be refunded or redeemed first.
			// TakerSwapCast match status requires action on both sides.
			if proof.IsRevoked() && status != order.TakerSwapCast {
				// NewlyMatched requires no further action from either side.
				if status == order.NewlyMatched {
					return nil
				}

				// Load the UserMatch to check the match Side.
				matchB := mBkt.Get(matchKey) // no copy, just need Side
				if matchB == nil {
					db.log.Errorf("nil match bytes for %x", k)
					return nil
				}
				match, err := order.DecodeMatch(matchB)
				if err != nil {
					db.log.Errorf("error decoding match %x: %v", k, err)
					return nil
				}
				side := match.Side // done with match and matchB

				// MakerSwapCast requires no further action from the taker.
				// MakerRedeemed requires no further action from the maker.
				if (status == order.MakerSwapCast && side == order.Taker) ||
					(status == order.MakerRedeemed && side == order.Maker) {
					return nil
				}
			}

			// The match is active.
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
func (db *BoltDB) MatchesForOrder(oid order.OrderID) ([]*dexdb.MetaMatch, error) {
	oidB := oid[:]
	return db.filteredMatches(func(mBkt *bbolt.Bucket) bool {
		oid := mBkt.Get(orderIDKey)
		return bytes.Equal(oid, oidB)
	})
}

// filteredMatches gets all matches that pass the provided filter function. Each
// match's bucket is provided to the filter, and a boolean true return value
// indicates the match should be decoded and returned.
func (db *BoltDB) filteredMatches(filter func(*bbolt.Bucket) bool) ([]*dexdb.MetaMatch, error) {
	var matches []*dexdb.MetaMatch
	return matches, db.matchesView(func(master *bbolt.Bucket) error {
		return master.ForEach(func(k, _ []byte) error {
			mBkt := master.Bucket(k)
			if mBkt == nil {
				return fmt.Errorf("match %x bucket is not a bucket", k)
			}
			if filter(mBkt) {
				var proof *dexdb.MatchProof
				matchB := getCopy(mBkt, matchKey)
				if matchB == nil {
					return fmt.Errorf("nil match bytes for %x", k)
				}
				match, err := order.DecodeMatch(matchB)
				if err != nil {
					return fmt.Errorf("error decoding match %x: %w", k, err)
				}
				proofB := getCopy(mBkt, proofKey)
				if len(proofB) == 0 {
					return fmt.Errorf("empty proof")
				}
				proof, err = dexdb.DecodeMatchProof(proofB)
				if err != nil {
					return fmt.Errorf("error decoding proof: %w", err)
				}
				matches = append(matches, &dexdb.MetaMatch{
					MetaData: &dexdb.MatchMetaData{
						Proof: *proof,
						DEX:   string(getCopy(mBkt, dexKey)),
						Base:  intCoder.Uint32(mBkt.Get(baseKey)),
						Quote: intCoder.Uint32(mBkt.Get(quoteKey)),
						Stamp: intCoder.Uint64(mBkt.Get(stampKey)),
					},
					UserMatch: match,
				})
			}
			return nil
		})
	})
}

// matchesView is a convenience function for reading from the match bucket.
func (db *BoltDB) matchesView(f bucketFunc) error {
	return db.withBucket(matchesBucket, db.View, f)
}

// matchesUpdate is a convenience function for updating the match bucket.
func (db *BoltDB) matchesUpdate(f bucketFunc) error {
	return db.withBucket(matchesBucket, db.Update, f)
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
func (db *BoltDB) SetWalletPassword(wid []byte, newPW []byte) error {
	return db.walletsUpdate(func(master *bbolt.Bucket) error {
		wBkt := master.Bucket(wid)
		if wBkt == nil {
			return fmt.Errorf("wallet with ID is %x not known", wid)
		}
		wallet, err := makeWallet(wBkt)
		if err != nil {
			return err
		}
		wallet.EncryptedPW = newPW
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
		return nil, err
	}
	balB := getCopy(wBkt, balanceKey)
	if balB != nil {
		bal, err := db.DecodeBalance(balB)
		if err != nil {
			return nil, err
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
		return fmt.Errorf("Storage of notification with severity %s is forbidden.", note.Severeness)
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
		pairs := newestBuckets(master, n, stampKey, nil)
		for _, pair := range pairs {
			noteBkt := master.Bucket(pair.k)
			note, err := dexdb.DecodeNotification(getCopy(noteBkt, noteKey))
			if err != nil {
				return err
			}
			note.Ack = bEqual(noteBkt.Get(ackKey), byteTrue)
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

// Newest buckets gets the nested buckets with the hightest timestamp from the
// specified master bucket. The nested bucket should have an encoded uint64 at
// the timeKey. An optional filter function can be used to reject buckets.
func newestBuckets(master *bbolt.Bucket, n int, timeKey []byte, filter func([]byte, *bbolt.Bucket) bool) []*keyTimePair {
	idx := newTimeIndexNewest(n)
	master.ForEach(func(k, _ []byte) error {
		bkt := master.Bucket(k)
		stamp := intCoder.Uint64(bkt.Get(timeKey))
		if filter == nil || filter(k, bkt) {
			idx.add(stamp, k)
		}
		return nil
	})
	return idx.pairs
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

// keyTimePair is used to build an on-the-fly time-sorted index.
type keyTimePair struct {
	k []byte
	t uint64
}

// timeIndexNewest is a struct used to build an index of sorted keyTimePairs.
// The index can have a maximum capacity. If the capacity is set to zero, the
// index size is unlimited.
type timeIndexNewest struct {
	pairs []*keyTimePair
	cap   int
}

// Create a new *timeIndexNewest, with the specified capacity.
func newTimeIndexNewest(n int) *timeIndexNewest {
	return &timeIndexNewest{
		pairs: make([]*keyTimePair, 0, n),
		cap:   n,
	}
}

// Conditionally add a time-key pair to the index. The pair will only be added
// if the timeIndexNewest is under capacity and the time t is larger than the
// oldest pair's time.
func (idx *timeIndexNewest) add(t uint64, k []byte) {
	count := len(idx.pairs)
	if idx.cap == 0 || count < idx.cap {
		idx.pairs = append(idx.pairs, &keyTimePair{
			// Need to make a copy, and []byte(k) upsets the linter.
			k: append([]byte(nil), k...),
			t: t,
		})
	} else {
		// non-zero length, at capacity.
		if t <= idx.pairs[count-1].t {
			// Too old. Discard.
			return
		}
		idx.pairs[count-1] = &keyTimePair{
			k: append([]byte(nil), k...),
			t: t,
		}
	}
	sort.Slice(idx.pairs, func(i, j int) bool {
		// newest (highest t) first, or by lexicographically by key if the times
		// are equal.
		t1, t2 := idx.pairs[i].t, idx.pairs[j].t
		return t1 > t2 || (t1 == t2 && bytes.Compare(idx.pairs[i].k, idx.pairs[j].k) == 1)
	})
}

// timeNow is the current unix timestamp in milliseconds.
func timeNow() uint64 {
	return encode.UnixMilliU(time.Now())
}

// A couple of common bbolt functions.
type bucketFunc func(*bbolt.Bucket) error
type txFunc func(func(*bbolt.Tx) error) error
