package encode

import (
	"testing"
)

var (
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
		if !bEqual(b, tt.exp) {
			t.Fatalf("test %d failed", i)
		}
	}
}

func TestDecodeBlob(t *testing.T) {
	longBlob := RandomBytes(255)
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
			v:   255,
			b:   BuildyBytes{255}.AddData(tA).AddData(longBlob),
			exp: [][]byte{tA, longBlob},
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
			if !bEqual(check, push) {
				t.Fatalf("push %d:%d incorrect. wanted %x, got %x", i, j, check, push)
			}
		}
	}
}
