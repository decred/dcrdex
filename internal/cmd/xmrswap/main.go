package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/config"
	dcradaptor "decred.org/dcrdex/internal/adaptorsigs/dcr"
	"decred.org/dcrdex/internal/libsecp256k1"
	"decred.org/dcrwallet/v4/rpc/client/dcrwallet"
	dcrwalletjson "decred.org/dcrwallet/v4/rpc/jsonrpc/types"
	"github.com/agl/ed25519/edwards25519"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrec"
	"github.com/decred/dcrd/dcrec/edwards/v2"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/rpcclient/v8"
	"github.com/decred/dcrd/txscript/v4"
	"github.com/decred/dcrd/txscript/v4/sign"
	"github.com/decred/dcrd/txscript/v4/stdaddr"
	"github.com/decred/dcrd/wire"
	"github.com/decred/slog"
	"github.com/dev-warrior777/go-monero/rpc"
	"github.com/haven-protocol-org/monero-go-utils/base58"
)

// fieldIntSize is the size of a field element encoded
// as bytes.
const (
	fieldIntSize = 32
	dcrAmt       = 7_000_000 // atoms
	xmrAmt       = 1_000     // 1e12 units
	dumbFee      = int64(6000)
)

var (
	homeDir    = os.Getenv("HOME")
	dextestDir = filepath.Join(homeDir, "dextest")
	bobDir     = filepath.Join(dextestDir, "xmr", "wallets", "bob")
	curve      = edwards.Edwards()
)

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

	kbvf, kbsf, kbvl, kbsl, vkbv *edwards.PrivateKey
	pkbsf, pkbv, pkbs            *edwards.PublicKey
	kaf, kal                     *secp256k1.PrivateKey
	pkal, pkaf, pkasl            *secp256k1.PublicKey
	dleag                        [libsecp256k1.ProofLen]byte
	lockTxEsig                   [libsecp256k1.CTLen]byte
	lockTx                       *wire.MsgTx
	vIn                          int
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

func newClient(ctx context.Context, xmrAddr, dcrNode string) (*client, error) {
	xmr := rpc.New(rpc.Config{
		Address: xmrAddr,
		Client:  &http.Client{},
	})

	settings, err := config.Parse(filepath.Join(dextestDir, "dcr", dcrNode, fmt.Sprintf("%s.conf", dcrNode)))
	if err != nil {
		return nil, err
	}
	settings["account"] = "default"

	dcr, err := newRPCWallet(settings, dex.StdOutLogger(dcrNode, slog.LevelTrace), dex.Simnet)
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
		Address: "http://127.0.0.1:28484/json_rpc",
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
func run(ctx context.Context) error {
	return runSuccess(ctx)
}

func (c *client) generateDleag() (pkbsf *edwards.PublicKey, kbvf *edwards.PrivateKey, pkaf *secp256k1.PublicKey, dleag [libsecp256k1.ProofLen]byte, err error) {
	fail := func(err error) (*edwards.PublicKey, *edwards.PrivateKey, *secp256k1.PublicKey, [libsecp256k1.ProofLen]byte, error) {
		return nil, nil, nil, [libsecp256k1.ProofLen]byte{}, err
	}
	// This private key is shared with bob.
	c.kbvf, err = edwards.GeneratePrivateKey()
	if err != nil {
		return fail(err)
	}

	// This private key becomes the proof. Not shared.
	c.kbsf, err = edwards.GeneratePrivateKey()
	if err != nil {
		return fail(err)
	}
	c.pkbsf = c.kbsf.PubKey()

	c.kaf, err = secp256k1.GeneratePrivateKey()
	if err != nil {
		return fail(err)
	}

	// Share this pubkey with the other party.
	c.pkaf = c.kaf.PubKey()

	// Share dleag with bob.
	c.dleag, err = libsecp256k1.Ed25519DleagProve(c.kbsf)
	if err != nil {
		return fail(err)
	}

	c.pkasl, err = secp256k1.ParsePubKey(c.dleag[:33])
	if err != nil {
		return fail(err)
	}

	return c.pkbsf, c.kbvf, c.pkaf, c.dleag, nil
}

func (c *client) generateLockTxn(ctx context.Context, pkbsf *edwards.PublicKey, kbvf *edwards.PrivateKey, pkaf *secp256k1.PublicKey, dleag [libsecp256k1.ProofLen]byte) (refundSig, p2shLockScript, lockTxScript []byte, refundTx *wire.MsgTx, lockTxVout int, pkbv, pkbs *edwards.PublicKey, err error) {
	fail := func(err error) ([]byte, []byte, []byte, *wire.MsgTx, int, *edwards.PublicKey, *edwards.PublicKey, error) {
		return nil, nil, nil, nil, 0, nil, nil, err
	}
	c.dleag = dleag
	c.pkasl, err = secp256k1.ParsePubKey(c.dleag[:33])
	if err != nil {
		return fail(err)
	}
	c.kbvf = kbvf
	c.pkbsf = pkbsf
	c.pkaf = pkaf
	c.kbvl, err = edwards.GeneratePrivateKey()
	if err != nil {
		return fail(err)
	}

	c.kbsl, err = edwards.GeneratePrivateKey()
	if err != nil {
		return fail(err)
	}

	c.kal, err = secp256k1.GeneratePrivateKey()
	if err != nil {
		return fail(err)
	}

	pkal := c.kal.PubKey()

	vkbvBig := scalarAdd(c.kbvf.GetD(), c.kbvl.GetD())
	vkbvBig.Mod(vkbvBig, curve.N)
	var vkbvBytes [32]byte
	vkbvBig.FillBytes(vkbvBytes[:])
	c.vkbv, _, err = edwards.PrivKeyFromScalar(vkbvBytes[:])
	if err != nil {
		return fail(fmt.Errorf("unable to create vkbv: %v", err))
	}

	c.pkbv = sumPubKeys(c.kbvl.PubKey(), c.kbvf.PubKey())
	c.pkbs = sumPubKeys(c.kbsl.PubKey(), c.pkbsf)

	lockTxScript, err = dcradaptor.LockTxScript(pkal.SerializeCompressed(), c.pkaf.SerializeCompressed())
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
	c.lockTx = wire.NewMsgTx()
	c.lockTx.AddTxOut(txOut)
	txBytes, err := c.lockTx.Bytes()
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

	durationLocktime := int64(30) // seconds
	durationLocktime &= wire.SequenceLockTimeIsSeconds
	lockRefundTxScript, err := dcradaptor.LockRefundTxScript(pkal.SerializeCompressed(), c.pkaf.SerializeCompressed(), durationLocktime)
	if err != nil {
		return fail(err)
	}

	scriptAddr, err = stdaddr.NewAddressScriptHashV0(lockRefundTxScript, c.dcr.chainParams)
	if err != nil {
		return fail(fmt.Errorf("error encoding script address: %w", err))
	}
	p2shScriptVer, p2shScript := scriptAddr.PaymentScript()
	// Add the transaction output.
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
	txIn.Sequence = uint32(durationLocktime)
	refundTx.AddTxIn(txIn)

	// This sig must be shared with Alice.
	refundSigBob, err := sign.SignatureScript(refundTx, c.vIn, p2shLockScript, txscript.SigHashAll, c.kal.Serialize(), dcrec.STEcdsaSecp256k1, true)
	if err != nil {
		return fail(err)
	}

	newAddr, err := c.dcr.GetNewAddress(ctx, "default")
	if err != nil {
		return fail(err)
	}
	p2AddrScriptVer, p2AddrScript := newAddr.PaymentScript()
	// Add the transaction output.
	txOut = &wire.TxOut{
		Value:    dcrAmt - dumbFee,
		Version:  p2AddrScriptVer,
		PkScript: p2AddrScript,
	}
	spendRefundTx := wire.NewMsgTx()
	spendRefundTx.AddTxOut(txOut)
	h = refundTx.TxHash()
	op = wire.NewOutPoint(&h, 0, 0)
	txIn = wire.NewTxIn(op, dcrAmt, nil)
	spendRefundTx.AddTxIn(txIn)
	return refundSigBob, p2shLockScript, lockTxScript, refundTx, c.vIn, c.pkbv, c.pkbs, nil
}

func (c *client) generateRefundSigs(refundTx *wire.MsgTx, vIn int, p2shLockScript []byte) (esig [libsecp256k1.CTLen]byte, refundSig []byte, err error) {
	c.vIn = vIn
	fail := func(err error) ([libsecp256k1.CTLen]byte, []byte, error) {
		return [libsecp256k1.CTLen]byte{}, nil, err
	}
	refundTxHash := refundTx.TxHash()

	esig, err = libsecp256k1.EcdsaotvesEncSign(c.kaf, c.pkasl, refundTxHash)
	if err != nil {
		return fail(err)
	}

	// Share with bob.
	refundSig, err = sign.SignatureScript(refundTx, c.vIn, p2shLockScript, txscript.SigHashAll, c.kaf.Serialize(), dcrec.STEcdsaSecp256k1, true)
	if err != nil {
		return fail(err)
	}
	return esig, refundSig, nil
}

func (c *client) initDcr(ctx context.Context) (spendTx *wire.MsgTx, err error) {
	fail := func(err error) (*wire.MsgTx, error) {
		return nil, err
	}
	pkaslAddr, err := stdaddr.NewAddressPubKeyEcdsaSecp256k1(0, c.pkasl, c.dcr.chainParams)
	if err != nil {
		return fail(err)
	}
	p2AddrScriptVer, p2AddrScript := pkaslAddr.PaymentScript()

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
		return fail(fmt.Errorf("unalbe to send lock tx: %v"))
	}
	return spendTx, nil
}

func (c *client) initXmr(ctx context.Context, pkbv, pkbs *edwards.PublicKey) error {
	c.pkbv = pkbv
	c.pkbs = pkbs
	var fullPubKey []byte
	fullPubKey = append(fullPubKey, c.pkbs.SerializeCompressed()...)
	fullPubKey = append(fullPubKey, c.pkbv.SerializeCompressed()...)

	sharedAddr := base58.EncodeAddr(18, fullPubKey)

	dest := rpc.Destination{
		Amount:  xmrAmt,
		Address: string(sharedAddr),
	}
	sendReq := rpc.TransferRequest{
		Destinations: []rpc.Destination{dest},
	}

	sendRes, err := c.xmr.Transfer(ctx, &sendReq)
	if err != nil {
		return err
	}
	fmt.Printf("xmr sent\n%+v\n", *sendRes)
	return nil
}

func (c *client) sendLockTxSig(lockTxScript []byte, spendTx *wire.MsgTx) (esig [libsecp256k1.CTLen]byte, err error) {
	hash, err := txscript.CalcSignatureHash(lockTxScript, txscript.SigHashAll, spendTx, 0, nil)
	if err != nil {
		return [libsecp256k1.CTLen]byte{}, err
	}

	var h chainhash.Hash
	copy(h[:], hash)

	esig, err = libsecp256k1.EcdsaotvesEncSign(c.kal, c.pkasl, h)
	if err != nil {
		return [libsecp256k1.CTLen]byte{}, err
	}
	c.lockTxEsig = esig
	return esig, nil
}

func (c *client) redeemDCR(ctx context.Context, esig [libsecp256k1.CTLen]byte, lockTxScript []byte, spendTx *wire.MsgTx) (kalSig []byte, err error) {
	kasl := secp256k1.PrivKeyFromBytes(c.kbsf.Serialize())
	kalSig, err = libsecp256k1.EcdsaotvesDecSig(kasl, esig)
	if err != nil {
		return nil, err
	}
	kalSig = append(kalSig, byte(txscript.SigHashAll))

	kafSig, err := sign.RawTxInSignature(spendTx, 0, lockTxScript, txscript.SigHashAll, c.kaf.Serialize(), dcrec.STEcdsaSecp256k1)
	if err != nil {
		return nil, err
	}

	spendSig, err := txscript.NewScriptBuilder().
		AddData(kalSig).
		AddData(kafSig).
		AddData(lockTxScript).
		Script()

	spendTx.TxIn[0].SignatureScript = spendSig

	time.Sleep(time.Second * 5)

	_, err = c.dcr.SendRawTransaction(ctx, spendTx, false)
	if err != nil {
		return nil, err
	}
	// TODO: Check balance changed as expected.

	return kalSig, nil
}

func (c *client) redeemXmr(ctx context.Context, kalSig []byte) error {
	kaslRecovered, err := libsecp256k1.EcdsaotvesRecEncKey(c.pkasl, c.lockTxEsig, kalSig[:len(kalSig)-1])
	if err != nil {
		return err
	}

	kbsfRecovered, _, err := edwards.PrivKeyFromScalar(kaslRecovered.Serialize())
	if err != nil {
		return fmt.Errorf("unable to recover kbsf: %v", err)
	}
	vkbsBig := scalarAdd(kbsfRecovered.GetD(), c.kbsl.GetD())
	vkbsBig.Mod(vkbsBig, curve.N)
	var vkbsBytes [32]byte
	vkbsBig.FillBytes(vkbsBytes[:])
	vkbs, _, err := edwards.PrivKeyFromScalar(vkbsBytes[:])
	if err != nil {
		return fmt.Errorf("unable to create vkbs: %v", err)
	}

	var fullPubKey []byte
	fullPubKey = append(fullPubKey, vkbs.PubKey().Serialize()...)
	fullPubKey = append(fullPubKey, c.vkbv.PubKey().Serialize()...)
	walletAddr := base58.EncodeAddr(18, fullPubKey)
	walletFileName := fmt.Sprintf("%s_spend", walletAddr)

	var vkbvBytes [32]byte
	copy(vkbvBytes[:], c.vkbv.Serialize())

	reverse(&vkbsBytes)
	reverse(&vkbvBytes)

	genReq := rpc.GenerateFromKeysRequest{
		Filename: walletFileName,
		Address:  walletAddr,
		SpendKey: hex.EncodeToString(vkbsBytes[:]),
		ViewKey:  hex.EncodeToString(vkbvBytes[:]),
	}

	xmrChecker, err := createNewXMRWallet(ctx, genReq)
	if err != nil {
		return err
	}

	time.Sleep(15 * time.Second)

	balReq := rpc.GetBalanceRequest{}
	balRes, err := xmrChecker.GetBalance(ctx, &balReq)
	if err != nil {
		return err
	}
	if balRes.Balance != xmrAmt {
		return fmt.Errorf("expected redeem xmr balance of %d but got %d", xmrAmt, balRes.Balance)
	}
	fmt.Printf("new xmr wallet balance\n%+v\n", *balRes)
	return nil
}

func runSuccess(ctx context.Context) error {
	alice, err := newClient(ctx, "http://127.0.0.1:28284/json_rpc", "alpha")
	if err != nil {
		return err
	}
	balReq := rpc.GetBalanceRequest{
		AccountIndex: 0,
	}
	balRes, err := alice.xmr.GetBalance(ctx, &balReq)
	if err != nil {
		return err
	}
	fmt.Printf("alice xmr balance\n%+v\n", *balRes)

	bob, err := newClient(ctx, "http://127.0.0.1:28184/json_rpc", "trading1")
	if err != nil {
		return err
	}

	// Alice generates dleag.

	pkbsf, kbvf, pkaf, dleag, err := alice.generateDleag()
	if err != nil {
		return err
	}

	// Bob generates transactions but does not send anything yet.

	_, p2shLockScript, lockTxScript, refundTx, vIn, pkbv, pkbs, err := bob.generateLockTxn(ctx, pkbsf, kbvf, pkaf, dleag)
	if err != nil {
		return fmt.Errorf("unalbe to generate lock transactions: %v", err)
	}

	// Alice signs a refund script for Bob.

	_, _, err = alice.generateRefundSigs(refundTx, vIn, p2shLockScript)
	if err != nil {
		return err
	}

	// Bob initializes the swap with dcr being sent.

	spendTx, err := bob.initDcr(ctx)
	if err != nil {
		return err
	}

	// Alice inits her monero side.
	if err := alice.initXmr(ctx, pkbv, pkbs); err != nil {
		return err
	}

	// Bob sends esig after confirming on chain xmr tx.

	bobEsig, err := bob.sendLockTxSig(lockTxScript, spendTx)
	if err != nil {
		return err
	}

	// Alice redeems using the esig.
	kalSig, err := alice.redeemDCR(ctx, bobEsig, lockTxScript, spendTx)
	if err != nil {
		return err
	}

	// Bob redeems the xmr with the dcr signature.
	if err := bob.redeemXmr(ctx, kalSig); err != nil {
		return err
	}
	return nil
}
