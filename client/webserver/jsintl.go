package webserver

import "decred.org/dcrdex/client/intl"

const (
	noPassErrMsgID                   = "NO_PASS_ERROR_MSG"
	noAppPassErrMsgID                = "NO_APP_PASS_ERROR_MSG"
	setButtonBuyID                   = "SET_BUTTON_BUY"
	setButtonSellID                  = "SET_BUTTON_SELL"
	offID                            = "OFF"
	maxID                            = "MAX"
	readyID                          = "READY"
	noWalletID                       = "NO_WALLET"
	disabledMsgID                    = "DISABLED_MSG"
	walletSyncProgressID             = "WALLET_SYNC_PROGRESS"
	hideAdditionalSettingsID         = "HIDE_ADDITIONAL_SETTINGS"
	showAdditionalSettingsID         = "SHOW_ADDITIONAL_SETTINGS"
	buyID                            = "BUY"
	sellID                           = "SELL"
	notSupportedID                   = "NOT_SUPPORTED"
	versionNotSupportedID            = "VERSION_NOT_SUPPORTED"
	connectionFailedID               = "CONNECTION_FAILED"
	orderPreviewID                   = "ORDER_PREVIEW"
	calculatingID                    = "CALCULATING"
	estimateUnavailableID            = "ESTIMATE_UNAVAILABLE"
	noZeroRateID                     = "NO_ZERO_RATE"
	noZeroQuantityID                 = "NO_ZERO_QUANTITY"
	tradeID                          = "TRADE"
	noAssetWalletID                  = "NO_ASSET_WALLET"
	executedID                       = "EXECUTED"
	bookedID                         = "BOOKED"
	cancelingID                      = "CANCELING"
	passwordNotMatchID               = "PASSWORD_NOT_MATCH"
	acctUndefinedID                  = "ACCT_UNDEFINED"
	keepWalletPassID                 = "KEEP_WALLET_PASS"
	newWalletPassID                  = "NEW_WALLET_PASS"
	lotID                            = "LOT"
	lotsID                           = "LOTS"
	unknownID                        = "UNKNOWN"
	epochID                          = "EPOCH"
	orderSubmittingID                = "ORDER_SUBMITTING"
	settlingID                       = "SETTLING"
	noMatchID                        = "NO_MATCH"
	canceledID                       = "CANCELED"
	revokedID                        = "REVOKED"
	waitingForConfsID                = "WAITING_FOR_CONFS"
	noneSelectedID                   = "NONE_SELECTED"
	regFeeSuccessID                  = "REGISTRATION_FEE_SUCCESS"
	apiErrorID                       = "API_ERROR"
	addID                            = "ADD"
	createID                         = "CREATE"
	setupWalletID                    = "SETUP_WALLET"
	changeWalletTypeID               = "CHANGE_WALLET_TYPE"
	keepWalletTypeID                 = "KEEP_WALLET_TYPE"
	walletReadyID                    = "WALLET_READY"
	walletPendingID                  = "WALLET_PENDING"
	setupNeededID                    = "SETUP_NEEDED"
	sendSuccessID                    = "SEND_SUCCESS"
	reconfigSuccessID                = "RECONFIG_SUCCESS"
	rescanStartedID                  = "RESCAN_STARTED"
	newWalletSuccessID               = "NEW_WALLET_SUCCESS"
	walletUnlockedID                 = "WALLET_UNLOCKED"
	sellingID                        = "SELLING"
	buyingID                         = "BUYING"
	walletDisabledID                 = "WALLET_DISABLED"
	walletEnabledID                  = "WALLET_ENABLED"
	activeOrdersErrorID              = "ACTIVE_ORDERS_ERR_MSG"
	availableID                      = "AVAILABLE"
	lockedID                         = "LOCKED"
	immatureID                       = "IMMATURE"
	feeBalanceID                     = "FEE_BALANCE"
	candlesLoadingID                 = "CANDLES_LOADING"
	depthLoadingID                   = "DEPTH_LOADING"
	invalidAddrressMsgID             = "INVALID_ADDRESS_MSG"
	txFeeSupportedID                 = "TXFEE_UNSUPPORTED"
	txFeeErrorMsgID                  = "TXFEE_ERR_MSG"
	activeOrdersLogoutErrorID        = "ACTIVE_ORDERS_LOGOUT_ERR_MSG"
	invalidDateErrorMsgID            = "INVALID_DATE_ERR_MSG"
	noArchivedRecordsID              = "NO_ARCHIVED_RECORDS"
	deleteArchivedRecordsID          = "DELETE_ARCHIVED_RECORDS_RESULT"
	archivedRecordsPathID            = "ARCHIVED_RECORDS_PATH"
	defaultID                        = "DEFAULT"
	addedID                          = "ADDED"
	discoveredID                     = "DISCOVERED"
	unsupportedAssetInfoErrMsgID     = "UNSUPPORTED_ASSET_INFO_ERR_MSG"
	limitOrderID                     = "LIMIT_ORDER"
	limitOrderImmediateTifID         = "LIMIT_ORDER_IMMEDIATE_TIF"
	marketOrderID                    = "MARKET_ORDER"
	matchStatusNewlyMatchedID        = "MATCH_STATUS_NEWLY_MATCHED"
	matchStatusMakerSwapCastID       = "MATCH_STATUS_MAKER_SWAP_CAST"
	matchStatusTakerSwapCastID       = "MATCH_STATUS_TAKER_SWAP_CAST"
	matchStatusMakerRedeemedID       = "MATCH_STATUS_MAKER_REDEEMED"
	matchStatusRedemptionSentID      = "MATCH_STATUS_REDEMPTION_SENT"
	matchStatusRedemptionConfirmedID = "MATCH_REDEMPTION_CONFIRMED"
	matchStatusRevokedID             = "MATCH_STATUS_REVOKED"
	matchStatusRefundedID            = "MATCH_STATUS_REFUNDED"
	matchStatusRefundPendingID       = "MATCH_STATUS_REFUND_PENDING"
	matchStatusRedeemPendingID       = "MATCH_STATUS_REDEEM_PENDING"
	matchStatusCompleteID            = "MATCH_STATUS_COMPLETE"
	takerFoundMakerRedemptionID      = "TAKER_FOUND_MAKER_REDEMPTION"
	openWalletErrMsgID               = "OPEN_WALLET_ERR_MSG"
	orderAccelerationFeeErrMsgID     = "ORDER_ACCELERATION_FEE_ERR_MSG"
	orderAccelerationErrMsgID        = "ORDER_ACCELERATION_ERR_MSG"
	connectedID                      = "CONNECTED"
	disconnectedID                   = "DISCONNECTED"
	invalidCertID                    = "INVALID_CERTIFICATE"
	confirmationsID                  = "CONFIRMATIONS"
	takerID                          = "TAKER"
	makerID                          = "MAKER"
	emptyDexAddrID                   = "EMPTY_DEX_ADDRESS_MSG"
	selectWalletForFeePaymentID      = "SELECT_WALLET_FOR_FEE_PAYMENT"
	unavailableID                    = "UNAVAILABLE"
	walletSyncFinishingID            = "WALLET_SYNC_FINISHING_UP"
	connectWalletErrMsgID            = "CONNECTING_WALLET_ERR_MSG"
	refundImminentID                 = "REFUND_IMMINENT"
	refundWillHappenAfterID          = "REFUND_WILL_HAPPEN_AFTER"
	availableTitleID                 = "AVAILABLE_TITLE"
	lockedTitleID                    = "LOCKED_TITLE"
	immatureTitleID                  = "IMMATURE_TITLE"
	swappingID                       = "SWAPPING"
	bondedID                         = "BONDED"
	lockedBalMsgID                   = "LOCKED_BAL_MSG"
	immatureBalMsgID                 = "IMMATURE_BAL_MSG"
	lockedSwappingBalMsgID           = "LOCKED_SWAPPING_BAL_MSG"
	lockedBonBalMsgID                = "LOCKED_BOND_BAL_MSG"
	reservesDeficitID                = "RESERVES_DEFICIT"
	reservesDeficitMsgID             = "RESERVES_DEFICIT_MSG"
	bondReservesID                   = "BOND_RESERVES"
	bondReservesMsgID                = "BOND_RESERVES_MSG"
	shieldedID                       = "SHIELDED"
	shieldedMsgID                    = "SHIELDED_MSG"
	orderID                          = "ORDER"
	lockedOrderBalMsgID              = "LOCKED_ORDER_BAL_MSG"
	creatingWalletsID                = "CREATING_WALLETS"
	addingServersID                  = "ADDING_SERVER"
	walletRecoverySupportMsgID       = "WALLET_RECOVERY_SUPPORT_MSG"
	ticketsPurchasedID               = "TICKETS_PURCHASED"
	ticketStatusUnknownID            = "TICKET_STATUS_UNKNOWN"
	ticketStatusUnminedID            = "TICKET_STATUS_UNMINED"
	ticketStatusImmatureID           = "TICKET_STATUS_IMMATURE"
	ticketStatusLiveID               = "TICKET_STATUS_LIVE"
	ticketStatusVotedID              = "TICKET_STATUS_VOTED"
	ticketStatusMissedID             = "TICKET_STATUS_MISSED"
	ticketStatusExpiredID            = "TICKET_STATUS_EXPIRED"
	ticketStatusUnspentID            = "TICKET_STATUS_UNSPENT"
	ticketStatusRevokedID            = "TICKET_STATUS_REVOKED"
	invalidSeedID                    = "INVALID_SEED"
	passwordResetSuccessMsgID        = "PASSWORD_RESET_SUCCESS_MSG"
	browserNtfnEnabledID             = "BROWSER_NTFN_ENABLED"
	browserNtfnOrdersID              = "BROWSER_NTFN_ORDERS"
	browserNtfnMatchesID             = "BROWSER_NTFN_MATCHES"
	browserNtfnBondsID               = "BROWSER_NTFN_BONDS"
	browserNtfnConnectionsID         = "BROWSER_NTFN_CONNECTIONS"
	orderBttnBuyBalErrID             = "ORDER_BUTTON_BUY_BALANCE_ERROR"
	orderBttnSellBalErrID            = "ORDER_BUTTON_SELL_BALANCE_ERROR"
	orderBttnQtyErrID                = "ORDER_BUTTON_QTY_ERROR"
	orderBttnQtyRateErrID            = "ORDER_BUTTON_QTY_RATE_ERROR"
	createAssetWalletMsgID           = "CREATE_ASSET_WALLET_MSG"
	noWalletMsgID                    = "NO_WALLET_MSG"
	tradingTierUpdateddID            = "TRADING_TIER_UPDATED"
	invalidTierValueID               = "INVALID_TIER_VALUE"
	invalidCompsValueID              = "INVALID_COMPS_VALUE"
	txTypeUnknownID                  = "TX_TYPE_UNKNOWN"
	txTypeSendID                     = "TX_TYPE_SEND"
	txTypeReceiveID                  = "TX_TYPE_RECEIVE"
	txTypeSwapID                     = "TX_TYPE_SWAP"
	txTypeRedeemID                   = "TX_TYPE_REDEEM"
	txTypeRefundID                   = "TX_TYPE_REFUND"
	txTypeSplitID                    = "TX_TYPE_SPLIT"
	txTypeCreateBondID               = "TX_TYPE_CREATE_BOND"
	txTypeRedeemBondID               = "TX_TYPE_REDEEM_BOND"
	txTypeApproveTokenID             = "TX_TYPE_APPROVE_TOKEN"
	txTypeAccelerationID             = "TX_TYPE_ACCELERATION"
	txTypeSelfTransferID             = "TX_TYPE_SELF_TRANSFER"
	txTypeRevokeTokenApprovalID      = "TX_TYPE_REVOKE_TOKEN_APPROVAL"
	txTypeTicketPurchaseID           = "TX_TYPE_TICKET_PURCHASE"
	txTypeTicketVoteID               = "TX_TYPE_TICKET_VOTE"
	txTypeTicketRevokeID             = "TX_TYPE_TICKET_REVOCATION"
	txTypeSwapOrSendID               = "TX_TYPE_SWAP_OR_SEND"
	txTypeMixID                      = "TX_TYPE_MIX"
	swapOrSendTooltipID              = "SWAP_OR_SEND_TOOLTIP"
	missingCexCredsID                = "MISSING_CEX_CREDS"
	matchBufferID                    = "MATCH_BUFFER"
	noPlacementsID                   = "NO_PLACEMENTS"
	invalidValueID                   = "INVALID_VALUE"
	noZeroID                         = "NO_ZERO"
	botTypeBasicMMID                 = "BOTTYPE_BASIC_MM"
	botTypeArbMMID                   = "BOTTYPE_ARB_MM"
	botTypeSimpleArbID               = "BOTTYPE_SIMPLE_ARB"
	botTypeNoneID                    = "NO_BOTTYPE"
	noCexID                          = "NO_CEX"
	cexBalanceErrID                  = "CEXBALANCE_ERR"
	pendingID                        = "PENDING"
	completeID                       = "COMPLETE"
	archivedSettingsID               = "ARCHIVED_SETTINGS"
	idTransparent                    = "TRANSPARENT"
)

var enUS = map[string]*intl.Translation{
	noPassErrMsgID:                   {T: "password cannot be empty"},
	noAppPassErrMsgID:                {T: "app password cannot be empty"},
	passwordNotMatchID:               {T: "passwords do not match"},
	setButtonBuyID:                   {T: "Place order to buy  {{ asset }}"},
	setButtonSellID:                  {T: "Place order to sell {{ asset }}"},
	orderBttnBuyBalErrID:             {T: "Insufficient balance to buy."},
	orderBttnSellBalErrID:            {T: "Insufficient balance to sell."},
	orderBttnQtyErrID:                {T: "Order quantity must be specified."},
	orderBttnQtyRateErrID:            {T: "Order quantity and price must be specified."},
	offID:                            {T: "off"},
	readyID:                          {T: "ready"},
	lockedID:                         {T: "locked"},
	noWalletID:                       {T: "no wallet"},
	walletSyncProgressID:             {T: "wallet is {{ syncProgress }}% synced"},
	hideAdditionalSettingsID:         {T: "hide additional settings"},
	showAdditionalSettingsID:         {T: "show additional settings"},
	buyID:                            {T: "Buy"},
	sellID:                           {T: "Sell"},
	notSupportedID:                   {T: "{{ asset }} is not supported"},
	versionNotSupportedID:            {T: "{{ asset }} (v{{version}}) is not supported"},
	connectionFailedID:               {T: "Connection to dex server failed. You can close bisonw and try again later or wait for it to reconnect."},
	orderPreviewID:                   {T: "Total: {{ total }} {{ asset }}"},
	calculatingID:                    {T: "calculating..."},
	estimateUnavailableID:            {T: "estimate unavailable"},
	noZeroRateID:                     {T: "zero rate not allowed"},
	noZeroQuantityID:                 {T: "zero quantity not allowed"},
	tradeID:                          {T: "trade"},
	noAssetWalletID:                  {T: "No {{ asset }} wallet"},
	executedID:                       {T: "executed"},
	bookedID:                         {T: "booked"},
	cancelingID:                      {T: "canceling"},
	acctUndefinedID:                  {T: "Account undefined."},
	keepWalletPassID:                 {T: "keep current wallet password"},
	newWalletPassID:                  {T: "set a new wallet password"},
	lotID:                            {T: "lot"},
	lotsID:                           {T: "lots"},
	unknownID:                        {T: "unknown"},
	epochID:                          {T: "epoch"},
	settlingID:                       {T: "settling"},
	noMatchID:                        {T: "no match"},
	canceledID:                       {T: "canceled"},
	revokedID:                        {T: "revoked"},
	waitingForConfsID:                {T: "Waiting for confirmations..."},
	noneSelectedID:                   {T: "none selected"},
	regFeeSuccessID:                  {Version: 1, T: "Fidelity bond accepted!"},
	addID:                            {T: "Add"},
	createID:                         {T: "Create"},
	walletReadyID:                    {T: "Ready"},
	setupWalletID:                    {T: "Setup"},
	changeWalletTypeID:               {T: "change the wallet type"},
	keepWalletTypeID:                 {T: "don't change the wallet type"},
	setupNeededID:                    {T: "Setup Needed"},
	walletPendingID:                  {T: "Creating Wallet"},
	sendSuccessID:                    {T: "{{ assetName }} Sent!"},
	reconfigSuccessID:                {T: "Wallet Reconfigured!"},
	rescanStartedID:                  {T: "Wallet Rescan Running"},
	newWalletSuccessID:               {T: "{{ assetName }} Wallet Created!"},
	walletUnlockedID:                 {T: "Wallet Unlocked"},
	sellingID:                        {T: "Selling"},
	buyingID:                         {T: "Buying"},
	walletEnabledID:                  {T: "{{ assetName }} Wallet Enabled"},
	walletDisabledID:                 {T: "{{ assetName }} Wallet Disabled"},
	disabledMsgID:                    {T: "wallet is disabled"},
	activeOrdersErrorID:              {T: "{{ assetName }} wallet is actively managing orders"},
	availableID:                      {T: "available"},
	immatureID:                       {T: "immature"},
	feeBalanceID:                     {T: "fee balance"},
	candlesLoadingID:                 {T: "waiting for candlesticks"},
	depthLoadingID:                   {T: "retrieving depth data"},
	invalidAddrressMsgID:             {T: "invalid address: {{ address }}"},
	txFeeSupportedID:                 {T: "fee estimation is not supported for this wallet type"},
	txFeeErrorMsgID:                  {T: "fee estimation failed: {{ err }}"},
	activeOrdersLogoutErrorID:        {T: "cannot logout with active orders"},
	invalidDateErrorMsgID:            {T: "error: invalid date or time"},
	noArchivedRecordsID:              {T: "No archived records found"},
	deleteArchivedRecordsID:          {T: "Message: {{ nRecords }} archived records has been deleted"},
	archivedRecordsPathID:            {T: "File Location: {{ path }}"},
	orderSubmittingID:                {T: "submitting"},
	defaultID:                        {T: "Default"},
	addedID:                          {T: "Added"},
	discoveredID:                     {T: "Discovered"},
	unsupportedAssetInfoErrMsgID:     {T: "no supported asset info for id = {{ assetID }}, and no exchange info provided"},
	limitOrderID:                     {T: "limit"},
	limitOrderImmediateTifID:         {T: "limit (i)", Notes: "i = immediate"},
	marketOrderID:                    {T: "market"},
	matchStatusNewlyMatchedID:        {T: "Newly Matched"},
	matchStatusMakerSwapCastID:       {T: "Maker Swap Sent"},
	matchStatusTakerSwapCastID:       {T: "Taker Swap Sent"},
	matchStatusMakerRedeemedID:       {T: "Maker Redeemed"},
	matchStatusRedemptionSentID:      {T: "Redemption Sent"},
	matchStatusRevokedID:             {T: "Revoked - {{ status }}"},
	matchStatusRefundPendingID:       {T: "Refund PENDING"},
	matchStatusRefundedID:            {T: "Refunded"},
	matchStatusRedeemPendingID:       {T: "Redeem PENDING"},
	matchStatusRedemptionConfirmedID: {T: "Redemption Confirmed"},
	matchStatusCompleteID:            {T: "Complete"},
	openWalletErrMsgID:               {T: "Error opening wallet: {{ msg }}"},
	orderAccelerationFeeErrMsgID:     {T: "Error estimating acceleration fee: {{ msg }}"},
	orderAccelerationErrMsgID:        {T: "Error accelerating order: {{ msg }}"},
	connectedID:                      {T: "Connected"},
	disconnectedID:                   {T: "Disconnected"},
	invalidCertID:                    {T: "Invalid Certificate"},
	confirmationsID:                  {T: "confirmations"},
	takerID:                          {T: "Taker"},
	makerID:                          {T: "Maker"},
	unavailableID:                    {T: "unavailable"},
	emptyDexAddrID:                   {T: "DEX address cannot be empty"},
	selectWalletForFeePaymentID:      {T: "Select a valid wallet to post a bond"},
	walletSyncFinishingID:            {T: "finishing up"},
	connectWalletErrMsgID:            {T: "Failed to connect {{ assetName }} wallet: {{ errMsg }}"},
	takerFoundMakerRedemptionID:      {T: "Redeemed by {{ makerAddr }}"},
	refundImminentID:                 {T: "Will happen in the next few blocks"},
	refundWillHappenAfterID:          {T: "Refund will happen after {{ refundAfterTime }}"},
	availableTitleID:                 {T: "Available"},
	lockedTitleID:                    {T: "Locked"},
	immatureTitleID:                  {T: "Immature"},
	swappingID:                       {T: "Swapping"},
	bondedID:                         {T: "Bonded"},
	lockedBalMsgID:                   {T: "Total funds temporarily locked to cover the costs of your bond maintenance, live orders, matches and other activities"},
	immatureBalMsgID:                 {T: "Incoming funds awaiting confirmation"},
	lockedSwappingBalMsgID:           {T: "Funds currently locked in settling matches"},
	lockedBonBalMsgID:                {T: "Funds locked in active bonds"},
	reservesDeficitID:                {T: "Reserves Deficit"},
	reservesDeficitMsgID:             {T: "The apparent wallet balance shortcoming to maintain bonding level. If this persists, you may need to add funds to stay fully bonded."},
	bondReservesID:                   {T: "Bond Reserves"},
	bondReservesMsgID:                {T: "Funds reserved to cover the expenses associated with bond maintenance"},
	shieldedID:                       {T: "Shielded"},
	shieldedMsgID:                    {T: "Total funds kept shielded"},
	orderID:                          {T: "Order"},
	lockedOrderBalMsgID:              {T: "Funds locked in unmatched orders"},
	creatingWalletsID:                {T: "Creating wallets"},
	addingServersID:                  {T: "Connecting to servers"},
	walletRecoverySupportMsgID:       {T: "Native {{ walletSymbol }} wallet failed to load properly. Try clicking the 'Recover' button below to fix it"},
	ticketsPurchasedID:               {T: "Purchasing {{ n }} Tickets!"},
	ticketStatusUnknownID:            {T: "unknown"},
	ticketStatusUnminedID:            {T: "unmined"},
	ticketStatusImmatureID:           {T: "immature"},
	ticketStatusLiveID:               {T: "live"},
	ticketStatusVotedID:              {T: "voted"},
	ticketStatusMissedID:             {T: "missed"},
	ticketStatusExpiredID:            {T: "expired"},
	ticketStatusUnspentID:            {T: "unspent"},
	ticketStatusRevokedID:            {T: "revoked"},
	invalidSeedID:                    {T: "Invalid seed"},
	passwordResetSuccessMsgID:        {T: "Your password reset was successful. You can proceed to login with your new password."},
	browserNtfnEnabledID:             {T: "Bison Wallet notifications enabled"},
	browserNtfnOrdersID:              {T: "Orders"},
	browserNtfnMatchesID:             {T: "Matches"},
	browserNtfnBondsID:               {T: "Bonds"},
	browserNtfnConnectionsID:         {T: "Server connections"},
	createAssetWalletMsgID:           {T: "Create a {{ asset }} wallet to trade"},
	noWalletMsgID:                    {T: "Create {{ asset1 }} and {{ asset2 }} wallet to trade"},
	tradingTierUpdateddID:            {T: "Trading Tier Updated"},
	invalidTierValueID:               {T: "Invalid tier value"},
	invalidCompsValueID:              {T: "Invalid comps value"},
	apiErrorID:                       {T: "api error: {{ msg }}"},
	txTypeUnknownID:                  {T: "Unknown"},
	txTypeSendID:                     {T: "Send"},
	txTypeReceiveID:                  {T: "Receive"},
	txTypeSwapID:                     {T: "Swap"},
	txTypeRedeemID:                   {T: "Redeem"},
	txTypeRefundID:                   {T: "Refund"},
	txTypeSplitID:                    {T: "Split"},
	txTypeCreateBondID:               {T: "Create bond"},
	txTypeRedeemBondID:               {T: "Redeem bond"},
	txTypeApproveTokenID:             {T: "Approve token"},
	txTypeAccelerationID:             {T: "Acceleration"},
	txTypeSelfTransferID:             {T: "Self transfer"},
	txTypeRevokeTokenApprovalID:      {T: "Revoke token approval"},
	txTypeTicketPurchaseID:           {T: "Ticket purchase"},
	txTypeTicketVoteID:               {T: "Ticket vote"},
	txTypeTicketRevokeID:             {T: "Ticket revocation"},
	txTypeSwapOrSendID:               {T: "Swap / Send"},
	txTypeMixID:                      {T: "Mix"},
	swapOrSendTooltipID:              {T: "The wallet was unable to determine if this transaction was a swap or a send."},
	missingCexCredsID:                {T: "specify both key and secret"},
	matchBufferID:                    {T: "Match buffer"},
	noPlacementsID:                   {T: "must specify 1 or more placements"},
	invalidValueID:                   {T: "invalid value"},
	noZeroID:                         {T: "zero not allowed"},
	botTypeBasicMMID:                 {T: "Market Maker"},
	botTypeArbMMID:                   {T: "Market Maker + Arbitrage"},
	botTypeSimpleArbID:               {Version: 1, T: "Arbitrage"},
	botTypeNoneID:                    {T: "choose a bot type"},
	noCexID:                          {T: "choose an exchange for arbitrage"},
	cexBalanceErrID:                  {T: "error fetching {{ cexName }} balance for {{ assetID }}: {{ err }}"},
	pendingID:                        {T: "Pending"},
	completeID:                       {T: "Complete"},
	archivedSettingsID:               {T: "Archived Settings"},
	idTransparent:                    {T: "Transparent"},
}

var ptBR = map[string]*intl.Translation{
	noPassErrMsgID:           {T: "senha não pode ser vazia"},
	noAppPassErrMsgID:        {T: "senha do app não pode ser vazia"},
	passwordNotMatchID:       {T: "senhas diferentes"},
	setButtonBuyID:           {T: "Ordem de compra de {{ asset }}"},
	setButtonSellID:          {T: "Ordem de venda de {{ asset }}"},
	offID:                    {T: "desligar"},
	readyID:                  {T: "pronto"},
	lockedID:                 {T: "trancado"},
	noWalletID:               {T: "sem carteira"},
	walletSyncProgressID:     {T: "carteira está {{ syncProgress }}% sincronizada"},
	hideAdditionalSettingsID: {T: "esconder configurações adicionais"},
	showAdditionalSettingsID: {T: "mostrar configurações adicionais"},
	buyID:                    {T: "Comprar"},
	sellID:                   {T: "Vender"},
	notSupportedID:           {T: "{{ asset }} não tem suporte"},
	connectionFailedID:       {T: "Conexão ao server dex falhou. Pode fechar bisonw e tentar novamente depois ou esperar para tentar se reconectar."},
	orderPreviewID:           {T: "Total: {{ total }} {{ asset }}"},
	calculatingID:            {T: "calculando..."},
	estimateUnavailableID:    {T: "estimativa indisponível"},
	noZeroRateID:             {T: "taxa não pode ser zero"},
	noZeroQuantityID:         {T: "quantidade não pode ser zero"},
	tradeID:                  {T: "troca"},
	noAssetWalletID:          {T: "Sem carteira {{ asset }}"},
	executedID:               {T: "executado"},
	bookedID:                 {T: "reservado"},
	cancelingID:              {T: "cancelando"},
	acctUndefinedID:          {T: "conta não definida."},
	keepWalletPassID:         {T: "manter senha da carteira"},
	newWalletPassID:          {T: "definir nova senha para carteira"},
	lotID:                    {T: "lote"},
	lotsID:                   {T: "lotes"},
	unknownID:                {T: "desconhecido"},
	epochID:                  {T: "epoque"},
	settlingID:               {T: "assentando"},
	noMatchID:                {T: "sem combinações"},
	canceledID:               {T: "cancelado"},
	revokedID:                {T: "revocado"},
	waitingForConfsID:        {T: "Esperando confirmações..."},
	noneSelectedID:           {T: "nenhuma selecionado"},
	regFeeSuccessID:          {T: "Sucesso no pagamento da taxa de registro!"},
	apiErrorID:               {T: "erro de API: {{ msg }}"},
	addID:                    {T: "Adicionar"},
	createID:                 {T: "Criar"},
	setupWalletID:            {T: "Configurar"},
	changeWalletTypeID:       {T: "trocar o tipo de carteira"},
	keepWalletTypeID:         {T: "Não trocara tipo de carteira"},
	walletReadyID:            {T: "Carteira Pronta"},
	setupNeededID:            {T: "Configuração Necessária"},
	availableID:              {T: "disponível"},
	immatureID:               {T: "imaturo"},
	maxID:                    {T: "ma"},
}

var zhCN = map[string]*intl.Translation{
	noPassErrMsgID:           {T: "密码不能为空"},
	noAppPassErrMsgID:        {T: "应用密码不能为空"},
	passwordNotMatchID:       {T: "密码不相同"},
	setButtonBuyID:           {T: "来自{{ asset }}的买入订单"},
	setButtonSellID:          {T: "来自{{ asset }}的卖出订单"},
	offID:                    {T: "关闭"},
	readyID:                  {T: "准备就绪"},
	lockedID:                 {T: "锁"},
	noWalletID:               {T: "未连接钱包"},
	walletSyncProgressID:     {T: "钱包同步进度{{ syncProgress }}%"},
	hideAdditionalSettingsID: {T: "隐藏其它设置"},
	showAdditionalSettingsID: {T: "显示其它设置"},
	buyID:                    {T: "买入"},
	sellID:                   {T: "卖出"},
	notSupportedID:           {T: "{{ asset }}不受支持"},
	connectionFailedID:       {T: "连接到服务器 dex 失败。您可以关闭 bisonw 并稍后重试或等待尝试重新连接。"},
	orderPreviewID:           {T: "总计： {{ total }} {{ asset }}"},
	calculatingID:            {T: "计算中..."},
	estimateUnavailableID:    {T: "估计不可用"},
	noZeroRateID:             {T: "汇率不能为零"},
	noZeroQuantityID:         {T: "数量不能为零"},
	tradeID:                  {T: "交易"},
	noAssetWalletID:          {T: "没有钱包 {{ asset }}"},
	executedID:               {T: "执行"},
	bookedID:                 {T: "保留"},
	cancelingID:              {T: "取消"},
	acctUndefinedID:          {T: "帐户未定义。"},
	keepWalletPassID:         {T: "保留钱包密码"},
	newWalletPassID:          {T: "设置新的钱包密码"},
	lotID:                    {T: "批处理"},
	lotsID:                   {T: "批"},
	epochID:                  {T: "时间"},
	apiErrorID:               {T: "接口错误: {{ msg }}"},
	addID:                    {T: "添加"},
	createID:                 {T: "创建"},
	availableID:              {T: "可用"},
	immatureID:               {T: "不成"},
}

var plPL = map[string]*intl.Translation{
	noPassErrMsgID:                   {T: "hasło nie może być puste"},
	noAppPassErrMsgID:                {T: "hasło aplikacji nie może być puste"},
	passwordNotMatchID:               {T: "hasła nie są jednakowe"},
	setButtonBuyID:                   {T: "Złóż zlecenie, aby kupić  {{ asset }}"},
	setButtonSellID:                  {T: "Złóż zlecenie, aby sprzedać {{ asset }}"},
	offID:                            {T: "wyłączony"},
	readyID:                          {T: "gotowy"},
	lockedID:                         {T: "zablokowany"},
	noWalletID:                       {T: "brak portfela"},
	walletSyncProgressID:             {T: "portfel zsynchronizowany w {{ syncProgress }}%"},
	hideAdditionalSettingsID:         {T: "ukryj dodatkowe ustawienia"},
	showAdditionalSettingsID:         {T: "pokaż dodatkowe ustawienia"},
	buyID:                            {T: "Kup"},
	sellID:                           {T: "Sprzedaj"},
	notSupportedID:                   {T: "{{ asset }} nie jest wspierany"},
	connectionFailedID:               {T: "Połączenie z serwerem dex nie powiodło się. Możesz zamknąć bisonw i spróbować ponownie później, lub poczekać na wznowienie połączenia."},
	orderPreviewID:                   {T: "W sumie: {{ total }} {{ asset }}"},
	calculatingID:                    {T: "obliczanie..."},
	estimateUnavailableID:            {T: "brak szacunkowego wyliczenia"},
	noZeroRateID:                     {T: "zero nie może być ceną"},
	noZeroQuantityID:                 {T: "zero nie może być ilością"},
	tradeID:                          {T: "handluj"},
	noAssetWalletID:                  {T: "Brak portfela {{ asset }}"},
	executedID:                       {T: "wykonano"},
	bookedID:                         {T: "zapisano"},
	cancelingID:                      {T: "anulowanie"},
	acctUndefinedID:                  {T: "Niezdefiniowane konto."},
	keepWalletPassID:                 {T: "zachowaj obecne hasło portfela"},
	newWalletPassID:                  {T: "ustaw nowe hasło portfela"},
	lotID:                            {T: "lot"},
	lotsID:                           {T: "loty(ów)"},
	unknownID:                        {T: "nieznane"},
	epochID:                          {T: "epoka"},
	settlingID:                       {T: "rozliczanie"},
	noMatchID:                        {T: "brak spasowania"},
	canceledID:                       {T: "anulowano"},
	revokedID:                        {T: "unieważniono"},
	waitingForConfsID:                {T: "Oczekiwanie na potwierdzenia..."},
	noneSelectedID:                   {T: "brak zaznaczenia"},
	apiErrorID:                       {T: "błąd API: {{ msg }}"},
	addID:                            {T: "Dodaj"},
	createID:                         {T: "Utwórz"},
	setupWalletID:                    {T: "Konfiguracja"},
	changeWalletTypeID:               {T: "zmień typ portfela"},
	keepWalletTypeID:                 {T: "nie zmieniaj typu portfela"},
	walletReadyID:                    {T: "Portfel jest gotowy"},
	setupNeededID:                    {T: "Potrzebna konfiguracja"},
	availableID:                      {T: "dostępne"},
	immatureID:                       {T: "niedojrzałe"},
	lockedOrderBalMsgID:              {T: "Środki zablokowane w niesparowanych zamówieniach"},
	invalidCertID:                    {T: "Nieważny certyfikat"},
	connectedID:                      {T: "Połączono"},
	invalidTierValueID:               {T: "Niepoprawna wartość poziomu"},
	selectWalletForFeePaymentID:      {T: "Wybierz odpowiedni portfel, aby opłacić kaucję"},
	makerID:                          {T: "Maker"},
	disconnectedID:                   {T: "Rozłączono"},
	reservesDeficitMsgID:             {T: "Brak odpowiedniego salda portfela do utrzymania poziomu kaucji. Jeśli ten problem będzie się utrzymywał, konieczne może być dodanie środków, aby utrzymać kaucję."},
	connectWalletErrMsgID:            {T: "Połączenie z portfelem {{ assetName }} nie powiodło się: {{ errMsg }}"},
	matchStatusTakerSwapCastID:       {T: "Wysłano swap taker"},
	txTypeAccelerationID:             {T: "Przyspieszenie"},
	orderBttnQtyErrID:                {T: "Ilość zamówień musi zostać określona."},
	botTypeSimpleArbID:               {T: "Prosty arbitraż"},
	walletPendingID:                  {T: "Tworzenie portfela"},
	bondReservesID:                   {T: "Rezerwy kaucji"},
	invalidValueID:                   {T: "nieprawidłowa wartość"},
	disabledMsgID:                    {T: "portfel jest wyłączony"},
	walletRecoverySupportMsgID:       {T: "Błąd wczytywania wbudowanego portfela {{ walletSymbol }}. Kliknij przycisk 'Odtwórz' poniżej, aby to naprawić"},
	sendSuccessID:                    {T: "Wysłano {{ assetName }}!"},
	limitOrderImmediateTifID:         {T: "limit (i)"},
	txTypeSwapID:                     {T: "Swap"},
	txFeeErrorMsgID:                  {T: "szacowanie opłaty nie powiodło się: {{ err }}"},
	botTypeBasicMMID:                 {T: "Animacja rynku"},
	botTypeNoneID:                    {T: "wybierz rodzaj bota"},
	orderBttnQtyRateErrID:            {T: "Ilość zamówień i cena muszą zostać określone."},
	shieldedMsgID:                    {T: "Suma osłoniętych środków"},
	noZeroID:                         {T: "wartość nie może być zerowa"},
	marketOrderID:                    {T: "market"},
	walletDisabledID:                 {T: "Portfel {{ assetName }} jest wyłączony"},
	missingCexCredsID:                {T: "określ zarówno klucz, jak i sekret"},
	takerID:                          {T: "Taker"},
	txTypeCreateBondID:               {T: "Utwórz kaucję"},
	confirmationsID:                  {T: "potwierdzenia"},
	deleteArchivedRecordsID:          {T: "Wiadomość: usunięto {{ nRecords }} wiadomości archiwalnych"},
	browserNtfnMatchesID:             {T: "Sparowania"},
	rescanStartedID:                  {T: "Trwa ponowne skanowanie portfela"},
	addingServersID:                  {T: "Łączenie z serwerami"},
	swappingID:                       {T: "Wymiana"},
	matchStatusRefundedID:            {T: "Zwrócono"},
	matchStatusRevokedID:             {T: "Odrzucono - {{ status }}"},
	txTypeRedeemID:                   {T: "Wykup"},
	orderAccelerationFeeErrMsgID:     {T: "Błąd przy szacowaniu opłaty przyspieszenia: {{ msg }}"},
	ticketStatusRevokedID:            {T: "odwołany"},
	limitOrderID:                     {T: "limit"},
	orderBttnBuyBalErrID:             {T: "Brak wystarczających środków do zakupu."},
	lockedBalMsgID:                   {T: "Suma środków tymczasowo zablokowanych na pokrycie kaucji, wystawionych zamówień, sparowań, oraz innych czynności"},
	unavailableID:                    {T: "niedostępny"},
	createAssetWalletMsgID:           {T: "Utwórz portfel {{ asset }}, aby rozpocząć handel"},
	sellingID:                        {T: "Sprzedaż"},
	discoveredID:                     {T: "Odkryto"},
	ticketStatusUnspentID:            {T: "niewykorzystany"},
	browserNtfnBondsID:               {T: "Kaucje"},
	invalidCompsValueID:              {T: "Nieprawidłowa wartość zrównoważenia"},
	buyingID:                         {T: "Kupno"},
	ticketsPurchasedID:               {T: "Zakupiono {{ n }} biletów!"},
	browserNtfnOrdersID:              {T: "Zamówienia"},
	invalidAddrressMsgID:             {T: "nieprawidłowy adres: {{ address }}"},
	browserNtfnConnectionsID:         {T: "Połączenia z serwerami"},
	txTypeTicketVoteID:               {T: "Głos"},
	reservesDeficitID:                {T: "Deficyt rezerw"},
	bondedID:                         {T: "Zabezpieczone kaucją"},
	creatingWalletsID:                {T: "Tworzenie portfeli"},
	orderID:                          {T: "Zamówienie"},
	emptyDexAddrID:                   {T: "Pole adresu DEX nie może być puste"},
	addedID:                          {T: "Dodano"},
	tradingTierUpdateddID:            {T: "Zaktualizowano poziom handlu"},
	txTypeTicketPurchaseID:           {T: "Zakup biletu"},
	unsupportedAssetInfoErrMsgID:     {T: "brak wspieranego aktywa dla id = {{ assetID }}, oraz brak informacji o giełdzie"},
	orderSubmittingID:                {T: "składanie"},
	ticketStatusMissedID:             {T: "przegapiony"},
	feeBalanceID:                     {T: "saldo opłat"},
	browserNtfnEnabledID:             {T: "Powiadomienia DCRDEX są włączone"},
	ticketStatusExpiredID:            {T: "wygasły"},
	txFeeSupportedID:                 {T: "szacowanie opłat dla tego rodzaju portfela nie jest wspierane"},
	matchStatusRedeemPendingID:       {T: "Wykup środków w toku"},
	passwordResetSuccessMsgID:        {T: "Hasło zostało zresetowane pomyślnie. Możesz zalogować się z użyciem nowego hasła."},
	invalidSeedID:                    {T: "Nieprawidłowe ziarno"},
	depthLoadingID:                   {T: "pobieranie danych o głębokości"},
	txTypeApproveTokenID:             {T: "Zatwierdź token"},
	txTypeSendID:                     {T: "Wyślij"},
	openWalletErrMsgID:               {T: "Błąd otwierania portfela: {{ msg }}"},
	reconfigSuccessID:                {T: "Konfiguracja portfela powiodła się!"},
	ticketStatusUnminedID:            {T: "niewydobyty"},
	immatureBalMsgID:                 {T: "Nadchodzące środki oczekujące na potwierdzenie"},
	matchStatusRedemptionSentID:      {T: "Wysłano transakcję wykupu"},
	availableTitleID:                 {T: "Dostępne"},
	newWalletSuccessID:               {T: "Utworzono portfel {{ assetName }}!"},
	txTypeTicketRevokeID:             {T: "Odwołanie biletu"},
	shieldedID:                       {T: "Osłonięte"},
	takerFoundMakerRedemptionID:      {T: "Wykupione przez {{ makerAddr }}"},
	lockedSwappingBalMsgID:           {T: "Środki obecnie zablokowane w rozliczanych sparowaniach"},
	refundImminentID:                 {T: "Odbędzie się w następnych paru blokach"},
	immatureTitleID:                  {T: "Niedojrzałe"},
	matchStatusMakerSwapCastID:       {T: "Wysłano swap maker"},
	matchStatusRedemptionConfirmedID: {T: "Wykup potwierdzony"},
	txTypeRefundID:                   {T: "Zwrot"},
	orderBttnSellBalErrID:            {T: "Brak wystarczających środków do sprzedaży."},
	txTypeSelfTransferID:             {T: "Przelew własny"},
	noWalletMsgID:                    {T: "Utwórz portfele {{ asset1 }} oraz {{ asset2 }}, aby rozpocząć handel"},
	ticketStatusUnknownID:            {T: "nieznany"},
	ticketStatusVotedID:              {T: "oddano głos"},
	txTypeRevokeTokenApprovalID:      {T: "Wycofaj zatwierdzenie tokena"},
	lockedBonBalMsgID:                {T: "Środki zablokowane w aktywnych kaucjach"},
	activeOrdersLogoutErrorID:        {T: "nie można wylogować się z aktywnymi zamówieniami"},
	activeOrdersErrorID:              {T: "Portfel {{ assetName }} zarządza aktywnymi zamówieniami"},
	lockedTitleID:                    {T: "Zablokowane"},
	noCexID:                          {T: "wybierz giełdę do arbitrażu"},
	bondReservesMsgID:                {T: "Środki zarezerwowane na pokrycie kosztów związanych z utrzymaniem kaucji"},
	walletUnlockedID:                 {T: "Odblokowano portfel"},
	matchStatusNewlyMatchedID:        {T: "Świeżo sparowane"},
	regFeeSuccessID:                  {Version: 1, T: "Kaucja lojalnościowa została przyjęta!"},
	botTypeArbMMID:                   {T: "Animacja rynku + arbitraż"},
	txTypeRedeemBondID:               {T: "Wykup kaucji"},
	ticketStatusImmatureID:           {T: "niedojrzałe"},
	archivedRecordsPathID:            {T: "Lokalizacja pliku:  {{ path }}"},
	candlesLoadingID:                 {T: "oczekiwanie na świece"},
	walletEnabledID:                  {T: "Portfel {{ assetName }} jest włączony"},
	matchStatusMakerRedeemedID:       {T: "Maker wykupił środki"},
	noPlacementsID:                   {T: "1 lub więcej miejsc musi zostać określone"},
	cexBalanceErrID:                  {T: "błąd pobierania salda {{ cexName }} dla {{ assetID }}: {{ err }}"},
	refundWillHappenAfterID:          {T: "Zwrot środków za {{ refundAfterTime }}"},
	txTypeSplitID:                    {T: "Dzielona"},
	versionNotSupportedID:            {T: "{{ asset }} (v{{version}}) nie jest wspierana"},
	matchStatusCompleteID:            {T: "Zakończone"},
	noArchivedRecordsID:              {T: "Brak zapisów archiwalnych"},
	ticketStatusLiveID:               {T: "gotowy do głosowania"},
	orderAccelerationErrMsgID:        {T: "Błąd przyspieszenia zamówienia: {{ msg }}"},
	matchStatusRefundPendingID:       {T: "Zwrot środków W TOKU"},
	invalidDateErrorMsgID:            {T: "błąd: nieprawidłowa data lub czas"},
	defaultID:                        {T: "Domyślne"},
	txTypeUnknownID:                  {T: "Nieznany"},
	walletSyncFinishingID:            {T: "na ukończeniu"},
	txTypeReceiveID:                  {T: "Odbiór"},
}

var deDE = map[string]*intl.Translation{
	noPassErrMsgID:           {T: "Passwort darf nicht leer sein"},
	noAppPassErrMsgID:        {T: "App-Passwort darf nicht leer sein"},
	passwordNotMatchID:       {T: "Passwörter stimmen nicht überein"},
	setButtonBuyID:           {T: "Platziere Auftrag zum Kauf von  {{ asset }}"},
	setButtonSellID:          {T: "Platziere Auftrag zum Verkauf von {{ asset }}"},
	offID:                    {T: "aus"},
	readyID:                  {T: "bereit"},
	lockedID:                 {T: "gesperrt"},
	noWalletID:               {T: "kein Wallet"},
	walletSyncProgressID:     {T: "Wallet ist zu {{ syncProgress }}% synchronisiert"},
	hideAdditionalSettingsID: {T: "zusätzliche Einstellungen ausblenden"},
	showAdditionalSettingsID: {T: "zusätzliche Einstellungen anzeigen"},
	buyID:                    {T: "Kaufen"},
	sellID:                   {T: "Verkaufen"},
	notSupportedID:           {T: "{{ asset }} wird nicht unterstützt"},
	connectionFailedID:       {T: "Die Verbindung zum Dex-Server fehlgeschlagen. Du kannst bisonw schließen und es später erneut versuchen oder warten bis die Verbindung wiederhergestellt ist."},
	orderPreviewID:           {T: "Insgesamt: {{ total }} {{ asset }}"},
	calculatingID:            {T: "kalkuliere..."},
	estimateUnavailableID:    {T: "Schätzung nicht verfügbar"},
	noZeroRateID:             {T: "Null-Satz nicht erlaubt"},
	noZeroQuantityID:         {T: "Null-Menge nicht erlaubt"},
	tradeID:                  {T: "Handel"},
	noAssetWalletID:          {T: "Kein {{ asset }} Wallet"},
	executedID:               {T: "ausgeführt"},
	bookedID:                 {T: "gebucht"},
	cancelingID:              {T: "Abbruch"},
	acctUndefinedID:          {T: "Account undefiniert."},
	keepWalletPassID:         {T: "aktuelles Passwort für das Wallet behalten"},
	newWalletPassID:          {T: "ein neues Passwort für das Wallet festlegen"},
	lotID:                    {T: "Lot"},
	lotsID:                   {T: "Lots"},
	unknownID:                {T: "unbekannt"},
	epochID:                  {T: "Epoche"},
	settlingID:               {T: "Abwicklung"},
	noMatchID:                {T: "kein Match"},
	canceledID:               {T: "abgebrochen"},
	revokedID:                {T: "widerrufen"},
	waitingForConfsID:        {T: "Warten auf Bestätigungen..."},
	noneSelectedID:           {T: "keine ausgewählt"},
	regFeeSuccessID:          {T: "Zahlung der Registrierungsgebühr erfolgreich!"},
	apiErrorID:               {T: "API Fehler: {{ msg }}"},
	addID:                    {T: "Hinzufügen"},
	createID:                 {T: "Erstellen"},
	setupWalletID:            {T: "Einrichten"},
	changeWalletTypeID:       {T: "den Wallet-Typ ändern"},
	keepWalletTypeID:         {T: "den Wallet-Typ nicht ändern"},
	walletReadyID:            {T: "Wallet bereit"},
	setupNeededID:            {T: "Einrichtung erforderlich"},
	walletPendingID:          {T: "Erstelle Wallet"},
	sendSuccessID:            {T: "{{ assetName }} gesendet!"},
	reconfigSuccessID:        {T: "Wallet neu konfiguriert!"},
	rescanStartedID:          {T: "Wallet Rescan läuft"},
	newWalletSuccessID:       {T: "{{ assetName }} Wallet erstellt!"},
	walletUnlockedID:         {T: "Wallet entsperrt"},
}

var ar = map[string]*intl.Translation{
	noPassErrMsgID:                   {T: "لا يمكن أن تكون كلمة المرور فارغة"},
	noAppPassErrMsgID:                {T: "لا يمكن أن تكون كلمة مرور التطبيق فارغة"},
	passwordNotMatchID:               {T: "كلمات المرور غير متطابقة"},
	setButtonBuyID:                   {T: "ضع طلبًا للشراء  {{ asset }}"},
	setButtonSellID:                  {T: "ضع طلبًا للبيع {{ asset }}"},
	offID:                            {T: "إيقاف"},
	readyID:                          {T: "متوقف"},
	lockedID:                         {T: "مقفل"},
	noWalletID:                       {T: "لا توجد أي محفظة"},
	walletSyncProgressID:             {T: "تمت مزامنة {{ syncProgress }}% المحفظة"},
	hideAdditionalSettingsID:         {T: "إخفاء الإعدادات الإضافية"},
	showAdditionalSettingsID:         {T: "عرض الإعدادات الإضافية"},
	buyID:                            {T: "شراء"},
	sellID:                           {T: "بيع"},
	notSupportedID:                   {T: "{{ asset }} غير مدعوم"},
	connectionFailedID:               {T: "فشل الاتصال بخادم dex. يمكنك إغلاق dexc والمحاولة مرة أخرى لاحقًا أو انتظار إعادة الاتصال."},
	orderPreviewID:                   {T: "إجمالي: {{ total }} {{ asset }}"},
	calculatingID:                    {T: "جاري الحساب ..."},
	estimateUnavailableID:            {T: "التقديرات غير متاحة"},
	noZeroRateID:                     {T: "معدل الصفر غير مسموح به"},
	noZeroQuantityID:                 {T: "غير مسموح بالكمية الصفرية"},
	tradeID:                          {T: "التداول"},
	noAssetWalletID:                  {T: "لا توجد {{ asset }} محفظة"},
	executedID:                       {T: "تم تنفيذها"},
	bookedID:                         {T: "تم الحجز"},
	cancelingID:                      {T: "جارٍ الإلغاء"},
	acctUndefinedID:                  {T: "حساب غير محدد."},
	keepWalletPassID:                 {T: "احتفظ بكلمة مرور المحفظة الحالية"},
	newWalletPassID:                  {T: "قم بتعيين كلمة مرور جديدة للمحفظة"},
	lotID:                            {T: "الحصة"},
	lotsID:                           {T: "الحصص"},
	unknownID:                        {T: "غير معروف"},
	epochID:                          {T: "الحقبة الزمنية"},
	settlingID:                       {T: "الإعدادات"},
	noMatchID:                        {T: "غير متطابقة"},
	canceledID:                       {T: "ملغاة"},
	revokedID:                        {T: "مستعادة"},
	waitingForConfsID:                {T: "في انتظار التأكيدات ..."},
	noneSelectedID:                   {T: "لم يتم تحديد أي شيء"},
	apiErrorID:                       {T: "خطأ في واجهة برمجة التطبيقات :{{ msg }}"},
	addID:                            {T: "إضافة"},
	createID:                         {T: "إنشاء"},
	setupWalletID:                    {T: "إعداد"},
	changeWalletTypeID:               {T: "تغيير نوع المحفظة"},
	keepWalletTypeID:                 {T: "لا تغير نوع المحفظة"},
	walletReadyID:                    {T: "المحفظة جاهزة"},
	setupNeededID:                    {T: "الإعداد مطلوب"},
	walletPendingID:                  {T: "إنشاء المحفظة"},
	sendSuccessID:                    {T: "{{ assetName }} تم الإرسال!"},
	reconfigSuccessID:                {T: "تمت إعادة تهيئة المحفظة!!"},
	rescanStartedID:                  {T: "إعادة فحص المحفظة قيد التشغيل"},
	newWalletSuccessID:               {T: "{{ assetName }} تم إنشاء المحفظة!"},
	walletUnlockedID:                 {T: "المحفظة غير مقفلة"},
	sellingID:                        {T: "البيع"},
	buyingID:                         {T: "Bالشراء"},
	walletEnabledID:                  {T: "{{ assetName }} المحفظة ممكنة"},
	walletDisabledID:                 {T: "{{ assetName }} المحفظة معطلة"},
	disabledMsgID:                    {T: "تم تعطيل المحفظة"},
	activeOrdersErrorID:              {T: "{{ assetName }} تدير المحفظة  الطلبات بفعالية"},
	orderBttnQtyRateErrID:            {T: "يجب تحديد كمية وسعر الطلب."},
	reservesDeficitMsgID:             {T: "النقص في رصيد المحفظة القابل للوصول للحفاظ على مستوى السند. إذا استمر هذا الوضع، قد تحتاج إلى إضافة أموال للبقاء مضمونا بسند."},
	invalidCertID:                    {T: "شهادة غير صالحة"},
	availableTitleID:                 {T: "متاح"},
	addedID:                          {T: "تمت الإضافة"},
	ticketStatusUnspentID:            {T: "غير منفقة"},
	txTypeTicketVoteID:               {T: "تصويت التذكرة"},
	disconnectedID:                   {T: "انقطع الاتصال"},
	shieldedID:                       {T: "محمية"},
	ticketStatusExpiredID:            {T: "منتهية الصلاحية"},
	txTypeRefundID:                   {T: "استرداد"},
	discoveredID:                     {T: "مكتشفة"},
	versionNotSupportedID:            {T: "{{ asset }} (v{{version}}) غير مدعوم"},
	noWalletMsgID:                    {T: "أنشئ محفظة لتداول {{ asset1 }} و {{ asset2 }}"},
	noPlacementsID:                   {T: "يجب تحديد موضع واحد أو أكثر"},
	ticketStatusUnknownID:            {T: "غير معروفة"},
	matchStatusMakerRedeemedID:       {T: "استرداد صانع السوق"},
	txTypeSelfTransferID:             {T: "التحويل الذاتي"},
	tradingTierUpdateddID:            {T: "تم تحديث مستوى التداول"},
	txTypeApproveTokenID:             {T: "اعتماد التوكن"},
	cexBalanceErrID:                  {T: "خطأ في جلب رصيد {{ cexName }} لـ {{ assetID }}: {{ err }}"},
	txTypeTicketRevokeID:             {T: "استرجاع التذكرة"},
	makerID:                          {T: "صانع السوق"},
	txTypeAccelerationID:             {T: "التعجيل"},
	noZeroID:                         {T: "الصفر غير مسموح"},
	txTypeReceiveID:                  {T: "استلام"},
	invalidValueID:                   {T: "قيمة غير صحيحة"},
	connectWalletErrMsgID:            {T: "فشل في الاتصال بمحفظة {{ assetName }}: {{ errMsg }}"},
	matchStatusRevokedID:             {T: "مسترجعة - {{ status }}"},
	lockedBonBalMsgID:                {T: "الأموال مقفلة في سندات نشطة"},
	orderID:                          {T: "طلب"},
	regFeeSuccessID:                  {Version: 1, T: "تم قبول سند الاخلاص!"},
	connectedID:                      {T: "متصل"},
	limitOrderID:                     {T: "حد السعر"},
	lockedOrderBalMsgID:              {T: "الأموال مقفلة في الطلبات غير المتطابقة"},
	limitOrderImmediateTifID:         {T: "حد السعر (i)"},
	swappingID:                       {T: "المقايضة"},
	refundWillHappenAfterID:          {T: "سيحدث الاسترداد بعد {{ refundAfterTime }}"},
	takerFoundMakerRedemptionID:      {T: "تم الاسترداد بواسطة {{ makerAddr }}"},
	ticketStatusRevokedID:            {T: "مسترجعة"},
	matchStatusRedemptionSentID:      {T: "تم إرسال الاسترداد"},
	ticketStatusVotedID:              {T: "مُصوِّتة"},
	browserNtfnBondsID:               {T: "السندات"},
	orderBttnBuyBalErrID:             {T: "رصيد غير كاف للشراء."},
	matchStatusRefundedID:            {T: "تم الاسترداد"},
	bondReservesMsgID:                {T: "الأموال المخصصة لتغطية النفقات المرتبطة بصيانة السندات"},
	noArchivedRecordsID:              {T: "لم يتم العثور على سجلات مؤرشفة"},
	marketOrderID:                    {T: "السوق"},
	walletSyncFinishingID:            {T: "الانتهاء"},
	browserNtfnMatchesID:             {T: "التطابقات"},
	noCexID:                          {T: "اختر منصة مبادلات للمراجحة"},
	walletRecoverySupportMsgID:       {T: "فشل تحميل محفظة {{ walletSymbol }} الأصلية بشكل صحيح. حاول النقر على زر 'استعادة' أدناه لإصلاحها"},
	invalidSeedID:                    {T: "بذرة غير صحيحة"},
	invalidCompsValueID:              {T: "قيمة المقارنات غير صحيحة"},
	immatureBalMsgID:                 {T: "الأموال الواردة في انتظار التأكيد"},
	archivedRecordsPathID:            {T: "موقع الملف: {{ path }}"},
	passwordResetSuccessMsgID:        {T: "تمت إعادة تعيين كلمة المرور الخاصة بك بنجاح. يمكنك المتابعة لتسجيل الدخول باستخدام كلمة المرور الجديدة."},
	matchStatusNewlyMatchedID:        {T: "مطابقة حديثة"},
	ticketStatusLiveID:               {T: "حية"},
	matchStatusRefundPendingID:       {T: "الاسترداد قيد الانتظار"},
	selectWalletForFeePaymentID:      {T: "حدد محفظة صالحة لنشر السند"},
	matchStatusRedemptionConfirmedID: {T: "تم تأكيد الإسترداد"},
	matchStatusCompleteID:            {T: "مكتملة"},
	ticketStatusUnminedID:            {T: "غير معدنة"},
	orderAccelerationErrMsgID:        {T: "خطأ في تسريع الطلب: {{ msg }}"},
	missingCexCredsID:                {T: "حدد كلاً من المفتاح والسر"},
	lockedSwappingBalMsgID:           {T: "الأموال مقفلة حاليا في تسوية المطابقات"},
	invalidAddrressMsgID:             {T: "عنوان غير صحيح: {{ address }}"},
	openWalletErrMsgID:               {T: "خطأ في فتح المحفظة: {{ msg }}"},
	botTypeBasicMMID:                 {T: "صانع السوق"},
	shieldedMsgID:                    {T: "إجمالي الأموال المحتفظ بها محمية"},
	txFeeErrorMsgID:                  {T: "فشل تقدير الرسوم: {{ err }}"},
	createAssetWalletMsgID:           {T: "أنشئ محفظة {{ asset }} للتداول"},
	takerID:                          {T: "المستفيد"},
	lockedBalMsgID:                   {T: "إجمالي الأموال مقفلة مؤقتًا لتغطية تكاليف صيانة السندات، والطلبات الحية، والمطابقات والأنشطة الأخرى"},
	matchStatusTakerSwapCastID:       {T: "تم إرسال مقايضة المستفيد"},
	defaultID:                        {T: "افتراضي"},
	botTypeNoneID:                    {T: "اختر نوع البوت"},
	availableID:                      {T: "متوفر"},
	creatingWalletsID:                {T: "إنشاء المحافظ"},
	orderSubmittingID:                {T: "تقديم"},
	deleteArchivedRecordsID:          {T: "الرسالة: تم حذف {{ nRecords }} من السجلات المؤرشفة"},
	ticketStatusImmatureID:           {T: "غير ناضجة"},
	txTypeRevokeTokenApprovalID:      {T: "إلغاء الموافقة بالتوكن"},
	txTypeRedeemBondID:               {T: "استرداد السند"},
	browserNtfnEnabledID:             {T: "تم تفعيل إشعارات منصة المبادلات اللامركزية لديكريد DCRDEX"},
	orderAccelerationFeeErrMsgID:     {T: "خطأ في تقدير رسوم التسريع: {{ msg }}"},
	depthLoadingID:                   {T: "استرجاع بيانات العمق"},
	matchStatusMakerSwapCastID:       {T: "تم إرسال مقايضة صانع السوق"},
	feeBalanceID:                     {T: "رصيد الرسوم"},
	confirmationsID:                  {T: "التأكيدات"},
	addingServersID:                  {T: "الاتصال بالخوادم"},
	invalidTierValueID:               {T: "قيمة المستوى غير صحيحة"},
	activeOrdersLogoutErrorID:        {T: "لا يمكن تسجيل الخروج مع وجود طلبات نشطة"},
	browserNtfnOrdersID:              {T: "الطلبات"},
	botTypeArbMMID:                   {T: "صانع السوق + المراجحة"},
	bondedID:                         {T: "مضمون بسند"},
	botTypeSimpleArbID:               {T: "المراجحة البسيطة"},
	txTypeSendID:                     {T: "إرسال"},
	txTypeTicketPurchaseID:           {T: "شراء التذكرة"},
	unsupportedAssetInfoErrMsgID:     {T: "لا توجد معلومات عن الأصول المدعومة للمعرف = {{ assetID }}، ولم يتم تقديم معلومات عن منصة المبادلات"},
	ticketsPurchasedID:               {T: "شراء {{ n }} تذاكر!"},
	refundImminentID:                 {T: "سيحدث في الكتل القليلة القادمة"},
	candlesLoadingID:                 {T: "في انتظار أعمدة الشموع"},
	immatureID:                       {T: "غير ناضجة"},
	unavailableID:                    {T: "غير متوفرة"},
	bondReservesID:                   {T: "احتياطيات السندات"},
	txTypeUnknownID:                  {T: "غير معروفة"},
	txTypeCreateBondID:               {T: "إنشاء السند"},
	emptyDexAddrID:                   {T: "لا يمكن أن يكون عنوان منصة المبادلات اللامركزية DEX فارغًا"},
	txTypeRedeemID:                   {T: "استرداد"},
	orderBttnQtyErrID:                {T: "يجب تحديد كمية الطلب."},
	immatureTitleID:                  {T: "غير ناضجة"},
	invalidDateErrorMsgID:            {T: "خطأ: تاريخ أو وقت غير صحيح"},
	reservesDeficitID:                {T: "عجز الاحتياطيات"},
	browserNtfnConnectionsID:         {T: "اتصالات الخادم"},
	lockedTitleID:                    {T: "مقفل"},
	txTypeSplitID:                    {T: "تقسيم"},
	matchStatusRedeemPendingID:       {T: "الاسترداد قيد الانتظار"},
	txTypeSwapID:                     {T: "مقايضة"},
	ticketStatusMissedID:             {T: "مفوتة"},
	txFeeSupportedID:                 {T: "تقدير الرسوم غير مدعوم لهذا النوع من المحفظة"},
	orderBttnSellBalErrID:            {T: "الرصيد غير كافي للبيع."},
}

var localesMap = map[string]map[string]*intl.Translation{
	"en-US": enUS,
	"pt-BR": ptBR,
	"zh-CN": zhCN,
	"pl-PL": plPL,
	"de-DE": deDE,
	"ar":    ar,
}

// RegisterTranslations registers translations with the init package for
// translator worksheet preparation.
func RegisterTranslations() {
	const callerID = "js"

	for lang, ts := range localesMap {
		r := intl.NewRegistrar(callerID, lang, len(ts))
		for translationID, t := range ts {
			r.Register(translationID, t)
		}
	}
}
