package core

import (
	"decred.org/dcrdex/client/i18n"
	"golang.org/x/text/language"
)

var translator = i18n.NewPackageTranslator("core", language.AmericanEnglish)

// originLocale is the American English translations.
var originLocale = map[Topic]*i18n.DocumentedTranslation{
	TopicAccountRegistered: {
		Translation: &i18n.Translation{
			Subject:  "Account registered",
			Template: "You may now trade at %s",
		},
		Docs: "[host]",
	},
	TopicFeePaymentInProgress: {
		Translation: &i18n.Translation{
			Subject:  "Fee payment in progress",
			Template: "Waiting for %d confirmations before trading at %s",
		},
		Docs: "[confs, host]",
	},
	TopicRegUpdate: {
		Translation: &i18n.Translation{
			Subject:  "regupdate",
			Template: "Fee payment confirmations %v/%v",
		},
		Docs: "[confs, required confs]",
	},
	TopicFeePaymentError: {
		Translation: &i18n.Translation{
			Subject:  "Fee payment error",
			Template: "Error encountered while paying fees to %s: %v",
		},
		Docs: "[host, error]",
	},
	TopicAccountUnlockError: {
		Translation: &i18n.Translation{
			Subject:  "Account unlock error",
			Template: "error unlocking account for %s: %v",
		},
		Docs: "[host, error]",
	},
	TopicFeeCoinError: {
		Translation: &i18n.Translation{
			Subject:  "Fee coin error",
			Template: "Empty fee coin for %s.",
		},
		Docs: "[host]",
	},
	TopicWalletConnectionWarning: {
		Translation: &i18n.Translation{
			Subject:  "Wallet connection warning",
			Template: "Incomplete registration detected for %s, but failed to connect to the Decred wallet",
		},
		Docs: "[host]",
	},
	TopicWalletUnlockError: {
		Translation: &i18n.Translation{
			Subject:  "Wallet unlock error",
			Template: "Connected to wallet to complete registration at %s, but failed to unlock: %v",
		},
		Docs: "[host, error]",
	},
	TopicWalletCommsWarning: {
		Translation: &i18n.Translation{
			Subject:  "Wallet connection issue",
			Template: "Unable to communicate with %v wallet! Reason: %v",
		},
		Docs: "[asset name, error message]",
	},
	TopicWalletPeersWarning: {
		Translation: &i18n.Translation{
			Subject:  "Wallet network issue",
			Template: "%v wallet has no network peers!",
		},
		Docs: "[asset name]",
	},
	TopicWalletPeersRestored: {
		Translation: &i18n.Translation{
			Subject:  "Wallet connectivity restored",
			Template: "%v wallet has reestablished connectivity.",
		},
		Docs: "[asset name]",
	},
	TopicSendError: {
		Translation: &i18n.Translation{
			Subject:  "Send error",
			Template: "Error encountered while sending %s: %v",
		},
		Docs: "[ticker, error]",
	},
	TopicSendSuccess: {
		Translation: &i18n.Translation{
			Subject:  "Send Successful",
			Template: "Sending %s %s to %s has completed successfully. Coin ID = %s",
		},
		Docs: "[value string, ticker, destination address, coin ID]",
	},
	TopicAsyncOrderFailure: {
		Translation: &i18n.Translation{
			Subject:  "In-Flight Order Error",
			Template: "In-Flight order with ID %v failed: %v",
		},
		Docs: "[order ID, error]",
	},
	TopicOrderLoadFailure: {
		Translation: &i18n.Translation{
			Subject:  "Order load failure",
			Template: "Some orders failed to load from the database: %v",
		},
		Docs: "[error]",
	},
	TopicYoloPlaced: {
		Translation: &i18n.Translation{
			Subject:  "Market order placed",
			Template: "selling %s %s at market rate (%s)",
		},
		Docs: "[qty, ticker, token]",
	},
	TopicBuyOrderPlaced: {
		Translation: &i18n.Translation{
			Subject:  "Order placed",
			Template: "Buying %s %s, rate = %s (%s)",
		},
		Docs: "[qty, ticker, rate string, token]",
	},
	TopicSellOrderPlaced: {
		Translation: &i18n.Translation{
			Subject:  "Order placed",
			Template: "Selling %s %s, rate = %s (%s)",
		},
		Docs: "[qty, ticker, rate string, token]",
	},
	TopicMissingMatches: {
		Translation: &i18n.Translation{
			Subject:  "Missing matches",
			Template: "%d matches for order %s were not reported by %q and are considered revoked",
		},
		Docs: "[missing count, token, host]",
	},
	TopicWalletMissing: {
		Translation: &i18n.Translation{
			Subject:  "Wallet missing",
			Template: "Wallet retrieval error for active order %s: %v",
		},
		Docs: "[token, error]",
	},
	TopicMatchErrorCoin: {
		Translation: &i18n.Translation{
			Subject:  "Match coin error",
			Template: "Match %s for order %s is in state %s, but has no maker swap coin.",
		},
		Docs: "[side, token, match status]",
	},
	TopicMatchErrorContract: {
		Translation: &i18n.Translation{
			Subject:  "Match contract error",
			Template: "Match %s for order %s is in state %s, but has no maker swap contract.",
		},
		Docs: "[side, token, match status]",
	},
	TopicMatchRecoveryError: {
		Translation: &i18n.Translation{
			Subject:  "Match recovery error",
			Template: "Error auditing counter-party's swap contract (%s %v) during swap recovery on order %s: %v",
		},
		Docs: "[ticker, contract, token, error]",
	},
	TopicOrderCoinError: {
		Translation: &i18n.Translation{
			Subject:  "Order coin error",
			Template: "No funding coins recorded for active order %s",
		},
		Docs: "[token]",
	},
	TopicOrderCoinFetchError: {
		Translation: &i18n.Translation{
			Subject:  "Order coin fetch error",
			Template: "Source coins retrieval error for order %s (%s): %v",
		},
		Docs: "[token, ticker, error]",
	},
	TopicMissedCancel: {
		Translation: &i18n.Translation{
			Subject:  "Missed cancel",
			Template: "Cancel order did not match for order %s. This can happen if the cancel order is submitted in the same epoch as the trade or if the target order is fully executed before matching with the cancel order.",
		},
		Docs: "[token]",
	},
	TopicBuyOrderCanceled: {
		Translation: &i18n.Translation{
			Subject:  "Order canceled",
			Template: "Buy order on %s-%s at %s has been canceled (%s)",
		},
		Docs: "[base ticker, quote ticker, host, token]",
	},
	TopicSellOrderCanceled: {
		Translation: &i18n.Translation{
			Subject:  "Order canceled",
			Template: "Sell order on %s-%s at %s has been canceled (%s)",
		},
		Docs: "[base ticker, quote ticker, host, token]",
	},
	TopicBuyMatchesMade: {
		Translation: &i18n.Translation{
			Subject:  "Matches made",
			Template: "Buy order on %s-%s %.1f%% filled (%s)",
		},
		Docs: "[base ticker, quote ticker, fill percent, token]",
	},
	TopicSellMatchesMade: {
		Translation: &i18n.Translation{
			Subject:  "Matches made",
			Template: "Sell order on %s-%s %.1f%% filled (%s)",
		},
		Docs: "[base ticker, quote ticker, fill percent, token]",
	},
	TopicSwapSendError: {
		Translation: &i18n.Translation{
			Subject:  "Swap send error",
			Template: "Error encountered sending a swap output(s) worth %s %s on order %s",
		},
		Docs: "[qty, ticker, token]",
	},
	TopicInitError: {
		Translation: &i18n.Translation{
			Subject:  "Swap reporting error",
			Template: "Error notifying DEX of swap for match %s: %v",
		},
		Docs: "[match, error]",
	},
	TopicReportRedeemError: {
		Translation: &i18n.Translation{
			Subject:  "Redeem reporting error",
			Template: "Error notifying DEX of redemption for match %s: %v",
		},
		Docs: "[match, error]",
	},
	TopicSwapsInitiated: {
		Translation: &i18n.Translation{
			Subject:  "Swaps initiated",
			Template: "Sent swaps worth %s %s on order %s",
		},
		Docs: "[qty, ticker, token]",
	},
	TopicRedemptionError: {
		Translation: &i18n.Translation{
			Subject:  "Redemption error",
			Template: "Error encountered sending redemptions worth %s %s on order %s",
		},
		Docs: "[qty, ticker, token]",
	},
	TopicMatchComplete: {
		Translation: &i18n.Translation{
			Subject:  "Match complete",
			Template: "Redeemed %s %s on order %s",
		},
		Docs: "[qty, ticker, token]",
	},
	TopicRefundFailure: {
		Translation: &i18n.Translation{
			Subject:  "Refund Failure",
			Template: "Refunded %s %s on order %s, with some errors",
		},
		Docs: "[qty, ticker, token]",
	},
	TopicMatchesRefunded: {
		Translation: &i18n.Translation{
			Subject:  "Matches Refunded",
			Template: "Refunded %s %s on order %s",
		},
		Docs: "[qty, ticker, token]",
	},
	TopicMatchRevoked: {
		Translation: &i18n.Translation{
			Subject:  "Match revoked",
			Template: "Match %s has been revoked",
		},
		Docs: "[match ID token]",
	},
	TopicOrderRevoked: {
		Translation: &i18n.Translation{
			Subject:  "Order revoked",
			Template: "Order %s on market %s at %s has been revoked by the server",
		},
		Docs: "[token, market name, host]",
	},
	TopicOrderAutoRevoked: {
		Translation: &i18n.Translation{
			Subject:  "Order auto-revoked",
			Template: "Order %s on market %s at %s revoked due to market suspension",
		},
		Docs: "[token, market name, host]",
	},
	TopicMatchRecovered: {
		Translation: &i18n.Translation{
			Subject:  "Match recovered",
			Template: "Found maker's redemption (%s: %v) and validated secret for match %s",
		},
		Docs: "[ticker, coin ID, match]",
	},
	TopicCancellingOrder: {
		Translation: &i18n.Translation{
			Subject:  "Cancelling order",
			Template: "A cancel order has been submitted for order %s",
		},
		Docs: "[token]",
	},
	TopicOrderStatusUpdate: {
		Translation: &i18n.Translation{
			Subject:  "Order status update",
			Template: "Status of order %v revised from %v to %v",
		},
		Docs: "[token, old status, new status]",
	},
	TopicMatchResolutionError: {
		Translation: &i18n.Translation{
			Subject:  "Match resolution error",
			Template: "%d matches reported by %s were not found for %s.",
		},
		Docs: "[count, host, token]",
	},
	TopicFailedCancel: {
		// NOTE: "failed" means we missed the preimage request and either got
		// the revoke_order message or it stayed in epoch status for too long.
		Translation: &i18n.Translation{
			Subject:  "Failed cancel",
			Template: "Cancel order for order %s stuck in Epoch status for 2 epochs and is now deleted.",
		},
		Docs: "[token]",
	},
	TopicAuditTrouble: {
		Translation: &i18n.Translation{
			Subject:  "Audit trouble",
			Template: "Still searching for counterparty's contract coin %v (%s) for match %s. Are your internet and wallet connections good?",
		},
		Docs: "[coin ID, ticker, match]",
	},
	TopicDexAuthError: {
		Translation: &i18n.Translation{
			Subject:  "DEX auth error",
			Template: "%s: %v",
		},
		Docs: "[host, error]",
	},
	TopicUnknownOrders: {
		Translation: &i18n.Translation{
			Subject:  "DEX reported unknown orders",
			Template: "%d active orders reported by DEX %s were not found.",
		},
		Docs: "[count, host]",
	},
	TopicOrdersReconciled: {
		Translation: &i18n.Translation{
			Subject:  "Orders reconciled with DEX",
			Template: "Statuses updated for %d orders.",
		},
		Docs: "[count]",
	},
	TopicWalletConfigurationUpdated: {
		Translation: &i18n.Translation{
			Subject:  "Wallet configuration updated",
			Template: "Configuration for %s wallet has been updated. Deposit address = %s",
		},
		Docs: "[ticker, address]",
	},
	TopicWalletPasswordUpdated: {
		Translation: &i18n.Translation{
			Subject:  "Wallet Password Updated",
			Template: "Password for %s wallet has been updated.",
		},
		Docs: " [ticker]",
	},
	TopicMarketSuspendScheduled: {
		Translation: &i18n.Translation{
			Subject:  "Market suspend scheduled",
			Template: "Market %s at %s is now scheduled for suspension at %v",
		},
		Docs: "[market name, host, time]",
	},
	TopicMarketSuspended: {
		Translation: &i18n.Translation{
			Subject:  "Market suspended",
			Template: "Trading for market %s at %s is now suspended.",
		},
		Docs: "[market name, host]",
	},
	TopicMarketSuspendedWithPurge: {
		Translation: &i18n.Translation{
			Subject:  "Market suspended, orders purged",
			Template: "Trading for market %s at %s is now suspended. All booked orders are now PURGED.",
		},
		Docs: "[market name, host]",
	},
	TopicMarketResumeScheduled: {
		Translation: &i18n.Translation{
			Subject:  "Market resume scheduled",
			Template: "Market %s at %s is now scheduled for resumption at %v",
		},
		Docs: "[market name, host, time]",
	},
	TopicMarketResumed: {
		Translation: &i18n.Translation{
			Subject:  "Market resumed",
			Template: "Market %s at %s has resumed trading at epoch %d",
		},
		Docs: "[market name, host, epoch]",
	},
	TopicUpgradeNeeded: {
		Translation: &i18n.Translation{
			Subject:  "Upgrade needed",
			Template: "You may need to update your client to trade at %s.",
		},
		Docs: "[host]",
	},
	TopicDEXConnected: {
		Translation: &i18n.Translation{
			Subject:  "Server connected",
			Template: "%s is connected",
		},
		Docs: "[host]",
	},
	TopicDEXDisconnected: {
		Translation: &i18n.Translation{
			Subject:  "Server disconnect",
			Template: "%s is disconnected",
		},
		Docs: "[host]",
	},
	TopicPenalized: {
		Translation: &i18n.Translation{
			Subject:  "Server has penalized you",
			Template: "Penalty from DEX at %s\nlast broken rule: %s\ntime: %v\ndetails:\n\"%s\"\n",
		},
		Docs: "[host, rule, time, details]",
	},
	TopicSeedNeedsSaving: {
		Translation: &i18n.Translation{
			Subject:  "Don't forget to back up your application seed",
			Template: "A new application seed has been created. Make a back up now in the settings view.",
		},
		Docs: "no args",
	},
	TopicUpgradedToSeed: {
		Translation: &i18n.Translation{
			Subject:  "Back up your new application seed",
			Template: "The client has been upgraded to use an application seed. Back up the seed now in the settings view.",
		},
		Docs: "no args",
	},
	TopicDEXNotification: {
		Translation: &i18n.Translation{
			Subject:  "Message from DEX",
			Template: "%s: %s",
		},
		Docs: "[host, msg]",
	},
	TopicQueuedCreationFailed: {
		Translation: &i18n.Translation{
			Subject:  "Failed to create token wallet",
			Template: "After creating %s wallet, failed to create the %s wallet",
		},
		Docs: "[parentSymbol, tokenSymbol]",
	},
	TopicRedemptionResubmitted: {
		Translation: &i18n.Translation{
			Subject:  "Redemption Resubmitted",
			Template: "Your redemption for match %s in order %s was resubmitted.",
		},
		Docs: "[match id, order id]",
	},
	TopicSwapRefunded: {
		Translation: &i18n.Translation{
			Subject:  "Swap Refunded",
			Template: "Match %s in order %s was refunded by the counterparty.",
		},
		Docs: "[match id, order id]",
	},
	TopicRedemptionConfirmed: {
		Translation: &i18n.Translation{
			Subject:  "Redemption Confirmed",
			Template: "Your redemption for match %s in order %s was confirmed",
		},
		Docs: "[match id, order id]",
	},
	TopicWalletTypeDeprecated: {
		Translation: &i18n.Translation{
			Subject:  "Wallet Disabled",
			Template: "Your %s wallet type is no longer supported. Create a new wallet.",
		},
		Docs: "[ticker]",
	},
	TopicOrderResumeFailure: {
		Translation: &i18n.Translation{
			Subject:  "Resume order failure",
			Template: "Failed to resume processing of trade: %v",
		},
		Docs: "[error]",
	},
}

var ptBR = map[Topic]*i18n.Translation{
	// [host]
	TopicAccountRegistered: {
		Subject:  "Conta Registrada",
		Template: "Você agora pode trocar em %s",
	},
	// [confs, host]
	TopicFeePaymentInProgress: {
		Subject:  "Pagamento da Taxa em andamento",
		Template: "Esperando por %d confirmações antes de trocar em %s",
	},
	// [confs, required confs]
	TopicRegUpdate: {
		Subject:  "Atualização de registro",
		Template: "Confirmações da taxa %v/%v",
	},
	// [host, error]
	TopicFeePaymentError: {
		Subject:  "Erro no Pagamento da Taxa",
		Template: "Erro enquanto pagando taxa para %s: %v",
	},
	// [host, error]
	TopicAccountUnlockError: {
		Subject:  "Erro ao Destrancar carteira",
		Template: "erro destrancando conta %s: %v",
	},
	// [host]
	TopicFeeCoinError: {
		Subject:  "Erro na Taxa",
		Template: "Taxa vazia para %s.",
	},
	// [host]
	TopicWalletConnectionWarning: {
		Subject:  "Aviso de Conexão com a Carteira",
		Template: "Registro incompleto detectado para %s, mas falhou ao conectar com carteira decred",
	},
	// [host, error]
	TopicWalletUnlockError: {
		Subject:  "Erro ao Destravar Carteira",
		Template: "Conectado com carteira para completar o registro em %s, mas falha ao destrancar: %v",
	},
	// [ticker, error]
	TopicSendError: {
		Subject:  "Erro Retirada",
		Template: "Erro encontrado durante retirada de %s: %v",
		Stale:    true,
	},
	// [value string, ticker, destination address, coin ID]
	TopicSendSuccess: {
		Template: "Retirada de %s %s (%s) foi completada com sucesso. ID da moeda = %s",
		Subject:  "Retirada Enviada",
		Stale:    true,
	},
	// [error]
	TopicOrderLoadFailure: {
		Template: "Alguns pedidos falharam ao carregar da base de dados: %v",
		Subject:  "Carregamendo de Pedidos Falhou",
	},
	// [qty, ticker, token]
	TopicYoloPlaced: {
		Template: "vendendo %s %s a taxa de mercado (%s)",
		Subject:  "Ordem de Mercado Colocada",
	},
	// [qty, ticker, rate string, token], RETRANSLATE.
	TopicBuyOrderPlaced: {
		Subject:  "Ordem Colocada",
		Template: "Buying %s %s, valor = %s (%s)",
	},
	// [qty, ticker, rate string, token], RETRANSLATE.
	TopicSellOrderPlaced: {
		Subject:  "Ordem Colocada",
		Template: "Selling %s %s, valor = %s (%s)",
	},
	// [missing count, token, host]
	TopicMissingMatches: {
		Template: "%d combinações para pedidos %s não foram reportados por %q e foram considerados revocados",
		Subject:  "Pedidos Faltando Combinações",
	},
	// [token, error]
	TopicWalletMissing: {
		Template: "Erro ao recuperar pedidos ativos por carteira %s: %v",
		Subject:  "Carteira Faltando",
	},
	// [side, token, match status]
	TopicMatchErrorCoin: {
		Subject:  "Erro combinação de Moedas",
		Template: "Combinação %s para pedido %s está no estado %s, mas não há um executador para trocar moedas.",
	},
	// [side, token, match status]
	TopicMatchErrorContract: {
		Template: "Combinação %s para pedido %s está no estado %s, mas não há um executador para trocar moedas.",
		Subject:  "Erro na Combinação de Contrato",
	},
	// [ticker, contract, token, error]
	TopicMatchRecoveryError: {
		Template: "Erro auditando contrato de troca da contraparte (%s %v) durante troca recuperado no pedido %s: %v",
		Subject:  "Erro Recuperando Combinações",
	},
	// [token]
	TopicOrderCoinError: {
		Template: "Não há Moedas de financiamento registradas para pedidos ativos %s",
		Subject:  "Erro no Pedido da Moeda",
	},
	// [token, ticker, error]
	TopicOrderCoinFetchError: {
		Template: "Erro ao recuperar moedas de origem para pedido %s (%s): %v",
		Subject:  "Erro na Recuperação do Pedido de Moedas",
	},
	// [token]
	TopicMissedCancel: {
		Template: "Pedido de cancelamento não combinou para pedido %s. Isto pode acontecer se o pedido de cancelamento foi enviado no mesmo epoque do que a troca ou se o pedido foi completamente executado antes da ordem de cancelamento ser executada.",
		Subject:  "Cancelamento Perdido",
	},
	// [base ticker, quote ticker, host, token], RETRANSLATE.
	TopicSellOrderCanceled: {
		Template: "Sell pedido sobre %s-%s em %s foi cancelado (%s)",
		Subject:  "Cancelamento de Pedido",
	},
	// [base ticker, quote ticker, host, token], RETRANSLATE.
	TopicBuyOrderCanceled: {
		Template: "Buy pedido sobre %s-%s em %s foi cancelado (%s)",
		Subject:  "Cancelamento de Pedido",
	},
	// [base ticker, quote ticker, fill percent, token], RETRANSLATE.
	TopicSellMatchesMade: {
		Template: "Sell pedido sobre %s-%s %.1f%% preenchido (%s)",
		Subject:  "Combinações Feitas",
	},
	// [base ticker, quote ticker, fill percent, token], RETRANSLATE.
	TopicBuyMatchesMade: {
		Template: "Buy pedido sobre %s-%s %.1f%% preenchido (%s)",
		Subject:  "Combinações Feitas",
	},
	// [qty, ticker, token]
	TopicSwapSendError: {
		Template: "Erro encontrado ao enviar a troca com output(s) no valor de %s %s no pedido %s",
		Subject:  "Erro ao Enviar Troca",
	},
	// [match, error]
	TopicInitError: {
		Template: "Erro notificando DEX da troca %s por combinação: %v",
		Subject:  "Erro na Troca",
	},
	// [match, error]
	TopicReportRedeemError: {
		Template: "Erro notificando DEX da redenção %s por combinação: %v",
		Subject:  "Reportando Erro na redenção",
	},
	// [qty, ticker, token]
	TopicSwapsInitiated: {
		Template: "Enviar trocas no valor de %s %s no pedido %s",
		Subject:  "Trocas Iniciadas",
	},
	// [qty, ticker, token]
	TopicRedemptionError: {
		Template: "Erro encontrado enviado redenção no valor de %s %s no pedido %s",
		Subject:  "Erro na Redenção",
	},
	// [qty, ticker, token]
	TopicMatchComplete: {
		Template: "Resgatado %s %s no pedido %s",
		Subject:  "Combinação Completa",
	},
	// [qty, ticker, token]
	TopicRefundFailure: {
		Template: "Devolvidos %s %s no pedido %s, com algum erro",
		Subject:  "Erro no Reembolso",
	},
	// [qty, ticker, token]
	TopicMatchesRefunded: {
		Template: "Devolvidos %s %s no pedido %s",
		Subject:  "Reembolso Sucedido",
	},
	// [match ID token]
	TopicMatchRevoked: {
		Template: "Combinação %s foi revocada",
		Subject:  "Combinação Revocada",
	},
	// [token, market name, host]
	TopicOrderRevoked: {
		Template: "Pedido %s no mercado %s em %s foi revocado pelo servidor",
		Subject:  "Pedido Revocado",
	},
	// [token, market name, host]
	TopicOrderAutoRevoked: {
		Template: "Pedido %s no mercado %s em %s revocado por suspenção do mercado",
		Subject:  "Pedido Revocado Automatiamente",
	},
	// [ticker, coin ID, match]
	TopicMatchRecovered: {
		Template: "Encontrado redenção do executador (%s: %v) e validado segredo para pedido %s",
		Subject:  "Pedido Recuperado",
	},
	// [token]
	TopicCancellingOrder: {
		Template: "Uma ordem de cancelamento foi submetida para o pedido %s",
		Subject:  "Cancelando Pedido",
	},
	// [token, old status, new status]
	TopicOrderStatusUpdate: {
		Template: "Status do pedido %v revisado de %v para %v",
		Subject:  "Status do Pedido Atualizado",
	},
	// [count, host, token]
	TopicMatchResolutionError: {
		Template: "%d combinações reportada para %s não foram encontradas para %s.",
		Subject:  "Erro na Resolução do Pedido",
	},
	// [token]
	TopicFailedCancel: {
		Template: "Ordem de cancelamento para pedido %s presa em estado de Epoque por 2 epoques e foi agora deletado.",
		Subject:  "Falhou Cancelamento",
		Stale:    true,
	},
	// [coin ID, ticker, match]
	TopicAuditTrouble: {
		Template: "Continua procurando por contrato de contrapartes para moeda %v (%s) para combinação %s. Sua internet e conexão com a carteira estão ok?",
		Subject:  "Problemas ao Auditar",
	},
	// [host, error]
	TopicDexAuthError: {
		Template: "%s: %v",
		Subject:  "Erro na Autenticação",
	},
	// [count, host]
	TopicUnknownOrders: {
		Template: "%d pedidos ativos reportados pela DEX %s não foram encontrados.",
		Subject:  "DEX Reportou Pedidos Desconhecidos",
	},
	// [count]
	TopicOrdersReconciled: {
		Template: "Estados atualizados para %d pedidos.",
		Subject:  "Pedidos Reconciliados com DEX",
	},
	// [ticker, address]
	TopicWalletConfigurationUpdated: {
		Template: "configuração para carteira %s foi atualizada. Endereço de depósito = %s",
		Subject:  "Configurações da Carteira Atualizada",
	},
	//  [ticker]
	TopicWalletPasswordUpdated: {
		Template: "Senha para carteira %s foi atualizada.",
		Subject:  "Senha da Carteira Atualizada",
	},
	// [market name, host, time]
	TopicMarketSuspendScheduled: {
		Template: "Mercado %s em %s está agora agendado para suspensão em %v",
		Subject:  "Suspensão de Mercado Agendada",
	},
	// [market name, host]
	TopicMarketSuspended: {
		Template: "Trocas no mercado %s em %s está agora suspenso.",
		Subject:  "Mercado Suspenso",
	},
	// [market name, host]
	TopicMarketSuspendedWithPurge: {
		Template: "Trocas no mercado %s em %s está agora suspenso. Todos pedidos no livro de ofertas foram agora EXPURGADOS.",
		Subject:  "Mercado Suspenso, Pedidos Expurgados",
	},
	// [market name, host, time]
	TopicMarketResumeScheduled: {
		Template: "Mercado %s em %s está agora agendado para resumir em %v",
		Subject:  "Resumo do Mercado Agendado",
	},
	// [market name, host, epoch]
	TopicMarketResumed: {
		Template: "Mercado %s em %s foi resumido para trocas no epoque %d",
		Subject:  "Mercado Resumido",
	},
	// [host]
	TopicUpgradeNeeded: {
		Template: "Você pode precisar atualizar seu cliente para trocas em %s.",
		Subject:  "Atualização Necessária",
	},
	// [host]
	TopicDEXConnected: {
		Subject:  "DEX conectado",
		Template: "%s está conectado",
	},
	// [host]
	TopicDEXDisconnected: {
		Template: "%s está desconectado",
		Subject:  "Server Disconectado",
	},
	// [host, rule, time, details]
	TopicPenalized: {
		Template: "Penalidade de DEX em %s\núltima regra quebrada: %s\nhorário: %v\ndetalhes:\n\"%s\"\n",
		Subject:  "Server Penalizou Você",
	},
	TopicSeedNeedsSaving: {
		Subject:  "Não se esqueça de guardar a seed do app",
		Template: "Uma nova seed para a aplicação foi criada. Faça um backup agora na página de configurações.",
	},
	TopicUpgradedToSeed: {
		Subject:  "Guardar nova seed do app",
		Template: "O cliente foi atualizado para usar uma seed. Faça backup dessa seed na página de configurações.",
	},
	// [host, msg]
	TopicDEXNotification: {
		Subject:  "Mensagem da DEX",
		Template: "%s: %s",
	},
}

// zhCN is the Simplified Chinese (PRC) translations.
var zhCN = map[Topic]*i18n.Translation{
	// [host]
	TopicAccountRegistered: {
		Subject:  "注册账户",
		Template: "您现在可以在 %s 进行交易", // alt. 您现在可以切换到 %s
	},
	// [confs, host]
	TopicFeePaymentInProgress: {
		Subject:  "费用支付中",
		Template: "在切换到 %s 之前等待 %d 次确认", // alt. 在 %s 交易之前等待 %d 确认
	},
	// [confs, required confs]
	TopicRegUpdate: {
		Subject:  "费用支付确认", // alt. 记录更新 (but not displayed)
		Template: "%v/%v 费率确认",
	},
	// [host, error]
	TopicFeePaymentError: {
		Subject:  "费用支付错误",
		Template: "向 %s 支付费用时遇到错误: %v", // alt. 为 %s 支付费率时出错：%v
	},
	// [host, error]
	TopicAccountUnlockError: {
		Subject:  "解锁钱包时出错",
		Template: "解锁帐户 %s 时出错： %v", // alt. 解锁 %s 的帐户时出错: %v
	},
	// [host]
	TopicFeeCoinError: {
		Subject:  "汇率错误",
		Template: "%s 的空置率。", // alt. %s 的费用硬币为空。
	},
	// [host]
	TopicWalletConnectionWarning: {
		Subject:  "钱包连接通知",
		Template: "检测到 %s 的注册不完整，无法连接 decred 钱包", // alt. 检测到 %s 的注册不完整，无法连接到 Decred 钱包
	},
	// [host, error]
	TopicWalletUnlockError: {
		Subject:  "解锁钱包时出错",
		Template: "与 decred 钱包连接以在 %s 上完成注册，但无法解锁： %v", // alt. 已连接到 Decred 钱包以在 %s 完成注册，但无法解锁：%v
	},
	// [ticker, error]
	TopicSendError: {
		Subject:  "提款错误",
		Template: "在 %s 提取过程中遇到错误: %v", // alt. 删除 %s 时遇到错误： %v
		Stale:    true,
	},
	// [value string, ticker, destination address, coin ID]
	TopicSendSuccess: {
		Subject:  "提款已发送",
		Template: "%s %s (%s) 的提款已成功完成。硬币 ID = %s",
		Stale:    true,
	},
	// [error]
	TopicOrderLoadFailure: {
		Subject:  "请求加载失败",
		Template: "某些订单无法从数据库加载：%v", // alt. 某些请求无法从数据库加载:
	},
	// [qty, ticker, token]
	TopicYoloPlaced: {
		Subject:  "下达市价单",
		Template: "以市场价格 (%[3]s) 出售 %[1]s %[2]s",
	},
	// [qty, ticker, rate string, token], RETRANSLATE.
	TopicBuyOrderPlaced: {
		Subject:  "已下订单",
		Template: "Buying %s %s，值 = %s (%s)",
	},
	// [qty, ticker, rate string, token], RETRANSLATE.
	TopicSellOrderPlaced: {
		Subject:  "已下订单",
		Template: "Selling %s %s，值 = %s (%s)",
	},
	// [missing count, token, host]
	TopicMissingMatches: {
		Subject:  "订单缺失匹配",
		Template: "%[2]s 订单的 %[1]d 匹配项未被 %[3]q 报告并被视为已撤销", // alt. %d 订单 %s 的匹配没有被 %q 报告并被视为已撤销
	},
	// [token, error]
	TopicWalletMissing: {
		Subject:  "丢失的钱包",
		Template: "活动订单 %s 的钱包检索错误： %v", // alt. 通过钱包 %s 检索活动订单时出错: %v
	},
	// [side, token, match status]
	TopicMatchErrorCoin: {
		Subject:  "货币不匹配错误",
		Template: "订单 %s 的组合 %s 处于状态 %s，但没有用于交换货币的运行程序。", // alt. 订单 %s 的匹配 %s 处于状态 %s，但没有交换硬币服务商。
	},
	// [side, token, match status]
	TopicMatchErrorContract: {
		Subject:  "合约组合错误",
		Template: "订单 %s 的匹配 %s 处于状态 %s，没有服务商交换合约。",
	},
	// [ticker, contract, token, error]
	TopicMatchRecoveryError: {
		Subject:  "检索匹配时出错",
		Template: "在检索订单 %s: %v 的交易期间审核交易对手交易合约 (%s %v) 时出错", // ? 在订单 %s: %v 的交易恢复期间审核对方的交易合约 (%s %v) 时出错
	},
	// [token]
	TopicOrderCoinError: {
		Subject:  "硬币订单错误",
		Template: "没有为活动订单 %s 记录资金硬币", // alt. 没有为活动订单 %s 注册资金货币
	},
	// [token, ticker, error]
	TopicOrderCoinFetchError: {
		Subject:  "硬币订单恢复错误",
		Template: "检索订单 %s (%s) 的源硬币时出错： %v", // alt. 订单 %s (%s) 的源硬币检索错误: %v
	},
	// [token]
	TopicMissedCancel: {
		Subject:  "丢失取消",
		Template: "取消订单与订单 %s 不匹配。如果取消订单与交易所同时发送，或者订单在取消订单执行之前已完全执行，则可能发生这种情况。",
	},
	// [base ticker, quote ticker, host, token], RETRANSLATE.
	TopicBuyOrderCanceled: {
		Subject:  "订单取消",
		Template: "买入 %s-%s 的 %s 订单已被取消 (%s)", // alt. %s 上 %s-%s 上的 %s 请求已被取消 (%s)
	},
	// [base ticker, quote ticker, host, token], RETRANSLATE.
	TopicSellOrderCanceled: {
		Subject:  "订单取消",
		Template: "卖出 %s-%s 的 %s 订单已被取消 (%s)", // alt. %s 上 %s-%s 上的 %s 请求已被取消 (%s)
	},
	// [base ticker, quote ticker, fill percent, token], RETRANSLATE.
	TopicBuyMatchesMade: {
		Subject:  "匹配完成",
		Template: "买入 %s-%s %.1f%% 的订单已完成 (%s)", // alt. %s 请求超过 %s-%s %.1f%% 已填充（%s）
	},
	// [base ticker, quote ticker, fill percent, token], RETRANSLATE.
	TopicSellMatchesMade: {
		Subject:  "匹配完成",
		Template: "卖出 %s-%s %.1f%% 的订单已完成 (%s)", // alt. %s 请求超过 %s-%s %.1f%% 已填充（%s）
	},
	// [qty, ticker, token]
	TopicSwapSendError: {
		Subject:  "发送交换时出错",
		Template: "在以 %[3]s 的顺序发送价值 %[1]s %[2]s 的输出的交换时遇到错误", // ? 在订单 %s 上发送价值 %.8f %s 的交换输出时遇到错误
	},
	// [match, error]
	TopicInitError: {
		Subject:  "交易错误",
		Template: "通知 DEX 匹配 %s 的交换时出错： %v", // alt. 错误通知 DEX %s 交换组合：%v
	},
	// [match, error]
	TopicReportRedeemError: {
		Subject:  "报销错误",
		Template: "通知 DEX %s 赎回时出错： %v",
	},
	// [qty, ticker, token]
	TopicSwapsInitiated: {
		Subject:  "发起交易",
		Template: "在订单 %[3]s 上发送价值 %[1]s %[2]s 的交易", // should mention "contract" (TODO) ? 已发送价值 %.8f %s 的交易，订单 %s
	},
	// [qty, ticker, token]
	TopicRedemptionError: {
		Subject:  "赎回错误",
		Template: "在订单 %[3]s 上发送价值 %[1]s %[2]s 的兑换时遇到错误", // alt. 在订单 %s 上发现发送价值 %.8f %s 的赎回错误
	},
	// [qty, ticker, token]
	TopicMatchComplete: {
		Subject:  "完全匹配",
		Template: "在订单 %s 上兑换了 %s %s",
	},
	// [qty, ticker, token]
	TopicRefundFailure: {
		Subject:  "退款错误",
		Template: "按顺序 %[3]s 返回 %[1]s %[2]s，有一些错误", // alt. 退款％.8f％s的订单％S，但出现一些错误
	},
	// [qty, ticker, token]
	TopicMatchesRefunded: {
		Subject:  "退款成功",
		Template: "在订单 %[3]s 上返回了 %[1]s %[2]s", // 在订单 %s 上返回了 %.8f %s
	},
	// [match ID token]
	TopicMatchRevoked: {
		Subject:  "撤销组合",
		Template: "匹配 %s 已被撤销", // alt. 组合 %s 已被撤销
	},
	// [token, market name, host]
	TopicOrderRevoked: {
		Subject:  "撤销订单",
		Template: "%s 市场 %s 的订单 %s 已被服务器撤销",
	},
	// [token, market name, host]
	TopicOrderAutoRevoked: {
		Subject:  "订单自动撤销",
		Template: "%s 市场 %s 上的订单 %s 由于市场暂停而被撤销", // alt. %s 市场 %s 中的订单 %s 被市场暂停撤销
	},
	// [ticker, coin ID, match]
	TopicMatchRecovered: {
		Subject:  "恢复订单",
		Template: "找到赎回 (%s: %v) 并验证了请求 %s 的秘密",
	},
	// [token]
	TopicCancellingOrder: {
		Subject:  "取消订单",
		Template: "已为订单 %s 提交了取消操作", // alt. 已为订单 %s 提交取消订单
	},
	// [token, old status, new status]
	TopicOrderStatusUpdate: {
		Subject:  "订单状态更新",
		Template: "订单 %v 的状态从 %v 修改为 %v", // alt. 订单状态 %v 从 %v 修改为 %v
	},
	// [count, host, token]
	TopicMatchResolutionError: {
		Subject:  "订单解析错误",
		Template: "没有为 %[3]s 找到为 %[2]s 报告的 %[1]d 个匹配项。请联系Decred社区以解决该问题。", // alt. %s 报告的 %d 个匹配项没有找到 %s。
	},
	// [token]
	TopicFailedCancel: {
		Subject:  "取消失败",
		Template: "取消订单 %s 的订单 %s 处于 Epoque 状态 2 个 epoques，现在已被删除。",
		Stale:    true,
	},
	// [coin ID, ticker, match]
	TopicAuditTrouble: {
		Subject:  "审计时的问题",
		Template: "继续寻找组合 %[3]s 的货币 %[1]v (%[2]s) 的交易对手合约。您的互联网和钱包连接是否正常？",
	},
	// [host, error]
	TopicDexAuthError: {
		Subject:  "身份验证错误",
		Template: "%s: %v",
	},
	// [count, host]
	TopicUnknownOrders: {
		Subject:  "DEX 报告的未知请求",
		Template: "未找到 DEX %[2]s 报告的 %[1]d 个活动订单。",
	},
	// [count]
	TopicOrdersReconciled: {
		Subject:  "与 DEX 协调的订单",
		Template: "%d 个订单的更新状态。", // alt. %d 个订单的状态已更新。
	},
	// [ticker, address]
	TopicWalletConfigurationUpdated: {
		Subject:  "更新的钱包设置a",
		Template: "钱包 %[1]s 的配置已更新。存款地址 = %[2]s", // alt. %s 钱包的配置已更新。存款地址 = %s
	},
	//  [ticker]
	TopicWalletPasswordUpdated: {
		Subject:  "钱包密码更新",
		Template: "钱包 %s 的密码已更新。", // alt. %s 钱包的密码已更新。
	},
	// [market name, host, time]
	TopicMarketSuspendScheduled: {
		Subject:  "市场暂停预定",
		Template: "%s 上的市场 %s 现在计划在 %v 暂停",
	},
	// [market name, host]
	TopicMarketSuspended: {
		Subject:  "暂停市场",
		Template: "%s 的 %s 市场交易现已暂停。", // alt. %s 市场 %s 的交易现已暂停。
	},
	// [market name, host]
	TopicMarketSuspendedWithPurge: {
		Subject:  "暂停市场，清除订单",
		Template: "%s 的市场交易 %s 现已暂停。订单簿中的所有订单现已被删除。", // alt. %s 市场 %s 的交易现已暂停。所有预订的订单现在都已清除。
	},
	// [market name, host, time]
	TopicMarketResumeScheduled: {
		Subject:  "预定市场摘要",
		Template: "%s 上的市场 %s 现在计划在 %v 恢",
	},
	// [market name, host, epoch]
	TopicMarketResumed: {
		Subject:  "总结市场",
		Template: "%[2]s 上的市场 %[1]s 已汇总用于时代 %[3]d 中的交易", // alt. M%s 的市场 %s 已在epoch %d 恢复交易
	},
	// [host]
	TopicUpgradeNeeded: {
		Subject:  "需要更新",
		Template: "您可能需要更新您的帐户以进行 %s 的交易。", // alt. 您可能需要更新您的客户端以在 %s 进行交易。
	},
	// [host]
	TopicDEXConnected: {
		Subject:  "DEX 连接",
		Template: "%s 已连接",
	},
	// [host]
	TopicDEXDisconnected: {
		Subject:  "服务器断开连接",
		Template: "%s 离线", // alt. %s 已断开连接
	},
	// [host, rule, time, details]
	TopicPenalized: {
		Subject:  "服务器惩罚了你",
		Template: "%s 上的 DEX 惩罚\n最后一条规则被破坏：%s \n时间： %v \n详细信息：\n \" %s \" \n",
	},
	TopicSeedNeedsSaving: {
		Subject:  "不要忘记备份你的应用程序种子", // alt. 别忘了备份应用程序种子
		Template: "已创建新的应用程序种子。请立刻在设置界面中进行备份。",
	},
	TopicUpgradedToSeed: {
		Subject:  "备份您的新应用程序种子",                   // alt. 备份新的应用程序种子
		Template: "客户端已升级为使用应用程序种子。请切换至设置界面备份种子。", // alt. 客户端已升级。请在“设置”界面中备份种子。
	},
	// [host, msg]
	TopicDEXNotification: {
		Subject:  "来自DEX的消息",
		Template: "%s: %s",
	},
}

var plPL = map[Topic]*i18n.Translation{
	// [host]
	TopicAccountRegistered: {
		Subject:  "Konto zarejestrowane",
		Template: "Możesz teraz handlować na %s",
	},
	// [confs, host]
	TopicFeePaymentInProgress: {
		Subject:  "Opłata rejestracyjna w drodze",
		Template: "Oczekiwanie na %d potwierdzeń przed rozpoczęciem handlu na %s",
	},
	// [confs, required confs]
	TopicRegUpdate: {
		Subject:  "Aktualizacja rejestracji",
		Template: "Potwierdzenia opłaty rejestracyjnej %v/%v",
	},
	// [host, error]
	TopicFeePaymentError: {
		Subject:  "Błąd płatności rejestracyjnej",
		Template: "Wystąpił błąd przy płatności dla %s: %v",
	},
	// [host, error]
	TopicAccountUnlockError: {
		Subject:  "Błąd odblokowywania konta",
		Template: "błąd odblokowywania konta dla %s: %v",
	},
	// [host]
	TopicFeeCoinError: {
		Subject:  "Błąd w płatności rejestracyjnej",
		Template: "Nie znaleziono środków na płatność rejestracyjną dla %s.",
	},
	// [host]
	TopicWalletConnectionWarning: {
		Subject:  "Ostrzeżenie połączenia z portfelem",
		Template: "Wykryto niedokończoną rejestrację dla %s, ale nie można połączyć się z portfelem Decred",
	},
	// [host, error]
	TopicWalletUnlockError: {
		Subject:  "Błąd odblokowywania portfela",
		Template: "Połączono z portfelem Decred, aby dokończyć rejestrację na %s, lecz próba odblokowania portfela nie powiodła się: %v",
	},
	// [ticker, error]
	TopicSendError: {
		Subject:  "Błąd wypłaty środków",
		Template: "Wystąpił błąd przy wypłacaniu %s: %v",
		Stale:    true,
	},
	// [value string, ticker, destination address, coin ID]
	TopicSendSuccess: {
		Subject:  "Wypłata zrealizowana",
		Template: "Wypłata %s %s (%s) została zrealizowana pomyślnie. ID monety = %s",
		Stale:    true,
	},
	// [error]
	TopicOrderLoadFailure: {
		Subject:  "Błąd wczytywania zleceń",
		Template: "Niektórych zleceń nie udało się wczytać z bazy danych: %v",
	},
	// [qty, ticker, token]
	TopicYoloPlaced: {
		Subject:  "Złożono zlecenie rynkowe",
		Template: "sprzedaż %s %s po kursie rynkowym (%s)",
	},
	// [qty, ticker, rate string, token], RETRANSLATE.
	TopicBuyOrderPlaced: {
		Subject:  "Złożono zlecenie",
		Template: "Buying %s %s, kurs = %s (%s)",
	},
	// [qty, ticker, rate string, token], RETRANSLATE.
	TopicSellOrderPlaced: {
		Subject:  "Złożono zlecenie",
		Template: "Selling %s %s, kurs = %s (%s)",
	},
	// [missing count, token, host]
	TopicMissingMatches: {
		Subject:  "Brak spasowanych zamówień",
		Template: "%d spasowań dla zlecenia %s nie zostało odnotowanych przez %q i są uznane za unieważnione",
	},
	// [token, error]
	TopicWalletMissing: {
		Subject:  "Brak portfela",
		Template: "Błąd odczytu z portfela dla aktywnego zlecenia %s: %v",
	},
	// [side, token, match status]
	TopicMatchErrorCoin: {
		Subject:  "Błąd spasowanej monety",
		Template: "Spasowanie %s dla zlecenia %s jest w stanie %s, lecz brakuje monety po stronie maker.",
	},
	// [side, token, match status]
	TopicMatchErrorContract: {
		Subject:  "Błąd kontraktu spasowania",
		Template: "Spasowanie %s dla zlecenia %s jest w stanie %s, lecz brakuje kontraktu zamiany po stronie maker.",
	},
	// [ticker, contract, token, error]
	TopicMatchRecoveryError: {
		Subject:  "Błąd odzyskiwania spasowania",
		Template: "Błąd przy audycie kontraktu zamiany u kontrahenta (%s %v) podczas odzyskiwania zamiany dla zlecenia %s: %v",
	},
	// [token]
	TopicOrderCoinError: {
		Subject:  "Błąd monety dla zlecenia",
		Template: "Nie znaleziono środków fundujących dla aktywnego zlecenia %s",
	},
	// [token, ticker, error]
	TopicOrderCoinFetchError: {
		Subject:  "Błąd pozyskania środków dla zlecenia",
		Template: "Błąd pozyskania środków źródłowych dla zlecenia %s (%s): %v",
	},
	// [token]
	TopicMissedCancel: {
		Subject:  "Spóźniona anulacja",
		Template: "Zlecenie anulacji nie zostało spasowane dla zlecenia %s. Może to mieć miejsce, gdy zlecenie anulacji wysłane jest w tej samej epoce, co zlecenie handlu, lub gdy zlecenie handlu zostaje w pełni wykonane przed spasowaniem ze zleceniem anulacji.",
	},
	// [base ticker, quote ticker, host, token], RETRANSLATE.
	TopicBuyOrderCanceled: {
		Subject:  "Zlecenie anulowane",
		Template: "Zlecenie buy dla %s-%s na %s zostało anulowane (%s)",
	},
	// [base ticker, quote ticker, host, token], RETRANSLATE.
	TopicSellOrderCanceled: {
		Subject:  "Zlecenie anulowane",
		Template: "Zlecenie sell dla %s-%s na %s zostało anulowane (%s)",
	},
	// [base ticker, quote ticker, fill percent, token], RETRANSLATE.
	TopicSellMatchesMade: {
		Subject:  "Dokonano spasowania",
		Template: "Zlecenie sell na %s-%s zrealizowane w %.1f%% (%s)",
	},
	// [base ticker, quote ticker, fill percent, token], RETRANSLATE.
	TopicBuyMatchesMade: {
		Subject:  "Dokonano spasowania",
		Template: "Zlecenie buy na %s-%s zrealizowane w %.1f%% (%s)",
	},
	// [qty, ticker, token]
	TopicSwapSendError: {
		Subject:  "Błąd wysyłki środków",
		Template: "Błąd przy wysyłaniu środków wartych %s %s dla zlecenia %s",
	},
	// [match, error]
	TopicInitError: {
		Subject:  "Błąd raportowania zamiany",
		Template: "Błąd powiadomienia DEX o zamianie dla spasowania %s: %v",
	},
	// [match, error]
	TopicReportRedeemError: {
		Subject:  "Błąd raportowania wykupienia",
		Template: "Błąd powiadomienia DEX o wykupieniu środków dla spasowania %s: %v",
	},
	// [qty, ticker, token]
	TopicSwapsInitiated: {
		Subject:  "Zamiana rozpoczęta",
		Template: "Wysłano środki o wartości %s %s dla zlecenia %s",
	},
	// [qty, ticker, token]
	TopicRedemptionError: {
		Subject:  "Błąd wykupienia",
		Template: "Napotkano błąd przy wykupywaniu środków o wartości %s %s dla zlecenia %s",
	},
	// [qty, ticker, token]
	TopicMatchComplete: {
		Subject:  "Spasowanie zakończone",
		Template: "Wykupiono %s %s ze zlecenia %s",
	},
	// [qty, ticker, token]
	TopicRefundFailure: {
		Subject:  "Niepowodzenie zwrotu środków",
		Template: "Zwrócono %s %s za zlecenie %s, z pewnymi błędami",
	},
	// [qty, ticker, token]
	TopicMatchesRefunded: {
		Subject:  "Zwrot środków za spasowanie zleceń",
		Template: "Zwrócono %s %s za zlecenie %s",
	},
	// [match ID token]
	TopicMatchRevoked: {
		Subject:  "Spasowanie zleceń unieważnione",
		Template: "Spasowanie %s zostało unieważnione",
	},
	// [token, market name, host]
	TopicOrderRevoked: {
		Subject:  "Zlecenie unieważnione",
		Template: "Zlecenie %s na rynku %s na %s zostało unieważnione przez serwer",
	},
	// [token, market name, host]
	TopicOrderAutoRevoked: {
		Subject:  "Zlecenie unieważnione automatycznie",
		Template: "Zlecenie %s na rynku %s na %s zostało unieważnione z powodu wstrzymania handlu na tym rynku",
	},
	// [ticker, coin ID, match]
	TopicMatchRecovered: {
		Subject:  "Odzyskano spasowanie",
		Template: "Odnaleziono wykup ze strony maker (%s: %v) oraz potwierdzono sekret dla spasowania %s",
	},
	// [token]
	TopicCancellingOrder: {
		Subject:  "Anulowanie zlecenia",
		Template: "Złożono polecenie anulowania dla zlecenia %s",
	},
	// [token, old status, new status]
	TopicOrderStatusUpdate: {
		Subject:  "Aktualizacja statusu zlecenia",
		Template: "Status zlecenia %v został zmieniony z %v na %v",
	},
	// [count, host, token]
	TopicMatchResolutionError: {
		Subject:  "Błąd rozstrzygnięcia spasowania",
		Template: "Nie znaleziono %d spasowań odnotowanych przez %s dla %s.",
	},
	// [token]
	TopicFailedCancel: {
		Subject:  "Niepowodzenie anulowania",
		Template: "Zlecenie anulacji dla zlecenia %s utknęło w statusie epoki przez 2 epoki i zostało usunięte.",
		Stale:    true,
	},
	// [coin ID, ticker, match]
	TopicAuditTrouble: {
		Subject:  "Problem z audytem",
		Template: "Wciąż szukamy monety kontraktowej kontrahenta %v (%s) dla spasowania %s. Czy Twoje połączenie z Internetem i portfelem jest dobre?",
	},
	// [host, error]
	TopicDexAuthError: {
		Subject:  "Błąd uwierzytelniania DEX",
		Template: "%s: %v",
	},
	// [count, host]
	TopicUnknownOrders: {
		Subject:  "DEX odnotował nieznane zlecenia",
		Template: "Nie znaleziono %d aktywnych zleceń odnotowanych przez DEX %s.",
	},
	// [count]
	TopicOrdersReconciled: {
		Subject:  "Pogodzono zlecenia z DEX",
		Template: "Zaktualizowano statusy dla %d zleceń.",
	},
	// [ticker, address]
	TopicWalletConfigurationUpdated: {
		Subject:  "Zaktualizowano konfigurację portfela",
		Template: "Konfiguracja dla portfela %s została zaktualizowana. Adres do depozytów = %s",
	},
	//  [ticker]
	TopicWalletPasswordUpdated: {
		Subject:  "Zaktualizowano hasło portfela",
		Template: "Hasło dla portfela %s zostało zaktualizowane.",
	},
	// [market name, host, time]
	TopicMarketSuspendScheduled: {
		Subject:  "Planowane zawieszenie rynku",
		Template: "Rynek %s na %s zostanie wstrzymany o %v",
	},
	// [market name, host]
	TopicMarketSuspended: {
		Subject:  "Rynek wstrzymany",
		Template: "Handel na rynku %s na %s jest obecnie wstrzymany.",
	},
	// [market name, host]
	TopicMarketSuspendedWithPurge: {
		Subject:  "Rynek wstrzymany, księga zamówień wyczyszczona",
		Template: "Handel na rynku %s na %s jest obecnie wstrzymany. Wszystkie złożone zamówienia zostały WYCOFANE.",
	},
	// [market name, host, time]
	TopicMarketResumeScheduled: {
		Subject:  "Planowane wznowienie rynku",
		Template: "Rynek %s na %s zostanie wznowiony o %v",
	},
	// [market name, host, epoch]
	TopicMarketResumed: {
		Subject:  "Rynek wznowiony",
		Template: "Rynek %s na %s wznowił handel w epoce %d",
	},
	// [host]
	TopicUpgradeNeeded: {
		Subject:  "Wymagana aktualizacja",
		Template: "Aby handlować na %s wymagana jest aktualizacja klienta.",
	},
	// [host]
	TopicDEXConnected: {
		Subject:  "Połączono z serwerem",
		Template: "Połączono z %s",
	},
	// [host]
	TopicDEXDisconnected: {
		Subject:  "Rozłączono z serwerem",
		Template: "Rozłączono z %s",
	},
	// [host, rule, time, details]
	TopicPenalized: {
		Subject:  "Serwer ukarał Cię punktami karnymi",
		Template: "Punkty karne od serwera DEX na %s\nostatnia złamana reguła: %s\nczas: %v\nszczegóły:\n\"%s\"\n",
	},
	TopicSeedNeedsSaving: {
		Subject:  "Nie zapomnij zrobić kopii ziarna aplikacji",
		Template: "Utworzono nowe ziarno aplikacji. Zrób jego kopię w zakładce ustawień.",
	},
	TopicUpgradedToSeed: {
		Subject:  "Zrób kopię nowego ziarna aplikacji",
		Template: "Klient został zaktualizowany, by korzystać z ziarna aplikacji. Zrób jego kopię w zakładce ustawień.",
	},
	// [host, msg]
	TopicDEXNotification: {
		Subject:  "Wiadomość od DEX",
		Template: "%s: %s",
	},
}

// deDE is the German translations.
var deDE = map[Topic]*i18n.Translation{
	// [host]
	TopicAccountRegistered: {
		Subject:  "Account registeriert",
		Template: "Du kannst nun auf %s handeln",
	},
	// [confs, host]
	TopicFeePaymentInProgress: {
		Subject:  "Abwicklung der Registrationsgebühr",
		Template: "Warten auf %d Bestätigungen bevor mit dem Handel bei %s begonnen werden kann",
	},
	// [confs, required confs]
	TopicRegUpdate: {
		Subject:  "Aktualisierung der Registration",
		Template: "%v/%v Bestätigungen der Registrationsgebühr",
	},
	// [host, error]
	TopicFeePaymentError: {
		Subject:  "Fehler bei der Zahlung der Registrationsgebühr",
		Template: "Bei der Zahlung der Registrationsgebühr für %s trat ein Fehler auf: %v",
	},
	// [host, error]
	TopicAccountUnlockError: {
		Subject:  "Fehler beim Entsperren des Accounts",
		Template: "Fehler beim Entsperren des Accounts für %s: %v",
	},
	// [host]
	TopicFeeCoinError: {
		Subject:  "Coin Fehler bei Registrationsgebühr",
		Template: "Fehlende Coin Angabe für Registrationsgebühr bei %s.",
	},
	// [host]
	TopicWalletConnectionWarning: {
		Subject:  "Warnung bei Wallet Verbindung",
		Template: "Unvollständige Registration für %s erkannt, konnte keine Verbindung zum Decred Wallet herstellen",
	},
	// [host, error]
	TopicWalletUnlockError: {
		Subject:  "Fehler beim Entsperren des Wallet",
		Template: "Verbunden zum Wallet um die Registration bei %s abzuschließen, ein Fehler beim entsperren des Wallet ist aufgetreten: %v",
	},
	// [asset name, error message]
	TopicWalletCommsWarning: {
		Subject:  "Probleme mit der Verbindung zum Wallet",
		Template: "Kommunikation mit dem %v Wallet nicht möglich! Grund: %q",
	},
	// [asset name]
	TopicWalletPeersWarning: {
		Subject:  "Problem mit dem Wallet-Netzwerk",
		Template: "%v Wallet hat keine Netzwerk-Peers!",
	},
	// [asset name]
	TopicWalletPeersRestored: {
		Subject:  "Wallet-Konnektivität wiederhergestellt",
		Template: "Die Verbindung mit dem %v Wallet wurde wiederhergestellt.",
	},
	// [ticker, error]
	TopicSendError: {
		Subject:  "Sendefehler",
		Template: "Fehler beim senden von %s aufgetreten: %v",
	},
	// [ticker, coin ID]
	TopicSendSuccess: {
		Subject:  "Erfolgreich gesendet",
		Template: "Das Senden von %s wurde erfolgreich abgeschlossen. Coin ID = %s",
	},
	// [error]
	TopicOrderLoadFailure: {
		Subject:  "Fehler beim Laden der Aufträge",
		Template: "Einige Aufträge konnten nicht aus der Datenbank geladen werden: %v",
	},
	// [qty, ticker, token]
	TopicYoloPlaced: {
		Subject:  "Marktauftrag platziert",
		Template: "Verkaufe %s %s zum Marktpreis (%s)",
	},
	// [qty, ticker, rate string, token]
	TopicBuyOrderPlaced: {
		Subject:  "Auftrag platziert",
		Template: "Buying %s %s, Kurs = %s (%s)",
		Stale:    true,
	},
	// [qty, ticker, rate string, token]
	TopicSellOrderPlaced: {
		Subject:  "Auftrag platziert",
		Template: "Selling %s %s, Kurs = %s (%s)",
		Stale:    true,
	},
	// [missing count, token, host]
	TopicMissingMatches: {
		Subject:  "Fehlende Matches",
		Template: "%d Matches für den Auftrag %s wurden nicht von %q gemeldet und gelten daher als widerrufen",
	},
	// [token, error]
	TopicWalletMissing: {
		Subject:  "Wallet fehlt",
		Template: "Fehler bei der Wallet-Abfrage für den aktiven Auftrag %s: %v",
	},
	// [side, token, match status]
	TopicMatchErrorCoin: {
		Subject:  "Fehler beim Coin Match",
		Template: "Match %s für den Auftrag %s hat den Status %s, hat aber keine Coins für den Swap vom Maker gefunden.",
	},
	// [side, token, match status]
	TopicMatchErrorContract: {
		Subject:  "Fehler beim Match Kontrakt",
		Template: "Match %s für Auftrag %s hat den Status %s, hat aber keinen passenden Maker Swap Kontrakt.",
	},
	// [ticker, contract, token, error]
	TopicMatchRecoveryError: {
		Subject:  "Fehler bei Match Wiederherstellung",
		Template: "Fehler bei der Prüfung des Swap-Kontrakts der Gegenpartei (%s %v) während der Wiederherstellung des Auftrags %s: %v",
	},
	// [token]
	TopicOrderCoinError: {
		Subject:  "Fehler bei den Coins für einen Auftrag",
		Template: "Keine Coins zur Finanzierung des aktiven Auftrags %s gefunden",
	},
	// [token, ticker, error]
	TopicOrderCoinFetchError: {
		Subject:  "Fehler beim Abruf der Coins für den Auftrag",
		Template: "Beim Abruf der Coins als Quelle für den Auftrag %s (%s) ist ein Fehler aufgetreten: %v",
	},
	// [token]
	TopicMissedCancel: {
		Subject:  "Abbruch verpasst",
		Template: "Der Abbruch passt nicht zum Auftrag %s. Dies kann passieren wenn der Abbruch in der gleichen Epoche wie der Abschluss übermittelt wird oder wenn der Zielauftrag vollständig ausgeführt wird bevor er mit dem Abbruch gematcht werden konnte.",
	},
	// [base ticker, quote ticker, host, token]
	TopicBuyOrderCanceled: {
		Subject:  "Auftrag abgebrochen",
		Template: "Auftrag für %s-%s bei %s wurde abgebrochen (%s)",
		Stale:    true,
	},
	TopicSellOrderCanceled: {
		Subject:  "Auftrag abgebrochen",
		Template: "Auftrag für %s-%s bei %s wurde abgebrochen (%s)",
		Stale:    true,
	},
	// [base ticker, quote ticker, fill percent, token]
	TopicBuyMatchesMade: {
		Subject:  "Matches durchgeführt",
		Template: "Auftrag für %s-%s %.1f%% erfüllt (%s)",
		Stale:    true,
	},
	// [base ticker, quote ticker, fill percent, token]
	TopicSellMatchesMade: {
		Subject:  "Matches durchgeführt",
		Template: "Auftrag für %s-%s %.1f%% erfüllt (%s)",
		Stale:    true,
	},
	// [qty, ticker, token]
	TopicSwapSendError: {
		Subject:  "Fehler beim Senden des Swaps",
		Template: "Beim Senden des Swap Output(s) im Wert von %s %s für den Auftrag %s",
	},
	// [match, error]
	TopicInitError: {
		Subject:  "Fehler beim Swap Reporting",
		Template: "Fehler bei der Benachrichtigung des Swaps an den DEX für den Match %s: %v",
	},
	// [match, error]
	TopicReportRedeemError: {
		Subject:  "Fehler beim Redeem Reporting",
		Template: "Fehler bei der Benachrichtigung des DEX für die Redemption des Match %s: %v",
	},
	// [qty, ticker, token]
	TopicSwapsInitiated: {
		Subject:  "Swaps initiiert",
		Template: "Swaps im Wert von %s %s für den Auftrag %s gesendet",
	},
	// [qty, ticker, token]
	TopicRedemptionError: {
		Subject:  "Fehler bei der Redemption",
		Template: "Fehler beim Senden von Redemptions im Wert von %s %s für Auftrag %s",
	},
	// [qty, ticker, token]
	TopicMatchComplete: {
		Subject:  "Match abgeschlossen",
		Template: "Redeemed %s %s für Auftrag %s",
	},
	// [qty, ticker, token]
	TopicRefundFailure: {
		Subject:  "Fehler bei der Erstattung",
		Template: "%s %s für Auftrag %s erstattet, mit einigen Fehlern",
	},
	// [qty, ticker, token]
	TopicMatchesRefunded: {
		Subject:  "Matches Erstattet",
		Template: "%s %s für Auftrag %s erstattet",
	},
	// [match ID token]
	TopicMatchRevoked: {
		Subject:  "Match widerrufen",
		Template: "Match %s wurde widerrufen",
	},
	// [token, market name, host]
	TopicOrderRevoked: {
		Subject:  "Auftrag widerrufen",
		Template: "Der Auftrag %s für den %s Markt bei %s wurde vom Server widerrufen",
	},
	// [token, market name, host]
	TopicOrderAutoRevoked: {
		Subject:  "Auftrag automatisch widerrufen",
		Template: "Der Auftrag %s für den %s Markt bei %s wurde wegen Aussetzung des Marktes widerrufen",
	},
	// [ticker, coin ID, match]
	TopicMatchRecovered: {
		Subject:  "Match wiederhergestellt",
		Template: "Die Redemption (%s: %v) des Anbieters wurde gefunden und das Geheimnis für Match %s verifiziert",
	},
	// [token]
	TopicCancellingOrder: {
		Subject:  "Auftrag abgebrochen",
		Template: "Der Auftrag %s wurde abgebrochen",
	},
	// [token, old status, new status]
	TopicOrderStatusUpdate: {
		Subject:  "Aktualisierung des Auftragsstatus",
		Template: "Status des Auftrags %v geändert von %v auf %v",
	},
	// [count, host, token]
	TopicMatchResolutionError: {
		Subject:  "Fehler bei der Auflösung für Match",
		Template: "%d Matches durch %s gemeldet wurden für %s nicht gefunden.",
	},
	// [token]
	TopicFailedCancel: {
		Subject:  "Abbruch fehlgeschlagen",
		Template: "Der Auftrag für den Abbruch des Auftrags %s blieb 2 Epochen lang im Epoche-Status hängen und wird nun gelöscht.",
		Stale:    true,
	},
	// [coin ID, ticker, match]
	TopicAuditTrouble: {
		Subject:  "Audit-Probleme",
		Template: "Immernoch auf der Suche den Coins %v (%s) der Gegenseite für Match %s. Überprüfe deine Internetverbindung und die Verbindungen zum Wallet.",
	},
	// [host, error]
	TopicDexAuthError: {
		Subject:  "DEX-Authentifizierungsfehler",
		Template: "%s: %v",
	},
	// [count, host]
	TopicUnknownOrders: {
		Subject:  "DEX meldet unbekannte Aufträge",
		Template: "%d aktive Aufträge von DEX %s gemeldet aber konnten nicht gefunden werden.",
	},
	// [count]
	TopicOrdersReconciled: {
		Subject:  "Aufträge mit DEX abgestimmt",
		Template: "Der Status für %d Aufträge wurde aktualisiert.",
	},
	// [ticker, address]
	TopicWalletConfigurationUpdated: {
		Subject:  "Aktualisierung der Wallet Konfiguration",
		Template: "Konfiguration für Wallet %s wurde aktualisiert. Einzahlungsadresse = %s",
	},
	//  [ticker]
	TopicWalletPasswordUpdated: {
		Subject:  "Wallet-Passwort aktualisiert",
		Template: "Passwort für das %s Wallet wurde aktualisiert.",
	},
	// [market name, host, time]
	TopicMarketSuspendScheduled: {
		Subject:  "Aussetzung des Marktes geplant",
		Template: "%s Markt bei %s ist nun für ab %v zur Aussetzung geplant.",
	},
	// [market name, host]
	TopicMarketSuspended: {
		Subject:  "Markt ausgesetzt",
		Template: "Der Handel für den %s Markt bei %s ist nun ausgesetzt.",
	},
	// [market name, host]
	TopicMarketSuspendedWithPurge: {
		Subject:  "Markt ausgesetzt, Aufträge gelöscht",
		Template: "Der Handel für den %s Markt bei %s ist nun ausgesetzt. Alle gebuchten Aufträge werden jetzt ENTFERNT.",
	},
	// [market name, host, time]
	TopicMarketResumeScheduled: {
		Subject:  "Wiederaufnahme des Marktes geplant",
		Template: "Der %s Markt bei %s wird nun zur Wiederaufnahme ab %v geplant",
	},
	// [market name, host, epoch]
	TopicMarketResumed: {
		Subject:  "Markt wiederaufgenommen",
		Template: "Der %s Markt bei %s hat den Handel mit der Epoche %d wieder aufgenommen",
	},
	// [host]
	TopicUpgradeNeeded: {
		Subject:  "Upgrade notwendig",
		Template: "Du musst deinen Klient aktualisieren um bei %s zu Handeln.",
	},
	// [host]
	TopicDEXConnected: {
		Subject:  "Server verbunden",
		Template: "Erfolgreich verbunden mit %s",
	},
	// [host]
	TopicDEXDisconnected: {
		Subject:  "Verbindung zum Server getrennt",
		Template: "Verbindung zu %s unterbrochen",
	},
	// [host, rule, time, details]
	TopicPenalized: {
		Subject:  "Bestrafung durch einen Server erhalten",
		Template: "Bestrafung von DEX %s\nletzte gebrochene Regel: %s\nZeitpunkt: %v\nDetails:\n\"%s\"\n",
	},
	TopicSeedNeedsSaving: {
		Subject:  "Vergiss nicht deinen App-Seed zu sichern",
		Template: "Es wurde ein neuer App-Seed erstellt. Erstelle jetzt eine Sicherungskopie in den Einstellungen.",
	},
	TopicUpgradedToSeed: {
		Subject:  "Sichere deinen neuen App-Seed",
		Template: "Dein Klient wurde aktualisiert und nutzt nun einen App-Seed. Erstelle jetzt eine Sicherungskopie in den Einstellungen.",
	},
	// [host, msg]
	TopicDEXNotification: {
		Subject:  "Nachricht von DEX",
		Template: "%s: %s",
	},
	// [parentSymbol, tokenSymbol]
	TopicQueuedCreationFailed: {
		Subject:  "Token-Wallet konnte nicht erstellt werden",
		Template: "Nach dem Erstellen des %s-Wallet kam es zu einen Fehler, konnte das %s-Wallet nicht erstellen",
	},
}

// ar is the Arabic translations.
var ar = map[Topic]*i18n.Translation{
	// [host]
	TopicAccountRegistered: {
		Subject:  "تم تسجيل الحساب",
		Template: "يمكنك الآن التداول عند \u200e%s",
	},
	// [confs, host]
	TopicFeePaymentInProgress: {
		Subject:  "جارٍ دفع الرسوم",
		Template: "في انتظار \u200e%d تأكيدات قبل التداول عند \u200e%s",
	},
	// [confs, required confs]
	TopicRegUpdate: {
		Subject:  "تحديث التسجيل",
		Template: "تأكيدات دفع الرسوم \u200e%v/\u200e%v",
	},
	// [host, error]
	TopicFeePaymentError: {
		Subject:  "خطأ في دفع الرسوم",
		Template: "عثر على خطأ أثناء دفع الرسوم إلى \u200e%s: \u200e%v",
	},
	// [host, error]
	TopicAccountUnlockError: {
		Subject:  "خطأ فتح الحساب",
		Template: "خطأ فتح الحساب لأجل \u200e%s: \u200e%v",
	},
	// [host]
	TopicFeeCoinError: {
		Subject:  "خطأ في رسوم العملة",
		Template: "رسوم العملة \u200e%s فارغة.",
	},
	// [host]
	TopicWalletConnectionWarning: {
		Subject:  "تحذير اتصال المحفظة",
		Template: "تم الكشف عن تسجيل غير مكتمل لـ  \u200e%s، لكنه فشل في الاتصال بمحفظة ديكريد",
	},
	// [host, error]
	TopicWalletUnlockError: {
		Subject:  "خطأ في فتح المحفظة",
		Template: "متصل بالمحفظة لإكمال التسجيل عند \u200e%s، لكنه فشل في فتح القفل: \u200e%v",
	},
	// [asset name, error message]
	TopicWalletCommsWarning: {
		Subject:  "مشكلة الإتصال بالمحفظة",
		Template: "غير قادر على الاتصال بمحفظة !\u200e%v السبب: \u200e%v",
	},
	// [asset name]
	TopicWalletPeersWarning: {
		Subject:  "مشكلة في شبكة المحفظة",
		Template: "!لا يوجد لدى المحفظة \u200e%v نظراء على الشبكة",
	},
	// [asset name]
	TopicWalletPeersRestored: {
		Subject:  "تمت استعادة الاتصال بالمحفظة",
		Template: "تمت اعادة الاتصال بالمحفظة \u200e%v.",
	},
	// [ticker, error]
	TopicSendError: {
		Subject:  "إرسال الخطأ",
		Template: "تمت مصادفة خطأ أثناء إرسال \u200e%s: \u200e%v",
	},
	// [value string, ticker, destination address, coin ID]
	TopicSendSuccess: {
		Subject:  "تم الإرسال بنجاح",
		Template: "تم بنجاح إرسال \u200e%s \u200e%s إلى \u200e%s. معرف العملة = \u200e%s",
	},
	// [error]
	TopicOrderLoadFailure: {
		Subject:  "فشل تحميل الطلب",
		Template: "فشل تحميل بعض الطلبات من قاعدة البيانات: \u200e%v",
	},
	// [qty, ticker, token]
	TopicYoloPlaced: {
		Subject:  "تم تقديم طلب السوق",
		Template: "بيع \u200e%s \u200e%s بسعر السوق (\u200e%s)",
	},
	// [qty, ticker, rate string, token]
	TopicBuyOrderPlaced: {
		Subject:  "تم وضع الطلب",
		Template: "الشراء \u200e%s \u200e%s، السعر = \u200e%s (\u200e%s)",
	},
	// [qty, ticker, rate string, token]
	TopicSellOrderPlaced: {
		Subject:  "تم وضع الطلب",
		Template: "بيع \u200e%s \u200e%s، السعر = \u200e%s (\u200e%s)",
	},
	// [missing count, token, host]
	TopicMissingMatches: {
		Subject:  "مطابقات مفقودة",
		Template: "لم يتم الإبلاغ عن \u200e%d تطابقات للطلب \u200e%s من قبل \u200e%q وتعتبر باطلة",
	},
	// [token, error]
	TopicWalletMissing: {
		Subject:  "المحفظة مفقودة",
		Template: "خطأ في استرداد المحفظة للطلب النشط \u200e%s: \u200e%v",
	},
	// [side, token, match status]
	TopicMatchErrorCoin: {
		Subject:  "خطأ في مطابقة العملة",
		Template: "مطابقة \u200e%s للطلب \u200e%s في الحالة \u200e%s، لكن لا توجد عملة مقايضة من الصانع.",
	},
	// [side, token, match status]
	TopicMatchErrorContract: {
		Subject:  "خطأ في عقد المطابقة",
		Template: "تطابق \u200e%s للطلب \u200e%s في الحالة \u200e%s، لكن لا يوجد عقد مقايضة من الصانع.",
	},
	// [ticker, contract, token, error]
	TopicMatchRecoveryError: {
		Subject:  "خطأ استعادة المطابقة",
		Template: "خطأ في تدقيق عقد مقايضة الطرف المقابل (\u200e%s \u200e%v) أثناء استرداد المقايضة عند الطلب \u200e%s: \u200e%v",
	},
	// [token]
	TopicOrderCoinError: {
		Subject:  "خطأ عند طلب العملة",
		Template: "لم يتم تسجيل عملات تمويل للطلب النشط \u200e%s",
	},
	// [token, ticker, error]
	TopicOrderCoinFetchError: {
		Subject:  "خطأ في جلب طلب العملة",
		Template: "خطأ في استرداد عملات المصدر للطلب \u200e%s (\u200e%s): \u200e%v",
	},
	// [token]
	TopicMissedCancel: {
		Subject:  "تفويت الإلغاء",
		Template: "طلب الإلغاء لم يتطابق مع الطلب \u200e%s. يمكن أن يحدث هذا إذا تم تقديم طلب الإلغاء في نفس حقبة التداول أو إذا تم تنفيذ الطلب المستهدف بالكامل قبل المطابقة مع طلب الإلغاء.",
	},
	// [base ticker, quote ticker, host, token]
	TopicBuyOrderCanceled: {
		Subject:  "تم إلغاء الطلب",
		Template: "تم إلغاء  (\u200e%s) طلب الشراء عند \u200e%s-\u200e%s في \u200e%s",
	},
	TopicSellOrderCanceled: {
		Subject:  "تم إلغاء الطلب",
		Template: "تم إلغاء  (\u200e%s) طلب البيع عند \u200e%s-\u200e%s في \u200e%s",
	},
	// [base ticker, quote ticker, fill percent, token]
	TopicBuyMatchesMade: {
		Subject:  "تم وضع المطابقات",
		Template: "تم تنفيذ (\u200e%s) طلب البيع عند \u200e%s-\u200e%s \u200e%.1f%%",
	},
	// [base ticker, quote ticker, fill percent, token]
	TopicSellMatchesMade: {
		Subject:  "تم وضع المطابقات",
		Template: "تم تنفيذ (\u200e%s) طلب الشراء عند \u200e%s-\u200e%s \u200e%.1f%%",
	},
	// [qty, ticker, token]
	TopicSwapSendError: {
		Subject:  "خطأ في إرسال المقايضة",
		Template: "تمت مصادفة خطأ في إرسال إخراج (مخرجات) مقايضة بقيمة \u200e%s \u200e%s عند الطلب \u200e%s",
	},
	// [match, error]
	TopicInitError: {
		Subject:  "خطأ إبلاغ مقايضة",
		Template: "خطأ إخطار منصة المبادلات اللامركزية بالمقايضة من أجل المطابقة \u200e%s: \u200e%v",
	},
	// [match, error]
	TopicReportRedeemError: {
		Subject:  "خطأ إبلاغ الإسترداد",
		Template: "خطأ في إعلام منصة المبادلات اللامركزية بالاسترداد من أجل المطابقة \u200e%s: \u200e%v",
	},
	// [qty, ticker, token]
	TopicSwapsInitiated: {
		Subject:  "بدأت المقايضات",
		Template: "تم إرسال مقايضات بقيمة \u200e%s \u200e%s عند الطلب \u200e%s",
	},
	// [qty, ticker, token]
	TopicRedemptionError: {
		Subject:  "خطأ في الاسترداد",
		Template: "حدث خطأ أثناء إرسال عمليات استرداد بقيمة \u200e%s \u200e%s للطلب \u200e%s",
	},
	// [qty, ticker, token]
	TopicMatchComplete: {
		Subject:  "اكتملت المطابقة",
		Template: "تم استرداد \u200e%s \u200e%s عند الطلب \u200e%s",
	},
	// [qty, ticker, token]
	TopicRefundFailure: {
		Subject:  "فشل الاسترداد",
		Template: "تم استرداد \u200e%s \u200e%s للطلب \u200e%s، مع وجود بعض الأخطاء",
	},
	// [qty, ticker, token]
	TopicMatchesRefunded: {
		Subject:  "تم استرداد مبالغ المطابقات",
		Template: "تم استرداد مبلغ \u200e%s \u200e%s للطلب \u200e%s",
	},
	// [match ID token]
	TopicMatchRevoked: {
		Subject:  "تم إبطال المطابقة",
		Template: "تم إبطال المطابقة \u200e%s",
	},
	// [token, market name, host]
	TopicOrderRevoked: {
		Subject:  "تم إبطال الطلب",
		Template: "تم إبطال الطلب \u200e%s في السوق \u200e%s عند \u200e%s من قبل الخادم",
	},
	// [token, market name, host]
	TopicOrderAutoRevoked: {
		Subject:  "تم إبطال الطلب تلقائيًا",
		Template: "تم إبطال الطلب \u200e%s في السوق \u200e%s عند \u200e%s بسبب تعليق عمليات السوق",
	},
	// [ticker, coin ID, match]
	TopicMatchRecovered: {
		Subject:  "تم استرداد المطابقة",
		Template: "تم إيجاد استرداد (\u200e%s: \u200e%v) صانع القيمة وسر المطابقة الذي تم التحقق منه \u200e%s",
	},
	// [token]
	TopicCancellingOrder: {
		Subject:  "إلغاء الطلب",
		Template: "تم إرسال إلغاء طلب للطلب \u200e%s",
	},
	// [token, old status, new status]
	TopicOrderStatusUpdate: {
		Subject:  "تحديث حالة الطلب",
		Template: "تمت مراجعة حالة الطلب \u200e%v من \u200e%v إلى \u200e%v",
	},
	// [count, host, token]
	TopicMatchResolutionError: {
		Subject:  "خطأ في دقة المطابقة",
		Template: "لم يتم العثور على \u200e%d تطابقات تم الإبلاغ عنها بواسطة \u200e%s لـ \u200e%s.",
	},
	// [token]
	TopicFailedCancel: {
		Subject:  "فشل الإلغاء",
		Template: "إلغاء الطلب للطلب \u200e%s عالق في حالة الحقبة الزمنية لحقبتين وتم حذفه الآن.",
	},
	// [coin ID, ticker, match]
	TopicAuditTrouble: {
		Subject:  "مشكلة في التدقيق",
		Template: "لا يزال البحث جارٍ عن عقد عملة \u200e%v (\u200e%s) الطرف الثاني للمطابقة \u200e%s. هل إتصالك بكل من الإنترنت و المحفظة جيد؟",
	},
	// [host, error]
	TopicDexAuthError: {
		Subject:  "خطأ في مصادقة منصة المبادلات اللامركزية",
		Template: "\u200e%s: \u200e%v",
	},
	// [count, host]
	TopicUnknownOrders: {
		Subject:  "أبلغت منصة المبادلات اللامركزية عن طلبات غير معروفة",
		Template: "لم يتم العثور على \u200e%d من الطلبات النشطة التي تم الإبلاغ عنها بواسطة منصة المبادلات اللامركزية \u200e%s.",
	},
	// [count]
	TopicOrdersReconciled: {
		Subject:  "تمت تسوية الطلبات مع منصة المبادلات اللامركزية",
		Template: "تم تحديث الحالات لـ \u200e%d من الطلبات.",
	},
	// [ticker, address]
	TopicWalletConfigurationUpdated: {
		Subject:  "تم تحديث تهيئة المحفظة",
		Template: "تم تحديث تهيئة المحفظة \u200e%s. عنوان الإيداع = \u200e%s",
	},
	//  [ticker]
	TopicWalletPasswordUpdated: {
		Subject:  "تم تحديث كلمة مرور المحفظة",
		Template: "تم تحديث كلمة المرور لمحفظة \u200e%s.",
	},
	// [market name, host, time]
	TopicMarketSuspendScheduled: {
		Subject:  "تمت جدولة تعليق السوق",
		Template: "تمت جدولة تعليق \u200e%v السوق \u200e%s عند \u200e%s",
	},
	// [market name, host]
	TopicMarketSuspended: {
		Subject:  "تعليق السوق",
		Template: "تم تعليق التداول في السوق \u200e%s عند \u200e%s.",
	},
	// [market name, host]
	TopicMarketSuspendedWithPurge: {
		Subject:  "تم تعليق السوق، وألغيت الطلبات",
		Template: "تم تعليق التداول في السوق \u200e%s عند \u200e%s. كما تمت إزالة جميع الطلبات المحجوزة.",
	},
	// [market name, host, time]
	TopicMarketResumeScheduled: {
		Subject:  "تمت جدولة استئناف السوق",
		Template: "تمت جدولة السوق \u200e%s في \u200e%s للاستئناف في \u200e%v",
	},
	// [market name, host, epoch]
	TopicMarketResumed: {
		Subject:  "استئناف السوق",
		Template: "استأنف السوق \u200e%s في \u200e%s التداول في الحقبة الزمنية \u200e%d",
	},
	// [host]
	TopicUpgradeNeeded: {
		Subject:  "التحديث مطلوب",
		Template: "قد تحتاج إلى تحديث عميلك للتداول في \u200e%s.",
	},
	// [host]
	TopicDEXConnected: {
		Subject:  "الخادم متصل",
		Template: "\u200e%s متصل",
	},
	// [host]
	TopicDEXDisconnected: {
		Subject:  "قطع الاتصال بالخادم",
		Template: "\u200e%s غير متصل",
	},
	// [host, rule, time, details]
	TopicPenalized: {
		Subject:  "لقد عاقبك الخادم",
		Template: "عقوبة من منصة المبادلات اللامركزية في \u200e%s\nآخر قاعدة مكسورة: \u200e%s\nالوقت: \u200e%v\nالتفاصيل:\n\"\u200e%s\"\n",
	},
	TopicSeedNeedsSaving: {
		Subject:  "لا تنس عمل نسخة احتياطية من بذرة التطبيق",
		Template: "تم إنشاء بذرة تطبيق جديدة. قم بعمل نسخة احتياطية الآن في عرض الإعدادات.",
	},
	TopicUpgradedToSeed: {
		Subject:  "قم بعمل نسخة احتياطية من بذرة التطبيق الجديدة",
		Template: "تم تحديث العميل لاستخدام بذرة التطبيق. قم بعمل نسخة احتياطية من البذرة الآن في عرض الإعدادات.",
	},
	// [host, msg]
	TopicDEXNotification: {
		Subject:  "رسالة من منصة المبادلات اللامركزية",
		Template: "\u200e%s: \u200e%s",
	},
	// [parentSymbol, tokenSymbol]
	TopicQueuedCreationFailed: {
		Subject:  "فشل إنشاء توكن المحفظة",
		Template: "بعد إنشاء محفظة \u200e%s، فشل في إنشاء \u200e%s للمحفظة",
	},
	TopicRedemptionResubmitted: {
		Subject:  "أعيد تقديم المبلغ المسترد",
		Template: "تمت إعادة إرسال مبلغك المسترد للمطابقة \u200e%s في الطلب .\u200e%s",
	},
	TopicSwapRefunded: {
		Subject:  "مقايضة مستردة",
		Template: "تمت استعادة تطابق \u200e%s للطلب \u200e%s من قبل الطرف الثاني.",
	},
	TopicRedemptionConfirmed: {
		Subject:  "تم تأكيد الاسترداد",
		Template: "تم تأكيد استردادك للمطابقة \u200e%s للطلب \u200e%s",
	},
}

// The language string key *must* parse with language.Parse.
// var locales = map[string]map[Topic]*translation{
// 	originLang: originLocale,
// 	"pt-BR":    ptBR,
// 	"zh-CN":    zhCN,
// 	"pl-PL":    plPL,
// 	"de-DE":    deDE,
// 	"ar":       ar,
// }

func registerLocale(lang language.Tag, dict map[Topic]*i18n.Translation) {
	t := translator.LanguageTranslator(lang)
	for topic, tln := range dict {
		t.RegisterNotifications(string(topic), tln)
	}
}

func init() {
	for topic, tln := range originLocale {
		translator.RegisterNotifications(string(topic), tln)
	}
	registerLocale(language.BrazilianPortuguese, ptBR)
	registerLocale(language.SimplifiedChinese, zhCN)
	registerLocale(language.Polish, plPL)
	registerLocale(language.German, deDE)
}
