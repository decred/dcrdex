package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/client/intl"
	"decred.org/dcrdex/client/webserver"
	"decred.org/dcrdex/client/webserver/locales"
)

func main() {
	if err := mainErr(); err != nil {
		fmt.Fprint(os.Stderr, err, "\n")
		os.Exit(1)
	}
	os.Exit(0)
}

func mainErr() error {
	core.RegisterTranslations()
	webserver.RegisterTranslations()
	locales.RegisterTranslations()

	reports := intl.Report()
	if err := os.MkdirAll("worksheets", 0755); err != nil {
		return fmt.Errorf("error making worksheets directory: %v", err)
	}

	for lang, r := range reports {
		if lang == "en-US" {
			continue
		}
		worksheet := make([]*WorksheetEntry, 0)
		for callerID, ts := range r.Missing {
			for translationID, t := range ts {
				worksheet = append(worksheet, &WorksheetEntry{
					ID:      translationID,
					Version: t.Version,
					Context: callerID,
					Notes:   t.Notes,
					English: t.T,
				})
			}
		}

		worksheetPath := filepath.Join("worksheets", lang+".json")
		f, err := os.OpenFile(worksheetPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0644)
		if err != nil {
			return fmt.Errorf("OpenFile(%q) error: %w", worksheetPath, err)
		}

		enc := json.NewEncoder(f)
		enc.SetEscapeHTML(false) // This is why we're using json.Encoder
		enc.SetIndent("", "    ")
		err = enc.Encode(worksheet)
		f.Close()
		if err != nil {
			return fmt.Errorf("json encode error: %w", err)
		}
	}
	return nil
}

type WorksheetEntry struct {
	ID          string `json:"id"`
	Version     int    `json:"version,omitempty"`
	Context     string `json:"context"`
	Notes       string `json:"notes,omitempty"`
	English     string `json:"english"`
	Translation string `json:"translation"`
}
