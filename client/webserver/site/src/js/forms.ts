import Doc, { Animation } from './doc'
import { postJSON } from './http'
import State from './state'
import * as intl from './locales'
import { Wave } from './charts'
import {
  bondReserveMultiplier,
  perTierBaseParcelLimit,
  parcelLimitScoreMultiplier,
  strongTier
} from './account'
import {
  app,
  PasswordCache,
  SupportedAsset,
  PageElement,
  WalletDefinition,
  ConfigOption,
  Exchange,
  Market,
  BondAsset,
  WalletState,
  BalanceNote,
  Order,
  XYRange,
  WalletStateNote,
  WalletInfo,
  Token,
  WalletCreationNote,
  CoreNote,
  PrepaidBondID
} from './registry'
import { XYRangeHandler } from './opts'
import { CoinExplorers } from './coinexplorers'

interface ConfigOptionInput extends HTMLInputElement {
  configOpt: ConfigOption
}

interface ProgressPoint {
  stamp: number
  progress: number
}

interface CurrentAsset {
  asset: SupportedAsset
  parentAsset?: SupportedAsset
  winfo: WalletInfo | Token
  // selectedDef is used in a strange way for tokens. If a token's parent wallet
  // already exists, then selectedDef is going to be the Token.definition.
  // BUT, if the token's parent wallet doesn't exist yet, the NewWalletForm
  // operates in a combined configuration mode, and the selectedDef will be the
  // currently selected parent asset definition. There is no loss of info
  // in such a case, because the token wallet only has one definition.
  selectedDef: WalletDefinition
}

interface WalletConfig {
  assetID: number
  config: Record<string, string>
  walletType: string
}

export class Forms {
  formsDiv: PageElement
  currentForm: PageElement
  keyup: (e: KeyboardEvent) => void

  constructor (formsDiv: PageElement) {
    this.formsDiv = formsDiv

    formsDiv.querySelectorAll('.form-closer').forEach(el => {
      Doc.bind(el, 'click', () => { this.close() })
    })

    Doc.bind(formsDiv, 'mousedown', (e: MouseEvent) => {
      if (!Doc.mouseInElement(e, this.currentForm)) { this.close() }
    })

    this.keyup = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        this.close()
      }
    }
    Doc.bind(document, 'keyup', this.keyup)
  }

  /* showForm shows a modal form with a little animation. */
  async show (form: HTMLElement): Promise<void> {
    this.currentForm = form
    Doc.hide(...Array.from(this.formsDiv.children))
    form.style.right = '10000px'
    Doc.show(this.formsDiv, form)
    const shift = (this.formsDiv.offsetWidth + form.offsetWidth) / 2
    await Doc.animate(animationLength, progress => {
      form.style.right = `${(1 - progress) * shift}px`
    }, 'easeOutHard')
    form.style.right = '0'
  }

  close (): void {
    Doc.hide(this.formsDiv)
  }

  exit () {
    Doc.unbind(document, 'keyup', this.keyup)
  }
}

/*
 * NewWalletForm should be used with the "newWalletForm" template. The enclosing
 * <form> element should be the first argument of the constructor.
 */
export class NewWalletForm {
  page: Record<string, PageElement>
  form: HTMLElement
  pwCache: PasswordCache | null
  success: (assetID: number) => void
  current: CurrentAsset
  pwHiders: HTMLElement[]
  subform: WalletConfigForm
  walletCfgGuide: PageElement
  parentSyncer: null | ((w: WalletState) => void)
  createUpdater: null | ((note: WalletCreationNote) => void)

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

    this.walletCfgGuide = Doc.tmplElement(form, 'walletCfgGuide')

    bind(form, page.submitAdd, () => this.submit())
    bind(form, page.oneBttn, () => this.submit())

    app().registerNoteFeeder({
      walletstate: (note: WalletStateNote) => { this.reportWalletState(note.wallet) },
      createwallet: (note: WalletCreationNote) => { this.reportCreationUpdate(note) }
    })
  }

  /*
   * reportWalletState should be called when a 'walletstate' notification is
   * received.
   * TODO: Let form classes register for notifications.
   */
  reportWalletState (w: WalletState): void {
    if (this.parentSyncer) this.parentSyncer(w)
  }

  /*
   * reportWalletState should be called when a 'createwallet' notification is
   * received.
   */
  reportCreationUpdate (note: WalletCreationNote) {
    if (this.createUpdater) this.createUpdater(note)
  }

  refresh () {
    const hidePWBox = State.passwordIsCached() || (this.pwCache && this.pwCache.pw)
    if (hidePWBox) Doc.hide(...this.pwHiders)
    else Doc.show(...this.pwHiders)
  }

  async createWallet (assetID: number, walletType: string, pw: string, parentForm?: WalletConfig) {
    const createForm = {
      assetID: assetID,
      pass: this.page.newWalletPass.value || '',
      config: this.subform.map(assetID),
      appPass: pw,
      walletType: walletType,
      parentForm: parentForm
    }

    const ani = new Wave(this.page.mainForm, { backgroundColor: true })
    const res = await postJSON('/api/newwallet', createForm)
    ani.stop()
    return res
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

    const { asset, parentAsset } = this.current
    const selectedDef = this.current.selectedDef
    let parentForm
    let walletType = selectedDef.type
    if (parentAsset) {
      walletType = (asset.token as Token).definition.type
      parentForm = {
        assetID: parentAsset.id,
        config: this.subform.map(parentAsset.id),
        walletType: selectedDef.type
      }
    }
    // Register the selected asset.
    const res = await this.createWallet(asset.id, walletType, pw, parentForm)
    if (!app().checkResponse(res)) {
      this.setError(res.msg)
      return
    }
    if (this.pwCache) this.pwCache.pw = pw
    page.appPass.value = ''
    newWalletPass.value = ''
    if (parentAsset) await this.runParentSync()
    else this.success(this.current.asset.id)
  }

  /*
   * runParentSync shows a syncing sub-dialog that tracks the parent asset's
   * syncProgress and informs the user that the token wallet will be created
   * after sync is complete.
   */
  async runParentSync () {
    const { page, current: { parentAsset, asset } } = this
    if (!parentAsset) return

    page.parentSyncPct.textContent = '0'
    page.parentName.textContent = parentAsset.name
    page.parentLogo.src = Doc.logoPath(parentAsset.symbol)
    page.childName.textContent = asset.name
    page.childLogo.src = Doc.logoPath(asset.symbol)
    Doc.hide(page.mainForm)
    Doc.show(page.parentSyncing)

    try {
      await this.syncParent(parentAsset)
      this.success(this.current.asset.id)
    } catch (error) {
      this.setError(error.message || error)
    }
    Doc.show(page.mainForm)
    Doc.hide(page.parentSyncing)
  }

  /*
   * syncParent monitors the sync progress of a token's parent asset, generating
   * an Error if the token wallet creation does not complete successfully.
   */
  syncParent (parentAsset: SupportedAsset): Promise<void> {
    const { page, current: { asset } } = this
    return new Promise((resolve, reject) => {
      // First, check if it's already synced.
      const w = app().assets[parentAsset.id].wallet
      if (w && w.synced) return resolve()
      // Not synced, so create a syncer to update the parent sync pane.
      this.parentSyncer = (w: WalletState) => {
        if (w.assetID !== parentAsset.id) return
        page.parentSyncPct.textContent = (w.syncProgress * 100).toFixed(1)
      }
      // Handle the async result.
      this.createUpdater = (note: WalletCreationNote) => {
        if (note.assetID !== asset.id) return
        switch (note.topic) {
          case 'QueuedCreationFailed':
            reject(new Error(`${note.subject}: ${note.details}`))
            break
          case 'QueuedCreationSuccess':
            resolve()
            break
          default:
            return
        }
        this.parentSyncer = null
        this.createUpdater = null
      }
    })
  }

  /* setAsset sets the current asset of the NewWalletForm */
  async setAsset (assetID: number) {
    if (!this.parseAsset(assetID)) return // nothing to change
    const page = this.page
    const tabs = page.walletTypeTabs
    const { winfo, asset, parentAsset } = this.current
    page.assetName.textContent = winfo.name
    page.newWalletPass.value = ''

    Doc.empty(tabs)
    Doc.hide(tabs, page.newWalletErr, page.tokenMsgBox)
    page.header.classList.remove('bordertop')
    this.page.assetLogo.src = Doc.logoPath(asset.symbol)
    if (parentAsset) {
      page.tokenParentLogo.src = Doc.logoPath(parentAsset.symbol)
      page.tokenParentName.textContent = parentAsset.name
      Doc.show(page.tokenMsgBox)
    }

    const pinfo = parentAsset ? parentAsset.info : null
    const walletDefs = pinfo ? pinfo.availablewallets : (winfo as WalletInfo).availablewallets ? (winfo as WalletInfo).availablewallets : [(winfo as Token).definition]

    if (walletDefs.length > 1) {
      Doc.show(tabs)
      for (const wDef of walletDefs) {
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

    await this.update(this.current.selectedDef)
    if (asset.walletCreationPending) await this.runParentSync()
  }

  /*
  * parseAsset parses the current data for the asset ID.
  */
  parseAsset (assetID: number) {
    if (this.current && this.current.asset.id === assetID) return false
    const asset = app().assets[assetID]
    const token = asset.token
    if (!token) {
      if (!asset.info) throw Error('this non-token asset has no wallet info!')
      this.current = { asset, winfo: asset.info, selectedDef: asset.info.availablewallets[0] }
      return true
    }
    const parentAsset = app().user.assets[token.parentID]
    if (parentAsset.wallet) {
      // If the parent asset already has a wallet, there's no need to configure
      // the parent too. Just configure the token.
      this.current = { asset, winfo: token, selectedDef: token.definition }
      return true
    }
    if (!parentAsset.info) throw Error('this parent has no wallet info!')
    this.current = { asset, parentAsset, winfo: token, selectedDef: parentAsset.info.availablewallets[0] }
    return true
  }

  async update (walletDef: WalletDefinition) {
    const page = this.page
    this.current.selectedDef = walletDef
    const appPwCached = State.passwordIsCached() || (this.pwCache && this.pwCache.pw)
    Doc.hide(page.walletPassAndSubmitBttn, page.oneBttnBox, page.newWalletPassBox)
    const guideLink = walletDef.guidelink
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
    // Either this is a walletDef for a token's uncreated parent asset, or this
    // is the definition for the token.
    let containsRequired = false
    for (const opt of configOpts) {
      if (opt.required) {
        containsRequired = true
        break
      }
    }
    const { asset, parentAsset, winfo } = this.current
    const displayCreateBtn = walletDef.seeded || Boolean(asset.token)
    if (appPwCached && displayCreateBtn && !containsRequired) {
      Doc.show(page.oneBttnBox)
    } else if (displayCreateBtn) {
      Doc.show(page.walletPassAndSubmitBttn)
      page.newWalletPass.value = ''
      page.submitAdd.textContent = intl.prep(intl.ID_CREATE)
    } else {
      Doc.show(page.walletPassAndSubmitBttn)
      if (!walletDef.noauth) Doc.show(page.newWalletPassBox)
      page.submitAdd.textContent = intl.prep(intl.ID_ADD)
    }

    if (parentAsset) {
      const parentAndTokenOpts = JSON.parse(JSON.stringify(configOpts))
      // Add the regAsset field to the configurations so proper logos will be displayed
      // next to them, and map can filter them out. The opts are copied here so the originals
      // do not have the regAsset field added to them.
      for (const opt of parentAndTokenOpts) opt.regAsset = parentAsset.id
      const tokenOpts = (winfo as Token).definition.configopts || []
      if (tokenOpts.length > 0) {
        const tokenOptsCopy = JSON.parse(JSON.stringify(tokenOpts))
        for (const opt of tokenOptsCopy) opt.regAsset = asset.id
        parentAndTokenOpts.push(...tokenOptsCopy)
      }
      this.subform.update(asset.id, parentAndTokenOpts, false)
    } else this.subform.update(asset.id, configOpts, false)
    this.setGuideLink(guideLink)

    if (this.subform.dynamicOpts.children.length || this.subform.defaultSettings.children.length) {
      Doc.show(page.walletSettingsHeader)
    } else Doc.hide(page.walletSettingsHeader)
    // A seeded or token wallet is internal to the dex client and as such does
    // not have an external config file to select.
    if (walletDef.seeded || Boolean(this.current.asset.token)) Doc.hide(this.subform.fileSelector)
    else Doc.show(this.subform.fileSelector)

    this.refresh()
    await this.loadDefaults()
  }

  setGuideLink (guideLink: string) {
    Doc.hide(this.walletCfgGuide)
    if (guideLink !== '') {
      this.walletCfgGuide.href = guideLink
      Doc.show(this.walletCfgGuide)
    }
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
    const { asset, parentAsset, selectedDef } = this.current
    if (!selectedDef.configpath) return
    let configID = asset.id
    if (parentAsset) {
      if (selectedDef.seeded) return
      configID = parentAsset.id
    }
    const loaded = app().loading(this.form)
    const res = await postJSON('/api/defaultwalletcfg', {
      assetID: configID,
      type: selectedDef.type
    })
    loaded()
    if (!app().checkResponse(res)) {
      this.setError(res.msg)
      return
    }
    this.subform.setLoadedConfig(res.config)
  }
}

let dynamicInputCounter = 0

/*
 * WalletConfigForm is a dynamically generated sub-form for setting
 * asset-specific wallet configuration options.
*/
export class WalletConfigForm {
  page: Record<string, PageElement>
  form: HTMLElement
  configElements: [ConfigOption, HTMLElement][]
  configOpts: ConfigOption[]
  sectionize: boolean
  allSettings: PageElement
  dynamicOpts: PageElement
  textInputTmpl: PageElement
  dateInputTmpl: PageElement
  checkboxTmpl: PageElement
  repeatableTmpl: PageElement
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
  assetHasActiveOrders: boolean
  assetID: number

  constructor (form: HTMLElement, sectionize: boolean) {
    this.page = Doc.idDescendants(form)
    this.form = form
    // A configElement is a div containing an input and its label.
    this.configElements = []
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
    this.repeatableTmpl = Doc.tmplElement(form, 'repeatableInput')
    this.repeatableTmpl.remove()
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

  addOpt (box: HTMLElement, opt: ConfigOption, insertAfter?: PageElement, skipRepeatN?: boolean): PageElement {
    let el: HTMLElement
    if (opt.isboolean) el = this.checkboxTmpl.cloneNode(true) as HTMLElement
    else if (opt.isdate) el = this.dateInputTmpl.cloneNode(true) as HTMLElement
    else if (opt.repeatable) {
      el = this.repeatableTmpl.cloneNode(true) as HTMLElement
      el.classList.add('repeatable')
      Doc.bind(Doc.tmplElement(el, 'add'), 'click', () => {
        this.addOpt(box, opt, el, true)
      })
      if (!skipRepeatN) for (let i = 0; i < (opt.repeatN ? opt.repeatN - 1 : 0); i++) this.addOpt(box, opt, insertAfter, true)
    } else el = this.textInputTmpl.cloneNode(true) as HTMLElement
    const hiddenFields = app().extensionWallet(this.assetID)?.hiddenFields || []
    if (hiddenFields.indexOf(opt.key) !== -1) Doc.hide(el)
    this.configElements.push([opt, el])
    const input = el.querySelector('input') as ConfigOptionInput
    input.dataset.configKey = opt.key
    // We need to generate a unique ID only for the <input id> => <label for>
    // matching.
    dynamicInputCounter++
    const elID = 'wcfg-' + String(dynamicInputCounter)
    input.id = elID
    const label = Doc.safeSelector(el, 'label')
    label.htmlFor = elID // 'for' attribute, but 'for' is a keyword
    label.prepend(opt.displayname)
    if (opt.regAsset !== undefined) {
      const logo = new window.Image(15, 15)
      logo.src = Doc.logoPathFromID(opt.regAsset || -1)
      label.prepend(logo)
    }
    if (insertAfter) insertAfter.after(el)
    else box.appendChild(el)
    if (opt.noecho) {
      input.type = 'password'
      input.autocomplete = 'off'
    }
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
      const date = opt.default ? new Date(opt.default * 1000) : new Date()
      // UI shows Dates in valueAsDate as UTC, but user interprets local. Set a
      // local date string so the UI displays what the user expects. alt:
      // input.valueAsDate = dateApplyOffset(date)
      input.value = dateToString(date)
    } else input.value = opt.default !== null ? opt.default : ''
    input.disabled = Boolean(opt.disablewhenactive && this.assetHasActiveOrders)
    return el
  }

  /*
   * update creates the dynamic form.
   */
  update (assetID: number, configOpts: ConfigOption[] | null, activeOrders: boolean) {
    this.assetHasActiveOrders = activeOrders
    this.configElements = []
    this.configOpts = configOpts || []
    this.assetID = assetID
    Doc.empty(this.dynamicOpts, this.defaultSettings, this.loadedSettings)

    // If there are no options, just hide the entire form.
    if (this.configOpts.length === 0) return Doc.hide(this.form)
    Doc.show(this.form)

    this.setOtherSettingsViz(false)
    Doc.hide(
      this.loadedSettingsMsg, this.loadedSettings, this.defaultSettingsMsg,
      this.defaultSettings, this.errMsg
    )
    const defaultedOpts = []
    for (const opt of this.configOpts) {
      if (this.sectionize && opt.default !== null) defaultedOpts.push(opt)
      else this.addOpt(this.dynamicOpts, opt)
    }
    if (defaultedOpts.length) {
      for (const opt of defaultedOpts) this.addOpt(this.defaultSettings, opt)
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
  setConfig (cfg: Record<string, string>): HTMLElement[] {
    const finds: HTMLElement[] = []
    const handledRepeatables: Record<string, boolean> = {}
    const removes: [ConfigOption, PageElement][] = []
    for (const r of [...this.configElements]) {
      const [opt, el] = r
      const v = cfg[opt.key]
      if (v === undefined) continue
      if (opt.repeatable) {
        if (handledRepeatables[opt.key]) {
          el.remove()
          removes.push(r)
          continue
        }
        handledRepeatables[opt.key] = true
        const vals = v.split(opt.repeatable)
        const firstVal = vals[0]
        finds.push(el)
        Doc.safeSelector(el, 'input').value = firstVal
        // Add repeatN - 1 empty elements to the reconfig form. Add them before
        // the populated inputs just because of the way we're using the
        // insertAfter argument to addOpt.
        for (let i = 1; i < (opt.repeatN || 1); i++) finds.push(this.addOpt(el.parentElement as PageElement, opt, el, true))
        for (let i = 1; i < vals.length; i++) {
          const newEl = this.addOpt(el.parentElement as PageElement, opt, el, true)
          Doc.safeSelector(newEl, 'input').value = vals[i]
          finds.push(newEl)
        }
        continue
      }
      finds.push(el)
      const input = Doc.safeSelector(el, 'input') as HTMLInputElement
      if (opt.isboolean) input.checked = isTruthyString(v)
      else if (opt.isdate) {
        input.value = dateToString(new Date(parseInt(v) * 1000))
        // alt: input.valueAsDate = dateApplyOffset(...)
      } else input.value = v
    }
    for (const r of removes) {
      const i = this.configElements.indexOf(r)
      if (i >= 0) this.configElements.splice(i, 1)
    }

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
  map (assetID: number): Record<string, string> {
    const config: Record<string, string> = {}
    for (const [opt, el] of this.configElements) {
      const input = Doc.safeSelector(el, 'input') as HTMLInputElement
      if (opt.regAsset !== undefined && opt.regAsset !== assetID) continue
      if (opt.isboolean && opt.key) {
        config[opt.key] = input.checked ? '1' : '0'
      } else if (opt.isdate && opt.key) {
        // Force local time interpretation by appending a time to the date
        // string, otherwise the Date constructor considers it UTC.
        const minDate = input.min ? toUnixDate(new Date(input.min + 'T00:00')) : Number.MIN_SAFE_INTEGER
        const maxDate = input.max ? toUnixDate(new Date(input.max + 'T00:00')) : Number.MAX_SAFE_INTEGER
        let date = input.value ? toUnixDate(new Date(input.value + 'T00:00')) : 0
        if (date < minDate) date = minDate
        else if (date > maxDate) date = maxDate
        config[opt.key] = '' + date
      } else if (input.value) {
        if (opt.repeatable && config[opt.key]) config[opt.key] += opt.repeatable + input.value
        else config[opt.key] = input.value
      }
    }
    return config
  }

  /*
   * reorder sorts the configElements in the box by the order of the
   * server-provided configOpts array.
   */
  reorder (box: HTMLElement) {
    const inputs: Record<string, HTMLElement[]> = {}
    box.querySelectorAll('input').forEach((input: ConfigOptionInput) => {
      const k = input.dataset.configKey
      if (!k) return // TS2538
      const els = []
      for (const [opt, el] of this.configElements) if (opt.key === k) els.push(el)
      inputs[k] = els
    })
    for (const opt of this.configOpts) {
      const els = inputs[opt.key] || []
      for (const el of els) box.append(el)
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
  bondAssetID: number
  tier: number
  fees: number
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

  setAsset (assetID: number, tier: number, fees: number) {
    const asset = app().assets[assetID]
    const { conversionFactor, unit } = asset.unitInfo.conventional
    this.bondAssetID = asset.id
    this.tier = tier
    this.fees = fees
    const page = this.page
    const bondAsset = this.xc.bondAssets[asset.symbol]
    const bondLock = bondAsset.amount * tier * bondReserveMultiplier
    const bondLockConventional = bondLock / conversionFactor
    page.tradingTier.textContent = String(tier)
    page.logo.src = Doc.logoPath(asset.symbol)
    page.bondLock.textContent = Doc.formatFourSigFigs(bondLockConventional)
    page.bondUnit.textContent = unit
    const r = app().fiatRatesMap[assetID]
    Doc.show(page.bondLockUSDBox)
    if (r) page.bondLockUSD.textContent = Doc.formatFourSigFigs(bondLockConventional * r)
    else Doc.hide(page.bondLockUSDBox)
    if (fees) page.feeReserves.textContent = Doc.formatFourSigFigs(fees / conversionFactor)
    page.reservesUnit.textContent = unit
  }

  setFees (assetID: number, fees: number) {
    this.fees = fees
    const conversionFactor = app().assets[assetID].unitInfo.conventional.conversionFactor
    this.page.feeReserves.textContent = Doc.formatFourSigFigs(fees / conversionFactor)
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
    const { page, bondAssetID, xc, certFile, pwCache, tier } = this
    const asset = app().assets[bondAssetID]
    if (!asset) {
      page.regErr.innerText = intl.prep(intl.ID_SELECT_WALLET_FOR_FEE_PAYMENT)
      Doc.show(page.regErr)
      return
    }
    Doc.hide(page.regErr)
    const bondAsset = xc.bondAssets[asset.wallet.symbol]
    const dexAddr = xc.host
    const pw = page.appPass.value || (pwCache ? pwCache.pw : '')
    let form: any
    let url: string

    if (!app().exchanges[xc.host] || app().exchanges[xc.host].viewOnly) {
      form = {
        addr: dexAddr,
        cert: certFile,
        pass: pw,
        bond: bondAsset.amount * tier,
        asset: bondAsset.id
      }
      url = '/api/postbond'
    } else {
      form = {
        host: dexAddr,
        targetTier: tier,
        bondAssetID: bondAssetID
      }
      url = '/api/updatebondoptions'
    }
    page.appPass.value = ''
    const loaded = app().loading(this.form)
    const res = await postJSON(url, form)
    loaded()
    if (!app().checkResponse(res)) {
      page.regErr.textContent = res.msg
      Doc.show(page.regErr)
      return
    }
    this.success()
  }
}

interface RegAssetRow {
  ready: PageElement
}

interface MarketLimitsRow {
  mkt: Market
  tmpl: Record<string, PageElement>
  setTier: ((tier: number) => void)
}

/*
 * FeeAssetSelectionForm should be used with the "regAssetForm" template.
 */
export class FeeAssetSelectionForm {
  form: HTMLElement
  success: (assetID: number, tier: number) => Promise<void>
  xc: Exchange
  selectedAssetID: number
  certFile: string
  page: Record<string, PageElement>
  assetRows: Record<string, RegAssetRow>
  marketRows: MarketLimitsRow[]
  pwCache: PasswordCache

  constructor (form: HTMLElement, success: (assetID: number, tier: number) => Promise<void>, pwCache: PasswordCache) {
    this.form = form
    this.pwCache = pwCache
    this.certFile = ''
    this.success = success
    const page = this.page = Doc.parseTemplate(form)
    Doc.cleanTemplates(page.currentBondTmpl, page.bondAssetTmpl, page.marketTmpl)

    Doc.bind(page.tradingTierInput, 'input', () => { this.setTier() })
    Doc.bind(page.tradingTierInput, 'keyup', (e: KeyboardEvent) => { if (e.key === 'Enter') this.acceptTier() })
    Doc.bind(page.submitTradingTier, 'click', () => { this.acceptTier() })

    Doc.bind(page.tierUp, 'click', () => { this.incrementTier(true) })
    Doc.bind(page.tierDown, 'click', () => { this.incrementTier(false) })

    Doc.bind(page.goBackToAssets, 'click', () => {
      Doc.hide(page.tradingTierForm)
      Doc.show(page.assetForm)
    })

    Doc.bind(page.whatsABond, 'click', () => {
      Doc.hide(page.assetForm)
      Doc.show(page.whatsABondPanel)
    })

    const hideWhatsABond = () => {
      Doc.show(page.assetForm)
      Doc.hide(page.whatsABondPanel)
    }

    Doc.bind(page.bondGotIt, 'click', () => { hideWhatsABond() })

    Doc.bind(page.whatsABondBack, 'click', () => { hideWhatsABond() })

    Doc.bind(page.usePrepaidBond, 'click', () => { this.showPrepaidBondForm() })
    Doc.bind(page.ppbGoBack, 'click', () => { this.hidePrepaidBondForm() })
    Doc.bind(page.submitPrepaidBond, 'click', () => { this.submitPrepaidBond() })

    app().registerNoteFeeder({
      createwallet: (note: WalletCreationNote) => {
        if (note.topic === 'QueuedCreationSuccess') this.walletCreated(note.assetID)
      }
    })
  }

  setTierError (errMsg: string) {
    this.page.tradingTierErr.textContent = errMsg
    Doc.show(this.page.tradingTierErr)
  }

  setAssetError (errMsg: string) {
    this.page.regAssetErr.textContent = errMsg
    Doc.show(this.page.regAssetErr)
  }

  clearErrors () {
    Doc.hide(this.page.regAssetErr, this.page.tradingTierErr)
  }

  setExchange (xc: Exchange, certFile: string) {
    this.xc = xc
    this.certFile = certFile
    this.assetRows = {}
    this.marketRows = []
    const page = this.page
    Doc.hide(page.assetForm, page.tradingTierForm, page.whatsABondPanel, page.prepaidBonds)
    Doc.empty(page.bondAssets, page.markets)
    this.clearErrors()

    const addBondRow = (assetID: number, bondAsset: BondAsset) => {
      const asset = app().assets[assetID]
      if (!asset) return
      const { unitInfo: { conventional: { unit, conversionFactor } }, name, symbol } = asset
      const tr = page.bondAssetTmpl.cloneNode(true) as HTMLElement
      page.bondAssets.appendChild(tr)
      const tmpl = Doc.parseTemplate(tr)

      tmpl.logo.src = Doc.logoPath(symbol)
      tmpl.name.textContent = name

      Doc.bind(tr, 'click', () => { this.assetSelected(assetID) })
      tmpl.feeSymbol.textContent = unit
      const bondSizeConventional = bondAsset.amount / conversionFactor
      tmpl.feeAmt.textContent = Doc.formatFourSigFigs(bondSizeConventional)
      const fiatRate = app().fiatRatesMap[assetID]
      Doc.setVis(fiatRate, tmpl.fiatBox)
      if (fiatRate) tmpl.fiatBondAmount.textContent = Doc.formatFourSigFigs(bondSizeConventional * fiatRate)
      this.assetRows[assetID] = { ready: tmpl.ready }
    }

    const addMarketRow = (mkt: Market) => {
      const { baseid: baseID, quoteid: quoteID } = mkt
      const [b, q] = [app().assets[baseID], app().assets[quoteID]]
      if (!b || !q) return
      const tr = page.marketTmpl.cloneNode(true) as HTMLElement
      page.markets.appendChild(tr)
      const { symbol: baseSymbol, unitInfo: bui } = xc.assets[baseID]
      const { symbol: quoteSymbol, unitInfo: qui } = xc.assets[quoteID]
      for (const el of Doc.applySelector(tr, '[data-base-ticker')) el.textContent = bui.conventional.unit
      for (const el of Doc.applySelector(tr, '[data-quote-ticker')) el.textContent = qui.conventional.unit

      const tmpl = Doc.parseTemplate(tr)
      tmpl.baseLogo.src = Doc.logoPath(baseSymbol)
      tmpl.quoteLogo.src = Doc.logoPath(quoteSymbol)

      const setTier = (tier: number) => {
        const { parcelsize: parcelSize, lotsize: lotSize } = mkt
        const conventionalLotSize = lotSize / bui.conventional.conversionFactor
        const startingLimit = conventionalLotSize * parcelSize * perTierBaseParcelLimit * tier
        const privilegedLimit = conventionalLotSize * parcelSize * perTierBaseParcelLimit * parcelLimitScoreMultiplier * tier
        tmpl.tradeLimitLow.textContent = Doc.formatFourSigFigs(startingLimit)
        tmpl.tradeLimitHigh.textContent = Doc.formatFourSigFigs(privilegedLimit)
        const baseFiatRate = app().fiatRatesMap[baseID]
        if (baseFiatRate) {
          tmpl.fiatTradeLimitLow.textContent = Doc.formatFourSigFigs(startingLimit * baseFiatRate)
          tmpl.fiatTradeLimitHigh.textContent = Doc.formatFourSigFigs(privilegedLimit * baseFiatRate)
        }
        Doc.setVis(baseFiatRate, page.fiatTradeLowBox, page.fiatTradeHighBox)
      }

      setTier(strongTier(xc.auth) || 1)
      this.marketRows.push({ mkt, tmpl, setTier })
    }

    for (const { symbol, id: assetID } of Object.values(xc.assets)) {
      if (!app().assets[assetID]) continue
      const bondAsset = xc.bondAssets[symbol]
      if (bondAsset) addBondRow(assetID, bondAsset)
    }

    for (const mkt of Object.values(xc.markets)) addMarketRow(mkt)

    // page.host.textContent = xc.host
    page.tradingTierInput.value = xc.auth.targetTier ? String(xc.auth.targetTier) : '1'

    if (this.validBondAssetSelected(xc)) this.assetSelected(xc.auth.bondAssetID)
    else Doc.show(page.assetForm)
  }

  validBondAssetSelected (xc: Exchange) {
    if (xc.viewOnly) return false
    const { targetTier, bondAssetID } = xc.auth
    if (targetTier < 1) return false
    const a = app().assets[bondAssetID]
    return a && Boolean(xc.bondAssets[a.symbol])
  }

  /*
   * walletCreated should be called when an asynchronous wallet creation
   * completes successfully.
   */
  walletCreated (assetID: number) {
    const a = this.assetRows[assetID]
    const asset = app().assets[assetID]
    setReadyMessage(a.ready, asset)
  }

  refresh () {
    this.setExchange(this.xc, this.certFile)
  }

  assetSelected (assetID: number) {
    this.selectedAssetID = assetID
    this.setTier()
    const { page: { assetForm, tradingTierForm, tradingTierInput } } = this
    Doc.hide(assetForm)
    Doc.show(tradingTierForm)
    tradingTierInput.focus()
  }

  setTier () {
    const { page, xc: { bondAssets }, selectedAssetID: assetID } = this
    const { symbol, unitInfo: ui } = app().assets[assetID]
    const { conventional: { conversionFactor, unit } } = ui

    const bondAsset = bondAssets[symbol]
    const raw = page.tradingTierInput.value ?? ''
    if (!raw) return
    const tier = parseInt(raw)
    if (isNaN(tier)) {
      this.setTierError(intl.prep(intl.ID_INVALID_TIER_VALUE))
      return
    }
    page.tradingTierInput.value = String(tier)
    page.bondSizeDisplay.textContent = Doc.formatCoinValue(bondAsset.amount, ui)
    for (const el of Doc.applySelector(page.tradingTierForm, '[data-tier]')) el.textContent = String(tier)
    for (const el of Doc.applySelector(page.tradingTierForm, '[data-bond-asset-ticker]')) el.textContent = unit
    const bondLock = bondAsset.amount * tier * bondReserveMultiplier
    page.bondLockDisplay.textContent = Doc.formatCoinValue(bondLock, ui)
    const fiatRate = app().fiatRatesMap[assetID]
    if (fiatRate) page.fiatLockDisplay.textContent = Doc.formatFourSigFigs(bondLock / conversionFactor * fiatRate)
    for (const m of Object.values(this.marketRows)) m.setTier(tier)
    const currentBondAmts: Record<number, number> = {}
    for (const [assetIDStr, { wallet }] of Object.entries(app().assets)) {
      if (!wallet) continue
      const { balance: { bondlocked, bondReserves } } = wallet
      const bonded = bondlocked + bondReserves
      if (bonded > 0) currentBondAmts[parseInt(assetIDStr)] = bonded
    }
    const haveLock = Object.keys(currentBondAmts).length > 0
    Doc.setVis(haveLock, page.currentBondBox)
    if (haveLock) {
      Doc.empty(page.currentBonds)
      for (const [assetIDStr, bondLocked] of Object.entries(currentBondAmts)) {
        const assetID = parseInt(assetIDStr)
        const { unitInfo: ui, symbol, name } = app().assets[assetID]
        const { conventional: { conversionFactor, unit } } = ui
        const tr = page.currentBondTmpl.cloneNode(true) as PageElement
        page.currentBonds.appendChild(tr)
        const tmpl = Doc.parseTemplate(tr)
        tmpl.icon.src = Doc.logoPath(symbol)
        tmpl.name.textContent = name
        tmpl.amt.textContent = Doc.formatCoinValue(bondLocked, ui)
        tmpl.ticker.textContent = unit
        tmpl.name.textContent = name
        const fiatRate = app().fiatRatesMap[assetID]
        Doc.setVis(tmpl.fiatBox)
        if (fiatRate) tmpl.fiatAmt.textContent = Doc.formatFourSigFigs(bondLocked / conversionFactor * fiatRate)
      }
    }
    Doc.setVis(fiatRate, page.fiatLockBox)
  }

  acceptTier () {
    const { page, selectedAssetID: assetID } = this
    this.clearErrors()
    const raw = page.tradingTierInput.value ?? ''
    if (!raw) return
    const tier = parseInt(raw)
    if (isNaN(tier)) {
      this.setTierError(intl.prep(intl.ID_INVALID_TIER_VALUE))
      return
    }
    this.success(assetID, tier)
  }

  incrementTier (up: boolean) {
    const { page: { tradingTierInput: input } } = this
    input.value = String(Math.max(1, (parseInt(input.value ?? '') || 1) + (up ? 1 : -1)))
    this.setTier()
  }

  /*
   * Animation to make the elements sort of expand into their space from the
   * bottom as they fade in.
   */
  async animate () {
    const { page, form } = this
    const extraMargin = 75
    const extraTop = 50
    const regAssetElements = Array.from(page.bondAssets.children) as PageElement[]
    form.style.opacity = '0'

    const aniLen = 350
    await Doc.animate(aniLen, prog => {
      for (const el of regAssetElements) {
        el.style.marginTop = `${(1 - prog) * extraMargin}px`
        el.style.transform = `scale(${prog})`
      }
      form.style.opacity = Math.pow(prog, 4).toFixed(1)
      form.style.top = `${(1 - prog) * extraTop}px`
    }, 'easeOut')
  }

  showPrepaidBondForm () {
    const { page, pwCache } = this
    Doc.hide(page.assetForm, page.prepaidBondErr)
    page.prepaidBondCode.value = ''
    page.prepaidBondPW.value = ''
    const hidePWBox = State.passwordIsCached() || (pwCache && pwCache.pw)
    Doc.setVis(!hidePWBox, page.prepaidBondPWBox)
    Doc.show(page.prepaidBonds)
  }

  hidePrepaidBondForm () {
    const { page } = this
    Doc.hide(page.prepaidBonds)
    Doc.show(page.assetForm)
  }

  async submitPrepaidBond () {
    const { page, xc: { host }, pwCache } = this
    Doc.hide(page.prepaidBondErr)
    const code = page.prepaidBondCode.value
    if (!code) {
      page.prepaidBondErr.textContent = intl.prep(intl.ID_INVALID_VALUE)
      Doc.show(page.prepaidBondErr)
      return
    }
    const appPW = page.prepaidBondPW.value || (pwCache ? pwCache.pw : '')
    const res = await postJSON('/api/redeemprepaidbond', { appPW, host, code, cert: this.certFile })
    if (!app().checkResponse(res)) {
      page.prepaidBondErr.textContent = res.msg
      Doc.show(page.prepaidBondErr)
      return
    }
    if (appPW && this.pwCache) this.pwCache.pw = appPW
    this.success(PrepaidBondID, res.tier)
  }
}

/*
 * setReadyMessage sets an asset's status message on the FeeAssetSelectionForm.
 */
function setReadyMessage (el: PageElement, asset: SupportedAsset) {
  if (asset.wallet) el.textContent = intl.prep(intl.ID_WALLET_READY)
  else if (asset.walletCreationPending) el.textContent = intl.prep(intl.ID_WALLET_PENDING)
  else el.textContent = intl.prep(intl.ID_SETUP_NEEDED)
  el.classList.remove('readygreen', 'setuporange')
  el.classList.add(asset.wallet ? 'readygreen' : 'setuporange')
}

/*
 * WalletWaitForm is a form used to track the wallet sync status and balance
 * in preparation for posting a bond.
 */
export class WalletWaitForm {
  form: HTMLElement
  success: () => void
  goBack: () => void
  page: Record<string, PageElement>
  assetID: number
  parentID?: number
  xc: Exchange
  bondAsset: BondAsset
  progressCache: ProgressPoint[]
  progressed: boolean
  funded: boolean
  // if progressed && funded, stop reporting balance or state; call success()
  bondFeeBuffer: number // in parent asset
  parentAssetSynced: boolean

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

    app().registerNoteFeeder({
      walletstate: (note: WalletStateNote) => this.reportWalletState(note.wallet),
      balance: (note: BalanceNote) => this.reportBalance(note.assetID)
    })
  }

  /* setExchange sets the exchange for which the fee is being paid. */
  setExchange (xc: Exchange) {
    this.xc = xc
  }

  /* setWallet must be called before showing the WalletWaitForm. */
  setWallet (assetID: number, bondFeeBuffer: number, tier: number) {
    this.assetID = assetID
    this.progressCache = []
    this.progressed = false
    this.funded = false
    this.bondFeeBuffer = bondFeeBuffer // in case we're a token, parent's balance must cover
    this.parentAssetSynced = false
    const page = this.page
    const asset = app().assets[assetID]
    const { symbol, unitInfo: ui, wallet: { balance: bal, address, synced, syncProgress }, token } = asset
    this.parentID = token?.parentID
    const bondAsset = this.bondAsset = this.xc.bondAssets[symbol]

    const symbolize = (el: PageElement, asset: SupportedAsset) => {
      Doc.empty(el)
      el.appendChild(Doc.symbolize(asset))
    }

    for (const span of Doc.applySelector(this.form, '.unit')) symbolize(span, asset)
    page.logo.src = Doc.logoPath(symbol)
    page.depoAddr.textContent = address

    Doc.hide(page.syncUncheck, page.syncCheck, page.balUncheck, page.balCheck, page.syncRemainBox, page.bondCostBreakdown)
    Doc.show(page.balanceBox)

    let bondLock = 2 * bondAsset.amount * tier
    if (bondFeeBuffer > 0) {
      Doc.show(page.bondCostBreakdown)
      page.bondLockNoFees.textContent = Doc.formatCoinValue(bondLock, ui)
      page.bondLockFees.textContent = Doc.formatCoinValue(bondFeeBuffer, ui)
      bondLock += bondFeeBuffer
      const need = Math.max(bondLock - bal.available + bal.reservesDeficit, 0)
      page.totalForBond.textContent = Doc.formatCoinValue(need, ui)
      Doc.hide(page.sendEnough) // generic msg when no fee info available when
      Doc.hide(page.txFeeBox, page.sendEnoughForToken, page.txFeeBalanceBox) // for tokens
      Doc.hide(page.sendEnoughWithEst) // non-tokens

      if (token) {
        Doc.show(page.txFeeBox, page.sendEnoughForToken, page.txFeeBalanceBox)
        const parentAsset = app().assets[token.parentID]
        page.txFee.textContent = Doc.formatCoinValue(bondFeeBuffer, parentAsset.unitInfo)
        page.parentFees.textContent = Doc.formatCoinValue(bondFeeBuffer, parentAsset.unitInfo)
        page.tokenFees.textContent = Doc.formatCoinValue(need, ui)
        symbolize(page.txFeeUnit, parentAsset)
        symbolize(page.parentUnit, parentAsset)
        symbolize(page.parentBalUnit, parentAsset)
        page.parentBal.textContent = parentAsset.wallet ? Doc.formatCoinValue(parentAsset.wallet.balance.available, parentAsset.unitInfo) : '0'
      } else {
        Doc.show(page.sendEnoughWithEst)
      }
      page.fee.textContent = Doc.formatCoinValue(bondLock, ui)
    } else { // show some generic message with no amounts, this shouldn't happen... show wallet error?
      Doc.show(page.sendEnough)
    }

    Doc.show(synced ? page.syncCheck : syncProgress >= 1 ? page.syncSpinner : page.syncUncheck)
    Doc.show(bal.available >= 2 * bondAsset.amount + bondFeeBuffer ? page.balCheck : page.balUncheck)

    page.progress.textContent = (syncProgress * 100).toFixed(1)

    if (synced) {
      this.progressed = true
    }
    this.reportBalance(assetID)
  }

  /*
   * reportWalletState sets the progress and balance, ultimately calling the
   * success function if conditions are met.
   */
  reportWalletState (wallet: WalletState) {
    if (this.progressed && this.funded) return
    if (wallet.assetID === this.assetID) this.reportProgress(wallet.synced, wallet.syncProgress)
    this.reportBalance(wallet.assetID)
  }

  /*
   * reportBalance sets the balance display and calls success if we go over the
   * threshold.
   */
  reportBalance (assetID: number) {
    if (this.funded || this.assetID === -1) return
    if (assetID !== this.assetID && assetID !== this.parentID) return
    const page = this.page
    const asset = app().assets[this.assetID]

    const avail = asset.wallet.balance.available
    page.balance.textContent = Doc.formatCoinValue(avail, asset.unitInfo)

    if (asset.token) {
      const parentAsset = app().assets[asset.token.parentID]
      const parentAvail = parentAsset.wallet.balance.available
      page.parentBal.textContent = Doc.formatCoinValue(parentAvail, parentAsset.unitInfo)
      if (parentAvail < this.bondFeeBuffer) return
    }

    // NOTE: when/if we allow one-time bond post (no maintenance) from the UI we
    // may allow to proceed as long as they have enough for tx fees. For now,
    // the balance check box will remain unchecked and we will not proceed.
    if (avail < 2 * this.bondAsset.amount + this.bondFeeBuffer) return

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
    page.progress.textContent = (prog * 100).toFixed(1)

    if (prog >= 0.999) {
      Doc.hide(page.syncRemaining)
      Doc.show(page.syncFinishingUp)
      Doc.show(page.syncRemainBox)
      // The final stage of wallet sync process can take a while (it might hang
      // at 99.9% for many minutes, indexing addresses for example), the simplest
      // way to handle it is to keep displaying "finishing up" message until the
      // sync is finished, since we can't reasonably show it progressing over time.
      page.syncFinishingUp.textContent = intl.prep(intl.ID_WALLET_SYNC_FINISHING_UP)
      return
    }
    // Before we get to 99.9% the remaining time estimate must be based on more
    // than one progress report. We'll cache up to the last 20 and look at the
    // difference between the first and last to make the estimate.
    const cacheSize = 20
    const cache = this.progressCache
    cache.push({
      stamp: new Date().getTime(),
      progress: prog
    })
    if (cache.length < 2) {
      // Can't meaningfully estimate remaining until we have at least 2 data points.
      return
    }
    while (cache.length > cacheSize) cache.shift()
    const [first, last] = [cache[0], cache[cache.length - 1]]
    const progDelta = last.progress - first.progress
    if (progDelta === 0) {
      // Having no progress for a while likely means we are experiencing network
      // issues, can't reasonably estimate time remaining in this case.
      return
    }
    Doc.hide(page.syncFinishingUp)
    Doc.show(page.syncRemaining)
    Doc.show(page.syncRemainBox)
    const timeDelta = last.stamp - first.stamp
    const progRate = progDelta / timeDelta
    const toGoProg = 1 - last.progress
    const toGoTime = toGoProg / progRate
    page.syncRemain.textContent = Doc.formatDuration(toGoTime)
  }
}

export class UnlockWalletForm {
  form: HTMLElement
  success: (assetID: number) => void
  pwCache: PasswordCache | null
  page: Record<string, PageElement>
  currentAsset: SupportedAsset

  constructor (form: HTMLElement, success: (assetID: number) => void, pwCache?: PasswordCache) {
    this.page = Doc.idDescendants(form)
    this.form = form
    this.pwCache = pwCache || null
    this.success = success
    bind(form, this.page.submitUnlock, () => this.submit())
  }

  refresh (asset: SupportedAsset) {
    const page = this.page
    this.currentAsset = asset
    page.uwAssetLogo.src = Doc.logoPath(asset.symbol)
    page.uwAssetName.textContent = asset.name
    page.uwAppPass.value = ''
    page.unlockErr.textContent = ''
    Doc.hide(page.unlockErr)
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
    const assetID = this.currentAsset.id
    Doc.hide(this.page.unlockErr)
    const open = {
      assetID: assetID,
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
    this.success(assetID)
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

  // sendAccelerateRequest makes an accelerate order request to the client
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
      page.accelerateErr.textContent = intl.prep(intl.ID_ORDER_ACCELERATION_ERR_MSG, { msg: res.msg })
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
      page.preAccelerateErr.textContent = intl.prep(intl.ID_ORDER_ACCELERATION_ERR_MSG, { msg: res.msg })
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
    this.acceleratedRate = preAccelerate.suggestedRange.start.y
    const selected = () => { /* do nothing */ }
    const roundY = true
    const updateRate = (_: number, newY: number) => { this.acceleratedRate = newY }
    const rangeHandler = new XYRangeHandler(preAccelerate.suggestedRange, preAccelerate.suggestedRange.start.x, {
      updated: updateRate, changed: () => this.updateAccelerationEstimate(), selected, roundY
    })
    Doc.empty(page.sliderContainer)
    page.sliderContainer.appendChild(rangeHandler.control)
    this.updateAccelerationEstimate()
  }

  // updateAccelerationEstimate makes an accelerate estimate request to the
  // client backend using the currently selected rate on the slider, and
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
      page.accelerateErr.textContent = intl.prep(intl.ID_ORDER_ACCELERATION_FEE_ERR_MSG, { msg: res.msg })
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
  page: Record<string, PageElement>
  knownExchanges: HTMLElement[]
  dexToUpdate?: string
  certPicker: CertificatePicker

  constructor (form: HTMLElement, success: (xc: Exchange, cert: string) => void, pwCache?: PasswordCache, dexToUpdate?: string) {
    this.form = form
    this.success = success
    this.pwCache = pwCache || null

    const page = this.page = Doc.parseTemplate(form)

    this.certPicker = new CertificatePicker(form)

    Doc.bind(page.skipRegistration, 'change', () => this.showOrHidePWBox())
    Doc.bind(page.showCustom, 'click', () => {
      Doc.hide(page.showCustom)
      Doc.show(page.customBox, page.auth)
    })

    this.knownExchanges = Array.from(page.knownXCs.querySelectorAll('.known-exchange'))
    for (const div of this.knownExchanges) {
      Doc.bind(div, 'click', () => {
        const host = div.dataset.host
        for (const d of this.knownExchanges) d.classList.remove('selected')
        // If we don't intend to register or we have the password cached, we're
        // good to go.
        if (this.skipRegistration() || State.passwordIsCached() || (pwCache && pwCache.pw)) {
          return this.checkDEX(host)
        }
        // Highlight the entry, but the user will have to enter their password
        // and click submit.
        div.classList.add('selected')
        page.appPW.focus()
        page.addr.value = host
      })
    }

    bind(form, page.submit, () => this.checkDEX())

    if (dexToUpdate) {
      Doc.hide(page.addDexHdr, page.skipRegistrationBox)
      Doc.show(page.updateDexHdr)
      this.dexToUpdate = dexToUpdate
    }

    this.refresh()
  }

  refresh () {
    const page = this.page
    page.addr.value = ''
    page.appPW.value = ''
    this.certPicker.clearCertFile()
    Doc.hide(page.err)
    if (this.knownExchanges.length === 0 || this.dexToUpdate) {
      Doc.show(page.customBox, page.auth)
      Doc.hide(page.showCustom, page.knownXCs, page.pickServerMsg, page.addCustomMsg)
    } else {
      Doc.hide(page.customBox)
      Doc.show(page.showCustom)
    }
    for (const div of this.knownExchanges) div.classList.remove('selected')
    this.showOrHidePWBox()
  }

  /**
   * Show or hide appPWBox depending on if password is required. Show the
   * submit button if connecting a custom server or password is required).
   */
  showOrHidePWBox () {
    const passwordCached = State.passwordIsCached() || (this.pwCache && this.pwCache.pw)
    const passwordRequired = !passwordCached && !this.skipRegistration()
    const page = this.page
    if (passwordRequired) {
      Doc.show(page.appPWBox, page.auth)
    } else {
      Doc.hide(page.appPWBox)
      Doc.setVis(Doc.isDisplayed(page.customBox), page.auth)
    }
  }

  skipRegistration () : boolean {
    return this.page.skipRegistration.checked ?? false
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
      page.err.textContent = intl.prep(intl.ID_EMPTY_DEX_ADDRESS_MSG)
      Doc.show(page.err)
      return
    }
    const cert = await this.certPicker.file()
    const skipRegistration = this.skipRegistration()
    let pw = ''
    if (!skipRegistration && !State.passwordIsCached()) {
      pw = page.appPW.value || (this.pwCache ? this.pwCache.pw : '')
    }
    let endpoint : string, req: any
    if (this.dexToUpdate) {
      endpoint = '/api/updatedexhost'
      req = {
        newHost: addr,
        cert: cert,
        pw: pw,
        oldHost: this.dexToUpdate
      }
    } else {
      endpoint = skipRegistration ? '/api/adddex' : '/api/discoveracct'
      req = {
        addr: addr,
        cert: cert,
        pass: pw
      }
    }

    const loaded = app().loading(this.form)
    const res = await postJSON(endpoint, req)
    loaded()
    if (!app().checkResponse(res)) {
      if (String(res.msg).includes('certificate required')) {
        Doc.show(page.needCert)
      } else {
        page.err.textContent = res.msg
        Doc.show(page.err)
      }
      return
    }
    await app().fetchUser()
    if (!this.dexToUpdate && (skipRegistration || res.paid || Object.keys(res.xc.auth.pendingBonds).length > 0)) {
      await app().loadPage('markets')
      return
    }
    if (this.pwCache) this.pwCache.pw = pw
    this.success(res.xc, cert)
  }
}

/* DiscoverAccountForm performs account discovery for a pre-selected DEX. */
export class DiscoverAccountForm {
  form: HTMLElement
  addr: string
  success: (xc: Exchange) => void
  pwCache: PasswordCache | null
  page: Record<string, PageElement>

  constructor (form: HTMLElement, addr: string, success: (xc: Exchange) => void, pwCache?: PasswordCache) {
    this.form = form
    this.addr = addr
    this.success = success
    this.pwCache = pwCache || null

    const page = this.page = Doc.parseTemplate(form)
    page.dexHost.textContent = addr
    bind(form, page.submit, () => this.checkDEX())

    this.refresh()
  }

  refresh () {
    const page = this.page
    page.appPW.value = ''
    Doc.hide(page.err)
    const hidePWBox = State.passwordIsCached() || (this.pwCache && this.pwCache.pw)
    if (hidePWBox) Doc.hide(page.appPWBox)
    else Doc.show(page.appPWBox)
  }

  /* Just a small size tweak and fade-in. */
  async animate () {
    const form = this.form
    Doc.animate(550, prog => {
      form.style.transform = `scale(${0.9 + 0.1 * prog})`
      form.style.opacity = String(Math.pow(prog, 4))
    }, 'easeOut')
  }

  async checkDEX () {
    const page = this.page
    Doc.hide(page.err)
    let pw = ''
    if (!State.passwordIsCached()) {
      pw = page.appPW.value || (this.pwCache ? this.pwCache.pw : '')
    }
    const req = {
      addr: this.addr,
      pass: pw
    }
    const loaded = app().loading(this.form)
    const res = await postJSON('/api/discoveracct', req)
    loaded()
    if (!app().checkResponse(res)) {
      page.err.textContent = res.msg
      Doc.show(page.err)
      return
    }
    if (res.paid) {
      await app().fetchUser()
      await app().loadPage('markets')
      return
    }
    if (this.pwCache) this.pwCache.pw = pw
    this.success(res.xc)
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

    app().registerNoteFeeder({
      login: (note: CoreNote) => { this.handleLoginNote(note) }
    })
  }

  handleLoginNote (n: CoreNote) {
    if (n.details === '') return
    const loginMsg = Doc.idel(this.form, 'loaderMsg')
    if (loginMsg) loginMsg.textContent = n.details
  }

  focus () {
    this.page.pw.focus()
  }

  refresh () {
    Doc.hide(this.page.errMsg)
    this.page.pw.value = ''
    this.page.rememberPass.checked = false
  }

  async submit () {
    const page = this.page
    Doc.hide(page.errMsg)
    const pw = page.pw.value || ''
    const rememberPass = page.rememberPass.checked
    if (pw === '') {
      Doc.showFormError(page.errMsg, intl.prep(intl.ID_NO_PASS_ERROR_MSG))
      return
    }
    const loaded = app().loading(this.form)
    const res = await postJSON('/api/login', { pass: pw, rememberPass })
    loaded()
    page.pw.value = ''
    if (!app().checkResponse(res)) {
      Doc.showFormError(page.errMsg, res.msg)
      return
    }
    await app().fetchUser()
    if (res.notes) {
      res.notes.reverse()
      app().setNotes(res.notes)
    }
    if (res.pokes) {
      app().setPokes(res.pokes)
    }
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

const traitNewAddresser = 1 << 1

/*
 * DepositAddress displays a deposit address, a QR code, and a button to
 * generate a new address (if supported).
 */
export class DepositAddress {
  form: PageElement
  page: Record<string, PageElement>
  assetID: number

  constructor (form: PageElement) {
    this.form = form
    const page = this.page = Doc.idDescendants(form)
    Doc.bind(page.newDepAddrBttn, 'click', async () => { this.newDepositAddress() })
    Doc.bind(page.copyAddressBtn, 'click', () => { this.copyAddress() })
  }

  /* Display a deposit address. */
  async setAsset (assetID: number) {
    this.assetID = assetID
    const page = this.page
    Doc.hide(page.depositErr, page.depositTokenMsgBox)
    const asset = app().assets[assetID]
    page.depositLogo.src = Doc.logoPath(asset.symbol)
    const wallet = app().walletMap[assetID]
    page.depositName.textContent = asset.unitInfo.conventional.unit
    page.depositAddress.textContent = wallet.address
    page.qrcode.src = `/generateqrcode?address=${wallet.address}`
    if (asset.token) {
      const parentAsset = app().assets[asset.token.parentID]
      page.depositTokenParentLogo.src = Doc.logoPath(parentAsset.symbol)
      page.depositTokenParentName.textContent = parentAsset.name
      Doc.show(page.depositTokenMsgBox)
    }
    if ((wallet.traits & traitNewAddresser) !== 0) Doc.show(page.newDepAddrBttn)
    else Doc.hide(page.newDepAddrBttn)
  }

  /* Fetch a new address from the wallet. */
  async newDepositAddress () {
    const page = this.page
    Doc.hide(page.depositErr)
    const loaded = app().loading(this.form)
    const res = await postJSON('/api/depositaddress', {
      assetID: this.assetID
    })
    loaded()
    if (!app().checkResponse(res)) {
      page.depositErr.textContent = res.msg
      Doc.show(page.depositErr)
      return
    }
    page.depositAddress.textContent = res.address
    page.qrcode.src = `/generateqrcode?address=${res.address}`
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
}

// AppPassResetForm is used to reset the app apssword using the app seed.
export class AppPassResetForm {
  form: PageElement
  page: Record<string, PageElement>
  success: () => void

  constructor (form: PageElement, success: () => void) {
    this.form = form
    this.success = success
    const page = this.page = Doc.idDescendants(form)
    bind(form, page.resetAppPWSubmitBtn, () => this.resetAppPW())
  }

  async resetAppPW () {
    const page = this.page
    const newAppPW = page.newAppPassword.value || ''
    const confirmNewAppPW = page.confirmNewAppPassword.value
    if (newAppPW === '') {
      Doc.showFormError(page.appPWResetErrMsg, intl.prep(intl.ID_NO_PASS_ERROR_MSG))
      return
    }
    if (newAppPW !== confirmNewAppPW) {
      Doc.showFormError(page.appPWResetErrMsg, intl.prep(intl.ID_PASSWORD_NOT_MATCH))
      return
    }

    const seed = page.seedInput.value?.replace(/\s+/g, '') // strip whitespace
    if (!seed || seed.length !== 128 /* 64 bytes hex encoded value, check and fail early */) {
      Doc.showFormError(page.appPWResetErrMsg, intl.prep(intl.ID_INVALID_SEED))
      return
    }
    const loaded = app().loading(this.form)
    const res = await postJSON('/api/resetapppassword', {
      newPass: newAppPW,
      seed
    })
    loaded()
    if (!app().checkResponse(res)) {
      Doc.showFormError(page.appPWResetErrMsg, res.msg)
      return
    }

    if (Doc.isDisplayed(page.appPWResetErrMsg)) Doc.hide(page.appPWResetErrMsg)
    page.appPWResetSuccessMsg.textContent = intl.prep(intl.ID_PASSWORD_RESET_SUCCESS_MSG)
    Doc.show(page.appPWResetSuccessMsg)
    setTimeout(() => this.success(), 3000) // allow time to view the message
  }

  focus () {
    this.page.newAppPassword.focus()
  }

  refresh () {
    const page = this.page
    page.newAppPassword.value = ''
    page.confirmNewAppPassword.value = ''
    Doc.hide(page.appPWResetSuccessMsg, page.appPWResetErrMsg)
  }
}

export class CertificatePicker {
  page: Record<string, PageElement>

  constructor (parent: PageElement) {
    const page = this.page = Doc.parseTemplate(parent)
    page.selectedCert.textContent = intl.prep(intl.ID_NONE_SELECTED)
    Doc.bind(page.certFile, 'change', () => this.onCertFileChange())
    Doc.bind(page.removeCert, 'click', () => this.clearCertFile())
    Doc.bind(page.addCert, 'click', () => page.certFile.click())
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
    page.selectedCert.textContent = intl.prep(intl.ID_NONE_SELECTED)
    Doc.hide(page.removeCert)
    Doc.show(page.addCert)
  }

  async file (): Promise<string> {
    const page = this.page
    if (page.certFile.value) {
      const files = page.certFile.files
      if (files && files.length) {
        return await files[0].text()
      }
    }
    return ''
  }
}

export class TokenApprovalForm {
  page: Record<string, PageElement>
  success?: () => void
  assetID: number
  parentID: number
  txFee: number
  host: string

  constructor (parent: PageElement, success?: () => void) {
    this.page = Doc.parseTemplate(parent)
    this.success = success
    Doc.bind(this.page.submit, 'click', () => { this.approve() })
  }

  async setAsset (assetID: number, host: string) {
    this.assetID = assetID
    this.host = host
    const tokenAsset = app().assets[assetID]
    const parentID = this.parentID = tokenAsset.token?.parentID as number
    const { page } = this

    Doc.show(page.submissionElements)
    Doc.hide(page.txMsg, page.errMsg, page.addressBox, page.balanceBox, page.addressBox)
    Doc.setVis(!State.passwordIsCached(), page.pwBox)
    page.pw.value = ''

    Doc.empty(page.tokenSymbol)
    page.tokenSymbol.appendChild(Doc.symbolize(tokenAsset, true))
    const protocolVersion = app().exchanges[host].assets[assetID].version
    const res = await postJSON('/api/approvetokenfee', {
      assetID: tokenAsset.id,
      version: protocolVersion,
      approving: true
    })
    if (!app().checkResponse(res)) {
      page.errMsg.textContent = res.msg
      Doc.show(page.errMsg)
    } else {
      const { unitInfo: ui, wallet: { address, balance: { available: avail } }, name: parentName } = app().assets[parentID]
      const txFee = this.txFee = res.txFee as number
      let feeText = `${Doc.formatCoinValue(txFee, ui)} ${ui.conventional.unit}`
      const rate = app().fiatRatesMap[parentID]
      if (rate) {
        feeText += ` (${Doc.formatFiatConversion(txFee, rate, ui)} USD)`
      }
      page.feeEstimate.textContent = feeText
      Doc.show(page.balanceBox)
      page.balance.textContent = Doc.formatCoinValue(avail, ui)
      page.parentTicker.textContent = ui.conventional.unit
      page.parentName.textContent = parentName
      if (avail < txFee) {
        Doc.show(page.addressBox)
        page.address.textContent = address
      }
    }
  }

  /*
   * approve calls the /api/approvetoken endpoint.
   */
  async approve () {
    const { page, assetID, host, success } = this
    const path = '/api/approvetoken'
    const tokenAsset = app().assets[assetID]
    const res = await postJSON(path, {
      assetID: tokenAsset.id,
      dexAddr: host,
      pass: page.pw.value
    })
    if (!app().checkResponse(res)) {
      page.errMsg.textContent = res.msg
      Doc.show(page.errMsg)
      return
    }
    page.txid.innerText = res.txID
    const assetExplorer = CoinExplorers[tokenAsset.id]
    if (assetExplorer && assetExplorer[app().user.net]) {
      page.txid.href = assetExplorer[app().user.net](res.txID)
    }
    Doc.hide(page.submissionElements, page.balanceBox, page.addressBox)
    Doc.show(page.txMsg)
    if (success) success()
  }

  handleBalanceNote (n: BalanceNote) {
    const { page, parentID, txFee } = this
    if (n.assetID !== parentID) return
    page.balance.textContent = Doc.formatCoinValue(n.balance.available, app().assets[parentID].unitInfo)
    if (n.balance.available >= txFee) {
      Doc.hide(page.addressBox)
    } else Doc.hide(page.errMsg)
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

export function showSuccess (page: Record<string, PageElement>, msg: string) {
  page.successMessage.textContent = msg
  Doc.show(page.forms, page.checkmarkForm)
  page.checkmarkForm.style.right = '0'
  page.checkmark.style.fontSize = '0px'

  const [startR, startG, startB] = State.isDark() ? [223, 226, 225] : [51, 51, 51]
  const [endR, endG, endB] = [16, 163, 16]
  const [diffR, diffG, diffB] = [endR - startR, endG - startG, endB - startB]

  return new Animation(1200, (prog: number) => {
    page.checkmark.style.fontSize = `${prog * 80}px`
    page.checkmark.style.color = `rgb(${startR + prog * diffR}, ${startG + prog * diffG}, ${startB + prog * diffB})`
  }, 'easeOutElastic')
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

// toUnixDate converts a javascript date object to a unix date, which is
// the number of *seconds* since the start of the epoch.
function toUnixDate (date: Date) {
  return Math.floor(date.getTime() / 1000)
}

// dateApplyOffset shifts a date by the timezone offset. This is used to make
// UTC dates show the local date. This can be used to prepare a Date so
// toISOString generates a local date string. This is also used to trick an html
// input element to show the local date when setting the valueAsDate field. When
// reading the date back to JS, the value field should be interpreted as local
// using the "T00:00" suffix, or the Date in valueAsDate should be shifted in
// the opposite direction.
function dateApplyOffset (date: Date) {
  return new Date(date.getTime() - date.getTimezoneOffset() * 60 * 1000)
}

// dateToString converts a javascript date object to a YYYY-MM-DD format string,
// in the local time zone.
function dateToString (date: Date) {
  return dateApplyOffset(date).toISOString().split('T')[0]
  // Another common hack:
  // date.toLocaleString("sv-SE", { year: "numeric", month: "2-digit", day: "2-digit" })
}
