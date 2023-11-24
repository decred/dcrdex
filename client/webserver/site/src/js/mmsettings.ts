import {
  PageElement,
  BotConfig,
  XYRange,
  OrderPlacement,
  BalanceType,
  app,
  MarketReport,
  OrderOption,
  BasicMarketMakingCfg,
  ArbMarketMakingCfg,
  ArbMarketMakingPlacement
} from './registry'
import Doc from './doc'
import BasePage from './basepage'
import { setOptionTemplates, XYRangeHandler } from './opts'
import { MM, CEXDisplayInfos } from './mm'
import { Forms } from './forms'
import * as intl from './locales'

const GapStrategyMultiplier = 'multiplier'
const GapStrategyAbsolute = 'absolute'
const GapStrategyAbsolutePlus = 'absolute-plus'
const GapStrategyPercent = 'percent'
const GapStrategyPercentPlus = 'percent-plus'
const arbMMRowCacheKey = 'arbmm'

const driftToleranceRange: XYRange = {
  start: {
    label: '0%',
    x: 0,
    y: 0
  },
  end: {
    label: '1%',
    x: 0.01,
    y: 1
  },
  xUnit: '',
  yUnit: '%'
}

const orderPersistenceRange: XYRange = {
  start: {
    label: '0',
    x: 0,
    y: 0
  },
  end: {
    label: 'max',
    x: 21,
    y: 21
  },
  xUnit: '',
  yUnit: 'epochs'
}

const oracleBiasRange: XYRange = {
  start: {
    label: '-1%',
    x: -0.01,
    y: -1
  },
  end: {
    label: '1%',
    x: 0.01,
    y: 1
  },
  xUnit: '',
  yUnit: '%'
}

const oracleWeightRange: XYRange = {
  start: {
    label: '0%',
    x: 0,
    y: 0
  },
  end: {
    label: '100%',
    x: 1,
    y: 100
  },
  xUnit: '',
  yUnit: '%'
}

const defaultMarketMakingConfig : BasicMarketMakingCfg = {
  gapStrategy: GapStrategyPercentPlus,
  sellPlacements: [],
  buyPlacements: [],
  driftTolerance: 0.001,
  oracleWeighting: 0.1,
  oracleBias: 0,
  emptyMarketRate: 0
}

const defaultArbMarketMakingConfig : any /* so I don't have to define all fields */ = {
  cexName: '',
  profit: 3,
  orderPersistence: 20
}

// walletSettingControl is used by the modified highlighting and
// reset values functionalities to manage the wallet settings
// defined in walletDefinition.multifundingopts
interface walletSettingControl {
  toHighlight: PageElement
  setValue: (value: string) => void
}

// cexButton stores parts of a CEX selection button.
interface cexButton {
  name: string
  div: PageElement
  tmpl: Record<string, PageElement>
}

/*
 * ConfigState is an amalgamation of BotConfig, ArbMarketMakingCfg, and
 * BasicMarketMakingCfg. ConfigState tracks the global state of the options
 * presented on the page, with a single field for each option / control element.
 * ConfigState is necessary because there are duplicate fields in the various
 * config structs, and the placement types are not identical.
 */
interface ConfigState {
  gapStrategy: string
  cexName: string
  useOracles: boolean
  profit: number
  useEmptyMarketRate: boolean
  emptyMarketRate: number
  driftTolerance: number
  orderPersistence: number // epochs
  oracleWeighting: number
  oracleBias: number
  baseBalanceType: BalanceType
  baseBalance: number
  quoteBalanceType: BalanceType
  quoteBalance: number
  disabled: boolean
  buyPlacements: OrderPlacement[]
  sellPlacements: OrderPlacement[]
  baseOptions: Record<string, any>
  quoteOptions: Record<string, any>
}

/*
 * RangeOption adds some functionality to XYRangeHandler. It will keep the
 * ConfigState up to date, recognizes a modified state, and adds default
 * callbacks for the XYRangeHandler. RangeOption is similar in function to an
 * XYRangeOption. The two types may be able to be merged.
 */
class RangeOption {
  // Args
  cfg: XYRange
  initVal: number
  lastVal: number
  settingsDict: {[key: string]: any}
  settingsKey: string
  // Set in constructor
  div: PageElement
  xyRange: XYRangeHandler
  // Set with setters
  update: (x: number, y: number) => any
  changed: () => void
  selected: () => void
  convert: (x: any) => any

  constructor (cfg: XYRange, initVal: number, roundX: boolean, roundY: boolean, disabled: boolean, settingsDict: {[key:string]: any}, settingsKey: string) {
    this.cfg = cfg
    this.initVal = initVal
    this.lastVal = initVal
    this.settingsDict = settingsDict
    this.settingsKey = settingsKey
    this.convert = (x: any) => x

    this.xyRange = new XYRangeHandler(
      cfg,
      initVal,
      (x: number, y: number) => { this.handleUpdate(x, y) },
      () => { this.handleChange() },
      () => { this.handleSelected() },
      roundX,
      roundY,
      disabled
    )
    this.div = this.xyRange.control
  }

  setUpdate (f: (x: number, y: number) => number) {
    this.update = f
  }

  setChanged (f: () => void) {
    this.changed = f
  }

  setSelected (f: () => void) {
    this.selected = f
  }

  stringify () {
    this.convert = (x: any) => String(x)
  }

  handleUpdate (x:number, y:number) {
    this.lastVal = this.update ? this.update(x, y) : x
    this.settingsDict[this.settingsKey] = this.convert(this.lastVal)
  }

  handleChange () {
    if (this.changed) this.changed()
  }

  handleSelected () {
    if (this.selected) this.selected()
  }

  modified (): boolean {
    return this.lastVal !== this.initVal
  }

  setValue (x: number) {
    this.xyRange.setValue(x, false)
  }

  reset () {
    this.xyRange.setValue(this.initVal, true)
  }
}

export default class MarketMakerSettingsPage extends BasePage {
  page: Record<string, PageElement>
  forms: Forms
  currentMarket: string
  originalConfig: ConfigState
  updatedConfig: ConfigState
  creatingNewBot: boolean
  host: string
  baseID: number
  quoteID: number
  oracleBias: RangeOption
  oracleWeighting: RangeOption
  driftTolerance: RangeOption
  orderPersistence: RangeOption
  baseBalance?: RangeOption
  quoteBalance?: RangeOption
  baseWalletSettingControl: Record<string, walletSettingControl> = {}
  quoteWalletSettingControl: Record<string, walletSettingControl> = {}
  cexes: Record<string, cexButton>
  placementsCache: Record<string, [OrderPlacement[], OrderPlacement[]]>

  constructor (main: HTMLElement) {
    super()

    const page = this.page = Doc.idDescendants(main)

    this.forms = new Forms(page.forms)
    this.placementsCache = {}

    app().headerSpace.appendChild(page.mmTitle)

    setOptionTemplates(page)
    Doc.cleanTemplates(
      page.orderOptTmpl, page.booleanOptTmpl, page.rangeOptTmpl, page.placementRowTmpl,
      page.oracleTmpl, page.boolSettingTmpl, page.rangeSettingTmpl, page.cexOptTmpl
    )

    Doc.bind(page.resetButton, 'click', () => { this.setOriginalValues(false) })
    Doc.bind(page.updateButton, 'click', () => { this.saveSettings() })
    Doc.bind(page.createButton, 'click', async () => { this.saveSettings() })
    Doc.bind(page.backButton, 'click', () => { app().loadPage('mm') })
    Doc.bind(page.cexSubmit, 'click', () => { this.handleCEXSubmit() })

    const urlParams = new URLSearchParams(window.location.search)
    const host = urlParams.get('host')
    const base = urlParams.get('base')
    const quote = urlParams.get('quote')
    if (!host || !base || !quote) {
      console.log("Missing 'host', 'base', or 'quote' URL parameter")
      return
    }
    this.setup(host, parseInt(base), parseInt(quote))
  }

  unload () {
    this.forms.exit()
  }

  defaultWalletOptions (assetID: number) : Record<string, string> {
    const walletDef = app().currentWalletDefinition(assetID)
    if (!walletDef.multifundingopts) {
      return {}
    }
    const options: Record<string, string> = {}
    for (const opt of walletDef.multifundingopts) {
      if (opt.quoteAssetOnly && assetID !== this.quoteID) {
        continue
      }
      options[opt.key] = `${opt.default}`
    }
    return options
  }

  async setup (host: string, baseID: number, quoteID: number) {
    this.baseID = baseID
    this.quoteID = quoteID
    this.host = host

    await MM.config()
    const status = await MM.status()
    const botCfg = this.currentBotConfig()
    const dmm = defaultMarketMakingConfig

    const oldCfg = this.originalConfig = Object.assign({}, defaultMarketMakingConfig, defaultArbMarketMakingConfig, {
      useOracles: dmm.oracleWeighting > 0,
      useEmptyMarketRate: dmm.emptyMarketRate > 0,
      baseBalanceType: BalanceType.Percentage,
      quoteBalanceType: BalanceType.Percentage,
      baseBalance: 0,
      quoteBalance: 0,
      disabled: status.running,
      baseOptions: this.defaultWalletOptions(baseID),
      quoteOptions: this.defaultWalletOptions(quoteID),
      buyPlacements: [],
      sellPlacements: []
    })

    const { page } = this

    Doc.hide(page.updateButton, page.resetButton, page.createButton)

    page.baseHeader.textContent = app().assets[this.baseID].symbol.toUpperCase()
    page.quoteHeader.textContent = app().assets[this.quoteID].symbol.toUpperCase()
    page.hostHeader.textContent = host
    page.baseBalanceLogo.src = Doc.logoPathFromID(this.baseID)
    page.quoteBalanceLogo.src = Doc.logoPathFromID(this.quoteID)
    page.baseSettingsLogo.src = Doc.logoPathFromID(this.baseID)
    page.quoteSettingsLogo.src = Doc.logoPathFromID(this.quoteID)
    page.baseLogo.src = Doc.logoPathFromID(this.baseID)
    page.quoteLogo.src = Doc.logoPathFromID(this.quoteID)

    if (botCfg) {
      const { basicMarketMakingConfig: mmCfg, arbMarketMakingConfig: arbCfg } = botCfg
      this.creatingNewBot = false
      // This is kinda sloppy, but we'll copy any relevant issues from the
      // old config into the originalConfig.
      const idx = oldCfg as {[k: string]: any} // typescript
      for (const [k, v] of Object.entries(botCfg)) if (idx[k] !== undefined) idx[k] = v
      if (mmCfg) {
        oldCfg.buyPlacements = mmCfg.buyPlacements
        oldCfg.sellPlacements = mmCfg.sellPlacements
      } else if (arbCfg) {
        const { buyPlacements, sellPlacements, cexName } = arbCfg
        oldCfg.cexName = cexName
        oldCfg.buyPlacements = Array.from(buyPlacements, (p: ArbMarketMakingPlacement) => { return { lots: p.lots, gapFactor: p.multiplier } })
        oldCfg.sellPlacements = Array.from(sellPlacements, (p: ArbMarketMakingPlacement) => { return { lots: p.lots, gapFactor: p.multiplier } })
      }
      Doc.setVis(!status.running, page.updateButton, page.resetButton)
    } else {
      this.creatingNewBot = true
      Doc.setVis(!status.running, page.createButton)
    }

    Doc.setVis(!status.running, page.cexPrompt, page.profitPrompt)
    Doc.setVis(status.running, page.viewOnly)
    page.profitInput.disabled = status.running

    // Now that we've updated the originalConfig, we'll copy it.
    this.updatedConfig = JSON.parse(JSON.stringify(oldCfg))

    this.setupCEXes()
    if (oldCfg.cexName) this.selectCEX(oldCfg.cexName)

    this.setupMMConfigSettings(status.running)
    this.setupBalanceSelectors(status.running)
    this.setupWalletSettings(status.running)
    this.setOriginalValues(status.running)
    Doc.show(page.botSettingsContainer)
    this.fetchOracles()
  }

  currentBotConfig (): BotConfig | null {
    const { baseID, quoteID, host } = this
    const cfgs = (MM.cfg.botConfigs || []).filter((cfg: BotConfig) => cfg.baseID === baseID && cfg.quoteID === quoteID && cfg.host === host)
    if (cfgs.length) return cfgs[0]
    return null
  }

  /*
   * updateModifiedMarkers checks each of the input elements on the page and
   * if the current value does not match the original value (since the last
   * save), then the input will have a colored border.
   */
  updateModifiedMarkers () {
    if (this.creatingNewBot) return
    const { page, originalConfig: oldCfg, updatedConfig: newCfg } = this

    // Gap strategy input
    const gapStrategyModified = oldCfg.gapStrategy !== newCfg.gapStrategy
    page.gapStrategySelect.classList.toggle('modified', gapStrategyModified)

    const profitModified = oldCfg.profit !== newCfg.profit
    page.profitInput.classList.toggle('modified', profitModified)

    // Buy placements Input
    let buyPlacementsModified = false
    if (oldCfg.buyPlacements.length !== newCfg.buyPlacements.length) {
      buyPlacementsModified = true
    } else {
      for (let i = 0; i < oldCfg.buyPlacements.length; i++) {
        if (oldCfg.buyPlacements[i].lots !== newCfg.buyPlacements[i].lots ||
          oldCfg.buyPlacements[i].gapFactor !== newCfg.buyPlacements[i].gapFactor) {
          buyPlacementsModified = true
          break
        }
      }
    }
    page.buyPlacementsTableWrapper.classList.toggle('modified', buyPlacementsModified)

    // Sell placements input
    let sellPlacementsModified = false
    if (oldCfg.sellPlacements.length !== newCfg.sellPlacements.length) {
      sellPlacementsModified = true
    } else {
      for (let i = 0; i < oldCfg.sellPlacements.length; i++) {
        if (oldCfg.sellPlacements[i].lots !== newCfg.sellPlacements[i].lots ||
          oldCfg.sellPlacements[i].gapFactor !== newCfg.sellPlacements[i].gapFactor) {
          sellPlacementsModified = true
          break
        }
      }
    }
    page.sellPlacementsTableWrapper.classList.toggle('modified', sellPlacementsModified)
    page.driftToleranceContainer.classList.toggle('modified', this.driftTolerance.modified())
    page.oracleBiasContainer.classList.toggle('modified', this.oracleBias.modified())
    page.useOracleCheckbox.classList.toggle('modified', oldCfg.useOracles !== newCfg.useOracles)
    page.oracleWeightingContainer.classList.toggle('modified', this.oracleWeighting.modified())
    page.emptyMarketRateInput.classList.toggle('modified', oldCfg.emptyMarketRate !== newCfg.emptyMarketRate)
    page.emptyMarketRateCheckbox.classList.toggle('modified', oldCfg.useEmptyMarketRate !== newCfg.useEmptyMarketRate)
    page.baseBalanceContainer.classList.toggle('modified', this.baseBalance && this.baseBalance.modified())
    page.quoteBalanceContainer.classList.toggle('modified', this.quoteBalance && this.quoteBalance.modified())
    page.orderPersistenceContainer.classList.toggle('modified', this.orderPersistence.modified())

    // Base wallet settings
    for (const opt of Object.keys(this.baseWalletSettingControl)) {
      this.baseWalletSettingControl[opt].toHighlight.classList.toggle('modified', oldCfg.baseOptions[opt] !== newCfg.baseOptions[opt])
    }

    // Quote wallet settings
    for (const opt of Object.keys(this.quoteWalletSettingControl)) {
      this.quoteWalletSettingControl[opt].toHighlight.classList.toggle('modified', oldCfg.quoteOptions[opt] !== newCfg.quoteOptions[opt])
    }
  }

  /*
   * gapFactorHeaderUnit returns the header on the placements table and the
   * units in the gap factor rows needed for each gap strategy.
   */
  gapFactorHeaderUnit (gapStrategy: string) : [string, string] {
    switch (gapStrategy) {
      case GapStrategyMultiplier:
        return ['Multiplier', 'x']
      case GapStrategyAbsolute:
      case GapStrategyAbsolutePlus: {
        const rateUnit = `${app().assets[this.quoteID].symbol}/${app().assets[this.baseID].symbol}`
        return ['Rate', rateUnit]
      }
      case GapStrategyPercent:
      case GapStrategyPercentPlus:
        return ['Percent', '%']
      default:
        throw new Error(`Unknown gap strategy ${gapStrategy}`)
    }
  }

  /*
   * checkGapFactorRange returns an error string if the value input for a
   * gap factor is valid for the currently selected gap strategy.
   */
  checkGapFactorRange (gapFactor: string, value: number) : (string | null) {
    switch (gapFactor) {
      case GapStrategyMultiplier:
        if (value < 1 || value > 100) {
          return 'Multiplier must be between 1 and 100'
        }
        return null
      case GapStrategyAbsolute:
      case GapStrategyAbsolutePlus:
        if (value <= 0) {
          return 'Rate must be greater than 0'
        }
        return null
      case GapStrategyPercent:
      case GapStrategyPercentPlus:
        if (value <= 0 || value > 10) {
          return 'Percent must be between 0 and 10'
        }
        return null
      default: {
        throw new Error(`Unknown gap factor ${gapFactor}`)
      }
    }
  }

  /*
   * convertGapFactor converts between the displayed gap factor in the
   * placement tables and the number that is passed to the market maker.
   * For gap strategies that involve a percentage it converts between the
   * decimal value required by the backend and a percentage displayed to
   * the user.
   */
  convertGapFactor (gapFactor: number, gapStrategy: string, toDisplay: boolean): number {
    switch (gapStrategy) {
      case GapStrategyMultiplier:
      case GapStrategyAbsolute:
      case GapStrategyAbsolutePlus:
        return gapFactor
      case GapStrategyPercent:
      case GapStrategyPercentPlus:
        if (toDisplay) {
          return gapFactor * 100
        }
        return gapFactor / 100
      default:
        throw new Error(`Unknown gap factor ${gapStrategy}`)
    }
  }

  /*
   * addPlacement adds a row to a placement table. This is called both when
   * the page is initially loaded, and when the "add" button is pressed on
   * the placement table. initialLoadPlacement is non-nil if this is being
   * called on the initial load.
   */
  addPlacement (isBuy: boolean, initialLoadPlacement: OrderPlacement | null, running: boolean, gapStrategy?: string) {
    const { page, updatedConfig: cfg } = this

    let tableBody: PageElement = page.sellPlacementsTableBody
    let addPlacementRow: PageElement = page.addSellPlacementRow
    let lotsElement: PageElement = page.addSellPlacementLots
    let gapFactorElement: PageElement = page.addSellPlacementGapFactor
    let errElement: PageElement = page.sellPlacementsErr
    if (isBuy) {
      tableBody = page.buyPlacementsTableBody
      addPlacementRow = page.addBuyPlacementRow
      lotsElement = page.addBuyPlacementLots
      gapFactorElement = page.addBuyPlacementGapFactor
      errElement = page.buyPlacementsErr
    }

    // updateArrowVis updates the visibility of the move up/down arrows in
    // each row of the placement table. The up arrow is not shown on the
    // top row, and the down arrow is not shown on the bottom row. They
    // are all hidden if market making is running.
    const updateArrowVis = () => {
      for (let i = 0; i < tableBody.children.length - 1; i++) {
        const row = Doc.parseTemplate(tableBody.children[i] as HTMLElement)
        if (running) {
          Doc.hide(row.upBtn, row.downBtn)
        } else {
          Doc.setVis(i !== 0, row.upBtn)
          Doc.setVis(i !== tableBody.children.length - 2, row.downBtn)
        }
      }
    }

    Doc.hide(errElement)
    const setErr = (err: string) => {
      errElement.textContent = err
      Doc.show(errElement)
    }

    let lots : number
    let actualGapFactor : number
    let displayedGapFactor : number
    if (!gapStrategy) gapStrategy = this.selectedCEX() ? GapStrategyMultiplier : cfg.gapStrategy
    const placements = isBuy ? cfg.buyPlacements : cfg.sellPlacements
    const unit = this.gapFactorHeaderUnit(gapStrategy)[1]
    if (initialLoadPlacement) {
      lots = initialLoadPlacement.lots
      actualGapFactor = initialLoadPlacement.gapFactor
      displayedGapFactor = this.convertGapFactor(actualGapFactor, gapStrategy, true)
    } else {
      lots = parseInt(lotsElement.value || '0')
      displayedGapFactor = parseFloat(gapFactorElement.value || '0')
      actualGapFactor = this.convertGapFactor(displayedGapFactor, gapStrategy, false)
      if (lots === 0) {
        setErr('Lots must be greater than 0')
        return
      }

      const gapFactorErr = this.checkGapFactorRange(gapStrategy, displayedGapFactor)
      if (gapFactorErr) {
        setErr(gapFactorErr)
        return
      }

      if (placements.find((placement) => placement.gapFactor === actualGapFactor)
      ) {
        setErr('Duplicate placement')
        return
      }

      placements.push({ lots, gapFactor: actualGapFactor })
    }

    const newRow = page.placementRowTmpl.cloneNode(true) as PageElement
    const newRowTmpl = Doc.parseTemplate(newRow)
    newRowTmpl.priority.textContent = `${tableBody.children.length}`
    newRowTmpl.lots.textContent = `${lots}`
    newRowTmpl.gapFactor.textContent = `${displayedGapFactor} ${unit}`
    Doc.bind(newRowTmpl.removeBtn, 'click', () => {
      const index = placements.findIndex((placement) => {
        return placement.lots === lots && placement.gapFactor === actualGapFactor
      })
      if (index === -1) return
      placements.splice(index, 1)
      newRow.remove()
      updateArrowVis()
      this.updateModifiedMarkers()
    })
    if (running) {
      Doc.hide(newRowTmpl.removeBtn)
    }

    Doc.bind(newRowTmpl.upBtn, 'click', () => {
      const index = placements.findIndex((p: OrderPlacement) => p.lots === lots && p.gapFactor === actualGapFactor)
      if (index === 0) return
      const prevPlacement = placements[index - 1]
      placements[index - 1] = placements[index]
      placements[index] = prevPlacement
      newRowTmpl.priority.textContent = `${index}`
      newRow.remove()
      tableBody.insertBefore(newRow, tableBody.children[index - 1])
      const movedDownTmpl = Doc.parseTemplate(
        tableBody.children[index] as HTMLElement
      )
      movedDownTmpl.priority.textContent = `${index + 1}`
      updateArrowVis()
      this.updateModifiedMarkers()
    })

    Doc.bind(newRowTmpl.downBtn, 'click', () => {
      const index = placements.findIndex((p) => p.lots === lots && p.gapFactor === actualGapFactor)
      if (index === placements.length - 1) return
      const nextPlacement = placements[index + 1]
      placements[index + 1] = placements[index]
      placements[index] = nextPlacement
      newRowTmpl.priority.textContent = `${index + 2}`
      newRow.remove()
      tableBody.insertBefore(newRow, tableBody.children[index + 1])
      const movedUpTmpl = Doc.parseTemplate(
        tableBody.children[index] as HTMLElement
      )
      movedUpTmpl.priority.textContent = `${index + 1}`
      updateArrowVis()
      this.updateModifiedMarkers()
    })

    tableBody.insertBefore(newRow, addPlacementRow)
    updateArrowVis()
  }

  setArbMMLabels () {
    this.page.buyGapFactorHdr.textContent = intl.prep(intl.ID_MULTIPLIER)
    this.page.sellGapFactorHdr.textContent = intl.prep(intl.ID_MULTIPLIER)
  }

  /*
   * setGapFactorLabels sets the headers on the gap factor column of each
   * placement table.
   */
  setGapFactorLabels (gapStrategy: string) {
    const page = this.page
    const header = this.gapFactorHeaderUnit(gapStrategy)[0]
    page.buyGapFactorHdr.textContent = header
    page.sellGapFactorHdr.textContent = header
    Doc.hide(page.percentPlusInfo, page.percentInfo, page.absolutePlusInfo, page.absoluteInfo, page.multiplierInfo)
    switch (gapStrategy) {
      case 'percent-plus':
        return Doc.show(page.percentPlusInfo)
      case 'percent':
        return Doc.show(page.percentInfo)
      case 'absolute-plus':
        return Doc.show(page.absolutePlusInfo)
      case 'absolute':
        return Doc.show(page.absoluteInfo)
      case 'multiplier':
        return Doc.show(page.multiplierInfo)
    }
  }

  /*
   * setupMMConfigSettings sets up the controls for the settings defined in
   * the market making config.
   */
  setupMMConfigSettings (running: boolean) {
    const { page, updatedConfig: cfg } = this

    // Gap Strategy
    Doc.bind(page.gapStrategySelect, 'change', () => {
      if (!page.gapStrategySelect.value) return
      const gapStrategy = page.gapStrategySelect.value
      this.clearPlacements(cfg.gapStrategy)
      this.loadCachedPlacements(gapStrategy)
      cfg.gapStrategy = gapStrategy
      this.setGapFactorLabels(gapStrategy)
      this.updateModifiedMarkers()
    })
    if (running) {
      page.gapStrategySelect.setAttribute('disabled', 'true')
    }

    // Buy/Sell placements
    Doc.bind(page.addBuyPlacementBtn, 'click', () => {
      this.addPlacement(true, null, false)
      page.addBuyPlacementLots.value = ''
      page.addBuyPlacementGapFactor.value = ''
      this.updateModifiedMarkers()
    })
    Doc.bind(page.addSellPlacementBtn, 'click', () => {
      this.addPlacement(false, null, false)
      page.addSellPlacementLots.value = ''
      page.addSellPlacementGapFactor.value = ''
      this.updateModifiedMarkers()
    })
    Doc.setVis(!running, page.addBuyPlacementRow, page.addSellPlacementRow)

    const maybeSubmitBuyRow = (e: KeyboardEvent) => {
      if (e.key !== 'Enter') return
      if (
        !isNaN(parseFloat(page.addBuyPlacementGapFactor.value || '')) &&
        !isNaN(parseFloat(page.addBuyPlacementLots.value || ''))
      ) {
        page.addBuyPlacementBtn.click()
      }
    }
    Doc.bind(page.addBuyPlacementGapFactor, 'keyup', (e: KeyboardEvent) => { maybeSubmitBuyRow(e) })
    Doc.bind(page.addBuyPlacementLots, 'keyup', (e: KeyboardEvent) => { maybeSubmitBuyRow(e) })

    const maybeSubmitSellRow = (e: KeyboardEvent) => {
      if (e.key !== 'Enter') return
      if (
        !isNaN(parseFloat(page.addSellPlacementGapFactor.value || '')) &&
        !isNaN(parseFloat(page.addSellPlacementLots.value || ''))
      ) {
        page.addSellPlacementBtn.click()
      }
    }
    Doc.bind(page.addSellPlacementGapFactor, 'keyup', (e: KeyboardEvent) => { maybeSubmitSellRow(e) })
    Doc.bind(page.addSellPlacementLots, 'keyup', (e: KeyboardEvent) => { maybeSubmitSellRow(e) })

    const handleChanged = () => { this.updateModifiedMarkers() }

    // Profit
    page.profitInput.value = String(cfg.profit)
    Doc.bind(page.profitInput, 'change', () => {
      Doc.hide(page.profitInputErr)
      const showError = (errID: string) => {
        Doc.show(page.profitInputErr)
        page.profitInputErr.textContent = intl.prep(errID)
      }
      cfg.profit = parseFloat(page.profitInput.value || '')
      if (isNaN(cfg.profit)) return showError(intl.ID_INVALID_VALUE)
      if (cfg.profit === 0) return showError(intl.ID_NO_ZERO)
    })

    // Drift tolerance
    this.driftTolerance = new RangeOption(driftToleranceRange, cfg.driftTolerance, false, false, running, cfg, 'driftTolerance')
    this.driftTolerance.setChanged(handleChanged)
    page.driftToleranceContainer.appendChild(this.driftTolerance.div)

    // CEX order persistence
    this.orderPersistence = new RangeOption(orderPersistenceRange, cfg.orderPersistence, true, true, running, cfg, 'orderPersistence')
    this.orderPersistence.setChanged(handleChanged)
    this.orderPersistence.setUpdate((x: number) => {
      this.orderPersistence.xyRange.setXLabel('')
      x = Math.round(x)
      this.orderPersistence.xyRange.setYLabel(x === 21 ? '∞' : String(x))
      return x
    })
    page.orderPersistenceContainer.appendChild(this.orderPersistence.div)

    // Use oracle
    Doc.bind(page.useOracleCheckbox, 'change', () => {
      this.useOraclesChanged()
      this.updateModifiedMarkers()
    })
    if (running) {
      page.useOracleCheckbox.setAttribute('disabled', 'true')
    }

    // Oracle Bias
    this.oracleBias = new RangeOption(oracleBiasRange, cfg.oracleBias, false, false, running, cfg, 'oracleBias')
    this.oracleBias.setChanged(handleChanged)
    page.oracleBiasContainer.appendChild(this.oracleBias.div)

    // Oracle Weighting
    this.oracleWeighting = new RangeOption(oracleWeightRange, cfg.oracleWeighting, false, false, running, cfg, 'oracleWeighting')
    this.oracleWeighting.setChanged(handleChanged)
    page.oracleWeightingContainer.appendChild(this.oracleWeighting.div)

    // Empty Market Rate
    Doc.bind(page.emptyMarketRateCheckbox, 'change', () => {
      this.useEmptyMarketRateChanged()
      this.updateModifiedMarkers()
    })
    Doc.bind(page.emptyMarketRateInput, 'change', () => {
      Doc.hide(page.emptyMarketRateErr)
      cfg.emptyMarketRate = parseFloat(page.emptyMarketRateInput.value || '0')
      this.updateModifiedMarkers()
      if (cfg.emptyMarketRate === 0) {
        Doc.show(page.emptyMarketRateErr)
        page.emptyMarketRateErr.textContent = intl.prep(intl.ID_NO_ZERO)
      }
    })
    if (running) {
      page.emptyMarketRateCheckbox.setAttribute('disabled', 'true')
      page.emptyMarketRateInput.setAttribute('disabled', 'true')
    }
  }

  clearPlacements (cacheKey: string) {
    const { page, updatedConfig: cfg } = this
    while (page.buyPlacementsTableBody.children.length > 1) {
      page.buyPlacementsTableBody.children[0].remove()
    }
    while (page.sellPlacementsTableBody.children.length > 1) {
      page.sellPlacementsTableBody.children[0].remove()
    }
    this.placementsCache[cacheKey] = [cfg.buyPlacements, cfg.sellPlacements]
    cfg.buyPlacements = []
    cfg.sellPlacements = []
  }

  loadCachedPlacements (cacheKey: string) {
    const c = this.placementsCache[cacheKey]
    if (!c) return
    const { updatedConfig: cfg } = this
    cfg.buyPlacements = c[0]
    cfg.sellPlacements = c[1]
    const gapStrategy = cacheKey === arbMMRowCacheKey ? GapStrategyMultiplier : cacheKey
    for (const p of cfg.buyPlacements) this.addPlacement(true, p, false, gapStrategy)
    for (const p of cfg.sellPlacements) this.addPlacement(false, p, false, gapStrategy)
  }

  useOraclesChanged () {
    const { page, updatedConfig: cfg } = this
    if (page.useOracleCheckbox.checked) {
      Doc.show(page.oracleBiasSection, page.oracleWeightingSection)
      cfg.useOracles = true
      this.oracleWeighting.setValue(cfg.oracleWeighting || defaultMarketMakingConfig.oracleWeighting)
      this.oracleBias.setValue(cfg.oracleBias || defaultMarketMakingConfig.oracleBias)
    } else {
      Doc.hide(page.oracleBiasSection, page.oracleWeightingSection)
      cfg.useOracles = false
    }
  }

  useEmptyMarketRateChanged () {
    const { page, updatedConfig: cfg } = this
    if (page.emptyMarketRateCheckbox.checked) {
      cfg.useEmptyMarketRate = true
      const r = cfg.emptyMarketRate ?? this.originalConfig.emptyMarketRate ?? 0
      page.emptyMarketRateInput.value = String(r)
      cfg.emptyMarketRate = r
      Doc.show(page.emptyMarketRateInputBox)
      this.updateModifiedMarkers()
    } else {
      cfg.useEmptyMarketRate = false
      Doc.hide(page.emptyMarketRateInputBox)
    }
  }

  /*
   * setOriginalValues sets the updatedConfig field to be equal to the
   * and sets the values displayed buy each field input to be equal
   * to the values since the last save.
   */
  setOriginalValues (running: boolean) {
    const {
      page, originalConfig: oldCfg, updatedConfig: cfg,
      oracleBias, oracleWeighting, driftTolerance, orderPersistence,
      baseBalance, quoteBalance
    } = this

    this.clearPlacements(oldCfg.cexName ? arbMMRowCacheKey : cfg.gapStrategy)

    // The RangeOptions maintain references to the options object, so we'll
    // need to preserve those.
    const [bOpts, qOpts] = [cfg.baseOptions, cfg.quoteOptions]

    Object.assign(cfg, oldCfg)

    // Re-assing the wallet options.
    for (const k of Object.keys(bOpts)) delete bOpts[k]
    for (const k of Object.keys(qOpts)) delete qOpts[k]
    Object.assign(bOpts, oldCfg.baseOptions)
    Object.assign(qOpts, oldCfg.quoteOptions)
    cfg.baseOptions = bOpts
    cfg.quoteOptions = qOpts

    oracleBias.reset()
    oracleWeighting.reset()
    driftTolerance.reset()
    orderPersistence.reset()
    if (baseBalance) baseBalance.reset()
    if (quoteBalance) quoteBalance.reset()
    page.profitInput.value = String(cfg.profit)
    page.useOracleCheckbox.checked = cfg.useOracles && oldCfg.oracleWeighting > 0
    this.useOraclesChanged()
    page.emptyMarketRateCheckbox.checked = cfg.useEmptyMarketRate && cfg.emptyMarketRate > 0
    this.useEmptyMarketRateChanged()

    // Gap strategy
    if (!page.gapStrategySelect.options) return
    Array.from(page.gapStrategySelect.options).forEach((opt: HTMLOptionElement) => { opt.selected = opt.value === cfg.gapStrategy })
    this.setGapFactorLabels(cfg.gapStrategy)

    if (cfg.cexName) this.selectCEX(cfg.cexName)
    else this.unselectCEX()

    // Buy/Sell placements
    oldCfg.buyPlacements.forEach((p) => { this.addPlacement(true, p, running) })
    oldCfg.sellPlacements.forEach((p) => { this.addPlacement(false, p, running) })

    for (const opt of Object.keys(cfg.baseOptions)) {
      const value = oldCfg.baseOptions[opt]
      cfg.baseOptions[opt] = value
      if (this.baseWalletSettingControl[opt]) {
        this.baseWalletSettingControl[opt].setValue(value)
      }
    }

    // Quote wallet options
    for (const opt of Object.keys(cfg.quoteOptions)) {
      const value = oldCfg.quoteOptions[opt]
      cfg.quoteOptions[opt] = value
      if (this.quoteWalletSettingControl[opt]) {
        this.quoteWalletSettingControl[opt].setValue(value)
      }
    }

    this.updateModifiedMarkers()
  }

  /*
   * validateFields validates configuration values and optionally shows error
   * messages.
   */
  validateFields (showErrors: boolean): boolean {
    let ok = true
    const { page, updatedConfig: { sellPlacements, buyPlacements, profit, useEmptyMarketRate, emptyMarketRate, baseBalance, quoteBalance } } = this
    const setError = (errEl: PageElement, errID: string) => {
      ok = false
      if (!showErrors) return
      errEl.textContent = intl.prep(errID)
      Doc.show(errEl)
    }
    if (showErrors) {
      Doc.hide(
        page.buyPlacementsErr, page.sellPlacementsErr, page.profitInputErr,
        page.emptyMarketRateErr, page.baseBalanceErr, page.quoteBalanceErr
      )
    }
    if (buyPlacements.length === 0) setError(page.buyPlacementsErr, intl.ID_NO_PLACEMENTS)
    if (sellPlacements.length === 0) setError(page.sellPlacementsErr, intl.ID_NO_PLACEMENTS)
    if (this.selectedCEX()) {
      if (isNaN(profit)) setError(page.profitInputErr, intl.ID_INVALID_VALUE)
      else if (profit === 0) setError(page.profitInputErr, intl.ID_NO_ZERO)
    } else { // basic mm
      // DRAFT TODO: Should we enforce an empty market rate if there are no
      // oracles?
      if (useEmptyMarketRate && emptyMarketRate === 0) setError(page.emptyMarketRateErr, intl.ID_NO_ZERO)
      if (baseBalance === 0) setError(page.baseBalanceErr, intl.ID_NO_ZERO)
      if (quoteBalance === 0) setError(page.quoteBalanceErr, intl.ID_NO_ZERO)
    }
    return ok
  }

  /*
   * saveSettings updates the settings in the backend, and sets the originalConfig
   * to be equal to the updatedConfig.
   */
  async saveSettings () {
    // Make a copy and delete either the basic mm config or the arb-mm config,
    // depending on whether a cex is selected.
    if (!this.validateFields(true)) return
    const { updatedConfig: cfg, baseID, quoteID, host } = this
    const botCfg: BotConfig = {
      host: host,
      baseID: baseID,
      quoteID: quoteID,
      baseBalanceType: cfg.baseBalanceType,
      baseBalance: cfg.baseBalance,
      quoteBalanceType: cfg.quoteBalanceType,
      quoteBalance: cfg.quoteBalance,
      disabled: cfg.disabled
    }
    if (this.selectedCEX()) botCfg.arbMarketMakingConfig = this.arbMMConfig()
    else botCfg.basicMarketMakingConfig = this.basicMMConfig()
    await MM.updateBotConfig(botCfg)
    this.originalConfig = JSON.parse(JSON.stringify(cfg))
    this.updateModifiedMarkers()
    app().loadPage('mm')
  }

  /*
   * arbMMConfig parses the configuration for the arb-mm bot. Only one of
   * arbMMConfig or basicMMConfig should be used when updating the bot
   * configuration. Which is used depends on if the user has configured and
   * selected a CEX or not.
   */
  arbMMConfig (): ArbMarketMakingCfg {
    const { updatedConfig: cfg } = this
    const arbCfg: ArbMarketMakingCfg = {
      cexName: cfg.cexName,
      buyPlacements: [],
      sellPlacements: [],
      profit: cfg.profit,
      driftTolerance: cfg.driftTolerance,
      orderPersistence: cfg.orderPersistence,
      baseOptions: cfg.baseOptions,
      quoteOptions: cfg.quoteOptions
    }
    for (const p of cfg.buyPlacements) arbCfg.buyPlacements.push({ lots: p.lots, multiplier: p.gapFactor })
    for (const p of cfg.sellPlacements) arbCfg.sellPlacements.push({ lots: p.lots, multiplier: p.gapFactor })
    return arbCfg
  }

  /*
   * basicMMConfig parses the configuration for the basic marketmaker. Only of
   * of basidMMConfig or arbMMConfig should be used when updating the bot
   * configuration.
   */
  basicMMConfig (): BasicMarketMakingCfg {
    const { updatedConfig: cfg } = this
    const mmCfg: BasicMarketMakingCfg = {
      gapStrategy: cfg.gapStrategy,
      sellPlacements: cfg.sellPlacements,
      buyPlacements: cfg.buyPlacements,
      driftTolerance: cfg.driftTolerance,
      oracleWeighting: cfg.useOracles ? cfg.oracleWeighting : 0,
      oracleBias: cfg.useOracles ? cfg.oracleBias : 0,
      emptyMarketRate: cfg.useEmptyMarketRate ? cfg.emptyMarketRate : 0,
      baseOptions: cfg.baseOptions,
      quoteOptions: cfg.quoteOptions
    }
    return mmCfg
  }

  /*
   * selectedCEX returns the currently selected CEX, else null if none are
   * selected.
   */
  selectedCEX (): cexButton | null {
    const cexes = Object.entries(this.cexes).filter((v: [string, cexButton]) => v[1].div.classList.contains('selected'))
    if (cexes.length) return cexes[0][1]
    return null
  }

  /*
   * setupBalanceSelectors sets up the balance selection sections. If an asset
   * has no balance available, or of other market makers have claimed the entire
   * balance, a message communicating this is displayed.
   */
  setupBalanceSelectors (running: boolean) {
    const { page, updatedConfig: cfg, host, baseID, quoteID } = this
    const { wallet: { balance: { available: bAvail } }, unitInfo: bui } = app().assets[baseID]
    const { wallet: { balance: { available: qAvail } }, unitInfo: qui } = app().assets[quoteID]

    let baseReservedByOtherBots = 0
    let quoteReservedByOtherBots = 0;
    (MM.cfg.botConfigs || []).forEach((botCfg: BotConfig) => {
      if (botCfg.baseID === baseID && botCfg.quoteID === quoteID &&
        botCfg.host === host) {
        return
      }
      if (botCfg.baseID === baseID) baseReservedByOtherBots += botCfg.baseBalance
      if (botCfg.quoteID === baseID) baseReservedByOtherBots += botCfg.quoteBalance
      if (botCfg.baseID === quoteID) quoteReservedByOtherBots += botCfg.baseBalance
      if (botCfg.quoteID === quoteID) quoteReservedByOtherBots += botCfg.quoteBalance
    })

    const baseMaxPercent = baseReservedByOtherBots < 100 ? 100 - baseReservedByOtherBots : 0
    const quoteMaxPercent = quoteReservedByOtherBots < 100 ? 100 - quoteReservedByOtherBots : 0
    const baseMaxAvailable = Doc.conventionalCoinValue(bAvail * baseMaxPercent / 100, bui)
    const quoteMaxAvailable = Doc.conventionalCoinValue(qAvail * quoteMaxPercent / 100, qui)

    const baseXYRange: XYRange = {
      start: {
        label: '0%',
        x: 0,
        y: 0
      },
      end: {
        label: `${baseMaxPercent}%`,
        x: baseMaxPercent,
        y: baseMaxAvailable
      },
      xUnit: '%',
      yUnit: bui.conventional.unit
    }

    const quoteXYRange: XYRange = {
      start: {
        label: '0%',
        x: 0,
        y: 0
      },
      end: {
        label: `${quoteMaxPercent}%`,
        x: quoteMaxPercent,
        y: quoteMaxAvailable
      },
      xUnit: '%',
      yUnit: qui.conventional.unit
    }

    Doc.hide(page.noBaseBalance, page.noQuoteBalance, page.baseBalanceContainer, page.quoteBalanceContainer)
    Doc.empty(page.baseBalanceContainer, page.quoteBalanceContainer)

    if (baseMaxAvailable > 0) {
      this.baseBalance = new RangeOption(baseXYRange, cfg.baseBalance, false, true, running, cfg, 'baseBalance')
      this.baseBalance.setChanged(() => { this.updateModifiedMarkers() })
      page.baseBalanceContainer.appendChild(this.baseBalance.div)
      Doc.show(page.baseBalanceContainer)
    } else {
      this.baseBalance = undefined
      Doc.show(page.noBaseBalance)
    }

    if (quoteMaxAvailable > 0) {
      this.quoteBalance = new RangeOption(quoteXYRange, cfg.quoteBalance, false, true, running, cfg, 'quoteBalance')
      this.quoteBalance.setChanged(() => { this.updateModifiedMarkers() })
      page.quoteBalanceContainer.appendChild(this.quoteBalance.div)
      Doc.show(page.quoteBalanceContainer)
    } else {
      this.quoteBalance = undefined
      Doc.show(page.noQuoteBalance)
    }
  }

  /*
    setupWalletSetting sets up the base and quote wallet setting sections.
    These are based on the multi funding settings in the wallet definition.
  */
  setupWalletSettings (running: boolean) {
    const { page, updatedConfig: { quoteOptions, baseOptions } } = this
    const baseWalletSettings = app().currentWalletDefinition(this.baseID)
    const quoteWalletSettings = app().currentWalletDefinition(this.quoteID)
    Doc.setVis(baseWalletSettings.multifundingopts, page.baseWalletSettings)
    Doc.setVis(quoteWalletSettings.multifundingopts, page.quoteWalletSettings)
    const baseOptToSetting : Record<string, PageElement> = {}
    const quoteOptToSetting : Record<string, PageElement> = {}
    const baseDependentOpts : Record<string, string[]> = {}
    const quoteDependentOpts : Record<string, string[]> = {}
    const addDependentOpt = (optKey: string, optSetting: PageElement, dependentOn: string, quote: boolean) => {
      let dependentOpts : Record<string, string[]>
      let optToSetting : Record<string, PageElement>
      if (quote) {
        dependentOpts = quoteDependentOpts
        optToSetting = quoteOptToSetting
      } else {
        dependentOpts = baseDependentOpts
        optToSetting = baseOptToSetting
      }
      if (!dependentOpts[dependentOn]) dependentOpts[dependentOn] = []
      dependentOpts[dependentOn].push(optKey)
      optToSetting[optKey] = optSetting
    }
    const setDependentOptsVis = (parentOptKey: string, vis: boolean, quote: boolean) => {
      let dependentOpts : Record<string, string[]>
      let optToSetting : Record<string, PageElement>
      if (quote) {
        dependentOpts = quoteDependentOpts
        optToSetting = quoteOptToSetting
      } else {
        dependentOpts = baseDependentOpts
        optToSetting = baseOptToSetting
      }
      const optKeys = dependentOpts[parentOptKey]
      if (!optKeys) return
      for (const optKey of optKeys) {
        Doc.setVis(vis, optToSetting[optKey])
      }
    }
    const storeWalletSettingControl = (optKey: string, toHighlight: PageElement, setValue: (x:string) => void, quote: boolean) => {
      if (quote) {
        this.quoteWalletSettingControl[optKey] = {
          toHighlight,
          setValue
        }
      } else {
        this.baseWalletSettingControl[optKey] = {
          toHighlight,
          setValue
        }
      }
    }
    const setWalletOption = (quote: boolean, key: string, value: string) => {
      if (quote) quoteOptions[key] = value
      else baseOptions[key] = value
    }
    const getWalletOption = (quote: boolean, key: string) : string | undefined => {
      if (quote) return quoteOptions[key]
      else return baseOptions[key]
    }
    const addOpt = (opt: OrderOption, quote: boolean) => {
      let container
      let optionsDict
      if (quote) {
        container = page.quoteWalletSettingsContainer
        optionsDict = quoteOptions
      } else {
        if (opt.quoteAssetOnly) return
        container = page.baseWalletSettingsContainer
        optionsDict = baseOptions
      }
      const currVal = optionsDict[opt.key]
      let setting : PageElement | undefined
      if (opt.isboolean) {
        setting = page.boolSettingTmpl.cloneNode(true) as PageElement
        const tmpl = Doc.parseTemplate(setting)
        tmpl.name.textContent = opt.displayname
        tmpl.input.checked = currVal === 'true'
        if (running) tmpl.input.setAttribute('disabled', 'true')
        Doc.bind(tmpl.input, 'change', () => {
          setWalletOption(quote, opt.key, tmpl.input.checked ? 'true' : 'false')
          setDependentOptsVis(opt.key, !!tmpl.input.checked, quote)
          this.updateModifiedMarkers()
        })
        const setValue = (x: string) => {
          tmpl.input.checked = x === 'true'
          setDependentOptsVis(opt.key, !!tmpl.input.checked, quote)
        }
        storeWalletSettingControl(opt.key, tmpl.input, setValue, quote)
        if (opt.description) tmpl.tooltip.dataset.tooltip = opt.description
        container.appendChild(setting)
      } else if (opt.xyRange) {
        setting = page.rangeSettingTmpl.cloneNode(true) as PageElement
        const tmpl = Doc.parseTemplate(setting)
        tmpl.name.textContent = opt.displayname
        if (opt.description) tmpl.tooltip.dataset.tooltip = opt.description
        const handler = new RangeOption(opt.xyRange, parseInt(currVal), Boolean(opt.xyRange.roundX), Boolean(opt.xyRange.roundY), running, optionsDict, opt.key)
        handler.stringify()
        handler.setChanged(() => { this.updateModifiedMarkers() })
        const setValue = (x: string) => { handler.setValue(parseInt(x)) }
        storeWalletSettingControl(opt.key, tmpl.sliderContainer, setValue, quote)
        tmpl.sliderContainer.appendChild(handler.div)
        container.appendChild(setting)
      }
      if (!setting) return
      if (opt.dependsOn) {
        addDependentOpt(opt.key, setting, opt.dependsOn, quote)
        const parentOptVal = getWalletOption(quote, opt.dependsOn)
        Doc.setVis(parentOptVal === 'true', setting)
      }
    }

    if (baseWalletSettings.multifundingopts && baseWalletSettings.multifundingopts.length > 0) {
      for (const opt of baseWalletSettings.multifundingopts) addOpt(opt, false)
    }
    if (quoteWalletSettings.multifundingopts && quoteWalletSettings.multifundingopts.length > 0) {
      for (const opt of quoteWalletSettings.multifundingopts) addOpt(opt, true)
    }
    app().bindTooltips(page.baseWalletSettingsContainer)
    app().bindTooltips(page.quoteWalletSettingsContainer)
  }

  /*
   * fetchOracles fetches the current oracle rates and fiat rates, and displays
   * them on the screen.
   */
  async fetchOracles (): Promise<void> {
    const { page, baseID, quoteID } = this

    const res = await MM.report(baseID, quoteID)
    Doc.hide(page.oraclesLoading)

    if (!app().checkResponse(res)) {
      page.oraclesErrMsg.textContent = res.msg
      Doc.show(page.oraclesErr)
      return
    }

    const r = res.report as MarketReport
    if (!r.oracles || r.oracles.length === 0) {
      Doc.show(page.noOracles)
    } else {
      Doc.empty(page.oracles)
      for (const o of r.oracles ?? []) {
        const tr = page.oracleTmpl.cloneNode(true) as PageElement
        page.oracles.appendChild(tr)
        const tmpl = Doc.parseTemplate(tr)
        tmpl.logo.src = 'img/' + o.host + '.png'
        tmpl.host.textContent = ExchangeNames[o.host]
        tmpl.volume.textContent = Doc.formatFourSigFigs(o.usdVol)
        tmpl.price.textContent = Doc.formatFourSigFigs((o.bestBuy + o.bestSell) / 2)
      }
      page.avgPrice.textContent = r.price ? Doc.formatFourSigFigs(r.price) : '0'
      Doc.show(page.oraclesTable)
    }

    page.baseFiatRateSymbol.textContent = app().assets[baseID].symbol.toUpperCase()
    page.baseFiatRateLogo.src = Doc.logoPathFromID(baseID)
    if (r.baseFiatRate > 0) {
      page.baseFiatRate.textContent = Doc.formatFourSigFigs(r.baseFiatRate)
    } else {
      page.baseFiatRate.textContent = 'N/A'
    }

    page.quoteFiatRateSymbol.textContent = app().assets[quoteID].symbol.toUpperCase()
    page.quoteFiatRateLogo.src = Doc.logoPathFromID(quoteID)
    if (r.quoteFiatRate > 0) {
      page.quoteFiatRate.textContent = Doc.formatFourSigFigs(r.quoteFiatRate)
    } else {
      page.quoteFiatRate.textContent = 'N/A'
    }
    Doc.show(page.fiatRates)
  }

  /*
   * handleCEXSubmit handles clicks on the CEX configuration submission button.
   */
  async handleCEXSubmit () {
    const page = this.page
    Doc.hide(page.cexFormErr)
    const cexName = page.cexConfigForm.dataset.cexName || ''
    const apiKey = page.cexApiKeyInput.value
    const apiSecret = page.cexSecretInput.value
    if (!apiKey || !apiSecret) {
      Doc.show(page.cexFormErr)
      page.cexFormErr.textContent = intl.prep(intl.ID_NO_PASS_ERROR_MSG)
      return
    }
    try {
      await MM.updateCEXConfig({
        name: cexName,
        apiKey: apiKey,
        apiSecret: apiSecret
      })
    } catch (e) {
      Doc.show(page.cexFormErr)
      page.cexFormErr.textContent = intl.prep(intl.ID_API_ERROR, { msg: e.msg ?? String(e) })
      return
    }
    if (!this.cexes[cexName]) this.cexes[cexName] = this.addCEX(cexName)
    this.setCEXAvailability()
    this.forms.close()
    this.selectCEX(cexName)
  }

  /*
   * setupCEXes should be called during initialization.
   */
  setupCEXes () {
    const page = this.page
    this.cexes = {}
    Doc.empty(page.cexOpts)
    for (const name of Object.keys(CEXDisplayInfos)) this.cexes[name] = this.addCEX(name)
    this.setCEXAvailability()
  }

  /*
   * setCEXAvailability sets the coloring and messaging of the CEX selection
   * buttons.
   */
  setCEXAvailability () {
    const hasMarket = this.supportingCEXes()
    for (const { name, tmpl } of Object.values(this.cexes)) {
      const has = hasMarket[name]
      const configured = MM.cexConfigured(name)
      Doc.hide(tmpl.unavailable, tmpl.needsconfig)
      tmpl.logo.classList.remove('off')
      if (!configured) {
        Doc.show(tmpl.needsconfig)
      } else if (!has) {
        Doc.show(tmpl.unavailable)
        tmpl.logo.classList.add('off')
      }
    }
  }

  /*
   * addCEX adds a button for the specified CEX, returning a cexButton.
   */
  addCEX (cexName: string): cexButton {
    const { page, updatedConfig: cfg } = this
    const div = page.cexOptTmpl.cloneNode(true) as PageElement
    Doc.bind(div, 'click', () => {
      const cex = this.cexes[cexName]
      if (cex.div.classList.contains('selected')) {
        this.clearPlacements(arbMMRowCacheKey)
        this.loadCachedPlacements(page.gapStrategySelect.value || '')
        this.unselectCEX()
        return
      }
      if (!cfg.cexName) {
        this.clearPlacements(cfg.gapStrategy)
        this.loadCachedPlacements(arbMMRowCacheKey)
      }
      this.selectCEX(cexName)
    })
    page.cexOpts.appendChild(div)
    const tmpl = Doc.parseTemplate(div)
    const dinfo = CEXDisplayInfos[cexName]
    tmpl.name.textContent = dinfo.name
    tmpl.logo.src = '/img/' + dinfo.logo
    return { name: cexName, div, tmpl }
  }

  unselectCEX () {
    const { page, updatedConfig: cfg } = this
    for (const cex of Object.values(this.cexes)) cex.div.classList.remove('selected')
    Doc.show(page.gapStrategyBox, page.oraclesSettingBox, page.baseBalanceBox, page.quoteBalanceBox, page.cexPrompt)
    Doc.hide(page.profitSelectorBox)
    cfg.cexName = ''
    this.setGapFactorLabels(page.gapStrategySelect.value || '')
  }

  /*
   * selectCEX sets the specified CEX as selected, hiding basicmm-only settings
   * and displaying settings specific to the arbmm.
   */
  selectCEX (cexName: string) {
    const { page, updatedConfig: cfg } = this
    cfg.cexName = cexName
    // Check if we already have the cex configured.
    for (const cexCfg of (MM.cfg.cexConfigs || [])) {
      if (cexCfg.name !== cexName) continue
      for (const cex of Object.values(this.cexes)) {
        cex.div.classList.toggle('selected', cex.name === cexName)
      }
      Doc.hide(page.gapStrategyBox, page.oraclesSettingBox, page.baseBalanceBox, page.quoteBalanceBox, page.cexPrompt)
      Doc.show(page.profitSelectorBox)
      this.setArbMMLabels()
      return
    }
    const dinfo = CEXDisplayInfos[cexName]
    page.cexConfigLogo.src = '/img/' + dinfo.logo
    page.cexConfigName.textContent = dinfo.name
    page.cexConfigForm.dataset.cexName = cexName
    page.cexApiKeyInput.value = ''
    page.cexSecretInput.value = ''
    this.forms.show(page.cexConfigForm)
  }

  /*
   * supportingCEXes returns a lookup CEXes that have a matching market for the
   * currently selected base and quote assets.
   */
  supportingCEXes (): Record<string, boolean> {
    const cexes: Record<string, boolean> = {}
    const { baseID, quoteID } = this
    for (const [cexName, mkts] of Object.entries(MM.mkts)) {
      for (const { baseID: b, quoteID: q } of mkts) {
        if (b === baseID && q === quoteID) {
          cexes[cexName] = true
          break
        }
      }
    }
    return cexes
  }
}

const ExchangeNames: Record<string, string> = {
  'binance.com': 'Binance',
  'coinbase.com': 'Coinbase',
  'bittrex.com': 'Bittrex',
  'hitbtc.com': 'HitBTC',
  'exmo.com': 'EXMO'
}
