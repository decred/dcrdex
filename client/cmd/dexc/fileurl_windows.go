//go:build systray && windows

package main

import (
	"path/filepath"
)

// Windows requires a leading "/" before the "C:" of an absolute path, and
// slashes converted to forward slashes.
// https://en.wikipedia.org/wiki/File_URI_scheme#Windows

func filePathToURL(name string) (string, error) {
	path, err := filepath.Abs(name)
	if err != nil { // can't pwd if name was relative, probably impossible
		return "", err
	}
	path = filepath.ToSlash(path)
	return "file:///" + path, nil
}
