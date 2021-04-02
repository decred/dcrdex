package orderbook

import (
	"bytes"
	"encoding/hex"
	"math/rand"
	"os"
	"sort"
	"testing"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/order"
	"github.com/decred/dcrd/crypto/blake256"
)

var tLogger = dex.NewLogger("TBOOK", dex.LevelTrace, os.Stdout)

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

// makeMatchProof generates the sorting seed and commitment checksum from the
// provided ordered set of preimages. The provide commitments are sorted.
func makeMatchProof(preimages []order.Preimage, commitments []order.Commitment) (msgjson.Bytes, msgjson.Bytes) {
	sort.Slice(commitments, func(i, j int) bool {
		return bytes.Compare(commitments[i][:], commitments[j][:]) < 0
	})

	cbuff := make([]byte, 0, len(commitments)*order.CommitmentSize)
	for i := range commitments {
		cbuff = append(cbuff, commitments[i][:]...)
	}
	csum := blake256.Sum256(cbuff)

	sbuff := make([]byte, 0, len(preimages)*order.PreimageSize)
	for i := 0; i < len(preimages); i++ {
		sbuff = append(sbuff, preimages[i][:]...)
	}
	seed := blake256.Sum256(sbuff)

	return seed[:], csum[:]
}

func TestEpochQueue(t *testing.T) {
	mid := "mkt"
	epoch := uint64(10)
	eq := NewEpochQueue()
	n1PimgB, _ := hex.DecodeString("e1f796fa0fc16ba7bb90be2a33e87c3d60ab628471a420834383661801bb0bfd")
	var n1Pimg order.Preimage
	copy(n1Pimg[:], n1PimgB)
	n1Commitment := n1Pimg.Commit() // aba75140b1f6edf26955a97e1b09d7b17abdc9c0b099fc73d9729501652fbf66
	n1OrderID := [32]byte{'a'}
	n1 := makeEpochOrderNote(mid, n1OrderID, msgjson.BuyOrderNum, 1, 2, n1Commitment, epoch)

	n2PimgB, _ := hex.DecodeString("8e6c140071db1eb2f7a18194f1a045a94c078835c75dff2f3e836180baad9e95")
	var n2Pimg order.Preimage
	copy(n2Pimg[:], n2PimgB)
	n2Commitment := n2Pimg.Commit() // 0f4bc030d392cef3f44d0781870ab7fcb78a0cda36c73e50b88c741b4f851600
	n2OrderID := [32]byte{'b'}
	n2 := makeEpochOrderNote(mid, n2OrderID, msgjson.BuyOrderNum, 1, 2, n2Commitment, epoch)

	n3PimgB, _ := hex.DecodeString("e1f796fa0fc16ba7bb90be2a33e87c3d60ab628471a420834383661801bb0bfd")
	var n3Pimg order.Preimage
	copy(n3Pimg[:], n3PimgB)
	n3Commitment := n3Pimg.Commit() // aba75140b1f6edf26955a97e1b09d7b17abdc9c0b099fc73d9729501652fbf66
	n3OrderID := [32]byte{'c'}
	n3 := makeEpochOrderNote(mid, n3OrderID, msgjson.BuyOrderNum, 1, 2, n3Commitment, epoch)

	// This csum matches the server-side tests.
	wantCSum, _ := hex.DecodeString("8c743c3225b89ffbb50b5d766d3e078cd8e2658fa8cb6e543c4101e1d59a8e8e")

	err := eq.Enqueue(n1)
	if err != nil {
		t.Fatalf("[Enqueue]: unexpected error: %v", err)
	}

	// Ensure the epoch queue size is 1.
	if eq.Size() != 1 {
		t.Fatalf("[Size] expected queue size of %d, got %d", 1, eq.Size())
	}

	// Reset the epoch queue.
	eq = NewEpochQueue()

	// Ensure the epoch queue does not enqueue duplicate orders.
	err = eq.Enqueue(n1)
	if err != nil {
		t.Fatalf("[Enqueue]: unexpected error: %v", err)
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
	eq = NewEpochQueue()

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
	commitments := []order.Commitment{n1Commitment, n3Commitment, n2Commitment}
	expectedSeed, expectedCmtChecksum := makeMatchProof(preimages, commitments)

	seed, cmtChecksum, err := eq.GenerateMatchProof(preimages, nil)
	if err != nil {
		t.Fatalf("[GenerateMatchProof] unexpected error: %v", err)
	}

	if !bytes.Equal(expectedSeed, seed) {
		t.Fatalf("expected seed %v, got %v", expectedSeed, seed)
	}

	if !bytes.Equal(expectedCmtChecksum, cmtChecksum) {
		t.Fatalf("expected commitment checksum %v, got %v",
			expectedCmtChecksum, cmtChecksum)
	}
	if !bytes.Equal(wantCSum, cmtChecksum) {
		t.Fatalf("expected commitment checksum %v, got %v",
			wantCSum, cmtChecksum)
	}

	eq = NewEpochQueue()

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
	preimages = []order.Preimage{n1Pimg, n2Pimg}                               // n3 missed
	commitments = []order.Commitment{n1Commitment, n2Commitment, n3Commitment} // all orders in the epoch queue
	expectedSeed, expectedCmtChecksum = makeMatchProof(preimages, commitments)

	misses := []order.OrderID{n3OrderID}
	seed, cmtChecksum, err = eq.GenerateMatchProof(preimages, misses)
	if err != nil {
		t.Fatalf("[GenerateMatchProof] unexpected error: %v", err)
	}

	if !bytes.Equal(expectedSeed, seed) {
		t.Fatalf("expected seed %v, got %v", expectedSeed, seed)
	}

	if !bytes.Equal(expectedCmtChecksum, cmtChecksum) {
		t.Fatalf("expected commitment checksum %v, got %v",
			expectedCmtChecksum, cmtChecksum)
	}
	if !bytes.Equal(wantCSum, cmtChecksum) {
		t.Fatalf("expected commitment checksum %v, got %v",
			wantCSum, cmtChecksum)
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
		t.Fatalf("[GenerateMatchProof] expected no order match for preimage error")
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

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		eq := NewEpochQueue()

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
