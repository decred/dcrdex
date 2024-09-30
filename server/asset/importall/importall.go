package importall

import (
	_ "decred.org/dcrdex/server/asset/bch"  // register bch asset
	_ "decred.org/dcrdex/server/asset/btc"  // register btc asset
	_ "decred.org/dcrdex/server/asset/dash" // register dash asset
	_ "decred.org/dcrdex/server/asset/dcr"  // register dcr asset
	_ "decred.org/dcrdex/server/asset/dgb"  // register dgb asset
	_ "decred.org/dcrdex/server/asset/doge" // register doge asset
	_ "decred.org/dcrdex/server/asset/firo" // register firo asset
	_ "decred.org/dcrdex/server/asset/ltc"  // register ltc asset
	_ "decred.org/dcrdex/server/asset/zec"  // register zec asset
	// nixed
	// _ "decred.org/dcrdex/server/asset/zcl"  // register zcl asset
)
