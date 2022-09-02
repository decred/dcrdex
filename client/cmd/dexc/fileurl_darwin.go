//go:build systray && darwin

package main

import (
	"net/url"
	"path/filepath"
)

// Darwin appears to need paths pre-escaped.

func filePathToURL(name string) (string, error) {
	path, err := filepath.Abs(name)
	if err != nil { // can't pwd if name was relative, probably impossible
		return "", err
	}
	fileURL, err := url.Parse("file://" + path)
	if err != nil {
		return "", err
	}
	// url.Parse can be touchy, so consider replacing only spaces, manually:
	// path = strings.ReplaceAll(path, " ", "%20")
	return fileURL.String(), nil
}
