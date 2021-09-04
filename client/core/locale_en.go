package core

import (
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

type translation struct {
	subject  string
	template string
}

// enUS is the American English translations.
var enUS = map[string]*translation{
	// [host]
	TopicAccountRegistered: {
		subject:  "Account registered",
		template: "You may now trade at %s",
	},
	// [confs, host]
	TopicFeePaymentInProgress: {
		subject:  "Fee payment in progress",
		template: "Waiting for %d confirmations before trading at %s",
	},
	// [confs, required confs]
	TopicRegUpdate: {
		subject:  "regupdate",
		template: "Fee payment confirmations %v/%v",
	},
	// [host, error]
	TopicFeePaymentError: {
		subject:  "Fee payment error",
		template: "Error encountered while paying fees to %s: %v",
	},
	// [host, error]
	TopicAccountUnlockError: {
		subject:  "Account unlock error",
		template: "error unlocking account for %s: %v",
	},
	// [host]
	TopicFeeCoinError: {
		subject:  "Fee coin error",
		template: "Empty fee coin for %s.",
	},
	// [host]
	TopicWalletConnectionWarning: {
		subject:  "Wallet connection warning",
		template: "Incomplete registration detected for %s, but failed to connect to the Decred wallet",
	},
	// [host, error]
	TopicWalletUnlockError: {
		subject:  "Wallet unlock error",
		template: "Connected to Decred wallet to complete registration at %s, but failed to unlock: %v",
	},
	// [ticker, error]
	TopicWithdrawError: {
		subject:  "Withdraw error",
		template: "Error encountered during %s withdraw: %v",
	},
	// [ticker, coin ID]
	TopicWithdrawSend: {
		subject:  "Withdraw sent",
		template: "Withdraw of %s has completed successfully. Coin ID = %s",
	},
	// [error]
	TopicOrderLoadFailure: {
		subject:  "Order load failure",
		template: "Some orders failed to load from the database: %v",
	},
	// [qty, ticker, token]
	TopicYoloPlaced: {
		subject:  "Market order placed",
		template: "selling %.8f %s at market rate (%s)",
	},
	// [sell string, qty, ticker, rate string, token]
	TopicOrderPlaced: {
		subject:  "Order placed",
		template: "%sing %.8f %s, rate = %s (%s)",
	},
	// [missing count, token, host]
	TopicMissingMatches: {
		subject:  "Missing matches",
		template: "%d matches for order %s were not reported by %q and are considered revoked",
	},
	// [token, error]
	TopicWalletMissing: {
		subject:  "Wallet missing",
		template: "Wallet retrieval error for active order %s: %v",
	},
	// [side, token, match status]
	TopicMatchErrorCoin: {
		subject:  "Match coin error",
		template: "Match %s for order %s is in state %s, but has no maker swap coin.",
	},
	// [side, token, match status]
	TopicMatchErrorContract: {
		subject:  "Match contract error",
		template: "Match %s for order %s is in state %s, but has no maker swap contract.",
	},
	// [ticker, contract, token, error]
	TopicMatchRecoveryError: {
		subject:  "Match recovery error",
		template: "Error auditing counter-party's swap contract (%s %v) during swap recovery on order %s: %v",
	},
	// [token]
	TopicOrderCoinError: {
		subject:  "Order coin error",
		template: "No funding coins recorded for active order %s",
	},
	// [token, ticker, error]
	TopicOrderCoinFetchError: {
		subject:  "Order coin fetch error",
		template: "Source coins retrieval error for order %s (%s): %v",
	},
	// [token, ticker, error]
	TopicMissedCancel: {
		subject:  "Missed cancel",
		template: "Cancel order did not match for order %s. This can happen if the cancel order is submitted in the same epoch as the trade or if the target order is fully executed before matching with the cancel order.",
	},
	// [capitalized sell string, base ticker, quote ticker, host, token]
	TopicOrderCanceled: {
		subject:  "Order canceled",
		template: "%s order on %s-%s at %s has been canceled (%s)",
	},
	// [capitalized sell string, base ticker, quote ticker, fill percent, token]
	TopicMatchesMade: {
		subject:  "Matches made",
		template: "%s order on %s-%s %.1f%% filled (%s)",
	},
	// [qty, ticker, token]
	TopicSwapSendError: {
		subject:  "Swap send error",
		template: "Error encountered sending a swap output(s) worth %.8f %s on order %s",
	},
	// [match, error]
	TopicInitError: {
		subject:  "Swap reporting error",
		template: "Error notifying DEX of swap for match %s: %v",
	},
	// [match, error]
	TopicReportRedeemError: {
		subject:  "Redeem reporting error",
		template: "Error notifying DEX of redemption for match %s: %v",
	},
	// [qty, ticker, token]
	TopicSwapsInitiated: {
		subject:  "Swaps initiated",
		template: "Sent swaps worth %.8f %s on order %s",
	},
	// [qty, ticker, token]
	TopicRedemptionError: {
		subject:  "Redemption error",
		template: "Error encountered sending redemptions worth %.8f %s on order %s",
	},
	// [qty, ticker, token]
	TopicMatchComplete: {
		subject:  "Match complete",
		template: "Redeemed %.8f %s on order %s",
	},
	// [qty, ticker, token]
	TopicRefundFailure: {
		subject:  "Refund Failure",
		template: "Refunded %.8f %s on order %s, with some errors",
	},
	// [qty, ticker, token]
	TopicMatchesRefunded: {
		subject:  "Matches Refunded",
		template: "Refunded %.8f %s on order %s",
	},
	// [match ID token]
	TopicMatchRevoked: {
		subject:  "Match revoked",
		template: "Match %s has been revoked",
	},
	// [token, market name, host]
	TopicOrderRevoked: {
		subject:  "Order revoked",
		template: "Order %s on market %s at %s has been revoked by the server",
	},
	// [token, market name, host]
	TopicOrderAutoRevoked: {
		subject:  "Order auto-revoked",
		template: "Order %s on market %s at %s revoked due to market suspension",
	},
	// [ticker, coin ID, match]
	TopicMatchRecovered: {
		subject:  "Match recovered",
		template: "Found maker's redemption (%s: %v) and validated secret for match %s",
	},
	// [token]
	TopicCancellingOrder: {
		subject:  "Cancelling order",
		template: "A cancel order has been submitted for order %s",
	},
	// [token, old status, new status]
	TopicOrderStatusUpdate: {
		subject:  "Order status update",
		template: "Status of order %v revised from %v to %v",
	},
	// [count, host, token]
	TopicMatchResolutionError: {
		subject:  "Match resolution error",
		template: "%d matches reported by %s were not found for %s.",
	},
	// [token]
	TopicFailedCancel: {
		subject:  "Failed cancel",
		template: "Cancel order for order %s stuck in Epoch status for 2 epochs and is now deleted.",
	},
	// [coin ID, ticker, match]
	TopicAuditTrouble: {
		subject:  "Audit trouble",
		template: "Still searching for counterparty's contract coin %v (%s) for match %s. Are your internet and wallet connections good?",
	},
	// [host, error]
	TopicDexAuthError: {
		subject:  "DEX auth error",
		template: "%s: %v",
	},
	// [count, host]
	TopicUnknownOrders: {
		subject:  "DEX reported unknown orders",
		template: "%d active orders reported by DEX %s were not found.",
	},
	// [count]
	TopicOrdersReconciled: {
		subject:  "Orders reconciled with DEX",
		template: "Statuses updated for %d orders.",
	},
	// [ticker, address]
	TopicWalletConfigurationUpdated: {
		subject:  "Wallet configuration updated",
		template: "Configuration for %s wallet has been updated. Deposit address = %s",
	},
	//  [ticker]
	TopicWalletPasswordUpdated: {
		subject:  "Wallet Password Updated",
		template: "Password for %s wallet has been updated.",
	},
	// [market name, host, time]
	TopicMarketSuspendScheduled: {
		subject:  "Market suspend scheduled",
		template: "Market %s at %s is now scheduled for suspension at %v",
	},
	// [market name, host]
	TopicMarketSuspended: {
		subject:  "Market suspended",
		template: "Trading for market %s at %s is now suspended.",
	},
	// [market name, host]
	TopicMarketSuspendedWithPurge: {
		subject:  "Market suspended, orders purged",
		template: "Trading for market %s at %s is now suspended. All booked orders are now PURGED.",
	},
	// [market name, host, time]
	TopicMarketResumeScheduled: {
		subject:  "Market resume scheduled",
		template: "Market %s at %s is now scheduled for resumption at %v",
	},
	// [market name, host, epoch]
	TopicMarketResumed: {
		subject:  "Market resumed",
		template: "Market %s at %s has resumed trading at epoch %d",
	},
	// [host]
	TopicUpgradeNeeded: {
		subject:  "Upgrade needed",
		template: "You may need to update your client to trade at %s.",
	},
	// [host]
	TopicDEXConnected: {
		subject:  "Server connected",
		template: "%s is connected",
	},
	// [host]
	TopicDEXDisconnected: {
		subject:  "Server disconnect",
		template: "%s is disconnected",
	},
	// [host, rule, time, details]
	TopicPenalized: {
		subject:  "Server has penalized you",
		template: "Penalty from DEX at %s\nlast broken rule: %s\ntime: %v\ndetails:\n\"%s\"\n",
	},
	TopicSeedNeedsSaving: {
		subject:  "Don't forget to back up your application seed",
		template: "A new application seed has been created. Make a back up now in the settings view.",
	},
	TopicUpgradedToSeed: {
		subject:  "Back up your new application seed",
		template: "The client has been upgraded to use an application seed. Back up the seed now in the settings view.",
	},
	TopicDEXNotification: {
		subject:  "Message from DEX",
		template: "%s: %s",
	},
}

var locales = map[string]map[string]*translation{
	language.AmericanEnglish.String(): enUS,
}

func init() {
	for lang, translations := range locales {
		for topic, translation := range translations {
			message.SetString(language.Make(lang), topic, translation.template)
		}
	}
}
