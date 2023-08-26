package main

/*
 * Starts an http server that responds with a hardcoded result to the binance API's
 * "/sapi/v1/capital/config/getall" endpoint. Binance's testnet does not support the
 * "sapi" endpoints, and this is the only "sapi" endpoint that we use.
 */

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"decred.org/dcrdex/client/websocket"
	"decred.org/dcrdex/dex"
)

const (
	pongWait   = 60 * time.Second
	pingPeriod = (pongWait * 9) / 10
)

var (
	log = dex.StdOutLogger("TBNC", dex.LevelDebug)
)

func main() {
	if err := mainErr(); err != nil {
		fmt.Fprint(os.Stderr, err, "\n")
		os.Exit(1)
	}
	os.Exit(0)
}

func mainErr() error {
	f := &fakeBinance{
		wsServer: websocket.New(nil, log.SubLogger("WS")),
		balances: map[string]*balance{
			"eth": {
				free:   1000.123432,
				locked: 0,
			},
			"btc": {
				free:   1000.21314123,
				locked: 0,
			},
			"ltc": {
				free:   1000.8689444,
				locked: 0,
			},
			"bch": {
				free:   1000.2358249,
				locked: 0,
			},
			"dcr": {
				free:   1000.2358249,
				locked: 0,
			},
		},
	}
	http.HandleFunc("/sapi/v1/capital/config/getall", f.handleWalletCoinsReq)

	return http.ListenAndServe(":37346", nil)
}

type balance struct {
	free   float64
	locked float64
}

type fakeBinance struct {
	wsServer *websocket.Server

	balanceMtx sync.RWMutex
	balances   map[string]*balance
}

func (f *fakeBinance) handleWalletCoinsReq(w http.ResponseWriter, r *http.Request) {
	ci := f.coinInfo()
	writeJSONWithStatus(w, ci, http.StatusOK)
}

type fakeBinanceNetworkInfo struct {
	Coin                    string `json:"coin"`
	MinConfirm              int    `json:"minConfirm"`
	Network                 string `json:"network"`
	UnLockConfirm           int    `json:"unLockConfirm"`
	WithdrawEnable          bool   `json:"withdrawEnable"`
	WithdrawFee             string `json:"withdrawFee"`
	WithdrawIntegerMultiple string `json:"withdrawIntegerMultiple"`
	WithdrawMax             string `json:"withdrawMax"`
	WithdrawMin             string `json:"withdrawMin"`
}

type fakeBinanceCoinInfo struct {
	Coin        string                    `json:"coin"`
	Free        string                    `json:"free"`
	Locked      string                    `json:"locked"`
	Withdrawing string                    `json:"withdrawing"`
	NetworkList []*fakeBinanceNetworkInfo `json:"networkList"`
}

func (f *fakeBinance) coinInfo() (coins []*fakeBinanceCoinInfo) {
	f.balanceMtx.Lock()
	for symbol, bal := range f.balances {
		bigSymbol := strings.ToUpper(symbol)
		coins = append(coins, &fakeBinanceCoinInfo{
			Coin:        bigSymbol,
			Free:        strconv.FormatFloat(bal.free, 'f', 8, 64),
			Locked:      strconv.FormatFloat(bal.locked, 'f', 8, 64),
			Withdrawing: "0",
			NetworkList: []*fakeBinanceNetworkInfo{
				{
					Coin:                    bigSymbol,
					Network:                 bigSymbol,
					MinConfirm:              1,
					WithdrawEnable:          true,
					WithdrawFee:             strconv.FormatFloat(0.00000800, 'f', 8, 64),
					WithdrawIntegerMultiple: strconv.FormatFloat(0.00000001, 'f', 8, 64),
					WithdrawMax:             strconv.FormatFloat(1000, 'f', 8, 64),
					WithdrawMin:             strconv.FormatFloat(0.01, 'f', 8, 64),
				},
			},
		})
	}
	f.balanceMtx.Unlock()
	return
}

// writeJSON writes marshals the provided interface and writes the bytes to the
// ResponseWriter with the specified response code.
func writeJSONWithStatus(w http.ResponseWriter, thing interface{}, code int) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	b, err := json.Marshal(thing)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Errorf("JSON encode error: %v", err)
		return
	}
	w.WriteHeader(code)
	_, err = w.Write(append(b, byte('\n')))
	if err != nil {
		log.Errorf("Write error: %v", err)
	}
}
