package txfee

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"decred.org/dcrdex/client/asset/bch"
	"decred.org/dcrdex/client/asset/btc"
	"decred.org/dcrdex/client/asset/dcr"
	"decred.org/dcrdex/client/asset/doge"
	"decred.org/dcrdex/client/asset/eth"
	"decred.org/dcrdex/client/asset/ltc"
	"decred.org/dcrdex/client/asset/zec"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/server/asset/dash"
	"github.com/decred/dcrd/dcrutil/v4"
)

const (
	defaultFeeRefreshInterval = 5 * time.Minute
	FeeEstimateExpiry         = 30 * time.Minute
	reactivationDuration      = 24 * time.Hour

	// blockDaemon provides mainnet and testnet fee estimates for 9 chain, 5 of
	// which dex currently has support for at the time of writing. Requires an
	// API key which gives us 3 Million CU, 50CU per chain tx fee request. That
	// means the 3 million CU would be exhausted at ~6 months if we request data
	// every 5 minutes for max 5 chains.
	blockDaemon           = "Block Deamon"
	blockDaemonAPIURL     = "https://svc.blockdaemon.com/universal/v1/%s/%s/tx/estimate_fee" // bitcoin/testnet
	defaultBlockDaemonKey = "2go1YqUcuAr4WZ2-3WgSD3c7qpatZqQuNWhTVBldKZnTSUtw"               // maybe we should just change this every month?

	// dcrdata provided fee estimate for dcr and supports mainnet and testnet.
	// Rate limits are unknown but we should be under limit if we request every
	// 5 minutes.
	dcrdata                 = "dcrdata"
	defaultDcrdataFeeBlocks = "2"
	dcrdataMainnetAPIURL    = "https://dcrdata.decred.org/insight/api/utils/estimatefee?nbBlocks=2" // nbBlocks=2 == 10 minutes
	dcrdataTestnetAPIURL    = "https://testnet.decred.org/insight/api/utils/estimatefee?nbBlocks=2" // nbBlocks=2 == 10 minutes

	// tatum supports 5 chains on testnet and mainnet at the time of writing. We
	// get 1 million credits per month and max 3 requests per second. We should
	// be under limit if we request for fee estimates every 5 minutes.
	tatum = "Tatum"
	// Note: tatumAPIURL works for mainnet by default without an API key.
	tatumAPIURL = "https://api.tatum.io/v3/blockchain/fee/%s"

	// blockchair provides fee estimate for upto 20 chains out of which 7 are
	// supported by dex at the time of writing. Rate limit is not clear but we
	// should be out of trouble if we request every 5 minutes.
	blockchair       = "Blockchair"
	blockchairAPIURL = "https://api.blockchair.com/stats"

	// blockCypher provides fee estimate for upto five chains. Rate limit is 3
	// per second and 100 per hour.
	blockCypher    = "BlockCypher"
	blockCypherURL = "https://api.blockcypher.com/v1/%s/main"
)

var (
	// blockDaemonChainsSupported is a map of bipIDs to string value used in
	// fetching data from the blockDaemon API. These are chains we can request
	// fee estimate for at the time of writing. Fortunately, we can request all
	// 4 simultaneously without issues as requests are capped at 5 per second.
	blockDaemonChainsSupported = map[uint32]string{
		btc.BipID: "bitcoin",
		bch.BipID: "bitcoincash",
		eth.BipID: "ethereum",
		ltc.BipID: "litecoin",
	}

	// tatumChainsSupported is a map of bipIDs to string value used in fetching
	// fee estimate from the tatum API. These are chains we can request fee
	// estimate for at the time of writing. Unfortunately, we can only make a
	// maximum of 3 request per seconds.
	tatumChainsSupported = map[uint32]string{
		btc.BipID:  "BTC",
		eth.BipID:  "ETH",
		doge.BipID: "DOGE",
		ltc.BipID:  "LTC",
	}

	// blockchairChainsSupported is a map of bipIDs to string value used in
	// identifying fee estimate data retrieved from the blockchair API. We
	// removed support for ethereum.
	blockchairChainsSupported = map[uint32]string{
		btc.BipID:  "bitcoin",
		zec.BipID:  "zcash",
		eth.BipID:  "ethereum",
		bch.BipID:  "bitcoin-cash",
		ltc.BipID:  "litecoin",
		doge.BipID: "dogecoin",
		dash.BipID: "dash",
	}

	// blockCypherChainsSupported is a map of bipIDs to string value used in
	// retrieving fee estimate data retrieved from the blockCypher API. See:
	// https://www.blockcypher.com/dev/bitcoin/#restful-resources. Data for eth
	// is returned from the blockCypher API but is yet to be included in their
	// doc.
	blockCypherChainsSupported = map[uint32]string{
		btc.BipID:  "btc",
		eth.BipID:  "eth",
		ltc.BipID:  "litecoin",
		doge.BipID: "doge",
		dash.BipID: "dash",
	}

	// Expected API errors
	errorFailedAuth = errors.New("authorization error")
	errorRateLimit  = errors.New("rate limit exceeded")
)

type feeEstimateSource struct {
	name            string
	mtx             sync.RWMutex
	feeEstimates    map[uint32]uint64
	disabled        bool
	canReactivate   bool
	getFeeEstimate  func(ctx context.Context, log dex.Logger, chainsIDs []uint32) (map[uint32]uint64, error)
	refreshInterval time.Duration
	lastRefresh     time.Time
}

func (fes *feeEstimateSource) isDisabled() bool {
	fes.mtx.RLock()
	defer fes.mtx.RUnlock()
	return fes.disabled
}

func (fes *feeEstimateSource) hasFeeEstimates() bool {
	fes.mtx.RLock()
	defer fes.mtx.RUnlock()
	return len(fes.feeEstimates) > 0
}

func (fes *feeEstimateSource) isExpired() bool {
	fes.mtx.RLock()
	defer fes.mtx.RUnlock()
	return time.Since(fes.lastRefresh) > FeeEstimateExpiry
}

func (fes *feeEstimateSource) deactivate(canReactivate bool) {
	fes.mtx.Lock()
	defer fes.mtx.Unlock()
	fes.canReactivate = canReactivate
}

func (fes *feeEstimateSource) checkIfSourceCanReactivate() bool {
	fes.mtx.RLock()
	defer fes.mtx.RUnlock()
	if !fes.canReactivate || time.Since(fes.lastRefresh) < reactivationDuration {
		return false
	}

	fes.disabled = false
	fes.lastRefresh = time.Now()
	return true
}

func feeEstimateSources(net dex.Network, cfg Config) []*feeEstimateSource {
	if cfg.BlockDeamonAPIKey == "" {
		cfg.BlockDeamonAPIKey = defaultBlockDaemonKey
	}

	feeSources := []*feeEstimateSource{
		{
			name:            blockDaemon,
			feeEstimates:    make(map[uint32]uint64),
			disabled:        cfg.BlockDeamonAPIKey == "",
			refreshInterval: defaultFeeRefreshInterval,
			getFeeEstimate: func(ctx context.Context, log dex.Logger, chainsIDs []uint32) (map[uint32]uint64, error) {
				authHeader := map[string]string{
					"X-API-Key": cfg.BlockDeamonAPIKey,
				}

				feeEstimates := make(map[uint32]uint64, len(chainsIDs))
				for _, chainID := range chainsIDs {
					chain, found := blockDaemonChainsSupported[chainID]
					if !found {
						continue
					}

					var feeEstimate uint64
					var err error
					if chainID == eth.BipID {
						feeEstimate, err = fetchBlockDeamonEthFeeEstimate(ctx, net, authHeader)
					} else {
						var resp struct {
							EstimatedFees struct {
								Fast uint64 `json:"fast"` // "estimated_fees": {"fast": 18,"medium": 16,"slow": 14}
							} `json:"estimated_fees"`
						}

						if err = getFeeEstimateWithHeader(ctx, fmt.Sprintf(blockDaemonAPIURL, chain, net.String()), &resp, authHeader); err == nil {
							feeEstimate = resp.EstimatedFees.Fast
						}
					}
					if err != nil {
						if isAuthError(err) || isRateLimitError(err) {
							return nil, err
						}

						log.Errorf("%s.getFeeEstimate error: %v", blockDaemon, err)
						continue
					}

					feeEstimates[chainID] = feeEstimate
				}

				return feeEstimates, nil
			},
		},
		{
			name:         dcrdata,
			feeEstimates: make(map[uint32]uint64),
			getFeeEstimate: func(ctx context.Context, log dex.Logger, chainsIDs []uint32) (map[uint32]uint64, error) {
				apiURL := dcrdataMainnetAPIURL
				if net == dex.Testnet {
					apiURL = dcrdataTestnetAPIURL
				}

				feeEstimates := make(map[uint32]uint64, 1)
				for _, chainID := range chainsIDs {
					if chainID != dcr.BipID {
						continue
					}

					resp := make(map[string]float64, 1) // {"2": 0.0001}
					err := getFeeEstimate(ctx, apiURL, &resp)
					if err != nil {
						return nil, err
					}

					feeEstimates[chainID] = uint64(resp[defaultDcrdataFeeBlocks] * dcrutil.AtomsPerCoin)
					break
				}

				return feeEstimates, nil
			},
			refreshInterval: defaultFeeRefreshInterval,
		},
		{
			name:         tatum,
			feeEstimates: make(map[uint32]uint64),
			disabled:     net == dex.Testnet && cfg.TatumTestnetAPIKey == "",
			getFeeEstimate: func(ctx context.Context, log dex.Logger, chainsIDs []uint32) (map[uint32]uint64, error) {
				apiKey := cfg.TatumMainnetAPIKey
				if net == dex.Testnet {
					apiKey = cfg.TatumTestnetAPIKey
				}

				authHeader := make(map[string]string)
				if apiKey != "" {
					authHeader["x-api-key"] = apiKey
				}

				feeEstimates := make(map[uint32]uint64, len(chainsIDs))
				var nRequests int
				for _, chainID := range chainsIDs {
					chain, found := tatumChainsSupported[chainID]
					if !found {
						continue
					}

					// btc example response:
					// {"fast":22.594,"medium":17.285,"slow":11.07,"block":843097,"time":"2024-05-12T03:11:44.804Z"}
					var resp struct {
						Fast float64 `json:"fast"`
					}

					if nRequests%3 == 0 {
						// Delay for one second so we don't hit our 3 request
						// per sec rate limit.
						time.Sleep(1 * time.Second)
					}

					err := getFeeEstimateWithHeader(ctx, fmt.Sprintf(tatumAPIURL, chain), &resp, authHeader)
					if err != nil {
						if isAuthError(err) || isRateLimitError(err) {
							return nil, err
						}

						log.Errorf("%s.getFeeEstimate error: %v", tatum, err)
						continue
					}

					nRequests++
					feeEstimates[chainID] = uint64(resp.Fast)
				}

				return feeEstimates, nil
			},
			refreshInterval: defaultFeeRefreshInterval,
		},
		{
			name:         blockchair,
			disabled:     net == dex.Testnet,
			feeEstimates: make(map[uint32]uint64),
			getFeeEstimate: func(ctx context.Context, log dex.Logger, chainsIDs []uint32) (map[uint32]uint64, error) {
				var resp struct {
					Data map[string]struct {
						Data json.RawMessage `json:"data"`
					} `json:"data"`
				}
				err := getFeeEstimate(ctx, blockchairAPIURL, &resp)
				if err != nil {
					return nil, err
				}

				feeEstimates := make(map[uint32]uint64, len(chainsIDs))
				for _, chainID := range chainsIDs {
					chain, ok := blockchairChainsSupported[chainID]
					if !ok {
						continue
					}

					var feeInfo blockchairFeeInfo
					feeInfoBytes := resp.Data[chain].Data
					if err := json.Unmarshal(feeInfoBytes, &feeInfo); err != nil {
						log.Errorf("%s: error unmarshalling (%s): %v", blockchair, string(feeInfoBytes), err)
						continue
					}

					feeEstimate := feeInfo.SuggestedTxFeePerBytePerSat
					if feeInfo.SuggestedFeeGweiOptions.Fast > 0 {
						feeEstimate = feeInfo.SuggestedFeeGweiOptions.Fast * eth.WalletInfo.UnitInfo.Conventional.ConversionFactor
					}
					feeEstimates[chainID] = feeEstimate
				}

				return feeEstimates, nil
			},
			refreshInterval: defaultFeeRefreshInterval,
		},
		{
			name:         blockCypher,
			disabled:     net == dex.Testnet,
			feeEstimates: make(map[uint32]uint64),
			getFeeEstimate: func(ctx context.Context, log dex.Logger, chainsIDs []uint32) (map[uint32]uint64, error) {

				feeEstimates := make(map[uint32]uint64, len(chainsIDs))
				var nRequests int
				for _, chainID := range chainsIDs {
					chain, found := blockCypherChainsSupported[chainID]
					if !found {
						continue
					}

					var resp struct {
						// There's medium_fee_per_kb, low_fee_per_kb but prefer high_fee_per_kb
						HighFeePerKb uint64 `json:"high_fee_per_kb"`
						// There's medium_gas_price, low_gas_price but prefer high_gas_price
						HighGasPrice uint64 `json:"high_gas_price"`
					}

					if nRequests%3 == 0 {
						// Delay for one second so we don't hit our 3 request
						// per sec rate limit.
						time.Sleep(1 * time.Second)
					}

					err := getFeeEstimate(ctx, fmt.Sprintf(blockCypherURL, chain), &resp)
					if err != nil {
						if isRateLimitError(err) {
							return nil, err
						}

						log.Errorf("%s.getFeeEstimate error: %v", blockCypher, err)
						continue
					}

					nRequests++
					feeEstimate := resp.HighFeePerKb / 1000
					if resp.HighGasPrice > 0 {
						feeEstimate = resp.HighGasPrice
					}
					feeEstimates[chainID] = feeEstimate
				}

				return feeEstimates, nil
			},
			refreshInterval: defaultFeeRefreshInterval,
		},
	}

	for i := range feeSources {
		feeSources[i].canReactivate = !feeSources[i].disabled
	}

	return feeSources
}

// fetchBlockDeamonEthFeeEstimate retrieves fee estimate for only ethereum as it
// has a different response.
func fetchBlockDeamonEthFeeEstimate(ctx context.Context, net dex.Network, authHeader map[string]string) (uint64, error) {
	var resp struct {
		EstimatedFees struct {
			//	"estimated_fees": {"fast": {"max_priority_fee": 1500000000,"max_total_fee": 5529003649},"medium": {"max_priority_fee": 1200000000 	"max_total_fee": 5099269857},"slow": {"max_priority_fee": 1000000000,"max_total_fee": 4704811587}
			Fast struct {
				MaxTotalFee uint64 `json:"max_total_fee"`
			} `json:"fast"`
		} `json:"estimated_fees"`
	}

	err := getFeeEstimateWithHeader(ctx, fmt.Sprintf(blockDaemonAPIURL, "ethereum", net.String()), &resp, authHeader)
	if err != nil {
		return 0, err
	}

	return resp.EstimatedFees.Fast.MaxTotalFee, nil
}

func isAuthError(err error) bool {
	return errors.Is(err, errorFailedAuth)
}

func isRateLimitError(err error) bool {
	return errors.Is(err, errorRateLimit)
}

func getFeeEstimate(ctx context.Context, url string, thing any) error {
	return getFeeEstimateWithHeader(ctx, url, thing, nil)
}

func getFeeEstimateWithHeader(ctx context.Context, url string, thing any, header map[string]string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	for key, value := range header {
		req.Header.Add(key, value)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			return errorFailedAuth
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			return errorRateLimit
		}
		return fmt.Errorf("error %d fetching %q", resp.StatusCode, url)
	}

	reader := io.LimitReader(resp.Body, 1<<22)
	return json.NewDecoder(reader).Decode(thing)
}
