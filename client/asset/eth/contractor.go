// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

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
	status(ctx context.Context, locator []byte) (*dexeth.SwapStatus, error)
	vector(ctx context.Context, locator []byte) (*dexeth.SwapVector, error)
	statusAndVector(ctx context.Context, locator []byte) (*dexeth.SwapStatus, *dexeth.SwapVector, error)
	initiate(*bind.TransactOpts, []*asset.Contract) (*types.Transaction, error)
	redeem(txOpts *bind.TransactOpts, redeems []*asset.Redemption) (*types.Transaction, error)
	refund(opts *bind.TransactOpts, locator []byte) (*types.Transaction, error)
	estimateInitGas(ctx context.Context, n int) (uint64, error)
	estimateRedeemGas(ctx context.Context, secrets [][32]byte, locators [][]byte) (uint64, error)
	estimateRefundGas(ctx context.Context, locator []byte) (uint64, error)
	// value checks the incoming or outgoing contract value. This is just the
	// one of redeem, refund, or initiate values. It is not an error if the
	// transaction does not pay to the contract, and the values returned in that
	// case will always be zero.
	value(context.Context, *types.Transaction) (incoming, outgoing uint64, err error)
	isRefundable(locator []byte) (bool, error)
	// packInitiateData returns ABI-packed calldata for an initiate transaction
	// with n dummy swaps. Used for L1 fee estimation on OP Stack chains.
	packInitiateData(n int) ([]byte, error)
	// packRedeemData returns ABI-packed calldata for a redeem transaction
	// with n dummy redemptions. Used for L1 fee estimation on OP Stack chains.
	packRedeemData(n int) ([]byte, error)
	// packRefundData returns ABI-packed calldata for a refund transaction
	// with dummy data. Used for L1 fee estimation on OP Stack chains.
	packRefundData() ([]byte, error)
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
	parseTransfer(*types.Receipt) (uint64, error)
	estimateTransferGas(context.Context, *big.Int) (uint64, error)
}

type unifiedContractor interface {
	tokenContractor(token *dexeth.Token) (tokenContractor, error)
}

type contractorConstructor func(net dex.Network, contractAddr, acctAddr common.Address, ec bind.ContractBackend) (contractor, error)

// contractV0 is the interface common to a version 0 swap contract or version 0
// token swap contract.
type contractV0 interface {
	Initiate(opts *bind.TransactOpts, initiations []swapv0.ETHSwapInitiation) (*types.Transaction, error)
	Redeem(opts *bind.TransactOpts, redemptions []swapv0.ETHSwapRedemption) (*types.Transaction, error)
	Swap(opts *bind.CallOpts, secretHash [32]byte) (swapv0.ETHSwapSwap, error)
	Refund(opts *bind.TransactOpts, secretHash [32]byte) (*types.Transaction, error)
	IsRefundable(opts *bind.CallOpts, secretHash [32]byte) (bool, error)
}

var _ contractV0 = (*swapv0.ETHSwap)(nil)

type contractV1 interface {
	Initiate(opts *bind.TransactOpts, token common.Address, contracts []swapv1.ETHSwapVector) (*types.Transaction, error)
	Redeem(opts *bind.TransactOpts, token common.Address, redemptions []swapv1.ETHSwapRedemption) (*types.Transaction, error)
	Status(opts *bind.CallOpts, token common.Address, c swapv1.ETHSwapVector) (swapv1.ETHSwapStatus, error)
	Refund(opts *bind.TransactOpts, token common.Address, c swapv1.ETHSwapVector) (*types.Transaction, error)
	IsRedeemable(opts *bind.CallOpts, token common.Address, c swapv1.ETHSwapVector) (bool, error)
	EntryPoint(opts *bind.CallOpts) (common.Address, error)

	ContractKey(opts *bind.CallOpts, token common.Address, v swapv1.ETHSwapVector) ([32]byte, error)
	Swaps(opts *bind.CallOpts, arg0 [32]byte) ([32]byte, error)
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
func newV0Contractor(_ dex.Network, contractAddr, acctAddr common.Address, cb bind.ContractBackend) (contractor, error) {
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
func (c *contractorV0) initiate(txOpts *bind.TransactOpts, contracts []*asset.Contract) (tx *types.Transaction, err error) {
	inits := make([]swapv0.ETHSwapInitiation, 0, len(contracts))
	secrets := make(map[[32]byte]bool, len(contracts))

	for _, contract := range contracts {
		if len(contract.SecretHash) != dexeth.SecretHashSize {
			return nil, fmt.Errorf("wrong secret hash length. wanted %d, got %d", dexeth.SecretHashSize, len(contract.SecretHash))
		}

		var secretHash [32]byte
		copy(secretHash[:], contract.SecretHash)

		if secrets[secretHash] {
			return nil, fmt.Errorf("secret hash %s is a duplicate", contract.SecretHash)
		}
		secrets[secretHash] = true

		if !common.IsHexAddress(contract.Address) {
			return nil, fmt.Errorf("%q is not an address", contract.Address)
		}

		inits = append(inits, swapv0.ETHSwapInitiation{
			RefundTimestamp: big.NewInt(int64(contract.LockTime)),
			SecretHash:      secretHash,
			Participant:     common.HexToAddress(contract.Address),
			Value:           c.evmify(contract.Value),
		})
	}

	return c.contractV0.Initiate(txOpts, inits)
}

// redeem sends the redemptions to the swap contracts redeem method.
func (c *contractorV0) redeem(txOpts *bind.TransactOpts, redemptions []*asset.Redemption) (tx *types.Transaction, err error) {
	redemps := make([]swapv0.ETHSwapRedemption, 0, len(redemptions))
	secretHashes := make(map[[32]byte]bool, len(redemptions))

	for _, r := range redemptions {
		secretB, secretHashB := r.Secret, r.Spends.SecretHash
		if len(secretB) != 32 || len(secretHashB) != 32 {
			return nil, fmt.Errorf("invalid secret and/or secret hash sizes, %d and %d", len(secretB), len(secretHashB))
		}
		var secret, secretHash [32]byte
		copy(secret[:], secretB)
		copy(secretHash[:], secretHashB)
		if secretHashes[secretHash] {
			return nil, fmt.Errorf("duplicate secret hash %x", secretHash[:])
		}
		checkHash := sha256.Sum256(secretB)
		if checkHash != secretHash {
			return nil, errors.New("wrong secret")
		}

		secretHashes[secretHash] = true

		redemps = append(redemps, swapv0.ETHSwapRedemption{
			Secret:     secret,
			SecretHash: secretHash,
		})
	}
	return c.contractV0.Redeem(txOpts, redemps)
}

// status fetches the SwapStatus, which specifies the current state of mutable
// swap data.
func (c *contractorV0) status(ctx context.Context, locator []byte) (*dexeth.SwapStatus, error) {
	secretHash, err := dexeth.ParseV0Locator(locator)
	if err != nil {
		return nil, err
	}
	swap, err := c.swap(ctx, secretHash)
	if err != nil {
		return nil, err
	}
	status := &dexeth.SwapStatus{
		Step:        swap.State,
		Secret:      swap.Secret,
		BlockHeight: swap.BlockHeight,
	}
	return status, nil
}

// vector generates a SwapVector, containing the immutable data that defines
// the swap.
func (c *contractorV0) vector(ctx context.Context, locator []byte) (*dexeth.SwapVector, error) {
	secretHash, err := dexeth.ParseV0Locator(locator)
	if err != nil {
		return nil, err
	}
	swap, err := c.swap(ctx, secretHash)
	if err != nil {
		return nil, err
	}
	vector := &dexeth.SwapVector{
		From:       swap.Initiator,
		To:         swap.Participant,
		Value:      swap.Value,
		SecretHash: secretHash,
		LockTime:   uint64(swap.LockTime.Unix()),
	}
	return vector, nil
}

// statusAndVector generates both the status and the vector simultaneously. For
// version 0, this is better than calling status and vector separately, since
// each makes an identical call to c.swap.
func (c *contractorV0) statusAndVector(ctx context.Context, locator []byte) (*dexeth.SwapStatus, *dexeth.SwapVector, error) {
	secretHash, err := dexeth.ParseV0Locator(locator)
	if err != nil {
		return nil, nil, err
	}
	swap, err := c.swap(ctx, secretHash)
	if err != nil {
		return nil, nil, err
	}
	vector := &dexeth.SwapVector{
		From:       swap.Initiator,
		To:         swap.Participant,
		Value:      swap.Value,
		SecretHash: secretHash,
		LockTime:   uint64(swap.LockTime.Unix()),
	}
	status := &dexeth.SwapStatus{
		Step:        swap.State,
		Secret:      swap.Secret,
		BlockHeight: swap.BlockHeight,
	}
	return status, vector, nil
}

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

// refund issues the refund command to the swap contract. Use isRefundable first
// to ensure the refund will be accepted.
func (c *contractorV0) refund(txOpts *bind.TransactOpts, locator []byte) (tx *types.Transaction, err error) {
	secretHash, err := dexeth.ParseV0Locator(locator)
	if err != nil {
		return nil, err
	}
	return c.refundImpl(txOpts, secretHash)
}

func (c *contractorV0) refundImpl(txOpts *bind.TransactOpts, secretHash [32]byte) (*types.Transaction, error) {
	return c.contractV0.Refund(txOpts, secretHash)
}

// isRefundable exposes the isRefundable method of the swap contract.
func (c *contractorV0) isRefundable(locator []byte) (bool, error) {
	secretHash, err := dexeth.ParseV0Locator(locator)
	if err != nil {
		return false, err
	}
	return c.isRefundableImpl(secretHash)
}

func (c *contractorV0) isRefundableImpl(secretHash [32]byte) (bool, error) {
	return c.contractV0.IsRefundable(&bind.CallOpts{From: c.acctAddr}, secretHash)
}

// estimateRedeemGas estimates the gas used to redeem. The secret hashes
// supplied must reference existing swaps, so this method can't be used until
// the swap is initiated.
func (c *contractorV0) estimateRedeemGas(ctx context.Context, secrets [][32]byte, _ [][]byte) (uint64, error) {
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
func (c *contractorV0) estimateRefundGas(ctx context.Context, locator []byte) (uint64, error) {
	secretHash, err := dexeth.ParseV0Locator(locator)
	if err != nil {
		return 0, err
	}
	return c.estimateRefundGasImpl(ctx, secretHash)
}

func (c *contractorV0) estimateRefundGasImpl(ctx context.Context, secretHash [32]byte) (uint64, error) {
	return c.estimateGas(ctx, nil, "refund", secretHash)
}

// packRedeemData returns ABI-packed calldata for a redeem transaction.
func (c *contractorV0) packRedeemData(n int) ([]byte, error) {
	redemps := make([]swapv0.ETHSwapRedemption, 0, n)
	for j := 0; j < n; j++ {
		var secret [32]byte
		copy(secret[:], encode.RandomBytes(32))
		redemps = append(redemps, swapv0.ETHSwapRedemption{
			Secret:     secret,
			SecretHash: sha256.Sum256(secret[:]),
		})
	}
	return c.abi.Pack("redeem", redemps)
}

// packRefundData returns ABI-packed calldata for a refund transaction.
func (c *contractorV0) packRefundData() ([]byte, error) {
	var secretHash [32]byte
	copy(secretHash[:], encode.RandomBytes(32))
	return c.abi.Pack("refund", secretHash)
}

// dummyInitiationsV0 creates n dummy initiations for gas/calldata estimation.
func dummyInitiationsV0(n int, acctAddr common.Address) []swapv0.ETHSwapInitiation {
	initiations := make([]swapv0.ETHSwapInitiation, 0, n)
	for j := 0; j < n; j++ {
		var secretHash [32]byte
		copy(secretHash[:], encode.RandomBytes(32))
		initiations = append(initiations, swapv0.ETHSwapInitiation{
			RefundTimestamp: big.NewInt(1),
			SecretHash:      secretHash,
			Participant:     acctAddr,
			Value:           big.NewInt(1),
		})
	}
	return initiations
}

// packInitiateData returns ABI-packed calldata for an initiate transaction.
func (c *contractorV0) packInitiateData(n int) ([]byte, error) {
	return c.abi.Pack("initiate", dummyInitiationsV0(n, c.acctAddr))
}

// estimateInitGas estimates the gas used to initiate n generic swaps. The
// account must have a balance of at least n wei, as well as the eth balance
// to cover fees.
func (c *contractorV0) estimateInitGas(ctx context.Context, n int) (uint64, error) {
	initiations := dummyInitiationsV0(n, c.acctAddr)

	var value *big.Int
	if !c.isToken {
		value = big.NewInt(int64(n))
	}

	return c.estimateGas(ctx, value, "initiate", initiations)
}

// estimateGas estimates the gas used to interact with the swap contract.
func (c *contractorV0) estimateGas(ctx context.Context, value *big.Int, method string, args ...any) (uint64, error) {
	data, err := c.abi.Pack(method, args...)
	if err != nil {
		return 0, fmt.Errorf("pack error: %v", err)
	}

	return c.cb.EstimateGas(ctx, ethereum.CallMsg{
		From:  c.acctAddr,
		To:    &c.contractAddr,
		Data:  data,
		Value: value,
	})
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
			swap, err := c.swap(ctx, redeem.SecretHash)
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
	swap, err := c.swap(ctx, secretHash)
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
	cb                bind.ContractBackend
	tokenContract     *erc20.IERC20
	tokenContractAddr common.Address
	acctAddr          common.Address
	swapAddr          common.Address
}

// balance exposes the read-only balanceOf method of the erc20 token contract.
func (c *erc20Contractor) balance(ctx context.Context) (*big.Int, error) {
	callOpts := &bind.CallOpts{
		From:    c.acctAddr,
		Context: ctx,
	}

	return c.tokenContract.BalanceOf(callOpts, c.acctAddr)
}

// allowance exposes the read-only allowance method of the erc20 token contract.
func (c *erc20Contractor) allowance(ctx context.Context) (*big.Int, error) {
	callOpts := &bind.CallOpts{
		From:    c.acctAddr,
		Context: ctx,
	}
	return c.tokenContract.Allowance(callOpts, c.acctAddr, c.swapAddr)
}

// approve sends an approve transaction approving the linked contract to call
// transferFrom for the specified amount.
func (c *erc20Contractor) approve(txOpts *bind.TransactOpts, amount *big.Int) (*types.Transaction, error) {
	return c.tokenContract.Approve(txOpts, c.swapAddr, amount)
}

// transfer calls the transfer method of the erc20 token contract. Used for
// sends or withdrawals.
func (c *erc20Contractor) transfer(txOpts *bind.TransactOpts, addr common.Address, amount *big.Int) (*types.Transaction, error) {
	return c.tokenContract.Transfer(txOpts, addr, amount)
}

// estimateApproveGas estimates the gas needed to send an approve tx.
func (c *erc20Contractor) estimateApproveGas(ctx context.Context, amount *big.Int) (uint64, error) {
	return estimateGas(ctx, c.acctAddr, c.tokenContractAddr, erc20.ERC20ABI, c.cb, new(big.Int), "approve", c.swapAddr, amount)
}

// estimateTransferGas estimates the gas needed for a transfer tx. The account
// needs to have > amount tokens to use this method.
func (c *erc20Contractor) estimateTransferGas(ctx context.Context, amount *big.Int) (uint64, error) {
	return estimateGas(ctx, c.acctAddr, c.swapAddr, erc20.ERC20ABI, c.cb, new(big.Int), "transfer", c.acctAddr, amount)
}

func (c *erc20Contractor) parseTransfer(receipt *types.Receipt) (uint64, error) {
	var transferredAmt uint64
	for _, log := range receipt.Logs {
		if log.Address != c.tokenContractAddr {
			continue
		}
		transfer, err := c.tokenContract.ParseTransfer(*log)
		if err != nil {
			continue
		}
		if transfer.To == c.acctAddr {
			transferredAmt += transfer.Value.Uint64()
		}
	}

	if transferredAmt > 0 {
		return transferredAmt, nil
	}

	return 0, fmt.Errorf("transfer log to %s not found", c.tokenContractAddr)
}

// tokenContractorV0 is a contractor that implements the tokenContractor
// methods, providing access to the methods of the token's ERC20 contract.
type tokenContractorV0 struct {
	*contractorV0
	*erc20Contractor
}

var _ contractor = (*tokenContractorV0)(nil)
var _ tokenContractor = (*tokenContractorV0)(nil)

// newV0TokenContractor is a contractor for version 0 erc20 token swap contract.
func newV0TokenContractor(net dex.Network, token *dexeth.Token, acctAddr common.Address, cb bind.ContractBackend) (tokenContractor, error) {
	netToken, found := token.NetTokens[net]
	if !found {
		return nil, fmt.Errorf("token %s has no network %s", token.Name, net)
	}
	tokenAddr := netToken.Address
	contract, found := netToken.SwapContracts[0] // contract version 0
	if !found {
		return nil, fmt.Errorf("token %s version 0 has no network %s token info", token.Name, net)
	}
	swapContractAddr := contract.Address

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

		erc20Contractor: &erc20Contractor{
			cb:                cb,
			tokenContract:     tokenContract,
			tokenContractAddr: tokenAddr,
			acctAddr:          acctAddr,
			swapAddr:          swapContractAddr,
		},
	}, nil
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
	if to != c.tokenContractAddr {
		return 0, 0, nil
	}

	// Consider removing. We'll never be sending transferFrom transactions
	// directly.
	if sender, _, value, err := erc20.ParseTransferFromData(tx.Data()); err == nil && sender == c.contractorV0.acctAddr {
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
	return c.tokenContractAddr
}

// gaslessRedeemContractor is a contractor that supports gasless redemptions
// using ERC-4337 account abstraction.
type gaslessRedeemContractor interface {
	// gaslessRedeemCalldata creates the calldata to be sent to the bundler
	// for a gasless redemption. The nonce must match the UserOp nonce.
	gaslessRedeemCalldata(redeems []*asset.Redemption, nonce *big.Int) ([]byte, error)
	// entrypointAddress returns the address of the entrypoint contract specified
	// in the ETH Swap contract.
	entrypointAddress() (common.Address, error)
}

type contractorV1 struct {
	contractV1
	net              dex.Network
	abi              *abi.ABI
	tokenAddr        common.Address // zero-address for base-chain asset, e.g. ETH, POL
	swapContractAddr common.Address
	acctAddr         common.Address
	cb               bind.ContractBackend
	isToken          bool
	evmify           func(uint64) *big.Int
	atomize          func(*big.Int) uint64
}

var _ contractor = (*contractorV1)(nil)
var _ gaslessRedeemContractor = (*contractorV1)(nil)

func newV1Contractor(net dex.Network, swapContractAddr, acctAddr common.Address, cb bind.ContractBackend) (contractor, error) {
	c, err := swapv1.NewETHSwap(swapContractAddr, cb)
	if err != nil {
		return nil, err
	}
	return &contractorV1{
		contractV1:       c,
		net:              net,
		abi:              dexeth.ABIs[1],
		swapContractAddr: swapContractAddr,
		acctAddr:         acctAddr,
		cb:               cb,
		atomize:          dexeth.WeiToGwei,
		evmify:           dexeth.GweiToWei,
	}, nil
}

func (c *contractorV1) status(ctx context.Context, locator []byte) (*dexeth.SwapStatus, error) {
	v, err := dexeth.ParseV1Locator(locator)
	if err != nil {
		return nil, err
	}
	rec, err := c.Status(&bind.CallOpts{From: c.acctAddr}, c.tokenAddr, dexeth.SwapVectorToAbigen(v))
	if err != nil {
		return nil, err
	}
	return &dexeth.SwapStatus{
		Step:        dexeth.SwapStep(rec.Step),
		Secret:      rec.Secret,
		BlockHeight: rec.BlockNumber.Uint64(),
	}, err
}

func (c *contractorV1) vector(ctx context.Context, locator []byte) (*dexeth.SwapVector, error) {
	return dexeth.ParseV1Locator(locator)
}

func (c *contractorV1) statusAndVector(ctx context.Context, locator []byte) (*dexeth.SwapStatus, *dexeth.SwapVector, error) {
	v, err := dexeth.ParseV1Locator(locator)
	if err != nil {
		return nil, nil, err
	}

	rec, err := c.Status(&bind.CallOpts{From: c.acctAddr}, c.tokenAddr, dexeth.SwapVectorToAbigen(v))
	if err != nil {
		return nil, nil, err
	}
	return &dexeth.SwapStatus{
		Step:        dexeth.SwapStep(rec.Step),
		Secret:      rec.Secret,
		BlockHeight: rec.BlockNumber.Uint64(),
	}, v, err
}

// func (c *contractorV1) record(v *dexeth.SwapVector) (r [32]byte, err error) {
// 	abiVec := dexeth.SwapVectorToAbigen(v)
// 	ck, err := c.ContractKey(&bind.CallOpts{From: c.acctAddr}, abiVec)
// 	if err != nil {
// 		return r, fmt.Errorf("ContractKey error: %v", err)
// 	}
// 	return c.Swaps(&bind.CallOpts{From: c.acctAddr}, ck)
// }

func (c *contractorV1) initiate(txOpts *bind.TransactOpts, contracts []*asset.Contract) (*types.Transaction, error) {
	versionedContracts := make([]swapv1.ETHSwapVector, 0, len(contracts))
	for _, ac := range contracts {
		v := &dexeth.SwapVector{
			From:     c.acctAddr,
			To:       common.HexToAddress(ac.Address),
			Value:    c.evmify(ac.Value),
			LockTime: ac.LockTime,
		}
		copy(v.SecretHash[:], ac.SecretHash)
		versionedContracts = append(versionedContracts, dexeth.SwapVectorToAbigen(v))
	}
	return c.Initiate(txOpts, c.tokenAddr, versionedContracts)
}

func (c *contractorV1) convertRedeems(redeems []*asset.Redemption) ([]swapv1.ETHSwapRedemption, error) {
	versionedRedemptions := make([]swapv1.ETHSwapRedemption, 0, len(redeems))
	secretHashes := make(map[[32]byte]bool, len(redeems))
	for _, r := range redeems {
		var secret [32]byte
		copy(secret[:], r.Secret)
		secretHash := sha256.Sum256(r.Secret)
		if !bytes.Equal(secretHash[:], r.Spends.SecretHash) {
			return nil, errors.New("wrong secret")
		}
		if secretHashes[secretHash] {
			return nil, fmt.Errorf("duplicate secret hash %x", secretHash[:])
		}
		secretHashes[secretHash] = true

		// Not checking version from DecodeLocator because it was already
		// audited and incorrect version locator would err below anyway.
		_, locator, err := dexeth.DecodeContractData(r.Spends.Contract)
		if err != nil {
			return nil, fmt.Errorf("error parsing locator redeem: %w", err)
		}
		v, err := dexeth.ParseV1Locator(locator)
		if err != nil {
			return nil, fmt.Errorf("error parsing locator: %w", err)
		}
		versionedRedemptions = append(versionedRedemptions, swapv1.ETHSwapRedemption{
			V:      dexeth.SwapVectorToAbigen(v),
			Secret: secret,
		})
	}
	return versionedRedemptions, nil
}

func (c *contractorV1) redeem(txOpts *bind.TransactOpts, redeems []*asset.Redemption) (*types.Transaction, error) {
	versionedRedemptions, err := c.convertRedeems(redeems)
	if err != nil {
		return nil, err
	}
	return c.Redeem(txOpts, c.tokenAddr, versionedRedemptions)
}

func (c *contractorV1) gaslessRedeemCalldata(redeems []*asset.Redemption, nonce *big.Int) ([]byte, error) {
	versionedRedemptions, err := c.convertRedeems(redeems)
	if err != nil {
		return nil, err
	}
	return c.abi.Pack("redeemAA", versionedRedemptions, nonce)
}

func (c *contractorV1) entrypointAddress() (common.Address, error) {
	return c.EntryPoint(&bind.CallOpts{From: c.acctAddr})
}

func (c *contractorV1) refund(txOpts *bind.TransactOpts, locator []byte) (*types.Transaction, error) {
	v, err := dexeth.ParseV1Locator(locator)
	if err != nil {
		return nil, err
	}
	return c.Refund(txOpts, c.tokenAddr, dexeth.SwapVectorToAbigen(v))
}

// dummyInitiationsV1 creates n dummy initiations for gas/calldata estimation.
func dummyInitiationsV1(n int, acctAddr common.Address, evmify func(uint64) *big.Int) []swapv1.ETHSwapVector {
	initiations := make([]swapv1.ETHSwapVector, 0, n)
	for j := 0; j < n; j++ {
		var secretHash [32]byte
		copy(secretHash[:], encode.RandomBytes(32))
		initiations = append(initiations, swapv1.ETHSwapVector{
			RefundTimestamp: uint64(time.Now().Add(time.Hour * 6).Unix()),
			SecretHash:      secretHash,
			Initiator:       acctAddr,
			Participant:     common.BytesToAddress(encode.RandomBytes(20)),
			Value:           evmify(1),
		})
	}
	return initiations
}

// packInitiateData returns ABI-packed calldata for an initiate transaction.
func (c *contractorV1) packInitiateData(n int) ([]byte, error) {
	return c.abi.Pack("initiate", c.tokenAddr, dummyInitiationsV1(n, c.acctAddr, c.evmify))
}

func (c *contractorV1) estimateInitGas(ctx context.Context, n int) (uint64, error) {
	initiations := dummyInitiationsV1(n, c.acctAddr, c.evmify)

	var value *big.Int
	if !c.isToken {
		value = dexeth.GweiToWei(uint64(n))
	}

	return c.estimateGas(ctx, value, "initiate", c.tokenAddr, initiations)
}

func (c *contractorV1) estimateGas(ctx context.Context, value *big.Int, method string, args ...any) (uint64, error) {
	return estimateGas(ctx, c.acctAddr, c.swapContractAddr, c.abi, c.cb, value, method, args...)
}

func (c *contractorV1) estimateRedeemGas(ctx context.Context, secrets [][32]byte, locators [][]byte) (uint64, error) {
	if len(secrets) != len(locators) {
		return 0, fmt.Errorf("number of secrets (%d) does not match number of contracts (%d)", len(secrets), len(locators))
	}

	vectors := make([]*dexeth.SwapVector, len(locators))
	for i, loc := range locators {
		v, err := dexeth.ParseV1Locator(loc)
		if err != nil {
			return 0, fmt.Errorf("unable to parse locator # %d (%x): %v", i, loc, err)
		}
		vectors[i] = v
	}

	redemps := make([]swapv1.ETHSwapRedemption, 0, len(secrets))
	for i, secret := range secrets {
		redemps = append(redemps, swapv1.ETHSwapRedemption{
			Secret: secret,
			V:      dexeth.SwapVectorToAbigen(vectors[i]),
		})
	}
	return c.estimateGas(ctx, nil, "redeem", c.tokenAddr, redemps)
}

func (c *contractorV1) estimateRefundGas(ctx context.Context, locator []byte) (uint64, error) {
	v, err := dexeth.ParseV1Locator(locator)
	if err != nil {
		return 0, err
	}
	return c.estimateGas(ctx, nil, "refund", c.tokenAddr, dexeth.SwapVectorToAbigen(v))
}

// packRedeemData returns ABI-packed calldata for a redeem transaction.
func (c *contractorV1) packRedeemData(n int) ([]byte, error) {
	redemps := make([]swapv1.ETHSwapRedemption, 0, n)
	for j := 0; j < n; j++ {
		var secret [32]byte
		copy(secret[:], encode.RandomBytes(32))
		redemps = append(redemps, swapv1.ETHSwapRedemption{
			Secret: secret,
			V: swapv1.ETHSwapVector{
				RefundTimestamp: uint64(time.Now().Add(time.Hour * 6).Unix()),
				SecretHash:      sha256.Sum256(secret[:]),
				Initiator:       common.BytesToAddress(encode.RandomBytes(20)),
				Participant:     c.acctAddr,
				Value:           c.evmify(1),
			},
		})
	}
	return c.abi.Pack("redeem", c.tokenAddr, redemps)
}

// packRefundData returns ABI-packed calldata for a refund transaction.
func (c *contractorV1) packRefundData() ([]byte, error) {
	var secretHash [32]byte
	copy(secretHash[:], encode.RandomBytes(32))
	v := swapv1.ETHSwapVector{
		RefundTimestamp: uint64(time.Now().Add(-time.Hour).Unix()),
		SecretHash:      secretHash,
		Initiator:       c.acctAddr,
		Participant:     common.BytesToAddress(encode.RandomBytes(20)),
		Value:           c.evmify(1),
	}
	return c.abi.Pack("refund", c.tokenAddr, v)
}

func (c *contractorV1) isRedeemable(locator []byte, secret [32]byte) (bool, error) {
	v, err := dexeth.ParseV1Locator(locator)
	if err != nil {
		return false, err
	}
	if v.To != c.acctAddr {
		return false, nil
	}
	if is, err := c.IsRedeemable(&bind.CallOpts{From: c.acctAddr}, c.tokenAddr, dexeth.SwapVectorToAbigen(v)); err != nil || !is {
		return is, err
	}
	return sha256.Sum256(secret[:]) == v.SecretHash, nil
}

func (c *contractorV1) isRefundable(locator []byte) (bool, error) {
	v, err := dexeth.ParseV1Locator(locator)
	if err != nil {
		return false, err
	}
	if is, err := c.IsRedeemable(&bind.CallOpts{From: c.acctAddr}, c.tokenAddr, dexeth.SwapVectorToAbigen(v)); err != nil || !is {
		return is, err
	}
	return time.Now().Unix() >= int64(v.LockTime), nil
}

func (c *contractorV1) incomingValue(ctx context.Context, tx *types.Transaction) (uint64, error) {
	if _, redeems, err := dexeth.ParseRedeemDataV1(tx.Data()); err == nil {
		var redeemed *big.Int
		for _, r := range redeems {
			redeemed.Add(redeemed, r.Contract.Value)
		}
		return c.atomize(redeemed), nil
	}
	refund, err := dexeth.ParseRefundDataV1(tx.Data())
	if err != nil {
		return 0, nil
	}
	return c.atomize(refund.Value), nil
}

func (c *contractorV1) outgoingValue(tx *types.Transaction) (swapped uint64) {
	if _, inits, err := dexeth.ParseInitiateDataV1(tx.Data()); err == nil {
		for _, init := range inits {
			swapped += c.atomize(init.Value)
		}
	}
	return
}

func (c *contractorV1) value(ctx context.Context, tx *types.Transaction) (in, out uint64, err error) {
	if *tx.To() != c.swapContractAddr {
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
}

func (c *contractorV1) tokenContractor(token *dexeth.Token) (tokenContractor, error) {
	netToken, found := token.NetTokens[c.net]
	if !found {
		return nil, fmt.Errorf("token %s has no network %s", token.Name, c.net)
	}
	tokenAddr := netToken.Address

	tokenContract, err := erc20.NewIERC20(tokenAddr, c.cb)
	if err != nil {
		return nil, err
	}

	return &tokenContractorV1{
		contractorV1: &contractorV1{
			contractV1:       c.contractV1,
			net:              c.net,
			abi:              c.abi,
			tokenAddr:        tokenAddr,
			swapContractAddr: c.swapContractAddr,
			acctAddr:         c.acctAddr,
			cb:               c.cb,
			isToken:          true,
			evmify:           token.AtomicToEVM,
			atomize:          token.EVMToAtomic,
		},
		erc20Contractor: &erc20Contractor{
			cb:                c.cb,
			tokenContract:     tokenContract,
			tokenContractAddr: tokenAddr,
			acctAddr:          c.acctAddr,
			swapAddr:          c.swapContractAddr,
		},
	}, nil
}

// value finds incoming or outgoing value for the tx to either the swap contract
// or the erc20 token contract. For the token contract, only transfer and
// transferFrom are parsed. It is not an error if this tx is a call to another
// method of the token contract, but no values will be parsed.
func (c *tokenContractorV1) value(ctx context.Context, tx *types.Transaction) (in, out uint64, err error) {
	to := *tx.To()
	if to == c.swapContractAddr {
		return c.contractorV1.value(ctx, tx)
	}
	if to != c.tokenAddr {
		return 0, 0, nil
	}

	// Consider removing. We'll never be sending transferFrom transactions
	// directly.
	if sender, _, value, err := erc20.ParseTransferFromData(tx.Data()); err == nil && sender == c.contractorV1.acctAddr {
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

func estimateGas(ctx context.Context, from, to common.Address, abi *abi.ABI, cb bind.ContractBackend, value *big.Int, method string, args ...any) (uint64, error) {
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
