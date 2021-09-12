package core

import (
	"fmt"

	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

type translation struct {
	subject  string
	template string
}

// enUS is the American English translations.
var enUS = map[Topic]*translation{
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
	// [token]
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
	// [host, msg]
	TopicDEXNotification: {
		subject:  "Message from DEX",
		template: "%s: %s",
	},
}

var ptBR = map[Topic]*translation{
	// [host]
	TopicAccountRegistered: {
		subject:  "Conta Registrada",
		template: "Você agora pode trocar em %s",
	},
	// [confs, host]
	TopicFeePaymentInProgress: {
		subject:  "Pagamento da Taxa em andamento",
		template: "Esperando por %d confirmações antes de trocar em %s",
	},
	// [confs, required confs]
	TopicRegUpdate: {
		subject:  "Atualização de registro",
		template: "Confirmações da taxa %v/%v",
	},
	// [host, error]
	TopicFeePaymentError: {
		subject:  "Erro no Pagamento da Taxa",
		template: "Erro enquanto pagando taxa para %s: %v",
	},
	// [host, error]
	TopicAccountUnlockError: {
		subject:  "Erro ao Destrancar carteira",
		template: "erro destrancando conta %s: %v",
	},
	// [host]
	TopicFeeCoinError: {
		subject:  "Erro na Taxa",
		template: "Taxa vazia para %s.",
	},
	// [host]
	TopicWalletConnectionWarning: {
		subject:  "Aviso de Conexão com a Carteira",
		template: "Registro incompleto detectado para %s, mas falhou ao conectar com carteira decred",
	},
	// [host, error]
	TopicWalletUnlockError: {
		subject:  "Erro ao Destravar Carteira",
		template: "Conectado com carteira decred para completar o registro em %s, mas falha ao destrancar: %v",
	},
	// [ticker, error]
	TopicWithdrawError: {
		subject:  "Erro Retirada",
		template: "Erro encontrado durante retirada de %s: %v",
	},
	// [ticker, coin ID]
	TopicWithdrawSend: {
		template: "Retirada de %s foi completada com sucesso. ID da moeda = %s",
		subject:  "Retirada Enviada",
	},
	// [error]
	TopicOrderLoadFailure: {
		template: "Alguns pedidos falharam ao carregar da base de dados: %v",
		subject:  "Carregamendo de Pedidos Falhou",
	},
	// [qty, ticker, token]
	TopicYoloPlaced: {
		template: "vendendo %.8f %s a taxa de mercado (%s)",
		subject:  "Ordem de Mercado Colocada",
	},
	// [sell string, qty, ticker, rate string, token]
	TopicOrderPlaced: {
		subject:  "Ordem Colocada",
		template: "%sing %.8f %s, valor = %s (%s)",
	},
	// [missing count, token, host]
	TopicMissingMatches: {
		template: "%d combinações para pedidos %s não foram reportados por %q e foram considerados revocados",
		subject:  "Pedidos Faltando Combinações",
	},
	// [token, error]
	TopicWalletMissing: {
		template: "Erro ao recuperar pedidos ativos por carteira %s: %v",
		subject:  "Carteira Faltando",
	},
	// [side, token, match status]
	TopicMatchErrorCoin: {
		subject:  "Erro combinação de Moedas",
		template: "Combinação %s para pedido %s está no estado %s, mas não há um executador para trocar moedas.",
	},
	// [side, token, match status]
	TopicMatchErrorContract: {
		template: "Combinação %s para pedido %s está no estado %s, mas não há um executador para trocar moedas.",
		subject:  "Erro na Combinação de Contrato",
	},
	// [ticker, contract, token, error]
	TopicMatchRecoveryError: {
		template: "Erro auditando contrato de troca da contraparte (%s %v) durante troca recuperado no pedido %s: %v",
		subject:  "Erro Recuperando Combinações",
	},
	// [token]
	TopicOrderCoinError: {
		template: "Não há Moedas de financiamento registradas para pedidos ativos %s",
		subject:  "Erro no Pedido da Moeda",
	},
	// [token, ticker, error]
	TopicOrderCoinFetchError: {
		template: "Erro ao recuperar moedas de origem para pedido %s (%s): %v",
		subject:  "Erro na Recuperação do Pedido de Moedas",
	},
	// [token]
	TopicMissedCancel: {
		template: "Pedido de cancelamento não combinou para pedido %s. Isto pode acontecer se o pedido de cancelamento foi enviado no mesmo epoque do que a troca ou se o pedido foi completamente executado antes da ordem de cancelamento ser executada.",
		subject:  "Cancelamento Perdido",
	},
	// [capitalized sell string, base ticker, quote ticker, host, token]
	TopicOrderCanceled: {
		template: "%s pedido sobre %s-%s em %s foi cancelado (%s)",
		subject:  "Cancelamento de Pedido",
	},
	// [capitalized sell string, base ticker, quote ticker, fill percent, token]
	TopicMatchesMade: {
		template: "%s pedido sobre %s-%s %.1f%% preenchido (%s)",
		subject:  "Combinações Feitas",
	},
	// [qty, ticker, token]
	TopicSwapSendError: {
		template: "Erro encontrado ao enviar a troca com output(s) no valor de %.8f %s no pedido %s",
		subject:  "Erro ao Enviar Troca",
	},
	// [match, error]
	TopicInitError: {
		template: "Erro notificando DEX da troca %s por combinação: %v",
		subject:  "Erro na Troca",
	},
	// [match, error]
	TopicReportRedeemError: {
		template: "Erro notificando DEX da redenção %s por combinação: %v",
		subject:  "Reportando Erro na redenção",
	},
	// [qty, ticker, token]
	TopicSwapsInitiated: {
		template: "Enviar trocas no valor de %.8f %s no pedido %s",
		subject:  "Trocas Iniciadas",
	},
	// [qty, ticker, token]
	TopicRedemptionError: {
		template: "Erro encontrado enviado redenção no valor de %.8f %s no pedido %s",
		subject:  "Erro na Redenção",
	},
	// [qty, ticker, token]
	TopicMatchComplete: {
		template: "Resgatado %.8f %s no pedido %s",
		subject:  "Combinação Completa",
	},
	// [qty, ticker, token]
	TopicRefundFailure: {
		template: "Devolvidos %.8f %s no pedido %s, com algum erro",
		subject:  "Erro no Reembolso",
	},
	// [qty, ticker, token]
	TopicMatchesRefunded: {
		template: "Devolvidos %.8f %s no pedido %s",
		subject:  "Reembolso Sucedido",
	},
	// [match ID token]
	TopicMatchRevoked: {
		template: "Combinação %s foi revocada",
		subject:  "Combinação Revocada",
	},
	// [token, market name, host]
	TopicOrderRevoked: {
		template: "Pedido %s no mercado %s em %s foi revocado pelo servidor",
		subject:  "Pedido Revocado",
	},
	// [token, market name, host]
	TopicOrderAutoRevoked: {
		template: "Pedido %s no mercado %s em %s revocado por suspenção do mercado",
		subject:  "Pedido Revocado Automatiamente",
	},
	// [ticker, coin ID, match]
	TopicMatchRecovered: {
		template: "Encontrado redenção do executador (%s: %v) e validado segredo para pedido %s",
		subject:  "Pedido Recuperado",
	},
	// [token]
	TopicCancellingOrder: {
		template: "Uma ordem de cancelamento foi submetida para o pedido %s",
		subject:  "Cancelando Pedido",
	},
	// [token, old status, new status]
	TopicOrderStatusUpdate: {
		template: "Status do pedido %v revisado de %v para %v",
		subject:  "Status do Pedido Atualizado",
	},
	// [count, host, token]
	TopicMatchResolutionError: {
		template: "%d combinações reportada para %s não foram encontradas para %s.",
		subject:  "Erro na Resolução do Pedido",
	},
	// [token]
	TopicFailedCancel: {
		template: "Ordem de cancelamento para pedido %s presa em estado de Epoque por 2 epoques e foi agora deletado.",
		subject:  "Falhou Cancelamento",
	},
	// [coin ID, ticker, match]
	TopicAuditTrouble: {
		template: "Continua procurando por contrato de contrapartes para moeda %v (%s) para combinação %s. Sua internet e conexão com a carteira estão ok?",
		subject:  "Problemas ao Auditar",
	},
	// [host, error]
	TopicDexAuthError: {
		template: "%s: %v",
		subject:  "Erro na Autenticação",
	},
	// [count, host]
	TopicUnknownOrders: {
		template: "%d pedidos ativos reportados pela DEX %s não foram encontrados.",
		subject:  "DEX Reportou Pedidos Desconhecidos",
	},
	// [count]
	TopicOrdersReconciled: {
		template: "Estados atualizados para %d pedidos.",
		subject:  "Pedidos Reconciliados com DEX",
	},
	// [ticker, address]
	TopicWalletConfigurationUpdated: {
		template: "configuração para carteira %s foi atualizada. Endereço de depósito = %s",
		subject:  "Configurações da Carteira Atualizada",
	},
	//  [ticker]
	TopicWalletPasswordUpdated: {
		template: "Senha para carteira %s foi atualizada.",
		subject:  "Senha da Carteira Atualizada",
	},
	// [market name, host, time]
	TopicMarketSuspendScheduled: {
		template: "Mercado %s em %s está agora agendado para suspensão em %v",
		subject:  "Suspensão de Mercado Agendada",
	},
	// [market name, host]
	TopicMarketSuspended: {
		template: "Trocas no mercado %s em %s está agora suspenso.",
		subject:  "Mercado Suspenso",
	},
	// [market name, host]
	TopicMarketSuspendedWithPurge: {
		template: "Trocas no mercado %s em %s está agora suspenso. Todos pedidos no livro de ofertas foram agora EXPURGADOS.",
		subject:  "Mercado Suspenso, Pedidos Expurgados",
	},
	// [market name, host, time]
	TopicMarketResumeScheduled: {
		template: "Mercado %s em %s está agora agendado para resumir em %v",
		subject:  "Resumo do Mercado Agendado",
	},
	// [market name, host, epoch]
	TopicMarketResumed: {
		template: "Mercado %s em %s foi resumido para trocas no epoque %d",
		subject:  "Mercado Resumido",
	},
	// [host]
	TopicUpgradeNeeded: {
		template: "Você pode precisar atualizar seu cliente para trocas em %s.",
		subject:  "Atualização Necessária",
	},
	// [host]
	TopicDEXConnected: {
		subject:  "DEX conectado",
		template: "%s está conectado",
	},
	// [host]
	TopicDEXDisconnected: {
		template: "%s está desconectado",
		subject:  "Server Disconectado",
	},
	// [host, rule, time, details]
	TopicPenalized: {
		template: "Penalidade de DEX em %s\núltima regra quebrada: %s\nhorário: %v\ndetalhes:\n\"%s\"\n",
		subject:  "Server Penalizou Você",
	},
	TopicSeedNeedsSaving: {
		subject:  "Não se esqueça de guardar a seed do app",
		template: "Uma nova seed para a aplicação foi criada. Faça um backup agora na página de configurações.",
	},
	TopicUpgradedToSeed: {
		subject:  "Guardar nova seed do app",
		template: "O cliente foi atualizado para usar uma seed. Faça backup dessa seed na página de configurações.",
	},
	// [host, msg]
	TopicDEXNotification: {
		subject:  "Mensagem da DEX",
		template: "%s: %s",
	},
}

var locales = map[string]map[Topic]*translation{
	language.AmericanEnglish.String():     enUS,
	language.BrazilianPortuguese.String(): ptBR,
}

func init() {
	for lang, translations := range locales {
		for topic, translation := range translations {
			err := message.SetString(language.Make(lang), string(topic), translation.template)
			if err != nil {
				panic(fmt.Sprintf("SetString(%s): %v", lang, err))
			}
		}
	}
}
