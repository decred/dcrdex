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
	RemovePeer(addr string) error
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

func (s *SPVPeerManager) connectedPeers() map[string]struct{} {
	peers := s.cs.Peers()
	connectedPeers := make(map[string]struct{}, len(peers))
	for _, peer := range peers {
		connectedPeers[peer.Addr()] = struct{}{}
	}
	return connectedPeers
}

// Peers returns the list of peers that the wallet is connected to. It also
// returns the peers that the user added that the wallet may not currently
// be connected to.
func (s *SPVPeerManager) Peers() ([]*asset.WalletPeer, error) {
	s.peersMtx.RLock()
	defer s.peersMtx.RUnlock()

	connectedPeers := s.connectedPeers()

	walletPeers := make([]*asset.WalletPeer, 0, len(connectedPeers))

	for originalAddr, peer := range s.peers {
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
func (s *SPVPeerManager) resolveAddress(addr string) (string, error) {
	host, strPort, err := net.SplitHostPort(addr)
	if err != nil {
		switch err.(type) {
		case *net.AddrError:
			host = addr
			strPort = s.defaultPort
		default:
			return "", err
		}
	}

	// Tor addresses cannot be resolved to an IP, so just return onionAddr
	// instead.
	if strings.HasSuffix(host, ".onion") {
		return addr, nil
	}

	ips, err := net.LookupIP(host)
	if err != nil {
		return "", err
	}
	if len(ips) == 0 {
		return "", fmt.Errorf("no addresses found for %s", host)
	}

	var ip string
	if host == "localhost" && len(ips) > 1 {
		ip = "127.0.0.1"
	} else {
		ip = ips[0].String()
	}

	return net.JoinHostPort(ip, strPort), nil
}

// peerWithResolvedAddress checks to see if there is a peer with a resolved
// address in s.peers, and if so, returns the address that was user to add
// the peer.
func (s *SPVPeerManager) peerWithResolvedAddr(resolvedAddr string) (string, bool) {
	for originalAddr, peer := range s.peers {
		if peer.resolvedName == resolvedAddr {
			return originalAddr, true
		}
	}
	return "", false
}

// loadSavedPeersFromFile returns the contents of dexc-peers.json.
func (s *SPVPeerManager) loadSavedPeersFromFile() (map[string]peerSource, error) {
	content, err := os.ReadFile(s.savedPeersFilePath)
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

// writeSavedPeersToFile replaces the contents of dexc-peers.json.
func (s *SPVPeerManager) writeSavedPeersToFile(peers map[string]peerSource) error {
	content, err := json.Marshal(peers)
	if err != nil {
		return err
	}
	return os.WriteFile(s.savedPeersFilePath, content, 0644)
}

func (s *SPVPeerManager) addPeer(addr string, source peerSource, initialLoad bool) error {
	s.peersMtx.Lock()
	defer s.peersMtx.Unlock()

	resolvedAddr, err := s.resolveAddress(addr)
	if err != nil {
		if initialLoad {
			// If this is the initial load, we still want to add peers that are
			// not able to be connected to the peers map, in order to display them
			// to the user. If a user previously added a peer that originally connected
			// but now the address cannot be resolved to an IP, it should be displayed
			// that the wallet was unable to connect to that peer.
			s.peers[addr] = &walletPeer{source: source}
		}
		return fmt.Errorf("failed to resolve address: %v", err)
	}

	if duplicatePeer, found := s.peerWithResolvedAddr(resolvedAddr); found {
		return fmt.Errorf("%s and %s resolve to the same node", duplicatePeer, addr)
	}

	s.peers[addr] = &walletPeer{source: source, resolvedName: resolvedAddr}

	if !initialLoad {
		savedPeers, err := s.loadSavedPeersFromFile()
		if err != nil {
			s.log.Errorf("failed to load saved peers from file")
		} else {
			savedPeers[addr] = source
			err = s.writeSavedPeersToFile(savedPeers)
			if err != nil {
				s.log.Errorf("failed to add peer to saved peers file: %v")
			}
		}
	}

	connectedPeers := s.connectedPeers()
	_, connected := connectedPeers[resolvedAddr]
	if !connected {
		return s.cs.AddPeer(resolvedAddr)
	}

	return nil
}

// AddPeer connects to a new peer and stores it in the db.
func (s *SPVPeerManager) AddPeer(addr string) error {
	return s.addPeer(addr, added, false)
}

// RemovePeer disconnects from a peer added by the user and removes it from
// the db.
func (s *SPVPeerManager) RemovePeer(addr string) error {
	s.peersMtx.Lock()
	defer s.peersMtx.Unlock()

	peer, found := s.peers[addr]
	if !found {
		return fmt.Errorf("peer not found: %v", addr)
	}

	savedPeers, err := s.loadSavedPeersFromFile()
	if err != nil {
		return err
	}
	delete(savedPeers, addr)
	err = s.writeSavedPeersToFile(savedPeers)
	if err != nil {
		s.log.Errorf("failed to delete peer from saved peers file: %v")
	} else {
		delete(s.peers, addr)
	}

	connectedPeers := s.connectedPeers()
	_, connected := connectedPeers[peer.resolvedName]
	if connected {
		return s.cs.RemovePeer(peer.resolvedName)
	}

	return nil
}

// ConnectToInitialWalletPeers connects to the default peers and the peers
// that were added by the user and persisted in the db.
func (s *SPVPeerManager) ConnectToInitialWalletPeers() {
	for _, peer := range s.defaultPeers {
		err := s.addPeer(peer, defaultPeer, true)
		if err != nil {
			s.log.Errorf("failed to add default peer %s: %v", peer, err)
		}
	}

	savedPeers, err := s.loadSavedPeersFromFile()
	if err != nil {
		s.log.Errorf("failed to load saved peers from file: v", err)
		return
	}

	for addr := range savedPeers {
		err := s.addPeer(addr, added, true)
		if err != nil {
			s.log.Errorf("failed to add peer %s: %v", addr, err)
		}
	}
}
