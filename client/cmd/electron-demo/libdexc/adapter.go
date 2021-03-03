// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"

	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/client/webserver"
	"decred.org/dcrdex/dex"
	"github.com/decred/slog"

	_ "decred.org/dcrdex/client/asset/btc" // register btc asset
	_ "decred.org/dcrdex/client/asset/dcr" // register dcr asset
	_ "decred.org/dcrdex/client/asset/ltc" // register ltc asset
)

type callHandler func(json.RawMessage) (string, error)

func reply(thing interface{}) (string, error) {
	b, err := json.Marshal(thing)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func replyWithErrorCheck(thing interface{}, err error) (string, error) {
	if err != nil {
		return "", err
	}
	return reply(thing)
}

// CoreAdapter manages a Core instance and possibly a Server instance for
// a library user.
type CoreAdapter struct {
	ctx           context.Context
	kill          context.CancelFunc
	inited        uint32
	logLevel      slog.Level
	serverRunning uint32
	core          *core.Core
	webServer     *dex.ConnectionMaster

	preInitMethods map[string]callHandler
	directMethods  map[string]callHandler
}

func NewCoreAdapter() *CoreAdapter {
	c := &CoreAdapter{}

	c.preInitMethods = map[string]callHandler{
		"IsInitialized": c.isInitialized,
	}

	c.directMethods = map[string]callHandler{
		// Some control functions.
		"startServer": c.startServer,
		"shutdown":    c.shutdown,
		// Pass-throughs to Core
		"Init":         c.init,
		"CreateWallet": c.createWallet,
		"User":         c.user,
		"Register":     c.register,
	}

	return c
}

func (c *CoreAdapter) startCore(raw json.RawMessage) error {
	if !atomic.CompareAndSwapUint32(&c.inited, 0, 1) {
		return fmt.Errorf("already initialized")
	}

	form := new(struct {
		DBPath   string      `json:"dbPath"`
		Net      dex.Network `json:"net"`
		LogLevel slog.Level  `json:"logLevel"`
	})
	if err := json.Unmarshal(raw, form); err != nil {
		return err
	}

	err := os.MkdirAll(filepath.Dir(form.DBPath), 0777)
	if err != nil {
		return err
	}

	c.ctx, c.kill = context.WithCancel(context.Background())
	ccore, err := core.New(&core.Config{
		DBPath: form.DBPath,
		Net:    form.Net,
		Logger: dex.StdOutLogger("CORE", form.LogLevel),
	})
	if err != nil {
		return fmt.Errorf("error creating client core: %v", err)
	}
	c.core = ccore
	c.logLevel = form.LogLevel

	go ccore.Run(c.ctx)
	<-ccore.Ready()

	return nil
}

func (c *CoreAdapter) startServer(raw json.RawMessage) (string, error) {
	if !atomic.CompareAndSwapUint32(&c.serverRunning, 0, 1) {
		return "", fmt.Errorf("already initialized")
	}
	var webAddr string
	if err := json.Unmarshal(raw, &webAddr); err != nil {
		return "", err
	}

	webSrv, err := webserver.New(c.core, webAddr, dex.StdOutLogger("SRVR", c.logLevel), false)
	if err != nil {
		return "", fmt.Errorf("Error creating web server: %v", err)
	}
	cm := dex.NewConnectionMaster(webSrv)
	err = cm.Connect(c.ctx)
	if err != nil {
		return "", fmt.Errorf("Error starting web server: %v", err)
	}
	c.webServer = cm
	go func() {
		defer atomic.StoreUint32(&c.serverRunning, 0)
		cm.Wait()
	}()
	return "", nil
}

func (c *CoreAdapter) shutdown(json.RawMessage) (string, error) {
	if atomic.LoadUint32(&c.inited) == 0 || c.kill == nil {
		return "", fmt.Errorf("already shut down")
	}
	c.kill()
	return "", nil
}

func (c *CoreAdapter) handlers(funcName string) (callHandler, callHandler) {
	return c.preInitMethods[funcName], c.directMethods[funcName]
}

func (c *CoreAdapter) run(callData *CallData) (string, error) {
	switch preInitHandler, coreHandler := c.handlers(callData.Function); {
	case callData.Function == "startCore":
		return "", c.startCore(callData.Params)
	case preInitHandler != nil:
		return preInitHandler(callData.Params)
	case atomic.LoadUint32(&c.inited) == 0:
		return "", fmt.Errorf("not initialized")
	case c.core == nil:
		return "", fmt.Errorf("core not constructed. probably an initialization error")
	case coreHandler != nil:
		return coreHandler(callData.Params)
	}
	return "", fmt.Errorf("no method %q", callData.Function)
}

func (c *CoreAdapter) init(raw json.RawMessage) (string, error) {
	form := new(struct {
		Pass string `json:"pass"`
	})
	if err := json.Unmarshal(raw, form); err != nil {
		return "", err
	}

	return "", c.core.InitializeClient([]byte(form.Pass))
}

func (c *CoreAdapter) isInitialized(json.RawMessage) (string, error) {
	return replyWithErrorCheck(c.core.IsInitialized())
}

func (c *CoreAdapter) createWallet(raw json.RawMessage) (string, error) {
	form := new(struct {
		AssetID uint32            `json:"assetID"`
		Config  map[string]string `json:"config"`
		Pass    string            `json:"pass"`
		AppPW   string            `json:"appPass"`
	})
	if err := json.Unmarshal(raw, form); err != nil {
		return "", err
	}
	return "", c.core.CreateWallet([]byte(form.AppPW), []byte(form.Pass), &core.WalletForm{
		AssetID: form.AssetID,
		Config:  form.Config,
	})
}

func (c *CoreAdapter) user(raw json.RawMessage) (string, error) {
	return reply(c.core.User())
}

func (c *CoreAdapter) getFee(raw json.RawMessage) (string, error) {
	form := new(struct {
		Addr string `json:"addr"`
		Cert string `json:"cert"` // Not required if there is an entry for the server in the dcrdex/client/core/certs.go
	})
	if err := json.Unmarshal(raw, form); err != nil {
		return "", err
	}
	return replyWithErrorCheck(c.core.GetFee(form.Addr, []byte(form.Cert)))
}

func (c *CoreAdapter) register(raw json.RawMessage) (string, error) {
	form := new(core.RegisterForm)
	if err := json.Unmarshal(raw, form); err != nil {
		return "", err
	}

	return replyWithErrorCheck(c.core.Register(form))
}
