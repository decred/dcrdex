//go:build lgpl
// +build lgpl

package eth

import (
	"bytes"
	"fmt"
	"math/big"
	"testing"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex/encode"
	dexeth "decred.org/dcrdex/dex/networks/eth"
	swapv0 "decred.org/dcrdex/dex/networks/eth/contracts/v0"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

type tContractV0 struct {
	lastInits   []swapv0.ETHSwapInitiation
	initErr     error
	lastRedeems []swapv0.ETHSwapRedemption
	redeemErr   error
	swap        swapv0.ETHSwapSwap
	swapErr     error
}

func (c *tContractV0) Initiate(opts *bind.TransactOpts, initiations []swapv0.ETHSwapInitiation) (*types.Transaction, error) {
	c.lastInits = initiations
	return nil, c.initErr
}

func (c *tContractV0) Redeem(opts *bind.TransactOpts, redemptions []swapv0.ETHSwapRedemption) (*types.Transaction, error) {
	c.lastRedeems = redemptions
	return nil, c.redeemErr
}

func (c *tContractV0) Swap(opts *bind.CallOpts, secretHash [32]byte) (swapv0.ETHSwapSwap, error) {
	return c.swap, c.swapErr
}

func (c *tContractV0) Refund(opts *bind.TransactOpts, secretHash [32]byte) (*types.Transaction, error) {
	return nil, nil
}

func (c *tContractV0) IsRedeemable(opts *bind.CallOpts, secretHash [32]byte, secret [32]byte) (bool, error) {
	return false, nil
}

func TestInitV0(t *testing.T) {
	abiContract := &tContractV0{}
	c := contractorV0{contractV0: abiContract}
	addrStr := "0xB6De8BB5ed28E6bE6d671975cad20C03931bE981"
	secretHashB := encode.RandomBytes(32)
	const gweiVal = 123456
	const lockTime = 100_000_000

	contract := &asset.Contract{
		SecretHash: secretHashB,
		Address:    addrStr,
		Value:      gweiVal,
		LockTime:   lockTime,
	}

	contracts := []*asset.Contract{contract}

	checkResult := func(tag string, wantErr bool) {
		_, err := c.initiate(nil, contracts)
		if (err != nil) != wantErr {
			t.Fatal(tag)
		}
	}

	checkResult("first success", false)

	if len(abiContract.lastInits) != 1 {
		t.Fatalf("wrong number of inits translated, %d", len(abiContract.lastInits))
	}
	init := abiContract.lastInits[0]
	if init.RefundTimestamp.Uint64() != lockTime {
		t.Fatalf("wrong RefundTimestamp. expected %d, got %d", lockTime, init.RefundTimestamp.Uint64())
	}
	if !bytes.Equal(init.SecretHash[:], secretHashB) {
		t.Fatalf("wrong secret hash.")
	}
	if init.Participant != common.HexToAddress(addrStr) {
		t.Fatalf("wrong address. wanted %s, got %s", common.HexToAddress(addrStr), init.Participant)
	}
	if dexeth.WeiToGwei(init.Value) != gweiVal {
		t.Fatalf("wrong value. wanted %d, got %d", gweiVal, dexeth.WeiToGwei(init.Value))
	}

	// wrong secret hash size
	contract.SecretHash = encode.RandomBytes(20)
	checkResult("bad hash", true)
	contract.SecretHash = encode.RandomBytes(32)

	// dupe hash
	contracts = []*asset.Contract{contract, contract}
	checkResult("dupe hash", true)

	// ok with two
	contract2 := *contract
	contract2.SecretHash = encode.RandomBytes(32)
	contracts = []*asset.Contract{contract, &contract2}
	checkResult("ok two", false)
	contracts = []*asset.Contract{contract}

	if len(abiContract.lastInits) != 2 {
		t.Fatalf("two contracts weren't passed")
	}

	// bad address
	contract.Address = "badaddress"
	checkResult("bad address", true)
	contract.Address = addrStr

	// Initiate error
	abiContract.initErr = fmt.Errorf("test error")
	checkResult("contract error", true)
	abiContract.initErr = nil

	// Success again
	checkResult("success again", false)
}

func TestRedeemV0(t *testing.T) {
	abiContract := &tContractV0{}
	c := contractorV0{contractV0: abiContract}

	secretB := encode.RandomBytes(32)
	secretHashB := encode.RandomBytes(32)

	redemption := &asset.Redemption{
		Secret: secretB,
		Spends: &asset.AuditInfo{SecretHash: secretHashB},
	}

	redemptions := []*asset.Redemption{redemption}

	checkResult := func(tag string, wantErr bool) {
		_, err := c.redeem(nil, redemptions)
		if (err != nil) != wantErr {
			t.Fatal(tag, err)
		}
	}

	checkResult("initial success", false)

	if len(abiContract.lastRedeems) != 1 {
		t.Fatalf("contract not passed")
	}
	redeem := abiContract.lastRedeems[0]
	if !bytes.Equal(redeem.Secret[:], secretB) {
		t.Fatalf("secret not translated")
	}
	if !bytes.Equal(redeem.SecretHash[:], secretHashB) {
		t.Fatalf("secret hash not translated")
	}

	// bad secret hash length
	redemption.Spends.SecretHash = encode.RandomBytes(20)
	checkResult("bad secret hash length", true)
	redemption.Spends.SecretHash = encode.RandomBytes(32)

	// bad secret length
	redemption.Secret = encode.RandomBytes(20)
	checkResult("bad secret length", true)
	redemption.Secret = encode.RandomBytes(32)

	// Redeem error
	abiContract.redeemErr = fmt.Errorf("test error")
	checkResult("contract error", true)
	abiContract.redeemErr = nil

	// Error on dupe.
	redemptions = []*asset.Redemption{redemption, redemption}
	checkResult("dupe error", true)

	// two OK
	redemption2 := &asset.Redemption{
		Secret: encode.RandomBytes(32),
		Spends: &asset.AuditInfo{SecretHash: encode.RandomBytes(32)},
	}
	redemptions = []*asset.Redemption{redemption, redemption2}
	checkResult("two ok", false)
}

func TestSwapV0(t *testing.T) {
	abiContract := &tContractV0{}
	c := contractorV0{contractV0: abiContract}

	var secret [32]byte
	const valGwei = 123_456
	const blockNum = 654_321
	const stamp = 789_654
	var initiator, participant common.Address
	copy(initiator[:], encode.RandomBytes(32))
	copy(participant[:], encode.RandomBytes(32))
	const state = 128

	abiContract.swap = swapv0.ETHSwapSwap{
		Secret:               secret,
		Value:                dexeth.GweiToWei(valGwei),
		InitBlockNumber:      big.NewInt(blockNum),
		RefundBlockTimestamp: big.NewInt(stamp),
		Initiator:            initiator,
		Participant:          participant,
		State:                state,
	}

	// error path
	abiContract.swapErr = fmt.Errorf("test error")
	_, err := c.swap(nil, [32]byte{})
	if err == nil {
		t.Fatalf("swap error not transmitted")
	}
	abiContract.swapErr = nil

	swap, err := c.swap(nil, [32]byte{})
	if err != nil {
		t.Fatalf("swap error: %v", err)
	}

	if swap.Secret != secret {
		t.Fatalf("wrong secret")
	}
	if swap.Value != valGwei {
		t.Fatalf("wrong value")
	}
	if swap.BlockHeight != blockNum {
		t.Fatalf("wrong block height")
	}
	if swap.LockTime.Unix() != stamp {
		t.Fatalf("wrong lock time")
	}
	if swap.Initiator != initiator {
		t.Fatalf("initiator not transmitted")
	}
	if swap.Participant != participant {
		t.Fatalf("participant not transmitted")
	}
	if swap.State != state {
		t.Fatalf("state not transmitted")
	}
}
