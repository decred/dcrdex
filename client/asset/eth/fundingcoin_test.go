// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build lgpl

package eth

import (
	"bytes"
	"testing"

	"decred.org/dcrdex/dex/encode"
)

func TestFundingCoinID(t *testing.T) {
	// Decode and encode fundingCoinID
	var address [20]byte
	copy(address[:], encode.RandomBytes(20))
	originalFundingCoin := fundingCoinID{
		Address: address,
		Amount:  100,
	}
	encodedFundingCoin := originalFundingCoin.Encode()
	decodedFundingCoin, err := decodeFundingCoinID(encodedFundingCoin)
	if err != nil {
		t.Fatalf("unexpected error decoding swap coin: %v", err)
	}
	if !bytes.Equal(originalFundingCoin.Address[:], decodedFundingCoin.Address[:]) {
		t.Fatalf("expected address to be equal before and after decoding")
	}
	if originalFundingCoin.Amount != decodedFundingCoin.Amount {
		t.Fatalf("expected amount to be equal before and after decoding")
	}

	// Decode amount coin id with incorrect length
	fundingCoinID := make([]byte, 35)
	copy(fundingCoinID, encode.RandomBytes(35))
	if _, err := decodeFundingCoinID(fundingCoinID); err == nil {
		t.Fatalf("expected error decoding amount coin ID with incorrect length")
	}
}
