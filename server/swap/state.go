// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package swap

import (
	"encoding/gob"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"

	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/order"
)

const stateBinaryVersion byte = 1

// State contains the Swapper's state data, which is used to restart the Swapper
// without interrupting active swaps or waiting for long duration operations to
// complete before stopping.
type State struct {
	// Version is State format's binary version. This field should be present in
	// any revision of the State struct.
	Version byte
	// Assets lists the IDs of the assets required to fully restore the the data
	// for all of the active matches.
	Assets []uint32
	// Swapper.matches
	MatchTrackers map[order.MatchID]*matchTrackerData
	// Swapper.orders.orderMatches
	OrderMatches map[order.OrderID]*orderSwapStat
	// Swapper.liveWaiters
	LiveWaiters map[waiterKey]*handlerArgs
}

// BackupFile is a located swap state file path and it's millisecond time stamp.
type BackupFile struct {
	// Name is the full path to the backup file.
	Name string
	// Stamp is the millisecond unix epoch time stamp of the state.
	Stamp int64
}

// LatestStateFile attempts to locate the swap state file with the most recent
// time stamp (in the file name) from the given directory. The *BackupFile
// return will be nil if the directory was read successfully but no matching
// files were found.
func LatestStateFile(dir string) (*BackupFile, error) {
	cwd, err := os.Open(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to Open %q: %w", dir, err)
	}
	defer cwd.Close()

	//files, err := ioutil.ReadDir(".")
	fileNames, err := cwd.Readdirnames(0)
	if err != nil {
		return nil, fmt.Errorf("failed to Readdirnames: %w", err)
	}

	var backupFiles []BackupFile
	ssMatcher := regexp.MustCompile(`^swapState-(\d*).gob$`)
	for i := range fileNames {
		subMatches := ssMatcher.FindStringSubmatch(fileNames[i])
		if len(subMatches) != 2 {
			continue
		}
		stamp, err := strconv.ParseInt(subMatches[1], 10, 64)
		if err != nil {
			log.Error(err)
			continue
		}
		fullFile := filepath.Join(dir, fileNames[i])
		backupFiles = append(backupFiles, BackupFile{fullFile, stamp})
	}

	sort.Slice(backupFiles, func(i, j int) bool {
		// descending, latest first
		return backupFiles[i].Stamp > backupFiles[j].Stamp
	})

	var bup *BackupFile
	if len(backupFiles) > 0 {
		bup = new(BackupFile)
		*bup = backupFiles[0]
	}
	return bup, nil
}

// LoadStateFile attempts to load and decode a swap state file.
func LoadStateFile(fileName string) (*State, error) {
	file, err := os.Open(fileName)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	dec := gob.NewDecoder(file)
	var state State
	err = dec.Decode(&state)
	if err != nil {
		return nil, err
	}
	// TODO: check State.Version vs. stateBinaryVersion and deal with differences
	// if state.Version != stateBinaryVersion {
	// 	return fmt.Errorf("Cannot load swap state with version %d, expected %d",
	// 		state.Version, stateBinaryVersion)
	// }
	return &state, nil
}

func init() {
	// Register the concrete types implementing the order.Order interface.
	gob.Register(&order.CancelOrder{})
	gob.Register(&order.MarketOrder{})
	gob.Register(&order.LimitOrder{})

	// Register the concrete types implementing the msgjson.Signable interface.
	gob.Register(&msgjson.Match{})
	gob.Register(&msgjson.Audit{})
	gob.Register(&msgjson.Redemption{})
	gob.Register(&msgjson.Redeem{})
	gob.Register(&msgjson.RevokeMatch{})
}

type matchTrackerData struct {
	Match       *order.Match
	Time        int64
	MakerStatus *swapStatusData
	TakerStatus *swapStatusData
}

func (mt *matchTracker) Data() *matchTrackerData {
	return &matchTrackerData{
		Match:       mt.Match,
		Time:        encode.UnixMilli(mt.time),
		MakerStatus: mt.makerStatus.Data(),
		TakerStatus: mt.takerStatus.Data(),
	}
}

type swapStatusData struct {
	SwapAsset       uint32
	RedeemAsset     uint32
	SwapTime        int64
	ContractCoinOut []byte
	ContractScript  []byte
	SwapConfirmTime int64
	RedeemTime      int64
	RedeemCoinIn    []byte
}

func (ss *swapStatus) Data() *swapStatusData {
	ssd := &swapStatusData{
		SwapAsset:   ss.swapAsset,
		RedeemAsset: ss.redeemAsset,
	}
	if ss.swap != nil {
		ssd.SwapTime = encode.UnixMilli(ss.swapTime)
		ssd.ContractCoinOut = ss.swap.ID()
		ssd.ContractScript = ss.swap.RedeemScript()
	}
	if !ss.swapConfirmed.IsZero() {
		ssd.SwapConfirmTime = encode.UnixMilli(ss.swapConfirmed)
	}
	if ss.redemption != nil {
		ssd.RedeemTime = encode.UnixMilli(ss.redeemTime)
		ssd.RedeemCoinIn = ss.redemption.ID()
	}
	return ssd
}
