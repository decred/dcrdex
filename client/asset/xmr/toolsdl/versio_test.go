package toolsdl

import (
	"testing"
)

// moneroVersionV0 tests

func TestToolsVersion(t *testing.T) {
	var toolsDir string
	var mv *moneroVersionV0
	toolsDir = "monero-freebsd-x64-v1.18.4.3"
	_, err := newMoneroVersionDir(toolsDir)
	if err == nil {
		t.Fatalf("bad or invalid tools version %s", toolsDir) // 'v1.' not accepted
	}
	toolsDir = "monero-win-x64-v0.18.4.3"
	mv, err = newMoneroVersionDir(toolsDir)
	if err != nil || !mv.valid() {
		t.Fatalf("bad or invalid tools version %s", toolsDir)
	}
	toolsDir = "monero-mac-x64-v0.18.4.3"
	mv, err = newMoneroVersionDir(toolsDir)
	if err != nil || !mv.valid() {
		t.Fatalf("bad or invalid tools version %s", toolsDir)
	}
	toolsDir = "monero-linux-x64-v0.18.4.4"
	mv, err = newMoneroVersionDir(toolsDir)
	if err != nil || !mv.valid() {
		t.Fatalf("bad or invalid tools version %s", toolsDir)
	}
	toolsDir = "monero-linux-armv8-v0.18.4.3"
	mv, err = newMoneroVersionDir(toolsDir)
	if err != nil || !mv.valid() {
		t.Fatalf("bad or invalid tools version %s", toolsDir)
	}
	toolsDir = "monero-freebsd-x64-v0.18.4.3"
	mv, err = newMoneroVersionDir(toolsDir)
	if err != nil || !mv.valid() {
		t.Fatalf("bad or invalid tools version %s", toolsDir)
	}
	toolsDir = "monero-freebsd-x64-v0.18.4.1" // less than v0.18.4.3
	mv, err = newMoneroVersionDir(toolsDir)
	if err != nil {
		t.Fatalf("bad or invalid tools version %s", toolsDir)
	}
	if mv.valid() {
		t.Fatalf("expected invalid tools version %s", toolsDir)
	}
	toolsDir = "freebsd-x64-v0.18.4.1" // no monero-*
	_, err = newMoneroVersionDir(toolsDir)
	if err == nil {
		t.Fatalf("expected bad tools version for dir %s", toolsDir)
	}
}
