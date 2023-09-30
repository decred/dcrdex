package btc

import (
	"fmt"
	"strconv"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
)

type UTxO struct {
	TxHash  *chainhash.Hash
	Vout    uint32
	Address string
	Amount  uint64
}

// OutPoint is the hash and output index of a transaction output.
type OutPoint struct {
	TxHash chainhash.Hash
	Vout   uint32
}

// NewOutPoint is the constructor for a new OutPoint.
func NewOutPoint(txHash *chainhash.Hash, vout uint32) OutPoint {
	return OutPoint{
		TxHash: *txHash,
		Vout:   vout,
	}
}

// String is a string representation of the outPoint.
func (pt OutPoint) String() string {
	return pt.TxHash.String() + ":" + strconv.Itoa(int(pt.Vout))
}

// Output is information about a transaction Output. Output satisfies the
// asset.Coin interface.
type Output struct {
	Pt  OutPoint
	Val uint64
}

// NewOutput is the constructor for an output.
func NewOutput(txHash *chainhash.Hash, vout uint32, value uint64) *Output {
	return &Output{
		Pt:  NewOutPoint(txHash, vout),
		Val: value,
	}
}

// Value returns the value of the Output. Part of the asset.Coin interface.
func (op *Output) Value() uint64 {
	return op.Val
}

// ID is the Output's coin ID. Part of the asset.Coin interface. For BTC, the
// coin ID is 36 bytes = 32 bytes tx hash + 4 bytes big-endian vout.
func (op *Output) ID() dex.Bytes {
	return ToCoinID(op.txHash(), op.vout())
}

// String is a string representation of the coin.
func (op *Output) String() string {
	return op.Pt.String()
}

// txHash returns the pointer of the wire.OutPoint's Hash.
func (op *Output) txHash() *chainhash.Hash {
	return &op.Pt.TxHash
}

// vout returns the wire.OutPoint's Index.
func (op *Output) vout() uint32 {
	return op.Pt.Vout
}

// WireOutPoint creates and returns a new *wire.OutPoint for the output.
func (op *Output) WireOutPoint() *wire.OutPoint {
	return wire.NewOutPoint(op.txHash(), op.vout())
}

// ConvertCoin converts the asset.Coin to an Output.
func ConvertCoin(coin asset.Coin) (*Output, error) {
	op, _ := coin.(*Output)
	if op != nil {
		return op, nil
	}
	txHash, vout, err := decodeCoinID(coin.ID())
	if err != nil {
		return nil, err
	}
	return NewOutput(txHash, vout, coin.Value()), nil
}

// AuditInfo is information about a swap contract on that blockchain.
type AuditInfo struct {
	Output     *Output
	Recipient  btcutil.Address // caution: use stringAddr, not the Stringer
	contract   []byte
	secretHash []byte
	expiration time.Time
}

// Expiration returns the expiration time of the contract, which is the earliest
// time that a refund can be issued for an un-redeemed contract.
func (ci *AuditInfo) Expiration() time.Time {
	return ci.expiration
}

// Coin returns the output as an asset.Coin.
func (ci *AuditInfo) Coin() asset.Coin {
	return ci.Output
}

// Contract is the contract script.
func (ci *AuditInfo) Contract() dex.Bytes {
	return ci.contract
}

// SecretHash is the contract's secret hash.
func (ci *AuditInfo) SecretHash() dex.Bytes {
	return ci.secretHash
}

type BlockVector struct {
	Height int64
	Hash   chainhash.Hash
}

// TxOutFromTxBytes parses the specified *wire.TxOut from the serialized
// transaction.
func TxOutFromTxBytes(
	txB []byte,
	vout uint32,
	deserializeTx func([]byte) (*wire.MsgTx, error),
	hashTx func(*wire.MsgTx) *chainhash.Hash,
) (*wire.TxOut, error) {
	msgTx, err := deserializeTx(txB)
	if err != nil {
		return nil, fmt.Errorf("error decoding transaction bytes: %v", err)
	}

	if len(msgTx.TxOut) <= int(vout) {
		return nil, fmt.Errorf("no vout %d in tx %s", vout, hashTx(msgTx))
	}
	return msgTx.TxOut[vout], nil
}

// SwapReceipt is information about a swap contract that was broadcast by this
// wallet. Satisfies the asset.Receipt interface.
type SwapReceipt struct {
	Output            *Output
	SwapContract      []byte
	SignedRefundBytes []byte
	ExpirationTime    time.Time
}

// Expiration is the time that the contract will expire, allowing the user to
// issue a refund transaction. Part of the asset.Receipt interface.
func (r *SwapReceipt) Expiration() time.Time {
	return r.ExpirationTime
}

// Contract is the contract script. Part of the asset.Receipt interface.
func (r *SwapReceipt) Contract() dex.Bytes {
	return r.SwapContract
}

// Coin is the output information as an asset.Coin. Part of the asset.Receipt
// interface.
func (r *SwapReceipt) Coin() asset.Coin {
	return r.Output
}

// String provides a human-readable representation of the contract's Coin.
func (r *SwapReceipt) String() string {
	return r.Output.String()
}

// SignedRefund is a signed refund script that can be used to return
// funds to the user in the case a contract expires.
func (r *SwapReceipt) SignedRefund() dex.Bytes {
	return r.SignedRefundBytes
}

// SyncStatus is the current synchronization state of the node.
type SyncStatus struct {
	Target  int32 `json:"target"`
	Height  int32 `json:"height"`
	Syncing bool  `json:"syncing"`
}
