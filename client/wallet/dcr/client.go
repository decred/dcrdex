// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dcr

import (
	"fmt"
	"time"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v2"
	"github.com/decred/dcrd/dcrutil/v2"
	"github.com/decred/dcrd/rpcclient/v5"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrwallet/rpc/jsonrpc/types"
)

// Config represents the configuration details for a decred wallet client.
type Config struct {
	// The wallet rpc user.
	User string
	// The wallet rpc user password.
	Pass string
	// The wallet password.
	WalletPass string
	// The host address.
	Host string
	// The wallet TLS certificate bytes.
	Certs []byte
}

// Client represents a decred wallet client.
type Client struct {
	cfg           *Config
	params        *chaincfg.Params
	rpcc          *rpcclient.Client
	relayFeePerKb dcrutil.Amount
}

var (
	// Wallet unlock timeout in seconds.
	rpcUnlockTimeoutSecs = 5
)

// NewClient creates a decred wallet client. The created client is required to
// have a connection to a consensus daemon and also able to unlock the wallet.
func NewClient(cfg *Config) (*Client, error) {
	rpcCfg := &rpcclient.ConnConfig{
		Host:         cfg.Host,
		Endpoint:     "ws",
		User:         cfg.User,
		Pass:         cfg.Pass,
		Certificates: cfg.Certs,
	}

	rpcc, err := rpcclient.New(rpcCfg, nil)
	if err != nil {
		return nil, err
	}

	c := &Client{
		cfg:  cfg,
		rpcc: rpcc,
	}

	// Ensure the wallet is connected to a consensus daemon.
	walletInfo, err := c.rpcc.WalletInfo()
	if err != nil {
		return nil, err
	}

	if !walletInfo.DaemonConnected {
		return nil, fmt.Errorf("the decred wallet client requires a " +
			"daemon connection")
	}

	// Get the tx fee per kb.
	txFee, err := dcrutil.NewAmount(walletInfo.TxFee)
	if err != nil {
		return nil, fmt.Errorf("unable to parse tx fee per kb: %v", err)
	}

	c.relayFeePerKb = txFee

	// Get the current network.
	net, err := c.rpcc.GetCurrentNet()
	if err != nil {
		return nil, fmt.Errorf("unable to get the current network: %v", err)
	}

	switch net {
	case wire.MainNet:
		c.params = chaincfg.MainNetParams()

	case wire.TestNet3:
		c.params = chaincfg.TestNet3Params()

	case wire.SimNet:
		c.params = chaincfg.SimNetParams()

	default:
		return nil, fmt.Errorf("unknown network: %v", net)
	}

	// Ensure the daemon and jsonrpc api versions are the expected or better.
	vInfo, err := c.rpcc.Version()
	if err != nil {
		return nil, err
	}

	err = checkVersionInfo(vInfo, "dcrd", "dcrdjsonrpcapi", "dcrwalletjsonrpcapi")
	if err != nil {
		return nil, err
	}

	// Ensure the client can unlock the wallet.
	err = c.unlock()
	if err != nil {
		return nil, err
	}

	return c, nil
}

// unlock is a convenience function that unlocks the wallet with a reasonable
// timeout.
func (c *Client) unlock() error {
	err := c.rpcc.WalletPassphrase(c.cfg.WalletPass, int64(rpcUnlockTimeoutSecs))
	if err != nil {
		return err
	}

	return nil
}

// ListUnspent is a wrapper for the listunspent rpc.
func (c *Client) ListUnspent() ([]types.ListUnspentResult, error) {
	err := c.unlock()
	if err != nil {
		return nil, err
	}

	return c.rpcc.ListUnspent()
}

// LockUnspent is a wrapper for the lockunspent rpc.
func (c *Client) LockUnspent(unlock bool, ops []types.ListUnspentResult) error {
	err := c.unlock()
	if err != nil {
		return err
	}

	outputs := make([]*wire.OutPoint, len(ops))
	for idx, out := range ops {
		hash, err := chainhash.NewHashFromStr(out.TxID)
		if err != nil {
			return err
		}

		outputs[idx] = &wire.OutPoint{
			Hash:  *hash,
			Index: out.Vout,
			Tree:  out.Tree,
		}
	}

	return c.rpcc.LockUnspent(unlock, outputs)
}

// ListLockUnspent is a wrapper for the listlockunspent rpc.
func (c *Client) ListLockUnspent() ([]*wire.OutPoint, error) {
	err := c.unlock()
	if err != nil {
		return nil, err
	}

	return c.rpcc.ListLockUnspent()
}

// signRawTransaction is a wrapper for the signrawtransaction rpc.
func (c *Client) signRawTransaction(tx *wire.MsgTx) (*wire.MsgTx, bool, error) {
	err := c.unlock()
	if err != nil {
		return nil, false, err
	}

	return c.rpcc.SignRawTransaction(tx)
}

// sendRawTransaction is a wrapper for the sendrawtransaction rpc.
func (c *Client) sendRawTransaction(tx *wire.MsgTx) (*chainhash.Hash, error) {
	err := c.unlock()
	if err != nil {
		return nil, err
	}

	return c.rpcc.SendRawTransaction(tx, true)
}

// SignMessage is a wrapper for the signmessage rpc.
func (c *Client) SignMessage(address string, message string) (string, error) {
	err := c.unlock()
	if err != nil {
		return "", err
	}

	addr, err := dcrutil.DecodeAddress(address, c.params)
	if err != nil {
		return "", err
	}

	return c.rpcc.SignMessage(addr, message)
}

// getNewAddress is a wrapper for the getnewaddress rpc, it unlocks the wallet
// before calling the rpc.-
func (c *Client) getNewAddress() (dcrutil.Address, error) {
	err := c.unlock()
	if err != nil {
		return nil, err
	}

	addr, err := c.rpcc.GetNewAddress("default", c.params) // what about other accounts?
	return addr, err
}

// generateNewAddresses is a helper for generating a collection of new addresses.
func (c *Client) generateNewAddresses(count uint64) ([]string, error) {
	addrs := make([]string, count)
	for idx := uint64(0); idx < count; idx++ {
		addr, err := c.getNewAddress()
		if err != nil {
			return nil, err
		}

		addrs[idx] = addr.String()
	}

	return addrs, nil
}

