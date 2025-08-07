///go:build xmrlive

package xmr

import (
	"context"
	"os"
	"path"
	"testing"

	"decred.org/dcrdex/dex"
)

func TestCliGenerateRefreshWalletCmd(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	home, _ := os.UserHomeDir()
	installedToolsDir := path.Join(home, InstalledToolsDir)
	testDir := path.Join(home, TestDir)
	net := dex.Testnet // Testnet 5m from checkpoint 550000; Mainnet 5s from checkpoint 3375700
	cliToolsDir := installedToolsDir
	dataDir := testDir
	trustedDaemons := getTrustedDaemons(net, true, dataDir)
	pw := "abc"
	err := cliGenerateRefreshWallet(ctx, trustedDaemons[0], net, dataDir, cliToolsDir, pw, true)
	if err != nil {
		t.Fatalf("%v", err)
	}
}
