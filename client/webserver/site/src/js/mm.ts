import {
  app,
  PageElement,
  BotConfig,
  CEXConfig,
  MarketWithHost,
  BotStartStopNote,
  MMStartStopNote,
  BalanceNote,
  MarketMakingConfig,
  MarketMakingStatus,
  CEXMarket,
  ExchangeBalance
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
    const marketMakingCfg = await MM.config()

    const botConfigs = this.botConfigs = marketMakingCfg.botConfigs || []
    app().registerNoteFeeder({
      botstartstop: (note: BotStartStopNote) => { this.handleBotStartStopNote(note) },
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
    this.setupBotTable(botConfigs, running, status.runningBots)
  }

  handleBotStartStopNote (note: BotStartStopNote) {
    const tableRows = this.page.botTableBody.children
    const rowID = marketStr(note.host, note.base, note.quote)
    for (let i = 0; i < tableRows.length; i++) {
      const row = tableRows[i] as PageElement
      if (row.id === rowID) {
        const rowTmpl = Doc.parseTemplate(row)
        this.setTableRowRunning(rowTmpl, undefined, note.running)
        return
      }
    }
  }

  handleMMStartStopNote (note: MMStartStopNote) {
    const page = this.page
    page.onIndicator.classList.add(note.running ? 'on' : 'off')
    page.onIndicator.classList.remove(note.running ? 'off' : 'on')
    Doc.setVis(note.running, page.stopBotsBtn, page.runningHeader, page.onMsg)
    Doc.setVis(!note.running, page.startBotsBtn, page.addBotBtnNoExisting, page.enabledHeader,
      page.baseBalanceHeader, page.quoteBalanceHeader, page.removeHeader, page.offMsg)
    const tableRows = page.botTableBody.children
    for (let i = 0; i < tableRows.length; i++) {
      const row = tableRows[i] as PageElement
      const rowTmpl = Doc.parseTemplate(row)
      this.setTableRowRunning(rowTmpl, note.running, undefined)
    }
  }

  handleBalanceNote (note: BalanceNote) {
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

  /*
   * walletBalanceStr returns a string like "50% - 0.0001 BTC" representing
   * the percentage of a wallet's balance selected in the market maker setting,
   * and the amount of that asset in the wallet.
   */
  walletBalanceStr (assetID: number, percentage: number): string {
    const asset = app().assets[assetID]
    const wallet = asset.wallet
    const balance = wallet.balance.available
    const unitInfo = asset.unitInfo
    const assetValue = Doc.formatCoinValue((balance * percentage) / 100, unitInfo)
    return `${percentage}% - ${assetValue} ${asset.symbol.toUpperCase()}`
  }

  /*
   * setTableRowRunning sets the visibility of elements on a market row depending
   * on whether the entire market maker is running, and specifically whether the
   * the current bot is running.
   */
  setTableRowRunning (rowTmpl: Record<string, PageElement>, mmRunning: boolean | undefined, thisBotRunning: boolean | undefined) {
    if (mmRunning !== undefined) {
      Doc.setVis(mmRunning, rowTmpl.running)
      Doc.setVis(!mmRunning, rowTmpl.enabled, rowTmpl.baseBalanceTd, rowTmpl.quoteBalanceTd, rowTmpl.removeTd)
    }
    if (thisBotRunning !== undefined) {
      Doc.setVis(thisBotRunning, rowTmpl.runningIcon)
      Doc.setVis(!thisBotRunning, rowTmpl.notRunningIcon)
    }
  }

  /*
   * setupBotTable populates the table of market maker bots.
   */
  setupBotTable (botConfigs: BotConfig[], running: boolean, runningBots: MarketWithHost[]) {
    const page = this.page
    Doc.empty(page.botTableBody)

    Doc.setVis(running, page.runningHeader)
    Doc.setVis(!running, page.enabledHeader, page.baseBalanceHeader, page.quoteBalanceHeader, page.removeHeader)

    for (const botCfg of botConfigs) {
      const row = page.botTableRowTmpl.cloneNode(true) as PageElement
      row.id = marketStr(botCfg.host, botCfg.baseID, botCfg.quoteID)
      const rowTmpl = Doc.parseTemplate(row)
      const thisBotRunning = runningBots.some((bot) => {
        return bot.host === botCfg.host &&
          bot.base === botCfg.baseID &&
          bot.quote === botCfg.quoteID
      })
      this.setTableRowRunning(rowTmpl, running, thisBotRunning)

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
      rowTmpl.baseBalance.textContent = this.walletBalanceStr(botCfg.baseID, botCfg.baseBalance)
      rowTmpl.quoteBalance.textContent = this.walletBalanceStr(botCfg.quoteID, botCfg.quoteBalance)
      rowTmpl.baseBalanceLogo.src = baseLogoPath
      rowTmpl.quoteBalanceLogo.src = quoteLogoPath
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
        await MM.removeMarketMakingConfig(botCfg)
        row.remove()
        const mmCfg = await MM.config()
        const noBots = !mmCfg || !mmCfg.botConfigs || mmCfg.botConfigs.length === 0
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
      page.botTableBody.appendChild(row)
    }
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
 * MarketMakerBot is the front end representaion of the server's mm.MarketMaker.
 * MarketMakerBot is a singleton assigned to MM below.
 */
class MarketMakerBot {
  cfg: MarketMakingConfig
  st: MarketMakingStatus | undefined
  mkts: Record<string, CEXMarket[]>

  constructor () {
    this.mkts = {}
  }

  handleStartStopNote (n: MMStartStopNote) {
    if (!this.st) return
    this.st.running = n.running
  }

  /*
   * config returns the curret MarketMakingConfig. config will fetch the
   * MarketMakingConfig once. Future updates occur in updateCEXConfig and
   * updateBotConfig. after a page handler calls config during intitialization,
   * the the config can be accessed directly thr MM.cfg to avoid the async
   * function.
   */
  async config () : Promise<MarketMakingConfig> {
    if (this.cfg) return this.cfg
    const res = await getJSON('/api/marketmakingconfig')
    if (!app().checkResponse(res)) {
      throw new Error('failed to fetch market making config')
    }
    this.cfg = res.cfg
    this.mkts = res.mkts
    return this.cfg
  }

  /*
   * updateBotConfig appends or updates the specified BotConfig, then updates
   * the cached MarketMakingConfig,
   */
  async updateBotConfig (cfg: BotConfig) : Promise<void> {
    const res = await postJSON('/api/updatebotconfig', cfg)
    if (!app().checkResponse(res)) {
      throw res.msg || Error(res)
    }
    this.cfg = res.cfg
  }

  /*
   * updateCEXConfig appends or updates the specified CEXConfig, then updates
   * the cached MarketMakingConfig,
   */
  async updateCEXConfig (cfg: CEXConfig) : Promise<void> {
    const res = await postJSON('/api/updatecexconfig', cfg)
    if (res.err) {
      throw new Error(res.err)
    }

    this.cfg = res.cfg
    this.mkts[cfg.name] = res.mkts || []
  }

  async removeMarketMakingConfig (cfg: BotConfig) : Promise<void> {
    const res = await postJSON('/api/removemarketmakingconfig', {
      host: cfg.host,
      baseAsset: cfg.baseID,
      quoteAsset: cfg.quoteID
    })
    if (res.err) {
      throw new Error(res.err)
    }
    this.cfg = res.cfg
  }

  async report (baseID: number, quoteID: number) {
    return postJSON('/api/marketreport', { baseID, quoteID })
  }

  async setEnabled (host: string, baseID: number, quoteID: number, enabled: boolean) : Promise<void> {
    const botCfgs = this.cfg.botConfigs || []
    const mktCfg = botCfgs.find((cfg : BotConfig) => {
      return cfg.host === host && cfg.baseID === baseID && cfg.quoteID === quoteID
    })
    if (!mktCfg) {
      throw new Error('market making config not found')
    }
    mktCfg.disabled = !enabled
    await this.updateBotConfig(mktCfg)
  }

  async start (appPW: string) {
    return await postJSON('/api/startmarketmaking', { appPW })
  }

  async stop () : Promise<void> {
    await postJSON('/api/stopmarketmaking')
  }

  async status () : Promise<MarketMakingStatus> {
    if (this.st !== undefined) return this.st
    const res = await getJSON('/api/marketmakingstatus')
    if (!app().checkResponse(res)) {
      throw new Error(`failed to fetch market making status: ${res.msg ?? String(res)}`)
    }
    const status = {} as MarketMakingStatus
    status.running = !!res.running
    status.runningBots = res.runningBots
    this.st = status
    return status
  }

  cexConfigured (cexName: string) {
    return (this.cfg.cexConfigs || []).some((cfg: CEXConfig) => cfg.name === cexName)
  }

  async cexBalance (cexName: string, assetID: number): Promise<ExchangeBalance> {
    const res = await postJSON('/api/cexbalance', { cexName, assetID })
    if (!app().checkResponse(res)) {
      throw new Error(`failed to fetch cexBalance status: ${res.msg ?? String(res)}`)
    }
    return res.cexBalance
  }
}

// MM is the front end representation of the server's mm.MarketMaker.
export const MM = new MarketMakerBot()
