import Doc from './doc'
import { postJSON } from './http'

var app

/*
 * bindNewWallet should be used with the "newWalletForm" template. The enclosing
 * <form> element should be the second argument.
 */
export class NewWalletForm {
  constructor (application, form, success) {
    this.form = form
    this.currentAsset = null
    const fields = this.fields = Doc.parsePage(form, [
      'nwAssetLogo', 'nwAssetName', 'newWalletPass', 'nwAppPass',
      'walletSettings', 'selectCfgFile', 'cfgFile', 'submitAdd', 'newWalletErr'
    ])

    // WalletConfigForm will set the global app variable.
    this.subform = new WalletConfigForm(application, fields.walletSettings, true)

    bind(form, fields.submitAdd, async () => {
      if (fields.nwAppPass.value === '') {
        fields.newWalletErr.textContent = 'app password cannot be empty'
        Doc.show(fields.newWalletErr)
        return
      }
      Doc.hide(fields.newWalletErr)

      const createForm = {
        assetID: parseInt(this.currentAsset.id),
        pass: fields.newWalletPass.value || '',
        config: this.subform.map(),
        appPass: fields.nwAppPass.value
      }
      fields.nwAppPass.value = ''
      app.loading(form)
      var res = await postJSON('/api/newwallet', createForm)
      app.loaded()
      if (!app.checkResponse(res)) {
        this.setError(res.msg)
        return
      }
      fields.newWalletPass.value = ''
      success()
    })
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
    app.loading(this.form)
    var res = await postJSON('/api/defaultwalletcfg', { assetID: this.currentAsset.id })
    app.loaded()
    if (!app.checkResponse(res)) {
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
    if (!this.fileInput.value) return
    app.loading(this.form)
    const config = await this.fileInput.files[0].text()
    if (!config) return
    const res = await postJSON('/api/parseconfig', {
      configtext: config
    })
    app.loaded()
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
      this.showHideMsg.textContent = 'hide additional settings'
      return
    }
    Doc.hide(this.hideIcon, this.otherSettings)
    Doc.show(this.showIcon)
    this.showHideMsg.textContent = 'show additional settings'
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
 * bindOpenWallet should be used with the "unlockWalletForm" template. The
 * enclosing <form> element should be second argument.
 */
export function bindOpenWallet (app, form, success) {
  const fields = Doc.parsePage(form, [
    'uwAssetLogo', 'uwAssetName',
    'uwAppPass', 'submitUnlock', 'unlockErr'
  ])
  var currentAsset
  form.setAsset = asset => {
    currentAsset = asset
    fields.uwAssetLogo.src = Doc.logoPath(asset.symbol)
    fields.uwAssetName.textContent = asset.info.name
    fields.uwAppPass.value = ''
  }
  bind(form, fields.submitUnlock, async () => {
    if (fields.uwAppPass.value === '') {
      fields.unlockErr.textContent = 'app password cannot be empty'
      Doc.show(fields.unlockErr)
      return
    }
    Doc.hide(fields.unlockErr)
    const open = {
      assetID: parseInt(currentAsset.id),
      pass: fields.uwAppPass.value
    }
    fields.uwAppPass.value = ''
    app.loading(form)
    var res = await postJSON('/api/openwallet', open)
    app.loaded()
    if (!app.checkResponse(res)) {
      fields.unlockErr.textContent = res.msg
      Doc.show(fields.unlockErr)
      return
    }
    success()
  })
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
