// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build systray

package main

import (
	"fmt"
	"os"
	"path/filepath"

	"decred.org/dcrdex/client/app"
	"fyne.io/systray"
	"github.com/pkg/browser"
)

var (
	mainDone = make(chan struct{})
	cfg      *app.Config
)

func onReady() {
	go func() {
		defer close(mainDone)
		if err := runCore(cfg); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
	}()

	go func() {
		<-appCtx.Done()
		systray.SetTooltip("Shutting down. Please wait...")
		<-mainDone
		systray.Quit()
	}()

	systray.SetIcon(FavIcon)
	systray.SetTitle("DCRDEX")
	systray.SetTooltip("The Decred DEX")

	mStarting := systray.AddMenuItem("Starting...", "Starting up. Please wait...")
	var addr string
	var ok bool
	select {
	case addr, ok = <-webserverReady:
		if !ok { // no webserver started
			fmt.Fprintln(os.Stderr, "Web server required!")
			cancel()
			return
		}
	case <-mainDone:
		return
	}

	mStarting.Hide()

	mOpen := systray.AddMenuItem("Launch browser", "Open the interface in a browser window.")
	mOpen.SetIcon(SymbolBWIcon)
	go func() {
		for range mOpen.ClickedCh {
			err := browser.OpenURL("http://" + addr)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
			}
		}
	}()

	systray.AddSeparator()

	if logDirURL, err := app.FilePathToURL(filepath.Dir(cfg.LogPath)); err != nil {
		fmt.Fprintln(os.Stderr, err)
	} else {
		mLogs := systray.AddMenuItem("Open logs folder", "Open the folder with your DEX logs.")
		go func() {
			for range mLogs.ClickedCh {
				err := browser.OpenURL(logDirURL)
				if err != nil {
					fmt.Fprintln(os.Stderr, err)
				}
			}
		}()
	}

	if cfgPathURL, err := app.FilePathToURL(cfg.ConfigPath); err != nil {
		fmt.Fprintln(os.Stderr, err)
	} else {
		mConfigFile := systray.AddMenuItem("Edit config file", "Open the config file in a text editor.")
		go func() {
			for range mConfigFile.ClickedCh {
				if _, err := os.Stat(cfg.ConfigPath); err != nil {
					if os.IsNotExist(err) {
						fid, err := os.Create(cfg.ConfigPath)
						if err != nil {
							fmt.Fprintf(os.Stderr, "failed to create new config file: %v", err)
							continue
						}
						fid.Close()
					}
				}
				err := browser.OpenURL(cfgPathURL)
				if err != nil {
					fmt.Fprintln(os.Stderr, err)
				}
			}
		}()
	}

	systray.AddSeparator()

	mQuit := systray.AddMenuItem("Quit", "Quit the DEX.")
	go func() {
		<-mQuit.ClickedCh
		mOpen.Disable()
		mQuit.Disable()
		cancel()
	}()

	err := browser.OpenURL("http://" + addr)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
}

func onExit() {
	// In case we got here before shutting down, do it now.
	cancel()
	<-mainDone
}

func main() {
	// Parse configuration.
	var err error
	cfg, err = configure()
	if err != nil {
		fmt.Fprintf(os.Stderr, "configuration error: %v", err)
		os.Exit(1)
	}
	systray.Run(onReady, onExit)
}
