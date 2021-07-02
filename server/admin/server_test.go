// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package admin

import (
	"bytes"
	"context"
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/server/account"
	"decred.org/dcrdex/server/asset"
	"decred.org/dcrdex/server/db"
	dexsrv "decred.org/dcrdex/server/dex"
	"decred.org/dcrdex/server/market"
	"github.com/decred/dcrd/certgen"
	"github.com/decred/slog"
	"github.com/go-chi/chi/v5"
)

func init() {
	log = slog.NewBackend(os.Stdout).Logger("TEST")
	log.SetLevel(slog.LevelTrace)
}

type TMarket struct {
	running     bool
	dur         uint64
	suspend     *market.SuspendEpoch
	startEpoch  int64
	activeEpoch int64
	resumeEpoch int64
	resumeTime  time.Time
	persist     bool
}

type TCore struct {
	markets          map[string]*TMarket
	accounts         []*db.Account
	accountsErr      error
	account          *db.Account
	accountErr       error
	penalizeErr      error
	unbanErr         error
	book             []*order.LimitOrder
	bookErr          error
	epochOrders      []order.Order
	epochOrdersErr   error
	marketMatches    []*dexsrv.MatchData
	marketMatchesErr error
	dataEnabled      uint32
}

func (c *TCore) ConfigMsg() json.RawMessage { return nil }

func (c *TCore) Suspend(tSusp time.Time, persistBooks bool) map[string]*market.SuspendEpoch {
	return nil
}
func (c *TCore) ResumeMarket(name string, tRes time.Time) (startEpoch int64, startTime time.Time, err error) {
	tMkt := c.markets[name]
	if tMkt == nil {
		err = fmt.Errorf("unknown market %s", name)
		return
	}
	tMkt.resumeEpoch = 1 + tRes.UnixMilli()/int64(tMkt.dur)
	tMkt.resumeTime = time.UnixMilli(tMkt.resumeEpoch * int64(tMkt.dur))
	return tMkt.resumeEpoch, tMkt.resumeTime, nil
}
func (c *TCore) SuspendMarket(name string, tSusp time.Time, persistBooks bool) (suspEpoch *market.SuspendEpoch, err error) {
	tMkt := c.markets[name]
	if tMkt == nil {
		err = fmt.Errorf("unknown market %s", name)
		return
	}
	tMkt.persist = persistBooks
	tMkt.suspend.Idx = tSusp.UnixMilli()
	tMkt.suspend.End = tSusp.Add(time.Millisecond)
	return tMkt.suspend, nil
}

func (c *TCore) market(name string) *TMarket {
	if c.markets == nil {
		return nil
	}
	return c.markets[name]
}

func (c *TCore) MarketStatus(mktName string) *market.Status {
	mkt := c.market(mktName)
	if mkt == nil {
		return nil
	}
	var suspendEpoch int64
	if mkt.suspend != nil {
		suspendEpoch = mkt.suspend.Idx
	}
	return &market.Status{
		Running:       mkt.running,
		EpochDuration: mkt.dur,
		ActiveEpoch:   mkt.activeEpoch,
		StartEpoch:    mkt.startEpoch,
		SuspendEpoch:  suspendEpoch,
		PersistBook:   mkt.persist,
	}
}

func (c *TCore) Asset(id uint32) (*asset.BackedAsset, error)     { return nil, fmt.Errorf("not tested") }
func (c *TCore) SetFeeRateScale(assetID uint32, scale float64)   {}
func (c *TCore) ScaleFeeRate(assetID uint32, rate uint64) uint64 { return 1 }

func (c *TCore) BookOrders(_, _ uint32) ([]*order.LimitOrder, error) {
	return c.book, c.bookErr
}

func (c *TCore) EpochOrders(_, _ uint32) ([]order.Order, error) {
	return c.epochOrders, c.epochOrdersErr
}

func (c *TCore) MarketMatchesStreaming(base, quote uint32, includeInactive bool, N int64, f func(*dexsrv.MatchData) error) (int, error) {
	if c.marketMatchesErr != nil {
		return 0, c.marketMatchesErr
	}
	for _, mm := range c.marketMatches {
		if err := f(mm); err != nil {
			return 0, err
		}
	}
	return len(c.marketMatches), nil
}

func (c *TCore) MarketStatuses() map[string]*market.Status {
	mktStatuses := make(map[string]*market.Status, len(c.markets))
	for name, mkt := range c.markets {
		var suspendEpoch int64
		if mkt.suspend != nil {
			suspendEpoch = mkt.suspend.Idx
		}
		mktStatuses[name] = &market.Status{
			Running:       mkt.running,
			EpochDuration: mkt.dur,
			ActiveEpoch:   mkt.activeEpoch,
			StartEpoch:    mkt.startEpoch,
			SuspendEpoch:  suspendEpoch,
			PersistBook:   mkt.persist,
		}
	}
	return mktStatuses
}

func (c *TCore) MarketRunning(mktName string) (found, running bool) {
	mkt := c.market(mktName)
	if mkt == nil {
		return
	}
	return true, mkt.running
}

func (c *TCore) EnableDataAPI(yes bool) {
	var v uint32
	if yes {
		v = 1
	}
	atomic.StoreUint32(&c.dataEnabled, v)
}

type tResponseWriter struct {
	b    []byte
	code int
}

func (w *tResponseWriter) Header() http.Header {
	return make(http.Header)
}
func (w *tResponseWriter) Write(msg []byte) (int, error) {
	w.b = msg
	return len(msg), nil
}
func (w *tResponseWriter) WriteHeader(statusCode int) {
	w.code = statusCode
}

func (c *TCore) Accounts() ([]*db.Account, error) { return c.accounts, c.accountsErr }
func (c *TCore) AccountInfo(_ account.AccountID) (*db.Account, error) {
	return c.account, c.accountErr
}
func (c *TCore) Penalize(_ account.AccountID, _ account.Rule, _ string) error {
	return c.penalizeErr
}
func (c *TCore) Unban(_ account.AccountID) error {
	return c.unbanErr
}
func (c *TCore) ForgiveMatchFail(_ account.AccountID, _ order.MatchID) (bool, bool, error) {
	return false, false, nil // TODO: tests
}
func (c *TCore) Notify(_ account.AccountID, _ *msgjson.Message) {}
func (c *TCore) NotifyAll(_ *msgjson.Message)                   {}

// genCertPair generates a key/cert pair to the paths provided.
func genCertPair(certFile, keyFile string) error {
	log.Infof("Generating TLS certificates...")

	org := "dcrdex autogenerated cert"
	validUntil := time.Now().Add(10 * 365 * 24 * time.Hour)
	cert, key, err := certgen.NewTLSCertPair(elliptic.P521(), org,
		validUntil, nil)
	if err != nil {
		return err
	}

	// Write cert and key files.
	if err = os.WriteFile(certFile, cert, 0644); err != nil {
		return err
	}
	if err = os.WriteFile(keyFile, key, 0600); err != nil {
		os.Remove(certFile)
		return err
	}

	log.Infof("Done generating TLS certificates")
	return nil
}

var tPort = 5555

// If start is true, the Server's Run goroutine is started, and the shutdown
// func must be called when finished with the Server.
func newTServer(t *testing.T, start bool, authSHA [32]byte) (*Server, func()) {
	tmp := t.TempDir()

	cert, key := filepath.Join(tmp, "tls.cert"), filepath.Join(tmp, "tls.key")
	err := genCertPair(cert, key)
	if err != nil {
		t.Fatal(err)
	}

	s, err := NewServer(&SrvConfig{
		Core:    new(TCore),
		Addr:    fmt.Sprintf("localhost:%d", tPort),
		Cert:    cert,
		Key:     key,
		AuthSHA: authSHA,
	})
	if err != nil {
		t.Fatalf("error creating Server: %v", err)
	}
	if !start {
		return s, func() {}
	}

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		s.Run(ctx)
		wg.Done()
	}()
	shutdown := func() {
		cancel()
		wg.Wait()
	}
	return s, shutdown
}

func TestPing(t *testing.T) {
	w := httptest.NewRecorder()
	apiPing(w, nil)
	if w.Code != 200 {
		t.Fatalf("apiPing returned code %d, expected 200", w.Code)
	}

	resp := w.Result()
	ctHdr := resp.Header.Get("Content-Type")
	wantCt := "application/json; charset=utf-8"
	if ctHdr != wantCt {
		t.Errorf("Content-Type incorrect. got %q, expected %q", ctHdr, wantCt)
	}

	// JSON strings are double quoted. Each value is terminated with a newline.
	expectedBody := `"` + pongStr + `"` + "\n"
	if w.Body == nil {
		t.Fatalf("got empty body")
	}
	gotBody := w.Body.String()
	if gotBody != expectedBody {
		t.Errorf("apiPong response said %q, expected %q", gotBody, expectedBody)
	}
}

func TestMarkets(t *testing.T) {
	core := &TCore{
		markets: make(map[string]*TMarket),
	}
	srv := &Server{
		core: core,
	}

	mux := chi.NewRouter()
	mux.Get("/markets", srv.apiMarkets)

	// No markets.
	w := httptest.NewRecorder()
	r, _ := http.NewRequest(http.MethodGet, "https://localhost/markets", nil)
	r.RemoteAddr = "localhost"

	mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("apiMarkets returned code %d, expected %d", w.Code, http.StatusOK)
	}
	respBody := w.Body.String()
	if respBody != "{}\n" {
		t.Errorf("incorrect response body: %q", respBody)
	}

	// A market.
	dur := uint64(1234)
	idx := int64(12345)
	tMkt := &TMarket{
		running:     true,
		dur:         dur,
		startEpoch:  12340,
		activeEpoch: 12343,
	}
	core.markets["dcr_btc"] = tMkt

	w = httptest.NewRecorder()
	r, _ = http.NewRequest(http.MethodGet, "https://localhost/markets", nil)
	r.RemoteAddr = "localhost"

	mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("apiMarkets returned code %d, expected %d", w.Code, http.StatusOK)
	}

	exp := `{
    "dcr_btc": {
        "running": true,
        "epochlen": 1234,
        "activeepoch": 12343,
        "startepoch": 12340
    }
}
`
	if exp != w.Body.String() {
		t.Errorf("unexpected response %q, wanted %q", w.Body.String(), exp)
	}

	var mktStatuses map[string]*MarketStatus
	err := json.Unmarshal(w.Body.Bytes(), &mktStatuses)
	if err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	wantMktStatuses := map[string]*MarketStatus{
		"dcr_btc": {
			Running:       true,
			EpochDuration: 1234,
			ActiveEpoch:   12343,
			StartEpoch:    12340,
		},
	}
	if len(wantMktStatuses) != len(mktStatuses) {
		t.Fatalf("got %d market statuses, wanted %d", len(mktStatuses), len(wantMktStatuses))
	}
	for name, stat := range mktStatuses {
		wantStat := wantMktStatuses[name]
		if wantStat == nil {
			t.Fatalf("market %s not expected", name)
		}
		if !reflect.DeepEqual(wantStat, stat) {
			log.Errorf("incorrect market status. got %v, expected %v", stat, wantStat)
		}
	}

	// Set suspend data.
	tMkt.suspend = &market.SuspendEpoch{Idx: 12345, End: time.UnixMilli(int64(dur) * idx)}
	tMkt.persist = true

	w = httptest.NewRecorder()
	r, _ = http.NewRequest(http.MethodGet, "https://localhost/markets", nil)
	r.RemoteAddr = "localhost"

	mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("apiMarkets returned code %d, expected %d", w.Code, http.StatusOK)
	}

	exp = `{
    "dcr_btc": {
        "running": true,
        "epochlen": 1234,
        "activeepoch": 12343,
        "startepoch": 12340,
        "finalepoch": 12345,
        "persistbook": true
    }
}
`
	if exp != w.Body.String() {
		t.Errorf("unexpected response %q, wanted %q", w.Body.String(), exp)
	}

	mktStatuses = nil
	err = json.Unmarshal(w.Body.Bytes(), &mktStatuses)
	if err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	persist := true
	wantMktStatuses = map[string]*MarketStatus{
		"dcr_btc": {
			Running:       true,
			EpochDuration: 1234,
			ActiveEpoch:   12343,
			StartEpoch:    12340,
			SuspendEpoch:  12345,
			PersistBook:   &persist,
		},
	}
	if len(wantMktStatuses) != len(mktStatuses) {
		t.Fatalf("got %d market statuses, wanted %d", len(mktStatuses), len(wantMktStatuses))
	}
	for name, stat := range mktStatuses {
		wantStat := wantMktStatuses[name]
		if wantStat == nil {
			t.Fatalf("market %s not expected", name)
		}
		if !reflect.DeepEqual(wantStat, stat) {
			log.Errorf("incorrect market status. got %v, expected %v", stat, wantStat)
		}
	}
}

func TestMarketInfo(t *testing.T) {

	core := &TCore{
		markets: make(map[string]*TMarket),
	}
	srv := &Server{
		core: core,
	}

	mux := chi.NewRouter()
	mux.Get("/market/{"+marketNameKey+"}", srv.apiMarketInfo)

	// Request a non-existent market.
	w := httptest.NewRecorder()
	name := "dcr_btc"
	r, _ := http.NewRequest(http.MethodGet, "https://localhost/market/"+name, nil)
	r.RemoteAddr = "localhost"

	mux.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("apiMarketInfo returned code %d, expected %d", w.Code, http.StatusBadRequest)
	}
	respBody := w.Body.String()
	if respBody != fmt.Sprintf("unknown market %q\n", name) {
		t.Errorf("incorrect response body: %q", respBody)
	}

	tMkt := &TMarket{}
	core.markets[name] = tMkt

	// Not running market.
	w = httptest.NewRecorder()
	r, _ = http.NewRequest(http.MethodGet, "https://localhost/market/"+name, nil)
	r.RemoteAddr = "localhost"

	mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("apiMarketInfo returned code %d, expected %d", w.Code, http.StatusOK)
	}
	mktStatus := new(MarketStatus)
	err := json.Unmarshal(w.Body.Bytes(), &mktStatus)
	if err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}
	if mktStatus.Name != name {
		t.Errorf("incorrect market name %q, expected %q", mktStatus.Name, name)
	}
	if mktStatus.Running {
		t.Errorf("market should not have been reported as running")
	}

	// Flip the market on.
	core.markets[name].running = true
	core.markets[name].suspend = &market.SuspendEpoch{Idx: 1324, End: time.Now()}
	w = httptest.NewRecorder()
	r, _ = http.NewRequest(http.MethodGet, "https://localhost/market/"+name, nil)
	r.RemoteAddr = "localhost"

	mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("apiMarketInfo returned code %d, expected %d", w.Code, http.StatusOK)
	}
	mktStatus = new(MarketStatus)
	err = json.Unmarshal(w.Body.Bytes(), &mktStatus)
	if err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}
	if mktStatus.Name != name {
		t.Errorf("incorrect market name %q, expected %q", mktStatus.Name, name)
	}
	if !mktStatus.Running {
		t.Errorf("market should have been reported as running")
	}
}

func TestMarketOrderBook(t *testing.T) {
	core := new(TCore)
	core.markets = make(map[string]*TMarket)
	srv := &Server{
		core: core,
	}
	tMkt := &TMarket{
		dur:         1234,
		startEpoch:  12340,
		activeEpoch: 12343,
	}
	core.markets["dcr_btc"] = tMkt
	mux := chi.NewRouter()
	mux.Get("/market/{"+marketNameKey+"}/orderbook", srv.apiMarketOrderBook)
	tests := []struct {
		name, mkt string
		running   bool
		book      []*order.LimitOrder
		bookErr   error
		wantCode  int
	}{{
		name:     "ok",
		mkt:      "dcr_btc",
		running:  true,
		book:     []*order.LimitOrder{},
		wantCode: http.StatusOK,
	}, {
		name:     "no market",
		mkt:      "btc_dcr",
		wantCode: http.StatusBadRequest,
	}, {
		name:     "core.OrderBook error",
		mkt:      "dcr_btc",
		running:  true,
		bookErr:  errors.New(""),
		wantCode: http.StatusInternalServerError,
	}}
	for _, test := range tests {
		core.book = test.book
		core.bookErr = test.bookErr
		tMkt.running = test.running
		w := httptest.NewRecorder()
		r, _ := http.NewRequest(http.MethodGet, "https://localhost/market/"+test.mkt+"/orderbook", nil)
		r.RemoteAddr = "localhost"

		mux.ServeHTTP(w, r)

		if w.Code != test.wantCode {
			t.Fatalf("%q: apiMarketOrderBook returned code %d, expected %d", test.name, w.Code, test.wantCode)
		}
		if w.Code == http.StatusOK {
			res := new(*msgjson.BookOrderNote)
			if err := json.Unmarshal(w.Body.Bytes(), res); err != nil {
				t.Errorf("%q: unexpected response %v: %v", test.name, w.Body.String(), err)
			}
		}
	}
}

func TestMarketEpochOrders(t *testing.T) {
	core := new(TCore)
	core.markets = make(map[string]*TMarket)
	srv := &Server{
		core: core,
	}
	tMkt := &TMarket{
		dur:         1234,
		startEpoch:  12340,
		activeEpoch: 12343,
	}
	core.markets["dcr_btc"] = tMkt
	mux := chi.NewRouter()
	mux.Get("/market/{"+marketNameKey+"}/epochorders", srv.apiMarketEpochOrders)
	tests := []struct {
		name, mkt      string
		running        bool
		orders         []order.Order
		epochOrdersErr error
		wantCode       int
	}{{
		name:     "ok",
		mkt:      "dcr_btc",
		running:  true,
		orders:   []order.Order{},
		wantCode: http.StatusOK,
	}, {
		name:     "no market",
		mkt:      "btc_dcr",
		wantCode: http.StatusBadRequest,
	}, {
		name:           "core.EpochOrders error",
		mkt:            "dcr_btc",
		running:        true,
		epochOrdersErr: errors.New(""),
		wantCode:       http.StatusInternalServerError,
	}}
	for _, test := range tests {
		core.epochOrders = test.orders
		core.epochOrdersErr = test.epochOrdersErr
		tMkt.running = test.running
		w := httptest.NewRecorder()
		r, _ := http.NewRequest(http.MethodGet, "https://localhost/market/"+test.mkt+"/epochorders", nil)
		r.RemoteAddr = "localhost"

		mux.ServeHTTP(w, r)

		if w.Code != test.wantCode {
			t.Fatalf("%q: apiMarketEpochOrders returned code %d, expected %d", test.name, w.Code, test.wantCode)
		}
		if w.Code == http.StatusOK {
			res := new(*msgjson.BookOrderNote)
			if err := json.Unmarshal(w.Body.Bytes(), res); err != nil {
				t.Errorf("%q: unexpected response %v: %v", test.name, w.Body.String(), err)
			}
		}
	}
}

func TestMarketMatches(t *testing.T) {
	core := new(TCore)
	core.markets = make(map[string]*TMarket)
	srv := &Server{
		core: core,
	}
	tMkt := &TMarket{
		dur:         1234,
		startEpoch:  12340,
		activeEpoch: 12343,
	}
	core.markets["dcr_btc"] = tMkt
	mux := chi.NewRouter()
	mux.Get("/market/{"+marketNameKey+"}/matches", srv.apiMarketMatches)
	tests := []struct {
		name, mkt, token    string
		running, tokenValue bool
		marketMatches       []*dexsrv.MatchData
		marketMatchesErr    error
		wantCode            int
	}{{
		name:          "ok no token",
		mkt:           "dcr_btc",
		running:       true,
		marketMatches: []*dexsrv.MatchData{},
		wantCode:      http.StatusOK,
	}, {
		name:          "ok with token",
		mkt:           "dcr_btc",
		running:       true,
		token:         "?" + includeInactiveKey + "=true",
		marketMatches: []*dexsrv.MatchData{},
		wantCode:      http.StatusOK,
	}, {
		name:          "bad token",
		mkt:           "dcr_btc",
		running:       true,
		token:         "?" + includeInactiveKey + "=blue",
		marketMatches: []*dexsrv.MatchData{},
		wantCode:      http.StatusBadRequest,
	}, {
		name:     "no market",
		mkt:      "btc_dcr",
		wantCode: http.StatusBadRequest,
	}, {
		name:             "core.MarketMatchesStreaming error",
		mkt:              "dcr_btc",
		running:          true,
		marketMatchesErr: errors.New("boom"),
		wantCode:         http.StatusInternalServerError,
	}}
	for _, test := range tests {
		core.marketMatches = test.marketMatches
		core.marketMatchesErr = test.marketMatchesErr
		tMkt.running = test.running
		w := httptest.NewRecorder()
		r, _ := http.NewRequest(http.MethodGet, "https://localhost/market/"+test.mkt+"/matches"+test.token, nil)
		r.RemoteAddr = "localhost"

		mux.ServeHTTP(w, r)

		if w.Code != test.wantCode {
			t.Fatalf("%q: apiMarketMatches returned code %d, expected %d", test.name, w.Code, test.wantCode)
		}
		if w.Code != http.StatusOK {
			continue
		}
		dec := json.NewDecoder(w.Body)

		var resi dexsrv.MatchData
		mustDec := func() {
			t.Helper()
			if err := dec.Decode(&resi); err != nil {
				t.Fatalf("%q: Failed to decode element: %v", test.name, err)
			}
		}
		if len(test.marketMatches) > 0 {
			mustDec()
		}
		for dec.More() {
			mustDec()
		}
	}
}

func TestResume(t *testing.T) {
	core := &TCore{
		markets: make(map[string]*TMarket),
	}
	srv := &Server{
		core: core,
	}

	mux := chi.NewRouter()
	mux.Get("/market/{"+marketNameKey+"}/resume", srv.apiResume)

	// Non-existent market
	name := "dcr_btc"
	w := httptest.NewRecorder()
	r, _ := http.NewRequest(http.MethodGet, "https://localhost/market/"+name+"/resume", nil)
	r.RemoteAddr = "localhost"

	mux.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("apiResume returned code %d, expected %d", w.Code, http.StatusBadRequest)
	}

	// With the market, but already running
	tMkt := &TMarket{
		running: true,
		dur:     6000,
	}
	core.markets[name] = tMkt

	w = httptest.NewRecorder()
	r, _ = http.NewRequest(http.MethodGet, "https://localhost/market/"+name+"/resume", nil)
	r.RemoteAddr = "localhost"

	mux.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("apiResume returned code %d, expected %d", w.Code, http.StatusOK)
	}
	wantMsg := "market \"dcr_btc\" running\n"
	if w.Body.String() != wantMsg {
		t.Errorf("expected body %q, got %q", wantMsg, w.Body)
	}

	// Now stopped.
	tMkt.running = false
	w = httptest.NewRecorder()
	r, _ = http.NewRequest(http.MethodGet, "https://localhost/market/"+name+"/resume", nil)
	r.RemoteAddr = "localhost"

	mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("apiResume returned code %d, expected %d", w.Code, http.StatusOK)
	}
	resRes := new(ResumeResult)
	err := json.Unmarshal(w.Body.Bytes(), &resRes)
	if err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}
	if resRes.Market != name {
		t.Errorf("incorrect market name %q, expected %q", resRes.Market, name)
	}

	if resRes.StartTime.IsZero() {
		t.Errorf("start time not set")
	}
	if resRes.StartEpoch == 0 {
		t.Errorf("start epoch not sest")
	}

	// reset
	tMkt.resumeEpoch = 0
	tMkt.resumeTime = time.Time{}

	// Time in past
	w = httptest.NewRecorder()
	r, _ = http.NewRequest(http.MethodGet, "https://localhost/market/"+name+"/resume?t=12", nil)
	r.RemoteAddr = "localhost"

	mux.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("apiResume returned code %d, expected %d", w.Code, http.StatusOK)
	}
	resp := w.Body.String()
	wantPrefix := "specified market resume time is in the past"
	if !strings.HasPrefix(resp, wantPrefix) {
		t.Errorf("Expected error message starting with %q, got %q", wantPrefix, resp)
	}

	// Bad suspend time (not a time)
	w = httptest.NewRecorder()
	r, _ = http.NewRequest(http.MethodGet, "https://localhost/market/"+name+"/resume?t=QWERT", nil)
	r.RemoteAddr = "localhost"

	mux.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("apiResume returned code %d, expected %d", w.Code, http.StatusBadRequest)
	}
	resp = w.Body.String()
	wantPrefix = "invalid resume time"
	if !strings.HasPrefix(resp, wantPrefix) {
		t.Errorf("Expected error message starting with %q, got %q", wantPrefix, resp)
	}
}

func TestSuspend(t *testing.T) {
	core := &TCore{
		markets: make(map[string]*TMarket),
	}
	srv := &Server{
		core: core,
	}

	mux := chi.NewRouter()
	mux.Get("/market/{"+marketNameKey+"}/suspend", srv.apiSuspend)

	// Non-existent market
	name := "dcr_btc"
	w := httptest.NewRecorder()
	r, _ := http.NewRequest(http.MethodGet, "https://localhost/market/"+name+"/suspend", nil)
	r.RemoteAddr = "localhost"

	mux.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("apiSuspend returned code %d, expected %d", w.Code, http.StatusBadRequest)
	}

	// With the market, but not running
	tMkt := &TMarket{
		suspend: &market.SuspendEpoch{},
	}
	core.markets[name] = tMkt

	w = httptest.NewRecorder()
	r, _ = http.NewRequest(http.MethodGet, "https://localhost/market/"+name+"/suspend", nil)
	r.RemoteAddr = "localhost"

	mux.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("apiSuspend returned code %d, expected %d", w.Code, http.StatusOK)
	}
	wantMsg := "market \"dcr_btc\" not running\n"
	if w.Body.String() != wantMsg {
		t.Errorf("expected body %q, got %q", wantMsg, w.Body)
	}

	// Now running.
	tMkt.running = true
	w = httptest.NewRecorder()
	r, _ = http.NewRequest(http.MethodGet, "https://localhost/market/"+name+"/suspend", nil)
	r.RemoteAddr = "localhost"

	mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("apiSuspend returned code %d, expected %d", w.Code, http.StatusOK)
	}
	suspRes := new(SuspendResult)
	err := json.Unmarshal(w.Body.Bytes(), &suspRes)
	if err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}
	if suspRes.Market != name {
		t.Errorf("incorrect market name %q, expected %q", suspRes.Market, name)
	}

	var zeroTime time.Time
	wantIdx := zeroTime.UnixMilli()
	if suspRes.FinalEpoch != wantIdx {
		t.Errorf("incorrect final epoch index. got %d, expected %d",
			suspRes.FinalEpoch, tMkt.suspend.Idx)
	}

	wantFinal := zeroTime.Add(time.Millisecond)
	if !suspRes.SuspendTime.Equal(wantFinal) {
		t.Errorf("incorrect suspend time. got %v, expected %v",
			suspRes.SuspendTime, tMkt.suspend.End)
	}

	// Specify a time in the past.
	w = httptest.NewRecorder()
	r, _ = http.NewRequest(http.MethodGet, "https://localhost/market/"+name+"/suspend?t=12", nil)
	r.RemoteAddr = "localhost"

	mux.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("apiSuspend returned code %d, expected %d", w.Code, http.StatusBadRequest)
	}
	resp := w.Body.String()
	wantPrefix := "specified market suspend time is in the past"
	if !strings.HasPrefix(resp, wantPrefix) {
		t.Errorf("Expected error message starting with %q, got %q", wantPrefix, resp)
	}

	// Bad suspend time (not a time)
	w = httptest.NewRecorder()
	r, _ = http.NewRequest(http.MethodGet, "https://localhost/market/"+name+"/suspend?t=QWERT", nil)
	r.RemoteAddr = "localhost"

	mux.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("apiSuspend returned code %d, expected %d", w.Code, http.StatusBadRequest)
	}
	resp = w.Body.String()
	wantPrefix = "invalid suspend time"
	if !strings.HasPrefix(resp, wantPrefix) {
		t.Errorf("Expected error message starting with %q, got %q", wantPrefix, resp)
	}

	// Good suspend time, one minute in the future
	w = httptest.NewRecorder()
	tMsFuture := time.Now().Add(time.Minute).UnixMilli()
	r, _ = http.NewRequest(http.MethodGet, fmt.Sprintf("https://localhost/market/%v/suspend?t=%d", name, tMsFuture), nil)
	r.RemoteAddr = "localhost"

	mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("apiSuspend returned code %d, expected %d", w.Code, http.StatusOK)
	}
	suspRes = new(SuspendResult)
	err = json.Unmarshal(w.Body.Bytes(), &suspRes)
	if err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	if suspRes.FinalEpoch != tMsFuture {
		t.Errorf("incorrect final epoch index. got %d, expected %d",
			suspRes.FinalEpoch, tMsFuture)
	}

	wantFinal = time.UnixMilli(tMsFuture + 1)
	if !suspRes.SuspendTime.Equal(wantFinal) {
		t.Errorf("incorrect suspend time. got %v, expected %v",
			suspRes.SuspendTime, wantFinal)
	}

	if !tMkt.persist {
		t.Errorf("market persist was false")
	}

	// persist=true (OK)
	w = httptest.NewRecorder()
	r, _ = http.NewRequest(http.MethodGet, "https://localhost/market/"+name+"/suspend?persist=true", nil)
	r.RemoteAddr = "localhost"

	mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("apiSuspend returned code %d, expected %d", w.Code, http.StatusOK)
	}

	if !tMkt.persist {
		t.Errorf("market persist was false")
	}

	// persist=0 (OK)
	w = httptest.NewRecorder()
	r, _ = http.NewRequest(http.MethodGet, "https://localhost/market/"+name+"/suspend?persist=0", nil)
	r.RemoteAddr = "localhost"

	mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("apiSuspend returned code %d, expected %d", w.Code, http.StatusOK)
	}

	if tMkt.persist {
		t.Errorf("market persist was true")
	}

	// invalid persist
	w = httptest.NewRecorder()
	r, _ = http.NewRequest(http.MethodGet, "https://localhost/market/"+name+"/suspend?persist=blahblahblah", nil)
	r.RemoteAddr = "localhost"

	mux.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("apiSuspend returned code %d, expected %d", w.Code, http.StatusBadRequest)
	}
	resp = w.Body.String()
	wantPrefix = "invalid persist book boolean"
	if !strings.HasPrefix(resp, wantPrefix) {
		t.Errorf("Expected error message starting with %q, got %q", wantPrefix, resp)
	}
}

func TestAuthMiddleware(t *testing.T) {
	pass := "password123"
	authSHA := sha256.Sum256([]byte(pass))
	s, _ := newTServer(t, false, authSHA)
	am := s.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	r, _ := http.NewRequest(http.MethodGet, "", nil)
	r.RemoteAddr = "localhost"

	wantAuthError := func(name string, want bool) {
		w := &tResponseWriter{}
		am.ServeHTTP(w, r)
		if w.code != http.StatusUnauthorized && w.code != http.StatusOK {
			t.Fatalf("unexpected HTTP error %d for test \"%s\"", w.code, name)
		}
		switch want {
		case true:
			if w.code != http.StatusUnauthorized {
				t.Fatalf("Expected unauthorized HTTP error for test \"%s\"", name)
			}
		case false:
			if w.code != http.StatusOK {
				t.Fatalf("Expected OK HTTP status for test \"%s\"", name)
			}
		}
	}

	tests := []struct {
		name, user, pass string
		wantErr          bool
	}{{
		name: "user and correct password",
		user: "user",
		pass: pass,
	}, {
		name: "only correct password",
		pass: pass,
	}, {
		name:    "only user",
		user:    "user",
		wantErr: true,
	}, {
		name:    "no user or password",
		wantErr: true,
	}, {
		name:    "wrong password",
		user:    "user",
		pass:    pass[1:],
		wantErr: true,
	}}
	for _, test := range tests {
		r.SetBasicAuth(test.user, test.pass)
		wantAuthError(test.name, test.wantErr)
	}
}

func TestAccounts(t *testing.T) {
	core := &TCore{
		accounts: []*db.Account{},
	}
	srv := &Server{
		core: core,
	}

	mux := chi.NewRouter()
	mux.Get("/accounts", srv.apiAccounts)

	// No accounts.
	w := httptest.NewRecorder()
	r, _ := http.NewRequest(http.MethodGet, "https://localhost/accounts", nil)
	r.RemoteAddr = "localhost"

	mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("apiAccounts returned code %d, expected %d", w.Code, http.StatusOK)
	}
	respBody := w.Body.String()
	if respBody != "[]\n" {
		t.Errorf("incorrect response body: %q", respBody)
	}

	accountIDSlice, err := hex.DecodeString("0a9912205b2cbab0c25c2de30bda9074de0ae23b065489a99199bad763f102cc")
	if err != nil {
		t.Fatal(err)
	}
	var accountID account.AccountID
	copy(accountID[:], accountIDSlice)
	pubkey, err := hex.DecodeString("0204988a498d5d19514b217e872b4dbd1cf071d365c4879e64ed5919881c97eb19")
	if err != nil {
		t.Fatal(err)
	}
	feeCoin, err := hex.DecodeString("6e515ff861f2016fd0da2f3eccdf8290c03a9d116bfba2f6729e648bdc6e5aed00000005")
	if err != nil {
		t.Fatal(err)
	}

	// An account.
	acct := &db.Account{
		AccountID:  accountID,
		Pubkey:     dex.Bytes(pubkey),
		FeeAsset:   42,
		FeeAddress: "DsdQFmH3azyoGKJHt2ArJNxi35LCEgMqi8k",
		FeeCoin:    dex.Bytes(feeCoin),
	}
	core.accounts = append(core.accounts, acct)

	w = httptest.NewRecorder()
	r, _ = http.NewRequest(http.MethodGet, "https://localhost/accounts", nil)
	r.RemoteAddr = "localhost"

	mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("apiAccounts returned code %d, expected %d", w.Code, http.StatusOK)
	}

	exp := `[
    {
        "accountid": "0a9912205b2cbab0c25c2de30bda9074de0ae23b065489a99199bad763f102cc",
        "pubkey": "0204988a498d5d19514b217e872b4dbd1cf071d365c4879e64ed5919881c97eb19",
        "feeasset": 42,
        "feeaddress": "DsdQFmH3azyoGKJHt2ArJNxi35LCEgMqi8k",
        "feecoin": "6e515ff861f2016fd0da2f3eccdf8290c03a9d116bfba2f6729e648bdc6e5aed00000005"
    }
]
`
	if exp != w.Body.String() {
		t.Errorf("unexpected response %q, wanted %q", w.Body.String(), exp)
	}

	// core.Accounts error
	core.accountsErr = errors.New("error")

	w = httptest.NewRecorder()
	r, _ = http.NewRequest(http.MethodGet, "https://localhost/accounts", nil)
	r.RemoteAddr = "localhost"

	mux.ServeHTTP(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("apiAccounts returned code %d, expected %d", w.Code, http.StatusInternalServerError)
	}
}

func TestAccountInfo(t *testing.T) {
	core := new(TCore)
	srv := &Server{
		core: core,
	}

	acctIDStr := "0a9912205b2cbab0c25c2de30bda9074de0ae23b065489a99199bad763f102cc"

	mux := chi.NewRouter()
	mux.Route("/account/{"+accountIDKey+"}", func(rm chi.Router) {
		rm.Get("/", srv.apiAccountInfo)
	})

	// No account.
	w := httptest.NewRecorder()
	r, _ := http.NewRequest(http.MethodGet, "https://localhost/account/"+acctIDStr, nil)
	r.RemoteAddr = "localhost"

	mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("apiAccounts returned code %d, expected %d", w.Code, http.StatusOK)
	}
	respBody := w.Body.String()
	if respBody != "null\n" {
		t.Errorf("incorrect response body: %q", respBody)
	}

	accountIDSlice, err := hex.DecodeString(acctIDStr)
	if err != nil {
		t.Fatal(err)
	}
	var accountID account.AccountID
	copy(accountID[:], accountIDSlice)
	pubkey, err := hex.DecodeString("0204988a498d5d19514b217e872b4dbd1cf071d365c4879e64ed5919881c97eb19")
	if err != nil {
		t.Fatal(err)
	}
	feeCoin, err := hex.DecodeString("6e515ff861f2016fd0da2f3eccdf8290c03a9d116bfba2f6729e648bdc6e5aed00000005")
	if err != nil {
		t.Fatal(err)
	}

	// An account.
	core.account = &db.Account{
		AccountID:  accountID,
		Pubkey:     dex.Bytes(pubkey),
		FeeAsset:   42,
		FeeAddress: "DsdQFmH3azyoGKJHt2ArJNxi35LCEgMqi8k",
		FeeCoin:    dex.Bytes(feeCoin),
	}

	w = httptest.NewRecorder()
	r, _ = http.NewRequest(http.MethodGet, "https://localhost/account/"+acctIDStr, nil)
	r.RemoteAddr = "localhost"

	mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("apiAccount returned code %d, expected %d", w.Code, http.StatusOK)
	}

	exp := `{
    "accountid": "0a9912205b2cbab0c25c2de30bda9074de0ae23b065489a99199bad763f102cc",
    "pubkey": "0204988a498d5d19514b217e872b4dbd1cf071d365c4879e64ed5919881c97eb19",
    "feeasset": 42,
    "feeaddress": "DsdQFmH3azyoGKJHt2ArJNxi35LCEgMqi8k",
    "feecoin": "6e515ff861f2016fd0da2f3eccdf8290c03a9d116bfba2f6729e648bdc6e5aed00000005"
}
`
	if exp != w.Body.String() {
		t.Errorf("unexpected response %q, wanted %q", w.Body.String(), exp)
	}

	// ok, upper case account id
	w = httptest.NewRecorder()
	r, _ = http.NewRequest(http.MethodGet, "https://localhost/account/"+strings.ToUpper(acctIDStr), nil)
	r.RemoteAddr = "localhost"

	mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("apiAccount returned code %d, expected %d", w.Code, http.StatusOK)
	}
	if exp != w.Body.String() {
		t.Errorf("unexpected response %q, wanted %q", w.Body.String(), exp)
	}

	// acct id is not hex
	w = httptest.NewRecorder()
	r, _ = http.NewRequest(http.MethodGet, "https://localhost/account/nothex", nil)
	r.RemoteAddr = "localhost"

	mux.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("apiAccount returned code %d, expected %d", w.Code, http.StatusBadRequest)
	}

	// acct id wrong length
	w = httptest.NewRecorder()
	r, _ = http.NewRequest(http.MethodGet, "https://localhost/account/"+acctIDStr[2:], nil)
	r.RemoteAddr = "localhost"

	mux.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("apiAccount returned code %d, expected %d", w.Code, http.StatusBadRequest)
	}

	// core.Account error
	core.accountErr = errors.New("error")

	w = httptest.NewRecorder()
	r, _ = http.NewRequest(http.MethodGet, "https://localhost/account/"+acctIDStr, nil)
	r.RemoteAddr = "localhost"

	mux.ServeHTTP(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("apiAccount returned code %d, expected %d", w.Code, http.StatusInternalServerError)
	}
}

func TestAPITimeMarshalJSON(t *testing.T) {
	now := APITime{time.Now()}
	b, err := json.Marshal(now)
	if err != nil {
		t.Fatalf("unable to marshal api time: %v", err)
	}
	var res APITime
	if err := json.Unmarshal(b, &res); err != nil {
		t.Fatalf("unable to unmarshal api time: %v", err)
	}
	if !res.Equal(now.Time) {
		t.Fatal("unmarshalled time not equal")
	}
}

func TestNotify(t *testing.T) {
	core := new(TCore)
	srv := &Server{
		core: core,
	}
	mux := chi.NewRouter()
	mux.Route("/account/{"+accountIDKey+"}/notify", func(rm chi.Router) {
		rm.Post("/", srv.apiNotify)
	})
	acctIDStr := "0a9912205b2cbab0c25c2de30bda9074de0ae23b065489a99199bad763f102cc"
	msgStr := "Hello world.\nAll your base are belong to us."
	tests := []struct {
		name, txt, acctID string
		wantCode          int
	}{{
		name:     "ok",
		acctID:   acctIDStr,
		txt:      msgStr,
		wantCode: http.StatusOK,
	}, {
		name:     "ok at max size",
		acctID:   acctIDStr,
		txt:      string(make([]byte, maxUInt16)),
		wantCode: http.StatusOK,
	}, {
		name:     "message too long",
		acctID:   acctIDStr,
		txt:      string(make([]byte, maxUInt16+1)),
		wantCode: http.StatusBadRequest,
	}, {
		name:     "account id not hex",
		acctID:   "nothex",
		txt:      msgStr,
		wantCode: http.StatusBadRequest,
	}, {
		name:     "account id wrong length",
		acctID:   acctIDStr[2:],
		txt:      msgStr,
		wantCode: http.StatusBadRequest,
	}, {
		name:     "no message",
		acctID:   acctIDStr,
		wantCode: http.StatusBadRequest,
	}}
	for _, test := range tests {
		w := httptest.NewRecorder()
		br := bytes.NewReader([]byte(test.txt))
		r, _ := http.NewRequest("POST", "https://localhost/account/"+test.acctID+"/notify", br)
		r.RemoteAddr = "localhost"

		mux.ServeHTTP(w, r)

		if w.Code != test.wantCode {
			t.Fatalf("%q: apiNotify returned code %d, expected %d", test.name, w.Code, test.wantCode)
		}
	}
}

func TestNotifyAll(t *testing.T) {
	core := new(TCore)
	srv := &Server{
		core: core,
	}
	mux := chi.NewRouter()
	mux.Route("/notifyall", func(rm chi.Router) {
		rm.Post("/", srv.apiNotifyAll)
	})
	tests := []struct {
		name, txt string
		wantCode  int
	}{{
		name:     "ok",
		txt:      "Hello world.\nAll your base are belong to us.",
		wantCode: http.StatusOK,
	}, {
		name:     "ok at max size",
		txt:      string(make([]byte, maxUInt16)),
		wantCode: http.StatusOK,
	}, {
		name:     "message too long",
		txt:      string(make([]byte, maxUInt16+1)),
		wantCode: http.StatusBadRequest,
	}, {
		name:     "no message",
		wantCode: http.StatusBadRequest,
	}}
	for _, test := range tests {
		w := httptest.NewRecorder()
		br := bytes.NewReader([]byte(test.txt))
		r, _ := http.NewRequest("POST", "https://localhost/notifyall", br)
		r.RemoteAddr = "localhost"

		mux.ServeHTTP(w, r)

		if w.Code != test.wantCode {
			t.Fatalf("%q: apiNotifyAll returned code %d, expected %d", test.name, w.Code, test.wantCode)
		}
	}
}

func TestEnableDataAPI(t *testing.T) {
	core := new(TCore)
	srv := &Server{
		core: core,
	}
	mux := chi.NewRouter()
	mux.Route("/enabledataapi/{yes}", func(rm chi.Router) {
		rm.Post("/", srv.apiEnableDataAPI)
	})

	tests := []struct {
		name, yes   string
		wantCode    int
		wantEnabled uint32
	}{{
		name:        "ok 1",
		yes:         "1",
		wantCode:    http.StatusOK,
		wantEnabled: 1,
	}, {
		name:        "ok true",
		yes:         "true",
		wantCode:    http.StatusOK,
		wantEnabled: 1,
	}, {
		name:        "message too long",
		yes:         "mabye",
		wantCode:    http.StatusBadRequest,
		wantEnabled: 0,
	}}
	for _, test := range tests {
		w := httptest.NewRecorder()
		br := bytes.NewReader([]byte{})
		r, _ := http.NewRequest("POST", "https://localhost/enabledataapi/"+test.yes, br)
		r.RemoteAddr = "localhost"

		mux.ServeHTTP(w, r)

		if w.Code != test.wantCode {
			t.Fatalf("%q: apiEnableDataAPI returned code %d, expected %d", test.name, w.Code, test.wantCode)
		}

		if test.wantEnabled != atomic.LoadUint32(&core.dataEnabled) {
			t.Fatalf("%q: apiEnableDataAPI expected dataEnabled = %d, got %d", test.name, test.wantEnabled, atomic.LoadUint32(&core.dataEnabled))
		}

		atomic.StoreUint32(&core.dataEnabled, 0)
	}

}
