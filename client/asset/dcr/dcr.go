// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dcr

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
	dexdcr "decred.org/dcrdex/dex/networks/dcr"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrec"
	"github.com/decred/dcrd/dcrec/secp256k1/v2"
	"github.com/decred/dcrd/dcrjson/v3"
	"github.com/decred/dcrd/dcrutil/v2"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types/v2"
	"github.com/decred/dcrd/rpcclient/v5"
	"github.com/decred/dcrd/txscript/v2"
	"github.com/decred/dcrd/wire"
	walletjson "github.com/decred/dcrwallet/rpc/jsonrpc/types"
)

const (
	// BipID is the BIP-0044 asset ID.
	BipID = 42
	// defaultFee is the default value for the fallbackfee.
	defaultFee = 20
	// splitTxBaggage is the total number of additional bytes associated with
	// using a split transaction to fund a swap.
	splitTxBaggage = dexdcr.MsgTxOverhead + dexdcr.P2PKHInputSize + 2*dexdcr.P2PKHOutputSize

	// RawRequest RPC methods
	methodListUnspent        = "listunspent"
	methodListLockUnspent    = "listlockunspent"
	methodSignRawTransaction = "signrawtransaction"
)

var (
	// blockTicker is the delay between calls to check for new blocks.
	blockTicker    = time.Second
	fallbackFeeKey = "fallbackfee"
	configOpts     = []*asset.ConfigOption{
		{
			Key:         "account",
			DisplayName: "Account Name",
			Description: "dcrwallet account name",
		},
		{
			Key:         "username",
			DisplayName: "RPC Username",
			Description: "dcrwallet's 'username' setting for JSON-RPC",
		},
		{
			Key:         "password",
			DisplayName: "RPC Password",
			Description: "dcrwallet's 'password' setting for JSON-RPC",
			NoEcho:      true,
		},
		{
			Key:         "rpclisten",
			DisplayName: "RPC Address",
			Description: "dcrwallet's address (host or host:port) (default port: 9109, testnet: 19109)",
		},
		{
			Key:         "rpccert",
			DisplayName: "TLS Certificate",
			Description: "Path to the dcrwallet TLS certificate file",
		},
		{
			Key:          fallbackFeeKey,
			DisplayName:  "Fallback fee rate",
			Description:  "The fee rate to use for fee payment and withdrawals when estimatesmartfee is not available. Units: DCR/kB",
			DefaultValue: defaultFee * 1000 / 1e8,
		},
		{
			Key:         "txsplit",
			DisplayName: "Pre-size funding inputs",
			Description: "When placing an order, create a \"split\" transaction to fund the order without locking more of the wallet balance than " +
				"necessary. Otherwise, excess funds may be reserved to fund the order until the first swap contract is broadcast " +
				"during match settlement, or the order is canceled. This an extra transaction for which network mining fees are paid. " +
				"Used only for standing-type orders, e.g. limit orders without immediate time-in-force.",
			IsBoolean: true,
		},
	}
	// walletInfo defines some general information about a Decred wallet.
	WalletInfo = &asset.WalletInfo{
		Name:              "Decred",
		Units:             "atoms",
		DefaultConfigPath: defaultConfigPath,
		ConfigOpts:        configOpts,
	}
)

// rpcClient is an rpcclient.Client, or a stub for testing.
type rpcClient interface {
	EstimateSmartFee(confirmations int64, mode chainjson.EstimateSmartFeeMode) (float64, error)
	SendRawTransaction(tx *wire.MsgTx, allowHighFees bool) (*chainhash.Hash, error)
	GetTxOut(txHash *chainhash.Hash, index uint32, mempool bool) (*chainjson.GetTxOutResult, error)
	GetBalanceMinConf(account string, minConfirms int) (*walletjson.GetBalanceResult, error)
	GetBestBlock() (*chainhash.Hash, int64, error)
	GetBlockHash(blockHeight int64) (*chainhash.Hash, error)
	GetBlockVerbose(blockHash *chainhash.Hash, verboseTx bool) (*chainjson.GetBlockVerboseResult, error)
	GetRawMempool(txType chainjson.GetRawMempoolTxTypeCmd) ([]*chainhash.Hash, error)
	GetRawTransactionVerbose(txHash *chainhash.Hash) (*chainjson.TxRawResult, error)
	LockUnspent(unlock bool, ops []*wire.OutPoint) error
	GetRawChangeAddress(account string, net dcrutil.AddressParams) (dcrutil.Address, error)
	GetNewAddressGapPolicy(string, rpcclient.GapPolicy, dcrutil.AddressParams) (dcrutil.Address, error)
	DumpPrivKey(address dcrutil.Address, net [2]byte) (*dcrutil.WIF, error)
	GetTransaction(txHash *chainhash.Hash) (*walletjson.GetTransactionResult, error)
	WalletLock() error
	WalletPassphrase(passphrase string, timeoutSecs int64) error
	Disconnected() bool
	RawRequest(method string, params []json.RawMessage) (json.RawMessage, error)
}

// outPoint is the hash and output index of a transaction output.
type outPoint struct {
	txHash chainhash.Hash
	vout   uint32
}

// newOutPoint is the constructor for a new outPoint.
func newOutPoint(txHash *chainhash.Hash, vout uint32) outPoint {
	return outPoint{
		txHash: *txHash,
		vout:   vout,
	}
}

// String is a human-readable string representation of the outPoint.
func (pt *outPoint) String() string {
	return pt.txHash.String() + ":" + strconv.Itoa(int(pt.vout))
}

// output is information about a transaction output. output satisfies the
// asset.Coin interface.
type output struct {
	pt     outPoint
	tree   int8
	value  uint64
	redeem dex.Bytes
	node   rpcClient // for calculating confirmations.
}

// newOutput is the constructor for an output.
func newOutput(node rpcClient, txHash *chainhash.Hash, vout uint32, value uint64, tree int8, redeem dex.Bytes) *output {
	return &output{
		pt: outPoint{
			txHash: *txHash,
			vout:   vout,
		},
		value:  value,
		redeem: redeem,
		tree:   tree,
		node:   node,
	}
}

// Value returns the value of the output. Part of the asset.Coin interface.
func (op *output) Value() uint64 {
	return op.value
}

// Confirmations is the number of confirmations on the output's block.
// Confirmations always pulls the block information fresh from the blockchain,
// and will return an error if the output has been spent. Part of the
// asset.Coin interface.
func (op *output) Confirmations() (uint32, error) {
	txOut, err := op.node.GetTxOut(op.txHash(), op.vout(), true)
	if err != nil {
		return 0, fmt.Errorf("error finding unspent contract: %v", err)
	}
	if txOut == nil {
		return 0, asset.CoinNotFoundError
	}
	return uint32(txOut.Confirmations), nil
}

// ID is the output's coin ID. Part of the asset.Coin interface. For DCR, the
// coin ID is 36 bytes = 32 bytes tx hash + 4 bytes big-endian vout.
func (op *output) ID() dex.Bytes {
	return toCoinID(op.txHash(), op.vout())
}

// String is a string representation of the coin.
func (op *output) String() string {
	return op.pt.String()
}

// Redeem is any known redeem script required to spend this output. Part of the
// asset.Coin interface.
func (op *output) Redeem() dex.Bytes {
	return op.redeem
}

// txHash returns the pointer of the outPoint's txHash.
func (op *output) txHash() *chainhash.Hash {
	return &op.pt.txHash
}

// vout returns the outPoint's vout.
func (op *output) vout() uint32 {
	return op.pt.vout
}

// wireOutPoint creates and returns a new *wire.OutPoint for the output.
func (op *output) wireOutPoint() *wire.OutPoint {
	return wire.NewOutPoint(op.txHash(), op.vout(), op.tree)
}

// auditInfo is information about a swap contract on the blockchain, not
// necessarily created by this wallet, as would be returned from AuditContract.
// auditInfo satisfies the asset.AuditInfo interface.
type auditInfo struct {
	output     *output
	secretHash []byte
	recipient  dcrutil.Address
	expiration time.Time
}

// Recipient is a base58 string for the contract's receiving address. Part of
// the asset.AuditInfo interface.
func (ci *auditInfo) Recipient() string {
	return ci.recipient.String()
}

// Expiration is the expiration time of the contract, which is the earliest time
// that a refund can be issued for an un-redeemed contract. Part of the
// asset.AuditInfo interface.
func (ci *auditInfo) Expiration() time.Time {
	return ci.expiration
}

// Coin returns the output as an asset.Coin. Part of the asset.AuditInfo
// interface.
func (ci *auditInfo) Coin() asset.Coin {
	return ci.output
}

// SecretHash is the contract's secret hash.
func (ci *auditInfo) SecretHash() dex.Bytes {
	return ci.secretHash
}

// swapReceipt is information about a swap contract that was broadcast by this
// wallet. Satisfies the asset.Receipt interface.
type swapReceipt struct {
	output     *output
	expiration time.Time
}

// Expiration is the time that the contract will expire, allowing the user to
// issue a refund transaction. Part of the asset.Receipt interface.
func (r *swapReceipt) Expiration() time.Time {
	return r.expiration
}

// Coin is the output information as an asset.Coin. Part of the asset.Receipt
// interface.
func (r *swapReceipt) Coin() asset.Coin {
	return r.output
}

// String provides a human-readable representation of the contract's Coin.
func (r *swapReceipt) String() string {
	return r.output.String()
}

// fundingCoin is similar to output, but also stores the address. The
// ExchangeWallet fundingCoins dict is used as a local cache of coins being
// spent.
type fundingCoin struct {
	op   *output
	addr string
}

// Driver implements asset.Driver.
type Driver struct{}

// Setup creates the DCR exchange wallet. Start the wallet with its Run method.
func (d *Driver) Setup(cfg *asset.WalletConfig, logger dex.Logger, network dex.Network) (asset.Wallet, error) {
	return NewWallet(cfg, logger, network)
}

// DecodeCoinID creates a human-readable representation of a coin ID for Decred.
func (d *Driver) DecodeCoinID(coinID []byte) (string, error) {
	txid, vout, err := decodeCoinID(coinID)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%v:%d", txid, vout), err
}

// Info returns basic information about the wallet and asset. WARNING: An
// ExchangeWallet instance may have different DefaultFeeRate set, so use
// (*ExchangeWallet).Info when possible.
func (d *Driver) Info() *asset.WalletInfo {
	return WalletInfo
}

func init() {
	asset.Register(BipID, &Driver{})
}

// ExchangeWallet is a wallet backend for Decred. The backend is how the DEX
// client app communicates with the Decred blockchain and wallet. ExchangeWallet
// satisfies the dex.Wallet interface.
type ExchangeWallet struct {
	client          *rpcclient.Client
	node            rpcClient
	log             dex.Logger
	acct            string
	tipChange       func(error)
	hasConnected    uint32
	fallbackFeeRate uint64
	useSplitTx      bool

	// Coins returned by Fund are cached for quick reference and for cleanup on
	// shutdown.
	fundingMtx   sync.RWMutex
	fundingCoins map[outPoint]*fundingCoin
	splitFunds   map[outPoint][]*fundingCoin
}

// Check that ExchangeWallet satisfies the Wallet interface.
var _ asset.Wallet = (*ExchangeWallet)(nil)

// NewWallet is the exported constructor by which the DEX will import the
// exchange wallet. The wallet will shut down when the provided context is
// canceled.
func NewWallet(cfg *asset.WalletConfig, logger dex.Logger, network dex.Network) (*ExchangeWallet, error) {
	// loadConfig will set fields if defaults are used and set the chainParams
	// package variable.
	walletCfg, err := loadConfig(cfg.Settings, network)
	if err != nil {
		return nil, err
	}

	dcr := unconnectedWallet(cfg, walletCfg, logger)

	logger.Infof("Setting up new DCR wallet at %s with TLS certificate %q.",
		walletCfg.RPCListen, walletCfg.RPCCert)
	dcr.client, err = newClient(walletCfg.RPCListen, walletCfg.RPCUser,
		walletCfg.RPCPass, walletCfg.RPCCert)
	if err != nil {
		return nil, fmt.Errorf("DCR ExchangeWallet.Run error: %v", err)
	}
	// Beyond this point, only node
	dcr.node = dcr.client

	return dcr, nil
}

// unconnectedWallet returns an ExchangeWallet without a node. The node should
// be set before use.
func unconnectedWallet(cfg *asset.WalletConfig, dcrCfg *Config, logger dex.Logger) *ExchangeWallet {
	// If set in the user config, the fallback fee will be in units of DCR/kB.
	// Convert to atoms/B.
	fallbackFeesPerByte := toAtoms(dcrCfg.FallbackFeeRate / 1000)
	if fallbackFeesPerByte == 0 {
		fallbackFeesPerByte = defaultFee
	}
	logger.Tracef("fallback fees set at %d atoms/byte", fallbackFeesPerByte)

	return &ExchangeWallet{
		log:             logger,
		acct:            cfg.Settings["account"],
		tipChange:       cfg.TipChange,
		fundingCoins:    make(map[outPoint]*fundingCoin),
		splitFunds:      make(map[outPoint][]*fundingCoin),
		fallbackFeeRate: fallbackFeesPerByte,
		useSplitTx:      dcrCfg.UseSplitTx,
	}
}

// newClient attempts to create a new websocket connection to a dcrwallet
// instance with the given credentials and notification handlers.
func newClient(host, user, pass, cert string) (*rpcclient.Client, error) {

	certs, err := ioutil.ReadFile(cert)
	if err != nil {
		return nil, fmt.Errorf("TLS certificate read error: %v", err)
	}

	config := &rpcclient.ConnConfig{
		Host:                host,
		Endpoint:            "ws",
		User:                user,
		Pass:                pass,
		Certificates:        certs,
		DisableConnectOnNew: true,
	}

	cl, err := rpcclient.New(config, nil)
	if err != nil {
		return nil, fmt.Errorf("Failed to start dcrwallet RPC client: %v", err)
	}

	return cl, nil
}

// Info returns basic information about the wallet and asset.
func (dcr *ExchangeWallet) Info() *asset.WalletInfo {
	return WalletInfo
}

// Connect connects the wallet to the RPC server. Satisfies the dex.Connector
// interface.
func (dcr *ExchangeWallet) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	err := dcr.client.Connect(ctx, false)
	if err != nil {
		return nil, fmt.Errorf("Decred Wallet connect error: %v", err)
	}
	// Check the min api versions.
	versions, err := dcr.client.Version()
	if err != nil {
		return nil, fmt.Errorf("DCR ExchangeWallet version fetch error: %v", err)
	}
	err = checkVersionInfo(versions)
	if err != nil {
		return nil, fmt.Errorf("DCR ExchangeWallet version check failed: %v", err)
	}
	// If this is the first time connecting, clear the locked coins. This should
	// have been done at shutdown, but shutdown may not have been clean.
	if atomic.SwapUint32(&dcr.hasConnected, 1) == 0 {
		err := dcr.node.LockUnspent(true, nil)
		if err != nil {
			return nil, err
		}
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		dcr.monitorBlocks(ctx)
		dcr.shutdown()
	}()
	return &wg, nil
}

// Balance should return the total available funds in the wallet. Note that
// after calling Fund, the amount returned by Balance may change by more than
// the value funded. Part of the asset.Wallet interface. TODO: Since this
// includes potentially untrusted 0-conf utxos, consider prioritizing confirmed
// utxos when funding an order.
func (dcr *ExchangeWallet) Balance() (*asset.Balance, error) {
	balances, err := dcr.node.GetBalanceMinConf(dcr.acct, 0)
	if err != nil {
		return nil, err
	}
	locked, err := dcr.lockedAtoms()
	if err != nil {
		return nil, err
	}

	var balance asset.Balance
	var acctFound bool
	for i := range balances.Balances {
		ab := &balances.Balances[i]
		if ab.AccountName == dcr.acct {
			acctFound = true
			balance.Available = toAtoms(ab.Spendable) - locked
			balance.Immature = toAtoms(ab.ImmatureCoinbaseRewards) +
				toAtoms(ab.ImmatureStakeGeneration)
			balance.Locked = locked + toAtoms(ab.LockedByTickets)
			break
		}
	}

	if !acctFound {
		return nil, fmt.Errorf("account not found: %q", dcr.acct)
	}

	return &balance, err
}

// FeeRate returns the current optimal fee rate in atoms / byte.
func (dcr *ExchangeWallet) FeeRate() (uint64, error) {
	// estimatesmartfee 1 returns extremely high rates on DCR.
	dcrPerKB, err := dcr.node.EstimateSmartFee(2, chainjson.EstimateSmartFeeConservative)
	if err != nil {
		return 0, err
	}
	atomsPerKB, err := dcrutil.NewAmount(dcrPerKB) // satPerKB is 0 when err != nil
	if err != nil {
		return 0, err
	}
	// Add 1 extra atom/byte, which is both extra conservative and prevents a
	// zero value if the atoms/KB is less than 1000.
	return 1 + uint64(atomsPerKB)/1000, nil // dcrPerKB * 1e8 / 1e3
}

// feeRateWithFallback attempts to get the optimal fee rate in atoms / byte via
// FeeRate. If that fails, it will return the configured fallback fee rate.
func (dcr *ExchangeWallet) feeRateWithFallback() uint64 {
	feeRate, err := dcr.FeeRate()
	if err != nil {
		feeRate = dcr.fallbackFeeRate
		dcr.log.Warnf("Unable to get optimal fee rate, using fallback of %d: %v",
			dcr.fallbackFeeRate, err)
	}
	return feeRate
}

// FundOrder selects coins for use in an order. The coins will be locked, and
// will not be returned in subsequent calls to Fund or calculated in calls to
// Available, unless they are unlocked with ReturnCoins.
func (dcr *ExchangeWallet) FundOrder(value uint64, immediate bool, nfo *dex.Asset) (asset.Coins, error) {
	if value == 0 {
		return nil, fmt.Errorf("cannot fund value = 0")
	}

	//oneInputSize := dexdcr.P2PKHInputSize
	enough := func(sum uint64, size uint32, unspent *compositeUTXO) bool {
		reqFunds := calc.RequiredOrderFunds(value, uint64(size+unspent.input.Size()), nfo)
		// needed fees are reqFunds - value
		return sum+toAtoms(unspent.rpc.Amount) >= reqFunds
	}

	coins, inputsSize, fundingCoins, err := dcr.fund(enough)
	if err != nil {
		return nil, err
	}

	// Send a split, if preferred.
	if dcr.useSplitTx && !immediate {
		return dcr.split(value, coins, inputsSize, fundingCoins, nfo)
	}

	dcr.log.Debugf("funding %d atom order with coins %v", value, coins)

	return coins, nil
}

// unspents fetches unspent outputs for the ExchangeWallet account using rpc
// RawRequest.
func (dcr *ExchangeWallet) unspents() ([]walletjson.ListUnspentResult, error) {
	var unspents []walletjson.ListUnspentResult
	// minconf, maxconf (rpcdefault=9999999), [address], account
	params := anylist{0, 9999999, nil, dcr.acct}
	err := dcr.nodeRawRequest(methodListUnspent, params, &unspents)
	return unspents, err
}

// fund finds coins for the specified value. A function is provided that can
// check whether adding the provided output would be enough to satisfy the
// needed value. Preference is given to selecting coins with 1 or more confs,
// falling back to 0-conf coins where there are not enough 1+ confs coins.
func (dcr *ExchangeWallet) fund(enough func(sum uint64, size uint32, unspent *compositeUTXO) bool) (asset.Coins, uint64, []*fundingCoin, error) {

	unspents, err := dcr.unspents()
	if err != nil {
		return nil, 0, nil, err
	}
	if len(unspents) == 0 {
		return nil, 0, nil, fmt.Errorf("insufficient funds. 0 DCR available in %q account", dcr.acct)
	}

	// Parse utxos to include script size for spending input.
	// Returned utxos will be sorted in ascending order by amount (smallest first).
	utxos, _, err := dcr.parseUTXOs(unspents)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("error parsing unspent outputs: %v", err)
	}

	var sum uint64
	var size uint32
	var coins asset.Coins
	var spents []*fundingCoin

	addUTXO := func(unspent *compositeUTXO) error {
		txHash, err := chainhash.NewHashFromStr(unspent.rpc.TxID)
		if err != nil {
			return fmt.Errorf("error decoding txid: %v", err)
		}
		v := toAtoms(unspent.rpc.Amount)
		redeemScript, err := hex.DecodeString(unspent.rpc.RedeemScript)
		if err != nil {
			return fmt.Errorf("error decoding redeem script for %s, script = %s: %v",
				unspent.rpc.TxID, unspent.rpc.RedeemScript, err)
		}
		op := newOutput(dcr.node, txHash, unspent.rpc.Vout, v, unspent.rpc.Tree, redeemScript)
		coins = append(coins, op)
		spents = append(spents, &fundingCoin{
			op:   op,
			addr: unspent.rpc.Address,
		})
		size += unspent.input.Size()
		sum += v
		return nil
	}

	tryUTXOs := func(minconf int64) (ok bool, err error) {
		sum, size = 0, 0
		coins, spents = nil, nil

		okUTXOs := make([]*compositeUTXO, 0, len(utxos)) // over-allocate
		for _, cu := range utxos {
			if cu.confs >= minconf {
				okUTXOs = append(okUTXOs, cu)
			}
		}

		for {
			// If there are none left, we don't have enough.
			if len(okUTXOs) == 0 {
				return false, nil
			}
			// On each loop, find the smallest UTXO that is enough.
			for _, txout := range okUTXOs {
				if enough(sum, size, txout) {
					if err = addUTXO(txout); err != nil {
						return false, err
					}
					return true, nil
				}
			}
			// No single UTXO was large enough. Add the largest (the last
			// output) and continue.
			if err = addUTXO(okUTXOs[len(okUTXOs)-1]); err != nil {
				return false, err
			}
			// Pop the utxo.
			okUTXOs = okUTXOs[:len(okUTXOs)-1]
		}
	}

	// First try with confs>0.
	ok, err := tryUTXOs(1)
	if err != nil {
		return nil, 0, nil, err
	}
	// Fallback to allowing 0-conf outputs.
	if !ok {
		ok, err = tryUTXOs(0)
		if err != nil {
			return nil, 0, nil, err
		}
		if !ok {
			return nil, 0, nil, fmt.Errorf("not enough to cover requested funds")
		}
	}

	err = dcr.lockFundingCoins(spents)
	if err != nil {
		return nil, 0, nil, err
	}
	return coins, uint64(size), spents, nil
}

// split will send a split transaction and return the sized output. If the
// split transaction is determined to be un-economical, it will not be sent,
// there is no error, and the input coins will be returned unmodified, but an
// info message will be logged.
//
// A split transaction nets additional network bytes consisting of
// - overhead from 1 transaction
// - 1 extra signed p2pkh-spending input. The split tx has the fundingCoins as
//   inputs now, but we'll add the input that spends the sized coin that will go
//   into the first swap
// - 2 additional p2pkh outputs for the split tx sized output and change
//
// If the fees associated with this extra baggage are more than the excess
// amount that would be locked if a split transaction were not used, then the
// split transaction is pointless. This might be common, for instance, if an
// order is canceled partially filled, and then the remainder resubmitted. We
// would already have an output of just the right size, and that would be
// recognized here.
func (dcr *ExchangeWallet) split(value uint64, coins asset.Coins, inputsSize uint64, fundingCoins []*fundingCoin, nfo *dex.Asset) (asset.Coins, error) {

	// Calculate the extra fees associated with the additional inputs, outputs,
	// and transaction overhead, and compare to the excess that would be locked.
	baggageFees := nfo.MaxFeeRate * splitTxBaggage

	var coinSum uint64
	for _, coin := range coins {
		coinSum += coin.Value()
	}

	excess := coinSum - calc.RequiredOrderFunds(value, inputsSize, nfo)
	if baggageFees > excess {
		dcr.log.Infof("skipping split transaction because cost is greater than potential over-lock. %d > %d.", baggageFees, excess)
		dcr.log.Debugf("funding %d atom order with coins %v", value, coins)
		return coins, nil
	}

	// Use an internal address for the sized output.
	addr, err := dcr.node.GetRawChangeAddress(dcr.acct, chainParams)
	if err != nil {
		return nil, fmt.Errorf("error creating split transaction address: %v", err)
	}

	reqFunds := calc.RequiredOrderFunds(value, dexdcr.P2PKHInputSize, nfo)

	msgTx, net, err := dcr.sendCoins(addr, coins, reqFunds, dcr.feeRateWithFallback(), false)
	if err != nil {
		return nil, fmt.Errorf("error sending split transaction: %v", err)
	}

	if net != reqFunds {
		dcr.log.Errorf("split - total sent %d does not match expected %d", net, reqFunds)
	}

	op := newOutput(dcr.node, msgTx.CachedTxHash(), 0, net, wire.TxTreeRegular, nil)

	// Lock the funding coin.
	err = dcr.lockFundingCoins([]*fundingCoin{{
		op:   op,
		addr: addr.String(),
	}})
	if err != nil {
		dcr.log.Errorf("error locking funding coin from split transaction %s", op)
	}

	// NOTE: Can't return coins yet, because dcrwallet doesn't recognize them as
	// spent immediately, so subsequent calls to FundOrder might result in a
	// `-4: rejected transaction: transaction in the pool already spends the
	// same coins` error.
	// // Unlock the spent coins.
	// err = dcr.ReturnCoins(coins)
	// if err != nil {
	// 	dcr.log.Errorf("error locking coins spent in split transaction %v", coins)
	// }

	dcr.logSplitFunds(op, fundingCoins)

	dcr.log.Debugf("funding %d atom order with split output coin %v from original coins %v", value, op, coins)
	dcr.log.Infof("sent split transaction %s to accommodate swap of size %d + fees = %d", op.txHash(), value, reqFunds)

	return asset.Coins{op}, nil

}

// lockFundingCoins locks the funding coins via RPC and stores them in the map.
func (dcr *ExchangeWallet) lockFundingCoins(fCoins []*fundingCoin) error {
	wireOPs := make([]*wire.OutPoint, 0, len(fCoins))
	for _, c := range fCoins {
		wireOPs = append(wireOPs, wire.NewOutPoint(c.op.txHash(), c.op.vout(), c.op.tree))
	}
	err := dcr.node.LockUnspent(false, wireOPs)
	if err != nil {
		return err
	}
	dcr.fundingMtx.Lock()
	defer dcr.fundingMtx.Unlock()
	for _, c := range fCoins {
		dcr.fundingCoins[c.op.pt] = c
	}
	return nil
}

// logSplitFunds associates the funding coins with the split tx output, so that
// the coins can be unlocked when the swap is sent. The helps to deal with a
// timing issue with dcrwallet where listunspent might still return outputs that were
// just spent in a transaction broadcast with sendrawtransaction. Instead, we'll
// keep them locked until the split output is spent.
func (dcr *ExchangeWallet) logSplitFunds(op *output, fCoins []*fundingCoin) {
	dcr.fundingMtx.Lock()
	defer dcr.fundingMtx.Unlock()
	dcr.splitFunds[op.pt] = fCoins
}

// ReturnCoins unlocks coins. This would be necessary in the case of a
// canceled order.
func (dcr *ExchangeWallet) ReturnCoins(unspents asset.Coins) error {
	if len(unspents) == 0 {
		return fmt.Errorf("cannot return zero coins")
	}
	ops := make([]*wire.OutPoint, 0, len(unspents))

	dcr.log.Debugf("returning coins %s", unspents)
	dcr.fundingMtx.Lock()
	defer dcr.fundingMtx.Unlock()
	for _, unspent := range unspents {
		op, err := dcr.convertCoin(unspent)
		if err != nil {
			return fmt.Errorf("error converting coin: %v", err)
		}
		ops = append(ops, wire.NewOutPoint(op.txHash(), op.vout(), op.tree))
		delete(dcr.fundingCoins, op.pt)
		splitFunds, found := dcr.splitFunds[op.pt]
		if found {
			for _, c := range splitFunds {
				ops = append(ops, c.op.wireOutPoint())
			}
			delete(dcr.splitFunds, op.pt)
		}
	}
	return dcr.node.LockUnspent(true, ops)
}

// FundingCoins gets funding coins for the coin IDs. The coins are locked. This
// method might be called to reinitialize an order from data stored externally.
// This method will only return funding coins, e.g. unspent transaction outputs.
func (dcr *ExchangeWallet) FundingCoins(ids []dex.Bytes) (asset.Coins, error) {
	// First check if we have the coins in cache.
	coins := make(asset.Coins, 0, len(ids))
	notFound := make(map[outPoint]bool)
	dcr.fundingMtx.RLock()
	for _, id := range ids {
		txHash, vout, err := decodeCoinID(id)
		if err != nil {
			dcr.fundingMtx.RUnlock()
			return nil, err
		}
		pt := newOutPoint(txHash, vout)
		fundingCoin, found := dcr.fundingCoins[pt]
		if found {
			coins = append(coins, fundingCoin.op)
			continue
		}
		notFound[pt] = true
	}
	dcr.fundingMtx.RUnlock()
	if len(notFound) == 0 {
		return coins, nil
	}
	unspents, err := dcr.unspents()
	if err != nil {
		return nil, err
	}
	lockers := make([]*wire.OutPoint, 0, len(notFound))
	dcr.fundingMtx.Lock()
	defer dcr.fundingMtx.Unlock()
	for _, txout := range unspents {
		txHash, err := chainhash.NewHashFromStr(txout.TxID)
		if err != nil {
			return nil, fmt.Errorf("error decoding txid from rpc server %s: %v", txout.TxID, err)
		}
		pt := newOutPoint(txHash, txout.Vout)
		if notFound[pt] {
			redeemScript, err := hex.DecodeString(txout.RedeemScript)
			if err != nil {
				return nil, fmt.Errorf("error decoding redeem script for %s, script = %s: %v", txout.TxID, txout.RedeemScript, err)
			}
			lockers = append(lockers, wire.NewOutPoint(txHash, txout.Vout, txout.Tree))
			coin := newOutput(dcr.node, txHash, txout.Vout, toAtoms(txout.Amount), txout.Tree, redeemScript)
			coins = append(coins, coin)
			dcr.fundingCoins[pt] = &fundingCoin{
				op:   coin,
				addr: txout.Address,
			}
			delete(notFound, pt)
			if len(notFound) == 0 {
				break
			}
		}
	}
	if len(notFound) != 0 {
		ids := make([]string, 0, len(notFound))
		for pt := range notFound {
			ids = append(ids, pt.String())
		}
		return nil, fmt.Errorf("coins not found: %s", strings.Join(ids, ", "))
	}
	err = dcr.node.LockUnspent(false, lockers)
	if err != nil {
		return nil, err
	}

	dcr.log.Debugf("locking coins %v", coins)

	return coins, nil
}

// Swap sends the swaps in a single transaction. The Receipts returned can be
// used to refund a failed transaction.
func (dcr *ExchangeWallet) Swap(swaps *asset.Swaps) ([]asset.Receipt, asset.Coin, error) {
	var totalOut uint64
	// Start with an empty MsgTx.
	baseTx := wire.NewMsgTx()
	// Add the funding utxos.
	totalIn, err := dcr.addInputCoins(baseTx, swaps.Inputs)
	if err != nil {
		return nil, nil, err
	}
	pkScripts := make([][]byte, 0, len(swaps.Contracts))
	// Add the contract outputs.
	for _, contract := range swaps.Contracts {
		totalOut += contract.Value
		// revokeAddr is the address that will receive the refund if the contract is
		// abandoned.
		revokeAddr, err := dcr.node.GetNewAddressGapPolicy(dcr.acct, rpcclient.GapPolicyIgnore, chainParams)
		if err != nil {
			return nil, nil, fmt.Errorf("error creating revocation address: %v", err)
		}
		// Create the contract, a P2SH redeem script.
		pkScript, err := dexdcr.MakeContract(contract.Address, revokeAddr.String(), contract.SecretHash, int64(contract.LockTime), chainParams)
		if err != nil {
			return nil, nil, fmt.Errorf("unable to create pubkey script for address %s: %v", contract.Address, err)
		}
		pkScripts = append(pkScripts, pkScript)
		// Make the P2SH address and pubkey script.
		scriptAddr, err := dcrutil.NewAddressScriptHash(pkScript, chainParams)
		if err != nil {
			return nil, nil, fmt.Errorf("error encoding script address: %v", err)
		}
		p2shScript, err := txscript.PayToAddrScript(scriptAddr)
		if err != nil {
			return nil, nil, fmt.Errorf("error creating P2SH script: %v", err)
		}
		// Add the transaction output.
		txOut := wire.NewTxOut(int64(contract.Value), p2shScript)
		baseTx.AddTxOut(txOut)
	}
	if totalIn < totalOut {
		return nil, nil, fmt.Errorf("unfunded contract. %d < %d", totalIn, totalOut)
	}

	// Ensure we have enough outputs before broadcasting.
	swapCount := len(swaps.Contracts)
	if len(baseTx.TxOut) < swapCount {
		return nil, nil, fmt.Errorf("fewer outputs than swaps. %d < %d", len(baseTx.TxOut), swapCount)
	}

	// Grab a change address.
	changeAddr, err := dcr.node.GetRawChangeAddress(dcr.acct, chainParams)
	if err != nil {
		return nil, nil, fmt.Errorf("error creating change address: %v", err)
	}

	// Add change, sign, and send the transaction.
	msgTx, change, err := dcr.sendWithReturn(baseTx, changeAddr, totalIn, totalOut, swaps.FeeRate, nil)
	if err != nil {
		return nil, nil, err
	}

	// Return spent outputs.
	err = dcr.ReturnCoins(swaps.Inputs)
	if err != nil {
		dcr.log.Errorf("error unlocking swapped coins", swaps.Inputs)
	}

	// Lock the change coin, if requested.
	if swaps.LockChange {
		dcr.log.Debugf("locking change coin %s", change)
		err = dcr.lockFundingCoins([]*fundingCoin{{
			op:   change,
			addr: changeAddr.String(),
		}})
		if err != nil {
			dcr.log.Warnf("Failed to lock dcr change coin %s", change)
		}
	}

	receipts := make([]asset.Receipt, 0, swapCount)
	txHash := msgTx.TxHash()
	for i, contract := range swaps.Contracts {
		receipts = append(receipts, &swapReceipt{
			output:     newOutput(dcr.node, &txHash, uint32(i), contract.Value, wire.TxTreeRegular, pkScripts[i]),
			expiration: time.Unix(int64(contract.LockTime), 0).UTC(),
		})
	}

	// If change is nil, return a nil asset.Coin.
	var changeCoin asset.Coin
	if change != nil {
		changeCoin = change
	}
	return receipts, changeCoin, nil
}

// Redeem sends the redemption transaction, which may contain more than one
// redemption.
func (dcr *ExchangeWallet) Redeem(redemptions []*asset.Redemption) ([]dex.Bytes, asset.Coin, error) {
	// Create a transaction that spends the referenced contract.
	msgTx := wire.NewMsgTx()
	var totalIn uint64
	var contracts [][]byte
	var addresses []dcrutil.Address
	for _, r := range redemptions {
		cinfo, ok := r.Spends.(*auditInfo)
		if !ok {
			return nil, nil, fmt.Errorf("Redemption contract info of wrong type")
		}
		// Extract the swap contract recipient and secret hash and check the secret
		// hash against the hash of the provided secret.
		_, receiver, _, secretHash, err := dexdcr.ExtractSwapDetails(cinfo.output.redeem, chainParams)
		if err != nil {
			return nil, nil, fmt.Errorf("error extracting swap addresses: %v", err)
		}
		checkSecretHash := sha256.Sum256(r.Secret)
		if !bytes.Equal(checkSecretHash[:], secretHash) {
			return nil, nil, fmt.Errorf("secret hash mismatch. %x != %x", checkSecretHash[:], secretHash)
		}
		addresses = append(addresses, receiver)
		contracts = append(contracts, cinfo.output.redeem)
		prevOut := cinfo.output.wireOutPoint()
		txIn := wire.NewTxIn(prevOut, int64(cinfo.output.value), []byte{})
		// Sequence = 0xffffffff - 1 is special value that marks the transaction as
		// irreplaceable and enables the use of lock time.
		//
		// https://github.com/bitcoin/bips/blob/master/bip-0125.mediawiki#Spending_wallet_policy
		txIn.Sequence = wire.MaxTxInSequenceNum - 1
		msgTx.AddTxIn(txIn)
		totalIn += cinfo.output.value
	}

	// Calculate the size and the fees.
	size := msgTx.SerializeSize() + dexdcr.RedeemSwapSigScriptSize*len(redemptions) + dexdcr.P2PKHOutputSize
	feeRate := dcr.feeRateWithFallback()
	fee := feeRate * uint64(size)
	if fee > totalIn {
		return nil, nil, fmt.Errorf("redeem tx not worth the fees")
	}
	// Send the funds back to the exchange wallet.
	redeemAddr, err := dcr.node.GetRawChangeAddress(dcr.acct, chainParams)
	if err != nil {
		return nil, nil, fmt.Errorf("error getting new address from the wallet: %v", err)
	}
	pkScript, err := txscript.PayToAddrScript(redeemAddr)
	if err != nil {
		return nil, nil, fmt.Errorf("error creating redemption script for address '%s': %v", redeemAddr, err)
	}
	txOut := wire.NewTxOut(int64(totalIn-fee), pkScript)
	// One last check for dust.
	if dexdcr.IsDust(txOut, feeRate) {
		return nil, nil, fmt.Errorf("redeem output is dust")
	}
	msgTx.AddTxOut(txOut)
	// Sign the inputs.
	for i, r := range redemptions {
		contract := contracts[i]
		redeemSig, redeemPubKey, err := dcr.createSig(msgTx, i, contract, addresses[i])
		if err != nil {
			return nil, nil, err
		}
		redeemSigScript, err := dexdcr.RedeemP2SHContract(contract, redeemSig, redeemPubKey, r.Secret)
		if err != nil {
			return nil, nil, err
		}
		msgTx.TxIn[i].SignatureScript = redeemSigScript
	}
	// Send the transaction.
	checkHash := msgTx.TxHash()
	txHash, err := dcr.node.SendRawTransaction(msgTx, false)
	if err != nil {
		return nil, nil, err
	}
	if *txHash != checkHash {
		return nil, nil, fmt.Errorf("redemption sent, but received unexpected transaction ID back from RPC server. "+
			"expected %s, got %s", *txHash, checkHash)
	}
	coinIDs := make([]dex.Bytes, 0, len(redemptions))
	for i := range redemptions {
		coinIDs = append(coinIDs, toCoinID(txHash, uint32(i)))
	}

	return coinIDs, newOutput(dcr.node, txHash, 0, uint64(txOut.Value), wire.TxTreeRegular, nil), nil
}

// SignMessage signs the message with the private key associated with the
// specified funding Coin. A slice of pubkeys required to spend the Coin and a
// signature for each pubkey are returned.
func (dcr *ExchangeWallet) SignMessage(coin asset.Coin, msg dex.Bytes) (pubkeys, sigs []dex.Bytes, err error) {
	op, err := dcr.convertCoin(coin)
	if err != nil {
		return nil, nil, fmt.Errorf("error converting coin: %v", err)
	}

	// First check if we have the funding coin cached. If so, grab the address
	// from there.
	dcr.fundingMtx.RLock()
	fCoin, found := dcr.fundingCoins[op.pt]
	dcr.fundingMtx.RUnlock()
	var addr string
	if found {
		addr = fCoin.addr
	} else {
		// Check if we can get the address from gettxout.
		txOut, err := dcr.node.GetTxOut(op.txHash(), op.vout(), true)
		if err == nil && txOut != nil {
			addrs := txOut.ScriptPubKey.Addresses
			if len(addrs) != 1 {
				return nil, nil, fmt.Errorf("multi-sig not supported")
			}
			addr = addrs[0]
			found = true
		}
	}
	// Could also try the gettransaction endpoint, which is supposed to return
	// information about wallet transactions, but which (I think?) doesn't list
	// ssgen outputs.
	if !found {
		return nil, nil, fmt.Errorf("did not locate coin %s. is this a coin returned from Fund?", coin)
	}
	address, err := dcrutil.DecodeAddress(addr, chainParams)
	if err != nil {
		return nil, nil, fmt.Errorf("error decoding address: %v", err)
	}
	priv, pub, err := dcr.getKeys(address)
	if err != nil {
		return nil, nil, err
	}
	signature, err := priv.Sign(msg)
	if err != nil {
		return nil, nil, fmt.Errorf("signing error: %v", err)
	}
	pubkeys = append(pubkeys, pub.SerializeCompressed())
	sigs = append(sigs, signature.Serialize())
	return pubkeys, sigs, nil
}

// AuditContract retrieves information about a swap contract on the
// blockchain. This would be used to verify the counter-party's contract
// during a swap.
func (dcr *ExchangeWallet) AuditContract(coinID, contract dex.Bytes) (asset.AuditInfo, error) {
	txHash, vout, err := decodeCoinID(coinID)
	if err != nil {
		return nil, err
	}
	// Get the receiving address.
	_, receiver, stamp, secretHash, err := dexdcr.ExtractSwapDetails(contract, chainParams)
	if err != nil {
		return nil, fmt.Errorf("error extracting swap addresses: %v", err)
	}
	// Get the contracts P2SH address from the tx output's pubkey script.
	txOut, err := dcr.node.GetTxOut(txHash, vout, true)
	if err != nil {
		return nil, fmt.Errorf("error finding unspent contract: %v", err)
	}
	if txOut == nil {
		return nil, asset.CoinNotFoundError
	}
	pkScript, err := hex.DecodeString(txOut.ScriptPubKey.Hex)
	if err != nil {
		return nil, fmt.Errorf("error decoding pubkey script from hex '%s': %v",
			txOut.ScriptPubKey.Hex, err)
	}
	// Check for standard P2SH.
	scriptClass, addrs, numReq, err := txscript.ExtractPkScriptAddrs(dexdcr.CurrentScriptVersion, pkScript, chainParams)
	if err != nil {
		return nil, fmt.Errorf("error extracting script addresses from '%x': %v", pkScript, err)
	}
	if scriptClass != txscript.ScriptHashTy {
		return nil, fmt.Errorf("unexpected script class %d", scriptClass)
	}
	if numReq != 1 {
		return nil, fmt.Errorf("unexpected number of signatures expected for P2SH script: %d", numReq)
	}
	if len(addrs) != 1 {
		return nil, fmt.Errorf("unexpected number of addresses for P2SH script: %d", len(addrs))
	}
	// Compare the contract hash to the P2SH address.
	contractHash := dcrutil.Hash160(contract)
	addr := addrs[0]
	if !bytes.Equal(contractHash, addr.ScriptAddress()) {
		return nil, fmt.Errorf("contract hash doesn't match script address. %x != %x",
			contractHash, addr.ScriptAddress())
	}
	return &auditInfo{
		output:     newOutput(dcr.node, txHash, vout, toAtoms(txOut.Value), wire.TxTreeRegular, contract),
		secretHash: secretHash,
		recipient:  receiver,
		expiration: time.Unix(int64(stamp), 0).UTC(),
	}, nil
}

// LocktimeExpired returns true if the specified contract's locktime has
// expired, making it possible to issue a Refund.
func (dcr *ExchangeWallet) LocktimeExpired(contract dex.Bytes) (bool, time.Time, error) {
	_, _, locktime, _, err := dexdcr.ExtractSwapDetails(contract, chainParams)
	if err != nil {
		return false, time.Time{}, fmt.Errorf("error extracting contract locktime: %v", err)
	}
	contractExpiry := time.Unix(int64(locktime), 0).UTC()
	return time.Now().UTC().After(contractExpiry), contractExpiry, nil
}

// FindRedemption should attempt to find the input that spends the specified
// coin, and return the secret key if it does.
//
// NOTE: FindRedemption is necessary to deal with the case of a maker
// redeeming but not forwarding their redemption information. The DEX does not
// monitor for this case. While it will result in the counter-party being
// penalized, the input still needs to be found so the swap can be completed.
// For typical blockchains, every input of every block starting at the
// contract block will need to be scanned until the spending input is found.
// This could potentially be an expensive operation if performed long after
// the swap is broadcast. Realistically, though, the taker should start
// looking for the maker's redemption beginning at swapconf confirmations
// regardless of whether the server sends the 'redemption' message or not.
func (dcr *ExchangeWallet) FindRedemption(ctx context.Context, coinID dex.Bytes) (dex.Bytes, error) {
	txHash, vout, err := decodeCoinID(coinID)
	if err != nil {
		return nil, err
	}
	txid := txHash.String()
	// Get the wallet transaction.
	tx, err := dcr.node.GetTransaction(txHash)
	if err != nil {
		if isTxNotFoundErr(err) {
			return nil, asset.CoinNotFoundError
		}
		return nil, fmt.Errorf("error finding transaction %s in wallet: %v", txHash, err)
	}
	if tx.Confirmations == 0 {
		// This is a mempool transaction, GetRawTransactionVerbose works on mempool
		// transactions, so grab the contract from there and prepare a hash.
		rawTx, err := dcr.node.GetRawTransactionVerbose(txHash)
		if err != nil {
			return nil, fmt.Errorf("error getting contract details from mempool")
		}
		if int(vout) > len(rawTx.Vout)-1 {
			return nil, fmt.Errorf("vout index %d out of range for transaction %s", vout, txHash)
		}
		contractHash, err := dexdcr.ExtractContractHash(rawTx.Vout[vout].ScriptPubKey.Hex)
		if err != nil {
			return nil, err
		}
		// Only need to look at other mempool transactions. For each txid, get the
		// verbose transaction and check each input's previous outpoint.
		txs, err := dcr.node.GetRawMempool(chainjson.GRMAll)
		if err != nil {
			return nil, fmt.Errorf("error retrieve mempool transactions")
		}
		for _, txHash := range txs {
			tx, err := dcr.node.GetRawTransactionVerbose(txHash)
			if err != nil {
				return nil, fmt.Errorf("error encountered retreving mempool transaction %s: %v", txHash, err)
			}
			for i := range tx.Vin {
				if tx.Vin[i].Txid == txid && tx.Vin[i].Vout == vout {
					// Found it. Extract the key.
					vin := tx.Vin[i]
					sigScript, err := hex.DecodeString(vin.ScriptSig.Hex)
					if err != nil {
						return nil, fmt.Errorf("error decoding scriptSig '%s': %v", vin.ScriptSig.Hex, err)
					}
					key, err := dexdcr.FindKeyPush(sigScript, contractHash, chainParams)
					if err != nil {
						return nil, fmt.Errorf("found spending input at %s:%d, but error extracting key from '%s': %v",
							txHash, i, vin.ScriptSig.Hex, err)
					}
					return key, nil
				}
			}
		}
		return nil, fmt.Errorf("no key found")
	}
	// Since it's not a mempool transaction, start scanning blocks at the height
	// of the swap.
	blockHash, err := chainhash.NewHashFromStr(tx.BlockHash)
	if err != nil {
		return nil, fmt.Errorf("error decoding block hash: %v", err)
	}
	contractBlock := *blockHash
	var contractHash []byte
	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("redemption search canceled")
		default:
		}
		block, err := dcr.node.GetBlockVerbose(blockHash, true)
		if err != nil {
			return nil, fmt.Errorf("error fetching verbose block '%s': %v", blockHash, err)
		}
		// If this is the swap contract's block, grab the contract hash.
		if *blockHash == contractBlock {
			for i := range block.RawTx {
				if block.RawTx[i].Txid == txid {
					vouts := block.RawTx[i].Vout
					if len(vouts) < int(vout)+1 {
						return nil, fmt.Errorf("vout %d not found in tx %s", vout, txid)
					}
					contractHash, err = dexdcr.ExtractContractHash(vouts[vout].ScriptPubKey.Hex)
					if err != nil {
						return nil, err
					}
				}
			}
		}
		if len(contractHash) == 0 {
			return nil, fmt.Errorf("no secret hash found at %s:%d", txid, vout)
		}
		// Look at the previous outpoint of each input of each transaction in the
		// block.
		for i := range block.RawTx {
			for j := range block.RawTx[i].Vin {
				if block.RawTx[i].Vin[j].Txid == txid && block.RawTx[i].Vin[j].Vout == vout {
					// Found it. Extract the key.
					vin := block.RawTx[i].Vin[j]
					sigScript, err := hex.DecodeString(vin.ScriptSig.Hex)
					if err != nil {
						return nil, fmt.Errorf("error decoding scriptSig '%s': %v", vin.ScriptSig.Hex, err)
					}
					key, err := dexdcr.FindKeyPush(sigScript, contractHash, chainParams)
					if err != nil {
						return nil, fmt.Errorf("error extracting key from '%s': %v", vin.ScriptSig.Hex, err)
					}
					return key, nil
				}
			}
		}
		// Not found in this block. Get the next block
		if block.NextHash == "" {
			return nil, fmt.Errorf("spending transaction not found")
		}
		blockHash, err = chainhash.NewHashFromStr(block.NextHash)
		if err != nil {
			return nil, fmt.Errorf("hash decode error: %v", err)
		}
	}
}

// Refund refunds a contract. This can only be used after the time lock has
// expired.
// NOTE: The contract cannot be retrieved from the unspent coin info as the
// wallet does not store it, even though it was known when the init transaction
// was created. The client should store this information for persistence across
// sessions.
func (dcr *ExchangeWallet) Refund(coinID, contract dex.Bytes) (dex.Bytes, error) {
	txHash, vout, err := decodeCoinID(coinID)
	if err != nil {
		return nil, err
	}
	// Grab the unspent output to make sure it's good and to get the value.
	utxo, err := dcr.node.GetTxOut(txHash, vout, true)
	if err != nil {
		return nil, fmt.Errorf("error finding unspent contract: %v", err)
	}
	if utxo == nil {
		return nil, asset.CoinNotFoundError
	}
	val := toAtoms(utxo.Value)
	sender, _, lockTime, _, err := dexdcr.ExtractSwapDetails(contract, chainParams)
	if err != nil {
		return nil, fmt.Errorf("error extracting swap addresses: %v", err)
	}

	// Create the transaction that spends the contract.
	feeRate := dcr.feeRateWithFallback()
	msgTx := wire.NewMsgTx()
	msgTx.LockTime = uint32(lockTime)
	prevOut := wire.NewOutPoint(txHash, vout, wire.TxTreeRegular)
	txIn := wire.NewTxIn(prevOut, int64(val), []byte{})
	txIn.Sequence = wire.MaxTxInSequenceNum - 1
	msgTx.AddTxIn(txIn)
	// Calculate fees and add the change output.
	size := msgTx.SerializeSize() + dexdcr.RefundSigScriptSize + dexdcr.P2PKHOutputSize
	fee := feeRate * uint64(size)
	if fee > val {
		return nil, fmt.Errorf("refund tx not worth the fees")
	}

	refundAddr, err := dcr.node.GetNewAddressGapPolicy(dcr.acct, rpcclient.GapPolicyIgnore, chainParams)
	if err != nil {
		return nil, fmt.Errorf("error getting new address from the wallet: %v", err)
	}
	pkScript, err := txscript.PayToAddrScript(refundAddr)
	if err != nil {
		return nil, fmt.Errorf("error creating refund script for address '%v': %v", refundAddr, err)
	}
	txOut := wire.NewTxOut(int64(val-fee), pkScript)
	// One last check for dust.
	if dexdcr.IsDust(txOut, feeRate) {
		return nil, fmt.Errorf("refund output is dust")
	}
	msgTx.AddTxOut(txOut)
	// Sign it.
	refundSig, refundPubKey, err := dcr.createSig(msgTx, 0, contract, sender)
	if err != nil {
		return nil, err
	}
	redeemSigScript, err := dexdcr.RefundP2SHContract(contract, refundSig, refundPubKey)
	if err != nil {
		return nil, err
	}
	txIn.SignatureScript = redeemSigScript
	// Send it.
	checkHash := msgTx.TxHash()
	refundHash, err := dcr.node.SendRawTransaction(msgTx, false)
	if err != nil {
		return nil, err
	}
	if *refundHash != checkHash {
		return nil, fmt.Errorf("refund sent, but received unexpected transaction ID back from RPC server. "+
			"expected %s, got %s", *refundHash, checkHash)
	}
	return toCoinID(refundHash, 0), nil
}

// Address returns an address for the exchange wallet.
func (dcr *ExchangeWallet) Address() (string, error) {
	addr, err := dcr.node.GetNewAddressGapPolicy(dcr.acct, rpcclient.GapPolicyIgnore, chainParams)
	if err != nil {
		return "", err
	}
	return addr.String(), nil
}

// Unlock unlocks the exchange wallet.
func (dcr *ExchangeWallet) Unlock(pw string, dur time.Duration) error {
	return dcr.node.WalletPassphrase(pw, int64(dur/time.Second))
}

// Lock locks the exchange wallet.
func (dcr *ExchangeWallet) Lock() error {
	return dcr.node.WalletLock()
}

// PayFee sends the dex registration fee. Transaction fees are in addition to
// the registration fee, and the fee rate is taken from the DEX configuration.
func (dcr *ExchangeWallet) PayFee(address string, regFee uint64) (asset.Coin, error) {
	addr, err := dcrutil.DecodeAddress(address, chainParams)
	if err != nil {
		return nil, err
	}
	// TODO: Evaluate SendToAddress and how it deals with the change output
	// address index to see if it can be used here instead.
	msgTx, sent, err := dcr.sendRegFee(addr, regFee, dcr.feeRateWithFallback())
	if err != nil {
		return nil, err
	}
	if sent != regFee {
		return nil, fmt.Errorf("transaction %s was sent, but the reported value sent was unexpected. "+
			"expected %d, but %d was reported", msgTx.CachedTxHash(), regFee, sent)
	}
	return newOutput(dcr.node, msgTx.CachedTxHash(), 0, regFee, wire.TxTreeRegular, nil), nil
}

// Withdraw withdraws funds to the specified address. Fees are subtracted from
// the value.
func (dcr *ExchangeWallet) Withdraw(address string, value uint64) (asset.Coin, error) {
	addr, err := dcrutil.DecodeAddress(address, chainParams)
	if err != nil {
		return nil, err
	}
	msgTx, net, err := dcr.sendMinusFees(addr, value, dcr.feeRateWithFallback())
	if err != nil {
		return nil, err
	}
	return newOutput(dcr.node, msgTx.CachedTxHash(), 0, net, wire.TxTreeRegular, nil), nil
}

// ValidateSecret checks that the secret satisfies the contract.
func (dcr *ExchangeWallet) ValidateSecret(secret, secretHash []byte) bool {
	h := sha256.Sum256(secret)
	return bytes.Equal(h[:], secretHash)
}

// Confirmations gets the number of confirmations for the specified coin ID.
// The coin must be known to the wallet, but need not be unspent.
func (dcr *ExchangeWallet) Confirmations(id dex.Bytes) (uint32, error) {
	// Could check with gettransaction first, figure out the tree, and look for a
	// redeem script with listscripts, but the listunspent entry has all the
	// necessary fields already.
	txHash, _, err := decodeCoinID(id)
	if err != nil {
		return 0, err
	}
	tx, err := dcr.node.GetTransaction(txHash)
	if err != nil {
		if isTxNotFoundErr(err) {
			return 0, asset.CoinNotFoundError
		}
		return 0, err
	}
	return uint32(tx.Confirmations), nil
}

// addInputCoins adds inputs to the MsgTx to spend the specified outputs.
func (dcr *ExchangeWallet) addInputCoins(msgTx *wire.MsgTx, coins asset.Coins) (uint64, error) {
	var totalIn uint64
	for _, coin := range coins {
		op, err := dcr.convertCoin(coin)
		if err != nil {
			return 0, err
		}
		if op.value == 0 {
			return 0, fmt.Errorf("zero-valued output detected for %s:%d", op.txHash(), op.vout())
		}
		totalIn += op.value
		prevOut := op.wireOutPoint()
		txIn := wire.NewTxIn(prevOut, int64(op.value), []byte{})
		msgTx.AddTxIn(txIn)
	}
	return totalIn, nil
}

// Shutdown down the rpcclient.Client.
func (dcr *ExchangeWallet) shutdown() {
	// Unlock any locked outputs.
	err := dcr.node.LockUnspent(true, nil)
	if err != nil {
		dcr.log.Errorf("failed to unlock DCR outputs on shutdown: %v", err)
	}
	if dcr.client != nil {
		dcr.client.Shutdown()
		dcr.client.WaitForShutdown()
	}
}

// Combines the RPC type with the spending input information.
type compositeUTXO struct {
	rpc   walletjson.ListUnspentResult
	input *dexdcr.SpendInfo
	confs int64
	// TODO: consider including isDexChange bool for consumer
}

// parseUTXOs constructs and returns a list of compositeUTXOs from the provided
// set of RPC utxos, including basic information required to spend each rpc utxo.
// The returned list is sorted by ascending value.
func (dcr *ExchangeWallet) parseUTXOs(unspents []walletjson.ListUnspentResult) ([]*compositeUTXO, uint64, error) {
	var sum uint64
	utxos := make([]*compositeUTXO, 0, len(unspents))
	for _, txout := range unspents {
		scriptPK, err := hex.DecodeString(txout.ScriptPubKey)
		if err != nil {
			return nil, 0, fmt.Errorf("error decoding pubkey script for %s, script = %s: %v", txout.TxID, txout.ScriptPubKey, err)
		}
		redeemScript, err := hex.DecodeString(txout.RedeemScript)
		if err != nil {
			return nil, 0, fmt.Errorf("error decoding redeem script for %s, script = %s: %v", txout.TxID, txout.RedeemScript, err)
		}

		nfo, err := dexdcr.InputInfo(scriptPK, redeemScript, chainParams)
		if err != nil {
			if errors.Is(err, dex.UnsupportedScriptError) {
				continue
			}
			return nil, 0, fmt.Errorf("error reading asset info: %v", err)
		}
		utxos = append(utxos, &compositeUTXO{
			rpc:   txout,
			input: nfo,
			confs: txout.Confirmations,
		})
		sum += toAtoms(txout.Amount)
	}
	// Sort in ascending order by amount (smallest first).
	sort.Slice(utxos, func(i, j int) bool { return utxos[i].rpc.Amount < utxos[j].rpc.Amount })
	return utxos, sum, nil
}

// lockedAtoms is the total value of locked outputs, as locked with LockUnspent.
func (dcr *ExchangeWallet) lockedAtoms() (uint64, error) {
	// Fetch locked outputs for the ExchangeWallet account using RawRequest.
	var lockedOutpoints []chainjson.TransactionInput
	err := dcr.nodeRawRequest(methodListLockUnspent, anylist{dcr.acct}, &lockedOutpoints)
	if err != nil {
		return 0, err
	}
	var sum uint64
	for _, op := range lockedOutpoints {
		sum += toAtoms(op.Amount)
	}
	return sum, nil
}

// convertCoin converts the asset.Coin to an unspent output.
func (dcr *ExchangeWallet) convertCoin(coin asset.Coin) (*output, error) {
	op, _ := coin.(*output)
	if op != nil {
		return op, nil
	}
	txHash, vout, err := decodeCoinID(coin.ID())
	if err != nil {
		return nil, err
	}
	txOut, err := dcr.node.GetTxOut(txHash, vout, true)
	if err != nil {
		return nil, fmt.Errorf("error finding unspent output %s:%d: %v", txHash, vout, err)
	}
	if txOut == nil {
		return nil, asset.CoinNotFoundError
	}
	pkScript, err := hex.DecodeString(txOut.ScriptPubKey.Hex)
	if err != nil {
		return nil, err
	}
	tree := wire.TxTreeRegular
	if dexdcr.ParseScriptType(dexdcr.CurrentScriptVersion, pkScript, coin.Redeem()).IsStake() {
		tree = wire.TxTreeStake
	}
	return newOutput(dcr.node, txHash, vout, coin.Value(), tree, coin.Redeem()), nil
}

// sendMinusFees sends the amount to the address. Fees are subtracted from the
// sent value.
func (dcr *ExchangeWallet) sendMinusFees(addr dcrutil.Address, val, feeRate uint64) (*wire.MsgTx, uint64, error) {
	if val == 0 {
		return nil, 0, fmt.Errorf("cannot send value = 0")
	}
	enough := func(sum uint64, size uint32, unspent *compositeUTXO) bool {
		return sum+toAtoms(unspent.rpc.Amount) >= val
	}
	coins, _, _, err := dcr.fund(enough)
	if err != nil {
		return nil, 0, err
	}
	return dcr.sendCoins(addr, coins, val, feeRate, true)
}

// sendRegFee sends the registration fee to the address. Transaction fees will
// be in addition to the registration fee and the output will be the zeroth
// output.
func (dcr *ExchangeWallet) sendRegFee(addr dcrutil.Address, regfee, netFeeRate uint64) (*wire.MsgTx, uint64, error) {
	enough := func(sum uint64, size uint32, unspent *compositeUTXO) bool {
		txFee := uint64(size+unspent.input.Size()) * netFeeRate
		return sum+toAtoms(unspent.rpc.Amount) >= regfee+txFee
	}
	coins, _, _, err := dcr.fund(enough)
	if err != nil {
		return nil, 0, err
	}
	return dcr.sendCoins(addr, coins, regfee, netFeeRate, false)
}

// sendCoins sends the amount to the address as the zeroth output, spending the
// specified coins. If subtract is true, the transaction fees will be taken from
// the sent value, otherwise it will taken from the change output. If there is
// change, it will be at index 1.
func (dcr *ExchangeWallet) sendCoins(addr dcrutil.Address, coins asset.Coins, val, feeRate uint64, subtract bool) (*wire.MsgTx, uint64, error) {
	baseTx := wire.NewMsgTx()
	totalIn, err := dcr.addInputCoins(baseTx, coins)
	if err != nil {
		return nil, 0, err
	}
	payScript, err := txscript.PayToAddrScript(addr)
	if err != nil {
		return nil, 0, fmt.Errorf("error creating P2SH script: %v", err)
	}
	txOut := wire.NewTxOut(int64(val), payScript)
	baseTx.AddTxOut(txOut)
	// Grab a change address.
	changeAddr, err := dcr.node.GetRawChangeAddress(dcr.acct, chainParams)
	if err != nil {
		return nil, 0, fmt.Errorf("error creating change address: %v", err)
	}
	// A nil subtractee indicates that fees should be taken from the change
	// output.
	var subtractee *wire.TxOut
	if subtract {
		subtractee = txOut
	}

	tx, _, err := dcr.sendWithReturn(baseTx, changeAddr, totalIn, val, feeRate, subtractee)
	return tx, uint64(txOut.Value), err
}

// msgTxFromHex creates a wire.MsgTx by deserializing the hex transaction.
func msgTxFromHex(txHex string) (*wire.MsgTx, error) {
	msgTx := wire.NewMsgTx()
	if err := msgTx.Deserialize(hex.NewDecoder(strings.NewReader(txHex))); err != nil {
		return nil, err
	}
	return msgTx, nil
}

func msgTxToHex(msgTx *wire.MsgTx) (string, error) {
	b, err := msgTx.Bytes()
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// signTx attempts to sign all transaction inputs. If it fails to completely
// sign the transaction, it is an error and a nil *wire.MsgTx is returned.
func (dcr *ExchangeWallet) signTx(baseTx *wire.MsgTx) (*wire.MsgTx, error) {
	txHex, err := msgTxToHex(baseTx)
	if err != nil {
		return nil, fmt.Errorf("failed to encode MsgTx: %w", err)
	}
	var res walletjson.SignRawTransactionResult
	err = dcr.nodeRawRequest(methodSignRawTransaction, anylist{txHex}, &res)
	if err != nil {
		return nil, fmt.Errorf("rawrequest error: %v", err)
	}

	for i := range res.Errors {
		sigErr := &res.Errors[i]
		dcr.log.Errorf("Signing %v:%d, seq = %d, sigScript = %v, failed: %v",
			sigErr.TxID, sigErr.Vout, sigErr.Sequence, sigErr.ScriptSig, sigErr.Error)
		// Will be incomplete below, so log each SignRawTransactionError and move on.
	}

	signedTx, err := msgTxFromHex(res.Hex)
	if err != nil {
		return nil, fmt.Errorf("failed to deserialize signed MsgTx: %w", err)
	}

	if !res.Complete {
		dcr.log.Errorf("Incomplete raw transaction signatures (input tx: %x / incomplete signed tx: %x): ",
			dcr.wireBytes(baseTx), dcr.wireBytes(signedTx))
		return nil, fmt.Errorf("incomplete raw tx signatures")
	}

	return signedTx, nil
}

// sendWithReturn sends the unsigned transaction with an added output (unless
// dust) for the change. If a subtractee output is specified, fees will be
// subtracted from that output, otherwise they will be subtracted from the
// change output.
func (dcr *ExchangeWallet) sendWithReturn(baseTx *wire.MsgTx, addr dcrutil.Address,
	totalIn, totalOut, feeRate uint64, subtractee *wire.TxOut) (*wire.MsgTx, *output, error) {
	// Sign the transaction to get an initial size estimate and calculate whether
	// a change output would be dust.
	sigCycles := 1
	msgTx, err := dcr.signTx(baseTx)
	if err != nil {
		return nil, nil, err
	}
	size := msgTx.SerializeSize()
	minFee := feeRate * uint64(size)
	remaining := totalIn - totalOut
	lastFee := remaining

	// Create the change output.
	changeScript, err := txscript.PayToAddrScript(addr)
	if err != nil {
		return nil, nil, fmt.Errorf("error creating change script for address '%s': %v", addr, err)
	}
	changeOutput := wire.NewTxOut(int64(remaining), changeScript)

	// The reservoir indicates the amount available to draw upon for fees.
	reservoir := remaining
	// If no subtractee was provided, subtract fees from the change output.
	if subtractee == nil {
		subtractee = changeOutput
		changeOutput.Value -= int64(minFee)
	} else {
		reservoir = uint64(subtractee.Value)
	}
	if minFee > reservoir {
		return nil, nil, fmt.Errorf("not enough funds to cover minimum fee rate. %d < %d",
			minFee, reservoir)
	}

	// If the change is not dust, recompute the signed txn size and iterate on
	// the fees vs. change amount.
	changeAdded := !dexdcr.IsDust(subtractee, feeRate)
	if changeAdded {
		// Add the change output.
		size0 := baseTx.SerializeSize()
		baseTx.AddTxOut(changeOutput)
		changeSize := baseTx.SerializeSize() - size0 // may be dexdcr.P2PKHOutputSize
		dcr.log.Debugf("Change output size = %d, addr = %s", changeSize, addr.String())

		size += changeSize
		lastFee = feeRate * uint64(size)
		subtractee.Value = int64(reservoir - lastFee)

		// Find the best fee rate by closing in on it in a loop.
		tried := map[uint64]bool{}
		for {
			// Each cycle, sign the transaction and see if there appears to be any
			// room to lower the total fees.
			sigCycles++
			msgTx, err = dcr.signTx(baseTx)
			if err != nil {
				return nil, nil, err
			}
			size = msgTx.SerializeSize()
			// reqFee is the lowest acceptable fee for a transaction of this size.
			reqFee := feeRate * uint64(size)
			if reqFee > reservoir {
				// I can't imagine a scenario where this condition would be true, but
				// I'd hate to be wrong.
				dcr.log.Errorf("reached the impossible place. in = %d, out = %d, reqFee = %d, lastFee = %d, raw tx = %x",
					totalIn, totalOut, reqFee, lastFee, dcr.wireBytes(msgTx))
				return nil, nil, fmt.Errorf("change error")
			}

			// If 1) lastFee == reqFee, nothing changed since the last cycle.
			// And there is likely no room for improvement. If 2) The reqFee
			// required for a transaction of this size is less than the
			// currently signed transaction fees, but we've already tried it,
			// then it must have a larger serialize size, so the current fee is
			// as good as it gets.
			if lastFee == reqFee || (lastFee > reqFee && tried[reqFee]) {
				break
			}

			// The minimum fee for a transaction of this size is either higher or
			// lower than the fee in the currently signed transaction, and it hasn't
			// been tried yet, so try it now.
			tried[lastFee] = true
			lastFee = reqFee
			subtractee.Value = int64(reservoir - lastFee)
			if dexdcr.IsDust(subtractee, feeRate) {
				// Another condition that should be impossible, but check anyway in case
				// the maximum fee was underestimated causing the first check to be
				// missed.
				dcr.log.Errorf("reached the impossible place. in = %d, out = %d, reqFee = %d, lastFee = %d, raw tx = %x",
					totalIn, totalOut, reqFee, lastFee, dcr.wireBytes(msgTx))
				return nil, nil, fmt.Errorf("dust error")
			}
			continue
		}
	}

	// Double check the resulting txns fee and fee rate.
	checkFee, checkRate := fees(msgTx)
	if checkFee != lastFee {
		return nil, nil, fmt.Errorf("fee mismatch! %d != %d, raw tx: %x", checkFee, lastFee, dcr.wireBytes(msgTx))
	}
	// Ensure the effective fee rate is at least the required fee rate.
	if checkRate < feeRate {
		return nil, nil, fmt.Errorf("final fee rate for %s, %d, is lower than expected, %d. raw tx: %x",
			msgTx.CachedTxHash(), checkRate, feeRate, dcr.wireBytes(msgTx))
	}
	// This is a last ditch effort to catch ridiculously high fees. Right now,
	// it's just erroring for fees more than triple the expected rate, which is
	// admittedly un-scientific. This should account for any signature length
	// related variation as well as a potential dust change output with no
	// subtractee specified, in which case the dust goes to the miner.
	if changeAdded && checkRate > feeRate*3 {
		return nil, nil, fmt.Errorf("final fee rate for %s, %d, is seemingly outrageous, target = %d, raw tx = %x",
			msgTx.CachedTxHash(), checkRate, feeRate, dcr.wireBytes(msgTx))
	}

	checkHash := msgTx.TxHash()
	dcr.log.Debugf("%d signature cycles to converge on fees for tx %s: "+
		"min rate = %d, actual fee rate = %d (%v for %v bytes), change = %v",
		sigCycles, checkHash, feeRate, checkRate, checkFee, size, changeAdded)
	txHash, err := dcr.node.SendRawTransaction(msgTx, false)
	if err != nil {
		return nil, nil, fmt.Errorf("sendrawtx error: %v, raw tx: %x", err, dcr.wireBytes(msgTx))
	}
	if *txHash != checkHash {
		return nil, nil, fmt.Errorf("transaction sent, but received unexpected transaction ID back from RPC server. "+
			"expected %s, got %s, raw tx: %x", *txHash, checkHash, dcr.wireBytes(msgTx))
	}

	var change *output
	if changeAdded {
		change = newOutput(dcr.node, txHash, uint32(len(msgTx.TxOut)-1), uint64(changeOutput.Value), wire.TxTreeRegular, nil)
	}
	return msgTx, change, nil
}

// For certain dcrutil.Address types.
type signatureTyper interface {
	DSA() dcrec.SignatureType
}

// createSig creates and returns the serialized raw signature and compressed
// pubkey for a transaction input signature.
func (dcr *ExchangeWallet) createSig(tx *wire.MsgTx, idx int, pkScript []byte, addr dcrutil.Address) (sig, pubkey []byte, err error) {
	sigTyper, ok := addr.(signatureTyper)
	if !ok {
		return nil, nil, fmt.Errorf("invalid address type")
	}

	priv, pub, err := dcr.getKeys(addr)
	if err != nil {
		return nil, nil, err
	}

	sigType := sigTyper.DSA()
	switch sigType {
	case dcrec.STEcdsaSecp256k1:
		sig, err = txscript.RawTxInSignature(tx, idx, pkScript, txscript.SigHashAll, priv)
	default:
		sig, err = txscript.RawTxInSignatureAlt(tx, idx, pkScript, txscript.SigHashAll, priv, sigType)
	}
	if err != nil {
		return nil, nil, err
	}

	return sig, pub.SerializeCompressed(), nil
}

// getKeys fetches the private/public key pair for the specified address.
func (dcr *ExchangeWallet) getKeys(addr dcrutil.Address) (*secp256k1.PrivateKey, *secp256k1.PublicKey, error) {
	wif, err := dcr.node.DumpPrivKey(addr, chainParams.PrivateKeyID)
	if err != nil {
		return nil, nil, err
	}

	priv, pub := secp256k1.PrivKeyFromBytes(wif.PrivKey.Serialize())
	return priv, pub, nil
}

// monitorBlocks pings for new blocks and runs the tipChange callback function
// when the block changes.
func (dcr *ExchangeWallet) monitorBlocks(ctx context.Context) {
	h, _, err := dcr.node.GetBestBlock()
	if err != nil {
		dcr.tipChange(fmt.Errorf("error initializing best block for DCR: %v", err))
	}
	tipHash := *h
	ticker := time.NewTicker(blockTicker)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			h, _, err := dcr.node.GetBestBlock()
			if err != nil {
				dcr.tipChange(fmt.Errorf("failed to get best block hash from DCR node"))
				continue
			}
			if *h != tipHash {
				tipHash = *h
				dcr.tipChange(nil)
			}
		case <-ctx.Done():
			return
		}
	}
}

// wireBytes dumps the serialized transaction bytes.
func (dcr *ExchangeWallet) wireBytes(tx *wire.MsgTx) []byte {
	s, err := tx.Bytes()
	// wireBytes is just used for logging, and a serialization error is
	// extremely unlikely, so just log the error and return the nil bytes.
	if err != nil {
		dcr.log.Errorf("error serializing transaction: %v", err)
	}
	return s
}

// anylist is a list of RPC parameters to be converted to []json.RawMessage and
// sent via nodeRawRequest.
type anylist []interface{}

// nodeRawRequest is used to marshal parameters and send requests to the RPC
// server via (*rpcclient.Client).RawRequest. If `thing` is non-nil, the result
// will be marshaled into `thing`.
func (dcr *ExchangeWallet) nodeRawRequest(method string, args anylist, thing interface{}) error {
	params := make([]json.RawMessage, 0, len(args))
	for i := range args {
		p, err := json.Marshal(args[i])
		if err != nil {
			return err
		}
		params = append(params, p)
	}
	b, err := dcr.node.RawRequest(method, params)
	if err != nil {
		return fmt.Errorf("rawrequest error: %v", err)
	}
	if thing != nil {
		return json.Unmarshal(b, thing)
	}
	return nil
}

// Convert the DCR value to atoms.
func toAtoms(v float64) uint64 {
	return uint64(math.Round(v * 1e8))
}

// toCoinID converts the tx hash and vout to a coin ID, as a []byte.
func toCoinID(txHash *chainhash.Hash, vout uint32) []byte {
	coinID := make([]byte, chainhash.HashSize+4)
	copy(coinID[:chainhash.HashSize], txHash[:])
	binary.BigEndian.PutUint32(coinID[chainhash.HashSize:], vout)
	return coinID
}

// decodeCoinID decodes the coin ID into a tx hash and a vout.
func decodeCoinID(coinID dex.Bytes) (*chainhash.Hash, uint32, error) {
	if len(coinID) != 36 {
		return nil, 0, fmt.Errorf("coin ID wrong length. expected 36, got %d", len(coinID))
	}
	var txHash chainhash.Hash
	copy(txHash[:], coinID[:32])
	return &txHash, binary.BigEndian.Uint32(coinID[32:]), nil
}

// Fees extracts the transaction fees and fee rate from the MsgTx.
func fees(tx *wire.MsgTx) (uint64, uint64) {
	var in, out int64
	for _, txIn := range tx.TxIn {
		in += txIn.ValueIn
	}
	for _, txOut := range tx.TxOut {
		out += txOut.Value
	}
	fees := uint64(in - out)
	return fees, fees / uint64(tx.SerializeSize())
}

// isTxNotFoundErr will return true if the error indicates that the requested
// transaction is not known.
func isTxNotFoundErr(err error) bool {
	var rpcErr *dcrjson.RPCError
	return errors.As(err, &rpcErr) && rpcErr.Code == dcrjson.ErrRPCNoTxInfo
}
