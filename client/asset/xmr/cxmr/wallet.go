// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build xmr

// Package cxmr provides Go bindings to monero_c, a C wrapper around
// Monero's wallet2 library.
package cxmr

/*
#cgo LDFLAGS: -lwallet2_api_c
#cgo linux,amd64 LDFLAGS: -L${SRCDIR}/../lib/linux-amd64 -Wl,-rpath,$ORIGIN -Wl,-rpath,/usr/lib/bisonw
#cgo darwin,amd64 LDFLAGS: -L${SRCDIR}/../lib/darwin-amd64
#cgo darwin,arm64 LDFLAGS: -L${SRCDIR}/../lib/darwin-arm64
#cgo windows,amd64 LDFLAGS: -L${SRCDIR}/../lib/windows-amd64

// Network types
#define NetworkType_MAINNET  0
#define NetworkType_TESTNET  1
#define NetworkType_STAGENET 2

// Transaction priority (fee levels)
#define Priority_Default 0
#define Priority_Low     1
#define Priority_Medium  2
#define Priority_High    3
#define Priority_Last    4

// Wallet status
#define WalletStatus_Ok       0
#define WalletStatus_Error    1
#define WalletStatus_Critical 2

// Connection status
#define WalletConnectionStatus_Disconnected  0
#define WalletConnectionStatus_Connected     1
#define WalletConnectionStatus_WrongVersion  2

// PendingTransaction status
#define PendingTransactionStatus_Ok       0
#define PendingTransactionStatus_Error    1
#define PendingTransactionStatus_Critical 2

// Log levels
#define LogLevel_Silent -1
#define LogLevel_0       0
#define LogLevel_1       1
#define LogLevel_2       2
#define LogLevel_3       3
#define LogLevel_4       4

#include <stdint.h>
#include <stdbool.h>
#include <stdlib.h>

// WalletManagerFactory
extern void* MONERO_WalletManagerFactory_getWalletManager();
extern void MONERO_WalletManagerFactory_setLogLevel(int level);
extern void MONERO_WalletManagerFactory_setLogCategories(const char* categories);

// WalletManager
extern void* MONERO_WalletManager_createWallet(void* wm_ptr, const char* path, const char* password, const char* language, int networkType);
extern void* MONERO_WalletManager_openWallet(void* wm_ptr, const char* path, const char* password, int networkType);
extern void* MONERO_WalletManager_recoveryWallet(void* wm_ptr, const char* path, const char* password, const char* mnemonic, int networkType, uint64_t restoreHeight, uint64_t kdfRounds, const char* seedOffset);
extern void* MONERO_WalletManager_createWalletFromKeys(void* wm_ptr, const char* path, const char* password, const char* language, int nettype, uint64_t restoreHeight, const char* addressString, const char* viewKeyString, const char* spendKeyString, uint64_t kdf_rounds);
extern void* MONERO_WalletManager_createDeterministicWalletFromSpendKey(void* wm_ptr, const char* path, const char* password, const char* language, int nettype, uint64_t restoreHeight, const char* spendKeyString, uint64_t kdf_rounds);
extern bool MONERO_WalletManager_closeWallet(void* wm_ptr, void* wallet_ptr, bool store);
extern bool MONERO_WalletManager_walletExists(void* wm_ptr, const char* path);
extern const char* MONERO_WalletManager_errorString(void* wm_ptr);

// Wallet - Status/Error
extern int MONERO_Wallet_status(void* wallet_ptr);
extern const char* MONERO_Wallet_errorString(void* wallet_ptr);

// Wallet - Connection/Sync
extern bool MONERO_Wallet_init(void* wallet_ptr, const char* daemon_address, uint64_t upper_transaction_size_limit, const char* daemon_username, const char* daemon_password, bool use_ssl, bool lightWallet, const char* proxy_address);
extern bool MONERO_Wallet_connectToDaemon(void* wallet_ptr);
extern int MONERO_Wallet_connected(void* wallet_ptr);
extern void MONERO_Wallet_setTrustedDaemon(void* wallet_ptr, bool arg);
extern bool MONERO_Wallet_trustedDaemon(void* wallet_ptr);
extern void MONERO_Wallet_setAllowMismatchedDaemonVersion(void* wallet_ptr, bool allow);
extern bool MONERO_Wallet_setProxy(void* wallet_ptr, const char* address);
extern bool MONERO_Wallet_refresh(void* wallet_ptr);
extern void MONERO_Wallet_refreshAsync(void* wallet_ptr);
extern bool MONERO_Wallet_rescanBlockchain(void* wallet_ptr);
extern void MONERO_Wallet_rescanBlockchainAsync(void* wallet_ptr);
extern void MONERO_Wallet_setAutoRefreshInterval(void* wallet_ptr, int millis);
extern int MONERO_Wallet_autoRefreshInterval(void* wallet_ptr);
extern void MONERO_Wallet_startRefresh(void* wallet_ptr);
extern void MONERO_Wallet_pauseRefresh(void* wallet_ptr);
extern bool MONERO_Wallet_synchronized(void* wallet_ptr);
extern uint64_t MONERO_Wallet_blockChainHeight(void* wallet_ptr);
extern uint64_t MONERO_Wallet_daemonBlockChainHeight(void* wallet_ptr);
extern uint64_t MONERO_Wallet_approximateBlockChainHeight(void* wallet_ptr);
extern void MONERO_Wallet_setRefreshFromBlockHeight(void* wallet_ptr, uint64_t refresh_from_block_height);
extern uint64_t MONERO_Wallet_getRefreshFromBlockHeight(void* wallet_ptr);
extern void MONERO_Wallet_setRecoveringFromSeed(void* wallet_ptr, bool recoveringFromSeed);

// Wallet - Balance
extern uint64_t MONERO_Wallet_balance(void* wallet_ptr, uint32_t accountIndex);
extern uint64_t MONERO_Wallet_unlockedBalance(void* wallet_ptr, uint32_t accountIndex);

// Wallet - Addresses
extern const char* MONERO_Wallet_address(void* wallet_ptr, uint64_t accountIndex, uint64_t addressIndex);
extern size_t MONERO_Wallet_numSubaddresses(void* wallet_ptr, uint32_t accountIndex);
extern void MONERO_Wallet_addSubaddress(void* wallet_ptr, uint32_t accountIndex, const char* label);
extern const char* MONERO_Wallet_getSubaddressLabel(void* wallet_ptr, uint32_t accountIndex, uint32_t addressIndex);
extern void MONERO_Wallet_setSubaddressLabel(void* wallet_ptr, uint32_t accountIndex, uint32_t addressIndex, const char* label);
extern size_t MONERO_Wallet_numSubaddressAccounts(void* wallet_ptr);
extern void MONERO_Wallet_addSubaddressAccount(void* wallet_ptr, const char* label);

// Wallet - Address Validation (static)
extern bool MONERO_Wallet_addressValid(const char* str, int nettype);

// Wallet - Keys/Seed
extern const char* MONERO_Wallet_seed(void* wallet_ptr, const char* seed_offset);
extern const char* MONERO_Wallet_getSeedLanguage(void* wallet_ptr);
extern void MONERO_Wallet_setSeedLanguage(void* wallet_ptr, const char* arg);
extern const char* MONERO_Wallet_secretViewKey(void* wallet_ptr);
extern const char* MONERO_Wallet_publicViewKey(void* wallet_ptr);
extern const char* MONERO_Wallet_secretSpendKey(void* wallet_ptr);
extern const char* MONERO_Wallet_publicSpendKey(void* wallet_ptr);

// Wallet - Storage
extern bool MONERO_Wallet_store(void* wallet_ptr, const char* path);
extern const char* MONERO_Wallet_filename(void* wallet_ptr);
extern const char* MONERO_Wallet_keysFilename(void* wallet_ptr);
extern const char* MONERO_Wallet_path(void* wallet_ptr);
extern bool MONERO_Wallet_setPassword(void* wallet_ptr, const char* password);

// Wallet - Network
extern int MONERO_Wallet_nettype(void* wallet_ptr);
extern bool MONERO_Wallet_watchOnly(void* wallet_ptr);

// Wallet - Transactions
extern void* MONERO_Wallet_createTransaction(void* wallet_ptr, const char* dst_addr, const char* payment_id, uint64_t amount, uint32_t mixin_count, int pendingTransactionPriority, uint32_t subaddr_account, const char* preferredInputs, const char* separator);
extern void* MONERO_Wallet_createTransactionMultDest(void* wallet_ptr, const char* dst_addr_list, const char* dst_addr_list_separator, const char* payment_id, bool amount_sweep_all, const char* amount_list, const char* amount_list_separator, uint32_t mixin_count, int pendingTransactionPriority, uint32_t subaddr_account, const char* preferredInputs, const char* preferredInputs_separator);
extern void* MONERO_Wallet_history(void* wallet_ptr);

// PendingTransaction
extern int MONERO_PendingTransaction_status(void* pendingTx_ptr);
extern const char* MONERO_PendingTransaction_errorString(void* pendingTx_ptr);
extern bool MONERO_PendingTransaction_commit(void* pendingTx_ptr, const char* filename, bool overwrite);
extern uint64_t MONERO_PendingTransaction_amount(void* pendingTx_ptr);
extern uint64_t MONERO_PendingTransaction_fee(void* pendingTx_ptr);
extern const char* MONERO_PendingTransaction_txid(void* pendingTx_ptr, const char* separator);
extern uint64_t MONERO_PendingTransaction_txCount(void* pendingTx_ptr);
extern const char* MONERO_PendingTransaction_hex(void* pendingTx_ptr, const char* separator);

// TransactionHistory
extern int MONERO_TransactionHistory_count(void* txHistory_ptr);
extern void* MONERO_TransactionHistory_transaction(void* txHistory_ptr, int index);
extern void MONERO_TransactionHistory_refresh(void* txHistory_ptr);

// TransactionInfo
extern int MONERO_TransactionInfo_direction(void* txInfo_ptr);
extern bool MONERO_TransactionInfo_isPending(void* txInfo_ptr);
extern bool MONERO_TransactionInfo_isFailed(void* txInfo_ptr);
extern uint64_t MONERO_TransactionInfo_amount(void* txInfo_ptr);
extern uint64_t MONERO_TransactionInfo_fee(void* txInfo_ptr);
extern uint64_t MONERO_TransactionInfo_blockHeight(void* txInfo_ptr);
extern uint64_t MONERO_TransactionInfo_confirmations(void* txInfo_ptr);
extern const char* MONERO_TransactionInfo_hash(void* txInfo_ptr);
extern uint64_t MONERO_TransactionInfo_timestamp(void* txInfo_ptr);

// Coins
extern void* MONERO_Wallet_coins(void* wallet_ptr);
extern void MONERO_Coins_refresh(void* coins_ptr);
extern int MONERO_Coins_count(void* coins_ptr);
extern void* MONERO_Coins_coin(void* coins_ptr, int index);
extern uint64_t MONERO_CoinsInfo_amount(void* coinsInfo_ptr);
extern bool MONERO_CoinsInfo_spent(void* coinsInfo_ptr);
extern bool MONERO_CoinsInfo_unlocked(void* coinsInfo_ptr);
extern bool MONERO_CoinsInfo_frozen(void* coinsInfo_ptr);
extern uint32_t MONERO_CoinsInfo_subaddrAccount(void* coinsInfo_ptr);

// Utility
extern void MONERO_free(void* ptr);
extern const char* MONERO_Wallet_displayAmount(uint64_t amount);
extern uint64_t MONERO_Wallet_amountFromString(const char* amount);
*/
import "C"
import (
	"errors"
	"sync"
	"unsafe"
)

// NetworkType represents a Monero network type.
type NetworkType int

// Network types
const (
	NetworkMainnet  NetworkType = C.NetworkType_MAINNET
	NetworkTestnet  NetworkType = C.NetworkType_TESTNET
	NetworkStagenet NetworkType = C.NetworkType_STAGENET
)

// Priority represents a transaction priority level.
type Priority int

// Transaction priority levels
const (
	PriorityDefault Priority = C.Priority_Default
	PriorityLow     Priority = C.Priority_Low
	PriorityMedium  Priority = C.Priority_Medium
	PriorityHigh    Priority = C.Priority_High
	PriorityLast    Priority = C.Priority_Last
)

// WalletStatus represents a wallet status code.
type WalletStatus int

// Wallet status codes
const (
	StatusOk       WalletStatus = C.WalletStatus_Ok
	StatusError    WalletStatus = C.WalletStatus_Error
	StatusCritical WalletStatus = C.WalletStatus_Critical
)

// ConnectionStatus represents a wallet connection status.
type ConnectionStatus int

// Connection status codes
const (
	ConnectionDisconnected ConnectionStatus = C.WalletConnectionStatus_Disconnected
	ConnectionConnected    ConnectionStatus = C.WalletConnectionStatus_Connected
	ConnectionWrongVersion ConnectionStatus = C.WalletConnectionStatus_WrongVersion
)

// Direction represents a transaction direction.
type Direction int

// Transaction direction
const (
	DirectionIn  Direction = 0
	DirectionOut Direction = 1
)

// WalletManager manages wallet instances.
type WalletManager struct {
	ptr unsafe.Pointer
}

var (
	walletManager     *WalletManager
	walletManagerOnce sync.Once
)

// GetWalletManager returns the singleton WalletManager instance.
func GetWalletManager() *WalletManager {
	walletManagerOnce.Do(func() {
		ptr := C.MONERO_WalletManagerFactory_getWalletManager()
		walletManager = &WalletManager{ptr: ptr}
	})
	return walletManager
}

// SetLogLevel sets the log level for the Monero library.
func SetLogLevel(level int) {
	C.MONERO_WalletManagerFactory_setLogLevel(C.int(level))
}

// SetLogCategories sets the log categories for the Monero library.
func SetLogCategories(categories string) {
	cCategories := C.CString(categories)
	defer C.free(unsafe.Pointer(cCategories))
	C.MONERO_WalletManagerFactory_setLogCategories(cCategories)
}

// CreateWallet creates a new wallet.
func (wm *WalletManager) CreateWallet(path, password, language string, networkType NetworkType) (*Wallet, error) {
	cPath := C.CString(path)
	cPassword := C.CString(password)
	cLanguage := C.CString(language)
	defer C.free(unsafe.Pointer(cPath))
	defer C.free(unsafe.Pointer(cPassword))
	defer C.free(unsafe.Pointer(cLanguage))

	ptr := C.MONERO_WalletManager_createWallet(wm.ptr, cPath, cPassword, cLanguage, C.int(networkType))
	if ptr == nil {
		return nil, errors.New(wm.ErrorString())
	}

	w := &Wallet{ptr: ptr}
	if w.Status() != StatusOk {
		return nil, errors.New(w.ErrorString())
	}
	return w, nil
}

// OpenWallet opens an existing wallet.
func (wm *WalletManager) OpenWallet(path, password string, networkType NetworkType) (*Wallet, error) {
	cPath := C.CString(path)
	cPassword := C.CString(password)
	defer C.free(unsafe.Pointer(cPath))
	defer C.free(unsafe.Pointer(cPassword))

	ptr := C.MONERO_WalletManager_openWallet(wm.ptr, cPath, cPassword, C.int(networkType))
	if ptr == nil {
		return nil, errors.New(wm.ErrorString())
	}

	w := &Wallet{ptr: ptr}
	if w.Status() != StatusOk {
		return nil, errors.New(w.ErrorString())
	}
	return w, nil
}

// RecoveryWallet recovers a wallet from a mnemonic seed.
func (wm *WalletManager) RecoveryWallet(path, password, mnemonic string, networkType NetworkType, restoreHeight uint64) (*Wallet, error) {
	cPath := C.CString(path)
	cPassword := C.CString(password)
	cMnemonic := C.CString(mnemonic)
	cSeedOffset := C.CString("")
	defer C.free(unsafe.Pointer(cPath))
	defer C.free(unsafe.Pointer(cPassword))
	defer C.free(unsafe.Pointer(cMnemonic))
	defer C.free(unsafe.Pointer(cSeedOffset))

	ptr := C.MONERO_WalletManager_recoveryWallet(
		wm.ptr, cPath, cPassword, cMnemonic,
		C.int(networkType), C.uint64_t(restoreHeight),
		1, // kdfRounds
		cSeedOffset,
	)
	if ptr == nil {
		return nil, errors.New(wm.ErrorString())
	}

	w := &Wallet{ptr: ptr}
	if w.Status() != StatusOk {
		return nil, errors.New(w.ErrorString())
	}
	return w, nil
}

// CreateWalletFromKeys creates a wallet from view and spend keys.
func (wm *WalletManager) CreateWalletFromKeys(path, password, language string, networkType NetworkType, restoreHeight uint64, address, viewKey, spendKey string) (*Wallet, error) {
	cPath := C.CString(path)
	cPassword := C.CString(password)
	cLanguage := C.CString(language)
	cAddress := C.CString(address)
	cViewKey := C.CString(viewKey)
	cSpendKey := C.CString(spendKey)
	defer C.free(unsafe.Pointer(cPath))
	defer C.free(unsafe.Pointer(cPassword))
	defer C.free(unsafe.Pointer(cLanguage))
	defer C.free(unsafe.Pointer(cAddress))
	defer C.free(unsafe.Pointer(cViewKey))
	defer C.free(unsafe.Pointer(cSpendKey))

	ptr := C.MONERO_WalletManager_createWalletFromKeys(
		wm.ptr, cPath, cPassword, cLanguage,
		C.int(networkType), C.uint64_t(restoreHeight),
		cAddress, cViewKey, cSpendKey,
		1, // kdfRounds
	)
	if ptr == nil {
		return nil, errors.New(wm.ErrorString())
	}

	w := &Wallet{ptr: ptr}
	if w.Status() != StatusOk {
		return nil, errors.New(w.ErrorString())
	}
	return w, nil
}

// CreateDeterministicWalletFromSpendKey creates a deterministic wallet from a spend key.
// The spend key should be a 32-byte hex-encoded string.
func (wm *WalletManager) CreateDeterministicWalletFromSpendKey(path, password, language string, networkType NetworkType, restoreHeight uint64, spendKey string) (*Wallet, error) {
	cPath := C.CString(path)
	cPassword := C.CString(password)
	cLanguage := C.CString(language)
	cSpendKey := C.CString(spendKey)
	defer C.free(unsafe.Pointer(cPath))
	defer C.free(unsafe.Pointer(cPassword))
	defer C.free(unsafe.Pointer(cLanguage))
	defer C.free(unsafe.Pointer(cSpendKey))

	ptr := C.MONERO_WalletManager_createDeterministicWalletFromSpendKey(
		wm.ptr, cPath, cPassword, cLanguage,
		C.int(networkType), C.uint64_t(restoreHeight),
		cSpendKey,
		1, // kdfRounds
	)
	if ptr == nil {
		return nil, errors.New(wm.ErrorString())
	}

	w := &Wallet{ptr: ptr}
	if w.Status() != StatusOk {
		return nil, errors.New(w.ErrorString())
	}
	return w, nil
}

// CloseWallet closes a wallet, optionally storing it first.
func (wm *WalletManager) CloseWallet(w *Wallet, store bool) bool {
	return bool(C.MONERO_WalletManager_closeWallet(wm.ptr, w.ptr, C.bool(store)))
}

// WalletExists checks if a wallet exists at the given path.
func (wm *WalletManager) WalletExists(path string) bool {
	cPath := C.CString(path)
	defer C.free(unsafe.Pointer(cPath))
	return bool(C.MONERO_WalletManager_walletExists(wm.ptr, cPath))
}

// ErrorString returns the last error message from the wallet manager.
func (wm *WalletManager) ErrorString() string {
	return C.GoString(C.MONERO_WalletManager_errorString(wm.ptr))
}

// Wallet represents a Monero wallet.
type Wallet struct {
	ptr unsafe.Pointer
}

// Status returns the wallet status code.
func (w *Wallet) Status() WalletStatus {
	return WalletStatus(C.MONERO_Wallet_status(w.ptr))
}

// ErrorString returns the last error message.
func (w *Wallet) ErrorString() string {
	return C.GoString(C.MONERO_Wallet_errorString(w.ptr))
}

// Init initializes the wallet with daemon connection parameters.
func (w *Wallet) Init(daemonAddress, daemonUsername, daemonPassword string, useSsl, lightWallet bool, proxyAddress string) bool {
	cDaemonAddr := C.CString(daemonAddress)
	cDaemonUser := C.CString(daemonUsername)
	cDaemonPass := C.CString(daemonPassword)
	cProxyAddr := C.CString(proxyAddress)
	defer C.free(unsafe.Pointer(cDaemonAddr))
	defer C.free(unsafe.Pointer(cDaemonUser))
	defer C.free(unsafe.Pointer(cDaemonPass))
	defer C.free(unsafe.Pointer(cProxyAddr))

	return bool(C.MONERO_Wallet_init(
		w.ptr, cDaemonAddr, 0,
		cDaemonUser, cDaemonPass,
		C.bool(useSsl), C.bool(lightWallet), cProxyAddr,
	))
}

// ConnectToDaemon attempts to connect to the daemon.
func (w *Wallet) ConnectToDaemon() bool {
	return bool(C.MONERO_Wallet_connectToDaemon(w.ptr))
}

// Connected returns the connection status.
func (w *Wallet) Connected() ConnectionStatus {
	return ConnectionStatus(C.MONERO_Wallet_connected(w.ptr))
}

// SetTrustedDaemon sets whether the daemon is trusted.
func (w *Wallet) SetTrustedDaemon(trusted bool) {
	C.MONERO_Wallet_setTrustedDaemon(w.ptr, C.bool(trusted))
}

// TrustedDaemon returns whether the daemon is trusted.
func (w *Wallet) TrustedDaemon() bool {
	return bool(C.MONERO_Wallet_trustedDaemon(w.ptr))
}

// SetAllowMismatchedDaemonVersion allows connection to a daemon running
// a different hard fork version. This is required for regtest/fakechain
// mode where the daemon starts at the latest hard fork from block 0.
func (w *Wallet) SetAllowMismatchedDaemonVersion(allow bool) {
	C.MONERO_Wallet_setAllowMismatchedDaemonVersion(w.ptr, C.bool(allow))
}

// SetProxy sets the proxy address for daemon connections.
func (w *Wallet) SetProxy(address string) bool {
	cAddr := C.CString(address)
	defer C.free(unsafe.Pointer(cAddr))
	return bool(C.MONERO_Wallet_setProxy(w.ptr, cAddr))
}

// Refresh refreshes the wallet from the blockchain.
func (w *Wallet) Refresh() bool {
	return bool(C.MONERO_Wallet_refresh(w.ptr))
}

// RefreshAsync refreshes the wallet asynchronously.
func (w *Wallet) RefreshAsync() {
	C.MONERO_Wallet_refreshAsync(w.ptr)
}

// RescanBlockchain rescans the blockchain from scratch.
// This will rescan all blocks from the restore height, detecting any
// transactions that were previously missed.
func (w *Wallet) RescanBlockchain() bool {
	return bool(C.MONERO_Wallet_rescanBlockchain(w.ptr))
}

// RescanBlockchainAsync rescans the blockchain asynchronously.
func (w *Wallet) RescanBlockchainAsync() {
	C.MONERO_Wallet_rescanBlockchainAsync(w.ptr)
}

// SetAutoRefreshInterval sets the auto-refresh interval in milliseconds.
func (w *Wallet) SetAutoRefreshInterval(millis int) {
	C.MONERO_Wallet_setAutoRefreshInterval(w.ptr, C.int(millis))
}

// AutoRefreshInterval returns the auto-refresh interval in milliseconds.
func (w *Wallet) AutoRefreshInterval() int {
	return int(C.MONERO_Wallet_autoRefreshInterval(w.ptr))
}

// StartRefresh starts automatic refresh.
func (w *Wallet) StartRefresh() {
	C.MONERO_Wallet_startRefresh(w.ptr)
}

// PauseRefresh pauses automatic refresh.
func (w *Wallet) PauseRefresh() {
	C.MONERO_Wallet_pauseRefresh(w.ptr)
}

// Synchronized returns whether the wallet is synchronized with the daemon.
func (w *Wallet) Synchronized() bool {
	return bool(C.MONERO_Wallet_synchronized(w.ptr))
}

// BlockChainHeight returns the current wallet blockchain height.
func (w *Wallet) BlockChainHeight() uint64 {
	return uint64(C.MONERO_Wallet_blockChainHeight(w.ptr))
}

// DaemonBlockChainHeight returns the daemon's blockchain height.
func (w *Wallet) DaemonBlockChainHeight() uint64 {
	return uint64(C.MONERO_Wallet_daemonBlockChainHeight(w.ptr))
}

// ApproximateBlockChainHeight returns an approximate blockchain height.
func (w *Wallet) ApproximateBlockChainHeight() uint64 {
	return uint64(C.MONERO_Wallet_approximateBlockChainHeight(w.ptr))
}

// SetRefreshFromBlockHeight sets the block height to start refresh from.
func (w *Wallet) SetRefreshFromBlockHeight(height uint64) {
	C.MONERO_Wallet_setRefreshFromBlockHeight(w.ptr, C.uint64_t(height))
}

// GetRefreshFromBlockHeight returns the block height refresh starts from.
func (w *Wallet) GetRefreshFromBlockHeight() uint64 {
	return uint64(C.MONERO_Wallet_getRefreshFromBlockHeight(w.ptr))
}

// SetRecoveringFromSeed sets the wallet's recovering-from-seed state.
// This must be called with true when restoring a wallet to ensure proper
// blockchain scanning behavior.
func (w *Wallet) SetRecoveringFromSeed(recovering bool) {
	C.MONERO_Wallet_setRecoveringFromSeed(w.ptr, C.bool(recovering))
}

// Balance returns the total balance for an account.
func (w *Wallet) Balance(accountIndex uint32) uint64 {
	return uint64(C.MONERO_Wallet_balance(w.ptr, C.uint32_t(accountIndex)))
}

// UnlockedBalance returns the unlocked balance for an account.
func (w *Wallet) UnlockedBalance(accountIndex uint32) uint64 {
	return uint64(C.MONERO_Wallet_unlockedBalance(w.ptr, C.uint32_t(accountIndex)))
}

// Address returns the address for a given account and address index.
func (w *Wallet) Address(accountIndex, addressIndex uint64) string {
	cStr := C.MONERO_Wallet_address(w.ptr, C.uint64_t(accountIndex), C.uint64_t(addressIndex))
	return C.GoString(cStr)
}

// NumSubaddresses returns the number of subaddresses for an account.
func (w *Wallet) NumSubaddresses(accountIndex uint32) uint64 {
	return uint64(C.MONERO_Wallet_numSubaddresses(w.ptr, C.uint32_t(accountIndex)))
}

// AddSubaddress adds a new subaddress to an account.
func (w *Wallet) AddSubaddress(accountIndex uint32, label string) {
	cLabel := C.CString(label)
	defer C.free(unsafe.Pointer(cLabel))
	C.MONERO_Wallet_addSubaddress(w.ptr, C.uint32_t(accountIndex), cLabel)
}

// GetSubaddressLabel returns the label for a subaddress.
func (w *Wallet) GetSubaddressLabel(accountIndex, addressIndex uint32) string {
	cStr := C.MONERO_Wallet_getSubaddressLabel(w.ptr, C.uint32_t(accountIndex), C.uint32_t(addressIndex))
	return C.GoString(cStr)
}

// SetSubaddressLabel sets the label for a subaddress.
func (w *Wallet) SetSubaddressLabel(accountIndex, addressIndex uint32, label string) {
	cLabel := C.CString(label)
	defer C.free(unsafe.Pointer(cLabel))
	C.MONERO_Wallet_setSubaddressLabel(w.ptr, C.uint32_t(accountIndex), C.uint32_t(addressIndex), cLabel)
}

// NumSubaddressAccounts returns the number of subaddress accounts.
func (w *Wallet) NumSubaddressAccounts() uint64 {
	return uint64(C.MONERO_Wallet_numSubaddressAccounts(w.ptr))
}

// AddSubaddressAccount adds a new subaddress account.
func (w *Wallet) AddSubaddressAccount(label string) {
	cLabel := C.CString(label)
	defer C.free(unsafe.Pointer(cLabel))
	C.MONERO_Wallet_addSubaddressAccount(w.ptr, cLabel)
}

// AddressValid checks if an address is valid for a given network type.
func AddressValid(address string, networkType NetworkType) bool {
	cAddr := C.CString(address)
	defer C.free(unsafe.Pointer(cAddr))
	return bool(C.MONERO_Wallet_addressValid(cAddr, C.int(networkType)))
}

// Seed returns the wallet's mnemonic seed.
func (w *Wallet) Seed(seedOffset string) string {
	cOffset := C.CString(seedOffset)
	defer C.free(unsafe.Pointer(cOffset))
	cStr := C.MONERO_Wallet_seed(w.ptr, cOffset)
	return C.GoString(cStr)
}

// GetSeedLanguage returns the seed language.
func (w *Wallet) GetSeedLanguage() string {
	return C.GoString(C.MONERO_Wallet_getSeedLanguage(w.ptr))
}

// SetSeedLanguage sets the seed language.
func (w *Wallet) SetSeedLanguage(language string) {
	cLang := C.CString(language)
	defer C.free(unsafe.Pointer(cLang))
	C.MONERO_Wallet_setSeedLanguage(w.ptr, cLang)
}

// SecretViewKey returns the secret view key.
func (w *Wallet) SecretViewKey() string {
	return C.GoString(C.MONERO_Wallet_secretViewKey(w.ptr))
}

// PublicViewKey returns the public view key.
func (w *Wallet) PublicViewKey() string {
	return C.GoString(C.MONERO_Wallet_publicViewKey(w.ptr))
}

// SecretSpendKey returns the secret spend key.
func (w *Wallet) SecretSpendKey() string {
	return C.GoString(C.MONERO_Wallet_secretSpendKey(w.ptr))
}

// PublicSpendKey returns the public spend key.
func (w *Wallet) PublicSpendKey() string {
	return C.GoString(C.MONERO_Wallet_publicSpendKey(w.ptr))
}

// Store saves the wallet to disk.
func (w *Wallet) Store(path string) bool {
	cPath := C.CString(path)
	defer C.free(unsafe.Pointer(cPath))
	return bool(C.MONERO_Wallet_store(w.ptr, cPath))
}

// Filename returns the wallet filename.
func (w *Wallet) Filename() string {
	return C.GoString(C.MONERO_Wallet_filename(w.ptr))
}

// KeysFilename returns the keys filename.
func (w *Wallet) KeysFilename() string {
	return C.GoString(C.MONERO_Wallet_keysFilename(w.ptr))
}

// Path returns the wallet path.
func (w *Wallet) Path() string {
	return C.GoString(C.MONERO_Wallet_path(w.ptr))
}

// SetPassword sets a new password for the wallet.
func (w *Wallet) SetPassword(password string) bool {
	cPass := C.CString(password)
	defer C.free(unsafe.Pointer(cPass))
	return bool(C.MONERO_Wallet_setPassword(w.ptr, cPass))
}

// Nettype returns the network type.
func (w *Wallet) Nettype() int {
	return int(C.MONERO_Wallet_nettype(w.ptr))
}

// WatchOnly returns whether this is a watch-only wallet.
func (w *Wallet) WatchOnly() bool {
	return bool(C.MONERO_Wallet_watchOnly(w.ptr))
}

// PendingTransaction represents a transaction waiting to be committed.
type PendingTransaction struct {
	ptr unsafe.Pointer
}

// CreateTransaction creates a new transaction.
func (w *Wallet) CreateTransaction(destAddr string, amount uint64, priority Priority, accountIndex uint32) (*PendingTransaction, error) {
	cDest := C.CString(destAddr)
	cPaymentID := C.CString("")
	cPreferredInputs := C.CString("")
	cSeparator := C.CString(",")
	defer C.free(unsafe.Pointer(cDest))
	defer C.free(unsafe.Pointer(cPaymentID))
	defer C.free(unsafe.Pointer(cPreferredInputs))
	defer C.free(unsafe.Pointer(cSeparator))

	ptr := C.MONERO_Wallet_createTransaction(
		w.ptr, cDest, cPaymentID, C.uint64_t(amount),
		0, // mixin_count (0 = use default)
		C.int(priority), C.uint32_t(accountIndex),
		cPreferredInputs, cSeparator,
	)
	if ptr == nil {
		return nil, errors.New(w.ErrorString())
	}

	tx := &PendingTransaction{ptr: ptr}
	if tx.Status() != StatusOk {
		return nil, errors.New(tx.ErrorString())
	}
	return tx, nil
}

// SweepAll creates a sweep transaction that sends the entire unlocked balance
// (minus fee) to the destination address, consuming all spendable outputs.
// Uses the createTransactionMultDest API with the amount_sweep_all flag.
// Monero's ignore_fractional_outputs (enabled by default) will skip outputs
// whose value is less than the marginal fee cost of including them.
func (w *Wallet) SweepAll(destAddr string, priority Priority, accountIndex uint32) (*PendingTransaction, error) {
	cDest := C.CString(destAddr)
	cDestSep := C.CString(",")
	cPaymentID := C.CString("")
	cAmounts := C.CString("")
	cAmountSep := C.CString(",")
	cInputs := C.CString("")
	cInputSep := C.CString(",")
	defer C.free(unsafe.Pointer(cDest))
	defer C.free(unsafe.Pointer(cDestSep))
	defer C.free(unsafe.Pointer(cPaymentID))
	defer C.free(unsafe.Pointer(cAmounts))
	defer C.free(unsafe.Pointer(cAmountSep))
	defer C.free(unsafe.Pointer(cInputs))
	defer C.free(unsafe.Pointer(cInputSep))

	ptr := C.MONERO_Wallet_createTransactionMultDest(
		w.ptr, cDest, cDestSep, cPaymentID,
		C.bool(true), // amount_sweep_all
		cAmounts, cAmountSep,
		0, // mixin_count (0 = use default)
		C.int(priority), C.uint32_t(accountIndex),
		cInputs, cInputSep,
	)
	if ptr == nil {
		return nil, errors.New(w.ErrorString())
	}

	tx := &PendingTransaction{ptr: ptr}
	if tx.Status() != StatusOk {
		return nil, errors.New(tx.ErrorString())
	}
	return tx, nil
}

// CoinInfo contains details about a wallet output.
type CoinInfo struct {
	Amount      uint64
	Spent       bool
	Unlocked    bool
	Frozen      bool
	SubaddrAcct uint32
}

// AllCoins returns info about every output in the wallet.
func (w *Wallet) AllCoins() []CoinInfo {
	coinsPtr := C.MONERO_Wallet_coins(w.ptr)
	C.MONERO_Coins_refresh(coinsPtr)
	count := int(C.MONERO_Coins_count(coinsPtr))

	coins := make([]CoinInfo, 0, count)
	for i := 0; i < count; i++ {
		ci := C.MONERO_Coins_coin(coinsPtr, C.int(i))
		if ci == nil {
			continue
		}
		coins = append(coins, CoinInfo{
			Amount:      uint64(C.MONERO_CoinsInfo_amount(ci)),
			Spent:       bool(C.MONERO_CoinsInfo_spent(ci)),
			Unlocked:    bool(C.MONERO_CoinsInfo_unlocked(ci)),
			Frozen:      bool(C.MONERO_CoinsInfo_frozen(ci)),
			SubaddrAcct: uint32(C.MONERO_CoinsInfo_subaddrAccount(ci)),
		})
	}
	return coins
}

// Status returns the transaction status.
func (tx *PendingTransaction) Status() WalletStatus {
	return WalletStatus(C.MONERO_PendingTransaction_status(tx.ptr))
}

// ErrorString returns the error string if status is not OK.
func (tx *PendingTransaction) ErrorString() string {
	return C.GoString(C.MONERO_PendingTransaction_errorString(tx.ptr))
}

// Commit broadcasts the transaction to the network.
func (tx *PendingTransaction) Commit() error {
	cFilename := C.CString("")
	defer C.free(unsafe.Pointer(cFilename))

	if !C.MONERO_PendingTransaction_commit(tx.ptr, cFilename, false) {
		return errors.New(tx.ErrorString())
	}
	return nil
}

// Amount returns the transaction amount.
func (tx *PendingTransaction) Amount() uint64 {
	return uint64(C.MONERO_PendingTransaction_amount(tx.ptr))
}

// Fee returns the transaction fee.
func (tx *PendingTransaction) Fee() uint64 {
	return uint64(C.MONERO_PendingTransaction_fee(tx.ptr))
}

// TxID returns the transaction ID(s).
func (tx *PendingTransaction) TxID() string {
	cSep := C.CString(",")
	defer C.free(unsafe.Pointer(cSep))
	cStr := C.MONERO_PendingTransaction_txid(tx.ptr, cSep)
	return C.GoString(cStr)
}

// TxCount returns the number of transactions.
func (tx *PendingTransaction) TxCount() uint64 {
	return uint64(C.MONERO_PendingTransaction_txCount(tx.ptr))
}

// Hex returns the raw transaction hex.
func (tx *PendingTransaction) Hex() string {
	cSep := C.CString(",")
	defer C.free(unsafe.Pointer(cSep))
	cStr := C.MONERO_PendingTransaction_hex(tx.ptr, cSep)
	return C.GoString(cStr)
}

// TransactionHistory provides access to transaction history.
type TransactionHistory struct {
	ptr unsafe.Pointer
}

// History returns the transaction history.
func (w *Wallet) History() *TransactionHistory {
	ptr := C.MONERO_Wallet_history(w.ptr)
	return &TransactionHistory{ptr: ptr}
}

// Count returns the number of transactions.
func (h *TransactionHistory) Count() int {
	return int(C.MONERO_TransactionHistory_count(h.ptr))
}

// Transaction returns the transaction at the given index.
func (h *TransactionHistory) Transaction(index int) *TransactionInfo {
	ptr := C.MONERO_TransactionHistory_transaction(h.ptr, C.int(index))
	if ptr == nil {
		return nil
	}
	return &TransactionInfo{ptr: ptr}
}

// Refresh refreshes the transaction history from the wallet.
func (h *TransactionHistory) Refresh() {
	C.MONERO_TransactionHistory_refresh(h.ptr)
}

// TransactionInfo contains information about a transaction.
type TransactionInfo struct {
	ptr unsafe.Pointer
}

// Direction returns the transaction direction (in or out).
func (ti *TransactionInfo) Direction() Direction {
	return Direction(C.MONERO_TransactionInfo_direction(ti.ptr))
}

// IsPending returns whether the transaction is pending.
func (ti *TransactionInfo) IsPending() bool {
	return bool(C.MONERO_TransactionInfo_isPending(ti.ptr))
}

// IsFailed returns whether the transaction failed.
func (ti *TransactionInfo) IsFailed() bool {
	return bool(C.MONERO_TransactionInfo_isFailed(ti.ptr))
}

// Amount returns the transaction amount.
func (ti *TransactionInfo) Amount() uint64 {
	return uint64(C.MONERO_TransactionInfo_amount(ti.ptr))
}

// Fee returns the transaction fee.
func (ti *TransactionInfo) Fee() uint64 {
	return uint64(C.MONERO_TransactionInfo_fee(ti.ptr))
}

// BlockHeight returns the block height.
func (ti *TransactionInfo) BlockHeight() uint64 {
	return uint64(C.MONERO_TransactionInfo_blockHeight(ti.ptr))
}

// Confirmations returns the number of confirmations.
func (ti *TransactionInfo) Confirmations() uint64 {
	return uint64(C.MONERO_TransactionInfo_confirmations(ti.ptr))
}

// Hash returns the transaction hash.
func (ti *TransactionInfo) Hash() string {
	return C.GoString(C.MONERO_TransactionInfo_hash(ti.ptr))
}

// Timestamp returns the transaction timestamp.
func (ti *TransactionInfo) Timestamp() uint64 {
	return uint64(C.MONERO_TransactionInfo_timestamp(ti.ptr))
}

// DisplayAmount formats an amount for display.
func DisplayAmount(amount uint64) string {
	cStr := C.MONERO_Wallet_displayAmount(C.uint64_t(amount))
	defer C.MONERO_free(unsafe.Pointer(cStr))
	return C.GoString(cStr)
}

// AmountFromString parses an amount from a string.
func AmountFromString(amount string) uint64 {
	cAmount := C.CString(amount)
	defer C.free(unsafe.Pointer(cAmount))
	return uint64(C.MONERO_Wallet_amountFromString(cAmount))
}
