//go:build tor

package tor

import _ "embed"

//go:embed build/tor
var torBinary []byte
