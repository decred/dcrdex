package importall

import (
	_ "decred.org/dcrdex/client/asset/bch"  // register bch asset
	_ "decred.org/dcrdex/client/asset/btc"  // register btc asset
	_ "decred.org/dcrdex/client/asset/dash" // register dash asset
	_ "decred.org/dcrdex/client/asset/dcr"  // register dcr asset
	_ "decred.org/dcrdex/client/asset/dgb"  // register dgb asset
	_ "decred.org/dcrdex/client/asset/doge" // register doge asset
	_ "decred.org/dcrdex/client/asset/firo" // register firo asset
	_ "decred.org/dcrdex/client/asset/ltc"  // register ltc asset
	_ "decred.org/dcrdex/client/asset/zec"  // register zec asset
	// nixed
	// _ "decred.org/dcrdex/client/asset/zcl"  // register zcl asset
)
