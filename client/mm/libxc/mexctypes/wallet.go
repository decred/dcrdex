package mexctypes

// CoinInfo structure from GET /api/v3/capital/config/getall.
type CoinInfo struct {
	Coin                    string    `json:"coin"`
	Name                    string    `json:"name"`
	NetworkList             []Network `json:"networkList"`
	MinConfirm              int       `json:"minConfirm"`                        // Deprecated according to some docs
	WithdrawIntegerMultiple string    `json:"withdrawIntegerMultiple,omitempty"` // String?
	IsDefault               int       `json:"isDefault,omitempty"`               // Check type
}

// Network defines network details for a coin.
type Network struct {
	Network                 string `json:"network"`
	Coin                    string `json:"coin"` // Redundant? Check API response
	Name                    string `json:"name"` // Network name e.g. "Ethereum (ERC20)"
	WithdrawEnable          bool   `json:"withdrawEnable"`
	DepositEnable           bool   `json:"depositEnable"`
	WithdrawFee             string `json:"withdrawFee"`
	WithdrawMin             string `json:"withdrawMin"`
	WithdrawMax             string `json:"withdrawMax"`
	DepositMin              string `json:"depositMin"`
	MinConfirm              int    `json:"minConfirm"`         // Confirmations required for deposit
	UnLockConfirm           int    `json:"unLockConfirm"`      // Confirmations required for unlock
	SameAddress             bool   `json:"sameAddress"`        // If the deposit address is shared between networks
	Contract                string `json:"contract,omitempty"` // Contract address for tokens
	WithdrawIntegerMultiple string `json:"withdrawIntegerMultiple,omitempty"`
	MemoRegex               string `json:"memoRegex,omitempty"`
	DepositDesc             string `json:"depositDesc,omitempty"`
	WithdrawDesc            string `json:"withdrawDesc,omitempty"`
}

// DepositAddress structure for GET /api/v3/capital/deposit/address.
type DepositAddress struct {
	Coin    string `json:"coin"`
	Network string `json:"network"`
	Address string `json:"address"`
	Tag     string `json:"tag,omitempty"` // Memo/Tag if required
	URL     string `json:"url,omitempty"` // URL? Check API response
}

// DepositHistoryRecord structure for GET /api/v3/capital/deposit/hisrec.
// MEXC returns both "network" and "netWork" fields (different capitalization),
// and "unlockConfirm" as a string, not an int.
type DepositHistoryRecord struct {
	Coin          string `json:"coin"`
	Network       string `json:"network"` // Lowercase "n" version
	NetWork       string `json:"netWork"` // Uppercase "W" version (MEXC inconsistency)
	Amount        string `json:"amount"`
	Status        int    `json:"status"` // 0:pending, 1:success, 4:processing, 6:credited/success
	Address       string `json:"address"`
	AddressTag    string `json:"addressTag,omitempty"`
	TxID          string `json:"txId"`
	InsertTime    int64  `json:"insertTime"`
	UnlockConfirm string `json:"unlockConfirm"` // Changed from int to string
	ConfirmTimes  string `json:"confirmTimes"`  // Current confirmations as string
	Memo          string `json:"memo,omitempty"`
	TransHash     string `json:"transHash,omitempty"`  // Transaction hash (may be same as txId without output index)
	UpdateTime    int64  `json:"updateTime,omitempty"` // Timestamp of the last status update
}

// Status code reference for deposit status:
// 0 = Pending
// 1 = Success
// 4 = Processing
// 6 = Credited/Success
// 7 = Wrong Deposit
// 8 = Waiting User Confirm

// WithdrawApplyResponse structure for POST /api/v3/capital/withdraw/apply.
type WithdrawApplyResponse struct {
	WithdrawID string `json:"id"` // The withdrawal ID
}

// WithdrawHistoryRecord structure for GET /api/v3/capital/withdraw/history.
type WithdrawHistoryRecord struct {
	ID             string `json:"id"` // Withdrawal ID from apply step
	Coin           string `json:"coin"`
	Network        string `json:"network"`
	NetWork        string `json:"netWork"` // Uppercase "W" version (MEXC inconsistency)
	Address        string `json:"address"`
	Amount         string `json:"amount"`
	TransactionFee string `json:"transactionFee"`
	Memo           string `json:"memo,omitempty"` // Address tag
	Status         int    `json:"status"`         // 0: PENDING, 1: PROCESSING, 2: FAILED, 3: SUCCESS, 4: PENDING_CANCEL, 5: CANCELED
	TxID           string `json:"txId,omitempty"`
	ApplyTime      string `json:"applyTime"` // Timestamp as string?
}
