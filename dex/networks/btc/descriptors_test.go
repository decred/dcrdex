// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package btc

import (
	"bytes"
	"encoding/hex"
	"reflect"
	"testing"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/davecgh/go-spew/spew"
)

var chainParams = &chaincfg.RegressionNetParams

func TestParseDescriptor(t *testing.T) {
	tests := []struct {
		name    string
		desc    string
		want    *Descriptor
		wantErr bool
	}{
		{
			"wpkh with bare key",
			"wpkh([8a94b43c]039e9e0813e46041e2fddf46640006f4e9ae5d4d6ab811d0d2a6b372d0b136ba8a)#eq3vqyes",
			&Descriptor{
				Function: "wpkh",
				KeyOrigin: &KeyOrigin{
					Fingerprint: "8a94b43c", // fingerprint of corresponding private key itself
					Path:        "",
					Steps:       nil,
				},
				Key:        "039e9e0813e46041e2fddf46640006f4e9ae5d4d6ab811d0d2a6b372d0b136ba8a",
				KeyFmt:     KeyHexPub,
				Nested:     nil,
				Expression: "[8a94b43c]039e9e0813e46041e2fddf46640006f4e9ae5d4d6ab811d0d2a6b372d0b136ba8a",
				Checksum:   "eq3vqyes",
			},
			false,
		}, {
			"wpkh with origin and checksum",
			"wpkh([b940190e/84'/1'/0'/0/0]030003429cd5d23b1a229ec88dba6f2b69fb539fe26cd80229267aa0c992dc26b2)#0pfw7rck",
			&Descriptor{
				Function: "wpkh",
				KeyOrigin: &KeyOrigin{
					Fingerprint: "b940190e",
					Path:        "84'/1'/0'/0/0",
					Steps:       []uint32{2147483732, 2147483649, 2147483648, 0, 0},
				},
				Key:        "030003429cd5d23b1a229ec88dba6f2b69fb539fe26cd80229267aa0c992dc26b2",
				KeyFmt:     KeyHexPub,
				Nested:     nil,
				Expression: "[b940190e/84'/1'/0'/0/0]030003429cd5d23b1a229ec88dba6f2b69fb539fe26cd80229267aa0c992dc26b2",
				Checksum:   "0pfw7rck",
			},
			false,
		}, {
			"range/extended wpkh-in-sh with origin and checksum",
			"sh(wpkh([b940190e/49'/1'/0']tpubDCDYiBwbWWM3FRB55DcdgWyr7AVraCmXSgnVHZpyJ716tWigvdhShXGgAREnQwXjBqvnuaT7k1oHA5LD2HN5uPjp1u4ubAemppGmqioFHAq/1/*))#a73wy5hk",
			&Descriptor{
				Function: "sh",
				KeyOrigin: &KeyOrigin{ // dup of nested
					Fingerprint: "b940190e",
					Path:        "49'/1'/0'",
					Steps:       []uint32{2147483697, 2147483649, 2147483648},
				},
				Key:    "tpubDCDYiBwbWWM3FRB55DcdgWyr7AVraCmXSgnVHZpyJ716tWigvdhShXGgAREnQwXjBqvnuaT7k1oHA5LD2HN5uPjp1u4ubAemppGmqioFHAq/1/*", // dup of nested
				KeyFmt: KeyExtended,                                                                                                           // dup of nested
				Nested: &Descriptor{
					Function: "wpkh",
					KeyOrigin: &KeyOrigin{
						Fingerprint: "b940190e",
						Path:        "49'/1'/0'",
						Steps:       []uint32{2147483697, 2147483649, 2147483648},
					},
					Key:        "tpubDCDYiBwbWWM3FRB55DcdgWyr7AVraCmXSgnVHZpyJ716tWigvdhShXGgAREnQwXjBqvnuaT7k1oHA5LD2HN5uPjp1u4ubAemppGmqioFHAq/1/*",
					KeyFmt:     KeyExtended,
					Nested:     nil,
					Expression: "[b940190e/49'/1'/0']tpubDCDYiBwbWWM3FRB55DcdgWyr7AVraCmXSgnVHZpyJ716tWigvdhShXGgAREnQwXjBqvnuaT7k1oHA5LD2HN5uPjp1u4ubAemppGmqioFHAq/1/*",
					Checksum:   "",
				},
				Expression: "wpkh([b940190e/49'/1'/0']tpubDCDYiBwbWWM3FRB55DcdgWyr7AVraCmXSgnVHZpyJ716tWigvdhShXGgAREnQwXjBqvnuaT7k1oHA5LD2HN5uPjp1u4ubAemppGmqioFHAq/1/*)",
				Checksum:   "a73wy5hk",
			},
			false,
		}, {
			"range private wpkh master (no key origin)",
			"wpkh(tprv8ZgxMBicQKsPdAQ2QZeTReB2hH2aKXBWGqgnrW1aYbutbC7YfUtPPJm1Nppb6eXy5hnLRrRqwCctBecfZV8HLNsLeivVhKT1BYFBiRbhUES/84'/1'/0'/0/*)#pm06dltl",
			&Descriptor{
				Function:   "wpkh",
				KeyOrigin:  nil,
				Key:        "tprv8ZgxMBicQKsPdAQ2QZeTReB2hH2aKXBWGqgnrW1aYbutbC7YfUtPPJm1Nppb6eXy5hnLRrRqwCctBecfZV8HLNsLeivVhKT1BYFBiRbhUES/84'/1'/0'/0/*",
				KeyFmt:     KeyExtended,
				Nested:     nil,
				Expression: "tprv8ZgxMBicQKsPdAQ2QZeTReB2hH2aKXBWGqgnrW1aYbutbC7YfUtPPJm1Nppb6eXy5hnLRrRqwCctBecfZV8HLNsLeivVhKT1BYFBiRbhUES/84'/1'/0'/0/*",
				Checksum:   "pm06dltl",
			},
			false,
		}, {
			"invalid nested sh",
			"wsh(sh(abababab))",
			nil,
			true,
		}, {
			"invalid nested wsh",
			"wsh(wsh(abababab))",
			nil,
			true,
		},
		{
			"valid nested wsh",
			"sh(wsh(pk(03a40e1db1a51231027a8261b15317a59218b6c1c5dbd3d155688a3fb32c547ccc)))",
			&Descriptor{
				Function:  "sh",
				KeyOrigin: nil,
				Key:       "03a40e1db1a51231027a8261b15317a59218b6c1c5dbd3d155688a3fb32c547ccc", // dup of nested
				KeyFmt:    KeyHexPub,                                                            // dup of nested
				Nested: &Descriptor{
					Function:  "wsh",
					KeyOrigin: nil,
					Key:       "03a40e1db1a51231027a8261b15317a59218b6c1c5dbd3d155688a3fb32c547ccc",
					KeyFmt:    KeyHexPub,
					Nested: &Descriptor{
						Function:   "pk",
						KeyOrigin:  nil,
						Key:        "03a40e1db1a51231027a8261b15317a59218b6c1c5dbd3d155688a3fb32c547ccc",
						KeyFmt:     KeyHexPub,
						Nested:     nil,
						Expression: "03a40e1db1a51231027a8261b15317a59218b6c1c5dbd3d155688a3fb32c547ccc",
						Checksum:   "",
					},
					Expression: "pk(03a40e1db1a51231027a8261b15317a59218b6c1c5dbd3d155688a3fb32c547ccc)",
					Checksum:   "",
				},
				Expression: "wsh(pk(03a40e1db1a51231027a8261b15317a59218b6c1c5dbd3d155688a3fb32c547ccc))",
				Checksum:   "",
			},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseDescriptor(tt.desc)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseDescriptor() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseDescriptor() = %#v, want %#v", got, tt.want)
				spew.Dump(got)
			}
		})
	}
}

func TestDescriptors(t *testing.T) {
	// getaddressinfo bcrt1qwuqqg9dajf7f7ddxp4un3p2dtagvrny4qll6xe
	descAddr := "wpkh([b940190e/84'/1'/0'/0/0]030003429cd5d23b1a229ec88dba6f2b69fb539fe26cd80229267aa0c992dc26b2)#0pfw7rck"
	d, err := ParseDescriptor(descAddr)
	if err != nil {
		t.Fatal(err)
	}
	if d.KeyFmt != KeyHexPub {
		t.Fatalf("wrong key type: %v", d.Key)
	}

	if d.KeyOrigin == nil {
		t.Fatalf("no key origin section")
	}
	if d.KeyOrigin.Fingerprint != "b940190e" {
		t.Fatalf("incorrect key origin master fingerprint: %v", d.KeyOrigin.Fingerprint)
	}
	if d.KeyOrigin.Path != "84'/1'/0'/0/0" {
		t.Fatalf("wrong path %q", d.KeyOrigin.Path)
	}
	if len(d.KeyOrigin.Steps) != 5 {
		t.Fatalf("wrong number of steps in path: %d", len(d.KeyOrigin.Steps))
	}
	addrPath := d.KeyOrigin.Steps
	addrOriginFP := d.KeyOrigin.Fingerprint

	addrPubKey, err := hex.DecodeString(d.Key)
	if err != nil {
		t.Fatalf("DecodeString: %v", err)
	}
	if _, err = btcec.ParsePubKey(addrPubKey); err != nil {
		t.Fatalf("ParsePubKey: %v", err)
	}
	addr, err := btcutil.NewAddressWitnessPubKeyHash(btcutil.Hash160(addrPubKey), chainParams)
	if err != nil {
		t.Fatalf("NewAddressWitnessPubKeyHash: %v", err)
	}
	if addr.String() != "bcrt1qwuqqg9dajf7f7ddxp4un3p2dtagvrny4qll6xe" {
		t.Fatal("wrong address pubkey")
	}

	// listdescriptors private=true

	descPriv := "wpkh(tprv8ZgxMBicQKsPdAQ2QZeTReB2hH2aKXBWGqgnrW1aYbutbC7YfUtPPJm1Nppb6eXy5hnLRrRqwCctBecfZV8HLNsLeivVhKT1BYFBiRbhUES/84'/1'/0'/0/*)#pm06dltl"
	dPriv, err := ParseDescriptor(descPriv)
	if err != nil {
		t.Fatal(err)
	}
	if dPriv.KeyFmt != KeyExtended {
		t.Fatalf("wrong key type: %v", dPriv.Key)
	}
	if dPriv.KeyOrigin != nil {
		t.Fatalf("unexpected key origin section")
	}

	xPriv, fingerprint, pathStr, isRange, err := ParseKeyExtended(dPriv.Key)
	if err != nil {
		t.Fatal(err)
	}
	if !xPriv.IsPrivate() {
		t.Fatal("not an extended private key")
	}
	if !isRange {
		t.Fatal("not a range path")
	}
	parentPath, isRange, err := ParsePath(pathStr)
	if err != nil {
		t.Fatal(err)
	}
	if isRange {
		t.Fatal("cleaned path was a range path")
	}

	// addrPath:    b940190e/84'/1'/0'/0/0   <-- child index at end
	// parentPath:  b940190e/84'/1'/0'/0
	if len(addrPath) != len(parentPath)+1 {
		t.Fatal("paths do not agree")
	}
	for i := range parentPath {
		if addrPath[i] != parentPath[i] {
			t.Errorf("path depth %d incorrect, want %d, got %d", i, addrPath[i], parentPath[i])
		}
	}
	childIdx := addrPath[len(addrPath)-1]

	// fingerprint := hex.EncodeToString(keyFingerprint(xPriv)) // this is what we need to get for each entry in listdescriptors
	if addrOriginFP != fingerprint {
		t.Fatalf("wrong fingerprint")
	}

	// now that we have the corresponding master private key, derive the branch
	// extended private key, then the child address key itself.

	branch, err := DeepChild(xPriv, parentPath)
	if err != nil {
		t.Fatalf("DeepChild: %v", err)
	}

	child, _ := branch.Derive(childIdx)
	pubkey := pubKeyBytes(child)
	pkh := btcutil.Hash160(pubkey)
	addrWPKH, err := btcutil.NewAddressWitnessPubKeyHash(pkh, chainParams)
	if err != nil {
		t.Fatalf("NewAddressWitnessPubKeyHash: %v", err)
	}
	if addrWPKH.String() != addr.String() {
		t.Fatalf("wrong address %s", addrWPKH)
	}

	privkey, err := child.ECPrivKey()
	if err != nil {
		t.Fatalf("ECPrivKey: %v", err)
	}
	pubkey = privkey.PubKey().SerializeCompressed()
	// check against "pubkey" field of getaddressinfo result
	if !bytes.Equal(pubkey, addrPubKey) {
		t.Fatal("wrong address pubkey")
	}
}

func TestParsePath(t *testing.T) {
	tests := []struct {
		name        string
		path        string
		wantPath    []uint32
		wantIsRange bool
		wantErr     bool
	}{
		{
			"empty",
			"",
			nil,
			false,
			false,
		}, {
			"unprefixed", // like path from ParseDescriptor or ParseKeyExtended
			"84'/1'/0'/0/0",
			[]uint32{2147483732, 2147483649, 2147483648, 0, 0},
			false,
			false,
		}, {
			"m", // like "hdkeypath" in getaddressinfo response
			"m/84'/1h/0'/0/0",
			[]uint32{2147483732, 2147483649, 2147483648, 0, 0},
			false,
			false,
		}, {
			"/",
			"/84'/1'/0'/0h/0",
			[]uint32{2147483732, 2147483649, 2147483648, 2147483648, 0},
			false,
			false,
		}, {
			"range",
			"m/84'/1'/0'/0/0/*",
			[]uint32{2147483732, 2147483649, 2147483648, 0, 0},
			true,
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotPath, gotIsRange, err := ParsePath(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParsePath() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(gotPath, tt.wantPath) {
				t.Errorf("ParsePath() gotPath = %v, want %v", gotPath, tt.wantPath)
			}
			if gotIsRange != tt.wantIsRange {
				t.Errorf("ParsePath() gotIsRange = %v, want %v", gotIsRange, tt.wantIsRange)
			}
		})
	}
}

func TestParseKeyExtended(t *testing.T) {
	tests := []struct {
		name            string
		keyStr          string
		wantFingerprint string
		wantPath        string
		wantIsRange     bool
		wantErr         bool
	}{
		{
			"tprv with ranged path",
			"tprv8ZgxMBicQKsPdAQ2QZeTReB2hH2aKXBWGqgnrW1aYbutbC7YfUtPPJm1Nppb6eXy5hnLRrRqwCctBecfZV8HLNsLeivVhKT1BYFBiRbhUES/84'/1'/0'/0/*",
			"b940190e",
			"84'/1'/0'/0",
			true,
			false,
		}, {
			"just tprv",
			"tprv8ZgxMBicQKsPdAQ2QZeTReB2hH2aKXBWGqgnrW1aYbutbC7YfUtPPJm1Nppb6eXy5hnLRrRqwCctBecfZV8HLNsLeivVhKT1BYFBiRbhUES",
			"b940190e",
			"",
			false,
			false,
		}, {
			"full tpub",
			"tpubDCoAK65iKNvE5x3wnCb87xRwD8wKDEKUyymu49KSgj9c5PG7DbfnYvwoPjgZaGhgTR4GfAQECPxrya46jeyiVn7jT1wuLDvb5CjJG6Q8FbT/0/*",
			"af5360d5",
			"0",
			true,
			false,
		}, {
			"hex pub",
			"030003429cd5d23b1a229ec88dba6f2b69fb539fe26cd80229267aa0c992dc26b2",
			"",
			"",
			false,
			true,
		}, {
			"wif priv",
			"cSqGdZNwiMcqJqC1NCwigLtQWmooMNnz5jMVuzuLd4pRiLP7CgFM", // alpha harness addr key
			"",
			"",
			false,
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, gotFingerprint, gotPath, gotIsRange, err := ParseKeyExtended(tt.keyStr)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseKeyExtended() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}
			if gotFingerprint != tt.wantFingerprint {
				t.Errorf("ParseKeyExtended() gotFingerprint = %v, want %v", gotFingerprint, tt.wantFingerprint)
			}
			if gotPath != tt.wantPath {
				t.Errorf("ParseKeyExtended() gotPath = %v, want %v", gotPath, tt.wantPath)
			}
			if gotIsRange != tt.wantIsRange {
				t.Errorf("ParseKeyExtended() gotIsRange = %v, want %v", gotIsRange, tt.wantIsRange)
			}
		})
	}
}

func Test_checkDescriptorKey(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want KeyFmt
	}{
		{
			"hex pub (compressed)",
			"030003429cd5d23b1a229ec88dba6f2b69fb539fe26cd80229267aa0c992dc26b2",
			KeyHexPub,
		}, {
			"hex pub (uncompressed)",
			"040003429cd5d23b1a229ec88dba6f2b69fb539fe26cd80229267aa0c992dc26b2cd3d19f0341e9064e21400bcde458ec96c38c25924413440c47cf5358443e871",
			KeyHexPub,
		}, {
			"wif priv",
			"cSqGdZNwiMcqJqC1NCwigLtQWmooMNnz5jMVuzuLd4pRiLP7CgFM",
			KeyWIFPriv,
		}, {
			"extended with path",
			"tpubDCoAK65iKNvE5x3wnCb87xRwD8wKDEKUyymu49KSgj9c5PG7DbfnYvwoPjgZaGhgTR4GfAQECPxrya46jeyiVn7jT1wuLDvb5CjJG6Q8FbT/0/*",
			KeyExtended,
		}, {
			"extended no path",
			"tpubDCoAK65iKNvE5x3wnCb87xRwD8wKDEKUyymu49KSgj9c5PG7DbfnYvwoPjgZaGhgTR4GfAQECPxrya46jeyiVn7jT1wuLDvb5CjJG6Q8FbT",
			KeyExtended,
		}, {
			"unknown",
			"asdfsadfsadf",
			KeyUnknown,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := checkDescriptorKey(tt.key); got != tt.want {
				t.Errorf("checkDescriptorKey() = %v, want %v", got, tt.want)
			}
		})
	}
}
