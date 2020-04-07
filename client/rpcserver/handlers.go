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
	versionRoute     = "version"
	walletsRoute     = "wallets"
)

const (
	initializedStr    = "initialized"
	walletLockedStr   = "wallet locked"
	walletUnlockedStr = "wallet unlocked"
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
	helpRoute:        handleHelp,
	initRoute:        handleInit,
	versionRoute:     handleVersion,
	preRegisterRoute: handlePreRegister,
	newWalletRoute:   handleNewWallet,
	openWalletRoute:  handleOpenWallet,
	closeWalletRoute: handleCloseWallet,
	walletsRoute:     handleWallets,
}

// handleHelp handles requests for help. Returns general help for all commands
// if no arguments are passed or verbose help if the passed argument is a known
// command.
func handleHelp(_ *RPCServer, req *msgjson.Message) *msgjson.ResponsePayload {
	helpWith := ""
	err := req.Unmarshal(&helpWith)
	if err != nil {
		resErr := msgjson.NewError(msgjson.RPCParseError, "unable to unmarshal request")
		return createResponse(req.Route, nil, resErr)
	}
	res := ""
	if helpWith == "" {
		// List all commands if no arguments.
		res = ListCommands()
	} else {
		res, err = CommandUsage(helpWith)
		if err != nil {
			resErr := msgjson.NewError(msgjson.RPCUnknownRoute, err.Error())
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
		resErr := msgjson.NewError(msgjson.RPCParseError, "unable to unmarshal request")
		return createResponse(req.Route, nil, resErr)
	}
	if err := s.core.InitializeClient(appPass); err != nil {
		errMsg := fmt.Sprintf("unable to initialize client: %v", err)
		resErr := msgjson.NewError(msgjson.RPCErrorUnspecified, errMsg)
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
// *msgjson.ResponsePayload.Error is empty if successful. Returns whether this
// is a new or preexisting wallet and the locked status.
func handleNewWallet(s *RPCServer, req *msgjson.Message) *msgjson.ResponsePayload {
	form := new(newWalletForm)
	err := req.Unmarshal(form)
	if err != nil {
		resErr := msgjson.NewError(msgjson.RPCParseError, "unable to unmarshal request")
		return createResponse(req.Route, nil, resErr)
	}
	res := new(newWalletResponse)
	walletState := s.core.WalletState(form.AssetID)
	if walletState != nil {
		res.IsLocked = !walletState.Open
		return createResponse(req.Route, res, nil)
	}
	// Wallet does not exist yet. Try to create it.
	err = s.core.CreateWallet(form.AppPass, form.WalletPass, &core.WalletForm{
		AssetID: form.AssetID,
		Account: form.Account,
		INIPath: form.INIPath,
	})
	if err != nil {
		errMsg := fmt.Sprintf("error creating %s wallet: %v", dex.BipIDSymbol(form.AssetID), err)
		resErr := msgjson.NewError(msgjson.RPCErrorUnspecified, errMsg)
		return createResponse(req.Route, nil, resErr)
	}
	s.notifyWalletUpdate(form.AssetID)
	err = s.core.OpenWallet(form.AssetID, form.AppPass)
	if err != nil {
		errMsg := fmt.Sprintf("wallet connected, but failed to open with provided password: %v", err)
		resErr := msgjson.NewError(msgjson.RPCErrorUnspecified, errMsg)
		return createResponse(req.Route, nil, resErr)
	}
	s.notifyWalletUpdate(form.AssetID)
	res.IsNew = true
	res.IsLocked = false
	return createResponse(req.Route, res, nil)
}

// handleOpenWallet handles requests for openWallet.
// *msgjson.ResponsePayload.Error is empty if successful. Requires the app
// password. Opens the wallet.
func handleOpenWallet(s *RPCServer, req *msgjson.Message) *msgjson.ResponsePayload {
	form := new(openWalletForm)
	err := req.Unmarshal(form)
	if err != nil {
		resErr := msgjson.NewError(msgjson.RPCParseError, "unable to unmarshal request")
		return createResponse(req.Route, nil, resErr)
	}
	err = s.core.OpenWallet(form.AssetID, form.AppPass)
	if err != nil {
		errMsg := fmt.Sprintf("error unlocking %s wallet: %v", dex.BipIDSymbol(form.AssetID), err)
		resErr := msgjson.NewError(msgjson.RPCErrorUnspecified, errMsg)
		return createResponse(req.Route, nil, resErr)
	}
	s.notifyWalletUpdate(form.AssetID)
	res := walletUnlockedStr
	return createResponse(req.Route, &res, nil)
}

// handleCloseWallet handles requests for closeWallet.
// *msgjson.ResponsePayload.Error is empty if successful. Closes the wallet.
func handleCloseWallet(s *RPCServer, req *msgjson.Message) *msgjson.ResponsePayload {
	var assetID uint32
	err := req.Unmarshal(&assetID)
	if err != nil {
		resErr := msgjson.NewError(msgjson.RPCParseError, "unable to unmarshal request")
		return createResponse(req.Route, nil, resErr)
	}
	if err := s.core.CloseWallet(assetID); err != nil {
		errMsg := fmt.Sprintf("unable to close wallet %s: %v", dex.BipIDSymbol(assetID), err)
		resErr := msgjson.NewError(msgjson.RPCErrorUnspecified, errMsg)
		return createResponse(req.Route, nil, resErr)
	}
	s.notifyWalletUpdate(assetID)
	res := walletLockedStr
	return createResponse(req.Route, res, nil)
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
		resErr := msgjson.NewError(msgjson.RPCParseError, "unable to unmarshal request")
		return createResponse(req.Route, nil, resErr)
	}
	fee, err := s.core.PreRegister(dexURL)
	if err != nil {
		resErr := msgjson.NewError(msgjson.RPCErrorUnspecified, err.Error())
		return createResponse(req.Route, nil, resErr)
	}
	res := &preRegisterResponse{
		Fee: fee,
	}
	return createResponse(req.Route, res, nil)
}

// ListCommands prints a short usage string for every route available to the
// rpcserver.
func ListCommands() string {
	var sb strings.Builder
	var err error
	for _, r := range sortHelpKeys() {
		_, err = sb.WriteString(fmt.Sprintf("%s %s\n", r, helpMsgs[r][0]))
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
    string: The message "` + initializedStr + `".`,
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
	newWalletRoute: {`assetID "account" "inipath" "walletPass" "appPass"`,
		`Connect to a new wallet.

Args:
    assetID (int): The assets's BIP ID, 42 for dcr or 0 for btc.
    account (string): The account name.
    inipath (string): The location of the wallet's config file.
    walletPass (string): The wallet's password.
    appPass (string): The dex client password.

Returns:
    obj: The newwallet result.
    {
      "isnew" (bool): Whether this wallet was just newly created, or already
   	existed.
      "islocked" (bool): Whether the wallet is locked.
    }`,
	},
	openWalletRoute: {`assetID "appPass"`,
		`Open an existing wallet.

Args:
    assetID (int): The asset's SLIP-0044 ID, 42 for dcr or 0 for btc.
    appPass (string): The DEX client password.

Returns:
    string: The message "` + walletUnlockedStr + `"`,
	},
	closeWalletRoute: {`assetID`,
		`Close an open wallet.

Args:
    assetID (int): The asset's SLIP-0044 ID. 42 for dcr or 0 for btc.

Returns:
    string: The message "` + walletLockedStr + `"`,
	},
	walletsRoute: {``,
		`List all wallets.

Returns:
    obj: The wallets result.
    [
      {
        "symbol" (string): The coin symbol.
        "assetID" (int): The coin SLIP-0044 number.
        "open" (bool): Whether the wallet is unlocked.
        "running" (bool): Whether the wallet is running.
	"updated" (int): Unix time of last balance update. Seconds since
	    00:00:00 Jan 1 1970.
        "balance" (int): The wallet balance.
        "address" (string): A wallet address.
        "feerate" (int): The fee rate.
        "units" (str): Unit of measure for amounts.
      },...
    ]`,
	},
}
