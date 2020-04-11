import Doc from './doc'
import { postJSON } from './http'

/*
 * bindNewWalletForm is used with the "newWalletForm" template. The enclosing
 * <form> element should be the second argument.
 */
export function bindNewWallet (app, form, success) {
  // CREATE DCR WALLET
  // This form is only shown the first time the user visits the /register page.
  const fields = Doc.parsePage(form, [
    'iniPath', 'acctName', 'newWalletPass', 'submitCreate', 'walletErr',
    'newWalletLogo', 'newWalletName', 'wClientPass'
  ])
  var currentAsset
  form.setAsset = asset => {
    currentAsset = asset
    fields.newWalletLogo.src = Doc.logoPath(asset.symbol)
    fields.newWalletName.textContent = asset.info.name
  }
  bind(form, fields.submitCreate, async () => {
    Doc.hide(fields.walletErr)
    const create = {
      assetID: parseInt(currentAsset.id),
      pass: fields.newWalletPass.value,
      account: fields.acctName.value,
      inipath: fields.iniPath.value,
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
