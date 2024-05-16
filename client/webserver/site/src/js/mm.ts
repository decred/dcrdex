import {
  app,
  PageElement,
  BotConfig,
  CEXConfig,
  MMBotStatus,
  RunStatsNote,
  MarketMakingStatus,
  ExchangeBalance,
  RunStats,
  MarketMakingRunOverview
} from './registry'
import { getJSON, postJSON } from './http'
import Doc from './doc'
import State from './state'
import BasePage from './basepage'
import { setOptionTemplates } from './opts'
import { bind as bindForm, NewWalletForm, Forms } from './forms'
import * as intl from './locales'

export const botTypeBasicMM = 'basicMM'
export const botTypeArbMM = 'arbMM'
export const botTypeBasicArb = 'basicArb'

interface CEXDisplayInfo {
  name: string
  logo: string
}

export const CEXDisplayInfos: Record<string, CEXDisplayInfo> = {
  'Binance': {
    name: 'Binance',
    logo: '/img/binance.com.png'
  },
  'BinanceUS': {
    name: 'Binance U.S.',
    logo: '/img/binance.us.png'
  }
}

function marketStr (host: string, baseID: number, quoteID: number): string {
  return `${host}-${baseID}-${quoteID}`
}

export default class MarketMakerPage extends BasePage {
  page: Record<string, PageElement>
  forms: Forms
  currentForm: HTMLElement
  keyup: (e: KeyboardEvent) => void
  newWalletForm: NewWalletForm
  botConfigs: BotConfig[]
  mmStatus: MarketMakingStatus

  constructor (main: HTMLElement) {
    super()

    const page = this.page = Doc.idDescendants(main)

    this.forms = new Forms(page.forms)

    Doc.bind(page.addBotBtnNoExisting, 'click', () => { this.newBot() })
    Doc.bind(page.addBotBtnWithExisting, 'click', () => { this.newBot() })
    Doc.bind(page.startBotsBtn, 'click', () => { this.start() })
    Doc.bind(page.stopBotsBtn, 'click', () => {
      MM.stop()
      Doc.hide(page.stopBotsBtn, page.onMsg)
      Doc.show(page.startBotsBtn, page.offMsg)
    })
    Doc.bind(page.archivedLogsBtn, 'click', () => { app().loadPage('mmarchives') })
    bindForm(page.pwForm, page.pwSubmit, () => this.startBots())

    setOptionTemplates(page)

    Doc.cleanTemplates(page.orderOptTmpl, page.booleanOptTmpl, page.rangeOptTmpl,
      page.botTableRowTmpl)

    this.setup()
  }

  async setup () {
    const page = this.page
    const mmStatus = this.mmStatus = await MM.status()

    const running = mmStatus.bots.some((botStatus: MMBotStatus) => botStatus.running)

    const botConfigs = this.botConfigs = mmStatus.bots.map((s: MMBotStatus) => s.config)
    app().registerNoteFeeder({
      runstats: (note: RunStatsNote) => { this.handleRunStatsNote(note) }
      // TODO bot start-stop notification
    })

    const noBots = !botConfigs || botConfigs.length === 0
    Doc.setVis(noBots, page.noBots)
    Doc.setVis(!noBots, page.botTable, page.onOff)
    if (noBots) return

    page.onIndicator.classList.add(running ? 'on' : 'off')
    page.onIndicator.classList.remove(running ? 'off' : 'on')
    Doc.setVis(running, page.stopBotsBtn, page.onMsg)
    Doc.setVis(!running, page.startBotsBtn, page.offMsg)
    await this.setupBotTable(botConfigs, mmStatus)
  }

  async handleRunStatsNote (note: RunStatsNote) {
    // something's running
    const { page } = this
    Doc.show(page.stopBotsBtn, page.onMsg)
    Doc.hide(page.startBotsBtn, page.offMsg)
    const tableRows = page.botTableBody.children
    const rowID = marketStr(note.host, note.base, note.quote)
    for (let i = 0; i < tableRows.length; i++) {
      const row = tableRows[i] as PageElement
      if (row.id === rowID) {
        const rowTmpl = Doc.parseTemplate(row)
        this.setTableRowRunning(rowTmpl, true)
        const botCfg = this.botConfigs[i]
        this.setTableRowBalances(rowTmpl, botCfg, await this.botStatusForConfig(botCfg))
        return
      }
    }
  }

  unload (): void {
    Doc.unbind(document, 'keyup', this.keyup)
  }

  newBot () {
    app().loadPage('mmsettings')
  }

  valueWithUnit (assetID: number, amount: number): string {
    const asset = app().assets[assetID]
    const unitInfo = asset.unitInfo
    const assetValue = Doc.formatCoinValue(amount, unitInfo)
    return `${assetValue} ${asset.symbol.toUpperCase()}`
  }

  percentageBalanceStr (assetID: number, balance: number, percentage: number): string {
    const asset = app().assets[assetID]
    const unitInfo = asset.unitInfo
    const assetValue = Doc.formatCoinValue((balance * percentage) / 100, unitInfo)
    return `${Doc.formatFourSigFigs(percentage)}% - ${assetValue} ${asset.symbol.toUpperCase()}`
  }

  /*
   * walletBalanceStr returns a string like "50% - 0.0001 BTC" representing
   * the percentage of a wallet's balance selected in the market maker setting,
   * and the amount of that asset in the wallet.
   */
  walletBalanceStr (assetID: number, percentage: number): string {
    const { wallet: { balance: { available } } } = app().assets[assetID]
    return this.percentageBalanceStr(assetID, available, percentage)
  }

  runningBalanceStr (assetID: number, amount: number): string {
    const { unitInfo } = app().assets[assetID]
    const assetValue = Doc.formatCoinValue(amount, unitInfo)
    return `${assetValue} ${unitInfo.conventional.unit}`
  }

  /*
   * setTableRowRunning sets the visibility of elements on a market row depending
   * on whether the entire market maker is running, and specifically whether the
   * the current bot is running.
   */
  setTableRowRunning (rowTmpl: Record<string, PageElement>, running: boolean | undefined) {
    Doc.setVis(running, rowTmpl.running, rowTmpl.logs, rowTmpl.runningIcon)
    Doc.setVis(!running, rowTmpl.removeTd, rowTmpl.notRunningIcon)
  }

  setTableRowBalances (rowTmpl: Record<string, PageElement>, botCfg: BotConfig, botStatus: MMBotStatus) {
    if (botStatus.running) {
      Doc.show(rowTmpl.profitLossTd)
      Doc.hide(rowTmpl.baseBalanceTd, rowTmpl.quoteBalanceTd)
      return
    }

    Doc.show(rowTmpl.baseBalanceTd, rowTmpl.quoteBalanceTd)

    const baseAsset = app().assets[botCfg.baseID]
    const quoteAsset = app().assets[botCfg.quoteID]
    const baseLogoPath = Doc.logoPath(baseAsset.symbol)
    const quoteLogoPath = Doc.logoPath(quoteAsset.symbol)
    rowTmpl.baseBalanceLogo.src = baseLogoPath
    rowTmpl.quoteBalanceLogo.src = quoteLogoPath
    if (botCfg.cexCfg) {
      const dinfo = CEXDisplayInfos[botCfg.cexCfg?.name || '']
      const cexLogoSrc = dinfo.logo
      rowTmpl.baseBalanceCexLogo.src = cexLogoSrc
      rowTmpl.quoteBalanceCexLogo.src = cexLogoSrc
      rowTmpl.cexBaseBalanceLogo.src = baseLogoPath
      rowTmpl.cexQuoteBalanceLogo.src = quoteLogoPath
    }

    Doc.setVis(!!botCfg.cexCfg, rowTmpl.cexBaseBalanceContainer, rowTmpl.cexQuoteBalanceContainer)
    Doc.setVis(botStatus.running, rowTmpl.baseBalanceDetails, rowTmpl.quoteBalanceDetails, rowTmpl.cexBaseBalanceDetails, rowTmpl.cexQuoteBalanceDetails, rowTmpl.profitLossTd)

    if (botStatus.running) {
      const baseBalances = botStatus.balances[botCfg.baseID].dex
      rowTmpl.runningBaseBalanceAvailable.textContent = this.runningBalanceStr(botCfg.baseID, baseBalances.available)
      rowTmpl.runningBaseBalanceLocked.textContent = this.runningBalanceStr(botCfg.baseID, baseBalances.locked)
      rowTmpl.runningBaseBalancePending.textContent = this.runningBalanceStr(botCfg.baseID, baseBalances.pending)
      const baseBalanceTotal = baseBalances.available + baseBalances.locked + baseBalances.pending
      rowTmpl.baseBalance.textContent = this.runningBalanceStr(botCfg.baseID, baseBalanceTotal)
      const quoteBalances = botStatus.balances[botCfg.quoteID].dex
      rowTmpl.runningQuoteBalanceAvailable.textContent = this.runningBalanceStr(botCfg.quoteID, quoteBalances.available)
      rowTmpl.runningQuoteBalanceLocked.textContent = this.runningBalanceStr(botCfg.quoteID, quoteBalances.locked)
      rowTmpl.runningQuoteBalancePending.textContent = this.runningBalanceStr(botCfg.quoteID, quoteBalances.pending)
      const quoteBalanceTotal = quoteBalances.available + quoteBalances.locked + quoteBalances.pending
      rowTmpl.quoteBalance.textContent = this.runningBalanceStr(botCfg.quoteID, quoteBalanceTotal)
      rowTmpl.profitLoss.textContent = `$${Doc.formatFiatValue((botStatus.runStats as RunStats).profitLoss)}`

      if (botCfg.cexCfg) {
        const cexBaseBalances = botStatus.balances[botCfg.baseID].cex
        rowTmpl.runningCexBaseBalanceAvailable.textContent = this.runningBalanceStr(botCfg.baseID, cexBaseBalances.available)
        rowTmpl.runningCexBaseBalanceLocked.textContent = this.runningBalanceStr(botCfg.baseID, cexBaseBalances.locked)
        const cexBaseBalanceTotal = cexBaseBalances.available + cexBaseBalances.locked + cexBaseBalances.pending
        rowTmpl.cexBaseBalance.textContent = this.runningBalanceStr(botCfg.baseID, cexBaseBalanceTotal)
        const cexQuoteBalances = botStatus.balances[botCfg.quoteID].cex
        rowTmpl.runningCexQuoteBalanceAvailable.textContent = this.runningBalanceStr(botCfg.quoteID, cexQuoteBalances.available)
        rowTmpl.runningCexQuoteBalanceLocked.textContent = this.runningBalanceStr(botCfg.quoteID, cexQuoteBalances.locked)
        const cexQuoteBalanceTotal = cexQuoteBalances.available + cexQuoteBalances.locked + cexQuoteBalances.pending
        rowTmpl.cexQuoteBalance.textContent = this.runningBalanceStr(botCfg.quoteID, cexQuoteBalanceTotal)
      }
    } else {
      rowTmpl.baseBalance.textContent = this.walletBalanceStr(botCfg.baseID, botCfg.alloc.dex[botCfg.baseID])
      rowTmpl.quoteBalance.textContent = this.walletBalanceStr(botCfg.quoteID, botCfg.alloc.dex[botCfg.quoteID])

      if (botCfg.cexCfg) {
        const cachedBaseBalance = MM.cachedCexBalance(botCfg.cexCfg.name, botCfg.baseID)
        Doc.setVis(!cachedBaseBalance, rowTmpl.cexBaseBalanceSpinner)
        Doc.setVis(!!cachedBaseBalance, rowTmpl.cexBaseBalanceLoaded)
        if (cachedBaseBalance) {
          rowTmpl.cexBaseBalance.textContent = this.percentageBalanceStr(botCfg.baseID, cachedBaseBalance.available, botCfg.alloc.cex[botCfg.baseID])
        } else {
          MM.cexBalance(botCfg.cexCfg.name, botCfg.baseID).then((cexBalance) => {
            if (!botCfg.cexCfg) return // typescript check above doesn't apply due to async
            rowTmpl.cexBaseBalance.textContent = this.percentageBalanceStr(botCfg.baseID, cexBalance.available, botCfg.alloc.cex[botCfg.baseID])
            Doc.hide(rowTmpl.cexBaseBalanceSpinner)
            Doc.show(rowTmpl.cexBaseBalanceLoaded)
          })
        }

        const cachedQuoteBalance = MM.cachedCexBalance(botCfg.cexCfg.name, botCfg.quoteID)
        Doc.setVis(!cachedQuoteBalance, rowTmpl.cexQuoteBalanceSpinner)
        Doc.setVis(!!cachedQuoteBalance, rowTmpl.cexQuoteBalanceLoaded)
        if (cachedQuoteBalance) {
          rowTmpl.cexQuoteBalance.textContent = this.percentageBalanceStr(botCfg.quoteID, cachedQuoteBalance.available, botCfg.alloc.cex[botCfg.quoteID])
        } else {
          MM.cexBalance(botCfg.cexCfg.name, botCfg.quoteID).then((cexBalance) => {
            if (!botCfg.cexCfg) return // typescript check above doesn't apply due to async
            rowTmpl.cexQuoteBalance.textContent = this.percentageBalanceStr(botCfg.quoteID, cexBalance.available, botCfg.alloc.cex[botCfg.quoteID])
            Doc.hide(rowTmpl.cexQuoteBalanceSpinner)
            Doc.show(rowTmpl.cexQuoteBalanceLoaded)
          })
        }
      }
    }
  }

  botStatusForConfig (botCfg: BotConfig): MMBotStatus {
    return this.mmStatus.bots.find(({ config: cfg }) => cfg.host === botCfg.host && cfg.baseID === botCfg.baseID && cfg.quoteID === botCfg.quoteID) as MMBotStatus
  }

  /*
   * setupBotTable populates the table of market maker bots.
   */
  async setupBotTable (botConfigs: BotConfig[], mmStatus: MarketMakingStatus) {
    const page = this.page
    Doc.empty(page.botTableBody)
    const running = mmStatus.bots.some((botStatus: MMBotStatus) => botStatus.running)

    Doc.setVis(running, page.runningHeader, page.profitLossHeader, page.logsHeader)
    Doc.setVis(!running, page.enabledHeader, page.removeHeader)

    for (const botCfg of botConfigs) {
      const row = page.botTableRowTmpl.cloneNode(true) as PageElement
      row.id = marketStr(botCfg.host, botCfg.baseID, botCfg.quoteID)
      const rowTmpl = Doc.parseTemplate(row)
      const botStatus = this.botStatusForConfig(botCfg)
      this.setTableRowRunning(rowTmpl, botStatus.running)

      const setupHover = (hoverEl: PageElement, hoverContent: PageElement) => {
        Doc.bind(hoverEl, 'mouseover', () => { Doc.show(hoverContent) })
        Doc.bind(hoverEl, 'mouseout', () => { Doc.hide(hoverContent) })
      }
      setupHover(rowTmpl.baseBalanceDetails, rowTmpl.baseBalanceHoverContainer)
      setupHover(rowTmpl.quoteBalanceDetails, rowTmpl.quoteBalanceHoverContainer)
      setupHover(rowTmpl.cexBaseBalanceDetails, rowTmpl.cexBaseBalanceHoverContainer)
      setupHover(rowTmpl.cexQuoteBalanceDetails, rowTmpl.cexQuoteBalanceHoverContainer)

      const baseSymbol = app().assets[botCfg.baseID].symbol
      const quoteSymbol = app().assets[botCfg.quoteID].symbol
      const baseLogoPath = Doc.logoPath(baseSymbol)
      const quoteLogoPath = Doc.logoPath(quoteSymbol)

      rowTmpl.host.textContent = botCfg.host
      rowTmpl.baseMktLogo.src = baseLogoPath
      rowTmpl.quoteMktLogo.src = quoteLogoPath
      rowTmpl.baseSymbol.textContent = baseSymbol.toUpperCase()
      rowTmpl.quoteSymbol.textContent = quoteSymbol.toUpperCase()

      this.setTableRowBalances(rowTmpl, botCfg, botStatus)

      if (botCfg.arbMarketMakingConfig || botCfg.simpleArbConfig) {
        if (botCfg.arbMarketMakingConfig) rowTmpl.botType.textContent = intl.prep(intl.ID_BOTTYPE_ARB_MM)
        else rowTmpl.botType.textContent = intl.prep(intl.ID_BOTTYPE_SIMPLE_ARB)
        Doc.show(rowTmpl.cexLink)
        const dinfo = CEXDisplayInfos[botCfg.cexCfg?.name || '']
        rowTmpl.cexLogo.src = dinfo.logo
        rowTmpl.cexName.textContent = dinfo.name
      } else {
        rowTmpl.botType.textContent = intl.prep(intl.ID_BOTTYPE_BASIC_MM)
      }

      Doc.bind(rowTmpl.removeTd, 'click', async () => {
        await MM.removeBotConfig(botCfg)
        row.remove()
        const noBots = (await MM.status()).bots.length === 0
        Doc.setVis(noBots, page.noBots)
        Doc.setVis(!noBots, page.botTable, page.onOff)
      })
      Doc.bind(rowTmpl.settings, 'click', () => {
        let botType = botTypeBasicMM
        let cexName
        switch (true) {
          case Boolean(botCfg.arbMarketMakingConfig):
            botType = botTypeArbMM
            cexName = botCfg.cexCfg?.name as string
            break
          case Boolean(botCfg.simpleArbConfig):
            botType = botTypeBasicArb
            cexName = botCfg.cexCfg?.name as string
        }
        app().loadPage('mmsettings', { host: botCfg.host, baseID: botCfg.baseID, quoteID: botCfg.quoteID, botType, cexName })
      })
      Doc.bind(rowTmpl.logs, 'click', () => {
        this.loadRunLogsPage(botCfg.host, botCfg.baseID, botCfg.quoteID)
      })
      page.botTableBody.appendChild(row)
    }
  }

  async loadRunLogsPage (host: string, baseID: number, quoteID: number) {
    const status = await MM.status()
    const bot = status.bots.find((s: MMBotStatus) => s.config.host === host && s.config.baseID === baseID && s.config.quoteID === quoteID)
    if (!bot || !bot.runStats) {
      console.error('bot not running', host, baseID, quoteID)
      return
    }
    app().loadPage(`mmlogs?host=${host}&baseID=${baseID}&quoteID=${quoteID}&startTime=${bot.runStats?.startTime || 0}`)
  }

  async start () {
    if (State.passwordIsCached()) this.startBots()
    else this.forms.show(this.page.pwForm)
  }

  async startBots () {
    const page = this.page
    Doc.hide(page.mmErr)
    const appPW = page.pwInput.value || ''
    this.page.pwInput.value = ''
    this.forms.close()
    const res = await MM.start(appPW)
    if (!app().checkResponse(res)) {
      page.mmErr.textContent = res.msg
      Doc.show(page.mmErr)
    }
  }
}

/*
 * MarketMakerBot is the front end representation of the server's
 * mm.MarketMaker. MarketMakerBot is a singleton assigned to MM below.
 */
class MarketMakerBot {
  cexBalanceCache: Record<string, Record<number, ExchangeBalance>> = {}

  /*
   * updateBotConfig appends or updates the specified BotConfig.
   */
  async updateBotConfig (cfg: BotConfig) {
    return postJSON('/api/updatebotconfig', cfg)
  }

  /*
   * updateCEXConfig appends or updates the specified CEXConfig.
   */
  async updateCEXConfig (cfg: CEXConfig) {
    return postJSON('/api/updatecexconfig', cfg)
  }

  async removeBotConfig (cfg: BotConfig) {
    return postJSON('/api/removebotconfig', {
      host: cfg.host,
      baseAsset: cfg.baseID,
      quoteAsset: cfg.quoteID
    })
  }

  async report (host: string, baseID: number, quoteID: number) {
    return postJSON('/api/marketreport', { host, baseID, quoteID })
  }

  async start (appPW: string) {
    return await postJSON('/api/startallmmbots', { appPW })
  }

  async stop () : Promise<void> {
    await postJSON('/api/stopallmmbots')
  }

  async status () : Promise<MarketMakingStatus> {
    return (await getJSON('/api/marketmakingstatus')).status
  }

  // botStats returns the RunStats for a running bot with the specified parameters.
  async botStats (baseID: number, quoteID: number, host: string, startTime: number) : Promise<RunStats | undefined> {
    const allBotStatus = await this.status()
    for (let i = 0; i < allBotStatus.bots.length; i++) {
      const botStatus = allBotStatus.bots[i]
      if (!botStatus.runStats) continue
      const runStats = botStatus.runStats
      const cfg = botStatus.config
      if (cfg.baseID === baseID && cfg.quoteID === quoteID && cfg.host === host && runStats.startTime === startTime) {
        return runStats
      }
    }
  }

  cachedCexBalance (cexName: string, assetID: number): ExchangeBalance | undefined {
    return this.cexBalanceCache[cexName]?.[assetID]
  }

  async cexBalance (cexName: string, assetID: number): Promise<ExchangeBalance> {
    if (!this.cexBalanceCache[cexName]) this.cexBalanceCache[cexName] = {}
    const cexBalance = (await postJSON('/api/cexbalance', { cexName, assetID })).cexBalance
    this.cexBalanceCache[cexName][assetID] = cexBalance
    return cexBalance
  }

  // mmRunOverview returns the MarketMakingRunOverview for an archived bot run.
  async mmRunOverview (host: string, baseID: number, quoteID: number, startTime: number) : Promise<MarketMakingRunOverview> {
    return (await postJSON('/api/mmrunoverview', {
      market: { host, base: baseID, quote: quoteID },
      startTime
    })).overview
  }
}

// MM is the front end representation of the server's mm.MarketMaker.
export const MM = new MarketMakerBot()
