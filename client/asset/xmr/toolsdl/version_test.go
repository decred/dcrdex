package toolsdl

import (
	"encoding/json"
	"testing"

	"decred.org/dcrdex/dex"
	"github.com/decred/slog"
)

// moneroVersionV0 tests

func TestToolsVersion(t *testing.T) {
	var toolsDir string
	var mv *moneroVersionV0
	toolsDir = "monero-freebsd-x64-v1.18.4.3"
	_, err := newMoneroVersionFromDir(toolsDir)
	if err == nil {
		t.Fatalf("bad or invalid tools version %s", toolsDir) // 'v1.' not accepted
	}
	toolsDir = "monero-win-x64-v0.18.4.3"
	mv, err = newMoneroVersionFromDir(toolsDir)
	if err != nil || !mv.valid() {
		t.Fatalf("bad or invalid tools version %s", toolsDir)
	}
	toolsDir = "monero-mac-x64-v0.18.4.3"
	mv, err = newMoneroVersionFromDir(toolsDir)
	if err != nil || !mv.valid() {
		t.Fatalf("bad or invalid tools version %s", toolsDir)
	}
	toolsDir = "monero-linux-x64-v0.18.4.4"
	mv, err = newMoneroVersionFromDir(toolsDir)
	if err != nil || !mv.valid() {
		t.Fatalf("bad or invalid tools version %s", toolsDir)
	}
	toolsDir = "monero-linux-armv8-v0.18.4.3"
	mv, err = newMoneroVersionFromDir(toolsDir)
	if err != nil || !mv.valid() {
		t.Fatalf("bad or invalid tools version %s", toolsDir)
	}
	toolsDir = "monero-freebsd-x64-v0.18.4.3"
	mv, err = newMoneroVersionFromDir(toolsDir)
	if err != nil || !mv.valid() {
		t.Fatalf("bad or invalid tools version %s", toolsDir)
	}
	toolsDir = "monero-freebsd-x64-v0.18.4.1" // less than v0.18.4.3
	mv, err = newMoneroVersionFromDir(toolsDir)
	if err != nil {
		t.Fatalf("bad or invalid tools version %s", toolsDir)
	}
	if mv.valid() {
		t.Fatalf("expected invalid tools version %s", toolsDir)
	}
	toolsDir = "freebsd-x64-v0.18.4.1" // no monero-*
	_, err = newMoneroVersionFromDir(toolsDir)
	if err == nil {
		t.Fatalf("expected bad tools version for dir %s", toolsDir)
	}
}

func TestAcceptableVersion(t *testing.T) {
	dataDir := t.TempDir()
	dl := NewDownload(dataDir, dex.StdOutLogger("Test", slog.LevelTrace))
	vset, err := getOtherAcceptableVersions(dl.getToolsBasePath())
	if err != nil || vset == nil {
		t.Fatal(err)
	}
	for _, v := range vset.Versions {
		s, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("%s\n", s)
		hzips := v.getHashedZips()
		t.Logf("%v \n", hzips)
		// should be in same order
		for i, az := range v.AcceptableZips {
			if az.Hash != hzips[i].hash || az.Zip != hzips[i].zip || az.Dir != hzips[i].dir || az.Os != hzips[i].os || az.Arch != hzips[i].arch ||
				az.Ext != hzips[i].ext {
				t.Fatal("acceptable zip and hashedZip do not have the same contents")
			}
			mv, err := newMoneroVersionFromDir(az.Dir)
			if err != nil {
				t.Fatalf("bad acceptableVersion monero version from Dir: %s", az.Dir)
			}
			if mv.notEqual(hzips[i].version) {
				t.Fatalf("acceptableVersion version %s is not the same as hashedZip version %s", az.Dir, hzips[i].version.string())
			}
		}
	}
}
