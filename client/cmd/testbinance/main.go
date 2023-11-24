package main

/*
 * Starts an http server that responds to some of the binance api's endpoints.
 * The "runserver" command starts the server, and other commands are used to
 * update the server's state.
 */

import (
	"encoding/hex"
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
	"decred.org/dcrdex/dex/encode"
)

const (
	pongWait   = 60 * time.Second
	pingPeriod = (pongWait * 9) / 10
)

var (
	log = dex.StdOutLogger("TBNC", dex.LevelDebug)
)

func printUsage() {
	fmt.Println("Commands:")
	fmt.Println("   runserver")
	fmt.Println("   complete-withdrawal <id> <txid>")
	fmt.Println("   complete-deposit <txid> <amt> <coin> <network>")
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cmd := os.Args[1]

	switch cmd {
	case "runserver":
		if err := runServer(); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		os.Exit(0)
	case "complete-withdrawal":
		if len(os.Args) < 4 {
			printUsage()
			os.Exit(1)
		}
		id := os.Args[2]
		txid := os.Args[3]
		if err := completeWithdrawal(id, txid); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
	case "complete-deposit":
		if len(os.Args) < 6 {
			printUsage()
			os.Exit(1)
		}
		txid := os.Args[2]
		amtStr := os.Args[3]
		amt, err := strconv.ParseFloat(amtStr, 64)
		if err != nil {
			fmt.Println("Error parsing amount: ", err)
			os.Exit(1)
		}
		coin := os.Args[4]
		network := os.Args[5]
		if err := completeDeposit(txid, amt, coin, network); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
	default:
		fmt.Printf("Unknown command: %s\n", cmd)
		printUsage()
	}
}

func runServer() error {
	f := &fakeBinance{
		wsServer: websocket.New(nil, log.SubLogger("WS")),
		balances: map[string]*balance{
			"eth": {
				free:   0.5,
				locked: 0,
			},
			"btc": {
				free:   10,
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
			"zec": {
				free:   1000.2358249,
				locked: 0,
			},
			"polygon": {
				free:   1000.2358249,
				locked: 0,
			},
		},
		withdrawalHistory: make([]*transfer, 0),
		depositHistory:    make([]*transfer, 0),
	}

	// Fake binance handlers
	http.HandleFunc("/sapi/v1/capital/config/getall", f.handleWalletCoinsReq)
	http.HandleFunc("/sapi/v1/capital/deposit/hisrec", f.handleConfirmDeposit)
	http.HandleFunc("/sapi/v1/capital/deposit/address", f.handleGetDepositAddress)
	http.HandleFunc("/sapi/v1/capital/withdraw/apply", f.handleWithdrawal)
	http.HandleFunc("/sapi/v1/capital/withdraw/history", f.handleWithdrawalHistory)

	// Handlers for updating fake binance state
	http.HandleFunc("/completewithdrawal", f.handleCompleteWithdrawal)
	http.HandleFunc("/completedeposit", f.handleCompleteDeposit)

	return http.ListenAndServe(":37346", nil)
}

// completeWithdrawal sends a request to the fake binance server to update a
// withdrawal's status to complete.
func completeWithdrawal(id, txid string) error {
	// Send complete withdrawal request
	req, err := http.NewRequest("GET", fmt.Sprintf("http://localhost:37346/completewithdrawal?id=%s&txid=%s", id, txid), nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("error sending request: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("request failed with status: %d", resp.StatusCode)
	}
	return nil
}

// completeDeposit sends a request to the fake binance server to update a
// deposit's status to complete.
func completeDeposit(txid string, amt float64, coin, network string) error {
	// Send complete deposit request
	req, err := http.NewRequest("GET", fmt.Sprintf("http://localhost:37346/completedeposit?txid=%s&amt=%f&coin=%s&network=%s", txid, amt, coin, network), nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("error sending request: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("request failed with status: %d", resp.StatusCode)
	}
	return nil
}

type balance struct {
	free   float64
	locked float64
}

type transfer struct {
	id      string
	amt     float64
	txID    string
	coin    string
	network string
}

type fakeBinance struct {
	wsServer *websocket.Server

	balanceMtx sync.RWMutex
	balances   map[string]*balance

	withdrawalHistoryMtx sync.RWMutex
	withdrawalHistory    []*transfer

	depositHistoryMtx sync.RWMutex
	depositHistory    []*transfer
}

func (f *fakeBinance) handleWalletCoinsReq(w http.ResponseWriter, r *http.Request) {
	ci := f.coinInfo()
	writeJSONWithStatus(w, ci, http.StatusOK)
}

func (f *fakeBinance) handleConfirmDeposit(w http.ResponseWriter, r *http.Request) {
	var resp []struct {
		Amount  float64 `json:"amount,string"`
		Coin    string  `json:"coin"`
		Network string  `json:"network"`
		Status  int     `json:"status"`
		TxID    string  `json:"txId"`
	}

	f.depositHistoryMtx.RLock()
	for _, d := range f.depositHistory {
		resp = append(resp, struct {
			Amount  float64 `json:"amount,string"`
			Coin    string  `json:"coin"`
			Network string  `json:"network"`
			Status  int     `json:"status"`
			TxID    string  `json:"txId"`
		}{
			Amount:  d.amt,
			Status:  6,
			TxID:    d.txID,
			Coin:    d.coin,
			Network: d.network,
		})
	}
	f.depositHistoryMtx.RUnlock()

	fmt.Println("\n\nSending deposit history: ")
	for _, d := range resp {
		fmt.Printf("%+v\n", d)
	}

	writeJSONWithStatus(w, resp, http.StatusOK)
}

func (f *fakeBinance) handleGetDepositAddress(w http.ResponseWriter, r *http.Request) {
	fmt.Printf("Get deposit address called %s\n", r.URL)
	coin := r.URL.Query().Get("coin")
	var address string

	switch coin {
	case "ETH":
		address = "0xab5801a7d398351b8be11c439e05c5b3259aec9b"
	case "BTC":
		address = "bcrt1qm8m7mqpc0k3wpdt6ljfm0lf2qmhvc0uh8mteh3"
	}

	resp := struct {
		Address string `json:"address"`
	}{
		Address: address,
	}

	writeJSONWithStatus(w, resp, http.StatusOK)
}

func (f *fakeBinance) handleWithdrawal(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	err := r.ParseForm()
	if err != nil {
		fmt.Println("Error parsing form: ", err)
		http.Error(w, "Error parsing form", http.StatusBadRequest)
		return
	}

	amountStr := r.Form.Get("amount")
	amt, err := strconv.ParseFloat(amountStr, 64)
	if err != nil {
		fmt.Println("Error parsing amount: ", err)
		http.Error(w, "Error parsing amount", http.StatusBadRequest)
		return
	}

	withdrawalID := hex.EncodeToString(encode.RandomBytes(32))
	fmt.Printf("\n\nWithdraw called: %+v\nResponding with ID: %s\n", r.Form, withdrawalID)

	f.withdrawalHistoryMtx.Lock()
	f.withdrawalHistory = append(f.withdrawalHistory, &transfer{
		id:  withdrawalID,
		amt: amt * 0.99,
	})
	f.withdrawalHistoryMtx.Unlock()

	resp := struct {
		ID string `json:"id"`
	}{withdrawalID}
	writeJSONWithStatus(w, resp, http.StatusOK)
}

func (f *fakeBinance) handleWithdrawalHistory(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	const withdrawalCompleteStatus = 6

	type withdrawalHistoryStatus struct {
		ID     string  `json:"id"`
		Amount float64 `json:"amount,string"`
		Status int     `json:"status"`
		TxID   string  `json:"txId"`
	}

	withdrawalHistory := make([]*withdrawalHistoryStatus, 0)

	f.withdrawalHistoryMtx.RLock()
	for _, w := range f.withdrawalHistory {
		var status int
		if w.txID == "" {
			status = 2
		} else {
			status = withdrawalCompleteStatus
		}
		withdrawalHistory = append(withdrawalHistory, &withdrawalHistoryStatus{
			ID:     w.id,
			Amount: w.amt,
			Status: status,
			TxID:   w.txID,
		})
	}
	f.withdrawalHistoryMtx.RUnlock()

	fmt.Println("\n\nSending withdrawal history: ")
	for _, w := range withdrawalHistory {
		fmt.Printf("%+v\n", w)
	}

	writeJSONWithStatus(w, withdrawalHistory, http.StatusOK)
}

func (f *fakeBinance) handleCompleteWithdrawal(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	txid := r.URL.Query().Get("txid")

	if id == "" || txid == "" {
		http.Error(w, "Missing id or txid", http.StatusBadRequest)
		return
	}

	f.withdrawalHistoryMtx.Lock()
	for _, w := range f.withdrawalHistory {
		if w.id == id {
			fmt.Println("\nUpdated withdrawal history")
			w.txID = txid
			break
		}
	}
	f.withdrawalHistoryMtx.Unlock()

	w.WriteHeader(http.StatusOK)
}

func (f *fakeBinance) handleCompleteDeposit(w http.ResponseWriter, r *http.Request) {
	txid := r.URL.Query().Get("txid")
	amtStr := r.URL.Query().Get("amt")
	coin := r.URL.Query().Get("coin")
	network := r.URL.Query().Get("network")

	amt, err := strconv.ParseFloat(amtStr, 64)
	if err != nil {
		fmt.Println("Error parsing amount: ", err)
		http.Error(w, "Error parsing amount", http.StatusBadRequest)
		return
	}

	if txid == "" {
		http.Error(w, "Missing txid", http.StatusBadRequest)
		return
	}

	f.depositHistoryMtx.Lock()
	f.depositHistory = append(f.depositHistory, &transfer{
		amt:     amt,
		txID:    txid,
		coin:    coin,
		network: network,
	})
	f.depositHistoryMtx.Unlock()
	w.WriteHeader(http.StatusOK)
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

// writeJSON marshals the provided interface and writes the bytes to the
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
