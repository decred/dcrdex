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

type swapCoinType uint8

const (
	sctInit swapCoinType = iota
	sctRedeem
)

func (sct swapCoinType) String() string {
	switch sct {
	case sctInit:
		return "init"
	case sctRedeem:
		return "redeem"
	default:
		return "invalid type"
	}
}

var _ asset.Coin = (*swapCoin)(nil)

type swapCoin struct {
	backend                    *Backend
	contractAddr, counterParty common.Address
	secret, secretHash         [32]byte
	value                      uint64
	gasPrice                   uint64
	txHash                     common.Hash
	txid                       string
	locktime                   int64
	sct                        swapCoinType
}

// newSwapCoin creates a new swapCoin that stores and retrieves info about a
// swap. It requires a coinID that is a txid type of the initial transaction
// initializing or redeeming the swap. A txid type and not a swap type is
// required because the contract will give us no useful information about the
// swap before it is mined. Having the initial transaction allows us to track
// it in the mempool. It also tells us all the data we need to confirm a tx
// will do what we expect if mined and satisfies contract constraints. These
// fields are verified the first time the Confirmations method is called, and
// an error is returned then if something is different than expected. As such,
// the swapCoin expects Confirmations to be called with confirmations
// available at least once before the swap be trusted for swap initializations.
func (backend *Backend) newSwapCoin(coinID []byte, contractData []byte, sct swapCoinType) (*swapCoin, error) {
	switch sct {
	case sctInit, sctRedeem:
	default:
		return nil, fmt.Errorf("unknown swapCoin type: %d", sct)
	}

	txHash, err := dexeth.DecodeCoinID(coinID)
	if err != nil {
		return nil, err
	}
	tx, _, err := backend.node.transaction(backend.rpcCtx, txHash)
	if err != nil {
		if errors.Is(err, ethereum.NotFound) {
			return nil, asset.CoinNotFoundError
		}
		return nil, fmt.Errorf("unable to fetch transaction: %v", err)
	}

	txdata := tx.Data()
	// Transactions that call contract functions must have extra data.
	if len(txdata) == 0 {
		return nil, errors.New("tx calling contract function has no extra data")
	}

	contractVer, secretHash, err := dexeth.DecodeContractData(contractData)
	if err != nil {
		return nil, err
	}
	if contractVer != version {
		return nil, fmt.Errorf("contract version %d not supported, only %d", contractVer, version)
	}
	contractAddr := tx.To()
	if *contractAddr != backend.contractAddr {
		return nil, fmt.Errorf("contract address is not supported: %v", contractAddr)
	}

	// Parse the call data for the counter-party address and lock time for an
	// init txn, and the secret key for a redeem txn.
	var (
		counterParty = new(common.Address)
		secret       [32]byte
		locktime     int64
	)

	switch sct {
	case sctInit:
		initiations, err := dexeth.ParseInitiateData(txdata, contractVer)
		if err != nil {
			return nil, fmt.Errorf("unable to parse initiate call data: %v", err)
		}
		var initiation *dexeth.Initiation
		for _, init := range initiations {
			// contractData points us to the init of interest.
			if init.SecretHash == secretHash {
				initiation = init
				break
			}
		}
		if initiation == nil {
			return nil, fmt.Errorf("tx %v does not contain initiation with secret hash %x",
				txHash, secretHash)
		}
		counterParty = &initiation.Participant
		locktime = initiation.LockTime.Unix()
	case sctRedeem:
		redemptions, err := dexeth.ParseRedeemData(txdata, contractVer)
		if err != nil {
			return nil, fmt.Errorf("unable to parse redemption call data: %v", err)
		}
		var redemption *dexeth.Redemption
		for _, redeem := range redemptions {
			// contractData points us to the redeem of interest.
			if redeem.SecretHash == secretHash {
				redemption = redeem
				break
			}
		}
		if redemption == nil {
			return nil, fmt.Errorf("tx %v does not contain redemption with secret hash %x",
				txHash, secretHash)
		}
		secret = redemption.Secret
	}

	// Gas price is not stored in the swap, and is used to determine if the
	// initialization transaction could take a long time to be mined. A
	// transaction with a very low gas price may need to be resent with a
	// higher price.
	gasPrice, err := dexeth.ToGwei(tx.GasPrice())
	if err != nil {
		return nil, fmt.Errorf("unable to convert gas price: %v", err)
	}

	// Value is stored in the swap with the initialization transaction.
	value, err := dexeth.ToGwei(tx.Value())
	if err != nil {
		return nil, fmt.Errorf("unable to convert value: %v", err)
	}

	// For redemptions, the transaction should move no value.
	if sct == sctRedeem && value != 0 {
		return nil, fmt.Errorf("expected swapCoin value of zero for redeem but got: %d", value)
	}

	return &swapCoin{
		backend:      backend,
		contractAddr: *contractAddr, // non-zero for init
		secret:       secret,        // non-zero for redeem
		secretHash:   secretHash,
		value:        value,
		gasPrice:     gasPrice,
		txHash:       txHash,
		txid:         txHash.Hex(), // == String(), with 0x prefix
		counterParty: *counterParty,
		locktime:     locktime, // non-zero for init
		sct:          sct,
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
func (c *swapCoin) Confirmations(_ context.Context) (int64, error) {
	swap, err := c.backend.node.swap(c.backend.rpcCtx, c.secretHash)
	if err != nil {
		return -1, err
	}

	switch c.sct {
	case sctRedeem:
		// There should be no need to check the counter party, or value
		// as a swap with a specific secret hash that has been redeemed
		// wouldn't have been redeemed without ensuring the initiator
		// is the expected address and value was also as expected. Also
		// not validating the locktime, as the swap is redeemed and
		// locktime no longer relevant.
		ss := dexeth.SwapStep(swap.State)
		if ss == dexeth.SSRedeemed {
			// While not completely accurate, we know that if the
			// swap is redeemed the redemption has at least one
			// confirmation.
			return 1, nil
		}
		// If swap is in the Initiated state, the transaction may be
		// unmined.
		if ss == dexeth.SSInitiated {
			// Assume the tx still has a chance of being mined.
			return 0, nil
		}
		// If swap is in None state, then the redemption can't possibly
		// succeed as the swap must already be in the Initialized state
		// to redeem. If the swap is in the Refunded state, then the
		// redemption either failed or never happened.
		return -1, fmt.Errorf("redemption in failed state with swap at %s state", ss)

	case sctInit:
		// Uninitiated state is zero confs. It could still be in mempool.
		// It is important to only trust confirmations according to the
		// swap contract. Until there are confirmations we cannot be sure
		// that initiation happened successfully.
		if dexeth.SwapStep(swap.State) == dexeth.SSNone {
			// Assume the tx still has a chance of being mined.
			return 0, nil
		}
		// Any other swap state is ok. We are sure that initialization
		// happened.

		// The swap initiation transaction has some number of
		// confirmations, and we are sure the secret hash belongs to
		// this swap. Assert that the value, receiver, and locktime are
		// as expected.
		value, err := dexeth.ToGwei(new(big.Int).Set(swap.Value))
		if err != nil {
			return -1, fmt.Errorf("unable to convert value: %v", err)
		}
		if value != c.value {
			return -1, fmt.Errorf("expected swap val (%dgwei) does not match expected (%dgwei)",
				c.value, value)
		}
		if swap.Participant != c.counterParty {
			return -1, fmt.Errorf("expected swap participant %q does not match expected %q",
				c.counterParty, swap.Participant)
		}
		if !swap.RefundBlockTimestamp.IsInt64() {
			return -1, errors.New("swap locktime is larger than expected")
		}
		locktime := swap.RefundBlockTimestamp.Int64()
		if locktime != c.locktime {
			return -1, fmt.Errorf("expected swap locktime (%d) does not match expected (%d)",
				c.locktime, locktime)
		}

		bn, err := c.backend.node.blockNumber(c.backend.rpcCtx)
		if err != nil {
			return 0, fmt.Errorf("unable to fetch block number: %v", err)
		}
		return int64(bn - swap.InitBlockNumber.Uint64()), nil
	}

	return -1, fmt.Errorf("unsupported swap type for confirmations: %d", c.sct)
}

// ID is the swap's coin ID.
func (c *swapCoin) ID() []byte {
	return c.txHash.Bytes() // c.txHash[:]
}

// TxID is the original init transaction txid.
func (c *swapCoin) TxID() string {
	return c.txid
}

// String is a human readable representation of the swap coin.
func (c *swapCoin) String() string {
	return fmt.Sprintf("%v (%s)", c.txid, c.sct)
}

// Value is the amount paid to the swap, set in initialization. Always zero for
// redemptions.
func (c *swapCoin) Value() uint64 {
	return c.value
}

// FeeRate returns the gas rate, in gwei/gas. It is set in initialization of
// the swapCoin.
func (c *swapCoin) FeeRate() uint64 {
	return c.gasPrice
}
