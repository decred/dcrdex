//go:build harness
// +build harness

package livetest

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"testing"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/asset/btc"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/encode"
	dexbtc "decred.org/dcrdex/dex/networks/btc"
)

const (
	alphaAddress  = "bcrt1qaujcvxuvp9vdcqaa6s3acyh8kxmuyqnyg4jcfl"
	betaAddress   = "bcrt1qwhxklx3vms6xc0lxlunez93m9wn8qzxkkn5dy2"
	gammaAddress  = "bcrt1qll362edf4levwg7yqyt7kawjklvejvj74w87py"
	walletTypeSPV = "SPV"
)

var (
	tBTC = &dex.Asset{
		ID:           0,
		Symbol:       "btc",
		Version:      0, // match btc.version
		SwapSize:     dexbtc.InitTxSizeSegwit,
		SwapSizeBase: dexbtc.InitTxSizeBaseSegwit,
		MaxFeeRate:   10,
		SwapConf:     1,
	}

	usr, _        = user.Current()
	harnessCtlDir = filepath.Join(usr.HomeDir, "dextest", "btc", "harness-ctl")
	tPW           = []byte("abc")
)

func TestWallet(t *testing.T) {
	const lotSize = 1e6

	fmt.Println("////////// RPC WALLET W/O SPLIT //////////")
	Run(t, &Config{
		NewWallet: btc.NewWallet,
		LotSize:   lotSize,
		Asset:     tBTC,
	})

	fmt.Println("////////// RPC WALLET WITH SPLIT //////////")
	Run(t, &Config{
		NewWallet: btc.NewWallet,
		LotSize:   lotSize,
		Asset:     tBTC,
		SplitTx:   true,
	})

	spvDir, err := os.MkdirTemp("", "")
	if err != nil {
		t.Fatalf("MkdirTemp error: %v", err)
	}
	defer os.RemoveAll(spvDir) // clean up

	createWallet := func(cfg *asset.WalletConfig, name string, logger dex.Logger) error {
		// var seed [32]byte
		// copy(seed[:], []byte(name))
		seed := encode.RandomBytes(32)

		err = (&btc.Driver{}).Create(&asset.CreateWalletParams{
			Type:    walletTypeSPV,
			Seed:    seed[:],
			Pass:    tPW, // match walletPassword in livetest.go -> Run
			DataDir: cfg.DataDir,
			Net:     dex.Simnet,
			Logger:  logger,
		})
		if err != nil {
			return fmt.Errorf("error creating SPV wallet: %w", err)
		}

		w, err := btc.NewWallet(cfg, logger, dex.Regtest)
		if err != nil {
			t.Fatalf("error creating backend: %v", err)
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		cm := dex.NewConnectionMaster(w)
		err = cm.Connect(ctx)
		if err != nil {
			t.Fatalf("error connecting backend: %v", err)
		}
		defer cm.Disconnect()

		addr, err := w.Address()
		if err != nil {
			return fmt.Errorf("Address error: %w", err)
		}

		// TODO: Randomize the address scope passed to
		// btcwallet.Wallet.{NewAddres, NewChangeAddress} between
		// waddrmgr.KeyScopeBIP0084 and waddrmgr.KeyScopeBIP0044 so that we
		// know we can handle non-segwit previous outpoints too.
		if err := loadAddress(addr); err != nil {
			return fmt.Errorf("loadAddress error: %v", err)
		}

		time.Sleep(time.Second * 3)

		for {
			synced, progress, err := w.SyncStatus()
			if err != nil {
				return fmt.Errorf("SyncStatus error: %w", err)
			}
			if synced {
				break
			}
			fmt.Printf("%s sync progress %.1f \n", name, progress*100)
			time.Sleep(time.Second)
		}

		bal, _ := w.Balance()
		logger.Infof("%s with address %s is synced with balance = %+v \n", name, addr, bal)

		return nil
	}

	spvConstructor := func(cfg *asset.WalletConfig, logger dex.Logger, network dex.Network) (asset.Wallet, error) {
		token := hex.EncodeToString(encode.RandomBytes(4))
		cfg.Type = walletTypeSPV
		cfg.DataDir = filepath.Join(spvDir, token)

		name := parseName(cfg.Settings)

		// regtest connects to alpha node by default if "peer" isn't set.
		if name == "beta" {
			cfg.Settings["peer"] = "localhost:20576" // beta node
		}

		err := createWallet(cfg, name, logger)
		if err != nil {
			return nil, fmt.Errorf("createWallet error: %v", err)
		}

		w, err := btc.NewWallet(cfg, logger, network)
		if err != nil {
			return nil, err
		}

		return w, nil
	}

	fmt.Println("////////// SPV WALLET W/O SPLIT //////////")
	Run(t, &Config{
		NewWallet: spvConstructor,
		LotSize:   lotSize,
		Asset:     tBTC,
		SPV:       true,
	})

	fmt.Println("////////// SPV WALLET WITH SPLIT //////////")
	Run(t, &Config{
		NewWallet: spvConstructor,
		LotSize:   lotSize,
		Asset:     tBTC,
		SPV:       true,
		SplitTx:   true,
	})
}

func loadAddress(addr string) error {
	for _, v := range []string{"10", "18", "5", "7", "1", "15", "3", "25"} {
		if err := runCmd("./alpha", "sendtoaddress", addr, v); err != nil {
			return err
		}
	}
	return runCmd("./mine-alpha", "1")
}

func runCmdWithOutput(exe string, args ...string) (string, error) {
	cmd := exec.Command(exe, args...)
	cmd.Dir = harnessCtlDir
	b, err := cmd.Output()
	// if len(op) > 0 {
	// 	fmt.Printf("output from command %q: %s \n", cmd, string(op))
	// }
	return string(b), err
}

func runCmd(exe string, args ...string) error {
	_, err := runCmdWithOutput(exe, args...)
	return err
}

func parseName(settings map[string]string) string {
	name := settings["walletname"]
	if name == "" {
		name = filepath.Base(settings["datadir"])
	}
	return name
}
