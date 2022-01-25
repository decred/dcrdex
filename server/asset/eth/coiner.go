// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build lgpl
// +build lgpl

package eth

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	dexeth "decred.org/dcrdex/dex/networks/eth"
	"decred.org/dcrdex/server/asset"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
)

var _ asset.Coin = (*swapCoin)(nil)
var _ asset.Coin = (*redeemCoin)(nil)

type baseCoin struct {
	backend      *Backend
	secretHash   [32]byte
	gasPrice     uint64
	gasTipCap    uint64
	dynamicTx    bool
	txHash       common.Hash
	value        uint64
	txData       []byte
	serializedTx []byte
}

type swapCoin struct {
	*baseCoin
	init *dexeth.Initiation
}

type redeemCoin struct {
	*baseCoin
	secret [32]byte
}

// newSwapCoin creates a new swapCoin that stores and retrieves info about a
// swap. It requires a coinID that is a txid type of the initial transaction
// initializing or redeeming the swap. A txid type and not a swap type is
// required because the contract will give us no useful information about the
// swap before it is mined. Having the initial transaction allows us to track
// it in the mempool. It also tells us all the data we need to confirm a tx
// will do what we expect if mined and satisfies contract constraints. These
// fields are verified when the Confirmations method is called.
func (eth *Backend) newSwapCoin(coinID []byte, contractData []byte) (*swapCoin, error) {
	bc, err := eth.baseCoin(coinID, contractData)
	if err != nil {
		return nil, err
	}

	inits, err := dexeth.ParseInitiateData(bc.txData, ethContractVersion)
	if err != nil {
		return nil, fmt.Errorf("unable to parse initiate call data: %v", err)
	}

	init, ok := inits[bc.secretHash]
	if !ok {
		return nil, fmt.Errorf("tx %v does not contain initiation with secret hash %x", bc.txHash, bc.secretHash)
	}

	var sum uint64
	for _, in := range inits {
		sum += in.Value
	}
	if bc.value < sum {
		return nil, fmt.Errorf("tx %s value < sum of inits. %d < %d", bc.txHash, bc.value, sum)
	}

	return &swapCoin{
		baseCoin: bc,
		init:     init,
	}, nil
}

// newRedeemCoin pulls the tx for the coinID => txHash, extracts the secret, and
// provides a coin to check confirmations, as required by asset.Coin interface,
// TODO: The redeemCoin's Confirmation method is never used by the current
// swapper implementation. Might consider an API change for
// asset.Backend.Redemption.
func (eth *Backend) newRedeemCoin(coinID []byte, contractData []byte) (*redeemCoin, error) {
	bc, err := eth.baseCoin(coinID, contractData)
	if err != nil {
		return nil, err
	}

	if bc.value != 0 {
		return nil, fmt.Errorf("expected tx value of zero for redeem but got: %d", bc.value)
	}

	redemptions, err := dexeth.ParseRedeemData(bc.txData, ethContractVersion)
	if err != nil {
		return nil, fmt.Errorf("unable to parse redemption call data: %v", err)
	}
	redemption, ok := redemptions[bc.secretHash]
	if !ok {
		return nil, fmt.Errorf("tx %v does not contain redemption with secret hash %x", bc.txHash, bc.secretHash)
	}

	return &redeemCoin{
		baseCoin: bc,
		secret:   redemption.Secret,
	}, nil
}

// The baseCoin is basic tx and swap contract data.
func (eth *Backend) baseCoin(coinID []byte, contractData []byte) (*baseCoin, error) {
	txHash, err := dexeth.DecodeCoinID(coinID)
	if err != nil {
		return nil, err
	}
	tx, _, err := eth.node.transaction(eth.rpcCtx, txHash)
	if err != nil {
		if errors.Is(err, ethereum.NotFound) {
			return nil, asset.CoinNotFoundError
		}
		return nil, fmt.Errorf("unable to fetch transaction: %v", err)
	}

	serializedTx, err := tx.MarshalBinary()
	if err != nil {
		return nil, err
	}

	contractVer, secretHash, err := dexeth.DecodeContractData(contractData)
	if err != nil {
		return nil, err
	}
	if contractVer != version {
		return nil, fmt.Errorf("contract version %d not supported, only %d", contractVer, version)
	}
	contractAddr := tx.To()
	if *contractAddr != eth.contractAddr {
		return nil, fmt.Errorf("contract address is not supported: %v", contractAddr)
	}

	// Gas price is not stored in the swap, and is used to determine if the
	// initialization transaction could take a long time to be mined. A
	// transaction with a very low gas price may need to be resent with a
	// higher price.
	zero := new(big.Int)
	var dynamicTx bool
	rate := tx.GasPrice()
	if rate == nil || rate.Cmp(zero) <= 0 {
		rate = tx.GasFeeCap()
		dynamicTx = true
		if rate == nil || rate.Cmp(zero) <= 0 {
			return nil, fmt.Errorf("Failed to parse gas price from tx %s", txHash)
		}
	}

	gasPrice, err := dexeth.WeiToGweiUint64(rate)
	if err != nil {
		return nil, fmt.Errorf("unable to convert gas price: %v", err)
	}

	var gasTipCap uint64
	priorityFee := tx.GasTipCap()
	if priorityFee != nil {
		gasTipCap, err = dexeth.WeiToGweiUint64(priorityFee)
		if err != nil {
			return nil, fmt.Errorf("unable to convert priority fee: %v", err)
		}
	}

	// Value is stored in the swap with the initialization transaction.
	value, err := dexeth.WeiToGweiUint64(tx.Value())
	if err != nil {
		return nil, fmt.Errorf("unable to convert value: %v", err)
	}

	return &baseCoin{
		backend:      eth,
		secretHash:   secretHash,
		gasPrice:     gasPrice,
		gasTipCap:    gasTipCap,
		dynamicTx:    dynamicTx,
		txHash:       txHash,
		value:        value,
		txData:       tx.Data(),
		serializedTx: serializedTx,
	}, nil
}

// Confirmations returns the number of confirmations for a Coin's
// transaction.
//
// In the case of ethereum it is extra important to check confirmations before
// confirming a swap. Even though we check the initial transaction's data, if
// that transaction were in mempool at the time, it could be swapped out with
// any other values if a user sent another transaction with a higher gas fee
// and the same account and nonce, effectively voiding the transaction we
// expected to be mined.
func (c *swapCoin) Confirmations(ctx context.Context) (int64, error) {
	swap, err := c.backend.node.swap(ctx, c.secretHash)
	if err != nil {
		return -1, err
	}

	// Uninitiated state is zero confs. It could still be in mempool.
	// It is important to only trust confirmations according to the
	// swap contract. Until there are confirmations we cannot be sure
	// that initiation happened successfully.
	if swap.State == dexeth.SSNone {
		// Assume the tx still has a chance of being mined.
		return 0, nil
	}
	// Any other swap state is ok. We are sure that initialization
	// happened.

	// The swap initiation transaction has some number of
	// confirmations, and we are sure the secret hash belongs to
	// this swap. Assert that the value, receiver, and locktime are
	// as expected.
	if swap.Value != c.init.Value {
		return -1, fmt.Errorf("tx data swap val (%dgwei) does not match contract value (%dgwei)",
			c.init.Value, swap.Value)
	}
	if swap.Participant != c.init.Participant {
		return -1, fmt.Errorf("tx data participant %q does not match contract value %q",
			c.init.Participant, swap.Participant)
	}

	// locktime := swap.RefundBlockTimestamp.Int64()
	if !swap.LockTime.Equal(c.init.LockTime) {
		return -1, fmt.Errorf("expected swap locktime (%s) does not match expected (%s)",
			c.init.LockTime, swap.LockTime)
	}

	bn, err := c.backend.node.blockNumber(ctx)
	if err != nil {
		return 0, fmt.Errorf("unable to fetch block number: %v", err)
	}
	return int64(bn - swap.BlockHeight + 1), nil
}

func (c *redeemCoin) Confirmations(ctx context.Context) (int64, error) {
	swap, err := c.backend.node.swap(ctx, c.secretHash)
	if err != nil {
		return -1, err
	}

	// There should be no need to check the counter party, or value
	// as a swap with a specific secret hash that has been redeemed
	// wouldn't have been redeemed without ensuring the initiator
	// is the expected address and value was also as expected. Also
	// not validating the locktime, as the swap is redeemed and
	// locktime no longer relevant.
	if swap.State == dexeth.SSRedeemed {
		bn, err := c.backend.node.blockNumber(ctx)
		if err != nil {
			return 0, fmt.Errorf("unable to fetch block number: %v", err)
		}
		return int64(bn - swap.BlockHeight + 1), nil
	}
	// If swap is in the Initiated state, the redemption may be
	// unmined.
	if swap.State == dexeth.SSInitiated {
		// Assume the tx still has a chance of being mined.
		return 0, nil
	}
	// If swap is in None state, then the redemption can't possibly
	// succeed as the swap must already be in the Initialized state
	// to redeem. If the swap is in the Refunded state, then the
	// redemption either failed or never happened.
	return -1, fmt.Errorf("redemption in failed state with swap at %s state", swap.State)
}

// ID is the swap's coin ID.
func (c *baseCoin) ID() []byte {
	return c.txHash.Bytes() // c.txHash[:]
}

// TxID is the original init transaction txid.
func (c *baseCoin) TxID() string {
	return c.txHash.String()
}

// String is a human readable representation of the swap coin.
func (c *baseCoin) String() string {
	return c.txHash.String()
}

// Value is the amount paid to the swap, set in initialization. Always zero for
// redemptions.
func (c *baseCoin) Value() uint64 {
	return c.value
}

// FeeRate returns the gas rate, in gwei/gas. It is set in initialization of
// the swapCoin.
func (c *baseCoin) FeeRate() uint64 {
	return c.gasPrice
}
