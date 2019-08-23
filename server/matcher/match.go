package matcher

import (
	"bytes"
	"encoding/binary"
	"math/rand"
	"sort"

	"github.com/decred/dcrd/crypto/blake256"
	"github.com/decred/dcrdex/server/market/order"
)

// HashFunc is the hash function used to generate the shuffling seed.
var HashFunc = blake256.Sum256

const (
	HashSize = blake256.Size
)

type Matcher struct{}

func New( /* TODO */ ) *Matcher {
	return new(Matcher)
}

// Match matches orders given a standing order book and an epoch queue.
func (m *Matcher) Match(book Booker, queue []order.Order) []*order.Match {
	// Start by applying the deterministic pseudorandom shuffling. Then apply
	// matching rules, requesting best buy and sell orders from the book as
	// needed.
	return nil // TODO
}

// sortQueue lexicographically sorts the Orders by their IDs.
func sortQueue(queue []order.Order) {
	sort.Slice(queue, func(i, j int) bool {
		ii, ij := queue[i].ID(), queue[j].ID()
		return bytes.Compare(ii[:], ij[:]) >= 0
	})
}

// shuffleQueue deterministically shuffles the Orders using a Fisher-Yates
// algorithm seeded with the hash of the concatenated order ID hashes.
func shuffleQueue(queue []order.Order) {
	// The shuffling seed is derived from the sorted orders.
	sortQueue(queue)

	// Compute and concatenate the hashes of the order IDs.
	qLen := len(queue)
	hashCat := make([]byte, HashSize*qLen)
	for i, o := range queue {
		id := o.ID()
		h := HashFunc(id[:])
		copy(hashCat[HashSize*i:HashSize*(i+1)], h[:])
	}

	// Fisher-Yates shuffle the slice using a seed derived from the hash of the
	// concatenated order ID hashes.
	seedHash := HashFunc(hashCat)
	seed := int64(binary.LittleEndian.Uint64(seedHash[:8]))
	prng := rand.New(rand.NewSource(seed))
	for i := range queue {
		j := prng.Intn(qLen-i) + i
		queue[i], queue[j] = queue[j], queue[i]
	}
}
