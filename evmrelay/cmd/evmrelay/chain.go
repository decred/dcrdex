// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package main

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"fmt"
	"math/big"
	"sync"

	"decred.org/dcrdex/dex"
	basenet "decred.org/dcrdex/dex/networks/base"
	dexeth "decred.org/dcrdex/dex/networks/eth"
	dexpolygon "decred.org/dcrdex/dex/networks/polygon"
	"decred.org/dcrdex/evmrelay"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

// chainClient manages a connection to a single EVM chain.
type chainClient struct {
	chainID        *big.Int
	ec             *ethclient.Client
	privKey        *ecdsa.PrivateKey
	relayAddr      common.Address
	signer         types.Signer
	gases          *dexeth.Gases
	isOpStack      bool
	profitPerGas   *big.Int
	allowedTargets map[common.Address]bool

	txMtx sync.Mutex
}

type preparedTx struct {
	tx    *types.Transaction
	rawTx []byte
	hash  common.Hash
	nonce uint64
}

// newChainClient creates a new chain client. The chainID is the routing key
// used to index this chain (mainnet chain ID on simnet). The signingChainID
// is the actual chain ID used for transaction signing.
func newChainClient(ctx context.Context, chainID, signingChainID int64, rpcURL string, privKey *ecdsa.PrivateKey, profitPerGas *big.Int, allowedTargets map[common.Address]bool) (*chainClient, error) {
	ec, err := ethclient.DialContext(ctx, rpcURL)
	if err != nil {
		return nil, fmt.Errorf("error connecting to chain %d: %w", chainID, err)
	}

	gases, ok := dexeth.VersionedGases[1]
	if !ok {
		return nil, fmt.Errorf("no gas table for contract version 1")
	}

	// isOpStack uses the routing chainID — works for mainnet (8453),
	// testnet (84532), and simnet (routing key 8453).
	isOpStack := chainID == basenet.MainnetChainID ||
		chainID == basenet.TestnetChainID

	cid := big.NewInt(signingChainID)
	client := &chainClient{
		chainID:        cid,
		ec:             ec,
		privKey:        privKey,
		relayAddr:      crypto.PubkeyToAddress(privKey.PublicKey),
		signer:         types.NewLondonSigner(cid),
		gases:          gases,
		isOpStack:      isOpStack,
		profitPerGas:   profitPerGas,
		allowedTargets: allowedTargets,
	}

	return client, nil
}

// close closes the underlying ethclient connection.
func (c *chainClient) close() {
	c.ec.Close()
}

// checkHealth verifies the chain RPC is reachable by fetching the latest block number.
func (c *chainClient) checkHealth(ctx context.Context) error {
	_, err := c.ec.BlockNumber(ctx)
	return err
}

// estimateFee calculates the relay fee for the given number of redemptions
// using gas table values and current network conditions.
func (c *chainClient) estimateFee(ctx context.Context, numRedemptions int) (*big.Int, error) {
	hdr, err := c.ec.HeaderByNumber(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("error getting latest header: %w", err)
	}

	networkTip, err := c.ec.SuggestGasTipCap(ctx)
	if err != nil {
		return nil, fmt.Errorf("error getting gas tip cap: %w", err)
	}

	var l1Fee *big.Int
	if c.isOpStack {
		syntheticCalldata, err := evmrelay.MockSignedRedeemCalldata(numRedemptions)
		if err != nil {
			return nil, fmt.Errorf("error generating synthetic calldata: %w", err)
		}
		l1Fee, err = basenet.L1FeeForCalldata(ctx, c.ec, syntheticCalldata)
		if err != nil {
			return nil, fmt.Errorf("error getting L1 fee: %w", err)
		}
	}

	return evmrelay.EstimateRelayFee(
		numRedemptions, c.gases.SignedRedeem, c.gases.SignedRedeemAdd,
		hdr.BaseFee, networkTip, c.profitPerGas, l1Fee,
	), nil
}

// validateFee checks that the relayer fee in the calldata is sufficient
// for the relay to submit the transaction profitably. It returns the baseFee and networkTip used.
func (c *chainClient) validateFee(ctx context.Context, calldata []byte, parsed *dexeth.SignedRedemptionV1) (*big.Int, *big.Int, error) {
	hdr, err := c.ec.HeaderByNumber(ctx, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("error getting latest header: %w", err)
	}

	networkTip, err := c.ec.SuggestGasTipCap(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("error getting gas tip cap: %w", err)
	}

	var l1Fee *big.Int
	if c.isOpStack {
		l1Fee, err = basenet.L1FeeForCalldata(ctx, c.ec, calldata)
		if err != nil {
			return nil, nil, fmt.Errorf("error getting L1 fee: %w", err)
		}
	}

	minFee := evmrelay.EstimateRelayFeeWithMultipliers(
		parsed.NumRedemptions, c.gases.SignedRedeem, c.gases.SignedRedeemAdd,
		hdr.BaseFee, networkTip, c.profitPerGas, l1Fee,
		evmrelay.ValidateBaseFeeMultNum, evmrelay.ValidateBaseFeeMultDen,
		evmrelay.L1FeeValidateMultNum, evmrelay.L1FeeValidateMultDen,
	)

	if parsed.RelayerFee.Cmp(minFee) < 0 {
		return nil, nil, &feeTooLowError{
			Need: new(big.Int).Set(minFee),
			Got:  new(big.Int).Set(parsed.RelayerFee),
		}
	}

	// The on-chain contract enforces relayerFee <= total / 2. Reject here
	// to avoid wasting gas on transactions that would revert.
	halfTotal := new(big.Int).Div(parsed.TotalRedeemed, big.NewInt(2))
	if parsed.RelayerFee.Cmp(halfTotal) > 0 {
		return nil, nil, &feeExceedsRedeemedValueError{
			Fee:   new(big.Int).Set(parsed.RelayerFee),
			Total: new(big.Int).Set(parsed.TotalRedeemed),
		}
	}

	return hdr.BaseFee, networkTip, nil
}

func (c *chainClient) prepareRedeemTx(ctx context.Context, to common.Address, calldata []byte, parsed *dexeth.SignedRedemptionV1, baseFee, tipCap *big.Int) (*preparedTx, error) {
	expectedGas := c.gases.SignedRedeemN(parsed.NumRedemptions)

	gasEstimate, err := c.ec.EstimateGas(ctx, ethereum.CallMsg{
		From: c.relayAddr,
		To:   &to,
		Data: calldata,
	})
	if err != nil {
		return nil, fmt.Errorf("gas estimation failed: %w", err)
	}

	if gasEstimate > expectedGas {
		return nil, &gasEstimateTooHighError{
			GasEstimate: gasEstimate,
			ExpectedGas: expectedGas,
		}
	}

	c.txMtx.Lock()
	defer c.txMtx.Unlock()

	nonce, err := c.ec.PendingNonceAt(ctx, c.relayAddr)
	if err != nil {
		return nil, fmt.Errorf("error getting pending nonce: %w", err)
	}

	// GasFeeCap = baseFee * 2 + gasTipCap
	gasFeeCap := new(big.Int).Mul(baseFee, big.NewInt(2))
	gasFeeCap.Add(gasFeeCap, tipCap)

	tx := types.NewTx(&types.DynamicFeeTx{
		ChainID:   c.chainID,
		Nonce:     nonce,
		GasTipCap: tipCap,
		GasFeeCap: gasFeeCap,
		Gas:       expectedGas,
		To:        &to,
		Data:      calldata,
	})

	signedTx, err := types.SignTx(tx, c.signer, c.privKey)
	if err != nil {
		return nil, fmt.Errorf("error signing transaction: %w", err)
	}

	rawTx, err := signedTx.MarshalBinary()
	if err != nil {
		return nil, fmt.Errorf("error marshaling transaction: %w", err)
	}

	return &preparedTx{
		tx:    signedTx,
		rawTx: rawTx,
		hash:  signedTx.Hash(),
		nonce: nonce,
	}, nil
}

func (c *chainClient) prepareRedeemReplacementTx(ctx context.Context, to common.Address, calldata []byte, parsed *dexeth.SignedRedemptionV1, baseFee, tipCap *big.Int, nonce uint64, prevTx *types.Transaction) (*preparedTx, error) {
	expectedGas := c.gases.SignedRedeemN(parsed.NumRedemptions)

	gasEstimate, err := c.ec.EstimateGas(ctx, ethereum.CallMsg{
		From: c.relayAddr,
		To:   &to,
		Data: calldata,
	})
	if err != nil {
		return nil, fmt.Errorf("gas estimation failed: %w", err)
	}

	if gasEstimate > expectedGas {
		return nil, &gasEstimateTooHighError{
			GasEstimate: gasEstimate,
			ExpectedGas: expectedGas,
		}
	}

	gasFeeCap, gasTipCap := replacementFeeCaps(baseFee, tipCap, prevTx)

	tx := types.NewTx(&types.DynamicFeeTx{
		ChainID:   c.chainID,
		Nonce:     nonce,
		GasTipCap: gasTipCap,
		GasFeeCap: gasFeeCap,
		Gas:       expectedGas,
		To:        &to,
		Data:      calldata,
	})

	signedTx, err := types.SignTx(tx, c.signer, c.privKey)
	if err != nil {
		return nil, fmt.Errorf("error signing replacement transaction: %w", err)
	}

	rawTx, err := signedTx.MarshalBinary()
	if err != nil {
		return nil, fmt.Errorf("error marshaling replacement transaction: %w", err)
	}

	return &preparedTx{
		tx:    signedTx,
		rawTx: rawTx,
		hash:  signedTx.Hash(),
		nonce: nonce,
	}, nil
}

func (c *chainClient) prepareCancellationTx(ctx context.Context, nonce uint64, prevTx *types.Transaction) (*preparedTx, error) {
	hdr, err := c.ec.HeaderByNumber(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("error getting latest header: %w", err)
	}

	tipCap, err := c.ec.SuggestGasTipCap(ctx)
	if err != nil {
		return nil, fmt.Errorf("error getting gas tip cap: %w", err)
	}

	gasFeeCap, gasTipCap := replacementFeeCaps(hdr.BaseFee, tipCap, prevTx)

	tx := types.NewTx(&types.DynamicFeeTx{
		ChainID:   c.chainID,
		Nonce:     nonce,
		GasTipCap: gasTipCap,
		GasFeeCap: gasFeeCap,
		Gas:       21000,
		To:        &c.relayAddr,
		Value:     big.NewInt(0),
	})

	signedTx, err := types.SignTx(tx, c.signer, c.privKey)
	if err != nil {
		return nil, fmt.Errorf("error signing cancellation transaction: %w", err)
	}

	rawTx, err := signedTx.MarshalBinary()
	if err != nil {
		return nil, fmt.Errorf("error marshaling cancellation transaction: %w", err)
	}

	return &preparedTx{
		tx:    signedTx,
		rawTx: rawTx,
		hash:  signedTx.Hash(),
		nonce: nonce,
	}, nil
}

func preparedTxFromRaw(rawTx []byte) (*preparedTx, error) {
	tx := new(types.Transaction)
	if err := tx.UnmarshalBinary(rawTx); err != nil {
		return nil, fmt.Errorf("error decoding raw transaction: %w", err)
	}

	return &preparedTx{
		tx:    tx,
		rawTx: append([]byte(nil), rawTx...),
		hash:  tx.Hash(),
		nonce: tx.Nonce(),
	}, nil
}

func (c *chainClient) sendPreparedTx(ctx context.Context, tx *preparedTx) error {
	if err := c.ec.SendTransaction(ctx, tx.tx); err != nil {
		return fmt.Errorf("error sending transaction: %w", err)
	}
	return nil
}

func replacementFeeCaps(baseFee, tipCap *big.Int, prevTx *types.Transaction) (gasFeeCap, gasTipCap *big.Int) {
	gasTipCap = new(big.Int).Set(tipCap)
	gasFeeCap = new(big.Int).Mul(baseFee, big.NewInt(2))
	gasFeeCap.Add(gasFeeCap, gasTipCap)

	if prevTx == nil {
		return gasFeeCap, gasTipCap
	}

	gasTipCap = maxBigInt(gasTipCap, bumpReplacementValue(prevTx.GasTipCap()))
	gasFeeCap = maxBigInt(gasFeeCap, bumpReplacementValue(prevTx.GasFeeCap()))
	return gasFeeCap, gasTipCap
}

func bumpReplacementValue(v *big.Int) *big.Int {
	if v == nil || v.Sign() <= 0 {
		return big.NewInt(1)
	}
	bumped := new(big.Int).Mul(v, big.NewInt(11))
	bumped.Div(bumped, big.NewInt(10))
	if bumped.Cmp(v) <= 0 {
		bumped = new(big.Int).Add(v, big.NewInt(1))
	}
	return bumped
}

func maxBigInt(a, b *big.Int) *big.Int {
	if a == nil {
		return new(big.Int).Set(b)
	}
	if b == nil {
		return new(big.Int).Set(a)
	}
	if a.Cmp(b) >= 0 {
		return new(big.Int).Set(a)
	}
	return new(big.Int).Set(b)
}

func isTxNotFound(err error) bool {
	return errors.Is(err, ethereum.NotFound)
}

// buildAllowedTargets creates a mapping from chain ID to the set of allowed
// contract addresses for all supported EVM chains on the given network. On
// simnet, mainnet chain IDs are used as routing keys.
func buildAllowedTargets(net dex.Network) map[int64]map[common.Address]bool {
	m := make(map[int64]map[common.Address]bool)
	addTargets := func(chainIDs map[dex.Network]int64, mainnetChainID int64, contracts map[uint32]map[dex.Network]common.Address) {
		routingID := chainIDs[net]
		if net == dex.Simnet {
			routingID = mainnetChainID
		}
		for _, netAddrs := range contracts {
			addr, ok := netAddrs[net]
			if !ok || addr == (common.Address{}) {
				continue
			}
			if m[routingID] == nil {
				m[routingID] = make(map[common.Address]bool)
			}
			m[routingID][addr] = true
		}
	}
	addTargets(dexeth.ChainIDs, dexeth.MainnetChainID, dexeth.ContractAddresses)
	addTargets(dexpolygon.ChainIDs, dexpolygon.MainnetChainID, dexpolygon.ContractAddresses)
	addTargets(basenet.ChainIDs, basenet.MainnetChainID, basenet.ContractAddresses)
	return m
}
