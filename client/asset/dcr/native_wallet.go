// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dcr

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	walletjson "decred.org/dcrwallet/v3/rpc/jsonrpc/types"
	"decred.org/dcrwallet/v3/wallet"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil/v4"
)

const (
	csppConfigFileName = "cspp_config.json"
)

var nativeAccounts = []string{defaultAccountName, mixedAccountName, tradingAccountName}

// mixingConfigFile is the structure for saving cspp server configuration to
// file.
type mixingConfigFile struct {
	CSPPServer string    `json:"csppserver"`
	Cert       dex.Bytes `json:"cert"`
}

// mixingConfig is the current mixer configuration.
type mixingConfig struct {
	server  string
	cert    []byte
	dialer  wallet.DialFunc
	enabled bool
}

// mixer is the settings and concurrency primitives for mixing operations.
type mixer struct {
	mtx    sync.RWMutex
	cfg    *mixingConfig
	ctx    context.Context
	cancel func()
	wg     sync.WaitGroup
}

func (m *mixer) config() *mixingConfig {
	m.mtx.RLock()
	defer m.mtx.RUnlock()
	return m.cfg
}

// turnOn should be called with the mtx locked.
func (m *mixer) turnOn(ctx context.Context) {
	if m.cancel != nil {
		m.cancel()
	}
	m.ctx, m.cancel = context.WithCancel(ctx)
}

// closeAndClear should be called with the mtx locked.
func (m *mixer) closeAndClear() {
	if m.cancel != nil {
		m.cancel()
	}
	m.ctx, m.cancel = nil, nil
}

func (m *mixer) isOn() bool {
	m.mtx.RLock()
	defer m.mtx.RUnlock()
	return m.ctx != nil
}

// NativeWallet implements optional interfaces that are only provided by the
// built-in SPV wallet.
type NativeWallet struct {
	*ExchangeWallet
	csppConfigFilePath string
	spvw               *spvWallet

	mixer *mixer
}

// NativeWallet must also satisfy the following interface(s).
var _ asset.FundsMixer = (*NativeWallet)(nil)
var _ asset.WalletHistorian = (*NativeWallet)(nil)

func initNativeWallet(ew *ExchangeWallet) (*NativeWallet, error) {
	spvWallet, ok := ew.wallet.(*spvWallet)
	if !ok {
		return nil, fmt.Errorf("spvwallet is required to init NativeWallet")
	}

	csppConfigFilePath := filepath.Join(spvWallet.dir, csppConfigFileName)
	cfgFileB, err := os.ReadFile(csppConfigFilePath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("unable to read cspp config file: %v", err)
	}

	var mixCfg *mixingConfig
	if len(cfgFileB) > 0 {
		var cfg mixingConfigFile
		err = json.Unmarshal(cfgFileB, &cfg)
		if err != nil {
			return nil, fmt.Errorf("unable to unmarshal csppConfig: %v", err)
		}

		dialer, err := makeCSPPDialer(cfg.CSPPServer, cfg.Cert)
		if err != nil {
			return nil, fmt.Errorf("unable to parse cspp tls config: %v", err)
		}

		mixCfg = &mixingConfig{
			server:  cfg.CSPPServer,
			dialer:  dialer,
			cert:    cfg.Cert,
			enabled: true,
		}
	}

	if mixCfg == nil {
		mixCfg = &mixingConfig{enabled: false}
	}

	spvWallet.setAccounts(mixCfg.enabled)

	w := &NativeWallet{
		ExchangeWallet:     ew,
		spvw:               spvWallet,
		csppConfigFilePath: csppConfigFilePath,
		mixer: &mixer{
			cfg: mixCfg,
		},
	}
	ew.cycleMixer = func() {
		w.mixer.mtx.RLock()
		defer w.mixer.mtx.RUnlock()
		w.mixFunds()
	}
	ew.mixingConfig = func() *mixingConfig {
		w.mixer.mtx.RLock()
		defer w.mixer.mtx.RUnlock()
		return w.mixer.cfg
	}

	return w, nil
}

func makeCSPPDialer(serverAddress string, certB []byte) (wallet.DialFunc, error) {
	serverName, _, err := net.SplitHostPort(serverAddress)
	if err != nil {
		return nil, fmt.Errorf("cannot parse CoinShuffle++ server name %q: %v", serverAddress, err)
	}

	tlsConfig := new(tls.Config)
	tlsConfig.ServerName = serverName

	if len(certB) > 0 {
		pool := x509.NewCertPool()
		pool.AppendCertsFromPEM(certB)
		tlsConfig.RootCAs = pool
	}

	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		conn, err := new(net.Dialer).DialContext(ctx, network, addr)
		if err != nil {
			return nil, err
		}
		conn = tls.Client(conn, tlsConfig)
		return conn, nil
	}, nil
}

func defaultMixerHostForNet(net dex.Network) string {
	switch net {
	case dex.Mainnet:
		return defaultCSPPMainnet
	case dex.Testnet:
		return defaultCSPPTestnet3
	case dex.Simnet:
		return "fake.simnet.mixer.gov:1000"
	}
	return ""
}

// ConfigureFundsMixer configures the wallet for funds mixing. Part of the
// asset.FundsMixer interface.
func (w *NativeWallet) ConfigureFundsMixer(serverAddress string, cert []byte) (err error) {
	if serverAddress == "" {
		if serverAddress = defaultMixerHostForNet(w.network); serverAddress == "" {
			return fmt.Errorf("cspp server address is required for network %q (ID %d)", w.network, uint8(w.network))
		}
	}

	dialer, err := makeCSPPDialer(serverAddress, cert)
	if err != nil {
		return err
	}

	csppCfgBytes, err := json.Marshal(&mixingConfigFile{
		CSPPServer: serverAddress,
		Cert:       cert,
	})
	if err != nil {
		return fmt.Errorf("error marshaling cspp config file: %w", err)
	}
	if err := os.WriteFile(w.csppConfigFilePath, csppCfgBytes, 0644); err != nil {
		return fmt.Errorf("error writing cspp config file: %w", err)
	}

	w.spvw.setAccounts(true)

	w.mixer.mtx.Lock()
	w.mixer.cfg = &mixingConfig{
		server:  serverAddress,
		dialer:  dialer,
		enabled: true,
		cert:    cert,
	}
	w.mixer.mtx.Unlock()
	return nil
}

// FundsMixingStats returns the current state of the wallet's funds mixer. Part
// of the asset.FundsMixer interface.
func (w *NativeWallet) FundsMixingStats() (*asset.FundsMixingStats, error) {
	mixCfg := w.mixer.config()

	srv := mixCfg.server
	if srv == "" {
		srv = defaultMixerHostForNet(w.network)
	}

	return &asset.FundsMixingStats{
		Enabled:                 mixCfg.enabled,
		IsMixing:                w.mixer.isOn(),
		Server:                  srv,
		UnmixedBalanceThreshold: smalletCSPPSplitPoint,
	}, nil
}

// StartFundsMixer starts the funds mixer.  This will error if the wallet does
// not allow starting or stopping the mixer or if the mixer was already
// started. Part of the asset.FundsMixer interface.
func (w *NativeWallet) StartFundsMixer(ctx context.Context) error {
	w.mixer.mtx.Lock()
	defer w.mixer.mtx.Unlock()
	if !w.mixer.cfg.enabled {
		return errors.New("mixing is not enabled")
	}
	w.mixer.turnOn(ctx)
	w.mixFunds()
	return nil
}

// Lock locks all the native wallet accounts.
func (w *NativeWallet) Lock() (err error) {
	w.mixer.mtx.Lock()
	w.mixer.closeAndClear()
	w.mixer.mtx.Unlock()
	w.mixer.wg.Wait()
	for _, acct := range nativeAccounts {
		if err = w.wallet.LockAccount(w.ctx, acct); err != nil {
			return fmt.Errorf("error locking native wallet account %q: %w", acct, err)
		}
	}
	return nil
}

// mixFunds checks the status of mixing operations and starts a mix cycle.
// mixFunds must be called with the mixer.mtx >= RLock'd.
func (w *NativeWallet) mixFunds() {
	if on := w.mixer.ctx != nil; !on {
		return
	}
	if !w.mixer.cfg.enabled {
		return
	}
	ctx, cfg := w.mixer.ctx, w.mixer.cfg
	if w.network == dex.Simnet {
		w.mixer.wg.Add(1)
		go func() {
			defer w.mixer.wg.Done()
			w.runSimnetMixer(ctx)
		}()
		return
	}
	w.mixer.wg.Add(1)
	go func() {
		defer w.mixer.wg.Done()
		w.spvw.mix(ctx, cfg)
		w.emitBalance()
	}()
}

// runSimnetMixer just sends all funds from the mixed account to the default
// account, after a short delay.
func (w *NativeWallet) runSimnetMixer(ctx context.Context) {
	if err := w.transferAccount(ctx, mixedAccountName, defaultAccountName); err != nil {
		w.log.Errorf("error transferring funds while disabling mixing: %w", err)
	}
}

// StopFundsMixer stops the funds mixer.
func (w *NativeWallet) StopFundsMixer() {
	w.mixer.mtx.Lock()
	w.mixer.closeAndClear()
	w.mixer.mtx.Unlock()
	w.mixer.wg.Wait()
}

// DisableFundsMixer disables the funds mixer and moves all funds to the default
// account. The wallet will need to be re-configured to re-enable mixing. Part
// of the asset.FundsMixer interface.
func (w *NativeWallet) DisableFundsMixer() error {
	w.mixer.mtx.Lock()
	defer w.mixer.mtx.Unlock()

	w.mixer.closeAndClear()
	w.mixer.wg.Wait()

	if err := w.transferAccount(w.ctx, defaultAccountName, mixedAccountName, tradingAccountName); err != nil {
		return fmt.Errorf("error transferring funds while disabling mixing: %w", err)
	}

	w.spvw.setAccounts(false)

	// Delete the cspp config file after moving funds, to prevent the mixer from
	// starting when the wallet is restarted. If moving the funds above failed,
	// this file will be left untouched and the mixer isn't really disabled yet.
	if err := os.Remove(w.csppConfigFilePath); err != nil {
		return fmt.Errorf("unable to delete cfg file: %v", err)
	}
	w.mixer.cfg = &mixingConfig{enabled: false}

	return nil
}

// transferAccount sends all funds from the fromAccts to the toAcct.
func (w *NativeWallet) transferAccount(ctx context.Context, toAcct string, fromAccts ...string) error {
	// Move funds from mixed and trading account to default account.
	var unspents []*walletjson.ListUnspentResult
	for _, acctName := range fromAccts {
		uns, err := w.spvw.Unspents(ctx, acctName)
		if err != nil {
			return fmt.Errorf("error listing unspent outputs for acct %q: %w", acctName, err)
		}
		unspents = append(unspents, uns...)
	}
	if len(unspents) == 0 {
		return nil
	}
	var coinsToTransfer asset.Coins
	for _, unspent := range unspents {
		txHash, err := chainhash.NewHashFromStr(unspent.TxID)
		if err != nil {
			return fmt.Errorf("error decoding txid: %w", err)
		}
		v := toAtoms(unspent.Amount)
		op := newOutput(txHash, unspent.Vout, v, unspent.Tree)
		coinsToTransfer = append(coinsToTransfer, op)
	}

	tx, totalSent, err := w.sendAll(coinsToTransfer, toAcct)
	if err != nil {
		return fmt.Errorf("unable to transfer all funds from %+v accounts: %v", fromAccts, err)
	} else {
		w.log.Infof("Transferred %s from %+v accounts to %s account in tx %s.",
			dcrutil.Amount(totalSent), fromAccts, toAcct, tx.TxHash())
	}
	return nil
}
