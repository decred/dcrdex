import Doc from './doc'
import BasePage from './basepage'
import State from './state'
import { postJSON } from './http'
import * as forms from './forms'
import * as intl from './locales'

import {
  app,
  PageElement,
  ConnectionStatus,
  Exchange
} from './registry'

const animationLength = 300
const bondOverlap = 2 // See client/core/bond.go#L28

export default class DexSettingsPage extends BasePage {
  body: HTMLElement
  forms: PageElement[]
  currentForm: PageElement
  page: Record<string, PageElement>
  host: string
  keyup: (e: KeyboardEvent) => void
  dexAddrForm: forms.DEXAddressForm
  bondFeeBufferCache: Record<string, number>

  constructor (body: HTMLElement) {
    super()
    this.body = body
    this.host = body.dataset.host ? body.dataset.host : ''
    const page = this.page = Doc.idDescendants(body)
    this.forms = Doc.applySelector(page.forms, ':scope > form')

    Doc.bind(page.exportDexBtn, 'click', () => this.prepareAccountExport(page.authorizeAccountExportForm))
    Doc.bind(page.disableAcctBtn, 'click', () => this.prepareAccountDisable(page.disableAccountForm))
    Doc.bind(page.bondDetailsBtn, 'click', () => this.prepareBondDetailsForm())
    Doc.bind(page.updateCertBtn, 'click', () => page.certFileInput.click())
    Doc.bind(page.updateHostBtn, 'click', () => this.prepareUpdateHost())
    Doc.bind(page.certFileInput, 'change', () => this.onCertFileChange())
    Doc.bind(page.bondAssetSelect, 'change', () => this.updateBondAssetCosts())
    Doc.bind(page.bondTargetTier, 'input', () => this.updateBondAssetCosts())

    this.dexAddrForm = new forms.DEXAddressForm(page.dexAddrForm, async (xc: Exchange) => {
      window.location.assign(`/dexsettings/${xc.host}`)
    }, undefined, this.host)

    forms.bind(page.bondDetailsForm, page.updateBondOptionsConfirm, () => this.updateBondOptions())
    forms.bind(page.authorizeAccountExportForm, page.authorizeExportAccountConfirm, () => this.exportAccount())
    forms.bind(page.disableAccountForm, page.disableAccountConfirm, () => this.disableAccount())

    const closePopups = () => {
      Doc.hide(page.forms)
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

    app().registerNoteFeeder({
      conn: () => { this.setConnectionStatus() }
    })

    this.setConnectionStatus()
  }

  unload () {
    Doc.unbind(document, 'keyup', this.keyup)
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
    const loaded = app().loading(this.body)
    const res = await postJSON('/api/exportaccount', req)
    loaded()
    if (!app().checkResponse(res)) {
      page.exportAccountErr.textContent = res.msg
      Doc.show(page.exportAccountErr)
      return
    }
    res.account.bonds = res.bonds // maintain backward compat of JSON file
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
    const loaded = app().loading(this.body)
    const res = await postJSON('/api/disableaccount', req)
    loaded()
    if (!app().checkResponse(res)) {
      page.disableAccountErr.textContent = res.msg
      Doc.show(page.disableAccountErr)
      return
    }
    Doc.hide(page.forms)
    window.location.assign('/settings')
  }

  async prepareAccountExport (authorizeAccountExportForm: HTMLElement) {
    const page = this.page
    page.exportAccountHost.textContent = this.host
    page.exportAccountErr.textContent = ''
    if (State.passwordIsCached()) {
      this.exportAccount()
    } else {
      this.showForm(authorizeAccountExportForm)
    }
  }

  async prepareAccountDisable (disableAccountForm: HTMLElement) {
    const page = this.page
    page.disableAccountHost.textContent = this.host
    page.disableAccountErr.textContent = ''
    this.showForm(disableAccountForm)
  }

  // prepareBondDetailsForm resets and prepares the Bond Details form.
  async prepareBondDetailsForm () {
    const page = this.page
    const xc = app().user.exchanges[this.host]
    // Update bond details on this form
    const targetTier = xc.auth.targetTier.toString()
    page.currentTargetTier.textContent = `${targetTier}`
    page.currentTier.textContent = `${xc.auth.effectiveTier}`
    page.bondTargetTier.setAttribute('placeholder', targetTier)
    page.bondTargetTier.value = ''
    this.bondFeeBufferCache = {}
    Doc.empty(page.bondAssetSelect)
    for (const [assetSymbol, bondAsset] of Object.entries(xc.bondAssets)) {
      const option = document.createElement('option') as HTMLOptionElement
      option.value = bondAsset.id.toString()
      option.textContent = assetSymbol.toUpperCase()
      if (bondAsset.id === xc.auth.bondAssetID) option.selected = true
      page.bondAssetSelect.appendChild(option)
    }
    page.bondOptionsErr.textContent = ''
    Doc.hide(page.bondOptionsErr)
    await this.updateBondAssetCosts()
    this.showForm(page.bondDetailsForm)
  }

  async updateBondAssetCosts () {
    const xc = app().user.exchanges[this.host]
    const page = this.page
    const bondAssetID = parseInt(page.bondAssetSelect.value ?? '')
    Doc.hide(page.bondCostFiat.parentElement as Element)
    Doc.hide(page.bondReservationAmtFiat.parentElement as Element)
    const assetInfo = xc.assets[bondAssetID]
    const bondAsset = xc.bondAssets[assetInfo.symbol]

    const bondCost = bondAsset.amount
    const ui = assetInfo.unitInfo
    const assetID = bondAsset.id
    Doc.applySelector(page.bondDetailsForm, '.bondAssetSym').forEach((el) => { el.textContent = assetInfo.symbol.toLocaleUpperCase() })
    page.bondCost.textContent = Doc.formatFullPrecision(bondCost, ui)
    Doc.showFiatValue(assetID, bondCost, page.bondCostFiat)

    let feeBuffer = this.bondFeeBufferCache[assetInfo.symbol]
    if (!feeBuffer) {
      feeBuffer = await this.getBondsFeeBuffer(assetID, page.bondDetailsForm)
      if (feeBuffer > 0) this.bondFeeBufferCache[assetInfo.symbol] = feeBuffer
    }
    if (feeBuffer === 0) {
      page.bondReservationAmt.textContent = intl.prep(intl.ID_UNAVAILABLE)
      return
    }
    const targetTier = parseInt(page.bondTargetTier.value ?? '')
    let reservation = 0
    if (targetTier > 0) reservation = bondCost * targetTier * bondOverlap + feeBuffer
    page.bondReservationAmt.textContent = Doc.formatFullPrecision(reservation, ui)
    Doc.showFiatValue(assetID, reservation, page.bondReservationAmtFiat)
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

  async prepareUpdateHost () {
    const page = this.page
    this.dexAddrForm.refresh()
    this.showForm(page.dexAddrForm)
  }

  async onCertFileChange () {
    const page = this.page
    Doc.hide(page.errMsg)
    const files = page.certFileInput.files
    let cert
    if (files && files.length) cert = await files[0].text()
    if (!cert) return
    const req = { host: this.host, cert: cert }
    const loaded = app().loading(this.body)
    const res = await postJSON('/api/updatecert', req)
    loaded()
    if (!app().checkResponse(res)) {
      page.errMsg.textContent = res.msg
      Doc.show(page.errMsg)
    } else {
      Doc.show(page.updateCertMsg)
      setTimeout(() => { Doc.hide(page.updateCertMsg) }, 5000)
    }
  }

  setConnectionStatus () {
    const page = this.page
    const exchange = app().user.exchanges[this.host]
    const displayIcons = (connected: boolean) => {
      if (connected) {
        Doc.hide(page.disconnectedIcon)
        Doc.show(page.connectedIcon)
      } else {
        Doc.show(page.disconnectedIcon)
        Doc.hide(page.connectedIcon)
      }
    }
    if (exchange) {
      switch (exchange.connectionStatus) {
        case ConnectionStatus.Connected:
          displayIcons(true)
          page.connectionStatus.textContent = intl.prep(intl.ID_CONNECTED)
          break
        case ConnectionStatus.Disconnected:
          displayIcons(false)
          page.connectionStatus.textContent = intl.prep(intl.ID_DISCONNECTED)
          break
        case ConnectionStatus.InvalidCert:
          displayIcons(false)
          page.connectionStatus.textContent = `${intl.prep(intl.ID_DISCONNECTED)} - ${intl.prep(intl.ID_INVALID_CERTIFICATE)}`
      }
    }
  }

  /*
   * updateBondOptions is called when the form to update bond options is
   * submitted.
   */
  async updateBondOptions () {
    const page = this.page
    const targetTier = parseInt(page.bondTargetTier.value ?? '')
    const bondAssetID = parseInt(page.bondAssetSelect.value ?? '')

    const bondOptions: Record<any, any> = {
      host: this.host,
      targetTier: targetTier,
      bondAssetID: bondAssetID
    }

    const assetInfo = app().assets[bondAssetID]
    if (assetInfo) {
      const feeBuffer = this.bondFeeBufferCache[assetInfo.symbol]
      if (feeBuffer > 0) bondOptions.feeBuffer = feeBuffer
    }

    const loaded = app().loading(this.body)
    const res = await postJSON('/api/updatebondoptions', bondOptions)
    loaded()
    if (!app().checkResponse(res)) {
      page.bondOptionsErr.textContent = res.msg
      Doc.show(page.bondOptionsErr)
    } else {
      Doc.hide(page.bondOptionsErr)
      Doc.show(page.bondOptionsMsg)
      setTimeout(() => {
        Doc.hide(page.bondOptionsMsg)
      }, 5000)
      // update the in-memory values.
      const xc = app().user.exchanges[this.host]
      xc.auth.bondAssetID = bondAssetID
      xc.auth.targetTier = targetTier
      page.currentTargetTier.textContent = `${targetTier}`
    }
  }
}
