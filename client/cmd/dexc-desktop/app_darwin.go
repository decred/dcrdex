//go:build darwin

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"runtime/pprof"
	"strconv"
	"sync"
	"sync/atomic"
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
	"github.com/progrium/macdriver/cocoa"
	"github.com/progrium/macdriver/core"
	"github.com/progrium/macdriver/objc"
	"github.com/progrium/macdriver/webkit"
	// Ethereum loaded in client/app/importlgpl.go
)

var (
	webviewConfig  = webkit.WKWebViewConfiguration_New()
	width, height  = defaultWindowWidthAndHeight()
	maxOpenWindows = 5          // what would they want to do with more than 5 😂?
	nOpenWindows   atomic.Int32 // number of open windows
)

const (
	// macOSAppTitle is the title of the macOS app. It is used to set the title
	// of the main menu.
	macOSAppTitle = "Decred DEX"
	// selOpenLogs is the selector for the "Open Logs" menu item.
	selOpenLogs = "openLogs:"
	// selNewWindow is the selector for the "New Window" menu item.
	selNewWindow = "newWindow:"
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
	return nOpenWindows.Load() > 0
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

	// shutdownCloser is used to execute functions that could have been executed
	// as a deferred statement.
	shutdownCloser := dex.NewErrorCloser()
	defer shutdownCloser.Done(log) // execute deferred statements if we return early

	// Use a hidden "dexc-desktop-state" file to prevent other processes when
	// dexc-desktop is already running (e.g when non-bundled version of
	// dexc-desktop is executed from cmd and vice versa).
	dexcDesktopStateFile := filepath.Join(cfg.Config.AppData, cfg.Net.String(), ".dexc-desktop-state")
	f, err := lockDexcDesktopStateFile(dexcDesktopStateFile)
	if err != nil {
		return err
	}
	shutdownCloser.Add(func() error {
		f.Close()
		return os.Remove(dexcDesktopStateFile)
	})

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

	// createNewWebView creates a new webview with the specified URL. The actual
	// window will be created when the webview is loaded (i.e the
	// "webView:didFinishNavigation:" method below have been executed).
	createNewWebView := func() {
		if int(nOpenWindows.Load()) >= maxOpenWindows {
			log.Debugf("Ignoring open new window request, max number of (%d) open windows exceeded", maxOpenWindows)
			return
		}

		// Create a new webview and load the provided url.
		req := core.NSURLRequest_Init(core.URL(url))
		webView := webkit.WKWebView_Init(core.Rect(0, 0, float64(width), float64(height)), webviewConfig)
		webView.LoadRequest(req)
		webView.SetNavigationDelegate_(cocoa.DefaultDelegate)
	}

	// WebView will execute this method when the page has loaded. We can then
	// create a new window to avoid a temporary blank window. See:
	// https://developer.apple.com/documentation/webkit/wknavigationdelegate/1455629-webview?language=objc
	// NOTE: This method actually receives three argument but the docs said to
	// expect two (webView and navigation).
	addMethodToDelegate("webView:didFinishNavigation:", func(_ objc.Object /* delegate */, webView objc.Object, _ objc.Object /* navigation */) {
		nOpenWindows.Add(1) // increment the number of open windows

		// Create a new window and set the webview as its content view.
		win := cocoa.NSWindow_Init(core.NSMakeRect(0, 0, float64(width), float64(height)), cocoa.NSClosableWindowMask|cocoa.NSTitledWindowMask|cocoa.NSResizableWindowMask|cocoa.NSFullSizeContentViewWindowMask|cocoa.NSMiniaturizableWindowMask, cocoa.NSBackingStoreBuffered, false)
		win.SetTitle(appTitle)
		win.Center()
		win.SetMovable_(true)
		win.SetContentView(webkit.WKWebView_fromRef(webView))
		win.SetMinSize_(core.NSSize{Width: 600, Height: 600})
		win.MakeKeyAndOrderFront(nil)
		win.SetDelegate_(cocoa.DefaultDelegate)
	})

	// Add custom selectors to the app delegate since there are reused in
	// different menus. App delegates methods should be added before NSApp is
	// initialized.
	addMethodToDelegate(selOpenLogs, func(_ objc.Object) {
		logDirURL, err := app.FilePathToURL(logDir)
		if err != nil {
			log.Errorf("error constructing log directory URL: %v", err)
		} else {
			openURL(logDirURL)
		}
	})

	addMethodToDelegate(selNewWindow, func(_ objc.Object) {
		windows := cocoa.NSApp().OrderedWindows()
		len := windows.Count()
		if len < uint64(maxOpenWindows) {
			createNewWebView()
		} else {
			// Show the last window if maxOpenWindows has been exceeded.
			winObj := windows.ObjectAtIndex(len - 1)
			win := cocoa.NSWindow_fromRef(winObj)
			win.MakeMainWindow()
		}
	})

	// MacOS will always send the "applicationShouldTerminate" event when an
	// application is about to exit so we should use this opportunity to
	// cleanup. See:
	// https://developer.apple.com/documentation/appkit/nsapplicationdelegate/1428642-applicationshouldterminate?language=objc
	addMethodToDelegate("applicationShouldTerminate:", func(s objc.Object) core.NSUInteger {
		cancel()                 // no-op with clean rpc/web server setup
		wg.Wait()                // no-op with clean setup and shutdown
		shutdownCloser.Done(log) // execute defer statements
		shutdownCloser.Success()
		return core.NSUInteger(1)
	})

	// "applicationDockMenu" method returns the app's dock menu. See:
	// https://developer.apple.com/documentation/appkit/nsapplicationdelegate/1428564-applicationdockmenu?language=objc
	addMethodToDelegate("applicationDockMenu:", func(_ objc.Object) objc.Object {
		// Menu Items
		newWindowMenuItem := cocoa.NSMenuItem_Init("New Window", objc.Sel(selNewWindow), "n")
		logsMenuItem := cocoa.NSMenuItem_Init("Open Logs", objc.Sel(selOpenLogs), "l")

		menu := cocoa.NSMenu_New()
		menu.AddItem(newWindowMenuItem)
		menu.AddItem(logsMenuItem)

		return menu
	})

	// MacOS will always send the "windowWillClose" event when an application
	// window is closing. See:
	// https://developer.apple.com/documentation/appkit/nswindowdelegate/1419605-windowwillclose?language=objc
	var noteSent bool
	addMethodToDelegate("windowWillClose:", func(_ objc.Object) {
		windowsOpen := nOpenWindows.Add(-1)
		if windowsOpen > 0 {
			return // nothing to do
		}

		err := clientCore.Logout()
		if err == nil {
			return // nothing to do
		}

		if !errors.Is(err, dexCore.ActiveOrdersLogoutErr) {
			log.Errorf("Core logout error: %v", err)
			return
		}

		if !noteSent && cocoa.NSApp().IsRunning() { // last window has been closed but app is still running
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
			createNewWebView()
		}

		// dexc-desktop is already running and there's a window open so we can
		// go ahead with the default action which is to bring the open window to
		// the front.
		return true
	})

	app := cocoa.NSApp()

	// Set the app's main and status bar menu when we receive the
	// "applicationWillFinishLaunching" notification. See:
	//  - https://github.com/go-gl/glfw/blob/master/v3.3/glfw/glfw/src/cocoa_init.m#L427-L443
	//  - https://developer.apple.com/documentation/appkit/nsapplicationwillfinishlaunchingnotification?language=objc
	addMethodToDelegate("applicationWillFinishLaunching:", func(_ objc.Object) {
		// Create the main menu bar.
		menuBar := cocoa.NSMenu_New()
		app.SetMainMenu(menuBar)
		appMenus := createMainMenuItems()
		for _, menu := range appMenus {
			// Create a menu item for the menuBar and set the menu as the
			// submenu. See:
			// https://developer.apple.com/documentation/appkit/nsmenuitem/1514845-submenu?language=objc
			mainBarItem := cocoa.NSMenuItem_New()
			mainBarItem.SetTitle(menu.Title())
			mainBarItem.SetSubmenu(menu)
			menuBar.AddItem(mainBarItem)

			if menu.Title() == "Window" {
				// Set NSApp's WindowsMenu to the Window menu. This will allow
				// windows to be grouped together in the dock icon and in the
				// Window menu. Also, MacOS will automatically add other default
				// Window menu items. See:
				// https://developer.apple.com/documentation/appkit/nsapplication/1428547-windowsmenu?language=objc
				app.Set("WindowsMenu:", menu)
			}
		}

		// Create the status bar menu. We want users to notice dexc desktop is
		// still running (even with the dot below the dock icon).
		obj := cocoa.NSStatusBar_System().StatusItemWithLength(cocoa.NSVariableStatusItemLength)
		obj.Retain()
		obj.Button().SetImage(cocoa.NSImage_InitWithData(core.NSData_WithBytes(SymbolBWIcon, uint64(len(SymbolBWIcon)))))
		obj.Button().Image().SetSize(core.Size(18, 18))
		obj.Button().SetToolTip("Self-custodial multi-wallet")

		runningItem := cocoa.NSMenuItem_New()
		runningItem.SetTitle("Dex Client is running")
		runningItem.SetEnabled(false)

		itemQuit := cocoa.NSMenuItem_Init("Force Quit", objc.Sel("terminate:"), "q")
		itemQuit.SetToolTip("Force DEX client to close")

		menu := cocoa.NSMenu_New()
		menu.AddItem(runningItem)
		menu.AddItem(itemQuit)
		obj.SetMenu(menu)

		// Hide the application until it is ready to be shown when we receive
		// the "applicationDidFinishLaunching" notification below. This also
		// allows us to ensure the menu bar is redrawn.
		app.TryToPerform_with_(objc.Sel("hide:"), nil)
	})

	// MacOS will always send the "applicationDidFinishLaunching" event when an
	// application has finished launching. See:
	// https://developer.apple.com/documentation/appkit/nsapplicationdidfinishlaunchingnotification?language=objc
	addMethodToDelegate("applicationDidFinishLaunching:", func(_, notification objc.Object) {
		// Unhide the app on the main thread after it has finished launching we
		// need to give this priority before creating the window to ensure the
		// window is immediately visible when it's created. This also has the
		// side effect of redrawing the menu bar which will be unresponsive
		// until it is redrawn.
		core.Dispatch(func() {
			app.TryToPerform_with_(objc.Sel("unhide:"), nil)
		})

		createNewWebView()
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

// createMainMenuItems creates the main menu items for the app menu.
func createMainMenuItems() []cocoa.NSMenu {
	// Create the App menu.
	appMenu := cocoa.NSMenu_Init(macOSAppTitle)

	// Add the menu items.
	hideMenuItem := cocoa.NSMenuItem_Init("Hide "+macOSAppTitle, objc.Sel("hide:"), "h")
	appMenu.AddItem(hideMenuItem)

	hideOthersMenuItem := cocoa.NSMenuItem_Init("Hide Others", objc.Sel("hideOtherApplications:"), "")
	appMenu.AddItem(hideOthersMenuItem)

	showAllMenuItem := cocoa.NSMenuItem_Init("Show All", objc.Sel("unhideAllApplications:"), "")
	appMenu.AddItem(showAllMenuItem)

	appMenu.AddItem(cocoa.NSMenuItem_Separator())

	quitMenuItem := cocoa.NSMenuItem_Init("Quit Decred DEX", objc.Sel("terminate:"), "q")
	quitMenuItem.SetToolTip("Force DEX client to close")
	appMenu.AddItem(quitMenuItem)

	// Create the Window menu.
	windowMenu := cocoa.NSMenu_Init("Window")

	// Add the menu items.
	newWindowItem := cocoa.NSMenuItem_Init("New Window", objc.Sel(selNewWindow), "n")
	windowMenu.AddItem(newWindowItem)

	windowMenu.AddItem(cocoa.NSMenuItem_Separator())

	minimizeMenuItem := cocoa.NSMenuItem_Init("Minimize", objc.Sel("performMiniaturize:"), "m")
	windowMenu.AddItem(minimizeMenuItem)

	zoomMenuItem := cocoa.NSMenuItem_Init("Zoom", objc.Sel("performZoom:"), "z")
	windowMenu.AddItem(zoomMenuItem)

	frontMenuItem := cocoa.NSMenuItem_Init("Bring All to Front", objc.Sel("arrangeInFront:"), "")
	windowMenu.AddItem(frontMenuItem)

	windowMenu.AddItem(cocoa.NSMenuItem_Separator())

	fullScreenMenuItem := cocoa.NSMenuItem_Init("Enter Full Screen", objc.Sel("toggleFullScreen:"), "f")
	windowMenu.AddItem(fullScreenMenuItem)

	// Create the "Others" menu.
	othersMenu := cocoa.NSMenu_Init("Others")

	logsMenuItem := cocoa.NSMenuItem_Init("Open Logs", objc.Sel(selOpenLogs), "l")
	othersMenu.AddItem(logsMenuItem)

	return []cocoa.NSMenu{appMenu, windowMenu, othersMenu}
}

// addMethodToDelegate adds a method to the default Cocoa delegate.
func addMethodToDelegate(method string, fn interface{}) {
	cocoa.DefaultDelegateClass.AddMethod(method, fn)
}

// openURL opens the file at the specified path using macOS's native APIs.
func openURL(path string) {
	// See: https://developer.apple.com/documentation/appkit/nsworkspace?language=objc
	objc.Get("NSWorkspace").Get("sharedWorkspace").Send("openURL:", core.NSURL_Init(path))
}

// lockDexcDesktopStateFile creates a lock file and returns a Closer that can be
// used to unlock it. If the file already exists, the process id in the file is
// checked to see if the process is still running.
func lockDexcDesktopStateFile(path string) (io.Closer, error) {
	pidB, err := os.ReadFile(path)
	if err == nil {
		// Check if the pid is a number.
		if pid, err := strconv.Atoi(string(pidB)); err == nil && pid != 0 {
			// Check if the process is still running.
			if p, err := os.FindProcess(pid); err == nil && p.Signal(syscall.Signal(0)) == nil {
				return nil, errors.New("dexc-desktop is already running")
			}
		}

		if err := os.Remove(path); err != nil {
			return nil, fmt.Errorf("lockDexcDesktopStateFile: failed to remove lock file %s %v", path, err)
		}
	}

	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC|os.O_EXCL, 0666)
	if err != nil {
		return nil, fmt.Errorf("lockDexcDesktopStateFile: failed to create lock file %s %v", path, err)
	}

	if _, err := f.WriteString(fmt.Sprintf("%d", os.Getpid())); err != nil {
		return nil, fmt.Errorf("lockDexcDesktopStateFile: cannot write owner pid: %v", err)
	}

	return f, nil
}
