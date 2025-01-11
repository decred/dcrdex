import Doc from './doc'
import BasePage from './basepage'
import State from './state'
import { postJSON } from './http'
import * as forms from './forms'
import * as intl from './locales'
import { setCoinHref } from './coinexplorers'
import {
  updateNtfnSetting,
  DesktopNtfnSetting,
  fetchDesktopNtfnSettings,
  desktopNtfnLabels,
  Notifier
} from './notifications'
import {
  app,
  Exchange,
  PageElement,
  PrepaidBondID
} from './registry'

const animationLength = 300

export default class SettingsPage extends BasePage {
  body: HTMLElement
  currentDEX: Exchange
  page: Record<string, PageElement>
  forms: PageElement[]
  fiatRateSources: PageElement[]
  regAssetForm: forms.FeeAssetSelectionForm
  confirmRegisterForm: forms.ConfirmRegistrationForm
  newWalletForm: forms.NewWalletForm
  walletWaitForm: forms.WalletWaitForm
  dexAddrForm: forms.DEXAddressForm
  appPassResetForm: forms.AppPassResetForm
  currentForm: PageElement
  keyup: (e: KeyboardEvent) => void

  constructor (body: HTMLElement) {
    super()
    this.body = body
    const page = this.page = Doc.idDescendants(body)

    this.forms = Doc.applySelector(page.forms, ':scope > form')
    this.fiatRateSources = Doc.applySelector(page.fiatRateSources, 'input[type=checkbox]')

    page.darkMode.checked = State.fetchLocal(State.darkModeLK) === '1'
    Doc.bind(page.darkMode, 'click', () => {
      State.storeLocal(State.darkModeLK, page.darkMode.checked || false ? '1' : '0')
      if (page.darkMode.checked) {
        document.body.classList.add('dark')
      } else {
        document.body.classList.remove('dark')
      }
    })

    page.showPokes.checked = State.fetchLocal(State.popupsLK) === '1'
    Doc.bind(page.showPokes, 'click', () => {
      const show = page.showPokes.checked || false
      State.storeLocal(State.popupsLK, show ? '1' : '0')
      app().showPopups = show
    })

    page.commitHash.textContent = app().commitHash.substring(0, 7)
    Doc.bind(page.addADex, 'click', () => {
      this.dexAddrForm.refresh()
      this.showForm(page.dexAddrForm)
    })

    this.fiatRateSources.forEach(src => {
      Doc.bind(src, 'change', async () => {
        const res = await postJSON('/api/toggleratesource', {
          disable: !src.checked,
          source: src.value
        })
        if (!app().checkResponse(res)) {
          src.checked = !src.checked
        }
        // Update asset rate values and disable conversion status.
        await app().fetchUser()
      })
    })

    // Asset selection
    this.regAssetForm = new forms.FeeAssetSelectionForm(page.regAssetForm, async (assetID: number, tier: number) => {
      if (assetID === PrepaidBondID) {
        await app().fetchUser()
        window.location.reload()
        return
      }
      const asset = app().assets[assetID]
      const wallet = asset.wallet
      if (wallet) {
        const bondAsset = this.currentDEX.bondAssets[asset.symbol]
        const bondsFeeBuffer = await this.getBondsFeeBuffer(assetID, page.regAssetForm)
        this.confirmRegisterForm.setAsset(assetID, tier, bondsFeeBuffer)
        if (wallet.synced && wallet.balance.available >= 2 * bondAsset.amount + bondsFeeBuffer) {
          this.animateConfirmForm(page.regAssetForm)
          return
        }
        this.walletWaitForm.setWallet(assetID, bondsFeeBuffer, tier)
        this.slideSwap(page.walletWait)
        return
      }

      this.confirmRegisterForm.setAsset(assetID, tier, 0)
      this.newWalletForm.setAsset(assetID)
      this.slideSwap(page.newWalletForm)
    })

    // Approve fee payment
    this.confirmRegisterForm = new forms.ConfirmRegistrationForm(page.confirmRegForm, () => {
      this.registerDEXSuccess()
    }, () => {
      this.animateRegAsset(page.confirmRegForm)
    })

    // Create a new wallet
    this.newWalletForm = new forms.NewWalletForm(
      page.newWalletForm,
      assetID => this.newWalletCreated(assetID, this.confirmRegisterForm.tier),
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
      this.regAssetForm.setExchange(xc, certFile)
      this.animateRegAsset(page.dexAddrForm)
    })

    Doc.bind(page.importAccount, 'click', () => this.prepareAccountImport(page.authorizeAccountImportForm))
    forms.bind(page.authorizeAccountImportForm, page.authorizeImportAccountConfirm, () => this.importAccount())

    Doc.bind(page.changeAppPW, 'click', () => this.showForm(page.changeAppPWForm))
    forms.bind(page.changeAppPWForm, page.submitNewPW, () => this.changeAppPW())

    this.appPassResetForm = new forms.AppPassResetForm(page.resetAppPWForm, async () => {
      await app().loadPage('login')
      Doc.hide(page.forms)
    })
    Doc.bind(page.resetAppPW, 'click', () => {
      this.appPassResetForm.refresh()
      this.showForm(page.resetAppPWForm)
      this.appPassResetForm.focus()
    })

    Doc.bind(page.companionAppBtn, 'click', () => {
      if (app().onionUrl !== '') {
        Doc.show(page.companionAppTorEnabled)
        Doc.hide(page.companionAppTorDisabled)
        page.companionAppQrcode.src = '/generatecompanionappqrcode'
      } else {
        Doc.hide(page.companionAppTorEnabled)
        Doc.show(page.companionAppTorDisabled)
      }
      this.showForm(page.companionAppForm)
    })

    Doc.bind(page.accountFile, 'change', () => this.onAccountFileChange())
    Doc.bind(page.removeAccount, 'click', () => this.clearAccountFile())
    Doc.bind(page.addAccount, 'click', () => page.accountFile.click())

    Doc.bind(page.exportSeed, 'click', () => {
      Doc.hide(page.exportSeedErr)
      this.showForm(page.exportSeedAuth)
    })
    forms.bind(page.exportSeedAuth, page.exportSeedSubmit, () => this.submitExportSeedReq())

    Doc.bind(page.exportLogs, 'click', () => this.exportLogs())

    Doc.bind(page.gameCodeLink, 'click', () => this.showForm(page.gameCodeForm))
    Doc.bind(page.gameCodeSubmit, 'click', () => this.submitGameCode())

    const closePopups = () => {
      Doc.hide(page.forms)
      page.exportSeedPW.value = ''
      page.legacySeed.textContent = ''
      page.mnemonic.textContent = ''
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

    this.renderDesktopNtfnSettings()
  }

  updateNtfnSetting (e: Event) {
    const checkbox = e.target as HTMLInputElement
    const noteType = checkbox.getAttribute('name')
    if (noteType === null) return
    const enabled = checkbox.checked
    updateNtfnSetting(noteType, enabled)
  }

  getBrowserNtfnSettings (): DesktopNtfnSetting {
    const permissions = fetchDesktopNtfnSettings()
    return permissions
  }

  async renderDesktopNtfnSettings () {
    const page = this.page
    const ntfnSettings = this.getBrowserNtfnSettings()
    const labels = desktopNtfnLabels
    const tmpl = page.browserNtfnCheckboxTemplate
    tmpl.removeAttribute('id')
    const container = page.browserNtfnCheckboxContainer
    Doc.empty(page.browserNtfnCheckboxContainer)

    Object.keys(labels).forEach((noteType) => {
      const html = tmpl.cloneNode(true) as PageElement
      const enabled = ntfnSettings[noteType]
      const checkbox = Doc.tmplElement(html, 'checkbox')
      Doc.tmplElement(html, 'label').textContent = intl.prep(labels[noteType])
      checkbox.setAttribute('name', noteType)
      if (enabled) checkbox.setAttribute('checked', 'checked')
      container.appendChild(html)
      Doc.bind(checkbox, 'click', this.updateNtfnSetting)
    })

    const enabledCheckbox = page.browserNtfnEnabled

    Doc.bind(enabledCheckbox, 'click', async (e: Event) => {
      if (Notifier.ntfnPermissionDenied()) return
      const checkbox = e.target as HTMLInputElement
      if (checkbox.checked) {
        await Notifier.requestNtfnPermission()
        checkbox.checked = !Notifier.ntfnPermissionDenied()
      }
      this.updateNtfnSetting(e)
      checkbox.dispatchEvent(new Event('change'))
    })

    Doc.bind(enabledCheckbox, 'change', (e: Event) => {
      const checkbox = e.target as HTMLInputElement
      const permDenied = Notifier.ntfnPermissionDenied()
      Doc.setVis(checkbox.checked, page.browserNtfnCheckboxContainer)
      Doc.setVis(permDenied, page.browserNtfnBlockedMsg)
      checkbox.disabled = permDenied
    })

    enabledCheckbox.checked = (Notifier.ntfnPermissionGranted() && ntfnSettings.browserNtfnEnabled)
    enabledCheckbox.dispatchEvent(new Event('change'))
  }

  /*
   * slideSwap animates the replacement of the currently shown form with the
   * newForm and sets this.currentForm.
   */
  slideSwap (newForm: PageElement) {
    forms.slideSwap(this.currentForm, newForm)
    this.currentForm = newForm
  }

  // Retrieve an estimate for the tx fee needed to create new bond reserves.
  async getBondsFeeBuffer (assetID: number, form: HTMLElement) {
    const loaded = app().loading(form)
    const res = await postJSON('/api/bondsfeebuffer', { assetID })
    loaded()
    if (!app().checkResponse(res)) {
      return 0
    }
    return res.feeBuffer
  }

  async newWalletCreated (assetID: number, tier: number) {
    const user = await app().fetchUser()
    if (!user) return
    const page = this.page
    const asset = user.assets[assetID]
    const wallet = asset.wallet
    const bondAmt = this.currentDEX.bondAssets[asset.symbol].amount

    const bondsFeeBuffer = await this.getBondsFeeBuffer(assetID, page.newWalletForm)
    this.confirmRegisterForm.setFees(assetID, bondsFeeBuffer)
    if (wallet.synced && wallet.balance.available >= 2 * bondAmt + bondsFeeBuffer) {
      await this.animateConfirmForm(page.newWalletForm)
      return
    }

    this.walletWaitForm.setWallet(assetID, bondsFeeBuffer, tier)
    this.slideSwap(page.walletWait)
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
    page.selectedAccount.textContent = intl.prep(intl.ID_NONE_SELECTED)
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
      Doc.showFormError(page.importAccountErr, intl.prep(intl.ID_ACCT_UNDEFINED))
      return
    }
    const { bonds = [], ...acctInf } = account
    const req = {
      account: acctInf,
      bonds: bonds
    }
    const loaded = app().loading(this.body)
    const res = await postJSON('/api/importaccount', req)
    loaded()
    if (!app().checkResponse(res)) {
      Doc.showFormError(page.importAccountErr, res.msg)
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
      Doc.showFormError(page.exportSeedErr, res.msg)
      return
    }
    page.exportSeedPW.value = ''
    if (res.seed.length === 128 && res.seed.split(' ').length === 1) {
      page.legacySeed.textContent = res.seed.match(/.{1,32}/g).map((chunk: string) => chunk.match(/.{1,8}/g)?.join(' ')).join('\n')
    } else page.mnemonic.textContent = res.seed
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

  /* Called after successful registration to a DEX. */
  async registerDEXSuccess () {
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
      Doc.showFormError(page.changePWErrMsg, intl.prep(intl.ID_NO_APP_PASS_ERROR_MSG))
      clearValues()
      return
    }
    // Ensure password confirmation matches.
    if (page.newAppPW.value !== page.confirmNewPW.value) {
      Doc.showFormError(page.changePWErrMsg, intl.prep(intl.ID_PASSWORD_NOT_MATCH))
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
    if (!app().checkResponse(res)) {
      Doc.showFormError(page.changePWErrMsg, res.msg)
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

  async exportLogs () {
    const url = new URL(window.location.href)
    url.pathname = '/api/exportapplog'
    window.open(url.toString())
  }

  async submitGameCode () {
    const page = this.page
    Doc.hide(page.gameCodeErr)
    const code = page.gameCodeInput.value
    if (!code) {
      page.gameCodeErr.textContent = intl.prep(intl.ID_NO_CODE_PROVIDED)
      Doc.show(page.gameCodeErr)
      return
    }
    const msg = page.gameCodeMsg.value || ''
    const loaded = app().loading(page.gameCodeForm)
    const resp = await postJSON('/api/redeemgamecode', { code, msg })
    loaded()
    if (!app().checkResponse(resp)) {
      page.gameCodeErr.textContent = intl.prep(intl.ID_API_ERROR, { msg: resp.msg })
      Doc.show(page.gameCodeErr)
      return
    }
    Doc.show(page.gameCodeSuccess)
    page.gameRedeemTx.dataset.explorerCoin = resp.coinString
    const dcrBipID = 42
    setCoinHref(dcrBipID, page.gameRedeemTx)
    page.gameRedeemTx.textContent = resp.coinString
    const ui = app().unitInfo(dcrBipID)
    page.gameRedeemValue.textContent = Doc.formatCoinValue(resp.win, ui)
  }
}
