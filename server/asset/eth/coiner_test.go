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
			contractAddr: *contractAddr,
		}
		sc, err := eth.newSwapCoin(test.coinID, test.ct)
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
			contractAddr: *contractAddr,
		}

		sc, err := eth.newSwapCoin(txCoinIDBytes, test.ct)
		if err != nil {
			t.Fatalf("unexpected error for test %q: %v", test.name, err)
		}

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
	contractAddr, nullAddr := new(common.Address), new(common.Address)
	copy(contractAddr[:], encode.RandomBytes(20))
	secretHash, nullHash := [32]byte{}, [32]byte{}
	copy(secretHash[:], encode.RandomBytes(20))
	scID := SwapCoinID{
		ContractAddress: *contractAddr,
		SecretHash:      secretHash,
	}
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
		contractID: scID.Encode(),
	}, {
		name: "sc not a redeem",
		sc: &swapCoin{
			contractAddr: *contractAddr,
			secretHash:   secretHash,
			sct:          sctInit,
		},
		contractID: scID.Encode(),
		wantErr:    true,
	}, {
		name:    "cannot decode contract ID",
		sc:      scRedeem,
		wantErr: true,
	}, {
		name:       "contract ID is not a swap",
		sc:         scRedeem,
		contractID: new(TxCoinID).Encode(),
		wantErr:    true,
	}, {
		name: "mismatched contract ID contract address",
		sc:   scRedeem,
		contractID: func() []byte {
			badAddr := SwapCoinID{
				ContractAddress: *nullAddr,
				SecretHash:      secretHash,
			}
			return badAddr.Encode()
		}(),
		wantErr: true,
	}, {
		name: "mismatched contract ID secret hash",
		sc:   scRedeem,
		contractID: func() []byte {
			badHash := SwapCoinID{
				ContractAddress: *contractAddr,
				SecretHash:      nullHash,
			}
			return badHash.Encode()
		}(),
		wantErr: true,
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

func TestNewAmountCoin(t *testing.T) {
	addr := new(common.Address)
	copy(addr[:], encode.RandomBytes(20))
	amt := uint64(3)
	acID := &AmountCoinID{
		Address: *addr,
		Amount:  amt,
	}

	tests := []struct {
		name                  string
		wantErr               bool
		acID                  []byte
		bal, pendingBal       uint64
		balErr, txpoolContErr error
	}{{
		name: "ok",
		acID: acID.Encode(),
		bal:  amt,
	}, {
		name:    "cannot decode coin ID",
		bal:     amt,
		wantErr: true,
	}, {
		name:    "not an amount coin",
		acID:    new(SwapCoinID).Encode(),
		bal:     amt,
		wantErr: true,
	}, {
		name:    "balance error",
		acID:    acID.Encode(),
		balErr:  errors.New(""),
		wantErr: true,
	}, {
		name:    "not enough funds",
		acID:    acID.Encode(),
		wantErr: true,
	}, {
		name:          "error getting pending tx fee",
		acID:          acID.Encode(),
		pendingBal:    amt,
		txpoolContErr: errors.New(""),
		wantErr:       true,
	}}
	for _, test := range tests {
		node := &testNode{
			bal:           big.NewInt(int64(test.bal * GweiFactor)),
			balErr:        test.balErr,
			pendingBal:    big.NewInt(int64(test.pendingBal * GweiFactor)),
			txpoolContErr: test.txpoolContErr,
		}
		eth := &Backend{
			node: node,
		}
		cnr, err := eth.newAmountCoin(test.acID)
		if test.wantErr {
			if err == nil {
				t.Fatalf("expected error for test %q", test.name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("unexpected error for test %q: %v", test.name, err)
		}
		if cnr.cID.Amount != amt ||
			cnr.cID.Address != *addr {
			t.Fatalf("unexpected coiner values for test %v", test.name)
		}
	}
}

func TestAccountBalance(t *testing.T) {
	addr := new(common.Address)
	copy(addr[:], encode.RandomBytes(20))
	amt := uint64(3)

	tests := []struct {
		name                  string
		wantErr               bool
		bal, pendingBal       *big.Int
		balErr, pendingBalErr error
	}{{
		name:       "ok",
		bal:        big.NewInt(int64(amt * GweiFactor)),
		pendingBal: big.NewInt(int64((amt - 1) * GweiFactor)),
	}, {
		name:          "pending balance error",
		pendingBalErr: errors.New(""),
		wantErr:       true,
	}, {
		name:       "balance error",
		pendingBal: big.NewInt(0),
		balErr:     errors.New(""),
		wantErr:    true,
	}, {
		name:       "balance too big",
		pendingBal: big.NewInt(0),
		bal:        overMaxWei(),
		wantErr:    true,
	}, {
		name:       "pending balance too big",
		pendingBal: overMaxWei(),
		bal:        big.NewInt(0),
		wantErr:    true,
	}}
	for _, test := range tests {
		node := &testNode{
			bal:           test.bal,
			balErr:        test.balErr,
			pendingBal:    test.pendingBal,
			pendingBalErr: test.pendingBalErr,
		}
		eth := &Backend{
			node: node,
		}
		bal, pendingBal, err := eth.accountBalance(addr)
		if test.wantErr {
			if err == nil {
				t.Fatalf("expected error for test %q", test.name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("unexpected error for test %q: %v", test.name, err)
		}
		if bal != test.bal.Uint64() || pendingBal != test.pendingBal.Uint64() {
			t.Fatalf("unexpected balance for test %v", test.name)
		}
	}
}

func TestLowestGasPrice(t *testing.T) {
	addr := new(common.Address)
	copy(addr[:], encode.RandomBytes(20))
	cont := func(gasses []int64) map[string]map[string]map[int]*types.Transaction {
		m := make(map[string]map[string]map[int]*types.Transaction)
		m["pending"] = make(map[string]map[int]*types.Transaction)
		m["pending"]["deadbeefaccount"] = make(map[int]*types.Transaction)
		m["pending"][addr.String()] = make(map[int]*types.Transaction)
		i := 0
		// These are credits from a dummy account.
		for _, gas := range gasses {
			bigGweiGas := big.NewInt(GweiFactor)
			tx := tTx(bigGweiGas.Mul(bigGweiGas, big.NewInt(gas)), nil, addr, nil)
			m["pending"]["deadbeefaccount"][i] = tx
			i++
		}
		// This is an ignored debit.
		m["pending"][addr.String()][i] = tTx(big.NewInt(0), nil, new(common.Address), nil)
		return m
	}

	tests := []struct {
		name          string
		wantLowestGas uint64
		wantErr       bool
		txpoolCont    map[string]map[string]map[int]*types.Transaction
		txpoolContErr error
	}{{
		name:          "ok lowest of three",
		txpoolCont:    cont([]int64{76, 44, 300}),
		wantLowestGas: 44,
	}, {
		name: "ok no txs",
	}, {
		name:          "content error",
		txpoolContErr: errors.New(""),
		wantErr:       true,
	}, {
		name:       "bad fee",
		txpoolCont: cont([]int64{-1}),
		wantErr:    true,
	}}
	for _, test := range tests {
		node := &testNode{
			txpoolCont:    test.txpoolCont,
			txpoolContErr: test.txpoolContErr,
		}
		eth := &Backend{
			node: node,
		}
		lgp, err := eth.lowestGasPrice(addr)
		if test.wantErr {
			if err == nil {
				t.Fatalf("expected error for test %q", test.name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("unexpected error for test %q: %v", test.name, err)
		}
		if lgp != test.wantLowestGas {
			t.Fatalf("expected %d but got %d for test %v", test.wantLowestGas, lgp, test.name)
		}
	}
}

func TestAmountCoinConfirmations(t *testing.T) {
	addr := new(common.Address)
	copy(addr[:], encode.RandomBytes(20))
	amt := uint64(3)

	tests := []struct {
		name            string
		wantErr         bool
		bal, pendingBal *big.Int
		pendingBalErr   error
		wantConfs       int64
	}{{
		name:       "ok has confirmed balance",
		bal:        big.NewInt(int64(amt * GweiFactor)),
		pendingBal: big.NewInt(0),
		wantConfs:  1,
	}, {
		name:       "ok has pending balance",
		pendingBal: big.NewInt(int64(amt * GweiFactor)),
		bal:        big.NewInt(0),
	}, {
		name:          "account has balance error",
		pendingBalErr: errors.New(""),
		wantErr:       true,
	}, {
		name:       "account doesn't have enough funds",
		bal:        big.NewInt(0),
		pendingBal: big.NewInt(0),
		wantErr:    true,
	}}
	for _, test := range tests {
		node := &testNode{
			bal:           test.bal,
			pendingBal:    test.pendingBal,
			pendingBalErr: test.pendingBalErr,
		}
		eth := &Backend{
			node: node,
		}
		ac := &amountCoin{
			backend: eth,
			cID: &AmountCoinID{
				Amount: amt,
			},
		}
		confs, err := ac.Confirmations(nil)
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
			t.Fatalf("want %d but got %d confs for test %v",
				test.wantConfs, confs, test.name)
		}
	}
}

func TestAuth(t *testing.T) {
	// "ok" values used are the same as tests in client/assets/eth.
	pkBytes := mustParseHex("04b911d1f39f7792e165767e35aa134083e2f70ac7de6945d7641a3015d09a54561b71112b8d60f63831f0e62c23c6921ec627820afedf8236155b9e9bd82b6523")
	msg := []byte("msg")
	sigBytes := mustParseHex("ffd26911d3fdaf11ac44801744f2df015a16539b6e688aff4cabc092b747466e7bc8036a03d1479a1570dd11bf042120301c34a65b237267720ef8a9e56f2eb101")
	addr := common.HexToAddress("2b84C791b79Ee37De042AD2ffF1A253c3ce9bc27")
	ac := &amountCoin{
		cID: &AmountCoinID{
			Address: addr,
		},
	}
	tests := []struct {
		name              string
		wantErr           bool
		ac                *amountCoin
		pkBytes, sigBytes [][]byte
		msg               []byte
	}{{
		name:     "ok",
		ac:       ac,
		pkBytes:  [][]byte{pkBytes},
		msg:      msg,
		sigBytes: [][]byte{sigBytes},
	}, {
		name:     "not one pubkey bytes",
		ac:       ac,
		pkBytes:  [][]byte{pkBytes, nil},
		msg:      msg,
		sigBytes: [][]byte{sigBytes},
		wantErr:  true,
	}, {
		name:     "not one sig bytes",
		ac:       ac,
		pkBytes:  [][]byte{pkBytes},
		msg:      msg,
		sigBytes: [][]byte{sigBytes, nil},
		wantErr:  true,
	}, {
		name:     "sig too short",
		ac:       ac,
		pkBytes:  [][]byte{pkBytes},
		msg:      msg,
		sigBytes: [][]byte{nil},
		wantErr:  true,
	}, {
		name: "pubkey doesn't match amount coin address",
		ac: &amountCoin{
			cID: new(AmountCoinID),
		},
		pkBytes:  [][]byte{pkBytes},
		msg:      msg,
		sigBytes: [][]byte{sigBytes},
		wantErr:  true,
	}, {
		name:     "bad pubkey",
		ac:       ac,
		pkBytes:  [][]byte{pkBytes[1:]},
		msg:      msg,
		sigBytes: [][]byte{sigBytes},
		wantErr:  true,
	}, {
		name:     "cannot verify signature, bad msg",
		ac:       ac,
		pkBytes:  [][]byte{pkBytes},
		sigBytes: [][]byte{sigBytes},
		wantErr:  true,
	}}

	for _, test := range tests {
		err := test.ac.Auth(test.pkBytes, test.sigBytes, test.msg)
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
