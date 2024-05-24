import {
  app,
  PageElement,
  UnitInfo,
  MarketMakingEvent,
  DEXOrderEvent,
  CEXOrderEvent,
  RunEventNote,
  RunStatsNote,
  DepositEvent,
  WithdrawalEvent
} from './registry'
import { Forms } from './forms'
import { postJSON } from './http'
import Doc from './doc'
import BasePage from './basepage'
import { MM, setMarketElements } from './mmutil'
import * as intl from './locales'
import * as wallets from './wallets'

interface LogsPageParams {
  host: string
  quoteID: number
  baseID: number
  startTime: number
}

export default class MarketMakerLogsPage extends BasePage {
  page: Record<string, PageElement>
  host: string
  baseID: number
  bui: UnitInfo
  baseTicker: string
  quoteID: number
  qui: UnitInfo
  quoteTicker: string
  startTime: number
  events: Record<number, MarketMakingEvent>
  forms: Forms

  constructor (main: HTMLElement, params: LogsPageParams) {
    super()
    const page = this.page = Doc.idDescendants(main)
    Doc.cleanTemplates(page.eventTableRowTmpl, page.dexOrderTxRowTmpl)
    if (params?.host) {
      this.host = params.host
      this.baseID = params.baseID
      this.quoteID = params.quoteID
      this.startTime = params.startTime
    } else {
      const urlParams = new URLSearchParams(window.location.search)
      this.host = urlParams.get('host') || ''
      this.baseID = parseInt(urlParams.get('baseID') || '0')
      this.quoteID = parseInt(urlParams.get('quoteID') || '0')
      this.startTime = parseInt(urlParams.get('startTime') || '0')
    }
    const { unitInfo: bui } = app().assets[this.baseID]
    this.bui = bui
    this.baseTicker = bui.conventional.unit
    const { unitInfo: qui } = app().assets[this.quoteID]
    this.qui = qui
    this.quoteTicker = qui.conventional.unit
    this.forms = new Forms(page.forms)
    this.events = {}
    setMarketElements(main, this.baseID, this.quoteID, this.host)
    this.setup()
  }

  async getRunLogs (): Promise<MarketMakingEvent[]> {
    const { baseID, quoteID, host, startTime } = this
    const market: any = { host, base: baseID, quote: quoteID }
    const req: any = { market, startTime }
    const res = await postJSON('/api/mmrunlogs', req)
    if (!app().checkResponse(res)) {
      console.error('failed to get bot logs', res)
    }
    return res.logs
  }

  async setup () {
    const { baseID, quoteID, host, startTime } = this
    const runStats = await MM.botStats(baseID, quoteID, host, startTime)
    if (runStats) {
      this.populateStats(runStats.profitLoss, 0)
    } else {
      const overview = await MM.mmRunOverview(host, baseID, quoteID, startTime)
      this.populateStats(overview.profitLoss, overview.endTime)
    }
    Doc.bind(this.page.backButton, 'click', () => { app().loadPage(runStats ? 'mm' : 'mmarchives') })
    const events = await this.getRunLogs()
    this.populateTable(events)
    app().registerNoteFeeder({
      runevent: (note: RunEventNote) => { this.handleRunEventNote(note) },
      runstats: (note: RunStatsNote) => { this.handleRunStatsNote(note) }
    })
  }

  handleRunEventNote (note: RunEventNote) {
    if (note.host !== this.host || note.base !== this.baseID || note.quote !== this.quoteID) return
    const page = this.page
    const event = note.event
    this.events[event.id] = event
    for (let i = 0; i < page.eventsTableBody.children.length; i++) {
      const row = page.eventsTableBody.children[i] as HTMLElement
      if (row.id === event.id.toString()) {
        this.setRowContents(row, event)
        return
      }
    }
    this.newEventRow(event, true)
  }

  handleRunStatsNote (note: RunStatsNote) {
    if (note.host !== this.host ||
      note.baseID !== this.baseID ||
      note.quoteID !== this.quoteID) return
    if (!note.stats || note.stats.startTime !== this.startTime) return
    this.page.profitLoss.textContent = `$${Doc.formatFiatValue(note.stats.profitLoss)}`
  }

  populateStats (profitLoss: number, endTime: number) {
    const page = this.page
    page.startTime.textContent = new Date(this.startTime * 1000).toLocaleString()
    if (endTime === 0) {
      Doc.hide(page.endTimeBlock)
    } else {
      page.endTime.textContent = new Date(endTime * 1000).toLocaleString()
    }
    page.profitLoss.textContent = `$${Doc.formatFiatValue(profitLoss)}`
  }

  populateTable (events: MarketMakingEvent[]) {
    Doc.empty(this.page.eventsTableBody)
    for (const event of events) {
      this.events[event.id] = event
      this.newEventRow(event, false)
    }
  }

  setRowContents (row: HTMLElement, event: MarketMakingEvent) {
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
    tmpl.baseDelta.textContent = Doc.formatCoinValue(event.baseDelta, this.bui)
    tmpl.quoteDelta.textContent = Doc.formatCoinValue(event.quoteDelta, this.qui)
    tmpl.baseFees.textContent = Doc.formatCoinValue(event.baseFees, this.bui)
    tmpl.quoteFees.textContent = Doc.formatCoinValue(event.quoteFees, this.qui)
    Doc.bind(tmpl.details, 'click', () => { this.showEventDetails(event.id) })
  }

  newEventRow (event: MarketMakingEvent, prepend: boolean) {
    const page = this.page
    const row = page.eventTableRowTmpl.cloneNode(true) as HTMLElement
    row.id = event.id.toString()
    this.setRowContents(row, event)
    if (prepend) {
      page.eventsTableBody.insertBefore(row, page.eventsTableBody.firstChild)
    } else {
      page.eventsTableBody.appendChild(row)
    }
  }

  eventType (event: MarketMakingEvent) : string {
    if (event.depositEvent) {
      return 'Deposit'
    } else if (event.withdrawalEvent) {
      return 'Withdrawal'
    } else if (event.dexOrderEvent) {
      return 'DEX Order'
    } else if (event.cexOrderEvent) {
      return 'CEX Order'
    }

    return ''
  }

  showDexOrderEventDetails (event: DEXOrderEvent) {
    const { page, bui, qui, baseTicker, quoteTicker } = this
    page.dexOrderID.textContent = trimStringWithEllipsis(event.id, 20)
    page.dexOrderID.setAttribute('title', event.id)
    const rate = app().conventionalRate(this.baseID, this.quoteID, event.rate)

    page.dexOrderRate.textContent = `${rate} ${baseTicker}/${quoteTicker}`
    page.dexOrderQty.textContent = `${event.qty / bui.conventional.conversionFactor} ${baseTicker}`
    if (event.sell) {
      page.dexOrderSide.textContent = intl.prep(intl.ID_SELL)
    } else {
      page.dexOrderSide.textContent = intl.prep(intl.ID_BUY)
    }
    Doc.empty(page.dexOrderTxsTableBody)
    Doc.setVis(event.transactions && event.transactions.length > 0, page.dexOrderTxsTable)
    const txUnits = (txType: number, sell: boolean) : UnitInfo | undefined => {
      switch (txType) {
        case wallets.txTypeSwap:
        case wallets.txTypeRefund:
        case wallets.txTypeSplit:
          return sell ? bui : qui
        case wallets.txTypeRedeem:
          return sell ? qui : bui
      }
    }
    for (let i = 0; event.transactions && i < event.transactions.length; i++) {
      const tx = event.transactions[i]
      const row = page.dexOrderTxRowTmpl.cloneNode(true) as HTMLElement
      const tmpl = Doc.parseTemplate(row)
      tmpl.id.textContent = trimStringWithEllipsis(tx.id, 20)
      tmpl.id.setAttribute('title', tx.id)
      tmpl.type.textContent = wallets.txTypeString(tx.type)
      const unitInfo = txUnits(tx.type, event.sell)
      if (!unitInfo) {
        console.error('unexpected tx type in dex order event', tx.type)
        continue
      }
      tmpl.amt.textContent = `${Doc.formatCoinValue(tx.amount, unitInfo)} ${unitInfo.conventional.unit.toLowerCase()}`
      tmpl.fees.textContent = `${Doc.formatCoinValue(tx.fees, unitInfo)} ${unitInfo.conventional.unit.toLowerCase()}`
      page.dexOrderTxsTableBody.appendChild(row)
    }
    this.forms.show(page.dexOrderDetailsForm)
  }

  showCexOrderEventDetails (event: CEXOrderEvent) {
    const { page, baseID, quoteID, bui, qui, quoteTicker, baseTicker } = this
    page.cexOrderID.textContent = trimStringWithEllipsis(event.id, 20)
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
    const event = this.events[eventID]
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
