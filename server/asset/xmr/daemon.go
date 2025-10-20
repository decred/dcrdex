package xmr

import (
	"context"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/server/asset"
	"github.com/bisoncraft/go-monero/rpc"
)

const BlockTickerInterval = 15 * time.Second // ~2m blocks

const (
	CakeMainnetTLS       = "https://xmr-node.cakewallet.com:18081"
	StackMainnetTLS      = "https://monero.stackwallet.com:18081"
	CakeMainnet          = "http://xmr-node.cakewallet.com:18081"
	MoneroDevsMainnetTLS = "https://node2.monerodevs.org:18089"
	MoneroDevsMainnet_1  = "http://node.monerodevs.org:18089"
	MoneroDevsMainnet_2  = "http://node2.monerodevs.org:18089"
	MoneroDevsMainnet_3  = "http://node3.monerodevs.org:18089"
	MoneroDevsStagenet_1 = "http://node.monerodevs.org:38089"
	MoneroDevsStagenet_2 = "http://node2.monerodevs.org:38089"
	MoneroDevsStagenet_3 = "http://node3.monerodevs.org:38089"
	DexSimnet            = "http://127.0.0.1:18081"
)

const (
	UserDaemonsFilename = "daemons.json"
)

// getTrustedDaemons gets known trusted daemon urls.
func getTrustedDaemons(net dex.Network, cli bool) []string {
	var td = make([]string, 0)
	switch net {
	case dex.Mainnet:
		if cli {
			td = append(td, CakeMainnetTLS)
			td = append(td, StackMainnetTLS)
			td = append(td, MoneroDevsMainnetTLS)
		}
		td = append(td, CakeMainnet)
		td = append(td, MoneroDevsMainnet_1)
		td = append(td, MoneroDevsMainnet_2)
		td = append(td, MoneroDevsMainnet_3)
	case dex.Testnet:
		td = append(td, MoneroDevsStagenet_1)
		td = append(td, MoneroDevsStagenet_2)
		td = append(td, MoneroDevsStagenet_3)
	case dex.Simnet:
		td = append(td, DexSimnet)
	}
	return td
}

// getInfo retrieves daemon info
func (b *Backend) getInfo() (*rpc.DemonGetInfoResponse, error) {
	giResp, err := b.daemon.DaemonGetInfo(b.ctx)
	if err != nil {
		return nil, err
	}
	return giResp, nil
}

// getFeeRate gives an estimation for fees (atoms) per byte.
func (b *Backend) getFeeRate() (uint64, error) {
	feeResp, err := b.daemon.DaemonGetFeeEstimate(b.ctx)
	if err != nil {
		b.log.Errorf("getFeeRate - %v", err)
		return 0, err
	}
	return feeResp.Fee, nil
}

func (b *Backend) monitorDaemon(ctx context.Context) {
	ticker := time.NewTicker(BlockTickerInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			b.checkDaemon()
		case <-ctx.Done():
			b.log.Trace("Ctx Done in monitorDaemon")
			return
		}
	}
}

func (b *Backend) checkDaemon() {
	info, err := b.getInfo()
	if err != nil {
		b.log.Errorf("DaemonGetInfo: %v", err)
		return
	}
	if info.Status != "OK" {
		b.log.Warnf("DaemonGetInfo: bad status: %s - expected OK", info.Status)
		return
	}
	b.daemonState.Lock()
	defer b.daemonState.Unlock()
	// b.log.Tracef("daemon %s --  height: %d, synced: %v, restricted: %v, untrusted: %v", b.daemonAddr, info.Height, info.Sychronized, info.Restricted, info.Untrusted)
	b.daemonState.synchronized = info.Sychronized
	// check height change
	heightNow := info.Height
	blockHashNow := info.TopBlockHash
	if heightNow > b.daemonState.height {
		b.log.Tracef("backend daemon %s -- tip change: %d (%s) => %d (%s)", b.daemonState.address, b.daemonState.height, b.daemonState.blockHash, heightNow, blockHashNow)
		b.daemonState.height = heightNow
		b.daemonState.blockHash = blockHashNow
		b.sendBlockUpdate(&asset.BlockUpdate{
			Err: nil,
		})
	}
}

// sendBlockUpdate sends the BlockUpdate to all subscribers.
func (b *Backend) sendBlockUpdate(u *asset.BlockUpdate) {
	b.blockChansMtx.RLock()
	defer b.blockChansMtx.RUnlock()
	for c := range b.blockChans {
		select {
		case c <- u:
		default:
			b.log.Error("failed to send block update on blocking channel")
		}
	}
}
