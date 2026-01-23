import { WalletTransaction } from '../registry'

// Extended transaction with source asset info.
export interface BridgeTransaction extends WalletTransaction {
  sourceAssetID: number
}
