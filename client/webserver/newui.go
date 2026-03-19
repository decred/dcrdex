//go:build newui

// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package webserver

import (
	"embed"
	"io/fs"
)

const (
	newUI = true
	// site is the common prefix for the site resources with respect to this
	// webserver package.
	site = "newui"
)

var (
	//go:embed newui/dist newui/src/img newui/src/font
	staticSiteRes embed.FS

	// Unused for New UI
	htmlTmplRes embed.FS
	htmlTmplSub fs.FS
)
