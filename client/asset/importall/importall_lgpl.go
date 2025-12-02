package importall

import (
	// Base is disable until we get L1 security fees worked out.
	// _ "decred.org/dcrdex/client/asset/base"    // register base network
	_ "decred.org/dcrdex/client/asset/eth"     // register eth asset
	_ "decred.org/dcrdex/client/asset/polygon" // register polygon network
)
