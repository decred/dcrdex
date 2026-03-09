// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package eth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/evmrelay"
	"github.com/ethereum/go-ethereum/common"
)

// relayStatus represents the status of a relay task.
type relayStatus struct {
	// State is the relay task state: "pending", "success", or "failed".
	State evmrelay.RelayState
	// FailureReason provides additional detail for failed tasks.
	FailureReason string
	// TxHash is the mined on-chain transaction hash for successful tasks.
	// It is empty while pending or after a failed relay attempt.
	TxHash common.Hash
}

// feeEstimate is the response from the relay fee estimation endpoint.
type feeEstimate struct {
	// RelayAddr is the relay's address, used as feeRecipient in the
	// EIP-712 signature.
	RelayAddr common.Address
	// Fee is the total relay fee in wei.
	Fee *big.Int
}

// relayer is an interface to interact with a relay service for gasless
// redemptions.
type relayer interface {
	// estimateFee returns the relay's address and the estimated fee for
	// the given number of redemptions targeting the specified contract.
	estimateFee(ctx context.Context, numRedemptions int, target common.Address) (*feeEstimate, error)
	// checkHealth checks if the relay supports the configured chain.
	checkHealth(ctx context.Context) error
	// submitSignedRedeem submits an EIP-712 signed redemption to the relay.
	// Returns a task ID that can be used to poll for status.
	submitSignedRedeem(ctx context.Context, req *relayRequest) (taskID string, err error)
	// getRelayStatus returns the current status of a relay task.
	getRelayStatus(ctx context.Context, taskID string) (*relayStatus, error)
}

// relayRequest contains the data needed to submit a signed redemption
// to a relay service.
type relayRequest struct {
	Target   common.Address // ETHSwap contract address
	Calldata []byte         // ABI-encoded calldata for the target
}

// simnetRelayChainIDs maps BIP asset IDs to mainnet chain IDs for use
// as relay routing keys on simnet (where all chains share chain ID 1337).
var simnetRelayChainIDs = map[uint32]int64{
	60:   1,    // ETH
	966:  137,  // Polygon
	8453: 8453, // Base (BIP ID happens to equal mainnet chain ID)
}

// httpRelayer implements the relayer interface using a custom relay HTTP API.
type httpRelayer struct {
	httpClient   *http.Client
	baseURL      string
	relayChainID int64 // chain ID to use in relay API requests
}

var _ relayer = (*httpRelayer)(nil)

// newHTTPRelayer creates a new relay client with the given base URL.
func newHTTPRelayer(baseURL string, net dex.Network, baseChainID uint32, chainID int64) *httpRelayer {
	relayChainID := chainID
	if net == dex.Simnet {
		if id, ok := simnetRelayChainIDs[baseChainID]; ok {
			relayChainID = id
		}
	}
	return &httpRelayer{
		httpClient:   &http.Client{Timeout: 30 * time.Second},
		baseURL:      baseURL,
		relayChainID: relayChainID,
	}
}

func (r *httpRelayer) doJSONRequest(ctx context.Context, method, path string, reqBody any) ([]byte, error) {
	var bodyReader io.Reader
	if reqBody != nil {
		jsonBody, err := json.Marshal(reqBody)
		if err != nil {
			return nil, fmt.Errorf("error marshaling %s request: %w", path, err)
		}
		bodyReader = bytes.NewReader(jsonBody)
	}

	httpReq, err := http.NewRequestWithContext(ctx, method, r.baseURL+path, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("error creating %s HTTP request: %w", path, err)
	}
	if reqBody != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}

	resp, err := r.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("%s HTTP request failed: %w", path, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MiB limit
	if err != nil {
		return nil, fmt.Errorf("error reading %s response: %w", path, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s returned status %d: %s", path, resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

// estimateFee calls the relay's fee estimation endpoint.
func (r *httpRelayer) estimateFee(ctx context.Context, numRedemptions int, target common.Address) (*feeEstimate, error) {
	body := &evmrelay.EstimateFeeRequest{
		ChainID:        r.relayChainID,
		Target:         target.Hex(),
		NumRedemptions: numRedemptions,
	}

	respBody, err := r.doJSONRequest(ctx, http.MethodPost, "/api/estimatefee", body)
	if err != nil {
		return nil, err
	}

	var result evmrelay.EstimateFeeResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("error parsing estimatefee response: %w", err)
	}

	fee, ok := new(big.Int).SetString(result.Fee, 10)
	if !ok {
		return nil, fmt.Errorf("invalid fee value: %s", result.Fee)
	}

	relayAddr := common.HexToAddress(result.RelayAddr)
	if relayAddr == (common.Address{}) {
		return nil, fmt.Errorf("relay returned zero fee-recipient address")
	}

	return &feeEstimate{
		RelayAddr: relayAddr,
		Fee:       fee,
	}, nil
}

// checkHealth calls the relay's health check endpoint for the configured chain.
func (r *httpRelayer) checkHealth(ctx context.Context) error {
	path := fmt.Sprintf("/api/health?chain=%d", r.relayChainID)
	respBody, err := r.doJSONRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return err
	}

	var result evmrelay.HealthResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return fmt.Errorf("error parsing health response: %w", err)
	}
	if result.Error != "" {
		return fmt.Errorf("relay health check failed: %s", result.Error)
	}
	return nil
}

// submitSignedRedeem submits the signed redeem calldata to the relay.
func (r *httpRelayer) submitSignedRedeem(ctx context.Context, req *relayRequest) (string, error) {
	body := &evmrelay.RelayRequest{
		ChainID:  r.relayChainID,
		Target:   req.Target.Hex(),
		Calldata: "0x" + common.Bytes2Hex(req.Calldata),
	}

	respBody, err := r.doJSONRequest(ctx, http.MethodPost, "/api/relay", body)
	if err != nil {
		return "", err
	}

	var result evmrelay.SubmitResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("error parsing relay response: %w", err)
	}

	if result.TaskID == "" {
		return "", fmt.Errorf("relay returned empty task ID")
	}

	return result.TaskID, nil
}

// getRelayStatus returns the current status of a relay task.
func (r *httpRelayer) getRelayStatus(ctx context.Context, taskID string) (*relayStatus, error) {
	respBody, err := r.doJSONRequest(ctx, http.MethodGet, "/api/relay/"+url.PathEscape(taskID), nil)
	if err != nil {
		return nil, err
	}

	var result evmrelay.StatusResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("error parsing status response: %w", err)
	}

	status := &relayStatus{
		State:         result.State,
		FailureReason: result.FailureReason,
	}
	if result.TxHash != "" {
		status.TxHash = common.HexToHash(result.TxHash)
	}

	return status, nil
}
