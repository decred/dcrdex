import {
  app,
  PageElement,
  BotConfig,
  CEXConfig,
  MMBotStatus,
  RunStatsNote,
  MMStartStopNote,
  BalanceNote,
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
    logo: 'binance.com.png'
  },
  'BinanceUS': {
    name: 'Binance U.S.',
    logo: 'binance.us.png'
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

  constructor (main: HTMLElement) {
    super()

    const page = this.page = Doc.idDescendants(main)

    this.forms = new Forms(page.forms)

    Doc.bind(page.addBotBtnNoExisting, 'click', () => { this.newBot() })
    Doc.bind(page.addBotBtnWithExisting, 'click', () => { this.newBot() })
    Doc.bind(page.startBotsBtn, 'click', () => { this.start() })
    Doc.bind(page.stopBotsBtn, 'click', () => { MM.stop() })
    Doc.bind(page.archivedLogsBtn, 'click', () => { app().loadPage('mmarchives') })
    bindForm(page.pwForm, page.pwSubmit, () => this.startBots())

    setOptionTemplates(page)

    Doc.cleanTemplates(page.orderOptTmpl, page.booleanOptTmpl, page.rangeOptTmpl,
      page.botTableRowTmpl)

    this.setup()
  }

  async setup () {
    const page = this.page

    const status = await MM.status()
    const running = status.running

    const botConfigs = this.botConfigs = status.bots.map((s: MMBotStatus) => s.config)
    app().registerNoteFeeder({
      runstats: (note: RunStatsNote) => { this.handleRunStatsNote(note) },
      mmstartstop: (note: MMStartStopNote) => { this.handleMMStartStopNote(note) },
      balance: (note: BalanceNote) => { this.handleBalanceNote(note) }
    })

    const noBots = !botConfigs || botConfigs.length === 0
    Doc.setVis(noBots, page.noBots)
    Doc.setVis(!noBots, page.botTable, page.onOff)
    if (noBots) return

    page.onIndicator.classList.add(running ? 'on' : 'off')
    page.onIndicator.classList.remove(running ? 'off' : 'on')
    Doc.setVis(running, page.stopBotsBtn, page.onMsg)
    Doc.setVis(!running, page.startBotsBtn, page.offMsg)
    await this.setupBotTable(botConfigs, running, status.bots)
  }

  handleRunStatsNote (note: RunStatsNote) {
    const tableRows = this.page.botTableBody.children
    const rowID = marketStr(note.host, note.base, note.quote)
    for (let i = 0; i < tableRows.length; i++) {
      const row = tableRows[i] as PageElement
      if (row.id === rowID) {
        const rowTmpl = Doc.parseTemplate(row)
        this.setTableRowRunning(rowTmpl, true, !!note.stats)
        this.setTableRowBalances(rowTmpl, true, this.botConfigs[i], note.stats)
        return
      }
    }
  }

  handleMMStartStopNote (note: MMStartStopNote) {
    const page = this.page
    page.onIndicator.classList.add(note.running ? 'on' : 'off')
    page.onIndicator.classList.remove(note.running ? 'off' : 'on')
    Doc.setVis(note.running, page.stopBotsBtn, page.runningHeader, page.onMsg, page.profitLossHeader, page.logsHeader)
    Doc.setVis(!note.running, page.startBotsBtn, page.addBotBtnNoExisting, page.enabledHeader,
      page.removeHeader, page.offMsg)
    const tableRows = page.botTableBody.children
    for (let i = 0; i < tableRows.length; i++) {
      const row = tableRows[i] as PageElement
      const rowTmpl = Doc.parseTemplate(row)
      this.setTableRowRunning(rowTmpl, note.running, undefined)
      this.setTableRowBalances(rowTmpl, note.running, this.botConfigs[i], undefined)
    }
  }

  async handleBalanceNote (note: BalanceNote) {
    if ((await MM.status()).running) return

    const getBotConfig = (mktID: string): BotConfig | undefined => {
      for (const cfg of this.botConfigs) {
        if (marketStr(cfg.host, cfg.baseID, cfg.quoteID) === mktID) return cfg
      }
    }
    const tableRows = this.page.botTableBody.children
    for (let i = 0; i < tableRows.length; i++) {
      const row = tableRows[i] as PageElement
      const rowTmpl = Doc.parseTemplate(row)
      const cfg = getBotConfig(row.id)
      if (!cfg) continue
      if (cfg.baseID === note.assetID) {
        rowTmpl.baseBalance.textContent = this.walletBalanceStr(note.assetID, cfg.baseBalance)
      } else if (cfg.quoteID === note.assetID) {
        rowTmpl.quoteBalance.textContent = this.walletBalanceStr(note.assetID, cfg.quoteBalance)
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
    return `${percentage}% - ${assetValue} ${asset.symbol.toUpperCase()}`
  }

  /*
   * walletBalanceStr returns a string like "50% - 0.0001 BTC" representing
   * the percentage of a wallet's balance selected in the market maker setting,
   * and the amount of that asset in the wallet.
   */
  walletBalanceStr (assetID: number, percentage: number): string {
    const asset = app().assets[assetID]
    const wallet = asset.wallet
    const balance = wallet.balance.available
    return this.percentageBalanceStr(assetID, balance, percentage)
  }

  runningBalanceStr (assetID: number, amount: number): string {
    const asset = app().assets[assetID]
    const unitInfo = asset.unitInfo
    const assetValue = Doc.formatCoinValue(amount, unitInfo)
    return `${assetValue} ${asset.symbol.toUpperCase()}`
  }

  /*
   * setTableRowRunning sets the visibility of elements on a market row depending
   * on whether the entire market maker is running, and specifically whether the
   * the current bot is running.
   */
  setTableRowRunning (rowTmpl: Record<string, PageElement>, mmRunning: boolean | undefined, thisBotRunning: boolean | undefined) {
    if (mmRunning !== undefined) {
      Doc.setVis(mmRunning, rowTmpl.running, rowTmpl.logs)
      Doc.setVis(!mmRunning, rowTmpl.enabled, rowTmpl.removeTd)
    }
    if (thisBotRunning !== undefined) {
      Doc.setVis(thisBotRunning, rowTmpl.runningIcon, rowTmpl.logs)
      Doc.setVis(!thisBotRunning, rowTmpl.notRunningIcon)
    }
  }

  setTableRowBalances (rowTmpl: Record<string, PageElement>, mmRunning: boolean | undefined, botCfg: BotConfig, stats?: RunStats) {
    if (mmRunning && !stats) {
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
      const cexLogoSrc = '/img/' + dinfo.logo
      rowTmpl.baseBalanceCexLogo.src = cexLogoSrc
      rowTmpl.quoteBalanceCexLogo.src = cexLogoSrc
      rowTmpl.cexBaseBalanceLogo.src = baseLogoPath
      rowTmpl.cexQuoteBalanceLogo.src = quoteLogoPath
    }

    Doc.setVis(!!botCfg.cexCfg, rowTmpl.cexBaseBalanceContainer, rowTmpl.cexQuoteBalanceContainer)
    Doc.setVis(!!stats, rowTmpl.baseBalanceDetails, rowTmpl.quoteBalanceDetails, rowTmpl.cexBaseBalanceDetails, rowTmpl.cexQuoteBalanceDetails, rowTmpl.profitLossTd)

    if (stats) {
      const baseBalances = stats.dexBalances[botCfg.baseID]
      rowTmpl.runningBaseBalanceAvailable.textContent = this.runningBalanceStr(botCfg.baseID, baseBalances.available)
      rowTmpl.runningBaseBalanceLocked.textContent = this.runningBalanceStr(botCfg.baseID, baseBalances.locked)
      rowTmpl.runningBaseBalancePending.textContent = this.runningBalanceStr(botCfg.baseID, baseBalances.pending)
      const baseBalanceTotal = baseBalances.available + baseBalances.locked + baseBalances.pending
      rowTmpl.baseBalance.textContent = this.runningBalanceStr(botCfg.baseID, baseBalanceTotal)
      const quoteBalances = stats.dexBalances[botCfg.quoteID]
      rowTmpl.runningQuoteBalanceAvailable.textContent = this.runningBalanceStr(botCfg.quoteID, quoteBalances.available)
      rowTmpl.runningQuoteBalanceLocked.textContent = this.runningBalanceStr(botCfg.quoteID, quoteBalances.locked)
      rowTmpl.runningQuoteBalancePending.textContent = this.runningBalanceStr(botCfg.quoteID, quoteBalances.pending)
      const quoteBalanceTotal = quoteBalances.available + quoteBalances.locked + quoteBalances.pending
      rowTmpl.quoteBalance.textContent = this.runningBalanceStr(botCfg.quoteID, quoteBalanceTotal)
      rowTmpl.profitLoss.textContent = `$${Doc.formatFiatValue(stats.profitLoss)}`

      if (botCfg.cexCfg) {
        const cexBaseBalances = stats.cexBalances[botCfg.baseID]
        rowTmpl.runningCexBaseBalanceAvailable.textContent = this.runningBalanceStr(botCfg.baseID, cexBaseBalances.available)
        rowTmpl.runningCexBaseBalanceLocked.textContent = this.runningBalanceStr(botCfg.baseID, cexBaseBalances.locked)
        const cexBaseBalanceTotal = cexBaseBalances.available + cexBaseBalances.locked + cexBaseBalances.pending
        rowTmpl.cexBaseBalance.textContent = this.runningBalanceStr(botCfg.baseID, cexBaseBalanceTotal)
        const cexQuoteBalances = stats.cexBalances[botCfg.quoteID]
        rowTmpl.runningCexQuoteBalanceAvailable.textContent = this.runningBalanceStr(botCfg.quoteID, cexQuoteBalances.available)
        rowTmpl.runningCexQuoteBalanceLocked.textContent = this.runningBalanceStr(botCfg.quoteID, cexQuoteBalances.locked)
        const cexQuoteBalanceTotal = cexQuoteBalances.available + cexQuoteBalances.locked + cexQuoteBalances.pending
        rowTmpl.cexQuoteBalance.textContent = this.runningBalanceStr(botCfg.quoteID, cexQuoteBalanceTotal)
      }
    } else {
      rowTmpl.baseBalance.textContent = this.walletBalanceStr(botCfg.baseID, botCfg.baseBalance)
      rowTmpl.quoteBalance.textContent = this.walletBalanceStr(botCfg.quoteID, botCfg.quoteBalance)

      if (botCfg.cexCfg) {
        const cachedBaseBalance = MM.cachedCexBalance(botCfg.cexCfg.name, botCfg.baseID)
        Doc.setVis(!cachedBaseBalance, rowTmpl.cexBaseBalanceSpinner)
        Doc.setVis(!!cachedBaseBalance, rowTmpl.cexBaseBalanceLoaded)
        if (cachedBaseBalance) {
          rowTmpl.cexBaseBalance.textContent = this.percentageBalanceStr(botCfg.baseID, cachedBaseBalance.available, botCfg.cexCfg.baseBalance)
        } else {
          MM.cexBalance(botCfg.cexCfg.name, botCfg.baseID).then((cexBalance) => {
            if (!botCfg.cexCfg) return // typescript check above doesn't apply due to async
            rowTmpl.cexBaseBalance.textContent = this.percentageBalanceStr(botCfg.baseID, cexBalance.available, botCfg.cexCfg.baseBalance)
            Doc.hide(rowTmpl.cexBaseBalanceSpinner)
            Doc.show(rowTmpl.cexBaseBalanceLoaded)
          })
        }

        const cachedQuoteBalance = MM.cachedCexBalance(botCfg.cexCfg.name, botCfg.quoteID)
        Doc.setVis(!cachedQuoteBalance, rowTmpl.cexQuoteBalanceSpinner)
        Doc.setVis(!!cachedQuoteBalance, rowTmpl.cexQuoteBalanceLoaded)
        if (cachedQuoteBalance) {
          rowTmpl.cexQuoteBalance.textContent = this.percentageBalanceStr(botCfg.quoteID, cachedQuoteBalance.available, botCfg.cexCfg.quoteBalance)
        } else {
          MM.cexBalance(botCfg.cexCfg.name, botCfg.quoteID).then((cexBalance) => {
            if (!botCfg.cexCfg) return // typescript check above doesn't apply due to async
            rowTmpl.cexQuoteBalance.textContent = this.percentageBalanceStr(botCfg.quoteID, cexBalance.available, botCfg.cexCfg.quoteBalance)
            Doc.hide(rowTmpl.cexQuoteBalanceSpinner)
            Doc.show(rowTmpl.cexQuoteBalanceLoaded)
          })
        }
      }
    }
  }

  /*
   * setupBotTable populates the table of market maker bots.
   */
  async setupBotTable (botConfigs: BotConfig[], running: boolean, bots: MMBotStatus[]) {
    const page = this.page
    Doc.empty(page.botTableBody)

    Doc.setVis(running, page.runningHeader, page.profitLossHeader, page.logsHeader)
    Doc.setVis(!running, page.enabledHeader, page.removeHeader)

    for (const botCfg of botConfigs) {
      const row = page.botTableRowTmpl.cloneNode(true) as PageElement
      row.id = marketStr(botCfg.host, botCfg.baseID, botCfg.quoteID)
      const rowTmpl = Doc.parseTemplate(row)
      const thisBot = bots.find(({ config: cfg }) => cfg.host === botCfg.host && cfg.baseID === botCfg.baseID && cfg.quoteID === botCfg.quoteID)
      const thisBotRunning = thisBot && !!thisBot.runStats
      this.setTableRowRunning(rowTmpl, running, thisBotRunning)

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

      rowTmpl.enabledCheckbox.checked = !botCfg.disabled
      Doc.bind(rowTmpl.enabledCheckbox, 'click', async () => {
        MM.setEnabled(botCfg.host, botCfg.baseID, botCfg.quoteID, !!rowTmpl.enabledCheckbox.checked)
      })

      rowTmpl.host.textContent = botCfg.host
      rowTmpl.baseMktLogo.src = baseLogoPath
      rowTmpl.quoteMktLogo.src = quoteLogoPath
      rowTmpl.baseSymbol.textContent = baseSymbol.toUpperCase()
      rowTmpl.quoteSymbol.textContent = quoteSymbol.toUpperCase()

      this.setTableRowBalances(rowTmpl, running, botCfg, thisBot?.runStats)

      if (botCfg.arbMarketMakingConfig || botCfg.simpleArbConfig) {
        if (botCfg.arbMarketMakingConfig) rowTmpl.botType.textContent = intl.prep(intl.ID_BOTTYPE_ARB_MM)
        else rowTmpl.botType.textContent = intl.prep(intl.ID_BOTTYPE_SIMPLE_ARB)
        Doc.show(rowTmpl.cexLink)
        const dinfo = CEXDisplayInfos[botCfg.cexCfg?.name || '']
        rowTmpl.cexLogo.src = '/img/' + dinfo.logo
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

  async report (baseID: number, quoteID: number) {
    return postJSON('/api/marketreport', { baseID, quoteID })
  }

  async setEnabled (host: string, baseID: number, quoteID: number, enabled: boolean) : Promise<void> {
    const bots = (await this.status()).bots
    const botStatus = bots.filter((s: MMBotStatus) => s.config.host === host && s.config.baseID === baseID && s.config.quoteID === quoteID)[0]
    botStatus.config.disabled = !enabled
    await this.updateBotConfig(botStatus.config)
  }

  async start (appPW: string) {
    return await postJSON('/api/startmarketmaking', { appPW })
  }

  async stop () : Promise<void> {
    await postJSON('/api/stopmarketmaking')
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
