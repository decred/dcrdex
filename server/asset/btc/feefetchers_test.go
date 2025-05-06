//go:build feefetcher

package btc

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"decred.org/dcrdex/dex/feeratefetcher"
)

func testSource(src *feeratefetcher.SourceConfig) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()
	feeRate, _, err := src.F(ctx)
	if err != nil {
		fmt.Printf("XXXXX Error fetching fee rate for %s: %v \n", src.Name, err)
		return
	}
	fmt.Printf("##### Fee rate fetched for %s: %d \n", src.Name, feeRate)
}

func TestFreeFeeFetchers(t *testing.T) {
	for _, src := range freeFeeSources {
		testSource(src)
	}
}

func TestTatumFeeFetcher(t *testing.T) {
	testSource(tatumFeeRateFetcher(os.Getenv("KEY")))
}

func TestBlockDaemonFeeFetcher(t *testing.T) {
	testSource(blockDaemonFeeRateFetcher(os.Getenv("KEY")))
}
