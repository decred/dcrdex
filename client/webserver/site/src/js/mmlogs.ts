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
import { MM } from './mm'
import * as intl from './locales'
import * as wallets from './wallets'

export default class MarketMakerLogsPage extends BasePage {
  page: Record<string, PageElement>
  host: string
  baseID: number
  quoteID: number
  startTime: number
  baseUnitInfo: UnitInfo
  quoteUnitInfo: UnitInfo
  events: Record<number, MarketMakingEvent>
  forms: Forms

  constructor (main: HTMLElement) {
    super()
    const page = this.page = Doc.idDescendants(main)
    Doc.cleanTemplates(page.eventTableRowTmpl, page.dexOrderTxRowTmpl)
    const urlParams = new URLSearchParams(window.location.search)
    this.host = urlParams.get('host') || ''
    this.baseID = parseInt(urlParams.get('baseID') || '0')
    this.quoteID = parseInt(urlParams.get('quoteID') || '0')
    this.startTime = parseInt(urlParams.get('startTime') || '0')
    this.forms = new Forms(page.forms)
    this.events = {}
    page.baseHeader.textContent = app().assets[this.baseID].symbol.toUpperCase()
    page.quoteHeader.textContent = app().assets[this.quoteID].symbol.toUpperCase()
    page.hostHeader.textContent = this.host
    const baseLogo = Doc.logoPathFromID(this.baseID)
    const quoteLogo = Doc.logoPathFromID(this.quoteID)
    page.baseLogo.src = baseLogo
    page.quoteLogo.src = quoteLogo
    page.baseChangeLogo.src = baseLogo
    page.quoteChangeLogo.src = quoteLogo
    page.baseFeesLogo.src = baseLogo
    page.quoteFeesLogo.src = quoteLogo
    this.setup()
  }

  async getRunLogs (): Promise<MarketMakingEvent[]> {
    const market: any = { host: this.host, base: this.baseID, quote: this.quoteID }
    const req: any = { market, startTime: this.startTime }
    const res = await postJSON('/api/mmrunlogs', req)
    if (!app().checkResponse(res)) {
      console.error('failed to get bot logs', res)
    }
    return res.logs
  }

  async setup () {
    this.baseUnitInfo = await app().assets[this.baseID].unitInfo
    this.quoteUnitInfo = await app().assets[this.quoteID].unitInfo
    const runStats = await MM.botStats(this.baseID, this.quoteID, this.host, this.startTime)
    if (runStats) {
      this.populateStats(runStats.baseBalanceDelta, runStats.quoteBalanceDelta, runStats.baseFees, runStats.quoteFees, runStats.profitLoss, 0)
    } else {
      const overview = await MM.mmRunOverview(this.host, this.baseID, this.quoteID, this.startTime)
      this.populateStats(overview.baseDelta, overview.quoteDelta, overview.baseFees, overview.quoteFees, overview.profitLoss, overview.endTime)
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

  setStats (baseDelta: number, quoteDelta: number, baseFees: number, quoteFees: number, profitLoss: number) {
    const page = this.page
    const baseSymbol = app().assets[this.baseID].symbol.toUpperCase()
    const quoteSymbol = app().assets[this.quoteID].symbol.toUpperCase()
    page.baseChange.textContent = `${Doc.formatCoinValue(baseDelta, this.baseUnitInfo)} ${baseSymbol}`
    page.quoteChange.textContent = `${Doc.formatCoinValue(quoteDelta, this.quoteUnitInfo)} ${quoteSymbol}`
    page.baseFees.textContent = `${Doc.formatCoinValue(baseFees, this.baseUnitInfo)} ${baseSymbol}`
    page.quoteFees.textContent = `${Doc.formatCoinValue(quoteFees, this.quoteUnitInfo)} ${quoteSymbol}`
    page.profitLoss.textContent = `$${Doc.formatFiatValue(profitLoss)}`
  }

  handleRunStatsNote (note: RunStatsNote) {
    if (note.host !== this.host ||
      note.base !== this.baseID ||
      note.quote !== this.quoteID) return
    if (!note.stats || note.stats.startTime !== this.startTime) return
    this.setStats(note.stats.baseBalanceDelta, note.stats.quoteBalanceDelta, note.stats.baseFees, note.stats.quoteFees, note.stats.profitLoss)
  }

  populateStats (baseDelta: number, quoteDelta: number, baseFees: number, quoteFees: number, profitLoss: number, endTime: number) {
    const page = this.page
    page.startTime.textContent = new Date(this.startTime * 1000).toLocaleString()
    if (endTime === 0) {
      Doc.hide(page.endTimeBlock)
    } else {
      page.endTime.textContent = new Date(endTime * 1000).toLocaleString()
    }
    this.setStats(baseDelta, quoteDelta, baseFees, quoteFees, profitLoss)
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
    tmpl.time.textContent = (new Date(event.timeStamp * 1000)).toLocaleString()
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
    tmpl.baseDelta.textContent = Doc.formatCoinValue(event.baseDelta, this.baseUnitInfo)
    tmpl.quoteDelta.textContent = Doc.formatCoinValue(event.quoteDelta, this.quoteUnitInfo)
    tmpl.baseFees.textContent = Doc.formatCoinValue(event.baseFees, this.baseUnitInfo)
    tmpl.quoteFees.textContent = Doc.formatCoinValue(event.quoteFees, this.quoteUnitInfo)
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
    const page = this.page
    page.dexOrderID.textContent = trimStringWithEllipsis(event.id, 20)
    page.dexOrderID.setAttribute('title', event.id)
    const rate = app().conventionalRate(this.baseID, this.quoteID, event.rate)
    const [bUnit, qUnit] = [this.baseUnitInfo.conventional.unit.toLowerCase(), this.quoteUnitInfo.conventional.unit.toLowerCase()]
    page.dexOrderRate.textContent = `${rate} ${bUnit}/${qUnit}`
    page.dexOrderQty.textContent = `${event.qty / this.baseUnitInfo.conventional.conversionFactor} ${bUnit}`
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
          return sell ? this.baseUnitInfo : this.quoteUnitInfo
        case wallets.txTypeRedeem:
          return sell ? this.quoteUnitInfo : this.baseUnitInfo
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
    const page = this.page
    page.cexOrderID.textContent = trimStringWithEllipsis(event.id, 20)
    page.cexOrderID.setAttribute('title', event.id)
    const rate = app().conventionalRate(this.baseID, this.quoteID, event.rate)
    const [bUnit, qUnit] = [this.baseUnitInfo.conventional.unit.toLowerCase(), this.quoteUnitInfo.conventional.unit.toLowerCase()]
    page.cexOrderRate.textContent = `${rate} ${bUnit}/${qUnit}`
    page.cexOrderQty.textContent = `${event.qty / this.baseUnitInfo.conventional.conversionFactor} ${bUnit}`
    if (event.sell) {
      page.cexOrderSide.textContent = intl.prep(intl.ID_SELL)
    } else {
      page.cexOrderSide.textContent = intl.prep(intl.ID_BUY)
    }
    page.cexOrderBaseFilled.textContent = `${event.baseFilled / this.baseUnitInfo.conventional.conversionFactor} ${bUnit}`
    page.cexOrderQuoteFilled.textContent = `${event.quoteFilled / this.quoteUnitInfo.conventional.conversionFactor} ${qUnit}`
    this.forms.show(page.cexOrderDetailsForm)
  }

  showDepositEventDetails (event: DepositEvent, pending: boolean) {
    const page = this.page
    page.depositID.textContent = trimStringWithEllipsis(event.transaction.id, 20)
    page.depositID.setAttribute('title', event.transaction.id)
    const unitInfo = app().assets[event.assetID].unitInfo
    page.depositAmt.textContent = `${Doc.formatCoinValue(event.transaction.amount, unitInfo)} ${unitInfo.conventional.unit.toUpperCase()}`
    page.depositFees.textContent = `${Doc.formatCoinValue(event.transaction.fees, unitInfo)} ${unitInfo.conventional.unit.toUpperCase()}`
    page.depositStatus.textContent = pending ? intl.prep(intl.ID_PENDING) : intl.prep(intl.ID_COMPLETE)
    Doc.setVis(!pending, page.depositCreditSection)
    if (!pending) {
      page.depositCredit.textContent = `${Doc.formatCoinValue(event.cexCredit, unitInfo)} ${unitInfo.conventional.unit.toUpperCase()}`
    }
    this.forms.show(page.depositDetailsForm)
  }

  showWithdrawalEventDetails (event: WithdrawalEvent, pending: boolean) {
    const page = this.page
    page.withdrawalID.textContent = trimStringWithEllipsis(event.id, 20)
    page.withdrawalID.setAttribute('title', event.id)
    const unitInfo = app().assets[event.assetID].unitInfo
    page.withdrawalAmt.textContent = `${Doc.formatCoinValue(event.cexDebit, unitInfo)} ${unitInfo.conventional.unit.toUpperCase()}`
    page.withdrawalStatus.textContent = pending ? intl.prep(intl.ID_PENDING) : intl.prep(intl.ID_COMPLETE)
    if (event.transaction) {
      page.withdrawalTxID.textContent = trimStringWithEllipsis(event.transaction.id, 20)
      page.withdrawalTxID.setAttribute('title', event.transaction.id)
      page.withdrawalReceived.textContent = `${Doc.formatCoinValue(event.transaction.amount, unitInfo)} ${unitInfo.conventional.unit.toUpperCase()}`
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
