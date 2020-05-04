// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package btc

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	dexbtc "decred.org/dcrdex/dex/btc"
	"decred.org/dcrdex/dex/calc"
	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/rpcclient"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
	"github.com/btcsuite/btcutil"
)

const (
	assetName = "btc"
	// Use RawRequest to get the verbose block with verbose txs, as the btcd
	// rpcclient.Client's GetBlockVerboseTx appears to be busted.
	methodGetBlockVerboseTx = "getblock"
	methodGetNetworkInfo    = "getnetworkinfo"
	BipID                   = 0
	// The default fee is passed to the user as part of the asset.WalletInfo
	// structure.
	defaultWithdrawalFee = 2
	minNetworkVersion    = 180100
	minProtocolVersion   = 70015
)

var (
	// blockTicker is the delay between calls to check for new blocks.
	blockTicker = time.Second
	// walletInfo defines some general information about a Bitcoin wallet.
	walletInfo = &asset.WalletInfo{
		Name:              "Bitcoin",
		Units:             "Satoshis",
		DefaultConfigPath: dexbtc.SystemConfigPath("bitcoin"),
		DefaultFeeRate:    defaultWithdrawalFee,
	}
)

// rpcClient is a wallet RPC client. In production, rpcClient is satisfied by
// rpcclient.Client. A stub can be used for testing.
type rpcClient interface {
	SendRawTransaction(tx *wire.MsgTx, allowHighFees bool) (*chainhash.Hash, error)
	GetTxOut(txHash *chainhash.Hash, index uint32, mempool bool) (*btcjson.GetTxOutResult, error)
	GetBlockHash(blockHeight int64) (*chainhash.Hash, error)
	GetBestBlockHash() (*chainhash.Hash, error)
	GetRawMempool() ([]*chainhash.Hash, error)
	GetRawTransactionVerbose(txHash *chainhash.Hash) (*btcjson.TxRawResult, error)
	RawRequest(method string, params []json.RawMessage) (json.RawMessage, error)
}

// outpointID creates a unique string for a transaction output.
func outpointID(txid string, vout uint32) string {
	return txid + ":" + strconv.Itoa(int(vout))
}

// output is information about a transaction output. output satisfies the
// asset.Coin interface.
type output struct {
	txHash chainhash.Hash
	vout   uint32
	value  uint64
	redeem dex.Bytes
	node   rpcClient // for calculating confirmations.
}

// newOutput is the constructor for an output.
func newOutput(node rpcClient, txHash *chainhash.Hash, vout uint32, value uint64, redeem dex.Bytes) *output {
	return &output{
		txHash: *txHash,
		vout:   vout,
		value:  value,
		redeem: redeem,
		node:   node,
	}
}

// Value returns the value of the output. Part of the asset.Coin interface.
func (output *output) Value() uint64 {
	return output.value
}

// Confirmations is the number of confirmations on the output's block.
// Confirmations always pulls the block information fresh from the block chain,
// and will return an error if the output has been spent. Part of the
// asset.Coin interface.
func (output *output) Confirmations() (uint32, error) {
	txOut, err := output.node.GetTxOut(&output.txHash, output.vout, true)
	if err != nil {
		return 0, fmt.Errorf("error finding coin: %v", err)
	}
	if txOut == nil {
		return 0, fmt.Errorf("tx output not found")
	}
	return uint32(txOut.Confirmations), nil
}

// ID is the output's coin ID. Part of the asset.Coin interface. For BTC, the
// coin ID is 36 bytes = 32 bytes tx hash + 4 bytes big-endian vout.
func (op *output) ID() dex.Bytes {
	return toCoinID(&op.txHash, op.vout)
}

// String is a string representation of the coin.
func (op *output) String() string {
	return fmt.Sprintf("%s:%v", op.txHash, op.vout)
}

// Redeem is any known redeem script required to spend this output. Part of the
// asset.Coin interface.
func (op *output) Redeem() dex.Bytes {
	return op.redeem
}

// auditInfo is information about a swap contract on that blockchain, not
// necessarily created by this wallet, as would be returned from AuditContract.
// auditInfo satisfies the asset.AuditInfo interface.
type auditInfo struct {
	output     *output
	recipient  btcutil.Address
	secretHash []byte
	expiration time.Time
}

// Recipient returns a base58 string for the contract's receiving address. Part
// of the asset.AuditInfo interface.
func (ci *auditInfo) Recipient() string {
	return ci.recipient.String()
}

// Expiration returns the expiration time of the contract, which is the earliest
// time that a refund can be issued for an un-redeemed contract. Part of the
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

// Driver implements asset.Driver.
type Driver struct{}

// Setup creates the BTC exchange wallet. Start the wallet with its Run method.
func (d *Driver) Setup(cfg *asset.WalletConfig, logger dex.Logger, network dex.Network) (asset.Wallet, error) {
	return NewWallet(cfg, logger, network)
}

// DecodeCoinID creates a human-readable representation of a coin ID for
// Bitcoin.
func (d *Driver) DecodeCoinID(coinID []byte) (string, error) {
	txid, vout, err := decodeCoinID(coinID)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%v:%d", txid, vout), err
}

// Info returns basic information about the wallet and asset.
func (d *Driver) Info() *asset.WalletInfo {
	return walletInfo
}

func init() {
	asset.Register(BipID, &Driver{})
}

// ExchangeWallet is a wallet backend for Bitcoin. The backend is how the DEX
// client app communicates with the BTC blockchain and wallet. ExchangeWallet
// satisfies the dex.Wallet interface.
type ExchangeWallet struct {
	client       *rpcclient.Client
	node         rpcClient
	wallet       *walletClient
	chainParams  *chaincfg.Params
	log          dex.Logger
	symbol       string
	hasConnected uint32
	// The DEX specifies that change outputs from DEX-monitored transactions are
	// exempt from minimum confirmation limits, so we must track those as they are
	// created.
	changeMtx   sync.RWMutex
	tradeChange map[string]time.Time
	tipChange   func(error)

	fundingMtx   sync.RWMutex
	fundingCoins map[string]*compositeUTXO
}

// Check that ExchangeWallet satisfies the Wallet interface.
var _ asset.Wallet = (*ExchangeWallet)(nil)

// NewWallet is the exported constructor by which the DEX will import the
// exchange wallet. The wallet will shut down when the provided context is
// canceled. The configPath can be an empty string, in which case the standard
// system location of the bitcoind config file is assumed.
func NewWallet(cfg *asset.WalletConfig, logger dex.Logger, network dex.Network) (asset.Wallet, error) {
	var params *chaincfg.Params
	switch network {
	case dex.Mainnet:
		params = &chaincfg.MainNetParams
	case dex.Testnet:
		params = &chaincfg.TestNet3Params
	case dex.Regtest:
		params = &chaincfg.RegressionNetParams
	default:
		return nil, fmt.Errorf("unknown network ID %v", network)
	}

	return BTCCloneWallet(cfg, "btc", logger, network, params, dexbtc.RPCPorts)
}

// BTCCloneWallet creates a wallet backend for a set of network parameters and
// default network ports. A BTC clone can use this method, possibly in
// conjunction with ReadCloneParams, to create a ExchangeWallet for other assets
// with minimal coding.
func BTCCloneWallet(cfg *asset.WalletConfig, symbol string, logger dex.Logger,
	network dex.Network, chainParams *chaincfg.Params, ports dexbtc.NetPorts) (*ExchangeWallet, error) {

	// Read the configuration parameters
	btcCfg, err := dexbtc.LoadConfigFromSettings(cfg.Settings, assetName, network, ports)
	if err != nil {
		return nil, err
	}

	client, err := rpcclient.New(&rpcclient.ConnConfig{
		HTTPPostMode: true,
		DisableTLS:   true,
		Host:         btcCfg.RPCBind + "/wallet/" + cfg.Account,
		User:         btcCfg.RPCUser,
		Pass:         btcCfg.RPCPass,
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating BTC RPC client: %v", err)
	}

	btc := newWallet(cfg, symbol, logger, chainParams, client)
	btc.client = client

	return btc, nil
}

// newWallet creates the ExchangeWallet and starts the block monitor.
func newWallet(cfg *asset.WalletConfig, symbol string, logger dex.Logger,
	chainParams *chaincfg.Params, node rpcClient) *ExchangeWallet {

	wallet := &ExchangeWallet{
		node:         node,
		wallet:       newWalletClient(node, chainParams),
		symbol:       symbol,
		chainParams:  chainParams,
		log:          logger,
		tradeChange:  make(map[string]time.Time),
		tipChange:    cfg.TipChange,
		fundingCoins: make(map[string]*compositeUTXO),
	}

	return wallet
}

var _ asset.Wallet = (*ExchangeWallet)(nil)

// Info returns basic information about the wallet and asset.
func (d *ExchangeWallet) Info() *asset.WalletInfo {
	return walletInfo
}

// Connect connects the wallet to the RPC server. Satisfies the dex.Connector
// interface.
func (btc *ExchangeWallet) Connect(ctx context.Context) (error, *sync.WaitGroup) {
	// Check the version. Do it here, so we can also diagnose a bad connection.
	netVer, codeVer, err := btc.getVersion()
	if err != nil {
		return fmt.Errorf("error getting version: %v", err), nil
	}
	if netVer < minNetworkVersion {
		return fmt.Errorf("reported node version %d is less than minimum %d", netVer, minNetworkVersion), nil
	}
	if codeVer < minProtocolVersion {
		return fmt.Errorf("node software out of date. version %d is less than minimum %d", codeVer, minProtocolVersion), nil
	}
	// If this is the first time connecting, clear the locked coins. This should
	// have been done at shutdown, but shutdown may not have been clean.
	if atomic.SwapUint32(&btc.hasConnected, 1) == 0 {
		err := btc.wallet.LockUnspent(true, nil)
		if err != nil {
			return err, nil
		}
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		btc.run(ctx)
		err := btc.wallet.LockUnspent(true, nil)
		if err != nil {
			btc.log.Errorf("failed to unlock %s outputs on shutdown: %v", btc.symbol, err)
		}
	}()
	return nil, &wg
}

// Balance should return the total available funds in the wallet.
// Note that after calling Fund, the amount returned by Balance may change
// by more than the value funded. Because of the DEX confirmation requirements,
// a simple getbalance with a minconf argument is not sufficient to see all
// balance available for the exchange. Instead, all wallet UTXOs are scanned
// for those that match DEX requirements. Part of the asset.Wallet interface.
func (btc *ExchangeWallet) Balance(confs uint32) (uint64, uint64, error) {
	_, _, sum, unconf, err := btc.spendableUTXOs(confs)
	return sum, unconf, err
}

// Fund selects utxos (as asset.Coin) for use in an order. Any Coins returned
// will be locked. Part of the asset.Wallet interface.
func (btc *ExchangeWallet) Fund(value uint64, nfo *dex.Asset) (asset.Coins, error) {
	if value == 0 {
		return nil, fmt.Errorf("cannot fund value = 0")
	}
	utxos, _, _, unconf, err := btc.spendableUTXOs(nfo.FundConf)
	if err != nil {
		return nil, fmt.Errorf("error parsing unspent outputs: %v", err)
	}
	if len(utxos) == 0 {
		return nil, fmt.Errorf("no funds ready to spend. %.8f unconfirmed", float64(unconf)/1e8)
	}
	var sum uint64
	var size uint32
	var coins asset.Coins
	var spents []*output
	fundingCoins := make(map[string]*compositeUTXO)

	isEnoughWith := func(unspent *compositeUTXO) bool {
		return sum+unspent.amount >= btc.reqFunds(value, size+unspent.input.VBytes(), nfo)
	}

	addUTXO := func(unspent *compositeUTXO) error {
		v := unspent.amount
		op := newOutput(btc.node, unspent.txHash, unspent.vout, v, unspent.redeemScript)
		coins = append(coins, op)
		spents = append(spents, op)
		size += unspent.input.VBytes()
		fundingCoins[op.String()] = unspent
		sum += v
		return nil
	}

out:
	for {
		// If there are none left, we don't have enough.
		if len(utxos) == 0 {
			return nil, fmt.Errorf("not enough to cover requested funds + fees = %d",
				btc.reqFunds(value, size, nfo))
		}
		// On each loop, find the smallest UTXO that is enough for the value. If
		// no UTXO is large enough, add the largest and continue.
		var txout *compositeUTXO
		for _, txout = range utxos {
			if isEnoughWith(txout) {
				err = addUTXO(txout)
				if err != nil {
					return nil, err
				}
				break out
			}
		}
		// Append the last output, which is the largest.
		err = addUTXO(txout)
		if err != nil {
			return nil, err
		}
		// Pop the utxo from the unspents
		utxos = utxos[:len(utxos)-1]
	}

	err = btc.wallet.LockUnspent(false, spents)
	if err != nil {
		return nil, err
	}

	btc.fundingMtx.Lock()
	for opID, utxo := range fundingCoins {
		btc.fundingCoins[opID] = utxo
	}
	btc.fundingMtx.Unlock()

	return coins, nil
}

// ReturnCoins unlocks coins. This would be used in the case of a
// canceled or partially filled order. Part of the asset.Wallet interface.
func (btc *ExchangeWallet) ReturnCoins(unspents asset.Coins) error {
	if len(unspents) == 0 {
		return fmt.Errorf("cannot return zero coins")
	}
	ops := make([]*output, 0, len(unspents))
	btc.fundingMtx.Lock()
	defer btc.fundingMtx.Unlock()
	for _, unspent := range unspents {
		op, err := btc.convertCoin(unspent)
		if err != nil {
			return fmt.Errorf("error converting coin: %v", err)
		}
		ops = append(ops, op)
		delete(btc.fundingCoins, outpointID(op.txHash.String(), op.vout))
	}
	return btc.wallet.LockUnspent(true, ops)
}

// FundingCoins gets funding coins for the coin IDs. The coins are locked. This
// method might be called to reinitialize an order from data stored externally.
// This method will only return funding coins, e.g. unspent transaction outputs.
func (btc *ExchangeWallet) FundingCoins(ids []dex.Bytes) (asset.Coins, error) {
	// First check if we have the coins in cache.
	coins := make(asset.Coins, 0, len(ids))
	notFound := make(map[string]struct{})
	btc.fundingMtx.RLock()
	for _, id := range ids {
		txHash, vout, err := decodeCoinID(id)
		if err != nil {
			btc.fundingMtx.RUnlock()
			return nil, err
		}
		opID := outpointID(txHash.String(), vout)
		fundingCoin, found := btc.fundingCoins[opID]
		if found {
			coins = append(coins, newOutput(btc.node, txHash, vout, fundingCoin.amount, fundingCoin.redeemScript))
			continue
		}
		notFound[opID] = struct{}{}
	}
	btc.fundingMtx.RUnlock()
	if len(notFound) == 0 {
		return coins, nil
	}
	_, utxoMap, _, _, err := btc.spendableUTXOs(0)
	if err != nil {
		return nil, err
	}
	lockers := make([]*output, 0, len(ids))
	btc.fundingMtx.Lock()
	defer btc.fundingMtx.Unlock()
	for _, id := range ids {
		txHash, vout, err := decodeCoinID(id)
		if err != nil {
			return nil, err
		}
		opID := outpointID(txHash.String(), vout)
		utxo, found := utxoMap[opID]
		if !found {
			return nil, fmt.Errorf("funding coin %s not found", opID)
		}
		btc.fundingCoins[opID] = utxo
		coin := newOutput(btc.node, utxo.txHash, utxo.vout, utxo.amount, utxo.redeemScript)
		coins = append(coins, coin)
		lockers = append(lockers, coin)
		delete(notFound, opID)
		if len(notFound) == 0 {
			break
		}
	}
	if len(notFound) != 0 {
		ids := make([]string, 0, len(notFound))
		for opID := range notFound {
			ids = append(ids, opID)
		}
		return nil, fmt.Errorf("coins not found: %s", strings.Join(ids, ", "))
	}
	err = btc.wallet.LockUnspent(false, lockers)
	if err != nil {
		return nil, err
	}
	return coins, nil
}

// Unlock unlocks the ExchangeWallet. The pw supplied should be the same as the
// password for the underlying bitcoind wallet which will also be unlocked.
func (btc *ExchangeWallet) Unlock(pw string, dur time.Duration) error {
	return btc.wallet.Unlock(pw, dur)
}

// Lock locks the ExchangeWallet and the underlying bitcoind wallet.
func (btc *ExchangeWallet) Lock() error {
	return btc.wallet.Lock()
}

// Swap sends the swap contracts and prepares the receipts.
func (btc *ExchangeWallet) Swap(swaps *asset.Swaps, nfo *dex.Asset) ([]asset.Receipt, asset.Coin, error) {
	var contracts [][]byte
	var totalOut, totalIn uint64
	// Start with an empty MsgTx.
	baseTx := wire.NewMsgTx(wire.TxVersion)
	// Add the funding utxos.
	opIDs := make([]string, 0, len(swaps.Inputs))
	for _, coin := range swaps.Inputs {
		output, err := btc.convertCoin(coin)
		if err != nil {
			return nil, nil, fmt.Errorf("error converting coin: %v", err)
		}
		if output.value == 0 {
			return nil, nil, fmt.Errorf("zero-valued output detected for %s:%d", output.txHash, output.vout)
		}
		totalIn += output.value
		prevOut := wire.NewOutPoint(&output.txHash, output.vout)
		txIn := wire.NewTxIn(prevOut, []byte{}, nil)
		baseTx.AddTxIn(txIn)
		opIDs = append(opIDs, outpointID(output.txHash.String(), output.vout))
	}
	// Add the contract outputs.
	for _, contract := range swaps.Contracts {
		totalOut += contract.Value
		// revokeAddr is the address that will receive the refund if the contract is
		// abandoned.
		revokeAddr, err := btc.wallet.AddressPKH()
		if err != nil {
			return nil, nil, fmt.Errorf("error creating revocation address: %v", err)
		}
		// Create the contract, a P2SH redeem script.
		pkScript, err := dexbtc.MakeContract(contract.Address, revokeAddr.String(), contract.SecretHash, int64(contract.LockTime), btc.chainParams)
		if err != nil {
			return nil, nil, fmt.Errorf("unable to create pubkey script for address %s: %v", contract.Address, err)
		}
		contracts = append(contracts, pkScript)
		// Make the P2SH address and pubkey script.
		scriptAddr, err := btcutil.NewAddressScriptHash(pkScript, btc.chainParams)
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
	// Grab a change address.
	changeAddr, err := btc.wallet.ChangeAddress()
	if err != nil {
		return nil, nil, fmt.Errorf("error creating change address: %v", err)
	}
	// Prepare the receipts.
	swapCount := len(swaps.Contracts)
	if len(baseTx.TxOut) < swapCount {
		return nil, nil, fmt.Errorf("fewer outputs than swaps. %d < %d", len(baseTx.TxOut), swapCount)
	}
	// Sign, add change, and send the transaction.
	msgTx, change, err := btc.sendWithReturn(baseTx, changeAddr, totalIn, totalOut, nfo)
	if err != nil {
		return nil, nil, err
	}
	receipts := make([]asset.Receipt, 0, swapCount)
	txHash := msgTx.TxHash()
	for i, contract := range swaps.Contracts {
		receipts = append(receipts, &swapReceipt{
			output:     newOutput(btc.node, &txHash, uint32(i), contract.Value, contracts[i]),
			expiration: time.Unix(int64(contract.LockTime), 0).UTC(),
		})
	}
	// Delete the utxos from the cache.
	btc.fundingMtx.Lock()
	defer btc.fundingMtx.Unlock()
	for i := range opIDs {
		delete(btc.fundingCoins, opIDs[i])
	}
	return receipts, change, nil
}

// Redeem sends the redemption transaction, completing the atomic swap.
func (btc *ExchangeWallet) Redeem(redemptions []*asset.Redemption, nfo *dex.Asset) ([]dex.Bytes, asset.Coin, error) {
	// Create a transaction that spends the referenced contract.
	msgTx := wire.NewMsgTx(wire.TxVersion)
	var totalIn uint64
	var contracts [][]byte
	var addresses []btcutil.Address
	for _, r := range redemptions {
		cinfo, ok := r.Spends.(*auditInfo)
		if !ok {
			return nil, nil, fmt.Errorf("Redemption contract info of wrong type")
		}
		// Extract the swap contract recipient and secret hash and check the secret
		// hash against the hash of the provided secret.
		_, receiver, _, secretHash, err := dexbtc.ExtractSwapDetails(cinfo.output.redeem, btc.chainParams)
		if err != nil {
			return nil, nil, fmt.Errorf("error extracting swap addresses: %v", err)
		}
		checkSecretHash := sha256.Sum256(r.Secret)
		if !bytes.Equal(checkSecretHash[:], secretHash) {
			return nil, nil, fmt.Errorf("secret hash mismatch")
		}
		addresses = append(addresses, receiver)
		contracts = append(contracts, cinfo.output.redeem)
		prevOut := wire.NewOutPoint(&cinfo.output.txHash, cinfo.output.vout)
		txIn := wire.NewTxIn(prevOut, []byte{}, nil)
		// Enable locktime
		// https://github.com/bitcoin/bips/blob/master/bip-0125.mediawiki#Spending_wallet_policy
		txIn.Sequence = wire.MaxTxInSequenceNum - 1
		msgTx.AddTxIn(txIn)
		totalIn += cinfo.output.value
	}

	// Calculate the size and the fees.
	size := msgTx.SerializeSize() + dexbtc.RedeemSwapSigScriptSize*len(redemptions) + dexbtc.P2WPKHOutputSize
	fee := nfo.FeeRate * uint64(size)
	if fee > totalIn {
		return nil, nil, fmt.Errorf("redeem tx not worth the fees")
	}
	// Send the funds back to the exchange wallet.
	redeemAddr, err := btc.wallet.ChangeAddress()
	if err != nil {
		return nil, nil, fmt.Errorf("error getting new address from the wallet: %v", err)
	}
	pkScript, err := txscript.PayToAddrScript(redeemAddr)
	if err != nil {
		return nil, nil, fmt.Errorf("error creating change script: %v", err)
	}
	txOut := wire.NewTxOut(int64(totalIn-fee), pkScript)
	// One last check for dust.
	if dexbtc.IsDust(txOut, nfo.FeeRate) {
		return nil, nil, fmt.Errorf("redeem output is dust")
	}
	msgTx.AddTxOut(txOut)
	// Sign the inputs.
	for i, r := range redemptions {
		contract := contracts[i]
		redeemSig, redeemPubKey, err := btc.createSig(msgTx, i, contract, addresses[i])
		if err != nil {
			return nil, nil, err
		}
		redeemSigScript, err := dexbtc.RedeemP2SHContract(contract, redeemSig, redeemPubKey, r.Secret)
		if err != nil {
			return nil, nil, err
		}
		msgTx.TxIn[i].SignatureScript = redeemSigScript
	}
	// Send the transaction.
	checkHash := msgTx.TxHash()
	txHash, err := btc.node.SendRawTransaction(msgTx, false)
	if err != nil {
		return nil, nil, err
	}
	if *txHash != checkHash {
		return nil, nil, fmt.Errorf("redemption sent, but received unexpected transaction ID back from RPC server. "+
			"expected %s, got %s", *txHash, checkHash)
	}
	// Log the change output.
	btc.addChange(txHash.String(), 0)
	coinIDs := make([]dex.Bytes, 0, len(redemptions))
	for i := range redemptions {
		coinIDs = append(coinIDs, toCoinID(txHash, uint32(i)))
	}
	return coinIDs, newOutput(btc.node, txHash, 0, uint64(txOut.Value), nil), nil
}

// SignMessage signs the message with the private key associated with the
// specified unspent coin. A slice of pubkeys required to spend the coin and a
// signature for each pubkey are returned.
func (btc *ExchangeWallet) SignMessage(coin asset.Coin, msg dex.Bytes) (pubkeys, sigs []dex.Bytes, err error) {
	output, err := btc.convertCoin(coin)
	if err != nil {
		return nil, nil, fmt.Errorf("error converting coin: %v", err)
	}
	btc.fundingMtx.RLock()
	utxo := btc.fundingCoins[output.String()]
	btc.fundingMtx.RUnlock()
	if utxo == nil {
		return nil, nil, fmt.Errorf("no utxo found for %s", output)
	}
	privKey, err := btc.wallet.PrivKeyForAddress(utxo.address)
	if err != nil {
		return nil, nil, err
	}
	pk := privKey.PubKey()
	sig, err := privKey.Sign(msg)
	if err != nil {
		return nil, nil, err
	}
	pubkeys = append(pubkeys, pk.SerializeCompressed())
	sigs = append(sigs, sig.Serialize())
	return
}

// AuditContract retrieves information about a swap contract on the blockchain.
// AuditContract would be used to audit the counter-party's contract during a
// swap.
func (btc *ExchangeWallet) AuditContract(coinID dex.Bytes, contract dex.Bytes) (asset.AuditInfo, error) {
	txHash, vout, err := decodeCoinID(coinID)
	if err != nil {
		return nil, err
	}
	// Get the receiving address.
	_, receiver, stamp, secretHash, err := dexbtc.ExtractSwapDetails(contract, btc.chainParams)
	if err != nil {
		return nil, fmt.Errorf("error extracting swap addresses: %v", err)
	}
	// Get the contracts P2SH address from the tx output's pubkey script.
	txOut, err := btc.node.GetTxOut(txHash, vout, true)
	if err != nil {
		var rpcErr *btcjson.RPCError
		if errors.As(err, &rpcErr) {
			if rpcErr.Code == btcjson.ErrRPCInvalidAddressOrKey {
				return nil, asset.CoinNotFoundError
			}
		}
		return nil, fmt.Errorf("error finding unspent contract: %v", err)
	}
	pkScript, err := hex.DecodeString(txOut.ScriptPubKey.Hex)
	if err != nil {
		return nil, fmt.Errorf("error decoding pubkey script from hex '%s': %v",
			txOut.ScriptPubKey.Hex, err)
	}
	// Check for standard P2SH.
	scriptClass, addrs, numReq, err := txscript.ExtractPkScriptAddrs(pkScript, btc.chainParams)
	if err != nil {
		return nil, fmt.Errorf("error extracting script addresses from '%x': %v", pkScript, err)
	}
	if scriptClass != txscript.ScriptHashTy {
		return nil, fmt.Errorf("unexpected script class %d", scriptClass)
	}
	// These last two checks are probably overkill.
	if numReq != 1 {
		return nil, fmt.Errorf("unexpected number of signatures expected for P2SH script: %d", numReq)
	}
	if len(addrs) != 1 {
		return nil, fmt.Errorf("unexpected number of addresses for P2SH script: %d", len(addrs))
	}
	// Compare the contract hash to the P2SH address.
	contractHash := btcutil.Hash160(contract)
	addr := addrs[0]
	if !bytes.Equal(contractHash, addr.ScriptAddress()) {
		return nil, fmt.Errorf("contract hash doesn't match script address. %x != %x",
			contractHash, addr.ScriptAddress())
	}
	return &auditInfo{
		output:     newOutput(btc.node, txHash, vout, toSatoshi(txOut.Value), contract),
		recipient:  receiver,
		secretHash: secretHash,
		expiration: time.Unix(int64(stamp), 0).UTC(),
	}, nil
}

// FindRedemption attempts to find the input that spends the specified output,
// and returns the secret key if it does. The inputs are the txid and vout of
// the contract output that was redeemed. This method only works with contracts
// sent from the wallet.
func (btc *ExchangeWallet) FindRedemption(ctx context.Context, coinID dex.Bytes) (dex.Bytes, error) {
	txHash, vout, err := decodeCoinID(coinID)
	if err != nil {
		return nil, err
	}
	txid := txHash.String()
	// Get the wallet transaction.
	tx, err := btc.wallet.GetTransaction(txHash.String())
	if err != nil {
		return nil, fmt.Errorf("error finding transaction %s in wallet: %v", txHash, err)
	}
	if tx.BlockIndex == 0 {
		// This is a mempool transaction, GetRawTransactionVerbose works on mempool
		// transactions, so grab the contract from there and prepare a hash.
		rawTx, err := btc.node.GetRawTransactionVerbose(txHash)
		if err != nil {
			return nil, fmt.Errorf("error getting contract details from mempool")
		}
		if int(vout) > len(rawTx.Vout)-1 {
			return nil, fmt.Errorf("vout index %d out of range for transaction %s", vout, txHash)
		}
		contractHash, err := dexbtc.ExtractContractHash(rawTx.Vout[vout].ScriptPubKey.Hex, btc.chainParams)
		if err != nil {
			return nil, err
		}
		// Only need to look at other mempool transactions. For each txid, get the
		// verbose transaction and check each input's previous outpoint.
		txs, err := btc.node.GetRawMempool()
		if err != nil {
			return nil, fmt.Errorf("error retrieve mempool transactions")
		}
		for _, txHash := range txs {
			tx, err := btc.node.GetRawTransactionVerbose(txHash)
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
					key, err := dexbtc.FindKeyPush(sigScript, contractHash, btc.chainParams)
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
	blockHeight := tx.BlockIndex
	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("redemption search canceled")
		default:
		}
		block, err := btc.getVerboseBlockTxs(blockHash.String())
		if err != nil {
			return nil, fmt.Errorf("error fetching verbose block '%s': %v", blockHash, err)
		}
		// If this is the swap contract's block, grab the contract hash.
		if *blockHash == contractBlock {
			for i := range block.Tx {
				if block.Tx[i].Txid == txid {
					vouts := block.Tx[i].Vout
					if len(vouts) < int(vout)+1 {
						return nil, fmt.Errorf("vout %d not found in tx %s", vout, txid)
					}
					contractHash, err = dexbtc.ExtractContractHash(vouts[vout].ScriptPubKey.Hex, btc.chainParams)
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
		for i := range block.Tx {
			for j := range block.Tx[i].Vin {
				if block.Tx[i].Vin[j].Txid == txid && block.Tx[i].Vin[j].Vout == vout {
					// Found it. Extract the key.
					vin := block.Tx[i].Vin[j]
					sigScript, err := hex.DecodeString(vin.ScriptSig.Hex)
					if err != nil {
						return nil, fmt.Errorf("error decoding scriptSig '%s': %v", vin.ScriptSig.Hex, err)
					}
					key, err := dexbtc.FindKeyPush(sigScript, contractHash, btc.chainParams)
					if err != nil {
						return nil, fmt.Errorf("error extracting key from '%s': %v", vin.ScriptSig.Hex, err)
					}
					return key, nil
				}
			}
		}
		// Not found in this block. Get the next block
		blockHeight++
		blockHash, err = btc.node.GetBlockHash(blockHeight)
		if err != nil {
			return nil, fmt.Errorf("spending transaction not found")
		}
	}
	// No viable way to get here.
}

// Refund revokes a contract. This can only be used after the time lock has
// expired.
func (btc *ExchangeWallet) Refund(receipt asset.Receipt, nfo *dex.Asset) error {
	op := receipt.Coin()
	txHash, vout, err := decodeCoinID(op.ID())
	if err != nil {
		return err
	}
	// Grab the unspent output to make sure it's good and to get the value.
	utxo, err := btc.node.GetTxOut(txHash, vout, true)
	if err != nil {
		return fmt.Errorf("error finding unspent contract: %v", err)
	}
	val := toSatoshi(utxo.Value)
	// DRAFT NOTE: The wallet does not store this contract, even though it was
	// known when the init transaction was created. The DEX should store this
	// information for persistence across sessions. Care has been taken to ensure
	// that any type satisfying asset.Coin can be passed to the Wallet's methods,
	// so the DEX can create it's own asset.Coin to issue a redeem or refund
	// after a restart, for example, but the (asset.Coin).Redeem script
	// (the swap contract itself, including the counter-party's pubkey) must be
	// included.
	redeem := op.Redeem()
	sender, _, lockTime, _, err := dexbtc.ExtractSwapDetails(redeem, btc.chainParams)
	if err != nil {
		return fmt.Errorf("error extracting swap addresses: %v", err)
	}
	// Create the transaction that spends the contract.
	msgTx := wire.NewMsgTx(wire.TxVersion)
	msgTx.LockTime = uint32(lockTime)
	prevOut := wire.NewOutPoint(txHash, vout)
	txIn := wire.NewTxIn(prevOut, []byte{}, nil)
	txIn.Sequence = wire.MaxTxInSequenceNum - 1
	msgTx.AddTxIn(txIn)
	// Calculate fees and add the change output.
	size := msgTx.SerializeSize() + dexbtc.RefundSigScriptSize + dexbtc.P2WPKHOutputSize
	fee := nfo.FeeRate * uint64(size)
	if fee > val {
		return fmt.Errorf("refund tx not worth the fees")
	}
	refundAddr, err := btc.wallet.ChangeAddress()
	if err != nil {
		return fmt.Errorf("error getting new address from the wallet: %v", err)
	}
	pkScript, err := txscript.PayToAddrScript(refundAddr)
	if err != nil {
		return fmt.Errorf("error creating change script: %v", err)
	}
	txOut := wire.NewTxOut(int64(val-fee), pkScript)
	// One last check for dust.
	if dexbtc.IsDust(txOut, nfo.FeeRate) {
		return fmt.Errorf("refund output is dust")
	}
	msgTx.AddTxOut(txOut)
	// Sign it.
	refundSig, refundPubKey, err := btc.createSig(msgTx, 0, redeem, sender)
	if err != nil {
		return err
	}
	redeemSigScript, err := dexbtc.RefundP2SHContract(redeem, refundSig, refundPubKey)
	if err != nil {
		return err
	}
	txIn.SignatureScript = redeemSigScript
	// Send it.
	checkHash := msgTx.TxHash()
	refundHash, err := btc.node.SendRawTransaction(msgTx, false)
	if err != nil {
		return err
	}
	if *refundHash != checkHash {
		return fmt.Errorf("refund sent, but received unexpected transaction ID back from RPC server. "+
			"expected %s, got %s", *refundHash, checkHash)
	}
	return nil
}

// Address returns a new external address from the wallet.
func (btc *ExchangeWallet) Address() (string, error) {
	addr, err := btc.wallet.AddressPKH()
	return addr.String(), err
}

// PayFee sends the dex registration fee. Transaction fees are in addition to
// the registration fee, and the fee rate is taken from the DEX configuration.
func (btc *ExchangeWallet) PayFee(address string, regFee uint64, nfo *dex.Asset) (asset.Coin, error) {
	txHash, vout, sent, err := btc.send(address, regFee, nfo.FeeRate, false)
	if err != nil {
		btc.log.Errorf("PayFee error address = '%s', fee = %d: %v", address, regFee, err)
		return nil, err
	}
	return newOutput(btc.node, txHash, vout, sent, nil), nil
}

// Withdraw withdraws funds to the specified address. Fees are subtracted from
// the value. feeRate is in units of atoms/byte.
func (btc *ExchangeWallet) Withdraw(address string, value, feeRate uint64) (asset.Coin, error) {
	txHash, vout, sent, err := btc.send(address, value, feeRate, true)
	if err != nil {
		btc.log.Errorf("Withdraw error address = '%s', fee = %d: %v", address, value, err)
		return nil, err
	}
	return newOutput(btc.node, txHash, vout, sent, nil), nil
}

// ValidateSecret checks that the secret satisfies the contract.
func (btc *ExchangeWallet) ValidateSecret(secret, secretHash []byte) bool {
	h := sha256.Sum256(secret)
	return bytes.Equal(h[:], secretHash)
}

// Send the value to the address, with the given fee rate. If subtract is true,
// the fees will be subtracted from the value. If false, the fees are in
// addition to the value. feeRate is in units of atoms/byte.
func (btc *ExchangeWallet) send(address string, val uint64, feeRate uint64, subtract bool) (*chainhash.Hash, uint32, uint64, error) {
	txHash, err := btc.wallet.SendToAddress(address, val, feeRate, subtract)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("SendToAddress error: %v", err)
	}
	tx, err := btc.wallet.GetTransaction(txHash.String())
	if err != nil {
		// btc.log.Errorf("error getting transaction for successful fee: %v", err)
		return nil, 0, 0, fmt.Errorf("failed to fetch transaction after send: %v", err)
	}
	for _, details := range tx.Details {
		if details.Address == address {
			return txHash, details.Vout, toSatoshi(details.Amount), nil
		}
	}
	return nil, 0, 0, fmt.Errorf("failed to locate transaction vout")
}

// Confirmations gets the number of confirmations for the specified coin ID.
// The coin must be known to the wallet, but need not be unspent.
func (btc *ExchangeWallet) Confirmations(id dex.Bytes) (uint32, error) {
	txHash, _, err := decodeCoinID(id)
	if err != nil {
		return 0, err
	}
	tx, err := btc.wallet.GetTransaction(txHash.String())
	if err != nil {
		return 0, err
	}
	return uint32(tx.Confirmations), nil
}

// run pings for new blocks and runs the tipChange callback function when the
// block changes.
func (btc *ExchangeWallet) run(ctx context.Context) {
	h, err := btc.node.GetBestBlockHash()
	if err != nil {
		btc.tipChange(fmt.Errorf("error initializing best block for %s: %v", btc.symbol, err))
	}
	tipHash := *h
	ticker := time.NewTicker(blockTicker)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			h, err := btc.node.GetBestBlockHash()
			if err != nil {
				btc.tipChange(fmt.Errorf("failed to get best block hash from %s node", btc.symbol))
				continue
			}
			if *h != tipHash {
				tipHash = *h
				btc.tipChange(nil)
			}
		case <-ctx.Done():
			return
		}
	}
}

// reqFunds calculates the total total value needed to fund a swap of the
// specified value, using a given total funding utxo input size (bytes).
func (btc *ExchangeWallet) reqFunds(val uint64, funding uint32, nfo *dex.Asset) uint64 {
	return calc.RequiredFunds(val, funding, nfo)
}

// convertCoin converts the asset.Coin to an output.
func (btc *ExchangeWallet) convertCoin(coin asset.Coin) (*output, error) {
	op, _ := coin.(*output)
	if op != nil {
		return op, nil
	}
	txHash, vout, err := decodeCoinID(coin.ID())
	if err != nil {
		return nil, err
	}
	return newOutput(btc.node, txHash, vout, coin.Value(), coin.Redeem()), nil
}

// sendWithReturn sends the unsigned transaction with an added output (unless
// dust) for the change.
func (btc *ExchangeWallet) sendWithReturn(baseTx *wire.MsgTx,
	addr btcutil.Address, totalIn, totalOut uint64, nfo *dex.Asset) (*wire.MsgTx, *output, error) {

	msgTx, err := btc.wallet.SignTx(baseTx)
	if err != nil {
		return nil, nil, fmt.Errorf("signing error: %v", err)
	}
	size := msgTx.SerializeSize()
	minFee := nfo.FeeRate * uint64(size)
	remaining := totalIn - totalOut
	if minFee > remaining {
		return nil, nil, fmt.Errorf("not enough funds to cover minimum fee rate. %d < %d",
			totalIn, minFee+totalOut)
	}
	changeScript, err := txscript.PayToAddrScript(addr)
	if err != nil {
		return nil, nil, fmt.Errorf("error creating change script: %v", err)
	}
	changeIdx := len(baseTx.TxOut)
	changeOutput := wire.NewTxOut(int64(remaining-minFee), changeScript)
	var changeAdded bool
	isDust := dexbtc.IsDust(changeOutput, nfo.FeeRate)
	sigCycles := 1
	if !isDust {
		// Add the change output with a recalculated fees
		size += dexbtc.P2WPKHOutputSize
		fee := nfo.FeeRate * uint64(size)
		changeOutput.Value = int64(remaining - fee)
		baseTx.AddTxOut(changeOutput)
		changeAdded = true
		// Find the best fee rate by closing in on it in a loop.
		tried := map[uint64]uint8{fee: 1}
		for {
			sigCycles++
			msgTx, err = btc.wallet.SignTx(baseTx)
			if err != nil {
				return nil, nil, fmt.Errorf("signing error: %v", err)
			}
			size := msgTx.SerializeSize()
			reqFee := nfo.FeeRate * uint64(size)
			if reqFee > remaining {
				// I can't imagine a scenario where this condition would be true, but
				// I'd hate to be wrong.
				btc.log.Errorf("reached the impossible place. in = %d, out = %d, reqFee = %d, lastFee = %d",
					totalIn, totalOut, reqFee, fee)
				return nil, nil, fmt.Errorf("change error")
			}
			if fee == reqFee || (reqFee < fee && tried[reqFee] == 1) {
				// If a lower fee appears available, but it's already been attempted and
				// had a longer serialized size, the current fee is likely as good as
				// it gets.
				break
			}
			// We must have some room for improvement
			tried[fee] = 1
			fee = reqFee
			changeOutput.Value = int64(remaining - fee)
			if dexbtc.IsDust(changeOutput, nfo.FeeRate) {
				// Another condition that should be impossible, but check anyway in case
				// the maximum fee was underestimated causing the first check to be
				// missed.
				btc.log.Errorf("reached the impossible place. in = %d, out = %d, reqFee = %d, lastFee = %d",
					totalIn, totalOut, reqFee, fee)
				return nil, nil, fmt.Errorf("dust error")
			}
			continue
		}
	}
	checkHash := msgTx.TxHash()
	btc.log.Debugf("%d signature cycles to converge on fees for tx %s", sigCycles, checkHash)
	txHash, err := btc.node.SendRawTransaction(msgTx, false)
	if err != nil {
		return nil, nil, err
	}
	if *txHash != checkHash {
		return nil, nil, fmt.Errorf("transaction sent, but received unexpected transaction ID back from RPC server. "+
			"expected %s, got %s", checkHash, *txHash)
	}
	if !isDust {
		btc.addChange(txHash.String(), uint32(changeIdx))
	}
	var change *output
	if changeAdded {
		change = newOutput(btc.node, txHash, uint32(len(msgTx.TxOut)-1), uint64(changeOutput.Value), nil)
	}
	return msgTx, change, nil
}

// createSig creates and returns the serialized raw signature and compressed
// pubkey for a transaction input signature.
func (btc *ExchangeWallet) createSig(tx *wire.MsgTx, idx int, pkScript []byte, addr btcutil.Address) (sig, pubkey []byte, err error) {
	privKey, err := btc.wallet.PrivKeyForAddress(addr.String())
	if err != nil {
		return nil, nil, err
	}
	sig, err = txscript.RawTxInSignature(tx, idx, pkScript, txscript.SigHashAll, privKey)
	if err != nil {
		return nil, nil, err
	}
	return sig, privKey.PubKey().SerializeCompressed(), nil
}

// Combines the RPC type with the spending input information.
type compositeUTXO struct {
	txHash       *chainhash.Hash
	vout         uint32
	address      string
	redeemScript []byte
	amount       uint64
	input        *dexbtc.SpendInfo
}

// spendableUTXOs filters the RPC utxos for those that are spendable with
// with regards to the DEX's configuration. The UTXOs will be sorted by
// ascending value.
func (btc *ExchangeWallet) spendableUTXOs(confs uint32) ([]*compositeUTXO, map[string]*compositeUTXO, uint64, uint64, error) {
	unspents, err := btc.wallet.ListUnspent()
	if err != nil {
		return nil, nil, 0, 0, err
	}
	sort.Slice(unspents, func(i, j int) bool { return unspents[i].Amount < unspents[j].Amount })
	var sum, unconf uint64
	utxos := make([]*compositeUTXO, 0, len(unspents))
	utxoMap := make(map[string]*compositeUTXO, len(unspents))
	for _, txout := range unspents {
		if txout.Confirmations >= confs || btc.isDEXChange(txout.TxID, txout.Vout) {
			nfo, err := dexbtc.InputInfo(txout.ScriptPubKey, txout.RedeemScript, btc.chainParams)
			if err != nil {
				return nil, nil, 0, 0, fmt.Errorf("error reading asset info: %v", err)
			}
			txHash, err := chainhash.NewHashFromStr(txout.TxID)
			if err != nil {
				return nil, nil, 0, 0, fmt.Errorf("error decoding txid in ListUnspentResult: %v", err)
			}
			utxo := &compositeUTXO{
				txHash:       txHash,
				vout:         txout.Vout,
				address:      txout.Address,
				redeemScript: txout.RedeemScript,
				amount:       toSatoshi(txout.Amount),
				input:        nfo,
			}
			utxos = append(utxos, utxo)
			utxoMap[outpointID(txout.TxID, txout.Vout)] = utxo
			sum += toSatoshi(txout.Amount)
		} else {
			unconf += toSatoshi(txout.Amount)
		}
	}
	return utxos, utxoMap, sum, unconf, nil
}

// addChange adds the output to the list of DEX-trade change outputs. These
// outputs are tracked because change outputs from DEX-monitored trades are
// exempt from the fundconf confirmations requirement.
func (btc *ExchangeWallet) addChange(txid string, vout uint32) {
	btc.changeMtx.Lock()
	defer btc.changeMtx.Unlock()
	btc.tradeChange[outpointID(txid, vout)] = time.Now()
}

// isDEXChange checks whether the specified output is a change output from a
// DEX trade.
func (btc *ExchangeWallet) isDEXChange(txid string, vout uint32) bool {
	btc.changeMtx.RLock()
	_, found := btc.tradeChange[outpointID(txid, vout)]
	btc.changeMtx.RUnlock()
	return found
}

// Convert the BTC value to satoshi.
func toSatoshi(v float64) uint64 {
	return uint64(math.Round(v * 1e8))
}

// verboseBlockTxs is a partial btcjson.GetBlockVerboseResult with
// key "rawtx" -> "tx".
type verboseBlockTxs struct {
	Hash   string                `json:"hash"`
	Height uint64                `json:"height"`
	Tx     []btcjson.TxRawResult `json:"tx"`
}

// getVerboseBlockTxs gets a list of TxRawResult for a block. The
// rpcclient.Client's GetBlockVerboseTx appears to be broken with the current
// version of bitcoind. Though it's not a wallet method, it uses the wallet's
// RPC call method for convenience.
func (btc *ExchangeWallet) getVerboseBlockTxs(blockID string) (*verboseBlockTxs, error) {
	blk := new(verboseBlockTxs)
	// verbosity = 2 -> verbose transactions
	err := btc.wallet.call(methodGetBlockVerboseTx, anylist{blockID, 2}, blk)
	if err != nil {
		return nil, err
	}
	return blk, nil
}

// getVersion gets the current BTC network and protocol versions.
func (btc *ExchangeWallet) getVersion() (uint64, uint64, error) {
	r := &struct {
		Version         uint64 `json:"version"`
		ProtocolVersion uint64 `json:"protocolversion"`
	}{}
	err := btc.wallet.call(methodGetNetworkInfo, nil, r)
	if err != nil {
		return 0, 0, err
	}
	return r.Version, r.ProtocolVersion, nil
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
