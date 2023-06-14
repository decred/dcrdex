// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package webserver

import (
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/client/db"
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
	cert := []byte(form.Cert)
	err := s.core.AddDEX(form.Addr, cert)
	if err != nil {
		s.writeAPIError(w, err)
		return
	}
	writeJSON(w, simpleAck(), s.indent)
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
	writeJSON(w, resp, s.indent)
}

// apiEstimateRegistrationTxFee is the handler for the '/regtxfee' API request.
func (s *WebServer) apiEstimateRegistrationTxFee(w http.ResponseWriter, r *http.Request) {
	form := new(registrationTxFeeForm)
	if !readPost(w, r, form) {
		return
	}
	if form.AssetID == nil {
		s.writeAPIError(w, errors.New("missing asset ID"))
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
	writeJSON(w, resp, s.indent)
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
	txFee, validAddress, err := s.core.EstimateSendTxFee(form.Addr, *form.AssetID, form.Value, form.Subtract)
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
	writeJSON(w, resp, s.indent)
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
	writeJSON(w, resp, s.indent)
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
	writeJSON(w, simpleAck(), s.indent)
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
	writeJSON(w, simpleAck(), s.indent)
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
	writeJSON(w, resp, s.indent)
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
	writeJSON(w, resp, s.indent)
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
	defer zero(pass)
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
	writeJSON(w, resp, s.indent)
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

	feeBuffer, err := s.bondsFeeBuffer(assetID) // could also put it in postBondForm, with some work on the frontend
	if err != nil {
		s.writeAPIError(w, err)
		return
	}

	_, err = s.core.PostBond(&core.PostBondForm{
		Addr:      post.Addr,
		Cert:      []byte(post.Cert),
		AppPass:   pass,
		Bond:      post.Bond,
		Asset:     &assetID,
		LockTime:  post.LockTime,
		FeeBuffer: feeBuffer,
		// Options valid only when creating an account with bond:
		MaintainTier: post.Maintain,
		MaxBondedAmt: post.MaxBondedAmt,
	})
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("add bond error: %w", err))
		return
	}
	// There was no error paying the fee, but we must wait on confirmations
	// before informing the DEX of the fee payment. Those results will come
	// through as a notification.
	writeJSON(w, simpleAck(), s.indent)
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

	writeJSON(w, simpleAck(), s.indent)
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

	writeJSON(w, simpleAck(), s.indent)
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
	status := s.core.WalletState(form.AssetID)
	if status == nil {
		s.writeAPIError(w, fmt.Errorf("no wallet for %d -> %s", form.AssetID, unbip(form.AssetID)))
		return
	}
	err := s.core.RecoverWallet(form.AssetID, form.AppPW, form.Force)
	if err != nil {
		// NOTE: client may check for code activeOrdersErr to prompt for
		// override the active orders safety check.
		s.writeAPIError(w, fmt.Errorf("error recovering %s wallet: %w", unbip(form.AssetID), err))
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
		s.writeAPIError(w, fmt.Errorf("error rescanning %s wallet: %w", unbip(form.AssetID), err))
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
	defer zero(pass)
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
	defer zero(pass)
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
	writeJSON(w, res, s.indent)
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
	defer zero(seed)
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
	defer zero(pass)
	err = s.core.AccountImport(pass, form.Account, form.Bonds)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("error importing account: %w", err))
		return
	}
	w.Header().Set("Connection", "close")
	writeJSON(w, simpleAck(), s.indent)
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

	writeJSON(w, simpleAck(), s.indent)
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

	writeJSON(w, resp, s.indent)
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
	writeJSON(w, resp, s.indent)
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
	if !readPost(w, r, form) {
		return
	}
	err := s.core.Cancel(form.OrderID)
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
	defer zero(init.Seed)
	if !readPost(w, r, init) {
		return
	}
	err := s.core.InitializeClient(init.Pass, init.Seed)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("initialization error: %w", err))
		return
	}
	err = s.actuallyLogin(w, r, &loginForm{Pass: init.Pass, RememberPass: init.RememberPass})
	if err != nil {
		s.writeAPIError(w, err)
		return
	}

	writeJSON(w, struct {
		OK    bool     `json:"ok"`
		Hosts []string `json:"hosts"`
	}{
		OK:    true,
		Hosts: s.knownUnregisteredExchanges(map[string]*core.Exchange{}),
	}, s.indent)
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

	err := s.actuallyLogin(w, r, login)
	if err != nil {
		s.writeAPIError(w, err)
		return
	}

	notes, err := s.core.Notifications(100)
	if err != nil {
		log.Errorf("failed to get notifications: %v", err)
	}

	writeJSON(w, &struct {
		OK    bool               `json:"ok"`
		Notes []*db.Notification `json:"notes"`
	}{
		OK:    true,
		Notes: notes,
	}, s.indent)
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
	writeJSON(w, response, s.indent)
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
	}, s.indent)
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
	}, s.indent)
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
			zero(key)
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

	writeJSON(w, simpleAck(), s.indent)
}

// apiWithdraw handles the 'withdraw' API request. This end-point is Deprecated.
// Use the 'send' end-point.
func (s *WebServer) apiWithdraw(w http.ResponseWriter, r *http.Request) {
	form := new(sendOrWithdrawForm)
	defer form.Pass.Clear()
	if !readPost(w, r, form) {
		return
	}
	form.Subtract = true
	s.send(w, r, form)
}

// apiSend handles the 'send' API request.
func (s *WebServer) apiSend(w http.ResponseWriter, r *http.Request) {
	form := new(sendOrWithdrawForm)
	defer form.Pass.Clear()
	if !readPost(w, r, form) {
		return
	}
	s.send(w, r, form)
}

func (s *WebServer) send(w http.ResponseWriter, r *http.Request, form *sendOrWithdrawForm) {
	state := s.core.WalletState(form.AssetID)
	if state == nil {
		s.writeAPIError(w, fmt.Errorf("no wallet found for %s", unbip(form.AssetID)))
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
		if login.RememberPass {
			key, err := s.cacheAppPassword(pass, authToken)
			if err != nil {
				return fmt.Errorf("login error: %w", err)

			}
			setCookie(pwKeyCK, hex.EncodeToString(key), w)
			zero(key)
		} else {
			// If dexc was shutdown and restarted, the old pw key cookie might
			// need to be cleared.
			clearCookie(pwKeyCK, w)
		}
	}

	return nil
}

// apiUser handles the 'user' API request.
func (s *WebServer) apiUser(w http.ResponseWriter, r *http.Request) {
	response := struct {
		*core.User
		Authed bool `json:"authed"`
		OK     bool `json:"ok"`
	}{
		User:   s.core.User(),
		Authed: s.isAuthed(r),
		OK:     true,
	}
	writeJSON(w, response, s.indent)
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
	writeJSON(w, simpleAck(), s.indent)
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
	writeJSON(w, resp, s.indent)
}

func (s *WebServer) apiShieldedStatus(w http.ResponseWriter, r *http.Request) {
	var assetID uint32
	if !readPost(w, r, &assetID) {
		return
	}
	status, err := s.core.ShieldedStatus(assetID)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("error getting shielded balances: %w", err))
		return
	}
	writeJSON(w, &struct {
		OK       bool                  `json:"ok"`
		Balances *asset.ShieldedStatus `json:"status"`
	}{
		OK:       true,
		Balances: status,
	}, s.indent)
}

func (s *WebServer) apiNewShieldedAddress(w http.ResponseWriter, r *http.Request) {
	var assetID uint32
	if !readPost(w, r, &assetID) {
		return
	}
	addr, err := s.core.NewShieldedAddress(assetID)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("error getting new shielded address: %w", err))
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

type shieldUnshieldForm struct {
	AssetID uint32 `json:"assetID"`
	Amount  uint64 `json:"amt"`
}

func (s *WebServer) apiShieldFunds(w http.ResponseWriter, r *http.Request) {
	var form shieldUnshieldForm
	if !readPost(w, r, &form) {
		return
	}
	_, err := s.core.ShieldFunds(form.AssetID, form.Amount)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("error shielding funds: %w", err))
		return
	}
	writeJSON(w, simpleAck(), s.indent)
}

func (s *WebServer) apiUnshieldFunds(w http.ResponseWriter, r *http.Request) {
	var form shieldUnshieldForm
	if !readPost(w, r, &form) {
		return
	}
	_, err := s.core.UnshieldFunds(form.AssetID, form.Amount)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("error unshielding funds: %w", err))
		return
	}
	writeJSON(w, simpleAck(), s.indent)
}

func (s *WebServer) apiSendShielded(w http.ResponseWriter, r *http.Request) {
	var form struct {
		shieldUnshieldForm
		ToAddress string           `json:"toAddress"`
		AppPW     encode.PassBytes `json:"appPW"`
	}
	if !readPost(w, r, &form) {
		return
	}
	pw, err := s.resolvePass(form.AppPW, r)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("password error: %w", err))
		return
	}
	_, err = s.core.SendShielded(pw, form.AssetID, form.ToAddress, form.Amount)
	if err != nil {
		s.writeAPIError(w, fmt.Errorf("error unshielding funds: %w", err))
		return
	}
	writeJSON(w, simpleAck(), s.indent)
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
