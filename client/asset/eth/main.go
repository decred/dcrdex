package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"os"
	"time"
)

func main() {
	tmpDir, _ := ioutil.TempDir("", "")

	eth, err := NewETH(tmpDir, true)
	if err != nil {
		fmt.Println("NewEth error:", err)
		os.Exit(1)
	}
	defer func() {
		fmt.Println("Qutting")
		eth.shutdown()
		eth.node.Close()
		eth.ethereum.Stop()
		eth.node.Wait()
	}()

	for _, rpc := range eth.ethereum.APIs() {
		fmt.Printf("API namespace = %s: type = %T \n", rpc.Namespace, rpc.Service)
	}

	pw := "pass"
	privB, _ := hex.DecodeString("9447129055a25c8496fca9e5ee1b9463e47e6043ff0c288d07169e8284860e34")
	acct, err := eth.importAccount(pw, privB)
	if err != nil {
		fmt.Print(err)
		os.Exit(1)
	}

	fmt.Println("Account created. Address", acct.Address.Hex())

	bal, err := eth.accountBalance(acct)
	if err != nil {
		fmt.Print(err)
		os.Exit(1)
	}

	fmt.Println("Current account balance:", bal)

	tStart := time.Now()
	for time.Since(tStart) < time.Minute*360 { // 6 hours
		shortCtx, cancel := context.WithTimeout(eth.ctx, time.Second*3)
		defer cancel()
		block, err := eth.client.BlockByNumber(shortCtx, nil)
		if err != nil {
			fmt.Println("GetBlock error:", err)
		} else {
			fmt.Println("Latest block:", block)
		}

		syncCtx, stop := context.WithTimeout(eth.ctx, time.Second*3)
		defer stop()
		syncProgress, err := eth.client.SyncProgress(syncCtx)
		if err != nil {
			fmt.Println("SyncProgress error:", err)
		} else {
			fmt.Printf("Sync Status: %+v \n", syncProgress)
		}
		<-time.After(time.Second * 10)
	}
}
