// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package rpcserver

import (
	"errors"
	"fmt"
	"strconv"

	"decred.org/dcrdex/client/core"
)

var (
	// errArgs is wrapped when arguments to the known command cannot be parsed.
	errArgs = errors.New("unable to parse arguments")
)

// RawParams is used for all server requests.
type RawParams struct {
	PWArgs []string `json:"PWArgs"`
	Args   []string `json:"args"`
}

// versionResponse holds a semver version JSON object.
type versionResponse struct {
	Major uint32 `json:"major"`
	Minor uint32 `json:"minor"`
	Patch uint32 `json:"patch"`
}

// String satisfies the Stringer interface.
func (vr versionResponse) String() string {
	return fmt.Sprintf("%d.%d.%d", vr.Major, vr.Minor, vr.Patch)
}

// preRegisterResponse is used when responding to the preregister route.
type preRegisterResponse struct {
	Fee uint64 `json:"fee"`
}

// openWalletForm is information necessary to open a wallet.
type openWalletForm struct {
	AssetID uint32 `json:"assetID"`
	AppPass string `json:"appPass"`
}

// newWalletForm is information necessary to create a new wallet.
type newWalletForm struct {
	AssetID    uint32 `json:"assetID"`
	Account    string `json:"account"`
	INIPath    string `json:"inipath"`
	WalletPass string `json:"walletPass"`
	AppPass    string `json:"appPass"`
}

// helpForm is information necessary to obtain help.
type helpForm struct {
	HelpWith         string `json:"helpwith"`
	IncludePasswords bool   `json:"includepasswords"`
}

// checkNArgs checks that args and pwArgs are the correct length.
func checkNArgs(params *RawParams, nPWArgs, nArgs []int) error {
	// For want, one integer indicates an exact match, two are the min and max.
	check := func(have int, want []int) error {
		if len(want) == 1 {
			if want[0] != have {
				return fmt.Errorf("%w: wanted %d but got %d", errArgs, want[0], have)
			}
		} else {
			if have < want[0] || have > want[1] {
				return fmt.Errorf("%w: wanted between %d and %d but got %d", errArgs, want[0], want[1], have)
			}
		}
		return nil
	}
	if err := check(len(params.Args), nArgs); err != nil {
		return fmt.Errorf("arguments: %w", err)
	}
	if err := check(len(params.PWArgs), nPWArgs); err != nil {
		return fmt.Errorf("password arguments: %w", err)
	}
	return nil
}

func checkUIntArg(arg, name string, bitSize int) (uint64, error) {
	i, err := strconv.ParseUint(arg, 10, bitSize)
	if err != nil {
		return i, fmt.Errorf("%w: cannot parse %s: %v", errArgs, name, err)
	}
	return i, nil
}

func checkBoolArg(arg, name string) (bool, error) {
	b, err := strconv.ParseBool(arg)
	if err != nil {
		return b, fmt.Errorf("%w: %s must be a boolean: %v", errArgs, name, err)
	}
	return b, nil
}

func parseHelpArgs(params *RawParams) (*helpForm, error) {
	if err := checkNArgs(params, []int{0}, []int{0, 2}); err != nil {
		return nil, err
	}
	var helpWith string
	if len(params.Args) > 0 {
		helpWith = params.Args[0]
	}
	var includePasswords bool
	if len(params.Args) > 1 {
		var err error
		includePasswords, err = checkBoolArg(params.Args[1], "includepasswords")
		if err != nil {
			return nil, err
		}
	}
	return &helpForm{
		HelpWith:         helpWith,
		IncludePasswords: includePasswords,
	}, nil
}

func parseInitArgs(params *RawParams) (string, error) {
	if err := checkNArgs(params, []int{1}, []int{0}); err != nil {
		return "", err
	}
	return params.PWArgs[0], nil
}

func parseNewWalletArgs(params *RawParams) (*newWalletForm, error) {
	if err := checkNArgs(params, []int{2}, []int{3}); err != nil {
		return nil, err
	}
	assetID, err := checkUIntArg(params.Args[0], "assetID", 32)
	if err != nil {
		return nil, err
	}
	req := &newWalletForm{
		AppPass:    params.PWArgs[0],
		WalletPass: params.PWArgs[1],
		AssetID:    uint32(assetID),
		Account:    params.Args[1],
		INIPath:    params.Args[2],
	}
	return req, nil
}

func parseOpenWalletArgs(params *RawParams) (*openWalletForm, error) {
	if err := checkNArgs(params, []int{1}, []int{1}); err != nil {
		return nil, err
	}
	assetID, err := checkUIntArg(params.Args[0], "assetID", 32)
	if err != nil {
		return nil, err
	}
	req := &openWalletForm{AppPass: params.PWArgs[0], AssetID: uint32(assetID)}
	return req, nil
}

func parseCloseWalletArgs(params *RawParams) (uint32, error) {
	if err := checkNArgs(params, []int{0}, []int{1}); err != nil {
		return 0, err
	}
	assetID, err := checkUIntArg(params.Args[0], "assetID", 32)
	if err != nil {
		return 0, err
	}
	return uint32(assetID), nil
}

func parsePreRegisterArgs(params *RawParams) (*core.PreRegisterForm, error) {
	if err := checkNArgs(params, []int{0}, []int{1, 2}); err != nil {
		return nil, err
	}
	var cert string
	if len(params.Args) > 1 {
		cert = params.Args[1]
	}
	req := &core.PreRegisterForm{
		URL:  params.Args[0],
		Cert: cert,
	}
	return req, nil
}

func parseRegisterArgs(params *RawParams) (*core.RegisterForm, error) {
	if err := checkNArgs(params, []int{1}, []int{2, 3}); err != nil {
		return nil, err
	}
	fee, err := checkUIntArg(params.Args[1], "fee", 64)
	if err != nil {
		return nil, err
	}
	cert := ""
	if len(params.Args) > 2 {
		cert = params.Args[2]
	}
	req := &core.RegisterForm{
		AppPass: params.PWArgs[0],
		URL:     params.Args[0],
		Fee:     fee,
		Cert:    cert,
	}
	return req, nil
}
