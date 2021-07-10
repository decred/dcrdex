package core

import (
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

type Translation struct {
	Subject  string
	Template string
}

var TemplateKeys = map[string]string{
	// [host]
	SubjectAccountRegistered: "You may now trade at %s",
	// [confs, host]
	SubjectFeePaymentInProgress: "Waiting for %d confirmations before trading at %s",
	// [confs, required confs]
	SubjectRegUpdate: "Fee payment confirmations %v/%v",
	// [host, error]
	SubjectFeePaymentError: "Error encountered while paying fees to %s: %v",
	// [host, error]
	SubjectAccountUnlockError: "error unlocking account for %s: %v",
	// [host]
	SubjectFeeCoinError: "Empty fee coin for %s.",
	// [host]
	SubjectWalletConnectionWarning: "Incomplete registration detected for %s, but failed to connect to the Decred wallet",
	// [host, error]
	SubjectWalletUnlockError: "Connected to Decred wallet to complete registration at %s, but failed to unlock: %v",
	// [ticker, error]
	SubjectWithdrawError: "Error encountered during %s withdraw: %v",
	// [ticker, coin ID]
	SubjectWithdrawSend: "Withdraw of %s has completed successfully. Coin ID = %s",
	// [error]
	SubjectOrderLoadFailure: "Some orders failed to load from the database: %v",
	// [qty, ticker, token]
	SubjectYoloPlaced: "selling %.8f %s at market rate (%s)",
	// [sell string, qty, ticker, rate string, token]
	SubjectOrderPlaced: "%sing %.8f %s, rate = %s (%s)",
	// [missing count, token, host]
	SubjectMissingMatches: "%d matches for order %s were not reported by %q and are considered revoked",
	// [token, error]
	SubjectWalletMissing: "Wallet retrieval error for active order %s: %v",
	// [side, token, match status]
	SubjectMatchErrorCoin: "Match %s for order %s is in state %s, but has no maker swap coin.",
	// [side, token, match status]
	SubjectMatchErrorContract: "Match %s for order %s is in state %s, but has no maker swap contract.",
	// [ticker, contract, token, error]
	SubjectMatchRecoveryError: "Error auditing counter-party's swap contract (%s %v) during swap recovery on order %s: %v",
	// [token]
	SubjectOrderCoinError: "No funding coins recorded for active order %s",
	// [token, ticker, error]
	SubjectOrderCoinFetchError: "Source coins retrieval error for order %s (%s): %v",
	// [token, ticker, error]
	SubjectMissedCancel: "Cancel order did not match for order %s. This can happen if the cancel order is submitted in the same epoch as the trade or if the target order is fully executed before matching with the cancel order.",
	// [capitalized sell string, base ticker, quote ticker, host, token]
	SubjectOrderCanceled: "%s order on %s-%s at %s has been canceled (%s)",
	// [capitalized sell string, base ticker, quote ticker, fill percent, token]
	SubjectMatchesMade: "%s order on %s-%s %.1f%% filled (%s)",
	// [qty, ticker, token]
	SubjectSwapSendError: "Error encountered sending a swap output(s) worth %.8f %s on order %s",
	// [match, error]
	SubjectInitError: "Error notifying DEX of swap for match %s: %v",
	// [match, error]
	SubjectReportRedeemError: "Error notifying DEX of redemption for match %s: %v",
	// [qty, ticker, token]
	SubjectSwapsInitiated: "Sent swaps worth %.8f %s on order %s",
	// [qty, ticker, token]
	SubjectRedemptionError: "Error encountered sending redemptions worth %.8f %s on order %s",
	// [qty, ticker, token]
	SubjectMatchComplete: "Redeemed %.8f %s on order %s",
	// [qty, ticker, token]
	SubjectRefundFailure: "Refunded %.8f %s on order %s, with some errors",
	// [qty, ticker, token]
	SubjectMatchesRefunded: "Refunded %.8f %s on order %s",
	// [match ID token]
	SubjectMatchRevoked: "Match %s has been revoked",
	// [token, market name, host]
	SubjectOrderRevoked: "Order %s on market %s at %s has been revoked by the server",
	// [token, market name, host]
	SubjectOrderAutoRevoked: "Order %s on market %s at %s revoked due to market suspension",
	// [ticker, coin ID, match]
	SubjectMatchRecovered: "Found maker's redemption (%s: %v) and validated secret for match %s",
	// [token]
	SubjectCancellingOrder: "A cancel order has been submitted for order %s",
	// [token, old status, new status]
	SubjectOrderStatusUpdate: "Status of order %v revised from %v to %v",
	// [count, host, token]
	SubjectMatchResolutionError: "%d matches reported by %s were not found for %s.",
	// [token]
	SubjectFailedCancel: "Cancel order for order %s stuck in Epoch status for 2 epochs and is now deleted.",
	// [coin ID, ticker, match]
	SubjectAuditTrouble: "Still searching for counterparty's contract coin %v (%s) for match %s. Are your internet and wallet connections good?",
	// [host, error]
	SubjectDexAuthError: "%s: %v",
	// [count, host]
	SubjectUnknownOrders: "%d active orders reported by DEX %s were not found.",
	// [count]
	SubjectOrdersReconciled: "Statuses updated for %d orders.",
	// [ticker, address]
	SubjectWalletConfigurationUpdated: "Configuration for %s wallet has been updated. Deposit address = %s",
	//  [ticker]
	SubjectWalletPasswordUpdated: "Password for %s wallet has been updated.",
	// [market name, host, time]
	SubjectMarketSuspendScheduled: "Market %s at %s is now scheduled for suspension at %v",
	// [market name, host]
	SubjectMarketSuspended: "Trading for market %s at %s is now suspended.",
	// [market name, host]
	SubjectMarketSuspendedWithPurge: "Trading for market %s at %s is now suspended. All booked orders are now PURGED.",
	// [market name, host, time]
	SubjectMarketResumeScheduled: "Market %s at %s is now scheduled for resumption at %v",
	// [market name, host, epoch]
	SubjectMarketResumed: "Market %s at %s has resumed trading at epoch %d",
	// [host]
	SubjectUpgradeNeeded: "You may need to update your client to trade at %s.",
	// [host]
	SubjectDEXConnected: "%s is connected",
	// [host]
	SubjectDEXDisconnected: "%s is disconnected",
	// [host, rule, time, details]
	SubjectPenalized: "Penalty from DEX at %s\nlast broken rule: %s\ntime: %v\ndetails:\n\"%s\"\n",
}

// EnLocale is the english translations. We can construct the EnLocale in an
// init function, since the subjects and templates are untranslated from the
// TemplateKeys. Other translators will define each entry in a struct literal.
var EnLocale = map[string]*Translation{}

var Subjects map[string]map[string]string

func registerTranslations(lang language.Tag, translations map[string]*Translation) {
	for subject, t := range translations {
		tmplKey := TemplateKeys[subject]
		message.SetString(lang, tmplKey, t.Template)
		message.SetString(lang, subject, t.Subject)
	}
}

func inintializeEnLocale() {
	for subject, t := range TemplateKeys {
		EnLocale[subject] = &Translation{
			Subject:  subject,
			Template: t,
		}
	}
	registerTranslations(language.AmericanEnglish, EnLocale)
}

func init() {
	inintializeEnLocale()
}
