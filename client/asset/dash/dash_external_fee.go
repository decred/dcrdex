// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dash

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sync"
	"time"

	"decred.org/dcrdex/dex"
)

// See Also: https://blockchair.com/api/docs

const (
	minCacheTime       = 3 * time.Second
	mainnetFeeStatsAPI = "https://api.blockchair.com/dash/stats"
)

var (
	statsCache = blockchairStatsCache{
		sync.Mutex{},
		time.Now(),
		blockchairStatsData{uint64(1)}}
)

type blockchairStatsData struct {
	SuggestedTxFee uint64 `json:"suggested_transaction_fee_per_byte_sat"`
}

// blockchairStatsCache caches statistics from a mainnet statistics endpoint
type blockchairStatsCache struct {
	sync.Mutex
	// Time of the last successful read from the statistics endpoint
	lastGoodRead time.Time
	// Statistics from the endpoint
	data blockchairStatsData
}

func (cache *blockchairStatsCache) update(readTime time.Time, stats blockchairStatsData) {
	cache.Lock()
	cache.lastGoodRead = readTime
	cache.data = stats
	cache.Unlock()
}

func (cache *blockchairStatsCache) value() *blockchairStatsData {
	cache.Lock()
	defer cache.Unlock()
	return &cache.data
}

func (cache *blockchairStatsCache) timestamp() *time.Time {
	cache.Lock()
	defer cache.Unlock()
	return &cache.lastGoodRead
}

func (cache *blockchairStatsCache) useCache() bool {
	lastGoodReadTime := cache.timestamp()
	return lastGoodReadTime.Add(minCacheTime).After(time.Now())
}

type blockchairStatsResponse struct {
	Data blockchairStatsData `json:"data"`
}

// fetchExternalFee calls mainnetFeeStatsAPI endpoint and returns Dash
// blockchain stats
func fetchExternalFee(ctx context.Context, net dex.Network) (uint64, error) {
	if net != dex.Mainnet {
		return 0, errors.New("mainnet endpoint only")
	}
	if statsCache.useCache() {
		return statsCache.value().SuggestedTxFee, nil
	}

	// timed http call
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, mainnetFeeStatsAPI, nil)
	if err != nil {
		return 0, err
	}
	httpResponse, err := http.DefaultClient.Do(r)
	if err != nil {
		return 0, err
	}
	var resp blockchairStatsResponse
	reader := io.LimitReader(httpResponse.Body, 1<<19)
	err = json.NewDecoder(reader).Decode(&resp)
	if err != nil {
		return 0, err
	}
	httpResponse.Body.Close()

	statsCache.update(time.Now(), resp.Data)

	return resp.Data.SuggestedTxFee, nil
}
