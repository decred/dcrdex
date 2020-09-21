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

	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/server/account"
	"github.com/go-chi/chi"
)

const (
	pongStr        = "pong"
	maxUInt16      = int(^uint16(0))
	defaultTimeout = time.Hour * 72
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
	w.WriteHeader(code)
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "    ")
	if err := encoder.Encode(thing); err != nil {
		log.Errorf("JSON encode error: %v", err)
	}
}

// apiPing is the handler for the '/ping' API request.
func (_ *Server) apiPing(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, pongStr)
}

// apiConfig is the handler for the '/config' API request.
func (s *Server) apiConfig(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.core.ConfigMsg())
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

	mktStatus := &MarketStatus{
		Name:          mkt,
		Running:       status.Running,
		EpochDuration: status.EpochDuration,
		ActiveEpoch:   status.ActiveEpoch,
		StartEpoch:    status.ActiveEpoch,
		SuspendEpoch:  status.SuspendEpoch,
	}
	if status.SuspendEpoch != 0 {
		persist := status.PersistBook
		mktStatus.PersistBook = &persist
	}
	writeJSON(w, mktStatus)
}

// hander for route '/market/{marketName}/suspend?t=EPOCH-MS&persist=BOOL'
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

	suspEpoch := s.core.SuspendMarket(mkt, suspTime, persistBook)
	if suspEpoch == nil {
		// Should not happen.
		http.Error(w, "failed to suspend market "+mkt, http.StatusInternalServerError)
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

// apiAccountInfo is the handler for the '/account/{account id}?verbose=BOOL' API request.
func (s *Server) apiAccountInfo(w http.ResponseWriter, r *http.Request) {
	acctIDStr := chi.URLParam(r, accountIDKey)
	acctID, err := decodeAcctID(acctIDStr)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var verbose bool
	if verboseStr := r.URL.Query().Get(verboseToken); verboseStr != "" {
		verbose, err = strconv.ParseBool(verboseStr)
		if err != nil {
			http.Error(w, fmt.Sprintf("invalid verbose boolean %q: %v", verboseStr, err), http.StatusBadRequest)
			return
		}
	}
	dbAcctInfo, err := s.core.AccountInfo(acctID)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to retrieve account: %v", err), http.StatusInternalServerError)
		return
	}
	// If verbose, expired and forgiven penalties will be included.
	dbPenalties, _, err := s.core.Penalties(acctID, 0, verbose)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to retrieve penalties: %v", err), http.StatusInternalServerError)
		return
	}
	acctInfo := &AccountInfoResult{
		Account:   dbAcctInfo,
		Penalties: dbPenalties,
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
		return acctID, fmt.Errorf("could not decode accout id: %v", err)
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
	ruleStr := r.URL.Query().Get(ruleToken)
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
	res, err := s.core.Penalize(acctID, account.Rule(ruleInt), "")
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to ban account: %v", err), http.StatusInternalServerError)
		return
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

func toNote(r *http.Request) (*msgjson.Message, int, error) {
	body, err := ioutil.ReadAll(r.Body)
	r.Body.Close()
	if err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("unable to read request body: %v", err)
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
		return nil, http.StatusInternalServerError, fmt.Errorf("unable to create notification: %v", err)
	}
	return msg, 0, nil
}

// apiNotify is the handler for the '/account/{accountID}/notify?timeout=TIMEOUT'
// API request.
func (s *Server) apiNotify(w http.ResponseWriter, r *http.Request) {
	acctIDStr := chi.URLParam(r, accountIDKey)
	acctID, err := decodeAcctID(acctIDStr)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	timeout := defaultTimeout
	if timeoutStr := r.URL.Query().Get(timeoutToken); timeoutStr != "" {
		var err error
		timeout, err = time.ParseDuration(timeoutStr)
		if err != nil {
			http.Error(w, fmt.Sprintf("invalid timeout %q: %v", timeoutStr, err), http.StatusBadRequest)
			return
		}
	}
	msg, errCode, err := toNote(r)
	if err != nil {
		http.Error(w, err.Error(), errCode)
		return
	}
	s.core.Notify(acctID, msg, timeout)
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
