// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dcr

import (
	"bytes"
	"fmt"

	"decred.org/dcrdex/dex"
	dexdcr "decred.org/dcrdex/dex/dcr"
	"decred.org/dcrdex/server/asset"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrec"
	"github.com/decred/dcrd/dcrutil/v2"
)

const ErrReorgDetected = dex.Error("reorg detected")

// TXIO is common information stored with an Input or a UTXO
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
// should be verified again on every call. An error will be returned if this
// tx is no longer ready to spend. An unmined transaction should have zero
// confirmations. A transaction in the current best block should have one
// confirmation. The value -1 will be returned with any error.
func (txio *TXIO) confirmations(checkApproval bool) (int64, error) {
	dcr := txio.dcr
	tipHash := dcr.blockCache.tipHash()
	// If the tx was in a mempool transaction, check if it has been confirmed.
	if txio.height == 0 {
		// If the tip hasn't changed, don't do anything here.
		if txio.lastLookup == nil || *txio.lastLookup != tipHash {
			txio.lastLookup = &tipHash
			verboseTx, err := txio.dcr.node.GetRawTransactionVerbose(&txio.tx.hash)
			if err != nil {
				return -1, fmt.Errorf("GetRawTransactionVerbose for txid %s: %v", txio.tx.hash, err)
			}
			// More than zero confirmations would indicate that the transaction has
			// been mined. Collect the block info and update the tx fields.
			if verboseTx.Confirmations > 0 {
				blk, err := txio.dcr.getBlockInfo(verboseTx.BlockHash)
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
		mainchainBlock, found := dcr.blockCache.atHeight(txio.height)
		if !found {
			return -1, fmt.Errorf("no mainchain block for tx %s at height %d", txio.tx.hash.String(), txio.height)
		}
		// If the tx's block has been orphaned, check for a new containing block.
		if mainchainBlock.hash != txio.blockHash {
			return -1, ErrReorgDetected
		}
		if mainchainBlock != nil && checkApproval {
			nextBlock, err := dcr.getMainchainDcrBlock(txio.height + 1)
			if err != nil {
				return -1, fmt.Errorf("error retrieving approving block tx %s: %v", txio.tx.hash, err)
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
	confs := int32(dcr.blockCache.tipHeight()) - int32(txio.height) + 1
	if confs < txio.maturity {
		return -1, fmt.Errorf("transaction %s became immature", txio.tx.hash)
	}
	return int64(confs), nil
}

// TxID is a string identifier for the transaction, typically a hexadecimal
// representation of the byte-reversed transaction hash. Should always return
// the same value as the txid argument passed to (Backend).UTXO.
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

// String creates a human-readable representation of a Decred transaction input
// in the format "{txid = [transaction hash], vin = [input index]}".
func (input *Input) String() string {
	return fmt.Sprintf("{txid = %s, vin = %d}", input.TxID(), input.vin)
}

// Confirmations returns the number of confirmations on this input's
// transaction.
func (input *Input) Confirmations() (int64, error) {
	confs, err := input.confirmations(false)
	if err == ErrReorgDetected {
		newInput, err := input.dcr.input(&input.tx.hash, input.vin)
		if err != nil {
			return -1, fmt.Errorf("input block is not mainchain")
		}
		*input = *newInput
		return input.Confirmations()
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
		return false, fmt.Errorf("error decoding coin ID %x: %v", coinID, err)
	}
	if uint32(len(input.tx.ins)) < input.vin+1 {
		return false, nil
	}
	txIn := input.tx.ins[input.vin]
	return txIn.prevTx == *txHash && txIn.vout == vout, nil
}

// A UTXO is information regarding an unspent transaction output.
type UTXO struct {
	TXIO
	vout uint32
	// A bitmask for script type information.
	scriptType dexdcr.DCRScriptType
	// The output's scriptPubkey.
	pkScript []byte
	// If the pubkey script is P2SH, the UTXO will only be generated if
	// the redeem script is supplied and the script-hash validated. If the
	// pubkey script is not P2SH, redeemScript will be nil.
	redeemScript []byte
	// numSigs is the number of signatures required to spend this output.
	numSigs int
	// spendSize stores the best estimate of the size (bytes) of the serialized
	// transaction input that spends this UTXO.
	spendSize uint32
	// The output value.
	value uint64
	// address is populated for swap contract outputs
	address string
}

// Check that UTXO satisfies the asset.Contract interface
var _ asset.Coin = (*UTXO)(nil)
var _ asset.FundingCoin = (*UTXO)(nil)
var _ asset.Contract = (*UTXO)(nil)

// String creates a human-readable representation of a Decred transaction output
// in the format "{txid = [transaction hash], vout = [output index]}".
func (utxo *UTXO) String() string {
	return fmt.Sprintf("{txid = %s, vout = %d}", utxo.TxID(), utxo.vout)
}

// Confirmations is an asset.Backend method that returns the number of
// confirmations for a UTXO. Because it is possible for a UTXO that was once
// considered valid to later be considered invalid, this method can return
// an error to indicate the UTXO is no longer valid. The definition of UTXO
// validity should not be confused with the validity of regular tree
// transactions that is voted on by stakeholders. While stakeholder approval is
// a part of UTXO validity, there are other considerations as well.
func (utxo *UTXO) Confirmations() (int64, error) {
	confs, err := utxo.confirmations(!utxo.scriptType.IsStake())
	if err == ErrReorgDetected {
		// See if we can find the utxo in another block.
		newUtxo, err := utxo.dcr.utxo(&utxo.tx.hash, utxo.vout, utxo.redeemScript)
		if err != nil {
			return -1, fmt.Errorf("utxo block is not mainchain")
		}
		*utxo = *newUtxo
		return utxo.Confirmations()
	}
	return confs, err
}

// Auth verifies that the utxo pays to the supplied public key(s). This is an
// asset.Backend method.
func (utxo *UTXO) Auth(pubkeys, sigs [][]byte, msg []byte) error {
	if len(pubkeys) < utxo.numSigs {
		return fmt.Errorf("not enough signatures for utxo %s:%d. expected %d, got %d", utxo.tx.hash, utxo.vout, utxo.numSigs, len(pubkeys))
	}
	evalScript := utxo.pkScript
	if utxo.scriptType.IsP2SH() {
		evalScript = utxo.redeemScript
	}
	scriptAddrs, err := dexdcr.ExtractScriptAddrs(evalScript, chainParams)
	if err != nil {
		return err
	}
	if scriptAddrs.NRequired != utxo.numSigs {
		return fmt.Errorf("signature requirement mismatch for utxo %s:%d. %d != %d", utxo.tx.hash, utxo.vout, scriptAddrs.NRequired, utxo.numSigs)
	}
	matches, err := pkMatches(pubkeys, scriptAddrs.PubKeys, nil)
	if err != nil {
		return fmt.Errorf("error during pubkey matching: %v", err)
	}
	m, err := pkMatches(pubkeys, scriptAddrs.PkHashes, dcrutil.Hash160)
	if err != nil {
		return fmt.Errorf("error during pubkey hash matching: %v", err)
	}
	matches = append(matches, m...)
	if len(matches) < utxo.numSigs {
		return fmt.Errorf("not enough pubkey matches to satisfy the script for utxo %s:%d. expected %d, got %d", utxo.tx.hash, utxo.vout, utxo.numSigs, len(matches))
	}
	for _, match := range matches {
		err := checkSig(msg, match.pubkey, sigs[match.idx], match.sigType)
		if err != nil {
			return err
		}

	}
	return nil
}

// Script returns the UTXO's redeem script.
func (utxo *UTXO) Script() []byte {
	return utxo.redeemScript
}

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

// AuditContract checks that UTXO is a swap contract and extracts the
// receiving address and contract value on success.
func (utxo *UTXO) auditContract() error {
	tx := utxo.tx
	if len(tx.outs) <= int(utxo.vout) {
		return fmt.Errorf("invalid index %d for transaction %s", utxo.vout, tx.hash)
	}
	output := tx.outs[int(utxo.vout)]
	scriptHash := dexdcr.ExtractScriptHash(output.pkScript)
	if scriptHash == nil {
		return fmt.Errorf("specified output %s:%d is not P2SH", tx.hash, utxo.vout)
	}
	if !bytes.Equal(dcrutil.Hash160(utxo.redeemScript), scriptHash) {
		return fmt.Errorf("swap contract hash mismatch for %s:%d", tx.hash, utxo.vout)
	}
	_, receiver, err := extractSwapAddresses(utxo.redeemScript)
	if err != nil {
		return fmt.Errorf("error extracting address from swap contract for %s:%d", tx.hash, utxo.vout)
	}
	utxo.address = receiver
	return nil
}

// SpendSize returns the maximum size of the serialized TxIn that spends this
// UTXO, in bytes. This is a method of the asset.UTXO interface.
func (utxo *UTXO) SpendSize() uint32 {
	return utxo.spendSize
}

// ID returns the coin ID.
func (utxo *UTXO) ID() []byte {
	return toCoinID(&utxo.tx.hash, utxo.vout)
}

// Value is the output value, in atoms.
func (utxo *UTXO) Value() uint64 {
	return utxo.value
}

// Address is the receiving address if this is a swap contract.
func (utxo *UTXO) Address() string {
	return utxo.address
}
