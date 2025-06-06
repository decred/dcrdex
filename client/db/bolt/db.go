// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package bolt

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"decred.org/dcrdex/client/db"
	dexdb "decred.org/dcrdex/client/db"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/config"
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
	// bucket keys
	appBucket             = []byte("appBucket")
	accountsBucket        = []byte("accounts")
	bondIndexesBucket     = []byte("bondIndexes")
	bondsSubBucket        = []byte("bonds") // sub bucket of accounts
	activeOrdersBucket    = []byte("activeOrders")
	archivedOrdersBucket  = []byte("orders")
	activeMatchesBucket   = []byte("activeMatches")
	archivedMatchesBucket = []byte("matches")
	botProgramsBucket     = []byte("botPrograms")
	walletsBucket         = []byte("wallets")
	notesBucket           = []byte("notes")
	pokesBucket           = []byte("pokes")
	credentialsBucket     = []byte("credentials")

	// value keys
	versionKey = []byte("version")
	linkedKey  = []byte("linked")
	// feeProofKey           = []byte("feecoin") unused
	statusKey             = []byte("status")
	baseKey               = []byte("base")
	quoteKey              = []byte("quote")
	orderKey              = []byte("order")
	matchKey              = []byte("match")
	orderIDKey            = []byte("orderID")
	matchIDKey            = []byte("matchID")
	proofKey              = []byte("proof")
	activeKey             = []byte("active")
	bondKey               = []byte("bond")
	confirmedKey          = []byte("confirmed")
	refundedKey           = []byte("refunded")
	lockTimeKey           = []byte("lockTime")
	dexKey                = []byte("dex")
	updateTimeKey         = []byte("utime")
	accountKey            = []byte("account")
	balanceKey            = []byte("balance")
	walletKey             = []byte("wallet")
	changeKey             = []byte("change")
	noteKey               = []byte("note")
	pokesKey              = []byte("pokesKey")
	stampKey              = []byte("stamp")
	severityKey           = []byte("severity")
	ackKey                = []byte("ack")
	swapFeesKey           = []byte("swapFees")
	maxFeeRateKey         = []byte("maxFeeRate")
	redeemMaxFeeRateKey   = []byte("redeemMaxFeeRate")
	redemptionFeesKey     = []byte("redeemFees")
	fundingFeesKey        = []byte("fundingFees")
	accelerationsKey      = []byte("accelerations")
	typeKey               = []byte("type")
	seedGenTimeKey        = []byte("seedGenTime")
	encSeedKey            = []byte("encSeed")
	encInnerKeyKey        = []byte("encInnerKey")
	innerKeyParamsKey     = []byte("innerKeyParams")
	outerKeyParamsKey     = []byte("outerKeyParams")
	birthdayKey           = []byte("birthday")
	credsVersionKey       = []byte("credsVersion")
	legacyKeyParamsKey    = []byte("keyParams")
	epochDurKey           = []byte("epochDur")
	fromVersionKey        = []byte("fromVersion")
	toVersionKey          = []byte("toVersion")
	fromSwapConfKey       = []byte("fromSwapConf")
	toSwapConfKey         = []byte("toSwapConf")
	optionsKey            = []byte("options")
	redemptionReservesKey = []byte("redemptionReservesKey")
	refundReservesKey     = []byte("refundReservesKey")
	disabledRateSourceKey = []byte("disabledRateSources")
	walletDisabledKey     = []byte("walletDisabled")
	// programKey            = []byte("program") unused
	langKey = []byte("lang")

	// values
	byteTrue  = encode.ByteTrue
	byteFalse = encode.ByteFalse

	backupDir = "backup"
)

// Opts is a set of options for the DB.
type Opts struct {
	BackupOnShutdown bool // default is true
}

var defaultOpts = Opts{
	BackupOnShutdown: true,
}

// BoltDB is a bbolt-based database backend for Bison Wallet. BoltDB satisfies
// the db.DB interface defined at decred.org/dcrdex/client/db.
type BoltDB struct {
	*bbolt.DB
	opts Opts
	log  dex.Logger
}

// Check that BoltDB satisfies the db.DB interface.
var _ dexdb.DB = (*BoltDB)(nil)

// NewDB is a constructor for a *BoltDB.
func NewDB(dbPath string, logger dex.Logger, opts ...Opts) (dexdb.DB, error) {
	_, err := os.Stat(dbPath)
	isNew := os.IsNotExist(err)

	db, err := bbolt.Open(dbPath, 0600, &bbolt.Options{Timeout: 3 * time.Second})
	if err != nil {
		if errors.Is(err, bbolt.ErrTimeout) {
			err = fmt.Errorf("%w, could happen when database is already being used by another process", err)
		}
		return nil, err
	}

	// Release the file lock on exit.
	bdb := &BoltDB{
		DB:   db,
		opts: defaultOpts,
		log:  logger,
	}
	if len(opts) > 0 {
		bdb.opts = opts[0]
	}

	if err = bdb.makeTopLevelBuckets([][]byte{
		appBucket, accountsBucket, bondIndexesBucket,
		activeOrdersBucket, archivedOrdersBucket,
		activeMatchesBucket, archivedMatchesBucket,
		walletsBucket, notesBucket, credentialsBucket,
		botProgramsBucket, pokesBucket,
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

			return nil
		})
		if err != nil {
			return nil, err
		}

		bdb.log.Infof("Created and started database (version = %d, file = %s)", DBVersion, dbPath)

		return bdb, nil
	}

	err = bdb.upgradeDB()
	if err != nil {
		return nil, err
	}

	bdb.log.Infof("Started database (version = %d, file = %s)", DBVersion, dbPath)

	return bdb, nil
}

func (db *BoltDB) fileSize(path string) int64 {
	stat, err := os.Stat(path)
	if err != nil {
		db.log.Errorf("Failed to stat %v: %v", path, err)
		return 0
	}
	return stat.Size()
}

// Run waits for context cancellation and closes the database.
func (db *BoltDB) Run(ctx context.Context) {
	<-ctx.Done() // wait for shutdown to backup and compact

	// Create a backup in the backups folder.
	if db.opts.BackupOnShutdown {
		db.log.Infof("Backing up database...")
		if err := db.Backup(); err != nil {
			db.log.Errorf("Unable to backup database: %v", err)
		}
	}

	// Only compact the current DB file if there is excessive free space, in
	// terms of bytes AND relative to total DB size.
	const byteThresh = 1 << 18 // 256 KiB free
	const pctThresh = 0.05     // 5% free
	var dbSize int64
	_ = db.View(func(tx *bbolt.Tx) error {
		dbSize = tx.Size() // db size including free bytes
		return nil
	})
	dbStats := db.Stats()
	// FreeAlloc is (FreePageN + PendingPageN) * db.Info().PageSize
	// FreelistInuse seems to be page header overhead (much smaller). Add them.
	// https://github.com/etcd-io/bbolt/blob/020684ea1eb7b5574a8007c8d69605b1de8d9ec4/tx.go#L308-L309
	freeBytes := int64(dbStats.FreeAlloc + dbStats.FreelistInuse)
	pctFree := float64(freeBytes) / float64(dbSize)
	db.log.Debugf("Total DB size %d bytes, %d bytes unused (%.2f%%)",
		dbSize, freeBytes, 100*pctFree)
	// Only compact if free space is at least the byte threshold AND that fee
	// space accounts for a significant percent of the file.
	if freeBytes < byteThresh || pctFree < pctThresh {
		db.Close()
		return
	}

	// TODO: If we never see trouble with the compacted DB files when bisonw
	// starts up, we can just compact to the backups folder and copy that backup
	// file back on top of the main DB file after it is closed.  For now, the
	// backup above is a direct (not compacted) copy.

	// Compact the database by writing into a temporary file, closing the source
	// DB, and overwriting the original with the compacted temporary file.
	db.log.Infof("Compacting database to reclaim at least %d bytes...", freeBytes)
	srcPath := db.Path()                     // before db.Close
	compFile := srcPath + ".tmp"             // deterministic on *same fs*
	err := db.BackupTo(compFile, true, true) // overwrite and compact
	if err != nil {
		db.Close()
		db.log.Errorf("Unable to compact database: %v", err)
		return
	}

	db.Close() // close db file at srcPath

	initSize, compSize := db.fileSize(srcPath), db.fileSize(compFile)
	db.log.Infof("Compacted database file from %v => %v bytes (%.2f%% reduction)",
		initSize, compSize, 100*float64(initSize-compSize)/float64(initSize))

	err = os.Rename(compFile, srcPath) // compFile => srcPath
	if err != nil {
		db.log.Errorf("Unable to switch to compacted database: %v", err)
		return
	}
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
			if len(acctInfo.LegacyEncKey) != 0 {
				privB, err := oldCrypter.Decrypt(acctInfo.LegacyEncKey)
				if err != nil {
					return err
				}

				acctInfo.LegacyEncKey, err = newCrypter.Encrypt(privB)
				if err != nil {
					return err
				}
				acctUpdates[acctInfo.Host] = acctInfo.LegacyEncKey
			} else if len(acctInfo.EncKeyV2) > 0 {
				privB, err := oldCrypter.Decrypt(acctInfo.EncKeyV2)
				if err != nil {
					return err
				}
				acctInfo.EncKeyV2, err = newCrypter.Encrypt(privB)
				if err != nil {
					return err
				}
				acctUpdates[acctInfo.Host] = acctInfo.EncKeyV2
			}

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
			return errors.New("no credentials bucket")
		}
		if bkt.Stats().KeyN == 0 {
			return dexdb.ErrNoCredentials
		}

		versionB := getCopy(bkt, credsVersionKey)
		if len(versionB) != 2 {
			versionB = []byte{0x00, 0x00}
		}

		bdayB := getCopy(bkt, birthdayKey)
		bday := time.Time{}
		if len(bdayB) > 0 {
			bday = time.Unix(int64(intCoder.Uint64(bdayB)), 0)
		}

		creds = &dexdb.PrimaryCredentials{
			EncSeed:        getCopy(bkt, encSeedKey),
			EncInnerKey:    getCopy(bkt, encInnerKeyKey),
			InnerKeyParams: getCopy(bkt, innerKeyParamsKey),
			OuterKeyParams: getCopy(bkt, outerKeyParamsKey),
			Birthday:       bday,
			Version:        intCoder.Uint16(versionB),
		}
		return nil
	})
}

// setCreds stores the *PrimaryCredentials.
func (db *BoltDB) setCreds(tx *bbolt.Tx, creds *dexdb.PrimaryCredentials) error {
	bkt := tx.Bucket(credentialsBucket)
	if bkt == nil {
		return errors.New("no credentials bucket")
	}
	return newBucketPutter(bkt).
		put(encSeedKey, creds.EncSeed).
		put(encInnerKeyKey, creds.EncInnerKey).
		put(innerKeyParamsKey, creds.InnerKeyParams).
		put(outerKeyParamsKey, creds.OuterKeyParams).
		put(birthdayKey, uint64Bytes(uint64(creds.Birthday.Unix()))).
		put(credsVersionKey, uint16Bytes(creds.Version)).
		err()
}

// PrimaryCredentials retrieves the *PrimaryCredentials, if they are stored. It
// is an error if none have been stored.
func (db *BoltDB) PrimaryCredentials() (creds *dexdb.PrimaryCredentials, err error) {
	return db.primaryCreds()
}

// SetSeedGenerationTime stores the time the app seed was generated.
func (db *BoltDB) SetSeedGenerationTime(time uint64) error {
	return db.Update(func(tx *bbolt.Tx) error {
		bkt := tx.Bucket(credentialsBucket)
		if bkt == nil {
			return errors.New("no credentials bucket")
		}

		return newBucketPutter(bkt).
			put(seedGenTimeKey, uint64Bytes(time)).
			err()
	})
}

// SeedGenerationTime returns the time the app seed was generated, if it was
// stored. It returns dexdb.ErrNoSeedGenTime if it was not stored.
func (db *BoltDB) SeedGenerationTime() (uint64, error) {
	var seedGenTime uint64
	return seedGenTime, db.View(func(tx *bbolt.Tx) error {
		bkt := tx.Bucket(credentialsBucket)
		if bkt == nil {
			return errors.New("no credentials bucket")
		}

		seedGenTimeBytes := bkt.Get(seedGenTimeKey)
		if seedGenTimeBytes == nil {
			return dexdb.ErrNoSeedGenTime
		}
		if len(seedGenTimeBytes) != 8 {
			return fmt.Errorf("seed generation time length %v, expected 8", len(seedGenTimeBytes))
		}

		seedGenTime = intCoder.Uint64(seedGenTimeBytes)
		return nil
	})
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

func loadAccountInfo(acct *bbolt.Bucket, log dex.Logger) (*db.AccountInfo, error) {
	acctB := getCopy(acct, accountKey)
	if acctB == nil {
		return nil, fmt.Errorf("empty account")
	}
	acctInfo, err := dexdb.DecodeAccountInfo(acctB)
	if err != nil {
		return nil, err
	}

	acctInfo.Disabled = bytes.Equal(acct.Get(activeKey), byteFalse)

	bondsBkt := acct.Bucket(bondsSubBucket)
	if bondsBkt == nil {
		return acctInfo, nil // no bonds, OK for legacy account
	}

	c := bondsBkt.Cursor()
	for bondUID, _ := c.First(); bondUID != nil; bondUID, _ = c.Next() {
		bond := bondsBkt.Bucket(bondUID)
		if acct == nil {
			return nil, fmt.Errorf("bond sub-bucket %x not a nested bucket", bondUID)
		}
		dbBond, err := dexdb.DecodeBond(getCopy(bond, bondKey))
		if err != nil {
			log.Errorf("Invalid bond data encoding: %v", err)
			continue
		}
		dbBond.Confirmed = bEqual(bond.Get(confirmedKey), byteTrue)
		dbBond.Refunded = bEqual(bond.Get(refundedKey), byteTrue)
		acctInfo.Bonds = append(acctInfo.Bonds, dbBond)
	}

	return acctInfo, nil
}

// Accounts returns a list of DEX Accounts. The DB is designed to have a single
// account per DEX, so the account itself is identified by the DEX host. TODO:
// allow bonds filter based on lockTime.
func (db *BoltDB) Accounts() ([]*dexdb.AccountInfo, error) {
	var accounts []*dexdb.AccountInfo
	return accounts, db.acctsView(func(accts *bbolt.Bucket) error {
		c := accts.Cursor()
		for acctKey, _ := c.First(); acctKey != nil; acctKey, _ = c.Next() {
			acct := accts.Bucket(acctKey)
			if acct == nil {
				return fmt.Errorf("account bucket %s value not a nested bucket", string(acctKey))
			}
			acctInfo, err := loadAccountInfo(acct, db.log)
			if err != nil {
				return err
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
	return acctInfo, db.acctsView(func(accts *bbolt.Bucket) (err error) {
		acct := accts.Bucket(acctKey)
		if acct == nil {
			return dexdb.ErrAcctNotFound
		}
		acctInfo, err = loadAccountInfo(acct, db.log)
		return
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
	return db.acctsUpdate(func(accts *bbolt.Bucket) error {
		acct, err := accts.CreateBucket([]byte(ai.Host))
		if err != nil {
			return fmt.Errorf("failed to create account bucket: %w", err)
		}

		err = acct.Put(accountKey, ai.Encode())
		if err != nil {
			return fmt.Errorf("accountKey put error: %w", err)
		}
		err = acct.Put(activeKey, byteTrue)
		if err != nil {
			return fmt.Errorf("activeKey put error: %w", err)
		}

		bonds, err := acct.CreateBucket(bondsSubBucket)
		if err != nil {
			return fmt.Errorf("unable to create bonds sub-bucket for account for %s: %w", ai.Host, err)
		}

		for _, bond := range ai.Bonds {
			bondUID := bond.UniqueID()
			bondBkt, err := bonds.CreateBucketIfNotExists(bondUID)
			if err != nil {
				return fmt.Errorf("failed to create bond %x bucket: %w", bondUID, err)
			}

			err = db.storeBond(bondBkt, bond)
			if err != nil {
				return err
			}
		}

		return nil
	})
}

// NextBondKeyIndex returns the next bond key index and increments the stored
// value so that subsequent calls will always return a higher index.
func (db *BoltDB) NextBondKeyIndex(assetID uint32) (uint32, error) {
	var bondIndex uint32
	return bondIndex, db.Update(func(tx *bbolt.Tx) error {
		bkt := tx.Bucket(bondIndexesBucket)
		if bkt == nil {
			return errors.New("no bond indexes bucket")
		}

		thisBondIdxKey := uint32Bytes(assetID)
		bondIndexB := bkt.Get(thisBondIdxKey)
		if len(bondIndexB) != 0 {
			bondIndex = intCoder.Uint32(bondIndexB)
		}
		return bkt.Put(thisBondIdxKey, uint32Bytes(bondIndex+1))
	})
}

// UpdateAccountInfo updates the account info for an existing account with
// the same Host as the parameter. If no account exists with this host,
// an error is returned.
func (db *BoltDB) UpdateAccountInfo(ai *dexdb.AccountInfo) error {
	return db.acctsUpdate(func(accts *bbolt.Bucket) error {
		acct := accts.Bucket([]byte(ai.Host))
		if acct == nil {
			return fmt.Errorf("account not found for %s", ai.Host)
		}

		err := acct.Put(accountKey, ai.Encode())
		if err != nil {
			return fmt.Errorf("accountKey put error: %w", err)
		}

		bonds, err := acct.CreateBucketIfNotExists(bondsSubBucket)
		if err != nil {
			return fmt.Errorf("unable to create bonds sub-bucket for account for %s: %w", ai.Host, err)
		}

		for _, bond := range ai.Bonds {
			bondUID := bond.UniqueID()
			bondBkt, err := bonds.CreateBucketIfNotExists(bondUID)
			if err != nil {
				return fmt.Errorf("failed to create bond %x bucket: %w", bondUID, err)
			}

			err = db.storeBond(bondBkt, bond)
			if err != nil {
				return err
			}
		}

		return nil
	})
}

// ToggleAccountStatus enables or disables the account associated with the given
// host.
func (db *BoltDB) ToggleAccountStatus(host string, disable bool) error {
	return db.acctsUpdate(func(accts *bbolt.Bucket) error {
		acct := accts.Bucket([]byte(host))
		if acct == nil {
			return fmt.Errorf("account not found for %s", host)
		}

		newStatus := byteTrue
		if disable {
			newStatus = byteFalse
		}

		if bytes.Equal(acct.Get(activeKey), newStatus) {
			msg := "account is already enabled"
			if disable {
				msg = "account is already disabled"
			}
			return errors.New(msg)
		}

		err := acct.Put(activeKey, newStatus)
		if err != nil {
			return fmt.Errorf("accountKey put error: %w", err)
		}
		return nil
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

func (db *BoltDB) storeBond(bondBkt *bbolt.Bucket, bond *db.Bond) error {
	err := bondBkt.Put(bondKey, bond.Encode())
	if err != nil {
		return fmt.Errorf("bondKey put error: %w", err)
	}

	confirmed := encode.ByteFalse
	if bond.Confirmed {
		confirmed = encode.ByteTrue
	}
	err = bondBkt.Put(confirmedKey, confirmed)
	if err != nil {
		return fmt.Errorf("confirmedKey put error: %w", err)
	}

	refunded := encode.ByteFalse
	if bond.Refunded {
		refunded = encode.ByteTrue
	}
	err = bondBkt.Put(refundedKey, refunded)
	if err != nil {
		return fmt.Errorf("refundedKey put error: %w", err)
	}

	err = bondBkt.Put(lockTimeKey, uint64Bytes(bond.LockTime)) // also in bond encoding
	if err != nil {
		return fmt.Errorf("lockTimeKey put error: %w", err)
	}

	return nil
}

// AddBond saves a new Bond or updates an existing bond for an existing DEX
// account.
func (db *BoltDB) AddBond(host string, bond *db.Bond) error {
	acctKey := []byte(host)
	return db.acctsUpdate(func(accts *bbolt.Bucket) error {
		acct := accts.Bucket(acctKey)
		if acct == nil {
			return fmt.Errorf("account not found for %s", host)
		}

		bonds, err := acct.CreateBucketIfNotExists(bondsSubBucket)
		if err != nil {
			return fmt.Errorf("unable to access bonds sub-bucket for account for %s: %w", host, err)
		}

		bondUID := bond.UniqueID()
		bondBkt, err := bonds.CreateBucketIfNotExists(bondUID)
		if err != nil {
			return fmt.Errorf("failed to create bond %x bucket: %w", bondUID, err)
		}

		return db.storeBond(bondBkt, bond)
	})
}

func (db *BoltDB) setBondFlag(host string, assetID uint32, bondCoinID []byte, flagKey []byte) error {
	acctKey := []byte(host)
	return db.acctsUpdate(func(accts *bbolt.Bucket) error {
		acct := accts.Bucket(acctKey)
		if acct == nil {
			return fmt.Errorf("account not found for %s", host)
		}

		bonds := acct.Bucket(bondsSubBucket)
		if bonds == nil {
			return fmt.Errorf("bonds sub-bucket not found for account for %s", host)
		}

		bondUID := dexdb.BondUID(assetID, bondCoinID)
		bondBkt := bonds.Bucket(bondUID)
		if bondBkt == nil {
			return fmt.Errorf("bond bucket does not exist: %x", bondUID)
		}

		return bondBkt.Put(flagKey, byteTrue)
	})
}

// ConfirmBond marks a DEX account bond as confirmed by the DEX.
func (db *BoltDB) ConfirmBond(host string, assetID uint32, bondCoinID []byte) error {
	return db.setBondFlag(host, assetID, bondCoinID, confirmedKey)
}

// BondRefunded marks a DEX account bond as refunded by the client wallet.
func (db *BoltDB) BondRefunded(host string, assetID uint32, bondCoinID []byte) error {
	return db.setBondFlag(host, assetID, bondCoinID, refundedKey)
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

		err = newBucketPutter(oBkt).
			put(baseKey, uint32Bytes(ord.Base())).
			put(quoteKey, uint32Bytes(ord.Quote())).
			put(dexKey, []byte(md.Host)).
			put(typeKey, []byte{byte(ord.Type())}).
			put(orderKey, order.EncodeOrder(ord)).
			put(epochDurKey, uint64Bytes(md.EpochDur)).
			put(fromVersionKey, uint32Bytes(md.FromVersion)).
			put(toVersionKey, uint32Bytes(md.ToVersion)).
			put(fromSwapConfKey, uint32Bytes(md.FromSwapConf)).
			put(toSwapConfKey, uint32Bytes(md.ToSwapConf)).
			put(redeemMaxFeeRateKey, uint64Bytes(md.RedeemMaxFeeRate)).
			put(maxFeeRateKey, uint64Bytes(md.MaxFeeRate)).
			err()

		if err != nil {
			return err
		}

		return updateOrderMetaData(oBkt, md)
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
// disabling the time filter, since no orders were created before 1970.
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
// disabling the time filter, since no orders were created before 1970.
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
func (db *BoltDB) Orders(orderFilter *dexdb.OrderFilter) (ords []*dexdb.MetaOrder, err error) {
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
			return slices.Contains(orderFilter.Statuses, status)
		})
		includeArchived = false
		for _, status := range orderFilter.Statuses {
			if !status.IsActive() {
				includeArchived = true
				break
			}
		}
	}

	if orderFilter.Market != nil {
		filters = append(filters, func(_ []byte, oBkt *bbolt.Bucket) bool {
			baseID, quoteID := intCoder.Uint32(oBkt.Get(baseKey)), intCoder.Uint32(oBkt.Get(quoteKey))
			return orderFilter.Market.Base == baseID && orderFilter.Market.Quote == quoteID
		})
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

	var redemptionReserves uint64
	redemptionReservesB := oBkt.Get(redemptionReservesKey)
	if len(redemptionReservesB) == 8 {
		redemptionReserves = intCoder.Uint64(redemptionReservesB)
	}

	var refundReserves uint64
	refundReservesB := oBkt.Get(refundReservesKey)
	if len(refundReservesB) == 8 {
		refundReserves = intCoder.Uint64(refundReservesB)
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

	var redeemMaxFeeRate uint64
	if redeemMaxFeeRateB := oBkt.Get(redeemMaxFeeRateKey); len(redeemMaxFeeRateB) == 8 {
		redeemMaxFeeRate = intCoder.Uint64(redeemMaxFeeRateB)
	}

	var fromVersion, toVersion uint32
	fromVersionB, toVersionB := oBkt.Get(fromVersionKey), oBkt.Get(toVersionKey)
	if len(fromVersionB) == 4 {
		fromVersion = intCoder.Uint32(fromVersionB)
	}
	if len(toVersionB) == 4 {
		toVersion = intCoder.Uint32(toVersionB)
	}

	var fromSwapConf, toSwapConf uint32
	fromSwapConfB, toSwapConfB := oBkt.Get(fromSwapConfKey), oBkt.Get(toSwapConfKey)
	if len(fromSwapConfB) == 4 {
		fromSwapConf = intCoder.Uint32(fromSwapConfB)
	}
	if len(toSwapConfB) == 4 {
		toSwapConf = intCoder.Uint32(toSwapConfB)
	}

	var epochDur uint64
	if epochDurB := oBkt.Get(epochDurKey); len(epochDurB) == 8 {
		epochDur = intCoder.Uint64(epochDurB)
	}

	optionsB := oBkt.Get(optionsKey)
	options, err := config.Parse(optionsB)
	if err != nil {
		return nil, fmt.Errorf("unable to decode order options")
	}

	var accelerationCoinIDs []order.CoinID
	accelerationsB := getCopy(oBkt, accelerationsKey)
	if len(accelerationsB) > 0 {
		_, coinIDs, err := encode.DecodeBlob(accelerationsB)
		if err != nil {
			return nil, fmt.Errorf("unable to decode accelerations")
		}
		for _, coinID := range coinIDs {
			accelerationCoinIDs = append(accelerationCoinIDs, order.CoinID(coinID))
		}
	}

	var fundingFeesPaid uint64
	if fundingFeesB := oBkt.Get(fundingFeesKey); len(fundingFeesB) == 8 {
		fundingFeesPaid = intCoder.Uint64(fundingFeesB)
	}

	return &dexdb.MetaOrder{
		MetaData: &dexdb.OrderMetaData{
			Proof:              *proof,
			Status:             order.OrderStatus(intCoder.Uint16(oBkt.Get(statusKey))),
			Host:               string(getCopy(oBkt, dexKey)),
			ChangeCoin:         getCopy(oBkt, changeKey),
			LinkedOrder:        linkedID,
			SwapFeesPaid:       intCoder.Uint64(oBkt.Get(swapFeesKey)),
			EpochDur:           epochDur,
			MaxFeeRate:         maxFeeRate,
			RedeemMaxFeeRate:   redeemMaxFeeRate,
			RedemptionFeesPaid: intCoder.Uint64(oBkt.Get(redemptionFeesKey)),
			FromSwapConf:       fromSwapConf,
			ToSwapConf:         toSwapConf,
			FromVersion:        fromVersion,
			ToVersion:          toVersion,
			Options:            options,
			RedemptionReserves: redemptionReserves,
			RefundReserves:     refundReserves,
			AccelerationCoins:  accelerationCoinIDs,
			FundingFeesPaid:    fundingFeesPaid,
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
func (db *BoltDB) UpdateOrderMetaData(oid order.OrderID, md *dexdb.OrderMetaData) error {
	return db.ordersUpdate(func(ob, archivedOB *bbolt.Bucket) error {
		oBkt, err := updateOrderBucket(ob, archivedOB, oid, md.Status)
		if err != nil {
			return fmt.Errorf("UpdateOrderMetaData: %w", err)
		}

		return updateOrderMetaData(oBkt, md)
	})
}

func updateOrderMetaData(bkt *bbolt.Bucket, md *dexdb.OrderMetaData) error {
	var linkedB []byte
	if !md.LinkedOrder.IsZero() {
		linkedB = md.LinkedOrder[:]
	}

	var accelerationsB encode.BuildyBytes
	if len(md.AccelerationCoins) > 0 {
		accelerationsB = encode.BuildyBytes{0}
		for _, acceleration := range md.AccelerationCoins {
			accelerationsB = accelerationsB.AddData(acceleration)
		}
	}

	return newBucketPutter(bkt).
		put(statusKey, uint16Bytes(uint16(md.Status))).
		put(updateTimeKey, uint64Bytes(timeNow())).
		put(proofKey, md.Proof.Encode()).
		put(changeKey, md.ChangeCoin).
		put(linkedKey, linkedB).
		put(swapFeesKey, uint64Bytes(md.SwapFeesPaid)).
		put(redemptionFeesKey, uint64Bytes(md.RedemptionFeesPaid)).
		put(optionsKey, config.Data(md.Options)).
		put(redemptionReservesKey, uint64Bytes(md.RedemptionReserves)).
		put(refundReservesKey, uint64Bytes(md.RefundReserves)).
		put(accelerationsKey, accelerationsB).
		put(fundingFeesKey, uint64Bytes(md.FundingFeesPaid)).
		err()
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
	match, matchVer, err := order.DecodeMatch(matchB)
	if err != nil {
		return nil, fmt.Errorf("error decoding match: %w", err)
	}
	if matchVer == 0 && match.Status == order.MatchComplete {
		// When v0 matches were written, there was no MatchConfirmed, so we will
		// "upgrade" this match on the fly to agree with MatchIsActive.
		match.Status = order.MatchConfirmed
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
func (db *BoltDB) UpdateBalance(wid []byte, bal *dexdb.Balance) error {
	return db.walletsUpdate(func(master *bbolt.Bucket) error {
		wBkt := master.Bucket(wid)
		if wBkt == nil {
			return fmt.Errorf("wallet %x bucket is not a bucket", wid)
		}
		return wBkt.Put(balanceKey, bal.Encode())
	})
}

// UpdateWalletStatus updates a wallet's status.
func (db *BoltDB) UpdateWalletStatus(wid []byte, disable bool) error {
	return db.walletsUpdate(func(master *bbolt.Bucket) error {
		wBkt := master.Bucket(wid)
		if wBkt == nil {
			return fmt.Errorf("wallet %x bucket is not a bucket", wid)
		}
		if disable {
			return wBkt.Put(walletDisabledKey, encode.ByteTrue)
		}
		return wBkt.Put(walletDisabledKey, encode.ByteFalse)
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

// Wallet loads a single wallet from the database.
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
		bal, err := dexdb.DecodeBalance(balB)
		if err != nil {
			return nil, fmt.Errorf("DecodeBalance error: %w", err)
		}
		w.Balance = bal
	}

	statusB := wBkt.Get(walletDisabledKey)
	if statusB != nil {
		w.Disabled = bytes.Equal(statusB, encode.ByteTrue)
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
	return notes, db.notesUpdate(func(master *bbolt.Bucket) error {
		trios := newestBuckets([]*bbolt.Bucket{master}, n, stampKey, nil)
		for _, trio := range trios {
			note, err := dexdb.DecodeNotification(getCopy(trio.b, noteKey))
			if err != nil {
				return err
			}
			note.Ack = bEqual(trio.b.Get(ackKey), byteTrue)
			note.Id = note.ID()
			if !bytes.Equal(note.Id, trio.k) {
				// This notification was initially stored when the serialization
				// and thus note ID did not include the TopicID. Ignore it, and
				// flag it acknowledged so we don't have to hear about it again.
				if !note.Ack {
					db.log.Tracef("Ignoring stored note with bad key: %x != %x \"%s\"",
						[]byte(note.Id), trio.k, note.String())
					_ = trio.b.Put(ackKey, byteTrue)
				}
				continue
			}
			notes = append(notes, note)
		}
		return nil
	})
}

// notesUpdate is a convenience function for updating the notifications bucket.
func (db *BoltDB) notesUpdate(f bucketFunc) error {
	return db.withBucket(notesBucket, db.Update, f)
}

// SavePokes saves a slice of notifications, overwriting any previously saved
// slice.
func (db *BoltDB) SavePokes(pokes []*dexdb.Notification) error {
	// Just save it as JSON.
	b, err := json.Marshal(pokes)
	if err != nil {
		return fmt.Errorf("JSON marshal error: %w", err)
	}
	return db.withBucket(pokesBucket, db.Update, func(bkt *bbolt.Bucket) error {
		return bkt.Put(pokesKey, b)
	})
}

// LoadPokes loads the slice of notifications last saved with SavePokes. The
// loaded pokes are deleted from the database.
func (db *BoltDB) LoadPokes() (pokes []*dexdb.Notification, _ error) {
	return pokes, db.withBucket(pokesBucket, db.Update, func(bkt *bbolt.Bucket) error {
		b := bkt.Get(pokesKey)
		if len(b) == 0 { // None saved
			return nil
		}
		if err := json.Unmarshal(b, &pokes); err != nil {
			return err
		}
		return bkt.Delete(pokesKey)
	})
}

// newest buckets gets the nested buckets with the highest timestamp from the
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

func ovrFlag(overwrite bool) int {
	if overwrite {
		return os.O_TRUNC
	}
	return os.O_EXCL
}

// compact writes a compacted copy of the DB into the specified destination
// file. This function should be called from BackupTo to validate the
// destination file and create any needed parent folders.
func (db *BoltDB) compact(dst string, overwrite bool) error {
	opts := bbolt.Options{
		OpenFile: func(path string, _ int, mode os.FileMode) (*os.File, error) {
			return os.OpenFile(path, os.O_RDWR|os.O_CREATE|ovrFlag(overwrite), mode)
		},
	}
	newDB, err := bbolt.Open(dst, 0600, &opts)
	if err != nil {
		return fmt.Errorf("unable to compact database: %w", err)
	}

	const txMaxSize = 1 << 19 // 512 KiB
	err = bbolt.Compact(newDB, db.DB, txMaxSize)
	if err != nil {
		_ = newDB.Close()
		return fmt.Errorf("unable to compact database: %w", err)
	}
	return newDB.Close()
}

// backup writes a direct copy of the DB into the specified destination file.
// This function should be called from BackupTo to validate the destination file
// and create any needed parent folders.
func (db *BoltDB) backup(dst string, overwrite bool) error {
	// Just copy. This is like tx.CopyFile but with the overwrite flag set
	// as specified.
	f, err := os.OpenFile(dst, os.O_RDWR|os.O_CREATE|ovrFlag(overwrite), 0600)
	if err != nil {
		return err
	}
	defer f.Close()

	err = db.View(func(tx *bbolt.Tx) error {
		_, err = tx.WriteTo(f)
		return err
	})
	if err != nil {
		return err
	}
	return f.Sync()
}

// BackupTo makes a copy of the database to the specified file, optionally
// overwriting and compacting the DB.
func (db *BoltDB) BackupTo(dst string, overwrite, compact bool) error {
	// If relative path, use current db path.
	var dir string
	if !filepath.IsAbs(dst) {
		dir = filepath.Dir(db.Path())
		dst = filepath.Join(dir, dst)
	}
	dst = filepath.Clean(dst)
	dir = filepath.Dir(dst)
	if dst == filepath.Clean(db.Path()) {
		return errors.New("destination is the active DB")
	}

	// Make the parent folder if it does not exists.
	if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		err = os.MkdirAll(dir, 0700)
		if err != nil {
			return fmt.Errorf("unable to create backup directory: %w", err)
		}
	}

	if compact {
		return db.compact(dst, overwrite)
	}

	return db.backup(dst, overwrite)
}

// Backup makes a copy of the database in the "backup" folder, overwriting any
// existing backup.
func (db *BoltDB) Backup() error {
	dir, file := filepath.Split(db.Path())
	return db.BackupTo(filepath.Join(dir, backupDir, file), true, false)
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

// DeleteInactiveOrders deletes orders that are no longer needed for normal
// operations. Optionally accepts a time to delete orders with a later time
// stamp. Accepts an optional function to perform on deleted orders.
func (db *BoltDB) DeleteInactiveOrders(ctx context.Context, olderThan *time.Time,
	perOrderFn func(ords *dexdb.MetaOrder) error) (int, error) {
	const batchSize = 1000
	var olderThanB []byte
	if olderThan != nil && !olderThan.IsZero() {
		olderThanB = uint64Bytes(uint64(olderThan.UnixMilli()))
	} else {
		olderThanB = uint64Bytes(timeNow())
	}

	activeMatchOrders := make(map[order.OrderID]struct{})
	if err := db.View(func(tx *bbolt.Tx) error {
		// Some archived orders may still be needed for active matches.
		// Put those order id's in a map to prevent deletion.
		amb := tx.Bucket(activeMatchesBucket)
		if err := amb.ForEach(func(k, _ []byte) error {
			mBkt := amb.Bucket(k)
			if mBkt == nil {
				return fmt.Errorf("match %x bucket is not a bucket", k)
			}
			oidB := mBkt.Get(orderIDKey)
			var oid order.OrderID
			copy(oid[:], oidB)
			activeMatchOrders[oid] = struct{}{}
			return nil
		}); err != nil {
			return fmt.Errorf("unable to get active matches: %v", err)
		}
		return nil
	}); err != nil {
		return 0, err
	}

	// Get the keys of every archived order.
	var keys [][]byte
	if err := db.View(func(tx *bbolt.Tx) error {
		archivedOB := tx.Bucket(archivedOrdersBucket)
		if archivedOB == nil {
			return fmt.Errorf("failed to open %s bucket", string(archivedOrdersBucket))
		}
		archivedOB.ForEach(func(k, _ []byte) error {
			keys = append(keys, bytes.Clone(k))
			return nil
		})
		return nil
	}); err != nil {
		return 0, fmt.Errorf("unable to get archived order keys: %v", err)
	}

	nDeletedOrders := 0
	start := time.Now()
	// Check orders in batches to prevent any single db transaction from
	// becoming too large.
	for i := 0; i < len(keys); i += batchSize {
		if err := ctx.Err(); err != nil {
			return 0, err
		}

		// Check if this is the last batch.
		end := min(i+batchSize, len(keys))

		nDeletedBatch := 0
		err := db.Update(func(tx *bbolt.Tx) error {
			archivedOB := tx.Bucket(archivedOrdersBucket)
			if archivedOB == nil {
				return fmt.Errorf("failed to open %s bucket", string(archivedOrdersBucket))
			}
			for j := i; j < end; j++ {
				key := keys[j]
				oBkt := archivedOB.Bucket(key)

				var oid order.OrderID
				copy(oid[:], key)

				// Don't delete this order if it is still active.
				if order.OrderStatus(intCoder.Uint16(oBkt.Get(statusKey))).IsActive() {
					db.log.Warnf("active order %v found in inactive bucket", oid)
					continue
				}

				// Don't delete this order if it still has active matches.
				if _, has := activeMatchOrders[oid]; has {
					continue
				}

				// Don't delete this order if it is too new.
				timeB := oBkt.Get(updateTimeKey)
				if bytes.Compare(timeB, olderThanB) > 0 {
					continue
				}

				// Proceed with deletion.
				o, err := decodeOrderBucket(key, oBkt)
				if err != nil {
					return fmt.Errorf("failed to decode order bucket: %v", err)
				}
				if err := archivedOB.DeleteBucket(key); err != nil {
					return fmt.Errorf("failed to delete order bucket: %v", err)
				}
				if perOrderFn != nil {
					if err := perOrderFn(o); err != nil {
						return fmt.Errorf("problem performing batch function: %v", err)
					}
				}
				nDeletedBatch++
			}
			nDeletedOrders += nDeletedBatch
			return nil
		})
		if err != nil {
			if perOrderFn != nil && nDeletedBatch != 0 {
				db.log.Warnf("%d orders reported as deleted have been rolled back due to error.", nDeletedBatch)
			}
			return 0, fmt.Errorf("unable to delete orders: %v", err)
		}
	}

	db.log.Infof("Deleted %d archived orders from the database in %v",
		nDeletedOrders, time.Since(start))

	return nDeletedOrders, nil
}

// orderSide Returns whether the order was for buying or selling the asset.
func orderSide(tx *bbolt.Tx, oid order.OrderID) (sell bool, err error) {
	oidB := oid[:]
	ob := tx.Bucket(activeOrdersBucket)
	if ob == nil {
		return false, fmt.Errorf("failed to open %s bucket", string(activeOrdersBucket))
	}
	oBkt := ob.Bucket(oidB)
	// If the order is not in the active bucket, check the archived bucket.
	if oBkt == nil {
		archivedOB := tx.Bucket(archivedOrdersBucket)
		if archivedOB == nil {
			return false, fmt.Errorf("failed to open %s bucket", string(archivedOrdersBucket))
		}
		oBkt = archivedOB.Bucket(oidB)
	}
	if oBkt == nil {
		return false, fmt.Errorf("order %s not found", oid)
	}
	orderB := getCopy(oBkt, orderKey)
	if orderB == nil {
		return false, fmt.Errorf("nil order bytes for order %x", oid)
	}
	ord, err := order.DecodeOrder(orderB)
	if err != nil {
		return false, fmt.Errorf("error decoding order %x: %w", oid, err)
	}
	// Cancel orders have no side.
	if ord.Type() == order.CancelOrderType {
		return false, nil
	}
	sell = ord.Trade().Sell
	return
}

// DeleteInactiveMatches deletes matches that are no longer needed for normal
// operations. Optionally accepts a time to delete matches with a later time
// stamp. Accepts an optional function to perform on deleted matches.
func (db *BoltDB) DeleteInactiveMatches(ctx context.Context, olderThan *time.Time,
	perMatchFn func(mtch *dexdb.MetaMatch, isSell bool) error) (int, error) {
	const batchSize = 1000
	var olderThanB []byte
	if olderThan != nil && !olderThan.IsZero() {
		olderThanB = uint64Bytes(uint64(olderThan.UnixMilli()))
	} else {
		olderThanB = uint64Bytes(timeNow())
	}

	activeOrders := make(map[order.OrderID]struct{})
	if err := db.View(func(tx *bbolt.Tx) error {
		// Some archived matches still have active orders. Put those
		// order id's in a map to prevent deletion just in case they
		// are needed again.
		aob := tx.Bucket(activeOrdersBucket)
		if err := aob.ForEach(func(k, _ []byte) error {
			var oid order.OrderID
			copy(oid[:], k)
			activeOrders[oid] = struct{}{}
			return nil
		}); err != nil {
			return fmt.Errorf("unable to get active orders: %v", err)
		}
		return nil
	}); err != nil {
		return 0, err
	}

	// Get the keys of every archived match.
	var keys [][]byte
	if err := db.View(func(tx *bbolt.Tx) error {
		archivedMB := tx.Bucket(archivedMatchesBucket)
		if archivedMB == nil {
			return fmt.Errorf("failed to open %s bucket", string(archivedMatchesBucket))
		}
		archivedMB.ForEach(func(k, _ []byte) error {
			keys = append(keys, bytes.Clone(k))
			return nil
		})
		return nil
	}); err != nil {
		return 0, fmt.Errorf("unable to get archived match keys: %v", err)
	}

	nDeletedMatches := 0
	start := time.Now()
	// Check matches in batches to prevent any single db transaction from
	// becoming too large.
	for i := 0; i < len(keys); i += batchSize {
		if err := ctx.Err(); err != nil {
			return 0, err
		}

		// Check if this is the last batch.
		end := min(i+batchSize, len(keys))

		nDeletedBatch := 0
		if err := db.Update(func(tx *bbolt.Tx) error {
			archivedMB := tx.Bucket(archivedMatchesBucket)
			if archivedMB == nil {
				return fmt.Errorf("failed to open %s bucket", string(archivedMatchesBucket))
			}
			for j := i; j < end; j++ {
				key := keys[j]
				mBkt := archivedMB.Bucket(key)

				oidB := mBkt.Get(orderIDKey)
				var oid order.OrderID
				copy(oid[:], oidB)

				// Don't delete this match if it still has active orders.
				if _, has := activeOrders[oid]; has {
					continue
				}

				// Don't delete this match if it is too new.
				timeB := mBkt.Get(stampKey)
				if bytes.Compare(timeB, olderThanB) > 0 {
					continue
				}

				// Proceed with deletion.
				m, err := loadMatchBucket(mBkt, false)
				if err != nil {
					return fmt.Errorf("failed to load match bucket: %v", err)
				}
				if err := archivedMB.DeleteBucket(key); err != nil {
					return fmt.Errorf("failed to delete match bucket: %v", err)
				}
				if perMatchFn != nil {
					isSell, err := orderSide(tx, m.OrderID)
					if err != nil {
						return fmt.Errorf("problem getting order side for order %v: %v", m.OrderID, err)
					}
					if err := perMatchFn(m, isSell); err != nil {
						return fmt.Errorf("problem performing batch function: %v", err)
					}
				}
				nDeletedBatch++
			}
			nDeletedMatches += nDeletedBatch

			return nil
		}); err != nil {
			if perMatchFn != nil && nDeletedBatch != 0 {
				db.log.Warnf("%d matches reported as deleted have been rolled back due to error.", nDeletedBatch)
			}
			return 0, fmt.Errorf("unable to delete matches: %v", err)
		}
	}

	db.log.Infof("Deleted %d archived matches from the database in %v",
		nDeletedMatches, time.Since(start))

	return nDeletedMatches, nil
}

// SaveDisabledRateSources updates disabled fiat rate sources.
func (db *BoltDB) SaveDisabledRateSources(disabledSources []string) error {
	return db.Update(func(tx *bbolt.Tx) error {
		bkt := tx.Bucket(appBucket)
		if bkt == nil {
			return fmt.Errorf("failed to open %s bucket", string(appBucket))
		}
		return bkt.Put(disabledRateSourceKey, []byte(strings.Join(disabledSources, ",")))
	})
}

// DisabledRateSources retrieves a map of disabled fiat rate sources.
func (db *BoltDB) DisabledRateSources() (disabledSources []string, err error) {
	return disabledSources, db.View(func(tx *bbolt.Tx) error {
		bkt := tx.Bucket(appBucket)
		if bkt == nil {
			return fmt.Errorf("no %s bucket", string(appBucket))
		}

		disabledString := string(bkt.Get(disabledRateSourceKey))
		if disabledString == "" {
			return nil
		}

		disabled := strings.Split(disabledString, ",")
		disabledSources = make([]string, 0, len(disabled))
		for _, token := range disabled {
			if token != "" {
				disabledSources = append(disabledSources, token)
			}
		}
		return nil
	})
}

// SetLanguage stores the language.
func (db *BoltDB) SetLanguage(lang string) error {
	return db.Update(func(dbTx *bbolt.Tx) error {
		bkt := dbTx.Bucket(appBucket)
		if bkt == nil {
			return fmt.Errorf("app bucket not found")
		}
		return bkt.Put(langKey, []byte(lang))
	})
}

// Language retrieves the language stored with SetLanguage. If no language
// has been stored, an empty string is returned without an error.
func (db *BoltDB) Language() (lang string, _ error) {
	return lang, db.View(func(dbTx *bbolt.Tx) error {
		bkt := dbTx.Bucket(appBucket)
		if bkt != nil {
			lang = string(bkt.Get(langKey))
		}
		return nil
	})
}

// timeNow is the current unix timestamp in milliseconds.
func timeNow() uint64 {
	return uint64(time.Now().UnixMilli())
}

// A couple of common bbolt functions.
type bucketFunc func(*bbolt.Bucket) error
type txFunc func(func(*bbolt.Tx) error) error
