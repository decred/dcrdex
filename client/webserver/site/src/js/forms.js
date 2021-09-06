import Doc from './doc'
import { postJSON } from './http'
import State from './state'
import { feeSendErr } from './constants'
import {
  ID_HIDE_ADDIIONAL_SETTINGS,
  ID_NO_APP_PASS_ERROR_MSG,
  ID_SHOW_ADDIIONAL_SETTINGS
} from './locales'

let app

/*
 * NewWalletForm should be used with the "newWalletForm" template. The enclosing
 * <form> element should be the second argument of the constructor.
 */
export class NewWalletForm {
  constructor (application, form, success, pwCache) {
    this.form = form
    this.currentAsset = null
    this.pwCache = pwCache
    const fields = this.fields = Doc.parsePage(form, [
      'nwAssetLogo', 'nwAssetName', 'newWalletPass', 'nwAppPass',
      'walletSettings', 'selectCfgFile', 'cfgFile', 'submitAdd', 'newWalletErr',
      'newWalletAppPWBox', 'nwRegMsg'
    ])
    this.refresh()

    // WalletConfigForm will set the global app variable.
    this.subform = new WalletConfigForm(application, fields.walletSettings, true)

    bind(form, fields.submitAdd, async () => {
      const pw = fields.nwAppPass.value || (this.pwCache ? this.pwCache.pw : '')
      if (!pw && !State.passwordIsCached()) {
        fields.newWalletErr.textContent = window.locales.formatDetails(ID_NO_APP_PASS_ERROR_MSG)
        Doc.show(fields.newWalletErr)
        return
      }
      Doc.hide(fields.newWalletErr)

      const createForm = {
        assetID: parseInt(this.currentAsset.id),
        pass: fields.newWalletPass.value || '',
        config: this.subform.map(),
        appPass: pw
      }
      fields.nwAppPass.value = ''
      const loaded = app.loading(form)
      const res = await postJSON('/api/newwallet', createForm)
      loaded()
      if (!app.checkResponse(res)) {
        this.setError(res.msg)
        return
      }
      if (this.pwCache) this.pwCache.pw = pw
      fields.newWalletPass.value = ''
      success()
    })
  }

  refresh () {
    const hidePWBox = State.passwordIsCached() || (this.pwCache && this.pwCache.pw)
    if (hidePWBox) Doc.hide(this.fields.newWalletAppPWBox)
    else Doc.show(this.fields.newWalletAppPWBox)
  }

  async setAsset (asset) {
    const fields = this.fields
    if (this.currentAsset && this.currentAsset.id === asset.id) return
    this.currentAsset = asset
    fields.nwAssetLogo.src = Doc.logoPath(asset.symbol)
    fields.nwAssetName.textContent = asset.info.name
    fields.newWalletPass.value = ''
    this.subform.update(asset.info)
    Doc.hide(fields.newWalletErr)
    this.refresh()
  }

  /* setError sets and shows the in-form error message. */
  async setError (errMsg) {
    this.fields.newWalletErr.textContent = errMsg
    Doc.show(this.fields.newWalletErr)
  }

  /*
   * loadDefaults attempts to load the ExchangeWallet configuration from the
   * default wallet config path on the server and will auto-fill the fields on
   * the subform if settings are found.
   */
  async loadDefaults () {
    const loaded = app.loading(this.form)
    const res = await postJSON('/api/defaultwalletcfg', { assetID: this.currentAsset.id })
    loaded()
    if (!app.checkResponse(res)) {
      this.setError(res.msg)
      return
    }
    this.subform.setLoadedConfig(res.config)
  }

  setRegMsg (msg) {
    this.fields.nwRegMsg.textContent = msg
  }
}

/*
 * WalletConfigForm is a dynamically generated sub-form for setting
 * asset-specific wallet configuration options.
*/
export class WalletConfigForm {
  constructor (application, form, sectionize) {
    app = application
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
    const loaded = app.loading(this.form)
    const config = await this.fileInput.files[0].text()
    if (!config) return
    const res = await postJSON('/api/parseconfig', {
      configtext: config
    })
    loaded()
    if (!app.checkResponse(res)) {
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
  update (walletInfo) {
    this.configElements = {}
    this.configOpts = walletInfo.configopts
    Doc.empty(this.dynamicOpts, this.otherSettings)
    this.setOtherSettingsViz(false)
    Doc.hide(
      this.loadedSettingsMsg, this.loadedSettings,
      this.defaultSettingsMsg, this.defaultSettings,
      this.errMsg
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
      for (const opt of defaultedOpts) addOpt(this.defaultSettings, opt)
      Doc.show(this.showOther, this.defaultSettingsMsg, this.defaultSettings)
    } else {
      Doc.hide(this.showOther)
    }
    app.bindTooltips(this.allSettings)
  }

  /*
   * setOtherSettingsViz sets the visibility of the additional settings section.
   */
  setOtherSettingsViz (visible) {
    if (visible) {
      Doc.hide(this.showIcon)
      Doc.show(this.hideIcon, this.otherSettings)
      this.showHideMsg.textContent = window.locales.formatDetails(ID_HIDE_ADDIIONAL_SETTINGS)
      return
    }
    Doc.hide(this.hideIcon, this.otherSettings)
    Doc.show(this.showIcon)
    this.showHideMsg.textContent = window.locales.formatDetails(ID_SHOW_ADDIIONAL_SETTINGS)
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
  constructor (application, form, { getDexAddr, getCertFile }, success, insufficientFundsFail) {
    this.fields = Doc.parsePage(form, [
      'feeDisplay', 'marketRowTemplate', 'marketsTableRows', 'appPass', 'appPassBox',
      'appPassSpan', 'submitConfirm', 'regErr'
    ])
    app = application
    this.getDexAddr = getDexAddr
    this.getCertFile = getCertFile
    this.success = success
    this.insufficientFundsFail = insufficientFundsFail
    this.form = form
    bind(form, this.fields.submitConfirm, () => this.submitForm())
  }

  /*
   * setExchange populates the form with the details of an exchange.
   */
  setExchange (xc) {
    const fields = this.fields
    this.fee = xc.feeAsset.amount
    fields.feeDisplay.textContent = Doc.formatCoinValue(this.fee / 1e8)
    while (fields.marketsTableRows.firstChild) {
      fields.marketsTableRows.removeChild(fields.marketsTableRows.firstChild)
    }
    const markets = Object.values(xc.markets)
    markets.sort((m1, m2) => {
      const compareBase = m1.basesymbol.localeCompare(m2.basesymbol)
      const compareQuote = m1.quotesymbol.localeCompare(m2.quotesymbol)
      return compareBase === 0 ? compareQuote : compareBase
    })
    markets.forEach((market) => {
      const tr = fields.marketRowTemplate.cloneNode(true)
      Doc.tmplElement(tr, 'baseicon').src = Doc.logoPath(market.basesymbol)
      Doc.tmplElement(tr, 'quoteicon').src = Doc.logoPath(market.quotesymbol)
      Doc.tmplElement(tr, 'base').innerText = market.basesymbol.toUpperCase()
      Doc.tmplElement(tr, 'quote').innerText = market.quotesymbol.toUpperCase()
      Doc.tmplElement(tr, 'lotsize').innerText = `${market.lotsize / 1e8} ${market.basesymbol.toUpperCase()}`
      fields.marketsTableRows.appendChild(tr)
      if (State.passwordIsCached()) {
        Doc.hide(fields.appPassBox)
        Doc.hide(fields.appPassSpan)
      } else {
        Doc.show(fields.appPassBox)
        Doc.show(fields.appPassSpan)
      }
    })
  }

  /*
   * submitForm is called when the form is submitted.
   */
  async submitForm () {
    const fields = this.fields
    Doc.hide(fields.regErr)
    const cert = await this.getCertFile()
    const dexAddr = this.getDexAddr()
    const registration = {
      addr: dexAddr,
      pass: fields.appPass.value,
      fee: this.fee,
      cert: cert
    }
    fields.appPass.value = ''
    const loaded = app.loading(this.form)
    const res = await postJSON('/api/register', registration)
    if (!app.checkResponse(res)) {
      // This form is used both in the register workflow and the
      // settings page. The register workflow handles a failure
      // where the user does not have enough funds to pay for the
      // registration fee in a different way.
      if (res.code === feeSendErr && this.insufficientFundsFail) {
        loaded()
        this.insufficientFundsFail(res.msg)
        return
      }
      fields.regErr.textContent = res.msg
      Doc.show(fields.regErr)
      loaded()
      return
    }
    loaded()
    this.success()
  }
}

export class UnlockWalletForm {
  constructor (application, form, success, pwCache) {
    this.fields = Doc.parsePage(form, [
      'uwAssetLogo', 'uwAssetName', 'uwAppPassBox', 'uwAppPass', 'submitUnlock',
      'unlockErr', 'submitUnlockDiv'
    ])
    app = application
    this.form = form
    this.pwCache = pwCache
    this.currentAsset = null
    this.success = success
    bind(form, this.fields.submitUnlock, () => this.submit())
  }

  setAsset (asset) {
    const fields = this.fields
    this.currentAsset = asset
    fields.uwAssetLogo.src = Doc.logoPath(asset.symbol)
    fields.uwAssetName.textContent = asset.info.name
    fields.uwAppPass.value = ''
    const hidePWBox = State.passwordIsCached() || (this.pwCache && this.pwCache.pw)
    if (hidePWBox) Doc.hide(fields.uwAppPassBox)
    else Doc.show(fields.uwAppPassBox)
  }

  /*
   * setError displays an error on the form.
   */
  setError (msg) {
    this.fields.unlockErr.textContent = msg
    Doc.show(this.fields.unlockErr)
  }

  /*
   * showErrorOnly displays only an error on the form. Hides the
   * app pass field and the submit button.
   */
  showErrorOnly (msg) {
    this.setError(msg)
    Doc.hide(this.fields.uwAppPassBox)
    Doc.hide(this.fields.submitUnlockDiv)
  }

  async submit () {
    const fields = this.fields
    const pw = fields.uwAppPass.value || (this.pwCache ? this.pwCache.pw : '')
    if (!pw && !State.passwordIsCached()) {
      fields.unlockErr.textContent = window.locales.formatDetails(ID_NO_APP_PASS_ERROR_MSG)
      Doc.show(fields.unlockErr)
      return
    }
    Doc.hide(this.fields.unlockErr)
    const open = {
      assetID: parseInt(this.currentAsset.id),
      pass: pw
    }
    fields.uwAppPass.value = ''
    const loaded = app.loading(this.form)
    const res = await postJSON('/api/openwallet', open)
    loaded()
    if (!app.checkResponse(res)) {
      this.setError(res.msg)
      return
    }
    if (this.pwCache) this.pwCache.pw = pw
    this.success()
  }
}

export class DEXAddressForm {
  constructor (application, form, success, pwCache) {
    app = application
    this.form = form
    this.success = success
    this.pwCache = pwCache
    this.defaultTLSText = 'none selected'

    const page = this.page = Doc.parsePage(form, [
      'dexAddr', 'dexShowMore', 'dexCertBox', 'dexNeedCert', 'certFile',
      'removeCert', 'addCert', 'selectedCert', 'dexAddrAppPWBox', 'dexAddrAppPW',
      'submitDEXAddr', 'dexAddrErr'
    ])

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

    const loaded = app.loading(this.form)

    const res = await postJSON('/api/preregister', {
      addr: addr,
      cert: cert,
      pass: pw
    })
    loaded()
    if (!app.checkResponse(res, true)) {
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
      await app.fetchUser()
      app.loadPage('markets')
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
