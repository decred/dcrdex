// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dex

import (
	"net"
	"strings"
)

// IPKey is a IP address byte array.
type IPKey [net.IPv6len]byte

// NewIPKey parses an IP address string into an IPKey. For an IPV6 address, this
// drops the interface identifier, which is the second half of the address. The
// first half of the address comprises 48-bits for the prefix, which is the
// public topology of a network that is assigned by an ISP, and 16-bits for the
// Site-Level Aggregation Identifier (SLA). However, any number of the SLA bits
// may be assigned by a provider, potentially giving a single link the freedom
// to use multiple subnets, thus expanding the set of addresses they may use
// freely. As such, IPv6 addresses with just the interface bits masked may not
// be sufficient to identify a single remote uplink.
func NewIPKey(addr string) IPKey {
	host, _, err := net.SplitHostPort(addr)
	if err == nil && host != "" {
		addr = host
	} else {
		// If SplitHostPort failed, IPv6 addresses may still have brackets.
		addr = strings.Trim(addr, "[]")
	}

	ip := net.ParseIP(addr)
	if ip == nil {
		return IPKey{} // i.e. net.IPv6unspecified
	}
	// IPv4 is encoded in a net.IP of length IPv6len as
	// 00:00:00:00:00:00:00:00:00:00:ff:ff:xx:xx:xx:xx
	// Thus, must copy all 16 bytes for IPv4.
	N := net.IPv6len
	if ip.To4() == nil && !ip.Equal(net.IPv6loopback) {
		// Drop the last 64 bits (interface) of non-loopback IPv6 addresses.
		N = net.IPv6len / 2 // i.e. ip = ip.Mask(net.CIDRMask(64, 128))
	}
	var ipKey IPKey
	copy(ipKey[:], ip[:N])
	return ipKey
}

// String returns a readable IP address representation of the IPKey. This is
// done by copying the bytes of the IPKey array, and invoking the
// net.(IP).String method. As such it is inefficient and should not be invoked
// repeatedly or in hot paths.
func (ipk IPKey) String() string {
	return net.IP(ipk[:]).String()
}

// PrefixV6 returns the first 48-bits of an IPv6 address, or nil for an IPv4
// address. This may be used to identify multiple remote hosts from the same
// public topology but different subnets. A heuristic may be defined to treat
// hosts with the same prefix as the same host if there are many such hosts.
func (ipk IPKey) PrefixV6() *IPKey {
	if net.IP(ipk[:]).To4() != nil {
		return nil
	}
	var prefix IPKey
	copy(prefix[:], ipk[:6]) // 48 bit prefix
	return &prefix
}

// IsLoopback reports whether the IPKey represents a loopback address.
func (ipk IPKey) IsLoopback() bool {
	return net.IP(ipk[:]).IsLoopback()
}

// IsUnspecified reports whether the IPKey is zero (unspecified).
func (ipk IPKey) IsUnspecified() bool {
	return ipk == IPKey{} // net.IP(ipk[:]).Equal(net.IPv6unspecified)
}
