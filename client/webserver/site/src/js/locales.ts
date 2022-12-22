import enUSJson from '../../../../i18n/en-us.json'
import ptBRJson from '../../../../i18n/pt-br.json'

type Locale = Record<string, string>

export const ID_NO_PASS_ERROR_MSG = 'ID_NO_PASS_ERROR_MSG'
export const ID_NO_APP_PASS_ERROR_MSG = 'ID_NO_APP_PASS_ERROR_MSG'
export const ID_SET_BUTTON_BUY = 'ID_SET_BUTTON_BUY'
export const ID_SET_BUTTON_SELL = 'ID_SET_BUTTON_SELL'
export const ID_OFF = 'ID_OFF'
export const ID_READY = 'ID_READY'
export const ID_NO_WALLET = 'ID_NO_WALLET'
export const ID_DISABLED_MSG = 'ID_DISABLED_MSG'
export const ID_WALLET_SYNC_PROGRESS = 'ID_WALLET_SYNC_PROGRESS'
export const ID_HIDE_ADDITIONAL_SETTINGS = 'ID_HIDE_ADDITIONAL_SETTINGS'
export const ID_SHOW_ADDITIONAL_SETTINGS = 'ID_SHOW_ADDITIONAL_SETTINGS'
export const ID_BUY = 'ID_BUY'
export const ID_SELL = 'ID_SELL'
export const ID_NOT_SUPPORTED = 'ID_NOT_SUPPORTED'
export const ID_VERSION_NOT_SUPPORTED = 'ID_VERSION_NOT_SUPPORTED'
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
export const ID_ORDER_SUBMITTING = 'ID_ORDER_SUBMITTING'
export const ID_SETTLING = 'ID_SETTLING'
export const ID_NO_MATCH = 'ID_NO_MATCH'
export const ID_CANCELED = 'ID_CANCELED'
export const ID_REVOKED = 'ID_REVOKED'
export const ID_WAITING_FOR_CONFS = 'ID_WAITING_FOR_CONFS'
export const ID_NONE_SELECTED = 'ID_NONE_SELECTED' // unused?
export const ID_REGISTRATION_FEE_SUCCESS = 'ID_REGISTRATION_FEE_SUCCESS'
export const ID_API_ERROR = 'ID_API_ERROR'
export const ID_ADD = 'ID_ADD'
export const ID_CREATE = 'ID_CREATE'
export const ID_SETUP_WALLET = 'ID_SETUP_WALLET'
export const ID_WALLET_READY = 'ID_WALLET_READY'
export const ID_CHANGE_WALLET_TYPE = 'ID_CHANGE_WALLET_TYPE'
export const ID_KEEP_WALLET_TYPE = 'ID_KEEP_WALLET_TYPE'
export const WALLET_READY = 'WALLET_READY'
export const WALLET_PENDING = 'WALLET_PENDING'
export const SETUP_NEEDED = 'SETUP_NEEDED'
export const ID_SEND_SUCCESS = 'SEND_SUCCESS'
export const ID_RECONFIG_SUCCESS = 'RECONFIG_SUCCESS'
export const ID_RESCAN_STARTED = 'RESCAN_STARTED'
export const ID_NEW_WALLET_SUCCESS = 'NEW_WALLET_SUCCESS'
export const ID_WALLET_UNLOCKED = 'WALLET_UNLOCKED'
export const ID_SELLING = 'ID_SELLING'
export const ID_BUYING = 'ID_BUYING'
export const ID_WALLET_DISABLED_MSG = 'WALLET_DISABLED'
export const ID_WALLET_ENABLED_MSG = 'WALLET_ENABLED'
export const ID_ACTIVE_ORDERS_ERR_MSG = 'ACTIVE_ORDERS_ERR_MSG'
export const ID_AVAILABLE = 'AVAILABLE'
export const ID_LOCKED = 'LOCKED'
export const ID_IMMATURE = 'IMMATURE'
export const ID_FEE_BALANCE = 'FEE_BALANCE'
export const ID_CANDLES_LOADING = 'CANDLES_LOADING'
export const ID_DEPTH_LOADING = 'DEPTH_LOADING'
export const ID_INVALID_ADDRESS_MSG = 'INVALID_ADDRESS_MSG'
export const ID_TXFEE_UNSUPPORTED = 'TXFEE_UNSUPPORTED'
export const ID_TXFEE_ERR_MSG = 'TXFEE_ERR_MSG'
export const ID_ACTIVE_ORDERS_LOGOUT_ERR_MSG = 'ACTIVE_ORDERS_LOGOUT_ERR_MSG'
export const ID_INVALID_DATE_ERR_MSG = 'INVALID_DATE_ERR_MSG'
export const ID_NO_ARCHIVED_RECORDS = 'NO_ARCHIVED_RECORDS'
export const ID_DELETE_ARCHIVED_RECORDS_RESULT = 'DELETE_ARCHIVED_RECORDS_RESULT'
export const ID_ARCHIVED_RECORDS_PATH = 'ARCHIVED_RECORDS_PATH'
export const ID_DEFAULT = 'DEFAULT'
export const ID_ADDED = 'USER_ADDED'
export const ID_DISCOVERED = 'DISCOVERED'
export const ID_UNSUPPORTED_ASSET_INFO_ERR_MSG = 'UNSUPPORTED_ASSET_INFO_ERR_MSG'
export const ID_LIMIT_ORDER = 'LIMIT_ORDER'
export const ID_LIMIT_ORDER_IMMEDIATE_TIF = 'LIMIT_ORDER_IMMEDIATE_TIF'
export const ID_MARKET_ORDER = 'MARKET_ORDER'
export const ID_MATCH_STATUS_NEWLY_MATCHED = 'MATCH_STATUS_NEWLY_MATCHED'
export const ID_MATCH_STATUS_MAKER_SWAP_CAST = 'MATCH_STATUS_MAKER_SWAP_CAST'
export const ID_MATCH_STATUS_TAKER_SWAP_CAST = 'MATCH_STATUS_TAKER_SWAP_CAST'
export const ID_MATCH_STATUS_MAKER_REDEEMED = 'MATCH_STATUS_MAKER_REDEEMED'
export const ID_MATCH_STATUS_REDEMPTION_SENT = 'MATCH_STATUS_REDEMPTION_SENT'
export const ID_MATCH_STATUS_REDEMPTION_CONFIRMED = 'MATCH_REDEMPTION_CONFIRMED'
export const ID_MATCH_STATUS_REVOKED = 'MATCH_STATUS_REVOKED'
export const ID_MATCH_STATUS_REFUNDED = 'MATCH_STATUS_REFUNDED'
export const ID_MATCH_STATUS_REFUND_PENDING = 'MATCH_STATUS_REFUND_PENDING'
export const ID_MATCH_STATUS_REDEEM_PENDING = 'MATCH_STATUS_REDEEM_PENDING'
export const ID_MATCH_STATUS_COMPLETE = 'MATCH_STATUS_COMPLETE'
export const ID_TAKER_FOUND_MAKER_REDEMPTION = 'TAKER_FOUND_MAKER_REDEMPTION'
export const ID_USER_ADDED = 'ID_USER_ADDED'
export const ID_OPEN_WALLET_ERR_MSG = 'OPEN_WALLET_ERR_MSG'
export const ID_ORDER_ACCELERATION_FEE_ERR_MSG = 'ORDER_ACCELERATION_FEE_ERR_MSG'
export const ID_ORDER_ACCELERATION_ERR_MSG = 'ORDER_ACCELERATION_FEE_ERR_MSG'
export const ID_CONNECTED = 'CONNECTED'
export const ID_DISCONNECTED = 'DISCONNECTED'
export const ID_INVALID_CERTIFICATE = 'INVALID_CERTIFICATE'
export const ID_CONFIRMATIONS = 'ID_CONFIRMATIONS'
export const ID_TAKER = 'TAKER'
export const ID_MAKER = 'MAKER'
export const ID_EMPTY_DEX_ADDRESS_MSG = 'EMPTY_DEX_ADDRESS_MSG'
export const ID_SELECT_WALLET_FOR_FEE_PAYMENT = 'SELECT_WALLET_FOR_FEE_PAYMENT'
export const ID_UNAVAILABLE = 'UNAVAILABLE'
export const ID_WALLET_SYNC_FINISHING_UP = 'WALLET_SYNC_FINISHING_UP'

export const enUS: Locale = enUSJson
export const ptBR: Locale = ptBRJson

export const zhCN: Locale = {
  [ID_NO_PASS_ERROR_MSG]: '密码不能为空',
  [ID_NO_APP_PASS_ERROR_MSG]: '应用密码不能为空',
  [ID_PASSWORD_NOT_MATCH]: '密码不相同',
  [ID_SET_BUTTON_BUY]: '来自{{ asset }}的买入订单',
  [ID_SET_BUTTON_SELL]: '来自{{ asset }}的卖出订单',
  [ID_OFF]: '关闭',
  [ID_READY]: '准备就绪', //  alt. 准备好
  [ID_LOCKED]: '锁',
  [ID_NO_WALLET]: '未连接钱包', // alt. 没有钱包
  [ID_WALLET_SYNC_PROGRESS]: '钱包同步进度{{ syncProgress }}%',
  [ID_HIDE_ADDITIONAL_SETTINGS]: '隐藏其它设置',
  [ID_SHOW_ADDITIONAL_SETTINGS]: '显示其它设置',
  [ID_BUY]: '买入',
  [ID_SELL]: '卖出',
  [ID_NOT_SUPPORTED]: '{{ asset }}不受支持',
  [ID_CONNECTION_FAILED]: '连接到服务器 dex 失败。您可以关闭 dexc 并稍后重试或等待尝试重新连接。',
  [ID_ORDER_PREVIEW]: '总计： {{ total }} {{ asset }}',
  [ID_CALCULATING]: '计算中...',
  [ID_ESTIMATE_UNAVAILABLE]: '估计不可用',
  [ID_NO_ZERO_RATE]: '汇率不能为零',
  [ID_NO_ZERO_QUANTITY]: '数量不能为零',
  [ID_TRADE]: '交易',
  [ID_NO_ASSET_WALLET]: '没有钱包 {{ asset }}',
  [ID_EXECUTED]: '执行',
  [ID_BOOKED]: '保留',
  [ID_CANCELING]: '取消',
  [ID_ACCT_UNDEFINED]: '帐户未定义。',
  [ID_KEEP_WALLET_PASS]: '保留钱包密码',
  [ID_NEW_WALLET_PASS]: '设置新的钱包密码',
  [ID_LOT]: '批处理',
  [ID_LOTS]: '批', // alt. 很多
  [ID_EPOCH]: '时间',
  [ID_API_ERROR]: '接口错误',
  [ID_ADD]: '添加',
  [ID_CREATE]: '创建',
  [ID_AVAILABLE]: '可用',
  [ID_IMMATURE]: '不成熟'
}

export const plPL: Locale = {
  [ID_NO_PASS_ERROR_MSG]: 'hasło nie może być puste',
  [ID_NO_APP_PASS_ERROR_MSG]: 'hasło aplikacji nie może być puste',
  [ID_PASSWORD_NOT_MATCH]: 'hasła nie są jednakowe',
  [ID_SET_BUTTON_BUY]: 'Złóż zlecenie, aby kupić  {{ asset }}',
  [ID_SET_BUTTON_SELL]: 'Złóż zlecenie, aby sprzedać {{ asset }}',
  [ID_OFF]: 'wyłączony',
  [ID_READY]: 'gotowy',
  [ID_LOCKED]: 'zablokowany',
  [ID_NO_WALLET]: 'brak portfela',
  [ID_WALLET_SYNC_PROGRESS]: 'portfel zsynchronizowany w {{ syncProgress }}%',
  [ID_HIDE_ADDITIONAL_SETTINGS]: 'ukryj dodatkowe ustawienia',
  [ID_SHOW_ADDITIONAL_SETTINGS]: 'pokaż dodatkowe ustawienia',
  [ID_BUY]: 'Kup',
  [ID_SELL]: 'Sprzedaj',
  [ID_NOT_SUPPORTED]: '{{ asset }} nie jest wspierany',
  [ID_CONNECTION_FAILED]: 'Połączenie z serwerem dex nie powiodło się. Możesz zamknąć dexc i spróbować ponownie później, lub poczekać na wznowienie połączenia.',
  [ID_ORDER_PREVIEW]: 'W sumie: {{ total }} {{ asset }}',
  [ID_CALCULATING]: 'obliczanie...',
  [ID_ESTIMATE_UNAVAILABLE]: 'brak szacunkowego wyliczenia',
  [ID_NO_ZERO_RATE]: 'zero nie może być ceną',
  [ID_NO_ZERO_QUANTITY]: 'zero nie może być ilością',
  [ID_TRADE]: 'handluj',
  [ID_NO_ASSET_WALLET]: 'Brak portfela {{ asset }}',
  [ID_EXECUTED]: 'wykonano',
  [ID_BOOKED]: 'zapisano',
  [ID_CANCELING]: 'anulowanie',
  [ID_ACCT_UNDEFINED]: 'Niezdefiniowane konto.',
  [ID_KEEP_WALLET_PASS]: 'zachowaj obecne hasło portfela',
  [ID_NEW_WALLET_PASS]: 'ustaw nowe hasło portfela',
  [ID_LOT]: 'lot',
  [ID_LOTS]: 'loty(ów)',
  [ID_UNKNOWN]: 'nieznane',
  [ID_EPOCH]: 'epoka',
  [ID_SETTLING]: 'rozliczanie',
  [ID_NO_MATCH]: 'brak spasowania',
  [ID_CANCELED]: 'anulowano',
  [ID_REVOKED]: 'unieważniono',
  [ID_WAITING_FOR_CONFS]: 'Oczekiwanie na potwierdzenia...',
  [ID_NONE_SELECTED]: 'brak zaznaczenia',
  [ID_REGISTRATION_FEE_SUCCESS]: 'Płatność rejestracyjna powiodła się!',
  [ID_API_ERROR]: 'błąd API',
  [ID_ADD]: 'Dodaj',
  [ID_CREATE]: 'Utwórz',
  [ID_WALLET_READY]: 'Gotowy',
  [ID_SETUP_WALLET]: 'Konfiguracja',
  [ID_CHANGE_WALLET_TYPE]: 'zmień typ portfela',
  [ID_KEEP_WALLET_TYPE]: 'nie zmieniaj typu portfela',
  [WALLET_READY]: 'Portfel jest gotowy',
  [SETUP_NEEDED]: 'Potrzebna konfiguracja',
  [ID_AVAILABLE]: 'dostępne',
  [ID_IMMATURE]: 'niedojrzałe'
}

export const deDE: Locale = {
  [ID_NO_PASS_ERROR_MSG]: 'Passwort darf nicht leer sein',
  [ID_NO_APP_PASS_ERROR_MSG]: 'App-Passwort darf nicht leer sein',
  [ID_PASSWORD_NOT_MATCH]: 'Passwörter stimmen nicht überein',
  [ID_SET_BUTTON_BUY]: 'Platziere Auftrag zum Kauf von  {{ asset }}',
  [ID_SET_BUTTON_SELL]: 'Platziere Auftrag zum Verkauf von {{ asset }}',
  [ID_OFF]: 'aus',
  [ID_READY]: 'bereit',
  [ID_LOCKED]: 'gesperrt',
  [ID_NO_WALLET]: 'kein Wallet',
  [ID_WALLET_SYNC_PROGRESS]: 'Wallet ist zu {{ syncProgress }}% synchronisiert',
  [ID_HIDE_ADDITIONAL_SETTINGS]: 'zusätzliche Einstellungen ausblenden',
  [ID_SHOW_ADDITIONAL_SETTINGS]: 'zusätzliche Einstellungen anzeigen',
  [ID_BUY]: 'Kaufen',
  [ID_SELL]: 'Verkaufen',
  [ID_NOT_SUPPORTED]: '{{ asset }} wird nicht unterstützt',
  [ID_CONNECTION_FAILED]: 'Die Verbindung zum Dex-Server fehlgeschlagen. Du kannst dexc schließen und es später erneut versuchen oder warten bis die Verbindung wiederhergestellt ist.',
  [ID_ORDER_PREVIEW]: 'Insgesamt: {{ total }} {{ asset }}',
  [ID_CALCULATING]: 'kalkuliere...',
  [ID_ESTIMATE_UNAVAILABLE]: 'Schätzung nicht verfügbar',
  [ID_NO_ZERO_RATE]: 'Null-Satz nicht erlaubt',
  [ID_NO_ZERO_QUANTITY]: 'Null-Menge nicht erlaubt',
  [ID_TRADE]: 'Handel',
  [ID_NO_ASSET_WALLET]: 'Kein {{ asset }} Wallet',
  [ID_EXECUTED]: 'ausgeführt',
  [ID_BOOKED]: 'gebucht',
  [ID_CANCELING]: 'Abbruch',
  [ID_ACCT_UNDEFINED]: 'Account undefiniert.',
  [ID_KEEP_WALLET_PASS]: 'aktuelles Passwort für das Wallet behalten',
  [ID_NEW_WALLET_PASS]: 'ein neues Passwort für das Wallet festlegen',
  [ID_LOT]: 'Lot',
  [ID_LOTS]: 'Lots',
  [ID_UNKNOWN]: 'unbekannt',
  [ID_EPOCH]: 'Epoche',
  [ID_SETTLING]: 'Abwicklung',
  [ID_NO_MATCH]: 'kein Match',
  [ID_CANCELED]: 'abgebrochen',
  [ID_REVOKED]: 'widerrufen',
  [ID_WAITING_FOR_CONFS]: 'Warten auf Bestätigungen...',
  [ID_NONE_SELECTED]: 'keine ausgewählt',
  [ID_REGISTRATION_FEE_SUCCESS]: 'Zahlung der Registrierungsgebühr erfolgreich!',
  [ID_API_ERROR]: 'API Fehler',
  [ID_ADD]: 'Hinzufügen',
  [ID_CREATE]: 'Erstellen',
  [ID_WALLET_READY]: 'Bereit',
  [ID_SETUP_WALLET]: 'Einrichten',
  [ID_CHANGE_WALLET_TYPE]: 'den Wallet-Typ ändern',
  [ID_KEEP_WALLET_TYPE]: 'den Wallet-Typ nicht ändern',
  [WALLET_READY]: 'Wallet bereit',
  [SETUP_NEEDED]: 'Einrichtung erforderlich',
  [WALLET_PENDING]: 'Erstelle Wallet',
  [ID_SEND_SUCCESS]: '{{ assetName }} gesendet!',
  [ID_RECONFIG_SUCCESS]: 'Wallet neu konfiguriert!',
  [ID_RESCAN_STARTED]: 'Wallet Rescan läuft',
  [ID_NEW_WALLET_SUCCESS]: '{{ assetName }} Wallet erstellt!',
  [ID_WALLET_UNLOCKED]: 'Wallet entsperrt'
}

export const ar: Locale = {
  [ID_NO_PASS_ERROR_MSG]: 'لا يمكن أن تكون كلمة المرور فارغة',
  [ID_NO_APP_PASS_ERROR_MSG]: 'لا يمكن أن تكون كلمة مرور التطبيق فارغة',
  [ID_PASSWORD_NOT_MATCH]: 'كلمات المرور غير متطابقة',
  [ID_SET_BUTTON_BUY]: 'ضع طلبًا للشراء  {{ asset }}',
  [ID_SET_BUTTON_SELL]: 'ضع طلبًا للبيع {{ asset }}',
  [ID_OFF]: 'إيقاف',
  [ID_READY]: 'متوقف',
  [ID_LOCKED]: 'مقفل',
  [ID_NO_WALLET]: 'لا توجد أي محفظة',
  [ID_WALLET_SYNC_PROGRESS]: 'تمت مزامنة {{ syncProgress }}% المحفظة',
  [ID_HIDE_ADDITIONAL_SETTINGS]: 'إخفاء الإعدادات الإضافية',
  [ID_SHOW_ADDITIONAL_SETTINGS]: 'عرض الإعدادات الإضافية',
  [ID_BUY]: 'شراء',
  [ID_SELL]: 'بيع',
  [ID_NOT_SUPPORTED]: '{{ asset }} غير مدعوم',
  [ID_CONNECTION_FAILED]: 'فشل الاتصال بخادم dex. يمكنك إغلاق dexc والمحاولة مرة أخرى لاحقًا أو انتظار إعادة الاتصال.',
  [ID_ORDER_PREVIEW]: 'إجمالي: {{ total }} {{ asset }}',
  [ID_CALCULATING]: 'جاري الحساب ...',
  [ID_ESTIMATE_UNAVAILABLE]: 'التقديرات غير متاحة',
  [ID_NO_ZERO_RATE]: 'معدل الصفر غير مسموح به',
  [ID_NO_ZERO_QUANTITY]: 'غير مسموح بالكمية الصفرية',
  [ID_TRADE]: 'التداول',
  [ID_NO_ASSET_WALLET]: 'لا توجد {{ asset }} محفظة',
  [ID_EXECUTED]: 'تم تنفيذها',
  [ID_BOOKED]: 'تم الحجز',
  [ID_CANCELING]: 'جارٍ الإلغاء',
  [ID_ACCT_UNDEFINED]: 'حساب غير محدد.',
  [ID_KEEP_WALLET_PASS]: 'احتفظ بكلمة مرور المحفظة الحالية',
  [ID_NEW_WALLET_PASS]: 'قم بتعيين كلمة مرور جديدة للمحفظة',
  [ID_LOT]: 'الحصة',
  [ID_LOTS]: 'الحصص',
  [ID_UNKNOWN]: 'غير معروف',
  [ID_EPOCH]: 'الحقبة الزمنية',
  [ID_SETTLING]: 'الإعدادات',
  [ID_NO_MATCH]: 'غير متطابقة',
  [ID_CANCELED]: 'ملغاة',
  [ID_REVOKED]: 'مستعادة',
  [ID_WAITING_FOR_CONFS]: 'في انتظار التأكيدات ...',
  [ID_NONE_SELECTED]: 'لم يتم تحديد أي شيء',
  [ID_REGISTRATION_FEE_SUCCESS]: 'تم دفع رسوم التسجيل بنجاح!',
  [ID_API_ERROR]: 'خطأ في واجهة برمجة التطبيقات',
  [ID_ADD]: 'إضافة',
  [ID_CREATE]: 'إنشاء',
  [ID_WALLET_READY]: 'جاهزة',
  [ID_SETUP_WALLET]: 'إعداد',
  [ID_CHANGE_WALLET_TYPE]: 'تغيير نوع المحفظة',
  [ID_KEEP_WALLET_TYPE]: 'لا تغير نوع المحفظة',
  [WALLET_READY]: 'المحفظة جاهزة',
  [SETUP_NEEDED]: 'الإعداد مطلوب',
  [WALLET_PENDING]: 'إنشاء المحفظة',
  [ID_SEND_SUCCESS]: '{{ assetName }} تم الإرسال!',
  [ID_RECONFIG_SUCCESS]: 'تمت إعادة تهيئة المحفظة!!',
  [ID_RESCAN_STARTED]: 'إعادة فحص المحفظة قيد التشغيل',
  [ID_NEW_WALLET_SUCCESS]: '{{ assetName }} تم إنشاء المحفظة!',
  [ID_WALLET_UNLOCKED]: 'المحفظة غير مقفلة',
  [ID_SELLING]: 'البيع',
  [ID_BUYING]: 'Bالشراء',
  [ID_WALLET_ENABLED_MSG]: '{{ assetName }} المحفظة ممكنة',
  [ID_WALLET_DISABLED_MSG]: '{{ assetName }} المحفظة معطلة',
  [ID_DISABLED_MSG]: 'تم تعطيل المحفظة',
  [ID_ACTIVE_ORDERS_ERR_MSG]: '{{ assetName }} تدير المحفظة  الطلبات بفعالية'
}

const localesMap: Record<string, Locale> = {
  'en-us': enUS,
  'pt-br': ptBR,
  'zh-cn': zhCN,
  'pl-pl': plPL,
  'de-de': deDE,
  'ar': ar
}

/* locale will hold the locale loaded via setLocale. */
let locale: Locale

const defaultLocale = enUS

/*
 * setLocale read the language tag from the current document's html element lang
 * attribute and sets the locale. setLocale should be called once by the
 * application before prep is used.
*/
export function setLocale () { locale = localesMap[document.documentElement.lang.toLowerCase()] }

/* prep will format the message to the current locale. */
export function prep (k: string, args?: Record<string, string>) {
  return stringTemplateParser(locale[k] || defaultLocale[k], args || {})
}

/*
 * stringTemplateParser is a template string matcher, where expression is any
 * text. It switches what is inside double brackets (e.g. 'buy {{ asset }}')
 * for the value described into args. args is an object with keys
 * equal to the placeholder keys. (e.g. {"asset": "dcr"}).
 * So that will be switched for: 'asset dcr'.
 */
function stringTemplateParser (expression: string, args: Record<string, string>) {
  // templateMatcher matches any text which:
  // is some {{ text }} between two brackets, and a space between them.
  // It is global, therefore it will change all occurrences found.
  // text can be anything, but brackets '{}' and space '\s'
  const templateMatcher = /{{\s?([^{}\s]*)\s?}}/g
  return expression.replace(templateMatcher, (_, value) => args[value])
}

window.localeDiscrepancies = () => {
  const ref = enUS
  for (const [lang, dict] of Object.entries(localesMap)) {
    if (dict === ref) continue
    for (const [k, s] of Object.entries(ref)) {
      if (!dict[k]) console.log(`${lang} needs a tranlation for: ${s}`)
    }
  }
}
