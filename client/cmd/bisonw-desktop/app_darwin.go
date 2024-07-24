//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -lobjc -framework WebKit -framework AppKit -framework UserNotifications

#import <objc/runtime.h>
#import <WebKit/WebKit.h>
#import <AppKit/AppKit.h>
#import <UserNotifications/UserNotifications.h>

// NavigationActionPolicyCancel is an integer used in Go code to represent
// WKNavigationActionPolicyCancel
const int NavigationActionPolicyCancel = 1;

// CompletionHandlerDelegate implements methods required for executing
// completion and decision handlers.
@interface CompletionHandlerDelegate:NSObject
- (void)completionHandler:(void (^)(NSArray<NSURL *> * _Nullable URLs))completionHandler withURLs:(NSArray<NSURL *> * _Nullable)URLs;
- (void)decisionHandler:(void (^)(WKNavigationActionPolicy))decisionHandler withPolicy:(int)policy;
- (void)authenticationCompletionHandler:(void (^)(NSURLSessionAuthChallengeDisposition disposition, NSURLCredential *credential))completionHandler withChallenge:(NSURLAuthenticationChallenge *)challenge;
- (void)deliverNotificationWithTitle:(NSString *)title message:(NSString *)message icon:(NSImage *)icon;
@end

@implementation CompletionHandlerDelegate
// "completionHandler:withURLs" accepts a completion handler function from
// "webView:runOpenPanelWithParameters:initiatedByFrame:completionHandler:" and
// executes it with the provided URLs.
- (void)completionHandler:(void (^)(NSArray<NSURL *> * _Nullable URLs))completionHandler withURLs:(NSArray<NSURL *> * _Nullable)URLs {
	completionHandler(URLs);
}

// "decisionHandler:withPolicy" accepts a decision handler function from
// "webView:decidePolicyForNavigationAction:decisionHandler" and executes
// it with the provided policy.
- (void)decisionHandler:(void (^)(WKNavigationActionPolicy))decisionHandler withPolicy:(int)policy {
	policy == NavigationActionPolicyCancel ? decisionHandler(WKNavigationActionPolicyCancel) : decisionHandler(WKNavigationActionPolicyAllow);
}

// Implements "authenticationCompletionHandler:withChallenge" for "webView:didReceiveAuthenticationChallenge:completionHandler".
// See: https://developer.apple.com/forums/thread/15610.
- (void)authenticationCompletionHandler:(void (^)(NSURLSessionAuthChallengeDisposition disposition, NSURLCredential *credential))completionHandler withChallenge:(NSURLAuthenticationChallenge *)challenge {
	SecTrustRef serverTrust = challenge.protectionSpace.serverTrust;
	CFDataRef exceptions = SecTrustCopyExceptions(serverTrust);
	SecTrustSetExceptions(serverTrust, exceptions);
	CFRelease(exceptions);
	completionHandler(NSURLSessionAuthChallengeUseCredential, [NSURLCredential credentialForTrust:serverTrust]);
}

// Implements "deliverNotificationWithTitle:message:icon:" handler with the NSUserNotification
// and NSUserNotificationCenter classes which have been marked deprecated
// but it is what works atm for macOS apps. The newer UNUserNotificationCenter has some
// implementation issues and very little information to aid debugging.
// See: https://developer.apple.com/documentation/foundation/nsusernotification?language=objc and
// https://github.com/progrium/macdriver/discussions/258
- (void)deliverNotificationWithTitle:(NSString *)title message:(NSString *)message icon:(NSImage *)icon{
    NSUserNotification *notification = [NSUserNotification new];
    notification.title = title;
    notification.informativeText = message;
	notification.actionButtonTitle = @"Ok";
    notification.hasActionButton = 1;
	notification.soundName = NSUserNotificationDefaultSoundName;
	[notification setValue:icon forKey:@"_identityImage"];
    [notification setValue:@(false) forKey:@"_identityImageHasBorder"];
	[notification setValue:@YES forKey:@"_ignoresDoNotDisturb"];
	[[NSUserNotificationCenter defaultUserNotificationCenter]deliverNotification:notification];
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

	"decred.org/dcrdex/client/app"
	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/client/mm"
	"decred.org/dcrdex/client/rpcserver"
	"decred.org/dcrdex/client/webserver"
	"decred.org/dcrdex/dex"
	"github.com/progrium/macdriver/cocoa"
	mdCore "github.com/progrium/macdriver/core"
	"github.com/progrium/macdriver/objc"
	"github.com/progrium/macdriver/webkit"
)

var (
	webviewConfig  = webkit.WKWebViewConfiguration_New()
	width, height  = windowWidthAndHeight()
	maxOpenWindows = 5          // what would they want to do with more than 5 😂?
	nOpenWindows   atomic.Int32 // number of open windows

	// Initialized when bisonw-desktop has been started.
	appURL *url.URL

	// completionHandler handles Objective-C callback functions for some
	// delegate methods.
	completionHandler = objc.Object_fromPointer(C.createCompletionHandlerDelegate())
	dexcAppIcon       = cocoa.NSImage_InitWithData(mdCore.NSData_WithBytes(Icon, uint64(len(Icon))))
)

const (
	// macOSAppTitle is the title of the macOS app. It is used to set the title
	// of the main menu.
	macOSAppTitle = "Bison Wallet"

	// selOpenLogs is the selector for the "Open Logs" menu item.
	selOpenLogs = "openLogs:"
	// selNewWindow is the selector for the "New Window" menu item.
	selNewWindow = "newWindow:"
	// selIsNewWebview is the selector for a webview that require a new window.
	selIsNewWebview = "isNewWebView:"

	// NavigationActionPolicyCancel is used in Objective-C code to represent
	// WKNavigationActionPolicyCancel.
	NavigationActionPolicyCancel = 1
)

func init() {
	// MacOS requires the app to be executed on the "main" thread only. See:
	// https://github.com/golang/go/wiki/LockOSThread. Use
	// mdCore.Dispatch(func()) to execute any function that needs to run in the
	// "main" thread.
	runtime.LockOSThread()

	// Set the user controller. This object coordinates interactions the app’s
	// native code and the webpage’s scripts and other content. See:
	// https://developer.apple.com/documentation/webkit/wkwebviewconfiguration/1395668-usercontentcontroller?language=objc.
	webviewConfig.Set("userContentController:", objc.Get("WKUserContentController").Alloc().Init())
	// Set "developerExtrasEnabled" to true to allow viewing the developer
	// console.
	webviewConfig.Preferences().SetValueForKey(mdCore.True, mdCore.String("developerExtrasEnabled"))
	// Set "DOMPasteAllowed" to true to allow user's paste text.
	webviewConfig.Preferences().SetValueForKey(mdCore.True, mdCore.String("DOMPasteAllowed"))

	// Set to false in order to prevent bisonw-desktop from exiting when the last
	// window is closed.
	cocoa.TerminateAfterWindowsClose = false
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
	logMaker, closeLogger := app.InitLogging(cfg.LogPath, cfg.DebugLevel, cfg.LogStdout, utc)
	shutdownCloser.Add(closeLogger)

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

	// Default to serving the web interface over TLS.
	cfg.WebTLS = true
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

	logDir := filepath.Dir(cfg.LogPath)
	classWrapper := initCocoaDefaultDelegateClassWrapper(logDir)

	// MacOS will always execute this method when bisonw-desktop is about to exit
	// so we should use this opportunity to cleanup. See:
	// https://developer.apple.com/documentation/appkit/nsapplicationdelegate/1428642-applicationshouldterminate?language=objc
	classWrapper.AddMethod("applicationShouldTerminate:", func(s objc.Object) mdCore.NSUInteger {
		cancel()              // no-op with clean rpc/web server setup
		wg.Wait()             // no-op with clean setup and shutdown
		shutdownCloser.Done() // execute shutdown functions
		return mdCore.NSUInteger(1)
	})

	// MacOS will always execute this method when bisonw-desktop window is about
	// to close See:
	// https://developer.apple.com/documentation/appkit/nswindowdelegate/1419605-windowwillclose?language=objc
	var noteSent bool
	classWrapper.AddMethod("windowWillClose:", func(_ objc.Object) {
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

		if !noteSent && cocoa.NSApp().IsRunning() { // last window has been closed but app is still running
			noteSent = true
			sendDesktopNotification("DEX client still running", "DEX client is still resolving active DEX orders")
		}
	})

	// Bind JS callback function handler.
	bindJSFunctionHandler()

	app := cocoa.NSApp()
	// Set the "ActivationPolicy" to "NSApplicationActivationPolicyRegular" in
	// order to run bisonw-desktop as a regular MacOS app (i.e as a non-cli
	// application). See:
	// https://developer.apple.com/documentation/appkit/nsapplication/1428621-setactivationpolicy?language=objc
	app.SetActivationPolicy(cocoa.NSApplicationActivationPolicyRegular)
	// Set "ActivateIgnoringOtherApps" to "false" in order to block other
	// attempt at opening bisonw-desktop from creating a new instance when
	// bisonw-desktop is already running. See:
	// https://developer.apple.com/documentation/appkit/nsapplication/1428468-activateignoringotherapps/.
	// "ActivateIgnoringOtherApps" is deprecated but still allowed until macOS
	// version 14. Ventura is macOS version 13.
	app.ActivateIgnoringOtherApps(false)
	// Set bisonw-desktop delegate to manage bisonw-desktop behavior. See:
	// https://developer.apple.com/documentation/appkit/nsapplication/1428705-delegate?language=objc
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

// createNewWebView creates a new webview with the specified URL. The actual
// window will be created when the webview is loaded (i.e the
// "webView:didFinishNavigation:" method have been executed).
func createNewWebView() {
	if int(nOpenWindows.Load()) >= maxOpenWindows {
		log.Debugf("Ignoring open new window request, max number of (%d) open windows exceeded", maxOpenWindows)
		return
	}

	// Create a new webview and loads the appURL.
	req := mdCore.NSURLRequest_Init(mdCore.URL(appURL.String()))
	webView := webkit.WKWebView_Init(mdCore.Rect(0, 0, float64(width), float64(height)), webviewConfig)
	webView.Object.Class().AddMethod(selIsNewWebview, func(_ objc.Object) objc.Object { return mdCore.True })
	webView.LoadRequest(req)
	webView.SetAllowsBackForwardNavigationGestures_(true)
	webView.SetUIDelegate_(cocoa.DefaultDelegate)
	webView.SetNavigationDelegate_(cocoa.DefaultDelegate)
}

// cocoaDefaultDelegateClassWrapper wraps cocoa.DefaultDelegateClass.
type cocoaDefaultDelegateClassWrapper struct {
	objc.Class
}

// initCocoaDefaultDelegateClassWrapper creates a new
// *cocoaDefaultDelegateClassWrapper and adds required Object-C methods. Other
// methods can be added to the returned *cocoaDefaultDelegateClassWrapper.
func initCocoaDefaultDelegateClassWrapper(logDir string) *cocoaDefaultDelegateClassWrapper {
	ad := &cocoaDefaultDelegateClassWrapper{
		Class: cocoa.DefaultDelegateClass,
	}

	// Set the app's main and status bar menu when we receive
	// NSApplicationWillFinishLaunchingNotification. See:
	//  - https://github.com/go-gl/glfw/blob/master/v3.3/glfw/glfw/src/cocoa_init.m#L427-L443
	//  - https://developer.apple.com/documentation/appkit/nsapplicationwillfinishlaunchingnotification?language=objc
	ad.AddMethod("applicationWillFinishLaunching:", ad.handleApplicationWillFinishLaunching)
	// MacOS will always send a notification named
	// "NSApplicationDidFinishLaunchingNotification" when an application has
	// finished launching. See:
	// https://developer.apple.com/documentation/appkit/nsapplicationdidfinishlaunchingnotification?language=objc
	ad.AddMethod("applicationDidFinishLaunching:", ad.handleApplicationDidFinishLaunching)
	// MacOS will always execute this method when bisonw-desktop icon on the dock
	// is clicked or a new process is about to start, so we hijack the action
	// and create new windows if all windows have been closed. See:
	// https://developer.apple.com/documentation/appkit/nsapplicationdelegate/1428638-applicationshouldhandlereopen?language=objc
	ad.AddMethod("applicationShouldHandleReopen:hasVisibleWindows:", ad.handleApplicationShouldHandleReopenHasVisibleWindows)
	// "applicationDockMenu:" method returns the app's dock menu. See:
	// https://developer.apple.com/documentation/appkit/nsapplicationdelegate/1428564-applicationdockmenu?language=objc
	ad.AddMethod("applicationDockMenu:", ad.handleApplicationDockMenu)
	// WebView will execute this method when the page has loaded. We can then
	// create a new window to avoid a temporary blank window. See:
	// https://developer.apple.com/documentation/webkit/wknavigationdelegate/1455629-webview?language=objc
	// NOTE: This method actually receives three argument but the docs said to
	// expect two (webView and navigation).
	ad.AddMethod("webView:didFinishNavigation:", ad.handleWebViewDidFinishNavigation)
	// MacOS will execute this method when a file upload button is clicked. See:
	// https://developer.apple.com/documentation/webkit/wkuidelegate/1641952-webview?language=objc
	ad.AddMethod("webView:runOpenPanelWithParameters:initiatedByFrame:completionHandler:", ad.handleWebViewRunOpenPanelWithParametersInitiatedByFrameCompletionHandler)
	// MacOS will execute this method for each navigation in webview to decide
	// if to open the URL in webview or in the user's default browser. See:
	// https://developer.apple.com/documentation/webkit/wknavigationdelegate/1455641-webview?language=objc
	ad.AddMethod("webView:decidePolicyForNavigationAction:decisionHandler:", ad.handleWebViewDecidePolicyForNavigationActionDecisionHandler)
	// MacOS will execute this method when bisonw-desktop is started with a
	// self-signed certificate and a new webview is requested. See:
	// https://developer.apple.com/documentation/webkit/wknavigationdelegate/1455638-webview?language=objc
	// and https://developer.apple.com/forums/thread/15610.
	ad.AddMethod("webView:didReceiveAuthenticationChallenge:completionHandler:", ad.handleWebViewDidReceiveAuthenticationChallengeCompletionHandler)

	// Add custom selectors to the app delegate since there are reused in
	// different menus. App delegates methods should be added before NSApp is
	// initialized.
	ad.AddMethod(selOpenLogs, func(_ objc.Object) {
		logDirURL, err := app.FilePathToURL(logDir)
		if err != nil {
			log.Errorf("error constructing log directory URL: %v", err)
		} else {
			openURL(logDirURL)
		}
	})
	ad.AddMethod(selNewWindow, func(_ objc.Object) {
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

	return ad
}

func (ad *cocoaDefaultDelegateClassWrapper) handleApplicationWillFinishLaunching(_ objc.Object) {
	// Create the status bar menu. We want users to notice bisonw desktop is
	// still running (even with the dot below the dock icon).
	obj := cocoa.NSStatusBar_System().StatusItemWithLength(cocoa.NSVariableStatusItemLength)
	obj.Retain()
	obj.Button().SetImage(cocoa.NSImage_InitWithData(mdCore.NSData_WithBytes(Icon, uint64(len(Icon)))))
	obj.Button().Image().SetSize(mdCore.Size(18, 18))
	obj.Button().SetToolTip("Self-custodial multi-wallet")

	runningItem := cocoa.NSMenuItem_New()
	runningItem.SetTitle("Dex Client is running")
	runningItem.SetEnabled(false)

	menu := cocoa.NSMenu_New()
	menu.AddItem(runningItem)
	quitMenuItem := cocoa.NSMenuItem_Init("Quit "+macOSAppTitle, objc.Sel("terminate:"), "q")
	quitMenuItem.SetToolTip("Quit DEX client")
	menu.AddItem(quitMenuItem)
	obj.SetMenu(menu)

	setAppMainMenuBar()

	// Hide the application until it is ready to be shown when we receive
	// the "NSApplicationDidFinishLaunchingNotification" below. This also
	// allows us to ensure the menu bar is redrawn.
	cocoa.NSApp().TryToPerform_with_(objc.Sel("hide:"), nil)
}

func (ad *cocoaDefaultDelegateClassWrapper) handleApplicationDidFinishLaunching(_, _ objc.Object) {
	// Unhide the app on the main thread after it has finished launching we need
	// to give this priority before creating the window to ensure the window is
	// immediately visible when it's created. This also has the side effect of
	// redrawing the menu bar which will be unresponsive until it is redrawn.
	mdCore.Dispatch(func() {
		cocoa.NSApp().TryToPerform_with_(objc.Sel("unhide:"), nil)
	})

	createNewWebView()
}

func (ad *cocoaDefaultDelegateClassWrapper) handleApplicationShouldHandleReopenHasVisibleWindows(_ objc.Object) bool {
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

func (ad *cocoaDefaultDelegateClassWrapper) handleApplicationDockMenu(_ objc.Object) objc.Object {
	menu := cocoa.NSMenu_New()
	newWindowMenuItem := cocoa.NSMenuItem_Init("New Window", objc.Sel(selNewWindow), "n")
	logsMenuItem := cocoa.NSMenuItem_Init("Open Logs", objc.Sel(selOpenLogs), "l")
	menu.AddItem(newWindowMenuItem)
	menu.AddItem(logsMenuItem)
	return menu
}

func (ad *cocoaDefaultDelegateClassWrapper) handleWebViewRunOpenPanelWithParametersInitiatedByFrameCompletionHandler(_ objc.Object, webview objc.Object, param objc.Object, fram objc.Object, completionHandlerFn objc.Object) {
	panel := objc.Get("NSOpenPanel").Send("openPanel")
	openFiles := panel.Send("runModal").Bool()
	if !openFiles {
		completionHandler.Send("completionHandler:withURLs:", completionHandlerFn, nil)
		return
	}
	completionHandler.Send("completionHandler:withURLs:", completionHandlerFn, panel.Send("URLs"))
}

func (ad *cocoaDefaultDelegateClassWrapper) handleWebViewDidReceiveAuthenticationChallengeCompletionHandler(_ objc.Object, webview objc.Object, challenge objc.Object, challengeCompletionHandler objc.Object) {
	completionHandler.Send("authenticationCompletionHandler:withChallenge:", challengeCompletionHandler, challenge)
}

func (ad *cocoaDefaultDelegateClassWrapper) handleWebViewDidFinishNavigation(_ objc.Object /* delegate */, webView objc.Object, _ objc.Object /* navigation */) {
	// Return early if we already created a window for this webview.
	if !mdCore.True.Equals(webView.Send(selIsNewWebview)) {
		return // Nothing to do. This is just a normal window refresh.
	}

	// Overwrite the webview "selIsNewWebview" method to return false. This will
	// prevent this new webview from opening new windows later.
	webView.Class().AddMethod(selIsNewWebview, func(_ objc.Object) objc.Object { return mdCore.False })
	nOpenWindows.Add(1) // increment the number of open windows

	// Create a new window and set the webview as its content view.
	win := cocoa.NSWindow_Init(mdCore.NSMakeRect(0, 0, float64(width), float64(height)), cocoa.NSClosableWindowMask|cocoa.NSTitledWindowMask|cocoa.NSResizableWindowMask|cocoa.NSFullSizeContentViewWindowMask|cocoa.NSMiniaturizableWindowMask, cocoa.NSBackingStoreBuffered, false)
	win.SetTitle(appTitle)
	win.Center()
	win.SetMovable_(true)
	win.SetContentView(webkit.WKWebView_fromRef(webView))
	win.SetMinSize_(mdCore.NSSize{Width: 600, Height: 600})
	win.MakeKeyAndOrderFront(nil)
	win.SetDelegate_(cocoa.DefaultDelegate)
}

func (ad *cocoaDefaultDelegateClassWrapper) handleWebViewDecidePolicyForNavigationActionDecisionHandler(delegate objc.Object, webview objc.Object, navigation objc.Object, decisionHandler objc.Object) {
	reqURL := mdCore.NSURLRequest_fromRef(navigation.Send("request")).URL()
	destinationHost := reqURL.Host().String()
	var decisionPolicy int
	if appURL.Hostname() != destinationHost {
		decisionPolicy = NavigationActionPolicyCancel
		openURL(reqURL.String())
	}
	completionHandler.Send("decisionHandler:withPolicy:", decisionHandler, decisionPolicy)
}

// setAppMainMenuBar creates and sets the main menu items for the app menu.
func setAppMainMenuBar() {
	// Create the App menu.
	appMenu := cocoa.NSMenu_Init(macOSAppTitle)

	// Create the menu items.
	hideMenuItem := cocoa.NSMenuItem_Init("Hide "+macOSAppTitle, objc.Sel("hide:"), "h")
	hideOthersMenuItem := cocoa.NSMenuItem_Init("Hide Others", objc.Sel("hideOtherApplications:"), "")
	showAllMenuItem := cocoa.NSMenuItem_Init("Show All", objc.Sel("unhideAllApplications:"), "")
	quitMenuItem := cocoa.NSMenuItem_Init("Quit "+macOSAppTitle, objc.Sel("terminate:"), "q")
	quitMenuItem.SetToolTip("Quit DEX client")

	// Add the menu items.
	appMenu.AddItem(hideMenuItem)
	appMenu.AddItem(hideOthersMenuItem)
	appMenu.AddItem(showAllMenuItem)
	appMenu.AddItem(cocoa.NSMenuItem_Separator())
	appMenu.AddItem(quitMenuItem)

	// Create the "Window" menu.
	windowMenu := cocoa.NSMenu_Init("Window")

	// Create the "Window" menu items.
	newWindowMenuItem := cocoa.NSMenuItem_Init("New Window", objc.Sel(selNewWindow), "n")
	minimizeMenuItem := cocoa.NSMenuItem_Init("Minimize", objc.Sel("performMiniaturize:"), "m")
	zoomMenuItem := cocoa.NSMenuItem_Init("Zoom", objc.Sel("performZoom:"), "z")
	frontMenuItem := cocoa.NSMenuItem_Init("Bring All to Front", objc.Sel("arrangeInFront:"), "")
	fullScreenMenuItem := cocoa.NSMenuItem_Init("Enter Full Screen", objc.Sel("toggleFullScreen:"), "f")

	// Add the "Window" menu items.
	windowMenu.AddItem(newWindowMenuItem)
	windowMenu.AddItem(cocoa.NSMenuItem_Separator())
	windowMenu.AddItem(minimizeMenuItem)
	windowMenu.AddItem(zoomMenuItem)
	windowMenu.AddItem(frontMenuItem)
	windowMenu.AddItem(cocoa.NSMenuItem_Separator())
	windowMenu.AddItem(fullScreenMenuItem)

	// Create the "Others" menu.
	othersMenu := cocoa.NSMenu_Init("Others")

	// Create the "Others" menu items.
	logsMenuItem := cocoa.NSMenuItem_Init("Open Logs", objc.Sel(selOpenLogs), "l")

	// Add the "Others" menu item.
	othersMenu.AddItem(logsMenuItem)

	// Create the main menu bar.
	menuBar := cocoa.NSMenu_New()
	app := cocoa.NSApp()
	for _, menu := range []cocoa.NSMenu{appMenu, windowMenu, othersMenu} {
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

	app.SetMainMenu(menuBar)
	return
}

func windowWidthAndHeight() (width, height int) {
	frame := cocoa.NSScreen_Main().Frame()
	return limitedWindowWidthAndHeight(int(math.Round(frame.Size.Width)), int(math.Round(frame.Size.Height)))
}

// bindJSFunctionHandler exports a function handler callable in the frontend.
// The exported function will appear under the given name as a global JavaScript
// function window.webkit.messageHandlers.bwHandler.postMessage([fnName,
// args...]).
// Expected arguments is an array of:
// 1. jsFunctionName as first argument
// 2. jsFunction arguments
func bindJSFunctionHandler() {
	const fnName = "bwHandler"

	// Create and register a new objc class for the function handler.
	fnClass := objc.NewClass(fnName, "NSObject")
	objc.RegisterClass(fnClass)

	// JS function handler must implement the WKScriptMessageHandler protocol.
	// See:
	// https://developer.apple.com/documentation/webkit/wkscriptmessagehandler?language=objc
	fnClass.AddMethod("userContentController:didReceiveScriptMessage:", handleJSFunctionsCallback)

	// The name of this function in the browser window is
	// window.webkit.messageHandlers.<name>.postMessage(<messageBody>), where
	// <name> corresponds to the value of this parameter. See:
	// https://developer.apple.com/documentation/webkit/wkusercontentcontroller/1537172-addscriptmessagehandler?language=objc
	webviewConfig.Get("userContentController").Send("addScriptMessageHandler:name:", objc.Get(fnName).Alloc().Init(), mdCore.String(fnName))
}

// handleJSFunctionsCallback handles function calls from a javascript
// environment.
func handleJSFunctionsCallback(f_ objc.Object /* functionHandler */, ct objc.Object /* WKUserContentController */, msg objc.Object, wv objc.Object /* webview */) {
	// Arguments must be provided as an array(NSSingleObjectArrayI or NSArrayI).
	msgBody := msg.Get("body")
	msgClass := msgBody.Class().String()
	if !strings.Contains(msgClass, "Array") {
		log.Errorf("Received unexpected argument type %s (content: %s)", msgClass, msgBody.String())
		return // do nothing
	}

	// Parse all argument to an array of strings. Individual function callers
	// can handle expected arguments parsed as string. For example, an object
	// parsed as a string will be returned as an objc stringified object { name
	// = "myName"; }.
	args := parseJSCallbackArgsString(msgBody)
	if len(args) == 0 {
		log.Errorf("Received unexpected argument type %s (content: %s)", msgClass, msgBody.String())
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
		log.Errorf("Received unexpected JS function type %s (message content: %s)", fnName, msgBody.String())
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
	// See: https://developer.apple.com/documentation/appkit/nsworkspace?language=objc
	cocoa.NSWorkspace_sharedWorkspace().Send("openURL:", mdCore.NSURL_Init(path))
}

func parseJSCallbackArgsString(msg objc.Object) []string {
	args := mdCore.NSArray_fromRef(msg)
	count := args.Count()
	if count == 0 {
		return nil
	}

	var argsAsStr []string
	for i := 0; i < int(count); i++ {
		ob := args.ObjectAtIndex(uint64(i))
		if ob.Class().String() == "NSNull" /* this is the string representation of the null type in objc. */ {
			continue // ignore
		}
		argsAsStr = append(argsAsStr, ob.String())
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
	nsTitle := mdCore.NSString_FromString(title)
	nsMessage := mdCore.NSString_FromString(msg)
	completionHandler.Send("deliverNotificationWithTitle:message:icon:", nsTitle, nsMessage, dexcAppIcon)
}
