// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package rpcserver

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/msgjson"
)

// routes
const (
	cancelRoute      = "cancel"
	closeWalletRoute = "closewallet"
	exchangesRoute   = "exchanges"
	helpRoute        = "help"
	initRoute        = "init"
	loginRoute       = "login"
	logoutRoute      = "logout"
	newWalletRoute   = "newwallet"
	openWalletRoute  = "openwallet"
	orderBookRoute   = "orderbook"
	getFeeRoute      = "getfee"
	registerRoute    = "register"
	tradeRoute       = "trade"
	versionRoute     = "version"
	walletsRoute     = "wallets"
	withdrawRoute    = "withdraw"
)

const (
	initializedStr    = "app initialized"
	walletCreatedStr  = "%s wallet created and unlocked"
	walletLockedStr   = "%s wallet locked"
	walletUnlockedStr = "%s wallet unlocked"
	canceledOrderStr  = "canceled order %s"
	logoutStr         = "goodbye"
)

// createResponse creates a msgjson response payload.
func createResponse(op string, res interface{}, resErr *msgjson.Error) *msgjson.ResponsePayload {
	encodedRes, err := json.Marshal(res)
	if err != nil {
		err := fmt.Errorf("unable to marshal data for %s: %v", op, err)
		panic(err)
	}
	return &msgjson.ResponsePayload{Result: encodedRes, Error: resErr}
}

// usage creates and returns usage for route combined with a passed error as a
// *msgjson.ResponsePayload.
func usage(route string, err error) *msgjson.ResponsePayload {
	usage, _ := commandUsage(route, false)
	err = fmt.Errorf("%v\n\n%s", err, usage)
	resErr := msgjson.NewError(msgjson.RPCArgumentsError, err.Error())
	return createResponse(route, nil, resErr)
}

// routes maps routes to a handler function.
var routes = map[string]func(s *RPCServer, params *RawParams) *msgjson.ResponsePayload{
	cancelRoute:      handleCancel,
	closeWalletRoute: handleCloseWallet,
	exchangesRoute:   handleExchanges,
	helpRoute:        handleHelp,
	initRoute:        handleInit,
	loginRoute:       handleLogin,
	logoutRoute:      handleLogout,
	newWalletRoute:   handleNewWallet,
	openWalletRoute:  handleOpenWallet,
	orderBookRoute:   handleOrderBook,
	getFeeRoute:      handleGetFee,
	registerRoute:    handleRegister,
	tradeRoute:       handleTrade,
	versionRoute:     handleVersion,
	walletsRoute:     handleWallets,
	withdrawRoute:    handleWithdraw,
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
	appPass, err := parseInitArgs(params)
	if err != nil {
		return usage(initRoute, err)
	}
	defer appPass.Clear()
	if err := s.core.InitializeClient(appPass); err != nil {
		errMsg := fmt.Sprintf("unable to initialize client: %v", err)
		resErr := msgjson.NewError(msgjson.RPCInitError, errMsg)
		return createResponse(initRoute, nil, resErr)
	}
	res := initializedStr
	return createResponse(initRoute, &res, nil)
}

// handleVersion handles requests for version. It takes no arguments and returns
// the semver.
func handleVersion(_ *RPCServer, _ *RawParams) *msgjson.ResponsePayload {
	res := &versionResponse{
		Major: rpcSemverMajor,
		Minor: rpcSemverMinor,
		Patch: rpcSemverPatch,
	}
	return createResponse(versionRoute, res.String(), nil)
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
	// Wallet does not exist yet. Try to create it.
	err = s.core.CreateWallet(form.appPass, form.walletPass, &core.WalletForm{
		AssetID: form.assetID,
		Config:  form.config,
	})
	if err != nil {
		errMsg := fmt.Sprintf("error creating %s wallet: %v",
			dex.BipIDSymbol(form.assetID), err)
		resErr := msgjson.NewError(msgjson.RPCCreateWalletError, errMsg)
		return createResponse(newWalletRoute, nil, resErr)
	}
	s.wsServer.NotifyWalletUpdate(form.assetID)
	err = s.core.OpenWallet(form.assetID, form.appPass)
	if err != nil {
		errMsg := fmt.Sprintf("wallet connected, but failed to open with provided password: %v",
			err)
		resErr := msgjson.NewError(msgjson.RPCOpenWalletError, errMsg)
		return createResponse(newWalletRoute, nil, resErr)
	}
	s.wsServer.NotifyWalletUpdate(form.assetID)
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

	err = s.core.OpenWallet(form.assetID, form.appPass)
	form.appPass.Clear() // AppPass not needed after this, clear
	if err != nil {
		errMsg := fmt.Sprintf("error unlocking %s wallet: %v",
			dex.BipIDSymbol(form.assetID), err)
		resErr := msgjson.NewError(msgjson.RPCOpenWalletError, errMsg)
		return createResponse(openWalletRoute, nil, resErr)
	}
	s.wsServer.NotifyWalletUpdate(form.assetID)
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
	s.wsServer.NotifyWalletUpdate(assetID)
	res := fmt.Sprintf(walletLockedStr, dex.BipIDSymbol(assetID))
	return createResponse(closeWalletRoute, &res, nil)
}

// handleWallets handles requests for wallets. Returns a list of wallet details.
func handleWallets(s *RPCServer, _ *RawParams) *msgjson.ResponsePayload {
	walletsStates := s.core.Wallets()
	return createResponse(walletsRoute, walletsStates, nil)
}

// handleGetFee handles requests for getfee.
// *msgjson.ResponsePayload.Error is empty if successful. Requires the address
// of a dex and returns the dex fee.
func handleGetFee(s *RPCServer, params *RawParams) *msgjson.ResponsePayload {
	host, cert, err := parseGetFeeArgs(params)
	if err != nil {
		return usage(getFeeRoute, err)
	}
	fee, err := s.core.GetFee(host, cert)
	if err != nil {
		resErr := msgjson.NewError(msgjson.RPCGetFeeError, err.Error())
		return createResponse(getFeeRoute, nil, resErr)
	}
	res := &getFeeResponse{
		Fee: fee,
	}
	return createResponse(getFeeRoute, res, nil)
}

// handleRegister handles requests for register. *msgjson.ResponsePayload.Error
// is empty if successful.
func handleRegister(s *RPCServer, params *RawParams) *msgjson.ResponsePayload {
	form, err := parseRegisterArgs(params)
	if err != nil {
		return usage(registerRoute, err)
	}
	defer form.AppPass.Clear()
	fee, err := s.core.GetFee(form.Addr, form.Cert)
	if err != nil {
		resErr := msgjson.NewError(msgjson.RPCGetFeeError,
			err.Error())
		return createResponse(registerRoute, nil, resErr)
	}
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

// handleExchanges handles requests for exchangess. It takes no arguments and
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
	// Convert something to a []interface{}.
	convA := func(in interface{}) []interface{} {
		var a []interface{}
		b, err := json.Marshal(in)
		if err != nil {
			panic(err)
		}
		if err = json.Unmarshal(b, &a); err != nil {
			panic(err)
		}
		return a
	}
	res := s.core.Exchanges()
	exchanges := convM(res)
	// Interate through exchanges converting structs into maps in order to
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
			orders := convA(marketDetails["orders"])
			for i, order := range orders {
				orderDetails := convM(order)
				// Remove redundant address field.
				delete(orderDetails, "dex")
				// Remove redundant market name field.
				delete(orderDetails, "market")
				orders[i] = orderDetails
			}
			marketDetails["orders"] = orders
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

// handleLogin sets up the dex connection and returns core.LoginResult.
func handleLogin(s *RPCServer, params *RawParams) *msgjson.ResponsePayload {
	appPass, err := parseLoginArgs(params)
	if err != nil {
		return usage(loginRoute, err)
	}
	defer appPass.Clear()
	res, err := s.core.Login(appPass)
	if err != nil {
		errMsg := fmt.Sprintf("unable to login: %v", err)
		resErr := msgjson.NewError(msgjson.RPCLoginError, errMsg)
		return createResponse(loginRoute, nil, resErr)
	}
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
		OrderID: res.ID,
		Sig:     res.Sig.String(),
		Stamp:   res.Stamp,
	}
	return createResponse(tradeRoute, &tradeRes, nil)
}

// handleCancel handles requests for cancel. *msgjson.ResponsePayload.Error is
// empty if successful.
func handleCancel(s *RPCServer, params *RawParams) *msgjson.ResponsePayload {
	form, err := parseCancelArgs(params)
	if err != nil {
		return usage(cancelRoute, err)
	}
	defer form.appPass.Clear()
	if err := s.core.Cancel(form.appPass, form.orderID); err != nil {
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
	form, err := parseWithdrawArgs(params)
	if err != nil {
		return usage(withdrawRoute, err)
	}
	defer form.appPass.Clear()
	coin, err := s.core.Withdraw(form.appPass, form.assetID, form.value, form.address)
	if err != nil {
		errMsg := fmt.Sprintf("unable to withdraw: %v", err)
		resErr := msgjson.NewError(msgjson.RPCWithdrawError, errMsg)
		return createResponse(withdrawRoute, nil, resErr)
	}
	res := coin.String()
	return createResponse(withdrawRoute, &res, nil)
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
// 1. Password argument example inputs. These are arguments the caller may not
//    want to echo listed in order of input.
// 2. Argument example inputs. These are non-sensitive arguments listed in order
//    of input.
// 3. A description of the command.
// 4. An extensive breakdown of the password arguments.
// 5. An extensive breakdown of the arguements.
// 6. An extensive breakdown of the returned values.
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
	initRoute: {
		pwArgsShort: `"appPass"`,
		cmdSummary:  `Initialize the client.`,
		pwArgsLong: `Password Args:
    appPass (string): The DEX client password.`,
		returns: `Returns:
    string: The message "` + initializedStr + `"`,
	},
	getFeeRoute: {
		argsShort:  `"dex" ("cert")`,
		cmdSummary: `Get dex registration fee.`,
		argsLong: `Args:
    dex (string): The dex address to get fee for.
    cert (string): Optional. The TLS certificate path.`,
		returns: `Returns:
    obj: The getFee result.
    {
      "fee" (int): The DEX registration fee.
    }`,
	},
	newWalletRoute: {
		pwArgsShort: `"appPass" "walletPass"`,
		argsShort:   `assetID ("path" "settings")`,
		cmdSummary:  `Connect to a new wallet.`,
		pwArgsLong: `Password Args:
    appPass (string): The DEX client password.
    walletPass (string): The wallet's password.`,
		argsLong: `Args:
    assetID (int): The asset's BIP-44 registered coin index. e.g. 42 for DCR.
      See https://github.com/satoshilabs/slips/blob/master/slip-0044.md
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
		argsShort:   `"addr" fee ("cert")`,
		cmdSummary: `Register for DEX. An ok response does not mean that registration is complete.
Registration is complete after the fee transaction has been confirmed.`,
		pwArgsLong: `Password Args:
    appPass (string): The DEX client password.`,
		argsLong: `Args:
    addr (string): The DEX address to register for.
    fee (int): The DEX fee.
    cert (string): Optional. The TLS certificate path.`,
		returns: `Returns:
    {
      "feeID" (string): The fee transactions's txid and output index.
      "reqConfirms" (int): The number of confirmations required to start trading.
    }`,
	},
	exchangesRoute: {
		cmdSummary: `Detailed information about known exchanges, markets, and active trades.`,
		returns: `Returns:
    obj: The exchanges result.
    {
      "[DEX host]": {
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
            "orders" (map): {
              "type" (int): 0 for market or 1 for limit.
              "id" (string): A unique trade ID.
              "stamp" (int): The unix trade timestamp. Seconds since 00:00:00
	        Jan 1 1970.
              "qty" (bool): Number of units offered in the trade.
              "sell" (bool): Whether this is a sell order.
              "filled" (int): How much of the order has been filled.
              "matches": [
	        {
                  "matchID" (string): A unique match ID.
                  "status" (string): The match status.
                  "rate" (int): Atoms quote asset per unit base asset.
                  "qty" (int): Number of units offered in the trade.
		},...
	      ]
              "cancelling" (bool): Whether this trade is in the process of being
	        cancelled.
              "canceled" (bool): Whether this trade has been canceled.
              "rate" (int): Atoms quote asset per unit base asset.
              "tif" (int): The number of epochs this trade is good for.
              "targetID" (string): The order ID of the order being canceled.
	      },...
          },...
        },
        "assets": {
          "[assetID]": {
            "symbol (string)": The asset's coin symbol.
            "lotSize" (int): The amount of units of a coin in one lot.
            "rateStep" (int): the price rate increment in atoms.
            "feeRate" (int): The transaction fee in atoms per byte.
			"swapSize" (int): The size of a swap transaction in bytes.
			"swapSizeBase" (int): The size of a swap transaction minus inputs in bytes.
            "swapConf" (int): The number of confirmations needed to confirm
	      trade transactions.
          },...
		},
        "confsrequired": (int) The number of confirmations needed for the
          registration fee payment
        "confs" (int): The current number of confirmations for the registration
          fee payment. This is only present during the registration process.
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
        }
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
    immediate (bool): Require immediate match. Do not book the order.`,
		returns: `Returns:
    obj: The order details.
    {
      "orderid" (string): The order's unique hex identifier.
      "sig" (string): The DEX's signature of the order information.
      "stamp" (int): The time the order was signed in milliseconds since 00:00:00
        Jan 1 1970.
    }`,
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
	withdrawRoute: {
		pwArgsShort: `"appPass"`,
		argsShort:   `assetID value "address"`,
		cmdSummary:  `Withdraw value from an exchange wallet to address.`,
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
	logoutRoute: {
		cmdSummary: `Logout the DEX client.`,
		returns: `Returns:
    string: The message "` + logoutStr + `"`,
	},
	orderBookRoute: {
		argsShort:  `"host" base quote nOrders`,
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
        },...
      ],
    }`,
	},
}
