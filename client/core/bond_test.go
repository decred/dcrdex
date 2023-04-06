package core

import (
	"bytes"
	"testing"
	"time"

	"decred.org/dcrdex/client/db"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/msgjson"
)

// TODO: test reusing pending update bonds
func TestRotateBonds(t *testing.T) {
	bondAmt := uint64(1e11)

	bondAssets := map[string]*msgjson.BondAsset{
		tUTXOAssetA.Symbol: {
			Version: 0,
			ID:      tUTXOAssetA.ID,
			Confs:   2,
			Amt:     bondAmt,
		},
		tUTXOAssetB.Symbol: {
			Version: 0,
			ID:      tUTXOAssetB.ID,
			Confs:   2,
			Amt:     bondAmt,
		},
	}

	now := uint64(time.Now().Unix())

	unsignedTx := encode.RandomBytes(20)
	signedTx := encode.RandomBytes(20)

	bondIDs := make([]dex.Bytes, 0, 6)
	for i := 0; i < 6; i++ {
		bondIDs = append(bondIDs, encode.RandomBytes(32))
	}

	newBond := func(assetID uint32, coinID dex.Bytes, lockTime uint64, amt uint64) *db.Bond {
		return &db.Bond{
			AssetID:  assetID,
			CoinID:   coinID,
			LockTime: lockTime,
			Amount:   amt,
		}
	}

	bondExpiry := uint64(60 * 60)
	lockDur := minBondLifetime(dex.Mainnet, int64(bondExpiry))
	lockTime := time.Now().Add(lockDur).Truncate(time.Second).Unix()

	weakBondThreshold := time.Now().Unix() + int64(bondExpiry) + pendingBuffer(dex.Mainnet)

	tests := []struct {
		name string

		// Dex config result
		bondAssets map[string]*msgjson.BondAsset
		bondExpiry uint64

		// DexConnection fields
		bonds         []*db.Bond
		pendingBonds  []*db.Bond
		expiredBonds  []*db.Bond
		tier          int64
		tierChange    int64
		targetTier    uint64
		maxBondedAmt  uint64
		totalReserved int64
		bondAsset     uint32

		// Server responses
		preValidateBond *msgjson.PreValidateBond

		// Test wallet fields
		useUpdaterWallet bool

		makeBondTxUnsignedTx dex.Bytes
		makeBondTxSignedTx   dex.Bytes
		makeBondTxCoinID     dex.Bytes

		updateBondTxUnsignedTx dex.Bytes
		updateBondTxSignedTx   dex.Bytes
		updateBondTxCoinIDs    []dex.Bytes
		updateBondTxChange     uint64

		expectMakeBondTxAmt   uint64
		expectUpdateBondTxAmt uint64
		expectBondsToUpdate   []dex.Bytes
		expectSentTransaction dex.Bytes
		expectExpiredBonds    []*db.Bond
		expectPendingBonds    []*db.Bond
		expectBonds           []*db.Bond
		expectRefundedBondID  dex.Bytes
	}{
		{
			name: "ok",

			bondAssets: bondAssets,
			bondExpiry: bondExpiry,

			tier:       2,
			targetTier: 4,
			bondAsset:  tUTXOAssetA.ID,

			expiredBonds: []*db.Bond{
				newBond(tUTXOAssetA.ID, bondIDs[0], now-10, bondAmt),
			},

			useUpdaterWallet: false,

			makeBondTxUnsignedTx: unsignedTx,
			makeBondTxSignedTx:   signedTx,
			makeBondTxCoinID:     bondIDs[1],

			expectMakeBondTxAmt:   2 * bondAmt,
			expectSentTransaction: signedTx,
			expectRefundedBondID:  bondIDs[0],
			expectPendingBonds: []*db.Bond{
				newBond(tUTXOAssetA.ID, bondIDs[1], 0, 2*bondAmt),
			},
		},
		{
			name: "updater, but no bonds to update",

			bondAssets: bondAssets,
			bondExpiry: bondExpiry,

			tier:       2,
			targetTier: 4,
			bondAsset:  tUTXOAssetB.ID,

			expiredBonds: []*db.Bond{},

			useUpdaterWallet: true,

			makeBondTxUnsignedTx: unsignedTx,
			makeBondTxSignedTx:   signedTx,
			makeBondTxCoinID:     bondIDs[0],

			expectPendingBonds: []*db.Bond{
				newBond(tUTXOAssetB.ID, bondIDs[0], 0, 2*bondAmt),
			},

			expectMakeBondTxAmt:   2 * bondAmt,
			expectSentTransaction: signedTx,
		},
		{
			name: "updater, 1 expired bond",

			bondAssets: bondAssets,
			bondExpiry: bondExpiry,

			tier:       2,
			targetTier: 4,
			bondAsset:  tUTXOAssetB.ID,

			expiredBonds: []*db.Bond{
				newBond(tUTXOAssetB.ID, bondIDs[0], now-10, bondAmt),
			},

			useUpdaterWallet: true,

			updateBondTxUnsignedTx: unsignedTx,
			updateBondTxSignedTx:   signedTx,
			updateBondTxCoinIDs:    []dex.Bytes{bondIDs[1]},

			expectUpdateBondTxAmt: 2 * bondAmt,
			expectBondsToUpdate: []dex.Bytes{
				bondIDs[0],
			},
			expectSentTransaction: signedTx,

			expectExpiredBonds: []*db.Bond{},
			expectPendingBonds: []*db.Bond{
				newBond(tUTXOAssetB.ID, bondIDs[1], 0, 2*bondAmt),
			},
		},
		{
			name: "updater, expired bonds that more than cover deficit and one strong bond",

			bondAssets: bondAssets,
			bondExpiry: bondExpiry,

			tier:       2,
			targetTier: 5,
			bondAsset:  tUTXOAssetB.ID,

			expiredBonds: []*db.Bond{
				newBond(tUTXOAssetB.ID, bondIDs[0], now-20, 2*bondAmt),
				newBond(tUTXOAssetB.ID, bondIDs[1], now-10, 2*bondAmt),
			},

			bonds: []*db.Bond{
				newBond(tUTXOAssetB.ID, bondIDs[2], uint64(lockTime-20), 3*bondAmt),
			},

			useUpdaterWallet: true,

			updateBondTxUnsignedTx: unsignedTx,
			updateBondTxSignedTx:   signedTx,
			updateBondTxCoinIDs:    []dex.Bytes{bondIDs[3], bondIDs[4]},
			updateBondTxChange:     bondAmt,

			expectUpdateBondTxAmt: 6 * bondAmt,
			expectSentTransaction: signedTx,
			expectBondsToUpdate: []dex.Bytes{
				bondIDs[2], bondIDs[1], bondIDs[0],
			},

			expectExpiredBonds: []*db.Bond{},
			expectPendingBonds: []*db.Bond{
				newBond(tUTXOAssetB.ID, bondIDs[3], 0, bondAmt),
				newBond(tUTXOAssetB.ID, bondIDs[4], 0, 6*bondAmt),
			},
		},
		{
			name: "updater, one expired and one weak bonds that more than cover deficit and one strong bond",

			bondAssets: bondAssets,
			bondExpiry: bondExpiry,

			tier:       3,
			targetTier: 6,
			bondAsset:  tUTXOAssetB.ID,

			expiredBonds: []*db.Bond{
				newBond(tUTXOAssetB.ID, bondIDs[0], now-20, 2*bondAmt),
				newBond(tUTXOAssetB.ID, bondIDs[1], now-10, 3*bondAmt),
			},

			bonds: []*db.Bond{
				newBond(tUTXOAssetB.ID, bondIDs[2], uint64(weakBondThreshold-20), 2*bondAmt),
				newBond(tUTXOAssetB.ID, bondIDs[3], uint64(weakBondThreshold+20), 3*bondAmt),
			},

			useUpdaterWallet: true,

			updateBondTxUnsignedTx: unsignedTx,
			updateBondTxSignedTx:   signedTx,
			updateBondTxCoinIDs:    []dex.Bytes{bondIDs[4]},
			updateBondTxChange:     bondAmt,

			expectUpdateBondTxAmt: 8 * bondAmt,
			expectSentTransaction: signedTx,
			expectBondsToUpdate: []dex.Bytes{
				bondIDs[3], bondIDs[2], bondIDs[1],
			},

			expectRefundedBondID: bondIDs[0],

			expectExpiredBonds: []*db.Bond{},
			expectPendingBonds: []*db.Bond{
				newBond(tUTXOAssetB.ID, bondIDs[4], 0, 8*bondAmt),
			},
		},
		{
			name: "updater, one expired, one weak bond, one strong bond, need to post additional",

			bondAssets: bondAssets,
			bondExpiry: bondExpiry,

			tier:       3,
			targetTier: 6,
			bondAsset:  tUTXOAssetB.ID,

			expiredBonds: []*db.Bond{
				newBond(tUTXOAssetB.ID, bondIDs[0], now-20, 2*bondAmt),
			},

			bonds: []*db.Bond{
				newBond(tUTXOAssetB.ID, bondIDs[1], uint64(weakBondThreshold-20), 2*bondAmt),
				newBond(tUTXOAssetB.ID, bondIDs[2], uint64(weakBondThreshold+20), 3*bondAmt),
				newBond(tUTXOAssetB.ID, bondIDs[3], uint64(lockTime+30), 3*bondAmt), // too strong to include in update
			},

			useUpdaterWallet: true,

			updateBondTxUnsignedTx: unsignedTx,
			updateBondTxSignedTx:   signedTx,
			updateBondTxCoinIDs:    []dex.Bytes{bondIDs[4]},
			updateBondTxChange:     bondAmt,

			expectUpdateBondTxAmt: 8 * bondAmt,
			expectSentTransaction: signedTx,
			expectBondsToUpdate: []dex.Bytes{
				bondIDs[2], bondIDs[1], bondIDs[0],
			},

			expectExpiredBonds: []*db.Bond{},
			expectPendingBonds: []*db.Bond{
				newBond(tUTXOAssetB.ID, bondIDs[4], 0, 8*bondAmt),
			},
			expectBonds: []*db.Bond{
				newBond(tUTXOAssetB.ID, bondIDs[3], uint64(lockTime+30), 3*bondAmt),
			},
		},
	}

	for _, test := range tests {
		rig := newTestRig()
		defer rig.shutdown()
		tCore := rig.core

		dcrWallet, tDCRWallet := newTWallet(tUTXOAssetA.ID)
		tCore.wallets[tUTXOAssetA.ID] = dcrWallet
		btcWallet, tBTCWallet := newTUpdateBonder(tUTXOAssetB.ID)
		tCore.wallets[tUTXOAssetB.ID] = btcWallet

		tDCRWallet.contractExpired = true
		tBTCWallet.contractExpired = true
		tDCRWallet.refundBondCoin = &tCoin{id: encode.RandomBytes(32), val: 1e11}
		tBTCWallet.refundBondCoin = &tCoin{id: encode.RandomBytes(32), val: 1e11}

		err := rig.core.Login(tPW)
		if err != nil {
			t.Fatalf("initial Login error: %v", err)
		}

		dc := rig.core.conns[tDexHost]

		dc.cfg = &msgjson.ConfigResult{
			BondAssets: test.bondAssets,
			BondExpiry: test.bondExpiry,
		}

		if test.expiredBonds == nil {
			dc.acct.expiredBonds = []*db.Bond{}
		} else {
			dc.acct.expiredBonds = test.expiredBonds
		}

		if test.pendingBonds == nil {
			dc.acct.pendingBonds = []*db.Bond{}
		} else {
			dc.acct.pendingBonds = test.pendingBonds
		}

		if test.bonds == nil {
			dc.acct.bonds = []*db.Bond{}
		} else {
			dc.acct.bonds = test.bonds
		}

		dc.acct.tier = test.tier
		dc.acct.tierChange = test.tierChange
		dc.acct.targetTier = test.targetTier
		dc.acct.bondAsset = test.bondAsset

		var wallet *TXCWallet
		var updateWallet *TUpdateBonder
		if test.useUpdaterWallet {
			updateWallet = tBTCWallet
			wallet = updateWallet.TXCWallet
		} else {
			wallet = tDCRWallet
		}

		wallet.makeBondTxSignedTx = test.makeBondTxSignedTx
		wallet.makeBondTxUnsignedTx = test.makeBondTxUnsignedTx
		wallet.makeBondTxCoinID = test.makeBondTxCoinID

		if test.expectUpdateBondTxAmt > 0 {
			rig.queuePreValidateBond(test.expectUpdateBondTxAmt, unsignedTx)
		} else {
			rig.queuePreValidateBond(test.expectMakeBondTxAmt, unsignedTx)
		}

		if updateWallet != nil {
			updateWallet.updateBondTxSignedTx = test.updateBondTxSignedTx
			updateWallet.updateBondTxUnsignedTx = test.updateBondTxUnsignedTx
			updateWallet.updateBondTxCoinIDs = test.updateBondTxCoinIDs
			updateWallet.updateBondTxChange = test.updateBondTxChange
		}

		rig.core.rotateBonds(rig.core.ctx)

		if !bytes.Equal(test.expectSentTransaction, wallet.sentRawTransaction) {
			t.Fatalf("%s: expected sent transaction %x, got %x", test.name, test.expectSentTransaction, wallet.sentRawTransaction)
		}

		if test.expectMakeBondTxAmt != wallet.makeBondTxAmtParam {
			t.Fatalf("%s: expected makeBondTxAmt %d, got %d", test.name, test.expectMakeBondTxAmt, tDCRWallet.makeBondTxAmtParam)
		}

		if updateWallet != nil {
			if test.expectUpdateBondTxAmt != updateWallet.updateBondTxAmtParam {
				t.Fatalf("%s: expected updateBondTxAmt %d, got %d", test.name, test.expectUpdateBondTxAmt, updateWallet.updateBondTxAmtParam)
			}

			// Ensure bonds to update are correct
			if len(test.expectBondsToUpdate) != len(updateWallet.updateBondBondsToUpdateParam) {
				t.Fatalf("%s: expected %d bonds to update, got %d", test.name, len(test.expectBondsToUpdate), len(updateWallet.updateBondBondsToUpdateParam))
			}
			for i, bond := range test.expectBondsToUpdate {
				if !bytes.Equal(bond, updateWallet.updateBondBondsToUpdateParam[i]) {
					t.Fatalf("%s: expected bond to update %x, got %x", test.name, bond, updateWallet.updateBondBondsToUpdateParam[i])
				}
			}
		}

		// Check that length and contents of expired bonds are the same
		if len(test.expectExpiredBonds) != len(dc.acct.expiredBonds) {
			t.Fatalf("%s: expected %d expired bonds, got %d", test.name, len(test.expectExpiredBonds), len(dc.acct.expiredBonds))
		}
		for i, bond := range test.expectExpiredBonds {
			if bond.Amount != dc.acct.expiredBonds[i].Amount {
				t.Fatalf("%s: expected expired bond amount %d, got %d", test.name, bond.Amount, dc.acct.expiredBonds[i].Amount)
			}
			if bond.AssetID != dc.acct.expiredBonds[i].AssetID {
				t.Fatalf("%s: expected expired bond assetID %v, got %v", test.name, bond.AssetID, dc.acct.expiredBonds[i].AssetID)
			}
			if !bytes.Equal(bond.CoinID, dc.acct.expiredBonds[i].CoinID) {
				t.Fatalf("%s: expected expired bond coinID %x, got %x", test.name, bond.CoinID, dc.acct.expiredBonds[i].CoinID)
			}

		}

		// Check that length and contents of pending bonds are the same
		if len(test.expectPendingBonds) != len(dc.acct.pendingBonds) {
			t.Fatalf("%s: expected %d pending bonds, got %d", test.name, len(test.expectPendingBonds), len(dc.acct.pendingBonds))
		}
		for i, bond := range test.expectPendingBonds {
			if bond.Amount != dc.acct.pendingBonds[i].Amount {
				t.Fatalf("%s: expected pending bond amount %d, got %d", test.name, bond.Amount, dc.acct.pendingBonds[i].Amount)
			}
			if bond.AssetID != dc.acct.pendingBonds[i].AssetID {
				t.Fatalf("%s: expected pending bond assetID %v, got %v", test.name, bond.AssetID, dc.acct.pendingBonds[i].AssetID)
			}
			if !bytes.Equal(bond.CoinID, dc.acct.pendingBonds[i].CoinID) {
				t.Fatalf("%s: expected pending bond coinID %x, got %x", test.name, bond.CoinID, dc.acct.pendingBonds[i].CoinID)
			}
		}

		// Check that length and contents of bonds are the same
		if len(test.expectBonds) != len(dc.acct.bonds) {
			t.Fatalf("%s: expected %d bonds, got %d", test.name, len(test.expectBonds), len(dc.acct.bonds))
		}
		for i, bond := range test.expectBonds {
			if bond.Amount != dc.acct.bonds[i].Amount {
				t.Fatalf("%s: expected bond amount %d, got %d", test.name, bond.Amount, dc.acct.bonds[i].Amount)
			}
			if bond.AssetID != dc.acct.bonds[i].AssetID {
				t.Fatalf("%s: expected bond assetID %v, got %v", test.name, bond.AssetID, dc.acct.bonds[i].AssetID)
			}
			if !bytes.Equal(bond.CoinID, dc.acct.bonds[i].CoinID) {
				t.Fatalf("%s: expected bond coinID %x, got %x", test.name, bond.CoinID, dc.acct.bonds[i].CoinID)
			}
		}

		if !bytes.Equal(test.expectRefundedBondID, wallet.refundBondCoinParam) {
			t.Fatalf("%s: expected refunded bond coinID %x, got %x", test.name, test.expectRefundedBondID, wallet.refundBondCoinParam)
		}
	}
}
