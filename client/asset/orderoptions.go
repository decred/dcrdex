package asset

import (
	"fmt"
	"reflect"

	"decred.org/dcrdex/dex/config"
)

// SwapOptions captures the available Swap options. Tagged to be used with
// config.Unmapify to decode.
type SwapOptions struct {
	MinFeeRate *uint64 `ini:"minswapfeerate"`
}

// RedeemOptions captures the available Swap options. Tagged to be used with
// config.Unmapify to decode.
type RedeemOptions struct {
}

// OrderOptions captures the available swap and redeem options.
type OrderOptions struct {
	Swap   SwapOptions
	Redeem RedeemOptions
}

func supportedOptionsImpl(wallet Wallet, swap bool) (map[string]interface{}, error) {
	supportedOptions := make(map[string]interface{})

	var options interface{}
	if swap {
		options = wallet.SupportedSwapOptions()
	} else {
		options = wallet.SupportedRedeemOptions()
	}
	optionsValue := reflect.ValueOf(options)
	optionsType := reflect.TypeOf(options)

	for i := 0; i < optionsValue.NumField(); i++ {
		value := optionsValue.Field(i)
		if !value.IsNil() {
			typ := optionsType.Field(i)
			iniTag := typ.Tag.Get("ini")
			supportedOptions[iniTag] = true
		}
	}

	return supportedOptions, nil
}

// SupportedOrderOptions returns the order options supported by the swapAssetWallet and the redeemAssetWallet.
func SupportedOrderOptions(swapAssetWallet Wallet, redeemAssetWallet Wallet) (map[string]interface{}, error) {
	supportedSwapOptions, err := supportedOptionsImpl(swapAssetWallet, true)
	if err != nil {
		return nil, err
	}

	supportedRedeemOptions, err := supportedOptionsImpl(redeemAssetWallet, false)
	if err != nil {
		return nil, err
	}

	supportedOptions := supportedSwapOptions
	for option, _ := range supportedRedeemOptions {
		supportedOptions[option] = true
	}

	return supportedOptions, nil
}

// ParseSupportedOrderOptions parses a string map into an OrderOptions struct, but only includes the
// options that are supported by the swapAssetWallet and redeemAssetWallet.
func ParseSupportedOrderOptions(options map[string]string, swapAssetWallet Wallet, redeemAssetWallet Wallet) (*OrderOptions, error) {
	supportedOptions, err := SupportedOrderOptions(swapAssetWallet, redeemAssetWallet)
	if err != nil {
		return nil, err
	}

	for option := range options {
		if _, ok := supportedOptions[option]; !ok {
			delete(options, option)
		}
	}

	swapOptions := new(SwapOptions)
	config.Unmapify(options, swapOptions)

	redeemOptions := new(RedeemOptions)
	config.Unmapify(options, redeemOptions)

	return &OrderOptions{
		Swap:   *swapOptions,
		Redeem: *redeemOptions,
	}, nil
}

// ParseAllOrderOptions parses a string map into an OrderOptions struct.
func ParseAllOrderOptions(options map[string]string) *OrderOptions {
	swapOptions := new(SwapOptions)
	config.Unmapify(options, swapOptions)

	redeemOptions := new(RedeemOptions)
	config.Unmapify(options, redeemOptions)

	return &OrderOptions{
		Swap:   *swapOptions,
		Redeem: *redeemOptions,
	}
}

// MapifyOrderOptions converts an OrderOptions string map.
func MapifyOrderOptions(options *OrderOptions) (map[string]string, error) {
	swapOptions, err := config.Mapify(&options.Swap)
	if err != nil {
		return nil, fmt.Errorf("error parsing swapOptions: %w", err)
	}
	redeemOptions, err := config.Mapify(&options.Redeem)
	if err != nil {
		return nil, fmt.Errorf("error parsing redeemOptions")
	}

	mergedOptions := swapOptions
	for option := range redeemOptions {
		mergedOptions[option] = redeemOptions[option]
	}

	return mergedOptions, nil
}
