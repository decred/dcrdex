import {
  PageElement,
  BotConfig,
  XYRange,
  OrderPlacement,
  BalanceType,
  app,
  MarketReport,
  ConfigOption
} from './registry'
import { postJSON } from './http'
import Doc from './doc'
import BasePage from './basepage'
import { setOptionTemplates, XYRangeHandler } from './opts'

const GapStrategyMultiplier = 'multiplier'
const GapStrategyAbsolute = 'absolute'
const GapStrategyAbsolutePlus = 'absolute-plus'
const GapStrategyPercent = 'percent'
const GapStrategyPercentPlus = 'percent-plus'

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

const defaultMarketMakingConfig = {
  gapStrategy: GapStrategyPercentPlus,
  sellPlacements: [],
  buyPlacements: [],
  driftTolerance: 0.001,
  oracleWeighting: 0.1,
  oracleBias: 0
}

// walletSettingControl is used by the modified highlighting and
// reset values functionalities to manage the wallet settings
// defined in walletDefinition.multifundingopts
interface walletSettingControl {
  toHighlight: PageElement
  setValue: (value: string) => void
}

export default class MarketMakerSettingsPage extends BasePage {
  page: Record<string, PageElement>
  currentMarket: string
  originalConfig: BotConfig
  updatedConfig: BotConfig
  creatingNewBot: boolean
  host: string
  baseID: number
  quoteID: number
  oracleBiasRangeHandler: XYRangeHandler
  oracleWeightingRangeHandler: XYRangeHandler
  driftToleranceRangeHandler: XYRangeHandler
  baseBalanceRangeHandler?: XYRangeHandler
  quoteBalanceRangeHandler?: XYRangeHandler
  baseWalletSettingControl: Record<string, walletSettingControl> = {}
  quoteWalletSettingControl: Record<string, walletSettingControl> = {}

  constructor (main: HTMLElement) {
    super()

    const page = (this.page = Doc.idDescendants(main))

    setOptionTemplates(page)
    Doc.cleanTemplates(
      page.orderOptTmpl,
      page.booleanOptTmpl,
      page.rangeOptTmpl,
      page.placementRowTmpl,
      page.oracleTmpl,
      page.boolSettingTmpl,
      page.rangeSettingTmpl
    )

    Doc.bind(page.resetButton, 'click', () => { this.setOriginalValues(false) })
    Doc.bind(page.updateButton, 'click', () => {
      this.saveSettings()
      Doc.show(page.settingsUpdatedMsg)
      setTimeout(() => {
        Doc.hide(page.settingsUpdatedMsg)
      }, 2000)
    })
    Doc.bind(page.createButton, 'click', async () => {
      await this.saveSettings()
      app().loadPage('mm')
    })
    Doc.bind(page.cancelButton, 'click', () => {
      app().loadPage('mm')
    })

    const urlParams = new URLSearchParams(window.location.search)
    const host = urlParams.get('host')
    const base = urlParams.get('base')
    const quote = urlParams.get('quote')
    if (!host || !base || !quote) {
      console.log("Missing 'host', 'base', or 'quote' URL parameter")
      return
    }
    this.baseID = parseInt(base)
    this.quoteID = parseInt(quote)
    this.host = host
    page.baseHeader.textContent = app().assets[this.baseID].symbol.toUpperCase()
    page.quoteHeader.textContent = app().assets[this.quoteID].symbol.toUpperCase()
    page.hostHeader.textContent = host

    page.baseBalanceLogo.src = Doc.logoPathFromID(this.baseID)
    page.quoteBalanceLogo.src = Doc.logoPathFromID(this.quoteID)
    page.baseSettingsLogo.src = Doc.logoPathFromID(this.baseID)
    page.quoteSettingsLogo.src = Doc.logoPathFromID(this.quoteID)
    page.baseLogo.src = Doc.logoPathFromID(this.baseID)
    page.quoteLogo.src = Doc.logoPathFromID(this.quoteID)

    this.setup()
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

  async setup () {
    const page = this.page
    const botConfigs = await app().getMarketMakingConfig()
    const status = await app().getMarketMakingStatus()

    for (const cfg of botConfigs) {
      if (cfg.host === this.host && cfg.baseAsset === this.baseID && cfg.quoteAsset === this.quoteID) {
        this.originalConfig = JSON.parse(JSON.stringify(cfg))
        this.updatedConfig = JSON.parse(JSON.stringify(cfg))
        break
      }
    }
    this.creatingNewBot = !this.updatedConfig

    if (this.creatingNewBot) {
      this.originalConfig = JSON.parse(JSON.stringify({
        host: this.host,
        baseAsset: this.baseID,
        quoteAsset: this.quoteID,
        baseBalanceType: BalanceType.Percentage,
        baseBalance: 0,
        quoteBalanceType: BalanceType.Percentage,
        quoteBalance: 0,
        marketMakingConfig: defaultMarketMakingConfig,
        disabled: false
      }))
      this.originalConfig.marketMakingConfig.baseOptions = this.defaultWalletOptions(this.baseID)
      this.originalConfig.marketMakingConfig.quoteOptions = this.defaultWalletOptions(this.quoteID)
      this.updatedConfig = JSON.parse(JSON.stringify(this.originalConfig))
      Doc.hide(page.updateButton, page.resetButton)
      Doc.show(page.createButton)
    }

    if (status.running) {
      Doc.hide(page.updateButton, page.createButton, page.resetButton)
    }

    this.setupMMConfigSettings(status.running)
    this.setupBalanceSelectors(botConfigs, status.running)
    this.setupWalletSettings(status.running)
    this.setOriginalValues(status.running)
    Doc.show(page.botSettingsContainer)

    this.fetchOracles()
  }

  /*
   * updateModifiedMarkers checks each of the input elements on the page and
   * if the current value does not match the original value (since the last
   * save), then the input will have a colored border.
   */
  updateModifiedMarkers () {
    if (this.creatingNewBot) return
    const page = this.page
    const originalMMCfg = this.originalConfig.marketMakingConfig
    const updatedMMCfg = this.updatedConfig.marketMakingConfig

    // Gap strategy input
    const gapStrategyModified = originalMMCfg.gapStrategy !== updatedMMCfg.gapStrategy
    page.gapStrategySelect.classList.toggle('modified', gapStrategyModified)

    // Buy placements Input
    let buyPlacementsModified = false
    if (originalMMCfg.buyPlacements.length !== updatedMMCfg.buyPlacements.length) {
      buyPlacementsModified = true
    } else {
      for (let i = 0; i < originalMMCfg.buyPlacements.length; i++) {
        if (originalMMCfg.buyPlacements[i].lots !== updatedMMCfg.buyPlacements[i].lots ||
            originalMMCfg.buyPlacements[i].gapFactor !== updatedMMCfg.buyPlacements[i].gapFactor) {
          buyPlacementsModified = true
          break
        }
      }
    }
    page.buyPlacementsTableWrapper.classList.toggle('modified', buyPlacementsModified)

    // Sell placements input
    let sellPlacementsModified = false
    if (originalMMCfg.sellPlacements.length !== updatedMMCfg.sellPlacements.length) {
      sellPlacementsModified = true
    } else {
      for (let i = 0; i < originalMMCfg.sellPlacements.length; i++) {
        if (originalMMCfg.sellPlacements[i].lots !== updatedMMCfg.sellPlacements[i].lots ||
            originalMMCfg.sellPlacements[i].gapFactor !== updatedMMCfg.sellPlacements[i].gapFactor) {
          sellPlacementsModified = true
          break
        }
      }
    }
    page.sellPlacementsTableWrapper.classList.toggle('modified', sellPlacementsModified)

    // Drift tolerance input
    const driftToleranceModified = originalMMCfg.driftTolerance !== updatedMMCfg.driftTolerance
    page.driftToleranceContainer.classList.toggle('modified', driftToleranceModified)

    // Oracle bias input
    const oracleBiasModified = originalMMCfg.oracleBias !== updatedMMCfg.oracleBias
    page.oracleBiasContainer.classList.toggle('modified', oracleBiasModified)

    // Use oracles input
    const originalUseOracles = originalMMCfg.oracleWeighting !== 0
    const updatedUseOracles = updatedMMCfg.oracleWeighting !== 0
    const useOraclesModified = originalUseOracles !== updatedUseOracles
    page.useOracleCheckbox.classList.toggle('modified', useOraclesModified)

    // Oracle weighting input
    const oracleWeightingModified = originalMMCfg.oracleWeighting !== updatedMMCfg.oracleWeighting
    page.oracleWeightingContainer.classList.toggle('modified', oracleWeightingModified)

    // Empty market rates inputs
    const emptyMarketRateModified = originalMMCfg.emptyMarketRate !== updatedMMCfg.emptyMarketRate
    page.emptyMarketRateInput.classList.toggle('modified', emptyMarketRateModified)
    const emptyMarketRateCheckboxModified = (originalMMCfg.emptyMarketRate === undefined) !== (updatedMMCfg.emptyMarketRate === undefined)
    page.emptyMarketRateCheckbox.classList.toggle('modified', emptyMarketRateCheckboxModified)

    // Base balance input
    const baseBalanceModified = this.originalConfig.baseBalance !== this.updatedConfig.baseBalance
    page.baseBalanceContainer.classList.toggle('modified', baseBalanceModified)

    // Quote balance input
    const quoteBalanceModified = this.originalConfig.quoteBalance !== this.updatedConfig.quoteBalance
    page.quoteBalanceContainer.classList.toggle('modified', quoteBalanceModified)

    // Base wallet settings
    for (const opt of Object.keys(this.baseWalletSettingControl)) {
      if (!this.updatedConfig.marketMakingConfig.baseOptions) break
      if (!this.originalConfig.marketMakingConfig.baseOptions) break
      const originalValue = this.originalConfig.marketMakingConfig.baseOptions[opt]
      const updatedValue = this.updatedConfig.marketMakingConfig.baseOptions[opt]
      const modified = originalValue !== updatedValue
      this.baseWalletSettingControl[opt].toHighlight.classList.toggle('modified', modified)
    }

    // Quote wallet settings
    for (const opt of Object.keys(this.quoteWalletSettingControl)) {
      if (!this.updatedConfig.marketMakingConfig.quoteOptions) break
      if (!this.originalConfig.marketMakingConfig.quoteOptions) break
      const originalValue = this.originalConfig.marketMakingConfig.quoteOptions[opt]
      const updatedValue = this.updatedConfig.marketMakingConfig.quoteOptions[opt]
      const modified = originalValue !== updatedValue
      this.quoteWalletSettingControl[opt].toHighlight.classList.toggle('modified', modified)
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
  addPlacement (isBuy: boolean, initialLoadPlacement: OrderPlacement | null, running: boolean) {
    const page = this.page

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

    const getPlacementsList = (buy: boolean) : OrderPlacement[] => {
      if (buy) {
        return this.updatedConfig.marketMakingConfig.buyPlacements
      }
      return this.updatedConfig.marketMakingConfig.sellPlacements
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
    const gapStrategy = this.updatedConfig.marketMakingConfig.gapStrategy
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

      const placements = getPlacementsList(isBuy)
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
    newRowTmpl.removeBtn.onclick = () => {
      const placements = getPlacementsList(isBuy)
      const index = placements.findIndex((placement) => {
        return placement.lots === lots && placement.gapFactor === actualGapFactor
      })
      if (index === -1) return
      placements.splice(index, 1)
      newRow.remove()
      updateArrowVis()
      this.updateModifiedMarkers()
    }
    if (running) {
      Doc.hide(newRowTmpl.removeBtn)
    }

    newRowTmpl.upBtn.onclick = () => {
      const placements = getPlacementsList(isBuy)
      const index = placements.findIndex(
        (placement) =>
          placement.lots === lots && placement.gapFactor === actualGapFactor
      )
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
    }

    newRowTmpl.downBtn.onclick = () => {
      const placements = getPlacementsList(isBuy)
      const index = placements.findIndex(
        (placement) =>
          placement.lots === lots && placement.gapFactor === actualGapFactor
      )
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
    }

    tableBody.insertBefore(newRow, addPlacementRow)
    updateArrowVis()
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
  }

  /*
   * setupMMConfigSettings sets up the controls for the settings defined in
   * the market making config.
   */
  setupMMConfigSettings (running: boolean) {
    const page = this.page

    // Gap Strategy
    page.gapStrategySelect.onchange = () => {
      if (!page.gapStrategySelect.value) return
      this.updatedConfig.marketMakingConfig.gapStrategy = page.gapStrategySelect.value
      while (page.buyPlacementsTableBody.children.length > 1) {
        page.buyPlacementsTableBody.children[0].remove()
      }
      while (page.sellPlacementsTableBody.children.length > 1) {
        page.sellPlacementsTableBody.children[0].remove()
      }
      this.updatedConfig.marketMakingConfig.buyPlacements = []
      this.updatedConfig.marketMakingConfig.sellPlacements = []
      this.setGapFactorLabels(page.gapStrategySelect.value)
      this.updateModifiedMarkers()
    }
    if (running) {
      page.gapStrategySelect.setAttribute('disabled', 'true')
    }

    // Buy/Sell placements
    page.addBuyPlacementBtn.onclick = () => {
      this.addPlacement(true, null, false)
      page.addBuyPlacementLots.value = ''
      page.addBuyPlacementGapFactor.value = ''
      this.updateModifiedMarkers()
    }
    page.addSellPlacementBtn.onclick = () => {
      this.addPlacement(false, null, false)
      page.addSellPlacementLots.value = ''
      page.addSellPlacementGapFactor.value = ''
      this.updateModifiedMarkers()
    }
    Doc.setVis(!running, page.addBuyPlacementRow, page.addSellPlacementRow)

    // Drift tolerance
    const updatedDriftTolerance = (x: number) => {
      this.updatedConfig.marketMakingConfig.driftTolerance = x
    }
    const changed = () => {
      this.updateModifiedMarkers()
    }
    const doNothing = () => {
      /* do nothing */
    }
    const currDriftTolerance = this.updatedConfig.marketMakingConfig.driftTolerance
    this.driftToleranceRangeHandler = new XYRangeHandler(
      driftToleranceRange,
      currDriftTolerance,
      updatedDriftTolerance,
      changed,
      doNothing,
      false,
      false,
      running
    )
    page.driftToleranceContainer.appendChild(
      this.driftToleranceRangeHandler.control
    )

    // User oracle
    page.useOracleCheckbox.onchange = () => {
      if (page.useOracleCheckbox.checked) {
        Doc.show(page.oracleBiasSection, page.oracleWeightingSection)
        this.updatedConfig.marketMakingConfig.oracleWeighting = defaultMarketMakingConfig.oracleWeighting
        this.updatedConfig.marketMakingConfig.oracleBias = defaultMarketMakingConfig.oracleBias
        this.oracleWeightingRangeHandler.setValue(defaultMarketMakingConfig.oracleWeighting)
        this.oracleBiasRangeHandler.setValue(defaultMarketMakingConfig.oracleBias)
      } else {
        Doc.hide(page.oracleBiasSection, page.oracleWeightingSection)
        this.updatedConfig.marketMakingConfig.oracleWeighting = 0
        this.updatedConfig.marketMakingConfig.oracleBias = 0
      }
      this.updateModifiedMarkers()
    }
    if (running) {
      page.useOracleCheckbox.setAttribute('disabled', 'true')
    }

    // Oracle Bias
    const currOracleBias = this.originalConfig.marketMakingConfig.oracleBias
    const updatedOracleBias = (x: number) => {
      this.updatedConfig.marketMakingConfig.oracleBias = x
    }
    this.oracleBiasRangeHandler = new XYRangeHandler(
      oracleBiasRange,
      currOracleBias,
      updatedOracleBias,
      changed,
      doNothing,
      false,
      false,
      running
    )
    page.oracleBiasContainer.appendChild(this.oracleBiasRangeHandler.control)

    // Oracle Weighting
    const currOracleWeighting = this.originalConfig.marketMakingConfig.oracleWeighting
    const updatedOracleWeighting = (x: number) => {
      this.updatedConfig.marketMakingConfig.oracleWeighting = x
    }
    this.oracleWeightingRangeHandler = new XYRangeHandler(
      oracleWeightRange,
      currOracleWeighting,
      updatedOracleWeighting,
      changed,
      doNothing,
      false,
      false,
      running
    )
    page.oracleWeightingContainer.appendChild(
      this.oracleWeightingRangeHandler.control
    )

    // Empty Market Rate
    page.emptyMarketRateCheckbox.onchange = () => {
      if (page.emptyMarketRateCheckbox.checked) {
        this.updatedConfig.marketMakingConfig.emptyMarketRate = 0
        page.emptyMarketRateInput.value = '0'
        Doc.show(page.emptyMarketRateInput)
        this.updateModifiedMarkers()
      } else {
        console.log(JSON.stringify(this.updatedConfig.marketMakingConfig))
        this.updatedConfig.marketMakingConfig.emptyMarketRate = undefined
        Doc.hide(page.emptyMarketRateInput)
        this.updateModifiedMarkers()
      }
    }
    page.emptyMarketRateInput.onchange = () => {
      const emptyMarketRate = parseFloat(
        page.emptyMarketRateInput.value || '0'
      )
      this.updatedConfig.marketMakingConfig.emptyMarketRate = emptyMarketRate
      this.updateModifiedMarkers()
    }
    if (running) {
      page.emptyMarketRateCheckbox.setAttribute('disabled', 'true')
      page.emptyMarketRateInput.setAttribute('disabled', 'true')
    }
  }

  /*
   * setOriginalValues sets the updatedConfig field to be equal to the
   * and sets the values displayed buy each field input to be equal
   * to the values since the last save.
   */
  setOriginalValues (running: boolean) {
    const page = this.page
    this.updatedConfig = JSON.parse(JSON.stringify(this.originalConfig))

    // Gap strategy
    if (!page.gapStrategySelect.options) return
    Array.from(page.gapStrategySelect.options).forEach(
      (opt: HTMLOptionElement) => {
        if (opt.value === this.originalConfig.marketMakingConfig.gapStrategy) {
          opt.selected = true
        }
      }
    )
    this.setGapFactorLabels(this.originalConfig.marketMakingConfig.gapStrategy)

    // Buy/Sell placements
    while (page.buyPlacementsTableBody.children.length > 1) {
      page.buyPlacementsTableBody.children[0].remove()
    }
    while (page.sellPlacementsTableBody.children.length > 1) {
      page.sellPlacementsTableBody.children[0].remove()
    }
    this.originalConfig.marketMakingConfig.buyPlacements.forEach((placement) => {
      this.addPlacement(true, placement, running)
    })
    this.originalConfig.marketMakingConfig.sellPlacements.forEach((placement) => {
      this.addPlacement(false, placement, running)
    })

    // Empty market rate
    page.emptyMarketRateCheckbox.checked =
    this.originalConfig.marketMakingConfig.emptyMarketRate !== undefined
    Doc.setVis(
      !!page.emptyMarketRateCheckbox.checked,
      page.emptyMarketRateInput
    )
    page.emptyMarketRateInput.value = `${
      this.originalConfig.marketMakingConfig.emptyMarketRate || 0
    }`

    // Use oracles
    if (this.originalConfig.marketMakingConfig.oracleWeighting === 0) {
      page.useOracleCheckbox.checked = false
      Doc.hide(page.oracleBiasSection, page.oracleWeightingSection)
    }

    // Oracle bias
    this.oracleBiasRangeHandler.setValue(this.originalConfig.marketMakingConfig.oracleBias)

    // Oracle weight
    this.oracleWeightingRangeHandler.setValue(this.originalConfig.marketMakingConfig.oracleWeighting)

    // Drift tolerance
    this.driftToleranceRangeHandler.setValue(this.originalConfig.marketMakingConfig.driftTolerance)

    // Base balance
    if (this.baseBalanceRangeHandler) {
      this.baseBalanceRangeHandler.setValue(this.originalConfig.baseBalance)
    }

    // Quote balance
    if (this.quoteBalanceRangeHandler) {
      this.quoteBalanceRangeHandler.setValue(this.originalConfig.quoteBalance)
    }

    // Base wallet options
    if (this.updatedConfig.marketMakingConfig.baseOptions && this.originalConfig.marketMakingConfig.baseOptions) {
      for (const opt of Object.keys(this.updatedConfig.marketMakingConfig.baseOptions)) {
        const value = this.originalConfig.marketMakingConfig.baseOptions[opt]
        this.updatedConfig.marketMakingConfig.baseOptions[opt] = value
        if (this.baseWalletSettingControl[opt]) {
          this.baseWalletSettingControl[opt].setValue(value)
        }
      }
    }

    // Quote wallet options
    if (this.updatedConfig.marketMakingConfig.quoteOptions && this.originalConfig.marketMakingConfig.quoteOptions) {
      for (const opt of Object.keys(this.updatedConfig.marketMakingConfig.quoteOptions)) {
        const value = this.originalConfig.marketMakingConfig.quoteOptions[opt]
        this.updatedConfig.marketMakingConfig.quoteOptions[opt] = value
        if (this.quoteWalletSettingControl[opt]) {
          this.quoteWalletSettingControl[opt].setValue(value)
        }
      }
    }

    this.updateModifiedMarkers()
  }

  /*
   * saveSettings updates the settings in the backend, and sets the originalConfig
   * to be equal to the updatedConfig.
   */
  async saveSettings () {
    await app().updateMarketMakingConfig(this.updatedConfig)
    this.originalConfig = JSON.parse(JSON.stringify(this.updatedConfig))
    this.updateModifiedMarkers()
  }

  /*
   * setupBalanceSelectors sets up the balance selection sections. If an asset
   * has no balance available, or of other market makers have claimed the entire
   * balance, a message communicating this is displayed.
   */
  setupBalanceSelectors (allConfigs: BotConfig[], running: boolean) {
    const page = this.page

    const baseAsset = app().assets[this.updatedConfig.baseAsset]
    const quoteAsset = app().assets[this.updatedConfig.quoteAsset]
    const availableBaseBalance = baseAsset.wallet.balance.available
    const availableQuoteBalance = quoteAsset.wallet.balance.available

    let baseReservedByOtherBots = 0
    let quoteReservedByOtherBots = 0
    allConfigs.forEach((market) => {
      if (market.baseAsset === this.updatedConfig.baseAsset && market.quoteAsset === this.updatedConfig.quoteAsset &&
          market.host === this.updatedConfig.host) {
        return
      }
      if (market.baseAsset === this.updatedConfig.baseAsset) {
        baseReservedByOtherBots += market.baseBalance
      }
      if (market.quoteAsset === this.updatedConfig.baseAsset) {
        baseReservedByOtherBots += market.quoteBalance
      }
      if (market.baseAsset === this.updatedConfig.quoteAsset) {
        quoteReservedByOtherBots += market.baseBalance
      }
      if (market.quoteAsset === this.updatedConfig.quoteAsset) {
        quoteReservedByOtherBots += market.quoteBalance
      }
    })

    let baseMaxPercent = 0
    let quoteMaxPercent = 0
    if (baseReservedByOtherBots < 100) {
      baseMaxPercent = 100 - baseReservedByOtherBots
    }
    if (quoteReservedByOtherBots < 100) {
      quoteMaxPercent = 100 - quoteReservedByOtherBots
    }

    const baseMaxAvailable = Doc.conventionalCoinValue(
      (availableBaseBalance * baseMaxPercent) / 100,
      baseAsset.unitInfo
    )
    const quoteMaxAvailable = Doc.conventionalCoinValue(
      (availableQuoteBalance * quoteMaxPercent) / 100,
      quoteAsset.unitInfo
    )

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
      yUnit: baseAsset.symbol
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
      yUnit: quoteAsset.symbol
    }

    Doc.hide(
      page.noBaseBalance,
      page.noQuoteBalance,
      page.baseBalanceContainer,
      page.quoteBalanceContainer
    )
    Doc.empty(page.baseBalanceContainer, page.quoteBalanceContainer)

    if (baseMaxAvailable > 0) {
      const updatedBase = (x: number) => {
        this.updatedConfig.baseBalance = x
        this.updateModifiedMarkers()
      }
      const currBase = this.originalConfig.baseBalance
      const baseRangeHandler = new XYRangeHandler(
        baseXYRange,
        currBase,
        updatedBase,
        () => { /* do nothing */ },
        () => { /* do nothing */ },
        false,
        true,
        running
      )
      page.baseBalanceContainer.appendChild(baseRangeHandler.control)
      this.baseBalanceRangeHandler = baseRangeHandler
      Doc.show(page.baseBalanceContainer)
    } else {
      Doc.show(page.noBaseBalance)
    }

    if (quoteMaxAvailable > 0) {
      const updatedQuote = (x: number) => {
        this.updatedConfig.quoteBalance = x
        this.updateModifiedMarkers()
      }
      const currQuote = this.originalConfig.quoteBalance
      const quoteRangeHandler = new XYRangeHandler(
        quoteXYRange,
        currQuote,
        updatedQuote,
        () => { /* do nothing */ },
        () => { /* do nothing */ },
        false,
        true,
        running
      )
      page.quoteBalanceContainer.appendChild(quoteRangeHandler.control)
      this.quoteBalanceRangeHandler = quoteRangeHandler
      Doc.show(page.quoteBalanceContainer)
    } else {
      Doc.show(page.noQuoteBalance)
    }
  }

  /*
    setupWalletSetting sets up the base and quote wallet setting sections.
    These are based on the multi funding settings in the wallet definition.
  */
  setupWalletSettings (running: boolean) {
    const page = this.page
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
      if (quote) {
        if (!this.updatedConfig.marketMakingConfig.quoteOptions) return
        this.updatedConfig.marketMakingConfig.quoteOptions[key] = value
      } else {
        if (!this.updatedConfig.marketMakingConfig.baseOptions) return
        this.updatedConfig.marketMakingConfig.baseOptions[key] = value
      }
    }
    const getWalletOption = (quote: boolean, key: string) : string | undefined => {
      if (quote) {
        if (!this.updatedConfig.marketMakingConfig.quoteOptions) return
        return this.updatedConfig.marketMakingConfig.quoteOptions[key]
      } else {
        if (!this.updatedConfig.marketMakingConfig.baseOptions) return
        return this.updatedConfig.marketMakingConfig.baseOptions[key]
      }
    }
    const addOpt = (opt: ConfigOption, quote: boolean) => {
      let currVal
      let container
      if (quote) {
        if (!this.updatedConfig.marketMakingConfig.quoteOptions) return
        currVal = this.updatedConfig.marketMakingConfig.quoteOptions[opt.key]
        container = page.quoteWalletSettingsContainer
      } else {
        if (opt.quoteAssetOnly) return
        if (!this.updatedConfig.marketMakingConfig.baseOptions) return
        currVal = this.updatedConfig.marketMakingConfig.baseOptions[opt.key]
        container = page.baseWalletSettingsContainer
      }
      let setting : PageElement | undefined
      if (opt.isboolean) {
        setting = page.boolSettingTmpl.cloneNode(true) as PageElement
        const tmpl = Doc.parseTemplate(setting)
        tmpl.name.textContent = opt.displayname
        tmpl.input.checked = currVal === 'true'
        if (running) tmpl.input.setAttribute('disabled', 'true')
        tmpl.input.onchange = () => {
          setWalletOption(quote, opt.key, tmpl.input.checked ? 'true' : 'false')
          setDependentOptsVis(opt.key, !!tmpl.input.checked, quote)
          this.updateModifiedMarkers()
        }
        const setValue = (x: string) => {
          tmpl.input.checked = x === 'true'
          setDependentOptsVis(opt.key, !!tmpl.input.checked, quote)
        }
        storeWalletSettingControl(opt.key, tmpl.input, setValue, quote)
        if (opt.description) tmpl.tooltip.dataset.tooltip = opt.description
        container.appendChild(setting)
      } else if (opt.range) {
        setting = page.rangeSettingTmpl.cloneNode(true) as PageElement
        const tmpl = Doc.parseTemplate(setting)
        tmpl.name.textContent = opt.displayname
        if (opt.description) tmpl.tooltip.dataset.tooltip = opt.description
        const currValNum = parseInt(currVal)
        const handler = new XYRangeHandler(
          opt.range,
          currValNum,
          (x: number) => { setWalletOption(quote, opt.key, `${x}`) },
          () => { this.updateModifiedMarkers() },
          () => { /* do nothing */ },
          opt.range.roundX,
          opt.range.roundY,
          running
        )
        const setValue = (x: string) => {
          handler.setValue(parseInt(x))
        }
        storeWalletSettingControl(opt.key, tmpl.sliderContainer, setValue, quote)
        tmpl.sliderContainer.appendChild(handler.control)
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
    const page = this.page
    const { baseAsset, quoteAsset } = this.originalConfig

    const res = await postJSON('/api/marketreport', { baseID: baseAsset, quoteID: quoteAsset })
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

    page.baseFiatRateSymbol.textContent = app().assets[baseAsset].symbol.toUpperCase()
    page.baseFiatRateLogo.src = Doc.logoPathFromID(baseAsset)
    if (r.baseFiatRate > 0) {
      page.baseFiatRate.textContent = Doc.formatFourSigFigs(r.baseFiatRate)
    } else {
      page.baseFiatRate.textContent = 'N/A'
    }

    page.quoteFiatRateSymbol.textContent = app().assets[quoteAsset].symbol.toUpperCase()
    page.quoteFiatRateLogo.src = Doc.logoPathFromID(quoteAsset)
    if (r.quoteFiatRate > 0) {
      page.quoteFiatRate.textContent = Doc.formatFourSigFigs(r.quoteFiatRate)
    } else {
      page.quoteFiatRate.textContent = 'N/A'
    }
    Doc.show(page.fiatRates)
  }
}

const ExchangeNames: Record<string, string> = {
  'binance.com': 'Binance',
  'coinbase.com': 'Coinbase',
  'bittrex.com': 'Bittrex',
  'hitbtc.com': 'HitBTC',
  'exmo.com': 'EXMO'
}
