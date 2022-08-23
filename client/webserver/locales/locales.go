package locales

import (
	"fmt"

	"golang.org/x/text/language"
)

var (
	Locales map[string]map[string]string
)

func init() {
	Locales = map[string]map[string]string{
		"en-US": EnUS,
		"pt-BR": PtBr,
		"zh-CN": ZhCN,
		"pl-PL": PlPL,
		"de-DE": DeDE,
	}

	for localeName := range Locales {
		_, err := language.Parse(localeName)
		if err != nil {
			panic(fmt.Sprintf("failed to parse locale name %v: %v", localeName, err))
		}
	}
}
