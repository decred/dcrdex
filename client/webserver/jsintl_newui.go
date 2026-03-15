//go:build newui

// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package webserver

import (
	"decred.org/dcrdex/client/intl"
	"decred.org/dcrdex/client/webserver/newui/locales"
)

var localesMap = locales.Locales

// RegisterTranslations registers translations with the init package for
// translator worksheet preparation.
func RegisterTranslations() {
	const callerID = "js"

	for lang, ts := range localesMap {
		r := intl.NewRegistrar(callerID, lang, len(ts))
		for translationID, t := range ts {
			r.Register(translationID, t)
		}
	}
}
