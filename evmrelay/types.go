// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package evmrelay

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"time"

	dexeth "decred.org/dcrdex/dex/networks/eth"
	swapv1 "decred.org/dcrdex/dex/networks/eth/contracts/v1"
	"github.com/ethereum/go-ethereum/common"
)

type RelayState string

const (
	// MaxSignedRedeemBatch is the maximum redemptions allowed in a signed redeem.
	MaxSignedRedeemBatch = 20

	// Relay task state string constants, used by both the server and client.
	RelayStatePending RelayState = "pending"
	RelayStateSuccess RelayState = "success"
	RelayStateFailed  RelayState = "failed"

	// Fee estimation multipliers. The estimate uses a higher baseFee
	// multiplier to provide buffer for baseFee increases between estimation
	// and block inclusion.
	EstimateBaseFeeMultNum = 22 // 2.2x baseFee for estimates
	EstimateBaseFeeMultDen = 10

	// Fee validation multipliers. Lower than estimation — the client
	// signed with the higher estimated fee.
	ValidateBaseFeeMultNum = 20 // 2.0x baseFee for validation
	ValidateBaseFeeMultDen = 10

	// L1 data fee multipliers for OP Stack chains (Base).
	L1FeeEstimateMultNum = 13 // 1.3x for estimates
	L1FeeEstimateMultDen = 10
	L1FeeValidateMultNum = 12 // 1.2x for validation
	L1FeeValidateMultDen = 10
)

func (s RelayState) Valid() bool {
	switch s {
	case RelayStatePending, RelayStateSuccess, RelayStateFailed:
		return true
	default:
		return false
	}
}

func (s *RelayState) UnmarshalJSON(b []byte) error {
	var str string
	if err := json.Unmarshal(b, &str); err != nil {
		return err
	}
	state := RelayState(str)
	if !state.Valid() {
		return fmt.Errorf("unknown relay state %q", str)
	}
	*s = state
	return nil
}

// MockSignedRedeemCalldata builds conservative synthetic redeemWithSignature
// calldata for OP Stack L1 fee estimation. It uses the real ABI to get the
// exact calldata length, then randomizes the payload bytes after the selector
// so the result is hard to compress.
func MockSignedRedeemCalldata(numRedemptions int) ([]byte, error) {
	if numRedemptions <= 0 || numRedemptions > MaxSignedRedeemBatch {
		return nil, fmt.Errorf("numRedemptions must be between 1 and %d", MaxSignedRedeemBatch)
	}

	redemptions := make([]swapv1.ETHSwapRedemption, numRedemptions)
	for i := range redemptions {
		if _, err := rand.Read(redemptions[i].V.SecretHash[:]); err != nil {
			return nil, fmt.Errorf("error generating redemption secret hash: %w", err)
		}
		if _, err := rand.Read(redemptions[i].V.Initiator[:]); err != nil {
			return nil, fmt.Errorf("error generating redemption initiator: %w", err)
		}
		if _, err := rand.Read(redemptions[i].V.Participant[:]); err != nil {
			return nil, fmt.Errorf("error generating redemption participant: %w", err)
		}
		redemptions[i].V.Value = new(big.Int).SetUint64(1e18)
		redemptions[i].V.RefundTimestamp = uint64(time.Now().Unix())
		if _, err := rand.Read(redemptions[i].Secret[:]); err != nil {
			return nil, fmt.Errorf("error generating redemption secret: %w", err)
		}
	}

	var feeRecipient common.Address
	if _, err := rand.Read(feeRecipient[:]); err != nil {
		return nil, fmt.Errorf("error generating fee recipient: %w", err)
	}

	sig := make([]byte, 65)
	if _, err := rand.Read(sig); err != nil {
		return nil, fmt.Errorf("error generating signature: %w", err)
	}

	calldata, err := dexeth.ABIs[1].Pack(dexeth.RedeemWithSignatureMethodName,
		redemptions, feeRecipient, new(big.Int), new(big.Int), new(big.Int), sig,
	)
	if err != nil {
		return nil, err
	}

	if len(calldata) > 4 {
		if _, err := rand.Read(calldata[4:]); err != nil {
			return nil, fmt.Errorf("error randomizing calldata payload: %w", err)
		}
	}

	return calldata, nil
}

// RelayRequest is the JSON body for POST /api/relay.
type RelayRequest struct {
	ChainID  int64  `json:"chainID"`
	Target   string `json:"target"`
	Calldata string `json:"calldata"`
}

// EstimateFeeRequest is the JSON body for POST /api/estimatefee.
type EstimateFeeRequest struct {
	ChainID        int64  `json:"chainID"`
	Target         string `json:"target"`
	NumRedemptions int    `json:"numRedemptions"`
}

// EstimateFeeResponse is the JSON response from POST /api/estimatefee.
type EstimateFeeResponse struct {
	RelayAddr string `json:"relayAddr"`
	Fee       string `json:"fee"`
}

// HealthResponse is the JSON response from GET /api/health.
type HealthResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// SubmitResponse is the JSON response from POST /api/relay.
type SubmitResponse struct {
	TaskID string `json:"taskID,omitempty"`
	Error  string `json:"error,omitempty"`
}

// StatusResponse is the JSON response from GET /api/relay/{taskID}.
type StatusResponse struct {
	State RelayState `json:"state"`
	// TxHash is set only when the relay task succeeded on chain.
	TxHash        string `json:"txHash"`
	FailureReason string `json:"failureReason,omitempty"`
	Error         string `json:"error,omitempty"`
}

// EstimateRelayFeeWithMultipliers calculates the total relay fee for a signed
// redemption with explicit baseFee and L1 fee multipliers. The caller passes
// raw baseFee and l1Fee; the multipliers are applied internally.
func EstimateRelayFeeWithMultipliers(numRedemptions int, gasSignedRedeem, gasSignedRedeemAdd uint64,
	baseFee, maxTipCap, relayerTip, l1Fee *big.Int,
	baseFeeMultNum, baseFeeMultDen, l1FeeMultNum, l1FeeMultDen int64) *big.Int {

	gas := gasSignedRedeem + gasSignedRedeemAdd*uint64(max(numRedemptions-1, 0))

	// Apply baseFee multiplier.
	adjBaseFee := new(big.Int).Mul(baseFee, big.NewInt(baseFeeMultNum))
	adjBaseFee.Div(adjBaseFee, big.NewInt(baseFeeMultDen))

	// fee = (adjBaseFee + maxTipCap + relayerTip) * gas + adjL1Fee
	feePerGas := new(big.Int).Add(adjBaseFee, maxTipCap)
	feePerGas.Add(feePerGas, relayerTip)

	totalFee := new(big.Int).Mul(feePerGas, new(big.Int).SetUint64(gas))
	if l1Fee != nil {
		adjL1Fee := new(big.Int).Mul(l1Fee, big.NewInt(l1FeeMultNum))
		adjL1Fee.Div(adjL1Fee, big.NewInt(l1FeeMultDen))
		totalFee.Add(totalFee, adjL1Fee)
	}
	return totalFee
}

// EstimateRelayFee calculates the total relay fee using the default estimation
// multipliers (2.2x baseFee, 1.3x L1 fee). Callers pass raw baseFee and l1Fee.
func EstimateRelayFee(numRedemptions int, gasSignedRedeem, gasSignedRedeemAdd uint64,
	baseFee, maxTipCap, relayerTip, l1Fee *big.Int) *big.Int {

	return EstimateRelayFeeWithMultipliers(numRedemptions, gasSignedRedeem, gasSignedRedeemAdd,
		baseFee, maxTipCap, relayerTip, l1Fee,
		EstimateBaseFeeMultNum, EstimateBaseFeeMultDen,
		L1FeeEstimateMultNum, L1FeeEstimateMultDen)
}

// ExtractRelayerTip back-calculates the relayer's per-gas profit from a total
// fee. Applies the default estimation multipliers (2.2x baseFee, 1.3x L1 fee)
// internally. Callers pass raw baseFee and l1Fee.
func ExtractRelayerTip(totalFee *big.Int, numRedemptions int,
	gasSignedRedeem, gasSignedRedeemAdd uint64,
	baseFee, maxTipCap, l1Fee *big.Int) *big.Int {

	gas := gasSignedRedeem + gasSignedRedeemAdd*uint64(max(numRedemptions-1, 0))

	// Apply baseFee multiplier.
	adjBaseFee := new(big.Int).Mul(baseFee, big.NewInt(EstimateBaseFeeMultNum))
	adjBaseFee.Div(adjBaseFee, big.NewInt(EstimateBaseFeeMultDen))

	// Apply L1 fee multiplier.
	var adjL1Fee *big.Int
	if l1Fee != nil {
		adjL1Fee = new(big.Int).Mul(l1Fee, big.NewInt(L1FeeEstimateMultNum))
		adjL1Fee.Div(adjL1Fee, big.NewInt(L1FeeEstimateMultDen))
	}

	// relayerTip = (totalFee - adjL1Fee) / gas - adjBaseFee - maxTipCap
	remaining := new(big.Int).Set(totalFee)
	if adjL1Fee != nil {
		remaining.Sub(remaining, adjL1Fee)
	}
	remaining.Div(remaining, new(big.Int).SetUint64(gas))
	remaining.Sub(remaining, adjBaseFee)
	remaining.Sub(remaining, maxTipCap)
	return remaining
}
