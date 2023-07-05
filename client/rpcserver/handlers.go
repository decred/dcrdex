// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package rpcserver

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"sort"
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
	registerRoute              = "register"
	postBondRoute              = "postbond"
	bondOptionsRoute           = "bondopts"
	tradeRoute                 = "trade"
	versionRoute               = "version"
	walletsRoute               = "wallets"
	rescanWalletRoute          = "rescanwallet"
	withdrawRoute              = "withdraw"
	sendRoute                  = "send"
	appSeedRoute               = "appseed"
	deleteArchivedRecordsRoute = "deletearchivedrecords"
	walletPeersRoute           = "walletpeers"
	addWalletPeerRoute         = "addwalletpeer"
	removeWalletPeerRoute      = "removewalletpeer"
	notificationsRoute         = "notifications"
	startMarketMakingRoute     = "startmarketmaking"
	stopMarketMakingRoute      = "stopmarketmaking"
	multiTradeRoute            = "multitrade"
)

const (
	initializedStr    = "app initialized"
	walletCreatedStr  = "%s wallet created and unlocked"
	walletLockedStr   = "%s wallet locked"
	walletUnlockedStr = "%s wallet unlocked"
	canceledOrderStr  = "canceled order %s"
	logoutStr         = "goodbye"
	walletStatusStr   = "%s wallet has been %s"
)

// createResponse creates a msgjson response payload.
func createResponse(op string, res interface{}, resErr *msgjson.Error) *msgjson.ResponsePayload {
	encodedRes, err := json.Marshal(res)
	if err != nil {
		err := fmt.Errorf("unable to marshal data for %s: %w", op, err)
		panic(err)
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
var routes = map[string]func(s *RPCServer, params *RawParams) *msgjson.ResponsePayload{
	cancelRoute:                handleCancel,
	closeWalletRoute:           handleCloseWallet,
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
	registerRoute:              handleRegister,
	postBondRoute:              handlePostBond,
	bondOptionsRoute:           handleBondOptions,
	bondAssetsRoute:            handleBondAssets,
	tradeRoute:                 handleTrade,
	versionRoute:               handleVersion,
	walletsRoute:               handleWallets,
	rescanWalletRoute:          handleRescanWallet,
	withdrawRoute:              handleWithdraw,
	sendRoute:                  handleSend,
	appSeedRoute:               handleAppSeed,
	deleteArchivedRecordsRoute: handleDeleteArchivedRecords,
	walletPeersRoute:           handleWalletPeers,
	addWalletPeerRoute:         handleAddWalletPeer,
	removeWalletPeerRoute:      handleRemoveWalletPeer,
	notificationsRoute:         handleNotificationsRoute,
	startMarketMakingRoute:     handleStartMarketMakingRoute,
	stopMarketMakingRoute:      handleStopMarketMakingRoute,
	multiTradeRoute:            handleMultiTrade,
}

// handleHelp handles requests for help. Returns general help for all commands
// if no arguments are passed or verbose help if the passed argument is a known
// command.
func handleHelp(_ *RPCServer, params *RawParams) *msgjson.ResponsePayload {
	form, err := parseHelpArgs(params)
	if err != nil {
		return usage(helpRoute, err)
	}
	res := ""
	if form.helpWith == "" {
		// List all commands if no arguments.
		res = ListCommands(form.includePasswords)
	} else {
		var err error
		res, err = commandUsage(form.helpWith, form.includePasswords)
		if err != nil {
			resErr := msgjson.NewError(msgjson.RPCUnknownRoute,
				err.Error())
			return createResponse(helpRoute, nil, resErr)
		}
	}
	return createResponse(helpRoute, &res, nil)
}

// handleInit handles requests for init. *msgjson.ResponsePayload.Error is empty
// if successful.
func handleInit(s *RPCServer, params *RawParams) *msgjson.ResponsePayload {
	appPass, seed, err := parseInitArgs(params)
	if err != nil {
		return usage(initRoute, err)
	}
	defer func() {
		appPass.Clear()
		seed.Clear()
	}()
	if err := s.core.InitializeClient(appPass, seed); err != nil {
		errMsg := fmt.Sprintf("unable to initialize client: %v", err)
		resErr := msgjson.NewError(msgjson.RPCInitError, errMsg)
		return createResponse(initRoute, nil, resErr)
	}
	res := initializedStr
	return createResponse(initRoute, &res, nil)
}

// handleVersion handles requests for version. It returns the rpc server version
// and dexc version.
func handleVersion(s *RPCServer, _ *RawParams) *msgjson.ResponsePayload {
	result := &VersionResponse{
		RPCServerVer: &dex.Semver{
			Major: rpcSemverMajor,
			Minor: rpcSemverMinor,
			Patch: rpcSemverPatch,
		},
		DexcVersion: s.dexcVersion,
	}

	return createResponse(versionRoute, result, nil)
}

// handleNewWallet handles requests for newwallet.
// *msgjson.ResponsePayload.Error is empty if successful. Returns a
// msgjson.RPCWalletExistsError if a wallet for the assetID already exists.
// Wallet will be unlocked if successful.
func handleNewWallet(s *RPCServer, params *RawParams) *msgjson.ResponsePayload {
	form, err := parseNewWalletArgs(params)
	if err != nil {
		return usage(newWalletRoute, err)
	}

	// zero password params in request payload when done handling this request
	defer func() {
		form.appPass.Clear()
		form.walletPass.Clear()
	}()

	if s.core.WalletState(form.assetID) != nil {
		errMsg := fmt.Sprintf("error creating %s wallet: wallet already exists",
			dex.BipIDSymbol(form.assetID))
		resErr := msgjson.NewError(msgjson.RPCWalletExistsError, errMsg)
		return createResponse(newWalletRoute, nil, resErr)
	}

	walletDef, err := asset.WalletDef(form.assetID, form.walletType)
	if err != nil {
		errMsg := fmt.Sprintf("error creating %s wallet: unable to get wallet definition: %v",
			dex.BipIDSymbol(form.assetID), err)
		resErr := msgjson.NewError(msgjson.RPCWalletDefinitionError, errMsg)
		return createResponse(newWalletRoute, nil, resErr)
	}

	// Apply default config options if they exist.
	for _, opt := range walletDef.ConfigOpts {
		if _, has := form.config[opt.Key]; !has && opt.DefaultValue != nil {
			form.config[opt.Key] = fmt.Sprintf("%v", opt.DefaultValue)
		}
	}

	// Wallet does not exist yet. Try to create it.
	err = s.core.CreateWallet(form.appPass, form.walletPass, &core.WalletForm{
		Type:    form.walletType,
		AssetID: form.assetID,
		Config:  form.config,
	})
	if err != nil {
		errMsg := fmt.Sprintf("error creating %s wallet: %v",
			dex.BipIDSymbol(form.assetID), err)
		resErr := msgjson.NewError(msgjson.RPCCreateWalletError, errMsg)
		return createResponse(newWalletRoute, nil, resErr)
	}

	err = s.core.OpenWallet(form.assetID, form.appPass)
	if err != nil {
		errMsg := fmt.Sprintf("wallet connected, but failed to open with provided password: %v",
			err)
		resErr := msgjson.NewError(msgjson.RPCOpenWalletError, errMsg)
		return createResponse(newWalletRoute, nil, resErr)
	}

	res := fmt.Sprintf(walletCreatedStr, dex.BipIDSymbol(form.assetID))
	return createResponse(newWalletRoute, &res, nil)
}

// handleOpenWallet handles requests for openWallet.
// *msgjson.ResponsePayload.Error is empty if successful. Requires the app
// password. Opens the wallet.
func handleOpenWallet(s *RPCServer, params *RawParams) *msgjson.ResponsePayload {
	form, err := parseOpenWalletArgs(params)
	if err != nil {
		return usage(openWalletRoute, err)
	}
	defer form.appPass.Clear()

	err = s.core.OpenWallet(form.assetID, form.appPass)
	if err != nil {
		errMsg := fmt.Sprintf("error unlocking %s wallet: %v",
			dex.BipIDSymbol(form.assetID), err)
		resErr := msgjson.NewError(msgjson.RPCOpenWalletError, errMsg)
		return createResponse(openWalletRoute, nil, resErr)
	}

	res := fmt.Sprintf(walletUnlockedStr, dex.BipIDSymbol(form.assetID))
	return createResponse(openWalletRoute, &res, nil)
}

// handleCloseWallet handles requests for closeWallet.
// *msgjson.ResponsePayload.Error is empty if successful. Closes the wallet.
func handleCloseWallet(s *RPCServer, params *RawParams) *msgjson.ResponsePayload {
	assetID, err := parseCloseWalletArgs(params)
	if err != nil {
		return usage(closeWalletRoute, err)
	}
	if err := s.core.CloseWallet(assetID); err != nil {
		errMsg := fmt.Sprintf("unable to close wallet %s: %v",
			dex.BipIDSymbol(assetID), err)
		resErr := msgjson.NewError(msgjson.RPCCloseWalletError, errMsg)
		return createResponse(closeWalletRoute, nil, resErr)
	}

	res := fmt.Sprintf(walletLockedStr, dex.BipIDSymbol(assetID))
	return createResponse(closeWalletRoute, &res, nil)
}

// handleToggleWalletStatus handles requests for toggleWalletStatus.
// *msgjson.ResponsePayload.Error is empty if successful. Disables or enables a
// wallet.
func handleToggleWalletStatus(s *RPCServer, params *RawParams) *msgjson.ResponsePayload {
	form, err := parseToggleWalletStatusArgs(params)
	if err != nil {
		return usage(toggleWalletStatusRoute, err)
	}
	if err := s.core.ToggleWalletStatus(form.assetID, form.disable); err != nil {
		errMsg := fmt.Sprintf("unable to change %s wallet status: %v",
			dex.BipIDSymbol(form.assetID), err)
		resErr := msgjson.NewError(msgjson.RPCToggleWalletStatusError, errMsg)
		return createResponse(toggleWalletStatusRoute, nil, resErr)
	}

	status := "enabled"
	if form.disable {
		status = "disabled"
	}

	res := fmt.Sprintf(walletStatusStr, dex.BipIDSymbol(form.assetID), status)
	return createResponse(toggleWalletStatusRoute, &res, nil)
}

// handleWallets handles requests for wallets. Returns a list of wallet details.
func handleWallets(s *RPCServer, _ *RawParams) *msgjson.ResponsePayload {
	walletsStates := s.core.Wallets()
	return createResponse(walletsRoute, walletsStates, nil)
}

// handleBondAssets handles requests for bondassets.
// *msgjson.ResponsePayload.Error is empty if successful. Requires the address
// of a dex and returns the bond expiry and supported asset bond details.
func handleBondAssets(s *RPCServer, params *RawParams) *msgjson.ResponsePayload {
	host, cert, err := parseBondAssetsArgs(params)
	if err != nil {
		return usage(bondAssetsRoute, err)
	}
	exchInf := s.core.Exchanges()
	exchCfg := exchInf[host]
	if exchCfg == nil {
		exchCfg, err = s.core.GetDEXConfig(host, cert) // cert is file contents, not name
		if err != nil {
			resErr := msgjson.NewError(msgjson.RPCGetDEXConfigError, err.Error())
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
func handleGetDEXConfig(s *RPCServer, params *RawParams) *msgjson.ResponsePayload {
	host, cert, err := parseGetDEXConfigArgs(params)
	if err != nil {
		return usage(getDEXConfRoute, err)
	}
	exchange, err := s.core.GetDEXConfig(host, cert) // cert is file contents, not name
	if err != nil {
		resErr := msgjson.NewError(msgjson.RPCGetDEXConfigError, err.Error())
		return createResponse(getDEXConfRoute, nil, resErr)
	}
	return createResponse(getDEXConfRoute, exchange, nil)
}

// handleDiscoverAcct is the handler for discoveracct. *msgjson.ResponsePayload.Error
// is empty if successful.
func handleDiscoverAcct(s *RPCServer, params *RawParams) *msgjson.ResponsePayload {
	form, err := parseDiscoverAcctArgs(params)
	if err != nil {
		return usage(discoverAcctRoute, err)
	}
	defer form.appPass.Clear()
	_, paid, err := s.core.DiscoverAccount(form.addr, form.appPass, form.cert)
	if err != nil {
		resErr := &msgjson.Error{Code: msgjson.RPCDiscoverAcctError, Message: err.Error()}
		return createResponse(discoverAcctRoute, nil, resErr)
	}
	return createResponse(discoverAcctRoute, &paid, nil)
}

// handleRegister handles requests for register. *msgjson.ResponsePayload.Error
// is empty if successful. DEPRECATED BY postbond. (V0PURGE)
func handleRegister(s *RPCServer, params *RawParams) *msgjson.ResponsePayload {
	form, err := parseRegisterArgs(params)
	if err != nil {
		return usage(registerRoute, err)
	}
	defer form.AppPass.Clear()
	assetID := uint32(42)
	if form.Asset != nil {
		assetID = *form.Asset
	}
	exchange, err := s.core.GetDEXConfig(form.Addr, form.Cert)
	if err != nil {
		resErr := &msgjson.Error{Code: msgjson.RPCGetDEXConfigError, Message: err.Error()}
		return createResponse(registerRoute, nil, resErr)
	}
	symb := dex.BipIDSymbol(assetID)
	feeAsset := exchange.RegFees[symb]
	if feeAsset == nil {
		errMsg := fmt.Sprintf("dex does not support asset %v for registration", symb)
		resErr := msgjson.NewError(msgjson.RPCRegisterError, errMsg)
		return createResponse(registerRoute, nil, resErr)
	}
	fee := feeAsset.Amt

	if fee != form.Fee {
		errMsg := fmt.Sprintf("DEX at %s expects a fee of %d but %d was offered", form.Addr, fee, form.Fee)
		resErr := msgjson.NewError(msgjson.RPCRegisterError, errMsg)
		return createResponse(registerRoute, nil, resErr)
	}
	res, err := s.core.Register(form)
	if err != nil {
		resErr := &msgjson.Error{Code: msgjson.RPCRegisterError, Message: err.Error()}
		return createResponse(registerRoute, nil, resErr)
	}
	return createResponse(registerRoute, res, nil)
}

func handleBondOptions(s *RPCServer, params *RawParams) *msgjson.ResponsePayload {
	form, err := parseBondOptsArgs(params)
	if err != nil {
		return usage(bondOptionsRoute, err)
	}
	err = s.core.UpdateBondOptions(form)
	if err != nil {
		resErr := &msgjson.Error{Code: msgjson.RPCPostBondError, Message: err.Error()}
		return createResponse(bondOptionsRoute, nil, resErr)
	}
	return createResponse(bondOptionsRoute, "ok", nil)
}

// handlePostBond handles requests for postbond. *msgjson.ResponsePayload.Error
// is empty if successful.
func handlePostBond(s *RPCServer, params *RawParams) *msgjson.ResponsePayload {
	form, err := parsePostBondArgs(params)
	if err != nil {
		return usage(postBondRoute, err)
	}
	defer form.AppPass.Clear()
	// Get the exchange config with Exchanges(), not GetDEXConfig, since we may
	// already be connected and even with an existing account.
	exchInf := s.core.Exchanges()
	exchCfg := exchInf[form.Addr]
	if exchCfg == nil {
		// Not already registered.
		exchCfg, err = s.core.GetDEXConfig(form.Addr, form.Cert)
		if err != nil {
			resErr := &msgjson.Error{Code: msgjson.RPCGetDEXConfigError, Message: err.Error()}
			return createResponse(registerRoute, nil, resErr)
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
		errMsg := fmt.Sprintf("DEX %s does not support registration with %s", form.Addr, symb)
		resErr := msgjson.NewError(msgjson.RPCPostBondError, errMsg)
		return createResponse(postBondRoute, nil, resErr)
	}
	if bondAsset.Amt > form.Bond || form.Bond%bondAsset.Amt != 0 {
		errMsg := fmt.Sprintf("DEX at %s expects a bond amount in multiples of %d %s but %d was offered",
			form.Addr, bondAsset.Amt, dex.BipIDSymbol(assetID), form.Bond)
		resErr := msgjson.NewError(msgjson.RPCPostBondError, errMsg)
		return createResponse(postBondRoute, nil, resErr)
	}
	res, err := s.core.PostBond(form)
	if err != nil {
		resErr := &msgjson.Error{Code: msgjson.RPCPostBondError, Message: err.Error()}
		return createResponse(postBondRoute, nil, resErr)
	}
	if res.BondID == "" {
		return createResponse(postBondRoute, "existing account configured - no bond posted", nil)
	}
	return createResponse(postBondRoute, res, nil)
}

// handleExchanges handles requests for exchanges. It takes no arguments and
// returns a map of exchanges.
func handleExchanges(s *RPCServer, _ *RawParams) *msgjson.ResponsePayload {
	// Convert something to a map[string]interface{}.
	convM := func(in interface{}) map[string]interface{} {
		var m map[string]interface{}
		b, err := json.Marshal(in)
		if err != nil {
			panic(err)
		}
		if err = json.Unmarshal(b, &m); err != nil {
			panic(err)
		}
		return m
	}
	res := s.core.Exchanges()
	exchanges := convM(res)
	// Iterate through exchanges converting structs into maps in order to
	// remove some fields. Keys are DEX addresses.
	for k, exchange := range exchanges {
		exchangeDetails := convM(exchange)
		// Remove a redundant address field.
		delete(exchangeDetails, "host")
		markets := convM(exchangeDetails["markets"])
		// Market keys are market name.
		for k, market := range markets {
			marketDetails := convM(market)
			// Remove redundant name field.
			delete(marketDetails, "name")
			delete(marketDetails, "orders")
			markets[k] = marketDetails
		}
		assets := convM(exchangeDetails["assets"])
		// Asset keys are assetIDs.
		for k, asset := range assets {
			assetDetails := convM(asset)
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

// handleLogin sets up the dex connections.
func handleLogin(s *RPCServer, params *RawParams) *msgjson.ResponsePayload {
	appPass, err := parseLoginArgs(params)
	if err != nil {
		return usage(loginRoute, err)
	}
	defer appPass.Clear()
	err = s.core.Login(appPass)
	if err != nil {
		errMsg := fmt.Sprintf("unable to login: %v", err)
		resErr := msgjson.NewError(msgjson.RPCLoginError, errMsg)
		return createResponse(loginRoute, nil, resErr)
	}
	res := "successfully logged in"
	return createResponse(loginRoute, &res, nil)
}

// handleTrade handles requests for trade. *msgjson.ResponsePayload.Error is
// empty if successful.
func handleTrade(s *RPCServer, params *RawParams) *msgjson.ResponsePayload {
	form, err := parseTradeArgs(params)
	if err != nil {
		return usage(tradeRoute, err)
	}
	defer form.appPass.Clear()
	res, err := s.core.Trade(form.appPass, form.srvForm)
	if err != nil {
		errMsg := fmt.Sprintf("unable to trade: %v", err)
		resErr := msgjson.NewError(msgjson.RPCTradeError, errMsg)
		return createResponse(tradeRoute, nil, resErr)
	}
	tradeRes := &tradeResponse{
		OrderID: res.ID.String(),
		Sig:     res.Sig.String(),
		Stamp:   res.Stamp,
	}
	return createResponse(tradeRoute, &tradeRes, nil)
}

func handleMultiTrade(s *RPCServer, params *RawParams) *msgjson.ResponsePayload {
	form, err := parseMultiTradeArgs(params)
	if err != nil {
		return usage(multiTradeRoute, err)
	}
	defer form.appPass.Clear()
	res, err := s.core.MultiTrade(form.appPass, form.srvForm)
	if err != nil {
		errMsg := fmt.Sprintf("unable to multi trade: %v", err)
		resErr := msgjson.NewError(msgjson.RPCTradeError, errMsg)
		return createResponse(multiTradeRoute, nil, resErr)
	}
	trades := make([]*tradeResponse, 0, len(res))
	for _, trade := range res {
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
func handleCancel(s *RPCServer, params *RawParams) *msgjson.ResponsePayload {
	form, err := parseCancelArgs(params)
	if err != nil {
		return usage(cancelRoute, err)
	}
	if err := s.core.Cancel(form.orderID); err != nil {
		errMsg := fmt.Sprintf("unable to cancel order %q: %v", form.orderID, err)
		resErr := msgjson.NewError(msgjson.RPCCancelError, errMsg)
		return createResponse(cancelRoute, nil, resErr)
	}
	res := fmt.Sprintf(canceledOrderStr, form.orderID)
	return createResponse(cancelRoute, &res, nil)
}

// handleWithdraw handles requests for withdraw. *msgjson.ResponsePayload.Error
// is empty if successful.
func handleWithdraw(s *RPCServer, params *RawParams) *msgjson.ResponsePayload {
	return send(s, params, withdrawRoute)
}

// handleSend handles the request for send. *msgjson.ResponsePayload.Error
// is empty if successful.
func handleSend(s *RPCServer, params *RawParams) *msgjson.ResponsePayload {
	return send(s, params, sendRoute)
}

func send(s *RPCServer, params *RawParams, route string) *msgjson.ResponsePayload {
	form, err := parseSendOrWithdrawArgs(params)
	if err != nil {
		return usage(route, err)
	}
	defer form.appPass.Clear()
	subtract := false
	if route == withdrawRoute {
		subtract = true
	}
	coin, err := s.core.Send(form.appPass, form.assetID, form.value, form.address, subtract)
	if err != nil {
		errMsg := fmt.Sprintf("unable to %s: %v", err, route)
		resErr := msgjson.NewError(msgjson.RPCFundTransferError, errMsg)
		return createResponse(route, nil, resErr)
	}
	res := coin.String()
	return createResponse(route, &res, nil)
}

// handleRescanWallet handles requests to rescan a wallet. This may trigger an
// asynchronous resynchronization of wallet address activity, and the wallet
// state should be consulted for status. *msgjson.ResponsePayload.Error is empty
// if successful.
func handleRescanWallet(s *RPCServer, params *RawParams) *msgjson.ResponsePayload {
	assetID, force, err := parseRescanWalletArgs(params)
	if err != nil {
		return usage(rescanWalletRoute, err)
	}
	err = s.core.RescanWallet(assetID, force)
	if err != nil {
		errMsg := fmt.Sprintf("unable to rescan wallet: %v", err)
		resErr := msgjson.NewError(msgjson.RPCWalletRescanError, errMsg)
		return createResponse(rescanWalletRoute, nil, resErr)
	}
	return createResponse(rescanWalletRoute, "started", nil)
}

// handleLogout logs out the DEX client. *msgjson.ResponsePayload.Error is empty
// if successful.
func handleLogout(s *RPCServer, _ *RawParams) *msgjson.ResponsePayload {
	if err := s.core.Logout(); err != nil {
		errMsg := fmt.Sprintf("unable to logout: %v", err)
		resErr := msgjson.NewError(msgjson.RPCLogoutError, errMsg)
		return createResponse(logoutRoute, nil, resErr)
	}
	res := logoutStr
	return createResponse(logoutRoute, &res, nil)
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
func handleOrderBook(s *RPCServer, params *RawParams) *msgjson.ResponsePayload {
	form, err := parseOrderBookArgs(params)
	if err != nil {
		return usage(orderBookRoute, err)
	}
	book, err := s.core.Book(form.host, form.base, form.quote)
	if err != nil {
		errMsg := fmt.Sprintf("unable to retrieve order book: %v", err)
		resErr := msgjson.NewError(msgjson.RPCOrderBookError, errMsg)
		return createResponse(orderBookRoute, nil, resErr)
	}
	if form.nOrders > 0 {
		truncateOrderBook(book, form.nOrders)
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
func handleMyOrders(s *RPCServer, params *RawParams) *msgjson.ResponsePayload {
	form, err := parseMyOrdersArgs(params)
	if err != nil {
		return usage(myOrdersRoute, err)
	}
	var myOrders myOrdersResponse
	filterMkts := form.base != nil && form.quote != nil
	exchanges := s.core.Exchanges()
	for host, exchange := range exchanges {
		if form.host != "" && form.host != host {
			continue
		}
		for _, market := range exchange.Markets {
			if filterMkts && (market.BaseID != *form.base || market.QuoteID != *form.quote) {
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

// handleAppSeed handles requests for the app seed. *msgjson.ResponsePayload.Error
// is empty if successful.
func handleAppSeed(s *RPCServer, params *RawParams) *msgjson.ResponsePayload {
	appPass, err := parseAppSeedArgs(params)
	if err != nil {
		return usage(appSeedRoute, err)
	}
	defer appPass.Clear()
	seed, err := s.core.ExportSeed(appPass)
	defer encode.ClearBytes(seed)
	if err != nil {
		errMsg := fmt.Sprintf("unable to retrieve app seed: %v", err)
		resErr := msgjson.NewError(msgjson.RPCExportSeedError, errMsg)
		return createResponse(appSeedRoute, nil, resErr)
	}
	// Zero seed and hex representation after use.
	seedHex := fmt.Sprintf("%x", seed[:])

	return createResponse(appSeedRoute, seedHex, nil)
}

// handleDeleteArchivedRecords handles requests for deleting archived records.
// *msgjson.ResponsePayload.Error is empty if successful.
func handleDeleteArchivedRecords(s *RPCServer, params *RawParams) *msgjson.ResponsePayload {
	form, err := parseDeleteArchivedRecordsArgs(params)
	if err != nil {
		return usage(deleteArchivedRecordsRoute, err)
	}
	nRecordsDeleted, err := s.core.DeleteArchivedRecords(form.olderThan, form.matchesFileStr, form.ordersFileStr)
	if err != nil {
		errMsg := fmt.Sprintf("unable to delete records: %v", err)
		resErr := msgjson.NewError(msgjson.RPCDeleteArchivedRecordsError, errMsg)
		return createResponse(deleteArchivedRecordsRoute, nil, resErr)
	}

	msg := fmt.Sprintf("%d archived records has been deleted successfully", nRecordsDeleted)
	if nRecordsDeleted <= 0 {
		msg = "No archived records found"
	}
	return createResponse(deleteArchivedRecordsRoute, msg, nil)
}

func handleWalletPeers(s *RPCServer, params *RawParams) *msgjson.ResponsePayload {
	assetID, err := parseWalletPeersArgs(params)
	if err != nil {
		return usage(walletPeersRoute, err)
	}

	peers, err := s.core.WalletPeers(assetID)
	if err != nil {
		errMsg := fmt.Sprintf("unable to get wallet peers: %v", err)
		resErr := msgjson.NewError(msgjson.RPCWalletPeersError, errMsg)
		return createResponse(walletPeersRoute, nil, resErr)
	}
	return createResponse(walletPeersRoute, peers, nil)
}

func handleAddWalletPeer(s *RPCServer, params *RawParams) *msgjson.ResponsePayload {
	form, err := parseAddRemoveWalletPeerArgs(params)
	if err != nil {
		return usage(addWalletPeerRoute, err)
	}

	err = s.core.AddWalletPeer(form.assetID, form.address)
	if err != nil {
		errMsg := fmt.Sprintf("unable to add wallet peer: %v", err)
		resErr := msgjson.NewError(msgjson.RPCWalletPeersError, errMsg)
		return createResponse(addWalletPeerRoute, nil, resErr)
	}

	return createResponse(addWalletPeerRoute, "successfully added peer", nil)
}

func handleRemoveWalletPeer(s *RPCServer, params *RawParams) *msgjson.ResponsePayload {
	form, err := parseAddRemoveWalletPeerArgs(params)
	if err != nil {
		return usage(removeWalletPeerRoute, err)
	}

	err = s.core.RemoveWalletPeer(form.assetID, form.address)
	if err != nil {
		errMsg := fmt.Sprintf("unable to remove wallet peer: %v", err)
		resErr := msgjson.NewError(msgjson.RPCWalletPeersError, errMsg)
		return createResponse(removeWalletPeerRoute, nil, resErr)
	}

	return createResponse(removeWalletPeerRoute, "successfully removed peer", nil)
}

func handleNotificationsRoute(s *RPCServer, params *RawParams) *msgjson.ResponsePayload {
	numNotes, err := parseNotificationsArgs(params)
	if err != nil {
		return usage(notificationsRoute, err)
	}

	notes, err := s.core.Notifications(numNotes)
	if err != nil {
		errMsg := fmt.Sprintf("unable to remove wallet peer: %v", err)
		resErr := msgjson.NewError(msgjson.RPCNotificationsError, errMsg)
		return createResponse(notificationsRoute, nil, resErr)
	}

	return createResponse(notificationsRoute, notes, nil)
}

// parseMarketMakingConfig takes a path to a json file, parses the contents, and
// returns a []*mm.BotConfig.
func parseMarketMakingConfig(path string) ([]*mm.BotConfig, error) {
	contents, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var configs []*mm.BotConfig
	err = json.Unmarshal(contents, &configs)
	if err != nil {
		return nil, err
	}

	return configs, nil
}

func handleStartMarketMakingRoute(s *RPCServer, params *RawParams) *msgjson.ResponsePayload {
	form, err := parseStartMarketMakingArgs(params)
	if err != nil {
		return usage(startMarketMakingRoute, err)
	}

	configs, err := parseMarketMakingConfig(form.cfgFilePath)
	if err != nil {
		errMsg := fmt.Sprintf("unable to parse market making config: %v", err)
		resErr := msgjson.NewError(msgjson.RPCStartMarketMakingError, errMsg)
		return createResponse(startMarketMakingRoute, nil, resErr)
	}

	err = s.mm.Run(s.ctx, configs, form.appPass)
	if err != nil {
		errMsg := fmt.Sprintf("unable to start market making: %v", err)
		resErr := msgjson.NewError(msgjson.RPCStartMarketMakingError, errMsg)
		return createResponse(startMarketMakingRoute, nil, resErr)
	}

	return createResponse(startMarketMakingRoute, "started market making", nil)
}

func handleStopMarketMakingRoute(s *RPCServer, params *RawParams) *msgjson.ResponsePayload {
	if !s.mm.Running() {
		errMsg := "market making is not running"
		resErr := msgjson.NewError(msgjson.RPCStopMarketMakingError, errMsg)
		return createResponse(stopMarketMakingRoute, nil, resErr)
	}
	s.mm.Stop()
	return createResponse(stopMarketMakingRoute, "stopped market making", nil)
}

// format concatenates thing and tail. If thing is empty, returns an empty
// string.
func format(thing, tail string) string {
	if thing == "" {
		return ""
	}
	return fmt.Sprintf("%s%s", thing, tail)
}

// ListCommands prints a short usage string for every route available to the
// rpcserver.
func ListCommands(includePasswords bool) string {
	var sb strings.Builder
	var err error
	for _, r := range sortHelpKeys() {
		msg := helpMsgs[r]
		// If help should include password arguments and this command
		// has password arguments, add them to the help message.
		if includePasswords && msg.pwArgsShort != "" {
			_, err = sb.WriteString(fmt.Sprintf("%s %s%s\n", r,
				format(msg.pwArgsShort, " "), msg.argsShort))
		} else {
			_, err = sb.WriteString(fmt.Sprintf("%s %s\n", r, msg.argsShort))
		}
		if err != nil {
			log.Errorf("unable to parse help message for %s", r)
			return ""
		}
	}
	s := sb.String()
	// Remove trailing newline.
	return s[:len(s)-1]
}

// commandUsage returns a help message for cmd or an error if cmd is unknown.
func commandUsage(cmd string, includePasswords bool) (string, error) {
	msg, exists := helpMsgs[cmd]
	if !exists {
		return "", fmt.Errorf("%w: %s", errUnknownCmd, cmd)
	}
	// If help should include password arguments and this command has
	// password arguments, return them as part of the help message.
	if includePasswords && msg.pwArgsShort != "" {
		return fmt.Sprintf("%s %s%s\n\n%s\n\n%s%s%s",
			cmd, format(msg.pwArgsShort, " "), msg.argsShort,
			msg.cmdSummary, format(msg.pwArgsLong, "\n\n"), format(msg.argsLong, "\n\n"),
			msg.returns), nil
	}
	return fmt.Sprintf("%s %s\n\n%s\n\n%s%s", cmd, msg.argsShort,
		msg.cmdSummary, format(msg.argsLong, "\n\n"), msg.returns), nil
}

// sortHelpKeys returns a sorted list of helpMsgs keys.
func sortHelpKeys() []string {
	keys := make([]string, 0, len(helpMsgs))
	for k := range helpMsgs {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		return keys[i] < keys[j]
	})
	return keys
}

type helpMsg struct {
	pwArgsShort, argsShort, cmdSummary, pwArgsLong, argsLong, returns string
}

// helpMsgs are a map of routes to help messages. They are broken down into six
// sections.
// In descending order:
//  1. Password argument example inputs. These are arguments the caller may not
//     want to echo listed in order of input.
//  2. Argument example inputs. These are non-sensitive arguments listed in
//     order of input.
//  3. A description of the command.
//  4. An extensive breakdown of the password arguments.
//  5. An extensive breakdown of the arguments.
//  6. An extensive breakdown of the returned values.
var helpMsgs = map[string]helpMsg{
	helpRoute: {
		pwArgsShort: ``,                           // password args example input
		argsShort:   `("cmd") (includePasswords)`, // args example input
		cmdSummary:  `Print a help message.`,      // command explanation
		pwArgsLong:  ``,                           // password args breakdown
		argsLong: `Args:
    cmd (string): Optional. The command to print help for.
    includePasswords (bool): Optional. Default is false. Whether to include
      password arguments in the returned help.`, // args breakdown
		returns: `Returns:
    string: The help message for command.`, // returns breakdown
	},
	versionRoute: {
		cmdSummary: `Print the DEX client rpcserver version.`,
		returns: `Returns:
    string: The DEX client rpcserver version.`,
	},
	discoverAcctRoute: {
		pwArgsShort: `"appPass"`,
		argsShort:   `"addr" ("cert")`,
		cmdSummary: `Discover an account that is used for a dex. Useful when restoring
    an account and can be used in place of register. Will error if
    the account has already been discovered/restored.`,
		pwArgsLong: `Password Args:
    appPass (string): The DEX client password.`,
		argsLong: `Args:
    addr (string): The DEX address to discover an account for.
    cert (string): Optional. The TLS certificate path.`,
		returns: `Returns:
    bool: True if the account has has been registered and paid for.`,
	},
	initRoute: {
		pwArgsShort: `"appPass"`,
		argsShort:   `("seed")`,
		cmdSummary:  `Initialize the client.`,
		pwArgsLong: `Password Args:
    appPass (string): The DEX client password.`,
		argsLong: `Args:
    seed (string): Optional. hex-encoded 512-bit restoration seed.`,
		returns: `Returns:
    string: The message "` + initializedStr + `"`,
	},
	deleteArchivedRecordsRoute: {
		argsShort: `("unix time milli") ("matches csv path") ("orders csv path")`,
		cmdSummary: `Delete archived records from the database and returns total deleted. Optionally
	set a time to delete records before and file paths to save deleted records as comma separated
    values. Note that file locations are from the perspective of dexc and not the caller.`,
		argsLong: `Args:
    unix time milli (int): Optional. If set deletes records before the date in unix time
      in milliseconds (not seconds). Unset or 0 will default to the current time.
    matches csv path (string): Optional. A path to save a csv with deleted matches.
      Will not save by default.
    orders csv path (string): Optional. A path to save a csv with deleted orders.
      Will not save by default.`,
		returns: `Returns:
    Nothing.`,
	},
	bondAssetsRoute: {
		argsShort:  `"dex" ("cert")`,
		cmdSummary: `Get dex bond asset config.`,
		argsLong: `Args:
    dex (string): The dex address to get bond info for.
    cert (string): Optional. The TLS certificate path.`,
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
		argsShort:  `"dex" ("cert")`,
		cmdSummary: `Get a DEX configuration.`,
		argsLong: `Args:
    dex (string): The dex address to get config for.
    cert (string): Optional. The TLS certificate path.`,
		returns: `Returns:
    obj: The getdexconfig result. See the 'exchanges' result.`,
	},
	newWalletRoute: {
		pwArgsShort: `"appPass" "walletPass"`,
		argsShort:   `assetID walletType ("path" "settings")`,
		cmdSummary:  `Connect to a new wallet.`,
		pwArgsLong: `Password Args:
    appPass (string): The DEX client password.
    walletPass (string): The wallet's password. Leave the password empty for wallets without a password set.`,
		argsLong: `Args:
    assetID (int): The asset's BIP-44 registered coin index. e.g. 42 for DCR.
      See https://github.com/satoshilabs/slips/blob/master/slip-0044.md
    walletType (string): The wallet type.
    path (string): Optional. The path to a configuration file.
    settings (string): A JSON-encoded string->string mapping of additional
       configuration settings. These settings take precedence over any settings
       parsed from file. e.g. '{"account":"default"}' for Decred accounts, and
       '{"walletname":""}' for the default Bitcoin wallet where bitcoind's listwallets RPC gives possible walletnames.`,
		returns: `Returns:
    string: The message "` + fmt.Sprintf(walletCreatedStr, "[coin symbol]") + `"`,
	},
	openWalletRoute: {
		pwArgsShort: `"appPass"`,
		argsShort:   `assetID`,
		cmdSummary:  `Open an existing wallet.`,
		pwArgsLong: `Password Args:
    appPass (string): The DEX client password.`,
		argsLong: `Args:
    assetID (int): The asset's BIP-44 registered coin index. e.g. 42 for DCR.
      See https://github.com/satoshilabs/slips/blob/master/slip-0044.md`,
		returns: `Returns:
    string: The message "` + fmt.Sprintf(walletUnlockedStr, "[coin symbol]") + `"`,
	},
	closeWalletRoute: {
		argsShort:  `assetID`,
		cmdSummary: `Close an open wallet.`,
		argsLong: `Args:
    assetID (int): The asset's BIP-44 registered coin index. e.g. 42 for DCR.
      See https://github.com/satoshilabs/slips/blob/master/slip-0044.md`,
		returns: `Returns:
    string: The message "` + fmt.Sprintf(walletLockedStr, "[coin symbol]") + `"`,
	},
	toggleWalletStatusRoute: {
		pwArgsShort: "appPass",
		argsShort:   `assetID disable`,
		cmdSummary: `Disable or enable an existing wallet. When disabling a chain's primary asset wallet,
	all token wallets for that chain will be disabled too.`,
		pwArgsLong: `Password Args:
    appPass (string): The DEX client password.`,
		argsLong: `Args:
   assetID (int): The asset's BIP-44 registered coin index. e.g. 42 for DCR.
                  See https://github.com/satoshilabs/slips/blob/master/slip-0044.md
  disable (bool): The wallet's status. e.g To disable a wallet set to "true", to enable set to "false".`,
		returns: `Returns:
    string: The message "` + fmt.Sprintf(walletStatusStr, "[coin symbol]", "[wallet status]") + `".`,
	},
	walletsRoute: {
		cmdSummary: `List all wallets.`,
		returns: `Returns:
    array: An array of wallet results.
    [
      {
        "symbol" (string): The coin symbol.
        "assetID" (int): The asset's BIP-44 registered coin index. e.g. 42 for DCR.
          See https://github.com/satoshilabs/slips/blob/master/slip-0044.md
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
	registerRoute: {
		pwArgsShort: `"appPass"`,
		argsShort:   `"addr" fee assetID ("cert")`,
		cmdSummary: `Register for DEX. An ok response does not mean that registration is complete.
Registration is complete after the fee transaction has been confirmed.`,
		pwArgsLong: `Password Args:
    appPass (string): The DEX client password.`,
		argsLong: `Args:
    addr (string): The DEX address to register for.
    fee (int): The DEX fee.
    assetID (int): The asset ID with which to pay the fee.
    cert (string): Optional. The TLS certificate path.`,
		returns: `Returns:
    {
      "feeID" (string): The fee transactions's txid and output index.
      "reqConfirms" (int): The number of confirmations required to start trading.
    }`,
	},
	postBondRoute: {
		pwArgsShort: `"appPass"`,
		argsShort:   `"addr" bond assetID (maintain "cert")`,
		cmdSummary: `Post new bond for DEX. An ok response does not mean that the bond is active.
		Bond is active after the bond transaction has been confirmed and the server notified.`,
		pwArgsLong: `Password Args:
    appPass (string): The DEX client password.`,
		argsLong: `Args:
    addr (string): The DEX address to post bond for for.
    bond (int): The bond amount (in DCR presently).
    assetID (int): The asset ID with which to pay the fee.
    maintain (bool): Optional. Whether to maintain the trading tier established by this bond. Only applicable when registering. (default is true)
    cert (string): Optional. The TLS certificate path. Only applicable when registering.`,
		returns: `Returns:
    {
      "bondID" (string): The bond transactions's txid and output index.
      "reqConfirms" (int): The number of confirmations required to start trading.
    }`,
	},
	bondOptionsRoute: {
		argsShort:  `"addr" targetTier (maxBondedAmt bondAssetID)`,
		cmdSummary: `Change bond options for a DEX.`,
		argsLong: `Args:
    addr (string): The DEX address to post bond for for.
    targetTier (int): The target trading tier.
    maxBondedAmt (int): The maximum amount that may be locked in bonds.
    bondAssetID (int): The asset ID with which to auto-post bonds.`,
		returns: `Returns: "ok"`,
	},
	exchangesRoute: {
		cmdSummary: `Detailed information about known exchanges and markets.`,
		returns: `Returns:
    obj: The exchanges result.
    {
      "[DEX host]": {
        "acctID" (string):  The client's account ID associated with this DEX.,
        "markets": {
          "[assetID-assetID]": {
            "baseid" (int): The base asset ID
            "basesymbol" (string): The base ticker symbol.
            "quoteid" (int): The quote asset ID.
            "quotesymbol" (string): The quote asset ID symbol,
            "epochlen" (int): Duration of a epoch in milliseconds.
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
		pwArgsShort: `"appPass"`,
		cmdSummary:  `Attempt to login to all registered DEX servers.`,
		pwArgsLong: `Password Args:
    appPass (string): The dex client password.`,
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
		pwArgsShort: `"appPass"`,
		argsShort:   `"host" isLimit sell base quote qty rate immediate`,
		cmdSummary:  `Make an order to buy or sell an asset.`,
		pwArgsLong: `Password Args:
    appPass (string): The DEX client password.`,
		argsLong: `Args:
    host (string): The DEX to trade on.
    isLimit (bool): Whether the order is a limit order.
    sell (bool): Whether the order is selling.
    base (int): The BIP-44 coin index for the market's base asset.
    quote (int): The BIP-44 coin index for the market's quote asset.
    qty (int): The number of units to buy/sell. Must be a multiple of the lot size.
    rate (int): The atoms quote asset to pay/accept per unit base asset. e.g.
      156000 satoshi/DCR for the DCR(base)_BTC(quote).
    immediate (bool): Require immediate match. Do not book the order.
    options (string): A JSON-encoded string->string mapping of additional
       trade options.`,
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
		pwArgsShort: `"appPass"`,
		argsShort:   `"host" sell base quote maxLock [[qty,rate]] options`,
		cmdSummary:  `Place multiple orders in one go.`,
		pwArgsLong: `Password Args:
    appPass (string): The DEX client password.`,
		argsLong: `Args:
    host (string): The DEX to trade on.
    sell (bool): Whether the order is selling.
    base (int): The BIP-44 coin index for the market's base asset.
    quote (int): The BIP-44 coin index for the market's quote asset.
    maxLock (int): The maximum amount the wallet can lock for this order. 0 means no limit.
    placements ([[int,int]]):  An array of [qty,rate] placements. Quantity must be
	 a multiple of the lot size. Rate must be in atomic units of the quote asset.
    options (string): A JSON-encoded string->string mapping of additional
       trade options.`,
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
		pwArgsShort: `"appPass"`,
		argsShort:   `"orderID"`,
		cmdSummary:  `Cancel an order.`,
		pwArgsLong: `Password Args:
    appPass (string): The DEX client password.`,
		argsLong: `Args:
    orderID (string): The hex ID of the order to cancel`,
		returns: `Returns:
    string: The message "` + fmt.Sprintf(canceledOrderStr, "[order ID]") + `"`,
	},
	rescanWalletRoute: {
		argsShort: `assetID (force)`,
		cmdSummary: `Initiate a rescan of an asset's wallet. This is only supported for certain
wallet types. Wallet resynchronization may be asynchronous, and the wallet
state should be consulted for progress.
	
WARNING: It is ill-advised to initiate a wallet rescan with active orders
unless as a last ditch effort to get the wallet to recognize a transaction
needed to complete a swap.`,
		returns: `Returns:
    string: "started"`,
		argsLong: `Args:
    assetID (int): The asset's BIP-44 registered coin index. Used to identify
      which wallet to withdraw from. e.g. 42 for DCR. See
      https://github.com/satoshilabs/slips/blob/master/slip-0044.md
    force (bool): Force a wallet rescan even if their are active orders. The
      default is false.`,
	},
	withdrawRoute: {
		pwArgsShort: `"appPass"`,
		argsShort:   `assetID value "address"`,
		cmdSummary:  `Withdraw value from an exchange wallet to address. Fees are subtracted from the value.`,
		pwArgsLong: `Password Args:
    appPass (string): The DEX client password.`,
		argsLong: `Args:
    assetID (int): The asset's BIP-44 registered coin index. Used to identify
      which wallet to withdraw from. e.g. 42 for DCR. See
      https://github.com/satoshilabs/slips/blob/master/slip-0044.md
    value (int): The amount to withdraw in units of the asset's smallest
      denomination (e.g. satoshis, atoms, etc.)"
    address (string): The address to which withdrawn funds are sent.`,
		returns: `Returns:
    string: "[coin ID]"`,
	},
	sendRoute: {
		pwArgsShort: `"appPass"`,
		argsShort:   `assetID value "address"`,
		cmdSummary:  `Sends exact value from an exchange wallet to address.`,
		pwArgsLong: `Password Args:
    appPass (string): The DEX client password.`,
		argsLong: `Args:
    assetID (int): The asset's BIP-44 registered coin index. Used to identify
      which wallet to withdraw from. e.g. 42 for DCR. See
      https://github.com/satoshilabs/slips/blob/master/slip-0044.md
    value (int): The amount to send in units of the asset's smallest
      denomination (e.g. satoshis, atoms, etc.)"
    address (string): The address to which funds are sent.`,
		returns: `Returns:
    string: "[coin ID]"`,
	},
	logoutRoute: {
		cmdSummary: `Logout the DEX client.`,
		returns: `Returns:
    string: The message "` + logoutStr + `"`,
	},
	orderBookRoute: {
		argsShort:  `"host" base quote (nOrders)`,
		cmdSummary: `Retrieve all orders for a market.`,
		argsLong: `Args:
    host (string): The DEX to retrieve the order book from.
    base (int): The BIP-44 coin index for the market's base asset.
    quote (int): The BIP-44 coin index for the market's quote asset.
    nOrders (int): Optional. Default is 0, which returns all orders. The number
      of orders from the top of buys and sells to return. Epoch orders are not
      truncated.`,
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
		argsShort: `("host") (base) (quote)`,
		cmdSummary: `Fetch all active and recently executed orders
    belonging to the user.`,
		argsLong: `Args:
    host (string): Optional. The DEX to show orders from.
    base (int): Optional. The BIP-44 coin index for the market's base asset.
    quote (int): Optional. The BIP-44 coin index for the market's quote asset.`,
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
		pwArgsShort: `"appPass"`,
		cmdSummary: `Show the application's seed. It is recommended to not store the seed
  digitally. Make a copy on paper with pencil and keep it safe.`,
		pwArgsLong: `Password Args:
    appPass (string): The DEX client password.`,
		returns: `Returns:
    string: The application's seed as hex.`,
	},
	walletPeersRoute: {
		cmdSummary: `Show the peers a wallet is connected to.`,
		argsShort:  `(assetID)`,
		argsLong: `Args:
		assetID (int): The asset's BIP-44 registered coin index. Used to identify
		which wallet's peers to return.`,
		returns: `Returns:
		[]string: Addresses of wallet peers.`,
	},
	addWalletPeerRoute: {
		cmdSummary: `Add a new wallet peer connection.`,
		argsShort:  `(assetID) (addr)`,
		argsLong: `Args:
		assetID (int): The asset's BIP-44 registered coin index. Used to identify
		which wallet to add a peer.
		addr (string): The peer's address (host:port).`,
	},
	removeWalletPeerRoute: {
		cmdSummary: `Remove an added wallet peer.`,
		argsShort:  `(assetID) (addr)`,
		argsLong: `Args:
		assetID (int): The asset's BIP-44 registered coin index. Used to identify
		which wallet to add a peer.
		addr (string): The peer's address (host:port).`,
	},
	notificationsRoute: {
		cmdSummary: `See recent notifications.`,
		argsShort:  `(num)`,
		argsLong: `Args:
		num (int): The number of notifications to load.`,
	},
	startMarketMakingRoute: {
		cmdSummary: `Start market making.`,
		argsShort:  `(cfgPath)`,
		argsLong: `Args:
		cfgPath (string): The path to the market maker config file.`,
	},
	stopMarketMakingRoute: {
		cmdSummary: `Stop market making.`,
	},
}
