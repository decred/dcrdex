package main

import (
	"decred.org/dcrdex/server/asset"
	"decred.org/dcrdex/server/asset/dcr"
)

func init() {
	asset.Register(dcr.AssetName, &dcr.Driver{})
}
