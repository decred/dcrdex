// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build !linux && !windows && !darwin

package main

import (
	_ "embed"

	"github.com/bisoncraft/webview_go"
)

//go:embed src/bisonw.png
var Icon []byte

//go:embed src/favicon-32.png
var FavIcon []byte

//go:embed src/favicon-bw-32.png
var FavIconBW []byte

func useIcon(w webview.WebView, iconPath string) { /* not supported on this platform */ }

func useIconBytes(w webview.WebView, iconBytes []byte) {}
