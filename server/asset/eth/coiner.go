// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

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
	backend      *AssetBackend
	vector       *dexeth.SwapVector
	gasFeeCap    uint64
	gasTipCap    uint64
	txHash       common.Hash
	value        *big.Int
	txData       []byte
	serializedTx []byte
	contractVer  uint32
}

type swapCoin struct {
	*baseCoin
	vector *dexeth.SwapVector
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
func (be *AssetBackend) newSwapCoin(coinID []byte, contractData []byte) (*swapCoin, error) {
	bc, err := be.baseCoin(coinID, contractData)
	if err != nil {
		return nil, err
	}

	vectors, err := dexeth.ParseInitiateDataV1(bc.txData)
	if err != nil {
		return nil, fmt.Errorf("unable to parse initiate call data: %v", err)
	}

	vec, ok := vectors[bc.vector.SecretHash]
	if !ok {
		return nil, fmt.Errorf("tx %v does not contain initiation with locator %x", bc.txHash, bc.vector.Locator())
	}

	if be.assetID == be.baseChainID {
		var sum uint64
		for _, in := range vectors {
			sum += in.Value
		}
		if sum != dexeth.WeiToGwei(bc.value) {
			return nil, fmt.Errorf("tx %s value < sum of inits. %d < %d", bc.txHash, bc.value, sum)
		}
	}

	return &swapCoin{
		baseCoin: bc,
		vector:   vec,
	}, nil
}

// newRedeemCoin pulls the tx for the coinID => txHash, extracts the secret, and
// provides a coin to check confirmations, as required by asset.Coin interface,
// TODO: The redeemCoin's Confirmation method is never used by the current
// swapper implementation. Might consider an API change for
// asset.Backend.Redemption.
func (be *AssetBackend) newRedeemCoin(coinID []byte, contractData []byte) (*redeemCoin, error) {
	bc, err := be.baseCoin(coinID, contractData)
	if err == asset.CoinNotFoundError {
		// If the coin is not found, check to see if the swap has been
		// redeemed by another transaction.
		contractVer, locator, err := dexeth.DecodeLocator(contractData)
		if err != nil {
			return nil, err
		}
		vector, err := dexeth.ParseV1Locator(locator)
		if err != nil {
			return nil, err
		}
		be.log.Warnf("redeem coin with ID %x for locator %x was not found", coinID, locator)
		status, err := be.node.status(be.ctx, be.assetID, vector)
		if err != nil {
			return nil, err
		}
		if status.Step != dexeth.SSRedeemed {
			return nil, asset.CoinNotFoundError
		}
		bc = &baseCoin{
			backend:     be,
			vector:      vector,
			contractVer: contractVer,
		}
		return &redeemCoin{
			baseCoin: bc,
			secret:   status.Secret,
		}, nil
	} else if err != nil {
		return nil, err
	}

	if bc.value.Cmp(new(big.Int)) != 0 {
		return nil, fmt.Errorf("expected tx value of zero for redeem but got: %d", bc.value)
	}

	redemptions, err := dexeth.ParseRedeemDataV1(bc.txData)
	if err != nil {
		return nil, fmt.Errorf("unable to parse redemption call data: %v", err)
	}
	redemption, ok := redemptions[bc.vector.SecretHash]
	if !ok {
		return nil, fmt.Errorf("tx %v does not contain redemption with locator %x", bc.txHash, bc.vector)
	}

	return &redeemCoin{
		baseCoin: bc,
		secret:   redemption.Secret,
	}, nil
}

// The baseCoin is basic tx and swap contract data.
func (be *AssetBackend) baseCoin(coinID []byte, contractData []byte) (*baseCoin, error) {
	txHash, err := dexeth.DecodeCoinID(coinID)
	if err != nil {
		return nil, err
	}
	tx, _, err := be.node.transaction(be.ctx, txHash)
	if err != nil {
		if errors.Is(err, ethereum.NotFound) {
			return nil, asset.CoinNotFoundError
		}
		return nil, fmt.Errorf("unable to fetch transaction: %v", err)
	}
	contractAddr := tx.To()
	if *contractAddr != be.contractAddr {
		return nil, fmt.Errorf("contract address is not supported: %v", contractAddr)
	}

	serializedTx, err := tx.MarshalBinary()
	if err != nil {
		return nil, err
	}

	contractVer, locator, err := dexeth.DecodeLocator(contractData)
	if err != nil {
		return nil, err
	}
	if contractVer != ethContractVersion {
		return nil, fmt.Errorf("contract version %d not supported, only %d", contractVer, ethContractVersion)
	}
	vector, err := dexeth.ParseV1Locator(locator)
	if err != nil {
		return nil, err
	}

	// Gas price is not stored in the swap, and is used to determine if the
	// initialization transaction could take a long time to be mined. A
	// transaction with a very low gas price may need to be resent with a
	// higher price.
	//
	// Although we only retrieve the GasFeeCap and GasTipCap here, legacy
	// transaction are also supported. In legacy transactions, the full
	// gas price that is specified will be used no matter what, so the
	// values returned from GasFeeCap and GasTipCap will both be equal to the
	// gas price.
	zero := new(big.Int)
	gasFeeCap := tx.GasFeeCap()
	if gasFeeCap == nil || gasFeeCap.Cmp(zero) <= 0 {
		return nil, fmt.Errorf("Failed to parse gas fee cap from tx %s", txHash)
	}
	gasFeeCapGwei, err := dexeth.WeiToGweiUint64(gasFeeCap)
	if err != nil {
		return nil, fmt.Errorf("unable to convert gas fee cap: %v", err)
	}

	gasTipCap := tx.GasTipCap()
	if gasTipCap == nil || gasTipCap.Cmp(zero) <= 0 {
		return nil, fmt.Errorf("Failed to parse gas tip cap from tx %s", txHash)
	}
	gasTipCapGwei, err := dexeth.WeiToGweiUint64(gasTipCap)
	if err != nil {
		return nil, fmt.Errorf("unable to convert gas tip cap: %v", err)
	}

	return &baseCoin{
		backend:      be,
		vector:       vector,
		gasFeeCap:    gasFeeCapGwei,
		gasTipCap:    gasTipCapGwei,
		txHash:       txHash,
		value:        tx.Value(),
		txData:       tx.Data(),
		serializedTx: serializedTx,
		contractVer:  contractVer,
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
	status, err := c.backend.node.status(ctx, c.backend.assetID, c.vector)
	if err != nil {
		return -1, err
	}

	// Uninitiated state is zero confs. It could still be in mempool.
	// It is important to only trust confirmations according to the
	// swap contract. Until there are confirmations we cannot be sure
	// that initiation happened successfully.
	if status.Step == dexeth.SSNone {
		// Assume the tx still has a chance of being mined.
		return 0, nil
	}
	bn, err := c.backend.node.blockNumber(ctx)
	if err != nil {
		return 0, fmt.Errorf("unable to fetch block number: %v", err)
	}
	if status.Step == dexeth.SSInitiated {
		return int64(bn - status.BlockHeight + 1), nil
	}
	// Redeemed or refunded. Have to check the tx.
	return c.backend.txConfirmations(ctx, c.txHash)
}

func (c *redeemCoin) Confirmations(ctx context.Context) (int64, error) {
	status, err := c.backend.node.status(ctx, c.backend.assetID, c.vector)
	if err != nil {
		return -1, err
	}

	// If swap is in None state, then the redemption can't possibly
	// succeed as the swap must already be in the Initialized state
	// to redeem. If the swap is in the Refunded state, then the
	// redemption either failed or never happened.
	if status.Step == dexeth.SSNone || status.Step == dexeth.SSRefunded {
		return -1, fmt.Errorf("redemption in failed state with swap at %s state", status.Step)
	}

	// If swap is in the Initiated state, the redemption may be
	// unmined.
	if status.Step == dexeth.SSInitiated {
		// Assume the tx still has a chance of being mined.
		return 0, nil
	}

	return c.backend.txConfirmations(ctx, c.txHash)
}

func (c *redeemCoin) Value() uint64 { return 0 }

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

// FeeRate returns the gas rate, in gwei/gas. It is set in initialization of
// the swapCoin.
func (c *baseCoin) FeeRate() uint64 {
	return c.gasFeeCap
}

// Value returns the value of one swap in order to validate during processing.
func (c *swapCoin) Value() uint64 {
	return c.vector.Value
}
