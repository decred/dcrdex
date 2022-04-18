package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"decred.org/dcrdex/client/webserver/locales"
)

var (
	workingDirectory, _ = os.Getwd()
)

func main() {
	siteDir := workingDirectory
	if filepath.Base(workingDirectory) == "template-builder" {
		siteDir = filepath.Dir(workingDirectory)
	}

	templateDir := filepath.Join(siteDir, "src", "html")
	outputDirectory := filepath.Join(siteDir, "src", "localized_html")

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

	err = filepath.Walk(templateDir, func(_ string, fi os.FileInfo, err error) error {
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
		fmt.Println(baseName)
		rawTmplPath := filepath.Join(templateDir, baseName)
		rawTmpl, err := os.ReadFile(rawTmplPath)
		if err != nil {
			return fmt.Errorf("ReadFile error: %w", err)
		}

		localizedTemplates := make(map[string][]byte, len(locales.Locales))
		enDict := locales.Locales["en-US"]

		for lang := range locales.Locales {
			fmt.Println("Prepping", lang)
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
				dict := locales.Locales[lang]
				replacement, found := dict[key]
				if !found {
					replacement, found = enDict[key]
					if !found {
						return fmt.Errorf("no translation and no default replacement for %s", key)
					}
					fmt.Printf("Warning: no %s replacement text for key %q, using 'en' value %s \n", lang, key, replacement)
				}
				localizedTemplates[lang] = bytes.Replace(tmpl, token, []byte(replacement), -1) // Could just do 1
			}
		}

		for lang, tmpl := range localizedTemplates {
			langDir := filepath.Join(outputDirectory, lang)
			localizedName := filepath.Join(langDir, baseName)
			// ext := filepath.Ext(d.Name())
			// name := baseName[:len(baseName)-len(ext)]
			// localizedName := filepath.Join(outputDirectory, name+"_"+lang+ext)
			fmt.Println("Writing", localizedName)
			if err := os.WriteFile(localizedName, tmpl, 0644); err != nil {
				return fmt.Errorf("error writing localized template %s: %v", localizedName, err)
			}
		}
		return nil
	})
	if err != nil {
		fmt.Println("WalkDir error:", err)
		os.Exit(3)
	}
}
