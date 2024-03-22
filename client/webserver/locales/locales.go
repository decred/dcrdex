package locales

import (
	"fmt"

	"decred.org/dcrdex/client/intl"
	"golang.org/x/text/language"
)

var (
	Locales map[string]map[string]*intl.Translation
)

// RegisterTranslations registers translations with the init package for
// translator worksheet preparation.
func RegisterTranslations() {
	const callerID = "html"
	for lang, ts := range Locales {
		r := intl.NewRegistrar(callerID, lang, len(ts))
		for translationID, t := range ts {
			r.Register(translationID, t)
		}
	}
}

func init() {
	Locales = map[string]map[string]*intl.Translation{
		"en-US": EnUS,
		"pt-BR": PtBr,
		"zh-CN": ZhCN,
		"pl-PL": PlPL,
		"de-DE": DeDE,
		"ar":    Ar,
	}

	for localeName := range Locales {
		_, err := language.Parse(localeName)
		if err != nil {
			panic(fmt.Sprintf("failed to parse locale name %v: %v", localeName, err))
		}
	}
}
