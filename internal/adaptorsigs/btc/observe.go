// ObserveSpend scans the chain for the transaction that spends a
// specific outpoint and returns the witness stack of the spending
// input. Needed by the adaptor-swap orchestrator so it can extract
// the completed BIP-340 signature from an on-chain taproot spend and
// feed it into RecoverTweakBIP340 to recover the counterparty's
// ed25519 scalar.
//
// Separated from btc.go so it can be used as a narrow primitive
// without pulling in the tapscript script builders. Callers provide
// the rpcclient.Client; this file holds no state.

package btc

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/rpcclient"
	"github.com/btcsuite/btcd/wire"
)

// ObserveSpend blocks until the outpoint has been spent and returns
// the witness stack of the spending input. It polls at the given
// interval (minimum 1 second) and stops when ctx is cancelled.
//
// startHeight is the block height from which to begin the scan;
// typically the confirmation height of the tx that created the
// outpoint. The scan walks forward, so if the outpoint is spent at
// or after startHeight this function returns the witness promptly.
//
// The pollInterval controls how often we refresh the chain tip when
// no spend is yet visible. For regtest, 1 second is fine; for mainnet
// 10-30 seconds is more appropriate.
func ObserveSpend(ctx context.Context, client *rpcclient.Client,
	outpoint wire.OutPoint, startHeight int64, pollInterval time.Duration) (wire.TxWitness, error) {

	if pollInterval < time.Second {
		pollInterval = time.Second
	}
	if startHeight < 0 {
		return nil, errors.New("negative startHeight")
	}

	// Current scan position. Advances as we consume blocks.
	cursor := startHeight
	for {
		tip, err := client.GetBlockCount()
		if err != nil {
			return nil, fmt.Errorf("block count: %w", err)
		}
		for cursor <= tip {
			witness, err := scanBlockForSpend(client, cursor, outpoint)
			if err != nil {
				return nil, err
			}
			if witness != nil {
				return witness, nil
			}
			cursor++
		}
		// No spend yet; wait and re-poll.
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}

// scanBlockForSpend looks through block at height `h` for a
// transaction whose input consumes `op`. If found, returns the input's
// witness; otherwise nil, nil.
func scanBlockForSpend(client *rpcclient.Client, h int64,
	op wire.OutPoint) (wire.TxWitness, error) {

	hash, err := client.GetBlockHash(h)
	if err != nil {
		return nil, fmt.Errorf("block hash at %d: %w", h, err)
	}
	block, err := client.GetBlock(hash)
	if err != nil {
		return nil, fmt.Errorf("get block %s: %w", hash, err)
	}
	for _, tx := range block.Transactions {
		for _, in := range tx.TxIn {
			if in.PreviousOutPoint == op {
				return in.Witness, nil
			}
		}
	}
	return nil, nil
}

// ObserveSpendInMempool checks the mempool (via getrawmempool +
// getrawtransaction) for an unconfirmed spend of the outpoint. This
// is faster than waiting for block inclusion but requires the
// bitcoind instance to have txindex=1 (otherwise getrawtransaction
// fails for non-wallet txs).
func ObserveSpendInMempool(client *rpcclient.Client,
	op wire.OutPoint) (wire.TxWitness, error) {

	mempool, err := client.GetRawMempool()
	if err != nil {
		return nil, fmt.Errorf("get raw mempool: %w", err)
	}
	for _, h := range mempool {
		tx, err := client.GetRawTransaction(h)
		if err != nil {
			// Skip txs we cannot retrieve.
			continue
		}
		for _, in := range tx.MsgTx().TxIn {
			if in.PreviousOutPoint == op {
				return in.Witness, nil
			}
		}
	}
	return nil, nil
}

// Convenience: ensure we link chainhash so a future caller can use
// it to construct OutPoints.
var _ = chainhash.Hash{}
