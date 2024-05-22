// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build darwin

package main

import (
	_ "embed"
)

//go:embed src/bisonw.png
var Icon []byte

//go:embed src/favicon-32.png
var FavIcon []byte // darwin doesn't use these

//go:embed src/favicon-bw-32.png
var FavIconBW []byte // darwin doesn't use these

// On Darwin, we instead use macdriver (not webview) and
// obj.Button().SetImage(cocoa.NSImage_InitWithData.
