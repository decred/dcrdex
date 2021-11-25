package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	dexasset "decred.org/dcrdex/client/asset"
	_ "decred.org/dcrdex/client/asset/btc"
	_ "decred.org/dcrdex/client/asset/dcr"
	"decred.org/dcrdex/dex"

	"github.com/decred/dcrd/dcrutil/v4"

	btcChainhash "github.com/btcsuite/btcd/chaincfg/chainhash"
	btcWire "github.com/btcsuite/btcd/wire"
	dcrChainhash "github.com/decred/dcrd/chaincfg/chainhash"
	dcrWire "github.com/decred/dcrd/wire"
)

// flags
var (
	assetF      = flag.String("asset", "dcr", "The asset name, e.g. dcr or btc")
	walletTypeF = flag.String("wallettype", "rpc", "e.g. rpc or native")

	walletdir = flag.String("walletdir", "", "Wallet data folder for native (non-RPC) wallets")

	rpchost = flag.String("btchost", "", "RPC host:port")
	rpcuser = flag.String("btcuser", "user", "RPC username")
	rpcpass = flag.String("btcpass", "pass", "RPC password")
	rpccert = flag.String("dcrcert", "dcrwallet.cert", "dcr RPC TLS certificate")

	name       = flag.String("name", "", "The wallet or account name, usually an empty string for BTC or \"default\" for DCR")
	walletpass = flag.String("walletpass", "abc", "The wallet passphrase")

	testnet = flag.Bool("testnet", false, "testnet")
	simnet  = flag.Bool("simnet", false, "simnet")
)

var (
	dexcDir string // needed for native wallets
)

func defaultRPCHost(net dex.Network, assetID uint32) string {
	switch net {
	case dex.Mainnet:
		switch assetID {
		case 0:
			return "127.0.0.1:8332"
		case 42:
			return "127.0.0.1:9110"
		}
	case dex.Testnet:
		switch assetID {
		case 0:
			return "127.0.0.1:18332"
		case 42:
			return "127.0.0.1:19110"
		}
	case dex.Simnet:
		switch assetID {
		case 0:
			return "127.0.0.1:20556" // "alpha" harness
		case 42:
			return "127.0.0.1:19562" // "alpha" harness wallet
		}
	}

	return ""
}

func hostFlagSet() bool {
	var hostSet bool
	flag.CommandLine.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "rpchost":
			hostSet = true
		case "wallettype":
			hostSet = f.Value.String() == "native"
		}
	})
	return hostSet
}

func main() {
	flag.Parse()

	args := flag.Args()
	if len(args) != 4 {
		fmt.Fprintf(os.Stderr, "need <contract tx hex> <idx> <contract data> <secret>\n")
		os.Exit(1)
	}

	assetSymb := strings.ToLower(*assetF)
	assetID, known := dex.BipSymbolID(assetSymb)
	if !known {
		fmt.Fprintf(os.Stderr, "unrecognized asset symbol %v\n", assetSymb)
		os.Exit(1)
	}
	supportedAssets := dexasset.Assets()
	if _, known = supportedAssets[assetID]; !known {
		fmt.Fprintf(os.Stderr, "unsupported asset %v\n", assetSymb)
		os.Exit(1)
	}

	dexcAppDir := dcrutil.AppDataDir("dexc", false)

	if *testnet && *simnet {
		fmt.Fprintf(os.Stderr, "cannot be both testnet and simnet")
		os.Exit(1)
	}
	if *testnet {
		dexcDir = filepath.Join(dexcAppDir, "testnet")
		net = dex.Testnet
	} else if *simnet {
		dexcDir = filepath.Join(dexcAppDir, "simnet")
		net = dex.Simnet
		if assetID == 42 && *rpccert == "" {
			*rpccert = "/home/jon/dextest/dcr/alpha/rpc.cert" // "alpha" harness wallet
		}
	} else { // mainnet
		dexcDir = filepath.Join(dexcAppDir, "mainnet")
		net = dex.Mainnet
	}

	if !hostFlagSet() {
		*rpchost = defaultRPCHost(net, assetID)
	}

	if err := _main(); err != nil {
		fmt.Fprint(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

var (
	ctx    context.Context
	cancel context.CancelFunc
	wg     *sync.WaitGroup

	net dex.Network
)

func _main() error {
	args := flag.Args()
	txrawS, voutS, contractS, secretS := args[0], args[1], args[2], args[3]

	contract, err := hex.DecodeString(contractS)
	if err != nil {
		return fmt.Errorf("contract %q decode error: %v", contractS, err)
	}
	txraw, err := hex.DecodeString(txrawS)
	if err != nil {
		return fmt.Errorf("txraw decode error: %v", err)
	}
	vout, err := strconv.ParseUint(voutS, 10, 32)
	if err != nil {
		return fmt.Errorf("vout %q decode error: %v", voutS, err)
	}
	secret, err := hex.DecodeString(secretS)
	if err != nil {
		return fmt.Errorf("secret %q decode error: %v", secretS, err)
	}

	ctx, cancel = context.WithCancel(context.Background())
	defer func() {
		cancel()
		if wg != nil {
			wg.Wait()
		}
	}()

	assetStr := *assetF
	wallet, err := connectClient(assetStr)
	if err != nil {
		return err
	}

	txid, coinID, err := decodeTx(assetStr, txraw, uint32(vout))
	if err != nil {
		return fmt.Errorf("checkTx: %v", err)
	}
	// fmt.Printf("\n[%f %s] ./%s redeem %x %x\n\n", float64(amt)/1e8, strings.ToUpper(asset), tool, contract, txraw, secret)

	// NOTE: Neither AuditContract nor Redeem check if the contract is already
	// spent! They will make a redeeming tx and report success regardless.

	auditInfo, err := wallet.AuditContract(coinID, contract, txraw, false)
	if err != nil {
		if !errors.Is(err, dexasset.CoinNotFoundError) {
			return fmt.Errorf("%s AuditContract: %v", assetStr, err)
		}
		// return fmt.Errorf("contract at %v:%d (%s) is already spent", txid, vout, asset)
	} else {
		fmt.Printf("Contract at %v (%s) expires at %v (past = %v)\n\n",
			auditInfo.Coin, assetStr, auditInfo.Expiration, time.Until(auditInfo.Expiration) < 0)
	}

	_, redeem, _, err := wallet.Redeem(&dexasset.RedeemForm{
		Redemptions: []*dexasset.Redemption{
			{
				Spends: auditInfo,
				Secret: secret,
			},
		},
	})
	if err != nil {
		return fmt.Errorf("redeem of %v:%d (%s) FAILED: %v", txid, vout, assetStr, err)
	}
	fmt.Printf("\nSUCCESSFULLY redeemed %v:%d (%s) with %v.\n\n", txid, vout, assetStr, redeem.String())
	if *walletTypeF == "native" {
		time.Sleep(7 * time.Second) // for native wallet broadcasters
	}

	return nil
}

// func coinIDString(assetID uint32, coinID []byte) string {
// 	coinStr, err := asset.DecodeCoinID(assetID, coinID)
// 	if err != nil {
// 		return "<invalid coin>:" + hex.EncodeToString(coinID)
// 	}
// 	return coinStr
// }

func toDCRCoinID(txHash *dcrChainhash.Hash, vout uint32) []byte {
	coinID := make([]byte, dcrChainhash.HashSize+4)
	copy(coinID[:dcrChainhash.HashSize], txHash[:])
	binary.BigEndian.PutUint32(coinID[dcrChainhash.HashSize:], vout)
	return coinID
}

func toBTCCoinID(txHash *btcChainhash.Hash, vout uint32) []byte {
	coinID := make([]byte, btcChainhash.HashSize+4)
	copy(coinID[:btcChainhash.HashSize], txHash[:])
	binary.BigEndian.PutUint32(coinID[btcChainhash.HashSize:], vout)
	return coinID
}

// There is no asset.Wallet method to create a coinID or txid from a raw
// transaction and an index, so we have to do some special handling
func decodeTx(assetStr string, txraw []byte, vout uint32) (txid string, coinID []byte, err error) {
	switch strings.ToLower(assetStr) {
	case "btc":
		msgTx := btcWire.NewMsgTx(btcWire.TxVersion)
		if err := msgTx.Deserialize(bytes.NewReader(txraw)); err != nil {
			return "", nil, err
		}
		// amt = msgTx.TxOut[vout].Value
		hash := msgTx.TxHash()
		txid = hash.String()
		coinID = toBTCCoinID(&hash, vout)
	case "dcr":
		msgTx := dcrWire.NewMsgTx()
		if err := msgTx.Deserialize(bytes.NewReader(txraw)); err != nil {
			return "", nil, err
		}
		hash := msgTx.TxHash()
		txid = hash.String()
		coinID = toDCRCoinID(&hash, vout)
	default:
		err = fmt.Errorf("unknown asset %s", assetStr)
	}
	return
}

func connectClient(asset string) (dexasset.Wallet, error) {
	switch asset {
	case "dcr":
		logger := dex.NewLogger("refunder[DCR]", dex.LevelTrace, os.Stdout)
		walletCfg := &dexasset.WalletConfig{
			Type: "dcrwalletRPC", // switch on walletTypeF when there are new types
			Settings: map[string]string{
				"account":   *name,
				"username":  *rpcuser,
				"password":  *rpcpass,
				"rpclisten": *rpchost,
				"rpccert":   *rpccert,
			},
			PeersChange: func(uint32) {},
			TipChange:   func(error) {},
		}

		wallet, err := dexasset.OpenWallet(42, walletCfg, logger, net)
		if err != nil {
			return nil, fmt.Errorf("Setup (%s): %v", asset, err)
		}

		wg, err = wallet.Connect(ctx)
		if err != nil {
			return nil, fmt.Errorf("%s wallet Connect: %v", asset, err)
		}

		err = wallet.Unlock([]byte(*walletpass))
		if err != nil {
			return nil, fmt.Errorf("%s wallet Unlock: %v", asset, err)
		}

		return wallet, nil

	case "btc":
		var walletDir string
		walletType := "bitcoindRPC" // client/asset/btc.walletTypeRPC
		passBytes := []byte(*walletpass)
		if *walletTypeF == "native" {
			walletType = "SPV" // client/asset/btc.walletTypeSPV
			walletDir = filepath.Join(dexcDir, "assetdb", "btc")
			var err error
			passBytes, err = hex.DecodeString(*walletpass)
			if err != nil {
				return nil, fmt.Errorf("native wallet pass must be hexadecimal bytes: %v", err)
			}
		}

		logger := dex.NewLogger("refunder[BTC]", dex.LevelTrace, os.Stdout)
		walletCfg := &dexasset.WalletConfig{
			Type:        walletType,
			DataDir:     walletDir, // for native
			PeersChange: func(uint32) {},
			TipChange:   func(error) {},
			Settings: map[string]string{
				"walletname":  *name,
				"rpcuser":     *rpcuser,
				"rpcpassword": *rpcpass,
				"rpcbind":     *rpchost,
			},
		}
		wallet, err := dexasset.OpenWallet(0, walletCfg, logger, net)
		if err != nil {
			return nil, fmt.Errorf("Setup (%s): %v", asset, err)
		}
		wg, err = wallet.Connect(ctx)
		if err != nil {
			return nil, fmt.Errorf("%s wallet Connect: %v", asset, err)
		}

		err = wallet.Unlock(passBytes)
		if err != nil {
			return nil, fmt.Errorf("%s wallet Unlock: %v", asset, err)
		}

		return wallet, nil
	default:
		return nil, fmt.Errorf("unsupported asset %v", asset)
	}
}
