// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package zec

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/asset/btc"
	"decred.org/dcrdex/dex"
	dexbtc "decred.org/dcrdex/dex/networks/btc"
	dexzec "decred.org/dcrdex/dex/networks/zec"
	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/ecdsa"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
)

const (
	version = 0
	BipID   = 133
	// The default fee is passed to the user as part of the asset.WalletInfo
	// structure.
	defaultFee          = 10
	defaultFeeRateLimit = 1000
	minNetworkVersion   = 5040250 // v5.4.2
	walletTypeRPC       = "zcashdRPC"

	transparentAcctNumber = 0
	shieldedAcctNumber    = 1

	transparentAddressType = "p2pkh"
	orchardAddressType     = "orchard"
	saplingAddressType     = "sapling"
	unifiedAddressType     = "unified"
)

var (
	configOpts = []*asset.ConfigOption{
		{
			Key:         "rpcuser",
			DisplayName: "JSON-RPC Username",
			Description: "Zcash's 'rpcuser' setting",
		},
		{
			Key:         "rpcpassword",
			DisplayName: "JSON-RPC Password",
			Description: "Zcash's 'rpcpassword' setting",
			NoEcho:      true,
		},
		{
			Key:         "rpcbind",
			DisplayName: "JSON-RPC Address",
			Description: "<addr> or <addr>:<port> (default 'localhost')",
		},
		{
			Key:         "rpcport",
			DisplayName: "JSON-RPC Port",
			Description: "Port for RPC connections (if not set in Address)",
		},
		{
			Key:          "fallbackfee",
			DisplayName:  "Fallback fee rate",
			Description:  "Zcash's 'fallbackfee' rate. Units: ZEC/kB",
			DefaultValue: defaultFee * 1000 / 1e8,
		},
		{
			Key:         "feeratelimit",
			DisplayName: "Highest acceptable fee rate",
			Description: "This is the highest network fee rate you are willing to " +
				"pay on swap transactions. If feeratelimit is lower than a market's " +
				"maxfeerate, you will not be able to trade on that market with this " +
				"wallet.  Units: BTC/kB",
			DefaultValue: defaultFeeRateLimit * 1000 / 1e8,
		},
		{
			Key:         "txsplit",
			DisplayName: "Pre-split funding inputs",
			Description: "When placing an order, create a \"split\" transaction to fund the order without locking more of the wallet balance than " +
				"necessary. Otherwise, excess funds may be reserved to fund the order until the first swap contract is broadcast " +
				"during match settlement, or the order is canceled. This an extra transaction for which network mining fees are paid. " +
				"Used only for standing-type orders, e.g. limit orders without immediate time-in-force.",
			IsBoolean: true,
		},
	}
	// WalletInfo defines some general information about a Zcash wallet.
	WalletInfo = &asset.WalletInfo{
		Name:              "Zcash",
		Version:           version,
		SupportedVersions: []uint32{version},
		UnitInfo:          dexzec.UnitInfo,
		AvailableWallets: []*asset.WalletDefinition{{
			Type:              walletTypeRPC,
			Tab:               "External",
			Description:       "Connect to zcashcoind",
			DefaultConfigPath: dexbtc.SystemConfigPath("zcash"),
			ConfigOpts:        configOpts,
			NoAuth:            true,
		}},
	}
)

func init() {
	asset.Register(BipID, &Driver{})
}

// Driver implements asset.Driver.
type Driver struct{}

// Open creates the ZEC exchange wallet. Start the wallet with its Run method.
func (d *Driver) Open(cfg *asset.WalletConfig, logger dex.Logger, network dex.Network) (asset.Wallet, error) {
	return NewWallet(cfg, logger, network)
}

// DecodeCoinID creates a human-readable representation of a coin ID for
// Zcash.
func (d *Driver) DecodeCoinID(coinID []byte) (string, error) {
	// Zcash shielded transactions don't have transparent outputs, so the coinID
	// will just be the tx hash.
	if len(coinID) == chainhash.HashSize {
		var txHash chainhash.Hash
		copy(txHash[:], coinID)
		return txHash.String(), nil
	}
	// For transparent transactions, Zcash and Bitcoin have the same tx hash
	// and output format.
	return (&btc.Driver{}).DecodeCoinID(coinID)
}

// Info returns basic information about the wallet and asset.
func (d *Driver) Info() *asset.WalletInfo {
	return WalletInfo
}

// NewWallet is the exported constructor by which the DEX will import the
// exchange wallet. The wallet will shut down when the provided context is
// canceled. The configPath can be an empty string, in which case the standard
// system location of the zcashd config file is assumed.
func NewWallet(cfg *asset.WalletConfig, logger dex.Logger, net dex.Network) (asset.Wallet, error) {
	var btcParams *chaincfg.Params
	var addrParams *dexzec.AddressParams
	switch net {
	case dex.Mainnet:
		btcParams = dexzec.MainNetParams
		addrParams = dexzec.MainNetAddressParams
	case dex.Testnet:
		btcParams = dexzec.TestNet4Params
		addrParams = dexzec.TestNet4AddressParams
	case dex.Regtest:
		btcParams = dexzec.RegressionNetParams
		addrParams = dexzec.RegressionNetAddressParams
	default:
		return nil, fmt.Errorf("unknown network ID %v", net)
	}

	// Designate the clone ports. These will be overwritten by any explicit
	// settings in the configuration file.
	ports := dexbtc.NetPorts{
		Mainnet: "8232",
		Testnet: "18232",
		Simnet:  "18232",
	}

	var w *btc.ExchangeWalletNoAuth
	cloneCFG := &btc.BTCCloneCFG{
		WalletCFG:                cfg,
		MinNetworkVersion:        minNetworkVersion,
		WalletInfo:               WalletInfo,
		Symbol:                   "zec",
		Logger:                   logger,
		Network:                  net,
		ChainParams:              btcParams,
		Ports:                    ports,
		DefaultFallbackFee:       defaultFee,
		DefaultFeeRateLimit:      defaultFeeRateLimit,
		LegacyRawFeeLimit:        true,
		ZECStyleBalance:          true,
		Segwit:                   false,
		InitTxSize:               dexzec.InitTxSize,
		InitTxSizeBase:           dexzec.InitTxSizeBase,
		OmitAddressType:          true,
		LegacySignTxRPC:          true,
		NumericGetRawRPC:         true,
		LegacyValidateAddressRPC: true,
		SingularWallet:           true,
		UnlockSpends:             true,
		FeeEstimator:             estimateFee,
		ConnectFunc: func() error {
			return connect(w)
		},
		AddrFunc: func() (btcutil.Address, error) {
			return transparentAddress(w, addrParams, btcParams)
		},
		AddressDecoder: func(addr string, net *chaincfg.Params) (btcutil.Address, error) {
			return dexzec.DecodeAddress(addr, addrParams, btcParams)
		},
		AddressStringer: func(addr btcutil.Address, btcParams *chaincfg.Params) (string, error) {
			return dexzec.EncodeAddress(addr, addrParams)
		},
		TxSizeCalculator: dexzec.CalcTxSize,
		NonSegwitSigner:  signTx,
		TxDeserializer: func(b []byte) (*wire.MsgTx, error) {
			zecTx, err := dexzec.DeserializeTx(b)
			if err != nil {
				return nil, err
			}
			return zecTx.MsgTx, nil
		},
		BlockDeserializer: func(b []byte) (*wire.MsgBlock, error) {
			zecBlock, err := dexzec.DeserializeBlock(b)
			if err != nil {
				return nil, err
			}
			return &zecBlock.MsgBlock, nil
		},
		TxSerializer: func(btcTx *wire.MsgTx) ([]byte, error) {
			return zecTx(btcTx).Bytes()
		},
		TxHasher: func(tx *wire.MsgTx) *chainhash.Hash {
			h := zecTx(tx).TxHash()
			return &h
		},
		TxVersion: func() int32 {
			return dexzec.VersionNU5
		},
		// https://github.com/zcash/zcash/pull/6005
		ManualMedianTime:  true,
		OmitRPCOptionsArg: true,
		AssetID:           BipID,
	}

	var err error
	w, err = btc.BTCCloneWalletNoAuth(cloneCFG)
	if err != nil {
		return nil, err
	}
	return &zecWallet{ExchangeWalletNoAuth: w, log: logger}, nil
}

type rpcCaller interface {
	CallRPC(method string, args []any, thing any) error
}

type zecWallet struct {
	*btc.ExchangeWalletNoAuth
	log         dex.Logger
	lastAddress atomic.Value // "string"
}

var _ asset.FeeRater = (*zecWallet)(nil)

// FeeRate returns the asset standard fee rate for Zcash.
func (w *zecWallet) FeeRate() uint64 {
	return dexzec.LegacyFeeRate
}

var _ asset.ShieldedWallet = (*zecWallet)(nil)

func transparentAddress(c rpcCaller, addrParams *dexzec.AddressParams, btcParams *chaincfg.Params) (btcutil.Address, error) {
	const zerothAccount = 0
	// One of the address types MUST be shielded.
	addrRes, err := zGetAddressForAccount(c, zerothAccount, []string{transparentAddressType, orchardAddressType})
	if err != nil {
		return nil, err
	}
	receivers, err := zGetUnifiedReceivers(c, addrRes.Address)
	if err != nil {
		return nil, err
	}
	return dexzec.DecodeAddress(receivers.Transparent, addrParams, btcParams)
}

// connect is Zcash's BTCCloneCFG.ConnectFunc. Ensures that accounts are set
// up correctly.
func connect(c rpcCaller) error {
	// Make sure we have zeroth and first account or are able to create them.
	accts, err := zListAccounts(c)
	if err != nil {
		return fmt.Errorf("error listing Zcash accounts: %w", err)
	}

	createAccount := func(n uint32) error {
		for _, acct := range accts {
			if acct.Number == n {
				return nil
			}
		}
		acctNumber, err := zGetNewAccount(c)
		if err != nil {
			if strings.Contains(err.Error(), "zcashd-wallet-tool") {
				return fmt.Errorf("account %d does not exist and cannot be created because wallet seed backup has not been acknowledged with the zcashd-wallet-tool utility", n)
			}
			return fmt.Errorf("error creating account %d: %w", n, err)
		}
		if acctNumber != n {
			return fmt.Errorf("no account %d found and newly created account has unexpected account number %d", n, acctNumber)
		}
		return nil
	}
	if err := createAccount(0); err != nil {
		return err
	}
	if err := createAccount(1); err != nil {
		return err
	}
	return nil
}

func (w *zecWallet) lastShieldedAddress() (addr string, err error) {
	if addrPtr := w.lastAddress.Load(); addrPtr != nil {
		return addrPtr.(string), nil
	}
	accts, err := zListAccounts(w)
	if err != nil {
		return "", err
	}
	for _, acct := range accts {
		if acct.Number != shieldedAcctNumber {
			continue
		}
		if len(acct.Addresses) == 0 {
			break // generate first address
		}
		lastAddr := acct.Addresses[len(acct.Addresses)-1].UnifiedAddr
		w.lastAddress.Store(lastAddr)
		return lastAddr, nil // Orchard = Unified for account 1
	}
	return w.NewShieldedAddress()
}

// ShieldedBalance list the last address and the balance in the shielded
// account.
func (w *zecWallet) ShieldedStatus() (status *asset.ShieldedStatus, err error) {
	// z_listaccounts to get account 1 addresses
	// DRAFT NOTE: It sucks that we need to list all accounts here. The zeroth
	// account is our transparent addresses, and they'll all be included in the
	// result. Should probably open a PR at zcash/zcash to add the ability to
	// list a single account.
	status = new(asset.ShieldedStatus)
	status.LastAddress, err = w.lastShieldedAddress()
	if err != nil {
		return nil, err
	}

	status.Balance, err = zGetBalanceForAccount(w, shieldedAcctNumber)
	if err != nil {
		return nil, err
	}

	return status, nil
}

// Balance adds a sum of shielded pool balances to the transparent balance info.
//
// Since v5.4.0, the getnewaddress RPC is deprecated if favor of using unified
// addresses from accounts generated with z_getnewaccount. Addresses are
// generated from account 0. Any addresses previously generated using
// getnewaddress belong to a legacy account that is not listed with
// z_listaccount, nor addressable with e.g. z_getbalanceforaccount. For
// transparent addresses, we still use the getbalance RPC, which combines
// transparent balance from both legacy and generated accounts. This matches the
// behavior of the listunspent RPC, and conveniently makes upgrading simple. So
// even though we ONLY use account 0 to generate t-addresses, any account's
// transparent outputs are eligible for trading. To minimize confusion, we don't
// add transparent receivers to addresses generated from the shielded account.
// This doesn't preclude a user doing something silly with zcash-cli.
func (w *zecWallet) Balance() (*asset.Balance, error) {
	bal, err := w.ExchangeWalletNoAuth.Balance()
	if err != nil {
		return nil, err
	}

	shielded, err := zGetBalanceForAccount(w, shieldedAcctNumber)
	if err != nil {
		return nil, err
	}

	if bal.Other == nil {
		bal.Other = make(map[asset.BalanceCategory]asset.CustomBalance)
	}

	bal.Other[asset.BalanceCategoryShielded] = asset.CustomBalance{
		Amount: shielded,
	}
	return bal, nil
}

// NewShieldedAddress creates a new shielded address. A shielded address can be
// be reused without sacrifice of privacy on-chain, but that doesn't stop
// meat-space coordination to reduce privacy.
func (w *zecWallet) NewShieldedAddress() (string, error) {
	// An orchard address is the same as a unified address with only an orchard
	// receiver.
	addrRes, err := zGetAddressForAccount(w, shieldedAcctNumber, []string{orchardAddressType})
	if err != nil {
		return "", err
	}
	w.lastAddress.Store(addrRes.Address)
	return addrRes.Address, nil
}

// ShieldFunds moves funds from the transparent account to the shielded account.
func (w *zecWallet) ShieldFunds(ctx context.Context, transparentVal uint64) ([]byte, error) {
	bal, err := w.Balance()
	if err != nil {
		return nil, err
	}
	const fees = 1000 // TODO: Update after v5.5.0 which includes ZIP137
	if bal.Available < fees || bal.Available-fees < transparentVal {
		return nil, asset.ErrInsufficientBalance
	}
	oneAddr, err := w.lastShieldedAddress()
	if err != nil {
		return nil, err
	}
	// DRAFT TODO: Using ANY_TADDR can fail if some of the balance is in
	// coinbase txs, which have special handling requirements. The user would
	// need to either 1) Send all coinbase outputs to a transparent address, or
	// 2) use z_shieldcoinbase from their zcash-cli interface.
	const anyTAddr = "ANY_TADDR"
	return w.sendOne(ctx, anyTAddr, oneAddr, transparentVal, AllowRevealedSenders)
}

// UnshieldFunds moves funds from the shielded account to the transparent
// account.
func (w *zecWallet) UnshieldFunds(ctx context.Context, amt uint64) ([]byte, error) {
	bal, err := zGetBalanceForAccount(w, shieldedAcctNumber)
	if err != nil {
		return nil, fmt.Errorf("z_getbalance error: %w", err)
	}
	const fees = 1000 // TODO: Update after v5.5.0 which includes ZIP137
	if bal < fees || bal-fees < amt {
		return nil, asset.ErrInsufficientBalance
	}

	unified, err := zGetAddressForAccount(w, transparentAcctNumber, []string{transparentAddressType, orchardAddressType})
	if err != nil {
		return nil, fmt.Errorf("z_getaddressforaccount error: %w", err)
	}

	receivers, err := zGetUnifiedReceivers(w, unified.Address)
	if err != nil {
		return nil, fmt.Errorf("z_getunifiedreceivers error: %w", err)
	}

	return w.sendOneShielded(ctx, receivers.Transparent, amt, AllowRevealedRecipients)
}

// sendOne is a helper function for doing a z_sendmany with a single recipient.
func (w *zecWallet) sendOne(ctx context.Context, fromAddr, toAddr string, amt uint64, priv privacyPolicy) ([]byte, error) {
	recip := singleSendManyRecipient(toAddr, amt)

	operationID, err := zSendMany(w, fromAddr, recip, priv)
	if err != nil {
		return nil, fmt.Errorf("z_sendmany error: %w", err)
	}

	txHash, err := w.awaitSendManyOperation(ctx, w, operationID)
	if err != nil {
		return nil, err
	}

	return txHash[:], nil
}

// awaitSendManyOperation waits for the asynchronous result from a z_sendmany
// operation.
func (w *zecWallet) awaitSendManyOperation(ctx context.Context, c rpcCaller, operationID string) (*chainhash.Hash, error) {
	for {
		res, err := zGetOperationResult(c, operationID)
		if err != nil && !errors.Is(err, ErrEmptyOpResults) {
			return nil, fmt.Errorf("error getting operation result: %w", err)
		}
		if res != nil {
			switch res.Status {
			case "failed":
				return nil, fmt.Errorf("z_sendmany operation failed: %s", res.Error.Message)

			case "success":
				txHash, err := chainhash.NewHashFromStr(res.Result.TxID)
				if err != nil {
					return nil, fmt.Errorf("error decoding txid: %w", err)
				}
				return txHash, nil
			default:
				w.log.Warnf("unexpected z_getoperationresult status %q: %+v", res.Status)
			}

		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

func (w *zecWallet) sendOneShielded(ctx context.Context, toAddr string, amt uint64, priv privacyPolicy) ([]byte, error) {
	lastAddr, err := w.lastShieldedAddress()
	if err != nil {
		return nil, err
	}
	return w.sendOne(ctx, lastAddr, toAddr, amt, priv)
}

// SendShielded sends funds from the shielded account to the provided shielded
// or transparent address.
func (w *zecWallet) SendShielded(ctx context.Context, toAddr string, amt uint64) ([]byte, error) {
	bal, err := zGetBalanceForAccount(w, shieldedAcctNumber)
	if err != nil {
		return nil, err
	}
	const fees = 1000 // TODO: Update after v5.5.0 which includes ZIP137
	if bal < fees || bal-fees < amt {
		return nil, asset.ErrInsufficientBalance
	}

	res, err := zValidateAddress(w, toAddr)
	if err != nil {
		return nil, fmt.Errorf("error validating address: %w", err)
	}

	favoredReceiverAndPolicy := func(r *unifiedReceivers) (string, privacyPolicy, error) {
		switch {
		case r.Orchard != "":
			return r.Orchard, FullPrivacy, nil
		case r.Sapling != "":
			return r.Sapling, AllowRevealedAmounts, nil
		case r.Transparent != "":
			return r.Transparent, AllowRevealedRecipients, nil
		default:
			return "", "", fmt.Errorf("no known receiver types")
		}
	}

	var priv privacyPolicy
	switch res.AddressType {
	case unifiedAddressType:
		// Could be orchard, or other unified..
		receivers, err := zGetUnifiedReceivers(w, toAddr)
		if err != nil {
			return nil, fmt.Errorf("error getting unified receivers: %w", err)
		}
		toAddr, priv, err = favoredReceiverAndPolicy(receivers)
		if err != nil {
			return nil, fmt.Errorf("error parsing unified receiver: %w", err)
		}
	case transparentAddressType:
		priv = AllowRevealedRecipients
	case saplingAddressType:
		priv = AllowRevealedAmounts
	default:
		return nil, fmt.Errorf("unknown address type: %q", res.AddressType)
	}

	return w.sendOneShielded(ctx, toAddr, amt, priv)
}

func zecTx(tx *wire.MsgTx) *dexzec.Tx {
	return dexzec.NewTxFromMsgTx(tx, dexzec.MaxExpiryHeight)
}

// estimateFee returns the asset standard legacy fee rate.
func estimateFee(context.Context, btc.RawRequester, uint64) (uint64, error) {
	return dexzec.LegacyFeeRate, nil
}

// signTx signs the transaction input with Zcash's BLAKE-2B sighash digest.
// Won't work with shielded or blended transactions.
func signTx(btcTx *wire.MsgTx, idx int, pkScript []byte, hashType txscript.SigHashType,
	key *btcec.PrivateKey, amts []int64, prevScripts [][]byte) ([]byte, error) {

	tx := zecTx(btcTx)

	sigHash, err := tx.SignatureDigest(idx, hashType, amts, prevScripts)
	if err != nil {
		return nil, fmt.Errorf("sighash calculation error: %v", err)
	}

	return append(ecdsa.Sign(key, sigHash[:]).Serialize(), byte(hashType)), nil
}
