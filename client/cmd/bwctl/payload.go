// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package main

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/client/mm"
	"decred.org/dcrdex/client/rpcserver"
	"decred.org/dcrdex/dex/encode"
)

// buildPayload converts positional CLI args and passwords into a typed params
// struct for the given route. Returns nil payload for routes that take no
// params.
func buildPayload(route string, pws []encode.PassBytes, args []string) (any, error) {
	switch route {

	// No params.
	case "version", "wallets", "logout", "exchanges", "mmstatus":
		return nil, nil

	// System.
	case "help":
		return buildHelp(args)
	case "init":
		return buildInit(pws, args)
	case "login":
		return buildLogin(pws)

	// Wallet.
	case "newwallet":
		return buildNewWallet(pws, args)
	case "openwallet":
		return buildOpenWallet(pws, args)
	case "closewallet":
		return buildCloseWallet(args)
	case "togglewalletstatus":
		return buildToggleWalletStatus(args)
	case "rescanwallet":
		return buildRescanWallet(args)

	// Trading.
	case "trade":
		return buildTrade(pws, args)
	case "multitrade":
		return buildMultiTrade(pws, args)
	case "cancel":
		return buildCancel(args)
	case "orderbook":
		return buildOrderBook(args)
	case "myorders":
		return buildMyOrders(args)

	// Transactions.
	case "withdraw":
		return buildWithdraw(pws, args)
	case "send":
		return buildSend(pws, args)
	case "abandontx":
		return buildAbandonTx(args)
	case "appseed":
		return buildAppSeed(pws)
	case "deletearchivedrecords":
		return buildDeleteRecords(args)
	case "notifications":
		return buildNotifications(args)
	case "txhistory":
		return buildTxHistory(args)
	case "wallettx":
		return buildWalletTx(args)
	case "withdrawbchspv":
		return buildWithdrawBchSpv(pws, args)

	// DEX.
	case "discoveracct":
		return buildDiscoverAcct(pws, args)
	case "getdexconfig":
		return buildGetDEXConfig(args)
	case "bondassets":
		return buildBondAssets(args)
	case "bondopts":
		return buildBondOpts(args)
	case "postbond":
		return buildPostBond(pws, args)

	// Market Making.
	case "mmavailablebalances":
		return buildMMAvailableBalances(args)
	case "startmmbot":
		return buildStartBot(pws, args)
	case "stopmmbot":
		return buildStopBot(args)
	case "updaterunningbotcfg":
		return buildUpdateRunningBotCfg(args)
	case "updaterunningbotinv":
		return buildUpdateRunningBotInv(args)

	// Staking.
	case "stakestatus":
		return buildStakeStatus(args)
	case "setvsp":
		return buildSetVSP(args)
	case "purchasetickets":
		return buildPurchaseTickets(pws, args)
	case "setvotingprefs":
		return buildSetVotingPreferences(args)

	// Bridge.
	case "bridge":
		return buildBridge(args)
	case "checkbridgeapproval":
		return buildCheckBridgeApproval(args)
	case "approvebridgecontract":
		return buildApproveBridge(args)
	case "pendingbridges":
		return buildPendingBridges(args)
	case "bridgehistory":
		return buildBridgeHistory(args)
	case "supportedbridges":
		return buildSupportedBridges(args)
	case "bridgefeesandlimits":
		return buildBridgeFeesAndLimits(args)

	// Multisig.
	case "paymentmultisigpubkey":
		return buildPaymentMultisigPubkey(args)
	case "sendfundstomultisig":
		return buildCsvFile(args)
	case "signmultisig":
		return buildSignMultisig(args)
	case "refundpaymentmultisig":
		return buildCsvFile(args)
	case "viewpaymentmultisig":
		return buildCsvFile(args)
	case "sendpaymentmultisig":
		return buildCsvFile(args)

	// Peers.
	case "walletpeers":
		return buildWalletPeers(args)
	case "addwalletpeer":
		return buildAddRemovePeer(args)
	case "removewalletpeer":
		return buildAddRemovePeer(args)

	default:
		return nil, fmt.Errorf("unknown route %q", route)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func parseUint32(s string) (uint32, error) {
	v, err := strconv.ParseUint(s, 10, 32)
	return uint32(v), err
}

func parseUint64(s string) (uint64, error) {
	return strconv.ParseUint(s, 10, 64)
}

func parseInt(s string) (int, error) {
	return strconv.Atoi(s)
}

func parseInt64(s string) (int64, error) {
	return strconv.ParseInt(s, 10, 64)
}

func parseBool(s string) (bool, error) {
	return strconv.ParseBool(s)
}

// optArg returns args[i] if it exists, or "" otherwise.
func optArg(args []string, i int) string {
	if i < len(args) {
		return args[i]
	}
	return ""
}

// parseMapArg parses a JSON-encoded map[string]string. Returns nil for empty
// input.
func parseMapArg(s string) (map[string]string, error) {
	if s == "" {
		return nil, nil
	}
	m := make(map[string]string)
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return nil, fmt.Errorf("error parsing JSON map: %w", err)
	}
	return m, nil
}

// parseInventoryArg parses a JSON-encoded map[uint32]int64 from a string like
// [[assetID, amount], ...].
func parseInventoryArg(s string) (map[uint32]int64, error) {
	if s == "" {
		return nil, nil
	}
	var pairs [][2]int64
	if err := json.Unmarshal([]byte(s), &pairs); err != nil {
		return nil, fmt.Errorf("error parsing inventory JSON: %w", err)
	}
	m := make(map[uint32]int64, len(pairs))
	for _, p := range pairs {
		m[uint32(p[0])] = p[1]
	}
	return m, nil
}

// ---------------------------------------------------------------------------
// System builders
// ---------------------------------------------------------------------------

func buildHelp(args []string) (*rpcserver.HelpParams, error) {
	p := &rpcserver.HelpParams{
		HelpWith: optArg(args, 0),
	}
	if s := optArg(args, 1); s != "" {
		v, err := parseBool(s)
		if err != nil {
			return nil, fmt.Errorf("bad includePasswords: %w", err)
		}
		p.IncludePasswords = v
	}
	return p, nil
}

func buildInit(pws []encode.PassBytes, args []string) (*rpcserver.InitParams, error) {
	if len(pws) < 1 {
		return nil, fmt.Errorf("missing app password")
	}
	p := &rpcserver.InitParams{
		AppPass: pws[0],
	}
	if s := optArg(args, 0); s != "" {
		p.Seed = &s
	}
	return p, nil
}

func buildLogin(pws []encode.PassBytes) (*rpcserver.LoginParams, error) {
	if len(pws) < 1 {
		return nil, fmt.Errorf("missing app password")
	}
	return &rpcserver.LoginParams{AppPass: pws[0]}, nil
}

// ---------------------------------------------------------------------------
// Wallet builders
// ---------------------------------------------------------------------------

func buildNewWallet(pws []encode.PassBytes, args []string) (*rpcserver.NewWalletParams, error) {
	if len(pws) < 2 {
		return nil, fmt.Errorf("missing passwords (need appPass and walletPass)")
	}
	if len(args) < 2 {
		return nil, fmt.Errorf("need at least assetID and walletType")
	}
	assetID, err := parseUint32(args[0])
	if err != nil {
		return nil, fmt.Errorf("bad assetID: %w", err)
	}
	p := &rpcserver.NewWalletParams{
		AppPass:    pws[0],
		WalletPass: pws[1],
		AssetID:    assetID,
		WalletType: args[1],
	}
	// Remaining args are config entries: either "key=value" or a JSON object.
	if len(args) > 2 {
		cfg := make(map[string]string)
		for _, a := range args[2:] {
			if strings.HasPrefix(a, "{") {
				// JSON object; merge into cfg.
				var jm map[string]string
				if err := json.Unmarshal([]byte(a), &jm); err != nil {
					return nil, fmt.Errorf("bad JSON config arg: %w", err)
				}
				for k, v := range jm {
					cfg[k] = v
				}
			} else if k, v, ok := strings.Cut(a, "="); ok {
				cfg[k] = v
			} else {
				return nil, fmt.Errorf("unrecognized config arg %q (expected key=value or JSON)", a)
			}
		}
		if len(cfg) > 0 {
			p.Config = cfg
		}
	}
	return p, nil
}

func buildOpenWallet(pws []encode.PassBytes, args []string) (*rpcserver.OpenWalletParams, error) {
	if len(pws) < 1 {
		return nil, fmt.Errorf("missing app password")
	}
	if len(args) < 1 {
		return nil, fmt.Errorf("missing assetID")
	}
	assetID, err := parseUint32(args[0])
	if err != nil {
		return nil, fmt.Errorf("bad assetID: %w", err)
	}
	return &rpcserver.OpenWalletParams{AppPass: pws[0], AssetID: assetID}, nil
}

func buildCloseWallet(args []string) (*rpcserver.CloseWalletParams, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("missing assetID")
	}
	assetID, err := parseUint32(args[0])
	if err != nil {
		return nil, fmt.Errorf("bad assetID: %w", err)
	}
	return &rpcserver.CloseWalletParams{AssetID: assetID}, nil
}

func buildToggleWalletStatus(args []string) (*rpcserver.ToggleWalletStatusParams, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("need assetID and disable")
	}
	assetID, err := parseUint32(args[0])
	if err != nil {
		return nil, fmt.Errorf("bad assetID: %w", err)
	}
	disable, err := parseBool(args[1])
	if err != nil {
		return nil, fmt.Errorf("bad disable: %w", err)
	}
	return &rpcserver.ToggleWalletStatusParams{AssetID: assetID, Disable: disable}, nil
}

func buildRescanWallet(args []string) (*rpcserver.RescanWalletParams, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("missing assetID")
	}
	assetID, err := parseUint32(args[0])
	if err != nil {
		return nil, fmt.Errorf("bad assetID: %w", err)
	}
	p := &rpcserver.RescanWalletParams{AssetID: assetID}
	if s := optArg(args, 1); s != "" {
		v, err := parseBool(s)
		if err != nil {
			return nil, fmt.Errorf("bad force: %w", err)
		}
		p.Force = v
	}
	return p, nil
}

// ---------------------------------------------------------------------------
// Trading builders
// ---------------------------------------------------------------------------

func buildTrade(pws []encode.PassBytes, args []string) (*rpcserver.TradeParams, error) {
	if len(pws) < 1 {
		return nil, fmt.Errorf("missing app password")
	}
	if len(args) < 8 {
		return nil, fmt.Errorf("need host, isLimit, sell, base, quote, qty, rate, tifNow")
	}
	isLimit, err := parseBool(args[1])
	if err != nil {
		return nil, fmt.Errorf("bad isLimit: %w", err)
	}
	sell, err := parseBool(args[2])
	if err != nil {
		return nil, fmt.Errorf("bad sell: %w", err)
	}
	base, err := parseUint32(args[3])
	if err != nil {
		return nil, fmt.Errorf("bad base: %w", err)
	}
	quote, err := parseUint32(args[4])
	if err != nil {
		return nil, fmt.Errorf("bad quote: %w", err)
	}
	qty, err := parseUint64(args[5])
	if err != nil {
		return nil, fmt.Errorf("bad qty: %w", err)
	}
	rate, err := parseUint64(args[6])
	if err != nil {
		return nil, fmt.Errorf("bad rate: %w", err)
	}
	tifNow, err := parseBool(args[7])
	if err != nil {
		return nil, fmt.Errorf("bad tifNow: %w", err)
	}
	p := &rpcserver.TradeParams{
		AppPass: pws[0],
		TradeForm: core.TradeForm{
			Host:    args[0],
			IsLimit: isLimit,
			Sell:    sell,
			Base:    base,
			Quote:   quote,
			Qty:     qty,
			Rate:    rate,
			TifNow:  tifNow,
		},
	}
	if s := optArg(args, 8); s != "" {
		opts, err := parseMapArg(s)
		if err != nil {
			return nil, fmt.Errorf("bad options: %w", err)
		}
		p.Options = opts
	}
	return p, nil
}

func buildMultiTrade(pws []encode.PassBytes, args []string) (*rpcserver.MultiTradeParams, error) {
	if len(pws) < 1 {
		return nil, fmt.Errorf("missing app password")
	}
	if len(args) < 7 {
		return nil, fmt.Errorf("need host, sell, base, quote, maxLock, placements, options")
	}
	sell, err := parseBool(args[1])
	if err != nil {
		return nil, fmt.Errorf("bad sell: %w", err)
	}
	base, err := parseUint32(args[2])
	if err != nil {
		return nil, fmt.Errorf("bad base: %w", err)
	}
	quote, err := parseUint32(args[3])
	if err != nil {
		return nil, fmt.Errorf("bad quote: %w", err)
	}
	maxLock, err := parseUint64(args[4])
	if err != nil {
		return nil, fmt.Errorf("bad maxLock: %w", err)
	}
	// Placements: JSON-encoded [[qty,rate], ...].
	var rawPlacements [][2]uint64
	if err := json.Unmarshal([]byte(args[5]), &rawPlacements); err != nil {
		return nil, fmt.Errorf("bad placements JSON: %w", err)
	}
	placements := make([]*core.QtyRate, len(rawPlacements))
	for i, rp := range rawPlacements {
		placements[i] = &core.QtyRate{Qty: rp[0], Rate: rp[1]}
	}
	opts, err := parseMapArg(args[6])
	if err != nil {
		return nil, fmt.Errorf("bad options: %w", err)
	}
	return &rpcserver.MultiTradeParams{
		AppPass: pws[0],
		MultiTradeForm: core.MultiTradeForm{
			Host:       args[0],
			Sell:       sell,
			Base:       base,
			Quote:      quote,
			MaxLock:    maxLock,
			Placements: placements,
			Options:    opts,
		},
	}, nil
}

func buildCancel(args []string) (*rpcserver.CancelParams, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("missing orderID")
	}
	return &rpcserver.CancelParams{OrderID: args[0]}, nil
}

func buildOrderBook(args []string) (*rpcserver.OrderBookParams, error) {
	if len(args) < 3 {
		return nil, fmt.Errorf("need host, base, quote")
	}
	base, err := parseUint32(args[1])
	if err != nil {
		return nil, fmt.Errorf("bad base: %w", err)
	}
	quote, err := parseUint32(args[2])
	if err != nil {
		return nil, fmt.Errorf("bad quote: %w", err)
	}
	p := &rpcserver.OrderBookParams{
		Host:  args[0],
		Base:  base,
		Quote: quote,
	}
	if s := optArg(args, 3); s != "" {
		n, err := parseUint64(s)
		if err != nil {
			return nil, fmt.Errorf("bad nOrders: %w", err)
		}
		p.NOrders = n
	}
	return p, nil
}

func buildMyOrders(args []string) (*rpcserver.MyOrdersParams, error) {
	p := &rpcserver.MyOrdersParams{
		Host: optArg(args, 0),
	}
	if s := optArg(args, 1); s != "" {
		v, err := parseUint32(s)
		if err != nil {
			return nil, fmt.Errorf("bad base: %w", err)
		}
		p.Base = &v
	}
	if s := optArg(args, 2); s != "" {
		v, err := parseUint32(s)
		if err != nil {
			return nil, fmt.Errorf("bad quote: %w", err)
		}
		p.Quote = &v
	}
	return p, nil
}

// ---------------------------------------------------------------------------
// Transaction builders
// ---------------------------------------------------------------------------

func buildSend(pws []encode.PassBytes, args []string) (*rpcserver.SendParams, error) {
	if len(pws) < 1 {
		return nil, fmt.Errorf("missing app password")
	}
	if len(args) < 3 {
		return nil, fmt.Errorf("need assetID, value, address")
	}
	assetID, err := parseUint32(args[0])
	if err != nil {
		return nil, fmt.Errorf("bad assetID: %w", err)
	}
	value, err := parseUint64(args[1])
	if err != nil {
		return nil, fmt.Errorf("bad value: %w", err)
	}
	return &rpcserver.SendParams{
		AppPass: pws[0],
		AssetID: assetID,
		Value:   value,
		Address: args[2],
	}, nil
}

func buildWithdraw(pws []encode.PassBytes, args []string) (*rpcserver.SendParams, error) {
	p, err := buildSend(pws, args)
	if err != nil {
		return nil, err
	}
	p.Subtract = true
	return p, nil
}

func buildAbandonTx(args []string) (*rpcserver.AbandonTxParams, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("need assetID and txID")
	}
	assetID, err := parseUint32(args[0])
	if err != nil {
		return nil, fmt.Errorf("bad assetID: %w", err)
	}
	return &rpcserver.AbandonTxParams{AssetID: assetID, TxID: args[1]}, nil
}

func buildAppSeed(pws []encode.PassBytes) (*rpcserver.AppSeedParams, error) {
	if len(pws) < 1 {
		return nil, fmt.Errorf("missing app password")
	}
	return &rpcserver.AppSeedParams{AppPass: pws[0]}, nil
}

func buildDeleteRecords(args []string) (*rpcserver.DeleteRecordsParams, error) {
	p := &rpcserver.DeleteRecordsParams{}
	if s := optArg(args, 0); s != "" {
		v, err := parseInt64(s)
		if err != nil {
			return nil, fmt.Errorf("bad olderThanMs: %w", err)
		}
		p.OlderThanMs = &v
	}
	p.MatchesFile = optArg(args, 1)
	p.OrdersFile = optArg(args, 2)
	return p, nil
}

func buildNotifications(args []string) (*rpcserver.NotificationsParams, error) {
	p := &rpcserver.NotificationsParams{}
	if s := optArg(args, 0); s != "" {
		n, err := parseInt(s)
		if err != nil {
			return nil, fmt.Errorf("bad n: %w", err)
		}
		p.N = n
	}
	return p, nil
}

func buildTxHistory(args []string) (*rpcserver.TxHistoryParams, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("missing assetID")
	}
	assetID, err := parseUint32(args[0])
	if err != nil {
		return nil, fmt.Errorf("bad assetID: %w", err)
	}
	p := &rpcserver.TxHistoryParams{AssetID: assetID}
	if s := optArg(args, 1); s != "" {
		n, err := parseInt(s)
		if err != nil {
			return nil, fmt.Errorf("bad n: %w", err)
		}
		p.N = n
	}
	if s := optArg(args, 2); s != "" {
		p.RefID = &s
	}
	if s := optArg(args, 3); s != "" {
		v, err := parseBool(s)
		if err != nil {
			return nil, fmt.Errorf("bad past: %w", err)
		}
		p.Past = v
	}
	return p, nil
}

func buildWalletTx(args []string) (*rpcserver.WalletTxParams, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("need assetID and txID")
	}
	assetID, err := parseUint32(args[0])
	if err != nil {
		return nil, fmt.Errorf("bad assetID: %w", err)
	}
	return &rpcserver.WalletTxParams{AssetID: assetID, TxID: args[1]}, nil
}

func buildWithdrawBchSpv(pws []encode.PassBytes, args []string) (*rpcserver.BchWithdrawParams, error) {
	if len(pws) < 1 {
		return nil, fmt.Errorf("missing app password")
	}
	if len(args) < 1 {
		return nil, fmt.Errorf("missing recipient")
	}
	return &rpcserver.BchWithdrawParams{AppPass: pws[0], Recipient: args[0]}, nil
}

// ---------------------------------------------------------------------------
// DEX builders
// ---------------------------------------------------------------------------

func buildDiscoverAcct(pws []encode.PassBytes, args []string) (*rpcserver.DiscoverAcctParams, error) {
	if len(pws) < 1 {
		return nil, fmt.Errorf("missing app password")
	}
	if len(args) < 1 {
		return nil, fmt.Errorf("missing addr")
	}
	return &rpcserver.DiscoverAcctParams{
		AppPass: pws[0],
		Addr:    args[0],
		Cert:    optArg(args, 1),
	}, nil
}

func buildGetDEXConfig(args []string) (*rpcserver.GetDEXConfigParams, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("missing host")
	}
	return &rpcserver.GetDEXConfigParams{
		Host: args[0],
		Cert: optArg(args, 1),
	}, nil
}

func buildBondAssets(args []string) (*rpcserver.BondAssetsParams, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("missing host")
	}
	return &rpcserver.BondAssetsParams{
		Host: args[0],
		Cert: optArg(args, 1),
	}, nil
}

func buildBondOpts(args []string) (*core.BondOptionsForm, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("need addr and targetTier")
	}
	targetTier, err := parseUint64(args[1])
	if err != nil {
		return nil, fmt.Errorf("bad targetTier: %w", err)
	}
	p := &core.BondOptionsForm{
		Host:       args[0],
		TargetTier: &targetTier,
	}
	if s := optArg(args, 2); s != "" {
		v, err := parseUint64(s)
		if err != nil {
			return nil, fmt.Errorf("bad maxBondedAmt: %w", err)
		}
		p.MaxBondedAmt = &v
	}
	if s := optArg(args, 3); s != "" {
		v, err := parseUint32(s)
		if err != nil {
			return nil, fmt.Errorf("bad bondAssetID: %w", err)
		}
		p.BondAssetID = &v
	}
	if s := optArg(args, 4); s != "" {
		v64, err := strconv.ParseUint(s, 10, 16)
		if err != nil {
			return nil, fmt.Errorf("bad penaltyComps: %w", err)
		}
		v := uint16(v64)
		p.PenaltyComps = &v
	}
	return p, nil
}

func buildPostBond(pws []encode.PassBytes, args []string) (*core.PostBondForm, error) {
	if len(pws) < 1 {
		return nil, fmt.Errorf("missing app password")
	}
	if len(args) < 3 {
		return nil, fmt.Errorf("need addr, bond, assetID")
	}
	bond, err := parseUint64(args[1])
	if err != nil {
		return nil, fmt.Errorf("bad bond: %w", err)
	}
	assetID, err := parseUint32(args[2])
	if err != nil {
		return nil, fmt.Errorf("bad assetID: %w", err)
	}
	p := &core.PostBondForm{
		Addr:    args[0],
		AppPass: pws[0],
		Bond:    bond,
		Asset:   &assetID,
	}
	// maintain defaults to true.
	maintainTier := true
	if s := optArg(args, 3); s != "" {
		maintainTier, err = parseBool(s)
		if err != nil {
			return nil, fmt.Errorf("bad maintain: %w", err)
		}
	}
	p.MaintainTier = &maintainTier
	if s := optArg(args, 4); s != "" {
		p.Cert = s
	}
	return p, nil
}

// ---------------------------------------------------------------------------
// Market Making builders
// ---------------------------------------------------------------------------

func buildMMAvailableBalances(args []string) (*rpcserver.MMAvailableBalancesParams, error) {
	if len(args) < 3 {
		return nil, fmt.Errorf("need host, baseID, quoteID")
	}
	baseID, err := parseUint32(args[1])
	if err != nil {
		return nil, fmt.Errorf("bad baseID: %w", err)
	}
	quoteID, err := parseUint32(args[2])
	if err != nil {
		return nil, fmt.Errorf("bad quoteID: %w", err)
	}
	p := &rpcserver.MMAvailableBalancesParams{
		MarketWithHost: mm.MarketWithHost{
			Host:    args[0],
			BaseID:  baseID,
			QuoteID: quoteID,
		},
	}
	if s := optArg(args, 3); s != "" {
		v, err := parseUint32(s)
		if err != nil {
			return nil, fmt.Errorf("bad cexBaseID: %w", err)
		}
		p.CexBaseID = v
	}
	if s := optArg(args, 4); s != "" {
		v, err := parseUint32(s)
		if err != nil {
			return nil, fmt.Errorf("bad cexQuoteID: %w", err)
		}
		p.CexQuoteID = v
	}
	if s := optArg(args, 5); s != "" {
		p.CexName = &s
	}
	return p, nil
}

func buildStartBot(pws []encode.PassBytes, args []string) (*rpcserver.StartBotParams, error) {
	if len(pws) < 1 {
		return nil, fmt.Errorf("missing app password")
	}
	if len(args) < 1 {
		return nil, fmt.Errorf("missing cfgPath")
	}
	p := &rpcserver.StartBotParams{
		AppPass:     pws[0],
		CfgFilePath: args[0],
	}
	// Optional market filter: host, baseID, quoteID.
	if len(args) >= 4 {
		baseID, err := parseUint32(args[2])
		if err != nil {
			return nil, fmt.Errorf("bad baseID: %w", err)
		}
		quoteID, err := parseUint32(args[3])
		if err != nil {
			return nil, fmt.Errorf("bad quoteID: %w", err)
		}
		p.Market = &mm.MarketWithHost{
			Host:    args[1],
			BaseID:  baseID,
			QuoteID: quoteID,
		}
	}
	return p, nil
}

func buildStopBot(args []string) (*rpcserver.StopBotParams, error) {
	p := &rpcserver.StopBotParams{}
	if len(args) >= 3 {
		baseID, err := parseUint32(args[1])
		if err != nil {
			return nil, fmt.Errorf("bad baseID: %w", err)
		}
		quoteID, err := parseUint32(args[2])
		if err != nil {
			return nil, fmt.Errorf("bad quoteID: %w", err)
		}
		p.Market = &mm.MarketWithHost{
			Host:    args[0],
			BaseID:  baseID,
			QuoteID: quoteID,
		}
	}
	return p, nil
}

func buildUpdateRunningBotCfg(args []string) (*rpcserver.UpdateRunningBotParams, error) {
	if len(args) < 4 {
		return nil, fmt.Errorf("need cfgPath, host, baseID, quoteID")
	}
	baseID, err := parseUint32(args[2])
	if err != nil {
		return nil, fmt.Errorf("bad baseID: %w", err)
	}
	quoteID, err := parseUint32(args[3])
	if err != nil {
		return nil, fmt.Errorf("bad quoteID: %w", err)
	}
	p := &rpcserver.UpdateRunningBotParams{
		CfgFilePath: args[0],
		Market: mm.MarketWithHost{
			Host:    args[1],
			BaseID:  baseID,
			QuoteID: quoteID,
		},
	}
	// Optional inventory diffs: dexInventory, cexInventory.
	if s := optArg(args, 4); s != "" {
		dexInv, err := parseInventoryArg(s)
		if err != nil {
			return nil, fmt.Errorf("bad dexInventory: %w", err)
		}
		cexInv, err := parseInventoryArg(optArg(args, 5))
		if err != nil {
			return nil, fmt.Errorf("bad cexInventory: %w", err)
		}
		p.Balances = &mm.BotInventoryDiffs{
			DEX: dexInv,
			CEX: cexInv,
		}
	}
	return p, nil
}

func buildUpdateRunningBotInv(args []string) (*rpcserver.UpdateRunningBotInventoryParams, error) {
	if len(args) < 5 {
		return nil, fmt.Errorf("need host, baseID, quoteID, dexInventory, cexInventory")
	}
	baseID, err := parseUint32(args[1])
	if err != nil {
		return nil, fmt.Errorf("bad baseID: %w", err)
	}
	quoteID, err := parseUint32(args[2])
	if err != nil {
		return nil, fmt.Errorf("bad quoteID: %w", err)
	}
	dexInv, err := parseInventoryArg(args[3])
	if err != nil {
		return nil, fmt.Errorf("bad dexInventory: %w", err)
	}
	cexInv, err := parseInventoryArg(args[4])
	if err != nil {
		return nil, fmt.Errorf("bad cexInventory: %w", err)
	}
	return &rpcserver.UpdateRunningBotInventoryParams{
		Market: mm.MarketWithHost{
			Host:    args[0],
			BaseID:  baseID,
			QuoteID: quoteID,
		},
		Balances: &mm.BotInventoryDiffs{
			DEX: dexInv,
			CEX: cexInv,
		},
	}, nil
}

// ---------------------------------------------------------------------------
// Staking builders
// ---------------------------------------------------------------------------

func buildStakeStatus(args []string) (*rpcserver.StakeStatusParams, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("missing assetID")
	}
	assetID, err := parseUint32(args[0])
	if err != nil {
		return nil, fmt.Errorf("bad assetID: %w", err)
	}
	return &rpcserver.StakeStatusParams{AssetID: assetID}, nil
}

func buildSetVSP(args []string) (*rpcserver.SetVSPParams, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("need assetID and addr")
	}
	assetID, err := parseUint32(args[0])
	if err != nil {
		return nil, fmt.Errorf("bad assetID: %w", err)
	}
	return &rpcserver.SetVSPParams{AssetID: assetID, Addr: args[1]}, nil
}

func buildPurchaseTickets(pws []encode.PassBytes, args []string) (*rpcserver.PurchaseTicketsParams, error) {
	if len(pws) < 1 {
		return nil, fmt.Errorf("missing app password")
	}
	if len(args) < 2 {
		return nil, fmt.Errorf("need assetID and n")
	}
	assetID, err := parseUint32(args[0])
	if err != nil {
		return nil, fmt.Errorf("bad assetID: %w", err)
	}
	n, err := parseInt(args[1])
	if err != nil {
		return nil, fmt.Errorf("bad n: %w", err)
	}
	return &rpcserver.PurchaseTicketsParams{AppPass: pws[0], AssetID: assetID, N: n}, nil
}

func buildSetVotingPreferences(args []string) (*rpcserver.SetVotingPreferencesParams, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("missing assetID")
	}
	assetID, err := parseUint32(args[0])
	if err != nil {
		return nil, fmt.Errorf("bad assetID: %w", err)
	}
	p := &rpcserver.SetVotingPreferencesParams{AssetID: assetID}
	if s := optArg(args, 1); s != "" {
		p.Choices, err = parseMapArg(s)
		if err != nil {
			return nil, fmt.Errorf("bad choices: %w", err)
		}
	}
	if s := optArg(args, 2); s != "" {
		p.TSpendPolicy, err = parseMapArg(s)
		if err != nil {
			return nil, fmt.Errorf("bad tSpendPolicy: %w", err)
		}
	}
	if s := optArg(args, 3); s != "" {
		p.TreasuryPolicy, err = parseMapArg(s)
		if err != nil {
			return nil, fmt.Errorf("bad treasuryPolicy: %w", err)
		}
	}
	return p, nil
}

// ---------------------------------------------------------------------------
// Bridge builders
// ---------------------------------------------------------------------------

func buildBridge(args []string) (*rpcserver.BridgeParams, error) {
	if len(args) < 4 {
		return nil, fmt.Errorf("need fromAssetID, toAssetID, amt, bridgeName")
	}
	fromAssetID, err := parseUint32(args[0])
	if err != nil {
		return nil, fmt.Errorf("bad fromAssetID: %w", err)
	}
	toAssetID, err := parseUint32(args[1])
	if err != nil {
		return nil, fmt.Errorf("bad toAssetID: %w", err)
	}
	amt, err := parseUint64(args[2])
	if err != nil {
		return nil, fmt.Errorf("bad amt: %w", err)
	}
	return &rpcserver.BridgeParams{
		FromAssetID: fromAssetID,
		ToAssetID:   toAssetID,
		Amt:         amt,
		BridgeName:  args[3],
	}, nil
}

func buildCheckBridgeApproval(args []string) (*rpcserver.CheckBridgeApprovalParams, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("need assetID and bridgeName")
	}
	assetID, err := parseUint32(args[0])
	if err != nil {
		return nil, fmt.Errorf("bad assetID: %w", err)
	}
	return &rpcserver.CheckBridgeApprovalParams{AssetID: assetID, BridgeName: args[1]}, nil
}

func buildApproveBridge(args []string) (*rpcserver.ApproveBridgeParams, error) {
	if len(args) < 3 {
		return nil, fmt.Errorf("need assetID, bridgeName, approve")
	}
	assetID, err := parseUint32(args[0])
	if err != nil {
		return nil, fmt.Errorf("bad assetID: %w", err)
	}
	approve, err := parseBool(args[2])
	if err != nil {
		return nil, fmt.Errorf("bad approve: %w", err)
	}
	return &rpcserver.ApproveBridgeParams{AssetID: assetID, BridgeName: args[1], Approve: approve}, nil
}

func buildPendingBridges(args []string) (*rpcserver.PendingBridgesParams, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("missing assetID")
	}
	assetID, err := parseUint32(args[0])
	if err != nil {
		return nil, fmt.Errorf("bad assetID: %w", err)
	}
	return &rpcserver.PendingBridgesParams{AssetID: assetID}, nil
}

func buildBridgeHistory(args []string) (*rpcserver.TxHistoryParams, error) {
	return buildTxHistory(args)
}

func buildSupportedBridges(args []string) (*rpcserver.SupportedBridgesParams, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("missing assetID")
	}
	assetID, err := parseUint32(args[0])
	if err != nil {
		return nil, fmt.Errorf("bad assetID: %w", err)
	}
	return &rpcserver.SupportedBridgesParams{AssetID: assetID}, nil
}

func buildBridgeFeesAndLimits(args []string) (*rpcserver.BridgeFeesAndLimitsParams, error) {
	if len(args) < 3 {
		return nil, fmt.Errorf("need fromAssetID, toAssetID, bridgeName")
	}
	fromAssetID, err := parseUint32(args[0])
	if err != nil {
		return nil, fmt.Errorf("bad fromAssetID: %w", err)
	}
	toAssetID, err := parseUint32(args[1])
	if err != nil {
		return nil, fmt.Errorf("bad toAssetID: %w", err)
	}
	return &rpcserver.BridgeFeesAndLimitsParams{
		FromAssetID: fromAssetID,
		ToAssetID:   toAssetID,
		BridgeName:  args[2],
	}, nil
}

// ---------------------------------------------------------------------------
// Multisig builders
// ---------------------------------------------------------------------------

func buildPaymentMultisigPubkey(args []string) (*rpcserver.PaymentMultisigPubkeyParams, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("missing assetID")
	}
	assetID, err := parseUint32(args[0])
	if err != nil {
		return nil, fmt.Errorf("bad assetID: %w", err)
	}
	return &rpcserver.PaymentMultisigPubkeyParams{AssetID: assetID}, nil
}

func buildCsvFile(args []string) (*rpcserver.CsvFileParams, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("missing csvFilePath")
	}
	return &rpcserver.CsvFileParams{CsvFilePath: args[0]}, nil
}

func buildSignMultisig(args []string) (*rpcserver.SignMultisigParams, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("need csvFilePath and sigIndex")
	}
	sigIdx, err := parseInt(args[1])
	if err != nil {
		return nil, fmt.Errorf("bad sigIndex: %w", err)
	}
	return &rpcserver.SignMultisigParams{CsvFilePath: args[0], SignIdx: sigIdx}, nil
}

// ---------------------------------------------------------------------------
// Peers builders
// ---------------------------------------------------------------------------

func buildWalletPeers(args []string) (*rpcserver.WalletPeersParams, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("missing assetID")
	}
	assetID, err := parseUint32(args[0])
	if err != nil {
		return nil, fmt.Errorf("bad assetID: %w", err)
	}
	return &rpcserver.WalletPeersParams{AssetID: assetID}, nil
}

func buildAddRemovePeer(args []string) (*rpcserver.AddRemovePeerParams, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("need assetID and address")
	}
	assetID, err := parseUint32(args[0])
	if err != nil {
		return nil, fmt.Errorf("bad assetID: %w", err)
	}
	return &rpcserver.AddRemovePeerParams{AssetID: assetID, Address: args[1]}, nil
}
