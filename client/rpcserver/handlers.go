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

// routes maps routes to a handler function.
var routes = map[string]func(s *RPCServer, req *msgjson.Message) *msgjson.ResponsePayload{
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
func handleHelp(_ *RPCServer, req *msgjson.Message) *msgjson.ResponsePayload {
	helpWith := ""
	err := req.Unmarshal(&helpWith)
	if err != nil {
		resErr := msgjson.NewError(msgjson.RPCParseError,
			"unable to unmarshal request")
		return createResponse(req.Route, nil, resErr)
	}
	res := ""
	if helpWith == "" {
		// List all commands if no arguments.
		res = ListCommands()
	} else {
		res, err = CommandUsage(helpWith)
		if err != nil {
			resErr := msgjson.NewError(msgjson.RPCUnknownRoute,
				err.Error())
			return createResponse(req.Route, nil, resErr)
		}
	}
	return createResponse(req.Route, &res, nil)
}

// handleInit handles requests for init. *msgjson.ResponsePayload.Error is empty
// if successful.
func handleInit(s *RPCServer, req *msgjson.Message) *msgjson.ResponsePayload {
	appPass := ""
	err := req.Unmarshal(&appPass)
	if err != nil {
		resErr := msgjson.NewError(msgjson.RPCParseError,
			"unable to unmarshal request")
		return createResponse(req.Route, nil, resErr)
	}
	if err := s.core.InitializeClient(appPass); err != nil {
		errMsg := fmt.Sprintf("unable to initialize client: %v", err)
		resErr := msgjson.NewError(msgjson.RPCInitError, errMsg)
		return createResponse(req.Route, nil, resErr)
	}
	res := initializedStr
	return createResponse(req.Route, &res, nil)
}

// handleVersion handles requests for version. It takes no arguments and returns
// the semver.
func handleVersion(_ *RPCServer, req *msgjson.Message) *msgjson.ResponsePayload {
	res := &versionResponse{
		Major: rpcSemverMajor,
		Minor: rpcSemverMinor,
		Patch: rpcSemverPatch,
	}
	return createResponse(req.Route, res.String(), nil)
}

// handleNewWallet handles requests for newwallet.
// *msgjson.ResponsePayload.Error is empty if successful. Returns a
// msgjson.RPCWalletExistsError if a wallet for the assetID already exists.
// Wallet will be unlocked if successful.
func handleNewWallet(s *RPCServer, req *msgjson.Message) *msgjson.ResponsePayload {
	form := new(newWalletForm)
	err := req.Unmarshal(form)
	if err != nil {
		resErr := msgjson.NewError(msgjson.RPCParseError,
			"unable to unmarshal request")
		return createResponse(req.Route, nil, resErr)
	}
	exists := s.core.WalletState(form.AssetID) != nil
	if exists {
		errMsg := fmt.Sprintf("error creating %s wallet: wallet already exists",
			dex.BipIDSymbol(form.AssetID))
		resErr := msgjson.NewError(msgjson.RPCWalletExistsError, errMsg)
		return createResponse(req.Route, nil, resErr)
	}
	// Wallet does not exist yet. Try to create it.
	err = s.core.CreateWallet(form.AppPass, form.WalletPass, &core.WalletForm{
		AssetID: form.AssetID,
		Account: form.Account,
		INIPath: form.INIPath,
	})
	if err != nil {
		errMsg := fmt.Sprintf("error creating %s wallet: %v",
			dex.BipIDSymbol(form.AssetID), err)
		resErr := msgjson.NewError(msgjson.RPCCreateWalletError, errMsg)
		return createResponse(req.Route, nil, resErr)
	}
	s.notifyWalletUpdate(form.AssetID)
	err = s.core.OpenWallet(form.AssetID, form.AppPass)
	if err != nil {
		errMsg := fmt.Sprintf("wallet connected, but failed to open with provided password: %v",
			err)
		resErr := msgjson.NewError(msgjson.RPCOpenWalletError, errMsg)
		return createResponse(req.Route, nil, resErr)
	}
	s.notifyWalletUpdate(form.AssetID)
	res := fmt.Sprintf(walletCreatedStr, dex.BipIDSymbol(form.AssetID))
	return createResponse(req.Route, &res, nil)
}

// handleOpenWallet handles requests for openWallet.
// *msgjson.ResponsePayload.Error is empty if successful. Requires the app
// password. Opens the wallet.
func handleOpenWallet(s *RPCServer, req *msgjson.Message) *msgjson.ResponsePayload {
	form := new(openWalletForm)
	err := req.Unmarshal(form)
	if err != nil {
		resErr := msgjson.NewError(msgjson.RPCParseError,
			"unable to unmarshal request")
		return createResponse(req.Route, nil, resErr)
	}
	err = s.core.OpenWallet(form.AssetID, form.AppPass)
	if err != nil {
		errMsg := fmt.Sprintf("error unlocking %s wallet: %v",
			dex.BipIDSymbol(form.AssetID), err)
		resErr := msgjson.NewError(msgjson.RPCOpenWalletError, errMsg)
		return createResponse(req.Route, nil, resErr)
	}
	s.notifyWalletUpdate(form.AssetID)
	res := fmt.Sprintf(walletUnlockedStr, dex.BipIDSymbol(form.AssetID))
	return createResponse(req.Route, &res, nil)
}

// handleCloseWallet handles requests for closeWallet.
// *msgjson.ResponsePayload.Error is empty if successful. Closes the wallet.
func handleCloseWallet(s *RPCServer, req *msgjson.Message) *msgjson.ResponsePayload {
	var assetID uint32
	err := req.Unmarshal(&assetID)
	if err != nil {
		resErr := msgjson.NewError(msgjson.RPCParseError,
			"unable to unmarshal request")
		return createResponse(req.Route, nil, resErr)
	}
	if err := s.core.CloseWallet(assetID); err != nil {
		errMsg := fmt.Sprintf("unable to close wallet %s: %v",
			dex.BipIDSymbol(assetID), err)
		resErr := msgjson.NewError(msgjson.RPCCloseWalletError, errMsg)
		return createResponse(req.Route, nil, resErr)
	}
	s.notifyWalletUpdate(assetID)
	res := fmt.Sprintf(walletLockedStr, dex.BipIDSymbol(assetID))
	return createResponse(req.Route, &res, nil)
}

// handleWallets handles requests for wallets. Returns a list of wallet details.
func handleWallets(s *RPCServer, req *msgjson.Message) *msgjson.ResponsePayload {
	walletsStates := s.core.Wallets()
	return createResponse(req.Route, walletsStates, nil)
}

// handlePreRegister handles requests for preregister.
// *msgjson.ResponsePayload.Error is empty if successful. Requires the address
// of a dex and returns the dex fee.
func handlePreRegister(s *RPCServer, req *msgjson.Message) *msgjson.ResponsePayload {
	dexURL := ""
	err := req.Unmarshal(&dexURL)
	if err != nil {
		resErr := msgjson.NewError(msgjson.RPCParseError,
			"unable to unmarshal request")
		return createResponse(req.Route, nil, resErr)
	}
	fee, err := s.core.PreRegister(dexURL)
	if err != nil {
		resErr := msgjson.NewError(msgjson.RPCPreRegisterError,
			err.Error())
		return createResponse(req.Route, nil, resErr)
	}
	res := &preRegisterResponse{
		Fee: fee,
	}
	return createResponse(req.Route, res, nil)
}

// handleRegister handles requests for register. *msgjson.ResponsePayload.Error
// is empty if successful.
func handleRegister(s *RPCServer, req *msgjson.Message) *msgjson.ResponsePayload {
	form := new(core.Registration)
	err := req.Unmarshal(form)
	if err != nil {
		resErr := msgjson.NewError(msgjson.RPCParseError, "unable to unmarshal request")
		return createResponse(req.Route, nil, resErr)
	}
	fee, err := s.core.PreRegister(form.DEX)
	if err != nil {
		resErr := msgjson.NewError(msgjson.RPCPreRegisterError,
			err.Error())
		return createResponse(req.Route, nil, resErr)
	}
	if fee != form.Fee {
		errMsg := fmt.Sprintf("DEX at %s expects a fee of %d but %d was offered", form.DEX, fee, form.Fee)
		resErr := msgjson.NewError(msgjson.RPCRegisterError, errMsg)
		return createResponse(req.Route, nil, resErr)
	}
	err = s.core.Register(form)
	if err != nil {
		resErr := &msgjson.Error{Code: msgjson.RPCRegisterError, Message: err.Error()}
		return createResponse(req.Route, nil, resErr)
	}

	resp := fmt.Sprintf(feePaidStr, form.Fee)

	return createResponse(req.Route, &resp, nil)
}

// ListCommands prints a short usage string for every route available to the
// rpcserver.
func ListCommands() string {
	var sb strings.Builder
	var err error
	for _, r := range sortHelpKeys() {
		_, err = sb.WriteString(fmt.Sprintf("%s %s\n",
			r, helpMsgs[r][0]))
		if err != nil {
			log.Errorf("unable to parse help message for %s", r)
			return ""
		}
	}
	s := sb.String()
	// Remove trailing newline.
	return s[:len(s)-1]
}

// CommandUsage returns a help message for cmd or an error if cmd is unknown.
func CommandUsage(cmd string) (string, error) {
	msg, exists := helpMsgs[cmd]
	if !exists {
		return "", fmt.Errorf("%w: %s", ErrUnknownCmd, cmd)
	}
	return fmt.Sprintf("%s %s\n\n%s", cmd, msg[0], msg[1]), nil
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

// helpMsgs are a map of routes to help messages. The first string is a short
// description of usage. The second is a brief description and a breakdown of
// the arguments and return values.
var helpMsgs = map[string][2]string{
	helpRoute: {`("cmd")`,
		`Print a help message.

Args:
    cmd (string): Optional. The command to print help for.

Returns:
    string: The help message for command.`,
	},
	versionRoute: {``,
		`Print the dex client rpcserver version.

Returns:
    string: The dex client rpcserver version.`,
	},
	initRoute: {`"appPass"`,
		`Initialize the client.

Args:
    appPass (string): The dex client password.

Returns:
    string: The message "` + initializedStr + `"`,
	},
	preRegisterRoute: {`"dex"`,
		`Preregister for dex.

Args:
    string: The dex address to preregister for.

Returns:
    obj: The preregister result.
    {
      "fee" (int): The dex registration fee.
    }`,
	},
	newWalletRoute: {`"appPass" "walletPass" assetID "account" "inipath"`,
		`Connect to a new wallet.

Args:
    appPass (string): The dex client password.
    walletPass (string): The wallet's password.
    assetID (int): The asset's BIP-44 registered coin index. e.g. 42 for DCR.
      See https://github.com/satoshilabs/slips/blob/master/slip-0044.md
    account (string): The account or wallet name, depending on wallet software.
    inipath (string): The location of the wallet's config file.

Returns:
    string: The message "` + fmt.Sprintf(walletCreatedStr, "[coin symbol]") + `"`,
	},
	openWalletRoute: {`"appPass" assetID`,
		`Open an existing wallet.

Args:
    appPass (string): The DEX client password.
    assetID (int): The asset's BIP-44 registered coin index. e.g. 42 for DCR.
      See https://github.com/satoshilabs/slips/blob/master/slip-0044.md

Returns:
    string: The message "` + fmt.Sprintf(walletUnlockedStr, "[coin symbol]") + `"`,
	},
	closeWalletRoute: {`assetID`,
		`Close an open wallet.

Args:
    assetID (int): The asset's BIP-44 registered coin index. e.g. 42 for DCR.
      See https://github.com/satoshilabs/slips/blob/master/slip-0044.md

Returns:
    string: The message "` + fmt.Sprintf(walletLockedStr, "[coin symbol]") + `"`,
	},
	walletsRoute: {``,
		`List all wallets.

Returns:
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
	registerRoute: {`"appPass" "dex" fee`,
		`Register for dex. An ok response does not mean that registration is complete.
Registration is complete after the fee transaction has been confirmed.

Args:
    appPass (string): The DEX client password.
    dex (string): The DEX addr to register for.
    fee (int): The DEX fee.

Returns:
    string: The message "` + fmt.Sprintf(feePaidStr, "[fee]") + `"`,
	},
}
