import Doc from './doc'
import { postJSON } from './http'

var configInputField = (option, idx) => {
  const id = `wcfg-${idx}`
  return `
  <div>
    <label for="${id}" class="pl-1 mb-1 small">${option.displayname || option.key}</label>
    <input id="${id}" type="text" class="form-control select" data-cfgkey="${option.key}" placeholder="${option.description}">
  </div>
  `
}

/*
 * bindNewWallet should be used with the "newWalletForm" template. The enclosing
 * <form> element should be the second argument.
 */
export function bindNewWallet (app, form, success) {
  const fields = Doc.parsePage(form, [
    'nwAssetLogo', 'nwAssetName',
    'acctName', 'newWalletPass', 'nwAppPass',
    'walletSettings', 'walletSettingsInputs', 'selectCfgFile',
    'walletConfig', 'cfgFile', 'selectedCfgFile', 'removeCfgFile',
    'submitAdd', 'newWalletErr'
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
    fields.nwAssetLogo.src = Doc.logoPath(asset.symbol)
    fields.nwAssetName.textContent = asset.info.name
    fields.acctName.value = ''
    fields.newWalletPass.value = ''
    resetWalletSettingsForm(asset.info.configopts)
    Doc.hide(fields.newWalletErr)
  }
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
    var config = ''
    if (fields.cfgFile.value) {
      config = await fields.cfgFile.files[0].text()
    } else {
      config = currentAsset.info.configopts.map(option => {
        const value = form.querySelector(`input[data-cfgkey="${option.key}"]`).value
        return `${option.key}=${value}`
      }).join('\n')
    }
    const create = {
      assetID: parseInt(currentAsset.id),
      pass: fields.newWalletPass.value,
      account: fields.acctName.value,
      config: config,
      appPass: fields.nwAppPass.value
    }
    fields.nwAppPass.value = ''
    app.loading(form)
    var res = await postJSON('/api/newwallet', create)
    app.loaded()
    if (!app.checkResponse(res)) {
      fields.newWalletErr.textContent = res.msg
      Doc.show(fields.newWalletErr)
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
