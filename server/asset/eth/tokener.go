// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

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
	swapv1 "decred.org/dcrdex/dex/networks/eth/contracts/v1"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
)

// swapContract is a generic source of swap contract data.
type swapContract interface {
	status(ctx context.Context, token common.Address, locator []byte) (*dexeth.SwapStatus, error)
	vector(ctx context.Context, locator []byte) (*dexeth.SwapVector, error)
	statusAndVector(ctx context.Context, locator []byte) (*dexeth.SwapStatus, *dexeth.SwapVector, error)
}

// erc2Contract exposes methods of a token's ERC20 contract.
type erc20Contract interface {
	BalanceOf(*bind.CallOpts, common.Address) (*big.Int, error)
}

// tokener is a contract data manager for a token.
type tokener struct {
	*VersionedToken
	swapContract
	erc20Contract
	contractAddr, tokenAddr common.Address
}

// newTokener is a constructor for a tokener.
func newTokener(
	ctx context.Context,
	assetID uint32,
	vToken *VersionedToken,
	net dex.Network,
	be bind.ContractBackend,
	v1addr common.Address,
) (*tokener, error) {

	netToken, contract, err := networkToken(vToken, net)
	if err != nil {
		return nil, err
	}

	var sc swapContract
	switch vToken.ContractVersion {
	case 0:
		es, err := erc20v0.NewERC20Swap(contract.Address, be)
		if err != nil {
			return nil, err
		}
		sc = &swapSourceV0{es}

		boundAddr, err := es.TokenAddress(readOnlyCallOpts(ctx))
		if err != nil {
			return nil, fmt.Errorf("error retrieving bound address for %s version %d contract: %w",
				vToken.Name, vToken.ContractVersion, err)
		}

		if boundAddr != netToken.Address {
			return nil, fmt.Errorf("wrong bound address for %s version %d contract. wanted %s, got %s",
				vToken.Name, vToken.ContractVersion, netToken.Address, boundAddr)
		}
	case 1:
		es, err := swapv1.NewETHSwap(v1addr, be)
		if err != nil {
			return nil, err
		}
		sc = &swapSourceV1{contract: es, tokenAddr: netToken.Address}
	default:
		return nil, fmt.Errorf("unsupported contract version %d", vToken.ContractVersion)
	}

	erc20, err := erc20.NewIERC20(netToken.Address, be)
	if err != nil {
		return nil, err
	}

	tkn := &tokener{
		VersionedToken: vToken,
		swapContract:   sc,
		erc20Contract:  erc20,
		contractAddr:   contract.Address,
		tokenAddr:      netToken.Address,
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
	inits, err := dexeth.ParseInitiateDataV0(txData)
	if err != nil {
		return nil
	}
	v := new(big.Int)
	for _, init := range inits {
		v.Add(v, init.Value)
	}
	return v
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

// swap gets the swap state for the secretHash on the version 0 contract.
func (s *swapSourceV0) swap(ctx context.Context, secretHash [32]byte) (*dexeth.SwapState, error) {
	state, err := s.contract.Swap(readOnlyCallOpts(ctx), secretHash)
	if err != nil {
		return nil, fmt.Errorf("swap error: %w", err)
	}
	return dexeth.SwapStateFromV0(&state), nil
}

// status fetches the SwapStatus, which specifies the current state of mutable
// swap data.
func (s *swapSourceV0) status(ctx context.Context, _ common.Address, locator []byte) (*dexeth.SwapStatus, error) {
	secretHash, err := dexeth.ParseV0Locator(locator)
	if err != nil {
		return nil, err
	}
	swap, err := s.swap(ctx, secretHash)
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
func (s *swapSourceV0) vector(ctx context.Context, locator []byte) (*dexeth.SwapVector, error) {
	secretHash, err := dexeth.ParseV0Locator(locator)
	if err != nil {
		return nil, err
	}
	swap, err := s.swap(ctx, secretHash)
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
func (s *swapSourceV0) statusAndVector(ctx context.Context, locator []byte) (*dexeth.SwapStatus, *dexeth.SwapVector, error) {
	secretHash, err := dexeth.ParseV0Locator(locator)
	if err != nil {
		return nil, nil, err
	}
	swap, err := s.swap(ctx, secretHash)
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

type swapContractV1 interface {
	Status(opts *bind.CallOpts, token common.Address, c swapv1.ETHSwapVector) (swapv1.ETHSwapStatus, error)
}

type swapSourceV1 struct {
	contract  swapContractV1 // *swapv0.ETHSwap or *erc20v0.ERCSwap
	tokenAddr common.Address
}

func (s *swapSourceV1) status(ctx context.Context, token common.Address, locator []byte) (*dexeth.SwapStatus, error) {
	v, err := dexeth.ParseV1Locator(locator)
	if err != nil {
		return nil, err
	}
	rec, err := s.contract.Status(readOnlyCallOpts(ctx), token, dexeth.SwapVectorToAbigen(v))
	if err != nil {
		return nil, err
	}
	return &dexeth.SwapStatus{
		Step:        dexeth.SwapStep(rec.Step),
		Secret:      rec.Secret,
		BlockHeight: rec.BlockNumber.Uint64(),
	}, err
}

func (s *swapSourceV1) vector(ctx context.Context, locator []byte) (*dexeth.SwapVector, error) {
	return dexeth.ParseV1Locator(locator)
}

func (s *swapSourceV1) statusAndVector(ctx context.Context, locator []byte) (*dexeth.SwapStatus, *dexeth.SwapVector, error) {
	v, err := dexeth.ParseV1Locator(locator)
	if err != nil {
		return nil, nil, err
	}

	rec, err := s.contract.Status(readOnlyCallOpts(ctx), s.tokenAddr, dexeth.SwapVectorToAbigen(v))
	if err != nil {
		return nil, nil, err
	}
	return &dexeth.SwapStatus{
		Step:        dexeth.SwapStep(rec.Step),
		Secret:      rec.Secret,
		BlockHeight: rec.BlockNumber.Uint64(),
	}, v, err
}

func (s *swapSourceV1) Status(ctx context.Context, locator []byte) (*dexeth.SwapStatus, error) {
	vec, err := dexeth.ParseV1Locator(locator)
	if err != nil {
		return nil, err
	}

	status, err := s.contract.Status(readOnlyCallOpts(ctx), s.tokenAddr, dexeth.SwapVectorToAbigen(vec))
	if err != nil {
		return nil, err
	}

	return &dexeth.SwapStatus{
		Step:        dexeth.SwapStep(status.Step),
		Secret:      status.Secret,
		BlockHeight: status.BlockNumber.Uint64(),
	}, err
}

// readOnlyCallOpts is the CallOpts used for read-only contract method calls.
func readOnlyCallOpts(ctx context.Context) *bind.CallOpts {
	return &bind.CallOpts{
		Context: ctx,
	}
}
