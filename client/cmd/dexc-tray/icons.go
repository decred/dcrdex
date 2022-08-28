//go:build !windows

package main

import _ "embed"

//go:embed logo_icon_v1.png
var FavIcon []byte
