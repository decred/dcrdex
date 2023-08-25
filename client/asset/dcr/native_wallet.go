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

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil/v4"
)

const (
	csppConfigFileName = "cspp_config.json"
)

type NativeWallet struct {
	*ExchangeWallet
	csppConfigFilePath string
}

// NativeWallet must also satisfy the following interface(s).
var _ asset.FundsMixer = (*NativeWallet)(nil)

type csppConfig struct {
	CSPPServer       string `json:"csppserver"`
	CSPPServerCAPath string `json:"csppservercapath"`
}

func initNativeWallet(w *ExchangeWallet) (*NativeWallet, error) {
	spvWallet, ok := w.wallet.(*spvWallet)
	if !ok {
		return nil, fmt.Errorf("spvwallet is required to init NativeWallet")
	}

	csppConfigFilePath := filepath.Join(spvWallet.dir, csppConfigFileName)
	b, err := os.ReadFile(csppConfigFilePath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("unable to read cspp config file: %v", err)
	}

	if len(b) > 0 {
		var cfg csppConfig
		err = json.Unmarshal(b, &cfg)
		if err != nil {
			return nil, fmt.Errorf("unable to unmarshal csppConfig: %v", err)
		}

		spvWallet.csppServer = cfg.CSPPServer
		spvWallet.csppTLSConfig, err = makeCSPPTLSConfig(cfg.CSPPServer, cfg.CSPPServerCAPath)
		if err != nil {
			return nil, fmt.Errorf("unable to parse cspp tls config: %v", err)
		}
	}

	return &NativeWallet{
		ExchangeWallet:     w,
		csppConfigFilePath: csppConfigFilePath,
	}, nil
}

func makeCSPPTLSConfig(serverAddress, caPath string) (*tls.Config, error) {
	serverName, _, err := net.SplitHostPort(serverAddress)
	if err != nil {
		return nil, fmt.Errorf("Cannot parse CoinShuffle++ server name %q: %v", serverAddress, err)
	}

	tlsConfig := new(tls.Config)
	tlsConfig.ServerName = serverName

	if caPath != "" {
		caPath = dex.CleanAndExpandPath(caPath)
		ca, err := os.ReadFile(caPath)
		if err != nil {
			return nil, fmt.Errorf("Cannot read CoinShuffle++ Certificate Authority file: %v", err)
		}

		pool := x509.NewCertPool()
		pool.AppendCertsFromPEM(ca)
		tlsConfig.RootCAs = pool
	}

	return tlsConfig, nil
}

// ConfigureFundsMixer configures the wallet for funds mixing. Part of the
// asset.FundsMixer interface.
func (dcr *NativeWallet) ConfigureFundsMixer(serverAddress, serverTLSCertPath string) error {
	spvWallet, ok := dcr.wallet.(*spvWallet)
	if !ok {
		return fmt.Errorf("invalid NativeWallet")
	}

	if serverAddress == "" {
		switch dcr.network {
		case dex.Mainnet:
			serverAddress = defaultCSPPMainnet
		case dex.Testnet:
			serverAddress = defaultCSPPTestnet3
		default:
			return fmt.Errorf("cspp server address is required for network %q (ID %d)", dcr.network, uint8(dcr.network))
		}
	}

	tlsConfig, err := makeCSPPTLSConfig(serverAddress, serverTLSCertPath)
	if err != nil {
		return err
	}

	csppCfgBytes, err := json.Marshal(&csppConfig{
		CSPPServer:       serverAddress,
		CSPPServerCAPath: serverTLSCertPath,
	})
	if err != nil {
		return err
	}
	if err := os.WriteFile(dcr.csppConfigFilePath, csppCfgBytes, 0666); err != nil {
		return err
	}

	spvWallet.updateCSPPServerConfig(serverAddress, tlsConfig)
	return nil
}

// FundsMixingStats returns the current state of the wallet's funds mixer. Part
// of the asset.FundsMixer interface.
func (dcr *NativeWallet) FundsMixingStats() (*asset.FundsMixingStats, error) {
	spvWallet, ok := dcr.wallet.(*spvWallet)
	if !ok {
		return nil, fmt.Errorf("invalid NativeWallet")
	}

	mixedBalance, err := dcr.wallet.AccountBalance(dcr.ctx, 0, mixedAccountName)
	if err != nil {
		return nil, err
	}

	unmixedBalance, err := dcr.wallet.AccountBalance(dcr.ctx, 0, defaultAcctName)
	if err != nil {
		return nil, err
	}

	return &asset.FundsMixingStats{
		Enabled:                 spvWallet.isMixerEnabled(),
		IsMixing:                spvWallet.isMixing(),
		MixedBalance:            toAtoms(mixedBalance.Total),
		UnmixedBalance:          toAtoms(unmixedBalance.Total),
		UnmixedBalanceThreshold: smalletCSPPSplitPoint,
	}, nil
}

// StartFundsMixer starts the funds mixer.  This will error if the wallet does
// not allow starting or stopping the mixer or if the mixer was already
// started. Part of the asset.FundsMixer interface.
func (dcr *NativeWallet) StartFundsMixer(ctx context.Context) error {
	spvWallet, ok := dcr.wallet.(*spvWallet)
	if !ok {
		return fmt.Errorf("invalid NativeWallet")
	}

	return spvWallet.startFundsMixer(ctx)
}

// StopFundsMixer stops the funds mixer. This will error if the wallet does not
// allow starting or stopping the mixer or if the mixer is not already running.
// Part of the asset.FundsMixer interface.
func (dcr *NativeWallet) StopFundsMixer() error {
	spvWallet, ok := dcr.wallet.(*spvWallet)
	if !ok {
		return fmt.Errorf("invalid NativeWallet")
	}
	return spvWallet.StopFundsMixer()
}

// DisableFundsMixer disables the funds mixer and moves all funds to the default
// account. The wallet will need to be re-configured to re-enable mixing. Part
// of the asset.FundsMixer interface.
func (dcr *NativeWallet) DisableFundsMixer() error {
	// Move funds from mixed and trading account to default account.
	unspents, err := dcr.wallet.Unspents(dcr.ctx, mixedAccountName)
	if err != nil {
		return err
	}
	tradingAcctSpendables, err := dcr.wallet.Unspents(dcr.ctx, tradingAccount)
	if err != nil {
		return err
	}
	unspents = append(unspents, tradingAcctSpendables...)

	if len(unspents) > 0 {
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

		tx, totalSent, err := dcr.sendAll(coinsToTransfer, defaultAcctName)
		if err != nil {
			return fmt.Errorf("unable to transfer all funds from mixed and trading accounts: %v", err)
		} else {
			dcr.log.Infof("Transferred %s from mixed and trading accounts to default account in tx %s.",
				dcrutil.Amount(totalSent), tx.TxHash())
		}
	}

	if spvWallet, ok := dcr.wallet.(*spvWallet); ok {
		spvWallet.StopFundsMixer() // ignore any error, just means mixer wasn't running
	}

	if err := os.Remove(dcr.csppConfigFilePath); err != nil {
		return fmt.Errorf("unable to delete cfg file: %v", err)
	}

	return nil
}
