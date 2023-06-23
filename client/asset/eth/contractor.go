// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package eth

import (
	"context"
	"crypto/sha256"
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
	swap(ctx context.Context, secretHash [32]byte) (*dexeth.SwapState, error)
	initiate(*bind.TransactOpts, []*asset.Contract) (*types.Transaction, error)
	redeem(txOpts *bind.TransactOpts, redeems []*asset.Redemption) (*types.Transaction, error)
	refund(opts *bind.TransactOpts, secretHash [32]byte) (*types.Transaction, error)
	estimateInitGas(ctx context.Context, n int) (uint64, error)
	estimateRedeemGas(ctx context.Context, secrets [][32]byte) (uint64, error)
	estimateRefundGas(ctx context.Context, secretHash [32]byte) (uint64, error)
	// value checks the incoming or outgoing contract value. This is just the
	// one of redeem, refund, or initiate values. It is not an error if the
	// transaction does not pay to the contract, and the values returned in that
	// case will always be zero.
	value(context.Context, *types.Transaction) (incoming, outgoing uint64, err error)
	isRefundable(secretHash [32]byte) (bool, error)
	voidUnusedNonce()
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

type contractorConstructor func(contractAddr, addr common.Address, ec bind.ContractBackend) (contractor, error)
type tokenContractorConstructor func(net dex.Network, token *dexeth.Token, acctAddr common.Address, ec bind.ContractBackend) (tokenContractor, error)

// contractV0 is the interface common to a version 0 swap contract or version 0
// token swap contract.
type contractV0 interface {
	Initiate(opts *bind.TransactOpts, initiations []swapv0.ETHSwapInitiation) (*types.Transaction, error)
	Redeem(opts *bind.TransactOpts, redemptions []swapv0.ETHSwapRedemption) (*types.Transaction, error)
	Swap(opts *bind.CallOpts, secretHash [32]byte) (swapv0.ETHSwapSwap, error)
	Refund(opts *bind.TransactOpts, secretHash [32]byte) (*types.Transaction, error)
	IsRefundable(opts *bind.CallOpts, secretHash [32]byte) (bool, error)
}

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
func newV0Contractor(contractAddr, acctAddr common.Address, cb bind.ContractBackend) (contractor, error) {
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

// refund issues the refund command to the swap contract. Use isRefundable first
// to ensure the refund will be accepted.
func (c *contractorV0) refund(txOpts *bind.TransactOpts, secretHash [32]byte) (tx *types.Transaction, err error) {
	return c.contractV0.Refund(txOpts, secretHash)
}

// isRefundable exposes the isRefundable method of the swap contract.
func (c *contractorV0) isRefundable(secretHash [32]byte) (bool, error) {
	return c.contractV0.IsRefundable(&bind.CallOpts{From: c.acctAddr}, secretHash)
}

// estimateRedeemGas estimates the gas used to redeem. The secret hashes
// supplied must reference existing swaps, so this method can't be used until
// the swap is initiated.
func (c *contractorV0) estimateRedeemGas(ctx context.Context, secrets [][32]byte) (uint64, error) {
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
func (c *contractorV0) estimateRefundGas(ctx context.Context, secretHash [32]byte) (uint64, error) {
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
	data, err := c.abi.Pack(method, args...)
	if err != nil {
		return 0, fmt.Errorf("Pack error: %v", err)
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
	if redeems, err := dexeth.ParseRedeemData(tx.Data(), 0); err == nil {
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
	secretHash, err := dexeth.ParseRefundData(tx.Data(), 0)
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
	if inits, err := dexeth.ParseInitiateData(tx.Data(), 0); err == nil {
		for _, init := range inits {
			swapped += c.atomize(init.Value)
		}
	}
	return
}

// voidUnusedNonce allows the next nonce received from a provider to be the same
// as a recent nonce. Use when we fetch a nonce but error before or while
// sending a transaction.
func (c *contractorV0) voidUnusedNonce() {
	if mRPC, is := c.cb.(*multiRPCClient); is {
		mRPC.voidUnusedNonce()
	}
}

// tokenContractorV0 is a contractor that implements the tokenContractor
// methods, providing access to the methods of the token's ERC20 contract.
type tokenContractorV0 struct {
	*contractorV0
	tokenAddr     common.Address
	tokenContract *erc20.IERC20
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
		tokenAddr:     tokenAddr,
		tokenContract: tokenContract,
	}, nil
}

// balance exposes the read-only balanceOf method of the erc20 token contract.
func (c *tokenContractorV0) balance(ctx context.Context) (*big.Int, error) {
	callOpts := &bind.CallOpts{
		From:    c.acctAddr,
		Context: ctx,
	}

	return c.tokenContract.BalanceOf(callOpts, c.acctAddr)
}

// allowance exposes the read-only allowance method of the erc20 token contract.
func (c *tokenContractorV0) allowance(ctx context.Context) (*big.Int, error) {
	// See if we support the pending state.
	_, pendingUnavailable := c.cb.(*multiRPCClient)
	callOpts := &bind.CallOpts{
		Pending: !pendingUnavailable,
		From:    c.acctAddr,
		Context: ctx,
	}
	return c.tokenContract.Allowance(callOpts, c.acctAddr, c.contractAddr)
}

// approve sends an approve transaction approving the linked contract to call
// transferFrom for the specified amount.
func (c *tokenContractorV0) approve(txOpts *bind.TransactOpts, amount *big.Int) (tx *types.Transaction, err error) {
	return c.tokenContract.Approve(txOpts, c.contractAddr, amount)
}

// transfer calls the transfer method of the erc20 token contract. Used for
// sends or withdrawals.
func (c *tokenContractorV0) transfer(txOpts *bind.TransactOpts, addr common.Address, amount *big.Int) (tx *types.Transaction, err error) {
	return c.tokenContract.Transfer(txOpts, addr, amount)
}

// estimateApproveGas estimates the gas needed to send an approve tx.
func (c *tokenContractorV0) estimateApproveGas(ctx context.Context, amount *big.Int) (uint64, error) {
	return c.estimateGas(ctx, "approve", c.contractAddr, amount)
}

// estimateTransferGas estimates the gas needed for a transfer tx. The account
// needs to have > amount tokens to use this method.
func (c *tokenContractorV0) estimateTransferGas(ctx context.Context, amount *big.Int) (uint64, error) {
	return c.estimateGas(ctx, "transfer", c.acctAddr, amount)
}

// estimateGas estimates the gas needed for methods on the ERC20 token contract.
// For estimating methods on the swap contract, use (contractorV0).estimateGas.
func (c *tokenContractorV0) estimateGas(ctx context.Context, method string, args ...interface{}) (uint64, error) {
	data, err := erc20.ERC20ABI.Pack(method, args...)
	if err != nil {
		return 0, fmt.Errorf("token estimateGas Pack error: %v", err)
	}

	return c.cb.EstimateGas(ctx, ethereum.CallMsg{
		From: c.acctAddr,
		To:   &c.tokenAddr,
		Data: data,
	})
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

var contractorConstructors = map[uint32]contractorConstructor{
	0: newV0Contractor,
}

var tokenContractorConstructors = map[uint32]tokenContractorConstructor{
	0: newV0TokenContractor,
}
