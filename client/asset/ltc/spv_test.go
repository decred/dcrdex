package ltc

import (
	"testing"

	dexltc "decred.org/dcrdex/dex/networks/ltc"
	ltcchaincfg "github.com/ltcsuite/ltcd/chaincfg"
	"github.com/ltcsuite/ltcd/ltcutil"
)

func TestAddressTranslationRoundTrip(t *testing.T) {
	w := ltcSPVWallet{
		chainParams: &ltcchaincfg.MainNetParams,
		btcParams:   dexltc.MainNetParams,
	}
	tests := []struct {
		name string
		addr string // ltcutil.Address
	}{
		{
			"bech32",
			"ltc1qx9ry0xnsz9spzw0vy7p9szyycmtk4a4xkessy5",
		},
		{
			"p2pkh",
			"LXyPtJexNLCdk99vYhgqrB2hXAdq6PQx8r",
		},
		{
			"p2sh",
			"MWidTW5JsYaxPeDyQKF3525D8PJZLVb5Ho",
		},
		{
			"32-byte segwit program (taproot)",
			"ltc1p7v5t2ltshynyrjj9ft5x5nulf76r8ml23789pwr29vu3rksnue3q2y7w9a",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ltcAddr, err := ltcutil.DecodeAddress(tt.addr, w.chainParams)
			if err != nil {
				t.Fatal(err)
			}
			btcAddr, err := w.addrLTC2BTC(ltcAddr)
			if err != nil {
				t.Errorf("ltcSPVWallet.addrLTC2BTC() error = %v", err)
				return
			}
			if btcAddr.String() != tt.addr {
				t.Errorf("ltcSPVWallet.addrLTC2BTC() = %v, want %v", btcAddr, tt.addr)
			}
			reLtcAddr, err := w.addrBTC2LTC(btcAddr)
			if err != nil {
				t.Errorf("ltcSPVWallet.addrBTC2LTC() error = %v", err)
			}
			if reLtcAddr.String() != tt.addr {
				t.Errorf("ltcSPVWallet.addrBTC2LTC() = %v, want %v", reLtcAddr, tt.addr)
			}
		})
	}
}

// func TestFormatAddrIPs(t *testing.T) {
//  addrs := []string{}
// 	for _, addr := range addrs {
// 		addrPort := netip.MustParseAddrPort(addr)
// 		b, _ := addrPort.MarshalBinary()
// 		fmt.Printf("{0x%x, 0x%x, 0x%x, 0x%x, 0x%x, 0x%x}, // %s \n", b[0], b[1], b[2], b[3], b[4], b[5], addr)
// 	}
// 	t.Fatal() // So it'll show in my VS Code output
// }
