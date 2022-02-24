package bolt

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"decred.org/dcrdex/client/db"
	dbtest "decred.org/dcrdex/client/db/test"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/order"
	ordertest "decred.org/dcrdex/dex/order/test"
	"go.etcd.io/bbolt"
)

var (
	tDir     string
	tCounter int
	tLogger  = dex.StdOutLogger("db_TEST", dex.LevelTrace)
)

func newTestDB(t *testing.T) (*BoltDB, func()) {
	t.Helper()
	tCounter++
	dbPath := filepath.Join(tDir, fmt.Sprintf("db%d.db", tCounter))
	dbi, err := NewDB(dbPath, tLogger)
	if err != nil {
		t.Fatalf("error creating dB: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		dbi.Run(ctx)
	}()
	db, ok := dbi.(*BoltDB)
	if !ok {
		t.Fatalf("DB is not a *BoltDB")
	}
	shutdown := func() {
		cancel()
		wg.Wait()
	}
	return db, shutdown
}

func TestMain(m *testing.M) {
	defer os.Stdout.Sync()
	doIt := func() int {
		var err error
		tDir, err = os.MkdirTemp("", "dbtest")
		if err != nil {
			fmt.Println("error creating temporary directory:", err)
			return -1
		}
		defer os.RemoveAll(tDir)
		return m.Run()
	}
	os.Exit(doIt())
}

func TestBackup(t *testing.T) {
	db, shutdown := newTestDB(t)
	defer shutdown()

	// Backup the database.
	err := db.Backup()
	if err != nil {
		t.Fatalf("unable to backup database: %v", err)
	}

	// Ensure the backup exists.
	path := filepath.Join(filepath.Dir(db.Path()), backupDir, filepath.Base(db.Path()))
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatalf("backup file does not exist: %v", err)
	}

	// Overwrite the backup.
	err = db.Backup()
	if err != nil {
		t.Fatalf("unable to overwrite backup: %v", err)
	}
}

func TestBackupTo(t *testing.T) {
	db, shutdown := newTestDB(t)
	defer shutdown()

	// Backup the database.
	testBackup := "asdf.db"
	err := db.BackupTo(testBackup, false, false)
	if err != nil {
		t.Fatalf("unable to backup database: %v", err)
	}

	// Ensure the backup exists.
	path := filepath.Join(filepath.Dir(db.Path()), testBackup)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatalf("backup file does not exist: %v", err)
	}

	// Don't overwrite existing.
	same := db.Path()
	err = db.BackupTo(same, false, false)
	if err == nil {
		t.Fatalf("overwrote file!")
	}
	err = db.BackupTo(testBackup, false, false)
	if err == nil {
		t.Fatalf("overwrote file!")
	}
	// Allow overwrite
	err = db.BackupTo(testBackup, true, false) // no compact
	if err != nil {
		t.Fatalf("unable to backup database: %v", err)
	}
	err = db.BackupTo(testBackup, true, true) // compact
	if err != nil {
		t.Fatalf("unable to backup database: %v", err)
	}
}

func TestStorePrimaryCredentials(t *testing.T) {
	boltdb, shutdown := newTestDB(t)
	defer shutdown()

	// Trying to fetch credentials before storing should be an error.
	_, err := boltdb.PrimaryCredentials()
	if err == nil {
		t.Fatalf("no error for missing credentials: %v", err)
	}

	newCreds := func(seed, inner, innerParams, outerParams bool) *db.PrimaryCredentials {
		creds := &db.PrimaryCredentials{}
		if seed {
			creds.EncSeed = []byte("EncSeed")
		}
		if inner {
			creds.EncInnerKey = []byte("EncInnerKey")
		}
		if innerParams {
			creds.InnerKeyParams = []byte("InnerKeyParams")
		}
		if outerParams {
			creds.OuterKeyParams = []byte("OuterKeyParams")
		}
		return creds
	}

	ensureErr := func(tag string, creds *db.PrimaryCredentials) {
		if err := boltdb.SetPrimaryCredentials(creds); err == nil {
			t.Fatalf("%s: no error", tag)
		}
	}

	ensureErr("no EncSeed", newCreds(false, true, true, true))
	ensureErr("no EncInnerKey", newCreds(true, false, true, true))
	ensureErr("no InnerKeyParams", newCreds(true, true, false, true))
	ensureErr("no OuterKeyParams", newCreds(true, true, true, false))

	// Success
	goodCreds := newCreds(true, true, true, true)
	err = boltdb.SetPrimaryCredentials(goodCreds)
	if err != nil {
		t.Fatalf("SetPrimaryCredentials error: %v", err)
	}

	// Retrieve the credentials and check for consistency.
	reCreds, err := boltdb.PrimaryCredentials()
	if err != nil {
		t.Fatalf("PrimaryCredentials error: %v", err)
	}

	if !bytes.Equal(reCreds.EncSeed, goodCreds.EncSeed) {
		t.Fatalf("EncSeed wrong, wanted %x, got %x", goodCreds.EncSeed, reCreds.EncSeed)
	}
	if !bytes.Equal(reCreds.EncInnerKey, goodCreds.EncInnerKey) {
		t.Fatalf("EncInnerKey wrong, wanted %x, got %x", goodCreds.EncInnerKey, reCreds.EncInnerKey)
	}
	if !bytes.Equal(reCreds.InnerKeyParams, goodCreds.InnerKeyParams) {
		t.Fatalf("InnerKeyParams wrong, wanted %x, got %x", goodCreds.InnerKeyParams, reCreds.InnerKeyParams)
	}
	if !bytes.Equal(reCreds.OuterKeyParams, goodCreds.OuterKeyParams) {
		t.Fatalf("OuterKeyParams wrong, wanted %x, got %x", goodCreds.OuterKeyParams, reCreds.OuterKeyParams)
	}
}

func TestAccounts(t *testing.T) {
	boltdb, shutdown := newTestDB(t)
	defer shutdown()

	dexURLs, err := boltdb.ListAccounts()
	if err != nil {
		t.Fatalf("error listing accounts: %v", err)
	}
	if len(dexURLs) != 0 {
		t.Fatalf("unexpected non-empty accounts in fresh DB")
	}
	// Create and insert 1,000 accounts.
	numToDo := 1000
	if testing.Short() {
		numToDo = 50
	}
	accts := make([]*db.AccountInfo, 0, numToDo)
	acctMap := make(map[string]*db.AccountInfo)
	nTimes(numToDo, func(int) {
		acct := dbtest.RandomAccountInfo()
		accts = append(accts, acct)
		acctMap[acct.Host] = acct
	})
	tStart := time.Now()
	nTimes(numToDo, func(i int) {
		boltdb.CreateAccount(accts[i])
	})
	t.Logf("%d milliseconds to insert %d AccountInfo", time.Since(tStart)/time.Millisecond, numToDo)

	tStart = time.Now()
	nTimes(numToDo, func(i int) {
		ai := accts[i]
		reAI, err := boltdb.Account(ai.Host)
		if err != nil {
			t.Fatalf("error fetching AccountInfo: %v", err)
		}
		dbtest.MustCompareAccountInfo(t, ai, reAI)
	})
	t.Logf("%d milliseconds to read and compare %d account names", time.Since(tStart)/time.Millisecond, numToDo)

	tStart = time.Now()
	readAccts, err := boltdb.Accounts()
	if err != nil {
		t.Fatalf("Accounts error: %v", err)
	}
	nTimes(numToDo, func(i int) {
		reAI := readAccts[i]
		ai, found := acctMap[reAI.Host]
		if !found {
			t.Fatalf("no account found in map for %s", reAI.Host)
		}
		dbtest.MustCompareAccountInfo(t, ai, reAI)
	})
	t.Logf("%d milliseconds to batch read and compare %d AccountInfo", time.Since(tStart)/time.Millisecond, numToDo)

	dexURLs, err = boltdb.ListAccounts()
	if err != nil {
		t.Fatalf("error listing accounts: %v", err)
	}
	if len(dexURLs) != numToDo {
		t.Fatalf("expected %d accounts, found %d", numToDo, len(dexURLs))
	}

	acct := dbtest.RandomAccountInfo()
	ensureErr := func(tag string) {
		err := boltdb.CreateAccount(acct)
		if err == nil {
			t.Fatalf("no error for %s", tag)
		}
	}

	host := acct.Host
	acct.Host = ""
	ensureErr("Host")
	acct.Host = host

	dexKey := acct.DEXPubKey
	acct.DEXPubKey = nil
	ensureErr("DEX key")
	acct.DEXPubKey = dexKey

	encKey := acct.EncKeyV2
	acct.EncKeyV2 = nil
	ensureErr("no private key")
	acct.EncKeyV2 = encKey

	err = boltdb.CreateAccount(acct)
	if err != nil {
		t.Fatalf("failed to create account after fixing")
	}

	// Test account proofs.
	zerothHost := accts[0].Host
	zerothAcct, _ := boltdb.Account(zerothHost)
	if zerothAcct.Paid {
		t.Fatalf("Account marked as paid before account proof set")
	}
	boltdb.AccountPaid(&db.AccountProof{
		Host:  zerothAcct.Host,
		Stamp: 123456789,
		Sig:   []byte("some signature here"),
	})
	reAcct, _ := boltdb.Account(zerothHost)
	if !reAcct.Paid {
		t.Fatalf("Account not marked as paid after account proof set")
	}
}

func TestDisableAccount(t *testing.T) {
	boltdb, shutdown := newTestDB(t)
	defer shutdown()

	acct := dbtest.RandomAccountInfo()
	host := acct.Host
	err := boltdb.CreateAccount(acct)
	if err != nil {
		t.Fatalf("Unexpected CreateAccount error: %v", err)
	}
	actualDisabledAccount, err := boltdb.disabledAccount(acct.EncKey())
	if err == nil {
		t.Fatalf("Expected disabledAccount error but there was none.")
	}
	if actualDisabledAccount != nil {
		t.Fatalf("Expected not to retrieve a disabledAccount.")
	}

	err = boltdb.DisableAccount(host)

	if err != nil {
		t.Fatalf("Unexpected DisableAccount error: %v", err)
	}
	actualAcct, _ := boltdb.Account(host)
	if actualAcct != nil {
		t.Fatalf("Expected retrieval of deleted account to be nil")
	}
	actualDisabledAccount, err = boltdb.disabledAccount(acct.EncKey())
	if err != nil {
		t.Fatalf("Unexpected disabledAccount error: %v", err)
	}
	if actualDisabledAccount == nil {
		t.Fatalf("Expected to retrieve a disabledAccount.")
	}
}

func TestAccountProof(t *testing.T) {
	boltdb, shutdown := newTestDB(t)
	defer shutdown()

	acct := dbtest.RandomAccountInfo()
	host := acct.Host

	err := boltdb.CreateAccount(acct)
	if err != nil {
		t.Fatalf("Unexpected CreateAccount error: %v", err)
	}

	boltdb.AccountPaid(&db.AccountProof{
		Host:  acct.Host,
		Stamp: 123456789,
		Sig:   []byte("some signature here"),
	})

	accountProof, err := boltdb.AccountProof(host)
	if err != nil {
		t.Fatalf("Unexpected AccountProof error: %v", err)
	}
	if accountProof == nil {
		t.Fatal("AccountProof not found")
	}
}

func TestWallets(t *testing.T) {
	boltdb, shutdown := newTestDB(t)
	defer shutdown()

	wallets, err := boltdb.Wallets()
	if err != nil {
		t.Fatalf("error listing wallets from empty DB: %v", err)
	}
	if len(wallets) != 0 {
		t.Fatalf("unexpected non-empty wallets in fresh DB")
	}
	// Create and insert 1,000 wallets.
	numToDo := 1000
	if testing.Short() {
		numToDo = 50
	}
	wallets = make([]*db.Wallet, 0, numToDo)
	walletMap := make(map[string]*db.Wallet)
	tStart := time.Now()
	nTimes(numToDo, func(int) {
		w := dbtest.RandomWallet()
		wallets = append(wallets, w)
		walletMap[w.SID()] = w
		boltdb.UpdateWallet(w)
	})
	t.Logf("%d milliseconds to insert %d Wallet", time.Since(tStart)/time.Millisecond, numToDo)

	tStart = time.Now()
	reWallets, err := boltdb.Wallets()
	if err != nil {
		t.Fatalf("wallets retrieval error: %v", err)
	}
	if len(reWallets) != numToDo {
		t.Fatalf("expected %d wallets, got %d", numToDo, len(reWallets))
	}
	for _, reW := range reWallets {
		wid := reW.SID()
		ogWallet, found := walletMap[wid]
		if !found {
			t.Fatalf("wallet %s not found after retrieval", wid)
		}
		dbtest.MustCompareWallets(t, reW, ogWallet)
	}
	// Test changing the balance
	w := reWallets[0]
	newBal := *w.Balance
	newBal.Available += 1e8
	newBal.Locked += 2e8
	newBal.Immature += 3e8
	newBal.Stamp = newBal.Stamp.Add(time.Second)
	boltdb.UpdateBalance(w.ID(), &newBal)
	reW, err := boltdb.Wallet(w.ID())
	if err != nil {
		t.Fatalf("failed to retrieve wallet for balance check")
	}
	dbtest.MustCompareBalances(t, reW.Balance, &newBal)
	if !reW.Balance.Stamp.After(w.Balance.Stamp) {
		t.Fatalf("update time can't be right: %s > %s", reW.Balance.Stamp, w.Balance.Stamp)
	}

	// Test changing the password.
	newPW := randBytes(32)
	boltdb.SetWalletPassword(w.ID(), newPW)
	reW, err = boltdb.Wallet(w.ID())
	if err != nil {
		t.Fatalf("failed to retrieve wallet for new password check")
	}
	if !bytes.Equal(newPW, reW.EncryptedPW) {
		t.Fatalf("failed to set password. wanted %x, got %x", newPW, reW.EncryptedPW)
	}
	t.Logf("%d milliseconds to read and compare %d Wallet", time.Since(tStart)/time.Millisecond, numToDo)

}

func randOrderForMarket(base, quote uint32) order.Order {
	switch rand.Intn(3) {
	case 0:
		o, _ := ordertest.RandomCancelOrder()
		o.BaseAsset = base
		o.QuoteAsset = quote
		return o
	case 1:
		o, _ := ordertest.RandomMarketOrder()
		o.BaseAsset = base
		o.QuoteAsset = quote
		return o
	default:
		o, _ := ordertest.RandomLimitOrder()
		o.BaseAsset = base
		o.QuoteAsset = quote
		return o
	}
}

func mustContainOrder(t *testing.T, os []*db.MetaOrder, o *db.MetaOrder) {
	t.Helper()
	oid := o.Order.ID()
	for _, mord := range os {
		if mord.Order.ID() == oid {
			ordertest.MustCompareOrders(t, mord.Order, o.Order)
			return
		}
	}
	t.Fatalf("order %x not contained in list", oid[:])
}

func TestOrders(t *testing.T) {
	boltdb, shutdown := newTestDB(t)
	defer shutdown()

	// Create an account to use.
	acct1 := dbtest.RandomAccountInfo()
	acct2 := dbtest.RandomAccountInfo()
	err := boltdb.CreateAccount(acct1)
	err1 := boltdb.CreateAccount(acct2)
	if err != nil || err1 != nil {
		t.Fatalf("CreateAccount error: %v : %v", err, err1)
	}
	base1, quote1 := randU32(), randU32()
	base2, quote2 := randU32(), randU32()

	numToDo := 1008 // must be a multiple of 16
	numActive := 100
	if testing.Short() {
		numToDo = 48
		numActive = 10
	}
	orders := make(map[int]*db.MetaOrder, numToDo)
	orderIndex := make(map[order.OrderID]order.Order)
	nTimes(numToDo, func(i int) {
		// statuses 3, 4, and 5 considered inactive orders
		status := order.OrderStatus(rand.Intn(3) + 3) // inactive
		if i < numActive {
			// Technically, this is putting even cancel and market orders in the
			// booked state half the time, which should be impossible. The DB does not
			// check for this, and will recognize the order as active.
			// statuses 1 and 2 considered inactive orders.
			status = order.OrderStatus(rand.Intn(2) + 1)
		}
		acct := acct1
		base, quote := base1, quote1
		if i%2 == 1 {
			acct = acct2
			base, quote = base2, quote2
		}
		ord := randOrderForMarket(base, quote)

		orders[i] = &db.MetaOrder{
			MetaData: &db.OrderMetaData{
				Status:             status,
				Host:               acct.Host,
				Proof:              db.OrderProof{DEXSig: randBytes(73)},
				SwapFeesPaid:       rand.Uint64(),
				RedemptionFeesPaid: rand.Uint64(),
				MaxFeeRate:         rand.Uint64(),
			},
			Order: ord,
		}
		orderIndex[ord.ID()] = ord
	})

	tStart := time.Now()
	// Grab a timestamp halfway through.
	var tMid uint64
	iMid := numToDo / 2
	nTimes(numToDo, func(i int) {
		time.Sleep(time.Millisecond)
		if i == iMid {
			tMid = timeNow()
		}
		err := boltdb.UpdateOrder(orders[i])
		if err != nil {
			t.Fatalf("error inserting order: %v", err)
		}
	})
	t.Logf("~ %d milliseconds to insert %d MetaOrder", int(time.Since(tStart)/time.Millisecond)-numToDo, numToDo)
	tStart = time.Now()

	// Grab an order by ID.

	firstOrd := orders[0]
	mord, err := boltdb.Order(firstOrd.Order.ID())
	if err != nil {
		t.Fatalf("unable to retrieve order by id")
	}
	ordertest.MustCompareOrders(t, firstOrd.Order, mord.Order)
	if firstOrd.MetaData.SwapFeesPaid != mord.MetaData.SwapFeesPaid {
		t.Fatalf("wrong SwapFeesPaid. wanted %d, got %d", firstOrd.MetaData.SwapFeesPaid, mord.MetaData.SwapFeesPaid)
	}
	if firstOrd.MetaData.RedemptionFeesPaid != mord.MetaData.RedemptionFeesPaid {
		t.Fatalf("wrong RedemptionFeesPaid. wanted %d, got %d", firstOrd.MetaData.RedemptionFeesPaid, mord.MetaData.RedemptionFeesPaid)
	}
	if firstOrd.MetaData.MaxFeeRate != mord.MetaData.MaxFeeRate {
		t.Fatalf("wrong MaxFeeRate. wanted %d, got %d", firstOrd.MetaData.MaxFeeRate, mord.MetaData.MaxFeeRate)
	}

	// Check the active orders.
	activeOrders, err := boltdb.ActiveOrders()
	if err != nil {
		t.Fatalf("error retrieving active orders: %v", err)
	}
	if len(activeOrders) != numActive {
		t.Fatalf("expected %d active orders, got %d", numActive, len(activeOrders))
	}
	for _, m := range activeOrders {
		ord := orderIndex[m.Order.ID()]
		ordertest.MustCompareOrders(t, m.Order, ord)
	}
	t.Logf("%d milliseconds to read and compare %d active MetaOrder", time.Since(tStart)/time.Millisecond, numActive)

	// Get the orders for account 1.
	tStart = time.Now()
	acctOrders, err := boltdb.AccountOrders(acct1.Host, 0, 0)
	if err != nil {
		t.Fatalf("error fetching account orders: %v", err)
	}
	if len(acctOrders) != numToDo/2 {
		t.Fatalf("expected %d account orders, got %d", numToDo/2, len(acctOrders))
	}
	for _, m := range acctOrders {
		ord := orderIndex[m.Order.ID()]
		ordertest.MustCompareOrders(t, m.Order, ord)
	}
	t.Logf("%d milliseconds to read and compare %d account MetaOrder", time.Since(tStart)/time.Millisecond, numToDo/2)

	// Filter the account's first half of orders by timestamp.
	tStart = time.Now()
	sinceOrders, err := boltdb.AccountOrders(acct1.Host, 0, tMid)
	if err != nil {
		t.Fatalf("error retrieve account's since orders: %v", err)
	}
	if len(sinceOrders) != numToDo/4 {
		t.Fatalf("expected %d orders for account with since time, got %d", numToDo/4, len(sinceOrders))
	}
	for _, mord := range sinceOrders {
		mustContainOrder(t, acctOrders, mord)
	}
	t.Logf("%d milliseconds to read %d time-filtered MetaOrders for account", time.Since(tStart)/time.Millisecond, numToDo/4)

	// Get the orders for the specified market.
	tStart = time.Now()
	mktOrders, err := boltdb.MarketOrders(acct1.Host, base1, quote1, 0, 0)
	if err != nil {
		t.Fatalf("error retrieving orders for market: %v", err)
	}
	if len(mktOrders) != numToDo/2 {
		t.Fatalf("expected %d orders for market, got %d", numToDo/2, len(mktOrders))
	}
	t.Logf("%d milliseconds to read and compare %d MetaOrder for market", time.Since(tStart)/time.Millisecond, numToDo/2)

	// Filter the market's first half out by timestamp.
	tStart = time.Now()
	sinceOrders, err = boltdb.MarketOrders(acct1.Host, base1, quote1, 0, tMid)
	if err != nil {
		t.Fatalf("error retrieving market's since orders: %v", err)
	}
	if len(sinceOrders) != numToDo/4 {
		t.Fatalf("expected %d orders for market with since time, got %d", numToDo/4, len(sinceOrders))
	}
	for _, mord := range sinceOrders {
		mustContainOrder(t, acctOrders, mord)
	}
	t.Logf("%d milliseconds to read %d time-filtered MetaOrders for market", time.Since(tStart)/time.Millisecond, numToDo/4)

	// Same thing, but only last half
	halfSince := len(sinceOrders) / 2
	nOrders, err := boltdb.MarketOrders(acct1.Host, base1, quote1, halfSince, tMid)
	if err != nil {
		t.Fatalf("error returning n orders: %v", err)
	}
	if len(nOrders) != halfSince {
		t.Fatalf("requested %d orders, got %d", halfSince, len(nOrders))
	}
	// Should match exactly with first half of sinceOrders.
	for i := 0; i < halfSince; i++ {
		ordertest.MustCompareOrders(t, nOrders[i].Order, sinceOrders[i].Order)
	}

	// Make a MetaOrder and check insertion errors.
	m := &db.MetaOrder{
		MetaData: &db.OrderMetaData{
			Status: order.OrderStatusExecuted,
			Host:   acct1.Host,
			Proof:  db.OrderProof{DEXSig: randBytes(73)},
		},
		Order: randOrderForMarket(base1, quote1),
	}

	host := m.MetaData.Host
	m.MetaData.Host = ""
	err = boltdb.UpdateOrder(m)
	if err == nil {
		t.Fatalf("no error for empty DEX")
	}
	m.MetaData.Host = host

	sig := m.MetaData.Proof.DEXSig
	m.MetaData.Proof.DEXSig = nil
	err = boltdb.UpdateOrder(m)
	if err == nil {
		t.Fatalf("no error for empty DEX signature")
	}
	m.MetaData.Proof.DEXSig = sig

	err = boltdb.UpdateOrder(m)
	if err != nil {
		t.Fatalf("error after order fixed: %v", err)
	}

	// Set the change coin for an order.
	activeOrder := activeOrders[0].Order
	err = boltdb.UpdateOrderStatus(activeOrder.ID(), order.OrderStatusExecuted)
	if err != nil {
		t.Fatalf("error setting order status: %v", err)
	}
	mord, err = boltdb.Order(activeOrder.ID())
	if mord.MetaData.Status != order.OrderStatusExecuted {
		t.Fatalf("failed to update order status")
	}
	if err != nil {
		t.Fatalf("failed to load order: %v", err)
	}
	// random id should be an error
	err = boltdb.UpdateOrderStatus(ordertest.RandomOrderID(), order.OrderStatusExecuted)
	if err == nil {
		t.Fatalf("no error encountered for updating unknown order's status")
	}
}

func TestOrderFilters(t *testing.T) {
	boltdb, shutdown := newTestDB(t)
	defer shutdown()

	makeOrder := func(host string, base, quote uint32, stamp int64, status order.OrderStatus) *db.MetaOrder {
		mord := &db.MetaOrder{
			MetaData: &db.OrderMetaData{
				Status: status,
				Host:   host,
				Proof:  db.OrderProof{DEXSig: randBytes(73)},
			},
			Order: &order.LimitOrder{
				P: order.Prefix{
					BaseAsset:  base,
					QuoteAsset: quote,
					ServerTime: encode.UnixTimeMilli(stamp),
				},
			},
		}
		oid := mord.Order.ID()

		err := boltdb.UpdateOrder(mord)
		if err != nil {
			t.Fatalf("error inserting order: %v", err)
		}

		// Set the update time.
		boltdb.ordersUpdate(func(aob, eob *bbolt.Bucket) error {
			oBkt := aob.Bucket(oid[:])
			if oBkt == nil {
				oBkt = eob.Bucket(oid[:])
			}
			if oBkt == nil {
				t.Fatalf("order %s not found", oid)
			}
			oBkt.Put(updateTimeKey, uint64Bytes(uint64(stamp)))
			return nil
		})

		return mord
	}

	var start int64
	host1 := "somehost.co"
	host2 := "anotherhost.org"
	var asset1 uint32 = 1
	var asset2 uint32 = 2
	var asset3 uint32 = 3
	orders := []*db.MetaOrder{
		makeOrder(host1, asset1, asset2, start, order.OrderStatusExecuted),   // 0, oid: 95318d1d4d1d19348d96f260e6d54eca942ce9bf0760f43edc7afa2c0173a401
		makeOrder(host2, asset2, asset3, start+1, order.OrderStatusRevoked),  // 1, oid: 58148721dd3109647fd912fb1b3e29be7c72e72cf589dcbe5d0697735e1df8bb
		makeOrder(host1, asset3, asset1, start+2, order.OrderStatusCanceled), // 2, oid: feea1852996c042174a40a88e74175f95009ce72b31d51402f452b28f2618a13
		makeOrder(host2, asset1, asset3, start+3, order.OrderStatusEpoch),    // 3, oid: 8707caf2e70bc615845673cf30e44c67dec972064ab137a321d9ee98e8c96fe3
		makeOrder(host1, asset2, asset3, start+4, order.OrderStatusBooked),   // 4, oid: e2fe7b28eae9a4511013ecb35be58e8031e5f7ec9f3a0e2f6411ec58efd4464a
		makeOrder(host2, asset3, asset1, start+4, order.OrderStatusExecuted), // 5, oid: c76d4dfbc4ea8e0e8065e809f9c3ebfca98a1053a42f464e7632f79126f752d0
	}
	orderCount := len(orders)

	for i, ord := range orders {
		fmt.Println(i, ord.Order.ID().String())
	}

	tests := []struct {
		name     string
		filter   *db.OrderFilter
		expected []int
	}{
		{
			name: "zero-filter",
			filter: &db.OrderFilter{
				N: orderCount,
			},
			expected: []int{4, 5, 3, 2, 1, 0},
		},
		{
			name: "all-hosts",
			filter: &db.OrderFilter{
				N:     orderCount,
				Hosts: []string{host1, host2},
			},
			expected: []int{4, 5, 3, 2, 1, 0},
		},
		{
			name: "host1",
			filter: &db.OrderFilter{
				N:     orderCount,
				Hosts: []string{host1},
			},
			expected: []int{4, 2, 0},
		},
		{
			name: "host1 + asset1",
			filter: &db.OrderFilter{
				N:      orderCount,
				Hosts:  []string{host1},
				Assets: []uint32{asset1},
			},
			expected: []int{2, 0},
		},
		{
			name: "asset1",
			filter: &db.OrderFilter{
				N:      orderCount,
				Assets: []uint32{asset1},
			},
			expected: []int{5, 3, 2, 0},
		},
		// Open filter with last order as Offset should return all but that
		// order, since order 5 is lexicographically after order 4.
		{
			name: "offset",
			filter: &db.OrderFilter{
				N:      orderCount,
				Offset: orders[5].Order.ID(),
			},
			expected: []int{4, 3, 2, 1, 0},
		},
		{
			name: "epoch & booked",
			filter: &db.OrderFilter{
				N:        orderCount,
				Statuses: []order.OrderStatus{order.OrderStatusEpoch, order.OrderStatusBooked},
			},
			expected: []int{4, 3},
		},
	}

	for _, test := range tests {
		ords, err := boltdb.Orders(test.filter)
		if err != nil {
			t.Fatalf("%s: Orders error: %v", test.name, err)
		}
		if len(ords) != len(test.expected) {
			t.Fatalf("%s: wrong number of orders. wanted %d, got %d", test.name, len(test.expected), len(ords))
		}
		for i, j := range test.expected {
			if ords[i].Order.ID() != orders[j].Order.ID() {
				t.Fatalf("%s: index %d wrong ID. wanted %s, got %s", test.name, i, orders[j].Order.ID(), ords[i].Order.ID())
			}
		}
	}
}

func TestOrderChange(t *testing.T) {
	boltdb, shutdown := newTestDB(t)
	defer shutdown()

	// Create an account to use.
	acct := dbtest.RandomAccountInfo()
	err := boltdb.CreateAccount(acct)
	if err != nil {
		t.Fatalf("CreateAccount error: %v", err)
	}

	ord := randOrderForMarket(randU32(), randU32())
	mordIn := &db.MetaOrder{
		MetaData: &db.OrderMetaData{
			Status: 3,
			Host:   acct.Host,
			Proof:  db.OrderProof{DEXSig: randBytes(73)},
		},
		Order: ord,
	}
	// fmt.Printf("%#v\n", mordIn.MetaData.ChangeCoin) // order.CoinID(nil)

	err = boltdb.UpdateOrder(mordIn)
	if err != nil {
		t.Fatalf("error inserting order: %v", err)
	}

	mord, err := boltdb.Order(ord.ID())
	if err != nil {
		t.Fatalf("unable to retrieve order by id")
	}
	if mord.MetaData.ChangeCoin != nil {
		t.Errorf("ChangeCoin was not nil: %#v", mord.MetaData.ChangeCoin)
	}

	// non-nil empty loads as nil too
	mord.MetaData.ChangeCoin = []byte{}
	err = boltdb.UpdateOrderMetaData(ord.ID(), mord.MetaData)
	if err != nil {
		t.Fatalf("error setting change coin: %v", err)
	}
	mord, err = boltdb.Order(ord.ID())
	if err != nil {
		t.Fatalf("failed to load order: %v", err)
	}
	if mord.MetaData.ChangeCoin != nil {
		t.Fatalf("failed to set change coin, got %#v, want <nil>", mord.MetaData.ChangeCoin)
	}

	// now some data
	someChange := []byte{1, 2, 3}
	mord.MetaData.ChangeCoin = someChange
	err = boltdb.UpdateOrderMetaData(ord.ID(), mord.MetaData)
	if err != nil {
		t.Fatalf("error setting change coin: %v", err)
	}
	// Add a linked order ID.
	linkedID := ordertest.RandomOrderID()
	err = boltdb.LinkOrder(ord.ID(), linkedID)
	if err != nil {
		t.Fatalf("error setting linked order: %v", err)
	}
	mord, err = boltdb.Order(ord.ID())
	if err != nil {
		t.Fatalf("failed to load order: %v", err)
	}
	if !bytes.Equal(someChange, mord.MetaData.ChangeCoin) {
		t.Fatalf("failed to set change coin, got %#v, want %#v", mord.MetaData.ChangeCoin, someChange)
	}
	if mord.MetaData.LinkedOrder != linkedID {
		t.Fatalf("wrong linked ID. expected %s, got %s", linkedID, mord.MetaData.LinkedOrder)
	}

	// random id should be an error
	err = boltdb.UpdateOrderMetaData(ordertest.RandomOrderID(), mord.MetaData)
	if err == nil {
		t.Fatalf("no error encountered for updating unknown order change coin")
	}
}

func TestMatches(t *testing.T) {
	boltdb, shutdown := newTestDB(t)
	defer shutdown()

	base, quote := randU32(), randU32()
	acct := dbtest.RandomAccountInfo()

	numToDo := 1000 // must be quadruply a multiple of 8.
	numActive := 100
	if testing.Short() {
		numToDo = 24
		numActive = 8
	}
	metaMatches := make([]*db.MetaMatch, 0, numToDo)
	matchIndex := make(map[order.MatchID]*db.MetaMatch, numToDo)
	nTimes(numToDo, func(i int) {
		m := &db.MetaMatch{
			MetaData: &db.MatchMetaData{
				Proof: *dbtest.RandomMatchProof(0.5),
				DEX:   acct.Host,
				Base:  base,
				Quote: quote,
				Stamp: rand.Uint64(),
			},
			UserMatch: ordertest.RandomUserMatch(),
		}
		if i < numActive {
			m.Status = order.MatchStatus(rand.Intn(4))
		} else {
			m.Status = order.MatchComplete              // inactive
			m.MetaData.Proof.Auth.RedeemSig = []byte{0} // redeemSig required for MatchComplete to be considered inactive
		}
		matchIndex[m.MatchID] = m
		metaMatches = append(metaMatches, m)
	})
	tStart := time.Now()
	nTimes(numToDo, func(i int) {
		err := boltdb.UpdateMatch(metaMatches[i])
		if err != nil {
			t.Fatalf("update error: %v", err)
		}
	})
	t.Logf("%d milliseconds to insert %d account MetaMatch", time.Since(tStart)/time.Millisecond, numToDo)

	tStart = time.Now()
	activeMatches, err := boltdb.ActiveMatches()
	if err != nil {
		t.Fatalf("error getting active matches: %v", err)
	}
	if len(activeMatches) != numActive {
		t.Fatalf("expected %d active matches, got %d", numActive, len(activeMatches))
	}
	activeOrders := make(map[order.OrderID]bool)
	for _, m1 := range activeMatches {
		activeOrders[m1.OrderID] = true
		m2 := matchIndex[m1.MatchID]
		ordertest.MustCompareUserMatch(t, m1.UserMatch, m2.UserMatch)
		dbtest.MustCompareMatchMetaData(t, m1.MetaData, m2.MetaData)
	}
	t.Logf("%d milliseconds to retrieve and compare %d active MetaMatch", time.Since(tStart)/time.Millisecond, numActive)

	activeMatchOrders, err := boltdb.DEXOrdersWithActiveMatches(acct.Host)
	if err != nil {
		t.Fatalf("error retrieving active match orders: %v", err)
	}
	if len(activeMatchOrders) != len(activeOrders) {
		t.Fatalf("wrong number of DEXOrdersWithActiveMatches returned. expected %d, got %d", len(activeOrders), len(activeMatchOrders))
	}
	for _, oid := range activeMatchOrders {
		if !activeOrders[oid] {
			t.Fatalf("active match order ID mismatch")
		}
	}

	m := &db.MetaMatch{
		MetaData: &db.MatchMetaData{
			Proof: *dbtest.RandomMatchProof(0.5),
			DEX:   acct.Host,
			Base:  base,
			Quote: quote,
			Stamp: rand.Uint64(),
		},
		UserMatch: ordertest.RandomUserMatch(),
	}
	m.Status = order.NewlyMatched

	m.MetaData.DEX = ""
	err = boltdb.UpdateMatch(m)
	if err == nil {
		t.Fatalf("no error on empty DEX")
	}
	m.MetaData.DEX = acct.Host

	m.MetaData.Base, m.MetaData.Quote = 0, 0
	err = boltdb.UpdateMatch(m)
	if err == nil {
		t.Fatalf("no error on same base and quote")
	}
	m.MetaData.Base, m.MetaData.Quote = base, quote

	err = boltdb.UpdateMatch(m)
	if err != nil {
		t.Fatalf("error after fixing match: %v", err)
	}
}

var randU32 = func() uint32 { return uint32(rand.Int31()) }

func nTimes(n int, f func(int)) {
	for i := 0; i < n; i++ {
		f(i)
	}
}

func randBytes(l int) []byte {
	b := make([]byte, l)
	rand.Read(b)
	return b
}

func TestNotifications(t *testing.T) {
	boltdb, shutdown := newTestDB(t)
	defer shutdown()

	numToDo := 1000
	numToFetch := 200
	if testing.Short() {
		numToDo = 10
		numToFetch = 2
	}
	newest := uint64(rand.Int63()) - uint64(numToFetch)

	notes := make([]*db.Notification, 0, numToDo)
	nTimes(numToDo, func(int) {
		notes = append(notes, dbtest.RandomNotification(newest))
	})

	fetches := make([]*db.Notification, numToFetch)
	for i := numToFetch - 1; i >= 0; i-- {
		newest++
		notes[i].TimeStamp = newest
		fetches[i] = notes[i]
	}

	tStart := time.Now()
	nTimes(numToDo, func(i int) {
		err := boltdb.SaveNotification(notes[i])
		if err != nil {
			t.Fatalf("SaveNotification error: %v", err)
		}
	})
	t.Logf("%d milliseconds to insert %d active Notification", time.Since(tStart)/time.Millisecond, numToDo)

	tStart = time.Now()
	fetched, err := boltdb.NotificationsN(numToFetch)
	if err != nil {
		t.Fatalf("fetch error: %v", err)
	}
	t.Logf("%d milliseconds to fetch %d sorted Notification", time.Since(tStart)/time.Millisecond, numToFetch)
	if len(fetched) != numToFetch {
		t.Fatalf("fetched wrong number of notifications. %d != %d", len(fetched), numToFetch)
	}

	for i, note := range fetched {
		dbtest.MustCompareNotifications(t, note, fetches[i])
		boltdb.AckNotification(note.ID())
	}

	fetched, err = boltdb.NotificationsN(numToFetch)
	if err != nil {
		t.Fatalf("fetch error after acks: %v", err)
	}

	for _, note := range fetched {
		if !note.Ack {
			t.Fatalf("order acknowledgement not recorded")
		}
	}
}

type tCrypter struct {
	enc []byte
}

func (c *tCrypter) Encrypt(b []byte) ([]byte, error) {
	return c.enc, nil
}

func (c *tCrypter) Decrypt(b []byte) ([]byte, error) {
	return b, nil
}

func (c *tCrypter) Serialize() []byte {
	return nil
}

func (c *tCrypter) Close() {}

func TestRecrypt(t *testing.T) {
	boltdb, shutdown := newTestDB(t)
	defer shutdown()

	tester := func(walletID []byte, host string, creds *db.PrimaryCredentials) error {
		encPW := randBytes(5)
		oldCrypter, newCrypter := &tCrypter{}, &tCrypter{encPW}
		walletUpdates, acctUpdates, err := boltdb.Recrypt(creds, oldCrypter, newCrypter)
		if err != nil {
			return err
		}

		if len(walletUpdates) != 1 {
			return fmt.Errorf("expected 1 wallet update, got %d", len(walletUpdates))
		}
		for _, newEncPW := range walletUpdates {
			if len(newEncPW) == 0 {
				return errors.New("no updated key")
			}
			if !bytes.Equal(newEncPW, encPW) {
				return fmt.Errorf("wrong encrypted password. wanted %x, got %x", encPW, newEncPW)
			}
		}

		w, err := boltdb.Wallet(walletID)
		if err != nil {
			return fmt.Errorf("error retrieving wallet: %v", err)
		}
		if !bytes.Equal(w.EncryptedPW, encPW) {
			return fmt.Errorf("wallet not wallet updated")
		}

		if len(acctUpdates) != 1 {
			return fmt.Errorf("expected 1 account update, got %d", len(acctUpdates))
		}
		newEncPW := acctUpdates[host]
		if len(newEncPW) == 0 {
			return fmt.Errorf("no account update")
		}
		if !bytes.Equal(newEncPW, encPW) {
			return fmt.Errorf("wrong encrypted account password")
		}

		acct, err := boltdb.Account(host)
		if err != nil {
			return fmt.Errorf("error retrieving account: %v", err)
		}
		if !bytes.Equal(acct.LegacyEncKey, encPW) {
			return fmt.Errorf("account not updated")
		}

		return nil
	}
	testCredentialsUpdate(t, boltdb, tester)
}

func testCredentialsUpdate(t *testing.T, boltdb *BoltDB, tester func([]byte, string, *db.PrimaryCredentials) error) {
	t.Helper()
	w := dbtest.RandomWallet()
	w.EncryptedPW = randBytes(6)
	walletID := w.ID()
	boltdb.UpdateWallet(w)

	acct := dbtest.RandomAccountInfo()
	acct.LegacyEncKey = acct.EncKeyV2
	acct.EncKeyV2 = nil
	boltdb.CreateAccount(acct)
	host := acct.Host

	clearCreds := func() {
		boltdb.Update(func(tx *bbolt.Tx) error {
			credsBkt := tx.Bucket(credentialsBucket)
			credsBkt.Delete(encSeedKey)
			credsBkt.Delete(encInnerKeyKey)
			credsBkt.Delete(innerKeyParamsKey)
			credsBkt.Delete(outerKeyParamsKey)
			return nil
		})
	}

	numToDo := 100
	if testing.Short() {
		numToDo = 5
	}

	nTimes(numToDo, func(int) {
		clearCreds()
		creds := dbtest.RandomPrimaryCredentials()
		err := tester(walletID, host, creds)
		if err != nil {
			t.Fatalf("%T error: %v", tester, err)
		}
	})
}
