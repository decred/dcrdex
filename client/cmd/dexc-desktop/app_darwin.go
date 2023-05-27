//go:build darwin

package main

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
	"strings"
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

var (
	webviewConfig  = webkit.WKWebViewConfiguration_New()
	width, height  = defaultWindowWidthAndHeight()
	maxOpenWindows = 5 // what would they want to do with more than 5 😂?
	nOpenWindows   int
)

func init() {
	runtime.LockOSThread()

	// Set webview preferences.
	webviewConfig.Preferences().SetValueForKey(core.True, core.String("developerExtrasEnabled"))
	webviewConfig.Preferences().SetValueForKey(core.True, core.String("javaScriptCanAccessClipboard"))
	webviewConfig.Preferences().SetValueForKey(core.True, core.String("DOMPasteAllowed"))
}

// hasOpenWindows is a convenience function to tell if there are any windows
// currently open.
func hasOpenWindows() bool {
	return nOpenWindows > 0
}

// createNewAppWindow creates a new window with the specified URL.
func createNewAppWindow(url string) {
	if nOpenWindows > maxOpenWindows-1 {
		log.Debugf("Ignoring open new window request, max number of (%d) open windows exceeded", maxOpenWindows)
		return
	}
	nOpenWindows++

	// Create a new webview and load the provided url.
	webView := webkit.WKWebView_Init(core.Rect(0, 0, float64(width), float64(height)), webviewConfig)
	req := core.NSURLRequest_Init(core.URL(url))
	webView.LoadRequest(req)

	// Create a new window and set the webview as its content view.
	win := cocoa.NSWindow_Init(core.NSMakeRect(0, 0, 1440, 900), cocoa.NSClosableWindowMask|cocoa.NSTitledWindowMask|cocoa.NSResizableWindowMask|cocoa.NSFullSizeContentViewWindowMask|cocoa.NSMiniaturizableWindowMask, cocoa.NSBackingStoreBuffered, false)
	win.SetTitle(fmt.Sprintf("Decred DEX Client %d", nOpenWindows))
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

	// Return early if unsupported flags are provided.
	if cfg.Webview != "" {
		return errors.New("--webview flag is not supported. Other OS use it for a specific reason (to support multiple windows)")
	}

	// The --kill flag is a backup measure to end a background process (that
	// presumably has active orders).
	if cfg.Kill {
		// This is a desktop app and the interactive quit or force quit
		// button can be used to shutdown the app.
		return errors.New(`--kill flag is not supported. Use the "Quit" or "Force Quit" button to kill any running process.`)
	}

	// Filter registered assets.
	asset.SetNetwork(cfg.Net)

	// Use a "dexc-desktop-state" file to prevent other processes when
	// dexc-desktop is already running (e.g when non-bundled version of
	// dexc-desktop is executed from cmd and vice versa).
	dexcDesktopStateFile := filepath.Join(cfg.Config.AppData, cfg.Net.String(), "dexc-desktop-state")
	if dex.FileExists(dexcDesktopStateFile) {
		return fmt.Errorf("dexc-desktop is already running, if not, manually delete this file: %s", strings.ReplaceAll(dexcDesktopStateFile, " ", `\ `))
	}

	err = os.WriteFile(dexcDesktopStateFile, []byte("running"), 0600)
	if err != nil {
		return fmt.Errorf("os.WriteFile error: %w", err)
	}

	// shutdownCloser is used to execute functions that could have been executed
	// as a deferred statement.
	shutdownCloser := dex.NewErrorCloser()
	defer shutdownCloser.Done(log) // execute deferred statements if we return early

	// Initialize logging.
	utc := !cfg.LocalLogs
	logMaker, closeLogger := app.InitLogging(cfg.LogPath, cfg.DebugLevel, cfg.LogStdout, utc)
	shutdownCloser.Add(func() error {
		log.Debug("Exiting dexc main.")
		closeLogger()
		return nil
	})

	log = logMaker.Logger("APP")
	log.Infof("%s version %s (Go version %s)", appName, app.Version, runtime.Version())
	if utc {
		log.Infof("Logging with UTC time stamps. Current local time is %v",
			time.Now().Local().Format("15:04:05 MST"))
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
		shutdownCloser.Add(func() error {
			pprof.StopCPUProfile()
			return nil
		})
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

	url := "http://" + webSrv.Addr()
	logDir := filepath.Dir(cfg.LogPath)

	// Set to false so that the app doesn't exit when the last window is closed.
	cocoa.TerminateAfterWindowsClose = false

	// MacOS will always send the "applicationShouldTerminate" event when an
	// application is about to exit so we should use this opportunity to
	// cleanup. See:
	// https://developer.apple.com/documentation/appkit/nsapplicationdelegate/1428642-applicationshouldterminate?language=objc
	addMethodToDelegate("applicationShouldTerminate:", func(s objc.Object) core.NSUInteger {
		cancel()                 // no-op with clean rpc/web server setup
		wg.Wait()                // no-op with clean setup and shutdown
		shutdownCloser.Done(log) // execute defer statements
		shutdownCloser.Success()

		if dex.FileExists(dexcDesktopStateFile) {
			err := os.Remove(dexcDesktopStateFile)
			if err != nil {
				// Just log the error, we shouldn't block shutdown.
				log.Error("failed to remove dexc-desktop state file: %w", err)
			}
		}

		return core.NSUInteger(1)
	})

	// "applicationDockMenu" method returns the app's dock menu. See:
	// https://developer.apple.com/documentation/appkit/nsapplicationdelegate/1428564-applicationdockmenu?language=objc
	addMethodToDelegate("applicationDockMenu:", func(_ objc.Object) objc.Object {
		menu := cocoa.NSMenu_New()
		windows := cocoa.NSApp().OrderedWindows()
		winLen := windows.Count()
		var winIndex uint64
		for ; winIndex < winLen; winIndex++ {
			winObj := windows.ObjectAtIndex(winIndex)
			window := cocoa.NSWindow_fromRef(winObj)
			item := cocoa.NSMenuItem_New()
			item.SetTitle(window.Title())
			// Set target to the window so the "orderFront:" selector is
			// executed by it.
			item.SetTarget(window)
			item.SetAction(objc.Sel("orderFront:"))
			menu.AddItem(item)
		}

		if winLen > 0 {
			menu.AddItem(cocoa.NSMenuItem_Separator())
		}

		newWindowItem := cocoa.NSMenuItem_New()
		newWindowItem.SetTitle("New Window")
		newWindowItem.SetAction(objc.Sel("newWindow:"))
		addMethodToDelegate("newWindow:", func(_ objc.Object) {
			windows := cocoa.NSApp().OrderedWindows()
			len := windows.Count()
			if len < uint64(maxOpenWindows) {
				createNewAppWindow(url)
			} else {
				// Show the last window if maxOpenWindows has been exceeded.
				winObj := windows.ObjectAtIndex(len - 1)
				win := cocoa.NSWindow_fromRef(winObj)
				win.OrderFront(nil)
			}
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

		menu.AddItem(newWindowItem)
		menu.AddItem(openLogsItem)
		return menu
	})

	// MacOS will always send the "windowWillClose" event when an application
	// window is closing. See:
	// https://developer.apple.com/documentation/appkit/nswindowdelegate/1419605-windowwillclose?language=objc
	var noteSent bool
	addMethodToDelegate("windowWillClose:", func(_ objc.Object) {
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
	// create new windows if all windows have been closed. See:
	// https://developer.apple.com/documentation/appkit/nsapplicationdelegate/1428638-applicationshouldhandlereopen?language=objc
	addMethodToDelegate("applicationShouldHandleReopen:hasVisibleWindows:", func(_ objc.Object) bool {
		if !hasOpenWindows() {
			// dexc-desktop is already running but there are no windows open so
			// we should create a new window.
			createNewAppWindow(url)
		}

		// dexc-desktop is already running and there's a window open so we can
		// go ahead with the default action which is to bring the open window to
		// the front.
		return true
	})

	app := cocoa.NSApp_WithDidLaunch(func(_ objc.Object) {
		// We want users to notice dexc desktop is still running (even with the
		// dot below the dock icon).
		obj := cocoa.NSStatusBar_System().StatusItemWithLength(cocoa.NSVariableStatusItemLength)
		obj.Retain()
		obj.Button().SetImage(cocoa.NSImage_InitWithData(core.NSData_WithBytes(SymbolBWIcon, uint64(len(SymbolBWIcon)))))
		obj.Button().Image().SetSize(core.Size(18, 18))
		obj.Button().SetToolTip("Self-custodial multi-wallet")

		runningItem := cocoa.NSMenuItem_New()
		runningItem.SetTitle("Dex Client is running")
		runningItem.SetEnabled(false)

		itemQuit := cocoa.NSMenuItem_New()
		itemQuit.SetTitle("Force Quit")
		itemQuit.SetToolTip("Force DEX client to close")
		itemQuit.SetAction(objc.Sel("terminate:"))

		menu := cocoa.NSMenu_New()
		menu.AddItem(runningItem)
		menu.AddItem(itemQuit)
		obj.SetMenu(menu)

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
			}
		}
	}()

	app.Run() // blocks until app.Terminate() is executed and wil not continue execution of any defer statements in the main thread.
	return nil
}

// addMethodToDelegate adds a method to the default Cocoa delegate.
func addMethodToDelegate(method string, fn interface{}) {
	cocoa.DefaultDelegateClass.AddMethod(method, fn)
}
