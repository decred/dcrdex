package importall

import (
	_ "decred.org/dcrdex/server/asset/base"    // register base asset
	_ "decred.org/dcrdex/server/asset/eth"     // register eth asset
	_ "decred.org/dcrdex/server/asset/polygon" // register polygon asset
)
