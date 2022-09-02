//go:build systray && !darwin && !windows

package main

import (
	"path/filepath"
)

func filePathToURL(name string) (string, error) {
	path, err := filepath.Abs(name)
	if err != nil { // can't pwd if name was relative, probably impossible
		return "", err
	}
	return "file://" + path, nil
}
