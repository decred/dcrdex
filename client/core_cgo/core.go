//go:build cgo

// A pacakge that exports Core functionalities as go code
// that can be compiled into a c-shared libary using
// go build -tags cgo -buildmode=c-shared -o ./client/cmd/dexcapp/lib/libcore/libcore.so ./client/core_cgo

package main

import "C"

import (
	"context"
	"errors"
	"path/filepath"

	_ "decred.org/dcrdex/client/asset/bch"  // register bch asset
	_ "decred.org/dcrdex/client/asset/btc"  // register btc asset
	_ "decred.org/dcrdex/client/asset/dcr"  // register dcr asset
	_ "decred.org/dcrdex/client/asset/doge" // register doge asset
	_ "decred.org/dcrdex/client/asset/ltc"  // register ltc asset

	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/dex"
	"github.com/decred/dcrd/dcrutil/v4"
)

var (
	defaultAppDataDir = dcrutil.AppDataDir("dexc", false)

	coreInstance  *core.Core
	cancelCoreCtx context.CancelFunc
)

//export startCore
func startCore(dataDir, net *C.char) *C.char {
	network, err := dex.NetFromString(C.GoString(net))
	if err != nil {
		return C.CString(err.Error())
	}

	appDataDir := C.GoString(dataDir)
	if appDataDir == "" {
		appDataDir = defaultAppDataDir
	}

	logDirectory := filepath.Join(appDataDir, network.String(), "logs")
	dbPath := filepath.Join(appDataDir, network.String(), "dexc.db")

	logMaker, err := initLogging(logDirectory, "debug", true)
	if err != nil {
		return C.CString(err.Error())
	}

	coreInstance, err = core.New(&core.Config{
		DBPath: dbPath,
		Net:    network,
		Logger: logMaker.Logger("CORE"),
	})
	if err != nil {
		return C.CString(err.Error())
	}

	return C.CString("")
}

//export run
func run() {
	var ctx context.Context
	ctx, cancelCoreCtx = context.WithCancel(context.Background())
	coreInstance.Run(ctx)
}

//export waitTillReady
func waitTillReady() {
	<-coreInstance.Ready()
}

//export kill
func kill() *C.char {
	err := coreInstance.Logout()
	if err == nil || errors.Is(err, core.ActiveOrdersLogoutErr) {
		// TODO: Don't just ignore ActiveOrdersLogoutErr.
		cancelCoreCtx()
		return C.CString("")
	}
	return C.CString(err.Error())
}

//export isInitialized
func isInitialized() bool {
	return coreInstance.IsInitialized()
}

func main() {}
