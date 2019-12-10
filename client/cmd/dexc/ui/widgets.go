// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package ui

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"

	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/client/rpcserver"
	"decred.org/dcrdex/client/webserver"
	"github.com/decred/slog"
	"github.com/gdamore/tcell"
	"github.com/rivo/tview"
)

var (
	// The application context. Provided to the Run function, but stored globally.
	appCtx context.Context
	// The tview application.
	app *tview.Application
	// The core DEX client application. Used by both the RPC server and the
	// web server.
	clientCore *core.Core
	// These are the main view widgets and loggers attached to their journals.
	screen            *Screen
	mainMenu          *chooser
	appJournal        *journal
	webView           *serverView
	rpcView           *serverView
	marketView        *marketViewer
	acctsView         *accountsViewer
	noteJournal       *journal
	noteLog           slog.Logger
	marketLog         slog.Logger
	acctsLog          slog.Logger
	focusColor        = tcell.GetColor("#dedeff")
	blurColor         = tcell.GetColor("grey")
	onColor           = tcell.GetColor("green")
	offColor          = blurColor
	colorBlack        = tcell.GetColor("black")
	notificationCount uint32
)

// For brevity, a commonly used tview callback.
type inputCapture func(event *tcell.EventKey) *tcell.EventKey

// Run the TUI app.
func Run(ctx context.Context) {
	appCtx = ctx
	// Initialize logging to a widget
	appJournal = newJournal("Application Log", handleAppLogKey)
	InitLogging(func(p []byte) {
		appJournal.Write(p)
	})
	// Create the UI and start the app.
	createApp()
	if err := app.SetRoot(screen, true).SetFocus(mainMenu).Run(); err != nil {
		panic(err)
	}
}

// A focuser is satisfied by anything that embeds *tview.Box and implements
// AddFocus and RemoveFocus methods. The two additional methods
type focuser interface {
	tview.Primitive
	SetInputCapture(capture func(event *tcell.EventKey) *tcell.EventKey) *tview.Box
	GetInputCapture() func(event *tcell.EventKey) *tcell.EventKey
	// AddFocus and RemoveFocus enable additional control over of focus
	// navigation.
	AddFocus()
	RemoveFocus()
}

// Screen is the the full screen. It is separated into a permanent menu on the
// left, and a swappable view on the right.
type Screen struct {
	*tview.Flex
	right   focuser
	focused focuser
}

var welcomeMessage = "Welcome to Decred DEX. Use [#838ac7]Up[white] and " +
	"[#838ac7]Down[white] arrows and then press [#838ac7]Enter[white] to select" +
	" a new view. The [#838ac7]Escape[white] key will usually toggle the focus" +
	" between the menu and the currently selected view. When the selected view" +
	" is focused, most navigation can be done with your the [#838ac7]Left" +
	"[white] and [#838ac7]Right[white] arrows, or alternatively [#838ac7]Tab" +
	"[white] and [#838ac7]Shift+Tab[white]. Use [#838ac7]Escape[white] to remove" +
	" focus from a form element."

// createApp creates the Screen and adds the menu and the initial view.
func createApp() {
	clientCore = core.New(appCtx, NewLogger("CORE", nil))
	createWidgets()
	// Create the Screen, which is the top-level layout manager.
	flex := tview.NewFlex().
		AddItem(mainMenu, 0, 15, false)
	screen = &Screen{
		Flex:    flex,
		right:   appJournal,
		focused: mainMenu,
	}
	// Initial view is the application journal.
	setRightBox(appJournal)
	setFocus(mainMenu)
	noteLog.Infof(welcomeMessage)
	// Print a message indicating the network.
	switch {
	case cfg.Simnet:
		log.Infof("DEX network set to simnet")
	case cfg.Testnet:
		log.Infof("DEX network set to testnet")
	default:
		log.Warnf("DEX NETWORK SET TO MAINNET. DEX client software is in " +
			"development and should not be used to perform trades on mainnet.")
	}
	// --rpc flag was set.
	if cfg.RPCOn {
		rpcView.toggle()
	}
	// --web flag was set.
	if cfg.WebOn {
		webView.toggle()
	}
}

// createWidgets creates all of the primitives.
func createWidgets() {
	app = tview.NewApplication()
	acctsView = newAccountsView()
	marketView = newMarketView()
	webView = newServerView("Web", cfg.WebAddr, func(ctx context.Context, addr string, logger slog.Logger) {
		setWebLabelOn(true)
		webserver.Run(ctx, clientCore, addr, logger)
		setWebLabelOn(false)
	})
	rpcView = newServerView("RPC", cfg.RPCAddr, func(ctx context.Context, addr string, logger slog.Logger) {
		setRPCLabelOn(true)
		rpcserver.Run(ctx, clientCore, addr, logger)
		setRPCLabelOn(false)
	})
	noteJournal = newJournal("Notifications", handleNotificationLog)
	noteLog = NewLogger("NOTIFICATION", func(msg []byte) {
		setNotificationCount(int(atomic.AddUint32(&notificationCount, 1)))
		noteJournal.Write(msg)
	})
	mainMenu = newMainMenu()
}

func handleAppLogKey(e *tcell.EventKey) *tcell.EventKey {
	return handleRightBox(e)
}

func handleNotificationLog(e *tcell.EventKey) *tcell.EventKey {
	return handleRightBox(e)
}

func handleRightBox(e *tcell.EventKey) *tcell.EventKey {
	switch e.Key() {
	case tcell.KeyEscape:
		setFocus(mainMenu)
		return nil
	}
	return e
}

// MAIN MENU

// Main menu titles.
const (
	entryAppLog        = "Application Log"
	entryAccounts      = "Accounts & Wallets"
	entryMarkets       = "Markets"
	entryNotifications = "Notifications"
	entryWebServer     = "Web Server"
	entryRPCServer     = "RPC Server"
)

// To modify the main menu text, you have to access the entry by index. These
// need to be set during instantiation.
var (
	noteEntryIdx int
	webEntryIdx  int
	rpcEntryIdx  int
)

func newMainMenu() *chooser {
	c := newChooser("", handleMainMenuKey)
	c.addEntry(entryAppLog, func() { setRightBox(appJournal) }).
		addEntry(entryAccounts, func() { setRightBox(acctsView) }).
		addEntry(entryMarkets, func() { setRightBox(marketView) }).
		addEntry(entryNotifications, func() {
			setRightBox(noteJournal)
		}).
		addEntry(entryWebServer, func() { setRightBox(webView) }).
		addEntry(entryRPCServer, func() { setRightBox(rpcView) }).
		addEntry("Quit", func() {
			app.Stop()
		})
	noteEntryIdx = 3
	webEntryIdx = 4
	rpcEntryIdx = 5
	return c
}

func handleMainMenuKey(e *tcell.EventKey) *tcell.EventKey {
	entry, _ := mainMenu.GetItemText(mainMenu.GetCurrentItem())
	match := strings.HasPrefix
	switch e.Key() {
	case tcell.KeyBacktab, tcell.KeyTab, tcell.KeyEscape, tcell.KeyEnter:
		switch {
		case match(entry, entryAppLog):
			setRightBox(appJournal)
		case match(entry, entryAccounts):
			setRightBox(acctsView)
		case match(entry, entryMarkets):
			setRightBox(marketView)
		case match(entry, entryNotifications):
			atomic.StoreUint32(&notificationCount, 0)
			setNotificationCount(0)
			setRightBox(noteJournal)
		case match(entry, entryWebServer):
			setRightBox(webView)
		case match(entry, entryRPCServer):
			setRightBox(rpcView)
		default:
			setRightBox(screen.right)
		}
		return nil
	}
	return e
}

func setRightBox(box focuser) {
	screen.RemoveItem(screen.right)
	screen.right = box
	screen.AddItem(box, 0, 80, false)
	setFocus(box)
}

func setFocus(wgt focuser) {
	screen.focused.RemoveFocus()
	screen.focused = wgt
	wgt.AddFocus()
}

func setWebLabelOn(on bool) {
	if on {
		mainMenu.SetItemText(webEntryIdx, "Web Server (on)", "")
		return
	}
	mainMenu.SetItemText(webEntryIdx, "Web Server", "")
}

func setRPCLabelOn(on bool) {
	if on {
		mainMenu.SetItemText(rpcEntryIdx, "RPC Server (on)", "")
		return
	}
	mainMenu.SetItemText(rpcEntryIdx, "RPC Server", "")
}

func setNotificationCount(n int) {
	suffix := fmt.Sprintf(" [#fc8c03](%d)[white]", n)
	if n == 0 {
		suffix = ""
	}
	mainMenu.SetItemText(noteEntryIdx, "Notifications"+suffix, "")
}

// CHOOSER

// chooser is an tview List with some default settings.
type chooser struct {
	*tview.List
}

func newChooser(title string, keyFunc inputCapture) *chooser {
	list := tview.NewList()
	list.SetBorder(true).
		SetBorderColor(blurColor).
		SetInputCapture(keyFunc).
		SetTitle(title).
		SetBorderPadding(1, 3, 1, 3)

	return &chooser{
		List: list,
	}
}

func (c *chooser) addEntry(name string, f func()) *chooser {
	c.AddItem(name, "", 0, f)
	return c
}

func (c *chooser) AddFocus() {
	c.SetBorderColor(focusColor)
	app.SetFocus(c)
}

func (c *chooser) RemoveFocus() {
	c.SetBorderColor(blurColor)
}

type simpleButton struct {
	*tview.TextView
}

func newSimpleButton(lbl string, f func()) *simpleButton {
	bttn := tview.NewTextView().
		SetText(lbl).
		SetTextAlign(tview.AlignCenter)
	bttn.SetBorder(true).SetBorderColor(blurColor)
	bttn.SetInputCapture(func(e *tcell.EventKey) *tcell.EventKey {
		switch e.Key() {
		case tcell.KeyEnter:
			f()
		default:
			return e
		}
		return nil
	})
	return &simpleButton{TextView: bttn}
}

func (b *simpleButton) AddFocus() {
	b.SetBorderColor(focusColor)
	app.SetFocus(b)
}

func (b *simpleButton) RemoveFocus() {
	b.SetBorderColor(blurColor)
}

// FOCUS CHAIN

// focusChain enables control over the order of progression of changing focus,
// i.e. what element receives focus next when the user presses tab/backtab.
type focusChain struct {
	parent focuser
	chain  []focuser
	curIdx int
}

func newFocusChain(parent focuser, prims ...focuser) *focusChain {
	c := &focusChain{
		parent: parent,
		chain:  prims,
	}
	// DRAFT NOTE: This is a little sloppy. Since the wrapped handler is being
	// re-assigned with SetInputCapture, this means an element should only be
	// added to a single focus chain for its lifetime. Ideally, we could
	// re-assign the element to a new focus chain if a new element is added or
	// an element is removed.
	for _, prim := range prims {
		ogCapture := prim.GetInputCapture()
		prim.SetInputCapture(func(e *tcell.EventKey) *tcell.EventKey {
			e = c.handleInput(e)
			if e != nil && ogCapture != nil {
				return ogCapture(e)
			}
			return e
		})
	}
	return c
}

// focus must be called from the parent view when it receives focus itself.
func (c *focusChain) focus() {
	setFocus(c.chain[c.curIdx])
}

// handleInput is called before the chain element's input capture callback.
func (c *focusChain) handleInput(e *tcell.EventKey) *tcell.EventKey {
	switch e.Key() {
	case tcell.KeyLeft, tcell.KeyBacktab:
		c.prev()
	case tcell.KeyRight, tcell.KeyTab:
		c.next()
	case tcell.KeyEscape:
		c.parent.RemoveFocus()
		setFocus(mainMenu)
	default:
		return e
	}
	return nil
}

// next moves focus to the next element in the chain.
func (c *focusChain) next() {
	c.chain[c.curIdx].RemoveFocus()
	c.curIdx = (c.curIdx + 1) % len(c.chain)
	setFocus(c.chain[c.curIdx])
}

// prev moves focus to the previous element in the chain.
func (c *focusChain) prev() {
	c.chain[c.curIdx].RemoveFocus()
	chainLen := len(c.chain)
	c.curIdx = (c.curIdx + chainLen - 1) % chainLen
	setFocus(c.chain[c.curIdx])
}

// Flex alignment utilities.

func verticallyCentered(prim tview.Primitive, h int) *tview.Flex {
	flex := horizontallyCentered(prim, h)
	return flex.SetDirection(tview.FlexRow)
}

func horizontallyCentered(prim tview.Primitive, w int) *tview.Flex {
	return tview.NewFlex().
		AddItem(tview.NewBox(), 0, 1, false).
		AddItem(prim, w, 0, false).
		AddItem(tview.NewBox(), 0, 1, false)
}

func fullyCentered(prim tview.Primitive, w, h int) *tview.Flex {
	flex := horizontallyCentered(prim, w)
	return verticallyCentered(flex, h)
}
