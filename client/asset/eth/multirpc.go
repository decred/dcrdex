// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build lgpl

// https://ethereumnodes.com/ for RPC providers

package eth

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"math/rand"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/dex"
	dexeth "decred.org/dcrdex/dex/networks/eth"
	"decred.org/dcrdex/server/asset"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/misc"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rpc"
)

const (
	failQuarantine         = time.Minute
	receiptCacheExpiration = time.Hour
)

var nonceProviderStickiness = time.Minute

// TODO: Handle rate limiting. From the docs:
// When you are rate limited, your JSON-RPC responses have HTTP Status code 429.

type combinedRPCClient struct {
	*ethclient.Client
	rpc *rpc.Client
}

type provider struct {
	host    string
	isLocal bool
	ec      atomic.Value // *combinedRPCClient
	ws      bool

	tip struct {
		sync.RWMutex
		header      *types.Header
		headerStamp time.Time
		failStamp   time.Time
		failCount   int
	}
}

func (p *provider) cl() *combinedRPCClient {
	return p.ec.Load().(*combinedRPCClient)
}

func (p *provider) setTip(header *types.Header) {
	p.tip.Lock()
	p.tip.header = header
	p.tip.headerStamp = time.Now()
	p.tip.failStamp = time.Time{}
	p.tip.failCount = 0
	p.tip.Unlock()
}

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

func (p *provider) setFailed() {
	p.tip.Lock()
	p.tip.failStamp = time.Now()
	p.tip.failCount++
	p.tip.Unlock()
}

func (p *provider) failed() bool {
	p.tip.Lock()
	defer p.tip.Unlock()
	return time.Since(p.tip.failStamp) < failQuarantine || p.tip.failCount > 100
}

func (p *provider) bestHeader(ctx context.Context, log dex.Logger) (*types.Header, error) {
	// Check if we have a cached header.
	if tip := p.cachedTip(); tip != nil {
		log.Tracef("using cached header from %q", p.host)
		return tip, nil
	}

	log.Tracef("fetching fresh header from %q", p.host)
	hdr, err := p.cl().HeaderByNumber(ctx, nil /* latest */)
	if err != nil {
		p.setFailed()
		return nil, fmt.Errorf("HeaderByNumber error: %w", err)
	}
	p.setTip(hdr)
	return hdr, nil
}

func (p *provider) headerByHash(ctx context.Context, h common.Hash) (*types.Header, error) {
	hdr, err := p.cl().HeaderByHash(ctx, h)
	if err != nil {
		p.setFailed()
		return nil, fmt.Errorf("HeaderByHash error: %w", err)
	}
	p.setTip(hdr)
	return hdr, nil
}

type receiptRecord struct {
	r          *types.Receipt
	lastAccess time.Time
}

// multiRPCClient is an ethFetcher backed by one or more public infrastructure
// providers.
// MATIC providers at
//
//	https://docs.polygon.technology/docs/develop/network-details/network/
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

func connectProviders(ctx context.Context, endpoints []string, addr common.Address, log dex.Logger) ([]*provider, error) {
	providers := make([]*provider, 0, len(endpoints))
	var success bool
	for _, endpoint := range endpoints {
		// First try to get a websocket connection.
		var ec *ethclient.Client
		var rpcClient *rpc.Client
		var sub ethereum.Subscription
		var h chan *types.Header
		host := "IPC"
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
				wsURL.Path = "/ws" + wsURL.Path
			case "http":
				wsURL.Scheme = "ws"
				wsURL.Path = "/ws" + wsURL.Path
			case "ws", "wss":
			default:
				return nil, fmt.Errorf("unknown scheme for endpoint %q: %q", endpoint, wsURL.Scheme)
			}
			if strings.HasPrefix(wsURL.Path, "/v3") {
				wsURL.Path = "/ws" + wsURL.Path
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

		// Get best header
		hdr, err := ec.HeaderByNumber(ctx, nil /* latest */)
		if err != nil {
			ec.Close()
			log.Errorf("Failed to get best header from %q", endpoint)
		}

		lower := strings.ToLower(endpoint)
		p := &provider{
			host:    host,
			ws:      sub != nil,
			isLocal: strings.HasSuffix(lower, ".ipc") || strings.HasPrefix(lower, "http") || strings.HasPrefix(lower, "ws"),
		}
		p.ec.Store(&combinedRPCClient{
			Client: ec,
			rpc:    rpcClient,
		})
		p.setTip(hdr)
		providers = append(providers, p)

		// Start websocket listen loop.
		if sub != nil {
			go subHeaders(ctx, p, sub, h, addr, log)
		}
	}

	if len(providers) != len(endpoints) {
		if len(providers) == 0 {
			return nil, fmt.Errorf("failed to connect")
		}
		log.Warnf("Only connected with %d of %d RPC servers", len(providers), len(endpoints))
	}

	success = true
	return providers, nil
}

func (m *multiRPCClient) connect(ctx context.Context) (err error) {
	providers, err := connectProviders(ctx, m.endpoints, m.creds.addr, m.log)
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
			p.cl().Close()
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
	providers, err := connectProviders(ctx, endpoints, m.creds.addr, m.log)
	if err != nil {
		return err
	}
	m.providerMtx.Lock()
	oldProviders := m.providers
	m.providers = providers
	m.endpoints = endpoints
	m.providerMtx.Unlock()
	for _, p := range oldProviders {
		p.cl().Close()
	}
	return nil
}

func subHeaders(ctx context.Context, p *provider, sub ethereum.Subscription, h chan *types.Header, addr common.Address, log dex.Logger) {
	defer sub.Unsubscribe()
	var lastWarning time.Time
	newSub := func() (ethereum.Subscription, error) {
		for {
			var err error
			sub, err = p.cl().SubscribeNewHead(ctx, h)
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

	// 	acctSub, err := p.cl().SubscribeFilterLogs(ctx, config, logs)
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
	log.Tracef("handling websocket subscriptions")

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

func (m *multiRPCClient) transactionReceipt(ctx context.Context, txHash common.Hash) (r *types.Receipt, tx *types.Transaction, err error) {
	// TODO
	// TODO: Plug into the monitoredTx system from #1638.
	// TODO
	if tx, _, err = m.getTransaction(ctx, txHash); err != nil {
		return nil, nil, err
	}

	// Check the cache.
	m.receipts.RLock()
	cached := m.receipts.cache[txHash]
	if cached != nil {
		cached.lastAccess = time.Now()
	}
	if time.Since(m.receipts.lastClean) > time.Minute*20 {
		m.receipts.lastClean = time.Now()
		go m.cleanReceipts()
	}
	m.receipts.RUnlock()
	if cached != nil {
		return cached.r, tx, nil
	}

	if err = m.withPreferred(func(p *provider) error {
		r, err = p.cl().TransactionReceipt(ctx, txHash)
		return err
	}); err != nil {
		return nil, nil, err
	}

	m.receipts.Lock()
	m.receipts.cache[txHash] = &receiptRecord{
		r:          r,
		lastAccess: time.Now(),
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
	tx          *types.Transaction
	BlockNumber *string         `json:"blockNumber,omitempty"`
	BlockHash   *common.Hash    `json:"blockHash,omitempty"`
	From        *common.Address `json:"from,omitempty"`
}

func (tx *rpcTransaction) UnmarshalJSON(b []byte) error {
	if err := json.Unmarshal(b, &tx.tx); err != nil {
		return err
	}
	return json.Unmarshal(b, tx)
}

func (m *multiRPCClient) getTransaction(ctx context.Context, txHash common.Hash) (tx *types.Transaction, h int64, err error) {
	var resp *rpcTransaction

	return tx, h, m.withPreferred(func(p *provider) error {
		err = p.cl().rpc.CallContext(ctx, &resp, "eth_getTransactionByHash", txHash)
		if err != nil {
			return err
		}
		if resp == nil {
			return asset.CoinNotFoundError
		}
		// Just copying geth with this one.
		if _, r, _ := resp.tx.RawSignatureValues(); r == nil {
			return fmt.Errorf("server returned transaction without signature")
		}

		tx = resp.tx
		if resp.BlockNumber != nil {
			b, _ := hex.DecodeString(*resp.BlockNumber)
			h = new(big.Int).SetBytes(b).Int64()
		}
		return nil
	})
}

func (m *multiRPCClient) getConfirmedNonce(ctx context.Context, blockNumber int64) (n uint64, err error) {
	return n, m.withPreferred(func(p *provider) error {
		n, err = p.cl().PendingNonceAt(ctx, m.address())
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

func (m *multiRPCClient) withOne(providers []*provider, f func(*provider) error) error {
	for _, p := range providers {
		if p.failed() {
			continue
		}
		if err := f(p); err != nil {
			m.log.Error(err)
			continue
		}
		return nil
	}
	return fmt.Errorf("all providers errored")
}

func shuffleProviders(p []*provider) {
	rand.Shuffle(len(p), func(i, j int) {
		p[i], p[j] = p[j], p[i]
	})
}

func (m *multiRPCClient) withAny(f func(*provider) error) error {
	providers := m.providerList()
	shuffleProviders(providers)
	return m.withOne(providers, f)
}

func (m *multiRPCClient) withPreferred(f func(*provider) error) error {
	return m.withOne(m.nonceProviderList(), f)
}

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

func (m *multiRPCClient) nextNonce(ctx context.Context) (nonce uint64, err error) {
	return nonce, m.withPreferred(func(p *provider) error {
		nonce, err = p.cl().PendingNonceAt(ctx, m.creds.addr)
		return err
	})
}

func (m *multiRPCClient) address() common.Address {
	return m.creds.addr
}

func (m *multiRPCClient) addressBalance(ctx context.Context, addr common.Address) (bal *big.Int, err error) {
	return bal, m.withAny(func(p *provider) error {
		bal, err = p.cl().BalanceAt(ctx, addr, nil /* latest */)
		return err
	})
}

func (m *multiRPCClient) bestHeader(ctx context.Context) (hdr *types.Header, err error) {
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
	})
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
		p.cl().Close()
	}

}

func (m *multiRPCClient) sendSignedTransaction(ctx context.Context, tx *types.Transaction) error {
	var lastProvider *provider
	if err := m.withPreferred(func(p *provider) error {
		lastProvider = p
		m.log.Tracef("Sending signed tx via %q", p.host)
		return p.cl().SendTransaction(ctx, tx)
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

func (m *multiRPCClient) syncProgress(ctx context.Context) (prog *ethereum.SyncProgress, err error) {
	return prog, m.withAny(func(p *provider) error {
		s, err := p.cl().SyncProgress(ctx)
		if err != nil {
			return fmt.Errorf("error getting sync progress from %s: %v", p.host, err)
		}
		if s != nil {
			prog = s
			return nil
		}

		// SyncProgress will return nil both before syncing has begun and after
		// it has finished. In order to discern when syncing has begun, check
		// that the best header came in under MaxBlockInterval.
		bh, err := p.cl().HeaderByNumber(ctx, nil /* latest */)
		if err != nil {
			return fmt.Errorf("error getting header for nil SyncProgress resolution: %v", err)
		}
		// Time in the header is in seconds.
		nowInSecs := time.Now().Unix() / 1000
		timeDiff := nowInSecs - int64(bh.Time)

		if timeDiff < dexeth.MaxBlockInterval {
			// Consider this synced.
			prog = &ethereum.SyncProgress{
				CurrentBlock: bh.Number.Uint64(),
				HighestBlock: bh.Number.Uint64(),
			}
			return nil
		}
		// Not synced.
		prog = &ethereum.SyncProgress{}
		return nil
	})
}

func (m *multiRPCClient) transactionConfirmations(ctx context.Context, txHash common.Hash) (confs uint32, err error) {
	var r *types.Receipt
	var tip *types.Header
	var notFound bool
	if err := m.withPreferred(func(p *provider) error {
		r, err = p.cl().TransactionReceipt(ctx, txHash)
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

		if p.isLocal {
			tipCap, err = p.cl().SuggestGasTipCap(ctx)
			if err != nil {
				return err
			}
		} else {
			tipCap = dexeth.GweiToWei(2)
		}

		minGasTipCapWei := dexeth.GweiToWei(dexeth.MinGasTipCap)
		if tipCap.Cmp(minGasTipCapWei) < 0 {
			tipCap = new(big.Int).Set(minGasTipCapWei)
		}

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
		code, err = p.cl().CodeAt(ctx, contract, blockNumber)
		return err
	})
}

func (m *multiRPCClient) CallContract(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) (res []byte, err error) {
	return res, m.withPreferred(func(p *provider) error {
		res, err = p.cl().CallContract(ctx, call, blockNumber)
		return err
	})
}

func (m *multiRPCClient) HeaderByNumber(ctx context.Context, number *big.Int) (hdr *types.Header, err error) {
	return hdr, m.withAny(func(p *provider) error {
		hdr, err = p.cl().HeaderByNumber(ctx, number)
		return err
	})
}

func (m *multiRPCClient) PendingCodeAt(ctx context.Context, account common.Address) (code []byte, err error) {
	return code, m.withAny(func(p *provider) error {
		code, err = p.cl().PendingCodeAt(ctx, account)
		return err
	})
}

func (m *multiRPCClient) PendingNonceAt(ctx context.Context, account common.Address) (nonce uint64, err error) {
	return nonce, m.withPreferred(func(p *provider) error {
		nonce, err = p.cl().PendingNonceAt(ctx, account)
		return err
	})
}

func (m *multiRPCClient) SuggestGasPrice(ctx context.Context) (price *big.Int, err error) {
	return price, m.withAny(func(p *provider) error {
		price, err = p.cl().SuggestGasPrice(ctx)
		return err
	})
}

func (m *multiRPCClient) SuggestGasTipCap(ctx context.Context) (tipCap *big.Int, err error) {
	return tipCap, m.withAny(func(p *provider) error {
		if p.isLocal {
			tipCap, err = p.cl().SuggestGasTipCap(ctx)
			if err != nil {
				return err
			}
		} else {
			// Probably don't have eth_maxPriorityFeePerGas.
			tipCap = dexeth.GweiToWei(2)
		}
		return err
	})
}

func (m *multiRPCClient) EstimateGas(ctx context.Context, call ethereum.CallMsg) (gas uint64, err error) {
	return gas, m.withAny(func(p *provider) error {
		gas, err = p.cl().EstimateGas(ctx, call)
		return err
	})
}

func (m *multiRPCClient) SendTransaction(ctx context.Context, tx *types.Transaction) error {
	return m.sendSignedTransaction(ctx, tx)
}

func (m *multiRPCClient) FilterLogs(ctx context.Context, query ethereum.FilterQuery) (logs []types.Log, err error) {
	return logs, m.withAny(func(p *provider) error {
		logs, err = p.cl().FilterLogs(ctx, query)
		return err
	})
}

func (m *multiRPCClient) SubscribeFilterLogs(ctx context.Context, query ethereum.FilterQuery, ch chan<- types.Log) (sub ethereum.Subscription, err error) {
	return sub, m.withAny(func(p *provider) error {
		sub, err = p.cl().SubscribeFilterLogs(ctx, query, ch)
		return err
	})
}
