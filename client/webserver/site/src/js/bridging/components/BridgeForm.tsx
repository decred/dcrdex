import React, { useEffect, useCallback, useMemo, useRef } from 'react'
import { app, BridgeApprovalPending } from '../../registry'
import Doc from '../../doc'
import {
  approvalStatusFromBridgeApproval,
  useBridgeState,
  useBridgeDispatch
} from '../utils/BridgeState'
import * as intl from '../../locales'
import { atomicToConventionalString, bridgeDisplayName, bridgeLogoPath, calculateMaxBridgeableAmount, networkInfo, parseConventionalToAtomic } from '../utils/bridgeUtils'
import { BridgeTransaction } from '../types'

const bridgeLimitsTooltip = () => intl.prep(intl.ID_BRIDGE_LIMITS_TOOLTIP) || 'This bridge does not support bridging amounts outside of these limits.'

interface BridgeFormProps {
  networkAssetIDs: number[]
}

const BridgeForm: React.FC<BridgeFormProps> = ({ networkAssetIDs }) => {
  const state = useBridgeState()
  const dispatch = useBridgeDispatch()
  const {
    bridgeName,
    sourceAssetID,
    destAssetID,
    amount,
    approvalStatus,
    feesAndLimits,
    allBridgePaths,
    loading,
    submitting,
    error,
    balanceRefreshToken
  } = state

  const sourceAsset = app().assets[sourceAssetID]
  const destAsset = app().assets[destAssetID]
  const availableAtomic = sourceAsset?.wallet?.balance?.available ?? 0
  const loadFeesAndApprovalReq = useRef(0)
  const limitsRef = useRef<HTMLDivElement>(null)

  // availableBridges returns all the bridges that support any of the network assets
  const availableBridges = useMemo(() => {
    const set = new Set<string>()
    for (const srcID of networkAssetIDs) {
      const dests = allBridgePaths[srcID] || {}
      for (const bridges of Object.values(dests)) for (const b of bridges) set.add(b)
    }
    return Array.from(set).sort()
  }, [allBridgePaths, networkAssetIDs])

  // availableSourceIDs returns all the source assets that can be bridged with the current selected bridge
  const availableSourceIDs = useMemo(() => {
    const ids: number[] = []
    for (const srcID of networkAssetIDs) {
      const dests = allBridgePaths[srcID]
      if (!dests) continue
      if (!bridgeName) {
        if (Object.keys(dests).length) ids.push(srcID)
        continue
      }
      const supportsBridge = Object.values(dests).some(bridges => bridges.includes(bridgeName))
      if (supportsBridge) ids.push(srcID)
    }
    return ids.sort((a, b) => a - b)
  }, [allBridgePaths, bridgeName, networkAssetIDs])

  // availableDestIDs returns all the destination assets that can be bridged with the current selected bridge
  const availableDestIDs = useMemo(() => {
    const set = new Set<number>()
    for (const srcID of networkAssetIDs) {
      const dests = allBridgePaths[srcID] || {}
      for (const [destStr, bridges] of Object.entries(dests)) {
        const destID = Number(destStr)
        if (!bridgeName || bridges.includes(bridgeName)) set.add(destID)
      }
    }
    return Array.from(set).sort((a, b) => a - b)
  }, [allBridgePaths, bridgeName, networkAssetIDs])

  const allSources = useMemo(() => {
    return availableSourceIDs.map(id => ({ id, ...networkInfo(id) }))
  }, [availableSourceIDs])

  const allDestinations = useMemo(() => {
    return availableDestIDs.map(id => ({ id, ...networkInfo(id) }))
  }, [availableDestIDs])

  // Validate amount
  const parsedAmount = useMemo(() => {
    if (!sourceAsset) return { atomic: null as number | null, error: amount ? (intl.prep(intl.ID_INVALID_AMOUNT) || 'Invalid amount') : null }
    return parseConventionalToAtomic(amount, sourceAsset.unitInfo)
  }, [amount, sourceAssetID, balanceRefreshToken])

  const amountError = useMemo(() => {
    if (!amount) return null
    if (parsedAmount.error) return parsedAmount.error
    const atomic = parsedAmount.atomic ?? 0
    if (atomic <= 0) return intl.prep(intl.ID_AMOUNT_MUST_BE_POSITIVE) || 'Amount must be greater than 0'
    if (atomic > availableAtomic) {
      const maxStr = sourceAsset ? Doc.formatCoinValue(availableAtomic, sourceAsset.unitInfo) : ''
      return intl.prep(intl.ID_INSUFFICIENT_BALANCE, { max: maxStr }) || `Insufficient balance (max: ${maxStr})`
    }
    // Check if bridging base asset would leave insufficient funds for fees
    if (sourceAsset && !sourceAsset.token && feesAndLimits) {
      const baseAssetID = sourceAssetID
      const initiationFee = feesAndLimits.fees[baseAssetID] || 0
      if (atomic + initiationFee > availableAtomic) {
        const maxBridgeable = calculateMaxBridgeableAmount(sourceAssetID, availableAtomic, feesAndLimits)
        const maxStr = Doc.formatCoinValue(maxBridgeable, sourceAsset.unitInfo)
        return intl.prep(intl.ID_RESERVE_FUNDS_FOR_FEES, { max: maxStr }) || `Must reserve funds for fees (max bridgeable: ${maxStr})`
      }
    }
    if (feesAndLimits?.hasLimits && sourceAsset) {
      if (atomic < feesAndLimits.minLimit) {
        const minStr = Doc.formatCoinValue(feesAndLimits.minLimit, sourceAsset.unitInfo)
        return intl.prep(intl.ID_AMOUNT_BELOW_MIN, { min: minStr }) || `Amount is below minimum (${minStr})`
      }
      if (atomic > feesAndLimits.maxLimit) {
        const maxStr = Doc.formatCoinValue(feesAndLimits.maxLimit, sourceAsset.unitInfo)
        return intl.prep(intl.ID_AMOUNT_ABOVE_MAX, { max: maxStr }) || `Amount is above maximum (${maxStr})`
      }
    }
    return null
  }, [amount, parsedAmount, availableAtomic, feesAndLimits, sourceAssetID, sourceAsset, balanceRefreshToken])

  // Load fees and approval status when selection changes
  useEffect(() => {
    if (!destAssetID || !bridgeName || loading) return
    let cancelled = false
    const reqID = ++(loadFeesAndApprovalReq.current)
    ;(async () => {
      try {
        const fees = await app().bridgeFeesAndLimits(sourceAssetID, destAssetID, bridgeName)
        const approvalResp = await app().bridgeApprovalStatus(sourceAssetID, bridgeName)
        if (cancelled || reqID !== loadFeesAndApprovalReq.current) return
        dispatch({ type: 'PATCH', patch: { feesAndLimits: fees } })
        dispatch({
          type: 'PATCH',
          patch: {
            approvalStatus: approvalResp.ok ? approvalStatusFromBridgeApproval(approvalResp.status) : 'notRequired'
          }
        })
      } catch (e) {
        if (cancelled || reqID !== loadFeesAndApprovalReq.current) return
        dispatch({ type: 'PATCH', patch: { error: intl.prep(intl.ID_BRIDGE_LOAD_ERR, { err: String(e) }) || `Failed to load bridge info: ${e}` } })
      }
    })()
    return () => { cancelled = true }
  }, [sourceAssetID, destAssetID, bridgeName, loading, dispatch])

  // Bind tooltip when limits element is rendered
  useEffect(() => {
    if (limitsRef.current) {
      app().bindTooltips(limitsRef.current)
    }
  }, [feesAndLimits])

  const handleBridgeChange = useCallback((e: React.ChangeEvent<HTMLSelectElement>) => {
    dispatch({ type: 'SELECT_BRIDGE', bridgeName: e.target.value })
  }, [dispatch])

  const handleSourceChange = useCallback((e: React.ChangeEvent<HTMLSelectElement>) => {
    dispatch({ type: 'SELECT_SOURCE', sourceAssetID: parseInt(e.target.value) })
  }, [dispatch])

  const handleDestinationChange = useCallback((e: React.ChangeEvent<HTMLSelectElement>) => {
    dispatch({ type: 'SELECT_DESTINATION', destAssetID: parseInt(e.target.value) })
  }, [dispatch])

  const handleAmountChange = useCallback((e: React.ChangeEvent<HTMLInputElement>) => {
    dispatch({ type: 'PATCH', patch: { amount: e.target.value } })
  }, [dispatch])

  const handleMaxClick = useCallback(() => {
    if (!sourceAsset?.wallet?.balance) return
    const maxBridgeable = calculateMaxBridgeableAmount(
      sourceAssetID,
      sourceAsset.wallet.balance.available,
      feesAndLimits
    )
    dispatch({ type: 'PATCH', patch: { amount: atomicToConventionalString(maxBridgeable, sourceAsset.unitInfo, true) } })
  }, [sourceAssetID, balanceRefreshToken, feesAndLimits, dispatch])

  const handleApprove = useCallback(async () => {
    dispatch({ type: 'PATCH', patch: { submitting: true } })
    try {
      const resp = await app().approveBridgeContract(sourceAssetID, bridgeName)
      if (resp.ok) {
        dispatch({ type: 'PATCH', patch: { approvalStatus: approvalStatusFromBridgeApproval(BridgeApprovalPending) } })
      } else {
        dispatch({ type: 'PATCH', patch: { error: resp.msg || (intl.prep(intl.ID_APPROVAL_FAILED) || 'Approval failed') } })
      }
    } catch (e) {
      dispatch({ type: 'PATCH', patch: { error: intl.prep(intl.ID_APPROVAL_ERR, { err: String(e) }) || `Approval error: ${e}` } })
    }
    dispatch({ type: 'PATCH', patch: { submitting: false } })
  }, [sourceAssetID, bridgeName, dispatch])

  const handleBridge = useCallback(async () => {
    if (!sourceAsset) {
      dispatch({ type: 'PATCH', patch: { error: intl.prep(intl.ID_INVALID_SOURCE_ASSET) || 'Invalid source asset' } })
      return
    }
    const { atomic, error: amtErr } = parseConventionalToAtomic(amount, sourceAsset.unitInfo)
    if (amtErr || atomic === null || atomic <= 0) {
      dispatch({ type: 'PATCH', patch: { error: amtErr || (intl.prep(intl.ID_INVALID_AMOUNT) || 'Invalid amount') } })
      return
    }

    dispatch({ type: 'PATCH', patch: { submitting: true } })
    try {
      const resp = await app().bridge(sourceAssetID, destAssetID, atomic, bridgeName)
      if (resp.ok) {
        let pendingBridges: BridgeTransaction[] | undefined
        try {
          const pending = await app().pendingBridges(sourceAssetID)
          pendingBridges = pending.map(tx => ({ ...tx, sourceAssetID }))
        } catch (_e) {
          // Non-fatal: notification stream should still catch up.
        }
        dispatch({
          type: 'PATCH',
          patch: {
            amount: '',
            activeTab: 'history',
            ...(pendingBridges && { pendingBridges })
          }
        })
      } else {
        dispatch({ type: 'PATCH', patch: { error: resp.msg || (intl.prep(intl.ID_BRIDGE_FAILED) || 'Bridge failed') } })
      }
    } catch (e) {
      dispatch({ type: 'PATCH', patch: { error: intl.prep(intl.ID_BRIDGE_ERR, { err: String(e) }) || `Bridge error: ${e}` } })
    }
    dispatch({ type: 'PATCH', patch: { submitting: false } })
  }, [sourceAssetID, destAssetID, amount, bridgeName, balanceRefreshToken, dispatch])

  const formatFees = () => {
    if (!feesAndLimits) return null
    const feeEntries = Object.entries(feesAndLimits.fees)
    if (feeEntries.length === 0) return null

    return feeEntries.map(([assetIDStr, fee]) => {
      const assetID = parseInt(assetIDStr)
      const asset = app().assets[assetID]
      if (!asset) return null
      return (
        <div key={assetID} className="fs14 text-muted">
          {Doc.formatCoinValue(fee, asset.unitInfo)} {asset.unitInfo.conventional.unit}
        </div>
      )
    })
  }

  const canSubmit = useMemo(() => {
    if (submitting || loading) return false
    if (!destAssetID || !amount) return false
    if (amountError) return false
    if (approvalStatus === 'notApproved' || approvalStatus === 'pending' || approvalStatus === 'loading') return false
    if (parsedAmount.atomic === null || parsedAmount.atomic <= 0) return false
    return true
  }, [submitting, loading, destAssetID, amount, amountError, approvalStatus, parsedAmount.atomic])

  const needsApproval = approvalStatus === 'notApproved'

  if (loading) {
    return (
      <div className="flex-center p-4" style={{ minHeight: '300px' }}>
        <div className="ico-spinner spinner fs20"></div>
      </div>
    )
  }

  const isBridgeDisabled = submitting || availableBridges.length <= 1
  const isSourceDisabled = submitting || allSources.length <= 1
  const isDestDisabled = submitting || allDestinations.length <= 1

  return (
    <div className="flex-stretch-column" style={{ minHeight: '300px' }}>
      {/* Bridge selector */}
      <div className="mb-3 pb-3 border-bottom">
        <label className="form-label">{intl.prep(intl.ID_BRIDGE) || 'Bridge'}</label>
        <div className="position-relative">
          <img
            src={bridgeName ? bridgeLogoPath(bridgeName) : ''}
            className="mini-icon position-absolute"
            alt=""
            style={{ left: '10px', top: '50%', transform: 'translateY(-50%)', visibility: bridgeName ? 'visible' : 'hidden', zIndex: 1, pointerEvents: 'none' }}
            onError={(e) => { (e.target as HTMLImageElement).style.visibility = 'hidden' }}
          />
          <select
            className="form-select"
            value={bridgeName}
            onChange={handleBridgeChange}
            disabled={isBridgeDisabled}
            style={{ opacity: isBridgeDisabled ? 0.5 : 1, paddingLeft: '38px' }}
          >
            {availableBridges.map(bridge => (
              <option key={bridge} value={bridge}>{bridgeDisplayName(bridge, 'long')}</option>
            ))}
          </select>
        </div>
        {/* Bridge Limits */}
        {feesAndLimits?.hasLimits && sourceAsset && (
          <div ref={limitsRef} className="d-flex align-items-center fs14 text-muted mt-2">
            <span className="me-1">{intl.prep(intl.ID_BRIDGE_LIMITS) || 'Bridge Limits'}:</span>
            <span>
              {Doc.formatCoinValue(feesAndLimits.minLimit, sourceAsset.unitInfo)} - {Doc.formatCoinValue(feesAndLimits.maxLimit, sourceAsset.unitInfo)}
            </span>
            <span className="ico-info fs12 ms-1" data-tooltip={bridgeLimitsTooltip()}></span>
          </div>
        )}
      </div>

      {/* Source selector */}
      <div className="mb-3 pb-3 border-bottom">
        <label className="form-label">{intl.prep(intl.ID_FROM) || 'From'}</label>
        <div className="position-relative">
          <img
            src={sourceAsset ? Doc.logoPath(networkInfo(sourceAssetID).symbol) : ''}
            className="mini-icon position-absolute"
            alt=""
            style={{ left: '10px', top: '50%', transform: 'translateY(-50%)', visibility: sourceAsset ? 'visible' : 'hidden', zIndex: 1, pointerEvents: 'none' }}
            onError={(e) => { (e.target as HTMLImageElement).style.visibility = 'hidden' }}
          />
          <select
            className="form-select"
            value={sourceAssetID}
            onChange={handleSourceChange}
            disabled={isSourceDisabled}
            style={{ paddingLeft: '38px' }}
          >
            {allSources.map(net => (
              <option key={net.id} value={net.id}>{net.name}</option>
            ))}
          </select>
        </div>
        <div className="fs15 text-muted mt-1" style={{ minHeight: '18px' }}>
          {sourceAsset?.wallet?.balance && (
            <>
              {intl.prep(intl.ID_AVAILABLE_TITLE) || 'Available'}: {Doc.formatCoinValue(availableAtomic, sourceAsset.unitInfo)}
              {/* Show max bridgeable if different from available (base asset case) */}
              {!sourceAsset.token && feesAndLimits && (() => {
                const maxBridgeable = calculateMaxBridgeableAmount(sourceAssetID, availableAtomic, feesAndLimits)
                if (maxBridgeable < availableAtomic) {
                  const maxStr = Doc.formatCoinValue(maxBridgeable, sourceAsset.unitInfo)
                  return (
                    <span className="ms-2">
                      ({intl.prep(intl.ID_MAX_BRIDGEABLE) || 'Max bridgeable'}: {maxStr})
                    </span>
                  )
                }
                return null
              })()}
            </>
          )}
        </div>
      </div>

      {/* Destination selector */}
      <div className="mb-3 pb-3 border-bottom">
        <label className="form-label">{intl.prep(intl.ID_TO) || 'To'}</label>
        <div className="position-relative">
          <img
            src={destAsset ? Doc.logoPath(networkInfo(destAssetID).symbol) : ''}
            className="mini-icon position-absolute"
            alt=""
            style={{ left: '10px', top: '50%', transform: 'translateY(-50%)', visibility: destAsset ? 'visible' : 'hidden', zIndex: 1, pointerEvents: 'none' }}
            onError={(e) => { (e.target as HTMLImageElement).style.visibility = 'hidden' }}
          />
          <select
            className="form-select"
            value={destAssetID}
            onChange={handleDestinationChange}
            disabled={isDestDisabled}
            style={{ paddingLeft: '38px' }}
          >
            {allDestinations.map(net => (
              <option key={net.id} value={net.id}>{net.name}</option>
            ))}
          </select>
        </div>
      </div>

      {/* Amount input with max button */}
      <div className="mb-3 pb-3 border-bottom">
        <label className="form-label">{intl.prep(intl.ID_AMOUNT) || 'Amount'}</label>
        <div className="position-relative">
          <input
            type="number"
            className={`form-control ${amountError ? 'is-invalid' : ''}`}
            value={amount}
            onChange={handleAmountChange}
            placeholder="0.0"
            disabled={submitting}
            step="any"
            style={{ paddingRight: '50px' }}
          />
          <div className="position-absolute d-flex align-items-center" style={{ right: '8px', top: '50%', transform: 'translateY(-50%)' }}>
            <button
              type="button"
              className="btn btn-sm btn-outline-secondary"
              onClick={handleMaxClick}
              disabled={submitting}
              style={{ fontSize: '11px', padding: '2px 6px' }}
            >
              {intl.prep(intl.ID_MAX) || 'max'}
            </button>
          </div>
        </div>
        {/* Amount error */}
        {amountError && (
          <div className="fs12 text-danger mt-1">{amountError}</div>
        )}
      </div>

      {/* Fees display - fixed height to prevent jumping */}
      <div className="mb-3" style={{ minHeight: '44px' }}>
        <label className="form-label">{intl.prep(intl.ID_ESTIMATED_FEES) || 'Estimated Fees'}</label>
        <div style={{ minHeight: '20px' }}>
          {feesAndLimits
            ? formatFees()
            : <span className="ico-spinner spinner fs14"></span>}
        </div>
      </div>

      {/* Error display */}
      {error && (
        <div className="mb-3 fs14 text-danger">{error}</div>
      )}

      {/* Action button - shows Approve, Pending Approval, or Bridge depending on state */}
      {needsApproval
        ? (
          <button
            className="feature w-100"
            onClick={handleApprove}
            disabled={submitting}
          >
            {submitting ? <span className="ico-spinner spinner me-2"></span> : null}
            {intl.prep(intl.ID_APPROVE_CONTRACT) || 'Approve Contract'}
          </button>
          )
        : approvalStatus === 'pending'
          ? (
            <button
              className="feature w-100"
              disabled={true}
            >
              <span className="ico-spinner spinner me-2"></span>
              {intl.prep(intl.ID_APPROVAL_PENDING) || 'Pending Approval'}
            </button>
            )
          : (
            <button
              className="feature w-100"
              onClick={handleBridge}
              disabled={!canSubmit}
            >
              {submitting ? <span className="ico-spinner spinner me-2"></span> : null}
              {intl.prep(intl.ID_BRIDGE) || 'Bridge'}
            </button>
            )}

      {availableDestIDs.length === 0 && !loading && (
        <div className="text-center text-muted p-4">
          {intl.prep(intl.ID_NO_DESTINATIONS) || 'No bridge destinations available'}
        </div>
      )}
    </div>
  )
}

export default BridgeForm
