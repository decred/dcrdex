package main

import (
	"decred.org/dcrdex/server/asset"
	"decred.org/dcrdex/server/asset/ltc"
)

func init() {
	asset.Register(ltc.AssetName, &ltc.Driver{})
}
