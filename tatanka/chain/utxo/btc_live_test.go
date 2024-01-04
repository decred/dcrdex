//go:build harness

package utxo

import (
	"context"
	"encoding/json"
	"fmt"
	"os/user"
	"path/filepath"
	"testing"
	"time"

	"decred.org/dcrdex/dex"
	dexbtc "decred.org/dcrdex/dex/networks/btc"
	"decred.org/dcrdex/tatanka/chain"
)

func TestSimnetBTC(t *testing.T) {
	simnetAlphaHarnessConfig := &BitcoinConfigFile{
		RPCConfig: dexbtc.RPCConfig{
			RPCUser: "user",
			RPCPass: "pass",
			RPCBind: "127.0.0.1",
			RPCPort: 20556,
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rawCfg, _ := json.Marshal(simnetAlphaHarnessConfig)
	c := prepareSimnetChain(t, ctx, NewBitcoin, rawCfg)

	q := &query{
		Method: "getchaintxstats",
		Args:   []json.RawMessage{[]byte("5")},
	}
	rawQuery, _ := json.Marshal(q)
	rawRes, err := c.Query(ctx, chain.Query(rawQuery))
	if err != nil {
		t.Fatalf("Query error: %v", err)
	}

	var res struct {
		Blocks uint `json:"window_block_count"`
	}
	if err := json.Unmarshal(rawRes, &res); err != nil {
		t.Fatalf("Query unmarshal error: %v", err)
	}

	if res.Blocks != 5 {
		t.Fatalf("Unexpected query results")
	}
}
func TestSimnetDCR(t *testing.T) {
	usr, _ := user.Current()
	dextestDir := filepath.Join(usr.HomeDir, "dextest")
	rpcPath := filepath.Join(dextestDir, "dcr", "alpha", "rpc.cert")
	simnetAlphaHarnessConfig := &DecredConfigFile{
		RPCUser:   "user",
		RPCPass:   "pass",
		RPCListen: "127.0.0.1:19561",
		RPCCert:   rpcPath,
	}
	rawCfg, _ := json.Marshal(simnetAlphaHarnessConfig)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := prepareSimnetChain(t, ctx, NewDecred, rawCfg)

	q := &query{
		Method: "getvoteinfo",
		Args:   []json.RawMessage{[]byte("4")},
	}
	rawQuery, _ := json.Marshal(q)
	rawRes, err := c.Query(ctx, chain.Query(rawQuery))
	if err != nil {
		t.Fatalf("Query error: %v", err)
	}

	var res struct {
		Agendas []struct {
			ID string `json:"id"`
		} `json:"agendas"`
	}
	if err := json.Unmarshal(rawRes, &res); err != nil {
		t.Fatalf("Query unmarshal error: %v", err)
	}

	if len(res.Agendas) != 1 || res.Agendas[0].ID != "maxblocksize" {
		t.Fatalf("Unexpected query results")
	}
}

func prepareSimnetChain(t *testing.T, ctx context.Context, newChain chain.ChainConstructor, rawCfg json.RawMessage) chain.Chain {
	feeMonitorTick = time.Second

	c, err := newChain(rawCfg, dex.StdOutLogger("T", dex.LevelTrace), dex.Simnet)
	if err != nil {
		t.Fatalf("NewBTC error: %v", err)
	}

	cm := dex.NewConnectionMaster(c)
	if err := cm.ConnectOnce(ctx); err != nil {
		t.Fatalf("Connect error: %v", err)
	}

	fmt.Println("Waiting for fee update")
	select {
	case fees := <-c.FeeChannel():
		fmt.Println("Fees received over fee channel =", fees)
	case <-time.After(time.Second * 30):
		t.Fatalf("No fee update seen. Is the miner running?")
	}

	return c
}
