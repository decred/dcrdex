// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build lgpl

package eth

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math/big"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/networks/erc20"
	erc20v0 "decred.org/dcrdex/dex/networks/erc20/contracts/v0"
	erc20v1 "decred.org/dcrdex/dex/networks/erc20/contracts/v1"
	dexeth "decred.org/dcrdex/dex/networks/eth"
	swapv0 "decred.org/dcrdex/dex/networks/eth/contracts/v0"
	swapv1 "decred.org/dcrdex/dex/networks/eth/contracts/v1"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// contractor is a translation layer between the abigen bindings and the DEX app.
// The intention is that if a new contract is implemented, the contractor
// interface itself will not require any updates.
type contractor interface {
	status(context.Context, *dex.SwapContractDetails) (dexeth.SwapStep, [32]byte, uint32, error)
	initiate(*bind.TransactOpts, []*dex.SwapContractDetails) (*types.Transaction, error)
	redeem(txOpts *bind.TransactOpts, redeems []*asset.Redemption) (*types.Transaction, error)
	refund(*bind.TransactOpts, *dex.SwapContractDetails) (*types.Transaction, error)
	estimateInitGas(ctx context.Context, n int) (uint64, error)
	estimateRedeemGas(ctx context.Context, secrets [][32]byte, contracts []*dex.SwapContractDetails) (uint64, error)
	estimateRefundGas(ctx context.Context, contract *dex.SwapContractDetails) (uint64, error)
	isRedeemable(secret [32]byte, contract *dex.SwapContractDetails) (bool, error)
	// value checks the incoming or outgoing contract value. This is just the
	// one of redeem, refund, or initiate values. It is not an error if the
	// transaction does not pay to the contract, and the values returned in that
	// case will always be zero.
	value(context.Context, *types.Transaction) (incoming, outgoing uint64, err error)
	isRefundable(contract *dex.SwapContractDetails) (bool, error)
}

// tokenContractor interacts with an ERC20 token contract and a token swap
// contract.
type tokenContractor interface {
	contractor
	tokenAddress() common.Address
	balance(context.Context) (*big.Int, error)
	allowance(context.Context) (*big.Int, error)
	approve(*bind.TransactOpts, *big.Int) (*types.Transaction, error)
	estimateApproveGas(context.Context, *big.Int) (uint64, error)
	transfer(*bind.TransactOpts, common.Address, *big.Int) (*types.Transaction, error)
	estimateTransferGas(context.Context, *big.Int) (uint64, error)
}

type contractorConstructor func(net dex.Network, addr common.Address, ec bind.ContractBackend) (contractor, error)
type tokenContractorConstructor func(net dex.Network, assetID uint32, acctAddr common.Address, ec bind.ContractBackend) (tokenContractor, error)

// contractV0 is the interface common to a version 0 swap contract or version 0
// token swap contract.
type contractV0 interface {
	Initiate(opts *bind.TransactOpts, initiations []swapv0.ETHSwapInitiation) (*types.Transaction, error)
	Redeem(opts *bind.TransactOpts, redemptions []swapv0.ETHSwapRedemption) (*types.Transaction, error)
	Swap(opts *bind.CallOpts, secretHash [32]byte) (swapv0.ETHSwapSwap, error)
	Refund(opts *bind.TransactOpts, secretHash [32]byte) (*types.Transaction, error)
	IsRedeemable(opts *bind.CallOpts, secretHash [32]byte, secret [32]byte) (bool, error)
	IsRefundable(opts *bind.CallOpts, secretHash [32]byte) (bool, error)
}

var _ contractV0 = (*swapv0.ETHSwap)(nil)

type contractV1 interface {
	Initiate(opts *bind.TransactOpts, contracts []swapv1.ETHSwapContract) (*types.Transaction, error)
	Redeem(opts *bind.TransactOpts, redemptions []swapv1.ETHSwapRedemption) (*types.Transaction, error)
	State(opts *bind.CallOpts, c swapv1.ETHSwapContract) (swapv1.ETHSwapRecord, error)
	Refund(opts *bind.TransactOpts, c swapv1.ETHSwapContract) (*types.Transaction, error)
	IsRedeemable(opts *bind.CallOpts, c swapv1.ETHSwapContract) (bool, error)
}

var _ contractV1 = (*swapv1.ETHSwap)(nil)

// contractorV0 is the contractor for contract version 0.
// Redeem and Refund methods of swapv0.ETHSwap already have suitable return types.
type contractorV0 struct {
	contractV0   // *swapv0.ETHSwap
	abi          *abi.ABI
	cb           bind.ContractBackend
	contractAddr common.Address
	acctAddr     common.Address
	isToken      bool
	evmify       func(uint64) *big.Int
	atomize      func(*big.Int) uint64
}

var _ contractor = (*contractorV0)(nil)

// newV0Contractor is the constructor for a version 0 ETH swap contract. For
// token swap contracts, use newV0TokenContractor to construct a
// tokenContractorV0.
func newV0Contractor(net dex.Network, acctAddr common.Address, cb bind.ContractBackend) (contractor, error) {
	contractAddr, exists := dexeth.ContractAddresses[0][net]
	if !exists || contractAddr == (common.Address{}) {
		return nil, fmt.Errorf("no contract address for version 0, net %s", net)
	}
	c, err := swapv0.NewETHSwap(contractAddr, cb)
	if err != nil {
		return nil, err
	}

	return &contractorV0{
		contractV0:   c,
		abi:          dexeth.ABIs[0],
		cb:           cb,
		contractAddr: contractAddr,
		acctAddr:     acctAddr,
		evmify:       dexeth.GweiToWei,
		atomize:      dexeth.WeiToGwei,
	}, nil
}

// initiate sends the initiations to the swap contract's initiate function.
func (c *contractorV0) initiate(txOpts *bind.TransactOpts, details []*dex.SwapContractDetails) (*types.Transaction, error) {
	inits := make([]swapv0.ETHSwapInitiation, 0, len(details))
	secrets := make(map[[32]byte]bool, len(details))

	for _, deets := range details {
		if len(deets.SecretHash) != dexeth.SecretHashSize {
			return nil, fmt.Errorf("wrong secret hash length. wanted %d, got %d", dexeth.SecretHashSize, len(deets.SecretHash))
		}

		var secretHash [32]byte
		copy(secretHash[:], deets.SecretHash)

		if secrets[secretHash] {
			return nil, fmt.Errorf("secret hash %s is a duplicate", deets.SecretHash)
		}
		secrets[secretHash] = true

		if !common.IsHexAddress(deets.To) {
			return nil, fmt.Errorf("%q is not an address", deets.To)
		}
		inits = append(inits, swapv0.ETHSwapInitiation{
			RefundTimestamp: big.NewInt(int64(deets.LockTime)),
			SecretHash:      secretHash,
			Participant:     common.HexToAddress(deets.To),
			Value:           c.evmify(deets.Value),
		})
	}

	return c.contractV0.Initiate(txOpts, inits)
}

// redeem sends the redemptions to the swap contracts redeem method.
func (c *contractorV0) redeem(txOpts *bind.TransactOpts, redemptions []*asset.Redemption) (*types.Transaction, error) {
	redemps := make([]swapv0.ETHSwapRedemption, 0, len(redemptions))
	secretHashes := make(map[[32]byte]bool, len(redemptions))
	for _, r := range redemptions {
		secretB, secretHashB := r.Secret, r.SwapDetails.SecretHash
		if len(secretB) != 32 || len(secretHashB) != 32 {
			return nil, fmt.Errorf("invalid secret and/or secret hash sizes, %d and %d", len(secretB), len(secretHashB))
		}
		var secret, secretHash [32]byte
		copy(secret[:], secretB)
		copy(secretHash[:], secretHashB)
		if secretHashes[secretHash] {
			return nil, fmt.Errorf("duplicate secret hash %x", secretHash[:])
		}
		secretHashes[secretHash] = true

		redemps = append(redemps, swapv0.ETHSwapRedemption{
			Secret:     secret,
			SecretHash: secretHash,
		})
	}
	return c.contractV0.Redeem(txOpts, redemps)
}

// swap retrieves the swap info from the read-only swap method.
func (c *contractorV0) swap(ctx context.Context, secretHash [32]byte) (*dexeth.SwapState, error) {
	callOpts := &bind.CallOpts{
		From:    c.acctAddr,
		Context: ctx,
	}
	state, err := c.contractV0.Swap(callOpts, secretHash)
	if err != nil {
		return nil, err
	}

	return &dexeth.SwapState{
		BlockHeight: state.InitBlockNumber.Uint64(),
		LockTime:    time.Unix(state.RefundBlockTimestamp.Int64(), 0),
		Secret:      state.Secret,
		Initiator:   state.Initiator,
		Participant: state.Participant,
		Value:       state.Value,
		State:       dexeth.SwapStep(state.State),
	}, nil
}

// swap retrieves the swap info from the read-only swap method.
func (c *contractorV0) status(ctx context.Context, contract *dex.SwapContractDetails) (step dexeth.SwapStep, secret [32]byte, blockNumber uint32, err error) {
	var secretHash [32]byte
	copy(secretHash[:], contract.SecretHash)
	swap, err := c.swap(ctx, secretHash)
	if err != nil {
		return
	}
	return swap.State, swap.Secret, uint32(swap.BlockHeight), nil
}

// refund issues the refund command to the swap contract. Use isRefundable first
// to ensure the refund will be accepted.
func (c *contractorV0) refund(txOpts *bind.TransactOpts, contract *dex.SwapContractDetails) (*types.Transaction, error) {
	var secretHash [32]byte
	copy(secretHash[:], contract.SecretHash)
	return c.contractV0.Refund(txOpts, secretHash)
}

// isRedeemable exposes the isRedeemable method of the swap contract.
func (c *contractorV0) isRedeemable(secret [32]byte, contract *dex.SwapContractDetails) (bool, error) {
	var secretHash [32]byte
	copy(secretHash[:], contract.SecretHash)
	return c.contractV0.IsRedeemable(&bind.CallOpts{From: c.acctAddr}, secretHash, secret)
}

// isRefundable exposes the isRefundable method of the swap contract.
func (c *contractorV0) isRefundable(contract *dex.SwapContractDetails) (bool, error) {
	var secretHash [32]byte
	copy(secretHash[:], contract.SecretHash)
	return c.contractV0.IsRefundable(&bind.CallOpts{From: c.acctAddr}, secretHash)
}

// estimateRedeemGas estimates the gas used to redeem. The secret hashes
// supplied must reference existing swaps, so this method can't be used until
// the swap is initiated.
func (c *contractorV0) estimateRedeemGas(ctx context.Context, secrets [][32]byte, _ []*dex.SwapContractDetails) (uint64, error) {
	redemps := make([]swapv0.ETHSwapRedemption, 0, len(secrets))
	for _, secret := range secrets {
		redemps = append(redemps, swapv0.ETHSwapRedemption{
			Secret:     secret,
			SecretHash: sha256.Sum256(secret[:]),
		})
	}
	return c.estimateGas(ctx, nil, "redeem", redemps)
}

// estimateRefundGas estimates the gas used to refund. The secret hashes
// supplied must reference existing swaps that are refundable, so this method
// can't be used until the swap is initiated and the lock time has expired.
func (c *contractorV0) estimateRefundGas(ctx context.Context, deets *dex.SwapContractDetails) (uint64, error) {
	var secretHash [32]byte
	copy(secretHash[:], deets.SecretHash)
	return c.estimateGas(ctx, nil, "refund", secretHash)
}

// estimateInitGas estimates the gas used to initiate n generic swaps. The
// account must have a balance of at least n wei, as well as the eth balance
// to cover fees.
func (c *contractorV0) estimateInitGas(ctx context.Context, n int) (uint64, error) {
	initiations := make([]swapv0.ETHSwapInitiation, 0, n)
	for j := 0; j < n; j++ {
		var secretHash [32]byte
		copy(secretHash[:], encode.RandomBytes(32))
		initiations = append(initiations, swapv0.ETHSwapInitiation{
			RefundTimestamp: big.NewInt(1),
			SecretHash:      secretHash,
			Participant:     c.acctAddr,
			Value:           big.NewInt(1),
		})
	}

	var value *big.Int
	if !c.isToken {
		value = big.NewInt(int64(n))
	}

	return c.estimateGas(ctx, value, "initiate", initiations)
}

// estimateGas estimates the gas used to interact with the swap contract.
func (c *contractorV0) estimateGas(ctx context.Context, value *big.Int, method string, args ...interface{}) (uint64, error) {
	return estimateGas(ctx, c.acctAddr, c.contractAddr, c.abi, c.cb, value, method, args...)
}

// value calculates the incoming or outgoing value of the transaction, excluding
// fees but including operations against the swap contract.
func (c *contractorV0) value(ctx context.Context, tx *types.Transaction) (in, out uint64, err error) {
	if *tx.To() != c.contractAddr {
		return 0, 0, nil
	}

	if v, err := c.incomingValue(ctx, tx); err != nil {
		return 0, 0, fmt.Errorf("incomingValue error: %w", err)
	} else if v > 0 {
		return v, 0, nil
	}

	return 0, c.outgoingValue(tx), nil
}

// incomingValue calculates the value being redeemed for refunded in the tx.
func (c *contractorV0) incomingValue(ctx context.Context, tx *types.Transaction) (uint64, error) {
	if redeems, err := dexeth.ParseRedeemDataV0(tx.Data()); err == nil {
		var redeemed uint64
		for _, redeem := range redeems {
			swap, err := c.contractV0.Swap(readOnlyCallOpts(ctx), redeem.SecretHash)
			if err != nil {
				return 0, fmt.Errorf("redeem swap error: %w", err)
			}
			redeemed += c.atomize(swap.Value)
		}
		return redeemed, nil
	}
	secretHash, err := dexeth.ParseRefundDataV0(tx.Data())
	if err != nil {
		return 0, nil
	}
	swap, err := c.contractV0.Swap(readOnlyCallOpts(ctx), secretHash)
	if err != nil {
		return 0, fmt.Errorf("refund swap error: %w", err)
	}
	return c.atomize(swap.Value), nil
}

// outgoingValue calculates the value sent in swaps in the tx.
func (c *contractorV0) outgoingValue(tx *types.Transaction) (swapped uint64) {
	if inits, err := dexeth.ParseInitiateDataV0(tx.Data()); err == nil {
		for _, init := range inits {
			swapped += c.atomize(init.Value)
		}
	}
	return
}

// erc20Contractor supports the ERC20 ABI. Embedded in token contractors.
type erc20Contractor struct {
	tokenContract *erc20.IERC20
	acct          common.Address
	contract      common.Address
}

// balance exposes the read-only balanceOf method of the erc20 token contract.
func (c *erc20Contractor) balance(ctx context.Context) (*big.Int, error) {
	callOpts := &bind.CallOpts{
		From:    c.acct,
		Context: ctx,
	}

	return c.tokenContract.BalanceOf(callOpts, c.acct)
}

// allowance exposes the read-only allowance method of the erc20 token contract.
func (c *erc20Contractor) allowance(ctx context.Context) (*big.Int, error) {
	callOpts := &bind.CallOpts{
		Pending: true,
		From:    c.acct,
		Context: ctx,
	}
	return c.tokenContract.Allowance(callOpts, c.acct, c.contract)
}

// approve sends an approve transaction approving the linked contract to call
// transferFrom for the specified amount.
func (c *erc20Contractor) approve(txOpts *bind.TransactOpts, amount *big.Int) (*types.Transaction, error) {
	return c.tokenContract.Approve(txOpts, c.contract, amount)
}

// transfer calls the transfer method of the erc20 token contract. Used for
// sends or withdrawals.
func (c *erc20Contractor) transfer(txOpts *bind.TransactOpts, addr common.Address, amount *big.Int) (*types.Transaction, error) {
	return c.tokenContract.Transfer(txOpts, addr, amount)
}

// tokenContractorV0 is a contractor that implements the tokenContractor
// methods, providing access to the methods of the token's ERC20 contract.
type tokenContractorV0 struct {
	*contractorV0
	*erc20Contractor
	tokenAddr common.Address
}

var _ contractor = (*tokenContractorV0)(nil)
var _ tokenContractor = (*tokenContractorV0)(nil)

// newV0TokenContractor is a contractor for version 0 erc20 token swap contract.
func newV0TokenContractor(net dex.Network, assetID uint32, acctAddr common.Address, cb bind.ContractBackend) (tokenContractor, error) {
	token, tokenAddr, swapContractAddr, err := dexeth.VersionedNetworkToken(assetID, 0, net)
	if err != nil {
		return nil, err
	}

	c, err := erc20v0.NewERC20Swap(swapContractAddr, cb)
	if err != nil {
		return nil, err
	}

	// tokenContract := bind.NewBoundContract(tokenAddr, *erc20.ERC20ABI, cb, cb, cb)
	tokenContract, err := erc20.NewIERC20(tokenAddr, cb)
	if err != nil {
		return nil, err
	}

	if boundAddr, err := c.TokenAddress(&bind.CallOpts{
		Context: context.TODO(),
	}); err != nil {
		return nil, fmt.Errorf("error reading bound token address: %w", err)
	} else if boundAddr != tokenAddr {
		return nil, fmt.Errorf("wrong bound address. expected %s, got %s", tokenAddr, boundAddr)
	}

	return &tokenContractorV0{
		contractorV0: &contractorV0{
			contractV0:   c,
			abi:          erc20.ERC20SwapABIV0,
			cb:           cb,
			contractAddr: swapContractAddr,
			acctAddr:     acctAddr,
			isToken:      true,
			evmify:       token.AtomicToEVM,
			atomize:      token.EVMToAtomic,
		},
		tokenAddr: tokenAddr,
		erc20Contractor: &erc20Contractor{
			tokenContract: tokenContract,
			acct:          acctAddr,
			contract:      swapContractAddr,
		},
	}, nil
}

// estimateApproveGas estimates the gas needed to send an approve tx.
func (c *tokenContractorV0) estimateApproveGas(ctx context.Context, amount *big.Int) (uint64, error) {
	return c.estimateGas(ctx, "approve", c.contractAddr, amount)
}

// estimateTransferGas esimates the gas needed for a transfer tx. The account
// needs to have > amount tokens to use this method.
func (c *tokenContractorV0) estimateTransferGas(ctx context.Context, amount *big.Int) (uint64, error) {
	return c.estimateGas(ctx, "transfer", c.acctAddr, amount)
}

// estimateGas estimates the gas needed for methods on the ERC20 token contract.
// For estimating methods on the swap contract, use (contractorV0).estimateGas.
func (c *tokenContractorV0) estimateGas(ctx context.Context, method string, args ...interface{}) (uint64, error) {
	return estimateGas(ctx, c.acctAddr, c.contractAddr, c.abi, c.cb, new(big.Int), method, args...)
}

// value finds incoming or outgoing value for the tx to either the swap contract
// or the erc20 token contract. For the token contract, only transfer and
// transferFrom are parsed. It is not an error if this tx is a call to another
// method of the token contract, but no values will be parsed.
func (c *tokenContractorV0) value(ctx context.Context, tx *types.Transaction) (in, out uint64, err error) {
	to := *tx.To()
	if to == c.contractAddr {
		return c.contractorV0.value(ctx, tx)
	}
	if to != c.tokenAddr {
		return 0, 0, nil
	}

	// Consider removing. We'll never be sending transferFrom transactions
	// directly.
	if sender, _, value, err := erc20.ParseTransferFromData(tx.Data()); err == nil && sender == c.acctAddr {
		return 0, c.atomize(value), nil
	}

	if _, value, err := erc20.ParseTransferData(tx.Data()); err == nil {
		return 0, c.atomize(value), nil
	}

	return 0, 0, nil
}

// tokenAddress exposes the token_address immutable address of the token-bound
// swap contract.
func (c *tokenContractorV0) tokenAddress() common.Address {
	return c.tokenAddr
}

type contractorV1 struct {
	contractV1
	abi          *abi.ABI
	net          dex.Network
	contractAddr common.Address
	acctAddr     common.Address
	cb           bind.ContractBackend
	isToken      bool
	evmify       func(uint64) *big.Int
	atomize      func(*big.Int) uint64
}

var _ contractor = (*contractorV1)(nil)

func newV1Contractor(net dex.Network, acctAddr common.Address, cb bind.ContractBackend) (contractor, error) {
	contractAddr, exists := dexeth.ContractAddresses[1][net]
	if !exists || contractAddr == (common.Address{}) {
		return nil, fmt.Errorf("no contract address for version 0, net %s", net)
	}
	c, err := swapv1.NewETHSwap(contractAddr, cb)
	if err != nil {
		return nil, err
	}
	return &contractorV1{
		contractV1:   c,
		abi:          dexeth.ABIs[1],
		net:          net,
		contractAddr: contractAddr,
		acctAddr:     acctAddr,
		cb:           cb,
		atomize:      dexeth.WeiToGwei,
	}, nil
}

func (c *contractorV1) status(ctx context.Context, deets *dex.SwapContractDetails) (step dexeth.SwapStep, secret [32]byte, blockNumber uint32, err error) {
	rec, err := c.State(&bind.CallOpts{From: c.acctAddr}, dexeth.SwapToV1(deets))
	if err != nil {
		return 0, secret, 0, err
	}
	return dexeth.SwapStep(rec.State), rec.Secret, uint32(rec.BlockNumber.Uint64()), err
}

func (c *contractorV1) initiate(txOpts *bind.TransactOpts, details []*dex.SwapContractDetails) (*types.Transaction, error) {
	versionedContracts := make([]swapv1.ETHSwapContract, 0, len(details))
	for _, deets := range details {
		versionedContracts = append(versionedContracts, dexeth.SwapToV1(deets))
	}
	return c.Initiate(txOpts, versionedContracts)
}

func (c *contractorV1) redeem(txOpts *bind.TransactOpts, redeems []*asset.Redemption) (*types.Transaction, error) {
	versionedRedemptions := make([]swapv1.ETHSwapRedemption, 0, len(redeems))
	secretHashes := make(map[[32]byte]bool, len(redeems))
	for _, r := range redeems {
		var secret [32]byte
		copy(secret[:], r.Secret)
		secretHash := sha256.Sum256(r.Secret)
		if !bytes.Equal(secretHash[:], r.SwapDetails.SecretHash) {
			return nil, errors.New("wrong secret hash")
		}
		if secretHashes[secretHash] {
			return nil, fmt.Errorf("duplicate secret hash %x", secretHash[:])
		}
		secretHashes[secretHash] = true
		versionedRedemptions = append(versionedRedemptions, RedemptionToV1(secret, r.SwapDetails))
	}
	return c.Redeem(txOpts, versionedRedemptions)
}

func (c *contractorV1) refund(txOpts *bind.TransactOpts, contract *dex.SwapContractDetails) (*types.Transaction, error) {
	return c.Refund(txOpts, dexeth.SwapToV1(contract))
}

func (c *contractorV1) estimateInitGas(ctx context.Context, n int) (uint64, error) {
	initiations := make([]swapv1.ETHSwapContract, 0, n)
	for j := 0; j < n; j++ {
		var secretHash [32]byte
		copy(secretHash[:], encode.RandomBytes(32))
		initiations = append(initiations, swapv1.ETHSwapContract{
			RefundTimestamp: 1,
			SecretHash:      secretHash,
			Initiator:       c.acctAddr,
			Participant:     common.BytesToAddress(encode.RandomBytes(20)),
			Value:           1,
		})
	}

	var value *big.Int
	if !c.isToken {
		value = dexeth.GweiToWei(uint64(n))
	}

	return c.estimateGas(ctx, value, "initiate", initiations)
}

// estimateGas estimates the gas used to interact with the swap contract.
func (c *contractorV1) estimateGas(ctx context.Context, value *big.Int, method string, args ...interface{}) (uint64, error) {
	return estimateGas(ctx, c.acctAddr, c.contractAddr, c.abi, c.cb, value, method, args...)
}

func (c *contractorV1) estimateRedeemGas(ctx context.Context, secrets [][32]byte, details []*dex.SwapContractDetails) (uint64, error) {
	if len(secrets) != len(details) {
		return 0, fmt.Errorf("number of secrets (%d) does not match number of contracts (%d)", len(secrets), len(details))
	}

	redemps := make([]swapv1.ETHSwapRedemption, 0, len(secrets))
	for i, secret := range secrets {
		redemps = append(redemps, swapv1.ETHSwapRedemption{
			Secret: secret,
			C:      dexeth.SwapToV1(details[i]),
		})
	}
	return c.estimateGas(ctx, nil, "redeem", redemps)
}

func (c *contractorV1) estimateRefundGas(ctx context.Context, deets *dex.SwapContractDetails) (uint64, error) {
	return c.estimateGas(ctx, nil, "refund", dexeth.SwapToV1(deets))
}

func (c *contractorV1) isRedeemable(secret [32]byte, deets *dex.SwapContractDetails) (bool, error) {
	if is, err := c.IsRedeemable(&bind.CallOpts{From: c.acctAddr}, dexeth.SwapToV1(deets)); err != nil || !is {
		return is, err
	}
	var secretHash [32]byte
	copy(secretHash[:], deets.SecretHash)
	return sha256.Sum256(secret[:]) == secretHash, nil
}

func (c *contractorV1) isRefundable(deets *dex.SwapContractDetails) (bool, error) {
	if is, err := c.IsRedeemable(&bind.CallOpts{From: c.acctAddr}, dexeth.SwapToV1(deets)); err != nil || !is {
		return is, err
	}
	return time.Now().Unix() >= int64(deets.LockTime), nil
}

func (c *contractorV1) incomingValue(ctx context.Context, tx *types.Transaction) (uint64, error) {
	if redeems, err := dexeth.ParseRedeemDataV1(tx.Data()); err == nil {
		var redeemed uint64
		for _, r := range redeems {
			redeemed += r.Contract.Value
		}
		return redeemed, nil
	}
	refund, err := dexeth.ParseRefundDataV1(tx.Data())
	if err != nil {
		return 0, nil
	}
	return refund.Value, nil
}

// outgoingValue calculates the value sent in swaps in the tx.
func (c *contractorV1) outgoingValue(tx *types.Transaction) (swapped uint64) {
	if inits, err := dexeth.ParseInitiateDataV1(tx.Data()); err == nil {
		for _, init := range inits {
			swapped += init.Value
		}
	}
	return
}

// value calculates the incoming or outgoing value of the transaction, excluding
// fees but including operations against the swap contract.
func (c *contractorV1) value(ctx context.Context, tx *types.Transaction) (in, out uint64, err error) {
	if *tx.To() != c.contractAddr {
		return 0, 0, nil
	}

	if v, err := c.incomingValue(ctx, tx); err != nil {
		return 0, 0, fmt.Errorf("incomingValue error: %w", err)
	} else if v > 0 {
		return v, 0, nil
	}

	return 0, c.outgoingValue(tx), nil
}

type tokenContractorV1 struct {
	*contractorV1
	*erc20Contractor
	tokenAddr common.Address
}

func newV1TokenContractor(net dex.Network, assetID uint32, acctAddr common.Address, cb bind.ContractBackend) (tokenContractor, error) {
	token, tokenAddr, swapContractAddr, err := dexeth.VersionedNetworkToken(assetID, 1, net)
	if err != nil {
		return nil, err
	}

	c, err := erc20v1.NewERC20Swap(swapContractAddr, cb)
	if err != nil {
		return nil, err
	}

	tokenContract, err := erc20.NewIERC20(tokenAddr, cb)
	if err != nil {
		return nil, err
	}

	if boundAddr, err := c.TokenAddress(&bind.CallOpts{
		Context: context.TODO(),
	}); err != nil {
		return nil, fmt.Errorf("error reading bound token address %q: %w", tokenAddr, err)
	} else if boundAddr != tokenAddr {
		return nil, fmt.Errorf("wrong bound address. expected %s, got %s", tokenAddr, boundAddr)
	}

	return &tokenContractorV1{
		contractorV1: &contractorV1{
			contractV1:   c,
			abi:          dexeth.ABIs[1],
			net:          net,
			contractAddr: swapContractAddr,
			acctAddr:     acctAddr,
			cb:           cb,
			evmify:       token.AtomicToEVM,
			atomize:      token.EVMToAtomic,
		},
		tokenAddr: tokenAddr,
		erc20Contractor: &erc20Contractor{
			tokenContract: tokenContract,
			acct:          acctAddr,
			contract:      swapContractAddr,
		},
	}, nil
}

// estimateApproveGas estimates the gas needed to send an approve tx.
func (c *tokenContractorV1) estimateApproveGas(ctx context.Context, amount *big.Int) (uint64, error) {
	return c.estimateTokenContractGas(ctx, "approve", c.contractAddr, amount)
}

// estimateTransferGas esimates the gas needed for a transfer tx. The account
// needs to have > amount tokens to use this method.
func (c *tokenContractorV1) estimateTransferGas(ctx context.Context, amount *big.Int) (uint64, error) {
	return c.estimateTokenContractGas(ctx, "transfer", c.acctAddr, amount)
}

// estimateGas estimates the gas needed for methods on the ERC20 token contract.
// For estimating methods on the swap contract, use (contractorV0).estimateGas.
func (c *tokenContractorV1) estimateTokenContractGas(ctx context.Context, method string, args ...interface{}) (uint64, error) {
	return estimateGas(ctx, c.acctAddr, c.tokenAddr, erc20.ERC20ABI, c.cb, new(big.Int), method, args...)
}

// value finds incoming or outgoing value for the tx to either the swap contract
// or the erc20 token contract. For the token contract, only transfer and
// transferFrom are parsed. It is not an error if this tx is a call to another
// method of the token contract, but no values will be parsed.
func (c *tokenContractorV1) value(ctx context.Context, tx *types.Transaction) (in, out uint64, err error) {
	to := *tx.To()
	if to == c.contractAddr {
		return c.contractorV1.value(ctx, tx)
	}
	if to != c.tokenAddr {
		return 0, 0, nil
	}

	// Consider removing. We'll never be sending transferFrom transactions
	// directly.
	if sender, _, value, err := erc20.ParseTransferFromData(tx.Data()); err == nil && sender == c.acctAddr {
		return 0, c.atomize(value), nil
	}

	if _, value, err := erc20.ParseTransferData(tx.Data()); err == nil {
		return 0, c.atomize(value), nil
	}

	return 0, 0, nil
}

// tokenAddress exposes the token_address immutable address of the token-bound
// swap contract.
func (c *tokenContractorV1) tokenAddress() common.Address {
	return c.tokenAddr
}

var _ contractor = (*tokenContractorV1)(nil)
var _ tokenContractor = (*tokenContractorV1)(nil)

func RedemptionToV1(secret [32]byte, deets *dex.SwapContractDetails) swapv1.ETHSwapRedemption {
	return swapv1.ETHSwapRedemption{
		C:      dexeth.SwapToV1(deets),
		Secret: secret,
	}
}

// readOnlyCallOpts is the CallOpts used for read-only contract method calls.
func readOnlyCallOpts(ctx context.Context) *bind.CallOpts {
	return &bind.CallOpts{
		Pending: true,
		Context: ctx,
	}
}

func estimateGas(ctx context.Context, from, to common.Address, abi *abi.ABI, cb bind.ContractBackend, value *big.Int, method string, args ...interface{}) (uint64, error) {
	data, err := abi.Pack(method, args...)
	if err != nil {
		return 0, fmt.Errorf("Pack error: %v", err)
	}

	return cb.EstimateGas(ctx, ethereum.CallMsg{
		From:  from,
		To:    &to,
		Data:  data,
		Value: value,
	})
}

var contractorConstructors = map[uint32]contractorConstructor{
	0: newV0Contractor,
	1: newV1Contractor,
}

var tokenContractorConstructors = map[uint32]tokenContractorConstructor{
	0: newV0TokenContractor,
	1: newV1TokenContractor,
}
