package dash

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"decred.org/dcrdex/dex"
)

// See Also: https://blockchair.com/api/docs

type blockchairStatsData struct {
	SuggestedTxFee uint64 `json:"suggested_transaction_fee_per_byte_sat"`
}

// blockchairStatsCache caches statistics from a mainnet statistics endpoint
type blockchairStatsCache struct {
	// Time of the last successful read from the statistics endpoint
	lastGoodRead time.Time
	// Statistics from the endpoint
	data blockchairStatsData
}

func newBlockchairStatsCache(time time.Time, stats blockchairStatsData) blockchairStatsCache {
	return blockchairStatsCache{time, stats}
}

func (b *blockchairStatsCache) update(readTime time.Time, stats blockchairStatsData) {
	b.lastGoodRead = readTime
	b.data = stats
}

func (b *blockchairStatsCache) useCache() bool {
	elapsed := time.Now().UnixMilli() - b.lastGoodRead.UnixMilli()
	return elapsed < int64(minCacheTime)
}

type blockchairStatsResponse struct {
	Data blockchairStatsData `json:"data"`
}

const (
	mainnetFeeStatsAPI = "https://api.blockchair.com/dash/stats"
	minCacheTime       = 3 * time.Second / time.Millisecond // 3s
)

var (
	statsCache = newBlockchairStatsCache(time.Now(), blockchairStatsData{uint64(1)})
)

// fetchExternalFee calls mainnetFeeStatsAPI endpoint and returns stats including
// 'suggested_transaction_fee_per_byte_sat' for mainnet.
func fetchExternalFee(ctx context.Context, net dex.Network) (uint64, error) {
	if net != dex.Mainnet {
		return 0, errors.New("mainnet endpoint only")
	}
	if statsCache.useCache() {
		return statsCache.data.SuggestedTxFee, nil
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
