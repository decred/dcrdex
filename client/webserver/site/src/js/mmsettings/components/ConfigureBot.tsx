import React, { useState } from 'react'
import Doc from '../../doc'
import { app } from '../../registry'
import { MM } from '../../mmutil'
import { useBotConfigState, buildRunningBotUpdatePayload } from '../utils/BotConfig'
import { renderSymbol, useMMSettingsSetError } from './MMSettings'
import BotPlacementsTab from './BotPlacementsTab'
import BotAllocationsTab from './BotAllocationsTab'
import BotSettingsTab from './BotSettingsTab'
import RebalanceSettingsTab from './RebalanceSettingsTab'
import Popup from './Popup'
import { useBootstrapBreakpoints } from '../hooks/PageSizeBreakpoints'
import {
  prep,
  ID_MM_PLACEMENTS,
  ID_MM_ALLOCATIONS,
  ID_MM_SETTINGS,
  ID_MM_REBALANCE_SETTINGS,
  ID_MM_BASIC_MARKET_MAKER,
  ID_MM_MM_PLUS_ARB,
  ID_MM_BASIC_ARBITRAGE,
  ID_MM_UNKNOWN,
  ID_MM_FAILED_SAVE_BOT_CONFIG,
  ID_MM_FAILED_START_BOT,
  ID_MM_START_BOT,
  ID_MM_SAVE_SETTINGS,
  ID_MM_DELETE_BOT,
  ID_MM_UPDATE_RUNNING_BOT,
  ID_MM_CONFIRM_DELETE,
  ID_MM_CANCEL,
  ID_MM_DELETE,
  ID_MM_FAILED_DELETE_BOT,
  ID_MM_FAILED_UPDATE_RUNNING
} from '../../locales'

// Market Button component
const MarketButton: React.FC<{ onChangeMarket?: () => void }> = ({
  onChangeMarket
}) => {
  const botConfigState = useBotConfigState()
  const mkt = botConfigState.dexMarket

  return (
    <div className="configure-bot-market-display mb-2 hoverbg pointer" onClick={onChangeMarket}>
      <div className="d-flex align-items-center fs20 lh1 pb-1">
        <img className="mini-icon" src={Doc.logoPath(mkt.baseAsset.symbol)} alt={mkt.baseAsset.symbol} />
        <img className="mx-1 mini-icon" src={Doc.logoPath(mkt.quoteAsset.symbol)} alt={mkt.quoteAsset.symbol} />
        {renderSymbol(mkt.baseID, mkt.baseAsset.symbol)}&ndash;{renderSymbol(mkt.quoteID, mkt.quoteAsset.symbol)}
        <span className="ico-edit fs16 ms-2 grey"></span>
      </div>
      <div className="fs14 grey">
        <span className="me-1">@</span>
        <span>{mkt.host}</span>
      </div>
    </div>
  )
}

// Bot Type Button component
const BotTypeButton: React.FC<{
  onChangeBotType: () => void
}> = ({
  onChangeBotType
}) => {
  const botConfigState = useBotConfigState()
  const cfg = botConfigState.botConfig

  // Determine bot type from config
  const getBotType = () => {
    if (cfg.basicMarketMakingConfig) return prep(ID_MM_BASIC_MARKET_MAKER)
    if (cfg.arbMarketMakingConfig) return prep(ID_MM_MM_PLUS_ARB)
    if (cfg.simpleArbConfig) return prep(ID_MM_BASIC_ARBITRAGE)
    return prep(ID_MM_UNKNOWN)
  }

  const botType = getBotType()

  return (
    <div className="configure-bot-bot-type-display mb-2 hoverbg pointer" onClick={onChangeBotType}>
      <div className="d-flex align-items-center lh1 pb-1">
        <div className="fs20">{botType}</div>
        <span className="ico-edit fs16 ms-2 grey"></span>
      </div>
    </div>
  )
}

// BotActionButtons component
const BotActionButtons: React.FC<{
  layout?: 'column' | 'row'
}> = ({
  layout = 'column'
}) => {
  const botConfigState = useBotConfigState()
  const setError = useMMSettingsSetError()
  const [showDeleteConfirmation, setShowDeleteConfirmation] = useState(false)
  const handleSaveSettings = async () => {
    try {
      await MM.updateBotConfig(botConfigState.botConfig)
      await app().fetchMMStatus()
      app().loadPage('mm')
    } catch (error) {
      setError({
        message: prep(ID_MM_FAILED_SAVE_BOT_CONFIG) + `${error}`
      })
    }
  }

  const handleStart = async () => {
    let res = await MM.updateBotConfig(botConfigState.botConfig)
    if (!app().checkResponse(res)) {
      setError({
        message: prep(ID_MM_FAILED_SAVE_BOT_CONFIG) + res.msg
      })
      return
    }

    res = await MM.startBot({
      baseID: botConfigState.dexMarket.baseID,
      quoteID: botConfigState.dexMarket.quoteID,
      host: botConfigState.dexMarket.host
    })
    if (!app().checkResponse(res)) {
      setError({
        message: prep(ID_MM_FAILED_START_BOT) + res.msg
      })
      return
    }

    await app().fetchMMStatus()

    app().loadPage('mm')
  }

  const handleDeleteBotClick = () => {
    setShowDeleteConfirmation(true)
  }

  const confirmDeleteBot = async () => {
    setShowDeleteConfirmation(false)
    try {
      await MM.removeBotConfig(botConfigState.botConfig.host, botConfigState.botConfig.baseID, botConfigState.botConfig.quoteID)
      await app().fetchMMStatus()
      app().loadPage('mm')
    } catch (error) {
      setError({
        message: prep(ID_MM_FAILED_DELETE_BOT) + `${error}`
      })
    }
  }

  const cancelDeleteBot = () => {
    setShowDeleteConfirmation(false)
  }

  const handleUpdateRunningBot = async () => {
    try {
      const runningBotUpdate = buildRunningBotUpdatePayload(botConfigState)
      await MM.updateRunningBot(runningBotUpdate.cfg, runningBotUpdate.diffs, runningBotUpdate.cfg.autoRebalance)
      await app().fetchMMStatus()
      app().loadPage('mm')
    } catch (error) {
      setError({
        message: prep(ID_MM_FAILED_UPDATE_RUNNING) + `${error}`
      })
    }
  }

  if (botConfigState.runStats) {
    return (
      <div className="py-2 mb-1">
        <button className="btn btn-outline-primary go w-100" onClick={handleUpdateRunningBot}>
          {prep(ID_MM_UPDATE_RUNNING_BOT)}
        </button>
      </div>
    )
  }

  const isRow = layout === 'row'

  return (
    <>
      <div className="py-2 mb-1">
        <div className={`mm-action-primary ${isRow ? 'flex-row' : 'flex-column'}`}>
          <button className={`btn btn-outline-primary go ${isRow ? 'flex-fill' : 'w-100 mb-1'}`} onClick={handleStart}>
            {prep(ID_MM_START_BOT)} <span className="ico-arrowright ms-1"></span>
          </button>
          <button className={`btn btn-primary ${isRow ? 'flex-fill' : 'w-100'}`} onClick={handleSaveSettings}>
            {prep(ID_MM_SAVE_SETTINGS)}
          </button>
        </div>
        <div className="mm-action-danger">
          <button className={`btn btn-primary danger ${isRow ? '' : 'w-100'} small`} onClick={handleDeleteBotClick}>
            {prep(ID_MM_DELETE_BOT)}
          </button>
        </div>
      </div>
      {showDeleteConfirmation && (
        <Popup
          message={prep(ID_MM_CONFIRM_DELETE)}
          buttons={[
            { text: prep(ID_MM_CANCEL), onClick: cancelDeleteBot },
            { text: prep(ID_MM_DELETE), onClick: confirmDeleteBot, className: 'danger' }
          ]}
          onClose={cancelDeleteBot}
        />
      )}
    </>
  )
}

// BotTabNavigation component
interface BotTabNavigationProps {
  activeTab: 'placements' | 'allocations' | 'settings' | 'rebalanceSettings'
  onTabChange: (tab: 'placements' | 'allocations' | 'settings' | 'rebalanceSettings') => void
  layout?: 'horizontal' | 'vertical'
}

const BotTabNavigation: React.FC<BotTabNavigationProps> = ({
  activeTab,
  onTabChange,
  layout = 'horizontal'
}) => {
  const cfg = useBotConfigState().botConfig
  const isVertical = layout === 'vertical'
  const navClass = isVertical ? 'mm-tab-nav-vertical' : 'mm-tab-nav'

  return (
    <div className={navClass}>
      {!cfg.simpleArbConfig && (
        <div
          className={`configure-bot-tab-section ${activeTab === 'placements' ? 'active' : ''}`}
          onClick={() => onTabChange('placements')}
        >
          <div className="fs16 fw-semibold">{prep(ID_MM_PLACEMENTS)}</div>
        </div>
      )}
      <div
        className={`configure-bot-tab-section ${activeTab === 'allocations' ? 'active' : ''}`}
        onClick={() => onTabChange('allocations')}
      >
        <div className="fs16 fw-semibold">{prep(ID_MM_ALLOCATIONS)}</div>
      </div>
      <div
        className={`configure-bot-tab-section ${activeTab === 'settings' ? 'active' : ''}`}
        onClick={() => onTabChange('settings')}
      >
        <div className="fs16 fw-semibold">{prep(ID_MM_SETTINGS)}</div>
      </div>
      { cfg.cexName && <div
        className={`configure-bot-tab-section ${activeTab === 'rebalanceSettings' ? 'active' : ''}`}
        onClick={() => onTabChange('rebalanceSettings')}
      >
        <div className="fs16 fw-semibold">{prep(ID_MM_REBALANCE_SETTINGS)}</div>
      </div>}
    </div>
  )
}

interface ConfigureBotProps {
  onChangeMarket: () => void
  onChangeBotType: () => void
}

const ConfigureBot: React.FC<ConfigureBotProps> = ({
  onChangeMarket,
  onChangeBotType
}) => {
  const botConfigState = useBotConfigState()
  const cfg = botConfigState.botConfig
  const initialTab = cfg.simpleArbConfig ? 'settings' : 'placements'
  const [activeTab, setActiveTab] = useState<'placements' | 'allocations' | 'settings' | 'rebalanceSettings'>(initialTab)
  const pageSize = useBootstrapBreakpoints(['md', 'lg', 'xl'])

  const handleTabChange = (tab: 'placements' | 'allocations' | 'settings' | 'rebalanceSettings') => {
    setActiveTab(tab)
  }

  const currentTabContent = () => {
    if (activeTab === 'placements') return <BotPlacementsTab />
    if (activeTab === 'allocations') return <BotAllocationsTab />
    if (activeTab === 'settings') return <BotSettingsTab />
    if (activeTab === 'rebalanceSettings') return <RebalanceSettingsTab />
  }

  const isLargeScreen = pageSize === 'lg' || pageSize === 'xl'
  const isMediumScreen = pageSize === 'md'

  // Large screen: sidebar layout
  if (isLargeScreen) {
    return (
      <div className="mm-settings-container px-4 py-3">
        <div className="d-flex align-items-start">

          {/* LEFT PANEL - Sidebar */}
          <section className="mm-sidebar py-2 me-3">
            <MarketButton onChangeMarket={onChangeMarket} />
            <BotTypeButton onChangeBotType={onChangeBotType} />
            <BotActionButtons />
            <div className="mt-2">
              <BotTabNavigation
                activeTab={activeTab}
                onTabChange={handleTabChange}
                layout="vertical"
              />
            </div>
          </section>

          {/* RIGHT PANEL - Content */}
          <section className="mm-content flex-grow-1 p-3">
            { currentTabContent() }
          </section>
        </div>
      </div>
    )
  }

  // Medium screen: compact header with horizontal tabs
  if (isMediumScreen) {
    return (
      <div className="mm-settings-container px-3 py-3">
        {/* Header row: Market + Bot Type side by side */}
        <div className="mm-header-row mb-2">
          <MarketButton onChangeMarket={onChangeMarket} />
          <BotTypeButton onChangeBotType={onChangeBotType} />
        </div>

        {/* Action buttons - horizontal */}
        <BotActionButtons layout="row" />

        {/* Tab navigation - horizontal */}
        <div className="mb-3">
          <BotTabNavigation
            activeTab={activeTab}
            onTabChange={handleTabChange}
            layout="horizontal"
          />
        </div>

        {/* Tab content */}
        <section>
          { currentTabContent() }
        </section>
      </div>
    )
  }

  // Small screen: stacked layout with scrollable tabs
  return (
    <div className="mm-settings-container px-2 py-2">
      {/* Market + Bot Type stacked */}
      <MarketButton onChangeMarket={onChangeMarket} />
      <BotTypeButton onChangeBotType={onChangeBotType} />

      {/* Action buttons - full width stacked */}
      <BotActionButtons layout="row" />

      {/* Tab navigation - scrollable */}
      <div className="mm-tab-scroll mb-3">
        <BotTabNavigation
          activeTab={activeTab}
          onTabChange={handleTabChange}
          layout="horizontal"
        />
      </div>

      {/* Tab content */}
      <section>
        { currentTabContent() }
      </section>
    </div>
  )
}

export default ConfigureBot
