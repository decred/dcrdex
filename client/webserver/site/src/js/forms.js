import { app } from './registry'
import Doc from './doc'
import { postJSON } from './http'
import State from './state'
import { feeSendErr } from './constants'
import * as intl from './locales'

/*
 * NewWalletForm should be used with the "newWalletForm" template. The enclosing
 * <form> element should be the second argument of the constructor.
 */
export class NewWalletForm {
  constructor (form, success, pwCache, closerFn) {
    this.form = form
    this.currentAsset = null
    this.pwCache = pwCache
    const page = this.page = Doc.idDescendants(form)
    this.refresh()

    if (closerFn) {
      form.querySelectorAll('.form-closer').forEach(el => {
        Doc.show(el)
        Doc.bind(el, 'click', closerFn)
      })
    }

    Doc.empty(page.walletTabTmpl)
    page.walletTabTmpl.removeAttribute('id')

    // WalletConfigForm will set the global app variable.
    this.subform = new WalletConfigForm(page.walletSettings, true)

    Doc.bind(this.subform.showOther, 'click', () => Doc.show(page.walletSettingsHeader))

    bind(form, page.submitAdd, async () => {
      const pw = page.nwAppPass.value || (this.pwCache ? this.pwCache.pw : '')
      if (!pw && !State.passwordIsCached()) {
        page.newWalletErr.textContent = intl.prep(intl.ID_NO_APP_PASS_ERROR_MSG)
        Doc.show(page.newWalletErr)
        return
      }
      Doc.hide(page.newWalletErr)
      const assetID = parseInt(this.currentAsset.id)

      const createForm = {
        assetID: assetID,
        pass: page.newWalletPass.value || '',
        config: this.subform.map(),
        appPass: pw,
        walletType: this.currentWalletType
      }
      page.nwAppPass.value = ''
      const loaded = app().loading(page.nwMainForm)
      const res = await postJSON('/api/newwallet', createForm)
      loaded()
      if (!app().checkResponse(res)) {
        this.setError(res.msg)
        return
      }
      if (this.pwCache) this.pwCache.pw = pw
      page.newWalletPass.value = ''
      success(assetID)
    })
  }

  refresh () {
    const hidePWBox = State.passwordIsCached() || (this.pwCache && this.pwCache.pw)
    if (hidePWBox) Doc.hide(this.page.newWalletAppPWBox)
    else Doc.show(this.page.newWalletAppPWBox)
  }

  async setAsset (assetID) {
    const page = this.page
    const asset = app().assets[assetID]
    const tabs = page.walletTypeTabs
    if (this.currentAsset && this.currentAsset.id === asset.id) return
    this.currentAsset = asset
    page.nwAssetLogo.src = Doc.logoPath(asset.symbol)
    page.nwAssetName.textContent = asset.info.name
    page.newWalletPass.value = ''

    const walletDef = asset.info.availablewallets[0]
    Doc.empty(tabs)
    Doc.hide(tabs, page.newWalletErr)

    if (asset.info.availablewallets.length > 1) {
      Doc.show(tabs)
      for (const wDef of asset.info.availablewallets) {
        const tab = page.walletTabTmpl.cloneNode(true)
        tab.dataset.tooltip = wDef.description
        tab.textContent = wDef.tab
        tabs.appendChild(tab)
        Doc.bind(tab, 'click', () => {
          for (const t of tabs.children) t.classList.remove('selected')
          tab.classList.add('selected')
          this.update(wDef)
        })
      }
      app().bindTooltips(tabs)
      tabs.firstChild.classList.add('selected')
    }

    await this.update(walletDef)
  }

  async update (walletDef) {
    const page = this.page
    this.currentWalletType = walletDef.type
    if (walletDef.seeded) {
      page.newWalletPass.value = ''
      page.submitAdd.textContent = 'Create'
      Doc.hide(page.newWalletPassBox)
    } else {
      Doc.show(page.newWalletPassBox)
      page.submitAdd.textContent = 'Add'
    }

    this.subform.update(walletDef.configopts || [])

    if (this.subform.dynamicOpts.children.length) Doc.show(page.walletSettingsHeader)
    else Doc.hide(page.walletSettingsHeader)

    this.refresh()
    await this.loadDefaults()
  }

  /* setError sets and shows the in-form error message. */
  async setError (errMsg) {
    this.page.newWalletErr.textContent = errMsg
    Doc.show(this.page.newWalletErr)
  }

  /*
   * loadDefaults attempts to load the ExchangeWallet configuration from the
   * default wallet config path on the server and will auto-fill the page on
   * the subform if settings are found.
   */
  async loadDefaults () {
    // No default config files for seeded assets right now.
    const walletDef = app().walletDefinition(this.currentAsset.id, this.currentWalletType)
    if (walletDef.seeded) return
    const loaded = app().loading(this.form)
    const res = await postJSON('/api/defaultwalletcfg', {
      assetID: this.currentAsset.id,
      type: this.currentWalletType
    })
    loaded()
    if (!app().checkResponse(res)) {
      this.setError(res.msg)
      return
    }
    this.subform.setLoadedConfig(res.config)
  }
}

/*
 * WalletConfigForm is a dynamically generated sub-form for setting
 * asset-specific wallet configuration options.
*/
export class WalletConfigForm {
  constructor (form, sectionize) {
    this.form = form
    // A configElement is a div containing an input and its label.
    this.configElements = {}
    // configOpts is the wallet options provided by core.
    this.configOpts = []
    this.sectionize = sectionize

    // Get template elements
    this.allSettings = Doc.tmplElement(form, 'allSettings')
    this.dynamicOpts = Doc.tmplElement(form, 'dynamicOpts')
    this.textInputTmpl = Doc.tmplElement(form, 'textInput')
    this.textInputTmpl.remove()
    this.checkboxTmpl = Doc.tmplElement(form, 'checkbox')
    this.checkboxTmpl.remove()
    this.fileSelector = Doc.tmplElement(form, 'fileSelector')
    this.fileInput = Doc.tmplElement(form, 'fileInput')
    this.errMsg = Doc.tmplElement(form, 'errMsg')
    this.showOther = Doc.tmplElement(form, 'showOther')
    this.showIcon = Doc.tmplElement(form, 'showIcon')
    this.hideIcon = Doc.tmplElement(form, 'hideIcon')
    this.showHideMsg = Doc.tmplElement(form, 'showHideMsg')
    this.otherSettings = Doc.tmplElement(form, 'otherSettings')
    this.loadedSettingsMsg = Doc.tmplElement(form, 'loadedSettingsMsg')
    this.loadedSettings = Doc.tmplElement(form, 'loadedSettings')
    this.defaultSettingsMsg = Doc.tmplElement(form, 'defaultSettingsMsg')
    this.defaultSettings = Doc.tmplElement(form, 'defaultSettings')

    if (!sectionize) Doc.hide(this.showOther)

    Doc.bind(this.fileSelector, 'click', () => this.fileInput.click())

    // config file upload
    Doc.bind(this.fileInput, 'change', async () => this.fileInputChanged())

    Doc.bind(this.showOther, 'click', () => {
      this.setOtherSettingsViz(this.hideIcon.classList.contains('d-hide'))
    })
  }

  /*
   * fileInputChanged will read the selected file and attempt to load the
   * configuration settings. All loaded settings will be made visible for
   * inspection by the user.
   */
  async fileInputChanged () {
    Doc.hide(this.errMsg)
    if (!this.fileInput.value) return
    const loaded = app().loading(this.form)
    const config = await this.fileInput.files[0].text()
    if (!config) return
    const res = await postJSON('/api/parseconfig', {
      configtext: config
    })
    loaded()
    if (!app().checkResponse(res)) {
      this.errMsg.textContent = res.msg
      Doc.show(this.errMsg)
      return
    }
    if (Object.keys(res.map).length === 0) return
    this.dynamicOpts.append(...this.setConfig(res.map))
    this.reorder(this.dynamicOpts)
    const [loadedOpts, defaultOpts] = [this.loadedSettings.children.length, this.defaultSettings.children.length]
    if (loadedOpts === 0) Doc.hide(this.loadedSettings, this.loadedSettingsMsg)
    if (defaultOpts === 0) Doc.hide(this.defaultSettings, this.defaultSettingsMsg)
    if (loadedOpts + defaultOpts === 0) Doc.hide(this.showOther, this.otherSettings)
  }

  /*
   * update creates the dynamic form.
   */
  update (configOpts) {
    this.configElements = {}
    this.configOpts = configOpts
    Doc.empty(this.dynamicOpts, this.defaultSettings, this.loadedSettings)

    // If there are no options, just hide the entire form.
    if (configOpts.length === 0) return Doc.hide(this.form)
    Doc.show(this.form)

    this.setOtherSettingsViz(false)
    Doc.hide(
      this.loadedSettingsMsg, this.loadedSettings, this.defaultSettingsMsg,
      this.defaultSettings, this.errMsg
    )
    const defaultedOpts = []
    const addOpt = (box, opt) => {
      const elID = 'wcfg-' + opt.key
      const el = opt.isboolean ? this.checkboxTmpl.cloneNode(true) : this.textInputTmpl.cloneNode(true)
      this.configElements[opt.key] = el
      const input = el.querySelector('input')
      input.id = elID
      input.configOpt = opt
      const label = el.querySelector('label')
      label.htmlFor = elID // 'for' attribute, but 'for' is a keyword
      label.prepend(opt.displayname)
      box.appendChild(el)
      if (opt.noecho) input.type = 'password'
      if (opt.description) label.dataset.tooltip = opt.description
      if (opt.isboolean) input.checked = opt.default
      else input.value = opt.default !== null ? opt.default : ''
    }
    for (const opt of this.configOpts) {
      if (this.sectionize && opt.default !== null) defaultedOpts.push(opt)
      else addOpt(this.dynamicOpts, opt)
    }
    if (defaultedOpts.length) {
      for (const opt of defaultedOpts) {
        addOpt(this.defaultSettings, opt)
      }
      Doc.show(this.showOther, this.defaultSettingsMsg, this.defaultSettings)
    } else {
      Doc.hide(this.showOther)
    }
    app().bindTooltips(this.allSettings)
    if (this.dynamicOpts.children.length) Doc.show(this.dynamicOpts)
    else Doc.hide(this.dynamicOpts)
  }

  /*
   * setOtherSettingsViz sets the visibility of the additional settings section.
   */
  setOtherSettingsViz (visible) {
    if (visible) {
      Doc.hide(this.showIcon)
      Doc.show(this.hideIcon, this.otherSettings)
      this.showHideMsg.textContent = intl.prep(intl.ID_HIDE_ADDITIONAL_SETTINGS)
      return
    }
    Doc.hide(this.hideIcon, this.otherSettings)
    Doc.show(this.showIcon)
    this.showHideMsg.textContent = intl.prep(intl.ID_SHOW_ADDITIONAL_SETTINGS)
  }

  /*
   * setConfig looks for inputs with configOpt keys matching the cfg object, and
   * sets the inputs value to the corresponding cfg value. A list of matching
   * configElements is returned.
   */
  setConfig (cfg) {
    const finds = []
    this.allSettings.querySelectorAll('input').forEach(input => {
      const k = input.configOpt.key
      const v = cfg[k]
      if (typeof v === 'undefined') return
      finds.push(this.configElements[k])
      if (input.configOpt.isboolean) input.checked = isTruthyString(v)
      else input.value = v
    })
    return finds
  }

  /*
   * setLoadedConfig sets the input values for the entries in cfg, and moves
   * them to the loadedSettings box.
   */
  setLoadedConfig (cfg) {
    const finds = this.setConfig(cfg)
    if (!this.sectionize || finds.length === 0) return
    this.loadedSettings.append(...finds)
    this.reorder(this.loadedSettings)
    Doc.show(this.loadedSettings, this.loadedSettingsMsg)
    if (this.defaultSettings.children.length === 0) Doc.hide(this.defaultSettings, this.defaultSettingsMsg)
  }

  /*
   * map reads all inputs and constructs an object from the configOpt keys and
   * values.
   */
  map () {
    const config = {}
    this.allSettings.querySelectorAll('input').forEach(input => {
      if (input.configOpt.isboolean && input.configOpt.key) config[input.configOpt.key] = input.checked ? '1' : '0'
      else if (input.value) config[input.configOpt.key] = input.value
    })

    return config
  }

  /*
   * reorder sorts the configElements in the box by the order of the
   * server-provided configOpts array.
   */
  reorder (box) {
    const els = {}
    box.querySelectorAll('input').forEach(el => {
      const k = el.configOpt.key
      els[k] = this.configElements[k]
    })
    for (const opt of this.configOpts) {
      const el = els[opt.key]
      if (el) box.append(el)
    }
  }
}

/*
 * ConfirmRegistrationForm should be used with the "confirmRegistrationForm" template.
 */
export class ConfirmRegistrationForm {
  constructor (form, { getDexAddr, getCertFile }, success, insufficientFundsFail, setupWalletFn) {
    this.page = Doc.idDescendants(form)
    this.getDexAddr = getDexAddr
    this.getCertFile = getCertFile
    this.success = success
    this.insufficientFundsFail = insufficientFundsFail
    this.form = form
    this.setupWalletFn = setupWalletFn
    this.feeAssetID = null
    this.syncWaiters = {}
    bind(form, this.page.submitConfirm, () => this.submitForm())
  }

  registerSyncWaiter (assetID, f) {
    let waiters = this.syncWaiters[assetID]
    if (!waiters) waiters = this.syncWaiters[assetID] = []
    waiters.push(f)
  }

  handleWalletStateNote (note) {
    if (note.wallet.synced) {
      const waiters = this.syncWaiters[note.wallet.assetID]
      if (!waiters) return
      for (const f of waiters) f()
      this.syncWaiters[note.wallet.assetID] = []
    }
  }

  /*
   * setExchange populates the form with the details of an exchange.
   */
  setExchange (xc) {
    // store xc in case we need to refresh the data, like after setting up
    // a new wallet.
    this.xc = xc
    const page = this.page
    this.fees = xc.regFees
    Doc.empty(page.marketsTableRows, page.feeTableRows)
    this.walletRows = {}

    for (const [symbol, fee] of Object.entries(xc.regFees)) {
      // if asset fee is not supported by the client we can skip it.
      if (app().user.assets[fee.id] === undefined) continue
      const unitInfo = app().assets[fee.id].info.unitinfo
      const wallet = app().user.assets[fee.id].wallet
      const tr = page.feeRowTemplate.cloneNode(true)
      this.walletRows[fee.id] = tr
      Doc.bind(tr, 'click', () => { this.selectRow(fee.id) })
      Doc.tmplElement(tr, 'asseticon').src = Doc.logoPath(symbol)
      Doc.tmplElement(tr, 'asset').innerText = unitInfo.conventional.unit
      Doc.tmplElement(tr, 'confs').innerText = fee.confs
      Doc.tmplElement(tr, 'fee').innerText = Doc.formatCoinValue(fee.amount, unitInfo)

      const setupWallet = Doc.tmplElement(tr, 'setupWallet')
      const walletReady = Doc.tmplElement(tr, 'walletReady')
      const walletSyncing = Doc.tmplElement(tr, 'walletSyncing')
      if (wallet) {
        if (wallet.synced) {
          walletReady.innerText = intl.prep(intl.ID_WALLET_READY)
          Doc.show(walletReady)
          Doc.hide(walletSyncing)
        } else {
          walletReady.innerText = intl.prep(intl.ID_WALLET_READY)
          Doc.show(walletSyncing)
          Doc.hide(walletReady)
          this.registerSyncWaiter(fee.id, () => {
            Doc.show(walletReady)
            Doc.hide(walletSyncing)
            if (this.feeAssetID === fee.id) page.submitConfirm.classList.add('selected')
          })
        }
        Doc.hide(setupWallet)
      } else {
        setupWallet.innerText = intl.prep(intl.ID_SETUP_WALLET)
        Doc.bind(setupWallet, 'click', () => this.setupWalletFn(fee.id))
        Doc.hide(walletReady)
        Doc.show(setupWallet)
      }
      page.feeTableRows.appendChild(tr)
      if (State.passwordIsCached()) {
        Doc.hide(page.appPassBox)
        Doc.hide(page.appPassSpan)
      } else {
        Doc.show(page.appPassBox)
        Doc.show(page.appPassSpan)
      }
    }
    const markets = Object.values(xc.markets)
    markets.sort((m1, m2) => {
      const compareBase = m1.basesymbol.localeCompare(m2.basesymbol)
      const compareQuote = m1.quotesymbol.localeCompare(m2.quotesymbol)
      return compareBase === 0 ? compareQuote : compareBase
    })
    markets.forEach((market) => {
      const tr = page.marketRowTemplate.cloneNode(true)
      Doc.tmplElement(tr, 'baseicon').src = Doc.logoPath(market.basesymbol)
      Doc.tmplElement(tr, 'quoteicon').src = Doc.logoPath(market.quotesymbol)
      Doc.tmplElement(tr, 'base').innerText = market.basesymbol.toUpperCase()
      Doc.tmplElement(tr, 'quote').innerText = market.quotesymbol.toUpperCase()
      const baseUnitInfo = app().unitInfo(market.baseid)
      const fmtVal = Doc.formatCoinValue(market.lotsize, baseUnitInfo)
      Doc.tmplElement(tr, 'lotsize').innerText = `${fmtVal} ${baseUnitInfo.conventional.unit}`
      page.marketsTableRows.appendChild(tr)
      if (State.passwordIsCached()) {
        Doc.hide(page.appPassBox)
        Doc.hide(page.appPassSpan)
      } else {
        Doc.show(page.appPassBox)
        Doc.show(page.appPassSpan)
      }
    })
  }

  selectRow (assetID) {
    const page = this.page
    const wallet = app().user.assets[assetID].wallet
    this.feeAssetID = assetID
    // remove selected class from all others row.
    const rows = Array.from(page.feeTableRows.querySelectorAll('tr.selected'))
    rows.forEach(row => row.classList.remove('selected'))
    this.walletRows[assetID].classList.add('selected')
    // if wallet is configured, we can active the register button.
    // Otherwise we do not allow it.
    if (wallet && wallet.synced) {
      page.submitConfirm.classList.add('selected')
    } else {
      page.submitConfirm.classList.remove('selected')
    }
  }

  /*
   * submitForm is called when the form is submitted.
   */
  async submitForm () {
    const page = this.page
    // if button is selected it can be clickable.
    if (!page.submitConfirm.classList.contains('selected')) {
      return
    }
    if (this.feeAssetID === null) {
      page.regErr.innerText = 'You must select a valid wallet for the fee payment'
      Doc.show(page.regErr)
      return
    }
    const symbol = app().user.assets[this.feeAssetID].wallet.symbol
    Doc.hide(page.regErr)
    const feeAsset = this.fees[symbol]
    const cert = await this.getCertFile()
    const dexAddr = this.getDexAddr()
    const registration = {
      addr: dexAddr,
      pass: page.appPass.value,
      fee: feeAsset.amount,
      asset: feeAsset.id,
      cert: cert
    }
    page.appPass.value = ''
    const loaded = app().loading(this.form)
    const res = await postJSON('/api/register', registration)
    if (!app().checkResponse(res)) {
      // This form is used both in the register workflow and the
      // settings page. The register workflow handles a failure
      // where the user does not have enough funds to pay for the
      // registration fee in a different way.
      if (res.code === feeSendErr && this.insufficientFundsFail) {
        loaded()
        this.insufficientFundsFail(res.msg)
        return
      }
      page.regErr.textContent = res.msg
      Doc.show(page.regErr)
      loaded()
      return
    }
    loaded()
    this.success()
  }

  refresh () {
    this.setExchange(this.xc)
  }
}

export class UnlockWalletForm {
  constructor (form, success, pwCache) {
    this.page = Doc.idDescendants(form)
    this.form = form
    this.pwCache = pwCache
    this.currentAsset = null
    this.success = success
    bind(form, this.page.submitUnlock, () => this.submit())
  }

  setAsset (asset) {
    const page = this.page
    this.currentAsset = asset
    page.uwAssetLogo.src = Doc.logoPath(asset.symbol)
    page.uwAssetName.textContent = asset.info.name
    page.uwAppPass.value = ''
    const hidePWBox = State.passwordIsCached() || (this.pwCache && this.pwCache.pw)
    if (hidePWBox) Doc.hide(page.uwAppPassBox)
    else Doc.show(page.uwAppPassBox)
  }

  /*
   * setError displays an error on the form.
   */
  setError (msg) {
    this.page.unlockErr.textContent = msg
    Doc.show(this.page.unlockErr)
  }

  /*
   * showErrorOnly displays only an error on the form. Hides the
   * app pass field and the submit button.
   */
  showErrorOnly (msg) {
    this.setError(msg)
    Doc.hide(this.page.uwAppPassBox)
    Doc.hide(this.page.submitUnlockDiv)
  }

  async submit () {
    const page = this.page
    const pw = page.uwAppPass.value || (this.pwCache ? this.pwCache.pw : '')
    if (!pw && !State.passwordIsCached()) {
      page.unlockErr.textContent = intl.prep(intl.ID_NO_APP_PASS_ERROR_MSG)
      Doc.show(page.unlockErr)
      return
    }
    Doc.hide(this.page.unlockErr)
    const open = {
      assetID: parseInt(this.currentAsset.id),
      pass: pw
    }
    page.uwAppPass.value = ''
    const loaded = app().loading(this.form)
    const res = await postJSON('/api/openwallet', open)
    loaded()
    if (!app().checkResponse(res)) {
      this.setError(res.msg)
      return
    }
    if (this.pwCache) this.pwCache.pw = pw
    this.success()
  }
}

export class DEXAddressForm {
  constructor (form, success, pwCache) {
    this.form = form
    this.success = success
    this.pwCache = pwCache
    this.defaultTLSText = 'none selected'

    const page = this.page = Doc.idDescendants(form)

    page.selectedCert.textContent = this.defaultTLSText
    Doc.bind(page.certFile, 'change', () => this.onCertFileChange())
    Doc.bind(page.removeCert, 'click', () => this.clearCertFile())
    Doc.bind(page.addCert, 'click', () => page.certFile.click())
    Doc.bind(page.dexShowMore, 'click', () => {
      Doc.hide(page.dexShowMore)
      Doc.show(page.dexCertBox)
    })

    bind(form, page.submitDEXAddr, () => this.checkDEX())
    this.refresh()
  }

  refresh () {
    const hidePWBox = State.passwordIsCached() || (this.pwCache && this.pwCache.pw)
    if (hidePWBox) Doc.hide(this.page.dexAddrAppPWBox)
    else Doc.show(this.page.dexAddrAppPWBox)
  }

  async checkDEX () {
    const page = this.page
    Doc.hide(page.dexAddrErr)
    const addr = page.dexAddr.value
    if (addr === '') {
      page.dexAddrErr.textContent = 'DEX address cannot be empty'
      Doc.show(page.dexAddrErr)
      return
    }

    let cert = ''
    if (page.certFile.value) {
      cert = await page.certFile.files[0].text()
    }

    let pw = ''
    if (!State.passwordIsCached()) {
      pw = page.dexAddrAppPW.value || this.pwCache.pw
    }

    const loaded = app().loading(this.form)

    const res = await postJSON('/api/discoveracct', {
      addr: addr,
      cert: cert,
      pass: pw
    })
    loaded()
    if (!app().checkResponse(res, true)) {
      if (res.msg === 'certificate required') {
        Doc.hide(page.dexShowMore)
        Doc.show(page.dexCertBox, page.dexNeedCert)
      } else {
        page.dexAddrErr.textContent = res.msg
        Doc.show(page.dexAddrErr)
      }

      return
    }

    if (res.paid) {
      await app().fetchUser()
      app().loadPage('markets')
      return
    }

    if (this.pwCache) this.pwCache.pw = pw
    this.success(res.xc)
  }

  /**
   * onCertFileChange when the input certFile changed, read the file
   * and setting cert name into text of selectedCert to display on the view
   */
  async onCertFileChange () {
    const page = this.page
    const files = page.certFile.files
    if (!files.length) return
    page.selectedCert.textContent = files[0].name
    Doc.show(page.removeCert)
    Doc.hide(page.addCert)
  }

  /* clearCertFile cleanup certFile value and selectedCert text */
  clearCertFile () {
    const page = this.page
    page.certFile.value = ''
    page.selectedCert.textContent = this.defaultTLSText
    Doc.hide(page.removeCert)
    Doc.show(page.addCert)
  }
}

/*
 * bind binds the click and submit events and prevents page reloading on
 * submission.
 */
export function bind (form, submitBttn, handler) {
  const wrapper = e => {
    if (e.preventDefault) e.preventDefault()
    handler(e)
  }
  Doc.bind(submitBttn, 'click', wrapper)
  Doc.bind(form, 'submit', wrapper)
}

// isTruthyString will be true if the provided string is recognized as a
// value representing true.
function isTruthyString (s) {
  return s === '1' || s.toLowerCase() === 'true'
}
