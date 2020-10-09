// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/dex"
	"github.com/gen2brain/dlgs"
	"github.com/getlantern/systray"
)

const (
	minChromiumMajorVersion = 76
)

var (
	// versionRegexp matches the output from --version on a chromium- based
	// browser. We could actually add one more digit group, but we wouldn't
	// really use it.
	versionRegexp = regexp.MustCompile(`^[^\d]*(\d+)\.(\d+)\.(\d+)`) // \.(\d+)
)

// runUI opens the app in a Chromium-based window in app mode. The Chromium
// window is run in an isolated environment with extensions disabled. The user
// must have Chromium, Chrome, Brave, or MS Edge in PATH or installed in a
// default location.
//
// TODO: Add --chromium-path Config option?
func runUI(cfg *Config, processChan <-chan struct{}, core *core.Core) {
	window, err := newWindow(cfg)
	if err != nil {
		log.Errorf("error finding chromium installation: %v", err)
		closeApp()
		return
	}
	winRunner := dex.NewStartStopWaiter(window)

	// Ensure shutdown on an context cancellation.
	go func() {
		<-appCtx.Done()
		systray.Quit()
		if winRunner.On() {
			winRunner.Stop()
		}
	}()

	// initSystray is one of two callbacks required for systray.Run. Set up the
	// system tray menu and start a goroutine to listen for clicks.
	initSystray := func() {
		systray.SetTemplateIcon(favicon, favicon)
		systray.SetTitle("Decred DEX")
		systray.SetTooltip("Decred DEX")

		// Buttons
		miQuit := systray.AddMenuItem("Quit", "Quit")
		miOpen := systray.AddMenuItem("Open", "Open")

		openWindow := func() {
			miOpen.Hide()
			winRunner.Start(appCtx)
			go func() {
				winRunner.WaitForShutdown()
				if appCtx.Err() != nil {
					return
				}
				miOpen.Show()
			}()
		}

		go func() {
		out:
			for {
				select {
				case <-miQuit.ClickedCh:
					if core.IsActivelyTrading() {
						yes, err := dlgs.Question("Question", "Critical trading activity is occuring. You should not shut down DEX with active orders or matches. Are you sure you want to quit?", true)
						if err != nil {
							log.Errorf("dlgs.Question error: %v", err)
						}
						if yes {
							break out
						}
					} else {
						break out
					}
				case <-miOpen.ClickedCh:
					if winRunner.On() {
						log.Errorf("Open clicked for already opened window")
						continue
					}
					openWindow()
				case <-processChan:
					if winRunner.On() {
						winRunner.Stop()
						winRunner.WaitForShutdown()
					}
					openWindow()
				case <-appCtx.Done():
					break out
				}
			}
			closeApp()
		}()

		openWindow()
	}

	systray.Run(initSystray, func() {
		log.Debugf("system tray app exiting")
	})
}

// newWindow creates a new Chromium-based Window. The user must have a Chromium-
// based browser installed and either in PATH (linux), or in a default location
// (Windows, MacOS).
func newWindow(cfg *Config) (Window, error) {
	switch runtime.GOOS {
	case "linux":
		return newWindowLinux(cfg)
	case "windows":
		return newWindowWindows(cfg)
	}
	return nil, fmt.Errorf("Unsupported OS %q, %t", runtime.GOOS, runtime.GOOS == "windows")
}

// linuxCmds is the executable name for the chromium-based browsers supported
// on Linux.
var linuxCmds = [4]string{"chromium-browser", "brave-browser", "google-chrome"}

// newWindowLinux attempts to find a suitable Chromium-based browser. The
// browser executable is assumed to be in PATH, and one of linuxCmds.
func newWindowLinux(cfg *Config) (Window, error) {
	for _, cmd := range linuxCmds {
		major, _, _, found := getBrowserVersion(cmd)
		if found && major >= minChromiumMajorVersion {
			return NewChromiumWindow(cmd, cfg), nil
		}
	}
	// Edge is chromium-based now, so if we can figure out how to check the
	// version and start it with the requisite flags, that should definitely be
	// added here.
	return nil, fmt.Errorf("No browser found. Install Chromium, Brave, or Chrome to run the Decred DEX Client GUI")
}

// newWindowLinux attempts to find a suitable Chromium-based browser, by looking
// at the expected default locations of various chromium-based browsers.
func newWindowWindows(cfg *Config) (Window, error) {
	roots := []string{
		os.Getenv("CommonProgramFiles(x86)"),
		os.Getenv("LOCALAPPDATA"),
	}

	subPaths := []string{
		filepath.Join("Chromium", "Application", "chrome.exe"), // chromium
		filepath.Join("BraveSoftware", "Brave-Browser", "Application", "brave.exe"),
		filepath.Join("Google", "Chrome", "Application", "chrome.exe"),
		filepath.Join("Microsoft", "Edge", "Application", "msedge.exe"),
	}

	search := func(root string) (string, bool) {
		for _, subPath := range subPaths {
			exe := filepath.Join(root, subPath)
			if !fileExists(exe) {
				continue
			}

			// For Windows, the --version flag is apparently innefective.
			// https://bugs.chromium.org/p/chromium/issues/detail?id=158372
			// But the version is the name of a sub-directory.
			items, _ := ioutil.ReadDir(filepath.Dir(exe))
			for _, item := range items {
				if !item.IsDir() {
					continue
				}
				major, _, _, found := parseVersion(item.Name())
				if found && major > minChromiumMajorVersion {
					return exe, true
				}
			}
		}
		return "", false
	}

	for _, root := range roots {
		exe, found := search(root)
		if found {
			return NewChromiumWindow(exe, cfg), nil
		}
	}

	// If the browser is not found, we could download it from e.g.
	// https://www.googleapis.com/download/storage/v1/b/chromium-browser-snapshots/o/Win_x64%2F818425%2Fchrome-win.zip?generation=1603109374298105&alt=media
	// and unzip it to the .dexc directory.

	return nil, fmt.Errorf("No browser found. Install Chromium, Brave, or Chrome to run the Decred DEX Client GUI")
}

// Get browser version attempts to get the currently installed version for the
// specified browser executable. The boolean return value, found, indicates if
// a browser with an acceptable version is located.
func getBrowserVersion(cmd string) (major, minor, patch int, found bool) {
	cmdOut, err := exec.CommandContext(appCtx, cmd, "--version").Output()
	if err == nil {
		log.Tracef("%s has version %s\n", cmd, string(cmdOut))
		return parseVersion(string(cmdOut))
	}
	return
}

func parseVersion(ver string) (major, minor, patch int, found bool) {
	matches := versionRegexp.FindStringSubmatch(ver)
	if len(matches) == 4 {
		// The regex grouped on \d+, so an error is impossible(?).
		major, _ = strconv.Atoi(matches[1])
		minor, _ = strconv.Atoi(matches[2])
		patch, _ = strconv.Atoi(matches[3])
		found = true
	}
	return
}

// Window is a UI window capable of connecting to the DEX client web server.
type Window dex.Runner

// ChromiumWindow is a Chromium-browser based Window. The browser is run in
// app mode, in an isolated environment, with extensions disabled.
type ChromiumWindow struct {
	cfg *Config
	exe string

	cmdMtx sync.RWMutex
	cmd    *exec.Cmd
}

// NewChromiumWindow is the constructor for a ChromiumWindow.
func NewChromiumWindow(exe string, cfg *Config) *ChromiumWindow {
	return &ChromiumWindow{
		cfg: cfg,
		exe: exe,
	}
}

// Run opens a window by running the chromium-based browser with exec. Run
// blocks until the window is closed. The window can be forced to close by
// cancelling the supplied Context.
func (mgr *ChromiumWindow) Run(ctx context.Context) {
	tokens := strings.Split(mgr.exe, string(os.PathSeparator))
	variant := strings.TrimSuffix(tokens[len(tokens)-1], ".exe")
	dataDir := filepath.Join(cfg.AppData, variant)
	err := os.MkdirAll(dataDir, 0700)
	if err != nil {
		log.Errorf("MkDirAll error for directory %s", dataDir)
		return
	}
	dataDirFlag := "--user-data-dir=" + dataDir
	appFlag := "--app=http://" + cfg.WebAddr

	mgr.cmdMtx.Lock()

	mgr.cmd = exec.CommandContext(ctx, mgr.exe, appFlag,
		"--disable-extensions", "--no-first-run", dataDirFlag)

	mgr.cmdMtx.Unlock()

	mgr.cmd.Run()
}

// filesExists reports whether the named file or directory exists.
func fileExists(name string) bool {
	_, err := os.Stat(name)
	return !os.IsNotExist(err)
}
