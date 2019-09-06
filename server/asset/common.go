// Copyright (c) 2019, The Decred developers
// See LICENSE for details.

package asset

import "github.com/decred/slog"

type Error string

func (err Error) Error() string { return string(err) }

const (
	UnsupportedScriptError = Error("unsupported script type")
)

// Network flags passed to asset backends to signify which network to use.
type Network uint8

const (
	Mainnet Network = iota
	Testnet
	Regtest
)

// The DEX recognizes only three networks. Simnet is a alias of Regtest.
const Simnet = Regtest

// Every backend constructor will accept a Logger. All logging should take place
// through the provided logger.
type Logger = slog.Logger

// The DEXAsset interface is an interface for a blockchain backend. The DEX
// primarily needs to track UTXOs and transactions to validate orders, and
// to monitor community conduct.
type DEXAsset interface {
	// UTXO should return a UTXO only for outputs that would be spendable on the
	// blockchain immediately. Pay-to-script-hash UTXOs require the redeem script
	// in order to calculate sigScript length and verify pubkeys.
	UTXO(txid string, vout uint32, redeemScript []byte) (UTXO, error)
	// BlockChannel creates and returns a new channel on which to receive updates
	// when new blocks are connected.
	BlockChannel(size int) chan uint32
	// Transaction returns a DEXTx, which has methods for checking UTXO spending
	// and swap contract info.
	Transaction(txid string) (DEXTx, error)
	// InitTxSize is the size of a serialized atomic swap initialization
	// transaction with 1 input spending a P2PKH utxo, 1 swap contract output and
	// 1 change output.
	InitTxSize() uint32
}

// UTXO provides data about an unspent transaction output.
type UTXO interface {
	// Confirmations returns the number of confirmations for a UTXO's transaction.
	// Because a UTXO can become invalid after once being considered valid, this
	// condition should be checked for during confirmation counting and an error
	// returned if this UTXO is no longer ready to spend. An unmined transaction
	// should have zero confirmations. A transaction in the current best block
	// should have one confirmation. A negative number can be returned if error
	// is not nil.
	Confirmations() (int64, error)
	// PaysToPubkeys checks that the provided pubkeys can spend the UTXO and
	// the signatures are valid.
	PaysToPubkeys(pubkeys, sigs [][]byte, msg []byte) (bool, error)
	// ScriptSize returns the UTXO's maximum sigScript byte count.
	ScriptSize() uint32
	// TxHash is a byte-slice of the UTXO's transaction hash.
	TxHash() []byte
	// TxID is a string identifier for the transaction, typically a hexadecimal
	// representation of the byte-reversed transaction hash. Should always return
	// the same value as the txid argument passed to (DEXAsset).UTXO.
	TxID() string
	// Vout is the output index of the UTXO.
	Vout() uint32
}

// DEXTx provides methods for verifying transaction data.
type DEXTx interface {
	// Confirmations returns the number of confirmations for a transaction.
	// Because a transaction can become invalid after once being considered valid,
	// this condition should be checked for during confirmation counting and an
	// error returned if this transactcion is no longer valid. An unmined
	// transaction should have zero confirmations. A transaction in the current
	// best block should have one transaction. A negative number can be returned
	// if error is not nil.
	Confirmations() (int64, error)
	// SpendsUTXO checks if the transaction spends a specified previous output.
	// An error will be returned if the input is not parseable. If the UTXO is not
	// spent in this transaction, the boolean return value will be false, but no
	// error is returned.
	SpendsUTXO(txid string, vout uint32) (bool, error)
	// AuditContract checks that the provided swap contract hashes to the script
	// hash specified in the output at the indicated vout. The receiving address
	// and output value (in atoms) are returned if no error is encountered.
	AuditContract(vout uint32, contract []byte) (string, uint64, error)
}
