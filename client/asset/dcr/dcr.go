// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dcr

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io/ioutil"
	"math"
	"sort"
	"strconv"
	"sync"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
	dexdcr "decred.org/dcrdex/dex/dcr"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrec"
	"github.com/decred/dcrd/dcrec/secp256k1/v2"
	"github.com/decred/dcrd/dcrutil/v2"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types/v2"
	"github.com/decred/dcrd/rpcclient/v5"
	"github.com/decred/dcrd/txscript/v2"
	"github.com/decred/dcrd/wire"
	walletjson "github.com/decred/dcrwallet/rpc/jsonrpc/types"
)

const (
	BipID                = 42
	defaultWithdrawalFee = 10
)

// How often to check the tip hash.
var (
	blockTicker = time.Second
	walletInfo  = &asset.WalletInfo{
		ConfigPath: defaultConfigPath,
		Name:       "Decred",
		FeeRate:    defaultWithdrawalFee,
		Units:      "atoms",
	}
)

// rpcClient is an rpcclient.Client, or a stub for testing.
type rpcClient interface {
	SendRawTransaction(tx *wire.MsgTx, allowHighFees bool) (*chainhash.Hash, error)
	GetTxOut(txHash *chainhash.Hash, index uint32, mempool bool) (*chainjson.GetTxOutResult, error)
	GetBestBlock() (*chainhash.Hash, int64, error)
	GetBlockHash(blockHeight int64) (*chainhash.Hash, error)
	GetBlockVerbose(blockHash *chainhash.Hash, verboseTx bool) (*chainjson.GetBlockVerboseResult, error)
	GetRawMempool(txType chainjson.GetRawMempoolTxTypeCmd) ([]*chainhash.Hash, error)
	GetRawTransactionVerbose(txHash *chainhash.Hash) (*chainjson.TxRawResult, error)
	ListUnspentMin(minConf int) ([]walletjson.ListUnspentResult, error)
	LockUnspent(unlock bool, ops []*wire.OutPoint) error
	GetRawChangeAddress(account string, net dcrutil.AddressParams) (dcrutil.Address, error)
	GetNewAddress(account string, net dcrutil.AddressParams) (dcrutil.Address, error)
	SignRawTransaction(tx *wire.MsgTx) (*wire.MsgTx, bool, error)
	DumpPrivKey(address dcrutil.Address, net [2]byte) (*dcrutil.WIF, error)
	GetTransaction(txHash *chainhash.Hash) (*walletjson.GetTransactionResult, error)
	WalletLock() error
	WalletPassphrase(passphrase string, timeoutSecs int64) error
	Disconnected() bool
}

// outpointID creates a unique string for a transaction output.
func outpointID(txHash *chainhash.Hash, vout uint32) string {
	return txHash.String() + ":" + strconv.Itoa(int(vout))
}

// output is information about a transaction output. output satisfies the
// asset.Coin interface.
type output struct {
	txHash chainhash.Hash
	vout   uint32
	value  uint64
	redeem dex.Bytes
	tree   int8
	node   rpcClient // for calculating confirmations.
}

// newOutput is the constructor for an output.
func newOutput(node rpcClient, txHash *chainhash.Hash, vout uint32, value uint64, tree int8, redeem dex.Bytes) *output {
	return &output{
		txHash: *txHash,
		vout:   vout,
		value:  value,
		redeem: redeem,
		tree:   tree,
		node:   node,
	}
}

// Value returns the value of the output. Part of the asset.Coin interface.
func (output *output) Value() uint64 {
	return output.value
}

// Confirmations is the number of confirmations on the output's block.
// Confirmations always pulls the block information fresh from the blockchain,
// and will return an error if the output has been spent. Part of the
// asset.Coin interface.
func (output *output) Confirmations() (uint32, error) {
	txOut, err := output.node.GetTxOut(&output.txHash, output.vout, true)
	if err != nil {
		return 0, fmt.Errorf("error finding unspent contract: %v", err)
	}
	if txOut == nil {
		return 0, fmt.Errorf("tx output not found")
	}
	return uint32(txOut.Confirmations), nil
}

// ID is the output's coin ID. Part of the asset.Coin interface. For DCR, the
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

// auditInfo is information about a swap contract on the blockchain, not
// necessarily created by this wallet, as would be returned from AuditContract.
// auditInfo satisfies the asset.AuditInfo interface.
type auditInfo struct {
	output     *output
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

// Setup creates the DCR exchange wallet. Start the wallet with its Run method.
func (d *Driver) Setup(cfg *asset.WalletConfig, logger dex.Logger, network dex.Network) (asset.Wallet, error) {
	return NewWallet(cfg, logger, network)
}

// Info returns basic information about the wallet and asset.
func (d *Driver) Info() *asset.WalletInfo {
	return walletInfo
}

func init() {
	asset.Register(BipID, &Driver{})
}

// ExchangeWallet is a wallet backend for Decred. The backend is how the DEX
// client app communicates with the Decred blockchain and wallet. ExchangeWallet
// satisfies the dex.Wallet interface.
type ExchangeWallet struct {
	client *rpcclient.Client
	node   rpcClient
	log    dex.Logger
	acct   string
	// The DEX specifies that change outputs from DEX-monitored transactions are
	// exempt from minimum confirmation limits, so we must track those as they are
	// created.
	changeMtx   sync.RWMutex
	tradeChange map[string]time.Time
	tipChange   func(error)
}

// Check that ExchangeWallet satisfies the Wallet interface.
var _ asset.Wallet = (*ExchangeWallet)(nil)

// NewWallet is the exported constructor by which the DEX will import the
// exchange wallet. The wallet will shut down when the provided context is
// canceled.
func NewWallet(cfg *asset.WalletConfig, logger dex.Logger, network dex.Network) (*ExchangeWallet, error) {
	// loadConfig will set fields if defaults are used and set the chainParams
	// package variable.
	walletCfg, err := loadConfig(cfg.INIPath, network)
	if err != nil {
		return nil, err
	}
	if cfg.Account == "" {
		cfg.Account = defaultAccountName
	}
	dcr := unconnectedWallet(cfg, logger)

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
func unconnectedWallet(cfg *asset.WalletConfig, logger dex.Logger) *ExchangeWallet {
	return &ExchangeWallet{
		log:         logger,
		acct:        cfg.Account,
		tradeChange: make(map[string]time.Time),
		tipChange:   cfg.TipChange,
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
func (d *ExchangeWallet) Info() *asset.WalletInfo {
	return walletInfo
}

// Connect connects the wallet to the RPC server. Satisfies the dex.Connector
// interface.
func (dcr *ExchangeWallet) Connect(ctx context.Context) (error, *sync.WaitGroup) {
	err := dcr.client.Connect(ctx, true)
	if err != nil {
		return fmt.Errorf("Decred Wallet connect error: %v", err), nil
	}
	// Check the min api versions.
	versions, err := dcr.client.Version()
	if err != nil {
		return fmt.Errorf("DCR ExchangeWallet version fetch error: %v", err), nil
	}
	err = checkVersionInfo(versions)
	if err != nil {
		return fmt.Errorf("DCR ExchangeWallet version check failed: %v", err), nil
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		dcr.monitorBlocks(ctx)
		dcr.shutdown()
	}()
	return nil, &wg
}

// Balance should return the total available funds in the wallet.
// Note that after calling Fund, the amount returned by Balance may change
// by more than the value funded. Because of the DEX confirmation requirements,
// a simple getbalance with a minconf argument is not sufficient to see all
// balance available for the exchange. Instead, all wallet UTXOs are scanned
// for those that match DEX requirements. Part of the asset.Wallet interface.
func (dcr *ExchangeWallet) Balance(confs uint32) (available, locked uint64, err error) {
	unspents, err := dcr.node.ListUnspentMin(0)
	if err != nil {
		return 0, 0, err
	}
	_, sum, unconf, err := dcr.spendableUTXOs(unspents, confs)
	return sum, unconf, err
}

// Fund selects coins for use in an order. The coins will be locked, and will
// not be returned in subsequent calls to Fund or calculated in calls to
// Available, unless they are unlocked with ReturnCoins.
func (dcr *ExchangeWallet) Fund(value uint64, nfo *dex.Asset) (asset.Coins, error) {
	if value == 0 {
		return nil, fmt.Errorf("cannot fund value = 0")
	}
	enough := func(sum uint64, size uint32, unspent *compositeUTXO) bool {
		return sum+toAtoms(unspent.rpc.Amount) >= dcr.reqFunds(value, size+unspent.input.Size(), nfo)
	}
	coins, _, _, err := dcr.fund(nfo.FundConf, enough)
	return coins, err
}

// fund finds coins for the specified value. A function is provided that can
// check whether adding the provided output would be enough to satisfy the
// needed value.
func (dcr *ExchangeWallet) fund(confs uint32,
	enough func(sum uint64, size uint32, unspent *compositeUTXO) bool) (asset.Coins, uint64, uint32, error) {

	unspents, err := dcr.node.ListUnspentMin(0)
	if err != nil {
		return nil, 0, 0, err
	}
	sort.Slice(unspents, func(i, j int) bool { return unspents[i].Amount < unspents[j].Amount })
	utxos, _, _, err := dcr.spendableUTXOs(unspents, confs)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("error parsing unspent outputs: %v", err)
	}
	var sum uint64
	var size uint32
	var coins asset.Coins
	var spents []*wire.OutPoint

	addUTXO := func(unspent *compositeUTXO) error {
		txHash, err := chainhash.NewHashFromStr(unspent.rpc.TxID)
		if err != nil {
			return fmt.Errorf("error decoding txid: %v", err)
		}
		v := toAtoms(unspent.rpc.Amount)
		redeemScript, err := hex.DecodeString(unspent.rpc.RedeemScript)
		if err != nil {
			return fmt.Errorf("error decoding redeem script for %s, script = %s: %v", unspent.rpc.TxID, unspent.rpc.RedeemScript, err)
		}
		op := newOutput(dcr.node, txHash, unspent.rpc.Vout, v, unspent.rpc.Tree, redeemScript)
		coins = append(coins, op)
		spents = append(spents, wire.NewOutPoint(txHash, op.vout, unspent.rpc.Tree))
		size += unspent.input.Size()
		sum += v
		return nil
	}

out:
	for {
		// On each loop, find the smallest UTXO that is enough. If no UTXO is large
		// enough, add the largest and continue.
		var txout *compositeUTXO
		for _, txout = range utxos {
			if enough(sum, size, txout) {
				err = addUTXO(txout)
				if err != nil {
					return nil, 0, 0, err
				}
				break out
			}
		}
		// Append the last output, which is the largest.
		err = addUTXO(txout)
		if err != nil {
			return nil, 0, 0, err
		}
		// Pop the utxo from the unspents
		utxos = utxos[:len(utxos)-1]
		// If there are none left, we don't have enough.
		if len(utxos) == 0 {
			return nil, 0, 0, fmt.Errorf("not enough to cover requested funds")
		}
	}

	err = dcr.node.LockUnspent(false, spents)
	if err != nil {
		return nil, 0, 0, err
	}
	return coins, sum, size, nil
}

// ReturnCoins unlocks coins. This would be necessary in the case of a
// canceled order.
func (dcr *ExchangeWallet) ReturnCoins(unspents asset.Coins) error {
	if len(unspents) == 0 {
		return fmt.Errorf("cannot return zero coins")
	}
	ops := make([]*wire.OutPoint, 0, len(unspents))

	for _, unspent := range unspents {
		op, err := dcr.convertCoin(unspent)
		if err != nil {
			return fmt.Errorf("error converting coin: %v", err)
		}
		ops = append(ops, wire.NewOutPoint(&op.txHash, op.vout, op.tree))
	}
	return dcr.node.LockUnspent(true, ops)
}

// Swap sends the swaps in a single transaction. The Receipts returned can be
// used to refund a failed transaction.
func (dcr *ExchangeWallet) Swap(swaps []*asset.Swap, nfo *dex.Asset) ([]asset.Receipt, error) {
	var contracts [][]byte
	var totalOut, totalIn uint64
	// Start with an empty MsgTx.
	baseTx := wire.NewMsgTx()
	// Add the funding utxos.
	for _, swap := range swaps {
		in, err := dcr.addInputCoins(baseTx, swap.Inputs)
		if err != nil {
			return nil, err
		}
		totalIn += in
	}
	// Add the contract outputs.
	for _, swap := range swaps {
		contract := swap.Contract
		totalOut += contract.Value
		// revokeAddr is the address that will receive the refund if the contract is
		// abandoned.
		revokeAddr, err := dcr.node.GetNewAddress(dcr.acct, chainParams)
		if err != nil {
			return nil, fmt.Errorf("error creating revocation address: %v", err)
		}
		// Create the contract, a P2SH redeem script.
		pkScript, err := dexdcr.MakeContract(contract.Address, revokeAddr.String(), contract.SecretHash, int64(contract.LockTime), chainParams)
		if err != nil {
			return nil, fmt.Errorf("unable to create pubkey script for address %s", contract.Address)
		}
		contracts = append(contracts, pkScript)
		// Make the P2SH address and pubkey script.
		scriptAddr, err := dcrutil.NewAddressScriptHash(pkScript, chainParams)
		if err != nil {
			return nil, fmt.Errorf("error encoding script address: %v", err)
		}
		p2shScript, err := txscript.PayToAddrScript(scriptAddr)
		if err != nil {
			return nil, fmt.Errorf("error creating P2SH script: %v", err)
		}
		// Add the transaction output.
		txOut := wire.NewTxOut(int64(contract.Value), p2shScript)
		baseTx.AddTxOut(txOut)
	}
	if totalIn < totalOut {
		return nil, fmt.Errorf("unfunded contract. %d < %d", totalIn, totalOut)
	}
	// Grab a change address.
	changeAddr, err := dcr.node.GetRawChangeAddress(dcr.acct, chainParams)
	if err != nil {
		return nil, fmt.Errorf("error creating change address: %v", err)
	}
	// Add change, sign, and send the transaction.
	msgTx, err := dcr.sendWithReturn(baseTx, changeAddr, totalIn, totalOut, nfo.FeeRate, nil)
	if err != nil {
		return nil, err
	}
	// Prepare the receipts.
	swapCount := len(swaps)
	if len(baseTx.TxOut) < swapCount {
		return nil, fmt.Errorf("fewer outputs than swaps. %d < %d", len(baseTx.TxOut), swapCount)
	}
	receipts := make([]asset.Receipt, 0, swapCount)
	txHash := msgTx.TxHash()
	for i, swap := range swaps {
		cinfo := swap.Contract
		receipts = append(receipts, &swapReceipt{
			output:     newOutput(dcr.node, &txHash, uint32(i), cinfo.Value, wire.TxTreeRegular, contracts[i]),
			expiration: time.Unix(int64(cinfo.LockTime), 0).UTC(),
		})
	}
	return receipts, nil
}

// Redeem sends the redemption transaction, which may contain more than one
// redemption.
func (dcr *ExchangeWallet) Redeem(redemptions []*asset.Redemption, nfo *dex.Asset) error {
	// Create a transaction that spends the referenced contract.
	msgTx := wire.NewMsgTx()
	var totalIn uint64
	var contracts [][]byte
	var addresses []dcrutil.Address
	for _, r := range redemptions {
		cinfo, ok := r.Spends.(*auditInfo)
		if !ok {
			return fmt.Errorf("Redemption contract info of wrong type")
		}
		// Extract the swap contract recipient and secret hash and check the secret
		// hash against the hash of the provided secret.
		_, receiver, _, secretHash, err := dexdcr.ExtractSwapDetails(cinfo.output.redeem, chainParams)
		if err != nil {
			return fmt.Errorf("error extracting swap addresses: %v", err)
		}
		checkSecretHash := sha256.Sum256(r.Secret)
		if !bytes.Equal(checkSecretHash[:], secretHash) {
			return fmt.Errorf("secret hash mismatch. %x != %x", checkSecretHash[:], secretHash)
		}
		addresses = append(addresses, receiver)
		contracts = append(contracts, cinfo.output.redeem)
		prevOut := wire.NewOutPoint(&cinfo.output.txHash, cinfo.output.vout, wire.TxTreeRegular)
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
	fee := nfo.FeeRate * uint64(size)
	if fee > totalIn {
		return fmt.Errorf("redeem tx not worth the fees")
	}
	// Send the funds back to the exchange wallet.
	redeemAddr, err := dcr.node.GetRawChangeAddress(dcr.acct, chainParams)
	if err != nil {
		return fmt.Errorf("error getting new address from the wallet: %v", err)
	}
	pkScript, err := txscript.PayToAddrScript(redeemAddr)
	if err != nil {
		return fmt.Errorf("error creating redemption script for address '%s': %v", redeemAddr, err)
	}
	txOut := wire.NewTxOut(int64(totalIn-fee), pkScript)
	// One last check for dust.
	if dexdcr.IsDust(txOut, nfo.FeeRate) {
		return fmt.Errorf("redeem output is dust")
	}
	msgTx.AddTxOut(txOut)
	// Sign the inputs.
	for i, r := range redemptions {
		contract := contracts[i]
		redeemSig, redeemPubKey, err := dcr.createSig(msgTx, i, contract, addresses[i])
		if err != nil {
			return err
		}
		redeemSigScript, err := dexdcr.RedeemP2SHContract(contract, redeemSig, redeemPubKey, r.Secret)
		if err != nil {
			return err
		}
		msgTx.TxIn[i].SignatureScript = redeemSigScript
	}
	// Send the transaction.
	checkHash := msgTx.TxHash()
	txHash, err := dcr.node.SendRawTransaction(msgTx, false)
	if err != nil {
		return err
	}
	if *txHash != checkHash {
		return fmt.Errorf("redemption sent, but received unexpected transaction ID back from RPC server. "+
			"expected %s, got %s", *txHash, checkHash)
	}
	// Log the change output.
	dcr.addChange(txHash, 0)
	return nil
}

const (
	txCatReceive  = "recv"
	txCatGenerate = "generate"
)

// SignMessage signs the message with the private key associated with the
// specified Coin. A slice of pubkeys required to spend the Coin and a
// signature for each pubkey are returned.
func (dcr *ExchangeWallet) SignMessage(coin asset.Coin, msg dex.Bytes) (pubkeys, sigs []dex.Bytes, err error) {
	output, err := dcr.convertCoin(coin)
	if err != nil {
		return nil, nil, fmt.Errorf("error converting coin: %v", err)
	}
	tx, err := dcr.node.GetTransaction(&output.txHash)
	if err != nil {
		return nil, nil, err
	}
	for _, txDetails := range tx.Details {
		if txDetails.Vout == output.vout &&
			(txDetails.Category == txCatReceive ||
				txDetails.Category == txCatGenerate) {

			addr, err := dcrutil.DecodeAddress(txDetails.Address, chainParams)
			if err != nil {
				return nil, nil, fmt.Errorf("error decoding address: %v", err)
			}

			priv, pub, err := dcr.getKeys(addr)
			if err != nil {
				return nil, nil, fmt.Errorf("error fetching keys: %v", err)
			}

			signature, err := priv.Sign(msg)
			if err != nil {
				return nil, nil, fmt.Errorf("signing error: %v", err)
			}
			pubkeys = append(pubkeys, pub.SerializeCompressed())
			sigs = append(sigs, signature.Serialize())
		}
	}
	if len(pubkeys) == 0 {
		return nil, nil, fmt.Errorf("no valid keys found.")
	}
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
	_, receiver, stamp, _, err := dexdcr.ExtractSwapDetails(contract, chainParams)
	if err != nil {
		return nil, fmt.Errorf("error extracting swap addresses: %v", err)
	}
	// Get the contracts P2SH address from the tx output's pubkey script.
	txOut, err := dcr.node.GetTxOut(txHash, vout, true)
	if err != nil {
		return nil, fmt.Errorf("error finding unspent contract: %v", err)
	}
	if txOut == nil {
		return nil, fmt.Errorf("nil gettxout result")
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
		recipient:  receiver,
		expiration: time.Unix(int64(stamp), 0).UTC(),
	}, nil
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
		contractHash, err := dexdcr.ExtractContractHash(rawTx.Vout[vout].ScriptPubKey.Hex, chainParams)
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
					contractHash, err = dexdcr.ExtractContractHash(vouts[vout].ScriptPubKey.Hex, chainParams)
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
func (dcr *ExchangeWallet) Refund(receipt asset.Receipt, nfo *dex.Asset) error {
	op := receipt.Coin()
	txHash, vout, err := decodeCoinID(op.ID())
	if err != nil {
		return err
	}
	// Grab the unspent output to make sure it's good and to get the value.
	utxo, err := dcr.node.GetTxOut(txHash, vout, true)
	if err != nil {
		return fmt.Errorf("error finding unspent contract: %v", err)
	}
	val := toAtoms(utxo.Value)
	// NOTE: The wallet does not store this contract, even though it was known
	// when the init transaction was created. The DEX should store this
	// information for persistence across sessions. Care has been taken to ensure
	// that any type satisfying asset.Coin can be passed to the Wallet's methods,
	// so the DEX can create it's own asset.Coin to issue a redeem or refund after
	// a restart, for example, but the (asset.Coin).Redeem script (the swap
	// contract itself, including the counter-party's pubkey) must be included.
	redeem := op.Redeem()
	sender, _, lockTime, _, err := dexdcr.ExtractSwapDetails(redeem, chainParams)
	if err != nil {
		return fmt.Errorf("error extracting swap addresses: %v", err)
	}
	// Create the transaction that spends the contract.
	msgTx := wire.NewMsgTx()
	msgTx.LockTime = uint32(lockTime)
	prevOut := wire.NewOutPoint(txHash, vout, wire.TxTreeRegular)
	txIn := wire.NewTxIn(prevOut, int64(op.Value()), []byte{})
	txIn.Sequence = wire.MaxTxInSequenceNum - 1
	msgTx.AddTxIn(txIn)
	// Calculate fees and add the change output.
	size := msgTx.SerializeSize() + dexdcr.RefundSigScriptSize + dexdcr.P2PKHOutputSize
	fee := nfo.FeeRate * uint64(size)
	if fee > val {
		return fmt.Errorf("refund tx not worth the fees")
	}

	refundAddr, err := dcr.node.GetRawChangeAddress(dcr.acct, chainParams)
	if err != nil {
		return fmt.Errorf("error getting new address from the wallet: %v", err)
	}
	pkScript, err := txscript.PayToAddrScript(refundAddr)
	if err != nil {
		return fmt.Errorf("error creating refund script for address '%s': %v", refundAddr, err)
	}
	txOut := wire.NewTxOut(int64(val-fee), pkScript)
	// One last check for dust.
	if dexdcr.IsDust(txOut, nfo.FeeRate) {
		return fmt.Errorf("refund output is dust")
	}
	msgTx.AddTxOut(txOut)
	// Sign it.
	refundSig, refundPubKey, err := dcr.createSig(msgTx, 0, redeem, sender)
	if err != nil {
		return err
	}
	redeemSigScript, err := dexdcr.RefundP2SHContract(redeem, refundSig, refundPubKey)
	if err != nil {
		return err
	}
	txIn.SignatureScript = redeemSigScript
	// Send it.
	checkHash := msgTx.TxHash()
	refundHash, err := dcr.node.SendRawTransaction(msgTx, false)
	if err != nil {
		return err
	}
	if *refundHash != checkHash {
		return fmt.Errorf("refund sent, but received unexpected transaction ID back from RPC server. "+
			"expected %s, got %s", *refundHash, checkHash)
	}
	return nil
}

// Address returns an address for the exchange wallet.
func (dcr *ExchangeWallet) Address() (string, error) {
	addr, err := dcr.node.GetNewAddress(dcr.acct, chainParams)
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
func (dcr *ExchangeWallet) PayFee(address string, regFee uint64, nfo *dex.Asset) (asset.Coin, error) {
	addr, err := dcrutil.DecodeAddress(address, chainParams)
	if err != nil {
		return nil, err
	}
	msgTx, sent, err := dcr.sendRegFee(addr, regFee, nfo)
	if err != nil {
		return nil, err
	}
	if sent != regFee {
		return nil, fmt.Errorf("transaction %s was sent, but the reported value sent was unexpcted. "+
			"expected %d, but %d was reported", msgTx.CachedTxHash(), regFee, sent)
	}
	return newOutput(dcr.node, msgTx.CachedTxHash(), 0, regFee, wire.TxTreeRegular, nil), nil
}

// Withdraw withdraws funds to the specified address. Fees are subtracted from
// the value.
func (dcr *ExchangeWallet) Withdraw(address string, value, feeRate uint64) (asset.Coin, error) {
	addr, err := dcrutil.DecodeAddress(address, chainParams)
	if err != nil {
		return nil, err
	}
	msgTx, net, err := dcr.sendMinusFees(addr, value, feeRate)
	if err != nil {
		return nil, err
	}
	return newOutput(dcr.node, msgTx.CachedTxHash(), 0, net, wire.TxTreeRegular, nil), nil
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
		return 0, err
	}
	return uint32(tx.Confirmations), nil
}

// addInputCoins adds inputs to the MsgTx to spend the specified outputs.
func (dcr *ExchangeWallet) addInputCoins(msgTx *wire.MsgTx, coins asset.Coins) (uint64, error) {
	var totalIn uint64
	for _, coin := range coins {
		output, err := dcr.convertCoin(coin)
		if err != nil {
			return 0, err
		}
		if output.value == 0 {
			return 0, fmt.Errorf("zero-valued output detected for %s:%d", output.txHash, output.vout)
		}
		totalIn += output.value
		prevOut := wire.NewOutPoint(&output.txHash, output.vout, output.tree)
		txIn := wire.NewTxIn(prevOut, int64(output.value), []byte{})
		msgTx.AddTxIn(txIn)
	}
	return totalIn, nil
}

// Shutdown down the rpcclient.Client.
func (dcr *ExchangeWallet) shutdown() {
	if dcr.client != nil {
		dcr.client.Shutdown()
		dcr.client.WaitForShutdown()
	}
}

// Combines the RPC type with the spending input information.
type compositeUTXO struct {
	rpc   walletjson.ListUnspentResult
	input *dexdcr.SpendInfo
}

// spendableUTXOs filters the RPC utxos for those that are spendable with
// regards to the DEX's configuration.
func (dcr *ExchangeWallet) spendableUTXOs(unspents []walletjson.ListUnspentResult, confs uint32) ([]*compositeUTXO, uint64, uint64, error) {
	var sum, unconf uint64
	utxos := make([]*compositeUTXO, 0, len(unspents))
	for _, txout := range unspents {
		txHash, err := chainhash.NewHashFromStr(txout.TxID)
		if err != nil {
			return nil, 0, 0, fmt.Errorf("error decoding txid from rpc server %s: %v", txout.TxID, err)
		}
		// Change from a DEX-monitored transaction is exempt from the fundconf requirement.
		if txout.Confirmations >= int64(confs) || dcr.isDEXChange(txHash, txout.Vout) {
			scriptPK, err := hex.DecodeString(txout.ScriptPubKey)
			if err != nil {
				return nil, 0, 0, fmt.Errorf("error decoding pubkey script for %s, script = %s: %v", txout.TxID, txout.ScriptPubKey, err)
			}
			redeemScript, err := hex.DecodeString(txout.RedeemScript)
			if err != nil {
				return nil, 0, 0, fmt.Errorf("error decoding redeem script for %s, script = %s: %v", txout.TxID, txout.RedeemScript, err)
			}

			nfo, err := dexdcr.InputInfo(scriptPK, redeemScript, chainParams)
			if err != nil {
				if errors.Is(err, dex.UnsupportedScriptError) {
					continue
				}
				return nil, 0, 0, fmt.Errorf("error reading asset info: %v", err)
			}
			utxos = append(utxos, &compositeUTXO{
				rpc:   txout,
				input: nfo,
			})
			sum += toAtoms(txout.Amount)
		} else {
			unconf += toAtoms(txout.Amount)
		}
	}
	return utxos, sum, unconf, nil
}

// addChange adds the output to the list of DEX-trade change outputs. These
// outputs are tracked because change outputs from DEX-monitored trades are
// exempt from the fundconf confirmations requirement.
func (dcr *ExchangeWallet) addChange(txHash *chainhash.Hash, vout uint32) {
	dcr.changeMtx.Lock()
	defer dcr.changeMtx.Unlock()
	dcr.tradeChange[outpointID(txHash, vout)] = time.Now()
}

// isDEXCange checks whether the specified output is a change output from a
// DEX trade.
func (dcr *ExchangeWallet) isDEXChange(txHash *chainhash.Hash, vout uint32) bool {
	dcr.changeMtx.RLock()
	defer dcr.changeMtx.RUnlock()
	_, found := dcr.tradeChange[outpointID(txHash, vout)]
	return found
}

// reqFunds calculates the total value needed to fund a swap of the specified
// value, using a given total funding utxo input size (bytes).
func (dcr *ExchangeWallet) reqFunds(val uint64, funding uint32, nfo *dex.Asset) uint64 {
	return calc.RequiredFunds(val, funding, nfo)
}

// convertCoin converts the asset.Coin to an output.
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
		return nil, fmt.Errorf("tx output not found")
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
	coins, _, _, err := dcr.fund(0, enough)
	if err != nil {
		return nil, 0, err
	}
	tx, sent, err := dcr.sendCoins(addr, coins, val, feeRate, true)
	if err != nil {
		return nil, 0, err
	}
	return tx, sent, nil
}

// sendRegFee sends the registration fee to the address. Transaction fees will
// be in addition to the registration fee and the output will be the zeroth
// output.
func (dcr *ExchangeWallet) sendRegFee(addr dcrutil.Address, regfee uint64, nfo *dex.Asset) (*wire.MsgTx, uint64, error) {
	coins, err := dcr.Fund(regfee, nfo)
	if err != nil {
		return nil, 0, err
	}
	return dcr.sendCoins(addr, coins, regfee, nfo.FeeRate, false)
}

// sendCoins sends the amount to the address as the zeroth output, spending the
// specified coins. If subtract is true, the transaction fees will be taken from
// the sent value, otherwise it will taken from the change output.
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
	tx, err := dcr.sendWithReturn(baseTx, changeAddr, totalIn, val, feeRate, subtractee)
	return tx, uint64(txOut.Value), err
}

// sendWithReturn sends the unsigned transaction with an added output (unless
// dust) for the change. If a subtractee output is specified, fees will be
// subtracted from that output, otherwise they will be subtracted from the
// change output.
func (dcr *ExchangeWallet) sendWithReturn(baseTx *wire.MsgTx,
	addr dcrutil.Address, totalIn, totalOut, feeRate uint64, subtractee *wire.TxOut) (*wire.MsgTx, error) {

	// Sign the transaction to get an initial size estimate and calculate whether
	// a change output would be dust.
	msgTx, signed, err := dcr.node.SignRawTransaction(baseTx)
	if err != nil {
		return nil, fmt.Errorf("signing error: %v", err)
	}
	if !signed {
		return nil, fmt.Errorf("incomplete raw tx signature")
	}
	size := msgTx.SerializeSize()
	minFee := feeRate * uint64(size)
	remaining := totalIn - totalOut
	if minFee > remaining {
		return nil, fmt.Errorf("not enough funds to cover minimum fee rate. %d < %d",
			totalIn, minFee+totalOut)
	}
	lastFee := remaining
	// Create the change output.
	changeScript, err := txscript.PayToAddrScript(addr)
	if err != nil {
		return nil, fmt.Errorf("error creating change script for address '%s': %v", addr, err)
	}
	changeIdx := len(baseTx.TxOut)
	changeOutput := wire.NewTxOut(int64(remaining), changeScript)
	// The reservoir indicates the amount available to draw upon for fees.
	reservoir := remaining
	// If no subtractee was provided, subtract fees from the change output. Its
	// value depends on whether a subtractee is provided or not.
	if subtractee == nil {
		subtractee = changeOutput
		changeOutput.Value -= int64(minFee)
	} else {
		reservoir = uint64(subtractee.Value)
	}
	isDust := dexdcr.IsDust(changeOutput, feeRate)
	sigCycles := 0
	if !isDust {
		// Add the change output.
		size += dexdcr.P2PKHOutputSize
		lastFee = feeRate * uint64(size)
		subtractee.Value = int64(reservoir - lastFee)
		baseTx.AddTxOut(changeOutput)
		// Find the best fee rate by closing in on it in a loop.
		tried := map[uint64]uint8{}
		for {
			sigCycles++
			// Each cycle, sign the transaction and see if there appears to be any
			// room to lower the total fees.
			msgTx, signed, err = dcr.node.SignRawTransaction(baseTx)
			if err != nil {
				return nil, fmt.Errorf("signing error: %v", err)
			}
			if !signed {
				return nil, fmt.Errorf("incomplete raw tx signature")
			}
			size = msgTx.SerializeSize()
			// minFee is the lowest acceptable fee for a transaction of this size.
			minFee := feeRate * uint64(size)
			if minFee > reservoir {
				// I can't imagine a scenario where this condition would be true, but
				// I'd hate to be wrong.
				dcr.log.Errorf("reached the impossible place. in = %d, out = %d, minFee = %d, lastFee = %d",
					totalIn, totalOut, minFee, lastFee)
				return nil, fmt.Errorf("change error")
			}
			// If 1) fee == minFee, nothing changed since the last cycle. And there is
			// likely no room for improvment. If 2) The fee required for a transaction
			// of this size is less than the currently signed transaction fees, but
			// we've already tried it, then it must have a larger serialize size, so
			// the current fee is as good as it gets.
			if lastFee == minFee || (minFee < lastFee && tried[minFee] == 1) {
				break
			}
			// The minimum fee for a transaction of this size is either higher or
			// lower than the fee in the currently signed transaction, and it hasn't
			// been tried yet, so try it now.
			tried[lastFee] = 1
			subtractee.Value = int64(reservoir - minFee)
			lastFee = minFee
			if dexdcr.IsDust(subtractee, feeRate) {
				// Another condition that should be impossible, but check anyway in case
				// the maximum fee was underestimated causing the first check to be
				// missed.
				dcr.log.Errorf("reached the impossible place. in = %d, out = %d, minFee = %d, lastFee = %d",
					totalIn, totalOut, minFee, lastFee)
				return nil, fmt.Errorf("dust error")
			}
			continue
		}
	}
	// A couple of additional checks on the fee rate.
	checkFee, checkRate := fees(msgTx)
	if checkFee != lastFee {
		return nil, fmt.Errorf("fee mismatch! %d != %d", checkFee, lastFee)
	}
	// If the integer truncated fee rate is not the same as the fee rate provided,
	// log a warning.
	if checkRate < float64(feeRate) {
		return nil, fmt.Errorf("final fee rate for %s, %f, is lower than expected, %d",
			msgTx.CachedTxHash(), checkRate, feeRate)
	}
	// This is a last ditch effort to catch ridiculously high fees. Right now,
	// it's just erroring for fees more than double the expected rate, which is
	// admittedly un-scientific. This should account for any signature length
	// related variation as well as a potential dust change output with no
	// subtractee specified, in which case the dust goes to the miner.
	if checkRate > float64(feeRate*uint64(size)*2) {
		return nil, fmt.Errorf("final fee rate for %s, %f, is seemingly outrageous, %d",
			msgTx.CachedTxHash(), checkRate, feeRate)
	}
	checkHash := msgTx.TxHash()
	dcr.log.Debugf("%d signature cycles to converge on fees for tx %s", sigCycles, checkHash)
	txHash, err := dcr.node.SendRawTransaction(msgTx, false)
	if err != nil {
		return nil, err
	}
	if *txHash != checkHash {
		return nil, fmt.Errorf("transaction sent, but received unexpected transaction ID back from RPC server. "+
			"expected %s, got %s", *txHash, checkHash)
	}
	if !isDust {
		dcr.addChange(txHash, uint32(changeIdx))
	}
	return msgTx, nil
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

func fees(tx *wire.MsgTx) (uint64, float64) {
	var in, out int64
	for _, txIn := range tx.TxIn {
		in += txIn.ValueIn
	}
	for _, txOut := range tx.TxOut {
		out += txOut.Value
	}
	fees := in - out
	return uint64(fees), float64(fees) / float64(tx.SerializeSize())
}
