// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package eth

import (
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"decred.org/dcrdex/dex"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/eth/downloader"
	"github.com/ethereum/go-ethereum/eth/ethconfig"
	"github.com/ethereum/go-ethereum/les"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/p2p/nat"
	"github.com/ethereum/go-ethereum/params"
)

const (
	maxPeers = 10
)

var simnetGenesis string

type nodeConfig struct {
	net                dex.Network
	listenAddr, appDir string
	logger             dex.Logger
}

// ethLogger satisifies geth's logger interface.
type ethLogger struct {
	dl dex.Logger
}

// New returns a new Logger that has this logger's context plus the given
// context.
func (el *ethLogger) New(ctx ...interface{}) log.Logger {
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

func formatEthLog(msg string, ctx ...interface{}) []interface{} {
	msgs := []interface{}{msg, "         "}
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
func (el *ethLogger) Trace(msg string, ctx ...interface{}) {
	el.dl.Trace(formatEthLog(msg, ctx...)...)
}

// Debug logs at debug level.
func (el *ethLogger) Debug(msg string, ctx ...interface{}) {
	el.dl.Debug(formatEthLog(msg, ctx...)...)
}

// Info logs at info level.
func (el *ethLogger) Info(msg string, ctx ...interface{}) {
	el.dl.Info(formatEthLog(msg, ctx...)...)
}

// Warn logs at warn level.
func (el *ethLogger) Warn(msg string, ctx ...interface{}) {
	el.dl.Warn(formatEthLog(msg, ctx...)...)
}

// Error logs at error level.
func (el *ethLogger) Error(msg string, ctx ...interface{}) {
	el.dl.Error(formatEthLog(msg, ctx...)...)
}

// Crit logs at critical level.
func (el *ethLogger) Crit(msg string, ctx ...interface{}) {
	el.dl.Critical(formatEthLog(msg, ctx...)...)
}

// Check that *ethLogger satisfies the log.Logger interface.
var _ log.Logger = (*ethLogger)(nil)

// SetSimnetGenesis should be set before using on simnet. It must be set before
// calling runNode, or a default will be used if found.
func SetSimnetGenesis(sng string) {
	simnetGenesis = sng
}

func runNode(cfg *nodeConfig) (*node.Node, error) {
	stackConf := &node.Config{DataDir: cfg.appDir}

	stackConf.Logger = &ethLogger{dl: cfg.logger}

	stackConf.P2P.MaxPeers = maxPeers
	var key *ecdsa.PrivateKey
	var err error
	if key, err = crypto.GenerateKey(); err != nil {
		return nil, err
	}
	stackConf.P2P.PrivateKey = key
	stackConf.P2P.ListenAddr = cfg.listenAddr
	stackConf.P2P.NAT = nat.Any()

	var urls []string
	switch cfg.net {
	case dex.Simnet:
		urls = []string{
			"enode://897c84f6e4f18195413c1d02927e6a4093f5e7574b52bdec6f20844c4f1f6dd3f16036a9e600bd8681ab50fd8dd144df4a6ba9dd8722bb578a86aaa8222c964f@127.0.0.1:30304", // alpha
			"enode://b1d3e358ee5c9b268e911f2cab47bc12d0e65c80a6d2b453fece34facc9ac3caed14aa3bc7578166bb08c5bc9719e5a2267ae14e0b42da393f4d86f6d5829061@127.0.0.1:30305", // beta
			"enode://b1c14deee09b9d5549c90b7b30a35c812a56bf6afea5873b05d7a1bcd79c7b0848bcfa982faf80cc9e758a3a0d9b470f0a002840d365050fd5bf45052a6ec313@127.0.0.1:30306", // gamma
			"enode://ca414c361d1a38716170923e4900d9dc9203dbaf8fdcaee73e1f861df9fdf20a1453b76fd218c18bc6f3c7e13cbca0b3416af02a53b8e31188faa45aab398d1c@127.0.0.1:30307", // delta
		}
	case dex.Testnet:
		urls = params.GoerliBootnodes
	case dex.Mainnet:
		// urls = params.MainnetBootnodes
		// TODO: Allow.
		return nil, fmt.Errorf("eth cannot be used on mainnet")
	default:
		return nil, fmt.Errorf("unknown network ID: %d", uint8(cfg.net))
	}

	for _, url := range urls {
		node, err := enode.Parse(enode.ValidSchemes, url)
		if err != nil {
			return nil, fmt.Errorf("Bootstrap URL %q invalid: %v", url, err)
		}
		stackConf.P2P.BootstrapNodes = append(stackConf.P2P.BootstrapNodes, node)
	}

	if cfg.net != dex.Simnet {
		for _, url := range params.V5Bootnodes {
			node, err := enode.Parse(enode.ValidSchemes, url)
			if err != nil {
				return nil, fmt.Errorf("Bootstrap v5 URL %q invalid: %v", url, err)
			}
			stackConf.P2P.BootstrapNodesV5 = append(stackConf.P2P.BootstrapNodesV5, node)
		}
	}

	stack, err := node.New(stackConf)
	if err != nil {
		return nil, err
	}

	ethCfg := ethconfig.Defaults
	switch cfg.net {
	case dex.Simnet:
		var sp core.Genesis
		if simnetGenesis == "" {
			homeDir := os.Getenv("HOME")
			genesisFile := filepath.Join(homeDir, "dextest", "eth", "genesis.json")
			genBytes, err := ioutil.ReadFile(genesisFile)
			if err != nil {
				return nil, fmt.Errorf("error reading genesis file: %v", err)
			}
			genLen := len(genBytes)
			if genLen == 0 {
				return nil, fmt.Errorf("no genesis found at %v", genesisFile)
			}
			genBytes = genBytes[:genLen-1]
			SetSimnetGenesis(string(genBytes))
		}
		if err := json.Unmarshal([]byte(simnetGenesis), &sp); err != nil {
			return nil, fmt.Errorf("unable to unmarshal simnet genesis: %v", err)
		}
		ethCfg.Genesis = &sp
		ethCfg.NetworkId = 42
	case dex.Testnet:
		ethCfg.Genesis = core.DefaultGoerliGenesisBlock()
		ethCfg.NetworkId = params.GoerliChainConfig.ChainID.Uint64()
	case dex.Mainnet:
		// urls = params.MainnetBootnodes
		// TODO: Allow.
		return nil, fmt.Errorf("eth cannot be used on mainnet")
	default:
		return nil, fmt.Errorf("unknown network ID: %d", uint8(cfg.net))
	}

	ethCfg.SyncMode = downloader.LightSync

	if _, err := les.New(stack, &ethCfg); err != nil {
		return nil, err
	}

	if err := stack.Start(); err != nil {
		return nil, err
	}

	return stack, nil
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
