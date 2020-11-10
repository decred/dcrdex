// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package main

import (
	"bytes"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/decred/dcrd/dcrec/secp256k1/v3"
)

func Test_createAndStoreKey(t *testing.T) {
	dir, _ := ioutil.TempDir("", "test")
	defer os.RemoveAll(dir)

	file := "newkey"

	tests := []struct {
		name    string
		path    string
		pass    []byte
		wantErr bool
	}{
		{
			"bad path",
			"/totally/not/a/path",
			[]byte("pass1234"),
			true,
		},
		{
			"ok new",
			filepath.Join(dir, file),
			[]byte("pass1234"),
			false,
		},
		{
			"already exists",
			filepath.Join(dir, file),
			[]byte("pass1234"),
			true,
		},
		{
			"empty pass",
			filepath.Join(dir, "newkey2"),
			[]byte{},
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := createAndStoreKey(tt.path, tt.pass)
			if (err != nil) != tt.wantErr {
				t.Errorf("createAndStoreKey() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
		})
	}
}

func Test_loadKeyFile(t *testing.T) {
	dir, _ := ioutil.TempDir("", "test")
	defer os.RemoveAll(dir)

	fullFile := filepath.Join(dir, "newkey")
	pass := []byte("pass1234")

	privKey, err := createAndStoreKey(fullFile, pass)
	if err != nil {
		t.Fatalf("createAndStoreKey: %v", err)
	}

	tests := []struct {
		name    string
		path    string
		pass    []byte
		want    *secp256k1.PrivateKey
		wantErr bool
	}{
		{
			"ok",
			fullFile,
			[]byte("pass1234"),
			privKey,
			false,
		},
		{
			"bad path",
			filepath.Join(dir, "wrongName"),
			[]byte("pass1234"),
			nil,
			true,
		},
		{
			"wrong pass",
			fullFile,
			[]byte("adsd"),
			nil,
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pk, err := loadKeyFile(tt.path, tt.pass)
			if (err != nil) != tt.wantErr {
				t.Errorf("loadKeyFile() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.want == nil {
				return
			}
			if !bytes.Equal(tt.want.Serialize(), pk.Serialize()) {
				t.Errorf("private key mismatch")
			}
		})
	}
}

func Test_dexKey(t *testing.T) {
	dir, _ := ioutil.TempDir("", "test")
	defer os.RemoveAll(dir)

	file := "newkey"
	fullFile := filepath.Join(dir, file)

	tests := []struct {
		name    string
		path    string
		pass    []byte
		wantErr bool
	}{
		{
			"bad path",
			"/totally/not/a/path",
			[]byte("pass1234"),
			true,
		},
		{
			"ok new",
			fullFile,
			[]byte("pass1234"),
			false,
		},
		{
			"ok exists",
			fullFile,
			[]byte("pass1234"),
			false,
		},
		{
			"wrong pass",
			fullFile,
			[]byte("adsf"),
			true,
		},
		{
			"empty pass for new",
			filepath.Join(dir, "newkey2"),
			[]byte{},
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := dexKey(tt.path, tt.pass)
			if (err != nil) != tt.wantErr {
				t.Errorf("dexKey() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
		})
	}
}
