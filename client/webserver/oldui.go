//go:build !newui

// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package webserver

import (
	"embed"
	"io/fs"
)

const (
	newUI = false
	// site is the common prefix for the site resources with respect to this
	// webserver package.
	site = "site"
)

var (
	//go:embed site/dist site/src/img site/src/font
	staticSiteRes embed.FS

	//go:embed site/src/html/*.tmpl
	htmlTmplRes    embed.FS
	htmlTmplSub, _ = fs.Sub(htmlTmplRes, "site/src/html") // unrooted slash separated path as per io/fs.ValidPath
)
