package simharness

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/config"
	dexsrv "decred.org/dcrdex/server/dex"
)

var (
	usr, _   = user.Current()
	Dir      = filepath.Join(usr.HomeDir, "dextest")
	CertPath = filepath.Join(Dir, "dcrdex", "rpc.cert")
	Pass     = []byte("abc")
	Host     = "127.0.0.1:17273"

	dcrID, _   = dex.BipSymbolID("dcr")
	btcID, _   = dex.BipSymbolID("btc")
	ltcID, _   = dex.BipSymbolID("ltc")
	ethID, _   = dex.BipSymbolID("eth")
	bchID, _   = dex.BipSymbolID("bch")
	zecID, _   = dex.BipSymbolID("zec")
	dogeID, _  = dex.BipSymbolID("doge")
	dexttID, _ = dex.BipSymbolID("dextt.eth")
)

const walletNameLength = 6

var chars = []byte("123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz")

func RandomWalletName() string {
	b := make([]byte, walletNameLength)
	for i := range b {
		b[i] = chars[rand.Intn(len(chars))]
	}
	return string(b)
}

func RPCWalletForm(symbol, node, name, port string) (form, parentForm *core.WalletForm) {
	switch symbol {
	case "dcr":
		form = &core.WalletForm{
			Type:    "dcrwalletRPC",
			AssetID: dcrID,
			Config: map[string]string{
				"account":   name,
				"username":  "user",
				"password":  "pass",
				"rpccert":   filepath.Join(Dir, "dcr/"+node+"/rpc.cert"),
				"rpclisten": port,
			},
		}
	case "btc":
		form = &core.WalletForm{
			Type:    "bitcoindRPC",
			AssetID: btcID,
			Config: map[string]string{
				"walletname":  name,
				"rpcuser":     "user",
				"rpcpassword": "pass",
				"rpcport":     port,
			},
		}
	case "ltc":
		form = &core.WalletForm{
			Type:    "litecoindRPC",
			AssetID: ltcID,
			Config: map[string]string{
				"walletname":  name,
				"rpcuser":     "user",
				"rpcpassword": "pass",
				"rpcport":     port,
			},
		}
	case "bch":
		form = &core.WalletForm{
			Type:    "bitcoindRPC",
			AssetID: bchID,
			Config: map[string]string{
				"walletname":  name,
				"rpcuser":     "user",
				"rpcpassword": "pass",
				"rpcport":     port,
			},
		}
	case "zec":
		form = &core.WalletForm{
			Type:    "zcashdRPC",
			AssetID: zecID,
			Config: map[string]string{
				"walletname":  name,
				"rpcuser":     "user",
				"rpcpassword": "pass",
				"rpcport":     port,
			},
		}
	case "doge":
		form = &core.WalletForm{
			Type:    "dogecoindRPC",
			AssetID: dogeID,
			Config: map[string]string{
				"walletname":  name,
				"rpcuser":     "user",
				"rpcpassword": "pass",
				"rpcport":     port,
			},
		}
	case "eth", "dextt.eth":
		form = &core.WalletForm{
			Type:    "geth",
			AssetID: ethID,
			Config: map[string]string{
				"walletname":  name,
				"rpcuser":     "user",
				"rpcpassword": "pass",
				"rpcport":     port,
			},
		}
		if symbol == "dextt.eth" {
			parentForm = form
			form = &core.WalletForm{
				Type:       "token",
				AssetID:    dexttID,
				ParentForm: form,
			}
		}
	}
	return
}

var zecSendMtx sync.RWMutex

// Mine will mine a single block on the node and asset indicated.
func Mine(ctx context.Context, symbol, node string) <-chan *HarnessResult {
	n := 1
	switch symbol {
	case "eth":
		// geth may not include some tx at first because ???. Mine more.
		n = 4
	case "zec":
		// zcash has a problem selecting unused utxo for a second when
		// also mining. https://github.com/zcash/zcash/issues/6045
		zecSendMtx.Lock()
		defer func() {
			time.Sleep(time.Second)
			zecSendMtx.Unlock()
		}()
	}
	return Ctl(ctx, symbol, fmt.Sprintf("./mine-%s", node), fmt.Sprintf("%d", n))
}

func Send(ctx context.Context, symbol, node, addr string, val uint64) error {
	var res *HarnessResult
	switch symbol {
	case "btc", "dcr", "ltc", "doge", "bch":
		res = <-Ctl(ctx, symbol, fmt.Sprintf("./%s", node), "sendtoaddress", addr, ValString(val, symbol))
	case "zec":
		// sendtoaddress will choose spen outputs if a block was
		// recently mined. Use the zecSendMtx to ensure we have waited
		// a sec after mining.
		//
		// TODO: This is not great and does not allow for multiple
		// loadbots to run on zec at once. Find a better way to avoid
		// double spends. Alternatively, wait for zec to fix this and
		// remove the lock https://github.com/zcash/zcash/issues/6045
		zecSendMtx.Lock()
		res = <-Ctl(ctx, symbol, fmt.Sprintf("./%s", node), "sendtoaddress", addr, ValString(val, symbol))
		zecSendMtx.Unlock()
	case "eth":
		// eth values are always handled as gwei, so multiply by 1e9
		// here to convert to wei.
		res = <-Ctl(ctx, symbol, fmt.Sprintf("./%s", node), "attach", fmt.Sprintf("--exec personal.sendTransaction({from:eth.accounts[1],to:\"%s\",gasPrice:200e9,value:%de9},\"%s\")", addr, val, "abc"))
	case "dextt.eth":
		res = <-Ctl(ctx, symbol, "./sendTokens", addr, strconv.FormatFloat(float64(val)/1e9, 'f', 9, 64))
	default:
		return fmt.Errorf("send unknown symbol %q", symbol)
	}
	return res.Err

}

// harnessResult is the result of a harnessCtl command.
type HarnessResult struct {
	Err    error
	Output string
	Cmd    string
}

// String returns a result string for the harnessResult.
func (res *HarnessResult) String() string {
	if res.Err != nil {
		return fmt.Sprintf("error running harness command %q: %v", res.Cmd, res.Err)
	}
	return fmt.Sprintf("response from harness command %q: %s", res.Cmd, res.Output)
}

// Ctl will run the command from the harness-ctl directory for the
// specified symbol. ctx shadows the global context. The global context is not
// used because stopping some nodes will occur after it is canceled.
func Ctl(ctx context.Context, symbol, cmd string, args ...string) <-chan *HarnessResult {
	dir := filepath.Join(Dir, symbol, "harness-ctl")
	c := make(chan *HarnessResult)
	go func() {
		cmd := exec.CommandContext(ctx, cmd, args...)
		cmd.Dir = dir
		output, err := cmd.CombinedOutput()
		c <- &HarnessResult{
			Err:    err,
			Output: strings.TrimSpace(string(output)),
			Cmd:    cmd.String(),
		}
	}()
	return c
}

func CreateWallet(ctx context.Context, symbol, node string) (name, rpcPort string, err error) {
	// Generate a name for this wallet.
	name = RandomWalletName()
	switch symbol {
	case "eth", "dextt.eth":
		// Nothing to do here for internal wallets.
	case "dcr":
		cmdOut := <-Ctl(ctx, symbol, fmt.Sprintf("./%s", node), "createnewaccount", name)
		if cmdOut.Err != nil {
			return "", "", fmt.Errorf("%s create account error: %v", symbol, cmdOut.Err)
		}
		// Even though the harnessCtl is synchronous, I've still observed some
		// issues with trying to create the wallet immediately.
		<-time.After(time.Second)
	case "ltc", "bch", "btc":
		cmdOut := <-Ctl(ctx, symbol, "./new-wallet", node, name)
		if cmdOut.Err != nil {
			return "", "", fmt.Errorf("%s create account error: %v", symbol, cmdOut.Err)
		}
		<-time.After(time.Second)
	case "doge", "zec":
		// Some coins require a totally new node. Create it and monitor
		// it. Shut it down with the stop function before exiting.
		addrs, err := FindOpenAddrs(2)
		if err != nil {
			return "", "", fmt.Errorf("unable to find open ports: %v", err)
		}
		addrPort := addrs[0].String()
		_, rpcPort, err = net.SplitHostPort(addrPort)
		if err != nil {
			return "", "", fmt.Errorf("unable to split addr and port: %v", err)
		}
		addrPort = addrs[1].String()
		_, networkPort, err := net.SplitHostPort(addrPort)
		if err != nil {
			return "", "", fmt.Errorf("unable to split addr and port: %v", err)
		}

		// NOTE: The exec package seems to listen for a SIGINT and call
		// cmd.Process.Kill() when it happens. Because of this it seems
		// zec will error when we run the stop-wallet script because the
		// node already shut down when killing with ctrl-c. doge however
		// does not respect the kill command and still needs the wallet
		// to be stopped here. So, it is probably fine to ignore the
		// error returned from stop-wallet.
		stopFn := func(ctx context.Context) {
			<-Ctl(ctx, symbol, "./stop-wallet", rpcPort)
		}
		if err = ProcessCtl(ctx, symbol, stopFn, "./start-wallet", name, rpcPort, networkPort); err != nil {
			return "", "", fmt.Errorf("%s create account error: %v", symbol, err)
		}
		<-time.After(time.Second * 3)
		if symbol == "zec" {
			<-time.After(time.Second * 10)
		}
		// Connect the new node to the alpha node.
		cmdOut := <-Ctl(ctx, symbol, "./connect-alpha", rpcPort)
		if cmdOut.Err != nil {
			return "", "", fmt.Errorf("%s create account error: %v", symbol, cmdOut.Err)
		}
		<-time.After(time.Second)
	default:
		return "", "", fmt.Errorf("createWallet: symbol %s unknown", symbol)
	}
	return name, rpcPort, nil
}

var nonBtcConversionFactors = map[string]uint64{
	"eth":       1e9,
	"dextt.eth": 1e9,
}

func conversionFactor(symbol string) uint64 {
	if c := nonBtcConversionFactors[symbol]; c > 0 {
		return c
	}
	return 1e8
}

// ValString returns a string representation of the value in conventional
// units.
func ValString(v uint64, assetSymbol string) string {
	precisionStr := fmt.Sprintf("%%.%vf", math.Log10(float64(conversionFactor(assetSymbol))))
	return fmt.Sprintf(precisionStr, float64(v)/float64(conversionFactor(assetSymbol)))
}

// FindOpenAddrs finds unused addresses.
func FindOpenAddrs(n int) ([]net.Addr, error) {
	addrs := make([]net.Addr, 0, n)
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
		addrs = append(addrs, l.Addr())
	}

	return addrs, nil
}

// process stores a long running command and the funcion to stop it on shutdown.
type process struct {
	cmd    *exec.Cmd
	stopFn func(ctx context.Context)
}

var (
	processesMtx sync.Mutex
	processes    []*process
)

// ProcessCtl will run the long running command from the harness-ctl
// directory for the specified symbol. The command and stop function are saved
// to a global slice for stopping later.
func ProcessCtl(ctx context.Context, symbol string, stopFn func(context.Context), cmd string, args ...string) error {
	processesMtx.Lock()
	defer processesMtx.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	dir := filepath.Join(Dir, symbol, "harness-ctl")
	// Killing the process with ctx or process.Kill *sometimes* does not
	// seem to stop the coin daemon. Kill later with an rpc "stop" command
	// contained in the stop function.
	command := exec.Command(cmd, args...)
	command.Dir = dir
	p := &process{
		cmd:    command,
		stopFn: stopFn,
	}
	if err := command.Start(); err != nil {
		return fmt.Errorf("unable to start process: %v", err)
	}
	processes = append(processes, p)
	return nil
}

// Shutdown stops some long running processes.
func Shutdown() {
	processesMtx.Lock()
	defer processesMtx.Unlock()
	for _, p := range processes {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
		defer cancel()
		p.stopFn(ctx)
		if err := p.cmd.Wait(); err != nil {
			fmt.Printf("failed to wait for stopped process: %v\n", err)
		}
	}
}

// LoadNodeConfig loads the INI configuration for the specified node into a
// map[string]string.
func LoadNodeConfig(symbol, node string) map[string]string {
	cfgPath := filepath.Join(Dir, symbol, node, node+".conf")
	if symbol == "eth" {
		cfgPath = filepath.Join(Dir, symbol, node, "node", "eth.conf")
	}
	cfg, err := config.Parse(cfgPath)
	if err != nil {
		panic(fmt.Sprintf("error parsing harness config file at %s: %v", cfgPath, err))
	}
	return cfg
}

// MarketsDotJSON models the server's markets.json configuration file.
type MarketsDotJSON struct {
	Markets []*struct {
		Base     string  `json:"base"`
		Quote    string  `json:"quote"`
		LotSize  uint64  `json:"lotSize"`
		RateStep uint64  `json:"rateStep"`
		Duration uint64  `json:"epochDuration"`
		MBBuffer float64 `json:"marketBuyBuffer"`
	} `json:"markets"`
	Assets map[string]*dexsrv.AssetConf `json:"assets"`
}

func ServerMarketsConfig() (*MarketsDotJSON, error) {
	f, err := os.ReadFile(filepath.Join(Dir, "dcrdex", "markets.json"))
	if err != nil {
		return nil, fmt.Errorf("error reading simnet dcrdex markets.json file: %v", err)
	}
	mktsCfg := new(MarketsDotJSON)
	err = json.Unmarshal(f, &mktsCfg)
	if err != nil {
		return nil, fmt.Errorf("error unmarshaling markets.json: %v", err)
	}
	return mktsCfg, nil
}

func RegisterCore(c *core.Core, regSymbol string) error {
	exchange, err := c.GetDEXConfig(Host, CertPath)
	if err != nil {
		return fmt.Errorf("unable to get dex config: %v", err)
	}
	feeAsset := exchange.RegFees[regSymbol]
	if feeAsset == nil {
		return fmt.Errorf("dex does not support asset %v for registration", regSymbol)
	}
	fee := feeAsset.Amt

	regAssetID, _ := dex.BipSymbolID(regSymbol)

	_, err = c.Register(&core.RegisterForm{
		Addr:    Host,
		AppPass: Pass,
		Fee:     fee,
		Asset:   &regAssetID,
		Cert:    CertPath,
	})
	if err != nil {
		return fmt.Errorf("registration error: %v", err)
	}
	return nil
}

func UnlockWallet(ctx context.Context, symbol string) error {
	switch symbol {
	case "btc", "ltc", "doge", "bch":
		<-Ctl(ctx, symbol, "./alpha", "walletpassphrase", "abc", "4294967295")
		<-Ctl(ctx, symbol, "./beta", "walletpassphrase", "abc", "4294967295")
	case "dcr":
		<-Ctl(ctx, symbol, "./alpha", "walletpassphrase", "abc", "0")
		<-Ctl(ctx, symbol, "./beta", "walletpassphrase", "abc", "0") // creating new accounts requires wallet unlocked
		<-Ctl(ctx, symbol, "./beta", "unlockaccount", "default", "abc")
	case "eth", "zec":
		// eth unlocking for send, so no need to here. Mining
		// accounts are always unlocked. zec is unlocked already.
	default:
		return fmt.Errorf("unlockWallets: unknown symbol %q", symbol)
	}
	return nil
}

func init() {
	rand.Seed(time.Now().UnixNano())
}
