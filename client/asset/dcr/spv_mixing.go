package dcr

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"

	"decred.org/dcrwallet/v3/wallet/udb"
)

const (
	smalletCSPPSplitPoint = 1 << 18 // 262144
	mixedAccountName      = "mixed"
	mixedAccountBranch    = udb.InternalBranch
	tradingAccount        = "dextrading"
)

func (w *spvWallet) updateCSPPServerConfig(serverAddress string, tlsConfig *tls.Config) {
	if serverAddress == w.csppServer && tlsConfig.ServerName == w.csppTLSConfig.ServerName &&
		tlsConfig.RootCAs.Equal(w.csppTLSConfig.RootCAs) {
		return // nothing to update
	}

	w.csppMtx.Lock()
	defer w.csppMtx.Unlock()

	// Stop the mixer if it is currently running. The next run will use the
	// updated server address and tlsConfig.
	if w.cancelCSPPMixer != nil {
		w.cancelCSPPMixer()
		w.cancelCSPPMixer = nil
	}

	w.csppServer = serverAddress
	w.csppTLSConfig = tlsConfig
}

func (w *spvWallet) isMixerEnabled() bool {
	w.csppMtx.Lock()
	defer w.csppMtx.Unlock()
	return w.csppServer != ""
}

// isMixing is true if the wallet is currently mixing funds.
func (w *spvWallet) isMixing() bool {
	w.csppMtx.Lock()
	defer w.csppMtx.Unlock()
	return w.cancelCSPPMixer != nil
}

// startFundsMixer starts mixing funds. Requires the wallet to be unlocked.
func (w *spvWallet) startFundsMixer(ctx context.Context) error {
	w.csppMtx.Lock()
	defer w.csppMtx.Unlock()

	if w.cancelCSPPMixer != nil {
		return fmt.Errorf("funds mixer is already running")
	}

	if w.csppServer == "" {
		return fmt.Errorf("funds mixer is disabled, configure first")
	}

	mixedAccount, err := w.dcrWallet.AccountNumber(ctx, mixedAccountName)
	if err != nil {
		return fmt.Errorf("unable to look up mixed account: %v", err)
	}

	// unmixed account is the default account
	unmixedAccount := uint32(defaultAcct)

	ctx, cancel := context.WithCancel(ctx)
	w.cancelCSPPMixer = cancel

	csppServer, csppTLSConfig := w.csppServer, w.csppTLSConfig
	dialCSPPServer := func(ctx context.Context, network, addr string) (net.Conn, error) {
		conn, err := new(net.Dialer).DialContext(ctx, network, addr)
		if err != nil {
			return nil, err
		}
		conn = tls.Client(conn, csppTLSConfig)
		return conn, nil
	}

	w.log.Debugf("Starting cspp funds mixer with %s", csppServer)

	go func() {
		tipChangeCh, stopTipChangeNtfn := w.dcrWallet.MainTipChangedNotifications()
		defer func() {
			stopTipChangeNtfn()
			w.StopFundsMixer()
		}()

		for {
			select {
			case <-ctx.Done():
				return

			case n := <-tipChangeCh:
				if len(n.AttachedBlocks) == 0 {
					continue
				}

				// Don't perform any actions while transactions are not synced
				// through the tip block.
				if !w.spv.Synced() {
					w.log.Debugf("Skipping autobuyer actions: transactions are not synced")
					continue
				}

				go func() {
					err := w.dcrWallet.MixAccount(ctx, dialCSPPServer, csppServer, unmixedAccount, mixedAccount, mixedAccountBranch)
					if err != nil {
						w.log.Error(err)
					}
				}()
			}
		}
	}()

	return nil
}

// StopFundsMixer stops the funds mixer. This will error if the mixer was not
// already running.
func (w *spvWallet) StopFundsMixer() error {
	w.csppMtx.Lock()
	defer w.csppMtx.Unlock()
	if w.cancelCSPPMixer == nil {
		return fmt.Errorf("funds mixer isn't running")
	}
	w.cancelCSPPMixer()
	w.cancelCSPPMixer = nil
	return nil
}
