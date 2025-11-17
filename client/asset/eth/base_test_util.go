package eth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"

	"decred.org/dcrdex/dex"
	ethereum "github.com/bisoncraft/op-geth"
	basecommon "github.com/bisoncraft/op-geth/common"
	basehexutil "github.com/bisoncraft/op-geth/common/hexutil"
	basetypes "github.com/bisoncraft/op-geth/core/types"
	baserpc "github.com/bisoncraft/op-geth/rpc"
	"github.com/ethereum/go-ethereum/rpc"
)

var baseID, _ = dex.BipSymbolID("base") // really weth.base

func toBlockNumArg(number *big.Int) string {
	if number == nil {
		return "latest"
	}
	if number.Sign() >= 0 {
		return basehexutil.EncodeBig(number)
	}
	// It's negative.
	if number.IsInt64() {
		return baserpc.BlockNumber(number.Int64()).String()
	}
	// It's negative and large, which is invalid.
	return fmt.Sprintf("<invalid %d>", number)
}

type baseRPCTransaction struct {
	tx *basetypes.Transaction
	txExtraInfo
}

type txExtraInfo struct {
	BlockNumber *string             `json:"blockNumber,omitempty"`
	BlockHash   *basecommon.Hash    `json:"blockHash,omitempty"`
	From        *basecommon.Address `json:"from,omitempty"`
}

func (tx *baseRPCTransaction) UnmarshalJSON(msg []byte) error {
	if err := json.Unmarshal(msg, &tx.tx); err != nil {
		return err
	}
	return json.Unmarshal(msg, &tx.txExtraInfo)
}

type rpcBlock struct {
	Hash         *basecommon.Hash        `json:"hash"`
	Transactions []baseRPCTransaction    `json:"transactions"`
	UncleHashes  []basecommon.Hash       `json:"uncles"`
	Withdrawals  []*basetypes.Withdrawal `json:"withdrawals,omitempty"`
}

var errNotCached = errors.New("sender not cached")

// senderFromServer is a types.Signer that remembers the sender address returned by the RPC
// server. It is stored in the transaction's sender address cache to avoid an additional
// request in TransactionSender.
type senderFromServer struct {
	addr      basecommon.Address
	blockhash basecommon.Hash
}

func setSenderFromServer(tx *basetypes.Transaction, addr basecommon.Address, block basecommon.Hash) {
	// Use types.Sender for side-effect to store our signer into the cache.
	basetypes.Sender(&senderFromServer{addr, block}, tx)
}

func (s *senderFromServer) Equal(other basetypes.Signer) bool {
	os, ok := other.(*senderFromServer)
	return ok && os.blockhash == s.blockhash
}

func (s *senderFromServer) Sender(tx *basetypes.Transaction) (basecommon.Address, error) {
	if s.addr == (basecommon.Address{}) {
		return basecommon.Address{}, errNotCached
	}
	return s.addr, nil
}

func (s *senderFromServer) ChainID() *big.Int {
	panic("can't sign with senderFromServer")
}
func (s *senderFromServer) Hash(tx *basetypes.Transaction) basecommon.Hash {
	panic("can't sign with senderFromServer")
}
func (s *senderFromServer) SignatureValues(tx *basetypes.Transaction, sig []byte) (R, S, V *big.Int, err error) {
	panic("can't sign with senderFromServer")
}

// BaseBlockByNumber gets a block from the base network. BlockByNumber will error
// with "transaction type not supported".
func (cc *combinedRPCClient) BaseBlockByNumber(ctx context.Context, number *big.Int) (*basetypes.Block, error) {
	var raw json.RawMessage
	err := cc.rpc.CallContext(ctx, &raw, "eth_getBlockByNumber", toBlockNumArg(number), true)
	if err != nil {
		return nil, err
	}

	// Decode header and transactions.
	var head *basetypes.Header
	if err := json.Unmarshal(raw, &head); err != nil {
		return nil, err
	}
	// When the block is not found, the API returns JSON null.
	if head == nil {
		return nil, ethereum.NotFound
	}

	var body rpcBlock
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, err
	}
	// Pending blocks don't return a block hash, compute it for sender caching.
	if body.Hash == nil {
		tmp := head.Hash()
		body.Hash = &tmp
	}

	// Quick-verify transaction and uncle lists. This mostly helps with debugging the server.
	if head.UncleHash == basetypes.EmptyUncleHash && len(body.UncleHashes) > 0 {
		return nil, errors.New("server returned non-empty uncle list but block header indicates no uncles")
	}
	if head.UncleHash != basetypes.EmptyUncleHash && len(body.UncleHashes) == 0 {
		return nil, errors.New("server returned empty uncle list but block header indicates uncles")
	}
	if head.TxHash == basetypes.EmptyTxsHash && len(body.Transactions) > 0 {
		return nil, errors.New("server returned non-empty transaction list but block header indicates no transactions")
	}
	if head.TxHash != basetypes.EmptyTxsHash && len(body.Transactions) == 0 {
		return nil, errors.New("server returned empty transaction list but block header indicates transactions")
	}
	// Load uncles because they are not included in the block response.
	var uncles []*basetypes.Header
	if len(body.UncleHashes) > 0 {
		uncles = make([]*basetypes.Header, len(body.UncleHashes))
		reqs := make([]rpc.BatchElem, len(body.UncleHashes))
		for i := range reqs {
			reqs[i] = rpc.BatchElem{
				Method: "eth_getUncleByBlockHashAndIndex",
				Args:   []any{body.Hash, basehexutil.EncodeUint64(uint64(i))},
				Result: &uncles[i],
			}
		}
		if err := cc.Client.Client().BatchCallContext(ctx, reqs); err != nil {
			return nil, err
		}
		for i := range reqs {
			if reqs[i].Error != nil {
				return nil, reqs[i].Error
			}
			if uncles[i] == nil {
				return nil, fmt.Errorf("got null header for uncle %d of block %x", i, body.Hash[:])
			}
		}
	}
	var basehash basecommon.Hash
	copy(basehash[:], body.Hash[:])
	// Fill the sender cache of transactions in the block.
	txs := make([]*basetypes.Transaction, len(body.Transactions))
	for i, tx := range body.Transactions {
		if tx.From != nil {
			setSenderFromServer(tx.tx, *tx.From, basehash)
		}
		txs[i] = tx.tx
	}

	return basetypes.NewBlockWithHeader(head).WithBody(
		basetypes.Body{
			Transactions: txs,
			Uncles:       uncles,
			Withdrawals:  body.Withdrawals,
		}), nil
}
