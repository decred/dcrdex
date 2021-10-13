//go:build !harness && lgpl
// +build !harness,lgpl

// These tests will not be run if the harness build tag is set.

package eth

import (
	"encoding/hex"
	"errors"
	"math/big"
	"testing"

	"decred.org/dcrdex/dex/encode"
	dexeth "decred.org/dcrdex/dex/networks/eth"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

func overMaxWei() *big.Int {
	maxInt := ^uint64(0)
	maxWei := new(big.Int).SetUint64(maxInt)
	gweiFactorBig := big.NewInt(GweiFactor)
	maxWei.Mul(maxWei, gweiFactorBig)
	overMaxWei := new(big.Int).Set(maxWei)
	return overMaxWei.Add(overMaxWei, gweiFactorBig)
}

func TestNewSwapCoin(t *testing.T) {
	receiverAddr, contractAddr, randomAddr := new(common.Address), new(common.Address), new(common.Address)
	copy(receiverAddr[:], encode.RandomBytes(20))
	copy(contractAddr[:], encode.RandomBytes(20))
	copy(randomAddr[:], encode.RandomBytes(20))
	secret, secretHash, txHash := [32]byte{}, [32]byte{}, [32]byte{}
	copy(txHash[:], encode.RandomBytes(32))
	copy(secret[:], secretSlice)
	copy(secretHash[:], secretHashSlice)
	tc := TxCoinID{
		TxID: txHash,
	}
	txCoinIDBytes := tc.Encode()
	sc := SwapCoinID{}
	swapCoinIDBytes := sc.Encode()
	gasPrice := big.NewInt(3e10)
	value := big.NewInt(5e18)
	wantGas, err := ToGwei(big.NewInt(3e10))
	if err != nil {
		t.Fatal(err)
	}
	wantVal, err := ToGwei(big.NewInt(5e18))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name          string
		coinID        []byte
		tx            *types.Transaction
		ct            swapCoinType
		swpErr, txErr error
		wantErr       bool
	}{{
		name:   "ok init",
		tx:     tTx(gasPrice, value, contractAddr, initCalldata),
		coinID: txCoinIDBytes,
		ct:     sctInit,
	}, {
		name:   "ok redeem",
		tx:     tTx(gasPrice, big.NewInt(0), contractAddr, redeemCalldata),
		coinID: txCoinIDBytes,
		ct:     sctRedeem,
	}, {
		name:    "unknown coin type",
		tx:      tTx(gasPrice, value, contractAddr, initCalldata),
		coinID:  txCoinIDBytes,
		ct:      swapCoinType(^uint8(0)),
		wantErr: true,
	}, {
		name:    "tx has no data",
		tx:      tTx(gasPrice, value, contractAddr, nil),
		coinID:  txCoinIDBytes,
		ct:      sctInit,
		wantErr: true,
	}, {
		name:    "non zero value with redeem",
		tx:      tTx(gasPrice, value, contractAddr, redeemCalldata),
		coinID:  txCoinIDBytes,
		ct:      sctRedeem,
		wantErr: true,
	}, {
		name:    "unable to decode init data, must be init for init coin type",
		tx:      tTx(gasPrice, value, contractAddr, redeemCalldata),
		coinID:  txCoinIDBytes,
		ct:      sctInit,
		wantErr: true,
	}, {
		name:    "unable to decode redeem data, must be redeem for redeem coin type",
		tx:      tTx(gasPrice, big.NewInt(0), contractAddr, initCalldata),
		coinID:  txCoinIDBytes,
		ct:      sctRedeem,
		wantErr: true,
	}, {
		name:    "unable to decode CoinID",
		tx:      tTx(gasPrice, value, contractAddr, initCalldata),
		ct:      sctInit,
		wantErr: true,
	}, {
		name:    "wrong type of coinID",
		tx:      tTx(gasPrice, value, contractAddr, initCalldata),
		coinID:  swapCoinIDBytes,
		ct:      sctInit,
		wantErr: true,
	}, {
		name:    "transaction error",
		tx:      tTx(gasPrice, value, contractAddr, initCalldata),
		coinID:  txCoinIDBytes,
		txErr:   errors.New(""),
		ct:      sctInit,
		wantErr: true,
	}, {
		name:    "wrong contract",
		tx:      tTx(gasPrice, value, randomAddr, initCalldata),
		coinID:  txCoinIDBytes,
		ct:      sctInit,
		wantErr: true,
	}, {
		name:    "value too big",
		tx:      tTx(gasPrice, overMaxWei(), contractAddr, initCalldata),
		coinID:  txCoinIDBytes,
		ct:      sctInit,
		wantErr: true,
	}, {
		name:    "gas too big",
		tx:      tTx(overMaxWei(), value, contractAddr, initCalldata),
		coinID:  txCoinIDBytes,
		ct:      sctInit,
		wantErr: true,
	}}
	for _, test := range tests {
		node := &testNode{
			tx:    test.tx,
			txErr: test.txErr,
		}
		eth := &Backend{
			node:         node,
			log:          tLogger,
			contractAddr: *contractAddr,
		}
		sc, err := newSwapCoin(eth, test.coinID, test.ct)
		if test.wantErr {
			if err == nil {
				t.Fatalf("expected error for test %q", test.name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("unexpected error for test %q: %v", test.name, err)
		}

		wv := wantVal
		var ws [32]byte
		lt := initLocktime
		sct := sctInit
		cp := initParticipantAddr
		if test.ct == sctRedeem {
			wv = 0
			ws = secret
			lt = 0
			sct = sctRedeem
			cp = common.Address{}
		}
		if sc.contractAddr != *contractAddr ||
			sc.counterParty != cp ||
			sc.secretHash != secretHash ||
			sc.secret != ws ||
			sc.value != wv ||
			sc.gasPrice != wantGas ||
			sc.locktime != lt ||
			sc.sct != sct ||
			sc.txid != hex.EncodeToString(txHash[:]) {
			t.Fatalf("returns do not match expected for test %q", test.name)
		}
	}
}

func TestConfirmations(t *testing.T) {
	contractAddr, nullAddr := new(common.Address), new(common.Address)
	copy(contractAddr[:], encode.RandomBytes(20))
	secretHash, txHash := [32]byte{}, [32]byte{}
	copy(txHash[:], encode.RandomBytes(32))
	copy(secretHash[:], secretHashSlice)
	tc := TxCoinID{
		TxID: txHash,
	}
	txCoinIDBytes := tc.Encode()
	gasPrice := big.NewInt(3e10)
	value := big.NewInt(5e18)
	locktime := big.NewInt(initLocktime)
	bigO := big.NewInt(0)
	oneGweiMore := big.NewInt(1e9)
	oneGweiMore.Add(oneGweiMore, value)
	tests := []struct {
		name           string
		swap           *dexeth.ETHSwapSwap
		bn             uint64
		value          *big.Int
		ct             swapCoinType
		wantConfs      int64
		swapErr, bnErr error
		wantErr, setCT bool
	}{{
		name:      "ok has confs value not verified",
		bn:        100,
		swap:      tSwap(97, locktime, value, SSInitiated, &initParticipantAddr),
		value:     value,
		ct:        sctInit,
		wantConfs: 3,
	}, {
		name:  "ok no confs",
		swap:  tSwap(0, bigO, bigO, SSNone, nullAddr),
		value: value,
		ct:    sctInit,
	}, {
		name:      "ok redeem swap status redeemed",
		swap:      tSwap(97, locktime, value, SSRedeemed, &initParticipantAddr),
		value:     bigO,
		ct:        sctRedeem,
		wantConfs: 1,
	}, {
		name:  "ok redeem swap status initiated",
		swap:  tSwap(97, locktime, value, SSInitiated, &initParticipantAddr),
		value: bigO,
		ct:    sctRedeem,
	}, {
		name:    "unknown coin type",
		ct:      sctInit,
		setCT:   true,
		wantErr: true,
	}, {
		name:    "redeem bad swap state None",
		swap:    tSwap(0, bigO, bigO, SSNone, nullAddr),
		value:   bigO,
		ct:      sctRedeem,
		wantErr: true,
	}, {
		name:    "error getting swap",
		swapErr: errors.New(""),
		value:   value,
		ct:      sctInit,
		wantErr: true,
	}, {
		name:    "swap value causes ToGwei error",
		swap:    tSwap(99, locktime, overMaxWei(), SSInitiated, &initParticipantAddr),
		value:   value,
		ct:      sctInit,
		wantErr: true,
	}, {
		name:    "value differs from initial transaction",
		swap:    tSwap(99, locktime, oneGweiMore, SSInitiated, &initParticipantAddr),
		value:   value,
		ct:      sctInit,
		wantErr: true,
	}, {
		name:    "participant differs from initial transaction",
		swap:    tSwap(99, locktime, value, SSInitiated, nullAddr),
		value:   value,
		ct:      sctInit,
		wantErr: true,
	}, {
		name:    "locktime not an int64",
		swap:    tSwap(99, new(big.Int).SetUint64(^uint64(0)), value, SSInitiated, &initParticipantAddr),
		value:   value,
		ct:      sctInit,
		wantErr: true,
	}, {
		name:    "locktime differs from initial transaction",
		swap:    tSwap(99, bigO, value, SSInitiated, &initParticipantAddr),
		value:   value,
		ct:      sctInit,
		wantErr: true,
	}, {
		name:    "block number error",
		swap:    tSwap(97, locktime, value, SSInitiated, &initParticipantAddr),
		value:   value,
		ct:      sctInit,
		bnErr:   errors.New(""),
		wantErr: true,
	}}
	for _, test := range tests {
		txdata := initCalldata
		if test.ct == sctRedeem {
			txdata = redeemCalldata
		}
		node := &testNode{
			tx:        tTx(gasPrice, test.value, contractAddr, txdata),
			swp:       test.swap,
			swpErr:    test.swapErr,
			blkNum:    test.bn,
			blkNumErr: test.bnErr,
		}
		eth := &Backend{
			node:         node,
			log:          tLogger,
			contractAddr: *contractAddr,
		}

		sc, err := newSwapCoin(eth, txCoinIDBytes, test.ct)
		if err != nil {
			t.Fatalf("unexpected error for test %q: %v", test.name, err)
		}

		_ = sc.String() // unrelated panic test

		if test.setCT {
			sc.sct = swapCoinType(^uint8(0))
		}

		confs, err := sc.Confirmations(nil)
		if test.wantErr {
			if err == nil {
				t.Fatalf("expected error for test %q", test.name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("unexpected error for test %q: %v", test.name, err)
		}
		if confs != test.wantConfs {
			t.Fatalf("want %d but got %d confs for test: %v", test.wantConfs, confs, test.name)
		}
	}
}
