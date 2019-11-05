// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016-2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package dcr

import (
	"encoding/hex"
	"fmt"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v2"
	"github.com/decred/dcrd/dcrutil/v2"
	"github.com/decred/dcrd/txscript/v2"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrwallet/rpc/jsonrpc/types"
	"github.com/decred/dcrwallet/wallet/v3/txrules"
)

// sumOutputValues returns the total output amount of the provided output set.
func sumOutputValues(outputs []*wire.TxOut) dcrutil.Amount {
	var totalOutput dcrutil.Amount
	for _, txOut := range outputs {
		totalOutput += dcrutil.Amount(txOut.Value)
	}
	return totalOutput
}

// sumInputValues returns the total input amount of the provided input set.
func sumInputValues(inputs []*wire.TxIn) dcrutil.Amount {
	var totalInput dcrutil.Amount
	for _, txIn := range inputs {
		totalInput += dcrutil.Amount(txIn.ValueIn)
	}
	return totalInput
}

// createTransaction generates a transaction using the provided inputs and amounts.
// The transaction defaults to creating P2PKH scripts if no pkScripts are provided.
func createTransaction(inputs []types.ListUnspentResult, amounts map[string]int64,
	pkScripts map[string][]byte, lockTime uint32, relayFee dcrutil.Amount,
	changeAddr dcrutil.Address, params *chaincfg.Params) (*wire.MsgTx, error) {
	if lockTime > wire.MaxTxInSequenceNum {
		return nil, fmt.Errorf("lockTime out of range")
	}

	inputSizes := make([]int, len(inputs))

	// Add all transaction inputs to a new transaction after performing
	// some validity checks.
	tx := wire.NewMsgTx()
	for idx, input := range inputs {
		txHash, err := chainhash.NewHashFromStr(input.TxID)
		if err != nil {
			return nil, err
		}

		if !(input.Tree == wire.TxTreeRegular || input.Tree == wire.TxTreeStake) {
			return nil, fmt.Errorf("tx tree must be regular or stake")
		}

		amt, err := dcrutil.NewAmount(input.Amount)
		if err != nil {
			return nil, err
		}

		prevOutV := int64(amt)
		prevOut := wire.NewOutPoint(txHash, input.Vout, input.Tree)
		txIn := wire.NewTxIn(prevOut, prevOutV, []byte{})
		if lockTime != 0 {
			txIn.Sequence = wire.MaxTxInSequenceNum - 1
		}

		pkScript, err := hex.DecodeString(input.ScriptPubKey)
		if err != nil {
			return nil, fmt.Errorf("unable to decode pkScript: %v", err)
		}

		// Unspent credits are currently expected to be either P2PKH or
		// P2PK, P2PKH/P2SH nested in a revocation/stakechange/vote output.
		scriptClass := txscript.GetScriptClass(0, pkScript)

		switch scriptClass {
		case txscript.PubKeyHashTy:
			inputSizes[idx] = RedeemP2PKHSigScriptSize

		case txscript.PubKeyTy:
			inputSizes[idx] = RedeemP2PKSigScriptSize

		case txscript.StakeRevocationTy, txscript.StakeSubChangeTy, txscript.StakeGenTy:
			scriptClass, err = txscript.GetStakeOutSubclass(pkScript)
			if err != nil {
				return nil, fmt.Errorf("failed to extract nested script "+
					"in stake output: %v", err)
			}

			// For stake transactions we expect P2PKH and P2SH script class
			// types only but ignore P2SH script type since it can pay
			// to any script which the wallet may not recognize.
			if scriptClass != txscript.PubKeyHashTy {
				return nil, fmt.Errorf("unexpected nested script class "+
					" for input #%d sourced from txid %s", idx, input.TxID)
			}

			inputSizes[idx] = RedeemP2PKHSigScriptSize

		default:
			return nil, fmt.Errorf("unexpected script class (%v) for "+
				"input #%d sourced from txid %s", scriptClass, idx, input.TxID)
		}

		tx.AddTxIn(txIn)
	}

	outputSizes := make([]int, 0)

	// Add all transaction outputs with their associated pkScripts to the
	// transaction after performing some validity checks.
	for encodedAddr, amt := range amounts {
		amount := dcrutil.Amount(amt)

		// Ensure the amount is in the valid range for monetary amounts.
		if amount <= 0 || amount > dcrutil.MaxAmount {
			return nil, fmt.Errorf("Invalid amount: 0 >= %v > %v",
				amount, dcrutil.MaxAmount)
		}

		addr, err := dcrutil.DecodeAddress(encodedAddr, params)
		if err != nil {
			return nil, fmt.Errorf("Could not decode address: %v", err)
		}

		// Ensure the address is one of the supported types.
		switch addr.(type) {
		case *dcrutil.AddressPubKeyHash:
		case *dcrutil.AddressScriptHash:
		default:
			return nil, fmt.Errorf("Invalid address type: %T", addr)
		}

		var txOut *wire.TxOut
		// If the pkScripts are not provided the transaction is simply paying
		// amounts to the addresses provided. When provided the transaction is
		// paying to swap contract scripts.
		if pkScripts == nil {
			// Create a new script which pays to the provided address.
			pkScript, err := txscript.PayToAddrScript(addr)
			if err != nil {
				return nil, err
			}

			outputSizes = append(outputSizes, P2PKHPkScriptSize)
			txOut = wire.NewTxOut(amt, pkScript)
		} else {
			// Fetch the associated script for the provided address.
			pkScript, ok := pkScripts[encodedAddr]
			if !ok {
				return nil, fmt.Errorf("no pkscript found for address %s",
					encodedAddr)
			}

			outputSizes = append(outputSizes, P2SHPkScriptSize)
			txOut = wire.NewTxOut(amt, pkScript)
		}

		tx.AddTxOut(txOut)
	}

	tx.LockTime = lockTime

	totalIn := sumInputValues(tx.TxIn)
	totalOut := sumOutputValues(tx.TxOut)

	changeScript, err := txscript.PayToAddrScript(changeAddr)
	if err != nil {
		return nil, err
	}

	changeScriptSize := len(changeScript)
	estSignedSize := EstimateSerializeSizeFromScriptSizes(inputSizes, outputSizes, changeScriptSize)
	txFee := txrules.FeeForSerializeSize(relayFee, estSignedSize)
	changeAmt := totalIn - totalOut - txFee

	if changeAmt != 0 && !txrules.IsDustAmount(changeAmt, changeScriptSize, relayFee) {
		change := &wire.TxOut{
			Value:    int64(changeAmt),
			Version:  wire.DefaultPkScriptVersion,
			PkScript: changeScript,
		}
		l := len(tx.TxOut)
		tx.TxOut = append(tx.TxOut[:l:l], change)
	}

	return tx, nil
}

// generateSwapPkScripts creates the associated pkScripts of the provided
// output addresses and amounts.
func generateSwapPkScripts(amounts map[string]int64, contracts []*Contract, params *chaincfg.Params) (map[string][]byte, error) {
	if len(amounts) != len(contracts) {
		return nil, fmt.Errorf("expected equal number of amounts "+
			"and contracts, got %v contracts, %v amounts",
			len(amounts), len(contracts))
	}

	pkScripts := make(map[string][]byte, len(contracts))
	for encodedAddr := range amounts {
		for _, contract := range contracts {
			if encodedAddr == contract.RedeemAddr.String() {
				contractP2SHPkScript, err :=
					generateContractP2SHPkScript(contract.ContractData, params)
				if err != nil {
					return nil, err
				}

				pkScripts[encodedAddr] = contractP2SHPkScript
			}
		}
	}

	if len(pkScripts) != len(amounts) {
		return nil, fmt.Errorf("expected equal number of amounts "+
			"and contract scripts, got %v contract scripts, %v amounts",
			len(pkScripts), len(amounts))
	}

	return pkScripts, nil
}
