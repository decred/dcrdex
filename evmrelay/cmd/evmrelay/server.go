// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"math"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"decred.org/dcrdex/dex"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

const (
	// taskPruneInterval is how often terminal tasks are pruned from memory and disk.
	taskPruneInterval = time.Hour
	// taskProcessInterval is how often each chain worker wakes up to reconcile tasks.
	taskProcessInterval = 5 * time.Second
	// taskProcessTimeout bounds one chain worker pass so a stuck RPC call cannot hang it forever.
	taskProcessTimeout = 30 * time.Second
	// taskRebroadcastPeriod is the minimum delay before rebroadcasting the current active tx.
	taskRebroadcastPeriod = 30 * time.Second
	// taskReplacePeriod is the minimum delay before trying a higher-fee same-nonce replacement tx.
	taskReplacePeriod = 2 * time.Minute
	// taskRetentionPeriod is how long resolved tasks are kept before pruning.
	taskRetentionPeriod = 48 * time.Hour
	// taskShutdownTimeout bounds graceful HTTP server shutdown.
	taskShutdownTimeout = 5 * time.Second
	// taskDeadlineMargin is subtracted from the signed deadline to give the relay time to stop retrying.
	taskDeadlineMargin = 2 * time.Minute
	// taskMaxPendingLifetime caps how long a queued task may remain eligible for relay processing.
	taskMaxPendingLifetime = 30 * time.Minute
	// taskMaxReplaceAttempts limits how many same-nonce replacements are attempted for redeem or cancel txs.
	taskMaxReplaceAttempts = 3
	// rateLimitPruneAge is how long idle IP limiter entries are retained.
	rateLimitPruneAge = time.Hour
	// maxQueuedTasksPerChain limits how many queued tasks a single chain can
	// accumulate before the relay starts rejecting new submissions.
	maxQueuedTasksPerChain = 100
)

// relayConfig is the JSON configuration for the relay server.
type relayConfig struct {
	Addr           string        `json:"addr"`
	PrivKey        string        `json:"privkey"`
	LogLevel       string        `json:"logLevel"`
	ProfitPerGas   *float64      `json:"profitPerGas"` // in gwei; nil defaults to 1.5
	DataDir        string        `json:"dataDir"`
	TrustedProxies []string      `json:"trustedProxies"` // IPs whose X-Real-IP / X-Forwarded-For headers are honored
	Chains         []chainConfig `json:"chains"`
	Net            dex.Network   `json:"-"` // set from CLI flags
}

type chainConfig struct {
	ChainID int64  `json:"chainID"`
	RPCURL  string `json:"rpcURL"`
}

// pendingKey identifies an in-flight redemption by the participant's address
// and their contract nonce.
type pendingKey struct {
	participant common.Address
	nonce       uint64
}

type pendingReservation struct {
	taskID   string
	reserved bool
}

func reservePendingTask() pendingReservation {
	return pendingReservation{reserved: true}
}

func pendingReservationForTask(taskID string) pendingReservation {
	return pendingReservation{taskID: taskID}
}

func (r pendingReservation) hasTask() bool {
	return r.taskID != ""
}

// relayServer is the HTTP relay server.
type relayServer struct {
	httpServer     *http.Server
	chains         map[int64]*chainClient
	relayAddr      common.Address
	limiter        *ipLimiter
	store          *taskStore
	trustedProxies map[string]struct{}
}

type middleware func(http.HandlerFunc) http.HandlerFunc

func composeMiddleware(h http.HandlerFunc, mws ...middleware) http.HandlerFunc {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

func newRelayServer(ctx context.Context, cfg *relayConfig) (*relayServer, error) {
	privKeyBytes, err := hex.DecodeString(strings.TrimPrefix(cfg.PrivKey, "0x"))
	if err != nil {
		return nil, fmt.Errorf("error decoding private key: %w", err)
	}

	privKey, err := crypto.ToECDSA(privKeyBytes)
	if err != nil {
		return nil, fmt.Errorf("error parsing private key: %w", err)
	}

	relayAddr := crypto.PubkeyToAddress(privKey.PublicKey)
	log.Infof("Relay address: %s", relayAddr.Hex())

	// Default 1.5 gwei profit per gas.
	profitGwei := 1.5
	if cfg.ProfitPerGas != nil {
		if *cfg.ProfitPerGas < 0 {
			return nil, fmt.Errorf("profitPerGas must be >= 0, got %v", *cfg.ProfitPerGas)
		}
		profitGwei = *cfg.ProfitPerGas
	}
	profitPerGas := new(big.Int).SetUint64(uint64(math.Round(profitGwei * 1e9)))

	allTargets := buildAllowedTargets(cfg.Net)

	chains := make(map[int64]*chainClient, len(cfg.Chains))
	for _, cc := range cfg.Chains {
		signingChainID := cc.ChainID
		if cfg.Net == dex.Simnet {
			signingChainID = 1337
		}
		client, err := newChainClient(ctx, cc.ChainID, signingChainID, cc.RPCURL, privKey, profitPerGas, allTargets[cc.ChainID])
		if err != nil {
			return nil, fmt.Errorf("error initializing chain %d: %w", cc.ChainID, err)
		}
		chains[cc.ChainID] = client
		log.Infof("Chain %d initialized (signingChainID=%d, opStack=%v)", cc.ChainID, signingChainID, client.isOpStack)
	}

	if len(cfg.Chains) == 0 {
		return nil, fmt.Errorf("no chains configured")
	}

	dataDir := cfg.DataDir
	if dataDir == "" {
		home, _ := os.UserHomeDir()
		dataDir = filepath.Join(home, ".evmrelay")
	}
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, fmt.Errorf("error creating data directory: %w", err)
	}

	trustedProxies := make(map[string]struct{}, len(cfg.TrustedProxies))
	for _, ip := range cfg.TrustedProxies {
		if net.ParseIP(ip) == nil {
			return nil, fmt.Errorf("invalid trusted proxy IP %q", ip)
		}
		trustedProxies[ip] = struct{}{}
	}
	if len(trustedProxies) > 0 {
		log.Infof("Trusted proxies: %v", cfg.TrustedProxies)
	}

	srv := &relayServer{
		chains:         chains,
		relayAddr:      relayAddr,
		limiter:        newIPLimiter(),
		store:          newTaskStore(dataDir),
		trustedProxies: trustedProxies,
	}

	// Load tasks from disk.
	if err := srv.store.load(); err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", composeMiddleware(srv.handleHealth, srv.logHandler))
	mux.HandleFunc("POST /api/estimatefee", composeMiddleware(srv.handleEstimateFee, srv.logHandler, srv.rateLimitHandler))
	mux.HandleFunc("POST /api/relay", composeMiddleware(srv.handleRelay, srv.logHandler, srv.rateLimitHandler))
	mux.HandleFunc("GET /api/relay/{taskID}", composeMiddleware(srv.handleRelayStatus, srv.logHandler))

	srv.httpServer = &http.Server{
		Addr:         cfg.Addr,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	return srv, nil
}

func (s *relayServer) run(ctx context.Context) error {
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		pruneTicker := time.NewTicker(taskPruneInterval)
		defer pruneTicker.Stop()

		for {
			select {
			case <-pruneTicker.C:
				s.pruneTasks()
			case <-ctx.Done():
				return
			}
		}
	}()

	for chainID := range s.chains {
		wg.Add(1)
		go func(chainID int64) {
			defer wg.Done()
			processTicker := time.NewTicker(taskProcessInterval)
			defer processTicker.Stop()

			for {
				select {
				case <-processTicker.C:
					if err := s.processChainTasks(ctx, chainID); err != nil {
						log.Errorf("Chain %d task processing error: %v", chainID, err)
					}
				case <-ctx.Done():
					return
				}
			}
		}(chainID)
	}

	// Start HTTP server.
	errCh := make(chan error, 1)
	go func() {
		log.Infof("Relay server listening on %s", s.httpServer.Addr)
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	// Graceful shutdown.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), taskShutdownTimeout)
	defer cancel()
	s.httpServer.Shutdown(shutdownCtx)

	wg.Wait()

	for chainID, cc := range s.chains {
		cc.close()
		log.Debugf("Chain %d connection closed", chainID)
	}

	if err := s.store.save(); err != nil {
		log.Errorf("Error saving tasks on shutdown: %v", err)
		return fmt.Errorf("error saving tasks on shutdown: %w", err)
	}
	log.Info("Relay server shut down.")

	return nil
}

// getChain returns the chain client for the given chain ID.
func (s *relayServer) getChain(chainID int64) (*chainClient, error) {
	cc, ok := s.chains[chainID]
	if !ok {
		return nil, fmt.Errorf("chain %d not configured", chainID)
	}
	return cc, nil
}
