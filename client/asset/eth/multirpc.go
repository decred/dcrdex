// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

// https://ethereumnodes.com/ for RPC providers

package eth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"math/rand"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/networks/erc20"
	dexeth "decred.org/dcrdex/dex/networks/eth"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/misc"
	"github.com/ethereum/go-ethereum/core/txpool"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rpc"
)

const (
	// failQuarantine is how long we will wait after a failed request before
	// trying a provider again.
	failQuarantine = time.Minute
	// headerCheckInterval is the time between header checks. Slightly less
	// than the fail quarantine to ensure providers with old headers stay
	// quarantined.
	headerCheckInterval = time.Second * 50
	// receiptCacheExpiration is how long we will track a receipt after the
	// last request. There is no persistent storage, so all receipts are cached
	// in-memory.
	receiptCacheExpiration       = time.Hour
	unconfirmedReceiptExpiration = time.Minute
	tipCapSuggestionExpiration   = time.Hour
	brickedFailCount             = 100
	providerDelimiter            = " "
	defaultRequestTimeout        = time.Second * 10
)

// nonceProviderStickiness is the minimum amount of time that must pass between
// requests to DIFFERENT nonce providers. If we use a provider for a
// nonce-sensitive (NS) operation, and later have another NS operation, we will
// use the same provider if < nonceProviderStickiness has passed.
var nonceProviderStickiness = time.Minute

// TODO: Handle rate limiting? From the docs:
// When you are rate limited, your JSON-RPC responses have HTTP Status code 429.
// I don't think we have access to these codes through ethclient.Client, but
// I haven't verified that.

// The suggested tip cap is expected to be very-slowly changing. We'll only
// update once per tipCapSuggestionExpiration.
type cachedTipCap struct {
	cap   *big.Int
	stamp time.Time
}

type combinedRPCClient struct {
	*ethclient.Client
	rpc *rpc.Client
}

type provider struct {
	// host is the domain and tld of the provider, and is used as a identifier
	// in logs and as a unique, path- and subdomain-independent ID for e.g. map
	// keys.
	host         string
	endpointAddr string
	ec           *combinedRPCClient
	ws           bool
	chainID      *big.Int
	net          dex.Network
	tipCapV      atomic.Value // *cachedTipCap
	stop         func()

	// tip tracks the best known header as well as any error encountered
	tip struct {
		sync.RWMutex
		header      *types.Header
		headerStamp time.Time
		failStamp   time.Time
		failCount   int
	}
}

// String returns the provider host name.
func (p *provider) String() string {
	return p.host
}

func (p *provider) shutdown() {
	p.stop()
	p.ec.Close()
}

func (p *provider) setTip(header *types.Header, log dex.Logger) {
	p.tip.Lock()
	p.tip.header = header
	p.tip.headerStamp = time.Now()
	p.tip.failStamp = time.Time{}
	unfailed := p.tip.failCount != 0
	p.tip.failCount = 0
	p.tip.Unlock()
	if unfailed {
		log.Debugf("Provider at %s was failed but is now useable again.", p.host)
	}
}

// cachedTip retrieves the last known best header.
func (p *provider) cachedTip() *types.Header {
	stale := time.Second * 10
	if p.ws {
		// We want to avoid requests, and we expect that our notification feed
		// is working. Setting this too low would result in unnecessary requests
		// when notifications are working right. Setting this too high will
		// inevitably result in long tip change intervals if notifications fail.
		stale = time.Minute
	}

	p.tip.RLock()
	defer p.tip.RUnlock()

	if time.Since(p.tip.failStamp) < failQuarantine || time.Since(p.tip.headerStamp) > stale {
		return nil
	}
	return p.tip.header
}

// setFailed should be called after a failed request, the provider is considered
// failed for failQuarantine.
func (p *provider) setFailed() {
	p.tip.Lock()
	p.tip.failStamp = time.Now()
	p.tip.failCount++
	p.tip.Unlock()
}

// failed will be true if setFailed has been called in the last failQuarantine.
func (p *provider) failed() bool {
	p.tip.Lock()
	defer p.tip.Unlock()
	return p.tip.failCount > brickedFailCount || time.Since(p.tip.failStamp) < failQuarantine
}

// bestHeader get the best known header from the provider, cached if available,
// otherwise a new RPC call is made.
func (p *provider) bestHeader(ctx context.Context, log dex.Logger) (*types.Header, error) {
	// Check if we have a cached header.
	if tip := p.cachedTip(); tip != nil {
		log.Tracef("Using cached header from %q", p.host)
		return tip, nil
	}

	log.Tracef("Fetching fresh header from %q", p.host)
	hdr, err := p.ec.HeaderByNumber(ctx, nil /* latest */)
	if err != nil {
		p.setFailed()
		return nil, fmt.Errorf("HeaderByNumber error: %w", err)
	}
	timeDiff := time.Now().Unix() - int64(hdr.Time)
	if timeDiff > dexeth.MaxBlockInterval && p.net != dex.Simnet {
		p.setFailed()
		return nil, fmt.Errorf("time since last eth block (%d sec) exceeds %d sec. "+
			"Assuming provider %s is not in sync. Ensure your computer's system clock "+
			"is correct.", timeDiff, dexeth.MaxBlockInterval, p.host)
	}
	p.setTip(hdr, log)
	return hdr, nil
}

func (p *provider) headerByHash(ctx context.Context, h common.Hash) (*types.Header, error) {
	hdr, err := p.ec.HeaderByHash(ctx, h)
	if err != nil {
		p.setFailed()
		return nil, fmt.Errorf("HeaderByHash error: %w", err)
	}
	return hdr, nil
}

// suggestTipCap returns a tip cap suggestion, cached if available, otherwise a
// new RPC call is made.
func (p *provider) suggestTipCap(ctx context.Context, log dex.Logger) *big.Int {
	if cachedV := p.tipCapV.Load(); cachedV != nil {
		rec := cachedV.(*cachedTipCap)
		if time.Since(rec.stamp) < tipCapSuggestionExpiration {
			return rec.cap
		}
	}
	tipCap, err := p.ec.SuggestGasTipCap(ctx)
	if err != nil {
		p.setFailed()
		log.Errorf("error getting tip cap suggestion from %q: %v", p.host, err)
		return dexeth.GweiToWei(dexeth.MinGasTipCap)
	}

	minGasTipCapWei := dexeth.GweiToWei(dexeth.MinGasTipCap)
	if tipCap.Cmp(minGasTipCapWei) < 0 {
		return tipCap.Set(minGasTipCapWei)
	}

	p.tipCapV.Store(&cachedTipCap{
		cap:   tipCap,
		stamp: time.Now(),
	})

	return tipCap
}

// refreshHeader fetches a header every headerCheckInterval. This keeps the
// cached header up to date or fails the provider if there is a problem getting
// the header.
func (p *provider) refreshHeader(ctx context.Context, log dex.Logger) {
	log.Tracef("handling header refreshes for %q", p.host)
	ticker := time.NewTicker(headerCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			// Fetching the best header will check that either the
			// provider's cached header is not too old or that a
			// newly fetched header is not too old. If it is too
			// old that indicates the provider is not in sync and
			// should not be used.
			innerCtx, cancel := context.WithTimeout(ctx, defaultRequestTimeout)
			if _, err := p.bestHeader(innerCtx, log); err != nil {
				log.Warnf("Problem getting best header from provider %s: %s.", p.host, err)
			}
			cancel()
		case <-ctx.Done():
			return
		}
	}
}

// subscribeHeaders starts a listening loop for header updates for a provider.
// The Subscription and header chan are passed in, because error-free
// instantiation of these variable is necessary to accepting that a websocket
// connection is valid, so they are generated early in connectProviders.
func (p *provider) subscribeHeaders(ctx context.Context, sub ethereum.Subscription, h chan *types.Header, log dex.Logger) {
	defer sub.Unsubscribe()
	var lastWarning time.Time
	newSub := func() (ethereum.Subscription, error) {
		for {
			var err error
			sub, err = p.ec.SubscribeNewHead(ctx, h)
			if err == nil {
				return sub, nil
			}
			if time.Since(lastWarning) > 5*time.Minute {
				log.Warnf("can't resubscribe to %q headers: %v", p.host, err)
			}
			select {
			case <-time.After(time.Second * 30):
				log.Debugf("attempting to resubscribe to websocket headers from %s", p.host)
			case <-ctx.Done():
				return nil, context.Canceled
			}
		}
	}

	// I thought the filter logs might catch some transactions we could cache
	// to avoid rpc calls, but in testing, I get nothing in the channel. May
	// revisit later.
	// logs := make(chan types.Log, 128)
	// newAcctSub := func(retryTimeout time.Duration) ethereum.Subscription {
	// 	config := ethereum.FilterQuery{
	// 		Addresses: []common.Address{addr},
	// 	}

	// 	acctSub, err := p.ec.SubscribeFilterLogs(ctx, config, logs)
	// 	if err != nil {
	// 		log.Errorf("failed to subscribe to filter logs: %v", err)
	// 		return newRetrySubscription(ctx, retryTimeout)
	// 	}
	// 	return acctSub
	// }

	// // If we fail the first time, don't try again.
	// acctSub := newAcctSub(time.Hour * 24 * 365)
	// defer acctSub.Unsubscribe()

	// Start the background filtering
	log.Tracef("handling websocket subscriptions for %q", p.host)

	for {
		select {
		case hdr := <-h:
			log.Tracef("%q reported new tip at height %s (%s)", p.host, hdr.Number, hdr.Hash())
			p.setTip(hdr, log)
		case err, ok := <-sub.Err():
			if !ok {
				// Subscription cancelled
				return
			}
			if ctx.Err() != nil || err == nil { // Both conditions indicate normal close
				return
			}
			log.Errorf("%q header subscription error: %v", p.host, err)
			log.Infof("Attempting to resubscribe to %q block headers", p.host)
			sub, err = newSub()
			if err != nil { // context cancelled
				return
			}
		// case l := <-logs:
		// 	log.Tracef("%q log reported: %+v", p.host, l)
		// case err, ok := <-acctSub.Err():
		// 	if err != nil && !errors.Is(err, retryError) {
		// 		log.Errorf("%q log subscription error: %v", p.host, err)
		// 	}
		// 	if ok {
		// 		acctSub = newAcctSub(time.Minute * 5)
		// 	}
		case <-ctx.Done():
			return
		}
	}
}

// receiptRecord is a cached receipt and its last-access time. Receipts are
// stored in-memory for up to receiptCacheExpiration.
type receiptRecord struct {
	r          *types.Receipt
	lastAccess time.Time
	confirmed  bool
}

// multiRPCClient is an ethFetcher backed by one or more public RPC providers.
type multiRPCClient struct {
	cfg     *params.ChainConfig
	creds   *accountCredentials
	log     dex.Logger
	chainID *big.Int
	net     dex.Network

	providerMtx sync.RWMutex
	endpoints   []string
	providers   []*provider

	lastNonce struct {
		sync.Mutex
		nonce uint64
		stamp time.Time
	}

	// When we send transactions close together, we'll want to use the same
	// provider.
	lastProvider struct {
		sync.Mutex
		*provider
		stamp time.Time
	}

	receipts struct {
		sync.RWMutex
		cache     map[common.Hash]*receiptRecord
		lastClean time.Time
	}
}

var _ ethFetcher = (*multiRPCClient)(nil)

func newMultiRPCClient(dir string, endpoints []string, log dex.Logger, cfg *params.ChainConfig, net dex.Network) (*multiRPCClient, error) {
	walletDir := getWalletDir(dir, net)
	creds, err := pathCredentials(filepath.Join(walletDir, "keystore"))
	if err != nil {
		return nil, fmt.Errorf("error parsing credentials from %q: %w", dir, err)
	}

	m := &multiRPCClient{
		net:       net,
		cfg:       cfg,
		log:       log,
		creds:     creds,
		chainID:   cfg.ChainID,
		endpoints: endpoints,
	}
	m.receipts.cache = make(map[common.Hash]*receiptRecord)
	m.receipts.lastClean = time.Now()

	return m, nil
}

// connectProviders attempts to connect to the list of endpoints, returning a
// list of providers that were successfully connected. It is not an error for a
// connection to fail, unless all endpoints fail. The caller can infer failed
// connections from the length and contents of the returned provider list.
func connectProviders(ctx context.Context, endpoints []string, log dex.Logger, chainID *big.Int, net dex.Network) ([]*provider, error) {
	providers := make([]*provider, 0, len(endpoints))
	var success bool

	defer func() {
		if !success {
			for _, p := range providers {
				p.shutdown()
			}
		}
	}()

	// addEndpoint only returns errors that should be propagated immediately.
	addEndpoint := func(endpoint string) error {
		// Give ourselves a limited time to resolve a connection.
		timedCtx, cancel := context.WithTimeout(ctx, defaultRequestTimeout)
		defer cancel()
		// First try to get a websocket connection. WebSockets have a header
		// feed, so are much preferred to http connections. So much so, that
		// we'll do some path inspection here and make an attempt to find a
		// websocket server, even if the user requested http.
		var ec *ethclient.Client
		var rpcClient *rpc.Client
		var sub ethereum.Subscription
		var wsSubscribed bool
		var h chan *types.Header
		host := providerIPC
		if !strings.HasSuffix(endpoint, ".ipc") {
			wsURL, err := url.Parse(endpoint)
			if err != nil {
				return fmt.Errorf("failed to parse url %q: %w", endpoint, err)
			}
			host = wsURL.Host
			ogScheme := wsURL.Scheme
			switch ogScheme {
			case "https":
				wsURL.Scheme = "wss"
			case "http":
				wsURL.Scheme = "ws"
			case "ws", "wss":
			default:
				return fmt.Errorf("unknown scheme for endpoint %q: %q, expected any of: ws(s)/http(s)",
					endpoint, wsURL.Scheme)
			}
			replaced := ogScheme != wsURL.Scheme

			// Handle known paths.
			switch {
			case strings.Contains(wsURL.String(), "infura.io/v3"):
				if replaced {
					wsURL.Path = "/ws" + wsURL.Path
				}
			case strings.Contains(wsURL.Host, "rpc.rivet.cloud"):
				// subdomain contains API key, so can't simply replace.
				wsURL.Host = strings.Replace(wsURL.Host, ".rpc.", ".ws.", 1)
				host = providerRivetCloud
			}

			rpcClient, err = rpc.DialWebsocket(timedCtx, wsURL.String(), "")
			if err == nil {
				ec = ethclient.NewClient(rpcClient)
				h = make(chan *types.Header, 8)
				sub, err = ec.SubscribeNewHead(timedCtx, h)
				if err != nil {
					rpcClient.Close()
					ec = nil
					if replaced {
						log.Debugf("Connected to websocket, but headers subscription not supported. Trying HTTP")
					} else {
						log.Errorf("Connected to websocket, but headers subscription not supported. Attempting HTTP fallback")
					}
				} else {
					wsSubscribed = true
				}
			} else {
				if replaced {
					log.Debugf("couldn't get a websocket connection for %q (original scheme: %q) (OK)", wsURL, ogScheme)
				} else {
					log.Errorf("failed to get websocket connection to %q. attempting http(s) fallback: error = %v", endpoint, err)
				}
			}
		}
		// Weren't able to get a websocket connection. Try HTTP now. Dial does
		// path discrimination, so I won't even try to validate the protocol.
		if ec == nil {
			var err error
			rpcClient, err = rpc.DialContext(timedCtx, endpoint)
			if err != nil {
				log.Errorf("error creating http client for %q: %v", endpoint, err)
				return nil
			}
			ec = ethclient.NewClient(rpcClient)
		}

		// Get chain ID.
		reportedChainID, err := ec.ChainID(timedCtx)
		if err != nil {
			// If we can't get a header, don't use this provider.
			ec.Close()
			log.Errorf("Failed to get chain ID from %q: %v", endpoint, err)
			return nil
		}
		if chainID.Cmp(reportedChainID) != 0 {
			ec.Close()
			log.Errorf("%q reported wrong chain ID. expected %d, got %d", endpoint, chainID, reportedChainID)
			return nil
		}

		hdr, err := ec.HeaderByNumber(timedCtx, nil /* latest */)
		if err != nil {
			// If we can't get a header, don't use this provider.
			ec.Close()
			log.Errorf("Failed to get header from %q: %v", endpoint, err)
			return nil
		}

		p := &provider{
			chainID:      chainID,
			host:         host,
			endpointAddr: endpoint,
			ws:           wsSubscribed,
			net:          net,
			ec: &combinedRPCClient{
				Client: ec,
				rpc:    rpcClient,
			},
		}
		p.setTip(hdr, log)

		ctx, cancel := context.WithCancel(ctx)
		var wg sync.WaitGroup

		// Start websocket listen loop.
		if wsSubscribed {
			wg.Add(1)
			go func() {
				p.subscribeHeaders(ctx, sub, h, log)
				wg.Done()
			}()
		}
		wg.Add(1)
		go func() {
			p.refreshHeader(ctx, log)
			wg.Done()
		}()

		p.stop = func() {
			cancel()
			wg.Wait()
		}

		providers = append(providers, p)

		return nil
	}

	for _, endpoint := range endpoints {
		if err := addEndpoint(endpoint); err != nil {
			return nil, err
		}
	}

	if len(providers) == 0 {
		return nil, fmt.Errorf("failed to connect to even single provider among: %s",
			failedProviders(providers, endpoints))
	}

	log.Debugf("Connected with %d of %d RPC providers", len(providers), len(endpoints))

	success = true
	return providers, nil
}

func (m *multiRPCClient) connect(ctx context.Context) (err error) {
	providers, err := connectProviders(ctx, m.endpoints, m.log, m.chainID, m.net)
	if err != nil {
		return err
	}

	m.providerMtx.Lock()
	m.providers = providers
	m.providerMtx.Unlock()

	var connections int
	for _, p := range m.providerList() {
		if _, err := p.bestHeader(ctx, m.log); err != nil {
			m.log.Errorf("Failed to synchrnoize header from %s: %v", p.host, err)
		} else {
			connections++
		}
	}
	// TODO: Require at least two if all connections are non-local.
	if connections == 0 {
		return fmt.Errorf("no connections established")
	}

	go func() {
		<-ctx.Done()
		for _, p := range m.providerList() {
			p.shutdown()
		}
	}()

	return nil
}

// registerNonce returns true and saves the nonce for the next call when a nonce
// has not been received recently.
func (m *multiRPCClient) registerNonce(nonce uint64) bool {
	const expiration = time.Minute
	ln := &m.lastNonce
	set := func() bool {
		ln.nonce = nonce
		ln.stamp = time.Now()
		return true
	}
	ln.Lock()
	defer ln.Unlock()
	// Ok if the nonce is larger than previous.
	if ln.nonce < nonce {
		return set()
	}
	// Ok if initiation.
	if ln.stamp.IsZero() {
		return set()
	}
	// Ok if expiration has passed.
	if time.Now().After(ln.stamp.Add(expiration)) {
		return set()
	}
	// Nonce is the same or less than previous and expiration has not
	// passed.
	return false
}

// voidUnusedNonce sets time to zero time so that the next call to registerNonce
// will return true. This is needed when we know that a tx has failed at the
// time of sending so that the same nonce can be used again.
func (m *multiRPCClient) voidUnusedNonce() {
	m.lastNonce.Lock()
	defer m.lastNonce.Unlock()
	m.lastNonce.stamp = time.Time{}
}

// createAndCheckProviders creates and connects to providers. It checks that
// unknown providers have a sufficient api to trade and saves good providers to
// file. One bad provider or connect problem will cause this to error.
func createAndCheckProviders(ctx context.Context, walletDir string, endpoints []string, chainID *big.Int,
	compat *CompatibilityData, net dex.Network, log dex.Logger) error {

	var localCP map[string]bool
	path := filepath.Join(walletDir, "compliant-providers.json")
	b, err := os.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(b, &localCP); err != nil {
			log.Warnf("Couldn't parse compliant providers file: %v", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		log.Warnf("Error reading providers file: %v", err)
	}
	if localCP == nil {
		localCP = make(map[string]bool)
	}

	var writeLocalCP bool

	var unknownEndpoints []string
	for _, p := range endpoints {
		d, err := domain(p)
		if err != nil {
			log.Warnf("unable to parse domain for endpoint %s: %v", p, err)
			unknownEndpoints = append(unknownEndpoints, p)
			continue
		}
		if localCP[d] {
			continue
		}
		writeLocalCP = true
		localCP[d] = true
		if _, known := compliantProviders[d]; !known {
			unknownEndpoints = append(unknownEndpoints, p)
		}
	}

	if len(unknownEndpoints) > 0 {
		providers, err := connectProviders(ctx, unknownEndpoints, log, chainID, net)
		if err != nil {
			return fmt.Errorf("expected to successfully connect to at least 1 of these unfamiliar providers: %s",
				failedProviders(providers, unknownEndpoints))
		}
		defer func() {
			for _, p := range providers {
				p.shutdown()
			}
		}()
		if len(providers) != len(unknownEndpoints) {
			return fmt.Errorf("expected to successfully connect to all of these unfamiliar providers: %s",
				failedProviders(providers, unknownEndpoints))
		}
		if err := checkProvidersCompliance(ctx, providers, compat, dex.Disabled /* logger is for testing only */); err != nil {
			return err
		}
	}
	if writeLocalCP {
		// All unknown providers were checked.
		b, err := json.Marshal(localCP)
		if err != nil {
			return err
		}
		if err := os.WriteFile(path, b, 0644); err != nil {
			log.Errorf("Failed to write compliant providers file: %v", err)
		}
	}
	return nil
}

// failedProviders builds string message that describes provider endpoints we
// tried to connect to but didn't succeed.
func failedProviders(succeeded []*provider, tried []string) string {
	ok := make(map[string]bool)
	for _, p := range succeeded {
		ok[p.endpointAddr] = true
	}
	notOK := make([]string, 0, len(tried)-len(succeeded))
	for _, endpoint := range tried {
		if !ok[endpoint] {
			if d, err := domain(endpoint); err == nil {
				endpoint = d
			}
			notOK = append(notOK, endpoint)
		}
	}
	return strings.Join(notOK, " ")
}

func (m *multiRPCClient) reconfigure(ctx context.Context, settings map[string]string, compat *CompatibilityData, walletDir string) error {
	providerDef := settings[providersKey]
	if len(providerDef) == 0 {
		return errors.New("no providers specified")
	}
	endpoints := strings.Split(providerDef, " ")
	if err := createAndCheckProviders(ctx, walletDir, endpoints, m.chainID, compat, m.net, m.log); err != nil {
		return fmt.Errorf("create and check providers: %v", err)
	}
	providers, err := connectProviders(ctx, endpoints, m.log, m.chainID, m.net)
	if err != nil {
		return err
	}
	m.providerMtx.Lock()
	oldProviders := m.providers
	m.providers = providers
	m.endpoints = endpoints
	m.providerMtx.Unlock()
	for _, p := range oldProviders {
		p.shutdown()
	}
	return nil
}

func (m *multiRPCClient) cachedReceipt(txHash common.Hash) *types.Receipt {
	m.receipts.Lock()
	defer m.receipts.Unlock()

	cached := m.receipts.cache[txHash]

	// Periodically clean up the receipts.
	if time.Since(m.receipts.lastClean) > time.Minute*20 {
		m.receipts.lastClean = time.Now()
		defer func() {
			for txHash, rec := range m.receipts.cache {
				if time.Since(rec.lastAccess) > receiptCacheExpiration {
					delete(m.receipts.cache, txHash)
				}
			}
		}()
	}

	// If confirmed or if it was just fetched, return it as is.
	if cached != nil {
		// If the cached receipt has the requisite confirmations, it's always
		// considered good and we'll just update the lastAccess stamp so we don't
		// delete it from the map.
		// If it's not confirmed, we never update the lastAccess stamp, which just
		// serves to age out the receipt so a new one can be requested and
		// confirmations checked again.
		if cached.confirmed {
			cached.lastAccess = time.Now()
		}
		if time.Since(cached.lastAccess) < unconfirmedReceiptExpiration {
			return cached.r
		}
	}
	return nil
}

func (m *multiRPCClient) transactionReceipt(ctx context.Context, txHash common.Hash) (r *types.Receipt, tx *types.Transaction, err error) {
	// TODO
	// TODO: Plug in to the monitoredTx system from #1638.
	// TODO
	if tx, _, err = m.getTransaction(ctx, txHash); err != nil {
		return nil, nil, err
	}

	if r = m.cachedReceipt(txHash); r != nil {
		return r, tx, nil
	}

	// Fetch a fresh one.
	if err = m.withPreferred(ctx, func(ctx context.Context, p *provider) error {
		r, err = p.ec.TransactionReceipt(ctx, txHash)
		return err
	}); err != nil {
		if isNotFoundError(err) {
			return nil, nil, asset.CoinNotFoundError
		}
		return nil, nil, err
	}

	var confs int64
	if r.BlockNumber != nil {
		tip, err := m.bestHeader(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("bestHeader error: %v", err)
		}
		confs = new(big.Int).Sub(tip.Number, r.BlockNumber).Int64() + 1
	}

	m.receipts.Lock()
	m.receipts.cache[txHash] = &receiptRecord{
		r:          r,
		lastAccess: time.Now(),
		confirmed:  confs > txConfsNeededToConfirm,
	}
	m.receipts.Unlock()

	return r, tx, nil
}

type rpcTransaction struct {
	tx *types.Transaction
	txExtraDetail
}

type txExtraDetail struct {
	BlockNumber *string         `json:"blockNumber,omitempty"`
	BlockHash   *common.Hash    `json:"blockHash,omitempty"`
	From        *common.Address `json:"from,omitempty"`
}

func (tx *rpcTransaction) UnmarshalJSON(b []byte) error {
	if err := json.Unmarshal(b, &tx.tx); err != nil {
		return err
	}
	return json.Unmarshal(b, &tx.txExtraDetail)
}

func getRPCTransaction(ctx context.Context, p *provider, txHash common.Hash) (*rpcTransaction, error) {
	var resp *rpcTransaction
	err := p.ec.rpc.CallContext(ctx, &resp, "eth_getTransactionByHash", txHash)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, asset.CoinNotFoundError
	}
	// Just copying geth with this one.
	if _, r, _ := resp.tx.RawSignatureValues(); r == nil {
		return nil, fmt.Errorf("server returned transaction without signature")
	}
	return resp, nil
}

func (m *multiRPCClient) getTransaction(ctx context.Context, txHash common.Hash) (tx *types.Transaction, h int64, err error) {
	return tx, h, m.withPreferred(ctx, func(ctx context.Context, p *provider) error {
		resp, err := getRPCTransaction(ctx, p, txHash)
		if err != nil {
			if isNotFoundError(err) {
				return asset.CoinNotFoundError
			}
			return err
		}
		tx = resp.tx
		if resp.BlockNumber == nil {
			h = -1
		} else {
			bigH, ok := new(big.Int).SetString(*resp.BlockNumber, 0 /* must start with 0x */)
			if !ok {
				return fmt.Errorf("couldn't parse hex number %q", *resp.BlockNumber)
			}
			h = bigH.Int64()
		}
		return nil
	})
}

func (m *multiRPCClient) getConfirmedNonce(ctx context.Context) (n uint64, err error) {
	return n, m.withPreferred(ctx, func(ctx context.Context, p *provider) error {
		n, err = p.ec.PendingNonceAt(ctx, m.address())
		return err
	})
}

func (m *multiRPCClient) providerList() []*provider {
	m.providerMtx.RLock()
	defer m.providerMtx.RUnlock()

	providers := make([]*provider, len(m.providers))
	copy(providers, m.providers)
	return providers
}

// acceptabilityFilter: When running a pick-a-provider function (withOne,
// withAny, withPreferred), sometimes errors will need special handling
// depending on what they are. Zero or more acceptabilityFilters can be added
// to provide extra control.
//
//	 discard: If a filter indicates discard = true, the error will be discarded,
//	   provider iteration will end immediately and a nil error will be returned.
//	propagate: If a filter indicates propagate = true, provider iteration will
//	  be ended and the error will be returned immediately.
//	fail: If a filter indicates fail = true, the provider will be quarantined
//	  and provider iteration will continue
//
// If false is returned for all three for all filters, the error is logged and
// provider iteration will continue.
type acceptabilityFilter func(error) (discard, propagate, fail bool)

func allRPCErrorsAreFails(err error) (discard, propagate, fail bool) {
	return false, false, true
}

func errorFilter(err error, matches ...interface{}) bool {
	errStr := err.Error()
	for _, mi := range matches {
		var s string
		switch m := mi.(type) {
		case string:
			s = m
		case error:
			if errors.Is(err, m) {
				return true
			}
			s = m.Error()
		}
		if strings.Contains(errStr, s) {
			return true
		}
	}
	return false
}

// withOne runs the provider function against the providers in order until one
// succeeds or all have failed. The context used to run functions has a time
// limit equal to defaultRequestTimeout for all requests to return. If
// operations are expected to run longer than that the calling function should
// not use the altered context.
func (m *multiRPCClient) withOne(ctx context.Context, providers []*provider, f func(context.Context, *provider) error, acceptabilityFilters ...acceptabilityFilter) (superError error) {
	readyProviders := make([]*provider, 0, len(providers))
	for _, p := range providers {
		if !p.failed() {
			readyProviders = append(readyProviders, p)
		}
	}
	if len(readyProviders) == 0 {
		// Just try them all.
		m.log.Tracef("all providers in a failed state, so acting like none are")
		readyProviders = providers
	}
	for _, p := range readyProviders {
		ctx, cancel := context.WithTimeout(ctx, defaultRequestTimeout)
		err := f(ctx, p)
		cancel()
		if err == nil {
			return nil
		}
		if superError == nil {
			superError = err
		} else if err.Error() != superError.Error() {
			superError = fmt.Errorf("%v: %w", superError, err)
		}
		for _, f := range acceptabilityFilters { // use case for more than one? is it just variadic to allow 0?
			discard, propagate, fail := f(err)
			if discard {
				return nil
			}
			if propagate {
				return err
			}
			if fail {
				p.setFailed()
			}
		}
	}
	if superError == nil {
		return errors.New("all providers in a failed state")
	}
	return
}

// withAll runs the provider function against all known providers in order of
// freshness, with any non-stale nonce provider first. This is similar to
// withPreferred, except that it does not stop after the first success. However,
// if an acceptability filter indicates to "propagate" the error (hard stop), it
// will not try all providers. withAll should only be used for actions that are
// safe to repeat, such as broadcasting a transaction or getting results for a
// read-only operation.
func (m *multiRPCClient) withAll(ctx context.Context, f func(context.Context, *provider) error,
	acceptabilityFilters ...acceptabilityFilter) error {
	var atLeastOne bool
	var errs []error
	for _, p := range m.nonceProviderList() {
		if p.failed() {
			continue
		}
		ctx, cancel := context.WithTimeout(ctx, defaultRequestTimeout)
		err := f(ctx, p)
		cancel()
		if err == nil {
			atLeastOne = true // return nil err unless a later "propagated" error says to
			continue
		}
		var discarded bool
		for i, f := range acceptabilityFilters {
			discard, propagate, fail := f(err)
			discarded = discard && (discarded || i == 0)
			if discard {
				m.log.Tracef("non-fatal provider error: %v (%T / %T)", err, err, errors.Unwrap(err))
				continue // or maybe break since what is the use case of conflicting filters?
			}
			if fail {
				p.setFailed()
			}
			if propagate {
				return err
			}
		}
		if discarded {
			atLeastOne = true
		} else {
			errs = append(errs, err)
			m.log.Warnf("Failed request from %q: %v", p, err)
		}
	}

	if atLeastOne {
		return nil
	}
	if errs == nil {
		return errors.New("all providers in a failed state")
	}
	// TODO: use errors.Join(errs) when Go 1.20 is the min
	var b []byte
	for i, err := range errs {
		if i > 0 {
			b = append(b, '\n')
		}
		b = append(b, err.Error()...)
	}
	return errors.New(string(b))
}

// withAny runs the provider function against known providers in random order
// until one succeeds or all have failed.
func (m *multiRPCClient) withAny(ctx context.Context, f func(context.Context, *provider) error, acceptabilityFilters ...acceptabilityFilter) error {
	providers := m.providerList()
	shuffleProviders(providers)
	return m.withOne(ctx, providers, f, acceptabilityFilters...)
}

// withFreshest runs the provider function against known providers in order of
// best header time until one succeeds or all have failed.
func (m *multiRPCClient) withFreshest(ctx context.Context, f func(context.Context, *provider) error, acceptabilityFilters ...acceptabilityFilter) error {
	providers := m.freshnessSortedProviders()
	return m.withOne(ctx, providers, f, acceptabilityFilters...)
}

// withPreferred is like withAny, but will prioritize recently used nonce
// providers.
func (m *multiRPCClient) withPreferred(ctx context.Context, f func(context.Context, *provider) error, acceptabilityFilters ...acceptabilityFilter) error {
	return m.withOne(ctx, m.nonceProviderList(), f, acceptabilityFilters...)
}

// freshnessSortedProviders generates a list of providers sorted by their header
// times, newest first.
func (m *multiRPCClient) freshnessSortedProviders() []*provider {
	unsorted := m.providerList()
	type stampedProvider struct {
		stamp time.Time
		p     *provider
	}
	sps := make([]*stampedProvider, len(unsorted))
	for i, p := range unsorted {
		p.tip.RLock()
		stamp := p.tip.headerStamp
		p.tip.RUnlock()
		sps[i] = &stampedProvider{
			stamp: stamp,
			p:     p,
		}
	}
	sort.Slice(sps, func(i, j int) bool { return sps[i].stamp.Before(sps[j].stamp) })
	providers := make([]*provider, len(sps))
	for i, sp := range sps {
		providers[i] = sp.p
	}
	return providers
}

// nonceProviderList returns the freshness-sorted provider list, but with any recent
// nonce provider inserted in the first position.
func (m *multiRPCClient) nonceProviderList() []*provider {
	var lastProvider *provider
	m.lastProvider.Lock()
	if time.Since(m.lastProvider.stamp) < nonceProviderStickiness {
		lastProvider = m.lastProvider.provider
	}
	m.lastProvider.Unlock()

	freshProviders := m.freshnessSortedProviders()

	providers := make([]*provider, 0, len(m.providers))
	for _, p := range freshProviders {
		if lastProvider != nil && lastProvider.host == p.host {
			continue // adding lastProvider below, as preferred provider
		}
		providers = append(providers, p)
	}

	if lastProvider != nil {
		providers = append([]*provider{lastProvider}, providers...)
	}

	return providers
}

// nextNonce returns the next nonce number for the account.
func (m *multiRPCClient) nextNonce(ctx context.Context) (nonce uint64, err error) {
	checks := 5
	checkDelay := time.Second * 5
	for i := 0; i < checks; i++ {
		var host string
		err = m.withPreferred(ctx, func(ctx context.Context, p *provider) error {
			host = p.host
			nonce, err = p.ec.PendingNonceAt(ctx, m.creds.addr)
			return err
		})
		if err != nil {
			return 0, err
		}
		if m.registerNonce(nonce) {
			return nonce, nil
		}
		m.log.Warnf("host %s returned recently used account nonce number %d. try %d of %d.",
			host, nonce, i+1, checks)
		// Delay all but the last check.
		if i+1 < checks {
			select {
			case <-time.After(checkDelay):
			case <-ctx.Done():
				return 0, ctx.Err()
			}
		}
	}
	return 0, errors.New("preferred provider returned a recently used account nonce")
}

func (m *multiRPCClient) address() common.Address {
	return m.creds.addr
}

func (m *multiRPCClient) addressBalance(ctx context.Context, addr common.Address) (bal *big.Int, err error) {
	return bal, m.withFreshest(ctx, func(ctx context.Context, p *provider) error {
		bal, err = p.ec.BalanceAt(ctx, addr, nil /* latest */)
		return err
	})
}

func (m *multiRPCClient) bestHeader(ctx context.Context) (hdr *types.Header, err error) {
	// Check for an unexpired cached header first.
	var bestHeader *types.Header
	for _, p := range m.providerList() {
		h := p.cachedTip()
		if h == nil {
			continue
		}
		if bestHeader == nil ||
			// In fact, we should be comparing the total terminal difficulty of
			// the blocks. We don't have the TTD, even though it is sent by RPC,
			// because ethclient strips it from header data and the header
			// subscriptions may or may not send the ttd (Infura docs do not
			// show it in message), but it doesn't come through the geth client
			// subscription machinery regardless.
			h.Number.Cmp(bestHeader.Number) > 0 {

			bestHeader = h
		}
	}
	if bestHeader != nil {
		return bestHeader, nil
	}

	return hdr, m.withAny(ctx, func(ctx context.Context, p *provider) error {
		hdr, err = p.bestHeader(ctx, m.log)
		return err
	}, allRPCErrorsAreFails)
}

func (m *multiRPCClient) headerByHash(ctx context.Context, h common.Hash) (hdr *types.Header, err error) {
	return hdr, m.withAny(ctx, func(ctx context.Context, p *provider) error {
		hdr, err = p.headerByHash(ctx, h)
		return err
	})
}

func (m *multiRPCClient) chainConfig() *params.ChainConfig {
	return m.cfg
}

func (m *multiRPCClient) peerCount() (c uint32) {
	m.providerMtx.RLock()
	defer m.providerMtx.RUnlock()
	for _, p := range m.providers {
		if !p.failed() {
			c++
		}
	}
	return
}

func (m *multiRPCClient) contractBackend() bind.ContractBackend {
	return m
}

func (m *multiRPCClient) lock() error {
	return m.creds.ks.Lock(m.creds.addr)
}

func (m *multiRPCClient) locked() bool {
	status, _ := m.creds.wallet.Status()
	return status != "Unlocked"
}

func (m *multiRPCClient) shutdown() {
	for _, p := range m.providerList() {
		p.shutdown()
	}

}

func (m *multiRPCClient) sendSignedTransaction(ctx context.Context, tx *types.Transaction) error {
	var lastProvider *provider
	if err := m.withAll(ctx, func(ctx context.Context, p *provider) error {
		lastProvider = p
		m.log.Tracef("Sending signed tx via %q", p.host)
		return p.ec.SendTransaction(ctx, tx)
	}, func(err error) (discard, propagate, fail bool) {
		// NOTE: err never hits errors.Is(err, txpool.ErrAlreadyKnown) because
		// err is a *rpc.jsonError, but it does have a Message that matches.
		return errorFilter(err, txpool.ErrAlreadyKnown, "known transaction"), false, false
	}); err != nil {
		return err
	}
	m.lastProvider.Lock()
	m.lastProvider.provider = lastProvider
	m.lastProvider.stamp = time.Now()
	m.lastProvider.Unlock()
	return nil
}

func (m *multiRPCClient) sendTransaction(ctx context.Context, txOpts *bind.TransactOpts, to common.Address, data []byte) (*types.Transaction, error) {
	tx, err := m.creds.ks.SignTx(*m.creds.acct, types.NewTx(&types.DynamicFeeTx{
		To:        &to,
		ChainID:   m.chainID,
		Nonce:     txOpts.Nonce.Uint64(),
		Gas:       txOpts.GasLimit,
		GasFeeCap: txOpts.GasFeeCap,
		GasTipCap: txOpts.GasTipCap,
		Value:     txOpts.Value,
		Data:      data,
	}), m.chainID)

	if err != nil {
		return nil, fmt.Errorf("signing error: %v", err)
	}

	return tx, m.sendSignedTransaction(ctx, tx)
}

func (m *multiRPCClient) signData(data []byte) (sig, pubKey []byte, err error) {
	return signData(m.creds, data)
}

// syncProgress: Current and Highest blocks are not very useful for the caller,
// but the best header's time in seconds can be used to determine if the
// provider is out of sync.
func (m *multiRPCClient) syncProgress(ctx context.Context) (prog *ethereum.SyncProgress, tipTime uint64, err error) {
	return prog, tipTime, m.withAny(ctx, func(ctx context.Context, p *provider) error {
		tip, err := p.bestHeader(ctx, m.log)
		if err != nil {
			return err
		}
		tipTime = tip.Time

		prog = &ethereum.SyncProgress{
			CurrentBlock: tip.Number.Uint64(),
			HighestBlock: tip.Number.Uint64(),
		}
		return nil
	}, allRPCErrorsAreFails)
}

func (m *multiRPCClient) transactionConfirmations(ctx context.Context, txHash common.Hash) (confs uint32, err error) {
	var r *types.Receipt
	var tip *types.Header
	if err := m.withPreferred(ctx, func(ctx context.Context, p *provider) error {
		r, err = p.ec.TransactionReceipt(ctx, txHash)
		if err != nil {
			return err
		}
		tip, err = p.bestHeader(ctx, m.log)
		return err
	}); err != nil {
		if isNotFoundError(err) {
			return 0, asset.CoinNotFoundError
		}
		return 0, err
	}
	if r.BlockNumber != nil && tip.Number != nil {
		bigConfs := new(big.Int).Sub(tip.Number, r.BlockNumber)
		if bigConfs.Sign() < 0 { // avoid potential overflow
			return 0, nil
		}
		bigConfs.Add(bigConfs, big.NewInt(1))
		if bigConfs.IsInt64() {
			return uint32(bigConfs.Int64()), nil
		}
	}
	return 0, nil
}

// txOpts creates transaction options and sets the passed nonce if supplied. If
// nonce is nil the next nonce will be fetched and the passed argument altered.
func (m *multiRPCClient) txOpts(ctx context.Context, val, maxGas uint64, maxFeeRate, nonce *big.Int) (*bind.TransactOpts, error) {
	baseFees, gasTipCap, err := m.currentFees(ctx)
	if err != nil {
		return nil, err
	}

	if maxFeeRate == nil {
		maxFeeRate = new(big.Int).Mul(baseFees, big.NewInt(2))
	}

	txOpts := newTxOpts(ctx, m.creds.addr, val, maxGas, maxFeeRate, gasTipCap)

	// If nonce is not nil, this indicates that we are trying to re-send an
	// old transaction with higher fee in order to ensure it is mined.
	if nonce == nil {
		n, err := m.nextNonce(ctx)
		if err != nil {
			return nil, fmt.Errorf("error getting nonce: %v", err)
		}
		nonce = new(big.Int).SetUint64(n)
	}
	txOpts.Nonce = nonce

	txOpts.Signer = func(addr common.Address, tx *types.Transaction) (*types.Transaction, error) {
		return m.creds.wallet.SignTx(*m.creds.acct, tx, m.chainID)
	}

	return txOpts, nil

}

func (m *multiRPCClient) currentFees(ctx context.Context) (baseFees, tipCap *big.Int, err error) {
	return baseFees, tipCap, m.withAny(ctx, func(ctx context.Context, p *provider) error {
		hdr, err := p.bestHeader(ctx, m.log)
		if err != nil {
			return err
		}

		baseFees = misc.CalcBaseFee(m.cfg, hdr)

		if baseFees.Cmp(minGasPrice) < 0 {
			baseFees.Set(minGasPrice)
		}

		tipCap = p.suggestTipCap(ctx, m.log)

		return nil
	})
}

func (m *multiRPCClient) unlock(pw string) error {
	return m.creds.ks.TimedUnlock(*m.creds.acct, pw, 0)
}

// Methods below implement bind.ContractBackend

var _ bind.ContractBackend = (*multiRPCClient)(nil)

func (m *multiRPCClient) CodeAt(ctx context.Context, contract common.Address, blockNumber *big.Int) (code []byte, err error) {
	return code, m.withAny(ctx, func(ctx context.Context, p *provider) error {
		code, err = p.ec.CodeAt(ctx, contract, blockNumber)
		return err
	})
}

func (m *multiRPCClient) CallContract(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) (res []byte, err error) {
	return res, m.withPreferred(ctx, func(ctx context.Context, p *provider) error {
		res, err = p.ec.CallContract(ctx, call, blockNumber)
		return err
	})
}

func (m *multiRPCClient) HeaderByNumber(ctx context.Context, number *big.Int) (hdr *types.Header, err error) {
	return hdr, m.withAny(ctx, func(ctx context.Context, p *provider) error {
		hdr, err = p.ec.HeaderByNumber(ctx, number)
		return err
	})
}

func (m *multiRPCClient) PendingCodeAt(ctx context.Context, account common.Address) (code []byte, err error) {
	return code, m.withAny(ctx, func(ctx context.Context, p *provider) error {
		code, err = p.ec.PendingCodeAt(ctx, account)
		return err
	})
}

func (m *multiRPCClient) PendingNonceAt(ctx context.Context, account common.Address) (nonce uint64, err error) {
	return nonce, m.withPreferred(ctx, func(ctx context.Context, p *provider) error {
		nonce, err = p.ec.PendingNonceAt(ctx, account)
		return err
	})
}

func (m *multiRPCClient) SuggestGasPrice(ctx context.Context) (price *big.Int, err error) {
	return price, m.withAny(ctx, func(ctx context.Context, p *provider) error {
		price, err = p.ec.SuggestGasPrice(ctx)
		return err
	})
}

func (m *multiRPCClient) SuggestGasTipCap(ctx context.Context) (tipCap *big.Int, err error) {
	return tipCap, m.withAny(ctx, func(ctx context.Context, p *provider) error {
		tipCap = p.suggestTipCap(ctx, m.log)
		return nil
	})
}

func (m *multiRPCClient) EstimateGas(ctx context.Context, call ethereum.CallMsg) (gas uint64, err error) {
	return gas, m.withAny(ctx, func(ctx context.Context, p *provider) error {
		gas, err = p.ec.EstimateGas(ctx, call)
		return err
	}, func(err error) (discard, propagate, fail bool) {
		// Assume this one will be the same all around.
		return false, errorFilter(err, "gas required exceeds allowance"), false
	})
}

func (m *multiRPCClient) SendTransaction(ctx context.Context, tx *types.Transaction) error {
	return m.sendSignedTransaction(ctx, tx)
}

func (m *multiRPCClient) FilterLogs(ctx context.Context, query ethereum.FilterQuery) (logs []types.Log, err error) {
	return logs, m.withAny(ctx, func(ctx context.Context, p *provider) error {
		logs, err = p.ec.FilterLogs(ctx, query)
		return err
	})
}

func (m *multiRPCClient) SubscribeFilterLogs(ctx context.Context, query ethereum.FilterQuery, ch chan<- types.Log) (sub ethereum.Subscription, err error) {
	return sub, m.withAny(ctx, func(ctx context.Context, p *provider) error {
		sub, err = p.ec.SubscribeFilterLogs(ctx, query, ch)
		return err
	})
}

const (
	// Compliant providers
	providerIPC         = "IPC"
	providerLinkPool    = "linkpool.io"
	providerMewAPI      = "mewapi.io"
	providerFlashBots   = "flashbots.net"
	providerMyCryptoAPI = "mycryptoapi.com"
	providerRunOnFlux   = "runonflux.io"
	providerInfura      = "infura.io"
	providerRivetCloud  = "rivet.cloud"
	providerAlchemy     = "alchemy.com"
	providerAnkr        = "ankr.com"
	providerBlast       = "blastapi.io"

	// Non-compliant providers
	// providerCloudflareETH = "cloudflare-eth.com" // "SuggestGasTipCap" error: Method not found
)

var compliantProviders = map[string]struct{}{
	providerLinkPool:    {},
	providerMewAPI:      {},
	providerFlashBots:   {},
	providerMyCryptoAPI: {},
	providerRunOnFlux:   {},
	providerInfura:      {},
	providerRivetCloud:  {},
	providerAlchemy:     {},
	providerAnkr:        {},
	providerBlast:       {},
}

type rpcTest struct {
	name string
	f    func(context.Context, *provider) error
}

// newCompatibilityTests returns a list of RPC tests to run to determine API
// compatibility.
// NOTE: The logger is intended for use the execution of the compatibility
// tests, and it will generally be dex.Disabled in production.
func newCompatibilityTests(cb bind.ContractBackend, compat *CompatibilityData, log dex.Logger) []*rpcTest {
	return []*rpcTest{
		{
			name: "HeaderByNumber",
			f: func(ctx context.Context, p *provider) error {
				_, err := p.ec.HeaderByNumber(ctx, nil /* latest */)
				return err
			},
		},
		{
			name: "HeaderByHash",
			f: func(ctx context.Context, p *provider) error {
				_, err := p.ec.HeaderByHash(ctx, compat.BlockHash)
				return err
			},
		},
		{
			name: "TransactionReceipt",
			f: func(ctx context.Context, p *provider) error {
				_, err := p.ec.TransactionReceipt(ctx, compat.TxHash)
				return err
			},
		},
		{
			name: "PendingNonceAt",
			f: func(ctx context.Context, p *provider) error {
				_, err := p.ec.PendingNonceAt(ctx, compat.Addr)
				return err
			},
		},
		{
			name: "SuggestGasTipCap",
			f: func(ctx context.Context, p *provider) error {
				tipCap, err := p.ec.SuggestGasTipCap(ctx)
				if err != nil {
					return err
				}
				log.Debugf("#### Retrieved tip cap: %d gwei", dexeth.WeiToGwei(tipCap))
				return nil
			},
		},
		{
			name: "BalanceAt",
			f: func(ctx context.Context, p *provider) error {
				bal, err := p.ec.BalanceAt(ctx, compat.Addr, nil)
				if err != nil {
					return err
				}
				log.Debugf("#### Balance retrieved: %.9f", float64(dexeth.WeiToGwei(bal))/1e9)
				return nil
			},
		},
		{
			name: "CodeAt",
			f: func(ctx context.Context, p *provider) error {
				if compat.TokenAddr == (common.Address{}) {
					log.Debug("#### Skipping CodeAt. No token address provided")
					return nil
				}
				code, err := p.ec.CodeAt(ctx, compat.TokenAddr, nil)
				if err != nil {
					return err
				}
				log.Debugf("#### %d bytes of USDC contract retrieved", len(code))
				return nil
			},
		},
		{
			name: "CallContract(balanceOf)",
			f: func(ctx context.Context, p *provider) error {
				if compat.TokenAddr == (common.Address{}) {
					log.Debug("#### Skipping CallContract. No token address provided")
					return nil
				}
				caller, err := erc20.NewIERC20(compat.TokenAddr, cb)
				if err != nil {
					return err
				}
				bal, err := caller.BalanceOf(&bind.CallOpts{
					From:    compat.Addr,
					Context: ctx,
				}, compat.Addr)
				if err != nil {
					return err
				}
				// I guess we would need to unpack the results. I don't really
				// know how to interpret these, but I'm really just looking for
				// a request error.
				log.Debug("#### ERC20 token balanceOf result:", dexeth.WeiToGwei(bal), "gwei")
				return nil
			},
		},
		{
			name: "ChainID",
			f: func(ctx context.Context, p *provider) error {
				chainID, err := p.ec.ChainID(ctx)
				if err != nil {
					return err
				}
				log.Debugf("#### Chain ID: %d", chainID)
				return nil
			},
		},
		{
			name: "getRPCTransaction",
			f: func(ctx context.Context, p *provider) error {
				rpcTx, err := getRPCTransaction(ctx, p, compat.TxHash)
				if err != nil {
					return err
				}
				var h string
				if rpcTx.BlockNumber != nil {
					h = *rpcTx.BlockNumber
				}
				log.Debugf("#### RPC Tx is nil? %t, block number: %q", rpcTx.tx == nil, h)
				return nil
			},
		},
	}
}

// domain accepts an url, ip, or file path and returns the domain:port if they
// exist. Returns just the domain if no port. Returns a cleaned file path if a
// file with .ipc suffix, otherwise returns the address as is if no errors were
// encountered.
func domain(addr string) (string, error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return "", errors.New("address is an empty string")
	}
	if strings.HasSuffix(addr, ".ipc") {
		return dex.CleanAndExpandPath(addr), nil // ipc file
	}
	const missingPort = "missing port in address"
	host, port, splitErr := net.SplitHostPort(addr)
	_, portErr := strconv.ParseUint(port, 10, 16)

	removeSubdomain := func(addr string) string {
		parts := strings.Split(host, ".")
		n := len(parts)
		if n <= 2 {
			return addr
		}
		// Possibly an ipv4 address with no subdomain.
		if ip := net.ParseIP(addr); ip != nil {
			return addr
		}
		// Top level domains such as .ne.jp or .co.uk exist.
		if n >= 3 && len(parts[n-2]) == 2 {
			return parts[n-3] + "." + parts[n-2] + "." + parts[n-1]
		}
		// Otherwise assume domain.topleveldomain at the end.
		return parts[n-2] + "." + parts[n-1]
	}

	// net.SplitHostPort will error on anything not in the format
	// string:string or :string or if a colon is in an unexpected position,
	// such as in the scheme.
	// If the port isn't a port, it must also be parsed.
	if splitErr != nil || portErr != nil {
		// Any address with no colons is appended with the default port.
		var addrErr *net.AddrError
		if errors.As(splitErr, &addrErr) && addrErr.Err == missingPort {
			if host == "" {
				// address was either an ip with no port like
				// [::1] or a file path with no .ipc at the end
				return addr, nil
			}
			return removeSubdomain(host), nil // no port
		}
		// These are addresses with at least one colon in an unexpected
		// position.
		a, err := url.Parse(addr)
		// This address is of an unknown format.
		if err != nil {
			return "", fmt.Errorf("addr %s of unknown format %v", addr, err)
		}
		host, port = a.Hostname(), a.Port()
		if port == "" {
			return removeSubdomain(host), nil // no port
		}
	}
	if host == "" {
		return "", fmt.Errorf("no domain found for %s", addr)
	}
	return net.JoinHostPort(removeSubdomain(host), port), nil
}

// checkProvidersCompliance verifies that providers support the API that DEX
// requires by sending a series of requests and verifying the responses. If a
// provider is found to be compliant, their domain name is added to a list and
// stored in a file on disk so that future checks can be short-circuited.
func checkProvidersCompliance(ctx context.Context, providers []*provider, compat *CompatibilityData, log dex.Logger) error {
	for _, p := range providers {
		// Need to run API tests on this endpoint.
		for _, t := range newCompatibilityTests(p.ec, compat, log) {
			ctx, cancel := context.WithTimeout(ctx, defaultRequestTimeout)
			err := t.f(ctx, p)
			cancel()
			if err != nil {
				return fmt.Errorf("%s: RPC Provider @ %q has a non-compliant API: %v", t.name, p.host, err)
			}
		}
	}
	return nil
}

// shuffleProviders shuffles the provider slice in-place.
func shuffleProviders(p []*provider) {
	rand.Shuffle(len(p), func(i, j int) {
		p[i], p[j] = p[j], p[i]
	})
}

func isNotFoundError(err error) bool {
	return strings.Contains(err.Error(), "not found")
}
