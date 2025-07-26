import {
  app,
  PageElement,
  MarketMakingEvent,
  DEXOrderEvent,
  CEXOrderEvent,
  RunEventNote,
  RunStatsNote,
  DepositEvent,
  WithdrawalEvent,
  MarketMakingRunOverview,
  SupportedAsset,
  BalanceEffects,
  MarketWithHost,
  ProfitLoss
} from './registry'
import { Forms } from './forms'
import { postJSON } from './http'
import Doc, { setupCopyBtn } from './doc'
import BasePage from './basepage'
import { setMarketElements, liveBotStatus } from './mmutil'
import * as intl from './locales'
import * as wallets from './wallets'
import { CoinExplorers } from './coinexplorers'

interface LogsPageParams {
  host: string
  quoteID: number
  baseID: number
  startTime: number
  returnPage: string
}

let net = 0

const logsBatchSize = 50

interface logFilters {
  dexSells: boolean
  dexBuys: boolean
  cexSells: boolean
  cexBuys: boolean
  deposits: boolean
  withdrawals: boolean
}

function eventPassesFilter (e: MarketMakingEvent, filters: logFilters): boolean {
  if (e.dexOrderEvent) {
    if (e.dexOrderEvent.sell) return filters.dexSells
    return filters.dexBuys
  }
  if (e.cexOrderEvent) {
    if (e.cexOrderEvent.sell) return filters.cexSells
    return filters.cexBuys
  }
  if (e.depositEvent) return filters.deposits
  if (e.withdrawalEvent) return filters.withdrawals
  return false
}

export default class MarketMakerLogsPage extends BasePage {
  page: Record<string, PageElement>
  mkt: MarketWithHost
  startTime: number
  fiatRates: Record<number, number>
  liveBot: boolean
  overview: MarketMakingRunOverview
  events: Record<number, [MarketMakingEvent, HTMLElement]>
  forms: Forms
  dexOrderIDCopyListener: () => void | undefined
  cexOrderIDCopyListener: () => void | undefined
  depositIDCopyListener: () => void | undefined
  withdrawalIDCopyListener: () => void | undefined
  filters: logFilters
  loading: boolean
  refID: number | undefined
  doneScrolling: boolean
  statsRows: Record<number, HTMLElement>

  constructor (main: HTMLElement, params: LogsPageParams) {
    super()
    const page = this.page = Doc.idDescendants(main)
    net = app().user.net
    Doc.cleanTemplates(page.eventTableRowTmpl, page.dexOrderTxRowTmpl, page.performanceTableRowTmpl)
    Doc.bind(this.page.backButton, 'click', () => { app().loadPage(params.returnPage ?? 'mm') })
    Doc.bind(this.page.filterButton, 'click', () => { this.applyFilters() })
    if (params?.host) {
      const url = new URL(window.location.href)
      url.searchParams.set('host', params.host)
      url.searchParams.set('baseID', String(params.baseID))
      url.searchParams.set('quoteID', String(params.quoteID))
      url.searchParams.set('startTime', String(params.startTime))
      window.history.replaceState({ page: 'mmsettings', ...params }, '', url)
    } else {
      const urlParams = new URLSearchParams(window.location.search)
      if (!params) params = {} as LogsPageParams
      params.host = urlParams.get('host') || ''
      params.baseID = parseInt(urlParams.get('baseID') || '0')
      params.quoteID = parseInt(urlParams.get('quoteID') || '0')
      params.startTime = parseInt(urlParams.get('startTime') || '0')
    }
    const { baseID, quoteID, host, startTime } = params
    this.startTime = startTime
    this.forms = new Forms(page.forms)
    this.events = {}
    this.statsRows = {}
    this.mkt = { baseID: baseID, quoteID: quoteID, host }
    setMarketElements(main, baseID, quoteID, host)
    Doc.bind(main, 'scroll', () => {
      if (this.loading) return
      if (this.doneScrolling) return
      const belowBottom = page.eventsTable.offsetHeight - main.offsetHeight - main.scrollTop
      if (belowBottom < 0) {
        this.nextPage()
      }
    })
    this.setup(host, baseID, quoteID)
  }

  async nextPage () {
    this.loading = true
    const [events, updatedLogs, overview] = await this.getRunLogs()
    const assets = this.mktAssets()
    for (const event of events) {
      if (this.events[event.id]) continue
      const row = this.newEventRow(event, false, assets)
      this.events[event.id] = [event, row]
    }
    this.populateStats(overview.profitLoss, overview.endTime)
    this.updateExistingRows(updatedLogs)
    this.loading = false
  }

  async getRunLogs (): Promise<[MarketMakingEvent[], MarketMakingEvent[], MarketMakingRunOverview]> {
    const { mkt, startTime } = this
    const req: any = { market: mkt, startTime, n: logsBatchSize, filters: this.filters, refID: this.refID }
    const res = await postJSON('/api/mmrunlogs', req)
    if (!app().checkResponse(res)) {
      console.error('failed to get bot logs', res)
    }
    if (res.logs.length <= 1) {
      this.doneScrolling = true
    }
    if (res.logs.length > 0) {
      this.refID = res.logs[res.logs.length - 1].id
    }
    return [res.logs, res.updatedLogs || [], res.overview]
  }

  async applyFilters () {
    const page = this.page
    this.filters = {
      dexSells: !!page.dexSellsCheckbox.checked,
      dexBuys: !!page.dexBuysCheckbox.checked,
      cexSells: !!page.cexSellsCheckbox.checked,
      cexBuys: !!page.cexBuysCheckbox.checked,
      deposits: !!page.depositsCheckbox.checked,
      withdrawals: !!page.withdrawalsCheckbox.checked
    }
    this.refID = undefined
    const [events, , overview] = await this.getRunLogs()
    this.populateTable(events)
    this.populateStats(overview.profitLoss, overview.endTime)
  }

  setFilters () {
    const page = this.page
    page.dexSellsCheckbox.checked = true
    page.dexBuysCheckbox.checked = true
    page.cexSellsCheckbox.checked = true
    page.cexBuysCheckbox.checked = true
    page.depositsCheckbox.checked = true
    page.withdrawalsCheckbox.checked = true
    this.filters = {
      dexSells: true,
      dexBuys: true,
      cexSells: true,
      cexBuys: true,
      deposits: true,
      withdrawals: true
    }
  }

  async setup (host: string, baseID: number, quoteID: number) {
    const page = this.page
    this.setFilters()
    const { startTime } = this
    let profitLoss: ProfitLoss
    let endTime = 0
    const botStatus = liveBotStatus(host, baseID, quoteID)
    const [events, , overview] = await this.getRunLogs()
    if (botStatus?.runStats?.startTime === startTime) {
      this.liveBot = true
      this.fiatRates = app().fiatRatesMap
      profitLoss = botStatus.runStats.profitLoss
    } else {
      this.fiatRates = overview.finalState.fiatRates
      profitLoss = overview.profitLoss
      endTime = overview.endTime
    }
    this.populateStats(profitLoss, endTime)
    const assets = this.mktAssets()
    const parentHeader = page.sumUSDHeader.parentElement
    for (const asset of assets) {
      const th = document.createElement('th') as PageElement
      th.textContent = `${asset.symbol.toUpperCase()} Delta`
      if (parentHeader) {
        parentHeader.insertBefore(th, page.sumUSDHeader)
      }
    }
    this.populateTable(events)

    app().registerNoteFeeder({
      runevent: (note: RunEventNote) => { this.handleRunEventNote(note) },
      runstats: (note: RunStatsNote) => { this.handleRunStatsNote(note) }
    })
  }

  handleRunEventNote (note: RunEventNote) {
    const { baseID, quoteID, host } = this.mkt
    if (note.host !== host || note.baseID !== baseID || note.quoteID !== quoteID) return
    if (!eventPassesFilter(note.event, this.filters)) return
    const event = note.event
    const cachedEvent = this.events[event.id]
    if (cachedEvent) {
      this.setRowContents(cachedEvent[1], event, this.mktAssets())
      cachedEvent[0] = event
      return
    }
    const row = this.newEventRow(event, true, this.mktAssets())
    this.events[event.id] = [event, row]
  }

  handleRunStatsNote (note: RunStatsNote) {
    const { mkt: { baseID, quoteID, host }, startTime } = this
    if (note.host !== host ||
      note.baseID !== baseID ||
      note.quoteID !== quoteID) return
    if (!note.stats || note.stats.startTime !== startTime) return
    this.populateStats(note.stats.profitLoss, 0)
  }

  populateStats (pl: ProfitLoss, endTime: number) {
    const page = this.page
    page.startTime.textContent = new Date(this.startTime * 1000).toLocaleString()
    if (endTime === 0) {
      Doc.hide(page.endTimeRow)
    } else {
      page.endTime.textContent = new Date(endTime * 1000).toLocaleString()
    }
    for (const assetID in pl.diffs) {
      const asset = app().assets[parseInt(assetID)]
      let row = this.statsRows[assetID]
      if (!row) {
        row = page.performanceTableRowTmpl.cloneNode(true) as HTMLElement
        const tmpl = Doc.parseTemplate(row)
        tmpl.logo.src = Doc.logoPath(asset.symbol)
        tmpl.ticker.textContent = asset.symbol.toUpperCase()
        this.statsRows[assetID] = row
        page.performanceTableBody.appendChild(row)
      }
      const diff = pl.diffs[assetID]
      const tmpl = Doc.parseTemplate(row)
      tmpl.diff.textContent = diff.fmt
      tmpl.usdDiff.textContent = diff.fmtUSD
      tmpl.fiatRate.textContent = `${Doc.formatFiatValue(this.fiatRates[asset.id])} USD`
    }
    page.profitLoss.textContent = `${Doc.formatFiatValue(pl.profit)} USD`
  }

  mktAssets () : SupportedAsset[] {
    const baseAsset = app().assets[this.mkt.baseID]
    const quoteAsset = app().assets[this.mkt.quoteID]

    const assets = [baseAsset, quoteAsset]
    const assetIDs = { [baseAsset.id]: true, [quoteAsset.id]: true }

    if (baseAsset.token && !assetIDs[baseAsset.token.parentID]) {
      const baseTokenAsset = app().assets[baseAsset.token.parentID]
      assetIDs[baseTokenAsset.id] = true
      assets.push(baseTokenAsset)
    }

    if (quoteAsset.token && !assetIDs[quoteAsset.token.parentID]) {
      const quoteTokenAsset = app().assets[quoteAsset.token.parentID]
      assets.push(quoteTokenAsset)
    }

    return assets
  }

  updateExistingRows (updatedLogs: MarketMakingEvent[]) {
    for (const event of updatedLogs) {
      const cachedEvent = this.events[event.id]
      if (!cachedEvent) continue
      this.setRowContents(cachedEvent[1], event, this.mktAssets())
      cachedEvent[0] = event
    }
  }

  populateTable (events: MarketMakingEvent[]) {
    const page = this.page
    Doc.empty(page.eventsTableBody)
    this.events = {}
    this.doneScrolling = false
    const assets = this.mktAssets()
    for (const event of events) {
      const row = this.newEventRow(event, false, assets)
      this.events[event.id] = [event, row]
    }
  }

  setRowContents (row: HTMLElement, event: MarketMakingEvent, assets: SupportedAsset[]) {
    const tmpl = Doc.parseTemplate(row)
    tmpl.time.textContent = (new Date(event.timestamp * 1000)).toLocaleString()
    tmpl.eventType.textContent = this.eventType(event)
    let id
    if (event.depositEvent) {
      id = event.depositEvent.transaction.id
    } else if (event.withdrawalEvent) {
      id = event.withdrawalEvent.id
    } else if (event.dexOrderEvent) {
      id = event.dexOrderEvent.id
    } else if (event.cexOrderEvent) {
      id = event.cexOrderEvent.id
    }
    if (id) {
      tmpl.eventID.textContent = trimStringWithEllipsis(id, 30)
      tmpl.eventID.setAttribute('title', id)
    }
    let usd = 0
    for (const asset of assets) {
      const be = event.balanceEffects
      const sum = sumBalanceEffects(asset.id, be)
      const tmplID = `sum${asset.symbol.toUpperCase()}`
      let el : PageElement
      if (tmpl[tmplID]) {
        el = tmpl[tmplID]
      } else {
        el = document.createElement('td')
        el.dataset.tmpl = tmplID
        const parent = tmpl.sumUSD.parentElement
        if (parent) {
          parent.insertBefore(el, tmpl.sumUSD)
        }
      }
      el.textContent = Doc.formatCoinValue(sum, asset.unitInfo)
      const factor = asset.unitInfo.conventional.conversionFactor
      usd += sum / factor * this.fiatRates[asset.id] || 0
    }
    tmpl.sumUSD.textContent = Doc.formatFourSigFigs(usd)
    Doc.bind(tmpl.details, 'click', () => { this.showEventDetails(event.id) })
  }

  newEventRow (event: MarketMakingEvent, prepend: boolean, assets: SupportedAsset[]) : HTMLElement {
    const page = this.page
    const row = page.eventTableRowTmpl.cloneNode(true) as HTMLElement
    row.id = event.id.toString()
    this.setRowContents(row, event, assets)
    if (prepend) {
      page.eventsTableBody.insertBefore(row, page.eventsTableBody.firstChild)
    } else {
      page.eventsTableBody.appendChild(row)
    }
    return row
  }

  eventType (event: MarketMakingEvent) : string {
    if (event.depositEvent) {
      return 'Deposit'
    } else if (event.withdrawalEvent) {
      return 'Withdrawal'
    } else if (event.dexOrderEvent) {
      return event.dexOrderEvent.sell ? 'DEX Sell' : 'DEX Buy'
    } else if (event.cexOrderEvent) {
      return event.cexOrderEvent.sell ? 'CEX Sell' : 'CEX Buy'
    }

    return ''
  }

  showDexOrderEventDetails (event: DEXOrderEvent) {
    const { page, mkt: { baseID, quoteID } } = this
    const baseAsset = app().assets[baseID]
    const quoteAsset = app().assets[quoteID]
    const [bui, qui] = [baseAsset.unitInfo, quoteAsset.unitInfo]
    const [baseTicker, quoteTicker] = [bui.conventional.unit, qui.conventional.unit]
    if (this.dexOrderIDCopyListener !== undefined) {
      page.copyDexOrderID.removeEventListener('click', this.dexOrderIDCopyListener)
    }
    this.dexOrderIDCopyListener = () => { setupCopyBtn(event.id, page.dexOrderID, page.copyDexOrderID, '#1e7d11') }
    page.copyDexOrderID.addEventListener('click', this.dexOrderIDCopyListener)
    page.dexOrderID.textContent = trimStringWithEllipsis(event.id, 20)
    page.dexOrderID.setAttribute('title', event.id)
    const rate = app().conventionalRate(baseID, quoteID, event.rate)

    page.dexOrderRate.textContent = `${rate} ${baseTicker}/${quoteTicker}`
    page.dexOrderQty.textContent = `${event.qty / bui.conventional.conversionFactor} ${baseTicker}`
    if (event.sell) {
      page.dexOrderSide.textContent = intl.prep(intl.ID_SELL)
    } else {
      page.dexOrderSide.textContent = intl.prep(intl.ID_BUY)
    }
    Doc.empty(page.dexOrderTxsTableBody)
    Doc.setVis(event.transactions && event.transactions.length > 0, page.dexOrderTxsTable)
    const txAsset = (txType: number, sell: boolean) : SupportedAsset | undefined => {
      switch (txType) {
        case wallets.txTypeSwap:
        case wallets.txTypeRefund:
        case wallets.txTypeSplit:
          return sell ? baseAsset : quoteAsset
        case wallets.txTypeRedeem:
          return sell ? quoteAsset : baseAsset
      }
    }

    for (let i = 0; event.transactions && i < event.transactions.length; i++) {
      const tx = event.transactions[i]
      const row = page.dexOrderTxRowTmpl.cloneNode(true) as HTMLElement
      const tmpl = Doc.parseTemplate(row)
      tmpl.id.textContent = trimStringWithEllipsis(tx.id, 20)
      tmpl.id.setAttribute('title', tx.id)
      tmpl.type.textContent = wallets.txTypeString(tx.type)
      const asset = txAsset(tx.type, event.sell)
      if (!asset) {
        console.error('unexpected tx type in dex order event', tx.type)
        continue
      }
      const assetExplorer = CoinExplorers[asset.id]
      if (assetExplorer && assetExplorer[net]) {
        tmpl.explorerLink.href = assetExplorer[net](tx.id)
      }
      tmpl.amt.textContent = `${Doc.formatCoinValue(tx.amount, asset.unitInfo)} ${asset.unitInfo.conventional.unit.toLowerCase()}`
      tmpl.fees.textContent = `${Doc.formatCoinValue(tx.fees, asset.unitInfo)} ${asset.unitInfo.conventional.unit.toLowerCase()}`
      page.dexOrderTxsTableBody.appendChild(row)
    }
    this.forms.show(page.dexOrderDetailsForm)
  }

  showCexOrderEventDetails (event: CEXOrderEvent) {
    const { page, mkt: { baseID, quoteID } } = this
    const baseAsset = app().assets[baseID]
    const quoteAsset = app().assets[quoteID]
    const [bui, qui] = [baseAsset.unitInfo, quoteAsset.unitInfo]
    const [baseTicker, quoteTicker] = [bui.conventional.unit, qui.conventional.unit]

    page.cexOrderID.textContent = trimStringWithEllipsis(event.id, 20)
    if (this.cexOrderIDCopyListener !== undefined) {
      page.copyCexOrderID.removeEventListener('click', this.cexOrderIDCopyListener)
    }
    this.cexOrderIDCopyListener = () => { setupCopyBtn(event.id, page.cexOrderID, page.copyCexOrderID, '#1e7d11') }
    page.copyCexOrderID.addEventListener('click', this.cexOrderIDCopyListener)
    page.cexOrderID.setAttribute('title', event.id)
    const rate = app().conventionalRate(baseID, quoteID, event.rate)
    page.cexOrderRate.textContent = `${rate} ${baseTicker}/${quoteTicker}`
    page.cexOrderQty.textContent = `${event.qty / bui.conventional.conversionFactor} ${baseTicker}`
    if (event.sell) {
      page.cexOrderSide.textContent = intl.prep(intl.ID_SELL)
    } else {
      page.cexOrderSide.textContent = intl.prep(intl.ID_BUY)
    }
    page.cexOrderBaseFilled.textContent = `${event.baseFilled / bui.conventional.conversionFactor} ${baseTicker}`
    page.cexOrderQuoteFilled.textContent = `${event.quoteFilled / qui.conventional.conversionFactor} ${quoteTicker}`
    this.forms.show(page.cexOrderDetailsForm)
  }

  showDepositEventDetails (event: DepositEvent, pending: boolean) {
    const page = this.page
    page.depositID.textContent = trimStringWithEllipsis(event.transaction.id, 20)
    if (this.depositIDCopyListener !== undefined) {
      page.copyDepositID.removeEventListener('click', this.depositIDCopyListener)
    }
    this.depositIDCopyListener = () => { setupCopyBtn(event.transaction.id, page.depositID, page.copyDepositID, '#1e7d11') }
    page.copyDepositID.addEventListener('click', this.depositIDCopyListener)
    page.depositID.setAttribute('title', event.transaction.id)
    const unitInfo = app().assets[event.assetID].unitInfo
    const unit = unitInfo.conventional.unit
    page.depositAmt.textContent = `${Doc.formatCoinValue(event.transaction.amount, unitInfo)} ${unit}`
    page.depositFees.textContent = `${Doc.formatCoinValue(event.transaction.fees, unitInfo)} ${unit}`
    page.depositStatus.textContent = pending ? intl.prep(intl.ID_PENDING) : intl.prep(intl.ID_COMPLETE)
    Doc.setVis(!pending, page.depositCreditSection)
    if (!pending) {
      page.depositCredit.textContent = `${Doc.formatCoinValue(event.cexCredit, unitInfo)} ${unit}`
    }
    this.forms.show(page.depositDetailsForm)
  }

  showWithdrawalEventDetails (event: WithdrawalEvent, pending: boolean) {
    const page = this.page
    page.withdrawalID.textContent = trimStringWithEllipsis(event.id, 20)
    if (this.withdrawalIDCopyListener !== undefined) {
      page.copyWithdrawalID.removeEventListener('click', this.withdrawalIDCopyListener)
    }
    this.withdrawalIDCopyListener = () => { setupCopyBtn(event.id, page.withdrawalID, page.copyWithdrawalID, '#1e7d11') }
    page.copyWithdrawalID.addEventListener('click', this.withdrawalIDCopyListener)
    page.withdrawalID.setAttribute('title', event.id)
    const unitInfo = app().assets[event.assetID].unitInfo
    const unit = unitInfo.conventional.unit
    page.withdrawalAmt.textContent = `${Doc.formatCoinValue(event.cexDebit, unitInfo)} ${unit}`
    page.withdrawalStatus.textContent = pending ? intl.prep(intl.ID_PENDING) : intl.prep(intl.ID_COMPLETE)
    if (event.transaction) {
      page.withdrawalTxID.textContent = trimStringWithEllipsis(event.transaction.id, 20)
      page.withdrawalTxID.setAttribute('title', event.transaction.id)
      page.withdrawalReceived.textContent = `${Doc.formatCoinValue(event.transaction.amount, unitInfo)} ${unit}`
    }
    this.forms.show(page.withdrawalDetailsForm)
  }

  showEventDetails (eventID: number) {
    const [event] = this.events[eventID]
    if (event.dexOrderEvent) this.showDexOrderEventDetails(event.dexOrderEvent)
    if (event.cexOrderEvent) this.showCexOrderEventDetails(event.cexOrderEvent)
    if (event.depositEvent) this.showDepositEventDetails(event.depositEvent, event.pending)
    if (event.withdrawalEvent) this.showWithdrawalEventDetails(event.withdrawalEvent, event.pending)
  }
}

function trimStringWithEllipsis (str: string, maxLen: number): string {
  if (str.length <= maxLen) return str
  return `${str.substring(0, maxLen / 2)}...${str.substring(str.length - maxLen / 2)}`
}

function sumBalanceEffects (assetID: number, be: BalanceEffects): number {
  let sum = 0
  if (be.settled[assetID]) sum += be.settled[assetID]
  if (be.pending[assetID]) sum += be.pending[assetID]
  if (be.locked[assetID]) sum += be.locked[assetID]
  if (be.reserved[assetID]) sum += be.reserved[assetID]
  return sum
}
