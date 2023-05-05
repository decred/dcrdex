// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package eth

import (
	"fmt"
	"sort"
	"strings"

	"decred.org/dcrdex/dex"
	"github.com/ethereum/go-ethereum/common"
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

// CheckAPIModules checks that the geth node supports the required modules.
func CheckAPIModules(c *rpc.Client, endpoint string, log dex.Logger, reqModules []string) (err error) {
	apis, err := c.SupportedModules()
	if err != nil {
		return fmt.Errorf("unable to check supported modules: %v", err)
	}
	reqModulesMap := make(map[string]struct{})
	for _, mod := range reqModules {
		reqModulesMap[mod] = struct{}{}
	}
	haveModules := make([]string, 0, len(apis))
	for api, version := range apis {
		_, has := reqModulesMap[api]
		if has {
			delete(reqModulesMap, api)
		}
		haveModules = append(haveModules, fmt.Sprintf("%s:%s", api, version))
	}
	if len(reqModulesMap) > 0 {
		reqs := make([]string, 0, len(reqModulesMap))
		for v := range reqModulesMap {
			reqs = append(reqs, v)
		}
		return fmt.Errorf("needed apis not present: %v.", strings.Join(reqs, " "))
	}
	sort.Strings(haveModules)
	log.Debugf("API endpoints supported by %s: %s", endpoint, strings.Join(haveModules, " "))
	return nil
}
