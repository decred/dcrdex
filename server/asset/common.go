// Copyright (c) 2019, The Decred developers
// See LICENSE for details.

package asset

type Error string

func (err Error) Error() string { return string(err) }

const (
	UnsupportedScriptError = Error("unsupported script type")
)

// The DEXAsset interface is an interface for a blockchain backend. The DEX
// primarily needs to track UTXOs and transactions to validate orders, and
// to monitor community conduct.
type DEXAsset interface {
	// UTXO should return a UTXO only for outputs that would be spendable on the
	// blockchain immediately.
	UTXO(txid string, vout uint32) (UTXO, error)
	// BlockChannel creates and returns a new channel on which to receive updates
	// when new blocks are connected.
	BlockChannel(size int) chan uint32
	// Transaction returns a DEXTx, which has utitlities for checking UTXO
	// spending and swap contract info.
	Transaction(txid string) (DEXTx, error)
	// VerifySignature verifies that the message was signed with the private key
	// corresponding to the provided public key.
	VerifySignature(message, pubkey, signature []byte) bool
}

// UTXO provides data about unspent transaction outputs.
type UTXO interface {
	// Confirmations returns the number of confirmations for a UTXO's transaction.
	// Because a UTXO can become invalid after once being considered valid, this
	// condition should be checked for during confirmation counting and an error
	// returned if this UTXO is no longer ready to spend. An unmined transaction
	// should have zero confirmations. A transaction in the current best block
	// should have one transaction. A negative number can be returned if error
	// is not nil.
	Confirmations() (int64, error)
	// PaysToPubkey checks that the hex-encoded pubkey can spend the UTXO. The
	// script argument will be nil for P2PKH, but will be needed if P2SH scripts
	// are supported.
	PaysToPubkey(pubkey, script []byte) bool
	// ScriptSize returns the UTXO's maximum spend script size, in bytes.
	ScriptSize() uint32
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
	SpendsUTXO(txid string, vout uint32) bool
	// SwapDetails returns basic information about a swap transaction. The info
	// returned is, in order, the sender's address, the receiver's address, and
	// the value being sent, in atoms/satoshi.
	SwapDetails(vout uint32) (string, string, uint64, error)
}
