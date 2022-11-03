// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package btc

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"decred.org/dcrdex/client/asset"
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

type walletPeer struct {
	source       peerSource
	resolvedName string
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

	savedPeersFilePath string

	defaultPeers []string

	defaultPort string

	log dex.Logger
}

// Peers returns the list of peers that the wallet is connected to. It also
// returns the peers that the user added that the wallet may not currently
// be connected to.
func (w *SPVPeerManager) Peers() ([]*asset.WalletPeer, error) {
	w.peersMtx.RLock()
	defer w.peersMtx.RUnlock()

	peers := w.cs.Peers()
	connectedPeers := make(map[string]interface{})
	for _, peer := range peers {
		connectedPeers[peer.Addr()] = struct{}{}
	}

	walletPeers := make([]*asset.WalletPeer, 0, len(connectedPeers))

	for originalAddr, peer := range w.peers {
		_, connected := connectedPeers[peer.resolvedName]
		delete(connectedPeers, peer.resolvedName)
		walletPeers = append(walletPeers, &asset.WalletPeer{
			Addr:      originalAddr,
			Connected: connected,
			Source:    peer.source.toAssetPeerSource(),
		})
	}

	for peer := range connectedPeers {
		walletPeers = append(walletPeers, &asset.WalletPeer{
			Addr:      peer,
			Connected: true,
			Source:    asset.Discovered,
		})
	}

	return walletPeers, nil
}

// resolveAddress resolves an address to ip:port. This is needed because neutrino
// internally resolves the address, and when neutrino is called to return its list
// of peers, it will return the resolved addresses. Therefore, we call neutrino
// with the resolved address, then we keep track of the mapping of address to
// resolved address in order to be able to display the address the user provided
// back to the user.
func (w *SPVPeerManager) resolveAddress(addr string) (string, error) {
	host, strPort, err := net.SplitHostPort(addr)
	if err != nil {
		switch err.(type) {
		case *net.AddrError:
			host = addr
			strPort = w.defaultPort
		default:
			return "", err
		}
	}

	// Tor addresses cannot be resolved to an IP, so just return onionAddr
	// instead.
	if strings.HasSuffix(host, ".onion") {
		return host, nil
	}

	// Attempt to look up an IP address associated with the parsed host.
	ips, err := net.LookupIP(host)
	if err != nil {
		return "", err
	}

	if len(ips) == 0 {
		return "", fmt.Errorf("no addresses found for %s", host)
	}

	port, err := strconv.Atoi(strPort)
	if err != nil {
		return "", err
	}

	return (&net.TCPAddr{
		IP:   ips[0],
		Port: port,
	}).String(), nil

}

// peerWithResolvedAddress checks to see if there is a peer with a resolved
// address in w.peers, and if so, returns the address that was user to add
// the peer.
func (w *SPVPeerManager) peerWithResolvedAddr(resolvedAddr string) (string, bool) {
	for originalAddr, peer := range w.peers {
		if peer.resolvedName == resolvedAddr {
			return originalAddr, true
		}
	}
	return "", false
}

// loadSavedPeersFromFile returns the contents of dexc-peers.json.
func (w *SPVPeerManager) loadSavedPeersFromFile() (map[string]peerSource, error) {
	content, err := os.ReadFile(w.savedPeersFilePath)
	if errors.Is(err, os.ErrNotExist) {
		return make(map[string]peerSource), nil
	}
	if err != nil {
		return nil, err
	}

	peers := make(map[string]peerSource)
	err = json.Unmarshal(content, &peers)
	if err != nil {
		return nil, err
	}

	return peers, nil
}

// loadSavedPeersFromFile replaces the contents of dexc-peers.json.
func (w *SPVPeerManager) writeSavedPeersToFile(peers map[string]peerSource) error {
	content, err := json.Marshal(peers)
	if err != nil {
		return err
	}
	return os.WriteFile(w.savedPeersFilePath, content, 0644)
}

func (w *SPVPeerManager) addPeer(addr string, source peerSource, initialLoad bool) error {
	w.peersMtx.Lock()
	defer w.peersMtx.Unlock()

	resolvedAddr, err := w.resolveAddress(addr)
	if err != nil {
		if initialLoad {
			// If this is the initial load, we still want to add peers that are
			// not able to be connected to the peers map, in order to display them
			// to the user. If a user previously added a peer that originally connected
			// but now the address cannot be resolved to an IP, it should be displayed
			// that the wallet was unable to connect to that peer.
			w.peers[addr] = &walletPeer{source: source, resolvedName: resolvedAddr}
		}
		return fmt.Errorf("failed to resolve address: %v", err)
	}

	if duplicatePeer, found := w.peerWithResolvedAddr(resolvedAddr); found {
		return fmt.Errorf("%s and %s resolve to the same node", duplicatePeer, addr)
	}

	w.peers[addr] = &walletPeer{source: source, resolvedName: resolvedAddr}

	if !initialLoad {
		savedPeers, err := w.loadSavedPeersFromFile()
		if err != nil {
			w.log.Errorf("failed to load saved peers from file")
		} else {
			savedPeers[addr] = source
			err = w.writeSavedPeersToFile(savedPeers)
			if err != nil {
				w.log.Errorf("failed to add peer to saved peers file: %v")
			}
		}
	}

	return w.cs.AddPeer(resolvedAddr)
}

// AddPeer connects to a new peer and stores it in the db.
func (w *SPVPeerManager) AddPeer(addr string) error {
	return w.addPeer(addr, added, false)
}

// RemovePeer disconnects from a peer added by the user and removes it from
// the db.
func (w *SPVPeerManager) RemovePeer(addr string) error {
	w.peersMtx.Lock()
	defer w.peersMtx.Unlock()

	peer, found := w.peers[addr]
	if !found {
		return fmt.Errorf("peer not found: %v", addr)
	}

	savedPeers, err := w.loadSavedPeersFromFile()
	if err != nil {
		return err
	}
	delete(savedPeers, addr)
	err = w.writeSavedPeersToFile(savedPeers)
	if err != nil {
		w.log.Errorf("failed to delete peer from saved peers file: %v")
	} else {
		delete(w.peers, addr)
	}

	return w.cs.RemoveNodeByAddr(peer.resolvedName)
}

// ConnectToInitialWalletPeers connects to the default peers and the peers
// that were added by the user and persisted in the db.
func (w *SPVPeerManager) ConnectToInitialWalletPeers() {
	for _, peer := range w.defaultPeers {
		w.addPeer(peer, defaultPeer, true)
	}

	savedPeers, err := w.loadSavedPeersFromFile()
	if err != nil {
		w.log.Errorf("failed to load saved peers from file: v", err)
		return
	}

	for addr := range savedPeers {
		err := w.addPeer(addr, added, true)
		if err != nil {
			w.log.Errorf("failed to add peer %s: %v", addr, err)
		}
	}
}

// NewSPVPeerManager creates a new SPVPeerManager.
func NewSPVPeerManager(cs PeerManagerChainService, defaultPeers []string, dir string, log dex.Logger, defaultPort string) *SPVPeerManager {
	return &SPVPeerManager{
		cs:                 cs,
		defaultPeers:       defaultPeers,
		peers:              make(map[string]*walletPeer),
		savedPeersFilePath: filepath.Join(dir, "dexc-peers.json"), // peers.json is used by neutrino
		log:                log,
		defaultPort:        defaultPort,
	}
}
