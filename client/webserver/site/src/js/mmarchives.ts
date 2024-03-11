import {
  app,
  PageElement,
  MarketWithHost,
  BotConfig
} from './registry'
import { getJSON } from './http'
import Doc from './doc'
import BasePage from './basepage'
import * as intl from './locales'
import { CEXDisplayInfos, botTypeBasicMM, botTypeArbMM, botTypeBasicArb } from './mm'

interface MarketMakingRun {
  startTime: number
  market: MarketWithHost
  cfg: BotConfig
}

export default class MarketMakerArchivesPage extends BasePage {
  page: Record<string, PageElement>
  base: number
  quote: number
  host: string

  constructor (main: HTMLElement) {
    super()
    const page = this.page = Doc.idDescendants(main)
    Doc.cleanTemplates(page.runTableRowTmpl)
    Doc.bind(page.backButton, 'click', () => { app().loadPage('mm') })
    this.setup()
  }

  async setup () {
    const res = await getJSON('/api/archivedmmruns')
    if (!app().checkResponse(res)) {
      console.error('failed to get archived mm runs', res)
      // TODO: show error
      return
    }

    const runs : MarketMakingRun[] = res.runs

    for (let i = 0; i < runs.length; i++) {
      const run = runs[i]
      const row = this.page.runTableRowTmpl.cloneNode(true) as HTMLElement
      const tmpl = Doc.parseTemplate(row)
      tmpl.startTime.textContent = new Date(run.startTime * 1000).toLocaleString()
      tmpl.host.textContent = run.market.host
      const baseAsset = app().assets[run.market.base]
      const quoteAsset = app().assets[run.market.quote]
      const baseLogoPath = Doc.logoPath(baseAsset.symbol)
      const quoteLogoPath = Doc.logoPath(quoteAsset.symbol)
      tmpl.baseSymbol.textContent = baseAsset.symbol.toUpperCase()
      tmpl.quoteSymbol.textContent = quoteAsset.symbol.toUpperCase()
      tmpl.baseMktLogo.src = baseLogoPath
      tmpl.quoteMktLogo.src = quoteLogoPath

      if (run.cfg.arbMarketMakingConfig || run.cfg.simpleArbConfig) {
        if (run.cfg.arbMarketMakingConfig) tmpl.botType.textContent = intl.prep(intl.ID_BOTTYPE_ARB_MM)
        else tmpl.botType.textContent = intl.prep(intl.ID_BOTTYPE_SIMPLE_ARB)
        Doc.show(tmpl.cexLink)
        const dinfo = CEXDisplayInfos[run.cfg.cexCfg?.name || '']
        tmpl.cexLogo.src = '/img/' + dinfo.logo
        tmpl.cexName.textContent = dinfo.name
      } else {
        tmpl.botType.textContent = intl.prep(intl.ID_BOTTYPE_BASIC_MM)
      }

      Doc.bind(tmpl.logs, 'click', () => {
        app().loadPage(`mmlogs?host=${run.market.host}&baseID=${run.market.base}&quoteID=${run.market.quote}&startTime=${run.startTime}`)
      })

      Doc.bind(tmpl.settings, 'click', () => {
        let botType = botTypeBasicMM
        let cexName
        switch (true) {
          case Boolean(run.cfg.arbMarketMakingConfig):
            botType = botTypeArbMM
            cexName = run.cfg.cexCfg?.name as string
            break
          case Boolean(run.cfg.simpleArbConfig):
            botType = botTypeBasicArb
            cexName = run.cfg.cexCfg?.name as string
        }
        app().loadPage('mmsettings', { host: run.market.host, baseID: run.market.base, quoteID: run.market.quote, startTime: run.startTime, botType, cexName })
      })

      this.page.runTableBody.appendChild(row)
    }
  }
}
