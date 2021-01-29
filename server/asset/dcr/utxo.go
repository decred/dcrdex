// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dcr

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"decred.org/dcrdex/dex"
	dexdcr "decred.org/dcrdex/dex/networks/dcr"
	"decred.org/dcrdex/server/asset"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrec"
	"github.com/decred/dcrd/dcrutil/v4"
)

const ErrReorgDetected = dex.ErrorKind("reorg detected")

// TXIO is common information stored with an Input or Output.
type TXIO struct {
	// Because a TXIO's validity and block info can change after creation, keep a
	// Backend around to query the state of the tx and update the block info.
	dcr *Backend
	tx  *Tx
	// The height and hash of the transaction's best known block.
	height    uint32
	blockHash chainhash.Hash
	// The number of confirmations needed for maturity. For outputs of coinbase
	// transactions and stake-related transactions, this will be set to
	// chaincfg.Params.CoinbaseMaturity (256 for mainchain). For other supported
	// script types, this will be zero.
	maturity int32
	// While the TXIO's tx is still in mempool, the tip hash will be stored.
	// This enables an optimization in the Confirmations method to return zero
	// without extraneous RPC calls.
	lastLookup *chainhash.Hash
}

// confirmations returns the number of confirmations for a tx's transaction.
// Because a tx can become invalid after once being considered valid, validity
// should be verified again on every call. An error will be returned if this tx
// is no longer ready to spend. An unmined transaction should have zero
// confirmations. A transaction in the current best block should have one
// confirmation. The value -1 will be returned with any error. This function is
// NOT thread-safe.
func (txio *TXIO) confirmations(ctx context.Context, checkApproval bool) (int64, error) {
	tipHash := txio.dcr.blockCache.tipHash()
	// If the tx was in a mempool transaction, check if it has been confirmed.
	if txio.height == 0 {
		// If the tip hasn't changed, don't do anything here.
		if txio.lastLookup == nil || *txio.lastLookup != tipHash {
			txio.lastLookup = &tipHash
			verboseTx, err := txio.dcr.node.GetRawTransactionVerbose(ctx, &txio.tx.hash)
			if err != nil {
				return -1, fmt.Errorf("GetRawTransactionVerbose for txid %s: %w", txio.tx.hash, translateRPCCancelErr(err))
			}
			// More than zero confirmations would indicate that the transaction has
			// been mined. Collect the block info and update the tx fields.
			if verboseTx.Confirmations > 0 {
				blk, err := txio.dcr.getBlockInfo(ctx, verboseTx.BlockHash)
				if err != nil {
					return -1, err
				}
				txio.height = blk.height
				txio.blockHash = blk.hash
			}
			return verboseTx.Confirmations, nil
		}
	} else {
		// The tx was included in a block, but make sure that the tx's block has
		// not been orphaned or voted as invalid.
		mainchainBlock, found := txio.dcr.blockCache.atHeight(txio.height)
		if !found || mainchainBlock.hash != txio.blockHash {
			return -1, ErrReorgDetected
		}
		if mainchainBlock != nil && checkApproval {
			nextBlock, err := txio.dcr.getMainchainDcrBlock(ctx, txio.height+1)
			if err != nil {
				return -1, fmt.Errorf("error retrieving approving block tx %s: %w", txio.tx.hash, err)
			}
			if nextBlock != nil && !nextBlock.vote {
				return -1, fmt.Errorf("transaction %s block %s has been voted as invalid", txio.tx.hash, nextBlock.hash)
			}
		}
	}
	// If the height is still 0, this is a mempool transaction.
	if txio.height == 0 {
		return 0, nil
	}
	// Otherwise just check that there hasn't been a reorg which would render the
	// output immature. This would be exceedingly rare (impossible?).
	confs := int32(txio.dcr.blockCache.tipHeight()) - int32(txio.height) + 1
	if confs < txio.maturity {
		return -1, fmt.Errorf("transaction %s became immature", txio.tx.hash)
	}
	return int64(confs), nil
}

// TxID is a string identifier for the transaction, typically a hexadecimal
// representation of the byte-reversed transaction hash.
func (txio *TXIO) TxID() string {
	return txio.tx.hash.String()
}

// FeeRate returns the transaction fee rate, in atoms/byte.
func (txio *TXIO) FeeRate() uint64 {
	return txio.tx.feeRate
}

// Input is a transaction input.
type Input struct {
	TXIO
	vin uint32
}

var _ asset.Coin = (*Input)(nil)

// Value is the value of the previous output spent by the input.
func (input *Input) Value() uint64 {
	return input.TXIO.tx.ins[input.vin].value
}

// String creates a human-readable representation of a Decred transaction input
// in the format "{txid = [transaction hash], vin = [input index]}".
func (input *Input) String() string {
	return fmt.Sprintf("{txid = %s, vin = %d}", input.TxID(), input.vin)
}

// Confirmations returns the number of confirmations on this input's
// transaction.
func (input *Input) Confirmations(ctx context.Context) (int64, error) {
	confs, err := input.confirmations(ctx, false)
	if errors.Is(err, ErrReorgDetected) {
		newInput, err := input.dcr.input(&input.tx.hash, input.vin)
		if err != nil {
			return -1, fmt.Errorf("input block is not mainchain")
		}
		*input = *newInput
		return input.Confirmations(ctx)
	}
	return confs, err
}

// ID returns the coin ID.
func (input *Input) ID() []byte {
	return toCoinID(&input.tx.hash, input.vin)
}

// spendsCoin checks whether a particular coin is spent in this coin's tx.
func (input *Input) spendsCoin(coinID []byte) (bool, error) {
	txHash, vout, err := decodeCoinID(coinID)
	if err != nil {
		return false, fmt.Errorf("error decoding coin ID %x: %w", coinID, err)
	}
	if uint32(len(input.tx.ins)) < input.vin+1 {
		return false, nil
	}
	txIn := input.tx.ins[input.vin]
	return txIn.prevTx == *txHash && txIn.vout == vout, nil
}

// Output represents a transaction output.
type Output struct {
	TXIO
	vout uint32
	// The output value.
	value uint64
	// Addresses encoded by the pkScript or the redeem script in the case of a
	// P2SH pkScript.
	addresses []string
	// A bitmask for script type information.
	scriptType dexdcr.DCRScriptType
	// If the pkScript, or redeemScript in the case of a P2SH pkScript, is
	// non-standard according to txscript.
	nonStandardScript bool
	// The output's scriptPubkey.
	pkScript []byte
	// If the pubkey script is P2SH, the Output will only be generated if
	// the redeem script is supplied and the script-hash validated. If the
	// pubkey script is not P2SH, redeemScript will be nil.
	redeemScript []byte
	// numSigs is the number of signatures required to spend this output.
	numSigs int
	// spendSize stores the best estimate of the size (bytes) of the serialized
	// transaction input that spends this Output.
	spendSize uint32
}

// Confirmations returns the number of confirmations for a transaction output.
// Because it is possible for an output that was once considered valid to later
// be considered invalid, this method can return an error to indicate the output
// is no longer valid. The definition of output validity should not be confused
// with the validity of regular tree transactions that is voted on by
// stakeholders. While stakeholder approval is a part of output validity, there
// are other considerations as well.
func (output *Output) Confirmations(ctx context.Context) (int64, error) {
	confs, err := output.confirmations(ctx, false)
	if errors.Is(err, ErrReorgDetected) {
		newOut, err := output.dcr.output(&output.tx.hash, output.vout, output.redeemScript)
		if err != nil {
			if !errors.Is(err, asset.ErrRequestTimeout) {
				err = fmt.Errorf("output block is not mainchain")
			}
			return -1, err
		}
		*output = *newOut
		return output.Confirmations(ctx)
	}
	return confs, err
}

var _ asset.Coin = (*Output)(nil)

// SpendSize returns the maximum size of the serialized TxIn that spends this
// Output, in bytes.
func (output *Output) SpendSize() uint32 {
	return output.spendSize
}

// ID returns the coin ID.
func (output *Output) ID() []byte {
	return toCoinID(&output.tx.hash, output.vout)
}

// Value is the output value, in atoms.
func (output *Output) Value() uint64 {
	return output.value // == output.TXIO.tx.outs[output.vout].value
}

func (output *Output) Addresses() []string {
	return output.addresses
}

// String creates a human-readable representation of a Decred transaction output
// in the format "{txid = [transaction hash], vout = [output index]}".
func (output *Output) String() string {
	return fmt.Sprintf("{txid = %s, vout = %d}", output.TxID(), output.vout)
}

// Auth verifies that the output pays to the supplied public key(s). This is an
// asset.FundingCoin method.
func (output *Output) Auth(pubkeys, sigs [][]byte, msg []byte) error {
	if len(pubkeys) < output.numSigs {
		return fmt.Errorf("not enough signatures for output %s:%d. expected %d, got %d", output.tx.hash, output.vout, output.numSigs, len(pubkeys))
	}
	evalScript := output.pkScript
	if output.scriptType.IsP2SH() {
		evalScript = output.redeemScript
	}
	scriptAddrs, nonStandard, err := dexdcr.ExtractScriptAddrs(evalScript, chainParams)
	if err != nil {
		return err
	}
	if nonStandard {
		return fmt.Errorf("non-standard script")
	}
	// Ensure that at least 1 signature is required to spend this output.
	// Non-standard scripts are already be caught, but check again here in case
	// this can happen another way. Note that Auth may be called via an
	// interface, where this requirement may not fit into a generic spendability
	// check.
	if scriptAddrs.NRequired == 0 {
		return fmt.Errorf("script requires no signatures to spend")
	}
	if scriptAddrs.NRequired != output.numSigs {
		return fmt.Errorf("signature requirement mismatch for output %s:%d. %d != %d", output.tx.hash, output.vout, scriptAddrs.NRequired, output.numSigs)
	}
	matches, err := pkMatches(pubkeys, scriptAddrs.PubKeys, nil)
	if err != nil {
		return fmt.Errorf("error during pubkey matching: %w", err)
	}
	m, err := pkMatches(pubkeys, scriptAddrs.PkHashes, dcrutil.Hash160)
	if err != nil {
		return fmt.Errorf("error during pubkey hash matching: %w", err)
	}
	matches = append(matches, m...)
	if len(matches) < output.numSigs {
		return fmt.Errorf("not enough pubkey matches to satisfy the script for output %s:%d. expected %d, got %d", output.tx.hash, output.vout, output.numSigs, len(matches))
	}
	for _, match := range matches {
		err := checkSig(msg, match.pubkey, sigs[match.idx], match.sigType)
		if err != nil {
			return err
		}

	}
	return nil
}

// TODO: Eliminate the UTXO type. Instead use Output (asset.Coin) and check for
// spendability in the consumer as needed. This is left as is to retain current
// behavior with respect to the unspent requirements.

// A UTXO is information regarding an unspent transaction output.
type UTXO struct {
	*Output
}

// Confirmations returns the number of confirmations on this output's
// transaction. See also (*Output).Confirmations. This function differs from the
// Output method in that it is necessary to relocate the utxo after a reorg, it
// may error if the output is spent.
func (utxo *UTXO) Confirmations(ctx context.Context) (int64, error) {
	confs, err := utxo.confirmations(ctx, !utxo.scriptType.IsStake())
	if errors.Is(err, ErrReorgDetected) {
		// See if we can find the utxo in another block.
		newUtxo, err := utxo.dcr.utxo(ctx, &utxo.tx.hash, utxo.vout, utxo.redeemScript)
		if err != nil {
			return -1, fmt.Errorf("utxo block is not mainchain")
		}
		*utxo = *newUtxo
		return utxo.Confirmations(ctx)
	}
	return confs, err
}

var _ asset.FundingCoin = (*UTXO)(nil)

type pkMatch struct {
	pubkey  []byte
	sigType dcrec.SignatureType
	idx     int
}

// pkMatches looks through a set of addresses and a returns a set of match
// structs with details about the match.
func pkMatches(pubkeys [][]byte, addrs []dcrutil.Address, hasher func([]byte) []byte) ([]pkMatch, error) {
	matches := make([]pkMatch, 0, len(pubkeys))
	if hasher == nil {
		hasher = func(a []byte) []byte { return a }
	}
	matchIndex := make(map[string]struct{})
	for _, addr := range addrs {
		for i, pubkey := range pubkeys {
			if bytes.Equal(addr.ScriptAddress(), hasher(pubkey)) {
				addrStr := addr.String()
				_, alreadyFound := matchIndex[addrStr]
				if alreadyFound {
					continue
				}
				var sigType dcrec.SignatureType
				switch a := addr.(type) {
				case *dcrutil.AddressPubKeyHash:
					sigType = a.DSA()
				case *dcrutil.AddressSecpPubKey:
					sigType = dcrec.STEcdsaSecp256k1
				case *dcrutil.AddressEdwardsPubKey:
					sigType = dcrec.STEd25519
				case *dcrutil.AddressSecSchnorrPubKey:
					sigType = dcrec.STSchnorrSecp256k1
				default:
					return nil, fmt.Errorf("unsupported signature type")
				}
				matchIndex[addrStr] = struct{}{}
				matches = append(matches, pkMatch{
					pubkey:  pubkey,
					sigType: sigType,
					idx:     i,
				})
				break
			}
		}
	}
	return matches, nil
}

// auditContract checks that the Contract is a swap contract and extracts the
// receiving address and contract value on success.
func auditContract(op *Output) (*asset.Contract, error) {
	tx := op.tx
	if len(tx.outs) <= int(op.vout) {
		return nil, fmt.Errorf("invalid index %d for transaction %s", op.vout, tx.hash)
	}
	output := tx.outs[int(op.vout)]
	scriptHash := dexdcr.ExtractScriptHash(output.pkScript)
	if scriptHash == nil {
		return nil, fmt.Errorf("specified output %s:%d is not P2SH", tx.hash, op.vout)
	}
	if !bytes.Equal(dcrutil.Hash160(op.redeemScript), scriptHash) {
		return nil, fmt.Errorf("swap contract hash mismatch for %s:%d", tx.hash, op.vout)
	}
	_, receiver, lockTime, _, err := dexdcr.ExtractSwapDetails(op.redeemScript, chainParams)
	if err != nil {
		return nil, fmt.Errorf("error parsing swap contract for %s:%d: %w", tx.hash, op.vout, err)
	}
	return &asset.Contract{
		Coin:         op,
		SwapAddress:  receiver.String(),
		RedeemScript: op.redeemScript,
		LockTime:     time.Unix(int64(lockTime), 0),
		TxData:       op.tx.raw,
	}, nil
}
