// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package rpcserver

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/client/mm"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/order"
)

// routes
const (
	cancelRoute                = "cancel"
	closeWalletRoute           = "closewallet"
	deployContractRoute        = "deploycontract"
	discoverAcctRoute          = "discoveracct"
	exchangesRoute             = "exchanges"
	helpRoute                  = "help"
	initRoute                  = "init"
	loginRoute                 = "login"
	logoutRoute                = "logout"
	myOrdersRoute              = "myorders"
	newWalletRoute             = "newwallet"
	openWalletRoute            = "openwallet"
	toggleWalletStatusRoute    = "togglewalletstatus"
	orderBookRoute             = "orderbook"
	getDEXConfRoute            = "getdexconfig"
	bondAssetsRoute            = "bondassets"
	postBondRoute              = "postbond"
	bondOptionsRoute           = "bondopts"
	tradeRoute                 = "trade"
	versionRoute               = "version"
	walletBalanceRoute         = "walletbalance"
	walletStateRoute           = "walletstate"
	walletsRoute               = "wallets"
	rescanWalletRoute          = "rescanwallet"
	abandonTxRoute             = "abandontx"
	withdrawRoute              = "withdraw"
	sendRoute                  = "send"
	appSeedRoute               = "appseed"
	deleteArchivedRecordsRoute = "deletearchivedrecords"
	walletPeersRoute           = "walletpeers"
	addWalletPeerRoute         = "addwalletpeer"
	removeWalletPeerRoute      = "removewalletpeer"
	notificationsRoute         = "notifications"
	startBotRoute              = "startmmbot"
	stopBotRoute               = "stopmmbot"
	updateRunningBotCfgRoute   = "updaterunningbotcfg"
	updateRunningBotInvRoute   = "updaterunningbotinv"
	mmAvailableBalancesRoute   = "mmavailablebalances"
	mmStatusRoute              = "mmstatus"
	multiTradeRoute            = "multitrade"
	stakeStatusRoute           = "stakestatus"
	setVSPRoute                = "setvsp"
	purchaseTicketsRoute       = "purchasetickets"
	setVotingPreferencesRoute  = "setvotingprefs"
	txHistoryRoute             = "txhistory"
	walletTxRoute              = "wallettx"
	withdrawBchSpvRoute        = "withdrawbchspv"
	bridgeRoute                = "bridge"
	checkBridgeApprovalRoute   = "checkbridgeapproval"
	approveBridgeContractRoute = "approvebridgecontract"
	pendingBridgesRoute        = "pendingbridges"
	bridgeHistoryRoute         = "bridgehistory"
	supportedBridgesRoute      = "supportedbridges"
	bridgeFeesAndLimitsRoute   = "bridgefeesandlimits"
	paymentMultisigPubkeyRoute = "paymentmultisigpubkey"
	sendFundsToMultisigRoute   = "sendfundstomultisig"
	signMultisigRoute          = "signmultisig"
	refundPaymentMultisigRoute = "refundpaymentmultisig"
	viewPaymentMultisigRoute   = "viewpaymentmultisig"
	sendPaymentMultisigRoute   = "sendpaymentmultisig"
	mmReportRoute              = "mmreport"
	pruneMMSnapshotsRoute      = "prunemmsnapshots"
)

const (
	initializedStr    = "app initialized"
	walletCreatedStr  = "%s wallet created and unlocked"
	walletLockedStr   = "%s wallet locked"
	walletUnlockedStr = "%s wallet unlocked"
	canceledOrderStr  = "canceled order %s"
	logoutStr         = "goodbye"
	walletStatusStr   = "%s wallet has been %s"
	setVotePrefsStr   = "vote preferences set"
	setVSPStr         = "vsp set to %s"
)

// createResponse creates a msgjson response payload.
func createResponse(op string, res any, resErr *msgjson.Error) *msgjson.ResponsePayload {
	encodedRes, err := json.Marshal(res)
	if err != nil {
		log.Errorf("unable to marshal data for %s: %v", op, err)
		errMsg := msgjson.NewError(msgjson.RPCInternal, "unable to marshal response for %s", op)
		return &msgjson.ResponsePayload{Error: errMsg}
	}
	return &msgjson.ResponsePayload{Result: encodedRes, Error: resErr}
}

// usage creates and returns usage for route combined with a passed error as a
// *msgjson.ResponsePayload.
func usage(route string, err error) *msgjson.ResponsePayload {
	usage, _ := commandUsage(route, false)
	resErr := msgjson.NewError(msgjson.RPCArgumentsError, "%v\n\n%s", err, usage)
	return createResponse(route, nil, resErr)
}

// routes maps routes to a handler function.
var routes = map[string]func(s *RPCServer, msg *msgjson.Message) *msgjson.ResponsePayload{
	cancelRoute:                handleCancel,
	closeWalletRoute:           handleCloseWallet,
	deployContractRoute:        handleDeployContract,
	discoverAcctRoute:          handleDiscoverAcct,
	exchangesRoute:             handleExchanges,
	helpRoute:                  handleHelp,
	initRoute:                  handleInit,
	loginRoute:                 handleLogin,
	logoutRoute:                handleLogout,
	myOrdersRoute:              handleMyOrders,
	newWalletRoute:             handleNewWallet,
	openWalletRoute:            handleOpenWallet,
	toggleWalletStatusRoute:    handleToggleWalletStatus,
	orderBookRoute:             handleOrderBook,
	getDEXConfRoute:            handleGetDEXConfig,
	postBondRoute:              handlePostBond,
	bondOptionsRoute:           handleBondOptions,
	bondAssetsRoute:            handleBondAssets,
	tradeRoute:                 handleTrade,
	versionRoute:               handleVersion,
	walletBalanceRoute:         handleWalletBalance,
	walletStateRoute:           handleWalletState,
	walletsRoute:               handleWallets,
	rescanWalletRoute:          handleRescanWallet,
	abandonTxRoute:             handleAbandonTx,
	withdrawRoute:              handleWithdraw,
	sendRoute:                  handleSend,
	appSeedRoute:               handleAppSeed,
	deleteArchivedRecordsRoute: handleDeleteArchivedRecords,
	walletPeersRoute:           handleWalletPeers,
	addWalletPeerRoute:         handleAddWalletPeer,
	removeWalletPeerRoute:      handleRemoveWalletPeer,
	notificationsRoute:         handleNotifications,
	startBotRoute:              handleStartBot,
	stopBotRoute:               handleStopBot,
	mmAvailableBalancesRoute:   handleMMAvailableBalances,
	mmStatusRoute:              handleMMStatus,
	updateRunningBotCfgRoute:   handleUpdateRunningBotCfg,
	updateRunningBotInvRoute:   handleUpdateRunningBotInventory,
	multiTradeRoute:            handleMultiTrade,
	stakeStatusRoute:           handleStakeStatus,
	setVSPRoute:                handleSetVSP,
	purchaseTicketsRoute:       handlePurchaseTickets,
	setVotingPreferencesRoute:  handleSetVotingPreferences,
	txHistoryRoute:             handleTxHistory,
	walletTxRoute:              handleWalletTx,
	withdrawBchSpvRoute:        handleWithdrawBchSpv,
	bridgeRoute:                handleBridge,
	checkBridgeApprovalRoute:   handleCheckBridgeApproval,
	approveBridgeContractRoute: handleApproveBridge,
	pendingBridgesRoute:        handlePendingBridges,
	bridgeHistoryRoute:         handleBridgeHistory,
	supportedBridgesRoute:      handleSupportedBridges,
	bridgeFeesAndLimitsRoute:   handleBridgeFeesAndLimits,
	paymentMultisigPubkeyRoute: handlePaymentMultisigPubkey,
	sendFundsToMultisigRoute:   handleSendFundsToMultisig,
	signMultisigRoute:          handleSignMultisig,
	refundPaymentMultisigRoute: handleRefundPaymentMultisig,
	viewPaymentMultisigRoute:   handleViewPaymentMultisig,
	sendPaymentMultisigRoute:   handleSendPaymentMultisig,
	mmReportRoute:              handleMMReport,
	pruneMMSnapshotsRoute:      handlePruneMMSnapshots,
}

//
// handlers_system handlers
//

// handleHelp handles requests for help. Returns general help for all commands
// if no arguments are passed or verbose help if the passed argument is a known
// command.
func handleHelp(_ *RPCServer, msg *msgjson.Message) *msgjson.ResponsePayload {
	var params HelpParams
	if err := msg.Unmarshal(&params); err != nil {
		return usage(helpRoute, err)
	}
	res := ""
	if params.HelpWith == "" {
		// List all commands if no arguments.
		res = ListCommands(params.IncludePasswords)
	} else {
		var err error
		res, err = commandUsage(params.HelpWith, params.IncludePasswords)
		if err != nil {
			resErr := msgjson.NewError(msgjson.RPCUnknownRoute, "error getting usage: %v", err)
			return createResponse(helpRoute, nil, resErr)
		}
	}
	return createResponse(helpRoute, &res, nil)
}

// handleInit handles requests for init. *msgjson.ResponsePayload.Error is empty
// if successful.
func handleInit(s *RPCServer, msg *msgjson.Message) *msgjson.ResponsePayload {
	var params InitParams
	if err := msg.Unmarshal(&params); err != nil {
		return usage(initRoute, err)
	}
	defer params.AppPass.Clear()
	if _, err := s.core.InitializeClient(params.AppPass, params.Seed); err != nil {
		resErr := msgjson.NewError(msgjson.RPCInitError, "unable to initialize client: %v", err)
		return createResponse(initRoute, nil, resErr)
	}
	res := initializedStr
	return createResponse(initRoute, &res, nil)
}

// handleVersion handles requests for version. It returns the rpc server version
// and dexc version.
func handleVersion(s *RPCServer, _ *msgjson.Message) *msgjson.ResponsePayload {
	result := &VersionResponse{
		RPCServerVer: &dex.Semver{
			Major: rpcSemverMajor,
			Minor: rpcSemverMinor,
			Patch: rpcSemverPatch,
		},
		BWVersion: s.bwVersion,
	}

	return createResponse(versionRoute, result, nil)
}

// handleLogin sets up the dex connections.
func handleLogin(s *RPCServer, msg *msgjson.Message) *msgjson.ResponsePayload {
	var params LoginParams
	if err := msg.Unmarshal(&params); err != nil {
		return usage(loginRoute, err)
	}
	defer params.AppPass.Clear()
	err := s.core.Login(params.AppPass)
	if err != nil {
		resErr := msgjson.NewError(msgjson.RPCLoginError, "unable to login: %v", err)
		return createResponse(loginRoute, nil, resErr)
	}
	res := "successfully logged in"
	return createResponse(loginRoute, &res, nil)
}

// handleLogout logs out Bison Wallet. *msgjson.ResponsePayload.Error is empty
// if successful.
func handleLogout(s *RPCServer, _ *msgjson.Message) *msgjson.ResponsePayload {
	if err := s.core.Logout(); err != nil {
		resErr := msgjson.NewError(msgjson.RPCLogoutError, "unable to logout: %v", err)
		return createResponse(logoutRoute, nil, resErr)
	}
	res := logoutStr
	return createResponse(logoutRoute, &res, nil)
}

//
// handlers_wallet handlers
//

// handleNewWallet handles requests for newwallet.
// *msgjson.ResponsePayload.Error is empty if successful. Returns a
// msgjson.RPCWalletExistsError if a wallet for the assetID already exists.
// Wallet will be unlocked if successful.
func handleNewWallet(s *RPCServer, msg *msgjson.Message) *msgjson.ResponsePayload {
	var params NewWalletParams
	if err := msg.Unmarshal(&params); err != nil {
		return usage(newWalletRoute, err)
	}

	// zero password params in request payload when done handling this request
	defer func() {
		params.AppPass.Clear()
		params.WalletPass.Clear()
	}()

	if s.core.WalletState(params.AssetID) != nil {
		resErr := msgjson.NewError(msgjson.RPCWalletExistsError, "error creating %s wallet: wallet already exists", dex.BipIDSymbol(params.AssetID))
		return createResponse(newWalletRoute, nil, resErr)
	}

	walletDef, err := asset.WalletDef(params.AssetID, params.WalletType)
	if err != nil {
		resErr := msgjson.NewError(msgjson.RPCWalletDefinitionError, "error creating %s wallet: unable to get wallet definition: %v", dex.BipIDSymbol(params.AssetID), err)
		return createResponse(newWalletRoute, nil, resErr)
	}

	if params.Config == nil {
		params.Config = make(map[string]string)
	}

	// Apply default config options if they exist.
	for _, opt := range walletDef.ConfigOpts {
		if _, has := params.Config[opt.Key]; !has {
			params.Config[opt.Key] = opt.DefaultValue
		}
	}

	// Wallet does not exist yet. Try to create it.
	err = s.core.CreateWallet(params.AppPass, params.WalletPass, &core.WalletForm{
		Type:    params.WalletType,
		AssetID: params.AssetID,
		Config:  params.Config,
	})
	if err != nil {
		resErr := msgjson.NewError(msgjson.RPCCreateWalletError, "error creating %s wallet: %v", dex.BipIDSymbol(params.AssetID), err)
		return createResponse(newWalletRoute, nil, resErr)
	}

	err = s.core.OpenWallet(params.AssetID, params.AppPass)
	if err != nil {
		resErr := msgjson.NewError(msgjson.RPCOpenWalletError, "wallet connected, but failed to open with provided password: %v", err)
		return createResponse(newWalletRoute, nil, resErr)
	}

	res := fmt.Sprintf(walletCreatedStr, dex.BipIDSymbol(params.AssetID))
	return createResponse(newWalletRoute, &res, nil)
}

// handleOpenWallet handles requests for openWallet.
// *msgjson.ResponsePayload.Error is empty if successful. Requires the app
// password. Opens the wallet.
func handleOpenWallet(s *RPCServer, msg *msgjson.Message) *msgjson.ResponsePayload {
	var params OpenWalletParams
	if err := msg.Unmarshal(&params); err != nil {
		return usage(openWalletRoute, err)
	}
	defer params.AppPass.Clear()

	err := s.core.OpenWallet(params.AssetID, params.AppPass)
	if err != nil {
		resErr := msgjson.NewError(msgjson.RPCOpenWalletError, "error unlocking %s wallet: %v", dex.BipIDSymbol(params.AssetID), err)
		return createResponse(openWalletRoute, nil, resErr)
	}

	res := fmt.Sprintf(walletUnlockedStr, dex.BipIDSymbol(params.AssetID))
	return createResponse(openWalletRoute, &res, nil)
}

// handleCloseWallet handles requests for closeWallet.
// *msgjson.ResponsePayload.Error is empty if successful. Closes the wallet.
func handleCloseWallet(s *RPCServer, msg *msgjson.Message) *msgjson.ResponsePayload {
	var params CloseWalletParams
	if err := msg.Unmarshal(&params); err != nil {
		return usage(closeWalletRoute, err)
	}
	if err := s.core.CloseWallet(params.AssetID); err != nil {
		resErr := msgjson.NewError(msgjson.RPCCloseWalletError, "unable to close wallet %s: %v", dex.BipIDSymbol(params.AssetID), err)
		return createResponse(closeWalletRoute, nil, resErr)
	}

	res := fmt.Sprintf(walletLockedStr, dex.BipIDSymbol(params.AssetID))
	return createResponse(closeWalletRoute, &res, nil)
}

// handleToggleWalletStatus handles requests for toggleWalletStatus.
// *msgjson.ResponsePayload.Error is empty if successful. Disables or enables a
// wallet.
func handleToggleWalletStatus(s *RPCServer, msg *msgjson.Message) *msgjson.ResponsePayload {
	var params ToggleWalletStatusParams
	if err := msg.Unmarshal(&params); err != nil {
		return usage(toggleWalletStatusRoute, err)
	}
	if err := s.core.ToggleWalletStatus(params.AssetID, params.Disable); err != nil {
		resErr := msgjson.NewError(msgjson.RPCToggleWalletStatusError, "unable to change %s wallet status: %v", dex.BipIDSymbol(params.AssetID), err)
		return createResponse(toggleWalletStatusRoute, nil, resErr)
	}

	status := "enabled"
	if params.Disable {
		status = "disabled"
	}

	res := fmt.Sprintf(walletStatusStr, dex.BipIDSymbol(params.AssetID), status)
	return createResponse(toggleWalletStatusRoute, &res, nil)
}

// handleWallets handles requests for wallets. Returns a list of wallet details.
func handleWallets(s *RPCServer, _ *msgjson.Message) *msgjson.ResponsePayload {
	walletsStates := s.core.Wallets()
	return createResponse(walletsRoute, walletsStates, nil)
}

// handleWalletBalance handles requests for walletbalance. Returns the balance
// for a single wallet.
func handleWalletBalance(s *RPCServer, msg *msgjson.Message) *msgjson.ResponsePayload {
	var params WalletBalanceParams
	if err := msg.Unmarshal(&params); err != nil {
		return usage(walletBalanceRoute, err)
	}
	bal, err := s.core.AssetBalance(params.AssetID)
	if err != nil {
		resErr := msgjson.NewError(msgjson.RPCInternal, "unable to get balance for %s: %v", dex.BipIDSymbol(params.AssetID), err)
		return createResponse(walletBalanceRoute, nil, resErr)
	}
	return createResponse(walletBalanceRoute, bal, nil)
}

// handleWalletState handles requests for walletstate. Returns the state for a
// single wallet.
func handleWalletState(s *RPCServer, msg *msgjson.Message) *msgjson.ResponsePayload {
	var params WalletStateParams
	if err := msg.Unmarshal(&params); err != nil {
		return usage(walletStateRoute, err)
	}
	state := s.core.WalletState(params.AssetID)
	if state == nil {
		resErr := msgjson.NewError(msgjson.RPCWalletNotFoundError, "no wallet found for %s", dex.BipIDSymbol(params.AssetID))
		return createResponse(walletStateRoute, nil, resErr)
	}
	return createResponse(walletStateRoute, state, nil)
}

// handleRescanWallet handles requests to rescan a wallet. This may trigger an
// asynchronous resynchronization of wallet address activity, and the wallet
// state should be consulted for status. *msgjson.ResponsePayload.Error is empty
// if successful.
func handleRescanWallet(s *RPCServer, msg *msgjson.Message) *msgjson.ResponsePayload {
	var params RescanWalletParams
	if err := msg.Unmarshal(&params); err != nil {
		return usage(rescanWalletRoute, err)
	}
	err := s.core.RescanWallet(params.AssetID, params.Force)
	if err != nil {
		resErr := msgjson.NewError(msgjson.RPCWalletRescanError, "unable to rescan wallet: %v", err)
		return createResponse(rescanWalletRoute, nil, resErr)
	}
	return createResponse(rescanWalletRoute, "started", nil)
}

//
// handlers_trading handlers
//

// handleExchanges handles requests for exchanges. It takes no arguments and
// returns a map of exchanges.
func handleExchanges(s *RPCServer, _ *msgjson.Message) *msgjson.ResponsePayload {
	// Convert something to a map[string]any.
	convM := func(in any) (map[string]any, error) {
		var m map[string]any
		b, err := json.Marshal(in)
		if err != nil {
			return nil, err
		}
		if err = json.Unmarshal(b, &m); err != nil {
			return nil, err
		}
		return m, nil
	}
	res := s.core.Exchanges()
	exchanges, err := convM(res)
	if err != nil {
		resErr := msgjson.NewError(msgjson.RPCInternal, "failed to encode exchanges: %v", err)
		return createResponse(exchangesRoute, nil, resErr)
	}
	// Iterate through exchanges converting structs into maps in order to
	// remove some fields. Keys are DEX addresses.
	for k, exchange := range exchanges {
		exchangeDetails, err := convM(exchange)
		if err != nil {
			resErr := msgjson.NewError(msgjson.RPCInternal, "failed to encode exchange: %v", err)
			return createResponse(exchangesRoute, nil, resErr)
		}
		// Remove a redundant address field.
		delete(exchangeDetails, "host")
		markets, err := convM(exchangeDetails["markets"])
		if err != nil {
			resErr := msgjson.NewError(msgjson.RPCInternal, "failed to encode markets: %v", err)
			return createResponse(exchangesRoute, nil, resErr)
		}
		// Market keys are market name.
		for k, market := range markets {
			marketDetails, err := convM(market)
			if err != nil {
				resErr := msgjson.NewError(msgjson.RPCInternal, "failed to encode market: %v", err)
				return createResponse(exchangesRoute, nil, resErr)
			}
			// Remove redundant name field.
			delete(marketDetails, "name")
			delete(marketDetails, "orders")
			markets[k] = marketDetails
		}
		assets, err := convM(exchangeDetails["assets"])
		if err != nil {
			resErr := msgjson.NewError(msgjson.RPCInternal, "failed to encode assets: %v", err)
			return createResponse(exchangesRoute, nil, resErr)
		}
		// Asset keys are assetIDs.
		for k, asset := range assets {
			assetDetails, err := convM(asset)
			if err != nil {
				resErr := msgjson.NewError(msgjson.RPCInternal, "failed to encode asset: %v", err)
				return createResponse(exchangesRoute, nil, resErr)
			}
			// Remove redundant id field.
			delete(assetDetails, "id")
			assets[k] = assetDetails
		}
		exchangeDetails["markets"] = markets
		exchangeDetails["assets"] = assets
		exchanges[k] = exchangeDetails
	}
	return createResponse(exchangesRoute, &exchanges, nil)
}

// handleTrade handles requests for trade. *msgjson.ResponsePayload.Error is
// empty if successful.
func handleTrade(s *RPCServer, msg *msgjson.Message) *msgjson.ResponsePayload {
	var params TradeParams
	if err := msg.Unmarshal(&params); err != nil {
		return usage(tradeRoute, err)
	}
	defer params.AppPass.Clear()
	res, err := s.core.Trade(params.AppPass, &params.TradeForm)
	if err != nil {
		resErr := msgjson.NewError(msgjson.RPCTradeError, "unable to trade: %v", err)
		return createResponse(tradeRoute, nil, resErr)
	}
	tradeRes := &tradeResponse{
		OrderID: res.ID.String(),
		Sig:     res.Sig.String(),
		Stamp:   res.Stamp,
	}
	return createResponse(tradeRoute, &tradeRes, nil)
}

func handleMultiTrade(s *RPCServer, msg *msgjson.Message) *msgjson.ResponsePayload {
	var params MultiTradeParams
	if err := msg.Unmarshal(&params); err != nil {
		return usage(multiTradeRoute, err)
	}
	defer params.AppPass.Clear()
	results := s.core.MultiTrade(params.AppPass, &params.MultiTradeForm)
	trades := make([]*tradeResponse, 0, len(results))
	for _, res := range results {
		if res.Error != nil {
			trades = append(trades, &tradeResponse{
				Error: res.Error,
			})
			continue
		}
		trade := res.Order
		trades = append(trades, &tradeResponse{
			OrderID: trade.ID.String(),
			Sig:     trade.Sig.String(),
			Stamp:   trade.Stamp,
		})
	}
	return createResponse(multiTradeRoute, &trades, nil)
}

// handleCancel handles requests for cancel. *msgjson.ResponsePayload.Error is
// empty if successful.
func handleCancel(s *RPCServer, msg *msgjson.Message) *msgjson.ResponsePayload {
	var params CancelParams
	if err := msg.Unmarshal(&params); err != nil {
		return usage(cancelRoute, err)
	}
	orderID, err := hex.DecodeString(params.OrderID)
	if err != nil || len(orderID) != order.OrderIDSize {
		return usage(cancelRoute, fmt.Errorf("invalid order ID"))
	}
	if err := s.core.Cancel(orderID); err != nil {
		resErr := msgjson.NewError(msgjson.RPCCancelError, "unable to cancel order %q: %v", params.OrderID, err)
		return createResponse(cancelRoute, nil, resErr)
	}
	res := fmt.Sprintf(canceledOrderStr, params.OrderID)
	return createResponse(cancelRoute, &res, nil)
}

// truncateOrderBook truncates book to the top nOrders of buys and sells.
func truncateOrderBook(book *core.OrderBook, nOrders uint64) {
	truncFn := func(orders []*core.MiniOrder) []*core.MiniOrder {
		if uint64(len(orders)) > nOrders {
			// Nullify pointers stored in the unused part of the
			// underlying array to allow for GC.
			for i := nOrders; i < uint64(len(orders)); i++ {
				orders[i] = nil
			}
			orders = orders[:nOrders]

		}
		return orders
	}
	book.Buys = truncFn(book.Buys)
	book.Sells = truncFn(book.Sells)
}

// handleOrderBook handles requests for orderbook.
// *msgjson.ResponsePayload.Error is empty if successful.
func handleOrderBook(s *RPCServer, msg *msgjson.Message) *msgjson.ResponsePayload {
	var params OrderBookParams
	if err := msg.Unmarshal(&params); err != nil {
		return usage(orderBookRoute, err)
	}
	book, err := s.core.Book(params.Host, params.Base, params.Quote)
	if err != nil {
		resErr := msgjson.NewError(msgjson.RPCOrderBookError, "unable to retrieve order book: %v", err)
		return createResponse(orderBookRoute, nil, resErr)
	}
	if params.NOrders > 0 {
		truncateOrderBook(book, params.NOrders)
	}
	return createResponse(orderBookRoute, book, nil)
}

// parseCoreOrder converts a *core.Order into a *myOrder.
func parseCoreOrder(co *core.Order, b, q uint32) *myOrder {
	// matchesParser parses core.Match slice & calculates how much of the order
	// has been settled/finalized.
	parseMatches := func(matches []*core.Match) (ms []*match, settled uint64) {
		ms = make([]*match, 0, len(matches))
		// coinSafeString gets the Coin's StringID safely.
		coinSafeString := func(c *core.Coin) string {
			if c == nil {
				return ""
			}
			return c.StringID
		}
		for _, m := range matches {
			// Sum up settled value.
			if (m.Side == order.Maker && m.Status >= order.MakerRedeemed) ||
				(m.Side == order.Taker && m.Status >= order.MatchComplete) {
				settled += m.Qty
			}
			match := &match{
				MatchID:  m.MatchID.String(),
				Status:   m.Status.String(),
				Revoked:  m.Revoked,
				Rate:     m.Rate,
				Qty:      m.Qty,
				Side:     m.Side.String(),
				FeeRate:  m.FeeRate,
				Stamp:    m.Stamp,
				IsCancel: m.IsCancel,
			}

			match.Swap = coinSafeString(m.Swap)
			match.CounterSwap = coinSafeString(m.CounterSwap)
			match.Redeem = coinSafeString(m.Redeem)
			match.CounterRedeem = coinSafeString(m.CounterRedeem)
			match.Refund = coinSafeString(m.Refund)
			ms = append(ms, match)
		}
		return ms, settled
	}
	srvTime := time.UnixMilli(int64(co.Stamp))
	age := time.Since(srvTime).Round(time.Millisecond)
	cancelling := co.Cancelling
	// If the order is executed, canceled, or revoked, it is no longer cancelling.
	if co.Status >= order.OrderStatusExecuted {
		cancelling = false
	}
	o := &myOrder{
		Host:        co.Host,
		MarketName:  co.MarketID,
		BaseID:      b,
		QuoteID:     q,
		ID:          co.ID.String(),
		Type:        co.Type.String(),
		Sell:        co.Sell,
		Stamp:       co.Stamp,
		SubmitTime:  co.SubmitTime,
		Age:         age.String(),
		Rate:        co.Rate,
		Quantity:    co.Qty,
		Filled:      co.Filled,
		Status:      co.Status.String(),
		Cancelling:  cancelling,
		Canceled:    co.Canceled,
		TimeInForce: co.TimeInForce.String(),
	}

	// Parse matches & calculate settled value
	o.Matches, o.Settled = parseMatches(co.Matches)

	return o
}

// handleMyOrders handles requests for myorders. *msgjson.ResponsePayload.Error
// is empty if successful.
func handleMyOrders(s *RPCServer, msg *msgjson.Message) *msgjson.ResponsePayload {
	var params MyOrdersParams
	if err := msg.Unmarshal(&params); err != nil {
		return usage(myOrdersRoute, err)
	}
	var myOrders myOrdersResponse
	filterMkts := params.Base != nil && params.Quote != nil
	exchanges := s.core.Exchanges()
	for host, exchange := range exchanges {
		if params.Host != "" && params.Host != host {
			continue
		}
		for _, market := range exchange.Markets {
			if filterMkts && (market.BaseID != *params.Base || market.QuoteID != *params.Quote) {
				continue
			}
			for _, order := range market.Orders {
				myOrders = append(myOrders, parseCoreOrder(order, market.BaseID, market.QuoteID))
			}
			for _, inFlight := range market.InFlightOrders {
				myOrders = append(myOrders, parseCoreOrder(inFlight.Order, market.BaseID, market.QuoteID))
			}
		}
	}
	return createResponse(myOrdersRoute, myOrders, nil)
}

//
// handlers_tx handlers
//

// handleWithdraw handles requests for withdraw. *msgjson.ResponsePayload.Error
// is empty if successful.
func handleWithdraw(s *RPCServer, msg *msgjson.Message) *msgjson.ResponsePayload {
	return send(s, msg, withdrawRoute)
}

// handleSend handles the request for send. *msgjson.ResponsePayload.Error
// is empty if successful.
func handleSend(s *RPCServer, msg *msgjson.Message) *msgjson.ResponsePayload {
	return send(s, msg, sendRoute)
}

func send(s *RPCServer, msg *msgjson.Message, route string) *msgjson.ResponsePayload {
	var params SendParams
	if err := msg.Unmarshal(&params); err != nil {
		return usage(route, err)
	}
	defer params.AppPass.Clear()
	if len(params.AppPass) == 0 {
		resErr := msgjson.NewError(msgjson.RPCFundTransferError, "empty pass")
		return createResponse(route, nil, resErr)
	}
	subtract := params.Subtract
	if route == withdrawRoute {
		subtract = true
	}
	coin, err := s.core.Send(params.AppPass, params.AssetID, params.Value, params.Address, subtract)
	if err != nil {
		resErr := msgjson.NewError(msgjson.RPCFundTransferError, "unable to %s: %v", route, err)
		return createResponse(route, nil, resErr)
	}
	res := coin.String()
	return createResponse(route, &res, nil)
}

// handleAbandonTx handles requests to abandon an unconfirmed transaction.
// This marks the transaction and all its descendants as abandoned, allowing
// the wallet to forget about it. *msgjson.ResponsePayload.Error is empty if
// successful.
func handleAbandonTx(s *RPCServer, msg *msgjson.Message) *msgjson.ResponsePayload {
	var params AbandonTxParams
	if err := msg.Unmarshal(&params); err != nil {
		return usage(abandonTxRoute, err)
	}
	err := s.core.AbandonTransaction(params.AssetID, params.TxID)
	if err != nil {
		resErr := msgjson.NewError(msgjson.RPCWalletRescanError, "unable to abandon transaction: %v", err)
		return createResponse(abandonTxRoute, nil, resErr)
	}
	return createResponse(abandonTxRoute, "transaction abandoned successfully", nil)
}

// handleAppSeed handles requests for the app seed. *msgjson.ResponsePayload.Error
// is empty if successful.
func handleAppSeed(s *RPCServer, msg *msgjson.Message) *msgjson.ResponsePayload {
	var params AppSeedParams
	if err := msg.Unmarshal(&params); err != nil {
		return usage(appSeedRoute, err)
	}
	defer params.AppPass.Clear()
	seed, err := s.core.ExportSeed(params.AppPass)
	if err != nil {
		resErr := msgjson.NewError(msgjson.RPCExportSeedError, "unable to retrieve app seed: %v", err)
		return createResponse(appSeedRoute, nil, resErr)
	}

	return createResponse(appSeedRoute, seed, nil)
}

// handleDeleteArchivedRecords handles requests for deleting archived records.
// *msgjson.ResponsePayload.Error is empty if successful.
func handleDeleteArchivedRecords(s *RPCServer, msg *msgjson.Message) *msgjson.ResponsePayload {
	var params DeleteRecordsParams
	if err := msg.Unmarshal(&params); err != nil {
		return usage(deleteArchivedRecordsRoute, err)
	}

	// Convert to internal form.
	form := &deleteRecordsForm{
		matchesFileStr: params.MatchesFile,
		ordersFileStr:  params.OrdersFile,
	}
	if params.OlderThanMs != nil && *params.OlderThanMs != 0 {
		t := time.UnixMilli(*params.OlderThanMs)
		form.olderThan = &t
	}

	nRecordsDeleted, err := s.core.DeleteArchivedRecords(form.olderThan, form.matchesFileStr, form.ordersFileStr)
	if err != nil {
		resErr := msgjson.NewError(msgjson.RPCDeleteArchivedRecordsError, "unable to delete records: %v", err)
		return createResponse(deleteArchivedRecordsRoute, nil, resErr)
	}

	msg2 := fmt.Sprintf("%d archived records has been deleted successfully", nRecordsDeleted)
	if nRecordsDeleted <= 0 {
		msg2 = "No archived records found"
	}
	return createResponse(deleteArchivedRecordsRoute, msg2, nil)
}

func handleNotifications(s *RPCServer, msg *msgjson.Message) *msgjson.ResponsePayload {
	var params NotificationsParams
	if err := msg.Unmarshal(&params); err != nil {
		return usage(notificationsRoute, err)
	}

	notes, _, err := s.core.Notifications(params.N)
	if err != nil {
		resErr := msgjson.NewError(msgjson.RPCNotificationsError, "unable to handle notification: %v", err)
		return createResponse(notificationsRoute, nil, resErr)
	}

	return createResponse(notificationsRoute, notes, nil)
}

func handleTxHistory(s *RPCServer, msg *msgjson.Message) *msgjson.ResponsePayload {
	var params TxHistoryParams
	if err := msg.Unmarshal(&params); err != nil {
		return usage(txHistoryRoute, err)
	}

	txs, err := s.core.TxHistory(params.AssetID, &asset.TxHistoryRequest{
		N:     params.N,
		RefID: params.RefID,
		Past:  params.Past,
	})
	if err != nil {
		resErr := msgjson.NewError(msgjson.RPCTxHistoryError, "unable to get tx history: %v", err)
		return createResponse(txHistoryRoute, nil, resErr)
	}

	return createResponse(txHistoryRoute, txs, nil)
}

func handleWalletTx(s *RPCServer, msg *msgjson.Message) *msgjson.ResponsePayload {
	var params WalletTxParams
	if err := msg.Unmarshal(&params); err != nil {
		return usage(walletTxRoute, err)
	}

	tx, err := s.core.WalletTransaction(params.AssetID, params.TxID)
	if err != nil {
		resErr := msgjson.NewError(msgjson.RPCTxHistoryError, "unable to get wallet tx: %v", err)
		return createResponse(walletTxRoute, nil, resErr)
	}

	return createResponse(walletTxRoute, tx, nil)
}

func handleWithdrawBchSpv(s *RPCServer, msg *msgjson.Message) *msgjson.ResponsePayload {
	var params BchWithdrawParams
	if err := msg.Unmarshal(&params); err != nil {
		return usage(withdrawBchSpvRoute, err)
	}
	defer params.AppPass.Clear()

	txB, err := s.core.GenerateBCHRecoveryTransaction(params.AppPass, params.Recipient)
	if err != nil {
		resErr := msgjson.NewError(msgjson.RPCCreateWalletError, "error generating tx: %v", err)
		return createResponse(withdrawBchSpvRoute, nil, resErr)
	}

	return createResponse(withdrawBchSpvRoute, dex.Bytes(txB).String(), nil)
}

//
// handlers_dex handlers
//

// handleBondAssets handles requests for bondassets.
// *msgjson.ResponsePayload.Error is empty if successful. Requires the address
// of a dex and returns the bond expiry and supported asset bond details.
func handleBondAssets(s *RPCServer, msg *msgjson.Message) *msgjson.ResponsePayload {
	var params BondAssetsParams
	if err := msg.Unmarshal(&params); err != nil {
		return usage(bondAssetsRoute, err)
	}
	var cert []byte
	if params.Cert != "" {
		cert = []byte(params.Cert)
	}
	exchInf := s.core.Exchanges()
	exchCfg := exchInf[params.Host]
	if exchCfg == nil {
		var err error
		exchCfg, err = s.core.GetDEXConfig(params.Host, cert) // cert is file contents, not name
		if err != nil {
			resErr := msgjson.NewError(msgjson.RPCGetDEXConfigError, "%v", err)
			return createResponse(bondAssetsRoute, nil, resErr)
		}
	}
	res := &getBondAssetsResponse{
		Expiry: exchCfg.BondExpiry,
		Assets: exchCfg.BondAssets,
	}
	return createResponse(bondAssetsRoute, res, nil)
}

// handleGetDEXConfig handles requests for getdexconfig.
// *msgjson.ResponsePayload.Error is empty if successful. Requires the address
// of a dex and returns its config..
func handleGetDEXConfig(s *RPCServer, msg *msgjson.Message) *msgjson.ResponsePayload {
	var params GetDEXConfigParams
	if err := msg.Unmarshal(&params); err != nil {
		return usage(getDEXConfRoute, err)
	}
	var cert []byte
	if params.Cert != "" {
		cert = []byte(params.Cert)
	}
	exchange, err := s.core.GetDEXConfig(params.Host, cert) // cert is file contents, not name
	if err != nil {
		resErr := msgjson.NewError(msgjson.RPCGetDEXConfigError, "%v", err)
		return createResponse(getDEXConfRoute, nil, resErr)
	}
	return createResponse(getDEXConfRoute, exchange, nil)
}

// handleDiscoverAcct is the handler for discoveracct. *msgjson.ResponsePayload.Error
// is empty if successful.
func handleDiscoverAcct(s *RPCServer, msg *msgjson.Message) *msgjson.ResponsePayload {
	var params DiscoverAcctParams
	if err := msg.Unmarshal(&params); err != nil {
		return usage(discoverAcctRoute, err)
	}
	defer params.AppPass.Clear()
	var cert []byte
	if params.Cert != "" {
		cert = []byte(params.Cert)
	}
	_, paid, err := s.core.DiscoverAccount(params.Addr, params.AppPass, cert)
	if err != nil {
		resErr := msgjson.NewError(msgjson.RPCDiscoverAcctError, "%v", err)
		return createResponse(discoverAcctRoute, nil, resErr)
	}
	return createResponse(discoverAcctRoute, &paid, nil)
}

func handleBondOptions(s *RPCServer, msg *msgjson.Message) *msgjson.ResponsePayload {
	var params core.BondOptionsForm
	if err := msg.Unmarshal(&params); err != nil {
		return usage(bondOptionsRoute, err)
	}
	err := s.core.UpdateBondOptions(&params)
	if err != nil {
		resErr := msgjson.NewError(msgjson.RPCPostBondError, "%v", err)
		return createResponse(bondOptionsRoute, nil, resErr)
	}
	return createResponse(bondOptionsRoute, "ok", nil)
}

// handlePostBond handles requests for postbond. *msgjson.ResponsePayload.Error
// is empty if successful.
func handlePostBond(s *RPCServer, msg *msgjson.Message) *msgjson.ResponsePayload {
	var form core.PostBondForm
	if err := msg.Unmarshal(&form); err != nil {
		return usage(postBondRoute, err)
	}
	defer form.AppPass.Clear()
	// Get the exchange config with Exchanges(), not GetDEXConfig, since we may
	// already be connected and even with an existing account.
	exchInf := s.core.Exchanges()
	exchCfg := exchInf[form.Addr]
	if exchCfg == nil {
		// Not already registered.
		var err error
		exchCfg, err = s.core.GetDEXConfig(form.Addr, form.Cert)
		if err != nil {
			resErr := msgjson.NewError(msgjson.RPCGetDEXConfigError, "%v", err)
			return createResponse(postBondRoute, nil, resErr)
		}
	}
	// Registration with different assets will be supported in the future, but
	// for now, this requires DCR.
	assetID := uint32(42)
	if form.Asset != nil {
		assetID = *form.Asset
	}
	symb := dex.BipIDSymbol(assetID)

	bondAsset, supported := exchCfg.BondAssets[symb]
	if !supported {
		resErr := msgjson.NewError(msgjson.RPCPostBondError, "DEX %s does not support registration with %s", form.Addr, symb)
		return createResponse(postBondRoute, nil, resErr)
	}
	if bondAsset.Amt > form.Bond || form.Bond%bondAsset.Amt != 0 {
		resErr := msgjson.NewError(msgjson.RPCPostBondError, "DEX at %s expects a bond amount in multiples of %d %s but %d was offered",
			form.Addr, bondAsset.Amt, dex.BipIDSymbol(assetID), form.Bond)
		return createResponse(postBondRoute, nil, resErr)
	}
	res, err := s.core.PostBond(&form)
	if err != nil {
		resErr := msgjson.NewError(msgjson.RPCPostBondError, "%v", err)
		return createResponse(postBondRoute, nil, resErr)
	}
	if res.BondID == "" {
		return createResponse(postBondRoute, "existing account configured - no bond posted", nil)
	}
	return createResponse(postBondRoute, res, nil)
}

//
// handlers_mm handlers
//

func handleMMAvailableBalances(s *RPCServer, msg *msgjson.Message) *msgjson.ResponsePayload {
	var params MMAvailableBalancesParams
	if err := msg.Unmarshal(&params); err != nil {
		return usage(mmAvailableBalancesRoute, err)
	}

	mkt := &mm.MarketWithHost{
		Host:    params.Host,
		BaseID:  params.BaseID,
		QuoteID: params.QuoteID,
	}

	dexBalances, cexBalances, err := s.mm.AvailableBalances(mkt, params.CexBaseID, params.CexQuoteID, params.CexName)
	if err != nil {
		resErr := msgjson.NewError(msgjson.RPCMMAvailableBalancesError, "unable to get available balances: %v", err)
		return createResponse(mmAvailableBalancesRoute, nil, resErr)
	}

	res := struct {
		DEXBalances map[uint32]uint64 `json:"dexBalances"`
		CEXBalances map[uint32]uint64 `json:"cexBalances"`
	}{
		DEXBalances: dexBalances,
		CEXBalances: cexBalances,
	}

	return createResponse(mmAvailableBalancesRoute, res, nil)
}

func handleStartBot(s *RPCServer, msg *msgjson.Message) *msgjson.ResponsePayload {
	var params StartBotParams
	if err := msg.Unmarshal(&params); err != nil {
		return usage(startBotRoute, err)
	}

	if params.Market == nil {
		// Start all bots
		started, err := s.mm.StartBots(&params.CfgFilePath, params.AppPass)
		if err != nil {
			resErr := msgjson.NewError(msgjson.RPCStartMarketMakingError, "error starting bots: %v", err)
			return createResponse(startBotRoute, nil, resErr)
		}
		return createResponse(startBotRoute, fmt.Sprintf("started %d bot(s)", started), nil)
	}

	// Start specific bot
	err := s.mm.StartBot(params.Market, &params.CfgFilePath, params.AppPass, true)
	if err != nil {
		resErr := msgjson.NewError(msgjson.RPCStartMarketMakingError, "unable to start market making: %v", err)
		return createResponse(startBotRoute, nil, resErr)
	}

	return createResponse(startBotRoute, "started bot", nil)
}

func handleStopBot(s *RPCServer, msg *msgjson.Message) *msgjson.ResponsePayload {
	var params StopBotParams
	if err := msg.Unmarshal(&params); err != nil {
		return usage(stopBotRoute, err)
	}

	if params.Market == nil {
		// Stop all bots
		stopped, err := s.mm.StopBots()
		if err != nil {
			resErr := msgjson.NewError(msgjson.RPCStopMarketMakingError, "error stopping bots: %v", err)
			return createResponse(stopBotRoute, nil, resErr)
		}
		return createResponse(stopBotRoute, fmt.Sprintf("stopping %d bot(s)", stopped), nil)
	}

	// Stop specific bot
	err := s.mm.StopBot(params.Market)
	if err != nil {
		resErr := msgjson.NewError(msgjson.RPCStopMarketMakingError, "unable to stop market making: %v", err)
		return createResponse(stopBotRoute, nil, resErr)
	}

	return createResponse(stopBotRoute, "stopping bot", nil)
}

func handleUpdateRunningBotCfg(s *RPCServer, msg *msgjson.Message) *msgjson.ResponsePayload {
	var params UpdateRunningBotParams
	if err := msg.Unmarshal(&params); err != nil {
		return usage(updateRunningBotCfgRoute, err)
	}

	data, err := os.ReadFile(params.CfgFilePath)
	if err != nil {
		resErr := msgjson.NewError(msgjson.RPCUpdateRunningBotCfgError, "unable to read config file: %v", err)
		return createResponse(updateRunningBotCfgRoute, nil, resErr)
	}

	cfg := &mm.MarketMakingConfig{}
	err = json.Unmarshal(data, cfg)
	if err != nil {
		resErr := msgjson.NewError(msgjson.RPCUpdateRunningBotCfgError, "unable to unmarshal config: %v", err)
		return createResponse(updateRunningBotCfgRoute, nil, resErr)
	}

	var botCfg *mm.BotConfig
	for _, bot := range cfg.BotConfigs {
		if bot.Host == params.Market.Host && bot.BaseID == params.Market.BaseID && bot.QuoteID == params.Market.QuoteID {
			botCfg = bot
			break
		}
	}

	if botCfg == nil {
		resErr := msgjson.NewError(msgjson.RPCUpdateRunningBotCfgError, "bot config not found for market %s", params.Market.String())
		return createResponse(updateRunningBotCfgRoute, nil, resErr)
	}

	err = s.mm.UpdateRunningBotCfg(botCfg, params.Balances, nil, false)
	if err != nil {
		resErr := msgjson.NewError(msgjson.RPCUpdateRunningBotCfgError, "unable to update running bot: %v", err)
		return createResponse(updateRunningBotCfgRoute, nil, resErr)
	}

	return createResponse(updateRunningBotCfgRoute, "updated running bot", nil)
}

func handleUpdateRunningBotInventory(s *RPCServer, msg *msgjson.Message) *msgjson.ResponsePayload {
	var params UpdateRunningBotInventoryParams
	if err := msg.Unmarshal(&params); err != nil {
		return usage(updateRunningBotInvRoute, err)
	}

	err := s.mm.UpdateRunningBotInventory(&params.Market, params.Balances)
	if err != nil {
		resErr := msgjson.NewError(msgjson.RPCUpdateRunningBotInvError, "unable to update running bot: %v", err)
		return createResponse(updateRunningBotInvRoute, nil, resErr)
	}

	return createResponse(updateRunningBotInvRoute, "updated running bot", nil)
}

func handleMMStatus(s *RPCServer, _ *msgjson.Message) *msgjson.ResponsePayload {
	status := s.mm.RunningBotsStatus()
	return createResponse(mmStatusRoute, status, nil)
}

//
// handlers_staking handlers
//

func handleSetVSP(s *RPCServer, msg *msgjson.Message) *msgjson.ResponsePayload {
	var params SetVSPParams
	if err := msg.Unmarshal(&params); err != nil {
		return usage(setVSPRoute, err)
	}

	err := s.core.SetVSP(params.AssetID, params.Addr)
	if err != nil {
		resErr := msgjson.NewError(msgjson.RPCSetVSPError, "unable to set vsp: %v", err)
		return createResponse(setVSPRoute, nil, resErr)
	}

	return createResponse(setVSPRoute, fmt.Sprintf(setVSPStr, params.Addr), nil)
}

func handlePurchaseTickets(s *RPCServer, msg *msgjson.Message) *msgjson.ResponsePayload {
	var params PurchaseTicketsParams
	if err := msg.Unmarshal(&params); err != nil {
		return usage(purchaseTicketsRoute, err)
	}
	defer params.AppPass.Clear()

	if err := s.core.PurchaseTickets(params.AssetID, params.AppPass, params.N); err != nil {
		resErr := msgjson.NewError(msgjson.RPCPurchaseTicketsError, "unable to purchase tickets: %v", err)
		return createResponse(purchaseTicketsRoute, nil, resErr)
	}

	return createResponse(purchaseTicketsRoute, true, nil)
}

func handleStakeStatus(s *RPCServer, msg *msgjson.Message) *msgjson.ResponsePayload {
	var params StakeStatusParams
	if err := msg.Unmarshal(&params); err != nil {
		return usage(stakeStatusRoute, err)
	}
	stakeStatus, err := s.core.StakeStatus(params.AssetID)
	if err != nil {
		resErr := msgjson.NewError(msgjson.RPCStakeStatusError, "unable to get staking status: %v", err)
		return createResponse(stakeStatusRoute, nil, resErr)
	}

	return createResponse(stakeStatusRoute, &stakeStatus, nil)
}

func handleSetVotingPreferences(s *RPCServer, msg *msgjson.Message) *msgjson.ResponsePayload {
	var params SetVotingPreferencesParams
	if err := msg.Unmarshal(&params); err != nil {
		return usage(setVotingPreferencesRoute, err)
	}

	err := s.core.SetVotingPreferences(params.AssetID, params.Choices, params.TSpendPolicy, params.TreasuryPolicy)
	if err != nil {
		resErr := msgjson.NewError(msgjson.RPCSetVotingPreferencesError, "unable to set voting preferences: %v", err)
		return createResponse(setVotingPreferencesRoute, nil, resErr)
	}

	return createResponse(setVotingPreferencesRoute, "vote preferences set", nil)
}

//
// handlers_bridge handlers
//

func handleCheckBridgeApproval(s *RPCServer, msg *msgjson.Message) *msgjson.ResponsePayload {
	var params CheckBridgeApprovalParams
	if err := msg.Unmarshal(&params); err != nil {
		return usage(checkBridgeApprovalRoute, err)
	}

	approvalStatus, err := s.core.BridgeContractApprovalStatus(params.AssetID, params.BridgeName)
	if err != nil {
		resErr := msgjson.NewError(msgjson.RPCBridgeError, "unable to check bridge approval: %v", err)
		return createResponse(checkBridgeApprovalRoute, nil, resErr)
	}

	return createResponse(checkBridgeApprovalRoute, approvalStatus.String(), nil)
}

func handleApproveBridge(s *RPCServer, msg *msgjson.Message) *msgjson.ResponsePayload {
	var params ApproveBridgeParams
	if err := msg.Unmarshal(&params); err != nil {
		return usage(approveBridgeContractRoute, err)
	}

	var txID string
	var err error
	if params.Approve {
		txID, err = s.core.ApproveBridgeContract(params.AssetID, params.BridgeName)
	} else {
		txID, err = s.core.UnapproveBridgeContract(params.AssetID, params.BridgeName)
	}

	if err != nil {
		resErr := msgjson.NewError(msgjson.RPCBridgeError, "unable to approve bridge contract: %v", err)
		return createResponse(approveBridgeContractRoute, nil, resErr)
	}

	return createResponse(approveBridgeContractRoute, txID, nil)
}

func handleBridge(s *RPCServer, msg *msgjson.Message) *msgjson.ResponsePayload {
	var params BridgeParams
	if err := msg.Unmarshal(&params); err != nil {
		return usage(bridgeRoute, err)
	}

	txID, err := s.core.Bridge(params.FromAssetID, params.ToAssetID, params.Amt, params.BridgeName)
	if err != nil {
		resErr := msgjson.NewError(msgjson.RPCBridgeError, "unable to initiate bridge: %v", err)
		return createResponse(bridgeRoute, nil, resErr)
	}

	return createResponse(bridgeRoute, txID, nil)
}

func handlePendingBridges(s *RPCServer, msg *msgjson.Message) *msgjson.ResponsePayload {
	var params PendingBridgesParams
	if err := msg.Unmarshal(&params); err != nil {
		return usage(pendingBridgesRoute, err)
	}

	bridges, err := s.core.PendingBridges(params.AssetID)
	if err != nil {
		resErr := msgjson.NewError(msgjson.RPCBridgeError, "unable to get pending bridges: %v", err)
		return createResponse(pendingBridgesRoute, nil, resErr)
	}

	return createResponse(pendingBridgesRoute, bridges, nil)
}

func handleBridgeHistory(s *RPCServer, msg *msgjson.Message) *msgjson.ResponsePayload {
	var params TxHistoryParams
	if err := msg.Unmarshal(&params); err != nil {
		return usage(bridgeHistoryRoute, err)
	}

	bridges, err := s.core.BridgeHistory(params.AssetID, params.N, params.RefID, params.Past)
	if err != nil {
		resErr := msgjson.NewError(msgjson.RPCBridgeError, "unable to get bridge history: %v", err)
		return createResponse(bridgeHistoryRoute, nil, resErr)
	}

	return createResponse(bridgeHistoryRoute, bridges, nil)
}

func handleSupportedBridges(s *RPCServer, msg *msgjson.Message) *msgjson.ResponsePayload {
	var params SupportedBridgesParams
	if err := msg.Unmarshal(&params); err != nil {
		return usage(supportedBridgesRoute, err)
	}

	bridgeDestinations, err := s.core.SupportedBridgeDestinations(params.AssetID)
	if err != nil {
		resErr := msgjson.NewError(msgjson.RPCBridgeError, "unable to get supported bridge destinations: %v", err)
		return createResponse(supportedBridgesRoute, nil, resErr)
	}

	// Convert asset IDs to symbols for each destination
	result := make(map[string][]string)
	for destAssetID, bridgeNames := range bridgeDestinations {
		destSymbol := dex.BipIDSymbol(destAssetID)
		result[destSymbol] = bridgeNames
	}

	return createResponse(supportedBridgesRoute, result, nil)
}

func handleBridgeFeesAndLimits(s *RPCServer, msg *msgjson.Message) *msgjson.ResponsePayload {
	var params BridgeFeesAndLimitsParams
	if err := msg.Unmarshal(&params); err != nil {
		return usage(bridgeFeesAndLimitsRoute, err)
	}

	result, err := s.core.BridgeFeesAndLimits(params.FromAssetID, params.ToAssetID, params.BridgeName)
	if err != nil {
		resErr := msgjson.NewError(msgjson.RPCBridgeError, "unable to get bridge fees and limits: %v", err)
		return createResponse(bridgeFeesAndLimitsRoute, nil, resErr)
	}

	return createResponse(bridgeFeesAndLimitsRoute, result, nil)
}

//
// handlers_multisig handlers
//

func handlePaymentMultisigPubkey(s *RPCServer, msg *msgjson.Message) *msgjson.ResponsePayload {
	var params PaymentMultisigPubkeyParams
	if err := msg.Unmarshal(&params); err != nil {
		return usage(paymentMultisigPubkeyRoute, err)
	}

	pubkey, err := s.core.PaymentMultisigPubkey(params.AssetID)
	if err != nil {
		resErr := msgjson.NewError(msgjson.RPCPaymentMultisigError, "unable to get pubkey: %v", err)
		return createResponse(paymentMultisigPubkeyRoute, nil, resErr)
	}
	return createResponse(paymentMultisigPubkeyRoute, pubkey, nil)
}

func handleSendFundsToMultisig(s *RPCServer, msg *msgjson.Message) *msgjson.ResponsePayload {
	var params CsvFileParams
	if err := msg.Unmarshal(&params); err != nil {
		return usage(sendFundsToMultisigRoute, err)
	}
	if err := s.core.SendFundsToMultisig(params.CsvFilePath); err != nil {
		resErr := msgjson.NewError(msgjson.RPCPaymentMultisigError, "unable to send funds to multisig: %v", err)
		return createResponse(sendFundsToMultisigRoute, nil, resErr)
	}
	res := "csv updated"
	return createResponse(sendFundsToMultisigRoute, &res, nil)
}

func handleSignMultisig(s *RPCServer, msg *msgjson.Message) *msgjson.ResponsePayload {
	var params SignMultisigParams
	if err := msg.Unmarshal(&params); err != nil {
		return usage(signMultisigRoute, err)
	}
	if err := s.core.SignMultisig(params.CsvFilePath, params.SignIdx); err != nil {
		resErr := msgjson.NewError(msgjson.RPCPaymentMultisigError, "unable to sign multisig: %v", err)
		return createResponse(signMultisigRoute, nil, resErr)
	}
	res := "csv updated"
	return createResponse(signMultisigRoute, &res, nil)
}

func handleRefundPaymentMultisig(s *RPCServer, msg *msgjson.Message) *msgjson.ResponsePayload {
	var params CsvFileParams
	if err := msg.Unmarshal(&params); err != nil {
		return usage(refundPaymentMultisigRoute, err)
	}
	res, err := s.core.RefundPaymentMultisig(params.CsvFilePath)
	if err != nil {
		resErr := msgjson.NewError(msgjson.RPCPaymentMultisigError, "unable to refund multisig: %v", err)
		return createResponse(refundPaymentMultisigRoute, nil, resErr)
	}
	return createResponse(refundPaymentMultisigRoute, &res, nil)
}

func handleViewPaymentMultisig(s *RPCServer, msg *msgjson.Message) *msgjson.ResponsePayload {
	var params CsvFileParams
	if err := msg.Unmarshal(&params); err != nil {
		return usage(viewPaymentMultisigRoute, err)
	}
	res, err := s.core.ViewPaymentMultisig(params.CsvFilePath)
	if err != nil {
		resErr := msgjson.NewError(msgjson.RPCPaymentMultisigError, "unable to view multisig: %v", err)
		return createResponse(viewPaymentMultisigRoute, nil, resErr)
	}
	return createResponse(viewPaymentMultisigRoute, &res, nil)
}

func handleSendPaymentMultisig(s *RPCServer, msg *msgjson.Message) *msgjson.ResponsePayload {
	var params CsvFileParams
	if err := msg.Unmarshal(&params); err != nil {
		return usage(sendPaymentMultisigRoute, err)
	}
	res, err := s.core.SendPaymentMultisig(params.CsvFilePath)
	if err != nil {
		resErr := msgjson.NewError(msgjson.RPCPaymentMultisigError, "unable to send multisig: %v", err)
		return createResponse(sendPaymentMultisigRoute, nil, resErr)
	}
	return createResponse(sendPaymentMultisigRoute, &res, nil)
}

//
// handlers_peers handlers
//

func handleWalletPeers(s *RPCServer, msg *msgjson.Message) *msgjson.ResponsePayload {
	var params WalletPeersParams
	if err := msg.Unmarshal(&params); err != nil {
		return usage(walletPeersRoute, err)
	}

	peers, err := s.core.WalletPeers(params.AssetID)
	if err != nil {
		resErr := msgjson.NewError(msgjson.RPCWalletPeersError, "unable to get wallet peers: %v", err)
		return createResponse(walletPeersRoute, nil, resErr)
	}
	return createResponse(walletPeersRoute, peers, nil)
}

func handleAddWalletPeer(s *RPCServer, msg *msgjson.Message) *msgjson.ResponsePayload {
	var params AddRemovePeerParams
	if err := msg.Unmarshal(&params); err != nil {
		return usage(addWalletPeerRoute, err)
	}

	err := s.core.AddWalletPeer(params.AssetID, params.Address)
	if err != nil {
		resErr := msgjson.NewError(msgjson.RPCWalletPeersError, "unable to add wallet peer: %v", err)
		return createResponse(addWalletPeerRoute, nil, resErr)
	}

	return createResponse(addWalletPeerRoute, "successfully added peer", nil)
}

func handleRemoveWalletPeer(s *RPCServer, msg *msgjson.Message) *msgjson.ResponsePayload {
	var params AddRemovePeerParams
	if err := msg.Unmarshal(&params); err != nil {
		return usage(removeWalletPeerRoute, err)
	}

	err := s.core.RemoveWalletPeer(params.AssetID, params.Address)
	if err != nil {
		resErr := msgjson.NewError(msgjson.RPCWalletPeersError, "unable to remove wallet peer: %v", err)
		return createResponse(removeWalletPeerRoute, nil, resErr)
	}

	return createResponse(removeWalletPeerRoute, "successfully removed peer", nil)
}

// passBytesType is used by reflection to detect password fields.
var passBytesType = reflect.TypeOf(encode.PassBytes{})

// routeInfo describes a route's parameters and help text, replacing the old
// helpMsg struct. Parameter documentation is auto-generated from the struct
// type via reflection, making it impossible for help text to drift from
// actual param types.
type routeInfo struct {
	paramsType reflect.Type      // nil for no-params routes
	summary    string            // short command description
	fieldDescs map[string]string // JSON field name -> description
	returns    string            // return value description
	extraHelp  func() string     // optional dynamic help text appended to usage
}

// fieldInfo holds metadata for a single struct field, extracted via reflection.
type fieldInfo struct {
	jsonName   string
	typeName   string // "string", "int", "bool", "password", "object", "array", "any"
	goType     reflect.Type
	isPassword bool   // true if Go type is encode.PassBytes
	isOptional bool   // true if pointer or omitempty
	desc       string // from routeInfo.fieldDescs
}

// FieldInfo is the exported version of fieldInfo for use by bwctl's generic
// builder.
type FieldInfo struct {
	JSONName   string
	GoType     reflect.Type
	IsPassword bool
	IsOptional bool
}

// ReflectFields returns exported field metadata for the given struct type in
// declaration order (same order as help output). Embedded structs are flattened.
func ReflectFields(t reflect.Type) []FieldInfo {
	internal := reflectFields(t, nil)
	out := make([]FieldInfo, len(internal))
	for i, f := range internal {
		out[i] = FieldInfo{
			JSONName:   f.jsonName,
			GoType:     f.goType,
			IsPassword: f.isPassword,
			IsOptional: f.isOptional,
		}
	}
	return out
}

// ParamType returns the reflect.Type for the given route's params struct, or
// nil for no-params routes.
func ParamType(route string) reflect.Type {
	info, ok := routeInfos[route]
	if !ok {
		return nil
	}
	return info.paramsType
}

// RouteExists reports whether route is a known RPC route.
func RouteExists(route string) bool {
	_, ok := routeInfos[route]
	return ok
}

// Routes returns the names of all known RPC routes.
func Routes() []string {
	routes := make([]string, 0, len(routeInfos))
	for r := range routeInfos {
		routes = append(routes, r)
	}
	return routes
}

// Common field description constants.
const (
	descAppPass     = "The Bison Wallet password."
	descAssetID     = "The asset's BIP-44 registered coin index. e.g. 42 for DCR. See https://github.com/satoshilabs/slips/blob/master/slip-0044.md"
	descHost        = "The DEX address."
	descCert        = "The TLS certificate path."
	descBase        = "The BIP-44 coin index for the market's base asset."
	descQuote       = "The BIP-44 coin index for the market's quote asset."
	descFromAssetID = `The asset's BIP-44 registered coin index on the "from" chain.`
	descToAssetID   = `The asset's BIP-44 registered coin index on the "to" chain.`
	descBridgeName  = "The name of the bridge."
	descCsvFilePath = "The csv file path from the point of view of the client, not bwctl."
)

// routeInfos maps each route to its parameter type and help metadata.
var routeInfos = map[string]routeInfo{
	helpRoute: {
		paramsType: reflect.TypeOf(HelpParams{}),
		summary:    `Print a help message.`,
		fieldDescs: map[string]string{
			"helpWith":         "The command to print help for.",
			"includePasswords": "Whether to include password arguments in the returned help.",
		},
		returns: `Returns:
    string: The help message for command.`,
	},
	versionRoute: {
		summary: `Print the Bison Wallet rpcserver version.`,
		returns: `Returns:
    string: The Bison Wallet rpcserver version.`,
	},
	discoverAcctRoute: {
		paramsType: reflect.TypeOf(DiscoverAcctParams{}),
		summary: `Discover an account that is used for a DEX. Useful when restoring
    an account. Will error if the account has already been discovered/restored.`,
		fieldDescs: map[string]string{
			"appPass": descAppPass,
			"addr":    "The DEX address to discover an account for.",
			"cert":    descCert,
		},
		returns: `Returns:
    bool: True if the account has been registered and paid for.`,
	},
	initRoute: {
		paramsType: reflect.TypeOf(InitParams{}),
		summary:    `Initialize the client.`,
		fieldDescs: map[string]string{
			"appPass": descAppPass,
			"seed":    "hex-encoded 512-bit restoration seed or mnemonic.",
		},
		returns: `Returns:
    string: The message "` + initializedStr + `"`,
	},
	deleteArchivedRecordsRoute: {
		paramsType: reflect.TypeOf(DeleteRecordsParams{}),
		summary: `Delete archived records from the database and returns total deleted. Optionally
    set a time to delete records before and file paths to save deleted records as comma separated
    values. Note that file locations are from the perspective of dexc and not the caller.`,
		fieldDescs: map[string]string{
			"olderThanMs": "If set, deletes records before the date in unix time in milliseconds (not seconds). Unset or 0 defaults to the current time.",
			"matchesFile": "A path to save a csv with deleted matches. Will not save by default.",
			"ordersFile":  "A path to save a csv with deleted orders. Will not save by default.",
		},
		returns: `Returns:
    Nothing.`,
	},
	bondAssetsRoute: {
		paramsType: reflect.TypeOf(BondAssetsParams{}),
		summary:    `Get dex bond asset config.`,
		fieldDescs: map[string]string{
			"host": "The dex address to get bond info for.",
			"cert": descCert,
		},
		returns: `Returns:
    obj: The getBondAssets result.
    {
      "expiry" (int): Bond expiry in seconds remaining until locktime.
      "assets" (object): {
        "id" (int): The BIP-44 coin type for the asset.
        "confs" (int): The required confirmations for the bond transaction.
        "amount" (int): The minimum bond amount.
      }
    }`,
	},
	getDEXConfRoute: {
		paramsType: reflect.TypeOf(GetDEXConfigParams{}),
		summary:    `Get a DEX configuration.`,
		fieldDescs: map[string]string{
			"host": "The dex address to get config for.",
			"cert": descCert,
		},
		returns: `Returns:
    obj: The getdexconfig result. See the 'exchanges' result.`,
	},
	newWalletRoute: {
		paramsType: reflect.TypeOf(NewWalletParams{}),
		summary:    `Connect to a new wallet.`,
		fieldDescs: map[string]string{
			"appPass":    descAppPass,
			"walletPass": "The wallet's password. Leave the password empty for wallets without a password set.",
			"assetID":    descAssetID,
			"walletType": "The wallet type.",
			"config":     `A JSON string->string mapping of additional configuration settings. These settings take precedence over any settings parsed from file.`,
		},
		returns: `Returns:
    string: The message "` + fmt.Sprintf(walletCreatedStr, "[coin symbol]") + `"`,
		extraHelp: walletTypesHelp,
	},
	openWalletRoute: {
		paramsType: reflect.TypeOf(OpenWalletParams{}),
		summary:    `Open an existing wallet.`,
		fieldDescs: map[string]string{
			"appPass": descAppPass,
			"assetID": descAssetID,
		},
		returns: `Returns:
    string: The message "` + fmt.Sprintf(walletUnlockedStr, "[coin symbol]") + `"`,
	},
	closeWalletRoute: {
		paramsType: reflect.TypeOf(CloseWalletParams{}),
		summary:    `Close an open wallet.`,
		fieldDescs: map[string]string{
			"assetID": descAssetID,
		},
		returns: `Returns:
    string: The message "` + fmt.Sprintf(walletLockedStr, "[coin symbol]") + `"`,
	},
	deployContractRoute: {
		paramsType: reflect.TypeOf(DeployContractParams{}),
		summary:    "Deploy a smart contract to one or more EVM chains.",
		fieldDescs: map[string]string{
			"appPass":      descAppPass,
			"chains":       `List of chain symbols to deploy to, e.g. ["eth", "polygon", "base"].`,
			"contractVer":  "The DEX swap contract version to deploy (0 or 1). Ignored if bytecode is provided.",
			"tokenAddress": "The ERC20 token address. Required when deploying a token swap contract with contractVer.",
			"bytecode":     "Hex-encoded contract bytecode for arbitrary deployment. If provided, takes priority over contractVer.",
		},
		returns: `Returns:
    array: Per-chain deployment results.
    [
      {
        "assetID" (int): The chain's BIP-44 asset ID.
        "symbol" (string): The chain symbol.
        "contractAddr" (string): The deployed contract address.
        "txID" (string): The deployment transaction ID.
        "error" (string): Error if deployment failed on this chain.
      }
    ]`,
		extraHelp: func() string {
			return `Examples:
    Deploy v1 base chain contract to all chains:
      bwctl deploycontract '["eth","polygon","base"]' 1

    Deploy v0 token contract:
      bwctl deploycontract '["polygon"]' 0 0x2791bca1f2de4661ed88a30c99a7a9449aa84174

    Deploy arbitrary bytecode (pass 0 and empty token address as placeholders):
      bwctl deploycontract '["eth"]' 0 '' 0x608060...`
		},
	},
	toggleWalletStatusRoute: {
		paramsType: reflect.TypeOf(ToggleWalletStatusParams{}),
		summary: `Disable or enable an existing wallet. When disabling a chain's primary asset wallet,
    all token wallets for that chain will be disabled too.`,
		fieldDescs: map[string]string{
			"assetID": descAssetID,
			"disable": `The wallet's status. To disable a wallet set to true, to enable set to false.`,
		},
		returns: `Returns:
    string: The message "` + fmt.Sprintf(walletStatusStr, "[coin symbol]", "[wallet status]") + `".`,
	},
	walletBalanceRoute: {
		paramsType: reflect.TypeOf(WalletBalanceParams{}),
		summary:    `Get the balance for a single wallet.`,
		fieldDescs: map[string]string{
			"assetID": descAssetID,
		},
		returns: `Returns:
    obj: The wallet balance.
    {
      "available" (int): The balance available for funding orders.
      "immature" (int): Balance that requires confirmations before use.
      "locked" (int): The total locked balance.
      "bondReserves" (int): Amount reserved for bond maintenance.
      "reservesDeficit" (int): Deficit if reserves are insufficient.
      "other" (obj): Other balance categories.
      "stamp" (string): Time stamp.
      "orderlocked" (int): Amount locked in active orders.
      "contractlocked" (int): Amount locked in active swap contracts.
      "bondlocked" (int): Amount locked in active bonds.
    }`,
	},
	walletStateRoute: {
		paramsType: reflect.TypeOf(WalletStateParams{}),
		summary:    `Get the state for a single wallet.`,
		fieldDescs: map[string]string{
			"assetID": descAssetID,
		},
		returns: `Returns:
    obj: The wallet state.
    {
      "symbol" (string): The coin symbol.
      "assetID" (int): The asset's BIP-44 registered coin index.
      "open" (bool): Whether the wallet is unlocked.
      "running" (bool): Whether the wallet is running.
      "disabled" (bool): Whether the wallet is disabled.
      "balance" (obj): The wallet balance.
      "address" (string): A wallet address.
    }`,
	},
	walletsRoute: {
		summary: `List all wallets.`,
		returns: `Returns:
    array: An array of wallet results.
    [
      {
        "symbol" (string): The coin symbol.
        "assetID" (int): The asset's BIP-44 registered coin index. e.g. 42 for DCR.
        "open" (bool): Whether the wallet is unlocked.
        "running" (bool): Whether the wallet is running.
        "disabled" (bool): Whether the wallet is disabled.
        "updated" (int): Unix time of last balance update. Seconds since 00:00:00 Jan 1 1970.
        "balance" (obj): {
          "available" (int): The balance available for funding orders case.
          "immature" (int): Balance that requires confirmations before use.
          "locked" (int): The total locked balance.
          "stamp" (string): Time stamp.
        }
        "address" (string): A wallet address.
        "feerate" (int): The fee rate.
        "units" (string): Unit of measure for amounts.
      },...
    ]`,
	},
	postBondRoute: {
		paramsType: reflect.TypeOf(core.PostBondForm{}),
		summary: `Post new bond for DEX. An ok response does not mean that the bond is active.
    Bond is active after the bond transaction has been confirmed and the server notified.`,
		fieldDescs: map[string]string{
			"host":         descHost,
			"appPass":      descAppPass,
			"assetID":      "The asset ID with which to pay the fee.",
			"bond":         "The bond amount.",
			"lockTime":     "The lock time. 0 means go with server-derived value.",
			"feeBuffer":    "Optional fee buffer to use during wallet funding.",
			"maintainTier": "Whether to maintain the trading tier established by this bond.",
			"maxBondedAmt": "The maximum amount that may be locked in bonds.",
			"cert":         "The TLS certificate path. Only applicable when registering.",
		},
		returns: `Returns:
    {
      "bondID" (string): The bond transactions's txid and output index.
      "reqConfirms" (int): The number of confirmations required to start trading.
    }`,
	},
	bondOptionsRoute: {
		paramsType: reflect.TypeOf(core.BondOptionsForm{}),
		summary:    `Change bond options for a DEX.`,
		fieldDescs: map[string]string{
			"host":         descHost,
			"targetTier":   "The target trading tier.",
			"maxBondedAmt": "The maximum amount that may be locked in bonds.",
			"bondAssetID":  "The asset ID with which to auto-post bonds.",
			"penaltyComps": "The maximum number of penalties to compensate.",
		},
		returns: `Returns: "ok"`,
	},
	exchangesRoute: {
		summary: `Detailed information about known exchanges and markets.`,
		returns: `Returns:
    obj: The exchanges result.
    {
      "[DEX host]": {
        "acctID" (string): The client's account ID associated with this DEX.
        "markets": {
          "[assetID-assetID]": {
            "baseid" (int): The base asset ID
            "basesymbol" (string): The base ticker symbol.
            "quoteid" (int): The quote asset ID.
            "quotesymbol" (string): The quote asset ID symbol.
            "epochlen" (int): Duration of an epoch in milliseconds.
            "startepoch" (int): Time of start of the last epoch in milliseconds
              since 00:00:00 Jan 1 1970.
            "buybuffer" (float): The minimum order size for a market buy order.
          },...
        },
        "assets": {
          "[assetID]": {
            "symbol" (string): The asset's coin symbol.
            "lotSize" (int): The amount of units of a coin in one lot.
            "rateStep" (int): the price rate increment in atoms.
            "feeRate" (int): The transaction fee in atoms per byte.
            "swapSize" (int): The size of a swap transaction in bytes.
            "swapSizeBase" (int): The size of a swap transaction minus inputs in bytes.
            "swapConf" (int): The number of confirmations needed to confirm
              trade transactions.
          },...
        },
        "regFees": {
          "[assetSymbol]": {
            "id" (int): The asset's BIP-44 coin ID.
            "confs" (int): The number of confirmations required.
            "amt" (int): The fee amount.
          },...
        }
      },...
    }`,
	},
	loginRoute: {
		paramsType: reflect.TypeOf(LoginParams{}),
		summary:    `Attempt to login to all registered DEX servers.`,
		fieldDescs: map[string]string{
			"appPass": descAppPass,
		},
		returns: `Returns:
    obj: A map of notifications and dexes.
    {
      "notifications" (array): An array of most recent notifications.
      [
        {
          "type" (string): The notification type.
          "subject" (string): A clarification of type.
          "details"(string): The notification details.
          "severity" (int): The importance of the notification on a scale of 0
            through 5.
          "stamp" (int): Unix time of the notification. Seconds since 00:00:00 Jan 1 1970.
          "acked" (bool): Whether the notification was acknowledged.
          "id" (string): A unique hex ID.
        },...
      ],
      "dexes" (array): An array of login attempted dexes.
      [
        {
          "host" (string): The DEX address.
          "acctID" (string): A unique hex ID.
          "authed" (bool): Whether the dex has been successfully authed.
          "autherr" (string): Omitted if authed. If not authed, the reason.
          "tradeIDs" (array): An array of active trade IDs.
        },...
      ]
    }`,
	},
	tradeRoute: {
		paramsType: reflect.TypeOf(TradeParams{}),
		summary:    `Make an order to buy or sell an asset.`,
		fieldDescs: map[string]string{
			"appPass": descAppPass,
			"host":    "The DEX to trade on.",
			"isLimit": "Whether the order is a limit order.",
			"sell":    "Whether the order is selling.",
			"base":    descBase,
			"quote":   descQuote,
			"qty":     "The number of units to buy/sell. Must be a multiple of the lot size.",
			"rate": `The atoms quote asset to pay/accept per unit base asset. e.g.
      156000 satoshi/DCR for the DCR(base)_BTC(quote).`,
			"tifnow":  "Require immediate match. Do not book the order.",
			"options": "A JSON-encoded string->string mapping of additional trade options.",
		},
		returns: `Returns:
    obj: The order details.
    {
      "orderid" (string): The order's unique hex identifier.
      "sig" (string): The DEX's signature of the order information.
      "stamp" (int): The time the order was signed in milliseconds since 00:00:00
        Jan 1 1970.
    }`,
	},
	multiTradeRoute: {
		paramsType: reflect.TypeOf(MultiTradeParams{}),
		summary:    `Place multiple orders in one go.`,
		fieldDescs: map[string]string{
			"appPass":   descAppPass,
			"host":      "The DEX to trade on.",
			"sell":      "Whether the order is selling.",
			"base":      descBase,
			"quote":     descQuote,
			"placement": "An array of [qty,rate] placements.",
			"options":   "A JSON-encoded string->string mapping of additional trade options.",
			"maxLock":   "The maximum amount the wallet can lock for this order. 0 means no limit.",
		},
		returns: `Returns:
    obj: The details of each order.
    [{
      "orderid" (string): The order's unique hex identifier.
      "sig" (string): The DEX's signature of the order information.
      "stamp" (int): The time the order was signed in milliseconds since 00:00:00
        Jan 1 1970.
    }]`,
	},
	cancelRoute: {
		paramsType: reflect.TypeOf(CancelParams{}),
		summary:    `Cancel an order.`,
		fieldDescs: map[string]string{
			"orderID": "The hex ID of the order to cancel.",
		},
		returns: `Returns:
    string: The message "` + fmt.Sprintf(canceledOrderStr, "[order ID]") + `"`,
	},
	rescanWalletRoute: {
		paramsType: reflect.TypeOf(RescanWalletParams{}),
		summary: `Initiate a rescan of an asset's wallet. This is only supported for certain
wallet types. Wallet resynchronization may be asynchronous, and the wallet
state should be consulted for progress.

WARNING: It is ill-advised to initiate a wallet rescan with active orders
unless as a last ditch effort to get the wallet to recognize a transaction
needed to complete a swap.`,
		fieldDescs: map[string]string{
			"assetID": descAssetID,
			"force":   "Force a wallet rescan even if there are active orders.",
		},
		returns: `Returns:
    string: "started"`,
	},
	abandonTxRoute: {
		paramsType: reflect.TypeOf(AbandonTxParams{}),
		summary: `Abandon an unconfirmed transaction. This marks the transaction and all
its descendants as abandoned, allowing the wallet to forget about it and
potentially spend its inputs in a different transaction. This is useful when
a transaction gets stuck (e.g., due to low fees).

Note: This only works for unconfirmed transactions. Returns an error if the
transaction is confirmed or does not exist in the wallet. Currently only
supported for DCR wallets.`,
		fieldDescs: map[string]string{
			"assetID": descAssetID,
			"txID":    "The transaction ID (hash) to abandon.",
		},
		returns: `Returns:
    string: Success message on completion.`,
	},
	withdrawRoute: {
		paramsType: reflect.TypeOf(SendParams{}),
		summary:    `Withdraw value from an exchange wallet to address. Fees are subtracted from the value.`,
		fieldDescs: map[string]string{
			"appPass":  descAppPass,
			"assetID":  descAssetID,
			"value":    "The amount to withdraw in units of the asset's smallest denomination (e.g. satoshis, atoms, etc.)",
			"address":  "The address to which withdrawn funds are sent.",
			"subtract": "Ignored for withdraw (always true). Use 'send' route for exact-amount sends.",
		},
		returns: `Returns:
    string: "[coin ID]"`,
	},
	sendRoute: {
		paramsType: reflect.TypeOf(SendParams{}),
		summary:    `Sends exact value from an exchange wallet to address.`,
		fieldDescs: map[string]string{
			"appPass":  descAppPass,
			"assetID":  descAssetID,
			"value":    "The amount to send in units of the asset's smallest denomination (e.g. satoshis, atoms, etc.)",
			"address":  "The address to which funds are sent.",
			"subtract": "Whether to subtract the tx fee from the value.",
		},
		returns: `Returns:
    string: "[coin ID]"`,
	},
	logoutRoute: {
		summary: `Logout of Bison Wallet.`,
		returns: `Returns:
    string: The message "` + logoutStr + `"`,
	},
	orderBookRoute: {
		paramsType: reflect.TypeOf(OrderBookParams{}),
		summary:    `Retrieve all orders for a market.`,
		fieldDescs: map[string]string{
			"host":    "The DEX to retrieve the order book from.",
			"base":    descBase,
			"quote":   descQuote,
			"nOrders": "The number of orders from the top of buys and sells to return. 0 returns all. Epoch orders are not truncated.",
		},
		returns: `Returns:
    obj: A map of orders.
    {
      "sells" (array): An array of booked sell orders.
      [
        {
          "qty" (float): The number of coins base asset being sold.
          "rate" (float): The coins quote asset to pay per coin base asset.
          "sell" (bool): Always true because this is a sell order.
          "token" (string): The first 8 bytes of the order id, coded in hex.
        },...
      ],
      "buys" (array): An array of booked buy orders.
      [
        {
          "qty" (float): The number of coins base asset being bought.
          "rate" (float): The coins quote asset to accept per coin base asset.
          "sell" (bool): Always false because this is a buy order.
          "token" (string): The first 8 bytes of the order id, coded in hex.
        },...
      ],
      "epoch" (array): An array of epoch orders. Epoch orders include all kinds
        of orders, even those that cannot or may not be booked. They are not
        truncated.
      [
        {
          "qty" (float): The number of coins base asset being bought or sold.
          "rate" (float): The coins quote asset to accept per coin base asset.
          "sell" (bool): Whether this order is a sell order.
          "token" (string): The first 8 bytes of the order id, coded in hex.
          "epoch" (int): The order's epoch.
        },...
      ],
    }`,
	},
	myOrdersRoute: {
		paramsType: reflect.TypeOf(MyOrdersParams{}),
		summary: `Fetch all active and recently executed orders
    belonging to the user.`,
		fieldDescs: map[string]string{
			"host":  "The DEX to show orders from.",
			"base":  descBase,
			"quote": descQuote,
		},
		returns: `Returns:
    array: An array of orders.
    [
      {
        "host" (string): The DEX address.
        "marketName" (string): The market's name. e.g. "DCR_BTC".
        "baseID" (int): The market's base asset BIP-44 coin index. e.g. 42 for DCR.
        "quoteID" (int): The market's quote asset BIP-44 coin index. e.g. 0 for BTC.
        "id" (string): The order's unique hex ID.
        "type" (string): The type of order. "limit", "market", or "cancel".
        "sell" (string): Whether this order is selling.
        "stamp" (int): Server's time stamp of the order in milliseconds since 00:00:00 Jan 1 1970.
        "submitTime" (int): Time of order submission, also in milliseconds.
        "age" (string): The time that this order has been active in human readable form.
        "rate" (int): The exchange rate limit. Limit orders only. Units: quote
          asset per unit base asset.
        "quantity" (int): The amount being traded.
        "filled" (int): The order quantity that has matched.
        "settled" (int): The sum quantity of all completed matches.
        "status" (string): The status of the order. "epoch", "booked", "executed",
          "canceled", or "revoked".
        "cancelling" (bool): Whether this order is in the process of cancelling.
        "canceled" (bool): Whether this order has been canceled.
        "tif" (string): "immediate" if this limit order will only match for one epoch.
          "standing" if the order can continue matching until filled or cancelled.
        "matches": (array): An array of matches associated with the order.
        [
          {
            "matchID (string): The match's ID."
            "status" (string): The match's status."
            "revoked" (bool): Indicates if match was revoked.
            "rate"    (int): The match's rate.
            "qty"     (int): The match's amount.
            "side"    (string): The match's side, "maker" or "taker".
            "feerate" (int): The match's fee rate.
            "swap"    (string): The match's swap transaction.
            "counterSwap" (string): The match's counter swap transaction.
            "redeem" (string): The match's redeem transaction.
            "counterRedeem" (string): The match's counter redeem transaction.
            "refund" (string): The match's refund transaction.
            "stamp" (int): The match's stamp.
            "isCancel" (bool): Indicates if match is canceled.
          },...
        ]
      },...
    ]`,
	},
	appSeedRoute: {
		paramsType: reflect.TypeOf(AppSeedParams{}),
		summary: `Show the application's seed. It is recommended to not store the seed
    digitally. Make a copy on paper with pencil and keep it safe.`,
		fieldDescs: map[string]string{
			"appPass": descAppPass,
		},
		returns: `Returns:
    string: The application's seed as hex.`,
	},
	walletPeersRoute: {
		paramsType: reflect.TypeOf(WalletPeersParams{}),
		summary:    `Show the peers a wallet is connected to.`,
		fieldDescs: map[string]string{
			"assetID": descAssetID,
		},
		returns: `Returns:
    []string: Addresses of wallet peers.`,
	},
	addWalletPeerRoute: {
		paramsType: reflect.TypeOf(AddRemovePeerParams{}),
		summary:    `Add a new wallet peer connection.`,
		fieldDescs: map[string]string{
			"assetID": descAssetID,
			"address": "The peer's address (host:port).",
		},
	},
	removeWalletPeerRoute: {
		paramsType: reflect.TypeOf(AddRemovePeerParams{}),
		summary:    `Remove an added wallet peer.`,
		fieldDescs: map[string]string{
			"assetID": descAssetID,
			"address": "The peer's address (host:port).",
		},
	},
	notificationsRoute: {
		paramsType: reflect.TypeOf(NotificationsParams{}),
		summary:    `See recent notifications.`,
		fieldDescs: map[string]string{
			"n": "The number of notifications to load.",
		},
	},
	startBotRoute: {
		paramsType: reflect.TypeOf(StartBotParams{}),
		summary:    `Start market making bot(s).`,
		fieldDescs: map[string]string{
			"appPass":     descAppPass,
			"cfgFilePath": "The path to the market maker config file.",
			"market":      "The market to start a bot for. If not provided, starts all bots in config.",
		},
	},
	stopBotRoute: {
		paramsType: reflect.TypeOf(StopBotParams{}),
		summary:    `Stop market making bot(s).`,
		fieldDescs: map[string]string{
			"market": "The market to stop a bot for. If not provided, stops all running bots.",
		},
	},
	mmAvailableBalancesRoute: {
		paramsType: reflect.TypeOf(MMAvailableBalancesParams{}),
		summary:    `Get available balances for starting a bot or adding additional balance to a running bot.`,
		fieldDescs: map[string]string{
			"host":       descHost,
			"baseID":     "The base asset's BIP-44 registered coin index.",
			"quoteID":    "The quote asset's BIP-44 registered coin index.",
			"cexBaseID":  "The CEX base asset's BIP-44 registered coin index for bridging.",
			"cexQuoteID": "The CEX quote asset's BIP-44 registered coin index for bridging.",
			"cexName":    "The name of the CEX to get balances for.",
		},
	},
	mmStatusRoute: {
		summary: `Get market making status.`,
	},
	updateRunningBotCfgRoute: {
		paramsType: reflect.TypeOf(UpdateRunningBotParams{}),
		summary:    `Update the config and optionally the inventory of a running bot.`,
		fieldDescs: map[string]string{
			"cfgFilePath": "The path to the market maker config file.",
			"market":      "The market for the bot to update.",
			"balances":    "The inventory adjustments.",
		},
	},
	updateRunningBotInvRoute: {
		paramsType: reflect.TypeOf(UpdateRunningBotInventoryParams{}),
		summary:    `Update the inventory of a running bot.`,
		fieldDescs: map[string]string{
			"market":   "The market for the bot to update.",
			"balances": "The inventory adjustments.",
		},
	},
	stakeStatusRoute: {
		paramsType: reflect.TypeOf(StakeStatusParams{}),
		summary:    `Get stake status.`,
		fieldDescs: map[string]string{
			"assetID": descAssetID,
		},
		returns: `Returns:
    obj: The staking status.
    {
      ticketPrice (uint64): The current ticket price in atoms.
      vsp (string): The url of the currently set vsp (voting service provider).
      isRPC (bool): Whether the wallet is an RPC wallet. False indicates
        an spv wallet and enables options to view and set the vsp.
      tickets (array): An array of ticket objects.
      [
        {
          tx (obj): Ticket transaction data.
          {
            hash (string): The ticket hash as hex.
            ticketPrice (int): The amount paid for the ticket in atoms.
            fees (int): The ticket transaction's tx fee.
            stamp (int): The UNIX time the ticket was purchased.
            blockHeight (int): The block number the ticket was mined.
          },
          status (int): The ticket status. 0: unknown, 1: unmined, 2: immature,
            3: live, 4: voted, 5: missed, 6: expired, 7: unspent, 8: revoked.
          spender (string): The transaction that votes on or revokes the ticket if available.
        },...
      ]
      stances (obj): Voting policies.
      {
        agendas (array): An array of consensus vote choices.
        [
          {
            id (string): The agenda ID.
            description (string): A description of the agenda being voted on.
            currentChoice (string): Your current choice.
            choices (array): A description of the available choices.
          },...
        ]
        tspends (array): An array of TSpend policies.
        [
          {
            hash (string): The TSpend txid.
            value (int): The total value sent in the tspend.
            currentValue (string): The policy.
          },...
        ]
        treasuryKeys (array): An array of treasury policies.
        [
          {
            key (string): The pubkey of the tspend creator.
            policy (string): The policy.
          },...
        ]
      }
    }`,
	},
	setVSPRoute: {
		paramsType: reflect.TypeOf(SetVSPParams{}),
		summary:    `Set a vsp by url.`,
		fieldDescs: map[string]string{
			"assetID": descAssetID,
			"addr":    "The vsp's url.",
		},
		returns: `Returns:
    string: The message "` + fmt.Sprintf(setVSPStr, "[vsp url]") + `"`,
	},
	purchaseTicketsRoute: {
		paramsType: reflect.TypeOf(PurchaseTicketsParams{}),
		summary:    `Start an asynchronous ticket purchasing process. Check stakestatus for number of tickets remaining to be purchased.`,
		fieldDescs: map[string]string{
			"appPass": descAppPass,
			"assetID": descAssetID,
			"n":       "The number of tickets to purchase.",
		},
		returns: `Returns:
    bool: true is the only non-error return value`,
	},
	setVotingPreferencesRoute: {
		paramsType: reflect.TypeOf(SetVotingPreferencesParams{}),
		summary:    `Set voting preferences.`,
		fieldDescs: map[string]string{
			"assetID":        descAssetID,
			"choices":        "A map of agenda IDs to choice IDs.",
			"tSpendPolicy":   "A map of tSpend txids to tSpend policies.",
			"treasuryPolicy": "A map of treasury spender public keys to tSpend policies.",
		},
		returns: `Returns:
    string: The message "` + setVotePrefsStr + `"`,
	},
	txHistoryRoute: {
		paramsType: reflect.TypeOf(TxHistoryParams{}),
		summary:    `Get transaction history for a wallet.`,
		fieldDescs: map[string]string{
			"assetID": descAssetID,
			"n":       "The number of transactions to return. If <= 0 or unset, all transactions are returned.",
			"refID":   "If set, the transactions before or after this tx (depending on the past argument) will be returned.",
			"past":    "If true, the transactions before the reference tx will be returned.",
		},
	},
	walletTxRoute: {
		paramsType: reflect.TypeOf(WalletTxParams{}),
		summary:    `Get a wallet transaction.`,
		fieldDescs: map[string]string{
			"assetID": descAssetID,
			"txID":    "The transaction ID.",
		},
	},
	withdrawBchSpvRoute: {
		paramsType: reflect.TypeOf(BchWithdrawParams{}),
		summary:    `Get a transaction that will withdraw all funds from the deprecated Bitcoin Cash SPV wallet.`,
		fieldDescs: map[string]string{
			"appPass":   descAppPass,
			"recipient": "The Bitcoin Cash address to withdraw the funds to.",
		},
	},
	bridgeRoute: {
		paramsType: reflect.TypeOf(BridgeParams{}),
		summary:    "Bridge tokens from one chain to another.",
		fieldDescs: map[string]string{
			"fromAssetID": descFromAssetID,
			"toAssetID":   descToAssetID,
			"amt":         "The amount of tokens to bridge.",
			"bridgeName":  descBridgeName,
		},
	},
	checkBridgeApprovalRoute: {
		paramsType: reflect.TypeOf(CheckBridgeApprovalParams{}),
		summary:    "Check if the bridge contract is approved.",
		fieldDescs: map[string]string{
			"assetID":    descFromAssetID,
			"bridgeName": descBridgeName,
		},
	},
	approveBridgeContractRoute: {
		paramsType: reflect.TypeOf(ApproveBridgeParams{}),
		summary:    "Approve or unapprove the bridge contract.",
		fieldDescs: map[string]string{
			"assetID":    descFromAssetID,
			"bridgeName": descBridgeName,
			"approve":    "True to approve, false to unapprove.",
		},
	},
	pendingBridgesRoute: {
		paramsType: reflect.TypeOf(PendingBridgesParams{}),
		summary:    "Get pending bridges.",
		fieldDescs: map[string]string{
			"assetID": descFromAssetID,
		},
	},
	bridgeHistoryRoute: {
		paramsType: reflect.TypeOf(TxHistoryParams{}),
		summary:    "Get bridge history.",
		fieldDescs: map[string]string{
			"assetID": descFromAssetID,
			"n":       "The number of transactions to return. If <= 0 or unset, all transactions are returned.",
			"refID":   "If set, the transactions before or after this tx (depending on the past argument) will be returned.",
			"past":    "If true, the transactions before the reference tx will be returned.",
		},
	},
	supportedBridgesRoute: {
		paramsType: reflect.TypeOf(SupportedBridgesParams{}),
		summary:    "Get supported bridge destinations.",
		fieldDescs: map[string]string{
			"assetID": "The asset's BIP-44 registered coin index to get bridge destinations for.",
		},
	},
	bridgeFeesAndLimitsRoute: {
		paramsType: reflect.TypeOf(BridgeFeesAndLimitsParams{}),
		summary:    "Get bridge fees and limits.",
		fieldDescs: map[string]string{
			"fromAssetID": descFromAssetID,
			"toAssetID":   descToAssetID,
			"bridgeName":  descBridgeName,
		},
	},
	paymentMultisigPubkeyRoute: {
		paramsType: reflect.TypeOf(PaymentMultisigPubkeyParams{}),
		summary:    "Get a multisig pubkey for asset id.",
		fieldDescs: map[string]string{
			"assetID": descAssetID,
		},
		returns: `Returns:
    string: a pubkey. The index is stored in the db and this pubkey will not be returned again.`,
	},
	sendFundsToMultisigRoute: {
		paramsType: reflect.TypeOf(CsvFileParams{}),
		summary:    "Send funds to a payment multisig.",
		fieldDescs: map[string]string{
			"csvFilePath": descCsvFilePath + " Will be written to.",
		},
	},
	signMultisigRoute: {
		paramsType: reflect.TypeOf(SignMultisigParams{}),
		summary:    "Sign a payment multisig.",
		fieldDescs: map[string]string{
			"csvFilePath": descCsvFilePath + " Will be written to.",
			"signIdx":     "The pubkey index we own and are able to sign.",
		},
	},
	refundPaymentMultisigRoute: {
		paramsType: reflect.TypeOf(CsvFileParams{}),
		summary:    "Refund a payment multisig.",
		fieldDescs: map[string]string{
			"csvFilePath": descCsvFilePath,
		},
		returns: `Returns:
    string: the refund tx hash`,
	},
	viewPaymentMultisigRoute: {
		paramsType: reflect.TypeOf(CsvFileParams{}),
		summary:    "View a payment multisig.",
		fieldDescs: map[string]string{
			"csvFilePath": descCsvFilePath,
		},
		returns: `Returns:
    string: tx in json format`,
	},
	sendPaymentMultisigRoute: {
		paramsType: reflect.TypeOf(CsvFileParams{}),
		summary:    "Send a payment multisig. May not error even with spv if not fully signed.",
		fieldDescs: map[string]string{
			"csvFilePath": descCsvFilePath,
		},
		returns: `Returns:
    string: the sent tx hash`,
	},
	mmReportRoute: {
		paramsType: reflect.TypeOf(MMReportParams{}),
		summary:    "Export MM epoch snapshots for a market to a JSON file.",
		fieldDescs: map[string]string{
			"host":       "The DEX host.",
			"baseID":     "The base asset BIP ID.",
			"quoteID":    "The quote asset BIP ID.",
			"startEpoch": "The start epoch index.",
			"endEpoch":   "The end epoch index.",
			"outFile":    "The output file path.",
		},
		returns: `Returns:
    string: the output file path`,
	},
	pruneMMSnapshotsRoute: {
		paramsType: reflect.TypeOf(PruneMMSnapshotsParams{}),
		summary:    "Delete MM epoch snapshots for a market older than a given epoch index.",
		fieldDescs: map[string]string{
			"host":        "The DEX host.",
			"baseID":      "The base asset BIP ID.",
			"quoteID":     "The quote asset BIP ID.",
			"minEpochIdx": "Delete snapshots with epochIdx strictly less than this value.",
		},
		returns: `Returns:
    int: the number of snapshots deleted`,
	},
}

// parseJSONTag splits a struct field's json tag into name and options.
func parseJSONTag(tag string) (name, opts string) {
	if idx := strings.IndexByte(tag, ','); idx != -1 {
		return tag[:idx], tag[idx+1:]
	}
	return tag, ""
}

// goTypeToHelpType maps Go reflect.Type to a human-readable type name for
// help output.
func goTypeToHelpType(t reflect.Type) string {
	if t == passBytesType {
		return "password"
	}
	switch t.Kind() {
	case reflect.String:
		return "string"
	case reflect.Bool:
		return "bool"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return "int"
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "int"
	case reflect.Float32, reflect.Float64:
		return "float"
	case reflect.Map:
		return "object"
	case reflect.Struct:
		return "object"
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 {
			return "string" // []byte
		}
		return "array"
	case reflect.Ptr:
		return goTypeToHelpType(t.Elem())
	case reflect.Interface:
		return "any"
	default:
		return t.Kind().String()
	}
}

// reflectFields walks a struct type and returns field metadata in declaration
// order. Embedded structs are recursively flattened. Pointer-to-struct named
// fields are shown as a single "object" field.
func reflectFields(t reflect.Type, descs map[string]string) []fieldInfo {
	if t == nil {
		return nil
	}
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil
	}
	var fields []fieldInfo
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)

		// Embedded (anonymous) struct: recurse and flatten.
		if sf.Anonymous {
			embedded := sf.Type
			if embedded.Kind() == reflect.Ptr {
				embedded = embedded.Elem()
			}
			if embedded.Kind() == reflect.Struct {
				fields = append(fields, reflectFields(embedded, descs)...)
				continue
			}
		}

		tag := sf.Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		jsonName, tagOpts := parseJSONTag(tag)
		if jsonName == "" {
			continue
		}

		isPassword := sf.Type == passBytesType
		isOptional := sf.Type.Kind() == reflect.Ptr || strings.Contains(tagOpts, "omitempty")

		typeName := goTypeToHelpType(sf.Type)

		fields = append(fields, fieldInfo{
			jsonName:   jsonName,
			typeName:   typeName,
			goType:     sf.Type,
			isPassword: isPassword,
			isOptional: isOptional,
			desc:       descs[jsonName],
		})
	}
	return fields
}

// ListCommands prints a short usage string for every route available to the
// rpcserver.
func ListCommands(includePasswords bool) string {
	var sb strings.Builder
	for _, r := range sortRouteInfoKeys() {
		info := routeInfos[r]
		fields := reflectFields(info.paramsType, info.fieldDescs)
		var parts []string
		for _, f := range fields {
			if f.isPassword && !includePasswords {
				continue
			}
			name := f.jsonName
			if f.isOptional {
				name += "?"
			}
			parts = append(parts, name)
		}
		if len(parts) > 0 {
			sb.WriteString(fmt.Sprintf("%s {%s}\n", r, strings.Join(parts, ", ")))
		} else {
			sb.WriteString(r + "\n")
		}
	}
	s := sb.String()
	if len(s) > 0 {
		s = s[:len(s)-1] // Remove trailing newline.
	}
	return s
}

// commandUsage returns a help message for cmd or an error if cmd is unknown.
func commandUsage(cmd string, includePasswords bool) (string, error) {
	info, exists := routeInfos[cmd]
	if !exists {
		return "", fmt.Errorf("%w: %s", errUnknownCmd, cmd)
	}

	fields := reflectFields(info.paramsType, info.fieldDescs)

	// Build short args line.
	var parts []string
	for _, f := range fields {
		if f.isPassword && !includePasswords {
			continue
		}
		name := f.jsonName
		if f.isOptional {
			name += "?"
		}
		parts = append(parts, name)
	}
	var sb strings.Builder
	if len(parts) > 0 {
		sb.WriteString(fmt.Sprintf("%s {%s}", cmd, strings.Join(parts, ", ")))
	} else {
		sb.WriteString(cmd)
	}

	// Summary
	sb.WriteString("\n\n")
	sb.WriteString(info.summary)

	// Params
	var paramFields []fieldInfo
	for _, f := range fields {
		if f.isPassword && !includePasswords {
			continue
		}
		paramFields = append(paramFields, f)
	}
	if len(paramFields) > 0 {
		sb.WriteString("\n\nParams:")
		for _, f := range paramFields {
			optStr := ""
			if f.isOptional {
				optStr = ", optional"
			}
			sb.WriteString(fmt.Sprintf("\n    %s (%s%s): %s", f.jsonName, f.typeName, optStr, f.desc))
		}
	}

	// Returns
	if info.returns != "" {
		sb.WriteString("\n\n")
		sb.WriteString(info.returns)
	}

	// Extra dynamic help.
	if info.extraHelp != nil {
		sb.WriteString("\n\n")
		sb.WriteString(info.extraHelp())
	}

	return sb.String(), nil
}

// walletTypesHelp returns a formatted list of available wallet types for each
// registered asset.
func walletTypesHelp() string {
	assets := asset.Assets()
	type assetEntry struct {
		id     uint32
		symbol string
		types  []string
	}
	var entries []assetEntry
	for _, ra := range assets {
		var types []string
		for _, wd := range ra.Info.AvailableWallets {
			if wd.Type != "" {
				types = append(types, wd.Type)
			}
		}
		if len(types) > 0 {
			slices.Sort(types)
			entries = append(entries, assetEntry{ra.ID, ra.Symbol, types})
		}
	}
	if len(entries) == 0 {
		return ""
	}
	slices.SortFunc(entries, func(a, b assetEntry) int {
		if a.id < b.id {
			return -1
		}
		if a.id > b.id {
			return 1
		}
		return 0
	})
	var sb strings.Builder
	sb.WriteString("Available wallet types:")
	for _, e := range entries {
		sb.WriteString(fmt.Sprintf("\n    %s (%d): %s", e.symbol, e.id, strings.Join(e.types, ", ")))
	}
	return sb.String()
}

func handleMMReport(s *RPCServer, msg *msgjson.Message) *msgjson.ResponsePayload {
	var params MMReportParams
	if err := msg.Unmarshal(&params); err != nil {
		return usage(mmReportRoute, err)
	}
	if params.OutFile == "" {
		resErr := msgjson.NewError(msgjson.RPCParseError, "outFile is required")
		return createResponse(mmReportRoute, nil, resErr)
	}
	outFile := filepath.Clean(params.OutFile)
	snaps, err := s.core.ExportMMSnapshots(params.Host, params.BaseID, params.QuoteID, params.StartEpoch, params.EndEpoch)
	if err != nil {
		resErr := msgjson.NewError(msgjson.RPCInternal, "error exporting snapshots: %v", err)
		return createResponse(mmReportRoute, nil, resErr)
	}
	b, err := json.MarshalIndent(snaps, "", "  ")
	if err != nil {
		resErr := msgjson.NewError(msgjson.RPCInternal, "error marshaling snapshots: %v", err)
		return createResponse(mmReportRoute, nil, resErr)
	}
	if err := os.WriteFile(outFile, b, 0644); err != nil {
		resErr := msgjson.NewError(msgjson.RPCInternal, "error writing file: %v", err)
		return createResponse(mmReportRoute, nil, resErr)
	}
	return createResponse(mmReportRoute, &outFile, nil)
}

func handlePruneMMSnapshots(s *RPCServer, msg *msgjson.Message) *msgjson.ResponsePayload {
	var params PruneMMSnapshotsParams
	if err := msg.Unmarshal(&params); err != nil {
		return usage(pruneMMSnapshotsRoute, err)
	}
	if params.MinEpochIdx == 0 {
		resErr := msgjson.NewError(msgjson.RPCParseError, "minEpochIdx is required")
		return createResponse(pruneMMSnapshotsRoute, nil, resErr)
	}
	n, err := s.core.PruneMMSnapshots(params.Host, params.BaseID, params.QuoteID, params.MinEpochIdx)
	if err != nil {
		resErr := msgjson.NewError(msgjson.RPCInternal, "error pruning snapshots: %v", err)
		return createResponse(pruneMMSnapshotsRoute, nil, resErr)
	}
	return createResponse(pruneMMSnapshotsRoute, &n, nil)
}

func handleDeployContract(s *RPCServer, msg *msgjson.Message) *msgjson.ResponsePayload {
	var params DeployContractParams
	if err := msg.Unmarshal(&params); err != nil {
		return usage(deployContractRoute, err)
	}
	defer params.AppPass.Clear()

	assetIDs := make([]uint32, 0, len(params.Chains))
	for _, chain := range params.Chains {
		assetID, found := dex.BipSymbolID(chain)
		if !found {
			resErr := msgjson.NewError(msgjson.RPCArgumentsError, "unknown chain %q", chain)
			return createResponse(deployContractRoute, nil, resErr)
		}
		assetIDs = append(assetIDs, assetID)
	}
	if len(assetIDs) == 0 {
		resErr := msgjson.NewError(msgjson.RPCArgumentsError, "no chains specified")
		return createResponse(deployContractRoute, nil, resErr)
	}

	var txData []byte
	switch {
	case params.Bytecode != nil && *params.Bytecode != "":
		var err error
		txData, err = hex.DecodeString(strings.TrimPrefix(*params.Bytecode, "0x"))
		if err != nil {
			resErr := msgjson.NewError(msgjson.RPCArgumentsError, "invalid bytecode hex: %v", err)
			return createResponse(deployContractRoute, nil, resErr)
		}
	case params.ContractVer == nil:
		resErr := msgjson.NewError(msgjson.RPCArgumentsError, "either bytecode or contractVer must be specified")
		return createResponse(deployContractRoute, nil, resErr)
	}

	var tokenAddr string
	if params.TokenAddress != nil {
		tokenAddr = *params.TokenAddress
	}

	results, err := s.core.DeployContract(params.AppPass, assetIDs, txData, params.ContractVer, tokenAddr)
	if err != nil {
		resErr := msgjson.NewError(msgjson.RPCDeployContractError, "deploy contract error: %v", err)
		return createResponse(deployContractRoute, nil, resErr)
	}
	return createResponse(deployContractRoute, results, nil)
}

// sortRouteInfoKeys returns a sorted list of routeInfos keys.
func sortRouteInfoKeys() []string {
	keys := make([]string, 0, len(routeInfos))
	for k := range routeInfos {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
