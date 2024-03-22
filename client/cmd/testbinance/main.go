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
	"sync"

	"decred.org/dcrdex/client/websocket"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/encode"
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
		wsServer:          websocket.New(nil, log.SubLogger("WS")),
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

	withdrawalHistoryMtx sync.RWMutex
	withdrawalHistory    []*transfer

	depositHistoryMtx sync.RWMutex
	depositHistory    []*transfer
}

func (f *fakeBinance) handleWalletCoinsReq(w http.ResponseWriter, r *http.Request) {
	// Returning configs for eth, btc, ltc, bch, zec, usdc, matic.
	// Balances do not use a sapi endpoint, so they do not need to be handled
	// here.
	resp := []byte(`[
		{
			"coin": "MATIC",
			"depositAllEnable": true,
			"free": "0",
			"freeze": "0",
			"ipoable": "0",
			"ipoing": "0",
			"isLegalMoney": false,
			"locked": "0",
			"name": "Polygon",
			"storage": "0",
			"trading": true,
			"withdrawAllEnable": true,
			"withdrawing": "0",
			"networkList": [
				{
					"addressRegex": "^(0x)[0-9A-Fa-f]{40}$",
					"coin": "MATIC",
					"depositEnable": true,
					"isDefault": false,
					"memoRegex": "",
					"minConfirm": 15,
					"name": "BNB Smart Chain (BEP20)",
					"network": "BSC",
					"resetAddressStatus": false,
					"specialTips": "",
					"unLockConfirm": 0,
					"withdrawEnable": true,
					"withdrawFee": "0.8",
					"withdrawIntegerMultiple": "1e-8",
					"withdrawMax": "9999999",
					"withdrawMin": "20",
					"sameAddress": false,
					"estimatedArrivalTime": 0,
					"busy": false
				},
				{
					"addressRegex": "^(0x)[0-9A-Fa-f]{40}$",
					"coin": "MATIC",
					"depositEnable": true,
					"isDefault": true,
					"memoRegex": "",
					"minConfirm": 6,
					"name": "Ethereum (ERC20)",
					"network": "ETH",
					"resetAddressStatus": false,
					"specialTips": "",
					"unLockConfirm": 64,
					"withdrawEnable": true,
					"withdrawFee": "15.8",
					"withdrawIntegerMultiple": "1e-8",
					"withdrawMax": "10000000000",
					"withdrawMin": "31.6",
					"sameAddress": false,
					"estimatedArrivalTime": 0,
					"busy": false
				},
				{
					"addressRegex": "^(0x)[0-9A-Fa-f]{40}$",
					"coin": "MATIC",
					"depositEnable": true,
					"isDefault": false,
					"memoRegex": "",
					"minConfirm": 300,
					"name": "Polygon",
					"network": "MATIC",
					"resetAddressStatus": false,
					"specialTips": "",
					"unLockConfirm": 800,
					"withdrawEnable": true,
					"withdrawFee": "0.1",
					"withdrawIntegerMultiple": "1e-8",
					"withdrawMax": "9999999",
					"withdrawMin": "20",
					"sameAddress": false,
					"estimatedArrivalTime": 0,
					"busy": false
				}
			]
		},
		{
			"coin": "ETH",
			"depositAllEnable": true,
			"free": "0",
			"freeze": "0",
			"ipoable": "0",
			"ipoing": "0",
			"isLegalMoney": false,
			"locked": "0",
			"name": "Ethereum",
			"storage": "0",
			"trading": true,
			"withdrawAllEnable": true,
			"withdrawing": "0",
			"networkList": [
				{
					"addressRegex": "^(0x)[0-9A-Fa-f]{40}$",
					"coin": "ETH",
					"depositEnable": true,
					"isDefault": false,
					"memoRegex": "",
					"minConfirm": 100,
					"name": "Arbitrum One",
					"network": "ARBITRUM",
					"resetAddressStatus": false,
					"specialTips": "",
					"unLockConfirm": 0,
					"withdrawEnable": true,
					"withdrawFee": "0.00035",
					"withdrawIntegerMultiple": "1e-8",
					"withdrawMax": "9999999",
					"withdrawMin": "0.0008",
					"sameAddress": false,
					"estimatedArrivalTime": 0,
					"busy": false
				},
				{
					"addressRegex": "^(bnb1)[0-9a-z]{38}$",
					"coin": "ETH",
					"depositEnable": true,
					"isDefault": false,
					"memoRegex": "^[0-9A-Za-z\\-_]{1,120}$",
					"minConfirm": 1,
					"name": "BNB Beacon Chain (BEP2)",
					"network": "BNB",
					"resetAddressStatus": false,
					"specialTips": "Both a MEMO and an Address are required to successfully deposit your BEP2 tokens to Binance US.",
					"unLockConfirm": 0,
					"withdrawEnable": false,
					"withdrawFee": "0.000086",
					"withdrawIntegerMultiple": "1e-8",
					"withdrawMax": "10000000000",
					"withdrawMin": "0.0005",
					"sameAddress": false,
					"estimatedArrivalTime": 0,
					"busy": false
				},
				{
					"addressRegex": "^(0x)[0-9A-Fa-f]{40}$",
					"coin": "ETH",
					"depositEnable": true,
					"isDefault": false,
					"memoRegex": "",
					"minConfirm": 15,
					"name": "BNB Smart Chain (BEP20)",
					"network": "BSC",
					"resetAddressStatus": false,
					"specialTips": "",
					"unLockConfirm": 0,
					"withdrawEnable": true,
					"withdrawFee": "0.000057",
					"withdrawIntegerMultiple": "1e-8",
					"withdrawMax": "10000000000",
					"withdrawMin": "0.00011",
					"sameAddress": false,
					"estimatedArrivalTime": 0,
					"busy": false
				},
				{
					"addressRegex": "^(0x)[0-9A-Fa-f]{40}$",
					"coin": "ETH",
					"depositEnable": true,
					"isDefault": true,
					"memoRegex": "",
					"minConfirm": 6,
					"name": "Ethereum (ERC20)",
					"network": "ETH",
					"resetAddressStatus": false,
					"specialTips": "",
					"unLockConfirm": 64,
					"withdrawEnable": true,
					"withdrawFee": "0.00221",
					"withdrawIntegerMultiple": "1e-8",
					"withdrawMax": "10000000000",
					"withdrawMin": "0.00442",
					"sameAddress": false,
					"estimatedArrivalTime": 0,
					"busy": false
				},
				{
					"addressRegex": "^(0x)[0-9A-Fa-f]{40}$",
					"coin": "ETH",
					"depositEnable": true,
					"isDefault": true,
					"memoRegex": "",
					"minConfirm": 6,
					"name": "Polygon (ERC20)",
					"network": "MATIC",
					"resetAddressStatus": false,
					"specialTips": "",
					"unLockConfirm": 64,
					"withdrawEnable": true,
					"withdrawFee": "0.00221",
					"withdrawIntegerMultiple": "1e-8",
					"withdrawMax": "10000000000",
					"withdrawMin": "0.00442",
					"sameAddress": false,
					"estimatedArrivalTime": 0,
					"busy": false
				},
				{
					"addressRegex": "^(0x)[0-9A-Fa-f]{40}$",
					"coin": "ETH",
					"depositEnable": true,
					"isDefault": false,
					"memoRegex": "",
					"minConfirm": 100,
					"name": "Optimism",
					"network": "OPTIMISM",
					"resetAddressStatus": false,
					"specialTips": "",
					"unLockConfirm": 120,
					"withdrawEnable": true,
					"withdrawFee": "0.00035",
					"withdrawIntegerMultiple": "1e-8",
					"withdrawMax": "10000000000",
					"withdrawMin": "0.001",
					"sameAddress": false,
					"estimatedArrivalTime": 0,
					"busy": false
				}
			]
		},
		{
			"coin": "ZEC",
			"depositAllEnable": true,
			"free": "0",
			"freeze": "0",
			"ipoable": "0",
			"ipoing": "0",
			"isLegalMoney": false,
			"locked": "0",
			"name": "Zcash",
			"storage": "0",
			"trading": true,
			"withdrawAllEnable": true,
			"withdrawing": "0",
			"networkList": [
				{
					"addressRegex": "^(0x)[0-9A-Fa-f]{40}$",
					"coin": "ZEC",
					"depositEnable": true,
					"isDefault": false,
					"memoRegex": "",
					"minConfirm": 15,
					"name": "BNB Smart Chain (BEP20)",
					"network": "BSC",
					"resetAddressStatus": false,
					"specialTips": "",
					"unLockConfirm": 0,
					"withdrawEnable": false,
					"withdrawFee": "0.0041",
					"withdrawIntegerMultiple": "1e-8",
					"withdrawMax": "10000000000",
					"withdrawMin": "0.0082",
					"sameAddress": false,
					"estimatedArrivalTime": 0,
					"busy": false
				},
				{
					"addressRegex": "^(t)[A-Za-z0-9]{34}$",
					"coin": "ZEC",
					"depositEnable": true,
					"isDefault": true,
					"memoRegex": "",
					"minConfirm": 15,
					"name": "Zcash",
					"network": "ZEC",
					"resetAddressStatus": false,
					"specialTips": "",
					"unLockConfirm": 20,
					"withdrawEnable": true,
					"withdrawFee": "0.005",
					"withdrawIntegerMultiple": "1e-8",
					"withdrawMax": "10000000000",
					"withdrawMin": "0.01",
					"sameAddress": false,
					"estimatedArrivalTime": 0,
					"busy": false
				}
			]
		},
		{
			"coin": "BTC",
			"depositAllEnable": true,
			"free": "0",
			"freeze": "0",
			"ipoable": "0",
			"ipoing": "0",
			"isLegalMoney": false,
			"locked": "0",
			"name": "Bitcoin",
			"storage": "0",
			"trading": true,
			"withdrawAllEnable": true,
			"withdrawing": "0",
			"networkList": [
				{
					"addressRegex": "^(bnb1)[0-9a-z]{38}$",
					"coin": "BTC",
					"depositEnable": true,
					"isDefault": false,
					"memoRegex": "^[0-9A-Za-z\\-_]{1,120}$",
					"minConfirm": 1,
					"name": "BNB Beacon Chain (BEP2)",
					"network": "BNB",
					"resetAddressStatus": false,
					"specialTips": "Both a MEMO and an Address are required to successfully deposit your BEP2-BTCB tokens to Binance.",
					"unLockConfirm": 0,
					"withdrawEnable": false,
					"withdrawFee": "0.0000061",
					"withdrawIntegerMultiple": "1e-8",
					"withdrawMax": "10000000000",
					"withdrawMin": "0.000012",
					"sameAddress": false,
					"estimatedArrivalTime": 0,
					"busy": false
				},
				{
					"addressRegex": "^(0x)[0-9A-Fa-f]{40}$",
					"coin": "BTC",
					"depositEnable": true,
					"isDefault": false,
					"memoRegex": "",
					"minConfirm": 15,
					"name": "BNB Smart Chain (BEP20)",
					"network": "BSC",
					"resetAddressStatus": false,
					"specialTips": "",
					"unLockConfirm": 0,
					"withdrawEnable": true,
					"withdrawFee": "0.000003",
					"withdrawIntegerMultiple": "1e-8",
					"withdrawMax": "10000000000",
					"withdrawMin": "0.000006",
					"sameAddress": false,
					"estimatedArrivalTime": 0,
					"busy": false
				},
				{
					"addressRegex": "^[13][a-km-zA-HJ-NP-Z1-9]{25,34}$|^[(bc1q)|(bc1p)][0-9A-Za-z]{37,62}$",
					"coin": "BTC",
					"depositEnable": true,
					"isDefault": true,
					"memoRegex": "",
					"minConfirm": 1,
					"name": "Bitcoin",
					"network": "BTC",
					"resetAddressStatus": false,
					"specialTips": "",
					"unLockConfirm": 2,
					"withdrawEnable": true,
					"withdrawFee": "0.00025",
					"withdrawIntegerMultiple": "1e-8",
					"withdrawMax": "10000000000",
					"withdrawMin": "0.0005",
					"sameAddress": false,
					"estimatedArrivalTime": 0,
					"busy": false
				}
			]
		},
		{
			"coin": "LTC",
			"depositAllEnable": true,
			"free": "0",
			"freeze": "0",
			"ipoable": "0",
			"ipoing": "0",
			"isLegalMoney": false,
			"locked": "0",
			"name": "Litecoin",
			"storage": "0",
			"trading": true,
			"withdrawAllEnable": true,
			"withdrawing": "0",
			"networkList": [
				{
					"addressRegex": "^(bnb1)[0-9a-z]{38}$",
					"coin": "LTC",
					"depositEnable": true,
					"isDefault": false,
					"memoRegex": "^[0-9A-Za-z\\-_]{1,120}$",
					"minConfirm": 1,
					"name": "BNB Beacon Chain (BEP2)",
					"network": "BNB",
					"resetAddressStatus": false,
					"specialTips": "Both a MEMO and an Address are required to successfully deposit your LTC BEP2 tokens to Binance.",
					"unLockConfirm": 0,
					"withdrawEnable": false,
					"withdrawFee": "0.0035",
					"withdrawIntegerMultiple": "1e-8",
					"withdrawMax": "10000000000",
					"withdrawMin": "0.007",
					"sameAddress": false,
					"estimatedArrivalTime": 0,
					"busy": false
				},
				{
					"addressRegex": "^(0x)[0-9A-Fa-f]{40}$",
					"coin": "LTC",
					"depositEnable": true,
					"isDefault": false,
					"memoRegex": "",
					"minConfirm": 15,
					"name": "BNB Smart Chain (BEP20)",
					"network": "BSC",
					"resetAddressStatus": false,
					"specialTips": "",
					"unLockConfirm": 0,
					"withdrawEnable": true,
					"withdrawFee": "0.0017",
					"withdrawIntegerMultiple": "1e-8",
					"withdrawMax": "10000000000",
					"withdrawMin": "0.0034",
					"sameAddress": false,
					"estimatedArrivalTime": 0,
					"busy": false
				},
				{
					"addressRegex": "^(L|M)[A-Za-z0-9]{33}$|^(ltc1)[0-9A-Za-z]{39}$",
					"coin": "LTC",
					"depositEnable": true,
					"isDefault": true,
					"memoRegex": "",
					"minConfirm": 3,
					"name": "Litecoin",
					"network": "LTC",
					"resetAddressStatus": false,
					"specialTips": "",
					"unLockConfirm": 4,
					"withdrawEnable": true,
					"withdrawFee": "0.00125",
					"withdrawIntegerMultiple": "1e-8",
					"withdrawMax": "10000000000",
					"withdrawMin": "0.002",
					"sameAddress": false,
					"estimatedArrivalTime": 0,
					"busy": false
				}
			]
		},
		{
			"coin": "BCH",
			"depositAllEnable": true,
			"free": "0",
			"freeze": "0",
			"ipoable": "0",
			"ipoing": "0",
			"isLegalMoney": false,
			"locked": "0",
			"name": "Bitcoin Cash",
			"storage": "0",
			"trading": true,
			"withdrawAllEnable": true,
			"withdrawing": "0",
			"networkList": [
				{
					"addressRegex": "^[1][a-km-zA-HJ-NP-Z1-9]{25,34}$|^[0-9a-z]{42,42}$",
					"coin": "BCH",
					"depositEnable": true,
					"isDefault": true,
					"memoRegex": "",
					"minConfirm": 2,
					"name": "Bitcoin Cash",
					"network": "BCH",
					"resetAddressStatus": false,
					"specialTips": "",
					"unLockConfirm": 6,
					"withdrawEnable": true,
					"withdrawFee": "0.0008",
					"withdrawIntegerMultiple": "1e-8",
					"withdrawMax": "10000000000",
					"withdrawMin": "0.002",
					"sameAddress": false,
					"estimatedArrivalTime": 0,
					"busy": false
				},
				{
					"addressRegex": "^(bnb1)[0-9a-z]{38}$",
					"coin": "BCH",
					"depositEnable": false,
					"isDefault": false,
					"memoRegex": "^[0-9A-Za-z\\-_]{1,120}$",
					"minConfirm": 1,
					"name": "BNB Beacon Chain (BEP2)",
					"network": "BNB",
					"resetAddressStatus": false,
					"specialTips": "Both a MEMO and an Address are required to successfully deposit your BCH BEP2 tokens to Binance.",
					"unLockConfirm": 0,
					"withdrawEnable": false,
					"withdrawFee": "0.0011",
					"withdrawIntegerMultiple": "1e-8",
					"withdrawMax": "10000000000",
					"withdrawMin": "0.0022",
					"sameAddress": false,
					"estimatedArrivalTime": 0,
					"busy": false
				},
				{
					"addressRegex": "^(0x)[0-9A-Fa-f]{40}$",
					"coin": "BCH",
					"depositEnable": true,
					"isDefault": false,
					"memoRegex": "",
					"minConfirm": 15,
					"name": "BNB Smart Chain (BEP20)",
					"network": "BSC",
					"resetAddressStatus": false,
					"specialTips": "",
					"unLockConfirm": 0,
					"withdrawEnable": true,
					"withdrawFee": "0.00054",
					"withdrawIntegerMultiple": "1e-8",
					"withdrawMax": "10000000000",
					"withdrawMin": "0.0011",
					"sameAddress": false,
					"estimatedArrivalTime": 0,
					"busy": false
				}
			]
		},
		{
			"coin": "USDC",
			"depositAllEnable": true,
			"free": "0",
			"freeze": "0",
			"ipoable": "0",
			"ipoing": "0",
			"isLegalMoney": false,
			"locked": "0",
			"name": "USD Coin",
			"storage": "0",
			"trading": true,
			"withdrawAllEnable": true,
			"withdrawing": "0",
			"networkList": [
				{
					"addressRegex": "^(0x)[0-9A-Fa-f]{40}$",
					"coin": "USDC",
					"depositEnable": true,
					"isDefault": false,
					"memoRegex": "",
					"minConfirm": 12,
					"name": "AVAX C-Chain",
					"network": "AVAXC",
					"resetAddressStatus": false,
					"specialTips": "",
					"unLockConfirm": 0,
					"withdrawEnable": true,
					"withdrawFee": "1",
					"withdrawIntegerMultiple": "0.000001",
					"withdrawMax": "9999999",
					"withdrawMin": "50",
					"sameAddress": false,
					"estimatedArrivalTime": 0,
					"busy": false
				},
				{
					"addressRegex": "^(0x)[0-9A-Fa-f]{40}$",
					"coin": "USDC",
					"depositEnable": true,
					"isDefault": false,
					"memoRegex": "",
					"minConfirm": 15,
					"name": "BNB Smart Chain (BEP20)",
					"network": "BSC",
					"resetAddressStatus": false,
					"specialTips": "",
					"unLockConfirm": 0,
					"withdrawEnable": true,
					"withdrawFee": "0.29",
					"withdrawIntegerMultiple": "1e-8",
					"withdrawMax": "10000000000",
					"withdrawMin": "10",
					"sameAddress": false,
					"estimatedArrivalTime": 0,
					"busy": false
				},
				{
					"addressRegex": "^(0x)[0-9A-Fa-f]{40}$",
					"coin": "USDC",
					"depositEnable": true,
					"isDefault": true,
					"memoRegex": "",
					"minConfirm": 6,
					"name": "Ethereum (ERC20)",
					"network": "ETH",
					"resetAddressStatus": false,
					"specialTips": "",
					"unLockConfirm": 64,
					"withdrawEnable": true,
					"withdrawFee": "15.16",
					"withdrawIntegerMultiple": "0.000001",
					"withdrawMax": "10000000000",
					"withdrawMin": "30.32",
					"sameAddress": false,
					"estimatedArrivalTime": 0,
					"busy": false
				},
				{
					"addressRegex": "^(0x)[0-9A-Fa-f]{40}$",
					"coin": "USDC",
					"depositEnable": true,
					"isDefault": false,
					"memoRegex": "",
					"minConfirm": 300,
					"name": "Polygon",
					"network": "MATIC",
					"resetAddressStatus": false,
					"specialTips": "",
					"unLockConfirm": 800,
					"withdrawEnable": true,
					"withdrawFee": "1",
					"withdrawIntegerMultiple": "0.000001",
					"withdrawMax": "9999999",
					"withdrawMin": "10",
					"sameAddress": false,
					"estimatedArrivalTime": 0,
					"busy": false
				},
				{
					"addressRegex": "^[1-9A-HJ-NP-Za-km-z]{32,44}$",
					"coin": "USDC",
					"depositEnable": true,
					"isDefault": false,
					"memoRegex": "",
					"minConfirm": 1,
					"name": "Solana",
					"network": "SOL",
					"resetAddressStatus": false,
					"specialTips": "",
					"unLockConfirm": 0,
					"withdrawEnable": true,
					"withdrawFee": "1",
					"withdrawIntegerMultiple": "0.000001",
					"withdrawMax": "250000100",
					"withdrawMin": "10",
					"sameAddress": false,
					"estimatedArrivalTime": 0,
					"busy": false
				},
				{
					"addressRegex": "^T[1-9A-HJ-NP-Za-km-z]{33}$",
					"coin": "USDC",
					"depositEnable": true,
					"isDefault": false,
					"memoRegex": "",
					"minConfirm": 1,
					"name": "Tron (TRC20)",
					"network": "TRX",
					"resetAddressStatus": false,
					"specialTips": "",
					"unLockConfirm": 0,
					"withdrawEnable": true,
					"withdrawFee": "1.5",
					"withdrawIntegerMultiple": "0.000001",
					"withdrawMax": "10000000000",
					"withdrawMin": "10",
					"sameAddress": false,
					"estimatedArrivalTime": 0,
					"busy": false
				}
			]
		},
		{
			"coin": "WBTC",
			"depositAllEnable": true,
			"free": "0",
			"freeze": "0",
			"ipoable": "0",
			"ipoing": "0",
			"isLegalMoney": false,
			"locked": "0",
			"name": "Wrapped Bitcoin",
			"storage": "0",
			"trading": true,
			"withdrawAllEnable": true,
			"withdrawing": "0",
			"networkList": [
				{
					"addressRegex": "^(0x)[0-9A-Fa-f]{40}$",
					"coin": "WBTC",
					"depositEnable": true,
					"isDefault": true,
					"memoRegex": "",
					"minConfirm": 6,
					"name": "Ethereum (ERC20)",
					"network": "ETH",
					"resetAddressStatus": false,
					"specialTips": "",
					"unLockConfirm": 64,
					"withdrawEnable": true,
					"withdrawFee": "0.0003",
					"withdrawIntegerMultiple": "1e-8",
					"withdrawMax": "10000000000",
					"withdrawMin": "0.0006",
					"sameAddress": false,
					"estimatedArrivalTime": 0,
					"busy": false
				}
			]
		}
	]`)

	writeBytesWithStatus(w, resp, http.StatusOK)
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
	writeBytesWithStatus(w, b, code)
}

func writeBytesWithStatus(w http.ResponseWriter, b []byte, code int) {
	w.WriteHeader(code)
	_, err := w.Write(append(b, byte('\n')))
	if err != nil {
		log.Errorf("Write error: %v", err)
	}
}
