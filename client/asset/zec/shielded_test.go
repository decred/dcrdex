//go:build harness

package zec

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	dexzec "decred.org/dcrdex/dex/networks/zec"
)

var (
	dextestDir  = filepath.Join(os.Getenv("HOME"), "dextest")
	harnessDir  = filepath.Join(dextestDir, "zec", "harness-ctl")
	ctx         = context.Background()
	log         = dex.StdOutLogger("ZT", dex.LevelTrace)
	tBtcParams  = dexzec.RegressionNetParams
	tAddrParams = dexzec.RegressionNetAddressParams
)

func harnessCmd(ctx context.Context, exe string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.Dir = harnessDir
	op, err := cmd.CombinedOutput()
	return string(op), err
}

func getDeltaWallet(t *testing.T) (*zecWallet, func()) {
	wi, err := NewWallet(&asset.WalletConfig{
		Type: walletTypeRPC,
		Settings: map[string]string{
			"rpcuser":     "user",
			"rpcpassword": "pass",
			"rpcport":     "33770",
		},
		PeersChange: func(uint32, error) {},
		TipChange:   func(err error) {},
	}, log, dex.Simnet)
	if err != nil {
		t.Fatal(err)
	}

	w := wi.(*zecWallet)

	ctx, cancel := context.WithCancel(ctx)

	_, err = w.Connect(ctx)
	if err != nil {
		t.Fatal(err)
	}

	return w, cancel
}

func TestTransparentAddress(t *testing.T) {
	w, cancel := getDeltaWallet(t)
	defer cancel()
	_, err := transparentAddress(w, tAddrParams, tBtcParams)
	if err != nil {
		t.Fatalf("Error getting transparent address: %v", err)
	}
}

func TestShieldUnshield(t *testing.T) {
	w, cancel := getDeltaWallet(t)
	defer cancel()

	_, err := w.ShieldFunds(ctx, 1e8)
	if err != nil {
		t.Fatalf("ShieldFunds error: %v", err)
	}

	time.Sleep(time.Second)
	harnessCmd(ctx, "./mine-alpha 1")
	time.Sleep(4 * time.Second)

	status, err := w.ShieldedStatus()
	if err != nil {
		t.Fatalf("Error getting shielded balance: %v", err)
	}

	bal, err := w.Balance()
	if err != nil {
		t.Fatalf("Balance error: %v", err)
	}

	if bal.Other["shielded"] != status.Balance {
		t.Fatalf("Account balance does not match sum of Orchard address balances. %d != %d", bal.Other["shielded"], status.Balance)
	}

	_, err = w.UnshieldFunds(ctx, 5e7)
	if err != nil {
		t.Fatalf("UnshieldFunds error: %v", err)
	}
}

func TestSendShielded(t *testing.T) {
	w, cancel := getDeltaWallet(t)
	defer cancel()

	_, err := w.ShieldFunds(ctx, 1e8)
	if err != nil {
		t.Fatalf("ShieldFunds error: %v", err)
	}

	harnessCmd(ctx, "./mine-alpha 1")
	time.Sleep(4 * time.Second)

	orchardAddr, err := w.NewShieldedAddress()
	if err != nil {
		t.Fatalf("NewShieldedAddress error: %v", err)
	}

	_, err = w.SendShielded(ctx, orchardAddr, 5e7)
	if err != nil {
		t.Fatalf("SendShielded error: %v", err)
	}

	tAddr, err := w.NewAddress()
	if err != nil {
		t.Fatalf("NewAddress error: %v", err)
	}

	_, err = w.SendShielded(ctx, tAddr, 1e7)
	if err != nil {
		t.Fatalf("SendShielded error: %v", err)
	}
}
