// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package webserver

import (
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"time"

	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/client/db"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/config"
	"decred.org/dcrdex/dex/encode"
)

// apiDiscoverAccount is the handler for the '/discoveracct' API request.
func (s *WebServer) apiDiscoverAccount(w http.ResponseWriter, r *http.Request) {
	form := new(registrationForm)
	if !readPost(w, r, form) {
		return
	}
	cert := []byte(form.Cert)
	pass, err := s.resolvePass(form.Password, r)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("password error: %w", err))
		return
	}
	exchangeInfo, paid, err := s.core.DiscoverAccount(form.Addr, pass, cert)
	if err != nil {
		s.writeAPIError(w, err)
		return
	}
	resp := struct {
		OK       bool           `json:"ok"`
		Exchange *core.Exchange `json:"xc,omitempty"`
		Paid     bool           `json:"paid"`
	}{
		OK:       true,
		Exchange: exchangeInfo,
		Paid:     paid,
	}
	writeJSON(w, resp, s.indent)
}

// apiEstimateRegistrationTxFee is the handler for the '/regtxfee' API request.
func (s *WebServer) apiEstimateRegistrationTxFee(w http.ResponseWriter, r *http.Request) {
	form := new(registrationTxFeeForm)
	if !readPost(w, r, form) {
		return
	}
	cert := []byte(form.Cert)
	txFee, err := s.core.EstimateRegistrationTxFee(form.Addr, cert, *form.AssetID)
	if err != nil {
		s.writeAPIError(w, err)
		return
	}
	resp := struct {
		OK    bool   `json:"ok"`
		TxFee uint64 `json:"txfee"`
	}{
		OK:    true,
		TxFee: txFee,
	}
	writeJSON(w, resp, s.indent)
}

// apiGetDEXInfo is the handler for the '/getdexinfo' API request.
func (s *WebServer) apiGetDEXInfo(w http.ResponseWriter, r *http.Request) {
	form := new(registrationForm)
	if !readPost(w, r, form) {
		return
	}
	cert := []byte(form.Cert)
	exchangeInfo, err := s.core.GetDEXConfig(form.Addr, cert)
	if err != nil {
		s.writeAPIError(w, err)
		return
	}
	resp := struct {
		OK       bool           `json:"ok"`
		Exchange *core.Exchange `json:"xc,omitempty"`
	}{
		OK:       true,
		Exchange: exchangeInfo,
	}
	writeJSON(w, resp, s.indent)
}

// apiRegister is the handler for the '/register' API request.
func (s *WebServer) apiRegister(w http.ResponseWriter, r *http.Request) {
	reg := new(registrationForm)
	defer reg.Password.Clear()
	if !readPost(w, r, reg) {
		return
	}
	assetID := uint32(42)
	if reg.AssetID != nil {
		assetID = *reg.AssetID
	}
	wallet := s.core.WalletState(assetID)
	if wallet == nil {
		s.writeAPIError(w, errors.New("no wallet"))
		return
	}
	pass, err := s.resolvePass(reg.Password, r)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("password error: %w", err))
		return
	}
	_, err = s.core.Register(&core.RegisterForm{
		Addr:    reg.Addr,
		Cert:    []byte(reg.Cert),
		AppPass: pass,
		Fee:     reg.Fee,
		Asset:   &assetID,
	})
	if err != nil {
		s.writeAPIError(w, err)
		return
	}
	// There was no error paying the fee, but we must wait on confirmations
	// before informing the DEX of the fee payment. Those results will come
	// through as a notification.
	writeJSON(w, simpleAck(), s.indent)
}

// apiNewWallet is the handler for the '/newwallet' API request.
func (s *WebServer) apiNewWallet(w http.ResponseWriter, r *http.Request) {
	form := new(newWalletForm)
	defer func() {
		form.AppPW.Clear()
		form.Pass.Clear()
	}()
	if !readPost(w, r, form) {
		return
	}
	has := s.core.WalletState(form.AssetID) != nil
	if has {
		s.writeAPIError(w, fmt.Errorf("already have a wallet for %s", unbip(form.AssetID)))
		return
	}
	pass, err := s.resolvePass(form.AppPW, r)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("password error: %w", err))
		return
	}
	// Wallet does not exist yet. Try to create it.
	err = s.core.CreateWallet(pass, form.Pass, &core.WalletForm{
		AssetID: form.AssetID,
		Type:    form.WalletType,
		Config:  form.Config,
	})
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("error creating %s wallet: %w", unbip(form.AssetID), err))
		return
	}

	writeJSON(w, simpleAck(), s.indent)
}

// apiRescanWallet is the handler for the '/rescanwallet' API request. Commands
// a rescan of the specified wallet.
func (s *WebServer) apiRescanWallet(w http.ResponseWriter, r *http.Request) {
	var form struct {
		AssetID uint32 `json:"assetID"`
		Force   bool   `json:"force"`
	}
	if !readPost(w, r, &form) {
		return
	}
	status := s.core.WalletState(form.AssetID)
	if status == nil {
		s.writeAPIError(w, fmt.Errorf("No wallet for %d -> %s", form.AssetID, unbip(form.AssetID)))
		return
	}
	err := s.core.RescanWallet(form.AssetID, form.Force)
	if err != nil {
		// NOTE: client may may check for code activeOrdersErr to prompt for
		// override the active orders safety check.
		s.writeAPIError(w, fmt.Errorf("error unlocking %s wallet: %w", unbip(form.AssetID), err))
		return
	}

	writeJSON(w, simpleAck(), s.indent)
}

// apiOpenWallet is the handler for the '/openwallet' API request. Unlocks the
// specified wallet.
func (s *WebServer) apiOpenWallet(w http.ResponseWriter, r *http.Request) {
	form := new(openWalletForm)
	defer form.Pass.Clear()
	if !readPost(w, r, form) {
		return
	}
	status := s.core.WalletState(form.AssetID)
	if status == nil {
		s.writeAPIError(w, fmt.Errorf("No wallet for %d -> %s", form.AssetID, unbip(form.AssetID)))
		return
	}
	pass, err := s.resolvePass(form.Pass, r)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("password error: %w", err))
		return
	}
	err = s.core.OpenWallet(form.AssetID, pass)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("error unlocking %s wallet: %w", unbip(form.AssetID), err))
		return
	}

	writeJSON(w, simpleAck(), s.indent)
}

// apiNewDepositAddress gets a new deposit address from a wallet.
func (s *WebServer) apiNewDepositAddress(w http.ResponseWriter, r *http.Request) {
	form := &struct {
		AssetID *uint32 `json:"assetID"`
	}{}
	if !readPost(w, r, form) {
		return
	}
	if form.AssetID == nil {
		s.writeAPIError(w, errors.New("missing asset ID"))
		return
	}
	assetID := *form.AssetID

	addr, err := s.core.NewDepositAddress(assetID)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("error connecting to %s wallet: %w", unbip(assetID), err))
		return
	}

	writeJSON(w, &struct {
		OK      bool   `json:"ok"`
		Address string `json:"address"`
	}{
		OK:      true,
		Address: addr,
	}, s.indent)
}

// apiConnectWallet is the handler for the '/connectwallet' API request.
// Connects to a specified wallet, but does not unlock it.
func (s *WebServer) apiConnectWallet(w http.ResponseWriter, r *http.Request) {
	form := &struct {
		AssetID uint32 `json:"assetID"`
	}{}
	if !readPost(w, r, form) {
		return
	}
	err := s.core.ConnectWallet(form.AssetID)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("error connecting to %s wallet: %w", unbip(form.AssetID), err))
		return
	}

	writeJSON(w, simpleAck(), s.indent)
}

// apiTrade is the handler for the '/trade' API request.
func (s *WebServer) apiTrade(w http.ResponseWriter, r *http.Request) {
	form := new(tradeForm)
	defer form.Pass.Clear()
	if !readPost(w, r, form) {
		return
	}
	r.Close = true
	pass, err := s.resolvePass(form.Pass, r)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("password error: %w", err))
		return
	}
	ord, err := s.core.Trade(pass, form.Order)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("error placing order: %w", err))
		return
	}
	resp := &struct {
		OK    bool        `json:"ok"`
		Order *core.Order `json:"order"`
	}{
		OK:    true,
		Order: ord,
	}
	w.Header().Set("Connection", "close")
	writeJSON(w, resp, s.indent)
}

// apiAccountExport is the handler for the '/exportaccount' API request.
func (s *WebServer) apiAccountExport(w http.ResponseWriter, r *http.Request) {
	form := new(accountExportForm)
	defer form.Pass.Clear()
	if !readPost(w, r, form) {
		return
	}
	r.Close = true
	pass, err := s.resolvePass(form.Pass, r)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("password error: %w", err))
		return
	}
	account, err := s.core.AccountExport(pass, form.Host)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("error exporting account: %w", err))
		return
	}
	w.Header().Set("Connection", "close")
	res := &struct {
		OK      bool          `json:"ok"`
		Account *core.Account `json:"account"`
	}{
		OK:      true,
		Account: account,
	}
	writeJSON(w, res, s.indent)
}

// apiExportSeed is the handler for the '/exportseed' API request.
func (s *WebServer) apiExportSeed(w http.ResponseWriter, r *http.Request) {
	form := &struct {
		Pass encode.PassBytes `json:"pass"`
	}{}
	if !readPost(w, r, form) {
		return
	}
	r.Close = true
	seed, err := s.core.ExportSeed(form.Pass)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("error exporting seed: %w", err))
		return
	}
	writeJSON(w, &struct {
		OK   bool      `json:"ok"`
		Seed dex.Bytes `json:"seed"`
	}{
		OK:   true,
		Seed: seed,
	}, s.indent)
}

// apiAccountImport is the handler for the '/importaccount' API request.
func (s *WebServer) apiAccountImport(w http.ResponseWriter, r *http.Request) {
	form := new(accountImportForm)
	defer form.Pass.Clear()
	if !readPost(w, r, form) {
		return
	}
	r.Close = true
	pass, err := s.resolvePass(form.Pass, r)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("password error: %w", err))
		return
	}
	err = s.core.AccountImport(pass, form.Account)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("error importing account: %w", err))
		return
	}
	w.Header().Set("Connection", "close")
	writeJSON(w, simpleAck(), s.indent)
}

// apiAccountDisable is the handler for the '/disableaccount' API request.
func (s *WebServer) apiAccountDisable(w http.ResponseWriter, r *http.Request) {
	form := new(accountDisableForm)
	defer form.Pass.Clear()
	if !readPost(w, r, form) {
		return
	}

	// Disable account.
	err := s.core.AccountDisable(form.Pass, form.Host)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("error disabling account: %w", err))
		return
	}
	w.Header().Set("Connection", "close")
	writeJSON(w, simpleAck(), s.indent)
}

// apiCancel is the handler for the '/cancel' API request.
func (s *WebServer) apiCancel(w http.ResponseWriter, r *http.Request) {
	form := new(cancelForm)
	defer form.Pass.Clear()
	if !readPost(w, r, form) {
		return
	}
	pass, err := s.resolvePass(form.Pass, r)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("password error: %w", err))
		return
	}
	err = s.core.Cancel(pass, form.OrderID)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("error cancelling order %s: %w", form.OrderID, err))
		return
	}
	writeJSON(w, simpleAck(), s.indent)
}

// apiCloseWallet is the handler for the '/closewallet' API request.
func (s *WebServer) apiCloseWallet(w http.ResponseWriter, r *http.Request) {
	form := &struct {
		AssetID uint32 `json:"assetID"`
	}{}
	if !readPost(w, r, form) {
		return
	}
	err := s.core.CloseWallet(form.AssetID)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("error locking %s wallet: %w", unbip(form.AssetID), err))
		return
	}

	writeJSON(w, simpleAck(), s.indent)
}

// apiInit is the handler for the '/init' API request.
func (s *WebServer) apiInit(w http.ResponseWriter, r *http.Request) {
	init := new(initForm)
	defer init.Pass.Clear()
	if !readPost(w, r, init) {
		return
	}
	err := s.core.InitializeClient(init.Pass, init.Seed)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("initialization error: %w", err))
		return
	}
	s.actuallyLogin(w, r, &loginForm{Pass: init.Pass, RememberPass: init.RememberPass})
}

// apiIsInitialized is the handler for the '/isinitialized' request.
func (s *WebServer) apiIsInitialized(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, &struct {
		OK          bool `json:"ok"`
		Initialized bool `json:"initialized"`
	}{
		OK:          true,
		Initialized: s.core.IsInitialized(),
	}, s.indent)
}

// apiLogin handles the 'login' API request.
func (s *WebServer) apiLogin(w http.ResponseWriter, r *http.Request) {
	login := new(loginForm)
	defer login.Pass.Clear()
	if !readPost(w, r, login) {
		return
	}
	s.actuallyLogin(w, r, login)
}

// apiLogout handles the 'logout' API request.
func (s *WebServer) apiLogout(w http.ResponseWriter, r *http.Request) {
	err := s.core.Logout()
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("logout error: %w", err))
		return
	}

	// With Core locked up, invalidate all known auth tokens and cached passwords
	// to force any other sessions to login again.
	s.deauth()

	clearCookie(authCK, w)
	clearCookie(pwKeyCK, w)

	response := struct {
		OK bool `json:"ok"`
	}{
		OK: true,
	}
	writeJSON(w, response, s.indent)
}

// apiGetBalance handles the 'balance' API request.
func (s *WebServer) apiGetBalance(w http.ResponseWriter, r *http.Request) {
	form := &struct {
		AssetID uint32 `json:"assetID"`
	}{}
	if !readPost(w, r, form) {
		return
	}
	bal, err := s.core.AssetBalance(form.AssetID)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("balance error: %w", err))
		return
	}
	resp := &struct {
		OK      bool                `json:"ok"`
		Balance *core.WalletBalance `json:"balance"`
	}{
		OK:      true,
		Balance: bal,
	}
	writeJSON(w, resp, s.indent)

}

// apiParseConfig parses an INI config file into a map[string]string.
func (s *WebServer) apiParseConfig(w http.ResponseWriter, r *http.Request) {
	form := &struct {
		ConfigText string `json:"configtext"`
	}{}
	if !readPost(w, r, form) {
		return
	}
	configMap, err := config.Parse([]byte(form.ConfigText))
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("parse error: %w", err))
		return
	}
	resp := &struct {
		OK  bool              `json:"ok"`
		Map map[string]string `json:"map"`
	}{
		OK:  true,
		Map: configMap,
	}
	writeJSON(w, resp, s.indent)
}

// apiWalletSettings fetches the currently stored wallet configuration settings.
func (s *WebServer) apiWalletSettings(w http.ResponseWriter, r *http.Request) {
	form := &struct {
		AssetID uint32 `json:"assetID"`
	}{}
	if !readPost(w, r, form) {
		return
	}
	settings, err := s.core.WalletSettings(form.AssetID)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("error setting wallet settings: %w", err))
		return
	}
	writeJSON(w, &struct {
		OK  bool              `json:"ok"`
		Map map[string]string `json:"map"`
	}{
		OK:  true,
		Map: settings,
	}, s.indent)
}

// apiDefaultWalletCfg attempts to load configuration settings from the
// asset's default path on the server.
func (s *WebServer) apiDefaultWalletCfg(w http.ResponseWriter, r *http.Request) {
	form := &struct {
		AssetID uint32 `json:"assetID"`
		Type    string `json:"type"`
	}{}
	if !readPost(w, r, form) {
		return
	}
	cfg, err := s.core.AutoWalletConfig(form.AssetID, form.Type)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("error getting wallet config: %w", err))
		return
	}
	writeJSON(w, struct {
		OK     bool              `json:"ok"`
		Config map[string]string `json:"config"`
	}{
		OK:     true,
		Config: cfg,
	}, s.indent)
}

// apiOrders responds with a filtered list of user orders.
func (s *WebServer) apiOrders(w http.ResponseWriter, r *http.Request) {
	filter := new(core.OrderFilter)
	if !readPost(w, r, filter) {
		return
	}

	ords, err := s.core.Orders(filter)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("Orders error: %w", err))
		return
	}
	writeJSON(w, &struct {
		OK     bool          `json:"ok"`
		Orders []*core.Order `json:"orders"`
	}{
		OK:     true,
		Orders: ords,
	}, s.indent)
}

// apiOrder responds with data for an order.
func (s *WebServer) apiOrder(w http.ResponseWriter, r *http.Request) {
	var oid dex.Bytes
	if !readPost(w, r, &oid) {
		return
	}

	ord, err := s.core.Order(oid)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("Order error: %w", err))
		return
	}
	writeJSON(w, &struct {
		OK    bool        `json:"ok"`
		Order *core.Order `json:"order"`
	}{
		OK:    true,
		Order: ord,
	}, s.indent)
}

// apiChangeAppPass updates the application password.
func (s *WebServer) apiChangeAppPass(w http.ResponseWriter, r *http.Request) {
	form := &struct {
		AppPW    encode.PassBytes `json:"appPW"`
		NewAppPW encode.PassBytes `json:"newAppPW"`
	}{}
	defer form.AppPW.Clear()
	defer form.NewAppPW.Clear()
	if !readPost(w, r, form) {
		return
	}

	// Update application password.
	err := s.core.ChangeAppPass(form.AppPW, form.NewAppPW)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("change app pass error: %w", err))
		return
	}

	passwordIsCached := s.isPasswordCached(r)
	// Since the user changed the password, we clear all of the auth tokens
	// and cached passwords. However, we assign a new auth token and cache
	// the new password (if it was previously cached) for this session.
	s.deauth()
	authToken := s.authorize()
	setCookie(authCK, authToken, w)
	if passwordIsCached {
		key, err := s.cacheAppPassword(form.NewAppPW, authToken)
		if err != nil {
			log.Errorf("unable to cache password: %w", err)
			clearCookie(pwKeyCK, w)
		} else {
			setCookie(pwKeyCK, hex.EncodeToString(key), w)
		}
	}

	writeJSON(w, simpleAck(), s.indent)
}

// apiReconfig sets new configuration details for the wallet.
func (s *WebServer) apiReconfig(w http.ResponseWriter, r *http.Request) {
	form := &struct {
		AssetID    uint32            `json:"assetID"`
		WalletType string            `json:"walletType"`
		Config     map[string]string `json:"config"`
		// newWalletPW json field should be omitted in case caller isn't interested
		// in setting new password, passing null JSON value will cause an unmarshal
		// error.
		NewWalletPW encode.PassBytes `json:"newWalletPW"`
		AppPW       encode.PassBytes `json:"appPW"`
	}{}
	defer form.NewWalletPW.Clear()
	defer form.AppPW.Clear()
	if !readPost(w, r, form) {
		return
	}
	pass, err := s.resolvePass(form.AppPW, r)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("password error: %w", err))
		return
	}
	// Update wallet settings.
	err = s.core.ReconfigureWallet(pass, form.NewWalletPW, &core.WalletForm{
		AssetID: form.AssetID,
		Config:  form.Config,
		Type:    form.WalletType,
	})
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("reconfig error: %w", err))
		return
	}

	writeJSON(w, simpleAck(), s.indent)
}

// apiWithdraw handles the 'withdraw' API request.
func (s *WebServer) apiWithdraw(w http.ResponseWriter, r *http.Request) {
	form := new(withdrawForm)
	defer form.Pass.Clear()
	if !readPost(w, r, form) {
		return
	}
	state := s.core.WalletState(form.AssetID)
	if state == nil {
		s.writeAPIError(w, fmt.Errorf("no wallet found for %s", unbip(form.AssetID)))
		return
	}
	coin, err := s.core.Withdraw(form.Pass, form.AssetID, form.Value, form.Address)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("withdraw error: %w", err))
		return
	}
	resp := struct {
		OK   bool   `json:"ok"`
		Coin string `json:"coin"`
	}{
		OK:   true,
		Coin: coin.String(),
	}
	writeJSON(w, resp, s.indent)
}

// apiMaxBuy handles the 'maxbuy' API request.
func (s *WebServer) apiMaxBuy(w http.ResponseWriter, r *http.Request) {
	form := &struct {
		Host  string `json:"host"`
		Base  uint32 `json:"base"`
		Quote uint32 `json:"quote"`
		Rate  uint64 `json:"rate"`
	}{}
	if !readPost(w, r, form) {
		return
	}
	maxBuy, err := s.core.MaxBuy(form.Host, form.Base, form.Quote, form.Rate)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("max order estimation error: %w", err))
		return
	}
	resp := struct {
		OK     bool                   `json:"ok"`
		MaxBuy *core.MaxOrderEstimate `json:"maxBuy"`
	}{
		OK:     true,
		MaxBuy: maxBuy,
	}
	writeJSON(w, resp, s.indent)
}

// apiMaxSell handles the 'maxsell' API request.
func (s *WebServer) apiMaxSell(w http.ResponseWriter, r *http.Request) {
	form := &struct {
		Host  string `json:"host"`
		Base  uint32 `json:"base"`
		Quote uint32 `json:"quote"`
	}{}
	if !readPost(w, r, form) {
		return
	}
	maxSell, err := s.core.MaxSell(form.Host, form.Base, form.Quote)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("max order estimation error: %w", err))
		return
	}
	resp := struct {
		OK      bool                   `json:"ok"`
		MaxSell *core.MaxOrderEstimate `json:"maxSell"`
	}{
		OK:      true,
		MaxSell: maxSell,
	}
	writeJSON(w, resp, s.indent)
}

// apiPreOrder handles the 'preorder' API request.
func (s *WebServer) apiPreOrder(w http.ResponseWriter, r *http.Request) {
	form := new(core.TradeForm)
	if !readPost(w, r, form) {
		return
	}

	est, err := s.core.PreOrder(form)
	if err != nil {
		s.writeAPIError(w, err)
		return
	}

	resp := struct {
		OK       bool                `json:"ok"`
		Estimate *core.OrderEstimate `json:"estimate"`
	}{
		OK:       true,
		Estimate: est,
	}
	writeJSON(w, resp, s.indent)
}

// apiActuallyLogin logs the user in.
func (s *WebServer) actuallyLogin(w http.ResponseWriter, r *http.Request, login *loginForm) {
	pass, err := s.resolvePass(login.Pass, r)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("password error: %w", err))
		return
	}
	loginResult, err := s.core.Login(pass)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("login error: %w", err))
		return
	}

	user := extractUserInfo(r)
	if !user.Authed {
		authToken := s.authorize()
		setCookie(authCK, authToken, w)
		if login.RememberPass {
			key, err := s.cacheAppPassword(pass, authToken)
			if err != nil {
				s.writeAPIError(w, fmt.Errorf("login error: %w", err))
				return
			}
			setCookie(pwKeyCK, hex.EncodeToString(key), w)
		} else {
			// If dexc was shutdown and restarted, the old pw key cookie might
			// need to be cleared.
			clearCookie(pwKeyCK, w)
		}
	}

	writeJSON(w, struct {
		OK    bool               `json:"ok"`
		Notes []*db.Notification `json:"notes"`
	}{
		OK:    true,
		Notes: loginResult.Notifications,
	}, s.indent)
}

// apiUser handles the 'user' API request.
func (s *WebServer) apiUser(w http.ResponseWriter, r *http.Request) {
	userInfo := extractUserInfo(r)
	response := struct {
		*core.User
		Authed bool `json:"authed"`
		OK     bool `json:"ok"`
	}{
		User:   userInfo.User,
		Authed: userInfo.Authed,
		OK:     true,
	}
	writeJSON(w, response, s.indent)
}

// writeAPIError logs the formatted error and sends a standardResponse with the
// error message.
func (s *WebServer) writeAPIError(w http.ResponseWriter, err error) {
	var cErr *core.Error
	var code *int
	if errors.As(err, &cErr) {
		code = cErr.Code()
	}

	rawErr := core.Unwrap(err)
	resp := &standardResponse{
		OK:   false,
		Msg:  rawErr.Error(),
		Code: code,
	}
	log.Error(err.Error())
	writeJSON(w, resp, s.indent)
}

// setCookie sets the value of a cookie in the http response.
func setCookie(name, value string, w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Path:     "/",
		Value:    value,
		SameSite: http.SameSiteStrictMode,
	})
}

// clearCookie removes a cookie in the http response.
func clearCookie(name string, w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Path:     "/",
		Value:    "",
		Expires:  time.Unix(0, 0),
		SameSite: http.SameSiteStrictMode,
	})
}

// resolvePass returns the appPW if it has a value, but if not, it attempts
// to retrieve the cached password using the information in cookies.
func (s *WebServer) resolvePass(appPW []byte, r *http.Request) ([]byte, error) {
	if len(appPW) > 0 {
		return appPW, nil
	}
	cachedPass, err := s.getCachedPasswordUsingRequest(r)
	if err != nil {
		if errors.Is(err, errNoCachedPW) {
			return nil, fmt.Errorf("app pass cannot be empty")
		}
		return nil, fmt.Errorf("error retrieving cached pw: %w", err)
	}
	return cachedPass, nil
}
