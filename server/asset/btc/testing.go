// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package btc

import (
	"encoding/hex"
	"fmt"
	"testing"

	dexbtc "decred.org/dcrdex/dex/networks/btc"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/txscript"
)

// LiveP2SHStats will scan the provided Backend's node for inputs that spend
// pay-to-script-hash outputs. The pubkey scripts and redeem scripts are
// examined to ensure the backend understands what they are and can extract
// addresses. Ideally, the stats will show no scripts which were unparseable by
// the backend, but the presence of unknowns is not an error.
func LiveP2SHStats(btc *Backend, t *testing.T) {
	numToDo := 10000
	type scriptStats struct {
		unknown      int
		p2pk         int
		p2pkh        int
		p2wpkh       int
		p2wsh        int
		multisig     int
		escrow       int
		found        int
		zeros        int
		empty        int
		swaps        int
		emptyRedeems int
		addrErr      int
		nonStd       int
		noSigs       int
	}
	var stats scriptStats
	hash, err := btc.node.GetBestBlockHash()
	if err != nil {
		t.Fatalf("error getting best block hash: %v", err)
	}
	block, err := btc.node.GetBlockVerbose(hash)
	if err != nil {
		t.Fatalf("error getting best block verbose: %v", err)
	}
	unknowns := []string{}
	// For each txIn, grab the previous outpoint. If the outpoint pkScript is
	// p2sh or p2wsh, locate the redeem script and take some stats on how the
	// redeem script parses.
out:
	for {
		for _, txid := range block.Tx {
			txHash, err := chainhash.NewHashFromStr(txid)
			if err != nil {
				t.Fatalf("error parsing transaction hash from %s: %v", txid, err)
			}
			tx, err := btc.node.GetRawTransactionVerbose(txHash)
			if err != nil {
				t.Fatalf("error fetching transaction %s: %v", txHash, err)
			}
			for vin, txIn := range tx.Vin {
				txOutHash, err := chainhash.NewHashFromStr(txIn.Txid)
				if err != nil {
					t.Fatalf("error decoding txhash from hex %s: %v", txIn.Txid, err)
				}
				if *txOutHash == zeroHash {
					stats.zeros++
					continue
				}
				prevOutTx, err := btc.node.GetRawTransactionVerbose(txOutHash)
				if err != nil {
					t.Fatalf("error fetching previous outpoint: %v", err)
				}
				prevOutpoint := prevOutTx.Vout[int(txIn.Vout)]
				pkScript, err := hex.DecodeString(prevOutpoint.ScriptPubKey.Hex)
				if err != nil {
					t.Fatalf("error decoding script from hex %s: %v", prevOutpoint.ScriptPubKey.Hex, err)
				}
				scriptType := dexbtc.ParseScriptType(pkScript, nil)
				if scriptType.IsP2SH() {
					stats.found++
					if stats.found > numToDo {
						break out
					}
					var redeemScript []byte
					if scriptType.IsSegwit() {
						// if it's segwit, the script is the last input witness data.
						redeemHex := txIn.Witness[len(txIn.Witness)-1]
						redeemScript, err = hex.DecodeString(redeemHex)
						if err != nil {
							t.Fatalf("error decoding redeem script from hex %s: %v", redeemHex, err)
						}
					} else {
						// If it's non-segwit P2SH, the script is the last data push
						// in the scriptSig.
						scriptSig, err := hex.DecodeString(txIn.ScriptSig.Hex)
						if err != nil {
							t.Fatalf("error decoding redeem script from hex %s: %v", txIn.ScriptSig.Hex, err)
						}
						pushed, err := txscript.PushedData(scriptSig)
						if err != nil {
							t.Fatalf("error parsing scriptSig: %v", err)
						}
						if len(pushed) == 0 {
							stats.empty++
							continue
						}
						redeemScript = pushed[len(pushed)-1]
					}
					scriptType := dexbtc.ParseScriptType(pkScript, redeemScript)
					scriptClass := txscript.GetScriptClass(redeemScript)
					switch scriptClass {
					case txscript.MultiSigTy:
						if !scriptType.IsMultiSig() {
							t.Fatalf("multi-sig script class but not parsed as multi-sig")
						}
						stats.multisig++
					case txscript.PubKeyTy:
						stats.p2pk++
					case txscript.PubKeyHashTy:
						stats.p2pkh++
					case txscript.WitnessV0PubKeyHashTy:
						stats.p2wpkh++
					case txscript.WitnessV0ScriptHashTy:
						stats.p2wsh++
					default:
						_, _, _, _, err = dexbtc.ExtractSwapDetails(redeemScript, btc.segwit, btc.chainParams)
						if err == nil {
							stats.swaps++
							continue
						}
						if isEscrowScript(redeemScript) {
							stats.escrow++
							continue
						}
						if len(redeemScript) == 0 {
							stats.emptyRedeems++
						}
						unknowns = append(unknowns, txHash.String()+":"+fmt.Sprintf("%d", vin))
						stats.unknown++
					}
					evalScript := pkScript
					if scriptType.IsP2SH() {
						evalScript = redeemScript
					}
					scriptAddrs, nonStandard, err := dexbtc.ExtractScriptAddrs(evalScript, btc.chainParams)
					if err != nil {
						stats.addrErr++
						continue
					}
					if nonStandard {
						stats.nonStd++
					}
					if scriptAddrs.NRequired == 0 {
						stats.noSigs++
					}
				}
			}
		}
		prevHash, err := chainhash.NewHashFromStr(block.PreviousHash)
		if err != nil {
			t.Fatalf("error decoding previous block hash: %v", err)
		}
		block, err = btc.node.GetBlockVerbose(prevHash)
		if err != nil {
			t.Fatalf("error getting previous block verbose: %v", err)
		}
	}
	t.Logf("%d P2WPKH redeem scripts", stats.p2wpkh)
	t.Logf("%d P2WSH redeem scripts", stats.p2wsh)
	t.Logf("%d multi-sig redeem scripts", stats.multisig)
	t.Logf("%d P2PK redeem scripts", stats.p2pk)
	t.Logf("%d P2PKH redeem scripts", stats.p2pkh)
	t.Logf("%d unknown redeem scripts, %d of which were empty", stats.unknown, stats.emptyRedeems)
	t.Logf("%d previous outpoint zero hashes (coinbase)", stats.zeros)
	t.Logf("%d atomic swap contract redeem scripts", stats.swaps)
	t.Logf("%d escrow scripts", stats.escrow)
	t.Logf("%d error parsing addresses from script", stats.addrErr)
	t.Logf("%d scripts parsed with 0 required signatures", stats.noSigs)
	t.Logf("%d unexpected empty scriptSig", stats.empty)
	numUnknown := len(unknowns)
	if numUnknown > 0 {
		numToShow := 5
		if numUnknown < numToShow {
			numToShow = numUnknown
		}
		t.Logf("showing %d of %d unknown scripts", numToShow, numUnknown)
		for i, unknown := range unknowns {
			if i == numToShow {
				break
			}
			t.Logf("    %x", unknown)
		}
	} else {
		t.Logf("no unknown script types")
	}
}

// LiveUTXOStats will scan the provided Backend's node for transaction
// outputs. The outputs are requested with GetRawTransactionVerbose, and
// statistics collected regarding spendability and pubkey script types. This
// test does not request via the Backend.UTXO method and is not meant to
// cover that code. Instead, these tests check the backend's real-world
// blockchain literacy. Ideally, the stats will show no scripts which were
// unparseable by the backend, but the presence of unknowns is not an error.
func LiveUTXOStats(btc *Backend, t *testing.T) {
	numToDo := 1000
	hash, err := btc.node.GetBestBlockHash()
	if err != nil {
		t.Fatalf("error getting best block hash: %v", err)
	}
	block, err := btc.node.GetBlockVerbose(hash)
	if err != nil {
		t.Fatalf("error getting best block verbose: %v", err)
	}
	type testStats struct {
		p2pkh    int
		p2wpkh   int
		p2sh     int
		p2wsh    int
		zeros    int
		unknown  int
		found    int
		checked  int
		utxoErr  int
		utxoVal  uint64
		feeRates []uint64
	}
	var stats testStats
	var unknowns [][]byte
	var processed int
out:
	for {
		for _, txid := range block.Tx {
			txHash, err := chainhash.NewHashFromStr(txid)
			if err != nil {
				t.Fatalf("error parsing transaction hash from %s: %v", txid, err)
			}
			tx, err := btc.node.GetRawTransactionVerbose(txHash)
			if err != nil {
				t.Fatalf("error fetching transaction %s: %v", txHash, err)
			}
			for vout, txOut := range tx.Vout {
				if txOut.Value == 0 {
					stats.zeros++
					continue
				}
				pkScript, err := hex.DecodeString(txOut.ScriptPubKey.Hex)
				if err != nil {
					t.Fatalf("error decoding script from hex %s: %v", txOut.ScriptPubKey.Hex, err)
				}
				scriptType := dexbtc.ParseScriptType(pkScript, nil)
				if scriptType == dexbtc.ScriptUnsupported {
					unknowns = append(unknowns, pkScript)
					stats.unknown++
					continue
				}
				processed++
				if processed >= numToDo {
					break out
				}
				if scriptType.IsP2PKH() {
					if scriptType.IsSegwit() {
						stats.p2wpkh++
					} else {
						stats.p2pkh++
					}
				} else {
					if scriptType.IsSegwit() {
						stats.p2wsh++
					} else {
						stats.p2sh++
					}
				}
				if scriptType.IsP2SH() {
					continue
				}
				stats.checked++
				utxo, err := btc.utxo(txHash, uint32(vout), nil)
				if err != nil {
					stats.utxoErr++
					continue
				}
				stats.feeRates = append(stats.feeRates, utxo.FeeRate())
				stats.found++
				stats.utxoVal += utxo.Value()
			}
		}
		prevHash, err := chainhash.NewHashFromStr(block.PreviousHash)
		if err != nil {
			t.Fatalf("error decoding previous block hash: %v", err)
		}
		block, err = btc.node.GetBlockVerbose(prevHash)
		if err != nil {
			t.Fatalf("error getting previous block verbose: %v", err)
		}
	}
	t.Logf("%d P2PKH scripts", stats.p2pkh)
	t.Logf("%d P2WPKH scripts", stats.p2wpkh)
	t.Logf("%d P2SH scripts", stats.p2sh)
	t.Logf("%d P2WSH scripts", stats.p2wsh)
	t.Logf("%d zero-valued outputs", stats.zeros)
	t.Logf("%d P2(W)PKH UTXOs found of %d checked, %.1f%%", stats.found, stats.checked, float64(stats.found)/float64(stats.checked)*100)
	t.Logf("total unspent value counted: %.2f", float64(stats.utxoVal)/1e8)
	t.Logf("%d P2PKH UTXO retrieval errors (likely already spent, OK)", stats.utxoErr)
	numUnknown := len(unknowns)
	if numUnknown > 0 {
		numToShow := 5
		if numUnknown < numToShow {
			numToShow = numUnknown
		}
		t.Logf("showing %d of %d unknown scripts", numToShow, numUnknown)
		for i, unknown := range unknowns {
			if i == numToShow {
				break
			}
			t.Logf("    %s", hex.EncodeToString(unknown))
		}
	} else {
		t.Logf("no unknown script types")
	}
	// Fees
	feeCount := len(stats.feeRates)
	if feeCount > 0 {
		var feeSum uint64
		for _, r := range stats.feeRates {
			feeSum += r
		}
		t.Logf("%d fees, avg rate %d", feeCount, feeSum/uint64(feeCount))
	}
}

// LiveFeeRates scans a mapping of txid -> fee rate checking that the backend
// returns the expected fee rate.
func LiveFeeRates(btc *Backend, t *testing.T, standards map[string]uint64) {
	for txid, expRate := range standards {
		txHash, err := chainhash.NewHashFromStr(txid)
		if err != nil {
			t.Fatalf("error parsing transaction hash from %s: %v", txid, err)
		}
		verboseTx, err := btc.node.GetRawTransactionVerbose(txHash)
		if err != nil {
			t.Fatalf("error getting raw transaction: %v", err)
		}
		tx, err := btc.transaction(txHash, verboseTx)
		if err != nil {
			t.Fatalf("error retrieving transaction %s", txid)
		}
		if tx.feeRate != expRate {
			t.Fatalf("unexpected fee rate for %s. expected %d, got %d", txid, expRate, tx.feeRate)
		}
	}
}

// This is an unsupported type of script, but one of the few that is fairly
// common.
func isEscrowScript(script []byte) bool {
	if len(script) != 77 {
		return false
	}
	if script[0] == txscript.OP_IF &&
		script[1] == txscript.OP_DATA_33 &&
		script[35] == txscript.OP_ELSE &&
		script[36] == txscript.OP_DATA_2 &&
		script[39] == txscript.OP_CHECKSEQUENCEVERIFY &&
		script[40] == txscript.OP_DROP &&
		script[41] == txscript.OP_DATA_33 &&
		script[75] == txscript.OP_ENDIF &&
		script[76] == txscript.OP_CHECKSIG {

		return true
	}
	return false
}

// CompatibilityItems are a set of pubkey scripts and corresponding
// string-encoded addresses checked in CompatibilityTest. They should be taken
// from existing on-chain data.
type CompatibilityItems struct {
	P2PKHScript  []byte
	PKHAddr      string
	P2WPKHScript []byte
	WPKHAddr     string
	P2SHScript   []byte
	SHAddr       string
	P2WSHScript  []byte
	WSHAddr      string
}

// CompatibilityCheck checks various scripts' compatibility with the Backend.
// If a fork's CompatibilityItems can pass the CompatibilityCheck, the node
// can likely use NewBTCClone to create a DEX-compatible backend.
func CompatibilityCheck(items *CompatibilityItems, chainParams *chaincfg.Params, t *testing.T) {
	checkAddr := func(pkScript []byte, addr string) {
		_, addrs, _, err := txscript.ExtractPkScriptAddrs(pkScript, chainParams)
		if err != nil {
			t.Fatalf("ExtractPkScriptAddrs error: %v", err)
		}
		if len(addrs) == 0 {
			t.Fatalf("no addresses extracted from script %x", pkScript)
		}
		if addrs[0].String() != addr {
			t.Fatalf("address mismatch %s != %s", addrs[0].String(), addr)
		}
	}

	// P2PKH
	pkh := dexbtc.ExtractPubKeyHash(items.P2PKHScript)
	if pkh == nil {
		t.Fatalf("incompatible P2PKH script")
	}
	checkAddr(items.P2PKHScript, items.PKHAddr)

	// P2WPKH
	// A clone doesn't necessarily need segwit, so a nil value here will skip
	// the test.
	if items.P2WPKHScript != nil {
		scriptClass := txscript.GetScriptClass(items.P2WPKHScript)
		if scriptClass != txscript.WitnessV0PubKeyHashTy {
			t.Fatalf("incompatible P2WPKH script")
		}
		checkAddr(items.P2WPKHScript, items.WPKHAddr)
	}

	// P2SH
	sh := dexbtc.ExtractScriptHash(items.P2SHScript)
	if sh == nil {
		t.Fatalf("incompatible P2SH script")
	}
	checkAddr(items.P2SHScript, items.SHAddr)

	// P2WSH
	if items.P2WSHScript != nil {
		scriptClass := txscript.GetScriptClass(items.P2WSHScript)
		if scriptClass != txscript.WitnessV0ScriptHashTy {
			t.Fatalf("incompatible P2WPKH script")
		}
		wsh := dexbtc.ExtractScriptHash(items.P2WSHScript)
		if wsh == nil {
			t.Fatalf("incompatible P2WSH script")
		}
		checkAddr(items.P2WSHScript, items.WSHAddr)
	}
}
