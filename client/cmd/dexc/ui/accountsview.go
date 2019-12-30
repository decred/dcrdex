// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package ui

import (
	"github.com/gdamore/tcell"
	"github.com/rivo/tview"
)

var dummyFormBox = tview.NewBox()

// accountsViewer is a view for manipulating DEX account and wallet settings.
type accountsViewer struct {
	*tview.Flex
	chain *focusChain
	form  *tview.Grid
}

// newAccountsView is the constructor for an accountsViewer.
func newAccountsView() *accountsViewer {
	// A journal for logging account-related messages.
	acctsJournal := newJournal("Accounts Journal", nil)
	// acctsLog is global.
	acctsLog = NewLogger("ACCTS", acctsJournal.Write)
	formBox := tview.NewGrid()
	formBox.SetBackgroundColor(colorBlack)
	var acctForm *tview.Form

	// The list of available DEX accounts, and a button to display a form to add
	// a new DEX account.
	acctsList := newChooser("Accounts", nil)
	newAcctBttn := newSimpleButton("Add New DEX Account", func() {
		formBox.Clear()
		formBox.AddItem(acctForm, 0, 0, 1, 1, 0, 0, false)
		acctForm.SetBorderColor(focusColor)
		app.SetFocus(acctForm)
	})
	acctsColumn := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(acctsList, 0, 1, false).
		AddItem(newAcctBttn, 3, 0, false)

	// The list of exchange wallets registered, and a button to dipslay a form
	// to add a new wallet.
	walletsList := newChooser("Wallets", nil)
	newWalletBttn := newSimpleButton("Add Wallet", func() {
		acctsLog.Errorf("cannot add a wallet without an account")
	})
	walletsColumn := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(walletsList, 0, 1, false).
		AddItem(newWalletBttn, 3, 0, false)

	// The third column is the main content area (formBox), and a log display at
	// the bottom
	thirdColumn := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(fullyCentered(formBox, 60, 17), 0, 3, false).
		AddItem(acctsJournal, 0, 1, false)

	// The main view.
	wgt := tview.NewFlex().
		AddItem(acctsColumn, 30, 0, false).
		AddItem(walletsColumn, 30, 0, false).
		AddItem(thirdColumn, 0, 1, false)

	// A form to add a DEX account.
	//
	// DRAFT NOTE: I think we need more control over forms and should implement
	// them using tview.Grid, but this is a start. A major downside is that
	// tview.Form hijacks tab and arrow keys, making for some unintuitive
	// navigation behavior.
	var acctURL, dcrAcctName, dcrwAddr, dcrwPW, dcrwRPCUser, dcrwRPCPass string
	acctForm = tview.NewForm().
		AddInputField("DEX URL", "", 0, nil, func(url string) {
			acctURL = url
		}).
		AddInputField("Account Name", "", 0, nil, func(name string) {
			dcrAcctName = name
		}).
		AddInputField("dcrwallet RPC Address", "", 0, nil, func(name string) {
			dcrwAddr = name
		}).
		AddInputField("RPC Username", "", 0, nil, func(name string) {
			dcrwRPCUser = name
		}).
		AddPasswordField("RPC Password", "", 0, 0, func(name string) {
			dcrwRPCPass = name
		}).
		AddPasswordField("Wallet Password", "", 0, 0, func(pw string) {
			dcrwPW = pw
		}).
		AddButton("register", func() {
			// Obviously the password won't be echoed. Just this way for
			// demonstration.
			acctsLog.Infof("registering acct %s with password %s and wallet node %s for DEX %s, using RPC username %s and RPC password %s",
				dcrAcctName, dcrwPW, dcrwAddr, acctURL, dcrwRPCUser, dcrwRPCPass)
		}).
		SetButtonsAlign(tview.AlignRight).
		SetFieldBackgroundColor(tcell.GetColor("#072938")).
		SetButtonBackgroundColor(tcell.GetColor("#0d4254"))
	acctForm.SetCancelFunc(func() {
		acctForm.SetBorderColor(blurColor)
		acctsView.setForm(dummyFormBox)
		setFocus(newAcctBttn)
	}).SetBorder(true).SetBorderColor(blurColor)
	acctForm.SetTitle("DEX Registration")

	av := &accountsViewer{
		Flex: wgt,
		form: formBox,
	}
	av.chain = newFocusChain(av, acctsList, newAcctBttn, walletsList, newWalletBttn, acctsJournal)
	return av
}

// AddFocus is part of the focuser interface. Since the accountsViewer supports
// sub-focus, this method simply passes focus to the focus chain and sets the
// view's border color.
func (v *accountsViewer) AddFocus() {
	// Pass control to the focusChain, but keep the border color on the view.
	v.chain.focus()
	v.SetBorderColor(focusColor)
}

// RemoveFocus is part of the focuser interface.
func (v *accountsViewer) RemoveFocus() {
	v.SetBorderColor(blurColor)
}

// Set the currently displayed form.
func (v *accountsViewer) setForm(form tview.Primitive) {
	v.form.Clear()
	v.form.AddItem(form, 0, 0, 1, 1, 0, 0, false)
}
