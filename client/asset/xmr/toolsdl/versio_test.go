package toolsdl

import "testing"

// moneroVersionV0 tests

func TestToolsVersion(t *testing.T) {
	var toolsDir = "monero-win-x64-v0.18.4.4"
	_, err := newMoneroVersionDir(toolsDir)
	if err != nil {
		t.Fatalf("bad tools version %s", toolsDir)
	}
	toolsDir = "monero-mac-x64-v0.18.4.1"
	_, err = newMoneroVersionDir(toolsDir)
	if err != nil {
		t.Fatalf("bad tools version %s", toolsDir)
	}
	toolsDir = "monero-linux-x64-v0.18.4.1"
	_, err = newMoneroVersionDir(toolsDir)
	if err != nil {
		t.Fatalf("bad tools version %s", toolsDir)
	}
	toolsDir = "monero-linux-armv8-v0.18.4.1"
	_, err = newMoneroVersionDir(toolsDir)
	if err != nil {
		t.Fatalf("bad tools version %s", toolsDir)
	}
	toolsDir = "monero-freebsd-x64-v0.18.4.1"
	_, err = newMoneroVersionDir(toolsDir)
	if err != nil {
		t.Fatalf("bad tools version %s", toolsDir)
	}
	toolsDir = "freebsd-x64-v0.18.4.1"
	_, err = newMoneroVersionDir(toolsDir)
	if err == nil {
		t.Fatalf("expected bad tools version for dir %s", toolsDir)
	}
}
