import { app } from './registry'
import Doc, { WalletIcons } from './doc'
import BasePage from './basepage'
import { postJSON } from './http'
import { NewWalletForm, WalletConfigForm, UnlockWalletForm, bind as bindForm } from './forms'
import * as ntfn from './notifications'
import State from './state'
import * as intl from './locales'

const bind = Doc.bind
const animationLength = 300
const traitNewAddresser = 1 << 1

export default class WalletsPage extends BasePage {
  constructor (body) {
    super()
    this.body = body
    const page = this.page = Doc.idDescendants(body)

    // Read the document, storing some info about each asset's row.
    const getAction = (row, name) => row.querySelector(`[data-action=${name}]`)
    const rowInfos = this.rowInfos = {}
    const rows = page.walletTable.querySelectorAll('tr')
    let firstRow
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
        rescan: getAction(tr, 'rescan'),
        lock: getAction(tr, 'lock'),
        settings: getAction(tr, 'settings')
      }
    }

    // Prepare templates
    page.marketCard.removeAttribute('id')
    page.marketCard.remove()
    page.oneMarket.removeAttribute('id')
    page.oneMarket.remove()

    // Methods to switch the item displayed on the right side, with a little
    // fade-in animation.
    this.displayed = null // The currently displayed right-side element.
    this.animation = null // Store Promise of currently running animation.

    this.openAsset = null
    this.walletAsset = null
    this.reconfigAsset = null
    this.withdrawAsset = null

    // Bind the new wallet form.
    this.newWalletForm = new NewWalletForm(page.newWalletForm, () => { this.createWalletSuccess() })

    // Bind the wallet reconfig form.
    this.reconfigForm = new WalletConfigForm(page.reconfigInputs, false)

    // Bind the wallet unlock form.
    this.unlockForm = new UnlockWalletForm(page.unlockWalletForm, () => { this.openWalletSuccess() })

    // Bind the withdraw form.
    bindForm(page.withdrawForm, page.submitWithdraw, () => { this.withdraw() })

    // Bind the wallet reconfiguration submission.
    bindForm(page.reconfigForm, page.submitReconfig, () => this.reconfig())

    // Bind the row clicks, which shows the available markets for the asset.
    for (const rowInfo of Object.values(rowInfos)) {
      bind(rowInfo.tr, 'click', () => {
        this.showMarkets(rowInfo.assetID)
      })
    }

    page.rightBox.querySelectorAll('.form-closer').forEach(el => {
      Doc.bind(el, 'click', () => {
        this.showMarkets(this.lastFormAsset)
      })
    })

    this.keyup = e => {
      if (e.key === 'Escape') {
        this.showMarkets(this.lastFormAsset)
      }
    }
    bind(document, 'keyup', this.keyup)

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
      bind(a.rescan, 'click', e => { run(e, this.rescanWallet.bind(this)) })
      bind(a.unlock, 'click', e => { run(e, this.openWallet.bind(this)) })
      bind(a.lock, 'click', async e => { run(e, this.lock.bind(this)) })
      bind(a.settings, 'click', e => { run(e, this.showReconfig.bind(this)) })
    }

    // New deposit address button.
    bind(page.newDepAddrBttn, 'click', async () => { this.newDepositAddress() })

    // Clicking on the available amount on the withdraw form populates the
    // amount field.
    bind(page.withdrawAvail, 'click', () => {
      const asset = this.withdrawAsset
      page.withdrawAmt.value = asset.wallet.balance.available / asset.info.unitinfo.conventional.conversionFactor
    })

    // A link on the wallet reconfiguration form to show/hide the password field.
    bind(page.showChangePW, 'click', () => {
      this.changeWalletPW = !this.changeWalletPW
      this.setPWSettingViz(this.changeWalletPW)
    })

    // Changing the type of wallet.
    bind(page.changeWalletTypeSelect, 'change', () => {
      this.changeWalletType()
    })
    bind(page.showChangeType, 'click', () => {
      if (Doc.isHidden(page.changeWalletType)) {
        Doc.show(page.changeWalletType, page.changeTypeHideIcon)
        Doc.hide(page.changeTypeShowIcon)
        page.changeTypeMsg.textContent = intl.prep(intl.ID_KEEP_WALLET_TYPE)
      } else this.showReconfig(this.reconfigAsset)
    })

    if (!firstRow) return
    this.showMarkets(firstRow.assetID)

    this.notifiers = {
      balance: note => { this.handleBalanceNote(note) },
      walletstate: note => { this.handleWalletStateNote(note) },
      walletconfig: note => { this.handleWalletStateNote(note) }
    }
  }

  /*
   * setPWSettingViz sets the visibility of the password field section.
   */
  setPWSettingViz (visible) {
    if (visible) {
      Doc.hide(this.page.showIcon)
      Doc.show(this.page.hideIcon, this.page.changePW)
      this.page.switchPWMsg.textContent = intl.prep(intl.ID_KEEP_WALLET_PASS)
      return
    }
    Doc.hide(this.page.hideIcon, this.page.changePW)
    Doc.show(this.page.showIcon)
    this.page.switchPWMsg.textContent = intl.prep(intl.ID_NEW_WALLET_PASS)
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
    page.marketsForLogo.src = Doc.logoPath(app().assets[assetID].symbol)
    for (const [host, xc] of Object.entries(app().user.exchanges)) {
      let count = 0
      for (const market of Object.values(xc.markets)) {
        if (market.baseid === assetID || market.quoteid === assetID) count++
      }
      if (count === 0) continue
      const marketBox = page.marketCard.cloneNode(true)
      const tmpl = Doc.parseTemplate(marketBox)
      tmpl.dexTitle.textContent = host
      card.appendChild(marketBox)
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
        bind(mBox, 'click', () => { app().loadPage('markets', pageData) })
        tmpl.markets.appendChild(mBox)
      }
    }
    this.animation = this.showBox(box)
  }

  /* Show the new wallet form. */
  async showNewWallet (assetID) {
    const page = this.page
    const box = page.newWalletForm
    await this.hideBox()
    this.walletAsset = this.lastFormAsset = assetID
    this.newWalletForm.setAsset(assetID)
    this.animation = this.showBox(box)
    await this.newWalletForm.loadDefaults()
  }

  async rescanWallet (assetID) {
    const loaded = app().loading(this.body)
    const res = await postJSON('/api/rescanwallet', {
      assetID: assetID,
      force: false // TODO input arg
    })
    loaded()
    app().checkResponse(res)
  }

  /* Show the open wallet form if the password is not cached, and otherwise
   * attempt to open the wallet.
   */
  async openWallet (assetID) {
    if (!State.passwordIsCached()) {
      this.showOpen(assetID)
    } else {
      this.openAsset = assetID
      const open = {
        assetID: parseInt(assetID)
      }
      const res = await postJSON('/api/openwallet', open)
      if (app().checkResponse(res)) {
        this.openWalletSuccess.bind(this)()
      } else {
        this.showOpen(assetID, `Error opening wallet: ${res.msg}`)
      }
    }
  }

  /* Show the form used to unlock a wallet. */
  async showOpen (assetID, errorMsg) {
    const page = this.page
    this.openAsset = this.lastFormAsset = assetID
    await this.hideBox()
    this.unlockForm.setAsset(app().assets[assetID])
    if (errorMsg) this.unlockForm.showErrorOnly(errorMsg)
    this.animation = this.showBox(page.unlockWalletForm, page.walletPass)
  }

  /* Show the form used to change wallet configuration settings. */
  async showReconfig (assetID) {
    const page = this.page
    Doc.hide(page.changeWalletType, page.changeTypeHideIcon, page.reconfigErr, page.showChangeType, page.changeTypeHideIcon)
    Doc.hide(page.reconfigErr)
    // Hide update password section by default
    this.reconfigAsset = this.lastFormAsset = assetID
    this.changeWalletPW = false
    this.setPWSettingViz(this.changeWalletPW)
    const asset = app().assets[assetID]

    const currentDef = app().currentWalletDefinition(assetID)

    if (asset.info.availablewallets.length > 1) {
      Doc.empty(page.changeWalletTypeSelect)
      Doc.show(page.showChangeType, page.changeTypeShowIcon)
      page.changeTypeMsg.textContent = intl.prep(intl.ID_CHANGE_WALLET_TYPE)
      for (const wDef of asset.info.availablewallets) {
        const option = document.createElement('option')
        if (wDef.type === currentDef.type) option.selected = '1'
        option.value = option.textContent = wDef.type
        page.changeWalletTypeSelect.appendChild(option)
      }
    } else {
      Doc.hide(page.showChangeType)
    }

    page.recfgAssetLogo.src = Doc.logoPath(asset.symbol)
    page.recfgAssetName.textContent = asset.info.name
    await this.hideBox()
    this.animation = this.showBox(page.reconfigForm)
    const loaded = app().loading(page.reconfigForm)
    const res = await postJSON('/api/walletsettings', {
      assetID: assetID
    })
    loaded()
    if (!app().checkResponse(res, true)) {
      page.reconfigErr.textContent = res.msg
      Doc.show(page.reconfigErr)
      return
    }
    this.reconfigForm.update(currentDef.configopts || [])
    this.reconfigForm.setConfig(res.map)
    this.updateDisplayedReconfigFields(currentDef)
  }

  changeWalletType () {
    const page = this.page
    const walletType = page.changeWalletTypeSelect.value
    const walletDef = app().walletDefinition(this.reconfigAsset, walletType)
    this.reconfigForm.update(walletDef.configopts || [])
    this.updateDisplayedReconfigFields(walletDef)
  }

  updateDisplayedReconfigFields (walletDef) {
    if (walletDef.seeded) {
      Doc.hide(this.page.showChangePW)
      this.changeWalletPW = false
      this.setPWSettingViz(false)
    } else Doc.show(this.page.showChangePW)
  }

  /* Display a deposit address. */
  async showDeposit (assetID) {
    const page = this.page
    Doc.hide(page.depositErr)
    const box = page.deposit
    const asset = app().assets[assetID]
    page.depositLogo.src = Doc.logoPath(asset.symbol)
    const wallet = app().walletMap[assetID]
    this.depositAsset = this.lastFormAsset = assetID
    if (!wallet) {
      app().notify(ntfn.make('Cannot retrieve deposit address.', `No wallet found for ${asset.info.name}`, ntfn.ERROR)) // TODO: translate
      return
    }
    await this.hideBox()
    page.depositName.textContent = asset.info.name
    page.depositAddress.textContent = wallet.address
    if ((wallet.traits & traitNewAddresser) !== 0) Doc.show(page.newDepAddrBttn)
    else Doc.hide(page.newDepAddrBttn)
    this.animation = this.showBox(box)
  }

  /* Fetch a new address from the wallet. */
  async newDepositAddress () {
    const page = this.page
    Doc.hide(page.depositErr)
    const loaded = app().loading(page.deposit)
    const res = await postJSON('/api/depositaddress', {
      assetID: this.depositAsset
    })
    loaded()
    if (!app().checkResponse(res, true)) {
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
    const asset = this.withdrawAsset = app().assets[assetID]
    const wallet = app().walletMap[assetID]
    if (!wallet) {
      app().notify(ntfn.make('Cannot withdraw.', `No wallet found for ${asset.info.name}`, ntfn.ERROR))
    }
    await this.hideBox()
    page.withdrawAddr.value = ''
    page.withdrawAmt.value = ''
    page.withdrawPW.value = ''

    page.withdrawAvail.textContent = Doc.formatFullPrecision(wallet.balance.available, asset.info.unitinfo)
    page.withdrawLogo.src = Doc.logoPath(asset.symbol)
    page.withdrawName.textContent = asset.info.name
    // page.withdrawFee.textContent = wallet.feerate
    // page.withdrawUnit.textContent = wallet.units
    box.dataset.assetID = this.lastFormAsset = assetID
    this.animation = this.showBox(box, page.walletPass)
  }

  /* doConnect connects to a wallet via the connectwallet API route. */
  async doConnect (assetID) {
    const loaded = app().loading(this.body)
    const res = await postJSON('/api/connectwallet', {
      assetID: assetID
    })
    loaded()
    if (!app().checkResponse(res)) return
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
    await app().fetchUser()
    if (app().walletMap[rowInfo.assetID].encrypted) {
      Doc.show(a.lock)
    }
  }

  /* openWalletSuccess is the success callback for wallet unlocking. */
  async openWalletSuccess () {
    const rowInfo = this.rowInfos[this.openAsset]
    const a = rowInfo.actions
    Doc.show(a.withdraw, a.deposit)
    Doc.hide(a.unlock, a.connect)
    if (app().walletMap[rowInfo.assetID].encrypted) {
      Doc.show(a.lock)
    }
    this.showMarkets(this.openAsset)
  }

  /* withdraw submits the withdrawal form to the API. */
  async withdraw () {
    const page = this.page
    Doc.hide(page.withdrawErr)
    const assetID = parseInt(page.withdrawForm.dataset.assetID)
    const conversionFactor = app().unitInfo(assetID).conventional.conversionFactor
    const open = {
      assetID: assetID,
      address: page.withdrawAddr.value,
      value: parseInt(Math.round(page.withdrawAmt.value * conversionFactor)),
      pw: page.withdrawPW.value
    }
    const loaded = app().loading(page.withdrawForm)
    const res = await postJSON('/api/withdraw', open)
    loaded()
    if (!app().checkResponse(res, true)) {
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
    if (!page.appPW.value && !State.passwordIsCached()) {
      page.reconfigErr.textContent = intl.prep(intl.ID_NO_APP_PASS_ERROR_MSG)
      Doc.show(page.reconfigErr)
      return
    }

    let walletType = app().currentWalletDefinition(this.reconfigAsset).type
    if (!Doc.isHidden(page.changeWalletType)) {
      walletType = page.changeWalletTypeSelect.value
    }

    const loaded = app().loading(page.reconfigForm)
    const req = {
      assetID: this.reconfigAsset,
      config: this.reconfigForm.map(),
      appPW: page.appPW.value,
      walletType: walletType
    }
    if (this.changeWalletPW) req.newWalletPW = page.newPW.value
    const res = await postJSON('/api/reconfigurewallet', req)
    page.appPW.value = ''
    page.newPW.value = ''
    loaded()
    if (!app().checkResponse(res, true)) {
      page.reconfigErr.textContent = res.msg
      Doc.show(page.reconfigErr)
      return
    }
    this.showMarkets(this.reconfigAsset)
  }

  /* lock instructs the API to lock the wallet. */
  async lock (assetID, asset) {
    const page = this.page
    const loaded = app().loading(page.newWalletForm)
    const res = await postJSON('/api/closewallet', { assetID: assetID })
    loaded()
    if (!app().checkResponse(res)) return
    const a = asset.actions
    Doc.hide(a.withdraw, a.lock, a.deposit)
    Doc.show(a.unlock)
  }

  /* handleBalance handles notifications updating a wallet's balance. */
  handleBalanceNote (note) {
    const td = this.page.walletTable.querySelector(`[data-balance-target="${note.assetID}"]`)
    td.textContent = Doc.formatFullPrecision(note.balance.available, app().unitInfo(note.assetID))
  }

  /*
   * handleWalletStateNote is a handler for both the 'walletstate' and
   * 'walletconfig' notifications.
   */
  handleWalletStateNote (note) {
    this.rowInfos[note.wallet.assetID].stateIcons.readWallet(note.wallet)
  }

  /*
   * unload is called by the Application when the user navigates away from
   * the /wallets page.
   */
  unload () {
    Doc.unbind(document, 'keyup', this.keyup)
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
