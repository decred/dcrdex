// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package tatanka

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/utils"
	"decred.org/dcrdex/tatanka/mj"
	"decred.org/dcrdex/tatanka/tanka"
)

// clientJob is a job for the remote clients loop.
type clientJob struct {
	task interface{}
	res  chan interface{}
}

// clientJobNewRemote is a clientJob task that adds a remote client to the
// remoteClients map.
type clientJobNewRemote struct {
	tankaID  tanka.PeerID
	clientID tanka.PeerID
}

// clientJobRemoteDisconnect is a clientJob task that removes a remote client
// from the remoteClients map.
type clientJobRemoteDisconnect clientJobNewRemote

// clientJobFindRemotes is a clientJob that produces a list of remote tatanka
// nodes to which the client is thought to be connected.
type clientJobFindRemotes struct {
	clientID tanka.PeerID
}

// runRemoteClientsLoop is a loop for reading and writing to the remoteClients
// map. I don't know if it's more performant, I'm just tired of mutex patterns.
func (t *Tatanka) runRemoteClientsLoop(ctx context.Context) {
	for {
		select {
		case job := <-t.clientJobs:
			switch task := job.task.(type) {
			case *clientJobNewRemote:
				srvs, found := t.remoteClients[task.clientID]
				if !found {
					srvs = make(map[tanka.PeerID]struct{})
					t.remoteClients[task.clientID] = srvs
				}
				if _, found = srvs[task.tankaID]; !found {
					srvs[task.tankaID] = struct{}{}
					if t.log.Level() <= dex.LevelTrace {
						t.log.Tracef("Indexing new remote client %s from %s", task.clientID, task.tankaID)
					}
				}
				job.res <- true
			case *clientJobRemoteDisconnect:
				srvs, found := t.remoteClients[task.clientID]
				if !found {
					return
				}
				delete(srvs, task.tankaID)
				if len(srvs) == 0 {
					delete(t.remoteClients, task.clientID)
				}
				job.res <- true
			case *clientJobFindRemotes:
				job.res <- utils.CopyMap(t.remoteClients[task.clientID])
			}
		case <-ctx.Done():
			return
		}
	}
}

// registerRemoteClient ensures the remote client is in the remoteClients map.
func (t *Tatanka) registerRemoteClient(tankaID, clientID tanka.PeerID) {
	job := &clientJob{
		task: &clientJobNewRemote{
			clientID: clientID,
			tankaID:  tankaID,
		},
		res: make(chan interface{}, 1),
	}
	t.clientJobs <- job
	select {
	case <-job.res:
	case <-t.ctx.Done():
	}
}

// handleClientConnect handles a new locally-connected client. checking
// reputation before adding the client to the map.
func (t *Tatanka) handleClientConnect(cl tanka.Sender, msg *msgjson.Message) *msgjson.Error {
	var conn *mj.Connect
	if err := msg.Unmarshal(&conn); err != nil {
		return msgjson.NewError(mj.ErrBadRequest, "error unmarshaling client connection configuration from %q: %v", cl.PeerID(), err)
	}

	p, rrs, err := t.loadPeer(conn.ID)
	if err != nil {
		return msgjson.NewError(mj.ErrInternal, "error getting peer info for peer %q: %v", conn.ID, err)
	}

	if err := mj.CheckSig(msg, p.PubKey); err != nil {
		return msgjson.NewError(mj.ErrAuth, "signature error: %v", err)
	}

	cl.SetPeerID(p.ID)

	pp := &peer{Peer: p, Sender: cl, rrs: rrs}
	if pp.banned() {
		return msgjson.NewError(mj.ErrBannned, "your tier is <= 0. post some bonds")
	}

	bondTier := p.BondTier()

	t.clientMtx.Lock()
	oldClient := t.clients[conn.ID]
	t.clients[conn.ID] = &client{peer: pp}
	t.clientMtx.Unlock()

	if oldClient != nil {
		t.log.Debugf("new connection for already connected client %q", conn.ID)
		oldClient.Disconnect()
	}

	t.sendResult(cl, msg.ID, t.generateConfig(bondTier))

	note := mj.MustNotification(mj.RouteNewClient, conn)
	for _, s := range t.tatankaNodes() {
		if err := t.send(s, note); err != nil {
			t.log.Errorf("error sharing new client info with tatanka node %q", s.ID)
		}
	}

	return nil
}

// handleClientMessage handles incoming message from locally-connected clients.
// All messages except for handleClientConnect and handlePostBond are handled
// here, with some common pre-processing and validation done before the
// subsequent route handler is called.
func (t *Tatanka) handleClientMessage(cl tanka.Sender, msg *msgjson.Message) *msgjson.Error {
	peerID := cl.PeerID()
	c := t.clientNode(peerID)
	if c == nil {
		t.log.Errorf("Ignoring message from unknown client %s", peerID)
		cl.Disconnect()
		return nil
	}

	if err := mj.CheckSig(msg, c.PubKey); err != nil {
		t.log.Errorf("Signature error for %q message from %q: %v", msg.Route, c.ID, err)
		return msgjson.NewError(mj.ErrSig, "signature doesn't check")
	}

	t.clientMtx.RLock()
	c, found := t.clients[peerID]
	t.clientMtx.RUnlock()
	if !found {
		t.log.Errorf("client %s sent a message requiring tier before connecting", peerID)
		return msgjson.NewError(mj.ErrAuth, "not connected")
	}

	switch msg.Type {
	case msgjson.Request:
		switch msg.Route {
		case mj.RouteSubscribe:
			return t.handleSubscription(c, msg)
		// case mj.RouteUnsubscribe:
		// 	return t.handleUnsubscribe(c, msg)
		case mj.RouteBroadcast:
			return t.handleBroadcast(c.peer, msg, true)
		case mj.RouteTankagram:
			return t.handleTankagram(c, msg)
		default:
			return msgjson.NewError(mj.ErrBadRequest, "unknown request route %q", msg.Route)
		}
	case msgjson.Notification:
		switch msg.Route {
		default:
			// TODO: What? Can't let this happen too much.
			return msgjson.NewError(mj.ErrBadRequest, "unknown notification route %q", msg.Route)
		}
	default:
		return msgjson.NewError(mj.ErrBadRequest, "unknown message type %d", msg.Type)
	}
}

// handlePostBond handles a new bond sent from a locally connected client.
// handlePostBond is the only client route than can be invoked before the user
// is bonded.
func (t *Tatanka) handlePostBond(cl tanka.Sender, msg *msgjson.Message) *msgjson.Error {
	var bonds []*tanka.Bond
	if err := msg.Unmarshal(&bonds); err != nil {
		t.log.Errorf("Bond-posting client sent a bad bond message: %v", err)
		return msgjson.NewError(mj.ErrBadRequest, "bad request")
	}

	if len(bonds) == 0 {
		t.log.Errorf("Bond-posting client sent zero bonds")
		return msgjson.NewError(mj.ErrBadRequest, "no bonds sent")
	}

	peerID := bonds[0].PeerID
	if peerID == (tanka.PeerID{}) {
		t.log.Errorf("Bond-posting client didn't provide a peer ID")
		return msgjson.NewError(mj.ErrBadRequest, "no peer ID")
	}
	for i := 1; i < len(bonds); i++ {
		if bonds[i].PeerID != bonds[0].PeerID {
			t.log.Errorf("Bond-posting client provided non uniform peer IDs")
			return msgjson.NewError(mj.ErrBadRequest, "mismatched peer IDs")
		}
	}

	var allBonds []*tanka.Bond
	for _, b := range bonds {
		if b == nil {
			t.log.Errorf("Bond-posting client %s sent a nil bond", peerID)
			return msgjson.NewError(mj.ErrBadRequest, "nil bond")
		}
		if b.Expiration.Before(time.Now()) {
			t.log.Errorf("Bond-posting client %q sent an expired bond", peerID)
			return msgjson.NewError(mj.ErrBadRequest, "bond already expired")
		}

		t.chainMtx.RLock()
		ch := t.chains[b.AssetID]
		t.chainMtx.RUnlock()
		if ch == nil {
			t.log.Errorf("Bond-posting client %q sent a bond for an unknown chain %d", peerID, b.AssetID)
			return msgjson.NewError(mj.ErrBadRequest, "unsupported asset")
		}

		if err := ch.CheckBond(b); err != nil {
			t.log.Errorf("Bond-posting client %q with bond %s didn't pass validation for chain %d: %v", peerID, b.CoinID, b.AssetID, err)
			return msgjson.NewError(mj.ErrBadRequest, "failed validation")
		}

		var err error
		allBonds, err = t.db.StoreBond(b)
		if err != nil {
			t.log.Errorf("Error storing bond for client %s in db: %v", peerID, err)
			return msgjson.NewError(mj.ErrInternal, "internal error")
		}

	}

	if len(allBonds) > 0 { // Probably no way to get here with empty allBonds, but checking anyway.
		if c := t.clientNode(peerID); c != nil {
			c.updateBonds(allBonds)
		}
	}

	t.sendResult(cl, msg.ID, true)

	return nil
}

// handleSubscription handles a new subscription, adding the subject to the
// map if it doesn't exist. It then distributes a NewSubscriber broadcast
// to all current subscribers and remote tatankas.
func (t *Tatanka) handleSubscription(c *client, msg *msgjson.Message) *msgjson.Error {
	if t.skipRelay(msg) {
		return nil
	}

	var sub *mj.Subscription
	if err := msg.Unmarshal(&sub); err != nil || sub == nil || sub.Topic == "" {
		t.log.Errorf("error unmarshaling subscription from %s: %w", c.ID, err)
		return msgjson.NewError(mj.ErrBadRequest, "is this payload a subscription?")
	}

	bcast := &mj.Broadcast{
		Topic:       sub.Topic,
		Subject:     sub.Subject,
		MessageType: mj.MessageTypeNewSubscriber,
		PeerID:      c.ID,
		Stamp:       time.Now(),
	}

	// Do a helper function here to keep things tidy below.
	relay := func(subs map[tanka.PeerID]struct{}) {
		clientMsg := mj.MustNotification(mj.RouteBroadcast, bcast)
		mj.SignMessage(t.priv, clientMsg)
		clientMsgB, _ := json.Marshal(clientMsg)
		for peerID := range subs {
			subscriber, found := t.clients[peerID]
			if !found {
				t.log.Errorf("client not found for subscriber %s on topic %q, subject %q", peerID, sub.Topic, sub.Subject)
				continue
			}

			if err := subscriber.Sender.SendRaw(clientMsgB); err != nil {
				// DRAFT TODO: Remove subscriber and client and disconnect?
				// Or do that in (*Tatanka).send?
				t.log.Errorf("Error relaying broadcast: %v", err)
				continue
			}
		}
	}

	// Send it to all other remote tatankas.
	t.relayBroadcast(bcast, c.ID)

	// Find it and broadcast to locally-connected clients, or add the subject if
	// it doesn't exist.
	t.clientMtx.Lock()
	defer t.clientMtx.Unlock()
	topic, exists := t.topics[sub.Topic]
	if exists {
		// We have the topic. Do we have the subject?
		topic.subscribers[c.ID] = struct{}{}
		subs, exists := topic.subjects[sub.Subject]
		if exists {
			// We already have the subject, distribute the broadcast to existing
			// subscribers.
			relay(subs)
			subs[c.ID] = struct{}{}
		} else {
			// Add the subject. Nothing to broadcast.
			topic.subjects[sub.Subject] = map[tanka.PeerID]struct{}{
				c.ID: {},
			}
		}
	} else {
		// New topic and subject.
		t.log.Tracef("Adding new subscription topic and subject %s -> %s", sub.Topic, sub.Subject)
		t.topics[sub.Topic] = &Topic{
			subjects: map[tanka.Subject]map[tanka.PeerID]struct{}{
				sub.Subject: {
					c.ID: {},
				},
			},
			subscribers: map[tanka.PeerID]struct{}{
				c.ID: {},
			},
		}
	}

	t.sendResult(c, msg.ID, true)
	t.replySubscription(c, sub.Topic)
	return nil
}

// replySubscription sends a follow up reply to a sender's subscription after
// their message has been processed successfully.
func (t *Tatanka) replySubscription(cl tanka.Sender, topic tanka.Topic) {
	switch topic {
	case mj.TopicFiatRate:
		if t.fiatOracleEnabled() {
			rates := t.fiatRateOracle.Rates()
			if len(rates) == 0 { // no data to send
				return
			}

			reply := mj.MustNotification(mj.RouteRates, &mj.RateMessage{
				Topic: mj.TopicFiatRate,
				Rates: rates,
			})

			if err := t.send(cl, reply); err != nil {
				peerID := cl.PeerID()
				t.log.Errorf("error sending result to %q: %v", dex.Bytes(peerID[:]), err)
			}
		}
	}
}

func (t *Tatanka) unsub(peerID tanka.PeerID, topicID tanka.Topic, subjectID tanka.Subject) *msgjson.Error {
	t.clientMtx.Lock()
	defer t.clientMtx.Unlock()
	topic, exists := t.topics[topicID]
	if !exists {
		t.log.Errorf("client %q unsubscribed from an unknown topic %q", peerID, topicID)
		return msgjson.NewError(mj.ErrBadRequest, "unknown topic")
	}
	if _, found := topic.subscribers[peerID]; !found {
		t.log.Errorf("client %q unsubscribed from topic %q, to which they were not suscribed", peerID, topicID)
		return msgjson.NewError(mj.ErrBadRequest, "unknown topic")
	}
	if subjectID == "" {
		// Unsubbing all subjects.
		for subID, subs := range topic.subjects {
			delete(subs, peerID)
			if len(subs) == 0 {
				delete(topic.subjects, subID)
			}
		}
	} else {
		subs, exists := topic.subjects[subjectID]
		if !exists {
			t.log.Errorf("client %q unsubscribed subject %q, topic %q, to which they were not suscribed", peerID, subjectID, topicID)
			return msgjson.NewError(mj.ErrBadRequest, "unknown subject")
		}
		delete(subs, peerID)
		if len(subs) == 0 {
			delete(topic.subjects, subjectID)
		}
	}

	if len(topic.subscribers) == 0 {
		delete(t.topics, topicID)
	}
	return nil
}

// skipRelay checks whether the message has already been handled. This function
// may not be necessary with the version 0 whitelisted mesh net, since
// it is expected to be highly connected and relays only go one hop.
func (t *Tatanka) skipRelay(msg *msgjson.Message) bool {
	bcastID := mj.MessageDigest(msg)
	t.relayMtx.Lock()
	defer t.relayMtx.Unlock()
	_, exists := t.recentRelays[bcastID]
	if !exists {
		t.recentRelays[bcastID] = time.Now()
	}
	return exists
}

// distributeBroadcastedMessage distributes the broadcast to any
// locally-connected subscribers.
func (t *Tatanka) distributeBroadcastedMessage(bcast *mj.Broadcast, mustExist bool) *msgjson.Error {
	relayedMsg := mj.MustNotification(mj.RouteBroadcast, bcast)
	mj.SignMessage(t.priv, relayedMsg)
	relayedMsgB, _ := json.Marshal(relayedMsg)

	t.clientMtx.RLock()
	defer t.clientMtx.RUnlock()
	topic, found := t.topics[bcast.Topic]
	if !found {
		if mustExist {
			t.log.Errorf("client %q broadcasted to an unknown topic %q", bcast.PeerID, bcast.Topic)
			return msgjson.NewError(mj.ErrBadRequest, "unknown topic")
		}
		return nil
	}

	relay := func(subs map[tanka.PeerID]struct{}) {
		for peerID := range subs {
			subscriber, found := t.clients[peerID]
			if !found {
				t.log.Errorf("client not found for subscriber %s on topic %q, subject %q", peerID, bcast.Topic, bcast.Subject)
				continue
			}

			if err := subscriber.Sender.SendRaw(relayedMsgB); err != nil {
				// DRAFT TODO: Remove subscriber and client and disconnect?
				// Or do that in (*Tatanka).send?
				t.log.Errorf("Error relaying broadcast: %v", err)
				continue
			}
		}
	}

	if bcast.Subject == "" {
		relay(topic.subscribers)
	} else {
		subs, found := topic.subjects[bcast.Subject]
		if !found {
			if mustExist {
				t.log.Errorf("client %s broadcasted to an unknown subject %q on topic %s", bcast.PeerID, bcast.Subject, bcast.Topic)
			}
			return msgjson.NewError(mj.ErrBadRequest, "unknown subject")
		}
		relay(subs)
	}
	return nil
}

// handleBroadcast handles a broadcast from a locally connected client,
// forwarding the message to all remote tatankas and local subscribers.
func (t *Tatanka) handleBroadcast(p *peer, msg *msgjson.Message, mustExist bool) *msgjson.Error {
	if t.skipRelay(msg) {
		return nil
	}

	var bcast *mj.Broadcast
	if err := msg.Unmarshal(&bcast); err != nil || bcast == nil || bcast.Topic == "" {
		t.log.Errorf("error unmarshaling broadcast from %s: %w", p.ID, err)
		return msgjson.NewError(mj.ErrBadRequest, "is this payload a broadcast?")
	}

	if bcast.PeerID != p.ID {
		t.log.Errorf("broadcast peer ID does not match connected client: %s != %s", bcast.PeerID, p.ID)
		return msgjson.NewError(mj.ErrBadRequest, "who's broadcast is this?")
	}

	if time.Since(bcast.Stamp) > tanka.EpochLength || time.Until(bcast.Stamp) > tanka.EpochLength {
		t.log.Errorf("Ignoring broadcast with old stamp received from %s", p.ID)
		return msgjson.NewError(mj.ErrBadRequest, "too old")
	}

	// Relay to remote tatankas first.
	t.relayBroadcast(bcast, p.ID)

	// Send to local subscribers.
	if msgErr := t.distributeBroadcastedMessage(bcast, mustExist); msgErr != nil {
		return msgErr
	}

	t.sendResult(p, msg.ID, true)

	// Handle unsubs.
	switch bcast.MessageType {
	case mj.MessageTypeUnsubTopic:
		t.unsub(p.ID, bcast.Topic, "")
	case mj.MessageTypeUnsubSubject:
		t.unsub(p.ID, bcast.Topic, bcast.Subject)
	}

	return nil
}

// relayBroadcast sends a relay_broadcast message to all remote tatankas.
func (t *Tatanka) relayBroadcast(bcast *mj.Broadcast, from tanka.PeerID) {
	relayedMsg := mj.MustNotification(mj.RouteRelayBroadcast, bcast)
	mj.SignMessage(t.priv, relayedMsg)
	relayedMsgB, _ := json.Marshal(relayedMsg)

	for _, tt := range t.tatankaNodes() {
		if tt.ID == from {
			// don't send back to sender
			continue
		}
		if err := tt.Sender.SendRaw(relayedMsgB); err != nil {
			t.log.Errorf("Error relaying broadcast to %s: %v", tt.ID, err)
		}
	}
}

// findPath finds remote tatankas that are hosting the specified peer.
func (t *Tatanka) findPath(peerID tanka.PeerID) []*remoteTatanka {
	job := &clientJob{
		task: &clientJobFindRemotes{
			clientID: peerID,
		},
		res: make(chan interface{}),
	}
	t.clientJobs <- job
	ttIDs := (<-job.res).(map[tanka.PeerID]struct{})

	nodes := make([]*remoteTatanka, 0, len(ttIDs))
	t.tatankasMtx.RLock()
	for ttID := range ttIDs {
		if tt, found := t.tatankas[ttID]; found {
			nodes = append(nodes, tt)
		}
	}
	t.tatankasMtx.RUnlock()
	return nodes
}

// handleTankagram forwards a tankagram from a locally connected client, sending
// it to the recipient if the recipient is also locally connected, or else
// bouncing it off of a remote tatanka if one is known.
func (t *Tatanka) handleTankagram(c *client, msg *msgjson.Message) *msgjson.Error {
	var gram mj.Tankagram
	if err := msg.Unmarshal(&gram); err != nil {
		t.log.Errorf("Error unmarshaling tankagram from %s: %w", c.ID, err)
		return msgjson.NewError(mj.ErrBadRequest, "bad tankagram")
	}

	if gram.From != c.ID {
		t.log.Errorf("Tankagram from %s has wrong sender %s", c.ID, gram.From)
		return msgjson.NewError(mj.ErrBadRequest, "wrong sender")
	}

	// The TankagramResult is signed separately.
	sendTankagramResult := func(r *mj.TankagramResult) {
		r.Sign(t.priv)
		t.sendResult(c, msg.ID, r)
	}

	t.clientMtx.RLock()
	recip, foundLocally := t.clients[gram.To]
	t.clientMtx.RUnlock()
	if foundLocally {
		var resB dex.Bytes
		relayedMsg := mj.MustRequest(mj.RouteTankagram, gram)
		sent, clientErr, err := t.requestAnyOne([]tanka.Sender{recip}, relayedMsg, &resB)
		if sent {
			sendTankagramResult(&mj.TankagramResult{Result: mj.TRTTransmitted, Response: resB})
			return nil
		}
		if clientErr != nil {
			sendTankagramResult(&mj.TankagramResult{Result: mj.TRTErrBadClient})
			return nil
		}
		if err != nil {
			t.log.Errorf("Error sending to local client %s: %v", recip.ID, err)
		}
	}

	// Either it's not a locally-connected client, or the send attempt failed.
	// See if we have any other routes.

	tts := t.findPath(gram.To)
	if len(tts) == 0 {
		// TODO: Disconnect client?
		t.log.Errorf("No local or remote client %s for tankagram from %s", gram.To, c.ID)
		sendTankagramResult(&mj.TankagramResult{Result: mj.TRTNoPath})
		return nil
	}

	relayedMsg := mj.MustRequest(mj.RouteRelayTankagram, gram)
	var r mj.TankagramResult
	sent, clientErr, err := t.requestAnyOne(tankasToSenders(tts), relayedMsg, &r)
	if sent {
		sendTankagramResult(&r)
		return nil
	}
	if clientErr != nil {
		// TODO: This means weren't able to communicate with the server
		// node. We should probably disconnect this server.
		sendTankagramResult(&mj.TankagramResult{Result: mj.TRTErrFromTanka})
		return nil
	}
	if err != nil {
		if errors.Is(err, ErrNoPath) {
			sendTankagramResult(&mj.TankagramResult{Result: mj.TRTNoPath})
		} else {
			t.log.Errorf("Error relaying tankagram: %v", err)
			sendTankagramResult(&mj.TankagramResult{Result: mj.TRTErrFromTanka})
		}
	}
	return nil
}

const ErrNoPath = dex.ErrorKind("no path")

// requestAnyOne tries to request from the senders in order until one succeeds.
func (t *Tatanka) requestAnyOne(senders []tanka.Sender, msg *msgjson.Message, resp interface{}) (sent bool, clientErr, err error) {
	mj.SignMessage(t.priv, msg)
	rawMsg, err := json.Marshal(msg)
	if err != nil {
		return false, nil, err
	}
	return t.requestAnyOneRaw(senders, msg.ID, rawMsg, resp)
}

func (t *Tatanka) requestAnyOneRaw(senders []tanka.Sender, msgID uint64, rawMsg []byte, resp interface{}) (sent bool, clientErr, err error) {
	for _, sender := range senders {
		var errChan = make(chan error)
		if err := sender.RequestRaw(msgID, rawMsg, func(msg *msgjson.Message) {
			if err := msg.UnmarshalResult(&resp); err != nil {
				errChan <- err
				return
			}
			errChan <- nil
		}); err != nil {
			peerID := sender.PeerID()
			t.log.Errorf("error sending message to %s. msg = %s, err = %v", dex.Bytes(peerID[:]), mj.Truncate(rawMsg), err)
			continue
		}
		select {
		case err := <-errChan:
			if err == nil {
				return true, nil, nil
			}
			// If we get here, that means we got a result from the client, but
			// it didn't parse. This is a client error.
			return false, err, nil
		case <-t.ctx.Done():
			return false, nil, nil
		}
	}
	return false, nil, ErrNoPath
}

func tankasToSenders(tts []*remoteTatanka) []tanka.Sender {
	senders := make([]tanka.Sender, len(tts))
	for i, tt := range tts {
		senders[i] = tt
	}
	return senders
}
