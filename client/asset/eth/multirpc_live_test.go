//go:build rpclive

package eth

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/encode"
	dexeth "decred.org/dcrdex/dex/networks/eth"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

const (
	alphaHTTPPort = "38556"
	alphaWSPort   = "38557"
)

var mt *MRPCTest

var (
	ctx              = context.Background()
	homeDir          = os.Getenv("HOME")
	simnetWalletDir  = filepath.Join(homeDir, "dextest", "eth", "client_rpc_tests", "simnet")
	simnetWalletSeed = "0812f5244004217452059e2fd11603a511b5d0870ead753df76c966ce3c71531"
	pw               = "bee75192465cef9f8ab1198093dfed594e93ae810ccbbf2b3b12e1771cc6cb19"
)

func TestMain(m *testing.M) {
	ctx, shutdown := context.WithCancel(context.Background())
	mt = NewMRPCTest(ctx, ChainConfig, NetworkCompatibilityData, "eth")
	doIt := func() int {
		defer shutdown()
		return m.Run()
	}
	os.Exit(doIt())
}

func TestHTTP(t *testing.T) {
	mt.TestHTTP(t, alphaHTTPPort)
}

func TestWS(t *testing.T) {
	mt.TestWS(t, alphaWSPort)
}

func TestWSTxLogs(t *testing.T) {
	mt.TestWSTxLogs(t, alphaWSPort)
}

func TestSimnetMultiRPCClient(t *testing.T) {
	mt.TestSimnetMultiRPCClient(t, alphaWSPort, alphaHTTPPort)
}

func TestMonitorTestnet(t *testing.T) {
	mt.TestMonitorNet(t, dex.Testnet)
}

func TestMonitorMainnet(t *testing.T) {
	mt.TestMonitorNet(t, dex.Mainnet)
}

func TestRPC(t *testing.T) {
	mt.TestRPC(t, dex.Mainnet)
}

var freeServers = []string{
	// https://www.alchemy.com/chain-connect/chain/ethereum
	// Passing 09-10-2023
	"https://rpc.builder0x69.io",
	"https://rpc.ankr.com/eth",
	"https://eth-mainnet.public.blastapi.io",
	"https://ethereum.blockpi.network/v1/rpc/public",
	"https://rpc.flashbots.net",
	"https://eth.api.onfinality.io/public",
	"https://eth-mainnet-public.unifra.io",
	// Not passing 09-10-2023
	"https://api.securerpc.com/v1",
	"https://virginia.rpc.blxrbdn.com",
	"https://eth.rpc.blxrbdn.com",
	"https://1rpc.io/eth",
	"https://g.w.lavanet.xyz:443/gateway/eth/rpc-http/f7ee0000000000000000000000000000",
	"https://api.mycryptoapi.com/eth",
	"https://rpc.mevblocker.io",
	"https://eth-mainnet.nodereal.io/v1/1659dfb40aa24bbb8153a677b98064d7",
	"https://endpoints.omniatech.io/v1/eth/mainnet/public",
	"https://uk.rpc.blxrbdn.com",
	"https://singapore.rpc.blxrbdn.com",
	"https://eth.llamarpc.com",
	"https://ethereum.publicnode.com",
	"https://api.zmok.io/mainnet/oaen6dy8ff6hju9k",
	"https://ethereumnodelight.app.runonflux.io",
	"https://eth-rpc.gateway.pokt.network",
	"https://main-light.eth.linkpool.io",
	"https://rpc.payload.de",
	"https://cloudflare-eth.com",
	"https://main-rpc.linkpool.io/",
	"https://nodes.mewapi.io/rpc/eth",
}

func TestFreeServers(t *testing.T) {
	mt.TestFreeServers(t, freeServers, dex.Mainnet)
}

func TestMainnetCompliance(t *testing.T) {
	mt.TestMainnetCompliance(t)
}

func setupWallet(walletDir, seed string, net dex.Network) error {
	walletType := walletTypeRPC
	seedBytes, err := hex.DecodeString(seed)
	if err != nil {
		return fmt.Errorf("error decoding seed: %v", err)
	}

	comp, err := NetworkCompatibilityData(net)
	if err != nil {
		return fmt.Errorf("error finding compatibility data: %v", err)
	}

	cfg := &asset.CreateWalletParams{
		Type: walletType,
		Seed: seedBytes,
		Pass: []byte(pw),
		Settings: map[string]string{
			"gasfeelimit":          "200",
			"providers":            filepath.Join(homeDir, "dextest", "eth", "alpha", "node", "geth.ipc"),
			"special_activelyUsed": "false",
		},
		DataDir: walletDir,
		Net:     net,
		Logger:  dex.StdOutLogger("T", dex.LevelTrace),
	}

	return CreateEVMWallet(dexeth.ChainIDs[net], cfg, &comp, false)
}

func TestMakeBondTx(t *testing.T) {
	err := setupWallet(simnetWalletDir, simnetWalletSeed, dex.Simnet)
	if err != nil {
		t.Fatalf("error setting up wallet: %v", err)
	}

	walletConfig := &asset.WalletConfig{
		Type: "rpc",
		Settings: map[string]string{
			"gasfeelimit":          "200",
			"providers":            filepath.Join(homeDir, "dextest", "eth", "alpha", "node", "geth.ipc"),
			"special_activelyUsed": "false",
		},
		PeersChange: func(uint32, error) {},
		DataDir:     simnetWalletDir,
	}
	log := dex.StdOutLogger("T", dex.LevelTrace)

	backend, err := newWallet(walletConfig, log, dex.Simnet)
	if err != nil {
		t.Fatalf("error creating wallet: %v", err)
	}

	context, cancel := context.WithCancel(context.Background())
	defer cancel()
	cm := dex.NewConnectionMaster(backend)
	err = cm.Connect(context)
	if err != nil {
		t.Fatalf("error connecting wallet: %v", err)
	}

	priv, err := secp256k1.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}

	addr, err := backend.DepositAddress()
	if err != nil {
		t.Fatalf("error getting deposit address: %v", err)
	}
	fmt.Printf("deposit address: %s\n", addr)

	accountID := encode.RandomBytes(32)
	fmt.Printf("accountID %x\n", accountID)
	amt := uint64(1000000000)
	bond, _, err := backend.MakeBondTx(0, amt, 0, time.Now(), priv, accountID)
	if err != nil {
		t.Fatalf("error creating bond tx: %v", err)
	}

	err = backend.Unlock([]byte(pw))
	if err != nil {
		t.Fatalf("error unlocking wallet: %v", err)
	}

	bondTxHash, err := backend.SendTransaction(bond.SignedTx)
	if err != nil {
		t.Fatalf("error sending bond tx: %v", err)
	}

	fmt.Printf("Initial bond ID %s\n", hex.EncodeToString(priv.Serialize()))
	fmt.Printf("Initial bond TX Hash %x\n", bondTxHash)

	var confs uint32
	for i := 0; i < 30; i++ {
		confs, err = backend.BondConfirmations(ctx, bond.CoinID)
		if err != nil {
			t.Fatalf("error getting confirmations: %v", err)
		}
		if confs > 0 {
			break
		}
		fmt.Println("Waiting for confirmations for initial bond tx...")
		time.Sleep(1 * time.Second)
	}
	if confs == 0 {
		t.Fatalf("no confirmations")
	}

	newPriv1, err := secp256k1.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}

	newPriv2, err := secp256k1.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}

	fmt.Printf("Change Bond ID: %s\n", hex.EncodeToString(newPriv1.Serialize()))
	fmt.Printf("Updated Bond ID: %s\n", hex.EncodeToString(newPriv2.Serialize()))

	newBonds, err := backend.UpdateBondTx(0, []dex.Bytes{bond.CoinID}, []*secp256k1.PrivateKey{newPriv1, newPriv2}, amt/2, time.Now())
	if err != nil {
		t.Fatalf("error creating update bond tx: %v", err)
	}

	for _, newBond := range newBonds {
		newBondTxHash, err := backend.SendTransaction(newBond.SignedTx)
		if err != nil {
			t.Fatalf("error sending update bond tx: %v", err)
		}
		fmt.Printf("Update bond tx hash %x\n", newBondTxHash)
	}

	for i := 0; i < 30; i++ {
		confs, err = backend.BondConfirmations(ctx, newBonds[0].CoinID)
		if err != nil {
			t.Fatalf("error getting confirmations: %v", err)
		}
		if confs > 0 {
			break
		}
		fmt.Println("Waiting for confirmations for update bond tx...")
		time.Sleep(1 * time.Second)
	}
	if confs == 0 {
		t.Fatalf("no confirmations")
	}

	bal, err := backend.Balance()
	if err != nil {
		t.Fatalf("error getting balance: %v", err)
	}
	preRefundBalance := bal.Available

	_, err = backend.RefundBond(ctx, 0, newBonds[0].CoinID, nil, 0, priv)
	if err != nil {
		t.Fatalf("error creating refund tx: %v", err)
	}

	for i := 0; i < 20; i++ {
		bal, err := backend.Balance()
		if err != nil {
			t.Fatalf("error getting balance: %v", err)
		}
		if bal.Available > preRefundBalance {
			return
		}
		fmt.Println("Waiting for bond refund...")
		time.Sleep(1 * time.Second)
	}
}
