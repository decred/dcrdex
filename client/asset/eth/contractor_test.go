package eth

import (
	"fmt"
	"math/big"
	"testing"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex/encode"
	swapv0 "decred.org/dcrdex/dex/networks/eth/contracts/v0"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/core/types"
)

type tContractV0 struct {
	initErr   error
	redeemErr error
	swapErr   error
}

func (c tContractV0) Initiate(opts *bind.TransactOpts, initiations []swapv0.ETHSwapInitiation) (*types.Transaction, error) {
	return nil, c.initErr
}

func (c tContractV0) Redeem(opts *bind.TransactOpts, redemptions []swapv0.ETHSwapRedemption) (*types.Transaction, error) {
	return nil, c.redeemErr
}

func (c tContractV0) Swap(opts *bind.CallOpts, secretHash [32]byte) (swapv0.ETHSwapSwap, error) {
	return swapv0.ETHSwapSwap{
		InitBlockNumber:      new(big.Int),
		RefundBlockTimestamp: new(big.Int),
		Value:                new(big.Int),
	}, c.swapErr
}

func (c tContractV0) Refund(opts *bind.TransactOpts, secretHash [32]byte) (*types.Transaction, error) {
	return nil, nil
}

func (c tContractV0) IsRedeemable(opts *bind.CallOpts, secretHash [32]byte, secret [32]byte) (bool, error) {
	return false, nil
}

func TestInitV0(t *testing.T) {
	abiContract := &tContractV0{}
	c := contractorV0{contractV0: abiContract}
	addrStr := "0xB6De8BB5ed28E6bE6d671975cad20C03931bE981"
	contract := &asset.Contract{
		SecretHash: encode.RandomBytes(32),
		Address:    addrStr,
	}

	contracts := []*asset.Contract{contract}

	checkResult := func(tag string, wantErr bool) {
		_, err := c.initiate(nil, contracts)
		if (err != nil) != wantErr {
			t.Fatal(tag)
		}
	}

	checkResult("first success", false)

	// wrong secret hash size
	contract.SecretHash = encode.RandomBytes(20)
	checkResult("bad hash", true)
	contract.SecretHash = encode.RandomBytes(32)

	// dupe hash
	contracts = []*asset.Contract{contract, contract}
	checkResult("dupe hash", true)
	contracts = []*asset.Contract{contract}

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

	redemption := &asset.Redemption{
		Secret: encode.RandomBytes(32),
		Spends: &asset.AuditInfo{SecretHash: encode.RandomBytes(32)},
	}

	redemptions := []*asset.Redemption{redemption}

	checkResult := func(tag string, wantErr bool) {
		_, err := c.redeem(nil, redemptions)
		if (err != nil) != wantErr {
			t.Fatal(tag)
		}
	}

	checkResult("initial success", false)

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

	// Success again
	checkResult("success again", false)
}
