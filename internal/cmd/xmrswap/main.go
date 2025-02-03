package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/config"
	"decred.org/dcrdex/internal/adaptorsigs"
	dcradaptor "decred.org/dcrdex/internal/adaptorsigs/dcr"
	"decred.org/dcrwallet/v4/rpc/client/dcrwallet"
	dcrwalletjson "decred.org/dcrwallet/v4/rpc/jsonrpc/types"
	"github.com/agl/ed25519/edwards25519"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrec"
	"github.com/decred/dcrd/dcrec/edwards/v2"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/schnorr"
	"github.com/decred/dcrd/rpcclient/v8"
	"github.com/decred/dcrd/txscript/v4"
	"github.com/decred/dcrd/txscript/v4/sign"
	"github.com/decred/dcrd/txscript/v4/stdaddr"
	"github.com/decred/dcrd/wire"
	"github.com/decred/slog"
	"github.com/dev-warrior777/go-monero/rpc"
	"github.com/fatih/color"
	"github.com/haven-protocol-org/monero-go-utils/base58"
)

// TODO: Verification at all stages has not been implemented yet.

// fieldIntSize is the size of a field element encoded
// as bytes.
const (
	fieldIntSize = 32
	dcrAmt       = 7_000_000 // atoms
	xmrAmt       = 1_000     // 1e12 units
	dumbFee      = int64(6000)
	configName   = "config.json"
	lockBlocks   = 2
)

var (
	homeDir    = os.Getenv("HOME")
	dextestDir = filepath.Join(homeDir, "dextest")
	bobDir     = filepath.Join(dextestDir, "xmr", "wallets", "bob")
	curve      = edwards.Edwards()

	// These should be wallets with funds.
	alicexmr = "http://127.0.0.1:28284/json_rpc"
	bobdcr   = filepath.Join(dextestDir, "dcr", "trading2", "trading2.conf")

	// These do not need funds.
	bobxmr   = "http://127.0.0.1:28184/json_rpc"
	alicedcr = filepath.Join(dextestDir, "dcr", "trading1", "trading1.conf")

	// This wallet does not need funds or to be loaded.
	extraxmr = "http://127.0.0.1:28484/json_rpc"

	testnet bool
	netTag  = uint64(18)
)

func init() {
	flag.BoolVar(&testnet, "testnet", false, "use testnet")
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
	}
}

type walletClient = dcrwallet.Client

type combinedClient struct {
	*rpcclient.Client
	*walletClient
	chainParams *chaincfg.Params
}

func newCombinedClient(nodeRPCClient *rpcclient.Client, chainParams *chaincfg.Params) *combinedClient {
	return &combinedClient{
		nodeRPCClient,
		dcrwallet.NewClient(dcrwallet.RawRequestCaller(nodeRPCClient), chainParams),
		chainParams,
	}
}

type client struct {
	xmr *rpc.Client
	dcr *combinedClient

	viewKey                                                                *edwards.PrivateKey
	pubSpendKeyf, pubSpendKey                                              *edwards.PublicKey
	pubInitSignKeyHalf, pubPartSignKeyHalf, pubSpendKeyProof, pubSpendKeyl *secp256k1.PublicKey
	partSpendKeyHalfDleag, initSpendKeyHalfDleag                           []byte
	lockTxEsig                                                             *adaptorsigs.AdaptorSignature
	lockTx                                                                 *wire.MsgTx
	vIn                                                                    int
}

// initClient is the swap initiator.
type initClient struct {
	*client
	initSpendKeyHalf *edwards.PrivateKey
	initSignKeyHalf  *secp256k1.PrivateKey
}

// partClient is the participator.
type partClient struct {
	*client
	partSpendKeyHalf *edwards.PrivateKey
	partSignKeyHalf  *secp256k1.PrivateKey
}

func newRPCWallet(settings map[string]string, logger dex.Logger, net dex.Network) (*combinedClient, error) {
	certs, err := os.ReadFile(settings["rpccert"])
	if err != nil {
		return nil, fmt.Errorf("TLS certificate read error: %w", err)
	}

	cfg := &rpcclient.ConnConfig{
		Host:                settings["rpclisten"],
		Endpoint:            "ws",
		User:                settings["rpcuser"],
		Pass:                settings["rpcpass"],
		Certificates:        certs,
		DisableConnectOnNew: true, // don't start until Connect
	}
	if cfg.User == "" {
		cfg.User = "user"
	}
	if cfg.Pass == "" {
		cfg.Pass = "pass"
	}

	nodeRPCClient, err := rpcclient.New(cfg, nil)
	if err != nil {
		return nil, fmt.Errorf("error setting up rpc client: %w", err)
	}

	var params *chaincfg.Params
	switch net {
	case dex.Simnet:
		params = chaincfg.SimNetParams()
	case dex.Testnet:
		params = chaincfg.TestNet3Params()
	case dex.Mainnet:
		params = chaincfg.MainNetParams()
	default:
		return nil, fmt.Errorf("unknown network ID: %d", uint8(net))
	}

	return newCombinedClient(nodeRPCClient, params), nil
}

func newClient(ctx context.Context, xmrAddr, dcrConf string) (*client, error) {
	xmr := rpc.New(rpc.Config{
		Address: xmrAddr,
		Client:  &http.Client{},
	})

	ticker := time.NewTicker(time.Second * 5)
	defer ticker.Stop()
	balReq := rpc.GetBalanceRequest{}
	i := 0
out:
	for {
		select {
		case <-ticker.C:
			bal, err := xmr.GetBalance(ctx, &balReq)
			if err != nil {
				return nil, fmt.Errorf("unable to get xmr balance: %v", err)
			}
			if bal.UnlockedBalance > xmrAmt*2 {
				break out
			}
			if i%5 == 0 {
				fmt.Println("xmr wallet has no unlocked funds. Waiting...")
			}
			i++
		case <-ctx.Done():
			return nil, ctx.Err()
		}

	}

	settings, err := config.Parse(dcrConf)
	if err != nil {
		return nil, err
	}
	settings["account"] = "default"

	net := dex.Simnet
	if testnet {
		net = dex.Testnet
	}
	dcr, err := newRPCWallet(settings, dex.StdOutLogger("client", slog.LevelTrace), net)
	if err != nil {
		return nil, err
	}

	err = dcr.Connect(ctx, false)
	if err != nil {
		return nil, err
	}

	return &client{
		xmr: xmr,
		dcr: dcr,
	}, nil
}

// reverse reverses a byte string.
func reverse(s *[32]byte) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}

// bigIntToEncodedBytes converts a big integer into its corresponding
// 32 byte little endian representation.
func bigIntToEncodedBytes(a *big.Int) *[32]byte {
	s := new([32]byte)
	if a == nil {
		return s
	}
	// Caveat: a can be longer than 32 bytes.
	aB := a.Bytes()

	// If we have a short byte string, expand
	// it so that it's long enough.
	aBLen := len(aB)
	if aBLen < fieldIntSize {
		diff := fieldIntSize - aBLen
		for i := 0; i < diff; i++ {
			aB = append([]byte{0x00}, aB...)
		}
	}

	for i := 0; i < fieldIntSize; i++ {
		s[i] = aB[i]
	}

	// Reverse the byte string --> little endian after
	// encoding.
	reverse(s)

	return s
}

// encodedBytesToBigInt converts a 32 byte little endian representation of
// an integer into a big, big endian integer.
func encodedBytesToBigInt(s *[32]byte) *big.Int {
	// Use a copy so we don't screw up our original
	// memory.
	sCopy := new([32]byte)
	for i := 0; i < fieldIntSize; i++ {
		sCopy[i] = s[i]
	}
	reverse(sCopy)

	bi := new(big.Int).SetBytes(sCopy[:])

	return bi
}

// scalarAdd adds two scalars.
func scalarAdd(a, b *big.Int) *big.Int {
	feA := bigIntToFieldElement(a)
	feB := bigIntToFieldElement(b)
	sum := new(edwards25519.FieldElement)

	edwards25519.FeAdd(sum, feA, feB)
	sumArray := new([32]byte)
	edwards25519.FeToBytes(sumArray, sum)

	return encodedBytesToBigInt(sumArray)
}

// bigIntToFieldElement converts a big little endian integer into its corresponding
// 40 byte field representation.
func bigIntToFieldElement(a *big.Int) *edwards25519.FieldElement {
	aB := bigIntToEncodedBytes(a)
	fe := new(edwards25519.FieldElement)
	edwards25519.FeFromBytes(fe, aB)
	return fe
}

func sumPubKeys(pubA, pubB *edwards.PublicKey) *edwards.PublicKey {
	pkSumX, pkSumY := curve.Add(pubA.GetX(), pubA.GetY(), pubB.GetX(), pubB.GetY())
	return edwards.NewPublicKey(pkSumX, pkSumY)
}

// Convert the DCR value to atoms.
func toAtoms(v float64) uint64 {
	return uint64(math.Round(v * 1e8))
}

// createNewXMRWallet uses the "own" wallet to create a new xmr wallet from keys
// and open it. Can only create one wallet at a time.
func createNewXMRWallet(ctx context.Context, genReq rpc.GenerateFromKeysRequest) (*rpc.Client, error) {
	xmrChecker := rpc.New(rpc.Config{
		Address: extraxmr,
		Client:  &http.Client{},
	})

	_, err := xmrChecker.GenerateFromKeys(ctx, &genReq)
	if err != nil {
		return nil, fmt.Errorf("unable to generate wallet: %v", err)
	}

	openReq := rpc.OpenWalletRequest{
		Filename: genReq.Filename,
	}

	err = xmrChecker.OpenWallet(ctx, &openReq)
	if err != nil {
		return nil, err
	}
	return xmrChecker, nil
}

type prettyLogger struct {
	c *color.Color
}

func (cl prettyLogger) Write(p []byte) (n int, err error) {
	return cl.c.Fprint(os.Stdout, string(p))
}

func run(ctx context.Context) error {
	if err := parseConfig(); err != nil {
		return err
	}

	if testnet {
		netTag = 24 // stagenet
	}

	pl := prettyLogger{c: color.New(color.FgGreen)}
	log := dex.NewLogger("T", dex.LevelInfo, pl)

	log.Info("Running success.")
	if err := success(ctx); err != nil {
		return err
	}
	log.Info("Success completed without error.")
	log.Info("------------------")
	log.Info("Running alice bails before xmr init.")
	if err := aliceBailsBeforeXmrInit(ctx); err != nil {
		return err
	}
	log.Info("Alice bails before xmr init completed without error.")
	log.Info("------------------")
	log.Info("Running refund.")
	if err := refund(ctx); err != nil {
		return err
	}
	log.Info("Refund completed without error.")
	log.Info("------------------")
	log.Info("Running bob bails after xmr init.")
	if err := bobBailsAfterXmrInit(ctx); err != nil {
		return err
	}
	log.Info("Bob bails after xmr init completed without error.")
	return nil
}

type clientJSON struct {
	XMRHost string `json:"xmrhost"`
	DCRConf string `json:"dcrconf"`
}

type configJSON struct {
	Alice        clientJSON `json:"alice"`
	Bob          clientJSON `json:"bob"`
	ExtraXMRHost string     `json:"extraxmrhost"`
}

func parseConfig() error {
	flag.Parse()

	if !testnet {
		return nil
	}

	ex, err := os.Executable()
	if err != nil {
		return err
	}

	exPath := filepath.Dir(ex)
	configPath := filepath.Join(exPath, configName)

	b, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}

	var cj configJSON
	if err := json.Unmarshal(b, &cj); err != nil {
		return err
	}

	alicexmr = cj.Alice.XMRHost
	bobxmr = cj.Bob.XMRHost
	alicedcr = cj.Alice.DCRConf
	bobdcr = cj.Bob.DCRConf
	extraxmr = cj.ExtraXMRHost

	return nil
}

// generateDleag starts the trade by creating some keys.
func (c *partClient) generateDleag(ctx context.Context) (pubSpendKeyf *edwards.PublicKey, kbvf *edwards.PrivateKey,
	pubPartSignKeyHalf *secp256k1.PublicKey, dleag []byte, err error) {
	fail := func(err error) (*edwards.PublicKey, *edwards.PrivateKey,
		*secp256k1.PublicKey, []byte, error) {
		return nil, nil, nil, nil, err
	}
	// This private key is shared with bob and becomes half of the view key.
	kbvf, err = edwards.GeneratePrivateKey()
	if err != nil {
		return fail(err)
	}

	// Not shared. Becomes half the spend key. The pubkey is shared.
	c.partSpendKeyHalf, err = edwards.GeneratePrivateKey()
	if err != nil {
		return fail(err)
	}
	c.pubSpendKeyf = c.partSpendKeyHalf.PubKey()

	// Not shared. This is used for all dcr signatures. Using a wallet
	// address because funds may go here in the case of success. Any address
	// would work for the spendTx though.
	partSignKeyHalfAddr, err := c.dcr.GetNewAddress(ctx, "default")
	if err != nil {
		return fail(err)
	}
	partSignKeyHalfWIF, err := c.dcr.DumpPrivKey(ctx, partSignKeyHalfAddr)
	if err != nil {
		return fail(err)
	}
	c.partSignKeyHalf = secp256k1.PrivKeyFromBytes(partSignKeyHalfWIF.PrivKey())

	// Share this pubkey with the other party.
	c.pubPartSignKeyHalf = c.partSignKeyHalf.PubKey()

	c.partSpendKeyHalfDleag, err = adaptorsigs.ProveDLEQ(c.partSpendKeyHalf.Serialize())
	if err != nil {
		return fail(err)
	}

	c.pubSpendKeyProof, err = adaptorsigs.ExtractSecp256k1PubKeyFromProof(c.partSpendKeyHalfDleag)
	if err != nil {
		return fail(err)
	}

	return c.pubSpendKeyf, kbvf, c.pubPartSignKeyHalf, c.partSpendKeyHalfDleag, nil
}

// generateLockTxn creates even more keys and some transactions.
func (c *initClient) generateLockTxn(ctx context.Context, pubSpendKeyf *edwards.PublicKey,
	kbvf *edwards.PrivateKey, pubPartSignKeyHalf *secp256k1.PublicKey, partSpendKeyHalfDleag []byte) (refundSig,
	lockRefundTxScript, lockTxScript []byte, refundTx, spendRefundTx *wire.MsgTx, lockTxVout int,
	pubSpendKey *edwards.PublicKey, viewKey *edwards.PrivateKey, dleag []byte, bdcrpk *secp256k1.PublicKey, err error) {

	fail := func(err error) ([]byte, []byte, []byte, *wire.MsgTx, *wire.MsgTx, int, *edwards.PublicKey, *edwards.PrivateKey, []byte, *secp256k1.PublicKey, error) {
		return nil, nil, nil, nil, nil, 0, nil, nil, nil, nil, err
	}
	c.partSpendKeyHalfDleag = partSpendKeyHalfDleag
	c.pubSpendKeyProof, err = adaptorsigs.ExtractSecp256k1PubKeyFromProof(c.partSpendKeyHalfDleag)
	if err != nil {
		return fail(err)
	}
	c.pubSpendKeyf = pubSpendKeyf
	c.pubPartSignKeyHalf = pubPartSignKeyHalf

	// This becomes the other half of the view key.
	kbvl, err := edwards.GeneratePrivateKey()
	if err != nil {
		return fail(err)
	}

	// This becomes the other half of the spend key and is shared.
	c.initSpendKeyHalf, err = edwards.GeneratePrivateKey()
	if err != nil {
		return fail(err)
	}

	// This kept private. This is used for all dcr signatures.
	c.initSignKeyHalf, err = secp256k1.GeneratePrivateKey()
	if err != nil {
		return fail(err)
	}

	pubInitSignKeyHalf := c.initSignKeyHalf.PubKey()

	// This is the full xmr view key and is shared. Alice can also calculate
	// it using kbvl.
	viewKeyBig := scalarAdd(kbvf.GetD(), kbvl.GetD())
	viewKeyBig.Mod(viewKeyBig, curve.N)
	var viewKeyBytes [32]byte
	viewKeyBig.FillBytes(viewKeyBytes[:])
	c.viewKey, _, err = edwards.PrivKeyFromScalar(viewKeyBytes[:])
	if err != nil {
		return fail(fmt.Errorf("unable to create viewKey: %v", err))
	}

	// The public key for the xmr spend key. No party knows the full private
	// key yet.
	c.pubSpendKey = sumPubKeys(c.initSpendKeyHalf.PubKey(), c.pubSpendKeyf)

	// The lock tx is the initial dcr transaction.
	lockTxScript, err = dcradaptor.LockTxScript(pubInitSignKeyHalf.SerializeCompressed(), c.pubPartSignKeyHalf.SerializeCompressed())
	if err != nil {
		return fail(err)
	}

	scriptAddr, err := stdaddr.NewAddressScriptHashV0(lockTxScript, c.dcr.chainParams)
	if err != nil {
		return fail(fmt.Errorf("error encoding script address: %w", err))
	}
	p2shLockScriptVer, p2shLockScript := scriptAddr.PaymentScript()
	// Add the transaction output.
	txOut := &wire.TxOut{
		Value:    dcrAmt,
		Version:  p2shLockScriptVer,
		PkScript: p2shLockScript,
	}
	unfundedLockTx := wire.NewMsgTx()
	unfundedLockTx.AddTxOut(txOut)
	txBytes, err := unfundedLockTx.Bytes()
	if err != nil {
		return fail(err)
	}

	fundRes, err := c.dcr.FundRawTransaction(ctx, hex.EncodeToString(txBytes), "default", dcrwalletjson.FundRawTransactionOptions{})
	if err != nil {
		return fail(err)
	}

	txBytes, err = hex.DecodeString(fundRes.Hex)
	if err != nil {
		return fail(err)
	}

	c.lockTx = wire.NewMsgTx()
	if err = c.lockTx.FromBytes(txBytes); err != nil {
		return fail(err)
	}
	for i, out := range c.lockTx.TxOut {
		if bytes.Equal(out.PkScript, p2shLockScript) {
			c.vIn = i
			break
		}
	}

	durationLocktime := int64(lockBlocks) // blocks
	// Unable to use time for tests as this is multiples of 512 seconds.
	// durationLocktime := int64(10) // seconds * 512
	// durationLocktime |= wire.SequenceLockTimeIsSeconds

	// The refund tx does not outright refund but moves funds to the refund
	// script's address. This is signed by both parties before the initial tx.
	lockRefundTxScript, err = dcradaptor.LockRefundTxScript(pubInitSignKeyHalf.SerializeCompressed(), c.pubPartSignKeyHalf.SerializeCompressed(), durationLocktime)
	if err != nil {
		return fail(err)
	}

	scriptAddr, err = stdaddr.NewAddressScriptHashV0(lockRefundTxScript, c.dcr.chainParams)
	if err != nil {
		return fail(fmt.Errorf("error encoding script address: %w", err))
	}
	p2shScriptVer, p2shScript := scriptAddr.PaymentScript()
	txOut = &wire.TxOut{
		Value:    dcrAmt - dumbFee,
		Version:  p2shScriptVer,
		PkScript: p2shScript,
	}
	refundTx = wire.NewMsgTx()
	refundTx.AddTxOut(txOut)
	h := c.lockTx.TxHash()
	op := wire.NewOutPoint(&h, uint32(c.vIn), 0)
	txIn := wire.NewTxIn(op, dcrAmt, nil)
	refundTx.AddTxIn(txIn)

	// This sig must be shared with Alice.
	refundSig, err = sign.RawTxInSignature(refundTx, c.vIn, lockTxScript, txscript.SigHashAll, c.initSignKeyHalf.Serialize(), dcrec.STSchnorrSecp256k1)
	if err != nil {
		return fail(err)
	}

	// SpendRefundTx is used in the final refund. Alice can sign it after a
	// time and send wherever. Bob must use a signature that will reveal his
	// half of the xmr key.
	newAddr, err := c.dcr.GetNewAddress(ctx, "default")
	if err != nil {
		return fail(err)
	}
	p2AddrScriptVer, p2AddrScript := newAddr.PaymentScript()
	txOut = &wire.TxOut{
		Value:    dcrAmt - dumbFee - dumbFee,
		Version:  p2AddrScriptVer,
		PkScript: p2AddrScript,
	}
	spendRefundTx = wire.NewMsgTx()
	spendRefundTx.AddTxOut(txOut)
	h = refundTx.TxHash()
	op = wire.NewOutPoint(&h, 0, 0)
	txIn = wire.NewTxIn(op, dcrAmt, nil)
	txIn.Sequence = uint32(durationLocktime)
	spendRefundTx.AddTxIn(txIn)
	spendRefundTx.Version = wire.TxVersionTreasury

	c.initSpendKeyHalfDleag, err = adaptorsigs.ProveDLEQ(c.initSpendKeyHalf.Serialize())
	if err != nil {
		return fail(err)
	}
	c.pubSpendKeyl, err = adaptorsigs.ExtractSecp256k1PubKeyFromProof(c.initSpendKeyHalfDleag)
	if err != nil {
		return fail(err)
	}

	return refundSig, lockRefundTxScript, lockTxScript, refundTx, spendRefundTx, c.vIn, c.pubSpendKey, c.viewKey, c.initSpendKeyHalfDleag, pubInitSignKeyHalf, nil
}

// generateRefundSigs signs the refund tx and shares the spendRefund esig that
// allows bob to spend the refund tx.
func (c *partClient) generateRefundSigs(refundTx, spendRefundTx *wire.MsgTx, vIn int, lockTxScript, lockRefundTxScript []byte, dleag []byte) (esig *adaptorsigs.AdaptorSignature, refundSig []byte, err error) {
	fail := func(err error) (*adaptorsigs.AdaptorSignature, []byte, error) {
		return nil, nil, err
	}
	c.initSpendKeyHalfDleag = dleag
	c.vIn = vIn
	c.pubSpendKeyl, err = adaptorsigs.ExtractSecp256k1PubKeyFromProof(c.initSpendKeyHalfDleag)
	if err != nil {
		return fail(err)
	}

	hash, err := txscript.CalcSignatureHash(lockRefundTxScript, txscript.SigHashAll, spendRefundTx, 0, nil)
	if err != nil {
		return fail(err)
	}

	var h chainhash.Hash
	copy(h[:], hash)

	jacobianBobPubKey := new(secp256k1.JacobianPoint)
	c.pubSpendKeyl.AsJacobian(jacobianBobPubKey)
	esig, err = adaptorsigs.PublicKeyTweakedAdaptorSig(c.partSignKeyHalf, h[:], jacobianBobPubKey)
	if err != nil {
		return fail(err)
	}

	// Share with bob.
	refundSig, err = sign.RawTxInSignature(refundTx, c.vIn, lockTxScript, txscript.SigHashAll, c.partSignKeyHalf.Serialize(), dcrec.STSchnorrSecp256k1)
	if err != nil {
		return fail(err)
	}

	return esig, refundSig, nil
}

// initDcr is the first transaction to happen and creates a dcr transaction.
func (c *initClient) initDcr(ctx context.Context) (spendTx *wire.MsgTx, err error) {
	fail := func(err error) (*wire.MsgTx, error) {
		return nil, err
	}
	pubSpendKeyProofAddr, err := stdaddr.NewAddressPubKeyHashEcdsaSecp256k1(0, stdaddr.Hash160(c.pubPartSignKeyHalf.SerializeCompressed()), c.dcr.chainParams)
	if err != nil {
		return fail(err)
	}
	p2AddrScriptVer, p2AddrScript := pubSpendKeyProofAddr.PaymentScript()

	txOut := &wire.TxOut{
		Value:    dcrAmt - dumbFee,
		Version:  p2AddrScriptVer,
		PkScript: p2AddrScript,
	}
	spendTx = wire.NewMsgTx()
	spendTx.AddTxOut(txOut)
	h := c.lockTx.TxHash()
	op := wire.NewOutPoint(&h, uint32(c.vIn), 0)
	txIn := wire.NewTxIn(op, dcrAmt, nil)
	spendTx.AddTxIn(txIn)

	tx, complete, err := c.dcr.SignRawTransaction(ctx, c.lockTx)
	if err != nil {
		return fail(err)
	}
	if !complete {
		return fail(errors.New("lock tx sign not complete"))
	}

	_, err = c.dcr.SendRawTransaction(ctx, tx, false)
	if err != nil {
		return fail(fmt.Errorf("unable to send lock tx: %v", err))
	}

	return spendTx, nil
}

// initXmr sends an xmr transaciton. Alice can only do this after confirming the
// dcr transaction.
func (c *partClient) initXmr(ctx context.Context, viewKey *edwards.PrivateKey, pubSpendKey *edwards.PublicKey) error {
	c.viewKey = viewKey
	c.pubSpendKey = pubSpendKey
	var fullPubKey []byte
	fullPubKey = append(fullPubKey, c.pubSpendKey.SerializeCompressed()...)
	fullPubKey = append(fullPubKey, c.viewKey.PubKey().SerializeCompressed()...)

	sharedAddr := base58.EncodeAddr(netTag, fullPubKey)

	dest := rpc.Destination{
		Amount:  xmrAmt,
		Address: sharedAddr,
	}
	sendReq := rpc.TransferRequest{
		Destinations: []rpc.Destination{dest},
	}

	sendRes, err := c.xmr.Transfer(ctx, &sendReq)
	if err != nil {
		return fmt.Errorf("unable to send xmr: %v", err)
	}
	fmt.Printf("xmr sent\n%+v\n", *sendRes)
	return nil
}

// sendLockTxSig allows Alice to redeem the dcr. If bob does not send this alice
// can eventually take his btc. Otherwise bob refunding will reveal his half of
// the xmr spend key allowing Alice to refund.
func (c *initClient) sendLockTxSig(lockTxScript []byte, spendTx *wire.MsgTx) (esig *adaptorsigs.AdaptorSignature, err error) {
	hash, err := txscript.CalcSignatureHash(lockTxScript, txscript.SigHashAll, spendTx, 0, nil)
	if err != nil {
		return nil, err
	}

	var h chainhash.Hash
	copy(h[:], hash)

	jacobianAlicePubKey := new(secp256k1.JacobianPoint)
	c.pubSpendKeyProof.AsJacobian(jacobianAlicePubKey)
	esig, err = adaptorsigs.PublicKeyTweakedAdaptorSig(c.initSignKeyHalf, h[:], jacobianAlicePubKey)
	if err != nil {
		return nil, err
	}

	c.lockTxEsig = esig

	return esig, nil
}

// redeemDcr redeems the dcr, revealing a signature that reveals half of the xmr
// spend key.
func (c *partClient) redeemDcr(ctx context.Context, esig *adaptorsigs.AdaptorSignature, lockTxScript []byte, spendTx *wire.MsgTx, bobDCRPK *secp256k1.PublicKey) (initSignKeyHalfSig []byte, err error) {
	kasl := secp256k1.PrivKeyFromBytes(c.partSpendKeyHalf.Serialize())

	initSignKeyHalfSigShnorr, err := esig.Decrypt(&kasl.Key)
	if err != nil {
		return nil, err
	}
	initSignKeyHalfSig = initSignKeyHalfSigShnorr.Serialize()
	initSignKeyHalfSig = append(initSignKeyHalfSig, byte(txscript.SigHashAll))

	partSignKeyHalfSig, err := sign.RawTxInSignature(spendTx, 0, lockTxScript, txscript.SigHashAll, c.partSignKeyHalf.Serialize(), dcrec.STSchnorrSecp256k1)
	if err != nil {
		return nil, err
	}

	spendSig, err := txscript.NewScriptBuilder().
		AddData(partSignKeyHalfSig).
		AddData(initSignKeyHalfSig).
		AddData(lockTxScript).
		Script()
	if err != nil {
		return nil, err
	}

	spendTx.TxIn[0].SignatureScript = spendSig

	tx, err := c.dcr.SendRawTransaction(ctx, spendTx, false)
	if err != nil {
		return nil, err
	}

	fmt.Println("Redeem Tx -", tx)

	return initSignKeyHalfSig, nil
}

// redeemXmr redeems xmr by creating a new xmr wallet with the complete spend
// and view private keys.
func (c *initClient) redeemXmr(ctx context.Context, initSignKeyHalfSig []byte, restoreHeight uint64) (*rpc.Client, error) {
	initSignKeyHalfSigParsed, err := schnorr.ParseSignature(initSignKeyHalfSig[:len(initSignKeyHalfSig)-1])
	if err != nil {
		return nil, err
	}
	kaslRecoveredScalar, err := c.lockTxEsig.RecoverTweak(initSignKeyHalfSigParsed)
	if err != nil {
		return nil, err
	}
	kaslRecoveredBytes := kaslRecoveredScalar.Bytes()
	kaslRecovered := secp256k1.PrivKeyFromBytes(kaslRecoveredBytes[:])

	partSpendKeyHalfRecovered, _, err := edwards.PrivKeyFromScalar(kaslRecovered.Serialize())
	if err != nil {
		return nil, fmt.Errorf("unable to recover partSpendKeyHalf: %v", err)
	}
	vkbsBig := scalarAdd(c.initSpendKeyHalf.GetD(), partSpendKeyHalfRecovered.GetD())
	vkbsBig.Mod(vkbsBig, curve.N)
	var vkbsBytes [32]byte
	vkbsBig.FillBytes(vkbsBytes[:])
	vkbs, _, err := edwards.PrivKeyFromScalar(vkbsBytes[:])
	if err != nil {
		return nil, fmt.Errorf("unable to create vkbs: %v", err)
	}

	var fullPubKey []byte
	fullPubKey = append(fullPubKey, vkbs.PubKey().Serialize()...)
	fullPubKey = append(fullPubKey, c.viewKey.PubKey().Serialize()...)
	walletAddr := base58.EncodeAddr(netTag, fullPubKey)
	walletFileName := fmt.Sprintf("%s_spend", walletAddr)

	var viewKeyBytes [32]byte
	copy(viewKeyBytes[:], c.viewKey.Serialize())

	reverse(&vkbsBytes)
	reverse(&viewKeyBytes)

	genReq := rpc.GenerateFromKeysRequest{
		Filename:      walletFileName,
		Address:       walletAddr,
		SpendKey:      hex.EncodeToString(vkbsBytes[:]),
		ViewKey:       hex.EncodeToString(viewKeyBytes[:]),
		RestoreHeight: restoreHeight,
	}

	xmrChecker, err := createNewXMRWallet(ctx, genReq)
	if err != nil {
		return nil, err
	}

	return xmrChecker, nil
}

// startRefund starts the refund and can be done by either party.
func (c *client) startRefund(ctx context.Context, initSignKeyHalfSig, partSignKeyHalfSig, lockTxScript []byte, refundTx *wire.MsgTx) error {
	refundSig, err := txscript.NewScriptBuilder().
		AddData(partSignKeyHalfSig).
		AddData(initSignKeyHalfSig).
		AddData(lockTxScript).
		Script()
	if err != nil {
		return err
	}

	refundTx.TxIn[0].SignatureScript = refundSig

	tx, err := c.dcr.SendRawTransaction(ctx, refundTx, false)
	if err != nil {
		return err
	}

	fmt.Println("Cancel Tx -", tx)

	return nil
}

// refundDcr returns dcr to bob while revealing his half of the xmr spend key.
func (c *initClient) refundDcr(ctx context.Context, spendRefundTx *wire.MsgTx, esig *adaptorsigs.AdaptorSignature, lockRefundTxScript []byte) (partSignKeyHalfSig []byte, err error) {
	kasf := secp256k1.PrivKeyFromBytes(c.initSpendKeyHalf.Serialize())
	decryptedSig, err := esig.Decrypt(&kasf.Key)
	if err != nil {
		return nil, err
	}
	partSignKeyHalfSig = decryptedSig.Serialize()
	partSignKeyHalfSig = append(partSignKeyHalfSig, byte(txscript.SigHashAll))

	initSignKeyHalfSig, err := sign.RawTxInSignature(spendRefundTx, 0, lockRefundTxScript, txscript.SigHashAll, c.initSignKeyHalf.Serialize(), dcrec.STSchnorrSecp256k1)
	if err != nil {
		return nil, err
	}

	refundSig, err := txscript.NewScriptBuilder().
		AddData(partSignKeyHalfSig).
		AddData(initSignKeyHalfSig).
		AddOp(txscript.OP_TRUE).
		AddData(lockRefundTxScript).
		Script()
	if err != nil {
		return nil, err
	}

	spendRefundTx.TxIn[0].SignatureScript = refundSig

	tx, err := c.dcr.SendRawTransaction(ctx, spendRefundTx, false)
	if err != nil {
		return nil, err
	}

	fmt.Println("Refund tx -", tx)

	// TODO: Confirm refund happened.
	return partSignKeyHalfSig, nil
}

// refundXmr refunds xmr but cannot happen without the dcr refund happening first.
func (c *partClient) refundXmr(ctx context.Context, partSignKeyHalfSig []byte, esig *adaptorsigs.AdaptorSignature, restoreHeight uint64) (*rpc.Client, error) {
	partSignKeyHalfSigParsed, err := schnorr.ParseSignature(partSignKeyHalfSig[:len(partSignKeyHalfSig)-1])
	if err != nil {
		return nil, err
	}
	initSpendKeyHalfRecoveredScalar, err := esig.RecoverTweak(partSignKeyHalfSigParsed)
	if err != nil {
		return nil, err
	}

	initSpendKeyHalfRecoveredBytes := initSpendKeyHalfRecoveredScalar.Bytes()
	initSpendKeyHalfRecovered := secp256k1.PrivKeyFromBytes(initSpendKeyHalfRecoveredBytes[:])

	kaslRecovered, _, err := edwards.PrivKeyFromScalar(initSpendKeyHalfRecovered.Serialize())
	if err != nil {
		return nil, fmt.Errorf("unable to recover kasl: %v", err)
	}
	vkbsBig := scalarAdd(c.partSpendKeyHalf.GetD(), kaslRecovered.GetD())
	vkbsBig.Mod(vkbsBig, curve.N)
	var vkbsBytes [32]byte
	vkbsBig.FillBytes(vkbsBytes[:])
	vkbs, _, err := edwards.PrivKeyFromScalar(vkbsBytes[:])
	if err != nil {
		return nil, fmt.Errorf("unable to create vkbs: %v", err)
	}

	var fullPubKey []byte
	fullPubKey = append(fullPubKey, vkbs.PubKey().Serialize()...)
	fullPubKey = append(fullPubKey, c.viewKey.PubKey().Serialize()...)
	walletAddr := base58.EncodeAddr(netTag, fullPubKey)
	walletFileName := fmt.Sprintf("%s_spend", walletAddr)

	var viewKeyBytes [32]byte
	copy(viewKeyBytes[:], c.viewKey.Serialize())

	reverse(&vkbsBytes)
	reverse(&viewKeyBytes)

	genReq := rpc.GenerateFromKeysRequest{
		Filename:      walletFileName,
		Address:       walletAddr,
		SpendKey:      hex.EncodeToString(vkbsBytes[:]),
		ViewKey:       hex.EncodeToString(viewKeyBytes[:]),
		RestoreHeight: restoreHeight,
	}

	xmrChecker, err := createNewXMRWallet(ctx, genReq)
	if err != nil {
		return nil, err
	}

	return xmrChecker, nil
}

// takeDcr is the punish if Bob takes too long. Alice gets the dcr while bob
// gets nothing.
func (c *partClient) takeDcr(ctx context.Context, lockRefundTxScript []byte, spendRefundTx *wire.MsgTx) (err error) {
	newAddr, err := c.dcr.GetNewAddress(ctx, "default")
	if err != nil {
		return err
	}
	p2AddrScriptVer, p2AddrScript := newAddr.PaymentScript()
	txOut := &wire.TxOut{
		Value:    dcrAmt - dumbFee - dumbFee,
		Version:  p2AddrScriptVer,
		PkScript: p2AddrScript,
	}
	spendRefundTx.TxOut[0] = txOut

	partSignKeyHalfSig, err := sign.RawTxInSignature(spendRefundTx, 0, lockRefundTxScript, txscript.SigHashAll, c.partSignKeyHalf.Serialize(), dcrec.STSchnorrSecp256k1)
	if err != nil {
		return err
	}
	refundSig, err := txscript.NewScriptBuilder().
		AddData(partSignKeyHalfSig).
		AddOp(txscript.OP_FALSE).
		AddData(lockRefundTxScript).
		Script()
	if err != nil {
		return err
	}

	spendRefundTx.TxIn[0].SignatureScript = refundSig

	_, err = c.dcr.SendRawTransaction(ctx, spendRefundTx, false)
	if err != nil {
		return err
	}
	// TODO: Confirm refund happened.
	return nil
}

func (c *client) waitDCR(ctx context.Context, startHeight int64) error {
	// Refund requires two blocks to be mined in tests.
	ticker := time.NewTicker(time.Second * 5)
	defer ticker.Stop()
	timeout := time.After(time.Minute * 30)
	i := 0
out:
	for {
		select {
		case <-ticker.C:
			_, height, err := c.dcr.GetBestBlock(ctx)
			if err != nil {
				return fmt.Errorf("undable to get best block: %v", err)
			}
			if height > startHeight+lockBlocks {
				break out
			}
			if i%25 == 0 {
				fmt.Println("Waiting for dcr blocks...")
			}
			i++
		case <-timeout:
			return errors.New("dcr timeout waiting for two blocks to be mined")
		case <-ctx.Done():
			return ctx.Err()
		}

	}
	return nil
}

func waitXMR(ctx context.Context, c *rpc.Client) (*rpc.GetBalanceResponse, error) {
	defer func() {
		if err := c.CloseWallet(ctx); err != nil {
			fmt.Printf("Error closing xmr wallet: %v\n", err)
		}
	}()

	var bal *rpc.GetBalanceResponse
	balReq := rpc.GetBalanceRequest{}
	ticker := time.NewTicker(time.Second * 5)
	defer ticker.Stop()
	timeout := time.After(time.Minute * 5)
	var err error
out:
	for {
		select {
		case <-ticker.C:
			bal, err = c.GetBalance(ctx, &balReq)
			if err != nil {
				return nil, err
			}
			if bal.Balance > 0 {
				break out
			}
		case <-timeout:
			return nil, errors.New("xmr wallet not synced after five minutes")
		case <-ctx.Done():
			return nil, ctx.Err()
		}

	}
	return bal, nil
}

// success is a successful trade.
func success(ctx context.Context) error {
	pc, err := newClient(ctx, alicexmr, alicedcr)
	if err != nil {
		return err
	}
	alice := partClient{client: pc}
	balReq := rpc.GetBalanceRequest{}
	xmrBal, err := alice.xmr.GetBalance(ctx, &balReq)
	if err != nil {
		return err
	}
	fmt.Printf("alice xmr balance\n%+v\n", *xmrBal)

	dcrBal, err := alice.dcr.GetBalance(ctx, "default")
	if err != nil {
		return err
	}
	dcrBeforeBal := toAtoms(dcrBal.Balances[0].Total)
	fmt.Printf("alice dcr balance %v\n", dcrBeforeBal)

	ic, err := newClient(ctx, bobxmr, bobdcr)
	if err != nil {
		return err
	}
	bob := initClient{client: ic}

	// Alice generates dleag.

	pubSpendKeyf, kbvf, pubPartSignKeyHalf, aliceDleag, err := alice.generateDleag(ctx)
	if err != nil {
		return err
	}

	// Bob generates transactions but does not send anything yet.

	_, lockRefundTxScript, lockTxScript, refundTx, spendRefundTx, vIn, pubSpendKey, viewKey, bobDleag, bDCRPK, err := bob.generateLockTxn(ctx, pubSpendKeyf, kbvf, pubPartSignKeyHalf, aliceDleag)
	if err != nil {
		return fmt.Errorf("unalbe to generate lock transactions: %v", err)
	}

	// Alice signs a refund script for Bob.

	_, _, err = alice.generateRefundSigs(refundTx, spendRefundTx, vIn, lockTxScript, lockRefundTxScript, bobDleag)
	if err != nil {
		return err
	}

	// Bob initializes the swap with dcr being sent.

	spendTx, err := bob.initDcr(ctx)
	if err != nil {
		return err
	}

	xmrRestoreHeightResp, err := alice.xmr.GetHeight(ctx)
	if err != nil {
		return err
	}

	// Alice inits her monero side.
	if err := alice.initXmr(ctx, viewKey, pubSpendKey); err != nil {
		return err
	}

	time.Sleep(time.Second * 5)

	// Bob sends esig after confirming on chain xmr tx.
	bobEsig, err := bob.sendLockTxSig(lockTxScript, spendTx)
	if err != nil {
		return err
	}

	// Alice redeems using the esig.
	initSignKeyHalfSig, err := alice.redeemDcr(ctx, bobEsig, lockTxScript, spendTx, bDCRPK)
	if err != nil {
		return err
	}

	// Prove that bob can't just sign the spend tx for the signature we need.
	ks, err := sign.RawTxInSignature(spendTx, 0, lockTxScript, txscript.SigHashAll, bob.initSignKeyHalf.Serialize(), dcrec.STSchnorrSecp256k1)
	if err != nil {
		return err
	}
	if bytes.Equal(ks, initSignKeyHalfSig) {
		return errors.New("bob was able to get the correct sig without alice")
	}

	// Bob redeems the xmr with the dcr signature.
	xmrChecker, err := bob.redeemXmr(ctx, initSignKeyHalfSig, xmrRestoreHeightResp.Height)
	if err != nil {
		return err
	}

	time.Sleep(time.Second * 5)

	xmrBal, err = waitXMR(ctx, xmrChecker)
	if err != nil {
		return err
	}

	if xmrBal.Balance != xmrAmt {
		return fmt.Errorf("expected redeem xmr balance of %d but got %d", xmrAmt, xmrBal.Balance)
	}

	dcrBal, err = alice.dcr.GetBalance(ctx, "default")
	if err != nil {
		return err
	}
	dcrAfterBal := toAtoms(dcrBal.Balances[0].Total)
	wantBal := dcrBeforeBal + dcrAmt - uint64(dumbFee)
	if wantBal != dcrAfterBal {
		return fmt.Errorf("expected alice balance to be %d but got %d", wantBal, dcrAfterBal)
	}

	return nil
}

// aliceBailsBeforeXmrInit is a trade that fails because alice does nothing after
// Bob inits.
func aliceBailsBeforeXmrInit(ctx context.Context) error {
	pc, err := newClient(ctx, alicexmr, alicedcr)
	if err != nil {
		return err
	}
	alice := partClient{client: pc}

	ic, err := newClient(ctx, alicexmr, alicedcr)
	if err != nil {
		return err
	}
	bob := initClient{client: ic}

	dcrBal, err := bob.dcr.GetBalance(ctx, "default")
	if err != nil {
		return err
	}
	dcrBeforeBal := toAtoms(dcrBal.Balances[0].Total)

	// Alice generates dleag.

	pubSpendKeyf, kbvf, pubPartSignKeyHalf, aliceDleag, err := alice.generateDleag(ctx)
	if err != nil {
		return err
	}

	// Bob generates transactions but does not send anything yet.

	bobRefundSig, lockRefundTxScript, lockTxScript, refundTx, spendRefundTx, vIn, _, _, bobDleag, _, err := bob.generateLockTxn(ctx, pubSpendKeyf, kbvf, pubPartSignKeyHalf, aliceDleag)
	if err != nil {
		return fmt.Errorf("unalbe to generate lock transactions: %v", err)
	}

	_, startHeight, err := bob.dcr.GetBestBlock(ctx)
	if err != nil {
		return fmt.Errorf("undable to get best block: %v", err)
	}

	// Alice signs a refund script for Bob.

	spendRefundESig, aliceRefundSig, err := alice.generateRefundSigs(refundTx, spendRefundTx, vIn, lockTxScript, lockRefundTxScript, bobDleag)
	if err != nil {
		return err
	}

	// Bob initializes the swap with dcr being sent.
	_, err = bob.initDcr(ctx)
	if err != nil {
		return err
	}

	time.Sleep(time.Second * 5)

	// Bob starts the refund.
	if err := bob.startRefund(ctx, bobRefundSig, aliceRefundSig, lockTxScript, refundTx); err != nil {
		return err
	}

	if err := bob.waitDCR(ctx, startHeight); err != nil {
		return err
	}

	// Bob refunds.
	_, err = bob.refundDcr(ctx, spendRefundTx, spendRefundESig, lockRefundTxScript)
	if err != nil {
		return err
	}

	time.Sleep(time.Second * 5)

	dcrBal, err = bob.dcr.GetBalance(ctx, "default")
	if err != nil {
		return err
	}

	var initFee uint64
	for _, input := range bob.lockTx.TxIn {
		initFee += uint64(input.ValueIn)
	}
	for _, output := range bob.lockTx.TxOut {
		initFee -= uint64(output.Value)
	}

	dcrAfterBal := toAtoms(dcrBal.Balances[0].Total)
	wantBal := dcrBeforeBal - initFee - uint64(dumbFee)*2
	if wantBal != dcrAfterBal {
		return fmt.Errorf("expected bob balance to be %d but got %d", wantBal, dcrAfterBal)
	}

	return nil
}

// refund is a failed trade where both parties have sent their initial funds and
// both get them back minus fees.
func refund(ctx context.Context) error {
	pc, err := newClient(ctx, alicexmr, alicedcr)
	if err != nil {
		return err
	}
	alice := partClient{client: pc}

	ic, err := newClient(ctx, bobxmr, bobdcr)
	if err != nil {
		return err
	}
	bob := initClient{client: ic}

	// Alice generates dleag.
	pubSpendKeyf, kbvf, pubPartSignKeyHalf, aliceDleag, err := alice.generateDleag(ctx)
	if err != nil {
		return err
	}

	// Bob generates transactions but does not send anything yet.
	bobRefundSig, lockRefundTxScript, lockTxScript, refundTx, spendRefundTx, vIn, pubSpendKey, viewKey, bobDleag, _, err := bob.generateLockTxn(ctx, pubSpendKeyf, kbvf, pubPartSignKeyHalf, aliceDleag)
	if err != nil {
		return fmt.Errorf("unalbe to generate lock transactions: %v", err)
	}

	_, startHeight, err := bob.dcr.GetBestBlock(ctx)
	if err != nil {
		return fmt.Errorf("undable to get best block: %v", err)
	}

	// Alice signs a refund script for Bob.
	spendRefundESig, aliceRefundSig, err := alice.generateRefundSigs(refundTx, spendRefundTx, vIn, lockTxScript, lockRefundTxScript, bobDleag)
	if err != nil {
		return err
	}

	// Bob initializes the swap with dcr being sent.
	_, err = bob.initDcr(ctx)
	if err != nil {
		return err
	}

	xmrRestoreHeightResp, err := alice.xmr.GetHeight(ctx)
	if err != nil {
		return err
	}

	// Alice inits her monero side.
	if err := alice.initXmr(ctx, viewKey, pubSpendKey); err != nil {
		return err
	}

	time.Sleep(time.Second * 5)

	// Bob starts the refund.
	if err := bob.startRefund(ctx, bobRefundSig, aliceRefundSig, lockTxScript, refundTx); err != nil {
		return err
	}

	if err := bob.waitDCR(ctx, startHeight); err != nil {
		return err
	}

	// Bob refunds.
	partSignKeyHalfSig, err := bob.refundDcr(ctx, spendRefundTx, spendRefundESig, lockRefundTxScript)
	if err != nil {
		return err
	}

	// Alice refunds.
	xmrChecker, err := alice.refundXmr(ctx, partSignKeyHalfSig, spendRefundESig, xmrRestoreHeightResp.Height)
	if err != nil {
		return err
	}

	time.Sleep(time.Second * 5)

	bal, err := waitXMR(ctx, xmrChecker)
	if err != nil {
		return err
	}

	if bal.Balance != xmrAmt {
		return fmt.Errorf("expected refund xmr balance of %d but got %d", xmrAmt, bal.Balance)
	}

	fmt.Printf("new xmr wallet balance\n%+v\n", *bal)

	return nil
}

// bobBailsAfterXmrInit is a failed trade where bob disappears after both parties
// init and alice takes all his dcr while losing her xmr. Bob gets nothing.
func bobBailsAfterXmrInit(ctx context.Context) error {
	pc, err := newClient(ctx, alicexmr, alicedcr)
	if err != nil {
		return err
	}
	alice := partClient{client: pc}

	ic, err := newClient(ctx, bobxmr, bobdcr)
	if err != nil {
		return err
	}
	bob := initClient{client: ic}

	dcrBal, err := alice.dcr.GetBalance(ctx, "default")
	if err != nil {
		return err
	}
	dcrBeforeBal := toAtoms(dcrBal.Balances[0].Total)

	// Alice generates dleag.

	pubSpendKeyf, kbvf, pubPartSignKeyHalf, aliceDleag, err := alice.generateDleag(ctx)
	if err != nil {
		return err
	}

	// Bob generates transactions but does not send anything yet.

	bobRefundSig, lockRefundTxScript, lockTxScript, refundTx, spendRefundTx, vIn, pubSpendKey, viewKey, bobDleag, _, err := bob.generateLockTxn(ctx, pubSpendKeyf, kbvf, pubPartSignKeyHalf, aliceDleag)
	if err != nil {
		return fmt.Errorf("unalbe to generate lock transactions: %v", err)
	}

	_, startHeight, err := bob.dcr.GetBestBlock(ctx)
	if err != nil {
		return fmt.Errorf("undable to get best block: %v", err)
	}

	// Alice signs a refund script for Bob.

	_, aliceRefundSig, err := alice.generateRefundSigs(refundTx, spendRefundTx, vIn, lockTxScript, lockRefundTxScript, bobDleag)
	if err != nil {
		return err
	}

	// Bob initializes the swap with dcr being sent.

	_, err = bob.initDcr(ctx)
	if err != nil {
		return err
	}

	// Alice inits her monero side.
	if err := alice.initXmr(ctx, viewKey, pubSpendKey); err != nil {
		return err
	}

	time.Sleep(time.Second * 5)

	// Alice starts the refund.
	if err := alice.startRefund(ctx, bobRefundSig, aliceRefundSig, lockTxScript, refundTx); err != nil {
		return err
	}

	if err := bob.waitDCR(ctx, startHeight); err != nil {
		return err
	}

	if err := alice.takeDcr(ctx, lockRefundTxScript, spendRefundTx); err != nil {
		return err
	}

	time.Sleep(time.Second * 5)

	dcrBal, err = alice.dcr.GetBalance(ctx, "default")
	if err != nil {
		return err
	}

	dcrAfterBal := toAtoms(dcrBal.Balances[0].Total)
	wantBal := dcrBeforeBal + dcrAmt - uint64(dumbFee)*2
	if wantBal != dcrAfterBal {
		return fmt.Errorf("expected alice balance to be %d but got %d", wantBal, dcrAfterBal)
	}
	return nil
}
