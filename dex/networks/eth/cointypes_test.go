// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build lgpl
// +build lgpl

package eth

import (
	"bytes"
	"encoding/binary"
	"testing"

	"decred.org/dcrdex/dex/encode"
)

func TestCoinIDs(t *testing.T) {
	// Decode and encode TxCoinID
	var txID [32]byte
	copy(txID[:], encode.RandomBytes(32))
	originalTxCoin := TxCoinID{
		TxID: txID,
	}
	encodedTxCoin := originalTxCoin.Encode()
	decodedCoin, err := DecodeCoinID(encodedTxCoin)
	if err != nil {
		t.Fatalf("unexpected error decoding tx coin: %v", err)
	}
	decodedTxCoin, ok := decodedCoin.(*TxCoinID)
	if !ok {
		t.Fatalf("expected coin to be a TxCoin")
	}
	if !bytes.Equal(originalTxCoin.TxID[:], decodedTxCoin.TxID[:]) {
		t.Fatalf("expected txIds to be equal before and after decoding")
	}

	// Decode tx coin id with incorrect length
	txCoinID := make([]byte, 33)
	binary.BigEndian.PutUint16(txCoinID[:2], uint16(CIDTxID))
	copy(txCoinID[2:], encode.RandomBytes(30))
	if _, err := DecodeCoinID(txCoinID); err == nil {
		t.Fatalf("expected error decoding tx coin ID with incorrect length")
	}

	// Decode and encode SwapCoinID
	var contractAddress [20]byte
	var secretHash [32]byte
	copy(contractAddress[:], encode.RandomBytes(20))
	copy(secretHash[:], encode.RandomBytes(32))
	originalSwapCoin := SwapCoinID{
		ContractAddress: contractAddress,
		SecretHash:      secretHash,
	}
	encodedSwapCoin := originalSwapCoin.Encode()
	decodedCoin, err = DecodeCoinID(encodedSwapCoin)
	if err != nil {
		t.Fatalf("unexpected error decoding swap coin: %v", err)
	}
	decodedSwapCoin, ok := decodedCoin.(*SwapCoinID)
	if !ok {
		t.Fatalf("expected coin to be a SwapCoinID")
	}
	if !bytes.Equal(originalSwapCoin.ContractAddress[:], decodedSwapCoin.ContractAddress[:]) {
		t.Fatalf("expected contract address to be equal before and after decoding")
	}
	if !bytes.Equal(originalSwapCoin.SecretHash[:], decodedSwapCoin.SecretHash[:]) {
		t.Fatalf("expected secret hash to be equal before and after decoding")
	}

	// Decode swap coin id with incorrect length
	swapCoinID := make([]byte, 53)
	binary.BigEndian.PutUint16(swapCoinID[:2], uint16(CIDSwap))
	copy(swapCoinID[2:], encode.RandomBytes(50))
	if _, err := DecodeCoinID(swapCoinID); err == nil {
		t.Fatalf("expected error decoding swap coin ID with incorrect length")
	}

	// Decode and encode AmountCoinID
	var address [20]byte
	var nonce [8]byte
	copy(address[:], encode.RandomBytes(20))
	copy(nonce[:], encode.RandomBytes(8))
	originalAmountCoin := AmountCoinID{
		Address: address,
		Amount:  100,
		Nonce:   nonce,
	}
	encodedAmountCoin := originalAmountCoin.Encode()
	decodedCoin, err = DecodeCoinID(encodedAmountCoin)
	if err != nil {
		t.Fatalf("unexpected error decoding swap coin: %v", err)
	}
	decodedAmountCoin, ok := decodedCoin.(*AmountCoinID)
	if !ok {
		t.Fatalf("expected coin to be a AmounCoinID")
	}
	if !bytes.Equal(originalAmountCoin.Address[:], decodedAmountCoin.Address[:]) {
		t.Fatalf("expected address to be equal before and after decoding")
	}
	if !bytes.Equal(originalAmountCoin.Nonce[:], decodedAmountCoin.Nonce[:]) {
		t.Fatalf("expected nonce to be equal before and after decoding")
	}
	if originalAmountCoin.Amount != decodedAmountCoin.Amount {
		t.Fatalf("expected amount to be equal before and after decoding")
	}

	// Decode amount coin id with incorrect length
	amountCoinId := make([]byte, 37)
	binary.BigEndian.PutUint16(amountCoinId[:2], uint16(CIDAmount))
	copy(amountCoinId[2:], encode.RandomBytes(35))
	if _, err := DecodeCoinID(amountCoinId); err == nil {
		t.Fatalf("expected error decoding amount coin ID with incorrect length")
	}

	// Decode coin id with non existant flag
	nonExistantCoinID := make([]byte, 37)
	binary.BigEndian.PutUint16(nonExistantCoinID[:2], uint16(5))
	copy(nonExistantCoinID, encode.RandomBytes(35))
	if _, err := DecodeCoinID(nonExistantCoinID); err == nil {
		t.Fatalf("expected error decoding coin id with non existant flag")
	}
}
