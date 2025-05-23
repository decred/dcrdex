// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package webserver

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"html/template"
	"io/fs"
	"os"
	"strings"

	"runtime/debug"

	"decred.org/dcrdex/client/intl"
	"decred.org/dcrdex/client/webserver/locales"
	"decred.org/dcrdex/dex/encode"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// pageTemplate holds the information necessary to process a template. Also
// holds information necessary to reload the templates for development.
type pageTemplate struct {
	preloads []string
	template *template.Template
}

// templates is a template processor.
type templates struct {
	templates    map[string]pageTemplate
	fs           fs.FS // must contain tmpl files at root
	reloadOnExec bool
	dict         map[string]*intl.Translation
	titler       cases.Caser

	addErr error
}

// newTemplates constructs a new templates.
func newTemplates(folder, lang string) *templates {
	embedded := folder == ""
	t := &templates{
		templates:    make(map[string]pageTemplate),
		reloadOnExec: !embedded,
	}

	var found bool
	if t.dict, found = locales.Locales[lang]; !found {
		t.addErr = fmt.Errorf("no translation dictionary found for lang %q", lang)
		return t
	}
	t.titler = cases.Title(language.MustParse(lang))

	if embedded {
		t.fs = htmlTmplSub
		return t
	}

	if !folderExists(folder) {
		t.addErr = fmt.Errorf("not using embedded site, but source directory not found at %s", folder)
	}
	t.fs = os.DirFS(folder)

	return t
}

// translate a template file.
func (t *templates) translate(name string) (string, error) {
	rawTmpl, err := fs.ReadFile(t.fs, name+".tmpl")
	if err != nil {
		return "", fmt.Errorf("ReadFile error: %w", err)
	}

	for _, matchGroup := range locales.Tokens(rawTmpl) {
		if len(matchGroup) != 2 {
			return "", fmt.Errorf("can't parse match group: %v", matchGroup)
		}
		token, key := matchGroup[0], string(matchGroup[1])

		var toTitle bool
		var found bool
		var replacement *intl.Translation
		if titleKey := strings.TrimPrefix(key, ":title:"); titleKey != key {
			// Check if there's a value for :title:key. Especially for languages
			// that do not work well with cases.Caser, e.g zh-cn.
			if replacement, found = t.dict[key]; !found {
				toTitle = true
				key = titleKey
			}
		}

		if !found {
			if replacement, found = t.dict[key]; !found {
				if replacement, found = locales.EnUS[key]; !found {
					return "", fmt.Errorf("warning: no translation text for key %q", key)
				}
			}
		}

		if toTitle {
			replacement.T = t.titler.String(replacement.T)
		}

		rawTmpl = bytes.ReplaceAll(rawTmpl, token, []byte(replacement.T))
	}

	return string(rawTmpl), nil
}

// addTemplate adds a new template. It can be embed from the binary or not, to
// help with development. The template is specified with a name, which
// must also be the base name of a file in the templates folder. Any preloads
// must also be base names of files in the template folder, which will be loaded
// in order before the name template is processed. addTemplate returns itself
// and defers errors to facilitate chaining.
func (t *templates) addTemplate(name string, preloads ...string) *templates {
	if t.addErr != nil {
		return t
	}

	tmpl := template.New(name).Funcs(templateFuncs)

	// Translate and parse each template for this page.
	for _, subName := range append(preloads, name) {
		localized, err := t.translate(subName)
		if err != nil {
			t.addErr = fmt.Errorf("error translating templates: %w", err)
			return t
		}

		tmpl, err = tmpl.Parse(localized)
		if err != nil {
			t.addErr = fmt.Errorf("error adding template %s: %w", name, err)
			return t
		}
	}

	t.templates[name] = pageTemplate{
		preloads: preloads,
		template: tmpl,
	}
	return t
}

// buildErr returns any error encountered during addTemplate. The error is
// cleared.
func (t *templates) buildErr() error {
	err := t.addErr
	t.addErr = nil
	return err
}

var commitHash = func() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" && len(setting.Value) >= 8 {
				return setting.Value
			}
		}
	}

	return ""
}()

// exec executes the specified input template using the supplied data, and
// writes the result into a string. If the template fails to execute or isn't
// found, a non-nil error will be returned. Check it before writing to the
// client, otherwise you might as well execute directly into your response
// writer instead of the internal buffer of this function.
//
// The template will be reloaded if using on-disk (not embedded) templates.
//
// DRAFT NOTE: Might consider writing directly to the buffer here. Could
// still set the error code appropriately.
func (t *templates) exec(name string, data any) (string, error) {
	tmpl, found := t.templates[name]
	if !found {
		return "", fmt.Errorf("template %q not found", name)
	}

	if t.reloadOnExec {
		// Retranslate and re-parse the template.
		if err := t.addTemplate(name, tmpl.preloads...).buildErr(); err != nil {
			// No need to return the error because we still want to display the
			// page.
			log.Errorf("Failed to reload HTML template %q: %v", name, err)
		} else {
			log.Debugf("reloaded HTML template %q", name)

			// Grab the new pageTemplate
			tmpl = t.templates[name]
		}
	}

	var page strings.Builder
	err := tmpl.template.ExecuteTemplate(&page, name, data)
	return page.String(), err
}

// templateFuncs are able to be called during template execution.
var templateFuncs = template.FuncMap{
	"toUpper": strings.ToUpper,
	// logoPath gets the logo image path for the base asset of the specified
	// market.
	"logoPath": func(symbol string) string {
		return "/img/coins/" + strings.ToLower(symbol) + ".png"
	},
	"x100": func(v float32) float32 {
		return v * 100
	},
	"dummyExchangeLogo": func(host string) string {
		if len(host) == 0 {
			return "/img/coins/z.png"
		}
		char := host[0]
		if char < 97 || char > 122 {
			return "/img/coins/z.png"
		}
		return "/img/coins/" + string(char) + ".png"
	},
	"baseAssetSymbol": func(symbol string) string {
		parts := strings.Split(symbol, ".")
		if len(parts) == 0 {
			return "wtf"
		}
		return parts[0]
	},
	"commitHash": func(allowRandom bool) string {
		if commitHash != "" {
			return commitHash[:8]
		}

		if allowRandom {
			// If the commit hash is not available, return a random string.
			// This is useful in invalidating JS and CSS file caches.
			return hex.EncodeToString(encode.RandomBytes(4))
		}

		return "unknown"
	},
}
