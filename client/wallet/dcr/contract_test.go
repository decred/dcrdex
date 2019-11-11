package dcr

import (
	"bytes"
	"testing"

	"github.com/decred/dcrd/chaincfg/v2"
	"github.com/decred/dcrd/dcrutil/v2"
)

func TestGenerateContracts(t *testing.T) {
	tests := []struct {
		redeemAddrs []string
		ctpAddrs    []string
		params      *chaincfg.Params
		wantErr     bool
	}{
		{
			redeemAddrs: []string{
				"DsaRVfWLBrGYpvUW6o8jDS3iSoHQQcx1wx2",
				"DsnHdhaBNtaahvjDPAgmsMCb9QH6LW69XsU",
			},
			ctpAddrs: []string{
				"DsmEiHKgE1PkovBCB6xkFGDLxdhhXQ31sgV",
				"DsYy9NLvAmhbpBPSDrfF87GJyZ9rSm9XXP3",
			},
			params:  chaincfg.MainNetParams(),
			wantErr: false,
		},
		{
			redeemAddrs: []string{
				"DsaRVfWLBrGYpvUW6o8jDS3iSoHQQcx1wx2",
				"DsnHdhaBNtaahvjDPAgmsMCb9QH6LW69XsU",
			},
			ctpAddrs: []string{
				"DsmEiHKgE1PkovBCB6xkFGDLxdhhXQ31sgV",
			},
			params:  chaincfg.MainNetParams(),
			wantErr: true,
		},
	}

	for idx, tc := range tests {
		contracts, err := generateContracts(tc.redeemAddrs, tc.ctpAddrs, 100, tc.params)
		if (err != nil) != tc.wantErr {
			t.Fatalf("[generateContracts] #%d: error: %v, wantErr: %v",
				idx+1, err, tc.wantErr)
		}

		if !tc.wantErr {
			for i, addr := range tc.redeemAddrs {
				redeemAddr, err := dcrutil.DecodeAddress(addr, tc.params)
				if err != nil {
					t.Fatalf("unexpected redeem address err %v", addr)
				}

				ctpAddr, err := dcrutil.DecodeAddress(tc.ctpAddrs[i], tc.params)
				if err != nil {
					t.Fatalf("unexpected counterparty address err %v", addr)
				}

				contract := contracts[i]
				if contract.RedeemAddr.String() != redeemAddr.String() {
					t.Fatalf("expected %s redeem address for contract", addr)
				}

				if contract.CounterPartyAddr.String() != ctpAddr.String() {
					t.Fatalf("expected %s counterparty address for contract", ctpAddr)
				}

				expectedContractData, err := atomicSwapContract(redeemAddr.Hash160(), ctpAddr.Hash160(),
					int64(contract.LockTime), sha256Hash(contract.Secret))
				if err != nil {
					t.Fatalf("unexpected contract error %v", err)
				}

				if !bytes.Equal(expectedContractData, contract.ContractData) {
					t.Fatalf("generated contract data (%x) is not equal to "+
						" expected contract data (%x)", expectedContractData,
						contract.ContractData)
				}
			}
		}
	}
}
