// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build lgpl
// +build lgpl

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
	"github.com/ethereum/go-ethereum/ethclient"
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
	isRedeemable(secretHash, secret [32]byte) (bool, error)
	// value checks the incoming and outgoing contract value. This is just the
	// sum of redeem, refund, and initiate values. It is not an error if the
	// transaction does not pay to the contract, and the value returned in that
	// case will always be zero.
	value(context.Context, *types.Transaction) (incoming, outgoing uint64, err error)
	isRefundable(secretHash [32]byte) (bool, error)
}

// tokenContractor interacts with an ERC20 token contract.
type tokenContractor interface {
	contractor
	tokenAddress(ctx context.Context) (common.Address, error)
	balance(context.Context) (*big.Int, error)
	allowance(context.Context) (*big.Int, error)
	approve(context.Context, *bind.TransactOpts, *big.Int) (*types.Transaction, error)
	estimateApproveGas(context.Context, *big.Int) (uint64, error)
	transfer(context.Context, *bind.TransactOpts, common.Address, *big.Int) (*types.Transaction, error)
	estimateTransferGas(context.Context, *big.Int) (uint64, error)
}

type contractorConstructor func(net dex.Network, addr common.Address, ec *ethclient.Client) (contractor, error)
type tokenContractorConstructor func(net dex.Network, assetID uint32, acctAddr common.Address, ec *ethclient.Client) (tokenContractor, error)

type contractV0 interface {
	Initiate(opts *bind.TransactOpts, initiations []swapv0.ETHSwapInitiation) (*types.Transaction, error)
	Redeem(opts *bind.TransactOpts, redemptions []swapv0.ETHSwapRedemption) (*types.Transaction, error)
	Swap(opts *bind.CallOpts, secretHash [32]byte) (swapv0.ETHSwapSwap, error)
	Refund(opts *bind.TransactOpts, secretHash [32]byte) (*types.Transaction, error)
	IsRedeemable(opts *bind.CallOpts, secretHash [32]byte, secret [32]byte) (bool, error)
	IsRefundable(opts *bind.CallOpts, secretHash [32]byte) (bool, error)
}

type tokenContractV0 interface {
	contractV0
	TokenAddress(*bind.CallOpts) (common.Address, error)
}

// contractorV0 is the contractor for contract version 0.
// Redeem and Refund methods of swapv0.ETHSwap already have suitable return types.
type contractorV0 struct {
	contractV0   // *swapv0.ETHSwap
	abi          *abi.ABI
	ec           *ethclient.Client
	contractAddr common.Address
	acctAddr     common.Address
	isToken      bool
}

var _ contractor = (*contractorV0)(nil)

func newV0contractor(net dex.Network, acctAddr common.Address, ec *ethclient.Client) (contractor, error) {
	contractAddr, exists := dexeth.ContractAddresses[0][net]
	if !exists || contractAddr == (common.Address{}) {
		return nil, fmt.Errorf("no contract address for version 0, net %s", net)
	}
	c, err := swapv0.NewETHSwap(contractAddr, ec)
	if err != nil {
		return nil, err
	}
	return &contractorV0{
		contractV0:   c,
		abi:          dexeth.ABIs[0],
		ec:           ec,
		contractAddr: contractAddr,
		acctAddr:     acctAddr,
	}, nil
}

func (c *contractorV0) initiate(txOpts *bind.TransactOpts, contracts []*asset.Contract) (*types.Transaction, error) {
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
			Value:           dexeth.GweiToWei(contract.Value),
		})
	}

	return c.contractV0.Initiate(txOpts, inits)
}

func (c *contractorV0) redeem(txOpts *bind.TransactOpts, redemptions []*asset.Redemption) (*types.Transaction, error) {
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

func (c *contractorV0) swap(ctx context.Context, secretHash [32]byte) (*dexeth.SwapState, error) {
	callOpts := &bind.CallOpts{
		Pending: true,
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
		Value:       dexeth.WeiToGwei(state.Value),
		State:       dexeth.SwapStep(state.State),
	}, nil
}

func (c *contractorV0) refund(txOpts *bind.TransactOpts, secretHash [32]byte) (*types.Transaction, error) {
	return c.contractV0.Refund(txOpts, secretHash)
}

func (c *contractorV0) isRedeemable(secretHash, secret [32]byte) (bool, error) {
	return c.contractV0.IsRedeemable(&bind.CallOpts{From: c.acctAddr}, secretHash, secret)
}

func (c *contractorV0) isRefundable(secretHash [32]byte) (bool, error) {
	return c.contractV0.IsRefundable(&bind.CallOpts{From: c.acctAddr}, secretHash)
}

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

func (c *contractorV0) estimateRefundGas(ctx context.Context, secretHash [32]byte) (uint64, error) {
	return c.estimateGas(ctx, nil, "refund", secretHash)
}

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

func (c *contractorV0) estimateGas(ctx context.Context, value *big.Int, method string, args ...interface{}) (uint64, error) {
	data, err := c.abi.Pack(method, args...)
	if err != nil {
		return 0, fmt.Errorf("Pack error: %v", err)
	}

	return c.ec.EstimateGas(ctx, ethereum.CallMsg{
		From:  c.acctAddr,
		To:    &c.contractAddr,
		Data:  data,
		Value: value,
	})
}

func (c *contractorV0) value(ctx context.Context, tx *types.Transaction) (in, out uint64, err error) {
	if *tx.To() != c.contractAddr {
		return 0, 0, nil
	}

	if v, err := c.incomingValue(ctx, tx); err != nil {
		return 0, 0, fmt.Errorf("incomingValue error: %w", err)
	} else if v > 0 {
		return v, 0, nil
	}

	return 0, c.outgoingValue(ctx, tx), nil
}

func (c *contractorV0) incomingValue(ctx context.Context, tx *types.Transaction) (uint64, error) {
	if redeems, err := dexeth.ParseRedeemData(tx.Data(), 0); err == nil {
		var redeemed uint64
		for _, redeem := range redeems {
			swap, err := c.swap(ctx, redeem.SecretHash)
			if err != nil {
				return 0, fmt.Errorf("redeem swap error: %w", err)
			}
			redeemed += swap.Value
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
	return swap.Value, nil
}

func (c *contractorV0) outgoingValue(ctx context.Context, tx *types.Transaction) (swapped uint64) {
	if inits, err := dexeth.ParseInitiateData(tx.Data(), 0); err == nil {
		for _, init := range inits {
			swapped += init.Value
		}
	}
	return
}

// tokenContractorV0 is a contractor that implements the tokenContractor
// methods, providing access to the methods of the token's ERC20 contract.
type tokenContractorV0 struct {
	*contractorV0
	tokenContractor tokenContractV0
	tokenAddr       common.Address
	tokenContract   *bind.BoundContract
}

var _ tokenContractor = (*tokenContractorV0)(nil)

func newV0TokenContractor(net dex.Network, assetID uint32, acctAddr common.Address, ec *ethclient.Client) (tokenContractor, error) {
	_, token, swapContractAddr, err := dexeth.VersionedNetworkToken(assetID, 0, net)
	if err != nil {
		return nil, err
	}

	c, err := erc20v0.NewERC20Swap(swapContractAddr, ec)
	if err != nil {
		return nil, err
	}

	tokenContract := bind.NewBoundContract(token.Address, *erc20.ERC20_ABI, ec, ec, ec)

	return &tokenContractorV0{
		contractorV0: &contractorV0{
			contractV0:   c,
			abi:          erc20.ERC20SwapABIV0,
			ec:           ec,
			contractAddr: swapContractAddr,
			acctAddr:     acctAddr,
			isToken:      true,
		},
		tokenContractor: c,
		tokenAddr:       token.Address,
		tokenContract:   tokenContract,
	}, nil
}

func (c *tokenContractorV0) balance(ctx context.Context) (*big.Int, error) {
	callOpts := &bind.CallOpts{
		Pending: true,
		From:    c.acctAddr,
		Context: ctx,
	}

	var out []interface{}

	err := c.tokenContract.Call(callOpts, &out, "balanceOf", c.acctAddr)
	if err != nil {
		return nil, err
	}

	balance := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)
	return balance, nil
}

func (c *tokenContractorV0) allowance(ctx context.Context) (*big.Int, error) {
	callOpts := &bind.CallOpts{
		Pending: true,
		From:    c.acctAddr,
		Context: ctx,
	}
	var out []interface{}
	err := c.tokenContract.Call(callOpts, &out, "allowance", c.acctAddr, c.contractAddr)
	if err != nil {
		return nil, err
	}

	balance := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)
	return balance, nil
}

func (c *tokenContractorV0) approve(ctx context.Context, txOpts *bind.TransactOpts, amount *big.Int) (*types.Transaction, error) {
	return c.tokenContract.Transact(txOpts, "approve", c.contractAddr, amount)
}

func (c *tokenContractorV0) transfer(ctx context.Context, txOpts *bind.TransactOpts, addr common.Address, amount *big.Int) (*types.Transaction, error) {
	return c.tokenContract.Transact(txOpts, "transfer", addr, amount)
}

func (c *tokenContractorV0) estimateApproveGas(ctx context.Context, amount *big.Int) (uint64, error) {
	return c.estimateGas(ctx, "approve", c.contractAddr, amount)
}

func (c *tokenContractorV0) estimateTransferGas(ctx context.Context, amount *big.Int) (uint64, error) {
	return c.estimateGas(ctx, "transfer", c.acctAddr, amount)
}

func (c *tokenContractorV0) estimateGas(ctx context.Context, method string, args ...interface{}) (uint64, error) {
	data, err := erc20.ERC20_ABI.Pack(method, args...)
	if err != nil {
		return 0, fmt.Errorf("token estimateGas Pack error: %v", err)
	}

	return c.ec.EstimateGas(ctx, ethereum.CallMsg{
		From: c.acctAddr,
		To:   &c.tokenAddr,
		Data: data,
	})
}

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
		return 0, dexeth.WeiToGwei(value), nil
	}

	if _, value, err := erc20.ParseTransferData(tx.Data()); err == nil {
		return 0, dexeth.WeiToGwei(value), nil
	}

	return 0, 0, nil
}

func (c *tokenContractorV0) tokenAddress(ctx context.Context) (common.Address, error) {
	return c.tokenContractor.TokenAddress(&bind.CallOpts{
		Pending: true,
		From:    c.acctAddr,
		Context: ctx,
	})
}

var contractorConstructors = map[uint32]contractorConstructor{
	0: newV0contractor,
}

var tokenContractorConstructors = map[uint32]tokenContractorConstructor{
	0: newV0TokenContractor,
}
