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
		template: "Connected to wallet to complete registration at %s, but failed to unlock: %v",
	},
	// [asset name, error message]
	TopicWalletCommsWarning: {
		subject:  "Wallet connection issue",
		template: "Unable to communicate with %v wallet! Reason: %q",
	},
	// [asset name]
	TopicWalletPeersWarning: {
		subject:  "Wallet network issue",
		template: "%v wallet has no network peers!",
	},
	// [asset name]
	TopicWalletPeersRestored: {
		subject:  "Wallet connectivity restored",
		template: "%v wallet has reestablished connectivity.",
	},
	// [ticker, error]
	TopicSendError: {
		subject:  "Send error",
		template: "Error encountered while sending %s: %v",
	},
	// [ticker, coin ID]
	TopicSendSuccess: {
		subject:  "Send Successful",
		template: "Sending %s has completed successfully. Coin ID = %s",
	},
	// [error]
	TopicOrderLoadFailure: {
		subject:  "Order load failure",
		template: "Some orders failed to load from the database: %v",
	},
	// [qty, ticker, token]
	TopicYoloPlaced: {
		subject:  "Market order placed",
		template: "selling %s %s at market rate (%s)",
	},
	// [sell string, qty, ticker, rate string, token]
	TopicOrderPlaced: {
		subject:  "Order placed",
		template: "%sing %s %s, rate = %s (%s)",
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
		template: "Error encountered sending a swap output(s) worth %s %s on order %s",
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
		template: "Sent swaps worth %s %s on order %s",
	},
	// [qty, ticker, token]
	TopicRedemptionError: {
		subject:  "Redemption error",
		template: "Error encountered sending redemptions worth %s %s on order %s",
	},
	// [qty, ticker, token]
	TopicMatchComplete: {
		subject:  "Match complete",
		template: "Redeemed %s %s on order %s",
	},
	// [qty, ticker, token]
	TopicRefundFailure: {
		subject:  "Refund Failure",
		template: "Refunded %s %s on order %s, with some errors",
	},
	// [qty, ticker, token]
	TopicMatchesRefunded: {
		subject:  "Matches Refunded",
		template: "Refunded %s %s on order %s",
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
	// [parentSymbol, tokenSymbol]
	TopicQueuedCreationFailed: {
		subject:  "Failed to create token wallet",
		template: "After creating %s wallet, failed to create the %s wallet",
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
		template: "Conectado com carteira para completar o registro em %s, mas falha ao destrancar: %v",
	},
	// [ticker, error], RETRANSLATE.
	TopicSendError: {
		subject:  "Erro Retirada",
		template: "Erro encontrado durante retirada de %s: %v",
	},
	// [ticker, coin ID], RETRANSLATE.
	TopicSendSuccess: {
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
		template: "vendendo %s %s a taxa de mercado (%s)",
		subject:  "Ordem de Mercado Colocada",
	},
	// [sell string, qty, ticker, rate string, token]
	TopicOrderPlaced: {
		subject:  "Ordem Colocada",
		template: "%sing %s %s, valor = %s (%s)",
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
		template: "Erro encontrado ao enviar a troca com output(s) no valor de %s %s no pedido %s",
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
		template: "Enviar trocas no valor de %s %s no pedido %s",
		subject:  "Trocas Iniciadas",
	},
	// [qty, ticker, token]
	TopicRedemptionError: {
		template: "Erro encontrado enviado redenção no valor de %s %s no pedido %s",
		subject:  "Erro na Redenção",
	},
	// [qty, ticker, token]
	TopicMatchComplete: {
		template: "Resgatado %s %s no pedido %s",
		subject:  "Combinação Completa",
	},
	// [qty, ticker, token]
	TopicRefundFailure: {
		template: "Devolvidos %s %s no pedido %s, com algum erro",
		subject:  "Erro no Reembolso",
	},
	// [qty, ticker, token]
	TopicMatchesRefunded: {
		template: "Devolvidos %s %s no pedido %s",
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

// zhCN is the Simplified Chinese (PRC) translations.
var zhCN = map[Topic]*translation{
	// [host]
	TopicAccountRegistered: {
		subject:  "注册账户",
		template: "您现在可以在 %s 进行交易", // alt. 您现在可以切换到 %s
	},
	// [confs, host]
	TopicFeePaymentInProgress: {
		subject:  "费用支付中",
		template: "在切换到 %s 之前等待 %d 次确认", // alt. 在 %s 交易之前等待 %d 确认
	},
	// [confs, required confs]
	TopicRegUpdate: {
		subject:  "费用支付确认", // alt. 记录更新 (but not displayed)
		template: "%v/%v 费率确认",
	},
	// [host, error]
	TopicFeePaymentError: {
		subject:  "费用支付错误",
		template: "向 %s 支付费用时遇到错误: %v", // alt. 为 %s 支付费率时出错：%v
	},
	// [host, error]
	TopicAccountUnlockError: {
		subject:  "解锁钱包时出错",
		template: "解锁帐户 %s 时出错： %v", // alt. 解锁 %s 的帐户时出错: %v
	},
	// [host]
	TopicFeeCoinError: {
		subject:  "汇率错误",
		template: "%s 的空置率。", // alt. %s 的费用硬币为空。
	},
	// [host]
	TopicWalletConnectionWarning: {
		subject:  "钱包连接通知",
		template: "检测到 %s 的注册不完整，无法连接 decred 钱包", // alt. 检测到 %s 的注册不完整，无法连接到 Decred 钱包
	},
	// [host, error]
	TopicWalletUnlockError: {
		subject:  "解锁钱包时出错",
		template: "与 decred 钱包连接以在 %s 上完成注册，但无法解锁： %v", // alt. 已连接到 Decred 钱包以在 %s 完成注册，但无法解锁：%v
	},
	// [ticker, error], RETRANSLATE.
	TopicSendError: {
		subject:  "提款错误",
		template: "在 %s 提取过程中遇到错误: %v", // alt. 删除 %s 时遇到错误： %v
	},
	// [ticker, coin ID], RETRANSLATE.
	TopicSendSuccess: {
		subject:  "提款已发送",
		template: "%s 的提款已成功完成。硬币 ID = %s",
	},
	// [error]
	TopicOrderLoadFailure: {
		subject:  "请求加载失败",
		template: "某些订单无法从数据库加载：%v", // alt. 某些请求无法从数据库加载:
	},
	// [qty, ticker, token]
	TopicYoloPlaced: {
		subject:  "下达市价单",
		template: "以市场价格 (%[3]s) 出售 %[1]s %[2]s",
	},
	// [sell string, qty, ticker, rate string, token]
	TopicOrderPlaced: {
		subject:  "已下订单",
		template: "%sing %s %s，值 = %s (%s)", // figure out the "ing" issue (TODO)
	},
	// [missing count, token, host]
	TopicMissingMatches: {
		subject:  "订单缺失匹配",
		template: "%[2]s 订单的 %[1]d 匹配项未被 %[3]q 报告并被视为已撤销", // alt. %d 订单 %s 的匹配没有被 %q 报告并被视为已撤销
	},
	// [token, error]
	TopicWalletMissing: {
		subject:  "丢失的钱包",
		template: "活动订单 %s 的钱包检索错误： %v", // alt. 通过钱包 %s 检索活动订单时出错: %v
	},
	// [side, token, match status]
	TopicMatchErrorCoin: {
		subject:  "货币不匹配错误",
		template: "订单 %s 的组合 %s 处于状态 %s，但没有用于交换货币的运行程序。", // alt. 订单 %s 的匹配 %s 处于状态 %s，但没有交换硬币服务商。
	},
	// [side, token, match status]
	TopicMatchErrorContract: {
		subject:  "合约组合错误",
		template: "订单 %s 的匹配 %s 处于状态 %s，没有服务商交换合约。",
	},
	// [ticker, contract, token, error]
	TopicMatchRecoveryError: {
		subject:  "检索匹配时出错",
		template: "在检索订单 %s: %v 的交易期间审核交易对手交易合约 (%s %v) 时出错", // ? 在订单 %s: %v 的交易恢复期间审核对方的交易合约 (%s %v) 时出错
	},
	// [token]
	TopicOrderCoinError: {
		subject:  "硬币订单错误",
		template: "没有为活动订单 %s 记录资金硬币", // alt. 没有为活动订单 %s 注册资金货币
	},
	// [token, ticker, error]
	TopicOrderCoinFetchError: {
		subject:  "硬币订单恢复错误",
		template: "检索订单 %s (%s) 的源硬币时出错： %v", // alt. 订单 %s (%s) 的源硬币检索错误: %v
	},
	// [token]
	TopicMissedCancel: {
		subject:  "丢失取消",
		template: "取消订单与订单 %s 不匹配。如果取消订单与交易所同时发送，或者订单在取消订单执行之前已完全执行，则可能发生这种情况。",
	},
	// [capitalized sell string, base ticker, quote ticker, host, token]
	TopicOrderCanceled: {
		subject:  "订单取消",
		template: "%s 的 %s-%s 的 %s 订单已被取消 (%s)", // alt. %s 上 %s-%s 上的 %s 请求已被取消 (%s)
	},
	// [capitalized sell string, base ticker, quote ticker, fill percent, token]
	TopicMatchesMade: {
		subject:  "匹配完成",
		template: "%s 订单 %s-%s %.1f%% 已完成 (%s)", // alt. %s 请求超过 %s-%s %.1f%% 已填充（%s）
	},
	// [qty, ticker, token]
	TopicSwapSendError: {
		subject:  "发送交换时出错",
		template: "在以 %[3]s 的顺序发送价值 %[1]s %[2]s 的输出的交换时遇到错误", // ? 在订单 %s 上发送价值 %.8f %s 的交换输出时遇到错误
	},
	// [match, error]
	TopicInitError: {
		subject:  "交换错误",
		template: "通知 DEX 匹配 %s 的交换时出错： %v", // alt. 错误通知 DEX %s 交换组合：%v
	},
	// [match, error]
	TopicReportRedeemError: {
		subject:  "报销错误",
		template: "通知 DEX %s 赎回时出错： %v",
	},
	// [qty, ticker, token]
	TopicSwapsInitiated: {
		subject:  "发起交流",
		template: "在订单 %[3]s 上发送价值 %[1]s %[2]s 的交易", // should mention "contract" (TODO) ? 已发送价值 %.8f %s 的交易，订单 %s
	},
	// [qty, ticker, token]
	TopicRedemptionError: {
		subject:  "赎回错误",
		template: "在订单 %[3]s 上发送价值 %[1]s %[2]s 的兑换时遇到错误", // alt. 在订单 %s 上发现发送价值 %.8f %s 的赎回错误
	},
	// [qty, ticker, token]
	TopicMatchComplete: {
		subject:  "完全匹配",
		template: "在订单 %s 上兑换了 %s %s",
	},
	// [qty, ticker, token]
	TopicRefundFailure: {
		subject:  "退款错误",
		template: "按顺序 %[3]s 返回 %[1]s %[2]s，有一些错误", // alt. 退款％.8f％s的订单％S，但出现一些错误
	},
	// [qty, ticker, token]
	TopicMatchesRefunded: {
		subject:  "退款成功",
		template: "在订单 %[3]s 上返回了 %[1]s %[2]s", // 在订单 %s 上返回了 %.8f %s
	},
	// [match ID token]
	TopicMatchRevoked: {
		subject:  "撤销组合",
		template: "匹配 %s 已被撤销", // alt. 组合 %s 已被撤销
	},
	// [token, market name, host]
	TopicOrderRevoked: {
		subject:  "撤销订单",
		template: "%s 市场 %s 的订单 %s 已被服务器撤销",
	},
	// [token, market name, host]
	TopicOrderAutoRevoked: {
		subject:  "订单自动撤销",
		template: "%s 市场 %s 上的订单 %s 由于市场暂停而被撤销", // alt. %s 市场 %s 中的订单 %s 被市场暂停撤销
	},
	// [ticker, coin ID, match]
	TopicMatchRecovered: {
		subject:  "恢复订单",
		template: "找到赎回 (%s: %v) 并验证了请求 %s 的秘密",
	},
	// [token]
	TopicCancellingOrder: {
		subject:  "取消订单",
		template: "已为订单 %s 提交了取消操作", // alt. 已为订单 %s 提交取消订单
	},
	// [token, old status, new status]
	TopicOrderStatusUpdate: {
		subject:  "订单状态更新",
		template: "订单 %v 的状态从 %v 修改为 %v", // alt. 订单状态 %v 从 %v 修改为 %v
	},
	// [count, host, token]
	TopicMatchResolutionError: {
		subject:  "订单解析错误",
		template: "没有为 %[3]s 找到为 %[2]s 报告的 %[1]d 个匹配项。请联系Decred社区以解决该问题。", // alt. %s 报告的 %d 个匹配项没有找到 %s。
	},
	// [token]
	TopicFailedCancel: {
		subject:  "取消失败",
		template: "取消订单 %s 的订单 %s 处于 Epoque 状态 2 个 epoques，现在已被删除。",
	},
	// [coin ID, ticker, match]
	TopicAuditTrouble: {
		subject:  "审计时的问题",
		template: "继续寻找组合 %[3]s 的货币 %[1]v (%[2]s) 的交易对手合约。您的互联网和钱包连接是否正常？",
	},
	// [host, error]
	TopicDexAuthError: {
		subject:  "身份验证错误",
		template: "%s: %v",
	},
	// [count, host]
	TopicUnknownOrders: {
		subject:  "DEX 报告的未知请求",
		template: "未找到 DEX %[2]s 报告的 %[1]d 个活动订单。",
	},
	// [count]
	TopicOrdersReconciled: {
		subject:  "与 DEX 协调的订单",
		template: "%d 个订单的更新状态。", // alt. %d 个订单的状态已更新。
	},
	// [ticker, address]
	TopicWalletConfigurationUpdated: {
		subject:  "更新的钱包设置a",
		template: "钱包 %[1]s 的配置已更新。存款地址 = %[2]s", // alt. %s 钱包的配置已更新。存款地址 = %s
	},
	//  [ticker]
	TopicWalletPasswordUpdated: {
		subject:  "钱包密码更新",
		template: "钱包 %s 的密码已更新。", // alt. %s 钱包的密码已更新。
	},
	// [market name, host, time]
	TopicMarketSuspendScheduled: {
		subject:  "市场暂停预定",
		template: "%s 上的市场 %s 现在计划在 %v 暂停",
	},
	// [market name, host]
	TopicMarketSuspended: {
		subject:  "暂停市场",
		template: "%s 的 %s 市场交易现已暂停。", // alt. %s 市场 %s 的交易现已暂停。
	},
	// [market name, host]
	TopicMarketSuspendedWithPurge: {
		subject:  "暂停市场，清除订单",
		template: "%s 的市场交易 %s 现已暂停。订单簿中的所有订单现已被删除。", // alt. %s 市场 %s 的交易现已暂停。所有预订的订单现在都已清除。
	},
	// [market name, host, time]
	TopicMarketResumeScheduled: {
		subject:  "预定市场摘要",
		template: "%s 上的市场 %s 现在计划在 %v 恢",
	},
	// [market name, host, epoch]
	TopicMarketResumed: {
		subject:  "总结市场",
		template: "%[2]s 上的市场 %[1]s 已汇总用于时代 %[3]d 中的交易", // alt. M%s 的市场 %s 已在epoch %d 恢复交易
	},
	// [host]
	TopicUpgradeNeeded: {
		subject:  "需要更新",
		template: "您可能需要更新您的帐户以进行 %s 的交易。", // alt. 您可能需要更新您的客户端以在 %s 进行交易。
	},
	// [host]
	TopicDEXConnected: {
		subject:  "DEX 连接",
		template: "%s 已连接",
	},
	// [host]
	TopicDEXDisconnected: {
		subject:  "服务器断开连接",
		template: "%s 离线", // alt. %s 已断开连接
	},
	// [host, rule, time, details]
	TopicPenalized: {
		subject:  "服务器惩罚了你",
		template: "%s 上的 DEX 惩罚\n最后一条规则被破坏：%s \n时间： %v \n详细信息：\n \" %s \" \n",
	},
	TopicSeedNeedsSaving: {
		subject:  "不要忘记备份你的应用程序种子", // alt. 别忘了备份应用程序种子
		template: "已创建新的应用程序种子。请立刻在设置界面中进行备份。",
	},
	TopicUpgradedToSeed: {
		subject:  "备份您的新应用程序种子",                   // alt. 备份新的应用程序种子
		template: "客户端已升级为使用应用程序种子。请切换至设置界面备份种子。", // alt. 客户端已升级。请在“设置”界面中备份种子。
	},
	// [host, msg]
	TopicDEXNotification: {
		subject:  "来自DEX的消息",
		template: "%s: %s",
	},
}

var plPL = map[Topic]*translation{
	// [host]
	TopicAccountRegistered: {
		subject:  "Konto zarejestrowane",
		template: "Możesz teraz handlować na %s",
	},
	// [confs, host]
	TopicFeePaymentInProgress: {
		subject:  "Opłata rejestracyjna w drodze",
		template: "Oczekiwanie na %d potwierdzeń przed rozpoczęciem handlu na %s",
	},
	// [confs, required confs]
	TopicRegUpdate: {
		subject:  "Aktualizacja rejestracji",
		template: "Potwierdzenia opłaty rejestracyjnej %v/%v",
	},
	// [host, error]
	TopicFeePaymentError: {
		subject:  "Błąd płatności rejestracyjnej",
		template: "Wystąpił błąd przy płatności dla %s: %v",
	},
	// [host, error]
	TopicAccountUnlockError: {
		subject:  "Błąd odblokowywania konta",
		template: "błąd odblokowywania konta dla %s: %v",
	},
	// [host]
	TopicFeeCoinError: {
		subject:  "Błąd w płatności rejestracyjnej",
		template: "Nie znaleziono środków na płatność rejestracyjną dla %s.",
	},
	// [host]
	TopicWalletConnectionWarning: {
		subject:  "Ostrzeżenie połączenia z portfelem",
		template: "Wykryto niedokończoną rejestrację dla %s, ale nie można połączyć się z portfelem Decred",
	},
	// [host, error]
	TopicWalletUnlockError: {
		subject:  "Błąd odblokowywania portfela",
		template: "Połączono z portfelem Decred, aby dokończyć rejestrację na %s, lecz próba odblokowania portfela nie powiodła się: %v",
	},
	// [ticker, error], RETRANSLATE.
	TopicSendError: {
		subject:  "Błąd wypłaty środków",
		template: "Wystąpił błąd przy wypłacaniu %s: %v",
	},
	// [ticker, coin ID], RETRANSLATE.
	TopicSendSuccess: {
		subject:  "Wypłata zrealizowana",
		template: "Wypłata %s została zrealizowana pomyślnie. ID monety = %s",
	},
	// [error]
	TopicOrderLoadFailure: {
		subject:  "Błąd wczytywania zleceń",
		template: "Niektórych zleceń nie udało się wczytać z bazy danych: %v",
	},
	// [qty, ticker, token]
	TopicYoloPlaced: {
		subject:  "Złożono zlecenie rynkowe",
		template: "sprzedaż %s %s po kursie rynkowym (%s)",
	},
	// [sell string, qty, ticker, rate string, token]
	TopicOrderPlaced: {
		subject:  "Złożono zlecenie",
		template: "%sing %s %s, kurs = %s (%s)",
	},
	// [missing count, token, host]
	TopicMissingMatches: {
		subject:  "Brak spasowanych zamówień",
		template: "%d spasowań dla zlecenia %s nie zostało odnotowanych przez %q i są uznane za unieważnione",
	},
	// [token, error]
	TopicWalletMissing: {
		subject:  "Brak portfela",
		template: "Błąd odczytu z portfela dla aktywnego zlecenia %s: %v",
	},
	// [side, token, match status]
	TopicMatchErrorCoin: {
		subject:  "Błąd spasowanej monety",
		template: "Spasowanie %s dla zlecenia %s jest w stanie %s, lecz brakuje monety po stronie maker.",
	},
	// [side, token, match status]
	TopicMatchErrorContract: {
		subject:  "Błąd kontraktu spasowania",
		template: "Spasowanie %s dla zlecenia %s jest w stanie %s, lecz brakuje kontraktu zamiany po stronie maker.",
	},
	// [ticker, contract, token, error]
	TopicMatchRecoveryError: {
		subject:  "Błąd odzyskiwania spasowania",
		template: "Błąd przy audycie kontraktu zamiany u kontrahenta (%s %v) podczas odzyskiwania zamiany dla zlecenia %s: %v",
	},
	// [token]
	TopicOrderCoinError: {
		subject:  "Błąd monety dla zlecenia",
		template: "Nie znaleziono środków fundujących dla aktywnego zlecenia %s",
	},
	// [token, ticker, error]
	TopicOrderCoinFetchError: {
		subject:  "Błąd pozyskania środków dla zlecenia",
		template: "Błąd pozyskania środków źródłowych dla zlecenia %s (%s): %v",
	},
	// [token]
	TopicMissedCancel: {
		subject:  "Spóźniona anulacja",
		template: "Zlecenie anulacji nie zostało spasowane dla zlecenia %s. Może to mieć miejsce, gdy zlecenie anulacji wysłane jest w tej samej epoce, co zlecenie handlu, lub gdy zlecenie handlu zostaje w pełni wykonane przed spasowaniem ze zleceniem anulacji.",
	},
	// [capitalized sell string, base ticker, quote ticker, host, token]
	TopicOrderCanceled: {
		subject:  "Zlecenie anulowane",
		template: "Zlecenie %s dla %s-%s na %s zostało anulowane (%s)",
	},
	// [capitalized sell string, base ticker, quote ticker, fill percent, token]
	TopicMatchesMade: {
		subject:  "Dokonano spasowania",
		template: "Zlecenie %s na %s-%s zrealizowane w %.1f%% (%s)",
	},
	// [qty, ticker, token]
	TopicSwapSendError: {
		subject:  "Błąd wysyłki środków",
		template: "Błąd przy wysyłaniu środków wartych %s %s dla zlecenia %s",
	},
	// [match, error]
	TopicInitError: {
		subject:  "Błąd raportowania zamiany",
		template: "Błąd powiadomienia DEX o zamianie dla spasowania %s: %v",
	},
	// [match, error]
	TopicReportRedeemError: {
		subject:  "Błąd raportowania wykupienia",
		template: "Błąd powiadomienia DEX o wykupieniu środków dla spasowania %s: %v",
	},
	// [qty, ticker, token]
	TopicSwapsInitiated: {
		subject:  "Zamiana rozpoczęta",
		template: "Wysłano środki o wartości %s %s dla zlecenia %s",
	},
	// [qty, ticker, token]
	TopicRedemptionError: {
		subject:  "Błąd wykupienia",
		template: "Napotkano błąd przy wykupywaniu środków o wartości %s %s dla zlecenia %s",
	},
	// [qty, ticker, token]
	TopicMatchComplete: {
		subject:  "Spasowanie zakończone",
		template: "Wykupiono %s %s ze zlecenia %s",
	},
	// [qty, ticker, token]
	TopicRefundFailure: {
		subject:  "Niepowodzenie zwrotu środków",
		template: "Zwrócono %s %s za zlecenie %s, z pewnymi błędami",
	},
	// [qty, ticker, token]
	TopicMatchesRefunded: {
		subject:  "Zwrot środków za spasowanie zleceń",
		template: "Zwrócono %s %s za zlecenie %s",
	},
	// [match ID token]
	TopicMatchRevoked: {
		subject:  "Spasowanie zleceń unieważnione",
		template: "Spasowanie %s zostało unieważnione",
	},
	// [token, market name, host]
	TopicOrderRevoked: {
		subject:  "Zlecenie unieważnione",
		template: "Zlecenie %s na rynku %s na %s zostało unieważnione przez serwer",
	},
	// [token, market name, host]
	TopicOrderAutoRevoked: {
		subject:  "Zlecenie unieważnione automatycznie",
		template: "Zlecenie %s na rynku %s na %s zostało unieważnione z powodu wstrzymania handlu na tym rynku",
	},
	// [ticker, coin ID, match]
	TopicMatchRecovered: {
		subject:  "Odzyskano spasowanie",
		template: "Odnaleziono wykup ze strony maker (%s: %v) oraz potwierdzono sekret dla spasowania %s",
	},
	// [token]
	TopicCancellingOrder: {
		subject:  "Anulowanie zlecenia",
		template: "Złożono polecenie anulowania dla zlecenia %s",
	},
	// [token, old status, new status]
	TopicOrderStatusUpdate: {
		subject:  "Aktualizacja statusu zlecenia",
		template: "Status zlecenia %v został zmieniony z %v na %v",
	},
	// [count, host, token]
	TopicMatchResolutionError: {
		subject:  "Błąd rozstrzygnięcia spasowania",
		template: "Nie znaleziono %d spasowań odnotowanych przez %s dla %s.",
	},
	// [token]
	TopicFailedCancel: {
		subject:  "Niepowodzenie anulowania",
		template: "Zlecenie anulacji dla zlecenia %s utknęło w statusie epoki przez 2 epoki i zostało usunięte.",
	},
	// [coin ID, ticker, match]
	TopicAuditTrouble: {
		subject:  "Problem z audytem",
		template: "Wciąż szukamy monety kontraktowej kontrahenta %v (%s) dla spasowania %s. Czy Twoje połączenie z Internetem i portfelem jest dobre?",
	},
	// [host, error]
	TopicDexAuthError: {
		subject:  "Błąd uwierzytelniania DEX",
		template: "%s: %v",
	},
	// [count, host]
	TopicUnknownOrders: {
		subject:  "DEX odnotował nieznane zlecenia",
		template: "Nie znaleziono %d aktywnych zleceń odnotowanych przez DEX %s.",
	},
	// [count]
	TopicOrdersReconciled: {
		subject:  "Pogodzono zlecenia z DEX",
		template: "Zaktualizowano statusy dla %d zleceń.",
	},
	// [ticker, address]
	TopicWalletConfigurationUpdated: {
		subject:  "Zaktualizowano konfigurację portfela",
		template: "Konfiguracja dla portfela %s została zaktualizowana. Adres do depozytów = %s",
	},
	//  [ticker]
	TopicWalletPasswordUpdated: {
		subject:  "Zaktualizowano hasło portfela",
		template: "Hasło dla portfela %s zostało zaktualizowane.",
	},
	// [market name, host, time]
	TopicMarketSuspendScheduled: {
		subject:  "Planowane zawieszenie rynku",
		template: "Rynek %s na %s zostanie wstrzymany o %v",
	},
	// [market name, host]
	TopicMarketSuspended: {
		subject:  "Rynek wstrzymany",
		template: "Handel na rynku %s na %s jest obecnie wstrzymany.",
	},
	// [market name, host]
	TopicMarketSuspendedWithPurge: {
		subject:  "Rynek wstrzymany, księga zamówień wyczyszczona",
		template: "Handel na rynku %s na %s jest obecnie wstrzymany. Wszystkie złożone zamówienia zostały WYCOFANE.",
	},
	// [market name, host, time]
	TopicMarketResumeScheduled: {
		subject:  "Planowane wznowienie rynku",
		template: "Rynek %s na %s zostanie wznowiony o %v",
	},
	// [market name, host, epoch]
	TopicMarketResumed: {
		subject:  "Rynek wznowiony",
		template: "Rynek %s na %s wznowił handel w epoce %d",
	},
	// [host]
	TopicUpgradeNeeded: {
		subject:  "Wymagana aktualizacja",
		template: "Aby handlować na %s wymagana jest aktualizacja klienta.",
	},
	// [host]
	TopicDEXConnected: {
		subject:  "Połączono z serwerem",
		template: "Połączono z %s",
	},
	// [host]
	TopicDEXDisconnected: {
		subject:  "Rozłączono z serwerem",
		template: "Rozłączono z %s",
	},
	// [host, rule, time, details]
	TopicPenalized: {
		subject:  "Serwer ukarał Cię punktami karnymi",
		template: "Punkty karne od serwera DEX na %s\nostatnia złamana reguła: %s\nczas: %v\nszczegóły:\n\"%s\"\n",
	},
	TopicSeedNeedsSaving: {
		subject:  "Nie zapomnij zrobić kopii ziarna aplikacji",
		template: "Utworzono nowe ziarno aplikacji. Zrób jego kopię w zakładce ustawień.",
	},
	TopicUpgradedToSeed: {
		subject:  "Zrób kopię nowego ziarna aplikacji",
		template: "Klient został zaktualizowany, by korzystać z ziarna aplikacji. Zrób jego kopię w zakładce ustawień.",
	},
	// [host, msg]
	TopicDEXNotification: {
		subject:  "Wiadomość od DEX",
		template: "%s: %s",
	},
}

// deDE is the German translations.
var deDE = map[Topic]*translation{
	// [host]
	TopicAccountRegistered: {
		subject:  "Account registeriert",
		template: "Du kannst nun auf %s handeln",
	},
	// [confs, host]
	TopicFeePaymentInProgress: {
		subject:  "Abwicklung der Registrationsgebühr",
		template: "Warten auf %d Bestätigungen bevor mit dem Handel bei %s begonnen werden kann",
	},
	// [confs, required confs]
	TopicRegUpdate: {
		subject:  "Aktualisierung der Registration",
		template: "%v/%v Bestätigungen der Registrationsgebühr",
	},
	// [host, error]
	TopicFeePaymentError: {
		subject:  "Fehler bei der Zahlung der Registrationsgebühr",
		template: "Bei der Zahlung der Registrationsgebühr für %s trat ein Fehler auf: %v",
	},
	// [host, error]
	TopicAccountUnlockError: {
		subject:  "Fehler beim Entsperren des Accounts",
		template: "Fehler beim Entsperren des Accounts für %s: %v",
	},
	// [host]
	TopicFeeCoinError: {
		subject:  "Coin Fehler bei Registrationsgebühr",
		template: "Fehlende Coin Angabe für Registrationsgebühr bei %s.",
	},
	// [host]
	TopicWalletConnectionWarning: {
		subject:  "Warnung bei Wallet Verbindung",
		template: "Unvollständige Registration für %s erkannt, konnte keine Verbindung zum Decred Wallet herstellen",
	},
	// [host, error]
	TopicWalletUnlockError: {
		subject:  "Fehler beim Entsperren des Wallet",
		template: "Verbunden zum Wallet um die Registration bei %s abzuschließen, ein Fehler beim entsperren des Wallet ist aufgetreten: %v",
	},
	// [asset name, error message]
	TopicWalletCommsWarning: {
		subject:  "Probleme mit der Verbindung zum Wallet",
		template: "Kommunikation mit dem %v Wallet nicht möglich! Grund: %q",
	},
	// [asset name]
	TopicWalletPeersWarning: {
		subject:  "Problem mit dem Wallet-Netzwerk",
		template: "%v Wallet hat keine Netzwerk-Peers!",
	},
	// [asset name]
	TopicWalletPeersRestored: {
		subject:  "Wallet-Konnektivität wiederhergestellt",
		template: "Die Verbindung mit dem %v Wallet wurde wiederhergestellt.",
	},
	// [ticker, error]
	TopicSendError: {
		subject:  "Sendefehler",
		template: "Fehler beim senden von %s aufgetreten: %v",
	},
	// [ticker, coin ID]
	TopicSendSuccess: {
		subject:  "Erfolgreich gesendet",
		template: "Das Senden von %s wurde erfolgreich abgeschlossen. Coin ID = %s",
	},
	// [error]
	TopicOrderLoadFailure: {
		subject:  "Fehler beim Laden der Aufträge",
		template: "Einige Aufträge konnten nicht aus der Datenbank geladen werden: %v",
	},
	// [qty, ticker, token]
	TopicYoloPlaced: {
		subject:  "Marktauftrag platziert",
		template: "Verkaufe %s %s zum Marktpreis (%s)",
	},
	// [sell string, qty, ticker, rate string, token]
	TopicOrderPlaced: {
		subject:  "Auftrag platziert",
		template: "%s %s %s, Kurs = %s (%s)",
	},
	// [missing count, token, host]
	TopicMissingMatches: {
		subject:  "Fehlende Matches",
		template: "%d Matches für den Auftrag %s wurden nicht von %q gemeldet und gelten daher als widerrufen",
	},
	// [token, error]
	TopicWalletMissing: {
		subject:  "Wallet fehlt",
		template: "Fehler bei der Wallet-Abfrage für den aktiven Auftrag %s: %v",
	},
	// [side, token, match status]
	TopicMatchErrorCoin: {
		subject:  "Fehler beim Coin Match",
		template: "Match %s für den Auftrag %s hat den Status %s, hat aber keine Coins für den Swap vom Maker gefunden.",
	},
	// [side, token, match status]
	TopicMatchErrorContract: {
		subject:  "Fehler beim Match Kontrakt",
		template: "Match %s für Auftrag %s hat den Status %s, hat aber keinen passenden Maker Swap Kontrakt.",
	},
	// [ticker, contract, token, error]
	TopicMatchRecoveryError: {
		subject:  "Fehler bei Match Wiederherstellung",
		template: "Fehler bei der Prüfung des Swap-Kontrakts der Gegenpartei (%s %v) während der Wiederherstellung des Auftrags %s: %v",
	},
	// [token]
	TopicOrderCoinError: {
		subject:  "Fehler bei den Coins für einen Auftrag",
		template: "Keine Coins zur Finanzierung des aktiven Auftrags %s gefunden",
	},
	// [token, ticker, error]
	TopicOrderCoinFetchError: {
		subject:  "Fehler beim Abruf der Coins für den Auftrag",
		template: "Beim Abruf der Coins als Quelle für den Auftrag %s (%s) ist ein Fehler aufgetreten: %v",
	},
	// [token]
	TopicMissedCancel: {
		subject:  "Abbruch verpasst",
		template: "Der Abbruch passt nicht zum Auftrag %s. Dies kann passieren wenn der Abbruch in der gleichen Epoche wie der Abschluss übermittelt wird oder wenn der Zielauftrag vollständig ausgeführt wird bevor er mit dem Abbruch gematcht werden konnte.",
	},
	// [capitalized sell string, base ticker, quote ticker, host, token]
	TopicOrderCanceled: {
		subject:  "Auftrag abgebrochen",
		template: "%s Auftrag für %s-%s bei %s wurde abgebrochen (%s)",
	},
	// [capitalized sell string, base ticker, quote ticker, fill percent, token]
	TopicMatchesMade: {
		subject:  "Matches durchgeführt",
		template: "%s Auftrag für %s-%s %.1f%% erfüllt (%s)",
	},
	// [qty, ticker, token]
	TopicSwapSendError: {
		subject:  "Fehler beim Senden des Swaps",
		template: "Beim Senden des Swap Output(s) im Wert von %s %s für den Auftrag %s",
	},
	// [match, error]
	TopicInitError: {
		subject:  "Fehler beim Swap Reporting",
		template: "Fehler bei der Benachrichtigung des Swaps an den DEX für den Match %s: %v",
	},
	// [match, error]
	TopicReportRedeemError: {
		subject:  "Fehler beim Redeem Reporting",
		template: "Fehler bei der Benachrichtigung des DEX für die Redemption des Match %s: %v",
	},
	// [qty, ticker, token]
	TopicSwapsInitiated: {
		subject:  "Swaps initiiert",
		template: "Swaps im Wert von %s %s für den Auftrag %s gesendet",
	},
	// [qty, ticker, token]
	TopicRedemptionError: {
		subject:  "Fehler bei der Redemption",
		template: "Fehler beim Senden von Redemptions im Wert von %s %s für Auftrag %s",
	},
	// [qty, ticker, token]
	TopicMatchComplete: {
		subject:  "Match abgeschlossen",
		template: "Redeemed %s %s für Auftrag %s",
	},
	// [qty, ticker, token]
	TopicRefundFailure: {
		subject:  "Fehler bei der Erstattung",
		template: "%s %s für Auftrag %s erstattet, mit einigen Fehlern",
	},
	// [qty, ticker, token]
	TopicMatchesRefunded: {
		subject:  "Matches Erstattet",
		template: "%s %s für Auftrag %s erstattet",
	},
	// [match ID token]
	TopicMatchRevoked: {
		subject:  "Match widerrufen",
		template: "Match %s wurde widerrufen",
	},
	// [token, market name, host]
	TopicOrderRevoked: {
		subject:  "Auftrag widerrufen",
		template: "Der Auftrag %s für den %s Markt bei %s wurde vom Server widerrufen",
	},
	// [token, market name, host]
	TopicOrderAutoRevoked: {
		subject:  "Auftrag automatisch widerrufen",
		template: "Der Auftrag %s für den %s Markt bei %s wurde wegen Aussetzung des Marktes widerrufen",
	},
	// [ticker, coin ID, match]
	TopicMatchRecovered: {
		subject:  "Match wiederhergestellt",
		template: "Die Redemption (%s: %v) des Anbieters wurde gefunden und das Geheimnis für Match %s verifiziert",
	},
	// [token]
	TopicCancellingOrder: {
		subject:  "Auftrag abgebrochen",
		template: "Der Auftrag %s wurde abgebrochen",
	},
	// [token, old status, new status]
	TopicOrderStatusUpdate: {
		subject:  "Aktualisierung des Auftragsstatus",
		template: "Status des Auftrags %v geändert von %v auf %v",
	},
	// [count, host, token]
	TopicMatchResolutionError: {
		subject:  "Fehler bei der Auflösung für Match",
		template: "%d Matches durch %s gemeldet wurden für %s nicht gefunden.",
	},
	// [token]
	TopicFailedCancel: {
		subject:  "Abbruch fehlgeschlagen",
		template: "Der Auftrag für den Abbruch des Auftrags %s blieb 2 Epochen lang im Epoche-Status hängen und wird nun gelöscht.",
	},
	// [coin ID, ticker, match]
	TopicAuditTrouble: {
		subject:  "Audit-Probleme",
		template: "Immernoch auf der Suche den Coins %v (%s) der Gegenseite für Match %s. Überprüfe deine Internetverbindung und die Verbindungen zum Wallet.",
	},
	// [host, error]
	TopicDexAuthError: {
		subject:  "DEX-Authentifizierungsfehler",
		template: "%s: %v",
	},
	// [count, host]
	TopicUnknownOrders: {
		subject:  "DEX meldet unbekannte Aufträge",
		template: "%d aktive Aufträge von DEX %s gemeldet aber konnten nicht gefunden werden.",
	},
	// [count]
	TopicOrdersReconciled: {
		subject:  "Aufträge mit DEX abgestimmt",
		template: "Der Status für %d Aufträge wurde aktualisiert.",
	},
	// [ticker, address]
	TopicWalletConfigurationUpdated: {
		subject:  "Aktualisierung der Wallet Konfiguration",
		template: "Konfiguration für Wallet %s wurde aktualisiert. Einzahlungsadresse = %s",
	},
	//  [ticker]
	TopicWalletPasswordUpdated: {
		subject:  "Wallet-Passwort aktualisiert",
		template: "Passwort für das %s Wallet wurde aktualisiert.",
	},
	// [market name, host, time]
	TopicMarketSuspendScheduled: {
		subject:  "Aussetzung des Marktes geplant",
		template: "%s Markt bei %s ist nun für ab %v zur Aussetzung geplant.",
	},
	// [market name, host]
	TopicMarketSuspended: {
		subject:  "Markt ausgesetzt",
		template: "Der Handel für den %s Markt bei %s ist nun ausgesetzt.",
	},
	// [market name, host]
	TopicMarketSuspendedWithPurge: {
		subject:  "Markt ausgesetzt, Aufträge gelöscht",
		template: "Der Handel für den %s Markt bei %s ist nun ausgesetzt. Alle gebuchten Aufträge werden jetzt ENTFERNT.",
	},
	// [market name, host, time]
	TopicMarketResumeScheduled: {
		subject:  "Wiederaufnahme des Marktes geplant",
		template: "Der %s Markt bei %s wird nun zur Wiederaufnahme ab %v geplant",
	},
	// [market name, host, epoch]
	TopicMarketResumed: {
		subject:  "Markt wiederaufgenommen",
		template: "Der %s Markt bei %s hat den Handel mit der Epoche %d wieder aufgenommen",
	},
	// [host]
	TopicUpgradeNeeded: {
		subject:  "Upgrade notwendig",
		template: "Du musst deinen Klient aktualisieren um bei %s zu Handeln.",
	},
	// [host]
	TopicDEXConnected: {
		subject:  "Server verbunden",
		template: "Erfolgreich verbunden mit %s",
	},
	// [host]
	TopicDEXDisconnected: {
		subject:  "Verbindung zum Server getrennt",
		template: "Verbindung zu %s unterbrochen",
	},
	// [host, rule, time, details]
	TopicPenalized: {
		subject:  "Bestrafung durch einen Server erhalten",
		template: "Bestrafung von DEX %s\nletzte gebrochene Regel: %s\nZeitpunkt: %v\nDetails:\n\"%s\"\n",
	},
	TopicSeedNeedsSaving: {
		subject:  "Vergiss nicht deinen App-Seed zu sichern",
		template: "Es wurde ein neuer App-Seed erstellt. Erstelle jetzt eine Sicherungskopie in den Einstellungen.",
	},
	TopicUpgradedToSeed: {
		subject:  "Sichere deinen neuen App-Seed",
		template: "Dein Klient wurde aktualisiert und nutzt nun einen App-Seed. Erstelle jetzt eine Sicherungskopie in den Einstellungen.",
	},
	// [host, msg]
	TopicDEXNotification: {
		subject:  "Nachricht von DEX",
		template: "%s: %s",
	},
	// karamble:
	// [parentSymbol, tokenSymbol]
	TopicQueuedCreationFailed: {
		subject:  "Token-Wallet konnte nicht erstellt werden",
		template: "Nach dem Erstellen des %s-Wallet kam es zu einen Fehler, konnte das %s-Wallet nicht erstellen",
	},
}

var locales = map[string]map[Topic]*translation{
	language.AmericanEnglish.String():     enUS,
	language.BrazilianPortuguese.String(): ptBR,
	"zh-CN":                               zhCN, // language.SimplifiedChinese is zh-Hans
	"pl-PL":                               plPL, // language.Polish is pl
	"de-DE":                               deDE, // language.German is de
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
