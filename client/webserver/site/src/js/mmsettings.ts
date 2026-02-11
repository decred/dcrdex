import BasePage from './basepage'
import React from 'react'
import { createRoot, Root } from 'react-dom/client'
import MMSettings, { cexSupportsArbOnMarket, MMSettingsHandle } from './mmsettings/components/MMSettings'
import { app, BalanceNote, CEXNotification, MMCEXStatus, CEXBalanceUpdate } from './registry'
import { CEXDisplayInfos, liveBotConfig } from './mmutil'
import type { AvailableMarkets, AvailableMarket } from './mmsettings/components/MMSettings'
import { initialBotConfigState, botConfigStateFromSavedConfig, BotConfigState } from './mmsettings/utils/BotConfig'
import State from './state'

export interface BotSpecs {
  host: string
  baseID: number
  quoteID: number
  botType: 'basicMM' | 'arbMM' | 'basicArb'
  cexName?: string
}

export const specLK = 'lastMMSpecs'

export default class MarketMakerSettingsPage extends BasePage {
  private reactRoot: Root | null = null
  private mainElement: HTMLElement
  private mmSettingsRef: React.RefObject<MMSettingsHandle>

  constructor (main: HTMLElement, specs?: BotSpecs) {
    super()
    this.mainElement = main
    this.mmSettingsRef = React.createRef<MMSettingsHandle>()
    this.renderReact(specs)

    app().registerNoteFeeder({
      balance: (note: BalanceNote) => { this.handleBalanceNote(note) },
      cexnote: (note: CEXNotification) => { this.handleCEXNote(note) }
    })
  }

  private initializeMarketRows (bridgePaths: Record<number, Record<number, string[]>>): AvailableMarkets {
    const markets: AvailableMarket[] = []
    const exchangesRequiringRegistration: string[] = []
    const cexStatuses = app().mmStatus.cexes

    for (const [host, exchange] of Object.entries(app().exchanges)) {
      const { markets: exchangeMarkets, auth: { effectiveTier, pendingStrength } } = exchange

      // Check if user has sufficient tier for this exchange
      if (effectiveTier + pendingStrength === 0) {
        exchangesRequiringRegistration.push(host)
        continue // Skip exchanges where user needs to register
      }

      // Process each market in this exchange
      for (const [marketName, market] of Object.entries(exchangeMarkets)) {
        const { baseid: baseID, quoteid: quoteID, basesymbol: baseSymbol, quotesymbol: quoteSymbol, spot } = market

        // Skip markets with missing assets
        if (!app().assets[baseID] || !app().assets[quoteID]) continue

        // Check arbitrage support
        const arbs: string[] = []

        for (const cexName of Object.keys(CEXDisplayInfos)) {
          const cexStatus = cexStatuses[cexName]
          if (!cexStatus) continue
          const [supportsDirectArb, intermediateAssets] = cexSupportsArbOnMarket(baseID, quoteID, cexStatus, bridgePaths)
          if (supportsDirectArb || (intermediateAssets && intermediateAssets.length > 0)) {
            arbs.push(cexName)
          }
        }

        const availableMarket: AvailableMarket = {
          host,
          name: marketName,
          baseID,
          quoteID,
          baseSymbol,
          quoteSymbol,
          hasArb: arbs.length > 0,
          arbs,
          spot
        }

        markets.push(availableMarket)
      }
    }

    // Sort markets by volume (simplified - would need fiat rates in real implementation)
    const fiatRates = app().fiatRatesMap
    markets.sort((a: AvailableMarket, b: AvailableMarket) => {
      let [volA, volB] = [a.spot?.vol24 ?? 0, b.spot?.vol24 ?? 0]
      if (fiatRates[a.baseID] && fiatRates[b.baseID]) {
        volA *= fiatRates[a.baseID]
        volB *= fiatRates[b.baseID]
      }
      return volB - volA
    })

    return {
      markets,
      exchangesRequiringRegistration
    }
  }

  unload () {
    if (this.reactRoot) {
      this.reactRoot.unmount()
      this.reactRoot = null
    }
    super.unload()
  }

  handleBalanceNote (note: BalanceNote) {
    if (this.mmSettingsRef.current) {
      this.mmSettingsRef.current.handleBalanceNote(note)
    }
  }

  handleCEXNote (note: CEXNotification) {
    if (!this.mmSettingsRef.current) return

    // Handle different CEX note topics
    if (note.topic === 'BalanceUpdate') {
      const update = note.note as CEXBalanceUpdate
      this.mmSettingsRef.current.handleCEXBalanceUpdate(note.cexName, update)
    }
  }

  private async renderReact (specs?: BotSpecs) {
    while (this.mainElement.firstChild) {
      this.mainElement.removeChild(this.mainElement.firstChild)
    }

    // Create a container div for React
    const reactContainer = document.createElement('div')
    reactContainer.id = 'react-mmsettings-container'
    this.mainElement.appendChild(reactContainer)

    // If this is a refresh, load the latest config that was being edited.
    const isRefresh = specs && Object.keys(specs).length === 0
    if (isRefresh) specs = State.fetchLocal(specLK)
    if (specs) State.storeLocal(specLK, specs)

    // If this is a refresh, or we are loading a saved config, get the initial state
    // of the react component.
    const bridgePaths = await app().allBridgePaths()
    let botConfigStateOnLoad: BotConfigState | string | undefined
    let intermediateAssets: number[] | null = null
    let cexStatus: MMCEXStatus | null = null

    if (specs) {
      let baseBridges: Record<number, string[]> | null = null
      let quoteBridges: Record<number, string[]> | null = null

      if (specs.cexName) {
        let supportsDirectArb: boolean
        cexStatus = app().mmStatus.cexes[specs.cexName] ?? null;
        [supportsDirectArb, intermediateAssets, baseBridges, quoteBridges] = cexSupportsArbOnMarket(
          specs.baseID,
          specs.quoteID,
          cexStatus,
          bridgePaths)
        if (!supportsDirectArb && (!intermediateAssets || intermediateAssets.length === 0)) {
          botConfigStateOnLoad = `CEX does not support arb on market: ${specs.cexName} ${specs.baseID} ${specs.quoteID}`
        }
      }

      if (typeof botConfigStateOnLoad !== 'string') {
        const savedBotConfig = liveBotConfig(specs.host, specs.baseID, specs.quoteID)
        if (savedBotConfig) {
          botConfigStateOnLoad = await botConfigStateFromSavedConfig(
            savedBotConfig, cexStatus, intermediateAssets, baseBridges, quoteBridges)
        } else {
          botConfigStateOnLoad = await initialBotConfigState(specs.host,
            specs.baseID, specs.quoteID, specs.botType, intermediateAssets,
            baseBridges, quoteBridges, cexStatus, specs.cexName)
        }
      }
    }

    // Create React root and render component with market data
    this.reactRoot = createRoot(reactContainer)
    this.reactRoot.render(React.createElement(MMSettings, {
      ref: this.mmSettingsRef,
      availableMarkets: this.initializeMarketRows(bridgePaths),
      initialCexes: app().mmStatus.cexes,
      bridgePaths: bridgePaths,
      botConfigStateOnLoad
    }))
  }
}
