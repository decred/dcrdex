//go:build lgpl
// +build lgpl

package eth

import (
	"bytes"
	"encoding/hex"
	"math/big"
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

func initiationsAreEqual(a, b ETHSwapInitiation) bool {
	return a.RefundTimestamp.Cmp(b.RefundTimestamp) == 0 &&
		a.SecretHash == b.SecretHash &&
		a.Participant == b.Participant &&
		a.Value.Cmp(b.Value) == 0
}

func TestParseInitiateData(t *testing.T) {
	participantAddr := common.HexToAddress("345853e21b1d475582E71cC269124eD5e2dD3422")
	var secretHashA [32]byte
	var secretHashB [32]byte
	copy(secretHashA[:], mustParseHex("99d971975c09331eb00f5e0dc1eaeca9bf4ee2d086d3fe1de489f920007d6546"))
	copy(secretHashB[:], mustParseHex("2c0a304c9321402dc11cbb5898b9f2af3029ce1c76ec6702c4cd5bb965fd3e73"))

	locktime := int64(1632112916)

	initiations := []ETHSwapInitiation{
		ETHSwapInitiation{
			RefundTimestamp: big.NewInt(locktime),
			SecretHash:      secretHashA,
			Participant:     participantAddr,
			Value:           big.NewInt(1),
		},
		ETHSwapInitiation{
			RefundTimestamp: big.NewInt(locktime),
			SecretHash:      secretHashB,
			Participant:     participantAddr,
			Value:           big.NewInt(1),
		},
	}
	calldata, err := PackInitiateData(initiations)
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

	redeemCalldata := mustParseHex("f4fd17f9000000000000000000000000000000000" +
		"000000000000000000000000000002000000000000000000000000000000000000" +
		"0000000000000000000000000000287eac09638c0c38b4e735b79f053cb869167e" +
		"e770640ac5df5c4ab030813122aebdc4c31b88d0c8f4d644591a8e00e92b607f92" +
		"0ad8050deb7c7469767d9c5612c0a304c9321402dc11cbb5898b9f2af3029ce1c7" +
		"6ec6702c4cd5bb965fd3e7399d971975c09331eb00f5e0dc1eaeca9bf4ee2d086d" +
		"3fe1de489f920007d6546")

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

func redemptionsAreEqual(a, b ETHSwapRedemption) bool {
	return a.SecretHash == b.SecretHash &&
		a.Secret == b.Secret
}

func TestParseRedeemData(t *testing.T) {
	secretHashA, secretA, secretHashB, secretB := [32]byte{}, [32]byte{}, [32]byte{}, [32]byte{}
	copy(secretHashA[:], mustParseHex("ebdc4c31b88d0c8f4d644591a8e00e92b607f920ad8050deb7c7469767d9c561"))
	copy(secretA[:], mustParseHex("87eac09638c0c38b4e735b79f053cb869167ee770640ac5df5c4ab030813122a"))
	copy(secretHashB[:], mustParseHex("99d971975c09331eb00f5e0dc1eaeca9bf4ee2d086d3fe1de489f920007d6546"))
	copy(secretB[:], mustParseHex("2c0a304c9321402dc11cbb5898b9f2af3029ce1c76ec6702c4cd5bb965fd3e73"))

	redemptions := []ETHSwapRedemption{
		ETHSwapRedemption{
			Secret:     secretA,
			SecretHash: secretHashA,
		},
		ETHSwapRedemption{
			Secret:     secretB,
			SecretHash: secretHashB,
		},
	}
	calldata, err := PackRedeemData(redemptions)
	if err != nil {
		t.Fatalf("unale to pack abi: %v", err)
	}
	redeemCallData := mustParseHex("f4fd17f9000000000000000000000000000000000" +
		"000000000000000000000000000002000000000000000000000000000000000000" +
		"0000000000000000000000000000287eac09638c0c38b4e735b79f053cb869167e" +
		"e770640ac5df5c4ab030813122aebdc4c31b88d0c8f4d644591a8e00e92b607f92" +
		"0ad8050deb7c7469767d9c5612c0a304c9321402dc11cbb5898b9f2af3029ce1c7" +
		"6ec6702c4cd5bb965fd3e7399d971975c09331eb00f5e0dc1eaeca9bf4ee2d086d" +
		"3fe1de489f920007d6546")

	if !bytes.Equal(calldata, redeemCallData) {
		t.Fatalf("packed calldata is different than expected")
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
		parsedRedemptions, err := ParseRedeemData(test.calldata)
		if test.wantErr {
			if err == nil {
				t.Fatalf("expected error for test %q", test.name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("unexpected error for test %q: %v", test.name, err)
		}

		if len(redemptions) != len(parsedRedemptions) {
			t.Fatalf("expected %d redemptions but got %d", len(redemptions), len(parsedRedemptions))
		}

		for i := range redemptions {
			if !redemptionsAreEqual(redemptions[i], parsedRedemptions[i]) {
				t.Fatalf("expected redemptions to be equal. original: %v, parsed: %v",
					redemptions[i], parsedRedemptions[i])
			}
		}
	}
}

func TestParseRefundData(t *testing.T) {
	var secretHash [32]byte
	copy(secretHash[:], mustParseHex("ebdc4c31b88d0c8f4d644591a8e00e92b607f920ad8050deb7c7469767d9c561"))

	calldata, err := PackRefundData(secretHash)
	if err != nil {
		t.Fatalf("unale to pack abi: %v", err)
	}

	refundCallData := mustParseHex("7249fbb6ebdc4c31b88d0c8f4d644591a8e00e92b607f920ad8050deb7c7469767d9c561")

	if !bytes.Equal(calldata, refundCallData) {
		t.Fatalf("packed calldata is different than expected")
	}

	redeemCallData := mustParseHex("f4fd17f9000000000000000000000000000000000" +
		"000000000000000000000000000002000000000000000000000000000000000000" +
		"0000000000000000000000000000287eac09638c0c38b4e735b79f053cb869167e" +
		"e770640ac5df5c4ab030813122aebdc4c31b88d0c8f4d644591a8e00e92b607f92" +
		"0ad8050deb7c7469767d9c5612c0a304c9321402dc11cbb5898b9f2af3029ce1c7" +
		"6ec6702c4cd5bb965fd3e7399d971975c09331eb00f5e0dc1eaeca9bf4ee2d086d" +
		"3fe1de489f920007d6546")

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
		calldata: redeemCallData,
		wantErr:  true,
	}}

	for _, test := range tests {
		parsedSecretHash, err := ParseRefundData(test.calldata)
		if test.wantErr {
			if err == nil {
				t.Fatalf("expected error for test %q", test.name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("unexpected error for test %q: %v", test.name, err)
		}

		if secretHash != parsedSecretHash {
			t.Fatalf("expected secretHash %x to equal parsed secret hash %x",
				secretHash, parsedSecretHash)
		}
	}
}
