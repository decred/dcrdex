// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package zec

import (
	"encoding/hex"
	"fmt"
	"math"

	"decred.org/dcrdex/dex"
	dexbtc "decred.org/dcrdex/dex/networks/btc"
	dexzec "decred.org/dcrdex/dex/networks/zec"
	"decred.org/dcrdex/server/asset"
	"decred.org/dcrdex/server/asset/btc"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
)

// Driver implements asset.Driver.
type Driver struct{}

// Setup creates the ZCash backend. Start the backend with its Run method.
func (d *Driver) Setup(configPath string, logger dex.Logger, network dex.Network) (asset.Backend, error) {
	return NewBackend(configPath, logger, network)
}

// DecodeCoinID creates a human-readable representation of a coin ID for
// ZCash.
func (d *Driver) DecodeCoinID(coinID []byte) (string, error) {
	// ZCash and Bitcoin have the same tx hash and output format.
	return (&btc.Driver{}).DecodeCoinID(coinID)
}

// Version returns the Backend implementation's version number.
func (d *Driver) Version() uint32 {
	return version
}

// UnitInfo returns the dex.UnitInfo for the asset.
func (d *Driver) UnitInfo() dex.UnitInfo {
	return dexzec.UnitInfo
}

func init() {
	asset.Register(BipID, &Driver{})
}

const (
	version   = 0
	BipID     = 133
	assetName = "zec"
	feeConfs  = 10 // Block time is 75 seconds
)

// NewBackend generates the network parameters and creates a zec backend as a
// btc clone using an asset/btc helper function.
func NewBackend(configPath string, logger dex.Logger, network dex.Network) (asset.Backend, error) {
	var btcParams *chaincfg.Params
	var addrParams *dexzec.AddressParams
	switch network {
	case dex.Mainnet:
		btcParams = dexzec.MainNetParams
		addrParams = dexzec.MainNetAddressParams
	case dex.Testnet:
		btcParams = dexzec.TestNet4Params
		addrParams = dexzec.TestNet4AddressParams
	case dex.Regtest:
		btcParams = dexzec.RegressionNetParams
		addrParams = dexzec.RegressionNetAddressParams
	default:
		return nil, fmt.Errorf("unknown network ID %v", network)
	}

	// Designate the clone ports. These will be overwritten by any explicit
	// settings in the configuration file.
	ports := dexbtc.NetPorts{
		Mainnet: "8232",
		Testnet: "18232",
		Simnet:  "18232",
	}

	if configPath == "" {
		configPath = dexbtc.SystemConfigPath("zcash")
	}

	be, err := btc.NewBTCClone(&btc.BackendCloneConfig{
		Name:        assetName,
		Segwit:      false,
		ConfigPath:  configPath,
		Logger:      logger,
		Net:         network,
		ChainParams: btcParams,
		Ports:       ports,
		AddressDecoder: func(addr string, net *chaincfg.Params) (btcutil.Address, error) {
			return dexzec.DecodeAddress(addr, addrParams, btcParams)
		},
		InitTxSize:     dexzec.InitTxSize,
		InitTxSizeBase: dexzec.InitTxSizeBase,
		TxDeserializer: func(b []byte) (*wire.MsgTx, error) {
			zecTx, err := dexzec.DeserializeTx(b)
			if err != nil {
				return nil, err
			}
			return zecTx.MsgTx, nil
		},
		DumbFeeEstimates:     true,
		ManualMedianFee:      true,
		BlockFeeTransactions: blockFeeTransactions,
		NumericGetRawRPC:     true,
		ShieldedIO:           shieldedIO,
	})
	if err != nil {
		return nil, err
	}

	return &ZECBackend{
		Backend:    be,
		addrParams: addrParams,
		btcParams:  btcParams,
	}, nil
}

// ZECBackend embeds *btc.Backend and re-implements the Contract method to deal
// with ZCash address translation.
type ZECBackend struct {
	*btc.Backend
	btcParams  *chaincfg.Params
	addrParams *dexzec.AddressParams
}

// Contract returns the output from embedded Backend's Contract method, but
// with the SwapAddress field converted to ZCash encoding.
// TODO: Drop this in favor of an AddressEncoder field in the
// BackendCloneConfig.
func (be *ZECBackend) Contract(coinID []byte, redeemScript []byte) (*asset.Contract, error) { // Contract.SwapAddress
	contract, err := be.Backend.Contract(coinID, redeemScript)
	if err != nil {
		return nil, err
	}
	contract.SwapAddress, err = dexzec.RecodeAddress(contract.SwapAddress, be.addrParams, be.btcParams)
	if err != nil {
		return nil, err
	}
	return contract, nil
}

func blockFeeTransactions(rc *btc.RPCClient, blockHash *chainhash.Hash) (feeTxs []btc.FeeTx, prevBlock chainhash.Hash, err error) {
	blockB, err := rc.GetRawBlock(blockHash)
	if err != nil {
		return nil, chainhash.Hash{}, err
	}

	blk, err := dexzec.DeserializeBlock(blockB)
	if err != nil {
		return nil, chainhash.Hash{}, err
	}

	if len(blk.Transactions) == 0 {
		return nil, chainhash.Hash{}, fmt.Errorf("block %s has no transactions", blockHash)
	}

	feeTxs = make([]btc.FeeTx, 0, len(blk.Transactions)-1)
	for _, tx := range blk.Transactions[1:] { // skip coinbase
		feeTx, err := newFeeTx(tx)
		if err != nil {
			return nil, chainhash.Hash{}, fmt.Errorf("error parsing fee tx: %w", err)
		}
		feeTxs = append(feeTxs, feeTx)
	}

	return feeTxs, blk.Header.PrevBlock, nil
}

// feeTx implements FeeTx for manual median-fee calculations.
type feeTx struct {
	size           uint64
	prevOuts       []wire.OutPoint
	shieldedIn     uint64
	transparentOut uint64
	shieldedOut    uint64
}

var _ btc.FeeTx = (*feeTx)(nil)

func newFeeTx(zecTx *dexzec.Tx) (*feeTx, error) {
	var transparentOut uint64
	for _, out := range zecTx.TxOut {
		transparentOut += uint64(out.Value)
	}
	prevOuts := make([]wire.OutPoint, len(zecTx.TxOut))
	for _, in := range zecTx.TxIn {
		prevOuts = append(prevOuts, in.PreviousOutPoint)
	}
	var shieldedIn, shieldedOut uint64
	for _, js := range zecTx.VJoinSplit {
		shieldedIn += js.New
		shieldedOut += js.Old
	}
	if zecTx.ValueBalanceSapling > 0 {
		shieldedIn += uint64(zecTx.ValueBalanceSapling)
	} else if zecTx.ValueBalanceSapling < 0 {
		shieldedOut += uint64(-1 * zecTx.ValueBalanceSapling)
	}
	if zecTx.ValueBalanceOrchard > 0 {
		shieldedIn += uint64(zecTx.ValueBalanceOrchard)
	} else if zecTx.ValueBalanceOrchard < 0 {
		shieldedOut += uint64(-1 * zecTx.ValueBalanceOrchard)
	}

	return &feeTx{
		size:           zecTx.SerializeSize(),
		transparentOut: transparentOut,
		shieldedOut:    shieldedOut,
		shieldedIn:     shieldedIn,
		prevOuts:       prevOuts,
	}, nil
}

func (tx *feeTx) PrevOuts() []wire.OutPoint {
	return tx.prevOuts
}

func (tx *feeTx) FeeRate(prevOuts map[chainhash.Hash]map[int]int64) (uint64, error) {
	var transparentIn uint64
	for _, op := range tx.prevOuts {
		outs, found := prevOuts[op.Hash]
		if !found {
			return 0, fmt.Errorf("previous outpoint tx not found for %+v", op)
		}
		prevOutValue, found := outs[int(op.Index)]
		if !found {
			return 0, fmt.Errorf("previous outpoint vout not found for %+v", op)
		}
		transparentIn += uint64(prevOutValue)
	}
	in := tx.shieldedIn + transparentIn
	out := tx.shieldedOut + tx.transparentOut
	if out > in {
		return 0, fmt.Errorf("out > in. %d > %d", out, in)
	}
	return uint64(math.Round(float64(in-out) / float64(tx.size))), nil
}

func shieldedIO(tx *btc.VerboseTxExtended) (in, out uint64, err error) {
	txB, err := hex.DecodeString(tx.Hex)
	if err != nil {
		return 0, 0, fmt.Errorf("hex.DecodeString error: %w", err)
	}
	zecTx, err := dexzec.DeserializeTx(txB)
	if err != nil {
		return 0, 0, fmt.Errorf("DeserializeTx error: %w", err)
	}
	feeTx, err := newFeeTx(zecTx)
	if err != nil {
		return 0, 0, err
	}
	return feeTx.shieldedIn, feeTx.shieldedOut, nil
}
