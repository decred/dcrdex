// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build lgpl
// +build lgpl

package eth

import (
	"context"
	"fmt"
	"math/big"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/networks/erc20"
	erc20v0 "decred.org/dcrdex/dex/networks/erc20/contracts/v0"
	dexeth "decred.org/dcrdex/dex/networks/eth"
	swapv0 "decred.org/dcrdex/dex/networks/eth/contracts/v0"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
)

// swapContract is a generic source of swap contract data.
type swapContract interface {
	Swap(context.Context, [32]byte) (*dexeth.SwapState, error)
}

// tokenAddresser exposes the TokenAddress method of a token swap contract.
type tokenAddresser interface {
	TokenAddress(*bind.CallOpts) (common.Address, error)
}

// erc2Contract exposes methods of a token's ERC20 contract.
type erc20Contract interface {
	BalanceOf(*bind.CallOpts, common.Address) (*big.Int, error)
}

// tokener is a contract data manager for a token.
type tokener struct {
	*registeredToken
	swapContract
	tokenAddresser
	erc20Contract
	ver                     uint32
	contractAddr, tokenAddr common.Address
}

// newTokener is a constructor for a tokener.
func newTokener(ctx context.Context, assetID uint32, net dex.Network, be bind.ContractBackend) (*tokener, error) {
	token, netToken, swapContract, err := networkToken(assetID, net)
	if err != nil {
		return nil, err
	}

	if token.ver != 0 {
		return nil, fmt.Errorf("only version 0 contracts supported")
	}

	es, err := erc20v0.NewERC20Swap(swapContract.Address, be)
	if err != nil {
		return nil, err
	}

	erc20, err := erc20.NewIERC20(netToken.Address, be)
	if err != nil {
		return nil, err
	}

	tkn := &tokener{
		ver:             0,
		registeredToken: token,
		swapContract:    &swapSourceV0{es},
		tokenAddresser:  es,
		erc20Contract:   erc20,
		contractAddr:    swapContract.Address,
		tokenAddr:       netToken.Address,
	}

	if boundAddr, err := tkn.tokenAddress(ctx); err != nil {
		return nil, fmt.Errorf("error retrieving bound address for %s version %d contract: %w",
			token.Name, token.ver, err)
	} else if boundAddr != netToken.Address {
		return nil, fmt.Errorf("wrong bound address for %s version %d contract. wanted %s, got %s",
			token.Name, token.ver, netToken.Address, boundAddr)
	}

	return tkn, nil
}

// transferred calculates the value transferred using the token contract's
// transfer method.
func (t *tokener) transferred(txData []byte) *big.Int {
	_, out, err := erc20.ParseTransferData(txData)
	if err != nil {
		return nil
	}
	return out
}

// swapped calculates the value sent to the swap contracts initiate method.
func (t *tokener) swapped(txData []byte) *big.Int {
	inits, err := dexeth.ParseInitiateData(txData, t.ver)
	if err != nil {
		return nil
	}
	var v uint64
	for _, init := range inits {
		v += init.Value
	}
	return dexeth.GweiToWei(v)
}

// tokenAddress fetches the token address bound to the swap contract.
func (t *tokener) tokenAddress(ctx context.Context) (common.Address, error) {
	return t.tokenAddresser.TokenAddress(readOnlyCallOpts(ctx))
}

// balanceOf checks the account's token balance.
func (t *tokener) balanceOf(ctx context.Context, addr common.Address) (*big.Int, error) {
	return t.BalanceOf(readOnlyCallOpts(ctx), addr)
}

// swapContractV0 represents a version 0 swap contract for ETH or a token.
type swapContractV0 interface {
	Swap(opts *bind.CallOpts, secretHash [32]byte) (swapv0.ETHSwapSwap, error)
}

// swapSourceV0 wraps a swapContractV0 and translates the swap data to satisfy
// swapSource.
type swapSourceV0 struct {
	contract swapContractV0 // *swapv0.ETHSwap or *erc20v0.ERCSwap
}

// Swap translates the version 0 swap data to the more general SwapState to
// satisfy the swapSource interface.
func (s *swapSourceV0) Swap(ctx context.Context, secretHash [32]byte) (*dexeth.SwapState, error) {
	state, err := s.contract.Swap(readOnlyCallOpts(ctx), secretHash)
	if err != nil {
		return nil, fmt.Errorf("Swap error: %w", err)
	}
	return dexeth.SwapStateFromV0(&state), nil
}

// readOnlyCallOpts is the CallOpts used for read-only contract method calls.
func readOnlyCallOpts(ctx context.Context) *bind.CallOpts {
	return &bind.CallOpts{
		Pending: true,
		Context: ctx,
	}
}
