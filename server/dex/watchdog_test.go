package dex

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/server/asset"
	"decred.org/dcrdex/server/market"
)

const (
	// maxBlockInterval is the maximum time in seconds between blocks,
	maxBlockInterval = uint32(10)
	assetID          = uint32(42)
)

var logger = dex.StdOutLogger("TEST", dex.LevelTrace)

func TestBlockWatchdog_NewBlockWatchdog(t *testing.T) {

	blockInterval := maxBlockInterval
	ntfnChan := make(chan *WatchdogNotification)

	wd := NewBlockWatchdog(&MonitoredAsset{
		Backend:       &TBackend{},
		AssetID:       assetID,
		BlockInterval: blockInterval,
	}, ntfnChan, logger)

	if wd.assetID != assetID {
		t.Fatalf("expected AssetID %d, got %d", assetID, wd.assetID)
	}

	if wd.blockInterval != blockInterval {
		t.Fatalf("expected BlockInterval %d, got %d", blockInterval, wd.blockInterval)
	}

	// NewBlockWatchdog returns nil if blockInterval is 0
	wdNil := NewBlockWatchdog(&MonitoredAsset{
		Backend:       &TBackend{},
		AssetID:       assetID,
		BlockInterval: 0,
	}, ntfnChan, logger)
	if wdNil != nil {
		t.Fatalf("expected nil, got %v", wdNil)
	}
}

func TestBlockWatchdog_Run(t *testing.T) {

	tBlockIntervals := []uint32{5, 8, 15, 2, 29, 22, 4}
	//
	// Blocks/intvls |--5--*----8---*------15-------*-2-*--------------29-------------*----------22-----------*--4-*
	// Timer resets  |     #        #          #    #   #          #          #       #          #          # #
	// Update ntfy   |                         v    ^              v                  ^          v            ^
	// Timestamp     |     5       13          23  28             40         50      59         69           81
	tExpected := []*TOutput{
		{timestamp: 23, synced: false},
		{timestamp: 28, synced: true},
		{timestamp: 40, synced: false},
		{timestamp: 59, synced: true},
		{timestamp: 69, synced: false},
		{timestamp: 81, synced: true},
	}

	ntfnChan := make(chan *WatchdogNotification)

	backend := &TBackend{
		BackendSynced: true,
		BlockChan:     make(chan *asset.BlockUpdate, 1),
	}

	wd := NewBlockWatchdog(&MonitoredAsset{
		AssetID:       assetID,
		BlockInterval: maxBlockInterval,
		Backend:       backend,
	}, ntfnChan, logger)

	newBlock := func(blockInterval uint32) {
		time.Sleep(time.Duration(blockInterval) * time.Second)
		backend.BlockChan <- &asset.BlockUpdate{}
	}

	ctx, cancel := context.WithCancel(context.TODO())
	go wd.Run(ctx)

	var tResults []*TOutput
	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		tResults = testSyncUpdate(ctx, t, ntfnChan)
	}()

	for height, interval := range tBlockIntervals {
		t.Logf("newBlock %d interval %d", height, interval)
		newBlock(interval)
	}

	cancel()
	wg.Wait()

	if !reflect.DeepEqual(tResults, tExpected) {
		t.Errorf("Test failed. Expected: %v, Got: %v", tExpected, tResults)
	}
}

type TOutput struct {
	timestamp int32
	synced    bool
}

func testSyncUpdate(ctx context.Context, t *testing.T, ntfnChan <-chan *WatchdogNotification) []*TOutput {

	results := []*TOutput{}
	startedAt := time.Now().UTC()

	for {
		select {
		case ntfn, ok := <-ntfnChan:
			if !ok {
				return results
			}
			results = append(results, &TOutput{
				timestamp: int32(time.Since(startedAt).Seconds()),
				synced:    ntfn.Synced,
			})
		case <-ctx.Done():
			return results
		}
	}
}

type TBackend struct {
	BlockChan     chan *asset.BlockUpdate
	BackendSynced bool
	mtx           sync.RWMutex
}

// Synced mocks the InitialBlockDownload/GetBlockChainInfo logic
func (tb *TBackend) Synced() (bool, error) {
	tb.mtx.RLock()
	defer tb.mtx.RUnlock()
	return tb.BackendSynced, nil
}

// SetSynced simulates a change in the backend's synced status
func (tb *TBackend) SetSynced(synced bool) {
	tb.mtx.Lock()
	defer tb.mtx.Unlock()
	tb.BackendSynced = synced
}
func (tb *TBackend) BlockChannel(size int) <-chan *asset.BlockUpdate {
	return tb.BlockChan
}

// mock methods
func (tb *TBackend) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	return nil, nil
}

func (tb *TBackend) Contract(coinID []byte, contractData []byte) (*asset.Contract, error) {
	return nil, nil
}
func (tb *TBackend) TxData(coinID []byte) ([]byte, error) {
	return nil, nil
}
func (tb *TBackend) ValidateSecret(secret, contractData []byte) bool {
	return false
}
func (tb *TBackend) Redemption(redemptionID, contractID, contractData []byte) (asset.Coin, error) {
	return nil, nil
}
func (tb *TBackend) InitTxSize() uint32 {
	return 0
}
func (tb *TBackend) InitTxSizeBase() uint32 {
	return 0
}
func (tb *TBackend) CheckSwapAddress(string) bool {
	return false
}
func (tb *TBackend) ValidateCoinID(coinID []byte) (string, error) {
	return "", nil
}
func (tb *TBackend) ValidateContract(contract []byte) error {
	return nil
}
func (tb *TBackend) FeeRate(context.Context) (uint64, error) {
	return 0, nil
}
func (tb *TBackend) Info() *asset.BackendInfo {
	return nil
}
func (tb *TBackend) ValidateFeeRate(contract *asset.Contract, reqFeeRate uint64) bool {
	return false
}

type TBackedAsset struct {
	dex.Asset
	Backend TBackend
}

func TestNewAssetMonitor(t *testing.T) {

	amConfig := &AssetMonitorConfig{
		DEX:    &DEX{},
		Logger: logger,
		Markets: map[string]*market.Market{
			"dcr_btc": {},
			"dcr_ltc": {},
		},
	}
	am := NewAssetMonitor(amConfig)
	if am.dexMgr == nil {
		t.Error("dexMgr is nil")
	}
}

type TDEX struct {
	markets map[string]AMMarketInterface
	mtx     sync.RWMutex
}

func (d *TDEX) MarketRunning(mktName string) (found, running bool) {
	d.mtx.RLock()
	defer d.mtx.RUnlock()
	mkt, found := d.markets[mktName]
	if !found {
		return false, false
	}
	return found, mkt.(*TMarket).running
}
func (d *TDEX) ResumeMarket(mktName string, now time.Time) (startEpoch int64, startTime time.Time, err error) {
	d.mtx.Lock()
	defer d.mtx.Unlock()
	m := d.markets[mktName]
	m.(*TMarket).running = true
	return 0, time.Now(), nil
}
func (d *TDEX) SuspendMarket(mktName string, now time.Time, persist bool) (suspEpoch *market.SuspendEpoch, err error) {
	d.mtx.Lock()
	defer d.mtx.Unlock()
	m := d.markets[mktName]
	m.(*TMarket).running = false
	return nil, nil
}

type TMarket struct {
	AMMarketInterface
	base, quote uint32
	running     bool
}

func (m *TMarket) Base() uint32 {
	return m.base
}
func (m *TMarket) Quote() uint32 {
	return m.quote
}
func (m *TMarket) Running() bool {
	return m.running
}

var tMarkets = map[string]AMMarketInterface{
	"dcr_btc": &TMarket{base: 42, quote: 0, running: true},
	"ltc_dcr": &TMarket{base: 2, quote: 42, running: true},
}

var dexMgr = &TDEX{
	// for mocking MarketRunning/ResumeMarket/SuspendMarket
	markets: tMarkets,
}

// func TestNewAssetMonitor_NewAssetMonitor(t *testing.T) {
// 	am := NewAssetMonitor(&AssetMonitorConfig{
// 		DEX:    &DEX{},
// 		Logger: logger,
// 		Markets: map[string]*market.Market{
// 			// FIXME find a way to mock the market interface
// 			"dcr_btc": {},
// 			"ltc_dcr": {},
// 		},
// 		MonitoredAssets: map[uint32]*MonitoredAsset{
// 			0: {
// 				AssetID:       0,
// 				BlockInterval: maxBlockInterval,
// 				Backend:       &TBackend{},
// 			},
// 			2: {
// 				AssetID:       2,
// 				BlockInterval: maxBlockInterval,
// 				Backend:       &TBackend{},
// 			},
// 			42: {
// 				AssetID:       42,
// 				BlockInterval: maxBlockInterval,
// 				Backend:       &TBackend{},
// 			},
// 		},
// 	})
// 	if am == nil {
// 		t.Error("expected AssetMonitor, got nil")
// 	}
// }

func TestAssetMonitor_runAssetMonitor(t *testing.T) {

	ctx, cancel := context.WithCancel(context.TODO())
	ntfnChan := make(chan *WatchdogNotification)

	sendNtfn := func(assetID uint32, synced bool) {
		ntfnChan <- &WatchdogNotification{
			AssetID: assetID,
			Synced:  synced,
		}
	}

	am := &AssetMonitor{
		dexMgr: dexMgr,
		logger: logger,
		markets: map[uint32]map[string]AMMarketInterface{
			0: {
				"dcr_btc": &TMarket{base: 42, quote: 0, running: true},
			},
			2: {
				"ltc_dcr": &TMarket{base: 2, quote: 42, running: true},
			},
			42: {
				"dcr_btc": &TMarket{base: 42, quote: 0, running: true},
				"ltc_dcr": &TMarket{base: 2, quote: 42, running: true},
			},
		},
		ntfnChan:        ntfnChan,
		chainSyncStates: make(map[uint32]bool),
	}

	assets := []uint32{0, 2, 42}

	for _, assetID := range assets {
		am.chainSyncStates[assetID] = true
		am.markets[assetID] = am.findMarketsByAsset(assetID, tMarkets)
	}

	assertMarketRunning := func(mkt string, want bool) {
		dexMgr.mtx.RLock()
		defer dexMgr.mtx.RUnlock()
		got := dexMgr.markets[mkt].(*TMarket).running
		if got != want {
			t.Errorf("expected %s market running=%v, got %v", mkt, want, got)
		}
	}

	go am.startNtfnListener(ctx)

	time.Sleep(1 * time.Second)

	// btc chain out of sync
	sendNtfn(0, false)
	time.Sleep(1 * time.Second)
	// dcr_btc market should be suspended
	assertMarketRunning("dcr_btc", false)

	// btc chain back in sync
	sendNtfn(0, true)
	time.Sleep(1 * time.Second)
	// dcr_btc market should be running
	assertMarketRunning("dcr_btc", true)

	// dcr chain out of sync
	sendNtfn(42, false)
	time.Sleep(1 * time.Second)
	assertMarketRunning("ltc_dcr", false)
	assertMarketRunning("dcr_btc", false)

	// ltc chain out of sync
	sendNtfn(2, false)
	time.Sleep(1 * time.Second)
	// ltc_dcr market should still be suspended
	assertMarketRunning("ltc_dcr", false)

	// dcr chain back in sync
	sendNtfn(42, true)
	time.Sleep(1 * time.Second)
	assertMarketRunning("dcr_btc", true)
	// ltc_dcr market should be suspended (ltc still down)
	assertMarketRunning("ltc_dcr", false)

	sendNtfn(2, true)
	time.Sleep(1 * time.Second)
	assertMarketRunning("ltc_dcr", true)
	assertMarketRunning("dcr_btc", true)

	time.Sleep(1 * time.Second)
	cancel()
}
