package toolsdl

import (
	"fmt"
	"strconv"
	"strings"
)

var minimumVersion *moneroVersionV0

type moneroVersionV0 struct {
	sys   string
	major uint64
	minor uint64
	patch uint64
}

// converts from "major", "minor", "patch". Errors if sys has been updated past 'v0'
func newMoneroVersionFromParts(sys, maj, min, patch string) (*moneroVersionV0, error) {
	if sys != "v0" {
		// Too much change - they did not change 'v0.' for a decade or more.
		// So this change needs investigating further
		return nil, fmt.Errorf("system version has changed from 'v0' to '%s'", sys)
	}
	v1, err := strconv.ParseUint(maj, 10, 8)
	if err != nil {
		return nil, err
	}
	v2, err := strconv.ParseUint(min, 10, 8)
	if err != nil {
		return nil, err
	}
	v3, err := strconv.ParseUint(patch, 10, 8)
	if err != nil {
		return nil, err
	}
	m := &moneroVersionV0{
		sys:   "v0",
		major: v1,
		minor: v2,
		patch: v3,
	}
	return m, nil
}

func moneroVersionV0Zero() *moneroVersionV0 {
	return &moneroVersionV0{
		sys:   "v0",
		major: 0,
		minor: 0,
		patch: 0,
	}
}

func newMoneroVersionFromDir(moneroToolsDir string) (*moneroVersionV0, error) {
	tkns := strings.Split(moneroToolsDir, Dash)
	lenTkns := len(tkns)
	if lenTkns < 3 || lenTkns > 4 {
		return nil, fmt.Errorf("incorrect number of tokens: %d expected 3 or 4", lenTkns)
	}
	if tkns[0] != "monero" {
		return nil, fmt.Errorf("expected dir to start with 'monero', got %s", tkns[0])
	}

	lastTkn := tkns[lenTkns-1]
	parts := strings.SplitN(lastTkn, Dot, 4)
	lenParts := len(parts)
	if lenParts < 4 {
		return nil, fmt.Errorf("incorrect number of parts: %d expected at least 4", lenParts)
	}
	return newMoneroVersionFromParts(parts[0], parts[1], parts[2], parts[3])
}

func (m *moneroVersionV0) string() string {
	sep := Dot
	sys := "v0"
	maj := strconv.FormatUint(m.major, 10)
	min := strconv.FormatUint(m.minor, 10)
	patch := strconv.FormatUint(m.patch, 10)
	return sys + sep + maj + sep + min + sep + patch
}

func (m *moneroVersionV0) compare(other *moneroVersionV0) int {
	if m.major > other.major {
		return +1
	}
	if m.major < other.major {
		return -1
	}
	// majors equal
	if m.minor > other.minor {
		return +1
	}
	if m.minor < other.minor {
		return -1
	}
	// minors also equal
	if m.patch > other.patch {
		return +1
	}
	if m.patch < other.patch {
		return -1
	}
	// all equal
	return 0
}

func (m *moneroVersionV0) equal(other *moneroVersionV0) bool {
	return m.compare(other) == 0
}

func (m *moneroVersionV0) notEqual(other *moneroVersionV0) bool {
	return m.compare(other) != 0
}

func (m *moneroVersionV0) greaterThan(other *moneroVersionV0) bool {
	return m.compare(other) > 0
}

func (m *moneroVersionV0) greaterOrEqual(other *moneroVersionV0) bool {
	return m.compare(other) >= 0
}

func (m *moneroVersionV0) lessThan(other *moneroVersionV0) bool {
	return m.compare(other) < 0
}

func (m *moneroVersionV0) lessOrEqual(other *moneroVersionV0) bool {
	return m.compare(other) <= 0
}

// This changes when v0.18.x.x changes to v0.19.x.x which will invalidate v0.19.x.x
// Consider making a policy object that can be updated via json file.
func (m *moneroVersionV0) majorsEqual(other *moneroVersionV0) bool {
	if m.major > other.major {
		return false
	}
	if m.major < other.major {
		return false
	}
	return true
}

// valid returns true if m >= minimumVersion and major version v0.18.x.x
func (m *moneroVersionV0) valid() bool {
	c := m.compare(minimumVersion)
	return c >= 0 && m.majorsEqual(minimumVersion)
}
