// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package main

import (
	"reflect"
	"testing"

	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/client/rpcserver"
	"decred.org/dcrdex/dex/encode"
)

func TestBuildPayloadNoParams(t *testing.T) {
	for _, route := range []string{"version", "wallets", "logout", "exchanges", "mmstatus"} {
		p, err := buildPayload(route, nil, nil)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", route, err)
		}
		if p != nil {
			t.Fatalf("%s: expected nil payload", route)
		}
	}
}

func TestBuildPayloadUnknownRoute(t *testing.T) {
	_, err := buildPayload("nonexistent", nil, nil)
	if err == nil {
		t.Fatal("expected error for unknown route")
	}
}

func TestSimpleRoutes(t *testing.T) {
	// Routes with only non-password, non-custom fields.
	tests := []struct {
		route     string
		args      []string
		wantErr   bool
		checkFunc func(t *testing.T, p any)
	}{
		{
			route: "closewallet",
			args:  []string{"42"},
			checkFunc: func(t *testing.T, p any) {
				cp := p.(*rpcserver.CloseWalletParams)
				if cp.AssetID != 42 {
					t.Fatalf("expected assetID 42, got %d", cp.AssetID)
				}
			},
		},
		{
			route:   "closewallet",
			args:    nil,
			wantErr: true,
		},
		{
			route:   "closewallet",
			args:    []string{"abc"},
			wantErr: true,
		},
		{
			route: "togglewalletstatus",
			args:  []string{"42", "true"},
			checkFunc: func(t *testing.T, p any) {
				tp := p.(*rpcserver.ToggleWalletStatusParams)
				if tp.AssetID != 42 || !tp.Disable {
					t.Fatalf("expected 42/true, got %d/%v", tp.AssetID, tp.Disable)
				}
			},
		},
		{
			route:   "togglewalletstatus",
			args:    []string{"42"},
			wantErr: true,
		},
		{
			route: "orderbook",
			args:  []string{"localhost:7232", "42", "0"},
			checkFunc: func(t *testing.T, p any) {
				op := p.(*rpcserver.OrderBookParams)
				if op.Host != "localhost:7232" || op.Base != 42 || op.Quote != 0 {
					t.Fatal("wrong orderbook values")
				}
			},
		},
		{
			route: "orderbook",
			args:  []string{"localhost:7232", "42", "0", "10"},
			checkFunc: func(t *testing.T, p any) {
				op := p.(*rpcserver.OrderBookParams)
				if op.NOrders != 10 {
					t.Fatalf("expected nOrders 10, got %d", op.NOrders)
				}
			},
		},
		{
			route: "cancel",
			args:  []string{"abc123"},
			checkFunc: func(t *testing.T, p any) {
				cp := p.(*rpcserver.CancelParams)
				if cp.OrderID != "abc123" {
					t.Fatalf("expected orderID abc123, got %s", cp.OrderID)
				}
			},
		},
		{
			route: "walletbalance",
			args:  []string{"42"},
			checkFunc: func(t *testing.T, p any) {
				bp := p.(*rpcserver.WalletBalanceParams)
				if bp.AssetID != 42 {
					t.Fatalf("expected assetID 42, got %d", bp.AssetID)
				}
			},
		},
		{
			route: "walletstate",
			args:  []string{"42"},
			checkFunc: func(t *testing.T, p any) {
				sp := p.(*rpcserver.WalletStateParams)
				if sp.AssetID != 42 {
					t.Fatalf("expected assetID 42, got %d", sp.AssetID)
				}
			},
		},
	}

	for _, tt := range tests {
		p, err := buildPayload(tt.route, nil, tt.args)
		if (err != nil) != tt.wantErr {
			t.Fatalf("%s %v: err = %v, wantErr = %v", tt.route, tt.args, err, tt.wantErr)
		}
		if err == nil && tt.checkFunc != nil {
			tt.checkFunc(t, p)
		}
	}
}

func TestPasswordHandling(t *testing.T) {
	pw := encode.PassBytes("password")
	pw2 := encode.PassBytes("walletpw")

	// init: 1 password + 1 optional seed.
	p, err := buildPayload("init", []encode.PassBytes{pw}, nil)
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	ip := p.(*rpcserver.InitParams)
	if string(ip.AppPass) != "password" {
		t.Fatal("init: wrong password")
	}
	if ip.Seed != nil {
		t.Fatal("init: seed should be nil")
	}

	// init with seed.
	p, err = buildPayload("init", []encode.PassBytes{pw}, []string{"myseed"})
	if err != nil {
		t.Fatalf("init with seed: %v", err)
	}
	ip = p.(*rpcserver.InitParams)
	if ip.Seed == nil || *ip.Seed != "myseed" {
		t.Fatal("init: expected seed myseed")
	}

	// init missing password.
	_, err = buildPayload("init", nil, nil)
	if err == nil {
		t.Fatal("init: expected error for missing password")
	}

	// login: 1 password, no other args.
	p, err = buildPayload("login", []encode.PassBytes{pw}, nil)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	lp := p.(*rpcserver.LoginParams)
	if string(lp.AppPass) != "password" {
		t.Fatal("login: wrong password")
	}

	// newwallet: 2 passwords + args.
	p, err = buildPayload("newwallet", []encode.PassBytes{pw, pw2}, []string{"42", "rpc"})
	if err != nil {
		t.Fatalf("newwallet: %v", err)
	}
	nw := p.(*rpcserver.NewWalletParams)
	if string(nw.AppPass) != "password" || string(nw.WalletPass) != "walletpw" {
		t.Fatal("newwallet: wrong passwords")
	}
	if nw.AssetID != 42 || nw.WalletType != "rpc" {
		t.Fatal("newwallet: wrong fields")
	}

	// newwallet missing password.
	_, err = buildPayload("newwallet", []encode.PassBytes{pw}, []string{"42", "rpc"})
	if err == nil {
		t.Fatal("newwallet: expected error for missing second password")
	}
}

func TestOptionalFields(t *testing.T) {
	// myorders: all fields optional.
	p, err := buildPayload("myorders", nil, nil)
	if err != nil {
		t.Fatalf("myorders no args: %v", err)
	}
	mo := p.(*rpcserver.MyOrdersParams)
	if mo.Host != "" || mo.Base != nil || mo.Quote != nil {
		t.Fatal("myorders: expected all zero values")
	}

	// myorders with some args.
	p, err = buildPayload("myorders", nil, []string{"localhost", "42"})
	if err != nil {
		t.Fatalf("myorders with args: %v", err)
	}
	mo = p.(*rpcserver.MyOrdersParams)
	if mo.Host != "localhost" {
		t.Fatalf("myorders: expected host localhost, got %s", mo.Host)
	}
	if mo.Base == nil || *mo.Base != 42 {
		t.Fatal("myorders: expected base 42")
	}
	if mo.Quote != nil {
		t.Fatal("myorders: expected quote nil")
	}

	// txhistory: 1 required + 3 optional.
	p, err = buildPayload("txhistory", nil, []string{"42"})
	if err != nil {
		t.Fatalf("txhistory minimal: %v", err)
	}
	th := p.(*rpcserver.TxHistoryParams)
	if th.AssetID != 42 || th.N != 0 || th.RefID != nil || th.Past {
		t.Fatal("txhistory: wrong defaults")
	}

	p, err = buildPayload("txhistory", nil, []string{"42", "10", "refxyz", "true"})
	if err != nil {
		t.Fatalf("txhistory full: %v", err)
	}
	th = p.(*rpcserver.TxHistoryParams)
	if th.AssetID != 42 || th.N != 10 || th.RefID == nil || *th.RefID != "refxyz" || !th.Past {
		t.Fatal("txhistory: wrong values")
	}
}

func TestSendAndWithdraw(t *testing.T) {
	pw := encode.PassBytes("password")

	// send
	p, err := buildPayload("send", []encode.PassBytes{pw}, []string{"42", "1000000", "DsAddr"})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	sp := p.(*rpcserver.SendParams)
	if sp.AssetID != 42 || sp.Value != 1000000 || sp.Address != "DsAddr" || sp.Subtract {
		t.Fatal("send: wrong values")
	}

	// withdraw sets subtract=true
	p, err = buildPayload("withdraw", []encode.PassBytes{pw}, []string{"42", "1000000", "DsAddr"})
	if err != nil {
		t.Fatalf("withdraw: %v", err)
	}
	sp = p.(*rpcserver.SendParams)
	if !sp.Subtract {
		t.Fatal("withdraw: subtract should be true")
	}
}

func TestTradeRoute(t *testing.T) {
	pw := encode.PassBytes("password")
	goodArgs := []string{"localhost:7232", "true", "true", "42", "0", "1000000", "5000000", "false"}

	p, err := buildPayload("trade", []encode.PassBytes{pw}, goodArgs)
	if err != nil {
		t.Fatalf("trade: %v", err)
	}
	tp := p.(*rpcserver.TradeParams)
	if tp.Host != "localhost:7232" || !tp.IsLimit || !tp.Sell ||
		tp.Base != 42 || tp.Quote != 0 || tp.Qty != 1000000 ||
		tp.Rate != 5000000 || tp.TifNow {
		t.Fatal("trade: wrong values")
	}
	if tp.Options != nil {
		t.Fatal("trade: options should be nil")
	}

	// With options.
	argsWithOpts := append(goodArgs, `{"swapMax":"true"}`)
	p, err = buildPayload("trade", []encode.PassBytes{pw}, argsWithOpts)
	if err != nil {
		t.Fatalf("trade with options: %v", err)
	}
	tp = p.(*rpcserver.TradeParams)
	if tp.Options == nil || tp.Options["swapMax"] != "true" {
		t.Fatal("trade: wrong options")
	}

	// Missing args.
	_, err = buildPayload("trade", []encode.PassBytes{pw}, []string{"host"})
	if err == nil {
		t.Fatal("trade: expected error for too few args")
	}

	// Missing password.
	_, err = buildPayload("trade", nil, goodArgs)
	if err == nil {
		t.Fatal("trade: expected error for missing password")
	}
}

func TestMultiTradeRoute(t *testing.T) {
	pw := encode.PassBytes("password")
	args := []string{"localhost", "true", "42", "0", "100000", "[[1000,5000],[2000,6000]]"}

	p, err := buildPayload("multitrade", []encode.PassBytes{pw}, args)
	if err != nil {
		t.Fatalf("multitrade: %v", err)
	}
	mt := p.(*rpcserver.MultiTradeParams)
	if mt.Host != "localhost" || !mt.Sell || mt.Base != 42 || mt.Quote != 0 || mt.MaxLock != 100000 {
		t.Fatal("multitrade: wrong values")
	}
	if len(mt.Placements) != 2 {
		t.Fatalf("multitrade: expected 2 placements, got %d", len(mt.Placements))
	}
	if mt.Placements[0].Qty != 1000 || mt.Placements[0].Rate != 5000 {
		t.Fatal("multitrade: wrong placement[0]")
	}
}

func TestNewWalletConfig(t *testing.T) {
	pws := []encode.PassBytes{encode.PassBytes("app"), encode.PassBytes("wallet")}

	// With key=value config args.
	p, err := buildPayload("newwallet", pws, []string{"42", "rpc", "user=admin", "pass=secret"})
	if err != nil {
		t.Fatalf("newwallet config: %v", err)
	}
	nw := p.(*rpcserver.NewWalletParams)
	if nw.Config == nil || nw.Config["user"] != "admin" || nw.Config["pass"] != "secret" {
		t.Fatal("newwallet: wrong config")
	}

	// With JSON config arg.
	p, err = buildPayload("newwallet", pws, []string{"42", "rpc", `{"user":"admin","pass":"secret"}`})
	if err != nil {
		t.Fatalf("newwallet JSON config: %v", err)
	}
	nw = p.(*rpcserver.NewWalletParams)
	if nw.Config["user"] != "admin" {
		t.Fatal("newwallet: wrong JSON config")
	}

	// Without config.
	p, err = buildPayload("newwallet", pws, []string{"42", "rpc"})
	if err != nil {
		t.Fatalf("newwallet no config: %v", err)
	}
	nw = p.(*rpcserver.NewWalletParams)
	if nw.Config != nil {
		t.Fatal("newwallet: config should be nil")
	}
}

func TestReconfigureWalletConfig(t *testing.T) {
	pws := []encode.PassBytes{encode.PassBytes("app"), encode.PassBytes("newwallet")}

	// With key=value config args.
	p, err := buildPayload("reconfigurewallet", pws, []string{"60", "rpc", "bundler=http://localhost:40000", "providers=http://node1"})
	if err != nil {
		t.Fatalf("reconfigurewallet config: %v", err)
	}
	rw := p.(*rpcserver.ReconfigureWalletParams)
	if string(rw.AppPass) != "app" || string(rw.NewWalletPW) != "newwallet" {
		t.Fatal("reconfigurewallet: wrong passwords")
	}
	if rw.AssetID != 60 || rw.WalletType != "rpc" {
		t.Fatal("reconfigurewallet: wrong fields")
	}
	if rw.Config == nil || rw.Config["bundler"] != "http://localhost:40000" || rw.Config["providers"] != "http://node1" {
		t.Fatal("reconfigurewallet: wrong config")
	}

	// Without config.
	p, err = buildPayload("reconfigurewallet", pws, []string{"60", "rpc"})
	if err != nil {
		t.Fatalf("reconfigurewallet no config: %v", err)
	}
	rw = p.(*rpcserver.ReconfigureWalletParams)
	if rw.Config != nil {
		t.Fatal("reconfigurewallet: config should be nil")
	}

	// Without new wallet password (only app pass).
	_, err = buildPayload("reconfigurewallet", []encode.PassBytes{encode.PassBytes("app")}, []string{"60", "rpc"})
	if err == nil {
		t.Fatal("reconfigurewallet: expected error for missing second password")
	}
}

func TestStartStopBot(t *testing.T) {
	pw := encode.PassBytes("password")

	// startmmbot with market filter.
	p, err := buildPayload("startmmbot", []encode.PassBytes{pw}, []string{"config.json", "localhost", "42", "0"})
	if err != nil {
		t.Fatalf("startmmbot: %v", err)
	}
	sb := p.(*rpcserver.StartBotParams)
	if sb.CfgFilePath != "config.json" {
		t.Fatal("startmmbot: wrong cfgFilePath")
	}
	if sb.Market == nil || sb.Market.Host != "localhost" || sb.Market.BaseID != 42 || sb.Market.QuoteID != 0 {
		t.Fatal("startmmbot: wrong market")
	}

	// startmmbot without market.
	p, err = buildPayload("startmmbot", []encode.PassBytes{pw}, []string{"config.json"})
	if err != nil {
		t.Fatalf("startmmbot no market: %v", err)
	}
	sb = p.(*rpcserver.StartBotParams)
	if sb.Market != nil {
		t.Fatal("startmmbot: market should be nil")
	}

	// startmmbot with incomplete market.
	_, err = buildPayload("startmmbot", []encode.PassBytes{pw}, []string{"config.json", "host", "42"})
	if err == nil {
		t.Fatal("startmmbot: expected error for incomplete market filter")
	}

	// stopmmbot with no args → stop all.
	p, err = buildPayload("stopmmbot", nil, nil)
	if err != nil {
		t.Fatalf("stopmmbot: %v", err)
	}
	stop := p.(*rpcserver.StopBotParams)
	if stop.Market != nil {
		t.Fatal("stopmmbot: market should be nil")
	}

	// stopmmbot with market.
	p, err = buildPayload("stopmmbot", nil, []string{"localhost", "42", "0"})
	if err != nil {
		t.Fatalf("stopmmbot with market: %v", err)
	}
	stop = p.(*rpcserver.StopBotParams)
	if stop.Market == nil || stop.Market.Host != "localhost" {
		t.Fatal("stopmmbot: wrong market")
	}
}

func TestPostBondMaintainTier(t *testing.T) {
	pw := encode.PassBytes("password")

	// postbond with default maintainTier (true).
	p, err := buildPayload("postbond", []encode.PassBytes{pw}, []string{"localhost", "1000000", "42"})
	if err != nil {
		t.Fatalf("postbond default: %v", err)
	}
	pb := p.(*core.PostBondForm)
	if pb.MaintainTier == nil || !*pb.MaintainTier {
		t.Fatal("postbond: maintainTier should default to true")
	}

	// postbond with explicit false.
	p, err = buildPayload("postbond", []encode.PassBytes{pw}, []string{"localhost", "1000000", "42", "0", "false"})
	if err != nil {
		t.Fatalf("postbond false: %v", err)
	}
	pb = p.(*core.PostBondForm)
	if pb.MaintainTier == nil || *pb.MaintainTier {
		t.Fatal("postbond: maintainTier should be false")
	}
}

func TestEmbeddedStructFlattening(t *testing.T) {
	// mmavailablebalances has embedded mm.MarketWithHost.
	p, err := buildPayload("mmavailablebalances", nil, []string{"localhost", "42", "0"})
	if err != nil {
		t.Fatalf("mmavailablebalances: %v", err)
	}
	mb := p.(*rpcserver.MMAvailableBalancesParams)
	if mb.Host != "localhost" || mb.BaseID != 42 || mb.QuoteID != 0 {
		t.Fatal("mmavailablebalances: wrong embedded fields")
	}
}

func TestUpdateRunningBotCfg(t *testing.T) {
	// Without inventory.
	p, err := buildPayload("updaterunningbotcfg", nil, []string{"config.json", "localhost", "42", "0"})
	if err != nil {
		t.Fatalf("updaterunningbotcfg: %v", err)
	}
	ub := p.(*rpcserver.UpdateRunningBotParams)
	if ub.CfgFilePath != "config.json" || ub.Market.Host != "localhost" || ub.Market.BaseID != 42 {
		t.Fatal("updaterunningbotcfg: wrong values")
	}
	if ub.Balances != nil {
		t.Fatal("updaterunningbotcfg: balances should be nil")
	}

	// With inventory.
	p, err = buildPayload("updaterunningbotcfg", nil,
		[]string{"config.json", "localhost", "42", "0", "[[42,1000]]", "[[0,500]]"})
	if err != nil {
		t.Fatalf("updaterunningbotcfg with inv: %v", err)
	}
	ub = p.(*rpcserver.UpdateRunningBotParams)
	if ub.Balances == nil {
		t.Fatal("updaterunningbotcfg: balances should not be nil")
	}
	if ub.Balances.DEX[42] != 1000 || ub.Balances.CEX[0] != 500 {
		t.Fatal("updaterunningbotcfg: wrong inventory")
	}
}

func TestUpdateRunningBotInv(t *testing.T) {
	p, err := buildPayload("updaterunningbotinv", nil,
		[]string{"localhost", "42", "0", "[[42,1000]]", "[[0,500]]"})
	if err != nil {
		t.Fatalf("updaterunningbotinv: %v", err)
	}
	ub := p.(*rpcserver.UpdateRunningBotInventoryParams)
	if ub.Market.Host != "localhost" || ub.Market.BaseID != 42 {
		t.Fatal("updaterunningbotinv: wrong market")
	}
	if ub.Balances == nil || ub.Balances.DEX[42] != 1000 {
		t.Fatal("updaterunningbotinv: wrong balances")
	}

	// Missing args.
	_, err = buildPayload("updaterunningbotinv", nil, []string{"localhost", "42", "0"})
	if err == nil {
		t.Fatal("updaterunningbotinv: expected error for missing balances")
	}
}

func TestHelpRoute(t *testing.T) {
	// No args.
	p, err := buildPayload("help", nil, nil)
	if err != nil {
		t.Fatalf("help no args: %v", err)
	}
	hp := p.(*rpcserver.HelpParams)
	if hp.HelpWith != "" {
		t.Fatal("help: expected empty helpWith")
	}

	// With args.
	p, err = buildPayload("help", nil, []string{"version", "true"})
	if err != nil {
		t.Fatalf("help with args: %v", err)
	}
	hp = p.(*rpcserver.HelpParams)
	if hp.HelpWith != "version" || !hp.IncludePasswords {
		t.Fatal("help: wrong values")
	}
}

func TestSetVotingPreferences(t *testing.T) {
	// With all maps.
	p, err := buildPayload("setvotingprefs", nil, []string{
		"42",
		`{"agenda1":"choice1"}`,
		`{"tspend1":"yes"}`,
		`{"key1":"yes"}`,
	})
	if err != nil {
		t.Fatalf("setvotingprefs: %v", err)
	}
	sv := p.(*rpcserver.SetVotingPreferencesParams)
	if sv.AssetID != 42 {
		t.Fatal("setvotingprefs: wrong assetID")
	}
	if sv.Choices["agenda1"] != "choice1" {
		t.Fatal("setvotingprefs: wrong choices")
	}
	if sv.TSpendPolicy["tspend1"] != "yes" {
		t.Fatal("setvotingprefs: wrong tSpendPolicy")
	}
	if sv.TreasuryPolicy["key1"] != "yes" {
		t.Fatal("setvotingprefs: wrong treasuryPolicy")
	}

	// Without optional maps.
	p, err = buildPayload("setvotingprefs", nil, []string{"42"})
	if err != nil {
		t.Fatalf("setvotingprefs minimal: %v", err)
	}
	sv = p.(*rpcserver.SetVotingPreferencesParams)
	if sv.Choices != nil || sv.TSpendPolicy != nil || sv.TreasuryPolicy != nil {
		t.Fatal("setvotingprefs: maps should be nil")
	}
}

func TestBondOpts(t *testing.T) {
	p, err := buildPayload("bondopts", nil, []string{"localhost", "5"})
	if err != nil {
		t.Fatalf("bondopts: %v", err)
	}
	bo := p.(*core.BondOptionsForm)
	if bo.Host != "localhost" {
		t.Fatal("bondopts: wrong host")
	}
	if bo.TargetTier == nil || *bo.TargetTier != 5 {
		t.Fatal("bondopts: wrong targetTier")
	}
}

func TestPasswordPrompts(t *testing.T) {
	// init uses custom prompt text.
	prompts := passwordPrompts("init")
	if len(prompts) != 1 || prompts[0] != "Set new app password:" {
		t.Fatalf("init prompts = %v, want [Set new app password:]", prompts)
	}

	// login derives from field name.
	prompts = passwordPrompts("login")
	if len(prompts) != 1 || prompts[0] != "App password:" {
		t.Fatalf("login prompts = %v, want [App password:]", prompts)
	}

	// newwallet has 2 passwords.
	prompts = passwordPrompts("newwallet")
	if len(prompts) != 2 {
		t.Fatalf("newwallet prompts: expected 2, got %d", len(prompts))
	}
	if prompts[0] != "App password:" || prompts[1] != "Wallet password:" {
		t.Fatalf("newwallet prompts = %v", prompts)
	}

	// version has no passwords.
	prompts = passwordPrompts("version")
	if prompts != nil {
		t.Fatalf("version prompts = %v, want nil", prompts)
	}
}

func TestCertArgIndex(t *testing.T) {
	tests := []struct {
		route     string
		fieldName string
		wantIdx   int
	}{
		{"discoveracct", "cert", 1},
		{"bondassets", "cert", 1},
		{"getdexconfig", "cert", 1},
		{"postbond", "cert", 5},
	}
	for _, tt := range tests {
		idx := certArgIndex(tt.route, tt.fieldName)
		if idx != tt.wantIdx {
			t.Fatalf("certArgIndex(%s, %s) = %d, want %d", tt.route, tt.fieldName, idx, tt.wantIdx)
		}
	}
}

func TestFieldByJSONNameEmbedded(t *testing.T) {
	// Test that fieldByJSONName navigates embedded structs.
	pt := rpcserver.ParamType("mmavailablebalances")
	if pt == nil {
		t.Fatal("expected non-nil param type for mmavailablebalances")
	}
	v := reflect.New(pt).Elem()
	fv := fieldByJSONName(v, "host")
	if !fv.IsValid() {
		t.Fatal("expected valid field for 'host' in embedded struct")
	}
	fv.SetString("test-host")
	// Verify the value was set in the embedded struct.
	hostField := fieldByJSONName(v, "host")
	if hostField.String() != "test-host" {
		t.Fatalf("expected test-host, got %s", hostField.String())
	}
}

func TestSetFieldFromString(t *testing.T) {
	type testStruct struct {
		S   string            `json:"s"`
		B   bool              `json:"b"`
		U32 uint32            `json:"u32"`
		U64 uint64            `json:"u64"`
		I   int               `json:"i"`
		I64 int64             `json:"i64"`
		PS  *string           `json:"ps"`
		PU  *uint32           `json:"pu"`
		M   map[string]string `json:"m"`
		A   any               `json:"a"`
	}

	v := reflect.New(reflect.TypeOf(testStruct{})).Elem()
	t.Run("string", func(t *testing.T) {
		if err := setFieldFromString(v.Field(0), reflect.TypeOf(""), "hello"); err != nil {
			t.Fatal(err)
		}
		if v.Field(0).String() != "hello" {
			t.Fatal("wrong string value")
		}
	})
	t.Run("bool", func(t *testing.T) {
		if err := setFieldFromString(v.Field(1), reflect.TypeOf(false), "true"); err != nil {
			t.Fatal(err)
		}
		if !v.Field(1).Bool() {
			t.Fatal("expected true")
		}
	})
	t.Run("uint32", func(t *testing.T) {
		if err := setFieldFromString(v.Field(2), reflect.TypeOf(uint32(0)), "42"); err != nil {
			t.Fatal(err)
		}
		if v.Field(2).Uint() != 42 {
			t.Fatal("wrong uint32")
		}
	})
	t.Run("uint64", func(t *testing.T) {
		if err := setFieldFromString(v.Field(3), reflect.TypeOf(uint64(0)), "1000000"); err != nil {
			t.Fatal(err)
		}
		if v.Field(3).Uint() != 1000000 {
			t.Fatal("wrong uint64")
		}
	})
	t.Run("int", func(t *testing.T) {
		if err := setFieldFromString(v.Field(4), reflect.TypeOf(0), "99"); err != nil {
			t.Fatal(err)
		}
		if v.Field(4).Int() != 99 {
			t.Fatal("wrong int")
		}
	})
	t.Run("int64", func(t *testing.T) {
		if err := setFieldFromString(v.Field(5), reflect.TypeOf(int64(0)), "-100"); err != nil {
			t.Fatal(err)
		}
		if v.Field(5).Int() != -100 {
			t.Fatal("wrong int64")
		}
	})
	t.Run("*string", func(t *testing.T) {
		if err := setFieldFromString(v.Field(6), reflect.TypeOf((*string)(nil)), "hello"); err != nil {
			t.Fatal(err)
		}
		if v.Field(6).IsNil() || v.Field(6).Elem().String() != "hello" {
			t.Fatal("wrong *string")
		}
	})
	t.Run("*uint32", func(t *testing.T) {
		if err := setFieldFromString(v.Field(7), reflect.TypeOf((*uint32)(nil)), "7"); err != nil {
			t.Fatal(err)
		}
		if v.Field(7).IsNil() || uint32(v.Field(7).Elem().Uint()) != 7 {
			t.Fatal("wrong *uint32")
		}
	})
	t.Run("map", func(t *testing.T) {
		if err := setFieldFromString(v.Field(8), reflect.TypeOf(map[string]string{}), `{"a":"b"}`); err != nil {
			t.Fatal(err)
		}
		if v.Field(8).Len() != 1 {
			t.Fatal("wrong map length")
		}
	})
	t.Run("any", func(t *testing.T) {
		if err := setFieldFromString(v.Field(9), reflect.TypeOf((*any)(nil)).Elem(), "some_value"); err != nil {
			t.Fatal(err)
		}
		if v.Field(9).Interface() != "some_value" {
			t.Fatal("wrong any value")
		}
	})
	t.Run("bad bool", func(t *testing.T) {
		if err := setFieldFromString(v.Field(1), reflect.TypeOf(false), "notabool"); err == nil {
			t.Fatal("expected error for bad bool")
		}
	})
}

func TestBridgeRoutes(t *testing.T) {
	p, err := buildPayload("bridge", nil, []string{"42", "0", "1000000", "mybridge"})
	if err != nil {
		t.Fatalf("bridge: %v", err)
	}
	bp := p.(*rpcserver.BridgeParams)
	if bp.FromAssetID != 42 || bp.ToAssetID != 0 || bp.Amt != 1000000 || bp.BridgeName != "mybridge" {
		t.Fatal("bridge: wrong values")
	}
}

func TestMultisigRoutes(t *testing.T) {
	p, err := buildPayload("signmultisig", nil, []string{"file.csv", "1"})
	if err != nil {
		t.Fatalf("signmultisig: %v", err)
	}
	sm := p.(*rpcserver.SignMultisigParams)
	if sm.CsvFilePath != "file.csv" || sm.SignIdx != 1 {
		t.Fatal("signmultisig: wrong values")
	}
}

func TestAllRoutesHaveCorrectType(t *testing.T) {
	// Routes with custom field parsers need hand-crafted args that the
	// generic dummy-arg builder below can't synthesize.
	customRoutes := map[string]bool{
		"newwallet":           true, // config uses remaining args
		"reconfigurewallet":   true, // config uses remaining args
		"multitrade":          true, // placements JSON
		"startmmbot":          true, // optional market filter
		"stopmmbot":           true, // optional market filter
		"updaterunningbotcfg": true, // market + optional inventory
		"updaterunningbotinv": true, // market + required inventory
		"postbond":            true, // maintainTier defaults true
		"withdraw":            true, // postProcess sets subtract
		"deploycontract":      true, // required slice field (chains)
		"testcontractgas":     true, // required slice field (chains)
	}

	// Verify that buildPayload returns the correct pointer type for every
	// route that has a paramsType, iterating dynamically over all routes.
	for _, route := range rpcserver.Routes() {
		if customRoutes[route] {
			continue
		}
		pt := rpcserver.ParamType(route)
		if pt == nil {
			continue
		}
		// Provide enough dummy data to not error. Use all zeros/empty.
		fields := rpcserver.ReflectFields(pt)
		var pws []encode.PassBytes
		var args []string
		for _, f := range fields {
			if f.IsPassword {
				pws = append(pws, encode.PassBytes("pw"))
				continue
			}
			switch f.GoType.Kind() {
			case reflect.String:
				args = append(args, "x")
			case reflect.Bool:
				args = append(args, "false")
			case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
				args = append(args, "0")
			case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
				args = append(args, "0")
			case reflect.Ptr:
				args = append(args, "0")
			case reflect.Map, reflect.Slice, reflect.Interface:
				// optional; don't add arg
			default:
				args = append(args, "x")
			}
		}
		p, err := buildPayload(route, pws, args)
		if err != nil {
			t.Fatalf("route %s: unexpected error: %v", route, err)
		}
		if p == nil {
			continue
		}
		gotType := reflect.TypeOf(p)
		wantType := reflect.PointerTo(pt)
		if gotType != wantType {
			t.Fatalf("route %s: got type %v, want %v", route, gotType, wantType)
		}
	}
}

func TestWithdrawSubtractFieldExists(t *testing.T) {
	// Verify that the withdraw postProcess can find the "subtract" field.
	// This guards against silent no-ops if SendParams is renamed or
	// restructured.
	pt := rpcserver.ParamType("withdraw")
	if pt == nil {
		t.Fatal("expected non-nil param type for withdraw")
	}
	v := reflect.New(pt).Elem()
	fv := fieldByJSONName(v, "subtract")
	if !fv.IsValid() {
		t.Fatal("fieldByJSONName failed to find 'subtract' on withdraw params — postProcess would silently do nothing")
	}
	if fv.Kind() != reflect.Bool {
		t.Fatalf("expected bool kind for 'subtract', got %v", fv.Kind())
	}
}

func TestPasswordPromptFromFieldName(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"appPass", "App password:"},
		{"walletPass", "Wallet password:"},
		{"appPassword", "App password:"},
	}
	for _, tt := range tests {
		got := passwordPromptFromFieldName(tt.name)
		if got != tt.want {
			t.Fatalf("passwordPromptFromFieldName(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestRescanWallet(t *testing.T) {
	// Without force.
	p, err := buildPayload("rescanwallet", nil, []string{"42"})
	if err != nil {
		t.Fatalf("rescanwallet: %v", err)
	}
	rw := p.(*rpcserver.RescanWalletParams)
	if rw.AssetID != 42 || rw.Force {
		t.Fatal("rescanwallet: wrong defaults")
	}

	// With force.
	p, err = buildPayload("rescanwallet", nil, []string{"42", "true"})
	if err != nil {
		t.Fatalf("rescanwallet with force: %v", err)
	}
	rw = p.(*rpcserver.RescanWalletParams)
	if !rw.Force {
		t.Fatal("rescanwallet: force should be true")
	}
}

func TestDiscoverAcct(t *testing.T) {
	pw := encode.PassBytes("password")
	p, err := buildPayload("discoveracct", []encode.PassBytes{pw}, []string{"localhost", "certdata"})
	if err != nil {
		t.Fatalf("discoveracct: %v", err)
	}
	da := p.(*rpcserver.DiscoverAcctParams)
	if da.Addr != "localhost" || da.Cert != "certdata" {
		t.Fatal("discoveracct: wrong values")
	}
}

func TestPurchaseTickets(t *testing.T) {
	pw := encode.PassBytes("password")
	p, err := buildPayload("purchasetickets", []encode.PassBytes{pw}, []string{"42", "5"})
	if err != nil {
		t.Fatalf("purchasetickets: %v", err)
	}
	pt := p.(*rpcserver.PurchaseTicketsParams)
	if pt.AssetID != 42 || pt.N != 5 {
		t.Fatal("purchasetickets: wrong values")
	}
}

func TestDeleteArchivedRecords(t *testing.T) {
	// All optional.
	p, err := buildPayload("deletearchivedrecords", nil, nil)
	if err != nil {
		t.Fatalf("deletearchivedrecords: %v", err)
	}
	dr := p.(*rpcserver.DeleteRecordsParams)
	if dr.OlderThanMs != nil || dr.MatchesFile != "" || dr.OrdersFile != "" {
		t.Fatal("deletearchivedrecords: expected zero values")
	}

	// With values.
	p, err = buildPayload("deletearchivedrecords", nil, []string{"1000", "matches.csv", "orders.csv"})
	if err != nil {
		t.Fatalf("deletearchivedrecords with args: %v", err)
	}
	dr = p.(*rpcserver.DeleteRecordsParams)
	if dr.OlderThanMs == nil || *dr.OlderThanMs != 1000 {
		t.Fatal("deletearchivedrecords: wrong olderThanMs")
	}
	if dr.MatchesFile != "matches.csv" || dr.OrdersFile != "orders.csv" {
		t.Fatal("deletearchivedrecords: wrong file paths")
	}
}

func TestWithdrawBchSpv(t *testing.T) {
	pw := encode.PassBytes("password")
	p, err := buildPayload("withdrawbchspv", []encode.PassBytes{pw}, []string{"bch_addr"})
	if err != nil {
		t.Fatalf("withdrawbchspv: %v", err)
	}
	bp := p.(*rpcserver.BchWithdrawParams)
	if bp.Recipient != "bch_addr" {
		t.Fatal("withdrawbchspv: wrong recipient")
	}
}

func TestBridgeFeesAndLimits(t *testing.T) {
	p, err := buildPayload("bridgefeesandlimits", nil, []string{"42", "0", "mybridge"})
	if err != nil {
		t.Fatalf("bridgefeesandlimits: %v", err)
	}
	bf := p.(*rpcserver.BridgeFeesAndLimitsParams)
	if bf.FromAssetID != 42 || bf.ToAssetID != 0 || bf.BridgeName != "mybridge" {
		t.Fatal("bridgefeesandlimits: wrong values")
	}
}

func TestNotifications(t *testing.T) {
	p, err := buildPayload("notifications", nil, []string{"10"})
	if err != nil {
		t.Fatalf("notifications: %v", err)
	}
	np := p.(*rpcserver.NotificationsParams)
	if np.N != 10 {
		t.Fatalf("notifications: expected N=10, got %d", np.N)
	}
}

func TestWalletTx(t *testing.T) {
	p, err := buildPayload("wallettx", nil, []string{"42", "txid123"})
	if err != nil {
		t.Fatalf("wallettx: %v", err)
	}
	wt := p.(*rpcserver.WalletTxParams)
	if wt.AssetID != 42 || wt.TxID != "txid123" {
		t.Fatal("wallettx: wrong values")
	}
}

func TestAbandonTx(t *testing.T) {
	p, err := buildPayload("abandontx", nil, []string{"42", "txid123"})
	if err != nil {
		t.Fatalf("abandontx: %v", err)
	}
	at := p.(*rpcserver.AbandonTxParams)
	if at.AssetID != 42 || at.TxID != "txid123" {
		t.Fatal("abandontx: wrong values")
	}
}

func TestAppSeed(t *testing.T) {
	pw := encode.PassBytes("password")
	p, err := buildPayload("appseed", []encode.PassBytes{pw}, nil)
	if err != nil {
		t.Fatalf("appseed: %v", err)
	}
	as := p.(*rpcserver.AppSeedParams)
	if string(as.AppPass) != "password" {
		t.Fatal("appseed: wrong password")
	}
}

func TestPeerRoutes(t *testing.T) {
	p, err := buildPayload("addwalletpeer", nil, []string{"42", "1.2.3.4:1234"})
	if err != nil {
		t.Fatalf("addwalletpeer: %v", err)
	}
	ap := p.(*rpcserver.AddRemovePeerParams)
	if ap.AssetID != 42 || ap.Address != "1.2.3.4:1234" {
		t.Fatal("addwalletpeer: wrong values")
	}
}

func TestCsvFileRoutes(t *testing.T) {
	for _, route := range []string{"sendfundstomultisig", "refundpaymentmultisig", "viewpaymentmultisig", "sendpaymentmultisig"} {
		p, err := buildPayload(route, nil, []string{"file.csv"})
		if err != nil {
			t.Fatalf("%s: %v", route, err)
		}
		cp := p.(*rpcserver.CsvFileParams)
		if cp.CsvFilePath != "file.csv" {
			t.Fatalf("%s: wrong csvFilePath", route)
		}
	}
}

func TestMMAvailableBalancesFullArgs(t *testing.T) {
	p, err := buildPayload("mmavailablebalances", nil, []string{"localhost", "42", "0", "100", "200", "binance"})
	if err != nil {
		t.Fatalf("mmavailablebalances full: %v", err)
	}
	mb := p.(*rpcserver.MMAvailableBalancesParams)
	if mb.Host != "localhost" || mb.BaseID != 42 || mb.QuoteID != 0 {
		t.Fatal("mmavailablebalances: wrong market fields")
	}
	if mb.CexBaseID != 100 || mb.CexQuoteID != 200 {
		t.Fatal("mmavailablebalances: wrong cex IDs")
	}
	if mb.CexName == nil || *mb.CexName != "binance" {
		t.Fatal("mmavailablebalances: wrong cexName")
	}
}
