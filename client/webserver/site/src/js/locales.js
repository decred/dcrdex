export const ID_NO_PASS_ERROR_MSG = 'ID_NO_PASS_ERROR_MSG'
export const ID_NO_APP_PASS_ERROR_MSG = 'ID_NO_APP_PASS_ERROR_MSG'
export const ID_SET_BUTTON_BUY = 'ID_SET_BUTTON_BUY'
export const ID_SET_BUTTON_SELL = 'ID_SET_BUTTON_SELL'
export const ID_OFF = 'ID_OFF'
export const ID_READY = 'ID_READY'
export const ID_LOCKED = 'ID_LOCKED'
export const ID_NOWALLET = 'ID_NOWALLET'
export const ID_WALLET_SYNC_PROGRESS = 'ID_WALLET_SYNC_PROGRESS'
export const ID_HIDE_ADDITIONAL_SETTINGS = 'ID_HIDE_ADDITIONAL_SETTINGS'
export const ID_SHOW_ADDITIONAL_SETTINGS = 'ID_SHOW_ADDITIONAL_SETTINGS'
export const ID_BUY = 'ID_BUY'
export const ID_SELL = 'ID_SELL'
export const ID_NOT_SUPPORTED = 'ID_NOT_SUPPORTED'
export const ID_CONNECTION_FAILED = 'ID_CONNECTION_FAILED'
export const ID_ORDER_PREVIEW = 'ID_ORDER_PREVIEW'
export const ID_CALCULATING = 'ID_CALCULATING'
export const ID_ESTIMATE_UNAVAILABLE = 'ID_ESTIMATE_UNAVAILABLE'
export const ID_NO_ZERO_RATE = 'ID_NO_ZERO_RATE'
export const ID_NO_ZERO_QUANTITY = 'ID_NO_ZERO_QUANTITY'
export const ID_TRADE = 'ID_TRADE'
export const ID_NO_ASSET_WALLET = 'ID_NO_ASSET_WALLET'
export const ID_EXECUTED = 'ID_EXECUTED'
export const ID_BOOKED = 'ID_BOOKED'
export const ID_CANCELING = 'ID_CANCELING'
export const ID_PASSWORD_NOT_MATCH = 'ID_PASSWORD_NOT_MATCH'
export const ID_ACCT_UNDEFINED = 'ID_ACCT_UNDEFINED'
export const ID_KEEP_WALLET_PASS = 'ID_KEEP_WALLET_PASS'
export const ID_NEW_WALLET_PASS = 'ID_NEW_WALLET_PASS'
export const ID_LOT = 'ID_LOT'
export const ID_LOTS = 'ID_LOTS'
export const ID_UNKNOWN = 'ID_UNKNOWN'
export const ID_EPOCH = 'ID_EPOCH'
export const ID_SETTLING = 'ID_SETTLING'
export const ID_NO_MATCH = 'ID_NO_MATCH'
export const ID_CANCELED = 'ID_CANCELED'
export const ID_REVOKED = 'ID_REVOKED'
export const ID_WAITING_FOR_CONFS = 'ID_WAITING_FOR_CONFS'
export const ID_NONE_SELECTED = 'ID_NONE_SELECTED' // unused?
export const ID_REGISTRATION_FEE_SUCCESS = 'ID_REGISTRATION_FEE_SUCCESS'
export const ID_API_ERROR = 'ID_API_ERROR'

export const enUS = {
  [ID_NO_PASS_ERROR_MSG]: 'password cannot be empty',
  [ID_NO_APP_PASS_ERROR_MSG]: 'app password cannot be empty',
  [ID_PASSWORD_NOT_MATCH]: 'passwords do not match',
  [ID_SET_BUTTON_BUY]: 'Place order to buy  {{ asset }}',
  [ID_SET_BUTTON_SELL]: 'Place order to sell {{ asset }}',
  [ID_OFF]: 'off',
  [ID_READY]: 'ready',
  [ID_LOCKED]: 'locked',
  [ID_NOWALLET]: 'no wallet',
  [ID_WALLET_SYNC_PROGRESS]: 'wallet is {{ syncProgress }}% synced',
  [ID_HIDE_ADDITIONAL_SETTINGS]: 'hide additional settings',
  [ID_SHOW_ADDITIONAL_SETTINGS]: 'show additional settings',
  [ID_BUY]: 'Buy',
  [ID_SELL]: 'Sell',
  [ID_NOT_SUPPORTED]: '{{ asset }} is not supported',
  [ID_CONNECTION_FAILED]: 'Connection to dex server failed. You can close dexc and try again later or wait for it to reconnect.',
  [ID_ORDER_PREVIEW]: 'Total: {{ total }} {{ asset }}',
  [ID_CALCULATING]: 'calculating...',
  [ID_ESTIMATE_UNAVAILABLE]: 'estimate unavailable',
  [ID_NO_ZERO_RATE]: 'zero rate not allowed',
  [ID_NO_ZERO_QUANTITY]: 'zero quantity not allowed',
  [ID_TRADE]: 'trade',
  [ID_NO_ASSET_WALLET]: 'No {{ asset }} wallet',
  [ID_EXECUTED]: 'executed',
  [ID_BOOKED]: 'booked',
  [ID_CANCELING]: 'canceling',
  [ID_ACCT_UNDEFINED]: 'Account undefined.',
  [ID_KEEP_WALLET_PASS]: 'keep current wallet password',
  [ID_NEW_WALLET_PASS]: 'set a new wallet password',
  [ID_LOT]: 'lot',
  [ID_LOTS]: 'lots',
  [ID_UNKNOWN]: 'unknown',
  [ID_EPOCH]: 'epoch',
  [ID_SETTLING]: 'settling',
  [ID_NO_MATCH]: 'no match',
  [ID_CANCELED]: 'canceled',
  [ID_REVOKED]: 'revoked',
  [ID_WAITING_FOR_CONFS]: 'Waiting for confirmations...',
  [ID_NONE_SELECTED]: 'none selected',
  [ID_REGISTRATION_FEE_SUCCESS]: 'Registration fee payment successful!',
  [ID_API_ERROR]: 'API error'
}

export const ptBR = {
  [ID_NO_PASS_ERROR_MSG]: 'senha não pode ser vazia',
  [ID_NO_APP_PASS_ERROR_MSG]: 'senha do app não pode ser vazia',
  [ID_PASSWORD_NOT_MATCH]: 'senhas diferentes',
  [ID_SET_BUTTON_BUY]: 'Ordem de compra de {{ asset }}',
  [ID_SET_BUTTON_SELL]: 'Ordem de venda de {{ asset }}',
  [ID_OFF]: 'desligar',
  [ID_READY]: 'pronto',
  [ID_LOCKED]: 'trancado',
  [ID_NOWALLET]: 'sem carteira',
  [ID_WALLET_SYNC_PROGRESS]: 'carteira está {{ syncProgress }}% sincronizada',
  [ID_HIDE_ADDITIONAL_SETTINGS]: 'esconder configurações adicionais',
  [ID_SHOW_ADDIIONAL_SETTINGS]: 'mostrar configurações adicionais',
  [ID_BUY]: 'Comprar',
  [ID_SELL]: 'Vender',
  [ID_NOT_SUPPORTED]: '{{ asset }} não tem suporte',
  [ID_CONNECTION_FAILED]: 'Conexão ao server dex falhou. Pode fechar dexc e tentar novamente depois ou esperar para tentar se reconectar.',
  [ID_ORDER_PREVIEW]: 'Total: {{ total }} {{ asset }}',
  [ID_CALCULATING]: 'calculando...',
  [ID_ESTIMATE_UNAVAILABLE]: 'estimativa indisponível',
  [ID_NO_ZERO_RATE]: 'taxa não pode ser zero',
  [ID_NO_ZERO_QUANTITY]: 'quantidade não pode ser zero',
  [ID_TRADE]: 'troca',
  [ID_NO_ASSET_WALLET]: 'Sem carteira {{ asset }}',
  [ID_EXECUTED]: 'executado',
  [ID_BOOKED]: 'reservado',
  [ID_CANCELING]: 'cancelando',
  [ID_ACCT_UNDEFINED]: 'conta não definida.',
  [ID_KEEP_WALLET_PASS]: 'manter senha da carteira',
  [ID_NEW_WALLET_PASS]: 'definir nova senha para carteira',
  [ID_LOT]: 'lote',
  [ID_LOTS]: 'lotes',
  [ID_UNKNOWN]: 'desconhecido',
  [ID_EPOCH]: 'epoque',
  [ID_SETTLING]: 'assentando',
  [ID_NO_MATCH]: 'sem combinações',
  [ID_CANCELED]: 'cancelado',
  [ID_REVOKED]: 'revocado',
  [ID_WAITING_FOR_CONFS]: 'Esperando confirmações...',
  [ID_NONE_SELECTED]: 'nenhuma selecionado',
  [ID_REGISTRATION_FEE_SUCCESS]: 'Sucesso no pagamento da taxa de registro!',
  [ID_API_ERROR]: 'Erro de API'
}

const localesMap = {
  'en-us': enUS,
  'pt-br': ptBR
}

/* locale will hold the locale loaded via setLocale. */
let locale

/*
 * setLocale read the language tag from the current document's html element lang
 * attribute and sets the locale. setLocale should be called once by the
 * application before prep is used.
*/
export function setLocale () { locale = localesMap[document.documentElement.lang.toLowerCase()] }

/* prep will format the message to the current locale. */
export function prep (k, args = undefined) {
  return stringTemplateParser(locale[k], args)
}

/*
 * stringTemplateParser is a template string matcher, where expression is any
 * text. It switches what is inside double brackets (e.g. 'buy {{ asset }}')
 * for the value described into valueObj. valueObj is an object with keys
 * equal to the placeholder keys. (e.g. {"asset": "dcr"}).
 * So that will be switched for: 'asset dcr'.
 */
function stringTemplateParser (expression, valueObj) {
  // templateMatcher matches any text which:
  // is some {{ text }} between two brackets, and a space between them.
  // It is global, therefore it will change all occurrences found.
  // text can be anything, but brackets '{}' and space '\s'
  const templateMatcher = /{{\s?([^{}\s]*)\s?}}/g
  return expression.replace(templateMatcher, (_, value) => valueObj[value])
}
