// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build lgpl

package eth

import (
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"

	"decred.org/dcrdex/dex"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/rpc"
)

// DecodeCoinID decodes the coin ID into a common.Hash. For eth, there are no
// funding coin IDs, just an account address. Care should be taken not to use
// DecodeCoinID or (Driver).DecodeCoinID for account addresses.
func DecodeCoinID(coinID []byte) (common.Hash, error) {
	if len(coinID) != common.HashLength {
		return common.Hash{}, fmt.Errorf("wrong coin ID length. wanted %d, got %d",
			common.HashLength, len(coinID))
	}
	var h common.Hash
	h.SetBytes(coinID)
	return h, nil
}

// SecretHashSize is the byte-length of the hash of the secret key used in
// swaps.
const SecretHashSize = 32

// JWTHTTPAuthFn returns a function that creates a signed jwt token using the
// provided secret and inserts it into the passed header.
func JWTHTTPAuthFn(jwtStr string) (func(h http.Header) error, error) {
	s, err := hex.DecodeString(strings.TrimPrefix(jwtStr, "0x"))
	if err != nil {
		return nil, err
	}
	var secret [32]byte
	copy(secret[:], s)
	return node.NewJWTAuth(secret), nil
}

// CheckAPIModules checks that the geth node supports the required modules.
func CheckAPIModules(c *rpc.Client, endpoint string, log dex.Logger) (err error) {
	apis, err := c.SupportedModules()
	if err != nil {
		return fmt.Errorf("unable to check supported modules: %v", err)
	}
	modules := make([]string, 0, len(apis))
	var hasETH bool
	for api, version := range apis {
		if api == "eth" {
			hasETH = true
		}
		modules = append(modules, fmt.Sprintf("%s:%s", api, version))
	}
	if !hasETH {
		return errors.New("eth api not present")
	}
	sort.Strings(modules)
	log.Debugf("API endpoints supported by %s: %s", endpoint, strings.Join(modules, " "))
	return nil
}

// FindJWTHex will detect if thing is hex or a file pointing to hex and return
// that hex. Errors if not hex or a file with just hex.
func FindJWTHex(thing string) (string, error) {
	// If the thing is hex pass it through.
	hexStr := strings.TrimPrefix(thing, "0x")
	_, hexErr := hex.DecodeString(strings.TrimPrefix(hexStr, "0x"))
	if hexErr == nil {
		return thing, nil
	}
	// If not a hex, check if it is a file.
	fp := dex.CleanAndExpandPath(thing)
	if _, err := os.Stat(fp); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("file at %s does not exist", fp)
		}
		return "", fmt.Errorf("jwt does not appear to be hex or a file location: hex error: %v: file error: %v", hexErr, err)
	}
	b, err := os.ReadFile(fp)
	if err != nil {
		return "", fmt.Errorf("unable to read jwt file at %s: %v", fp, err)
	}
	// Check if file contents are hex.
	hexStr = strings.TrimRight(strings.TrimPrefix(string(b), "0x"), "\r\n")
	_, err = hex.DecodeString(hexStr)
	if err != nil {
		return "", fmt.Errorf("file at %s does not appear to contain jwt hex: %v", fp, err)
	}
	return hexStr, nil
}
