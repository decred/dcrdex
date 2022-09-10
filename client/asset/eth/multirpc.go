// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build lgpl

// https://ethereumnodes.com/ for RPC providers

package eth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"math/rand"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/networks/erc20"
	dexeth "decred.org/dcrdex/dex/networks/eth"
	"decred.org/dcrdex/server/asset"
	"github.com/decred/slog"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/misc"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rpc"
)

const (
	// failQuarantine is how long we will wait after a failed request before
	// trying a provider again.
	failQuarantine = time.Minute
	// receiptCacheExpiration is how long we will track a receipt after the
	// last request. There is no persistent storage, so all receipts are cached
	// in-memory.
	receiptCacheExpiration     = time.Hour
	tipCapSuggestionExpiration = time.Hour
	ipcHost                    = "IPC"
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
	host    string
	ec      *combinedRPCClient
	ws      bool
	tipCapV atomic.Value // *cachedTipCap

	// tip tracks the best known header as well as any error encount
	tip struct {
		sync.RWMutex
		header      *types.Header
		headerStamp time.Time
		failStamp   time.Time
		failCount   int
	}
}

func (p *provider) setTip(header *types.Header) {
	p.tip.Lock()
	p.tip.header = header
	p.tip.headerStamp = time.Now()
	p.tip.failStamp = time.Time{}
	p.tip.failCount = 0
	p.tip.Unlock()
}

// cachedTip retrieves the last known best header.
func (p *provider) cachedTip() *types.Header {
	stale := time.Second * 10
	if p.ws {
		stale = time.Minute * 2
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
	return time.Since(p.tip.failStamp) < failQuarantine || p.tip.failCount > 100
}

// bestHeader get the best known header from the provider, cached if available,
// otherwise a new RPC call is made.
func (p *provider) bestHeader(ctx context.Context, log dex.Logger) (*types.Header, error) {
	// Check if we have a cached header.
	if tip := p.cachedTip(); tip != nil {
		log.Tracef("using cached header from %q", p.host)
		return tip, nil
	}

	log.Tracef("fetching fresh header from %q", p.host)
	hdr, err := p.ec.HeaderByNumber(ctx, nil /* latest */)
	if err != nil {
		p.setFailed()
		return nil, fmt.Errorf("HeaderByNumber error: %w", err)
	}
	p.setTip(hdr)
	return hdr, nil
}

func (p *provider) headerByHash(ctx context.Context, h common.Hash) (*types.Header, error) {
	hdr, err := p.ec.HeaderByHash(ctx, h)
	if err != nil {
		p.setFailed()
		return nil, fmt.Errorf("HeaderByHash error: %w", err)
	}
	p.setTip(hdr)
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

// subscribeHeaders starts a listening loop for header updates for a provider.
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
				log.Warnf("can't resubscribe to %q headers: %v", err)
			}
			select {
			case <-time.After(time.Second * 30):
			case <-ctx.Done():
				return nil, context.Canceled
			}
		}
	}

	// I thought the filter logs might catch some transactions we coudld cache
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
			p.setTip(hdr)
		case err, ok := <-sub.Err():
			if !ok {
				// Subscription cancelled
				return
			}
			log.Errorf("%q header subscription error: %v", err)
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

	providerMtx sync.Mutex
	endpoints   []string
	providers   []*provider

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

func newMultiRPCClient(dir string, endpoints []string, log dex.Logger, cfg *params.ChainConfig, chainID *big.Int, net dex.Network) (*multiRPCClient, error) {
	walletDir := getWalletDir(dir, net)
	creds, err := pathCredentials(filepath.Join(walletDir, "keystore"))
	if err != nil {
		return nil, fmt.Errorf("error parsing credentials from %q: %w", dir, err)
	}

	m := &multiRPCClient{
		cfg:       cfg,
		log:       log,
		creds:     creds,
		chainID:   chainID,
		endpoints: endpoints,
	}
	m.receipts.cache = make(map[common.Hash]*receiptRecord)
	m.receipts.lastClean = time.Now()

	return m, nil
}

// connectProviders attempts to connnect to the list of endpoints, returning a
// list of providers that were succesfully connected. It is not an error for
// a connection to fail. The caller can infer failed connections from the
// length and contents of the returned provider list.
func connectProviders(ctx context.Context, endpoints []string, log dex.Logger, chainID *big.Int) ([]*provider, error) {
	providers := make([]*provider, 0, len(endpoints))
	var success bool
	for _, endpoint := range endpoints {
		// First try to get a websocket connection. Websockets have a header
		// feed, so are much preferred to http connections. So much so, that
		// we'll do some path inspection here and make an attempt to find a
		// websocket server, even if the user requested http.
		var ec *ethclient.Client
		var rpcClient *rpc.Client
		var sub ethereum.Subscription
		var h chan *types.Header
		host := ipcHost
		if !strings.HasSuffix(endpoint, ".ipc") {
			wsURL, err := url.Parse(endpoint)
			if err != nil {
				return nil, fmt.Errorf("Failed to parse url %q", endpoint)
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
				return nil, fmt.Errorf("unknown scheme for endpoint %q: %q", endpoint, wsURL.Scheme)
			}

			// Handle known paths.
			switch {
			case strings.Contains(wsURL.String(), "infura.io/v3"):
				wsURL.Path = "/ws" + wsURL.Path
			case strings.Contains(wsURL.Host, "rpc.rivet.cloud"):
				wsURL.Host = strings.Replace(wsURL.Host, ".rpc.", ".ws.", 1)
				host = "rivet.cloud" // subdomain contains api key
				// Alchemy is just a protocol change
			}

			replaced := ogScheme != wsURL.Scheme
			rpcClient, err = rpc.DialWebsocket(ctx, wsURL.String(), "")
			if err == nil {
				ec = ethclient.NewClient(rpcClient)
				h = make(chan *types.Header, 8)
				sub, err = ec.SubscribeNewHead(ctx, h)
				if err != nil {
					rpcClient.Close()
					ec = nil
					if replaced {
						log.Debugf("Connected to websocket, but headers subscription not supported. Trying HTTP")
					} else {
						log.Errorf("Connected to websocket, but headers subscription not supported. Attempting HTTP fallback")
					}
				}
			} else {
				if replaced {
					log.Debugf("couldn't get a websocket connection for %q (original scheme: %q) (OK)", wsURL, ogScheme)
				} else {
					log.Errorf("failed to get websocket connection to %q. attempting http(s) fallback: error = %v", endpoint, err)
				}
			}
		}
		// Weren't able to get a websocket connection. Try HTTP now.
		if ec == nil {
			var err error
			rpcClient, err = rpc.Dial(endpoint)
			if err != nil {
				log.Errorf("error creating http client for %q: %v", endpoint, err)
			}
			ec = ethclient.NewClient(rpcClient)
		}

		defer func() {
			if !success {
				ec.Close()
			}
		}()

		// Get chain ID.
		reportedChainID, err := ec.ChainID(ctx)
		if err != nil {
			// If we can't get a header, don't use this provider.
			ec.Close()
			log.Errorf("Failed to get chain ID from %q: %v", endpoint, err)
			continue
		}
		if chainID.Cmp(reportedChainID) != 0 {
			ec.Close()
			log.Errorf("%q reported wrong chain ID. expected %d, got %d", endpoint, chainID, reportedChainID)
			continue
		}

		hdr, err := ec.HeaderByNumber(ctx, nil /* latest */)
		if err != nil {
			// If we can't get a header, don't use this provider.
			ec.Close()
			log.Errorf("Failed to get header from %q: %v", endpoint, err)
			continue
		}

		p := &provider{
			host: host,
			ws:   sub != nil,
			ec: &combinedRPCClient{
				Client: ec,
				rpc:    rpcClient,
			},
		}
		p.setTip(hdr)
		providers = append(providers, p)

		// Start websocket listen loop.
		if sub != nil {
			go p.subscribeHeaders(ctx, sub, h, log)
		}
	}

	if len(providers) == 0 {
		return nil, fmt.Errorf("failed to connect")
	}

	log.Debugf("Connected with %d of %d RPC servers", len(providers), len(endpoints))

	success = true
	return providers, nil
}

func (m *multiRPCClient) connect(ctx context.Context) (err error) {
	providers, err := connectProviders(ctx, m.endpoints, m.log, m.chainID)
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
			p.ec.Close()
		}
	}()

	return nil
}

func (m *multiRPCClient) reconfigure(ctx context.Context, settings map[string]string) error {
	providerDef := settings[providersKey]
	if len(providerDef) == 0 {
		return errors.New("no providers specified")
	}
	endpoints := strings.Split(providerDef, " ")
	providers, err := connectProviders(ctx, endpoints, m.log, m.chainID)
	if err != nil {
		return err
	}
	m.providerMtx.Lock()
	oldProviders := m.providers
	m.providers = providers
	m.endpoints = endpoints
	m.providerMtx.Unlock()
	for _, p := range oldProviders {
		p.ec.Close()
	}
	return nil
}

// cleanReceipts cleans up the receipt cache, deleting any receipts that haven't
// been access for > receiptCacheExpiration.
func (m *multiRPCClient) cleanReceipts() {
	m.receipts.Lock()
	for txHash, rec := range m.receipts.cache {
		if time.Since(rec.lastAccess) > receiptCacheExpiration {
			delete(m.receipts.cache, txHash)
		}
	}
	m.receipts.Unlock()
}

<<<<<<< HEAD
func (m *multiRPCClient) transactionReceipt(ctx context.Context, txHash common.Hash) (r *types.Receipt, tx *types.Transaction, err error) {
	// TODO
	// TODO: Plug in to the monitoredTx system from #1638.
	// TODO
	if tx, _, err = m.getTransaction(ctx, txHash); err != nil {
		return nil, nil, err
	}

=======
func (m *multiRPCClient) transactionReceipt(ctx context.Context, txHash common.Hash) (r *types.Receipt, err error) {
>>>>>>> martonp early review followup
	// Check the cache.
	const cacheExpiration = time.Minute

	m.receipts.RLock()
	cached := m.receipts.cache[txHash]
	if cached != nil && cached.confirmed {
		cached.lastAccess = time.Now()
	}
	if time.Since(m.receipts.lastClean) > time.Minute*20 {
		m.receipts.lastClean = time.Now()
		go m.cleanReceipts()
	}
	m.receipts.RUnlock()
<<<<<<< HEAD
	if cached != nil {
		return cached.r, tx, nil
=======

	// If confirmed or if it was just fetched, return it as is.
	if cached != nil && (cached.confirmed || time.Since(cached.lastAccess) < cacheExpiration) {
		return cached.r, nil
>>>>>>> martonp early review followup
	}

	// Fetch a fresh one.
	if err = m.withPreferred(func(p *provider) error {
		r, err = p.ec.TransactionReceipt(ctx, txHash)
		return err
	}); err != nil {
		return nil, nil, err
	}

	var confs int64
	if r.BlockNumber != nil {
		tip, err := m.bestHeader(ctx)
		if err != nil {
			return nil, fmt.Errorf("bestHeader error: %v", err)
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

	// TODO
	// TODO: Plug into the monitoredTx system from #1638.
	// TODO
	if tx, _, err = m.getTransaction(ctx, txHash); err != nil {
		return nil, nil, err
	}

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
	return tx, h, m.withPreferred(func(p *provider) error {
		resp, err := getRPCTransaction(ctx, p, txHash)
		if err != nil {
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

func (m *multiRPCClient) getConfirmedNonce(ctx context.Context, blockNumber int64) (n uint64, err error) {
	return n, m.withPreferred(func(p *provider) error {
		n, err = p.ec.PendingNonceAt(ctx, m.address())
		return err
	})
}

func (m *multiRPCClient) providerList() []*provider {
	m.providerMtx.Lock()
	defer m.providerMtx.Unlock()

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
// succeeds or all have failed.
func (m *multiRPCClient) withOne(providers []*provider, f func(*provider) error, acceptabilityFilters ...acceptabilityFilter) (superError error) {
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
		err := f(p)
		if err == nil {
			break
		}
		if superError == nil {
			superError = err
		} else {
			superError = fmt.Errorf("%v: %w", superError, err)
		}
		for _, f := range acceptabilityFilters {
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
		m.log.Errorf("error from provider %q: %v", p.host, err)
	}
	return
}

// withAny runs the provider function against known providers in random order
// until one succeeds or all have failed.
func (m *multiRPCClient) withAny(f func(*provider) error, acceptabilityFilters ...acceptabilityFilter) error {
	providers := m.providerList()
	shuffleProviders(providers)
	return m.withOne(providers, f, acceptabilityFilters...)
}

// withPreferred is like withAny, but will prioritize recently used nonce
// providers.
func (m *multiRPCClient) withPreferred(f func(*provider) error, acceptabilityFilters ...acceptabilityFilter) error {
	return m.withOne(m.nonceProviderList(), f, acceptabilityFilters...)
}

// nonceProviderList returns the randomized provider list, but with any recent
// nonce provider inserted in the first position.
func (m *multiRPCClient) nonceProviderList() []*provider {
	m.providerMtx.Lock()
	defer m.providerMtx.Unlock()

	providers := make([]*provider, 0, len(m.providers))

	var lastProvider *provider
	if time.Since(m.lastProvider.stamp) < nonceProviderStickiness {
		lastProvider = m.lastProvider.provider
	}

	for _, p := range m.providers {
		if lastProvider != nil && lastProvider.host == p.host {
			continue // already added it
		}
		providers = append(providers, p)
	}

	shuffleProviders(providers)

	if lastProvider != nil {
		providers = append([]*provider{lastProvider}, providers...)
	}

	return providers
}

// nextNonce returns the next nonce number for the account.
func (m *multiRPCClient) nextNonce(ctx context.Context) (nonce uint64, err error) {
	return nonce, m.withPreferred(func(p *provider) error {
		nonce, err = p.ec.PendingNonceAt(ctx, m.creds.addr)
		return err
	})
}

func (m *multiRPCClient) address() common.Address {
	return m.creds.addr
}

func (m *multiRPCClient) addressBalance(ctx context.Context, addr common.Address) (bal *big.Int, err error) {
	return bal, m.withAny(func(p *provider) error {
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
		// This block choosing algo is probably too rudimentary. Really need
		// shnuld traverse parents to a common block and sum up gas (including
		// uncles?), I think.
		if bestHeader == nil || // first one
			h.Number.Cmp(bestHeader.Number) > 0 || // newer
			(h.Number.Cmp(bestHeader.Number) == 0 && h.GasUsed > bestHeader.GasUsed) { // same height, but more gas used

			bestHeader = h
		}
	}
	if bestHeader != nil {
		return bestHeader, nil
	}

	return hdr, m.withAny(func(p *provider) error {
		hdr, err = p.bestHeader(ctx, m.log)
		return err
	}, allRPCErrorsAreFails)
}

func (m *multiRPCClient) headerByHash(ctx context.Context, h common.Hash) (hdr *types.Header, err error) {
	return hdr, m.withAny(func(p *provider) error {
		hdr, err = p.headerByHash(ctx, h)
		return err
	})
}

func (m *multiRPCClient) chainConfig() *params.ChainConfig {
	return m.cfg
}

func (m *multiRPCClient) peerCount() (c uint32) {
	m.providerMtx.Lock()
	defer m.providerMtx.Unlock()
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

func (m *multiRPCClient) pendingTransactions() ([]*types.Transaction, error) {
	return []*types.Transaction{}, nil
}

func (m *multiRPCClient) shutdown() {
	for _, p := range m.providerList() {
		p.ec.Close()
	}

}

func (m *multiRPCClient) sendSignedTransaction(ctx context.Context, tx *types.Transaction) error {
	var lastProvider *provider
	if err := m.withPreferred(func(p *provider) error {
		lastProvider = p
		m.log.Tracef("Sending signed tx via %q", p.host)
		return p.ec.SendTransaction(ctx, tx)
	}, func(err error) (discard, propagate, fail bool) {
		return errorFilter(err, core.ErrAlreadyKnown, "known transaction"), false, false
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

// syncProgress: We're going to lie and just always say we're synced if we
// can get a header.
func (m *multiRPCClient) syncProgress(ctx context.Context) (prog *ethereum.SyncProgress, err error) {
	return prog, m.withAny(func(p *provider) error {
		tip, err := p.bestHeader(ctx, m.log)
		if err != nil {
			return err
		}

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
	var notFound bool
	if err := m.withPreferred(func(p *provider) error {
		r, err = p.ec.TransactionReceipt(ctx, txHash)
		if err != nil {
			if strings.Contains(err.Error(), "not found") {
				notFound = true
			}
			return err
		}
		tip, err = p.bestHeader(ctx, m.log)
		return err
	}); err != nil {
		if notFound {
			return 0, asset.CoinNotFoundError
		}
		return 0, err
	}
	if r.BlockNumber != nil && tip.Number != nil {
		bigConfs := new(big.Int).Sub(tip.Number, r.BlockNumber)
		bigConfs.Add(bigConfs, big.NewInt(1))
		if bigConfs.IsInt64() {
			return uint32(bigConfs.Int64()), nil
		}
	}
	return 0, nil
}

func (m *multiRPCClient) txOpts(ctx context.Context, val, maxGas uint64, maxFeeRate *big.Int) (*bind.TransactOpts, error) {
	baseFees, gasTipCap, err := m.currentFees(ctx)
	if err != nil {
		return nil, err
	}

	if maxFeeRate == nil {
		maxFeeRate = new(big.Int).Mul(baseFees, big.NewInt(2))
	}

	txOpts := newTxOpts(ctx, m.creds.addr, val, maxGas, maxFeeRate, gasTipCap)

	nonce, err := m.nextNonce(ctx)
	if err != nil {
		return nil, fmt.Errorf("error getting nonce: %v", err)
	}
	txOpts.Nonce = new(big.Int).SetUint64(nonce)

	txOpts.Signer = func(addr common.Address, tx *types.Transaction) (*types.Transaction, error) {
		return m.creds.wallet.SignTx(*m.creds.acct, tx, m.chainID)
	}

	return txOpts, nil

}

func (m *multiRPCClient) currentFees(ctx context.Context) (baseFees, tipCap *big.Int, err error) {
	return baseFees, tipCap, m.withAny(func(p *provider) error {
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
	return code, m.withAny(func(p *provider) error {
		code, err = p.ec.CodeAt(ctx, contract, blockNumber)
		return err
	})
}

func (m *multiRPCClient) CallContract(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) (res []byte, err error) {
	return res, m.withPreferred(func(p *provider) error {
		res, err = p.ec.CallContract(ctx, call, blockNumber)
		return err
	})
}

func (m *multiRPCClient) HeaderByNumber(ctx context.Context, number *big.Int) (hdr *types.Header, err error) {
	return hdr, m.withAny(func(p *provider) error {
		hdr, err = p.ec.HeaderByNumber(ctx, number)
		return err
	})
}

func (m *multiRPCClient) PendingCodeAt(ctx context.Context, account common.Address) (code []byte, err error) {
	return code, m.withAny(func(p *provider) error {
		code, err = p.ec.PendingCodeAt(ctx, account)
		return err
	})
}

func (m *multiRPCClient) PendingNonceAt(ctx context.Context, account common.Address) (nonce uint64, err error) {
	return nonce, m.withPreferred(func(p *provider) error {
		nonce, err = p.ec.PendingNonceAt(ctx, account)
		return err
	})
}

func (m *multiRPCClient) SuggestGasPrice(ctx context.Context) (price *big.Int, err error) {
	return price, m.withAny(func(p *provider) error {
		price, err = p.ec.SuggestGasPrice(ctx)
		return err
	})
}

func (m *multiRPCClient) SuggestGasTipCap(ctx context.Context) (tipCap *big.Int, err error) {
	return tipCap, m.withAny(func(p *provider) error {
		tipCap = p.suggestTipCap(ctx, m.log)
		return nil
	})
}

func (m *multiRPCClient) EstimateGas(ctx context.Context, call ethereum.CallMsg) (gas uint64, err error) {
	return gas, m.withAny(func(p *provider) error {
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
	return logs, m.withAny(func(p *provider) error {
		logs, err = p.ec.FilterLogs(ctx, query)
		return err
	})
}

func (m *multiRPCClient) SubscribeFilterLogs(ctx context.Context, query ethereum.FilterQuery, ch chan<- types.Log) (sub ethereum.Subscription, err error) {
	return sub, m.withAny(func(p *provider) error {
		sub, err = p.ec.SubscribeFilterLogs(ctx, query, ch)
		return err
	})
}

var compliantProviders = []string{
	"linkpool.io",
	"mewapi.io",
	"flashbots.net",
	"mycryptoapi.com",
	"runonflux.io",
	"infura.io",
	"rivet.cloud",
	"alchemy.com",
}

var nonCompliantProviders = []string{
	"cloudflare-eth.com", // "SuggestGasTipCap" error: Method not found
	"ankr.com",           // "SyncProgress" error: the method eth_syncing does not exist/is not available
}

func providerIsCompliant(addr string) (known, compliant bool) {
	addr = domain(addr)
	for _, host := range compliantProviders {
		if addr == host {
			return true, true
		}
	}
	for _, host := range nonCompliantProviders {
		if addr == host {
			return true, false
		}
	}
	return false, false
}

type rpcTest struct {
	name string
	f    func(*provider) error
}

// newCompatibilityTests returns a list of RPC tests to run to determine API
// compatibility.
func newCompatibilityTests(ctx context.Context, cb bind.ContractBackend, log slog.Logger) []*rpcTest {
	var (
		// Vitalik's address from https://twitter.com/VitalikButerin/status/1050126908589887488
		mainnetAddr   = common.HexToAddress("0xab5801a7d398351b8be11c439e05c5b3259aec9b")
		mainnetUSDC   = common.HexToAddress("0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48")
		mainnetTxHash = common.HexToHash("0xea1a717af9fad5702f189d6f760bb9a5d6861b4ee915976fe7732c0c95cd8a0e")
	)
	if log == nil { // Logging detail is for testing.
		log = slog.Disabled
	}
	return []*rpcTest{
		{
			name: "HeaderByNumber",
			f: func(p *provider) error {
				_, err := p.ec.HeaderByNumber(ctx, nil /* latest */)
				return err
			},
		},
		{
			name: "TransactionReceipt",
			f: func(p *provider) error {
				_, err := p.ec.TransactionReceipt(ctx, mainnetTxHash)
				return err
			},
		},
		{
			name: "PendingNonceAt",
			f: func(p *provider) error {
				_, err := p.ec.PendingNonceAt(ctx, mainnetAddr)
				return err
			},
		},
		{
			name: "SuggestGasTipCap",
			f: func(p *provider) error {
				tipCap, err := p.ec.SuggestGasTipCap(ctx)
				if err != nil {
					return err
				}
				log.Info("#### Retreived tip cap:", tipCap)
				return nil
			},
		},
		{
			name: "BalanceAt",
			f: func(p *provider) error {
				bal, err := p.ec.BalanceAt(ctx, mainnetAddr, nil)
				if err != nil {
					return err
				}
				log.Infof("#### Vitalik's balance retrieved: %.9f", float64(dexeth.WeiToGwei(bal))/1e9)
				return nil
			},
		},
		{
			name: "CodeAt",
			f: func(p *provider) error {
				code, err := p.ec.CodeAt(ctx, mainnetUSDC, nil)
				if err != nil {
					return err
				}
				log.Infof("#### %d bytes of USDC contract retrieved", len(code))
				return nil
			},
		},
		{
			name: "CallContract(balanceOf)",
			f: func(p *provider) error {
				caller, err := erc20.NewIERC20(mainnetUSDC, cb)
				if err != nil {
					return err
				}
				bal, err := caller.BalanceOf(&bind.CallOpts{
					From:    mainnetAddr,
					Context: ctx,
				}, mainnetAddr)
				if err != nil {
					return err
				}
				// I guess we would need to unpack the results. I don't really
				// know how to interpret these, but I'm really just looking for
				// a request error.
				log.Info("#### USDC balanceOf result:", bal, "wei")
				return nil
			},
		},
		{
			name: "ChainID",
			f: func(p *provider) error {
				chainID, err := p.ec.ChainID(ctx)
				if err != nil {
					return err
				}
				log.Infof("#### Chain ID: %d", chainID)
				return nil
			},
		},
		{
			name: "PendingNonceAt",
			f: func(p *provider) error {
				n, err := p.ec.PendingNonceAt(ctx, mainnetAddr)
				if err != nil {
					return err
				}
				log.Infof("#### Pending nonce: %d", n)
				return nil
			},
		},
		{
			name: "getRPCTransaction",
			f: func(p *provider) error {
				rpcTx, err := getRPCTransaction(ctx, p, mainnetTxHash)
				if err != nil {
					return err
				}
				var h string
				if rpcTx.BlockNumber != nil {
					h = *rpcTx.BlockNumber
				}
				log.Infof("#### RPC Tx is nil? %t, block number: %q", rpcTx.tx == nil, h)
				return nil
			},
		},
	}
}

func domain(host string) string {
	parts := strings.Split(host, ".")
	n := len(parts)
	if n <= 2 {
		return host
	}
	return parts[n-2] + "." + parts[n-1]
}

func checkProvidersCompliance(ctx context.Context, walletDir string, providers []*provider, log dex.Logger) error {
	var compliantProviders map[string]bool
	path := filepath.Join(walletDir, "compliant-providers.json")
	b, err := os.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(b, &compliantProviders); err != nil {
			log.Warn("Couldn't parse compliant providers file")
		}
	}
	if compliantProviders == nil {
		compliantProviders = make(map[string]bool)
	}

	n := len(compliantProviders)

	for _, p := range providers {
		if p.host == ipcHost || compliantProviders[domain(p.host)] {
			continue
		}

		if known, _ := providerIsCompliant(p.host); !known {
			// Need to run API tests on this endpoint.
			for _, t := range newCompatibilityTests(ctx, p.ec, nil /* logger is for testing only */) {
				if err := t.f(p); err != nil {
					log.Errorf("RPC Provider @ %q has a non-compliant API: %v", err)
					return fmt.Errorf("RPC Provider @ %q has a non-compliant API", p.host)
				}
			}
			compliantProviders[domain(p.host)] = true
		}
	}

	if len(compliantProviders) != n {
		b, _ /* c'mon */ := json.Marshal(compliantProviders)
		if err := os.WriteFile(path, b, 0644); err != nil {
			log.Errorf("Failed to write compliant providers file: %v", err)
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
