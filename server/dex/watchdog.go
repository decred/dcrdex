package dex

import (
	"context"
	"sync"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/server/asset"
	"decred.org/dcrdex/server/market"
)

// BlockWatchdog monitors block updates for a backed asset and sends
// updates on chain state changes. Out of sync condition is detected
// using a simple heuristic: when the interval between blocks exceeds
// a (configurable) threshold, the chain is considered out of sync.
type BlockWatchdog struct {
	assetID       uint32
	logger        dex.Logger
	blockChan     <-chan *asset.BlockUpdate
	ntfnChan      chan *WatchdogNotification
	blockInterval uint32
}

type MonitoredAsset struct {
	asset.Backend
	AssetID       uint32
	BlockInterval uint32
}

type WatchdogNotification struct {
	AssetID uint32
	Synced  bool
}

// NewBlockWatchdog initializes a new BlockWatchdog.
func NewBlockWatchdog(ma *MonitoredAsset, ntfnChan chan *WatchdogNotification, logger dex.Logger) *BlockWatchdog {
	// if blockInterval is not specified, there is no reason for our existence.
	if ma.BlockInterval == 0 {
		return nil
	}
	wd := &BlockWatchdog{
		logger:        logger,
		assetID:       ma.AssetID,
		ntfnChan:      ntfnChan,
		blockChan:     ma.Backend.BlockChannel(5),
		blockInterval: ma.BlockInterval,
	}
	return wd
}

func (wd *BlockWatchdog) Run(ctx context.Context) {

	log := wd.logger
	symbol := dex.BipIDSymbol(wd.assetID)
	log.Tracef("Starting block watchdog for [%s]", symbol)

	maxBlockIntvl := time.Duration(wd.blockInterval) * time.Second
	timer := time.NewTimer(maxBlockIntvl)
	defer timer.Stop()

	// This flag is used to detect changes in chain sync state.
	// It defaults to true so as to avoid triggering an in-sync notification
	// at startup.
	lastSynced := true
	timerStarted := time.Now().UTC()

	resetTimer := func() {
		timer.Reset(maxBlockIntvl)
		timerStarted = time.Now().UTC()
	}

out:
	for {
		select {
		// listen for block updates coming from the asset Backend
		case update := <-wd.blockChan:
			if update.Err != nil {
				log.Errorf("error encountered while monitoring blocks: %v", update.Err)
				continue
			}

			lastBlockIntvl := time.Since(timerStarted)
			log.Tracef("Last %s block interval %.0fs", symbol, lastBlockIntvl.Seconds())
			if !lastSynced {
				lastSynced = true
				wd.ntfnChan <- &WatchdogNotification{
					AssetID: wd.assetID,
					Synced:  true,
				}
			}
			resetTimer()

		case <-timer.C:
			log.Warnf("No %s block received in %.0fs, chain out of sync", symbol, maxBlockIntvl.Seconds())
			if lastSynced {
				lastSynced = false
				wd.ntfnChan <- &WatchdogNotification{
					AssetID: wd.assetID,
					Synced:  false,
				}
			}
			resetTimer()

		case <-ctx.Done():
			break out
		}
	}
	log.Tracef("Exiting block watchdog for [%s]", symbol)
}

// AssetMonitor monitors chain sync status updates emitted by BlockWatchdog.
// When a chain is out of sync, it suspends the markets that trade that
// asset, and resumes them when the chain is synced.
type AssetMonitor struct {
	dex.Runner
	dexMgr          AMDEXInterface
	mtx             sync.RWMutex
	logger          dex.Logger
	markets         map[uint32]map[string]AMMarketInterface
	watchdogs       map[uint32]*BlockWatchdog
	chainSyncStates map[uint32]bool
	ntfnChan        chan *WatchdogNotification
}

type AssetMonitorConfig struct {
	DEX     *DEX
	Logger  dex.Logger
	Markets map[string]*market.Market
	Assets  map[uint32]*MonitoredAsset
}

// AMDEXInterface is defined to allow mocking the DEX interface in unit tests.
type AMDEXInterface interface {
	MarketRunning(string) (bool, bool)
	ResumeMarket(string, time.Time) (int64, time.Time, error)
	SuspendMarket(string, time.Time, bool) (*market.SuspendEpoch, error)
}

// AMMarketInterface is defined to allow mocking the Market interface in unit tests.
type AMMarketInterface interface {
	Base() uint32
	Quote() uint32
}

// NewAssetMonitor initializes an AssetMonitor.
func NewAssetMonitor(amConfig *AssetMonitorConfig) *AssetMonitor {

	am := &AssetMonitor{
		dexMgr:          AMDEXInterface(amConfig.DEX),
		logger:          amConfig.Logger,
		markets:         make(map[uint32]map[string]AMMarketInterface),
		ntfnChan:        make(chan *WatchdogNotification, len(amConfig.Assets)),
		watchdogs:       make(map[uint32]*BlockWatchdog),
		chainSyncStates: make(map[uint32]bool),
	}

	markets := make(map[string]AMMarketInterface)
	for name, mkt := range amConfig.Markets {
		markets[name] = AMMarketInterface(mkt)
	}

	for assetID, ma := range amConfig.Assets {
		// create watchdog for the asset
		wd := NewBlockWatchdog(ma, am.ntfnChan, am.logger)
		if wd == nil {
			am.logger.Tracef("No block interval specified for %s chain", dex.BipIDSymbol(assetID))
			continue
		}
		am.watchdogs[assetID] = wd
		// Chain sync state cache.
		// Initialize synced status for each monitored asset to true,
		// assuming that the chain is synced at startup; if initial block
		// download is in progress, the arriving blocks will keep the asset
		// in synced state.
		am.chainSyncStates[assetID] = true
		// obtain the list of markets the asset is traded in
		am.markets[assetID] = am.findMarketsByAsset(assetID, markets)
	}
	return am
}

// Satisfies dex.Runner
func (am *AssetMonitor) Run(ctx context.Context) {

	defer close(am.ntfnChan)

	var wg sync.WaitGroup
	for _, wd := range am.watchdogs {
		wg.Add(1)
		go func(wd *BlockWatchdog) {
			defer wg.Done()
			wd.Run(ctx)
		}(wd)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		am.startNtfnListener(ctx)
	}()
	am.startNtfnListener(ctx)
	wg.Wait()
}

func (am *AssetMonitor) startNtfnListener(ctx context.Context) {
	log := am.logger
	dexMgr := am.dexMgr
out:
	for {
		select {
		case ntfn := <-am.ntfnChan:
			synced := ntfn.Synced
			assetID := ntfn.AssetID
			markets := am.markets[assetID]
			am.updateChainSyncState(assetID, synced)
			if synced {
				for mktName, mkt := range markets {
					if found, running := dexMgr.MarketRunning(mktName); found && !running {
						if otherSide, synced := am.otherSideSynced(assetID, mkt); synced {
							log.Tracef("Resuming market %s", mktName)
							dexMgr.ResumeMarket(mktName, time.Now())
						} else {
							otherSymbol := dex.BipIDSymbol(otherSide)
							log.Tracef("Not resuming market %s: the other side (%s) is out of sync", mktName, otherSymbol)
						}
					}
				}
			} else {
				for mktName := range markets {
					if found, running := dexMgr.MarketRunning(mktName); found && running {
						log.Tracef("Suspending market %s", mktName)
						dexMgr.SuspendMarket(mktName, time.Now(), true)
					}
				}
			}
		case <-ctx.Done():
			break out
		}
	}
	log.Tracef("Asset monitor stopped")

}

// findMarketsByAsset returns a map of markets that trade the specified asset
func (am *AssetMonitor) findMarketsByAsset(assetID uint32, allMarkets map[string]AMMarketInterface) map[string]AMMarketInterface {
	var markets = make(map[string]AMMarketInterface)
	for mktName, mkt := range allMarkets {
		if mkt.Base() == assetID || mkt.Quote() == assetID {
			markets[mktName] = mkt
		}
	}
	return markets
}

func (am *AssetMonitor) updateChainSyncState(assetID uint32, synced bool) {
	am.mtx.Lock()
	defer am.mtx.Unlock()
	am.chainSyncStates[assetID] = synced
}

func (am *AssetMonitor) otherSideSynced(assetID uint32, mkt AMMarketInterface) (uint32, bool) {
	var otherSide uint32
	if assetID == mkt.Base() {
		otherSide = mkt.Quote()
	} else {
		otherSide = mkt.Base()
	}
	am.mtx.RLock()
	defer am.mtx.RUnlock()
	// if the other side is not monitored, assume it is synced
	if synced, found := am.chainSyncStates[otherSide]; found && synced || !found {
		return otherSide, true
	}
	return otherSide, false
}
