// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

/*
bisonw-desktop is the Desktop version of Bison Wallet. There are a number of
differences that make this version more suitable for less tech-savvy users.

| CLI version                       | Desktop version                          |
|-----------------------------------|------------------------------------------|
| Installed by building from source | Installed with an installer program.     |
| or downloading a binary. Or with  | Debian archive for Debian Linux,         |
| dcrinstall from a terminal.       | .exe (e.g. Inno Setup) for Windows.      |
|-----------------------------------|------------------------------------------|
| Started by command-line.          | Started by selecting from the start/main |
|                                   | menu, or by selecting a desktop icon or  |
|                                   | pinned taskbar icon. CLI is fine too.    |
|                                   | Program is installed in PATH.            |
|-----------------------------------|------------------------------------------|
| Accessed by going to localhost    | Opens in WebView, a simple window        |
| address in the browser.           | backed by a web engine.                  |
|-----------------------------------|------------------------------------------|
| Shutdown via ctrl-c signal.       | When user closes window, continues       |
| Prompt user to force shutdown if  | running in the background if there are   |
| there are active orders.          | active orders. Run a little server that  |
|                                   | synchronizes at start-up, enabling the   |
|                                   | window to be reopened when the user      |
|                                   | tries to start another instance. A       |
|                                   | desktop notification is sent and the     |
|                                   | system tray icon remains in the tray.    |
|-----------------------------------|------------------------------------------|

Both versions use the same default client configuration file locations at
AppDataDir("dexc").

Since the program continues running in the background if there are active
orders, there becomes a question of how and when to shutdown, or what happens
when the user simply shuts off their computer.
 1) If there are no active orders when the user closes the window, bisonw will
    exit immediately.
 2) If we receive a SIGTERM signal, expected for system shutdown, shut down
    immediately. Ctrl-c still works if running via CLI, with no prompt.
 3) If the window remains closed, but the active orders all resolve, shut down.
    We check every minute while the window is closed.
 4) The user can kill the background program with a command-line argument,
    --kill, which uses the sync server in the background to issue the command.
*/

package main

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	_ "decred.org/dcrdex/client/asset/importall"
	"decred.org/dcrdex/dex"
)

const (
	appName  = "bisonw-desktop"
	appTitle = "Bison Wallet"
)

var (
	log     dex.Logger
	exePath = findExePath()

	// tmpLogoPath is set to a temp file path on startup for the logo on desktop
	// notifications. Do not use sendDesktopNotification until it is set.
	tmpLogoPath string
)

//go:embed src/bisonw-dark-256.png
var symbolSolidPNG []byte

func storeTmpLogo() (tempDir string) {
	// For desktop notifications, Windows seems to be fine with a PNG file,
	// unlike the window and system tray icons.
	var err error
	if tempDir, err = os.MkdirTemp("", "dexc"); err != nil {
		fmt.Printf("Failed to make temp folder for image resources: %v\n", err)
	} else if tempDir != "/" {
		srcImg := Icon
		if runtime.GOOS == "windows" {
			srcImg = symbolSolidPNG // png ok, but Windows can be quirky with transparency
		}
		tmpLogoPath = filepath.Join(tempDir, "bisonw.png")
		_ = os.WriteFile(tmpLogoPath, srcImg, 0644)
		// sendDesktopNotification will work now.
	}
	return
}

func main() {
	// Wrap the actual main so defers run in it.
	err := mainCore()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

func findExePath() string {
	rawPath, err := os.Executable()
	if err != nil {
		panic("error finding executable: " + err.Error())
	}
	s, err := filepath.EvalSymlinks(rawPath)
	if err != nil {
		panic("error resolving symlinks:" + err.Error())
	}
	return s
}

func limitedWindowWidthAndHeight(width int, height int) (int, int) {
	if width <= 0 || width > 1920 {
		width = 1920
	}
	if height <= 0 || height > 1080 {
		height = 1080
	}
	return width, height
}
