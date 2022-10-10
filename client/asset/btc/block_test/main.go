package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/rpcclient"
)

var host = flag.String("host", "127.0.0.1:8332", "node RPC host:port")
var user = flag.String("user", "", "node RPC username")
var pass = flag.String("pass", "", "node RPC password")

func main() {
	os.Exit(mainCore())
}

func mainCore() int {
	flag.Parse()

	client, err := rpcclient.New(&rpcclient.ConnConfig{
		HTTPPostMode: true,
		DisableTLS:   true,
		Host:         *host,
		User:         *user,
		Pass:         *pass,
	}, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: Unable to connect to RPC server: %v\n", err)
		return 1
	}
	defer client.Shutdown()

	infoResult, err := client.GetNetworkInfo()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: GetInfo failed: %v\n", err)
		return 1
	}
	fmt.Printf("Node connection count: %d\n", infoResult.Connections)

	hash, err := client.GetBestBlockHash()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: GetBestBlockHash failed: %v\n", err)
		return 2
	}
	hdr, err := client.GetBlockHeaderVerbose(hash)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: GetBestBlockHash failed: %v\n", err)
		return 2
	}
	fmt.Println(hdr.Height, hash)

	// block with tx with big witness item that fails for old btcd/wire
	bigBlockHash, _ := chainhash.NewHashFromStr("0000000000000000000400a35a007e223a7fb8a622dc7b5aa5eaace6824291fb")
	msgBlock, err := client.GetBlock(bigBlockHash)
	if err != nil {
		fmt.Println(err)
		return 1
	}
	fmt.Println(msgBlock.BlockHash())

	return 0
}
