import Doc from './doc'
import BasePage from './basepage'
import State from './state'
import { postJSON } from './http'
import * as forms from './forms'

const animationLength = 300

let app

export default class SettingsPage extends BasePage {
  constructor (application, body) {
    super()
    app = application
    this.body = body
    const page = this.page = Doc.parsePage(body, [
      'darkMode', 'commitHash',
      'addADex',
      // Form to configure DEX server
      'dexAddrForm', 'dexAddr', 'certFile', 'selectedCert', 'removeCert', 'addCert',
      'submitDEXAddr', 'dexAddrErr',
      // Form to confirm DEX registration and pay fee
      'forms', 'confirmRegForm', 'feeDisplay', 'dexDCRLotSize', 'appPass', 'submitConfirm', 'regErr',
      // Export Account
      'exchanges', 'authorizeAccountExportForm', 'exportAccountAppPass', 'authorizeExportAccountConfirm',
      'exportAccountHost', 'exportAccountErr',
      // Import Account
      'importAccount', 'authorizeAccountImportForm', 'authorizeImportAccountConfirm', 'importAccountAppPass',
      'accountFile', 'selectedAccount', 'removeAccount', 'addAccount', 'importAccountErr',
      // Disable Account
      'disableAccount', 'disableAccountForm', 'disableAccountConfirm', 'disableAccountAppPW',
      'disableAccountHost', 'disableAccountErr',
      // Change App Password
      'changeAppPW', 'changeAppPWForm', 'appPW', 'newAppPW', 'confirmNewPW',
      'submitNewPW', 'changePWErrMsg',
      // Others
      'showPokes'
    ])

    Doc.bind(page.darkMode, 'click', () => {
      State.dark(page.darkMode.checked)
      if (page.darkMode.checked) {
        document.body.classList.add('dark')
      } else {
        document.body.classList.remove('dark')
      }
    })

    Doc.bind(page.showPokes, 'click', () => {
      const show = page.showPokes.checked
      State.setCookie('popups', show ? '1' : '0')
      app.showPopups = show
    })

    page.commitHash.textContent = app.commitHash.substring(0, 7)
    Doc.bind(page.addADex, 'click', () => this.showForm(page.dexAddrForm))
    Doc.bind(page.certFile, 'change', () => this.onCertFileChange())
    Doc.bind(page.removeCert, 'click', () => this.clearCertFile())
    Doc.bind(page.addCert, 'click', () => this.page.certFile.click())
    forms.bind(page.dexAddrForm, page.submitDEXAddr, () => this.verifyDEX())
    forms.bind(page.confirmRegForm, page.submitConfirm, () => this.registerDEX())
    forms.bind(page.authorizeAccountExportForm, page.authorizeExportAccountConfirm, () => this.exportAccount())
    forms.bind(page.disableAccountForm, page.disableAccountConfirm, () => this.disableAccount())

    const exchangesDiv = page.exchanges
    if (typeof app.user.exchanges !== 'undefined') {
      for (const host of Object.keys(app.user.exchanges)) {
        // Bind export account button click event.
        const exportAccountButton = Doc.tmplElement(exchangesDiv, `exportAccount-${host}`)
        Doc.bind(exportAccountButton, 'click', () => this.prepareAccountExport(host, page.authorizeAccountExportForm))
        // Bind disable account button click event.
        const disableAccountButton = Doc.tmplElement(exchangesDiv, `disableAccount-${host}`)
        Doc.bind(disableAccountButton, 'click', () => this.prepareAccountDisable(host, page.disableAccountForm))
      }
    }

    Doc.bind(page.importAccount, 'click', () => this.prepareAccountImport(page.authorizeAccountImportForm))
    forms.bind(page.authorizeAccountImportForm, page.authorizeImportAccountConfirm, () => this.importAccount())

    Doc.bind(page.changeAppPW, 'click', () => this.showForm(page.changeAppPWForm))
    forms.bind(page.changeAppPWForm, page.submitNewPW, () => this.changeAppPW())

    Doc.bind(page.accountFile, 'change', () => this.onAccountFileChange())
    Doc.bind(page.removeAccount, 'click', () => this.clearAccountFile())
    Doc.bind(page.addAccount, 'click', () => this.page.accountFile.click())

    const closePopups = () => {
      Doc.hide(page.forms)
      page.appPass.value = ''
    }

    Doc.bind(page.forms, 'mousedown', e => {
      if (!Doc.mouseInElement(e, this.currentForm)) { closePopups() }
    })

    this.keyup = e => {
      if (e.key === 'Escape') {
        closePopups()
      }
    }
    Doc.bind(document, 'keyup', this.keyup)

    page.forms.querySelectorAll('.form-closer').forEach(el => {
      Doc.bind(el, 'click', () => { closePopups() })
    })
  }

  async prepareAccountExport (host, authorizeAccountExportForm) {
    const page = this.page
    page.exportAccountHost.textContent = host
    page.exportAccountErr.textContent = ''
    this.showForm(authorizeAccountExportForm)
  }

  async prepareAccountDisable (host, disableAccountForm) {
    const page = this.page
    page.disableAccountHost.textContent = host
    page.disableAccountErr.textContent = ''
    this.showForm(disableAccountForm)
  }

  // exportAccount exports and downloads the account info.
  async exportAccount () {
    const page = this.page
    const pw = page.exportAccountAppPass.value
    const host = page.exportAccountHost.textContent
    page.exportAccountAppPass.value = ''
    const req = {
      pw,
      host
    }
    const loaded = app.loading(this.body)
    const res = await postJSON('/api/exportaccount', req)
    loaded()
    if (!app.checkResponse(res)) {
      page.exportAccountErr.textContent = res.msg
      Doc.show(page.exportAccountErr)
      return
    }
    const accountForExport = JSON.parse(JSON.stringify(res.account))
    const a = document.createElement('a')
    a.setAttribute('download', 'dcrAccount-' + host + '.json')
    a.setAttribute('href', 'data:text/json,' + JSON.stringify(accountForExport, null, 2))
    a.click()
    Doc.hide(page.forms)
  }

  // disableAccount disables the account associated with the provided host.
  async disableAccount () {
    const page = this.page
    const pw = page.disableAccountAppPW.value
    const host = page.disableAccountHost.textContent
    page.disableAccountAppPW.value = ''
    const req = {
      pw,
      host
    }
    const loaded = app.loading(this.body)
    const res = await postJSON('/api/disableaccount', req)
    loaded()
    if (!app.checkResponse(res, true)) {
      page.disableAccountErr.textContent = res.msg
      Doc.show(page.disableAccountErr)
      return
    }
    Doc.hide(page.forms)
    // Initial method of removing disabled account.
    window.location.reload()
  }

  async onAccountFileChange () {
    const page = this.page
    const files = page.accountFile.files
    if (!files.length) return
    page.selectedAccount.textContent = files[0].name
    Doc.show(page.removeAccount)
    Doc.hide(page.addAccount)
  }

  /* clearAccountFile cleanup accountFile value and selectedAccount text */
  clearAccountFile () {
    const page = this.page
    page.accountFile.value = ''
    page.selectedAccount.textContent = this.defaultTLSText
    Doc.hide(page.removeAccount)
    Doc.show(page.addAccount)
  }

  async prepareAccountImport (authorizeAccountImportForm) {
    const page = this.page
    page.importAccountErr.textContent = ''
    this.showForm(authorizeAccountImportForm)
  }

  // importAccount imports the account
  async importAccount () {
    const page = this.page
    const pw = page.importAccountAppPass.value
    page.importAccountAppPass.value = ''
    let accountString = ''
    if (page.accountFile.value) {
      accountString = await page.accountFile.files[0].text()
    }
    let account
    try {
      account = JSON.parse(accountString)
    } catch (e) {
      page.importAccountErr.textContent = e.message
      Doc.show(page.importAccountErr)
      return
    }
    if (typeof account === 'undefined') {
      page.importAccountErr.textContent = 'Account undefined.'
      Doc.show(page.importAccountErr)
      return
    }
    const req = {
      pw: pw,
      account: account
    }
    const loaded = app.loading(this.body)
    const importResponse = await postJSON('/api/importaccount', req)
    loaded()
    if (!app.checkResponse(importResponse)) {
      page.importAccountErr.textContent = importResponse.msg
      Doc.show(page.importAccountErr)
      return
    }
    const loginResponse = await postJSON('/api/login', { pass: pw })
    if (!app.checkResponse(loginResponse)) {
      page.importAccountErr.textContent = loginResponse.msg
      Doc.show(page.importAccountErr)
      return
    }
    await app.fetchUser()
    Doc.hide(page.forms)
    // Initial method of displaying imported account.
    window.location.reload()
  }

  /* showForm shows a modal form with a little animation. */
  async showForm (form) {
    const page = this.page
    this.currentForm = form
    Doc.hide(page.dexAddrForm, page.confirmRegForm,
      page.authorizeAccountExportForm, page.authorizeAccountImportForm,
      page.disableAccountForm, page.changeAppPWForm)
    form.style.right = '10000px'
    Doc.show(page.forms, form)
    const shift = (page.forms.offsetWidth + form.offsetWidth) / 2
    await Doc.animate(animationLength, progress => {
      form.style.right = `${(1 - progress) * shift}px`
    }, 'easeOutHard')
    form.style.right = '0'
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

  /* Get the reg fees for the DEX. */
  async verifyDEX () {
    const page = this.page
    Doc.hide(page.dexAddrErr)
    const addr = page.dexAddr.value
    if (addr === '') {
      page.dexAddrErr.textContent = 'URL cannot be empty'
      Doc.show(page.dexAddrErr)
      return
    }

    let cert = ''
    if (page.certFile.value) {
      cert = await page.certFile.files[0].text()
    }

    const loaded = app.loading(page.dexAddrForm)
    const res = await postJSON('/api/getdexinfo', {
      addr: addr,
      cert: cert
    })
    loaded()
    if (!app.checkResponse(res)) {
      page.dexAddrErr.textContent = res.msg
      Doc.show(page.dexAddrErr)
      return
    }
    this.fee = res.xc.feeAsset.amount

    page.feeDisplay.textContent = Doc.formatCoinValue(this.fee / 1e8)
    const dcrAsset = res.xc.assets['42']
    if (dcrAsset) page.dexDCRLotSize.textContent = Doc.formatCoinValue(dcrAsset.lotSize / 1e8)
    await this.showForm(page.confirmRegForm)
  }

  /* Authorize DEX registration. */
  async registerDEX () {
    const page = this.page
    Doc.hide(page.regErr)
    let cert = ''
    if (page.certFile.value) {
      cert = await page.certFile.files[0].text()
    }
    const registration = {
      addr: page.dexAddr.value,
      pass: page.appPass.value,
      fee: this.fee,
      cert: cert
    }
    page.appPass.value = ''
    const loaded = app.loading(page.confirmRegForm)
    const res = await postJSON('/api/register', registration)
    if (!app.checkResponse(res)) {
      page.regErr.textContent = res.msg
      Doc.show(page.regErr)
      loaded()
      return
    }
    page.dexAddr.value = ''
    this.clearCertFile()
    Doc.hide(page.forms)
    await app.fetchUser()
    loaded()
    // Initial method of displaying added dex.
    window.location.reload()
  }

  /* Change application password  */
  async changeAppPW () {
    const page = this.page
    Doc.hide(page.changePWErrMsg)

    const clearValues = () => {
      page.appPW.value = ''
      page.newAppPW.value = ''
      page.confirmNewPW.value = ''
    }
    // Ensure password fields are nonempty.
    if (!page.appPW.value || !page.newAppPW.value || !page.confirmNewPW.value) {
      page.changePWErrMsg.textContent = 'app password cannot be empty'
      Doc.show(page.changePWErrMsg)
      clearValues()
      return
    }
    // Ensure password confirmation matches.
    if (page.newAppPW.value !== page.confirmNewPW.value) {
      page.changePWErrMsg.textContent = 'password confirmation doesn\'t match'
      Doc.show(page.changePWErrMsg)
      clearValues()
      return
    }
    const loaded = app.loading(page.changeAppPW)
    const req = {
      appPW: page.appPW.value,
      newAppPW: page.newAppPW.value
    }
    clearValues()
    const res = await postJSON('/api/changeapppass', req)
    loaded()
    if (!app.checkResponse(res, true)) {
      page.changePWErrMsg.textContent = res.msg
      Doc.show(page.changePWErrMsg)
      return
    }
    Doc.hide(page.forms)
  }

  /*
   * unload is called by the Application when the user navigates away from
   * the /settings page.
   */
  unload () {
    Doc.unbind(document, 'keyup', this.keyup)
  }
}
