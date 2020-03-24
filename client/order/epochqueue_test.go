package order

import (
	"bytes"
	"fmt"
	"math/rand"
	"testing"

	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/order"
	"github.com/decred/dcrd/crypto/blake256"
)

func makeEpochOrderNote(mid string, oid order.OrderID, side uint8, rate uint64, qty uint64, commitment order.Commitment, epoch uint64) *msgjson.EpochOrderNote {
	return &msgjson.EpochOrderNote{
		Commit: commitment[:],
		BookOrderNote: msgjson.BookOrderNote{
			OrderNote: msgjson.OrderNote{
				MarketID: mid,
				OrderID:  oid[:],
			},
			TradeNote: msgjson.TradeNote{
				Side:     side,
				Rate:     rate,
				Quantity: qty,
			},
		},
		Epoch: epoch,
	}
}

func makeCommitment(pimg order.Preimage) order.Commitment {
	return order.Commitment(blake256.Sum256(pimg[:]))
}

// makeMatchProof generates the sorting seed and commmitment checksum from the
// provided ordered set of preimages and commitments.
func makeMatchProof(preimages []order.Preimage, commitments []order.Commitment) (msgjson.Bytes, msgjson.Bytes, error) {
	if len(preimages) != len(commitments) {
		return nil, nil, fmt.Errorf("expected equal number of preimages and commitments")
	}

	sbuff := make([]byte, 0, len(preimages)*order.PreimageSize)
	cbuff := make([]byte, 0, len(commitments)*order.CommitmentSize)
	for i := 0; i < len(preimages); i++ {
		sbuff = append(sbuff, preimages[i][:]...)
		cbuff = append(cbuff, commitments[i][:]...)
	}
	seed := blake256.Sum256(sbuff)
	csum := blake256.Sum256(cbuff)
	return seed[:], csum[:], nil
}

func TestEpochQueue(t *testing.T) {
	mid := "mkt"
	epoch := uint64(10)
	eq := NewEpochQueue()
	n1Pimg := [32]byte{'1'}
	n1Commitment := makeCommitment(n1Pimg)
	n1OrderID := [32]byte{'a'}
	n1 := makeEpochOrderNote(mid, n1OrderID, msgjson.BuyOrderNum, 1, 2, n1Commitment, epoch)

	n2Pimg := [32]byte{'2'}
	n2Commitment := makeCommitment(n2Pimg)
	n2OrderID := [32]byte{'b'}
	n2 := makeEpochOrderNote(mid, n2OrderID, msgjson.BuyOrderNum, 1, 2, n2Commitment, epoch)

	n3Pimg := [32]byte{'3'}
	n3Commitment := makeCommitment(n3Pimg)
	n3OrderID := [32]byte{'c'}
	n3 := makeEpochOrderNote(mid, n3OrderID, msgjson.BuyOrderNum, 1, 2, n3Commitment, epoch)

	n4Pimg := [32]byte{'4'}
	n4Commitment := makeCommitment(n4Pimg)
	n4OrderID := [32]byte{'d'}
	n4 := makeEpochOrderNote(mid, n4OrderID, msgjson.BuyOrderNum, 1, 2, n4Commitment, epoch+1)

	err := eq.Enqueue(n1)
	if err != nil {
		t.Fatalf("[Enqueue]: unexpected error: %v", err)
	}

	// Ensure the epoch queue size is 1.
	if eq.Size() != 1 {
		t.Fatalf("[Size] expected queue size of %d, got %d", 1, eq.Size())
	}

	// Enqueue an order from a different epoch.
	err = eq.Enqueue(n4)
	if err == nil {
		t.Fatal("[Enqueue]: expected epoch mismatch error")
	}

	// Reset the epoch queue.
	eq.Reset()

	// Ensure the epoch queue size is 0.
	if eq.Size() != 0 {
		t.Fatalf("[Size] expected queue size of %d, got %d", 0, eq.Size())
	}

	// Ensure a new or reset epoch queue has an epoch of 0 (default).
	epoch = eq.Epoch()
	if epoch != 0 {
		t.Fatalf("[Epoch]: expected epoch value of %d, got %d", 0, epoch)
	}

	// Ensure the epoch queue does not enqueue duplicate orders.
	err = eq.Enqueue(n1)
	if err != nil {
		t.Fatalf("[Enqueue]: unexpected error: %v", err)
	}

	// Ensure the epoch is set when the first order note is queued.
	epoch = eq.Epoch()
	if err != nil {
		t.Fatalf("[Epoch]: unexpected error: %v", err)
	}

	if epoch != n1.Epoch {
		t.Fatalf("[Epoch]: expected epoch value of %d, got %d", n1.Epoch, epoch)
	}

	err = eq.Enqueue(n1)
	if err == nil {
		t.Fatal("[Enqueue]: expected a duplicate enqueue error")
	}

	// Ensure the epoch queue size is 1.
	if eq.Size() != 1 {
		t.Fatalf("[Size] expected queue size of %d, got %d", 1, eq.Size())
	}

	// Reset the epoch queue.
	eq.Reset()

	// Ensure match proof generation fails if there epoch queue is empty.
	preimages := []order.Preimage{n1Pimg, n2Pimg, n3Pimg}
	_, _, err = eq.GenerateMatchProof(preimages, nil)
	if err == nil {
		t.Fatalf("[GenerateMatchProof] expected an empty epoch queue error")
	}

	err = eq.Enqueue(n1)
	if err != nil {
		t.Fatalf("[Enqueue]: unexpected error: %v", err)
	}

	err = eq.Enqueue(n2)
	if err != nil {
		t.Fatalf("[Enqueue]: unexpected error: %v", err)
	}

	err = eq.Enqueue(n3)
	if err != nil {
		t.Fatalf("[Enqueue]: unexpected error: %v", err)
	}

	// Ensure the queue has n2 epoch order.
	if !eq.Exists(n2OrderID) {
		t.Fatalf("[Exists] expected order with id %x in the epoch queue", n2OrderID)
	}

	// Ensure the epoch queue size is 3.
	if eq.Size() != 3 {
		t.Fatalf("[Size] expected queue size of %d, got %d", 3, eq.Size())
	}

	// Ensure match proof generation works as expected.
	preimages = []order.Preimage{n1Pimg, n2Pimg, n3Pimg}
	commitments := []order.Commitment{n3Commitment, n1Commitment, n2Commitment}
	expectedSeed, expectedCmtChecksum, err := makeMatchProof(preimages, commitments)
	if err != nil {
		t.Fatalf("[makeMatchProof] unexpected error: %v", err)
	}

	seed, cmtChecksum, err := eq.GenerateMatchProof(preimages, nil)
	if err != nil {
		t.Fatalf("[GenerateMatchProof] unexpected error: %v", err)
	}

	if !bytes.Equal(expectedSeed, seed) {
		t.Fatalf("expected seed %x, got %x", expectedSeed, seed)
	}

	if !bytes.Equal(expectedCmtChecksum, cmtChecksum) {
		t.Fatalf("expected commitment checksum %x, got %x",
			expectedCmtChecksum, cmtChecksum)
	}
	eq.Reset()

	// Queue epoch orders.
	err = eq.Enqueue(n3)
	if err != nil {
		t.Fatalf("[Enqueue]: unexpected error: %v", err)
	}

	err = eq.Enqueue(n1)
	if err != nil {
		t.Fatalf("[Enqueue]: unexpected error: %v", err)
	}

	err = eq.Enqueue(n2)
	if err != nil {
		t.Fatalf("[Enqueue]: unexpected error: %v", err)
	}

	// Ensure the queue has n1 epoch order.
	if !eq.Exists(n1OrderID) {
		t.Fatalf("[Exists] expected order with id %x in the epoch queue", n1OrderID)
	}

	// Ensure match proof generation works as expected, when there are misses.
	preimages = []order.Preimage{n1Pimg, n3Pimg}
	commitments = []order.Commitment{n3Commitment, n1Commitment}
	expectedSeed, expectedCmtChecksum, err = makeMatchProof(preimages, commitments)
	if err != nil {
		t.Fatalf("[makeMatchProof] unexpected error: %v", err)
	}

	misses := []order.OrderID{n2OrderID}
	seed, cmtChecksum, err = eq.GenerateMatchProof(preimages, misses)
	if err != nil {
		t.Fatalf("[GenerateMatchProof] unexpected error: %v", err)
	}

	if !bytes.Equal(expectedSeed, seed) {
		t.Fatalf("expected seed %x, got %x", expectedSeed, seed)
	}

	if !bytes.Equal(expectedCmtChecksum, cmtChecksum) {
		t.Fatalf("expected commitment checksum %x, got %x",
			expectedCmtChecksum, cmtChecksum)
	}

	// Ensure match proof fails when there is a preimage mismatch.
	preimages = []order.Preimage{n1Pimg}
	_, _, err = eq.GenerateMatchProof(preimages, nil)
	if err == nil {
		t.Fatalf("[GenerateMatchProof] expected a preimage/orders mismatch")
	}

	preimages = []order.Preimage{n1Pimg, n2Pimg, n3Pimg}
	_, _, err = eq.GenerateMatchProof(preimages, nil)
	if err == nil {
		t.Fatalf("[GenerateMatchProof] expected no order match " +
			"for preimate error")
	}
}

func randOrderID() order.OrderID {
	var oid order.OrderID
	rand.Read(oid[:])
	return oid
}

func randPreimage() order.Preimage {
	var pi order.Preimage
	rand.Read(pi[:])
	return pi
}

func benchmarkGenerateMatchProof(c int, b *testing.B) {
	notes := make([]*msgjson.EpochOrderNote, c)
	preimages := make([]order.Preimage, 0, c)
	for i := range notes {
		pi := randPreimage()
		notes[i] = makeEpochOrderNote("mkt", randOrderID(),
			msgjson.BuyOrderNum, 1, 2, blake256.Sum256(pi[:]), 10)
		preimages = append(preimages, pi)
	}

	numMisses := c / 20
	misses := make([]order.OrderID, numMisses)
	for i := range misses {
		in := rand.Intn(len(notes))
		copy(misses[i][:], notes[in].OrderID)

		// Remove the missed preimage.
		for idx := 0; idx < len(preimages); idx++ {
			commit := blake256.Sum256(preimages[idx][:])
			if bytes.Equal(commit[:], notes[in].Commit[:]) {
				if idx < len(preimages)-1 {
					copy(preimages[idx:], preimages[idx+1:])
				}
				preimages[len(preimages)-1] = order.Preimage{}
				preimages = preimages[:len(preimages)-1]
			}
		}
	}

	eq := NewEpochQueue()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		eq.Reset()

		for _, note := range notes {
			if err := eq.Enqueue(note); err != nil {
				b.Fatalf("[Enqueue]: unexpected error: %v", err)
			}
		}

		_, _, err := eq.GenerateMatchProof(preimages, misses)
		if err != nil {
			b.Error(err)
		}
	}
}

func BenchmarkMatchProof500(b *testing.B)  { benchmarkGenerateMatchProof(500, b) }
func BenchmarkMatchProof1000(b *testing.B) { benchmarkGenerateMatchProof(1000, b) }
func BenchmarkMatchProof5000(b *testing.B) { benchmarkGenerateMatchProof(5000, b) }
