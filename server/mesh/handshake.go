// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mesh

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/server/db"
)

type frontierMessage struct {
	Tip     uint64    `json:"tip"`
	TipHash dex.Bytes `json:"tipHash"`
}

func fromFrontierMessage(msg frontierMessage) *db.EventLogPosition {
	return &db.EventLogPosition{
		Seq:     msg.Tip,
		TipHash: append([]byte(nil), msg.TipHash...),
	}
}

func toFrontierMessage(frontier *db.EventLogPosition) frontierMessage {
	return frontierMessage{
		Tip:     frontier.Seq,
		TipHash: dex.Bytes(frontier.TipHash),
	}
}

func validateFrontierMessage(frontier frontierMessage) error {
	if frontier.Tip == 0 {
		if len(frontier.TipHash) != 0 {
			return fmt.Errorf("zero event-log frontier hash length %d, want 0", len(frontier.TipHash))
		}
		return nil
	}
	if len(frontier.TipHash) != eventLogTipHashSize {
		return fmt.Errorf("event-log frontier hash length %d, want %d", len(frontier.TipHash), eventLogTipHashSize)
	}
	return nil
}

type helloMessage struct {
	NodeID       string          `json:"nodeID"`
	Role         helloRole       `json:"role"`
	Frontier     frontierMessage `json:"frontier"`
	CompatHash   dex.Bytes       `json:"compatHash"`
	ClientHost   string          `json:"clientHost,omitempty"`
	ClientCert   dex.Bytes       `json:"clientCert,omitempty"`
	CompatConfig *CompatConfig   `json:"compatConfig,omitempty"`
	Sig          dex.Bytes       `json:"sig,omitempty"`
}

// helloSignable is the signed subset of helloMessage (no CompatConfig, no Sig).
type helloSignable struct {
	NodeID     string          `json:"nodeID"`
	Role       helloRole       `json:"role"`
	Frontier   frontierMessage `json:"frontier"`
	CompatHash dex.Bytes       `json:"compatHash"`
	ClientHost string          `json:"clientHost,omitempty"`
	ClientCert dex.Bytes       `json:"clientCert,omitempty"`
}

func (h *helloMessage) signableBytes() ([]byte, error) {
	if h == nil {
		return nil, fmt.Errorf("nil hello payload")
	}
	return json.Marshal(helloSignable{
		NodeID:     h.NodeID,
		Role:       h.Role,
		Frontier:   h.Frontier,
		CompatHash: h.CompatHash,
		ClientHost: h.ClientHost,
		ClientCert: h.ClientCert,
	})
}

func (h *helloMessage) sign(s signer) error {
	hash, err := h.signableBytes()
	if err != nil {
		return err
	}
	sig, err := s.sign(hash)
	if err != nil {
		return err
	}
	h.Sig = sig
	return nil
}

func (h *helloMessage) verifySig(s signer) error {
	if h == nil {
		return fmt.Errorf("nil hello payload")
	}
	signable, err := h.signableBytes()
	if err != nil {
		return fmt.Errorf("failed to marshal hello signable: %w", err)
	}
	if !s.verify(h.Sig, signable) {
		return fmt.Errorf("invalid hello signature")
	}
	return nil
}

type helloResponse struct {
	Hello *helloMessage `json:"hello"`
	// Ancestor is meaningful only when the responder is strictly ahead.
	// True means the initiator's frontier is in the responder's history;
	// false means a fork.
	Ancestor bool `json:"ancestor,omitempty"`
	// NotAdopted means the responder kept an existing peer session instead of
	// this connection.
	NotAdopted bool `json:"notAdopted,omitempty"`
}

type decisionMessage struct {
	// Ancestor is true if the responder's frontier is in the initiator's history.
	Ancestor bool `json:"ancestor"`
}

type signer interface {
	sign(data []byte) ([]byte, error)
	verify(sig, data []byte) bool
}

type handshakeTransport interface {
	requestHello(ctx context.Context, req *helloMessage) (*helloResponse, error)
	requestDecision(ctx context.Context, req *decisionMessage) error
}

type handshakeResult struct {
	peerHello  *helloMessage
	progress   progressState
	clientHost string
	clientCert []byte
}

// handshakeService implements the mesh hello protocol. Two nodes maintain
// replicas of one logical event log; before serving together they must establish
// compatibility and whether their logs have diverged or not.
//
// Compatibility is determined by checking if the hash of the CompatConfigs are equal.
//
// Determining if the logs have diverged or not is done by comparing the frontiers. If
// the initiator is behind or the two nodes are equal, one round of messages is enough.
// If the initiator is ahead, they will need to check if the responder's frontier is
// part of their history, so a second round of messages is needed.
//
//	initiator                            responder
//	    |-- mesh_hello -------------------->|
//	    |<-- hello_response ----------------|  Ancestor only if responder is ahead
//	    |   only when the initiator is ahead:
//	    |-- mesh_hello_decision ------------>|
//	    |<-- ack ----------------------------|
type handshakeService struct {
	nodeID         string
	currentRole    func() helloRole
	signer         signer
	compat         *CompatSnapshot
	eventLogReader db.EventLogReader
	clientHost     string
	clientCert     []byte
	log            dex.Logger
}

func newHandshakeService(
	nodeID string,
	roleFn func() helloRole,
	signer signer,
	compat *CompatSnapshot,
	eventLogReader db.EventLogReader,
	clientHost string,
	clientCert []byte,
	log dex.Logger,
) *handshakeService {
	return &handshakeService{
		nodeID:         nodeID,
		currentRole:    roleFn,
		signer:         signer,
		compat:         compat,
		eventLogReader: eventLogReader,
		clientHost:     clientHost,
		clientCert:     append([]byte(nil), clientCert...),
		log:            log,
	}
}

// initiateHandshake is called to initiate a handshake with a peer.
// It sends a hello request, and if the initiator is ahead, they follow up with
// a decision request.
func (s *handshakeService) initiateHandshake(ctx context.Context, peer handshakeTransport) (*handshakeResult, error) {
	localFrontier, err := s.eventLogReader.EventLogFrontier(ctx)
	if err != nil {
		return nil, err
	}

	hello, err := s.signedHello(localFrontier)
	if err != nil {
		return nil, err
	}
	resp, err := peer.requestHello(ctx, hello)
	if err != nil {
		return nil, err
	}

	peerHello := resp.Hello
	if err := s.validatePeerHello(peerHello); err != nil {
		return nil, wrapPeerIncompatible(err)
	}
	if resp.NotAdopted {
		return nil, fmt.Errorf("%w: peer marked our hello not adopted", errPeerAlreadyConnected)
	}

	peerFrontier := fromFrontierMessage(peerHello.Frontier)
	var progress progressState
	switch {
	case localFrontier.Seq == peerFrontier.Seq:
		progress = equalProgress(localFrontier, peerFrontier)
	case localFrontier.Seq < peerFrontier.Seq:
		progress = progressFromAncestor(resp.Ancestor)
	default: // localFrontier.Seq > peerFrontier.Seq
		progress, err = s.sendDecision(ctx, peer, peerFrontier)
		if err != nil {
			return nil, err
		}
	}

	return &handshakeResult{
		peerHello:  peerHello,
		progress:   progress,
		clientHost: peerHello.ClientHost,
		clientCert: append([]byte(nil), peerHello.ClientCert...),
	}, nil
}

// sendDecision is used by the initiator to send a decision request to the responder.
func (s *handshakeService) sendDecision(ctx context.Context, peer handshakeTransport, peerFrontier *db.EventLogPosition) (progressState, error) {
	_, progress, err := compareEventLogFrontier(ctx, s.eventLogReader, peerFrontier)
	if err != nil {
		return progress, err
	}

	if err := peer.requestDecision(ctx, &decisionMessage{Ancestor: progress == progressLocalAhead}); err != nil {
		if progress == progressDiverged && ctx.Err() == nil {
			// We have already proven that the logs diverge. Report that result even if
			// notifying the peer fails, so this node halts regardless of which node dialed.
			s.log.Warnf("Mesh peer unreachable for diverged decision delivery: %v", err)
			return progress, nil
		}
		return progress, err
	}

	return progress, nil
}

// processHello processes the initiator's hello. If the initiator is ahead,
// the handshake waits for its decision message.
func (s *handshakeService) processHello(ctx context.Context, hello *helloMessage) (*helloResponse, *handshakeResult, error) {
	peerFrontier := fromFrontierMessage(hello.Frontier)
	localFrontier, progress, err := compareEventLogFrontier(ctx, s.eventLogReader, peerFrontier)
	if err != nil {
		return nil, nil, err
	}

	ourHello, err := s.signedHello(localFrontier)
	if err != nil {
		return nil, nil, err
	}

	resp := &helloResponse{Hello: ourHello, Ancestor: progress == progressLocalAhead}
	result := &handshakeResult{
		peerHello:  hello,
		progress:   progress,
		clientHost: hello.ClientHost,
		clientCert: append([]byte(nil), hello.ClientCert...),
	}
	return resp, result, nil
}

// signedHello builds and signs this node's hello.
func (s *handshakeService) signedHello(frontier *db.EventLogPosition) (*helloMessage, error) {
	hello := &helloMessage{
		NodeID:       s.nodeID,
		Role:         s.currentRole(),
		Frontier:     toFrontierMessage(frontier),
		CompatHash:   s.compat.Hash[:],
		CompatConfig: s.compat.Config,
		ClientHost:   s.clientHost,
		ClientCert:   append(dex.Bytes(nil), s.clientCert...),
	}
	if err := hello.sign(s.signer); err != nil {
		return nil, err
	}
	return hello, nil
}

func (s *handshakeService) validatePeerHello(hello *helloMessage) error {
	if hello == nil {
		return fmt.Errorf("nil hello payload")
	}
	if err := validatePeerNodeID(s.nodeID, hello.NodeID); err != nil {
		return err
	}
	if err := validatePeerRole(hello.Role); err != nil {
		return err
	}
	if err := validateFrontierMessage(hello.Frontier); err != nil {
		return fmt.Errorf("invalid peer frontier: %w", err)
	}
	if err := hello.verifySig(s.signer); err != nil {
		return err
	}
	return validatePeerCompat(s.compat, hello.CompatHash, hello.CompatConfig)
}

func validatePeerNodeID(localNodeID, peerNodeID string) error {
	if err := validateNodeID(peerNodeID); err != nil {
		return fmt.Errorf("invalid peer node ID: %w", err)
	}
	if localNodeID != "" && peerNodeID == localNodeID {
		return fmt.Errorf("peer node ID matches local node ID")
	}
	return nil
}

func validatePeerRole(role helloRole) error {
	if !role.valid() {
		return fmt.Errorf("invalid peer role %d", role)
	}
	return nil
}

func validatePeerCompat(localCompat *CompatSnapshot, peerHash dex.Bytes, peerConfig *CompatConfig) error {
	if localCompat == nil {
		return fmt.Errorf("nil local compatibility snapshot")
	}
	var peerHashArr [32]byte
	if len(peerHash) != len(peerHashArr) {
		return fmt.Errorf("invalid peer compatibility hash length %d", len(peerHash))
	}
	copy(peerHashArr[:], peerHash)

	if peerHashArr != localCompat.Hash {
		return fmt.Errorf("peer compatibility hash mismatch")
	}
	if peerConfig != nil {
		snap, err := NewCompatSnapshot(*peerConfig)
		if err != nil {
			return fmt.Errorf("invalid peer compatibility config: %w", err)
		}
		if snap.Hash != peerHashArr {
			return fmt.Errorf("peer compatibility config does not match peer compatibility hash")
		}
	}
	return nil
}

// errPeerBelowAnchor is returned when the peer's frontier is below the anchor
// at which this node's log begins. This node was seeded from a snapshot taken
// above the peer's tip. It cannot check whether the peer's log is an ancestor
// of its own, and the peer cannot subscribe to the event stream from that
// position. The peer must wipe its event sourced state and rejoin from a
// snapshot.
var errPeerBelowAnchor = errors.New("peer frontier is below this node's snapshot anchor")

func compareEventLogFrontier(ctx context.Context, reader db.EventLogReader, peer *db.EventLogPosition) (*db.EventLogPosition, progressState, error) {
	local, err := reader.EventLogFrontier(ctx)
	if err != nil {
		return nil, progressEqual, err
	}

	switch {
	case local.Seq == peer.Seq:
		return local, equalProgress(local, peer), nil
	case local.Seq < peer.Seq:
		return local, progressPeerAhead, nil
	}

	// local.Seq > peer.Seq

	if peer.Seq == 0 { // new peer
		return local, progressLocalAhead, nil
	}

	entries, err := reader.EventLogEntriesAfter(ctx, peer.Seq-1, 1)
	if err != nil {
		return local, progressEqual, err
	}

	if len(entries) == 0 || entries[0] == nil {
		return local, progressEqual, fmt.Errorf("no event log entry at seq %d", peer.Seq)
	}

	entry := entries[0]
	if entry.Seq != peer.Seq {
		// The first retained row is above the peer's position. If that row
		// is an anchor, this node's log begins there and the peer is below
		// it. Any other gap is an error.
		if entry.Seq > peer.Seq && db.IsEventLogAnchorKind(entry.Kind) {
			return local, progressEqual, fmt.Errorf("%w, this node's log begins at the seq %d anchor",
				errPeerBelowAnchor, entry.Seq)
		}
		return local, progressEqual, fmt.Errorf("no event log entry at seq %d", peer.Seq)
	}
	if bytes.Equal(entry.TipHash, peer.TipHash) {
		return local, progressLocalAhead, nil
	}
	return local, progressDiverged, nil
}

func equalProgress(local, peer *db.EventLogPosition) progressState {
	if bytes.Equal(local.TipHash, peer.TipHash) {
		return progressEqual
	}
	return progressDiverged
}

func progressFromAncestor(ancestor bool) progressState {
	if ancestor {
		return progressPeerAhead
	}
	return progressDiverged
}
