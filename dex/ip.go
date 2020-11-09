// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dex

import (
	"encoding/hex"
	"fmt"
	"net"
)

// IPKey is a IP address byte array. For an IPV6 address, the IPKey drops the
// interface identifier, which is the second half of the address.
type IPKey [net.IPv6len / 2]byte

// NewIPKey parses an IP address string into an IPKey.
func NewIPKey(addr string) IPKey {
	host, _, err := net.SplitHostPort(addr)
	if err == nil && host != "" {
		addr = host
	}
	netIP := net.ParseIP(addr)
	fmt.Println("--", addr, ",", host, ",", hex.EncodeToString(netIP[:]))

	ip := netIP.To4()
	if ip == nil {
		ip = netIP.To16()
	}
	var ipKey IPKey
	copy(ipKey[:], ip)
	return ipKey
}
