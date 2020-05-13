package main

import (
	"decred.org/dcrdex/server/asset"
	"decred.org/dcrdex/server/asset/btc"
)

func init()  {
	asset.Register(btc.AssetName, &btc.Driver{})
}