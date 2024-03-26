// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package tor

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"decred.org/dcrdex/dex"
	"github.com/jrick/logrotate/rotator"
)

type HiddenService struct {
	log     dex.Logger
	exePath string
	dataDir string
	logPath string

	serverAddr string
	onionAddr  string
}

func New(dataDir string, log dex.Logger) (_ *HiddenService, err error) {
	if len(torBinary) == 0 {
		log.Error("It doesn't look like tor was packaged correctly")
		log.Error("Tor is only available on Linux for now")
		log.Error("If you are on linux and building bisonwallet from source, run client/build_linux.sh and then rebuild bisonwallet")
		log.Error("If this is a release, we really botched it")
		return nil, errors.New("tor not packaged")
	}

	logDir := filepath.Join(dataDir, "logs")
	if err := os.MkdirAll(logDir, 0700); err != nil {
		return nil, fmt.Errorf("error creating tor directory: %w", err)
	}

	exePath := filepath.Join(dataDir, "tor")
	if err := os.WriteFile(exePath, torBinary, 0700); err != nil {
		return nil, fmt.Errorf("error writing tor binary: %w", err)
	}

	return &HiddenService{
		log:     log,
		exePath: exePath,
		logPath: filepath.Join(logDir, "tor.Log"),
		dataDir: dataDir,
	}, nil
}

const torrcTemplate = `#Bison Wallet tor relay configuration
SOCKSPort %s
DataDirectory %s

HiddenServiceDir %s
HiddenServicePort 80 %s
`

func (s *HiddenService) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	torrcPath := filepath.Join(s.dataDir, "torrc")

	hiddenServiceDir := filepath.Join(s.dataDir, "hidden-service")

	ports, err := findOpenPort(2)
	if err != nil {
		return nil, fmt.Errorf("error finding open port: %w", err)
	}

	socksPort, serverPort := ports[0], ports[1]
	s.serverAddr = "127.0.0.1:" + serverPort

	torrcData := []byte(fmt.Sprintf(torrcTemplate, socksPort, s.dataDir, hiddenServiceDir, s.serverAddr))
	if err := os.WriteFile(torrcPath, torrcData, 0600); err != nil {
		return nil, fmt.Errorf("error writing torrc file: %w", err)
	}

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()

		const maxLogRolls = 8
		const logFileMaxSizeKB = 16 * 1024 // 16 MB
		logRotator, err := rotator.New(s.logPath, logFileMaxSizeKB, false, maxLogRolls)
		if err != nil {
			s.log.Errorf("error intializing log files: %w", err)
			return
		}
		defer logRotator.Close()

		cmd := exec.CommandContext(ctx, s.exePath, "-f", torrcPath)
		cmd.Stdout = logRotator
		cmd.Stderr = logRotator
		cmd.Cancel = func() error {
			if cmd.Process == nil { // probably not possible?
				return nil
			}
			if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
				cmd.Process.Kill()
				return err
			}
			return nil
		}
		if err := cmd.Start(); err != nil {
			s.log.Errorf("tor node exited with an error: %v", err)
			return
		}

		<-ctx.Done()

		errC := make(chan error)
		go func() {
			_, err := cmd.Process.Wait()
			errC <- err
		}()

		select {
		case err := <-errC:
			if err != nil {
				s.log.Errorf("Error attempting clean shutdown: %v", err)
			}
		case <-time.After(time.Second * 5):
			cmd.Process.Kill()
			s.log.Error("Timed out waiting for clean shutdown")
		}
	}()

	hostnamePath := filepath.Join(hiddenServiceDir, "hostname")
	timeout := time.After(time.Second * 10)
	checkTick := time.After(0)
out:
	for {
		select {
		case <-checkTick:
			b, err := os.ReadFile(hostnamePath)
			if err != nil {
				if !os.IsNotExist(err) {
					return nil, fmt.Errorf("error attemtpting to read hostname file: %w", err)
				}
			} else {
				s.onionAddr = strings.TrimSpace(string(b))
				break out
			}
		case <-timeout:
			return nil, fmt.Errorf("timed out waiting for tor to publish hostname")
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		checkTick = time.After(time.Millisecond * 100)
	}

	return &wg, nil
}

func (s *HiddenService) ServerAddress() string {
	return s.serverAddr
}

func (s *HiddenService) OnionAddress() string {
	return "http://" + s.onionAddr
}

func findOpenPort(n int) ([]string, error) {
	ports := make([]string, n)
	for i := 0; i < n; i++ {
		addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
		if err != nil {
			return nil, err
		}

		l, err := net.ListenTCP("tcp", addr)
		if err != nil {
			return nil, err
		}
		defer l.Close()
		_, port, err := net.SplitHostPort(l.Addr().String())
		if err != nil {
			return nil, err
		}
		ports[i] = port
	}

	return ports, nil
}
