package importall

import (
	_ "decred.org/dcrdex/client/asset/base"    // register base network
	_ "decred.org/dcrdex/client/asset/eth"     // register eth asset
	_ "decred.org/dcrdex/client/asset/polygon" // register polygon network
)
