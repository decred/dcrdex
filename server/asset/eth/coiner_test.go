//go:build !harness

// These tests will not be run if the harness build tag is set.

package eth

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"testing"

	"decred.org/dcrdex/dex/encode"
	dexeth "decred.org/dcrdex/dex/networks/eth"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	// swapv0 "decred.org/dcrdex/dex/networks/eth/contracts/v0"
	// swapv1 "decred.org/dcrdex/dex/networks/eth/contracts/v1"
)

func randomAddress() *common.Address {
	var addr common.Address
	copy(addr[:], encode.RandomBytes(20))
	return &addr
}

func TestNewRedeemCoin(t *testing.T) {
	contractAddr := randomAddress()
	secret, secretHash, locator := secretB, secretHashB, locatorB
	var txHash [32]byte
	copy(txHash[:], encode.RandomBytes(32))
	const gasPrice = 30
	const gasTipCap = 2
	const value = 5e9
	const wantGas = 30
	const wantGasTipCap = 2
	tests := []struct {
		name          string
		ver           uint32
		locator       []byte
		tx            *types.Transaction
		swpErr, txErr error
		swap          *dexeth.SwapState
		wantErr       bool
	}{
		{
			name:    "ok redeem v0",
			tx:      tTx(gasPrice, gasTipCap, 0, contractAddr, redeemCalldataV0),
			swap:    tSwap(97, initLocktime, 1000, secret, dexeth.SSRedeemed, &participantAddr),
			locator: secretHash[:],
		}, {
			name:    "ok redeem v1",
			ver:     1,
			tx:      tTx(gasPrice, gasTipCap, 0, contractAddr, redeemCalldataV1),
			swap:    tSwap(97, initLocktime, 1000, secret, dexeth.SSRedeemed, &participantAddr),
			locator: locator,
		}, {
			name:    "non zero value with redeem",
			tx:      tTx(gasPrice, gasTipCap, value, contractAddr, redeemCalldataV0),
			locator: secretHash[:],
			wantErr: true,
		}, {
			name:    "unable to decode redeem data, must be redeem for redeem coin type",
			tx:      tTx(gasPrice, gasTipCap, 0, contractAddr, initCalldataV0),
			locator: secretHash[:],
			wantErr: true,
		}, {
			name:    "tx coin id for redeem - contract not in tx",
			tx:      tTx(gasPrice, gasTipCap, value, contractAddr, redeemCalldataV0),
			locator: encode.RandomBytes(32),
			wantErr: true,
		}, {
			name:    "tx not found, redeemed",
			txErr:   ethereum.NotFound,
			swap:    tSwap(97, initLocktime, 1000, secret, dexeth.SSRedeemed, &participantAddr),
			locator: secretHash[:],
		}, {
			name:    "tx not found, not redeemed",
			txErr:   ethereum.NotFound,
			swap:    tSwap(97, initLocktime, 1000, secret, dexeth.SSInitiated, &participantAddr),
			locator: secretHash[:],
			wantErr: true,
		}, {
			name:    "tx not found, swap err",
			txErr:   ethereum.NotFound,
			swpErr:  errors.New("swap not found"),
			locator: secretHash[:],
			wantErr: true,
		},
	}
	for _, test := range tests {
		node := &testNode{
			tx:     test.tx,
			txErr:  test.txErr,
			swpErr: test.swpErr,
			swp: map[string]*dexeth.SwapState{
				string(secretHash[:]): test.swap,
			},
		}
		eth := &AssetBackend{
			baseBackend: &baseBackend{
				node:       node,
				baseLogger: tLogger,
			},
			contractAddr: *contractAddr,
			assetID:      BipID,
			log:          tLogger,
			atomize:      dexeth.WeiToGwei,
		}
		rc, err := eth.newRedeemCoin(txHash[:], dexeth.EncodeContractData(test.ver, test.locator))
		if test.wantErr {
			if err == nil {
				t.Fatalf("expected error for test %q", test.name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("unexpected error for test %q: %v", test.name, err)
		}

		if test.txErr == nil && (!bytes.Equal(rc.locator, test.locator) ||
			rc.secret != secretB ||
			rc.value != 0 ||
			dexeth.WeiToGwei(rc.gasFeeCap) != wantGas ||
			dexeth.WeiToGwei(rc.gasTipCap) != wantGasTipCap) {
			t.Fatalf("returns do not match expected for test %q / %v", test.name, rc)
		}
	}
}

func TestNewSwapCoin(t *testing.T) {
	contractAddr, randomAddr := randomAddress(), randomAddress()
	var txHash [32]byte
	copy(txHash[:], encode.RandomBytes(32))
	txCoinIDBytes := txHash[:]
	badCoinIDBytes := encode.RandomBytes(39)
	const gasPrice = 30
	const value = 5e9
	const gasTipCap = 2
	wantGas := dexeth.WeiToGweiCeil(big.NewInt(3e10))
	wantVal := dexeth.GweiToWei(initValue)
	wantGasTipCap := dexeth.WeiToGweiCeil(big.NewInt(2e9))
	tests := []struct {
		name    string
		ver     uint32
		coinID  []byte
		locator []byte
		tx      *types.Transaction
		swap    *dexeth.SwapState
		txErr   error
		wantErr bool
	}{
		{
			name:    "ok init v0",
			tx:      tTx(gasPrice, gasTipCap, value, contractAddr, initCalldataV0),
			coinID:  txCoinIDBytes,
			locator: secretHashA[:],
			swap:    tSwap(97, initLocktime, initValue, secretA, dexeth.SSRedeemed, &participantAddr),
		}, {
			name:    "ok init v1",
			ver:     1,
			tx:      tTx(gasPrice, gasTipCap, value, contractAddr, initCalldataV1),
			coinID:  txCoinIDBytes,
			locator: locatorA,
			swap:    tSwap(97, initLocktime, initValue, secretA, dexeth.SSRedeemed, &participantAddr),
		}, {
			name:    "contract incorrect length",
			tx:      tTx(gasPrice, gasTipCap, value, contractAddr, initCalldataV0),
			coinID:  txCoinIDBytes,
			locator: secretHashA[:31],
			wantErr: true,
		}, {
			name:    "tx has no data",
			tx:      tTx(gasPrice, gasTipCap, value, contractAddr, nil),
			coinID:  txCoinIDBytes,
			locator: secretHashA[:],
			wantErr: true,
		}, {
			name:    "unable to decode init data, must be init for init coin type",
			tx:      tTx(gasPrice, gasTipCap, value, contractAddr, redeemCalldataV0),
			coinID:  txCoinIDBytes,
			locator: secretHashA[:],
			wantErr: true,
		}, {
			name:    "unable to decode CoinID",
			tx:      tTx(gasPrice, gasTipCap, value, contractAddr, initCalldataV0),
			locator: secretHashA[:],
			wantErr: true,
		}, {
			name:    "invalid coinID",
			tx:      tTx(gasPrice, gasTipCap, value, contractAddr, initCalldataV0),
			coinID:  badCoinIDBytes,
			locator: secretHashA[:],
			wantErr: true,
		}, {
			name:    "transaction error",
			tx:      tTx(gasPrice, gasTipCap, value, contractAddr, initCalldataV0),
			coinID:  txCoinIDBytes,
			locator: secretHashA[:],
			txErr:   errors.New(""),
			wantErr: true,
		}, {
			name:    "transaction not found error",
			tx:      tTx(gasPrice, gasTipCap, value, contractAddr, initCalldataV0),
			coinID:  txCoinIDBytes,
			locator: secretHashA[:],
			txErr:   ethereum.NotFound,
			wantErr: true,
		}, {
			name:    "wrong contract",
			tx:      tTx(gasPrice, gasTipCap, value, randomAddr, initCalldataV0),
			coinID:  txCoinIDBytes,
			locator: secretHashA[:],
			wantErr: true,
		}, {
			name:    "tx coin id for swap - contract not in tx",
			tx:      tTx(gasPrice, gasTipCap, value, contractAddr, initCalldataV0),
			coinID:  txCoinIDBytes,
			locator: encode.RandomBytes(32),
			wantErr: true,
		},
	}
	for _, test := range tests {
		node := &testNode{
			tx:    test.tx,
			txErr: test.txErr,
			swp: map[string]*dexeth.SwapState{
				string(secretHashA[:]): test.swap,
			},
		}
		eth := &AssetBackend{
			baseBackend: &baseBackend{
				node:       node,
				baseLogger: tLogger,
			},
			contractAddr: *contractAddr,
			atomize:      dexeth.WeiToGwei,
		}
		sc, err := eth.newSwapCoin(test.coinID, dexeth.EncodeContractData(test.ver, test.locator))
		if test.wantErr {
			if err == nil {
				t.Fatalf("expected error for test %q", test.name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("unexpected error for test %q: %v", test.name, err)
		}

		if sc.vector.To != participantAddr ||
			sc.vector.SecretHash != secretHashA ||
			sc.vector.Value.Cmp(wantVal) != 0 ||
			dexeth.WeiToGwei(sc.gasFeeCap) != wantGas ||
			dexeth.WeiToGwei(sc.gasTipCap) != wantGasTipCap ||
			sc.vector.LockTime != initLocktime {

			fmt.Println("to:", sc.vector.To, participantAddr)
			fmt.Println("secret hash:", hex.EncodeToString(sc.vector.SecretHash[:]), hex.EncodeToString(secretHashA[:]))
			fmt.Println("value:", sc.value, value)
			fmt.Println("swap value:", sc.vector.Value, wantVal)
			fmt.Println("gas fee cap:", sc.gasFeeCap, wantGas)
			fmt.Println("gas tip cap:", sc.gasTipCap, wantGasTipCap)
			fmt.Println("lock time:", sc.vector.LockTime, initLocktime)

			t.Fatalf("returns do not match expected for test %q / %v", test.name, sc)
		}
	}
}

type Confirmer interface {
	Confirmations(context.Context) (int64, error)
	String() string
}

func TestConfirmations(t *testing.T) {
	contractAddr, nullAddr := new(common.Address), new(common.Address)
	copy(contractAddr[:], encode.RandomBytes(20))
	secret, secretHash := secretB, secretHashB
	var txHash [32]byte
	copy(txHash[:], encode.RandomBytes(32))
	const gasPrice = 30
	const gasTipCap = 2
	txVal := initValue * 2
	oneGweiMore := initValue + 1
	tests := []struct {
		name            string
		ver             uint32
		swap            *dexeth.SwapState
		receipt         *types.Receipt // v1 only
		bn              uint64
		value           uint64
		wantConfs       int64
		swapErr, bnErr  error
		wantErr, redeem bool
	}{
		{
			name:      "ok has confs value not verified v0",
			bn:        100,
			swap:      tSwap(97, initLocktime, initValue, secret, dexeth.SSInitiated, &participantAddr),
			value:     txVal,
			wantConfs: 4,
		}, {
			name:      "ok has confs value not verified v1",
			ver:       1,
			bn:        100,
			swap:      tSwap(97, initLocktime, initValue, secret, dexeth.SSInitiated, &participantAddr),
			value:     txVal,
			wantConfs: 4,
		}, {
			name:  "ok no confs",
			swap:  tSwap(0, 0, 0, secret, dexeth.SSNone, nullAddr),
			value: txVal,
		}, {
			name: "ok 1 conf v1",
			ver:  1,
			bn:   97,
			receipt: &types.Receipt{
				BlockNumber: big.NewInt(97),
			},
			swap:      tSwap(0, 0, 0, secret, dexeth.SSRedeemed, nullAddr),
			value:     txVal,
			wantConfs: 1,
		}, {
			name:      "ok redeem swap status redeemed",
			bn:        97,
			swap:      tSwap(97, initLocktime, initValue, secret, dexeth.SSRedeemed, &participantAddr),
			value:     0,
			wantConfs: 1,
			redeem:    true,
		}, {
			name:   "ok redeem swap status initiated",
			swap:   tSwap(97, initLocktime, initValue, secret, dexeth.SSInitiated, &participantAddr),
			value:  0,
			redeem: true,
		}, {
			name:    "redeem bad swap state None",
			swap:    tSwap(0, 0, 0, secret, dexeth.SSNone, nullAddr),
			value:   0,
			wantErr: true,
			redeem:  true,
		}, {
			name:    "error getting swap",
			swapErr: errors.New("test error"),
			value:   txVal,
			wantErr: true,
		}, {
			name:    "value differs from initial transaction",
			swap:    tSwap(99, initLocktime, oneGweiMore, secret, dexeth.SSInitiated, &participantAddr),
			value:   txVal,
			wantErr: true,
		}, {
			name:    "participant differs from initial transaction",
			swap:    tSwap(99, initLocktime, initValue, secret, dexeth.SSInitiated, nullAddr),
			value:   txVal,
			wantErr: true,
			// }, {
			// 	name:    "locktime not an int64",
			// 	swap:    tSwap(99, new(big.Int).SetUint64(^uint64(0)), value, secret, dexeth.SSInitiated, &participantAddr),
			// 	value:   value,
			// 	ct:      sctInit,
			// 	wantErr: true,
		}, {
			name:    "locktime differs from initial transaction",
			swap:    tSwap(99, 0, initValue, secret, dexeth.SSInitiated, &participantAddr),
			value:   txVal,
			wantErr: true,
		}, {
			name:    "block number error",
			swap:    tSwap(97, initLocktime, initValue, secret, dexeth.SSInitiated, &participantAddr),
			value:   txVal,
			bnErr:   errors.New("test error"),
			wantErr: true,
		},
	}
	for _, test := range tests {
		node := &testNode{
			swpErr:    test.swapErr,
			blkNum:    test.bn,
			blkNumErr: test.bnErr,
			swp: map[string]*dexeth.SwapState{
				string(secretHash[:]): test.swap,
				string(locatorA):      test.swap,
			},
			receipt: test.receipt,
		}
		eth := &AssetBackend{
			baseBackend: &baseBackend{
				node:       node,
				baseLogger: tLogger,
			},
			contractAddr: *contractAddr,
			atomize:      dexeth.WeiToGwei,
		}

		locator := secretHash[:]
		redeemCalldata := redeemCalldataV0
		initCalldata := initCalldataV0
		if test.ver == 1 {
			redeemCalldata = redeemCalldataV1
			initCalldata = initCalldataV1
			locator = locatorA
		}
		swapData := dexeth.EncodeContractData(test.ver, locator)

		var confirmer Confirmer
		var err error
		if test.redeem {
			node.tx = tTx(gasPrice, gasTipCap, test.value, contractAddr, redeemCalldata)
			confirmer, err = eth.newRedeemCoin(txHash[:], swapData)
		} else {
			node.tx = tTx(gasPrice, gasTipCap, test.value, contractAddr, initCalldata)
			confirmer, err = eth.newSwapCoin(txHash[:], swapData)
		}
		if err != nil {
			t.Fatalf("unexpected error for test %q: %v", test.name, err)
		}

		_ = confirmer.String() // unrelated panic test

		confs, err := confirmer.Confirmations(context.TODO())
		if test.wantErr {
			if err == nil {
				t.Fatalf("expected error for test %q", test.name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("unexpected error for test %q (redeem = %t): %v", test.name, test.redeem, err)
		}
		if confs != test.wantConfs {
			t.Fatalf("want %d but got %d confs for test: %v", test.wantConfs, confs, test.name)
		}
	}
}

// func TestGeneratePackedInits(t *testing.T) {
// 	initsV0 := []swapv0.ETHSwapInitiation{
// 		{
// 			RefundTimestamp: big.NewInt(int64(swapVectorA.LockTime)),
// 			SecretHash:      swapVectorA.SecretHash,
// 			Value:           swapVectorA.Value,
// 			Participant:     swapVectorA.To,
// 		},
// 		{
// 			RefundTimestamp: big.NewInt(int64(swapVectorB.LockTime)),
// 			SecretHash:      swapVectorB.SecretHash,
// 			Value:           swapVectorB.Value,
// 			Participant:     swapVectorB.To,
// 		},
// 	}
// 	dataV0, err := dexeth.ABIs[0].Pack("initiate", initsV0)
// 	if err != nil {
// 		t.Fatalf("Pack error: %v", err)
// 	}
// 	fmt.Println("V0 Init Data", hex.EncodeToString(dataV0))

// 	init0 := dexeth.SwapVectorToAbigen(swapVectorA)
// 	init1 := dexeth.SwapVectorToAbigen(swapVectorB)
// 	inits := []swapv1.ETHSwapVector{init0, init1}

// 	dataV1, err := dexeth.ABIs[1].Pack("initiate", common.Address{}, inits)
// 	if err != nil {
// 		t.Fatalf("Pack error: %v", err)
// 	}
// 	fmt.Println("V1 Init Data:", hex.EncodeToString(dataV1))

// 	redeemsV0 := []swapv0.ETHSwapRedemption{
// 		{
// 			SecretHash: secretHashA,
// 			Secret:     secretA,
// 		},
// 		{
// 			SecretHash: secretHashB,
// 			Secret:     secretB,
// 		},
// 	}
// 	dataV0, err = dexeth.ABIs[0].Pack("redeem", redeemsV0)
// 	if err != nil {
// 		t.Fatalf("Pack error: %v", err)
// 	}
// 	fmt.Println("V0 Redeem Data", hex.EncodeToString(dataV0))

// 	redeemsV1 := []swapv1.ETHSwapRedemption{
// 		{
// 			V:      init0,
// 			Secret: secretA,
// 		},
// 		{
// 			V:      init1,
// 			Secret: secretB,
// 		},
// 	}
// 	dataV1, err = dexeth.ABIs[1].Pack("redeem", common.Address{}, redeemsV1)
// 	if err != nil {
// 		t.Fatalf("Pack error: %v", err)
// 	}
// 	fmt.Println("V1 Redeem Data", hex.EncodeToString(dataV1))

// 	t.Fatal("ok")
// }
