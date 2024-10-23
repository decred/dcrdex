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
	pkbsf, pkbs                  *edwards.PublicKey
	kaf, kal                     *secp256k1.PrivateKey
	pkal, pkaf, pkasl, pkbsl     *secp256k1.PublicKey
	kbsfDleag, kbslDleag         []byte
	lockTxEsig                   *adaptorsigs.AdaptorSignature
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

type prettyLogger struct {
	c *color.Color
}

func (cl prettyLogger) Write(p []byte) (n int, err error) {
	return cl.c.Fprint(os.Stdout, string(p))
}

func run(ctx context.Context) error {
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

// generateDleag starts the trade by creating some keys.
func (c *client) generateDleag(ctx context.Context) (pkbsf *edwards.PublicKey, kbvf *edwards.PrivateKey,
	pkaf *secp256k1.PublicKey, dleag []byte, err error) {
	fail := func(err error) (*edwards.PublicKey, *edwards.PrivateKey,
		*secp256k1.PublicKey, []byte, error) {
		return nil, nil, nil, nil, err
	}
	// This private key is shared with bob and becomes half of the view key.
	c.kbvf, err = edwards.GeneratePrivateKey()
	if err != nil {
		return fail(err)
	}

	// Not shared. Becomes half the spend key. The pubkey is shared.
	c.kbsf, err = edwards.GeneratePrivateKey()
	if err != nil {
		return fail(err)
	}
	c.pkbsf = c.kbsf.PubKey()

	// Not shared. This is used for all dcr signatures. Using a wallet
	// address because funds may go here in the case of success. Any address
	// would work for the spendTx though.
	kafAddr, err := c.dcr.GetNewAddress(ctx, "default")
	if err != nil {
		return fail(err)
	}
	kafWIF, err := c.dcr.DumpPrivKey(ctx, kafAddr)
	if err != nil {
		return fail(err)
	}
	c.kaf = secp256k1.PrivKeyFromBytes(kafWIF.PrivKey())

	// Share this pubkey with the other party.
	c.pkaf = c.kaf.PubKey()

	c.kbsfDleag, err = adaptorsigs.ProveDLEQ(c.kbsf.Serialize())
	if err != nil {
		return fail(err)
	}

	c.pkasl, err = adaptorsigs.ExtractSecp256k1PubKeyFromProof(c.kbsfDleag)
	if err != nil {
		return fail(err)
	}

	return c.pkbsf, c.kbvf, c.pkaf, c.kbsfDleag, nil
}

// generateLockTxn creates even more keys and some transactions.
func (c *client) generateLockTxn(ctx context.Context, pkbsf *edwards.PublicKey,
	kbvf *edwards.PrivateKey, pkaf *secp256k1.PublicKey, kbsfDleag []byte) (refundSig,
	lockRefundTxScript, lockTxScript []byte, refundTx, spendRefundTx *wire.MsgTx, lockTxVout int,
	pkbs *edwards.PublicKey, vkbv *edwards.PrivateKey, dleag []byte, bdcrpk *secp256k1.PublicKey, err error) {

	fail := func(err error) ([]byte, []byte, []byte, *wire.MsgTx, *wire.MsgTx, int, *edwards.PublicKey, *edwards.PrivateKey, []byte, *secp256k1.PublicKey, error) {
		return nil, nil, nil, nil, nil, 0, nil, nil, nil, nil, err
	}
	c.kbsfDleag = kbsfDleag
	c.pkasl, err = adaptorsigs.ExtractSecp256k1PubKeyFromProof(c.kbsfDleag)
	if err != nil {
		return fail(err)
	}
	c.kbvf = kbvf
	c.pkbsf = pkbsf
	c.pkaf = pkaf

	// This becomes the other half of the view key.
	c.kbvl, err = edwards.GeneratePrivateKey()
	if err != nil {
		return fail(err)
	}

	// This becomes the other half of the spend key and is shared.
	c.kbsl, err = edwards.GeneratePrivateKey()
	if err != nil {
		return fail(err)
	}

	// This kept private. This is used for all dcr signatures.
	c.kal, err = secp256k1.GeneratePrivateKey()
	if err != nil {
		return fail(err)
	}

	pkal := c.kal.PubKey()

	// This is the full xmr view key and is shared. Alice can also calculate
	// it using kbvl.
	vkbvBig := scalarAdd(c.kbvf.GetD(), c.kbvl.GetD())
	vkbvBig.Mod(vkbvBig, curve.N)
	var vkbvBytes [32]byte
	vkbvBig.FillBytes(vkbvBytes[:])
	c.vkbv, _, err = edwards.PrivKeyFromScalar(vkbvBytes[:])
	if err != nil {
		return fail(fmt.Errorf("unable to create vkbv: %v", err))
	}

	// The public key for the xmr spend key. No party knows the full private
	// key yet.
	c.pkbs = sumPubKeys(c.kbsl.PubKey(), c.pkbsf)

	// The lock tx is the initial dcr transaction.
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

	durationLocktime := int64(2) // blocks
	// Unable to use time for tests as this is multiples of 512 seconds.
	// durationLocktime := int64(10) // seconds * 512
	// durationLocktime |= wire.SequenceLockTimeIsSeconds

	// The refund tx does not outright refund but moves funds to the refund
	// script's address. This is signed by both parties before the initial tx.
	lockRefundTxScript, err = dcradaptor.LockRefundTxScript(pkal.SerializeCompressed(), c.pkaf.SerializeCompressed(), durationLocktime)
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
	refundSig, err = sign.RawTxInSignature(refundTx, c.vIn, lockTxScript, txscript.SigHashAll, c.kal.Serialize(), dcrec.STSchnorrSecp256k1)
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

	c.kbslDleag, err = adaptorsigs.ProveDLEQ(c.kbsl.Serialize())
	if err != nil {
		return fail(err)
	}
	c.pkbsl, err = adaptorsigs.ExtractSecp256k1PubKeyFromProof(c.kbslDleag)
	if err != nil {
		return fail(err)
	}

	return refundSig, lockRefundTxScript, lockTxScript, refundTx, spendRefundTx, c.vIn, c.pkbs, c.vkbv, c.kbslDleag, pkal, nil
}

// generateRefundSigs signs the refund tx and shares the spendRefund esig that
// allows bob to spend the refund tx.
func (c *client) generateRefundSigs(refundTx, spendRefundTx *wire.MsgTx, vIn int, lockTxScript, lockRefundTxScript []byte, dleag []byte) (esig *adaptorsigs.AdaptorSignature, refundSig []byte, err error) {
	fail := func(err error) (*adaptorsigs.AdaptorSignature, []byte, error) {
		return nil, nil, err
	}
	c.kbslDleag = dleag
	c.vIn = vIn
	c.pkbsl, err = adaptorsigs.ExtractSecp256k1PubKeyFromProof(c.kbslDleag)
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
	c.pkbsl.AsJacobian(jacobianBobPubKey)
	esig, err = adaptorsigs.PublicKeyTweakedAdaptorSig(c.kaf, h[:], jacobianBobPubKey)
	if err != nil {
		return fail(err)
	}

	// Share with bob.
	refundSig, err = sign.RawTxInSignature(refundTx, c.vIn, lockTxScript, txscript.SigHashAll, c.kaf.Serialize(), dcrec.STSchnorrSecp256k1)
	if err != nil {
		return fail(err)
	}

	return esig, refundSig, nil
}

// initDcr is the first transaction to happen and creates a dcr transaction.
func (c *client) initDcr(ctx context.Context) (spendTx *wire.MsgTx, err error) {
	fail := func(err error) (*wire.MsgTx, error) {
		return nil, err
	}
	pkaslAddr, err := stdaddr.NewAddressPubKeyHashEcdsaSecp256k1(0, stdaddr.Hash160(c.pkaf.SerializeCompressed()), c.dcr.chainParams)
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
		return fail(fmt.Errorf("unable to send lock tx: %v", err))
	}

	return spendTx, nil
}

// initXmr sends an xmr transaciton. Alice can only do this after confirming the
// dcr transaction.
func (c *client) initXmr(ctx context.Context, vkbv *edwards.PrivateKey, pkbs *edwards.PublicKey) error {
	c.vkbv = vkbv
	c.pkbs = pkbs
	var fullPubKey []byte
	fullPubKey = append(fullPubKey, c.pkbs.SerializeCompressed()...)
	fullPubKey = append(fullPubKey, c.vkbv.PubKey().SerializeCompressed()...)

	sharedAddr := base58.EncodeAddr(18, fullPubKey)

	dest := rpc.Destination{
		Amount:  xmrAmt,
		Address: sharedAddr,
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

// sendLockTxSig allows Alice to redeem the dcr. If bob does not send this alice
// can eventually take his btc. Otherwise bob refunding will reveal his half of
// the xmr spend key allowing Alice to refund.
func (c *client) sendLockTxSig(lockTxScript []byte, spendTx *wire.MsgTx) (esig *adaptorsigs.AdaptorSignature, err error) {
	hash, err := txscript.CalcSignatureHash(lockTxScript, txscript.SigHashAll, spendTx, 0, nil)
	if err != nil {
		return nil, err
	}

	var h chainhash.Hash
	copy(h[:], hash)

	jacobianAlicePubKey := new(secp256k1.JacobianPoint)
	c.pkasl.AsJacobian(jacobianAlicePubKey)
	esig, err = adaptorsigs.PublicKeyTweakedAdaptorSig(c.kal, h[:], jacobianAlicePubKey)
	if err != nil {
		return nil, err
	}

	c.lockTxEsig = esig

	return esig, nil
}

// redeemDcr redeems the dcr, revealing a signature that reveals half of the xmr
// spend key.
func (c *client) redeemDcr(ctx context.Context, esig *adaptorsigs.AdaptorSignature, lockTxScript []byte, spendTx *wire.MsgTx, bobDCRPK *secp256k1.PublicKey) (kalSig []byte, err error) {
	kasl := secp256k1.PrivKeyFromBytes(c.kbsf.Serialize())

	kalSigShnorr, err := esig.Decrypt(&kasl.Key)
	if err != nil {
		return nil, err
	}
	kalSig = kalSigShnorr.Serialize()
	kalSig = append(kalSig, byte(txscript.SigHashAll))

	kafSig, err := sign.RawTxInSignature(spendTx, 0, lockTxScript, txscript.SigHashAll, c.kaf.Serialize(), dcrec.STSchnorrSecp256k1)
	if err != nil {
		return nil, err
	}

	spendSig, err := txscript.NewScriptBuilder().
		AddData(kafSig).
		AddData(kalSig).
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

	return kalSig, nil
}

// redeemXmr redeems xmr by creating a new xmr wallet with the complete spend
// and view private keys.
func (c *client) redeemXmr(ctx context.Context, kalSig []byte) (*rpc.Client, error) {
	kalSigParsed, err := schnorr.ParseSignature(kalSig[:len(kalSig)-1])
	if err != nil {
		return nil, err
	}
	kaslRecoveredScalar, err := c.lockTxEsig.RecoverTweak(kalSigParsed)
	if err != nil {
		return nil, err
	}
	kaslRecoveredBytes := kaslRecoveredScalar.Bytes()
	kaslRecovered := secp256k1.PrivKeyFromBytes(kaslRecoveredBytes[:])

	kbsfRecovered, _, err := edwards.PrivKeyFromScalar(kaslRecovered.Serialize())
	if err != nil {
		return nil, fmt.Errorf("unable to recover kbsf: %v", err)
	}
	vkbsBig := scalarAdd(c.kbsl.GetD(), kbsfRecovered.GetD())
	vkbsBig.Mod(vkbsBig, curve.N)
	var vkbsBytes [32]byte
	vkbsBig.FillBytes(vkbsBytes[:])
	vkbs, _, err := edwards.PrivKeyFromScalar(vkbsBytes[:])
	if err != nil {
		return nil, fmt.Errorf("unable to create vkbs: %v", err)
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
		return nil, err
	}

	return xmrChecker, nil
}

// startRefund starts the refund and can be done by either party.
func (c *client) startRefund(ctx context.Context, kalSig, kafSig, lockTxScript []byte, refundTx *wire.MsgTx) error {
	refundSig, err := txscript.NewScriptBuilder().
		AddData(kafSig).
		AddData(kalSig).
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
func (c *client) refundDcr(ctx context.Context, spendRefundTx *wire.MsgTx, esig *adaptorsigs.AdaptorSignature, lockRefundTxScript []byte) (kafSig []byte, err error) {
	kasf := secp256k1.PrivKeyFromBytes(c.kbsl.Serialize())
	decryptedSig, err := esig.Decrypt(&kasf.Key)
	if err != nil {
		return nil, err
	}
	kafSig = decryptedSig.Serialize()
	kafSig = append(kafSig, byte(txscript.SigHashAll))

	kalSig, err := sign.RawTxInSignature(spendRefundTx, 0, lockRefundTxScript, txscript.SigHashAll, c.kal.Serialize(), dcrec.STSchnorrSecp256k1)
	if err != nil {
		return nil, err
	}

	refundSig, err := txscript.NewScriptBuilder().
		AddData(kafSig).
		AddData(kalSig).
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
	return kafSig, nil
}

// refundXmr refunds xmr but cannot happen without the dcr refund happening first.
func (c *client) refundXmr(ctx context.Context, kafSig []byte, esig *adaptorsigs.AdaptorSignature) (*rpc.Client, error) {
	kafSigParsed, err := schnorr.ParseSignature(kafSig[:len(kafSig)-1])
	if err != nil {
		return nil, err
	}
	kbslRecoveredScalar, err := esig.RecoverTweak(kafSigParsed)
	if err != nil {
		return nil, err
	}

	kbslRecoveredBytes := kbslRecoveredScalar.Bytes()
	kbslRecovered := secp256k1.PrivKeyFromBytes(kbslRecoveredBytes[:])

	kaslRecovered, _, err := edwards.PrivKeyFromScalar(kbslRecovered.Serialize())
	if err != nil {
		return nil, fmt.Errorf("unable to recover kasl: %v", err)
	}
	vkbsBig := scalarAdd(c.kbsf.GetD(), kaslRecovered.GetD())
	vkbsBig.Mod(vkbsBig, curve.N)
	var vkbsBytes [32]byte
	vkbsBig.FillBytes(vkbsBytes[:])
	vkbs, _, err := edwards.PrivKeyFromScalar(vkbsBytes[:])
	if err != nil {
		return nil, fmt.Errorf("unable to create vkbs: %v", err)
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
		return nil, err
	}

	return xmrChecker, nil
}

// takeDcr is the punish if Bob takes too long. Alice gets the dcr while bob
// gets nothing.
func (c *client) takeDcr(ctx context.Context, lockRefundTxScript []byte, spendRefundTx *wire.MsgTx) (err error) {
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

	kafSig, err := sign.RawTxInSignature(spendRefundTx, 0, lockRefundTxScript, txscript.SigHashAll, c.kaf.Serialize(), dcrec.STSchnorrSecp256k1)
	if err != nil {
		return err
	}
	refundSig, err := txscript.NewScriptBuilder().
		AddData(kafSig).
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

// success is a successful trade.
func success(ctx context.Context) error {
	alice, err := newClient(ctx, "http://127.0.0.1:28284/json_rpc", "trading1")
	if err != nil {
		return err
	}
	balReq := rpc.GetBalanceRequest{
		AccountIndex: 0,
	}
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

	bob, err := newClient(ctx, "http://127.0.0.1:28184/json_rpc", "trading2")
	if err != nil {
		return err
	}

	// Alice generates dleag.

	pkbsf, kbvf, pkaf, aliceDleag, err := alice.generateDleag(ctx)
	if err != nil {
		return err
	}

	// Bob generates transactions but does not send anything yet.

	_, lockRefundTxScript, lockTxScript, refundTx, spendRefundTx, vIn, pkbs, vkbv, bobDleag, bDCRPK, err := bob.generateLockTxn(ctx, pkbsf, kbvf, pkaf, aliceDleag)
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

	// Alice inits her monero side.
	if err := alice.initXmr(ctx, vkbv, pkbs); err != nil {
		return err
	}

	time.Sleep(time.Second * 5)

	// Bob sends esig after confirming on chain xmr tx.
	bobEsig, err := bob.sendLockTxSig(lockTxScript, spendTx)
	if err != nil {
		return err
	}

	// Alice redeems using the esig.
	kalSig, err := alice.redeemDcr(ctx, bobEsig, lockTxScript, spendTx, bDCRPK)
	if err != nil {
		return err
	}

	// Prove that bob can't just sign the spend tx for the signature we need.
	ks, err := sign.RawTxInSignature(spendTx, 0, lockTxScript, txscript.SigHashAll, bob.kal.Serialize(), dcrec.STSchnorrSecp256k1)
	if err != nil {
		return err
	}
	if bytes.Equal(ks, kalSig) {
		return errors.New("bob was able to get the correct sig without alice")
	}

	// Bob redeems the xmr with the dcr signature.
	xmrChecker, err := bob.redeemXmr(ctx, kalSig)
	if err != nil {
		return err
	}

	// NOTE: This wallet must sync so may take a long time on mainnet.
	// TODO: Wait for wallet sync rather than a dumb sleep.
	time.Sleep(time.Second * 40)

	xmrBal, err = xmrChecker.GetBalance(ctx, &balReq)
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
	alice, err := newClient(ctx, "http://127.0.0.1:28284/json_rpc", "trading1")
	if err != nil {
		return err
	}

	bob, err := newClient(ctx, "http://127.0.0.1:28184/json_rpc", "trading2")
	if err != nil {
		return err
	}

	dcrBal, err := bob.dcr.GetBalance(ctx, "default")
	if err != nil {
		return err
	}
	dcrBeforeBal := toAtoms(dcrBal.Balances[0].Total)

	// Alice generates dleag.

	pkbsf, kbvf, pkaf, aliceDleag, err := alice.generateDleag(ctx)
	if err != nil {
		return err
	}

	// Bob generates transactions but does not send anything yet.

	bobRefundSig, lockRefundTxScript, lockTxScript, refundTx, spendRefundTx, vIn, _, _, bobDleag, _, err := bob.generateLockTxn(ctx, pkbsf, kbvf, pkaf, aliceDleag)
	if err != nil {
		return fmt.Errorf("unalbe to generate lock transactions: %v", err)
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

	time.Sleep(time.Second * 5)

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
	alice, err := newClient(ctx, "http://127.0.0.1:28284/json_rpc", "trading1")
	if err != nil {
		return err
	}

	bob, err := newClient(ctx, "http://127.0.0.1:28184/json_rpc", "trading2")
	if err != nil {
		return err
	}

	// Alice generates dleag.
	pkbsf, kbvf, pkaf, aliceDleag, err := alice.generateDleag(ctx)
	if err != nil {
		return err
	}

	// Bob generates transactions but does not send anything yet.
	bobRefundSig, lockRefundTxScript, lockTxScript, refundTx, spendRefundTx, vIn, pkbs, vkbv, bobDleag, _, err := bob.generateLockTxn(ctx, pkbsf, kbvf, pkaf, aliceDleag)
	if err != nil {
		return fmt.Errorf("unalbe to generate lock transactions: %v", err)
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

	// Alice inits her monero side.
	if err := alice.initXmr(ctx, vkbv, pkbs); err != nil {
		return err
	}

	time.Sleep(time.Second * 5)

	// Bob starts the refund.
	if err := bob.startRefund(ctx, bobRefundSig, aliceRefundSig, lockTxScript, refundTx); err != nil {
		return err
	}

	time.Sleep(time.Second * 5)

	// Bob refunds.
	kafSig, err := bob.refundDcr(ctx, spendRefundTx, spendRefundESig, lockRefundTxScript)
	if err != nil {
		return err
	}

	// Alice refunds.
	xmrChecker, err := alice.refundXmr(ctx, kafSig, spendRefundESig)
	if err != nil {
		return err
	}

	// NOTE: This wallet must sync so may take a long time on mainnet.
	// TODO: Wait for wallet sync rather than a dumb sleep.
	time.Sleep(time.Second * 40)

	balReq := rpc.GetBalanceRequest{}
	bal, err := xmrChecker.GetBalance(ctx, &balReq)
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
	alice, err := newClient(ctx, "http://127.0.0.1:28284/json_rpc", "trading1")
	if err != nil {
		return err
	}

	bob, err := newClient(ctx, "http://127.0.0.1:28184/json_rpc", "trading2")
	if err != nil {
		return err
	}

	dcrBal, err := alice.dcr.GetBalance(ctx, "default")
	if err != nil {
		return err
	}
	dcrBeforeBal := toAtoms(dcrBal.Balances[0].Total)

	// Alice generates dleag.

	pkbsf, kbvf, pkaf, aliceDleag, err := alice.generateDleag(ctx)
	if err != nil {
		return err
	}

	// Bob generates transactions but does not send anything yet.

	bobRefundSig, lockRefundTxScript, lockTxScript, refundTx, spendRefundTx, vIn, pkbs, vkbv, bobDleag, _, err := bob.generateLockTxn(ctx, pkbsf, kbvf, pkaf, aliceDleag)
	if err != nil {
		return fmt.Errorf("unalbe to generate lock transactions: %v", err)
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
	if err := alice.initXmr(ctx, vkbv, pkbs); err != nil {
		return err
	}

	time.Sleep(time.Second * 5)

	// Alice starts the refund.
	if err := alice.startRefund(ctx, bobRefundSig, aliceRefundSig, lockTxScript, refundTx); err != nil {
		return err
	}

	// Lessen this sleep for failure. Two blocks must be mined for success.
	time.Sleep(time.Second * 35)

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
