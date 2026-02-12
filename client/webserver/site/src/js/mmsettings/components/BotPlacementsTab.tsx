import React from 'react'
import { useBotConfigState } from '../utils/BotConfig'
import { AdvancedPlacements } from './AdvancedPlacements'
import { QuickPlacements } from './QuickPlacements'

const BotPlacementsTab: React.FC = () => {
  const { quickPlacements } = useBotConfigState()

  return (
    <div>
      {quickPlacements
        ? <QuickPlacements />
        : <AdvancedPlacements />
      }
    </div>
  )
}

export default BotPlacementsTab
