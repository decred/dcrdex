// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package evmrelay

import (
	"bytes"
	"math/big"
	"testing"
	"time"

	dexeth "decred.org/dcrdex/dex/networks/eth"
	swapv1 "decred.org/dcrdex/dex/networks/eth/contracts/v1"
	"github.com/ethereum/go-ethereum/common"
)

func packedSignedRedeemCalldataForTest(t *testing.T, numRedemptions int) []byte {
	t.Helper()

	redemptions := make([]swapv1.ETHSwapRedemption, numRedemptions)
	for i := range redemptions {
		redemptions[i].V.SecretHash = [32]byte{byte(i + 1)}
		redemptions[i].V.Value = new(big.Int).SetUint64(1e18)
		redemptions[i].V.Initiator = common.HexToAddress("0x1111111111111111111111111111111111111111")
		redemptions[i].V.RefundTimestamp = uint64(time.Now().Unix())
		redemptions[i].V.Participant = common.HexToAddress("0x2222222222222222222222222222222222222222")
		redemptions[i].Secret = [32]byte{byte(i + 2)}
	}

	calldata, err := dexeth.ABIs[1].Pack(dexeth.RedeemWithSignatureMethodName,
		redemptions,
		common.HexToAddress("0x3333333333333333333333333333333333333333"),
		new(big.Int),
		new(big.Int),
		new(big.Int),
		make([]byte, 65),
	)
	if err != nil {
		t.Fatalf("Pack error: %v", err)
	}
	return calldata
}

func TestMockSignedRedeemCalldataRejectsBadCounts(t *testing.T) {
	tests := []int{0, MaxSignedRedeemBatch + 1}
	for _, numRedemptions := range tests {
		if _, err := MockSignedRedeemCalldata(numRedemptions); err == nil {
			t.Fatalf("expected error for numRedemptions=%d", numRedemptions)
		}
	}
}

func TestMockSignedRedeemCalldataMatchesABILengthAndSelector(t *testing.T) {
	for _, numRedemptions := range []int{1, 2, MaxSignedRedeemBatch} {
		got, err := MockSignedRedeemCalldata(numRedemptions)
		if err != nil {
			t.Fatalf("MockSignedRedeemCalldata(%d) error: %v", numRedemptions, err)
		}
		want := packedSignedRedeemCalldataForTest(t, numRedemptions)

		if len(got) != len(want) {
			t.Fatalf("len(MockSignedRedeemCalldata(%d)) = %d, want %d", numRedemptions, len(got), len(want))
		}
		if !bytes.Equal(got[:4], want[:4]) {
			t.Fatalf("selector mismatch for n=%d: got %x, want %x", numRedemptions, got[:4], want[:4])
		}
	}
}
