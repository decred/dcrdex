// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package webserver

import (
	"bytes"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"strings"

	"decred.org/dcrdex/client/webserver/locales"
)

// pageTemplate holds the information necessary to process a template. Also
// holds information necessary to reload the templates for development.
type pageTemplate struct {
	preloads []string
	template *template.Template
}

// templates is a template processor.
type templates struct {
	templates map[string]pageTemplate
	folder    string
	locale    map[string]string
	embedded  bool // used in exec and addTemplate
	addErr    error
}

// newTemplates constructs a new templates.
func newTemplates(folder, lang string, embedded bool) *templates {
	t := &templates{
		templates: make(map[string]pageTemplate),
		folder:    folder,
		embedded:  embedded,
	}
	// t.locale is only used in retranslate, which currently only applies when
	// not using the embedded site resources, but check the lang anyway. TODO:
	// We should aim to embed only the untranslated template, and translate
	// those in-memory on template creation.
	var found bool
	if t.locale, found = locales.Locales[lang]; !found {
		t.addErr = fmt.Errorf("no translation dictionary found for lang %q", lang)
		return t
	}
	if embedded {
		return t
	}

	// Make sure we have the expected directory structure.
	if !folderExists(t.srcDir()) {
		t.addErr = fmt.Errorf("no-embed-site set but source directory not found at %s", t.srcDir())
	}

	return t
}

// filepath constructs the template path from the template ID.
func (t *templates) filepath(name string) string {
	return filepath.Join(t.folder, name+".tmpl")
}

// srcDir is the expected directory of the translation source templates. Only
// used in development when the --no-embed-site flag is used.
// <root>/localized_html/[lang] -> <root>/html
func (t *templates) srcDir() string {
	return filepath.Join(filepath.Dir(filepath.Dir(t.folder)), "html")
}

// srcPath is the path translation source. Only used in
// development when the --no-embed-site flag is used.
func (t *templates) srcPath(name string) string {
	return filepath.Join(t.srcDir(), name+".tmpl")
}

// retranslate rebuilds the localized html template. Only used in development
// when the --no-embed-site flag is used.
func (t *templates) retranslate(name string, preloads ...string) error {
	for _, iName := range append(preloads, name) {
		srcPath := t.srcPath(iName)
		rawTmpl, err := os.ReadFile(srcPath)
		if err != nil {
			return fmt.Errorf("ReadFile error: %w", err)
		}
		destTmpl := make([]byte, len(rawTmpl))
		copy(destTmpl, rawTmpl)
		for _, matchGroup := range locales.Tokens(rawTmpl) {
			if len(matchGroup) != 2 {
				return fmt.Errorf("can't parse match group: %v", matchGroup)
			}
			token, key := matchGroup[0], string(matchGroup[1])
			replacement, found := t.locale[key]
			if !found {
				replacement, found = locales.EnUS[key]
				if !found {
					return fmt.Errorf("warning: no translation text for key %q", key)
				}
			}
			destTmpl = bytes.Replace(destTmpl, token, []byte(replacement), -1)
		}

		if err := os.WriteFile(t.filepath(iName), destTmpl, 0644); err != nil {
			return fmt.Errorf("error writing localized template %s: %v", t.filepath(iName), err)
		}
	}
	return nil
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
	files := make([]string, 0, len(preloads)+1)
	for i := range preloads {
		files = append(files, t.filepath(preloads[i]))
	}
	files = append(files, t.filepath(name))

	tmpl := template.New(name).Funcs(templateFuncs)
	var err error
	if t.embedded {
		tmpl, err = tmpl.ParseFS(siteRes, files...)
	} else {
		tmpl, err = tmpl.ParseFiles(files...)
	}
	if err != nil {
		t.addErr = fmt.Errorf("error adding template %s: %w", name, err)
		return t
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

// exec executes the specified input template using the supplied data, and
// writes the result into a string. If the template fails to execute or isn't
// found, a non-nil error will be returned. Check it before writing to the
// client, otherwise you might as well execute directly into your response
// writer instead of the internal buffer of this function.
//
// The template will be reloaded if using on-disk (not embedded) templates.
//
// DRAFT NOTE: Might consider writing directly to the the buffer here. Could
// still set the error code appropriately.
func (t *templates) exec(name string, data interface{}) (string, error) {
	tmpl, found := t.templates[name]
	if !found {
		return "", fmt.Errorf("template %q not found", name)
	}

	if !t.embedded {
		err := t.retranslate(name, tmpl.preloads...)
		if err != nil {
			return "", err
		}

		t.addTemplate(name, tmpl.preloads...)
		log.Debugf("reloaded HTML template %q", name)
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
}
