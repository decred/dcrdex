//go:build !darwin && !windows

package app

import (
	"path/filepath"
)

func FilePathToURL(name string) (string, error) {
	path, err := filepath.Abs(name)
	if err != nil { // can't pwd if name was relative, probably impossible
		return "", err
	}
	return "file://" + path, nil
}
