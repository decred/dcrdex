// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package bolt

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"decred.org/dcrdex/client/db"
	dexdb "decred.org/dcrdex/client/db"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/order"
	"go.etcd.io/bbolt"
)

// Short names for some commonly used imported functions.
var (
	intCoder    = encode.IntCoder
	bEqual      = bytes.Equal
	uint16Bytes = encode.Uint16Bytes
	uint32Bytes = encode.Uint32Bytes
	uint64Bytes = encode.Uint64Bytes
	bCopy       = encode.CopySlice
)

// Bolt works on []byte keys and values. These are some commonly used key and
// value encodings.
var (
	appBucket      = []byte("appBucket")
	accountsBucket = []byte("accounts")
	ordersBucket   = []byte("orders")
	matchesBucket  = []byte("matches")
	walletsBucket  = []byte("wallets")
	notesBucket    = []byte("notes")
	feeProofKey    = []byte("feecoin")
	statusKey      = []byte("status")
	baseKey        = []byte("base")
	quoteKey       = []byte("quote")
	orderKey       = []byte("order")
	matchKey       = []byte("match")
	orderIDKey     = []byte("orderID")
	matchIDKey     = []byte("matchID")
	proofKey       = []byte("proof")
	activeKey      = []byte("active")
	dexKey         = []byte("dex")
	updateTimeKey  = []byte("utime")
	accountKey     = []byte("account")
	balanceKey     = []byte("balance")
	walletKey      = []byte("wallet")
	changeKey      = []byte("change")
	noteKey        = []byte("note")
	stampKey       = []byte("stamp")
	severityKey    = []byte("severity")
	ackKey         = []byte("ack")
	byteTrue       = encode.ByteTrue
	byteFalse      = encode.ByteFalse
	byteEpoch      = uint16Bytes(uint16(order.OrderStatusEpoch))
	byteBooked     = uint16Bytes(uint16(order.OrderStatusBooked))
	backupDir      = "backup"
)

// BoltDB is a bbolt-based database backend for a DEX client. BoltDB satisfies
// the db.DB interface defined at decred.org/dcrdex/client/db.
type BoltDB struct {
	*bbolt.DB
}

// Check that BoltDB satisfies the db.DB interface.
var _ dexdb.DB = (*BoltDB)(nil)

// NewDB is a constructor for a *BoltDB.
func NewDB(dbPath string) (dexdb.DB, error) {
	db, err := bbolt.Open(dbPath, 0600, &bbolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		return nil, err
	}

	// Release the file lock on exit.
	bdb := &BoltDB{
		DB: db,
	}

	return bdb, bdb.makeTopLevelBuckets([][]byte{appBucket, accountsBucket,
		ordersBucket, matchesBucket, walletsBucket, notesBucket})
}

// Run waits for context cancellation and closes the database.
func (db *BoltDB) Run(ctx context.Context) {
	<-ctx.Done()
	err := db.Backup()
	if err != nil {
		log.Errorf("unable to backup database: %v", err)
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
		v = bucket.Get(keyB)
		if v == nil {
			return fmt.Errorf("no value found for %s", k)
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
			acctB := acct.Get(accountKey)
			if acct == nil {
				return fmt.Errorf("empty account found for %s", string(acctKey))
			}
			acctInfo, err := dexdb.DecodeAccountInfo(bCopy(acctB))
			if err != nil {
				return err
			}
			acctInfo.Paid = len(acct.Get(feeProofKey)) > 0
			if acctInfo.Paid {
				acctInfo.AccountProof = acct.Get(feeProofKey)
			}
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
		acctB := acct.Get(accountKey)
		if acct == nil {
			return fmt.Errorf("empty account found for %s", url)
		}
		var err error
		acctInfo, err = dexdb.DecodeAccountInfo(bCopy(acctB))
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
			return fmt.Errorf("accountKey put error: %v", err)
		}
		err = acct.Put(activeKey, byteTrue)
		if err != nil {
			return fmt.Errorf("activeKey put error: %v", err)
		}
		return nil
	})
}

// AccountPaid marks the account as paid by setting the "fee proof".
func (db *BoltDB) AccountPaid(proof *msgjson.AccountProof) error {
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
			return fmt.Errorf("order bucket error: %v", err)
		}
		return newBucketPutter(oBkt).
			put(baseKey, uint32Bytes(ord.Base())).
			put(quoteKey, uint32Bytes(ord.Quote())).
			put(statusKey, uint16Bytes(uint16(md.Status))).
			put(dexKey, []byte(md.Host)).
			put(updateTimeKey, uint64Bytes(timeNow())).
			put(proofKey, md.Proof.Encode()).
			put(changeKey, md.ChangeCoin).
			put(orderKey, order.EncodeOrder(ord)).
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
	return db.newestOrders(n, func(oBkt *bbolt.Bucket) bool {
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
	return db.newestOrders(n, func(oBkt *bbolt.Bucket) bool {
		timeB := oBkt.Get(updateTimeKey)
		return bEqual(dexB, oBkt.Get(dexKey)) && bEqual(baseB, oBkt.Get(baseKey)) &&
			bEqual(quoteB, oBkt.Get(quoteKey)) && bytes.Compare(timeB, sinceB) >= 0
	})
}

// newestOrders returns the n newest orders, filtered with a supplied filter
// function. Each order's bucket is provided to the filter, and a boolean true
// return value indicates the order should is eligible to be decoded and
// returned.
func (db *BoltDB) newestOrders(n int, filter func(*bbolt.Bucket) bool) ([]*dexdb.MetaOrder, error) {
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
		master.ForEach(func(oid, _ []byte) error {
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
		return nil
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

// decodeOrderBucket decodes the order's *bbolt.Bucket into a *MetaOrder.
func decodeOrderBucket(oid []byte, oBkt *bbolt.Bucket) (*dexdb.MetaOrder, error) {
	orderB := oBkt.Get(orderKey)
	if orderB == nil {
		return nil, fmt.Errorf("nil order bytes for order %x", oid)
	}
	ord, err := order.DecodeOrder(bCopy(orderB))
	if err != nil {
		return nil, fmt.Errorf("error decoding order %x: %v", oid, err)
	}
	proofB := oBkt.Get(proofKey)
	if proofB == nil {
		return nil, fmt.Errorf("nil proof for order %x", oid)
	}
	proof, err := dexdb.DecodeOrderProof(bCopy(proofB))
	if err != nil {
		return nil, fmt.Errorf("error decoding order proof for %x: %v", oid, err)
	}
	return &dexdb.MetaOrder{
		MetaData: &dexdb.OrderMetaData{
			Proof:      *proof,
			Status:     order.OrderStatus(intCoder.Uint16(oBkt.Get(statusKey))),
			Host:       string(oBkt.Get(dexKey)),
			ChangeCoin: oBkt.Get(changeKey),
		},
		Order: ord,
	}, nil
}

// SetChangeCoin sets the current change coin on an order. Change coins are
// chained for matches, so only the last change coin needs to be saved.
func (db *BoltDB) SetChangeCoin(oid order.OrderID, changeCoin order.CoinID) error {
	return db.ordersUpdate(func(master *bbolt.Bucket) error {
		oBkt := master.Bucket(oid[:])
		if oBkt == nil {
			return fmt.Errorf("SetChangeCoin - order %s not found", oid)
		}
		return oBkt.Put(changeKey, changeCoin)
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
	match, md := m.Match, m.MetaData
	if md.Quote == md.Base {
		return fmt.Errorf("quote and base asset cannot be the same")
	}
	if md.DEX == "" {
		return fmt.Errorf("empty DEX not allowed")
	}
	return db.matchesUpdate(func(master *bbolt.Bucket) error {
		metaID := m.ID()
		mBkt, err := master.CreateBucketIfNotExists(metaID)
		if err != nil {
			return fmt.Errorf("order bucket error: %v", err)
		}
		return newBucketPutter(mBkt).
			put(baseKey, uint32Bytes(md.Base)).
			put(quoteKey, uint32Bytes(md.Quote)).
			put(statusKey, []byte{byte(md.Status)}).
			put(dexKey, []byte(md.DEX)).
			put(updateTimeKey, uint64Bytes(timeNow())).
			put(proofKey, md.Proof.Encode()).
			put(orderIDKey, match.OrderID[:]).
			put(matchIDKey, match.MatchID[:]).
			put(matchKey, order.EncodeMatch(match)).
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

// ActiveDEXMatches retrieves the matches that are in an active state for a
// specified DEX, which is any state except order.MatchComplete.
func (db *BoltDB) ActiveDEXMatches(dex string) ([]*dexdb.MetaMatch, error) {
	dexB := []byte(dex)
	return db.filteredMatches(func(mBkt *bbolt.Bucket) bool {
		status := mBkt.Get(statusKey)
		return bytes.Equal(dexB, mBkt.Get(dexKey)) && (len(status) != 1 || status[0] != uint8(order.MatchComplete))
	})
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
		master.ForEach(func(k, _ []byte) error {
			mBkt := master.Bucket(k)
			if mBkt == nil {
				return fmt.Errorf("match %x bucket is not a bucket", k)
			}
			if filter(mBkt) {
				var proof *dexdb.MatchProof
				matchB := mBkt.Get(matchKey)
				if matchB == nil {
					return fmt.Errorf("nil match bytes for %x", k)
				}
				match, err := order.DecodeMatch(bCopy(matchB))
				if err != nil {
					return fmt.Errorf("error decoding match %x: %v", k, err)
				}
				proofB := mBkt.Get(proofKey)
				if len(proofB) == 0 {
					return fmt.Errorf("empty proof")
				}
				proof, err = dexdb.DecodeMatchProof(bCopy(proofB))
				if err != nil {
					return fmt.Errorf("error decoding proof: %v", err)
				}
				statusB := mBkt.Get(statusKey)
				if len(statusB) != 1 {
					return fmt.Errorf("expected status length 1, got %d", len(statusB))
				}
				matches = append(matches, &dexdb.MetaMatch{
					MetaData: &dexdb.MatchMetaData{
						Proof:  *proof,
						Status: order.MatchStatus(statusB[0]),
						DEX:    string(mBkt.Get(dexKey)),
						Base:   intCoder.Uint32(mBkt.Get(baseKey)),
						Quote:  intCoder.Uint32(mBkt.Get(quoteKey)),
					},
					Match: match,
				})
			}
			return nil
		})
		return nil
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
	b := wBkt.Get(walletKey)
	if b == nil {
		return nil, fmt.Errorf("no wallet found in bucket")
	}
	w, err := dexdb.DecodeWallet(b)
	if err != nil {
		return nil, err
	}
	balB := wBkt.Get(balanceKey)
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
			note, err := dexdb.DecodeNotification(noteBkt.Get(noteKey))
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
func newestBuckets(master *bbolt.Bucket, n int, timeKey []byte, filter func(*bbolt.Bucket) bool) []*keyTimePair {
	idx := newTimeIndexNewest(n)
	master.ForEach(func(k, _ []byte) error {
		bkt := master.Bucket(k)
		timeB := bkt.Get(timeKey)
		stamp := intCoder.Uint64(timeB)
		if filter == nil || filter(bkt) {
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
	dir := filepath.Join(filepath.Dir(db.Path()), backupDir)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		err := os.Mkdir(dir, 0700)
		if err != nil {
			return fmt.Errorf("unable to create backup directory: %v", err)
		}
	}

	path := filepath.Join(dir, filepath.Base(db.Path()))
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
		return idx.pairs[i].t > idx.pairs[j].t
	})
}

// timeNow is the current unix timestamp in milliseconds.
func timeNow() uint64 {
	return encode.UnixMilliU(time.Now())
}

// A couple of common bbolt functions.
type bucketFunc func(*bbolt.Bucket) error
type txFunc func(func(*bbolt.Tx) error) error
