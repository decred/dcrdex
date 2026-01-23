import React, { useCallback, useState } from 'react'
import { app } from '../../registry'
import Doc from '../../doc'
import { BridgeTransaction } from '../types'
import { bridgeDisplayName, bridgeLogoPath, formatDateTime, getFeeAsset, networkInfo, trimStringWithEllipsis } from '../utils/bridgeUtils'
import * as intl from '../../locales'

interface BridgeDetailsProps {
  tx: BridgeTransaction
}

const BridgeDetails: React.FC<BridgeDetailsProps> = ({ tx }) => {
  const [copiedId, setCopiedId] = useState<string | null>(null)

  const sourceAsset = app().assets[tx.sourceAssetID]
  const destAssetID = tx.bridgeCounterpartTx?.assetID
  const destAsset = destAssetID ? app().assets[destAssetID] : null
  const counterpart = tx.bridgeCounterpartTx

  const copyColor = '#1e7d11'

  const handleCopy = useCallback(async (text: string, id: string) => {
    try {
      await navigator.clipboard.writeText(text)
      setCopiedId(id)
      setTimeout(() => setCopiedId(null), 350)
    } catch (err) {
      console.error('Unable to copy:', err)
    }
  }, [])

  const getStatusBadge = () => {
    if (tx.bridgeCounterpartTx?.complete) {
      return <span className="badge bg-success">{intl.prep(intl.ID_COMPLETE) || 'Complete'}</span>
    }
    return <span className="badge bg-warning">{intl.prep(intl.ID_PENDING) || 'Pending'}</span>
  }

  return (
    <div className="flex-stretch-column" style={{ minWidth: '425px' }}>
      <header>{intl.prep(intl.ID_BRIDGE_DETAILS) || 'Bridge Details'}</header>

      <table className="compact w-100">
        <tbody>
          {/* Status */}
          <tr>
            <td className="grey">{intl.prep(intl.ID_STATUS) || 'Status'}</td>
            <td>{getStatusBadge()}</td>
          </tr>

          {/* Bridge */}
          {tx.bridgeName && (
            <tr>
              <td className="grey">{intl.prep(intl.ID_BRIDGE) || 'Bridge'}</td>
              <td>
                <div>
                  <img
                    src={bridgeLogoPath(tx.bridgeName)}
                    className="micro-icon me-1"
                    alt=""
                    onError={(e) => { (e.target as HTMLImageElement).style.display = 'none' }}
                  />
                  <span>{bridgeDisplayName(tx.bridgeName, 'long')}</span>
                </div>
              </td>
            </tr>
          )}

          {/* Direction */}
          <tr>
            <td className="grey">{intl.prep(intl.ID_DIRECTION) || 'Direction'}</td>
            <td>
              <div>
                {sourceAsset && (
                  <>
                    <img
                      src={Doc.logoPath(networkInfo(tx.sourceAssetID).symbol)}
                      className="micro-icon me-1"
                      alt=""
                      onError={(e) => { (e.target as HTMLImageElement).style.display = 'none' }}
                    />
                    <span>{networkInfo(tx.sourceAssetID).name}</span>
                  </>
                )}
                <span className="mx-2">&rarr;</span>
                {destAsset && destAssetID && (
                  <>
                    <img
                      src={Doc.logoPath(networkInfo(destAssetID).symbol)}
                      className="micro-icon me-1"
                      alt=""
                      onError={(e) => { (e.target as HTMLImageElement).style.display = 'none' }}
                    />
                    <span>{networkInfo(destAssetID).name}</span>
                  </>
                )}
              </div>
            </td>
          </tr>

          {/* Amount sent */}
          <tr>
            <td className="grey">{intl.prep(intl.ID_AMOUNT_SENT) || 'Amount Sent'}</td>
            <td>
              {(() => {
                if (!sourceAsset) return String(tx.amount)
                return `${Doc.formatCoinValue(tx.amount, sourceAsset.unitInfo)} ${sourceAsset.unitInfo.conventional.unit}`
              })()}
            </td>
          </tr>

          {/* Amount received */}
          {counterpart && counterpart.amountReceived > 0 && destAssetID !== undefined && (
            <tr>
              <td className="grey">{intl.prep(intl.ID_AMOUNT_RECEIVED) || 'Amount Received'}</td>
              <td className="text-success">
                {(() => {
                  if (!destAsset) return String(counterpart.amountReceived)
                  return `${Doc.formatCoinValue(counterpart.amountReceived, destAsset.unitInfo)} ${destAsset.unitInfo.conventional.unit}`
                })()}
              </td>
            </tr>
          )}

          {/* Bridge Fee (difference between amount sent and received) */}
          {counterpart && counterpart.amountReceived > 0 && tx.amount > counterpart.amountReceived && destAsset && (
            <tr>
              <td className="grey">{intl.prep(intl.ID_BRIDGE_FEE) || 'Bridge Fee'}</td>
              <td>
                {`${Doc.formatCoinValue(tx.amount - counterpart.amountReceived, destAsset.unitInfo)} ${destAsset.unitInfo.conventional.unit}`}
              </td>
            </tr>
          )}

          {/* Source Gas Fee */}
          {tx.fees > 0 && (
            <tr>
              <td className="grey">{intl.prep(intl.ID_SOURCE_FEE) || 'Source Fee'}</td>
              <td>
                {(() => {
                  const feeAsset = getFeeAsset(tx.sourceAssetID)
                  if (!feeAsset) return String(tx.fees)
                  return `${Doc.formatCoinValue(tx.fees, feeAsset.unitInfo)} ${feeAsset.unitInfo.conventional.unit}`
                })()}
              </td>
            </tr>
          )}

          {/* Destination Gas Fee (if available) */}
          {counterpart && counterpart.fees > 0 && destAssetID !== undefined && (
            <tr>
              <td className="grey">{intl.prep(intl.ID_DEST_FEE) || 'Dest Fee'}</td>
              <td>
                {(() => {
                  const feeAsset = getFeeAsset(destAssetID)
                  if (!feeAsset) return String(counterpart.fees)
                  return `${Doc.formatCoinValue(counterpart.fees, feeAsset.unitInfo)} ${feeAsset.unitInfo.conventional.unit}`
                })()}
              </td>
            </tr>
          )}

          {/* Timestamp */}
          <tr>
            <td className="grey">{intl.prep(intl.ID_TIMESTAMP) || 'Timestamp'}</td>
            <td>{formatDateTime(tx.timestamp)}</td>
          </tr>

          {/* Transaction ID */}
          <tr>
            <td className="grey">{intl.prep(intl.ID_TX_ID) || 'TX ID'}</td>
            <td>
              <span
                className="ease-color pointer"
                style={{ color: copiedId === 'source' ? copyColor : undefined }}
                title={tx.id}
                onClick={() => handleCopy(tx.id, 'source')}
              >
                {trimStringWithEllipsis(tx.id, 20)}
              </span>
              <span
                className="ico-copy pointer ease-color ms-1"
                style={{ color: copiedId === 'source' ? copyColor : undefined }}
                onClick={() => handleCopy(tx.id, 'source')}
              ></span>
            </td>
          </tr>

          {/* Counterpart TX IDs */}
          {counterpart && counterpart.ids && counterpart.ids.length > 0 && (
            <>
              {counterpart.ids.map((id, i) => (
                <tr key={i}>
                  <td className="grey">{i === 0 ? (intl.prep(intl.ID_DEST_TX) || 'Dest TX') : ''}</td>
                  <td>
                    <span
                      className="ease-color pointer"
                      style={{ color: copiedId === `dest-${i}` ? copyColor : undefined }}
                      title={id}
                      onClick={() => handleCopy(id, `dest-${i}`)}
                    >
                      {trimStringWithEllipsis(id, 20)}
                    </span>
                    <span
                      className="ico-copy pointer ease-color ms-1"
                      style={{ color: copiedId === `dest-${i}` ? copyColor : undefined }}
                      onClick={() => handleCopy(id, `dest-${i}`)}
                    ></span>
                  </td>
                </tr>
              ))}
            </>
          )}
        </tbody>
      </table>
    </div>
  )
}

export default BridgeDetails
