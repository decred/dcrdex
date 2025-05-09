package mexctypes

import "fmt"

// Define common structures used across different MEXC API responses.

// ErrorResponse represents the standard error format.
type ErrorResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

func (e *ErrorResponse) Error() string {
	if e == nil {
		return "<nil mexctypes.ErrorResponse>"
	}
	return fmt.Sprintf("MEXC API Error: code=%d, msg=%q", e.Code, e.Msg)
}

// PingResponse structure for GET /api/v3/ping.
type PingResponse struct{} // Success is indicated by HTTP 200

// TimeResponse structure for GET /api/v3/time.
type TimeResponse struct {
	ServerTime int64 `json:"serverTime"`
}
