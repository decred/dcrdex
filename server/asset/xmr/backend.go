package xmr

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/server/asset"
	"github.com/dev-warrior777/go-monero/old_rpc"
	"github.com/dev-warrior777/go-monero/rpc"
)

type rpcDaemon struct {
	sync.RWMutex
	address      string
	synchronized bool
	restricted   bool
	height       uint64
	blockHash    string
}

var _ asset.Backend = (*Backend)(nil)

type Backend struct {
	ctx           context.Context
	net           dex.Network
	log           dex.Logger
	daemon        *rpc.Client
	oldDaemon     *old_rpc.Client
	blockChansMtx sync.RWMutex
	blockChans    map[chan *asset.BlockUpdate]struct{}
	daemonState   *rpcDaemon
}

// NewBackend generates the network parameters and creates an xmr backend.
func NewBackend(cfg *asset.BackendConfig) (asset.Backend, error) {
	daemons := getTrustedDaemons(cfg.Net, false)
	if len(daemons) <= 0 {
		return nil, fmt.Errorf("no daemons for new daemon requests")
	}
	daemonAddr := daemons[0]
	return &Backend{
		ctx: nil,
		net: cfg.Net,
		log: cfg.Logger.SubLogger("XMRB"),
		daemon: rpc.New(rpc.Config{
			Address: daemonAddr + "/json_rpc",
			Client:  &http.Client{},
		}),
		oldDaemon: old_rpc.New(old_rpc.Config{
			Address: daemonAddr,
			Client:  &http.Client{},
		}),
		daemonState: &rpcDaemon{
			address:      daemonAddr,
			synchronized: false,
			restricted:   false,
			height:       0,
			blockHash:    "",
		},
		blockChans: make(map[chan *asset.BlockUpdate]struct{}),
	}, nil
}

var ErrNotImpl = errors.New("not implemented for XMR")

func (b *Backend) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	var wg sync.WaitGroup
	b.ctx = ctx
	// try daemon
	_, err := b.getInfo()
	if err != nil {
		return nil, err
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		b.monitorDaemon(ctx)
	}()
	return &wg, nil
}

// BlockChannel creates and returns a new channel on which to receive updates
// when new blocks are connected.
func (b *Backend) BlockChannel(size int) <-chan *asset.BlockUpdate {
	fmt.Println("BlockChannel called")
	c := make(chan *asset.BlockUpdate, size)
	b.blockChansMtx.Lock()
	defer b.blockChansMtx.Unlock()
	b.blockChans[c] = struct{}{}
	return c
}

// FeeRate returns the current optimal fee rate in atoms / byte.
func (b *Backend) FeeRate(_ context.Context) (uint64, error) {
	return b.getFeeRate()
}

// Synced should return true when the blockchain is synced and ready for
// fee rate estimation.
func (b *Backend) Synced() (bool, error) {
	info, err := b.getInfo()
	if err != nil {
		return false, err
	}
	return info.Sychronized, nil
}

// Info provides auxiliary information about a backend.
func (b *Backend) Info() *asset.BackendInfo {
	return &asset.BackendInfo{
		SupportsDynamicTxFee: false,
	}
}

///////////////////////////////////////////
// See if needed for xmr specific checks //
///////////////////////////////////////////

// TxData fetches the raw transaction data for the specified coin.
func (b *Backend) TxData(coinID []byte) ([]byte, error) {
	return nil, ErrNotImpl // _can_ impl from old rpc server api with coin id as tx hash
}

// Redemption returns a Coin for redemptionID, a transaction input, that
// spends contract ID, an output containing the swap contract.
func (b *Backend) Redemption(redemptionID []byte, contractID []byte, contractData []byte) (asset.Coin, error) {
	return nil, ErrNotImpl // maybe implement
}

// ValidateSecret checks that the secret satisfies the contract.
func (b *Backend) ValidateSecret(secret []byte, contractData []byte) bool {
	return false // maybe implement
}

// CheckSwapAddress checks that the given address is parseable, and suitable
// as a redeem address in a swap contract script or initiation.
func (b *Backend) CheckSwapAddress(_ string) bool {
	return false // probably implement
}

// ValidateCoinID checks the coinID to ensure it can be decoded, returning a
// human-readable string if it is valid.
func (b *Backend) ValidateCoinID(coinID []byte) (string, error) {
	return "", ErrNotImpl // probably implement
}

/////////////////////////////
// Not implemented for XMR //
/////////////////////////////

// ValidateFeeRate checks that the transaction fees used to initiate the
// contract are sufficient.
func (b *Backend) ValidateFeeRate(coin asset.Coin, reqFeeRate uint64) bool {
	return false
}

// Contract returns a Contract only for outputs that would be spendable on
// the blockchain immediately.
func (b *Backend) Contract(coinID []byte, contractData []byte) (*asset.Contract, error) {
	return nil, ErrNotImpl
}

// ValidateContract ensures that the swap contract is constructed properly
// for the asset.
func (b *Backend) ValidateContract(contract []byte) error {
	return ErrNotImpl
}
