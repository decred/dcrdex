package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/client/intl"
	"decred.org/dcrdex/client/webserver"
	"decred.org/dcrdex/client/webserver/locales"
	"decred.org/dcrdex/dex"
)

func main() {
	if err := mainErr(); err != nil {
		fmt.Fprint(os.Stderr, err, "\n")
		os.Exit(1)
	}
	os.Exit(0)
}

func mainErr() error {
	var formatFile string
	flag.StringVar(&formatFile, "format", "", "filename. format the completed translation report for Go and print to stdout then quit")
	flag.Parse()

	if formatFile != "" {
		return formatReport(formatFile)
	}

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

func formatReport(reportFile string) error {
	b, err := os.ReadFile(dex.CleanAndExpandPath(reportFile))
	if err != nil {
		return fmt.Errorf("error reading file at %q: %w", reportFile, err)
	}
	var entries []*WorksheetEntry
	if err := json.Unmarshal(b, &entries); err != nil {
		return fmt.Errorf("JSON error for report file at %q: %w", reportFile, err)
	}
	sortedEntries := make(map[string][]*WorksheetEntry, 3)
	entryErrs := make([]string, 0)
	for _, e := range entries {
		if e.Context == "" {
			entryErrs = append(entryErrs, fmt.Sprintf("no context found for report with ID %q", e.ID))
			continue
		}
		sortedEntries[e.Context] = append(sortedEntries[e.Context], e)
	}

	entryErrs = append(entryErrs, printCoreLines(sortedEntries["notifications"])...)
	entryErrs = append(entryErrs, printJSLines(sortedEntries["js"])...)
	entryErrs = append(entryErrs, printHTMLLines(sortedEntries["html"])...)

	if len(entryErrs) == 0 {
		return nil
	}

	fmt.Printf("\nErrors encountered\n")
	for i := range entryErrs {
		fmt.Println(entryErrs[i])
	}

	return errors.New("completed with errors")
}

func printCoreLines(es []*WorksheetEntry) (entryErrs []string) {
	fmt.Printf("\nCore notifications. Put in correct locale map in client/core/locale_ntfn.go\n------------\n")
	noteEntries := make(map[string][2]*WorksheetEntry)
	for _, e := range es {
		var i int
		var entryID string
		switch {
		case strings.HasSuffix(e.ID, " subject"):
			entryID = e.ID[:len(e.ID)-len(" subject")]
		case strings.HasSuffix(e.ID, " template"):
			i = 1
			entryID = e.ID[:len(e.ID)-len(" template")]
		default:
			entryErrs = append(entryErrs, fmt.Sprintf("corrupted ID %q for web notification", e.ID))
			continue
		}
		ne := noteEntries[entryID]
		ne[i] = e
		noteEntries[entryID] = ne
	}

	var unpaired []*WorksheetEntry
	for entryID, st := range noteEntries {
		subjectEntry, templateEntry := st[0], st[1]
		if subjectEntry == nil {
			if templateEntry == nil {
				entryErrs = append(entryErrs, fmt.Sprintf("Missing subject or template for translation with ID %s", entryID))
			} else {
				unpaired = append(unpaired, templateEntry)
			}
			continue
		} else if templateEntry == nil {
			unpaired = append(unpaired, subjectEntry)
			continue
		}
		fmt.Printf(webNoteTemplate, entryID, formatReportWithType(subjectEntry), formatReportWithType(templateEntry))
	}
	for _, e := range unpaired {
		fmt.Println("Topic"+e.ID, ": ", formatReportWithType(e)+",")
	}

	return
}

func printJSLines(es []*WorksheetEntry) (entryErrs []string) {
	fmt.Printf("\nJS Notifications. Add these to the appropriate map in client/webserver/jsintl.go, replacing the IDs with the appropriate variables\n------------\n")
	for _, e := range es {
		fmt.Printf("	%s: %s,\n", e.ID, formatReportEntry(e))
	}
	return
}

func printHTMLLines(es []*WorksheetEntry) (entryErrs []string) {
	fmt.Printf("\nHTML Notifications. Add these to the language file in client/webserver/locales\n------------\n")
	for _, e := range es {
		fmt.Printf(`	"%s": %s,`+"\n", e.ID, formatReportEntry(e))
	}
	return
}

func escapeTemplate(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func formatReportEntry(e *WorksheetEntry) string {
	var v, n string
	if e.Version > 0 {
		v = fmt.Sprintf("Version: %d, ", e.Version)
	}
	t := escapeTemplate(e.Translation)
	if e.Notes != "" {
		n = fmt.Sprintf(", Notes: %s", escapeTemplate(e.Notes))
	}
	return fmt.Sprintf("{%sT: %s%s}", v, t, n)
}

func formatReportWithType(e *WorksheetEntry) string {
	return fmt.Sprintf("intl.Translation%s", formatReportEntry(e))
}

var webNoteTemplate = `	Topic%s: {
		subject:  %s,
		template: %s,
	},
`
