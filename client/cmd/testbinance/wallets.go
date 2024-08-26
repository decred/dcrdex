package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	ethrpc "github.com/ethereum/go-ethereum/rpc"
)

var (
	dextestDir = filepath.Join(os.Getenv("HOME"), "dextest")
)

type Wallet interface {
	DepositAddress() string
	Confirmations(ctx context.Context, txID string) (uint32, error)
	Send(ctx context.Context, addr, coin string, amt float64) (string, error)
}

type utxoWallet struct {
	symbol string
	dir    string
	addr   string
}

func newUtxoWallet(ctx context.Context, symbol string) (*utxoWallet, error) {
	symbol = strings.ToLower(symbol)
	dir := filepath.Join(dextestDir, symbol, "harness-ctl")
	var addr string
	switch symbol {
	case "zec":
		addr = "tmEgW8c44RQQfft9FHXnqGp8XEcQQSRcUXD"
	default:
		ctx, cancel := context.WithTimeout(ctx, time.Second*5)
		defer cancel()
		cmd := exec.CommandContext(ctx, "./alpha", "getnewaddress")
		cmd.Dir = dir
		addrB, err := cmd.CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("getnewaddress error with output = %q, err = %v", string(addrB), err)
		}
		addr = string(addrB)
	}

	return &utxoWallet{
		symbol: symbol,
		dir:    dir,
		addr:   strings.TrimSpace(addr),
	}, nil
}

func (w *utxoWallet) DepositAddress() string {
	return w.addr
}

func (w *utxoWallet) Confirmations(ctx context.Context, txID string) (uint32, error) {
	cmd := exec.CommandContext(ctx, "./alpha", "gettransaction", txID)
	cmd.Dir = w.dir
	log.Tracef("Running utxoWallet.Confirmations command %q from directory %q", cmd, w.dir)
	b, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("gettransaction error with output = %q, err = %v", string(b), err)
	}
	var resp struct {
		Confs uint32 `json:"confirmations"`
	}
	if err = json.Unmarshal(b, &resp); err != nil {
		return 0, fmt.Errorf("error unmarshaling gettransaction response = %q, err = %s", string(b), err)
	}
	return resp.Confs, nil
}

func (w *utxoWallet) unlock(ctx context.Context) {
	switch w.symbol {
	case "zec": // TODO: Others?
		return
	}
	cmd := exec.CommandContext(ctx, "./alpha", "walletpassphrase", "abc", "100000000")
	cmd.Dir = w.dir
	errText, err := cmd.CombinedOutput()
	if err != nil {
		log.Errorf("walletpassphrase error with output = %q, err = %v", string(errText), err)
	}
}

func (w *utxoWallet) Send(ctx context.Context, addr, _ string, amt float64) (string, error) {
	w.unlock(ctx)
	cmd := exec.CommandContext(ctx, "./alpha", "sendtoaddress", addr, strconv.FormatFloat(amt, 'f', 8, 64))
	cmd.Dir = w.dir
	log.Tracef("Running utxoWallet.Send command %q from directory %q", cmd, w.dir)
	txID, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("sendtoaddress error with output = %q, err = %v", string(txID), err)
	}
	return strings.TrimSpace(string(txID)), err
}

type evmWallet struct {
	dir  string
	addr string
	ec   *ethclient.Client
}

func newEvmWallet(ctx context.Context, symbol string) (*evmWallet, error) {
	symbol = strings.ToLower(symbol)
	rpcAddr := "http://localhost:38556"
	switch symbol {
	case "matic", "polygon":
		symbol = "polygon"
		rpcAddr = "http://localhost:48296"
	}

	ctx, cancel := context.WithTimeout(ctx, time.Second*5)
	defer cancel()
	rpcClient, err := ethrpc.DialContext(ctx, rpcAddr)
	if err != nil {
		return nil, err
	}

	ec := ethclient.NewClient(rpcClient)
	return &evmWallet{
		dir:  filepath.Join(dextestDir, symbol, "harness-ctl"),
		addr: "0x18d65fb8d60c1199bb1ad381be47aa692b482605",
		ec:   ec,
	}, nil
}

func (w *evmWallet) DepositAddress() string {
	return w.addr
}

func (w *evmWallet) Confirmations(ctx context.Context, txID string) (uint32, error) {
	r, err := w.ec.TransactionReceipt(ctx, common.HexToHash(txID))
	if err != nil {
		return 0, fmt.Errorf("TransactionReceipt error: %v", err)
	}
	tip, err := w.ec.HeaderByNumber(ctx, nil /* latest */)
	if err != nil {
		return 0, fmt.Errorf("HeaderByNumber error: %w", err)
	}
	if r.BlockNumber != nil && tip.Number != nil {
		bigConfs := new(big.Int).Sub(tip.Number, r.BlockNumber)
		if bigConfs.Sign() < 0 { // avoid potential overflow
			return 0, nil
		}
		bigConfs.Add(bigConfs, big.NewInt(1))
		if bigConfs.IsInt64() {
			return uint32(bigConfs.Int64()), nil
		}
	}
	return 0, nil
}

func (w *evmWallet) Send(ctx context.Context, addr, coin string, amt float64) (string, error) {
	script := "./sendtoaddress"
	if strings.ToLower(coin) == "usdc" {
		script = "./sendUSDC"
	}
	cmd := exec.CommandContext(ctx, script, addr, strconv.FormatFloat(amt, 'f', 9, 64))
	cmd.Dir = w.dir
	log.Tracef("Running evmWallet.Send command %q from directory %q", cmd, w.dir)
	b, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("sendtoaddress error with output = %q, err = %v", string(b), err)
	}
	// There's probably a deprecation warning ending in a newline before the txid.
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	jsonTxID := lines[len(lines)-1]
	var txID string
	if err = json.Unmarshal([]byte(jsonTxID), &txID); err != nil {
		return "", fmt.Errorf("error decoding address from %q: %v", jsonTxID, err)
	}
	if common.HexToHash(txID) == (common.Hash{}) {
		return "", fmt.Errorf("output %q did not parse to a tx hash", txID)
	}
	return txID, nil
}

func newWallet(ctx context.Context, symbol string) (w Wallet, err error) {
	switch strings.ToLower(symbol) {
	case "btc", "dcr", "zec":
		w, err = newUtxoWallet(ctx, symbol)
	case "eth", "matic", "polygon":
		w, err = newEvmWallet(ctx, symbol)
	}
	return w, err
}
