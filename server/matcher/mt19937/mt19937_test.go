// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

// Package mt19937 implements the 64-bit version of the Mersenne Twister (MT)
// pseudo-random number generator (PRNG) with a period of 2^19937-1, also known
// as mt19937_64, according to the reference implementation by Matsumoto and
// Nishimura at http://www.math.sci.hiroshima-u.ac.jp/~m-mat/MT/emt64.html
//
// MT is not cryptographically secure. Given a sufficiently long sequence of the
// generated values, one may predict additional values without knowing the seed.
// A secure hashing algorithm should be used with MT if a CSPRNG is required.
//
// When possible, the MT generator should be seeded with a slice of bytes or
// uint64 values to address known "zero-excess" seed initialization issues where
// "shifted" sequences may be generated for seeds with only a few non-zero bits.
// For more information, see
// http://www.math.sci.hiroshima-u.ac.jp/~m-mat/MT/MT2002/emt19937ar.html
package mt19937

import (
	"bufio"
	"encoding/binary"
	"math/rand"
	"os"
	"strconv"
	"testing"
)

// Ensure mt19937.Source satisfies rand.Source and rand.Source64.
var _ rand.Source = (*Source)(nil)
var _ rand.Source64 = (*Source)(nil)

func BenchmarkSource_Uint64(b *testing.B) {
	s := NewSource()
	for i := 0; i < b.N; i++ {
		s.Uint64()
	}
}

func BenchmarkSource_Int63(b *testing.B) {
	s := NewSource()
	for i := 0; i < b.N; i++ {
		s.Int63()
	}
}

func BenchmarkSource_Seed(b *testing.B) {
	s := NewSource()
	for i := 0; i < b.N; i++ {
		s.Seed(12341324)
	}
}

func TestSource_Uint64_default(t *testing.T) {
	file, err := os.Open("refuint64.dat")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Split(bufio.ScanWords)

	var refUint64 []uint64
	for scanner.Scan() {
		val, err := strconv.ParseUint(scanner.Text(), 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		refUint64 = append(refUint64, val)
	}

	// Default seed, automatically seeded on first generate.
	s := NewSource()
	for i, want := range refUint64 {
		got := s.Uint64()
		if got != want {
			t.Errorf("incorrect value #%d: got %d want %d", i, got, want)
		}
	}

	s = NewSource()
	got63 := s.Int63()
	want63 := int64(7257142393139058515)
	if got63 != want63 {
		t.Errorf("incorrect value: got %d want %d", got63, want63)
	}

	// Int63 and Uint64 both affect the state and advance the sequence.
	got64 := s.Uint64()
	want64 := uint64(4620546740167642908)
	if got64 != want64 {
		t.Errorf("incorrect value: got %d want %d", got64, want64)
	}

	// Check the 10000th value as per
	// https://en.cppreference.com/w/cpp/numeric/random/mersenne_twister_engine
	for i := 3; i < 10000; i++ {
		_ = s.Uint64()
	}
	tenThou := s.Uint64()
	wantTenThou := uint64(9981545732273789042)
	if tenThou != wantTenThou {
		t.Errorf("10000th value should be %d, got %d", wantTenThou, tenThou)
	}
}

func TestSource_Uint64_SliceSeed(t *testing.T) {
	// The authors gold standard for the array initialization tests uses the
	// following seedValues to generate the sequence at
	// http://www.math.sci.hiroshima-u.ac.jp/~m-mat/MT/mt19937-64.out.txt
	seedVals := []uint64{0x12345, 0x23456, 0x34567, 0x45678}
	file, err := os.Open("refuint64_sliceseed.dat")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Split(bufio.ScanWords)

	var refUint64 []uint64
	for scanner.Scan() {
		val, err := strconv.ParseUint(scanner.Text(), 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		refUint64 = append(refUint64, val)
	}

	s := NewSource()
	s.SeedVals(seedVals)
	for i, want := range refUint64 {
		got := s.Uint64()
		if got != want {
			t.Errorf("incorrect value #%d: got %d want %d", i, got, want)
		}
	}
}

func TestSource_Uint64_BytesSeed(t *testing.T) {
	file, err := os.Open("refuint64_sliceseed.dat")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Split(bufio.ScanWords)

	var refUint64 []uint64
	for scanner.Scan() {
		val, err := strconv.ParseUint(scanner.Text(), 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		refUint64 = append(refUint64, val)
	}

	vals := []uint64{0x12345, 0x23456, 0x34567, 0x45678}
	numBytes := len(vals) * 8
	bytes := make([]byte, numBytes)
	for i, v := range vals {
		ib := i * 8
		binary.BigEndian.PutUint64(bytes[ib:ib+8], v)
	}

	s := NewSource()
	s.SeedBytes(bytes)
	for i, want := range refUint64 {
		got := s.Uint64()
		if got != want {
			t.Errorf("incorrect value #%d: got %d want %d", i, got, want)
		}
	}

	s = NewSource()
	s.SeedBytes(bytes[:numBytes-2])
	got, want := s.Uint64(), uint64(7862454683178703257)
	if got != want {
		t.Errorf("incorrect value: got %d want %d", got, want)
	}
}
