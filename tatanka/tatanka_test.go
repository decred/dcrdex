package tatanka

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/tatanka/mj"
	"decred.org/dcrdex/tatanka/tanka"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

type tSender struct {
	id           tanka.PeerID
	disconnected bool
	responseErr  error
	responses    []*msgjson.Message
	recvd        []*msgjson.Message
	sendErr      error
}

func tNewSender(peerID tanka.PeerID) *tSender {
	return &tSender{id: peerID}
}

func (s *tSender) queueResponse(resp *msgjson.Message) {
	s.responses = append(s.responses, resp)
}

func (s *tSender) received() *msgjson.Message {
	if len(s.recvd) == 0 {
		return nil
	}
	msg := s.recvd[0]
	s.recvd = s.recvd[1:]
	return msg
}

func (s *tSender) Send(msg *msgjson.Message) error {
	s.recvd = append(s.recvd, msg)
	return s.sendErr
}
func (s *tSender) SendRaw(rawMsg []byte) error {
	msg, err := msgjson.DecodeMessage(rawMsg)
	if err != nil {
		return fmt.Errorf("tSender DecodeMessage error: %w", err)
	}
	s.recvd = append(s.recvd, msg)
	return s.sendErr
}
func (s *tSender) Request(msg *msgjson.Message, respHandler func(*msgjson.Message)) error {
	return s.RequestRaw(0, nil, respHandler)
}
func (s *tSender) RequestRaw(msgID uint64, rawMsg []byte, respHandler func(*msgjson.Message)) error {
	if s.responseErr != nil {
		return s.responseErr
	}
	if len(s.responses) == 0 {
		return errors.New("not test responses queued")
	}
	resp := s.responses[0]
	s.responses = s.responses[1:]
	go respHandler(resp) // goroutine to simulate Sender impls
	return nil
}
func (s *tSender) SetPeerID(tanka.PeerID) {}

func (s *tSender) PeerID() tanka.PeerID {
	return s.id
}
func (s *tSender) Disconnect() {
	s.disconnected = true
}

func tNewTatanka(id byte) (*Tatanka, func()) {
	priv, _ := secp256k1.GeneratePrivateKey()
	var peerID tanka.PeerID
	peerID[tanka.PeerIDLength-1] = id

	ctx, cancel := context.WithCancel(context.Background())

	srv := &Tatanka{
		ctx: ctx,
		net: dex.Simnet,
		log: dex.StdOutLogger("T", dex.LevelTrace),
		// db:            db,
		priv: priv,
		id:   peerID,
		// chains:        chains,
		tatankas:      make(map[tanka.PeerID]*remoteTatanka),
		clients:       make(map[tanka.PeerID]*client),
		remoteClients: make(map[tanka.PeerID]map[tanka.PeerID]struct{}),
		topics:        make(map[tanka.Topic]*Topic),
		recentRelays:  make(map[[32]byte]time.Time),
		clientJobs:    make(chan *clientJob, 128),
	}

	go srv.runRemoteClientsLoop(ctx)

	return srv, cancel
}

func tNewPeer(id byte) (*peer, *tSender) {
	var peerID tanka.PeerID
	peerID[tanka.PeerIDLength-1] = id
	p := &tanka.Peer{ID: peerID}
	s := tNewSender(peerID)
	return &peer{Peer: p, Sender: s, rrs: make(map[tanka.PeerID]*mj.RemoteReputation)}, s
}

func tNewRemoteTatanka(id byte) (*remoteTatanka, *tSender) {
	p, s := tNewPeer(id)
	return &remoteTatanka{peer: p}, s
}

func tNewClient(id byte) (*client, *tSender) {
	p, s := tNewPeer(id)
	return &client{peer: p}, s
}

func TestTankagrams(t *testing.T) {
	srv, shutdown := tNewTatanka(0)
	defer shutdown()

	tt, tts := tNewRemoteTatanka(1)
	srv.tatankas[tt.ID] = tt

	c0, c0s := tNewClient(2)
	srv.clients[c0.ID] = c0

	c1, c1s := tNewClient(3)
	srv.clients[c1.ID] = c1

	gram := &mj.Tankagram{
		From: c0.ID,
		To:   c1.ID,
	}
	msg := mj.MustRequest(mj.RouteTankagram, gram)
	var r mj.TankagramResult
	checkResponse := func(expResult mj.TankagramResultType) {
		t.Helper()
		if msgErr := srv.handleTankagram(c0, msg); msgErr != nil {
			t.Fatalf("Initial handleTankagram error: %v", msgErr)
		}
		respMsg := c0s.received()
		if respMsg == nil {
			t.Fatalf("No response received")
		}

		respMsg.UnmarshalResult(&r)
		if r.Result != expResult {
			t.Fatalf("Expected result %q, got %q", expResult, r.Result)
		}
	}

	// tankagram to locally connected client success
	responseB := dex.Bytes(encode.RandomBytes(10))
	resp := mj.MustResponse(msg.ID, responseB, nil)
	c1s.queueResponse(resp)
	checkResponse(mj.TRTTransmitted)
	if !bytes.Equal(r.Response, responseB) {
		t.Fatalf("wrong response")
	}

	// Bad response from client.
	resp, _ = msgjson.NewResponse(msg.ID, "zz", nil)
	c1s.queueResponse(resp)
	checkResponse(mj.TRTErrBadClient)

	// Client not responsive locally, but is remotely.
	c1s.responseErr = errors.New("test error")
	resp, _ = msgjson.NewResponse(msg.ID, &mj.TankagramResult{Result: mj.TRTTransmitted, Response: responseB}, nil)
	tts.queueResponse(resp)
	srv.remoteClients[c1.ID] = map[tanka.PeerID]struct{}{tt.ID: {}}
	checkResponse(mj.TRTTransmitted)
	if !bytes.Equal(r.Response, responseB) {
		t.Fatalf("wrong response")
	}
	c1s.responseErr = nil

	// Client not known locally, and is erroneous remotely.
	delete(srv.clients, c1.ID)
	resp, _ = msgjson.NewResponse(msg.ID, &mj.TankagramResult{Result: mj.TRTErrBadClient}, nil)
	tts.queueResponse(resp)
	checkResponse(mj.TRTErrBadClient)

	// Client not known locally or remotely
	delete(srv.remoteClients, c1.ID)
	checkResponse(mj.TRTNoPath)

	// Now we're the remote.

	// We know the client and transmit the relayed tankagram successfully.
	srv.clients[c1.ID] = c1
	resp, _ = msgjson.NewResponse(msg.ID, responseB, nil)
	c1s.queueResponse(resp)
	msg, _ = msgjson.NewRequest(mj.NewMessageID(), mj.RouteRelayTankagram, gram)
	srv.handleRelayedTankagram(tt, msg)
	respMsg := tts.received()
	if respMsg == nil {
		t.Fatalf("No response received")
	}
	respMsg.UnmarshalResult(&r)
	if r.Result != mj.TRTTransmitted {
		t.Fatalf("Expected result %q, got %q", mj.TRTTransmitted, r.Result)
	}
}
