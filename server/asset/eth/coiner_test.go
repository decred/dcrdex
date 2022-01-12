//go:build !harness && lgpl

// These tests will not be run if the harness build tag is set.

package eth

import (
	"context"
	"errors"
	"math/big"
	"testing"

	"decred.org/dcrdex/dex/encode"
	dexeth "decred.org/dcrdex/dex/networks/eth"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

func randomAddress() *common.Address {
	var addr common.Address
	copy(addr[:], encode.RandomBytes(20))
	return &addr
}

func TestNewRedeemCoin(t *testing.T) {
	contractAddr := randomAddress()
	var secret, secretHash, txHash [32]byte
	copy(txHash[:], encode.RandomBytes(32))
	copy(secret[:], redeemSecretB)
	copy(secretHash[:], redeemSecretHashB)
	contract := dexeth.EncodeContractData(0, secretHash)
	const gasPrice = 30
	const gasTipCap = 2
	const value = 5e9
	const wantGas = 30
	const wantGasTipCap = 2
	tests := []struct {
		name          string
		contract      []byte
		tx            *types.Transaction
		swpErr, txErr error
		wantErr       bool
	}{{
		name:     "ok redeem",
		tx:       tTx(gasPrice, gasTipCap, 0, contractAddr, redeemCalldata),
		contract: contract,
	}, {
		name:     "non zero value with redeem",
		tx:       tTx(gasPrice, gasTipCap, value, contractAddr, redeemCalldata),
		contract: contract,
		wantErr:  true,
	}, {
		name:     "unable to decode redeem data, must be redeem for redeem coin type",
		tx:       tTx(gasPrice, gasTipCap, 0, contractAddr, initCalldata),
		contract: contract,
		wantErr:  true,
	}, {
		name:     "tx coin id for redeem - contract not in tx",
		tx:       tTx(gasPrice, gasTipCap, value, contractAddr, redeemCalldata),
		contract: encode.RandomBytes(32),
		wantErr:  true,
	}}
	for _, test := range tests {
		node := &testNode{
			tx:    test.tx,
			txErr: test.txErr,
		}
		eth := &AssetBackend{
			baseBackend: &baseBackend{
				node: node,
				log:  tLogger,
			},
			contractAddr: *contractAddr,
			initTxSize:   uint32(dexeth.InitGas(1, 0)),
			assetID:      BipID,
		}
		rc, err := eth.newRedeemCoin(txHash[:], test.contract)
		if test.wantErr {
			if err == nil {
				t.Fatalf("expected error for test %q", test.name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("unexpected error for test %q: %v", test.name, err)
		}
		if rc.secretHash != secretHash ||
			rc.secret != secret ||
			rc.value != 0 ||
			rc.gasFeeCap != wantGas ||
			rc.gasTipCap != wantGasTipCap {
			t.Fatalf("returns do not match expected for test %q / %v", test.name, rc)
		}
	}
}

func TestNewSwapCoin(t *testing.T) {
	contractAddr, randomAddr := randomAddress(), randomAddress()
	var secret, secretHash, txHash [32]byte
	copy(txHash[:], encode.RandomBytes(32))
	copy(secret[:], redeemSecretB)
	copy(secretHash[:], redeemSecretHashB)
	txCoinIDBytes := txHash[:]
	badCoinIDBytes := encode.RandomBytes(39)
	const gasPrice = 30
	const value = 5e9
	const gasTipCap = 2
	wantGas, err := dexeth.WeiToGweiUint64(big.NewInt(3e10))
	if err != nil {
		t.Fatal(err)
	}
	wantVal, err := dexeth.WeiToGweiUint64(big.NewInt(5e18))
	if err != nil {
		t.Fatal(err)
	}
	wantGasTipCap, err := dexeth.WeiToGweiUint64(big.NewInt(2e9))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name          string
		coinID        []byte
		contract      []byte
		tx            *types.Transaction
		swpErr, txErr error
		wantErr       bool
	}{{
		name:     "ok init",
		tx:       tTx(gasPrice, gasTipCap, value, contractAddr, initCalldata),
		coinID:   txCoinIDBytes,
		contract: dexeth.EncodeContractData(0, secretHash),
	}, {
		name:     "contract incorrect length",
		tx:       tTx(gasPrice, gasTipCap, value, contractAddr, initCalldata),
		coinID:   txCoinIDBytes,
		contract: initSecretHashA[:31],
		wantErr:  true,
	}, {
		name:     "tx has no data",
		tx:       tTx(gasPrice, gasTipCap, value, contractAddr, nil),
		coinID:   txCoinIDBytes,
		contract: initSecretHashA,
		wantErr:  true,
	}, {
		name:     "unable to decode init data, must be init for init coin type",
		tx:       tTx(gasPrice, gasTipCap, value, contractAddr, redeemCalldata),
		coinID:   txCoinIDBytes,
		contract: initSecretHashA,
		wantErr:  true,
	}, {
		name:     "unable to decode CoinID",
		tx:       tTx(gasPrice, gasTipCap, value, contractAddr, initCalldata),
		contract: initSecretHashA,
		wantErr:  true,
	}, {
		name:     "invalid coinID",
		tx:       tTx(gasPrice, gasTipCap, value, contractAddr, initCalldata),
		coinID:   badCoinIDBytes,
		contract: initSecretHashA,
		wantErr:  true,
	}, {
		name:     "transaction error",
		tx:       tTx(gasPrice, gasTipCap, value, contractAddr, initCalldata),
		coinID:   txCoinIDBytes,
		contract: initSecretHashA,
		txErr:    errors.New(""),
		wantErr:  true,
	}, {
		name:     "transaction not found error",
		tx:       tTx(gasPrice, gasTipCap, value, contractAddr, initCalldata),
		coinID:   txCoinIDBytes,
		contract: initSecretHashA,
		txErr:    ethereum.NotFound,
		wantErr:  true,
	}, {
		name:     "wrong contract",
		tx:       tTx(gasPrice, gasTipCap, value, randomAddr, initCalldata),
		coinID:   txCoinIDBytes,
		contract: initSecretHashA,
		wantErr:  true,
	}, {
		name:     "tx coin id for swap - contract not in tx",
		tx:       tTx(gasPrice, gasTipCap, value, contractAddr, initCalldata),
		coinID:   txCoinIDBytes,
		contract: encode.RandomBytes(32),
		wantErr:  true,
	}}
	for _, test := range tests {
		node := &testNode{
			tx:    test.tx,
			txErr: test.txErr,
		}
		eth := &AssetBackend{
			baseBackend: &baseBackend{
				node: node,
				log:  tLogger,
			},
			contractAddr: *contractAddr,
			initTxSize:   uint32(dexeth.InitGas(1, 0)),
		}
		sc, err := eth.newSwapCoin(test.coinID, test.contract)
		if test.wantErr {
			if err == nil {
				t.Fatalf("expected error for test %q", test.name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("unexpected error for test %q: %v", test.name, err)
		}

		if sc.init.Participant != initParticipantAddr ||
			sc.secretHash != secretHash ||
			sc.value != wantVal ||
			sc.gasFeeCap != wantGas ||
			sc.gasTipCap != wantGasTipCap ||
			sc.init.LockTime.Unix() != initLocktime {
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
	var secret, secretHash, txHash [32]byte
	copy(txHash[:], encode.RandomBytes(32))
	copy(secret[:], redeemSecretB)
	copy(secretHash[:], redeemSecretHashB)
	const gasPrice = 30
	const gasTipCap = 2
	const swapVal = 25e8
	const txVal = swapVal * 2
	const oneGweiMore = swapVal + 1
	tests := []struct {
		name            string
		swap            *dexeth.SwapState
		bn              uint64
		value           uint64
		wantConfs       int64
		swapErr, bnErr  error
		wantErr, redeem bool
	}{{
		name:      "ok has confs value not verified",
		bn:        100,
		swap:      tSwap(97, initLocktime, swapVal, secret, dexeth.SSInitiated, &initParticipantAddr),
		value:     txVal,
		wantConfs: 4,
	}, {
		name:  "ok no confs",
		swap:  tSwap(0, 0, 0, secret, dexeth.SSNone, nullAddr),
		value: txVal,
	}, {
		name:      "ok redeem swap status redeemed",
		bn:        97,
		swap:      tSwap(97, initLocktime, swapVal, secret, dexeth.SSRedeemed, &initParticipantAddr),
		value:     0,
		wantConfs: 1,
		redeem:    true,
	}, {
		name:   "ok redeem swap status initiated",
		swap:   tSwap(97, initLocktime, swapVal, secret, dexeth.SSInitiated, &initParticipantAddr),
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
		swapErr: errors.New(""),
		value:   txVal,
		wantErr: true,
	}, {
		name:    "value differs from initial transaction",
		swap:    tSwap(99, initLocktime, oneGweiMore, secret, dexeth.SSInitiated, &initParticipantAddr),
		value:   txVal,
		wantErr: true,
	}, {
		name:    "participant differs from initial transaction",
		swap:    tSwap(99, initLocktime, swapVal, secret, dexeth.SSInitiated, nullAddr),
		value:   txVal,
		wantErr: true,
		// }, {
		// 	name:    "locktime not an int64",
		// 	swap:    tSwap(99, new(big.Int).SetUint64(^uint64(0)), value, secret, dexeth.SSInitiated, &initParticipantAddr),
		// 	value:   value,
		// 	ct:      sctInit,
		// 	wantErr: true,
	}, {
		name:    "locktime differs from initial transaction",
		swap:    tSwap(99, 0, swapVal, secret, dexeth.SSInitiated, &initParticipantAddr),
		value:   txVal,
		wantErr: true,
	}, {
		name:    "block number error",
		swap:    tSwap(97, initLocktime, swapVal, secret, dexeth.SSInitiated, &initParticipantAddr),
		value:   txVal,
		bnErr:   errors.New(""),
		wantErr: true,
	}}
	for _, test := range tests {
		node := &testNode{
			swp:       test.swap,
			swpErr:    test.swapErr,
			blkNum:    test.bn,
			blkNumErr: test.bnErr,
		}
		eth := &AssetBackend{
			baseBackend: &baseBackend{
				node: node,
				log:  tLogger,
			},
			contractAddr: *contractAddr,
			initTxSize:   uint32(dexeth.InitGas(1, 0)),
		}

		swapData := dexeth.EncodeContractData(0, secretHash)

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

		confs, err := confirmer.Confirmations(nil)
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

// func TestGeneratePackedInits(t *testing.T) {
// 	hexToHash := func(s string) (h [32]byte) {
// 		b, _ := hex.DecodeString(s)
// 		copy(h[:], b)
// 		return
// 	}
// 	inits := []swapv0.ETHSwapInitiation{
// 		{
// 			RefundTimestamp: big.NewInt(1632112916),
// 			SecretHash:      hexToHash("8b3e4acc53b664f9cf6fcac0adcd328e95d62ba1f4379650ae3e1460a0f9d1a1"),
// 			Value:           dexeth.GweiToWei(25e8),
// 			Participant:     common.HexToAddress("0x345853e21b1d475582e71cc269124ed5e2dd3422"),
// 		},
// 		{
// 			RefundTimestamp: big.NewInt(1632112916),
// 			SecretHash:      hexToHash("ebdc4c31b88d0c8f4d644591a8e00e92b607f920ad8050deb7c7469767d9c561"),
// 			Value:           dexeth.GweiToWei(25e8),
// 			Participant:     common.HexToAddress("0x345853e21b1d475582e71cc269124ed5e2dd3422"),
// 		},
// 	}
// 	data, err := dexeth.ABIs[0].Pack("initiate", inits)
// 	if err != nil {
// 		t.Fatalf("Pack error: %v", err)
// 	}

// 	fmt.Printf("tx data: %x \n", data)
// }
