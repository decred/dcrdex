//go:build live

package dexnet

import (
	"context"
	"net/http"
	"testing"
)

func TestGet(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	uri := "https://dcrdata.decred.org/api/block/best"
	var resp struct {
		Height int64 `json:"height"`
	}
	var code int
	if err := Get(ctx, uri, &resp, WithStatusFunc(func(c int) { code = c })); err != nil {
		t.Fatalf("Get error: %v", err)
	}
	if resp.Height == 0 {
		t.Fatal("Height not parsed")
	}
	if code != http.StatusOK {
		t.Fatalf("expected code 200, got %d", code)
	}
	// Check size limit
	if err := Get(ctx, uri, &resp, WithSizeLimit(1)); err == nil {
		t.Fatal("Didn't get parse error for low size limit")
	}

}
