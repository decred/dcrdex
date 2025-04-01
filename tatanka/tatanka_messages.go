// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package tatanka

import (
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/tatanka/mj"
	"decred.org/dcrdex/tatanka/tanka"
)

// handleInboundTatankaConnect handles an inbound tatanka connection.
func (t *Tatanka) handleInboundTatankaConnect(cl tanka.Sender, msg *msgjson.Message) *msgjson.Error {
	var cfg mj.TatankaConfig
	if err := msg.Unmarshal(&cfg); err != nil {
		return msgjson.NewError(mj.ErrBadRequest, "unmarshal error: %v", err)
	}

	if _, found := t.whitelist[cfg.ID]; !found {
		return msgjson.NewError(mj.ErrAuth, "not whitelisted")
	}

	p, err := t.db.Peer(cfg.ID)
	if err != nil {
		return msgjson.NewError(mj.ErrInternal, "error finding peer: %v", err)
	}

	if err := mj.CheckSig(msg, p.PubKey); err != nil {
		return msgjson.NewError(mj.ErrAuth, "bad sig")
	}

	// if calcTier(rep, p.BondTier()) <= 0 {
	// 	return msgjson.NewError(mj.ErrAuth, "denying inbound banned tatanka node %q", p.ID)
	// }

	cfgMsg := mj.MustNotification(mj.RouteTatankaConfig, t.generateConfig(p.BondTier()))
	if err := t.send(cl, cfgMsg); err != nil {
		peerID := cl.PeerID()
		t.log.Errorf("error sending configuration to connecting tatanka %q", dex.Bytes(peerID[:]))
		cl.Disconnect()
		return nil // don't bother
	}

	// TODO: Resolve our own bond tier with the tatanka node.
	// myTier := t.bondTier.Load()
	// if conn.BondTier != myTier {
	// 	bonds, err := t.db.GetBonds(t.id)
	// 	if err != nil {
	// 		t.log.Errorf("Error getting bonds from DB: %v", err)
	// 		return msgjson.NewError(mj.ErrInternal, "internal error")
	// 	}
	// 	var bondTier uint64
	// 	for _, b := range bonds {
	// 		bondTier += b.Strength
	// 	}
	// 	if conn.BondTier != myTier {
	// 		bondsUpdate := mj.MustNotification(mj.RouteBonds, bonds)
	// 		if err := cl.Send(bondsUpdate); err != nil {
	// 			t.log.Errorf("Error sending bonds update to %s during connect: %w", p.ID, err)
	// 			cl.Disconnect()
	// 			return nil
	// 		}
	// 	}

	// }

	cl.SetPeerID(cfg.ID)

	t.tatankasMtx.Lock()
	// TODO: Track peer reconnections and ban peer if on a runaway.
	if _, exists := t.tatankas[p.ID]; exists {
		t.log.Debugf("Connecting Tatanka node %s replaces already connected node", cl.PeerID())
	}

	pp := &peer{Peer: p, Sender: cl, rrs: make(map[tanka.PeerID]*tanka.Reputation)}

	// TODO: Check Tatanka Node reputation too
	// if pp.banned() {
	// 	t.tatankasMtx.Unlock()
	// 	return msgjson.NewError(mj.ErrAuth, "inbound peer %q is banned", p.ID)
	// }

	tt := &remoteTatanka{peer: pp}
	tt.cfg.Store(&cfg)
	t.tatankas[cfg.ID] = tt
	t.tatankasMtx.Unlock()

	t.sendResult(cl, msg.ID, true)

	return nil
}

type tatankaRequestHandler = func(tt *remoteTatanka, msg *msgjson.Message) *msgjson.Error
type tatankaNotificationHandler = func(tt *remoteTatanka, msg *msgjson.Message)

// handleTatankaMessage handles all messages from remote tatanka nodes except
// for mj.RouteTatankaConnect. The node is expected to already be connected.
// handleTatankaMessage perfoms some initial  handling of the message, like
// fetching the *remoteTatanka and checking signatures, before finding calling
// the appropriate handler.
func (t *Tatanka) handleTatankaMessage(cl tanka.Sender, msg *msgjson.Message) *msgjson.Error {
	if t.log.Level() == dex.LevelTrace {
		t.log.Tracef("Tatanka node handling message from remote tatanka. route = %s, payload = %s", msg.Route, string(msg.Payload))
	}

	peerID := cl.PeerID()
	tt := t.tatankaNode(peerID)
	if tt == nil {
		t.log.Errorf("%q (%d) message received from unknown outbound tatanka peer %q", msg.Route, msg.ID, peerID)
		return msgjson.NewError(mj.ErrAuth, "who even is this?")
	}

	if err := mj.CheckSig(msg, tt.PubKey); err != nil {
		t.log.Errorf("Signature error for %q message from %q: %v", msg.Route, tt.ID, err)
		return msgjson.NewError(mj.ErrSig, "signature doesn't check")
	}

	// The first message received after mj.RouteTatankaConnect must be the
	// config.
	if msg.Route != mj.RouteTatankaConfig && tt.cfg.Load() == nil {
		t.log.Errorf("message received from tatanka peer %q before configuration", peerID)
		tt.Disconnect()
		return msgjson.NewError(mj.ErrNoConfig, "send your configuration first")
	}

	switch handle := t.tatankaHandlers[msg.Route].(type) {
	case tatankaRequestHandler:
		return handle(tt, msg)
	case tatankaNotificationHandler:
		handle(tt, msg)
	}
	return nil
}

// handleRelayedTankagram handles a tankagram relayed from another node. The
// mesh is currently only capable of single-hop relays.
func (t *Tatanka) handleRelayedTankagram(tt *remoteTatanka, msg *msgjson.Message) *msgjson.Error {
	var gram *mj.Tankagram
	if err := msg.Unmarshal(&gram); err != nil {
		t.log.Errorf("Error unmarshaling tankagram from %s: %w", tt.ID, err)
		return msgjson.NewError(mj.ErrBadRequest, "unmarshal error")
	}

	t.clientMtx.RLock()
	c, found := t.clients[gram.To]
	t.clientMtx.RUnlock()
	if !found {
		t.sendResult(tt, msg.ID, &mj.TankagramResult{Result: mj.TRTNoPath})
		t.log.Warnf("Tankagram relay from %s to unknown client %s", tt.ID, gram.To)
		return nil
	}

	relayedMsg := mj.MustRequest(mj.RouteTankagram, gram)
	var resB dex.Bytes
	sent, clientErr, err := t.requestAnyOne([]tanka.Sender{c}, relayedMsg, &resB)
	if sent {
		t.sendResult(tt, msg.ID, &mj.TankagramResult{Result: mj.TRTTransmitted, EncryptedPayload: resB})
		return nil
	}
	if clientErr != nil {
		t.sendResult(tt, msg.ID, &mj.TankagramResult{Result: mj.TRTErrBadClient})
		return nil
	}
	if err != nil {
		t.log.Errorf("Error sending to local client %s: %v", gram.To, err)
		t.sendResult(tt, msg.ID, &mj.TankagramResult{Result: mj.TRTErrFromTanka})
		return nil
	}
	// We might get here if the context expires during the call to requestOne.
	return nil
}

// handlePathInquiry returns whether a particular client is connected to this
// node.
func (t *Tatanka) handlePathInquiry(tt *remoteTatanka, msg *msgjson.Message) *msgjson.Error {
	var inq mj.PathInquiry
	if err := msg.Unmarshal(&inq); err != nil {
		t.log.Errorf("Failed to unmarshal path inquiry from %s: %v", tt.ID, err)
	}

	t.clientMtx.RLock()
	_, found := t.clients[inq.ID]
	t.clientMtx.RUnlock()
	t.sendResult(tt, msg.ID, found)
	return nil
}

// handleNewRemoteClientNotification handles an mj.RouteNewClient notification,
// updating the t.remoteClients map.
func (t *Tatanka) handleNewRemoteClientNotification(tt *remoteTatanka, msg *msgjson.Message) {
	if t.skipRelay(msg) {
		return
	}

	var conn mj.Connect
	if err := msg.Unmarshal(&conn); err != nil {
		t.log.Errorf("error unmarshaling %s notification payload: %q", msg.Route, err)
		return
	}
	t.registerRemoteClient(tt.ID, conn.ID)
	t.clientMtx.RLock()
	var rep *tanka.Reputation
	c, found := t.clients[conn.ID]
	if found {
		rep = c.Reputation
	}
	t.clientMtx.RUnlock()
	if rep == nil {
		var err error
		rep, err = t.db.Reputation(conn.ID)
		if err != nil {
			t.log.Errorf("error getting reputation for %s from DB: %q", conn.ID, err)
			return
		}
	}
	t.sendResult(tt, msg.ID, rep)
}

// handleRemoteClientDisconnect handles an mj.RouteClientDisconnect
// notification, updating the t.remoteClients map.
func (t *Tatanka) handleRemoteClientDisconnect(tt *remoteTatanka, msg *msgjson.Message) {
	if t.skipRelay(msg) {
		return
	}

	var dconn mj.Disconnect
	if err := msg.Unmarshal(&dconn); err != nil {
		t.log.Errorf("error unmarshaling %s notification payload: %q", msg.Route, err)
		return
	}

	job := &clientJob{
		task: &clientJobRemoteDisconnect{
			clientID: dconn.ID,
			tankaID:  tt.ID,
		},
		res: make(chan interface{}, 1),
	}
	t.clientJobs <- job
	<-job.res
}

// handleTatankaConfig handles the mj.RouteTatankaConfig notification,
// storing a remote tatanka node's updated configuration info.
func (t *Tatanka) handleTatankaConfig(tt *remoteTatanka, msg *msgjson.Message) {
	peerID := tt.PeerID()

	var cfg mj.TatankaConfig
	if err := msg.Unmarshal(&cfg); err != nil {
		tt.Disconnect()
		t.log.Errorf("failed to parse tatanka config from %q: %w", peerID, err)
		return
	}

	tt.cfg.Store(&cfg)
}

// handleRelayBroadcast distributes the broadcast to any locally connected
// subscribers.
func (t *Tatanka) handleRelayBroadcast(tt *remoteTatanka, msg *msgjson.Message) {
	if t.skipRelay(msg) {
		return
	}

	var bcast *mj.Broadcast
	if err := msg.Unmarshal(&bcast); err != nil || bcast == nil || bcast.Topic == "" {
		t.log.Errorf("error unmarshaling broadcast from %s: %w", tt.ID, err)
		return
	}

	if time.Since(bcast.Stamp) > tanka.EpochLength || time.Until(bcast.Stamp) > tanka.EpochLength {
		t.log.Errorf("Ignoring relayed broadcast with old stamp received from %s", tt.ID)
		return
	}

	t.registerRemoteClient(tt.ID, tt.ID)

	if msgErr := t.distributeBroadcastedMessage(bcast, false); msgErr != nil {
		t.log.Errorf("error distributing broadcast: %v", msgErr)
		return
	}
}

func (t *Tatanka) handleShareScore(tt *remoteTatanka, msg *msgjson.Message) {
	var ss mj.SharedScore
	if err := msg.Unmarshal(&ss); err != nil {
		t.log.Errorf("error unmarshaling shared score: %v", err)
		return
	}
	if err := t.db.SetScore(ss.Scored, ss.Scorer, ss.Score, time.Now()); err != nil {
		return
	}
	t.clientMtx.RLock()
	c, found := t.clients[ss.Scored]
	t.clientMtx.RUnlock()
	if !found {
		return
	}

	rep, err := t.db.Reputation(ss.Scored)
	if err != nil {
		t.log.Errorf("error updating reputation after shared score update: %v", err)
		return
	}

	c.mtx.Lock()
	c.Reputation = rep
	c.rrs[tt.ID] = ss.Reputation
	c.mtx.Unlock()
}
