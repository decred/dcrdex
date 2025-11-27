package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"decred.org/dcrdex/client/intl"
	"decred.org/dcrdex/client/webserver/locales"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

var (
	workingDirectory, _ = os.Getwd()
	titler              cases.Caser
)

func main() {
	write := flag.Bool("write", false, "write out translated html templates, otherwise just check (default false)")
	siteDir := workingDirectory
	if filepath.Base(workingDirectory) == "template-builder" {
		siteDir = filepath.Dir(workingDirectory)
	}

	templateDir := filepath.Join(siteDir, "src", "html")
	outputDirectory := filepath.Join(siteDir, "src", "localized_html")

	if *write {
		fmt.Println("Creating output directory:", outputDirectory)
		err := os.MkdirAll(outputDirectory, 0755)
		if err != nil {
			fmt.Printf("MkdirAll %q error: %v \n", outputDirectory, err)
			os.Exit(1)
		}

		for lang := range locales.Locales {
			langDir := filepath.Join(outputDirectory, lang)
			err := os.MkdirAll(langDir, 0755)
			if err != nil {
				fmt.Printf("MkdirAll %q error: %v \n", langDir, err)
				os.Exit(2)
			}
		}
	}

	err := filepath.Walk(templateDir, func(_ string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if fi.IsDir() {
			return nil
		}
		baseName := fi.Name()
		if filepath.Ext(baseName) != ".tmpl" {
			return nil
		}
		rawTmplPath := filepath.Join(templateDir, baseName)
		rawTmpl, err := os.ReadFile(rawTmplPath)
		if err != nil {
			return fmt.Errorf("ReadFile error: %w", err)
		}

		localizedTemplates := make(map[string][]byte, len(locales.Locales))
		enDict := locales.Locales["en-US"]

		for lang := range locales.Locales {
			tmpl := make([]byte, len(rawTmpl))
			copy(tmpl, rawTmpl)
			localizedTemplates[lang] = tmpl
		}

		for _, matchGroup := range locales.Tokens(rawTmpl) {
			if len(matchGroup) != 2 {
				return fmt.Errorf("can't parse match group: %v", matchGroup)
			}
			token, key := matchGroup[0], string(matchGroup[1])
			for lang, tmpl := range localizedTemplates {
				titler = cases.Title(language.MustParse(lang))
				dict := locales.Locales[lang]

				var toTitle bool
				var found bool
				var replacement *intl.Translation
				if titleKey, ok := strings.CutPrefix(key, ":title:"); ok {
					// Check if there's a value for :title:key. Especially for languages
					// that do not work well with cases.Caser, e.g zh-cn.
					if replacement, found = dict[key]; !found {
						toTitle = true
						key = titleKey
					}
				}

				if !found {
					if replacement, found = dict[key]; !found {
						if replacement, found = enDict[key]; !found {
							return fmt.Errorf("no %s translation in %q and no default replacement for %s", lang, baseName, key)
						}
					}
				}

				if toTitle {
					replacement.T = titler.String(replacement.T)
				}

				localizedTemplates[lang] = bytes.Replace(tmpl, token, []byte(replacement.T), -1) // Could just do 1
			}
		}

		if *write {
			for lang, tmpl := range localizedTemplates {
				langDir := filepath.Join(outputDirectory, lang)
				localizedName := filepath.Join(langDir, baseName)
				if err := os.WriteFile(localizedName, tmpl, 0644); err != nil {
					return fmt.Errorf("error writing localized template %s: %v", localizedName, err)
				}
			}
		}
		return nil
	})
	if err != nil {
		fmt.Println("WalkDir error:", err)
		os.Exit(3)
	}
}
