// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build systray && !windows

package main

import _ "embed"

//go:embed icons/logo_icon_v1.png
var FavIcon []byte

//go:embed icons/symbol-bw-round.png
var SymbolBWIcon []byte
