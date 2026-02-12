import React, { useState, useEffect } from 'react'
import Doc from '../../doc'
import { app } from '../../registry'
import { MM } from '../../mmutil'
import { useBotConfigState } from '../utils/BotConfig'
import { renderSymbol, useMMSettingsSetError } from './MMSettings'
import BotPlacementsTab from './BotPlacementsTab'
import BotAllocationsTab from './BotAllocationsTab'
import BotSettingsTab from './BotSettingsTab'
import RebalanceSettingsTab from './RebalanceSettingsTab'
import Popup from './Popup'
import { useBootstrapBreakpoints } from '../hooks/PageSizeBreakpoints'
import {
  prep,
  ID_MM_START_BOT,
  ID_MM_SAVE_SETTINGS,
  ID_MM_DELETE_BOT,
  ID_MM_UPDATE_RUNNING_BOT,
  ID_MM_CONFIRM_DELETE,
  ID_MM_CANCEL,
  ID_MM_DELETE,
  ID_MM_PLACEMENTS,
  ID_MM_ALLOCATIONS,
  ID_MM_SETTINGS,
  ID_MM_REBALANCE_SETTINGS,
  ID_MM_BASIC_MARKET_MAKER,
  ID_MM_MM_PLUS_ARB,
  ID_MM_BASIC_ARBITRAGE,
  ID_MM_UNKNOWN,
  ID_MM_FAILED_SAVE_BOT_CONFIG,
  ID_MM_FAILED_START_BOT
} from '../../locales'

// Market Button component
const MarketButton: React.FC<{ onChangeMarket?: () => void }> = ({
  onChangeMarket
}) => {
  const botConfigState = useBotConfigState()
  const mkt = botConfigState.dexMarket

  return (
    <div className="configure-bot-market-display mb-3 hoverbg pointer" onClick={onChangeMarket}>
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
  alignRight?: boolean
}> = ({
  onChangeBotType,
  alignRight = false
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
    <div className="configure-bot-bot-type-display mb-3 hoverbg pointer" onClick={onChangeBotType}>
      <div className={`d-flex lh1 pb-1 ${alignRight ? 'align-items-end' : 'align-items-start'}`}>
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
        message: `Failed to delete bot: ${error}`
      })
    }
  }

  const cancelDeleteBot = () => {
    setShowDeleteConfirmation(false)
  }

  const handleUpdateRunningBot = async () => {
    try {
      await MM.updateRunningBot(botConfigState.botConfig, botConfigState.botConfig.alloc || { dex: {}, cex: {} }, botConfigState.botConfig.autoRebalance)
      await app().fetchMMStatus()
      app().loadPage('mm')
    } catch (error) {
      setError({
        message: `Failed to update running bot: ${error}`
      })
    }
  }

  let containerClass = 'd-flex flex-row gap-2 p-2 mb-1'
  let buttonClass = 'm-2 flex-fill'
  if (layout === 'column') {
    buttonClass = 'my-1 w-100'
    containerClass = 'd-flex flex-column gap-2 p-2 mb-3'
  }

  if (botConfigState.runStats) {
    return (
      <div className={containerClass}>
        <button className={`btn btn-outline-primary go ${buttonClass}`} onClick={handleUpdateRunningBot}>
          {prep(ID_MM_UPDATE_RUNNING_BOT)}
        </button>
      </div>
    )
  }

  return (
    <>
      <div className={containerClass}>
        <button className={`btn btn-outline-primary go ${buttonClass}`} onClick={handleStart}>
          {prep(ID_MM_START_BOT)} <span className="ico-arrowright ms-1"></span>
        </button>
        <button className={`btn btn-primary ${buttonClass}`} onClick={handleSaveSettings}>
          {prep(ID_MM_SAVE_SETTINGS)}
        </button>
        <button className={`btn btn-primary danger ${buttonClass}`} onClick={handleDeleteBotClick}>
          {prep(ID_MM_DELETE_BOT)}
        </button>
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
  layout?: 'column' | 'row'
}

const BotTabNavigation: React.FC<BotTabNavigationProps> = ({
  activeTab,
  onTabChange,
  layout = 'column'
}) => {
  const cfg = useBotConfigState().botConfig
  const flexDirection = layout === 'row' ? 'flex-row' : 'flex-column'
  const spacing = layout === 'row' ? 'me-3' : 'mb-2'

  return (
    <div className="p-2">
      <div className={`d-flex ${flexDirection}`}>
        {!cfg.simpleArbConfig && (
          <div
            className={`configure-bot-tab-section ${activeTab === 'placements' ? 'active' : ''} ${spacing}`}
            onClick={() => onTabChange('placements')}
          >
            <div className="fs16 fw-semibold">{prep(ID_MM_PLACEMENTS)}</div>
          </div>
        )}
        <div
          className={`configure-bot-tab-section ${activeTab === 'allocations' ? 'active' : ''} ${spacing}`}
          onClick={() => onTabChange('allocations')}
        >
          <div className="fs16 fw-semibold">{prep(ID_MM_ALLOCATIONS)}</div>
        </div>
        <div
          className={`configure-bot-tab-section ${activeTab === 'settings' ? 'active' : ''} ${spacing}`}
          onClick={() => onTabChange('settings')}
        >
          <div className="fs16 fw-semibold">{prep(ID_MM_SETTINGS)}</div>
        </div>
        { cfg.cexName && <div
          className={`configure-bot-tab-section ${activeTab === 'rebalanceSettings' ? 'active' : ''} ${spacing}`}
          onClick={() => onTabChange('rebalanceSettings')}
        >
          <div className="fs16 fw-semibold">{prep(ID_MM_REBALANCE_SETTINGS)}</div>
        </div>}
      </div>
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
  const [availableHeight, setAvailableHeight] = useState<number>(0)
  const pageSize = useBootstrapBreakpoints(['lg', 'xl'])

  // Calculate available height dynamically and prevent page scrolling
  useEffect(() => {
    // Store original overflow values
    const originalHtmlOverflow = document.documentElement.style.overflow
    const originalBodyOverflow = document.body.style.overflow

    // Prevent page scrolling
    document.documentElement.style.overflow = 'hidden'
    document.body.style.overflow = 'hidden'

    const calculateHeight = () => {
      // Get viewport height
      const viewportHeight = window.innerHeight

      // Account for top/bottom margins (5% each) and leave some buffer
      const topMargin = viewportHeight * 0.05
      const bottomMargin = viewportHeight * 0.05
      const buffer = 20 // Small buffer for any additional spacing

      const available = viewportHeight - topMargin - bottomMargin - buffer
      setAvailableHeight(Math.max(available, 400)) // Minimum height of 400px
    }

    // Calculate initial height
    calculateHeight()

    // Add resize listener
    window.addEventListener('resize', calculateHeight)

    // Cleanup function
    return () => {
      window.removeEventListener('resize', calculateHeight)
      document.documentElement.style.overflow = originalHtmlOverflow
      document.body.style.overflow = originalBodyOverflow
    }
  }, [])

  const handleTabChange = (tab: 'placements' | 'allocations' | 'settings' | 'rebalanceSettings') => {
    setActiveTab(tab)
  }

  const currentTabContent = () => {
    if (activeTab === 'placements') return <BotPlacementsTab />
    if (activeTab === 'allocations') return <BotAllocationsTab />
    if (activeTab === 'settings') return <BotSettingsTab />
    if (activeTab === 'rebalanceSettings') return <RebalanceSettingsTab />
  }

  // Check if screen is large or larger (lg or xl)
  const isLargeScreen = pageSize === 'lg' || pageSize === 'xl'

  if (isLargeScreen) {
    return (
      <div style={{
        marginLeft: '5%',
        marginRight: '5%',
        height: availableHeight > 0 ? `${availableHeight}px` : '100vh'
      }}>
        <div className="d-flex align-items-start h-100">

          {/* LEFT PANEL */}
          <section
            className="flex-shrink-0 py-2"
            style={{
              width: '330px',
              maxHeight: '100%',
              overflowY: 'auto'
            }}
          >
            <MarketButton onChangeMarket={onChangeMarket} />
            <BotTypeButton onChangeBotType={onChangeBotType} />
            <BotActionButtons />
            <BotTabNavigation
              activeTab={activeTab}
              onTabChange={handleTabChange}
            />
          </section>

          {/* RIGHT PANEL */}
          <section
            className="p-3 w-100"
            style={{
              maxHeight: '100%',
              overflowY: 'auto'
            }}
          >
            { currentTabContent() }
          </section>
        </div>
      </div>
    )
  }

  // Small screen layout - stacked vertically
  return (
    <div className="flex-row" style={{ marginLeft: '5%', marginRight: '5%' }}>
      {/* TOP ROW - Market and Bot Type buttons */}
      <div className="row">
        <MarketButton onChangeMarket={onChangeMarket} />
        <BotTypeButton onChangeBotType={onChangeBotType} alignRight/>
      </div>

      {/* MIDDLE ROW - Action buttons */}
      <section>
        <BotActionButtons
          layout="row"
        />
      </section>

      {/* BOTTOM ROW - Tab navigation */}
      <section>
        <BotTabNavigation
          activeTab={activeTab}
          onTabChange={handleTabChange}
          layout="row"
        />
      </section>

      {/* TAB CONTENT */}
      <section className="p-2">
        { currentTabContent() }
      </section>
    </div>
  )
}

export default ConfigureBot
