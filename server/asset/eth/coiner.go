// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package eth

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"decred.org/dcrdex/dex"
	dexeth "decred.org/dcrdex/dex/networks/eth"
	"decred.org/dcrdex/dex/networks/eth/contracts/entrypoint"
	"decred.org/dcrdex/server/asset"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
)

var _ asset.Coin = (*swapCoin)(nil)
var _ asset.Coin = (*redeemCoin)(nil)

type baseCoin struct {
	tokenAddr    common.Address
	backend      *AssetBackend
	locator      []byte
	gasFeeCap    *big.Int
	gasTipCap    *big.Int
	txHash       common.Hash
	value        uint64
	txData       []byte
	serializedTx []byte
	contractVer  uint32
	isUserOp     bool
	userOpHash   common.Hash
}

type swapCoin struct {
	*baseCoin
	vector *dexeth.SwapVector
}

type redeemCoin struct {
	*baseCoin
	secret [32]byte
	// vector *dexeth.SwapVector
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

	if bc.isUserOp {
		return nil, fmt.Errorf("user op coin not supported")
	}

	var sum uint64
	var vector *dexeth.SwapVector
	switch bc.contractVer {
	case 0:
		var secretHash [32]byte
		copy(secretHash[:], bc.locator)

		inits, err := dexeth.ParseInitiateDataV0(bc.txData)
		if err != nil {
			return nil, fmt.Errorf("unable to parse v0 initiate call data: %v", err)
		}

		init, ok := inits[secretHash]
		if !ok {
			return nil, fmt.Errorf("tx %v does not contain v0 initiation with secret hash %x", bc.txHash, secretHash[:])
		}
		for _, in := range inits {
			sum = +be.atomize(in.Value)
		}

		vector = &dexeth.SwapVector{
			// From: ,
			To:         init.Participant,
			Value:      init.Value,
			SecretHash: secretHash,
			LockTime:   uint64(init.LockTime.Unix()),
		}
	case 1:
		contractVector, err := dexeth.ParseV1Locator(bc.locator)
		if err != nil {
			return nil, fmt.Errorf("contract data vector decoding error: %w", err)
		}
		tokenAddr, txVectors, err := dexeth.ParseInitiateDataV1(bc.txData)
		if err != nil {
			return nil, fmt.Errorf("unable to parse v1 initiate call data: %v", err)
		}
		if tokenAddr != be.tokenAddr {
			return nil, fmt.Errorf("token address in init tx data is incorrect. %s != %s", tokenAddr, be.tokenAddr)
		}
		txVector, ok := txVectors[contractVector.SecretHash]
		if !ok {
			return nil, fmt.Errorf("tx %v does not contain v1 initiation with vector %s", bc.txHash, contractVector)
		}
		if !dexeth.CompareVectors(contractVector, txVector) {
			return nil, fmt.Errorf("contract and transaction vectors do not match. %+v != %+v", contractVector, txVector)
		}
		vector = txVector
		if vector.SecretHash == refundRecordHash {
			return nil, errors.New("illegal secret hash (refund record hash)")
		}
		for _, v := range txVectors {
			sum += be.atomize(v.Value)
		}
	}

	if be.assetID == BipID && bc.value < sum {
		return nil, fmt.Errorf("tx %s value < sum of inits. %d < %d", bc.txHash, bc.value, sum)
	}

	return &swapCoin{
		baseCoin: bc,
		vector:   vector,
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
		contractVer, locator, err := dexeth.DecodeContractData(contractData)
		if err != nil {
			return nil, err
		}
		if be.contractVer != contractVer {
			return nil, fmt.Errorf("wrong contract version for %s. wanted %d, got %d", dex.BipIDSymbol(be.assetID), be.contractVer, contractVer)
		}

		status, vec, err := be.node.statusAndVector(be.ctx, be.assetID, locator)
		if err != nil {
			return nil, err
		}
		be.log.Warnf("redeem coin with ID %x for %s was not found", coinID, vec)
		if status.Step != dexeth.SSRedeemed {
			return nil, asset.CoinNotFoundError
		}

		bc = &baseCoin{
			tokenAddr:   be.tokenAddr,
			backend:     be,
			locator:     locator,
			contractVer: contractVer,
		}
		return &redeemCoin{
			baseCoin: bc,
			secret:   status.Secret,
			// vector:   vec,
		}, nil
	} else if err != nil {
		return nil, err
	}

	if bc.value != 0 {
		return nil, fmt.Errorf("expected tx value of zero for redeem but got: %d", bc.value)
	}

	var secret [32]byte
	switch bc.contractVer {
	case 0:
		if bc.isUserOp {
			return nil, fmt.Errorf("user op coin not supported in v0")
		}
		secretHash, err := dexeth.ParseV0Locator(bc.locator)
		if err != nil {
			return nil, fmt.Errorf("error parsing vector from v0 locator '%x': %w", bc.locator, err)
		}
		redemptions, err := dexeth.ParseRedeemDataV0(bc.txData)
		if err != nil {
			return nil, fmt.Errorf("unable to parse v0 redemption call data: %v", err)
		}
		redemption, ok := redemptions[secretHash]
		if !ok {
			return nil, fmt.Errorf("tx %v does not contain redemption for v0 secret hash %x", bc.txHash, secretHash[:])
		}
		secret = redemption.Secret
	case 1:
		vector, err := dexeth.ParseV1Locator(bc.locator)
		if err != nil {
			return nil, fmt.Errorf("error parsing vector from v1 locator '%x': %w", bc.locator, err)
		}
		tokenAddr, redemptions, err := dexeth.ParseRedeemDataV1(bc.txData)
		if err != nil {
			return nil, fmt.Errorf("unable to parse v1 redemption call data: %v", err)
		}
		if tokenAddr != be.tokenAddr {
			return nil, fmt.Errorf("token address in redeem tx data is incorrect. %s != %s", tokenAddr, be.tokenAddr)
		}
		redemption, ok := redemptions[vector.SecretHash]
		if !ok {
			return nil, fmt.Errorf("tx %v does not contain redemption for v1 vector %s", bc.txHash, vector)
		}
		if !dexeth.CompareVectors(redemption.Contract, vector) {
			return nil, fmt.Errorf("encoded vector %q doesn't match expected vector %q", redemption.Contract, vector)
		}
		secret = redemption.Secret
	default:
		return nil, fmt.Errorf("version %d redeem coin not supported", bc.contractVer)
	}

	return &redeemCoin{
		baseCoin: bc,
		secret:   secret,
	}, nil
}

// userOpBaseCoin creates a baseCoin from a user operation.
func (be *AssetBackend) userOpBaseCoin(txHash, userOpHash common.Hash, contractData []byte) (*baseCoin, error) {
	contractVer, locator, err := dexeth.DecodeContractData(contractData)
	if err != nil {
		return nil, err
	}

	tx, isMempool, err := be.node.transaction(be.ctx, txHash)
	if err != nil {
		if errors.Is(err, ethereum.NotFound) {
			return nil, asset.CoinNotFoundError
		}
		return nil, fmt.Errorf("unable to fetch transaction: %v", err)
	}
	if isMempool {
		return nil, asset.CoinNotFoundError
	}

	if *tx.To() != be.entryPointAddress {
		return nil, fmt.Errorf("unknown entrypoint address: %s", tx.To().String())
	}

	handleOpsData, err := dexeth.ParseHandleOpsData(tx.Data())
	if err != nil {
		return nil, fmt.Errorf("unable to parse handle ops data: %v", err)
	}

	var userOp *entrypoint.UserOperation
	for _, op := range handleOpsData {
		hash, err := dexeth.HashUserOp(op, be.entryPointAddress, big.NewInt(int64(be.evmChainID)))
		if err != nil {
			return nil, fmt.Errorf("unable to hash user op: %v", err)
		}
		if hash == userOpHash {
			userOp = &op
			break
		}
	}
	if userOp == nil {
		return nil, fmt.Errorf("user op not found in tx %s", txHash)
	}

	return &baseCoin{
		backend:     be,
		tokenAddr:   be.tokenAddr,
		locator:     locator,
		txHash:      txHash,
		isUserOp:    true,
		txData:      userOp.CallData,
		contractVer: contractVer,

		// The following is not populated, but since user ops are only used
		// for redemptions, and the following data is not used in confirming
		// redemptions, it is not necessary to populate it.
		gasFeeCap:    new(big.Int),
		gasTipCap:    new(big.Int),
		value:        0,
		serializedTx: []byte{},
	}, nil
}

// txBaseCoin creates a baseCoin from an ethereum transaction.
func (be *AssetBackend) txBaseCoin(txHash common.Hash, contractData []byte) (*baseCoin, error) {
	tx, _, err := be.node.transaction(be.ctx, txHash)
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

	contractVer, locator, err := dexeth.DecodeContractData(contractData)
	if err != nil {
		return nil, err
	}

	contractAddr := tx.To()
	if *contractAddr != be.contractAddr {
		return nil, fmt.Errorf("contract address is not supported: %v", contractAddr)
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
		return nil, fmt.Errorf("failed to parse gas fee cap from tx %s", txHash)
	}

	gasTipCap := tx.GasTipCap()
	if gasTipCap == nil || gasTipCap.Cmp(zero) <= 0 {
		return nil, fmt.Errorf("failed to parse gas tip cap from tx %s", txHash)
	}

	return &baseCoin{
		backend:      be,
		tokenAddr:    be.tokenAddr,
		locator:      locator,
		gasFeeCap:    gasFeeCap,
		gasTipCap:    gasTipCap,
		txHash:       txHash,
		value:        be.atomize(tx.Value()),
		txData:       tx.Data(),
		serializedTx: serializedTx,
		contractVer:  contractVer,
	}, nil
}

// The baseCoin is basic tx and swap contract data.
func (be *AssetBackend) baseCoin(coinID []byte, contractData []byte) (*baseCoin, error) {
	ethCoinID, err := dexeth.DecodeCoinID(coinID)
	if err != nil {
		return nil, err
	}

	if ethCoinID.IsUserOp {
		return be.userOpBaseCoin(ethCoinID.TxHash, ethCoinID.UserOpHash, contractData)
	}

	return be.txBaseCoin(ethCoinID.TxHash, contractData)
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
	status, checkVec, err := c.backend.node.statusAndVector(ctx, c.backend.assetID, c.locator)
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
	// Any other swap state is ok. We are sure that initialization
	// happened.

	// The swap initiation transaction has some number of
	// confirmations, and we are sure the secret hash belongs to
	// this swap. Assert that the value, receiver, and locktime are
	// as expected.
	switch c.contractVer {
	case 0:
		if checkVec.Value.Cmp(c.vector.Value) != 0 {
			return -1, fmt.Errorf("tx data swap val (%d) does not match contract value (%d)",
				c.vector.Value, checkVec.Value)
		}
		if checkVec.To != c.vector.To {
			return -1, fmt.Errorf("tx data participant %q does not match contract value %q",
				c.vector.To, checkVec.To)
		}
		if checkVec.LockTime != c.vector.LockTime {
			return -1, fmt.Errorf("expected swap locktime (%d) does not match expected (%d)",
				c.vector.LockTime, checkVec.LockTime)
		}
	case 1:
		if err := setV1StatusBlockHeight(ctx, c.backend.node, status, c.baseCoin); err != nil {
			return 0, err
		}
	}

	bn, err := c.backend.node.blockNumber(ctx)
	if err != nil {
		return 0, fmt.Errorf("unable to fetch block number: %v", err)
	}
	return int64(bn - status.BlockHeight + 1), nil
}

func setV1StatusBlockHeight(ctx context.Context, node ethFetcher, status *dexeth.SwapStatus, bc *baseCoin) error {
	switch status.Step {
	case dexeth.SSNone, dexeth.SSInitiated:
	case dexeth.SSRedeemed, dexeth.SSRefunded:
		// No block height for redeemed or refunded version 1 contracts,
		// only SSInitiated.
		r, err := node.transactionReceipt(ctx, bc.txHash)
		if err != nil {
			return err
		}
		status.BlockHeight = r.BlockNumber.Uint64()
	}
	return nil
}

func (c *redeemCoin) Confirmations(ctx context.Context) (int64, error) {
	status, err := c.backend.node.status(ctx, c.backend.assetID, c.tokenAddr, c.locator)
	if err != nil {
		return -1, err
	}

	// There should be no need to check the counter party, or value
	// as a swap with a specific secret hash that has been redeemed
	// wouldn't have been redeemed without ensuring the initiator
	// is the expected address and value was also as expected. Also
	// not validating the locktime, as the swap is redeemed and
	// locktime no longer relevant.
	if status.Step == dexeth.SSRedeemed {
		if c.contractVer == 1 {
			if err := setV1StatusBlockHeight(ctx, c.backend.node, status, c.baseCoin); err != nil {
				return -1, err
			}
		}
		bn, err := c.backend.node.blockNumber(ctx)
		if err != nil {
			return 0, fmt.Errorf("unable to fetch block number: %v", err)
		}
		return int64(bn - status.BlockHeight + 1), nil
	}
	// If swap is in the Initiated state, the redemption may be
	// unmined.
	if status.Step == dexeth.SSInitiated {
		// Assume the tx still has a chance of being mined.
		return 0, nil
	}
	// If swap is in None state, then the redemption can't possibly
	// succeed as the swap must already be in the Initialized state
	// to redeem. If the swap is in the Refunded state, then the
	// redemption either failed or never happened.
	return -1, fmt.Errorf("redemption in failed state with swap at %s state", status.Step)
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
	return dexeth.WeiToGweiCeil(c.gasFeeCap)
}

// Value returns the value of one swap in order to validate during processing.
func (c *swapCoin) Value() uint64 {
	return c.backend.atomize(c.vector.Value)
}
