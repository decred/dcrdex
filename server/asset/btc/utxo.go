// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package btc

import (
	"bytes"
	"crypto/sha256"
	"fmt"

	"decred.org/dcrdex/dex"
	dexbtc "decred.org/dcrdex/dex/btc"
	"decred.org/dcrdex/server/asset"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcutil"
)

const ErrReorgDetected = dex.Error("reorg detected")

// TXIO is common information stored with an Input or a UTXO.
type TXIO struct {
	// Because a TXIO's validity and block info can change after creation, keep a
	// Backend around to query the state of the tx and update the block info.
	btc *Backend
	tx  *Tx
	// The height and hash of the transaction's best known block.
	height    uint32
	blockHash chainhash.Hash
	// The number of confirmations needed for maturity. For outputs of coinbase
	// transactions, this will be set to chaincfg.Params.CoinbaseMaturity (256 for
	// mainchain). For other supported script types, this will be zero.
	maturity int32
	// While the TXIO's tx is still in mempool, the tip hash will be stored.
	// This enables an optimization in the Confirmations method to return zero
	// without extraneous RPC calls.
	lastLookup *chainhash.Hash
}

// confirmations returns the number of confirmations for a TXIO's transaction.
// Because a tx can become invalid after once being considered valid, validity
// should be verified again on every call. An error will be returned if this
// TXIO is no longer ready to spend. An unmined transaction should have zero
// confirmations. A transaction in the current best block should have one
// confirmation. The value -1 will be returned with any error.
func (txio *TXIO) confirmations() (int64, error) {
	btc := txio.btc
	tipHash := btc.blockCache.tipHash()
	// If the tx was a mempool transaction, check if it has been confirmed.
	if txio.height == 0 {
		// If the tip hasn't changed, don't do anything here.
		if txio.lastLookup == nil || *txio.lastLookup != tipHash {
			txio.lastLookup = &tipHash
			verboseTx, err := txio.btc.node.GetRawTransactionVerbose(&txio.tx.hash)
			if err != nil {
				return -1, fmt.Errorf("GetRawTransactionVerbose for txid %s: %v", txio.tx.hash, err)
			}
			// More than zero confirmations would indicate that the transaction has
			// been mined. Collect the block info and update the tx fields.
			if verboseTx.Confirmations > 0 {
				blk, err := txio.btc.getBlockInfo(verboseTx.BlockHash)
				if err != nil {
					return -1, err
				}
				txio.height = blk.height
				txio.blockHash = blk.hash
			}
			return int64(verboseTx.Confirmations), nil
		}
	} else {
		// The tx was included in a block, but make sure that the tx's block has
		// not been orphaned.
		mainchainBlock, found := btc.blockCache.atHeight(txio.height)
		if !found {
			return -1, fmt.Errorf("no mainchain block for tx %s at height %d", txio.tx.hash.String(), txio.height)
		}
		// If the tx's block has been orphaned, check for a new containing block.
		if mainchainBlock.hash != txio.blockHash {
			return -1, ErrReorgDetected
		}
	}
	// If the height is still 0, this is a mempool transaction.
	if txio.height == 0 {
		return 0, nil
	}
	// Otherwise just check that there hasn't been a reorg which would render the
	// output immature. This would be exceedingly rare (impossible?).
	confs := int32(btc.blockCache.tipHeight()) - int32(txio.height) + 1
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

// FeeRate returns the transaction fee rate, in satoshi/vbyte.
func (txio *TXIO) FeeRate() uint64 {
	return txio.tx.feeRate
}

// Input is a transaction input.
type Input struct {
	TXIO
	vin uint32
}

var _ asset.Coin = (*Input)(nil)

// String creates a human-readable representation of a Bitcoin transaction input
// in the format "{txid = [transaction hash], vin = [input index]}".
func (input *Input) String() string {
	return fmt.Sprintf("{txid = %s, vin = %d}", input.TxID(), input.vin)
}

// Confirmations returns the number of confirmations on this input's
// transaction.
func (input *Input) Confirmations() (int64, error) {
	confs, err := input.confirmations()
	if err == ErrReorgDetected {
		newInput, err := input.btc.input(&input.tx.hash, input.vin)
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
	scriptType dexbtc.BTCScriptType
	// The output's scriptPubkey.
	pkScript []byte
	// If the pubkey script is P2SH or P2WSH, the UTXO will only be generated if
	// the redeem script is supplied and the script-hash validated. For P2PKH and
	// P2WPKH pubkey scripts, the redeem script should be nil.
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

// String creates a human-readable representation of a Bitcoin transaction output
// in the format "{txid = [transaction hash], vout = [output index]}".
func (utxo *UTXO) String() string {
	return fmt.Sprintf("{txid = %s, vout = %d}", utxo.TxID(), utxo.vout)
}

// Confirmations returns the number of confirmations for a UTXO's transaction.
// Because a UTXO can become invalid after once being considered valid, validity
// should be verified again on every call. An error will be returned if this
// UTXO is no longer ready to spend. An unmined transaction should have zero
// confirmations. A transaction in the current best block should have one
// confirmation. The value -1 will be returned with any error.
func (utxo *UTXO) Confirmations() (int64, error) {
	confs, err := utxo.confirmations()
	if err == ErrReorgDetected {
		// See if we can find the utxo in another block.
		newUtxo, err := utxo.btc.utxo(&utxo.tx.hash, utxo.vout, utxo.redeemScript)
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
	// If there are not enough pubkeys, no reason to check anything.
	if len(pubkeys) < utxo.numSigs {
		return fmt.Errorf("not enough signatures for utxo %s:%d. expected %d, got %d",
			utxo.tx.hash, utxo.vout, utxo.numSigs, len(pubkeys))
	}
	// Extract the addresses from the pubkey scripts and redeem scripts.
	evalScript := utxo.pkScript
	if utxo.scriptType.IsP2SH() || utxo.scriptType.IsP2WSH() {
		evalScript = utxo.redeemScript
	}
	scriptAddrs, err := dexbtc.ExtractScriptAddrs(evalScript, utxo.btc.chainParams)
	if err != nil {
		return err
	}
	// Sanity check that the required signature count matches the count parsed
	// during UTXO initialization.
	if scriptAddrs.NRequired != utxo.numSigs {
		return fmt.Errorf("signature requirement mismatch. required: %d, matched: %d",
			scriptAddrs.NRequired, utxo.numSigs)
	}
	matches := append(pkMatches(pubkeys, scriptAddrs.PubKeys, nil),
		pkMatches(pubkeys, scriptAddrs.PKHashes, btcutil.Hash160)...)
	if len(matches) < utxo.numSigs {
		return fmt.Errorf("not enough pubkey matches to satisfy the script for utxo %s:%d. expected %d, got %d",
			utxo.tx.hash, utxo.vout, utxo.numSigs, len(matches))
	}
	for _, match := range matches {
		err := checkSig(msg, match.pubkey, sigs[match.idx])
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
	pubkey []byte
	idx    int
}

// pkMatches looks through a set of addresses and a returns a set of match
// structs with details about the match.
func pkMatches(pubkeys [][]byte, addrs []btcutil.Address, hasher func([]byte) []byte) []pkMatch {
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
				matchIndex[addrStr] = struct{}{}
				matches = append(matches, pkMatch{
					pubkey: pubkey,
					idx:    i,
				})
				break
			}
		}
	}
	return matches
}

// auditContract checks that UTXO is a swap contract and extracts the
// receiving address and contract value on success.
func (utxo *UTXO) auditContract() error {
	tx := utxo.tx
	if len(tx.outs) <= int(utxo.vout) {
		return fmt.Errorf("invalid index %d for transaction %s", utxo.vout, tx.hash)
	}
	output := tx.outs[int(utxo.vout)]

	// If it's a pay-to-script-hash, extract the script hash and check it against
	// the hash of the user-supplied redeem script.
	scriptType := dexbtc.ParseScriptType(output.pkScript, utxo.redeemScript)
	if scriptType == dexbtc.ScriptUnsupported {
		return fmt.Errorf("specified output %s:%d is not P2SH", tx.hash, utxo.vout)
	}
	var scriptHash, hashed []byte
	if scriptType.IsP2SH() {
		if scriptType.IsSegwit() {
			scriptHash = extractWitnessScriptHash(output.pkScript)
			shash := sha256.Sum256(utxo.redeemScript)
			hashed = shash[:]
		} else {
			scriptHash = extractScriptHash(output.pkScript)
			hashed = btcutil.Hash160(utxo.redeemScript)
		}
	}
	if scriptHash == nil {
		return fmt.Errorf("specified output %s:%d is not P2SH", tx.hash, utxo.vout)
	}
	if !bytes.Equal(hashed, scriptHash) {
		return fmt.Errorf("swap contract hash mismatch for %s:%d", tx.hash, utxo.vout)
	}
	_, receiver, err := extractSwapAddresses(utxo.redeemScript, utxo.btc.chainParams)
	if err != nil {
		return fmt.Errorf("error extracting address from swap contract for %s:%d: %v", tx.hash, utxo.redeemScript, err)
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
