// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package admin

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/server/account"
	"decred.org/dcrdex/server/market"
	"github.com/go-chi/chi/v5"
)

const (
	pongStr   = "pong"
	maxUInt16 = int(^uint16(0))
)

// writeJSON marshals the provided interface and writes the bytes to the
// ResponseWriter. The response code is assumed to be StatusOK.
func writeJSON(w http.ResponseWriter, thing interface{}) {
	writeJSONWithStatus(w, thing, http.StatusOK)
}

// writeJSON marshals the provided interface and writes the bytes to the
// ResponseWriter with the specified response code.
func writeJSONWithStatus(w http.ResponseWriter, thing interface{}, code int) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	b, err := json.MarshalIndent(thing, "", "    ")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Errorf("JSON encode error: %v", err)
		return
	}
	w.WriteHeader(code)
	_, err = w.Write(append(b, byte('\n')))
	if err != nil {
		log.Errorf("Write error: %v", err)
	}
}

// apiPing is the handler for the '/ping' API request.
func apiPing(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, pongStr)
}

// apiConfig is the handler for the '/config' API request.
func (s *Server) apiConfig(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.core.ConfigMsg())
}

// apiAsset is the handler for the '/asset/{assetID}' API request.
func (s *Server) apiAsset(w http.ResponseWriter, r *http.Request) {
	assetSymbol := strings.ToLower(chi.URLParam(r, assetSymKey))
	assetID, found := dex.BipSymbolID(assetSymbol)
	if !found {
		http.Error(w, fmt.Sprintf("unknown asset %q", assetSymbol), http.StatusBadRequest)
		return
	}
	asset, err := s.core.Asset(assetID)
	if err != nil {
		http.Error(w, fmt.Sprintf("unsupported asset %q / %d", assetSymbol, assetID), http.StatusBadRequest)
		return
	}

	var errs []string
	backend := asset.Backend
	var scaledFeeRate uint64
	currentFeeRate, err := backend.FeeRate()
	if err != nil {
		errs = append(errs, fmt.Sprintf("unable to get current fee rate: %v", err))
	} else {
		scaledFeeRate = s.core.ScaleFeeRate(assetID, currentFeeRate)
		// Limit the scaled fee rate just as in (*Market).processReadyEpoch.
		if scaledFeeRate > asset.MaxFeeRate {
			scaledFeeRate = asset.MaxFeeRate
		}
	}

	synced, err := backend.Synced()
	if err != nil {
		errs = append(errs, fmt.Sprintf("unable to get sync status: %v", err))
	}

	res := &AssetInfo{
		Asset:          asset.Asset,
		CurrentFeeRate: currentFeeRate,
		ScaledFeeRate:  scaledFeeRate,
		Synced:         synced,
		Errors:         errs,
	}
	writeJSON(w, res)
}

func (s *Server) apiSetFeeScale(w http.ResponseWriter, r *http.Request) {
	assetSymbol := strings.ToLower(chi.URLParam(r, assetSymKey))
	assetID, found := dex.BipSymbolID(assetSymbol)
	if !found {
		http.Error(w, fmt.Sprintf("unknown asset %q", assetSymbol), http.StatusBadRequest)
		return
	}

	feeRateScaleStr := chi.URLParam(r, scaleKey)
	feeRateScale, err := strconv.ParseFloat(feeRateScaleStr, 64)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid fee rate scale %q", feeRateScaleStr), http.StatusBadRequest)
		return
	}

	_, err = s.core.Asset(assetID) // asset return may be used if other asset settings are modified
	if err != nil {
		http.Error(w, fmt.Sprintf("unsupported asset %q / %d", assetSymbol, assetID), http.StatusBadRequest)
		return
	}

	log.Infof("Setting %s (%d) fee rate scale factor to %f", strings.ToUpper(assetSymbol), assetID, feeRateScale)
	s.core.SetFeeRateScale(assetID, feeRateScale)

	w.WriteHeader(http.StatusOK)
}

func (s *Server) apiMarkets(w http.ResponseWriter, r *http.Request) {
	statuses := s.core.MarketStatuses()
	mktStatuses := make(map[string]*MarketStatus)
	for name, status := range statuses {
		mktStatus := &MarketStatus{
			// Name is empty since the key is the name.
			Running:       status.Running,
			EpochDuration: status.EpochDuration,
			ActiveEpoch:   status.ActiveEpoch,
			StartEpoch:    status.StartEpoch,
			SuspendEpoch:  status.SuspendEpoch,
		}
		if status.SuspendEpoch != 0 {
			persist := status.PersistBook
			mktStatus.PersistBook = &persist
		}
		mktStatuses[name] = mktStatus
	}

	writeJSON(w, mktStatuses)
}

// apiMarketInfo is the handler for the '/market/{marketName}' API request.
func (s *Server) apiMarketInfo(w http.ResponseWriter, r *http.Request) {
	mkt := strings.ToLower(chi.URLParam(r, marketNameKey))
	status := s.core.MarketStatus(mkt)
	if status == nil {
		http.Error(w, fmt.Sprintf("unknown market %q", mkt), http.StatusBadRequest)
		return
	}

	var persist *bool
	if status.SuspendEpoch != 0 {
		persistLocal := status.PersistBook
		persist = &persistLocal
	}
	mktStatus := &MarketStatus{
		Name:          mkt,
		Running:       status.Running,
		EpochDuration: status.EpochDuration,
		ActiveEpoch:   status.ActiveEpoch,
		StartEpoch:    status.StartEpoch,
		SuspendEpoch:  status.SuspendEpoch,
		PersistBook:   persist,
	}
	if status.SuspendEpoch != 0 {
		persist := status.PersistBook
		mktStatus.PersistBook = &persist
	}
	writeJSON(w, mktStatus)
}

// apiMarketOrderBook is the handler for the '/market/{marketName}/orderbook'
// API request.
func (s *Server) apiMarketOrderBook(w http.ResponseWriter, r *http.Request) {
	mkt := strings.ToLower(chi.URLParam(r, marketNameKey))
	status := s.core.MarketStatus(mkt)
	if status == nil {
		http.Error(w, fmt.Sprintf("unknown market %q", mkt), http.StatusBadRequest)
		return
	}
	orders, err := s.core.BookOrders(status.Base, status.Quote)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to obtain order book: %v", err), http.StatusInternalServerError)
		return
	}
	msgBook := make([]*msgjson.BookOrderNote, 0, len(orders))
	for _, o := range orders {
		msgOrder, err := market.OrderToMsgOrder(o, mkt)
		if err != nil {
			log.Errorf("unable to encode order: %w", err)
			continue
		}
		msgBook = append(msgBook, msgOrder)
	}
	// This is a msgjson.OrderBook without the seq field.
	res := &struct {
		MarketID string                   `json:"marketid"`
		Epoch    uint64                   `json:"epoch"`
		Orders   []*msgjson.BookOrderNote `json:"orders"`
	}{
		MarketID: mkt,
		Epoch:    uint64(status.ActiveEpoch),
		Orders:   msgBook,
	}
	writeJSON(w, res)
}

// hander for route '/market/{marketName}/epochorders' API request.
func (s *Server) apiMarketEpochOrders(w http.ResponseWriter, r *http.Request) {
	mkt := strings.ToLower(chi.URLParam(r, marketNameKey))
	status := s.core.MarketStatus(mkt)
	if status == nil {
		http.Error(w, fmt.Sprintf("unknown market %q", mkt), http.StatusBadRequest)
		return
	}
	orders, err := s.core.EpochOrders(status.Base, status.Quote)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to obtain epoch orders: %v", err), http.StatusInternalServerError)
		return
	}
	msgBook := make([]*msgjson.BookOrderNote, 0, len(orders))
	for _, o := range orders {
		msgOrder, err := market.OrderToMsgOrder(o, mkt)
		if err != nil {
			log.Errorf("unable to encode order: %w", err)
			continue
		}
		msgBook = append(msgBook, msgOrder)
	}
	// This is a msgjson.OrderBook without the seq field.
	res := &struct {
		MarketID string                   `json:"marketid"`
		Epoch    uint64                   `json:"epoch"`
		Orders   []*msgjson.BookOrderNote `json:"orders"`
	}{
		MarketID: mkt,
		Epoch:    uint64(status.ActiveEpoch),
		Orders:   msgBook,
	}
	writeJSON(w, res)
}

// hander for route '/market/{marketName}/matches?includeinactive=BOOL' API
// request.
func (s *Server) apiMarketMatches(w http.ResponseWriter, r *http.Request) {
	var includeInactive bool
	if includeInactiveStr := r.URL.Query().Get(includeInactiveKey); includeInactiveStr != "" {
		var err error
		includeInactive, err = strconv.ParseBool(includeInactiveStr)
		if err != nil {
			http.Error(w, fmt.Sprintf("invalid include inactive boolean %q: %v", includeInactiveStr, err), http.StatusBadRequest)
			return
		}
	}
	mkt := strings.ToLower(chi.URLParam(r, marketNameKey))
	status := s.core.MarketStatus(mkt)
	if status == nil {
		http.Error(w, fmt.Sprintf("unknown market %q", mkt), http.StatusBadRequest)
		return
	}
	matches, err := s.core.MarketMatches(status.Base, status.Quote, includeInactive)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to obtain match data: %v", err), http.StatusInternalServerError)
		return
	}
	matchData := make([]*MatchData, len(matches))
	for i, match := range matches {
		matchData[i] = &MatchData{
			TakerSell:   match.TakerSell,
			ID:          match.ID.String(),
			Maker:       match.Maker.String(),
			MakerAcct:   match.MakerAcct.String(),
			MakerSwap:   match.MakerSwap,
			MakerRedeem: match.MakerRedeem,
			MakerAddr:   match.MakerAddr,
			Taker:       match.Taker.String(),
			TakerAcct:   match.TakerAcct.String(),
			TakerSwap:   match.TakerSwap,
			TakerRedeem: match.TakerRedeem,
			TakerAddr:   match.TakerAddr,
			EpochIdx:    match.Epoch.Idx,
			EpochDur:    match.Epoch.Dur,
			Quantity:    match.Quantity,
			Rate:        match.Rate,
			BaseRate:    match.BaseRate,
			QuoteRate:   match.QuoteRate,
			Active:      match.Active,
			Status:      match.Status.String(),
		}
	}
	writeJSON(w, matchData)
}

// hander for route '/market/{marketName}/resume?t=UNIXMS'
func (s *Server) apiResume(w http.ResponseWriter, r *http.Request) {
	// Ensure the market exists and is not running.
	mkt := strings.ToLower(chi.URLParam(r, marketNameKey))
	found, running := s.core.MarketRunning(mkt)
	if !found {
		http.Error(w, fmt.Sprintf("unknown market %q", mkt), http.StatusBadRequest)
		return
	}
	if running {
		http.Error(w, fmt.Sprintf("market %q running", mkt), http.StatusBadRequest)
		return
	}

	// Validate the resume time provided in the "t" query. If not specified,
	// the zero time.Time is used to indicate ASAP.
	var resTime time.Time
	if tResumeStr := r.URL.Query().Get("t"); tResumeStr != "" {
		resTimeMs, err := strconv.ParseInt(tResumeStr, 10, 64)
		if err != nil {
			http.Error(w, fmt.Sprintf("invalid resume time %q: %v", tResumeStr, err), http.StatusBadRequest)
			return
		}

		resTime = encode.UnixTimeMilli(resTimeMs)
		if time.Until(resTime) < 0 {
			http.Error(w, fmt.Sprintf("specified market resume time is in the past: %v", resTime),
				http.StatusBadRequest)
			return
		}
	}

	resEpoch, resTime, err := s.core.ResumeMarket(mkt, resTime)
	if resEpoch == 0 || err != nil {
		// Should not happen.
		msg := fmt.Sprintf("Failed to resume market: %v", err)
		log.Errorf(msg)
		http.Error(w, msg, http.StatusInternalServerError)
		return
	}

	writeJSON(w, &ResumeResult{
		Market:     mkt,
		StartEpoch: resEpoch,
		StartTime:  APITime{resTime},
	})
}

// hander for route '/market/{marketName}/suspend?t=UNIXMS&persist=BOOL'
func (s *Server) apiSuspend(w http.ResponseWriter, r *http.Request) {
	// Ensure the market exists and is running.
	mkt := strings.ToLower(chi.URLParam(r, marketNameKey))
	found, running := s.core.MarketRunning(mkt)
	if !found {
		http.Error(w, fmt.Sprintf("unknown market %q", mkt), http.StatusBadRequest)
		return
	}
	if !running {
		http.Error(w, fmt.Sprintf("market %q not running", mkt), http.StatusBadRequest)
		return
	}

	// Validate the suspend time provided in the "t" query. If not specified,
	// the zero time.Time is used to indicate ASAP.
	var suspTime time.Time
	if tSuspendStr := r.URL.Query().Get("t"); tSuspendStr != "" {
		suspTimeMs, err := strconv.ParseInt(tSuspendStr, 10, 64)
		if err != nil {
			http.Error(w, fmt.Sprintf("invalid suspend time %q: %v", tSuspendStr, err), http.StatusBadRequest)
			return
		}

		suspTime = encode.UnixTimeMilli(suspTimeMs)
		if time.Until(suspTime) < 0 {
			http.Error(w, fmt.Sprintf("specified market suspend time is in the past: %v", suspTime),
				http.StatusBadRequest)
			return
		}
	}

	// Validate the persist book flag provided in the "persist" query. If not
	// specified, persist the books, do not purge.
	persistBook := true
	if persistBookStr := r.URL.Query().Get("persist"); persistBookStr != "" {
		var err error
		persistBook, err = strconv.ParseBool(persistBookStr)
		if err != nil {
			http.Error(w, fmt.Sprintf("invalid persist book boolean %q: %v", persistBookStr, err), http.StatusBadRequest)
			return
		}
	}

	suspEpoch, err := s.core.SuspendMarket(mkt, suspTime, persistBook)
	if suspEpoch == nil || err != nil {
		// Should not happen.
		msg := fmt.Sprintf("Failed to suspend market: %v", err)
		log.Errorf(msg)
		http.Error(w, msg, http.StatusInternalServerError)
		return
	}

	writeJSON(w, &SuspendResult{
		Market:      mkt,
		FinalEpoch:  suspEpoch.Idx,
		SuspendTime: APITime{suspEpoch.End},
	})
}

// apiAccounts is the handler for the '/accounts' API request.
func (s *Server) apiAccounts(w http.ResponseWriter, _ *http.Request) {
	accts, err := s.core.Accounts()
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to retrieve accounts: %v", err), http.StatusInternalServerError)
		return
	}
	writeJSON(w, accts)
}

// apiEnableDataAPI is the handler for the `/enabledataapi/{yes}` API request,
// used to enable or disable the HTTP data API.
func (s *Server) apiEnableDataAPI(w http.ResponseWriter, r *http.Request) {
	yes, err := strconv.ParseBool(chi.URLParam(r, yesKey))
	if err != nil {
		http.Error(w, "unable to parse selection: "+err.Error(), http.StatusBadRequest)
		return
	}
	s.core.EnableDataAPI(yes)
	msg := "Data API disabled"
	if yes {
		msg = "Data API enabled"
	}
	writeJSON(w, msg)
}

// apiAccountInfo is the handler for the '/account/{account id}' API request.
func (s *Server) apiAccountInfo(w http.ResponseWriter, r *http.Request) {
	acctIDStr := chi.URLParam(r, accountIDKey)
	acctIDSlice, err := hex.DecodeString(acctIDStr)
	if err != nil {
		http.Error(w, fmt.Sprintf("could not decode accout id: %v", err), http.StatusBadRequest)
		return
	}
	if len(acctIDSlice) != account.HashSize {
		http.Error(w, "account id has incorrect length", http.StatusBadRequest)
		return
	}
	var acctID account.AccountID
	copy(acctID[:], acctIDSlice)
	acctInfo, err := s.core.AccountInfo(acctID)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to retrieve account: %v", err), http.StatusInternalServerError)
		return
	}
	writeJSON(w, acctInfo)
}

// decodeAcctID checks a string as being both hex and the right length and
// returns its bytes encoded as an account.AccountID.
func decodeAcctID(acctIDStr string) (account.AccountID, error) {
	var acctID account.AccountID
	if len(acctIDStr) != account.HashSize*2 {
		return acctID, errors.New("account id has incorrect length")
	}
	if _, err := hex.Decode(acctID[:], []byte(acctIDStr)); err != nil {
		return acctID, fmt.Errorf("could not decode accout id: %w", err)
	}
	return acctID, nil
}

// apiBan is the handler for the '/account/{accountID}/ban?rule=RULE' API request.
func (s *Server) apiBan(w http.ResponseWriter, r *http.Request) {
	acctIDStr := chi.URLParam(r, accountIDKey)
	acctID, err := decodeAcctID(acctIDStr)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	ruleStr := r.URL.Query().Get(ruleKey)
	if ruleStr == "" {
		http.Error(w, "rule not specified", http.StatusBadRequest)
		return
	}
	ruleInt, err := strconv.Atoi(ruleStr)
	if err != nil {
		http.Error(w, fmt.Sprintf("bad rule: %v", err), http.StatusBadRequest)
		return
	}
	if !account.Rule(ruleInt).Punishable() {
		msg := fmt.Sprintf("bad rule (%d): not known or not punishable", ruleInt)
		http.Error(w, msg, http.StatusBadRequest)
		return
	}
	// TODO: Allow operator supplied details to go with the ban.
	if err := s.core.Penalize(acctID, account.Rule(ruleInt), ""); err != nil {
		http.Error(w, fmt.Sprintf("failed to ban account: %v", err), http.StatusInternalServerError)
		return
	}
	res := BanResult{
		AccountID:  acctIDStr,
		BrokenRule: byte(ruleInt),
		BanTime:    APITime{time.Now()},
	}
	writeJSON(w, res)
}

// apiUnBan is the handler for the '/account/{accountID}/unban' API request.
func (s *Server) apiUnban(w http.ResponseWriter, r *http.Request) {
	acctIDStr := chi.URLParam(r, accountIDKey)
	acctID, err := decodeAcctID(acctIDStr)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.core.Unban(acctID); err != nil {
		http.Error(w, fmt.Sprintf("failed to unban account: %v", err), http.StatusInternalServerError)
		return
	}
	res := UnbanResult{
		AccountID: acctIDStr,
		UnbanTime: APITime{time.Now()},
	}
	writeJSON(w, res)
}

// apiForgiveMatchFail is the handler for the '/account/{accountID}/forgive_match/{matchID}' API request.
func (s *Server) apiForgiveMatchFail(w http.ResponseWriter, r *http.Request) {
	acctIDStr := chi.URLParam(r, accountIDKey)
	acctID, err := decodeAcctID(acctIDStr)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	matchIDStr := chi.URLParam(r, matchIDKey)
	matchID, err := order.DecodeMatchID(matchIDStr)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	forgiven, unbanned, err := s.core.ForgiveMatchFail(acctID, matchID)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to forgive failed match %v for account %v: %v", matchID, acctID, err), http.StatusInternalServerError)
		return
	}
	res := ForgiveResult{
		AccountID:   acctIDStr,
		Forgiven:    forgiven,
		Unbanned:    unbanned,
		ForgiveTime: APITime{time.Now()},
	}
	writeJSON(w, res)
}

func toNote(r *http.Request) (*msgjson.Message, int, error) {
	body, err := ioutil.ReadAll(r.Body)
	r.Body.Close()
	if err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("unable to read request body: %w", err)
	}
	if len(body) == 0 {
		return nil, http.StatusBadRequest, errors.New("no message to broadcast")
	}
	// Remove trailing newline if present. A newline is added by the curl
	// command when sending from file.
	if body[len(body)-1] == '\n' {
		body = body[:len(body)-1]
	}
	if len(body) > maxUInt16 {
		return nil, http.StatusBadRequest, fmt.Errorf("cannot send messages larger than %d bytes", maxUInt16)
	}
	msg, err := msgjson.NewNotification(msgjson.NotifyRoute, string(body))
	if err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("unable to create notification: %w", err)
	}
	return msg, 0, nil
}

// apiNotify is the handler for the '/account/{accountID}/notify' API request.
func (s *Server) apiNotify(w http.ResponseWriter, r *http.Request) {
	acctIDStr := chi.URLParam(r, accountIDKey)
	acctID, err := decodeAcctID(acctIDStr)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	msg, errCode, err := toNote(r)
	if err != nil {
		http.Error(w, err.Error(), errCode)
		return
	}
	s.core.Notify(acctID, msg)
	w.WriteHeader(http.StatusOK)
}

// apiNotifyAll is the handler for the '/notifyall' API request.
func (s *Server) apiNotifyAll(w http.ResponseWriter, r *http.Request) {
	msg, errCode, err := toNote(r)
	if err != nil {
		http.Error(w, err.Error(), errCode)
		return
	}
	s.core.NotifyAll(msg)
	w.WriteHeader(http.StatusOK)
}
