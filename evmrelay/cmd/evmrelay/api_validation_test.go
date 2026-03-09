// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package main

import (
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"

	dexeth "decred.org/dcrdex/dex/networks/eth"
	"decred.org/dcrdex/evmrelay"
	"github.com/ethereum/go-ethereum/common"
)

func TestValidateNumRedemptions(t *testing.T) {
	tests := []struct {
		name    string
		num     int
		wantErr string
	}{
		{name: "zero", num: 0, wantErr: "must be > 0"},
		{name: "negative", num: -1, wantErr: "must be > 0"},
		{name: "too large", num: evmrelay.MaxSignedRedeemBatch + 1, wantErr: "must be <="},
		{name: "max allowed", num: evmrelay.MaxSignedRedeemBatch},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateNumRedemptions(tt.num)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error: got %v, want substring %q", err, tt.wantErr)
			}
		})
	}
}

func TestValidateAllowedTarget(t *testing.T) {
	allowed := common.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")

	tests := []struct {
		name    string
		target  string
		wantErr string
	}{
		{name: "invalid hex address", target: "not-an-address", wantErr: "invalid target address"},
		{name: "disallowed address", target: "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", wantErr: "target address not allowed"},
		{name: "allowed address", target: allowed.Hex()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr, err := validateAllowedTarget(tt.target, map[common.Address]bool{allowed: true})
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if addr != allowed {
					t.Fatalf("address: got %s, want %s", addr.Hex(), allowed.Hex())
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error: got %v, want substring %q", err, tt.wantErr)
			}
		})
	}
}

func TestValidateSignedRedeemRequest(t *testing.T) {
	relayAddr := common.HexToAddress("0x3333333333333333333333333333333333333333")
	participant := common.HexToAddress("0xabcdef0123456789abcdef0123456789abcdef01")
	now := time.Now()

	tests := []struct {
		name         string
		feeRecipient common.Address
		nonce        uint64
		deadline     time.Time
		wantErr      string
		wantIs       error
	}{
		{
			name:         "zero fee recipient",
			feeRecipient: common.Address{},
			nonce:        1,
			deadline:     now.Add(time.Hour),
			wantErr:      "feeRecipient must match relay address",
		},
		{
			name:         "wrong fee recipient",
			feeRecipient: common.HexToAddress("0x2222222222222222222222222222222222222222"),
			nonce:        1,
			deadline:     now.Add(time.Hour),
			wantErr:      "does not match relay address",
		},
		{
			name:         "expired deadline",
			feeRecipient: relayAddr,
			nonce:        1,
			deadline:     now.Add(-time.Minute),
			wantErr:      "deadline expired",
			wantIs:       errDeadlineExpired,
		},
		{
			name:         "valid request",
			feeRecipient: relayAddr,
			nonce:        1,
			deadline:     now.Add(time.Hour),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calldata := tCalldataForRecipient(t, participant, tt.feeRecipient, big.NewInt(1e18), big.NewInt(1e15), tt.nonce, tt.deadline)
			parsed, err := dexeth.ParseSignedRedeemDataV1(calldata)
			if err != nil {
				t.Fatalf("parse calldata: %v", err)
			}
			if tt.wantErr == "" {
				if err := validateSignedRedeemRequest(parsed, relayAddr, now); err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			err = validateSignedRedeemRequest(parsed, relayAddr, now)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error: got %v, want substring %q", err, tt.wantErr)
			}
			if tt.wantIs != nil && !errors.Is(err, tt.wantIs) {
				t.Fatalf("error: got %v, want errors.Is(_, %v)", err, tt.wantIs)
			}
		})
	}
}

func TestIsPermanentPreflightError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "deadline expired",
			err: &deadlineExpiredError{
				Deadline: time.Unix(1, 0),
				Now:      time.Unix(2, 0),
			},
			want: true,
		},
		{
			name: "wrapped fee too low",
			err: fmt.Errorf("wrapped: %w", &feeTooLowError{
				Need: big.NewInt(2),
				Got:  big.NewInt(1),
			}),
			want: true,
		},
		{
			name: "wrapped fee exceeds redeemed value",
			err: fmt.Errorf("wrapped: %w", &feeExceedsRedeemedValueError{
				Fee:   big.NewInt(3),
				Total: big.NewInt(2),
			}),
			want: true,
		},
		{
			name: "wrapped gas estimate too high",
			err: fmt.Errorf("wrapped: %w", &gasEstimateTooHighError{
				GasEstimate: 101,
				ExpectedGas: 100,
			}),
			want: true,
		},
		{
			name: "other error",
			err:  errors.New("other"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isPermanentPreflightError(tt.err); got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidateEstimateFeeRequest(t *testing.T) {
	allowed := common.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")

	tests := []struct {
		name    string
		req     evmrelay.EstimateFeeRequest
		wantErr string
	}{
		{
			name:    "bad target",
			req:     evmrelay.EstimateFeeRequest{Target: "bad", NumRedemptions: 1},
			wantErr: "invalid target address",
		},
		{
			name:    "bad redemption count",
			req:     evmrelay.EstimateFeeRequest{Target: allowed.Hex(), NumRedemptions: 0},
			wantErr: "must be > 0",
		},
		{
			name:    "valid",
			req:     evmrelay.EstimateFeeRequest{Target: allowed.Hex(), NumRedemptions: 1},
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr, err := validateEstimateFeeRequest(&tt.req, map[common.Address]bool{allowed: true})
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if addr != allowed {
					t.Fatalf("address: got %s, want %s", addr.Hex(), allowed.Hex())
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error: got %v, want substring %q", err, tt.wantErr)
			}
		})
	}
}

func TestValidateRelayCalldata(t *testing.T) {
	relayAddr := common.HexToAddress("0x3333333333333333333333333333333333333333")
	participant := common.HexToAddress("0xabcdef0123456789abcdef0123456789abcdef01")
	now := time.Now()

	validCalldata := tCalldataForRecipient(t, participant, relayAddr, big.NewInt(1e18), big.NewInt(1e15), 1, now.Add(time.Hour))

	tests := []struct {
		name    string
		input   string
		wantErr string
	}{
		{name: "invalid hex", input: "zzzz", wantErr: "invalid calldata hex"},
		{name: "invalid calldata", input: hex.EncodeToString([]byte{0x01, 0x02}), wantErr: "invalid calldata"},
		{name: "valid calldata", input: hex.EncodeToString(validCalldata)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calldata, parsed, err := validateRelayCalldata(tt.input, relayAddr, now)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if len(calldata) == 0 || parsed == nil {
					t.Fatal("expected parsed calldata")
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error: got %v, want substring %q", err, tt.wantErr)
			}
		})
	}
}
