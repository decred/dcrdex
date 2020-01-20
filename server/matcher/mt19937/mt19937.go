// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mt19937

import (
	"encoding/binary"
)

const (
	defaultSeed int64 = 5489
	sliceSeed   int64 = 19650218

	n = 312 // state size
	m = 156 // shift size

	msbm uint64 = 0xffffffff80000000 // 33 most sig. bits
	lsbm uint64 = 0x000000007fffffff // 31 least sig. bits

	a uint64 = 0xb5026f5aa96619e9 // xor mask

	// tempering shift sizes and xor masks
	uShift uint64 = 29
	uMask  uint64 = 0x5555555555555555
	sShift uint64 = 17
	sMask  uint64 = 0x71d67fffeda60000
	tShift uint64 = 37
	tMask  uint64 = 0xfff7eee000000000
	lShift uint64 = 43

	// initialization values for seeding the state sequence
	ivInt    uint64 = 6364136223846793005
	ivSlice0 uint64 = 3935559000370003845
	ivSlice1 uint64 = 2862933555777941757
)

// Source is a pseudo-random number generator that satisfies both
// math/rand.Source and math/rand.Source64.
type Source struct {
	state [n]uint64
	index int
}

// NewSource creates a new unseeded Source.
func NewSource() *Source {
	return &Source{
		index: n + 1, // not seeded
	}
}

// Seed initializes the source with the provided value. Note that SeedBytes or
// SeedValues should be preferred since Mersenne Twister suffers from a
// "zero-excess" initial state defect where seeds with many zero bits can result
// in similar/"shifted" sequences. The authors describe the issues:
// http://www.math.sci.hiroshima-u.ac.jp/~m-mat/MT/MT2002/emt19937ar.html
func (s *Source) Seed(seed int64) {
	s.state[0] = uint64(seed)
	prev := s.state[0]
	for i := 1; i < n; i++ {
		s.state[i] = ivInt*(prev^prev>>62) + uint64(i)
		prev = s.state[i]
	}
	s.index = n
}

// SeedBytes seeds the mt19937 engine with up to 2496 bytes from a slice. Only
// the first 2496 bytes of the slice are used. Additional elements are unused.
func (s *Source) SeedBytes(b []byte) {
	// Pad the slice to a multiple of 8 elements if not already.
	numVals := len(b) / 8
	if len(b)%8 != 0 {
		numVals++
		bx := make([]byte, numVals*8)
		copy(bx, b)
		b = bx
	}

	// Convert the byte slice to a uint64 slice.
	vals := make([]uint64, numVals)
	for i := range vals {
		ib := i * 8
		vals[i] = binary.BigEndian.Uint64(b[ib : ib+8])
	}

	s.SeedVals(vals)
}

// SeedVals seeds the mt19937 engine with up to 312 uint64 values. Only the
// first 312 values of the slice are used. Additional elements are unused.
func (s *Source) SeedVals(v []uint64) {
	s.Seed(sliceSeed)

	is := 1 // state index
	next := func() {
		is++
		if is >= n {
			is = 1
			s.state[0] = s.state[n-1]
		}
	}

	i := n // iterator
	if len(v) > n {
		i = len(v) // TODO: test this case
	}

	for iv := 0; i > 0; i-- {
		s.state[is] = v[iv] + uint64(iv) + (s.state[is] ^ ((s.state[is-1] ^ (s.state[is-1] >> 62)) * ivSlice0))
		next()
		iv++
		if iv >= len(v) {
			iv = 0
		}
	}

	for i = n - 1; i > 0; i-- {
		s.state[is] = (s.state[is] ^ ((s.state[is-1] ^ (s.state[is-1] >> 62)) * ivSlice1)) - uint64(is)
		next()
	}

	s.state[0] = 1 << 63
}

func (s *Source) newState() {
	var i int
	for ; i < n-m; i++ {
		x := s.state[i]&msbm | s.state[i+1]&lsbm
		x = x>>1 ^ a*(x&1)
		s.state[i] = s.state[i+m] ^ x
	}
	for ; i < n-1; i++ {
		x := s.state[i]&msbm | s.state[i+1]&lsbm
		x = x>>1 ^ a*(x&1)
		s.state[i] = s.state[i+m-n] ^ x
	}
	x := s.state[n-1]&msbm | s.state[0]&lsbm
	x = x>>1 ^ a*(x&1)
	s.state[n-1] = s.state[m-1] ^ x
	s.index = 0
}

// Uint64 returns the next pseudo-random integer on [0, 2^64-1] in the sequence.
// Uint64 satisfies math/rand.Source.
func (s *Source) Uint64() uint64 {
	if s.index >= n {
		if s.index == n+1 {
			s.Seed(defaultSeed)
		}
		s.newState()
	}

	x := s.state[s.index]
	x ^= x >> uShift & uMask
	x ^= x << sShift & sMask
	x ^= x << tShift & tMask
	x ^= x >> lShift

	s.index++
	return x
}

// Int63 returns the next pseudo-random integer on [0, 2^63-1) in the sequence.
// Both Uint64 and Int63 advance the sequence. Int63 satisfies
// math/rand.Source64.
func (s *Source) Int63() int64 {
	// TODO: shift or mask?
	//return int64(s.Uint64() & 0x7fffffffffffffff)
	return int64(s.Uint64() >> 1)
}

// TODO: implement io.Reader
