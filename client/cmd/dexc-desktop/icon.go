// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build !linux && !windows && !darwin

package main

import (
	_ "embed"
	"github.com/webview/webview"
)

//go:embed src/dexc.png
var FavIcon []byte

//go:embed src/symbol-bw-round.png
var SymbolBWIcon []byte

func useIcon(w webview.WebView, iconPath string) { /* not supported on this platform */ }

func useIconBytes(w webview.WebView, iconBytes []byte) {}
