//go:build lgpl
// +build lgpl

package eth

import (
	"bytes"
	"encoding/hex"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

func mustParseHex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}

func initiationsAreEqual(i1, i2 ETHSwapInitiation) bool {
	return i1.RefundTimestamp.Cmp(i2.RefundTimestamp) == 0 &&
		bytes.Equal(i1.SecretHash[:], i2.SecretHash[:]) &&
		bytes.Equal(i1.Participant.Bytes(), i2.Participant.Bytes()) &&
		i1.Value.Cmp(i2.Value) == 0
}

func TestParseInitiateData(t *testing.T) {
	participantAddr := common.HexToAddress("345853e21b1d475582E71cC269124eD5e2dD3422")
	var secretHash [32]byte
	var secretHash2 [32]byte
	copy(secretHash[:], mustParseHex("99d971975c09331eb00f5e0dc1eaeca9bf4ee2d086d3fe1de489f920007d6546"))
	copy(secretHash2[:], mustParseHex("2c0a304c9321402dc11cbb5898b9f2af3029ce1c76ec6702c4cd5bb965fd3e73"))

	locktime := int64(1632112916)

	initiations := []ETHSwapInitiation{
		ETHSwapInitiation{
			RefundTimestamp: big.NewInt(locktime),
			SecretHash:      secretHash,
			Participant:     participantAddr,
			Value:           big.NewInt(1),
		},
		ETHSwapInitiation{
			RefundTimestamp: big.NewInt(locktime),
			SecretHash:      secretHash2,
			Participant:     participantAddr,
			Value:           big.NewInt(1),
		},
	}
	parsedABI, err := abi.JSON(strings.NewReader(ETHSwapABI))
	if err != nil {
		t.Fatalf("unable to parse abi: %v", err)
	}
	calldata, err := parsedABI.Pack("initiate", initiations)
	if err != nil {
		t.Fatalf("unale to pack abi: %v", err)
	}
	initiateCalldata := mustParseHex("a8793f94000000000000000000000" +
		"0000000000000000000000000000000000000000020000000000000000" +
		"0000000000000000000000000000000000000000000000002000000000" +
		"000000000000000000000000000000000000000000000006148111499d" +
		"971975c09331eb00f5e0dc1eaeca9bf4ee2d086d3fe1de489f920007d6" +
		"546000000000000000000000000345853e21b1d475582e71cc269124ed" +
		"5e2dd34220000000000000000000000000000000000000000000000000" +
		"0000000000000010000000000000000000000000000000000000000000" +
		"0000000000000614811142c0a304c9321402dc11cbb5898b9f2af3029c" +
		"e1c76ec6702c4cd5bb965fd3e73000000000000000000000000345853e" +
		"21b1d475582e71cc269124ed5e2dd34220000000000000000000000000" +
		"000000000000000000000000000000000000001")

	if !bytes.Equal(calldata, initiateCalldata) {
		t.Fatalf("packed calldata is different than expected")
	}

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
		parsedInitiations, err := ParseInitiateData(test.calldata)
		if test.wantErr {
			if err == nil {
				t.Fatalf("expected error for test %q", test.name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("unexpected error for test %q: %v", test.name, err)
		}

		if len(parsedInitiations) != len(initiations) {
			t.Fatalf("expected %d initiations but got %d", len(initiations), len(parsedInitiations))
		}

		for i := range initiations {
			if !initiationsAreEqual(parsedInitiations[i], initiations[i]) {
				t.Fatalf("expected initiations to be equal. original: %v, parsed: %v",
					initiations[i], parsedInitiations[i])
			}
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
		"000000008d83B207674bfd53B418a6E47DA148F5bFeCc652")

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
