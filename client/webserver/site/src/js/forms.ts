import Doc from './doc'
import { postJSON } from './http'
import State from './state'
import * as intl from './locales'
import * as OrderUtil from './orderutil'
import {
  app,
  PasswordCache,
  SupportedAsset,
  PageElement,
  WalletDefinition,
  ConfigOption,
  Exchange,
  Market,
  UnitInfo,
  FeeAsset,
  WalletState,
  WalletBalance,
  Order,
  XYRange
} from './registry'

interface ConfigOptionInput extends HTMLInputElement {
  configOpt: ConfigOption
}

interface ProgressPoint {
  stamp: number
  progress: number
}

/*
 * NewWalletForm should be used with the "newWalletForm" template. The enclosing
 * <form> element should be the second argument of the constructor.
 */
export class NewWalletForm {
  page: Record<string, PageElement>
  form: HTMLElement
  pwCache: PasswordCache | null
  success: (assetID: number) => void
  currentAsset: SupportedAsset
  pwHiders: HTMLElement[]
  subform: WalletConfigForm
  currentWalletType: string

  constructor (form: HTMLElement, success: (assetID: number) => void, pwCache?: PasswordCache, backFunc?: () => void) {
    this.form = form
    this.success = success
    this.pwCache = pwCache || null
    const page = this.page = Doc.parseTemplate(form)
    this.pwHiders = Array.from(form.querySelectorAll('.hide-pw'))
    this.refresh()

    if (backFunc) {
      Doc.show(page.goBack)
      Doc.bind(page.goBack, 'click', () => { backFunc() })
    }

    Doc.empty(page.walletTabTmpl)
    page.walletTabTmpl.removeAttribute('id')

    // WalletConfigForm will set the global app variable.
    this.subform = new WalletConfigForm(page.walletSettings, true)

    Doc.bind(this.subform.showOther, 'click', () => Doc.show(page.walletSettingsHeader))

    bind(form, page.submitAdd, () => this.submit())
    bind(form, page.oneBttn, () => this.submit())
  }

  refresh () {
    const hidePWBox = State.passwordIsCached() || (this.pwCache && this.pwCache.pw)
    if (hidePWBox) Doc.hide(...this.pwHiders)
    else Doc.show(...this.pwHiders)
  }

  async submit () {
    const page = this.page
    const appPass = page.appPass as HTMLInputElement
    const newWalletPass = page.newWalletPass as HTMLInputElement
    const pw = appPass.value || (this.pwCache ? this.pwCache.pw : '')
    if (!pw && !State.passwordIsCached()) {
      page.newWalletErr.textContent = intl.prep(intl.ID_NO_APP_PASS_ERROR_MSG)
      Doc.show(page.newWalletErr)
      return
    }
    Doc.hide(page.newWalletErr)
    const assetID = this.currentAsset.id
    const createForm = {
      assetID: assetID,
      pass: newWalletPass.value || '',
      config: this.subform.map(),
      appPass: pw,
      walletType: this.currentWalletType
    }
    appPass.value = ''
    const loaded = app().loading(page.mainForm)
    const res = await postJSON('/api/newwallet', createForm)
    loaded()
    if (!app().checkResponse(res)) {
      this.setError(res.msg)
      return
    }
    if (this.pwCache) this.pwCache.pw = pw
    newWalletPass.value = ''
    this.success(assetID)
  }

  async setAsset (assetID: number) {
    const page = this.page
    const asset = app().assets[assetID]
    const tabs = page.walletTypeTabs
    if (this.currentAsset && this.currentAsset.id === asset.id) return
    this.currentAsset = asset
    page.assetLogo.src = Doc.logoPath(asset.symbol)
    page.assetName.textContent = asset.info.name
    page.newWalletPass.value = ''

    if (asset.info.availablewallets.length > 1) page.header.classList.add('bordertop')
    else page.header.classList.remove('bordertop')

    const walletDef = asset.info.availablewallets[0]
    Doc.empty(tabs)
    Doc.hide(tabs, page.newWalletErr)

    if (asset.info.availablewallets.length > 1) {
      Doc.show(tabs)
      for (const wDef of asset.info.availablewallets) {
        const tab = page.walletTabTmpl.cloneNode(true) as HTMLElement
        tab.dataset.tooltip = wDef.description
        tab.textContent = wDef.tab
        tabs.appendChild(tab)
        Doc.bind(tab, 'click', () => {
          for (const t of Doc.kids(tabs)) t.classList.remove('selected')
          tab.classList.add('selected')
          this.update(wDef)
        })
      }
      app().bindTooltips(tabs)
      const first = tabs.firstChild as HTMLElement
      first.classList.add('selected')
    }

    await this.update(walletDef)
  }

  async update (walletDef: WalletDefinition) {
    const page = this.page
    this.currentWalletType = walletDef.type
    const appPwCached = State.passwordIsCached() || (this.pwCache && this.pwCache.pw)
    Doc.hide(page.auth, page.oneBttnBox, page.newWalletPassBox)
    const configOpts = walletDef.configopts || []
    // If a config represents a wallet's birthday, we update the default
    // selection to the current date if this installation of the client
    // generated a seed.
    configOpts.map((opt) => {
      if (opt.isBirthdayConfig && app().seedGenTime > 0) {
        opt.default = toUnixDate(new Date())
      }
      return opt
    })
    if (appPwCached && walletDef.seeded) {
      Doc.show(page.oneBttnBox)
    } else if (walletDef.seeded) {
      Doc.show(page.auth)
      page.newWalletPass.value = ''
      page.submitAdd.textContent = intl.prep(intl.ID_CREATE)
    } else {
      Doc.show(page.auth, page.newWalletPassBox)
      page.submitAdd.textContent = intl.prep(intl.ID_ADD)
    }

    this.subform.update(configOpts)

    if (this.subform.dynamicOpts.children.length) Doc.show(page.walletSettingsHeader)
    else Doc.hide(page.walletSettingsHeader)

    this.refresh()
    await this.loadDefaults()
  }

  /* setError sets and shows the in-form error message. */
  async setError (errMsg: string) {
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
  form: HTMLElement
  configElements: Record<string, HTMLElement>
  configOpts: ConfigOption[]
  sectionize: boolean
  allSettings: PageElement
  dynamicOpts: PageElement
  textInputTmpl: PageElement
  dateInputTmpl: PageElement
  checkboxTmpl: PageElement
  fileSelector: PageElement
  fileInput: PageElement
  errMsg: PageElement
  showOther: PageElement
  showIcon: PageElement
  hideIcon: PageElement
  showHideMsg: PageElement
  otherSettings: PageElement
  loadedSettingsMsg: PageElement
  loadedSettings: PageElement
  defaultSettingsMsg: PageElement
  defaultSettings: PageElement

  constructor (form: HTMLElement, sectionize: boolean) {
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
    this.dateInputTmpl = Doc.tmplElement(form, 'dateInput')
    this.dateInputTmpl.remove()
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
    const files = this.fileInput.files
    if (!files || files.length === 0) return
    const loaded = app().loading(this.form)
    const config = await files[0].text()
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
  update (configOpts: ConfigOption[], assetHasActiveOrders?: boolean) {
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
    const addOpt = (box: HTMLElement, opt: ConfigOption) => {
      const elID = 'wcfg-' + opt.key
      let el: HTMLElement
      if (opt.isboolean) el = this.checkboxTmpl.cloneNode(true) as HTMLElement
      else if (opt.isdate) el = this.dateInputTmpl.cloneNode(true) as HTMLElement
      else el = this.textInputTmpl.cloneNode(true) as HTMLElement
      this.configElements[opt.key] = el
      const input = el.querySelector('input') as ConfigOptionInput
      input.id = elID
      input.configOpt = opt
      const label = Doc.safeSelector(el, 'label')
      label.htmlFor = elID // 'for' attribute, but 'for' is a keyword
      label.prepend(opt.displayname)
      box.appendChild(el)
      if (opt.noecho) input.type = 'password'
      if (opt.description) label.dataset.tooltip = opt.description
      if (opt.isboolean) input.checked = opt.default
      else if (opt.isdate) {
        const getMinMaxVal = (minMax: string | number) => {
          if (!minMax) return ''
          if (minMax === 'now') return dateToString(new Date())
          return dateToString(new Date((minMax as number) * 1000))
        }
        input.max = getMinMaxVal(opt.max)
        input.min = getMinMaxVal(opt.min)
        input.valueAsDate = opt.default ? new Date(opt.default * 1000) : new Date()
      } else input.value = opt.default !== null ? opt.default : ''
      input.disabled = Boolean(opt.disablewhenactive && assetHasActiveOrders)
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
  setOtherSettingsViz (visible: boolean) {
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
  setConfig (cfg: Record<string, string>) {
    const finds: HTMLElement[] = []
    this.allSettings.querySelectorAll('input').forEach((input: ConfigOptionInput) => {
      const k = input.configOpt.key
      const v = cfg[k]
      if (typeof v === 'undefined') return
      finds.push(this.configElements[k])
      if (input.configOpt.isboolean) input.checked = isTruthyString(v)
      else if (input.configOpt.isdate) input.valueAsDate = new Date(parseInt(v) * 1000)
      else input.value = v
    })
    return finds
  }

  /*
   * setLoadedConfig sets the input values for the entries in cfg, and moves
   * them to the loadedSettings box.
   */
  setLoadedConfig (cfg: Record<string, string>) {
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
  map (): Record<string, string> {
    const config: Record<string, string> = {}
    this.allSettings.querySelectorAll('input').forEach((input: ConfigOptionInput) => {
      if (input.configOpt.isboolean && input.configOpt.key) {
        config[input.configOpt.key] = input.checked ? '1' : '0'
      } else if (input.configOpt.isdate && input.configOpt.key) {
        const minDate = input.min ? toUnixDate(new Date(input.min)) : Number.MIN_SAFE_INTEGER
        const maxDate = input.max ? toUnixDate(new Date(input.max)) : Number.MAX_SAFE_INTEGER
        let date = input.value ? toUnixDate(new Date(input.value)) : 0
        if (date < minDate) date = minDate
        else if (date > maxDate) date = maxDate
        config[input.configOpt.key] = '' + date
      } else if (input.value) {
        config[input.configOpt.key] = input.value
      }
    })

    return config
  }

  /*
   * reorder sorts the configElements in the box by the order of the
   * server-provided configOpts array.
   */
  reorder (box: HTMLElement) {
    const inputs: Record<string, HTMLElement> = {}
    box.querySelectorAll('input').forEach((input: ConfigOptionInput) => {
      const k = input.configOpt.key
      inputs[k] = this.configElements[k]
    })
    for (const opt of this.configOpts) {
      const input = inputs[opt.key]
      if (input) box.append(input)
    }
  }
}

/*
 * ConfirmRegistrationForm should be used with the "confirmRegistrationForm"
 * template.
 */
export class ConfirmRegistrationForm {
  form: HTMLElement
  success: () => void
  page: Record<string, PageElement>
  xc: Exchange
  certFile: string
  feeAssetID: number
  pwCache: PasswordCache

  constructor (form: HTMLElement, success: () => void, goBack: () => void, pwCache: PasswordCache) {
    this.form = form
    this.success = success
    this.page = Doc.parseTemplate(form)
    this.certFile = ''
    this.pwCache = pwCache

    Doc.bind(this.page.goBack, 'click', () => goBack())
    bind(form, this.page.submit, () => this.submitForm())
  }

  setExchange (xc: Exchange, certFile: string) {
    this.xc = xc
    this.certFile = certFile
    const page = this.page
    if (State.passwordIsCached() || (this.pwCache && this.pwCache.pw)) Doc.hide(page.passBox)
    else Doc.show(page.passBox)
    page.host.textContent = xc.host
  }

  setAsset (assetID: number) {
    const asset = app().assets[assetID]
    const unitInfo = asset.info.unitinfo
    this.feeAssetID = asset.id
    const page = this.page
    const regAsset = this.xc.regFees[asset.symbol]
    page.fee.textContent = Doc.formatCoinValue(regAsset.amount, unitInfo)
    page.feeUnit.textContent = unitInfo.conventional.unit.toUpperCase()
    page.logo.src = Doc.logoPath(asset.symbol)
  }

  /* Form expands into its space quickly from the lower-right as it fades in. */
  async animate () {
    const form = this.form
    Doc.animate(400, prog => {
      form.style.transform = `scale(${prog})`
      form.style.opacity = String(Math.pow(prog, 4))
      const offset = `${(1 - prog) * 500}px`
      form.style.top = offset
      form.style.left = offset
    })
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
  form: HTMLElement
  success: (assetID: number) => void
  xc: Exchange
  page: Record<string, PageElement>

  constructor (form: HTMLElement, success: (assetID: number) => void) {
    this.form = form
    this.success = success
    this.page = Doc.parseTemplate(form)
    Doc.cleanTemplates(this.page.marketTmpl, this.page.assetTmpl)
  }

  setExchange (xc: Exchange) {
    this.xc = xc
    const page = this.page
    Doc.empty(page.assets, page.allMarkets)

    const cFactor = (ui: UnitInfo) => ui.conventional.conversionFactor

    const marketNode = (mkt: Market, excludeIcon?: number) => {
      const n = page.marketTmpl.cloneNode(true) as HTMLElement
      const marketTmpl = Doc.parseTemplate(n)

      const baseAsset = xc.assets[mkt.baseid]
      const baseUnitInfo = app().unitInfo(mkt.baseid, xc)
      const quoteAsset = xc.assets[mkt.quoteid]
      const quoteUnitInfo = app().unitInfo(mkt.quoteid, xc)

      if (cFactor(baseUnitInfo) === 0 || cFactor(quoteUnitInfo) === 0) return null

      if (typeof excludeIcon !== 'undefined') {
        const excludeBase = excludeIcon === mkt.baseid
        const otherSymbol = xc.assets[excludeBase ? mkt.quoteid : mkt.baseid].symbol
        marketTmpl.logo.src = Doc.logoPath(otherSymbol)
      } else {
        const otherLogo = marketTmpl.logo.cloneNode(true) as PageElement
        marketTmpl.logo.src = Doc.logoPath(baseAsset.symbol)
        otherLogo.src = Doc.logoPath(quoteAsset.symbol)
        const parent = marketTmpl.logo.parentNode
        if (parent) parent.insertBefore(otherLogo, marketTmpl.logo.nextSibling)
      }

      const baseSymbol = baseAsset.symbol.toUpperCase()
      const quoteSymbol = quoteAsset.symbol.toUpperCase()

      marketTmpl.name.textContent = `${baseSymbol}-${quoteSymbol}`
      const s = Doc.formatCoinValue(mkt.lotsize, baseUnitInfo)
      marketTmpl.lotSize.textContent = `${s} ${baseSymbol}`

      if (mkt.spot) {
        Doc.show(marketTmpl.quoteLotSize)
        const r = cFactor(quoteUnitInfo) / cFactor(baseUnitInfo)
        const quoteLot = mkt.lotsize * mkt.spot.rate / OrderUtil.RateEncodingFactor * r
        const s = Doc.formatCoinValue(quoteLot, quoteUnitInfo)
        marketTmpl.quoteLotSize.textContent = `(~${s} ${quoteSymbol})`
      }
      return n
    }

    for (const [symbol, feeAsset] of Object.entries(xc.regFees)) {
      const asset = app().assets[feeAsset.id]
      if (!asset) continue
      const haveWallet = asset.wallet
      const unitInfo = asset.info.unitinfo
      const assetNode = page.assetTmpl.cloneNode(true) as HTMLElement
      Doc.bind(assetNode, 'click', () => { this.success(feeAsset.id) })
      const assetTmpl = Doc.parseTemplate(assetNode)
      page.assets.appendChild(assetNode)
      assetTmpl.logo.src = Doc.logoPath(symbol)
      const fee = Doc.formatCoinValue(feeAsset.amount, unitInfo)
      assetTmpl.fee.textContent = `${fee} ${unitInfo.conventional.unit}`
      assetTmpl.confs.textContent = String(feeAsset.confs)
      assetTmpl.ready.textContent = haveWallet ? intl.prep(intl.WALLET_READY) : intl.prep(intl.SETUP_NEEDED)
      assetTmpl.ready.classList.add(haveWallet ? 'readygreen' : 'setuporange')

      let count = 0
      for (const mkt of Object.values(xc.markets)) {
        if (mkt.baseid !== feeAsset.id && mkt.quoteid !== feeAsset.id) continue
        const node = marketNode(mkt, feeAsset.id)
        if (!node) continue
        count++
        assetTmpl.markets.appendChild(node)
      }
      if (count < 3) Doc.hide(assetTmpl.fader)
    }

    page.host.textContent = xc.host
    for (const mkt of Object.values(xc.markets)) {
      const node = marketNode(mkt)
      if (!node) continue
      page.allMarkets.appendChild(node)
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
    const regAssetElements = Array.from(page.assets.children) as PageElement[]
    regAssetElements.push(page.allmkts)
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

/*
 * WalletWaitForm is a form used to track the wallet sync status and balance
 * in preparation for paying the registration fee.
 */
export class WalletWaitForm {
  form: HTMLElement
  success: () => void
  goBack: () => void
  page: Record<string, PageElement>
  assetID: number
  xc: Exchange
  regFee: FeeAsset
  progressCache: ProgressPoint[]
  progressed: boolean
  funded: boolean

  constructor (form: HTMLElement, success: () => void, goBack: () => void) {
    this.form = form
    this.success = success
    this.page = Doc.parseTemplate(form)
    this.assetID = -1
    this.progressCache = []
    this.progressed = false
    this.funded = false

    Doc.bind(this.page.goBack, 'click', () => {
      this.assetID = -1
      goBack()
    })
  }

  /* setExchange sets the exchange for which the fee is being paid. */
  setExchange (xc: Exchange) {
    this.xc = xc
  }

  /* setWallet must be called before showing the form. */
  setWallet (wallet: WalletState, txFee: number) {
    this.assetID = wallet.assetID
    this.progressCache = []
    this.progressed = false
    this.funded = false
    const page = this.page
    const asset = app().assets[wallet.assetID]
    const fee = this.regFee = this.xc.regFees[asset.symbol]

    for (const span of Doc.applySelector(this.form, '.unit')) span.textContent = asset.symbol.toUpperCase()
    page.logo.src = Doc.logoPath(asset.symbol)
    page.depoAddr.textContent = wallet.address
    page.fee.textContent = Doc.formatCoinValue(fee.amount, asset.info.unitinfo)

    Doc.hide(page.syncUncheck, page.syncCheck, page.balUncheck, page.balCheck, page.syncRemainBox)
    Doc.show(page.balanceBox)

    if (txFee > 0) {
      page.totalFees.textContent = Doc.formatCoinValue(fee.amount + txFee, asset.info.unitinfo)
      Doc.show(page.sendEnoughWithEst)
      Doc.hide(page.sendEnough)
    } else {
      Doc.show(page.sendEnough)
      Doc.hide(page.sendEnoughWithEst)
    }

    Doc.show(wallet.synced ? page.syncCheck : wallet.syncProgress >= 1 ? page.syncSpinner : page.syncUncheck)
    Doc.show(wallet.balance.available > fee.amount ? page.balCheck : page.balUncheck)

    page.progress.textContent = String(Math.round(wallet.syncProgress * 100))

    if (wallet.synced) {
      this.progressed = true
    }
    this.reportBalance(wallet.balance, wallet.assetID)
  }

  /*
   * reportWalletState sets the progress and balance, ultimately calling the
   * success function if conditions are met.
   */
  reportWalletState (wallet: WalletState) {
    if (wallet.assetID !== this.assetID) return
    if (this.progressed && this.funded) return
    this.reportProgress(wallet.synced, wallet.syncProgress)
    this.reportBalance(wallet.balance, wallet.assetID)
  }

  /*
   * reportBalance sets the balance display and calls success if we go over the
   * threshold.
   */
  reportBalance (bal: WalletBalance, assetID: number) {
    if (this.funded || this.assetID === -1 || this.assetID !== assetID) return
    const page = this.page
    const asset = app().assets[this.assetID]

    if (bal.available <= this.regFee.amount) {
      page.balance.textContent = Doc.formatCoinValue(bal.available, asset.info.unitinfo)
      return
    }

    Doc.show(page.balCheck)
    Doc.hide(page.balUncheck, page.balanceBox, page.sendEnough)
    this.funded = true

    if (this.progressed) this.success()
  }

  /*
   * reportProgress sets the progress display and calls success if we are fully
   * synced.
   */
  reportProgress (synced: boolean, prog: number) {
    const page = this.page
    if (synced) {
      page.progress.textContent = '100'
      Doc.hide(page.syncUncheck, page.syncRemainBox, page.syncSpinner)
      Doc.show(page.syncCheck)
      this.progressed = true
      if (this.funded) this.success()
      return
    } else if (prog === 1) {
      Doc.hide(page.syncUncheck)
      Doc.show(page.syncSpinner)
    } else {
      Doc.hide(page.syncSpinner)
      Doc.show(page.syncUncheck)
    }
    page.progress.textContent = String(Math.round(prog * 100))

    // The remaining time estimate must be based on more than one progress
    // report. We'll cache up to the last 20 and look at the difference between
    // the first and last to make the estimate.
    const cacheSize = 20
    const cache = this.progressCache
    cache.push({
      stamp: new Date().getTime(),
      progress: prog
    })
    while (cache.length > cacheSize) cache.shift()
    if (cache.length === 1) return
    Doc.show(page.syncRemainBox)
    const [first, last] = [cache[0], cache[cache.length - 1]]
    const progDelta = last.progress - first.progress
    if (progDelta === 0) {
      page.syncRemain.textContent = '> 1 day'
      return
    }
    const timeDelta = last.stamp - first.stamp
    const progRate = progDelta / timeDelta
    const toGoProg = 1 - last.progress
    const toGoTime = toGoProg / progRate
    page.syncRemain.textContent = Doc.formatDuration(toGoTime)
  }
}

export class UnlockWalletForm {
  form: HTMLElement
  success: () => void
  pwCache: PasswordCache | null
  page: Record<string, PageElement>
  currentAsset: SupportedAsset

  constructor (form: HTMLElement, success: () => void, pwCache?: PasswordCache) {
    this.page = Doc.idDescendants(form)
    this.form = form
    this.pwCache = pwCache || null
    this.success = success
    bind(form, this.page.submitUnlock, () => this.submit())
  }

  setAsset (asset: SupportedAsset) {
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
  setError (msg: string) {
    this.page.unlockErr.textContent = msg
    Doc.show(this.page.unlockErr)
  }

  /*
   * showErrorOnly displays only an error on the form. Hides the
   * app pass field and the submit button.
   */
  showErrorOnly (msg: string) {
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
      assetID: this.currentAsset.id,
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

interface EarlyAcceleration {
  timePast: number,
  wasAcceleration: boolean
}

interface PreAccelerate {
  swapRate: number
  suggestedRate: number
  suggestedRange: XYRange
  earlyAcceleration?: EarlyAcceleration
}

/*
 * AccelerateOrderForm is used to submit an acceleration request for an order.
 */
export class AccelerateOrderForm {
  form: HTMLElement
  page: Record<string, PageElement>
  order: Order
  acceleratedRate: number
  earlyAcceleration?: EarlyAcceleration
  currencyUnit: string
  success: () => void

  constructor (form: HTMLElement, success: () => void) {
    this.form = form
    this.success = success
    const page = this.page = Doc.idDescendants(form)

    Doc.bind(page.accelerateSubmit, 'click', () => {
      this.submit()
    })
    Doc.bind(page.submitEarlyConfirm, 'click', () => {
      this.sendAccelerateRequest()
    })
  }

  /*
   * displayEarlyAccelerationMsg displays a message asking for confirmation
   * when the user tries to submit an acceleration transaction very soon after
   * the swap transaction was broadcast, or very soon after a previous
   * acceleration.
   */
  displayEarlyAccelerationMsg () {
    const page = this.page
    // this is checked in submit, but another check is needed for ts compiler
    if (!this.earlyAcceleration) return
    page.recentAccelerationTime.textContent = `${Math.floor(this.earlyAcceleration.timePast / 60)}`
    page.recentSwapTime.textContent = `${Math.floor(this.earlyAcceleration.timePast / 60)}`
    if (this.earlyAcceleration.wasAcceleration) {
      Doc.show(page.recentAccelerationMsg)
      Doc.hide(page.recentSwapMsg)
      page.recentAccelerationTime.textContent = `${Math.floor(this.earlyAcceleration.timePast / 60)}`
    } else {
      Doc.show(page.recentSwapMsg)
      Doc.hide(page.recentAccelerationMsg)
      page.recentSwapTime.textContent = `${Math.floor(this.earlyAcceleration.timePast / 60)}`
    }
    Doc.hide(page.configureAccelerationDiv, page.accelerateErr)
    Doc.show(page.earlyAccelerationDiv)
  }

  // sendAccelerateRequest makes an accelerateorder request to the client
  // backend.
  async sendAccelerateRequest () {
    const order = this.order
    const page = this.page
    const req = {
      pw: page.acceleratePass.value,
      orderID: order.id,
      newRate: this.acceleratedRate
    }
    page.acceleratePass.value = ''
    const loaded = app().loading(page.accelerateMainDiv)
    const res = await postJSON('/api/accelerateorder', req)
    loaded()
    if (app().checkResponse(res)) {
      page.accelerateTxID.textContent = res.txID
      Doc.hide(page.accelerateMainDiv, page.preAccelerateErr, page.accelerateErr)
      Doc.show(page.accelerateMsgDiv, page.accelerateSuccess)
      this.success()
    } else {
      page.accelerateErr.textContent = `Error accelerating order: ${res.msg}`
      Doc.hide(page.earlyAccelerationDiv)
      Doc.show(page.accelerateErr, page.configureAccelerationDiv)
    }
  }

  // submit is called when the submit button is clicked.
  async submit () {
    if (this.earlyAcceleration) {
      this.displayEarlyAccelerationMsg()
    } else {
      this.sendAccelerateRequest()
    }
  }

  // refresh should be called before the form is displayed. It makes a
  // preaccelerate request to the client backend and sets up the form
  // based on the results.
  async refresh (order: Order) {
    const page = this.page
    this.order = order
    const res = await postJSON('/api/preaccelerate', order.id)
    if (!app().checkResponse(res)) {
      page.preAccelerateErr.textContent = `Error accelerating order: ${res.msg}`
      Doc.hide(page.accelerateMainDiv, page.accelerateSuccess)
      Doc.show(page.accelerateMsgDiv, page.preAccelerateErr)
      return
    }
    Doc.hide(page.accelerateMsgDiv, page.preAccelerateErr, page.accelerateErr, page.feeEstimateDiv, page.earlyAccelerationDiv)
    Doc.show(page.accelerateMainDiv, page.accelerateSuccess, page.configureAccelerationDiv)
    const preAccelerate: PreAccelerate = res.preAccelerate
    this.earlyAcceleration = preAccelerate.earlyAcceleration
    this.currencyUnit = preAccelerate.suggestedRange.yUnit
    page.accelerateAvgFeeRate.textContent = `${preAccelerate.swapRate} ${preAccelerate.suggestedRange.yUnit}`
    page.accelerateCurrentFeeRate.textContent = `${preAccelerate.suggestedRate} ${preAccelerate.suggestedRange.yUnit}`
    OrderUtil.setOptionTemplates(page)
    this.acceleratedRate = preAccelerate.suggestedRange.start.y
    const selected = () => { /* do nothing */ }
    const roundY = true
    const updateRate = (_: number, newY: number) => { this.acceleratedRate = newY }
    const rangeHandler = new OrderUtil.XYRangeHandler(preAccelerate.suggestedRange,
      preAccelerate.suggestedRange.start.x, updateRate, () => this.updateAccelerationEstimate(), selected, roundY)
    Doc.empty(page.sliderContainer)
    page.sliderContainer.appendChild(rangeHandler.control)
    this.updateAccelerationEstimate()
  }

  // updateAccelerationEstimate makes an accelerateestimate request to the
  // client backend using the curretly selected rate on the slider, and
  // displays the results.
  async updateAccelerationEstimate () {
    const page = this.page
    const order = this.order
    const req = {
      orderID: order.id,
      newRate: this.acceleratedRate
    }
    const loaded = app().loading(page.sliderContainer)
    const res = await postJSON('/api/accelerationestimate', req)
    loaded()
    if (!app().checkResponse(res)) {
      page.accelerateErr.textContent = `Error estimating acceleration fee: ${res.msg}`
      Doc.show(page.accelerateErr)
      return
    }
    page.feeRateEstimate.textContent = `${this.acceleratedRate} ${this.currencyUnit}`
    let assetID
    let assetSymbol
    if (order.sell) {
      assetID = order.baseID
      assetSymbol = order.baseSymbol
    } else {
      assetID = order.quoteID
      assetSymbol = order.quoteSymbol
    }
    const unitInfo = app().unitInfo(assetID)
    page.feeEstimate.textContent = `${res.fee / unitInfo.conventional.conversionFactor} ${assetSymbol}`
    Doc.show(page.feeEstimateDiv)
  }
}

/* DEXAddressForm accepts a DEX address and performs account discovery. */
export class DEXAddressForm {
  form: HTMLElement
  success: (xc: Exchange, cert: string) => void
  pwCache: PasswordCache | null
  defaultTLSText: string
  page: Record<string, PageElement>
  knownExchanges: HTMLElement[]

  constructor (form: HTMLElement, success: (xc: Exchange, cert: string) => void, pwCache?: PasswordCache) {
    this.form = form
    this.success = success
    this.pwCache = pwCache || null
    this.defaultTLSText = 'none selected'

    const page = this.page = Doc.parseTemplate(form)

    page.selectedCert.textContent = this.defaultTLSText
    Doc.bind(page.certFile, 'change', () => this.onCertFileChange())
    Doc.bind(page.removeCert, 'click', () => this.clearCertFile())
    Doc.bind(page.addCert, 'click', () => page.certFile.click())
    Doc.bind(page.showCustom, 'click', () => {
      Doc.hide(page.showCustom)
      Doc.show(page.customBox, page.auth)
    })

    this.knownExchanges = Array.from(page.knownXCs.querySelectorAll('.known-exchange'))
    for (const div of this.knownExchanges) {
      Doc.bind(div, 'click', () => {
        const host = div.dataset.host
        for (const d of this.knownExchanges) d.classList.remove('selected')
        // If we have the password cached, we're good to go.
        if (State.passwordIsCached() || (pwCache && pwCache.pw)) return this.checkDEX(host)
        // Highlight the entry, but the user will have to enter their password
        // and click submit.
        div.classList.add('selected')
        page.appPW.focus()
        page.addr.value = host
      })
    }

    bind(form, page.submit, () => this.checkDEX())
    this.refresh()
  }

  refresh () {
    const page = this.page
    page.addr.value = ''
    const hidePWBox = State.passwordIsCached() || (this.pwCache && this.pwCache.pw)
    if (hidePWBox) Doc.hide(page.appPWBox, page.auth)
    else Doc.show(page.appPWBox, page.auth)
    Doc.hide(page.customBox)
    Doc.show(page.showCustom)
    for (const div of this.knownExchanges) div.classList.remove('selected')
  }

  /* Just a small size tweak and fade-in. */
  async animate () {
    const form = this.form
    Doc.animate(550, prog => {
      form.style.transform = `scale(${0.9 + 0.1 * prog})`
      form.style.opacity = String(Math.pow(prog, 4))
    }, 'easeOut')
  }

  async checkDEX (addr?: string) {
    const page = this.page
    Doc.hide(page.err)
    addr = addr || page.addr.value
    if (addr === '') {
      page.err.textContent = 'DEX address cannot be empty'
      Doc.show(page.err)
      return
    }

    let cert = ''
    if (page.certFile.value) {
      const files = page.certFile.files
      if (files && files.length) {
        cert = await files[0].text()
      }
    }

    let pw = ''
    if (!State.passwordIsCached()) {
      pw = page.appPW.value || (this.pwCache ? this.pwCache.pw : '')
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
        Doc.show(page.needCert)
      } else {
        page.err.textContent = res.msg
        Doc.show(page.err)
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
    if (!files || !files.length) return
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
  form: HTMLElement
  success: () => void
  pwCache: PasswordCache | null
  headerTxt: string
  page: Record<string, PageElement>

  constructor (form: HTMLElement, success: () => void, pwCache?: PasswordCache) {
    this.success = success
    this.form = form
    this.pwCache = pwCache || null
    const page = this.page = Doc.parseTemplate(form)
    this.headerTxt = page.header.textContent || ''

    bind(form, page.submit, () => { this.submit() })
  }

  focus () {
    this.page.pw.focus()
  }

  async submit () {
    const page = this.page
    Doc.hide(page.errMsg)
    const pw = page.pw.value || ''
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
      form.style.opacity = String(Math.pow(prog, 4))
    }, 'easeOut')
  }
}

const animationLength = 300

/* Swap form1 for form2 with an animation. */
export async function slideSwap (form1: HTMLElement, form2: HTMLElement) {
  const shift = document.body.offsetWidth / 2
  await Doc.animate(animationLength, progress => {
    form1.style.right = `${progress * shift}px`
  }, 'easeInHard')
  Doc.hide(form1)
  form1.style.right = '0'
  form2.style.right = String(-shift)
  Doc.show(form2)
  if (form2.querySelector('input')) {
    Doc.safeSelector(form2, 'input').focus()
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
export function bind (form: HTMLElement, submitBttn: HTMLElement, handler: (e: Event) => void) {
  const wrapper = (e: Event) => {
    if (e.preventDefault) e.preventDefault()
    handler(e)
  }
  Doc.bind(submitBttn, 'click', wrapper)
  Doc.bind(form, 'submit', wrapper)
}

// isTruthyString will be true if the provided string is recognized as a
// value representing true.
function isTruthyString (s: string) {
  return s === '1' || s.toLowerCase() === 'true'
}

// toUnixDate converts a javscript date object to a unix date, which is
// the number of *seconds* since the start of the epoch.
function toUnixDate (date: Date) {
  return Math.floor(date.getTime() / 1000)
}

// dateToString converts a javascript date object to a YYYY-MM-DD format string.
function dateToString (date: Date) {
  return date.toISOString().split('T')[0]
}
