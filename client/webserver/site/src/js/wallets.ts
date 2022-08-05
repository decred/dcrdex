import Doc, { WalletIcons } from './doc'
import BasePage from './basepage'
import { postJSON } from './http'
import { NewWalletForm, WalletConfigForm, UnlockWalletForm, bind as bindForm } from './forms'
import * as ntfn from './notifications'
import State from './state'
import * as intl from './locales'
import {
  app,
  PageElement,
  SupportedAsset,
  WalletDefinition,
  BalanceNote,
  WalletStateNote,
  Market,
  RateNote,
  WalletState
} from './registry'

const bind = Doc.bind
const animationLength = 300
const traitNewAddresser = 1 << 1
const traitLogFiler = 1 << 2
const traitRecoverer = 1 << 5
const traitWithdrawer = 1 << 6
const traitRestorer = 1 << 8

const activeOrdersErrCode = 35

interface Actions {
  connect: HTMLElement
  unlock: HTMLElement
  send: HTMLElement
  deposit: HTMLElement
  create: HTMLElement
  rescan: HTMLElement
  lock: HTMLElement
  settings: HTMLElement
}

interface RowInfo {
  assetID: number
  tr: HTMLElement
  symbol: string
  name: string
  stateIcons: WalletIcons
  actions: Actions
}

interface ReconfigRequest {
  assetID: number
  walletType: string
  config: Record<string, string>
  newWalletPW?: string
  appPW: string
}

interface RescanRecoveryRequest {
  assetID: number
  appPW?: string
  force?: boolean
}

interface WalletRestoration {
  target: string
  seed: string
  seedName: string
  instructions: string
}

export default class WalletsPage extends BasePage {
  body: HTMLElement
  page: Record<string, PageElement>
  rowInfos: Record<string, RowInfo>
  sendAsset: SupportedAsset
  newWalletForm: NewWalletForm
  reconfigForm: WalletConfigForm
  unlockForm: UnlockWalletForm
  lastFormAsset: number
  keyup: (e: KeyboardEvent) => void
  changeWalletPW: boolean
  depositAsset: number
  // Methods to switch the item displayed on the right side, with a little
  // fade-in animation.
  displayed: HTMLElement
  animation: Promise<void>
  openAsset: number
  walletAsset: number
  reconfigAsset: number
  forms: PageElement[]
  forceReq: RescanRecoveryRequest
  forceUrl: string
  currentForm: PageElement
  restoreInfoCard: HTMLElement

  constructor (body: HTMLElement) {
    super()
    this.body = body
    const page = this.page = Doc.idDescendants(body)

    Doc.cleanTemplates(page.restoreInfoCard)
    this.restoreInfoCard = page.restoreInfoCard.cloneNode(true) as HTMLElement

    this.forms = Doc.applySelector(page.forms, ':scope > form')
    page.forms.querySelectorAll('.form-closer').forEach(el => {
      Doc.bind(el, 'click', () => { this.closePopups() })
    })
    Doc.bind(page.cancelForce, 'click', () => { this.closePopups() })
    Doc.bind(page.copyAddressBtn, 'click', () => { this.copyAddress() })

    // Read the document, storing some info about each asset's row.
    const getAction = (row: HTMLElement, name: string) => row.querySelector(`[data-action=${name}]`) as HTMLElement
    const rowInfos: Record<string, RowInfo> = this.rowInfos = {}
    const rows = Doc.applySelector(page.walletTable, 'tr')
    let firstRow
    for (const tr of rows) {
      const assetID = parseInt(tr.dataset.assetID || '')
      rowInfos[assetID] = {
        assetID: assetID,
        tr: tr,
        symbol: tr.dataset.symbol || '',
        name: tr.dataset.name || '',
        stateIcons: new WalletIcons(tr),
        actions: {
          connect: getAction(tr, 'connect'),
          unlock: getAction(tr, 'unlock'),
          send: getAction(tr, 'send'),
          deposit: getAction(tr, 'deposit'),
          create: getAction(tr, 'create'),
          rescan: getAction(tr, 'rescan'),
          lock: getAction(tr, 'lock'),
          settings: getAction(tr, 'settings')
        }
      }
      if (!firstRow) firstRow = rowInfos[assetID]
    }

    // Prepare templates
    page.marketCard.removeAttribute('id')
    page.marketCard.remove()
    page.oneMarket.removeAttribute('id')
    page.oneMarket.remove()

    // Bind the new wallet form.
    this.newWalletForm = new NewWalletForm(page.newWalletForm, () => { this.createWalletSuccess() })

    // Bind the wallet reconfig form.
    this.reconfigForm = new WalletConfigForm(page.reconfigInputs, false)

    // Bind the wallet unlock form.
    this.unlockForm = new UnlockWalletForm(page.unlockWalletForm, () => { this.openWalletSuccess() })

    // Bind the Send form.
    bindForm(page.sendForm, page.submitSendForm, () => { this.send() })

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

    Doc.bind(page.forms, 'mousedown', (e: MouseEvent) => {
      if (!Doc.mouseInElement(e, this.currentForm)) { this.closePopups() }
    })

    this.keyup = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        if (Doc.isDisplayed(this.page.forms)) {
          this.closePopups()
        } else {
          this.showMarkets(this.lastFormAsset)
        }
      }
    }
    bind(document, 'keyup', this.keyup)

    bind(page.downloadLogs, 'click', async () => { this.downloadLogs() })
    bind(page.exportWallet, 'click', async () => { this.displayExportWalletAuth() })
    bind(page.recoverWallet, 'click', async () => { this.showRecoverWallet() })
    bindForm(page.exportWalletAuth, page.exportWalletAuthSubmit, async () => { this.exportWalletAuthSubmit() })
    bindForm(page.recoverWalletConfirm, page.recoverWalletSubmit, () => { this.recoverWallet() })
    bindForm(page.confirmForce, page.confirmForceSubmit, async () => { this.confirmForceSubmit() })

    // Bind buttons
    for (const [k, asset] of Object.entries(rowInfos)) {
      const assetID = parseInt(k) // keys are string asset ID.
      const a = asset.actions
      const run = (e: Event, f: (assetID: number, asset: RowInfo) => void) => {
        e.stopPropagation()
        f(assetID, asset)
      }
      bind(a.connect, 'click', e => { run(e, this.doConnect.bind(this)) })
      bind(a.send, 'click', e => { run(e, this.showSendForm.bind(this)) })
      bind(a.deposit, 'click', e => { run(e, this.showDeposit.bind(this)) })
      bind(a.create, 'click', e => { run(e, this.showNewWallet.bind(this)) })
      bind(a.rescan, 'click', e => { run(e, this.rescanWallet.bind(this)) })
      bind(a.unlock, 'click', e => { run(e, this.openWallet.bind(this)) })
      bind(a.lock, 'click', async e => { run(e, this.lock.bind(this)) })
      bind(a.settings, 'click', e => { run(e, this.showReconfig.bind(this)) })
    }

    // New deposit address button.
    bind(page.newDepAddrBttn, 'click', async () => { this.newDepositAddress() })

    // Clicking on the available amount on the Send form populates the
    // amount field.
    bind(page.sendAvail, 'click', () => {
      const asset = this.sendAsset
      const bal = asset.wallet.balance.available
      page.sendAmt.value = String(bal / asset.info.unitinfo.conventional.conversionFactor)
      this.showFiatValue(asset.id, bal, page.sendValue)
      // Ensure we don't check subtract checkbox for assets that don't have a
      // withdraw method.
      if ((asset.wallet.traits & traitWithdrawer) === 0) page.subtractCheckBox.checked = false
      else page.subtractCheckBox.checked = true
    })

    for (const [assetID, wallet] of (Object.entries(app().walletMap) as [any, WalletState][])) {
      if (!wallet) continue
      const fiatDisplay = this.page.walletTable.querySelector(`[data-conversion-target="${assetID}"]`) as PageElement
      if (!fiatDisplay) continue
      this.showFiatValue(assetID, wallet.balance.available, fiatDisplay)
    }

    // Display fiat value for current send amount.
    bind(page.sendAmt, 'input', () => {
      const asset = this.sendAsset
      if (!asset) return
      const amt = parseFloat(page.sendAmt.value || '0')
      const conversionFactor = asset.info.unitinfo.conventional.conversionFactor
      this.showFiatValue(asset.id, amt * conversionFactor, page.sendValue)
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

    app().registerNoteFeeder({
      fiatrateupdate: (note: RateNote) => { this.handleRatesNote(note) },
      balance: (note: BalanceNote) => { this.handleBalanceNote(note) },
      walletstate: (note: WalletStateNote) => { this.handleWalletStateNote(note) },
      walletconfig: (note: WalletStateNote) => { this.handleWalletStateNote(note) }
    })
  }

  closePopups () {
    Doc.hide(this.page.forms)
  }

  async copyAddress () {
    const page = this.page
    navigator.clipboard.writeText(page.depositAddress.textContent || '')
      .then(() => {
        Doc.show(page.copyAlert)
        setTimeout(() => {
          Doc.hide(page.copyAlert)
        }, 800)
      })
      .catch((reason) => {
        console.error('Unable to copy: ', reason)
      })
  }

  /*
   * setPWSettingViz sets the visibility of the password field section.
   */
  setPWSettingViz (visible: boolean) {
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
  async showBox (box: HTMLElement, focuser?: PageElement) {
    box.style.opacity = '0'
    Doc.show(box)
    if (focuser) focuser.focus()
    await Doc.animate(animationLength, progress => {
      box.style.opacity = `${progress}`
    }, 'easeOut')
    box.style.opacity = '1'
    this.displayed = box
  }

  /* showForm shows a modal form with a little animation. */
  async showForm (form: PageElement) {
    const page = this.page
    this.currentForm = form
    this.forms.forEach(form => Doc.hide(form))
    form.style.right = '10000px'
    Doc.show(page.forms, form)
    const shift = (page.forms.offsetWidth + form.offsetWidth) / 2
    await Doc.animate(animationLength, progress => {
      form.style.right = `${(1 - progress) * shift}px`
    }, 'easeOutHard')
    form.style.right = '0'
  }

  /*
   * Show the markets box, which lists the markets available for a selected
   * asset.
   */
  async showMarkets (assetID: number) {
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
      if (!xc.markets) continue
      for (const market of Object.values(xc.markets)) {
        if (market.baseid === assetID || market.quoteid === assetID) count++
      }
      if (count === 0) continue
      const marketBox = page.marketCard.cloneNode(true) as HTMLElement
      const tmpl = Doc.parseTemplate(marketBox)
      tmpl.dexTitle.textContent = host
      card.appendChild(marketBox)
      if (!xc.markets) continue
      for (const market of Object.values(xc.markets)) {
        // Only show markets where this is the base or quote asset.
        if (market.baseid !== assetID && market.quoteid !== assetID) continue
        const mBox = page.oneMarket.cloneNode(true) as HTMLElement
        Doc.safeSelector(mBox, 'span').textContent = prettyMarketName(market)
        let counterSymbol = market.basesymbol
        if (market.baseid === assetID) counterSymbol = market.quotesymbol
        Doc.safeSelector(mBox, 'img').src = Doc.logoPath(counterSymbol)
        // Bind the click to a load of the markets page.
        const pageData = { host: host, base: market.baseid, quote: market.quoteid }
        bind(mBox, 'click', () => { app().loadPage('markets', pageData) })
        tmpl.markets.appendChild(mBox)
      }
    }
    this.animation = this.showBox(box)
  }

  /* Show the new wallet form. */
  async showNewWallet (assetID: number) {
    const page = this.page
    const box = page.newWalletForm
    await this.hideBox()
    this.walletAsset = this.lastFormAsset = assetID
    this.newWalletForm.setAsset(assetID)
    this.animation = this.showBox(box)
    await this.newWalletForm.loadDefaults()
  }

  async rescanWallet (assetID: number) {
    const loaded = app().loading(this.body)
    const url = '/api/rescanwallet'
    const req = { assetID: assetID }
    const res = await postJSON(url, req)
    loaded()
    if (res.code === activeOrdersErrCode) {
      this.forceUrl = url
      this.forceReq = req
      this.showConfirmForce()
      return
    }
    app().checkResponse(res)
  }

  showConfirmForce () {
    Doc.hide(this.page.confirmForceErr)
    this.showForm(this.page.confirmForce)
  }

  showRecoverWallet () {
    Doc.hide(this.page.recoverWalletErr)
    this.showForm(this.page.recoverWalletConfirm)
  }

  /* Show the open wallet form if the password is not cached, and otherwise
   * attempt to open the wallet.
   */
  async openWallet (assetID: number) {
    if (!State.passwordIsCached()) {
      this.showOpen(assetID)
    } else {
      this.openAsset = assetID
      const open = {
        assetID: assetID
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
  async showOpen (assetID: number, errorMsg?: string) {
    const page = this.page
    this.openAsset = this.lastFormAsset = assetID
    await this.hideBox()
    this.unlockForm.refresh(app().assets[assetID])
    if (errorMsg) this.unlockForm.showErrorOnly(errorMsg)
    this.animation = this.showBox(page.unlockWalletForm, page.walletPass)
  }

  /* Show the form used to change wallet configuration settings. */
  async showReconfig (assetID: number) {
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
        const option = document.createElement('option') as HTMLOptionElement
        if (wDef.type === currentDef.type) option.selected = true
        option.value = option.textContent = wDef.type
        page.changeWalletTypeSelect.appendChild(option)
      }
    } else {
      Doc.hide(page.showChangeType)
    }

    const wallet = app().walletMap[assetID]
    if ((wallet.traits & traitLogFiler) !== 0) Doc.show(page.downloadLogs)
    else Doc.hide(page.downloadLogs)
    if ((wallet.traits & traitRecoverer) !== 0) Doc.show(page.recoverWallet)
    else Doc.hide(page.recoverWallet)
    if ((wallet.traits & traitRestorer)) Doc.show(page.exportWallet)
    else Doc.hide(page.exportWallet)

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
    const walletIsActive = app().walletIsActive(assetID)
    this.reconfigForm.update(currentDef.configopts || [], walletIsActive)
    this.reconfigForm.setConfig(res.map)
    this.updateDisplayedReconfigFields(currentDef)
  }

  changeWalletType () {
    const page = this.page
    const walletType = page.changeWalletTypeSelect.value || ''
    const walletDef = app().walletDefinition(this.reconfigAsset, walletType)
    this.reconfigForm.update(walletDef.configopts || [])
    this.updateDisplayedReconfigFields(walletDef)
  }

  updateDisplayedReconfigFields (walletDef: WalletDefinition) {
    if (walletDef.seeded) {
      Doc.hide(this.page.showChangePW)
      this.changeWalletPW = false
      this.setPWSettingViz(false)
    } else Doc.show(this.page.showChangePW)
  }

  /* Display a deposit address. */
  async showDeposit (assetID: number) {
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
    page.qrcode.src = `/generateqrcode?address=${wallet.address}`
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
    page.qrcode.src = `/generateqrcode?address=${res.address}`
  }

  /* Show the form to either send or withdraw funds. */
  async showSendForm (assetID: number) {
    const page = this.page
    const box = page.sendForm
    const asset = this.sendAsset = app().assets[assetID]
    this.lastFormAsset = assetID
    const wallet = app().walletMap[assetID]
    if (!wallet) {
      app().notify(ntfn.make('Cannot send/withdraw.', `No wallet found for ${asset.info.name}`, ntfn.ERROR))
    }
    await this.hideBox()

    Doc.hide(page.senderOnlyHelpText)
    Doc.hide(page.toggleSubtract)
    page.subtractCheckBox.checked = false

    const isWithdrawer = (wallet.traits & traitWithdrawer) !== 0
    if (!isWithdrawer) {
      Doc.show(page.senderOnlyHelpText)
      page.subtractCheckBox.checked = false
    } else {
      Doc.show(page.toggleSubtract)
    }

    page.sendAddr.value = ''
    page.sendAmt.value = ''
    page.sendPW.value = ''
    page.sendErr.textContent = ''

    this.showFiatValue(asset.id, 0, page.sendValue)
    page.sendAvail.textContent = Doc.formatFullPrecision(wallet.balance.available, asset.info.unitinfo)
    page.sendLogo.src = Doc.logoPath(asset.symbol)
    page.sendName.textContent = asset.info.name
    // page.sendFee.textContent = wallet.feerate
    // page.sendUnit.textContent = wallet.units
    box.dataset.assetID = String(assetID)
    this.animation = this.showBox(box, page.walletPass)
  }

  /* doConnect connects to a wallet via the connectwallet API route. */
  async doConnect (assetID: number) {
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
    await app().fetchUser()
    await app().loadPage('wallets')
  }

  /* openWalletSuccess is the success callback for wallet unlocking. */
  async openWalletSuccess () {
    const rowInfo = this.rowInfos[this.openAsset]
    const a = rowInfo.actions
    Doc.show(a.send, a.deposit)
    Doc.hide(a.unlock, a.connect)
    if (app().walletMap[rowInfo.assetID].encrypted) {
      Doc.show(a.lock)
    }
    this.showMarkets(this.openAsset)
  }

  /* send submits the send form to the API. */
  async send () {
    const page = this.page
    Doc.hide(page.sendErr)
    const assetID = parseInt(page.sendForm.dataset.assetID || '')
    const subtract = page.subtractCheckBox.checked || false
    const conversionFactor = app().unitInfo(assetID).conventional.conversionFactor
    const open = {
      assetID: assetID,
      address: page.sendAddr.value,
      subtract: subtract,
      value: Math.round(parseFloat(page.sendAmt.value || '') * conversionFactor),
      pw: page.sendPW.value
    }
    const loaded = app().loading(page.sendForm)
    const res = await postJSON('/api/send', open)
    loaded()
    if (!app().checkResponse(res, true)) {
      page.sendErr.textContent = res.msg
      Doc.show(page.sendErr)
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
      walletType = page.changeWalletTypeSelect.value || ''
    }

    const loaded = app().loading(page.reconfigForm)
    const req: ReconfigRequest = {
      assetID: this.reconfigAsset,
      config: this.reconfigForm.map(),
      appPW: page.appPW.value || '',
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
  async lock (assetID: number, asset: RowInfo) {
    const page = this.page
    const loaded = app().loading(page.newWalletForm)
    const res = await postJSON('/api/closewallet', { assetID: assetID })
    loaded()
    if (!app().checkResponse(res)) return
    const a = asset.actions
    Doc.hide(a.send, a.lock, a.deposit)
    Doc.show(a.unlock)
  }

  async downloadLogs () {
    const search = new URLSearchParams('')
    search.append('assetid', `${this.reconfigAsset}`)
    const url = new URL(window.location.href)
    url.search = search.toString()
    url.pathname = '/wallets/logfile'
    window.open(url.toString())
  }

  // displayExportWalletAuth displays a form to warn the user about the
  // dangers of exporting a wallet, and asks them to enter their password.
  async displayExportWalletAuth () {
    const page = this.page
    Doc.hide(page.exportWalletErr)
    page.exportWalletPW.value = ''
    this.showForm(page.exportWalletAuth)
  }

  // exportWalletAuthSubmit is called after the user enters their password to
  // authorize looking up the information to restore their wallet in an
  // external wallet.
  async exportWalletAuthSubmit () {
    const page = this.page
    const req = {
      assetID: this.reconfigAsset,
      pass: page.exportWalletPW.value
    }
    const url = '/api/restorewalletinfo'
    const loaded = app().loading(page.forms)
    const res = await postJSON(url, req)
    loaded()
    if (app().checkResponse(res)) {
      page.exportWalletPW.value = ''
      this.displayRestoreWalletInfo(res.restorationinfo)
    } else {
      page.exportWalletErr.textContent = res.msg
      Doc.show(page.exportWalletErr)
    }
  }

  // displayRestoreWalletInfo displays the information needed to restore a
  // wallet in external wallets.
  async displayRestoreWalletInfo (info: WalletRestoration[]) {
    const page = this.page
    Doc.empty(page.restoreInfoCardsList)
    for (const wr of info) {
      const card = this.restoreInfoCard.cloneNode(true) as HTMLElement
      const tmpl = Doc.parseTemplate(card)
      tmpl.name.textContent = wr.target
      tmpl.seed.textContent = wr.seed
      tmpl.seedName.textContent = `${wr.seedName}:`
      tmpl.instructions.textContent = wr.instructions
      page.restoreInfoCardsList.appendChild(card)
    }
    this.showForm(page.restoreWalletInfo)
  }

  async recoverWallet () {
    const page = this.page
    Doc.hide(page.recoverWalletErr)
    const req = {
      assetID: this.reconfigAsset,
      appPW: page.recoverWalletPW.value
    }
    page.recoverWalletPW.value = ''
    const url = '/api/recoverwallet'
    const loaded = app().loading(page.forms)
    const res = await postJSON(url, req)
    loaded()
    if (res.code === activeOrdersErrCode) {
      this.forceUrl = url
      this.forceReq = req
      this.showConfirmForce()
    } else if (app().checkResponse(res)) {
      this.closePopups()
    } else {
      page.recoverWalletErr.textContent = res.msg
      Doc.show(page.recoverWalletErr)
    }
  }

  /*
   * confirmForceSubmit resubmits either the recover or rescan requests with
   * force set to true. These two requests require force to be set to true if
   * they are called while the wallet is managing active orders.
   */
  async confirmForceSubmit () {
    const page = this.page
    this.forceReq.force = true
    const loaded = app().loading(page.forms)
    const res = await postJSON(this.forceUrl, this.forceReq)
    loaded()
    if (app().checkResponse(res)) this.closePopups()
    else {
      page.confirmForceErr.textContent = res.msg
      Doc.show(page.confirmForceErr)
    }
  }

  /* handleBalance handles notifications updating a wallet's balance and assets'
     value in default fiat rate.
  . */
  handleBalanceNote (note: BalanceNote) {
    const td = Doc.safeSelector(this.page.walletTable, `[data-balance-target="${note.assetID}"]`)
    td.textContent = Doc.formatFullPrecision(note.balance.available, app().unitInfo(note.assetID))
    const fiatDisplay = Doc.safeSelector(this.page.walletTable, `[data-conversion-target="${note.assetID}"]`)
    if (!fiatDisplay) return
    this.showFiatValue(note.assetID, note.balance.available, fiatDisplay)
  }

  /* handleRatesNote handles fiat rate notifications, updating the fiat value of
   *  all supported assets.
   */
  handleRatesNote (note: RateNote) {
    app().fiatRatesMap = note.fiatRates
    for (const [assetID, wallet] of (Object.entries(app().walletMap) as [any, WalletState][])) {
      if (!wallet) continue
      const fiatDisplay = this.page.walletTable.querySelector(`[data-conversion-target="${assetID}"]`) as PageElement
      if (!fiatDisplay) continue
      this.showFiatValue(assetID, wallet.balance.available, fiatDisplay)
    }
  }

  // showFiatValue displays the fiat equivalent for the provided amount.
  showFiatValue (assetID: number, amount: number, display: PageElement) {
    if (display) {
      const rate = app().fiatRatesMap[assetID]
      display.textContent = Doc.formatFiatConversion(amount, rate, app().unitInfo(assetID))
      if (rate) Doc.show(display.parentElement as Element)
      else Doc.hide(display.parentElement as Element)
    }
  }

  /*
   * handleWalletStateNote is a handler for both the 'walletstate' and
   * 'walletconfig' notifications.
   */
  handleWalletStateNote (note: WalletStateNote) {
    this.rowInfos[note.wallet.assetID].stateIcons.readWallet(note.wallet)
    const fiatDisplay = this.page.walletTable.querySelector(`[data-conversion-target="${note.wallet.assetID}"]`) as PageElement
    if (!fiatDisplay) return
    this.showFiatValue(note.wallet.assetID, note.wallet.balance.available, fiatDisplay)
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
function prettyMarketName (market: Market) {
  return `${market.basesymbol.toUpperCase()}-${market.quotesymbol.toUpperCase()}`
}
