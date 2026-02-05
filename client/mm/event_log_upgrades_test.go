package mm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/encode"
	"go.etcd.io/bbolt"
)

func TestEventLogV2Upgrade(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	// Create a v1 database with test data
	db1, err := bbolt.Open(dbPath, 0600, nil)
	if err != nil {
		t.Fatalf("Failed to create DB: %v", err)
	}
	err = db1.Update(func(tx *bbolt.Tx) error {
		botRuns, err := tx.CreateBucketIfNotExists(botRunsBucket)
		if err != nil {
			return err
		}
		if err := botRuns.Put(versionKey, encode.Uint32Bytes(1)); err != nil {
			return err
		}
		return setupV1EventLogData(tx)
	})
	if err != nil {
		t.Fatalf("Failed to setup test data: %v", err)
	}
	db1.Close()

	// Create new DB instance which should trigger upgrade
	ctx := t.Context()

	var wg sync.WaitGroup
	db2, err := newBoltEventLogDB(ctx, dbPath, &wg, dex.StdOutLogger("TEST", dex.LevelError))
	if err != nil {
		t.Fatalf("Failed to create DB for upgrade: %v", err)
	}

	// Verify upgrade worked
	version, err := db2.getVersion()
	if err != nil {
		t.Fatalf("Failed to get version after upgrade: %v", err)
	}
	if version != 2 {
		t.Fatalf("Expected version 2 after upgrade, got %d", version)
	}
	verifyV2EventLogUpgrade(t, db2.DB)
}

func setupV1EventLogData(tx *bbolt.Tx) error {
	botRuns := tx.Bucket(botRunsBucket)

	mkt := &MarketWithHost{
		BaseID:  42,
		QuoteID: 0,
		Host:    "dex.example.com",
	}

	startTime := time.Now().Unix()
	runBucket, err := botRuns.CreateBucket(runKey(startTime, mkt))
	if err != nil {
		return err
	}

	cfgsBucket, err := runBucket.CreateBucket(cfgsBucket)
	if err != nil {
		return err
	}

	cfg1 := &BotConfig{
		Host:    mkt.Host,
		BaseID:  mkt.BaseID,
		QuoteID: mkt.QuoteID,
		CEXName: "binance",
		// CEXBaseID and CEXQuoteID are missing
	}
	cfg1B, _ := json.Marshal(cfg1)
	cfgsBucket.Put(encode.Uint64Bytes(uint64(startTime)), versionedBytes(0).AddData(cfg1B))

	// Config 2: without CEXName (should not be updated)
	cfg2 := &BotConfig{
		Host:    mkt.Host,
		BaseID:  mkt.BaseID,
		QuoteID: mkt.QuoteID,
		CEXName: "", // empty, should not be updated
	}
	cfg2B, _ := json.Marshal(cfg2)
	cfgsBucket.Put(encode.Uint64Bytes(uint64(startTime+100)), versionedBytes(0).AddData(cfg2B))

	// Create events bucket with test events
	eventsBucket, err := runBucket.CreateBucket(eventsBucket)
	if err != nil {
		return err
	}

	// Event 1: Deposit event (should be updated)
	depositEvent := &MarketMakingEvent{
		ID:        1,
		TimeStamp: startTime,
		DepositEvent: &DepositEvent{
			DexAssetID: 42,
			// CEXAssetID is missing (old format)
			CEXCredit: 1000000,
		},
	}
	depositEventB, _ := json.Marshal(depositEvent)
	eventsBucket.Put(encode.Uint64Bytes(1), versionedBytes(0).AddData(depositEventB))

	// Event 2: Withdrawal event (should be updated)
	withdrawalEvent := &MarketMakingEvent{
		ID:        2,
		TimeStamp: startTime,
		WithdrawalEvent: &WithdrawalEvent{
			ID:         "withdraw-123",
			DEXAssetID: 0,
			// CEXAssetID is missing (old format)
			CEXDebit: 500000,
		},
	}
	withdrawalEventB, _ := json.Marshal(withdrawalEvent)
	eventsBucket.Put(encode.Uint64Bytes(2), versionedBytes(0).AddData(withdrawalEventB))

	// Event 3: DEX order event (should not be updated)
	dexOrderEvent := &MarketMakingEvent{
		ID:        3,
		TimeStamp: startTime,
		DEXOrderEvent: &DEXOrderEvent{
			ID:   "order-123",
			Rate: 50000000,
			Qty:  100000000,
			Sell: true,
		},
	}
	dexOrderEventB, _ := json.Marshal(dexOrderEvent)
	eventsBucket.Put(encode.Uint64Bytes(3), versionedBytes(0).AddData(dexOrderEventB))

	return nil
}

func verifyV2EventLogUpgrade(t *testing.T, db *bbolt.DB) {
	t.Helper()

	err := db.View(func(tx *bbolt.Tx) error {
		// Check version was updated
		version, err := getVersionTx(tx)
		if err != nil {
			return err
		}
		if version != 2 {
			t.Errorf("Expected version 2, got %d", version)
		}

		botRuns := tx.Bucket(botRunsBucket)
		if botRuns == nil {
			return fmt.Errorf("botRuns bucket not found")
		}

		// Find our test run bucket and its key
		var testRunBucket *bbolt.Bucket
		var testRunKey []byte
		botRuns.ForEachBucket(func(k []byte) error {
			if !bytes.Equal(k, versionKey) {
				testRunBucket = botRuns.Bucket(k)
				testRunKey = k
				return nil // stop iteration
			}
			return nil
		})

		if testRunBucket == nil {
			return fmt.Errorf("test run bucket not found")
		}

		// Parse run key to get expected market info
		_, mkt, err := parseRunKey(testRunKey)
		if err != nil {
			return err
		}

		// Verify configs were updated correctly
		cfgsBucket := testRunBucket.Bucket(cfgsBucket)
		if cfgsBucket == nil {
			return fmt.Errorf("cfgs bucket not found")
		}

		cfgCount := 0
		cfgsBucket.ForEach(func(k, v []byte) error {
			cfgCount++

			cfg, err := decodeBotConfig(v)
			if err != nil {
				t.Errorf("Error decoding config: %v", err)
				return nil
			}

			if cfg.CEXName != "" {
				// Config with CEXName should have been updated
				if cfg.CEXBaseID != mkt.BaseID {
					t.Errorf("Expected CEXBaseID %d, got %d", mkt.BaseID, cfg.CEXBaseID)
				}
				if cfg.CEXQuoteID != mkt.QuoteID {
					t.Errorf("Expected CEXQuoteID %d, got %d", mkt.QuoteID, cfg.CEXQuoteID)
				}
			} else {
				// Config without CEXName should not have been updated
				if cfg.CEXBaseID != 0 {
					t.Errorf("Expected CEXBaseID 0 for empty CEXName, got %d", cfg.CEXBaseID)
				}
				if cfg.CEXQuoteID != 0 {
					t.Errorf("Expected CEXQuoteID 0 for empty CEXName, got %d", cfg.CEXQuoteID)
				}
			}
			return nil
		})

		if cfgCount != 2 {
			t.Errorf("Expected 2 configs, found %d", cfgCount)
		}

		// Verify events were updated correctly
		eventsBucket := testRunBucket.Bucket(eventsBucket)
		if eventsBucket == nil {
			return fmt.Errorf("events bucket not found")
		}

		eventCount := 0
		eventsBucket.ForEach(func(k, v []byte) error {
			eventCount++

			event, err := decodeMarketMakingEvent(v)
			if err != nil {
				t.Errorf("Error decoding event: %v", err)
				return nil
			}

			if event.DepositEvent != nil {
				if event.DepositEvent.CEXAssetID != event.DepositEvent.DexAssetID {
					t.Errorf("Deposit event: Expected CEXAssetID %d, got %d",
						event.DepositEvent.DexAssetID, event.DepositEvent.CEXAssetID)
				}
			}

			if event.WithdrawalEvent != nil {
				if event.WithdrawalEvent.CEXAssetID != event.WithdrawalEvent.DEXAssetID {
					t.Errorf("Withdrawal event: Expected CEXAssetID %d, got %d",
						event.WithdrawalEvent.DEXAssetID, event.WithdrawalEvent.CEXAssetID)
				}
			}

			return nil
		})

		if eventCount != 3 {
			t.Errorf("Expected 3 events, found %d", eventCount)
		}

		return nil
	})

	if err != nil {
		t.Error(err)
	}
}
