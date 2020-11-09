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
	}{
		{
			name:      "ipv4 unequal",
			addr1:     "127.0.0.1",
			addr2:     "127.0.0.2",
			wantEqual: false,
		},
		{
			name:      "ipv4 equal",
			addr1:     "127.0.0.1",
			addr2:     "127.0.0.1",
			wantEqual: true,
		},
		{
			name:      "ipv4 port unequal",
			addr1:     "127.0.0.1:1234",
			addr2:     "127.0.0.2:1234",
			wantEqual: false,
		},
		{
			name:      "ipv4 port equal",
			addr1:     "127.0.0.1:1234",
			addr2:     "127.0.0.1:1234",
			wantEqual: true,
		},
		{
			name:      "ipv4 port equal noport",
			addr1:     "127.0.0.1",
			addr2:     "127.0.0.1:1234",
			wantEqual: true,
		},
		{
			name:      "ipv6 port unequal",
			addr1:     "[a:b:c:d::]:1234",
			addr2:     "[a:b:c:e::]:1234",
			wantEqual: false,
		},
		{
			name:      "ipv6 port equal",
			addr1:     "[a:b:c:d::]:1234",
			addr2:     "[a:b:c:d::]:1234",
			wantEqual: true,
		},
		{
			name:      "ipv6 port equal noport",
			addr1:     "a:b:c:d::",
			addr2:     "[a:b:c:d::]:1234",
			wantEqual: true,
		},
	}

	zeroKey := IPKey{}

	for _, tt := range tests {
		ipKey1 := NewIPKey(tt.addr1)
		ipKey2 := NewIPKey(tt.addr2)

		if ipKey1 == zeroKey || ipKey2 == zeroKey {
			t.Fatalf("%s: zeroKey found. ipKey1 = %x, ipKey2 = %x", tt.name, ipKey1[:], ipKey2[:])
		}

		if (ipKey1 == ipKey2) != tt.wantEqual {
			t.Fatalf("%s: wantEqual = %t, addr1 = %s, addr2 = %s, ipKey1 = %x, ipKey2 = %x",
				tt.name, tt.wantEqual, tt.addr1, tt.addr2, ipKey1[:], ipKey2[:])
		}
	}
}
