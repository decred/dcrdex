//go:build lgpl
// +build lgpl

package eth

import (
	"bytes"
	"encoding/hex"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

func mustParseHex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}

func initiationsAreEqual(a, b Initiation) bool {
	return a.LockTime == b.LockTime &&
		a.SecretHash == b.SecretHash &&
		a.Participant == b.Participant &&
		a.Value == b.Value
}

func TestParseInitiateDataV0(t *testing.T) {
	participantAddr := common.HexToAddress("345853e21b1d475582E71cC269124eD5e2dD3422")
	var secretHashA [32]byte
	var secretHashB [32]byte
	copy(secretHashA[:], mustParseHex("99d971975c09331eb00f5e0dc1eaeca9bf4ee2d086d3fe1de489f920007d6546"))
	copy(secretHashB[:], mustParseHex("2c0a304c9321402dc11cbb5898b9f2af3029ce1c76ec6702c4cd5bb965fd3e73"))

	locktime := int64(1632112916)

	initiations := []Initiation{
		Initiation{
			LockTime:    time.Unix(locktime, 0),
			SecretHash:  secretHashA,
			Participant: participantAddr,
			Value:       1,
		},
		Initiation{
			LockTime:    time.Unix(locktime, 0),
			SecretHash:  secretHashB,
			Participant: participantAddr,
			Value:       1,
		},
	}
	calldata, err := PackInitiateData(initiations, 0)
	if err != nil {
		t.Fatalf("unale to pack abi: %v", err)
	}
	initiateCalldata := mustParseHex("a8793f940000000000000000000000" +
		"00000000000000000000000000000000000000002000000000000000000" +
		"00000000000000000000000000000000000000000000002000000000000" +
		"000000000000000000000000000000000000000000006148111499d9719" +
		"75c09331eb00f5e0dc1eaeca9bf4ee2d086d3fe1de489f920007d654600" +
		"0000000000000000000000345853e21b1d475582e71cc269124ed5e2dd3" +
		"42200000000000000000000000000000000000000000000000000000000" +
		"3b9aca00000000000000000000000000000000000000000000000000000" +
		"00000614811142c0a304c9321402dc11cbb5898b9f2af3029ce1c76ec67" +
		"02c4cd5bb965fd3e73000000000000000000000000345853e21b1d47558" +
		"2e71cc269124ed5e2dd3422000000000000000000000000000000000000" +
		"000000000000000000003b9aca00")

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
		parsedInitiations, err := ParseInitiateData(test.calldata, 0)
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

func redemptionsAreEqual(a, b Redemption) bool {
	return a.SecretHash == b.SecretHash &&
		a.Secret == b.Secret
}

func TestParseRedeemDataV0(t *testing.T) {
	secretHashA, secretA, secretHashB, secretB := [32]byte{}, [32]byte{}, [32]byte{}, [32]byte{}
	copy(secretHashA[:], mustParseHex("ebdc4c31b88d0c8f4d644591a8e00e92b607f920ad8050deb7c7469767d9c561"))
	copy(secretA[:], mustParseHex("87eac09638c0c38b4e735b79f053cb869167ee770640ac5df5c4ab030813122a"))
	copy(secretHashB[:], mustParseHex("99d971975c09331eb00f5e0dc1eaeca9bf4ee2d086d3fe1de489f920007d6546"))
	copy(secretB[:], mustParseHex("2c0a304c9321402dc11cbb5898b9f2af3029ce1c76ec6702c4cd5bb965fd3e73"))

	redemptions := []Redemption{
		Redemption{
			Secret:     secretA,
			SecretHash: secretHashA,
		},
		Redemption{
			Secret:     secretB,
			SecretHash: secretHashB,
		},
	}
	calldata, err := PackRedeemData(redemptions, 0)
	if err != nil {
		t.Fatalf("unable to pack abi: %v", err)
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
		parsedRedemptions, err := ParseRedeemData(test.calldata, 0)
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

func TestParseRefundDataV0(t *testing.T) {
	var secretHash [32]byte
	copy(secretHash[:], mustParseHex("ebdc4c31b88d0c8f4d644591a8e00e92b607f920ad8050deb7c7469767d9c561"))

	calldata, err := PackRefundData(secretHash, 0)
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
		parsedSecretHash, err := ParseRefundData(test.calldata, 0)
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
