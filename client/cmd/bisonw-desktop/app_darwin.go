//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -lobjc -framework WebKit

#import <objc/runtime.h>
#import <WebKit/WebKit.h>

// CompletionHandlerDelegate implements methods required for executing
// completion and decision handlers.
@interface CompletionHandlerDelegate:NSObject
- (WKWebView *)newWebView;
@end

@implementation CompletionHandlerDelegate
// Implements "newWebview". This is because the inspectable property is not available via
// darwinkit at the time of writing. Only MacOs 13.3+ has this property.
// This means that the webview is not inspectable on older versions of macOS.
// See: https://stackoverflow.com/a/75984167
- (WKWebView *)newWebview {
	WKWebViewConfiguration *webConfiguration = [WKWebViewConfiguration new];
	WKWebView *webView = [[WKWebView alloc] initWithFrame:CGRectZero configuration:webConfiguration];
	if (@available(macOS 13.3, *)) {
		webView.inspectable = YES;
	}
	return webView;
}
@end

void* createCompletionHandlerDelegate() {
   return [[CompletionHandlerDelegate alloc] init];
}
*/
import "C"
import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"runtime/pprof"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	bwapp "decred.org/dcrdex/client/app"
	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/client/mm"
	"decred.org/dcrdex/client/rpcserver"
	"decred.org/dcrdex/client/webserver"
	"decred.org/dcrdex/dex"
	"github.com/progrium/darwinkit/macos/appkit"
	"github.com/progrium/darwinkit/macos/foundation"
	"github.com/progrium/darwinkit/macos/webkit"
	"github.com/progrium/darwinkit/objc"
)

var (
	width, height  = windowWidthAndHeight()
	maxOpenWindows = 5          // what would they want to do with more than 5 😂?
	nOpenWindows   atomic.Int32 // number of open windows

	// Initialized when bisonw-desktop has been started.
	appHost string
	appURL  *url.URL
	logDir  string

	// completionHandler handles Objective-C callback functions for some
	// delegate methods.
	completionHandler = objc.ObjectFrom(C.createCompletionHandlerDelegate())

	bisonwAppIcon  = appkit.NewImageWithData(Icon)
	windowDelegate = appkit.WindowDelegate{}
	dw             = &delegateWrapper{}
)

const (
	// macOSAppTitle is the title of the macOS app. It is used to set the title
	// of the main menu.
	macOSAppTitle = "Bison Wallet"

	// The name of the Bison Wallet Js Handler. To access this function in the
	// browser window, do:
	// window.webkit.messageHandlers.<name>.postMessage(<messageBody>)
	bwJsHandlerName = "bwHandler"
)

func init() {
	// MacOS requires the app to be executed on the "main" thread only. See:
	// https://github.com/golang/go/wiki/LockOSThread. Use
	// mdCore.Dispatch(func()) to execute any function that needs to run in the
	// "main" thread.
	runtime.LockOSThread()
}

// mainCore is the darwin entry point for the Bison Wallet Desktop client.
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
		return errors.New("the --webview flag is not supported. Other OS use it for a specific reason (to support multiple windows)")
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

	// Prepare the image file for desktop notifications.
	if tmpLogoPath := storeTmpLogo(); tmpLogoPath != "" {
		defer os.RemoveAll(tmpLogoPath)
	}

	// Use a hidden "bisonw-desktop-state" file to prevent other processes when
	// bisonw-desktop is already running (e.g when non-bundled version of
	// bisonw-desktop is executed from cmd and vice versa).
	bisonwDesktopStateFile := filepath.Join(cfg.Config.AppData, cfg.Net.String(), ".bisonw-desktop-state")
	alreadyRunning, err := createDesktopStateFile(bisonwDesktopStateFile)
	if err != nil {
		return err
	}

	if alreadyRunning {
		return errors.New("bisonw-desktop is already running")
	}

	// shutdownCloser is used to execute functions that could have been executed
	// as a deferred statement in the main goroutine.
	shutdownCloser := new(shutdownCloser)
	defer shutdownCloser.Done() // execute deferred functions if we return early
	// Initialize logging.
	utc := !cfg.LocalLogs
	logMaker, closeLogger := bwapp.InitLogging(cfg.LogPath, cfg.DebugLevel, cfg.LogStdout, utc)
	shutdownCloser.Add(closeLogger)

	log = logMaker.Logger("APP")
	log.Infof("%s version %s (Go version %s)", appName, bwapp.Version, runtime.Version())
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
		shutdownCloser.Add(pprof.StopCPUProfile)
	}

	defer func() {
		if pv := recover(); pv != nil {
			log.Criticalf("Uh-oh! \n\nPanic:\n\n%v\n\nStack:\n\n%v\n\n",
				pv, string(debug.Stack()))
		}
	}()

	// Prepare the core.
	clientCore, err := core.New(cfg.Core(logMaker.Logger("CORE")))
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

	// TODO: on shutdown, stop market making and wait for trades to be
	// canceled.
	marketMaker, err := mm.NewMarketMaker(clientCore, cfg.MMConfig.EventLogDBPath, cfg.MMConfig.BotConfigPath, logMaker.Logger("MM"))
	if err != nil {
		return fmt.Errorf("error creating market maker: %w", err)
	}
	cm := dex.NewConnectionMaster(marketMaker)
	if err := cm.ConnectOnce(appCtx); err != nil {
		return fmt.Errorf("error connecting market maker")
	}

	defer func() {
		cancel()
		cm.Wait()
	}()

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

	webSrv, err := webserver.New(cfg.Web(clientCore, marketMaker, logMaker.Logger("WEB"), utc))
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

	scheme := "http"
	if cfg.WebTLS {
		scheme = "https"
	}

	appURL, err = url.ParseRequestURI(fmt.Sprintf("%s://%s", scheme, webSrv.Addr()))
	if err != nil {
		return fmt.Errorf("url.ParseRequestURI error: %w", err)
	}

	// Set the appHost to the host part of the URL. This is used to determine if
	// the URL should be opened in the webview or in the user's default browser.
	appHost, _, _ = strings.Cut(appURL.Host, ":")

	logDir = filepath.Dir(cfg.LogPath)

	app := appkit.Application_SharedApplication()

	// MacOS will always execute this method when bisonw-desktop window is about
	// to close.
	var noteSent bool
	windowDelegate.SetWindowWillClose(func(_ foundation.Notification) {
		windowsOpen := nOpenWindows.Add(-1)
		if windowsOpen > 0 {
			return // nothing to do
		}

		err := clientCore.Logout()
		if err == nil {
			return // nothing to do
		}

		if !errors.Is(err, core.ActiveOrdersLogoutErr) {
			log.Errorf("Core logout error: %v", err)
			return
		}

		if !noteSent && app.IsRunning() { // last window has been closed but app is still running
			noteSent = true
			sendDesktopNotification("Bison Wallet still running", "Bison Wallet is still resolving active DEX orders")
		}
	})

	// Set the "ActivationPolicy" to "ApplicationActivationPolicyRegular" in
	// order to run bisonw-desktop as a regular MacOS app (i.e as a non-cli
	// application).
	app.SetActivationPolicy(appkit.ApplicationActivationPolicyRegular)
	// Set "ActivateIgnoringOtherApps" to "false" in order to block other
	// attempt at opening bisonw-desktop from creating a new instance when
	// bisonw-desktop is already running. "ActivateIgnoringOtherApps" is
	// deprecated but still allowed until macOS version 14. Ventura is macOS
	// version 13.
	app.ActivateIgnoringOtherApps(false)

	delegate := newAppDelegate(logDir, func() {
		log.Infof("Shutting down...")
		cancel()
		wg.Wait()             // no-op with clean setup and shutdown
		shutdownCloser.Done() // execute shutdown functions
	})
	// Set bisonw-desktop delegate to manage bisonw-desktop behavior. See:
	// https://developer.apple.com/documentation/appkit/nsapplication/1428705-delegate?language=objc
	app.SetDelegate(&delegate)

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-appCtx.Done():
				app.Terminate(nil)
				return
			}
		}
	}()

	// Run blocks until app.Terminate() is executed and will not continue
	// execution of any deferred functions in the main thread.
	app.Run()
	return nil
}

// hasOpenWindows is a convenience function to tell if there are any windows
// currently open.
func hasOpenWindows() bool {
	return nOpenWindows.Load() > 0
}

// createNewWebView creates a new webview and window.
func createNewWebView() {
	if int(nOpenWindows.Load()) >= maxOpenWindows {
		log.Debugf("Ignoring open new window request, max number of (%d) open windows exceeded", maxOpenWindows)
		return
	}

	webView := objc.Call[webkit.WebView](completionHandler, objc.Sel("newWebview"))
	webView.SetFrameSize(foundation.Size{Width: width, Height: height})
	webView.SetAllowsBackForwardNavigationGestures(true)
	webView.SetUIDelegate(newUIDelegate())
	webView.SetNavigationDelegate(newNavigationDelegate())
	webView.Configuration().Preferences().SetJavaScriptCanOpenWindowsAutomatically(true)
	webkit.AddScriptMessageHandler(webView, bwJsHandlerName, userContentControllerDidReceiveScriptMessageHandler)
	webkit.LoadURL(webView, appURL.String())

	// Create a new window.
	win := appkit.NewWindowWithSize(width, height)
	objc.Retain(&win)

	win.SetTitle(appTitle)
	win.SetMovable(true)
	win.SetContentView(webView)
	win.MakeKeyAndOrderFront(win)
	win.SetDelegate(&windowDelegate)
	win.SetMinSize(foundation.Size{Width: 600, Height: 600})
	win.Center()

	nOpenWindows.Add(1)
}

// newAppDelegate creates a new appkit.ApplicationDelegate and sets required
// methods. Other methods can be added to the returned
// appkit.ApplicationDelegate.
func newAppDelegate(logDir string, shutdownCallback func()) appkit.ApplicationDelegate {
	ad := appkit.ApplicationDelegate{}

	ad.SetApplicationWillFinishLaunching(dw.handleApplicationWillFinishLaunching)

	ad.SetApplicationDidFinishLaunching(dw.handleApplicationDidFinishLaunching)

	ad.SetApplicationShouldHandleReopenHasVisibleWindows(dw.handleApplicationShouldHandleReopenHasVisibleWindows)

	ad.SetApplicationDockMenu(dw.handleApplicationDockMenu)

	ad.SetApplicationShouldTerminateAfterLastWindowClosed(func(appkit.Application) bool {
		return false
	})

	// MacOS will always execute this method when bisonw-desktop is about to exit
	// so we should use this opportunity to cleanup.
	ad.SetApplicationShouldTerminate(func(_ appkit.Application) appkit.ApplicationTerminateReply {
		shutdownCallback()
		return appkit.TerminateNow
	})

	return ad
}

func newUIDelegate() *webkit.UIDelegate {
	wd := &webkit.UIDelegate{}
	// MacOS will execute this method when a file upload button is clicked. See:
	wd.SetWebViewRunOpenPanelWithParametersInitiatedByFrameCompletionHandler(dw.handleWebViewRunOpenPanelWithParametersInitiatedByFrameCompletionHandler)
	return wd
}

func newNavigationDelegate() *webkit.NavigationDelegate {
	navigationDelegate := &webkit.NavigationDelegate{}

	// MacOS will execute this method when bisonw-desktop is started with a
	// self-signed certificate and a new webview is requested.
	navigationDelegate.SetWebViewDidReceiveAuthenticationChallengeCompletionHandler(dw.handleWebViewDidReceiveAuthenticationChallengeCompletionHandler)

	// MacOS will execute this method for each navigation in webview to decide
	// if to open the URL in webview or in the user's default browser.
	navigationDelegate.SetWebViewDecidePolicyForNavigationActionDecisionHandler(dw.handleWebViewDecidePolicyForNavigationActionDecisionHandler)

	return navigationDelegate
}

// delegateWrapper implements methods that a added to an
// appkit.ApplicationDelegate.
type delegateWrapper struct{}

func (_ *delegateWrapper) handleApplicationWillFinishLaunching(_ foundation.Notification) {
	app := appkit.Application_SharedApplication()
	setSystemBar(app)
	setAppMainMenuBar(app)
}

func (_ *delegateWrapper) handleApplicationDidFinishLaunching(_ foundation.Notification) {
	createNewWebView()
}

func (_ *delegateWrapper) handleApplicationShouldHandleReopenHasVisibleWindows(_ appkit.Application, hasVisibleWindows bool) bool {
	if !hasOpenWindows() {
		// bisonw-desktop is already running but there are no windows open so
		// we should create a new window.
		createNewWebView()
		return false
	}

	// bisonw-desktop is already running and there's a window open so we can
	// go ahead with the default action which is to bring the open window to
	// the front.
	return true
}

func (_ *delegateWrapper) handleApplicationDockMenu(_ appkit.Application) appkit.Menu {
	menu := appkit.NewMenu()
	menu.AddItem(appkit.NewMenuItemWithAction("New Window", "n", menuItemActionNewWindow))
	menu.AddItem(appkit.NewMenuItemWithAction("Open Logs", "l", menuItemActionOpenLogs))
	return menu
}

// handleWebViewRunOpenPanelWithParametersInitiatedByFrameCompletionHandler
// handles file selection requests from the webview. This is used to open a file
// selection dialog when the user clicks on a file upload button in the webview.
func (_ *delegateWrapper) handleWebViewRunOpenPanelWithParametersInitiatedByFrameCompletionHandler(_ webkit.WebView, p webkit.OpenPanelParameters, _ webkit.FrameInfo, completionHandler func(URLs []foundation.URL)) {
	panel := appkit.OpenPanel_OpenPanel()
	panel.SetAllowsMultipleSelection(p.AllowsMultipleSelection())
	panel.SetCanChooseDirectories(p.AllowsDirectories())
	resp := panel.RunModal()
	if resp == appkit.ModalResponseOK {
		completionHandler(panel.URLs())
		return
	}
	completionHandler([]foundation.URL{})
}

func (_ *delegateWrapper) handleWebViewDidReceiveAuthenticationChallengeCompletionHandler(_ webkit.WebView, challenge foundation.URLAuthenticationChallenge, challengeCompletionHandler func(disposition foundation.URLSessionAuthChallengeDisposition, credential foundation.URLCredential)) {
	challengeCompletionHandler(foundation.URLSessionAuthChallengeUseCredential, challenge.ProposedCredential())
}

func (_ *delegateWrapper) handleWebViewDecidePolicyForNavigationActionDecisionHandler(webView webkit.WebView, navigationAction webkit.NavigationAction, decisionHandler func(arg0 webkit.NavigationActionPolicy)) {
	decisionPolicy := webkit.NavigationActionPolicyAllow
	reqURL := navigationAction.Request().URL()
	host, _, _ := strings.Cut(reqURL.Host(), ":")
	isLogExport := reqURL.Path() == "/api/exportapplog"
	if host != appHost || isLogExport {
		decisionPolicy = webkit.NavigationActionPolicyCancel
		if isLogExport {
			openLogs()
		} else {
			openURL(reqURL.AbsoluteString())
		}
	}
	decisionHandler(decisionPolicy)
}

func menuItemActionNewWindow(_ objc.Object) {
	windows := appkit.Application_SharedApplication().OrderedWindows()
	len := len(windows)
	if len < maxOpenWindows {
		createNewWebView()
		return
	}
	// Show the last window if maxOpenWindows has been exceeded.
	windows[len-1].MakeMainWindow()
}

func menuItemActionOpenLogs(_ objc.Object) {
	openLogs()
}

func openLogs() {
	logDirURL, err := bwapp.FilePathToURL(logDir)
	if err != nil {
		log.Errorf("error constructing log directory URL: %v", err)
		return

	}
	openURL(logDirURL)
}

// setSystemBar creates and sets the system bar menu items for the app.
func setSystemBar(app appkit.Application) {
	runningMenuItem := appkit.NewMenuItem()
	runningMenuItem.SetTitle("Bison Wallet is running")
	runningMenuItem.SetEnabled(false)

	hideMenuItem := appkit.NewMenuItemWithAction("Hide", "h", func(_ objc.Object) { app.Hide(nil) })
	hideMenuItem.SetToolTip("Hide Bison Wallet")

	quitMenuItem := appkit.NewMenuItemWithAction("Quit "+macOSAppTitle, "q", func(_ objc.Object) { app.Terminate(nil) })
	quitMenuItem.SetToolTip("Quit Bison Wallet")

	// Create the status bar menu item.
	menu := appkit.NewMenuWithTitle("main")
	menu.AddItem(runningMenuItem)
	menu.AddItem(hideMenuItem)
	menu.AddItem(quitMenuItem)

	// Create the status bar icon.
	icon := appkit.NewImageWithData(Icon)
	icon.SetSize(foundation.Size{Width: 18, Height: 18})

	// Create the status bar menu. We want users to notice bisonw desktop is
	// still running (even with the dot below the dock icon).
	item := appkit.StatusBar_SystemStatusBar().StatusItemWithLength(appkit.VariableStatusItemLength)
	objc.Retain(&item)

	item.Button().SetImage(icon)
	item.Button().SetToolTip("Self-custodial multi-wallet")
	item.SetMenu(menu)
}

// setAppMainMenuBar creates and sets the main menu items for the app menu.
func setAppMainMenuBar(app appkit.Application) {
	// Create the App menu.
	appMenu := appkit.NewMenuWithTitle(macOSAppTitle)
	appMenu.AddItem(appkit.NewMenuItemWithAction("Hide "+macOSAppTitle, "h", func(_ objc.Object) { app.Hide(nil) }))
	appMenu.AddItem(appkit.NewMenuItemWithAction("Hide Others", "", func(_ objc.Object) { app.HideOtherApplications(nil) }))
	appMenu.AddItem(appkit.NewMenuItemWithAction("Show All", "", func(_ objc.Object) { app.UnhideAllApplications(nil) }))
	appMenu.AddItem(appkit.MenuItem_SeparatorItem())
	quitMenuItem := appkit.NewMenuItemWithAction("Quit "+macOSAppTitle, "q", func(_ objc.Object) { app.Terminate(nil) })
	quitMenuItem.SetToolTip("Quit " + macOSAppTitle)
	appMenu.AddItem(quitMenuItem)

	// Create the "Window" menu.
	windowMenu := appkit.NewMenuWithTitle("Window")
	windowMenu.AddItem(appkit.NewMenuItemWithAction("New Window", "n", menuItemActionNewWindow))
	windowMenu.AddItem(appkit.MenuItem_SeparatorItem())
	windowMenu.AddItem(appkit.NewMenuItemWithSelector("Minimize", "m", objc.Sel("performMiniaturize:")))
	windowMenu.AddItem(appkit.NewMenuItemWithSelector("Zoom", "z", objc.Sel("performZoom:")))
	windowMenu.AddItem(appkit.NewMenuItemWithSelector("Bring All to Front", "", objc.Sel("arrangeInFront:")))
	windowMenu.AddItem(appkit.MenuItem_SeparatorItem())
	windowMenu.AddItem(appkit.NewMenuItemWithSelector("Enter Full Screen", "f", objc.Sel("toggleFullScreen:")))

	// Create the "Edit" menu.
	editMenu := appkit.NewMenuWithTitle("Edit")
	editMenu.AddItem(appkit.NewMenuItemWithSelector("Select All", "a", objc.Sel("selectAll:")))
	editMenu.AddItem(appkit.MenuItem_SeparatorItem())
	editMenu.AddItem(appkit.NewMenuItemWithSelector("Copy", "c", objc.Sel("copy:")))
	editMenu.AddItem(appkit.NewMenuItemWithSelector("Paste", "v", objc.Sel("paste:")))
	editMenu.AddItem(appkit.NewMenuItemWithSelector("Cut", "x", objc.Sel("cut:")))
	editMenu.AddItem(appkit.NewMenuItemWithSelector("Undo", "z", objc.Sel("undo:")))
	editMenu.AddItem(appkit.NewMenuItemWithSelector("Redo", "Z", objc.Sel("redo:")))

	// Create the "Others" menu.
	othersMenu := appkit.NewMenuWithTitle("Others")
	othersMenu.AddItem(appkit.NewMenuItemWithAction("Open Logs", "l", menuItemActionOpenLogs))

	// Create the main menu bar.
	menu := appkit.NewMenuWithTitle("main")
	for _, m := range []appkit.Menu{appMenu, editMenu, windowMenu, othersMenu} {
		menuItem := appkit.NewMenuItemWithSelector(m.Title(), "", objc.Selector{})
		menuItem.SetSubmenu(m)
		menu.AddItem(menuItem)

		if menu.Title() == "Window" {
			// Set app's WindowsMenu to the Window menu. This will allow windows
			// to be grouped together in the dock icon and in the Window menu.
			// Also, MacOS will automatically add other default Window menu
			// items. See:
			// https://developer.apple.com/documentation/appkit/nsapplication/1428547-windowsmenu?language=objc.
			// TODO: Since the new update, this is not working as expected, the
			// windows are not grouped as expected.
			app.SetWindowsMenu(m)
		}
	}

	app.SetMainMenu(menu)
	return
}

func windowWidthAndHeight() (width, height float64) {
	frame := appkit.Screen_MainScreen().Frame()
	return limitedWindowWidthAndHeight(math.Round(frame.Size.Width), math.Round(frame.Size.Height))
}

// userContentControllerDidReceiveScriptMessageHandler is called when a script message
// is received from the webview. Expected arguments is an array of: 1.
// jsFunctionName as first argument, 2. jsFunction arguments (e.g function
// window.webkit.messageHandlers.bwHandler.postMessage([fnName, args...])).
// Satifies the webkit.PScriptMessageHandler interface.
func userContentControllerDidReceiveScriptMessageHandler(msgBody objc.Object) {
	// Arguments must be provided as an array(NSSingleObjectArrayI or NSArrayI).
	msgClass := msgBody.Class().Name()
	if !strings.Contains(msgClass, "Array") {
		log.Errorf("Received unexpected argument type %s (content: %s)", msgClass, msgBody.Description())
		return // do nothing
	}

	// Parse all argument to an array of strings. Individual function callers
	// can handle expected arguments parsed as string. For example, an object
	// parsed as a string will be returned as an objc stringified object { name
	// = "myName"; }.
	args := parseJSCallbackArgsString(msgBody)
	if len(args) == 0 {
		log.Errorf("Received unexpected argument type %s (content: %s)", msgClass, msgBody.Description())
		return // do nothing
	}

	// minArg is the minimum number of args expected which is the function name.
	const minArg = 1
	fnName := args[0]
	nArgs := len(args)
	switch {
	case fnName == "openURL" && nArgs > minArg:
		openURL(args[1])
	case fnName == "sendOSNotification" && nArgs > minArg:
		sendDesktopNotificationJSCallback(args[1:])
	default:
		log.Errorf("Received unexpected JS function type %s (message content: %s)", fnName, msgBody.Description())
	}
}

// sendDesktopNotificationJSCallback sends a desktop notification as request
// from a webpage script. Expected message content: [title, body].
func sendDesktopNotificationJSCallback(msg []string) {
	const expectedArgs = 2
	const defaultTitle = "Bison Wallet Notification"
	if len(msg) == 1 {
		sendDesktopNotification(defaultTitle, msg[0])
	} else if len(msg) >= expectedArgs {
		sendDesktopNotification(msg[0], msg[1])
	}
}

// openURL opens the provided path using macOS's native APIs. This will ensure
// the "path" is opened with the appropriate app (e.g a valid HTTP URL will be
// opened in the user's default browser)
func openURL(path string) {
	url := foundation.NewURLWithString(path)
	appkit.Workspace_SharedWorkspace().OpenURL(url)
}

func parseJSCallbackArgsString(msg objc.Object) []string {
	args := foundation.ArrayFrom(msg.Ptr())
	count := args.Count()
	if count == 0 {
		return nil
	}

	var argsAsStr []string
	for i := 0; i < int(count); i++ {
		ob := args.ObjectAtIndex(uint(i))
		if ob.IsNil() || ob.Description() == "<null>" {
			continue // ignore
		}
		argsAsStr = append(argsAsStr, ob.Description())
	}
	return argsAsStr
}

// createDesktopStateFile writes the id of the current process to the file
// located at filePath. If the file already exists, the process id in the file
// is checked to see if the process is still running. Returns true and a nil
// error if the file exists and bisonw-desktop is already running.
func createDesktopStateFile(filePath string) (bool, error) {
	pidB, err := os.ReadFile(filePath)
	if err == nil {
		// Check if the pid is a number.
		if pid, err := strconv.Atoi(string(pidB)); err == nil && pid != 0 {
			// Check if the process is still running.
			if p, err := os.FindProcess(pid); err == nil && p.Signal(syscall.Signal(0)) == nil {
				return true, nil
			}
		}

		if err := os.Remove(filePath); err != nil {
			return false, fmt.Errorf("failed to remove lock file %s: %w", filePath, err)
		}
	}

	err = os.WriteFile(filePath, []byte(fmt.Sprintf("%d", os.Getpid())), 0600)
	if err != nil {
		return false, fmt.Errorf("os.WriteFile error: %w", err)
	}

	return false, nil
}

// shutdownCloser is a helper for running shutdown functions in reverse order.
type shutdownCloser struct {
	closers []func()
}

// Add adds a closer function to the shutdownCloser. Done should be called when
// the shutdownCloser is no longer needed.
func (sc *shutdownCloser) Add(closer func()) {
	sc.closers = append(sc.closers, closer)
}

// Done runs all of the closer functions in reverse order.
func (sc *shutdownCloser) Done() {
	for i := len(sc.closers) - 1; i >= 0; i-- {
		sc.closers[i]()
	}
	sc.closers = nil
}

func sendDesktopNotification(title, msg string) {
	// This API is deprecated but still functional.
	notif := objc.Call[objc.Object](objc.GetClass("NSUserNotification"), objc.Sel("new"))
	objc.Retain(&notif)
	objc.Call[objc.Void](notif, objc.Sel("setTitle:"), title)
	objc.Call[objc.Void](notif, objc.Sel("setInformativeText:"), msg)

	center := objc.Call[objc.Object](objc.GetClass("NSUserNotificationCenter"), objc.Sel("defaultUserNotificationCenter"))
	if center.Ptr() != nil {
		objc.Call[objc.Void](center, objc.Sel("deliverNotification:"), notif)
	}
}
