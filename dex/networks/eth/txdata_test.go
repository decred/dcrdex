package eth

import (
	"bytes"
	"encoding/hex"
	"math/big"
	"testing"
	"time"

	swapv0 "decred.org/dcrdex/dex/networks/eth/contracts/v0"
	"github.com/ethereum/go-ethereum/common"
)

func packInitiateDataV0(initiations []*Initiation) ([]byte, error) {
	abiInitiations := make([]swapv0.ETHSwapInitiation, 0, len(initiations))
	for _, init := range initiations {
		abiInitiations = append(abiInitiations, swapv0.ETHSwapInitiation{
			RefundTimestamp: big.NewInt(init.LockTime.Unix()),
			SecretHash:      init.SecretHash,
			Participant:     init.Participant,
			Value:           init.Value,
		})
	}
	return (*ABIs[0]).Pack("initiate", abiInitiations)
}

func packRedeemDataV0(redemptions []*Redemption) ([]byte, error) {
	abiRedemptions := make([]swapv0.ETHSwapRedemption, 0, len(redemptions))
	for _, redeem := range redemptions {
		abiRedemptions = append(abiRedemptions, swapv0.ETHSwapRedemption{
			Secret:     redeem.Secret,
			SecretHash: redeem.SecretHash,
		})
	}
	return (*ABIs[0]).Pack("redeem", abiRedemptions)
}

func packRefundDataV0(secretHash [32]byte) ([]byte, error) {
	return (*ABIs[0]).Pack("refund", secretHash)
}

func mustParseHex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}

func initiationsAreEqual(a, b *Initiation) bool {
	return a.LockTime == b.LockTime &&
		a.SecretHash == b.SecretHash &&
		a.Participant == b.Participant &&
		a.Value.Cmp(b.Value) == 0
}

func TestParseInitiateDataV0(t *testing.T) {
	participantAddr := common.HexToAddress("345853e21b1d475582E71cC269124eD5e2dD3422")
	var secretHashA [32]byte
	var secretHashB [32]byte
	copy(secretHashA[:], mustParseHex("99d971975c09331eb00f5e0dc1eaeca9bf4ee2d086d3fe1de489f920007d6546"))
	copy(secretHashB[:], mustParseHex("2c0a304c9321402dc11cbb5898b9f2af3029ce1c76ec6702c4cd5bb965fd3e73"))

	locktime := int64(1632112916)

	initiations := []*Initiation{
		{
			LockTime:    time.Unix(locktime, 0),
			SecretHash:  secretHashA,
			Participant: participantAddr,
			Value:       GweiToWei(1),
		},
		{
			LockTime:    time.Unix(locktime, 0),
			SecretHash:  secretHashB,
			Participant: participantAddr,
			Value:       GweiToWei(1),
		},
	}
	calldata, err := packInitiateDataV0(initiations)
	if err != nil {
		t.Fatalf("failed to pack abi: %v", err)
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
		parsedInitiations, err := ParseInitiateDataV0(test.calldata)
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

		for _, init := range initiations {
			if !initiationsAreEqual(parsedInitiations[init.SecretHash], init) {
				t.Fatalf("expected initiations to be equal. original: %v, parsed: %v",
					init, parsedInitiations[init.SecretHash])
			}
		}
	}
}

func redemptionsAreEqual(a, b *Redemption) bool {
	return a.SecretHash == b.SecretHash &&
		a.Secret == b.Secret
}

func TestParseRedeemDataV0(t *testing.T) {
	secretHashA, secretA, secretHashB, secretB := [32]byte{}, [32]byte{}, [32]byte{}, [32]byte{}
	copy(secretHashA[:], mustParseHex("ebdc4c31b88d0c8f4d644591a8e00e92b607f920ad8050deb7c7469767d9c561"))
	copy(secretA[:], mustParseHex("87eac09638c0c38b4e735b79f053cb869167ee770640ac5df5c4ab030813122a"))
	copy(secretHashB[:], mustParseHex("99d971975c09331eb00f5e0dc1eaeca9bf4ee2d086d3fe1de489f920007d6546"))
	copy(secretB[:], mustParseHex("2c0a304c9321402dc11cbb5898b9f2af3029ce1c76ec6702c4cd5bb965fd3e73"))

	redemptions := []*Redemption{
		{
			Secret:     secretA,
			SecretHash: secretHashA,
		},
		{
			Secret:     secretB,
			SecretHash: secretHashB,
		},
	}
	calldata, err := packRedeemDataV0(redemptions)
	if err != nil {
		t.Fatalf("failed to pack abi: %v", err)
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
		parsedRedemptions, err := ParseRedeemDataV0(test.calldata)
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

		for _, redemption := range redemptions {
			if !redemptionsAreEqual(redemption, parsedRedemptions[redemption.SecretHash]) {
				t.Fatalf("expected redemptions to be equal. original: %v, parsed: %v",
					redemption, parsedRedemptions[redemption.SecretHash])
			}
		}
	}
}

func TestParseRefundDataV0(t *testing.T) {
	var secretHash [32]byte
	copy(secretHash[:], mustParseHex("ebdc4c31b88d0c8f4d644591a8e00e92b607f920ad8050deb7c7469767d9c561"))

	calldata, err := packRefundDataV0(secretHash)
	if err != nil {
		t.Fatalf("failed to pack abi: %v", err)
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
		parsedSecretHash, err := ParseRefundDataV0(test.calldata)
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

func TestParseHandleOps(t *testing.T) {
	calldata := "1fad948c000000000000000000000000000000000000000000000000000000000000004000000000000000000000000005736be876755de230e809784def1937dcb6303e00000000000000000000000000000000000000000000000000000000000000010000000000000000000000000000000000000000000000000000000000000020000000000000000000000000af5e6d6fd9011a8256a74b22791eb7865d0720efd157ae1594bda41e7e2c99f878555b6841ff1f4a000000000000000000000010000000000000000000000000000000000000000000000000000000000000016000000000000000000000000000000000000000000000000000000000000001800000000000000000000000000000000000000000000000000000000000005bb8000000000000000000000000000000000000000000000000000000000000f67d000000000000000000000000000000000000000000000000000000000000b46c000000000000000000000000000000000000000000000000000000013ade0a8e000000000000000000000000000000000000000000000000000000007735940000000000000000000000000000000000000000000000000000000000000002c000000000000000000000000000000000000000000000000000000000000002e000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000104919835a400000000000000000000000000000000000000000000000000000000000000200000000000000000000000000000000000000000000000000000000000000001461e9116f88c220de9b4cb59e1dcdd29d201634310d58251586fae8b40c68d9a0000000000000000000000000000000000000000000000000214e8348c4f0000000000000000000000000000ce4ad4154b9914f3f641019e894060ae7b9684e30000000000000000000000000000000000000000000000000000000067997a66000000000000000000000000d157ae1594bda41e7e2c99f878555b6841ff1f4ac9f2956110b2ce62f9adfb543ea043961b62173f9f8e2b9734ea5353436fe5dd0000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000041fffffffffffffffffffffffffffffff0000000000000000000000000000000007aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa1c00000000000000000000000000000000000000000000000000000000000000"
	calldataB, err := hex.DecodeString(calldata)
	if err != nil {
		t.Fatalf("failed to decode calldata: %v", err)
	}

	userOps, err := ParseHandleOpsData(calldataB)
	if err != nil {
		t.Fatalf("failed to parse handle ops data: %v", err)
	}

	hash, err := HashUserOp(userOps[0], common.HexToAddress("0x5FF137D4b0FDCD49DcA30c7CF57E578a026d2789"), big.NewInt(11155111))
	if err != nil {
		t.Fatalf("failed to hash user op: %v", err)
	}

	if hash != common.HexToHash("0xff642622a4fba14cebc2266b4c3431a88f5a2ae355d7dfe60759e60811c13e2e") {
		t.Fatalf("unexpected hash: %s", hash.Hex())
	}
}
