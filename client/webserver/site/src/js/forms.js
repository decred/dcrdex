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
    this.subform = new WalletConfigForm(application, fields.walletSettings)

    bind(form, fields.submitAdd, async () => {
      if (fields.newWalletPass.value === '') {
        fields.newWalletErr.textContent = 'wallet password cannot be empty'
        Doc.show(fields.newWalletErr)
        return
      }
      if (fields.nwAppPass.value === '') {
        fields.newWalletErr.textContent = 'app password cannot be empty'
        Doc.show(fields.newWalletErr)
        return
      }
      Doc.hide(fields.newWalletErr)

      const createForm = {
        assetID: parseInt(this.currentAsset.id),
        pass: fields.newWalletPass.value,
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
    this.subform.setConfig(res.config)
  }
}

/*
 * WalletConfigForm is a dynamically generated sub-form for setting
 * asset-specific wallet configuration options.
*/
export class WalletConfigForm {
  constructor (application, form) {
    app = application
    this.form = form

    // Get template elements
    this.dynamicOpts = Doc.tmplElement(form, 'dynamicOpts')
    this.textInputTmpl = Doc.tmplElement(form, 'textInput')
    this.textInputTmpl.remove()
    this.checkboxTmpl = Doc.tmplElement(form, 'checkbox')
    this.checkboxTmpl.remove()
    this.fileSelector = Doc.tmplElement(form, 'fileSelector')
    this.fileInput = Doc.tmplElement(form, 'fileInput')
    this.errMsg = Doc.tmplElement(form, 'errMsg')

    Doc.bind(this.fileSelector, 'click', () => this.fileInput.click())

    // config file upload
    Doc.bind(this.fileInput, 'change', async () => {
      if (!this.fileInput.value) return
      app.loading(form)
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
      this.setConfig(res.map)
    })
  }

  update (walletInfo) {
    Doc.empty(this.dynamicOpts)
    for (const opt of walletInfo.configopts) {
      const elID = 'wcfg-' + opt.key
      const el = opt.isboolean ? this.checkboxTmpl.cloneNode(true) : this.textInputTmpl.cloneNode(true)
      const input = el.querySelector('input')
      input.id = elID
      input.configOpt = opt
      const label = el.querySelector('label')
      label.htmlFor = elID // 'for' attribute, but 'for' is a keyword
      label.prepend(opt.displayname)
      this.dynamicOpts.appendChild(el)
      if (opt.noecho) input.type = 'password'
      if (opt.description) label.dataset.tooltip = opt.description
      if (opt.isboolean) input.checked = opt.default
      else input.value = opt.default ? opt.default : ''
    }
    Doc.hide(this.errMsg)
    app.bindTooltips(this.form)
  }

  setConfig (configMap) {
    this.dynamicOpts.querySelectorAll('input').forEach(input => {
      const v = configMap[input.configOpt.key]
      if (!v) return
      if (input.configOpt.isboolean) input.checked = isTruthyString(v)
      else input.value = v
    })
  }

  map () {
    const config = {}
    this.dynamicOpts.querySelectorAll('input').forEach(input => {
      if (input.configOpt.isboolean && input.configOpt.key) config[input.configOpt.key] = input.checked ? '1' : '0'
      else if (input.value) config[input.configOpt.key] = input.value
    })

    return config
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
