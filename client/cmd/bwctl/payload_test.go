// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package main

import (
	"testing"

	"decred.org/dcrdex/client/rpcserver"
	"decred.org/dcrdex/dex/encode"
)

func TestBuildPayloadNoParams(t *testing.T) {
	for _, route := range []string{"version", "wallets", "logout", "exchanges", "mmstatus"} {
		p, err := buildPayload(route, nil, nil)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", route, err)
		}
		if p != nil {
			t.Fatalf("%s: expected nil payload", route)
		}
	}
}

func TestBuildPayloadUnknownRoute(t *testing.T) {
	_, err := buildPayload("nonexistent", nil, nil)
	if err == nil {
		t.Fatal("expected error for unknown route")
	}
}

func TestBuildHelp(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{name: "no args", args: nil},
		{name: "with helpWith", args: []string{"version"}},
		{name: "with includePasswords", args: []string{"version", "true"}},
		{name: "bad bool", args: []string{"version", "notabool"}, wantErr: true},
	}
	for _, tt := range tests {
		p, err := buildHelp(tt.args)
		if (err != nil) != tt.wantErr {
			t.Fatalf("%s: err = %v, wantErr = %v", tt.name, err, tt.wantErr)
		}
		if err == nil && p == nil {
			t.Fatalf("%s: expected non-nil result", tt.name)
		}
	}
}

func TestBuildInit(t *testing.T) {
	pw := encode.PassBytes("password")
	tests := []struct {
		name    string
		pws     []encode.PassBytes
		args    []string
		wantErr bool
	}{
		{name: "ok", pws: []encode.PassBytes{pw}},
		{name: "ok with seed", pws: []encode.PassBytes{pw}, args: []string{"myseed"}},
		{name: "missing password", pws: nil, wantErr: true},
	}
	for _, tt := range tests {
		p, err := buildInit(tt.pws, tt.args)
		if (err != nil) != tt.wantErr {
			t.Fatalf("%s: err = %v, wantErr = %v", tt.name, err, tt.wantErr)
		}
		if err == nil && p == nil {
			t.Fatalf("%s: expected non-nil result", tt.name)
		}
	}
}

func TestBuildLogin(t *testing.T) {
	pw := encode.PassBytes("password")
	if _, err := buildLogin(nil); err == nil {
		t.Fatal("expected error for missing password")
	}
	p, err := buildLogin([]encode.PassBytes{pw})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestBuildCloseWallet(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{name: "ok", args: []string{"42"}},
		{name: "missing assetID", args: nil, wantErr: true},
		{name: "bad assetID", args: []string{"abc"}, wantErr: true},
	}
	for _, tt := range tests {
		p, err := buildCloseWallet(tt.args)
		if (err != nil) != tt.wantErr {
			t.Fatalf("%s: err = %v, wantErr = %v", tt.name, err, tt.wantErr)
		}
		if err == nil {
			if p.AssetID != 42 {
				t.Fatalf("%s: expected assetID 42, got %d", tt.name, p.AssetID)
			}
		}
	}
}

func TestBuildWalletBalance(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{name: "ok", args: []string{"42"}},
		{name: "missing assetID", args: nil, wantErr: true},
		{name: "bad assetID", args: []string{"abc"}, wantErr: true},
	}
	for _, tt := range tests {
		p, err := buildWalletBalance(tt.args)
		if (err != nil) != tt.wantErr {
			t.Fatalf("%s: err = %v, wantErr = %v", tt.name, err, tt.wantErr)
		}
		if err == nil {
			if p.AssetID != 42 {
				t.Fatalf("%s: expected assetID 42, got %d", tt.name, p.AssetID)
			}
		}
	}
}

func TestBuildWalletState(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{name: "ok", args: []string{"42"}},
		{name: "missing assetID", args: nil, wantErr: true},
		{name: "bad assetID", args: []string{"abc"}, wantErr: true},
	}
	for _, tt := range tests {
		p, err := buildWalletState(tt.args)
		if (err != nil) != tt.wantErr {
			t.Fatalf("%s: err = %v, wantErr = %v", tt.name, err, tt.wantErr)
		}
		if err == nil {
			if p.AssetID != 42 {
				t.Fatalf("%s: expected assetID 42, got %d", tt.name, p.AssetID)
			}
		}
	}
}

func TestBuildToggleWalletStatus(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{name: "ok disable", args: []string{"42", "true"}},
		{name: "ok enable", args: []string{"42", "false"}},
		{name: "missing args", args: []string{"42"}, wantErr: true},
		{name: "bad assetID", args: []string{"abc", "true"}, wantErr: true},
		{name: "bad disable", args: []string{"42", "notabool"}, wantErr: true},
	}
	for _, tt := range tests {
		_, err := buildToggleWalletStatus(tt.args)
		if (err != nil) != tt.wantErr {
			t.Fatalf("%s: err = %v, wantErr = %v", tt.name, err, tt.wantErr)
		}
	}
}

func TestBuildTrade(t *testing.T) {
	pw := encode.PassBytes("password")
	goodArgs := []string{"localhost:7232", "true", "true", "42", "0", "1000000", "5000000", "false"}
	tests := []struct {
		name    string
		pws     []encode.PassBytes
		args    []string
		wantErr bool
	}{
		{name: "ok", pws: []encode.PassBytes{pw}, args: goodArgs},
		{name: "missing password", pws: nil, args: goodArgs, wantErr: true},
		{name: "too few args", pws: []encode.PassBytes{pw}, args: []string{"host"}, wantErr: true},
		{name: "bad isLimit", pws: []encode.PassBytes{pw}, args: []string{"host", "bad", "true", "42", "0", "1000", "5000", "false"}, wantErr: true},
	}
	for _, tt := range tests {
		_, err := buildTrade(tt.pws, tt.args)
		if (err != nil) != tt.wantErr {
			t.Fatalf("%s: err = %v, wantErr = %v", tt.name, err, tt.wantErr)
		}
	}
}

func TestBuildCancel(t *testing.T) {
	p, err := buildCancel([]string{"abc123"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.OrderID != "abc123" {
		t.Fatalf("expected orderID abc123, got %s", p.OrderID)
	}
	if _, err := buildCancel(nil); err == nil {
		t.Fatal("expected error for missing orderID")
	}
}

func TestBuildSend(t *testing.T) {
	pw := encode.PassBytes("password")
	tests := []struct {
		name    string
		pws     []encode.PassBytes
		args    []string
		wantErr bool
	}{
		{name: "ok", pws: []encode.PassBytes{pw}, args: []string{"42", "1000000", "DsAddr"}},
		{name: "missing password", pws: nil, args: []string{"42", "1000000", "DsAddr"}, wantErr: true},
		{name: "too few args", pws: []encode.PassBytes{pw}, args: []string{"42"}, wantErr: true},
		{name: "bad assetID", pws: []encode.PassBytes{pw}, args: []string{"abc", "1000000", "DsAddr"}, wantErr: true},
		{name: "bad value", pws: []encode.PassBytes{pw}, args: []string{"42", "abc", "DsAddr"}, wantErr: true},
	}
	for _, tt := range tests {
		_, err := buildSend(tt.pws, tt.args)
		if (err != nil) != tt.wantErr {
			t.Fatalf("%s: err = %v, wantErr = %v", tt.name, err, tt.wantErr)
		}
	}
}

func TestBuildPayloadIntegration(t *testing.T) {
	// Test that buildPayload dispatches correctly for a selection of routes.
	pw := encode.PassBytes("password")

	// help
	p, err := buildPayload("help", nil, []string{"version"})
	if err != nil {
		t.Fatalf("help: %v", err)
	}
	if hp, ok := p.(*rpcserver.HelpParams); !ok || hp.HelpWith != "version" {
		t.Fatal("help: wrong result")
	}

	// login
	p, err = buildPayload("login", []encode.PassBytes{pw}, nil)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if _, ok := p.(*rpcserver.LoginParams); !ok {
		t.Fatal("login: wrong type")
	}

	// closewallet
	p, err = buildPayload("closewallet", nil, []string{"42"})
	if err != nil {
		t.Fatalf("closewallet: %v", err)
	}
	if cp, ok := p.(*rpcserver.CloseWalletParams); !ok || cp.AssetID != 42 {
		t.Fatal("closewallet: wrong result")
	}

	// walletbalance
	p, err = buildPayload("walletbalance", nil, []string{"42"})
	if err != nil {
		t.Fatalf("walletbalance: %v", err)
	}
	if bp, ok := p.(*rpcserver.WalletBalanceParams); !ok || bp.AssetID != 42 {
		t.Fatal("walletbalance: wrong result")
	}

	// walletstate
	p, err = buildPayload("walletstate", nil, []string{"42"})
	if err != nil {
		t.Fatalf("walletstate: %v", err)
	}
	if sp, ok := p.(*rpcserver.WalletStateParams); !ok || sp.AssetID != 42 {
		t.Fatal("walletstate: wrong result")
	}
}
