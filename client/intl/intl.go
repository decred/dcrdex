// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package intl

const defaultLanguage = "en-US"

// Trasnlation is a versioned localized string. Notes added to the english
// translation will be presented to human translators.
type Translation struct {
	Version int
	T       string
	Notes   string // english only
}

var translations = map[string] /* lang */ map[string] /* caller ID */ map[string] /* translation ID */ *Translation{}

// Registrar is used for registering translations for a specific language and
// caller context.
type Registrar struct {
	callerID string
	lang     string
	m        map[string]*Translation
}

// NewRegistrar constructs a Registrar.
func NewRegistrar(callerID string, lang string, preAlloc int) *Registrar {
	callers, found := translations[lang]
	if !found {
		callers = make(map[string]map[string]*Translation)
		translations[lang] = callers
	}
	m := make(map[string]*Translation, preAlloc)
	callers[callerID] = m
	return &Registrar{
		callerID: callerID,
		lang:     lang,
		m:        m,
	}
}

// Register registers a translation.
func (r *Registrar) Register(translationID string, t *Translation) {
	r.m[translationID] = t
}

// TranslationReport is a report of missing and extraneous translations.
type TranslationReport struct {
	Missing map[string] /* caller ID */ map[string]*Translation // English translation
	Extras  []string                                            /* caller ID */
}

// Report generates a TranslationReport for each registered language.
func Report() map[string] /* lang */ *TranslationReport {
	reports := make(map[string]*TranslationReport)

	for lang := range translations {
		if lang == defaultLanguage {
			continue
		}
		reports[lang] = &TranslationReport{
			Missing: make(map[string]map[string]*Translation),
		}
	}
	enUS := translations[defaultLanguage]
	for callerID, enTranslations := range enUS {
		for lang, callers := range translations {
			if lang == defaultLanguage {
				continue
			}
			r := reports[lang]
			ts := callers[callerID]
			if ts == nil {
				ts = make(map[string]*Translation)
			}
			missing := r.Missing[callerID]
			if missing == nil {
				missing = make(map[string]*Translation)
				r.Missing[callerID] = missing
			}
			for translationID, enTranslation := range enTranslations {
				t := ts[translationID]
				if t == nil || t.Version < enTranslation.Version {
					missing[translationID] = enTranslation
				}
			}
		}
	}

	for lang, callers := range translations {
		if lang == defaultLanguage {
			continue
		}
		r := reports[lang]
		for callerID, ts := range callers {
			enTranslations := enUS[callerID]
			for translationID := range ts {
				if enTranslations[translationID] == nil {
					r.Extras = append(r.Extras, callerID+" -> "+translationID)
				}
			}
		}
	}

	return reports
}
