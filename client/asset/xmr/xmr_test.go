package xmr

import (
	"fmt"
	"os"
	"path"
	"runtime"
	"testing"

	"decred.org/dcrdex/dex"
)

// adjust if needed
const InstalledToolsDir = "/monero-x86_64-linux-gnu-v0.18.4.1"
const TestDir = "/dex/dcrdex/client/asset/xmr/tmp_dir" // TODO(xmr) compare td against expected

var userDaemons = `
[
    {
        "url": "http://127.0.0.1:18081",
        "net": "main",
        "tls": false
    },
    {
        "url": "https://node.sethforprivacy.com:18089",
        "net": "main",
        "tls": true
    },
    {
        "url": "http://node.sethforprivacy.com:18089",
        "net": "main",
        "tls": false
    }
]
`

func TestUserDaemons(t *testing.T) {
	dataDir, err := os.MkdirTemp("", "dataDir_")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.RemoveAll(dataDir); err != nil {
			t.Logf("error removing temporary directory %s: %v", dataDir, err)
		}
	}()
	f, err := os.Create(path.Join(dataDir, "daemons.json"))
	if err != nil {
		t.Fatalf("create tmp file = %v", err)
	}
	_, err = f.Write([]byte(userDaemons))
	if err != nil {
		t.Fatalf("error writing json file - %v", err)
	}
	f.Close()

	td := getTrustedDaemons(dex.Mainnet, true, dataDir)
	for _, t := range td {
		fmt.Println(t)
	}
	fmt.Println()

	fmt.Println("Any")
	td = getTrustedDaemons(dex.Mainnet, false, dataDir)
	for _, t := range td {
		fmt.Println(t)
	}
	fmt.Println()

	fmt.Println("Can be used for CLI generate refresh")
	td = getTrustedDaemons(dex.Testnet, true, dataDir)
	for _, t := range td {
		fmt.Println(t)
	}
	fmt.Println()

	fmt.Println("Any")
	td = getTrustedDaemons(dex.Testnet, false, dataDir)
	for _, t := range td {
		fmt.Println(t)
	}
	fmt.Println()

	fmt.Println("Can be used for CLI generate refresh")
	td = getTrustedDaemons(dex.Simnet, true, dataDir)
	for _, t := range td {
		fmt.Println(t)
	}
	fmt.Println()

	fmt.Println("Any")
	td = getTrustedDaemons(dex.Simnet, false, dataDir)
	for _, t := range td {
		fmt.Println(t)
	}
	fmt.Println()
}

func TestSimnetFilepaths(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Log("not tested for windows os")
		return
	}
	home, _ := os.UserHomeDir()
	dataDir1 := path.Join(home, "dextest", "simnet-walletpair", "dexc1", "assetdb", "xmr")
	dataDir2 := path.Join(home, "dextest", "simnet-walletpair", "dexc2", "assetdb", "xmr")
	dataDirBad := path.Join(home, "dextest", "walletpair", "dexc2", "assetdb", "xmr")
	port1 := getRegtestWalletServerRpcPort(dataDir1)
	if port1 != DefaultRegtestWalletServerRpcPort {
		t.Fatalf("bad port %s for dataDir1=%s", port1, dataDir1)
	}
	port2 := getRegtestWalletServerRpcPort(dataDir2)
	if port2 != AlternateRegtestWalletServerRpcPort {
		t.Fatalf("bad port %s for dataDir2=%s", port2, dataDir2)
	}
	portBad := getRegtestWalletServerRpcPort(dataDirBad)
	if portBad != "bad-path" {
		t.Fatalf("bad path dataDirBad=%s", dataDirBad)
	}
}

func TestToolsVersion(t *testing.T) {
	var toolsDir = "monero-win-x64-v0.18.4.1"
	err := checkToolsVersion(toolsDir)
	if err != nil {
		t.Fatalf("bad tools version %s", toolsDir)
	}
	toolsDir = "monero-mac-x64-v0.18.4.1"
	err = checkToolsVersion(toolsDir)
	if err != nil {
		t.Fatalf("bad tools version %s", toolsDir)
	}
	toolsDir = "monero-linux-x64-v0.18.4.1"
	err = checkToolsVersion(toolsDir)
	if err != nil {
		t.Fatalf("bad tools version %s", toolsDir)
	}
	toolsDir = "monero-linux-armv8-v0.18.4.1"
	err = checkToolsVersion(toolsDir)
	if err != nil {
		t.Fatalf("bad tools version %s", toolsDir)
	}
	toolsDir = "monero-freebsd-x64-v0.18.4.1"
	err = checkToolsVersion(toolsDir)
	if err != nil {
		t.Fatalf("bad tools version %s", toolsDir)
	}
	toolsDir = "freebsd-x64-v0.18.4.1"
	err = checkToolsVersion(toolsDir)
	if err == nil {
		t.Fatalf("expected bad tools version %s", toolsDir)
	}
}

func TestMoneroVersion(t *testing.T) {
	_, err := newMoneroVersionFromVersionString("x.y.3.6")
	if err == nil {
		t.Fatal("expected x.y.3.6 invalid version")
	}
	_, err = newMoneroVersionFromVersionString("7.3.6")
	if err == nil {
		t.Fatal("expected 7.3.6 invalid version")
	}
	m0, err := newMoneroVersionFromVersionString("0.7.3.6")
	if err != nil {
		t.Fatalf("%v", err)
	}
	if m0.valid() {
		t.Fatalf("%v", err)
	}

	m1, err := newMoneroVersionFromVersionString("0.18.4.0")
	if err != nil {
		t.Fatalf("%v", err)
	}
	if !m1.valid() {
		t.Fatalf("0.18.4.0 - expected valid got invalid")
	}

	m2, err := newMoneroVersionFromVersionString("0.18.4.1")
	if err != nil {
		t.Fatalf("%v", err)
	}
	if !m2.valid() {
		t.Fatalf("0.18.4.1 - expected valid got invalid")
	}
	if m1.compare(m2) > 0 {
		t.Fatal("compare error")

	}
	if m1.compare(m2) == 0 {
		t.Fatal("compare error")
	}
	if !(m1.compare(m2) < 0) {
		t.Fatal("compare error")
	}

	// greater than recommended
	m3, err := newMoneroVersionFromVersionString("0.18.4.7")
	if err != nil {
		t.Fatalf("%v", err)
	}
	if !m3.valid() {
		t.Fatalf("0.18.4.7 - expected valid got invalid")
	}
}

// No keystore service running on CI so commented out.
//
// WARNING THIS TEST WILL DESTROY ANY EXISTING DEX XMR WALLET PW ON SIMNET

// func TestKeystore(t *testing.T) {
// 	var net dex.Network = dex.Simnet
// 	fmt.Println(runtime.GOOS)
// 	fmt.Println("network =", net)

// 	b := make([]byte, 32)
// 	rand.Read(b)
// 	pass := hex.EncodeToString(b)

// 	s := new(keystore)
// 	err := s.put(pass, net)
// 	if err != nil {
// 		t.Fatal(err)
// 	}

// 	s2 := new(keystore)
// 	pass2, err := s2.get(net)
// 	if err != nil {
// 		t.Fatal(err)
// 	}

// 	if pass != pass2 {
// 		t.Fatalf("unexpected stored password mismatch pass %s pass2 %x", pass, pass2)
// 	}

// 	err = s2.put(pass, net)
// 	if err != nil {
// 		t.Fatal(err)
// 	}

// 	pw, err := s.get(net)
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	fmt.Println(pw)

// 	pw2, err := s.get(net)
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	fmt.Println(pw2)

// 	if pw2 != pw {
// 		t.Fatalf("unexpected stored password mismatch pw %s pw2 %s", pw, pw2)
// 	}

// 	err = s.delete(net)
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	fmt.Println("pw deleted")

// 	err = s.delete(net)
// 	if err == nil {
// 		fmt.Println("pw deleted again")
// 		t.Fatalf("pw deleted again %v", err)
// 	}
// }
