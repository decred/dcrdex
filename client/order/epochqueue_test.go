package order

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"

	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/order"
	"github.com/decred/dcrd/crypto/blake256"
)

func makeEpochOrderNote(mid string, side uint8, oid order.OrderID, rate uint64,
	qty uint64, commitment order.Commitment) *msgjson.EpochOrderNote {
	return &msgjson.EpochOrderNote{
		BookOrderNote: msgjson.BookOrderNote{
			TradeNote: msgjson.TradeNote{
				Side:     side,
				Rate:     rate,
				Quantity: qty,
			},
			OrderNote: msgjson.OrderNote{
				MarketID:   mid,
				OrderID:    oid[:],
				Commitment: commitment[:],
			},
		},
	}
}

func makeCommitment(pimg order.Preimage) order.Commitment {
	return order.Commitment(blake256.Sum256(pimg[:]))
}

// makeMatchProof generates the sorting seed and commmitment checksum from the
// provided ordered set of preimages and commitments.
func makeMatchProof(preimages []order.Preimage, commitments []order.Commitment) (int64, msgjson.Bytes, error) {
	if len(preimages) != len(commitments) {
		return 0, nil, fmt.Errorf("expected equal number of preimages and commitments")
	}

	sbuff := make([]byte, 0, len(preimages)*order.PreimageSize)
	cbuff := make([]byte, 0, len(commitments)*order.CommitmentSize)
	for i := 0; i < len(preimages); i++ {
		sbuff = append(sbuff, preimages[i][:]...)
		cbuff = append(cbuff, commitments[i][:]...)
	}
	seed := int64(binary.LittleEndian.Uint64(sbuff[:64]))
	csum := blake256.Sum256(cbuff)
	return seed, msgjson.Bytes(csum[:]), nil
}

func makeSortOrder(ids ...order.OrderID) []order.OrderID {
	so := make([]order.OrderID, 0, len(ids))
	for _, entry := range ids {
		var oid order.OrderID
		copy(oid[:], entry[:])
		so = append(so, oid)
	}
	return so
}

func TestEpochQueue(t *testing.T) {
	mid := "mkt"
	eq := NewEpochQueue()
	n1Pimg := [32]byte{'1'}
	n1Commitment := makeCommitment(n1Pimg)
	n1OrderID := [32]byte{'a'}
	n1 := makeEpochOrderNote(mid, msgjson.BuyOrderNum, n1OrderID, 1, 3, n1Commitment)
	eq.Queue(n1)

	// Ensure the epoch queue size is 1.
	if eq.Size() != 1 {
		t.Fatalf("[Size] expected queue size of %d, got %d", 1, eq.Size())
	}

	// Reset the epoch queue.
	eq.Reset()

	// Ensure the epoch queue size is 0.
	if eq.Size() != 0 {
		t.Fatalf("[Size] expected queue size of %d, got %d", 0, eq.Size())
	}

	eq.Queue(n1)

	n2Pimg := [32]byte{'2'}
	n2Commitment := makeCommitment(n2Pimg)
	n2OrderID := [32]byte{'b'}
	n2 := makeEpochOrderNote(mid, msgjson.BuyOrderNum, n2OrderID, 2, 4, n2Commitment)
	eq.Queue(n2)

	n3Pimg := [32]byte{'3'}
	n3Commitment := makeCommitment(n3Pimg)
	n3OrderID := [32]byte{'c'}
	n3 := makeEpochOrderNote(mid, msgjson.BuyOrderNum, n3OrderID, 3, 6, n3Commitment)
	eq.Queue(n3)

	// Ensure the queue has n2 epoch order.
	if !eq.Exists(n2OrderID) {
		t.Fatalf("[Exists] expected order with id %x in the epoch queue", n2OrderID)
	}

	// Ensure the epoch queue size is 3.
	if eq.Size() != 3 {
		t.Fatalf("[Size] expected queue size of %d, got %d", 3, eq.Size())
	}

	// Ensure the seed index has the expected order.
	expectedSeedSO := makeSortOrder(n1OrderID, n2OrderID, n3OrderID)

	eq.seedIndexMtx.Lock()
	for i := 0; i < len(eq.seedIndex); i++ {
		if !bytes.Equal(expectedSeedSO[i][:], eq.seedIndex[i][:]) {
			t.Fatalf("expected id %s at seed sort order index %d, got %s",
				expectedSeedSO[i].String(), i, eq.seedIndex[i].String())
		}
	}
	eq.seedIndexMtx.Unlock()

	// Ensure the commitment index has the expected order.
	expectedCommitmentSO := makeSortOrder(n3OrderID, n1OrderID, n2OrderID)

	eq.commitmentIndexMtx.Lock()
	for i := 0; i < len(eq.commitmentIndex); i++ {
		if !bytes.Equal(expectedCommitmentSO[i][:], eq.commitmentIndex[i][:]) {
			t.Fatalf("expected id %s at commitment sort order index %d, got %s",
				expectedCommitmentSO[i].String(), i, eq.seedIndex[i].String())
		}
	}
	eq.commitmentIndexMtx.Unlock()

	// Ensure match proof generation works as expected.
	preimages := []order.Preimage{n1Pimg, n2Pimg, n3Pimg}
	commitments := []order.Commitment{n3Commitment, n1Commitment, n2Commitment}
	expectedSeed, expectedCmtChecksum, err := makeMatchProof(preimages, commitments)
	if err != nil {
		t.Fatalf("[makeMatchProof] unexpected error: %v", err)
	}

	seed, cmtChecksum, err := eq.GenerateMatchProof(preimages, nil)
	if err != nil {
		t.Fatalf("[GenerateMatchProof] unexpected error: %v", err)
	}

	if expectedSeed != seed {
		t.Fatalf("expected seed %d, got %d", expectedSeed, seed)
	}

	if !bytes.Equal(expectedCmtChecksum, cmtChecksum) {
		t.Fatalf("expected commitment checksum %x, got %x",
			expectedCmtChecksum, cmtChecksum)
	}

	eq.Reset()

	// Queue epoch orders.
	eq.Queue(n3)
	eq.Queue(n1)
	eq.Queue(n2)

	// Ensure the queue has n1 epoch order.
	if !eq.Exists(n1OrderID) {
		t.Fatalf("[Exists] expected order with id %x in the epoch queue", n1OrderID)
	}

	// Ensure the seed index has the expected order.
	eq.seedIndexMtx.Lock()
	for i := 0; i < len(eq.seedIndex); i++ {
		if !bytes.Equal(expectedSeedSO[i][:], eq.seedIndex[i][:]) {
			t.Fatalf("expected id %s at seed sort order index %d, got %s",
				expectedSeedSO[i].String(), i, eq.seedIndex[i].String())
		}
	}
	eq.seedIndexMtx.Unlock()

	// Ensure the commitment index has the expected order.
	eq.commitmentIndexMtx.Lock()
	for i := 0; i < len(eq.commitmentIndex); i++ {
		if !bytes.Equal(expectedCommitmentSO[i][:], eq.commitmentIndex[i][:]) {
			t.Fatalf("expected id %s at commitment sort order index %d, got %s",
				expectedCommitmentSO[i].String(), i, eq.seedIndex[i].String())
		}
	}
	eq.commitmentIndexMtx.Unlock()

	// Ensure match proof generation works as expected, when there are misses.
	preimages = []order.Preimage{n1Pimg, n3Pimg}
	commitments = []order.Commitment{n3Commitment, n1Commitment}
	expectedSeed, expectedCmtChecksum, err = makeMatchProof(preimages, commitments)
	if err != nil {
		t.Fatalf("[makeMatchProof] unexpected error: %v", err)
	}

	misses := []string{n2.OrderID.String()}
	seed, cmtChecksum, err = eq.GenerateMatchProof(preimages, misses)
	if err != nil {
		t.Fatalf("[GenerateMatchProof] unexpected error: %v", err)
	}

	if expectedSeed != seed {
		t.Fatalf("expected seed %d, got %d", expectedSeed, seed)
	}

	if !bytes.Equal(expectedCmtChecksum, cmtChecksum) {
		t.Fatalf("expected commitment checksum %x, got %x",
			expectedCmtChecksum, cmtChecksum)
	}
}
