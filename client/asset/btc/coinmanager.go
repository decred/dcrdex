package btc

import (
	"errors"
	"fmt"
	"maps"
	"math"
	"sort"
	"sync"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	dexbtc "decred.org/dcrdex/dex/networks/btc"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
)

// CompositeUTXO combines utxo info with the spending input information.
type CompositeUTXO struct {
	*UTxO
	Confs        uint32
	RedeemScript []byte
	Input        *dexbtc.SpendInfo
}

// EnoughFunc considers information about funding inputs and indicates whether
// it is enough to fund an order. EnoughFunc is bound to an order by the
// OrderFundingThresholder.
type EnoughFunc func(inputCount, inputsSize, sum uint64) (bool, uint64)

// OrderEstimator is a function that accepts information about an order and
// estimates the total required funds needed for the order.
type OrderEstimator func(swapVal, inputCount, inputsSize, maxSwaps, feeRate uint64) uint64

// OrderFundingThresholder accepts information about an order and generates an
// EnoughFunc that can be used to test funding input combinations.
type OrderFundingThresholder func(val, lots, maxFeeRate uint64, reportChange bool) EnoughFunc

// CoinManager provides utilities for working with unspent transaction outputs.
// In addition to translation to and from custom wallet types, there are
// CoinManager methods to help pick UTXOs for funding in various contexts.
type CoinManager struct {
	// Coins returned by Fund are cached for quick reference.
	mtx sync.RWMutex
	log dex.Logger

	orderEnough OrderFundingThresholder
	chainParams *chaincfg.Params
	listUnspent func() ([]*ListUnspentResult, error)
	lockUnspent func(unlock bool, ops []*Output) error
	listLocked  func() ([]*RPCOutpoint, error)
	getTxOut    func(txHash *chainhash.Hash, vout uint32) (*wire.TxOut, error)
	stringAddr  func(btcutil.Address) (string, error)

	lockedOutputs map[OutPoint]*UTxO
}

func NewCoinManager(
	log dex.Logger,
	chainParams *chaincfg.Params,
	orderEnough OrderFundingThresholder,
	listUnspent func() ([]*ListUnspentResult, error),
	lockUnspent func(unlock bool, ops []*Output) error,
	listLocked func() ([]*RPCOutpoint, error),
	getTxOut func(txHash *chainhash.Hash, vout uint32) (*wire.TxOut, error),
	stringAddr func(btcutil.Address) (string, error),
) *CoinManager {

	return &CoinManager{
		log:           log,
		orderEnough:   orderEnough,
		chainParams:   chainParams,
		listUnspent:   listUnspent,
		lockUnspent:   lockUnspent,
		listLocked:    listLocked,
		getTxOut:      getTxOut,
		lockedOutputs: make(map[OutPoint]*UTxO),
		stringAddr:    stringAddr,
	}
}

// FundWithUTXOs attempts to find the best combination of UTXOs to satisfy the
// given EnoughFunc while respecting the specified keep reserves (if non-zero).
func (c *CoinManager) FundWithUTXOs(
	utxos []*CompositeUTXO,
	keep uint64,
	lockUnspents bool,
	enough EnoughFunc,
) (coins asset.Coins, fundingCoins map[OutPoint]*UTxO, spents []*Output, redeemScripts []dex.Bytes, size, sum uint64, err error) {
	var avail uint64
	for _, utxo := range utxos {
		avail += utxo.Amount
	}

	c.mtx.Lock()
	defer c.mtx.Unlock()
	return c.fundWithUTXOs(utxos, avail, keep, lockUnspents, enough)
}

func (c *CoinManager) fundWithUTXOs(
	utxos []*CompositeUTXO,
	avail, keep uint64,
	lockUnspents bool,
	enough EnoughFunc,
) (coins asset.Coins, fundingCoins map[OutPoint]*UTxO, spents []*Output, redeemScripts []dex.Bytes, size, sum uint64, err error) {

	if keep > 0 {
		kept := leastOverFund(reserveEnough(keep), utxos)
		c.log.Debugf("Setting aside %v BTC in %d UTXOs to respect the %v BTC reserved amount",
			toBTC(SumUTXOs(kept)), len(kept), toBTC(keep))
		utxosPruned := UTxOSetDiff(utxos, kept)
		sum, _, size, coins, fundingCoins, redeemScripts, spents, err = TryFund(utxosPruned, enough)
		if err != nil {
			c.log.Debugf("Unable to fund order with UTXOs set aside (%v), trying again with full UTXO set.", err)
		}
	}
	if len(spents) == 0 { // either keep is zero or it failed with utxosPruned
		// Without utxos set aside for keep, we have to consider any spendable
		// change (extra) that the enough func grants us.

		var extra uint64
		sum, extra, size, coins, fundingCoins, redeemScripts, spents, err = TryFund(utxos, enough)
		if err != nil {
			return nil, nil, nil, nil, 0, 0, err
		}
		if avail-sum+extra < keep {
			return nil, nil, nil, nil, 0, 0, asset.ErrInsufficientBalance
		}
		// else we got lucky with the legacy funding approach and there was
		// either available unspent or the enough func granted spendable change.
		if keep > 0 && extra > 0 {
			c.log.Debugf("Funding succeeded with %v BTC in spendable change.", toBTC(extra))
		}
	}

	if lockUnspents {
		err = c.lockUnspent(false, spents)
		if err != nil {
			return nil, nil, nil, nil, 0, 0, fmt.Errorf("LockUnspent error: %w", err)
		}
		maps.Copy(c.lockedOutputs, fundingCoins)
	}

	return coins, fundingCoins, spents, redeemScripts, size, sum, err
}

func (c *CoinManager) fund(keep uint64, minConfs uint32, lockUnspents bool,
	enough func(_, size, sum uint64) (bool, uint64)) (
	coins asset.Coins, fundingCoins map[OutPoint]*UTxO, spents []*Output, redeemScripts []dex.Bytes, size, sum uint64, err error) {
	utxos, _, avail, err := c.spendableUTXOs(minConfs)
	if err != nil {
		return nil, nil, nil, nil, 0, 0, fmt.Errorf("error getting spendable utxos: %w", err)
	}
	return c.fundWithUTXOs(utxos, avail, keep, lockUnspents, enough)
}

// Fund attempts to satisfy the given EnoughFunc with all available UTXOs. For
// situations where Fund might be called repeatedly, the caller should instead
// do SpendableUTXOs and use the results in FundWithUTXOs.
func (c *CoinManager) Fund(
	keep uint64,
	minConfs uint32,
	lockUnspents bool,
	enough EnoughFunc,
) (coins asset.Coins, fundingCoins map[OutPoint]*UTxO, spents []*Output, redeemScripts []dex.Bytes, size, sum uint64, err error) {

	c.mtx.Lock()
	defer c.mtx.Unlock()

	return c.fund(keep, minConfs, lockUnspents, enough)
}

// OrderWithLeastOverFund returns the index of the order from a slice of orders
// that requires the least over-funding without using more than maxLock. It
// also returns the UTXOs that were used to fund the order. If none can be
// funded without using more than maxLock, -1 is returned.
func (c *CoinManager) OrderWithLeastOverFund(maxLock, feeRate uint64, orders []*asset.MultiOrderValue, utxos []*CompositeUTXO) (orderIndex int, leastOverFundingUTXOs []*CompositeUTXO) {
	minOverFund := uint64(math.MaxUint64)
	orderIndex = -1
	for i, value := range orders {
		enough := c.orderEnough(value.Value, value.MaxSwapCount, feeRate, false)
		var fundingUTXOs []*CompositeUTXO
		if maxLock > 0 {
			fundingUTXOs = leastOverFundWithLimit(enough, maxLock, utxos)
		} else {
			fundingUTXOs = leastOverFund(enough, utxos)
		}
		if len(fundingUTXOs) == 0 {
			continue
		}
		sum := SumUTXOs(fundingUTXOs)
		overFund := sum - value.Value
		if overFund < minOverFund {
			minOverFund = overFund
			orderIndex = i
			leastOverFundingUTXOs = fundingUTXOs
		}
	}
	return
}

// FundMultiBestEffort makes a best effort to fund every order. If it is not
// possible, it returns coins for the orders that could be funded. The coins
// that fund each order are returned in the same order as the values that were
// passed in. If a split is allowed and all orders cannot be funded, nil slices
// are returned.
func (c *CoinManager) FundMultiBestEffort(keep, maxLock uint64, values []*asset.MultiOrderValue,
	maxFeeRate uint64, splitAllowed bool) ([]asset.Coins, [][]dex.Bytes, map[OutPoint]*UTxO, []*Output, error) {
	utxos, _, avail, err := c.SpendableUTXOs(0)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("error getting spendable utxos: %w", err)
	}

	fundAllOrders := func() [][]*CompositeUTXO {
		indexToFundingCoins := make(map[int][]*CompositeUTXO, len(values))
		remainingUTXOs := utxos
		remainingOrders := values
		remainingIndexes := make([]int, len(values))
		for i := range remainingIndexes {
			remainingIndexes[i] = i
		}
		var totalFunded uint64
		for range values {
			orderIndex, fundingUTXOs := c.OrderWithLeastOverFund(maxLock-totalFunded, maxFeeRate, remainingOrders, remainingUTXOs)
			if orderIndex == -1 {
				return nil
			}
			totalFunded += SumUTXOs(fundingUTXOs)
			if totalFunded > avail-keep {
				return nil
			}
			newRemainingOrders := make([]*asset.MultiOrderValue, 0, len(remainingOrders)-1)
			newRemainingIndexes := make([]int, 0, len(remainingOrders)-1)
			for j := range remainingOrders {
				if j != orderIndex {
					newRemainingOrders = append(newRemainingOrders, remainingOrders[j])
					newRemainingIndexes = append(newRemainingIndexes, remainingIndexes[j])
				}
			}
			indexToFundingCoins[remainingIndexes[orderIndex]] = fundingUTXOs
			remainingOrders = newRemainingOrders
			remainingIndexes = newRemainingIndexes
			remainingUTXOs = UTxOSetDiff(remainingUTXOs, fundingUTXOs)
		}
		allFundingUTXOs := make([][]*CompositeUTXO, len(values))
		for i := range values {
			allFundingUTXOs[i] = indexToFundingCoins[i]
		}
		return allFundingUTXOs
	}

	fundInOrder := func(orderedValues []*asset.MultiOrderValue) [][]*CompositeUTXO {
		allFundingUTXOs := make([][]*CompositeUTXO, 0, len(orderedValues))
		remainingUTXOs := utxos
		var totalFunded uint64
		for _, value := range orderedValues {
			enough := c.orderEnough(value.Value, value.MaxSwapCount, maxFeeRate, false)

			var fundingUTXOs []*CompositeUTXO
			if maxLock > 0 {
				if maxLock < totalFunded {
					// Should never happen unless there is a bug in leastOverFundWithLimit
					c.log.Errorf("maxLock < totalFunded. %d < %d", maxLock, totalFunded)
					return allFundingUTXOs
				}
				fundingUTXOs = leastOverFundWithLimit(enough, maxLock-totalFunded, remainingUTXOs)
			} else {
				fundingUTXOs = leastOverFund(enough, remainingUTXOs)
			}
			if len(fundingUTXOs) == 0 {
				return allFundingUTXOs
			}
			totalFunded += SumUTXOs(fundingUTXOs)
			if totalFunded > avail-keep {
				return allFundingUTXOs
			}
			allFundingUTXOs = append(allFundingUTXOs, fundingUTXOs)
			remainingUTXOs = UTxOSetDiff(remainingUTXOs, fundingUTXOs)
		}
		return allFundingUTXOs
	}

	returnValues := func(allFundingUTXOs [][]*CompositeUTXO) (coins []asset.Coins, redeemScripts [][]dex.Bytes, fundingCoins map[OutPoint]*UTxO, spents []*Output, err error) {
		coins = make([]asset.Coins, len(allFundingUTXOs))
		fundingCoins = make(map[OutPoint]*UTxO)
		spents = make([]*Output, 0, len(allFundingUTXOs))
		redeemScripts = make([][]dex.Bytes, len(allFundingUTXOs))
		for i, fundingUTXOs := range allFundingUTXOs {
			coins[i] = make(asset.Coins, len(fundingUTXOs))
			redeemScripts[i] = make([]dex.Bytes, len(fundingUTXOs))
			for j, output := range fundingUTXOs {
				coins[i][j] = NewOutput(output.TxHash, output.Vout, output.Amount)
				fundingCoins[OutPoint{TxHash: *output.TxHash, Vout: output.Vout}] = &UTxO{
					TxHash:  output.TxHash,
					Vout:    output.Vout,
					Amount:  output.Amount,
					Address: output.Address,
				}
				spents = append(spents, NewOutput(output.TxHash, output.Vout, output.Amount))
				redeemScripts[i][j] = output.RedeemScript
			}
		}
		return
	}

	// Attempt to fund all orders by selecting the order that requires the least
	// over funding, removing the funding utxos from the set of available utxos,
	// and continuing until all orders are funded.
	allFundingUTXOs := fundAllOrders()
	if allFundingUTXOs != nil {
		return returnValues(allFundingUTXOs)
	}

	// Return nil if a split is allowed. There is no need to fund in priority
	// order if a split will be done regardless.
	if splitAllowed {
		return returnValues([][]*CompositeUTXO{})
	}

	// If could not fully fund, fund as much as possible in the priority
	// order.
	allFundingUTXOs = fundInOrder(values)
	return returnValues(allFundingUTXOs)
}

// SpendableUTXOs filters the RPC utxos for those that are spendable with
// regards to the DEX's configuration, and considered safe to spend according to
// confirmations and coin source. The UTXOs will be sorted by ascending value.
// spendableUTXOs should only be called with the fundingMtx RLock'ed.
func (c *CoinManager) SpendableUTXOs(confs uint32) ([]*CompositeUTXO, map[OutPoint]*CompositeUTXO, uint64, error) {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	return c.spendableUTXOs(confs)
}

func (c *CoinManager) spendableUTXOs(confs uint32) ([]*CompositeUTXO, map[OutPoint]*CompositeUTXO, uint64, error) {
	unspents, err := c.listUnspent()
	if err != nil {
		return nil, nil, 0, err
	}

	utxos, utxoMap, sum, err := ConvertUnspent(confs, unspents, c.chainParams)
	if err != nil {
		return nil, nil, 0, err
	}

	var relock []*Output
	var i int
	for _, utxo := range utxos {
		// Guard against inconsistencies between the wallet's view of
		// spendable unlocked UTXOs and ExchangeWallet's. e.g. User manually
		// unlocked something or even restarted the wallet software.
		pt := NewOutPoint(utxo.TxHash, utxo.Vout)
		if c.lockedOutputs[pt] != nil {
			c.log.Warnf("Known order-funding coin %s returned by listunspent!", pt)
			delete(utxoMap, pt)
			relock = append(relock, &Output{pt, utxo.Amount})
		} else { // in-place filter maintaining order
			utxos[i] = utxo
			i++
		}
	}
	if len(relock) > 0 {
		if err = c.lockUnspent(false, relock); err != nil {
			c.log.Errorf("Failed to re-lock funding coins with wallet: %v", err)
		}
	}
	utxos = utxos[:i]
	return utxos, utxoMap, sum, nil
}

// ReturnCoins makes the locked utxos available for use again.
func (c *CoinManager) ReturnCoins(unspents asset.Coins) error {
	if unspents == nil { // not just empty to make this harder to do accidentally
		c.log.Debugf("Returning all coins.")
		c.mtx.Lock()
		defer c.mtx.Unlock()
		if err := c.lockUnspent(true, nil); err != nil {
			return err
		}
		c.lockedOutputs = make(map[OutPoint]*UTxO)
		return nil
	}
	if len(unspents) == 0 {
		return fmt.Errorf("cannot return zero coins")
	}

	ops := make([]*Output, 0, len(unspents))
	c.log.Debugf("returning coins %s", unspents)
	c.mtx.Lock()
	defer c.mtx.Unlock()
	for _, unspent := range unspents {
		op, err := ConvertCoin(unspent)
		if err != nil {
			return fmt.Errorf("error converting coin: %w", err)
		}
		ops = append(ops, op)
	}
	if err := c.lockUnspent(true, ops); err != nil {
		return err // could it have unlocked some of them? we may want to loop instead if that's the case
	}
	for _, op := range ops {
		delete(c.lockedOutputs, op.Pt)
	}
	return nil
}

// ReturnOutPoint makes the UTXO represented by the OutPoint available for use
// again.
func (c *CoinManager) ReturnOutPoint(pt OutPoint) error {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	if err := c.lockUnspent(true, []*Output{NewOutput(&pt.TxHash, pt.Vout, 0)}); err != nil {
		return err // could it have unlocked some of them? we may want to loop instead if that's the case
	}
	delete(c.lockedOutputs, pt)
	return nil
}

// FundingCoins attempts to find the specified utxos and locks them.
func (c *CoinManager) FundingCoins(ids []dex.Bytes) (asset.Coins, error) {
	// First check if we have the coins in cache.
	coins := make(asset.Coins, 0, len(ids))
	notFound := make(map[OutPoint]bool)
	c.mtx.Lock()
	defer c.mtx.Unlock() // stay locked until we update the map at the end
	for _, id := range ids {
		txHash, vout, err := decodeCoinID(id)
		if err != nil {
			return nil, err
		}
		pt := NewOutPoint(txHash, vout)
		fundingCoin, found := c.lockedOutputs[pt]
		if found {
			coins = append(coins, NewOutput(txHash, vout, fundingCoin.Amount))
			continue
		}
		notFound[pt] = true
	}
	if len(notFound) == 0 {
		return coins, nil
	}

	// Check locked outputs for not found coins.
	lockedOutpoints, err := c.listLocked()
	if err != nil {
		return nil, err
	}

	for _, rpcOP := range lockedOutpoints {
		txHash, err := chainhash.NewHashFromStr(rpcOP.TxID)
		if err != nil {
			return nil, fmt.Errorf("error decoding txid from rpc server %s: %w", rpcOP.TxID, err)
		}
		pt := NewOutPoint(txHash, rpcOP.Vout)
		if !notFound[pt] {
			continue // unrelated to the order
		}

		txOut, err := c.getTxOut(txHash, rpcOP.Vout)
		if err != nil {
			return nil, err
		}
		if txOut == nil {
			continue
		}
		if txOut.Value <= 0 {
			c.log.Warnf("Invalid value %v for %v", txOut.Value, pt)
			continue // try the listunspent output
		}
		_, addrs, _, err := txscript.ExtractPkScriptAddrs(txOut.PkScript, c.chainParams)
		if err != nil {
			c.log.Warnf("Invalid pkScript for %v: %v", pt, err)
			continue
		}
		if len(addrs) != 1 {
			c.log.Warnf("pkScript for %v contains %d addresses instead of one", pt, len(addrs))
			continue
		}
		addrStr, err := c.stringAddr(addrs[0])
		if err != nil {
			c.log.Errorf("Failed to stringify address %v (default encoding): %v", addrs[0], err)
			addrStr = addrs[0].String() // may or may not be able to retrieve the private keys by address!
		}
		utxo := &UTxO{
			TxHash:  txHash,
			Vout:    rpcOP.Vout,
			Address: addrStr, // for retrieving private key by address string
			Amount:  uint64(txOut.Value),
		}
		coin := NewOutput(txHash, rpcOP.Vout, uint64(txOut.Value))
		coins = append(coins, coin)
		c.lockedOutputs[pt] = utxo
		delete(notFound, pt)
		if len(notFound) == 0 {
			return coins, nil
		}
	}

	// Some funding coins still not found after checking locked outputs.
	// Check wallet unspent outputs as last resort. Lock the coins if found.
	_, utxoMap, _, err := c.spendableUTXOs(0)
	if err != nil {
		return nil, err
	}
	coinsToLock := make([]*Output, 0, len(notFound))
	for pt := range notFound {
		utxo, found := utxoMap[pt]
		if !found {
			return nil, fmt.Errorf("funding coin not found: %s", pt.String())
		}
		c.lockedOutputs[pt] = utxo.UTxO
		coin := NewOutput(utxo.TxHash, utxo.Vout, utxo.Amount)
		coins = append(coins, coin)
		coinsToLock = append(coinsToLock, coin)
		delete(notFound, pt)
	}
	c.log.Debugf("Locking funding coins that were unlocked %v", coinsToLock)
	err = c.lockUnspent(false, coinsToLock)
	if err != nil {
		return nil, err
	}
	return coins, nil
}

// LockUTXOs locks the specified utxos.
// TODO: Move lockUnspent calls into this method instead of the caller doing it
// at every callsite, and because that's what we do with unlocking.
func (c *CoinManager) LockUTXOs(utxos []*UTxO) {
	c.mtx.Lock()
	for _, utxo := range utxos {
		c.lockedOutputs[NewOutPoint(utxo.TxHash, utxo.Vout)] = utxo
	}
	c.mtx.Unlock()
}

// LockOutputsMap locks the utxos in the provided mapping.
func (c *CoinManager) LockOutputsMap(utxos map[OutPoint]*UTxO) {
	c.mtx.Lock()
	maps.Copy(c.lockedOutputs, utxos)
	c.mtx.Unlock()
}

// UnlockOutPoints unlocks the utxos represented by the provided outpoints.
func (c *CoinManager) UnlockOutPoints(pts []OutPoint) {
	c.mtx.Lock()
	for _, pt := range pts {
		delete(c.lockedOutputs, pt)
	}
	c.mtx.Unlock()
}

// LockedOutput returns the currently locked utxo represented by the provided
// outpoint, or nil if there is no record of the utxo in the local map.
func (c *CoinManager) LockedOutput(pt OutPoint) *UTxO {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	return c.lockedOutputs[pt]
}

func ConvertUnspent(confs uint32, unspents []*ListUnspentResult, chainParams *chaincfg.Params) ([]*CompositeUTXO, map[OutPoint]*CompositeUTXO, uint64, error) {
	sort.Slice(unspents, func(i, j int) bool { return unspents[i].Amount < unspents[j].Amount })
	var sum uint64
	utxos := make([]*CompositeUTXO, 0, len(unspents))
	utxoMap := make(map[OutPoint]*CompositeUTXO, len(unspents))
	for _, txout := range unspents {
		if txout.Confirmations >= confs && txout.Safe() && txout.Spendable {
			txHash, err := chainhash.NewHashFromStr(txout.TxID)
			if err != nil {
				return nil, nil, 0, fmt.Errorf("error decoding txid in ListUnspentResult: %w", err)
			}

			nfo, err := dexbtc.InputInfo(txout.ScriptPubKey, txout.RedeemScript, chainParams)
			if err != nil {
				if errors.Is(err, dex.UnsupportedScriptError) {
					continue
				}
				return nil, nil, 0, fmt.Errorf("error reading asset info: %w", err)
			}
			if nfo.ScriptType == dexbtc.ScriptUnsupported || nfo.NonStandardScript {
				// InputInfo sets NonStandardScript for P2SH with non-standard
				// redeem scripts. Don't return these since they cannot fund
				// arbitrary txns.
				continue
			}
			utxo := &CompositeUTXO{
				UTxO: &UTxO{
					TxHash:  txHash,
					Vout:    txout.Vout,
					Address: txout.Address,
					Amount:  toSatoshi(txout.Amount),
				},
				Confs:        txout.Confirmations,
				RedeemScript: txout.RedeemScript,
				Input:        nfo,
			}
			utxos = append(utxos, utxo)
			utxoMap[NewOutPoint(txHash, txout.Vout)] = utxo
			sum += toSatoshi(txout.Amount)
		}
	}
	return utxos, utxoMap, sum, nil
}

func TryFund(
	utxos []*CompositeUTXO,
	enough EnoughFunc,
) (
	sum, extra, size uint64,
	coins asset.Coins,
	fundingCoins map[OutPoint]*UTxO,
	redeemScripts []dex.Bytes,
	spents []*Output,
	err error,
) {

	fundingCoins = make(map[OutPoint]*UTxO)

	isEnoughWith := func(count int, unspent *CompositeUTXO) bool {
		ok, _ := enough(uint64(count), size+uint64(unspent.Input.VBytes()), sum+unspent.Amount)
		return ok
	}

	addUTXO := func(unspent *CompositeUTXO) {
		v := unspent.Amount
		op := NewOutput(unspent.TxHash, unspent.Vout, v)
		coins = append(coins, op)
		redeemScripts = append(redeemScripts, unspent.RedeemScript)
		spents = append(spents, op)
		size += uint64(unspent.Input.VBytes())
		fundingCoins[op.Pt] = unspent.UTxO
		sum += v
	}

	tryUTXOs := func(minconf uint32) bool {
		sum, size = 0, 0
		coins, spents, redeemScripts = nil, nil, nil
		fundingCoins = make(map[OutPoint]*UTxO)

		okUTXOs := make([]*CompositeUTXO, 0, len(utxos)) // over-allocate
		for _, cu := range utxos {
			if cu.Confs >= minconf {
				okUTXOs = append(okUTXOs, cu)
			}
		}

		for {
			// If there are none left, we don't have enough.
			if len(okUTXOs) == 0 {
				return false
			}

			// Check if the largest output is too small.
			lastUTXO := okUTXOs[len(okUTXOs)-1]
			if !isEnoughWith(1, lastUTXO) {
				addUTXO(lastUTXO)
				okUTXOs = okUTXOs[0 : len(okUTXOs)-1]
				continue
			}

			// We only need one then. Find it.
			idx := sort.Search(len(okUTXOs), func(i int) bool {
				return isEnoughWith(1, okUTXOs[i])
			})
			// No need to check idx == len(okUTXOs). We already verified that the last
			// utxo passes above.
			addUTXO(okUTXOs[idx])
			_, extra = enough(uint64(len(coins)), size, sum)
			return true
		}
	}

	// First try with confs>0, falling back to allowing 0-conf outputs.
	if !tryUTXOs(1) {
		if !tryUTXOs(0) {
			return 0, 0, 0, nil, nil, nil, nil, fmt.Errorf("not enough to cover requested funds. "+
				"%d available in %d UTXOs", amount(sum), len(coins))
		}
	}

	return
}
