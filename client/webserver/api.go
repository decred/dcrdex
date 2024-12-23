// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package webserver

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/client/db"
	"decred.org/dcrdex/client/mm"
	"decred.org/dcrdex/client/mm/libxc"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/config"
	"decred.org/dcrdex/dex/encode"
)

var zero = encode.ClearBytes

// apiAddDEX is the handler for the '/adddex' API request.
func (s *WebServer) apiAddDEX(w http.ResponseWriter, r *http.Request) {
	form := new(addDexForm)
	if !readPost(w, r, form) {
		return
	}
	defer form.AppPW.Clear()
	appPW, err := s.resolvePass(form.AppPW, r)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("password error: %w", err))
		return
	}
	cert := []byte(form.Cert)

	if err = s.core.AddDEX(appPW, form.Addr, cert); err != nil {
		s.writeAPIError(w, err)
		return
	}
	writeJSON(w, simpleAck())
}

// apiDiscoverAccount is the handler for the '/discoveracct' API request.
func (s *WebServer) apiDiscoverAccount(w http.ResponseWriter, r *http.Request) {
	form := new(registrationForm)
	defer form.Password.Clear()
	if !readPost(w, r, form) {
		return
	}
	cert := []byte(form.Cert)
	pass, err := s.resolvePass(form.Password, r)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("password error: %w", err))
		return
	}
	defer zero(pass)
	exchangeInfo, paid, err := s.core.DiscoverAccount(form.Addr, pass, cert) // TODO: update when paid return removed
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
	writeJSON(w, resp)
}

// apiValidateAddress is the handlers for the '/validateaddress' API request.
func (s *WebServer) apiValidateAddress(w http.ResponseWriter, r *http.Request) {
	form := &struct {
		Addr    string  `json:"addr"`
		AssetID *uint32 `json:"assetID"`
	}{}
	if !readPost(w, r, form) {
		return
	}
	if form.AssetID == nil {
		s.writeAPIError(w, errors.New("missing asset ID"))
		return
	}
	valid, err := s.core.ValidateAddress(form.Addr, *form.AssetID)
	if err != nil {
		s.writeAPIError(w, err)
		return
	}
	resp := struct {
		OK bool `json:"ok"`
	}{
		OK: valid,
	}
	writeJSON(w, resp)
}

// apiEstimateSendTxFee is the handler for the '/txfee' API request.
func (s *WebServer) apiEstimateSendTxFee(w http.ResponseWriter, r *http.Request) {
	form := new(sendTxFeeForm)
	if !readPost(w, r, form) {
		return
	}
	if form.AssetID == nil {
		s.writeAPIError(w, errors.New("missing asset ID"))
		return
	}
	txFee, validAddress, err := s.core.EstimateSendTxFee(form.Addr, *form.AssetID, form.Value, form.Subtract, form.MaxWithdraw)
	if err != nil {
		s.writeAPIError(w, err)
		return
	}
	resp := struct {
		OK           bool   `json:"ok"`
		TxFee        uint64 `json:"txfee"`
		ValidAddress bool   `json:"validaddress"`
	}{
		OK:           true,
		TxFee:        txFee,
		ValidAddress: validAddress,
	}
	writeJSON(w, resp)
}

// apiGetWalletPeers is the handler for the '/getwalletpeers' API request.
func (s *WebServer) apiGetWalletPeers(w http.ResponseWriter, r *http.Request) {
	var form struct {
		AssetID uint32 `json:"assetID"`
	}
	if !readPost(w, r, &form) {
		return
	}
	peers, err := s.core.WalletPeers(form.AssetID)
	if err != nil {
		s.writeAPIError(w, err)
		return
	}
	resp := struct {
		OK    bool                `json:"ok"`
		Peers []*asset.WalletPeer `json:"peers"`
	}{
		OK:    true,
		Peers: peers,
	}
	writeJSON(w, resp)
}

// apiAddWalletPeer is the handler for the '/addwalletpeer' API request.
func (s *WebServer) apiAddWalletPeer(w http.ResponseWriter, r *http.Request) {
	var form struct {
		AssetID uint32 `json:"assetID"`
		Address string `json:"addr"`
	}
	if !readPost(w, r, &form) {
		return
	}
	err := s.core.AddWalletPeer(form.AssetID, form.Address)
	if err != nil {
		s.writeAPIError(w, err)
		return
	}
	writeJSON(w, simpleAck())
}

// apiRemoveWalletPeer is the handler for the '/removewalletpeer' API request.
func (s *WebServer) apiRemoveWalletPeer(w http.ResponseWriter, r *http.Request) {
	var form struct {
		AssetID uint32 `json:"assetID"`
		Address string `json:"addr"`
	}
	if !readPost(w, r, &form) {
		return
	}
	err := s.core.RemoveWalletPeer(form.AssetID, form.Address)
	if err != nil {
		s.writeAPIError(w, err)
		return
	}
	writeJSON(w, simpleAck())
}

func (s *WebServer) apiApproveTokenFee(w http.ResponseWriter, r *http.Request) {
	var form struct {
		AssetID  uint32 `json:"assetID"`
		Version  uint32 `json:"version"`
		Approval bool   `json:"approval"`
	}
	if !readPost(w, r, &form) {
		return
	}

	txFee, err := s.core.ApproveTokenFee(form.AssetID, form.Version, form.Approval)
	if err != nil {
		s.writeAPIError(w, err)
		return
	}

	resp := struct {
		OK    bool   `json:"ok"`
		TxFee uint64 `json:"txFee"`
	}{
		OK:    true,
		TxFee: txFee,
	}
	writeJSON(w, resp)
}

func (s *WebServer) apiApproveToken(w http.ResponseWriter, r *http.Request) {
	var form struct {
		AssetID  uint32           `json:"assetID"`
		DexAddr  string           `json:"dexAddr"`
		Password encode.PassBytes `json:"pass"`
	}
	if !readPost(w, r, &form) {
		return
	}
	pass, err := s.resolvePass(form.Password, r)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("password error: %w", err))
		return
	}
	defer zero(pass)

	txID, err := s.core.ApproveToken(pass, form.AssetID, form.DexAddr, func() {})
	if err != nil {
		s.writeAPIError(w, err)
		return
	}
	resp := struct {
		OK   bool   `json:"ok"`
		TxID string `json:"txID"`
	}{
		OK:   true,
		TxID: txID,
	}
	writeJSON(w, resp)
}

func (s *WebServer) apiUnapproveToken(w http.ResponseWriter, r *http.Request) {
	var form struct {
		AssetID  uint32           `json:"assetID"`
		Version  uint32           `json:"version"`
		Password encode.PassBytes `json:"pass"`
	}
	if !readPost(w, r, &form) {
		return
	}
	pass, err := s.resolvePass(form.Password, r)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("password error: %w", err))
		return
	}
	defer zero(pass)

	txID, err := s.core.UnapproveToken(pass, form.AssetID, form.Version)
	if err != nil {
		s.writeAPIError(w, err)
		return
	}
	resp := struct {
		OK   bool   `json:"ok"`
		TxID string `json:"txID"`
	}{
		OK:   true,
		TxID: txID,
	}
	writeJSON(w, resp)
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
	writeJSON(w, resp)
}

// bondsFeeBuffer is a caching helper for the bonds fee buffer. Values for a
// given asset are cached for 45 minutes. These values are meant to provide a
// sensible but well-padded fee buffer for bond transactions now and well into
// the future, so a long expiry is appropriate.
func (s *WebServer) bondsFeeBuffer(assetID uint32) (feeBuffer uint64, err error) {
	// (*Core).BondsFeeBuffer returns a fresh fee buffer based on a current (but
	// padded) fee rate estimate. We assist the frontend by stabilizing this
	// value for up to 45 minutes from the last request for a given asset. A web
	// app could conceivably do the same, but we'll do this here between the
	// backend (Core) and UI so that a webapp does not need to employ local
	// storage/cookies and associated caching logic.
	const expiry = 45 * time.Minute
	s.bondBufMtx.Lock()
	defer s.bondBufMtx.Unlock()
	if buf, ok := s.bondBuf[assetID]; ok && time.Since(buf.stamp) < expiry {
		feeBuffer = buf.val
		log.Tracef("Using cached bond fee buffer (%v old): %d",
			time.Since(buf.stamp), feeBuffer)
	} else {
		feeBuffer, err = s.core.BondsFeeBuffer(assetID)
		if err != nil {
			return
		}
		log.Tracef("Obtained fresh bond fee buffer: %d", feeBuffer)
		s.bondBuf[assetID] = valStamp{feeBuffer, time.Now()}
	}
	return
}

// apiBondsFeeBuffer is the handler for the '/bondsfeebuffer' API request.
func (s *WebServer) apiBondsFeeBuffer(w http.ResponseWriter, r *http.Request) {
	form := new(bondsFeeBufferForm)
	if !readPost(w, r, form) {
		return
	}
	feeBuffer, err := s.bondsFeeBuffer(form.AssetID)
	if err != nil {
		s.writeAPIError(w, err)
		return
	}
	resp := struct {
		OK        bool   `json:"ok"`
		FeeBuffer uint64 `json:"feeBuffer"`
	}{
		OK:        true,
		FeeBuffer: feeBuffer,
	}
	writeJSON(w, resp)
}

// apiPostBond is the handler for the '/postbond' API request.
func (s *WebServer) apiPostBond(w http.ResponseWriter, r *http.Request) {
	post := new(postBondForm)
	defer post.Password.Clear()
	if !readPost(w, r, post) {
		return
	}
	assetID := uint32(42)
	if post.AssetID != nil {
		assetID = *post.AssetID
	}
	wallet := s.core.WalletState(assetID)
	if wallet == nil {
		s.writeAPIError(w, errors.New("no wallet"))
		return
	}
	pass, err := s.resolvePass(post.Password, r)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("password error: %w", err))
		return
	}
	defer zero(pass)

	bondForm := &core.PostBondForm{
		Addr:     post.Addr,
		Cert:     []byte(post.Cert),
		AppPass:  pass,
		Bond:     post.Bond,
		Asset:    &assetID,
		LockTime: post.LockTime,
		// Options valid only when creating an account with bond:
		MaintainTier: post.Maintain,
		MaxBondedAmt: post.MaxBondedAmt,
	}

	if post.FeeBuffer != nil {
		bondForm.FeeBuffer = *post.FeeBuffer
	}

	_, err = s.core.PostBond(bondForm)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("add bond error: %w", err))
		return
	}
	// There was no error paying the fee, but we must wait on confirmations
	// before informing the DEX of the fee payment. Those results will come
	// through as a notification.
	writeJSON(w, simpleAck())
}

// apiUpdateBondOptions is the handler for the '/updatebondoptions' API request.
func (s *WebServer) apiUpdateBondOptions(w http.ResponseWriter, r *http.Request) {
	form := new(core.BondOptionsForm)
	if !readPost(w, r, form) {
		return
	}

	err := s.core.UpdateBondOptions(form)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("update bond options error: %w", err))
		return
	}

	writeJSON(w, simpleAck())
}

func (s *WebServer) apiRedeemPrepaidBond(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Host  string           `json:"host"`
		Code  dex.Bytes        `json:"code"`
		AppPW encode.PassBytes `json:"appPW"`
		Cert  string           `json:"cert"`
	}
	defer req.AppPW.Clear()
	if !readPost(w, r, &req) {
		return
	}
	appPW, err := s.resolvePass(req.AppPW, r)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("password error: %w", err))
		return
	}
	tier, err := s.core.RedeemPrepaidBond(appPW, req.Code, req.Host, req.Cert)
	if err != nil {
		s.writeAPIError(w, err)
		return
	}
	resp := &struct {
		OK   bool   `json:"ok"`
		Tier uint64 `json:"tier"`
	}{
		OK:   true,
		Tier: tier,
	}
	writeJSON(w, resp)
}

// apiNewWallet is the handler for the '/newwallet' API request.
func (s *WebServer) apiNewWallet(w http.ResponseWriter, r *http.Request) {
	form := new(newWalletForm)
	defer form.AppPW.Clear()
	defer form.Pass.Clear()
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
	defer zero(pass)
	var parentForm *core.WalletForm
	if f := form.ParentForm; f != nil {
		parentForm = &core.WalletForm{
			AssetID: f.AssetID,
			Config:  f.Config,
			Type:    f.WalletType,
		}
	}
	// Wallet does not exist yet. Try to create it.
	err = s.core.CreateWallet(pass, form.Pass, &core.WalletForm{
		AssetID:    form.AssetID,
		Type:       form.WalletType,
		Config:     form.Config,
		ParentForm: parentForm,
	})
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("error creating %s wallet: %w", unbip(form.AssetID), err))
		return
	}

	writeJSON(w, simpleAck())
}

// apiRecoverWallet is the handler for the '/recoverwallet' API request. Commands
// a recovery of the specified wallet.
func (s *WebServer) apiRecoverWallet(w http.ResponseWriter, r *http.Request) {
	var form struct {
		AppPW   encode.PassBytes `json:"appPW"`
		AssetID uint32           `json:"assetID"`
		Force   bool             `json:"force"`
	}
	if !readPost(w, r, &form) {
		return
	}
	appPW, err := s.resolvePass(form.AppPW, r)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("password error: %w", err))
		return
	}
	status := s.core.WalletState(form.AssetID)
	if status == nil {
		s.writeAPIError(w, fmt.Errorf("no wallet for %d -> %s", form.AssetID, unbip(form.AssetID)))
		return
	}
	err = s.core.RecoverWallet(form.AssetID, appPW, form.Force)
	if err != nil {
		// NOTE: client may check for code activeOrdersErr to prompt for
		// override the active orders safety check.
		s.writeAPIError(w, fmt.Errorf("error recovering %s wallet: %w", unbip(form.AssetID), err))
		return
	}

	writeJSON(w, simpleAck())
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
		s.writeAPIError(w, fmt.Errorf("error rescanning %s wallet: %w", unbip(form.AssetID), err))
		return
	}

	writeJSON(w, simpleAck())
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
	defer zero(pass)
	err = s.core.OpenWallet(form.AssetID, pass)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("error unlocking %s wallet: %w", unbip(form.AssetID), err))
		return
	}

	writeJSON(w, simpleAck())
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
	})
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

	writeJSON(w, simpleAck())
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
	defer zero(pass)
	if form.Order == nil {
		s.writeAPIError(w, errors.New("order missing"))
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
	writeJSON(w, resp)
}

// apiTradeAsync is the handler for the '/tradeasync' API request.
func (s *WebServer) apiTradeAsync(w http.ResponseWriter, r *http.Request) {
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
	defer zero(pass)
	ord, err := s.core.TradeAsync(pass, form.Order)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("error placing order: %w", err))
		return
	}
	resp := &struct {
		OK    bool                `json:"ok"`
		Order *core.InFlightOrder `json:"order"`
	}{
		OK:    true,
		Order: ord,
	}
	w.Header().Set("Connection", "close")
	writeJSON(w, resp)
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
	defer zero(pass)
	account, bonds, err := s.core.AccountExport(pass, form.Host)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("error exporting account: %w", err))
		return
	}
	if bonds == nil {
		bonds = make([]*db.Bond, 0) // marshal to [], not null
	}
	w.Header().Set("Connection", "close")
	res := &struct {
		OK      bool          `json:"ok"`
		Account *core.Account `json:"account"`
		Bonds   []*db.Bond    `json:"bonds"`
	}{
		OK:      true,
		Account: account,
		Bonds:   bonds,
	}
	writeJSON(w, res)
}

// apiExportSeed is the handler for the '/exportseed' API request.
func (s *WebServer) apiExportSeed(w http.ResponseWriter, r *http.Request) {
	form := &struct {
		Pass encode.PassBytes `json:"pass"`
	}{}
	defer form.Pass.Clear()
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
		OK   bool   `json:"ok"`
		Seed string `json:"seed"`
	}{
		OK:   true,
		Seed: seed,
	})
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
	defer zero(pass)
	if form.Account == nil {
		s.writeAPIError(w, errors.New("account missing"))
		return
	}
	err = s.core.AccountImport(pass, form.Account, form.Bonds)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("error importing account: %w", err))
		return
	}
	w.Header().Set("Connection", "close")
	writeJSON(w, simpleAck())
}

func (s *WebServer) apiUpdateCert(w http.ResponseWriter, r *http.Request) {
	form := &struct {
		Host string `json:"host"`
		Cert string `json:"cert"`
	}{}
	if !readPost(w, r, form) {
		return
	}

	err := s.core.UpdateCert(form.Host, []byte(form.Cert))
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("error updating cert: %w", err))
		return
	}

	writeJSON(w, simpleAck())
}

func (s *WebServer) apiUpdateDEXHost(w http.ResponseWriter, r *http.Request) {
	form := &struct {
		Pass    encode.PassBytes `json:"pw"`
		OldHost string           `json:"oldHost"`
		NewHost string           `json:"newHost"`
		Cert    string           `json:"cert"`
	}{}
	defer form.Pass.Clear()
	if !readPost(w, r, form) {
		return
	}
	pass, err := s.resolvePass(form.Pass, r)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("password error: %w", err))
		return
	}
	defer zero(pass)
	cert := []byte(form.Cert)
	exchange, err := s.core.UpdateDEXHost(form.OldHost, form.NewHost, pass, cert)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("error updating host: %w", err))
		return
	}

	resp := struct {
		OK       bool           `json:"ok"`
		Exchange *core.Exchange `json:"xc,omitempty"`
	}{
		OK:       true,
		Exchange: exchange,
	}

	writeJSON(w, resp)
}

// apiRestoreWalletInfo is the handler for the '/restorewalletinfo' API
// request.
func (s *WebServer) apiRestoreWalletInfo(w http.ResponseWriter, r *http.Request) {
	form := &struct {
		AssetID uint32
		Pass    encode.PassBytes
	}{}
	defer form.Pass.Clear()
	if !readPost(w, r, form) {
		return
	}

	info, err := s.core.WalletRestorationInfo(form.Pass, form.AssetID)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("error updating cert: %w", err))
		return
	}

	resp := struct {
		OK              bool                       `json:"ok"`
		RestorationInfo []*asset.WalletRestoration `json:"restorationinfo,omitempty"`
	}{
		OK:              true,
		RestorationInfo: info,
	}
	writeJSON(w, resp)
}

// apiToggleAccountStatus is the handler for the '/toggleaccountstatus' API request.
func (s *WebServer) apiToggleAccountStatus(w http.ResponseWriter, r *http.Request) {
	form := new(updateAccountStatusForm)
	defer form.Pass.Clear()
	if !readPost(w, r, form) {
		return
	}
	defer form.Pass.Clear()
	appPW, err := s.resolvePass(form.Pass, r)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("password error: %w", err))
		return
	}
	// Disable account.
	err = s.core.ToggleAccountStatus(appPW, form.Host, form.Disable)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("error updating account status: %w", err))
		return
	}
	if form.Disable {
		w.Header().Set("Connection", "close")
	}
	writeJSON(w, simpleAck())
}

// apiCancel is the handler for the '/cancel' API request.
func (s *WebServer) apiCancel(w http.ResponseWriter, r *http.Request) {
	form := new(cancelForm)
	if !readPost(w, r, form) {
		return
	}
	err := s.core.Cancel(form.OrderID)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("error cancelling order %s: %w", form.OrderID, err))
		return
	}
	writeJSON(w, simpleAck())
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

	writeJSON(w, simpleAck())
}

// apiInit is the handler for the '/init' API request.
func (s *WebServer) apiInit(w http.ResponseWriter, r *http.Request) {
	var init struct {
		Pass encode.PassBytes `json:"pass"`
		Seed string           `json:"seed,omitempty"`
	}
	defer init.Pass.Clear()
	if !readPost(w, r, &init) {
		return
	}
	var seed *string
	if len(init.Seed) > 0 {
		seed = &init.Seed
	}
	mnemonicSeed, err := s.core.InitializeClient(init.Pass, seed)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("initialization error: %w", err))
		return
	}
	err = s.actuallyLogin(w, r, &loginForm{Pass: init.Pass})
	if err != nil {
		s.writeAPIError(w, err)
		return
	}

	writeJSON(w, struct {
		OK           bool     `json:"ok"`
		Hosts        []string `json:"hosts"`
		MnemonicSeed string   `json:"mnemonic"`
	}{
		OK:           true,
		Hosts:        s.knownUnregisteredExchanges(map[string]*core.Exchange{}),
		MnemonicSeed: mnemonicSeed,
	})
}

// apiIsInitialized is the handler for the '/isinitialized' request.
func (s *WebServer) apiIsInitialized(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, &struct {
		OK          bool `json:"ok"`
		Initialized bool `json:"initialized"`
	}{
		OK:          true,
		Initialized: s.core.IsInitialized(),
	})
}

func (s *WebServer) apiLocale(w http.ResponseWriter, r *http.Request) {
	var lang string
	if !readPost(w, r, &lang) {
		return
	}
	m, found := localesMap[lang]
	if !found {
		s.writeAPIError(w, fmt.Errorf("no locale for language %q", lang))
		return
	}
	resp := make(map[string]string)
	for translationID, defaultTranslation := range enUS {
		t, found := m[translationID]
		if !found {
			t = defaultTranslation
		}
		resp[translationID] = t.T
	}

	writeJSON(w, resp)
}

func (s *WebServer) apiSetLocale(w http.ResponseWriter, r *http.Request) {
	var lang string
	if !readPost(w, r, &lang) {
		return
	}
	if err := s.core.SetLanguage(lang); err != nil {
		s.writeAPIError(w, err)
		return
	}

	s.lang.Store(lang)
	if err := s.buildTemplates(lang); err != nil {
		s.writeAPIError(w, err)
		return
	}

	writeJSON(w, simpleAck())
}

// apiLogin handles the 'login' API request.
func (s *WebServer) apiLogin(w http.ResponseWriter, r *http.Request) {
	login := new(loginForm)
	defer login.Pass.Clear()
	if !readPost(w, r, login) {
		return
	}

	err := s.actuallyLogin(w, r, login)
	if err != nil {
		s.writeAPIError(w, err)
		return
	}

	notes, pokes, err := s.core.Notifications(100)
	if err != nil {
		log.Errorf("failed to get notifications: %v", err)
	}

	writeJSON(w, &struct {
		OK    bool               `json:"ok"`
		Notes []*db.Notification `json:"notes"`
		Pokes []*db.Notification `json:"pokes"`
	}{
		OK:    true,
		Notes: notes,
		Pokes: pokes,
	})
}

func (s *WebServer) apiNotes(w http.ResponseWriter, r *http.Request) {
	notes, pokes, err := s.core.Notifications(100)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("failed to get notifications: %w", err))
		return
	}

	writeJSON(w, &struct {
		OK    bool               `json:"ok"`
		Notes []*db.Notification `json:"notes"`
		Pokes []*db.Notification `json:"pokes"`
	}{
		OK:    true,
		Notes: notes,
		Pokes: pokes,
	})
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
	writeJSON(w, response)
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
	writeJSON(w, resp)

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
	writeJSON(w, resp)
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
	})
}

// apiToggleWalletStatus updates the wallet's status.
func (s *WebServer) apiToggleWalletStatus(w http.ResponseWriter, r *http.Request) {
	form := new(walletStatusForm)
	if !readPost(w, r, form) {
		return
	}
	err := s.core.ToggleWalletStatus(form.AssetID, form.Disable)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("error setting wallet settings: %w", err))
		return
	}
	response := struct {
		OK bool `json:"ok"`
	}{
		OK: true,
	}
	writeJSON(w, response)
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
	})
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
	})
}

// apiAccelerateOrder speeds up the mining of transactions in an order.
func (s *WebServer) apiAccelerateOrder(w http.ResponseWriter, r *http.Request) {
	form := struct {
		Pass    encode.PassBytes `json:"pw"`
		OrderID dex.Bytes        `json:"orderID"`
		NewRate uint64           `json:"newRate"`
	}{}
	defer form.Pass.Clear()
	if !readPost(w, r, &form) {
		return
	}
	pass, err := s.resolvePass(form.Pass, r)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("password error: %w", err))
		return
	}

	txID, err := s.core.AccelerateOrder(pass, form.OrderID, form.NewRate)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("Accelerate Order error: %w", err))
		return
	}

	writeJSON(w, &struct {
		OK   bool   `json:"ok"`
		TxID string `json:"txID"`
	}{
		OK:   true,
		TxID: txID,
	})
}

// apiPreAccelerate responds with information about accelerating the mining of
// swaps in an order
func (s *WebServer) apiPreAccelerate(w http.ResponseWriter, r *http.Request) {
	var oid dex.Bytes
	if !readPost(w, r, &oid) {
		return
	}

	preAccelerate, err := s.core.PreAccelerateOrder(oid)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("Pre accelerate error: %w", err))
		return
	}

	writeJSON(w, &struct {
		OK            bool                `json:"ok"`
		PreAccelerate *core.PreAccelerate `json:"preAccelerate"`
	}{
		OK:            true,
		PreAccelerate: preAccelerate,
	})
}

// apiAccelerationEstimate responds with how much it would cost to accelerate
// an order to the requested fee rate.
func (s *WebServer) apiAccelerationEstimate(w http.ResponseWriter, r *http.Request) {
	form := struct {
		OrderID dex.Bytes `json:"orderID"`
		NewRate uint64    `json:"newRate"`
	}{}

	if !readPost(w, r, &form) {
		return
	}

	fee, err := s.core.AccelerationEstimate(form.OrderID, form.NewRate)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("Accelerate Order error: %w", err))
		return
	}

	writeJSON(w, &struct {
		OK  bool   `json:"ok"`
		Fee uint64 `json:"fee"`
	}{
		OK:  true,
		Fee: fee,
	})
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
	})
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
			zero(key)
		}
	}

	writeJSON(w, simpleAck())
}

// apiResetAppPassword resets the application password.
func (s *WebServer) apiResetAppPassword(w http.ResponseWriter, r *http.Request) {
	form := new(struct {
		NewPass encode.PassBytes `json:"newPass"`
		Seed    string           `json:"seed"`
	})
	defer form.NewPass.Clear()
	if !readPost(w, r, form) {
		return
	}

	err := s.core.ResetAppPass(form.NewPass, form.Seed)
	if err != nil {
		s.writeAPIError(w, err)
		return
	}

	writeJSON(w, simpleAck())
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
	defer zero(pass)
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

	writeJSON(w, simpleAck())
}

// apiSend handles the 'send' API request.
func (s *WebServer) apiSend(w http.ResponseWriter, r *http.Request) {
	form := new(sendForm)
	defer form.Pass.Clear()
	if !readPost(w, r, form) {
		return
	}
	state := s.core.WalletState(form.AssetID)
	if state == nil {
		s.writeAPIError(w, fmt.Errorf("no wallet found for %s", unbip(form.AssetID)))
		return
	}
	if len(form.Pass) == 0 {
		s.writeAPIError(w, fmt.Errorf("empty password"))
		return
	}
	coin, err := s.core.Send(form.Pass, form.AssetID, form.Value, form.Address, form.Subtract)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("send/withdraw error: %w", err))
		return
	}
	resp := struct {
		OK   bool   `json:"ok"`
		Coin string `json:"coin"`
	}{
		OK:   true,
		Coin: coin.String(),
	}
	writeJSON(w, resp)
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
	writeJSON(w, resp)
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
	writeJSON(w, resp)
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

	writeJSON(w, resp)
}

// apiActuallyLogin logs the user in. login form private data is expected to be
// cleared by the caller.
func (s *WebServer) actuallyLogin(w http.ResponseWriter, r *http.Request, login *loginForm) error {
	pass, err := s.resolvePass(login.Pass, r)
	defer zero(pass)
	if err != nil {
		return fmt.Errorf("password error: %w", err)
	}
	err = s.core.Login(pass)
	if err != nil {
		return fmt.Errorf("login error: %w", err)
	}

	if !s.isAuthed(r) {
		authToken := s.authorize()
		setCookie(authCK, authToken, w)
		key, err := s.cacheAppPassword(pass, authToken)
		if err != nil {
			return fmt.Errorf("login error: %w", err)

		}
		setCookie(pwKeyCK, hex.EncodeToString(key), w)
		zero(key)
	}

	return nil
}

// apiUser handles the 'user' API request.
func (s *WebServer) apiUser(w http.ResponseWriter, r *http.Request) {
	var u *core.User
	if s.isAuthed(r) {
		u = s.core.User()
	}

	var mmStatus *mm.Status
	if s.mm != nil {
		mmStatus = s.mm.Status()
	}

	response := struct {
		User     *core.User `json:"user"`
		Lang     string     `json:"lang"`
		Langs    []string   `json:"langs"`
		Inited   bool       `json:"inited"`
		OK       bool       `json:"ok"`
		OnionUrl string     `json:"onionUrl"`
		MMStatus *mm.Status `json:"mmStatus"`
	}{
		User:     u,
		Lang:     s.lang.Load().(string),
		Langs:    s.langs,
		Inited:   s.core.IsInitialized(),
		OK:       true,
		OnionUrl: s.onion,
		MMStatus: mmStatus,
	}
	writeJSON(w, response)
}

// apiToggleRateSource handles the /toggleratesource API request.
func (s *WebServer) apiToggleRateSource(w http.ResponseWriter, r *http.Request) {
	form := &struct {
		Disable bool   `json:"disable"`
		Source  string `json:"source"`
	}{}
	if !readPost(w, r, form) {
		return
	}
	err := s.core.ToggleRateSourceStatus(form.Source, form.Disable)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("error disabling/enabling rate source: %w", err))
		return
	}
	writeJSON(w, simpleAck())
}

// apiDeleteArchiveRecords handles the '/deletearchivedrecords' API request.
func (s *WebServer) apiDeleteArchivedRecords(w http.ResponseWriter, r *http.Request) {
	form := new(deleteRecordsForm)
	if !readPost(w, r, form) {
		return
	}

	var olderThan *time.Time
	if form.OlderThanMs > 0 {
		ot := time.UnixMilli(form.OlderThanMs)
		olderThan = &ot
	}

	archivedRecordsPath, nRecordsDeleted, err := s.core.DeleteArchivedRecordsWithBackup(olderThan, form.SaveMatchesToFile, form.SaveOrdersToFile)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("error deleting archived records: %w", err))
		return
	}
	resp := &struct {
		Ok                     bool   `json:"ok"`
		ArchivedRecordsDeleted int    `json:"archivedRecordsDeleted"`
		ArchivedRecordsPath    string `json:"archivedRecordsPath"`
	}{
		Ok:                     true,
		ArchivedRecordsDeleted: nRecordsDeleted,
		ArchivedRecordsPath:    archivedRecordsPath,
	}
	writeJSON(w, resp)
}

func (s *WebServer) apiMarketReport(w http.ResponseWriter, r *http.Request) {
	form := &struct {
		BaseID  uint32 `json:"baseID"`
		QuoteID uint32 `json:"quoteID"`
		Host    string `json:"host"`
	}{}
	if !readPost(w, r, form) {
		return
	}
	report, err := s.mm.MarketReport(form.Host, form.BaseID, form.QuoteID)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("error getting market report: %w", err))
		return
	}
	writeJSON(w, &struct {
		OK     bool             `json:"ok"`
		Report *mm.MarketReport `json:"report"`
	}{
		OK:     true,
		Report: report,
	})
}

func (s *WebServer) apiCEXBalance(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CEXName string `json:"cexName"`
		AssetID uint32 `json:"assetID"`
	}
	if !readPost(w, r, &req) {
		return
	}
	bal, err := s.mm.CEXBalance(req.CEXName, req.AssetID)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("error getting cex balance: %w", err))
		return
	}
	writeJSON(w, &struct {
		OK         bool                   `json:"ok"`
		CEXBalance *libxc.ExchangeBalance `json:"cexBalance"`
	}{
		OK:         true,
		CEXBalance: bal,
	})
}

func (s *WebServer) apiArchivedRuns(w http.ResponseWriter, r *http.Request) {
	runs, err := s.mm.ArchivedRuns()
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("error getting archived runs: %w", err))
		return
	}

	writeJSON(w, &struct {
		OK   bool                  `json:"ok"`
		Runs []*mm.MarketMakingRun `json:"runs"`
	}{
		OK:   true,
		Runs: runs,
	})
}

func (s *WebServer) apiRunLogs(w http.ResponseWriter, r *http.Request) {
	var req struct {
		StartTime int64              `json:"startTime"`
		Market    *mm.MarketWithHost `json:"market"`
		N         uint64             `json:"n"`
		RefID     *uint64            `json:"refID,omitempty"`
		Filters   *mm.RunLogFilters  `json:"filters,omitempty"`
	}
	if !readPost(w, r, &req) {
		return
	}

	if req.Market == nil {
		s.writeAPIError(w, errors.New("market missing"))
		return
	}

	logs, updatedLogs, overview, err := s.mm.RunLogs(req.StartTime, req.Market, req.N, req.RefID, req.Filters)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("error getting run logs: %w", err))
		return
	}

	writeJSON(w, &struct {
		OK          bool                        `json:"ok"`
		Overview    *mm.MarketMakingRunOverview `json:"overview"`
		Logs        []*mm.MarketMakingEvent     `json:"logs"`
		UpdatedLogs []*mm.MarketMakingEvent     `json:"updatedLogs"`
	}{
		OK:          true,
		Overview:    overview,
		Logs:        logs,
		UpdatedLogs: updatedLogs,
	})
}

func (s *WebServer) apiCEXBook(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Host    string `json:"host"`
		BaseID  uint32 `json:"baseID"`
		QuoteID uint32 `json:"quoteID"`
	}
	if !readPost(w, r, &req) {
		return
	}
	buys, sells, err := s.mm.CEXBook(req.Host, req.BaseID, req.QuoteID)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("error CEX Book: %w", err))
		return
	}

	writeJSON(w, &struct {
		OK   bool            `json:"ok"`
		Book *core.OrderBook `json:"book"`
	}{
		OK: true,
		Book: &core.OrderBook{
			Buys:  buys,
			Sells: sells,
		},
	})

}

func (s *WebServer) apiStakeStatus(w http.ResponseWriter, r *http.Request) {
	var assetID uint32
	if !readPost(w, r, &assetID) {
		return
	}
	status, err := s.core.StakeStatus(assetID)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("error fetching stake status for asset ID %d: %w", assetID, err))
		return
	}
	writeJSON(w, &struct {
		OK     bool                       `json:"ok"`
		Status *asset.TicketStakingStatus `json:"status"`
	}{
		OK:     true,
		Status: status,
	})
}

func (s *WebServer) apiSetVSP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AssetID uint32 `json:"assetID"`
		URL     string `json:"url"`
	}
	if !readPost(w, r, &req) {
		return
	}
	if err := s.core.SetVSP(req.AssetID, req.URL); err != nil {
		s.writeAPIError(w, fmt.Errorf("error settings vsp to %q for asset ID %d: %w", req.URL, req.AssetID, err))
		return
	}
	writeJSON(w, simpleAck())
}

func (s *WebServer) apiPurchaseTickets(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AssetID uint32           `json:"assetID"`
		N       int              `json:"n"`
		AppPW   encode.PassBytes `json:"appPW"`
	}
	if !readPost(w, r, &req) {
		return
	}
	appPW, err := s.resolvePass(req.AppPW, r)
	defer zero(appPW)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("password error: %w", err))
		return
	}
	if err = s.core.PurchaseTickets(req.AssetID, appPW, req.N); err != nil {
		s.writeAPIError(w, fmt.Errorf("error purchasing tickets for asset ID %d: %w", req.AssetID, err))
		return
	}
	writeJSON(w, simpleAck())
}

func (s *WebServer) apiSetVotingPreferences(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AssetID        uint32            `json:"assetID"`
		Choices        map[string]string `json:"choices"`
		TSpendPolicy   map[string]string `json:"tSpendPolicy"`
		TreasuryPolicy map[string]string `json:"treasuryPolicy"`
	}
	if !readPost(w, r, &req) {
		return
	}
	if err := s.core.SetVotingPreferences(req.AssetID, req.Choices, req.TSpendPolicy, req.TreasuryPolicy); err != nil {
		s.writeAPIError(w, fmt.Errorf("error setting voting preferences for asset ID %d: %w", req.AssetID, err))
		return
	}
	writeJSON(w, simpleAck())
}

func (s *WebServer) apiListVSPs(w http.ResponseWriter, r *http.Request) {
	var assetID uint32
	if !readPost(w, r, &assetID) {
		return
	}
	vsps, err := s.core.ListVSPs(assetID)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("error listing VSPs for asset ID %d: %w", assetID, err))
		return
	}
	writeJSON(w, &struct {
		OK   bool                           `json:"ok"`
		VSPs []*asset.VotingServiceProvider `json:"vsps"`
	}{
		OK:   true,
		VSPs: vsps,
	})
}

func (s *WebServer) apiTicketPage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AssetID   uint32 `json:"assetID"`
		ScanStart int32  `json:"scanStart"`
		N         int    `json:"n"`
		SkipN     int    `json:"skipN"`
	}
	if !readPost(w, r, &req) {
		return
	}
	tickets, err := s.core.TicketPage(req.AssetID, req.ScanStart, req.N, req.SkipN)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("error retrieving ticket page for %d: %w", req.AssetID, err))
		return
	}
	writeJSON(w, &struct {
		OK      bool            `json:"ok"`
		Tickets []*asset.Ticket `json:"tickets"`
	}{
		OK:      true,
		Tickets: tickets,
	})
}

func (s *WebServer) apiMixingStats(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AssetID uint32 `json:"assetID"`
	}
	if !readPost(w, r, &req) {
		return
	}
	stats, err := s.core.FundsMixingStats(req.AssetID)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("error reteiving mixing stats for %d: %w", req.AssetID, err))
		return
	}
	writeJSON(w, &struct {
		OK    bool                    `json:"ok"`
		Stats *asset.FundsMixingStats `json:"stats"`
	}{
		OK:    true,
		Stats: stats,
	})
}

func (s *WebServer) apiConfigureMixer(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AssetID uint32 `json:"assetID"`
		Enabled bool   `json:"enabled"`
	}
	if !readPost(w, r, &req) {
		return
	}
	pass, err := s.resolvePass(nil, r)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("password error: %w", err))
		return
	}
	defer zero(pass)
	if err := s.core.ConfigureFundsMixer(pass, req.AssetID, req.Enabled); err != nil {
		s.writeAPIError(w, fmt.Errorf("error configuring mixing for %d: %w", req.AssetID, err))
		return
	}
	writeJSON(w, simpleAck())
}

func (s *WebServer) apiStartMarketMakingBot(w http.ResponseWriter, r *http.Request) {
	var form struct {
		Config *mm.StartConfig  `json:"config"`
		AppPW  encode.PassBytes `json:"appPW"`
	}
	defer form.AppPW.Clear()
	if !readPost(w, r, &form) {
		s.writeAPIError(w, fmt.Errorf("failed to read form"))
		return
	}
	appPW, err := s.resolvePass(form.AppPW, r)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("password error: %w", err))
		return
	}
	defer zero(appPW)
	if form.Config == nil {
		s.writeAPIError(w, errors.New("config missing"))
		return
	}
	if err = s.mm.StartBot(form.Config, nil, appPW); err != nil {
		s.writeAPIError(w, fmt.Errorf("error starting market making: %v", err))
		return
	}

	writeJSON(w, simpleAck())
}

func (s *WebServer) apiStopMarketMakingBot(w http.ResponseWriter, r *http.Request) {
	var form struct {
		Market *mm.MarketWithHost `json:"market"`
	}
	if !readPost(w, r, &form) {
		s.writeAPIError(w, fmt.Errorf("failed to read form"))
		return
	}
	if form.Market == nil {
		s.writeAPIError(w, errors.New("market missing"))
		return
	}
	if err := s.mm.StopBot(form.Market); err != nil {
		s.writeAPIError(w, fmt.Errorf("error stopping mm bot %q: %v", form.Market, err))
		return
	}
	writeJSON(w, simpleAck())
}

func (s *WebServer) apiUpdateCEXConfig(w http.ResponseWriter, r *http.Request) {
	var updatedCfg *mm.CEXConfig
	if !readPost(w, r, &updatedCfg) {
		s.writeAPIError(w, fmt.Errorf("failed to read config"))
		return
	}

	if err := s.mm.UpdateCEXConfig(updatedCfg); err != nil {
		s.writeAPIError(w, err)
		return
	}

	writeJSON(w, simpleAck())
}

func (s *WebServer) apiUpdateBotConfig(w http.ResponseWriter, r *http.Request) {
	var updatedCfg *mm.BotConfig
	if !readPost(w, r, &updatedCfg) {
		s.writeAPIError(w, fmt.Errorf("failed to read config"))
		return
	}

	if err := s.mm.UpdateBotConfig(updatedCfg); err != nil {
		s.writeAPIError(w, err)
		return
	}

	writeJSON(w, simpleAck())
}

func (s *WebServer) apiRemoveBotConfig(w http.ResponseWriter, r *http.Request) {
	var form struct {
		Host    string `json:"host"`
		BaseID  uint32 `json:"baseID"`
		QuoteID uint32 `json:"quoteID"`
	}
	if !readPost(w, r, &form) {
		s.writeAPIError(w, fmt.Errorf("failed to read form"))
		return
	}

	if err := s.mm.RemoveBotConfig(form.Host, form.BaseID, form.QuoteID); err != nil {
		s.writeAPIError(w, err)
		return
	}

	writeJSON(w, simpleAck())
}

func (s *WebServer) apiMarketMakingStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, &struct {
		OK     bool       `json:"ok"`
		Status *mm.Status `json:"status"`
	}{
		OK:     true,
		Status: s.mm.Status(),
	})
}

func (s *WebServer) apiTxHistory(w http.ResponseWriter, r *http.Request) {
	var form struct {
		AssetID uint32 `json:"assetID"`
		N       int    `json:"n"`
		RefID   string `json:"refID"`
		Past    bool   `json:"past"`
	}
	if !readPost(w, r, &form) {
		return
	}

	var refID *string
	if len(form.RefID) > 0 {
		refID = &form.RefID
	}

	txs, err := s.core.TxHistory(form.AssetID, form.N, refID, form.Past)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("error getting transaction history: %w", err))
		return
	}
	writeJSON(w, &struct {
		OK  bool                       `json:"ok"`
		Txs []*asset.WalletTransaction `json:"txs"`
	}{
		OK:  true,
		Txs: txs,
	})
}

func (s *WebServer) apiTakeAction(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AssetID  uint32          `json:"assetID"`
		ActionID string          `json:"actionID"`
		Action   json.RawMessage `json:"action"`
	}
	if !readPost(w, r, &req) {
		return
	}
	if err := s.core.TakeAction(req.AssetID, req.ActionID, req.Action); err != nil {
		s.writeAPIError(w, fmt.Errorf("error taking action: %w", err))
		return
	}
	writeJSON(w, simpleAck())
}

func (s *WebServer) apiExportAppLogs(w http.ResponseWriter, r *http.Request) {
	fmt.Printf("apiExportAppLogs")
}

func (s *WebServer) redeemGameCode(w http.ResponseWriter, r *http.Request) {
	var form struct {
		Code  dex.Bytes        `json:"code"`
		Msg   string           `json:"msg"`
		AppPW encode.PassBytes `json:"appPW"`
	}
	if !readPost(w, r, &form) {
		return
	}
	defer form.AppPW.Clear()
	appPW, err := s.resolvePass(form.AppPW, r)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("password error: %w", err))
		return
	}
	coinID, win, err := s.core.RedeemGeocode(appPW, form.Code, form.Msg)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("redemption error: %w", err))
		return
	}
	const dcrBipID = 42
	coinIDString, _ := asset.DecodeCoinID(dcrBipID, coinID)
	writeJSON(w, &struct {
		OK         bool      `json:"ok"`
		CoinID     dex.Bytes `json:"coinID"`
		CoinString string    `json:"coinString"`
		Win        uint64    `json:"win"`
	}{
		OK:         true,
		CoinID:     coinID,
		CoinString: coinIDString,
		Win:        win,
	})
}

// writeAPIError logs the formatted error and sends a standardResponse with the
// error message.
func (s *WebServer) writeAPIError(w http.ResponseWriter, err error) {
	var cErr *core.Error
	var code *int
	if errors.As(err, &cErr) {
		code = cErr.Code()
	}

	innerErr := core.UnwrapErr(err)
	resp := &standardResponse{
		OK:   false,
		Msg:  innerErr.Error(),
		Code: code,
	}
	log.Error(err.Error())
	writeJSON(w, resp)
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
