// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

/*
dexc-desktop is the Desktop version of the DEX Client. There are a number of
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
 1) If there are no active orders when the user closes the window, dexc will
    exit immediately.
 2) If we receive a SIGTERM signal, expected for system shutdown, shut down
    immediately. Ctrl-c still works if running via CLI, with no prompt.
 3) If the window remains closed, but the active orders all resolve, shut down.
    We check every minute while the window is closed.
 4) The user can kill the background program with a command-line argument,
    --kill, which uses the sync server in the background to issue the command.
*/

package main

// Full screen cgo solution. Seems to work on Debian.
// TODO: Check multi-screen.
// https://github.com/webview/webview/issues/458#issuecomment-738034846

/*
#cgo darwin LDFLAGS: -framework CoreGraphics
#cgo linux pkg-config: x11
#if defined(__APPLE__)
#include <CoreGraphics/CGDisplayConfiguration.h>
int display_width() {
	return CGDisplayPixelsWide(CGMainDisplayID());
}
int display_height() {
	return CGDisplayPixelsHigh(CGMainDisplayID());
}
#elif defined(_WIN32)
#include <wtypes.h>
int display_width() {
	RECT desktop;
	const HWND hDesktop = GetDesktopWindow();
	GetWindowRect(hDesktop, &desktop);
	return desktop.right;
}
int display_height() {
	RECT desktop;
	const HWND hDesktop = GetDesktopWindow();
	GetWindowRect(hDesktop, &desktop);
	return desktop.bottom;
}
#else
#include <X11/Xlib.h>
int display_width() {
	Display* d = XOpenDisplay(NULL);
	Screen*  s = DefaultScreenOfDisplay(d);
	return s->width;
}
int display_height() {
	Display* d = XOpenDisplay(NULL);
	Screen*  s = DefaultScreenOfDisplay(d);
	return s->height;
}
#endif
*/
import "C"
import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"

	_ "decred.org/dcrdex/client/asset/bch"  // register bch asset
	_ "decred.org/dcrdex/client/asset/btc"  // register btc asset
	_ "decred.org/dcrdex/client/asset/dcr"  // register dcr asset
	_ "decred.org/dcrdex/client/asset/dgb"  // register dgb asset
	_ "decred.org/dcrdex/client/asset/doge" // register doge asset
	_ "decred.org/dcrdex/client/asset/firo" // register firo asset
	_ "decred.org/dcrdex/client/asset/ltc"  // register ltc asset
	_ "decred.org/dcrdex/client/asset/zec"  // register zec asset

	// Ethereum loaded in client/app/importlgpl.go

	"decred.org/dcrdex/dex"
	"github.com/gen2brain/beeep"
)

const (
	appName  = "dexc-desktop"
	appTitle = "Decred DEX Client"
)

var (
	log     dex.Logger
	exePath = findExePath()
	srcDir  = filepath.Join(filepath.Dir(exePath), "src")

	//go:embed src/dexc.png
	FavIcon []byte

	//go:embed src/symbol-bw-round.png
	SymbolBWIcon []byte
)

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

func defaultWindowWidthAndHeight() (int, int) {
	width, height := int(C.display_width()), int(C.display_height())
	if width <= 0 || width > 1920 {
		width = 1920
	}
	if height <= 0 || height > 1080 {
		height = 1080
	}
	return width, height
}

func sendDesktopNotification(title, msg string) {
	err := beeep.Notify(title, msg, filepath.Join(srcDir, "dexc.png"))
	if err != nil {
		log.Errorf("error sending desktop notification: %v", err)
		return
	}
}
