import React from 'react'
import { useBotConfigState } from '../utils/BotConfig'
import QuickAllocationView from './QuickAllocation'
import ManualAllocationView from './ManualAllocation'

const BotAllocationsTab: React.FC = () => {
  const { botConfig } = useBotConfigState()

  return (
    <div>
      {botConfig.uiConfig.usingQuickBalance
        ? <QuickAllocationView />
        : <ManualAllocationView />
      }
    </div>
  )
}

export default BotAllocationsTab
