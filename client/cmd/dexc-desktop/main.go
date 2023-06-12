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
	"context"
	_ "embed"
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
	_ "decred.org/dcrdex/client/asset/bch"  // register bch asset
	_ "decred.org/dcrdex/client/asset/btc"  // register btc asset
	_ "decred.org/dcrdex/client/asset/dash" // register dash asset
	_ "decred.org/dcrdex/client/asset/dcr"  // register dcr asset
	_ "decred.org/dcrdex/client/asset/dgb"  // register dgb asset
	_ "decred.org/dcrdex/client/asset/doge" // register doge asset
	_ "decred.org/dcrdex/client/asset/firo" // register firo asset
	_ "decred.org/dcrdex/client/asset/ltc"  // register ltc asset
	_ "decred.org/dcrdex/client/asset/zec"  // register zec asset
	"decred.org/dcrdex/client/mm"

	// Ethereum loaded in client/app/importlgpl.go

	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/client/rpcserver"
	"decred.org/dcrdex/client/webserver"
	"decred.org/dcrdex/dex"
	"fyne.io/systray"
	"github.com/gen2brain/beeep"
	"github.com/webview/webview"
)

const appName = "dexc-desktop"

var (
	log     dex.Logger
	exePath = findExePath()
	srcDir  = filepath.Join(filepath.Dir(exePath), "src")
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
	// would be substantially smaller than the dexc binary, but when done that
	// way, the opened window does not have the icon that the system associates
	// with dexc and taskbar icons won't be stacked. Instead, we'll create a
	// short path here and execute ourself with the --webview flag.
	if cfg.Webview != "" {
		runWebview(cfg.Webview)
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
						sendDesktopNotification("DEX client still running", "DEX client is still resolving active DEX orders")
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
	cmd.Process.Kill()
}

func closeAllWindows() {
	m := windowManager
	m.Lock()
	defer m.Unlock()
	for windowID, cmd := range m.windows {
		cmd.Process.Kill()
		delete(m.windows, windowID)
	}
}

func runWebview(url string) {
	w := webview.New(true)
	defer w.Destroy()
	w.SetTitle("Decred DEX Client")
	w.SetSize(600, 600, webview.HintMin)

	width, height := int(C.display_width()), int(C.display_height())
	if width <= 0 || width > 1920 {
		width = 1920
	}
	if height <= 0 || height > 1080 {
		height = 1080
	}

	w.SetSize(width, height, webview.HintNone)
	w.Navigate(url)
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

//go:embed src/dexc.png
var FavIcon []byte

//go:embed src/symbol-bw-round.png
var SymbolBWIcon []byte

func systrayOnReady(ctx context.Context, logDirectory string, openC chan<- struct{},
	killC chan<- os.Signal, activeState <-chan bool) {
	systray.SetIcon(FavIcon)
	systray.SetTitle("DEX client")
	systray.SetTooltip("Self-custodial multi-wallet")

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

	mOpen := systray.AddMenuItem("Open", "Open DEX client window")
	mOpen.SetIcon(SymbolBWIcon)
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
				log.Debug("Opening browser to log directory at", logDirURL)
				runWebviewSubprocess(ctx, logDirURL)
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

	mQuit := systray.AddMenuItem("Force Quit", "Force DEX client to close with active orders.")
	go func() {
		for {
			select {
			case active := <-activeState:
				if active {
					mQuit.SetTitle("Force Quit")
					mQuit.SetTooltip("Force DEX client to close with active orders.")
				} else {
					mQuit.SetTitle("Quit")
					mQuit.SetTooltip("Shutdown the DEX client. You have no active orders.")
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
	err := beeep.Notify(title, msg, filepath.Join(srcDir, "dexc.png"))
	if err != nil {
		log.Errorf("error sending desktop notification: %v", err)
		return
	}
}
