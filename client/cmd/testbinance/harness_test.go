//go:build harness

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"decred.org/dcrdex/client/comms"
	"decred.org/dcrdex/client/mm/libxc/bntypes"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/dexnet"
)

const testAPIKey = "Test Client 3000"

func TestMain(m *testing.M) {
	log = dex.StdOutLogger("T", dex.LevelTrace)
	m.Run()
}

func TestUtxoWallet(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w, err := newUtxoWallet(ctx, "btc")
	if err != nil {
		t.Fatalf("newUtxoWallet error: %v", err)
	}
	testWallet(t, ctx, w)
}

func TestEvmWallet(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w, err := newEvmWallet(ctx, "eth")
	if err != nil {
		t.Fatalf("newUtxoWallet error: %v", err)
	}
	testWallet(t, ctx, w)
}

func testWallet(t *testing.T, ctx context.Context, w Wallet) {
	addr := w.DepositAddress()
	fmt.Println("##### Deposit address:", addr)
	txID, err := w.Send(ctx, addr, "", 0.1)
	if err != nil {
		t.Fatalf("Send error: %v", err)
	}
	fmt.Println("##### Self-send tx ID:", txID)
	for i := 0; i < 3; i++ {
		confs, err := w.Confirmations(ctx, txID)
		if err != nil {
			fmt.Println("##### Confirmations error:", err)
			if i < 2 {
				fmt.Println("##### Trying again in 15 seconds")
				time.Sleep(time.Second * 15)
			}
		} else {
			fmt.Println("##### Confirmations:", confs)
			return
		}
	}
	t.Fatal("Failed to get confirmations")
}

func getInto(method, endpoint string, thing any) error {
	req, err := http.NewRequest(method, "http://localhost:37346"+endpoint, nil)
	if err != nil {
		return err
	}
	return requestInto(req, thing)
}

func requestInto(req *http.Request, thing any) error {
	req.Header.Add("X-MBX-APIKEY", testAPIKey)

	resp, err := dexnet.Client.Do(req)
	if err != nil || thing == nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return json.Unmarshal(b, thing)
}

func printThing(name string, thing any) {
	b, _ := json.MarshalIndent(thing, "", "    ")
	fmt.Println("#####", name, ":", string(b))
}

func newWebsocketClient(ctx context.Context, uri string, handler func([]byte)) (comms.WsConn, *dex.ConnectionMaster, error) {
	wsClient, err := comms.NewWsConn(&comms.WsCfg{
		URL:            uri,
		PingWait:       pongWait,
		Logger:         dex.StdOutLogger("W", log.Level()),
		RawHandler:     handler,
		ConnectHeaders: http.Header{"X-MBX-APIKEY": []string{testAPIKey}},
	})
	if err != nil {
		return nil, nil, fmt.Errorf("Error creating websocket client: %v", err)
	}
	wsCM := dex.NewConnectionMaster(wsClient)
	if err = wsCM.ConnectOnce(ctx); err != nil {
		return nil, nil, fmt.Errorf("Error connecting websocket client: %v", err)
	}
	return wsClient, wsCM, nil
}

func TestMarketFeed(t *testing.T) {
	// httpURL := "http://localhost:37346"
	wsURL := "ws://localhost:37346"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var xcInfo bntypes.ExchangeInfo
	if err := getInto("GET", "/api/v3/exchangeInfo", &xcInfo); err != nil {
		t.Fatalf("Error getting exchange info: %v", err)
	}
	printThing("ExchangeInfo", xcInfo)

	depthStreamID := func(mkt *bntypes.Market) string {
		return fmt.Sprintf("%s@depth", strings.ToLower(mkt.Symbol))
	}

	mkt0, mkt1 := xcInfo.Symbols[0], xcInfo.Symbols[1]

	streamHandler := func(b []byte) {
		fmt.Printf("\n##### Market stream -> %s \n\n", string(b))
	}

	uri := fmt.Sprintf(wsURL+"/stream?streams=%s", depthStreamID(mkt0))
	wsClient, wsCM, err := newWebsocketClient(ctx, uri, streamHandler)
	if err != nil {
		t.Fatal(err)
	}
	defer wsCM.Disconnect()

	var book bntypes.OrderbookSnapshot
	if err := getInto("GET", fmt.Sprintf("/api/v3/depth?symbol=%s", mkt0.Symbol), &book); err != nil {
		t.Fatalf("Error getting depth for symbol %s: %v", mkt0.Symbol, err)
	}

	printThing("First order book", &book)

	addSubB, _ := json.Marshal(&bntypes.StreamSubscription{
		Method: "SUBSCRIBE",
		Params: []string{depthStreamID(mkt1)},
		ID:     1,
	})

	fmt.Println("##### Sending second market subscription in 30 seconds")

	select {
	case <-time.After(time.Second * 30):
		if err := wsClient.SendRaw(addSubB); err != nil {
			t.Fatalf("error sending subscription stream request: %v", err)
		}
		fmt.Println("##### Sent second market subscription")
		var book bntypes.OrderbookSnapshot
		if err := getInto("GET", fmt.Sprintf("/api/v3/depth?symbol=%s", mkt1.Symbol), &book); err != nil {
			t.Fatalf("Error getting depth for symbol %s: %v", mkt1.Symbol, err)
		}
		printThing("Second order book", &book)
	case <-ctx.Done():
		return
	}

	unubB, _ := json.Marshal(&bntypes.StreamSubscription{
		Method: "UNSUBSCRIBE",
		Params: []string{depthStreamID(mkt0), depthStreamID(mkt1)},
		ID:     2,
	})

	fmt.Println("##### Monitoring stream for 5 minutes")
	select {
	case <-time.After(time.Minute * 5):
		if err := wsClient.SendRaw(unubB); err != nil {
			t.Fatalf("error sending unsubscribe request: %v", err)
		}
		fmt.Println("##### All done")
	case <-ctx.Done():
		return
	}
}

func TestAccountFeed(t *testing.T) {
	// httpURL := "http://localhost:37346"
	wsURL := "ws://localhost:37346"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var addrResp struct {
		Address string `json:"address"`
	}
	if err := getInto("GET", "/sapi/v1/capital/deposit/address?coin=BTC", &addrResp); err != nil {
		t.Fatalf("Error getting deposit address: %v", err)
	}

	printThing("Deposit address", addrResp)

	w, err := newUtxoWallet(ctx, "btc")
	if err != nil {
		t.Fatalf("Error constructing btc wallet: %v", err)
	}
	txID, err := w.Send(ctx, addrResp.Address, "", 0.1)
	if err != nil {
		t.Fatalf("Send error: %v", err)
	}

	streamHandler := func(b []byte) {
		fmt.Printf("\n##### Account stream -> %s \n\n", string(b))
	}

	_, wsCM, err := newWebsocketClient(ctx, wsURL+"/ws/testListenKey", streamHandler)
	if err != nil {
		t.Fatal(err)
	}
	defer wsCM.Disconnect()

	endpoint := "/sapi/v1/capital/deposit/hisrec?amt=0.1&coin=BTC&network=BTC&txid=" + txID
	for {
		var resp []*bntypes.PendingDeposit
		if err := getInto("GET", endpoint, &resp); err != nil {
			t.Fatalf("Error getting deposit confirmations: %v", err)
		}
		printThing("Deposit Status", resp)
		if len(resp) > 0 && resp[0].Status == bntypes.DepositStatusCredited {
			fmt.Println("##### Deposit confirmed")
			break
		}
		fmt.Println("##### Checking unconfirmed deposit again in 15 seconds")
		select {
		case <-time.After(time.Second * 15):
		case <-ctx.Done():
			return
		}
	}

	withdrawAddr := w.DepositAddress()
	form := make(url.Values)
	form.Add("coin", "BTC")
	form.Add("network", "BTC")
	form.Add("address", withdrawAddr)
	form.Add("amount", "0.1")
	bodyString := form.Encode()
	req, err := http.NewRequest(http.MethodPost, "http://localhost:37346/sapi/v1/capital/withdraw/apply", bytes.NewBufferString(bodyString))
	if err != nil {
		t.Fatalf("Error constructing withdraw request: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	var withdrawResp struct {
		ID string `json:"id"`
	}
	if err := requestInto(req, &withdrawResp); err != nil {
		t.Fatalf("Error requesting withdraw: %v", err)
	}

	printThing("Withdraw response", &withdrawResp)

	var withdraws []*withdrawalHistoryStatus
	if err := getInto("GET", "/sapi/v1/capital/withdraw/history?coin=BTC", &withdraws); err != nil {
		t.Fatalf("Error fetching withdraw history: %v", err)
	}
	printThing("Withdraw history", withdraws)

}
