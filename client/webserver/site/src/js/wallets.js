import Doc, { WalletIcons } from './doc'
import BasePage from './basepage'
import { postJSON } from './http'
import { NewWalletForm, WalletConfigForm, bindOpenWallet, bind as bindForm } from './forms'
import * as ntfn from './notifications'

const bind = Doc.bind
const animationLength = 300
var app

export default class WalletsPage extends BasePage {
  constructor (application, body) {
    super()
    app = application
    this.body = body
    const page = this.page = Doc.parsePage(body, [
      'rightBox',
      // Table Rows
      'assetArrow', 'balanceArrow', 'statusArrow', 'walletTable',
      // Available markets
      'markets', 'dexTitle', 'marketsBox', 'oneMarket', 'marketsFor',
      'marketsCard',
      // New wallet, unlock wallet, wallet settings
      'walletForm', 'openForm',
      // Wallet configuration
      'walletReconfig', 'recfgAssetLogo', 'recfgAssetName', 'reconfigInputs',
      'submitReconfig', 'reconfigErr', 'reconfigPW', 'changePW',
      // Wallet password change
      'walletRepw', 'repwAssetLogo', 'repwAssetName', 'repwNewPw', 'repwAppPw',
      'submitRepw', 'repwErr',
      // Deposit
      'deposit', 'depositName', 'depositAddress', 'newDepAddrBttn',
      'depositErr',
      // Withdraw
      'withdrawForm', 'withdrawLogo', 'withdrawName', 'withdrawAddr',
      'withdrawAmt', 'withdrawAvail', 'submitWithdraw', // 'withdrawFee',
      'withdrawUnit', 'withdrawPW', 'withdrawErr'
    ])

    // Read the document, storing some info about each asset's row.
    const getAction = (row, name) => row.querySelector(`[data-action=${name}]`)
    const rowInfos = this.rowInfos = {}
    const rows = page.walletTable.querySelectorAll('tr')
    var firstRow
    for (const tr of rows) {
      const assetID = parseInt(tr.dataset.assetID)
      const rowInfo = rowInfos[assetID] = {}
      if (!firstRow) firstRow = rowInfo
      rowInfo.assetID = assetID
      rowInfo.tr = tr
      rowInfo.symbol = tr.dataset.symbol
      rowInfo.name = tr.dataset.name
      rowInfo.stateIcons = new WalletIcons(tr)
      rowInfo.actions = {
        connect: getAction(tr, 'connect'),
        unlock: getAction(tr, 'unlock'),
        withdraw: getAction(tr, 'withdraw'),
        deposit: getAction(tr, 'deposit'),
        create: getAction(tr, 'create'),
        lock: getAction(tr, 'lock'),
        settings: getAction(tr, 'settings')
      }
    }

    // Prepare templates
    page.dexTitle.removeAttribute('id')
    page.dexTitle.remove()
    page.oneMarket.removeAttribute('id')
    page.oneMarket.remove()
    page.markets.removeAttribute('id')
    page.markets.remove()

    // Methods to switch the item displayed on the right side, with a little
    // fade-in animation.
    this.displayed = null // The currently displayed right-side element.
    this.animation = null // Store Promise of currently running animation.

    this.openAsset = null
    this.walletAsset = null
    this.reconfigAsset = null

    // Bind the new wallet form.
    this.walletForm = new NewWalletForm(app, page.walletForm, () => { this.createWalletSuccess() })

    // Bind the wallet reconfig form.
    this.walletReconfig = new WalletConfigForm(app, page.reconfigInputs, false)

    // Bind the wallet unlock form.
    bindOpenWallet(app, page.openForm, () => { this.openWalletSuccess() })

    // Bind the withdraw form.
    bindForm(page.withdrawForm, page.submitWithdraw, () => { this.withdraw() })

    // Bind the wallet reconfiguration submission.
    bindForm(page.walletReconfig, page.submitReconfig, () => this.reconfig())

    // Bind the row clicks, which shows the available markets for the asset.
    for (const rowInfo of Object.values(rowInfos)) {
      bind(rowInfo.tr, 'click', () => {
        this.showMarkets(rowInfo.assetID)
      })
    }

    // Bind buttons
    for (const [k, asset] of Object.entries(rowInfos)) {
      const assetID = parseInt(k) // keys are string asset ID.
      const a = asset.actions
      const run = (e, f) => {
        e.stopPropagation()
        f(assetID, asset)
      }
      bind(a.connect, 'click', e => { run(e, this.doConnect.bind(this)) })
      bind(a.withdraw, 'click', e => { run(e, this.showWithdraw.bind(this)) })
      bind(a.deposit, 'click', e => { run(e, this.showDeposit.bind(this)) })
      bind(a.create, 'click', e => { run(e, this.showNewWallet.bind(this)) })
      bind(a.unlock, 'click', e => { run(e, this.showOpen.bind(this)) })
      bind(a.lock, 'click', async e => { run(e, this.lock.bind(this)) })
      bind(a.settings, 'click', e => { run(e, this.showReconfig.bind(this)) })
    }

    // New deposit address button.
    bind(page.newDepAddrBttn, 'click', async () => { this.newDepositAddress() })

    // Clicking on the available amount on the withdraw form populates the
    // amount field.
    bind(page.withdrawAvail, 'click', () => {
      page.withdrawAmt.value = page.withdrawAvail.textContent
    })

    // A link on the wallet reconfiguration form to show the password change
    // form.
    bind(page.changePW, 'click', () => { this.showChangePW() })

    // Submit the password change form.
    bindForm(page.walletRepw, page.submitRepw, () => { this.submitChangePW() })

    if (!firstRow) return
    this.showMarkets(firstRow.assetID)

    this.notifiers = {
      balance: note => { this.handleBalanceNote(note) },
      walletstate: note => { this.handleWalletStateNote(note) }
    }
  }

  /*
   * hideBox hides the displayed box after waiting for the currently running
   * animation to complete.
   */
  async hideBox () {
    if (this.animation) await this.animation
    if (!this.displayed) return
    Doc.hide(this.displayed)
  }

  /*
   * showBox shows the box with a fade-in animation.
   */
  async showBox (box, focuser) {
    box.style.opacity = '0'
    Doc.show(box)
    if (focuser) focuser.focus()
    await Doc.animate(animationLength, progress => {
      box.style.opacity = `${progress}`
    }, 'easeOut')
    box.style.opacity = '1'
    this.displayed = box
  }

  /*
   * Show the markets box, which lists the markets available for a selected
   * asset.
   */
  async showMarkets (assetID) {
    const page = this.page
    const box = page.marketsBox
    const card = page.marketsCard
    const rowInfo = this.rowInfos[assetID]
    await this.hideBox()
    Doc.empty(card)
    page.marketsFor.textContent = rowInfo.name
    for (const [host, xc] of Object.entries(app.user.exchanges)) {
      let count = 0
      for (const market of Object.values(xc.markets)) {
        if (market.baseid === assetID || market.quoteid === assetID) count++
      }
      if (count === 0) continue
      const header = page.dexTitle.cloneNode(true)
      header.textContent = host
      card.appendChild(header)
      const marketsBox = page.markets.cloneNode(true)
      card.appendChild(marketsBox)
      for (const market of Object.values(xc.markets)) {
        // Only show markets where this is the base or quote asset.
        if (market.baseid !== assetID && market.quoteid !== assetID) continue
        const mBox = page.oneMarket.cloneNode(true)
        mBox.querySelector('span').textContent = prettyMarketName(market)
        let counterSymbol = market.basesymbol
        if (market.baseid === assetID) counterSymbol = market.quotesymbol
        mBox.querySelector('img').src = Doc.logoPath(counterSymbol)
        // Bind the click to a load of the markets page.
        const pageData = { host: host, base: market.baseid, quote: market.quoteid }
        bind(mBox, 'click', () => { app.loadPage('markets', pageData) })
        marketsBox.appendChild(mBox)
      }
    }
    this.animation = this.showBox(box)
  }

  /* Show the new wallet form. */
  async showNewWallet (assetID) {
    const page = this.page
    const box = page.walletForm
    const asset = app.assets[assetID]
    await this.hideBox()
    this.walletAsset = assetID
    this.walletForm.setAsset(asset)
    this.animation = this.showBox(box)
    await this.walletForm.loadDefaults()
  }

  /* Show the form used to unlock a wallet. */
  async showOpen (assetID) {
    const page = this.page
    this.openAsset = assetID
    await this.hideBox()
    page.openForm.setAsset(app.assets[assetID])
    this.animation = this.showBox(page.openForm, page.walletPass)
  }

  /* Show the form used to change wallet configuration settings. */
  async showReconfig (assetID) {
    const page = this.page
    Doc.hide(page.reconfigErr)
    const asset = app.assets[assetID]
    this.walletReconfig.update(asset.info)
    page.recfgAssetLogo.src = Doc.logoPath(asset.symbol)
    page.recfgAssetName.textContent = asset.info.name
    this.reconfigAsset = assetID
    await this.hideBox()
    this.animation = this.showBox(page.walletReconfig)
    app.loading(page.walletReconfig)
    var res = await postJSON('/api/walletsettings', {
      assetID: assetID
    })
    app.loaded()
    if (!app.checkResponse(res, true)) {
      page.reconfigErr.textContent = res.msg
      Doc.show(page.reconfigErr)
      return
    }
    this.walletReconfig.setConfig(res.map)
  }

  /* showChangePW shows the form to change the wallet password. */
  async showChangePW () {
    const page = this.page
    Doc.hide(page.repwErr)
    const assetID = this.reconfigAsset
    const asset = app.assets[assetID]
    page.repwAssetLogo.src = Doc.logoPath(asset.symbol)
    page.repwAssetName.textContent = asset.info.name
    await this.hideBox()
    this.animation = this.showBox(page.walletRepw)
  }

  /*
   * submitChangePW is called when the wallet password change form is submitted.
   */
  async submitChangePW () {
    const page = this.page
    Doc.hide(page.repwErr)
    if (!page.repwAppPw.value) {
      page.repwErr.textContent = 'app password cannot be empty'
      Doc.show(page.repwErr)
      return
    }
    app.loading(page.walletRepw)
    var res = await postJSON('/api/setwalletpass', {
      assetID: this.reconfigAsset,
      newPW: page.repwNewPw.value,
      appPW: page.repwAppPw.value
    })
    page.repwNewPw.value = ''
    page.repwAppPw.value = ''
    app.loaded()
    if (!app.checkResponse(res, true)) {
      page.repwErr.textContent = res.msg
      Doc.show(page.repwErr)
      return
    }
    this.showMarkets(this.reconfigAsset)
  }

  /* Display a deposit address. */
  async showDeposit (assetID) {
    const page = this.page
    Doc.hide(page.depositErr)
    const box = page.deposit
    const asset = app.assets[assetID]
    const wallet = app.walletMap[assetID]
    this.depositAsset = assetID
    if (!wallet) {
      app.notify(ntfn.make(`No wallet found for ${asset.info.name}`, 'Cannot retrieve deposit address.', ntfn.ERROR))
      return
    }
    await this.hideBox()
    page.depositName.textContent = asset.info.name
    page.depositAddress.textContent = wallet.address
    this.animation = this.showBox(box)
  }

  /* Fetch a new address from the wallet. */
  async newDepositAddress () {
    const page = this.page
    Doc.hide(page.depositErr)
    app.loading(page.deposit)
    const res = await postJSON('/api/depositaddress', {
      assetID: this.depositAsset
    })
    app.loaded()
    if (!app.checkResponse(res, true)) {
      page.depositErr.textContent = res.msg
      Doc.show(page.depositErr)
      return
    }
    page.depositAddress.textContent = res.address
  }

  /* Show the form to withdraw funds. */
  async showWithdraw (assetID) {
    const page = this.page
    const box = page.withdrawForm
    const asset = app.assets[assetID]
    const wallet = app.walletMap[assetID]
    if (!wallet) {
      app.notify(ntfn.make(`No wallet found for ${asset.info.name}`, 'Cannot withdraw.', ntfn.ERROR))
    }
    await this.hideBox()
    page.withdrawAddr.value = ''
    page.withdrawAmt.value = ''
    page.withdrawAvail.textContent = (wallet.balance.available / 1e8).toFixed(8)
    page.withdrawLogo.src = Doc.logoPath(asset.symbol)
    page.withdrawName.textContent = asset.info.name
    // page.withdrawFee.textContent = wallet.feerate
    // page.withdrawUnit.textContent = wallet.units
    box.dataset.assetID = assetID
    this.animation = this.showBox(box, page.walletPass)
  }

  /* doConnect connects to a wallet via the connectwallet API route. */
  async doConnect (assetID) {
    app.loading(this.body)
    var res = await postJSON('/api/connectwallet', {
      assetID: assetID
    })
    app.loaded()
    if (!app.checkResponse(res)) return
    const rowInfo = this.rowInfos[assetID]
    Doc.hide(rowInfo.actions.connect)
  }

  /* createWalletSuccess is the success callback for wallet creation. */
  async createWalletSuccess () {
    const rowInfo = this.rowInfos[this.walletAsset]
    this.showMarkets(rowInfo.assetID)
    const a = rowInfo.actions
    Doc.hide(a.create)
    Doc.show(a.withdraw, a.deposit, a.settings)
    await app.fetchUser()
    if (app.walletMap[rowInfo.assetID].encrypted) {
      Doc.show(a.lock)
    }
  }

  /* openWalletSuccess is the success callback for wallet unlocking. */
  async openWalletSuccess () {
    const rowInfo = this.rowInfos[this.openAsset]
    const a = rowInfo.actions
    Doc.show(a.withdraw, a.deposit)
    Doc.hide(a.unlock, a.connect)
    if (app.walletMap[rowInfo.assetID].encrypted) {
      Doc.show(a.lock)
    }
    this.showMarkets(this.openAsset)
  }

  /* withdraw submits the withdrawal form to the API. */
  async withdraw () {
    const page = this.page
    Doc.hide(page.withdrawErr)
    const assetID = parseInt(page.withdrawForm.dataset.assetID)
    const open = {
      assetID: assetID,
      address: page.withdrawAddr.value,
      value: parseInt(Math.round(page.withdrawAmt.value * 1e8)),
      pw: page.withdrawPW.value
    }
    app.loading(page.withdrawForm)
    var res = await postJSON('/api/withdraw', open)
    app.loaded()
    if (!app.checkResponse(res, true)) {
      page.withdrawErr.textContent = res.msg
      Doc.show(page.withdrawErr)
      return
    }
    this.showMarkets(assetID)
  }

  /* update wallet configuration */
  async reconfig () {
    const page = this.page
    Doc.hide(page.reconfigErr)
    if (!page.reconfigPW.value) {
      page.reconfigErr.textContent = 'app password cannot be empty'
      Doc.show(page.reconfigErr)
      return
    }
    app.loading(page.walletReconfig)
    var res = await postJSON('/api/reconfigurewallet', {
      assetID: this.reconfigAsset,
      config: this.walletReconfig.map(),
      pw: page.reconfigPW.value
    })
    page.reconfigPW.value = ''
    app.loaded()
    if (!app.checkResponse(res, true)) {
      page.reconfigErr.textContent = res.msg
      Doc.show(page.reconfigErr)
      return
    }
    this.showMarkets(this.reconfigAsset)
  }

  /* lock instructs the API to lock the wallet. */
  async lock (assetID, asset) {
    const page = this.page
    app.loading(page.walletForm)
    var res = await postJSON('/api/closewallet', { assetID: assetID })
    app.loaded()
    if (!app.checkResponse(res)) return
    const a = asset.actions
    Doc.hide(a.withdraw, a.lock, a.deposit)
    Doc.show(a.unlock)
  }

  /* handleBalance handles notifications updating a wallet's balance. */
  handleBalanceNote (note) {
    const td = this.page.walletTable.querySelector(`[data-balance-target="${note.assetID}"]`)
    td.textContent = (note.balance.available / 1e8).toFixed(8)
  }

  handleWalletStateNote (note) {
    this.rowInfos[note.assetID].stateIcons.readWallet(note)
  }
}

/*
 * Given a market object as created with makeMarket, prettyMarketName will
 * create a string ABC-XYZ, where ABC and XYZ are the upper-case ticker symbols
 * for the base and quote assets respectively.
 */
function prettyMarketName (market) {
  return `${market.basesymbol.toUpperCase()}-${market.quotesymbol.toUpperCase()}`
}
