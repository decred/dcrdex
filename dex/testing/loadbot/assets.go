// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package main

import (
	"context"
	"net"
	"path/filepath"
	"strconv"
	"time"

	"decred.org/dcrdex/client/core"
)

// AssetType categorizes assets by their wallet behavior.
type AssetType int

const (
	// AssetTypeBTCClone covers btc, ltc, bch, dgb - standard bitcoind-like wallets with multi-wallet support.
	AssetTypeBTCClone AssetType = iota
	// AssetTypeDCR is for Decred which uses dcrwallet with account-based wallets.
	AssetTypeDCR
	// AssetTypeETH is for Ethereum and its tokens.
	AssetTypeETH
	// AssetTypePolygon is for Polygon and its tokens (uses IPC).
	AssetTypePolygon
	// AssetTypeNewNode covers doge, firo, zec, zcl - require a new node per wallet.
	AssetTypeNewNode
	// AssetTypeDash is for Dash which behaves like BTC but with different unlock params.
	AssetTypeDash
)

// AssetDef defines asset-specific behavior for the loadbot.
type AssetDef struct {
	ID           uint32
	Symbol       string
	Type         AssetType
	WalletType   string // "bitcoindRPC", "dcrwalletRPC", etc.
	RPCConfigKey string // "rpcport" vs "rpclisten" vs "ListenAddr"
	NeedsNewNode bool
	// NeedsWalletPass is true if the wallet needs a password for operations.
	NeedsWalletPass bool
	// UnlockDur is the unlock duration for walletpassphrase commands ("0" for indefinite, "4294967295" for max).
	UnlockDur string
	// ExtraUnlock contains additional unlock commands (e.g., dcr's unlockaccount).
	ExtraUnlock [][]string
	// AddressArgs are arguments for getting a new address from the harness.
	AddressArgs []string
	// StaticAddress is used for assets like zec that use a fixed address.
	StaticAddress string
	// WalletFormFunc creates the wallet form given name and port.
	WalletFormFunc func(node, name, port string) *core.WalletForm
	// SendCmd returns the harness command to send funds.
	SendCmd func(addr, amt string) []string
	// HasSpecialSend indicates the asset uses a special send script (eth, tokens).
	HasSpecialSend bool
	// SpecialSendCmd is the send command for assets with special send handling.
	SpecialSendCmd func(addr, amt string) []string
	// StartupDelay is extra time to wait after starting a new node.
	StartupDelay int // seconds
}

// assetRegistry maps asset symbols to their definitions.
var assetRegistry = map[string]*AssetDef{
	dcr: {
		ID:          dcrID,
		Symbol:      dcr,
		Type:        AssetTypeDCR,
		WalletType:  "SPV",
		AddressArgs: []string{"getnewaddress", "default", "ignore"},
		WalletFormFunc: func(node, name, port string) *core.WalletForm {
			return &core.WalletForm{
				Type:    "SPV",
				AssetID: dcrID,
				Config:  map[string]string{},
			}
		},
		SendCmd: func(addr, amt string) []string {
			return []string{"./alpha", "sendtoaddress", addr, amt}
		},
	},
	btc: {
		ID:           btcID,
		Symbol:       btc,
		Type:         AssetTypeBTCClone,
		WalletType:   "bitcoindRPC",
		RPCConfigKey: "rpcport",
		UnlockDur:    "4294967295",
		AddressArgs:  []string{"getnewaddress", "''", "bech32"},
		WalletFormFunc: func(node, name, port string) *core.WalletForm {
			return &core.WalletForm{
				Type:    "bitcoindRPC",
				AssetID: btcID,
				Config: map[string]string{
					"walletname":  name,
					"rpcuser":     "user",
					"rpcpassword": "pass",
					"rpcport":     port,
				},
			}
		},
		SendCmd: func(addr, amt string) []string {
			return []string{"./alpha", "sendtoaddress", addr, amt}
		},
	},
	ltc: {
		ID:           ltcID,
		Symbol:       ltc,
		Type:         AssetTypeBTCClone,
		WalletType:   "litecoindRPC",
		RPCConfigKey: "rpcport",
		UnlockDur:    "4294967295",
		AddressArgs:  []string{"getnewaddress", "''", "bech32"},
		WalletFormFunc: func(node, name, port string) *core.WalletForm {
			return &core.WalletForm{
				Type:    "litecoindRPC",
				AssetID: ltcID,
				Config: map[string]string{
					"walletname":  name,
					"rpcuser":     "user",
					"rpcpassword": "pass",
					"rpcport":     port,
				},
			}
		},
		SendCmd: func(addr, amt string) []string {
			return []string{"./alpha", "sendtoaddress", addr, amt}
		},
	},
	bch: {
		ID:           bchID,
		Symbol:       bch,
		Type:         AssetTypeBTCClone,
		WalletType:   "bitcoindRPC",
		RPCConfigKey: "rpcport",
		UnlockDur:    "4294967295",
		AddressArgs:  []string{"getnewaddress"},
		WalletFormFunc: func(node, name, port string) *core.WalletForm {
			return &core.WalletForm{
				Type:    "bitcoindRPC",
				AssetID: bchID,
				Config: map[string]string{
					"walletname":  name,
					"rpcuser":     "user",
					"rpcpassword": "pass",
					"rpcport":     port,
				},
			}
		},
		SendCmd: func(addr, amt string) []string {
			return []string{"./alpha", "sendtoaddress", addr, amt}
		},
	},
	dgb: {
		ID:           dgbID,
		Symbol:       dgb,
		Type:         AssetTypeBTCClone,
		WalletType:   "digibytedRPC",
		RPCConfigKey: "rpcport",
		UnlockDur:    "4294967295",
		AddressArgs:  []string{"getnewaddress", "''", "bech32"},
		WalletFormFunc: func(node, name, port string) *core.WalletForm {
			return &core.WalletForm{
				Type:    "digibytedRPC",
				AssetID: dgbID,
				Config: map[string]string{
					"walletname":  name,
					"rpcuser":     "user",
					"rpcpassword": "pass",
					"rpcport":     port,
				},
			}
		},
		SendCmd: func(addr, amt string) []string {
			return []string{"./alpha", "sendtoaddress", addr, amt}
		},
	},
	dash: {
		ID:           dashID,
		Symbol:       dash,
		Type:         AssetTypeDash,
		WalletType:   "dashdRPC",
		RPCConfigKey: "rpcport",
		UnlockDur:    "4294967295",
		AddressArgs:  []string{"getnewaddress"},
		WalletFormFunc: func(node, name, port string) *core.WalletForm {
			return &core.WalletForm{
				Type:    "dashdRPC",
				AssetID: dashID,
				Config: map[string]string{
					"walletname":  name,
					"rpcuser":     "user",
					"rpcpassword": "pass",
					"rpcport":     port,
				},
			}
		},
		SendCmd: func(addr, amt string) []string {
			return []string{"./alpha", "sendtoaddress", addr, amt}
		},
	},
	doge: {
		ID:           dogeID,
		Symbol:       doge,
		Type:         AssetTypeNewNode,
		WalletType:   "dogecoindRPC",
		RPCConfigKey: "rpcport",
		NeedsNewNode: true,
		UnlockDur:    "4294967295",
		AddressArgs:  []string{"getnewaddress"},
		WalletFormFunc: func(node, name, port string) *core.WalletForm {
			return &core.WalletForm{
				Type:    "dogecoindRPC",
				AssetID: dogeID,
				Config: map[string]string{
					"walletname":   name,
					"rpcuser":      "user",
					"rpcpassword":  "pass",
					"rpcport":      port,
					"feeratelimit": "40000",
				},
			}
		},
		SendCmd: func(addr, amt string) []string {
			return []string{"./alpha", "sendtoaddress", addr, amt}
		},
	},
	firo: {
		ID:           firoID,
		Symbol:       firo,
		Type:         AssetTypeNewNode,
		WalletType:   "firodRPC",
		RPCConfigKey: "rpcport",
		NeedsNewNode: true,
		UnlockDur:    "4294967295",
		AddressArgs:  []string{"getnewaddress"},
		WalletFormFunc: func(node, name, port string) *core.WalletForm {
			return &core.WalletForm{
				Type:    "firodRPC",
				AssetID: firoID,
				Config: map[string]string{
					"walletname":  name,
					"rpcuser":     "user",
					"rpcpassword": "pass",
					"rpcport":     port,
				},
			}
		},
		SendCmd: func(addr, amt string) []string {
			return []string{"./alpha", "sendtoaddress", addr, amt}
		},
	},
	zec: {
		ID:            zecID,
		Symbol:        zec,
		Type:          AssetTypeNewNode,
		WalletType:    "zcashdRPC",
		RPCConfigKey:  "rpcport",
		NeedsNewNode:  true,
		StaticAddress: "tmEgW8c44RQQfft9FHXnqGp8XEcQQSRcUXD", // ALPHA_ADDR in the zcash harness.sh
		StartupDelay:  10,
		WalletFormFunc: func(node, name, port string) *core.WalletForm {
			return &core.WalletForm{
				Type:    "zcashdRPC",
				AssetID: zecID,
				Config: map[string]string{
					"walletname":  name,
					"rpcuser":     "user",
					"rpcpassword": "pass",
					"rpcport":     port,
				},
			}
		},
		SendCmd: func(addr, amt string) []string {
			return []string{"./alpha", "sendtoaddress", addr, amt}
		},
	},
	zcl: {
		ID:            zclID,
		Symbol:        zcl,
		Type:          AssetTypeNewNode,
		WalletType:    "zclassicdRPC",
		RPCConfigKey:  "rpcport",
		NeedsNewNode:  true,
		StaticAddress: "tmEgW8c44RQQfft9FHXnqGp8XEcQQSRcUXD", // Same as zec
		StartupDelay:  10,
		WalletFormFunc: func(node, name, port string) *core.WalletForm {
			return &core.WalletForm{
				Type:    "zclassicdRPC",
				AssetID: zclID,
				Config: map[string]string{
					"walletname":  name,
					"rpcuser":     "user",
					"rpcpassword": "pass",
					"rpcport":     port,
				},
			}
		},
		SendCmd: func(addr, amt string) []string {
			return []string{"./alpha", "sendtoaddress", addr, amt}
		},
	},
	eth: {
		ID:             ethID,
		Symbol:         eth,
		Type:           AssetTypeETH,
		WalletType:     "rpc",
		RPCConfigKey:   "ListenAddr",
		AddressArgs:    []string{"attach", `--exec eth.accounts[0]`},
		HasSpecialSend: true,
		WalletFormFunc: func(node, name, port string) *core.WalletForm {
			return &core.WalletForm{
				Type:    "rpc",
				AssetID: ethID,
				Config: map[string]string{
					"providers": "ws://127.0.0.1:38557",
				},
			}
		},
		SpecialSendCmd: func(addr, amt string) []string {
			return []string{"./sendtoaddress", addr, amt}
		},
	},
	usdc: {
		ID:             usdcID,
		Symbol:         usdc,
		Type:           AssetTypeETH,
		WalletType:     "rpc",
		RPCConfigKey:   "ListenAddr",
		AddressArgs:    []string{"attach", `--exec eth.accounts[0]`},
		HasSpecialSend: true,
		WalletFormFunc: func(node, name, port string) *core.WalletForm {
			// Token wallets use the parent chain's wallet
			return &core.WalletForm{
				Type:    "rpc",
				AssetID: ethID,
				Config: map[string]string{
					"providers": "ws://127.0.0.1:38557",
				},
			}
		},
		SpecialSendCmd: func(addr, amt string) []string {
			return []string{"./sendUSDC", addr, amt}
		},
	},
	polygon: {
		ID:             polygonID,
		Symbol:         polygon,
		Type:           AssetTypePolygon,
		WalletType:     "rpc",
		RPCConfigKey:   "", // uses IPC
		AddressArgs:    []string{"attach", `--exec eth.accounts[0]`},
		HasSpecialSend: true,
		WalletFormFunc: func(node, name, port string) *core.WalletForm {
			rpcProvider := filepath.Join(dextestDir, "polygon", "alpha", "bor", "bor.ipc")
			if node == beta {
				rpcProvider = filepath.Join(dextestDir, "eth", "beta", "bor", "bor.ipc")
			}
			return &core.WalletForm{
				Type:    "rpc",
				AssetID: polygonID,
				Config: map[string]string{
					"providers": rpcProvider,
				},
			}
		},
		SpecialSendCmd: func(addr, amt string) []string {
			return []string{"./sendtoaddress", addr, amt}
		},
	},
	usdcp: {
		ID:             usdcpID,
		Symbol:         usdcp,
		Type:           AssetTypePolygon,
		WalletType:     "rpc",
		RPCConfigKey:   "", // uses IPC
		AddressArgs:    []string{"attach", `--exec eth.accounts[0]`},
		HasSpecialSend: true,
		WalletFormFunc: func(node, name, port string) *core.WalletForm {
			rpcProvider := filepath.Join(dextestDir, "polygon", "alpha", "bor", "bor.ipc")
			if node == beta {
				rpcProvider = filepath.Join(dextestDir, "eth", "beta", "bor", "bor.ipc")
			}
			return &core.WalletForm{
				Type:    "rpc",
				AssetID: polygonID,
				Config: map[string]string{
					"providers": rpcProvider,
				},
			}
		},
		SpecialSendCmd: func(addr, amt string) []string {
			return []string{"./sendUSDC", addr, amt}
		},
	},
}

// getAssetDef returns the asset definition for the given symbol.
// It also handles token symbols by mapping them to their parent chain.
func getAssetDef(symbol string) *AssetDef {
	if def, ok := assetRegistry[symbol]; ok {
		return def
	}
	return nil
}

// rpcAddrFromRegistry returns the RPC address for the given symbol using the registry.
func rpcAddrFromRegistry(symbol string) string {
	def := getAssetDef(symbol)
	if def == nil || def.RPCConfigKey == "" {
		return ""
	}
	if symbol == baseSymbol {
		return alphaCfgBase[def.RPCConfigKey]
	}
	return alphaCfgQuote[def.RPCConfigKey]
}

// unlockWalletFromRegistry unlocks wallets for the given symbol.
func unlockWalletFromRegistry(symbol string) error {
	def := getAssetDef(symbol)
	if def == nil {
		return nil
	}

	// ETH, polygon, DCR SPV, zec, zcl don't need unlocking
	switch def.Type {
	case AssetTypeETH, AssetTypePolygon:
		return nil
	case AssetTypeDCR:
		if def.WalletType == "SPV" {
			return nil
		}
	case AssetTypeNewNode:
		if symbol == zec || symbol == zcl {
			return nil
		}
	}

	// Standard unlock with walletpassphrase
	if def.UnlockDur != "" {
		<-harnessCtl(ctx, symbol, "./alpha", "walletpassphrase", "abc", def.UnlockDur)
		<-harnessCtl(ctx, symbol, "./beta", "walletpassphrase", "abc", def.UnlockDur)
	}

	// Additional unlock commands (e.g., DCR's unlockaccount)
	for _, cmd := range def.ExtraUnlock {
		<-harnessCtl(ctx, symbol, cmd[0], cmd[1:]...)
	}

	return nil
}

// alphaAddressFromRegistry gets an alpha address for the given symbol.
func alphaAddressFromRegistry(symbol string) (string, error) {
	def := getAssetDef(symbol)
	if def == nil {
		return "", nil
	}

	// Check for static address first
	if def.StaticAddress != "" {
		return def.StaticAddress, nil
	}

	if len(def.AddressArgs) == 0 {
		return "", nil
	}

	res := <-harnessCtl(ctx, symbol, "./alpha", def.AddressArgs...)
	if res.err != nil {
		return "", res.err
	}
	return trimQuotes(res.output), nil
}

// trimQuotes removes surrounding quotes from a string.
func trimQuotes(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}

// sendFromRegistry sends funds using the asset registry.
func sendFromRegistry(symbol, addr string, val uint64) error {
	def := getAssetDef(symbol)
	if def == nil {
		return nil
	}

	log.Tracef("Sending %s %s to %s", fmtAtoms(val, symbol), symbol, addr)

	var res *harnessResult

	if def.HasSpecialSend && def.SpecialSendCmd != nil {
		var amt string
		switch symbol {
		case eth, polygon:
			// eth values are always handled as gwei, convert to wei
			amt = strconv.FormatFloat(float64(val)/1e9, 'f', 9, 64)
		case usdc, usdcp:
			amt = strconv.FormatFloat(float64(val)/1e6, 'f', 6, 64)
		default:
			amt = fmtConv(val, symbol)
		}
		cmd := def.SpecialSendCmd(addr, amt)
		res = <-harnessCtl(ctx, symbol, cmd[0], cmd[1:]...)
	} else if def.SendCmd != nil {
		// Handle zec/zcl special locking
		if symbol == zec || symbol == zcl {
			zecSendMtx.Lock()
			cmd := def.SendCmd(addr, fmtConv(val, symbol))
			res = <-harnessCtl(ctx, symbol, cmd[0], cmd[1:]...)
			zecSendMtx.Unlock()
		} else {
			cmd := def.SendCmd(addr, fmtConv(val, symbol))
			res = <-harnessCtl(ctx, symbol, cmd[0], cmd[1:]...)
		}
	}

	if res != nil {
		return res.err
	}
	return nil
}

// createWalletAccount creates a new wallet account for the given symbol.
// Returns the rpcPort to use (may be different for new-node assets).
func createWalletAccount(m *Mantle, symbol, name string) (rpcPort string, err error) {
	def := getAssetDef(symbol)
	if def == nil {
		return "", nil
	}

	switch def.Type {
	case AssetTypeETH, AssetTypePolygon:
		// Nothing to do for internal wallets
		return "", nil

	case AssetTypeDCR:
		if def.WalletType == "SPV" {
			return "", nil
		}
		cmdOut := <-harnessCtl(ctx, symbol, "./alpha", "createnewaccount", name)
		if cmdOut.err != nil {
			return "", cmdOut.err
		}

	case AssetTypeBTCClone, AssetTypeDash:
		cmdOut := <-harnessCtl(ctx, symbol, "./new-wallet", alpha, name)
		if cmdOut.err != nil {
			return "", cmdOut.err
		}

	case AssetTypeNewNode:
		// These require a totally new node
		rpcPort, err = startNewNode(m, symbol, name, def.StartupDelay)
		if err != nil {
			return "", err
		}
	}

	return rpcPort, nil
}

// startNewNode starts a new node for assets that require it (doge, firo, zec, zcl).
func startNewNode(m *Mantle, symbol, name string, extraDelay int) (string, error) {
	addrs, err := findOpenAddrs(2)
	if err != nil {
		return "", err
	}
	_, rpcPort, err := splitHostPort(addrs[0].String())
	if err != nil {
		return "", err
	}
	_, networkPort, err := splitHostPort(addrs[1].String())
	if err != nil {
		return "", err
	}

	stopFn := func(ctx_ context.Context) {
		<-harnessCtl(ctx_, symbol, "./stop-wallet", rpcPort, name)
	}
	if err = harnessProcessCtl(symbol, stopFn, "./start-wallet", name, rpcPort, networkPort); err != nil {
		return "", err
	}

	// Wait for node to start
	sleepCtx(3)
	if extraDelay > 0 {
		sleepCtx(extraDelay)
	}

	// Connect to alpha
	cmdOut := <-harnessCtl(ctx, symbol, "./connect-alpha", rpcPort, name)
	if cmdOut.err != nil {
		return "", cmdOut.err
	}

	sleepCtx(1)
	if extraDelay > 0 {
		sleepCtx(extraDelay * 2)
	}

	return rpcPort, nil
}

// splitHostPort is a helper wrapper around net.SplitHostPort.
func splitHostPort(addr string) (host, port string, err error) {
	return net.SplitHostPort(addr)
}

// sleepCtx sleeps for the given number of seconds, respecting context cancellation.
func sleepCtx(seconds int) {
	select {
	case <-ctx.Done():
	case <-time.After(time.Duration(seconds) * time.Second):
	}
}

// OrderErrorType classifies order placement errors.
type OrderErrorType int

const (
	OrderErrorNone OrderErrorType = iota
	OrderErrorOverLimit
	OrderErrorApprovalPending
	OrderErrorFatal
)

// classifyOrderError returns the type of order error.
func classifyOrderError(err error) OrderErrorType {
	if err == nil {
		return OrderErrorNone
	}
	if isOverLimitError(err) {
		return OrderErrorOverLimit
	}
	if isApprovalPendingError(err) {
		return OrderErrorApprovalPending
	}
	return OrderErrorFatal
}

// isRecoverableOrderError returns true if the error is recoverable (not fatal).
func isRecoverableOrderError(err error) bool {
	errType := classifyOrderError(err)
	return errType == OrderErrorOverLimit || errType == OrderErrorApprovalPending
}
