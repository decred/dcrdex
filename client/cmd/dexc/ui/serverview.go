// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package ui

import (
	"context"
	"strings"

	"decred.org/dcrdex/dex"
	"github.com/gdamore/tcell"
	"github.com/rivo/tview"
)

// serverView is a view with a simple for for starting a server and a journal
// to view its log output.
type serverView struct {
	*tview.Flex
	form   *tview.Form
	toggle func()
}

// newServerView is a constructor for the server view. The server view's only
// job is to run the supplied runFunc and display its log output.
func newServerView(tag, addr string, runFunc func(context.Context, string, dex.Logger)) *serverView {
	// A journal to display log output from the server.
	var serverJournal *journal
	serverJournal = newJournal(tag+" Journal", func(e *tcell.EventKey) *tcell.EventKey {
		switch e.Key() {
		case tcell.KeyEscape:
			webView.RemoveFocus()
			serverJournal.SetBorderColor(blurColor)
			setFocus(mainMenu)
		}
		return e
	})

	// Get a logger for the server.
	logTag := strings.ToUpper(tag) + "SVR"
	lm, err := CustomLogMaker(func(p []byte) {
		serverJournal.Write(p)
	})
	if err != nil {
		log.Errorf("error creating " + logTag + " logger")
	}
	serverLogger := lm.Logger(logTag)

	onMsg := " " + tag + " server is on"
	offMsg := " " + tag + " server is off"

	// The indicator is just a small box that changes color when the server is on.
	indicator := tview.NewBox().
		SetBackgroundColor(offColor)
	lbl := tview.NewTextView().SetText(offMsg)
	header := tview.NewFlex().
		AddItem(verticallyCentered(indicator, 1), 2, 0, false).
		AddItem(lbl, 0, 1, false)

	// The arguments necessary to add the address input to the form.
	addrInput := func() (string, string, int, func(string, rune) bool, func(string)) {
		return "address", addr, 0, tview.InputFieldInteger, func(v string) {
			addr = v
		}
	}

	// Crate a method to toggle the state of the server. Will be available as
	// (*serverView).toggle.
	var form *tview.Form
	var serverToggle func()
	var ctx context.Context
	var kill func()
	serverToggle = func() {
		ctx, kill = context.WithCancel(appCtx)
		form.Clear(true)
		form.AddButton("stop", func() {
			kill()
		})
		setFocus(serverJournal)
		indicator.SetBackgroundColor(onColor)
		lbl.SetText(onMsg)
		go func() {
			runFunc(ctx, addr, serverLogger)
			app.QueueUpdateDraw(func() {
				indicator.SetBackgroundColor(offColor)
				form.ClearButtons()
				form.AddInputField(addrInput())
				form.AddButton("start", serverToggle)
				setFocus(serverJournal)
				lbl.SetText(offMsg)
			})
		}()
	}

	// The form to accept the address and start the server, and then present a
	// button to stop the server.
	form = tview.NewForm().
		AddInputField(addrInput()).
		AddButton("start", serverToggle).
		SetButtonsAlign(tview.AlignRight).
		SetFieldBackgroundColor(metalBlue).
		SetButtonBackgroundColor(metalBlue)
	form.SetBorderColor(blurColor).SetBorder(true)
	form.SetCancelFunc(func() {
		form.SetBorderColor(blurColor)
		serverJournal.AddFocus()
	})

	// formBox adds a header and centers the form.
	formBox := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(header, 2, 0, false).
		AddItem(form, 0, 1, false)

	wgt := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(fullyCentered(formBox, 40, 10), 0, 1, false).
		AddItem(serverJournal, 0, 2, false)
	wgt.SetBorder(true)
	return &serverView{
		Flex:   wgt,
		form:   form,
		toggle: serverToggle,
	}
}

func (w *serverView) AddFocus() {
	w.SetBorderColor(focusColor)
	w.form.SetBorderColor(focusColor)
	w.form.SetFocus(1)
	app.SetFocus(w.form)
}

func (w *serverView) RemoveFocus() {
	w.SetBorderColor(blurColor)
}
