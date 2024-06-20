// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package btc

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/asset/btc/electrum"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/config"
	dexbtc "decred.org/dcrdex/dex/networks/btc"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
)

// ExchangeWalletElectrum is the asset.Wallet for an external Electrum wallet.
type ExchangeWalletElectrum struct {
	*baseWallet
	*authAddOn
	ew *electrumWallet

	findRedemptionMtx   sync.RWMutex
	findRedemptionQueue map[OutPoint]*FindRedemptionReq
}

var _ asset.Wallet = (*ExchangeWalletElectrum)(nil)
var _ asset.Authenticator = (*ExchangeWalletElectrum)(nil)

// ElectrumWallet creates a new ExchangeWalletElectrum for the provided
// configuration, which must contain the necessary details for accessing the
// Electrum wallet's RPC server in the WalletCFG.Settings map.
func ElectrumWallet(cfg *BTCCloneCFG) (*ExchangeWalletElectrum, error) {
	clientCfg := new(RPCWalletConfig)
	err := config.Unmapify(cfg.WalletCFG.Settings, clientCfg)
	if err != nil {
		return nil, fmt.Errorf("error parsing rpc wallet config: %w", err)
	}

	btc, err := newUnconnectedWallet(cfg, &clientCfg.WalletConfig)
	if err != nil {
		return nil, err
	}

	rpcCfg := &clientCfg.RPCConfig
	dexbtc.StandardizeRPCConf(&rpcCfg.RPCConfig, "")
	ewc := electrum.NewWalletClient(rpcCfg.RPCUser, rpcCfg.RPCPass,
		"http://"+rpcCfg.RPCBind, rpcCfg.WalletName)
	ew := newElectrumWallet(ewc, &electrumWalletConfig{
		params:       cfg.ChainParams,
		log:          cfg.Logger.SubLogger("ELECTRUM"),
		addrDecoder:  cfg.AddressDecoder,
		addrStringer: cfg.AddressStringer,
		segwit:       cfg.Segwit,
		rpcCfg:       rpcCfg,
	})
	btc.setNode(ew)

	eew := &ExchangeWalletElectrum{
		baseWallet:          btc,
		authAddOn:           &authAddOn{btc.node},
		ew:                  ew,
		findRedemptionQueue: make(map[OutPoint]*FindRedemptionReq),
	}
	// In (*baseWallet).feeRate, use ExchangeWalletElectrum's walletFeeRate
	// override for localFeeRate. No externalFeeRate is required but will be
	// used if eew.walletFeeRate returned an error and an externalFeeRate is
	// enabled.
	btc.localFeeRate = eew.walletFeeRate

	return eew, nil
}

// DepositAddress returns an address for depositing funds into the exchange
// wallet. The address will be unused but not necessarily new. Use NewAddress to
// request a new address, but it should be used immediately.
func (btc *ExchangeWalletElectrum) DepositAddress() (string, error) {
	return btc.ew.wallet.GetUnusedAddress(btc.ew.ctx)
}

// RedemptionAddress gets an address for use in redeeming the counterparty's
// swap. This would be included in their swap initialization. The address will
// be unused but not necessarily new because these addresses often go unused.
func (btc *ExchangeWalletElectrum) RedemptionAddress() (string, error) {
	return btc.ew.wallet.GetUnusedAddress(btc.ew.ctx)
}

// Connect connects to the Electrum wallet's RPC server and an electrum server
// directly. Goroutines are started to monitor for new blocks and server
// connection changes. Satisfies the dex.Connector interface.
func (btc *ExchangeWalletElectrum) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	wg, err := btc.connect(ctx) // prepares btc.ew.chainV via btc.node.connect()
	if err != nil {
		return nil, err
	}

	commands, err := btc.ew.wallet.Commands(ctx)
	if err != nil {
		return nil, err
	}
	var hasFreezeUTXO bool
	for i := range commands {
		if commands[i] == "freeze_utxo" {
			hasFreezeUTXO = true
			break
		}
	}
	if !hasFreezeUTXO {
		return nil, errors.New("wallet does not support the freeze_utxo command")
	}

	serverFeats, err := btc.ew.chain().Features(ctx)
	if err != nil {
		return nil, err
	}
	// TODO: for chainforks with the same genesis hash (BTC -> BCH), compare a
	// block hash at some post-fork height.
	if genesis := btc.chainParams.GenesisHash; genesis != nil && genesis.String() != serverFeats.Genesis {
		return nil, fmt.Errorf("wanted genesis hash %v, got %v (wrong network)",
			genesis.String(), serverFeats.Genesis)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		btc.watchBlocks(ctx) // ExchangeWalletElectrum override
		btc.cancelRedemptionSearches()
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		btc.monitorPeers(ctx)
	}()

	return wg, nil
}

func (btc *ExchangeWalletElectrum) cancelRedemptionSearches() {
	// Close all open channels for contract redemption searches
	// to prevent leakages and ensure goroutines that are started
	// to wait on these channels end gracefully.
	btc.findRedemptionMtx.Lock()
	for contractOutpoint, req := range btc.findRedemptionQueue {
		req.fail("shutting down")
		delete(btc.findRedemptionQueue, contractOutpoint)
	}
	btc.findRedemptionMtx.Unlock()
}

// walletFeeRate satisfies BTCCloneCFG.FeeEstimator.
func (btc *ExchangeWalletElectrum) walletFeeRate(ctx context.Context, _ RawRequester, confTarget uint64) (uint64, error) {
	satPerKB, err := btc.ew.wallet.FeeRate(ctx, int64(confTarget))
	if err != nil {
		return 0, err
	}
	return uint64(dex.IntDivUp(satPerKB, 1000)), nil
}

// findRedemption will search for the spending transaction of specified
// outpoint. If found, the secret key will be extracted from the input scripts.
// If not found, but otherwise without an error, a nil Hash will be returned
// along with a nil error. Thus, both the error and the Hash should be checked.
// This convention is only used since this is not part of the public API.
func (btc *ExchangeWalletElectrum) findRedemption(ctx context.Context, op OutPoint, contractHash []byte) (*chainhash.Hash, uint32, []byte, error) {
	msgTx, vin, err := btc.ew.findOutputSpender(ctx, &op.TxHash, op.Vout)
	if err != nil {
		return nil, 0, nil, err
	}
	if msgTx == nil {
		return nil, 0, nil, nil
	}
	txHash := msgTx.TxHash()
	txIn := msgTx.TxIn[vin]
	secret, err := dexbtc.FindKeyPush(txIn.Witness, txIn.SignatureScript,
		contractHash, btc.segwit, btc.chainParams)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("failed to extract secret key from tx %v input %d: %w",
			txHash, vin, err) // name the located tx in the error since we found it
	}
	return &txHash, vin, secret, nil
}

func (btc *ExchangeWalletElectrum) tryRedemptionRequests(ctx context.Context) {
	btc.findRedemptionMtx.RLock()
	reqs := make([]*FindRedemptionReq, 0, len(btc.findRedemptionQueue))
	for _, req := range btc.findRedemptionQueue {
		reqs = append(reqs, req)
	}
	btc.findRedemptionMtx.RUnlock()

	for _, req := range reqs {
		txHash, vin, secret, err := btc.findRedemption(ctx, req.outPt, req.contractHash)
		if err != nil {
			req.fail("findRedemption: %w", err)
			continue
		}
		if txHash == nil {
			continue // maybe next time
		}
		req.success(&FindRedemptionResult{
			redemptionCoinID: ToCoinID(txHash, vin),
			secret:           secret,
		})
	}
}

// FindRedemption locates a swap contract output's redemption transaction input
// and the secret key used to spend the output.
func (btc *ExchangeWalletElectrum) FindRedemption(ctx context.Context, coinID, contract dex.Bytes) (redemptionCoin, secret dex.Bytes, err error) {
	txHash, vout, err := decodeCoinID(coinID)
	if err != nil {
		return nil, nil, err
	}
	contractHash := btc.hashContract(contract)
	// We can verify the contract hash via:
	// txRes, _ := btc.ewc.getWalletTransaction(txHash)
	// msgTx, _ := msgTxFromBytes(txRes.Hex)
	// contractHash := dexbtc.ExtractScriptHash(msgTx.TxOut[vout].PkScript)
	// OR
	// txOut, _, _ := btc.ew.getTxOutput(txHash, vout)
	// contractHash := dexbtc.ExtractScriptHash(txOut.PkScript)

	// Check once before putting this in the queue.
	outPt := NewOutPoint(txHash, vout)
	spendTxID, vin, secret, err := btc.findRedemption(ctx, outPt, contractHash)
	if err != nil {
		return nil, nil, err
	}
	if spendTxID != nil {
		return ToCoinID(spendTxID, vin), secret, nil
	}

	req := &FindRedemptionReq{
		outPt:        outPt,
		resultChan:   make(chan *FindRedemptionResult, 1),
		contractHash: contractHash,
		// blockHash, blockHeight, and pkScript not used by this impl.
		blockHash: &chainhash.Hash{},
	}
	if err := btc.queueFindRedemptionRequest(req); err != nil {
		return nil, nil, err
	}

	var result *FindRedemptionResult
	select {
	case result = <-req.resultChan:
		if result == nil {
			err = fmt.Errorf("unexpected nil result for redemption search for %s", outPt)
		}
	case <-ctx.Done():
		err = fmt.Errorf("context cancelled during search for redemption for %s", outPt)
	}

	// If this contract is still in the findRedemptionQueue, remove from the
	// queue to prevent further redemption search attempts for this contract.
	btc.findRedemptionMtx.Lock()
	delete(btc.findRedemptionQueue, outPt)
	btc.findRedemptionMtx.Unlock()

	// result would be nil if ctx is canceled or the result channel is closed
	// without data, which would happen if the redemption search is aborted when
	// this ExchangeWallet is shut down.
	if result != nil {
		return result.redemptionCoinID, result.secret, result.err
	}
	return nil, nil, err
}

func (btc *ExchangeWalletElectrum) queueFindRedemptionRequest(req *FindRedemptionReq) error {
	btc.findRedemptionMtx.Lock()
	defer btc.findRedemptionMtx.Unlock()
	if _, exists := btc.findRedemptionQueue[req.outPt]; exists {
		return fmt.Errorf("duplicate find redemption request for %s", req.outPt)
	}
	btc.findRedemptionQueue[req.outPt] = req
	return nil
}

// watchBlocks pings for new blocks and runs the tipChange callback function
// when the block changes.
func (btc *ExchangeWalletElectrum) watchBlocks(ctx context.Context) {
	const electrumBlockTick = 5 * time.Second
	ticker := time.NewTicker(electrumBlockTick)
	defer ticker.Stop()

	bestBlock := func() (*BlockVector, error) {
		hdr, err := btc.node.getBestBlockHeader()
		if err != nil {
			return nil, fmt.Errorf("getBestBlockHeader: %v", err)
		}
		hash, err := chainhash.NewHashFromStr(hdr.Hash)
		if err != nil {
			return nil, fmt.Errorf("invalid best block hash %s: %v", hdr.Hash, err)
		}
		return &BlockVector{hdr.Height, *hash}, nil
	}

	currentTip, err := bestBlock()
	if err != nil {
		btc.log.Errorf("Failed to get best block: %v", err)
		currentTip = new(BlockVector) // zero height and hash
	}

	for {
		select {
		case <-ticker.C:
			// Don't make server requests on every tick. Wallet has a headers
			// subscription, so we can just ask wallet the height. That means
			// only comparing heights instead of hashes, which means we might
			// not notice a reorg to a block at the same height, which is
			// unimportant because of how electrum searches for transactions.
			stat, err := btc.node.syncStatus()
			if err != nil {
				btc.log.Errorf("failed to get sync status: %w", err)
				continue
			}

			sameTip := currentTip.Height == int64(stat.Height)
			if sameTip {
				// Could have actually been a reorg to different block at same
				// height. We'll report a new tip block on the next block.
				continue
			}

			newTip, err := bestBlock()
			if err != nil {
				// NOTE: often says "height X out of range", then succeeds on next tick
				if !strings.Contains(err.Error(), "out of range") {
					btc.log.Errorf("failed to get best block from %s electrum server: %v", btc.symbol, err)
				}
				continue
			}

			btc.log.Tracef("tip change: %d (%s) => %d (%s)", currentTip.Height, currentTip.Hash,
				newTip.Height, newTip.Hash)
			currentTip = newTip
			btc.emit.TipChange(uint64(newTip.Height))
			go btc.tryRedemptionRequests(ctx)

		case <-ctx.Done():
			return
		}
	}
}
