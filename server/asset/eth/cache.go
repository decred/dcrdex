// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

//go:build lgpl
// +build lgpl

package eth

import (
	"context"
	"fmt"
	"sync"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/server/asset"
	"github.com/ethereum/go-ethereum/common"
)

type hashN struct {
	height uint64
	hash   common.Hash
}

// The hashCache caches block information to detect reorgs and notify
// listeners of new blocks. All methods are concurrent safe unless specified
// otherwise.
type hashCache struct {
	// signalMtx locks the blockChans array.
	signalMtx  *sync.RWMutex
	blockChans map[chan *asset.BlockUpdate]struct{}

	mtx  sync.RWMutex
	best hashN

	log  dex.Logger
	node ethFetcher
}

// poll pulls the best hash from an eth node and compares that to a stored
// hash. If the same does nothing. If different, updates the stored hash and
// notifies listeners on block chans.
func (hc *hashCache) poll(ctx context.Context) {
	send := func(reorg bool, err error) {
		if err != nil {
			hc.log.Error(err)
		}
		hc.signalMtx.Lock()
		for c := range hc.blockChans {
			select {
			case c <- &asset.BlockUpdate{
				Reorg: reorg,
				Err:   err,
			}:
			default:
				hc.log.Error("failed to send block update on blocking channel")
			}
		}
		hc.signalMtx.Unlock()
	}
	hc.mtx.Lock()
	defer hc.mtx.Unlock()
	bhdr, err := hc.node.bestHeader(ctx)
	if err != nil {
		send(false, fmt.Errorf("error getting best block header from geth: %w", err))
		return
	}
	if bhdr.Hash() == hc.best.hash {
		// Same hash, nothing to do.
		return
	}
	update := func(reorg bool, fastBlocks bool) {
		hash := bhdr.Hash()
		height := bhdr.Number.Uint64()
		str := fmt.Sprintf("Tip change from %s (%d) to %s (%d).",
			hc.best.hash, hc.best.height, hash, height)
		switch {
		case reorg:
			str += " Detected reorg."
		case fastBlocks:
			str += " Fast blocks."
		}
		hc.log.Info(str)
		hc.best.hash = hash
		hc.best.height = height
	}
	if bhdr.ParentHash == hc.best.hash {
		// Sequencial hash, report a block update.
		update(false, false)
		send(false, nil)
		return
	}
	// Either a block was skipped or a reorg happened. We can only detect
	// the reorg if our last best header's hash has changed. Otherwise,
	// assume no reorg and report the new block change.
	//
	// headerByHeight will only return mainchain headers.
	hdr, err := hc.node.headerByHeight(ctx, hc.best.height)
	if err != nil {
		send(false, fmt.Errorf("error getting block header from geth: %w", err))
		return
	}
	if hdr.Hash() == hc.best.hash {
		// Our recorded hash is still on main chain so there is no reorg
		// that we know of. The chain has advanced more than one block.
		update(false, true)
		send(false, nil)
		return
	}
	// The block for our recorded hash was forked off and the chain had a
	// reorganization.
	update(true, false)
	send(true, nil)
}
