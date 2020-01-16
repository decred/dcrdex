// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

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

	s := NewSource()
	for i, want := range refUint64 {
		got := s.Uint64()
		if got != want {
			t.Errorf("incorrect value #%d: got %d want %d", i, got, want)
		}
	}

	s = NewSource()
	got := s.Int63()
	want := int64(7257142393139058515)
	if got != want {
		t.Errorf("incorrect value: got %d want %d", got, want)
	}
}

func TestSource_Uint64_SliceSeed(t *testing.T) {
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
	s.SeedVals([]uint64{0x12345, 0x23456, 0x34567, 0x45678})
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
