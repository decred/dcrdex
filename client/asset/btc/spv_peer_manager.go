// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package btc

import (
	"encoding/binary"
	"fmt"
	"net"
	"path/filepath"
	"sync"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/asset/kvdb"
	"decred.org/dcrdex/dex"
)

type peerSource uint16

const (
	added peerSource = iota
	defaultPeer
)

func (s peerSource) toAssetPeerSource() asset.PeerSource {
	switch s {
	case added:
		return asset.UserAdded
	case defaultPeer:
		return asset.WalletDefault
	}
	return asset.Discovered
}

func (s peerSource) MarshalBinary() ([]byte, error) {
	b := make([]byte, 16)
	binary.BigEndian.PutUint16(b, uint16(s))
	return b, nil
}

type walletPeer struct {
	source peerSource
	// resolvedNames is used to avoid adding duplicate peers
	resolvedNames []string
}

// PeerManagerChainService are the functions needed for an SPVPeerManager
// to communicate with a chain service.
type PeerManagerChainService interface {
	AddPeer(addr string) error
	RemoveNodeByAddr(addr string) error
	Peers() []SPVPeer
}

// SPVPeerManager implements peer management functionality for all bitcoin
// clone SPV wallets.
type SPVPeerManager struct {
	cs PeerManagerChainService

	peersMtx sync.RWMutex
	peers    map[string]*walletPeer
	peersDB  kvdb.KeyValueDB

	defaultPeers []string

	log dex.Logger
}

// Peers returns the list of peers that the wallet is connected to. It also
// returns the peers that the user added that the wallet may not currently
// be connected to.
func (w *SPVPeerManager) Peers() ([]*asset.WalletPeer, error) {
	w.peersMtx.RLock()
	defer w.peersMtx.RUnlock()

	peers := w.cs.Peers()
	peersMap := make(map[string]*asset.WalletPeer, len(peers))

	for _, peer := range peers {
		walletPeer := &asset.WalletPeer{
			Host:      peer.Addr(),
			Connected: true,
			Source:    asset.Discovered,
		}

		if peer, found := w.peers[peer.Addr()]; found {
			walletPeer.Source = peer.source.toAssetPeerSource()
		}

		peersMap[walletPeer.Host] = walletPeer
	}

	// Also return the peers that the user has added but are not connected
	for host, peer := range w.peers {
		if _, found := peersMap[host]; !found {
			walletPeer := &asset.WalletPeer{
				Host:   host,
				Source: peer.source.toAssetPeerSource(),
			}
			peersMap[host] = walletPeer
		}
	}

	walletPeers := make([]*asset.WalletPeer, 0, len(peersMap))
	for _, peer := range peersMap {
		walletPeers = append(walletPeers, peer)
	}

	return walletPeers, nil
}

// peerWithResolvedNames checks if there is a peer which has a resolved name
// which matches one of the resolvedNames passed into the function.
func (w *SPVPeerManager) peerWithResolvedNames(resolvedNames []string) (string, bool) {
	for name, peer := range w.peers {
		for _, peerResolvedName := range peer.resolvedNames {
			for _, newPeerResolvedName := range resolvedNames {
				if peerResolvedName == newPeerResolvedName {
					return name, true
				}
			}
		}
	}
	return "", false
}

// AddPeer connects to a new peer and stores it in the db.
func (w *SPVPeerManager) AddPeer(addr string) error {
	return w.addPeer(addr, added)
}

func (w *SPVPeerManager) addPeer(addr string, source peerSource) error {
	w.peersMtx.Lock()
	defer w.peersMtx.Unlock()

	walletPeer := &walletPeer{source: source}
	resolvedNames, err := resolvePeerNames(addr)
	if err != nil {
		w.log.Errorf("failed to resolve peer name: %v", err)
	} else {
		if duplicatePeer, found := w.peerWithResolvedNames(resolvedNames); found {
			return fmt.Errorf("%s and %s resolve to the same node", duplicatePeer, addr)
		}
		walletPeer.resolvedNames = resolvedNames
	}

	w.peers[addr] = walletPeer

	if source != defaultPeer {
		err = w.peersDB.Store([]byte(addr), added)
		if err != nil {
			w.log.Errorf("failed to store peer in db: %w", err)
		}
	}

	return w.cs.AddPeer(addr)
}

// RemovePeer disconnects from a peer added by the user and removes it from
// the db.
func (w *SPVPeerManager) RemovePeer(addr string) error {
	w.peersMtx.Lock()
	defer w.peersMtx.Unlock()

	err := w.peersDB.Delete([]byte(addr))
	if err != nil {
		w.log.Errorf("failed to delete host from peers DB: %v")
	} else {
		delete(w.peers, addr)
	}

	return w.cs.RemoveNodeByAddr(addr)
}

// resolvePeerNames returns the IP addresses that the address resolves
// to.
func resolvePeerNames(addr string) ([]string, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}

	ips, err := net.LookupIP(host)
	if err != nil {
		return nil, err
	}

	if len(ips) == 0 {
		return nil, fmt.Errorf("failed to resolve host: %v", host)
	}

	names := make([]string, 0, len(ips))
	for _, ip := range ips {
		switch {
		case ip.To4() != nil:
			names = append(names, fmt.Sprintf("%s:%s", ip.To4(), port))
		case ip.To16() != nil:
			names = append(names, fmt.Sprintf("[%s]:%s", ip.To16(), port))
		}
	}

	return names, nil
}

// ConnectToInitialWalletPeers connects to the default peers and the peers
// that were added by the user and persisted in the db.
func (w *SPVPeerManager) ConnectToInitialWalletPeers() {
	for _, peer := range w.defaultPeers {
		w.addPeer(peer, defaultPeer)
	}

	_ = w.peersDB.ForEach(func(k []byte, v []byte) error {
		addr := string(k)
		err := w.addPeer(addr, added)
		if err != nil {
			w.log.Errorf("failed to add peer %s: %v", addr, err)
		}
		return nil
	})
}

// NewSPVPeerManager creates a new SPVPeerManager.
func NewSPVPeerManager(cs PeerManagerChainService, defaultPeers []string, dir string, log dex.Logger) (*SPVPeerManager, error) {
	db, err := kvdb.NewFileDB(filepath.Join(dir, "peers.db"), log.SubLogger("PEERDB"))
	if err != nil {
		return nil, fmt.Errorf("failed to create peers db: %w", err)
	}

	return &SPVPeerManager{
		cs:           cs,
		defaultPeers: defaultPeers,
		peers:        make(map[string]*walletPeer),
		peersDB:      db,
		log:          log,
	}, nil
}
