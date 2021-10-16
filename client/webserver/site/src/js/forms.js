import { app } from './registry'
import Doc from './doc'
import { postJSON } from './http'
import State from './state'
import * as intl from './locales'
import { RateEncodingFactor } from './orderutil'

/*
 * NewWalletForm should be used with the "newWalletForm" template. The enclosing
 * <form> element should be the second argument of the constructor.
 */
export class NewWalletForm {
  constructor (form, success, pwCache, backFunc) {
    this.form = form
    this.currentAsset = null
    this.pwCache = pwCache
    const page = this.page = Doc.idDescendants(form)
    this.pwHiders = Array.from(form.querySelectorAll('.hide-pw'))
    this.refresh()

    if (backFunc) {
      Doc.show(page.nwGoBack)
      Doc.bind(page.nwGoBack, 'click', () => { backFunc() })
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
    if (hidePWBox) Doc.hide(...this.pwHiders)
    else Doc.show(...this.pwHiders)
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
 * ConfirmRegistrationForm should be used with the "confirmRegistrationForm"
 * template.
 */
export class ConfirmRegistrationForm {
  constructor (form, success, goBack, pwCache) {
    this.form = form
    this.success = success
    this.page = Doc.parseTemplate(form)
    this.xc = null
    this.certFile = ''
    this.feeAssetID = null
    this.pwCache = pwCache
    this.syncWaiters = {}

    Doc.bind(this.page.goBack, 'click', () => goBack())
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

  setExchange (xc, certFile) {
    this.xc = xc
    this.certFile = certFile
    const page = this.page
    if (State.passwordIsCached() || (this.pwCache && this.pwCache.pw)) Doc.hide(page.passBox)
    else Doc.show(page.passBox)
    page.host.textContent = xc.host
  }

  setAsset (assetID) {
    const asset = app().assets[assetID]
    const unitInfo = asset.info.unitinfo
    this.feeAssetID = asset.id
    const page = this.page
    const regAsset = this.xc.regFees[asset.symbol]
    const s = Doc.formatCoinValue(regAsset.amount, unitInfo)
    page.fee.textContent = `${s} ${unitInfo.conventional.unit}`
    page.logo.src = Doc.logoPath(asset.symbol)
  }

  /* Form expands into its space quickly from the lower-right as it fades in. */
  async animate () {
    const form = this.form
    Doc.animate(400, prog => {
      form.style.transform = `scale(${prog})`
      form.style.opacity = Math.pow(prog, 4)
      const offset = `${(1 - prog) * 500}px`
      form.style.top = offset
      form.style.left = offset
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
    if (!page.submit.classList.contains('selected')) {
      return
    }
    if (this.feeAssetID === null) {
      page.regErr.innerText = 'You must select a valid wallet for the fee payment'
      Doc.show(page.regErr)
      return
    }
    const symbol = app().user.assets[this.feeAssetID].wallet.symbol
    Doc.hide(page.regErr)
    const feeAsset = this.xc.regFees[symbol]
    const cert = await this.certFile
    const dexAddr = this.xc.host
    const pw = page.appPass.value || (this.pwCache ? this.pwCache.pw : '')
    const registration = {
      addr: dexAddr,
      pass: pw,
      fee: feeAsset.amount,
      asset: feeAsset.id,
      cert: cert
    }
    page.appPass.value = ''
    const loaded = app().loading(this.form)
    const res = await postJSON('/api/register', registration)
    loaded()
    if (!app().checkResponse(res)) {
      page.regErr.textContent = res.msg
      Doc.show(page.regErr)
      return
    }
    this.success()
  }
}

/*
 * FeeAssetSelectionForm should be used with the "regAssetForm" template.
 */
export class FeeAssetSelectionForm {
  constructor (form, success) {
    this.form = form
    this.success = success
    this.xc = null
    this.page = Doc.parseTemplate(form)
    Doc.cleanTemplates(this.page.marketTmpl, this.page.assetTmpl)
  }

  setExchange (xc) {
    this.xc = xc
    const page = this.page
    Doc.empty(page.assets)
    for (const [symbol, feeAsset] of Object.entries(xc.regFees)) {
      const asset = app().assets[feeAsset.id]
      if (!asset) continue // We don't support this asset
      const unitInfo = asset.info.unitinfo
      const assetNode = page.assetTmpl.cloneNode(true)
      Doc.bind(assetNode, 'click', () => { this.success(feeAsset.id) })
      const assetTmpl = Doc.parseTemplate(assetNode)
      page.assets.appendChild(assetNode)
      assetTmpl.logo.src = Doc.logoPath(symbol)
      const fee = Doc.formatCoinValue(feeAsset.amount, unitInfo)
      assetTmpl.fee.textContent = `${fee} ${unitInfo.conventional.unit}`
      assetTmpl.confs.textContent = feeAsset.confs
      assetTmpl.ready.textContent = asset.wallet ? intl.prep(intl.WALLET_READY) : intl.prep(intl.SETUP_NEEDED)
      assetTmpl.ready.classList.add(asset.wallet ? 'readygreen' : 'setuporange')

      let count = 0
      for (const mkt of Object.values(xc.markets)) {
        if (mkt.baseid !== feeAsset.id && mkt.quoteid !== feeAsset.id) continue
        count++
        const marketNode = page.marketTmpl.cloneNode(true)
        assetTmpl.markets.appendChild(marketNode)

        const marketTmpl = Doc.parseTemplate(marketNode)

        const isBase = mkt.baseid === feeAsset.id
        const otherAsset = app().assets[isBase ? mkt.quoteid : mkt.baseid]
        if (!otherAsset) continue // not supported
        const baseAsset = app().assets[mkt.baseid]
        const quoteAsset = app().assets[mkt.quoteid]
        const baseSymbol = baseAsset.symbol.toUpperCase()
        const quoteSymbol = quoteAsset.symbol.toUpperCase()

        marketTmpl.logo.src = Doc.logoPath(otherAsset.symbol)
        marketTmpl.name.textContent = `${baseSymbol}-${quoteSymbol}`
        const s = Doc.formatCoinValue(mkt.lotsize, baseAsset.info.unitinfo) // TODO: Use UnitInfo
        marketTmpl.lotSize.textContent = `${s} ${baseSymbol}`

        if (!isBase && mkt.spot) {
          Doc.show(marketTmpl.quoteLotSize)
          const cFactor = asset => asset.info.unitinfo.conventional.conversionFactor
          const r = cFactor(quoteAsset) / cFactor(baseAsset)
          const quoteLot = mkt.lotsize * mkt.spot.rate / RateEncodingFactor * r
          const s = Doc.formatCoinValue(quoteLot, quoteAsset.info.unitinfo)
          marketTmpl.quoteLotSize.textContent = `(~${s} ${otherAsset.symbol.toUpperCase()})`
        }
      }
      if (count < 3) Doc.hide(assetTmpl.fader)
    }
  }

  refresh () {
    this.setExchange(this.xc)
  }

  /*
   * Animation to make the elements sort of expand into their space from the
   * bottom as they fade in.
   */
  async animate () {
    const { page, form } = this
    const how = page.how
    const extraMargin = 75
    const extraTop = 50
    const fontSize = 24
    const regAssetElements = Array.from(page.assets.children)
    form.style.opacity = '0'

    const aniLen = 350
    await Doc.animate(aniLen, prog => {
      for (const el of regAssetElements) {
        el.style.marginTop = `${(1 - prog) * extraMargin}px`
        el.style.transform = `scale(${prog})`
      }
      form.style.opacity = Math.pow(prog, 4).toFixed(1)
      form.style.paddingTop = `${(1 - prog) * extraTop}px`
      how.style.fontSize = `${fontSize * prog}px`
    }, 'easeOut')
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

/* DEXAddressForm accepts a DEX address and performs account discovery. */
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

  /* Just a small size tweak and fade-in. */
  async animate () {
    const form = this.form
    Doc.animate(550, prog => {
      form.style.transform = `scale(${0.9 + 0.1 * prog})`
      form.style.opacity = Math.pow(prog, 4)
    }, 'easeOut')
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
    this.success(res.xc, cert)
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

/* LoginForm is used to sign into the app. */
export class LoginForm {
  constructor (form, success, pwCache) {
    this.success = success
    this.form = form
    this.pwCache = pwCache
    const page = this.page = Doc.parseTemplate(form)
    this.headerTxt = page.header.textContent
    page.header.style.height = `${page.header.offsetHeight}px`

    bind(form, page.submit, () => { this.submit() })
  }

  focus () {
    this.page.pw.focus()
  }

  async submit (e) {
    const page = this.page
    Doc.hide(page.errMsg)
    const pw = page.pw.value
    page.pw.value = ''
    const rememberPass = page.rememberPass.checked
    if (pw === '') {
      page.errMsg.textContent = intl.prep(intl.ID_NO_PASS_ERROR_MSG)
      Doc.show(page.errMsg)
      return
    }
    const loaded = app().loading(this.form)
    const res = await postJSON('/api/login', { pass: pw, rememberPass })
    loaded()
    if (!app().checkResponse(res)) {
      page.errMsg.textContent = res.msg
      Doc.show(page.errMsg)
      return
    }
    if (res.notes) {
      res.notes.reverse()
    }
    app().setNotes(res.notes || [])
    if (this.pwCache) this.pwCache.pw = pw
    this.success()
  }

  /* Just a small size tweak and fade-in. */
  async animate () {
    const form = this.form
    Doc.animate(550, prog => {
      form.style.transform = `scale(${0.9 + 0.1 * prog})`
      form.style.opacity = Math.pow(prog, 4)
    }, 'easeOut')
  }
}

const animationLength = 300

/* Swap form1 for form2 with an animation. */
export async function slideSwap (form1, form2) {
  const shift = document.body.offsetWidth / 2
  await Doc.animate(animationLength, progress => {
    form1.style.right = `${progress * shift}px`
  }, 'easeInHard')
  Doc.hide(form1)
  form1.style.right = '0'
  form2.style.right = -shift
  Doc.show(form2)
  if (form2.querySelector('input')) {
    form2.querySelector('input').focus()
  }
  await Doc.animate(animationLength, progress => {
    form2.style.right = `${-shift + progress * shift}px`
  }, 'easeOutHard')
  form2.style.right = '0'
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
