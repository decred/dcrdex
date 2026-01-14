import React, { useCallback, useMemo } from 'react'
import { app } from '../../registry'
import { useBridgeState, useBridgeDispatch } from '../utils/BridgeState'
import * as intl from '../../locales'
import Doc from '../../doc'
import { BridgeTransaction } from '../types'
import { loadMoreBridgeHistory } from '../utils/bridgeData'
import { bridgeDisplayName, bridgeLogoPath, formatDateTime, networkInfo } from '../utils/bridgeUtils'

interface BridgeHistoryProps {
  networkAssetIDs: number[]
  onSelectTx: (tx: BridgeTransaction) => void
}

const BridgeHistory: React.FC<BridgeHistoryProps> = ({ networkAssetIDs, onSelectTx }) => {
  const state = useBridgeState()
  const dispatch = useBridgeDispatch()
  const {
    loading,
    historyLoading,
    bridgeHistory,
    pendingBridges,
    bridgeHistoryPageIndex,
    bridgeHistoryPageSize,
    bridgeHistoryRefIDByAsset,
    bridgeHistoryDoneByAsset
  } = state

  const pageStart = bridgeHistoryPageIndex * bridgeHistoryPageSize
  const pageEnd = pageStart + bridgeHistoryPageSize

  const visibleHistory = useMemo(() => {
    return bridgeHistory.slice(pageStart, pageEnd)
  }, [bridgeHistory, pageStart, pageEnd])

  const canPrev = bridgeHistoryPageIndex > 0
  const canNextFromCache = bridgeHistory.length > pageEnd
  const hasMoreFromBackend = networkAssetIDs.some(id => !!bridgeHistoryRefIDByAsset[id] && !bridgeHistoryDoneByAsset[id])
  const canNext = canNextFromCache || hasMoreFromBackend
  const showPager = canPrev || canNext

  const handlePrev = useCallback(() => {
    if (!canPrev) return
    dispatch({ type: 'PATCH', patch: { bridgeHistoryPageIndex: bridgeHistoryPageIndex - 1 } })
  }, [canPrev, bridgeHistoryPageIndex, dispatch])

  const handleNext = useCallback(async () => {
    if (historyLoading) return
    if (canNextFromCache) {
      dispatch({ type: 'PATCH', patch: { bridgeHistoryPageIndex: bridgeHistoryPageIndex + 1 } })
      return
    }
    if (!hasMoreFromBackend) return

    dispatch({ type: 'PATCH', patch: { historyLoading: true } })
    try {
      const { added, refIDByAsset, doneByAsset } = await loadMoreBridgeHistory(
        networkAssetIDs,
        bridgeHistoryPageSize,
        bridgeHistoryRefIDByAsset,
        bridgeHistoryDoneByAsset
      )

      const merged = [...bridgeHistory, ...added].sort((a, b) => b.timestamp - a.timestamp)

      // Advance if the next page would have at least one item; otherwise stay put.
      const desiredIndex = bridgeHistoryPageIndex + 1
      const nextPageStart = desiredIndex * bridgeHistoryPageSize
      const nextIndex = merged.length > nextPageStart ? desiredIndex : bridgeHistoryPageIndex

      dispatch({
        type: 'PATCH',
        patch: {
          bridgeHistory: merged,
          bridgeHistoryRefIDByAsset: refIDByAsset,
          bridgeHistoryDoneByAsset: doneByAsset,
          bridgeHistoryPageIndex: nextIndex,
          historyLoading: false
        }
      })
    } catch (e) {
      dispatch({ type: 'PATCH', patch: { error: intl.prep(intl.ID_LOAD_HISTORY_ERR, { err: String(e) }) || `Failed to load more history: ${e}`, historyLoading: false } })
    }
  }, [
    historyLoading,
    canNextFromCache,
    hasMoreFromBackend,
    dispatch,
    bridgeHistory,
    bridgeHistoryPageIndex,
    bridgeHistoryPageSize,
    networkAssetIDs,
    bridgeHistoryRefIDByAsset,
    bridgeHistoryDoneByAsset
  ])

  const getDirection = useCallback((tx: BridgeTransaction) => {
    const sourceAsset = app().assets[tx.sourceAssetID]
    const destAssetID = tx.bridgeCounterpartTx?.assetID
    const destAsset = destAssetID ? app().assets[destAssetID] : null

    return (
      <div className="d-flex align-items-center">
        {sourceAsset && (
          <img
            src={Doc.logoPath(networkInfo(tx.sourceAssetID).symbol)}
            className="mini-icon"
            alt=""
            onError={(e) => { (e.target as HTMLImageElement).style.display = 'none' }}
          />
        )}
        <span className="mx-1">&rarr;</span>
        {destAsset && destAssetID && (
          <img
            src={Doc.logoPath(networkInfo(destAssetID).symbol)}
            className="mini-icon"
            alt=""
            onError={(e) => { (e.target as HTMLImageElement).style.display = 'none' }}
          />
        )}
      </div>
    )
  }, [])

  const renderTable = (transactions: BridgeTransaction[], isPending: boolean) => {
    if (transactions.length === 0) return null

    return (
      <table className="w-100 mb-3">
        <thead>
          <tr className="text-muted fs14">
            <th className="py-2 fw-normal">{intl.prep(intl.ID_ROUTE) || 'Route'}</th>
            <th className="py-2 fw-normal">{intl.prep(intl.ID_BRIDGE) || 'Bridge'}</th>
            <th className="py-2 fw-normal text-end">{intl.prep(intl.ID_AMOUNT) || 'Amount'}</th>
            <th className="py-2 fw-normal text-end">{intl.prep(intl.ID_TIME) || 'Time'}</th>
          </tr>
        </thead>
        <tbody>
          {transactions.map(tx => (
            <tr
              key={tx.id}
              className={`pointer hoverbg ${isPending ? 'bg-body-secondary' : ''}`}
              onClick={() => onSelectTx(tx)}
              style={{ borderBottom: '1px solid var(--border-color)' }}
            >
              <td className="py-2">{getDirection(tx)}</td>
              <td className="py-2">
                <div className="d-flex align-items-center">
                  {tx.bridgeName && (
                    <img
                      src={bridgeLogoPath(tx.bridgeName)}
                      className="micro-icon me-1"
                      alt=""
                      onError={(e) => { (e.target as HTMLImageElement).style.display = 'none' }}
                    />
                  )}
                  <span>{tx.bridgeName ? bridgeDisplayName(tx.bridgeName, 'short') : '-'}</span>
                </div>
              </td>
              <td className="py-2 text-end fw-bold">
                {(() => {
                  const asset = app().assets[tx.sourceAssetID]
                  if (!asset) return String(tx.amount)
                  return Doc.formatCoinValue(tx.amount, asset.unitInfo)
                })()}
              </td>
              <td className="py-2 text-end text-muted">{formatDateTime(tx.timestamp)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    )
  }

  if (loading) {
    return (
      <div className="flex-center p-4">
        <div className="ico-spinner spinner fs20"></div>
      </div>
    )
  }

  return (
    <div className="flex-stretch-column">
      {/* Pending bridges */}
      {pendingBridges.length > 0 && (
        <div className="mb-2">
          <h6 className="mb-2 fs15 fw-bold">{intl.prep(intl.ID_PENDING_BRIDGES) || 'Pending'}</h6>
          {renderTable(pendingBridges, true)}
        </div>
      )}

      {/* Bridge history */}
      {bridgeHistory.length > 0 && (
        <div>
          <h6 className="mb-2 fs15 fw-bold">{intl.prep(intl.ID_BRIDGE_HISTORY) || 'History'}</h6>
          {renderTable(visibleHistory, false)}
          {showPager && (
            <div className="d-flex mt-2 justify-content-center align-items-center">
              {canPrev && (
                <button
                  className={`go fs14 ${canPrev && canNext ? 'me-2' : ''}`}
                  type="button"
                  disabled={historyLoading}
                  onClick={handlePrev}
                  aria-label={intl.prep(intl.ID_PREVIOUS) || 'Previous'}
                  title={intl.prep(intl.ID_PREVIOUS) || 'Previous'}
                >
                  <span className="ico-arrowleft"></span>
                </button>
              )}
              {historyLoading && <span className="ico-spinner spinner fs14 mx-2"></span>}
              {canNext && (
                <button
                  className={`go fs14 ${canPrev && canNext ? 'ms-2' : ''}`}
                  type="button"
                  disabled={historyLoading}
                  onClick={handleNext}
                  aria-label={intl.prep(intl.ID_NEXT) || 'Next'}
                  title={intl.prep(intl.ID_NEXT) || 'Next'}
                >
                  <span className="ico-arrowright"></span>
                </button>
              )}
            </div>
          )}
        </div>
      )}

      {pendingBridges.length === 0 && bridgeHistory.length === 0 && (
        <div className="text-center text-muted p-4 fs15">
          {intl.prep(intl.ID_NO_BRIDGES_FOUND) || 'No bridges found'}
        </div>
      )}
    </div>
  )
}

export default BridgeHistory
