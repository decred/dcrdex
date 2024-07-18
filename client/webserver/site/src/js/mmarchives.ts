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
      const { startTime, market: { baseID, quoteID, host } } = runs[i]
      const row = this.page.runTableRowTmpl.cloneNode(true) as HTMLElement
      const tmpl = Doc.parseTemplate(row)
      tmpl.startTime.textContent = new Date(startTime * 1000).toLocaleString()
      setMarketElements(row, baseID, quoteID, host)

      Doc.bind(tmpl.logs, 'click', () => {
        app().loadPage('mmlogs', { baseID, quoteID, host, startTime })
      })

      Doc.bind(tmpl.settings, 'click', () => {
        app().loadPage('mmsettings', { host, baseID, quoteID })
      })

      this.page.runTableBody.appendChild(row)
    }
  }
}
