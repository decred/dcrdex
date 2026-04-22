package xmr

import (
	"context"
	"testing"
	"time"
)

// TestNewAuditorValidation covers the construction-time input checks.
func TestNewAuditorValidation(t *testing.T) {
	if _, err := NewAuditor(nil); err == nil {
		t.Fatal("nil config should error")
	}
	if _, err := NewAuditor(&Config{}); err == nil {
		t.Fatal("empty RPCAddress should error")
	}
	a, err := NewAuditor(&Config{
		RPCAddress: "http://127.0.0.1:18083/json_rpc",
		MinConf:    10,
	})
	if err != nil {
		t.Fatalf("valid config: %v", err)
	}
	// Default poll interval applied.
	if a.pollInterval != 30*time.Second {
		t.Errorf("pollInterval=%s want 30s", a.pollInterval)
	}
	if a.minConf != 10 {
		t.Errorf("minConf=%d want 10", a.minConf)
	}
}

// TestAuditorContextCancel ensures WaitOutputAtAddress returns
// promptly when the context is cancelled before the underlying RPC
// can respond. Uses an unreachable RPC address; the first dial will
// hang until ctx fires.
func TestAuditorContextCancel(t *testing.T) {
	a, err := NewAuditor(&Config{
		// Definitively unbound port to force an immediate
		// connection error or a hang.
		RPCAddress:   "http://127.0.0.1:1/json_rpc",
		PollInterval: 100 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	err = a.WaitOutputAtAddress(ctx, "swap1", "addr", "viewhex", 0, 1000, 0)
	if err == nil {
		t.Fatal("expected error from unreachable RPC or context timeout")
	}
}
