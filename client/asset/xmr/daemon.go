package xmr

import (
	"encoding/json"
	"os"
	"path"

	"decred.org/dcrdex/dex"
	"github.com/dev-warrior777/go-monero/rpc"
)

const (
	CakeMainnetTLS       = "https://xmr-node.cakewallet.com:18081"
	StackMainnetTLS      = "https://monero.stackwallet.com:18081"
	CakeMainnet          = "http://xmr-node.cakewallet.com:18081"
	MoneroDevsMainnetTLS = "https://node2.monerodevs.org:18089"
	MoneroDevsMainnet_1  = "http://node.monerodevs.org:18089"
	MoneroDevsMainnet_2  = "http://node2.monerodevs.org:18089"
	MoneroDevsMainnet_3  = "http://node3.monerodevs.org:18089"
	MoneroDevsStagenet_1 = "http://node.monerodevs.org:38089"
	MoneroDevsStagenet_2 = "http://node2.monerodevs.org:38089"
	MoneroDevsStagenet_3 = "http://node3.monerodevs.org:38089"
	DexSimnet            = "http://127.0.0.1:18081"
)

const (
	UserDaemonsFilename = "daemons.json"
)

type daemon struct {
	URL string `json:"url"`
	TLS bool   `json:"tls"`
	Net string `json:"net"`
}

// getTrustedDaemons gets known trusted daemon urls. If any user defined daemons
// they get precedence in the list returned.
func getTrustedDaemons(net dex.Network, cli bool, dataDir string) []string {
	var td = make([]string, 0)
	td = getUserDamons(td, net, cli, dataDir)
	switch net {
	case dex.Mainnet:
		if cli {
			td = append(td, CakeMainnetTLS)
			td = append(td, StackMainnetTLS)
			td = append(td, MoneroDevsMainnetTLS)
		}
		td = append(td, CakeMainnet)
		td = append(td, MoneroDevsMainnet_1)
		td = append(td, MoneroDevsMainnet_2)
		td = append(td, MoneroDevsMainnet_3)
	case dex.Testnet:
		td = append(td, MoneroDevsStagenet_1)
		td = append(td, MoneroDevsStagenet_2)
		td = append(td, MoneroDevsStagenet_3)
	case dex.Simnet:
		td = append(td, DexSimnet)
	}
	return td
}

// getUserDamons returns any user defined daemons in dataDir/daemons.json.
// [cli] indicates that we can also select an HTTPS URL to send to monero-wallet-cli
// for initial sync.
func getUserDamons(td []string, net dex.Network, cli bool, dataDir string) []string {
	userDaemonFilepath := path.Join(dataDir, UserDaemonsFilename)
	b, err := os.ReadFile(userDaemonFilepath)
	if err != nil {
		return td
	}
	var daemons []daemon
	err = json.Unmarshal(b, &daemons)
	if err != nil {
		return td
	}
	for _, d := range daemons {
		switch net {
		case dex.Mainnet:
			if d.Net == "main" {
				if (d.TLS && cli) || !d.TLS {
					td = append(td, d.URL)
				}
			}
		case dex.Testnet:
			if d.Net == "stage" {
				if (d.TLS && cli) || !d.TLS {
					td = append(td, d.URL)
				}
			}
		case dex.Simnet:
			if d.Net == "reg" {
				if (d.TLS && cli) || !d.TLS {
					td = append(td, d.URL)
				}
			}
		}
	}
	return td
}

// getInfo retrieves daemon info
func (r *xmrRpc) getInfo() (*rpc.DemonGetInfoResponse, error) {
	giResp, err := r.daemon.DaemonGetInfo(r.ctx)
	if err != nil {
		return nil, err
	}
	return giResp, nil
}

// getFeeRate gives an estimation for fees (atoms) per byte.
func (r *xmrRpc) getFeeRate() (uint64, error) {
	if r.rescanning() {
		return 0, errRescanning
	}
	feeResp, err := r.daemon.DaemonGetFeeEstimate(r.ctx)
	if err != nil {
		r.log.Errorf("getFeeRate - %v", err)
		return 0, err
	}
	return feeResp.Fee, nil
}
