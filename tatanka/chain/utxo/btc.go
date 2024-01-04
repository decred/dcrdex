// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package utxo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/dex"
	dexbtc "decred.org/dcrdex/dex/networks/btc"
	"decred.org/dcrdex/tatanka/chain"
	"decred.org/dcrdex/tatanka/tanka"
	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types/v4"
	"github.com/decred/dcrd/rpcclient/v8"
)

const (
	BitcoinID      = 0
	feeMonitorTick = time.Second * 10
)

func init() {
	chain.RegisterChainConstructor(0, NewBitcoin)
}

type BitcoinConfigFile struct {
	dexbtc.RPCConfig
	NodeRelay string `json:"nodeRelay"`
}

type bitcoinChain struct {
	cfg  *BitcoinConfigFile
	net  dex.Network
	log  dex.Logger
	fees chan uint64
	name string

	cl        *rpcclient.Client
	connected atomic.Bool
}

func NewBitcoin(rawConfig json.RawMessage, log dex.Logger, net dex.Network) (chain.Chain, error) {
	var cfg BitcoinConfigFile
	if err := json.Unmarshal(rawConfig, &cfg); err != nil {
		return nil, fmt.Errorf("error parsing configuration: %w", err)
	}
	if cfg.NodeRelay != "" {
		cfg.RPCBind = cfg.NodeRelay
	}

	if err := dexbtc.CheckRPCConfig(&cfg.RPCConfig, "Bitcoin", net, dexbtc.RPCPorts); err != nil {
		return nil, fmt.Errorf("error validating RPC configuration: %v", err)
	}

	return &bitcoinChain{
		cfg:  &cfg,
		net:  net,
		log:  log,
		name: "Bitcoin",
		fees: make(chan uint64, 1),
	}, nil
}

func (c *bitcoinChain) Connect(ctx context.Context) (_ *sync.WaitGroup, err error) {
	cfg := c.cfg
	host := cfg.RPCBind
	if cfg.NodeRelay != "" {
		host = cfg.NodeRelay
	}
	c.cl, err = connectLocalHTTP(host, cfg.RPCUser, cfg.RPCPass)
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

func (c *bitcoinChain) initialize(ctx context.Context) error {
	// TODO: Check min version

	tip, err := c.getBestBlockHash(ctx)
	if err != nil {
		return fmt.Errorf("error getting best block from rpc: %w", err)
	}
	if tip == nil {
		return fmt.Errorf("nil best block hash?")
	}

	txindex, err := c.checkTxIndex(ctx)
	if err != nil {
		c.log.Warnf(`Please ensure txindex is enabled in the node config and you might need to re-index if txindex was not previously enabled for %s`, c.name)
		return fmt.Errorf("error checking txindex for %s: %w", c.name, err)
	}
	if !txindex {
		return fmt.Errorf("%s transaction index is not enabled. Please enable txindex in the node config and you might need to re-index when you enable txindex", c.name)
	}

	return nil
}

// func (c *bitcoinChain) Query(ctx context.Context, rawQuery chain.Query) (chain.Result, error) {
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

func (c *bitcoinChain) Connected() bool {
	return c.connected.Load()
}

func (c *bitcoinChain) FeeChannel() <-chan uint64 {
	return c.fees
}

func (c *bitcoinChain) monitorFees(ctx context.Context) {
	tick := time.NewTicker(feeMonitorTick)
	var tip *chainhash.Hash
	for {
		select {
		case <-tick.C:
		case <-ctx.Done():
			return
		}

		newTip, err := c.getBestBlockHash(ctx)
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
		atomsPerKB, err := btcutil.NewAmount(estimateFeeResult.FeeRate)
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

func (c *bitcoinChain) callHashGetter(ctx context.Context, method string, args []any) (*chainhash.Hash, error) {
	var txid string
	err := c.call(ctx, method, args, &txid)
	if err != nil {
		return nil, err
	}
	return chainhash.NewHashFromStr(txid)
}

// GetBestBlockHash returns the hash of the best block in the longest block
// chain.
func (c *bitcoinChain) getBestBlockHash(ctx context.Context) (*chainhash.Hash, error) {
	return c.callHashGetter(ctx, "getbestblockhash", nil)
}

// checkTxIndex checks if bitcoind transaction index is enabled.
func (c *bitcoinChain) checkTxIndex(ctx context.Context) (bool, error) {
	var res struct {
		TxIndex *struct{} `json:"txindex"`
	}
	err := c.call(ctx, "getindexinfo", []any{"txindex"}, &res)
	if err == nil {
		// Return early if there is no error. bitcoind returns an empty json
		// object if txindex is not enabled. It is safe to conclude txindex is
		// enabled if res.Txindex is not nil.
		return res.TxIndex != nil, nil
	}

	if !isMethodNotFoundErr(err) {
		return false, err
	}

	// Using block at index 5 to retrieve a coinbase transaction and ensure
	// txindex is enabled for pre 0.21 versions of bitcoind.
	const blockIndex = 5
	blockHash, err := c.getBlockHash(ctx, blockIndex)
	if err != nil {
		return false, err
	}

	blockInfo, err := c.getBlockVerbose(ctx, blockHash)
	if err != nil {
		return false, err
	}

	if len(blockInfo.Tx) == 0 {
		return false, fmt.Errorf("block %d does not have a coinbase transaction", blockIndex)
	}

	txHash, err := chainhash.NewHashFromStr(blockInfo.Tx[0])
	if err != nil {
		return false, err
	}

	// Retrieve coinbase transaction information.
	txBytes, err := c.getRawTransaction(ctx, txHash)
	if err != nil {
		return false, err
	}

	return len(txBytes) != 0, nil
}

func (c *bitcoinChain) getBlockHash(ctx context.Context, index int64) (*chainhash.Hash, error) {
	var blockHashStr string
	if err := c.call(ctx, "getblockhash", []any{index}, &blockHashStr); err != nil {
		return nil, err
	}
	return chainhash.NewHashFromStr(blockHashStr)
}

type getBlockVerboseResult struct {
	Hash          string   `json:"hash"`
	Confirmations int64    `json:"confirmations"`
	Height        int64    `json:"height"`
	Tx            []string `json:"tx,omitempty"`
	PreviousHash  string   `json:"previousblockhash"`
}

func (c *bitcoinChain) getBlockVerbose(ctx context.Context, blockHash *chainhash.Hash) (*getBlockVerboseResult, error) {
	var res getBlockVerboseResult
	return &res, c.call(ctx, "getblock", []any{blockHash.String(), []any{1}}, res)
}

func (c *bitcoinChain) getRawTransaction(ctx context.Context, txHash *chainhash.Hash) ([]byte, error) {
	var txB dex.Bytes
	return txB, c.call(ctx, "getrawtransaction", []any{txHash.String(), false}, &txB)
}

func (c *bitcoinChain) call(ctx context.Context, method string, args []any, thing any) error {
	params := make([]json.RawMessage, 0, len(args))
	for i := range args {
		p, err := json.Marshal(args[i])
		if err != nil {
			return err
		}
		params = append(params, p)
	}
	b, err := c.cl.RawRequest(ctx, method, params)
	if err != nil {
		return fmt.Errorf("rawrequest error: %w", err)
	}

	if thing != nil {
		return json.Unmarshal(b, thing)
	}
	return nil
}

func (c *bitcoinChain) CheckBond(b *tanka.Bond) error {

	// TODO: Validate bond

	return nil
}

func (c *bitcoinChain) AuditHTLC(*tanka.HTLCAudit) (bool, error) {

	// TODO: Perform the audit

	return true, nil
}

// isMethodNotFoundErr will return true if the error indicates that the RPC
// method was not found by the RPC server. The error must be dcrjson.RPCError
// with a numeric code equal to btcjson.ErrRPCMethodNotFound.Code or a message
// containing "method not found".
func isMethodNotFoundErr(err error) bool {
	var errRPCMethodNotFound = int(btcjson.ErrRPCMethodNotFound.Code)
	var rpcErr *btcjson.RPCError
	return errors.As(err, &rpcErr) &&
		(int(rpcErr.Code) == errRPCMethodNotFound ||
			strings.Contains(strings.ToLower(rpcErr.Message), "method not found"))
}
