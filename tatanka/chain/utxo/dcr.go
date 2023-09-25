// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package utxo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/tatanka/chain"
	"decred.org/dcrdex/tatanka/tanka"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil/v4"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types/v4"
	"github.com/decred/dcrd/rpcclient/v8"
	"github.com/decred/dcrd/wire"
)

const (
	ChainID = 42
)

var (
	compatibleNodeRPCVersions = []dex.Semver{
		{Major: 8, Minor: 0, Patch: 0}, // 1.8-pre, just dropped unused ticket RPCs
		{Major: 7, Minor: 0, Patch: 0}, // 1.7 release, new gettxout args
	}
)

func init() {
	chain.RegisterChainConstructor(42, NewDecred)
}

type DecredConfigFile struct {
	RPCUser   string `json:"rpcuser"`
	RPCPass   string `json:"rpcpass"`
	RPCListen string `json:"rpclisten"`
	RPCCert   string `json:"rpccert"`
	NodeRelay string `json:"nodeRelay"`
}

type decredChain struct {
	cfg  *DecredConfigFile
	net  dex.Network
	log  dex.Logger
	fees chan uint64

	cl        *rpcclient.Client
	connected atomic.Bool
}

func NewDecred(rawConfig json.RawMessage, log dex.Logger, net dex.Network) (chain.Chain, error) {
	var cfg DecredConfigFile
	if err := json.Unmarshal(rawConfig, &cfg); err != nil {
		return nil, fmt.Errorf("error parsing configuration: %w", err)
	}
	return &decredChain{
		cfg:  &cfg,
		net:  net,
		log:  log,
		fees: make(chan uint64, 1),
	}, nil
}

func (c *decredChain) Connect(ctx context.Context) (_ *sync.WaitGroup, err error) {
	cfg := c.cfg
	if cfg.NodeRelay == "" {
		c.cl, err = connectNodeRPC(cfg.RPCListen, cfg.RPCUser, cfg.RPCPass, cfg.RPCCert)
	} else {
		c.cl, err = connectLocalHTTP(cfg.NodeRelay, cfg.RPCUser, cfg.RPCPass)
	}
	if err != nil {
		return nil, fmt.Errorf("error connecting RPC client: %w", err)
	}

	if err = c.initialize(ctx); err != nil {
		return nil, err
	}

	c.connected.Store(true)
	defer c.connected.Store(false)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		c.monitorFees(ctx)
	}()

	return &wg, nil
}

func (c *decredChain) initialize(ctx context.Context) error {
	net, err := c.cl.GetCurrentNet(ctx)
	if err != nil {
		return fmt.Errorf("getcurrentnet failure: %w", err)
	}
	var wantCurrencyNet wire.CurrencyNet
	switch c.net {
	case dex.Testnet:
		wantCurrencyNet = wire.TestNet3
	case dex.Mainnet:
		wantCurrencyNet = wire.MainNet
	case dex.Regtest: // dex.Simnet
		wantCurrencyNet = wire.SimNet
	}
	if net != wantCurrencyNet {
		return fmt.Errorf("wrong net %v", net.String())
	}

	// Check the required API versions.
	versions, err := c.cl.Version(ctx)
	if err != nil {
		return fmt.Errorf("DCR node version fetch error: %w", err)
	}

	ver, exists := versions["dcrdjsonrpcapi"]
	if !exists {
		return fmt.Errorf("dcrd.Version response missing 'dcrdjsonrpcapi'")
	}
	nodeSemver := dex.NewSemver(ver.Major, ver.Minor, ver.Patch)
	if !dex.SemverCompatibleAny(compatibleNodeRPCVersions, nodeSemver) {
		return fmt.Errorf("dcrd has an incompatible JSON-RPC version %s, require one of %s",
			nodeSemver, compatibleNodeRPCVersions)
	}

	// Verify dcrd has tx index enabled (required for getrawtransaction).
	info, err := c.cl.GetInfo(ctx)
	if err != nil {
		return fmt.Errorf("dcrd getinfo check failed: %w", err)
	}
	if !info.TxIndex {
		return errors.New("dcrd does not have transaction index enabled (specify --txindex)")
	}

	// Prime the cache with the best block.
	tip, err := c.cl.GetBestBlockHash(ctx)
	if err != nil {
		return fmt.Errorf("error getting best block from dcrd: %w", err)
	}
	if tip == nil {
		return fmt.Errorf("nil best block hash?")
	}
	// if bestHash != nil {
	// 	_, err := dcr.getDcrBlock(ctx, bestHash)
	// 	if err != nil {
	// 		return nil, fmt.Errorf("error priming the cache: %w", err)
	// 	}
	// }

	// if _, err = c.cl.FeeRate(ctx); err != nil {
	// 	c.log.Warnf("Decred backend started without fee estimation available: %v", err)
	// }
	return nil
}

type query struct {
	Method string            `json:"method"`
	Args   []json.RawMessage `json:"args"`
}

// func (c *decredChain) Query(ctx context.Context, rawQuery chain.Query) (chain.Result, error) {
// 	var q query
// 	if err := json.Unmarshal(rawQuery, &q); err != nil {
// 		return nil, chain.BadQueryError(fmt.Errorf("error parsing raw query: %w", err))
// 	}

// 	if q.Method == "" {
// 		return nil, chain.BadQueryError(errors.New("invalid query parameters. no method"))
// 	}

// 	res, err := c.cl.RawRequest(ctx, q.Method, q.Args)
// 	if err != nil {
// 		// Could potentially try to parse certain errors here

// 		return nil, fmt.Errorf("error performing query: %w", err)
// 	}

// 	return chain.Result(res), nil
// }

func (c *decredChain) Connected() bool {
	return c.connected.Load()
}

func (c *decredChain) FeeChannel() <-chan uint64 {
	return c.fees
}

func (c *decredChain) monitorFees(ctx context.Context) {
	tick := time.NewTicker(feeMonitorTick)
	var tip *chainhash.Hash
	for {
		select {
		case <-tick.C:
		case <-ctx.Done():
			return
		}

		newTip, err := c.cl.GetBestBlockHash(ctx)
		if err != nil {
			c.connected.Store(false)
			c.log.Errorf("Decred is not connected: %w", err)
			continue
		}
		if newTip == nil { // sanity check
			c.log.Error("nil tip hash?")
			continue
		}
		if tip != nil && *tip == *newTip {
			continue
		}
		c.connected.Store(true)
		// estimatesmartfee 1 returns extremely high rates on DCR.
		estimateFeeResult, err := c.cl.EstimateSmartFee(ctx, 2, chainjson.EstimateSmartFeeConservative)
		if err != nil {
			c.log.Errorf("estimatesmartfee error: %w", err)
			continue
		}
		atomsPerKB, err := dcrutil.NewAmount(estimateFeeResult.FeeRate)
		if err != nil {
			c.log.Errorf("NewAmount error: %w", err)
			continue
		}
		atomsPerB := uint64(math.Round(float64(atomsPerKB) / 1000))
		if atomsPerB == 0 {
			atomsPerB = 1
		}
		select {
		case c.fees <- atomsPerB:
		case <-time.After(time.Second * 5):
			c.log.Errorf("fee channel is blocking")
		}

	}

}

func (c *decredChain) CheckBond(b *tanka.Bond) error {

	// TODO: Validate bond

	return nil
}

func (c *decredChain) AuditHTLC(*tanka.HTLCAudit) (bool, error) {

	// TODO: Perform the audit

	return true, nil
}

// connectNodeRPC attempts to create a new websocket connection to a dcrd node
// with the given credentials and notification handlers.
func connectNodeRPC(host, user, pass, cert string) (*rpcclient.Client, error) {
	dcrdCerts, err := os.ReadFile(cert)
	if err != nil {
		return nil, fmt.Errorf("TLS certificate read error: %w", err)
	}

	config := &rpcclient.ConnConfig{
		Host:         host,
		Endpoint:     "ws", // websocket
		User:         user,
		Pass:         pass,
		Certificates: dcrdCerts,
	}

	dcrdClient, err := rpcclient.New(config, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to start dcrd RPC client: %w", err)
	}

	return dcrdClient, nil
}

func connectLocalHTTP(host, user, pass string) (*rpcclient.Client, error) {
	config := &rpcclient.ConnConfig{
		Host:         host,
		HTTPPostMode: true,
		DisableTLS:   true,
		User:         user,
		Pass:         pass,
	}

	dcrdClient, err := rpcclient.New(config, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to start dcrd RPC client: %w", err)
	}

	return dcrdClient, nil
}
