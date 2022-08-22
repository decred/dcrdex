import Doc, { Animation } from './doc'
import BasePage from './basepage'
import { postJSON } from './http'
import { NewWalletForm, WalletConfigForm, UnlockWalletForm, bind as bindForm } from './forms'
import State from './state'
import * as intl from './locales'
import * as OrderUtil from './orderutil'
import {
  app,
  PageElement,
  SupportedAsset,
  WalletDefinition,
  BalanceNote,
  WalletStateNote,
  RateNote,
  Order,
  OrderFilter,
  WalletCreationNote,
  Market
} from './registry'

const animationLength = 300
const traitRescanner = 1
const traitNewAddresser = 1 << 1
const traitLogFiler = 1 << 2
const traitRecoverer = 1 << 5
const traitWithdrawer = 1 << 6
const traitRestorer = 1 << 8
const traitsExtraOpts = traitLogFiler & traitRecoverer & traitRestorer & traitRescanner

const activeOrdersErrCode = 35

interface ReconfigRequest {
  assetID: number
  walletType: string
  config: Record<string, string>
  newWalletPW?: string
  appPW: string
}

interface RescanRecoveryRequest {
  assetID: number
  appPW?: string
  force?: boolean
}

interface WalletRestoration {
  target: string
  seed: string
  seedName: string
  instructions: string
}

interface AssetButton {
  tmpl: Record<string, PageElement>
  bttn: PageElement
}

export default class WalletsPage extends BasePage {
  body: HTMLElement
  page: Record<string, PageElement>
  assetButtons: Record<number, AssetButton>
  newWalletForm: NewWalletForm
  reconfigForm: WalletConfigForm
  unlockForm: UnlockWalletForm
  keyup: (e: KeyboardEvent) => void
  changeWalletPW: boolean
  displayed: HTMLElement
  animation: Animation
  forms: PageElement[]
  forceReq: RescanRecoveryRequest
  forceUrl: string
  currentForm: PageElement
  restoreInfoCard: HTMLElement
  selectedAssetID: number

  constructor (body: HTMLElement) {
    super()
    this.body = body
    const page = this.page = Doc.idDescendants(body)

    Doc.cleanTemplates(page.restoreInfoCard)
    this.restoreInfoCard = page.restoreInfoCard.cloneNode(true) as HTMLElement

    this.forms = Doc.applySelector(page.forms, ':scope > form')
    page.forms.querySelectorAll('.form-closer').forEach(el => {
      Doc.bind(el, 'click', () => { this.closePopups() })
    })
    Doc.bind(page.cancelForce, 'click', () => { this.closePopups() })
    Doc.bind(page.copyAddressBtn, 'click', () => { this.copyAddress() })

    this.selectedAssetID = -1
    Doc.cleanTemplates(
      page.iconSelectTmpl, page.balanceDetailRow, page.recentOrderTmpl
    )
    const firstAsset = this.sortAssetButtons()
    Doc.bind(page.createWallet, 'click', () => this.showNewWallet(this.selectedAssetID))
    Doc.bind(page.connectBttn, 'click', () => this.doConnect(this.selectedAssetID))
    Doc.bind(page.send, 'click', () => this.showSendForm(this.selectedAssetID))
    Doc.bind(page.receive, 'click', () => this.showDeposit(this.selectedAssetID))
    Doc.bind(page.unlockBttn, 'click', () => this.openWallet(this.selectedAssetID))
    Doc.bind(page.lockBttn, 'click', () => this.lock(this.selectedAssetID))
    Doc.bind(page.reconfigureBttn, 'click', () => this.showReconfig(this.selectedAssetID))
    Doc.bind(page.rescanWallet, 'click', () => this.rescanWallet(this.selectedAssetID))

    // Bind the new wallet form.
    this.newWalletForm = new NewWalletForm(page.newWalletForm, (assetID: number) => {
      const fmtParams = { assetName: app().assets[assetID].name }
      this.assetUpdated(assetID, page.newWalletForm, intl.prep(intl.ID_NEW_WALLET_SUCCESS, fmtParams))
      this.sortAssetButtons()
    })

    // Bind the wallet reconfig form.
    this.reconfigForm = new WalletConfigForm(page.reconfigInputs, false)

    // Bind the wallet unlock form.
    this.unlockForm = new UnlockWalletForm(page.unlockWalletForm, (assetID: number) => this.openWalletSuccess(assetID, page.unlockWalletForm))

    // Bind the Send form.
    bindForm(page.sendForm, page.submitSendForm, () => { this.send() })

    // Bind the wallet reconfiguration submission.
    bindForm(page.reconfigForm, page.submitReconfig, () => this.reconfig())

    page.forms.querySelectorAll('.form-closer').forEach(el => {
      Doc.bind(el, 'click', () => this.closePopups())
    })

    Doc.bind(page.forms, 'mousedown', (e: MouseEvent) => {
      if (!Doc.mouseInElement(e, this.currentForm)) { this.closePopups() }
    })

    this.keyup = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        if (Doc.isDisplayed(this.page.forms)) this.closePopups()
      }
    }
    Doc.bind(document, 'keyup', this.keyup)

    Doc.bind(page.downloadLogs, 'click', async () => { this.downloadLogs() })
    Doc.bind(page.exportWallet, 'click', async () => { this.displayExportWalletAuth() })
    Doc.bind(page.recoverWallet, 'click', async () => { this.showRecoverWallet() })
    bindForm(page.exportWalletAuth, page.exportWalletAuthSubmit, async () => { this.exportWalletAuthSubmit() })
    bindForm(page.recoverWalletConfirm, page.recoverWalletSubmit, () => { this.recoverWallet() })
    bindForm(page.confirmForce, page.confirmForceSubmit, async () => { this.confirmForceSubmit() })

    // New deposit address button.
    Doc.bind(page.newDepAddrBttn, 'click', async () => { this.newDepositAddress() })

    // Clicking on the available amount on the Send form populates the
    // amount field.
    Doc.bind(page.sendAvail, 'click', () => {
      const { wallet: { balance: { available: avail }, traits }, unitInfo: ui } = app().assets[this.selectedAssetID]
      page.sendAmt.value = String(avail / ui.conventional.conversionFactor)
      this.showFiatValue(this.selectedAssetID, avail, page.sendValue)
      // Ensure we don't check subtract checkbox for assets that don't have a
      // withdraw method.
      if ((traits & traitWithdrawer) === 0) page.subtractCheckBox.checked = false
      else page.subtractCheckBox.checked = true
    })

    // Display fiat value for current send amount.
    Doc.bind(page.sendAmt, 'input', () => {
      const { unitInfo: ui } = app().assets[this.selectedAssetID]
      const amt = parseFloat(page.sendAmt.value ?? '0')
      const conversionFactor = ui.conventional.conversionFactor
      this.showFiatValue(this.selectedAssetID, amt * conversionFactor, page.sendValue)
    })

    // A link on the wallet reconfiguration form to show/hide the password field.
    Doc.bind(page.showChangePW, 'click', () => {
      this.changeWalletPW = !this.changeWalletPW
      this.setPWSettingViz(this.changeWalletPW)
    })

    // Changing the type of wallet.
    Doc.bind(page.changeWalletTypeSelect, 'change', () => {
      this.changeWalletType()
    })
    Doc.bind(page.showChangeType, 'click', () => {
      if (Doc.isHidden(page.changeWalletType)) {
        Doc.show(page.changeWalletType, page.changeTypeHideIcon)
        Doc.hide(page.changeTypeShowIcon)
        page.changeTypeMsg.textContent = intl.prep(intl.ID_KEEP_WALLET_TYPE)
      } else this.showReconfig(this.selectedAssetID, true)
    })

    app().registerNoteFeeder({
      fiatrateupdate: (note: RateNote) => { this.handleRatesNote(note) },
      balance: (note: BalanceNote) => { this.handleBalanceNote(note) },
      walletstate: (note: WalletStateNote) => { this.handleWalletStateNote(note) },
      walletconfig: (note: WalletStateNote) => { this.handleWalletStateNote(note) },
      createwallet: (note: WalletCreationNote) => { this.handleCreateWalletNote(note) }
    })

    this.setSelectedAsset(firstAsset.id)
  }

  closePopups () {
    Doc.hide(this.page.forms)
    if (this.animation) this.animation.stop()
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

  /*
   * setPWSettingViz sets the visibility of the password field section.
   */
  setPWSettingViz (visible: boolean) {
    if (visible) {
      Doc.hide(this.page.showIcon)
      Doc.show(this.page.hideIcon, this.page.changePW)
      this.page.switchPWMsg.textContent = intl.prep(intl.ID_KEEP_WALLET_PASS)
      return
    }
    Doc.hide(this.page.hideIcon, this.page.changePW)
    Doc.show(this.page.showIcon)
    this.page.switchPWMsg.textContent = intl.prep(intl.ID_NEW_WALLET_PASS)
  }

  /*
   * showBox shows the box with a fade-in animation.
   */
  async showBox (box: HTMLElement, focuser?: PageElement) {
    box.style.opacity = '0'
    Doc.show(box)
    if (focuser) focuser.focus()
    await Doc.animate(animationLength, progress => {
      box.style.opacity = `${progress}`
    }, 'easeOut')
    box.style.opacity = '1'
    this.displayed = box
  }

  /* showForm shows a modal form with a little animation. */
  async showForm (form: PageElement) {
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

  async showSuccess (msg: string) {
    const page = this.page
    page.successMessage.textContent = msg
    this.currentForm = page.checkmarkForm
    this.forms.forEach(form => Doc.hide(form))
    Doc.show(page.forms, page.checkmarkForm)
    page.checkmarkForm.style.right = '0'
    page.checkmark.style.fontSize = '0px'

    const [startR, startG, startB] = State.isDark() ? [223, 226, 225] : [51, 51, 51]
    const [endR, endG, endB] = [16, 163, 16]
    const [diffR, diffG, diffB] = [endR - startR, endG - startG, endB - startB]

    this.animation = new Animation(1200, (prog: number) => {
      page.checkmark.style.fontSize = `${prog * 80}px`
      page.checkmark.style.color = `rgb(${startR + prog * diffR}, ${startG + prog * diffG}, ${startB + prog * diffB})`
    }, 'easeOutElastic', () => {
      this.animation = new Animation(1500, () => { /* pass */ }, '', () => {
        if (this.currentForm === page.checkmarkForm) this.closePopups()
      })
    })
  }

  /* Show the new wallet form. */
  async showNewWallet (assetID: number) {
    const page = this.page
    const box = page.newWalletForm
    this.newWalletForm.setAsset(assetID)
    const defaultsLoaded = this.newWalletForm.loadDefaults()
    await this.showForm(box)
    await defaultsLoaded
  }

  sortAssetButtons (): SupportedAsset {
    const page = this.page
    this.assetButtons = {}
    Doc.empty(page.assetSelect)
    const sortedAssets = [...Object.values(app().assets)]
    sortedAssets.sort((a: SupportedAsset, b: SupportedAsset) => {
      if (a.wallet && !b.wallet) return -1
      if (!a.wallet && b.wallet) return 1
      return a.symbol.localeCompare(b.symbol)
    })
    for (const a of sortedAssets) {
      const bttn = page.iconSelectTmpl.cloneNode(true) as HTMLElement
      page.assetSelect.appendChild(bttn)
      const tmpl = Doc.parseTemplate(bttn)
      this.assetButtons[a.id] = { tmpl, bttn }
      this.updateAssetButton(a.id)
      Doc.bind(bttn, 'click', () => this.setSelectedAsset(a.id))
    }
    page.assetSelect.classList.remove('invisible')
    return sortedAssets[0]
  }

  updateAssetButton (assetID: number) {
    const a = app().assets[assetID]
    const { bttn, tmpl } = this.assetButtons[assetID]
    Doc.hide(tmpl.fiat, tmpl.noWallet)
    bttn.classList.add('nowallet')
    tmpl.img.src = Doc.logoPath(a.symbol)
    tmpl.name.textContent = a.name
    if (a.wallet) {
      bttn.classList.remove('nowallet')
      const { wallet: { balance: b }, unitInfo: ui } = a
      const totalBalance = b.available + b.locked + b.immature
      tmpl.balance.textContent = Doc.formatCoinValue(totalBalance, ui)
      const rate = app().fiatRatesMap[a.id]
      if (rate) {
        Doc.show(tmpl.fiat)
        tmpl.fiat.textContent = Doc.formatFiatConversion(totalBalance, rate, ui)
      }
    } else Doc.show(tmpl.noWallet)
  }

  setSelectedAsset (assetID: number) {
    const { assetSelect } = this.page
    for (const b of assetSelect.children) b.classList.remove('selected')
    this.assetButtons[assetID].bttn.classList.add('selected')
    this.selectedAssetID = assetID
    this.updateDisplayedAsset(assetID)
    this.showAvailableMarkets(assetID)
    this.showRecentActivity(assetID)
  }

  updateDisplayedAsset (assetID: number) {
    if (assetID !== this.selectedAssetID) return
    const { symbol, wallet, name } = app().assets[assetID]
    const page = this.page
    for (const el of document.querySelectorAll('[data-asset-name]')) el.textContent = name
    page.assetLogo.src = Doc.logoPath(symbol)
    Doc.hide(
      page.balanceBox, page.fiatBalanceBox, page.createWalletBox, page.walletDetails,
      page.sendReceive, page.connectBttnBox, page.statusLocked, page.statusReady,
      page.statusOff, page.unlockBttnBox, page.lockBttnBox, page.connectBttnBox,
      page.reconfigureBox, page.peerCountBox, page.syncProgressBox
    )
    if (wallet) {
      this.updateDisplayedAssetBalance()

      const walletDef = app().walletDefinition(assetID, wallet.type)
      page.walletType.textContent = walletDef.tab
      const configurable = assetIsConfigurable(assetID)
      if (configurable) Doc.show(page.reconfigureBox)

      if (wallet.running) {
        Doc.show(page.sendReceive, page.peerCountBox, page.syncProgressBox)
        page.peerCount.textContent = String(wallet.peerCount)
        page.syncProgress.textContent = `${(wallet.syncProgress * 100).toFixed(1)}%`
        if (wallet.open) {
          Doc.show(page.statusReady)
          if (!app().haveAssetOrders(assetID) && wallet.encrypted) Doc.show(page.lockBttnBox)
        } else Doc.show(page.statusLocked, page.unlockBttnBox) // wallet not unlocked
      } else Doc.show(page.statusOff, page.connectBttnBox) // wallet not running
    } else Doc.show(page.createWalletBox) // no wallet

    page.walletDetailsBox.classList.remove('invisible')
  }

  updateDisplayedAssetBalance (): void {
    const page = this.page
    const { wallet, unitInfo: ui, symbol, id: assetID } = app().assets[this.selectedAssetID]
    const bal = wallet.balance
    Doc.show(page.balanceBox, page.walletDetails)
    const totalBalance = bal.available + bal.locked + bal.immature
    page.balance.textContent = Doc.formatCoinValue(totalBalance, ui)
    Doc.empty(page.balanceUnit)
    page.balanceUnit.appendChild(Doc.symbolize(symbol))
    const rate = app().fiatRatesMap[assetID]
    if (rate) {
      Doc.show(page.fiatBalanceBox)
      page.fiatBalance.textContent = Doc.formatFiatConversion(totalBalance, rate, ui)
    }
    Doc.empty(page.balanceDetailBox)
    const addSubBalance = (category: string, subBalance: number) => {
      const row = page.balanceDetailRow.cloneNode(true) as PageElement
      page.balanceDetailBox.appendChild(row)
      const tmpl = Doc.parseTemplate(row)
      tmpl.category.textContent = category
      tmpl.subBalance.textContent = Doc.formatCoinValue(subBalance, ui)
    }
    addSubBalance('Available', bal.available)
    addSubBalance('Locked', bal.locked) // Hide if zero?
    addSubBalance('Immature', bal.immature) // Hide if zero?
    const sortedCats = Object.entries(bal.other || {})
    sortedCats.sort((a: [string, number], b: [string, number]): number => a[0].localeCompare(b[0]))
    for (const [cat, sub] of sortedCats) addSubBalance(cat, sub)
  }

  showAvailableMarkets (assetID: number) {
    const page = this.page
    const markets: [string, Market][] = []
    for (const xc of Object.values(app().user.exchanges)) {
      if (!xc.markets) continue
      for (const mkt of Object.values(xc.markets)) {
        if (mkt.baseid === assetID || mkt.quoteid === assetID) markets.push([xc.host, mkt])
      }
    }

    const spotVolume = (assetID: number, mkt: Market): number => {
      const spot = mkt.spot
      if (!spot) return 0
      return assetID === mkt.baseid ? spot.vol24 : spot.vol24 * spot.rate / OrderUtil.RateEncodingFactor
    }

    markets.sort((a: [string, Market], b: [string, Market]): number => {
      const [hostA, mktA] = a
      const [hostB, mktB] = b
      if (!mktA.spot && !mktB.spot) return hostA.localeCompare(hostB)
      return spotVolume(assetID, mktB) - spotVolume(assetID, mktA)
    })
    Doc.empty(page.availableMarkets)

    for (const [host, mkt] of markets) {
      const { spot, baseid, basesymbol, quoteid, quotesymbol } = mkt
      const row = page.marketRow.cloneNode(true) as PageElement
      page.availableMarkets.appendChild(row)
      const tmpl = Doc.parseTemplate(row)
      tmpl.host.textContent = host
      tmpl.baseLogo.src = Doc.logoPath(basesymbol)
      tmpl.quoteLogo.src = Doc.logoPath(quotesymbol)
      Doc.empty(tmpl.baseSymbol, tmpl.quoteSymbol)
      tmpl.baseSymbol.appendChild(Doc.symbolize(basesymbol))
      tmpl.quoteSymbol.appendChild(Doc.symbolize(quotesymbol))

      if (spot) {
        const convRate = app().conventionalRate(baseid, quoteid, spot.rate)
        tmpl.price.textContent = fourSigFigs(convRate)
        tmpl.priceQuoteUnit.textContent = quotesymbol.toUpperCase()
        tmpl.priceBaseUnit.textContent = basesymbol.toUpperCase()
        tmpl.volume.textContent = fourSigFigs(spotVolume(assetID, mkt))
        tmpl.volumeUnit.textContent = assetID === baseid ? basesymbol.toUpperCase() : quotesymbol.toUpperCase()
      } else Doc.hide(tmpl.priceBox, tmpl.volumeBox)
      Doc.bind(row, 'click', () => app().loadPage('markets', { host, base: baseid, quote: quoteid }))
    }
    page.marketsOverviewBox.classList.remove('invisible')
  }

  async showRecentActivity (assetID: number) {
    const page = this.page
    const loaded = app().loading(page.orderActivityBox)
    const filter: OrderFilter = {
      n: 20,
      assets: [assetID],
      hosts: [],
      statuses: []
    }
    const res = await postJSON('/api/orders', filter)
    loaded()
    Doc.hide(page.noActivity, page.orderActivity)
    if (!res.orders || res.orders.length === 0) {
      Doc.show(page.noActivity)
      page.orderActivityBox.classList.remove('invisible')
      return
    }
    Doc.show(page.orderActivity)
    Doc.empty(page.recentOrders)
    for (const ord of (res.orders as Order[])) {
      const row = page.recentOrderTmpl.cloneNode(true) as PageElement
      page.recentOrders.appendChild(row)
      const tmpl = Doc.parseTemplate(row)
      let from: string, to: string
      const [baseUnitInfo, quoteUnitInfo] = [app().unitInfo(ord.baseID), app().unitInfo(ord.quoteID)]
      if (ord.sell) {
        [from, to] = [ord.baseSymbol, ord.quoteSymbol]
        tmpl.fromQty.textContent = Doc.formatCoinValue(ord.qty, baseUnitInfo)
        if (ord.type === OrderUtil.Limit) {
          tmpl.toQty.textContent = Doc.formatCoinValue(ord.qty / OrderUtil.RateEncodingFactor * ord.rate, quoteUnitInfo)
        }
      } else {
        [from, to] = [ord.quoteSymbol, ord.baseSymbol]
        if (ord.type === OrderUtil.Market) {
          tmpl.fromQty.textContent = Doc.formatCoinValue(ord.qty, baseUnitInfo)
        } else {
          tmpl.fromQty.textContent = Doc.formatCoinValue(ord.qty / OrderUtil.RateEncodingFactor * ord.rate, quoteUnitInfo)
          tmpl.toQty.textContent = Doc.formatCoinValue(ord.qty, baseUnitInfo)
        }
      }

      tmpl.fromLogo.src = Doc.logoPath(from)
      Doc.empty(tmpl.fromSymbol, tmpl.toSymbol)
      tmpl.fromSymbol.appendChild(Doc.symbolize(from))
      tmpl.toLogo.src = Doc.logoPath(to)
      tmpl.toSymbol.appendChild(Doc.symbolize(to))
      tmpl.status.textContent = OrderUtil.statusString(ord)
      tmpl.filled.textContent = `${(OrderUtil.filled(ord) / ord.qty * 100).toFixed(1)}%`
      tmpl.age.textContent = Doc.timeSince(ord.submitTime)
      tmpl.link.href = `order/${ord.id}`
      app().bindInternalNavigation(row)
    }
    page.orderActivityBox.classList.remove('invisible')
  }

  async rescanWallet (assetID: number) {
    const page = this.page
    Doc.hide(page.reconfigErr)

    const url = '/api/rescanwallet'
    const req = { assetID: assetID }

    const loaded = app().loading(this.body)
    const res = await postJSON(url, req)
    loaded()
    if (res.code === activeOrdersErrCode) {
      this.forceUrl = url
      this.forceReq = req
      this.showConfirmForce()
      return
    }
    if (!app().checkResponse(res)) {
      page.reconfigErr.textContent = res.msg
      Doc.show(page.reconfigErr)
      return
    }
    this.assetUpdated(assetID, page.reconfigForm, intl.prep(intl.ID_RESCAN_STARTED))
  }

  showConfirmForce () {
    Doc.hide(this.page.confirmForceErr)
    this.showForm(this.page.confirmForce)
  }

  showRecoverWallet () {
    Doc.hide(this.page.recoverWalletErr)
    this.showForm(this.page.recoverWalletConfirm)
  }

  /* Show the open wallet form if the password is not cached, and otherwise
   * attempt to open the wallet.
   */
  async openWallet (assetID: number) {
    if (!State.passwordIsCached()) {
      this.showOpen(assetID)
    } else {
      const open = {
        assetID: assetID
      }
      const res = await postJSON('/api/openwallet', open)
      if (app().checkResponse(res)) {
        this.openWalletSuccess(assetID)
      } else {
        this.showOpen(assetID, `Error opening wallet: ${res.msg}`)
      }
    }
  }

  /* Show the form used to unlock a wallet. */
  async showOpen (assetID: number, errorMsg?: string) {
    const page = this.page
    // await this.hideBox()
    this.unlockForm.refresh(app().assets[assetID])
    if (errorMsg) this.unlockForm.showErrorOnly(errorMsg)
    this.showForm(page.unlockWalletForm)
  }

  /* Show the form used to change wallet configuration settings. */
  async showReconfig (assetID: number, skipAnimation?: boolean) {
    const page = this.page
    Doc.hide(page.changeWalletType, page.changeTypeHideIcon, page.reconfigErr, page.showChangeType, page.changeTypeHideIcon)
    Doc.hide(page.reconfigErr)
    // Hide update password section by default
    this.changeWalletPW = false
    this.setPWSettingViz(this.changeWalletPW)
    const asset = app().assets[assetID]

    const currentDef = app().currentWalletDefinition(assetID)
    const walletDefs = asset.token ? [asset.token.definition] : asset.info ? asset.info.availablewallets : []

    if (walletDefs.length > 1) {
      Doc.empty(page.changeWalletTypeSelect)
      Doc.show(page.showChangeType, page.changeTypeShowIcon)
      page.changeTypeMsg.textContent = intl.prep(intl.ID_CHANGE_WALLET_TYPE)
      for (const wDef of walletDefs) {
        const option = document.createElement('option') as HTMLOptionElement
        if (wDef.type === currentDef.type) option.selected = true
        option.value = option.textContent = wDef.type
        page.changeWalletTypeSelect.appendChild(option)
      }
    } else {
      Doc.hide(page.showChangeType)
    }

    const wallet = app().walletMap[assetID]
    Doc.setVis(wallet.traits & traitLogFiler, page.downloadLogs)
    Doc.setVis(wallet.traits & traitRecoverer, page.recoverWallet)
    Doc.setVis(wallet.traits & traitRestorer, page.exportWallet)
    Doc.setVis(wallet.traits & traitRescanner, page.rescanWallet)
    Doc.setVis(wallet.traits & traitsExtraOpts, page.otherActionsLabel)

    page.recfgAssetLogo.src = Doc.logoPath(asset.symbol)
    page.recfgAssetName.textContent = asset.name
    if (!skipAnimation) this.showForm(page.reconfigForm)
    const loaded = app().loading(page.reconfigForm)
    const res = await postJSON('/api/walletsettings', { assetID })
    loaded()
    if (!app().checkResponse(res)) {
      page.reconfigErr.textContent = res.msg
      Doc.show(page.reconfigErr)
      return
    }
    const assetHasActiveOrders = app().haveAssetOrders(assetID)
    this.reconfigForm.update(currentDef.configopts || [], { assetHasActiveOrders })
    this.reconfigForm.setConfig(res.map)
    this.updateDisplayedReconfigFields(currentDef)
  }

  changeWalletType () {
    const page = this.page
    const walletType = page.changeWalletTypeSelect.value || ''
    const walletDef = app().walletDefinition(this.selectedAssetID, walletType)
    this.reconfigForm.update(walletDef.configopts || [], {})
    this.updateDisplayedReconfigFields(walletDef)
  }

  updateDisplayedReconfigFields (walletDef: WalletDefinition) {
    if (walletDef.seeded || walletDef.type === 'token') {
      Doc.hide(this.page.showChangePW)
      this.changeWalletPW = false
      this.setPWSettingViz(false)
    } else Doc.show(this.page.showChangePW)
  }

  /* Display a deposit address. */
  async showDeposit (assetID: number) {
    const page = this.page
    Doc.hide(page.depositErr)
    const box = page.deposit
    const asset = app().assets[assetID]
    page.depositLogo.src = Doc.logoPath(asset.symbol)
    const wallet = app().walletMap[assetID]
    page.depositName.textContent = asset.name
    page.depositAddress.textContent = wallet.address
    page.qrcode.src = `/generateqrcode?address=${wallet.address}`
    if ((wallet.traits & traitNewAddresser) !== 0) Doc.show(page.newDepAddrBttn)
    else Doc.hide(page.newDepAddrBttn)
    this.showForm(box)
  }

  /* Fetch a new address from the wallet. */
  async newDepositAddress () {
    const page = this.page
    Doc.hide(page.depositErr)
    const loaded = app().loading(page.deposit)
    const res = await postJSON('/api/depositaddress', {
      assetID: this.selectedAssetID
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

  /* Show the form to either send or withdraw funds. */
  async showSendForm (assetID: number) {
    const page = this.page
    const box = page.sendForm
    const { wallet, name, unitInfo: ui, symbol } = app().assets[assetID]
    Doc.hide(page.senderOnlyHelpText)
    Doc.hide(page.toggleSubtract)
    page.subtractCheckBox.checked = false

    const isWithdrawer = (wallet.traits & traitWithdrawer) !== 0
    if (!isWithdrawer) {
      Doc.show(page.senderOnlyHelpText)
      page.subtractCheckBox.checked = false
    } else {
      Doc.show(page.toggleSubtract)
    }

    page.sendAddr.value = ''
    page.sendAmt.value = ''
    page.sendPW.value = ''
    page.sendErr.textContent = ''
    this.showFiatValue(assetID, 0, page.sendValue)
    page.sendAvail.textContent = Doc.formatFullPrecision(wallet.balance.available, ui)
    page.sendLogo.src = Doc.logoPath(symbol)
    page.sendName.textContent = name
    // page.sendFee.textContent = wallet.feerate
    // page.sendUnit.textContent = wallet.units
    box.dataset.assetID = String(assetID)
    this.showForm(box)
  }

  /* doConnect connects to a wallet via the connectwallet API route. */
  async doConnect (assetID: number) {
    const loaded = app().loading(this.body)
    const res = await postJSON('/api/connectwallet', { assetID })
    loaded()
    if (!app().checkResponse(res)) return
    this.updateDisplayedAsset(assetID)
  }

  /* openWalletSuccess is the success callback for wallet unlocking. */
  async openWalletSuccess (assetID: number, form?: PageElement) {
    this.assetUpdated(assetID, form, intl.prep(intl.ID_WALLET_UNLOCKED))
  }

  assetUpdated (assetID: number, oldForm?: PageElement, successMsg?: string) {
    if (assetID !== this.selectedAssetID) return
    this.updateDisplayedAsset(assetID)
    if (oldForm && Object.is(this.currentForm, oldForm)) {
      if (successMsg) this.showSuccess(successMsg)
      else this.closePopups()
    }
  }

  /* send submits the send form to the API. */
  async send (): Promise<void> {
    const page = this.page
    Doc.hide(page.sendErr)
    const assetID = parseInt(page.sendForm.dataset.assetID ?? '')
    const subtract = page.subtractCheckBox.checked ?? false
    const conversionFactor = app().unitInfo(assetID).conventional.conversionFactor
    const open = {
      assetID: assetID,
      address: page.sendAddr.value,
      subtract: subtract,
      value: Math.round(parseFloat(page.sendAmt.value ?? '') * conversionFactor),
      pw: page.sendPW.value
    }
    const loaded = app().loading(page.sendForm)
    const res = await postJSON('/api/send', open)
    loaded()
    if (!app().checkResponse(res)) {
      page.sendErr.textContent = res.msg
      Doc.show(page.sendErr)
      return
    }
    const name = app().assets[assetID].name
    this.assetUpdated(assetID, page.sendForm, intl.prep(intl.ID_SEND_SUCCESS, { assetName: name }))
  }

  /* update wallet configuration */
  async reconfig (): Promise<void> {
    const page = this.page
    const assetID = this.selectedAssetID
    Doc.hide(page.reconfigErr)
    if (!page.appPW.value && !State.passwordIsCached()) {
      page.reconfigErr.textContent = intl.prep(intl.ID_NO_APP_PASS_ERROR_MSG)
      Doc.show(page.reconfigErr)
      return
    }

    let walletType = app().currentWalletDefinition(assetID).type
    if (!Doc.isHidden(page.changeWalletType)) {
      walletType = page.changeWalletTypeSelect.value || ''
    }

    const loaded = app().loading(page.reconfigForm)
    const req: ReconfigRequest = {
      assetID: assetID,
      config: this.reconfigForm.map(assetID),
      appPW: page.appPW.value ?? '',
      walletType: walletType
    }
    if (this.changeWalletPW) req.newWalletPW = page.newPW.value
    const res = await postJSON('/api/reconfigurewallet', req)
    page.appPW.value = ''
    page.newPW.value = ''
    loaded()
    if (!app().checkResponse(res)) {
      page.reconfigErr.textContent = res.msg
      Doc.show(page.reconfigErr)
      return
    }
    this.assetUpdated(assetID, page.reconfigForm, intl.prep(intl.ID_RECONFIG_SUCCESS))
  }

  /* lock instructs the API to lock the wallet. */
  async lock (assetID: number): Promise<void> {
    const page = this.page
    const loaded = app().loading(page.newWalletForm)
    const res = await postJSON('/api/closewallet', { assetID: assetID })
    loaded()
    if (!app().checkResponse(res)) return
    this.updateDisplayedAsset(assetID)
  }

  async downloadLogs (): Promise<void> {
    const search = new URLSearchParams('')
    search.append('assetid', `${this.selectedAssetID}`)
    const url = new URL(window.location.href)
    url.search = search.toString()
    url.pathname = '/wallets/logfile'
    window.open(url.toString())
  }

  // displayExportWalletAuth displays a form to warn the user about the
  // dangers of exporting a wallet, and asks them to enter their password.
  async displayExportWalletAuth (): Promise<void> {
    const page = this.page
    Doc.hide(page.exportWalletErr)
    page.exportWalletPW.value = ''
    this.showForm(page.exportWalletAuth)
  }

  // exportWalletAuthSubmit is called after the user enters their password to
  // authorize looking up the information to restore their wallet in an
  // external wallet.
  async exportWalletAuthSubmit (): Promise<void> {
    const page = this.page
    const req = {
      assetID: this.selectedAssetID,
      pass: page.exportWalletPW.value
    }
    const url = '/api/restorewalletinfo'
    const loaded = app().loading(page.forms)
    const res = await postJSON(url, req)
    loaded()
    if (app().checkResponse(res)) {
      page.exportWalletPW.value = ''
      this.displayRestoreWalletInfo(res.restorationinfo)
    } else {
      page.exportWalletErr.textContent = res.msg
      Doc.show(page.exportWalletErr)
    }
  }

  // displayRestoreWalletInfo displays the information needed to restore a
  // wallet in external wallets.
  async displayRestoreWalletInfo (info: WalletRestoration[]): Promise<void> {
    const page = this.page
    Doc.empty(page.restoreInfoCardsList)
    for (const wr of info) {
      const card = this.restoreInfoCard.cloneNode(true) as HTMLElement
      const tmpl = Doc.parseTemplate(card)
      tmpl.name.textContent = wr.target
      tmpl.seed.textContent = wr.seed
      tmpl.seedName.textContent = `${wr.seedName}:`
      tmpl.instructions.textContent = wr.instructions
      page.restoreInfoCardsList.appendChild(card)
    }
    this.showForm(page.restoreWalletInfo)
  }

  async recoverWallet (): Promise<void> {
    const page = this.page
    Doc.hide(page.recoverWalletErr)
    const req = {
      assetID: this.selectedAssetID,
      appPW: page.recoverWalletPW.value
    }
    page.recoverWalletPW.value = ''
    const url = '/api/recoverwallet'
    const loaded = app().loading(page.forms)
    const res = await postJSON(url, req)
    loaded()
    if (res.code === activeOrdersErrCode) {
      this.forceUrl = url
      this.forceReq = req
      this.showConfirmForce()
    } else if (app().checkResponse(res)) {
      this.closePopups()
    } else {
      page.recoverWalletErr.textContent = res.msg
      Doc.show(page.recoverWalletErr)
    }
  }

  /*
   * confirmForceSubmit resubmits either the recover or rescan requests with
   * force set to true. These two requests require force to be set to true if
   * they are called while the wallet is managing active orders.
   */
  async confirmForceSubmit (): Promise<void> {
    const page = this.page
    this.forceReq.force = true
    const loaded = app().loading(page.forms)
    const res = await postJSON(this.forceUrl, this.forceReq)
    loaded()
    if (app().checkResponse(res)) this.closePopups()
    else {
      page.confirmForceErr.textContent = res.msg
      Doc.show(page.confirmForceErr)
    }
  }

  /* handleBalance handles notifications updating a wallet's balance and assets'
     value in default fiat rate.
  . */
  handleBalanceNote (note: BalanceNote): void {
    this.updateAssetButton(note.assetID)
    if (note.assetID === this.selectedAssetID) this.updateDisplayedAssetBalance()
  }

  /* handleRatesNote handles fiat rate notifications, updating the fiat value of
   *  all supported assets.
   */
  handleRatesNote (note: RateNote): void {
    this.updateAssetButton(this.selectedAssetID)
    if (!note.fiatRates[this.selectedAssetID]) return
    this.updateDisplayedAssetBalance()
  }

  // showFiatValue displays the fiat equivalent for the provided amount.
  showFiatValue (assetID: number, amount: number, display: PageElement): void {
    const rate = app().fiatRatesMap[assetID]
    if (rate) {
      display.textContent = Doc.formatFiatConversion(amount, rate, app().unitInfo(assetID))
      Doc.show(display.parentElement as Element)
    } else Doc.hide(display.parentElement as Element)
  }

  /*
   * handleWalletStateNote is a handler for both the 'walletstate' and
   * 'walletconfig' notifications.
   */
  handleWalletStateNote (note: WalletStateNote): void {
    this.updateAssetButton(note.wallet.assetID)
    this.assetUpdated(note.wallet.assetID)
  }

  /*
   * handleCreateWalletNote is a handler for 'createwallet' notifications.
   */
  handleCreateWalletNote (note: WalletCreationNote) {
    this.updateAssetButton(note.assetID)
    this.assetUpdated(note.assetID)
  }

  /*
   * unload is called by the Application when the user navigates away from
   * the /wallets page.
   */
  unload (): void {
    Doc.unbind(document, 'keyup', this.keyup)
  }
}

/*
 * assetIsConfigurable indicates whether there are any user-configurable wallet
 * settings for the asset.
 */
function assetIsConfigurable (assetID: number) {
  const asset = app().assets[assetID]
  if (asset.token) {
    const opts = asset.token.definition.configopts
    return opts && opts.length > 0
  }
  if (!asset.info) throw Error('this asset isn\'t an asset, I guess')
  const defs = asset.info.availablewallets
  const zerothOpts = defs[0].configopts
  return defs.length > 1 || (zerothOpts && zerothOpts.length > 0)
}

const FourSigFigs = new Intl.NumberFormat((navigator.languages as string[]), {
  minimumSignificantDigits: 4,
  maximumSignificantDigits: 4
})

const intFormatter = new Intl.NumberFormat((navigator.languages as string[]))

function fourSigFigs (v: number): string {
  if (v < 100) return FourSigFigs.format(v)
  return intFormatter.format(Math.round(v))
}
