// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mesh

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"decred.org/dcrdex/server/db"
)

func testSession(cpNode, localNode string) *nodeConn {
	return newNodeConn(newTPeerConn(), cpNode, localNode)
}

var (
	testLocalFrontier = &db.EventLogPosition{Seq: 41, TipHash: []byte{0xde, 0xad, 0xbe, 0xef, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06}}
	testPeerFrontier  = &db.EventLogPosition{Seq: 44, TipHash: []byte{0xfe, 0xed, 0xfa, 0xce, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f}}
)

func testHandshakeResolvedSignal(conn *nodeConn, role helloRole, progress progressState) handshakeResolvedSignal {
	return handshakeResolvedSignal{
		conn:          conn,
		peerRole:      role,
		progress:      progress,
		localFrontier: testLocalFrontier,
		peerFrontier:  testPeerFrontier,
	}
}

func requireTransition(t *testing.T, localNodeID string, initial nodeState, signal meshSignal, want reduceResult) {
	t.Helper()
	result, err := reduceSignal(localNodeID, defaultSlavePromotionDelay, initial, signal)
	if err != nil {
		t.Fatalf("reduceSignal error: %v", err)
	}
	if !reflect.DeepEqual(result, want) {
		t.Fatalf("result = %#v, want %#v", result, want)
	}
}

func TestReduceConnectionAdoption(t *testing.T) {
	at := time.Unix(1700000000, 0)
	const nodeA = "node-a"
	const nodeB = "node-b"
	const nodeC = "node-c"
	t.Run("adopts lower initiator connection", func(t *testing.T) {
		localNode, cpNode := nodeA, nodeB
		active := testSession(cpNode, cpNode)
		adopted := testSession(cpNode, localNode)
		initial := nodeState{
			mode:       modeEstablishedMaster,
			activeConn: active,
		}
		signal := testHandshakeResolvedSignal(adopted, roleSlave, progressEqual)
		want := handledResult(nodeState{
			mode:       modeEstablishedMaster,
			activeConn: adopted,
		}, []effect{
			effectDisconnect{Conn: active},
			effectWatchConn{Conn: adopted},
			// No stream at adoption: the replacement conn's slave
			// must subscribe before anything streams.
			effectPeerClientEndpointChanged{},
		}).withOutcome(handshakeAdopted)
		requireTransition(t, localNode, initial, signal, want)
	})

	t.Run("rejects higher initiator connection", func(t *testing.T) {
		localNode, cpNode := nodeA, nodeB
		active := testSession(cpNode, localNode)
		rejected := testSession(cpNode, cpNode)
		initial := nodeState{
			mode:       modeEstablishedMaster,
			activeConn: active,
		}
		signal := testHandshakeResolvedSignal(rejected, roleSlave, progressEqual)
		want := handledResult(nodeState{
			mode:       modeEstablishedMaster,
			activeConn: active,
		}, nil).withOutcome(handshakeNotAdopted)
		requireTransition(t, localNode, initial, signal, want)
	})

	t.Run("same-peer handshake supersedes a stale connection", func(t *testing.T) {
		localNode, cpNode := nodeA, nodeB
		active := testSession(cpNode, localNode)
		redial := testSession(cpNode, cpNode)
		sig := testHandshakeResolvedSignal(redial, roleSlave, progressEqual)
		sig.at = at.Add(supersedeAfter)
		initial := nodeState{
			mode:        modeEstablishedMaster,
			activeConn:  active,
			connAdopted: at,
		}
		signal := sig
		want := handledResult(nodeState{
			mode:        modeEstablishedMaster,
			activeConn:  redial,
			connAdopted: at.Add(supersedeAfter),
		}, []effect{
			effectDisconnect{Conn: active},
			effectWatchConn{Conn: redial},
			effectPeerClientEndpointChanged{},
		}).withOutcome(handshakeAdopted)
		requireTransition(t, localNode, initial, signal, want)
	})

	t.Run("fresh same-peer conflict still tie-breaks", func(t *testing.T) {
		localNode, cpNode := nodeA, nodeB
		active := testSession(cpNode, localNode)
		redial := testSession(cpNode, cpNode)
		sig := testHandshakeResolvedSignal(redial, roleSlave, progressEqual)
		sig.at = at.Add(supersedeAfter - time.Second)
		initial := nodeState{
			mode:        modeEstablishedMaster,
			activeConn:  active,
			connAdopted: at,
		}
		signal := sig
		want := handledResult(nodeState{
			mode:        modeEstablishedMaster,
			activeConn:  active,
			connAdopted: at,
		}, nil).withOutcome(handshakeNotAdopted)
		requireTransition(t, localNode, initial, signal, want)
	})

	t.Run("rejects connection from different peer", func(t *testing.T) {
		localNode, cpNode := nodeA, nodeB
		otherNode := nodeC
		active := testSession(cpNode, cpNode)
		differentPeer := testSession(otherNode, otherNode)
		initial := nodeState{
			mode:       modeEstablishedMaster,
			activeConn: active,
		}
		signal := testHandshakeResolvedSignal(differentPeer, roleSlave, progressEqual)
		want := handledResult(nodeState{
			mode:       modeEstablishedMaster,
			activeConn: active,
		}, nil).withOutcome(handshakeNotAdopted)
		requireTransition(t, localNode, initial, signal, want)
	})
}

func TestReduceRoleSelection(t *testing.T) {
	at := time.Unix(1700000000, 0)
	const nodeA = "node-a"
	const nodeB = "node-b"
	t.Run("equal progress local wins tie break", func(t *testing.T) {
		localNode, cpNode := nodeA, nodeB
		peerConn := testSession(cpNode, localNode)
		initial := nodeState{mode: modePending}
		signal := testHandshakeResolvedSignal(peerConn, roleUnknown, progressEqual)
		want := handledResult(nodeState{
			mode:       modePreparingMaster,
			activeConn: peerConn,
		}, []effect{
			effectBecameMaster{},
			effectWatchConn{Conn: peerConn},
			effectPeerClientEndpointChanged{},
			effectFailPendingCommands{Reason: "node promoted to mesh master"},
		}).withOutcome(handshakeAdopted)
		requireTransition(t, localNode, initial, signal, want)
	})

	t.Run("equal progress local loses tie break", func(t *testing.T) {
		localNode, cpNode := nodeB, nodeA
		tieLoseConn := testSession(cpNode, localNode)
		initial := nodeState{mode: modePending}
		signal := testHandshakeResolvedSignal(tieLoseConn, roleUnknown, progressEqual)
		want := handledResult(nodeState{
			mode:       modeEstablishedSlave,
			activeConn: tieLoseConn,
		}, []effect{
			effectWatchConn{Conn: tieLoseConn},
			effectStartSubscriber{Conn: tieLoseConn},
			effectPeerClientEndpointChanged{},
			effectStartupResolved{},
		}).withOutcome(handshakeAdopted)
		requireTransition(t, localNode, initial, signal, want)
	})

	t.Run("equal progress peer master becomes slave", func(t *testing.T) {
		localNode, cpNode := nodeB, nodeA
		peerMasterConn := testSession(cpNode, cpNode)
		initial := nodeState{mode: modePending}
		signal := testHandshakeResolvedSignal(peerMasterConn, roleMaster, progressEqual)
		want := handledResult(nodeState{
			mode:       modeEstablishedSlave,
			activeConn: peerMasterConn,
		}, []effect{
			effectWatchConn{Conn: peerMasterConn},
			effectStartSubscriber{Conn: peerMasterConn},
			effectPeerClientEndpointChanged{},
			effectStartupResolved{},
		}).withOutcome(handshakeAdopted)
		requireTransition(t, localNode, initial, signal, want)
	})

	t.Run("local ahead becomes master", func(t *testing.T) {
		localNode, cpNode := nodeA, nodeB
		peerConn := testSession(cpNode, localNode)
		initial := nodeState{mode: modePending}
		signal := testHandshakeResolvedSignal(peerConn, roleUnknown, progressLocalAhead)
		want := handledResult(nodeState{
			mode:       modePreparingMaster,
			activeConn: peerConn,
		}, []effect{
			effectBecameMaster{},
			effectWatchConn{Conn: peerConn},
			effectPeerClientEndpointChanged{},
			effectFailPendingCommands{Reason: "node promoted to mesh master"},
		}).withOutcome(handshakeAdopted)
		requireTransition(t, localNode, initial, signal, want)
	})

	t.Run("peer ahead enters stream sync", func(t *testing.T) {
		localNode, cpNode := nodeA, nodeB
		peerConn := testSession(cpNode, localNode)
		initial := nodeState{mode: modePending}
		signal := testHandshakeResolvedSignal(peerConn, roleMaster, progressPeerAhead)
		want := handledResult(nodeState{
			mode:       modeEstablishedSlaveSyncing,
			activeConn: peerConn,
		}, []effect{
			effectWatchConn{Conn: peerConn},
			effectStartSubscriber{Conn: peerConn},
			effectPeerClientEndpointChanged{},
		}).withOutcome(handshakeAdopted)
		requireTransition(t, localNode, initial, signal, want)
	})

	t.Run("syncing replacement connection with equal frontier becomes established slave", func(t *testing.T) {
		localNode, cpNode := nodeB, nodeA
		active := testSession(cpNode, localNode)
		replacement := testSession(cpNode, cpNode)
		initial := nodeState{
			mode:       modeEstablishedSlaveSyncing,
			activeConn: active,
		}
		signal := testHandshakeResolvedSignal(replacement, roleMaster, progressEqual)
		want := handledResult(nodeState{
			mode:       modeEstablishedSlave,
			activeConn: replacement,
		}, []effect{
			effectDisconnect{Conn: active},
			effectWatchConn{Conn: replacement},
			effectStartSubscriber{Conn: replacement},
			effectPeerClientEndpointChanged{},
			effectStartupResolved{},
		}).withOutcome(handshakeAdopted)
		requireTransition(t, localNode, initial, signal, want)
	})

	t.Run("established master peer ahead halts", func(t *testing.T) {
		localNode, cpNode := nodeA, nodeB
		peerConn := testSession(cpNode, localNode)
		initial := nodeState{mode: modeEstablishedMaster}
		signal := testHandshakeResolvedSignal(peerConn, roleSlave, progressPeerAhead)
		want := handledResult(nodeState{
			mode:    modeHalted,
			haltErr: fmt.Errorf("halting after handshake: local role master, peer %s role slave, progress peer_ahead", cpNode),
		}, []effect{
			effectDisconnect{Conn: peerConn},
			effectHalt{},
		}).withOutcome(handshakeHalted)
		requireTransition(t, localNode, initial, signal, want)
	})

	t.Run("diverged handshake halts", func(t *testing.T) {
		localNode, cpNode := nodeA, nodeB
		peerConn := testSession(cpNode, localNode)
		initial := nodeState{mode: modePending}
		signal := testHandshakeResolvedSignal(peerConn, roleSlave, progressDiverged)
		want := handledResult(nodeState{
			mode: modeHalted,
			haltErr: fmt.Errorf("MESH FORK DETECTED: halting after handshake, event logs diverged "+
				"(local role unknown frontier Position{seq=41 hash=deadbeef010203040506}, peer %s role slave frontier Position{seq=44 hash=feedface0a0b0c0d0e0f})", cpNode),
		}, []effect{
			effectDisconnect{Conn: peerConn},
			effectHalt{},
		}).withOutcome(handshakeHalted)
		requireTransition(t, localNode, initial, signal, want)
	})

	t.Run("diverged startup join against serving master halts with reset token", func(t *testing.T) {
		localNode, cpNode := nodeA, nodeB
		peerConn := testSession(cpNode, localNode)
		initial := nodeState{mode: modePending}
		signal := testHandshakeResolvedSignal(peerConn, roleMaster, progressDiverged)
		want := handledResult(nodeState{
			mode: modeHalted,
			haltErr: fmt.Errorf("MESH FORK DETECTED: halting after handshake, event logs diverged "+
				"(local role unknown frontier Position{seq=41 hash=deadbeef010203040506}, peer %s role master frontier Position{seq=44 hash=feedface0a0b0c0d0e0f})"+
				"; this node holds committed events the serving master never received. "+
				"Back up the database for audit (pg_dump), then restart with --meshforkreset=41:deadbeef01020304 "+
				"to wipe and resync from the master", cpNode),
		}, []effect{
			effectDisconnect{Conn: peerConn},
			effectHalt{},
		}).withOutcome(handshakeHalted)
		requireTransition(t, localNode, initial, signal, want)
	})

	t.Run("serving master rejects diverged non-master join and keeps serving", func(t *testing.T) {
		localNode, cpNode := nodeA, nodeB
		peerConn := testSession(cpNode, localNode)
		initial := nodeState{mode: modeEstablishedMaster}
		signal := testHandshakeResolvedSignal(peerConn, roleUnknown, progressDiverged)
		want := handledResult(nodeState{
			mode: modeEstablishedMaster,
		}, []effect{
			effectDisconnect{Conn: peerConn},
		}).withOutcome(handshakeDivergedJoinRejected)
		requireTransition(t, localNode, initial, signal, want)
	})

	t.Run("preparing master rejects diverged slave join and keeps preparing", func(t *testing.T) {
		localNode, cpNode := nodeA, nodeB
		peerConn := testSession(cpNode, localNode)
		initial := nodeState{mode: modePreparingMaster}
		signal := testHandshakeResolvedSignal(peerConn, roleSlave, progressDiverged)
		want := handledResult(nodeState{
			mode: modePreparingMaster,
		}, []effect{
			effectDisconnect{Conn: peerConn},
		}).withOutcome(handshakeDivergedJoinRejected)
		requireTransition(t, localNode, initial, signal, want)
	})

	t.Run("symmetric fork master versus master still halts", func(t *testing.T) {
		localNode, cpNode := nodeA, nodeB
		peerConn := testSession(cpNode, localNode)
		initial := nodeState{mode: modeEstablishedMaster}
		signal := testHandshakeResolvedSignal(peerConn, roleMaster, progressDiverged)
		want := handledResult(nodeState{
			mode: modeHalted,
			haltErr: fmt.Errorf("MESH FORK DETECTED: halting after handshake, event logs diverged "+
				"(local role master frontier Position{seq=41 hash=deadbeef010203040506}, peer %s role master frontier Position{seq=44 hash=feedface0a0b0c0d0e0f})", cpNode),
		}, []effect{
			effectDisconnect{Conn: peerConn},
			effectHalt{},
		}).withOutcome(handshakeHalted)
		requireTransition(t, localNode, initial, signal, want)
	})

	t.Run("equal master master halts", func(t *testing.T) {
		localNode, cpNode := nodeA, nodeB
		peerConn := testSession(cpNode, localNode)
		initial := nodeState{mode: modeEstablishedMaster}
		signal := testHandshakeResolvedSignal(peerConn, roleMaster, progressEqual)
		want := handledResult(nodeState{
			mode:    modeHalted,
			haltErr: fmt.Errorf("halting after handshake: local role master, peer %s role master, progress equal", cpNode),
		}, []effect{
			effectDisconnect{Conn: peerConn},
			effectHalt{},
		}).withOutcome(handshakeHalted)
		requireTransition(t, localNode, initial, signal, want)
	})

	t.Run("slave rejoins behind an established master", func(t *testing.T) {
		localNode, cpNode := nodeA, nodeB
		peerConn := testSession(cpNode, localNode)
		initial := nodeState{mode: modeEstablishedMaster, peerDisconnected: at}
		signal := testHandshakeResolvedSignal(peerConn, roleSlave, progressLocalAhead)
		want := handledResult(nodeState{
			mode:       modeEstablishedMaster,
			activeConn: peerConn,
		}, []effect{
			effectWatchConn{Conn: peerConn},
			effectPeerClientEndpointChanged{},
		}).withOutcome(handshakeAdopted)
		requireTransition(t, localNode, initial, signal, want)
	})
}

func TestReduceStreamProgress(t *testing.T) {
	at := time.Unix(1700000000, 0)
	const nodeA = "node-a"
	const nodeB = "node-b"
	t.Run("stream caught up establishes slave", func(t *testing.T) {
		localNode, cpNode := nodeA, nodeB
		streamConn := testSession(cpNode, localNode)
		initial := nodeState{mode: modeEstablishedSlaveSyncing, activeConn: streamConn}
		signal := streamCaughtUpSignal{conn: streamConn, target: &db.EventLogPosition{Seq: 2, TipHash: testTipHash(2)}}
		want := handledResult(nodeState{
			mode:       modeEstablishedSlave,
			activeConn: streamConn,
		}, []effect{effectStartupResolved{}})
		requireTransition(t, localNode, initial, signal, want)
	})

	t.Run("master stream failure disconnects peer and remains master", func(t *testing.T) {
		localNode, cpNode := nodeA, nodeB
		streamConn := testSession(cpNode, localNode)
		initial := nodeState{mode: modeEstablishedMaster, activeConn: streamConn}
		signal := streamFailedSignal{conn: streamConn, err: errors.New("boom"), at: at}
		want := handledResult(nodeState{
			mode:             modeEstablishedMaster,
			peerDisconnected: at,
		}, []effect{effectDisconnect{Conn: streamConn}})
		requireTransition(t, localNode, initial, signal, want)
	})

	t.Run("stale stream caught up ignored", func(t *testing.T) {
		localNode, cpNode := nodeA, nodeB
		streamConn := testSession(cpNode, localNode)
		staleConn := testSession(cpNode, localNode)
		initial := nodeState{mode: modeEstablishedSlaveSyncing, activeConn: streamConn}
		signal := streamCaughtUpSignal{conn: staleConn, target: &db.EventLogPosition{Seq: 2, TipHash: testTipHash(2)}}
		want := ignoredResult(nodeState{
			mode:       modeEstablishedSlaveSyncing,
			activeConn: streamConn,
		})
		requireTransition(t, localNode, initial, signal, want)
	})

	t.Run("stale stream failure ignored", func(t *testing.T) {
		localNode, cpNode := nodeA, nodeB
		streamConn := testSession(cpNode, localNode)
		staleConn := testSession(cpNode, localNode)
		initial := nodeState{mode: modeEstablishedMaster, activeConn: streamConn}
		signal := streamFailedSignal{conn: staleConn, err: errors.New("boom"), at: at}
		want := ignoredResult(nodeState{
			mode:       modeEstablishedMaster,
			activeConn: streamConn,
		})
		requireTransition(t, localNode, initial, signal, want)
	})
}

func TestReduceDisconnect(t *testing.T) {
	at := time.Unix(1700000000, 0)
	const nodeA = "node-a"
	const nodeB = "node-b"
	t.Run("disconnect during stream sync returns pending", func(t *testing.T) {
		localNode, cpNode := nodeA, nodeB
		streamConn := testSession(cpNode, localNode)
		initial := nodeState{mode: modeEstablishedSlaveSyncing, activeConn: streamConn}
		signal := connectionDisconnectedSignal{conn: streamConn, at: at}
		want := handledResult(nodeState{
			mode:             modePending,
			peerDisconnected: at,
		}, []effect{effectStopEventStream{Conn: streamConn}})
		requireTransition(t, localNode, initial, signal, want)
	})

	t.Run("slave disconnect enters slave without master", func(t *testing.T) {
		localNode, cpNode := nodeA, nodeB
		slaveConn := testSession(cpNode, localNode)
		initial := nodeState{mode: modeEstablishedSlave, activeConn: slaveConn}
		signal := connectionDisconnectedSignal{conn: slaveConn, at: at}
		want := handledResult(nodeState{
			mode:             modeSlaveNoMaster,
			peerDisconnected: at,
		}, []effect{
			effectStopEventStream{Conn: slaveConn},
			effectScheduleSlavePromotionCheck{},
			effectFailPendingCommands{Reason: "mesh slave lost its master connection"},
		})
		requireTransition(t, localNode, initial, signal, want)
	})

	t.Run("established master keeps its mode on disconnect", func(t *testing.T) {
		localNode, cpNode := nodeA, nodeB
		activeConn := testSession(cpNode, localNode)
		initial := nodeState{mode: modeEstablishedMaster, activeConn: activeConn}
		signal := connectionDisconnectedSignal{conn: activeConn, at: at}
		want := handledResult(nodeState{
			mode:             modeEstablishedMaster,
			peerDisconnected: at,
		}, []effect{effectStopEventStream{Conn: activeConn}})
		requireTransition(t, localNode, initial, signal, want)
	})

	t.Run("preparing master keeps its mode on disconnect", func(t *testing.T) {
		localNode, cpNode := nodeA, nodeB
		activeConn := testSession(cpNode, localNode)
		initial := nodeState{mode: modePreparingMaster, activeConn: activeConn}
		signal := connectionDisconnectedSignal{conn: activeConn, at: at}
		want := handledResult(nodeState{
			mode:             modePreparingMaster,
			peerDisconnected: at,
		}, []effect{effectStopEventStream{Conn: activeConn}})
		requireTransition(t, localNode, initial, signal, want)
	})
}

func TestReduceSlavePromotion(t *testing.T) {
	at := time.Unix(1700000000, 0)
	const nodeA = "node-a"
	t.Run("promotion delay starts master preparation", func(t *testing.T) {
		localNode := nodeA
		initial := nodeState{
			mode:             modeSlaveNoMaster,
			peerDisconnected: at,
		}
		signal := slavePromotionCheckSignal{at: at.Add(defaultSlavePromotionDelay)}
		want := handledResult(nodeState{
			mode: modePreparingMaster,
		}, []effect{
			effectBecameMaster{},
			effectFailPendingCommands{Reason: "node promoted to mesh master"},
		})
		requireTransition(t, localNode, initial, signal, want)
	})

	t.Run("slave promotion check ignored before delay elapses", func(t *testing.T) {
		localNode := nodeA
		initial := nodeState{
			mode:             modeSlaveNoMaster,
			peerDisconnected: at,
		}
		signal := slavePromotionCheckSignal{at: at.Add(defaultSlavePromotionDelay - time.Millisecond)}
		want := ignoredResult(nodeState{
			mode:             modeSlaveNoMaster,
			peerDisconnected: at,
		})
		requireTransition(t, localNode, initial, signal, want)
	})

	t.Run("stale slave promotion check ignored after newer disconnect", func(t *testing.T) {
		localNode := nodeA
		newerDisconnect := at.Add(time.Second)
		initial := nodeState{
			mode:             modeSlaveNoMaster,
			peerDisconnected: newerDisconnect,
		}
		signal := slavePromotionCheckSignal{at: at.Add(defaultSlavePromotionDelay)}
		want := ignoredResult(nodeState{
			mode:             modeSlaveNoMaster,
			peerDisconnected: newerDisconnect,
		})
		requireTransition(t, localNode, initial, signal, want)
	})

	t.Run("master evidence restarts the promotion window", func(t *testing.T) {
		localNode := nodeA
		evidenceAt := at.Add(10 * time.Second)
		initial := nodeState{
			mode:             modeSlaveNoMaster,
			peerDisconnected: at,
		}
		signal := masterEvidenceSignal{at: evidenceAt}
		want := handledResult(nodeState{
			mode:             modeSlaveNoMaster,
			peerDisconnected: evidenceAt,
		}, []effect{
			effectScheduleSlavePromotionCheck{},
		})
		requireTransition(t, localNode, initial, signal, want)
	})

	t.Run("stale master evidence ignored", func(t *testing.T) {
		localNode := nodeA
		initial := nodeState{
			mode:             modeSlaveNoMaster,
			peerDisconnected: at,
		}
		signal := masterEvidenceSignal{at: at}
		want := ignoredResult(nodeState{
			mode:             modeSlaveNoMaster,
			peerDisconnected: at,
		})
		requireTransition(t, localNode, initial, signal, want)
	})

	t.Run("master evidence ignored outside slave-no-master", func(t *testing.T) {
		localNode := nodeA
		initial := nodeState{mode: modePending}
		signal := masterEvidenceSignal{at: at.Add(10 * time.Second)}
		want := ignoredResult(nodeState{mode: modePending})
		requireTransition(t, localNode, initial, signal, want)
	})
}

func TestReduceMasterHandoff(t *testing.T) {
	at := time.Unix(1700000000, 0)
	const nodeA = "node-a"
	const nodeB = "node-b"
	t.Run("planned handoff promotes caught-up slave immediately", func(t *testing.T) {
		localNode, cpNode := nodeA, nodeB
		active := testSession(cpNode, localNode)
		local := &db.EventLogPosition{Seq: 5, TipHash: testTipHash(5)}
		target := &db.EventLogPosition{Seq: 5, TipHash: testTipHash(5)}
		initial := nodeState{mode: modeEstablishedSlave, activeConn: active}
		signal := plannedHandoffSignal{conn: active, local: local, target: target, at: at}
		want := handledResult(nodeState{
			mode:       modePreparingMaster,
			activeConn: active,
		}, []effect{
			effectBecameMaster{},
			effectFailPendingCommands{Reason: "node promoted to mesh master"},
		})
		requireTransition(t, localNode, initial, signal, want)
	})

	t.Run("planned handoff promotes durably caught-up syncing slave", func(t *testing.T) {
		localNode, cpNode := nodeA, nodeB
		active := testSession(cpNode, localNode)
		local := &db.EventLogPosition{Seq: 5, TipHash: testTipHash(5)}
		target := &db.EventLogPosition{Seq: 5, TipHash: testTipHash(5)}
		initial := nodeState{mode: modeEstablishedSlaveSyncing, activeConn: active}
		signal := plannedHandoffSignal{conn: active, local: local, target: target, at: at}
		want := handledResult(nodeState{
			mode:       modePreparingMaster,
			activeConn: active,
		}, []effect{
			effectBecameMaster{},
			effectFailPendingCommands{Reason: "node promoted to mesh master"},
		})
		requireTransition(t, localNode, initial, signal, want)
	})

	t.Run("planned handoff halts on behind frontier", func(t *testing.T) {
		localNode, cpNode := nodeA, nodeB
		active := testSession(cpNode, localNode)
		local := &db.EventLogPosition{Seq: 4, TipHash: testTipHash(4)}
		target := &db.EventLogPosition{Seq: 5, TipHash: testTipHash(5)}
		haltErr := fmt.Errorf("planned handoff frontier mismatch (local %s, master %s)", local, target)
		initial := nodeState{mode: modeEstablishedSlave, activeConn: active}
		signal := plannedHandoffSignal{conn: active, local: local, target: target, at: at}
		want := handledResult(nodeState{
			mode:             modeHalted,
			peerDisconnected: at,
			haltErr:          haltErr,
		}, []effect{
			effectDisconnect{Conn: active},
			effectHalt{},
		})
		requireTransition(t, localNode, initial, signal, want)
	})

	t.Run("planned handoff halts on conflicting frontier", func(t *testing.T) {
		localNode, cpNode := nodeA, nodeB
		active := testSession(cpNode, localNode)
		local := &db.EventLogPosition{Seq: 5, TipHash: testTipHash(5)}
		target := &db.EventLogPosition{Seq: 5, TipHash: testTipHash(6)}
		haltErr := fmt.Errorf("planned handoff frontier mismatch (local %s, master %s)", local, target)
		initial := nodeState{mode: modeEstablishedSlave, activeConn: active}
		signal := plannedHandoffSignal{conn: active, local: local, target: target, at: at}
		want := handledResult(nodeState{
			mode:             modeHalted,
			peerDisconnected: at,
			haltErr:          haltErr,
		}, []effect{
			effectDisconnect{Conn: active},
			effectHalt{},
		})
		requireTransition(t, localNode, initial, signal, want)
	})

	t.Run("planned handoff from stale connection is ignored", func(t *testing.T) {
		localNode, cpNode := nodeA, nodeB
		active := testSession(cpNode, localNode)
		stale := testSession(cpNode, localNode)
		state := nodeState{mode: modeEstablishedSlave, activeConn: active}
		frontier := &db.EventLogPosition{Seq: 5, TipHash: testTipHash(5)}
		initial := state
		signal := plannedHandoffSignal{
			conn: stale, local: frontier, target: frontier, at: at,
		}
		want := ignoredResult(state)
		requireTransition(t, localNode, initial, signal, want)
	})
}

func TestReduceMasterReadiness(t *testing.T) {
	at := time.Unix(1700000000, 0)
	const nodeA = "node-a"
	const nodeB = "node-b"
	t.Run("master ready establishes master and starts no stream", func(t *testing.T) {
		localNode, cpNode := nodeA, nodeB
		slaveConn := testSession(cpNode, localNode)
		initial := nodeState{
			mode:       modePreparingMaster,
			activeConn: slaveConn,
		}
		signal := masterReadySignal{}
		want := handledResult(nodeState{
			mode:       modeEstablishedMaster,
			activeConn: slaveConn,
		}, []effect{
			effectStartupResolved{},
		})
		requireTransition(t, localNode, initial, signal, want)
	})

	t.Run("master preparation failure halts authoritative master", func(t *testing.T) {
		localNode, cpNode := nodeA, nodeB
		slaveConn := testSession(cpNode, localNode)
		prepErr := errors.New("market startup cleanup failed")
		initial := nodeState{
			mode:       modePreparingMaster,
			activeConn: slaveConn,
		}
		signal := masterPreparationFailedSignal{err: prepErr, at: at}
		want := handledResult(nodeState{
			mode:             modeHalted,
			peerDisconnected: at,
			haltErr:          prepErr,
		}, []effect{
			effectDisconnect{Conn: slaveConn},
			effectHalt{},
		})
		requireTransition(t, localNode, initial, signal, want)
	})
}

func TestReduceDialIncompatibility(t *testing.T) {
	at := time.Unix(1700000000, 0)
	const nodeA = "node-a"
	const nodeB = "node-b"
	t.Run("startup incompatibility halts and disconnects", func(t *testing.T) {
		localNode, cpNode := nodeA, nodeB
		incompatConn := testSession(cpNode, localNode)
		initial := nodeState{mode: modePending, activeConn: incompatConn}
		signal := dialIncompatibleSignal{err: errors.New("boom"), at: at}
		want := handledResult(nodeState{
			mode:             modeHalted,
			peerDisconnected: at,
			haltErr:          errors.New("boom"),
		}, []effect{effectDisconnect{Conn: incompatConn}, effectHalt{}})
		requireTransition(t, localNode, initial, signal, want)
	})

	t.Run("startup incompatibility without conn halts", func(t *testing.T) {
		localNode := nodeA
		initial := nodeState{mode: modePending}
		signal := dialIncompatibleSignal{err: errors.New("boom"), at: at}
		want := handledResult(nodeState{
			mode:    modeHalted,
			haltErr: errors.New("boom"),
		}, []effect{effectHalt{}})
		requireTransition(t, localNode, initial, signal, want)
	})

	t.Run("preparing master dial incompatibility keeps active conn", func(t *testing.T) {
		localNode, cpNode := nodeA, nodeB
		activeConn := testSession(cpNode, localNode)
		initial := nodeState{mode: modePreparingMaster, activeConn: activeConn}
		signal := dialIncompatibleSignal{err: errors.New("boom"), at: at}
		want := ignoredResult(nodeState{
			mode:       modePreparingMaster,
			activeConn: activeConn,
		})
		requireTransition(t, localNode, initial, signal, want)
	})

	t.Run("slave-no-master incompatibility ignored", func(t *testing.T) {
		localNode := nodeA
		initial := nodeState{mode: modeSlaveNoMaster, peerDisconnected: at}
		signal := dialIncompatibleSignal{err: errors.New("boom"), at: at}
		want := ignoredResult(nodeState{mode: modeSlaveNoMaster, peerDisconnected: at})
		requireTransition(t, localNode, initial, signal, want)
	})
}

func TestReduceSubscribeRejection(t *testing.T) {
	at := time.Unix(1700000000, 0)
	const nodeA = "node-a"
	const nodeB = "node-b"
	t.Run("subscribe rejected from replaced conn ignored", func(t *testing.T) {
		localNode, cpNode := nodeA, nodeB
		activeConn := testSession(cpNode, localNode)
		staleConn := testSession(cpNode, localNode)
		initial := nodeState{mode: modeEstablishedSlaveSyncing, activeConn: activeConn}
		signal := subscribeRejectedSignal{conn: staleConn, err: errors.New("boom"), at: at}
		want := ignoredResult(nodeState{
			mode:       modeEstablishedSlaveSyncing,
			activeConn: activeConn,
		})
		requireTransition(t, localNode, initial, signal, want)
	})

	t.Run("subscribe rejected from active conn halts", func(t *testing.T) {
		localNode, cpNode := nodeA, nodeB
		activeConn := testSession(cpNode, localNode)
		initial := nodeState{mode: modeEstablishedSlaveSyncing, activeConn: activeConn}
		signal := subscribeRejectedSignal{conn: activeConn, err: errors.New("boom"), at: at}
		want := handledResult(nodeState{
			mode:             modeHalted,
			peerDisconnected: at,
			haltErr:          errors.New("boom"),
		}, []effect{effectDisconnect{Conn: activeConn}, effectHalt{}})
		requireTransition(t, localNode, initial, signal, want)
	})
}

func TestReduceTerminalApplyFailure(t *testing.T) {
	at := time.Unix(1700000000, 0)
	const nodeA = "node-a"
	const nodeB = "node-b"
	t.Run("terminal apply failure halts and disconnects the active conn", func(t *testing.T) {
		localNode, cpNode := nodeA, nodeB
		activeConn := testSession(cpNode, localNode)
		initial := nodeState{mode: modeEstablishedMaster, activeConn: activeConn}
		signal := terminalApplyFailureSignal{err: errors.New("boom"), at: at}
		want := handledResult(nodeState{
			mode:             modeHalted,
			peerDisconnected: at,
			haltErr:          errors.New("boom"),
		}, []effect{effectDisconnect{Conn: activeConn}, effectHalt{}})
		requireTransition(t, localNode, initial, signal, want)
	})

	t.Run("terminal apply failure halts without a conn", func(t *testing.T) {
		initial := nodeState{mode: modePending}
		signal := terminalApplyFailureSignal{err: errors.New("boom"), at: at}
		want := handledResult(nodeState{
			mode:    modeHalted,
			haltErr: errors.New("boom"),
		}, []effect{effectHalt{}})
		requireTransition(t, nodeA, initial, signal, want)
	})

	t.Run("terminal apply failure ignored when already halted", func(t *testing.T) {
		halted := nodeState{mode: modeHalted, haltErr: errors.New("first")}
		initial := halted
		signal := terminalApplyFailureSignal{err: errors.New("second"), at: at}
		want := ignoredResult(halted)
		requireTransition(t, nodeA, initial, signal, want)
	})
}

// TestHelloFrontierIsNotAStreamPosition checks that handshake adoption
// does not start an event stream.
func TestHelloFrontierIsNotAStreamPosition(t *testing.T) {
	conn := testSession("node-b", "node-a")
	cur := nodeState{mode: modeEstablishedMaster}
	result, err := reduceSignal("node-a", defaultSlavePromotionDelay, cur,
		testHandshakeResolvedSignal(conn, roleSlave, progressLocalAhead))
	if err != nil {
		t.Fatalf("reduce error: %v", err)
	}
	if !result.handled {
		t.Fatal("handshake not handled")
	}
	for _, eff := range result.effects {
		if _, ok := eff.(effectStartEventStream); ok {
			t.Fatal("adoption started a stream; streaming must be slave-initiated")
		}
	}
}

func TestReduceStreamSubscribe(t *testing.T) {
	frontier := &db.EventLogPosition{Seq: 4, TipHash: testTipHash(4)}
	conn := testSession("node-b", "node-a")

	tests := []struct {
		name      string
		mode      nodeMode
		connID    uint64
		wantStart bool
	}{
		{"established master starts stream", modeEstablishedMaster, conn.ID(), true},
		{"preparing master rejects retryably", modePreparingMaster, conn.ID(), false},
		{"slave ignores", modeEstablishedSlave, conn.ID(), false},
		{"pending ignores", modePending, conn.ID(), false},
		{"non-active conn ignores", modeEstablishedMaster, conn.ID() + 1, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cur := nodeState{mode: tt.mode, activeConn: conn}
			result, err := reduceSignal("node-a", defaultSlavePromotionDelay, cur,
				streamSubscribeSignal{connID: tt.connID, frontier: frontier})
			if err != nil {
				t.Fatalf("reduce error: %v", err)
			}
			if result.handled != tt.wantStart {
				t.Fatalf("handled = %t, want %t", result.handled, tt.wantStart)
			}
			var started *effectStartEventStream
			for _, eff := range result.effects {
				if start, ok := eff.(effectStartEventStream); ok {
					started = &start
				}
			}
			if (started != nil) != tt.wantStart {
				t.Fatalf("stream start = %v, want %t", started, tt.wantStart)
			}
			if started != nil {
				if started.SlaveFrontier.Seq != frontier.Seq {
					t.Fatalf("start effect = %+v, want subscribe frontier %d", started, frontier.Seq)
				}
			}
		})
	}
}

func TestReduceSnapshotRequest(t *testing.T) {
	conn := testSession("node-b", "node-a")

	tests := []struct {
		name      string
		mode      nodeMode
		connID    uint64
		wantStart bool
	}{
		{"established master starts send", modeEstablishedMaster, conn.ID(), true},
		{"preparing master rejects retryably", modePreparingMaster, conn.ID(), false},
		{"non-active conn ignores", modeEstablishedMaster, conn.ID() + 1, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cur := nodeState{mode: tt.mode, activeConn: conn}
			result, err := reduceSignal("node-a", defaultSlavePromotionDelay, cur,
				snapshotRequestSignal{connID: tt.connID})
			if err != nil {
				t.Fatalf("reduce error: %v", err)
			}
			if result.handled != tt.wantStart {
				t.Fatalf("handled = %t, want %t", result.handled, tt.wantStart)
			}
			var started bool
			for _, eff := range result.effects {
				if _, ok := eff.(effectStartSnapshotSend); ok {
					started = true
				}
			}
			if started != tt.wantStart {
				t.Fatalf("snapshot send start = %t, want %t", started, tt.wantStart)
			}
		})
	}
}
