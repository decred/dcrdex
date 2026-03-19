package main

import (
	"bytes"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"decred.org/dcrdex/client/intl"
	oldloc "decred.org/dcrdex/client/webserver/locales"
	newloc "decred.org/dcrdex/client/webserver/newui/locales"
)

/*
importlocales looks for entries in the EnUS translations map that don't have
translations in other maps, and then checks the locales from the old UI to
see if there is a translation there that can be used, and updates the files.
*/

type mapMatch struct {
	filename string
	oldMap   map[string]*intl.Translation
	newMap   map[string]*intl.Translation
}

var mapMatches = []mapMatch{
	{"ar.go", oldloc.Ar, newloc.Ar},
	{"de-de.go", oldloc.DeDE, newloc.DeDE},
	{"pl-pl.go", oldloc.PlPL, newloc.PlPL},
	{"pt-br.go", oldloc.PtBr, newloc.PtBr},
	{"zh-cn.go", oldloc.ZhCN, newloc.ZhCN},
}

func main() {
	newuiLocalesDir, _ := filepath.Abs("../../")

	// Extract & sort keys
	orderedKeys := slices.Collect(maps.Keys(newloc.EnUS))
	slices.Sort(orderedKeys)

	// Determine which ones to transfer
	var transferKeys []string
	for _, key := range orderedKeys {
		newTrans := newloc.EnUS[key]
		oldTrans, okOld := oldloc.EnUS[key]
		if okOld && oldTrans.T == newTrans.T {
			transferKeys = append(transferKeys, key)
		}
	}

	for _, match := range mapMatches {
		fpath := filepath.Join(newuiLocalesDir, match.filename)
		content, err := os.ReadFile(fpath)
		if err != nil {
			fmt.Println("Error reading file:", fpath)
			continue
		}

		idx := bytes.LastIndexByte(content, '}')
		if idx == -1 {
			fmt.Println("No closing brace found in", fpath)
			continue
		}

		var newLines []string
		for _, key := range transferKeys {
			// check if key already exists in actual target map
			if _, exists := match.newMap[key]; exists {
				continue
			}

			if oldVal, ok := match.oldMap[key]; ok {
				// We need to format it properly.
				if oldVal.Version != 0 {
					newLines = append(newLines, fmt.Sprintf("\t%q: {Version: %d, T: %q},", key, oldVal.Version, oldVal.T))
				} else {
					newLines = append(newLines, fmt.Sprintf("\t%q: {T: %q},", key, oldVal.T))
				}
			}
		}

		if len(newLines) > 0 {
			prefix := string(content[:idx])
			prefix = strings.TrimRight(prefix, " \n\r\t")
			if !strings.HasSuffix(prefix, "{") {
				prefix += "\n"
			} else {
				prefix += "\n"
			}

			newContent := prefix + strings.Join(newLines, "\n") + "\n}\n"
			err = os.WriteFile(fpath, []byte(newContent), 0644)
			if err != nil {
				fmt.Println("Error writing file:", fpath, err)
			} else {
				fmt.Printf("Updated %s with %d entries\n", match.filename, len(newLines))

				cmd := exec.Command("gofmt", "-w", fpath)
				if err := cmd.Run(); err != nil {
					fmt.Println("Error running gofmt on:", fpath, err)
				}
			}
		}
	}
}
