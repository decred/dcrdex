// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package firo

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"time"

	"decred.org/dcrdex/dex"
	dexbtc "decred.org/dcrdex/dex/networks/btc"
	dexfiro "decred.org/dcrdex/dex/networks/firo"
	"decred.org/dcrdex/server/asset"
	"decred.org/dcrdex/server/asset/btc"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
)

// Driver implements asset.Driver.
type Driver struct{}

// Setup creates the DGB backend. Start the backend with its Run method.
func (d *Driver) Setup(cfg *asset.BackendConfig) (asset.Backend, error) {
	return NewBackend(cfg)
}

// DecodeCoinID creates a human-readable representation of a coin ID for
// DigiByte.
func (d *Driver) DecodeCoinID(coinID []byte) (string, error) {
	// Digibyte and Bitcoin have the same tx hash and output format.
	return (&btc.Driver{}).DecodeCoinID(coinID)
}

// Version returns the Backend implementation's version number.
func (d *Driver) Version() uint32 {
	return version
}

// UnitInfo returns the dex.UnitInfo for the asset.
func (d *Driver) UnitInfo() dex.UnitInfo {
	return dexfiro.UnitInfo
}

// MinBondSize calculates the minimum bond size for a given fee rate that avoids
// dust outputs on the bond and refund txs, assuming the maxFeeRate doesn't
// change.
func (d *Driver) MinBondSize(maxFeeRate uint64) uint64 {
	return dexbtc.MinBondSize(maxFeeRate, false)
}

// MinLotSize calculates the minimum bond size for a given fee rate that avoids
// dust outputs on the swap and refund txs, assuming the maxFeeRate doesn't
// change.
func (d *Driver) MinLotSize(maxFeeRate uint64) uint64 {
	return dexbtc.MinLotSize(maxFeeRate, false)
}

// Name is the asset's name.
func (d *Driver) Name() string {
	return "Firo"
}

func init() {
	asset.Register(BipID, &Driver{})
}

const (
	version   = 0
	BipID     = 136 // Zcoin XZC
	assetName = "firo"
)

// NewBackend generates the network parameters and creates a dgb backend as a
// btc clone using an asset/btc helper function.
func NewBackend(cfg *asset.BackendConfig) (asset.Backend, error) {
	var params *chaincfg.Params
	switch cfg.Net {
	case dex.Mainnet:
		params = dexfiro.MainNetParams
	case dex.Testnet:
		params = dexfiro.TestNetParams
	case dex.Regtest:
		params = dexfiro.RegressionNetParams
	default:
		return nil, fmt.Errorf("unknown network ID %v", cfg.Net)
	}

	// Designate the clone ports.
	ports := dexbtc.NetPorts{
		Mainnet: "8888",
		Testnet: "18888",
		Simnet:  "28888",
	}

	configPath := cfg.ConfigPath
	if configPath == "" {
		configPath = dexbtc.SystemConfigPath("firo")
	}

	return btc.NewBTCClone(&btc.BackendCloneConfig{
		Name:        assetName,
		Segwit:      false,
		ConfigPath:  configPath,
		Logger:      cfg.Logger,
		Net:         cfg.Net,
		ChainParams: params,
		Ports:       ports,
		// 2 blocks should be enough - Firo has masternode 1 block finalize
		// confirms with Instasend (see also: Dash instasend)
		// Also 'estimatefee 2' usually returns 0.00001000
		FeeConfs: 2,
		// Firo mainnet blocks are rarely full so this should be the most
		// common fee rate .. as can be seen from looking at the explorer
		NoCompetitionFeeRate: 1,    // 1 Sats/B
		ManualMedianFee:      true, // no getblockstats
		// BlockFeeTransactions: testnet never returns a result for estimatefee,
		// apparently, and we don't have getblockstats, so we need to scan
		// blocks on testnet.
		BlockFeeTransactions: func(rc *btc.RPCClient, blockHash *chainhash.Hash) ([]btc.FeeTx, chainhash.Hash, error) {
			return btcBlockFeeTransactions(rc, blockHash, cfg.Net)
		},
		MaxFeeBlocks:       16, // failsafe
		BooleanGetBlockRPC: true,
		// DumbFeeEstimates: Firo actually has estimatesmartfee, but with a big
		// warning
		// 'WARNING: This interface is unstable and may disappear or change!'
		// It also doesn't accept an estimate_mode argument.
		// Neither estimatesmartfee or estimatefee work on testnet v0.14.12.4,
		// but estimatefee works on simnet, and on mainnet v0.14.12.1.
		DumbFeeEstimates: true,
		RelayAddr:        cfg.RelayAddr,
	})
}

// Firo v0.14.12.1 defaults:
// -fallbackfee= (default: 20000) 	wallet.h: DEFAULT_FALLBACK_FEE unused afaics
// -mintxfee= (default: 1000)  		for tx creation
// -maxtxfee= (default: 1000000000) 10 FIRO .. also looks unused
// -minrelaytxfee= (default: 1000) 	0.00001 firo,
// -blockmintxfee= (default: 1000)

type FiroBlock struct {
	wire.MsgBlock
	// ProgPOW
	Height  uint32
	Nonce64 uint64
	MixHash chainhash.Hash
	// MTP
	NVersionMTP  int32
	MTPHashValue chainhash.Hash
	Reserved     [2]chainhash.Hash
	HashRootMTP  [16]byte
	// Discarding MTP proofs

	mtpTime     uint32 // Params::nMTPSwitchTime
	progPowTime uint32 // Params::nPPSwitchTime
}

func NewFiroBlock(net dex.Network) *FiroBlock {
	switch net {
	case dex.Mainnet:
		return &FiroBlock{mtpTime: 1544443200, progPowTime: 1635228000} // block 419_269
	case dex.Testnet:
		return &FiroBlock{mtpTime: 1539172800, progPowTime: 1630069200} // block 37_310
	default:
		return &FiroBlock{mtpTime: math.MaxUint32, progPowTime: math.MaxUint32}
	}
}

// BlockFeeTransactions is for the manual median-fee backup method when
// estimatefee doesn't return a result. This may only be a problem on testnet
// (v0.14.12.4). In testing, estimatefee returns non-negative and reasonable
// values on both mainnet (v0.14.12.1) and simnet (v0.14.12.4).
func btcBlockFeeTransactions(rc *btc.RPCClient, blockHash *chainhash.Hash, net dex.Network) (feeTxs []btc.FeeTx, prevBlock chainhash.Hash, err error) {
	blockB, err := rc.GetRawBlock(blockHash)
	if err != nil {
		return nil, chainhash.Hash{}, fmt.Errorf("GetRawBlock error: %w", err)
	}

	firoBlock, err := deserializeFiroBlock(blockB, net)
	if err != nil {
		return nil, chainhash.Hash{}, fmt.Errorf("deserializeFiroBlock error for block %q: %w", blockHash, err)
	}

	if len(firoBlock.Transactions) == 0 {
		return nil, chainhash.Hash{}, fmt.Errorf("no transactions?")
	}

	feeTxs = make([]btc.FeeTx, len(firoBlock.Transactions)-1)
	for i, msgTx := range firoBlock.Transactions[1:] { // skip coinbase
		feeTxs[i] = &btc.BTCFeeTx{MsgTx: msgTx}
	}
	return feeTxs, firoBlock.Header.PrevBlock, nil
}

func deserializeFiroBlock(b []byte, net dex.Network) (*FiroBlock, error) {
	r := bytes.NewReader(b)
	blk := NewFiroBlock(net)
	if err := blk.readBlockHeader(r); err != nil {
		return nil, fmt.Errorf("readBlockHeader error: %v", err)
	}

	const pver = 0

	txCount, err := wire.ReadVarInt(r, pver)
	if err != nil {
		return nil, err
	}

	blk.Transactions = make([]*wire.MsgTx, 0, txCount)
	for i := uint64(0); i < txCount; i++ {
		tx := new(wire.MsgTx)

		if err := tx.BtcDecode(r, pver, wire.BaseEncoding); err != nil {
			return nil, fmt.Errorf("error decoding tx at index %d: %w", i, err)
		}

		// https://github.com/firoorg/firo/blob/26da759ee79b1f65fad42beda0b614bb9c2ab756/src/primitives/transaction.h#L332
		// nVersion := tx.Version & 0xffff
		nType := (tx.Version >> 16) & 0xffff
		const txTypeNormal = 0
		if nType != txTypeNormal {
			// vExtraPayload
			sz, err := wire.ReadVarInt(r, pver)
			if err != nil {
				return nil, fmt.Errorf("ReadVarInt error: %v", err)
			}
			if _, err = io.CopyN(io.Discard, r, int64(sz)); err != nil {
				return nil, fmt.Errorf("error discarding vExtraPayload: %v", err)
			}
		}

		blk.Transactions = append(blk.Transactions, tx)
	}

	return blk, nil

}

// https://github.com/firoorg/firo/blob/26da759ee79b1f65fad42beda0b614bb9c2ab756/src/primitives/block.h#L157-L194
func (blk *FiroBlock) readBlockHeader(r io.Reader) error {
	hdr := &blk.Header

	nVersion, err := readUint32(r)
	if err != nil {
		return err
	}
	hdr.Version = int32(nVersion)

	if _, err = io.ReadFull(r, hdr.PrevBlock[:]); err != nil {
		return err
	}

	if _, err := io.ReadFull(r, hdr.MerkleRoot[:]); err != nil {
		return err
	}

	nTime, err := readUint32(r)
	if err != nil {
		return err
	}
	hdr.Timestamp = time.Unix(int64(nTime), 0)

	if hdr.Bits, err = readUint32(r); err != nil {
		return err
	}

	if nTime >= blk.progPowTime {
		if blk.Height, err = readUint32(r); err != nil {
			return err
		}

		if blk.Nonce64, err = readUint64(r); err != nil {
			return err
		}

		if _, err = io.ReadFull(r, blk.MixHash[:]); err != nil {
			return err
		}
	} else {
		if hdr.Nonce, err = readUint32(r); err != nil {
			return err
		}
		if nTime >= blk.mtpTime {
			nVersionMTP, err := readUint32(r)
			if err != nil {
				return err
			}
			blk.NVersionMTP = int32(nVersionMTP)

			if _, err := io.ReadFull(r, blk.MTPHashValue[:]); err != nil {
				return err
			}

			if _, err := io.ReadFull(r, blk.Reserved[0][:]); err != nil {
				return err
			}

			if _, err := io.ReadFull(r, blk.Reserved[1][:]); err != nil {
				return err
			}

			if _, err := io.ReadFull(r, blk.HashRootMTP[:]); err != nil {
				return err
			}

			if blk.HashRootMTP != [16]byte{} {
				// I have scanned the pre-ProgPOW sections of both testnet
				// and mainnet and have yet to find non-zero HashRootMTP. I am
				// speculating that the MTP proof data was purged in an upgrade
				// some time after the switch to ProgPOW.
				// This entire block could maybe be deleted, and is untested.
				// Based on https://github.com/firoorg/firo/blob/26da759ee79b1f65fad42beda0b614bb9c2ab756/src/primitives/block.h#L94-L112
				const mtpL = 64
				for i := 0; i < mtpL*3; i++ {
					var numberOfProofBlocksArr [1]byte
					if _, err := io.ReadFull(r, numberOfProofBlocksArr[:]); err != nil {
						return err
					}
					numberOfProofBlocks := int64(numberOfProofBlocksArr[0])
					proofSize := 16 * numberOfProofBlocks
					if _, err = io.CopyN(io.Discard, r, proofSize); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

// readUint32 reads a little-endian encoded uint32 from the Reader.
func readUint32(r io.Reader) (uint32, error) {
	b := make([]byte, 4)
	if _, err := io.ReadFull(r, b); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(b), nil
}

// readUint64 reads a little-endian encoded uint64 from the Reader.
func readUint64(r io.Reader) (uint64, error) {
	b := make([]byte, 8)
	if _, err := io.ReadFull(r, b); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint64(b), nil
}
