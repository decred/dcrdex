// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build darwin

package main

import (
	_ "embed"
)

//go:embed src/dexc.png
var FavIcon []byte

//go:embed src/symbol-bw-round.png
var SymbolBWIcon []byte

// On Darwin, we instead use macdriver (not webview) and
// obj.Button().SetImage(cocoa.NSImage_InitWithData.
