package xmr

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	// monero README
	MinimumVersion     = "0.18.4.0"
	RecommendedVersion = "0.18.4.2"
)

var minimumVersion *moneroVersion
var recommendedVersion *moneroVersion

func init() {
	minimumVersion, _ = newMoneroVersionFromVersionString(MinimumVersion)
	recommendedVersion, _ = newMoneroVersionFromVersionString(RecommendedVersion)
}

type moneroVersion struct {
	sys   uint64
	major uint64
	minor uint64
	patch uint64
}

// converts from "sys.major.minor.patch"
func newMoneroVersionFromVersionString(ver string) (*moneroVersion, error) {
	verSplit := strings.Split(ver, ".")
	if len(verSplit) != 4 {
		return nil, fmt.Errorf("invalid version %s", ver)
	}
	v0, err := strconv.ParseUint(verSplit[0], 10, 8)
	if err != nil {
		return nil, err
	}
	v1, err := strconv.ParseUint(verSplit[1], 10, 8)
	if err != nil {
		return nil, err
	}
	v2, err := strconv.ParseUint(verSplit[2], 10, 8)
	if err != nil {
		return nil, err
	}
	v3, err := strconv.ParseUint(verSplit[3], 10, 8)
	if err != nil {
		return nil, err
	}
	m := &moneroVersion{
		sys:   v0,
		major: v1,
		minor: v2,
		patch: v3,
	}
	return m, nil
}

func (m *moneroVersion) string() string {
	sep := "."
	sys := strconv.FormatUint(m.sys, 10)
	maj := strconv.FormatUint(m.sys, 10)
	min := strconv.FormatUint(m.sys, 10)
	patch := strconv.FormatUint(m.sys, 10)
	return sys + sep + maj + sep + min + sep + patch
}

func (m *moneroVersion) uint64() uint64 {
	var u64 uint64
	u64 |= m.sys << 24
	u64 |= m.major << 16
	u64 |= m.minor << 8
	u64 |= m.patch
	return u64
}

func (m *moneroVersion) compare(other *moneroVersion) int {
	thisUint64 := m.uint64()
	otherUint64 := other.uint64()
	if thisUint64 > otherUint64 {
		return 1
	} else if thisUint64 < otherUint64 {
		return -1
	}
	return 0
}

func (m *moneroVersion) valid() bool {
	c := m.compare(minimumVersion)
	if c < 0 {
		return false
	}
	c = m.compare(recommendedVersion)
	return c <= 0
}
