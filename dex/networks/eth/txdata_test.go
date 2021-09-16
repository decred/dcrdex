package eth

import (
	"encoding/hex"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func mustParseHex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}

func TestParseInitiateData(t *testing.T) {
	participantAddr := common.HexToAddress("345853e21b1d475582E71cC269124eD5e2dD3422")
	secretHashSlice := mustParseHex("4aec4dc47fc6bd1fd5091c1aa4067c7fbd6bbcdb476209756354ec784d6082dc")
	var secretHash [32]byte
	copy(secretHash[:], secretHashSlice)
	locktime := int64(1632112916)
	calldata := mustParseHex("ae0521470000000000000000000000000000000000" +
		"0000000000000000000000614811144aec4dc47fc6bd1fd5091c1aa4067" +
		"c7fbd6bbcdb476209756354ec784d6082dc000000000000000000000000" +
		"345853e21b1d475582e71cc269124ed5e2dd3422")

	redeemCalldata := mustParseHex("b31597ad87eac09638c0c38b4e735b79f053" +
		"cb869167ee770640ac5df5c4ab030813122aebdc4c31b88d0c8f4d64459" +
		"1a8e00e92b607f920ad8050deb7c7469767d9c561")

	tests := []struct {
		name     string
		calldata []byte
		wantErr  bool
	}{{
		name:     "ok",
		calldata: calldata,
	}, {
		name:     "unable to parse call data",
		calldata: calldata[1:],
		wantErr:  true,
	}, {
		name:     "wrong function name",
		calldata: redeemCalldata,
		wantErr:  true,
	}}

	for _, test := range tests {
		addr, sh, lt, err := ParseInitiateData(test.calldata)
		if test.wantErr {
			if err == nil {
				t.Fatalf("expected error for test %q", test.name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("unexpected error for test %q: %v", test.name, err)
		}
		if *addr != participantAddr {
			t.Fatalf("want participant address %x but got %x for test %q", participantAddr, addr, test.name)
		}
		if sh != secretHash {
			t.Fatalf("want secret hash %x but got %x for test %q", secretHash, sh, test.name)
		}
		if lt != locktime {
			t.Fatalf("want locktime %d but got %d for test %q", locktime, lt, test.name)
		}
	}
}

func TestParseRedeemData(t *testing.T) {
	secretHashSlice := mustParseHex("ebdc4c31b88d0c8f4d644591a8e00e92b607f920ad8050deb7c7469767d9c561")
	secretSlice := mustParseHex("87eac09638c0c38b4e735b79f053cb869167ee770640ac5df5c4ab030813122a")
	secretHash, secret := [32]byte{}, [32]byte{}
	copy(secretHash[:], secretHashSlice)
	copy(secret[:], secretSlice)
	calldata := mustParseHex("b31597ad87eac09638c0c38b4e735b79f053cb8691" +
		"67ee770640ac5df5c4ab030813122aebdc4c31b88d0c8f4d644591a8e00" +
		"e92b607f920ad8050deb7c7469767d9c561")

	initiateCalldata := mustParseHex("ae05214700000000000000000000000000" +
		"000000000000000000000000000000614811144aec4dc47fc6bd1fd5091" +
		"c1aa4067c7fbd6bbcdb476209756354ec784d6082dc0000000000000000" +
		"00000000345853e21b1d475582e71cc269124ed5e2dd3422")

	tests := []struct {
		name     string
		calldata []byte
		wantErr  bool
	}{{
		name:     "ok",
		calldata: calldata,
	}, {
		name:     "unable to parse call data",
		calldata: calldata[1:],
		wantErr:  true,
	}, {
		name:     "wrong function name",
		calldata: initiateCalldata,
		wantErr:  true,
	}}

	for _, test := range tests {
		s, sh, err := ParseRedeemData(test.calldata)
		if test.wantErr {
			if err == nil {
				t.Fatalf("expected error for test %q", test.name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("unexpected error for test %q: %v", test.name, err)
		}
		if sh != secretHash {
			t.Fatalf("want secret hash %x but got %x for test %q", secretHash, sh, test.name)
		}
		if s != secret {
			t.Fatalf("want secret %x but got %x for test %q", secret, s, test.name)
		}
	}
}
