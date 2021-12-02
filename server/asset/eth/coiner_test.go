//go:build !harness && lgpl
// +build !harness,lgpl

// These tests will not be run if the harness build tag is set.

package eth

import (
	"errors"
	"fmt"
	"math/big"
	"testing"

	"decred.org/dcrdex/dex/encode"
	dexeth "decred.org/dcrdex/dex/networks/eth"
	swapv0 "decred.org/dcrdex/dex/networks/eth/contracts/v0"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

func overMaxWei() *big.Int {
	maxInt := ^uint64(0)
	maxWei := new(big.Int).SetUint64(maxInt)
	gweiFactorBig := big.NewInt(dexeth.GweiFactor)
	maxWei.Mul(maxWei, gweiFactorBig)
	overMaxWei := new(big.Int).Set(maxWei)
	return overMaxWei.Add(overMaxWei, gweiFactorBig)
}

func randomAddress() *common.Address {
	var addr common.Address
	copy(addr[:], encode.RandomBytes(20))
	return &addr
}

func TestNewSwapCoin(t *testing.T) {
	contractAddr, randomAddr := randomAddress(), randomAddress()
	var secret, secretHash, txHash [32]byte
	copy(txHash[:], encode.RandomBytes(32))
	txid := fmt.Sprintf("%#x", txHash[:]) // 0xababab...
	copy(secret[:], redeemSecretB)
	copy(secretHash[:], redeemSecretHashB)
	txCoinIDBytes := txHash[:]
	badCoinIDBytes := encode.RandomBytes(39)
	gasPrice := big.NewInt(3e10)
	value := big.NewInt(5e18)
	wantGas, err := dexeth.ToGwei(big.NewInt(3e10))
	if err != nil {
		t.Fatal(err)
	}
	wantVal, err := dexeth.ToGwei(big.NewInt(5e18))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name          string
		coinID        []byte
		contract      []byte
		tx            *types.Transaction
		ct            swapCoinType
		swpErr, txErr error
		wantErr       bool
	}{{
		name:     "ok init",
		tx:       tTx(gasPrice, value, contractAddr, initCalldata),
		coinID:   txCoinIDBytes,
		contract: initSecretHashA,
		ct:       sctInit,
	}, {
		name:     "ok redeem",
		tx:       tTx(gasPrice, new(big.Int), contractAddr, redeemCalldata),
		coinID:   txCoinIDBytes,
		contract: redeemSecretHashB,
		ct:       sctRedeem,
	}, {
		name:     "contract incorrect length",
		tx:       tTx(gasPrice, value, contractAddr, initCalldata),
		coinID:   txCoinIDBytes,
		contract: initSecretHashA[:31],
		ct:       sctInit,
		wantErr:  true,
	}, {
		name:     "unknown coin type",
		tx:       tTx(gasPrice, value, contractAddr, initCalldata),
		coinID:   txCoinIDBytes,
		contract: initSecretHashA,
		ct:       swapCoinType(^uint8(0)),
		wantErr:  true,
	}, {
		name:     "tx has no data",
		tx:       tTx(gasPrice, value, contractAddr, nil),
		coinID:   txCoinIDBytes,
		contract: initSecretHashA,
		ct:       sctInit,
		wantErr:  true,
	}, {
		name:     "non zero value with redeem",
		tx:       tTx(gasPrice, value, contractAddr, redeemCalldata),
		coinID:   txCoinIDBytes,
		contract: redeemSecretHashB,
		ct:       sctRedeem,
		wantErr:  true,
	}, {
		name:     "unable to decode init data, must be init for init coin type",
		tx:       tTx(gasPrice, value, contractAddr, redeemCalldata),
		coinID:   txCoinIDBytes,
		contract: initSecretHashA,
		ct:       sctInit,
		wantErr:  true,
	}, {
		name:     "unable to decode redeem data, must be redeem for redeem coin type",
		tx:       tTx(gasPrice, new(big.Int), contractAddr, initCalldata),
		coinID:   txCoinIDBytes,
		contract: redeemSecretHashB,
		ct:       sctRedeem,
		wantErr:  true,
	}, {
		name:     "unable to decode CoinID",
		tx:       tTx(gasPrice, value, contractAddr, initCalldata),
		ct:       sctInit,
		contract: initSecretHashA,
		wantErr:  true,
	}, {
		name:     "invalid coinID",
		tx:       tTx(gasPrice, value, contractAddr, initCalldata),
		coinID:   badCoinIDBytes,
		contract: initSecretHashA,
		ct:       sctInit,
		wantErr:  true,
	}, {
		name:     "transaction error",
		tx:       tTx(gasPrice, value, contractAddr, initCalldata),
		coinID:   txCoinIDBytes,
		contract: initSecretHashA,
		txErr:    errors.New(""),
		ct:       sctInit,
		wantErr:  true,
	}, {
		name:     "transaction not found error",
		tx:       tTx(gasPrice, value, contractAddr, initCalldata),
		coinID:   txCoinIDBytes,
		contract: initSecretHashA,
		txErr:    ethereum.NotFound,
		ct:       sctInit,
		wantErr:  true,
	}, {
		name:     "wrong contract",
		tx:       tTx(gasPrice, value, randomAddr, initCalldata),
		coinID:   txCoinIDBytes,
		contract: initSecretHashA,
		ct:       sctInit,
		wantErr:  true,
	}, {
		name:     "value too big",
		tx:       tTx(gasPrice, overMaxWei(), contractAddr, initCalldata),
		coinID:   txCoinIDBytes,
		contract: initSecretHashA,
		ct:       sctInit,
		wantErr:  true,
	}, {
		name:     "gas too big",
		tx:       tTx(overMaxWei(), value, contractAddr, initCalldata),
		coinID:   txCoinIDBytes,
		contract: initSecretHashA,
		ct:       sctInit,
		wantErr:  true,
	}, {
		name:     "tx coin id for swap - contract not in tx",
		tx:       tTx(gasPrice, value, contractAddr, initCalldata),
		coinID:   txCoinIDBytes,
		contract: encode.RandomBytes(32),
		ct:       sctInit,
		wantErr:  true,
	}, {
		name:     "tx coin id for redeem - contract not in tx",
		tx:       tTx(gasPrice, value, contractAddr, redeemCalldata),
		coinID:   txCoinIDBytes,
		contract: encode.RandomBytes(32),
		ct:       sctRedeem,
		wantErr:  true,
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
			initTxSize:   uint32(dexeth.InitGas(1, 0)),
		}
		sc, err := eth.newSwapCoin(test.coinID, test.contract, test.ct)
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
			sc.txid != txid {
			t.Fatalf("returns do not match expected for test %q / %v", test.name, sc)
		}
	}
}

func TestConfirmations(t *testing.T) {
	contractAddr, nullAddr := new(common.Address), new(common.Address)
	copy(contractAddr[:], encode.RandomBytes(20))
	secretHash, txHash := [32]byte{}, [32]byte{}
	copy(txHash[:], encode.RandomBytes(32))
	copy(secretHash[:], redeemSecretHashB)
	gasPrice := big.NewInt(3e10)
	value := big.NewInt(5e18)
	locktime := big.NewInt(initLocktime)
	bigO := new(big.Int)
	oneGweiMore := big.NewInt(1e9)
	oneGweiMore.Add(oneGweiMore, value)
	tests := []struct {
		name           string
		swap           *swapv0.ETHSwapSwap
		bn             uint64
		value          *big.Int
		ct             swapCoinType
		wantConfs      int64
		swapErr, bnErr error
		wantErr, setCT bool
	}{{
		name:      "ok has confs value not verified",
		bn:        100,
		swap:      tSwap(97, locktime, value, dexeth.SSInitiated, &initParticipantAddr),
		value:     value,
		ct:        sctInit,
		wantConfs: 3,
	}, {
		name:  "ok no confs",
		swap:  tSwap(0, bigO, bigO, dexeth.SSNone, nullAddr),
		value: value,
		ct:    sctInit,
	}, {
		name:      "ok redeem swap status redeemed",
		swap:      tSwap(97, locktime, value, dexeth.SSRedeemed, &initParticipantAddr),
		value:     bigO,
		ct:        sctRedeem,
		wantConfs: 1,
	}, {
		name:  "ok redeem swap status initiated",
		swap:  tSwap(97, locktime, value, dexeth.SSInitiated, &initParticipantAddr),
		value: bigO,
		ct:    sctRedeem,
	}, {
		name:    "unknown coin type",
		ct:      sctInit,
		setCT:   true,
		wantErr: true,
	}, {
		name:    "redeem bad swap state None",
		swap:    tSwap(0, bigO, bigO, dexeth.SSNone, nullAddr),
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
		swap:    tSwap(99, locktime, overMaxWei(), dexeth.SSInitiated, &initParticipantAddr),
		value:   value,
		ct:      sctInit,
		wantErr: true,
	}, {
		name:    "value differs from initial transaction",
		swap:    tSwap(99, locktime, oneGweiMore, dexeth.SSInitiated, &initParticipantAddr),
		value:   value,
		ct:      sctInit,
		wantErr: true,
	}, {
		name:    "participant differs from initial transaction",
		swap:    tSwap(99, locktime, value, dexeth.SSInitiated, nullAddr),
		value:   value,
		ct:      sctInit,
		wantErr: true,
	}, {
		name:    "locktime not an int64",
		swap:    tSwap(99, new(big.Int).SetUint64(^uint64(0)), value, dexeth.SSInitiated, &initParticipantAddr),
		value:   value,
		ct:      sctInit,
		wantErr: true,
	}, {
		name:    "locktime differs from initial transaction",
		swap:    tSwap(99, bigO, value, dexeth.SSInitiated, &initParticipantAddr),
		value:   value,
		ct:      sctInit,
		wantErr: true,
	}, {
		name:    "block number error",
		swap:    tSwap(97, locktime, value, dexeth.SSInitiated, &initParticipantAddr),
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
			initTxSize:   uint32(dexeth.InitGas(1, 0)),
		}

		sc, err := eth.newSwapCoin(txHash[:], secretHash[:], test.ct)
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

func TestValidateRedeem(t *testing.T) {
	contractAddr := new(common.Address)
	copy(contractAddr[:], encode.RandomBytes(20))
	secretHash := [32]byte{}
	copy(secretHash[:], encode.RandomBytes(20))
	scRedeem := &swapCoin{
		contractAddr: *contractAddr,
		secretHash:   secretHash,
		sct:          sctRedeem,
	}

	tests := []struct {
		name       string
		wantErr    bool
		sc         *swapCoin
		contractID []byte
	}{{
		name:       "ok",
		sc:         scRedeem,
		contractID: secretHash[:],
	}, {
		name:    "cannot decode contract ID",
		sc:      scRedeem,
		wantErr: true,
	}, {
		name:       "mismatched contract ID secret hash",
		sc:         scRedeem,
		contractID: encode.RandomBytes(32),
		wantErr:    true,
	}}
	for _, test := range tests {
		err := test.sc.validateRedeem(test.contractID)
		if test.wantErr {
			if err == nil {
				t.Fatalf("expected error for test %q", test.name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("unexpected error for test %q: %v", test.name, err)
		}
	}
}
