// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package eth

import (
	"bytes"
	"crypto/ecdsa"
	"fmt"

	"decred.org/dcrdex/dex"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/accounts/keystore"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
)

// ethLogger satisfies geth's logger interface.
type ethLogger struct {
	dl dex.Logger
}

// New returns a new Logger that has this logger's context plus the given
// context.
func (el *ethLogger) New(ctx ...any) log.Logger {
	s := ""
	for _, v := range ctx {
		if s == "" {
			s = fmt.Sprintf("%s", v)
			continue
		}
		s = fmt.Sprintf("%s: %s", s, v)
	}
	l := el.dl.SubLogger(s)
	return &ethLogger{dl: l}
}

// dummyHandler is used to return from the logger's GetHandler method in order
// to avoid null pointer errors in case geth ever uses that function.
type dummyHandler struct{}

// Log does nothing and returns nil.
func (dummyHandler) Log(r *log.Record) error {
	return nil
}

// Check that *dummyHandler satisfies the log.Handler interface.
var _ log.Handler = (*dummyHandler)(nil)

// GetHandler gets the handler associated with the logger. Unused in geth. Does
// nothing and returns a nil interface here.
func (el *ethLogger) GetHandler() log.Handler {
	return new(dummyHandler)
}

// SetHandler updates the logger to write records to the specified handler.
// Used during setup in geth when a logger is not supplied. Does nothing here.
func (el *ethLogger) SetHandler(h log.Handler) {}

func formatEthLog(msg string, ctx ...any) []any {
	msgs := []any{msg, "         "}
	alternator := 0
	for _, v := range ctx {
		deliminator := "="
		if alternator == 0 {
			deliminator = " "
		}
		alternator = 1 - alternator
		msgs = append(msgs, deliminator, v)
	}
	return msgs
}

// Trace logs at Trace level.
func (el *ethLogger) Trace(msg string, ctx ...any) {
	el.dl.Trace(formatEthLog(msg, ctx...)...)
}

// Debug logs at debug level.
func (el *ethLogger) Debug(msg string, ctx ...any) {
	el.dl.Debug(formatEthLog(msg, ctx...)...)
}

// Info logs at info level.
func (el *ethLogger) Info(msg string, ctx ...any) {
	el.dl.Info(formatEthLog(msg, ctx...)...)
}

// Warn logs at warn level.
func (el *ethLogger) Warn(msg string, ctx ...any) {
	el.dl.Warn(formatEthLog(msg, ctx...)...)
}

// Error logs at error level.
func (el *ethLogger) Error(msg string, ctx ...any) {
	el.dl.Error(formatEthLog(msg, ctx...)...)
}

// Crit logs at critical level.
func (el *ethLogger) Crit(msg string, ctx ...any) {
	el.dl.Critical(formatEthLog(msg, ctx...)...)
}

// Check that *ethLogger satisfies the log.Logger interface.
var _ log.Logger = (*ethLogger)(nil)

func importKeyToKeyStore(ks *keystore.KeyStore, priv *ecdsa.PrivateKey, pw []byte) error {
	accounts := ks.Accounts()
	if len(accounts) == 0 {
		_, err := ks.ImportECDSA(priv, string(pw))
		return err
	} else if len(accounts) == 1 {
		address := crypto.PubkeyToAddress(priv.PublicKey)
		if !bytes.Equal(accounts[0].Address.Bytes(), address.Bytes()) {
			errMsg := "importKeyToKeyStore: attemping to import account to eth wallet: %v, " +
				"but node already contains imported account: %v"
			return fmt.Errorf(errMsg, address, accounts[0].Address)
		}
	} else {
		return fmt.Errorf("importKeyToKeyStore: eth wallet keystore contains %v accounts", accounts)
	}
	return nil
}

// accountCredentials captures the account-specific geth interfaces.
type accountCredentials struct {
	ks     *keystore.KeyStore
	acct   *accounts.Account
	addr   common.Address
	wallet accounts.Wallet
}

func pathCredentials(dir string) (*accountCredentials, error) {
	// TODO: Use StandardScryptN and StandardScryptP?
	return credentialsFromKeyStore(keystore.NewKeyStore(dir, keystore.LightScryptN, keystore.LightScryptP))

}

func credentialsFromKeyStore(ks *keystore.KeyStore) (*accountCredentials, error) {
	accts := ks.Accounts()
	if len(accts) != 1 {
		return nil, fmt.Errorf("unexpected number of accounts, %d", len(accts))
	}
	acct := accts[0]
	wallets := ks.Wallets()
	if len(wallets) != 1 {
		return nil, fmt.Errorf("unexpected number of wallets, %d", len(wallets))
	}
	return &accountCredentials{
		ks:     ks,
		acct:   &acct,
		addr:   acct.Address,
		wallet: wallets[0],
	}, nil
}

func signData(creds *accountCredentials, data []byte) (sig, pubKey []byte, err error) {
	h := crypto.Keccak256(data)
	sig, err = creds.ks.SignHash(*creds.acct, h)
	if err != nil {
		return nil, nil, err
	}
	if len(sig) != 65 {
		return nil, nil, fmt.Errorf("unexpected signature length %d", len(sig))
	}

	pubKey, err = recoverPubkey(h, sig)
	if err != nil {
		return nil, nil, fmt.Errorf("SignMessage: error recovering pubkey %w", err)
	}

	// Lop off the "recovery id", since we already recovered the pub key and
	// it's not used for validation.
	sig = sig[:64]

	return
}

//
// type Ethereum struct {
// 	 // unexported fields
// 	 APIBackend *EthAPIBackend
// }
//
// --Methods--
// APIs() []rpc.API
// ResetWithGenesisBlock(gb *types.Block)
// Etherbase() (eb common.Address, err error)
// SetEtherbase(etherbase common.Address)
// StartMining(threads int) error
// StopMining()
// StopMining()
// IsMining() bool
// AccountManager() *accounts.Manager
// BlockChain() *core.BlockChain
// TxPool() *core.TxPool
// EventMux() *event.TypeMux
// Engine() consensus.Engine
// ChainDb() ethdb.Database
// IsListening() bool
// Downloader() *downloader.Downloader
// Synced() bool
// ArchiveMode() bool
// BloomIndexer() *core.ChainIndexer
// Protocols() []p2p.Protocol
// Start() error
// Stop() error

//

// type account.Manager struct {
// 	 no exported fields
// }
//
// --Methods--
// Close() error
// Config() *Config
// Backends(kind reflect.Type) []Backend
// Wallets() []Wallet
// walletsNoLock() []Wallet
// Wallet(url string) (Wallet, error)
// Accounts() []common.Address
// Find(account Account) (Wallet, error)
// Subscribe(sink chan<- WalletEvent) event.Subscription

//

// type EthAPIBackend struct {
// 	 no exported fields
// }
//
// --Methods--
// ChainConfig() *params.ChainConfig
// CurrentBlock() *types.Block
// SetHead(number uint64)
// HeaderByNumber(ctx context.Context, number rpc.BlockNumber) (*types.Header, error)
// HeaderByNumberOrHash(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) (*types.Header, error)
// HeaderByHash(ctx context.Context, hash common.Hash) (*types.Header, error)
// BlockByNumber(ctx context.Context, number rpc.BlockNumber) (*types.Block, error)
// BlockByHash(ctx context.Context, hash common.Hash) (*types.Block, error)
// BlockByNumberOrHash(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) (*types.Block, error)
// StateAndHeaderByNumber(ctx context.Context, number rpc.BlockNumber) (*state.StateDB, *types.Header, error)
// StateAndHeaderByNumberOrHash(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) (*state.StateDB, *types.Header, error)
// GetReceipts(ctx context.Context, hash common.Hash) (types.Receipts, error)
// GetLogs(ctx context.Context, hash common.Hash) ([][]*types.Log, error)
// GetTd(ctx context.Context, hash common.Hash) *big.Int
// GetEVM(ctx context.Context, msg core.Message, state *state.StateDB, header *types.Header) (*vm.EVM, func() error, error)
// SubscribeRemovedLogsEvent(ch chan<- core.RemovedLogsEvent) event.Subscription
// SubscribePendingLogsEvent(ch chan<- []*types.Log) event.Subscription
// SubscribeChainEvent(ch chan<- core.ChainEvent) event.Subscription
// SubscribeChainHeadEvent(ch chan<- core.ChainHeadEvent) event.Subscription
// SubscribeChainSideEvent(ch chan<- core.ChainSideEvent) event.Subscription
// SubscribeLogsEvent(ch chan<- []*types.Log) event.Subscription
// SendTx(ctx context.Context, signedTx *types.Transaction) error
// GetPoolTransactions() (types.Transactions, error)
// GetTransaction(ctx context.Context, txHash common.Hash) (*types.Transaction, common.Hash, uint64, uint64, error)
// GetPoolNonce(ctx context.Context, addr common.Address) (uint64, error)
// Stats() (pending int, queued int)
// TxPoolContent() (map[common.Address]types.Transactions, map[common.Address]types.Transactions)
// TxPool() *core.TxPool
// SubscribeNewTxsEvent(ch chan<- core.NewTxsEvent) event.Subscription
// Downloader() *downloader.Downloader
// SuggestPrice(ctx context.Context) (*big.Int, error)
// ChainDb() ethdb.Database
// EventMux() *event.TypeMux
// AccountManager() *accounts.Manager
// ExtRPCEnabled() bool
// UnprotectedAllowed() bool
// RPCGasCap() uint64
// RPCTxFeeCap() float64
// BloomStatus() (uint64, uint64)
// ServiceFilter(ctx context.Context, session *bloombits.MatcherSession)
// Engine() consensus.Engine
// CurrentHeader() *types.Header
// Miner() *miner.Miner
// StartMining(threads int) error
// StateAtBlock(ctx context.Context, block *types.Block, reexec uint64) (*state.StateDB, func(), error)
// StatesInRange(ctx context.Context, fromBlock *types.Block, toBlock *types.Block, reexec uint64) ([]*state.StateDB, func(), error)
// StateAtTransaction(ctx context.Context, block *types.Block, txIndex int, reexec uint64) (core.Message, vm.BlockContext, *state.StateDB, func(), error)

//

// type Wallet interface {
// 	URL() URL
// 	Status() (string, error)
// 	Open(passphrase string) error
// 	Close() error
// 	Accounts() []Account
// 	Contains(account Account) bool
// 	Derive(path DerivationPath, pin bool) (Account, error)
// 	SelfDerive(bases []DerivationPath, chain ethereum.ChainStateReader)
// 	SignData(account Account, mimeType string, data []byte) ([]byte, error)
// 	SignDataWithPassphrase(account Account, passphrase, mimeType string, data []byte) ([]byte, error)
// 	SignText(account Account, text []byte) ([]byte, error)
// 	SignTextWithPassphrase(account Account, passphrase string, hash []byte) ([]byte, error)
// 	SignTx(account Account, tx *types.Transaction, chainID *big.Int) (*types.Transaction, error)
// 	SignTxWithPassphrase(account Account, passphrase string, tx *types.Transaction, chainID *big.Int) (*types.Transaction, error)
// }

//

// type Account struct {
// 	Address common.Address `json:"address"` // Ethereum account address derived from the key
// 	URL     URL            `json:"url"`     // Optional resource locator within a backend
// }
//

//

// type Client struct { // ethclient.client
// 	 no exported fields
// }
//
// --Methods--
// Close()
// ChainID(ctx context.Context) (*big.Int, error)
// BlockByHash(ctx context.Context, hash common.Hash) (*types.Block, error)
// BlockByNumber(ctx context.Context, number *big.Int) (*types.Block, error)
// BlockNumber(ctx context.Context) (uint64, error)
// TransactionCount(ctx context.Context, blockHash common.Hash) (uint, error)
// TransactionInBlock(ctx context.Context, blockHash common.Hash, index uint) (*types.Transaction, error)
// TransactionReceipt(ctx context.Context, txHash common.Hash) (*types.Receipt, error)
// SyncProgress(ctx context.Context) (*ethereum.SyncProgress, error)
// SubscribeNewHead(ctx context.Context, ch chan<- *types.Header) (ethereum.Subscription, error)
// NetworkID(ctx context.Context) (*big.Int, error)
// BalanceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (*big.Int, error)
// StorageAt(ctx context.Context, account common.Address, key common.Hash, blockNumber *big.Int) ([]byte, error)
// CodeAt(ctx context.Context, account common.Address, blockNumber *big.Int) ([]byte, error)
// NonceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (uint64, error)
// FilterLogs(ctx context.Context, q ethereum.FilterQuery) ([]types.Log, error)
// SubscribeFilterLogs(ctx context.Context, q ethereum.FilterQuery, ch chan<- types.Log) (ethereum.Subscription, error)
// PendingBalanceAt(ctx context.Context, account common.Address) (*big.Int, error)
// PendingStorageAt(ctx context.Context, account common.Address, key common.Hash) ([]byte, error)
// PendingCodeAt(ctx context.Context, account common.Address) ([]byte, error)
// PendingNonceAt(ctx context.Context, account common.Address) (uint64, error)
// PendingTransactionCount(ctx context.Context) (uint, error)
// CallContract(ctx context.Context, msg ethereum.CallMsg, blockNumber *big.Int) ([]byte, error)
// PendingCallContract(ctx context.Context, msg ethereum.CallMsg) ([]byte, error)
// SuggestGasPrice(ctx context.Context) (*big.Int, error)
// EstimateGas(ctx context.Context, msg ethereum.CallMsg) (uint64, error)
// SendTransaction(ctx context.Context, tx *types.Transaction) error
