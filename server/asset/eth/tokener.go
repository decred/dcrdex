// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build lgpl

package eth

import (
	"context"
	"fmt"
	"math/big"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/networks/erc20"
	erc20v1 "decred.org/dcrdex/dex/networks/erc20/contracts/v1"
	dexeth "decred.org/dcrdex/dex/networks/eth"
	swapv1 "decred.org/dcrdex/dex/networks/eth/contracts/v1"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
)

// swapContract is a generic source of swap contract data.
type swapContract interface {
	Status(context.Context, *dex.SwapContractDetails) (step dexeth.SwapStep, secret [32]byte, blockNum uint32, err error)
}

// erc2Contract exposes methods of a token's ERC20 contract.
type erc20Contract interface {
	BalanceOf(*bind.CallOpts, common.Address) (*big.Int, error)
}

// tokener is a contract data manager for a token.
type tokener struct {
	*registeredToken
	swapContract
	erc20Contract
	contractAddr, tokenAddr common.Address
}

// newTokener is a constructor for a tokener.
func newTokener(ctx context.Context, assetID uint32, net dex.Network, be bind.ContractBackend) (*tokener, error) {
	token, netToken, swapContract, err := networkToken(assetID, net)
	if err != nil {
		return nil, err
	}

	if token.ver != 1 {
		return nil, fmt.Errorf("only version 0 contracts supported")
	}

	es, err := erc20v1.NewERC20Swap(swapContract.Address, be)
	if err != nil {
		return nil, err
	}

	erc20, err := erc20.NewIERC20(netToken.Address, be)
	if err != nil {
		return nil, err
	}

	boundAddr, err := es.TokenAddress(readOnlyCallOpts(ctx, false))
	if err != nil {
		return nil, fmt.Errorf("error retrieving bound address for %s version %d contract: %w",
			token.Name, token.ver, err)
	}

	if boundAddr != netToken.Address {
		return nil, fmt.Errorf("wrong bound address for %s version %d contract. wanted %s, got %s",
			token.Name, token.ver, netToken.Address, boundAddr)
	}

	tkn := &tokener{
		registeredToken: token,
		swapContract:    &swapSourceV1{es},
		erc20Contract:   erc20,
		contractAddr:    swapContract.Address,
		tokenAddr:       netToken.Address,
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
	contracts, err := dexeth.ParseInitiateDataV1(txData)
	if err != nil {
		return nil
	}
	v := new(big.Int)
	for _, c := range contracts {
		v.Add(v, dexeth.GweiToWei(c.Value))
	}
	return v
}

// balanceOf checks the account's token balance.
func (t *tokener) balanceOf(ctx context.Context, addr common.Address) (*big.Int, error) {
	return t.BalanceOf(readOnlyCallOpts(ctx, false), addr)
}

// swapContractV1 represents a version 0 swap contract for ETH or a token.
type swapContractV1 interface {
	State(*bind.CallOpts, swapv1.ETHSwapContract) (swapv1.ETHSwapRecord, error)
}

// swapSourceV1 wraps a swapContractV1 and translates the swap data to satisfy
// swapSource.
type swapSourceV1 struct {
	contract swapContractV1 // *swapv1.ETHSwap or *erc20v1.ERCSwap
}

// Status translates the version 0 swap data to the more general SwapState to
// satisfy the swapSource interface.
func (s *swapSourceV1) Status(ctx context.Context, deets *dex.SwapContractDetails) (step dexeth.SwapStep, secret [32]byte, blockNum uint32, err error) {
	rec, err := s.contract.State(readOnlyCallOpts(ctx, true), dexeth.SwapToV1(deets))
	if err != nil {
		return
	}
	return dexeth.SwapStep(rec.State), rec.Secret, uint32(rec.BlockNumber.Uint64()), nil
}

// readOnlyCallOpts is the CallOpts used for read-only contract method calls.
func readOnlyCallOpts(ctx context.Context, includePending bool) *bind.CallOpts {
	return &bind.CallOpts{
		Pending: includePending,
		Context: ctx,
	}
}
