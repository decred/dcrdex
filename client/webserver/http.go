// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package webserver

import (
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/order"
	qrcode "github.com/skip2/go-qrcode"
)

const (
	homeRoute        = "/"
	registerRoute    = "/register"
	loginRoute       = "/login"
	marketsRoute     = "/markets"
	walletsRoute     = "/wallets"
	walletLogRoute   = "/wallets/logfile"
	settingsRoute    = "/settings"
	ordersRoute      = "/orders"
	exportOrderRoute = "/orders/export"
)

// sendTemplate processes the template and sends the result.
func (s *WebServer) sendTemplate(w http.ResponseWriter, tmplID string, data interface{}) {
	page, err := s.html.exec(tmplID, data)
	if err != nil {
		log.Errorf("template exec error for %s: %v", tmplID, err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, page)
}

// CommonArguments are common page arguments that must be supplied to every
// page to populate the <title> and <header> elements.
type CommonArguments struct {
	UserInfo *userInfo
	Title    string
}

// Create the CommonArguments for the request.
func commonArgs(r *http.Request, title string) *CommonArguments {
	return &CommonArguments{
		UserInfo: extractUserInfo(r),
		Title:    title,
	}
}

// handleHome is the handler for the '/' page request. It redirects the
// requester to the markets page.
func (s *WebServer) handleHome(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, marketsRoute, http.StatusSeeOther)
}

// handleLogin is the handler for the '/login' page request.
func (s *WebServer) handleLogin(w http.ResponseWriter, r *http.Request) {
	cArgs := commonArgs(r, "Login | Decred DEX")
	if cArgs.UserInfo.Authed {
		http.Redirect(w, r, marketsRoute, http.StatusSeeOther)
		return
	}
	s.sendTemplate(w, "login", cArgs)
}

// registerTmplData is template data for the /register page.
type registerTmplData struct {
	CommonArguments
	Initialized    bool
	Authed         bool
	KnownExchanges []string
}

// handleRegister is the handler for the '/register' page request.
func (s *WebServer) handleRegister(w http.ResponseWriter, r *http.Request) {
	common := commonArgs(r, "Register | Decred DEX")
	s.sendTemplate(w, "register", &registerTmplData{
		CommonArguments: *common,
		KnownExchanges:  s.knownUnregisteredExchanges(common.UserInfo.Exchanges),
	})
}

// knownUnregisteredExchanges returns all the known exchanges that
// the user has not registered for.
func (s *WebServer) knownUnregisteredExchanges(registeredExchanges map[string]*core.Exchange) []string {
	certs := core.CertStore[s.core.Network()]
	exchanges := make([]string, 0, len(certs))
	for host := range certs {
		if _, alreadyRegistered := registeredExchanges[host]; !alreadyRegistered {
			exchanges = append(exchanges, host)
		}
	}
	return exchanges
}

// marketResult is the template data for the `/markets` page request.
type marketTmplData struct {
	CommonArguments
	Exchanges map[string]*core.Exchange
}

// handleMarkets is the handler for the '/markets' page request.
func (s *WebServer) handleMarkets(w http.ResponseWriter, r *http.Request) {
	cArgs := commonArgs(r, "Markets | Decred DEX")
	s.sendTemplate(w, "markets", &marketTmplData{
		CommonArguments: *cArgs,
	})
}

type walletsTmplData struct {
	CommonArguments
	Assets []*core.SupportedAsset
}

// handleWallets is the handler for the '/wallets' page request.
func (s *WebServer) handleWallets(w http.ResponseWriter, r *http.Request) {
	assetMap := s.core.SupportedAssets()
	// Sort assets by 1. wallet vs no wallet, and 2) alphabetically.
	assets := make([]*core.SupportedAsset, 0, len(assetMap))
	// over-allocating, but assuming user will not have set up most wallets.
	nowallets := make([]*core.SupportedAsset, 0, len(assetMap))
	for _, asset := range assetMap {
		if asset.Wallet == nil {
			nowallets = append(nowallets, asset)
		} else {
			assets = append(assets, asset)
		}
	}
	sort.Slice(assets, func(i, j int) bool {
		return assets[i].Info.Name < assets[j].Info.Name
	})
	sort.Slice(nowallets, func(i, j int) bool {
		return nowallets[i].Info.Name < nowallets[j].Info.Name
	})
	data := &walletsTmplData{
		CommonArguments: *commonArgs(r, "Wallets | Decred DEX"),
		Assets:          append(assets, nowallets...),
	}
	s.sendTemplate(w, "wallets", data)
}

// handleWalletLogFile is the handler for the '/wallets/logfile' page request.
func (s *WebServer) handleWalletLogFile(w http.ResponseWriter, r *http.Request) {
	err := r.ParseForm()
	if err != nil {
		log.Errorf("error parsing form for wallet log file: %v", err)
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	assetIDQueryString := r.Form["assetid"]
	if len(assetIDQueryString) != 1 || len(assetIDQueryString[0]) == 0 {
		log.Error("could not find asset id in query string")
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	assetID, err := strconv.ParseUint(assetIDQueryString[0], 10, 32)
	if err != nil {
		log.Errorf("failed to parse asset id query string %v", err)
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	logFilePath, err := s.core.WalletLogFilePath(uint32(assetID))
	if err != nil {
		log.Errorf("failed to get log file path %v", err)
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	logFile, err := os.Open(logFilePath)
	if err != nil {
		log.Errorf("error opening log file: %v", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	defer logFile.Close()

	assetName := dex.BipIDSymbol(uint32(assetID))
	logFileName := fmt.Sprintf("dcrdex-%s-wallet.log", assetName)
	w.Header().Set("Content-Disposition", "attachment; filename="+logFileName)
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)

	_, err = io.Copy(w, logFile)
	if err != nil {
		log.Errorf("error copying log file: %v", err)
	}
}

// handleGenerateQRCode is the handler for the '/generateqrcode' page request
func (s *WebServer) handleGenerateQRCode(w http.ResponseWriter, r *http.Request) {
	err := r.ParseForm()
	if err != nil {
		log.Errorf("error parsing form for generate qr code: %v", err)
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	address := r.Form["address"]
	if len(address) != 1 || len(address[0]) == 0 {
		log.Error("form for generating qr code does not contain address")
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	png, err := qrcode.Encode(address[0], qrcode.Medium, 200)
	if err != nil {
		log.Error("error generating qr code: %v", err)
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Content-Length", strconv.Itoa(len(png)))
	w.WriteHeader(http.StatusOK)

	_, err = w.Write(png)
	if err != nil {
		log.Errorf("error writing qr code image: %v", err)
	}
}

// handleSettings is the handler for the '/settings' page request.
func (s *WebServer) handleSettings(w http.ResponseWriter, r *http.Request) {
	common := commonArgs(r, "Settings | Decred DEX")
	data := &struct {
		CommonArguments
		KnownExchanges  []string
		FiatRateSources map[string]bool
		FiatCurrency    string
	}{
		CommonArguments: *common,
		KnownExchanges:  s.knownUnregisteredExchanges(common.UserInfo.Exchanges),
		FiatCurrency:    core.DefaultFiatCurrency,
		FiatRateSources: s.core.FiatRateSources(),
	}
	s.sendTemplate(w, "settings", data)
}

// handleDexSettings is the handler for the '/dexsettings' page request.
func (s *WebServer) handleDexSettings(w http.ResponseWriter, r *http.Request) {
	host, err := getHostCtx(r)
	if err != nil {
		log.Errorf("error getting host ctx: %v", err)
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	exchange, err := s.core.Exchange(host)
	if err != nil {
		log.Errorf("error getting exchange: %v", err)
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	common := *commonArgs(r, fmt.Sprintf("%v Settings | Decred DEX", host))
	data := &struct {
		CommonArguments
		Exchange       *core.Exchange
		KnownExchanges []string
	}{
		CommonArguments: common,
		Exchange:        exchange,
		KnownExchanges:  s.knownUnregisteredExchanges(common.UserInfo.Exchanges),
	}

	s.sendTemplate(w, "dexsettings", data)
}

type ordersTmplData struct {
	CommonArguments
	Assets   map[uint32]*core.SupportedAsset
	Hosts    []string
	Statuses map[uint8]string
}

var allStatuses = map[uint8]string{
	uint8(order.OrderStatusEpoch):    order.OrderStatusEpoch.String(),
	uint8(order.OrderStatusBooked):   order.OrderStatusBooked.String(),
	uint8(order.OrderStatusExecuted): order.OrderStatusExecuted.String(),
	uint8(order.OrderStatusCanceled): order.OrderStatusCanceled.String(),
	uint8(order.OrderStatusRevoked):  order.OrderStatusRevoked.String(),
}

// handleOrders is the handler for the /orders page request.
func (s *WebServer) handleOrders(w http.ResponseWriter, r *http.Request) {
	user := extractUserInfo(r).User
	hosts := make([]string, 0, len(user.Exchanges))
	for _, xc := range user.Exchanges {
		hosts = append(hosts, xc.Host)
	}

	s.sendTemplate(w, "orders", &ordersTmplData{
		CommonArguments: *commonArgs(r, "Orders | Decred DEX"),
		Assets:          user.Assets,
		Hosts:           hosts,
		Statuses:        allStatuses,
	})
}

// handleExportOrders is the handler for the /orders/export page request.
func (s *WebServer) handleExportOrders(w http.ResponseWriter, r *http.Request) {
	filter := new(core.OrderFilter)
	err := r.ParseForm()
	if err != nil {
		log.Errorf("error parsing form for export order: %v", err)
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	filter.Hosts = r.Form["hosts"]
	assets := r.Form["assets"]
	filter.Assets = make([]uint32, len(assets))
	for k, assetStrID := range assets {
		assetNumID, err := strconv.ParseUint(assetStrID, 10, 32)
		if err != nil {
			log.Errorf("error parsing asset id: %v", err)
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		filter.Assets[k] = uint32(assetNumID)
	}
	statuses := r.Form["statuses"]
	filter.Statuses = make([]order.OrderStatus, len(statuses))
	for k, statusStrID := range statuses {
		statusNumID, err := strconv.ParseUint(statusStrID, 10, 16)
		if err != nil {
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			log.Errorf("error parsing status id: %v", err)
			return
		}
		filter.Statuses[k] = order.OrderStatus(statusNumID)
	}

	ords, err := s.core.Orders(filter)
	if err != nil {
		log.Errorf("error retrieving order: %v", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Disposition", "attachment; filename=orders.csv")
	w.Header().Set("Content-Type", "text/csv")
	w.WriteHeader(http.StatusOK)
	csvWriter := csv.NewWriter(w)
	csvWriter.UseCRLF = strings.Contains(r.UserAgent(), "Windows")

	err = csvWriter.Write([]string{
		"Host",
		"Base",
		"Quote",
		"Base Quantity",
		"Order Rate",
		"Actual Rate",
		"Base Fees",
		"Quote Fees",
		"Type",
		"Side",
		"Time in Force",
		"Status",
		"Filled (%)",
		"Settled (%)",
		"Time",
	})
	if err != nil {
		log.Errorf("error writing CSV: %v", err)
		return
	}
	csvWriter.Flush()
	err = csvWriter.Error()
	if err != nil {
		log.Errorf("error writing CSV: %v", err)
		return
	}

	for _, ord := range ords {
		ordReader := s.orderReader(ord)

		timestamp := time.UnixMilli(int64(ord.Stamp)).Local().Format(time.RFC3339Nano)
		err = csvWriter.Write([]string{
			ord.Host,                      // Host
			ord.BaseSymbol,                // Base
			ord.QuoteSymbol,               // Quote
			ordReader.BaseQtyString(),     // Base Quantity
			ordReader.SimpleRateString(),  // Order Rate
			ordReader.AverageRateString(), // Actual Rate
			ordReader.BaseAssetFees(),     // Base Fees
			ordReader.QuoteAssetFees(),    // Quote Fees
			ordReader.Type.String(),       // Type
			ordReader.SideString(),        // Side
			ord.TimeInForce.String(),      // Time in Force
			ordReader.StatusString(),      // Status
			ordReader.FilledPercent(),     // Filled
			ordReader.SettledPercent(),    // Settled
			timestamp,                     // Time
		})
		if err != nil {
			log.Errorf("error writing CSV: %v", err)
			return
		}
		csvWriter.Flush()
		err = csvWriter.Error()
		if err != nil {
			log.Errorf("error writing CSV: %v", err)
			return
		}
	}
}

type orderTmplData struct {
	CommonArguments
	Order *core.OrderReader
	// Don't use dex.Network because the template parser will use the Stringer.
	Net uint8
}

// handleOrder is the handler for the /order/{oid} page request.
func (s *WebServer) handleOrder(w http.ResponseWriter, r *http.Request) {
	oid, err := getOrderIDCtx(r)
	if err != nil {
		log.Errorf("error retrieving order ID from request context: %v", err)
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	ord, err := s.core.Order(oid)
	if err != nil {
		log.Errorf("error retrieving order: %v", err)
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	s.sendTemplate(w, "order", &orderTmplData{
		CommonArguments: *commonArgs(r, "Order | Decred DEX"),
		Order:           s.orderReader(ord),
		Net:             uint8(s.core.Network()),
	})
}

func defaultUnitInfo(symbol string) dex.UnitInfo {
	return dex.UnitInfo{
		AtomicUnit: "atoms",
		Conventional: dex.Denomination{
			ConversionFactor: 1e8,
			Unit:             symbol,
		},
	}
}

func (s *WebServer) orderReader(ord *core.Order) *core.OrderReader {
	unitInfo := func(assetID uint32, symbol string) dex.UnitInfo {
		assetInfo, err := asset.Info(assetID)
		if err != nil {
			xc := s.core.Exchanges()[ord.Host]
			asset, found := xc.Assets[assetID]
			if !found || asset.UnitInfo.Conventional.ConversionFactor == 0 {
				return defaultUnitInfo(symbol)
			}
		}
		return assetInfo.UnitInfo
	}

	return &core.OrderReader{
		Order:         ord,
		BaseUnitInfo:  unitInfo(ord.BaseID, ord.BaseSymbol),
		QuoteUnitInfo: unitInfo(ord.QuoteID, ord.QuoteSymbol),
	}
}
