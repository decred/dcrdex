package eth

import (
	"math/big"
	"testing"

	"decred.org/dcrdex/dex/encode"
)

func TestPackCreateBondTx(t *testing.T) {
	var accountID, bondID [32]byte
	copy(accountID[:], encode.RandomBytes(32))
	copy(bondID[:], encode.RandomBytes(32))

	lockTime := uint64(1632112916)

	packedData, err := PackCreateBondTx(accountID, bondID, lockTime)
	if err != nil {
		t.Fatal(err)
	}

	decodedAccountID, decodedBondID, decodedLockTime, err := ParseCreateBondTx(packedData)
	if err != nil {
		t.Fatal(err)
	}

	if decodedAccountID != accountID {
		t.Fatalf("expected accountID %x, got %x", accountID, decodedAccountID)
	}
	if decodedBondID != bondID {
		t.Fatalf("expected bondID %x, got %x", bondID, decodedBondID)
	}
	if decodedLockTime != lockTime {
		t.Fatalf("expected lockTime %d, got %d", lockTime, decodedLockTime)
	}
}

func TestPackUpdateBondsTx(t *testing.T) {
	var accountID [32]byte
	copy(accountID[:], encode.RandomBytes(32))

	bondsToUpdate := make([][32]byte, 2)
	newBondIDs := make([][32]byte, 2)
	for i := 0; i < 2; i++ {
		copy(bondsToUpdate[i][:], encode.RandomBytes(32))
		copy(newBondIDs[i][:], encode.RandomBytes(32))
	}

	value := big.NewInt(1000000000)
	lockTime := uint64(1632112916)

	packedData, err := PackUpdateBondsTx(accountID, bondsToUpdate, newBondIDs, value, lockTime)
	if err != nil {
		t.Fatal(err)
	}

	decodedAccountID, decodedBondsToUpdate, decodedNewBondIDs, decodedValue, decodedLockTime, err := ParseUpdateBondsTx(packedData)
	if err != nil {
		t.Fatal(err)
	}

	if decodedAccountID != accountID {
		t.Fatalf("expected accountID %x, got %x", accountID, decodedAccountID)
	}
	if len(decodedBondsToUpdate) != len(bondsToUpdate) || len(decodedNewBondIDs) != len(newBondIDs) {
		t.Fatalf("expected same length of bondsToUpdate and newBondIDs, got different lengths")
	}
	for i := 0; i < len(bondsToUpdate); i++ {
		if decodedBondsToUpdate[i] != bondsToUpdate[i] {
			t.Fatalf("expected bondToUpdate %x, got %x", bondsToUpdate[i], decodedBondsToUpdate[i])
		}
		if decodedNewBondIDs[i] != newBondIDs[i] {
			t.Fatalf("expected newBondID %x, got %x", newBondIDs[i], decodedNewBondIDs[i])
		}
	}
	if decodedValue.Cmp(value) != 0 {
		t.Fatalf("expected value %s, got %s", value, decodedValue)
	}
	if decodedLockTime != lockTime {
		t.Fatalf("expected lockTime %d, got %d", lockTime, decodedLockTime)
	}
}

func TestPackRefundBondTx(t *testing.T) {
	var accountID, bondID [32]byte
	copy(accountID[:], encode.RandomBytes(32))
	copy(bondID[:], encode.RandomBytes(32))

	packedData, err := PackRefundBondTx(accountID, bondID)
	if err != nil {
		t.Fatal(err)
	}

	decodedAccountID, decodedBondID, err := ParseRefundBondTx(packedData)
	if err != nil {
		t.Fatal(err)
	}

	if decodedAccountID != accountID {
		t.Fatalf("expected accountID %x, got %x", accountID, decodedAccountID)
	}
	if decodedBondID != bondID {
		t.Fatalf("expected bondID %x, got %x", bondID, decodedBondID)
	}
}
