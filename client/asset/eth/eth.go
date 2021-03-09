package main

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/accounts/keystore"
	"github.com/ethereum/go-ethereum/cmd/utils"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/eth"
	"github.com/ethereum/go-ethereum/eth/downloader"
	"github.com/ethereum/go-ethereum/eth/ethconfig"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/p2p/nat"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rpc"
)

type ETH struct {
	ctx       context.Context
	ethClient *ethclient.Client
	rpcClient *rpc.Client
	shutdown  context.CancelFunc
	nodeCfg   *node.Config
	node      *node.Node
	eth       *eth.Ethereum
}

func NewETH(appDir string, goerli bool) (*ETH, error) {
	stackConf := &node.Config{DataDir: appDir}

	stackConf.P2P.MaxPeers = 10
	var key *ecdsa.PrivateKey
	var err error
	if key, err = crypto.GenerateKey(); err != nil {
		return nil, err
	}
	stackConf.P2P.PrivateKey = key
	stackConf.P2P.ListenAddr = ":30303"
	stackConf.P2P.NAT = nat.Any()

	var urls []string
	switch {
	case goerli:
		urls = params.GoerliBootnodes
	default:
		urls = params.MainnetBootnodes
	}

	for _, url := range urls {
		node, err := enode.Parse(enode.ValidSchemes, url)
		if err != nil {
			return nil, fmt.Errorf("Bootstrap URL %q invalid: %v", url, err)
		}
		stackConf.P2P.BootstrapNodes = append(stackConf.P2P.BootstrapNodes, node)
	}

	for _, url := range params.V5Bootnodes {
		node, err := enode.Parse(enode.ValidSchemes, url)
		if err != nil {
			return nil, fmt.Errorf("Bootstrap v5 URL %q invalid: %v", url, err)
		}
		stackConf.P2P.BootstrapNodesV5 = append(stackConf.P2P.BootstrapNodesV5, node)
	}

	stack, err := node.New(stackConf)
	if err != nil {
		return nil, err
	}

	ethCfg := ethconfig.Defaults
	if goerli {
		ethCfg.Genesis = core.DefaultGoerliGenesisBlock()
		ethCfg.NetworkId = params.GoerliChainConfig.ChainID.Uint64()
	}

	// ethCfg.Genesis = core.DefaultGenesisBlock() // eth will do this anyway when Genesis == nil is encountered, and the messaging it better that way.
	// ethCfg.SyncMode = downloader.SnapSync // Supposed to be enabled with Berlin on April 14th 2021
	ethCfg.SyncMode = downloader.FastSync // Actually the default, be good for doc'ing.
	ethBackend, err := eth.New(stack, &ethCfg)
	if err != nil {
		return nil, err
	}

	if err := stack.Start(); err != nil {
		return nil, err
	}

	// Create a client to interact with local geth node.
	rpcClient, err := stack.Attach()
	if err != nil {
		utils.Fatalf("Failed to attach to self: %v", err)
	}
	ethClient := ethclient.NewClient(rpcClient)

	ctx, shutdown := context.WithCancel(context.Background())
	return &ETH{
		ctx:       ctx,
		ethClient: ethClient,
		rpcClient: rpcClient,
		shutdown:  shutdown,
		nodeCfg:   stackConf,
		node:      stack,
		eth:       ethBackend,
	}, nil
}

func (eth *ETH) importAccount(pw string, privKeyB []byte) (accounts.Account, error) {
	privKey, err := crypto.ToECDSA(privKeyB)
	if err != nil {
		return accounts.Account{}, fmt.Errorf("Error restoring private key: %v \n", err)
	}

	ks := eth.node.AccountManager().Backends(keystore.KeyStoreType)[0].(*keystore.KeyStore)
	return ks.ImportECDSA(privKey, pw)
}

func (eth *ETH) accountBalance(acct accounts.Account) (*big.Int, error) {
	return eth.ethClient.BalanceAt(eth.ctx, acct.Address, nil)
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
