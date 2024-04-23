package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"

	"decred.org/dcrdex/client/app"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/server/comms"
	"github.com/decred/dcrd/dcrutil/v4"
	"github.com/go-chi/chi/v5"
)

const (
	configFilenameMainnet = "dexadm.conf"
	configFilenameTestnet = "dexadm_testnet.conf"
	configFilenameHarness = "dexadm_harness.conf"
	defaultPort           = "31043"
)

var (
	defaultApplicationDirectory = dcrutil.AppDataDir("dexadm", false)
)

func main() {
	if err := mainErr(); err != nil {
		fmt.Fprint(os.Stderr, err, "\n")
		os.Exit(1)
	}
	os.Exit(0)
}

func mainErr() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	comms.UseLogger(dex.StdOutLogger("SRV", dex.LevelInfo))

	cfg, err := configure()
	if err != nil {
		return err
	}

	killChan := make(chan os.Signal, 1)
	signal.Notify(killChan, os.Interrupt)
	go func() {
		<-killChan
		fmt.Println("Shutting down...")
		cancel()
	}()

	uri, err := url.Parse(cfg.AdminSrvURL)
	if err != nil {
		return fmt.Errorf("error parsing adminsrvurl: %w", err)
	}

	cl := http.DefaultClient

	if len(cfg.AdminSrvCertPath) > 0 {
		certB, err := os.ReadFile(cfg.AdminSrvCertPath)
		if err != nil {
			return fmt.Errorf("error reading certificate file: %w", err)
		}
		rootCAs, _ := x509.SystemCertPool()
		if rootCAs == nil {
			rootCAs = x509.NewCertPool()
		}
		if ok := rootCAs.AppendCertsFromPEM(certB); !ok {
			return fmt.Errorf("error appending certificate: %w", err)
		}
		cl.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs:    rootCAs,
				MinVersion: tls.VersionTLS12,
				ServerName: uri.Hostname(),
			},
		}
	}

	srv, err := comms.NewServer(&comms.RPCConfig{
		ListenAddrs: []string{"127.0.0.1:" + cfg.Port},
		NoTLS:       true,
	})
	if err != nil {
		return err
	}

	mux := srv.Mux()
	fileServer(mux, "/", "index.html", "text/html")
	fileServer(mux, "/script.js", "script.js", "text/javascript")

	// Everything else goes to the admin server.
	mux.NotFound(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Connection", "close")
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		r.Close = true

		uri := cfg.AdminSrvURL + "/api" + r.URL.Path + "?" + r.URL.RawQuery
		req, err := http.NewRequest(r.Method, uri, r.Body)
		if err != nil {
			http.Error(w, fmt.Sprintf("Error constructing request: %v", err), http.StatusInternalServerError)
			return
		}

		// Encode username and password
		auth := cfg.AdminSrvUsername + ":" + cfg.AdminSrvPassword
		encodedAuth := base64.StdEncoding.EncodeToString([]byte(auth))

		// Set the Authorization header
		req.Header = r.Header
		req.Header.Add("Authorization", "Basic "+encodedAuth)

		resp, err := cl.Do(req)
		if err != nil {
			http.Error(w, fmt.Sprintf("Request error: %v", err), http.StatusInternalServerError)
			return
		}
		defer resp.Body.Close()

		b, _ := io.ReadAll(resp.Body)
		w.WriteHeader(resp.StatusCode)
		if len(b) > 0 {
			_, err = w.Write(append(b, byte('\n')))
			if err != nil {
				fmt.Printf("Write error: %v \n", err)
			}
		}
	})

	fmt.Println("Open http://127.0.0.1:" + cfg.Port + " in your browser")
	srv.Run(ctx)

	return nil
}

// writeJSON writes marshals the provided interface and writes the bytes to the
func fileServer(r chi.Router, pattern, filePath, contentType string) {
	// Define a http.HandlerFunc to serve files but not directory indexes.
	hf := func(w http.ResponseWriter, r *http.Request) {
		// Ensure the path begins with "/".

		w.Header().Set("Content-Type", contentType)

		// Generate the full file system path and test for existence.
		fi, err := os.Stat(filePath)
		if err != nil {
			http.NotFound(w, r)
			return
		}

		// Deny directory listings
		if fi.IsDir() {
			http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
			return
		}

		http.ServeFile(w, r, filePath)
	}

	// Mount the http.HandlerFunc on the pathPrefix.
	r.Get(pattern, hf)
}

type Config struct {
	AppData          string `long:"appdata" description:"Path to application directory."`
	ConfigPath       string `long:"config" description:"Path to an INI configuration file."`
	Testnet          bool   `long:"testnet" description:"use testnet"`
	Harness          bool   `long:"harness" description:"use simnet harness"`
	Port             string `long:"port" description:"Web server port"`
	AdminSrvURL      string `long:"adminsrvurl" description:"Administration HTTPS server address (default: 127.0.0.1:6542)."`
	AdminSrvUsername string `long:"adminsrvuser" description:"Username to user for authentication"`
	AdminSrvPassword string `long:"adminsrvpass" description:"Admin server password. INSECURE. Do not set unless absolutely necessary."`
	AdminSrvCertPath string `long:"adminsrvcertpath" description:"TLS certificate for connecting to the admin server"`
}

var DefaultConfig = Config{
	AppData:          defaultApplicationDirectory,
	AdminSrvUsername: "u",
	Port:             defaultPort,
}

func configure() (*Config, error) {
	// Pre-parse the command line options to see if an alternative config file
	// or the version flag was specified. Override any environment variables
	// with parsed command line flags.
	preCfg := DefaultConfig
	if err := app.ParseCLIConfig(&preCfg); err != nil {
		return nil, err
	}

	if preCfg.AppData != defaultApplicationDirectory {
		preCfg.AppData = dex.CleanAndExpandPath(preCfg.AppData)
		// If the app directory has been changed, but the config file path hasn't,
		// reform the config file path with the new directory.
	}
	if preCfg.ConfigPath == "" {
		switch {
		case preCfg.Harness:
			preCfg.ConfigPath = configFilenameHarness
		case preCfg.Testnet:
			preCfg.ConfigPath = filepath.Join(preCfg.AppData, configFilenameTestnet)
		default: //mainnet
			preCfg.ConfigPath = filepath.Join(preCfg.AppData, configFilenameMainnet)
		}
	}
	configPath := dex.CleanAndExpandPath(preCfg.ConfigPath)

	// Load additional config from file.
	cfg := DefaultConfig
	if err := app.ParseFileConfig(configPath, &cfg); err != nil {
		return nil, err
	}

	if cfg.AdminSrvCertPath == "" {
		return nil, fmt.Errorf("no adminsrvaddr argument in file or by command-line")
	}

	if cfg.AdminSrvPassword == "" {
		return nil, fmt.Errorf("no adminsrvpass argument in file or by command-line")
	}

	if cfg.AdminSrvCertPath != "" {
		cfg.AdminSrvCertPath = dex.CleanAndExpandPath(cfg.AdminSrvCertPath)
	}

	return &cfg, nil
}
