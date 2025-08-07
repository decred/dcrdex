import {
  app,
  PageElement,
  MarketWithHost
} from './registry'
import { getJSON } from './http'
import Doc from './doc'
import BasePage from './basepage'
import { setMarketElements } from './mmutil'

interface MarketMakingRun {
  startTime: number
  market: MarketWithHost
  profit: number
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

    const profitStr = (profit: number) : [string, string] => {
      const profitStr = profit.toFixed(2)
      if (profitStr === '-0.00' || profitStr === '0.00') {
        return ['$0.00', '']
      } else if (profit < 0) {
        return [`-$${profitStr.substring(1)}`, 'sellcolor']
      }
      return [`$${profitStr}`, 'buycolor']
    }

    let totalProfit = 0
    for (let i = 0; i < runs.length; i++) {
      const { startTime, market: { baseID, quoteID, host }, profit } = runs[i]
      const row = this.page.runTableRowTmpl.cloneNode(true) as HTMLElement
      const tmpl = Doc.parseTemplate(row)
      tmpl.startTime.textContent = new Date(startTime * 1000).toLocaleString()
      setMarketElements(row, baseID, quoteID, host)
      const [profitText, profitColor] = profitStr(profit)
      tmpl.profit.textContent = profitText
      if (profitColor) tmpl.profit.classList.add(profitColor)
      totalProfit += profit
      Doc.bind(tmpl.logs, 'click', () => {
        app().loadPage('mmlogs', { baseID, quoteID, host, startTime, returnPage: 'mmarchives' })
      })

      Doc.bind(tmpl.settings, 'click', () => {
        app().loadPage('mmsettings', { host, baseID, quoteID })
      })

      this.page.runTableBody.appendChild(row)
    }

    const [profitText, profitColor] = profitStr(totalProfit)
    this.page.totalProfit.textContent = profitText
    if (profitColor) this.page.totalProfit.classList.add(profitColor)
    this.page.numRuns.textContent = `${runs.length}`
  }
}
