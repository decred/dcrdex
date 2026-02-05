import { app } from '../../registry'
import { BridgeTransaction } from '../types'

export async function loadPendingBridges (networkAssetIDs: number[]): Promise<BridgeTransaction[]> {
  if (!networkAssetIDs.length) return []
  const assetsWithWallets = networkAssetIDs.filter(id => app().assets[id]?.wallet)
  if (!assetsWithWallets.length) return []

  const pending: BridgeTransaction[] = []
  const seen = new Set<string>()

  await Promise.all(assetsWithWallets.map(async (assetID) => {
    try {
      const txs = await app().pendingBridges(assetID)
      for (const tx of txs) {
        if (seen.has(tx.id)) continue
        seen.add(tx.id)
        pending.push({ ...tx, sourceAssetID: assetID })
      }
    } catch (e) {
      console.error(`Failed to load pending bridges for asset ${assetID}:`, e)
    }
  }))

  pending.sort((a, b) => b.timestamp - a.timestamp)
  return pending
}

async function loadBridgeHistory (
  assetIDs: number[],
  pageSize: number,
  opts: {
    // If provided, fetch older history from these refs (ref tx included in
    // results and must be dropped).
    refIDByAsset?: Record<number, string | null>
    doneByAsset?: Record<number, boolean>
    // IDs to seed the dedupe set (e.g. pending tx IDs for initial load).
    seedSeenIDs?: Iterable<string>
    logErrMsg: string
  }
): Promise<{
    txs: BridgeTransaction[]
    refIDByAsset: Record<number, string | null>
    doneByAsset: Record<number, boolean>
  }> {
  const txs: BridgeTransaction[] = []
  const seen = new Set<string>(opts.seedSeenIDs)
  const newRef: Record<number, string | null> = { ...(opts.refIDByAsset ?? {}) }
  const newDone: Record<number, boolean> = { ...(opts.doneByAsset ?? {}) }

  const fromRef = !!opts.refIDByAsset

  await Promise.all(assetIDs.map(async (assetID) => {
    const done = newDone[assetID] === true
    const refID = newRef[assetID]
    if (fromRef && (done || !refID)) return

    try {
      const fetched = fromRef
        ? await app().bridgeHistory(assetID, pageSize + 1, refID as string, true)
        : await app().bridgeHistory(assetID, pageSize)

      const pageTxs = (fromRef && refID && fetched.length && fetched[0]?.id === refID)
        ? fetched.slice(1)
        : fetched

      if (fromRef) {
        if (!pageTxs.length) {
          // No new entries past the ref.
          newDone[assetID] = true
          return
        }
        newRef[assetID] = pageTxs[pageTxs.length - 1].id
        newDone[assetID] = pageTxs.length < pageSize
      } else {
        newRef[assetID] = pageTxs.length ? pageTxs[pageTxs.length - 1].id : null
        newDone[assetID] = pageTxs.length < pageSize
      }

      for (const tx of pageTxs) {
        const id = tx.id
        if (id && seen.has(id)) continue
        if (id) seen.add(id)
        txs.push({ ...tx, sourceAssetID: assetID })
      }
    } catch (e) {
      console.error(`${opts.logErrMsg} for asset ${assetID}:`, e)
      newDone[assetID] = true
      if (!fromRef) newRef[assetID] = null
    }
  }))

  return { txs, refIDByAsset: newRef, doneByAsset: newDone }
}

export async function loadInitialBridgeHistory (
  networkAssetIDs: number[],
  pageSize: number
): Promise<{
    pending: BridgeTransaction[]
    history: BridgeTransaction[]
    refIDByAsset: Record<number, string | null>
    doneByAsset: Record<number, boolean>
  }> {
  if (!networkAssetIDs.length) return { pending: [], history: [], refIDByAsset: {}, doneByAsset: {} }
  const assetsWithWallets = networkAssetIDs.filter(id => app().assets[id]?.wallet)
  if (!assetsWithWallets.length) return { pending: [], history: [], refIDByAsset: {}, doneByAsset: {} }

  const pending = await loadPendingBridges(assetsWithWallets)

  const { txs: history, refIDByAsset, doneByAsset } = await loadBridgeHistory(assetsWithWallets, pageSize, {
    seedSeenIDs: pending.flatMap(t => t.id ? [t.id] : []),
    logErrMsg: 'Failed to load bridge history'
  })

  history.sort((a, b) => b.timestamp - a.timestamp)
  return { pending, history, refIDByAsset, doneByAsset }
}

export async function loadMoreBridgeHistory (
  networkAssetIDs: number[],
  pageSize: number,
  refIDByAsset: Record<number, string | null>,
  doneByAsset: Record<number, boolean>
): Promise<{
    added: BridgeTransaction[]
    refIDByAsset: Record<number, string | null>
    doneByAsset: Record<number, boolean>
  }> {
  const assetsWithWallets = networkAssetIDs.filter(id => app().assets[id]?.wallet)
  const { txs: added, refIDByAsset: newRef, doneByAsset: newDone } = await loadBridgeHistory(assetsWithWallets, pageSize, {
    refIDByAsset,
    doneByAsset,
    logErrMsg: 'Failed to load more bridge history'
  })

  return { added, refIDByAsset: newRef, doneByAsset: newDone }
}
