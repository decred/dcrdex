// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package admin

import (
	"encoding/json"
	"net/http"
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
	writeJSON(w, "pong")
}

// apiConfig is the handler for the '/config' API request.
func (s *Server) apiConfig(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.core.ConfigMsg())
}
