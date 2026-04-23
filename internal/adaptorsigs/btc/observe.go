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
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/btcsuite/btcd/btcjson"
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

// FundBroadcastTaproot funds and broadcasts a tx that pays `value` to
// the given taproot pkScript, waits for it to confirm, and returns
// the confirmed tx, the vout index of the taproot output, and the
// confirmation block height.
//
// The lock output's pkScript is what distinguishes it from any
// change output the funding process may have added, so the caller
// must pass in the exact pkScript they want to locate.
//
// waitForConfirm is a caller-provided function; typically it mines
// regtest blocks or polls for mainnet confirms. It receives the
// tx hash and should return the block height the tx confirmed at.
// The separation lets tests and live runs share this helper without
// the helper itself knowing anything about mining or chain cadence.
func FundBroadcastTaproot(ctx context.Context, client *rpcclient.Client,
	pkScript []byte, value int64,
	waitForConfirm func(ctx context.Context, txid *chainhash.Hash) (int64, error),
) (*wire.MsgTx, uint32, int64, error) {

	unfunded := wire.NewMsgTx(2)
	unfunded.AddTxOut(&wire.TxOut{Value: value, PkScript: pkScript})

	funded, err := client.FundRawTransaction(unfunded, fundOpts(), nil)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("fund raw: %w", err)
	}
	// Use RawRequest for signrawtransactionwithwallet and sendrawtransaction
	// rather than client.SignRawTransaction / client.SendRawTransaction:
	// btcd's rpcclient calls the legacy "signrawtransaction" method, which
	// Bitcoin Core removed, and its SendRawTransaction fails version
	// detection against Bitcoin Core 28+. Same pattern as the BTCRPCAdapter
	// in client/core/adaptorswap_bridge.go.
	var fundedBuf bytes.Buffer
	if err := funded.Transaction.Serialize(&fundedBuf); err != nil {
		return nil, 0, 0, fmt.Errorf("serialize funded: %w", err)
	}
	hexFunded, err := json.Marshal(hex.EncodeToString(fundedBuf.Bytes()))
	if err != nil {
		return nil, 0, 0, err
	}
	signRaw, err := client.RawRequest("signrawtransactionwithwallet",
		[]json.RawMessage{hexFunded})
	if err != nil {
		return nil, 0, 0, fmt.Errorf("sign raw: %w", err)
	}
	var signResult struct {
		Hex      string `json:"hex"`
		Complete bool   `json:"complete"`
	}
	if err := json.Unmarshal(signRaw, &signResult); err != nil {
		return nil, 0, 0, fmt.Errorf("decode sign result: %w", err)
	}
	if !signResult.Complete {
		return nil, 0, 0, errors.New("fundBroadcastTaproot: sign incomplete")
	}
	signedBytes, err := hex.DecodeString(signResult.Hex)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("decode signed hex: %w", err)
	}
	signed := wire.NewMsgTx(2)
	if err := signed.Deserialize(bytes.NewReader(signedBytes)); err != nil {
		return nil, 0, 0, fmt.Errorf("deserialize signed: %w", err)
	}
	hexSigned, err := json.Marshal(hex.EncodeToString(signedBytes))
	if err != nil {
		return nil, 0, 0, err
	}
	sendRaw, err := client.RawRequest("sendrawtransaction",
		[]json.RawMessage{hexSigned})
	if err != nil {
		return nil, 0, 0, fmt.Errorf("send raw: %w", err)
	}
	var txidStr string
	if err := json.Unmarshal(sendRaw, &txidStr); err != nil {
		return nil, 0, 0, fmt.Errorf("decode txid: %w", err)
	}
	txHash, err := chainhash.NewHashFromStr(txidStr)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("parse txid: %w", err)
	}

	// Locate the taproot output.
	vout := uint32(0)
	found := false
	for i, out := range signed.TxOut {
		if bytes.Equal(out.PkScript, pkScript) {
			vout = uint32(i)
			found = true
			break
		}
	}
	if !found {
		return nil, 0, 0, errors.New("taproot output missing after funding")
	}

	height := int64(-1)
	if waitForConfirm != nil {
		height, err = waitForConfirm(ctx, txHash)
		if err != nil {
			return nil, 0, 0, fmt.Errorf("wait confirm: %w", err)
		}
	}
	return signed, vout, height, nil
}

// fundOpts returns the default fundrawtransaction options. Kept as a
// function so callers can override if they need specific fee rates
// or change types.
func fundOpts() btcjson.FundRawTransactionOpts {
	return btcjson.FundRawTransactionOpts{}
}
