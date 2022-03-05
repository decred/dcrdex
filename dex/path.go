// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dex

import (
	"os"
	"path/filepath"
	"strings"
)

// CleanAndExpandPath expands environment variables and leading ~ in the passed
// path, cleans the result, and returns it.
func CleanAndExpandPath(path string) string {
	// Nothing to do when no path is given.
	if path == "" {
		return path
	}

	dirName := ""

	// This supports Windows cmd.exe-style %VARIABLE%.
	if strings.HasPrefix(path, "%") {
		// Split path into %VARIABLE% and path
		pathArray := strings.SplitAfterN(path, "/", 2)
		dirEnv := strings.ToUpper(strings.Trim(pathArray[0], "%/"))
		path = pathArray[1]
		dirName = os.Getenv(dirEnv)
		if dirName == "" {
			// This does not support Windows XP and before as
			// they didn't have a LOCALAPPDATA.
			dirName = os.Getenv("LOCALAPPDATA")
		}
		return filepath.Join(dirName, path)
	}

	if !strings.HasPrefix(path, "~") {
		// NOTE: The os.ExpandEnv doesn't work with Windows cmd.exe-style
		// %VARIABLE%, but the variables can still be expanded via POSIX-style
		// $VARIABLE.
		path = os.ExpandEnv(path)
		return filepath.Clean(path)
	}

	dirName, err := os.UserHomeDir()
	if err != nil {
		// Fallback to CWD if retrieving user home directory fails.
		dirName = "."
	}

	return filepath.Join(dirName, path[1:])
}
