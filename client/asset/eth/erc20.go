// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build lgpl
// +build lgpl

package eth

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	"github.com/ethereum/go-ethereum/common"
)

func init() {
	asset.RegisterToken(42000, BipID, &tokenDriver{assetID: 42000})
}

// tokenDriver implements asset.Driver.
type tokenDriver struct {
	assetID uint32
}

// Check that Driver implements Driver and TokenDriver.
var _ asset.Driver = (*tokenDriver)(nil)
var _ asset.TokenDriver = (*tokenDriver)(nil)

// Open opens the ETH exchange wallet. Start the wallet with its Run method.
func (d *tokenDriver) Open(cfg *asset.WalletConfig, logger dex.Logger, network dex.Network) (asset.Wallet, error) {
	return nil, errors.New("token wallets must be opened with OpenToken")
}

func (d *tokenDriver) OpenToken(baseWallet *asset.Wallet, cfg *asset.WalletConfig, log dex.Logger) (asset.Wallet, error) {
	ethWallet, isEthWallet := (*baseWallet).(*ExchangeWallet)
	if !isEthWallet {
		return nil, fmt.Errorf("OpenToken: expected ETH wallet but got %T", baseWallet)
	}
	return newTokenWallet(ethWallet, d.assetID, cfg, log)
}

// DecodeCoinID creates a human-readable representation of a coin ID for Ethereum.
func (d *tokenDriver) DecodeCoinID(coinID []byte) (string, error) {
	return "", asset.ErrNotImplemented
}

// Info returns basic information about the wallet and asset.
func (d *tokenDriver) Info() *asset.WalletInfo {
	return &asset.WalletInfo{
		Name: supportedTokens[d.assetID].name,
		AvailableWallets: []*asset.WalletDefinition{
			{
				Type:        walletTypeGeth,
				Tab:         "Internal",
				Description: "Use the built-in DEX wallet with snap sync",
				ConfigOpts:  []*asset.ConfigOption{},
				Seeded:      true,
			},
		},
		UnitInfo: supportedTokens[d.assetID].unitInfo,
	}
}

// Check that TokenWallet satisfies the asset.Wallet interface.
var _ asset.Wallet = (*TokenWallet)(nil)

type tokenDefinition struct {
	name      string
	addresses map[dex.Network]common.Address
	unitInfo  dex.UnitInfo
}

var supportedTokens = map[uint32]tokenDefinition{
	42000: {
		name: "TestToken",
		addresses: map[dex.Network]common.Address{
			dex.Simnet: common.HexToAddress("0x77ee4c5bf5ceee927815084afdcd2dd89750d827"),
		},
		unitInfo: dex.UnitInfo{
			AtomicUnit: "Atom",
			Conventional: dex.Denomination{
				Unit:             "TST",
				ConversionFactor: 1,
			},
		},
	},
}

// ExchangeWallet is a wallet backend for Ethereum. The backend is how the DEX
// client app communicates with the Ethereum blockchain and wallet. ExchangeWallet
// satisfies the dex.Wallet interface.
type TokenWallet struct {
	baseWallet   *ExchangeWallet
	log          dex.Logger
	tokenAddress common.Address
	tokenName    string
	tipChange    func(error)
}

func newTokenWallet(baseWallet *ExchangeWallet, assetID uint32, cfg *asset.WalletConfig, log dex.Logger) (*TokenWallet, error) {
	token, ok := supportedTokens[assetID]
	if !ok {
		return nil, fmt.Errorf("assetID %d is not a supported ETH token", assetID)
	}

	address, ok := token.addresses[baseWallet.net]
	if !ok {
		return nil, fmt.Errorf("assetID %d is not a supported on %s", assetID, baseWallet.net)
	}

	tokenWallet := &TokenWallet{
		baseWallet:   baseWallet,
		log:          log,
		tokenAddress: address,
		tokenName:    token.name,
		tipChange:    cfg.TipChange,
	}

	baseWallet.addTokenWallet(tokenWallet)

	return tokenWallet, nil
}

// Info returns basic information about the wallet and asset.
func (t *TokenWallet) Info() *asset.WalletInfo {
	return &asset.WalletInfo{Name: t.tokenName}
}

// Connect connects to the node RPC server. A dex.Connector.
func (token *TokenWallet) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	if !token.baseWallet.monitoringBlocks {
		return nil, fmt.Errorf("ETH wallet must be connected to connect token wallet")
	}
	return &token.baseWallet.wg, nil
}

// OwnsAddress indicates if an address belongs to the wallet. The address need
// not be a EIP55-compliant formatted address. It may or may not have a 0x
// prefix, and case is not important.
//
// In Ethereum, an address is an account.
func (token *TokenWallet) OwnsAddress(address string) (bool, error) {
	return token.baseWallet.OwnsAddress(address)
}

// Balance returns the total available funds in the account.
// TODO: implement immature and locked
func (token *TokenWallet) Balance() (*asset.Balance, error) {
	balance, err := token.baseWallet.node.tokenBalance(token.baseWallet.ctx, token.tokenAddress)
	if err != nil {
		return nil, err
	}

	return &asset.Balance{
		Available: balance.Uint64(),
		Immature:  0,
		Locked:    0,
	}, nil

}

// MaxOrder generates information about the maximum order size and associated
// fees that the wallet can support for the given DEX configuration. The fees are an
// estimate based on current network conditions, and will be <= the fees
// associated with nfo.MaxFeeRate. For quote assets, the caller will have to
// calculate lotSize based on a rate conversion from the base asset's lot size.
func (*TokenWallet) MaxOrder(lotSize uint64, feeSuggestion uint64, nfo *dex.Asset) (*asset.SwapEstimate, error) {
	return nil, asset.ErrNotImplemented
}

// PreSwap gets order estimates based on the available funds and the wallet
// configuration.
func (*TokenWallet) PreSwap(req *asset.PreSwapForm) (*asset.PreSwap, error) {
	return nil, asset.ErrNotImplemented
}

// PreRedeem generates an estimate of the range of redemption fees that could
// be assessed.
func (*TokenWallet) PreRedeem(req *asset.PreRedeemForm) (*asset.PreRedeem, error) {
	return nil, asset.ErrNotImplemented
}

// FundOrder selects coins for use in an order. The coins will be locked, and
// will not be returned in subsequent calls to FundOrder or calculated in calls
// to Available, unless they are unlocked with ReturnCoins.
// In UTXO based coins, the returned []dex.Bytes contains the redeem scripts for the
// selected coins, but since there are no redeem scripts in Ethereum, nil is returned.
// Equal number of coins and redeem scripts must be returned.
func (*TokenWallet) FundOrder(ord *asset.Order) (asset.Coins, []dex.Bytes, error) {
	return nil, nil, asset.ErrNotImplemented
}

// ReturnCoins unlocks coins. This would be necessary in the case of a
// canceled order.
func (*TokenWallet) ReturnCoins(coins asset.Coins) error {
	return asset.ErrNotImplemented

}

// FundingCoins gets funding coins for the coin IDs. The coins are locked. This
// method might be called to reinitialize an order from data stored externally.
func (*TokenWallet) FundingCoins(ids []dex.Bytes) (asset.Coins, error) {
	return nil, asset.ErrNotImplemented
}

// Swap sends the swaps in a single transaction. The fees used returned are the
// max fees that will possibly be used, since in ethereum with EIP-1559 we cannot
// know exactly how much fees will be used.
func (*TokenWallet) Swap(swaps *asset.Swaps) ([]asset.Receipt, asset.Coin, uint64, error) {
	return nil, nil, 0, asset.ErrNotImplemented
}

// Redeem sends the redemption transaction, which may contain more than one
// redemption. All redemptions must be for the same contract version because the
// current API requires a single transaction reported (asset.Coin output), but
// conceptually a batch of redeems could be processed for any number of
// different contract addresses with multiple transactions.
func (*TokenWallet) Redeem(form *asset.RedeemForm) ([]dex.Bytes, asset.Coin, uint64, error) {
	return nil, nil, 0, asset.ErrNotImplemented
}

// SignMessage signs the message with the private key associated with the
// specified funding Coin. Only a coin that came from the address this wallet
// is initialized with can be used to sign.
func (*TokenWallet) SignMessage(coin asset.Coin, msg dex.Bytes) (pubkeys, sigs []dex.Bytes, err error) {
	return nil, nil, asset.ErrNotImplemented
}

// AuditContract retrieves information about a swap contract on the
// blockchain. This would be used to verify the counter-party's contract
// during a swap. coinID is expected to be the transaction id, and must
// be the same as the hash of txData. contract is expected to be
// (contractVersion|secretHash) where the secretHash uniquely keys the swap.
func (*TokenWallet) AuditContract(coinID, contract, txData dex.Bytes, rebroadcast bool) (*asset.AuditInfo, error) {
	return nil, asset.ErrNotImplemented
}

// LocktimeExpired returns true if the specified contract's locktime has
// expired, making it possible to issue a Refund.
func (*TokenWallet) LocktimeExpired(contract dex.Bytes) (bool, time.Time, error) {
	return false, time.Time{}, asset.ErrNotImplemented
}

// FindRedemption checks the contract for a redemption. If the swap is initiated
// but un-redeemed and un-refunded, FindRedemption will block until a redemption
// is seen.
func (*TokenWallet) FindRedemption(ctx context.Context, _, contract dex.Bytes) (redemptionCoin, secret dex.Bytes, err error) {
	return nil, nil, asset.ErrNotImplemented
}

// Refund refunds a contract. This can only be used after the time lock has
// expired.
func (*TokenWallet) Refund(_, contract dex.Bytes, feeSuggestion uint64) (dex.Bytes, error) {
	return nil, asset.ErrNotImplemented
}

// Address returns an address for the exchange wallet.
func (token *TokenWallet) Address() (string, error) {
	return token.baseWallet.Address()
}

// Unlock unlocks the exchange wallet.
func (*TokenWallet) Unlock(pw []byte) error {
	return asset.ErrNotImplemented
}

// Lock locks the exchange wallet.
func (*TokenWallet) Lock() error {
	return asset.ErrNotImplemented
}

// Locked will be true if the wallet is currently locked.
func (token *TokenWallet) Locked() bool {
	return token.baseWallet.Locked()
}

// PayFee sends the dex registration fee. Transaction fees are in addition to
// the registration fee, and the fee rate is taken from the DEX configuration.
//
// NOTE: PayFee is not intended to be used with Ethereum at this time.
func (*TokenWallet) PayFee(address string, regFee, feeRateSuggestion uint64) (asset.Coin, error) {
	return nil, asset.ErrNotImplemented
}

// SwapConfirmations gets the number of confirmations and the spend status
// for the specified swap.
func (*TokenWallet) SwapConfirmations(ctx context.Context, _ dex.Bytes, contract dex.Bytes, _ time.Time) (confs uint32, spent bool, err error) {
	return 0, false, asset.ErrNotImplemented
}

// Withdraw withdraws funds to the specified address. Value is gwei.
func (*TokenWallet) Withdraw(addr string, value, _ uint64) (asset.Coin, error) {
	return nil, asset.ErrNotImplemented
}

// ValidateSecret checks that the secret satisfies the contract.
func (*TokenWallet) ValidateSecret(secret, secretHash []byte) bool {
	return false
}

// Confirmations gets the number of confirmations for the specified coin ID.
func (*TokenWallet) Confirmations(ctx context.Context, id dex.Bytes) (confs uint32, spent bool, err error) {
	return 0, false, asset.ErrNotImplemented
}

// SyncStatus is information about the blockchain sync status.
func (t *TokenWallet) SyncStatus() (bool, float32, error) {
	return t.baseWallet.SyncStatus()
}

// RegFeeConfirmations gets the number of confirmations for the specified
// transaction.
func (*TokenWallet) RegFeeConfirmations(ctx context.Context, coinID dex.Bytes) (confs uint32, err error) {
	return 0, asset.ErrNotImplemented
}
