package core

import (
	"fmt"

	"decred.org/dcrdex/client/intl"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

type translation struct {
	subject  intl.Translation
	template intl.Translation
}

const originLang = "en-US"

// originLocale is the American English translations.
var originLocale = map[Topic]*translation{
	TopicAccountRegistered: {
		subject:  intl.Translation{T: "Account registered"},
		template: intl.Translation{T: "You may now trade at %s", Notes: "args: [host]"},
	},
	TopicFeePaymentInProgress: {
		subject:  intl.Translation{T: "Fee payment in progress"},
		template: intl.Translation{T: "Waiting for %d confirmations before trading at %s", Notes: "args: [confs, host]"},
	},
	TopicRegUpdate: {
		subject:  intl.Translation{T: "regupdate"},
		template: intl.Translation{T: "Fee payment confirmations %v/%v", Notes: "args: [confs, required confs]"},
	},
	TopicFeePaymentError: {
		subject:  intl.Translation{T: "Fee payment error"},
		template: intl.Translation{T: "Error encountered while paying fees to %s: %v", Notes: "args: [host, error]"},
	},
	TopicAccountUnlockError: {
		subject:  intl.Translation{T: "Account unlock error"},
		template: intl.Translation{T: "error unlocking account for %s: %v", Notes: "args: [host, error]"},
	},
	TopicFeeCoinError: {
		subject:  intl.Translation{T: "Fee coin error"},
		template: intl.Translation{T: "Empty fee coin for %s.", Notes: "args: [host]"},
	},
	TopicWalletConnectionWarning: {
		subject:  intl.Translation{T: "Wallet connection warning"},
		template: intl.Translation{T: "Incomplete registration detected for %s, but failed to connect to the Decred wallet", Notes: "args: [host]"},
	},
	TopicBondWalletNotConnected: {
		subject:  intl.Translation{T: "Bond wallet not connected"},
		template: intl.Translation{T: "Wallet for selected bond asset %s is not connected"},
	},
	TopicWalletUnlockError: {
		subject:  intl.Translation{T: "Wallet unlock error"},
		template: intl.Translation{T: "Connected to wallet to complete registration at %s, but failed to unlock: %v", Notes: "args: [host, error]"},
	},
	TopicWalletCommsWarning: {
		subject:  intl.Translation{T: "Wallet connection issue"},
		template: intl.Translation{T: "Unable to communicate with %v wallet! Reason: %v", Notes: "args: [asset name, error message]"},
	},
	TopicWalletPeersWarning: {
		subject:  intl.Translation{T: "Wallet network issue"},
		template: intl.Translation{T: "%v wallet has no network peers!", Notes: "args: [asset name]"},
	},
	TopicWalletPeersRestored: {
		subject:  intl.Translation{T: "Wallet connectivity restored"},
		template: intl.Translation{T: "%v wallet has reestablished connectivity.", Notes: "args: [asset name]"},
	},
	TopicSendError: {
		subject:  intl.Translation{T: "Send error"},
		template: intl.Translation{Version: 1, T: "Error encountered while sending %s: %v", Notes: "args: [ticker, error]"},
	},
	TopicSendSuccess: {
		subject:  intl.Translation{T: "Send successful"},
		template: intl.Translation{Version: 1, T: "Sending %s %s to %s has completed successfully. Tx ID = %s", Notes: "args: [value string, ticker, destination address, coin ID]"},
	},
	TopicAsyncOrderFailure: {
		subject:  intl.Translation{T: "In-Flight Order Error"},
		template: intl.Translation{T: "In-Flight order with ID %v failed: %v", Notes: "args: order ID, error]"},
	},
	TopicOrderQuantityTooHigh: {
		subject:  intl.Translation{T: "Trade limit exceeded"},
		template: intl.Translation{T: "Order quantity exceeds current trade limit on %s", Notes: "args: [host]"},
	},
	TopicOrderLoadFailure: {
		subject:  intl.Translation{T: "Order load failure"},
		template: intl.Translation{T: "Some orders failed to load from the database: %v", Notes: "args: [error]"},
	},
	TopicYoloPlaced: {
		subject:  intl.Translation{T: "Market order placed"},
		template: intl.Translation{T: "selling %s %s at market rate (%s)", Notes: "args: [qty, ticker, token]"},
	},
	TopicBuyOrderPlaced: {
		subject:  intl.Translation{T: "Order placed"},
		template: intl.Translation{Version: 1, T: "Buying %s %s, rate = %s (%s)", Notes: "args: [qty, ticker, rate string, token]"},
	},
	TopicSellOrderPlaced: {
		subject:  intl.Translation{T: "Order placed"},
		template: intl.Translation{Version: 1, T: "Selling %s %s, rate = %s (%s)", Notes: "args: [qty, ticker, rate string, token]"},
	},
	TopicMissingMatches: {
		subject:  intl.Translation{T: "Missing matches"},
		template: intl.Translation{T: "%d matches for order %s were not reported by %q and are considered revoked", Notes: "args: [missing count, token, host]"},
	},
	TopicWalletMissing: {
		subject:  intl.Translation{T: "Wallet missing"},
		template: intl.Translation{T: "Wallet retrieval error for active order %s: %v", Notes: "args: [token, error]"},
	},
	TopicMatchErrorCoin: {
		subject:  intl.Translation{T: "Match coin error"},
		template: intl.Translation{T: "Match %s for order %s is in state %s, but has no maker swap coin.", Notes: "args: [side, token, match status]"},
	},
	TopicMatchErrorContract: {
		subject:  intl.Translation{T: "Match contract error"},
		template: intl.Translation{T: "Match %s for order %s is in state %s, but has no maker swap contract.", Notes: "args: [side, token, match status]"},
	},
	TopicMatchRecoveryError: {
		subject:  intl.Translation{T: "Match recovery error"},
		template: intl.Translation{T: "Error auditing counter-party's swap contract (%s %v) during swap recovery on order %s: %v", Notes: "args: [ticker, contract, token, error]"},
	},
	TopicOrderCoinError: {
		subject:  intl.Translation{T: "Order coin error"},
		template: intl.Translation{T: "No funding coins recorded for active order %s", Notes: "args: [token]"},
	},
	TopicOrderCoinFetchError: {
		subject:  intl.Translation{T: "Order coin fetch error"},
		template: intl.Translation{T: "Source coins retrieval error for order %s (%s): %v", Notes: "args: [token, ticker, error]"},
	},
	TopicMissedCancel: {
		subject:  intl.Translation{T: "Missed cancel"},
		template: intl.Translation{T: "Cancel order did not match for order %s. This can happen if the cancel order is submitted in the same epoch as the trade or if the target order is fully executed before matching with the cancel order.", Notes: "args: [token]"},
	},
	TopicBuyOrderCanceled: {
		subject:  intl.Translation{T: "Order canceled"},
		template: intl.Translation{Version: 1, T: "Buy order on %s-%s at %s has been canceled (%s)", Notes: "args: [base ticker, quote ticker, host, token]"},
	},
	TopicSellOrderCanceled: {
		subject:  intl.Translation{T: "Order canceled"},
		template: intl.Translation{Version: 1, T: "Sell order on %s-%s at %s has been canceled (%s)"},
	},
	TopicBuyMatchesMade: {
		subject:  intl.Translation{T: "Matches made"},
		template: intl.Translation{Version: 1, T: "Buy order on %s-%s %.1f%% filled (%s)", Notes: "args: [base ticker, quote ticker, fill percent, token]"},
	},
	TopicSellMatchesMade: {
		subject:  intl.Translation{T: "Matches made"},
		template: intl.Translation{Version: 1, T: "Sell order on %s-%s %.1f%% filled (%s)", Notes: "args: [base ticker, quote ticker, fill percent, token]"},
	},
	TopicSwapSendError: {
		subject:  intl.Translation{T: "Swap send error"},
		template: intl.Translation{T: "Error encountered sending a swap output(s) worth %s %s on order %s", Notes: "args: [qty, ticker, token]"},
	},
	TopicInitError: {
		subject:  intl.Translation{T: "Swap reporting error"},
		template: intl.Translation{T: "Error notifying DEX of swap for match %s: %v", Notes: "args: [match, error]"},
	},
	TopicReportRedeemError: {
		subject:  intl.Translation{T: "Redeem reporting error"},
		template: intl.Translation{T: "Error notifying DEX of redemption for match %s: %v", Notes: "args: [match, error]"},
	},
	TopicSwapsInitiated: {
		subject:  intl.Translation{T: "Swaps initiated"},
		template: intl.Translation{T: "Sent swaps worth %s %s on order %s", Notes: "args: [qty, ticker, token]"},
	},
	TopicRedemptionError: {
		subject:  intl.Translation{T: "Redemption error"},
		template: intl.Translation{T: "Error encountered sending redemptions worth %s %s on order %s", Notes: "args: [qty, ticker, token]"},
	},
	TopicMatchComplete: {
		subject:  intl.Translation{T: "Match complete"},
		template: intl.Translation{T: "Redeemed %s %s on order %s", Notes: "args: [qty, ticker, token]"},
	},
	TopicRefundFailure: {
		subject:  intl.Translation{T: "Refund Failure"},
		template: intl.Translation{T: "Refunded %s %s on order %s, with some errors", Notes: "args: [qty, ticker, token]"},
	},
	TopicMatchesRefunded: {
		subject:  intl.Translation{T: "Matches Refunded"},
		template: intl.Translation{T: "Refunded %s %s on order %s", Notes: "args: [qty, ticker, token]"},
	},
	TopicMatchRevoked: {
		subject:  intl.Translation{T: "Match revoked"},
		template: intl.Translation{T: "Match %s has been revoked", Notes: "args: [match ID token]"},
	},
	TopicOrderRevoked: {
		subject:  intl.Translation{T: "Order revoked"},
		template: intl.Translation{T: "Order %s on market %s at %s has been revoked by the server", Notes: "args: [token, market name, host]"},
	},
	TopicOrderAutoRevoked: {
		subject:  intl.Translation{T: "Order auto-revoked"},
		template: intl.Translation{T: "Order %s on market %s at %s revoked due to market suspension", Notes: "args: [token, market name, host]"},
	},
	TopicMatchRecovered: {
		subject:  intl.Translation{T: "Match recovered"},
		template: intl.Translation{T: "Found maker's redemption (%s: %v) and validated secret for match %s", Notes: "args: [ticker, coin ID, match]"},
	},
	TopicCancellingOrder: {
		subject:  intl.Translation{T: "Cancelling order"},
		template: intl.Translation{T: "A cancel order has been submitted for order %s", Notes: "args: [token]"},
	},
	TopicOrderStatusUpdate: {
		subject:  intl.Translation{T: "Order status update"},
		template: intl.Translation{T: "Status of order %v revised from %v to %v", Notes: "args: [token, old status, new status]"},
	},
	TopicMatchResolutionError: {
		subject:  intl.Translation{T: "Match resolution error"},
		template: intl.Translation{T: "%d matches reported by %s were not found for %s.", Notes: "args: [count, host, token]"},
	},
	TopicFailedCancel: {
		subject: intl.Translation{T: "Failed cancel"},
		template: intl.Translation{
			Version: 1,
			T:       "Cancel order for order %s failed and is now deleted.",
			Notes: `args: [token], "failed" means we missed the preimage request ` +
				`and either got the revoke_order message or it stayed in epoch status for too long.`,
		},
	},
	TopicAuditTrouble: {
		subject:  intl.Translation{T: "Audit trouble"},
		template: intl.Translation{T: "Still searching for counterparty's contract coin %v (%s) for match %s. Are your internet and wallet connections good?", Notes: "args: [coin ID, ticker, match]"},
	},
	TopicDexAuthError: {
		subject:  intl.Translation{T: "DEX auth error"},
		template: intl.Translation{T: "%s: %v", Notes: "args: [host, error]"},
	},
	TopicUnknownOrders: {
		subject:  intl.Translation{T: "DEX reported unknown orders"},
		template: intl.Translation{T: "%d active orders reported by DEX %s were not found.", Notes: "args: [count, host]"},
	},
	TopicOrdersReconciled: {
		subject:  intl.Translation{T: "Orders reconciled with DEX"},
		template: intl.Translation{T: "Statuses updated for %d orders.", Notes: "args: [count]"},
	},
	TopicWalletConfigurationUpdated: {
		subject:  intl.Translation{T: "Wallet configuration updated"},
		template: intl.Translation{T: "Configuration for %s wallet has been updated. Deposit address = %s", Notes: "args: [ticker, address]"},
	},
	TopicWalletPasswordUpdated: {
		subject:  intl.Translation{T: "Wallet Password Updated"},
		template: intl.Translation{T: "Password for %s wallet has been updated.", Notes: "args:  [ticker]"},
	},
	TopicMarketSuspendScheduled: {
		subject:  intl.Translation{T: "Market suspend scheduled"},
		template: intl.Translation{T: "Market %s at %s is now scheduled for suspension at %v", Notes: "args: [market name, host, time]"},
	},
	TopicMarketSuspended: {
		subject:  intl.Translation{T: "Market suspended"},
		template: intl.Translation{T: "Trading for market %s at %s is now suspended.", Notes: "args: [market name, host]"},
	},
	TopicMarketSuspendedWithPurge: {
		subject:  intl.Translation{T: "Market suspended, orders purged"},
		template: intl.Translation{T: "Trading for market %s at %s is now suspended. All booked orders are now PURGED.", Notes: "args: [market name, host]"},
	},
	TopicMarketResumeScheduled: {
		subject:  intl.Translation{T: "Market resume scheduled"},
		template: intl.Translation{T: "Market %s at %s is now scheduled for resumption at %v", Notes: "args: [market name, host, time]"},
	},
	TopicMarketResumed: {
		subject:  intl.Translation{T: "Market resumed"},
		template: intl.Translation{T: "Market %s at %s has resumed trading at epoch %d", Notes: "args: [market name, host, epoch]"},
	},
	TopicUpgradeNeeded: {
		subject:  intl.Translation{T: "Upgrade needed"},
		template: intl.Translation{T: "You may need to update your client to trade at %s.", Notes: "args: [host]"},
	},
	TopicDEXConnected: {
		subject:  intl.Translation{T: "Server connected"},
		template: intl.Translation{T: "%s is connected", Notes: "args: [host]"},
	},
	TopicDEXDisconnected: {
		subject:  intl.Translation{T: "Server disconnect"},
		template: intl.Translation{T: "%s is disconnected", Notes: "args: [host]"},
	},
	TopicDexConnectivity: {
		subject:  intl.Translation{T: "Internet Connectivity"},
		template: intl.Translation{T: "Your internet connection to %s is unstable, check your internet connection", Notes: "args: [host]"},
	},
	TopicPenalized: {
		subject:  intl.Translation{T: "Server has penalized you"},
		template: intl.Translation{T: "Penalty from DEX at %s\nlast broken rule: %s\ntime: %v\ndetails:\n\"%s\"\n", Notes: "args: [host, rule, time, details]"},
	},
	TopicSeedNeedsSaving: {
		subject:  intl.Translation{T: "Don't forget to back up your application seed"},
		template: intl.Translation{T: "A new application seed has been created. Make a back up now in the settings view."},
	},
	TopicUpgradedToSeed: {
		subject:  intl.Translation{T: "Back up your new application seed"},
		template: intl.Translation{T: "The client has been upgraded to use an application seed. Back up the seed now in the settings view."},
	},
	TopicDEXNotification: {
		subject:  intl.Translation{T: "Message from DEX"},
		template: intl.Translation{T: "%s: %s", Notes: "args: [host, msg]"},
	},
	TopicQueuedCreationFailed: {
		subject:  intl.Translation{T: "Failed to create token wallet"},
		template: intl.Translation{T: "After creating %s wallet, failed to create the %s wallet", Notes: "args: [parentSymbol, tokenSymbol]"},
	},
	TopicRedemptionResubmitted: {
		subject:  intl.Translation{T: "Redemption Resubmitted"},
		template: intl.Translation{T: "Your redemption for match %s in order %s was resubmitted."},
	},
	TopicRefundResubmitted: {
		subject:  intl.Translation{T: "Refund Resubmitted"},
		template: intl.Translation{T: "Your refund for match %s in order %s was resubmitted."},
	},
	TopicSwapRefunded: {
		subject:  intl.Translation{T: "Swap Refunded"},
		template: intl.Translation{T: "Match %s in order %s was refunded by the counterparty."},
	},
	TopicRedemptionConfirmed: {
		subject:  intl.Translation{T: "Redemption Confirmed"},
		template: intl.Translation{T: "Your redemption for match %s in order %s was confirmed"},
	},
	TopicRefundConfirmed: {
		subject:  intl.Translation{T: "Refund Confirmed"},
		template: intl.Translation{T: "Your refund for match %s in order %s was confirmed"},
	},
	TopicWalletTypeDeprecated: {
		subject:  intl.Translation{T: "Wallet Disabled"},
		template: intl.Translation{T: "Your %s wallet type is no longer supported. Create a new wallet."},
	},
	TopicOrderResumeFailure: {
		subject:  intl.Translation{T: "Resume order failure"},
		template: intl.Translation{T: "Failed to resume processing of trade: %v"},
	},
	TopicBondConfirming: {
		subject:  intl.Translation{T: "Confirming bond"},
		template: intl.Translation{T: "Waiting for %d confirmations to post bond %v (%s) to %s", Notes: "args: [reqConfs, bondCoinStr, assetID, acct.host]"},
	},
	TopicBondConfirmed: {
		subject:  intl.Translation{T: "Bond confirmed"},
		template: intl.Translation{T: "New tier = %d (target = %d).", Notes: "args: [effectiveTier, targetTier]"},
	},
	TopicBondExpired: {
		subject:  intl.Translation{T: "Bond expired"},
		template: intl.Translation{T: "New tier = %d (target = %d).", Notes: "args: [effectiveTier, targetTier]"},
	},
	TopicBondRefunded: {
		subject:  intl.Translation{T: "Bond refunded"},
		template: intl.Translation{T: "Bond %v for %v refunded in %v, reclaiming %v of %v after tx fees", Notes: "args: [bondIDStr, acct.host, refundCoinStr, refundVal, Amount]"},
	},
	TopicBondPostError: {
		subject:  intl.Translation{T: "Bond post error"},
		template: intl.Translation{T: "postbond request error (will retry): %v (%T)", Notes: "args: [err, err]"},
	},
	TopicBondPostErrorConfirm: {
		subject:  intl.Translation{T: "Bond post error"},
		template: intl.Translation{T: "Error encountered while waiting for bond confirms for %s: %v"},
	},
	TopicDexAuthErrorBond: {
		subject:  intl.Translation{T: "Authentication error"},
		template: intl.Translation{T: "Bond confirmed, but failed to authenticate connection: %v", Notes: "args: [err]"},
	},
	TopicAccountRegTier: {
		subject:  intl.Translation{T: "Account registered"},
		template: intl.Translation{T: "New tier = %d", Notes: "args: [effectiveTier]"},
	},
	TopicUnknownBondTierZero: {
		subject: intl.Translation{T: "Unknown bond found"},
		template: intl.Translation{
			T: "Unknown %s bonds were found and added to active bonds " +
				"but your target tier is zero for the dex at %s. Set your " +
				"target tier in Settings to stay bonded with auto renewals.",
			Notes: "args: [bond asset, dex host]",
		},
	},
}

var ptBR = map[Topic]*translation{
	TopicAccountRegistered: {
		subject:  intl.Translation{T: "Conta Registrada"},
		template: intl.Translation{T: "Você agora pode trocar em %s"},
	},
	TopicFeePaymentInProgress: {
		subject:  intl.Translation{T: "Pagamento da Taxa em andamento"},
		template: intl.Translation{T: "Esperando por %d confirmações antes de trocar em %s"},
	},
	TopicRegUpdate: {
		subject:  intl.Translation{T: "Atualização de registro"},
		template: intl.Translation{T: "Confirmações da taxa %v/%v"},
	},
	TopicFeePaymentError: {
		subject:  intl.Translation{T: "Erro no Pagamento da Taxa"},
		template: intl.Translation{T: "Erro enquanto pagando taxa para %s: %v"},
	},
	TopicAccountUnlockError: {
		subject:  intl.Translation{T: "Erro ao Destrancar carteira"},
		template: intl.Translation{T: "erro destrancando conta %s: %v"},
	},
	TopicFeeCoinError: {
		subject:  intl.Translation{T: "Erro na Taxa"},
		template: intl.Translation{T: "Taxa vazia para %s."},
	},
	TopicWalletConnectionWarning: {
		subject:  intl.Translation{T: "Aviso de Conexão com a Carteira"},
		template: intl.Translation{T: "Registro incompleto detectado para %s, mas falhou ao conectar com carteira decred"},
	},
	TopicWalletUnlockError: {
		subject:  intl.Translation{T: "Erro ao Destravar Carteira"},
		template: intl.Translation{T: "Conectado com carteira para completar o registro em %s, mas falha ao destrancar: %v"},
	},
	TopicSendError: {
		subject:  intl.Translation{T: "Erro Retirada"},
		template: intl.Translation{T: "Erro encontrado durante retirada de %s: %v"},
	},
	TopicSendSuccess: {
		template: intl.Translation{T: "Retirada de %s %s (%s) foi completada com sucesso. ID da moeda = %s"},
		subject:  intl.Translation{T: "Retirada Enviada"},
	},
	TopicOrderLoadFailure: {
		template: intl.Translation{T: "Alguns pedidos falharam ao carregar da base de dados: %v"},
		subject:  intl.Translation{T: "Carregamendo de Pedidos Falhou"},
	},
	TopicYoloPlaced: {
		template: intl.Translation{T: "vendendo %s %s a taxa de mercado (%s)"},
		subject:  intl.Translation{T: "Ordem de Mercado Colocada"},
	},
	TopicBuyOrderPlaced: {
		subject:  intl.Translation{T: "Ordem Colocada"},
		template: intl.Translation{T: "Buying %s %s, valor = %s (%s)"},
	},
	TopicSellOrderPlaced: {
		subject:  intl.Translation{T: "Ordem Colocada"},
		template: intl.Translation{T: "Selling %s %s, valor = %s (%s)"},
	},
	TopicMissingMatches: {
		template: intl.Translation{T: "%d combinações para pedidos %s não foram reportados por %q e foram considerados revocados"},
		subject:  intl.Translation{T: "Pedidos Faltando Combinações"},
	},
	TopicWalletMissing: {
		template: intl.Translation{T: "Erro ao recuperar pedidos ativos por carteira %s: %v"},
		subject:  intl.Translation{T: "Carteira Faltando"},
	},
	TopicMatchErrorCoin: {
		subject:  intl.Translation{T: "Erro combinação de Moedas"},
		template: intl.Translation{T: "Combinação %s para pedido %s está no estado %s, mas não há um executador para trocar moedas."},
	},
	TopicMatchErrorContract: {
		template: intl.Translation{T: "Combinação %s para pedido %s está no estado %s, mas não há um executador para trocar moedas."},
		subject:  intl.Translation{T: "Erro na Combinação de Contrato"},
	},
	TopicMatchRecoveryError: {
		template: intl.Translation{T: "Erro auditando contrato de troca da contraparte (%s %v) durante troca recuperado no pedido %s: %v"},
		subject:  intl.Translation{T: "Erro Recuperando Combinações"},
	},
	TopicOrderCoinError: {
		template: intl.Translation{T: "Não há Moedas de financiamento registradas para pedidos ativos %s"},
		subject:  intl.Translation{T: "Erro no Pedido da Moeda"},
	},
	TopicOrderCoinFetchError: {
		template: intl.Translation{T: "Erro ao recuperar moedas de origem para pedido %s (%s): %v"},
		subject:  intl.Translation{T: "Erro na Recuperação do Pedido de Moedas"},
	},
	TopicMissedCancel: {
		template: intl.Translation{T: "Pedido de cancelamento não combinou para pedido %s. Isto pode acontecer se o pedido de cancelamento foi enviado no mesmo epoque do que a troca ou se o pedido foi completamente executado antes da ordem de cancelamento ser executada."},
		subject:  intl.Translation{T: "Cancelamento Perdido"},
	},
	TopicSellOrderCanceled: {
		template: intl.Translation{T: "Sell pedido sobre %s-%s em %s foi cancelado (%s)"},
		subject:  intl.Translation{T: "Cancelamento de Pedido"},
	},
	TopicBuyOrderCanceled: {
		template: intl.Translation{T: "Buy pedido sobre %s-%s em %s foi cancelado (%s)"},
		subject:  intl.Translation{T: "Cancelamento de Pedido"},
	},
	TopicSellMatchesMade: {
		template: intl.Translation{T: "Sell pedido sobre %s-%s %.1f%% preenchido (%s)"},
		subject:  intl.Translation{T: "Combinações Feitas"},
	},
	TopicBuyMatchesMade: {
		template: intl.Translation{T: "Buy pedido sobre %s-%s %.1f%% preenchido (%s)"},
		subject:  intl.Translation{T: "Combinações Feitas"},
	},
	TopicSwapSendError: {
		template: intl.Translation{T: "Erro encontrado ao enviar a troca com output(s) no valor de %s %s no pedido %s"},
		subject:  intl.Translation{T: "Erro ao Enviar Troca"},
	},
	TopicInitError: {
		template: intl.Translation{T: "Erro notificando DEX da troca %s por combinação: %v"},
		subject:  intl.Translation{T: "Erro na Troca"},
	},
	TopicReportRedeemError: {
		template: intl.Translation{T: "Erro notificando DEX da redenção %s por combinação: %v"},
		subject:  intl.Translation{T: "Reportando Erro na redenção"},
	},
	TopicSwapsInitiated: {
		template: intl.Translation{T: "Enviar trocas no valor de %s %s no pedido %s"},
		subject:  intl.Translation{T: "Trocas Iniciadas"},
	},
	TopicRedemptionError: {
		template: intl.Translation{T: "Erro encontrado enviado redenção no valor de %s %s no pedido %s"},
		subject:  intl.Translation{T: "Erro na Redenção"},
	},
	TopicMatchComplete: {
		template: intl.Translation{T: "Resgatado %s %s no pedido %s"},
		subject:  intl.Translation{T: "Combinação Completa"},
	},
	TopicRefundFailure: {
		template: intl.Translation{T: "Devolvidos %s %s no pedido %s, com algum erro"},
		subject:  intl.Translation{T: "Erro no Reembolso"},
	},
	TopicMatchesRefunded: {
		template: intl.Translation{T: "Devolvidos %s %s no pedido %s"},
		subject:  intl.Translation{T: "Reembolso Sucedido"},
	},
	TopicMatchRevoked: {
		template: intl.Translation{T: "Combinação %s foi revocada"},
		subject:  intl.Translation{T: "Combinação Revocada"},
	},
	TopicOrderRevoked: {
		template: intl.Translation{T: "Pedido %s no mercado %s em %s foi revocado pelo servidor"},
		subject:  intl.Translation{T: "Pedido Revocado"},
	},
	TopicOrderAutoRevoked: {
		template: intl.Translation{T: "Pedido %s no mercado %s em %s revocado por suspenção do mercado"},
		subject:  intl.Translation{T: "Pedido Revocado Automatiamente"},
	},
	TopicMatchRecovered: {
		template: intl.Translation{T: "Encontrado redenção do executador (%s: %v) e validado segredo para pedido %s"},
		subject:  intl.Translation{T: "Pedido Recuperado"},
	},
	TopicCancellingOrder: {
		template: intl.Translation{T: "Uma ordem de cancelamento foi submetida para o pedido %s"},
		subject:  intl.Translation{T: "Cancelando Pedido"},
	},
	TopicOrderStatusUpdate: {
		template: intl.Translation{T: "Status do pedido %v revisado de %v para %v"},
		subject:  intl.Translation{T: "Status do Pedido Atualizado"},
	},
	TopicMatchResolutionError: {
		template: intl.Translation{T: "%d combinações reportada para %s não foram encontradas para %s."},
		subject:  intl.Translation{T: "Erro na Resolução do Pedido"},
	},
	TopicFailedCancel: {
		template: intl.Translation{T: "Ordem de cancelamento para pedido %s presa em estado de Epoque por 2 epoques e foi agora deletado."},
		subject:  intl.Translation{T: "Falhou Cancelamento"},
	},
	TopicAuditTrouble: {
		template: intl.Translation{T: "Continua procurando por contrato de contrapartes para moeda %v (%s) para combinação %s. Sua internet e conexão com a carteira estão ok?"},
		subject:  intl.Translation{T: "Problemas ao Auditar"},
	},
	TopicDexAuthError: {
		template: intl.Translation{T: "%s: %v"},
		subject:  intl.Translation{T: "Erro na Autenticação"},
	},
	TopicUnknownOrders: {
		template: intl.Translation{T: "%d pedidos ativos reportados pela DEX %s não foram encontrados."},
		subject:  intl.Translation{T: "DEX Reportou Pedidos Desconhecidos"},
	},
	TopicOrdersReconciled: {
		template: intl.Translation{T: "Estados atualizados para %d pedidos."},
		subject:  intl.Translation{T: "Pedidos Reconciliados com DEX"},
	},
	TopicWalletConfigurationUpdated: {
		template: intl.Translation{T: "configuração para carteira %s foi atualizada. Endereço de depósito = %s"},
		subject:  intl.Translation{T: "Configurações da Carteira Atualizada"},
	},
	TopicWalletPasswordUpdated: {
		template: intl.Translation{T: "Senha para carteira %s foi atualizada."},
		subject:  intl.Translation{T: "Senha da Carteira Atualizada"},
	},
	TopicMarketSuspendScheduled: {
		template: intl.Translation{T: "Mercado %s em %s está agora agendado para suspensão em %v"},
		subject:  intl.Translation{T: "Suspensão de Mercado Agendada"},
	},
	TopicMarketSuspended: {
		template: intl.Translation{T: "Trocas no mercado %s em %s está agora suspenso."},
		subject:  intl.Translation{T: "Mercado Suspenso"},
	},
	TopicMarketSuspendedWithPurge: {
		template: intl.Translation{T: "Trocas no mercado %s em %s está agora suspenso. Todos pedidos no livro de ofertas foram agora EXPURGADOS."},
		subject:  intl.Translation{T: "Mercado Suspenso, Pedidos Expurgados"},
	},
	TopicMarketResumeScheduled: {
		template: intl.Translation{T: "Mercado %s em %s está agora agendado para resumir em %v"},
		subject:  intl.Translation{T: "Resumo do Mercado Agendado"},
	},
	TopicMarketResumed: {
		template: intl.Translation{T: "Mercado %s em %s foi resumido para trocas no epoque %d"},
		subject:  intl.Translation{T: "Mercado Resumido"},
	},
	TopicUpgradeNeeded: {
		template: intl.Translation{T: "Você pode precisar atualizar seu cliente para trocas em %s."},
		subject:  intl.Translation{T: "Atualização Necessária"},
	},
	TopicDEXConnected: {
		subject:  intl.Translation{T: "DEX conectado"},
		template: intl.Translation{T: "%s está conectado"},
	},
	TopicDEXDisconnected: {
		template: intl.Translation{T: "%s está desconectado"},
		subject:  intl.Translation{T: "Server Disconectado"},
	},
	TopicPenalized: {
		template: intl.Translation{T: "Penalidade de DEX em %s\núltima regra quebrada: %s\nhorário: %v\ndetalhes:\n\"%s\"\n"},
		subject:  intl.Translation{T: "Server Penalizou Você"},
	},
	TopicSeedNeedsSaving: {
		subject:  intl.Translation{T: "Não se esqueça de guardar a seed do app"},
		template: intl.Translation{T: "Uma nova seed para a aplicação foi criada. Faça um backup agora na página de configurações."},
	},
	TopicUpgradedToSeed: {
		subject:  intl.Translation{T: "Guardar nova seed do app"},
		template: intl.Translation{T: "O cliente foi atualizado para usar uma seed. Faça backup dessa seed na página de configurações."},
	},
	TopicDEXNotification: {
		subject:  intl.Translation{T: "Mensagem da DEX"},
		template: intl.Translation{T: "%s: %s"},
	},
}

// zhCN is the Simplified Chinese (PRC) translations.
var zhCN = map[Topic]*translation{
	TopicAccountRegistered: {
		subject:  intl.Translation{T: "注册账户"},
		template: intl.Translation{T: "您现在可以在 %s 进行交易"}, // alt. 您现在可以切换到 %s
	},
	TopicFeePaymentInProgress: {
		subject:  intl.Translation{T: "费用支付中"},
		template: intl.Translation{T: "在切换到 %s 之前等待 %d 次确认"}, // alt. 在 %s 交易之前等待 %d 确认
	},
	TopicRegUpdate: {
		subject:  intl.Translation{T: "费用支付确认"}, // alt. 记录更新 (but not displayed)
		template: intl.Translation{T: "%v/%v 费率确认"},
	},
	TopicFeePaymentError: {
		subject:  intl.Translation{T: "费用支付错误"},
		template: intl.Translation{T: "向 %s 支付费用时遇到错误: %v"}, // alt. 为 %s 支付费率时出错：%v
	},
	TopicAccountUnlockError: {
		subject:  intl.Translation{T: "解锁钱包时出错"},
		template: intl.Translation{T: "解锁帐户 %s 时出错： %v"}, // alt. 解锁 %s 的帐户时出错: %v
	},
	TopicFeeCoinError: {
		subject:  intl.Translation{T: "汇率错误"},
		template: intl.Translation{T: "%s 的空置率。"}, // alt. %s 的费用硬币为空。
	},
	TopicWalletConnectionWarning: {
		subject:  intl.Translation{T: "钱包连接通知"},
		template: intl.Translation{T: "检测到 %s 的注册不完整，无法连接 decred 钱包"}, // alt. 检测到 %s 的注册不完整，无法连接到 Decred 钱包
	},
	TopicWalletUnlockError: {
		subject:  intl.Translation{T: "解锁钱包时出错"},
		template: intl.Translation{T: "与 decred 钱包连接以在 %s 上完成注册，但无法解锁： %v"}, // alt. 已连接到 Decred 钱包以在 %s 完成注册，但无法解锁：%v
	},
	TopicSendError: {
		subject:  intl.Translation{T: "提款错误"},
		template: intl.Translation{T: "在 %s 提取过程中遇到错误: %v"}, // alt. 删除 %s 时遇到错误： %v
	},
	TopicSendSuccess: {
		subject:  intl.Translation{T: "提款已发送"},
		template: intl.Translation{T: "已成功发送 %s %s 到 %s。交易 ID = %s"}, // alt. %s %s (%s) 的提款已成功完成。硬币 ID = %s
	},
	TopicOrderLoadFailure: {
		subject:  intl.Translation{T: "请求加载失败"},
		template: intl.Translation{T: "某些订单无法从数据库加载：%v"}, // alt. 某些请求无法从数据库加载:
	},
	TopicYoloPlaced: {
		subject:  intl.Translation{T: "下达市价单"},
		template: intl.Translation{T: "以市场价格 (%[3]s) 出售 %[1]s %[2]s"},
	},
	// [qty, ticker, rate string, token], RETRANSLATE -> Retranslated.
	TopicBuyOrderPlaced: {
		subject:  intl.Translation{T: "已下订单"},
		template: intl.Translation{T: "购买 %s %s，价格 = %s（%s）"},
	},
	TopicSellOrderPlaced: {
		subject:  intl.Translation{T: "已下订单"},
		template: intl.Translation{T: "卖出 %s %s，价格 = %s（%s）"}, // alt. Selling %s %s，值 = %s (%s)
	},
	TopicMissingMatches: {
		subject:  intl.Translation{T: "订单缺失匹配"},
		template: intl.Translation{T: "%[2]s 订单的 %[1]d 匹配项未被 %[3]q 报告并被视为已撤销"}, // alt. %d 订单 %s 的匹配没有被 %q 报告并被视为已撤销
	},
	TopicWalletMissing: {
		subject:  intl.Translation{T: "丢失的钱包"},
		template: intl.Translation{T: "活动订单 %s 的钱包检索错误： %v"}, // alt. 通过钱包 %s 检索活动订单时出错: %v
	},
	TopicMatchErrorCoin: {
		subject:  intl.Translation{T: "货币不匹配错误"},
		template: intl.Translation{T: "订单 %s 的组合 %s 处于状态 %s，但没有用于交换货币的运行程序。"}, // alt. 订单 %s 的匹配 %s 处于状态 %s，但没有交换硬币服务商。
	},
	TopicMatchErrorContract: {
		subject:  intl.Translation{T: "合约组合错误"},
		template: intl.Translation{T: "订单 %s 的匹配 %s 处于状态 %s，没有服务商交换合约。"},
	},
	TopicMatchRecoveryError: {
		subject:  intl.Translation{T: "检索匹配时出错"},
		template: intl.Translation{T: "在检索订单 %s: %v 的交易期间审核交易对手交易合约 (%s %v) 时出错"}, // ? 在订单 %s: %v 的交易恢复期间审核对方的交易合约 (%s %v) 时出错
	},
	TopicOrderCoinError: {
		subject:  intl.Translation{T: "硬币订单错误"},
		template: intl.Translation{T: "没有为活动订单 %s 记录资金硬币"}, // alt. 没有为活动订单 %s 注册资金货币
	},
	TopicOrderCoinFetchError: {
		subject:  intl.Translation{T: "硬币订单恢复错误"},
		template: intl.Translation{T: "检索订单 %s (%s) 的源硬币时出错： %v"}, // alt. 订单 %s (%s) 的源硬币检索错误: %v
	},
	TopicMissedCancel: {
		subject:  intl.Translation{T: "丢失取消"},
		template: intl.Translation{T: "取消订单与订单 %s 不匹配。如果取消订单与交易所同时发送，或者订单在取消订单执行之前已完全执行，则可能发生这种情况。"},
	},
	TopicBuyOrderCanceled: {
		subject:  intl.Translation{T: "订单取消"},
		template: intl.Translation{T: "在 %s-%s 上的买单 %s 已被取消（%s）"}, // alt. %s 上 %s-%s 上的 %s 请求已被取消 (%s)  alt2. 买入 %s-%s 的 %s 订单已被取消 (%s)
	},
	TopicSellOrderCanceled: {
		subject:  intl.Translation{T: "订单取消"},
		template: intl.Translation{T: "在 %s-%s 上的卖单，价格为 %s，已被取消（%s）"}, // alt. %s 上 %s-%s 上的 %s 请求已被取消 (%s)  alt2. 卖出 %s-%s 的 %s 订单已被取消 (%s)
	},
	TopicBuyMatchesMade: {
		subject:  intl.Translation{T: "匹配完成"},
		template: intl.Translation{T: "在 %s-%s 上的买单已完成 %.1f%%（%s）"}, // alt. %s 请求超过 %s-%s %.1f%% 已填充（%s）
	},
	TopicSellMatchesMade: {
		subject:  intl.Translation{T: "匹配完成"},
		template: intl.Translation{T: "在 %s-%s 上的卖单已完成 %.1f%%（%s)"}, // alt. %s 请求超过 %s-%s %.1f%% 已填充（%s） alt 2. 卖出 %s-%s %.1f%% 的订单已完成 (%s)
	},
	TopicSwapSendError: {
		subject:  intl.Translation{T: "发送交换时出错"},
		template: intl.Translation{T: "发送 %s 时遇到错误：%v"}, // ? 在订单 %s 上发送价值 %.8f %s 的交换输出时遇到错误
	},
	TopicInitError: {
		subject:  intl.Translation{T: "交易错误"},
		template: intl.Translation{T: "通知 DEX 匹配 %s 的交换时出错： %v"}, // alt. 错误通知 DEX %s 交换组合：%v
	},
	TopicReportRedeemError: {
		subject:  intl.Translation{T: "报销错误"},
		template: intl.Translation{T: "通知 DEX %s 赎回时出错： %v"},
	},
	TopicSwapsInitiated: {
		subject:  intl.Translation{T: "发起交易"},
		template: intl.Translation{T: "在订单 %[3]s 上发送价值 %[1]s %[2]s 的交易"}, // should mention "contract" (TODO) ? 已发送价值 %.8f %s 的交易，订单 %s
	},
	TopicRedemptionError: {
		subject:  intl.Translation{T: "赎回错误"},
		template: intl.Translation{T: "在订单 %[3]s 上发送价值 %[1]s %[2]s 的兑换时遇到错误"}, // alt. 在订单 %s 上发现发送价值 %.8f %s 的赎回错误
	},
	TopicMatchComplete: {
		subject:  intl.Translation{T: "完全匹配"},
		template: intl.Translation{T: "在订单 %s 上兑换了 %s %s"},
	},
	TopicRefundFailure: {
		subject:  intl.Translation{T: "退款错误"},
		template: intl.Translation{T: "按顺序 %[3]s 返回 %[1]s %[2]s，有一些错误"}, // alt. 退款％.8f％s的订单％S，但出现一些错误
	},
	TopicMatchesRefunded: {
		subject:  intl.Translation{T: "退款成功"},
		template: intl.Translation{T: "在订单 %[3]s 上返回了 %[1]s %[2]s"}, // 在订单 %s 上返回了 %.8f %s
	},
	TopicMatchRevoked: {
		subject:  intl.Translation{T: "撤销组合"},
		template: intl.Translation{T: "匹配 %s 已被撤销"}, // alt. 组合 %s 已被撤销
	},
	TopicOrderRevoked: {
		subject:  intl.Translation{T: "撤销订单"},
		template: intl.Translation{T: "%s 市场 %s 的订单 %s 已被服务器撤销"},
	},
	TopicOrderAutoRevoked: {
		subject:  intl.Translation{T: "订单自动撤销"},
		template: intl.Translation{T: "%s 市场 %s 上的订单 %s 由于市场暂停而被撤销"}, // alt. %s 市场 %s 中的订单 %s 被市场暂停撤销
	},
	TopicMatchRecovered: {
		subject:  intl.Translation{T: "恢复订单"},
		template: intl.Translation{T: "找到赎回 (%s: %v) 并验证了请求 %s 的秘密"},
	},
	TopicCancellingOrder: {
		subject:  intl.Translation{T: "取消订单"},
		template: intl.Translation{T: "已为订单 %s 提交了取消操作"}, // alt. 已为订单 %s 提交取消订单
	},
	TopicOrderStatusUpdate: {
		subject:  intl.Translation{T: "订单状态更新"},
		template: intl.Translation{T: "订单 %v 的状态从 %v 修改为 %v"}, // alt. 订单状态 %v 从 %v 修改为 %v
	},
	TopicMatchResolutionError: {
		subject:  intl.Translation{T: "订单解析错误"},
		template: intl.Translation{T: "没有为 %[3]s 找到为 %[2]s 报告的 %[1]d 个匹配项。请联系Decred社区以解决该问题。"}, // alt. %s 报告的 %d 个匹配项没有找到 %s。
	},
	TopicFailedCancel: {
		subject:  intl.Translation{T: "取消失败"},
		template: intl.Translation{T: "订单 %s 的取消请求失败，现已删除。订单 %s 的取消请求失败，现已删除。"}, // alt.   取消订单 %s 的订单 %s 处于 Epoque 状态 2 个 epoques，现在已被删除。
	},
	TopicAuditTrouble: {
		subject:  intl.Translation{T: "审计时的问题"},
		template: intl.Translation{T: "继续寻找组合 %[3]s 的货币 %[1]v (%[2]s) 的交易对手合约。您的互联网和钱包连接是否正常？"},
	},
	TopicDexAuthError: {
		subject:  intl.Translation{T: "身份验证错误"},
		template: intl.Translation{T: "%s: %v"},
	},
	TopicUnknownOrders: {
		subject:  intl.Translation{T: "DEX 报告的未知请求"},
		template: intl.Translation{T: "未找到 DEX %[2]s 报告的 %[1]d 个活动订单。"},
	},
	TopicOrdersReconciled: {
		subject:  intl.Translation{T: "与 DEX 协调的订单"},
		template: intl.Translation{T: "%d 个订单的更新状态。"}, // alt. %d 个订单的状态已更新。
	},
	TopicWalletConfigurationUpdated: {
		subject:  intl.Translation{T: "更新的钱包设置a"},
		template: intl.Translation{T: "钱包 %[1]s 的配置已更新。存款地址 = %[2]s"}, // alt. %s 钱包的配置已更新。存款地址 = %s
	},
	TopicWalletPasswordUpdated: {
		subject:  intl.Translation{T: "钱包密码更新"},
		template: intl.Translation{T: "钱包 %s 的密码已更新。"}, // alt. %s 钱包的密码已更新。
	},
	TopicMarketSuspendScheduled: {
		subject:  intl.Translation{T: "市场暂停预定"},
		template: intl.Translation{T: "%s 上的市场 %s 现在计划在 %v 暂停"},
	},
	TopicMarketSuspended: {
		subject:  intl.Translation{T: "暂停市场"},
		template: intl.Translation{T: "%s 的 %s 市场交易现已暂停。"}, // alt. %s 市场 %s 的交易现已暂停。
	},
	TopicMarketSuspendedWithPurge: {
		subject:  intl.Translation{T: "暂停市场，清除订单"},
		template: intl.Translation{T: "%s 的市场交易 %s 现已暂停。订单簿中的所有订单现已被删除。"}, // alt. %s 市场 %s 的交易现已暂停。所有预订的订单现在都已清除。
	},
	TopicMarketResumeScheduled: {
		subject:  intl.Translation{T: "预定市场摘要"},
		template: intl.Translation{T: "%s 上的市场 %s 现在计划在 %v 恢"},
	},
	TopicMarketResumed: {
		subject:  intl.Translation{T: "总结市场"},
		template: intl.Translation{T: "%[2]s 上的市场 %[1]s 已汇总用于时代 %[3]d 中的交易"}, // alt. M%s 的市场 %s 已在epoch %d 恢复交易
	},
	TopicUpgradeNeeded: {
		subject:  intl.Translation{T: "需要更新"},
		template: intl.Translation{T: "您可能需要更新您的帐户以进行 %s 的交易。"}, // alt. 您可能需要更新您的客户端以在 %s 进行交易。
	},
	TopicDEXConnected: {
		subject:  intl.Translation{T: "DEX 连接"},
		template: intl.Translation{T: "%s 已连接"},
	},
	TopicDEXDisconnected: {
		subject:  intl.Translation{T: "服务器断开连接"},
		template: intl.Translation{T: "%s 离线"}, // alt. %s 已断开连接
	},
	TopicPenalized: {
		subject:  intl.Translation{T: "服务器惩罚了你"},
		template: intl.Translation{T: "%s 上的 DEX 惩罚\n最后一条规则被破坏：%s \n时间： %v \n详细信息：\n \" %s \" \n"},
	},
	TopicSeedNeedsSaving: {
		subject:  intl.Translation{T: "不要忘记备份你的应用程序种子"}, // alt. 别忘了备份应用程序种子
		template: intl.Translation{T: "已创建新的应用程序种子。请立刻在设置界面中进行备份。"},
	},
	TopicUpgradedToSeed: {
		subject:  intl.Translation{T: "备份您的新应用程序种子"},                   // alt. 备份新的应用程序种子
		template: intl.Translation{T: "客户端已升级为使用应用程序种子。请切换至设置界面备份种子。"}, // alt. 客户端已升级。请在“设置”界面中备份种子。
	},
	TopicDEXNotification: {
		subject:  intl.Translation{T: "来自DEX的消息"},
		template: intl.Translation{T: "%s: %s"},
	},
	// Inserted
	TopicDexConnectivity: {
		subject:  intl.Translation{T: "互联网连接"},
		template: intl.Translation{T: "您与 %s 的互联网连接不稳定，请检查您的网络连接", Notes: "args: [host]"},
	},
	TopicWalletPeersWarning: {
		subject:  intl.Translation{T: "钱包网络问题"},
		template: intl.Translation{T: "%v 钱包没有网络对等节点！", Notes: "args: [asset name]"},
	},
	TopicAsyncOrderFailure: {
		subject:  intl.Translation{T: "订单处理错误"},
		template: intl.Translation{T: "ID 为 %v 的正在处理订单失败：%v", Notes: "args: order ID, error]"},
	},
	TopicWalletCommsWarning: {
		subject:  intl.Translation{T: "钱包连接问题"},
		template: intl.Translation{T: "无法与 %v 钱包通信！原因：%v", Notes: "args: [asset name, error message]"},
	},
	TopicBondWalletNotConnected: {
		subject:  intl.Translation{T: "债券钱包未连接"},
		template: intl.Translation{T: "所选债券资产 %s 的钱包未连接"},
	},
	TopicOrderQuantityTooHigh: {
		subject:  intl.Translation{T: "超出交易限制"},
		template: intl.Translation{T: "订单数量超出 %s 当前的交易限制", Notes: "args: [host]"},
	},
	TopicWalletPeersRestored: {
		subject:  intl.Translation{T: "钱包连接已恢复"},
		template: intl.Translation{T: "%v 钱包已重新建立连接。", Notes: "args: [asset name]"},
	},
	// End Inserted
	// START NEW
	TopicQueuedCreationFailed: {
		subject:  intl.Translation{T: "创建代币钱包失败"},
		template: intl.Translation{T: "成功创建 %s 钱包后，创建 %s 钱包失败", Notes: "args: [parentSymbol, tokenSymbol]"},
	},
	TopicRedemptionResubmitted: {
		subject:  intl.Translation{T: "赎回请求已重新提交"},
		template: intl.Translation{T: "您在订单 %s 中的匹配 %s 赎回请求已重新提交"},
	},
	TopicSwapRefunded: {
		subject:  intl.Translation{T: "兑换已退款"},
		template: intl.Translation{T: "订单 %s 中的匹配 %s 已被对方退款"},
	},
	TopicRedemptionConfirmed: {
		subject:  intl.Translation{T: "赎回已确认"},
		template: intl.Translation{T: "您在订单 %s 中的匹配 %s 赎回请求已确认"},
	},
	TopicWalletTypeDeprecated: {
		subject:  intl.Translation{T: "钱包已禁用"},
		template: intl.Translation{T: "您的 %s 钱包类型不再受支持，请创建一个新钱包。"},
	},
	TopicOrderResumeFailure: {
		subject:  intl.Translation{T: "恢复订单失败"},
		template: intl.Translation{T: "恢复交易处理失败：%v"},
	},
	TopicBondConfirming: {
		subject:  intl.Translation{T: "正在确认债券"},
		template: intl.Translation{T: "正在等待 %d 个确认，以将债券 %v (%s) 发布到 %s", Notes: "args: [reqConfs, bondCoinStr, assetID, acct.host]"},
	},
	TopicBondConfirmed: {
		subject:  intl.Translation{T: "债券已确认"},
		template: intl.Translation{T: "新等级 = %d（目标等级 = %d）", Notes: "args: [effectiveTier, targetTier]"},
	},
	TopicBondExpired: {
		subject:  intl.Translation{T: "债券已过期"},
		template: intl.Translation{T: "新等级 = %d (目标 = %d)。", Notes: "args: [effectiveTier, targetTier]"},
	},
	TopicBondRefunded: {
		subject:  intl.Translation{T: "债券已退款"},
		template: intl.Translation{T: "债券 %v 为 %v 已退款，扣除交易费后追回 %v 的 %v", Notes: "args: [bondIDStr, acct.host, refundCoinStr, refundVal, Amount]"},
	},
	TopicBondPostError: {
		subject:  intl.Translation{T: "债券发布错误"},
		template: intl.Translation{T: "保税后 请求错误（将重试）：%v (%T)", Notes: "args: [err, err]"},
	},
	TopicBondPostErrorConfirm: {
		subject:  intl.Translation{T: "债券发布错误"},
		template: intl.Translation{T: "在等待 %s 的债券确认时遇到错误：%v"},
	},
	TopicDexAuthErrorBond: {
		subject:  intl.Translation{T: "身份验证错误"},
		template: intl.Translation{T: "债券已确认，但连接验证失败：%v", Notes: "args: [err]"},
	},
	TopicAccountRegTier: {
		subject:  intl.Translation{T: "账户已注册"},
		template: intl.Translation{T: "新等级 = %d", Notes: "args: [effectiveTier]"},
	},
	TopicUnknownBondTierZero: {
		subject: intl.Translation{T: "发现未知债券"},
		template: intl.Translation{
			T:     "发现未知的 %s 债券并已添加到活跃债券中，但在 %s 的 DEX 上您的目标等级为零。请在设置中设置您的目标等级，以便保持债券并启用自动续期。",
			Notes: "args: [bond asset, dex host]",
		},
	},
}

var plPL = map[Topic]*translation{
	TopicAccountRegistered: {
		subject:  intl.Translation{T: "Konto zarejestrowane"},
		template: intl.Translation{T: "Możesz teraz handlować na %s"},
	},
	TopicFeePaymentInProgress: {
		subject:  intl.Translation{T: "Opłata rejestracyjna w drodze"},
		template: intl.Translation{T: "Oczekiwanie na %d potwierdzeń przed rozpoczęciem handlu na %s"},
	},
	TopicRegUpdate: {
		subject:  intl.Translation{T: "Aktualizacja rejestracji"},
		template: intl.Translation{T: "Potwierdzenia opłaty rejestracyjnej %v/%v"},
	},
	TopicFeePaymentError: {
		subject:  intl.Translation{T: "Błąd płatności rejestracyjnej"},
		template: intl.Translation{T: "Wystąpił błąd przy płatności dla %s: %v"},
	},
	TopicAccountUnlockError: {
		subject:  intl.Translation{T: "Błąd odblokowywania konta"},
		template: intl.Translation{T: "błąd odblokowywania konta dla %s: %v"},
	},
	TopicFeeCoinError: {
		subject:  intl.Translation{T: "Błąd w płatności rejestracyjnej"},
		template: intl.Translation{T: "Nie znaleziono środków na płatność rejestracyjną dla %s."},
	},
	TopicWalletConnectionWarning: {
		subject:  intl.Translation{T: "Ostrzeżenie połączenia z portfelem"},
		template: intl.Translation{T: "Wykryto niedokończoną rejestrację dla %s, ale nie można połączyć się z portfelem Decred"},
	},
	TopicWalletUnlockError: {
		subject:  intl.Translation{T: "Błąd odblokowywania portfela"},
		template: intl.Translation{T: "Połączono z portfelem Decred, aby dokończyć rejestrację na %s, lecz próba odblokowania portfela nie powiodła się: %v"},
	},
	TopicSendError: {
		subject:  intl.Translation{T: "Błąd wypłaty środków"},
		template: intl.Translation{Version: 1, T: "Napotkano błąd przy wysyłaniu %s: %v"},
	},
	TopicSendSuccess: {
		subject:  intl.Translation{T: "Wypłata zrealizowana"},
		template: intl.Translation{Version: 1, T: "Wysyłka %s %s na adres %s została zakończona. Tx ID = %s"},
	},
	TopicOrderLoadFailure: {
		subject:  intl.Translation{T: "Błąd wczytywania zleceń"},
		template: intl.Translation{T: "Niektórych zleceń nie udało się wczytać z bazy danych: %v"},
	},
	TopicYoloPlaced: {
		subject:  intl.Translation{T: "Złożono zlecenie rynkowe"},
		template: intl.Translation{T: "sprzedaż %s %s po kursie rynkowym (%s)"},
	},
	TopicBuyOrderPlaced: {
		subject:  intl.Translation{T: "Złożono zlecenie"},
		template: intl.Translation{Version: 1, T: "Kupno %s %s, kurs = %s (%s)"},
	},
	TopicSellOrderPlaced: {
		subject:  intl.Translation{T: "Złożono zlecenie"},
		template: intl.Translation{Version: 1, T: "Sprzedaż %s %s, kurs = %s (%s)"},
	},
	TopicMissingMatches: {
		subject:  intl.Translation{T: "Brak spasowanych zamówień"},
		template: intl.Translation{T: "%d spasowań dla zlecenia %s nie zostało odnotowanych przez %q i są uznane za unieważnione"},
	},
	TopicWalletMissing: {
		subject:  intl.Translation{T: "Brak portfela"},
		template: intl.Translation{T: "Błąd odczytu z portfela dla aktywnego zlecenia %s: %v"},
	},
	TopicMatchErrorCoin: {
		subject:  intl.Translation{T: "Błąd spasowanej monety"},
		template: intl.Translation{T: "Spasowanie %s dla zlecenia %s jest w stanie %s, lecz brakuje monety po stronie maker."},
	},
	TopicMatchErrorContract: {
		subject:  intl.Translation{T: "Błąd kontraktu spasowania"},
		template: intl.Translation{T: "Spasowanie %s dla zlecenia %s jest w stanie %s, lecz brakuje kontraktu zamiany po stronie maker."},
	},
	TopicMatchRecoveryError: {
		subject:  intl.Translation{T: "Błąd odzyskiwania spasowania"},
		template: intl.Translation{T: "Błąd przy audycie kontraktu zamiany u kontrahenta (%s %v) podczas odzyskiwania zamiany dla zlecenia %s: %v"},
	},
	TopicOrderCoinError: {
		subject:  intl.Translation{T: "Błąd monety dla zlecenia"},
		template: intl.Translation{T: "Nie znaleziono środków fundujących dla aktywnego zlecenia %s"},
	},
	TopicOrderCoinFetchError: {
		subject:  intl.Translation{T: "Błąd pozyskania środków dla zlecenia"},
		template: intl.Translation{T: "Błąd pozyskania środków źródłowych dla zlecenia %s (%s): %v"},
	},
	TopicMissedCancel: {
		subject:  intl.Translation{T: "Spóźniona anulacja"},
		template: intl.Translation{T: "Zlecenie anulacji nie zostało spasowane dla zlecenia %s. Może to mieć miejsce, gdy zlecenie anulacji wysłane jest w tej samej epoce, co zlecenie handlu, lub gdy zlecenie handlu zostaje w pełni wykonane przed spasowaniem ze zleceniem anulacji."},
	},
	TopicBuyOrderCanceled: {
		subject:  intl.Translation{T: "Zlecenie anulowane"},
		template: intl.Translation{Version: 1, T: "Zlecenie kupna na parze %s-%s po kursie %s zostało anulowane (%s)"},
	},
	TopicSellOrderCanceled: {
		subject:  intl.Translation{T: "Zlecenie anulowane"},
		template: intl.Translation{Version: 1, T: "Zlecenie sprzedaży na parze %s-%s po kursie %s zostało anulowane (%s)"},
	},
	TopicSellMatchesMade: {
		subject:  intl.Translation{T: "Dokonano spasowania"},
		template: intl.Translation{Version: 1, T: "Zlecenie sprzedaży na parze %s-%s wypełnione w %.1f%% (%s)"},
	},
	TopicBuyMatchesMade: {
		subject:  intl.Translation{T: "Dokonano spasowania"},
		template: intl.Translation{Version: 1, T: "Zlecenie kupna na parze %s-%s wypełnione w %.1f%% (%s)"},
	},
	TopicSwapSendError: {
		subject:  intl.Translation{T: "Błąd wysyłki środków"},
		template: intl.Translation{T: "Błąd przy wysyłaniu środków wartych %s %s dla zlecenia %s"},
	},
	TopicInitError: {
		subject:  intl.Translation{T: "Błąd raportowania zamiany"},
		template: intl.Translation{T: "Błąd powiadomienia DEX o zamianie dla spasowania %s: %v"},
	},
	TopicReportRedeemError: {
		subject:  intl.Translation{T: "Błąd raportowania wykupienia"},
		template: intl.Translation{T: "Błąd powiadomienia DEX o wykupieniu środków dla spasowania %s: %v"},
	},
	TopicSwapsInitiated: {
		subject:  intl.Translation{T: "Zamiana rozpoczęta"},
		template: intl.Translation{T: "Wysłano środki o wartości %s %s dla zlecenia %s"},
	},
	TopicRedemptionError: {
		subject:  intl.Translation{T: "Błąd wykupienia"},
		template: intl.Translation{T: "Napotkano błąd przy wykupywaniu środków o wartości %s %s dla zlecenia %s"},
	},
	TopicMatchComplete: {
		subject:  intl.Translation{T: "Spasowanie zakończone"},
		template: intl.Translation{T: "Wykupiono %s %s ze zlecenia %s"},
	},
	TopicRefundFailure: {
		subject:  intl.Translation{T: "Niepowodzenie zwrotu środków"},
		template: intl.Translation{T: "Zwrócono %s %s za zlecenie %s, z pewnymi błędami"},
	},
	TopicMatchesRefunded: {
		subject:  intl.Translation{T: "Zwrot środków za spasowanie zleceń"},
		template: intl.Translation{T: "Zwrócono %s %s za zlecenie %s"},
	},
	TopicMatchRevoked: {
		subject:  intl.Translation{T: "Spasowanie zleceń unieważnione"},
		template: intl.Translation{T: "Spasowanie %s zostało unieważnione"},
	},
	TopicOrderRevoked: {
		subject:  intl.Translation{T: "Zlecenie unieważnione"},
		template: intl.Translation{T: "Zlecenie %s na rynku %s na %s zostało unieważnione przez serwer"},
	},
	TopicOrderAutoRevoked: {
		subject:  intl.Translation{T: "Zlecenie unieważnione automatycznie"},
		template: intl.Translation{T: "Zlecenie %s na rynku %s na %s zostało unieważnione z powodu wstrzymania handlu na tym rynku"},
	},
	TopicMatchRecovered: {
		subject:  intl.Translation{T: "Odzyskano spasowanie"},
		template: intl.Translation{T: "Odnaleziono wykup ze strony maker (%s: %v) oraz potwierdzono sekret dla spasowania %s"},
	},
	TopicCancellingOrder: {
		subject:  intl.Translation{T: "Anulowanie zlecenia"},
		template: intl.Translation{T: "Złożono polecenie anulowania dla zlecenia %s"},
	},
	TopicOrderStatusUpdate: {
		subject:  intl.Translation{T: "Aktualizacja statusu zlecenia"},
		template: intl.Translation{T: "Status zlecenia %v został zmieniony z %v na %v"},
	},
	TopicMatchResolutionError: {
		subject:  intl.Translation{T: "Błąd rozstrzygnięcia spasowania"},
		template: intl.Translation{T: "Nie znaleziono %d spasowań odnotowanych przez %s dla %s."},
	},
	TopicFailedCancel: {
		subject:  intl.Translation{T: "Niepowodzenie anulowania"},
		template: intl.Translation{Version: 1, T: "Polecenie anulacji dla zamówienia %s nie powiodło się i zostało usunięte."},
	},
	TopicAuditTrouble: {
		subject:  intl.Translation{T: "Problem z audytem"},
		template: intl.Translation{T: "Wciąż szukamy monety kontraktowej kontrahenta %v (%s) dla spasowania %s. Czy Twoje połączenie z Internetem i portfelem jest dobre?"},
	},
	TopicDexAuthError: {
		subject:  intl.Translation{T: "Błąd uwierzytelniania DEX"},
		template: intl.Translation{T: "%s: %v"},
	},
	TopicUnknownOrders: {
		subject:  intl.Translation{T: "DEX odnotował nieznane zlecenia"},
		template: intl.Translation{T: "Nie znaleziono %d aktywnych zleceń odnotowanych przez DEX %s."},
	},
	TopicOrdersReconciled: {
		subject:  intl.Translation{T: "Pogodzono zlecenia z DEX"},
		template: intl.Translation{T: "Zaktualizowano statusy dla %d zleceń."},
	},
	TopicWalletConfigurationUpdated: {
		subject:  intl.Translation{T: "Zaktualizowano konfigurację portfela"},
		template: intl.Translation{T: "Konfiguracja dla portfela %s została zaktualizowana. Adres do depozytów = %s"},
	},
	TopicWalletPasswordUpdated: {
		subject:  intl.Translation{T: "Zaktualizowano hasło portfela"},
		template: intl.Translation{T: "Hasło dla portfela %s zostało zaktualizowane."},
	},
	TopicMarketSuspendScheduled: {
		subject:  intl.Translation{T: "Planowane zawieszenie rynku"},
		template: intl.Translation{T: "Rynek %s na %s zostanie wstrzymany o %v"},
	},
	TopicMarketSuspended: {
		subject:  intl.Translation{T: "Rynek wstrzymany"},
		template: intl.Translation{T: "Handel na rynku %s na %s jest obecnie wstrzymany."},
	},
	TopicMarketSuspendedWithPurge: {
		subject:  intl.Translation{T: "Rynek wstrzymany, księga zamówień wyczyszczona"},
		template: intl.Translation{T: "Handel na rynku %s na %s jest obecnie wstrzymany. Wszystkie złożone zamówienia zostały WYCOFANE."},
	},
	TopicMarketResumeScheduled: {
		subject:  intl.Translation{T: "Planowane wznowienie rynku"},
		template: intl.Translation{T: "Rynek %s na %s zostanie wznowiony o %v"},
	},
	TopicMarketResumed: {
		subject:  intl.Translation{T: "Rynek wznowiony"},
		template: intl.Translation{T: "Rynek %s na %s wznowił handel w epoce %d"},
	},
	TopicUpgradeNeeded: {
		subject:  intl.Translation{T: "Wymagana aktualizacja"},
		template: intl.Translation{T: "Aby handlować na %s wymagana jest aktualizacja klienta."},
	},
	TopicDEXConnected: {
		subject:  intl.Translation{T: "Połączono z serwerem"},
		template: intl.Translation{T: "Połączono z %s"},
	},
	TopicDEXDisconnected: {
		subject:  intl.Translation{T: "Rozłączono z serwerem"},
		template: intl.Translation{T: "Rozłączono z %s"},
	},
	TopicPenalized: {
		subject:  intl.Translation{T: "Serwer ukarał Cię punktami karnymi"},
		template: intl.Translation{T: "Punkty karne od serwera DEX na %s\nostatnia złamana reguła: %s\nczas: %v\nszczegóły:\n\"%s\"\n"},
	},
	TopicSeedNeedsSaving: {
		subject:  intl.Translation{T: "Nie zapomnij zrobić kopii ziarna aplikacji"},
		template: intl.Translation{T: "Utworzono nowe ziarno aplikacji. Zrób jego kopię w zakładce ustawień."},
	},
	TopicUpgradedToSeed: {
		subject:  intl.Translation{T: "Zrób kopię nowego ziarna aplikacji"},
		template: intl.Translation{T: "Klient został zaktualizowany, by korzystać z ziarna aplikacji. Zrób jego kopię w zakładce ustawień."},
	},
	TopicDEXNotification: {
		subject:  intl.Translation{T: "Wiadomość od DEX"},
		template: intl.Translation{T: "%s: %s"},
	},
	TopicBondExpired: {
		subject:  intl.Translation{T: "Kaucja wygasła"},
		template: intl.Translation{T: "Nowy poziom = %d (docelowy = %d)."},
	},
	TopicRedemptionConfirmed: {
		subject:  intl.Translation{T: "Wykup potwierdzony"},
		template: intl.Translation{T: "Twój wykup środków dla sparowania %s w zamówieniu %s został potwierdzony"},
	},
	TopicWalletCommsWarning: {
		subject:  intl.Translation{T: "Problem z połączeniem portfela"},
		template: intl.Translation{T: "Nie można połączyć się z portfelem %v! Powód: %v"},
	},
	TopicAccountRegTier: {
		subject:  intl.Translation{T: "Konto zarejestrowane"},
		template: intl.Translation{T: "Nowy poziom = %d"},
	},
	TopicBondPostError: {
		subject:  intl.Translation{T: "Błąd wpłaty kaucji"},
		template: intl.Translation{T: "błąd wpłaty kaucji (zostanie wykonana ponowna próba): %v (%T)"},
	},
	TopicBondConfirming: {
		subject:  intl.Translation{T: "Potwierdzanie kaucji"},
		template: intl.Translation{T: "Oczekiwanie na %d potwierdzeń do wpłaty kaucji %v (%s) na rzecz %s"},
	},
	TopicQueuedCreationFailed: {
		subject:  intl.Translation{T: "Tworzenie portfela tokenu nie powiodło się"},
		template: intl.Translation{T: "Po utworzeniu portfela %s, utworzenie portfela %s nie powiodło się"},
	},
	TopicWalletPeersRestored: {
		subject:  intl.Translation{T: "Przywrócono łączność z portfelem"},
		template: intl.Translation{T: "Portfel %v odzyskał połączenie."},
	},
	TopicWalletTypeDeprecated: {
		subject:  intl.Translation{T: "Portfel wyłączony"},
		template: intl.Translation{T: "Twój portfel %s nie jest już wspierany. Utwórz nowy portfel."},
	},
	TopicRedemptionResubmitted: {
		subject:  intl.Translation{T: "Wykup środków wysłany ponownie"},
		template: intl.Translation{T: "Twój wykup środków dla sparowania %s został wysłany ponownie."},
	},
	TopicBondRefunded: {
		subject:  intl.Translation{T: "Kaucja zwrócona"},
		template: intl.Translation{T: "Kaucja %v dla %v zrefundowana w %v, zwraca %v z %v po opłatach transakcyjnych"},
	},
	TopicOrderResumeFailure: {
		subject:  intl.Translation{T: "Zlecenie wznowienia nie powiodło się"},
		template: intl.Translation{T: "Wznowienie przetwarzania wymiany nie powiodło się: %"},
	},
	TopicWalletPeersWarning: {
		subject:  intl.Translation{T: "Problem z siecią portfela"},
		template: intl.Translation{T: "Portfel %v nie ma połączeń z resztą sieci (peer)!"},
	},
	TopicDexConnectivity: {
		subject:  intl.Translation{T: "Łączność z Internetem"},
		template: intl.Translation{T: "Twoje połączenie z %s jest niestabilne. Sprawdź połączenie z Internetem"},
	},
	TopicAsyncOrderFailure: {
		subject:  intl.Translation{T: "Błąd składania zamówienia"},
		template: intl.Translation{T: "Składanie zamówienia o ID %v nie powiodło się: %v"},
	},
	TopicUnknownBondTierZero: {
		subject:  intl.Translation{T: "Znaleziono nieznaną kaucję"},
		template: intl.Translation{T: "Znaleziono nieznane kaucje %s i dodano je do aktywnych, ale Twój poziom handlu dla %s wynosi zero. Ustaw poziom docelowy w menu Ustawień, aby podtrzymać kaucje poprzez autoodnawianie."},
	},
	TopicDexAuthErrorBond: {
		subject:  intl.Translation{T: "Błąd uwierzytelnienia"},
		template: intl.Translation{T: "Kaucja została potwierdzona, ale uwierzytelnienie połączenia nie powiodło się: %v"},
	},
	TopicBondConfirmed: {
		subject:  intl.Translation{T: "Kaucja potwierdzona"},
		template: intl.Translation{T: "Nowy poziom = %d (docelowy = %d)."},
	},
	TopicBondPostErrorConfirm: {
		subject:  intl.Translation{T: "Błąd wpłaty kaucji"},
		template: intl.Translation{T: "Napotkano błąd podczas oczekiwania na potwierdzenia kaucji dla %s: %v"},
	},
	TopicBondWalletNotConnected: {
		subject:  intl.Translation{T: "Brak połączenia z portfelem kaucji"},
		template: intl.Translation{T: "Portfel dla wybranej waluty kaucji %s nie jest połączony"},
	},
	TopicOrderQuantityTooHigh: {
		subject:  intl.Translation{T: "Przekroczono limit handlu"},
		template: intl.Translation{T: "Kwota zamówienia przekracza aktualny limit handlowy na %s"},
	},
	TopicSwapRefunded: {
		subject:  intl.Translation{T: "Swap zrefundowany"},
		template: intl.Translation{T: "Sparowanie %s w zamówieniu %s zostało zwrócone przez kontrahenta."},
	},
}

// deDE is the German translations.
var deDE = map[Topic]*translation{
	TopicAccountRegistered: {
		subject:  intl.Translation{T: "Account registeriert"},
		template: intl.Translation{T: "Du kannst nun auf %s handeln"},
	},
	TopicFeePaymentInProgress: {
		subject:  intl.Translation{T: "Abwicklung der Registrationsgebühr"},
		template: intl.Translation{T: "Warten auf %d Bestätigungen bevor mit dem Handel bei %s begonnen werden kann"},
	},
	TopicRegUpdate: {
		subject:  intl.Translation{T: "Aktualisierung der Registration"},
		template: intl.Translation{T: "%v/%v Bestätigungen der Registrationsgebühr"},
	},
	TopicFeePaymentError: {
		subject:  intl.Translation{T: "Fehler bei der Zahlung der Registrationsgebühr"},
		template: intl.Translation{T: "Bei der Zahlung der Registrationsgebühr für %s trat ein Fehler auf: %v"},
	},
	TopicAccountUnlockError: {
		subject:  intl.Translation{T: "Fehler beim Entsperren des Accounts"},
		template: intl.Translation{T: "Fehler beim Entsperren des Accounts für %s: %v"},
	},
	TopicFeeCoinError: {
		subject:  intl.Translation{T: "Coin Fehler bei Registrationsgebühr"},
		template: intl.Translation{T: "Fehlende Coin Angabe für Registrationsgebühr bei %s."},
	},
	TopicWalletConnectionWarning: {
		subject:  intl.Translation{T: "Warnung bei Wallet Verbindung"},
		template: intl.Translation{T: "Unvollständige Registration für %s erkannt, konnte keine Verbindung zum Decred Wallet herstellen"},
	},
	TopicBondWalletNotConnected: {
		subject:  intl.Translation{T: "Kautions-Wallet nicht verbunden"},
		template: intl.Translation{T: "Das Wallet für den ausgewählten Coin %s zur Bezahlung der Kaution ist nicht verbunden"},
	},
	TopicWalletUnlockError: {
		subject:  intl.Translation{T: "Fehler beim Entsperren des Wallet"},
		template: intl.Translation{T: "Verbunden zum Wallet um die Registration bei %s abzuschließen, ein Fehler beim entsperren des Wallet ist aufgetreten: %v"},
	},
	TopicWalletCommsWarning: {
		subject:  intl.Translation{T: "Probleme mit der Verbindung zum Wallet"},
		template: intl.Translation{T: "Kommunikation mit dem %v Wallet nicht möglich! Grund: %q"},
	},
	TopicWalletPeersWarning: {
		subject:  intl.Translation{T: "Problem mit dem Wallet-Netzwerk"},
		template: intl.Translation{T: "%v Wallet hat keine Netzwerk-Peers!"},
	},
	TopicWalletPeersRestored: {
		subject:  intl.Translation{T: "Wallet-Konnektivität wiederhergestellt"},
		template: intl.Translation{T: "Die Verbindung mit dem %v Wallet wurde wiederhergestellt."},
	},
	TopicSendError: {
		subject:  intl.Translation{T: "Sendefehler"},
		template: intl.Translation{T: "Fehler beim senden von %s aufgetreten: %v"},
	},
	TopicSendSuccess: {
		subject:  intl.Translation{T: "Erfolgreich gesendet"},
		template: intl.Translation{T: "Das Senden von %s wurde erfolgreich abgeschlossen. Coin ID = %s"},
	},
	TopicAsyncOrderFailure: {
		subject:  intl.Translation{T: "In-Flight Order Error"},
		template: intl.Translation{T: "In-Flight Auftrag mit ID %v fehlgeschlagen: %v", Notes: "args: order ID, error]"},
	},
	TopicOrderQuantityTooHigh: {
		subject:  intl.Translation{T: "Trade limit exceeded"},
		template: intl.Translation{T: "Auftragsmenge überschreited aktuelles Handelslimit bei %s", Notes: "args: [host]"},
	},
	TopicOrderLoadFailure: {
		subject:  intl.Translation{T: "Fehler beim Laden der Aufträge"},
		template: intl.Translation{T: "Einige Aufträge konnten nicht aus der Datenbank geladen werden: %v"},
	},
	TopicYoloPlaced: {
		subject:  intl.Translation{T: "Marktauftrag platziert"},
		template: intl.Translation{T: "Verkaufe %s %s zum Marktpreis (%s)"},
	},
	TopicBuyOrderPlaced: {
		subject:  intl.Translation{T: "Auftrag platziert"},
		template: intl.Translation{T: "Buying %s %s, Kurs = %s (%s)"},
	},
	TopicSellOrderPlaced: {
		subject:  intl.Translation{T: "Auftrag platziert"},
		template: intl.Translation{T: "Selling %s %s, Kurs = %s (%s)"},
	},
	TopicMissingMatches: {
		subject:  intl.Translation{T: "Fehlende Matches"},
		template: intl.Translation{T: "%d Matches für den Auftrag %s wurden nicht von %q gemeldet und gelten daher als widerrufen"},
	},
	TopicWalletMissing: {
		subject:  intl.Translation{T: "Wallet fehlt"},
		template: intl.Translation{T: "Fehler bei der Wallet-Abfrage für den aktiven Auftrag %s: %v"},
	},
	TopicMatchErrorCoin: {
		subject:  intl.Translation{T: "Fehler beim Coin Match"},
		template: intl.Translation{T: "Match %s für den Auftrag %s hat den Status %s, hat aber keine Coins für den Swap vom Maker gefunden."},
	},
	TopicMatchErrorContract: {
		subject:  intl.Translation{T: "Fehler beim Match Kontrakt"},
		template: intl.Translation{T: "Match %s für Auftrag %s hat den Status %s, hat aber keinen passenden Maker Swap Kontrakt."},
	},
	TopicMatchRecoveryError: {
		subject:  intl.Translation{T: "Fehler bei Match Wiederherstellung"},
		template: intl.Translation{T: "Fehler bei der Prüfung des Swap-Kontrakts der Gegenpartei (%s %v) während der Wiederherstellung des Auftrags %s: %v"},
	},
	TopicOrderCoinError: {
		subject:  intl.Translation{T: "Fehler bei den Coins für einen Auftrag"},
		template: intl.Translation{T: "Keine Coins zur Finanzierung des aktiven Auftrags %s gefunden"},
	},
	TopicOrderCoinFetchError: {
		subject:  intl.Translation{T: "Fehler beim Abruf der Coins für den Auftrag"},
		template: intl.Translation{T: "Beim Abruf der Coins als Quelle für den Auftrag %s (%s) ist ein Fehler aufgetreten: %v"},
	},
	TopicMissedCancel: {
		subject:  intl.Translation{T: "Abbruch verpasst"},
		template: intl.Translation{T: "Der Abbruch passt nicht zum Auftrag %s. Dies kann passieren wenn der Abbruch in der gleichen Epoche wie der Abschluss übermittelt wird oder wenn der Zielauftrag vollständig ausgeführt wird bevor er mit dem Abbruch gematcht werden konnte."},
	},
	TopicBuyOrderCanceled: {
		subject:  intl.Translation{T: "Auftrag abgebrochen"},
		template: intl.Translation{T: "Auftrag für %s-%s bei %s wurde abgebrochen (%s)"},
	},
	TopicSellOrderCanceled: {
		subject:  intl.Translation{T: "Auftrag abgebrochen"},
		template: intl.Translation{T: "Auftrag für %s-%s bei %s wurde abgebrochen (%s)"},
	},
	TopicBuyMatchesMade: {
		subject:  intl.Translation{T: "Matches durchgeführt"},
		template: intl.Translation{T: "Auftrag für %s-%s %.1f%% erfüllt (%s)"},
	},
	TopicSellMatchesMade: {
		subject:  intl.Translation{T: "Matches durchgeführt"},
		template: intl.Translation{T: "Auftrag für %s-%s %.1f%% erfüllt (%s)"},
	},
	TopicSwapSendError: {
		subject:  intl.Translation{T: "Fehler beim Senden des Swaps"},
		template: intl.Translation{T: "Beim Senden des Swap Output(s) im Wert von %s %s für den Auftrag %s"},
	},
	TopicInitError: {
		subject:  intl.Translation{T: "Fehler beim Swap Reporting"},
		template: intl.Translation{T: "Fehler bei der Benachrichtigung des Swaps an den DEX für den Match %s: %v"},
	},
	TopicReportRedeemError: {
		subject:  intl.Translation{T: "Fehler beim Redeem Reporting"},
		template: intl.Translation{T: "Fehler bei der Benachrichtigung des DEX für die Redemption des Match %s: %v"},
	},
	TopicSwapsInitiated: {
		subject:  intl.Translation{T: "Swaps initiiert"},
		template: intl.Translation{T: "Swaps im Wert von %s %s für den Auftrag %s gesendet"},
	},
	TopicRedemptionError: {
		subject:  intl.Translation{T: "Fehler bei der Redemption"},
		template: intl.Translation{T: "Fehler beim Senden von Redemptions im Wert von %s %s für Auftrag %s"},
	},
	TopicMatchComplete: {
		subject:  intl.Translation{T: "Match abgeschlossen"},
		template: intl.Translation{T: "Redeemed %s %s für Auftrag %s"},
	},
	TopicRefundFailure: {
		subject:  intl.Translation{T: "Fehler bei der Erstattung"},
		template: intl.Translation{T: "%s %s für Auftrag %s erstattet, mit einigen Fehlern"},
	},
	TopicMatchesRefunded: {
		subject:  intl.Translation{T: "Matches Erstattet"},
		template: intl.Translation{T: "%s %s für Auftrag %s erstattet"},
	},
	TopicMatchRevoked: {
		subject:  intl.Translation{T: "Match widerrufen"},
		template: intl.Translation{T: "Match %s wurde widerrufen"},
	},
	TopicOrderRevoked: {
		subject:  intl.Translation{T: "Auftrag widerrufen"},
		template: intl.Translation{T: "Der Auftrag %s für den %s Markt bei %s wurde vom Server widerrufen"},
	},
	TopicOrderAutoRevoked: {
		subject:  intl.Translation{T: "Auftrag automatisch widerrufen"},
		template: intl.Translation{T: "Der Auftrag %s für den %s Markt bei %s wurde wegen Aussetzung des Marktes widerrufen"},
	},
	TopicMatchRecovered: {
		subject:  intl.Translation{T: "Match wiederhergestellt"},
		template: intl.Translation{T: "Die Redemption (%s: %v) des Anbieters wurde gefunden und das Geheimnis für Match %s verifiziert"},
	},
	TopicCancellingOrder: {
		subject:  intl.Translation{T: "Auftrag abgebrochen"},
		template: intl.Translation{T: "Der Auftrag %s wurde abgebrochen"},
	},
	TopicOrderStatusUpdate: {
		subject:  intl.Translation{T: "Aktualisierung des Auftragsstatus"},
		template: intl.Translation{T: "Status des Auftrags %v geändert von %v auf %v"},
	},
	TopicMatchResolutionError: {
		subject:  intl.Translation{T: "Fehler bei der Auflösung für Match"},
		template: intl.Translation{T: "%d Matches durch %s gemeldet wurden für %s nicht gefunden."},
	},
	TopicFailedCancel: {
		subject: intl.Translation{T: "Failed cancel"},
		template: intl.Translation{
			Version: 1,
			T:       "Order Abbruch für %s fehlgeschlagen und wurde nun gelöscht.",
			Notes: `args: [token], "failed" means we missed the preimage request ` +
				`and either got the revoke_order message or it stayed in epoch status for too long.`,
		},
	},
	TopicAuditTrouble: {
		subject:  intl.Translation{T: "Audit-Probleme"},
		template: intl.Translation{T: "Immernoch auf der Suche den Coins %v (%s) der Gegenseite für Match %s. Überprüfe deine Internetverbindung und die Verbindungen zum Wallet."},
	},
	TopicDexAuthError: {
		subject:  intl.Translation{T: "DEX-Authentifizierungsfehler"},
		template: intl.Translation{T: "%s: %v"},
	},
	TopicUnknownOrders: {
		subject:  intl.Translation{T: "DEX meldet unbekannte Aufträge"},
		template: intl.Translation{T: "%d aktive Aufträge von DEX %s gemeldet aber konnten nicht gefunden werden."},
	},
	TopicOrdersReconciled: {
		subject:  intl.Translation{T: "Aufträge mit DEX abgestimmt"},
		template: intl.Translation{T: "Der Status für %d Aufträge wurde aktualisiert."},
	},
	TopicWalletConfigurationUpdated: {
		subject:  intl.Translation{T: "Aktualisierung der Wallet Konfiguration"},
		template: intl.Translation{T: "Konfiguration für Wallet %s wurde aktualisiert. Einzahlungsadresse = %s"},
	},
	TopicWalletPasswordUpdated: {
		subject:  intl.Translation{T: "Wallet-Passwort aktualisiert"},
		template: intl.Translation{T: "Passwort für das %s Wallet wurde aktualisiert."},
	},
	TopicMarketSuspendScheduled: {
		subject:  intl.Translation{T: "Aussetzung des Marktes geplant"},
		template: intl.Translation{T: "%s Markt bei %s ist nun für ab %v zur Aussetzung geplant."},
	},
	TopicMarketSuspended: {
		subject:  intl.Translation{T: "Markt ausgesetzt"},
		template: intl.Translation{T: "Der Handel für den %s Markt bei %s ist nun ausgesetzt."},
	},
	TopicMarketSuspendedWithPurge: {
		subject:  intl.Translation{T: "Markt ausgesetzt, Aufträge gelöscht"},
		template: intl.Translation{T: "Der Handel für den %s Markt bei %s ist nun ausgesetzt. Alle gebuchten Aufträge werden jetzt ENTFERNT."},
	},
	TopicMarketResumeScheduled: {
		subject:  intl.Translation{T: "Wiederaufnahme des Marktes geplant"},
		template: intl.Translation{T: "Der %s Markt bei %s wird nun zur Wiederaufnahme ab %v geplant"},
	},
	TopicMarketResumed: {
		subject:  intl.Translation{T: "Markt wiederaufgenommen"},
		template: intl.Translation{T: "Der %s Markt bei %s hat den Handel mit der Epoche %d wieder aufgenommen"},
	},
	TopicUpgradeNeeded: {
		subject:  intl.Translation{T: "Upgrade notwendig"},
		template: intl.Translation{T: "Du musst deinen Klient aktualisieren um bei %s zu Handeln."},
	},
	TopicDEXConnected: {
		subject:  intl.Translation{T: "Server verbunden"},
		template: intl.Translation{T: "Erfolgreich verbunden mit %s"},
	},
	TopicDEXDisconnected: {
		subject:  intl.Translation{T: "Verbindung zum Server getrennt"},
		template: intl.Translation{T: "Verbindung zu %s unterbrochen"},
	},
	TopicDexConnectivity: {
		subject:  intl.Translation{T: "Internet Connectivity"},
		template: intl.Translation{T: "Die Internet Verbindung zu %s ist instabil, Überprüfe deine Internet Verbindung", Notes: "args: [host]"},
	},
	TopicPenalized: {
		subject:  intl.Translation{T: "Bestrafung durch einen Server erhalten"},
		template: intl.Translation{T: "Bestrafung von DEX %s\nletzte gebrochene Regel: %s\nZeitpunkt: %v\nDetails:\n\"%s\"\n"},
	},
	TopicSeedNeedsSaving: {
		subject:  intl.Translation{T: "Vergiss nicht deinen App-Seed zu sichern"},
		template: intl.Translation{T: "Es wurde ein neuer App-Seed erstellt. Erstelle jetzt eine Sicherungskopie in den Einstellungen."},
	},
	TopicUpgradedToSeed: {
		subject:  intl.Translation{T: "Sichere deinen neuen App-Seed"},
		template: intl.Translation{T: "Dein Klient wurde aktualisiert und nutzt nun einen App-Seed. Erstelle jetzt eine Sicherungskopie in den Einstellungen."},
	},
	TopicDEXNotification: {
		subject:  intl.Translation{T: "Nachricht von DEX"},
		template: intl.Translation{T: "%s: %s"},
	},
	TopicQueuedCreationFailed: {
		subject:  intl.Translation{T: "Token-Wallet konnte nicht erstellt werden"},
		template: intl.Translation{T: "Nach dem Erstellen des %s-Wallet kam es zu einen Fehler, konnte das %s-Wallet nicht erstellen"},
	},
	TopicRedemptionResubmitted: {
		subject:  intl.Translation{T: "Redemption Resubmitted"},
		template: intl.Translation{T: "Deine Redemption für Match %s für Order %s wurde neu eingereicht."},
	},
	TopicSwapRefunded: {
		subject:  intl.Translation{T: "Swap Refunded"},
		template: intl.Translation{T: "Match %s für Order %s wurde von der Gegenpartei zurückerstattet."},
	},
	TopicRedemptionConfirmed: {
		subject:  intl.Translation{T: "Redemption Confirmed"},
		template: intl.Translation{T: "Deine Redemption für Match %s für Order %s wurde bestätigt."},
	},
	TopicWalletTypeDeprecated: {
		subject:  intl.Translation{T: "Wallet Disabled"},
		template: intl.Translation{T: "Dein %s Wallet wird nicht länger unterstützt. Erstelle eine neues Wallet."},
	},
	TopicOrderResumeFailure: {
		subject:  intl.Translation{T: "Resume order failure"},
		template: intl.Translation{T: "Wiederaufnahme des Orders nicht möglich: %v"},
	},
	TopicBondConfirming: {
		subject:  intl.Translation{T: "Kauftions-Bestätigung"},
		template: intl.Translation{T: "Warte auf %d Bestätigungen für die Kaution %v (%s) bei %s", Notes: "args: [reqConfs, bondCoinStr, assetID, acct.host]"},
	},
	TopicBondConfirmed: {
		subject:  intl.Translation{T: "Kaution Bestätigt"},
		template: intl.Translation{T: "Neuer Konto-Tier = %d (Ziel = %d).", Notes: "args: [effectiveTier, targetTier]"},
	},
	TopicBondExpired: {
		subject:  intl.Translation{T: "Kaution ausgelaufen"},
		template: intl.Translation{T: "Neuer Konto-Tier = %d (Ziel = %d).", Notes: "args: [effectiveTier, targetTier]"},
	},
	TopicBondRefunded: {
		subject:  intl.Translation{T: "Kaution zurückerstattet"},
		template: intl.Translation{T: "Kaution %v bei %v zurückerstattet in %v, fordere %v als Rückerstattung %v nach Abzug der Transaktionsgebühren", Notes: "args: [bondIDStr, acct.host, refundCoinStr, refundVal, Amount]"},
	},
	TopicBondPostError: {
		subject:  intl.Translation{T: "Bond post error"},
		template: intl.Translation{T: "Fehler beim Einreichen der Kaution (versuche weiter): %v (%T)", Notes: "args: [err, err]"},
	},
	TopicBondPostErrorConfirm: {
		subject:  intl.Translation{T: "Bond post error"},
		template: intl.Translation{T: "Fehler beim warten auf Bestätigungen der Kaution %s: %v"},
	},
	TopicDexAuthErrorBond: {
		subject:  intl.Translation{T: "Authentication error"},
		template: intl.Translation{T: "Kauftion bestätigt, aber Fehler bei Authentifizierung der Verbindung: %v", Notes: "args: [err]"},
	},
	TopicAccountRegTier: {
		subject:  intl.Translation{T: "Account registered"},
		template: intl.Translation{T: "Neurer Konto-Tier = %d", Notes: "args: [effectiveTier]"},
	},
	TopicUnknownBondTierZero: {
		subject: intl.Translation{T: "Unknown bond found"},
		template: intl.Translation{
			T: "Unbekannte %s Kautionen wurden gefunden und den aktiven Bonds hinzugefügt " +
				"aber dein Ziel-Tier bei %s ist auf 0 konfiguriert. Setze deinen " +
				"Ziel-Tier in den Einstellungen um deine Kaution automatisch zu verlängern.",
			Notes: "args: [bond asset, dex host]",
		},
	},
}

// ar is the Arabic translations.
var ar = map[Topic]*translation{
	TopicAccountRegistered: {
		subject:  intl.Translation{T: "تم تسجيل الحساب"},
		template: intl.Translation{T: "يمكنك الآن التداول عند \u200e%s"},
	},
	TopicFeePaymentInProgress: {
		subject:  intl.Translation{T: "جارٍ دفع الرسوم"},
		template: intl.Translation{T: "في انتظار \u200e%d تأكيدات قبل التداول عند \u200e%s"},
	},
	TopicRegUpdate: {
		subject:  intl.Translation{T: "تحديث التسجيل"},
		template: intl.Translation{T: "تأكيدات دفع الرسوم \u200e%v/\u200e%v"},
	},
	TopicFeePaymentError: {
		subject:  intl.Translation{T: "خطأ في دفع الرسوم"},
		template: intl.Translation{T: "عثر على خطأ أثناء دفع الرسوم إلى \u200e%s: \u200e%v"},
	},
	TopicAccountUnlockError: {
		subject:  intl.Translation{T: "خطأ فتح الحساب"},
		template: intl.Translation{T: "خطأ فتح الحساب لأجل \u200e%s: \u200e%v"},
	},
	TopicFeeCoinError: {
		subject:  intl.Translation{T: "خطأ في رسوم العملة"},
		template: intl.Translation{T: "رسوم العملة \u200e%s فارغة."},
	},
	TopicWalletConnectionWarning: {
		subject:  intl.Translation{T: "تحذير اتصال المحفظة"},
		template: intl.Translation{T: "تم الكشف عن تسجيل غير مكتمل لـ  \u200e%s، لكنه فشل في الاتصال بمحفظة ديكريد"},
	},
	TopicWalletUnlockError: {
		subject:  intl.Translation{T: "خطأ في فتح المحفظة"},
		template: intl.Translation{T: "متصل بالمحفظة لإكمال التسجيل عند \u200e%s، لكنه فشل في فتح القفل: \u200e%v"},
	},
	TopicWalletCommsWarning: {
		subject:  intl.Translation{T: "مشكلة الإتصال بالمحفظة"},
		template: intl.Translation{T: "غير قادر على الاتصال بمحفظة !\u200e%v السبب: \u200e%v"},
	},
	TopicWalletPeersWarning: {
		subject:  intl.Translation{T: "مشكلة في شبكة المحفظة"},
		template: intl.Translation{T: "!لا يوجد لدى المحفظة \u200e%v نظراء على الشبكة"},
	},
	TopicWalletPeersRestored: {
		subject:  intl.Translation{T: "تمت استعادة الاتصال بالمحفظة"},
		template: intl.Translation{T: "تمت اعادة الاتصال بالمحفظة \u200e%v."},
	},
	TopicSendError: {
		subject:  intl.Translation{T: "إرسال الخطأ"},
		template: intl.Translation{Version: 1, T: "حدث خطأ أثناء إرسال %s: %v"},
	},
	TopicSendSuccess: {
		subject:  intl.Translation{T: "تم الإرسال بنجاح"},
		template: intl.Translation{Version: 1, T: "تم إرسال %s %s إلى %s بنجاح. معرف المعاملة Tx ID = %s"},
	},
	TopicOrderLoadFailure: {
		subject:  intl.Translation{T: "فشل تحميل الطلب"},
		template: intl.Translation{T: "فشل تحميل بعض الطلبات من قاعدة البيانات: \u200e%v"},
	},
	TopicYoloPlaced: {
		subject:  intl.Translation{T: "تم تقديم طلب السوق"},
		template: intl.Translation{T: "بيع \u200e%s \u200e%s بسعر السوق (\u200e%s)"},
	},
	TopicBuyOrderPlaced: {
		subject:  intl.Translation{T: "تم وضع الطلب"},
		template: intl.Translation{Version: 1, T: "شراء %s %s، السعر = %s (%s)"},
	},
	TopicSellOrderPlaced: {
		subject:  intl.Translation{T: "تم وضع الطلب"},
		template: intl.Translation{Version: 1, T: "بيع %s %s، بمعدل = %s (%s)"},
	},
	TopicMissingMatches: {
		subject:  intl.Translation{T: "مطابقات مفقودة"},
		template: intl.Translation{T: "لم يتم الإبلاغ عن \u200e%d تطابقات للطلب \u200e%s من قبل \u200e%q وتعتبر باطلة"},
	},
	TopicWalletMissing: {
		subject:  intl.Translation{T: "المحفظة مفقودة"},
		template: intl.Translation{T: "خطأ في استرداد المحفظة للطلب النشط \u200e%s: \u200e%v"},
	},
	TopicMatchErrorCoin: {
		subject:  intl.Translation{T: "خطأ في مطابقة العملة"},
		template: intl.Translation{T: "مطابقة \u200e%s للطلب \u200e%s في الحالة \u200e%s، لكن لا توجد عملة مقايضة من الصانع."},
	},
	TopicMatchErrorContract: {
		subject:  intl.Translation{T: "خطأ في عقد المطابقة"},
		template: intl.Translation{T: "تطابق \u200e%s للطلب \u200e%s في الحالة \u200e%s، لكن لا يوجد عقد مقايضة من الصانع."},
	},
	TopicMatchRecoveryError: {
		subject:  intl.Translation{T: "خطأ استعادة المطابقة"},
		template: intl.Translation{T: "خطأ في تدقيق عقد مقايضة الطرف المقابل (\u200e%s \u200e%v) أثناء استرداد المقايضة عند الطلب \u200e%s: \u200e%v"},
	},
	TopicOrderCoinError: {
		subject:  intl.Translation{T: "خطأ عند طلب العملة"},
		template: intl.Translation{T: "لم يتم تسجيل عملات تمويل للطلب النشط \u200e%s"},
	},
	TopicOrderCoinFetchError: {
		subject:  intl.Translation{T: "خطأ في جلب طلب العملة"},
		template: intl.Translation{T: "خطأ في استرداد عملات المصدر للطلب \u200e%s (\u200e%s): \u200e%v"},
	},
	TopicMissedCancel: {
		subject:  intl.Translation{T: "تفويت الإلغاء"},
		template: intl.Translation{T: "طلب الإلغاء لم يتطابق مع الطلب \u200e%s. يمكن أن يحدث هذا إذا تم تقديم طلب الإلغاء في نفس حقبة التداول أو إذا تم تنفيذ الطلب المستهدف بالكامل قبل المطابقة مع طلب الإلغاء."},
	},
	TopicBuyOrderCanceled: {
		subject:  intl.Translation{T: "تم إلغاء الطلب"},
		template: intl.Translation{Version: 1, T: "تم إلغاء (%s) طلب الشراء على %s-%s بسعر %s"},
	},
	TopicSellOrderCanceled: {
		subject:  intl.Translation{T: "تم إلغاء الطلب"},
		template: intl.Translation{Version: 1, T: "تم إلغاء (%s) طلب البيع على %s-%s بسعر %s"},
	},
	TopicBuyMatchesMade: {
		subject:  intl.Translation{T: "تم وضع المطابقات"},
		template: intl.Translation{Version: 1, T: "طلب شراء على %s-%s %.1f%% مُنفّذ بنسبة (%s) "},
	},
	TopicSellMatchesMade: {
		subject:  intl.Translation{T: "تم وضع المطابقات"},
		template: intl.Translation{Version: 1, T: "طلب بيع على %s-%s مُنفّذ بنسبة (%s) "},
	},
	TopicSwapSendError: {
		subject:  intl.Translation{T: "خطأ في إرسال المقايضة"},
		template: intl.Translation{T: "تمت مصادفة خطأ في إرسال إخراج (مخرجات) مقايضة بقيمة \u200e%s \u200e%s عند الطلب \u200e%s"},
	},
	TopicInitError: {
		subject:  intl.Translation{T: "خطأ إبلاغ مقايضة"},
		template: intl.Translation{T: "خطأ إخطار منصة المبادلات اللامركزية بالمقايضة من أجل المطابقة \u200e%s: \u200e%v"},
	},
	TopicReportRedeemError: {
		subject:  intl.Translation{T: "خطأ إبلاغ الإسترداد"},
		template: intl.Translation{T: "خطأ في إعلام منصة المبادلات اللامركزية بالاسترداد من أجل المطابقة \u200e%s: \u200e%v"},
	},
	TopicSwapsInitiated: {
		subject:  intl.Translation{T: "بدأت المقايضات"},
		template: intl.Translation{T: "تم إرسال مقايضات بقيمة \u200e%s \u200e%s عند الطلب \u200e%s"},
	},
	TopicRedemptionError: {
		subject:  intl.Translation{T: "خطأ في الاسترداد"},
		template: intl.Translation{T: "حدث خطأ أثناء إرسال عمليات استرداد بقيمة \u200e%s \u200e%s للطلب \u200e%s"},
	},
	TopicMatchComplete: {
		subject:  intl.Translation{T: "اكتملت المطابقة"},
		template: intl.Translation{T: "تم استرداد \u200e%s \u200e%s عند الطلب \u200e%s"},
	},
	TopicRefundFailure: {
		subject:  intl.Translation{T: "فشل الاسترداد"},
		template: intl.Translation{T: "تم استرداد \u200e%s \u200e%s للطلب \u200e%s، مع وجود بعض الأخطاء"},
	},
	TopicMatchesRefunded: {
		subject:  intl.Translation{T: "تم استرداد مبالغ المطابقات"},
		template: intl.Translation{T: "تم استرداد مبلغ \u200e%s \u200e%s للطلب \u200e%s"},
	},
	TopicMatchRevoked: {
		subject:  intl.Translation{T: "تم إبطال المطابقة"},
		template: intl.Translation{T: "تم إبطال المطابقة \u200e%s"},
	},
	TopicOrderRevoked: {
		subject:  intl.Translation{T: "تم إبطال الطلب"},
		template: intl.Translation{T: "تم إبطال الطلب \u200e%s في السوق \u200e%s عند \u200e%s من قبل الخادم"},
	},
	TopicOrderAutoRevoked: {
		subject:  intl.Translation{T: "تم إبطال الطلب تلقائيًا"},
		template: intl.Translation{T: "تم إبطال الطلب \u200e%s في السوق \u200e%s عند \u200e%s بسبب تعليق عمليات السوق"},
	},
	TopicMatchRecovered: {
		subject:  intl.Translation{T: "تم استرداد المطابقة"},
		template: intl.Translation{T: "تم إيجاد استرداد (\u200e%s: \u200e%v) صانع القيمة وسر المطابقة الذي تم التحقق منه \u200e%s"},
	},
	TopicCancellingOrder: {
		subject:  intl.Translation{T: "إلغاء الطلب"},
		template: intl.Translation{T: "تم إرسال إلغاء طلب للطلب \u200e%s"},
	},
	TopicOrderStatusUpdate: {
		subject:  intl.Translation{T: "تحديث حالة الطلب"},
		template: intl.Translation{T: "تمت مراجعة حالة الطلب \u200e%v من \u200e%v إلى \u200e%v"},
	},
	TopicMatchResolutionError: {
		subject:  intl.Translation{T: "خطأ في دقة المطابقة"},
		template: intl.Translation{T: "لم يتم العثور على \u200e%d تطابقات تم الإبلاغ عنها بواسطة \u200e%s لـ \u200e%s."},
	},
	TopicFailedCancel: {
		subject:  intl.Translation{T: "فشل الإلغاء"},
		template: intl.Translation{Version: 1, T: "فشل إلغاء الطلب للطلب %s وتم حذفه الآن."},
	},
	TopicAuditTrouble: {
		subject:  intl.Translation{T: "مشكلة في التدقيق"},
		template: intl.Translation{T: "لا يزال البحث جارٍ عن عقد عملة \u200e%v (\u200e%s) الطرف الثاني للمطابقة \u200e%s. هل إتصالك بكل من الإنترنت و المحفظة جيد؟"},
	},
	TopicDexAuthError: {
		subject:  intl.Translation{T: "خطأ في مصادقة منصة المبادلات اللامركزية"},
		template: intl.Translation{T: "\u200e%s: \u200e%v"},
	},
	TopicUnknownOrders: {
		subject:  intl.Translation{T: "أبلغت منصة المبادلات اللامركزية عن طلبات غير معروفة"},
		template: intl.Translation{T: "لم يتم العثور على \u200e%d من الطلبات النشطة التي تم الإبلاغ عنها بواسطة منصة المبادلات اللامركزية \u200e%s."},
	},
	TopicOrdersReconciled: {
		subject:  intl.Translation{T: "تمت تسوية الطلبات مع منصة المبادلات اللامركزية"},
		template: intl.Translation{T: "تم تحديث الحالات لـ \u200e%d من الطلبات."},
	},
	TopicWalletConfigurationUpdated: {
		subject:  intl.Translation{T: "تم تحديث تهيئة المحفظة"},
		template: intl.Translation{T: "تم تحديث تهيئة المحفظة \u200e%s. عنوان الإيداع = \u200e%s"},
	},
	TopicWalletPasswordUpdated: {
		subject:  intl.Translation{T: "تم تحديث كلمة مرور المحفظة"},
		template: intl.Translation{T: "تم تحديث كلمة المرور لمحفظة \u200e%s."},
	},
	TopicMarketSuspendScheduled: {
		subject:  intl.Translation{T: "تمت جدولة تعليق السوق"},
		template: intl.Translation{T: "تمت جدولة تعليق \u200e%v السوق \u200e%s عند \u200e%s"},
	},
	TopicMarketSuspended: {
		subject:  intl.Translation{T: "تعليق السوق"},
		template: intl.Translation{T: "تم تعليق التداول في السوق \u200e%s عند \u200e%s."},
	},
	TopicMarketSuspendedWithPurge: {
		subject:  intl.Translation{T: "تم تعليق السوق، وألغيت الطلبات"},
		template: intl.Translation{T: "تم تعليق التداول في السوق \u200e%s عند \u200e%s. كما تمت إزالة جميع الطلبات المحجوزة."},
	},
	TopicMarketResumeScheduled: {
		subject:  intl.Translation{T: "تمت جدولة استئناف السوق"},
		template: intl.Translation{T: "تمت جدولة السوق \u200e%s في \u200e%s للاستئناف في \u200e%v"},
	},
	TopicMarketResumed: {
		subject:  intl.Translation{T: "استئناف السوق"},
		template: intl.Translation{T: "استأنف السوق \u200e%s في \u200e%s التداول في الحقبة الزمنية \u200e%d"},
	},
	TopicUpgradeNeeded: {
		subject:  intl.Translation{T: "التحديث مطلوب"},
		template: intl.Translation{T: "قد تحتاج إلى تحديث عميلك للتداول في \u200e%s."},
	},
	TopicDEXConnected: {
		subject:  intl.Translation{T: "الخادم متصل"},
		template: intl.Translation{T: "\u200e%s متصل"},
	},
	TopicDEXDisconnected: {
		subject:  intl.Translation{T: "قطع الاتصال بالخادم"},
		template: intl.Translation{T: "\u200e%s غير متصل"},
	},
	TopicPenalized: {
		subject:  intl.Translation{T: "لقد عاقبك الخادم"},
		template: intl.Translation{T: "عقوبة من منصة المبادلات اللامركزية في \u200e%s\nآخر قاعدة مكسورة: \u200e%s\nالوقت: \u200e%v\nالتفاصيل:\n\"\u200e%s\"\n"},
	},
	TopicSeedNeedsSaving: {
		subject:  intl.Translation{T: "لا تنس عمل نسخة احتياطية من بذرة التطبيق"},
		template: intl.Translation{T: "تم إنشاء بذرة تطبيق جديدة. قم بعمل نسخة احتياطية الآن في عرض الإعدادات."},
	},
	TopicUpgradedToSeed: {
		subject:  intl.Translation{T: "قم بعمل نسخة احتياطية من بذرة التطبيق الجديدة"},
		template: intl.Translation{T: "تم تحديث العميل لاستخدام بذرة التطبيق. قم بعمل نسخة احتياطية من البذرة الآن في عرض الإعدادات."},
	},
	TopicDEXNotification: {
		subject:  intl.Translation{T: "رسالة من منصة المبادلات اللامركزية"},
		template: intl.Translation{T: "\u200e%s: \u200e%s"},
	},
	TopicQueuedCreationFailed: {
		subject:  intl.Translation{T: "فشل إنشاء توكن المحفظة"},
		template: intl.Translation{T: "بعد إنشاء محفظة \u200e%s، فشل في إنشاء \u200e%s للمحفظة"},
	},
	TopicRedemptionResubmitted: {
		subject:  intl.Translation{T: "أعيد تقديم المبلغ المسترد"},
		template: intl.Translation{T: "تمت إعادة إرسال مبلغك المسترد للمطابقة \u200e%s في الطلب .\u200e%s"},
	},
	TopicSwapRefunded: {
		subject:  intl.Translation{T: "مقايضة مستردة"},
		template: intl.Translation{T: "تمت استعادة تطابق \u200e%s للطلب \u200e%s من قبل الطرف الثاني."},
	},
	TopicRedemptionConfirmed: {
		subject:  intl.Translation{T: "تم تأكيد الاسترداد"},
		template: intl.Translation{T: "تم تأكيد استردادك للمطابقة \u200e%s للطلب \u200e%s"},
	},
	TopicBondPostErrorConfirm: {
		subject:  intl.Translation{T: "خطأ في وظيفة السندات"},
		template: intl.Translation{T: "تم مواجهة خطأ أثناء انتظار تأكيدات السند لـ %s: %v"},
	},
	TopicBondConfirming: {
		subject:  intl.Translation{T: "تأكيد السند"},
		template: intl.Translation{T: "في انتظار %d تأكيدات لنشر السند %v (%s) إلى %s"},
	},
	TopicDexConnectivity: {
		subject:  intl.Translation{T: "الاتصال بالإنترنت"},
		template: intl.Translation{T: "اتصالك بالإنترنت إلى %s غير مستقر، تحقق من اتصالك بالإنترنت"},
	},
	TopicOrderQuantityTooHigh: {
		subject:  intl.Translation{T: "تم تجاوز حدود التداول"},
		template: intl.Translation{T: "كمية الطلب تتجاوز حد التداول الحالي على %s"},
	},
	TopicBondWalletNotConnected: {
		subject:  intl.Translation{T: "محفظة السندات غير متصلة"},
		template: intl.Translation{T: "المحفظة الخاصة بأصل السندات المحدد %s غير متصلة"},
	},
	TopicAccountRegTier: {
		subject:  intl.Translation{T: "حساب مسجل"},
		template: intl.Translation{T: "المستوى الجديد = %d"},
	},
	TopicDexAuthErrorBond: {
		subject:  intl.Translation{T: "خطأ في المصادقة"},
		template: intl.Translation{T: "تم تأكيد السند، ولكن فشل في التحقق من صحة الاتصال: %v"},
	},
	TopicBondPostError: {
		subject:  intl.Translation{T: "خطأ في وظيفة السندات"},
		template: intl.Translation{T: "خطأ في طلب السند (سيتم إعادة المحاولة): %v (%T)"},
	},
	TopicWalletTypeDeprecated: {
		subject:  intl.Translation{T: "المحفظة معطلة"},
		template: intl.Translation{T: "لم يعد نوع محفظتك %s مدعوماً. قم بإنشاء محفظة جديدة."},
	},
	TopicOrderResumeFailure: {
		subject:  intl.Translation{T: "استئناف فشل الطلب"},
		template: intl.Translation{T: "فشل في استئناف معالجة عملية التداول: %v"},
	},
	TopicBondRefunded: {
		subject:  intl.Translation{T: "استرداد السندات"},
		template: intl.Translation{T: "تم استرداد السند %v لـ %v بـ %v، واستعادة %v من %v بعد رسوم المعاملات"},
	},
	TopicBondExpired: {
		subject:  intl.Translation{T: "انتهت صلاحية السند"},
		template: intl.Translation{T: "المستوى الجديد = %d (الهدف = %d)."},
	},
	TopicUnknownBondTierZero: {
		subject:  intl.Translation{T: "تم العثور على سند غير معروف"},
		template: intl.Translation{T: "تم العثور على سندات %s مجهولة وأضيفت إلى السندات النشطة ولكن الفئة المستهدفة الخاصة بك هي صفر لمنصة المبادلات اللامركزية dex في %s. قم بتعيين الفئة المستهدفة في الإعدادات للبقاء مرتبطًا بتجديدات تلقائية."},
	},
	TopicBondConfirmed: {
		subject:  intl.Translation{T: "تم تأكيد السند"},
		template: intl.Translation{T: "الفئة الجديدة = %d (الهدف = %d)."},
	},
	TopicAsyncOrderFailure: {
		subject:  intl.Translation{T: "خطأ في الطلب القيد التنفيذ"},
		template: intl.Translation{T: "فشل: %v الطلب قيد التنفيذ بالمعرف %v"},
	},
}

// The language string key *must* parse with language.Parse.
var locales = map[string]map[Topic]*translation{
	originLang: originLocale,
	"pt-BR":    ptBR,
	"zh-CN":    zhCN,
	"pl-PL":    plPL,
	"de-DE":    deDE,
	"ar":       ar,
}

func init() {
	for lang, translations := range locales {
		langtag, err := language.Parse(lang)
		if err != nil {
			panic(err.Error())
		} // otherwise would fail in core.New parsing the languages
		for topic, translation := range translations {
			err := message.SetString(langtag, string(topic), translation.template.T)
			if err != nil {
				panic(fmt.Sprintf("SetString(%s): %v", lang, err))
			}
		}
	}
}

// RegisterTranslations registers translations with the init package for
// translator worksheet preparation.
func RegisterTranslations() {
	const callerID = "notifications"

	for lang, m := range locales {
		r := intl.NewRegistrar(callerID, lang, len(m)*2)
		for topic, t := range m {
			r.Register(string(topic)+" subject", &t.subject)
			r.Register(string(topic)+" template", &t.template)
		}
	}
}

// CheckTopicLangs is used to report missing notification translations.
func CheckTopicLangs() (missingTranslations int) {
	for topic := range originLocale {
		for _, m := range locales {
			if _, found := m[topic]; !found {
				missingTranslations += len(m)
			}
		}
	}
	return
}
