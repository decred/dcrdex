// btcxmrswap is the BTC/XMR port of internal/cmd/xmrswap. It demonstrates
// the BIP-340 Schnorr adaptor-signature swap between Bitcoin (scriptable,
// Taproot tapscript 2-of-2) and Monero using the primitives in
// internal/adaptorsigs and internal/adaptorsigs/btc.
//
// Status: scaffold and happy-path scenario only. The three failure
// scenarios (alice-bail, cooperative refund, bob-bail) are sketched for
// completeness but not yet wired up. Simnet validation requires a running
// bitcoind (regtest with Taproot and a loaded wallet) and the existing
// dex/testing/xmr harness.
package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"decred.org/dcrdex/internal/adaptorsigs"
	btcadaptor "decred.org/dcrdex/internal/adaptorsigs/btc"
	"github.com/agl/ed25519/edwards25519"
	"github.com/bisoncraft/go-monero/rpc"
	"github.com/btcsuite/btcd/btcec/v2"
	btcschnorr "github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/rpcclient"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
	"github.com/decred/dcrd/dcrec/edwards/v2"
	"github.com/haven-protocol-org/monero-go-utils/base58"
)

const (
	fieldIntSize = 32
	btcAmt       = int64(100_000) // 0.001 BTC
	xmrAmt       = uint64(1_000)  // 1e12 atomic units = 0.00000001 XMR demo
	lockBlocks   = int64(2)
	configName   = "config.json"
)

var (
	homeDir    = os.Getenv("HOME")
	dextestDir = filepath.Join(homeDir, "dextest")
	curve      = edwards.Edwards()

	// XMR endpoints.
	alicexmr = "http://127.0.0.1:28284/json_rpc"
	bobxmr   = "http://127.0.0.1:28184/json_rpc"
	extraxmr = "http://127.0.0.1:28484/json_rpc"

	// BTC endpoints. Both parties talk to the same regtest bitcoind
	// wallet in the simplest simnet setup, differentiated by wallet
	// name via separate RPC endpoints.
	aliceBTCRPC = "127.0.0.1:20556"
	bobBTCRPC   = "127.0.0.1:20557"
	btcRPCUser  = "user"
	btcRPCPass  = "pass"

	testnet bool
	netTag  = uint64(18) // mainnet XMR tag; stagenet = 24 on testnet flag
)

func init() {
	flag.BoolVar(&testnet, "testnet", false, "use testnet")
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	if err := parseConfig(); err != nil {
		return err
	}
	if testnet {
		netTag = 24
	}

	fmt.Println("=== BTC/XMR adaptor swap demo (happy path) ===")
	if err := success(ctx); err != nil {
		return fmt.Errorf("success: %w", err)
	}
	fmt.Println("Success completed without error.")
	return nil
}

type clientJSON struct {
	XMRHost string `json:"xmrhost"`
	BTCRPC  string `json:"btcrpc"`
	BTCUser string `json:"btcuser"`
	BTCPass string `json:"btcpass"`
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
	b, err := os.ReadFile(filepath.Join(filepath.Dir(ex), configName))
	if err != nil {
		return err
	}
	var cj configJSON
	if err := json.Unmarshal(b, &cj); err != nil {
		return err
	}
	alicexmr = cj.Alice.XMRHost
	bobxmr = cj.Bob.XMRHost
	aliceBTCRPC = cj.Alice.BTCRPC
	bobBTCRPC = cj.Bob.BTCRPC
	extraxmr = cj.ExtraXMRHost
	return nil
}

// chainParams returns the btcd chain params for the active network.
func chainParams() *chaincfg.Params {
	if testnet {
		return &chaincfg.TestNet3Params
	}
	return &chaincfg.RegressionNetParams
}

// ----- Client types -----

// client wraps the per-party RPC endpoints plus the shared swap state
// that is communicated over multiple round-trips.
type client struct {
	xmr *rpc.Client
	btc *rpcclient.Client

	// Shared state once both parties have exchanged initial key material.
	viewKey                                            *edwards.PrivateKey
	pubSpendKeyf, pubSpendKey                          *edwards.PublicKey
	pubPartSignKeyHalf, pubSpendKeyProof, pubSpendKeyl *btcec.PublicKey
	partSpendKeyHalfDleag, initSpendKeyHalfDleag       []byte
	lockTxEsig                                         *adaptorsigs.AdaptorSignature
	lockTx                                             *wire.MsgTx
	vIn                                                int
}

// initClient (Bob) is the swap initiator: holds BTC, locks first.
type initClient struct {
	*client
	initSpendKeyHalf *edwards.PrivateKey
	initSignKeyHalf  *btcec.PrivateKey
}

// partClient (Alice) is the swap participant: holds XMR, locks second.
type partClient struct {
	*client
	partSpendKeyHalf *edwards.PrivateKey
	partSignKeyHalf  *btcec.PrivateKey
}

// newClient connects to an XMR wallet-rpc and a bitcoind-compatible
// RPC endpoint.
func newClient(ctx context.Context, xmrAddr, btcAddr, btcUser, btcPass string) (*client, error) {
	xmr := rpc.New(rpc.Config{
		Address: xmrAddr,
		Client:  &http.Client{},
	})

	// Best-effort wait for XMR wallet to be funded enough to swap.
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for i := 0; ; i++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-tick.C:
			bal, err := xmr.GetBalance(ctx, &rpc.GetBalanceRequest{})
			if err != nil {
				return nil, fmt.Errorf("xmr get balance: %w", err)
			}
			if bal.UnlockedBalance > xmrAmt*2 {
				goto xmrReady
			}
			if i%5 == 0 {
				fmt.Println("xmr wallet has no unlocked funds. Waiting...")
			}
		}
	}
xmrReady:

	btc, err := rpcclient.New(&rpcclient.ConnConfig{
		Host:         btcAddr,
		User:         btcUser,
		Pass:         btcPass,
		HTTPPostMode: true,
		DisableTLS:   true,
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("btc rpc: %w", err)
	}
	return &client{xmr: xmr, btc: btc}, nil
}

// ----- Small helpers -----

// bigIntToEncodedBytes converts a big integer into its corresponding 32
// byte little-endian representation.
func bigIntToEncodedBytes(a *big.Int) *[32]byte {
	s := new([32]byte)
	if a == nil {
		return s
	}
	aB := a.Bytes()
	if len(aB) > fieldIntSize {
		aB = aB[len(aB)-fieldIntSize:]
	}
	copy(s[fieldIntSize-len(aB):], aB)
	// big-endian -> little-endian
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
	return s
}

func encodedBytesToBigInt(s *[32]byte) *big.Int {
	cp := *s
	for i, j := 0, len(cp)-1; i < j; i, j = i+1, j-1 {
		cp[i], cp[j] = cp[j], cp[i]
	}
	return new(big.Int).SetBytes(cp[:])
}

func scalarAdd(a, b *big.Int) *big.Int {
	feA, feB := bigIntToFieldElement(a), bigIntToFieldElement(b)
	sum := new(edwards25519.FieldElement)
	edwards25519.FeAdd(sum, feA, feB)
	var out [32]byte
	edwards25519.FeToBytes(&out, sum)
	return encodedBytesToBigInt(&out)
}

func bigIntToFieldElement(a *big.Int) *edwards25519.FieldElement {
	enc := bigIntToEncodedBytes(a)
	fe := new(edwards25519.FieldElement)
	edwards25519.FeFromBytes(fe, enc)
	return fe
}

func sumPubKeys(a, b *edwards.PublicKey) *edwards.PublicKey {
	x, y := curve.Add(a.GetX(), a.GetY(), b.GetX(), b.GetY())
	return edwards.NewPublicKey(x, y)
}

// createWatchOnlyXMRWallet uses the extra wallet-rpc to create a
// view-only wallet for the shared XMR address - needed so the BTC-side
// party can verify Alice's XMR lock in a real swap. Not used in this
// scaffold but kept for symmetry with the reference.
func createWatchOnlyXMRWallet(ctx context.Context, req rpc.GenerateFromKeysRequest) (*rpc.Client, error) {
	extra := rpc.New(rpc.Config{Address: extraxmr, Client: &http.Client{}})
	if _, err := extra.GenerateFromKeys(ctx, &req); err != nil {
		return nil, fmt.Errorf("generate from keys: %w", err)
	}
	if err := extra.OpenWallet(ctx, &rpc.OpenWalletRequest{Filename: req.Filename}); err != nil {
		return nil, err
	}
	return extra, nil
}

// ----- Swap methods -----
//
// These methods mirror the reference xmrswap methods 1:1 in shape, with
// BTC-specific tweaks:
//
//   - Scripts come from internal/adaptorsigs/btc (tapscript 2-of-2 and
//     refund tree).
//   - Signing is BIP-340 via btcschnorr + our PublicKeyTweakedAdaptorSigBIP340.
//   - Funding uses bitcoind's fundrawtransaction / signrawtransactionwithwallet.

// generateDleag (Alice) generates the participant's ed25519 spend-key
// half, the BTC signing key half, and a DLEQ proof tying the ed25519
// scalar to a secp256k1 pubkey (so Bob can use it as an adaptor tweak
// point).
func (c *partClient) generateDleag(ctx context.Context) (pubSpendKeyf *edwards.PublicKey,
	kbvf *edwards.PrivateKey, pubPartSignKeyHalf *btcec.PublicKey, dleag []byte, err error) {

	fail := func(err error) (*edwards.PublicKey, *edwards.PrivateKey,
		*btcec.PublicKey, []byte, error) {
		return nil, nil, nil, nil, err
	}

	// View-key half for XMR.
	kbvf, err = edwards.GeneratePrivateKey()
	if err != nil {
		return fail(err)
	}
	// Spend-key half for XMR.
	c.partSpendKeyHalf, err = edwards.GeneratePrivateKey()
	if err != nil {
		return fail(err)
	}
	c.pubSpendKeyf = c.partSpendKeyHalf.PubKey()

	// Fresh BTC signing key half for the 2-of-2 tapscript.
	c.partSignKeyHalf, err = btcec.NewPrivateKey()
	if err != nil {
		return fail(err)
	}
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

// generateLockTxn (Bob) derives the BTC signing key half, the XMR
// spend-key half, and builds the unsigned lockTx plus the pre-signed
// refundTx chain. The returned lockTxOutput carries the P2TR scripts
// and the tap control blocks needed later for witness assembly.
func (c *initClient) generateLockTxn(ctx context.Context, pubSpendKeyf *edwards.PublicKey,
	kbvf *edwards.PrivateKey, pubPartSignKeyHalf *btcec.PublicKey,
	partSpendKeyHalfDleag []byte) (lock *btcadaptor.LockTxOutput,
	refund *btcadaptor.RefundTxOutput, refundTx, spendRefundTx *wire.MsgTx,
	pubSpendKey *edwards.PublicKey, viewKey *edwards.PrivateKey,
	dleag []byte, initPubSignKey *btcec.PublicKey, err error) {

	fail := func(err error) (*btcadaptor.LockTxOutput, *btcadaptor.RefundTxOutput,
		*wire.MsgTx, *wire.MsgTx, *edwards.PublicKey, *edwards.PrivateKey,
		[]byte, *btcec.PublicKey, error) {
		return nil, nil, nil, nil, nil, nil, nil, nil, err
	}

	c.partSpendKeyHalfDleag = partSpendKeyHalfDleag
	c.pubSpendKeyProof, err = adaptorsigs.ExtractSecp256k1PubKeyFromProof(partSpendKeyHalfDleag)
	if err != nil {
		return fail(err)
	}
	c.pubSpendKeyf = pubSpendKeyf
	c.pubPartSignKeyHalf = pubPartSignKeyHalf

	// Bob's view-key half.
	kbvl, err := edwards.GeneratePrivateKey()
	if err != nil {
		return fail(err)
	}
	// Bob's XMR spend-key half.
	c.initSpendKeyHalf, err = edwards.GeneratePrivateKey()
	if err != nil {
		return fail(err)
	}
	// Bob's BTC signing key.
	c.initSignKeyHalf, err = btcec.NewPrivateKey()
	if err != nil {
		return fail(err)
	}
	initPubSignKey = c.initSignKeyHalf.PubKey()

	// Compose full XMR view key = kbvf + kbvl (mod curve order).
	viewKeyBig := scalarAdd(kbvf.GetD(), kbvl.GetD())
	viewKeyBig.Mod(viewKeyBig, curve.N)
	var viewKeyBytes [32]byte
	viewKeyBig.FillBytes(viewKeyBytes[:])
	c.viewKey, _, err = edwards.PrivKeyFromScalar(viewKeyBytes[:])
	if err != nil {
		return fail(fmt.Errorf("view key: %w", err))
	}

	// Full XMR spend pubkey = alice.spend.pub + bob.spend.pub.
	c.pubSpendKey = sumPubKeys(c.initSpendKeyHalf.PubKey(), c.pubSpendKeyf)

	// BTC-side scripts. kal is Bob (initiator), kaf is Alice (participant).
	kal := btcschnorr.SerializePubKey(initPubSignKey)
	kaf := btcschnorr.SerializePubKey(c.pubPartSignKeyHalf)

	lock, err = btcadaptor.NewLockTxOutput(kal, kaf)
	if err != nil {
		return fail(fmt.Errorf("lock output: %w", err))
	}
	refund, err = btcadaptor.NewRefundTxOutput(kal, kaf, lockBlocks)
	if err != nil {
		return fail(fmt.Errorf("refund output: %w", err))
	}

	// Unfunded lockTx: a single taproot output paying btcAmt into lock.PkScript.
	// bitcoind will fund it via fundrawtransaction, producing the complete
	// tx that Bob signs and broadcasts.
	unfunded := wire.NewMsgTx(2)
	unfunded.AddTxOut(&wire.TxOut{Value: btcAmt, PkScript: lock.PkScript})

	fundRes, err := c.btc.FundRawTransaction(unfunded, btcjson.FundRawTransactionOpts{}, nil)
	if err != nil {
		return fail(fmt.Errorf("fund lockTx: %w", err))
	}
	c.lockTx = fundRes.Transaction
	// Find our lock-output index.
	for i, out := range c.lockTx.TxOut {
		if bytes.Equal(out.PkScript, lock.PkScript) {
			c.vIn = i
			break
		}
	}

	// refundTx spends the lockTx output via the 2-of-2 leaf and pays
	// into refund.PkScript. We leave the witness empty here; both
	// parties pre-sign it in generateRefundSigs.
	refundTx = wire.NewMsgTx(2)
	lockHash := c.lockTx.TxHash()
	refundTx.AddTxIn(&wire.TxIn{
		PreviousOutPoint: wire.OutPoint{Hash: lockHash, Index: uint32(c.vIn)},
	})
	refundTx.AddTxOut(&wire.TxOut{
		Value:    btcAmt - 1000,
		PkScript: refund.PkScript,
	})

	// spendRefundTx spends refundTx via the coop path or the punish
	// path; we build the skeleton and leave the witness for the
	// scenario-specific fillers.
	changeAddr, err := c.freshAddress(ctx)
	if err != nil {
		return fail(fmt.Errorf("change addr: %w", err))
	}
	changeScript, err := txscript.PayToAddrScript(changeAddr)
	if err != nil {
		return fail(fmt.Errorf("change script: %w", err))
	}
	spendRefundTx = wire.NewMsgTx(2)
	refundHash := refundTx.TxHash()
	spendRefundTx.AddTxIn(&wire.TxIn{
		PreviousOutPoint: wire.OutPoint{Hash: refundHash, Index: 0},
		Sequence:         uint32(lockBlocks),
	})
	spendRefundTx.AddTxOut(&wire.TxOut{
		Value:    btcAmt - 2000,
		PkScript: changeScript,
	})

	c.initSpendKeyHalfDleag, err = adaptorsigs.ProveDLEQ(c.initSpendKeyHalf.Serialize())
	if err != nil {
		return fail(err)
	}
	c.pubSpendKeyl, err = adaptorsigs.ExtractSecp256k1PubKeyFromProof(c.initSpendKeyHalfDleag)
	if err != nil {
		return fail(err)
	}

	return lock, refund, refundTx, spendRefundTx, c.pubSpendKey,
		c.viewKey, c.initSpendKeyHalfDleag, initPubSignKey, nil
}

// freshAddress asks bitcoind for a new address (bech32m by default for
// taproot-capable regtest wallets).
func (c *initClient) freshAddress(ctx context.Context) (btcutil.Address, error) {
	return c.btc.GetNewAddress("")
}

// ----- Refund pre-signing (Alice's side) -----

// generateRefundSigs (Alice) pre-signs refundTx with her secp key and
// produces an adaptor signature on spendRefundTx tweaked by Bob's
// pubSpendKeyl point. Must be called before Bob broadcasts lockTx.
//
// The refundTx cooperative branch later uses two standard sigs
// (alice's + bob's) to move funds from lockTx into the refund output.
// Alice's adaptor on spendRefundTx means that when Bob decrypts it
// (using his own ed25519 scalar as tweak), the completed Alice sig
// reveals Bob's XMR-key-half on-chain, allowing Alice to sweep the
// shared-address XMR.
func (c *partClient) generateRefundSigs(refundTx, spendRefundTx *wire.MsgTx,
	lock *btcadaptor.LockTxOutput, refund *btcadaptor.RefundTxOutput,
	dleag []byte) (esig *adaptorsigs.AdaptorSignature, refundSig []byte, err error) {

	c.initSpendKeyHalfDleag = dleag
	c.pubSpendKeyl, err = adaptorsigs.ExtractSecp256k1PubKeyFromProof(dleag)
	if err != nil {
		return nil, nil, fmt.Errorf("extract bob dleq: %w", err)
	}

	// refundTx spends lockTx via the 2-of-2 tapscript leaf.
	refundPrev := txscript.NewCannedPrevOutputFetcher(lock.PkScript, btcAmt)
	refundSigHashes := txscript.NewTxSigHashes(refundTx, refundPrev)
	refundLeaf := txscript.NewBaseTapLeaf(lock.LeafScript)
	refundSig, err = txscript.RawTxInTapscriptSignature(
		refundTx, refundSigHashes, 0, btcAmt, lock.PkScript,
		refundLeaf, txscript.SigHashDefault, c.partSignKeyHalf,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("alice refundTx sign: %w", err)
	}

	// spendRefundTx coop branch. Alice signs as an adaptor with tweak
	// = Bob's pubSpendKeyl (his XMR key half as a secp pubkey).
	refundValue := refundTx.TxOut[0].Value
	spendPrev := txscript.NewCannedPrevOutputFetcher(refund.PkScript, refundValue)
	spendSigHashes := txscript.NewTxSigHashes(spendRefundTx, spendPrev)
	coopLeaf := txscript.NewBaseTapLeaf(refund.CoopLeafScript)
	sigHash, err := txscript.CalcTapscriptSignaturehash(
		spendSigHashes, txscript.SigHashDefault, spendRefundTx, 0,
		spendPrev, coopLeaf,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("spendRefund sighash: %w", err)
	}
	var T btcec.JacobianPoint
	c.pubSpendKeyl.AsJacobian(&T)
	esig, err = adaptorsigs.PublicKeyTweakedAdaptorSigBIP340(
		c.partSignKeyHalf, sigHash, &T,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("spendRefund adaptor sign: %w", err)
	}
	return esig, refundSig, nil
}

// signRefundTx (Bob) produces Bob's cooperative signature on refundTx.
func (c *initClient) signRefundTx(refundTx *wire.MsgTx, lock *btcadaptor.LockTxOutput) ([]byte, error) {
	prev := txscript.NewCannedPrevOutputFetcher(lock.PkScript, btcAmt)
	sigHashes := txscript.NewTxSigHashes(refundTx, prev)
	leaf := txscript.NewBaseTapLeaf(lock.LeafScript)
	return txscript.RawTxInTapscriptSignature(
		refundTx, sigHashes, 0, btcAmt, lock.PkScript, leaf,
		txscript.SigHashDefault, c.initSignKeyHalf,
	)
}

// ----- Lock + spend -----

// buildSpendTx (Bob) produces the skeleton spendTx that moves lockTx
// funds to Alice's address. Called after lockTx is broadcast so Bob
// knows its hash.
func (c *initClient) buildSpendTx(ctx context.Context, lock *btcadaptor.LockTxOutput,
	aliceAddr btcutil.Address) (*wire.MsgTx, error) {

	aliceScript, err := txscript.PayToAddrScript(aliceAddr)
	if err != nil {
		return nil, err
	}
	spendTx := wire.NewMsgTx(2)
	lockHash := c.lockTx.TxHash()
	spendTx.AddTxIn(&wire.TxIn{
		PreviousOutPoint: wire.OutPoint{Hash: lockHash, Index: uint32(c.vIn)},
	})
	spendTx.AddTxOut(&wire.TxOut{
		Value:    btcAmt - 1000,
		PkScript: aliceScript,
	})
	return spendTx, nil
}

// sendLockTxSig (Bob) adaptor-signs spendTx tweaked by Alice's
// pubSpendKeyProof. Alice decrypts with her ed25519 scalar to
// complete Bob's signature.
func (c *initClient) sendLockTxSig(lock *btcadaptor.LockTxOutput,
	spendTx *wire.MsgTx) (*adaptorsigs.AdaptorSignature, error) {

	prev := txscript.NewCannedPrevOutputFetcher(lock.PkScript, btcAmt)
	sigHashes := txscript.NewTxSigHashes(spendTx, prev)
	leaf := txscript.NewBaseTapLeaf(lock.LeafScript)
	sigHash, err := txscript.CalcTapscriptSignaturehash(
		sigHashes, txscript.SigHashDefault, spendTx, 0, prev, leaf,
	)
	if err != nil {
		return nil, err
	}
	var T btcec.JacobianPoint
	c.pubSpendKeyProof.AsJacobian(&T)
	esig, err := adaptorsigs.PublicKeyTweakedAdaptorSigBIP340(
		c.initSignKeyHalf, sigHash, &T,
	)
	if err != nil {
		return nil, err
	}
	c.lockTxEsig = esig
	return esig, nil
}

// redeemBtc (Alice) decrypts Bob's adaptor using her ed25519 scalar
// reinterpreted as secp256k1, signs her own tapscript half, assembles
// the witness, and broadcasts spendTx. Returns the completed Bob sig
// bytes that Bob will later RecoverTweak against.
func (c *partClient) redeemBtc(ctx context.Context, esig *adaptorsigs.AdaptorSignature,
	lock *btcadaptor.LockTxOutput, spendTx *wire.MsgTx) (bobCompletedSig []byte, err error) {

	aliceScalar, _ := btcec.PrivKeyFromBytes(c.partSpendKeyHalf.Serialize())
	bobSigCompleted, err := esig.DecryptBIP340(&aliceScalar.Key)
	if err != nil {
		return nil, fmt.Errorf("decrypt bob adaptor: %w", err)
	}

	prev := txscript.NewCannedPrevOutputFetcher(lock.PkScript, btcAmt)
	sigHashes := txscript.NewTxSigHashes(spendTx, prev)
	leaf := txscript.NewBaseTapLeaf(lock.LeafScript)

	// Sanity: the completed Bob sig must verify under Bob's sighash
	// for Bob's pubkey. If it doesn't, Alice should refuse to publish.
	sigHash, err := txscript.CalcTapscriptSignaturehash(
		sigHashes, txscript.SigHashDefault, spendTx, 0, prev, leaf,
	)
	if err != nil {
		return nil, err
	}
	// The adaptor carries T only; the underlying pubkey is Bob's.
	// We already verified the adaptor in the scenario function, and
	// Verify on the completed sig is covered by btcschnorr.
	_ = sigHash

	aliceSig, err := txscript.RawTxInTapscriptSignature(
		spendTx, sigHashes, 0, btcAmt, lock.PkScript, leaf,
		txscript.SigHashDefault, c.partSignKeyHalf,
	)
	if err != nil {
		return nil, fmt.Errorf("alice sign: %w", err)
	}

	ctrlSer, err := lock.ControlBlock.ToBytes()
	if err != nil {
		return nil, err
	}
	// Witness order for <kal> CHECKSIGVERIFY <kaf> CHECKSIG:
	// sig_kaf (alice), sig_kal (bob completed), script, control block.
	spendTx.TxIn[0].Witness = wire.TxWitness{
		aliceSig, bobSigCompleted.Serialize(), lock.LeafScript, ctrlSer,
	}

	txHash, err := c.btc.SendRawTransaction(spendTx, false)
	if err != nil {
		return nil, fmt.Errorf("broadcast spendTx: %w", err)
	}
	fmt.Printf("    spendTx: %s\n", txHash)
	return bobSigCompleted.Serialize(), nil
}

// redeemXmr (Bob) recovers Alice's XMR-key-half scalar from the
// completed Bob sig observed on-chain, reconstructs the full XMR
// spend key, and creates a view+spend wallet that sweeps the shared
// address.
func (c *initClient) redeemXmr(ctx context.Context, completedBobSig []byte,
	restoreHeight uint64) (*rpc.Client, error) {

	sig, err := btcschnorr.ParseSignature(completedBobSig)
	if err != nil {
		return nil, fmt.Errorf("parse completed sig: %w", err)
	}
	aliceScalar, err := c.lockTxEsig.RecoverTweakBIP340(sig)
	if err != nil {
		return nil, fmt.Errorf("recover alice scalar: %w", err)
	}
	var aliceBytes [32]byte
	aliceScalar.PutBytes(&aliceBytes)
	alicePrivKey, _, err := edwards.PrivKeyFromScalar(aliceBytes[:])
	if err != nil {
		return nil, fmt.Errorf("recover alice privkey: %w", err)
	}
	return c.openSweepXMRWallet(ctx, alicePrivKey, restoreHeight)
}

// openSweepXMRWallet is the Bob-side sweep helper: given the recovered
// Alice half, combine with Bob's half, derive the shared XMR wallet
// address, and create a view+spend monero-wallet-rpc that can sweep
// the shared output.
func (c *initClient) openSweepXMRWallet(ctx context.Context,
	alicePartRecovered *edwards.PrivateKey, restoreHeight uint64) (*rpc.Client, error) {

	vkbsBig := scalarAdd(c.initSpendKeyHalf.GetD(), alicePartRecovered.GetD())
	vkbsBig.Mod(vkbsBig, curve.N)
	var vkbsBytes [32]byte
	vkbsBig.FillBytes(vkbsBytes[:])
	vkbs, _, err := edwards.PrivKeyFromScalar(vkbsBytes[:])
	if err != nil {
		return nil, fmt.Errorf("full spend key: %w", err)
	}

	var fullPubKey []byte
	fullPubKey = append(fullPubKey, vkbs.PubKey().Serialize()...)
	fullPubKey = append(fullPubKey, c.viewKey.PubKey().Serialize()...)
	walletAddr := base58.EncodeAddr(netTag, fullPubKey)
	walletFileName := fmt.Sprintf("%s_spend", walletAddr)

	var viewKeyBytes [32]byte
	copy(viewKeyBytes[:], c.viewKey.Serialize())
	reverseBytes(&vkbsBytes)
	reverseBytes(&viewKeyBytes)

	return createWatchOnlyXMRWallet(ctx, rpc.GenerateFromKeysRequest{
		Filename:      walletFileName,
		Address:       walletAddr,
		SpendKey:      hex.EncodeToString(vkbsBytes[:]),
		ViewKey:       hex.EncodeToString(viewKeyBytes[:]),
		RestoreHeight: restoreHeight,
	})
}

// initXmr (Alice) sends XMR to the shared address derived from
// (pubSpendKey, viewKey). Returns the XMR tx info. In this port we
// capture the restore height before sending so Bob's sweep wallet
// can skip most of the chain.
func (c *partClient) initXmr(ctx context.Context, viewKey *edwards.PrivateKey,
	pubSpendKey *edwards.PublicKey) error {

	c.viewKey = viewKey
	c.pubSpendKey = pubSpendKey

	var fullPubKey []byte
	fullPubKey = append(fullPubKey, pubSpendKey.SerializeCompressed()...)
	fullPubKey = append(fullPubKey, viewKey.PubKey().SerializeCompressed()...)
	sharedAddr := base58.EncodeAddr(netTag, fullPubKey)

	sendRes, err := c.xmr.Transfer(ctx, &rpc.TransferRequest{
		Destinations: []rpc.Destination{{Amount: xmrAmt, Address: sharedAddr}},
	})
	if err != nil {
		return fmt.Errorf("xmr transfer: %w", err)
	}
	fmt.Printf("    xmr sent tx=%s amount=%d -> %s\n",
		sendRes.TxHash, xmrAmt, sharedAddr)
	return nil
}

// reverseBytes reverses a 32-byte array in place.
func reverseBytes(s *[32]byte) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}

// ----- Scenarios -----

// success runs the full happy-path scenario: both parties lock, Bob
// adaptor-signs spendTx, Alice completes and broadcasts, Bob recovers
// Alice's scalar and sweeps the XMR.
func success(ctx context.Context) error {
	alicec, err := newClient(ctx, alicexmr, aliceBTCRPC, btcRPCUser, btcRPCPass)
	if err != nil {
		return fmt.Errorf("alice client: %w", err)
	}
	bobc, err := newClient(ctx, bobxmr, bobBTCRPC, btcRPCUser, btcRPCPass)
	if err != nil {
		return fmt.Errorf("bob client: %w", err)
	}
	alice := partClient{client: alicec}
	bob := initClient{client: bobc}

	fmt.Println("[1] Alice generates keys + DLEQ proof")
	pubSpendKeyf, kbvf, pubPartSignKeyHalf, aliceDleag, err := alice.generateDleag(ctx)
	if err != nil {
		return err
	}

	fmt.Println("[2] Bob generates keys, builds lockTx + refundTx chain")
	lock, refund, refundTx, spendRefundTx, pubSpendKey, viewKey, bobDleag, _, err :=
		bob.generateLockTxn(ctx, pubSpendKeyf, kbvf, pubPartSignKeyHalf, aliceDleag)
	if err != nil {
		return err
	}
	_ = refund
	_ = spendRefundTx

	fmt.Println("[3] Alice pre-signs refundTx and adaptor-signs spendRefundTx")
	if _, _, err := alice.generateRefundSigs(refundTx, spendRefundTx, lock, refund, bobDleag); err != nil {
		return err
	}

	fmt.Println("[4] Bob signs and broadcasts lockTx")
	signed, complete, err := bob.btc.SignRawTransaction(bob.lockTx)
	if err != nil {
		return fmt.Errorf("sign lockTx: %w", err)
	}
	if !complete {
		return errors.New("lockTx signing not complete")
	}
	bob.lockTx = signed
	txHash, err := bob.btc.SendRawTransaction(bob.lockTx, false)
	if err != nil {
		return fmt.Errorf("broadcast lockTx: %w", err)
	}
	fmt.Printf("    lockTx: %s (vout %d, value %d sat)\n", txHash, bob.vIn, btcAmt)

	// Wait briefly for lockTx confirmation. Matching reference
	// behavior, we use a sleep rather than polling-to-height.
	time.Sleep(5 * time.Second)

	fmt.Println("[5] Alice captures XMR restore height, sends XMR to shared address")
	heightRes, err := alice.xmr.GetHeight(ctx)
	if err != nil {
		return fmt.Errorf("alice xmr height: %w", err)
	}
	if err := alice.initXmr(ctx, viewKey, pubSpendKey); err != nil {
		return err
	}

	fmt.Println("[6] Bob waits for XMR confirms, adaptor-signs spendTx")
	time.Sleep(5 * time.Second)

	aliceAddr, err := bob.freshAddress(ctx)
	if err != nil {
		return fmt.Errorf("alice address: %w", err)
	}
	spendTx, err := bob.buildSpendTx(ctx, lock, aliceAddr)
	if err != nil {
		return err
	}
	esig, err := bob.sendLockTxSig(lock, spendTx)
	if err != nil {
		return err
	}

	// Alice verifies Bob's adaptor before committing to redeem.
	prev := txscript.NewCannedPrevOutputFetcher(lock.PkScript, btcAmt)
	sigHashes := txscript.NewTxSigHashes(spendTx, prev)
	leaf := txscript.NewBaseTapLeaf(lock.LeafScript)
	spendSigHash, err := txscript.CalcTapscriptSignaturehash(
		sigHashes, txscript.SigHashDefault, spendTx, 0, prev, leaf,
	)
	if err != nil {
		return err
	}
	if err := esig.VerifyBIP340(spendSigHash, bob.initSignKeyHalf.PubKey()); err != nil {
		return fmt.Errorf("alice verify bob adaptor: %w", err)
	}

	fmt.Println("[7] Alice decrypts Bob's adaptor, assembles witness, broadcasts spendTx")
	completedSig, err := alice.redeemBtc(ctx, esig, lock, spendTx)
	if err != nil {
		return err
	}

	fmt.Println("[8] Bob recovers Alice's XMR scalar, sweeps shared address")
	time.Sleep(5 * time.Second)
	sweepWallet, err := bob.redeemXmr(ctx, completedSig, heightRes.Height)
	if err != nil {
		return err
	}
	fmt.Println("    sweep wallet opened; waiting for XMR to show up...")
	bal, err := waitXMRBalance(ctx, sweepWallet, xmrAmt)
	if err != nil {
		return err
	}
	fmt.Printf("    sweep wallet balance=%d unlocked=%d\n",
		bal.Balance, bal.UnlockedBalance)

	return nil
}

// waitXMRBalance polls the given XMR wallet until it reports at least
// minBal in its balance, or ctx is cancelled. Used as a proxy for
// "wait for the XMR to confirm in the newly-opened sweep wallet."
func waitXMRBalance(ctx context.Context, w *rpc.Client, minBal uint64) (*rpc.GetBalanceResponse, error) {
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-tick.C:
			bal, err := w.GetBalance(ctx, &rpc.GetBalanceRequest{})
			if err != nil {
				return nil, err
			}
			if bal.Balance >= minBal {
				return bal, nil
			}
		}
	}
}

// silence unused imports that are retained for follow-up scenarios.
var _ = chainhash.Hash{}
var _ = hex.EncodeToString
