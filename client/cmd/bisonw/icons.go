// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build systray && !windows

package main

import _ "embed"

//go:embed icons/favicon-32.png
var FavIcon []byte

//go:embed icons/favicon-bw-32.png
var FavIconBW []byte
