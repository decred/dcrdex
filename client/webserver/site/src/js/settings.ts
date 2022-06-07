import Doc from './doc'
import BasePage from './basepage'
import State from './state'
import { postJSON } from './http'
import * as forms from './forms'
import * as intl from './locales'
import {
  app,
  Exchange,
  PageElement,
  PasswordCache,
  WalletStateNote,
  BalanceNote
} from './registry'

const animationLength = 300

export default class SettingsPage extends BasePage {
  body: HTMLElement
  currentDEX: Exchange
  page: Record<string, PageElement>
  forms: PageElement[]
  regAssetForm: forms.FeeAssetSelectionForm
  confirmRegisterForm: forms.ConfirmRegistrationForm
  newWalletForm: forms.NewWalletForm
  walletWaitForm: forms.WalletWaitForm
  dexAddrForm: forms.DEXAddressForm
  currentForm: PageElement
  pwCache: PasswordCache
  defaultTLSText: string
  keyup: (e: KeyboardEvent) => void

  constructor (body: HTMLElement) {
    super()
    this.body = body
    this.defaultTLSText = 'none selected'
    const page = this.page = Doc.idDescendants(body)

    this.forms = Doc.applySelector(page.forms, ':scope > form')

    Doc.bind(page.darkMode, 'click', () => {
      State.dark(page.darkMode.checked || false)
      if (page.darkMode.checked) {
        document.body.classList.add('dark')
      } else {
        document.body.classList.remove('dark')
      }
    })

    Doc.bind(page.showPokes, 'click', () => {
      const show = page.showPokes.checked || false
      State.setCookie('popups', show ? '1' : '0')
      app().showPopups = show
    })

    page.commitHash.textContent = app().commitHash.substring(0, 7)
    Doc.bind(page.addADex, 'click', () => {
      this.dexAddrForm.refresh()
      this.showForm(page.dexAddrForm)
    })

    // Asset selection
    this.regAssetForm = new forms.FeeAssetSelectionForm(page.regAssetForm, async assetID => {
      this.confirmRegisterForm.setAsset(assetID)

      const asset = app().assets[assetID]
      const wallet = asset.wallet
      if (wallet) {
        const fee = this.currentDEX.regFees[asset.symbol]
        if (wallet.synced && wallet.balance.available > fee.amount) {
          this.animateConfirmForm(page.regAssetForm)
          return
        }
        const txFee = await this.getRegistrationTxFeeEstimate(assetID, page.regAssetForm)
        this.walletWaitForm.setWallet(wallet, txFee)
        forms.slideSwap(page.regAssetForm, page.walletWait)
        return
      }

      this.newWalletForm.setAsset(assetID)
      this.newWalletForm.loadDefaults()
      this.currentForm = page.newWalletForm
      forms.slideSwap(page.regAssetForm, page.newWalletForm)
    })

    // Approve fee payment
    this.confirmRegisterForm = new forms.ConfirmRegistrationForm(page.confirmRegForm, () => {
      this.registerDEXSuccess()
    }, () => {
      this.animateRegAsset(page.confirmRegForm)
    }, this.pwCache)

    // Create a new wallet
    this.newWalletForm = new forms.NewWalletForm(
      page.newWalletForm,
      assetID => this.newWalletCreated(assetID),
      this.pwCache,
      () => this.animateRegAsset(page.newWalletForm)
    )

    this.walletWaitForm = new forms.WalletWaitForm(page.walletWait, () => {
      this.animateConfirmForm(page.walletWait)
    }, () => { this.animateRegAsset(page.walletWait) })

    // Enter an address for a new DEX
    this.dexAddrForm = new forms.DEXAddressForm(page.dexAddrForm, async (xc: Exchange, certFile: string) => {
      this.currentDEX = xc
      this.confirmRegisterForm.setExchange(xc, certFile)
      this.walletWaitForm.setExchange(xc)
      this.regAssetForm.setExchange(xc)
      this.animateRegAsset(page.dexAddrForm)
    })

    Doc.bind(page.importAccount, 'click', () => this.prepareAccountImport(page.authorizeAccountImportForm))
    forms.bind(page.authorizeAccountImportForm, page.authorizeImportAccountConfirm, () => this.importAccount())

    Doc.bind(page.changeAppPW, 'click', () => this.showForm(page.changeAppPWForm))
    forms.bind(page.changeAppPWForm, page.submitNewPW, () => this.changeAppPW())

    Doc.bind(page.accountFile, 'change', () => this.onAccountFileChange())
    Doc.bind(page.removeAccount, 'click', () => this.clearAccountFile())
    Doc.bind(page.addAccount, 'click', () => page.accountFile.click())

    Doc.bind(page.exportSeed, 'click', () => this.showForm(page.exportSeedAuth))
    forms.bind(page.exportSeedAuth, page.exportSeedSubmit, () => this.submitExportSeedReq())

    const closePopups = () => {
      Doc.hide(page.forms)
      page.exportSeedPW.value = ''
      page.seedDiv.textContent = ''
    }

    Doc.bind(page.forms, 'mousedown', (e: MouseEvent) => {
      if (!Doc.mouseInElement(e, this.currentForm)) { closePopups() }
    })

    this.keyup = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        closePopups()
      }
    }
    Doc.bind(document, 'keyup', this.keyup)

    page.forms.querySelectorAll('.form-closer').forEach(el => {
      Doc.bind(el, 'click', () => { closePopups() })
    })

    this.notifiers = {
      walletstate: (note: WalletStateNote) => this.walletWaitForm.reportWalletState(note.wallet),
      balance: (note: BalanceNote) => this.walletWaitForm.reportBalance(note.balance, note.assetID)
    }
  }

  // Retrieve an estimate for the tx fee needed to pay the registration fee.
  async getRegistrationTxFeeEstimate (assetID: number, form: HTMLElement) {
    const cert = await this.getCertFile()
    const loaded = app().loading(form)
    const res = await postJSON('/api/regtxfee', {
      addr: this.currentDEX.host,
      cert: cert,
      asset: assetID
    })
    loaded()
    if (!app().checkResponse(res, true)) {
      return 0
    }
    return res.txfee
  }

  async newWalletCreated (assetID: number) {
    const user = await app().fetchUser()
    if (!user) return
    const page = this.page
    const asset = user.assets[assetID]
    const wallet = asset.wallet
    const feeAmt = this.currentDEX.regFees[asset.symbol].amount

    if (wallet.synced && wallet.balance.available > feeAmt) {
      await this.animateConfirmForm(page.newWalletForm)
      return
    }

    const txFee = await this.getRegistrationTxFeeEstimate(assetID, page.newWalletForm)
    this.walletWaitForm.setWallet(wallet, txFee)
    this.currentForm = page.walletWait
    await forms.slideSwap(page.newWalletForm, page.walletWait)
  }

  async onAccountFileChange () {
    const page = this.page
    const files = page.accountFile.files
    if (!files || !files.length) return
    page.selectedAccount.textContent = files[0].name
    Doc.show(page.removeAccount)
    Doc.hide(page.addAccount)
  }

  /* clearAccountFile cleanup accountFile value and selectedAccount text */
  clearAccountFile () {
    const page = this.page
    page.accountFile.value = ''
    page.selectedAccount.textContent = 'none selected'
    Doc.hide(page.removeAccount)
    Doc.show(page.addAccount)
  }

  async prepareAccountImport (authorizeAccountImportForm: HTMLElement) {
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
      const files = page.accountFile.files
      if (!files || !files.length) {
        console.error('importAccount: no file specified')
        return
      }
      accountString = await files[0].text()
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
      page.importAccountErr.textContent = intl.prep(intl.ID_ACCT_UNDEFINED)
      Doc.show(page.importAccountErr)
      return
    }
    const req = {
      pw: pw,
      account: account
    }
    const loaded = app().loading(this.body)
    const importResponse = await postJSON('/api/importaccount', req)
    loaded()
    if (!app().checkResponse(importResponse)) {
      page.importAccountErr.textContent = importResponse.msg
      Doc.show(page.importAccountErr)
      return
    }
    const loginResponse = await postJSON('/api/login', { pass: pw })
    if (!app().checkResponse(loginResponse)) {
      page.importAccountErr.textContent = loginResponse.msg
      Doc.show(page.importAccountErr)
      return
    }
    await app().fetchUser()
    Doc.hide(page.forms)
    // Initial method of displaying imported account.
    window.location.reload()
  }

  async submitExportSeedReq () {
    const page = this.page
    const pw = page.exportSeedPW.value
    const loaded = app().loading(this.body)
    const res = await postJSON('/api/exportseed', { pass: pw })
    loaded()
    if (!app().checkResponse(res)) {
      page.exportAccountErr.textContent = res.msg
      Doc.show(page.exportSeedE)
      return
    }
    page.exportSeedPW.value = ''
    page.seedDiv.textContent = res.seed
    this.showForm(page.authorizeSeedDisplay)
  }

  /* showForm shows a modal form with a little animation. */
  async showForm (form: HTMLElement) {
    const page = this.page
    this.currentForm = form
    this.forms.forEach(form => Doc.hide(form))
    form.style.right = '10000px'
    Doc.show(page.forms, form)
    const shift = (page.forms.offsetWidth + form.offsetWidth) / 2
    await Doc.animate(animationLength, progress => {
      form.style.right = `${(1 - progress) * shift}px`
    }, 'easeOutHard')
    form.style.right = '0'
  }

  /* gets the contents of the cert file */
  async getCertFile () {
    let cert = ''
    if (this.dexAddrForm.page.certFile.value) {
      const files = this.dexAddrForm.page.certFile.files
      if (files && files.length) cert = await files[0].text()
    }
    return cert
  }

  /* gets the dex address input by the user */
  getDexAddr () {
    const page = this.page
    return page.dexAddr.value
  }

  /* Called after successful registration to a DEX. */
  async registerDEXSuccess () {
    const page = this.page
    Doc.hide(page.forms)
    await app().fetchUser()
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
      page.changePWErrMsg.textContent = intl.prep(intl.ID_NO_APP_PASS_ERROR_MSG)
      Doc.show(page.changePWErrMsg)
      clearValues()
      return
    }
    // Ensure password confirmation matches.
    if (page.newAppPW.value !== page.confirmNewPW.value) {
      page.changePWErrMsg.textContent = intl.prep(intl.ID_PASSWORD_NOT_MATCH)
      Doc.show(page.changePWErrMsg)
      clearValues()
      return
    }
    const loaded = app().loading(page.changeAppPW)
    const req = {
      appPW: page.appPW.value,
      newAppPW: page.newAppPW.value
    }
    clearValues()
    const res = await postJSON('/api/changeapppass', req)
    loaded()
    if (!app().checkResponse(res, true)) {
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

  /* Swap in the asset selection form and run the animation. */
  async animateRegAsset (oldForm: HTMLElement) {
    Doc.hide(oldForm)
    const form = this.page.regAssetForm
    this.currentForm = form
    this.regAssetForm.animate()
    Doc.show(form)
  }

  /* Swap in the confirmation form and run the animation. */
  async animateConfirmForm (oldForm: HTMLElement) {
    this.confirmRegisterForm.animate()
    const form = this.page.confirmRegForm
    this.currentForm = form
    Doc.hide(oldForm)
    Doc.show(form)
  }
}
