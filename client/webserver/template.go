// Copyright (c) 2018-2019, The Decred developers
// Copyright (c) 2017, The dcrdata developers
// See LICENSE for details.

package webserver

import (
	"fmt"
	"html/template"
	"net/url"
	"path/filepath"
	"strings"
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
	exec      func(string, interface{}) (string, error)
	addErr    error
}

// newTemplates constructs a new templates.
func newTemplates(folder string, reload bool) *templates {
	t := &templates{
		templates: make(map[string]pageTemplate),
		folder:    folder,
	}
	t.exec = t.execTemplateToString
	if reload {
		t.exec = t.execWithReload
	}
	return t
}

// filepath constructs the template path from the template ID.
func (t *templates) filepath(name string) string {
	return filepath.Join(t.folder, name+".tmpl")
}

// addTemplate adds a new template. The template is specified with a name, which
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
	temp, err := template.New(name).Funcs(templateFuncs).ParseFiles(files...)
	if err != nil {
		t.addErr = fmt.Errorf("error adding template %s: %v", name, err)
		return t
	}
	t.templates[name] = pageTemplate{
		preloads: preloads,
		template: temp,
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

// reloadTemplates relaods all templates. Use this during front-end development
// when editing templates.
func (t *templates) reloadTemplates() error {
	var errorStrings []string
	for name, tmpl := range t.templates {
		t.addTemplate(name, tmpl.preloads...)
		if t.buildErr() != nil {
			log.Errorf(t.buildErr().Error())
		}
	}
	if errorStrings == nil {
		return nil
	}
	return fmt.Errorf(strings.Join(errorStrings, " | "))
}

// execTemplateToString executes the specified input template using the
// supplied data, and writes the result into a string. If the template fails to
// execute or isn't found, a non-nil error will be returned. Check it before
// writing to the client, otherwise you might as well execute directly into
// your response writer instead of the internal buffer of this function.
//
// DRAFT NOTE: Might consider writing directly to the the buffer here. Could
// still set the error code appropriately.
func (t *templates) execTemplateToString(name string, data interface{}) (string, error) {
	temp, ok := t.templates[name]
	if !ok {
		return "", fmt.Errorf("Template %s not known", name)
	}
	var page strings.Builder
	err := temp.template.ExecuteTemplate(&page, name, data)
	return page.String(), err
}

// execWithReload is the same as execTemplateToString, but will reload the
// template first.
func (t *templates) execWithReload(name string, data interface{}) (string, error) {
	tmpl, found := t.templates[name]
	if !found {
		return "", fmt.Errorf("template %s not found", name)
	}
	t.addTemplate(name, tmpl.preloads...)
	log.Debugf("reloaded HTML template %q", name)
	return t.execTemplateToString(name, data)
}

// marketLogoPath takes a logo image path for the specified index 0 or 1 of the
// market formatted like ABC-DEF.
func marketLogoPath(mktPath string, idx int) string {
	pair := strings.Split(mktPath, "-")
	if len(pair) != 2 || idx > 1 || idx < 0 {
		log.Errorf("marketLogoPath called with invalid parameters. %d, %d", len(pair), idx)
		return ""
	}
	return "/img/coins/" + strings.ToLower(pair[idx]) + ".png"
}

// templateFuncs are able to be called during template execution.
var templateFuncs = template.FuncMap{
	// baseLogoPath gets the logo image path for the base asset of the specified
	// market.
	"baseLogoPath": func(mkt string) string {
		return marketLogoPath(mkt, 0)
	},
	// quoteLogoPath gets the logo image path for the quote asset of the specified
	// market.
	"quoteLogoPath": func(mkt string) string {
		return marketLogoPath(mkt, 1)
	},
	// urlBase attempts to get the domain name without the TLD.
	"urlBase": func(uri string) string {
		u, err := url.Parse(uri)
		if err != nil {
			log.Errorf("failed to parse URL: %s", uri)
		}
		return strings.TrimSuffix(u.Host, filepath.Ext(u.Host))
	},
}
