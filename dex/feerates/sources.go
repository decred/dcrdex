package feerates

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
	FeeRateEstimateExpiry     = 30 * time.Minute
	reactivationDuration      = 24 * time.Hour

	// blockDaemon provides mainnet and testnet fee rate estimates for 9 chains,
	// 5 of which dex currently has support for at the time of writing (See:
	// blockDaemonChainsSupported below). Requires an API key which gives us 3
	// Million CU per month, 50CU per chain fee rate request. That means the 3
	// million CU would be exhausted at ~6 months if we request data every 5
	// minutes for max 5 chains.
	blockDaemon       = "Block Deamon"
	blockDaemonAPIURL = "https://svc.blockdaemon.com/universal/v1/%s/%s/tx/estimate_fee" // bitcoin/testnet

	// dcrdata provided fee rate estimate for dcr and supports mainnet and testnet.
	// Rate limits are unknown but we should be under limit if we request every
	// 5 minutes.
	dcrdata                 = "dcrdata"
	defaultDcrdataFeeBlocks = "2"
	dcrdataMainnetAPIURL    = "https://dcrdata.decred.org/insight/api/utils/estimatefee?nbBlocks=2"
	dcrdataTestnetAPIURL    = "https://testnet.decred.org/insight/api/utils/estimatefee?nbBlocks=2"

	// tatum supports 4 chains (See: tatumChainsSupported below) on testnet and
	// mainnet at the time of writing. We get 1 million credits per month and
	// max 3 requests per second. We should be under limit if we request for fee
	// estimates every 5 minutes.
	tatum = "Tatum"
	// Note: tatumAPIURL works for mainnet by default without an API key.
	tatumAPIURL = "https://api.tatum.io/v3/blockchain/fee/%s"

	// blockchair provides fee rate estimate for 20 chains (Bitcoin, Bitcoin Cash,
	// Ethereum, Litecoin, Bitcoin-sv, Dogecoin, Dash, Ripple, Groestlcoin,
	// Stellar, Monero, Cardano, Zcash, Mixin, EOS, Ecash (xec), Polkadot,
	// Solana, Kusama, Cross Chains - Tether, USD Coin, Binance US) out of which
	// 7 are supported by dex at the time of writing (See:
	// blockchairChainsSupported below). Rate limit is 1440 requests a day but
	// we should be out of trouble if we request every 5 minutes.
	blockchair       = "Blockchair"
	blockchairAPIURL = "https://api.blockchair.com/stats"

	// blockCypher provides fee rate estimate for 5 chains (See:
	// blockCypherChainsSupported below). Rate limit is 3 per second and 100 per
	// hour.
	blockCypher    = "BlockCypher"
	blockCypherURL = "https://api.blockcypher.com/v1/%s/main"
)

var (
	// blockDaemonChainsSupported is a map of bipIDs to string value used in
	// fetching data from the blockDaemon API. These are chains we can request
	// fee rate estimate for at the time of writing. Fortunately, we can request all
	// 4 simultaneously without issues as requests are capped at 5 per second.
	blockDaemonChainsSupported = map[uint32]string{
		btc.BipID: "bitcoin",
		bch.BipID: "bitcoincash",
		eth.BipID: "ethereum",
		ltc.BipID: "litecoin",
	}

	// tatumChainsSupported is a map of bipIDs to string value used in fetching
	// fee rate estimate from the tatum API. These are chains we can request fee
	// rate estimate for at the time of writing. Unfortunately, we can only make
	// a maximum of 3 request per seconds.
	tatumChainsSupported = map[uint32]string{
		btc.BipID:  "BTC",
		eth.BipID:  "ETH",
		doge.BipID: "DOGE",
		ltc.BipID:  "LTC",
	}

	// blockchairChainsSupported is a map of bipIDs to string value used in
	// identifying fee rate estimate data retrieved from the blockchair API.
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
	// retrieving fee rate estimate data retrieved from the blockCypher API. See:
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

type feeRateEstimateSource struct {
	name               string
	mtx                sync.RWMutex
	feeRateEstimates   map[uint32]uint64
	disabled           bool
	canReactivate      bool
	getFeeRateEstimate func(ctx context.Context, log dex.Logger, chainsIDs []uint32) (map[uint32]uint64, error)
	lastRefresh        time.Time
}

func (fes *feeRateEstimateSource) isDisabled() bool {
	fes.mtx.RLock()
	defer fes.mtx.RUnlock()
	return fes.disabled
}

func (fes *feeRateEstimateSource) hasFeeRateEstimates() bool {
	fes.mtx.RLock()
	defer fes.mtx.RUnlock()
	return len(fes.feeRateEstimates) > 0
}

func (fes *feeRateEstimateSource) isExpired() bool {
	fes.mtx.RLock()
	defer fes.mtx.RUnlock()
	return time.Since(fes.lastRefresh) > FeeRateEstimateExpiry
}

func (fes *feeRateEstimateSource) deactivate(canReactivate bool) {
	fes.mtx.Lock()
	defer fes.mtx.Unlock()
	fes.canReactivate = canReactivate
}

func (fes *feeRateEstimateSource) checkIfSourceCanReactivate() bool {
	fes.mtx.RLock()
	defer fes.mtx.RUnlock()
	if !fes.canReactivate || time.Since(fes.lastRefresh) < reactivationDuration {
		return false
	}

	fes.disabled = false
	fes.lastRefresh = time.Now()
	return true
}

func feeRateEstimateSources(net dex.Network, cfg Config) []*feeRateEstimateSource {
	feeSources := []*feeRateEstimateSource{
		{
			name:             blockDaemon,
			feeRateEstimates: make(map[uint32]uint64),
			disabled:         cfg.BlockDeamonAPIKey == "",
			getFeeRateEstimate: func(ctx context.Context, log dex.Logger, chainsIDs []uint32) (map[uint32]uint64, error) {
				authHeader := map[string]string{
					"X-API-Key": cfg.BlockDeamonAPIKey,
				}

				feeRateEstimates := make(map[uint32]uint64, len(chainsIDs))
				for _, chainID := range chainsIDs {
					chain, found := blockDaemonChainsSupported[chainID]
					if !found {
						continue
					}

					var feeRateEstimate uint64
					var err error
					if chainID == eth.BipID {
						feeRateEstimate, err = fetchBlockDaemonEthFeeRateEstimate(ctx, net, authHeader)
					} else {
						var resp struct {
							EstimatedFees struct {
								Fast uint64 `json:"fast"` // "estimated_fees": {"fast": 18,"medium": 16,"slow": 14}
							} `json:"estimated_fees"`
						}

						if err = getFeeRateEstimateWithHeader(ctx, fmt.Sprintf(blockDaemonAPIURL, chain, net.String()), &resp, authHeader); err == nil {
							feeRateEstimate = resp.EstimatedFees.Fast
						}
					}
					if err != nil {
						if isAuthError(err) || isRateLimitError(err) {
							return nil, err
						}

						log.Errorf("%s.getfeeRateEstimate error: %v", blockDaemon, err)
						continue
					}

					feeRateEstimates[chainID] = feeRateEstimate
				}

				return feeRateEstimates, nil
			},
		},
		{
			name:             dcrdata,
			feeRateEstimates: make(map[uint32]uint64),
			getFeeRateEstimate: func(ctx context.Context, log dex.Logger, chainsIDs []uint32) (map[uint32]uint64, error) {
				apiURL := dcrdataMainnetAPIURL
				if net == dex.Testnet {
					apiURL = dcrdataTestnetAPIURL
				}

				feeRateEstimates := make(map[uint32]uint64, 1)
				for _, chainID := range chainsIDs {
					if chainID != dcr.BipID {
						continue
					}

					resp := make(map[string]float64, 1) // {"2": 0.0001}
					err := getFeeRateEstimate(ctx, apiURL, &resp)
					if err != nil {
						return nil, err
					}

					feeRateEstimates[chainID] = uint64(resp[defaultDcrdataFeeBlocks] * dcrutil.AtomsPerCoin)
					break
				}

				return feeRateEstimates, nil
			},
		},
		{
			name:             tatum,
			feeRateEstimates: make(map[uint32]uint64),
			disabled:         net == dex.Testnet && cfg.TatumAPIKey == "",
			getFeeRateEstimate: func(ctx context.Context, log dex.Logger, chainsIDs []uint32) (map[uint32]uint64, error) {
				authHeader := make(map[string]string)
				if cfg.TatumAPIKey != "" {
					authHeader["x-api-key"] = cfg.TatumAPIKey
				}

				feeRateEstimates := make(map[uint32]uint64, len(chainsIDs))
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

					err := getFeeRateEstimateWithHeader(ctx, fmt.Sprintf(tatumAPIURL, chain), &resp, authHeader)
					if err != nil {
						if isAuthError(err) || isRateLimitError(err) {
							return nil, err
						}

						log.Errorf("%s.getfeeRateEstimate error: %v", tatum, err)
						continue
					}

					nRequests++
					feeRateEstimates[chainID] = uint64(resp.Fast)
				}

				return feeRateEstimates, nil
			},
		},
		{
			name:             blockchair,
			disabled:         net == dex.Testnet,
			feeRateEstimates: make(map[uint32]uint64),
			getFeeRateEstimate: func(ctx context.Context, log dex.Logger, chainsIDs []uint32) (map[uint32]uint64, error) {
				var resp struct {
					Data map[string]struct {
						Data json.RawMessage `json:"data"`
					} `json:"data"`
				}
				err := getFeeRateEstimate(ctx, blockchairAPIURL, &resp)
				if err != nil {
					return nil, err
				}

				feeRateEstimates := make(map[uint32]uint64, len(chainsIDs))
				for _, chainID := range chainsIDs {
					chain, ok := blockchairChainsSupported[chainID]
					if !ok {
						continue
					}

					var feeInfo struct {
						SuggestedTransactionFeePerBytePerSat uint64 `json:"suggested_transaction_fee_per_byte_sat"`
						SuggestedFeeGweiOptions              struct {
							Fast uint64 `json:"fast"`
						} `json:"suggested_transaction_fee_gwei_options"`
					}
					feeInfoBytes := resp.Data[chain].Data
					if err := json.Unmarshal(feeInfoBytes, &feeInfo); err != nil {
						log.Errorf("%s: error unmarshalling (%s): %v", blockchair, string(feeInfoBytes), err)
						continue
					}

					feeRateEstimate := feeInfo.SuggestedTransactionFeePerBytePerSat
					if feeInfo.SuggestedFeeGweiOptions.Fast > 0 {
						feeRateEstimate = feeInfo.SuggestedFeeGweiOptions.Fast * eth.WalletInfo.UnitInfo.Conventional.ConversionFactor
					}
					feeRateEstimates[chainID] = feeRateEstimate
				}

				return feeRateEstimates, nil
			},
		},
		{
			name:             blockCypher,
			disabled:         net == dex.Testnet,
			feeRateEstimates: make(map[uint32]uint64),
			getFeeRateEstimate: func(ctx context.Context, log dex.Logger, chainsIDs []uint32) (map[uint32]uint64, error) {

				feeRateEstimates := make(map[uint32]uint64, len(chainsIDs))
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

					err := getFeeRateEstimate(ctx, fmt.Sprintf(blockCypherURL, chain), &resp)
					if err != nil {
						if isRateLimitError(err) {
							return nil, err
						}

						log.Errorf("%s.getfeeRateEstimate error: %v", blockCypher, err)
						continue
					}

					nRequests++
					feeRateEstimate := resp.HighFeePerKb / 1000
					if resp.HighGasPrice > 0 {
						feeRateEstimate = resp.HighGasPrice
					}
					feeRateEstimates[chainID] = feeRateEstimate
				}

				return feeRateEstimates, nil
			},
		},
	}

	for i := range feeSources {
		feeSources[i].canReactivate = !feeSources[i].disabled
	}

	return feeSources
}

// fetchBlockDaemonEthFeeRateEstimate retrieves fee rate estimate for only ethereum as it
// has a different response.
func fetchBlockDaemonEthFeeRateEstimate(ctx context.Context, net dex.Network, authHeader map[string]string) (uint64, error) {
	var resp struct {
		EstimatedFees struct {
			//	"estimated_fees": {"fast": {"max_priority_fee": 1500000000,"max_total_fee": 5529003649},"medium": {"max_priority_fee": 1200000000 	"max_total_fee": 5099269857},"slow": {"max_priority_fee": 1000000000,"max_total_fee": 4704811587}
			Fast struct {
				MaxTotalFee uint64 `json:"max_total_fee"`
			} `json:"fast"`
		} `json:"estimated_fees"`
	}

	err := getFeeRateEstimateWithHeader(ctx, fmt.Sprintf(blockDaemonAPIURL, "ethereum", net.String()), &resp, authHeader)
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

func getFeeRateEstimate(ctx context.Context, url string, thing any) error {
	return getFeeRateEstimateWithHeader(ctx, url, thing, nil)
}

func getFeeRateEstimateWithHeader(ctx context.Context, url string, thing any, header map[string]string) error {
	ctx, cancel := context.WithTimeout(ctx, time.Second*5)
	defer cancel()
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
