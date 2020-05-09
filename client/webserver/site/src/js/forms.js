import Doc from './doc'
import { postJSON } from './http'

var configInputField = (option) => {
  return `
  <div>
    <label for="${option.key}" class="pl-1 mb-1 small">${option.displayname || option.key}</label>
    <input type="text" class="form-control select" placeholder="${option.description}" id="${option.key}">
  </div>
  `
}

/*
 * bindNewWalletForm is used with the "newWalletForm" template. The enclosing
 * <form> element should be the second argument.
 */
export function bindNewWallet (app, form, success) {
  // CREATE DCR WALLET
  // This form is only shown the first time the user visits the /register page.
  const fields = Doc.parsePage(form, [
    'acctName', 'newWalletPass',
    'walletSettings', 'walletSettingsInputs', 'selectCfgFile',
    'walletConfig', 'cfgFile', 'selectedCfgFile', 'removeCfgFile',
    'submitCreate', 'walletErr',
    'newWalletLogo', 'newWalletName', 'wClientPass'
  ])

  // wallet settings form
  const resetWalletSettingsForm = (configOpts) => {
    fields.walletSettingsInputs.innerHTML = configOpts.map(configInputField).join('\n')
    Doc.show(fields.walletSettings)
    Doc.hide(fields.walletConfig)
  }
  Doc.bind(fields.selectCfgFile, 'click', () => fields.cfgFile.click())

  // config file upload
  Doc.bind(fields.cfgFile, 'change', () => {
    const files = fields.cfgFile.files
    fields.selectedCfgFile.textContent = files[0].name
    Doc.show(fields.walletConfig)
    Doc.hide(fields.walletSettings)
  })
  Doc.bind(fields.removeCfgFile, 'click', () => {
    fields.cfgFile.value = ''
    fields.selectedCfgFile.textContent = ''
    Doc.show(fields.walletSettings)
    Doc.hide(fields.walletConfig)
  })

  var currentAsset
  form.setAsset = asset => {
    if (currentAsset && currentAsset.id === asset.id) return
    currentAsset = asset
    fields.newWalletLogo.src = Doc.logoPath(asset.symbol)
    fields.newWalletName.textContent = asset.info.name
    fields.acctName.value = ''
    fields.newWalletPass.value = ''
    resetWalletSettingsForm(asset.info.configopts)
    Doc.hide(fields.walletErr)
  }

  bind(form, fields.submitCreate, async () => {
    Doc.hide(fields.walletErr)
    var config = ''
    if (fields.cfgFile.value) {
      config = await fields.cfgFile.files[0].text()
    } else {
      config = currentAsset.info.configopts.map(option => {
        return `${option.key}=${Doc.idel(form, option.key).value}`
      }).join('\n')
    }
    const create = {
      assetID: parseInt(currentAsset.id),
      pass: fields.newWalletPass.value,
      account: fields.acctName.value,
      config: config,
      appPass: fields.wClientPass.value
    }
    fields.wClientPass.value = ''
    app.loading(form)
    var res = await postJSON('/api/newwallet', create)
    app.loaded()
    if (!app.checkResponse(res)) {
      fields.walletErr.textContent = res.msg
      Doc.show(fields.walletErr)
      return
    }
    fields.newWalletPass.value = ''
    success()
  })
}

/*
 * bindOpenWallet should be used with the "unlockWalletForm" template. The
 * enclosing <form> element should be second argument.
 */
export function bindOpenWallet (app, form, success) {
  const fields = Doc.parsePage(form, [
    'submitOpen', 'openErr', 'walletPass', 'unlockLogo', 'unlockName'
  ])
  var currentAsset
  form.setAsset = asset => {
    currentAsset = asset
    fields.unlockLogo.src = Doc.logoPath(asset.symbol)
    fields.unlockName.textContent = asset.name
    fields.walletPass.value = ''
  }
  bind(form, fields.submitOpen, async () => {
    Doc.hide(fields.openErr)
    const open = {
      assetID: parseInt(currentAsset.id),
      pass: fields.walletPass.value
    }
    fields.walletPass.value = ''
    app.loading(form)
    var res = await postJSON('/api/openwallet', open)
    app.loaded()
    if (!app.checkResponse(res)) {
      fields.openErr.textContent = res.msg
      Doc.show(fields.openErr)
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
