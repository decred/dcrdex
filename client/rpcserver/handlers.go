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
	closeWalletRoute = "closewallet"
	helpRoute        = "help"
	initRoute        = "init"
	newWalletRoute   = "newwallet"
	openWalletRoute  = "openwallet"
	preRegisterRoute = "preregister"
	registerRoute    = "register"
	versionRoute     = "version"
	walletsRoute     = "wallets"
)

const (
	initializedStr    = "app initialized"
	feePaidStr        = "the DEX fee of %v has been paid"
	walletCreatedStr  = "%s wallet created and unlocked"
	walletLockedStr   = "%s wallet locked"
	walletUnlockedStr = "%s wallet unlocked"
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
	closeWalletRoute: handleCloseWallet,
	helpRoute:        handleHelp,
	initRoute:        handleInit,
	newWalletRoute:   handleNewWallet,
	openWalletRoute:  handleOpenWallet,
	preRegisterRoute: handlePreRegister,
	registerRoute:    handleRegister,
	versionRoute:     handleVersion,
	walletsRoute:     handleWallets,
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
	if form.HelpWith == "" {
		// List all commands if no arguments.
		res = ListCommands(form.IncludePasswords)
	} else {
		var err error
		res, err = commandUsage(form.HelpWith, form.IncludePasswords)
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
		form.AppPass.Clear()
		form.WalletPass.Clear()
	}()

	exists := s.core.WalletState(form.AssetID) != nil
	if exists {
		errMsg := fmt.Sprintf("error creating %s wallet: wallet already exists",
			dex.BipIDSymbol(form.AssetID))
		resErr := msgjson.NewError(msgjson.RPCWalletExistsError, errMsg)
		return createResponse(newWalletRoute, nil, resErr)
	}
	// Wallet does not exist yet. Try to create it.
	err = s.core.CreateWallet(form.AppPass, form.WalletPass, &core.WalletForm{
		AssetID:    form.AssetID,
		Account:    form.Account,
		ConfigText: form.ConfigText,
	})
	if err != nil {
		errMsg := fmt.Sprintf("error creating %s wallet: %v",
			dex.BipIDSymbol(form.AssetID), err)
		resErr := msgjson.NewError(msgjson.RPCCreateWalletError, errMsg)
		return createResponse(newWalletRoute, nil, resErr)
	}
	s.notifyWalletUpdate(form.AssetID)
	err = s.core.OpenWallet(form.AssetID, form.AppPass)
	if err != nil {
		errMsg := fmt.Sprintf("wallet connected, but failed to open with provided password: %v",
			err)
		resErr := msgjson.NewError(msgjson.RPCOpenWalletError, errMsg)
		return createResponse(newWalletRoute, nil, resErr)
	}
	s.notifyWalletUpdate(form.AssetID)
	res := fmt.Sprintf(walletCreatedStr, dex.BipIDSymbol(form.AssetID))
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

	err = s.core.OpenWallet(form.AssetID, form.AppPass)
	form.AppPass.Clear() // AppPass not needed after this, clear
	if err != nil {
		errMsg := fmt.Sprintf("error unlocking %s wallet: %v",
			dex.BipIDSymbol(form.AssetID), err)
		resErr := msgjson.NewError(msgjson.RPCOpenWalletError, errMsg)
		return createResponse(openWalletRoute, nil, resErr)
	}
	s.notifyWalletUpdate(form.AssetID)
	res := fmt.Sprintf(walletUnlockedStr, dex.BipIDSymbol(form.AssetID))
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
	s.notifyWalletUpdate(assetID)
	res := fmt.Sprintf(walletLockedStr, dex.BipIDSymbol(assetID))
	return createResponse(closeWalletRoute, &res, nil)
}

// handleWallets handles requests for wallets. Returns a list of wallet details.
func handleWallets(s *RPCServer, _ *RawParams) *msgjson.ResponsePayload {
	walletsStates := s.core.Wallets()
	return createResponse(walletsRoute, walletsStates, nil)
}

// handlePreRegister handles requests for preregister.
// *msgjson.ResponsePayload.Error is empty if successful. Requires the address
// of a dex and returns the dex fee.
func handlePreRegister(s *RPCServer, params *RawParams) *msgjson.ResponsePayload {
	url, cert, err := parsePreRegisterArgs(params)
	if err != nil {
		return usage(preRegisterRoute, err)
	}
	fee, err := s.core.GetFee(url, cert)
	if err != nil {
		resErr := msgjson.NewError(msgjson.RPCPreRegisterError,
			err.Error())
		return createResponse(preRegisterRoute, nil, resErr)
	}
	res := &preRegisterResponse{
		Fee: fee,
	}
	return createResponse(preRegisterRoute, res, nil)
}

// handleRegister handles requests for register. *msgjson.ResponsePayload.Error
// is empty if successful.
func handleRegister(s *RPCServer, params *RawParams) *msgjson.ResponsePayload {
	form, err := parseRegisterArgs(params)
	if err != nil {
		return usage(registerRoute, err)
	}
	defer form.AppPass.Clear()
	fee, err := s.core.GetFee(form.URL, form.Cert)
	if err != nil {
		resErr := msgjson.NewError(msgjson.RPCPreRegisterError,
			err.Error())
		return createResponse(registerRoute, nil, resErr)
	}
	if fee != form.Fee {
		errMsg := fmt.Sprintf("DEX at %s expects a fee of %d but %d was offered", form.URL, fee, form.Fee)
		resErr := msgjson.NewError(msgjson.RPCRegisterError, errMsg)
		return createResponse(registerRoute, nil, resErr)
	}
	err = s.core.Register(form)
	if err != nil {
		resErr := &msgjson.Error{Code: msgjson.RPCRegisterError, Message: err.Error()}
		return createResponse(registerRoute, nil, resErr)
	}

	resp := fmt.Sprintf(feePaidStr, form.Fee)

	return createResponse(registerRoute, &resp, nil)
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
		pwArgsShort: ``,
		argsShort:   ``,
		cmdSummary:  `Print the dex client rpcserver version.`,
		pwArgsLong:  ``,
		argsLong:    ``,
		returns: `Returns:
    string: The dex client rpcserver version.`,
	},
	initRoute: {
		pwArgsShort: `"appPass"`,
		argsShort:   ``,
		cmdSummary:  `Initialize the client.`,
		pwArgsLong: `Password Args:
    appPass (string): The dex client password.`,
		argsLong: ``,
		returns: `Returns:
    string: The message "` + initializedStr + `"`,
	},
	preRegisterRoute: {
		pwArgsShort: ``,
		argsShort:   `"dex" ("cert")`,
		cmdSummary:  `Preregister for dex.`,
		pwArgsLong:  ``,
		argsLong: `Args:
    dex (string): The dex address to preregister for.
    cert (string): Optional. The TLS certificate path.`,
		returns: `Returns:
    obj: The preregister result.
    {
      "fee" (int): The dex registration fee.
    }`,
	},
	newWalletRoute: {
		pwArgsShort: `"appPass" "walletPass"`,
		argsShort:   `assetID "account" ("config")`,
		cmdSummary:  `Connect to a new wallet.`,
		pwArgsLong: `Password Args:
    appPass (string): The dex client password.
    walletPass (string): The wallet's password.`,
		argsLong: `Args:
    assetID (int): The asset's BIP-44 registered coin index. e.g. 42 for DCR.
      See https://github.com/satoshilabs/slips/blob/master/slip-0044.md
    account (string): The account or wallet name, depending on wallet software.
    config (string): Optional. The path to the wallet's config file.`,
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
		pwArgsShort: ``,
		argsShort:   `assetID`,
		cmdSummary:  `Close an open wallet.`,
		pwArgsLong:  ``,
		argsLong: `Args:
    assetID (int): The asset's BIP-44 registered coin index. e.g. 42 for DCR.
      See https://github.com/satoshilabs/slips/blob/master/slip-0044.md`,
		returns: `Returns:
    string: The message "` + fmt.Sprintf(walletLockedStr, "[coin symbol]") + `"`,
	},
	walletsRoute: {
		pwArgsShort: ``,
		argsShort:   ``,
		cmdSummary:  `List all wallets.`,
		pwArgsLong:  ``,
		argsLong:    ``,
		returns: `Returns:
    obj: The wallets result.
    [
      {
        "symbol" (string): The coin symbol.
        "assetID" (int): The asset's BIP-44 registered coin index. e.g. 42 for DCR.
          See https://github.com/satoshilabs/slips/blob/master/slip-0044.md
        "open" (bool): Whether the wallet is unlocked.
        "running" (bool): Whether the wallet is running.
        "updated" (int): Unix time of last balance update. Seconds since 00:00:00 Jan 1 1970.
        "balance" (int): The wallet balance.
        "address" (string): A wallet address.
        "feerate" (int): The fee rate.
        "units" (str): Unit of measure for amounts.
      },...
    ]`,
	},
	registerRoute: {
		pwArgsShort: `"appPass"`,
		argsShort:   `"url" fee ("cert")`,
		cmdSummary: `Register for dex. An ok response does not mean that registration is complete.
Registration is complete after the fee transaction has been confirmed.`,
		pwArgsLong: `Password Args:
    appPass (string): The DEX client password.`,
		argsLong: `Args:
    url (string): The DEX addr to register for.
    fee (int): The DEX fee.
    cert (string): Optional. The TLS certificate path.`,
		returns: `Returns:
    string: The message "` + fmt.Sprintf(feePaidStr, "[fee]") + `"`,
	},
}
