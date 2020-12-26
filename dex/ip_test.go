// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dex

import (
	"testing"
)

func TestIPKey(t *testing.T) {
	if NewIPKey("127.0.0.1") == NewIPKey("127.0.0.2") {
		t.Fatalf("IPKey v4 failed basic comparison test, %x = %x", NewIPKey("127.0.0.1"), NewIPKey("127.0.0.2"))
	}
	if NewIPKey("[a:b:c:d::]:1234") == NewIPKey("[a:b:c:e::]:1234") {
		t.Fatalf("IPKey v6 failed basic comparison test, %x = %x", NewIPKey("[a:b:c:d]"), NewIPKey("[a:b:c:d]"))
	}

	tests := []struct {
		name, addr1, addr2 string
		wantEqual          bool
		wantString1        string
	}{
		{
			name:        "bad",
			addr1:       "",
			addr2:       "xxx",
			wantEqual:   true,
			wantString1: "::",
		},
		{
			name:        "ipv4 unequal",
			addr1:       "127.0.0.1",
			addr2:       "127.0.0.2",
			wantEqual:   false,
			wantString1: "127.0.0.1",
		},
		{
			name:        "ipv4 equal",
			addr1:       "127.0.0.1",
			addr2:       "127.0.0.1",
			wantEqual:   true,
			wantString1: "127.0.0.1",
		},
		{
			name:        "ipv4 port unequal",
			addr1:       "127.0.0.1:1234",
			addr2:       "127.0.0.2:1234",
			wantEqual:   false,
			wantString1: "127.0.0.1",
		},
		{
			name:        "ipv4 port equal",
			addr1:       "127.0.0.1:1234",
			addr2:       "127.0.0.1:1234",
			wantEqual:   true,
			wantString1: "127.0.0.1",
		},
		{
			name:        "ipv4 port equal noport",
			addr1:       "127.0.0.1",
			addr2:       "127.0.0.1:1234",
			wantEqual:   true,
			wantString1: "127.0.0.1",
		},
		{
			name:        "ipv6 loopback with and without port",
			addr1:       "[::1]", // brackets to strip
			addr2:       "[::1]:13245",
			wantEqual:   true,
			wantString1: "::1", // mask exception for ipv6 loopback
		},
		{
			name:        "ipv6 port unequal",
			addr1:       "[a:b:c:d::]:1234",
			addr2:       "[a:b:c:e::]:1234",
			wantEqual:   false,
			wantString1: "a:b:c:d::",
		},
		{
			name:        "ipv6 port equal",
			addr1:       "[a:b:c:d::]:1234",
			addr2:       "[a:b:c:d::]:1234",
			wantEqual:   true,
			wantString1: "a:b:c:d::",
		},
		{
			name:        "ipv6 port equal noport",
			addr1:       "a:b:c:d::",
			addr2:       "[a:b:c:d::]:1234",
			wantEqual:   true,
			wantString1: "a:b:c:d::",
		},
		{
			name:        "ipv6 port equal noport with brackets",
			addr1:       "[a:b:c:d::]", // brackets to strip
			addr2:       "[a:b:c:d::]:1234",
			wantEqual:   true,
			wantString1: "a:b:c:d::",
		},
		{
			name:        "ipv6 mask equal",
			addr1:       "[a:b:c:d:e:f:a:b]:1234",
			addr2:       "[a:b:c:d:f:d:c:f]:1234",
			wantEqual:   true,
			wantString1: "a:b:c:d::",
		},
	}

	for _, tt := range tests {
		ipKey1 := NewIPKey(tt.addr1)
		ipKey2 := NewIPKey(tt.addr2)

		if (ipKey1 == ipKey2) != tt.wantEqual {
			t.Fatalf("%s: wantEqual = %t, addr1 = %s, addr2 = %s, ipKey1 = %x, ipKey2 = %x",
				tt.name, tt.wantEqual, tt.addr1, tt.addr2, ipKey1[:], ipKey2[:])
		}
		if tt.wantString1 != ipKey1.String() {
			t.Errorf("expected ipKey1 string %s, got %s", tt.wantString1, ipKey1.String())
		}
	}
}
