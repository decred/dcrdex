import React, { useReducer, useRef, useCallback, useEffect, useState, useMemo, forwardRef, useImperativeHandle } from 'react'
import { app, WalletStateNote, BridgeNote, TransactionNote } from '../../registry'
import Doc from '../../doc'
import {
  BridgeStateContext,
  BridgeDispatchContext,
  bridgeReducer,
  createInitialState,
  approvalStatusFromBridgeApproval
} from '../utils/BridgeState'
import BridgeForm from './BridgeForm'
import BridgeHistory from './BridgeHistory'
import BridgeDetails from './BridgeDetails'
import { BridgeTransaction } from '../types'
import * as intl from '../../locales'
import { loadInitialBridgeHistory } from '../utils/bridgeData'

// Handle interface for imperative methods exposed to parent
export interface BridgePopupHandle {
  handleWalletState: (note: WalletStateNote) => void
  handleBridgeUpdate: (note: BridgeNote) => void
  handleTransactionNote: (note: TransactionNote) => void
  handleBalanceUpdate: (assetID: number) => void
}

export interface BridgingPopupProps {
  networkAssetIDs: number[]
  bridgePaths: Record<number, Record<number, string[]>>
  onClose: () => void
}

const BridgingPopup = forwardRef<BridgePopupHandle, BridgingPopupProps>(({ networkAssetIDs, bridgePaths, onClose }, ref) => {
  const [state, dispatch] = useReducer(bridgeReducer, undefined, createInitialState)
  const formRef = useRef<HTMLFormElement>(null)
  const [selectedTxID, setSelectedTxID] = useState<string | null>(null)

  // Derive selected transaction from pending/history lists
  const selectedTx = useMemo(() => {
    if (!selectedTxID) return null
    return state.pendingBridges.find(tx => tx.id === selectedTxID) ||
           state.bridgeHistory.find(tx => tx.id === selectedTxID) ||
           null
  }, [selectedTxID, state.pendingBridges, state.bridgeHistory])

  const headerAsset = !state.loading ? app().assets[state.sourceAssetID] : undefined
  const headerSymbol = (() => {
    if (!headerAsset) return ''
    const unit = headerAsset.unitInfo?.conventional?.unit
    const raw = (unit && unit.length > 0) ? unit : headerAsset.symbol
    return raw.split('.')[0].toUpperCase()
  })()

  // Expose handleWalletState and handleBridgeUpdate to parent via ref
  useImperativeHandle(ref, () => {
    return {
      handleWalletState: async (note: WalletStateNote) => {
        if (note.wallet.assetID !== state.sourceAssetID) return
        if (note.topic !== 'BridgeApproval') return
        if (!state.bridgeName) { return }

        // Re-fetch approval status when wallet state changes
        try {
          const approvalResp = await app().bridgeApprovalStatus(state.sourceAssetID, state.bridgeName)
          if (approvalResp.ok) {
            dispatch({ type: 'PATCH', patch: { approvalStatus: approvalStatusFromBridgeApproval(approvalResp.status) } })
          }
        } catch (e) {
          console.error('Failed to refresh approval status:', e)
        }
      },

      handleBridgeUpdate: (note: BridgeNote) => {
        const applyNoteToTx = (tx: BridgeTransaction): BridgeTransaction => {
          const existingCounterpart = tx.bridgeCounterpartTx || {
            assetID: note.destAssetID,
            ids: [],
            complete: false,
            amountReceived: 0,
            fees: 0
          }
          return {
            ...tx,
            bridgeCounterpartTx: {
              ...existingCounterpart,
              assetID: note.destAssetID,
              ids: note.completionTxIDs,
              amountReceived: note.amount,
              complete: note.complete
            }
          }
        }

        // Update pending/history in-place and automatically move completed pending tx to history.
        const pending = [...state.pendingBridges]
        const history = [...state.bridgeHistory]
        const pIdx = pending.findIndex(tx => tx.id === note.txID)
        const hIdx = history.findIndex(tx => tx.id === note.txID)

        if (pIdx >= 0) {
          const updated = applyNoteToTx(pending[pIdx])
          if (note.complete) {
            pending.splice(pIdx, 1)
            history.unshift(updated)
          } else {
            pending[pIdx] = updated
          }
        } else if (hIdx >= 0) {
          history[hIdx] = applyNoteToTx(history[hIdx])
        }

        pending.sort((a, b) => b.timestamp - a.timestamp)
        history.sort((a, b) => b.timestamp - a.timestamp)
        dispatch({ type: 'PATCH', patch: { pendingBridges: pending, bridgeHistory: history } })
      },

      handleTransactionNote: (note: TransactionNote) => {
        const tx = note.transaction
        if (!tx?.id || !tx.timestamp) return

        // Update any initiation tx in pending/history lists, and keep the lists sorted.
        // (selectedTx is derived from these lists, so no separate update needed)
        const updateTimestamp = (txs: BridgeTransaction[]) => {
          let changed = false
          const updated = txs.map(t => {
            if (t.id !== tx.id) return t
            if (t.timestamp === tx.timestamp) return t
            changed = true
            return { ...t, timestamp: tx.timestamp }
          })
          if (changed) updated.sort((a, b) => b.timestamp - a.timestamp)
          return { updated, changed }
        }

        const p = updateTimestamp(state.pendingBridges)
        const h = updateTimestamp(state.bridgeHistory)
        if (p.changed || h.changed) {
          dispatch({
            type: 'PATCH',
            patch: {
              pendingBridges: p.changed ? p.updated : state.pendingBridges,
              bridgeHistory: h.changed ? h.updated : state.bridgeHistory
            }
          })
        }
      },

      handleBalanceUpdate: (assetID: number) => {
        if (networkAssetIDs.includes(assetID)) {
          dispatch({ type: 'BUMP_BALANCE_REFRESH' })
        }
      }
    }
  }, [
    state.sourceAssetID,
    state.bridgeName,
    state.pendingBridges,
    state.bridgeHistory,
    networkAssetIDs
  ])

  // Filter paths and find initial (bridge, source, dest) tuple
  useEffect(() => {
    const assets = app().assets

    // Create set of network asset IDs for quick lookup
    const networkAssetSet = new Set(networkAssetIDs)

    // Filter paths to only include:
    // 1. Assets with wallets
    // 2. Both source and dest must be in networkAssetIDs (same asset on different networks)
    const filteredPaths: Record<number, Record<number, string[]>> = {}
    for (const [srcIDStr, dests] of Object.entries(bridgePaths)) {
      const srcID = parseInt(srcIDStr)
      if (!assets[srcID]?.wallet) continue
      if (!networkAssetSet.has(srcID)) continue // Must be in current asset's networks

      const filteredDests: Record<number, string[]> = {}
      for (const [destIDStr, bridges] of Object.entries(dests)) {
        const destID = parseInt(destIDStr)
        if (!networkAssetSet.has(destID)) continue // Must be in current asset's networks
        if (assets[destID]?.wallet) {
          filteredDests[destID] = bridges
        }
      }

      if (Object.keys(filteredDests).length > 0) {
        filteredPaths[srcID] = filteredDests
      }
    }

    // Find initial valid (bridge, source, dest) tuple
    // IMPORTANT: use filteredPaths (wallet-filtered + network-filtered), not raw bridgePaths.
    const initialSource = networkAssetIDs.find(id => !!filteredPaths[id]) ?? Number(Object.keys(filteredPaths)[0] ?? 0)
    const initialDest = initialSource ? Number(Object.keys(filteredPaths[initialSource] || {})[0] ?? 0) : 0
    const initialBridge = (initialSource && initialDest)
      ? (filteredPaths[initialSource]?.[initialDest]?.[0] ?? '')
      : ''

    // If no valid path found, component shouldn't have been shown
    if (!initialBridge) {
      console.error('No valid bridge paths found')
      return
    }

    dispatch({
      type: 'INITIALIZE',
      paths: filteredPaths,
      initialBridge,
      initialSource,
      initialDest
    })
  }, [bridgePaths, networkAssetIDs])

  // Load history exactly once when the popup is initialized. After that, rely on
  // notifications and explicit pagination fetches.
  useEffect(() => {
    if (state.loading) return
    if (state.bridgeHistoryLoaded) return
    let cancelled = false
    ;(async () => {
      dispatch({ type: 'PATCH', patch: { historyLoading: true } })
      try {
        const { pending, history, refIDByAsset, doneByAsset } = await loadInitialBridgeHistory(networkAssetIDs, state.bridgeHistoryPageSize)
        if (cancelled) return
        dispatch({
          type: 'PATCH',
          patch: {
            pendingBridges: pending,
            bridgeHistory: history,
            bridgeHistoryRefIDByAsset: refIDByAsset,
            bridgeHistoryDoneByAsset: doneByAsset,
            bridgeHistoryLoaded: true,
            bridgeHistoryPageIndex: 0,
            historyLoading: false
          }
        })
      } catch (e) {
        if (cancelled) return
        dispatch({ type: 'PATCH', patch: { error: intl.prep(intl.ID_LOAD_HISTORY_ERR, { err: String(e) }) || `Failed to load history: ${e}`, historyLoading: false, bridgeHistoryLoaded: true } })
      }
    })()
    return () => { cancelled = true }
  }, [state.loading, state.bridgeHistoryLoaded, state.bridgeHistoryPageSize, networkAssetIDs])

  const handleBackdropClick = useCallback((e: React.MouseEvent<HTMLDivElement>) => {
    if (formRef.current && !formRef.current.contains(e.target as Node)) {
      onClose()
    }
  }, [onClose])

  const handleTabClick = useCallback((tab: 'bridge' | 'history') => {
    dispatch({ type: 'PATCH', patch: { activeTab: tab, error: null } })
  }, [])

  // If a transaction is selected, show details popup
  if (selectedTx) {
    return (
      <BridgeStateContext.Provider value={state}>
        <BridgeDispatchContext.Provider value={dispatch}>
          <div
            id="forms"
            className="stylish-overflow flex-center"
            onMouseDown={handleBackdropClick}
          >
            <form
              ref={formRef}
              className="position-relative stylish-overflow" style={{ maxWidth: '525px' }}
              autoComplete="off"
              onSubmit={(e) => e.preventDefault()}
            >
              {/* Close button */}
              <div className="form-closer">
                <span className="ico-cross pointer" onClick={() => setSelectedTxID(null)}></span>
              </div>
              <BridgeDetails tx={selectedTx} />
            </form>
          </div>
        </BridgeDispatchContext.Provider>
      </BridgeStateContext.Provider>
    )
  }

  return (
    <BridgeStateContext.Provider value={state}>
      <BridgeDispatchContext.Provider value={dispatch}>
        <div
          id="forms"
          className="stylish-overflow flex-center"
          onMouseDown={handleBackdropClick}
        >
          <form
            ref={formRef}
            className="position-relative stylish-overflow" style={{ maxWidth: '525px' }}
            autoComplete="off"
            onSubmit={(e) => e.preventDefault()}
          >
            {/* Close button */}
            <div className="form-closer">
              <span className="ico-cross pointer" onClick={onClose}></span>
            </div>

            {/* Header */}
            <header className="d-flex align-items-center mb-3">
              <span className="ico-exchange fs22 me-2"></span>
              <span className="fs22">{intl.prep(intl.ID_BRIDGE) || 'Bridge'}</span>
              {headerAsset && (
                <>
                  <span className="fs22 ms-2">{headerSymbol}</span>
                  <img
                    src={Doc.logoPath(headerAsset.symbol)}
                    className="micro-icon ms-2"
                    alt=""
                    onError={(e) => { (e.target as HTMLImageElement).style.display = 'none' }}
                  />
                </>
              )}
            </header>

            {/* Tab navigation */}
            <div className="d-flex mb-3 border-bottom">
              <button
                type="button"
                className={`flex-grow-1 py-2 border-0 hoverbg ${state.activeTab === 'bridge' ? 'active border-bottom border-primary border-2 fw-bold' : 'text-muted bg-transparent'}`}
                onClick={() => handleTabClick('bridge')}
              >
                {intl.prep(intl.ID_BRIDGE) || 'Bridge'}
              </button>
              <button
                type="button"
                className={`flex-grow-1 py-2 border-0 hoverbg ${state.activeTab === 'history' ? 'active border-bottom border-primary border-2 fw-bold' : 'text-muted bg-transparent'}`}
                onClick={() => handleTabClick('history')}
              >
                {intl.prep(intl.ID_HISTORY) || 'History'}
                {state.pendingBridges.length > 0 && (
                  <span className="badge bg-warning ms-1">{state.pendingBridges.length}</span>
                )}
              </button>
            </div>

            {/* Tab content */}
            <div className="bridge-content" style={{ minHeight: '200px', minWidth: '475px' }}>
              {state.activeTab === 'bridge'
                ? (
                <BridgeForm networkAssetIDs={networkAssetIDs} />
                  )
                : (
                <BridgeHistory networkAssetIDs={networkAssetIDs} onSelectTx={(tx) => setSelectedTxID(tx.id)} />
                  )}
            </div>
          </form>
        </div>
      </BridgeDispatchContext.Provider>
    </BridgeStateContext.Provider>
  )
})

export default BridgingPopup
