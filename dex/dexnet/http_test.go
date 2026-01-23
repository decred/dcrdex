package dexnet

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestErrorParsing(t *testing.T) {
	ctx := t.Context()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"code": -150, "msg": "you messed up, bruh"}`, http.StatusBadRequest)
	}))
	defer ts.Close()

	var errPayload struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := Get(ctx, ts.URL, nil, WithErrorParsing(&errPayload)); err == nil {
		t.Fatal("didn't get an http error")
	}
	if errPayload.Code != -150 || errPayload.Msg != "you messed up, bruh" {
		t.Fatal("unexpected error body")
	}
}
