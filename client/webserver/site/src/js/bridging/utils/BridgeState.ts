import { createContext, useContext } from 'react'
import {
  BridgeFeesAndLimits,
  BridgeApprovalApproved,
  BridgeApprovalPending
} from '../../registry'
import { BridgeTransaction } from '../types'

// State interface - simplified to derive selector options from allBridgePaths
export interface BridgeState {
  // Selection state (all non-nullable after initialization)
  bridgeName: string
  sourceAssetID: number
  destAssetID: number

  // Form state
  amount: string
  approvalStatus: 'approved' | 'pending' | 'notApproved' | 'loading' | 'notRequired'
  feesAndLimits: BridgeFeesAndLimits | null

  // History
  pendingBridges: BridgeTransaction[]
  bridgeHistory: BridgeTransaction[]
  // Per-asset cursor for paging older history.
  bridgeHistoryRefIDByAsset: Record<number, string | null>
  bridgeHistoryDoneByAsset: Record<number, boolean>
  bridgeHistoryPageIndex: number
  bridgeHistoryPageSize: number
  bridgeHistoryLoaded: boolean

  // Bridge topology (immutable after init)
  allBridgePaths: Record<number, Record<number, string[]>>

  // UI state
  loading: boolean
  historyLoading: boolean
  submitting: boolean
  error: string | null
  activeTab: 'bridge' | 'history'

  // Tokens used to trigger refresh/re-render from imperative note handlers.
  balanceRefreshToken: number
}

type BridgePatch = Partial<Pick<
  BridgeState,
  'amount' | 'approvalStatus' | 'feesAndLimits' | 'pendingBridges' | 'bridgeHistory' |
  'submitting' | 'error' | 'activeTab' | 'historyLoading' |
  'bridgeHistoryRefIDByAsset' | 'bridgeHistoryDoneByAsset' | 'bridgeHistoryPageIndex' | 'bridgeHistoryPageSize' | 'bridgeHistoryLoaded'
>>

// Action types
export type BridgeAction =
  | { type: 'INITIALIZE'; paths: Record<number, Record<number, string[]>>; initialBridge: string; initialSource: number; initialDest: number }
  | { type: 'SELECT_BRIDGE'; bridgeName: string }
  | { type: 'SELECT_SOURCE'; sourceAssetID: number }
  | { type: 'SELECT_DESTINATION'; destAssetID: number }
  | { type: 'PATCH'; patch: BridgePatch }
  | { type: 'BUMP_BALANCE_REFRESH' }

// Initial state factory - uses placeholder values until INITIALIZE is called
export function createInitialState (): BridgeState {
  return {
    bridgeName: '',
    sourceAssetID: 0,
    destAssetID: 0,
    amount: '',
    approvalStatus: 'loading',
    feesAndLimits: null,
    pendingBridges: [],
    bridgeHistory: [],
    bridgeHistoryRefIDByAsset: {},
    bridgeHistoryDoneByAsset: {},
    bridgeHistoryPageIndex: 0,
    bridgeHistoryPageSize: 10,
    bridgeHistoryLoaded: false,
    allBridgePaths: {},
    loading: true,
    historyLoading: false,
    submitting: false,
    error: null,
    activeTab: 'bridge',
    balanceRefreshToken: 0
  }
}

export function approvalStatusFromBridgeApproval (status: number): BridgeState['approvalStatus'] {
  if (status === BridgeApprovalApproved) return 'approved'
  if (status === BridgeApprovalPending) return 'pending'
  return 'notApproved'
}

// Helper: Check if a (bridge, source, dest) combination is valid
function isValidCombination (
  paths: Record<number, Record<number, string[]>>,
  bridgeName: string,
  sourceAssetID: number,
  destAssetID: number
): boolean {
  const bridges = paths[sourceAssetID]?.[destAssetID] || []
  return bridges.includes(bridgeName)
}

// Helper: Find a valid (bridge, source) for a destination
function findBridgeAndSourceForDest (
  paths: Record<number, Record<number, string[]>>,
  destAssetID: number,
  preferredBridge?: string,
  preferredSource?: number
): { bridge: string; source: number } | null {
  // First try preferred source with any bridge
  if (preferredSource !== undefined) {
    const bridges = paths[preferredSource]?.[destAssetID] || []
    if (bridges.length > 0) {
      // Prefer the preferred bridge if valid
      if (preferredBridge && bridges.includes(preferredBridge)) {
        return { bridge: preferredBridge, source: preferredSource }
      }
      return { bridge: bridges[0], source: preferredSource }
    }
  }

  // Find any valid (bridge, source) for this dest
  for (const [srcIDStr, dests] of Object.entries(paths)) {
    const srcID = Number(srcIDStr)
    const bridges = dests[destAssetID] || []
    if (bridges.length > 0) {
      if (preferredBridge && bridges.includes(preferredBridge)) {
        return { bridge: preferredBridge, source: srcID }
      }
      return { bridge: bridges[0], source: srcID }
    }
  }
  return null
}

// Helper: Find a valid (bridge, dest) for a source
function findBridgeAndDestForSource (
  paths: Record<number, Record<number, string[]>>,
  sourceAssetID: number,
  preferredBridge?: string,
  preferredDest?: number
): { bridge: string; dest: number } | null {
  const dests = paths[sourceAssetID] || {}

  // First try preferred dest with any bridge
  if (preferredDest !== undefined) {
    const bridges = dests[preferredDest] || []
    if (bridges.length > 0) {
      if (preferredBridge && bridges.includes(preferredBridge)) {
        return { bridge: preferredBridge, dest: preferredDest }
      }
      return { bridge: bridges[0], dest: preferredDest }
    }
  }

  // Find any valid (bridge, dest) for this source
  for (const [destIDStr, bridges] of Object.entries(dests)) {
    if (bridges.length > 0) {
      if (preferredBridge && bridges.includes(preferredBridge)) {
        return { bridge: preferredBridge, dest: Number(destIDStr) }
      }
      return { bridge: bridges[0], dest: Number(destIDStr) }
    }
  }
  return null
}

// Reducer
export function bridgeReducer (state: BridgeState, action: BridgeAction): BridgeState {
  switch (action.type) {
    case 'INITIALIZE': {
      const { paths, initialBridge, initialSource, initialDest } = action
      return {
        ...state,
        allBridgePaths: paths,
        bridgeName: initialBridge,
        sourceAssetID: initialSource,
        destAssetID: initialDest,
        loading: false
      }
    }

    case 'SELECT_BRIDGE': {
      const { bridgeName } = action

      // Check if current (source, dest) works with new bridge
      if (isValidCombination(state.allBridgePaths, bridgeName, state.sourceAssetID, state.destAssetID)) {
        return {
          ...state,
          bridgeName,
          approvalStatus: 'loading',
          feesAndLimits: null
        }
      }

      // Try to keep current source, find valid dest
      const withSource = findBridgeAndDestForSource(
        state.allBridgePaths,
        state.sourceAssetID,
        bridgeName,
        state.destAssetID
      )
      if (withSource && withSource.bridge === bridgeName) {
        return {
          ...state,
          bridgeName,
          destAssetID: withSource.dest,
          amount: '',
          approvalStatus: 'loading',
          feesAndLimits: null
        }
      }

      // Try to keep current dest, find valid source
      const withDest = findBridgeAndSourceForDest(
        state.allBridgePaths,
        state.destAssetID,
        bridgeName,
        state.sourceAssetID
      )
      if (withDest && withDest.bridge === bridgeName) {
        return {
          ...state,
          bridgeName,
          sourceAssetID: withDest.source,
          amount: '',
          approvalStatus: 'loading',
          feesAndLimits: null
        }
      }

      // Find any valid (source, dest) for this bridge
      for (const [srcIDStr, dests] of Object.entries(state.allBridgePaths)) {
        for (const [destIDStr, bridges] of Object.entries(dests)) {
          if (bridges.includes(bridgeName)) {
            return {
              ...state,
              bridgeName,
              sourceAssetID: Number(srcIDStr),
              destAssetID: Number(destIDStr),
              amount: '',
              approvalStatus: 'loading',
              feesAndLimits: null
            }
          }
        }
      }

      // Bridge has no valid paths (shouldn't happen)
      return state
    }

    case 'SELECT_SOURCE': {
      const { sourceAssetID } = action

      // Check if current (bridge, dest) works with new source
      if (isValidCombination(state.allBridgePaths, state.bridgeName, sourceAssetID, state.destAssetID)) {
        return {
          ...state,
          sourceAssetID,
          approvalStatus: 'loading',
          feesAndLimits: null
        }
      }

      // Find valid (bridge, dest) for new source, preferring current values
      const result = findBridgeAndDestForSource(
        state.allBridgePaths,
        sourceAssetID,
        state.bridgeName,
        state.destAssetID
      )
      if (result) {
        return {
          ...state,
          sourceAssetID,
          bridgeName: result.bridge,
          destAssetID: result.dest,
          amount: '',
          approvalStatus: 'loading',
          feesAndLimits: null
        }
      }

      // No valid path from this source (shouldn't happen if source is in allBridgePaths)
      return state
    }

    case 'SELECT_DESTINATION': {
      const { destAssetID } = action

      // Check if current (bridge, source) works with new dest
      if (isValidCombination(state.allBridgePaths, state.bridgeName, state.sourceAssetID, destAssetID)) {
        return {
          ...state,
          destAssetID,
          approvalStatus: 'loading',
          feesAndLimits: null
        }
      }

      // Find valid (bridge, source) for new dest, preferring current values
      const result = findBridgeAndSourceForDest(
        state.allBridgePaths,
        destAssetID,
        state.bridgeName,
        state.sourceAssetID
      )
      if (result) {
        return {
          ...state,
          destAssetID,
          bridgeName: result.bridge,
          sourceAssetID: result.source,
          amount: '',
          approvalStatus: 'loading',
          feesAndLimits: null
        }
      }

      // No valid path to this dest (shouldn't happen if dest is in allBridgePaths)
      return state
    }

    case 'PATCH':
      return { ...state, ...action.patch }

    case 'BUMP_BALANCE_REFRESH':
      return { ...state, balanceRefreshToken: state.balanceRefreshToken + 1 }

    default:
      return state
  }
}

// Context
export const BridgeStateContext = createContext<BridgeState | null>(null)
export const BridgeDispatchContext = createContext<React.Dispatch<BridgeAction> | null>(null)

// Custom hooks
export function useBridgeState (): BridgeState {
  const ctx = useContext(BridgeStateContext)
  if (!ctx) throw new Error('useBridgeState must be used within BridgeStateProvider')
  return ctx
}

export function useBridgeDispatch (): React.Dispatch<BridgeAction> {
  const ctx = useContext(BridgeDispatchContext)
  if (!ctx) throw new Error('useBridgeDispatch must be used within BridgeDispatchProvider')
  return ctx
}
