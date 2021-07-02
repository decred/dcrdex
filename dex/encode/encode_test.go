package encode

import (
	"bytes"
	"testing"
)

var (
	bEqual = bytes.Equal
	tEmpty = []byte{}
	tA     = []byte{0xaa}
	tB     = []byte{0xbb, 0xbb}
	tC     = []byte{0xcc, 0xcc, 0xcc}
)

func TestBuildyBytes(t *testing.T) {
	type test struct {
		pushes [][]byte
		exp    []byte
	}
	tests := []test{
		{
			pushes: [][]byte{tA},
			exp:    []byte{0x01, 0xaa},
		},
		{
			pushes: [][]byte{tA, tB},
			exp:    []byte{1, 0xaa, 2, 0xbb, 0xbb},
		},
		{
			pushes: [][]byte{tA, nil},
			exp:    []byte{1, 0xaa, 0},
		},
		{
			pushes: [][]byte{tEmpty, tEmpty},
			exp:    []byte{0, 0},
		},
	}
	for i, tt := range tests {
		var b BuildyBytes
		for _, p := range tt.pushes {
			b = b.AddData(p)
		}
		if !bytes.Equal(b, tt.exp) {
			t.Fatalf("test %d failed", i)
		}
	}
}

func TestDecodeBlob(t *testing.T) {
	almostLongBlob := RandomBytes(254)
	longBlob := RandomBytes(255)
	longBlob2 := RandomBytes(555)
	longestUint16Blob := RandomBytes(65535)
	longerBlob := RandomBytes(65536)
	longerBlob2 := RandomBytes(65599)
	megaBlob := RandomBytes(12_345_678)
	almostLargestBlob := RandomBytes(MaxDataLen - 1)
	largestBlob := RandomBytes(MaxDataLen)
	// tooLargeBlob tested after the loop, recovering the expected panic

	type test struct {
		v       byte
		b       []byte
		exp     [][]byte
		wantErr bool
	}
	tests := []test{
		{
			v:   1,
			b:   BuildyBytes{1}.AddData(nil).AddData(tEmpty).AddData(tA),
			exp: [][]byte{tEmpty, tEmpty, tA},
		},
		{
			v:   5,
			b:   BuildyBytes{5}.AddData(tB).AddData(tC),
			exp: [][]byte{tB, tC},
		},
		{
			v:   250,
			b:   BuildyBytes{250}.AddData(tA).AddData(almostLongBlob),
			exp: [][]byte{tA, almostLongBlob},
		},
		{
			v:   255,
			b:   BuildyBytes{255}.AddData(tA).AddData(longBlob),
			exp: [][]byte{tA, longBlob},
		},
		{
			v:   255,
			b:   BuildyBytes{255}.AddData(tA).AddData(longBlob2),
			exp: [][]byte{tA, longBlob2},
		},
		{
			v:   255,
			b:   BuildyBytes{255}.AddData(tA).AddData(longestUint16Blob),
			exp: [][]byte{tA, longestUint16Blob},
		},
		{
			v:   255,
			b:   BuildyBytes{255}.AddData(tA).AddData(longerBlob),
			exp: [][]byte{tA, longerBlob},
		},
		{
			v:   255,
			b:   BuildyBytes{255}.AddData(tA).AddData(longerBlob2),
			exp: [][]byte{tA, longerBlob2},
		},
		{
			v:   255,
			b:   BuildyBytes{255}.AddData(tA).AddData(megaBlob),
			exp: [][]byte{tA, megaBlob},
		},
		{
			v:   255,
			b:   BuildyBytes{255}.AddData(tA).AddData(almostLargestBlob),
			exp: [][]byte{tA, almostLargestBlob},
		},
		{
			v:   255,
			b:   BuildyBytes{255}.AddData(tA).AddData(largestBlob),
			exp: [][]byte{tA, largestBlob},
		},
		{
			b:       []byte{0x01, 0x02}, // missing two bytes
			wantErr: true,
		},
	}
	for i, tt := range tests {
		ver, pushes, err := DecodeBlob(tt.b)
		if (err != nil) != tt.wantErr {
			t.Fatalf("test %d: %v", i, err)
		}
		if tt.wantErr {
			continue
		}
		if ver != tt.v {
			t.Fatalf("test %d: wanted version %d, got %d", i, tt.v, ver)
		}
		if len(pushes) != len(tt.exp) {
			t.Fatalf("wrongs number of pushes. wanted %d, got %d", len(tt.exp), len(pushes))
		}
		for j, push := range pushes {
			check := tt.exp[j]
			if !bytes.Equal(check, push) {
				t.Fatalf("push %d:%d incorrect. wanted %x, got %x", i, j, check, push)
			}
		}
	}

	tooLargeBlob := RandomBytes(MaxDataLen + 1)
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Errorf("no panic encoding data that's too large")
			}
		}()
		BuildyBytes{255}.AddData(tA).AddData(tooLargeBlob)
	}()
}
