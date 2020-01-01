package boltdb

import (
	"context"
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"decred.org/dcrdex/client/db"
	dbtest "decred.org/dcrdex/client/db/test"
	"decred.org/dcrdex/dex/order"
	ordertest "decred.org/dcrdex/dex/order/test"
)

var (
	tDir     string
	tCtx     context.Context
	tCounter int
)

func newTestDB(t *testing.T) *boltDB {
	tCounter++
	dbPath := filepath.Join(tDir, fmt.Sprintf("db%d.db", tCounter))
	dbi, err := NewDB(tCtx, dbPath)
	if err != nil {
		t.Fatalf("error creating dB: %v", err)
	}
	db, ok := dbi.(*boltDB)
	if !ok {
		t.Fatalf("DB is not a *boltDB")
	}
	return db
}

func TestMain(m *testing.M) {
	doIt := func() int {
		var err error
		tDir, err = ioutil.TempDir("", "dbtest")
		if err != nil {
			fmt.Println("error creating temporary directory:", err)
			return -1
		}
		defer os.RemoveAll(tDir)
		var shutdown func()
		tCtx, shutdown = context.WithCancel(context.Background())
		defer shutdown()
		return m.Run()
	}
	os.Exit(doIt())
}

func TestAccounts(t *testing.T) {
	boltdb := newTestDB(t)
	dexURLs := boltdb.ListAccounts()
	if len(dexURLs) != 0 {
		t.Fatalf("unexpected non-empty accounts in fresh DB")
	}
	// Create and insert 1,000 accounts.
	numToDo := 1000
	if testing.Short() {
		numToDo /= 4
	}
	accts := make([]*db.AccountInfo, 0, numToDo)
	nTimes(numToDo, func(int) { accts = append(accts, dbtest.RandomAccountInfo()) })
	tStart := time.Now()
	nTimes(numToDo, func(i int) {
		boltdb.CreateAccount(accts[i])
	})
	t.Logf("%d milliseconds to insert %d AccountInfo", time.Since(tStart)/time.Millisecond, numToDo)
	tStart = time.Now()
	nTimes(numToDo, func(i int) {
		ai := accts[i]
		reAI, err := boltdb.Account(ai.URL)
		if err != nil {
			t.Fatalf("error fetching AccountInfo")
		}
		dbtest.MustCompareAccountInfo(t, ai, reAI)
	})
	t.Logf("%d milliseconds to read and compare %d AccountInfo", time.Since(tStart)/time.Millisecond, numToDo)

	dexURLs = boltdb.ListAccounts()
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

	url := acct.URL
	acct.URL = ""
	ensureErr("URL")
	acct.URL = url

	dexKey := acct.DEXPubKey
	acct.DEXPubKey = nil
	ensureErr("DEX key")
	acct.DEXPubKey = dexKey

	encKey := acct.EncKey
	acct.EncKey = nil
	ensureErr("private key")
	acct.EncKey = encKey

	err := boltdb.CreateAccount(acct)
	if err != nil {
		t.Fatalf("failed to create account after fixing")
	}
}

func randOrderForMarket(base, quote uint32) order.Order {
	switch rand.Intn(3) {
	case 0:
		o := ordertest.RandomCancelOrder()
		o.BaseAsset = base
		o.QuoteAsset = quote
		return o
	case 1:
		o := ordertest.RandomMarketOrder()
		o.BaseAsset = base
		o.QuoteAsset = quote
		return o
	default:
		o := ordertest.RandomLimitOrder()
		o.BaseAsset = base
		o.QuoteAsset = quote
		return o
	}
}

func mustContainOrder(t *testing.T, os []*db.MetaOrder, o *db.MetaOrder) {
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
	boltdb := newTestDB(t)
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
		numToDo /= 4
		numActive /= 4
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
				Status: status,
				DEX:    acct.URL,
				Proof:  db.OrderProof{DEXSig: randBytes(73)},
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
	firstOrd := orders[0].Order
	mord, err := boltdb.Order(firstOrd.ID())
	if err != nil {
		t.Fatalf("unable to retrieve order by id")
	}
	ordertest.MustCompareOrders(t, firstOrd, mord.Order)

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
	acctOrders, err := boltdb.AccountOrders(acct1.URL, 0, 0)
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
	sinceOrders, err := boltdb.AccountOrders(acct1.URL, 0, tMid)
	if err != nil {
		t.Fatalf("error retreiving account's since orders: %v", err)
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
	mktOrders, err := boltdb.MarketOrders(acct1.URL, base1, quote1, 0, 0)
	if err != nil {
		t.Fatalf("error retrieving orders for market: %v", err)
	}
	if len(mktOrders) != numToDo/2 {
		t.Fatalf("expected %d orders for market, got %d", numToDo/2, len(mktOrders))
	}
	t.Logf("%d milliseconds to read and compare %d MetaOrder for market", time.Since(tStart)/time.Millisecond, numToDo/2)

	// Filter the market's first half out by timestamp.
	tStart = time.Now()
	sinceOrders, err = boltdb.MarketOrders(acct1.URL, base1, quote1, 0, tMid)
	if err != nil {
		t.Fatalf("error retreiving market's since orders: %v", err)
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
	nOrders, err := boltdb.MarketOrders(acct1.URL, base1, quote1, halfSince, tMid)
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
			DEX:    acct1.URL,
			Proof:  db.OrderProof{DEXSig: randBytes(73)},
		},
		Order: randOrderForMarket(base1, quote1),
	}

	dex := m.MetaData.DEX
	m.MetaData.DEX = ""
	err = boltdb.UpdateOrder(m)
	if err == nil {
		t.Fatalf("no error for empty DEX")
	}
	m.MetaData.DEX = dex

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
}

func TestMatches(t *testing.T) {
	boltdb := newTestDB(t)
	base, quote := randU32(), randU32()
	acct := dbtest.RandomAccountInfo()

	numToDo := 1000 // must be quadruply even e.g. numToDo/4 is even.
	numActive := 100
	if testing.Short() {
		numToDo /= 4
		numActive /= 4
	}
	metaMatches := make([]*db.MetaMatch, 0, numToDo)
	matchIndex := make(map[order.MatchID]*db.MetaMatch, numToDo)
	nTimes(numToDo, func(i int) {
		status := order.MatchComplete // inactive
		if i < numActive {
			status = order.MatchStatus(rand.Intn(4))
		}
		m := &db.MetaMatch{
			MetaData: &db.MatchMetaData{
				Status: status,
				Proof:  *dbtest.RandomMatchProof(0.5),
				DEX:    acct.URL,
				Base:   base,
				Quote:  quote,
			},
			Match: ordertest.RandomUserMatch(),
		}
		matchIndex[m.Match.MatchID] = m
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
	for _, m1 := range activeMatches {
		m2 := matchIndex[m1.Match.MatchID]
		ordertest.MustCompareUserMatch(t, m1.Match, m2.Match)
		dbtest.MustCompareMatchProof(t, &m1.MetaData.Proof, &m2.MetaData.Proof)
	}
	t.Logf("%d milliseconds to retrieve and compare %d active MetaMatch", time.Since(tStart)/time.Millisecond, numActive)

	m := &db.MetaMatch{
		MetaData: &db.MatchMetaData{
			Status: order.NewlyMatched,
			Proof:  *dbtest.RandomMatchProof(0.5),
			DEX:    acct.URL,
			Base:   base,
			Quote:  quote,
		},
		Match: ordertest.RandomUserMatch(),
	}

	m.MetaData.DEX = ""
	err = boltdb.UpdateMatch(m)
	if err == nil {
		t.Fatalf("no error on empty DEX")
	}
	m.MetaData.DEX = acct.URL

	m.MetaData.Base, m.MetaData.Quote = 0, 0
	err = boltdb.UpdateMatch(m)
	if err == nil {
		t.Fatalf("no error on double zero base/quote")
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
