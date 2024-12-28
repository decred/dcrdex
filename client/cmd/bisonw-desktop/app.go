//go:build !darwin

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
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"runtime/pprof"
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
	"fyne.io/systray"
	"github.com/gen2brain/beeep"
	"github.com/pkg/browser"
	"github.com/webview/webview"
)

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

	// A single process cannot run multiple webview windows, so we run webview
	// as a subprocess. We could create a simpler webview binary to call that
	// would be substantially smaller than the bisonw binary, but when done that
	// way, the opened window does not have the icon that the system associates
	// with bisonw and taskbar icons won't be stacked. Instead, we'll create a
	// short path here and execute ourself with the --webview flag.
	if cfg.Webview != "" {
		runWebview(cfg.Webview)
		return nil
	}

	// Prepare the image file for desktop notifications.
	if tmpLogoPath := storeTmpLogo(); tmpLogoPath != "" {
		defer os.RemoveAll(tmpLogoPath)
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
	clientCore, err := core.New(cfg.Core(logMaker.Logger("CORE")))
	if err != nil {
		return fmt.Errorf("error creating client core: %w", err)
	}

	// Handle shutdown by user (if running via terminal), or on system shutdown.
	// TODO: SIGTERM is apparently spoofed by Go for Windows. Nice feature, but
	// not well documented. Test to verify. Could also catch SIGKILL, which is
	// sent after a configured timeout if the program doesn't exit on SIGTERM.
	killChan := make(chan os.Signal, 1)
	signal.Notify(killChan, os.Interrupt /* ctrl-c */, syscall.SIGTERM /* system shutdown */)
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
		log.Info("Exiting bisonw main.")
		cancel()  // no-op with clean rpc/web server setup
		wg.Wait() // no-op with clean setup and shutdown
	}()

	// TODO: on shutdown, stop market making and wait for trades to be
	// canceled.
	marketMaker, err := mm.NewMarketMaker(clientCore, cfg.MMConfig.EventLogDBPath, cfg.BotConfigPath, logMaker.Logger("MM"))
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

	// No errors running webserver, so we can be certain we won any race between
	// starting instances. Start the sync server now.
	openC := make(chan struct{}) // no buffer. Is ignored if window is currently open
	wg.Add(1)
	go func() {
		defer wg.Done()
		runServer(appCtx, syncDir, openC, killChan, cfg.Net)
	}()

	openWindow := func() {
		wg.Add(1)
		go func() {
			runWebviewSubprocess(appCtx, "http://"+webSrv.Addr())
			wg.Done()
		}()
	}

	openWindow()

	wg.Add(1)
	go func() {
		defer func() {
			systray.SetTooltip("Shutting down. Please wait...")
			systray.Quit()
			wg.Done()
			defer cancel()
		}()
	windowloop:
		for {
			var backgroundNoteSent bool
			select {
			case <-windowManager.zeroLeft:
				if windowManager.persist.Load() {
					continue // ignore
				}
			logout:
				for {
					err := clientCore.Logout()
					if err == nil {
						// Okay to quit.
						break windowloop
					}
					if !errors.Is(err, core.ActiveOrdersLogoutErr) {
						// Unknown error. Force shutdown.
						log.Errorf("Core logout error: %v", err)
						break windowloop
					}
					if !backgroundNoteSent {
						sendDesktopNotification("Bison Wallet still running", "Bison Wallet is still resolving active DEX orders")
						backgroundNoteSent = true
					}
					// Can't log out. Keep checking until either
					//   1. We can log out. Exit the program.
					//   2. The user reopens the window (via syncserver).
					select {
					case <-time.After(time.Minute):
						// Try to log out again.
						continue logout
					case <-openC:
						// re-open the window
						openWindow()
						continue windowloop
					case <-appCtx.Done():
						break windowloop
					}
				}
			case <-appCtx.Done():
				break windowloop
			case <-openC:
				openWindow()
			}
		}
	}()

	activeState := make(chan bool, 8)
	wg.Add(1)
	go func() {
		defer wg.Done()
		active := true // starting with "Force Quit"
		for {
			select {
			case <-time.After(time.Second):
				// inelegant polling? Even Core doesn't know until it checks, so
				// can't feasibly rig a signal from Core without changes there.
				coreActive := clientCore.Active()
				if coreActive != active {
					active = coreActive
					activeState <- coreActive
				}
			case <-appCtx.Done():
				return
			}
		}
	}()

	systray.Run(func() {
		systrayOnReady(appCtx, filepath.Dir(cfg.LogPath), openC, killChan, activeState)
	}, nil)

	closeAllWindows()

	log.Infof("Shutting down...")
	cancel()
	wg.Wait()

	return nil
}

var windowManager = &struct {
	sync.Mutex
	counter  uint32
	windows  map[uint32]*exec.Cmd
	zeroLeft chan struct{}
	persist  atomic.Bool
}{
	windows:  make(map[uint32]*exec.Cmd),
	zeroLeft: make(chan struct{}, 1),
}

func closeWindow(windowID uint32) {
	m := windowManager
	m.Lock()
	cmd, found := m.windows[windowID]
	if !found {
		m.Unlock()
		// Probably killed by caller via closeAllWindows.
		return
	}
	delete(m.windows, windowID)
	remain := len(m.windows)
	m.Unlock()
	if remain == 0 {
		select {
		case m.zeroLeft <- struct{}{}:
		default:
		}
	}
	log.Infof("Closing window. %d windows remain open.", remain)
	if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		log.Errorf("Failed to kill %v: %v", cmd, err)
	}
}

func closeAllWindows() {
	m := windowManager
	m.Lock()
	defer m.Unlock()
	for windowID, cmd := range m.windows {
		if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			log.Errorf("Failed to kill %v: %v", cmd, err)
		}
		delete(m.windows, windowID)
	}
}

// bindJSFunctions exports functions callable in the frontend
func bindJSFunctions(w webview.WebView) {
	w.Bind("isWebview", func() bool {
		return true
	})

	w.Bind("openUrl", func(url string) {
		var err error
		switch runtime.GOOS {
		case "linux":
			err = exec.Command("xdg-open", url).Start()
		case "windows":
			err = exec.Command("rundll32", "url.dll", "FileProtocolHandler", url).Start()
		}
		if err != nil {
			log.Errorf("unable to run URL handler: %s", err.Error())
		}
	})

	w.Bind("sendOSNotification", func(title, body string) {
		sendDesktopNotification(title, body)
	})
}

func runWebview(url string) {
	w := webview.New(true)
	defer w.Destroy()
	w.SetTitle(appTitle)
	w.SetSize(600, 600, webview.HintMin)
	if runtime.GOOS == "windows" { // windows can use icons in its resources section, or ico files
		useIcon(w, "#32512") // IDI_APPLICATION, see winres.json and https://learn.microsoft.com/en-us/windows/win32/menurc/about-icons
	} else {
		useIconBytes(w, Icon) // useIcon(w, "src/dexc.png")
	}

	width, height := limitedWindowWidthAndHeight(int(C.display_width()), int(C.display_height()))

	w.SetSize(width, height, webview.HintNone)
	w.Navigate(url)
	bindJSFunctions(w)
	w.Run()
}

func runWebviewSubprocess(ctx context.Context, url string) {
	cmd := exec.CommandContext(ctx, exePath, "--webview", url)
	if err := cmd.Start(); err != nil {
		log.Errorf("webview start error: %v", err)
		return
	}
	m := windowManager
	m.Lock()
	m.counter++
	windowID := windowManager.counter
	m.windows[windowID] = cmd
	windowCount := len(m.windows)
	m.Unlock()
	defer closeWindow(windowID)
	log.Infof("Opening new window. %d windows open now", windowCount)
	if err := cmd.Wait(); err != nil {
		log.Errorf("webview error: %v", err)
	}
}

func systrayOnReady(ctx context.Context, logDirectory string, openC chan<- struct{},
	killC chan<- os.Signal, activeState <-chan bool) {
	systray.SetIcon(FavIcon)
	systray.SetTitle("Bison Wallet")
	systray.SetTooltip("Self-custodial multi-wallet with atomic swap capability, by Decred.")

	// TODO: Consider reworking main so we can show the icon earlier?
	// mStarting := systray.AddMenuItem("Starting...", "Starting up. Please wait...")
	// var addr string
	// var ok bool
	// select {
	// case addr, ok = <-webserverReady:
	// 	if !ok { // no webserver started
	// 		fmt.Fprintln(os.Stderr, "Web server required!")
	// 		cancel()
	// 		return
	// 	}
	// case <-mainDone:
	// 	return
	// }

	// mStarting.Hide()

	mOpen := systray.AddMenuItem("Open", "Open Bison Wallet window")
	mOpen.SetIcon(FavIcon)
	go func() {
		for range mOpen.ClickedCh {
			select {
			case openC <- struct{}{}:
				log.Debug("Received window reopen request from system tray")
			default:
				log.Infof("Ignored a window open request from the system tray")
			}
		}
	}()

	systray.AddSeparator()

	if logDirURL, err := app.FilePathToURL(logDirectory); err != nil {
		log.Errorf("error constructing log directory URL: %v", err)
	} else {
		mLogs := systray.AddMenuItem("Open logs folder", "Open the folder with your DEX logs.")
		go func() {
			for range mLogs.ClickedCh {
				if err := browser.OpenURL(logDirURL); err != nil {
					fmt.Fprintln(os.Stderr, err) // you're actually looking for the log file, so info on stdout is warranted
					log.Errorf("Unable to open log file directory: %v", err)
				}
			}
		}()
	}

	systray.AddSeparator()

	mPersist := systray.AddMenuItemCheckbox("Persist after windows closed.",
		"Keep the process running after all windows are closed. "+
			"Shutdown is initiated through the Quit item in the system tray menu.", false)
	go func() {
		for range mPersist.ClickedCh {
			var persisting = !mPersist.Checked() // toggle
			if persisting {
				mPersist.Check()
			} else {
				mPersist.Uncheck()
			}
			windowManager.persist.Store(persisting)
		}
	}()

	mQuit := systray.AddMenuItem("Force Quit", "Force Bison Wallet to close with active orders.")
	go func() {
		for {
			select {
			case active := <-activeState:
				if active {
					mQuit.SetTitle("Force Quit")
					mQuit.SetTooltip("Force Bison Wallet to close with active orders.")
				} else {
					mQuit.SetTitle("Quit")
					mQuit.SetTooltip("Shutdown Bison Wallet. You have no active orders.")
				}
			case <-mQuit.ClickedCh:
				mOpen.Disable()
				mQuit.Disable()
				killC <- os.Interrupt
				return
			}
		}
	}()
}

func sendDesktopNotification(title, msg string) {
	err := beeep.Notify(title, msg, tmpLogoPath)
	if err != nil {
		log.Errorf("error sending desktop notification: %v", err)
		return
	}
}
