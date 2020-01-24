// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package bolt

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"time"

	dexdb "decred.org/dcrdex/client/db"
	"decred.org/dcrdex/dex/encode"
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
	encKeyKey      = []byte("enckey")
	feeProofKey    = []byte("feecoin")
	statusKey      = []byte("status")
	baseKey        = []byte("base")
	quoteKey       = []byte("quote")
	orderKey       = []byte("order")
	matchKey       = []byte("match")
	proofKey       = []byte("proof")
	activeKey      = []byte("active")
	dexKey         = []byte("dex")
	updateTimeKey  = []byte("utime")
	accountKey     = []byte("account")
	byteTrue       = encode.ByteTrue
	byteFalse      = encode.ByteFalse
	byteEpoch      = uint16Bytes(uint16(order.OrderStatusEpoch))
	byteBooked     = uint16Bytes(uint16(order.OrderStatusBooked))
)

// boltDB is a bbolt-based database backend for a DEX client. boltDB satisfies
// the db.DB interface defined at decred.org/dcrdex/client/db.
type boltDB struct {
	*bbolt.DB
}

// Check that boltDB satisfies the db.DB interface.
var _ dexdb.DB = (*boltDB)(nil)

// NewDB is a constructor for a *boltDB.
func NewDB(dbPath string) (dexdb.DB, error) {
	db, err := bbolt.Open(dbPath, 0600, &bbolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		return nil, err
	}

	// Release the file lock on exit.
	bdb := &boltDB{
		DB: db,
	}

	return bdb, bdb.makeTopLevelBuckets([][]byte{appBucket, accountsBucket,
		ordersBucket, matchesBucket, walletsBucket})
}

// Run waits for context cancellation and closes the database.
func (db *boltDB) Run(ctx context.Context) {
	<-ctx.Done()
	db.Close()
}

// StoreEncryptedKey stores the encrypted key.
func (db *boltDB) StoreEncryptedKey(k []byte) error {
	return db.Update(func(tx *bbolt.Tx) error {
		bucket, err := tx.CreateBucketIfNotExists(appBucket)
		if err != nil {
			return fmt.Errorf("failed to create key bucket")
		}
		return bucket.Put(encKeyKey, k)
	})
}

// EncryptedKey retrieves the currently stored encrypted key.
func (db *boltDB) EncryptedKey() ([]byte, error) {
	var encKey []byte
	return encKey, db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(appBucket)
		if bucket == nil {
			return fmt.Errorf("app bucket not found")
		}
		encKey = bucket.Get(encKeyKey)
		if encKey == nil {
			return fmt.Errorf("no key found")
		}
		return nil
	})
}

// ListAccounts returns a list of DEX URLs. The DB is designed to have a single
// account per DEX, so the account itself is identified by the DEX URL.
func (db *boltDB) ListAccounts() ([]string, error) {
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
func (db *boltDB) Accounts() ([]*dexdb.AccountInfo, error) {
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
			accounts = append(accounts, acctInfo)
		}
		return nil
	})
}

// Account gets the AccountInfo associated with the specified DEX address.
func (db *boltDB) Account(url string) (*dexdb.AccountInfo, error) {
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
// DEX, it will be overwritten without indication.
func (db *boltDB) CreateAccount(ai *dexdb.AccountInfo) error {
	if ai.URL == "" {
		return fmt.Errorf("empty URL not allowed")
	}
	if ai.DEXPubKey == nil {
		return fmt.Errorf("nil DEXPubKey not allowed")
	}
	if len(ai.EncKey) == 0 {
		return fmt.Errorf("zero-length EncKey not allowed")
	}
	return db.acctsUpdate(func(accts *bbolt.Bucket) error {
		acct, err := accts.CreateBucketIfNotExists([]byte(ai.URL))
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
func (db *boltDB) AccountPaid(proof *dexdb.AccountProof) error {
	acctKey := []byte(proof.URL)
	return db.acctsUpdate(func(accts *bbolt.Bucket) error {
		acct := accts.Bucket(acctKey)
		if acct == nil {
			return fmt.Errorf("account not found for %s", proof.URL)
		}
		return acct.Put(feeProofKey, proof.Encode())
	})
}

// acctsView is a convenience function for reading from the account bucket.
func (db *boltDB) acctsView(f bucketFunc) error {
	return db.withBucket(accountsBucket, db.View, f)
}

// acctsUpdate is a convenience function for updating the account bucket.
func (db *boltDB) acctsUpdate(f bucketFunc) error {
	return db.withBucket(accountsBucket, db.Update, f)
}

// UpdateOrder saves the order information in the database. Any existing order
// info for the same order ID will be overwritten without indication.
func (db *boltDB) UpdateOrder(m *dexdb.MetaOrder) error {
	ord, md := m.Order, m.MetaData
	if md.DEX == "" {
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
			put(dexKey, []byte(md.DEX)).
			put(updateTimeKey, uint64Bytes(timeNow())).
			put(proofKey, md.Proof.Encode()).
			put(orderKey, order.EncodeOrder(ord)).
			err()
	})
}

// ActiveOrders retrieves all orders which appear to be in an active state,
// which is either in the epoch queue or in the order book.
func (db *boltDB) ActiveOrders() ([]*dexdb.MetaOrder, error) {
	return db.filteredOrders(func(oBkt *bbolt.Bucket) bool {
		status := oBkt.Get(statusKey)
		return bEqual(status, byteEpoch) || bEqual(status, byteBooked)
	})
}

// AccountOrders retrieves all orders associated with the specified DEX. n = 0
// applies no limit on number of orders returned. since = 0 is equivalent to
// disabling the time filter, since no orders were created before before 1970.
func (db *boltDB) AccountOrders(dex string, n int, since uint64) ([]*dexdb.MetaOrder, error) {
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
func (db *boltDB) MarketOrders(dex string, base, quote uint32, n int, since uint64) ([]*dexdb.MetaOrder, error) {
	dexB := []byte(dex)
	baseB := uint32Bytes(base)
	quoteB := uint32Bytes(quote)
	if n == 0 && since == 0 {
		return db.marketOrdersAll(dexB, baseB, quoteB)
	}
	return db.marketOrdersSince(dexB, baseB, quoteB, n, since)
}

// marketOrdersAll retrieves all orders for the specified DEX and market.
func (db *boltDB) marketOrdersAll(dexB, baseB, quoteB []byte) ([]*dexdb.MetaOrder, error) {
	return db.filteredOrders(func(oBkt *bbolt.Bucket) bool {
		return bEqual(dexB, oBkt.Get(dexKey)) && bEqual(baseB, oBkt.Get(baseKey)) &&
			bEqual(quoteB, oBkt.Get(quoteKey))
	})
}

// marketOrdersSince grabs market orders with optional filters for maximum count
// and age. The sort order is always newest first. If n is 0, there is no limit
// to the number of orders returned. If using with both n = 0, and since = 0,
// use marketOrdersAll instead.
func (db *boltDB) marketOrdersSince(dexB, baseB, quoteB []byte, n int, since uint64) ([]*dexdb.MetaOrder, error) {
	sinceB := uint64Bytes(since)
	return db.newestOrders(n, func(oBkt *bbolt.Bucket) bool {
		timeB := oBkt.Get(updateTimeKey)
		return bEqual(dexB, oBkt.Get(dexKey)) && bEqual(baseB, oBkt.Get(baseKey)) &&
			bEqual(quoteB, oBkt.Get(quoteKey)) && bytes.Compare(timeB, sinceB) >= 0
	})
}

// orderTimePair is used to build an on-the-fly index to sort orders by time.
type orderTimePair struct {
	oid []byte
	t   uint64
}

// newestOrders returns the n newest orders, filtered with a supplied filter
// function. Each order's bucket is provided to the filter, and a boolean true
// return value indicates the order should is eligible to be decoded and
// returned.
func (db *boltDB) newestOrders(n int, filter func(*bbolt.Bucket) bool) ([]*dexdb.MetaOrder, error) {
	var orders []*dexdb.MetaOrder
	return orders, db.ordersView(func(master *bbolt.Bucket) error {
		pairs := make([]*orderTimePair, 0, n)
		master.ForEach(func(oid, _ []byte) error {
			oBkt := master.Bucket(oid)
			timeB := oBkt.Get(updateTimeKey)
			if filter(oBkt) {
				pairs = append(pairs, &orderTimePair{
					oid: oid,
					t:   intCoder.Uint64(timeB),
				})
			}
			return nil
		})
		// Sort the pairs newest first.
		sort.Slice(pairs, func(i, j int) bool { return pairs[i].t > pairs[j].t })
		if n > 0 && len(pairs) > n {
			pairs = pairs[:n]
		}

		for _, pair := range pairs {
			oBkt := master.Bucket(pair.oid)
			o, err := decodeOrderBucket(pair.oid, oBkt)
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
func (db *boltDB) filteredOrders(filter func(*bbolt.Bucket) bool) ([]*dexdb.MetaOrder, error) {
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
func (db *boltDB) Order(oid order.OrderID) (mord *dexdb.MetaOrder, err error) {
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
			Proof:  *proof,
			Status: order.OrderStatus(intCoder.Uint16(oBkt.Get(statusKey))),
			DEX:    string(oBkt.Get(dexKey)),
		},
		Order: ord,
	}, nil
}

// ordersView is a convenience function for reading from the order bucket.
func (db *boltDB) ordersView(f bucketFunc) error {
	return db.withBucket(ordersBucket, db.View, f)
}

// ordersUpdate is a convenience function for updating the order bucket.
func (db *boltDB) ordersUpdate(f bucketFunc) error {
	return db.withBucket(ordersBucket, db.Update, f)
}

// UpdateMatch updates the match information in the database. Any existing
// entry for the same match ID will be overwritten without indication.
func (db *boltDB) UpdateMatch(m *dexdb.MetaMatch) error {
	match, md := m.Match, m.MetaData
	if md.Quote == md.Base {
		return fmt.Errorf("quote and base asset cannot be the same")
	}
	if md.DEX == "" {
		return fmt.Errorf("empty DEX not allowed")
	}
	return db.matchesUpdate(func(master *bbolt.Bucket) error {
		mBkt, err := master.CreateBucketIfNotExists(match.MatchID[:])
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
			put(matchKey, order.EncodeMatch(match)).
			err()
	})
}

// ActiveMatches retrieves the matches that are in an active state, which is
// any state except order.MatchComplete.
func (db *boltDB) ActiveMatches() ([]*dexdb.MetaMatch, error) {
	return db.filteredMatches(func(mBkt *bbolt.Bucket) bool {
		status := mBkt.Get(statusKey)
		return len(status) != 1 || status[0] != uint8(order.MatchComplete)
	})
}

// filteredMatches gets all matches that pass the provided filter function. Each
// match's bucket is provided to the filter, and a boolean true return value
// indicates the match should be decoded and returned.
func (db *boltDB) filteredMatches(filter func(*bbolt.Bucket) bool) ([]*dexdb.MetaMatch, error) {
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
func (db *boltDB) matchesView(f bucketFunc) error {
	return db.withBucket(matchesBucket, db.View, f)
}

// matchesUpdate is a convenience function for updating the match bucket.
func (db *boltDB) matchesUpdate(f bucketFunc) error {
	return db.withBucket(matchesBucket, db.Update, f)
}

// UpdateWallet adds a wallet to the database.
func (db *boltDB) UpdateWallet(wallet *dexdb.Wallet) error {
	return db.walletsUpdate(func(wBkt *bbolt.Bucket) error {
		return wBkt.Put(wallet.ID(), wallet.Encode())
	})
}

// Wallets loads all wallets from the database.
func (db *boltDB) Wallets() ([]*dexdb.Wallet, error) {
	var wallets []*dexdb.Wallet
	return wallets, db.walletsView(func(wBkt *bbolt.Bucket) error {
		return wBkt.ForEach(func(_, v []byte) error {
			w, err := dexdb.DecodeWallet(v)
			if err != nil {
				return err
			}
			wallets = append(wallets, w)
			return nil
		})
	})
}

// wallets is a convenience function for reading from the wallets bucket.
func (db *boltDB) walletsView(f bucketFunc) error {
	return db.withBucket(walletsBucket, db.View, f)
}

// walletUpdate is a convenience function for updating the wallets bucket.
func (db *boltDB) walletsUpdate(f bucketFunc) error {
	return db.withBucket(walletsBucket, db.Update, f)
}

// makeTopLevelBuckets creates a top-level bucket for each of the provided keys,
// if the bucket doesn't already exist.
func (db *boltDB) makeTopLevelBuckets(buckets [][]byte) error {
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
func (db *boltDB) withBucket(bkt []byte, viewer txFunc, f bucketFunc) error {
	return viewer(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(bkt)
		if bucket == nil {
			return fmt.Errorf("failed to open %s bucket", string(bkt))
		}
		return f(bucket)
	})
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

// timeNow is the current unix timestamp in milliseconds.
func timeNow() uint64 {
	t := time.Now()
	return uint64(t.Unix()*1e3) + uint64(t.Nanosecond())/1e6
}

// A couple of common bbolt functions.
type bucketFunc func(*bbolt.Bucket) error
type txFunc func(func(*bbolt.Tx) error) error
