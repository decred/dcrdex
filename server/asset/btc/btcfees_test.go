//go:build btcfees

package btc

import (
	"context"
	"testing"
	"time"

	"decred.org/dcrdex/dex"
)

func TestExternalBTCFeeRates(t *testing.T) {
	log := dex.StdOutLogger("T", dex.LevelTrace)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	feeRate := getMempoolDotSpaceFeeRate(ctx, log)
	if feeRate == 0 {
		t.Fatalf("mempool.space fee rate failed")
	}

	feeRate = getBtcDotComFeeRate(ctx, log)
	if feeRate == 0 {
		t.Fatalf("btc.com fee rate failed")
	}

	time.Sleep(time.Second)

	feeRate = (&Backend{log: log}).fetchExternalBitcoinFeeRate(ctx)
	if feeRate == 0 {
		t.Fatalf("fetchExternalBitcoinFeeRate failed")
	}
}
