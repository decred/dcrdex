// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package i18n

import (
	"fmt"

	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

type Translation struct {
	Subject  string
	Template string
	// stale is used to indicate that a translation has changed, is only
	// partially translated, or just needs review, and should be updated. This
	// is useful when it's better than falling back to english, but it allows
	// these translations to be identified programmatically.
	Stale bool
}

const OriginLang = "en-US"

type DocumentedTranslation struct {
	*Translation
	Docs string
}

var (
	pkgTranslators = make(map[string]*BaseTranslator)
)

type BaseTranslator struct {
	pkg         string
	defaultLang language.Tag
	dicts       map[language.Tag]map[string]*Translation
	dict        map[string]*Translation
	printer     *message.Printer
	origin      map[string]*DocumentedTranslation
}

func NewPackageTranslator(pkg string, defaultLang language.Tag) *BaseTranslator {
	t := &BaseTranslator{
		pkg:         pkg,
		defaultLang: defaultLang,
		dicts:       map[language.Tag]map[string]*Translation{defaultLang: {}},
		printer:     message.NewPrinter(defaultLang),
	}
	t.dict = t.dicts[defaultLang]
	t.origin = make(map[string]*DocumentedTranslation)
	pkgTranslators[pkg] = t
	return t
}

func (t *BaseTranslator) Register(topic string, tln *DocumentedTranslation) {
	t.dicts[t.defaultLang][topic] = tln.Translation
	t.origin[topic] = tln
	if err := message.SetString(t.defaultLang, topic, tln.Template); err != nil {
		panic(fmt.Sprintf("SetString(%s): %v", t.defaultLang, err))
	}
}

func (t *BaseTranslator) Format(topic string, args ...interface{}) (subject string, msg string) {
	trans, found := t.dict[topic]
	if !found {
		baseTrans, found := t.origin[topic]
		if !found {
			return topic, "translation error"
		}
		trans = baseTrans.Translation
	}
	return trans.Subject, t.printer.Sprintf(topic, args...)
}

func (t *BaseTranslator) SetLanguage(lang language.Tag) {
	t.dict = t.dicts[lang]
	t.printer = message.NewPrinter(lang)
}

func (t *BaseTranslator) Langs() []language.Tag {
	langs := make([]language.Tag, 0, len(t.dicts))
	for lang := range t.dicts {
		langs = append(langs, lang)
	}
	return langs
}

type Translator struct {
	*BaseTranslator
	lang language.Tag
	dict map[string]*Translation
}

func (t *BaseTranslator) LanguageTranslator(lang language.Tag) *Translator {
	dict := make(map[string]*Translation)
	t.dicts[lang] = dict

	return &Translator{
		BaseTranslator: t,
		lang:           lang,
		dict:           dict,
	}
}

func (t *Translator) Register(topic string, tln *Translation) {
	t.dicts[t.lang][topic] = tln
	if err := message.SetString(t.lang, topic, tln.Template); err != nil {
		panic(fmt.Sprintf("SetString(%s): %v", t.lang, err))
	}
}

// CheckTopicLangs is used to report missing notification translations.
func CheckTopicLangs() (missing, stale map[language.Tag][]string) {
	const pkg = "core"
	var originLang = language.AmericanEnglish
	bt := pkgTranslators[pkg]

	missing = make(map[language.Tag][]string, len(bt.dicts)-1)
	stale = make(map[language.Tag][]string, len(bt.dicts)-1)

	for lang, translations := range bt.dicts {
		if lang == originLang {
			continue
		}
		var missingTopics, staleTopics []string
		for topic := range bt.origin {
			t, found := translations[topic]
			if !found {
				missingTopics = append(missingTopics, topic)
			} else if t.Stale {
				staleTopics = append(staleTopics, topic)
			}
		}
		if len(missingTopics) > 0 {
			missing[lang] = missingTopics
		}
		if len(staleTopics) > 0 {
			stale[lang] = staleTopics
		}
	}

	return
}
