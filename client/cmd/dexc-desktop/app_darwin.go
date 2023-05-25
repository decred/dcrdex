//go:build darwin

package main

/*
dexc-desktop is the Desktop version of the DEX Client. There are a number of
differences that make this version more suitable for less tech-savvy users.

| CLI version                       | Desktop version                          |
|-----------------------------------|------------------------------------------|
| Installed by building from source | Installed with an installer program.     |
| or downloading a binary. Or with  | (e.g a .dmg MacOS installer)             |
| dcrinstall from a terminal.       |                                          |
|-----------------------------------|------------------------------------------|
| Started by command-line.          | Started by selecting from the start/main |
|                                   | menu, or by selecting a desktop icon or  |
|                                   | pinned taskbar icon. CLI is fine too.    |
|                                   |                                          |
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

The program will continue to run in the background even if all windows are
closed. This is the default behavior for MacOS apps but new windows can be
created by clicking on the icon in the dock.
*/

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"runtime/pprof"
	"sync"
	"syscall"
	"time"

	"decred.org/dcrdex/client/app"
	"decred.org/dcrdex/client/asset"
	_ "decred.org/dcrdex/client/asset/bch"  // register bch asset
	_ "decred.org/dcrdex/client/asset/btc"  // register btc asset
	_ "decred.org/dcrdex/client/asset/dcr"  // register dcr asset
	_ "decred.org/dcrdex/client/asset/dgb"  // register dgb asset
	_ "decred.org/dcrdex/client/asset/doge" // register doge asset
	_ "decred.org/dcrdex/client/asset/firo" // register firo asset
	_ "decred.org/dcrdex/client/asset/ltc"  // register ltc asset
	_ "decred.org/dcrdex/client/asset/zec"  // register zec asset
	dexCore "decred.org/dcrdex/client/core"
	"decred.org/dcrdex/client/mm"
	"decred.org/dcrdex/client/rpcserver"
	"decred.org/dcrdex/client/webserver"
	"decred.org/dcrdex/dex"
	"github.com/pkg/browser"
	"github.com/progrium/macdriver/cocoa"
	"github.com/progrium/macdriver/core"
	"github.com/progrium/macdriver/objc"
	"github.com/progrium/macdriver/webkit"
	// Ethereum loaded in client/app/importlgpl.go
)

func init() {
	runtime.LockOSThread()
}

var nOpenWindows int

// createNewAppWindow creates a new window with the specified URL. Only one
// window can be open at a time to portray the feel of a native app. We can
// allow multiple windows in the future if needed.
func createNewAppWindow(url string) {
	if nOpenWindows > 0 {
		return
	}
	nOpenWindows++

	// Prepare webview config.
	config := webkit.WKWebViewConfiguration_New()
	config.Preferences().SetValueForKey(core.True, core.String("developerExtrasEnabled"))
	width, height := defaultWindowWidthAndHeight()

	// Create a new webview and load the provided url.
	webView := webkit.WKWebView_Init(core.Rect(0, 0, float64(width), float64(height)), config)
	req := core.NSURLRequest_Init(core.URL(url))
	webView.LoadRequest(req)

	// Create a new window and set the webview as its content view.
	win := cocoa.NSWindow_Init(core.NSMakeRect(0, 0, 1440, 900), cocoa.NSClosableWindowMask|cocoa.NSTitledWindowMask|cocoa.NSResizableWindowMask|cocoa.NSFullSizeContentViewWindowMask|cocoa.NSMiniaturizableWindowMask, cocoa.NSBackingStoreBuffered, false)
	win.SetTitle("Decred DEX Client")
	win.Center()
	win.SetMovable_(true)
	win.MakeKeyAndOrderFront(nil)
	win.SetContentView(webView)
	win.SetMinSize_(core.NSSize{Width: 600, Height: 600})
	win.SetDelegate_(cocoa.DefaultDelegate)
}

// mainCore is the darwin entry point for the DEX Desktop client.
func mainCore() error {
	appCtx, cancel := context.WithCancel(context.Background())
	defer cancel() // don't leak on the earliest returns

	// Parse configuration.
	cfg, err := configure()
	if err != nil {
		return fmt.Errorf("configuration error: %w", err)
	}

	// Filter registered assets.
	asset.SetNetwork(cfg.Net)

	if cfg.Webview != "" { // Ignore, other OS use this for a specific reason (to support multiple windows).
		return nil
	}

	// Initialize logging.
	utc := !cfg.LocalLogs
	logMaker, closeLogger := app.InitLogging(cfg.LogPath, cfg.DebugLevel, cfg.LogStdout, utc)
	defer closeLogger()
	log = logMaker.Logger("APP")
	log.Infof("%s version %s (Go version %s)", appName, app.Version, runtime.Version())
	if utc {
		log.Infof("Logging with UTC time stamps. Current local time is %v",
			time.Now().Local().Format("15:04:05 MST"))
	}

	syncDir := filepath.Join(cfg.AppData, cfg.Net.String())

	// The --kill flag is a backup measure to end a background process (that
	// presumably has active orders).
	if cfg.Kill {
		sendKillSignal(syncDir)
		return nil
	}

	startServer, err := synchronize(syncDir)
	if err != nil || !startServer {
		// If we didn't start the server but there is no error, and it means
		// that we've successfully sent an open window request to an already
		// running instance.
		return err
	}

	if cfg.CPUProfile != "" {
		f, err := os.Create(cfg.CPUProfile)
		if err != nil {
			return fmt.Errorf("error starting CPU profiler: %w", err)
		}
		err = pprof.StartCPUProfile(f)
		if err != nil {
			return fmt.Errorf("error starting CPU profiler: %w", err)
		}
		defer pprof.StopCPUProfile()
	}

	defer func() {
		if pv := recover(); pv != nil {
			log.Criticalf("Uh-oh! \n\nPanic:\n\n%v\n\nStack:\n\n%v\n\n",
				pv, string(debug.Stack()))
		}
	}()

	// Prepare the Core.
	clientCore, err := dexCore.New(cfg.Core(logMaker.Logger("CORE")))
	if err != nil {
		return fmt.Errorf("error creating client core: %w", err)
	}

	// Handle shutdown by user (if running via terminal), or on system shutdown.
	// TODO: SIGTERM is apparently spoofed by Go for Windows. Nice feature, but
	// not well documented. Test to verify. Could also catch SIGKILL, which is
	// sent after a configured timeout if the program doesn't exit on SIGTERM.
	killChan := make(chan os.Signal, 1)
	signal.Notify(killChan, os.Interrupt /* ctrl-c */, syscall.SIGTERM /* system shutdown */, syscall.SIGQUIT /* ctrl-\ */)
	go func() {
		for range killChan {
			log.Infof("Shutting down...")
			cancel()
			return
		}
	}()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		clientCore.Run(appCtx)
		cancel() // in the event that Run returns prematurely prior to context cancellation
	}()

	<-clientCore.Ready()

	defer func() {
		log.Info("Exiting dexc main.")
		cancel()  // no-op with clean rpc/web server setup
		wg.Wait() // no-op with clean setup and shutdown
	}()

	var marketMaker *mm.MarketMaker
	if cfg.Experimental {
		// TODO: on shutdown, stop market making and wait for trades to be
		// canceled.
		marketMaker, err = mm.NewMarketMaker(clientCore, logMaker.Logger("MM"))
		if err != nil {
			return fmt.Errorf("error creating market maker: %w", err)
		}
	}

	if cfg.RPCOn {
		rpcSrv, err := rpcserver.New(cfg.RPC(clientCore, marketMaker, logMaker.Logger("RPC")))
		if err != nil {
			return fmt.Errorf("failed to create rpc server: %w", err)
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			cm := dex.NewConnectionMaster(rpcSrv)
			err := cm.Connect(appCtx)
			if err != nil {
				log.Errorf("Error starting rpc server: %v", err)
				cancel()
				return
			}
			cm.Wait()
		}()
	}

	webSrv, err := webserver.New(cfg.Web(clientCore, logMaker.Logger("WEB"), utc))
	if err != nil {
		return fmt.Errorf("failed creating web server: %w", err)
	}

	webStart := make(chan error)
	wg.Add(1)
	go func() {
		defer wg.Done()
		cm := dex.NewConnectionMaster(webSrv)
		webStart <- cm.Connect(appCtx)
		cm.Wait()
	}()

	if err := <-webStart; err != nil {
		return err
	}

	// No errors running webserver, so we can be certain we won any race between
	// starting instances. Start the sync server now.
	openC := make(chan struct{}) // no buffer. Is ignored if window is currently open
	wg.Add(1)
	go func() {
		defer wg.Done()
		runServer(appCtx, syncDir, openC, killChan, cfg.Net)
	}()

	url := "http://" + webSrv.Addr()
	logDir := filepath.Dir(cfg.LogPath)

	// Set to false so that the app doesn't exit when the last window is closed.
	cocoa.TerminateAfterWindowsClose = false

	addMethodToDelegate("applicationDockMenu:", func(_ objc.Object) objc.Object {
		newWindowItem := cocoa.NSMenuItem_New()
		newWindowItem.SetTitle("New Window")
		newWindowItem.SetAction(objc.Sel("newWindow:"))
		addMethodToDelegate("newWindow:", func(_ objc.Object) {
			createNewAppWindow(url)
		})

		openLogsItem := cocoa.NSMenuItem_New()
		openLogsItem.SetTitle("Open Logs")
		openLogsItem.SetAction(objc.Sel("openLogs:"))
		addMethodToDelegate("openLogs:", func(_ objc.Object) {
			logDirURL, err := app.FilePathToURL(logDir)
			if err != nil {
				log.Errorf("error constructing log directory URL: %v", err)
			} else {
				browser.OpenURL(logDirURL)
			}
		})

		menu := cocoa.NSMenu_New()
		menu.AddItem(newWindowItem)
		menu.AddItem(openLogsItem)
		return menu
	})

	// MacOS will always send the "windowWillClose" event when an application
	// window is closing.
	var noteSent bool
	addMethodToDelegate("windowWillClose:", func(s objc.Object) {
		nOpenWindows--

		err := clientCore.Logout()
		if err == nil {
			return // nothing to do
		}

		if !errors.Is(err, dexCore.ActiveOrdersLogoutErr) {
			log.Errorf("Core logout error: %v", err)
			return
		}

		if nOpenWindows == 0 && !noteSent && cocoa.NSApp().IsRunning() { // last window has been closed but app is still running
			noteSent = true
			sendDesktopNotification("DEX client still running", "DEX client is still resolving active DEX orders")
		}
	})

	// MacOS will always send this event when an application icon on the dock is
	// clicked or a new process is about to start, so we hijack the action and
	// create new windows if all windows have been closed.
	addMethodToDelegate("applicationShouldHandleReopen:hasVisibleWindows:", func(_ objc.Object) bool {
		if nOpenWindows < 1 {
			// dexc-desktop is already running but there are no windows open so
			// we should create a new window.
			createNewAppWindow(url)
		}

		// dexc-desktop is already running and there's a window open so we can
		// go ahead with the default action which is to bring the open window to
		// the front.
		return true
	})

	app := cocoa.NSApp_WithDidLaunch(func(notification objc.Object) {
		createNewAppWindow(url)
	})
	app.SetActivationPolicy(cocoa.NSApplicationActivationPolicyRegular)
	app.ActivateIgnoringOtherApps(false)
	app.SetDelegate(cocoa.DefaultDelegate)

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-appCtx.Done():
				cocoa.NSApp().Terminate()
				return
			case <-killChan:
				cocoa.NSApp().Terminate()
				return
			case <-openC:
				// Execute on main thread.
				core.Dispatch(func() {
					createNewAppWindow(url)
				})
			}
		}
	}()

	app.Run() // blocks until app.Terminate() is called
	log.Infof("Shutting down...")
	cancel()
	wg.Wait()

	return nil
}

// addMethodToDelegate adds a method to the default Cocoa delegate.
func addMethodToDelegate(method string, fn interface{}) {
	cocoa.DefaultDelegateClass.AddMethod(method, fn)
}
